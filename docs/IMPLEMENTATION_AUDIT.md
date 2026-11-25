# MagicDown — Phase 1 Implementation Audit

- **Ngày thực hiện:** 2026-11-26
- **Branch / Commit baseline:** `hongducdev/audit-magicdown-phase1` (dựa trên `3266b0f`)
- **Phạm vi:** Audit toàn bộ repository MagicDown (không chỉnh sửa code hiện tại) nhằm chuẩn bị cho lộ trình **Store-based distribution** (Chrome Web Store Unlisted & Microsoft Edge Add-ons), **Browser Integration thật**, **handshake `HELLO`/`WELCOME`**, tách biệt **dev/prod build**, và hardening bảo mật cho môi trường production.
- **Baseline kiểm thử:**
  - `go test ./...`: **PASS** toàn bộ 7 package có test (`pkg/autostart`, `pkg/engine`, `pkg/gui`, `pkg/i18n`, `pkg/ipc`, `pkg/nativemessaging`, `pkg/updater`).
  - `node --test extension/*.test.js`: **PASS** 38/38 test cases.

---

## Tổng quan cấu trúc Repository

```text
MagicDown/
├── cmd/
│   ├── magicdown/              # Desktop GUI app (mygo), hỗ trợ subcommand `host`, single-instance, system tray
│   └── magicdown-host/         # Standalone Native Messaging Host binary (stdio -> HTTP IPC)
├── extension/                  # Manifest V3 browser extension (background.js, content.js, popup, batch-picker, youtube/bilibili/thumbnail)
├── pkg/
│   ├── autostart/              # HKCU\...\Run autostart management (Windows)
│   ├── engine/                 # Multi-segment downloader, HLS/DASH/FFmpeg muxing, categories, state persistence
│   ├── gui/                    # Desktop UI, Download Info Dialog, Settings, Welcome Onboarding / Browser Integration
│   ├── i18n/                   # Localization EN/VI
│   ├── ipc/                    # Local HTTP IPC server & client (127.0.0.1:28888)
│   ├── nativemessaging/        # Stdio framing protocol, message handler, Registry registration, browser discovery
│   ├── notify/                 # Windows toast notifications
│   ├── updater/                # GitHub Releases semver updater + checksum verification
│   └── version/                # Build-time version/commit/repo ldflags
├── plans/                      # Các plan phát triển trước đó
└── scripts/                    # install.ps1, uninstall.ps1, install-host.ps1, uninstall-host.ps1, build-release.ps1, publish-release.ps1
```

---

## 1. Những thành phần đã hoạt động tốt

### 1.1. Kiến trúc Desktop App & Single-Instance / System Tray
- `cmd/magicdown/main.go` (dòng 24–183):
  - Hỗ trợ single-instance lock (`mygo.App.RequestSingleInstanceLock()`, dòng 46) và wake-up cửa sổ cũ khi mở lần 2 (`OnSecondInstance`, dòng 90–92).
  - Hỗ trợ `--minimized` / `--tray` (dòng 40–41, 175–177), giữ app chạy ngầm dưới System Tray với menu Pause All / Resume All / Quit.
  - Hỗ trợ subcommand `magicdown.exe host [--port 28888]` (dòng 27–36) làm fallback nếu chạy trực tiếp từ binary chính.
  - Khởi động local IPC server song song với GUI (`ipc.NewServer`, dòng 77–80) và gắn callback `SetOnIncomingDownload` (dòng 84) để hiển thị Download Info Dialog khi nhận link từ extension.

### 1.2. Giao thức Native Messaging Framing & Stdio Loop
- `pkg/nativemessaging/protocol.go` (dòng 1–50):
  - Triển khai chuẩn 4-byte little-endian length-prefixed JSON theo đặc tả Chrome/Edge Native Messaging (`ReadMessage`, `WriteMessage`).
  - Có giới hạn `MaxMessageSize = 8 * 1024 * 1024` (dòng 13) để hỗ trợ download plan nhiều segment mà vẫn chặn cấp phát bộ nhớ vô hạn.
- `pkg/nativemessaging/host.go` (dòng 211–238):
  - Vòng lặp `RunStdioHost` xử lý `io.EOF` sạch sẽ khi browser đóng pipe.
  - Hỗ trợ 4 action cơ bản: `ping`, `status`, `download`, `batch_download` (dòng 68–207).

### 1.3. Dynamic Browser Discovery trên Windows
- `pkg/nativemessaging/browsers.go` (dòng 79–476) và `pkg/nativemessaging/browsers_windows.go` (dòng 25–76, 161–257):
  - Quét đa nguồn: đường dẫn cài đặt chuẩn (`CandidateBrowserPaths`), Windows Registry `App Paths`, `HKCU/HKLM\SOFTWARE\Clients\StartMenuInternet`, và `WindowsApps` MSIX aliases (Dia, Arc).
  - Phân loại family (`chromium`, `firefox`, `other`), deduplicate theo executable path/ID, sắp xếp trình duyệt đã cài lên đầu.

### 1.4. Extension Manifest V3 & Bộ tính năng cốt lõi
- `extension/manifest.json` (dòng 1–53):
  - Tuân thủ Manifest V3 (`manifest_version: 3`, `background.service_worker: "background.js"`).
  - Phân tách content script `world: "MAIN"` (`inject-main.js` chạy `document_start`) và isolated world (`content.js` + modules chạy `document_idle`).
- `extension/background.js` (dòng 1–250, 1163–1346):
  - Chặn download (`shouldIntercept`), lọc theo extension, minFileSizeMB, excludedDomains.
  - Đính kèm cookies (`chrome.cookies.getAll`), referrer, sanitized headers, hỗ trợ YouTube/Bilibili/Facebook DASH pairing, thumbnail download, batch scan.
  - Có bộ unit test JS chạy nhanh bằng `node --test` (38/38 pass).

### 1.5. Quy trình Build, Install & Release cơ bản
- `scripts/build-release.ps1` (dòng 57–110): Build `magicdown.exe` với `-H=windowsgui` (không hiện cửa sổ console) và `magicdown-host.exe`, nhúng `pkg/version` qua `-ldflags`, đóng gói `MagicDown-windows-amd64.zip` và tạo `checksums.txt` (SHA-256).
- `scripts/install.ps1` & `scripts/uninstall.ps1`: Cài đặt per-user vào `%LOCALAPPDATA%\Programs\MagicDown`, tạo Start Menu/Desktop shortcuts, đăng ký `HKCU\...\Uninstall\MagicDown` (Apps & Features) và tùy chọn `HKCU\...\Run` autostart.

---

## 2. Những thành phần còn thiếu

So với mục tiêu **Store-based distribution (Chrome Web Store Unlisted & Microsoft Edge Add-ons)**, **Browser Integration thật**, **handshake `HELLO`/`WELCOME`**, và **tách biệt dev/prod build**:

1. **Chưa tách biệt cấu hình Dev vs Prod cho Extension & Native Messaging:**
   - Hiện tại chỉ có một `extension/manifest.json` duy nhất chứa cứng `"key"` (dòng 5) sinh ra 1 Extension ID cố định `cndchhhjghchjigecgkjclembafgjaje`.
   - Không có cơ chế build/packaging riêng cho `dev` (unpacked, có `key`), `chrome-store` (không có `key`, dùng Store ID), và `edge-addons` (không có `key`, dùng Edge Store ID).
   - `pkg/nativemessaging/register.go` (dòng 17) chỉ có 1 hằng `DefaultExtensionID`, không hỗ trợ danh sách nhiều `allowed_origins` (Chrome prod ID + Edge prod ID + optional dev ID).

2. **Thiếu giao thức Handshake `HELLO` / `WELCOME` & Versioning giữa Extension <-> Host <-> Desktop App:**
   - `pkg/nativemessaging/host.go` (dòng 68–207) chỉ có `ping` trả về `pong` + `app_running`.
   - Không có `HELLO` / `WELCOME` để trao đổi `protocol_version`, `host_version`, `app_version`, `extension_version`, `browser_id`, `capabilities`, và `extension_id`.
   - Không có cơ chế phát hiện version mismatch hoặc kiểm tra trạng thái "Extension thực sự đã cài và kết nối thành công" từ phía Desktop App.

3. **Browser Integration hiện tại vẫn là kiểu "unpacked dev" chứ chưa phải Store-based thật:**
   - `pkg/nativemessaging/browsers_windows.go` (dòng 135–147) mở trình duyệt kèm cờ `--load-extension=<extDir>` và `chrome://extensions` / `edge://extensions`. Trên Chrome/Edge production hiện đại, `--load-extension` bị vô hiệu hóa hoặc hiện cảnh báo Developer Mode, và không hoạt động nếu trình duyệt đã mở sẵn.
   - `pkg/nativemessaging/browsers.go` (dòng 80–202) chưa có trường `StoreURL` (Chrome Web Store Unlisted URL / Edge Add-ons URL) hay trạng thái cài đặt extension thật (`ExtensionInstalled`, `LastHandshakeAt`).
   - `scripts/install.ps1` (dòng 78–86) và `scripts/build-release.ps1` (dòng 88–91) vẫn đóng gói nguyên thư mục `extension/` (bao gồm cả `*.test.js`) vào bản phát hành desktop.

4. **Thiếu xác thực (Authentication) cho Local IPC (`127.0.0.1:28888`):**
   - `pkg/ipc/server.go` không có token/secret hay kiểm tra nguồn gọi. Bất kỳ tiến trình hoặc trang web nào (qua DNS rebinding / CSRF) đều có thể gọi trực tiếp `http://127.0.0.1:28888/*`.

5. **Thiếu lệnh/script đóng gói Extension riêng cho Store (`pack-extension`):**
   - Chưa có script tự động loại bỏ `*.test.js`, strip `"key"` cho bản upload Store, kiểm tra `manifest.json` hợp lệ, và tạo 2 gói ZIP riêng biệt cho Chrome Web Store và Edge Add-ons.

6. **Thiếu đồng bộ Version giữa Desktop App và Extension:**
   - `pkg/ipc/server.go` (dòng 173) hardcode `Version: "1.0.0"` thay vì dùng `version.Version` (`0.1.0`).
   - `extension/manifest.json` (dòng 4) hardcode `"version": "1.0.0"`.

---

## 3. Những đoạn code cần sửa

| STT | File & Dòng | Vấn đề hiện tại | Hướng sửa đề xuất (Phase 2–6) |
|---|---|---|---|
| 1 | `extension/manifest.json` (dòng 4–5, 13–24) | Hardcode `"key"` trực tiếp trong manifest nguồn (Chrome Web Store / Edge Add-ons sẽ báo lỗi hoặc yêu cầu strip `"key"` khi submit); `host_permissions: ["<all_urls>"]` và `permissions` rộng cần giải trình kỹ khi review Store. | Tách template manifest hoặc build script: giữ `"key"` cho dev build, strip `"key"` cho `chrome-store` và `edge-addons` zip; rà soát tối giản quyền cần thiết. |
| 2 | `pkg/nativemessaging/register.go` (dòng 17, 30–46) | Chỉ nhận 1 `extensionID string` và ghi duy nhất 1 phần tử vào `allowed_origins`. | Đổi sang hỗ trợ nhiều origins (`AllowedExtensionIDs []string` gồm Chrome Prod ID, Edge Prod ID, và optional Dev ID qua config/build flag); validate định dạng `[a-p]{32}` và deduplicate. |
| 3 | `pkg/nativemessaging/register_windows.go` (dòng 30–80) | `RegisterHost` và `CheckRegistration` chỉ thao tác 2 key cố định Chrome & Edge; không có hàm `UnregisterHost`. | Thống nhất dùng danh sách key từ `browsers.go` (hoặc hỗ trợ toàn bộ Chromium browsers đã cài) và bổ sung `UnregisterHost`. |
| 4 | `pkg/nativemessaging/browsers.go` (dòng 20–32, 79–203) | `BrowserInfo` có `ExternalExtKey` dùng `DefaultExtensionID` nhưng chưa dùng; chưa có `StoreURL` cho Chrome/Edge/Chromium khác. | Thêm `StoreURL` (Chrome Web Store URL cho Chrome/Brave/Vivaldi/Opera/Cốc Cốc/Dia/Arc; Edge Add-ons URL cho Edge); tách rõ `ExtensionID` theo store. |
| 5 | `pkg/nativemessaging/browsers_windows.go` (dòng 133–156) | Dùng `--load-extension=` và mở `chrome://extensions` để user load unpacked thủ công. | Chuyển sang luồng Store-based: đăng ký Native Host manifest + mở trực tiếp `StoreURL` tương ứng của trình duyệt (giữ fallback dev mode chỉ khi có cờ dev). |
| 6 | `pkg/nativemessaging/host.go` (dòng 13–45, 58–208) | Thiếu action `hello` (handshake `HELLO`/`WELCOME`), không truyền metadata version/capabilities, không validate URL scheme trước khi forward. | Thêm action `hello` trả về `welcome` (`protocol_version`, `host_version`, `app_version`, `app_running`, `capabilities`); validate URL `http://`/`https://` trước khi gọi IPC. |
| 7 | `pkg/ipc/server.go` (dòng 119–135, 168–179, 194–271, 298–311, 426–464) | Không có auth token; không kiểm tra `Origin`/CORS; `handleHealth` hardcode `Version: "1.0.0"`; `handleAddDownload` không kiểm tra URL scheme và cho phép truyền `SaveDir` tùy ý (arbitrary path write); `handlePause`/`handleResume`/`handleDelete` không kiểm tra HTTP method (chấp nhận cả `GET` -> dễ bị CSRF qua `<img>`/`<script>`). | Thêm per-user random IPC auth token (lưu trong `%LOCALAPPDATA%\MagicDown\ipc.token` với ACL user-only, gửi qua header `X-MagicDown-Token`); từ chối mọi request có header `Origin` không hợp lệ; bắt buộc `POST` cho mọi mutating endpoint (`/pause`, `/resume`, `/delete`); dùng `version.Version`; validate URL `http`/`https` và ràng buộc `SaveDir`. |
| 8 | `pkg/ipc/client.go` (dòng 20–116) | Không đọc/gửi IPC auth token; `IsAppRunning` không xác minh response là của MagicDown thật. | Đọc token từ file user-only và đính kèm `X-MagicDown-Token` trong mọi request; xác minh `App == "MagicDown"` khi healthcheck. |
| 9 | `pkg/gui/app.go` (dòng 161–167, 250–265, 1965–2043) | Onboarding chỉ hiển thị nút "Install Selected" (mở extensions page) và đường dẫn folder `extension/` để copy load unpacked; không có bước xác nhận kết nối thật từ extension. | Cập nhật UI Browser Integration: nút mở Store tương ứng, trạng thái realtime (Native Host OK + Extension Connected qua handshake `HELLO`/`WELCOME`). |
| 10 | `scripts/install.ps1` (dòng 13, 78–86, 124–141) & `scripts/install-host.ps1` (dòng 17, 49–70) | Hardcode 1 `$ExtensionId`; chỉ đăng ký 2 key Chrome & Edge; copy cả thư mục `extension/` (kèm `*.test.js`) vào `%LOCALAPPDATA%\Programs\MagicDown\extension`. | Hỗ trợ mảng `$ExtensionIds` (Chrome + Edge + optional Dev); ngừng đóng gói `extension/` dev (hoặc tách hẳn khỏi production installer); đồng bộ với `uninstall.ps1`. |
| 11 | `scripts/build-release.ps1` (dòng 88–91) | Copy toàn bộ `extension/` (gồm cả `*.test.js`) vào `MagicDown-windows-amd64.zip`. | Loại bỏ `*.test.js` (hoặc không bundle unpacked extension trong release zip khi đã chuyển sang Store); bổ sung script `scripts/build-extension.ps1` xuất 2 gói ZIP sạch cho Chrome & Edge Store. |
| 12 | `extension/background.js` (dòng 164–186, 1163–1166) | Chỉ gửi `{ action: "ping" }` khi popup mở; chưa gửi handshake `HELLO` khi `onInstalled`/`onStartup` hoặc khi kết nối lại. | Triển khai handshake `HELLO` -> nhận `WELCOME`, lưu trạng thái kết nối + version mismatch warning để hiển thị trên popup. |

---

## 4. Những vấn đề liên quan đến Chrome và Edge

### 4.1. Extension ID độc lập giữa Chrome Web Store và Microsoft Edge Add-ons
- Khi publish lên **Chrome Web Store** (kể cả chế độ **Unlisted**), Google tự sinh khóa ký riêng và cấp một **Chrome Extension ID** cố định (32 ký tự `[a-p]`).
- Khi publish lên **Microsoft Edge Add-ons**, Microsoft cũng sinh khóa ký riêng và cấp một **Edge Extension ID** hoàn toàn khác với Chrome ID (trừ khi dùng quy trình đặc biệt, nhưng thực tế production cần coi 2 ID là độc lập).
- **Hệ quả với code hiện tại:** `pkg/nativemessaging/register.go:17` (`DefaultExtensionID = "cndchhhjghchjigecgkjclembafgjaje"`), `scripts/install.ps1:13`, và `scripts/install-host.ps1:17` chỉ ghi **1 origin duy nhất** vào `allowed_origins`. Nếu người dùng cài từ Edge Add-ons hoặc Chrome Web Store, trình duyệt sẽ từ chối kết nối Native Messaging với lỗi `Access to the specified native messaging host is forbidden.`
- **Giải pháp:** Manifest `com.magicdown.downloader.json` phải liệt kê đồng thời:
  - `chrome-extension://<CHROME_STORE_EXT_ID>/`
  - `chrome-extension://<EDGE_STORE_EXT_ID>/`
  - (Chỉ trong dev build / dev mode): `chrome-extension://<DEV_EXT_ID>/`

### 4.2. Định dạng `allowed_origins` trên Edge
- Microsoft Edge vẫn sử dụng scheme `chrome-extension://<EDGE_EXT_ID>/` trong trường `allowed_origins` của Native Messaging manifest (KHÔNG phải `extension://`). Tuy nhiên, Registry key của Edge là riêng biệt:
  - Chrome: `HKCU\Software\Google\Chrome\NativeMessagingHosts\com.magicdown.downloader`
  - Edge: `HKCU\Software\Microsoft\Edge\NativeMessagingHosts\com.magicdown.downloader`
- Các trình duyệt Chromium khác (Brave, Vivaldi, Opera, Cốc Cốc, Dia, Arc) khi cài extension từ Chrome Web Store sẽ dùng **Chrome Extension ID** và đọc key Registry của chính trình duyệt đó hoặc fallback về key Chrome (`browsers.go:80-185` đã xử lý tốt phần Registry key fallback này).

### 4.3. Khác biệt về Store URL & Quy trình Review (Chrome Unlisted vs Edge Add-ons)
- **Chrome Web Store (Unlisted):**
  - URL dạng `https://chromewebstore.google.com/detail/<slug>/<CHROME_EXT_ID>`.
  - Không hiển thị khi tìm kiếm công khai, chỉ người có link (mở từ Desktop App MagicDown) mới cài được, nhưng **vẫn trải qua quy trình review chính sách đầy đủ** của Google.
  - Phải **xóa trường `"key"`** trong `manifest.json` trước khi nén ZIP upload.
  - Phải giải trình rõ ràng từng quyền nhạy cảm: `downloads`, `nativeMessaging`, `cookies`, `webRequest`, `<all_urls>`, `world: "MAIN"`.
- **Microsoft Edge Add-ons:**
  - URL dạng `https://microsoftedge.microsoft.com/addons/detail/<slug>/<EDGE_EXT_ID>`.
  - Cũng yêu cầu xóa trường `"key"` khi upload ZIP và giải trình quyền tương tự (đặc biệt `cookies` + `<all_urls>` + `nativeMessaging`).
  - Edge không hỗ trợ cài trực tiếp từ Chrome Web Store một cách mượt mà mặc định nếu user chưa bật "Allow extensions from other stores", nên mở đúng Edge Add-ons URL là bắt buộc cho trải nghiệm production.

### 4.4. Chính sách `--load-extension` & External Extension Registry
- `pkg/nativemessaging/browsers_windows.go:138` đang truyền `--load-extension=<extDir>` cho Chromium browsers. Trên Chrome bản ổn định (Stable), Google đã siết chặt/loại bỏ hỗ trợ `--load-extension` trên bản release thông thường và chỉ cho phép trên Chrome for Testing hoặc Developer Mode thủ công.
- `pkg/nativemessaging/browsers.go:87,96` có khai báo `ExternalExtKey` (`Software\Google\Chrome\Extensions\<ID>`), nhưng hiện chưa dùng. Trong production, cài đặt qua **Store URL** (user chủ động bấm "Add to Chrome / Get") là phương pháp sạch nhất, không bị Chrome/Edge chặn bởi chính sách chống sideloading.

---

## 5. Những rủi ro bảo mật

| Mức độ | Thành phần & Vị trí | Mô tả rủi ro | Khuyến nghị khắc phục |
|---|---|---|---|
| **Cao (Critical)** | Local IPC Server không xác thực (`pkg/ipc/server.go:119-135, 194-464`) | Server lắng nghe `127.0.0.1:28888` không yêu cầu token/secret. Bất kỳ tiến trình local nào (hoặc trang web độc hại qua CSRF / DNS rebinding) đều có thể gọi `/downloads/add`, `/downloads/batch`, `/config`, `/downloads/delete?delete_files=true`. | Tạo random 32-byte token mỗi phiên hoặc lưu per-user trong `%LOCALAPPDATA%\MagicDown\ipc.token` (ACL chỉ current user); bắt buộc header `X-MagicDown-Token` cho mọi endpoint (trừ hoặc kể cả `/health`). |
| **Cao (Critical)** | CSRF trên các endpoint thay đổi trạng thái (`pkg/ipc/server.go:398-424, 451-464`) | `handlePause`, `handleResume`, `handleDelete` **không kiểm tra `r.Method`**, chấp nhận cả `GET`! Một trang web bất kỳ có thể nhúng `<img src="http://127.0.0.1:28888/downloads/delete?id=...&delete_files=true">` hoặc pause/resume toàn bộ download. | Bắt buộc `r.Method == http.MethodPost` (hoặc `DELETE`), yêu cầu `Content-Type: application/json` + `X-MagicDown-Token`, và từ chối mọi request có header `Origin` không rỗng. |
| **Cao (High)** | Arbitrary Directory Write qua `SaveDir` & `/config` (`pkg/ipc/server.go:239, 298-311, 431-445`) | `resolveSaveDir` trả về nguyên `explicitDir` nếu client gửi `save_dir` (chỉ sanitize `subfolder`). Tương tự `POST /config` cho phép thay đổi `DownloadDir` tùy ý. Nếu kết hợp với lỗi không auth, kẻ tấn công có thể tải file thực thi vào thư mục Startup của Windows. | Với request từ Native Host, không cho phép override `SaveDir` ra ngoài `DownloadDir` (hoặc chỉ cho phép `Subfolder` đã sanitize); khóa `POST /config` bằng auth token và validate đường dẫn. |
| **Trung bình (Medium)** | Thiếu validate URL scheme ở IPC & Native Host (`pkg/ipc/server.go:206`, `pkg/nativemessaging/host.go:99`) | Chỉ kiểm tra `URL == ""`, không kiểm tra scheme `http://` hoặc `https://` ở tầng Host/IPC (chỉ kiểm tra ở extension). | Validate chặt `url.Parse` và chỉ chấp nhận scheme `http`/`https` ở cả `pkg/nativemessaging/host.go` và `pkg/ipc/server.go`. |
| **Trung bình (Medium)** | Truyền `Cookies` & `Headers` qua HTTP plaintext localhost (`pkg/ipc/client.go:59`, `extension/background.js:242`) | Extension đọc toàn bộ cookies của URL (kể cả `HttpOnly`) và gửi qua Native Messaging -> HTTP `127.0.0.1:28888`. Cần đảm bảo không bao giờ log `Cookies`, `Authorization`, `Headers` ra stdout/stderr hoặc file state (`downloads.json`). | Audit `pkg/engine` và `pkg/ipc` để chắc chắn cookies/headers nhạy cảm không bị ghi vào log hoặc lộ qua `GET /downloads` (nếu có tiến trình khác đọc). |
| **Trung bình (Medium)** | `allowed_origins` chứa Dev ID trên môi trường Production (`pkg/nativemessaging/register.go:44`) | Nếu giữ nguyên Dev Extension ID (từ public key trong repo công khai `extension/manifest.json:5`) trong bản production, bất kỳ ai lấy public key đó đều có thể dựng extension giả mạo cùng ID trên máy nạn nhân nếu cài unpacked. | Tách biệt hoàn toàn Prod `allowed_origins` (chỉ chứa Chrome Store ID & Edge Store ID) và Dev `allowed_origins` (chỉ bật khi build/install ở chế độ dev). |
| **Thấp (Low)** | `uninstall.ps1` & `install.ps1` chạy PowerShell với `-ExecutionPolicy Bypass` (`scripts/install.ps1:153`) | `UninstallString` gọi `powershell.exe -ExecutionPolicy Bypass -File "$InstalledUninstall"` trong `%LOCALAPPDATA%\Programs\MagicDown`. Vì thư mục per-user có thể bị ghi đè bởi tiến trình cùng user, đây là rủi ro chuẩn của per-user installer nhưng cần đảm bảo ký số (code signing) khi phân phối rộng rãi. | Cân nhắc đóng gói bằng Inno Setup / MSIX có ký số (Code Signing Certificate) ở giai đoạn release chính thức. |

---

## 6. Những thay đổi cần thiết để đưa dự án lên môi trường Production (Lộ trình Phase 2 → Phase 6)

### Phase 2 — Giao thức Handshake `HELLO`/`WELCOME` & Hardening IPC Security
- **IPC Auth & CSRF Protection (`pkg/ipc/server.go`, `pkg/ipc/client.go`):**
  - Sinh random auth token khi Desktop App khởi động, ghi vào `%LOCALAPPDATA%\MagicDown\ipc.token` với quyền chỉ user hiện tại đọc được; xóa khi tắt app.
  - Middleware kiểm tra `X-MagicDown-Token`, từ chối request có header `Origin`, bắt buộc `POST` cho `/downloads/pause`, `/downloads/resume`, `/downloads/delete`.
  - Thay hardcode `Version: "1.0.0"` bằng `version.Version`.
  - Validate URL scheme (`http`/`https`) và chặn `SaveDir` traversal.
- **Native Messaging Handshake (`pkg/nativemessaging/host.go`, `extension/background.js`):**
  - Thêm message `action: "hello"` gửi `extension_version`, `browser`, `extension_id`.
  - Host phản hồi `WELCOME` gồm `protocol_version`, `host_version`, `app_version`, `app_running`, `capabilities`.
  - Desktop App ghi nhận trạng thái handshake gần nhất (qua endpoint IPC mới, ví dụ `POST /integration/heartbeat` hoặc `POST /integration/hello`) để GUI biết chắc chắn extension đã kết nối thật.

### Phase 3 — Multi-Origin Native Messaging & Tách biệt Dev/Prod Config
- **Native Messaging Registration (`pkg/nativemessaging/register.go`, `register_windows.go`, `browsers.go`):**
  - Hỗ trợ danh sách `AllowedOrigins` (Chrome Store ID + Edge Store ID cho Prod; thêm Dev ID khi bật dev mode).
  - Thêm `StoreURL` vào `BrowserInfo` cho Chrome, Edge, và các trình duyệt Chromium phổ biến.
  - Thêm `UnregisterHost` để dọn dẹp sạch Registry cho mọi trình duyệt đã đăng ký.
- **Extension Build Separation (`scripts/build-extension.ps1`, `extension/manifest.json`):**
  - Tách cấu hình build:
    - `dev`: giữ `"key"` để load unpacked cố định ID.
    - `chrome-store`: strip `"key"`, loại bỏ `*.test.js`, xuất `dist/magicdown-extension-chrome-v<ver>.zip`.
    - `edge-addons`: strip `"key"`, loại bỏ `*.test.js`, xuất `dist/magicdown-extension-edge-v<ver>.zip`.
  - Đồng bộ `version` giữa Desktop App và Extension.

### Phase 4 — Browser Integration thật trên Desktop GUI
- **Loại bỏ luồng `--load-extension` ở production (`pkg/nativemessaging/browsers_windows.go`, `pkg/gui/app.go`):**
  - Khi người dùng chọn trình duyệt và bấm tích hợp:
    1. Ghi manifest `com.magicdown.downloader.json` với prod `allowed_origins` và đăng ký Registry HKCU cho trình duyệt đã chọn.
    2. Mở trực tiếp trang Store tương ứng (`Chrome Web Store Unlisted` cho Chrome/Chromium, `Edge Add-ons` cho Edge).
    3. Hiển thị trạng thái 2 bước rõ ràng trên UI: **Native Host Registered** → **Waiting for Extension Handshake** → **Connected (vX.Y.Z)**.

### Phase 5 — Đóng gói Production & Installer/Uninstaller hoàn chỉnh
- Cập nhật `scripts/install.ps1`, `scripts/uninstall.ps1`, `scripts/build-release.ps1`:
  - Ngừng bundle thư mục `extension/` (và `*.test.js`) vào production installer/zip (trừ khi truyền cờ `-Dev`).
  - Đăng ký đầy đủ `allowed_origins` (Chrome + Edge) và dọn sạch toàn bộ Registry keys đã đăng ký khi gỡ cài đặt.
  - Chuẩn bị tài liệu privacy policy & permission justification cho Chrome Web Store và Microsoft Edge Add-ons.

### Phase 6 — Kiểm thử End-to-End (E2E) & Phát hành Store
- Kiểm thử E2E trên cả **Google Chrome** và **Microsoft Edge**:
  - Cài Desktop App -> Đăng ký Native Host -> Cài Extension từ Store (hoặc gói build store test) -> Xác minh handshake `HELLO`/`WELCOME` -> Chặn download, tải video, batch download, tắt/bật app, kiểm tra token auth IPC.
- Kiểm thử bảo mật: xác minh request không có `X-MagicDown-Token` hoặc `GET /downloads/delete` bị trả về `401`/`405`.
- Publish Chrome Web Store (Unlisted) & Microsoft Edge Add-ons, cập nhật 2 Production Extension ID chính thức vào cấu hình build release và phát hành GitHub Release.

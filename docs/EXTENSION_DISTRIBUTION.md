# MagicDown Browser Extension Distribution Guide

> **Trạng thái hiện tại:** Extension **chưa được submit hay phê duyệt** trên Chrome Web Store hoặc Microsoft Edge Add-ons. Các trường `prod` Extension ID trong `config/extension-ids.json` được giữ trống có chủ đích cho đến khi chủ sở hữu hoàn tất bước submit thủ công.

---

## 1. Phân biệt Development Build vs Production Build

| Tiêu chí | Development Build (`-Mode dev`) | Production Build (`-Mode prod`) |
|---|---|---|
| Nguồn extension | Load unpacked từ thư mục `extension/` | Cài đặt từ Chrome Web Store (Unlisted) & Microsoft Edge Add-ons |
| Trường `"key"` trong `manifest.json` | **Giữ nguyên** để cố định Dev Extension ID (`cndchhhjghchjigecgkjclembafgjaje`) | **Tự động loại bỏ (`strip_key_in_prod: true`)** khi đóng gói ZIP vì Store tự ký |
| Extension ID | 1 Dev ID chung cho Chrome & Edge | **2 Production ID độc lập** (`chrome.prod` và `edge.prod`) |
| File trong gói | Chứa cả `*.test.js` trong repo dev | Chỉ 19 file runtime sạch trong `MagicDown-extension-chrome.zip` & `MagicDown-extension-edge.zip` |

---

## 2. Quản lý Extension ID tập trung (`config/extension-ids.json`)

File `config/extension-ids.json` là nguồn cấu hình duy nhất cho cả Go (`pkg/nativemessaging/extension_config.go`) và PowerShell scripts:

- `browsers.chrome.dev` / `browsers.chrome.prod` / `browsers.chrome.store_url`
- `browsers.edge.dev` / `browsers.edge.prod` / `browsers.edge.store_url`
- Hỗ trợ override linh hoạt qua biến môi trường khi build CI/local:
  - `MAGICDOWN_BUILD_MODE` (`dev` hoặc `prod`)
  - `MAGICDOWN_CHROME_PROD_EXT_ID`, `MAGICDOWN_CHROME_STORE_URL`
  - `MAGICDOWN_EDGE_PROD_EXT_ID`, `MAGICDOWN_EDGE_STORE_URL`
- Hoặc nhúng trực tiếp vào binary qua `-ldflags` trong `scripts/build-release.ps1`.

---

## 3. Đóng gói Extension cho Store

Chạy lệnh sau từ thư mục gốc repository:

```powershell
.\scripts\package-extension.ps1
```

Đầu ra trong `dist/`:
- `MagicDown-extension-chrome.zip` (dành cho Chrome Web Store)
- `MagicDown-extension-edge.zip` (dành cho Microsoft Edge Add-ons)

Script tự động kiểm tra:
- `manifest_version == 3`, không có `"key"`, không dùng `<all_urls>` (`host_permissions` chỉ gồm `http://*/*` và `https://*/*`).
- Không chứa `*.test.js`, `README`, `.DS_Store`, `.map`, `.log`.
- `manifest.version` khớp chính xác với `pkg/version/version.go`.

---

## 4. Quy trình Submit lên Chrome Web Store (Chế độ Unlisted)

1. Đăng nhập **Chrome Web Store Developer Dashboard** (`https://chrome.google.com/webstore/devconsole`).
2. Chọn **New Item** → Upload `dist\MagicDown-extension-chrome.zip`.
3. Điền thông tin mô tả, icon, screenshot, Privacy Policy và giải trình từng quyền (`downloads`, `nativeMessaging`, `storage`, `contextMenus`, `cookies`, `activeTab`, `webRequest`, `http://*/*`, `https://*/*`).
4. Ở mục **Visibility**, chọn **Unlisted** (chỉ người dùng mở link từ MagicDown Desktop App mới cài được; không hiển thị công khai khi tìm kiếm, nhưng vẫn qua review chính sách chuẩn của Google).
5. Sau khi tạo item, sao chép **Chrome Extension ID** (32 ký tự `[a-p]`) và Store URL (`https://chromewebstore.google.com/detail/.../<CHROME_ID>`), cập nhật vào `config/extension-ids.json` (`browsers.chrome.prod` và `browsers.chrome.store_url`).

---

## 5. Quy trình Submit lên Microsoft Edge Add-ons

1. Đăng nhập **Microsoft Partner Center — Edge Add-ons** (`https://partner.microsoft.com/dashboard/microsoftedge`).
2. Chọn **Create new extension** → Upload `dist\MagicDown-extension-edge.zip`.
3. Điền thông tin mô tả, icon, Privacy Policy và giải trình quyền (tương tự Chrome).
4. Submit để Microsoft kiểm duyệt.
5. Sau khi có **Edge Extension ID** (32 ký tự `[a-p]`) và Edge Add-ons URL (`https://microsoftedge.microsoft.com/addons/detail/.../<EDGE_ID>`), cập nhật vào `config/extension-ids.json` (`browsers.edge.prod` và `browsers.edge.store_url`).

---

## 6. Cập nhật Extension lên phiên bản mới

1. Tăng phiên bản đồng bộ trong `pkg/version/version.go` và `extension/manifest.json`.
2. Chạy `.\scripts\package-extension.ps1` để tạo 2 gói ZIP mới.
3. Upload gói mới lên Chrome Web Store Developer Dashboard và Microsoft Edge Partner Center.
4. Trình duyệt của người dùng sẽ tự động cập nhật extension; khi khởi động lại, extension gửi `HELLO` với `extensionVersion` mới để MagicDown Desktop App cập nhật trạng thái tức thì.

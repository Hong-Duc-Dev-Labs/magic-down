# MagicDown Native Messaging & IPC Architecture

Tài liệu mô tả cơ chế giao tiếp giữa **Browser Extension**, **Native Messaging Host (`magicdown-host.exe`)** và **MagicDown Desktop App (`127.0.0.1:28888`)**.

---

## 1. Kiến trúc tổng thể

```text
Browser Extension (MV3)
   │  chrome.runtime.sendNativeMessage("com.magicdown.downloader", payload)
   ▼  (4-byte little-endian length-prefixed JSON over stdio)
magicdown-host.exe (pkg/nativemessaging)
   │  HTTP 127.0.0.1:28888 + Header: X-MagicDown-Token
   ▼
MagicDown Desktop App (pkg/ipc) ──► pkg/engine + pkg/gui (Browser Integration)
```

---

## 2. Đăng ký Native Messaging Host (Windows HKCU)

MagicDown tách manifest độc lập theo kênh Store để Chrome và Edge sử dụng đúng Extension ID riêng biệt:

- **Chrome & Chromium browsers (Brave, Vivaldi, Opera, Cốc Cốc, Dia, Arc, Thorium):**
  - Manifest: `%LOCALAPPDATA%\MagicDown\com.magicdown.downloader.chrome.json`
  - Registry: `HKCU\Software\Google\Chrome\NativeMessagingHosts\com.magicdown.downloader` (và key riêng của từng trình duyệt Chromium)
- **Microsoft Edge:**
  - Manifest: `%LOCALAPPDATA%\MagicDown\com.magicdown.downloader.edge.json`
  - Registry: `HKCU\Software\Microsoft\Edge\NativeMessagingHosts\com.magicdown.downloader`

Trước khi ghi Registry, `ValidateManifest` bắt buộc kiểm tra:
1. `name == "com.magicdown.downloader"` và `type == "stdio"`.
2. `path` là đường dẫn tuyệt đối tới `magicdown-host.exe` đang tồn tại.
3. `allowed_origins` không rỗng và mọi phần tử đúng định dạng `chrome-extension://<32-char-[a-p]-id>/`.

Khi gỡ cài đặt (`UninstallNativeHost` / `scripts/uninstall.ps1`), MagicDown **chỉ xóa** các Registry key `NativeMessagingHosts\com.magicdown.downloader` và 3 file manifest do MagicDown quản lý, tuyệt đối không ảnh hưởng dữ liệu download của người dùng.

---

## 3. Giao thức Handshake `HELLO` / `WELCOME`

- Khi khởi động, cài đặt mới hoặc theo chu kỳ heartbeat (30s, có exponential backoff khi lỗi), Extension gửi:
  ```json
  {
    "type": "HELLO",
    "action": "hello",
    "protocolVersion": 1,
    "extensionVersion": "0.1.0",
    "browser": "chrome"
  }
  ```
- `magicdown-host.exe` chuyển tiếp tới `POST /handshake/hello` trên Desktop IPC và trả về:
  ```json
  {
    "type": "WELCOME",
    "protocolVersion": 1,
    "hostVersion": "0.1.0",
    "status": "connected",
    "success": true,
    "app_running": true
  }
  ```
- Desktop App lưu trạng thái kết nối thực theo từng trình duyệt kèm TTL hết hạn (90s). Khi trình duyệt đóng pipe (`io.EOF`), `magicdown-host.exe` gọi `POST /handshake/disconnect` để cập nhật trạng thái ngắt kết nối tức thì.

---

## 4. Bảo mật Localhost IPC & Logging

- **Per-session Auth Token:** Khi khởi động, `ipc.Server` sinh token ngẫu nhiên 64 ký tự hex, lưu vào `%LOCALAPPDATA%\MagicDown\ipc.token` với quyền `0600` (chỉ user hiện tại đọc được), xóa khi tắt app. Mọi request thiếu/sai `X-MagicDown-Token` bị từ chối `401 Unauthorized`.
- **Chống CSRF & Cross-Origin:** Từ chối mọi request có header `Origin` (`403 Forbidden`), không bật `Access-Control-Allow-Origin: *`, bắt buộc `POST` cho `/downloads/pause`, `/downloads/resume`, `/downloads/delete`.
- **Input Validation:** Chỉ cho phép URL `http://` và `https://`, chặn path traversal (`..`, `/`, `\`) ở `filename` và `save_dir`, giới hạn request body 10 MB.
- **Structured Logging (`%LOCALAPPDATA%\MagicDown\logs\native-host.log`):** Ghi JSON-lines cho `host_install`, `manifest_validate`, `registry_register`, `registry_unregister`, `host_startup`, `host_shutdown`, `comm_error`; tự xoay vòng ở 2 MB và tự động redact mọi trường nhạy cảm (`cookies`, `token`, `authorization`, `headers`, `secret`, `password`).

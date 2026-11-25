# MagicDown Browser Extension Installation Guide (End-User)

Hướng dẫn cài đặt, kết nối, cập nhật và xử lý lỗi tiện ích MagicDown trên Google Chrome, Microsoft Edge và các trình duyệt Chromium (Brave, Vivaldi, Opera, Cốc Cốc, Dia, Arc).

---

## 1. Cài đặt cho người dùng cuối (Production — Store-based)

Người dùng **không cần** bật Developer Mode hay thao tác Load unpacked:

1. **Cài đặt MagicDown Desktop App** (qua `install.ps1` hoặc bản phát hành GitHub Releases) và mở ứng dụng.
2. Mở **Settings → Browser Integration** (hoặc màn hình **Welcome Setup** khi chạy lần đầu).
3. MagicDown tự động phát hiện trình duyệt đã cài, phiên bản trình duyệt, trạng thái Native Messaging Host và trạng thái kết nối thực của Extension.
4. Bấm **Install Extension** ở trình duyệt mong muốn:
   - MagicDown tự động đăng ký & xác thực Native Messaging Host (HKCU Registry + manifest).
   - Trình duyệt mở thẳng trang **Chrome Web Store** (cho Chrome & Chromium) hoặc **Microsoft Edge Add-ons** (cho Edge).
   - Trạng thái trên MagicDown chuyển sang **Waiting for Connection**.
5. Bấm **Add to Chrome** / **Get** trên trang Store để cài đặt tiện ích.
6. Ngay khi tiện ích được cài, extension tự động gửi gói tin `HELLO` qua Native Messaging và nhận `WELCOME` từ MagicDown → trạng thái tự động chuyển sang **Connected**.

---

## 2. Trạng thái kết nối trong Browser Integration

| Trạng thái | Ý nghĩa |
|---|---|
| **Not Installed** | Trình duyệt chưa được cài trên máy |
| **Unsupported Browser** | Trình duyệt không thuộc họ Chromium được hỗ trợ |
| **Host Not Registered** | Chưa đăng ký hoặc manifest Native Messaging Host chưa hợp lệ |
| **Installation Required** | Native Host đã hợp lệ, cần cài Extension từ Store |
| **Waiting for Connection** | Đã mở Store hoặc đang chờ handshake `HELLO`/`WELCOME` thực |
| **Connected** | Extension đã kết nối và bắt tay thành công với Desktop App |
| **Connection Error** | Có lỗi khi đăng ký Host hoặc giao tiếp Native Messaging |

---

## 3. Cập nhật Extension

- Extension cài từ Chrome Web Store / Edge Add-ons được trình duyệt tự động cập nhật ngầm.
- Người dùng có thể cập nhật thủ công bằng cách vào `chrome://extensions` hoặc `edge://extensions` và bấm **Update**.

---

## 4. Xử lý lỗi cài đặt & kết nối (Troubleshooting)

1. Bấm nút **Troubleshoot** ngay cạnh trình duyệt trong **Settings → Browser Integration** để xem:
   - Đường dẫn tệp thực thi trình duyệt (`Executable`).
   - Đường dẫn file Native Messaging Manifest (`Manifest`).
   - Đường dẫn nhật ký lỗi (`%LOCALAPPDATA%\MagicDown\logs\native-host.log`).
2. Các lỗi thường gặp:
   - **Native Host Not Registered:** Bấm **Reconnect** để MagicDown tự động ghi lại manifest và cập nhật HKCU Registry.
   - **Waiting for Connection kéo dài:** Kiểm tra xem MagicDown Desktop App đang chạy chưa, tiện ích đã được bật (Enabled) trong trình duyệt chưa, sau đó khởi động lại trình duyệt.
   - **Store URL chưa cấu hình:** Xuất hiện trong môi trường dev trước khi extension được publish lên Store; lập trình viên có thể load unpacked thư mục `extension/` ở chế độ dev.

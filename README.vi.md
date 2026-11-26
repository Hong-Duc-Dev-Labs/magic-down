# MagicDown

[English](README.md) | **Tiếng Việt**

Trình quản lý tải xuống tốc độ cao dành cho Windows lấy cảm hứng từ IDM, được xây dựng hoàn toàn bằng Go thuần với **MyGo Native UI** (render bằng GPU, không dùng webview) kết hợp **Tiện ích trình duyệt Chromium Manifest V3** kết nối qua Native Messaging kèm xác minh bắt tay `HELLO/WELCOME` theo thời gian thực.

<p align="center">
  <img src="screenshots/01-main-dark.png" alt="Ứng dụng MagicDown Desktop — Chế độ tối" width="72%" />
  <img src="screenshots/ext-popup-dark.png" alt="Tiện ích trình duyệt MagicDown" width="26%" />
</p>

---

## Tính năng nổi bật

- **Công cụ tải xuống đa luồng (Multi-Segment Download Engine)**
  - Tự động thăm dò HTTP/HTTPS Range (`Accept-Ranges`, `Content-Length`, `Content-Disposition`), chuyển về một luồng khi cần, và **chia luồng động (Dynamic Work-Stealing Segmentation)** tự động chia nhỏ phân đoạn chậm để duy trì tốc độ tối đa tới 100%.
  - Tùy chỉnh số luồng tải đồng thời (mặc định `8`), theo dõi tốc độ/thời gian còn lại (ETA)/tiến độ theo thời gian thực, hoàn tất tệp an toàn (atomic) với phần mở rộng tạm `.magicpart`, lưu trạng thái atomic chống lỗi ghi đè, và giới hạn băng thông tổng (`speed_limit_kbps`).
  - Hỗ trợ **Tạm dừng, Tiếp tục, Hủy, Xóa**, **Làm mới liên kết tải xuống (Refresh Download Address)** để khôi phục link hết hạn `403`/`410` mà không mất tiến độ đã tải, **Phát hiện tải xuống trùng lặp (Duplicate Detection)** với 3 lựa chọn rõ ràng (Mở/Xem mục có sẵn, Tải bản sao, Ghi đè), hàng đợi bền vững và tự động thử lại theo cơ chế lùi thời gian lũy tiến (exponential backoff).
- **Tải video MP4 & âm thanh MP3 từ YouTube**
  - Nút nổi tự động phát hiện video kèm bộ chọn chất lượng cho **MP4** (ưu tiên H.264/avc1, tự động ghép hình và tiếng bằng FFmpeg) và trích xuất âm thanh **MP3** ở các mức **320 / 192 / 128 kbps**.
- **Hỗ trợ Video Mạng xã hội & Streaming**
  - Tích hợp sẵn khả năng phát hiện và lập kế hoạch tải cho **X / Twitter**, **Facebook**, **Weibo**, **Bilibili**, **Douyin**, các luồng HLS (`.m3u8` bao gồm cả `#EXT-X-MAP`) và trình phát DASH/MSE.
- **Tải ảnh thu nhỏ Video (Video Thumbnail Downloader)**
  - Trích xuất ảnh thu nhỏ độ phân giải cao chỉ với một cú nhấp chuột trên YouTube, Bilibili, Weibo, Facebook, Douyin và các trang HTML5 `<video>` tiêu chuẩn.
- **Tải hàng loạt (Batch Download All)**
  - Quét toàn bộ trang web để tìm hình ảnh, video, âm thanh, tài liệu và tệp nén; lọc theo danh mục, kích thước hoặc từ khóa tìm kiếm, sau đó gửi tất cả mục đã chọn sang MagicDown trong một lần duy nhất.
- **Hộp thoại thông tin tải xuống & Tự động phân loại**
  - Xem trước tên tệp, dung lượng ước tính, thư mục lưu và danh mục trước khi bắt đầu tải, kèm tính năng tự động sắp xếp vào các thư mục con (`Video`, `Music`, `Compressed`, `Documents`, `Programs`, `Other`).
- **Khay hệ thống (System Tray), Tự khởi động & Thiết lập ban đầu**
  - Chế độ Khay hệ thống tối ưu hóa GPU/CPU (tạm dừng render giao diện khi ẩn xuống khay và đánh thức ngay lập tức khi có lượt tải mới), tùy chọn **Khởi động cùng Windows** và **Hướng dẫn thiết lập lần đầu (Welcome Setup)**.
- **Giao diện song ngữ (Tiếng Việt & English)**
  - Chuyển đổi tức thì giữa Tiếng Việt và Tiếng Anh trên cả Ứng dụng Desktop lẫn Tiện ích trình duyệt.
- **Tự động cập nhật qua GitHub (GitHub Auto-Updater)**
  - Tích hợp trình kiểm tra bản phát hành mới và tự động cập nhật có xác minh mã băm SHA-256 từ GitHub Releases.
- **Native Messaging bảo mật & Tích hợp trình duyệt**
  - Giao tiếp IPC cục bộ (localhost) có auth token riêng theo từng người dùng, xác minh bắt tay `HELLO/WELCOME`, tự động kết nối lại và bảng chẩn đoán **Tích hợp trình duyệt (Browser Integration)** 7 trạng thái theo thời gian thực.

---

## Cài đặt Ứng dụng Desktop (Windows)

### Cách 1: Từ GitHub Releases (Khuyên dùng)

1. Tải tệp `MagicDown-windows-amd64.zip` từ bản [GitHub Release](../../releases) mới nhất.
2. Giải nén vào một thư mục tạm bất kỳ.
3. Chạy kịch bản cài đặt trong PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1 -LaunchAfterInstall
```

Lệnh này sẽ cài đặt MagicDown vào `%LOCALAPPDATA%\Programs\MagicDown` (bao gồm cả thư mục tiện ích `extension/` đi kèm), tạo lối tắt tại Start Menu & Desktop, đăng ký Native Messaging Host cho Chrome, Edge và các trình duyệt nhân Chromium được phát hiện, đồng thời thêm mục gỡ cài đặt vào **Apps & Features** của Windows.

### Cách 2: Biên dịch & Cài đặt từ mã nguồn

Yêu cầu cài đặt sẵn **Go 1.22+** trên Windows:

```powershell
.\scripts\install.ps1 -LaunchAfterInstall
```

Các cờ tùy chọn:
- `-EnableAutostart` — Tự động khởi chạy MagicDown ở chế độ thu nhỏ vào Khay hệ thống cùng Windows.
- `-NoDesktopShortcut` — Bỏ qua bước tạo lối tắt ngoài Desktop.
- `-SkipBuild` — Cài đặt trực tiếp từ các tệp thực thi đã biên dịch sẵn trong `dist/`.

Để gỡ cài đặt sau này, bạn có thể dùng **Apps & Features** của Windows hoặc chạy lệnh:

```powershell
& "$env:LOCALAPPDATA\Programs\MagicDown\uninstall.ps1"
```

---

## Cài đặt Tiện ích trình duyệt (Load unpacked)

MagicDown đóng gói sẵn tiện ích Manifest V3 tại đường dẫn:

```text
%LOCALAPPDATA%\Programs\MagicDown\extension
```

Các trình duyệt nhân Chromium được hỗ trợ: **Google Chrome**, **Microsoft Edge**, **Dia**, **Brave**, **Vivaldi**, **Opera / Opera GX**, và **Cốc Cốc**.

> **Extension ID cố định (Deterministic Extension ID):** `extension/manifest.json` đã gắn sẵn khóa công khai `"key"` cố định, vì vậy khi tự nạp dưới dạng Load unpacked trên bất kỳ máy tính hay trình duyệt Chromium nào, tiện ích luôn tạo ra chính xác cùng một Extension ID (`cndchhhjghchjigecgkjclembafgjaje`) khớp với danh sách `allowed_origins` đã đăng ký của Native Messaging Host.

### Hướng dẫn từng bước

#### Cách nhanh nhất (Trực tiếp từ Cài đặt MagicDown)
1. Mở **MagicDown → Cài đặt (Settings) → Tích hợp trình duyệt (Browser Integration)**.
2. Nhấn **Cài đặt tiện ích (Install Extension)** ở dòng tương ứng với trình duyệt của bạn.
   - MagicDown sẽ tự động đăng ký Native Messaging Host, sao chép đường dẫn `%LOCALAPPDATA%\Programs\MagicDown\extension` vào clipboard và mở sẵn trang Quản lý tiện ích của trình duyệt.
3. Làm theo 3 bước trên trình duyệt:
   1. Bật **Chế độ dành cho nhà phát triển (Developer mode)** (công tắc ở góc trên bên phải).
   2. Nhấn **Tải tiện ích đã giải nén (Load unpacked)**.
   3. Dán đường dẫn vừa sao chép (`%LOCALAPPDATA%\Programs\MagicDown\extension`) và chọn thư mục đó.

#### Cách thủ công
1. Mở trang Quản lý tiện ích của trình duyệt:
   - Chrome / Dia: `chrome://extensions`
   - Edge: `edge://extensions`
   - Brave: `brave://extensions`
   - Vivaldi: `vivaldi://extensions`
   - Opera: `opera://extensions`
   - Cốc Cốc: `coccoc://extensions`
2. Bật **Developer mode** (Chế độ dành cho nhà phát triển).
3. Nhấn **Load unpacked** (Tải tiện ích đã giải nén) và chọn thư mục `%LOCALAPPDATA%\Programs\MagicDown\extension`.

### Kiểm tra trạng thái kết nối

Ngay sau khi nạp xong, tiện ích sẽ lập tức kết nối tới `magicdown-host.exe` và thực hiện bắt tay `HELLO/WELCOME` với ứng dụng MagicDown:
- Trong **MagicDown → Settings → Browser Integration**, trạng thái trình duyệt của bạn sẽ tự động chuyển từ **Waiting for Connection** sang **Connected**.
- Khi nhấp vào biểu tượng **MagicDown** trên thanh công cụ trình duyệt, trạng thái cũng sẽ hiển thị **Connected**.

---

## Quyền riêng tư & Bảo mật cục bộ

MagicDown hoạt động **100% cục bộ** trên máy của bạn, không thu thập dữ liệu phân tích/telemetry và không đi qua máy chủ trung gian:

- **IPC cục bộ có xác thực:** Tiện ích trình duyệt chỉ giao tiếp với `magicdown-host.exe` (qua Chrome Native Messaging) và ứng dụng MagicDown tại `127.0.0.1` bằng auth token ngẫu nhiên theo từng người dùng (`pkg/ipc/auth.go`).
- **Lý do yêu cầu các quyền của tiện ích:**
  - `http://*/*` & `https://*/*` + `webRequest`: Phát hiện luồng video/HLS/DASH có thể tải trên trang đang mở.
  - `cookies`: Chuyển tiếp cookie phiên cục bộ sang ứng dụng MagicDown trên máy bạn để tải được tệp yêu cầu đăng nhập.
  - `downloads` & `contextMenus`: Bắt sự kiện tải tệp và thêm thao tác chuột phải cho ảnh/video.
  - `nativeMessaging`: Kết nối với `magicdown-host.exe` trên máy tính của bạn.

---

## Phát triển & Kiểm thử

```powershell
# Chạy toàn bộ bộ kiểm thử Go
go test ./...

# Chạy toàn bộ kiểm thử Tiện ích trình duyệt (Node.js 18+)
node --test extension/*.test.js

# Biên dịch tệp thực thi GUI Windows (không mở cửa sổ console) và Native Messaging Host
go build -ldflags="-s -w -H=windowsgui" -o dist/magicdown.exe ./cmd/magicdown
go build -ldflags="-s -w" -o dist/magicdown-host.exe ./cmd/magicdown-host
```

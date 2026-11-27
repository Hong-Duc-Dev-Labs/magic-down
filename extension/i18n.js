(function (root) {
  const CATALOGS = {
    en: {
      // Popup header & connection
      "popup.status_tooltip": "Click to re-check Desktop App connection",
      "popup.status_checking": "Checking...",
      "popup.status_connected": "Connected",
      "popup.status_offline": "App Offline",
      "popup.status_host_missing": "Host Missing",
      "popup.footer_connected": "IPC :28888 Connected",
      "popup.footer_offline": "Desktop App Offline",
      "popup.footer_host_missing": "Host Not Registered",
      "popup.theme_toggle": "Toggle Dark / Light Mode",
      "popup.lang_toggle": "Switch Language (VI / EN)",

      // Popup controls
      "popup.intercept_title": "Auto-Intercept Downloads",
      "popup.intercept_sub": "Intercept browser downloads (hold Alt to bypass)",
      "popup.exclude_title": "Exclude Current Site",
      "popup.exclude_none": "No active web page",
      "popup.exclude_unavailable": "Not available on this page",
      "popup.quick_placeholder": "Paste direct URL to download...",
      "popup.download_btn": "Download",
      "popup.scan_page_btn": "Scan & Download All on Page",

      // Popup detected videos & tabs
      "popup.detected_videos": "VIDEOS ON THIS PAGE",
      "popup.quality_original": "Original",
      "popup.tab_activity": "Activity",
      "popup.tab_filters": "Filters",
      "popup.recent_handoffs": "RECENT HANDOFFS",
      "popup.clear": "Clear",
      "popup.empty_title": "No recent downloads",
      "popup.empty_sub": "Files intercepted or sent to MagicDown will appear here.",
      "popup.sent": "Sent",
      "popup.failed": "Failed",
      "popup.just_now": "just now",
      "popup.mins_ago": "{n}m ago",
      "popup.hrs_ago": "{n}h ago",
      "popup.days_ago": "{n}d ago",

      // Popup filters
      "popup.file_extensions": "FILE EXTENSIONS",
      "popup.ext_hint": "Use * for all files",
      "popup.ext_placeholder": "Add extension (e.g. dmg, tar)",
      "popup.add": "Add",
      "popup.all_intercepted": "All files intercepted",
      "popup.min_size_title": "Minimum File Size",
      "popup.min_size_sub": "Skip files smaller than threshold (0 = all)",
      "popup.excluded_domains": "EXCLUDED DOMAINS",
      "popup.never_intercept": "Never intercept",
      "popup.domain_placeholder": "Add domain (e.g. intranet.local)",
      "popup.no_excluded_domains": "No excluded domains",
      "popup.remove_aria": "Remove {val}",

      // Popup toasts
      "popup.toast_added": "Added {val}",
      "popup.toast_excluded": "Excluded {val}",
      "popup.toast_host_missing": "Run scripts/install-host.ps1 to register Native Host.",
      "popup.toast_preview_video": "Preview mode: video queued.",
      "popup.toast_video_sent": "Video sent to MagicDown.",
      "popup.toast_app_offline": "Desktop app offline.",
      "popup.toast_intercept_on": "Auto-interception enabled.",
      "popup.toast_intercept_off": "Auto-interception paused.",
      "popup.toast_site_excluded": "Excluded {host} from interception.",
      "popup.toast_site_enabled": "Re-enabled interception on {host}.",
      "popup.toast_min_size_set": "Minimum size set to {n} MB.",
      "popup.toast_min_size_off": "Minimum size filter disabled.",
      "popup.toast_cleared": "Cleared recent activity.",
      "popup.toast_invalid_url": "Enter a valid http:// or https:// URL.",
      "popup.toast_preview_queued": "Preview mode: download queued.",
      "popup.toast_sent": "Sent to MagicDown desktop app.",
      "popup.toast_send_failed": "Failed to send download.",
      "popup.toast_preview_batch": "Preview mode: opened batch picker.",
      "popup.toast_no_active_page": "No active web page to scan.",
      "popup.toast_cannot_scan": "Cannot scan this page (try reloading the tab).",
      "popup.toast_scan_failed": "Failed to scan page.",

      // Context menus
      "menu.download": "Download with MagicDown",
      "menu.download_all": "Download all with MagicDown",
      "menu.image_root": "MagicDown",
      "menu.image_original": "Download Image (Original)",
      "menu.image_png": "Save Image as PNG",
      "menu.image_jpg": "Save Image as JPG",
      "menu.image_webp": "Save Image as WebP",

      // Video overlay dropdown
      "overlay.dismiss_title": "Dismiss for this video",
      "overlay.header_title": "SELECT QUALITY & FORMAT",
      "overlay.header_sub": "1-click to MagicDown",
      "overlay.sub_youtube": "YouTube · {n} streams",
      "overlay.sub_youtube_one": "YouTube · 1 stream",
      "overlay.sub_bilibili": "Bilibili · {n} streams",
      "overlay.sub_bilibili_one": "Bilibili · 1 stream",
      "overlay.sub_x": "X · {n} streams",
      "overlay.sub_x_one": "X · 1 stream",
      "overlay.sub_dash": "DASH · {n} qualities",
      "overlay.sub_dash_one": "DASH · 1 quality",
      "overlay.sub_hls": "HLS · quality list",
      "overlay.sub_drm": "DRM protected",
      "overlay.sub_unresolved": "Unresolved",
      "overlay.sub_resolving": "Resolving via yt-dlp...",
      "overlay.yt_resolving": "Resolving YouTube qualities via MagicDown (yt-dlp)...",
      "overlay.yt_need_ytdlp": "YouTube SABR stream requires yt-dlp. Install yt-dlp (winget install yt-dlp.yt-dlp) or place yt-dlp.exe next to MagicDown.",
      "overlay.yt_app_offline": "Open MagicDown desktop app to resolve and download YouTube SABR streams.",
      "overlay.yt_resolve_failed": "Could not resolve YouTube streams ({err}).",
      "overlay.video_audio": "Video+Audio",
      "overlay.video_only": "Video only",
      "overlay.audio_only": "Audio only",
      "overlay.audio_mp3": "Audio · MP3",
      "overlay.thumb_badge": "Thumb",
      "overlay.download": "Download",
      "overlay.sent": "Sent!",
      "overlay.offline": "Offline",

      // Batch Picker
      "batch.title": "MagicDown — Download All on Page",
      "batch.loading": "Loading resource list...",
      "batch.subfolder_title": "Subfolder inside MagicDown's download directory",
      "batch.subfolder_label": "Subfolder:",
      "batch.subfolder_placeholder": "Folder name (optional)",
      "batch.rescan": "Rescan",
      "batch.tab_image": "Images",
      "batch.tab_media": "Video/Audio",
      "batch.tab_file": "Documents/Files",
      "batch.tab_all": "All",
      "batch.min_size_title": "Filter minimum image dimensions to skip small icons",
      "batch.min_size_0": "All image sizes",
      "batch.min_size_50": "Images ≥ 50px",
      "batch.min_size_100": "Images ≥ 100px (skip icons)",
      "batch.min_size_200": "Images ≥ 200px",
      "batch.min_size_400": "Images ≥ 400px",
      "batch.search_placeholder": "Search by name or extension (e.g. jpg, pdf, zip)...",
      "batch.select_all": "Select All",
      "batch.deselect_all": "Deselect",
      "batch.empty_title": "No matching resources found",
      "batch.empty_sub": "Try switching filter tabs, lowering minimum image size, or clearing the search query.",
      "batch.summary_showing": "Showing",
      "batch.summary_items": "items",
      "batch.summary_selected": "Selected",
      "batch.download_all": "Download All ({n})",
      "batch.download_selected": "Download Selected ({n})",
      "batch.status_sending": "Sending {n} items to MagicDown...",
      "batch.status_added_partial": "Added {added} items to MagicDown queue ({failed} failed).",
      "batch.status_added_ok": "Successfully added {added} items to MagicDown queue!",
      "batch.status_connect_err": "Cannot connect to MagicDown Desktop app.",
      "batch.status_send_err": "Error sending download list: {err}",
      "batch.unknown_source": "Unknown page source",
      "batch.no_scan_data": "Scan data not found. Try scanning again from the web page.",
      "batch.rescanning": "Rescanning page...",
      "batch.rescan_err": "Cannot rescan source tab (it may have been closed)."
    },
    vi: {
      // Popup header & connection
      "popup.status_tooltip": "Nhấn để kiểm tra lại kết nối tới MagicDown Desktop",
      "popup.status_checking": "Đang kiểm tra...",
      "popup.status_connected": "Đã kết nối",
      "popup.status_offline": "App ngoại tuyến",
      "popup.status_host_missing": "Thiếu Host",
      "popup.footer_connected": "IPC :28888 Đã kết nối",
      "popup.footer_offline": "MagicDown Desktop ngoại tuyến",
      "popup.footer_host_missing": "Chưa đăng ký Native Host",
      "popup.theme_toggle": "Chuyển giao diện Sáng / Tối",
      "popup.lang_toggle": "Đổi ngôn ngữ (VI / EN)",

      // Popup controls
      "popup.intercept_title": "Tự động bắt liên kết tải",
      "popup.intercept_sub": "Tự động bắt tải trình duyệt (giữ Alt để bỏ qua)",
      "popup.exclude_title": "Bỏ qua trang hiện tại",
      "popup.exclude_none": "Không có trang web đang mở",
      "popup.exclude_unavailable": "Không khả dụng trên trang này",
      "popup.quick_placeholder": "Dán đường dẫn trực tiếp để tải...",
      "popup.download_btn": "Tải xuống",
      "popup.scan_page_btn": "Quét & tải toàn bộ trang",

      // Popup detected videos & tabs
      "popup.detected_videos": "VIDEO TRÊN TRANG NÀY",
      "popup.quality_original": "Gốc",
      "popup.tab_activity": "Hoạt động",
      "popup.tab_filters": "Bộ lọc",
      "popup.recent_handoffs": "TẢI GẦN ĐÂY",
      "popup.clear": "Xóa",
      "popup.empty_title": "Chưa có lượt tải gần đây",
      "popup.empty_sub": "Các tệp được bắt hoặc gửi sang MagicDown sẽ hiển thị ở đây.",
      "popup.sent": "Đã gửi",
      "popup.failed": "Lỗi",
      "popup.just_now": "vừa xong",
      "popup.mins_ago": "{n} phút trước",
      "popup.hrs_ago": "{n} giờ trước",
      "popup.days_ago": "{n} ngày trước",

      // Popup filters
      "popup.file_extensions": "ĐUÔI TỆP",
      "popup.ext_hint": "Dùng * cho mọi tệp",
      "popup.ext_placeholder": "Thêm đuôi tệp (vd: dmg, tar)",
      "popup.add": "Thêm",
      "popup.all_intercepted": "Bắt tất cả các tệp",
      "popup.min_size_title": "Dung lượng tệp tối thiểu",
      "popup.min_size_sub": "Bỏ qua tệp nhỏ hơn mức này (0 = tất cả)",
      "popup.excluded_domains": "TÊN MIỀN LOẠI TRỪ",
      "popup.never_intercept": "Không bao giờ bắt",
      "popup.domain_placeholder": "Thêm tên miền (vd: intranet.local)",
      "popup.no_excluded_domains": "Chưa có tên miền loại trừ",
      "popup.remove_aria": "Xóa {val}",

      // Popup toasts
      "popup.toast_added": "Đã thêm {val}",
      "popup.toast_excluded": "Đã loại trừ {val}",
      "popup.toast_host_missing": "Hãy chạy scripts/install-host.ps1 để đăng ký Native Host.",
      "popup.toast_preview_video": "Chế độ xem trước: đã xếp hàng video.",
      "popup.toast_video_sent": "Đã gửi video sang MagicDown.",
      "popup.toast_app_offline": "Ứng dụng MagicDown Desktop đang ngoại tuyến.",
      "popup.toast_intercept_on": "Đã bật tự động bắt tải xuống.",
      "popup.toast_intercept_off": "Đã tạm dừng tự động bắt tải xuống.",
      "popup.toast_site_excluded": "Đã loại trừ {host} khỏi tự động bắt tải.",
      "popup.toast_site_enabled": "Đã bật lại tự động bắt tải trên {host}.",
      "popup.toast_min_size_set": "Đã đặt dung lượng tối thiểu {n} MB.",
      "popup.toast_min_size_off": "Đã tắt bộ lọc dung lượng tối thiểu.",
      "popup.toast_cleared": "Đã xóa lịch sử hoạt động.",
      "popup.toast_invalid_url": "Vui lòng nhập đường dẫn http:// hoặc https:// hợp lệ.",
      "popup.toast_preview_queued": "Chế độ xem trước: đã xếp hàng tải.",
      "popup.toast_sent": "Đã gửi sang ứng dụng MagicDown Desktop.",
      "popup.toast_send_failed": "Gửi liên kết tải thất bại.",
      "popup.toast_preview_batch": "Chế độ xem trước: đã mở bộ chọn tải hàng loạt.",
      "popup.toast_no_active_page": "Không có trang web đang mở để quét.",
      "popup.toast_cannot_scan": "Không thể quét trang này (hãy thử tải lại tab).",
      "popup.toast_scan_failed": "Quét trang thất bại.",

      // Context menus
      "menu.download": "Tải bằng MagicDown",
      "menu.download_all": "Tải toàn bộ trang bằng MagicDown",
      "menu.image_root": "MagicDown",
      "menu.image_original": "Tải ảnh gốc",
      "menu.image_png": "Lưu ảnh dạng PNG",
      "menu.image_jpg": "Lưu ảnh dạng JPG",
      "menu.image_webp": "Lưu ảnh dạng WebP",

      // Video overlay dropdown
      "overlay.dismiss_title": "Ẩn cho video này",
      "overlay.header_title": "CHỌN CHẤT LƯỢNG & ĐỊNH DẠNG",
      "overlay.header_sub": "1 chạm sang MagicDown",
      "overlay.sub_youtube": "YouTube · {n} luồng",
      "overlay.sub_youtube_one": "YouTube · 1 luồng",
      "overlay.sub_bilibili": "Bilibili · {n} luồng",
      "overlay.sub_bilibili_one": "Bilibili · 1 luồng",
      "overlay.sub_x": "X · {n} luồng",
      "overlay.sub_x_one": "X · 1 luồng",
      "overlay.sub_dash": "DASH · {n} chất lượng",
      "overlay.sub_dash_one": "DASH · 1 chất lượng",
      "overlay.sub_hls": "HLS · danh sách chất lượng",
      "overlay.sub_drm": "Có bảo vệ DRM",
      "overlay.sub_unresolved": "Chưa nhận diện được",
      "overlay.sub_resolving": "Đang phân tích qua yt-dlp...",
      "overlay.yt_resolving": "Đang lấy danh sách chất lượng YouTube qua MagicDown (yt-dlp)...",
      "overlay.yt_need_ytdlp": "Luồng YouTube SABR cần yt-dlp. Hãy cài yt-dlp (winget install yt-dlp.yt-dlp) hoặc đặt yt-dlp.exe cạnh MagicDown.",
      "overlay.yt_app_offline": "Hãy mở ứng dụng MagicDown Desktop để phân tích và tải video YouTube SABR.",
      "overlay.yt_resolve_failed": "Không thể phân tích luồng YouTube ({err}).",
      "overlay.video_audio": "Video+Âm thanh",
      "overlay.video_only": "Chỉ video",
      "overlay.audio_only": "Chỉ âm thanh",
      "overlay.audio_mp3": "Âm thanh · MP3",
      "overlay.thumb_badge": "Ảnh bìa",
      "overlay.download": "Tải xuống",
      "overlay.sent": "Đã gửi!",
      "overlay.offline": "Ngoại tuyến",

      // Batch Picker
      "batch.title": "MagicDown — Tải toàn bộ trang",
      "batch.loading": "Đang tải danh sách tài nguyên...",
      "batch.subfolder_title": "Thư mục con trong thư mục tải xuống của MagicDown",
      "batch.subfolder_label": "Thư mục con:",
      "batch.subfolder_placeholder": "Tên thư mục (tuỳ chọn)",
      "batch.rescan": "Quét lại",
      "batch.tab_image": "Ảnh",
      "batch.tab_media": "Video/Audio",
      "batch.tab_file": "Tài liệu/File",
      "batch.tab_all": "Tất cả",
      "batch.min_size_title": "Lọc kích thước ảnh tối thiểu để bỏ icon nhỏ",
      "batch.min_size_0": "Mọi kích thước ảnh",
      "batch.min_size_50": "Ảnh ≥ 50px",
      "batch.min_size_100": "Ảnh ≥ 100px (lọc icon)",
      "batch.min_size_200": "Ảnh ≥ 200px",
      "batch.min_size_400": "Ảnh ≥ 400px",
      "batch.search_placeholder": "Tìm theo tên, đuôi file (vd: jpg, pdf, zip)...",
      "batch.select_all": "Chọn tất cả",
      "batch.deselect_all": "Bỏ chọn",
      "batch.empty_title": "Không tìm thấy tài nguyên phù hợp",
      "batch.empty_sub": "Thử đổi tab bộ lọc, giảm kích thước ảnh tối thiểu hoặc xoá từ khoá tìm kiếm.",
      "batch.summary_showing": "Đang hiển thị",
      "batch.summary_items": "mục",
      "batch.summary_selected": "Đã chọn",
      "batch.download_all": "Tải toàn bộ ({n})",
      "batch.download_selected": "Tải các mục đã chọn ({n})",
      "batch.status_sending": "Đang gửi {n} mục sang MagicDown...",
      "batch.status_added_partial": "Đã thêm {added} mục vào hàng đợi MagicDown ({failed} mục lỗi).",
      "batch.status_added_ok": "Đã thêm {added} mục vào hàng đợi MagicDown thành công!",
      "batch.status_connect_err": "Không thể kết nối tới ứng dụng MagicDown Desktop.",
      "batch.status_send_err": "Lỗi gửi danh sách tải: {err}",
      "batch.unknown_source": "Không rõ nguồn trang",
      "batch.no_scan_data": "Không tìm thấy dữ liệu quét. Hãy thử quét lại từ trang web.",
      "batch.rescanning": "Đang quét lại trang...",
      "batch.rescan_err": "Không thể quét lại tab gốc (có thể tab đã đóng)."
    }
  };

  function normalizeLang(raw) {
    const s = String(raw || "").trim().toLowerCase();
    if (s === "vi" || s.startsWith("vi-") || s.startsWith("vi_")) {
      return "vi";
    }
    return "en";
  }

  function detectBrowserLang() {
    try {
      if (typeof chrome !== "undefined" && chrome.i18n && typeof chrome.i18n.getUILanguage === "function") {
        return normalizeLang(chrome.i18n.getUILanguage());
      }
    } catch {
      // ignore
    }
    try {
      if (typeof navigator !== "undefined" && navigator.language) {
        return normalizeLang(navigator.language);
      }
    } catch {
      // ignore
    }
    return "en";
  }

  let currentLang = detectBrowserLang();

  function setLang(lang) {
    currentLang = normalizeLang(lang);
    return currentLang;
  }

  function getLang() {
    return currentLang;
  }

  function resolveLang(storedLang) {
    if (storedLang && String(storedLang).trim()) {
      return normalizeLang(storedLang);
    }
    return detectBrowserLang();
  }

  function t(key, params, langOverride) {
    const lang = langOverride ? normalizeLang(langOverride) : currentLang;
    const dict = CATALOGS[lang] || CATALOGS.en;
    let template = Object.prototype.hasOwnProperty.call(dict, key)
      ? dict[key]
      : (Object.prototype.hasOwnProperty.call(CATALOGS.en, key) ? CATALOGS.en[key] : key);
    if (params && typeof params === "object") {
      for (const [k, v] of Object.entries(params)) {
        template = template.replace(new RegExp(`\\{${k}\\}`, "g"), String(v));
      }
    }
    return template;
  }

  const api = {
    CATALOGS,
    normalizeLang,
    detectBrowserLang,
    resolveLang,
    setLang,
    getLang,
    t
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = api;
  }
  if (root) {
    root.MagicDownI18n = api;
  }
})(typeof globalThis !== "undefined" ? globalThis : (typeof self !== "undefined" ? self : window));

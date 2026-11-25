package i18n

import (
	"fmt"
	"strings"
	"sync"
)

// Lang represents a supported UI language code.
type Lang string

const (
	LangEN Lang = "en"
	LangVI Lang = "vi"
)

var (
	mu          sync.RWMutex
	currentLang = DetectSystemLang()
)

// Normalize converts a raw locale or language string (e.g. "vi-VN", "VI", "en_US")
// into a supported Lang, defaulting to LangEN.
func Normalize(raw string) Lang {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "vi" || strings.HasPrefix(s, "vi-") || strings.HasPrefix(s, "vi_") {
		return LangVI
	}
	return LangEN
}

// ParseOrDetect returns Normalize(raw) when raw is non-empty, or DetectSystemLang() when empty.
func ParseOrDetect(raw string) Lang {
	if strings.TrimSpace(raw) == "" {
		return DetectSystemLang()
	}
	return Normalize(raw)
}

// SetLang updates the active application language immediately.
func SetLang(lang Lang) {
	norm := Normalize(string(lang))
	mu.Lock()
	currentLang = norm
	mu.Unlock()
}

// CurrentLang returns the currently active language.
func CurrentLang() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return currentLang
}

// T returns the localized string for key in the active language, falling back to English
// and then to the key itself if missing.
func T(key string) string {
	return TFor(CurrentLang(), key)
}

// Tf formats a localized string template with fmt.Sprintf arguments.
func Tf(key string, args ...any) string {
	return fmt.Sprintf(T(key), args...)
}

// TFor returns the localized string for key in the specified language.
func TFor(lang Lang, key string) string {
	if dict, ok := catalogs[Normalize(string(lang))]; ok {
		if val, exists := dict[key]; exists {
			return val
		}
	}
	if val, exists := catalogs[LangEN][key]; exists {
		return val
	}
	return key
}

// Catalog returns a copy of the translation map for a language (useful for tests).
func Catalog(lang Lang) map[string]string {
	src := catalogs[Normalize(string(lang))]
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

var catalogs = map[Lang]map[string]string{
	LangEN: {
		// App / Title bar
		"app.subtitle": "Multi-Segment Download Manager",

		// Language names
		"lang.label":   "Language",
		"lang.en":      "English",
		"lang.vi":      "Tiếng Việt",

		// Sidebar sections
		"sidebar.downloads":     "DOWNLOADS",
		"sidebar.categories":    "CATEGORIES",
		"sidebar.all_downloads": "All Downloads",
		"sidebar.downloading":   "Downloading",
		"sidebar.completed":     "Completed",
		"sidebar.queued_paused": "Queued & Paused",
		"sidebar.failed":        "Failed",
		"sidebar.light_mode":    "Light Mode",
		"sidebar.dark_mode":     "Dark Mode",
		"sidebar.settings":      "Settings",

		// Categories
		"cat.All":        "All",
		"cat.Video":      "Video",
		"cat.Music":      "Music",
		"cat.Compressed": "Compressed",
		"cat.Documents":  "Documents",
		"cat.Programs":   "Programs",
		"cat.Other":      "Other",

		// Task statuses
		"status.Queued":      "Queued",
		"status.Probing":     "Probing",
		"status.Downloading": "Downloading",
		"status.Muxing":      "Muxing",
		"status.Paused":      "Paused",
		"status.Completed":   "Completed",
		"status.Failed":      "Failed",
		"status.Cancelled":   "Cancelled",

		// Header / Toolbar
		"toolbar.search_placeholder": "Search downloads...",
		"toolbar.resume":             "Resume",
		"toolbar.pause":              "Pause",
		"toolbar.delete":             "Delete",
		"toolbar.clear_done":         "Clear Done",
		"toolbar.new_download":       "New Download",

		// Status messages
		"msg.resumed":          "Resumed %s",
		"msg.paused":           "Paused %s",
		"msg.removed":          "Removed %s",
		"msg.retrying":         "Retrying %s",
		"msg.opened_folder":    "Opened folder for %s",
		"msg.cleared_done":     "Cleared %d completed download(s)",
		"msg.added_download":   "Added download: %s",
		"msg.settings_saved":   "Settings saved",
		"msg.setup_complete":   "Setup complete",
		"msg.native_registered": "Chrome/Edge Native Messaging registered",

		// Empty states
		"empty.no_downloads_title": "No downloads yet",
		"empty.no_downloads_sub":   "Add a URL or click any file in Chrome/Edge to accelerate downloads with multi-segment streams.",
		"empty.no_match_title":     "No matching downloads",
		"empty.no_match_sub":       "Try clearing your search query or switching to another status or category.",
		"empty.add_new":            "Add New Download",
		"empty.reset_filters":      "Reset Filters",

		// Download card
		"card.single_stream": "1 stream",
		"card.multi_segs":    "%d segs",
		"card.retry":         "retry %d",
		"format.unknown":     "Unknown",

		// Status bar
		"statusbar.ipc_ready":   "Extension IPC :%d Ready",
		"statusbar.ipc_offline": "IPC Offline",
		"statusbar.summary":     "Active: %d   •   Queued: %d   •   Speed: %s",

		// Add / Download Info Dialog
		"add.title_new":           "New Download",
		"add.sub_new":             "Enter a direct HTTP/HTTPS link to accelerate",
		"add.title_info":          "Download File Info",
		"add.sub_info":            "Review destination, category, and filename before starting",
		"add.estimated_unknown":   "Estimated Size: Unknown",
		"add.estimated_size":      "Estimated Size: %s",
		"add.auto_mux_badge":      "Video + Audio (Auto-Mux MP4)",
		"add.url_label":           "Download URL",
		"add.url_placeholder":     "https://example.com/archive.zip",
		"add.paste":               "Paste",
		"add.auto_filename":       "Auto-detected filename: %s",
		"add.filename_label":      "Filename",
		"add.filename_placeholder": "Leave blank to auto-detect from server or URL",
		"add.category_label":      "Category",
		"add.savedir_label":       "Save Directory",
		"add.segments_label":      "Concurrent Segments",
		"add.connections":         "%d connections",
		"add.cancel":              "Cancel",
		"add.download_later":      "Download Later",
		"add.start_download":      "Start Download",
		"add.err_invalid_url":     "Please enter a valid http:// or https:// URL.",

		// Settings Modal
		"settings.title":                 "Settings",
		"settings.subtitle":              "Configure downloads, concurrency, and browser integration",
		"settings.download_dir":          "Default Download Directory",
		"settings.max_concurrent":        "Max Concurrent Downloads",
		"settings.default_segments":      "Default Segments per File",
		"settings.max_retries":           "Max Retries per Download",
		"settings.max_retries_hint":      "0 = no retry",
		"settings.speed_limit":           "Speed Limit (KB/s)",
		"settings.speed_limit_hint":      "0 = unlimited",
		"settings.auto_categorize":       "Auto-Categorize Subfolders",
		"settings.auto_categorize_on":    "Enabled (Video, Music, ...)",
		"settings.enabled":               "Enabled",
		"settings.disabled":              "Disabled",
		"settings.launch_startup":        "Launch at Windows Startup",
		"settings.appearance":            "Appearance",
		"settings.dark":                  "Dark",
		"settings.light":                 "Light",
		"settings.close_behavior":        "Close Button (X) Behavior",
		"settings.minimize_behavior":     "Minimize Button Behavior",
		"settings.hide_to_tray":          "Hide to Tray",
		"settings.exit_app":              "Exit App",
		"settings.taskbar":               "Taskbar",
		"settings.native_title":          "Chrome / Edge Native Messaging",
		"settings.native_registered":     "Registered",
		"settings.native_not_registered": "Not registered",
		"settings.native_register":       "Register",
		"settings.native_reregister":     "Re-register",
		"settings.native_err":            "Native Messaging error: %s",
		"settings.native_ok":             "Registered Native Messaging: %s",
		"settings.welcome_setup":         "Welcome Setup",
		"settings.cancel":                "Cancel",
		"settings.save":                  "Save Changes",
		"settings.err_empty_dir":         "Download directory cannot be empty.",
		"settings.err_retries":           "Max retries must be a number of 0 or greater (0 = no retry).",
		"settings.err_speed_limit":       "Speed limit must be 0 (unlimited) or a positive number of KB/s.",
		"settings.err_startup":           "Failed to update Windows startup: %s",

		// Software Updates
		"update.title":            "Software Updates  •  %s",
		"update.auto_check_sub":   "Auto-check on startup",
		"update.auto":             "Auto",
		"update.manual":           "Manual",
		"update.check_btn":        "Check for Updates",
		"update.checking_btn":     "Checking...",
		"update.checking_msg":     "Checking for updates...",
		"update.check_failed":     "Update check failed: %s",
		"update.available":        "New version %s available",
		"update.up_to_date":       "You're up to date (%s)",
		"update.downloading_msg":  "Downloading update...",
		"update.failed":           "Update failed: %s",
		"update.applying":         "Applying update & restarting MagicDown...",
		"update.restart_hint":     "App will restart automatically to apply.",
		"update.download_install": "Download & Install Update",
		"update.progress":         "Downloading... %s",
		"update.progress_total":   "Downloading... %s / %s (%.0f%%)",

		// Welcome / Onboarding Modal
		"onboarding.title":                "Welcome to MagicDown",
		"onboarding.subtitle":             "Quick setup for downloads, startup, system tray, and browser integration",
		"onboarding.download_folder":      "Default Download Folder",
		"onboarding.auto_categorize":      "Auto-Categorize Subfolders (Video, Music, Compressed, ...)",
		"onboarding.enabled_recommended":  "Enabled (Recommended)",
		"onboarding.launch_windows":       "Launch with Windows",
		"onboarding.run_in_tray":          "Run in System Tray",
		"onboarding.browser_title":        "Browser Extension Integration",
		"onboarding.browser_sub":          "Select installed Chromium browsers to register Native Messaging & open Extensions",
		"onboarding.install_selected":     "Install Selected",
		"onboarding.browser_ready":        "Ready to integrate",
		"onboarding.browser_registered":   "Native Host registered",
		"onboarding.no_browser":           "No supported Chromium browser detected automatically. You can still load the extension folder manually.",
		"onboarding.extension_folder":     "Extension folder: %s",
		"onboarding.copy_path":            "Copy Path",
		"onboarding.copied_path":          "Copied extension folder path to clipboard",
		"onboarding.select_one_browser":   "Select at least one installed browser to integrate.",
		"onboarding.integrated_count":     "Integrated %d/%d browser(s).",
		"onboarding.skip":                 "Skip for now",
		"onboarding.finish":               "Finish Setup",
		"onboarding.err_empty_dir":        "Default download folder cannot be empty.",

		// System Tray
		"tray.open":       "Open MagicDown",
		"tray.pause_all":  "Pause All",
		"tray.resume_all": "Resume All",
		"tray.quit":       "Quit",
		"tray.tooltip":    "MagicDown — Multi-Segment Download Manager",
	},
	LangVI: {
		// App / Title bar
		"app.subtitle": "Trình tăng tốc tải xuống đa luồng",

		// Language names
		"lang.label": "Ngôn ngữ",
		"lang.en":    "English",
		"lang.vi":    "Tiếng Việt",

		// Sidebar sections
		"sidebar.downloads":     "TẢI XUỐNG",
		"sidebar.categories":    "DANH MỤC",
		"sidebar.all_downloads": "Tất cả tải xuống",
		"sidebar.downloading":   "Đang tải",
		"sidebar.completed":     "Hoàn tất",
		"sidebar.queued_paused": "Chờ & Tạm dừng",
		"sidebar.failed":        "Thất bại",
		"sidebar.light_mode":    "Giao diện sáng",
		"sidebar.dark_mode":     "Giao diện tối",
		"sidebar.settings":      "Cài đặt",

		// Categories
		"cat.All":        "Tất cả",
		"cat.Video":      "Video",
		"cat.Music":      "Âm nhạc",
		"cat.Compressed": "Tệp nén",
		"cat.Documents":  "Tài liệu",
		"cat.Programs":   "Phần mềm",
		"cat.Other":      "Khác",

		// Task statuses
		"status.Queued":      "Đang chờ",
		"status.Probing":     "Đang kiểm tra",
		"status.Downloading": "Đang tải",
		"status.Muxing":      "Đang ghép luồng",
		"status.Paused":      "Tạm dừng",
		"status.Completed":   "Hoàn tất",
		"status.Failed":      "Thất bại",
		"status.Cancelled":   "Đã hủy",

		// Header / Toolbar
		"toolbar.search_placeholder": "Tìm kiếm tải xuống...",
		"toolbar.resume":             "Tiếp tục",
		"toolbar.pause":              "Tạm dừng",
		"toolbar.delete":             "Xóa",
		"toolbar.clear_done":         "Xóa đã xong",
		"toolbar.new_download":       "Thêm liên kết",

		// Status messages
		"msg.resumed":           "Đã tiếp tục %s",
		"msg.paused":            "Đã tạm dừng %s",
		"msg.removed":           "Đã xóa %s",
		"msg.retrying":          "Đang thử lại %s",
		"msg.opened_folder":     "Đã mở thư mục cho %s",
		"msg.cleared_done":      "Đã dọn %d mục hoàn tất",
		"msg.added_download":    "Đã thêm tải xuống: %s",
		"msg.settings_saved":    "Đã lưu cài đặt",
		"msg.setup_complete":    "Thiết lập hoàn tất",
		"msg.native_registered": "Đã đăng ký Native Messaging cho Chrome/Edge",

		// Empty states
		"empty.no_downloads_title": "Chưa có lượt tải nào",
		"empty.no_downloads_sub":   "Thêm liên kết hoặc nhấp vào tệp trên Chrome/Edge để tăng tốc tải xuống đa luồng.",
		"empty.no_match_title":     "Không tìm thấy mục phù hợp",
		"empty.no_match_sub":       "Thử xóa từ khóa tìm kiếm hoặc chuyển sang trạng thái hay danh mục khác.",
		"empty.add_new":            "Thêm tải xuống mới",
		"empty.reset_filters":      "Đặt lại bộ lọc",

		// Download card
		"card.single_stream": "1 luồng",
		"card.multi_segs":    "%d luồng",
		"card.retry":         "thử lại %d",
		"format.unknown":     "Không rõ",

		// Status bar
		"statusbar.ipc_ready":   "Extension IPC :%d Sẵn sàng",
		"statusbar.ipc_offline": "IPC Ngoại tuyến",
		"statusbar.summary":     "Đang tải: %d   •   Đang chờ: %d   •   Tốc độ: %s",

		// Add / Download Info Dialog
		"add.title_new":            "Thêm tải xuống mới",
		"add.sub_new":              "Nhập liên kết HTTP/HTTPS trực tiếp để tăng tốc tải",
		"add.title_info":           "Thông tin tệp tải xuống",
		"add.sub_info":             "Kiểm tra thư mục lưu, danh mục và tên tệp trước khi tải",
		"add.estimated_unknown":    "Dung lượng ước tính: Không rõ",
		"add.estimated_size":       "Dung lượng ước tính: %s",
		"add.auto_mux_badge":       "Video + Âm thanh (Tự ghép MP4)",
		"add.url_label":            "Đường dẫn tải xuống (URL)",
		"add.url_placeholder":      "https://example.com/archive.zip",
		"add.paste":                "Dán",
		"add.auto_filename":        "Tên tệp tự nhận diện: %s",
		"add.filename_label":       "Tên tệp",
		"add.filename_placeholder": "Để trống để tự nhận diện từ máy chủ hoặc URL",
		"add.category_label":       "Danh mục",
		"add.savedir_label":        "Thư mục lưu",
		"add.segments_label":       "Số luồng tải song song",
		"add.connections":          "%d kết nối",
		"add.cancel":               "Hủy",
		"add.download_later":       "Tải sau",
		"add.start_download":       "Bắt đầu tải",
		"add.err_invalid_url":      "Vui lòng nhập đường dẫn http:// hoặc https:// hợp lệ.",

		// Settings Modal
		"settings.title":                 "Cài đặt",
		"settings.subtitle":              "Cấu hình tải xuống, số luồng và tích hợp trình duyệt",
		"settings.download_dir":          "Thư mục tải xuống mặc định",
		"settings.max_concurrent":        "Số tải xuống đồng thời tối đa",
		"settings.default_segments":      "Số luồng mặc định mỗi tệp",
		"settings.max_retries":           "Số lần thử lại tối đa",
		"settings.max_retries_hint":      "0 = không thử lại",
		"settings.speed_limit":           "Giới hạn tốc độ (KB/s)",
		"settings.speed_limit_hint":      "0 = không giới hạn",
		"settings.auto_categorize":       "Tự phân loại thư mục con",
		"settings.auto_categorize_on":    "Bật (Video, Âm nhạc, ...)",
		"settings.enabled":               "Bật",
		"settings.disabled":              "Tắt",
		"settings.launch_startup":        "Khởi động cùng Windows",
		"settings.appearance":            "Giao diện",
		"settings.dark":                  "Tối",
		"settings.light":                 "Sáng",
		"settings.close_behavior":        "Khi bấm nút Đóng (X)",
		"settings.minimize_behavior":     "Khi bấm nút Thu nhỏ",
		"settings.hide_to_tray":          "Ẩn xuống khay hệ thống",
		"settings.exit_app":              "Thoát ứng dụng",
		"settings.taskbar":               "Thanh tác vụ",
		"settings.native_title":          "Native Messaging cho Chrome / Edge",
		"settings.native_registered":     "Đã đăng ký",
		"settings.native_not_registered": "Chưa đăng ký",
		"settings.native_register":       "Đăng ký",
		"settings.native_reregister":     "Đăng ký lại",
		"settings.native_err":            "Lỗi Native Messaging: %s",
		"settings.native_ok":             "Đã đăng ký Native Messaging: %s",
		"settings.welcome_setup":         "Thiết lập ban đầu",
		"settings.cancel":                "Hủy",
		"settings.save":                  "Lưu thay đổi",
		"settings.err_empty_dir":         "Thư mục tải xuống không được để trống.",
		"settings.err_retries":           "Số lần thử lại phải là số lớn hơn hoặc bằng 0 (0 = không thử lại).",
		"settings.err_speed_limit":       "Giới hạn tốc độ phải là 0 (không giới hạn) hoặc số dương (KB/s).",
		"settings.err_startup":           "Không thể cập nhật khởi động cùng Windows: %s",

		// Software Updates
		"update.title":            "Cập nhật phần mềm  •  %s",
		"update.auto_check_sub":   "Tự động kiểm tra khi khởi động",
		"update.auto":             "Tự động",
		"update.manual":           "Thủ công",
		"update.check_btn":        "Kiểm tra cập nhật",
		"update.checking_btn":     "Đang kiểm tra...",
		"update.checking_msg":     "Đang kiểm tra bản cập nhật...",
		"update.check_failed":     "Kiểm tra cập nhật thất bại: %s",
		"update.available":        "Đã có phiên bản mới %s",
		"update.up_to_date":       "Bạn đang dùng phiên bản mới nhất (%s)",
		"update.downloading_msg":  "Đang tải bản cập nhật...",
		"update.failed":           "Cập nhật thất bại: %s",
		"update.applying":         "Đang cài đặt cập nhật và khởi động lại MagicDown...",
		"update.restart_hint":     "Ứng dụng sẽ tự khởi động lại để hoàn tất.",
		"update.download_install": "Tải & Cài đặt cập nhật",
		"update.progress":         "Đang tải... %s",
		"update.progress_total":   "Đang tải... %s / %s (%.0f%%)",

		// Welcome / Onboarding Modal
		"onboarding.title":               "Chào mừng đến với MagicDown",
		"onboarding.subtitle":            "Thiết lập nhanh thư mục tải, khởi động, khay hệ thống và tiện ích trình duyệt",
		"onboarding.download_folder":     "Thư mục tải xuống mặc định",
		"onboarding.auto_categorize":     "Tự phân loại thư mục con (Video, Âm nhạc, Tệp nén, ...)",
		"onboarding.enabled_recommended": "Bật (Khuyên dùng)",
		"onboarding.launch_windows":      "Khởi động cùng Windows",
		"onboarding.run_in_tray":         "Chạy ẩn dưới khay hệ thống",
		"onboarding.browser_title":       "Tích hợp tiện ích trình duyệt",
		"onboarding.browser_sub":         "Chọn trình duyệt Chromium đã cài để đăng ký Native Messaging và mở trang Tiện ích",
		"onboarding.install_selected":    "Cài đặt đã chọn",
		"onboarding.browser_ready":       "Sẵn sàng tích hợp",
		"onboarding.browser_registered":  "Đã đăng ký Native Host",
		"onboarding.no_browser":          "Không tự động phát hiện trình duyệt Chromium hỗ trợ. Bạn vẫn có thể tải thư mục tiện ích thủ công.",
		"onboarding.extension_folder":    "Thư mục tiện ích: %s",
		"onboarding.copy_path":           "Sao chép đường dẫn",
		"onboarding.copied_path":         "Đã sao chép đường dẫn thư mục tiện ích",
		"onboarding.select_one_browser":  "Hãy chọn ít nhất một trình duyệt đã cài để tích hợp.",
		"onboarding.integrated_count":    "Đã tích hợp %d/%d trình duyệt.",
		"onboarding.skip":                "Bỏ qua lúc này",
		"onboarding.finish":              "Hoàn tất thiết lập",
		"onboarding.err_empty_dir":       "Thư mục tải xuống mặc định không được để trống.",

		// System Tray
		"tray.open":       "Mở MagicDown",
		"tray.pause_all":  "Tạm dừng tất cả",
		"tray.resume_all": "Tiếp tục tất cả",
		"tray.quit":       "Thoát",
		"tray.tooltip":    "MagicDown — Trình tăng tốc tải xuống đa luồng",
	},
}

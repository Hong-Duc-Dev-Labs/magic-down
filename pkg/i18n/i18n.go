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
		"toolbar.batch_import":       "Batch URLs",
		"toolbar.new_download":       "New Download",

		// Delete confirmation modal
		"delete.title":   "Remove Download?",
		"delete.body":    "Remove \"%s\" from the download list? Any completed file on disk will be kept.",
		"delete.cancel":  "Cancel",
		"delete.confirm": "Remove",

		// Refresh Download Address
		"refresh.title":           "Refresh Download Address",
		"refresh.subtitle":        "Recover an expired link without losing downloaded progress",
		"refresh.capture_title":   "Capture from Source Page",
		"refresh.capture_sub":     "Open the original page and click Download again — MagicDown will capture the fresh link automatically.",
		"refresh.open_source":     "Open Source Page",
		"refresh.paste_label":     "Or Paste New Direct URL",
		"refresh.url_placeholder": "https://example.com/fresh-signed-link",
		"refresh.apply":           "Apply & Resume",
		"refresh.cancel":          "Cancel",
		"refresh.waiting_banner":  "Waiting for refreshed link for %s — click download on the source page",
		"refresh.host_mismatch":   "Host differs from original (%s → %s). Confirm to apply this link.",
		"refresh.btn_paste_url":   "Paste URL",
		"refresh.err_invalid_url": "Please enter a valid http:// or https:// URL.",

		// Duplicate Download Detection
		"dup.title":         "Duplicate Download Detected",
		"dup.body":          "\"%s\" already exists in your download list (%s). Choose how to proceed:",
		"dup.open_existing": "Open Existing File",
		"dup.focus_task":    "Focus Existing Task",
		"dup.download_copy": "Download as Copy",
		"dup.overwrite":     "Overwrite / Re-download",
		"dup.cancel":        "Cancel",

		// Status messages
		"msg.resumed":           "Resumed %s",
		"msg.paused":            "Paused %s",
		"msg.removed":           "Removed %s",
		"msg.retrying":          "Retrying %s",
		"msg.opened_folder":     "Opened folder for %s",
		"msg.cleared_done":      "Cleared %d completed download(s)",
		"msg.added_download":    "Added download: %s",
		"msg.settings_saved":    "Settings saved",
		"msg.startup_enabled":   "Launch at Windows Startup enabled",
		"msg.startup_disabled":  "Launch at Windows Startup disabled",
		"msg.setup_complete":    "Setup complete",
		"msg.native_registered": "Chrome/Edge Native Messaging registered",
		"msg.refresh_waiting":   "Waiting for refreshed link for %s",
		"msg.refresh_applied":   "Refreshed link and resumed %s",
		"msg.refresh_cancelled": "Cancelled link refresh",
		"msg.dup_focused":       "Focused existing download: %s",
		"msg.dup_overwritten":   "Replacing existing download: %s",
		"msg.task_limit_updated": "Updated speed limit for %s",
		"msg.copied":            "Copied to clipboard",
		"msg.power_cancelled":   "Cancelled scheduled power action",

		// Task Details & Segment Visualizer
		"details.title":              "Download Details",
		"details.speed":              "Speed",
		"details.eta":                "ETA",
		"details.downloaded":         "Downloaded",
		"details.connections":        "Connections",
		"details.ranges_yes":         "Ranges Supported",
		"details.ranges_no":          "Single Stream",
		"details.url_label":          "Source URL",
		"details.save_path_label":    "Save Path",
		"details.copy":               "Copy",
		"details.reveal":             "Reveal",
		"details.open_file":          "Open File",
		"details.segments_title":     "Live Segments",
		"details.seg_col_id":         "#",
		"details.seg_col_range":      "Range",
		"details.seg_col_progress":   "Downloaded",
		"details.seg_col_status":     "Status",
		"details.speed_limit_label":  "Per-Task Speed Limit (KB/s)",
		"details.speed_limit_hint":   "0 = use global limit",
		"details.apply_limit":        "Apply",
		"details.err_speed_limit":    "Speed limit must be 0 or a positive number of KB/s.",
		"details.checksum_title":     "Checksum Verification",
		"details.checksum_compute":   "Compute SHA-256 / MD5",
		"details.checksum_computing": "Computing SHA-256 & MD5...",
		"details.checksum_expected":  "Paste expected SHA-256 or MD5 hash to verify",
		"details.checksum_match":     "Match (%s)",
		"details.checksum_mismatch":  "Mismatch",
		"details.close":              "Close",

		// Network Proxy settings
		"settings.proxy_title":    "Network Proxy",
		"settings.proxy_system":   "System Proxy",
		"settings.proxy_none":     "No Proxy",
		"settings.proxy_http":     "HTTP",
		"settings.proxy_socks5":   "SOCKS5",
		"settings.proxy_host":     "Proxy Host",
		"settings.proxy_port":     "Port",
		"settings.proxy_user":     "Username (optional)",
		"settings.proxy_pass":     "Password (optional)",
		"settings.proxy_test":     "Test Proxy",
		"settings.proxy_testing":  "Testing...",
		"settings.proxy_ok":       "Proxy connection succeeded",
		"settings.proxy_err":      "Proxy test failed: %s",
		"settings.err_proxy_host": "Proxy host is required for HTTP/SOCKS5 mode.",
		"settings.err_proxy_port": "Proxy port must be between 1 and 65535.",

		// Post-Download Actions
		"power.after_done":              "After finish:",
		"power.none":                    "Do nothing",
		"power.sleep":                   "Sleep PC",
		"power.shutdown":                "Shut down PC",
		"power.countdown_title_sleep":   "Sleeping PC in %ds",
		"power.countdown_title_shutdown": "Shutting down PC in %ds",
		"power.countdown_body":          "All downloads have finished. Click Cancel to abort or start a new download.",
		"power.cancel":                  "Cancel",
		"power.run_now":                 "Execute Now",

		// Batch URL Pattern Import
		"batch.title":            "Batch URL Import",
		"batch.subtitle":         "Paste multiple links or numeric/letter wildcards (up to 500 URLs)",
		"batch.urls_label":       "URLs or Wildcard Patterns (one per line)",
		"batch.urls_placeholder": "https://cdn.example.com/part_[01-20].zip",
		"batch.wildcard_hint":    "Supports [01-20], [1-100], [a-z] wildcards and comments starting with #.",
		"batch.preview_count":    "%d URL(s) ready to import",
		"batch.preview_more":     "...and %d more",
		"batch.start_btn":        "Start Batch",
		"batch.err_empty":        "Enter at least one valid http:// or https:// URL.",
		"msg.batch_added":        "Added %d batch download(s)",

		// Queue Scheduler & Clipboard Monitor
		"sched.title":       "Queue Scheduler",
		"sched.subtitle":    "Automatically start queued downloads in a time window and pause when it ends",
		"sched.start_time":  "Start Time (HH:MM)",
		"sched.stop_time":   "Stop Time (HH:MM)",
		"sched.days_label":  "Active Days (leave all off for every day)",
		"sched.day_0":       "Sun",
		"sched.day_1":       "Mon",
		"sched.day_2":       "Tue",
		"sched.day_3":       "Wed",
		"sched.day_4":       "Thu",
		"sched.day_5":       "Fri",
		"sched.day_6":       "Sat",
		"sched.stop_action": "When window ends",
		"sched.err_invalid": "Invalid schedule: %s",
		"clip.title":        "Clipboard Monitor (Opt-In)",
		"clip.subtitle":     "Open Download Info Dialog automatically when copying direct file URLs",

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
		"settings.tab_general":           "General",
		"settings.tab_downloads":         "Downloads",
		"settings.tab_browser":           "Browser",
		"settings.tab_updates":           "Updates",
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
		"settings.launch_startup_hint":   "Start silently in the system tray when Windows boots",
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
		"settings.welcome_setup_sub":     "Re-run the initial setup wizard for folder, tray, and browser integration",
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

		// YouTube (yt-dlp)
		"ytdlp.section_title":           "YouTube (yt-dlp)",
		"ytdlp.section_sub":             "Required for downloading YouTube videos (including SABR streams)",
		"ytdlp.checking":                "Checking yt-dlp status...",
		"ytdlp.installed":               "Installed (%s)",
		"ytdlp.installed_no_ver":        "Installed",
		"ytdlp.not_installed":           "Not installed — required for YouTube SABR video downloads",
		"ytdlp.btn_install":             "Install yt-dlp",
		"ytdlp.btn_update":              "Update yt-dlp",
		"ytdlp.btn_installing":          "Installing...",
		"ytdlp.stage_downloading":       "Downloading yt-dlp... %s",
		"ytdlp.stage_downloading_total": "Downloading yt-dlp... %s / %s (%.0f%%)",
		"ytdlp.stage_verifying":         "Verifying SHA-256 checksum...",
		"ytdlp.stage_installing":        "Installing yt-dlp...",
		"ytdlp.install_ok":              "yt-dlp installed (%s)",
		"ytdlp.install_ok_no_ver":       "yt-dlp installed successfully",
		"ytdlp.install_err":             "Failed to install yt-dlp: %s",

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
		"browser.section_title":           "Browser Integration",
		"browser.section_sub":             "Connect Chrome, Edge, and Chromium browsers via unpacked extension and Native Messaging",
		"browser.host_ok":                 "Host: Valid",
		"browser.host_missing":            "Host: Not registered",
		"browser.state_not_installed":     "Not Installed",
		"browser.state_install_required":  "Installation Required",
		"browser.state_host_missing":      "Host Not Registered",
		"browser.state_waiting":           "Waiting for Connection",
		"browser.state_connected":         "Connected",
		"browser.state_error":             "Connection Error",
		"browser.state_unsupported":       "Unsupported Browser",
		"browser.btn_install":             "Install Extension",
		"browser.btn_reconnect":           "Reconnect",
		"browser.btn_troubleshoot":        "Troubleshoot",
		"browser.store_opened":            "Opened Store page for %s. Waiting for extension handshake...",
		"browser.store_url_missing":       "Store URL for %s is not configured yet (waiting for Store submission). Native Host is ready.",
		"browser.unpacked_opened":         "Opened %s Extensions & copied extension path. Load unpacked to connect.",
		"browser.guide_title":             "Quick Setup — %s (Path copied to clipboard)",
		"browser.guide_step1":             "1. Enable Developer mode (top-right toggle).",
		"browser.guide_step2":             "2. Click Load unpacked.",
		"browser.guide_step3":             "3. Paste the copied path (%s) and select the folder.",
		"browser.reconnected":             "Re-verified Native Host for %s. Waiting for extension connection.",
		"browser.diag_title":              "Diagnostics — %s",
		"browser.diag_exe":                "Executable: %s",
		"browser.diag_manifest":           "Manifest: %s",
		"browser.diag_log":                "Host Log: %s",
		"browser.diag_hint":               "Hint: Load the unpacked extension folder in your browser and reload it if connection does not complete.",

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
		"toolbar.batch_import":       "Nhập hàng loạt",
		"toolbar.new_download":       "Thêm liên kết",

		// Delete confirmation modal
		"delete.title":   "Xóa khỏi danh sách?",
		"delete.body":    "Xóa \"%s\" khỏi danh sách tải xuống? Tệp đã tải trên ổ đĩa vẫn được giữ nguyên.",
		"delete.cancel":  "Hủy",
		"delete.confirm": "Xóa",

		// Refresh Download Address
		"refresh.title":           "Làm mới liên kết tải xuống",
		"refresh.subtitle":        "Khôi phục liên kết hết hạn mà không mất tiến độ đã tải",
		"refresh.capture_title":   "Bắt liên kết từ trang nguồn",
		"refresh.capture_sub":     "Mở lại trang nguồn và bấm tải xuống — MagicDown sẽ tự động bắt liên kết mới.",
		"refresh.open_source":     "Mở trang nguồn",
		"refresh.paste_label":     "Hoặc dán đường dẫn trực tiếp mới",
		"refresh.url_placeholder": "https://example.com/duong-dan-moi",
		"refresh.apply":           "Áp dụng & Tiếp tục",
		"refresh.cancel":          "Hủy",
		"refresh.waiting_banner":  "Đang chờ liên kết mới cho %s — hãy bấm tải xuống trên trang nguồn",
		"refresh.host_mismatch":   "Tên miền khác với liên kết gốc (%s → %s). Xác nhận để áp dụng.",
		"refresh.btn_paste_url":   "Dán URL",
		"refresh.err_invalid_url": "Vui lòng nhập đường dẫn http:// hoặc https:// hợp lệ.",

		// Duplicate Download Detection
		"dup.title":         "Phát hiện tải xuống trùng lặp",
		"dup.body":          "\"%s\" đã tồn tại trong danh sách tải xuống (%s). Chọn cách xử lý:",
		"dup.open_existing": "Mở tệp có sẵn",
		"dup.focus_task":    "Xem mục hiện có",
		"dup.download_copy": "Tải bản sao",
		"dup.overwrite":     "Ghi đè / Tải lại",
		"dup.cancel":        "Hủy",

		// Status messages
		"msg.resumed":           "Đã tiếp tục %s",
		"msg.paused":            "Đã tạm dừng %s",
		"msg.removed":           "Đã xóa %s",
		"msg.retrying":          "Đang thử lại %s",
		"msg.opened_folder":     "Đã mở thư mục cho %s",
		"msg.cleared_done":      "Đã dọn %d mục hoàn tất",
		"msg.added_download":    "Đã thêm tải xuống: %s",
		"msg.settings_saved":    "Đã lưu cài đặt",
		"msg.startup_enabled":   "Đã bật khởi động cùng Windows",
		"msg.startup_disabled":  "Đã tắt khởi động cùng Windows",
		"msg.setup_complete":    "Thiết lập hoàn tất",
		"msg.native_registered": "Đã đăng ký Native Messaging cho Chrome/Edge",
		"msg.refresh_waiting":   "Đang chờ liên kết mới cho %s",
		"msg.refresh_applied":   "Đã cập nhật liên kết và tiếp tục %s",
		"msg.refresh_cancelled": "Đã hủy làm mới liên kết",
		"msg.dup_focused":       "Đã chọn mục tải hiện có: %s",
		"msg.dup_overwritten":   "Đang tải lại thay thế: %s",
		"msg.task_limit_updated": "Đã cập nhật giới hạn tốc độ cho %s",
		"msg.copied":            "Đã sao chép vào bộ nhớ tạm",
		"msg.power_cancelled":   "Đã hủy hành động sau khi tải xong",

		// Task Details & Segment Visualizer
		"details.title":              "Chi tiết tải xuống",
		"details.speed":              "Tốc độ",
		"details.eta":                "Thời gian còn lại",
		"details.downloaded":         "Đã tải",
		"details.connections":        "Kết nối",
		"details.ranges_yes":         "Hỗ trợ đa luồng",
		"details.ranges_no":          "Đơn luồng",
		"details.url_label":          "Đường dẫn nguồn",
		"details.save_path_label":    "Đường dẫn lưu",
		"details.copy":               "Sao chép",
		"details.reveal":             "Mở thư mục",
		"details.open_file":          "Mở tệp",
		"details.segments_title":     "Phân đoạn trực tiếp",
		"details.seg_col_id":         "#",
		"details.seg_col_range":      "Phạm vi",
		"details.seg_col_progress":   "Đã tải",
		"details.seg_col_status":     "Trạng thái",
		"details.speed_limit_label":  "Giới hạn tốc độ riêng (KB/s)",
		"details.speed_limit_hint":   "0 = theo giới hạn chung",
		"details.apply_limit":        "Áp dụng",
		"details.err_speed_limit":    "Giới hạn tốc độ phải là 0 hoặc số dương (KB/s).",
		"details.checksum_title":     "Kiểm tra mã băm (Checksum)",
		"details.checksum_compute":   "Tính SHA-256 / MD5",
		"details.checksum_computing": "Đang tính SHA-256 & MD5...",
		"details.checksum_expected":  "Dán mã SHA-256 hoặc MD5 dự kiến để đối chiếu",
		"details.checksum_match":     "Khớp (%s)",
		"details.checksum_mismatch":  "Không khớp",
		"details.close":              "Đóng",

		// Network Proxy settings
		"settings.proxy_title":    "Proxy mạng",
		"settings.proxy_system":   "Proxy hệ thống",
		"settings.proxy_none":     "Không dùng Proxy",
		"settings.proxy_http":     "HTTP",
		"settings.proxy_socks5":   "SOCKS5",
		"settings.proxy_host":     "Máy chủ Proxy",
		"settings.proxy_port":     "Cổng",
		"settings.proxy_user":     "Tên đăng nhập (tùy chọn)",
		"settings.proxy_pass":     "Mật khẩu (tùy chọn)",
		"settings.proxy_test":     "Kiểm tra Proxy",
		"settings.proxy_testing":  "Đang kiểm tra...",
		"settings.proxy_ok":       "Kết nối Proxy thành công",
		"settings.proxy_err":      "Kiểm tra Proxy thất bại: %s",
		"settings.err_proxy_host": "Cần nhập máy chủ Proxy cho chế độ HTTP/SOCKS5.",
		"settings.err_proxy_port": "Cổng Proxy phải từ 1 đến 65535.",

		// Post-Download Actions
		"power.after_done":              "Khi xong:",
		"power.none":                    "Không làm gì",
		"power.sleep":                   "Ngủ máy",
		"power.shutdown":                "Tắt máy",
		"power.countdown_title_sleep":   "Sẽ chuyển sang chế độ ngủ sau %d giây",
		"power.countdown_title_shutdown": "Sẽ tắt máy tính sau %d giây",
		"power.countdown_body":          "Tất cả lượt tải đã hoàn tất. Bấm Hủy để dừng lại hoặc bắt đầu lượt tải mới.",
		"power.cancel":                  "Hủy",
		"power.run_now":                 "Thực hiện ngay",

		// Batch URL Pattern Import
		"batch.title":            "Nhập danh sách URL hàng loạt",
		"batch.subtitle":         "Dán nhiều liên kết hoặc mẫu ký tự đại diện số/chữ (tối đa 500 URL)",
		"batch.urls_label":       "Danh sách URL hoặc mẫu wildcard (mỗi dòng 1 mục)",
		"batch.urls_placeholder": "https://cdn.example.com/part_[01-20].zip",
		"batch.wildcard_hint":    "Hỗ trợ wildcard [01-20], [1-100], [a-z] và dòng chú thích bắt đầu bằng #.",
		"batch.preview_count":    "%d URL sẵn sàng nhập",
		"batch.preview_more":     "...và %d liên kết khác",
		"batch.start_btn":        "Bắt đầu tải hàng loạt",
		"batch.err_empty":        "Vui lòng nhập ít nhất một liên kết http:// hoặc https:// hợp lệ.",
		"msg.batch_added":        "Đã thêm %d lượt tải hàng loạt",

		// Queue Scheduler & Clipboard Monitor
		"sched.title":       "Lịch trình hàng đợi (Scheduler)",
		"sched.subtitle":    "Tự động chạy các mục đang chờ trong khung giờ và tạm dừng khi kết thúc",
		"sched.start_time":  "Giờ bắt đầu (HH:MM)",
		"sched.stop_time":   "Giờ kết thúc (HH:MM)",
		"sched.days_label":  "Ngày hoạt động (bỏ chọn hết = mọi ngày)",
		"sched.day_0":       "CN",
		"sched.day_1":       "T2",
		"sched.day_2":       "T3",
		"sched.day_3":       "T4",
		"sched.day_4":       "T5",
		"sched.day_5":       "T6",
		"sched.day_6":       "T7",
		"sched.stop_action": "Khi hết khung giờ",
		"sched.err_invalid": "Lịch trình không hợp lệ: %s",
		"clip.title":        "Theo dõi bộ nhớ tạm (Tùy chọn)",
		"clip.subtitle":     "Tự mở hộp thoại tải xuống khi sao chép liên kết tệp trực tiếp",

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
		"settings.tab_general":           "Chung",
		"settings.tab_downloads":         "Tải xuống",
		"settings.tab_browser":           "Trình duyệt",
		"settings.tab_updates":           "Cập nhật",
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
		"settings.launch_startup_hint":   "Khởi chạy ẩn dưới khay hệ thống khi mở Windows",
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
		"settings.welcome_setup_sub":     "Mở lại trình thiết lập nhanh cho thư mục, khay hệ thống và trình duyệt",
		"settings.cancel":                "Hủy",
		"settings.save":                  "Lưu cài đặt",
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
		"update.download_install": "Tải & Cài đặt bản mới",
		"update.progress":         "Đang tải... %s",
		"update.progress_total":   "Đang tải... %s / %s (%.0f%%)",

		// YouTube (yt-dlp)
		"ytdlp.section_title":           "YouTube (yt-dlp)",
		"ytdlp.section_sub":             "Cần thiết để tải video YouTube (bao gồm luồng SABR)",
		"ytdlp.checking":                "Đang kiểm tra trạng thái yt-dlp...",
		"ytdlp.installed":               "Đã cài đặt (%s)",
		"ytdlp.installed_no_ver":        "Đã cài đặt",
		"ytdlp.not_installed":           "Chưa cài đặt — cần thiết để tải video YouTube SABR",
		"ytdlp.btn_install":             "Cài đặt yt-dlp",
		"ytdlp.btn_update":              "Cập nhật yt-dlp",
		"ytdlp.btn_installing":          "Đang cài đặt...",
		"ytdlp.stage_downloading":       "Đang tải yt-dlp... %s",
		"ytdlp.stage_downloading_total": "Đang tải yt-dlp... %s / %s (%.0f%%)",
		"ytdlp.stage_verifying":         "Đang xác minh mã băm SHA-256...",
		"ytdlp.stage_installing":        "Đang cài đặt yt-dlp...",
		"ytdlp.install_ok":              "Đã cài đặt yt-dlp (%s)",
		"ytdlp.install_ok_no_ver":       "Đã cài đặt yt-dlp thành công",
		"ytdlp.install_err":             "Cài đặt yt-dlp thất bại: %s",

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
		"browser.section_title":          "Tích hợp trình duyệt",
		"browser.section_sub":            "Kết nối Chrome, Edge và trình duyệt Chromium qua tiện ích giải nén và Native Messaging",
		"browser.host_ok":                "Host: Hợp lệ",
		"browser.host_missing":           "Host: Chưa đăng ký",
		"browser.state_not_installed":    "Chưa cài đặt",
		"browser.state_install_required": "Cần cài tiện ích",
		"browser.state_host_missing":     "Chưa đăng ký Host",
		"browser.state_waiting":          "Đang chờ kết nối",
		"browser.state_connected":        "Đã kết nối",
		"browser.state_error":            "Lỗi kết nối",
		"browser.state_unsupported":      "Không hỗ trợ",
		"browser.btn_install":            "Cài đặt Extension",
		"browser.btn_reconnect":          "Kết nối lại",
		"browser.btn_troubleshoot":       "Khắc phục sự cố",
		"browser.store_opened":           "Đã mở trang Store cho %s. Đang chờ tiện ích bắt tay kết nối...",
		"browser.store_url_missing":      "Chưa cấu hình Store URL cho %s (đang chờ duyệt trên Store). Native Host đã sẵn sàng.",
		"browser.unpacked_opened":        "Đã mở trang Extensions của %s và sao chép đường dẫn. Hãy chọn Load unpacked để kết nối.",
		"browser.guide_title":            "Hướng dẫn nhanh — %s (Đã sao chép đường dẫn)",
		"browser.guide_step1":            "1. Bật Developer mode (Chế độ dành cho nhà phát triển).",
		"browser.guide_step2":            "2. Bấm Load unpacked (Tải tiện ích đã giải nén).",
		"browser.guide_step3":            "3. Dán đường dẫn vừa sao chép (%s) và chọn thư mục.",
		"browser.reconnected":            "Đã kiểm tra lại Native Host cho %s. Đang chờ tiện ích kết nối.",
		"browser.diag_title":             "Chẩn đoán — %s",
		"browser.diag_exe":               "Tệp thực thi: %s",
		"browser.diag_manifest":          "Manifest: %s",
		"browser.diag_log":               "Nhật ký Host: %s",
		"browser.diag_hint":              "Gợi ý: Hãy nạp thư mục extension đã giải nén (Load unpacked) trong trình duyệt và tải lại nếu chưa tự kết nối.",

		// System Tray
		"tray.open":       "Mở MagicDown",
		"tray.pause_all":  "Tạm dừng tất cả",
		"tray.resume_all": "Tiếp tục tất cả",
		"tray.quit":       "Thoát",
		"tray.tooltip":    "MagicDown — Trình tăng tốc tải xuống đa luồng",
	},
}

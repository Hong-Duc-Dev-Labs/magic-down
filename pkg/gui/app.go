package gui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/autostart"
	"magicdown/pkg/engine"
	"magicdown/pkg/i18n"
	"magicdown/pkg/ipc"
	"magicdown/pkg/nativemessaging"
	"magicdown/pkg/updater"
	"magicdown/pkg/version"
)

// WindowController abstracts native window caption and tray visibility actions
// so the UI remains decoupled and testable.
type WindowController interface {
	Minimize()
	ToggleMaximize()
	IsMaximized() bool
	Close()
	Hide()
	Show()
	Restore()
	Focus()
	IsVisible() bool
	Invalidate()
}

// FrameScheduleMode describes how the MyGo render loop should schedule the next frame.
type FrameScheduleMode int

const (
	// FrameSchedulePaused suspends all automatic GPU/UI frame scheduling while hidden in tray.
	FrameSchedulePaused FrameScheduleMode = iota
	// FrameScheduleActive requests continuous animation frames while downloads are active.
	FrameScheduleActive
	// FrameScheduleIdle polls lightly when visible and idle.
	FrameScheduleIdle
)

// App holds the UI state for MagicDown's MyGo Native UI desktop application.
type App struct {
	engine    *engine.Engine
	ipcServer *ipc.Server
	winCtrl   WindowController
	maximized bool

	// Tray & power-save state
	trayMu   sync.RWMutex
	inTray   bool
	quitting bool

	// Injectable OS hooks (default to real Windows implementations, overridable in tests)
	autostartSetter     func(bool) error
	nativeRegFn         func() (string, error)
	nativeStatusFn      func() nativemessaging.RegistrationStatus
	detectBrowsersFn    func() []nativemessaging.BrowserInfo
	integrateBrowsersFn func([]nativemessaging.BrowserInfo, string, string, bool) []nativemessaging.BrowserInstallResult
	nativeStatus        nativemessaging.RegistrationStatus
	nativeRegMsg        string

	// Detected Chromium browsers for Welcome Setup & Settings integration
	detectedBrowsers []nativemessaging.BrowserInfo
	selectedBrowsers map[string]bool
	browserResults   map[string]nativemessaging.BrowserInstallResult
	extensionDir     string

	darkMode       bool
	filterStatus   string       // "All", "Downloading", "Completed", "Queued", "Failed"
	filterCategory FileCategory // CatAll or specific category
	searchQuery    string
	selectedTaskID string

	// Add / Download Info Dialog state
	showAddModal       bool
	inputFromExtension bool
	inputURL           string
	inputAudioURL      string
	inputFilename      string
	inputCategory      string
	inputSaveDir       string
	inputSegments      int
	inputEstimatedSize int64
	inputReferrer      string
	inputCookies       string
	inputHeaders       map[string]string
	inputConvertFormat string
	inputTargetFormat  string
	inputOutputFormat  string
	inputAudioBitrate  string
	inputPlan          *engine.DownloadPlan
	lastAutoTarget     string
	addError           string

	// Incoming extension downloads waiting for the Download Info Dialog
	pendingMu       sync.Mutex
	pendingIncoming []ipc.AddDownloadRequest

	// Settings modal state
	showSettingsModal       bool
	settingDownloadDir      string
	settingMaxConcurrent    int
	settingSegments         int
	settingMaxRetries       string
	settingSpeedLimit       string
	settingAutoCategorize   bool
	settingLaunchAtStartup  bool
	settingMinimizeToTray   bool
	settingCloseToTray      bool
	settingAutoCheckUpdates bool
	settingsError           string

	// Auto-updater state (protected by updateMu)
	quitFn             func()
	checkUpdateFn      func(context.Context) (*updater.CheckResult, error)
	downloadAndApplyFn func(context.Context, *updater.ReleaseInfo, updater.ProgressFunc) error
	updateMu           sync.Mutex
	updateStatus       string // "", "checking", "up_to_date", "available", "downloading", "applying", "error"
	updateRelease      *updater.ReleaseInfo
	updateMessage      string
	updateDownloaded   int64
	updateTotal        int64

	// First-run Welcome / Onboarding modal state
	showOnboardingModal       bool
	onboardingDownloadDir     string
	onboardingAutoCategorize  bool
	onboardingLaunchAtStartup bool
	onboardingRunInTray       bool
	onboardingError           string

	// Language & i18n
	language         i18n.Lang
	onLanguageChange func(i18n.Lang)

	statusMessage string
}

// NewApp creates a new MagicDown UI state instance with sleek dark mode enabled by default.
func NewApp(eng *engine.Engine, srv *ipc.Server) *App {
	cfg := eng.Config()
	lang := i18n.ParseOrDetect(cfg.Language)
	i18n.SetLang(lang)
	app := &App{
		engine:                    eng,
		ipcServer:                 srv,
		language:                  lang,
		autostartSetter:           autostart.SetEnabled,
		nativeRegFn:               func() (string, error) { return nativemessaging.RegisterHost("", nativemessaging.DefaultExtensionID) },
		nativeStatusFn:            nativemessaging.CheckRegistration,
		detectBrowsersFn:          nativemessaging.DetectInstalledBrowsers,
		integrateBrowsersFn:       nativemessaging.IntegrateBrowsers,
		selectedBrowsers:          make(map[string]bool),
		browserResults:            make(map[string]nativemessaging.BrowserInstallResult),
		extensionDir:              nativemessaging.ResolveExtensionDir(),
		darkMode:                  true,
		filterStatus:              "All",
		filterCategory:            CatAll,
		inputCategory:             engine.CategoryOther,
		inputSaveDir:              cfg.DownloadDir,
		inputSegments:             cfg.DefaultSegments,
		settingDownloadDir:        cfg.DownloadDir,
		settingMaxConcurrent:      cfg.MaxConcurrentDownloads,
		settingSegments:           cfg.DefaultSegments,
		settingMaxRetries:         strconv.Itoa(cfg.MaxRetries),
		settingSpeedLimit:         strconv.FormatInt(cfg.SpeedLimitKBps, 10),
		settingAutoCategorize:     cfg.AutoCategorize,
		settingLaunchAtStartup:    cfg.LaunchAtStartup,
		settingMinimizeToTray:     cfg.MinimizeToTray,
		settingCloseToTray:        cfg.CloseToTray,
		settingAutoCheckUpdates:   cfg.AutoCheckUpdates,
		showOnboardingModal:       !cfg.OnboardingCompleted,
		onboardingDownloadDir:     cfg.DownloadDir,
		onboardingAutoCategorize:  cfg.AutoCategorize,
		onboardingLaunchAtStartup: cfg.LaunchAtStartup,
		onboardingRunInTray:       cfg.CloseToTray || cfg.MinimizeToTray,
	}
	app.refreshNativeStatus()
	app.refreshDetectedBrowsers()
	return app
}

// SetOnLanguageChange registers a callback invoked immediately when the user switches UI language.
func (a *App) SetOnLanguageChange(fn func(i18n.Lang)) {
	a.onLanguageChange = fn
}

// SetLanguage applies a new UI language immediately, persists it in config, and notifies listeners (e.g., System Tray).
func (a *App) SetLanguage(lang i18n.Lang) {
	norm := i18n.Normalize(string(lang))
	a.language = norm
	i18n.SetLang(norm)
	if a.engine != nil {
		cfg := a.engine.Config()
		if cfg.Language != string(norm) {
			cfg.Language = string(norm)
			a.engine.UpdateConfig(cfg)
		}
	}
	if a.onLanguageChange != nil {
		a.onLanguageChange(norm)
	}
	a.invalidateUI()
}

func (a *App) refreshNativeStatus() {
	if a.nativeStatusFn != nil {
		a.nativeStatus = a.nativeStatusFn()
	}
}

func (a *App) refreshDetectedBrowsers() {
	if a.detectBrowsersFn == nil {
		return
	}
	a.detectedBrowsers = a.detectBrowsersFn()
	if a.selectedBrowsers == nil {
		a.selectedBrowsers = make(map[string]bool)
	}
	for _, b := range a.detectedBrowsers {
		if _, exists := a.selectedBrowsers[b.ID]; !exists && b.Installed {
			a.selectedBrowsers[b.ID] = true
		}
	}
}

func (a *App) selectedBrowserList() []nativemessaging.BrowserInfo {
	var list []nativemessaging.BrowserInfo
	for _, b := range a.detectedBrowsers {
		if b.Installed && a.selectedBrowsers[b.ID] {
			list = append(list, b)
		}
	}
	return list
}

func (a *App) integrateSelectedBrowsers(openBrowserUI bool) {
	if a.integrateBrowsersFn == nil {
		return
	}
	selected := a.selectedBrowserList()
	if len(selected) == 0 {
		a.nativeRegMsg = i18n.T("onboarding.select_one_browser")
		return
	}
	results := a.integrateBrowsersFn(selected, "", nativemessaging.DefaultExtensionID, openBrowserUI)
	if a.browserResults == nil {
		a.browserResults = make(map[string]nativemessaging.BrowserInstallResult)
	}
	okCount := 0
	for _, r := range results {
		a.browserResults[r.BrowserID] = r
		if r.Err == nil && r.NativeHostOK {
			okCount++
		}
	}
	a.refreshNativeStatus()
	a.refreshDetectedBrowsers()
	a.nativeRegMsg = i18n.Tf("onboarding.integrated_count", okCount, len(results))
	a.statusMessage = a.nativeRegMsg
}

// IsInTray reports whether the main window is currently hidden in the system tray.
func (a *App) IsInTray() bool {
	a.trayMu.RLock()
	defer a.trayMu.RUnlock()
	return a.inTray
}

// SetQuitting marks the app as shutting down so close events are not intercepted by tray hiding.
func (a *App) SetQuitting(q bool) {
	a.trayMu.Lock()
	a.quitting = q
	a.trayMu.Unlock()
}

// IsQuitting reports whether the app is in the process of quitting.
func (a *App) IsQuitting() bool {
	a.trayMu.RLock()
	defer a.trayMu.RUnlock()
	return a.quitting
}

// HideToTray hides the main window and suspends GPU/UI frame scheduling.
func (a *App) HideToTray() {
	a.trayMu.Lock()
	a.inTray = true
	a.trayMu.Unlock()
	if a.winCtrl != nil {
		a.winCtrl.Hide()
	}
}

// WakeFromTray restores and focuses the main window and resumes UI rendering.
func (a *App) WakeFromTray() {
	a.trayMu.Lock()
	a.inTray = false
	a.trayMu.Unlock()
	if a.winCtrl != nil {
		a.winCtrl.Show()
		a.winCtrl.Restore()
		a.winCtrl.Focus()
		a.winCtrl.Invalidate()
	}
}

// HandleCloseRequest handles a window close action (title bar X or OS close event).
// Returns true if the window was hidden to tray (caller should prevent close).
func (a *App) HandleCloseRequest() bool {
	if !a.IsQuitting() && a.engine.Config().CloseToTray {
		a.HideToTray()
		return true
	}
	return false
}

// HandleMinimizeRequest handles the custom title bar minimize button.
func (a *App) HandleMinimizeRequest() {
	if a.engine.Config().MinimizeToTray {
		a.HideToTray()
		return
	}
	if a.winCtrl != nil {
		a.winCtrl.Minimize()
	}
}

// FrameScheduleMode returns the current render-loop scheduling policy.
// While hidden in the system tray, rendering is completely paused to minimize CPU/GPU usage.
func (a *App) FrameScheduleMode() FrameScheduleMode {
	if a.IsInTray() {
		return FrameSchedulePaused
	}
	if a.engine != nil && a.engine.ActiveCount() > 0 {
		return FrameScheduleActive
	}
	return FrameScheduleIdle
}

// EnqueueIncomingDownload queues a download received over IPC so the UI opens
// the Download Info Dialog on the next frame, waking the window if hidden in tray.
func (a *App) EnqueueIncomingDownload(req ipc.AddDownloadRequest) {
	a.pendingMu.Lock()
	a.pendingIncoming = append(a.pendingIncoming, req)
	a.pendingMu.Unlock()
	if a.IsInTray() {
		a.WakeFromTray()
	} else if a.winCtrl != nil {
		a.winCtrl.Invalidate()
	}
}

func (a *App) popPendingIncoming() {
	if a.showAddModal {
		return
	}
	a.pendingMu.Lock()
	if len(a.pendingIncoming) == 0 {
		a.pendingMu.Unlock()
		return
	}
	req := a.pendingIncoming[0]
	a.pendingIncoming = a.pendingIncoming[1:]
	a.pendingMu.Unlock()

	a.openIncomingDownloadDialog(req)
}

func (a *App) openIncomingDownloadDialog(req ipc.AddDownloadRequest) {
	cfg := a.engine.Config()
	a.inputFromExtension = true
	a.inputURL = strings.TrimSpace(req.URL)
	a.inputAudioURL = strings.TrimSpace(req.AudioURL)
	a.inputReferrer = req.Referrer
	a.inputCookies = req.Cookies
	a.inputHeaders = req.Headers
	a.inputConvertFormat = req.ConvertFormat
	a.inputTargetFormat = req.TargetFormat
	a.inputOutputFormat = req.OutputFormat
	a.inputAudioBitrate = req.AudioBitrate
	a.inputEstimatedSize = req.TotalBytes
	a.inputPlan = nil
	if len(req.Plan) > 0 {
		if parsed, err := engine.ParseDownloadPlan(req.Plan, req.URL); err == nil {
			a.inputPlan = parsed
			if a.inputEstimatedSize <= 0 {
				a.inputEstimatedSize = parsed.KnownSize()
			}
		}
	}

	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		filename = engine.ExtractFilename(a.inputURL, nil)
		if a.inputAudioURL != "" {
			filename = engine.ReplaceExtension(filename, "mp4")
		}
	}
	a.inputFilename = filename

	cat := req.Category
	if cat == "" {
		if a.inputAudioURL != "" {
			cat = engine.CategoryVideo
		} else {
			cat = engine.CategoryForFilename(a.inputFilename, a.inputURL)
		}
	}
	a.inputCategory = cat
	if strings.TrimSpace(req.SaveDir) != "" {
		a.inputSaveDir = strings.TrimSpace(req.SaveDir)
	} else {
		a.inputSaveDir = engine.ResolveSaveDir(cfg.DownloadDir, cat, cfg.AutoCategorize)
	}
	a.inputSegments = req.NumSegments
	if a.inputSegments != 4 && a.inputSegments != 8 && a.inputSegments != 16 {
		a.inputSegments = cfg.DefaultSegments
		if a.inputSegments != 4 && a.inputSegments != 8 && a.inputSegments != 16 {
			a.inputSegments = 8
		}
	}
	a.lastAutoTarget = a.inputFilename + "|" + a.inputURL
	a.addError = ""
	a.showSettingsModal = false
	a.showAddModal = true
}

// SetWindowController attaches the native MyGo window controller for custom title bar buttons.
func (a *App) SetWindowController(ctrl WindowController) {
	a.winCtrl = ctrl
}

// SetMaximized updates the cached maximize state (wired from Window.OnMaximize/OnUnmaximize).
func (a *App) SetMaximized(maxed bool) {
	a.maximized = maxed
}

func (a *App) palette() Palette {
	if a.darkMode {
		return DarkPalette()
	}
	return LightPalette()
}

// View renders the entire desktop interface for each frame.
func (a *App) View(c *ui.Context) {
	// Power-save mode: when hidden in the Windows System Tray, skip layout/draw
	// and do not schedule AnimationFrame or timer wakeups.
	if a.IsInTray() {
		return
	}

	pal := a.palette()
	c.SetTheme(pal.UITheme)

	// Global keyboard shortcuts
	if c.Shortcut(ui.Cmd, ui.KeyN) && !a.showAddModal && !a.showOnboardingModal {
		a.openAddModal()
	}
	if c.Shortcut(0, ui.KeyEscape) {
		a.showAddModal = false
		a.showSettingsModal = false
	}

	// Open Download Info Dialog for any incoming extension download.
	a.popPendingIncoming()

	tasks := a.engine.GetTasks()

	// Animate smoothly while downloads are active; poll lightly otherwise for IPC arrivals.
	switch a.FrameScheduleMode() {
	case FrameScheduleActive:
		c.AnimationFrame()
	case FrameScheduleIdle:
		c.After(250 * time.Millisecond)
	}

	var totalSpeed int64
	statusCounts := map[string]int{
		"All":         len(tasks),
		"Downloading": 0,
		"Completed":   0,
		"Queued":      0,
		"Failed":      0,
	}
	catCounts := map[FileCategory]int{
		CatVideo:      0,
		CatMusic:      0,
		CatCompressed: 0,
		CatDocuments:  0,
		CatPrograms:   0,
		CatOther:      0,
	}

	var selectedTask *engine.Task
	completedCount := 0
	for _, t := range tasks {
		if t.ID == a.selectedTaskID {
			selectedTask = t
		}
		totalSpeed += t.Speed
		switch t.Status {
		case engine.StatusDownloading, engine.StatusProbing, engine.StatusMuxing:
			statusCounts["Downloading"]++
		case engine.StatusCompleted:
			statusCounts["Completed"]++
			completedCount++
		case engine.StatusQueued, engine.StatusPaused:
			statusCounts["Queued"]++
		case engine.StatusFailed, engine.StatusCancelled:
			statusCounts["Failed"]++
		}
		catCounts[ClassifyFile(t.Filename, t.URL)]++
	}

	// Root window layout: Integrated Custom Title Bar + (Left Sidebar | Right Main Column)
	ui.Column(c).Fill().Background(pal.Canvas).TextColor(pal.TextPrimary).Children(func() {
		a.renderTitleBar(c, pal)

		ui.Row(c).Fill().Grow(1).AlignItems(ui.Stretch).Children(func() {
			a.renderSidebar(c, pal, statusCounts, catCounts)

			// Main right column: Header + List + Status Bar
			ui.Column(c).Fill().Grow(1).Children(func() {
				a.renderHeader(c, pal, selectedTask, completedCount)
				a.renderDownloadList(c, pal, tasks)
				a.renderStatusBar(c, pal, statusCounts["Downloading"], statusCounts["Queued"], totalSpeed)
			})
		})
	})

	// Modals
	a.renderAddModal(c, pal)
	a.renderSettingsModal(c, pal)
	a.renderOnboardingModal(c, pal)
}

func (a *App) renderTitleBar(c *ui.Context, pal Palette) {
	ui.Row(c).
		Height(38).
		Shrink(0).
		Background(pal.Sidebar).
		BorderWidth(0, 0, 1, 0).
		BorderColor(pal.Border).
		AlignItems(ui.Stretch).
		Children(func() {
			// Draggable left + center title strip (supports drag & double-click to maximize)
			ui.Row(c).
				Grow(1).
				Padding(0, 14).
				Gap(8).
				AlignItems(ui.Center).
				DragWindow().
				Children(func() {
					ui.Row(c).Width(20).Height(20).Radius(5).Background(pal.Accent).Center().Shrink(0).Children(func() {
						ui.Icon(c, iconBolt).Width(12).Height(12).TextColor(pal.AccentText)
					})
					ui.Text(c, "MagicDown").FontSize(13).FontWeight(700).TextColor(pal.TextPrimary).SingleLine()
					ui.Box(c).Padding(1, 5).Radius(4).Background(pal.BadgeBg).Shrink(0).Children(func() {
						ui.Text(c, "v1.0").FontSize(10).FontWeight(600).TextColor(pal.TextMuted).SingleLine()
					})
					ui.Text(c, "—").FontSize(11).TextColor(pal.BorderStrong).SingleLine()
					ui.Text(c, i18n.T("app.subtitle")).FontSize(11).TextColor(pal.TextMuted).SingleLine()
					ui.Spacer(c)
				})

			// Native-style Minimize, Maximize/Restore (with Windows 11 Snap Layout), and Close buttons
			ui.Row(c).Shrink(0).AlignItems(ui.Stretch).Children(func() {
				// Minimize
				minBtn := ui.Row(c).Width(46).Center().Cursor(ui.CursorPointer).Focusable()
				minBg := ui.Transparent
				minFg := pal.TextSecondary
				if minBtn.Hovered() {
					minBg = pal.NavHover
					minFg = pal.TextPrimary
				}
				minBtn.Background(minBg).Children(func() {
					ui.Icon(c, iconWinMin).Width(11).Height(11).TextColor(minFg)
				})
				if minBtn.Clicked() {
					a.HandleMinimizeRequest()
				}

				// Maximize / Restore
				isMax := a.maximized
				if a.winCtrl != nil {
					isMax = a.winCtrl.IsMaximized()
				}
				maxIcon := iconWinMax
				if isMax {
					maxIcon = iconWinRestore
				}
				maxBtn := ui.Row(c).Width(46).Center().Cursor(ui.CursorPointer).Focusable().MaximizeButton()
				maxBg := ui.Transparent
				maxFg := pal.TextSecondary
				if maxBtn.Hovered() {
					maxBg = pal.NavHover
					maxFg = pal.TextPrimary
				}
				maxBtn.Background(maxBg).Children(func() {
					ui.Icon(c, maxIcon).Width(11).Height(11).TextColor(maxFg)
				})
				if maxBtn.Clicked() && a.winCtrl != nil {
					a.winCtrl.ToggleMaximize()
				}

				// Close (semantic Windows red hover)
				closeBtn := ui.Row(c).Width(46).Center().Cursor(ui.CursorPointer).Focusable()
				closeBg := ui.Transparent
				closeFg := pal.TextSecondary
				if closeBtn.Hovered() {
					closeBg = ui.Hex("#E81123")
					closeFg = ui.Hex("#FFFFFF")
				}
				closeBtn.Background(closeBg).Children(func() {
					ui.Icon(c, iconWinClose).Width(11).Height(11).TextColor(closeFg)
				})
				if closeBtn.Clicked() {
					if !a.HandleCloseRequest() && a.winCtrl != nil {
						a.winCtrl.Close()
					}
				}
			})
		})
}

func (a *App) openAddModal() {
	cfg := a.engine.Config()
	a.inputFromExtension = false
	a.inputURL = ""
	a.inputAudioURL = ""
	a.inputFilename = ""
	a.inputCategory = engine.CategoryOther
	a.inputSaveDir = engine.ResolveSaveDir(cfg.DownloadDir, a.inputCategory, cfg.AutoCategorize)
	a.inputSegments = cfg.DefaultSegments
	if a.inputSegments != 4 && a.inputSegments != 8 && a.inputSegments != 16 {
		a.inputSegments = 8
	}
	a.inputEstimatedSize = 0
	a.inputReferrer = ""
	a.inputCookies = ""
	a.inputHeaders = nil
	a.inputConvertFormat = ""
	a.inputPlan = nil
	a.lastAutoTarget = ""
	a.addError = ""
	a.showSettingsModal = false
	a.showOnboardingModal = false
	a.showAddModal = true
}

func (a *App) openSettingsModal() {
	cfg := a.engine.Config()
	a.settingDownloadDir = cfg.DownloadDir
	a.settingMaxConcurrent = cfg.MaxConcurrentDownloads
	a.settingSegments = cfg.DefaultSegments
	a.settingMaxRetries = strconv.Itoa(cfg.MaxRetries)
	a.settingSpeedLimit = strconv.FormatInt(cfg.SpeedLimitKBps, 10)
	a.settingAutoCategorize = cfg.AutoCategorize
	a.settingLaunchAtStartup = cfg.LaunchAtStartup
	a.settingMinimizeToTray = cfg.MinimizeToTray
	a.settingCloseToTray = cfg.CloseToTray
	a.settingAutoCheckUpdates = cfg.AutoCheckUpdates
	a.settingsError = ""
	a.nativeRegMsg = ""
	a.refreshNativeStatus()
	a.showAddModal = false
	a.showOnboardingModal = false
	a.showSettingsModal = true
}

// OpenOnboardingModal opens the Welcome Setup dialog populated from current config.
func (a *App) OpenOnboardingModal() {
	cfg := a.engine.Config()
	a.onboardingDownloadDir = cfg.DownloadDir
	a.onboardingAutoCategorize = cfg.AutoCategorize
	a.onboardingLaunchAtStartup = cfg.LaunchAtStartup
	a.onboardingRunInTray = cfg.CloseToTray || cfg.MinimizeToTray
	a.onboardingError = ""
	a.nativeRegMsg = ""
	a.refreshNativeStatus()
	a.refreshDetectedBrowsers()
	a.showAddModal = false
	a.showSettingsModal = false
	a.showOnboardingModal = true
}

func (a *App) renderSidebar(c *ui.Context, pal Palette, statusCounts map[string]int, catCounts map[FileCategory]int) {
	ui.Column(c).
		Width(220).
		Shrink(0).
		Background(pal.Sidebar).
		BorderWidth(0, 1, 0, 0).
		BorderColor(pal.Border).
		Padding(14, 12, 12, 12).
		Gap(14).
		Children(func() {
			// Status section
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, i18n.T("sidebar.downloads")).FontSize(10).FontWeight(700).TextColor(pal.TextMuted).Padding(0, 10, 4, 10)
				statusNav := []struct {
					key   string
					label string
					icon  *ui.SVG
				}{
					{"All", i18n.T("sidebar.all_downloads"), iconInbox},
					{"Downloading", i18n.T("sidebar.downloading"), iconDownload},
					{"Completed", i18n.T("sidebar.completed"), iconCheck},
					{"Queued", i18n.T("sidebar.queued_paused"), iconClock},
					{"Failed", i18n.T("sidebar.failed"), iconAlert},
				}
				for _, item := range statusNav {
					active := a.filterStatus == item.key && a.filterCategory == CatAll
					if navPill(c, pal, item.icon, item.label, statusCounts[item.key], active) {
						a.filterStatus = item.key
						a.filterCategory = CatAll
					}
				}
			})

			// Categories section
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, i18n.T("sidebar.categories")).FontSize(10).FontWeight(700).TextColor(pal.TextMuted).Padding(0, 10, 4, 10)
				catNav := []struct {
					cat   FileCategory
					label string
					icon  *ui.SVG
				}{
					{CatVideo, i18n.T("cat.Video"), iconMedia},
					{CatMusic, i18n.T("cat.Music"), iconMedia},
					{CatCompressed, i18n.T("cat.Compressed"), iconArchive},
					{CatDocuments, i18n.T("cat.Documents"), iconDoc},
					{CatPrograms, i18n.T("cat.Programs"), iconProgram},
					{CatOther, i18n.T("cat.Other"), iconFile},
				}
				for _, item := range catNav {
					active := a.filterCategory == item.cat
					if navPill(c, pal, item.icon, item.label, catCounts[item.cat], active) {
						if a.filterCategory == item.cat {
							a.filterCategory = CatAll
						} else {
							a.filterCategory = item.cat
							a.filterStatus = "All"
						}
					}
				}
			})

			ui.Spacer(c)

			// Bottom pinned utility controls
			ui.Column(c).Gap(3).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Padding(10, 0, 0, 0).Children(func() {
				themeIcon := iconSun
				themeLabel := i18n.T("sidebar.light_mode")
				if !a.darkMode {
					themeIcon = iconMoon
					themeLabel = i18n.T("sidebar.dark_mode")
				}
				if navPill(c, pal, themeIcon, themeLabel, -1, false) {
					a.darkMode = !a.darkMode
				}
				if navPill(c, pal, iconSettings, i18n.T("sidebar.settings"), -1, a.showSettingsModal) {
					a.openSettingsModal()
				}
			})
		})
}

func (a *App) renderHeader(c *ui.Context, pal Palette, selected *engine.Task, completedCount int) {
	canResume := selected != nil && (selected.Status == engine.StatusPaused || selected.Status == engine.StatusFailed || selected.Status == engine.StatusQueued)
	canPause := selected != nil && (selected.Status == engine.StatusDownloading || selected.Status == engine.StatusProbing)
	canDelete := selected != nil
	canClearDone := completedCount > 0

	ui.Row(c).
		Height(60).
		Shrink(0).
		Padding(0, 20).
		Gap(10).
		Background(pal.Header).
		BorderWidth(0, 0, 1, 0).
		BorderColor(pal.Border).
		AlignItems(ui.Center).
		Children(func() {
			// Search field container
			ui.Row(c).
				Width(260).
				Padding(0, 10).
				Gap(7).
				Radius(8).
				Background(pal.InputBg).
				Border(1, pal.Border).
				AlignItems(ui.Center).
				Children(func() {
					ui.Icon(c, iconSearch).Width(14).Height(14).TextColor(pal.TextMuted).Shrink(0)
					ui.TextInput(c, &a.searchQuery).
						Placeholder(i18n.T("toolbar.search_placeholder")).
						Background(ui.Transparent).
						Border(0, ui.Transparent).
						Padding(6, 0).
						Grow(1)
				})

			ui.Spacer(c)

			// Contextual action buttons
			if toolbarButton(c, pal, iconPlay, i18n.T("toolbar.resume"), !canResume, false) {
				if err := a.engine.ResumeTask(selected.ID); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.statusMessage = i18n.Tf("msg.resumed", selected.Filename)
				}
			}

			if toolbarButton(c, pal, iconPause, i18n.T("toolbar.pause"), !canPause, false) {
				if err := a.engine.PauseTask(selected.ID); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.statusMessage = i18n.Tf("msg.paused", selected.Filename)
				}
			}

			if toolbarButton(c, pal, iconTrash, i18n.T("toolbar.delete"), !canDelete, true) {
				if err := a.engine.DeleteTask(selected.ID, false); err != nil {
					a.statusMessage = err.Error()
				} else {
					a.statusMessage = i18n.Tf("msg.removed", selected.Filename)
					a.selectedTaskID = ""
				}
			}

			if toolbarButton(c, pal, iconCheck, i18n.T("toolbar.clear_done"), !canClearDone, false) {
				removed := 0
				for _, t := range a.engine.GetTasks() {
					if t.Status == engine.StatusCompleted {
						if err := a.engine.DeleteTask(t.ID, false); err == nil {
							removed++
							if a.selectedTaskID == t.ID {
								a.selectedTaskID = ""
							}
						}
					}
				}
				a.statusMessage = i18n.Tf("msg.cleared_done", removed)
			}

			// Primary CTA
			if primaryCTA(c, pal, iconPlus, i18n.T("toolbar.new_download")) {
				a.openAddModal()
			}
		})
}

func (a *App) filterTasks(tasks []*engine.Task) []*engine.Task {
	query := strings.ToLower(strings.TrimSpace(a.searchQuery))
	filtered := make([]*engine.Task, 0, len(tasks))
	for _, t := range tasks {
		// Category filter
		if a.filterCategory != CatAll && ClassifyFile(t.Filename, t.URL) != a.filterCategory {
			continue
		}
		// Status filter
		switch a.filterStatus {
		case "Downloading":
			if t.Status != engine.StatusDownloading && t.Status != engine.StatusProbing {
				continue
			}
		case "Completed":
			if t.Status != engine.StatusCompleted {
				continue
			}
		case "Queued":
			if t.Status != engine.StatusQueued && t.Status != engine.StatusPaused {
				continue
			}
		case "Failed":
			if t.Status != engine.StatusFailed && t.Status != engine.StatusCancelled {
				continue
			}
		}
		// Search filter
		if query != "" {
			if !strings.Contains(strings.ToLower(t.Filename), query) &&
				!strings.Contains(strings.ToLower(t.URL), query) {
				continue
			}
		}
		filtered = append(filtered, t)
	}
	return filtered
}

func (a *App) renderDownloadList(c *ui.Context, pal Palette, allTasks []*engine.Task) {
	filtered := a.filterTasks(allTasks)

	if len(filtered) == 0 {
		a.renderEmptyState(c, pal, len(allTasks) == 0)
		return
	}

	ui.Scroll(c).Fill().Grow(1).Children(func() {
		ui.Column(c).Fill().Padding(16, 20).Gap(10).Children(func() {
			for _, task := range filtered {
				a.renderDownloadCard(c, pal, task)
			}
		})
	})
}

func (a *App) renderEmptyState(c *ui.Context, pal Palette, noDownloadsAtAll bool) {
	ui.Column(c).Fill().Grow(1).Center().Padding(40).Gap(12).Children(func() {
		ui.Row(c).Width(56).Height(56).Radius(14).Background(pal.Card).Border(1, pal.Border).Center().Children(func() {
			ui.Icon(c, iconDownload).Width(24).Height(24).TextColor(pal.Accent)
		})

		title := i18n.T("empty.no_downloads_title")
		subtitle := i18n.T("empty.no_downloads_sub")
		if !noDownloadsAtAll {
			title = i18n.T("empty.no_match_title")
			subtitle = i18n.T("empty.no_match_sub")
		}

		ui.Text(c, title).FontSize(16).FontWeight(600).TextColor(pal.TextPrimary)
		ui.Text(c, subtitle).FontSize(13).TextColor(pal.TextSecondary).MaxWidth(420)

		ui.Box(c).Height(4)
		if noDownloadsAtAll {
			if primaryCTA(c, pal, iconPlus, i18n.T("empty.add_new")) {
				a.openAddModal()
			}
		} else if toolbarButton(c, pal, iconRefresh, i18n.T("empty.reset_filters"), false, false) {
			a.filterStatus = "All"
			a.filterCategory = CatAll
			a.searchQuery = ""
		}
	})
}

func (a *App) renderDownloadCard(c *ui.Context, pal Palette, task *engine.Task) {
	isSelected := a.selectedTaskID == task.ID
	cat := ClassifyFile(task.Filename, task.URL)
	catIcon, catFg, catBg := categoryVisual(cat, pal)

	card := ui.Column(c).
		Padding(14, 16).
		Gap(10).
		Radius(10).
		Cursor(ui.CursorPointer).
		Focusable()

	bg := pal.Card
	border := pal.Border
	if isSelected {
		bg = pal.CardSelected
		border = pal.Accent
	} else if card.Hovered() {
		bg = pal.CardHover
		border = pal.BorderStrong
	}

	name := task.Filename
	if name == "" {
		name = extractHost(task.URL)
	}
	host := extractHost(task.URL)
	pct := task.ProgressPercentage()

	barColor := pal.Accent
	switch task.Status {
	case engine.StatusCompleted:
		barColor = pal.Success
	case engine.StatusPaused, engine.StatusQueued:
		barColor = pal.Warning
	case engine.StatusFailed, engine.StatusCancelled:
		barColor = pal.Danger
	}

	segLabel := i18n.T("card.single_stream")
	if task.SupportsRanges && len(task.Segments) > 1 {
		segLabel = i18n.Tf("card.multi_segs", len(task.Segments))
	} else if task.NumSegments > 1 {
		segLabel = i18n.Tf("card.multi_segs", task.NumSegments)
	}

	card.Background(bg).Border(1, border).Children(func() {
		// Top row: Category icon + Filename/Host + Status Pill + Inline quick actions
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			// File category badge
			ui.Row(c).Width(38).Height(38).Radius(9).Background(catBg).Center().Shrink(0).Children(func() {
				ui.Icon(c, catIcon).Width(18).Height(18).TextColor(catFg)
			})

			// Title + metadata
			ui.Column(c).Gap(3).Grow(1).Children(func() {
				ui.Text(c, name).FontSize(14).FontWeight(600).TextColor(pal.TextPrimary).SingleLine().Ellipsis("...")
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, host).FontSize(12).TextColor(pal.TextSecondary).SingleLine().Ellipsis("...")
					ui.Box(c).Padding(1, 6).Radius(4).Background(pal.BadgeBg).Shrink(0).Children(func() {
						ui.Text(c, segLabel).FontSize(10).FontWeight(600).FontFeatures("tnum").TextColor(pal.TextMuted).SingleLine()
					})
					if task.Retries > 0 && (task.Status == engine.StatusDownloading || task.Status == engine.StatusProbing) {
						ui.Box(c).Padding(1, 6).Radius(4).Background(pal.WarningSoft).Shrink(0).Children(func() {
							ui.Text(c, i18n.Tf("card.retry", task.Retries)).FontSize(10).FontWeight(600).FontFeatures("tnum").TextColor(pal.Warning).SingleLine()
						})
					}
					if task.ErrorMessage != "" {
						ui.Text(c, task.ErrorMessage).FontSize(11).TextColor(pal.Danger).SingleLine().Ellipsis("...")
					}
				})
			})

			// Status badge
			statusBadge(c, pal, task.Status)

			// Inline quick action icons
			ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
				switch task.Status {
				case engine.StatusDownloading, engine.StatusProbing:
					if rowIconButton(c, pal, iconPause, false) {
						_ = a.engine.PauseTask(task.ID)
						a.statusMessage = i18n.Tf("msg.paused", name)
					}
				case engine.StatusPaused, engine.StatusQueued:
					if rowIconButton(c, pal, iconPlay, false) {
						_ = a.engine.ResumeTask(task.ID)
						a.statusMessage = i18n.Tf("msg.resumed", name)
					}
				case engine.StatusFailed, engine.StatusCancelled:
					if rowIconButton(c, pal, iconRefresh, false) {
						_ = a.engine.ResumeTask(task.ID)
						a.statusMessage = i18n.Tf("msg.retrying", name)
					}
				}

				if task.SavePath != "" {
					if rowIconButton(c, pal, iconFolder, false) {
						openInFileManager(task.SavePath)
						a.statusMessage = i18n.Tf("msg.opened_folder", name)
					}
				}

				if rowIconButton(c, pal, iconTrash, true) {
					_ = a.engine.DeleteTask(task.ID, false)
					if a.selectedTaskID == task.ID {
						a.selectedTaskID = ""
					}
					a.statusMessage = i18n.Tf("msg.removed", name)
				}
			})
		})

		// Bottom row: Rounded progress bar + right-aligned tabular metrics
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Progress(c, pct/100.0).
				Height(6).
				Radius(999).
				Background(pal.TrackBg).
				TextColor(barColor).
				Grow(1)

			// Tabular metrics
			sizeText := FormatBytes(task.DownloadedBytes)
			if task.TotalBytes > 0 {
				sizeText = fmt.Sprintf("%s / %s", FormatBytes(task.DownloadedBytes), FormatBytes(task.TotalBytes))
			}

			ui.Row(c).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, sizeText).FontSize(12).FontFeatures("tnum").TextColor(pal.TextSecondary).SingleLine()

				if task.Status == engine.StatusDownloading && task.Speed > 0 {
					ui.Text(c, "•").FontSize(11).TextColor(pal.TextMuted)
					ui.Text(c, FormatSpeed(task.Speed)).FontSize(12).FontWeight(600).FontFeatures("tnum").TextColor(pal.Accent).SingleLine()
					if task.ETA > 0 {
						ui.Text(c, "•").FontSize(11).TextColor(pal.TextMuted)
						ui.Text(c, FormatETA(task.ETA)).FontSize(12).FontFeatures("tnum").TextColor(pal.TextSecondary).SingleLine()
					}
				}

				ui.Text(c, fmt.Sprintf("%.1f%%", pct)).
					FontSize(12).
					FontWeight(600).
					FontFeatures("tnum").
					TextColor(pal.TextPrimary).
					MinWidth(52).
					SingleLine()
			})
		})
	})

	if card.Clicked() {
		a.selectedTaskID = task.ID
	}
}

func (a *App) renderStatusBar(c *ui.Context, pal Palette, activeCount, queuedCount int, totalSpeed int64) {
	ipcPort := ipc.DefaultPort
	ipcConnected := false
	if a.ipcServer != nil {
		ipcPort = a.ipcServer.Port()
		ipcConnected = ipcPort > 0
	}

	dotColor := pal.Success
	ipcLabel := i18n.Tf("statusbar.ipc_ready", ipcPort)
	if !ipcConnected {
		dotColor = pal.Warning
		ipcLabel = i18n.T("statusbar.ipc_offline")
	}

	ui.Row(c).
		Height(34).
		Shrink(0).
		Padding(0, 20).
		Gap(12).
		Background(pal.Header).
		BorderWidth(1, 0, 0, 0).
		BorderColor(pal.Border).
		AlignItems(ui.Center).
		Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Box(c).Width(7).Height(7).Radius(999).Background(dotColor)
				ui.Text(c, ipcLabel).FontSize(11).FontWeight(500).TextColor(pal.TextSecondary).SingleLine()
			})

			if a.statusMessage != "" {
				ui.Text(c, "|").FontSize(11).TextColor(pal.BorderStrong)
				ui.Text(c, a.statusMessage).FontSize(11).TextColor(pal.TextMuted).SingleLine().Ellipsis("...")
			}

			ui.Spacer(c)

			summary := i18n.Tf("statusbar.summary", activeCount, queuedCount, FormatSpeed(totalSpeed))
			ui.Text(c, summary).FontSize(11).FontWeight(500).FontFeatures("tnum").TextColor(pal.TextSecondary).SingleLine()
		})
}

func (a *App) renderAddModal(c *ui.Context, pal Palette) {
	if !a.showAddModal {
		return
	}
	if c.Shortcut(0, ui.KeyEnter) {
		a.submitAddDownload(true)
		return
	}

	ui.Modal(c, &a.showAddModal, func() {
		ui.Column(c).Width(520).Gap(16).Children(func() {
			// Auto-update category and save dir when URL or filename changes
			autoTarget := strings.TrimSpace(a.inputFilename) + "|" + strings.TrimSpace(a.inputURL)
			if autoTarget != a.lastAutoTarget {
				a.lastAutoTarget = autoTarget
				cfg := a.engine.Config()
				detectedCat := engine.CategoryForFilename(strings.TrimSpace(a.inputFilename), strings.TrimSpace(a.inputURL))
				if a.inputAudioURL != "" {
					detectedCat = engine.CategoryVideo
				}
				a.inputCategory = detectedCat
				a.inputSaveDir = engine.ResolveSaveDir(cfg.DownloadDir, detectedCat, cfg.AutoCategorize)
			}

			// Modal Header
			modalTitle := i18n.T("add.title_new")
			modalSub := i18n.T("add.sub_new")
			if a.inputFromExtension {
				modalTitle = i18n.T("add.title_info")
				modalSub = i18n.T("add.sub_info")
			}
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.AccentSoft).Center().Children(func() {
						ui.Icon(c, iconPlus).Width(16).Height(16).TextColor(pal.Accent)
					})
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, modalTitle).FontSize(16).FontWeight(700).TextColor(pal.TextPrimary)
						ui.Text(c, modalSub).FontSize(12).TextColor(pal.TextMuted)
					})
				})
				if rowIconButton(c, pal, iconClose, false) {
					a.showAddModal = false
				}
			})

			ui.Divider(c)

			// Estimated size & Auto-Mux badge bar
			if a.inputEstimatedSize > 0 || a.inputAudioURL != "" {
				ui.Row(c).Padding(8, 10).Radius(7).Background(pal.InputBg).Border(1, pal.Border).Gap(10).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					sizeLabel := i18n.T("add.estimated_unknown")
					if a.inputEstimatedSize > 0 {
						sizeLabel = i18n.Tf("add.estimated_size", FormatBytes(a.inputEstimatedSize))
					}
					ui.Text(c, sizeLabel).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					if a.inputAudioURL != "" {
						ui.Box(c).Padding(2, 7).Radius(999).Background(pal.PurpleSoft).Children(func() {
							ui.Text(c, i18n.T("add.auto_mux_badge")).FontSize(11).FontWeight(600).TextColor(pal.Purple)
						})
					}
				})
			}

			// URL Field + Clipboard Paste button
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("add.url_label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c, &a.inputURL).
						Placeholder(i18n.T("add.url_placeholder")).
						Background(pal.InputBg).
						Border(1, pal.Border).
						Radius(7).
						Grow(1)
					if toolbarButton(c, pal, iconClipboard, i18n.T("add.paste"), false, false) {
						clip := strings.TrimSpace(c.ReadClipboard())
						if clip != "" {
							a.inputURL = clip
							a.addError = ""
						}
					}
				})
				if strings.TrimSpace(a.inputFilename) == "" && strings.HasPrefix(strings.TrimSpace(a.inputURL), "http") {
					preview := engine.ExtractFilename(strings.TrimSpace(a.inputURL), nil)
					ui.Text(c, i18n.Tf("add.auto_filename", preview)).FontSize(11).TextColor(pal.Accent)
				}
			})

			// Filename
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("add.filename_label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.TextInput(c, &a.inputFilename).
					Placeholder(i18n.T("add.filename_placeholder")).
					Background(pal.InputBg).
					Border(1, pal.Border).
					Radius(7)
			})

			// Category Selector
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("add.category_label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(6).Children(func() {
					cfg := a.engine.Config()
					for _, cat := range engine.AllCategories {
						if segmentOption(c, pal, i18n.T("cat."+cat), a.inputCategory == cat) {
							a.inputCategory = cat
							a.inputSaveDir = engine.ResolveSaveDir(cfg.DownloadDir, cat, cfg.AutoCategorize)
						}
					}
				})
			})

			// Save Dir
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("add.savedir_label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.TextInput(c, &a.inputSaveDir).
					Background(pal.InputBg).
					Border(1, pal.Border).
					Radius(7)
			})

			// Segment Count Selector
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("add.segments_label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					for _, seg := range []int{4, 8, 16} {
						if segmentOption(c, pal, i18n.Tf("add.connections", seg), a.inputSegments == seg) {
							a.inputSegments = seg
						}
					}
				})
			})

			if a.addError != "" {
				ui.Row(c).Padding(8, 10).Radius(6).Background(pal.DangerSoft).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Danger)
					ui.Text(c, a.addError).FontSize(12).TextColor(pal.Danger)
				})
			}

			ui.Divider(c)

			// Modal Footer
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c)
				if toolbarButton(c, pal, nil, i18n.T("add.cancel"), false, false) {
					a.showAddModal = false
				}
				if toolbarButton(c, pal, iconClock, i18n.T("add.download_later"), false, false) {
					a.submitAddDownload(false)
				}
				if primaryCTA(c, pal, iconDownload, i18n.T("add.start_download")) {
					a.submitAddDownload(true)
				}
			})
		})
	})
}

func (a *App) submitAddDownload(autoStart bool) {
	rawURL := strings.TrimSpace(a.inputURL)
	if rawURL == "" || (!strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://")) {
		a.addError = i18n.T("add.err_invalid_url")
		return
	}

	segs := a.inputSegments
	if segs < 1 || segs > 16 {
		segs = a.engine.Config().DefaultSegments
	}

	task, err := a.engine.AddTask(engine.AddTaskRequest{
		URL:           rawURL,
		AudioURL:      strings.TrimSpace(a.inputAudioURL),
		Category:      a.inputCategory,
		Filename:      strings.TrimSpace(a.inputFilename),
		SaveDir:       strings.TrimSpace(a.inputSaveDir),
		NumSegments:   segs,
		TotalBytes:    a.inputEstimatedSize,
		Referrer:      a.inputReferrer,
		Cookies:       a.inputCookies,
		Headers:       a.inputHeaders,
		ConvertFormat: a.inputConvertFormat,
		TargetFormat:  a.inputTargetFormat,
		OutputFormat:  a.inputOutputFormat,
		AudioBitrate:  a.inputAudioBitrate,
		Plan:          a.inputPlan,
		AutoStart:     autoStart,
		StartPaused:   !autoStart,
	})
	if err != nil {
		a.addError = err.Error()
		return
	}

	a.selectedTaskID = task.ID
	a.statusMessage = i18n.Tf("msg.added_download", task.URL)
	a.showAddModal = false
}

func (a *App) renderSettingsModal(c *ui.Context, pal Palette) {
	if !a.showSettingsModal {
		return
	}

	ui.Modal(c, &a.showSettingsModal, func() {
		ui.Column(c).Width(520).Gap(16).Children(func() {
			// Header
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.AccentSoft).Center().Children(func() {
						ui.Icon(c, iconSettings).Width(16).Height(16).TextColor(pal.Accent)
					})
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, i18n.T("settings.title")).FontSize(16).FontWeight(700).TextColor(pal.TextPrimary)
						ui.Text(c, i18n.T("settings.subtitle")).FontSize(12).TextColor(pal.TextMuted)
					})
				})
				if rowIconButton(c, pal, iconClose, false) {
					a.showSettingsModal = false
				}
			})

			ui.Divider(c)

			// Language (takes effect immediately)
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("lang.label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					if segmentOption(c, pal, i18n.T("lang.vi"), a.language == i18n.LangVI) {
						a.SetLanguage(i18n.LangVI)
					}
					if segmentOption(c, pal, i18n.T("lang.en"), a.language == i18n.LangEN) {
						a.SetLanguage(i18n.LangEN)
					}
				})
			})

			// Download Directory
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("settings.download_dir")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.TextInput(c, &a.settingDownloadDir).
					Background(pal.InputBg).
					Border(1, pal.Border).
					Radius(7)
			})

			// Max Concurrent Downloads
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("settings.max_concurrent")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					for _, n := range []int{1, 2, 3, 5, 8} {
						if segmentOption(c, pal, fmt.Sprintf("%d", n), a.settingMaxConcurrent == n) {
							a.settingMaxConcurrent = n
						}
					}
				})
			})

			// Default Segments
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("settings.default_segments")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					for _, seg := range []int{4, 8, 16} {
						if segmentOption(c, pal, i18n.Tf("add.connections", seg), a.settingSegments == seg) {
							a.settingSegments = seg
						}
					}
				})
			})

			// Reliability & throughput limits
			ui.Row(c).Gap(16).Children(func() {
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, i18n.T("settings.max_retries")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingMaxRetries).
						Background(pal.InputBg).
						Border(1, pal.Border).
						Radius(7)
					ui.Text(c, i18n.T("settings.max_retries_hint")).FontSize(10).TextColor(pal.TextMuted)
				})
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, i18n.T("settings.speed_limit")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingSpeedLimit).
						Background(pal.InputBg).
						Border(1, pal.Border).
						Radius(7)
					ui.Text(c, i18n.T("settings.speed_limit_hint")).FontSize(10).TextColor(pal.TextMuted)
				})
			})

			// Auto-Categorize toggle
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("settings.auto_categorize")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					if segmentOption(c, pal, i18n.T("settings.auto_categorize_on"), a.settingAutoCategorize) {
						a.settingAutoCategorize = true
					}
					if segmentOption(c, pal, i18n.T("settings.disabled"), !a.settingAutoCategorize) {
						a.settingAutoCategorize = false
					}
				})
			})

			// Windows Startup & System Tray
			ui.Row(c).Gap(16).Children(func() {
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, i18n.T("settings.launch_startup")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.Row(c).Gap(6).Children(func() {
						if segmentOption(c, pal, i18n.T("settings.enabled"), a.settingLaunchAtStartup) {
							a.settingLaunchAtStartup = true
						}
						if segmentOption(c, pal, i18n.T("settings.disabled"), !a.settingLaunchAtStartup) {
							a.settingLaunchAtStartup = false
						}
					})
				})
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, i18n.T("settings.appearance")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.Row(c).Gap(6).Children(func() {
						if segmentOption(c, pal, i18n.T("settings.dark"), a.darkMode) {
							a.darkMode = true
						}
						if segmentOption(c, pal, i18n.T("settings.light"), !a.darkMode) {
							a.darkMode = false
						}
					})
				})
			})

			ui.Row(c).Gap(16).Children(func() {
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, i18n.T("settings.close_behavior")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.Row(c).Gap(6).Children(func() {
						if segmentOption(c, pal, i18n.T("settings.hide_to_tray"), a.settingCloseToTray) {
							a.settingCloseToTray = true
						}
						if segmentOption(c, pal, i18n.T("settings.exit_app"), !a.settingCloseToTray) {
							a.settingCloseToTray = false
						}
					})
				})
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, i18n.T("settings.minimize_behavior")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.Row(c).Gap(6).Children(func() {
						if segmentOption(c, pal, i18n.T("settings.hide_to_tray"), a.settingMinimizeToTray) {
							a.settingMinimizeToTray = true
						}
						if segmentOption(c, pal, i18n.T("settings.taskbar"), !a.settingMinimizeToTray) {
							a.settingMinimizeToTray = false
						}
					})
				})
			})

			// Native Messaging & IPC Status Info Card
			ipcPort := ipc.DefaultPort
			if a.ipcServer != nil {
				ipcPort = a.ipcServer.Port()
			}
			ui.Row(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, i18n.T("settings.native_title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
					regState := i18n.T("settings.native_not_registered")
					if a.nativeStatus.IsReady() {
						regState = i18n.T("settings.native_registered")
					}
					ui.Text(c, fmt.Sprintf("%s  •  IPC 127.0.0.1:%d", regState, ipcPort)).FontSize(11).FontFeatures("tnum").TextColor(pal.TextMuted)
				})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					regLabel := i18n.T("settings.native_register")
					if a.nativeStatus.IsReady() {
						regLabel = i18n.T("settings.native_reregister")
					}
					if toolbarButton(c, pal, nil, regLabel, false, false) {
						a.registerNativeMessaging()
					}
				})
			})
			if a.nativeRegMsg != "" {
				ui.Text(c, a.nativeRegMsg).FontSize(11).TextColor(pal.Accent)
			}

			// Software Updates card
			a.renderUpdateCard(c, pal)

			if a.settingsError != "" {
				ui.Row(c).Padding(8, 10).Radius(6).Background(pal.DangerSoft).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Danger)
					ui.Text(c, a.settingsError).FontSize(12).TextColor(pal.Danger)
				})
			}

			ui.Divider(c)

			ui.Row(c).Gap(8).Children(func() {
				if toolbarButton(c, pal, nil, i18n.T("settings.welcome_setup"), false, false) {
					a.OpenOnboardingModal()
				}
				ui.Spacer(c)
				if toolbarButton(c, pal, nil, i18n.T("settings.cancel"), false, false) {
					a.showSettingsModal = false
				}
				if primaryCTA(c, pal, iconCheck, i18n.T("settings.save")) {
					a.saveSettings()
				}
			})
		})
	})
}

func (a *App) registerNativeMessaging() {
	if a.nativeRegFn == nil {
		return
	}
	path, err := a.nativeRegFn()
	if err != nil {
		a.nativeRegMsg = i18n.Tf("settings.native_err", err.Error())
		return
	}
	a.refreshNativeStatus()
	a.nativeRegMsg = i18n.Tf("settings.native_ok", filepath.Base(path))
	a.statusMessage = i18n.T("msg.native_registered")
}

// SetQuitFunc registers a callback used to cleanly exit the application (e.g. before self-update restart).
func (a *App) SetQuitFunc(fn func()) {
	a.quitFn = fn
}

// StartAutoUpdateCheck runs a background update check on startup if enabled in config.
func (a *App) StartAutoUpdateCheck() {
	if !a.engine.Config().AutoCheckUpdates {
		return
	}
	a.triggerUpdateCheck()
}

func (a *App) invalidateUI() {
	if a.winCtrl != nil {
		a.winCtrl.Invalidate()
	}
}

func (a *App) newUpdater() *updater.Updater {
	cfg := a.engine.Config()
	return updater.New(updater.Options{
		Repo:           cfg.UpdateRepo,
		CurrentVersion: version.Version,
	})
}

func (a *App) triggerUpdateCheck() {
	a.updateMu.Lock()
	if a.updateStatus == "checking" || a.updateStatus == "downloading" || a.updateStatus == "applying" {
		a.updateMu.Unlock()
		return
	}
	a.updateStatus = "checking"
	a.updateMessage = i18n.T("update.checking_msg")
	a.updateMu.Unlock()
	a.invalidateUI()

	checkFn := a.checkUpdateFn
	if checkFn == nil {
		u := a.newUpdater()
		checkFn = u.CheckForUpdate
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		res, err := checkFn(ctx)
		a.updateMu.Lock()
		if err != nil {
			a.updateStatus = "error"
			a.updateMessage = i18n.Tf("update.check_failed", err.Error())
		} else if res.UpdateAvailable && res.Release != nil {
			a.updateStatus = "available"
			a.updateRelease = res.Release
			a.updateMessage = i18n.Tf("update.available", res.LatestVersion)
		} else {
			a.updateStatus = "up_to_date"
			a.updateRelease = res.Release
			a.updateMessage = i18n.Tf("update.up_to_date", version.NormalizedVersion())
		}
		a.updateMu.Unlock()
		a.invalidateUI()
	}()
}

func (a *App) triggerDownloadAndInstall() {
	a.updateMu.Lock()
	if a.updateStatus == "downloading" || a.updateStatus == "applying" || a.updateRelease == nil {
		a.updateMu.Unlock()
		return
	}
	rel := a.updateRelease
	a.updateStatus = "downloading"
	a.updateDownloaded = 0
	a.updateTotal = rel.AssetSize
	a.updateMessage = i18n.T("update.downloading_msg")
	a.updateMu.Unlock()
	a.invalidateUI()

	applyFn := a.downloadAndApplyFn
	if applyFn == nil {
		u := a.newUpdater()
		applyFn = func(ctx context.Context, info *updater.ReleaseInfo, onProgress updater.ProgressFunc) error {
			dlDir, err := os.MkdirTemp("", "magicdown-update-dl-*")
			if err != nil {
				return err
			}
			archivePath, err := u.DownloadAsset(ctx, info, dlDir, onProgress)
			if err != nil {
				return err
			}
			if err := u.VerifySHA256(ctx, archivePath, info.AssetName, info.ChecksumURL); err != nil {
				return err
			}
			stagingDir, err := os.MkdirTemp("", "magicdown-update-stage-*")
			if err != nil {
				return err
			}
			if err := updater.StageUpdate(archivePath, stagingDir); err != nil {
				return err
			}
			_ = os.RemoveAll(dlDir)

			exePath, err := os.Executable()
			if err != nil {
				return err
			}
			installDir := filepath.Dir(exePath)
			relaunchExe := filepath.Join(installDir, "magicdown.exe")
			_, err = updater.LaunchApplyScript(stagingDir, installDir, os.Getpid(), relaunchExe)
			return err
		}
	}

	go func() {
		ctx := context.Background()
		err := applyFn(ctx, rel, func(downloaded, total int64) {
			a.updateMu.Lock()
			a.updateDownloaded = downloaded
			a.updateTotal = total
			a.updateMu.Unlock()
			a.invalidateUI()
		})

		a.updateMu.Lock()
		if err != nil {
			a.updateStatus = "error"
			a.updateMessage = i18n.Tf("update.failed", err.Error())
			a.updateMu.Unlock()
			a.invalidateUI()
			return
		}
		a.updateStatus = "applying"
		a.updateMessage = i18n.T("update.applying")
		a.updateMu.Unlock()
		a.invalidateUI()

		if a.quitFn != nil {
			time.Sleep(300 * time.Millisecond)
			a.quitFn()
		}
	}()
}

func summarizeReleaseNotes(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		kept = append(kept, trimmed)
		if len(kept) >= 2 {
			break
		}
	}
	summary := strings.Join(kept, " • ")
	if len(summary) > 140 {
		summary = summary[:137] + "..."
	}
	return summary
}

func (a *App) renderUpdateCard(c *ui.Context, pal Palette) {
	a.updateMu.Lock()
	status := a.updateStatus
	msg := a.updateMessage
	rel := a.updateRelease
	downloaded := a.updateDownloaded
	total := a.updateTotal
	a.updateMu.Unlock()

	ui.Column(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, i18n.Tf("update.title", version.DisplayVersion())).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
				ui.Text(c, i18n.T("update.auto_check_sub")).FontSize(11).TextColor(pal.TextMuted)
			})
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				if segmentOption(c, pal, i18n.T("update.auto"), a.settingAutoCheckUpdates) {
					a.settingAutoCheckUpdates = true
				}
				if segmentOption(c, pal, i18n.T("update.manual"), !a.settingAutoCheckUpdates) {
					a.settingAutoCheckUpdates = false
				}
				checkLabel := i18n.T("update.check_btn")
				if status == "checking" {
					checkLabel = i18n.T("update.checking_btn")
				}
				if toolbarButton(c, pal, nil, checkLabel, false, status == "checking" || status == "downloading" || status == "applying") {
					a.triggerUpdateCheck()
				}
			})
		})

		if msg != "" {
			msgColor := pal.TextSecondary
			if status == "error" {
				msgColor = pal.Danger
			} else if status == "available" || status == "applying" {
				msgColor = pal.Accent
			}
			ui.Text(c, msg).FontSize(11).TextColor(msgColor)
		}

		if status == "available" && rel != nil {
			if summary := summarizeReleaseNotes(rel.Body); summary != "" {
				ui.Text(c, summary).FontSize(11).TextColor(pal.TextMuted)
			}
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
				ui.Text(c, i18n.T("update.restart_hint")).FontSize(10).TextColor(pal.TextMuted)
				if primaryCTA(c, pal, iconDownload, i18n.T("update.download_install")) {
					a.triggerDownloadAndInstall()
				}
			})
		}

		if status == "downloading" {
			pct := 0.0
			if total > 0 {
				pct = float64(downloaded) / float64(total)
				if pct > 1 {
					pct = 1
				}
			}
			ui.Row(c).Height(6).Radius(3).Background(pal.Border).Children(func() {
				barW := int(pct * 460)
				if barW < 4 && downloaded > 0 {
					barW = 4
				}
				if barW > 0 {
					ui.Row(c).Width(float32(barW)).Height(6).Radius(3).Background(pal.Accent)
				}
			})
			progressLabel := i18n.Tf("update.progress", FormatBytes(downloaded))
			if total > 0 {
				progressLabel = i18n.Tf("update.progress_total", FormatBytes(downloaded), FormatBytes(total), pct*100)
			}
			ui.Text(c, progressLabel).FontSize(10).FontFeatures("tnum").TextColor(pal.TextMuted)
		}
	})
}

func (a *App) saveSettings() {
	dir := strings.TrimSpace(a.settingDownloadDir)
	if dir == "" {
		a.settingsError = i18n.T("settings.err_empty_dir")
		return
	}
	maxC := a.settingMaxConcurrent
	if maxC < 1 || maxC > 20 {
		maxC = 3
	}
	segs := a.settingSegments
	if segs < 1 || segs > 16 {
		segs = 8
	}
	retries, err := strconv.Atoi(strings.TrimSpace(a.settingMaxRetries))
	if err != nil || retries < 0 {
		a.settingsError = i18n.T("settings.err_retries")
		return
	}
	if retries > 20 {
		retries = 20
	}
	speedKBps, err := strconv.ParseInt(strings.TrimSpace(a.settingSpeedLimit), 10, 64)
	if err != nil || speedKBps < 0 {
		a.settingsError = i18n.T("settings.err_speed_limit")
		return
	}
	if speedKBps > 1048576 {
		speedKBps = 1048576
	}

	if a.autostartSetter != nil {
		if err := a.autostartSetter(a.settingLaunchAtStartup); err != nil {
			a.settingsError = i18n.Tf("settings.err_startup", err.Error())
			return
		}
	}

	cfg := a.engine.Config()
	cfg.DownloadDir = dir
	cfg.MaxConcurrentDownloads = maxC
	cfg.DefaultSegments = segs
	cfg.MaxRetries = retries
	cfg.SpeedLimitKBps = speedKBps
	cfg.AutoCategorize = a.settingAutoCategorize
	cfg.LaunchAtStartup = a.settingLaunchAtStartup
	cfg.MinimizeToTray = a.settingMinimizeToTray
	cfg.CloseToTray = a.settingCloseToTray
	cfg.AutoCheckUpdates = a.settingAutoCheckUpdates
	cfg.Language = string(a.language)
	a.engine.UpdateConfig(cfg)

	a.settingMaxRetries = strconv.Itoa(retries)
	a.settingSpeedLimit = strconv.FormatInt(speedKBps, 10)
	a.settingsError = ""
	a.statusMessage = i18n.T("msg.settings_saved")
	a.showSettingsModal = false
}

func (a *App) renderOnboardingModal(c *ui.Context, pal Palette) {
	if !a.showOnboardingModal {
		return
	}

	ui.Modal(c, &a.showOnboardingModal, func() {
		ui.Column(c).Width(540).Gap(16).Children(func() {
			// Header
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(36).Height(36).Radius(9).Background(pal.Accent).Center().Children(func() {
						ui.Icon(c, iconBolt).Width(18).Height(18).TextColor(pal.AccentText)
					})
					ui.Column(c).Gap(2).Children(func() {
						ui.Text(c, i18n.T("onboarding.title")).FontSize(17).FontWeight(700).TextColor(pal.TextPrimary)
						ui.Text(c, i18n.T("onboarding.subtitle")).FontSize(12).TextColor(pal.TextMuted)
					})
				})
				if rowIconButton(c, pal, iconClose, false) {
					a.skipOnboarding()
				}
			})

			ui.Divider(c)

			// Language selector (takes effect immediately)
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("lang.label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					if segmentOption(c, pal, i18n.T("lang.vi"), a.language == i18n.LangVI) {
						a.SetLanguage(i18n.LangVI)
					}
					if segmentOption(c, pal, i18n.T("lang.en"), a.language == i18n.LangEN) {
						a.SetLanguage(i18n.LangEN)
					}
				})
			})

			// Default Download Directory
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("onboarding.download_folder")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.TextInput(c, &a.onboardingDownloadDir).
					Background(pal.InputBg).
					Border(1, pal.Border).
					Radius(7)
			})

			// Auto-Categorize
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("onboarding.auto_categorize")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).Children(func() {
					if segmentOption(c, pal, i18n.T("onboarding.enabled_recommended"), a.onboardingAutoCategorize) {
						a.onboardingAutoCategorize = true
					}
					if segmentOption(c, pal, i18n.T("settings.disabled"), !a.onboardingAutoCategorize) {
						a.onboardingAutoCategorize = false
					}
				})
			})

			// Startup & System Tray
			ui.Row(c).Gap(16).Children(func() {
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, i18n.T("onboarding.launch_windows")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.Row(c).Gap(6).Children(func() {
						if segmentOption(c, pal, i18n.T("settings.enabled"), a.onboardingLaunchAtStartup) {
							a.onboardingLaunchAtStartup = true
						}
						if segmentOption(c, pal, i18n.T("settings.disabled"), !a.onboardingLaunchAtStartup) {
							a.onboardingLaunchAtStartup = false
						}
					})
				})
				ui.Column(c).Gap(6).Grow(1).Children(func() {
					ui.Text(c, i18n.T("onboarding.run_in_tray")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
					ui.Row(c).Gap(6).Children(func() {
						if segmentOption(c, pal, i18n.T("settings.enabled"), a.onboardingRunInTray) {
							a.onboardingRunInTray = true
						}
						if segmentOption(c, pal, i18n.T("settings.disabled"), !a.onboardingRunInTray) {
							a.onboardingRunInTray = false
						}
					})
				})
			})

			// Browser Extension & Native Messaging Auto-Integration
			ui.Column(c).Gap(8).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Column(c).Gap(2).Children(func() {
						ui.Text(c, i18n.T("onboarding.browser_title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
						ui.Text(c, i18n.T("onboarding.browser_sub")).FontSize(11).TextColor(pal.TextMuted)
					})
					if toolbarButton(c, pal, nil, i18n.T("onboarding.install_selected"), false, false) {
						a.integrateSelectedBrowsers(true)
					}
				})

				installedCount := 0
				for _, b := range a.detectedBrowsers {
					if !b.Installed {
						continue
					}
					installedCount++
					checked := a.selectedBrowsers[b.ID]
					row := ui.Row(c).
						Padding(6, 8).
						Radius(6).
						Background(pal.Canvas).
						Border(1, pal.Border).
						AlignItems(ui.Center).
						Justify(ui.SpaceBetween).
						Cursor(ui.CursorPointer).
						Focusable()
					row.Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							boxBg := pal.InputBg
							boxBorder := pal.BorderStrong
							if checked {
								boxBg = pal.Accent
								boxBorder = pal.Accent
							}
							ui.Row(c).Width(16).Height(16).Radius(4).Background(boxBg).Border(1, boxBorder).Center().Children(func() {
								if checked {
									ui.Icon(c, iconCheck).Width(10).Height(10).TextColor(pal.AccentText)
								}
							})
							ui.Text(c, b.Name).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
						})

						statusLabel := i18n.T("onboarding.browser_ready")
						statusColor := pal.TextMuted
						if res, hasRes := a.browserResults[b.ID]; hasRes {
							statusLabel = res.StatusText
							if res.Err != nil {
								statusColor = pal.Danger
							} else {
								statusColor = pal.Success
							}
						} else if b.NativeRegistered {
							statusLabel = i18n.T("onboarding.browser_registered")
							statusColor = pal.Success
						}
						ui.Text(c, statusLabel).FontSize(11).TextColor(statusColor)
					})
					if row.Clicked() {
						a.selectedBrowsers[b.ID] = !checked
					}
				}
				if installedCount == 0 {
					ui.Text(c, i18n.T("onboarding.no_browser")).FontSize(11).TextColor(pal.TextMuted)
				}
				if a.extensionDir != "" {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
						ui.Text(c, i18n.Tf("onboarding.extension_folder", a.extensionDir)).FontSize(10).TextColor(pal.TextMuted).SingleLine()
						if toolbarButton(c, pal, iconClipboard, i18n.T("onboarding.copy_path"), false, false) {
							c.WriteClipboard(a.extensionDir)
							a.nativeRegMsg = i18n.T("onboarding.copied_path")
						}
					})
				}
			})
			if a.nativeRegMsg != "" {
				ui.Text(c, a.nativeRegMsg).FontSize(11).TextColor(pal.Accent)
			}

			if a.onboardingError != "" {
				ui.Row(c).Padding(8, 10).Radius(6).Background(pal.DangerSoft).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Danger)
					ui.Text(c, a.onboardingError).FontSize(12).TextColor(pal.Danger)
				})
			}

			ui.Divider(c)

			ui.Row(c).Gap(8).Children(func() {
				if toolbarButton(c, pal, nil, i18n.T("onboarding.skip"), false, false) {
					a.skipOnboarding()
				}
				ui.Spacer(c)
				if primaryCTA(c, pal, iconCheck, i18n.T("onboarding.finish")) {
					a.completeOnboarding()
				}
			})
		})
	})
}

func (a *App) skipOnboarding() {
	cfg := a.engine.Config()
	cfg.OnboardingCompleted = true
	a.engine.UpdateConfig(cfg)
	a.showOnboardingModal = false
}

func (a *App) completeOnboarding() {
	dir := strings.TrimSpace(a.onboardingDownloadDir)
	if dir == "" {
		a.onboardingError = i18n.T("onboarding.err_empty_dir")
		return
	}
	if a.autostartSetter != nil {
		if err := a.autostartSetter(a.onboardingLaunchAtStartup); err != nil {
			a.onboardingError = i18n.Tf("settings.err_startup", err.Error())
			return
		}
	}
	if len(a.selectedBrowserList()) > 0 {
		a.integrateSelectedBrowsers(true)
	}

	cfg := a.engine.Config()
	cfg.DownloadDir = dir
	cfg.AutoCategorize = a.onboardingAutoCategorize
	cfg.LaunchAtStartup = a.onboardingLaunchAtStartup
	cfg.MinimizeToTray = a.onboardingRunInTray
	cfg.CloseToTray = a.onboardingRunInTray
	cfg.OnboardingCompleted = true
	cfg.Language = string(a.language)
	a.engine.UpdateConfig(cfg)

	a.onboardingError = ""
	a.statusMessage = i18n.T("msg.setup_complete")
	a.showOnboardingModal = false
}

func openInFileManager(targetPath string) {
	go func() {
		if runtime.GOOS == "windows" {
			if _, err := os.Stat(targetPath); err == nil {
				_ = exec.Command("explorer.exe", "/select,"+targetPath).Start()
				return
			}
			_ = exec.Command("explorer.exe", filepath.Dir(targetPath)).Start()
		}
	}()
}

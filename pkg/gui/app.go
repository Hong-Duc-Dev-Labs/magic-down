package gui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/autostart"
	"magicdown/pkg/engine"
	"magicdown/pkg/i18n"
	"magicdown/pkg/ipc"
	"magicdown/pkg/nativemessaging"
	"magicdown/pkg/power"
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

type nativeHandleProvider interface {
	NativeHandle() uintptr
}

type uiThreadDispatcher interface {
	Update(fn func())
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

type settingsTab int

const (
	settingsTabGeneral settingsTab = iota
	settingsTabDownloads
	settingsTabBrowser
	settingsTabUpdates
)

// App holds the UI state for MagicDown's MyGo Native UI desktop application.
type App struct {
	engine    *engine.Engine
	ipcServer *ipc.Server
	winMu     sync.RWMutex
	winCtrl   WindowController
	maximized bool

	// Tray & power-save state
	trayMu       sync.RWMutex
	inTray       bool
	quitting     bool
	trimMu       sync.Mutex
	trimTimer    *time.Timer
	lastTrimAt   time.Time
	trimDelay    time.Duration
	trimCooldown time.Duration
	trimMemoryFn func()

	// Injectable OS hooks (default to real Windows implementations, overridable in tests)
	autostartGetter     func() (bool, error)
	autostartSetter     func(bool) error
	nativeRegFn         func() (string, error)
	nativeStatusFn      func() nativemessaging.RegistrationStatus
	detectBrowsersFn    func() []nativemessaging.BrowserInfo
	integrateBrowsersFn func([]nativemessaging.BrowserInfo, string, string, bool) []nativemessaging.BrowserInstallResult
	bringToForegroundFn func(uintptr) bool
	nativeStatus        nativemessaging.RegistrationStatus
	nativeRegMsg        string

	// Detected Chromium browsers for Welcome Setup & Settings integration
	detectedBrowsers      []nativemessaging.BrowserInfo
	selectedBrowsers      map[string]bool
	browserResults        map[string]nativemessaging.BrowserInstallResult
	browserConnStates     map[string]nativemessaging.ExtensionConnectionState
	troubleshootBrowserID string
	installGuideBrowserID string
	openStoreURLFn        func(nativemessaging.BrowserInfo, string) error
	openExtensionsPageFn  func(nativemessaging.BrowserInfo) error
	openURLFn             func(string) error
	extensionDir          string

	darkMode            bool
	filterStatus        string       // "All", "Downloading", "Completed", "Queued", "Failed"
	filterCategory      FileCategory // CatAll or specific category
	searchQuery         string
	focusSearchNext     bool
	selectedTaskID      string
	pendingDeleteTaskID string
	pendingDeleteName   string

	// Refresh Download Address state
	refreshTargetTaskID       string
	refreshTargetFilename     string
	refreshTargetOriginalHost string
	showRefreshModal          bool
	refreshModalTaskID        string
	refreshModalFilename      string
	refreshModalSourceURL     string
	refreshInputURL           string
	refreshInputReferrer      string
	refreshInputCookies       string
	refreshInputHeaders       map[string]string
	refreshHostWarning        string
	refreshError              string

	// Duplicate Download Detection modal state
	pendingDuplicateTask *engine.Task
	pendingDuplicateReq  *engine.AddTaskRequest

	// Task Details, Live Segment Visualizer & Checksum modal state
	detailsTaskID        string
	detailsSpeedLimit    string
	detailsSpeedLimitErr string
	detailsExpectedHash  string
	checksumMu           sync.Mutex
	checksumTaskID       string
	checksumCancel       context.CancelFunc
	checksumRunning      bool
	checksumHashes       *engine.FileHashes
	checksumErr          string

	// Post-Download Actions & 30s cancelable countdown
	powerCtrl              *power.Controller
	powerMu                sync.RWMutex
	postAction             power.Action
	powerCountdown         *power.Countdown
	powerCountdownDuration time.Duration
	sessionCompletedAny    atomic.Bool

	// Batch URL Pattern Import modal & Opt-In Background Clipboard Monitor state
	showBatchModal     bool
	batchInputText     string
	batchSaveDir       string
	batchError         string
	clipMu             sync.Mutex
	clipStop           chan struct{}
	clipDone           chan struct{}
	clipSeqInitialized bool
	clipLastSeq        uint32
	clipLastText       string
	clipPollInterval   time.Duration
	clipSeqFn          func() (uint32, bool)
	clipReadFn         func() (string, error)

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
	activeSettingsTab       settingsTab
	settingDownloadDir      string
	settingMaxConcurrent    int
	settingSegments         int
	settingMaxRetries       string
	settingSpeedLimit       string
	settingAutoCategorize   bool
	settingLaunchAtStartup  bool
	autostartSyncedValue    bool
	settingMinimizeToTray   bool
	settingCloseToTray      bool
	settingAutoCheckUpdates bool
	settingMonitorClipboard bool
	settingScheduleEnabled  bool
	settingScheduleStart    string
	settingScheduleStop     string
	settingScheduleDays     map[int]bool
	settingScheduleAction   string
	settingProxyMode        string
	settingProxyHost        string
	settingProxyPort        string
	settingProxyUser        string
	settingProxyPass        string
	proxyTestMu             sync.Mutex
	proxyTesting            bool
	proxyTestMsg            string
	proxyTestOK             bool
	testProxyFn             func(context.Context, engine.ProxyConfig) error
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
		autostartGetter:           autostart.IsEnabled,
		autostartSetter:           autostart.SetEnabled,
		trimDelay:                 2 * time.Second,
		trimCooldown:              30 * time.Second,
		trimMemoryFn:              trimProcessMemory,
		clipPollInterval:          750 * time.Millisecond,
		clipSeqFn:                 clipboardSequenceNumber,
		clipReadFn:                readClipboardText,
		nativeRegFn:               func() (string, error) { return nativemessaging.RegisterHost("", nativemessaging.DefaultExtensionID) },
		nativeStatusFn:            nativemessaging.CheckRegistration,
		detectBrowsersFn:          nativemessaging.DetectInstalledBrowsers,
		integrateBrowsersFn:       nativemessaging.IntegrateBrowsers,
		bringToForegroundFn:       bringWindowToForeground,
		selectedBrowsers:          make(map[string]bool),
		browserResults:            make(map[string]nativemessaging.BrowserInstallResult),
		browserConnStates:         make(map[string]nativemessaging.ExtensionConnectionState),
		openStoreURLFn:            nativemessaging.OpenStoreURL,
		openExtensionsPageFn:      nativemessaging.OpenExtensionsPage,
		openURLFn:                 openExternalURL,
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
		settingMonitorClipboard:   cfg.MonitorClipboard,
		settingScheduleEnabled:    cfg.Schedule.Enabled,
		settingScheduleStart:      defaultStr(cfg.Schedule.StartTime, "01:00"),
		settingScheduleStop:       defaultStr(cfg.Schedule.StopTime, "06:00"),
		settingScheduleDays:       daysSliceToMap(cfg.Schedule.DaysOfWeek),
		settingScheduleAction:     defaultStr(cfg.Schedule.StopAction, "none"),
		settingProxyMode:          engine.NormalizeProxyMode(cfg.Proxy.Mode),
		settingProxyHost:          cfg.Proxy.Host,
		settingProxyPort:          formatProxyPort(cfg.Proxy.Port),
		settingProxyUser:          cfg.Proxy.Username,
		settingProxyPass:          cfg.Proxy.Password,
		testProxyFn:               func(ctx context.Context, pc engine.ProxyConfig) error { return engine.TestProxyConnection(ctx, pc, "") },
		powerCtrl:                 power.NewController(),
		postAction:                power.ActionNone,
		powerCountdownDuration:    power.DefaultCountdownDuration,
		showOnboardingModal:       !cfg.OnboardingCompleted,
		onboardingDownloadDir:     cfg.DownloadDir,
		onboardingAutoCategorize:  cfg.AutoCategorize,
		onboardingLaunchAtStartup: cfg.LaunchAtStartup,
		onboardingRunInTray:       cfg.CloseToTray || cfg.MinimizeToTray,
	}
	app.powerCountdown = power.NewCountdown(app.invalidateUI, func(act power.Action) {
		if app.powerCtrl != nil {
			_ = app.powerCtrl.Execute(act)
		}
	})
	app.refreshNativeStatus()
	app.refreshDetectedBrowsers()
	if srv != nil {
		srv.SetOnHandshakeChange(func(string, bool) {
			app.invalidateUI()
		})
	}
	app.syncClipboardMonitor()
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
	a.syncClipboardMonitor()
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

// PrepareHiddenLaunch marks the app as starting hidden in the system tray before
// the window or startup background checks run, avoiding any initial GPU/UI wakeups.
func (a *App) PrepareHiddenLaunch() {
	a.trayMu.Lock()
	a.inTray = true
	a.trayMu.Unlock()
}

// Close stops background UI helpers (clipboard monitor, idle memory trim timer).
func (a *App) Close() {
	a.cancelIdleMemoryTrim()
	a.stopClipboardMonitor()
}

// HideToTray hides the main window and suspends GPU/UI frame scheduling.
func (a *App) HideToTray() {
	a.trayMu.Lock()
	a.inTray = true
	a.trayMu.Unlock()
	a.dispatchWindowAction(func(ctrl WindowController) {
		ctrl.Hide()
	})
	a.scheduleIdleMemoryTrim()
}

// BringToForeground wakes the app if hidden in tray, restores it if minimized,
// raises it above background windows (such as browsers), and invalidates the UI.
// Window API calls are marshalled onto the UI thread when supported.
func (a *App) BringToForeground() {
	a.cancelIdleMemoryTrim()
	a.trayMu.Lock()
	a.inTray = false
	a.trayMu.Unlock()
	fgFn := a.bringToForegroundFn
	a.dispatchWindowAction(func(ctrl WindowController) {
		ctrl.Show()
		ctrl.Restore()
		ctrl.Focus()
		if fgFn != nil {
			var hwnd uintptr
			if hp, ok := ctrl.(nativeHandleProvider); ok {
				hwnd = hp.NativeHandle()
			}
			fgFn(hwnd)
		}
		ctrl.Invalidate()
	})
}

func (a *App) scheduleIdleMemoryTrim() {
	if a.trimMemoryFn == nil {
		return
	}
	if a.engine != nil && !a.engine.IsIdle() {
		return
	}
	a.trimMu.Lock()
	defer a.trimMu.Unlock()

	cooldown := a.trimCooldown
	if cooldown > 0 && !a.lastTrimAt.IsZero() && time.Since(a.lastTrimAt) < cooldown {
		return
	}
	if a.trimTimer != nil {
		a.trimTimer.Stop()
	}
	delay := a.trimDelay
	if delay < 0 {
		delay = 0
	}
	a.trimTimer = time.AfterFunc(delay, func() {
		if !a.IsInTray() {
			return
		}
		if a.engine != nil && !a.engine.IsIdle() {
			return
		}
		a.trimMu.Lock()
		if a.trimCooldown > 0 && !a.lastTrimAt.IsZero() && time.Since(a.lastTrimAt) < a.trimCooldown {
			a.trimMu.Unlock()
			return
		}
		a.lastTrimAt = time.Now()
		fn := a.trimMemoryFn
		a.trimMu.Unlock()
		if fn != nil {
			fn()
		}
	})
}

func (a *App) cancelIdleMemoryTrim() {
	a.trimMu.Lock()
	if a.trimTimer != nil {
		a.trimTimer.Stop()
		a.trimTimer = nil
	}
	a.trimMu.Unlock()
}

// WakeFromTray restores and focuses the main window and resumes UI rendering.
func (a *App) WakeFromTray() {
	a.BringToForeground()
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
	if ctrl := a.windowController(); ctrl != nil {
		ctrl.Minimize()
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
// the Download Info Dialog on the next frame, waking and raising the window to
// the foreground whether it is hidden in tray, minimized, or behind another app.
// Concurrent near-simultaneous arrivals for the same URL are deduplicated.
func (a *App) EnqueueIncomingDownload(req ipc.AddDownloadRequest) {
	a.pendingMu.Lock()
	key := engine.NormalizeDuplicateURL(req.URL)
	duplicateQueued := false
	if key != "" {
		for _, existing := range a.pendingIncoming {
			if engine.NormalizeDuplicateURL(existing.URL) == key {
				duplicateQueued = true
				break
			}
		}
	}
	if !duplicateQueued {
		a.pendingIncoming = append(a.pendingIncoming, req)
	}
	a.pendingMu.Unlock()
	a.cancelPowerCountdown()
	a.BringToForeground()
}

func (a *App) popPendingIncoming() {
	if a.showAddModal || a.showRefreshModal || a.showBatchModal || a.detailsTaskID != "" || a.pendingDeleteTaskID != "" || a.pendingDuplicateTask != nil {
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

	if a.refreshTargetTaskID != "" {
		a.handleCapturedRefreshRequest(req)
		return
	}
	a.openIncomingDownloadDialog(req)
}

func (a *App) openIncomingDownloadDialog(req ipc.AddDownloadRequest) {
	cfg := a.engine.Config()
	rawURL := strings.TrimSpace(req.URL)
	audioURL := strings.TrimSpace(req.AudioURL)
	estimatedSize := req.TotalBytes
	var parsedPlan *engine.DownloadPlan
	if len(req.Plan) > 0 {
		if parsed, err := engine.ParseDownloadPlan(req.Plan, req.URL); err == nil {
			parsedPlan = parsed
			if estimatedSize <= 0 {
				estimatedSize = parsed.KnownSize()
			}
		}
	}

	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		filename = engine.ExtractFilename(rawURL, nil)
		if audioURL != "" {
			filename = engine.ReplaceExtension(filename, "mp4")
		}
	}

	cat := req.Category
	if cat == "" {
		if audioURL != "" {
			cat = engine.CategoryVideo
		} else {
			cat = engine.CategoryForFilename(filename, rawURL)
		}
	}
	saveDir := strings.TrimSpace(req.SaveDir)
	if saveDir == "" {
		saveDir = engine.ResolveSaveDir(cfg.DownloadDir, cat, cfg.AutoCategorize)
	}
	segs := req.NumSegments
	if segs != 4 && segs != 8 && segs != 16 {
		segs = cfg.DefaultSegments
		if segs != 4 && segs != 8 && segs != 16 {
			segs = 8
		}
	}

	candidateSavePath := ""
	if filename != "" {
		candidateSavePath = filepath.Join(saveDir, engine.SanitizeFilename(filename))
	}
	if dup := a.engine.FindDuplicate(rawURL, candidateSavePath); dup != nil {
		autoStart := req.AutoStart || !req.StartPaused
		a.openDuplicateModal(dup, engine.AddTaskRequest{
			URL:           rawURL,
			AudioURL:      audioURL,
			Category:      cat,
			Filename:      filename,
			SaveDir:       saveDir,
			NumSegments:   segs,
			TotalBytes:    estimatedSize,
			Referrer:      req.Referrer,
			Cookies:       req.Cookies,
			Headers:       req.Headers,
			ConvertFormat: req.ConvertFormat,
			TargetFormat:  req.TargetFormat,
			OutputFormat:  req.OutputFormat,
			AudioBitrate:  req.AudioBitrate,
			Plan:          parsedPlan,
			AutoStart:     autoStart,
			StartPaused:   !autoStart,
		})
		a.BringToForeground()
		return
	}

	a.inputFromExtension = true
	a.inputURL = rawURL
	a.inputAudioURL = audioURL
	a.inputReferrer = req.Referrer
	a.inputCookies = req.Cookies
	a.inputHeaders = req.Headers
	a.inputConvertFormat = req.ConvertFormat
	a.inputTargetFormat = req.TargetFormat
	a.inputOutputFormat = req.OutputFormat
	a.inputAudioBitrate = req.AudioBitrate
	a.inputEstimatedSize = estimatedSize
	a.inputPlan = parsedPlan
	a.inputFilename = filename
	a.inputCategory = cat
	a.inputSaveDir = saveDir
	a.inputSegments = segs
	a.lastAutoTarget = a.inputFilename + "|" + a.inputURL
	a.addError = ""
	a.cancelDeleteTask()
	a.cancelDuplicateModal()
	a.closeRefreshModal()
	a.closeTaskDetails()
	a.closeBatchModal()
	a.showSettingsModal = false
	a.showOnboardingModal = false
	a.showAddModal = true
	a.BringToForeground()
}

// SetWindowController attaches the native MyGo window controller for custom title bar buttons.
func (a *App) SetWindowController(ctrl WindowController) {
	a.winMu.Lock()
	a.winCtrl = ctrl
	a.winMu.Unlock()
}

func (a *App) windowController() WindowController {
	a.winMu.RLock()
	ctrl := a.winCtrl
	a.winMu.RUnlock()
	return ctrl
}

// dispatchWindowAction invokes fn on the MyGo main UI thread when the attached
// WindowController supports Update(func()), avoiding off-UI-thread Win32 calls.
func (a *App) dispatchWindowAction(fn func(WindowController)) {
	ctrl := a.windowController()
	if ctrl == nil {
		return
	}
	if dispatcher, ok := ctrl.(uiThreadDispatcher); ok {
		dispatcher.Update(func() {
			fn(ctrl)
		})
		return
	}
	fn(ctrl)
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
	powerActive := false
	if a.powerCountdown != nil {
		powerActive, _, _ = a.powerCountdown.State()
	}
	if a.engine != nil && !a.engine.IsIdle() {
		a.cancelPowerCountdown()
		powerActive = false
	}
	anyModalOpen := a.showAddModal || a.showSettingsModal || a.showOnboardingModal || a.showRefreshModal || a.showBatchModal || a.detailsTaskID != "" || a.pendingDeleteTaskID != "" || a.pendingDuplicateTask != nil || powerActive
	if c.Shortcut(ui.Cmd, ui.KeyN) && !anyModalOpen {
		a.openAddModal()
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) && !anyModalOpen {
		a.openSettingsModal()
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) && !anyModalOpen {
		a.focusSearchNext = true
	}
	if c.Shortcut(0, ui.KeyEscape) {
		a.showAddModal = false
		a.showSettingsModal = false
		a.closeRefreshModal()
		a.closeBatchModal()
		a.closeTaskDetails()
		a.cancelDeleteTask()
		a.cancelDuplicateModal()
		a.cancelPowerCountdown()
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

	if c.Shortcut(0, ui.KeyDelete) && !c.FocusTakesText() && !anyModalOpen && selectedTask != nil {
		a.requestDeleteTask(selectedTask)
	}

	// Root window layout: Integrated Custom Title Bar + (Left Sidebar | Right Main Column)
	ui.Column(c).Fill().Background(pal.Canvas).TextColor(pal.TextPrimary).Children(func() {
		a.renderTitleBar(c, pal)

		ui.Row(c).Fill().Grow(1).AlignItems(ui.Stretch).Children(func() {
			a.renderSidebar(c, pal, statusCounts, catCounts)

			// Main right column: Header + Optional Refresh Banner + List + Status Bar
			ui.Column(c).Fill().Grow(1).Children(func() {
				a.renderHeader(c, pal, selectedTask, completedCount)
				a.renderRefreshWaitingBanner(c, pal)
				a.renderDownloadList(c, pal, tasks)
				a.renderStatusBar(c, pal, statusCounts["Downloading"], statusCounts["Queued"], totalSpeed)
			})
		})
	})

	// Modals
	a.renderAddModal(c, pal)
	a.renderSettingsModal(c, pal)
	a.renderOnboardingModal(c, pal)
	a.renderDeleteConfirmModal(c, pal)
	a.renderRefreshModal(c, pal)
	a.renderDuplicateModal(c, pal)
	a.renderTaskDetailsModal(c, pal)
	a.renderBatchModal(c, pal)
	a.renderPowerCountdownModal(c, pal)
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
						ui.Text(c, version.NormalizedVersion()).FontSize(10).FontWeight(600).TextColor(pal.TextMuted).SingleLine()
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
				ctrl := a.windowController()
				isMax := a.maximized
				if ctrl != nil {
					isMax = ctrl.IsMaximized()
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
				if maxBtn.Clicked() && ctrl != nil {
					ctrl.ToggleMaximize()
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
					if !a.HandleCloseRequest() && ctrl != nil {
						ctrl.Close()
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
	a.cancelDeleteTask()
	a.cancelDuplicateModal()
	a.closeRefreshModal()
	a.closeTaskDetails()
	a.closeBatchModal()
	a.showSettingsModal = false
	a.showOnboardingModal = false
	a.showAddModal = true
}

func formatProxyPort(port int) string {
	if port <= 0 {
		return ""
	}
	return strconv.Itoa(port)
}

func defaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func daysSliceToMap(days []int) map[int]bool {
	m := make(map[int]bool, len(days))
	for _, d := range days {
		if d >= 0 && d <= 6 {
			m[d] = true
		}
	}
	return m
}

func daysMapToSlice(m map[int]bool) []int {
	var out []int
	for d := 0; d <= 6; d++ {
		if m[d] {
			out = append(out, d)
		}
	}
	return out
}

func (a *App) syncLaunchAtStartup(fallback bool) bool {
	if a.autostartGetter == nil {
		return fallback
	}
	enabled, err := a.autostartGetter()
	if err != nil {
		return fallback
	}
	return enabled
}

func (a *App) setLaunchAtStartupImmediate(desired bool) {
	if desired == a.settingLaunchAtStartup {
		return
	}
	if a.autostartSetter != nil {
		if err := a.autostartSetter(desired); err != nil {
			a.settingsError = i18n.Tf("settings.err_startup", err.Error())
			return
		}
	}
	a.settingLaunchAtStartup = desired
	a.autostartSyncedValue = desired
	a.settingsError = ""
	cfg := a.engine.Config()
	cfg.LaunchAtStartup = desired
	a.engine.UpdateConfig(cfg)
	a.syncClipboardMonitor()
	if desired {
		a.statusMessage = i18n.T("msg.startup_enabled")
	} else {
		a.statusMessage = i18n.T("msg.startup_disabled")
	}
}

func (a *App) openSettingsModal() {
	cfg := a.engine.Config()
	a.settingDownloadDir = cfg.DownloadDir
	a.settingMaxConcurrent = cfg.MaxConcurrentDownloads
	a.settingSegments = cfg.DefaultSegments
	a.settingMaxRetries = strconv.Itoa(cfg.MaxRetries)
	a.settingSpeedLimit = strconv.FormatInt(cfg.SpeedLimitKBps, 10)
	a.settingAutoCategorize = cfg.AutoCategorize
	a.settingLaunchAtStartup = a.syncLaunchAtStartup(cfg.LaunchAtStartup)
	a.autostartSyncedValue = a.settingLaunchAtStartup
	a.settingMinimizeToTray = cfg.MinimizeToTray
	a.settingCloseToTray = cfg.CloseToTray
	a.settingAutoCheckUpdates = cfg.AutoCheckUpdates
	a.settingMonitorClipboard = cfg.MonitorClipboard
	a.settingScheduleEnabled = cfg.Schedule.Enabled
	a.settingScheduleStart = defaultStr(cfg.Schedule.StartTime, "01:00")
	a.settingScheduleStop = defaultStr(cfg.Schedule.StopTime, "06:00")
	a.settingScheduleDays = daysSliceToMap(cfg.Schedule.DaysOfWeek)
	a.settingScheduleAction = defaultStr(cfg.Schedule.StopAction, "none")
	a.settingProxyMode = engine.NormalizeProxyMode(cfg.Proxy.Mode)
	a.settingProxyHost = cfg.Proxy.Host
	a.settingProxyPort = formatProxyPort(cfg.Proxy.Port)
	a.settingProxyUser = cfg.Proxy.Username
	a.settingProxyPass = cfg.Proxy.Password
	a.proxyTestMu.Lock()
	a.proxyTestMsg = ""
	a.proxyTestOK = false
	a.proxyTestMu.Unlock()
	a.settingsError = ""
	a.nativeRegMsg = ""
	a.activeSettingsTab = settingsTabGeneral
	a.refreshNativeStatus()
	a.refreshDetectedBrowsers()
	a.cancelDeleteTask()
	a.cancelDuplicateModal()
	a.closeRefreshModal()
	a.closeTaskDetails()
	a.closeBatchModal()
	a.showAddModal = false
	a.showOnboardingModal = false
	a.showSettingsModal = true
}

// OpenOnboardingModal opens the Welcome Setup dialog populated from current config.
func (a *App) OpenOnboardingModal() {
	cfg := a.engine.Config()
	a.onboardingDownloadDir = cfg.DownloadDir
	a.onboardingAutoCategorize = cfg.AutoCategorize
	a.onboardingLaunchAtStartup = a.syncLaunchAtStartup(cfg.LaunchAtStartup)
	a.onboardingRunInTray = cfg.CloseToTray || cfg.MinimizeToTray
	a.onboardingError = ""
	a.nativeRegMsg = ""
	a.refreshNativeStatus()
	a.refreshDetectedBrowsers()
	a.cancelDeleteTask()
	a.cancelDuplicateModal()
	a.closeRefreshModal()
	a.closeTaskDetails()
	a.closeBatchModal()
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
					searchEl := ui.TextInput(c, &a.searchQuery).
						Placeholder(i18n.T("toolbar.search_placeholder")).
						Background(ui.Transparent).
						Border(0, ui.Transparent).
						Padding(6, 0).
						Grow(1)
					if a.focusSearchNext {
						searchEl.Focus()
						a.focusSearchNext = false
					}
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
				a.requestDeleteTask(selected)
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

			// Batch URL Import + Primary CTA
			if toolbarButton(c, pal, iconDownload, i18n.T("toolbar.batch_import"), false, false) {
				a.openBatchModal()
			}
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
					if task.Status == engine.StatusPaused {
						if rowIconButton(c, pal, iconLink, false) {
							a.openRefreshModal(task)
						}
					}
				case engine.StatusFailed, engine.StatusCancelled:
					if rowIconButton(c, pal, iconRefresh, false) {
						_ = a.engine.ResumeTask(task.ID)
						a.statusMessage = i18n.Tf("msg.retrying", name)
					}
					if task.Status == engine.StatusFailed {
						if rowIconButton(c, pal, iconLink, false) {
							a.openRefreshModal(task)
						}
					}
				}

				if rowIconButton(c, pal, iconInfo, false) {
					a.openTaskDetails(task)
				}

				if task.SavePath != "" {
					if rowIconButton(c, pal, iconFolder, false) {
						a.revealTaskInFileManager(task)
						a.statusMessage = i18n.Tf("msg.opened_folder", name)
					}
				}

				if rowIconButton(c, pal, iconTrash, true) {
					a.requestDeleteTask(task)
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

			// Post-download action selector (cycles None -> Sleep -> Shutdown)
			curAct := a.PostDownloadAction()
			actLabel := i18n.T("power.none")
			actColor := pal.TextMuted
			actBg := ui.Transparent
			switch curAct {
			case power.ActionSleep:
				actLabel = i18n.T("power.sleep")
				actColor = pal.Warning
				actBg = pal.WarningSoft
			case power.ActionShutdown:
				actLabel = i18n.T("power.shutdown")
				actColor = pal.Danger
				actBg = pal.DangerSoft
			}
			powerBtn := ui.Row(c).
				Padding(3, 8).
				Gap(5).
				Radius(999).
				Background(actBg).
				AlignItems(ui.Center).
				Shrink(0).
				Cursor(ui.CursorPointer).
				Focusable()
			powerBtn.Children(func() {
				ui.Icon(c, iconPower).Width(12).Height(12).TextColor(actColor).Shrink(0)
				ui.Text(c, i18n.T("power.after_done")+" "+actLabel).FontSize(11).FontWeight(600).TextColor(actColor).SingleLine()
			})
			if powerBtn.Clicked() {
				switch curAct {
				case power.ActionNone:
					a.SetPostDownloadAction(power.ActionSleep)
				case power.ActionSleep:
					a.SetPostDownloadAction(power.ActionShutdown)
				default:
					a.SetPostDownloadAction(power.ActionNone)
				}
			}

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

	addReq := engine.AddTaskRequest{
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
	}

	task, dup, err := a.engine.AddTaskOrFindDuplicate(addReq)
	if err != nil {
		a.addError = err.Error()
		return
	}
	if dup != nil {
		a.showAddModal = false
		a.openDuplicateModal(dup, addReq)
		return
	}

	a.cancelPowerCountdown()
	a.selectedTaskID = task.ID
	a.statusMessage = i18n.Tf("msg.added_download", task.URL)
	a.showAddModal = false
}

func (a *App) renderSettingsModal(c *ui.Context, pal Palette) {
	if !a.showSettingsModal {
		return
	}

	_, winH := c.Size()
	modalH := float32(520)
	if winH > 0 {
		if maxAvail := winH - 100; maxAvail < modalH {
			modalH = maxAvail
		}
		if modalH < 340 {
			modalH = 340
		}
	}

	ui.Modal(c, &a.showSettingsModal, func() {
		ui.Column(c).Width(560).Height(modalH).Clip().Gap(12).Children(func() {
			// Fixed Header
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.AccentSoft).Center().Shrink(0).Children(func() {
						ui.Icon(c, iconSettings).Width(16).Height(16).TextColor(pal.Accent)
					})
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, i18n.T("settings.title")).FontSize(16).FontWeight(700).TextColor(pal.TextPrimary).SingleLine()
						ui.Text(c, i18n.T("settings.subtitle")).FontSize(12).TextColor(pal.TextMuted).SingleLine()
					})
				})
				if rowIconButton(c, pal, iconClose, false) {
					a.showSettingsModal = false
				}
			})

			// Fixed Horizontal Tab Bar
			ui.Row(c).Shrink(0).Padding(4).Gap(4).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Children(func() {
				tabs := []struct {
					tab   settingsTab
					label string
				}{
					{settingsTabGeneral, i18n.T("settings.tab_general")},
					{settingsTabDownloads, i18n.T("settings.tab_downloads")},
					{settingsTabBrowser, i18n.T("settings.tab_browser")},
					{settingsTabUpdates, i18n.T("settings.tab_updates")},
				}
				for _, item := range tabs {
					if settingsTabButton(c, pal, item.label, a.activeSettingsTab == item.tab) {
						a.activeSettingsTab = item.tab
						if item.tab == settingsTabBrowser {
							a.refreshNativeStatus()
							a.refreshDetectedBrowsers()
						}
					}
				}
			})

			ui.Divider(c)

			// Scrollable Tab Content Area (clipped within modal viewport)
			ui.Scroll(c).Fill().Grow(1).Clip().Children(func() {
				ui.Column(c).Fill().Gap(14).Padding(2, 6, 4, 0).Children(func() {
					switch a.activeSettingsTab {
					case settingsTabDownloads:
						a.renderSettingsDownloadsTab(c, pal)
					case settingsTabBrowser:
						a.renderSettingsBrowserTab(c, pal)
					case settingsTabUpdates:
						a.renderSettingsUpdatesTab(c, pal)
					default:
						a.renderSettingsGeneralTab(c, pal)
					}
				})
			})

			if a.settingsError != "" {
				ui.Row(c).Shrink(0).Padding(8, 10).Radius(6).Background(pal.DangerSoft).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Danger).Shrink(0)
					ui.Text(c, a.settingsError).FontSize(12).TextColor(pal.Danger)
				})
			}

			ui.Divider(c)

			// Fixed Footer
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
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

func (a *App) renderSettingsGeneralTab(c *ui.Context, pal Palette) {
	// Language & Appearance (short labels fit comfortably in 2 columns)
	ui.Row(c).Gap(16).Children(func() {
		ui.Column(c).Gap(6).Grow(1).Children(func() {
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
		ui.Column(c).Gap(6).Grow(1).Children(func() {
			ui.Text(c, i18n.T("settings.appearance")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
			ui.Row(c).Gap(8).Children(func() {
				if segmentOption(c, pal, i18n.T("settings.dark"), a.darkMode) {
					a.darkMode = true
				}
				if segmentOption(c, pal, i18n.T("settings.light"), !a.darkMode) {
					a.darkMode = false
				}
			})
		})
	})

	// Launch at Windows Startup
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, i18n.T("settings.launch_startup")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
		ui.Text(c, i18n.T("settings.launch_startup_hint")).FontSize(11).TextColor(pal.TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			if segmentOption(c, pal, i18n.T("settings.enabled"), a.settingLaunchAtStartup) {
				a.setLaunchAtStartupImmediate(true)
			}
			if segmentOption(c, pal, i18n.T("settings.disabled"), !a.settingLaunchAtStartup) {
				a.setLaunchAtStartupImmediate(false)
			}
		})
	})

	// Close Button (X) Behavior (full-width row so labels never truncate)
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, i18n.T("settings.close_behavior")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
		ui.Row(c).Gap(8).Children(func() {
			if segmentOption(c, pal, i18n.T("settings.hide_to_tray"), a.settingCloseToTray) {
				a.settingCloseToTray = true
			}
			if segmentOption(c, pal, i18n.T("settings.exit_app"), !a.settingCloseToTray) {
				a.settingCloseToTray = false
			}
		})
	})

	// Minimize Button Behavior (full-width row so labels never truncate)
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, i18n.T("settings.minimize_behavior")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
		ui.Row(c).Gap(8).Children(func() {
			if segmentOption(c, pal, i18n.T("settings.hide_to_tray"), a.settingMinimizeToTray) {
				a.settingMinimizeToTray = true
			}
			if segmentOption(c, pal, i18n.T("settings.taskbar"), !a.settingMinimizeToTray) {
				a.settingMinimizeToTray = false
			}
		})
	})

	// Welcome Setup quick re-launch card
	ui.Row(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, i18n.T("settings.welcome_setup")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
			ui.Text(c, i18n.T("settings.welcome_setup_sub")).FontSize(11).TextColor(pal.TextMuted)
		})
		if toolbarButton(c, pal, nil, i18n.T("settings.welcome_setup"), false, false) {
			a.OpenOnboardingModal()
		}
	})
}

func (a *App) renderSettingsDownloadsTab(c *ui.Context, pal Palette) {
	// Download Directory
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, i18n.T("settings.download_dir")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
		ui.TextInput(c, &a.settingDownloadDir).
			Background(pal.InputBg).
			Border(1, pal.Border).
			Radius(7)
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

	// Network Proxy Configuration
	a.renderSettingsProxySection(c, pal)

	// Queue Scheduler & Clipboard Monitor
	a.renderSettingsSchedulerAndClipboard(c, pal)
}

func (a *App) renderSettingsSchedulerAndClipboard(c *ui.Context, pal Palette) {
	// Queue Scheduler Card
	ui.Column(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, i18n.T("sched.title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
				ui.Text(c, i18n.T("sched.subtitle")).FontSize(11).TextColor(pal.TextMuted)
			})
			ui.Row(c).Gap(6).Children(func() {
				if segmentOption(c, pal, i18n.T("settings.enabled"), a.settingScheduleEnabled) {
					a.settingScheduleEnabled = true
				}
				if segmentOption(c, pal, i18n.T("settings.disabled"), !a.settingScheduleEnabled) {
					a.settingScheduleEnabled = false
				}
			})
		})

		if a.settingScheduleEnabled {
			ui.Row(c).Gap(10).Children(func() {
				ui.Column(c).Gap(4).Grow(1).Children(func() {
					ui.Text(c, i18n.T("sched.start_time")).FontSize(11).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingScheduleStart).Placeholder("01:00").Background(pal.Card).Border(1, pal.Border).Radius(7)
				})
				ui.Column(c).Gap(4).Grow(1).Children(func() {
					ui.Text(c, i18n.T("sched.stop_time")).FontSize(11).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingScheduleStop).Placeholder("06:00").Background(pal.Card).Border(1, pal.Border).Radius(7)
				})
			})

			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, i18n.T("sched.days_label")).FontSize(11).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(4).Children(func() {
					dayKeys := []string{"sched.day_0", "sched.day_1", "sched.day_2", "sched.day_3", "sched.day_4", "sched.day_5", "sched.day_6"}
					if a.settingScheduleDays == nil {
						a.settingScheduleDays = make(map[int]bool)
					}
					for idx, key := range dayKeys {
						if segmentOption(c, pal, i18n.T(key), a.settingScheduleDays[idx]) {
							a.settingScheduleDays[idx] = !a.settingScheduleDays[idx]
						}
					}
				})
			})

			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, i18n.T("sched.stop_action")).FontSize(11).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(6).Children(func() {
					actions := []struct {
						val   string
						label string
					}{
						{"none", i18n.T("power.none")},
						{"sleep", i18n.T("power.sleep")},
						{"shutdown", i18n.T("power.shutdown")},
					}
					for _, act := range actions {
						if segmentOption(c, pal, act.label, a.settingScheduleAction == act.val) {
							a.settingScheduleAction = act.val
						}
					}
				})
			})
		}
	})

	// Clipboard Monitor (Opt-in, default false)
	ui.Row(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, i18n.T("clip.title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
			ui.Text(c, i18n.T("clip.subtitle")).FontSize(11).TextColor(pal.TextMuted)
		})
		ui.Row(c).Gap(6).Children(func() {
			if segmentOption(c, pal, i18n.T("settings.enabled"), a.settingMonitorClipboard) {
				a.settingMonitorClipboard = true
			}
			if segmentOption(c, pal, i18n.T("settings.disabled"), !a.settingMonitorClipboard) {
				a.settingMonitorClipboard = false
			}
		})
	})
}

func (a *App) buildSettingsProxyConfig() (engine.ProxyConfig, error) {
	mode := engine.NormalizeProxyMode(a.settingProxyMode)
	pc := engine.ProxyConfig{
		Mode:     mode,
		Host:     strings.TrimSpace(a.settingProxyHost),
		Username: strings.TrimSpace(a.settingProxyUser),
		Password: a.settingProxyPass,
	}
	if mode == engine.ProxyModeHTTP || mode == engine.ProxyModeSOCKS5 {
		if pc.Host == "" {
			return pc, fmt.Errorf("%s", i18n.T("settings.err_proxy_host"))
		}
		port, err := strconv.Atoi(strings.TrimSpace(a.settingProxyPort))
		if err != nil || port < 1 || port > 65535 {
			return pc, fmt.Errorf("%s", i18n.T("settings.err_proxy_port"))
		}
		pc.Port = port
	}
	return pc, nil
}

func (a *App) triggerProxyTest() {
	pc, err := a.buildSettingsProxyConfig()
	if err != nil {
		a.proxyTestMu.Lock()
		a.proxyTestOK = false
		a.proxyTestMsg = err.Error()
		a.proxyTestMu.Unlock()
		return
	}
	a.proxyTestMu.Lock()
	if a.proxyTesting {
		a.proxyTestMu.Unlock()
		return
	}
	a.proxyTesting = true
	a.proxyTestMsg = i18n.T("settings.proxy_testing")
	a.proxyTestOK = false
	a.proxyTestMu.Unlock()
	a.invalidateUI()

	testFn := a.testProxyFn
	if testFn == nil {
		testFn = func(ctx context.Context, cfg engine.ProxyConfig) error {
			return engine.TestProxyConnection(ctx, cfg, "")
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := testFn(ctx, pc)
		a.proxyTestMu.Lock()
		a.proxyTesting = false
		if err != nil {
			a.proxyTestOK = false
			a.proxyTestMsg = i18n.Tf("settings.proxy_err", err.Error())
		} else {
			a.proxyTestOK = true
			a.proxyTestMsg = i18n.T("settings.proxy_ok")
		}
		a.proxyTestMu.Unlock()
		a.invalidateUI()
	}()
}

func (a *App) renderSettingsProxySection(c *ui.Context, pal Palette) {
	a.proxyTestMu.Lock()
	testing := a.proxyTesting
	testMsg := a.proxyTestMsg
	testOK := a.proxyTestOK
	a.proxyTestMu.Unlock()

	ui.Column(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(c, i18n.T("settings.proxy_title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
			testLabel := i18n.T("settings.proxy_test")
			if testing {
				testLabel = i18n.T("settings.proxy_testing")
			}
			if toolbarButton(c, pal, iconRefresh, testLabel, testing, false) {
				a.triggerProxyTest()
			}
		})

		ui.Row(c).Gap(6).Children(func() {
			modes := []struct {
				mode  string
				label string
			}{
				{engine.ProxyModeSystem, i18n.T("settings.proxy_system")},
				{engine.ProxyModeNone, i18n.T("settings.proxy_none")},
				{engine.ProxyModeHTTP, i18n.T("settings.proxy_http")},
				{engine.ProxyModeSOCKS5, i18n.T("settings.proxy_socks5")},
			}
			for _, m := range modes {
				if segmentOption(c, pal, m.label, engine.NormalizeProxyMode(a.settingProxyMode) == m.mode) {
					a.settingProxyMode = m.mode
				}
			}
		})

		curMode := engine.NormalizeProxyMode(a.settingProxyMode)
		if curMode == engine.ProxyModeHTTP || curMode == engine.ProxyModeSOCKS5 {
			ui.Row(c).Gap(10).Children(func() {
				ui.Column(c).Gap(4).Grow(1).Children(func() {
					ui.Text(c, i18n.T("settings.proxy_host")).FontSize(11).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingProxyHost).Placeholder("127.0.0.1").Background(pal.Card).Border(1, pal.Border).Radius(7)
				})
				ui.Column(c).Gap(4).Width(100).Children(func() {
					ui.Text(c, i18n.T("settings.proxy_port")).FontSize(11).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingProxyPort).Placeholder("8080").Background(pal.Card).Border(1, pal.Border).Radius(7)
				})
			})
			ui.Row(c).Gap(10).Children(func() {
				ui.Column(c).Gap(4).Grow(1).Children(func() {
					ui.Text(c, i18n.T("settings.proxy_user")).FontSize(11).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingProxyUser).Background(pal.Card).Border(1, pal.Border).Radius(7)
				})
				ui.Column(c).Gap(4).Grow(1).Children(func() {
					ui.Text(c, i18n.T("settings.proxy_pass")).FontSize(11).FontWeight(600).TextColor(pal.TextSecondary)
					ui.TextInput(c, &a.settingProxyPass).Background(pal.Card).Border(1, pal.Border).Radius(7)
				})
			})
		}

		if testMsg != "" {
			msgColor := pal.Danger
			if testing {
				msgColor = pal.TextSecondary
			} else if testOK {
				msgColor = pal.Success
			}
			ui.Text(c, testMsg).FontSize(11).TextColor(msgColor)
		}
	})
}

func (a *App) renderSettingsBrowserTab(c *ui.Context, pal Palette) {
	// Native Messaging & IPC Status Info Card
	ipcPort := ipc.DefaultPort
	if a.ipcServer != nil {
		ipcPort = a.ipcServer.Port()
	}
	ui.Row(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(10).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, i18n.T("settings.native_title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
			regState := i18n.T("settings.native_not_registered")
			if a.nativeStatus.IsReady() {
				regState = i18n.T("settings.native_registered")
			}
			ui.Text(c, fmt.Sprintf("%s  •  IPC 127.0.0.1:%d", regState, ipcPort)).FontSize(11).FontFeatures("tnum").TextColor(pal.TextMuted)
		})
		regLabel := i18n.T("settings.native_register")
		if a.nativeStatus.IsReady() {
			regLabel = i18n.T("settings.native_reregister")
		}
		if toolbarButton(c, pal, nil, regLabel, false, false) {
			a.registerNativeMessaging()
		}
	})
	if a.nativeRegMsg != "" {
		ui.Text(c, a.nativeRegMsg).FontSize(11).TextColor(pal.Accent)
	}

	// Browser Integration card (Chrome / Edge / Dia / Chromium)
	a.renderBrowserIntegrationSection(c, pal)
}

func (a *App) renderSettingsUpdatesTab(c *ui.Context, pal Palette) {
	// Auto-check on startup toggle
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, i18n.T("update.auto_check_sub")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
		ui.Row(c).Gap(8).Children(func() {
			if segmentOption(c, pal, i18n.T("update.auto"), a.settingAutoCheckUpdates) {
				a.settingAutoCheckUpdates = true
			}
			if segmentOption(c, pal, i18n.T("update.manual"), !a.settingAutoCheckUpdates) {
				a.settingAutoCheckUpdates = false
			}
		})
	})

	// Software Updates status & action card
	a.renderUpdateCard(c, pal)
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
	if a.IsInTray() {
		return
	}
	a.dispatchWindowAction(func(ctrl WindowController) {
		ctrl.Invalidate()
	})
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

	ui.Column(c).Padding(12, 14).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, i18n.Tf("update.title", version.DisplayVersion())).FontSize(13).FontWeight(600).TextColor(pal.TextPrimary)
				if msg != "" {
					msgColor := pal.TextSecondary
					if status == "error" {
						msgColor = pal.Danger
					} else if status == "available" || status == "applying" {
						msgColor = pal.Accent
					}
					ui.Text(c, msg).FontSize(11).TextColor(msgColor)
				}
			})
			checkLabel := i18n.T("update.check_btn")
			if status == "checking" {
				checkLabel = i18n.T("update.checking_btn")
			}
			if toolbarButton(c, pal, iconRefresh, checkLabel, status == "checking" || status == "downloading" || status == "applying", false) {
				a.triggerUpdateCheck()
			}
		})

		if status == "available" && rel != nil {
			if summary := summarizeReleaseNotes(rel.Body); summary != "" {
				ui.Text(c, summary).FontSize(11).TextColor(pal.TextMuted)
			}
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Children(func() {
				ui.Text(c, i18n.T("update.restart_hint")).FontSize(11).TextColor(pal.TextMuted)
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
			ui.Progress(c, pct).
				Height(6).
				Radius(999).
				Background(pal.TrackBg).
				TextColor(pal.Accent)
			progressLabel := i18n.Tf("update.progress", FormatBytes(downloaded))
			if total > 0 {
				progressLabel = i18n.Tf("update.progress_total", FormatBytes(downloaded), FormatBytes(total), pct*100)
			}
			ui.Text(c, progressLabel).FontSize(11).FontFeatures("tnum").TextColor(pal.TextMuted)
		}
	})
}

func (a *App) saveSettings() {
	dir := strings.TrimSpace(a.settingDownloadDir)
	if dir == "" {
		a.activeSettingsTab = settingsTabDownloads
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
		a.activeSettingsTab = settingsTabDownloads
		a.settingsError = i18n.T("settings.err_retries")
		return
	}
	if retries > 20 {
		retries = 20
	}
	speedKBps, err := strconv.ParseInt(strings.TrimSpace(a.settingSpeedLimit), 10, 64)
	if err != nil || speedKBps < 0 {
		a.activeSettingsTab = settingsTabDownloads
		a.settingsError = i18n.T("settings.err_speed_limit")
		return
	}
	if speedKBps > 1048576 {
		speedKBps = 1048576
	}

	proxyCfg, err := a.buildSettingsProxyConfig()
	if err != nil {
		a.activeSettingsTab = settingsTabDownloads
		a.settingsError = err.Error()
		return
	}

	schedCfg := engine.ScheduleConfig{
		Enabled:    a.settingScheduleEnabled,
		StartTime:  strings.TrimSpace(a.settingScheduleStart),
		StopTime:   strings.TrimSpace(a.settingScheduleStop),
		DaysOfWeek: daysMapToSlice(a.settingScheduleDays),
		StopAction: defaultStr(a.settingScheduleAction, "none"),
	}
	if err := schedCfg.Validate(); err != nil {
		a.activeSettingsTab = settingsTabDownloads
		a.settingsError = i18n.Tf("sched.err_invalid", err.Error())
		return
	}

	if a.autostartSetter != nil && a.settingLaunchAtStartup != a.autostartSyncedValue {
		if err := a.autostartSetter(a.settingLaunchAtStartup); err != nil {
			a.activeSettingsTab = settingsTabGeneral
			a.settingsError = i18n.Tf("settings.err_startup", err.Error())
			return
		}
		a.autostartSyncedValue = a.settingLaunchAtStartup
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
	cfg.MonitorClipboard = a.settingMonitorClipboard
	cfg.Language = string(a.language)
	cfg.Proxy = proxyCfg
	cfg.Schedule = schedCfg
	a.engine.UpdateConfig(cfg)
	a.syncClipboardMonitor()

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

			// Browser Integration (Store-based, real status)
			a.renderBrowserIntegrationSection(c, pal)
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
	a.syncClipboardMonitor()
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
	a.syncClipboardMonitor()

	a.onboardingError = ""
	a.statusMessage = i18n.T("msg.setup_complete")
	a.showOnboardingModal = false
}

func (a *App) requestDeleteTask(task *engine.Task) {
	if task == nil {
		return
	}
	name := task.Filename
	if name == "" {
		name = task.URL
	}
	a.cancelDuplicateModal()
	a.closeRefreshModal()
	a.closeTaskDetails()
	a.closeBatchModal()
	a.showAddModal = false
	a.showSettingsModal = false
	a.showOnboardingModal = false
	a.pendingDeleteTaskID = task.ID
	a.pendingDeleteName = name
}

func (a *App) cancelDeleteTask() {
	a.pendingDeleteTaskID = ""
	a.pendingDeleteName = ""
}

func (a *App) confirmDeleteTask() {
	if a.pendingDeleteTaskID == "" {
		return
	}
	id := a.pendingDeleteTaskID
	name := a.pendingDeleteName
	a.cancelDeleteTask()

	if err := a.engine.DeleteTask(id, false); err != nil {
		a.statusMessage = err.Error()
		return
	}
	if a.selectedTaskID == id {
		a.selectedTaskID = ""
	}
	a.statusMessage = i18n.Tf("msg.removed", name)
}

func (a *App) renderDeleteConfirmModal(c *ui.Context, pal Palette) {
	if a.pendingDeleteTaskID == "" {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(420).Gap(16).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.DangerSoft).Center().Shrink(0).Children(func() {
					ui.Icon(c, iconTrash).Width(16).Height(16).TextColor(pal.Danger)
				})
				ui.Text(c, i18n.T("delete.title")).FontSize(15).FontWeight(700).TextColor(pal.TextPrimary)
			})

			ui.Text(c, i18n.Tf("delete.body", a.pendingDeleteName)).FontSize(12).TextColor(pal.TextSecondary)

			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if toolbarButton(c, pal, nil, i18n.T("delete.cancel"), false, false) {
					a.cancelDeleteTask()
				}
				if toolbarButton(c, pal, iconTrash, i18n.T("delete.confirm"), false, true) {
					a.confirmDeleteTask()
				}
			})
		})
	})
	if !open {
		a.cancelDeleteTask()
	}
}

// --- Refresh Download Address ---

func (a *App) openRefreshModal(task *engine.Task) {
	if task == nil {
		return
	}
	name := task.Filename
	if name == "" {
		name = extractHost(task.URL)
	}
	sourceURL := strings.TrimSpace(task.Referrer)
	if sourceURL == "" {
		sourceURL = task.URL
	}
	a.cancelDeleteTask()
	a.cancelDuplicateModal()
	a.closeTaskDetails()
	a.closeBatchModal()
	a.showAddModal = false
	a.showSettingsModal = false
	a.showOnboardingModal = false
	a.refreshModalTaskID = task.ID
	a.refreshModalFilename = name
	a.refreshModalSourceURL = sourceURL
	a.refreshInputURL = ""
	a.refreshInputReferrer = ""
	a.refreshInputCookies = ""
	a.refreshInputHeaders = nil
	a.refreshHostWarning = ""
	a.refreshError = ""
	a.showRefreshModal = true
}

func (a *App) closeRefreshModal() {
	a.showRefreshModal = false
	a.refreshError = ""
	a.refreshHostWarning = ""
}

func (a *App) startBrowserRefreshCapture() {
	if a.refreshModalTaskID == "" {
		return
	}
	task, err := a.engine.GetTask(a.refreshModalTaskID)
	if err != nil || task == nil {
		a.refreshError = i18n.T("refresh.err_invalid_url")
		return
	}
	name := task.Filename
	if name == "" {
		name = extractHost(task.URL)
	}
	sourceURL := strings.TrimSpace(task.Referrer)
	if sourceURL == "" {
		sourceURL = task.URL
	}
	a.refreshTargetTaskID = task.ID
	a.refreshTargetFilename = name
	a.refreshTargetOriginalHost = extractHost(task.URL)
	a.showRefreshModal = false
	a.statusMessage = i18n.Tf("msg.refresh_waiting", name)

	if a.openURLFn != nil && sourceURL != "" {
		go func(u string) {
			_ = a.openURLFn(u)
		}(sourceURL)
	}
}

func (a *App) cancelRefreshCapture() {
	a.refreshTargetTaskID = ""
	a.refreshTargetFilename = ""
	a.refreshTargetOriginalHost = ""
}

func (a *App) handleCapturedRefreshRequest(req ipc.AddDownloadRequest) {
	targetID := a.refreshTargetTaskID
	task, err := a.engine.GetTask(targetID)
	if err != nil || task == nil {
		a.cancelRefreshCapture()
		a.openIncomingDownloadDialog(req)
		return
	}

	newURL := strings.TrimSpace(req.URL)
	origHost := a.refreshTargetOriginalHost
	newHost := extractHost(newURL)

	if origHost != "" && newHost != "" && !strings.EqualFold(origHost, newHost) {
		a.openRefreshModal(task)
		a.refreshInputURL = newURL
		a.refreshInputReferrer = req.Referrer
		a.refreshInputCookies = req.Cookies
		a.refreshInputHeaders = req.Headers
		a.refreshHostWarning = i18n.Tf("refresh.host_mismatch", origHost, newHost)
		a.BringToForeground()
		return
	}

	if err := a.engine.RefreshTaskAddress(targetID, engine.RefreshAddressRequest{
		URL:      newURL,
		Referrer: req.Referrer,
		Cookies:  req.Cookies,
		Headers:  req.Headers,
	}); err != nil {
		a.openRefreshModal(task)
		a.refreshInputURL = newURL
		a.refreshInputReferrer = req.Referrer
		a.refreshInputCookies = req.Cookies
		a.refreshInputHeaders = req.Headers
		a.refreshError = err.Error()
		a.BringToForeground()
		return
	}

	name := a.refreshTargetFilename
	a.cancelRefreshCapture()
	a.closeRefreshModal()
	a.selectedTaskID = targetID
	a.statusMessage = i18n.Tf("msg.refresh_applied", name)
	a.BringToForeground()
}

func (a *App) submitRefreshURL() {
	rawURL := strings.TrimSpace(a.refreshInputURL)
	if rawURL == "" || (!strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://")) {
		a.refreshError = i18n.T("refresh.err_invalid_url")
		return
	}
	targetID := a.refreshModalTaskID
	if targetID == "" {
		a.closeRefreshModal()
		return
	}
	if err := a.engine.RefreshTaskAddress(targetID, engine.RefreshAddressRequest{
		URL:      rawURL,
		Referrer: a.refreshInputReferrer,
		Cookies:  a.refreshInputCookies,
		Headers:  a.refreshInputHeaders,
	}); err != nil {
		a.refreshError = err.Error()
		return
	}
	name := a.refreshModalFilename
	if a.refreshTargetTaskID == targetID {
		a.cancelRefreshCapture()
	}
	a.closeRefreshModal()
	a.selectedTaskID = targetID
	a.statusMessage = i18n.Tf("msg.refresh_applied", name)
}

func (a *App) renderRefreshWaitingBanner(c *ui.Context, pal Palette) {
	if a.refreshTargetTaskID == "" {
		return
	}
	ui.Row(c).
		Shrink(0).
		Padding(10, 20).
		Gap(10).
		Background(pal.AccentSoft).
		BorderWidth(0, 0, 1, 0).
		BorderColor(pal.Accent).
		AlignItems(ui.Center).
		Children(func() {
			ui.Icon(c, iconLink).Width(15).Height(15).TextColor(pal.Accent).Shrink(0)
			ui.Text(c, i18n.Tf("refresh.waiting_banner", a.refreshTargetFilename)).
				FontSize(12).
				FontWeight(600).
				TextColor(pal.TextPrimary).
				Grow(1).
				SingleLine().
				Ellipsis("...")
			if toolbarButton(c, pal, iconClipboard, i18n.T("refresh.btn_paste_url"), false, false) {
				if task, err := a.engine.GetTask(a.refreshTargetTaskID); err == nil && task != nil {
					a.openRefreshModal(task)
				}
			}
			if toolbarButton(c, pal, iconClose, i18n.T("refresh.cancel"), false, false) {
				a.cancelRefreshCapture()
				a.statusMessage = i18n.T("msg.refresh_cancelled")
			}
		})
}

func (a *App) renderRefreshModal(c *ui.Context, pal Palette) {
	if !a.showRefreshModal {
		return
	}
	ui.Modal(c, &a.showRefreshModal, func() {
		ui.Column(c).Width(500).Gap(16).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.AccentSoft).Center().Shrink(0).Children(func() {
						ui.Icon(c, iconLink).Width(16).Height(16).TextColor(pal.Accent)
					})
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, i18n.T("refresh.title")).FontSize(16).FontWeight(700).TextColor(pal.TextPrimary)
						ui.Text(c, i18n.T("refresh.subtitle")).FontSize(12).TextColor(pal.TextMuted)
					})
				})
				if rowIconButton(c, pal, iconClose, false) {
					a.closeRefreshModal()
				}
			})

			ui.Divider(c)

			// Option 1: Open Source Page in Browser to Capture
			ui.Row(c).Padding(10, 12).Radius(8).Background(pal.InputBg).Border(1, pal.Border).Gap(12).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, i18n.T("refresh.capture_title")).FontSize(12).FontWeight(600).TextColor(pal.TextPrimary)
					ui.Text(c, i18n.T("refresh.capture_sub")).FontSize(11).TextColor(pal.TextMuted)
				})
				if toolbarButton(c, pal, iconRefresh, i18n.T("refresh.open_source"), false, false) {
					a.startBrowserRefreshCapture()
				}
			})

			// Option 2: Paste New Direct URL
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, i18n.T("refresh.paste_label")).FontSize(12).FontWeight(600).TextColor(pal.TextSecondary)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c, &a.refreshInputURL).
						Placeholder(i18n.T("refresh.url_placeholder")).
						Background(pal.InputBg).
						Border(1, pal.Border).
						Radius(7).
						Grow(1)
					if toolbarButton(c, pal, iconClipboard, i18n.T("add.paste"), false, false) {
						if clip := strings.TrimSpace(c.ReadClipboard()); clip != "" {
							a.refreshInputURL = clip
							a.refreshError = ""
						}
					}
				})
			})

			if a.refreshHostWarning != "" {
				ui.Row(c).Padding(8, 10).Radius(6).Background(pal.WarningSoft).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Warning).Shrink(0)
					ui.Text(c, a.refreshHostWarning).FontSize(12).TextColor(pal.Warning)
				})
			}

			if a.refreshError != "" {
				ui.Row(c).Padding(8, 10).Radius(6).Background(pal.DangerSoft).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).Width(14).Height(14).TextColor(pal.Danger).Shrink(0)
					ui.Text(c, a.refreshError).FontSize(12).TextColor(pal.Danger)
				})
			}

			ui.Divider(c)

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c)
				if toolbarButton(c, pal, nil, i18n.T("refresh.cancel"), false, false) {
					a.closeRefreshModal()
				}
				if primaryCTA(c, pal, iconPlay, i18n.T("refresh.apply")) {
					a.submitRefreshURL()
				}
			})
		})
	})
}

// --- Duplicate Download Detection ---

func (a *App) openDuplicateModal(existing *engine.Task, req engine.AddTaskRequest) {
	if existing == nil {
		return
	}
	reqCopy := req
	a.cancelDeleteTask()
	a.closeRefreshModal()
	a.closeTaskDetails()
	a.closeBatchModal()
	a.showAddModal = false
	a.showSettingsModal = false
	a.showOnboardingModal = false
	a.pendingDuplicateTask = existing
	a.pendingDuplicateReq = &reqCopy
}

func (a *App) cancelDuplicateModal() {
	a.pendingDuplicateTask = nil
	a.pendingDuplicateReq = nil
}

func (a *App) resolveDuplicateFocusOrOpen() {
	dup := a.pendingDuplicateTask
	a.cancelDuplicateModal()
	if dup == nil {
		return
	}
	a.filterStatus = "All"
	a.filterCategory = CatAll
	a.searchQuery = ""
	a.selectedTaskID = dup.ID
	name := dup.Filename
	if name == "" {
		name = extractHost(dup.URL)
	}
	if dup.Status == engine.StatusCompleted && dup.SavePath != "" {
		a.revealTaskInFileManager(dup)
		a.statusMessage = i18n.Tf("msg.opened_folder", name)
		return
	}
	a.statusMessage = i18n.Tf("msg.dup_focused", name)
}

func (a *App) resolveDuplicateDownloadCopy() {
	req := a.pendingDuplicateReq
	a.cancelDuplicateModal()
	if req == nil {
		return
	}
	task, err := a.engine.AddTask(*req)
	if err != nil {
		a.statusMessage = err.Error()
		return
	}
	a.selectedTaskID = task.ID
	a.statusMessage = i18n.Tf("msg.added_download", task.Filename)
}

func (a *App) resolveDuplicateOverwrite() {
	dup := a.pendingDuplicateTask
	req := a.pendingDuplicateReq
	a.cancelDuplicateModal()
	if req == nil {
		return
	}
	if dup != nil {
		_ = a.engine.DeleteTask(dup.ID, true)
	}
	task, err := a.engine.AddTask(*req)
	if err != nil {
		a.statusMessage = err.Error()
		return
	}
	a.selectedTaskID = task.ID
	name := task.Filename
	if name == "" {
		name = task.URL
	}
	a.statusMessage = i18n.Tf("msg.dup_overwritten", name)
}

func (a *App) renderDuplicateModal(c *ui.Context, pal Palette) {
	if a.pendingDuplicateTask == nil {
		return
	}
	dup := a.pendingDuplicateTask
	name := dup.Filename
	if name == "" {
		name = extractHost(dup.URL)
	}
	statusLabel := i18n.T("status." + string(dup.Status))
	focusLabel := i18n.T("dup.focus_task")
	if dup.Status == engine.StatusCompleted {
		focusLabel = i18n.T("dup.open_existing")
	}

	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(480).Gap(16).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Width(32).Height(32).Radius(8).Background(pal.WarningSoft).Center().Shrink(0).Children(func() {
						ui.Icon(c, iconAlert).Width(16).Height(16).TextColor(pal.Warning)
					})
					ui.Text(c, i18n.T("dup.title")).FontSize(15).FontWeight(700).TextColor(pal.TextPrimary)
				})
				if rowIconButton(c, pal, iconClose, false) {
					a.cancelDuplicateModal()
				}
			})

			ui.Text(c, i18n.Tf("dup.body", name, statusLabel)).FontSize(12).TextColor(pal.TextSecondary)

			ui.Divider(c)

			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if toolbarButton(c, pal, nil, i18n.T("dup.cancel"), false, false) {
					a.cancelDuplicateModal()
				}
				ui.Spacer(c)
				if toolbarButton(c, pal, iconTrash, i18n.T("dup.overwrite"), false, true) {
					a.resolveDuplicateOverwrite()
				}
				if toolbarButton(c, pal, iconDownload, i18n.T("dup.download_copy"), false, false) {
					a.resolveDuplicateDownloadCopy()
				}
				if primaryCTA(c, pal, iconFolder, focusLabel) {
					a.resolveDuplicateFocusOrOpen()
				}
			})
		})
	})
	if !open {
		a.cancelDuplicateModal()
	}
}

package gui

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
	"magicdown/pkg/nativemessaging"
)

type mockWindowController struct {
	hwnd        uintptr
	visible     bool
	maximized   bool
	minimized   int
	closed      int
	hidden      int
	shown       int
	restored    int
	focused     int
	invalidated int
}

func (m *mockWindowController) Minimize()            { m.minimized++ }
func (m *mockWindowController) ToggleMaximize()      { m.maximized = !m.maximized }
func (m *mockWindowController) IsMaximized() bool    { return m.maximized }
func (m *mockWindowController) Close()               { m.closed++ }
func (m *mockWindowController) Hide()                { m.visible = false; m.hidden++ }
func (m *mockWindowController) Show()                { m.visible = true; m.shown++ }
func (m *mockWindowController) Restore()             { m.restored++ }
func (m *mockWindowController) Focus()               { m.focused++ }
func (m *mockWindowController) IsVisible() bool      { return m.visible }
func (m *mockWindowController) Invalidate()          { m.invalidated++ }
func (m *mockWindowController) NativeHandle() uintptr { return m.hwnd }

func TestTrayPowerSaveAndWakeOnIncomingDownload(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	win := &mockWindowController{visible: true}
	app.SetWindowController(win)

	if app.FrameScheduleMode() != FrameScheduleIdle {
		t.Fatalf("expected FrameScheduleIdle when visible & idle, got %v", app.FrameScheduleMode())
	}

	// Close button should hide to tray when CloseToTray is true.
	if !app.HandleCloseRequest() {
		t.Fatalf("expected HandleCloseRequest to hide to tray")
	}
	if !app.IsInTray() || win.hidden != 1 {
		t.Fatalf("expected app in tray and window hidden, inTray=%v hidden=%d", app.IsInTray(), win.hidden)
	}
	if app.FrameScheduleMode() != FrameSchedulePaused {
		t.Fatalf("expected FrameSchedulePaused while hidden in tray, got %v", app.FrameScheduleMode())
	}

	// View while in tray must be a fast no-op (no panic, no modal popups).
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)
	tester.Frame()

	// Incoming IPC download wakes window immediately from tray.
	app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
		URL:      "https://example.com/archive.zip",
		Filename: "archive.zip",
	})
	if app.IsInTray() {
		t.Fatalf("expected EnqueueIncomingDownload to wake app from tray")
	}
	if win.shown != 1 || win.focused != 1 || win.invalidated != 1 {
		t.Fatalf("expected Show/Focus/Invalidate on wake, got shown=%d focused=%d invalidated=%d", win.shown, win.focused, win.invalidated)
	}
	if app.FrameScheduleMode() != FrameScheduleIdle {
		t.Fatalf("expected FrameScheduleIdle after waking from tray, got %v", app.FrameScheduleMode())
	}

	// When quitting, HandleCloseRequest must allow actual close.
	app.SetQuitting(true)
	if app.HandleCloseRequest() {
		t.Fatalf("expected HandleCloseRequest to return false while quitting")
	}
}

func TestOnboardingFlowAndConfigPersistence(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, statePath))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}

	app := NewApp(eng, nil)
	var autostartCalled bool
	var autostartValue bool
	app.autostartSetter = func(enabled bool) error {
		autostartCalled = true
		autostartValue = enabled
		return nil
	}
	app.nativeStatusFn = func() nativemessaging.RegistrationStatus {
		return nativemessaging.RegistrationStatus{ManifestExists: true, ChromeReady: true}
	}
	app.detectBrowsersFn = func() []nativemessaging.BrowserInfo {
		return []nativemessaging.BrowserInfo{
			{ID: "chrome", Name: "Google Chrome", Installed: true, ExecutablePath: `C:\chrome.exe`},
			{ID: "edge", Name: "Microsoft Edge", Installed: true, ExecutablePath: `C:\msedge.exe`},
			{ID: "brave", Name: "Brave", Installed: false},
		}
	}
	var integratedIDs []string
	app.integrateBrowsersFn = func(selected []nativemessaging.BrowserInfo, hostBin, extID string, openUI bool) []nativemessaging.BrowserInstallResult {
		var out []nativemessaging.BrowserInstallResult
		for _, b := range selected {
			integratedIDs = append(integratedIDs, b.ID)
			out = append(out, nativemessaging.BrowserInstallResult{
				BrowserID:    b.ID,
				BrowserName:  b.Name,
				NativeHostOK: true,
				StatusText:   "Native Host registered",
			})
		}
		return out
	}
	app.selectedBrowsers = make(map[string]bool)
	app.refreshDetectedBrowsers()
	// Uncheck Edge so only Chrome is integrated.
	app.selectedBrowsers["edge"] = false

	if !app.showOnboardingModal {
		t.Fatalf("expected onboarding modal to open on first run")
	}

	// Render onboarding modal cleanly.
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)
	tester.Frame()

	app.onboardingDownloadDir = filepath.Join(tempDir, "CustomDownloads")
	app.onboardingAutoCategorize = true
	app.onboardingLaunchAtStartup = true
	app.onboardingRunInTray = true
	app.completeOnboarding()

	if app.onboardingError != "" {
		t.Fatalf("unexpected onboarding error: %s", app.onboardingError)
	}
	if app.showOnboardingModal {
		t.Fatalf("onboarding modal should close after completion")
	}
	if !autostartCalled || !autostartValue {
		t.Fatalf("expected autostartSetter(true) to be called")
	}
	if len(integratedIDs) != 1 || integratedIDs[0] != "chrome" {
		t.Fatalf("expected only chrome to be integrated, got %v", integratedIDs)
	}
	eng.Close()

	// Re-open engine from disk and verify onboarding_completed & startup/tray flags persisted.
	eng2, err := engine.NewEngine(engine.DefaultConfig(tempDir, statePath))
	if err != nil {
		t.Fatalf("failed to reload engine: %v", err)
	}
	defer eng2.Close()

	cfg := eng2.Config()
	if !cfg.OnboardingCompleted || !cfg.LaunchAtStartup || !cfg.MinimizeToTray || !cfg.CloseToTray {
		t.Fatalf("persisted config mismatch: %+v", cfg)
	}
	app2 := NewApp(eng2, nil)
	if app2.showOnboardingModal {
		t.Fatalf("onboarding modal must not re-open once OnboardingCompleted is true")
	}
}

func TestIncomingDownloadDialogBringsWindowToForegroundFromBackgroundOrTray(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	win := &mockWindowController{hwnd: 0xBEEF, visible: true}
	app.SetWindowController(win)

	var fgCalls []uintptr
	app.bringToForegroundFn = func(hwnd uintptr) bool {
		fgCalls = append(fgCalls, hwnd)
		return true
	}

	// Case 1: App is NOT in tray (e.g. minimized to taskbar or behind browser).
	// EnqueueIncomingDownload must still restore/show/focus and invoke OS bring-to-foreground.
	app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
		URL:      "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg",
		Filename: "video-thumbnail.jpg",
	})
	if win.shown != 1 || win.restored != 1 || win.focused != 1 || win.invalidated != 1 {
		t.Fatalf("expected Show/Restore/Focus/Invalidate from background, got shown=%d restored=%d focused=%d invalidated=%d",
			win.shown, win.restored, win.focused, win.invalidated)
	}
	if len(fgCalls) != 1 || fgCalls[0] != 0xBEEF {
		t.Fatalf("expected bringToForegroundFn(0xBEEF) once on enqueue, got %v", fgCalls)
	}

	// Rendering the next frame pops the incoming download into Download Info Dialog,
	// clears onboarding/settings modals, and ensures foreground activation.
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)
	tester.Frame()

	if !app.showAddModal || !app.inputFromExtension || app.showOnboardingModal {
		t.Fatalf("expected Download Info Dialog open and onboarding hidden, got add=%v ext=%v onboarding=%v",
			app.showAddModal, app.inputFromExtension, app.showOnboardingModal)
	}
	if len(fgCalls) < 2 {
		t.Fatalf("expected openIncomingDownloadDialog to also invoke bringToForegroundFn, got %v", fgCalls)
	}

	// Case 2: App is hidden in tray and openIncomingDownloadDialog is triggered.
	app.showAddModal = false
	app.HideToTray()
	if !app.IsInTray() || win.visible {
		t.Fatalf("expected app hidden in tray")
	}

	app.openIncomingDownloadDialog(ipc.AddDownloadRequest{
		URL:      "https://sns-webpic-qc.xhscdn.com/cover.webp",
		Filename: "rednote-thumbnail.webp",
	})
	if app.IsInTray() || !win.visible || !app.showAddModal {
		t.Fatalf("expected openIncomingDownloadDialog to wake from tray and open modal, inTray=%v visible=%v modal=%v",
			app.IsInTray(), win.visible, app.showAddModal)
	}
}

type mockDispatchWindowController struct {
	mu           sync.Mutex
	hwnd         uintptr
	updateCalls  atomic.Int32
	shown        atomic.Int32
	restored     atomic.Int32
	focused      atomic.Int32
	invalidated  atomic.Int32
	queue        []func()
}

func (m *mockDispatchWindowController) Minimize()             {}
func (m *mockDispatchWindowController) ToggleMaximize()       {}
func (m *mockDispatchWindowController) IsMaximized() bool     { return false }
func (m *mockDispatchWindowController) Close()                {}
func (m *mockDispatchWindowController) Hide()                 {}
func (m *mockDispatchWindowController) Show()                 { m.shown.Add(1) }
func (m *mockDispatchWindowController) Restore()              { m.restored.Add(1) }
func (m *mockDispatchWindowController) Focus()                { m.focused.Add(1) }
func (m *mockDispatchWindowController) IsVisible() bool       { return true }
func (m *mockDispatchWindowController) Invalidate()           { m.invalidated.Add(1) }
func (m *mockDispatchWindowController) NativeHandle() uintptr { return m.hwnd }
func (m *mockDispatchWindowController) Update(fn func()) {
	m.updateCalls.Add(1)
	m.mu.Lock()
	m.queue = append(m.queue, fn)
	m.mu.Unlock()
}
func (m *mockDispatchWindowController) drainOnUIThread() {
	m.mu.Lock()
	items := m.queue
	m.queue = nil
	m.mu.Unlock()
	for _, fn := range items {
		fn()
	}
}

func TestBringToForegroundAndInvalidateDispatchToUIThreadAndRaceFree(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	win := &mockDispatchWindowController{hwnd: 0xCAFE}
	app.SetWindowController(win)

	var fgCount atomic.Int32
	app.bringToForegroundFn = func(hwnd uintptr) bool {
		if hwnd == 0xCAFE {
			fgCount.Add(1)
		}
		return true
	}

	// Off-UI-thread EnqueueIncomingDownload must marshal through Update() rather than
	// directly calling Show/Restore/Focus/bringToForegroundFn on the caller goroutine.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
				URL:      "https://example.com/file.zip",
				Filename: "file.zip",
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			app.SetWindowController(win)
			app.invalidateUI()
		}
	}()
	wg.Wait()

	if win.shown.Load() != 0 || fgCount.Load() != 0 {
		t.Fatalf("expected window calls to be deferred to Update(), got shown=%d fg=%d", win.shown.Load(), fgCount.Load())
	}
	if win.updateCalls.Load() < 20 {
		t.Fatalf("expected Update() to be called for enqueues and invalidations, got %d", win.updateCalls.Load())
	}

	win.drainOnUIThread()
	if win.shown.Load() != 10 || win.restored.Load() != 10 || win.focused.Load() != 10 || fgCount.Load() != 10 {
		t.Fatalf("expected 10 foreground activations on UI thread, got shown=%d restored=%d focused=%d fg=%d",
			win.shown.Load(), win.restored.Load(), win.focused.Load(), fgCount.Load())
	}
	if win.invalidated.Load() < 20 {
		t.Fatalf("expected at least 20 invalidations on UI thread, got %d", win.invalidated.Load())
	}
}

func TestTraySuppressesInvalidateAndTrimsMemoryWhenIdle(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	defer app.Close()

	win := &mockWindowController{visible: true}
	app.SetWindowController(win)

	var trimCount atomic.Int32
	app.trimDelay = 25 * time.Millisecond
	app.trimCooldown = 120 * time.Millisecond
	app.trimMemoryFn = func() { trimCount.Add(1) }

	// Hide to tray while idle -> invalidateUI() must be completely suppressed.
	app.HideToTray()
	beforeInv := win.invalidated
	app.invalidateUI()
	app.invalidateUI()
	if win.invalidated != beforeInv {
		t.Fatalf("expected invalidateUI() to be suppressed while in tray, got %d -> %d", beforeInv, win.invalidated)
	}

	// After debounce delay, idle memory trim must fire once.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) && trimCount.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if trimCount.Load() != 1 {
		t.Fatalf("expected idle memory trim to fire once, got %d", trimCount.Load())
	}

	// Wake from tray restores invalidation immediately.
	app.WakeFromTray()
	if app.IsInTray() || win.invalidated <= beforeInv {
		t.Fatalf("expected WakeFromTray to resume UI invalidations")
	}

	// Rapid hide within cooldown must NOT re-trigger trim.
	app.HideToTray()
	time.Sleep(45 * time.Millisecond)
	if trimCount.Load() != 1 {
		t.Fatalf("expected cooldown to suppress rapid memory trim, got %d", trimCount.Load())
	}

	// Wait past cooldown, hide, then wake before debounce -> trim must be cancelled.
	app.WakeFromTray()
	time.Sleep(130 * time.Millisecond)
	app.HideToTray()
	time.Sleep(5 * time.Millisecond)
	app.WakeFromTray()
	time.Sleep(45 * time.Millisecond)
	if trimCount.Load() != 1 {
		t.Fatalf("expected WakeFromTray before debounce to cancel trim, got %d", trimCount.Load())
	}

	// When engine has a queued/non-idle task, hiding to tray must skip memory trim.
	_, err = eng.AddTask(engine.AddTaskRequest{
		URL:       "https://example.com/busy.zip",
		Filename:  "busy.zip",
		AutoStart: false,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}
	app.HideToTray()
	time.Sleep(45 * time.Millisecond)
	if trimCount.Load() != 1 {
		t.Fatalf("expected memory trim to be skipped while engine is not idle, got %d", trimCount.Load())
	}
}

func TestOptInClipboardMonitorSequenceGatedAndWorksInTray(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	defer app.Close()

	win := &mockWindowController{visible: true}
	app.SetWindowController(win)
	app.trimMemoryFn = func() {}

	// Default MonitorClipboard == false -> zero background goroutine.
	if app.isClipboardMonitorRunning() {
		t.Fatalf("expected clipboard monitor NOT to run when disabled")
	}

	var seq atomic.Uint32
	seq.Store(10)
	var readCalls atomic.Int32
	var clipMu sync.Mutex
	clipText := "https://example.com/old-before-enable.zip"

	app.clipPollInterval = 15 * time.Millisecond
	app.clipSeqFn = func() (uint32, bool) { return seq.Load(), true }
	app.clipReadFn = func() (string, error) {
		readCalls.Add(1)
		clipMu.Lock()
		defer clipMu.Unlock()
		return clipText, nil
	}

	// Enable Clipboard Monitor and hide to tray.
	cfg := eng.Config()
	cfg.MonitorClipboard = true
	eng.UpdateConfig(cfg)
	app.syncClipboardMonitor()
	if !app.isClipboardMonitorRunning() {
		t.Fatalf("expected clipboard monitor goroutine to start when enabled")
	}
	app.HideToTray()

	// Unchanged sequence number must cause ZERO clipboard reads.
	time.Sleep(60 * time.Millisecond)
	if got := readCalls.Load(); got != 0 {
		t.Fatalf("expected 0 clipboard reads when sequence number unchanged, got %d", got)
	}
	if !app.IsInTray() {
		t.Fatalf("expected app to remain in tray when clipboard unchanged")
	}

	// Changing clipboard sequence with a downloadable URL wakes the app from tray.
	clipMu.Lock()
	clipText = "https://cdn.example.com/installer-v2.zip"
	clipMu.Unlock()
	seq.Store(11)

	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) && app.IsInTray() {
		time.Sleep(10 * time.Millisecond)
	}
	if app.IsInTray() {
		t.Fatalf("expected clipboard download URL to wake app from tray")
	}
	if got := readCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 clipboard read on sequence change, got %d", got)
	}

	// Disabling MonitorClipboard stops the background goroutine cleanly.
	cfg.MonitorClipboard = false
	eng.UpdateConfig(cfg)
	app.syncClipboardMonitor()
	if app.isClipboardMonitorRunning() {
		t.Fatalf("expected clipboard monitor goroutine to stop when disabled")
	}
}

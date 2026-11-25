package gui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
	"magicdown/pkg/nativemessaging"
)

type mockWindowController struct {
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

func (m *mockWindowController) Minimize()        { m.minimized++ }
func (m *mockWindowController) ToggleMaximize()  { m.maximized = !m.maximized }
func (m *mockWindowController) IsMaximized() bool { return m.maximized }
func (m *mockWindowController) Close()           { m.closed++ }
func (m *mockWindowController) Hide()            { m.visible = false; m.hidden++ }
func (m *mockWindowController) Show()            { m.visible = true; m.shown++ }
func (m *mockWindowController) Restore()         { m.restored++ }
func (m *mockWindowController) Focus()           { m.focused++ }
func (m *mockWindowController) IsVisible() bool  { return m.visible }
func (m *mockWindowController) Invalidate()      { m.invalidated++ }

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

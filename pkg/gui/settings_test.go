package gui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/i18n"
	"magicdown/pkg/ipc"
	"magicdown/pkg/nativemessaging"
	"magicdown/pkg/updater"
)

func TestSettingsApplyRetriesAndSpeedLimit(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 3,
		DefaultSegments:        8,
		MaxRetries:             3,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	app.openSettingsModal()
	if app.settingMaxRetries != "3" || app.settingSpeedLimit != "0" {
		t.Fatalf("settings did not prefill from config: retries=%q speed=%q", app.settingMaxRetries, app.settingSpeedLimit)
	}

	app.settingDownloadDir = tempDir
	app.settingMaxConcurrent = 2
	app.settingSegments = 4
	app.settingMaxRetries = "5"
	app.settingSpeedLimit = "512"
	app.saveSettings()
	if app.settingsError != "" {
		t.Fatalf("unexpected settings error: %s", app.settingsError)
	}

	cfg := eng.Config()
	if cfg.MaxRetries != 5 || cfg.SpeedLimitKBps != 512 || cfg.MaxConcurrentDownloads != 2 || cfg.DefaultSegments != 4 {
		t.Fatalf("config not applied: %+v", cfg)
	}
	if app.showSettingsModal {
		t.Fatalf("settings modal should close after a successful save")
	}

	// Invalid numeric input keeps the modal open with an error and does not
	// change the active config.
	app.openSettingsModal()
	app.settingSpeedLimit = "not-a-number"
	app.saveSettings()
	if app.settingsError == "" {
		t.Fatalf("expected a validation error for a non-numeric speed limit")
	}
	if got := eng.Config().SpeedLimitKBps; got != 512 {
		t.Fatalf("invalid save changed speed limit to %d", got)
	}

	// Rendering the settings modal must not panic with the new fields present.
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)
	app.openSettingsModal()
	tester.Frame()
}

func TestSettingsAutoCheckUpdatesPersistenceAndRender(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	if !eng.Config().AutoCheckUpdates {
		t.Fatalf("expected AutoCheckUpdates to default to true")
	}

	app := NewApp(eng, nil)
	app.openSettingsModal()
	app.settingAutoCheckUpdates = false
	app.saveSettings()
	if eng.Config().AutoCheckUpdates {
		t.Fatalf("expected AutoCheckUpdates=false after save")
	}

	// Render settings modal in update-available and downloading states without panic.
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)
	app.openSettingsModal()
	app.activeSettingsTab = settingsTabUpdates
	app.updateMu.Lock()
	app.updateStatus = "available"
	app.updateMessage = "New version v2.0.0 available"
	app.updateRelease = &updater.ReleaseInfo{Version: "v2.0.0", Body: "New tabbed Settings modal"}
	app.updateMu.Unlock()
	tester.Frame()

	app.updateMu.Lock()
	app.updateStatus = "downloading"
	app.updateDownloaded = 512 * 1024
	app.updateTotal = 1024 * 1024
	app.updateMu.Unlock()
	tester.Frame()
}

func TestSettingsModalTabsRenderAllLocalesAndViewports(t *testing.T) {
	prevLang := i18n.CurrentLang()
	defer i18n.SetLang(prevLang)

	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	app.detectBrowsersFn = func() []nativemessaging.BrowserInfo {
		return []nativemessaging.BrowserInfo{
			{ID: "chrome", Name: "Google Chrome", Family: nativemessaging.BrowserFamilyChromium, Installed: true, Version: "131.0"},
			{ID: "edge", Name: "Microsoft Edge", Family: nativemessaging.BrowserFamilyChromium, Installed: true, Version: "131.0"},
			{ID: "dia", Name: "Dia", Family: nativemessaging.BrowserFamilyChromium, Installed: true},
		}
	}
	app.openSettingsModal()
	if app.activeSettingsTab != settingsTabGeneral {
		t.Fatalf("expected openSettingsModal to default to General tab, got %v", app.activeSettingsTab)
	}

	viewports := [][2]int{
		{1120, 700},
		{920, 580},
	}
	tabs := []settingsTab{
		settingsTabGeneral,
		settingsTabDownloads,
		settingsTabBrowser,
		settingsTabUpdates,
	}

	for _, lang := range []i18n.Lang{i18n.LangVI, i18n.LangEN} {
		app.SetLanguage(lang)
		for _, vp := range viewports {
			tester := ui.NewTester(app.View, vp[0], vp[1])
			tester.SetScale(1)
			for _, tab := range tabs {
				app.activeSettingsTab = tab
				if tab == settingsTabBrowser {
					app.troubleshootBrowserID = "chrome"
					app.installGuideBrowserID = "edge"
				}
				tester.Frame()
			}
		}
	}

	// Validation error on Downloads fields switches focus to Downloads tab automatically.
	app.activeSettingsTab = settingsTabGeneral
	app.settingSpeedLimit = "invalid"
	app.saveSettings()
	if app.settingsError == "" || app.activeSettingsTab != settingsTabDownloads {
		t.Fatalf("expected downloads validation error and tab switch, err=%q tab=%v", app.settingsError, app.activeSettingsTab)
	}
}

type fakeInvalidateWinCtrl struct {
	invalidations atomic.Int32
}

func (f *fakeInvalidateWinCtrl) Minimize()         {}
func (f *fakeInvalidateWinCtrl) ToggleMaximize()   {}
func (f *fakeInvalidateWinCtrl) IsMaximized() bool { return false }
func (f *fakeInvalidateWinCtrl) Close()            {}
func (f *fakeInvalidateWinCtrl) Hide()             {}
func (f *fakeInvalidateWinCtrl) Show()             {}
func (f *fakeInvalidateWinCtrl) Restore()          {}
func (f *fakeInvalidateWinCtrl) Focus()            {}
func (f *fakeInvalidateWinCtrl) IsVisible() bool   { return true }
func (f *fakeInvalidateWinCtrl) Invalidate()       { f.invalidations.Add(1) }

func TestBrowserIntegrationInvalidatesUIAndShowsConnectedForChromeEdgeDia(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tempDir, "ipc.token"))
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	srv := ipc.NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("start IPC server: %v", err)
	}
	defer srv.Stop()

	app := NewApp(eng, srv)
	win := &fakeInvalidateWinCtrl{}
	app.SetWindowController(win)
	app.detectBrowsersFn = func() []nativemessaging.BrowserInfo {
		return []nativemessaging.BrowserInfo{
			{ID: "chrome", Name: "Google Chrome", Family: nativemessaging.BrowserFamilyChromium, Installed: true, NativeRegistered: true},
			{ID: "edge", Name: "Microsoft Edge", Family: nativemessaging.BrowserFamilyChromium, Installed: true, NativeRegistered: true},
			{ID: "dia", Name: "Dia", Family: nativemessaging.BrowserFamilyChromium, Installed: true, NativeRegistered: true},
		}
	}
	app.refreshDetectedBrowsers()

	client := ipc.NewClient(srv.Port())
	client.SetAuthToken(srv.AuthToken())
	for _, browserID := range []string{"chrome", "edge", "dia"} {
		if _, err := client.SendHello(ipc.HelloRequest{
			Type:             "HELLO",
			ProtocolVersion:  1,
			ExtensionVersion: "0.1.0",
			Browser:          browserID,
		}); err != nil {
			t.Fatalf("SendHello(%s) failed: %v", browserID, err)
		}
	}

	if win.invalidations.Load() < 3 {
		t.Fatalf("expected UI invalidations on each HELLO handshake, got %d", win.invalidations.Load())
	}

	statuses := app.browserIntegrationStatuses()
	byID := make(map[string]nativemessaging.BrowserIntegrationStatus, len(statuses))
	for _, st := range statuses {
		byID[st.Browser.ID] = st
	}
	for _, browserID := range []string{"chrome", "edge", "dia"} {
		st, ok := byID[browserID]
		if !ok {
			t.Fatalf("missing browser status for %s", browserID)
		}
		if !st.Connection.Connected {
			t.Fatalf("expected %s Connection.Connected=true after HELLO, got %+v", browserID, st.Connection)
		}
	}
}

func TestSettingsLaunchAtStartupSyncAndImmediateToggle(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)

	// Simulate registry already enabled externally (e.g. via Inno Setup installer)
	// while config.json still has LaunchAtStartup=false.
	regEnabled := true
	var setCalls []bool
	var setErr error
	app.autostartGetter = func() (bool, error) { return regEnabled, nil }
	app.autostartSetter = func(enabled bool) error {
		setCalls = append(setCalls, enabled)
		if setErr != nil {
			return setErr
		}
		regEnabled = enabled
		return nil
	}

	app.openSettingsModal()
	if !app.settingLaunchAtStartup {
		t.Fatalf("expected openSettingsModal to sync LaunchAtStartup=true from OS registry")
	}

	// Saving unrelated settings must NOT spuriously write to the registry.
	app.settingSpeedLimit = "256"
	app.saveSettings()
	if len(setCalls) != 0 {
		t.Fatalf("expected no autostartSetter calls when autostart unchanged, got %v", setCalls)
	}
	if !eng.Config().LaunchAtStartup {
		t.Fatalf("expected saved config to retain synced LaunchAtStartup=true")
	}

	// Immediate toggle to false updates registry and config immediately.
	app.openSettingsModal()
	app.setLaunchAtStartupImmediate(false)
	if app.settingsError != "" || app.settingLaunchAtStartup || regEnabled {
		t.Fatalf("expected immediate disable to succeed: err=%q setting=%v reg=%v", app.settingsError, app.settingLaunchAtStartup, regEnabled)
	}
	if eng.Config().LaunchAtStartup {
		t.Fatalf("expected config LaunchAtStartup=false after immediate toggle")
	}
	if len(setCalls) != 1 || setCalls[0] != false {
		t.Fatalf("expected single autostartSetter(false) call, got %v", setCalls)
	}

	// Registry failure keeps UI/config state unchanged and surfaces a clear error.
	setErr = errors.New("registry write denied")
	app.setLaunchAtStartupImmediate(true)
	if app.settingLaunchAtStartup || regEnabled {
		t.Fatalf("expected state to remain false on registry failure")
	}
	if !strings.Contains(app.settingsError, "registry write denied") {
		t.Fatalf("expected settingsError to include registry error, got %q", app.settingsError)
	}

	// Clearing error and toggling back to true succeeds immediately.
	setErr = nil
	app.setLaunchAtStartupImmediate(true)
	if app.settingsError != "" || !app.settingLaunchAtStartup || !regEnabled || !eng.Config().LaunchAtStartup {
		t.Fatalf("expected immediate enable to succeed: err=%q setting=%v reg=%v cfg=%v",
			app.settingsError, app.settingLaunchAtStartup, regEnabled, eng.Config().LaunchAtStartup)
	}
}

func TestSettingsYTDLPStatusAndInstallUI(t *testing.T) {
	prevLang := i18n.CurrentLang()
	defer i18n.SetLang(prevLang)

	tempDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", tempDir)
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	win := &fakeInvalidateWinCtrl{}
	app.SetWindowController(win)

	checkDone := make(chan struct{})
	app.checkYTDLPStatusFn = func(ctx context.Context) engine.YTDLPStatus {
		defer close(checkDone)
		return engine.YTDLPStatus{Installed: false}
	}

	app.openSettingsModal()
	select {
	case <-checkDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for async yt-dlp status check")
	}

	// Wait briefly for the goroutine to release ytdlpMu and store status.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.ytdlpMu.Lock()
		checked := app.ytdlpChecked
		checking := app.ytdlpChecking
		app.ytdlpMu.Unlock()
		if checked && !checking {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Trigger install with progress updates and verify duplicate click guard.
	var installCalls atomic.Int32
	installStarted := make(chan struct{})
	installRelease := make(chan struct{})
	installedPath := filepath.Join(tempDir, "MagicDown", "bin", "yt-dlp.exe")

	app.installYTDLPFn = func(ctx context.Context, onProgress engine.YTDLPProgressFunc) (engine.YTDLPStatus, error) {
		installCalls.Add(1)
		if onProgress != nil {
			onProgress(512*1024, 1024*1024, engine.YTDLPStageDownloading)
		}
		close(installStarted)
		<-installRelease
		if onProgress != nil {
			onProgress(1024*1024, 1024*1024, engine.YTDLPStageVerifying)
			onProgress(1024*1024, 1024*1024, engine.YTDLPStageInstalling)
		}
		return engine.YTDLPStatus{
			Installed: true,
			Path:      installedPath,
			Version:   "2026.11.01",
			Managed:   true,
		}, nil
	}

	app.triggerYTDLPInstall()
	select {
	case <-installStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for yt-dlp install to start")
	}

	// Duplicate trigger while installing must be ignored.
	app.triggerYTDLPInstall()
	if got := installCalls.Load(); got != 1 {
		t.Fatalf("expected concurrent triggerYTDLPInstall to be ignored, got %d calls", got)
	}

	// Render installing state across locales without panic.
	app.activeSettingsTab = settingsTabUpdates
	for _, lang := range []i18n.Lang{i18n.LangEN, i18n.LangVI} {
		app.SetLanguage(lang)
		tester := ui.NewTester(app.View, 1120, 700)
		tester.SetScale(1)
		tester.Frame()
	}

	close(installRelease)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.ytdlpMu.Lock()
		installing := app.ytdlpInstalling
		st := app.ytdlpStatus
		ok := app.ytdlpMessageOK
		app.ytdlpMu.Unlock()
		if !installing && st.Installed && ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	app.ytdlpMu.Lock()
	finalSt := app.ytdlpStatus
	finalOK := app.ytdlpMessageOK
	app.ytdlpMu.Unlock()
	if !finalSt.Installed || finalSt.Version != "2026.11.01" || !finalOK {
		t.Fatalf("expected installed yt-dlp status after install, got st=%+v ok=%v", finalSt, finalOK)
	}

	// Verify shortened path helper formats %LOCALAPPDATA% cleanly.
	if short := shortenYTDLPPath(installedPath); !strings.HasPrefix(short, "%LOCALAPPDATA%") {
		t.Errorf("expected shortened path to start with %%LOCALAPPDATA%%, got %q", short)
	}

	// Render installed and error states cleanly.
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)
	tester.Frame()

	app.ytdlpMu.Lock()
	app.ytdlpMessageOK = false
	app.ytdlpMessageErr = true
	app.ytdlpMessage = i18n.Tf("ytdlp.install_err", "SHA-256 mismatch")
	app.ytdlpMu.Unlock()
	tester.Frame()
}

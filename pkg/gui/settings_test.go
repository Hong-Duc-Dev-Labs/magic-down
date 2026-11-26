package gui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/i18n"
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

package gui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
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
	app.updateMu.Lock()
	app.updateStatus = "available"
	app.updateMessage = "New version v2.0.0 available"
	app.updateMu.Unlock()
	tester.Frame()
}

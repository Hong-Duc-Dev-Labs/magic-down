package gui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
)

func TestFormatHelpers(t *testing.T) {
	if got := FormatBytes(512); got != "512 B" {
		t.Errorf("FormatBytes(512) = %q, want 512 B", got)
	}
	if got := FormatBytes(2048); got != "2.0 KB" {
		t.Errorf("FormatBytes(2048) = %q, want 2.0 KB", got)
	}
	if got := FormatBytes(5 * 1024 * 1024); got != "5.00 MB" {
		t.Errorf("FormatBytes(5MB) = %q, want 5.00 MB", got)
	}
	if got := FormatSpeed(0); got != "-" {
		t.Errorf("FormatSpeed(0) = %q, want -", got)
	}
	if got := FormatETA(125); got != "2m 5s" {
		t.Errorf("FormatETA(125) = %q, want 2m 5s", got)
	}
}

func TestMyGoNativeUIRendersCleanly(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 3,
		DefaultSegments:        8,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	srv := ipc.NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start ipc server: %v", err)
	}
	defer srv.Stop()

	_, _ = eng.AddTask(engine.AddTaskRequest{
		URL:       "https://example.com/demo.zip",
		Filename:  "demo.zip",
		AutoStart: false,
	})

	app := NewApp(eng, srv)
	tester := ui.NewTester(app.View, 1040, 660)
	tester.Frame()

	// Exercise modals rendering
	app.showAddModal = true
	tester.Frame()
	app.showAddModal = false

	app.showSettingsModal = true
	tester.Frame()
	app.showSettingsModal = false
}

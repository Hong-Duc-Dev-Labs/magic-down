package gui

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if got := ClassifyFile("ubuntu-24.04.iso", ""); got != CatPrograms {
		t.Errorf("ClassifyFile(.iso) = %q, want Programs", got)
	}
	if got := ClassifyFile("movie.mkv", ""); got != CatVideo {
		t.Errorf("ClassifyFile(.mkv) = %q, want Video", got)
	}
	if got := ClassifyFile("song.mp3", ""); got != CatMusic {
		t.Errorf("ClassifyFile(.mp3) = %q, want Music", got)
	}
	if got := ClassifyFile("archive.tar.gz", ""); got != CatCompressed {
		t.Errorf("ClassifyFile(.tar.gz) = %q, want Compressed", got)
	}
}

func savePNG(t *testing.T, tester *ui.Tester, path string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create screenshot %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, tester.Image()); err != nil {
		t.Fatalf("failed to encode png %s: %v", path, err)
	}
}

func TestMyGoNativeUIRendersCleanly(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")

	// Seed realistic persisted sample downloads for rendering & visual verification
	now := time.Now()
	seedTasks := []*engine.Task{
		{
			ID:              "t1",
			URL:             "https://releases.ubuntu.com/24.04/ubuntu-24.04.2-desktop-amd64.iso",
			Filename:        "ubuntu-24.04.2-desktop-amd64.iso",
			SavePath:        filepath.Join(tempDir, "ubuntu-24.04.2-desktop-amd64.iso"),
			TotalBytes:      5980000000,
			DownloadedBytes: 3827200000,
			Speed:           24800000,
			ETA:             86,
			Status:          engine.StatusPaused, // loadState preserves Paused; we verify display
			SupportsRanges:  true,
			NumSegments:     8,
			CreatedAt:       now.Add(-5 * time.Minute),
		},
		{
			ID:              "t2",
			URL:             "https://cdn.example.com/releases/DesignSystem-Kit-v4.2.zip",
			Filename:        "DesignSystem-Kit-v4.2.zip",
			SavePath:        filepath.Join(tempDir, "DesignSystem-Kit-v4.2.zip"),
			TotalBytes:      412000000,
			DownloadedBytes: 412000000,
			Status:          engine.StatusCompleted,
			SupportsRanges:  true,
			NumSegments:     8,
			CreatedAt:       now.Add(-12 * time.Minute),
		},
		{
			ID:              "t3",
			URL:             "https://media.example.org/videos/Keynote-4K-HDR.mkv",
			Filename:        "Keynote-4K-HDR.mkv",
			SavePath:        filepath.Join(tempDir, "Keynote-4K-HDR.mkv"),
			TotalBytes:      2150000000,
			DownloadedBytes: 0,
			Status:          engine.StatusQueued,
			SupportsRanges:  true,
			NumSegments:     16,
			CreatedAt:       now.Add(-2 * time.Minute),
		},
		{
			ID:              "t4",
			URL:             "https://docs.example.io/reports/Q4-Architecture-Spec.pdf",
			Filename:        "Q4-Architecture-Spec.pdf",
			SavePath:        filepath.Join(tempDir, "Q4-Architecture-Spec.pdf"),
			TotalBytes:      18400000,
			DownloadedBytes: 7360000,
			Status:          engine.StatusFailed,
			ErrorMessage:    "connection reset by peer",
			NumSegments:     4,
			CreatedAt:       now.Add(-20 * time.Minute),
		},
	}
	seedBytes, _ := json.MarshalIndent(seedTasks, "", "  ")
	_ = os.WriteFile(stateFile, seedBytes, 0644)

	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 3,
		DefaultSegments:        8,
		MaxRetries:             3,
		DownloadDir:            tempDir,
		StateFilePath:          stateFile,
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

	app := NewApp(eng, srv)
	app.selectedTaskID = "t1"
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)
	tester.Frame()

	// Optionally save visual verification screenshots when MAGICDOWN_CAPTURE_SCREENSHOTS is set
	if outDir := os.Getenv("MAGICDOWN_CAPTURE_SCREENSHOTS"); outDir != "" {
		savePNG(t, tester, filepath.Join(outDir, "01-main-dark.png"))

		app.openAddModal()
		app.inputURL = "https://github.com/egoist/mygo/releases/download/v0.10.0/mygo-windows-amd64.zip"
		tester.Frame()
		savePNG(t, tester, filepath.Join(outDir, "02-add-modal.png"))
		app.showAddModal = false

		app.openSettingsModal()
		tester.Frame()
		savePNG(t, tester, filepath.Join(outDir, "03-settings-modal.png"))
		app.showSettingsModal = false

		app.darkMode = false
		tester.Frame()
		savePNG(t, tester, filepath.Join(outDir, "04-main-light.png"))
		app.darkMode = true

		app.searchQuery = "nonexistent-file-query"
		tester.Frame()
		savePNG(t, tester, filepath.Join(outDir, "05-empty-state.png"))
		app.searchQuery = ""
	} else {
		// Still exercise modals & light mode in regular unit tests
		app.openAddModal()
		tester.Frame()
		app.showAddModal = false

		// Exercise Download Info Dialog from incoming extension download
		app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
			URL:        "https://example.com/video_1080p.mp4",
			AudioURL:   "https://example.com/audio_128k.m4a",
			Filename:   "clip_1080p.mp4",
			TotalBytes: 25 * 1024 * 1024,
			AutoStart:  true,
		})
		tester.Frame()
		if !app.showAddModal || !app.inputFromExtension || app.inputCategory != engine.CategoryVideo {
			t.Fatalf("expected Download Info Dialog to open for incoming video, got show=%v ext=%v cat=%q", app.showAddModal, app.inputFromExtension, app.inputCategory)
		}
		app.showAddModal = false

		app.openSettingsModal()
		tester.Frame()
		app.showSettingsModal = false

		app.darkMode = false
		tester.Frame()
		app.darkMode = true
	}
}

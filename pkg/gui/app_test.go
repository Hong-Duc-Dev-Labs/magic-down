package gui

import (
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
	"magicdown/pkg/power"
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
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tempDir, "ipc.token"))
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

		// Exercise delete confirmation modal (request -> cancel -> request -> confirm)
		tasks := eng.GetTasks()
		if len(tasks) > 0 {
			target := tasks[0]
			app.selectedTaskID = target.ID
			app.requestDeleteTask(target)
			if app.pendingDeleteTaskID != target.ID {
				t.Fatalf("expected pendingDeleteTaskID=%q, got %q", target.ID, app.pendingDeleteTaskID)
			}
			// Incoming IPC download must not stack while delete confirmation is open
			app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
				URL:        "https://example.com/queued-during-delete.zip",
				Filename:   "queued-during-delete.zip",
				TotalBytes: 1024,
			})
			tester.Frame()
			if app.showAddModal {
				t.Fatalf("expected incoming download modal to wait while delete confirmation is open")
			}

			// Opening another modal clears pendingDeleteTaskID cleanly
			app.openSettingsModal()
			if app.pendingDeleteTaskID != "" {
				t.Fatalf("expected openSettingsModal to clear pendingDeleteTaskID")
			}
			app.showSettingsModal = false

			// Ctrl+F search focus flag is consumed cleanly during frame render
			app.focusSearchNext = true
			tester.Frame()
			if app.focusSearchNext {
				t.Fatalf("expected focusSearchNext to be cleared after focusing search input")
			}

			app.requestDeleteTask(target)
			app.cancelDeleteTask()
			if app.pendingDeleteTaskID != "" {
				t.Fatalf("expected cancelDeleteTask to clear pendingDeleteTaskID")
			}

			app.requestDeleteTask(target)
			app.confirmDeleteTask()
			if app.pendingDeleteTaskID != "" || app.selectedTaskID != "" {
				t.Fatalf("expected confirmDeleteTask to clear pendingDeleteTaskID and selectedTaskID")
			}
		}

		app.darkMode = false
		tester.Frame()
		app.darkMode = true
	}
}

func TestDuplicateDetectionResolutionPathsAndModalExclusivity(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
		OnboardingCompleted:    true,
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)

	// Seed an initial paused task on disk.
	orig, err := eng.AddTask(engine.AddTaskRequest{
		URL:         "https://cdn.example.com/files/release.zip?token=old",
		Filename:    "release.zip",
		SaveDir:     tempDir,
		StartPaused: true,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}
	_ = os.WriteFile(orig.SavePath, []byte("existing"), 0644)

	// Enqueue two near-simultaneous IPC requests for the same URL: pendingIncoming should dedup to 1.
	app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
		URL:         "https://cdn.example.com/files/release.zip?token=new1",
		Filename:    "release.zip",
		SaveDir:     tempDir,
		StartPaused: true,
	})
	app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
		URL:         "https://cdn.example.com/files/release.zip?token=new2",
		Filename:    "release.zip",
		SaveDir:     tempDir,
		StartPaused: true,
	})
	app.pendingMu.Lock()
	queuedLen := len(app.pendingIncoming)
	app.pendingMu.Unlock()
	if queuedLen != 1 {
		t.Fatalf("expected pendingIncoming deduplicated to 1, got %d", queuedLen)
	}

	// Render frame: should pop and open Duplicate modal (not Add modal).
	tester.Frame()
	if app.pendingDuplicateTask == nil || app.pendingDuplicateTask.ID != orig.ID {
		t.Fatalf("expected duplicate modal for task %q, got %v", orig.ID, app.pendingDuplicateTask)
	}
	if app.showAddModal {
		t.Fatalf("expected showAddModal to stay false when duplicate modal opens")
	}

	// Path 1: Focus Existing Task
	app.filterStatus = "Failed"
	app.resolveDuplicateFocusOrOpen()
	if app.pendingDuplicateTask != nil {
		t.Fatalf("expected duplicate modal cleared after focus")
	}
	if app.selectedTaskID != orig.ID || app.filterStatus != "All" {
		t.Fatalf("expected selectedTaskID=%q and filterStatus=All, got %q / %q", orig.ID, app.selectedTaskID, app.filterStatus)
	}

	// Path 2: Download as Copy (via manual submitAddDownload)
	app.openAddModal()
	app.inputURL = "https://cdn.example.com/files/release.zip?token=copy"
	app.inputFilename = "release.zip"
	app.inputSaveDir = tempDir
	app.submitAddDownload(false)
	if app.pendingDuplicateTask == nil {
		t.Fatalf("expected submitAddDownload to open duplicate modal")
	}
	tester.Frame()
	app.resolveDuplicateDownloadCopy()
	tasks := eng.GetTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks after Download as Copy, got %d", len(tasks))
	}
	if !strings.Contains(tasks[1].Filename, "(1)") {
		t.Errorf("expected copied filename to contain (1), got %q", tasks[1].Filename)
	}

	// Path 3: Overwrite / Re-download
	app.openDuplicateModal(orig, engine.AddTaskRequest{
		URL:         "https://cdn.example.com/files/release.zip?token=overwrite",
		Filename:    "release.zip",
		SaveDir:     tempDir,
		StartPaused: true,
	})
	// Opening delete modal clears duplicate modal (non-stacking invariant).
	app.requestDeleteTask(orig)
	if app.pendingDuplicateTask != nil || app.pendingDeleteTaskID != orig.ID {
		t.Fatalf("expected requestDeleteTask to clear pendingDuplicateTask cleanly")
	}
	app.cancelDeleteTask()

	app.openDuplicateModal(orig, engine.AddTaskRequest{
		URL:         "https://cdn.example.com/files/release.zip?token=overwrite",
		Filename:    "release.zip",
		SaveDir:     tempDir,
		StartPaused: true,
	})
	app.resolveDuplicateOverwrite()
	if _, err := eng.GetTask(orig.ID); err == nil {
		t.Fatalf("expected original task %q to be replaced on overwrite", orig.ID)
	}
	if app.selectedTaskID == "" || app.selectedTaskID == orig.ID {
		t.Fatalf("expected newly created replacement task to be selected, got %q", app.selectedTaskID)
	}
}

func TestRefreshDownloadAddressModalAndIPCInterception(t *testing.T) {
	payload := make([]byte, 1024*128)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[:1])
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
		OnboardingCompleted:    true,
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	openedCh := make(chan string, 1)
	app.openURLFn = func(u string) error {
		openedCh <- u
		return nil
	}
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)

	task, err := eng.AddTask(engine.AddTaskRequest{
		URL:         server.URL + "/expired.bin",
		Filename:    "expired.bin",
		Referrer:    "https://source.example.com/page",
		TotalBytes:  int64(len(payload)),
		StartPaused: true,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// Open Refresh modal and render it.
	app.openRefreshModal(task)
	tester.Frame()
	if !app.showRefreshModal {
		t.Fatalf("expected refresh modal open")
	}

	// Start browser capture -> sets waiting banner and opens referrer.
	app.startBrowserRefreshCapture()
	if app.refreshTargetTaskID != task.ID {
		t.Fatalf("expected refreshTargetTaskID=%q, got %q", task.ID, app.refreshTargetTaskID)
	}
	select {
	case openedURL := <-openedCh:
		if openedURL != "https://source.example.com/page" {
			t.Errorf("expected openURLFn called with referrer, got %q", openedURL)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for openURLFn")
	}
	tester.Frame()

	// Different-host IPC arrival should open Refresh modal with host mismatch warning instead of auto-applying.
	app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
		URL: "https://other-host.example.org/file.bin",
	})
	tester.Frame()
	if !app.showRefreshModal || app.refreshHostWarning == "" {
		t.Fatalf("expected host-mismatch warning in refresh modal, got show=%v warn=%q", app.showRefreshModal, app.refreshHostWarning)
	}
	app.closeRefreshModal()

	// Same-host IPC arrival should auto-apply and clear waiting state.
	app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
		URL: server.URL + "/fresh.bin",
	})
	tester.Frame()
	if app.refreshTargetTaskID != "" {
		t.Fatalf("expected refreshTargetTaskID cleared after successful capture, got %q (err=%q)", app.refreshTargetTaskID, app.refreshError)
	}
	updated, _ := eng.GetTask(task.ID)
	if updated.URL != server.URL+"/fresh.bin" {
		t.Fatalf("expected task URL updated to fresh link, got %q", updated.URL)
	}
}

func TestTaskDetailsModalSegmentVisualizerAndChecksum(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")
	filePath := filepath.Join(tempDir, "archive.zip")
	if err := os.WriteFile(filePath, []byte("magicdown-phase2-checksum-test"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	seedTasks := []*engine.Task{
		{
			ID:              "det-1",
			URL:             "https://cdn.example.com/archive.zip",
			Filename:        "archive.zip",
			SavePath:        filePath,
			TotalBytes:      30,
			DownloadedBytes: 30,
			Status:          engine.StatusCompleted,
			SupportsRanges:  true,
			NumSegments:     4,
			Segments: []*engine.Segment{
				{Index: 0, Start: 0, End: 7, Downloaded: 8, Status: "done"},
				{Index: 1, Start: 8, End: 15, Downloaded: 8, Status: "done"},
				{Index: 2, Start: 16, End: 23, Downloaded: 8, Status: "done"},
				{Index: 3, Start: 24, End: 29, Downloaded: 6, Status: "done"},
			},
			CreatedAt: time.Now(),
		},
	}
	raw, _ := json.MarshalIndent(seedTasks, "", "  ")
	_ = os.WriteFile(stateFile, raw, 0644)

	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          stateFile,
		OnboardingCompleted:    true,
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)

	task, _ := eng.GetTask("det-1")
	app.openTaskDetails(task)
	tester.Frame()
	if app.detailsTaskID != "det-1" {
		t.Fatalf("expected detailsTaskID=det-1, got %q", app.detailsTaskID)
	}

	// Update per-task speed limit live.
	app.detailsSpeedLimit = "512"
	app.applyTaskSpeedLimit()
	if app.detailsSpeedLimitErr != "" {
		t.Fatalf("unexpected speed limit error: %q", app.detailsSpeedLimitErr)
	}
	updated, _ := eng.GetTask("det-1")
	if updated.SpeedLimitKBps != 512 {
		t.Fatalf("expected SpeedLimitKBps=512, got %d", updated.SpeedLimitKBps)
	}

	// Compute SHA-256 / MD5 asynchronously.
	app.startChecksumComputation(task.ID, task.SavePath)
	deadline := time.Now().Add(3 * time.Second)
	var computed *engine.FileHashes
	for time.Now().Before(deadline) {
		app.checksumMu.Lock()
		if app.checksumHashes != nil {
			cp := *app.checksumHashes
			computed = &cp
		}
		app.checksumMu.Unlock()
		if computed != nil {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if computed == nil || len(computed.SHA256) != 64 || len(computed.MD5) != 32 {
		t.Fatalf("expected valid computed hashes, got %+v", computed)
	}

	// Render with matching and mismatched expected hash badges.
	app.detailsExpectedHash = computed.SHA256
	tester.Frame()
	app.detailsExpectedHash = "00000000000000000000000000000000"
	tester.Frame()

	app.closeTaskDetails()
	if app.detailsTaskID != "" {
		t.Fatalf("expected detailsTaskID cleared on close")
	}
}

func TestProxySettingsSaveLoadAndPostDownloadCountdown(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")
	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          stateFile,
		OnboardingCompleted:    true,
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	app := NewApp(eng, nil)
	app.autostartSetter = func(bool) error { return nil }
	var slept int
	app.powerCtrl = &power.Controller{
		SleepFn: func() error { slept++; return nil },
	}
	app.powerCountdownDuration = 200 * time.Millisecond

	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)

	// Exercise proxy settings validation + save.
	app.openSettingsModal()
	app.activeSettingsTab = settingsTabDownloads
	app.settingProxyMode = engine.ProxyModeHTTP
	app.settingProxyHost = "127.0.0.1"
	app.settingProxyPort = "99999" // invalid port
	app.saveSettings()
	if app.settingsError == "" {
		t.Fatalf("expected error for invalid proxy port")
	}

	app.settingProxyPort = "8080"
	app.settingProxyUser = "alice"
	app.settingProxyPass = "secret"
	tester.Frame()
	app.saveSettings()
	if app.settingsError != "" || app.showSettingsModal {
		t.Fatalf("expected proxy settings save to succeed, err=%q open=%v", app.settingsError, app.showSettingsModal)
	}
	savedProxy := eng.Config().Proxy
	if savedProxy.Mode != engine.ProxyModeHTTP || savedProxy.Host != "127.0.0.1" || savedProxy.Port != 8080 || savedProxy.Username != "alice" {
		t.Fatalf("unexpected persisted proxy config: %+v", savedProxy)
	}

	// Post-download countdown guard: must NOT trigger while a queued task exists.
	app.SetPostDownloadAction(power.ActionSleep)
	queuedTask, err := eng.AddTask(engine.AddTaskRequest{
		URL:       "https://example.com/queued.bin",
		Filename:  "queued.bin",
		AutoStart: false,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}
	app.HandleTaskCompleted(queuedTask)
	if active, _, _ := app.powerCountdown.State(); active {
		t.Fatalf("expected countdown NOT to start while queued task exists")
	}

	// Pause queued task so engine becomes idle, then complete -> countdown starts.
	_ = eng.PauseTask(queuedTask.ID)
	app.HandleTaskCompleted(queuedTask)
	if active, act, _ := app.powerCountdown.State(); !active || act != power.ActionSleep {
		t.Fatalf("expected active sleep countdown when engine is idle, got active=%v act=%q", active, act)
	}
	tester.Frame()

	// Incoming download must automatically cancel countdown.
	app.EnqueueIncomingDownload(ipc.AddDownloadRequest{
		URL:      "https://example.com/new-arrival.bin",
		Filename: "new-arrival.bin",
	})
	if active, _, _ := app.powerCountdown.State(); active {
		t.Fatalf("expected incoming download to cancel active power countdown")
	}
	time.Sleep(260 * time.Millisecond)
	if slept != 0 {
		t.Fatalf("expected cancelled countdown not to execute sleep, got %d", slept)
	}
}

func TestBatchModalSchedulerSettingsAndClipboardMonitor(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
		OnboardingCompleted:    true,
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	// Clipboard monitor must default to false (opt-in privacy invariant).
	if eng.Config().MonitorClipboard {
		t.Fatalf("expected MonitorClipboard to default to false")
	}

	// Verify URL matcher for clipboard monitor.
	if !IsMonitorableDownloadURL("https://cdn.example.com/app/release-v1.zip", nil) {
		t.Errorf("expected direct .zip URL to match clipboard monitor")
	}
	if IsMonitorableDownloadURL("https://example.com/articles/hello-world", nil) {
		t.Errorf("expected non-file webpage URL not to match clipboard monitor")
	}
	if IsMonitorableDownloadURL("not a url\nhttps://example.com/a.zip", nil) {
		t.Errorf("expected multiline text not to match clipboard monitor")
	}

	app := NewApp(eng, nil)
	app.autostartSetter = func(bool) error { return nil }
	tester := ui.NewTester(app.View, 1120, 700)
	tester.SetScale(1)

	// Batch URL modal preview & import.
	app.openBatchModal()
	app.batchInputText = "https://cdn.example.com/part_[01-05].zip"
	app.batchSaveDir = tempDir
	tester.Frame()
	app.submitBatchImport(false)
	if app.showBatchModal || app.batchError != "" {
		t.Fatalf("expected batch import to succeed, open=%v err=%q", app.showBatchModal, app.batchError)
	}
	if got := len(eng.GetTasks()); got != 5 {
		t.Fatalf("expected 5 batch tasks added, got %d", got)
	}

	// Scheduler & Clipboard Monitor settings save + validation.
	app.openSettingsModal()
	app.activeSettingsTab = settingsTabDownloads
	app.settingScheduleEnabled = true
	app.settingScheduleStart = "25:00" // invalid hour
	app.settingScheduleStop = "06:00"
	app.saveSettings()
	if app.settingsError == "" {
		t.Fatalf("expected validation error for invalid schedule start_time")
	}

	app.settingScheduleStart = "23:00"
	app.settingScheduleStop = "06:00"
	app.settingScheduleDays[int(time.Monday)] = true
	app.settingScheduleAction = "sleep"
	app.settingMonitorClipboard = true
	tester.Frame()
	app.saveSettings()
	if app.settingsError != "" || app.showSettingsModal {
		t.Fatalf("expected valid schedule & clipboard settings to save, err=%q open=%v", app.settingsError, app.showSettingsModal)
	}
	cfg := eng.Config()
	if !cfg.Schedule.Enabled || cfg.Schedule.StartTime != "23:00" || cfg.Schedule.StopTime != "06:00" || !cfg.MonitorClipboard {
		t.Fatalf("unexpected saved schedule/clipboard config: %+v / clip=%v", cfg.Schedule, cfg.MonitorClipboard)
	}

	// HandleScheduleStop starts cancelable countdown.
	app.powerCountdownDuration = 300 * time.Millisecond
	app.HandleScheduleStop("sleep")
	if active, act, _ := app.powerCountdown.State(); !active || act != power.ActionSleep {
		t.Fatalf("expected HandleScheduleStop to start sleep countdown, got active=%v act=%q", active, act)
	}
	app.cancelPowerCountdown()
}

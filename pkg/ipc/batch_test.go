package ipc

import (
	"path/filepath"
	"sync/atomic"
	"testing"

	"magicdown/pkg/engine"
)

func TestBatchDownloadBypassesDialogAndResolvesSubfolder(t *testing.T) {
	tmpDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tmpDir, filepath.Join(tmpDir, "tasks.json")))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	srv := NewServer(eng, 0)
	var dialogCalls atomic.Int32
	srv.SetOnIncomingDownload(func(req AddDownloadRequest) {
		dialogCalls.Add(1)
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	client := NewClient(srv.Port())
	resp, err := client.AddBatchDownload(BatchDownloadRequest{
		Subfolder: "../Vacation: 2026/Photos",
		Referrer:  "https://example.com/gallery",
		AutoStart: false,
		Items: []AddDownloadRequest{
			{URL: "https://example.com/img1.jpg", Filename: "img1.jpg", StartPaused: true},
			{URL: "https://example.com/img2.png", Filename: "img2.png", StartPaused: true},
			{URL: ""}, // invalid item should be counted as failed without aborting batch
		},
	})
	if err != nil {
		t.Fatalf("AddBatchDownload failed: %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected Success=true, got false: %+v", resp)
	}
	if resp.Added != 2 || resp.Failed != 1 {
		t.Fatalf("expected Added=2 Failed=1, got Added=%d Failed=%d", resp.Added, resp.Failed)
	}
	if dialogCalls.Load() != 0 {
		t.Fatalf("expected batch download to bypass onIncomingDownload dialog, got %d calls", dialogCalls.Load())
	}

	tasks := eng.GetTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks in engine, got %d", len(tasks))
	}
	for _, task := range tasks {
		saveDir := filepath.Dir(task.SavePath)
		if filepath.Dir(saveDir) != tmpDir {
			t.Fatalf("expected sanitized subfolder directly under %q, got %q (savePath=%q)", tmpDir, saveDir, task.SavePath)
		}
	}
}

func TestAddDownloadSkipDialogBypassesCallback(t *testing.T) {
	tmpDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tmpDir, filepath.Join(tmpDir, "tasks.json")))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	srv := NewServer(eng, 0)
	var dialogCalls atomic.Int32
	srv.SetOnIncomingDownload(func(req AddDownloadRequest) {
		dialogCalls.Add(1)
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	client := NewClient(srv.Port())
	resp, err := client.AddDownload(AddDownloadRequest{
		URL:         "https://example.com/direct.zip",
		Filename:    "direct.zip",
		SkipDialog:  true,
		StartPaused: true,
	})
	if err != nil {
		t.Fatalf("AddDownload failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected Success=true")
	}
	if dialogCalls.Load() != 0 {
		t.Fatalf("expected SkipDialog=true to bypass dialog callback")
	}
}

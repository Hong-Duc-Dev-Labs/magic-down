package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"magicdown/pkg/engine"
)

func TestBatchDownloadBypassesDialogAndResolvesSubfolder(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tmpDir, "ipc.token"))
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
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tmpDir, "ipc.token"))
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

func TestBatchDownloadValidatesURLAudioURLFilenameSaveDirAndItemCap(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tmpDir, "ipc.token"))
	eng, err := engine.NewEngine(engine.DefaultConfig(tmpDir, filepath.Join(tmpDir, "tasks.json")))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	srv := NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	client := NewClient(srv.Port())
	resp, err := client.AddBatchDownload(BatchDownloadRequest{
		AutoStart: false,
		Items: []AddDownloadRequest{
			{URL: "file:///C:/Windows/win.ini", Filename: "win.ini", StartPaused: true},
			{URL: "javascript:alert(1)", Filename: "xss.js", StartPaused: true},
			{URL: "https://example.com/v.mp4", AudioURL: "file:///C:/evil.m4a", Filename: "v.mp4", StartPaused: true},
			{URL: "https://example.com/a.zip", Filename: "../../evil.exe", StartPaused: true},
			{URL: "https://example.com/b.zip", Filename: "b.zip", SaveDir: `C:\Windows\System32`, StartPaused: true},
			{URL: "https://example.com/ok.zip", Filename: "ok.zip", StartPaused: true},
		},
	})
	if err != nil {
		t.Fatalf("AddBatchDownload failed: %v", err)
	}
	if resp.Added != 1 || resp.Failed != 5 {
		t.Fatalf("expected Added=1 Failed=5, got Added=%d Failed=%d (errors=%v)", resp.Added, resp.Failed, resp.Errors)
	}

	// Exceeding MaxBatchItems must be rejected with 400 Bad Request.
	tooMany := make([]AddDownloadRequest, MaxBatchItems+1)
	for i := range tooMany {
		tooMany[i] = AddDownloadRequest{URL: "https://example.com/item.bin"}
	}
	body, _ := json.Marshal(BatchDownloadRequest{Items: tooMany})
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/downloads/batch", srv.Port()), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(TokenHeader, srv.AuthToken())
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /downloads/batch cap check: %v", err)
	}
	httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized batch, got %d", httpResp.StatusCode)
	}
}

package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"magicdown/pkg/engine"
)

func TestIPCServerAndClient(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tempDir, "ipc.token"))
	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	// Use port 0 or high ephemeral port for testing
	server := NewServer(eng, 0)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start IPC server: %v", err)
	}
	defer server.Stop()

	port := server.Port()
	if port <= 0 {
		t.Fatalf("invalid bound port: %d", port)
	}

	client := NewClient(port)

	// Test IsAppRunning
	if !client.IsAppRunning() {
		t.Fatalf("expected IsAppRunning to be true")
	}

	// Test GetStatus
	status, err := client.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.App != "MagicDown" {
		t.Errorf("expected App MagicDown, got %s", status.App)
	}

	// Test AddDownload
	resp, err := client.AddDownload(AddDownloadRequest{
		URL:         "https://example.com/archive.zip",
		Filename:    "archive.zip",
		NumSegments: 4,
		AutoStart:   false,
	})
	if err != nil {
		t.Fatalf("AddDownload failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success true, got false")
	}
	if resp.Task == nil || resp.Task.Filename != "archive.zip" {
		t.Fatalf("expected task with filename archive.zip, got %v", resp.Task)
	}

	// Verify task in engine
	task, err := eng.GetTask(resp.Task.ID)
	if err != nil {
		t.Fatalf("task not found in engine: %v", err)
	}
	if task.Status != engine.StatusQueued {
		t.Errorf("expected status Queued, got %s", task.Status)
	}

	// Test stop
	if err := server.Stop(); err != nil {
		t.Fatalf("server stop failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if client.IsAppRunning() {
		t.Errorf("expected IsAppRunning to be false after server stopped")
	}
}

func TestConfigEndpointAppliesSettings(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tempDir, "ipc.token"))
	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	server := NewServer(eng, 0)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start IPC server: %v", err)
	}
	defer server.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d/config", server.Port())

	cfg := eng.Config()
	cfg.MaxRetries = 7
	cfg.SpeedLimitKBps = 1536
	cfg.StateFilePath = `C:\should\be\ignored.json`
	body, _ := json.Marshal(cfg)

	req, _ := http.NewRequest(http.MethodPost, base, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(TokenHeader, server.AuthToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /config failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /config status = %d", resp.StatusCode)
	}

	var updated engine.Config
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if updated.MaxRetries != 7 || updated.SpeedLimitKBps != 1536 {
		t.Fatalf("config not applied: %+v", updated)
	}
	if updated.StateFilePath != filepath.Join(tempDir, "state.json") {
		t.Fatalf("state path should be process-owned, got %q", updated.StateFilePath)
	}

	getReq, _ := http.NewRequest(http.MethodGet, base, nil)
	getReq.Header.Set(TokenHeader, server.AuthToken())
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET /config failed: %v", err)
	}
	defer getResp.Body.Close()
	var fetched engine.Config
	if err := json.NewDecoder(getResp.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if fetched.MaxRetries != 7 || fetched.SpeedLimitKBps != 1536 {
		t.Fatalf("GET /config mismatch: %+v", fetched)
	}
}

func TestServer_IncomingDownloadDialogHookAndAudioURL(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tempDir, "ipc.token"))
	eng, err := engine.NewEngine(engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json")))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	srv := NewServer(eng, 0)
	var intercepted AddDownloadRequest
	srv.SetOnIncomingDownload(func(req AddDownloadRequest) {
		intercepted = req
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	client := NewClient(srv.Port())
	resp, err := client.AddDownload(AddDownloadRequest{
		URL:        "https://example.com/video_1080p.mp4",
		AudioURL:   "https://example.com/audio_128k.m4a",
		Filename:   "clip.mp4",
		TotalBytes: 1234567,
		AutoStart:  true,
	})
	if err != nil {
		t.Fatalf("AddDownload failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true")
	}
	if intercepted.URL != "https://example.com/video_1080p.mp4" || intercepted.AudioURL != "https://example.com/audio_128k.m4a" {
		t.Fatalf("expected intercepted request with AudioURL, got %+v", intercepted)
	}
	if intercepted.TotalBytes != 1234567 {
		t.Fatalf("expected TotalBytes=1234567, got %d", intercepted.TotalBytes)
	}
	if len(eng.GetTasks()) != 0 {
		t.Fatalf("expected 0 tasks added directly when dialog hook is active")
	}
}

func TestIPCAddDownload_MP3OutputFormatAndBitrate(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tempDir, "ipc.token"))
	cfg := engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json"))
	cfg.AutoCategorize = true
	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	srv := NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	client := NewClient(srv.Port())
	resp, err := client.AddDownload(AddDownloadRequest{
		URL:          "https://example.com/audio.webm",
		Filename:     "track.webm",
		OutputFormat: "mp3",
		AudioBitrate: "320k",
		TargetFormat: "mp4",
		AutoStart:    false,
	})
	if err != nil {
		t.Fatalf("AddDownload failed: %v", err)
	}
	if !resp.Success || resp.Task == nil {
		t.Fatalf("expected success with task, got %+v", resp)
	}
	if resp.Task.OutputFormat != "mp3" || resp.Task.AudioBitrate != "320k" || resp.Task.TargetFormat != "mp4" {
		t.Fatalf("unexpected format fields: %+v", resp.Task)
	}
	if resp.Task.Filename != "track.mp3" {
		t.Fatalf("expected .mp3 filename, got %q", resp.Task.Filename)
	}
	if resp.Task.Category != engine.CategoryMusic {
		t.Fatalf("expected Music category, got %q", resp.Task.Category)
	}
}

func TestIPCAddDownload_GoogleDriveFolderZip(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tempDir, "ipc.token"))
	cfg := engine.DefaultConfig(tempDir, filepath.Join(tempDir, "state.json"))
	cfg.AutoCategorize = true
	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	srv := NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	folderZipURL := "https://takeout-download-drive.usercontent.google.com/download/j/1234567890?user=0&authuser=0"
	folderZipFilename := "[44.1-24 Tidal MQA]-20261126T081600Z-001.zip"

	if err := ValidateHTTPURL(folderZipURL); err != nil {
		t.Fatalf("ValidateHTTPURL rejected Google Drive folder ZIP URL: %v", err)
	}
	if err := ValidateFilename(folderZipFilename); err != nil {
		t.Fatalf("ValidateFilename rejected bracketed Google Drive folder ZIP filename: %v", err)
	}

	client := NewClient(srv.Port())
	resp, err := client.AddDownload(AddDownloadRequest{
		URL:        folderZipURL,
		Filename:   folderZipFilename,
		Referrer:   "https://drive.google.com/",
		TotalBytes: 0, // chunked streaming ZIP where browser reports totalBytes = -1
		AutoStart:  false,
	})
	if err != nil {
		t.Fatalf("AddDownload failed for Google Drive folder ZIP: %v", err)
	}
	if !resp.Success || resp.Task == nil {
		t.Fatalf("expected success with task, got %+v", resp)
	}
	if resp.Task.Filename != folderZipFilename {
		t.Fatalf("expected filename %q, got %q", folderZipFilename, resp.Task.Filename)
	}
	if resp.Task.TotalBytes != 0 {
		t.Fatalf("expected TotalBytes=0 for streaming ZIP, got %d", resp.Task.TotalBytes)
	}
	if resp.Task.Category != engine.CategoryCompressed {
		t.Fatalf("expected Compressed category for .zip, got %q", resp.Task.Category)
	}
}

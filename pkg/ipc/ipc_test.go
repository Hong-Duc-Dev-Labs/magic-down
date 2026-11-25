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

	resp, err := http.Post(base, "application/json", bytes.NewReader(body))
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

	getResp, err := http.Get(base)
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

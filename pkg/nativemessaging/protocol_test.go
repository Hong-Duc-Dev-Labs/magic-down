package nativemessaging

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
)

func TestNativeMessagingFramingRoundtrip(t *testing.T) {
	testPayload := []byte(`{"action":"ping","data":"hello world 12345"}`)

	var buf bytes.Buffer
	if err := WriteMessage(&buf, testPayload); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	// Verify length prefix: 4 bytes
	if buf.Len() != len(testPayload)+4 {
		t.Fatalf("expected buffer length %d, got %d", len(testPayload)+4, buf.Len())
	}

	// Read message back
	readBack, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if !bytes.Equal(readBack, testPayload) {
		t.Fatalf("payload mismatch! expected %s, got %s", string(testPayload), string(readBack))
	}
}

func TestNativeMessagingIPCForwarding(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        2,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	server := ipc.NewServer(eng, 0)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start IPC server: %v", err)
	}
	defer server.Stop()

	client := ipc.NewClient(server.Port())
	handler := NewHandler(client)

	// 1. Test Ping action
	pingMsg, _ := json.Marshal(InboundMessage{Action: "ping"})
	pingRespBytes, err := handler.HandleMessage(pingMsg)
	if err != nil {
		t.Fatalf("HandleMessage ping error: %v", err)
	}

	var pingResp OutboundMessage
	if err := json.Unmarshal(pingRespBytes, &pingResp); err != nil {
		t.Fatalf("failed to unmarshal ping response: %v", err)
	}
	if !pingResp.Success || !pingResp.AppRunning || pingResp.Message != "pong" {
		t.Fatalf("unexpected ping response: %+v", pingResp)
	}

	// 2. Test Download action
	dlMsg, _ := json.Marshal(InboundMessage{
		Action:   "download",
		URL:      "https://example.com/movie.mp4",
		Filename: "movie.mp4",
		Referrer: "https://example.com",
	})
	dlRespBytes, err := handler.HandleMessage(dlMsg)
	if err != nil {
		t.Fatalf("HandleMessage download error: %v", err)
	}

	var dlResp OutboundMessage
	if err := json.Unmarshal(dlRespBytes, &dlResp); err != nil {
		t.Fatalf("failed to unmarshal download response: %v", err)
	}
	if !dlResp.Success || dlResp.TaskID == "" {
		t.Fatalf("unexpected download response: %+v", dlResp)
	}

	// 3. Verify task exists in the download engine
	task, err := eng.GetTask(dlResp.TaskID)
	if err != nil {
		t.Fatalf("task was not created in engine: %v", err)
	}
	if task.Filename != "movie.mp4" {
		t.Errorf("expected filename movie.mp4, got %s", task.Filename)
	}
	if task.Referrer != "https://example.com" {
		t.Errorf("expected referrer https://example.com, got %s", task.Referrer)
	}

	// 4. A media plan must reach the engine intact through host -> IPC.
	planJSON := json.RawMessage(`{"kind":"range","url":"https://cdn.example.com/v.mp4","format":"mp4","parts":[{"start":0,"end":999},{"start":2000}]}`)
	planMsg, _ := json.Marshal(InboundMessage{
		Action:   "download",
		URL:      "https://example.com/watch?v=42",
		Filename: "clip.mp4",
		Plan:     planJSON,
	})
	planRespBytes, err := handler.HandleMessage(planMsg)
	if err != nil {
		t.Fatalf("HandleMessage plan download error: %v", err)
	}
	var planResp OutboundMessage
	if err := json.Unmarshal(planRespBytes, &planResp); err != nil {
		t.Fatalf("failed to unmarshal plan response: %v", err)
	}
	if !planResp.Success || planResp.TaskID == "" {
		t.Fatalf("unexpected plan download response: %+v", planResp)
	}
	planTask, err := eng.GetTask(planResp.TaskID)
	if err != nil {
		t.Fatalf("plan task was not created: %v", err)
	}
	if planTask.Plan == nil {
		t.Fatalf("plan was dropped between native host and engine")
	}
	if planTask.Plan.Kind != "range" || len(planTask.Plan.Parts) != 2 {
		t.Fatalf("unexpected plan in task: %+v", planTask.Plan)
	}
	if planTask.IsHLS || planTask.SupportsRanges {
		t.Errorf("plan task must not be flagged as HLS/ranged: %+v", planTask)
	}
}

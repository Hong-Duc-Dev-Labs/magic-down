package nativemessaging

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
)

func TestHandleBatchDownloadAction(t *testing.T) {
	tmpDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tmpDir, filepath.Join(tmpDir, "tasks.json")))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	srv := ipc.NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start IPC server: %v", err)
	}
	defer srv.Stop()

	handler := NewHandler(ipc.NewClient(srv.Port()))

	in := InboundMessage{
		Action:    "batch_download",
		Referrer:  "https://example.com/album",
		Subfolder: "Album 2026",
		Items: []ipc.AddDownloadRequest{
			{URL: "https://example.com/a.jpg", Filename: "a.jpg", StartPaused: true},
			{URL: "https://example.com/b.pdf", Filename: "b.pdf", StartPaused: true},
		},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	outBytes, err := handler.HandleMessage(raw)
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	var out OutboundMessage
	if err := json.Unmarshal(outBytes, &out); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if !out.Success || out.Action != "batch_download" || out.Added != 2 || out.Failed != 0 {
		t.Fatalf("unexpected batch_download response: %+v", out)
	}
}

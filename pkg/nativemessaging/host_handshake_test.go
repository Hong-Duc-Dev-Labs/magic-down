package nativemessaging

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
)

func TestMatchBrowserIDFromProcess(t *testing.T) {
	cases := []struct {
		exeName string
		exePath string
		want    string
	}{
		{"chrome.exe", `C:\Program Files\Google\Chrome\Application\chrome.exe`, "chrome"},
		{"msedge.exe", `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`, "edge"},
		{"Dia.exe", `C:\Users\u\AppData\Local\Microsoft\WindowsApps\Dia.exe`, "dia"},
		{"Arc.exe", `C:\Users\u\AppData\Local\Microsoft\WindowsApps\Arc.exe`, "arc"},
		{"brave.exe", "", "brave"},
		{"vivaldi.exe", "", "vivaldi"},
		{"opera.exe", `C:\Users\u\AppData\Local\Programs\Opera\opera.exe`, "opera"},
		{"opera.exe", `C:\Users\u\AppData\Local\Programs\Opera GX\opera.exe`, "operagx"},
		{"browser.exe", `C:\Program Files\CocCoc\Browser\Application\browser.exe`, "coccoc"},
		{"browser.exe", `C:\SomeOther\browser.exe`, ""},
		{"comet.exe", "", "comet"},
		{"thorium.exe", "", "thorium"},
		{"firefox.exe", "", "firefox"},
		{"zen.exe", "", "zen"},
		{"cmd.exe", `C:\Windows\System32\cmd.exe`, ""},
		{"go.exe", "", ""},
		{"", `C:\Users\u\AppData\Local\Programs\Dia\Dia.exe`, "dia"},
	}
	for _, tc := range cases {
		if got := MatchBrowserIDFromProcess(tc.exeName, tc.exePath); got != tc.want {
			t.Errorf("MatchBrowserIDFromProcess(%q, %q) = %q, want %q", tc.exeName, tc.exePath, got, tc.want)
		}
	}
}

func TestRunStdioHostOneShotHelloRemainsConnectedAfterEOF(t *testing.T) {
	tmpDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tmpDir, filepath.Join(tmpDir, "state.json")))
	if err != nil {
		t.Fatalf("init engine: %v", err)
	}
	defer eng.Close()

	srv := ipc.NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("start IPC server: %v", err)
	}
	defer srv.Stop()

	rawHello, _ := json.Marshal(InboundMessage{
		Type:             "HELLO",
		Action:           "hello",
		ProtocolVersion:  1,
		ExtensionVersion: "0.1.0",
		Browser:          "edge",
	})
	var inBuf, outBuf bytes.Buffer
	if err := WriteMessage(&inBuf, rawHello); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	// RunStdioHost reads 1 message then hits io.EOF (exact chrome.runtime.sendNativeMessage lifecycle).
	if err := RunStdioHost(&inBuf, &outBuf, srv.Port()); err != nil {
		t.Fatalf("RunStdioHost returned error: %v", err)
	}

	respBytes, err := ReadMessage(&outBuf)
	if err != nil {
		t.Fatalf("ReadMessage response: %v", err)
	}
	var out OutboundMessage
	if err := json.Unmarshal(respBytes, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !out.Success || out.Type != "WELCOME" || out.Status != "connected" || !out.AppRunning {
		t.Fatalf("unexpected WELCOME response: %+v", out)
	}

	// Regression check: normal EOF on one-shot sendNativeMessage must NOT erase the handshake.
	snaps := srv.GetHandshakeSnapshots()
	rec, ok := snaps["edge"]
	if !ok || !rec.Connected {
		t.Fatalf("expected edge to remain Connected=true after one-shot RunStdioHost EOF, got snapshots=%+v", snaps)
	}
}

func TestHandlerPrefersParentProcessBrowserAndSupportsExplicitDisconnect(t *testing.T) {
	tmpDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tmpDir, filepath.Join(tmpDir, "state.json")))
	if err != nil {
		t.Fatalf("init engine: %v", err)
	}
	defer eng.Close()

	srv := ipc.NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("start IPC server: %v", err)
	}
	defer srv.Stop()

	client := ipc.NewClient(srv.Port())
	client.SetAuthToken(srv.AuthToken())
	h := NewHandler(client)

	// Dia's User-Agent looks like Chrome, so extension sends browser="chrome",
	// while parent process inspection detects "dia".
	h.detectParentBrowserFn = func() string { return "dia" }

	rawHello, _ := json.Marshal(InboundMessage{
		Type:             "HELLO",
		Action:           "hello",
		ProtocolVersion:  1,
		ExtensionVersion: "0.1.0",
		Browser:          "chrome",
	})
	respBytes, err := h.HandleMessage(rawHello)
	if err != nil {
		t.Fatalf("HandleMessage(hello): %v", err)
	}
	var welcome OutboundMessage
	if err := json.Unmarshal(respBytes, &welcome); err != nil {
		t.Fatalf("unmarshal welcome: %v", err)
	}
	if !welcome.Success || welcome.Type != "WELCOME" || welcome.Status != "connected" {
		t.Fatalf("unexpected welcome: %+v", welcome)
	}

	snaps := srv.GetHandshakeSnapshots()
	if !snaps["dia"].Connected {
		t.Fatalf("expected dia to be connected via parent process detection, got %+v", snaps)
	}
	if snaps["chrome"].Connected {
		t.Fatalf("chrome must not be marked connected when parent process is dia, got %+v", snaps)
	}

	// Explicit disconnect message still disconnects the resolved browser.
	rawDisc, _ := json.Marshal(InboundMessage{Type: "DISCONNECT", Action: "disconnect", Browser: "chrome"})
	if _, err := h.HandleMessage(rawDisc); err != nil {
		t.Fatalf("HandleMessage(disconnect): %v", err)
	}
	if srv.GetHandshakeSnapshots()["dia"].Connected {
		t.Fatalf("expected dia to be disconnected after explicit DISCONNECT")
	}

	// Fallback to declared browser when parent process is not a known browser.
	h.detectParentBrowserFn = func() string { return "" }
	rawEdge, _ := json.Marshal(InboundMessage{
		Type:             "HELLO",
		ProtocolVersion:  1,
		ExtensionVersion: "0.1.0",
		Browser:          "edge",
	})
	if _, err := h.HandleMessage(rawEdge); err != nil {
		t.Fatalf("HandleMessage(edge): %v", err)
	}
	if !srv.GetHandshakeSnapshots()["edge"].Connected {
		t.Fatalf("expected edge fallback when parent process detection returns empty")
	}
}

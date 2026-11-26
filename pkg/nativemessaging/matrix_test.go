package nativemessaging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"magicdown/pkg/engine"
	"magicdown/pkg/ipc"
)

// TestWindowsIntegrationScenarioMatrix verifies the full Windows integration lifecycle:
// Chrome/Edge installed vs missing, host registered vs missing, extension handshake
// HELLO/WELCOME, desktop restart, browser restart, desktop upgrade, clean uninstall,
// and extension version update.
func TestWindowsIntegrationScenarioMatrix(t *testing.T) {
	// 1. Browser installed vs not installed + Host registered vs not registered
	chrome := BrowserInfo{ID: "chrome", Name: "Google Chrome", Installed: true, Family: BrowserFamilyChromium}
	edgeMissing := BrowserInfo{ID: "edge", Name: "Microsoft Edge", Installed: false, Family: BrowserFamilyChromium}

	if got := EvaluateBrowserIntegrationState(edgeMissing, false, ExtensionConnectionState{}); got != StateNotInstalled {
		t.Fatalf("Edge not installed: got %q, want %q", got, StateNotInstalled)
	}
	if got := EvaluateBrowserIntegrationState(chrome, false, ExtensionConnectionState{}); got != StateHostNotRegistered {
		t.Fatalf("Host not registered: got %q, want %q", got, StateHostNotRegistered)
	}
	if got := EvaluateBrowserIntegrationState(chrome, true, ExtensionConnectionState{}); got != StateInstallationRequired {
		t.Fatalf("Extension not installed: got %q, want %q", got, StateInstallationRequired)
	}
	if got := EvaluateBrowserIntegrationState(chrome, true, ExtensionConnectionState{Waiting: true}); got != StateWaitingForConnection {
		t.Fatalf("Extension waiting: got %q, want %q", got, StateWaitingForConnection)
	}

	// 2. Real Desktop IPC + Native Messaging Host HELLO/WELCOME handshake
	tmpDir := t.TempDir()
	t.Setenv("MAGICDOWN_IPC_TOKEN_PATH", filepath.Join(tmpDir, "ipc.token"))
	eng, err := engine.NewEngine(engine.DefaultConfig(tmpDir, filepath.Join(tmpDir, "state.json")))
	if err != nil {
		t.Fatalf("init engine: %v", err)
	}
	defer eng.Close()

	srv := ipc.NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("start IPC server: %v", err)
	}

	sendFramed := func(port int, token string, msg InboundMessage) OutboundMessage {
		t.Helper()
		raw, _ := json.Marshal(msg)
		var inBuf, outBuf bytes.Buffer
		if err := WriteMessage(&inBuf, raw); err != nil {
			t.Fatalf("WriteMessage: %v", err)
		}
		client := ipc.NewClient(port)
		client.SetAuthToken(token)
		h := NewHandler(client)
		msgBytes, err := ReadMessage(&inBuf)
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		respBytes, err := h.HandleMessage(msgBytes)
		if err != nil {
			t.Fatalf("HandleMessage: %v", err)
		}
		if err := WriteMessage(&outBuf, respBytes); err != nil {
			t.Fatalf("WriteMessage resp: %v", err)
		}
		gotBytes, err := ReadMessage(&outBuf)
		if err != nil {
			t.Fatalf("ReadMessage resp: %v", err)
		}
		var out OutboundMessage
		if err := json.Unmarshal(gotBytes, &out); err != nil {
			t.Fatalf("unmarshal resp: %v", err)
		}
		return out
	}

	// Extension connects via HELLO -> WELCOME
	welcome := sendFramed(srv.Port(), srv.AuthToken(), InboundMessage{
		Type:             "HELLO",
		ProtocolVersion:  1,
		ExtensionVersion: "0.1.0",
		Browser:          "chrome",
	})
	if !welcome.Success || welcome.Type != "WELCOME" || welcome.Status != "connected" {
		t.Fatalf("unexpected WELCOME: %+v", welcome)
	}
	snap := srv.GetHandshakeSnapshots()["chrome"]
	if !snap.Connected || snap.ExtensionVersion != "0.1.0" {
		t.Fatalf("expected connected chrome v0.1.0, got %+v", snap)
	}
	if got := EvaluateBrowserIntegrationState(chrome, true, ExtensionConnectionState{Connected: snap.Connected}); got != StateConnected {
		t.Fatalf("after HELLO/WELCOME: got %q, want %q", got, StateConnected)
	}

	// 3. Browser restart (graceful disconnect -> reconnect)
	client := ipc.NewClient(srv.Port())
	client.SetAuthToken(srv.AuthToken())
	if err := client.SendDisconnect("chrome"); err != nil {
		t.Fatalf("SendDisconnect: %v", err)
	}
	if srv.GetHandshakeSnapshots()["chrome"].Connected {
		t.Fatalf("expected chrome disconnected after browser restart")
	}

	// 4. Extension version update (0.1.0 -> 0.2.0)
	welcomeV2 := sendFramed(srv.Port(), srv.AuthToken(), InboundMessage{
		Type:             "HELLO",
		ProtocolVersion:  1,
		ExtensionVersion: "0.2.0",
		Browser:          "chrome",
	})
	if !welcomeV2.Success || srv.GetHandshakeSnapshots()["chrome"].ExtensionVersion != "0.2.0" {
		t.Fatalf("expected updated extension version 0.2.0, got %+v", srv.GetHandshakeSnapshots()["chrome"])
	}

	// 5. Desktop restart (stop old IPC server, start new IPC server with fresh token)
	_ = srv.Stop()
	srv2 := ipc.NewServer(eng, 0)
	if err := srv2.Start(); err != nil {
		t.Fatalf("restart IPC server: %v", err)
	}
	defer srv2.Stop()

	welcomeRestart := sendFramed(srv2.Port(), srv2.AuthToken(), InboundMessage{
		Type:             "HELLO",
		ProtocolVersion:  1,
		ExtensionVersion: "0.2.0",
		Browser:          "edge",
	})
	if !welcomeRestart.Success || !srv2.GetHandshakeSnapshots()["edge"].Connected {
		t.Fatalf("expected edge reconnect after desktop restart, got %+v", welcomeRestart)
	}

	// 6. Desktop upgrade (idempotent manifest overwrite) & clean uninstall
	manifestDir := t.TempDir()
	userState := filepath.Join(manifestDir, "downloads.json")
	_ = os.WriteFile(userState, []byte(`{"ok":true}`), 0644)

	exeV1 := createDummyExe(t)
	exeV2 := createDummyExe(t)
	mPath := filepath.Join(manifestDir, HostName+".chrome.json")
	m1, _ := BuildManifest(exeV1, DefaultExtensionID)
	m2, _ := BuildManifest(exeV2, DefaultExtensionID)
	if err := WriteManifest(mPath, m1); err != nil {
		t.Fatalf("WriteManifest v1: %v", err)
	}
	if err := WriteManifest(mPath, m2); err != nil {
		t.Fatalf("WriteManifest v2 upgrade: %v", err)
	}
	if err := ValidateManifestFile(mPath); err != nil {
		t.Fatalf("ValidateManifestFile after upgrade: %v", err)
	}
	removed, err := RemoveManagedManifests(manifestDir)
	if err != nil || len(removed) == 0 {
		t.Fatalf("RemoveManagedManifests failed: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(userState); err != nil {
		t.Fatalf("clean uninstall must preserve user state file: %v", err)
	}
}

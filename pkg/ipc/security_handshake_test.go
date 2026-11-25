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

func startTestIPCServer(t *testing.T) (*Server, *engine.Engine) {
	t.Helper()
	tmpDir := t.TempDir()
	eng, err := engine.NewEngine(engine.DefaultConfig(tmpDir, filepath.Join(tmpDir, "state.json")))
	if err != nil {
		t.Fatalf("init engine: %v", err)
	}
	t.Cleanup(func() { eng.Close() })

	srv := NewServer(eng, 0)
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv, eng
}

func TestIPCAuthRejectsMissingOrInvalidTokenAndCrossOrigin(t *testing.T) {
	srv, _ := startTestIPCServer(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", srv.Port())

	// 1. Missing token -> 401
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", resp.StatusCode)
	}

	// 2. Wrong token -> 401
	req, _ := http.NewRequest(http.MethodGet, base+"/health", nil)
	req.Header.Set(TokenHeader, "wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health wrong token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", resp.StatusCode)
	}

	// 3. Cross-origin header -> 403 Forbidden (even with valid token)
	req, _ = http.NewRequest(http.MethodGet, base+"/health", nil)
	req.Header.Set(TokenHeader, srv.AuthToken())
	req.Header.Set("Origin", "https://evil.example")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health origin: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d, want 403", resp.StatusCode)
	}
	if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin header, got %q", acao)
	}
}

func TestIPCCSRFRejectsGETOnMutatingEndpoints(t *testing.T) {
	srv, _ := startTestIPCServer(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", srv.Port())

	for _, ep := range []string{"/downloads/pause", "/downloads/resume", "/downloads/delete?id=123"} {
		req, _ := http.NewRequest(http.MethodGet, base+ep, nil)
		req.Header.Set(TokenHeader, srv.AuthToken())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", ep, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s status = %d, want 405", ep, resp.StatusCode)
		}
	}
}

func TestIPCValidatesURLSchemeAndPathTraversal(t *testing.T) {
	srv, _ := startTestIPCServer(t)
	base := fmt.Sprintf("http://127.0.0.1:%d/downloads/add", srv.Port())

	postAdd := func(payload AddDownloadRequest) int {
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(TokenHeader, srv.AuthToken())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /downloads/add: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := postAdd(AddDownloadRequest{URL: "file:///C:/Windows/win.ini"}); code != http.StatusBadRequest {
		t.Fatalf("file:// URL status = %d, want 400", code)
	}
	if code := postAdd(AddDownloadRequest{URL: "javascript:alert(1)"}); code != http.StatusBadRequest {
		t.Fatalf("javascript: URL status = %d, want 400", code)
	}
	if code := postAdd(AddDownloadRequest{URL: "https://example.com/a.zip", Filename: "../../evil.exe"}); code != http.StatusBadRequest {
		t.Fatalf("traversal filename status = %d, want 400", code)
	}
	if code := postAdd(AddDownloadRequest{URL: "https://example.com/a.zip", SaveDir: `C:\Windows\System32`}); code != http.StatusBadRequest {
		t.Fatalf("outside save_dir status = %d, want 400", code)
	}
}

func TestIPCHelloWelcomeHandshakeDisconnectAndExpiration(t *testing.T) {
	srv, _ := startTestIPCServer(t)
	srv.SetHandshakeTTL(120 * time.Millisecond)

	client := NewClient(srv.Port())
	client.SetAuthToken(srv.AuthToken())

	welcome, err := client.SendHello(HelloRequest{
		Type:             "HELLO",
		ProtocolVersion:  1,
		ExtensionVersion: "0.1.0",
		Browser:          "edge",
	})
	if err != nil {
		t.Fatalf("SendHello failed: %v", err)
	}
	if welcome.Type != "WELCOME" || welcome.Status != "connected" || welcome.ProtocolVersion != 1 {
		t.Fatalf("unexpected welcome: %+v", welcome)
	}

	snaps := srv.GetHandshakeSnapshots()
	if rec, ok := snaps["edge"]; !ok || !rec.Connected || rec.ExtensionVersion != "0.1.0" {
		t.Fatalf("expected connected edge handshake snapshot, got %+v", snaps)
	}

	// TTL expiration transitions Connected -> false
	time.Sleep(150 * time.Millisecond)
	snaps = srv.GetHandshakeSnapshots()
	if rec := snaps["edge"]; rec.Connected {
		t.Fatalf("expected expired handshake to report Connected=false")
	}

	// Re-hello then explicit graceful disconnect
	if _, err := client.SendHello(HelloRequest{Type: "HELLO", ProtocolVersion: 1, ExtensionVersion: "0.1.0", Browser: "chrome"}); err != nil {
		t.Fatalf("SendHello(chrome) failed: %v", err)
	}
	if err := client.SendDisconnect("chrome"); err != nil {
		t.Fatalf("SendDisconnect(chrome) failed: %v", err)
	}
	if rec := srv.GetHandshakeSnapshots()["chrome"]; rec.Connected {
		t.Fatalf("expected chrome to be disconnected after SendDisconnect")
	}
}

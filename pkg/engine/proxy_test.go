package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyHTTPAndSOCKS5Routing(t *testing.T) {
	payload, expectedHash := generateTestPayload(256 * 1024)

	// Origin server serving the test payload.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if rh := r.Header.Get("Range"); strings.HasPrefix(rh, "bytes=") {
			parts := strings.Split(strings.TrimPrefix(rh, "bytes="), "-")
			start, _ := strconv.Atoi(parts[0])
			end := len(payload) - 1
			if parts[1] != "" {
				end, _ = strconv.Atoi(parts[1])
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start : end+1])
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer origin.Close()

	// Local HTTP forward proxy that counts proxied requests and forwards them to origin.
	var proxyHits atomic.Int32
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		targetURL := r.RequestURI
		if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
			targetURL = origin.URL + r.URL.Path
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if rng := r.Header.Get("Range"); rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vals := range resp.Header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		buf := make([]byte, 32*1024)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				_, _ = w.Write(buf[:n])
			}
			if rerr != nil {
				break
			}
		}
	}))
	defer proxySrv.Close()

	pu, _ := url.Parse(proxySrv.URL)
	port, _ := strconv.Atoi(pu.Port())

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		MaxRetries:             0,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
		Proxy: ProxyConfig{
			Mode: ProxyModeHTTP,
			Host: pu.Hostname(),
			Port: port,
		},
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	// Verify TestProxyConnection succeeds against origin via HTTP proxy.
	if err := TestProxyConnection(context.Background(), eng.Config().Proxy, origin.URL+"/ping"); err != nil {
		t.Fatalf("TestProxyConnection failed: %v", err)
	}

	task, err := eng.AddTask(AddTaskRequest{
		URL:         origin.URL + "/proxied.bin",
		Filename:    "proxied.bin",
		NumSegments: 4,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var latest *Task
	for time.Now().Before(deadline) {
		latest, _ = eng.GetTask(task.ID)
		if latest.Status == StatusCompleted || latest.Status == StatusFailed {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if latest == nil || latest.Status != StatusCompleted {
		t.Fatalf("expected proxied download to complete, got %v", latest)
	}
	if proxyHits.Load() == 0 {
		t.Fatalf("expected HTTP proxy to receive requests, got 0 hits")
	}
	actualHash, err := computeFileSHA256(latest.SavePath)
	if err != nil || actualHash != expectedHash {
		t.Fatalf("SHA-256 mismatch: err=%v want=%s got=%s", err, expectedHash, actualHash)
	}

	// Swap to unreachable SOCKS5 proxy atomically via UpdateConfig -> should fail cleanly.
	cfg := eng.Config()
	cfg.Proxy = ProxyConfig{
		Mode: ProxyModeSOCKS5,
		Host: "127.0.0.1",
		Port: 1, // unreachable port
	}
	eng.UpdateConfig(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := TestProxyConnection(ctx, cfg.Proxy, origin.URL+"/ping"); err == nil {
		t.Fatalf("expected unreachable SOCKS5 proxy test to fail")
	}

	// Verify Windows WinINet proxy parser & bypass rules.
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/file.zip", nil)
	parsed, err := parseWindowsProxyServer("http=10.0.0.1:8080;https=10.0.0.2:8443", "localhost;*.internal", req)
	if err != nil || parsed == nil || parsed.Host != "10.0.0.2:8443" {
		t.Fatalf("unexpected parsed https proxy: %v %v", parsed, err)
	}
	bypassReq, _ := http.NewRequest(http.MethodGet, "https://app.internal/file.zip", nil)
	bypassed, err := parseWindowsProxyServer("10.0.0.1:8080", "*.internal;<local>", bypassReq)
	if err != nil || bypassed != nil {
		t.Fatalf("expected *.internal to bypass proxy, got %v %v", bypassed, err)
	}
}

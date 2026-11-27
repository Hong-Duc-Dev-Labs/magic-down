package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCheckYTDLPStatusAndManagedPriority(t *testing.T) {
	origLocator := ytdlpLocator
	origVersion := ytdlpVersionRunner
	t.Cleanup(func() {
		ytdlpLocator = origLocator
		ytdlpVersionRunner = origVersion
	})

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	managedPath, err := ManagedYTDLPPath()
	if err != nil {
		t.Fatalf("ManagedYTDLPPath failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(managedPath), 0755); err != nil {
		t.Fatalf("mkdir managed bin: %v", err)
	}
	if err := os.WriteFile(managedPath, []byte("mock-ytdlp"), 0755); err != nil {
		t.Fatalf("write managed yt-dlp: %v", err)
	}

	ytdlpLocator = findYTDLPDefault
	ytdlpVersionRunner = func(ctx context.Context, path string) (string, error) {
		return "2026.03.17", nil
	}

	st := CheckYTDLPStatus(context.Background())
	if !st.Installed {
		t.Fatalf("expected Installed=true, got %+v", st)
	}
	if !st.Managed {
		t.Fatalf("expected Managed=true for %s, got %+v", managedPath, st)
	}
	if st.Version != "2026.03.17" {
		t.Fatalf("expected Version 2026.03.17, got %q", st.Version)
	}

	// External (unmanaged) yt-dlp path
	ytdlpLocator = func() (string, error) { return filepath.Join(t.TempDir(), ytdlpBinaryName()), nil }
	extSt := CheckYTDLPStatus(context.Background())
	if !extSt.Installed || extSt.Managed {
		t.Fatalf("expected Installed=true Managed=false for external binary, got %+v", extSt)
	}

	// Missing yt-dlp
	ytdlpLocator = func() (string, error) { return "", ErrYTDLPNotFound }
	missingSt := CheckYTDLPStatus(context.Background())
	if missingSt.Installed || missingSt.Path != "" || missingSt.Version != "" {
		t.Fatalf("expected Installed=false when missing, got %+v", missingSt)
	}
}

func TestInstallYTDLPSuccessAndAtomicReplace(t *testing.T) {
	origVersion := ytdlpVersionRunner
	t.Cleanup(func() { ytdlpVersionRunner = origVersion })
	ytdlpVersionRunner = func(ctx context.Context, path string) (string, error) {
		return "2026.11.01", nil
	}

	payload := []byte("mock-official-yt-dlp-binary-payload-v2026.11.01")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/yt-dlp.exe":
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
			_, _ = w.Write(payload)
		case "/SHA2-256SUMS":
			fmt.Fprintf(w, "%s  yt-dlp.exe\n", hexSum)
			fmt.Fprintf(w, "%s  yt-dlp_linux\n", strings.Repeat("0", 64))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	destDir, err := ManagedYTDLPDir()
	if err != nil {
		t.Fatalf("ManagedYTDLPDir: %v", err)
	}

	// Pre-create an older binary to verify atomic replacement works cleanly.
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("mkdir destDir: %v", err)
	}
	destPath := filepath.Join(destDir, ytdlpBinaryName())
	if err := os.WriteFile(destPath, []byte("older-version"), 0755); err != nil {
		t.Fatalf("seed old binary: %v", err)
	}

	var (
		mu     sync.Mutex
		stages []string
	)
	st, err := installYTDLPWithConfig(context.Background(), ytdlpInstallConfig{
		binaryURL:    srv.URL + "/yt-dlp.exe",
		checksumsURL: srv.URL + "/SHA2-256SUMS",
		assetName:    "yt-dlp.exe",
		destDir:      destDir,
		httpClient:   srv.Client(),
	}, func(downloaded, total int64, stage string) {
		mu.Lock()
		if len(stages) == 0 || stages[len(stages)-1] != stage {
			stages = append(stages, stage)
		}
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("installYTDLPWithConfig failed: %v", err)
	}
	if !st.Installed || !st.Managed || st.Version != "2026.11.01" || st.Path != destPath {
		t.Fatalf("unexpected status: %+v", st)
	}

	gotBytes, err := os.ReadFile(destPath)
	if err != nil || string(gotBytes) != string(payload) {
		t.Fatalf("installed binary mismatch: err=%v got=%q", err, string(gotBytes))
	}

	assertNoTempFiles(t, destDir)

	mu.Lock()
	gotStages := strings.Join(stages, "->")
	mu.Unlock()
	wantStages := strings.Join([]string{YTDLPStageDownloading, YTDLPStageVerifying, YTDLPStageInstalling}, "->")
	if gotStages != wantStages {
		t.Errorf("progress stages = %q, want %q", gotStages, wantStages)
	}
}

func TestInstallYTDLPChecksumMismatchCleansUpTempFile(t *testing.T) {
	payload := []byte("tampered-or-corrupted-binary")
	wrongHash := strings.Repeat("a", 64)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/yt-dlp.exe":
			_, _ = w.Write(payload)
		case "/SHA2-256SUMS":
			fmt.Fprintf(w, "%s  yt-dlp.exe\n", wrongHash)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	destDir := t.TempDir()
	_, err := installYTDLPWithConfig(context.Background(), ytdlpInstallConfig{
		binaryURL:    srv.URL + "/yt-dlp.exe",
		checksumsURL: srv.URL + "/SHA2-256SUMS",
		assetName:    "yt-dlp.exe",
		destDir:      destDir,
		httpClient:   srv.Client(),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("expected SHA-256 mismatch error, got %v", err)
	}

	destPath := filepath.Join(destDir, ytdlpBinaryName())
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Errorf("expected target binary not to be created on checksum mismatch, statErr=%v", statErr)
	}
	assertNoTempFiles(t, destDir)
}

func TestInstallYTDLPCancelContextCleansUpTempFile(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/yt-dlp.exe" {
			w.Header().Set("Content-Length", "1048576")
			_, _ = w.Write([]byte("partial-chunk"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			close(started)
			<-release
			return
		}
		http.NotFound(w, r)
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	destDir := t.TempDir()
	_, err := installYTDLPWithConfig(ctx, ytdlpInstallConfig{
		binaryURL:    srv.URL + "/yt-dlp.exe",
		checksumsURL: srv.URL + "/SHA2-256SUMS",
		assetName:    "yt-dlp.exe",
		destDir:      destDir,
		httpClient:   srv.Client(),
	}, nil)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context.Canceled error, got %v", err)
	}

	destPath := filepath.Join(destDir, ytdlpBinaryName())
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Errorf("expected no installed binary after cancel, statErr=%v", statErr)
	}
	assertNoTempFiles(t, destDir)
}

func TestInstallYTDLPRejectsConcurrentCalls(t *testing.T) {
	origVersion := ytdlpVersionRunner
	t.Cleanup(func() { ytdlpVersionRunner = origVersion })
	ytdlpVersionRunner = func(ctx context.Context, path string) (string, error) {
		return "2026.11.01", nil
	}

	payload := []byte("concurrent-guard-binary")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	inHandler := make(chan struct{})
	unblock := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/yt-dlp.exe":
			close(inHandler)
			<-unblock
			_, _ = w.Write(payload)
		case "/SHA2-256SUMS":
			fmt.Fprintf(w, "%s  yt-dlp.exe\n", hexSum)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	destDir := t.TempDir()
	cfg := ytdlpInstallConfig{
		binaryURL:    srv.URL + "/yt-dlp.exe",
		checksumsURL: srv.URL + "/SHA2-256SUMS",
		assetName:    "yt-dlp.exe",
		destDir:      destDir,
		httpClient:   srv.Client(),
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := installYTDLPWithConfig(context.Background(), cfg, nil)
		firstDone <- err
	}()

	select {
	case <-inHandler:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for first install to start")
	}

	_, secondErr := installYTDLPWithConfig(context.Background(), cfg, nil)
	if !errors.Is(secondErr, ErrYTDLPInstallInProgress) {
		t.Errorf("expected ErrYTDLPInstallInProgress for concurrent call, got %v", secondErr)
	}

	close(unblock)
	if err := <-firstDone; err != nil {
		t.Fatalf("first install failed: %v", err)
	}
}

func TestValidateYTDLPDownloadURL(t *testing.T) {
	valid := []string{
		DefaultYTDLPBinaryURL,
		DefaultYTDLPChecksumsURL,
		"http://127.0.0.1:8080/yt-dlp.exe",
		"http://localhost:9000/SHA2-256SUMS",
	}
	for _, u := range valid {
		if err := validateYTDLPDownloadURL(u); err != nil {
			t.Errorf("validateYTDLPDownloadURL(%q) unexpected error: %v", u, err)
		}
	}

	invalid := []string{
		"",
		"http://example.com/yt-dlp.exe",
		"file:///C:/yt-dlp.exe",
		"ftp://github.com/yt-dlp.exe",
	}
	for _, u := range invalid {
		if err := validateYTDLPDownloadURL(u); err == nil {
			t.Errorf("validateYTDLPDownloadURL(%q) expected error, got nil", u)
		}
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("stray temp file left behind: %s", e.Name())
		}
	}
}

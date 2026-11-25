package updater

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.1.0", "v0.1.0", 0},
		{"0.1.0", "v0.1.0", 0},
		{"v0.1.0", "v0.2.0", -1},
		{"v1.2.3", "v1.2.4", -1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0-beta.1", "v1.0.0", -1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
	}
	for _, tc := range cases {
		got, err := CompareSemver(tc.a, tc.b)
		if err != nil {
			t.Fatalf("CompareSemver(%q, %q) unexpected error: %v", tc.a, tc.b, err)
		}
		if got != tc.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}

	if !IsNewer("v0.1.0", "v1.2.3") {
		t.Error("expected v1.2.3 to be newer than v0.1.0")
	}
	if IsNewer("v1.2.3", "v1.2.3") {
		t.Error("expected equal versions not to be newer")
	}
	if IsNewer("v2.0.0", "v1.9.9") {
		t.Error("expected older version not to be newer")
	}
}

func TestSelectWindowsAsset(t *testing.T) {
	assets := []githubAsset{
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		{Name: "magicdown-host.exe", BrowserDownloadURL: "https://example.com/magicdown-host.exe"},
		{Name: "magicdown.exe", BrowserDownloadURL: "https://example.com/magicdown.exe"},
		{Name: "MagicDown-windows-amd64.zip", BrowserDownloadURL: "https://example.com/MagicDown-windows-amd64.zip", Size: 1024},
	}
	got, ok := selectWindowsAsset(assets)
	if !ok || got.Name != "MagicDown-windows-amd64.zip" {
		t.Fatalf("expected MagicDown-windows-amd64.zip, got %+v (ok=%v)", got, ok)
	}

	// Fallback to .exe when no zip is present
	exeOnly := []githubAsset{
		{Name: "magicdown-host.exe", BrowserDownloadURL: "https://example.com/magicdown-host.exe"},
		{Name: "magicdown.exe", BrowserDownloadURL: "https://example.com/magicdown.exe"},
	}
	got, ok = selectWindowsAsset(exeOnly)
	if !ok || got.Name != "magicdown.exe" {
		t.Fatalf("expected magicdown.exe fallback, got %+v (ok=%v)", got, ok)
	}
}

func TestCheckDownloadVerifyAndStage(t *testing.T) {
	// Build a real zip payload in memory via a temp file
	payloadDir := t.TempDir()
	zipPath := filepath.Join(payloadDir, "MagicDown-windows-amd64.zip")
	createTestZip(t, zipPath, map[string]string{
		"magicdown.exe":          "FAKE-GUI-BINARY",
		"magicdown-host.exe":     "FAKE-HOST-BINARY",
		"extension/manifest.json": `{"name":"MagicDown"}`,
	})

	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read test zip: %v", err)
	}
	sum := sha256.Sum256(zipBytes)
	zipHash := hex.EncodeToString(sum[:])

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Hong-Duc-Dev-Labs/MagicDown/releases/latest":
			rel := githubRelease{
				TagName: "v1.2.3",
				Name:    "MagicDown v1.2.3",
				Body:    "- Added auto-updater\n- Bug fixes",
				HTMLURL: "https://github.com/Hong-Duc-Dev-Labs/MagicDown/releases/tag/v1.2.3",
				Assets: []githubAsset{
					{
						Name:               "MagicDown-windows-amd64.zip",
						BrowserDownloadURL: srv.URL + "/download/MagicDown-windows-amd64.zip",
						Size:               int64(len(zipBytes)),
					},
					{
						Name:               "checksums.txt",
						BrowserDownloadURL: srv.URL + "/download/checksums.txt",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(rel)
		case "/download/MagicDown-windows-amd64.zip":
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(zipBytes)))
			_, _ = w.Write(zipBytes)
		case "/download/checksums.txt":
			_, _ = fmt.Fprintf(w, "%s  MagicDown-windows-amd64.zip\n", zipHash)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	u := New(Options{
		Repo:           "Hong-Duc-Dev-Labs/MagicDown",
		CurrentVersion: "v0.1.0",
		APIBaseURL:     srv.URL,
		HTTPClient:     srv.Client(),
	})

	res, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate failed: %v", err)
	}
	if !res.UpdateAvailable {
		t.Fatalf("expected UpdateAvailable=true, got false")
	}
	if res.LatestVersion != "v1.2.3" || res.Release.AssetName != "MagicDown-windows-amd64.zip" {
		t.Fatalf("unexpected release info: %+v", res.Release)
	}

	var lastDownloaded, lastTotal int64
	dlDir := t.TempDir()
	downloadedPath, err := u.DownloadAsset(context.Background(), res.Release, dlDir, func(d, total int64) {
		lastDownloaded = d
		lastTotal = total
	})
	if err != nil {
		t.Fatalf("DownloadAsset failed: %v", err)
	}
	if lastDownloaded != int64(len(zipBytes)) || lastTotal != int64(len(zipBytes)) {
		t.Fatalf("unexpected progress: downloaded=%d total=%d want=%d", lastDownloaded, lastTotal, len(zipBytes))
	}

	if err := u.VerifySHA256(context.Background(), downloadedPath, res.Release.AssetName, res.Release.ChecksumURL); err != nil {
		t.Fatalf("VerifySHA256 failed: %v", err)
	}

	stagingDir := filepath.Join(t.TempDir(), "staging")
	if err := StageUpdate(downloadedPath, stagingDir); err != nil {
		t.Fatalf("StageUpdate failed: %v", err)
	}

	for _, relPath := range []string{"magicdown.exe", "magicdown-host.exe", filepath.Join("extension", "manifest.json")} {
		if _, err := os.Stat(filepath.Join(stagingDir, relPath)); err != nil {
			t.Errorf("expected staged file %s to exist: %v", relPath, err)
		}
	}
}

func TestVerifySHA256Mismatch(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "MagicDown-windows-amd64.zip")
	if err := os.WriteFile(tmpFile, []byte("corrupted"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "0000000000000000000000000000000000000000000000000000000000000000  MagicDown-windows-amd64.zip")
	}))
	defer srv.Close()

	u := New(Options{HTTPClient: srv.Client()})
	err := u.VerifySHA256(context.Background(), tmpFile, "MagicDown-windows-amd64.zip", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("expected SHA256 mismatch error, got: %v", err)
	}
}

func TestBuildApplyScript(t *testing.T) {
	script := BuildApplyScript(`C:\Temp\staging`, `C:\Users\Me\AppData\Local\Programs\MagicDown`, 4242, `C:\Users\Me\AppData\Local\Programs\MagicDown\magicdown.exe`)
	if script == "" {
		t.Skip("non-Windows stub")
	}
	for _, want := range []string{
		"$TargetPid = 4242",
		"magicdown-host",
		"magicdown.exe",
		"extension",
		"Start-Process",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("apply script missing %q", want)
		}
	}
}

func createTestZip(t *testing.T, zipPath string, files map[string]string) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

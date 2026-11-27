package engine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"magicdown/pkg/version"
)

const (
	// Official yt-dlp GitHub Releases latest download URLs.
	DefaultYTDLPBinaryURL    = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe"
	DefaultYTDLPChecksumsURL = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/SHA2-256SUMS"

	// Progress stages emitted to YTDLPProgressFunc during InstallYTDLP.
	YTDLPStageDownloading = "downloading"
	YTDLPStageVerifying   = "verifying"
	YTDLPStageInstalling  = "installing"

	maxYTDLPBinaryBytes   int64 = 150 * 1024 * 1024 // 150 MiB safety ceiling
	maxYTDLPChecksumBytes int64 = 2 * 1024 * 1024   // 2 MiB safety ceiling
	ytdlpVersionTimeout         = 5 * time.Second
	ytdlpChecksumTimeout        = 30 * time.Second
)

// ErrYTDLPInstallInProgress is returned when a second yt-dlp install/update is
// requested while one is already running.
var ErrYTDLPInstallInProgress = errors.New("yt-dlp installation is already in progress")

// YTDLPStatus describes the detected yt-dlp installation state.
type YTDLPStatus struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	Managed   bool   `json:"managed"`
}

// YTDLPProgressFunc receives real-time download and verification progress
// during InstallYTDLP. total may be <= 0 when Content-Length is unknown.
type YTDLPProgressFunc func(downloaded, total int64, stage string)

var (
	ytdlpInstallActive atomic.Bool
	ytdlpVersionRunner = runYTDLPVersionDefault
)

func ytdlpBinaryName() string {
	if runtime.GOOS == "windows" {
		return "yt-dlp.exe"
	}
	return "yt-dlp"
}

// ManagedYTDLPDir returns MagicDown's managed binary directory
// (%LOCALAPPDATA%\MagicDown\bin on Windows, ~/.local/share/magicdown/bin otherwise).
func ManagedYTDLPDir() (string, error) {
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, "MagicDown", "bin"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", errors.New("cannot determine local application data directory")
	}
	return filepath.Join(home, ".local", "share", "magicdown", "bin"), nil
}

// ManagedYTDLPPath returns the full path to MagicDown's managed yt-dlp binary.
func ManagedYTDLPPath() (string, error) {
	dir, err := ManagedYTDLPDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ytdlpBinaryName()), nil
}

func isManagedYTDLPPath(candidate string) bool {
	managed, err := ManagedYTDLPPath()
	if err != nil || managed == "" || candidate == "" {
		return false
	}
	absCand, err1 := filepath.Abs(candidate)
	absManaged, err2 := filepath.Abs(managed)
	if err1 != nil || err2 != nil {
		absCand = filepath.Clean(candidate)
		absManaged = filepath.Clean(managed)
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(absCand, absManaged)
	}
	return absCand == absManaged
}

func runYTDLPVersionDefault(ctx context.Context, ytdlpPath string) (string, error) {
	vCtx, cancel := context.WithTimeout(ctx, ytdlpVersionTimeout)
	defer cancel()

	cmd := exec.CommandContext(vCtx, ytdlpPath, "--version")
	configureProcessCleanup(cmd)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	line := strings.TrimSpace(stdout.String())
	if nl := strings.IndexAny(line, "\r\n"); nl >= 0 {
		line = strings.TrimSpace(line[:nl])
	}
	return line, nil
}

// CheckYTDLPStatus locates yt-dlp (preferring MagicDown's managed bin directory,
// then app directory, then PATH) and queries `yt-dlp --version` with a short timeout.
func CheckYTDLPStatus(ctx context.Context) YTDLPStatus {
	if ctx == nil {
		ctx = context.Background()
	}
	path, err := FindYTDLP()
	if err != nil || strings.TrimSpace(path) == "" {
		return YTDLPStatus{Installed: false}
	}
	ver, _ := ytdlpVersionRunner(ctx, path)
	return YTDLPStatus{
		Installed: true,
		Path:      path,
		Version:   ver,
		Managed:   isManagedYTDLPPath(path),
	}
}

type ytdlpInstallConfig struct {
	binaryURL    string
	checksumsURL string
	assetName    string
	destDir      string
	httpClient   *http.Client
}

// InstallYTDLP downloads the official yt-dlp binary into MagicDown's managed bin
// directory, verifies its SHA-256 digest against SHA2-256SUMS, and atomically
// replaces the target executable. Uses the Engine's configured HTTP client
// (respecting Proxy settings with no short 30s download timeout).
func (e *Engine) InstallYTDLP(ctx context.Context, onProgress YTDLPProgressFunc) (YTDLPStatus, error) {
	var client *http.Client
	if e != nil {
		client = e.getHTTPClient()
	}
	return InstallYTDLP(ctx, client, onProgress)
}

// InstallYTDLP downloads the official yt-dlp binary using client (or a default
// no-timeout client if nil), verifies SHA-256 against SHA2-256SUMS, and installs
// it atomically into ManagedYTDLPDir().
func InstallYTDLP(ctx context.Context, client *http.Client, onProgress YTDLPProgressFunc) (YTDLPStatus, error) {
	destDir, err := ManagedYTDLPDir()
	if err != nil {
		return YTDLPStatus{}, err
	}
	return installYTDLPWithConfig(ctx, ytdlpInstallConfig{
		binaryURL:    DefaultYTDLPBinaryURL,
		checksumsURL: DefaultYTDLPChecksumsURL,
		assetName:    "yt-dlp.exe",
		destDir:      destDir,
		httpClient:   client,
	}, onProgress)
}

func validateYTDLPDownloadURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid yt-dlp download URL: %q", raw)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "https" {
		return nil
	}
	if scheme == "http" {
		host := strings.ToLower(u.Hostname())
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("yt-dlp download URL must use HTTPS: %q", raw)
}

func installYTDLPWithConfig(ctx context.Context, cfg ytdlpInstallConfig, onProgress YTDLPProgressFunc) (YTDLPStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !ytdlpInstallActive.CompareAndSwap(false, true) {
		return YTDLPStatus{}, ErrYTDLPInstallInProgress
	}
	defer ytdlpInstallActive.Store(false)

	if err := validateYTDLPDownloadURL(cfg.binaryURL); err != nil {
		return YTDLPStatus{}, err
	}
	if err := validateYTDLPDownloadURL(cfg.checksumsURL); err != nil {
		return YTDLPStatus{}, err
	}
	if strings.TrimSpace(cfg.destDir) == "" {
		return YTDLPStatus{}, errors.New("destination directory cannot be empty")
	}
	assetName := strings.TrimSpace(cfg.assetName)
	if assetName == "" {
		assetName = "yt-dlp.exe"
	}

	// Clone HTTP client with Timeout=0 so large (~18 MB) downloads never hit a
	// short 30s client timeout while preserving the Engine's proxy Transport.
	dlClient := &http.Client{
		Timeout: 0,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return validateYTDLPDownloadURL(req.URL.String())
		},
	}
	if cfg.httpClient != nil {
		dlClient.Transport = cfg.httpClient.Transport
		if cfg.httpClient.CheckRedirect != nil {
			dlClient.CheckRedirect = cfg.httpClient.CheckRedirect
		}
	}

	if err := os.MkdirAll(cfg.destDir, 0755); err != nil {
		return YTDLPStatus{}, fmt.Errorf("create yt-dlp directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(cfg.destDir, "yt-dlp-*.tmp")
	if err != nil {
		return YTDLPStatus{}, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	// Stage 1: Download yt-dlp binary while computing SHA-256 on the fly.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.binaryURL, nil)
	if err != nil {
		_ = tmpFile.Close()
		return YTDLPStatus{}, fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "MagicDown/"+version.NormalizedVersion())

	resp, err := dlClient.Do(req)
	if err != nil {
		_ = tmpFile.Close()
		return YTDLPStatus{}, fmt.Errorf("download yt-dlp: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = tmpFile.Close()
		return YTDLPStatus{}, fmt.Errorf("download yt-dlp returned HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if total > maxYTDLPBinaryBytes {
		_ = resp.Body.Close()
		_ = tmpFile.Close()
		return YTDLPStatus{}, fmt.Errorf("yt-dlp binary exceeds maximum allowed size (%d bytes)", maxYTDLPBinaryBytes)
	}

	if onProgress != nil {
		onProgress(0, total, YTDLPStageDownloading)
	}

	hasher := sha256.New()
	limited := io.LimitReader(resp.Body, maxYTDLPBinaryBytes+1)
	buf := make([]byte, 32*1024)
	var downloaded int64
	lastReport := time.Now()

	for {
		n, readErr := limited.Read(buf)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > maxYTDLPBinaryBytes {
				_ = resp.Body.Close()
				_ = tmpFile.Close()
				return YTDLPStatus{}, fmt.Errorf("yt-dlp binary exceeds maximum allowed size (%d bytes)", maxYTDLPBinaryBytes)
			}
			if _, wErr := tmpFile.Write(buf[:n]); wErr != nil {
				_ = resp.Body.Close()
				_ = tmpFile.Close()
				return YTDLPStatus{}, fmt.Errorf("write yt-dlp temp file: %w", wErr)
			}
			_, _ = hasher.Write(buf[:n])
			if onProgress != nil && time.Since(lastReport) >= 50*time.Millisecond {
				onProgress(downloaded, total, YTDLPStageDownloading)
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = resp.Body.Close()
			_ = tmpFile.Close()
			return YTDLPStatus{}, fmt.Errorf("read yt-dlp download: %w", readErr)
		}
	}
	_ = resp.Body.Close()

	if downloaded == 0 {
		_ = tmpFile.Close()
		return YTDLPStatus{}, errors.New("downloaded yt-dlp binary is empty")
	}
	if onProgress != nil {
		onProgress(downloaded, total, YTDLPStageDownloading)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return YTDLPStatus{}, fmt.Errorf("sync yt-dlp temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return YTDLPStatus{}, fmt.Errorf("close yt-dlp temp file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return YTDLPStatus{}, err
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))

	// Stage 2: Fetch SHA2-256SUMS and verify SHA-256 digest before accepting file.
	if onProgress != nil {
		onProgress(downloaded, total, YTDLPStageVerifying)
	}
	expectedHash, err := fetchExpectedYTDLPSHA256(ctx, dlClient, cfg.checksumsURL, assetName)
	if err != nil {
		return YTDLPStatus{}, err
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(actualHash)), []byte(strings.ToLower(expectedHash))) != 1 {
		return YTDLPStatus{}, fmt.Errorf("SHA-256 verification failed for %s: expected %s, got %s", assetName, expectedHash, actualHash)
	}

	// Stage 3: Atomically replace destination binary.
	if onProgress != nil {
		onProgress(downloaded, total, YTDLPStageInstalling)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(tmpPath, 0755)
	}

	destPath := filepath.Join(cfg.destDir, ytdlpBinaryName())
	if err := os.Rename(tmpPath, destPath); err != nil {
		return YTDLPStatus{}, fmt.Errorf("install yt-dlp binary: %w", err)
	}
	committed = true

	ver, _ := ytdlpVersionRunner(ctx, destPath)
	return YTDLPStatus{
		Installed: true,
		Path:      destPath,
		Version:   ver,
		Managed:   isManagedYTDLPPath(destPath),
	}, nil
}

func fetchExpectedYTDLPSHA256(ctx context.Context, client *http.Client, checksumsURL, assetName string) (string, error) {
	cCtx, cancel := context.WithTimeout(ctx, ytdlpChecksumTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(cCtx, http.MethodGet, checksumsURL, nil)
	if err != nil {
		return "", fmt.Errorf("build checksum request: %w", err)
	}
	req.Header.Set("User-Agent", "MagicDown/"+version.NormalizedVersion())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch SHA2-256SUMS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch SHA2-256SUMS returned HTTP %d", resp.StatusCode)
	}

	sums := parseYTDLPChecksums(io.LimitReader(resp.Body, maxYTDLPChecksumBytes))
	expected, ok := sums[strings.ToLower(filepath.Base(assetName))]
	if !ok {
		return "", fmt.Errorf("SHA2-256SUMS does not contain an entry for %s", assetName)
	}
	return expected, nil
}

func parseYTDLPChecksums(r io.Reader) map[string]string {
	out := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := strings.ToLower(strings.TrimSpace(fields[0]))
		if len(hash) != 64 {
			continue
		}
		if _, err := hex.DecodeString(hash); err != nil {
			continue
		}
		name := strings.TrimPrefix(strings.TrimSpace(fields[1]), "*")
		name = filepath.Base(name)
		out[strings.ToLower(name)] = hash
	}
	return out
}

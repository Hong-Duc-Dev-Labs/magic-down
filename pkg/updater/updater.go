package updater

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"magicdown/pkg/version"
)

const defaultAPIBaseURL = "https://api.github.com"

// Options configures a GitHub Releases Updater instance.
type Options struct {
	Repo            string       // "owner/repo"; defaults to version.GitHubRepo
	CurrentVersion  string       // defaults to version.Version
	AllowPrerelease bool         // skip pre-releases when false
	APIBaseURL      string       // defaults to https://api.github.com
	HTTPClient      *http.Client // optional custom client
}

// ReleaseInfo describes a GitHub release and its selected Windows asset.
type ReleaseInfo struct {
	TagName     string
	Version     string
	Name        string
	Body        string
	HTMLURL     string
	PublishedAt string
	Prerelease  bool
	Draft       bool
	AssetName   string
	AssetURL    string
	AssetSize   int64
	ChecksumURL string
}

// CheckResult is returned by CheckForUpdate.
type CheckResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	Release         *ReleaseInfo
}

// ProgressFunc receives download progress updates (bytes downloaded, total bytes; total may be -1).
type ProgressFunc func(downloaded, total int64)

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Prerelease  bool          `json:"prerelease"`
	Draft       bool          `json:"draft"`
	Assets      []githubAsset `json:"assets"`
}

// Updater checks, downloads, verifies, and stages MagicDown updates from GitHub Releases.
type Updater struct {
	repo            string
	currentVersion  string
	allowPrerelease bool
	apiBaseURL      string
	httpClient      *http.Client
}

// New creates a new Updater with sane defaults.
func New(opts Options) *Updater {
	repo := version.ResolveGitHubRepo(opts.Repo)
	cur := strings.TrimSpace(opts.CurrentVersion)
	if cur == "" {
		cur = version.Version
	}
	base := strings.TrimRight(strings.TrimSpace(opts.APIBaseURL), "/")
	if base == "" {
		base = defaultAPIBaseURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Updater{
		repo:            repo,
		currentVersion:  cur,
		allowPrerelease: opts.AllowPrerelease,
		apiBaseURL:      base,
		httpClient:      client,
	}
}

// CheckForUpdate queries GitHub Releases and compares the latest release against CurrentVersion.
func (u *Updater) CheckForUpdate(ctx context.Context) (*CheckResult, error) {
	if u.repo == "" || !strings.Contains(u.repo, "/") {
		return nil, fmt.Errorf("invalid GitHub repository %q", u.repo)
	}

	rel, err := u.fetchLatestRelease(ctx)
	if err != nil {
		return nil, err
	}

	info := &ReleaseInfo{
		TagName:     rel.TagName,
		Version:     normalizeTag(rel.TagName),
		Name:        rel.Name,
		Body:        rel.Body,
		HTMLURL:     rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
		Prerelease:  rel.Prerelease,
		Draft:       rel.Draft,
	}

	if asset, ok := selectWindowsAsset(rel.Assets); ok {
		info.AssetName = asset.Name
		info.AssetURL = asset.BrowserDownloadURL
		info.AssetSize = asset.Size
	}
	if cs, ok := selectChecksumAsset(rel.Assets); ok {
		info.ChecksumURL = cs.BrowserDownloadURL
	}

	updateAvail := IsNewer(u.currentVersion, rel.TagName)
	return &CheckResult{
		CurrentVersion:  normalizeTag(u.currentVersion),
		LatestVersion:   info.Version,
		UpdateAvailable: updateAvail,
		Release:         info,
	}, nil
}

func (u *Updater) fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	if !u.allowPrerelease {
		endpoint := fmt.Sprintf("%s/repos/%s/releases/latest", u.apiBaseURL, u.repo)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		u.setHeaders(req)
		resp, err := u.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request latest release: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return nil, errors.New("no published releases found")
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
		}
		var rel githubRelease
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			return nil, fmt.Errorf("decode release JSON: %w", err)
		}
		if rel.Draft || rel.Prerelease {
			return nil, errors.New("no stable release available")
		}
		return &rel, nil
	}

	// When pre-releases are allowed, query /releases and pick the first non-draft release.
	endpoint := fmt.Sprintf("%s/repos/%s/releases?per_page=10", u.apiBaseURL, u.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	u.setHeaders(req)
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}
	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode releases JSON: %w", err)
	}
	for i := range releases {
		if !releases[i].Draft {
			return &releases[i], nil
		}
	}
	return nil, errors.New("no published releases found")
}

func (u *Updater) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MagicDown-Updater/"+version.NormalizedVersion())
}

// selectWindowsAsset picks the best Windows release asset, preferring
// MagicDown-windows-amd64.zip, then any Windows zip, then a Windows .exe.
func selectWindowsAsset(assets []githubAsset) (githubAsset, bool) {
	// Priority 1: exact MagicDown-windows-amd64.zip (case-insensitive)
	for _, a := range assets {
		if strings.EqualFold(a.Name, "MagicDown-windows-amd64.zip") {
			return a, true
		}
	}
	// Priority 2: *windows*amd64*.zip
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".zip") && strings.Contains(lower, "windows") && strings.Contains(lower, "amd64") {
			return a, true
		}
	}
	// Priority 3: any *windows*.zip
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".zip") && strings.Contains(lower, "windows") {
			return a, true
		}
	}
	// Priority 4: Windows .exe (excluding magicdown-host.exe)
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".exe") && !strings.Contains(lower, "host") {
			return a, true
		}
	}
	return githubAsset{}, false
}

func selectChecksumAsset(assets []githubAsset) (githubAsset, bool) {
	for _, a := range assets {
		if strings.EqualFold(a.Name, "checksums.txt") {
			return a, true
		}
	}
	return githubAsset{}, false
}

func normalizeTag(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "v0.0.0"
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return "v" + v[1:]
	}
	return "v" + v
}

// DownloadAsset downloads the release asset into destDir (or os.TempDir() if empty)
// and invokes onProgress as bytes arrive.
func (u *Updater) DownloadAsset(ctx context.Context, info *ReleaseInfo, destDir string, onProgress ProgressFunc) (string, error) {
	if info == nil || info.AssetURL == "" || info.AssetName == "" {
		return "", errors.New("release does not contain a downloadable Windows asset")
	}
	if destDir == "" {
		var err error
		destDir, err = os.MkdirTemp("", "magicdown-update-*")
		if err != nil {
			return "", fmt.Errorf("create temp dir: %w", err)
		}
	} else {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return "", fmt.Errorf("create dest dir: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.AssetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MagicDown-Updater/"+version.NormalizedVersion())

	// Use a client without short timeout for large asset downloads.
	dlClient := &http.Client{
		Transport: u.httpClient.Transport,
		Timeout:   0,
	}
	resp, err := dlClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download asset returned HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if total <= 0 && info.AssetSize > 0 {
		total = info.AssetSize
	}

	destPath := filepath.Join(destDir, filepath.Base(info.AssetName))
	f, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create destination file: %w", err)
	}

	buf := make([]byte, 32*1024)
	var downloaded int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := f.Write(buf[:n]); wErr != nil {
				f.Close()
				_ = os.Remove(destPath)
				return "", fmt.Errorf("write asset: %w", wErr)
			}
			downloaded += int64(n)
			if onProgress != nil {
				onProgress(downloaded, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			_ = os.Remove(destPath)
			return "", fmt.Errorf("read asset: %w", readErr)
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return destPath, nil
}

// ParseChecksums parses a sha256sum-style checksums.txt into a map[lowercaseFilename]lowercaseHexHash.
func ParseChecksums(r io.Reader) map[string]string {
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
		name := strings.TrimPrefix(strings.TrimSpace(fields[1]), "*")
		name = filepath.Base(name)
		out[strings.ToLower(name)] = hash
	}
	return out
}

// VerifySHA256 downloads checksums.txt from checksumURL (when non-empty) and verifies
// the SHA256 digest of filePath. If checksumURL is empty, verification is skipped (nil).
func (u *Updater) VerifySHA256(ctx context.Context, filePath, assetName, checksumURL string) error {
	if strings.TrimSpace(checksumURL) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MagicDown-Updater/"+version.NormalizedVersion())

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch checksums.txt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch checksums.txt returned HTTP %d", resp.StatusCode)
	}

	checksums := ParseChecksums(resp.Body)
	expected, ok := checksums[strings.ToLower(filepath.Base(assetName))]
	if !ok {
		return fmt.Errorf("checksums.txt does not contain an entry for %s", assetName)
	}

	actual, err := FileSHA256(filePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s", assetName, expected, actual)
	}
	return nil
}

// FileSHA256 computes the lowercase hex SHA256 digest of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// StageUpdate extracts a downloaded .zip (with ZipSlip protection) or copies a .exe
// into stagingDir so the apply helper can overwrite the installation directory cleanly.
func StageUpdate(archivePath, stagingDir string) error {
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	lower := strings.ToLower(archivePath)
	if strings.HasSuffix(lower, ".zip") {
		return extractZip(archivePath, stagingDir)
	}
	if strings.HasSuffix(lower, ".exe") {
		return copyFile(archivePath, filepath.Join(stagingDir, "magicdown.exe"))
	}
	return fmt.Errorf("unsupported update package format: %s", filepath.Base(archivePath))
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	cleanDest := filepath.Clean(destDir) + string(os.PathSeparator)

	for _, f := range r.File {
		targetPath := filepath.Join(destDir, f.Name)
		cleanTarget := filepath.Clean(targetPath)
		if !strings.HasPrefix(cleanTarget, cleanDest) && cleanTarget != filepath.Clean(destDir) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanTarget, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(cleanTarget)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrYTDLPNotFound is returned when yt-dlp is not installed or cannot be located.
var ErrYTDLPNotFound = errors.New("yt-dlp not found: install yt-dlp (e.g. winget install yt-dlp.yt-dlp) or place yt-dlp.exe next to MagicDown or in %LOCALAPPDATA%\\MagicDown\\bin")

// IsYTDLPMissingError reports whether err indicates that yt-dlp is not installed.
func IsYTDLPMissingError(err error) bool {
	return errors.Is(err, ErrYTDLPNotFound)
}

// YouTubeStreamOption represents a single downloadable YouTube quality or MP3 option
// resolved via yt-dlp.
type YouTubeStreamOption struct {
	URL          string `json:"url"`
	Quality      string `json:"quality"`
	Height       int    `json:"height,omitempty"`
	Width        int    `json:"width,omitempty"`
	FPS          int    `json:"fps,omitempty"`
	Size         int64  `json:"size,omitempty"`
	Format       string `json:"format"`
	Ext          string `json:"ext"`
	StreamType   string `json:"streamType"`
	Source       string `json:"source"`
	Title        string `json:"title,omitempty"`
	YTDLPFormat  string `json:"ytdlpFormat"`
	TargetFormat string `json:"targetFormat,omitempty"`
	OutputFormat string `json:"outputFormat,omitempty"`
	AudioBitrate string `json:"audioBitrate,omitempty"`
	HasVideo     bool   `json:"hasVideo"`
	HasAudio     bool   `json:"hasAudio"`
}

// YouTubeResolveResult holds resolved metadata and downloadable formats for a YouTube video.
type YouTubeResolveResult struct {
	VideoID     string                `json:"videoId"`
	URL         string                `json:"url"`
	Title       string                `json:"title"`
	Thumbnail   string                `json:"thumbnail,omitempty"`
	Duration    int                   `json:"duration,omitempty"`
	IsLive      bool                  `json:"isLive,omitempty"`
	Variants    []YouTubeStreamOption `json:"variants"`
	MP3Variants []YouTubeStreamOption `json:"mp3Variants"`
}

var (
	ytVideoIDRegex   = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)
	ytdlpFormatRegex = regexp.MustCompile(`^[A-Za-z0-9_+\[\]<>=./,-]{1,120}$`)

	ytdlpLocator = findYTDLPDefault

	ytdlpJSONRunner = func(ctx context.Context, ytdlpPath string, args []string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, ytdlpPath, args...)
		configureProcessCleanup(cmd)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if len(msg) > 240 {
				msg = msg[len(msg)-240:]
			}
			if msg != "" {
				return stdout.Bytes(), fmt.Errorf("%w: %s", err, msg)
			}
			return stdout.Bytes(), err
		}
		return stdout.Bytes(), nil
	}

	ytdlpDownloadRunner = runYTDLPProcessDefault
)

// FindYTDLP locates the yt-dlp binary next to the current executable,
// in %LOCALAPPDATA%\MagicDown\bin, or in PATH.
func FindYTDLP() (string, error) {
	return ytdlpLocator()
}

func findYTDLPDefault() (string, error) {
	binName := ytdlpBinaryName()

	// 1. MagicDown managed bin directory (%LOCALAPPDATA%\MagicDown\bin\yt-dlp.exe)
	// Checked first so an in-app install/update overrides any older bundled binary.
	if managedPath, err := ManagedYTDLPPath(); err == nil && managedPath != "" {
		if info, statErr := os.Stat(managedPath); statErr == nil && !info.IsDir() {
			return managedPath, nil
		}
	}

	// 2. Next to current executable (same directory as magicdown.exe / magicdown-host.exe)
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), binName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	// 3. System PATH
	if path, err := exec.LookPath("yt-dlp"); err == nil {
		return path, nil
	}

	return "", ErrYTDLPNotFound
}

// ValidateYTDLPFormat ensures a yt-dlp format selector string is safe and free of
// spaces or shell/flag injection characters.
func ValidateYTDLPFormat(format string) (string, error) {
	clean := strings.TrimSpace(format)
	if clean == "" {
		return "", nil
	}
	if strings.HasPrefix(clean, "-") || !ytdlpFormatRegex.MatchString(clean) {
		return "", fmt.Errorf("invalid ytdlp format selector: %q", format)
	}
	return clean, nil
}

// ValidateYouTubeURL strictly validates a YouTube URL and returns its canonical
// watch/shorts URL and video ID.
func ValidateYouTubeURL(raw string) (canonicalURL string, videoID string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", errors.New("empty YouTube URL")
	}
	u, parseErr := url.Parse(trimmed)
	if parseErr != nil {
		return "", "", fmt.Errorf("invalid URL: %w", parseErr)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", fmt.Errorf("unsupported URL scheme: %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	isShorts := false

	switch {
	case host == "youtu.be":
		segs := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(segs) > 0 && ytVideoIDRegex.MatchString(segs[0]) {
			videoID = segs[0]
		}
	case host == "youtube.com" || host == "www.youtube.com" || host == "m.youtube.com" ||
		host == "music.youtube.com" || host == "youtube-nocookie.com" || host == "www.youtube-nocookie.com":
		if v := strings.TrimSpace(u.Query().Get("v")); ytVideoIDRegex.MatchString(v) {
			videoID = v
		} else {
			segs := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(segs) >= 2 {
				switch segs[0] {
				case "shorts":
					if ytVideoIDRegex.MatchString(segs[1]) {
						videoID = segs[1]
						isShorts = true
					}
				case "embed", "live", "v":
					if ytVideoIDRegex.MatchString(segs[1]) {
						videoID = segs[1]
					}
				}
			}
		}
	default:
		return "", "", fmt.Errorf("unsupported YouTube host: %q", host)
	}

	if videoID == "" {
		return "", "", errors.New("could not extract valid YouTube video ID")
	}
	if isShorts {
		return "https://www.youtube.com/shorts/" + videoID, videoID, nil
	}
	return "https://www.youtube.com/watch?v=" + videoID, videoID, nil
}

// Short TTL cache for resolved YouTube metadata so reopening the extension
// dropdown is instantaneous.
type ytCacheEntry struct {
	result    *YouTubeResolveResult
	expiresAt time.Time
}

var (
	ytResolveCacheMu  sync.Mutex
	ytResolveCache    = make(map[string]ytCacheEntry)
	ytResolveCacheTTL = 5 * time.Minute
	ytResolveCacheMax = 32
)

// ClearYouTubeResolveCache clears the in-memory YouTube resolve cache (used in tests).
func ClearYouTubeResolveCache() {
	ytResolveCacheMu.Lock()
	ytResolveCache = make(map[string]ytCacheEntry)
	ytResolveCacheMu.Unlock()
}

func cloneYouTubeResolveResult(in *YouTubeResolveResult) *YouTubeResolveResult {
	if in == nil {
		return nil
	}
	cp := *in
	if len(in.Variants) > 0 {
		cp.Variants = make([]YouTubeStreamOption, len(in.Variants))
		copy(cp.Variants, in.Variants)
	}
	if len(in.MP3Variants) > 0 {
		cp.MP3Variants = make([]YouTubeStreamOption, len(in.MP3Variants))
		copy(cp.MP3Variants, in.MP3Variants)
	}
	return &cp
}

func getCachedYouTubeResolve(videoID string) *YouTubeResolveResult {
	ytResolveCacheMu.Lock()
	defer ytResolveCacheMu.Unlock()
	entry, ok := ytResolveCache[videoID]
	if !ok {
		return nil
	}
	if time.Now().After(entry.expiresAt) {
		delete(ytResolveCache, videoID)
		return nil
	}
	return cloneYouTubeResolveResult(entry.result)
}

func putCachedYouTubeResolve(videoID string, res *YouTubeResolveResult) {
	if videoID == "" || res == nil {
		return
	}
	ytResolveCacheMu.Lock()
	defer ytResolveCacheMu.Unlock()
	if len(ytResolveCache) >= ytResolveCacheMax {
		// Evict expired or arbitrary oldest entry.
		now := time.Now()
		var oldestKey string
		var oldestExp time.Time
		for k, v := range ytResolveCache {
			if now.After(v.expiresAt) {
				delete(ytResolveCache, k)
				continue
			}
			if oldestKey == "" || v.expiresAt.Before(oldestExp) {
				oldestKey = k
				oldestExp = v.expiresAt
			}
		}
		if len(ytResolveCache) >= ytResolveCacheMax && oldestKey != "" {
			delete(ytResolveCache, oldestKey)
		}
	}
	ytResolveCache[videoID] = ytCacheEntry{
		result:    cloneYouTubeResolveResult(res),
		expiresAt: time.Now().Add(ytResolveCacheTTL),
	}
}

type ytdlpFormatJSON struct {
	FormatID       string  `json:"format_id"`
	Ext            string  `json:"ext"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	FPS            float64 `json:"fps"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	Filesize       int64   `json:"filesize"`
	FilesizeApprox int64   `json:"filesize_approx"`
	TBR            float64 `json:"tbr"`
	ABR            float64 `json:"abr"`
}

type ytdlpInfoJSON struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Thumbnail string            `json:"thumbnail"`
	Duration  float64           `json:"duration"`
	IsLive    bool              `json:"is_live"`
	Formats   []ytdlpFormatJSON `json:"formats"`
}

// ParseYTDLPInfoJSON converts `yt-dlp --dump-single-json` output into a clean,
// deduplicated list of MP4 video qualities and MP3 audio presets.
func ParseYTDLPInfoJSON(rawJSON []byte, canonicalURL, fallbackVideoID string) (*YouTubeResolveResult, error) {
	var info ytdlpInfoJSON
	if err := json.Unmarshal(rawJSON, &info); err != nil {
		return nil, fmt.Errorf("failed to parse yt-dlp metadata: %w", err)
	}

	videoID := strings.TrimSpace(info.ID)
	if videoID == "" {
		videoID = fallbackVideoID
	}
	title := strings.TrimSpace(info.Title)
	if title == "" {
		title = "YouTube Video"
	}

	// Find best audio size estimate for muxed size display.
	var bestAudioSize int64
	var bestAudioABR float64
	for _, f := range info.Formats {
		vcodec := strings.ToLower(strings.TrimSpace(f.VCodec))
		acodec := strings.ToLower(strings.TrimSpace(f.ACodec))
		if (vcodec == "" || vcodec == "none") && acodec != "" && acodec != "none" {
			sz := f.Filesize
			if sz <= 0 {
				sz = f.FilesizeApprox
			}
			if f.ABR >= bestAudioABR {
				bestAudioABR = f.ABR
				if sz > 0 {
					bestAudioSize = sz
				}
			}
		}
	}

	type heightCandidate struct {
		height int
		width  int
		fps    int
		size   int64
		score  float64
	}
	byHeight := make(map[int]heightCandidate)

	for _, f := range info.Formats {
		vcodec := strings.ToLower(strings.TrimSpace(f.VCodec))
		if vcodec == "none" || f.Height < 144 {
			continue
		}
		// Skip storyboard/image formats
		if f.Ext == "mhtml" || f.Ext == "webp" || f.Ext == "jpg" {
			continue
		}
		sz := f.Filesize
		if sz <= 0 {
			sz = f.FilesizeApprox
		}
		fps := int(math.Round(f.FPS))
		score := f.TBR
		if strings.HasPrefix(vcodec, "avc1") || strings.HasPrefix(vcodec, "h264") {
			score += 10000
		} else if f.Ext == "mp4" {
			score += 5000
		}
		if prev, ok := byHeight[f.Height]; !ok || fps > prev.fps || (fps == prev.fps && score > prev.score) {
			byHeight[f.Height] = heightCandidate{
				height: f.Height,
				width:  f.Width,
				fps:    fps,
				size:   sz,
				score:  score,
			}
		}
	}

	heights := make([]int, 0, len(byHeight))
	for h := range byHeight {
		heights = append(heights, h)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(heights)))

	variants := make([]YouTubeStreamOption, 0, len(heights))
	for _, h := range heights {
		c := byHeight[h]
		label := fmt.Sprintf("%dp", h)
		if c.fps >= 50 {
			label = fmt.Sprintf("%dp%d", h, c.fps)
		}
		estSize := c.size
		if estSize > 0 && bestAudioSize > 0 {
			estSize += bestAudioSize
		}
		selector := fmt.Sprintf("bestvideo[height<=%d][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=%d]+bestaudio/best[height<=%d]", h, h, h)
		variants = append(variants, YouTubeStreamOption{
			URL:          canonicalURL,
			Quality:      label,
			Height:       c.height,
			Width:        c.width,
			FPS:          c.fps,
			Size:         estSize,
			Format:       "MP4",
			Ext:          "mp4",
			StreamType:   "muxed",
			Source:       "youtube",
			Title:        title,
			YTDLPFormat:  selector,
			TargetFormat: "mp4",
			HasVideo:     true,
			HasAudio:     true,
		})
	}

	// Fallback if yt-dlp returned formats without height metadata (e.g. live or unusual stream)
	if len(variants) == 0 && len(info.Formats) > 0 {
		variants = append(variants, YouTubeStreamOption{
			URL:          canonicalURL,
			Quality:      "Best",
			Format:       "MP4",
			Ext:          "mp4",
			StreamType:   "muxed",
			Source:       "youtube",
			Title:        title,
			YTDLPFormat:  "bestvideo[ext=mp4]+bestaudio[ext=m4a]/bestvideo+bestaudio/best",
			TargetFormat: "mp4",
			HasVideo:     true,
			HasAudio:     true,
		})
	}

	mp3Presets := []struct {
		quality string
		bitrate string
	}{
		{"MP3 · 320 kbps", "320k"},
		{"MP3 · 192 kbps", "192k"},
		{"MP3 · 128 kbps", "128k"},
	}
	mp3Variants := make([]YouTubeStreamOption, 0, len(mp3Presets))
	for _, p := range mp3Presets {
		mp3Variants = append(mp3Variants, YouTubeStreamOption{
			URL:          canonicalURL,
			Quality:      p.quality,
			Format:       "MP3",
			Ext:          "mp3",
			StreamType:   "audio-mp3",
			Source:       "youtube",
			Title:        title,
			YTDLPFormat:  "bestaudio/best",
			OutputFormat: "mp3",
			AudioBitrate: p.bitrate,
			HasVideo:     false,
			HasAudio:     true,
		})
	}

	thumb := strings.TrimSpace(info.Thumbnail)
	if thumb == "" && videoID != "" {
		thumb = fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", videoID)
	}

	return &YouTubeResolveResult{
		VideoID:     videoID,
		URL:         canonicalURL,
		Title:       title,
		Thumbnail:   thumb,
		Duration:    int(math.Round(info.Duration)),
		IsLive:      info.IsLive,
		Variants:    variants,
		MP3Variants: mp3Variants,
	}, nil
}

// ResolveYouTube resolves a YouTube video URL via yt-dlp with strict URL
// validation, short per-video caching, and a bounded context timeout.
func ResolveYouTube(ctx context.Context, rawURL string) (*YouTubeResolveResult, error) {
	canonicalURL, videoID, err := ValidateYouTubeURL(rawURL)
	if err != nil {
		return nil, err
	}

	if cached := getCachedYouTubeResolve(videoID); cached != nil {
		return cached, nil
	}

	ytdlpPath, err := FindYTDLP()
	if err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
	}

	args := []string{
		"--dump-single-json",
		"--no-playlist",
		"--no-warnings",
		"--skip-download",
		"--",
		canonicalURL,
	}

	out, err := ytdlpJSONRunner(ctx, ytdlpPath, args)
	if err != nil {
		return nil, fmt.Errorf("yt-dlp resolve failed: %w", err)
	}

	res, err := ParseYTDLPInfoJSON(out, canonicalURL, videoID)
	if err != nil {
		return nil, err
	}
	putCachedYouTubeResolve(videoID, res)
	return res, nil
}

// ytdlpProgressEvent represents parsed progress output from yt-dlp --progress-template.
type ytdlpProgressEvent struct {
	Downloaded int64
	Total      int64
	Speed      int64
	ETA        int64
	Muxing     bool
}

func parseYTDLPProgressLine(line string) (ytdlpProgressEvent, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ytdlpProgressEvent{}, false
	}
	if strings.HasPrefix(trimmed, "[Merger]") || strings.HasPrefix(trimmed, "[ExtractAudio]") || strings.HasPrefix(trimmed, "[Fixup") {
		return ytdlpProgressEvent{Muxing: true}, true
	}
	const prefix = "MAGICDOWN_PROGRESS:"
	idx := strings.Index(trimmed, prefix)
	if idx < 0 {
		return ytdlpProgressEvent{}, false
	}
	fields := strings.Split(trimmed[idx+len(prefix):], "|")
	if len(fields) < 5 {
		return ytdlpProgressEvent{}, false
	}
	parseNum := func(s string) int64 {
		s = strings.TrimSpace(s)
		if s == "" || s == "NA" || s == "None" {
			return 0
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			return 0
		}
		return int64(f)
	}
	downloaded := parseNum(fields[0])
	total := parseNum(fields[1])
	if total <= 0 {
		total = parseNum(fields[2])
	}
	speed := parseNum(fields[3])
	eta := parseNum(fields[4])
	return ytdlpProgressEvent{
		Downloaded: downloaded,
		Total:      total,
		Speed:      speed,
		ETA:        eta,
	}, true
}

func runYTDLPProcessDefault(ctx context.Context, ytdlpPath string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, ytdlpPath, args...)
	configureProcessCleanup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		lastErrs []string
	)
	consume := func(r io.Reader, recordErr bool) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if onLine != nil {
				onLine(line)
			}
			if recordErr {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					errMu.Lock()
					lastErrs = append(lastErrs, trimmed)
					if len(lastErrs) > 6 {
						lastErrs = lastErrs[len(lastErrs)-6:]
					}
					errMu.Unlock()
				}
			}
		}
	}

	wg.Add(2)
	go consume(stdout, false)
	go consume(stderr, true)

	waitErr := cmd.Wait()
	wg.Wait()

	if waitErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		errMu.Lock()
		snippet := strings.Join(lastErrs, " | ")
		errMu.Unlock()
		if len(snippet) > 260 {
			snippet = snippet[len(snippet)-260:]
		}
		if snippet != "" {
			return fmt.Errorf("yt-dlp failed: %w (%s)", waitErr, snippet)
		}
		return fmt.Errorf("yt-dlp failed: %w", waitErr)
	}
	return nil
}

// runYTDLPDownload downloads a YouTube video or MP3 audio track using yt-dlp
// (+ ffmpeg for muxing/MP3 extraction), reporting real-time progress, speed,
// and status, and cleaning up temporary files on cancel or failure.
func (t *Task) runYTDLPDownload(ctx context.Context, onUpdate func(*Task)) error {
	t.mu.RLock()
	rawURL := t.URL
	ytdlpFormat := t.YTDLPFormat
	outputFmt := t.OutputFormat
	audioBitrate := t.AudioBitrate
	savePath := t.SavePath
	partPath := t.PartPath
	t.mu.RUnlock()

	canonicalURL, _, err := ValidateYouTubeURL(rawURL)
	if err != nil {
		t.fail(err.Error(), onUpdate)
		return err
	}
	safeFormat, err := ValidateYTDLPFormat(ytdlpFormat)
	if err != nil {
		t.fail(err.Error(), onUpdate)
		return err
	}
	if safeFormat == "" {
		safeFormat = "bestvideo[ext=mp4]+bestaudio[ext=m4a]/bestvideo+bestaudio/best"
	}

	ytdlpPath, err := FindYTDLP()
	if err != nil {
		t.fail(err.Error(), onUpdate)
		return err
	}

	ffmpegPath, ffmpegErr := FindFFmpeg()
	isMP3 := strings.EqualFold(outputFmt, "mp3")
	if isMP3 && ffmpegErr != nil {
		t.fail(ffmpegErr.Error(), onUpdate)
		return ffmpegErr
	}

	saveDir := filepath.Dir(savePath)
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		t.fail(fmt.Sprintf("failed to create directory: %v", err), onUpdate)
		return err
	}

	// Isolate temporary files inside a per-task temp directory next to the
	// target so cancellation/failure never leaves stray files in Downloads.
	tmpDir := partPath + ".ytdlp_tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.fail(fmt.Sprintf("failed to create temp directory: %v", err), onUpdate)
		return err
	}
	defer os.RemoveAll(tmpDir)

	outTemplate := filepath.Join(tmpDir, "output.%(ext)s")
	args := []string{
		"--no-playlist",
		"--no-warnings",
		"--newline",
		"--progress",
		"--progress-template", "download:MAGICDOWN_PROGRESS:%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s|%(progress.speed)s|%(progress.eta)s",
		"-f", safeFormat,
		"-o", outTemplate,
	}

	if ffmpegErr == nil && ffmpegPath != "" {
		args = append(args, "--ffmpeg-location", filepath.Dir(ffmpegPath))
	}

	if isMP3 {
		br := NormalizeAudioBitrate(audioBitrate)
		args = append(args,
			"-x",
			"--audio-format", "mp3",
			"--audio-quality", br,
		)
	} else {
		args = append(args, "--merge-output-format", "mp4")
	}

	args = append(args, "--", canonicalURL)

	atomic.StoreInt64(&t.DownloadedBytes, 0)
	var lastNotifyNs atomic.Int64

	onLine := func(line string) {
		ev, ok := parseYTDLPProgressLine(line)
		if !ok {
			return
		}
		if ev.Muxing {
			t.mu.Lock()
			t.Status = StatusMuxing
			t.Speed = 0
			t.ETA = 0
			t.mu.Unlock()
			if onUpdate != nil {
				onUpdate(t)
			}
			return
		}
		if ev.Downloaded > 0 {
			atomic.StoreInt64(&t.DownloadedBytes, ev.Downloaded)
		}
		t.mu.Lock()
		if ev.Total > 0 {
			t.TotalBytes = ev.Total
		}
		t.Speed = ev.Speed
		t.ETA = ev.ETA
		if t.Status != StatusDownloading {
			t.Status = StatusDownloading
		}
		t.mu.Unlock()
		nowNs := time.Now().UnixNano()
		prevNs := lastNotifyNs.Load()
		if onUpdate != nil && nowNs-prevNs >= int64(200*time.Millisecond) && lastNotifyNs.CompareAndSwap(prevNs, nowNs) {
			onUpdate(t)
		}
	}

	if err := ytdlpDownloadRunner(ctx, ytdlpPath, args, onLine); err != nil {
		if ctx.Err() != nil {
			t.mu.Lock()
			t.Speed = 0
			t.ETA = 0
			if t.Status == StatusDownloading || t.Status == StatusMuxing || t.Status == StatusProbing {
				t.Status = StatusCancelled
			}
			t.mu.Unlock()
			if onUpdate != nil {
				onUpdate(t)
			}
			return ctx.Err()
		}
		t.fail(err.Error(), onUpdate)
		return err
	}

	// Find finalized output file inside tmpDir.
	expectedExt := ".mp4"
	if isMP3 {
		expectedExt = ".mp3"
	}
	producedPath := filepath.Join(tmpDir, "output"+expectedExt)
	if _, statErr := os.Stat(producedPath); statErr != nil {
		// Fall back to any non-part file created in tmpDir.
		entries, readErr := os.ReadDir(tmpDir)
		if readErr == nil {
			for _, ent := range entries {
				if ent.IsDir() {
					continue
				}
				name := ent.Name()
				if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".ytdl") {
					continue
				}
				producedPath = filepath.Join(tmpDir, name)
				break
			}
		}
	}

	info, err := os.Stat(producedPath)
	if err != nil || info.IsDir() {
		errMsg := "yt-dlp finished without producing an output file"
		t.fail(errMsg, onUpdate)
		return errors.New(errMsg)
	}

	_ = os.Remove(savePath)
	if err := os.Rename(producedPath, savePath); err != nil {
		t.fail(fmt.Sprintf("failed to save yt-dlp output: %v", err), onUpdate)
		return err
	}

	finalSize := info.Size()
	now := time.Now()
	atomic.StoreInt64(&t.DownloadedBytes, finalSize)
	t.mu.Lock()
	t.TotalBytes = finalSize
	t.Speed = 0
	t.ETA = 0
	t.Status = StatusCompleted
	t.CompletedAt = &now
	t.ErrorMessage = ""
	t.mu.Unlock()
	if onUpdate != nil {
		onUpdate(t)
	}
	return nil
}

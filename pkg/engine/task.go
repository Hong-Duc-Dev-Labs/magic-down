package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var invalidFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SanitizeFilename cleans up a filename to be safe for Windows and cross-platform filesystems.
func SanitizeFilename(name string) string {
	clean := invalidFilenameChars.ReplaceAllString(name, "_")
	clean = strings.TrimSpace(clean)
	clean = strings.Trim(clean, ". ")
	if clean == "" {
		clean = "download.bin"
	}
	return clean
}

// ExtractFilename determines the suggested filename from Content-Disposition header or the URL.
func ExtractFilename(rawURL string, respHeader http.Header) string {
	if cd := respHeader.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename, ok := params["filename*"]; ok && filename != "" {
				// Handle RFC 5987 (e.g. UTF-8''filename.ext)
				parts := strings.SplitN(filename, "''", 2)
				if len(parts) == 2 {
					if unesc, err := url.PathUnescape(parts[1]); err == nil {
						return SanitizeFilename(unesc)
					}
				}
			}
			if filename, ok := params["filename"]; ok && filename != "" {
				return SanitizeFilename(filename)
			}
		}
	}

	if parsed, err := url.Parse(rawURL); err == nil {
		base := filepath.Base(parsed.Path)
		if base != "" && base != "." && base != "/" {
			if unesc, err := url.PathUnescape(base); err == nil {
				return SanitizeFilename(unesc)
			}
			return SanitizeFilename(base)
		}
	}

	return "download.bin"
}

// Probe inspects the remote URL to check content length, range support, and filename.
func (t *Task) Probe(ctx context.Context, client *http.Client, downloadDir string) error {
	t.mu.Lock()
	t.Status = StatusProbing
	t.mu.Unlock()

	// Try HEAD first
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, t.URL, nil)
	if err == nil {
		headReq.Header.Set("User-Agent", "MagicDown/1.0 (Windows NT 10.0; Win64; x64)")
		if t.Referrer != "" {
			headReq.Header.Set("Referer", t.Referrer)
		}
		if t.Cookies != "" {
			headReq.Header.Set("Cookie", t.Cookies)
		}
		for k, v := range t.Headers {
			headReq.Header.Set(k, v)
		}

		resp, headErr := client.Do(headReq)
		if headErr == nil && resp.StatusCode >= 200 && resp.StatusCode < 400 {
			defer resp.Body.Close()
			t.processProbeResponse(resp, downloadDir)
			return nil
		}
	}

	// If HEAD failed or returned 405 Method Not Allowed, fallback to ranged GET probe
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create probe request: %w", err)
	}
	getReq.Header.Set("Range", "bytes=0-0")
	getReq.Header.Set("User-Agent", "MagicDown/1.0 (Windows NT 10.0; Win64; x64)")
	if t.Referrer != "" {
		getReq.Header.Set("Referer", t.Referrer)
	}
	if t.Cookies != "" {
		getReq.Header.Set("Cookie", t.Cookies)
	}
	for k, v := range t.Headers {
		getReq.Header.Set(k, v)
	}

	resp, err := client.Do(getReq)
	if err != nil {
		return fmt.Errorf("probe GET failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("probe returned unexpected HTTP status: %d", resp.StatusCode)
	}

	t.processProbeResponse(resp, downloadDir)
	return nil
}

func (t *Task) processProbeResponse(resp *http.Response, downloadDir string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Detect filename if not set
	if t.Filename == "" {
		t.Filename = ExtractFilename(t.URL, resp.Header)
	}
	if fmtExt, ok := NormalizeImageFormat(t.ConvertFormat); ok {
		t.Filename = ReplaceExtension(t.Filename, fmtExt)
	}
	t.SavePath = filepath.Join(downloadDir, t.Filename)
	t.PartPath = t.SavePath + ".magicpart"

	// Detect HLS playlist (.m3u8)
	if IsHLSURL(t.URL, resp.Header.Get("Content-Type")) {
		t.IsHLS = true
		t.SupportsRanges = false
		return
	}

	// Check Range support
	acceptRanges := resp.Header.Get("Accept-Ranges")
	isRangedStatus := resp.StatusCode == http.StatusPartialContent
	contentRange := resp.Header.Get("Content-Range")

	totalSize := int64(-1)
	if resp.ContentLength > 0 {
		totalSize = resp.ContentLength
	}

	if isRangedStatus && contentRange != "" {
		t.SupportsRanges = true
		// Content-Range format: bytes 0-0/123456
		parts := strings.Split(contentRange, "/")
		if len(parts) == 2 {
			var parsedTotal int64
			if _, err := fmt.Sscanf(parts[1], "%d", &parsedTotal); err == nil && parsedTotal > 0 {
				totalSize = parsedTotal
			}
		}
	} else if strings.EqualFold(acceptRanges, "bytes") && totalSize > 0 {
		t.SupportsRanges = true
	} else {
		t.SupportsRanges = false
	}

	t.TotalBytes = totalSize

	// Partition segments if range supported and not already partitioned
	if t.SupportsRanges && t.TotalBytes > 0 && len(t.Segments) == 0 {
		if t.NumSegments <= 0 {
			t.NumSegments = 8
		}
		// If file is very small (< 64KB per segment), adjust segment count
		if t.TotalBytes < int64(t.NumSegments)*64*1024 {
			adjusted := int(t.TotalBytes / (64 * 1024))
			if adjusted < 1 {
				adjusted = 1
			}
			t.NumSegments = adjusted
		}

		segSize := t.TotalBytes / int64(t.NumSegments)
		t.Segments = make([]*Segment, t.NumSegments)
		for i := 0; i < t.NumSegments; i++ {
			start := int64(i) * segSize
			end := (int64(i)+1)*segSize - 1
			if i == t.NumSegments-1 {
				end = t.TotalBytes - 1
			}
			t.Segments[i] = &Segment{
				Index:      i,
				Start:      start,
				End:        end,
				Downloaded: 0,
				Status:     "idle",
			}
		}
	}
}

// Start initiates or resumes the download process.
func (t *Task) Start(parentCtx context.Context, client *http.Client, onUpdate func(*Task)) error {
	t.mu.Lock()
	if t.Status == StatusCompleted {
		t.mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(parentCtx)
	t.cancelFn = cancel
	t.activeCtx = ctx
	t.Status = StatusDownloading
	t.ErrorMessage = ""
	t.speedCalc = newSpeedCalculator(atomic.LoadInt64(&t.DownloadedBytes))
	supportsRanges := t.SupportsRanges
	isHLS := t.IsHLS || IsHLSURL(t.URL, "")
	totalBytes := t.TotalBytes
	numSegments := len(t.Segments)
	savePath := t.SavePath
	t.mu.Unlock()

	// Ensure download directory exists
	saveDir := filepath.Dir(savePath)
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		t.fail(fmt.Sprintf("failed to create directory: %v", err), onUpdate)
		return err
	}

	if isHLS {
		return t.runHLSDownload(ctx, client, onUpdate)
	}

	if supportsRanges && totalBytes > 0 && numSegments > 0 {
		return t.runRangedDownload(ctx, client, onUpdate)
	}

	return t.runSingleStreamDownload(ctx, client, onUpdate)
}

func (t *Task) runRangedDownload(ctx context.Context, client *http.Client, onUpdate func(*Task)) error {
	// Open or create .part file
	partFile, err := os.OpenFile(t.PartPath, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		t.fail(fmt.Sprintf("failed to open part file: %v", err), onUpdate)
		return err
	}
	defer partFile.Close()

	// Pre-allocate / truncate file to expected size
	if err := partFile.Truncate(t.TotalBytes); err != nil {
		// Non-fatal if filesystem doesn't support preallocation, but log
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(t.Segments))

	onChunk := func(delta int64) {
		newTotal := atomic.AddInt64(&t.DownloadedBytes, delta)
		speed, eta := t.speedCalc.Record(newTotal, t.TotalBytes)
		t.mu.Lock()
		t.Speed = speed
		t.ETA = eta
		t.mu.Unlock()
		if onUpdate != nil {
			onUpdate(t)
		}
	}

	for _, seg := range t.Segments {
		wg.Add(1)
		go func(s *Segment) {
			defer wg.Done()
			if atomic.LoadInt64(&s.Downloaded) >= (s.End - s.Start + 1) {
				s.setStatus("done")
				return
			}
			if err := downloadSegment(ctx, client, t.URL, t.Headers, t.Cookies, t.Referrer, s, partFile, onChunk); err != nil {
				if !errors.Is(err, context.Canceled) {
					errChan <- err
				}
			}
		}(seg)
	}

	wg.Wait()
	close(errChan)

	// Check if context was cancelled (pause or cancel)
	if ctx.Err() != nil {
		partFile.Sync()
		t.mu.Lock()
		if t.Status == StatusDownloading {
			t.Status = StatusPaused
		}
		t.Speed = 0
		t.ETA = 0
		t.mu.Unlock()
		if onUpdate != nil {
			onUpdate(t)
		}
		return ctx.Err()
	}

	// Check segment errors
	if len(errChan) > 0 {
		var firstErr error
		for e := range errChan {
			if firstErr == nil {
				firstErr = e
			}
		}
		t.fail(fmt.Sprintf("segment error: %v", firstErr), onUpdate)
		return firstErr
	}

	// Verify all segments completed
	var actualDownloaded int64
	for _, seg := range t.Segments {
		actualDownloaded += atomic.LoadInt64(&seg.Downloaded)
	}
	atomic.StoreInt64(&t.DownloadedBytes, actualDownloaded)

	partFile.Sync()
	partFile.Close()

	// Atomic finalize
	return t.finalizeFile(onUpdate)
}

func (t *Task) runSingleStreamDownload(ctx context.Context, client *http.Client, onUpdate func(*Task)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		t.fail(fmt.Sprintf("request creation error: %v", err), onUpdate)
		return err
	}

	req.Header.Set("User-Agent", "MagicDown/1.0 (Windows NT 10.0; Win64; x64)")
	if t.Referrer != "" {
		req.Header.Set("Referer", t.Referrer)
	}
	if t.Cookies != "" {
		req.Header.Set("Cookie", t.Cookies)
	}
	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}

	// Resume single stream if possible
	var startOffset int64 = 0
	if fi, err := os.Stat(t.PartPath); err == nil && fi.Size() > 0 {
		startOffset = fi.Size()
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			t.mu.Lock()
			t.Status = StatusPaused
			t.Speed = 0
			t.mu.Unlock()
			if onUpdate != nil {
				onUpdate(t)
			}
			return ctx.Err()
		}
		t.fail(fmt.Sprintf("download request failed: %v", err), onUpdate)
		return err
	}
	defer resp.Body.Close()

	var file *os.File
	if resp.StatusCode == http.StatusPartialContent && startOffset > 0 {
		file, err = os.OpenFile(t.PartPath, os.O_APPEND|os.O_WRONLY, 0666)
		atomic.StoreInt64(&t.DownloadedBytes, startOffset)
	} else {
		// Reset or start from 0
		startOffset = 0
		atomic.StoreInt64(&t.DownloadedBytes, 0)
		file, err = os.OpenFile(t.PartPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	}
	if err != nil {
		t.fail(fmt.Sprintf("failed to open file for writing: %v", err), onUpdate)
		return err
	}
	defer file.Close()

	if resp.ContentLength > 0 {
		t.mu.Lock()
		if t.TotalBytes <= 0 {
			t.TotalBytes = startOffset + resp.ContentLength
		}
		t.mu.Unlock()
	}

	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			file.Sync()
			t.mu.Lock()
			if t.Status == StatusDownloading {
				t.Status = StatusPaused
			}
			t.Speed = 0
			t.ETA = 0
			t.mu.Unlock()
			if onUpdate != nil {
				onUpdate(t)
			}
			return ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				t.fail(fmt.Sprintf("write error: %v", writeErr), onUpdate)
				return writeErr
			}
			newTotal := atomic.AddInt64(&t.DownloadedBytes, int64(n))
			t.mu.RLock()
			tb := t.TotalBytes
			t.mu.RUnlock()
			speed, eta := t.speedCalc.Record(newTotal, tb)
			t.mu.Lock()
			t.Speed = speed
			t.ETA = eta
			t.mu.Unlock()
			if onUpdate != nil {
				onUpdate(t)
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			if ctx.Err() != nil {
				t.mu.Lock()
				t.Status = StatusPaused
				t.Speed = 0
				t.mu.Unlock()
				return ctx.Err()
			}
			t.fail(fmt.Sprintf("stream read error: %v", readErr), onUpdate)
			return readErr
		}
	}

	file.Sync()
	file.Close()

	return t.finalizeFile(onUpdate)
}

func (t *Task) finalizeFile(onUpdate func(*Task)) error {
	t.mu.RLock()
	convertFmt := t.ConvertFormat
	partPath := t.PartPath
	savePath := t.SavePath
	t.mu.RUnlock()

	if fmtExt, ok := NormalizeImageFormat(convertFmt); ok {
		savePath = ReplaceExtension(savePath, fmtExt)
		if err := ConvertImageFile(partPath, savePath, fmtExt); err != nil {
			t.fail(fmt.Sprintf("image conversion to %s failed: %v", fmtExt, err), onUpdate)
			return err
		}
		_ = os.Remove(partPath)
		t.mu.Lock()
		t.SavePath = savePath
		t.Filename = filepath.Base(savePath)
		if fi, err := os.Stat(savePath); err == nil {
			atomic.StoreInt64(&t.DownloadedBytes, fi.Size())
			t.TotalBytes = fi.Size()
		}
		t.mu.Unlock()
	} else {
		// Atomic rename from .part to final destination
		// If final destination exists, remove first on Windows to avoid access conflict
		if _, err := os.Stat(savePath); err == nil {
			_ = os.Remove(savePath)
		}

		if err := os.Rename(partPath, savePath); err != nil {
			t.fail(fmt.Sprintf("atomic file rename failed: %v", err), onUpdate)
			return err
		}
	}

	now := time.Now()
	t.mu.Lock()
	t.Status = StatusCompleted
	t.CompletedAt = &now
	t.Speed = 0
	t.ETA = 0
	if t.TotalBytes <= 0 {
		t.TotalBytes = atomic.LoadInt64(&t.DownloadedBytes)
	}
	t.mu.Unlock()

	if onUpdate != nil {
		onUpdate(t)
	}
	return nil
}

func (t *Task) fail(errMsg string, onUpdate func(*Task)) {
	t.mu.Lock()
	t.Status = StatusFailed
	t.ErrorMessage = errMsg
	t.Speed = 0
	t.ETA = 0
	t.mu.Unlock()
	if onUpdate != nil {
		onUpdate(t)
	}
}

// Pause stops active workers and updates task status to Paused.
func (t *Task) Pause() {
	t.mu.Lock()
	cancel := t.cancelFn
	t.Status = StatusPaused
	t.Speed = 0
	t.ETA = 0
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// Cancel stops active workers and marks the task as Cancelled.
func (t *Task) Cancel() {
	t.mu.Lock()
	cancel := t.cancelFn
	t.Status = StatusCancelled
	t.Speed = 0
	t.ETA = 0
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// ProgressPercentage returns a percentage from 0.0 to 100.0.
func (t *Task) ProgressPercentage() float64 {
	t.mu.RLock()
	total := t.TotalBytes
	status := t.Status
	t.mu.RUnlock()
	downloaded := atomic.LoadInt64(&t.DownloadedBytes)
	if total <= 0 {
		if status == StatusCompleted {
			return 100.0
		}
		return 0.0
	}
	pct := (float64(downloaded) / float64(total)) * 100.0
	if pct > 100.0 {
		return 100.0
	}
	return pct
}

// GetStatus returns the current status safely.
func (t *Task) GetStatus() TaskStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status
}

// SetStatus sets the current status safely.
func (t *Task) SetStatus(s TaskStatus) {
	t.mu.Lock()
	t.Status = s
	t.mu.Unlock()
}

// GetDownloadedBytes returns downloaded byte count atomically.
func (t *Task) GetDownloadedBytes() int64 {
	return atomic.LoadInt64(&t.DownloadedBytes)
}

// Clone returns a thread-safe deep snapshot of the task for UI rendering, IPC, or JSON persistence.
func (t *Task) Clone() *Task {
	t.mu.RLock()
	defer t.mu.RUnlock()

	cp := &Task{
		ID:              t.ID,
		URL:             t.URL,
		Filename:        t.Filename,
		SavePath:        t.SavePath,
		PartPath:        t.PartPath,
		TotalBytes:      t.TotalBytes,
		DownloadedBytes: atomic.LoadInt64(&t.DownloadedBytes),
		Speed:           t.Speed,
		ETA:             t.ETA,
		Status:          t.Status,
		SupportsRanges:  t.SupportsRanges,
		IsHLS:           t.IsHLS,
		ConvertFormat:   t.ConvertFormat,
		NumSegments:     t.NumSegments,
		ErrorMessage:    t.ErrorMessage,
		Referrer:        t.Referrer,
		Cookies:         t.Cookies,
		CreatedAt:       t.CreatedAt,
	}
	if t.CompletedAt != nil {
		ts := *t.CompletedAt
		cp.CompletedAt = &ts
	}
	if len(t.Headers) > 0 {
		cp.Headers = make(map[string]string, len(t.Headers))
		for k, v := range t.Headers {
			cp.Headers[k] = v
		}
	}
	if len(t.Segments) > 0 {
		cp.Segments = make([]*Segment, len(t.Segments))
		for i, seg := range t.Segments {
			cp.Segments[i] = &Segment{
				Index:      seg.Index,
				Start:      seg.Start,
				End:        seg.End,
				Downloaded: atomic.LoadInt64(&seg.Downloaded),
				Status:     seg.getStatus(),
			}
		}
	}
	return cp
}

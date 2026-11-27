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

// retryBudget returns how many retry attempts this task may make per operation.
func (t *Task) retryBudget() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.retryLimit
}

// noteRetry records one retry attempt and keeps the task surface showing
// Downloading while it backs off.
func (t *Task) noteRetry(onUpdate func(*Task)) {
	t.mu.Lock()
	t.Retries++
	t.Status = StatusDownloading
	t.ErrorMessage = ""
	t.mu.Unlock()
	if onUpdate != nil {
		onUpdate(t)
	}
}

// Probe inspects the remote URL to check content length, range support, and filename.
// Transient probe failures are retried with backoff up to the task's retry budget.
func (t *Task) Probe(ctx context.Context, client *http.Client, downloadDir string) error {
	maxRetries := t.retryBudget()
	var lastErr error
	for attempt := 0; ; attempt++ {
		t.mu.Lock()
		t.Status = StatusProbing
		t.mu.Unlock()

		retryable, err := t.probeOnce(ctx, client, downloadDir)
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !retryable || attempt >= maxRetries {
			return lastErr
		}
		t.noteRetry(nil)
		if werr := waitRetry(ctx, attempt+1); werr != nil {
			return werr
		}
	}
}

// probeOnce performs a single HEAD/GET probe and reports whether the failure is retryable.
func (t *Task) probeOnce(ctx context.Context, client *http.Client, downloadDir string) (retryable bool, err error) {
	t.mu.RLock()
	taskURL := t.URL
	referrer := t.Referrer
	cookies := t.Cookies
	var headers map[string]string
	if len(t.Headers) > 0 {
		headers = make(map[string]string, len(t.Headers))
		for k, v := range t.Headers {
			headers[k] = v
		}
	}
	t.mu.RUnlock()

	// Try HEAD first
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, taskURL, nil)
	if err == nil {
		headReq.Header.Set("User-Agent", "MagicDown/1.0 (Windows NT 10.0; Win64; x64)")
		if referrer != "" {
			headReq.Header.Set("Referer", referrer)
		}
		if cookies != "" {
			headReq.Header.Set("Cookie", cookies)
		}
		for k, v := range headers {
			headReq.Header.Set(k, v)
		}

		resp, headErr := client.Do(headReq)
		if headErr == nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				defer resp.Body.Close()
				t.processProbeResponse(resp, downloadDir)
				return false, nil
			}
			_ = resp.Body.Close()
		}
	}

	// If HEAD failed or returned 405 Method Not Allowed, fallback to ranged GET probe
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, taskURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create probe request: %w", err)
	}
	getReq.Header.Set("Range", "bytes=0-0")
	getReq.Header.Set("User-Agent", "MagicDown/1.0 (Windows NT 10.0; Win64; x64)")
	if referrer != "" {
		getReq.Header.Set("Referer", referrer)
	}
	if cookies != "" {
		getReq.Header.Set("Cookie", cookies)
	}
	for k, v := range headers {
		getReq.Header.Set(k, v)
	}

	resp, err := client.Do(getReq)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, fmt.Errorf("probe GET failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return retryableStatus(resp.StatusCode), fmt.Errorf("probe returned unexpected HTTP status: %d", resp.StatusCode)
	}

	t.processProbeResponse(resp, downloadDir)
	return false, nil
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
	if outFmt, ok := NormalizeOutputFormat(t.OutputFormat); ok && outFmt == "mp3" {
		t.Filename = ReplaceExtension(t.Filename, "mp3")
		if t.Category == "" {
			t.Category = CategoryMusic
		}
	}
	if t.Category == "" {
		t.Category = CategoryForFilename(t.Filename, t.URL)
	}
	if t.SavePath == "" {
		_ = os.MkdirAll(downloadDir, 0755)
		t.SavePath = UniqueFilePath(downloadDir, t.Filename)
		t.Filename = filepath.Base(t.SavePath)
	}
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
	// A pause or cancel may land between queueing and this call.
	if t.Status == StatusPaused || t.Status == StatusCancelled {
		t.mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	t.cancelFn = cancel
	t.activeCtx = ctx
	t.Status = StatusDownloading
	t.ErrorMessage = ""
	t.speedCalc = newSpeedCalculator(atomic.LoadInt64(&t.DownloadedBytes))
	supportsRanges := t.SupportsRanges
	plan := t.Plan
	audioURL := t.AudioURL
	outputFmt := t.OutputFormat
	ytdlpFmt := t.YTDLPFormat
	isHLS := t.IsHLS || (plan == nil && audioURL == "" && ytdlpFmt == "" && IsHLSURL(t.URL, ""))
	totalBytes := t.TotalBytes
	numSegments := len(t.Segments)
	savePath := t.SavePath
	t.mu.Unlock()

	// Ensure ffmpeg is available before spending bandwidth on MP3 conversion.
	if outFmt, ok := NormalizeOutputFormat(outputFmt); ok && outFmt == "mp3" {
		if _, err := FindFFmpeg(); err != nil {
			t.fail(err.Error(), onUpdate)
			return err
		}
	}

	// Ensure download directory exists
	saveDir := filepath.Dir(savePath)
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		t.fail(fmt.Sprintf("failed to create directory: %v", err), onUpdate)
		return err
	}

	if ytdlpFmt != "" {
		return t.runYTDLPDownload(ctx, onUpdate)
	}

	if plan != nil {
		return t.runPlanDownload(ctx, client, onUpdate)
	}

	if audioURL != "" {
		return t.runMuxedDownload(ctx, client, onUpdate)
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
	t.mu.RLock()
	partPath := t.PartPath
	totalBytes := t.TotalBytes
	taskURL := t.URL
	cookies := t.Cookies
	referrer := t.Referrer
	numSegments := t.NumSegments
	onStateDirty := t.onStateDirty
	var headers map[string]string
	if len(t.Headers) > 0 {
		headers = make(map[string]string, len(t.Headers))
		for k, v := range t.Headers {
			headers[k] = v
		}
	}
	initialSegs := make([]*Segment, len(t.Segments))
	copy(initialSegs, t.Segments)
	t.mu.RUnlock()

	// Open or create .part file
	partFile, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		t.fail(fmt.Sprintf("failed to open part file: %v", err), onUpdate)
		return err
	}
	defer partFile.Close()

	// Pre-allocate / truncate file to expected size
	if err := partFile.Truncate(totalBytes); err != nil {
		// Non-fatal if filesystem doesn't support preallocation, but log
	}

	onChunk := func(delta int64) {
		newTotal := atomic.AddInt64(&t.DownloadedBytes, delta)
		speed, eta := t.speedCalc.Record(newTotal, totalBytes)
		t.mu.Lock()
		t.Speed = speed
		t.ETA = eta
		t.mu.Unlock()
		if onUpdate != nil {
			onUpdate(t)
		}
	}

	limiter := t.limiter
	taskLimiter := t.taskLimiter
	maxRetries := t.retryBudget()
	onRetry := func() { t.noteRetry(onUpdate) }

	pending := make([]*Segment, 0, len(initialSegs))
	for _, seg := range initialSegs {
		if atomic.LoadInt64(&seg.Downloaded) >= seg.getEnd()-seg.Start+1 {
			seg.setStatus("done")
			continue
		}
		pending = append(pending, seg)
	}

	pendingCh := make(chan *Segment, len(pending))
	for _, seg := range pending {
		pendingCh <- seg
	}

	coord := newSegmentCoordinator(t, defaultMinStealRemaining, func() {
		if onStateDirty != nil {
			onStateDirty()
		}
		if onUpdate != nil {
			onUpdate(t)
		}
	})

	numWorkers := numSegments
	if numWorkers <= 0 {
		numWorkers = len(initialSegs)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				var seg *Segment
				select {
				case seg = <-pendingCh:
				default:
					seg = coord.stealSegment()
				}
				if seg == nil {
					return
				}
				if atomic.LoadInt64(&seg.Downloaded) >= seg.getEnd()-seg.Start+1 {
					seg.setStatus("done")
					continue
				}
				if err := downloadSegment(ctx, client, taskURL, headers, cookies, referrer, seg, partFile, taskLimiter, limiter, maxRetries, onChunk, onRetry); err != nil {
					if !errors.Is(err, context.Canceled) {
						errMu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						errMu.Unlock()
					}
					return
				}
			}
		}()
	}

	wg.Wait()

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
	if firstErr != nil {
		t.fail(fmt.Sprintf("segment error: %v", firstErr), onUpdate)
		return firstErr
	}

	// Verify all segments completed
	var actualDownloaded int64
	t.mu.RLock()
	for _, seg := range t.Segments {
		actualDownloaded += atomic.LoadInt64(&seg.Downloaded)
	}
	t.mu.RUnlock()
	atomic.StoreInt64(&t.DownloadedBytes, actualDownloaded)

	partFile.Sync()
	partFile.Close()

	// Atomic finalize
	return t.finalizeFile(onUpdate)
}

func (t *Task) runSingleStreamDownload(ctx context.Context, client *http.Client, onUpdate func(*Task)) error {
	maxRetries := t.retryBudget()
	for attempt := 0; ; attempt++ {
		retryable, err := t.runSingleStreamAttempt(ctx, client, onUpdate)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			t.markPaused(onUpdate)
			return ctx.Err()
		}
		if !retryable || attempt >= maxRetries {
			t.fail(fmt.Sprintf("download failed: %v", err), onUpdate)
			return err
		}
		t.noteRetry(onUpdate)
		if werr := waitRetry(ctx, attempt+1); werr != nil {
			t.markPaused(onUpdate)
			return werr
		}
	}
}

// runSingleStreamAttempt performs one non-ranged (or whole-file ranged) transfer,
// resuming from the existing .magicpart size when the server honours Range.
func (t *Task) runSingleStreamAttempt(ctx context.Context, client *http.Client, onUpdate func(*Task)) (retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return false, fmt.Errorf("request creation error: %w", err)
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
	if fi, statErr := os.Stat(t.PartPath); statErr == nil && fi.Size() > 0 {
		startOffset = fi.Size()
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	resuming := resp.StatusCode == http.StatusPartialContent && startOffset > 0
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return retryableStatus(resp.StatusCode), fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}

	var file *os.File
	if resuming {
		file, err = os.OpenFile(t.PartPath, os.O_APPEND|os.O_WRONLY, 0666)
		atomic.StoreInt64(&t.DownloadedBytes, startOffset)
	} else {
		// Reset or start from 0
		startOffset = 0
		atomic.StoreInt64(&t.DownloadedBytes, 0)
		file, err = os.OpenFile(t.PartPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	}
	if err != nil {
		return false, fmt.Errorf("failed to open file for writing: %w", err)
	}
	defer file.Close()

	var expectedSize int64
	if resp.ContentLength > 0 {
		expectedSize = startOffset + resp.ContentLength
		t.mu.Lock()
		if t.TotalBytes <= 0 {
			t.TotalBytes = expectedSize
		}
		t.mu.Unlock()
	}

	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			_ = file.Sync()
			return false, ctx.Err()
		default:
		}

		maxRead := cappedReadBufferSize(len(buf), t.taskLimiter, t.limiter)
		n, readErr := resp.Body.Read(buf[:maxRead])
		if n > 0 {
			if waitErr := t.waitLimiters(ctx, int64(n)); waitErr != nil {
				_ = file.Sync()
				return false, waitErr
			}
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				return false, fmt.Errorf("write error: %w", writeErr)
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
				return false, ctx.Err()
			}
			return true, fmt.Errorf("stream read error: %w", readErr)
		}
	}

	_ = file.Sync()
	if expectedSize > 0 {
		if fi, statErr := os.Stat(t.PartPath); statErr == nil && fi.Size() < expectedSize {
			return true, fmt.Errorf("stream truncated at %d of %d bytes", fi.Size(), expectedSize)
		}
	}
	_ = file.Close()

	return false, t.finalizeFile(onUpdate)
}

func (t *Task) finalizeFile(onUpdate func(*Task)) error {
	t.mu.RLock()
	convertFmt := t.ConvertFormat
	outputFmt := t.OutputFormat
	audioBitrate := t.AudioBitrate
	activeCtx := t.activeCtx
	partPath := t.PartPath
	savePath := t.SavePath
	t.mu.RUnlock()

	if activeCtx == nil {
		activeCtx = context.Background()
	}

	if outFmt, ok := NormalizeOutputFormat(outputFmt); ok && outFmt == "mp3" {
		t.mu.Lock()
		t.Status = StatusMuxing
		t.Speed = 0
		t.ETA = 0
		t.mu.Unlock()
		if onUpdate != nil {
			onUpdate(t)
		}

		savePath = ReplaceExtension(savePath, "mp3")
		if err := ConvertToMP3(activeCtx, partPath, savePath, audioBitrate); err != nil {
			if activeCtx.Err() != nil {
				t.markPaused(onUpdate)
				return activeCtx.Err()
			}
			t.fail(err.Error(), onUpdate)
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
	} else if fmtExt, ok := NormalizeImageFormat(convertFmt); ok {
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

// waitWorkerDone blocks until the task's most recent worker goroutine has
// exited, or until ctx is cancelled.
func (t *Task) waitWorkerDone(ctx context.Context) error {
	t.mu.RLock()
	done := t.workerDone
	t.mu.RUnlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// probeURLMetadata inspects a remote URL via HEAD (with ranged GET fallback)
// without mutating any Task fields. Used by RefreshTaskAddress to validate a
// replacement link before resuming.
func probeURLMetadata(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	headers map[string]string,
	cookies string,
	referrer string,
) (totalSize int64, supportsRanges bool, err error) {
	applyHeaders := func(req *http.Request) {
		req.Header.Set("User-Agent", "MagicDown/1.0 (Windows NT 10.0; Win64; x64)")
		if referrer != "" {
			req.Header.Set("Referer", referrer)
		}
		if cookies != "" {
			req.Header.Set("Cookie", cookies)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	// Try HEAD first
	if headReq, reqErr := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil); reqErr == nil {
		applyHeaders(headReq)
		if resp, headErr := client.Do(headReq); headErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if resp.ContentLength > 0 && strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes") {
					return resp.ContentLength, true, nil
				}
			}
		}
	}

	// Fallback to ranged GET probe (bytes=0-0)
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return -1, false, fmt.Errorf("failed to create probe request: %w", err)
	}
	getReq.Header.Set("Range", "bytes=0-0")
	applyHeaders(getReq)

	resp, err := client.Do(getReq)
	if err != nil {
		return -1, false, fmt.Errorf("probe GET failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return -1, false, fmt.Errorf("probe returned unexpected HTTP status: %d", resp.StatusCode)
	}

	totalSize = int64(-1)
	if resp.ContentLength > 0 {
		totalSize = resp.ContentLength
	}
	contentRange := resp.Header.Get("Content-Range")
	if resp.StatusCode == http.StatusPartialContent && contentRange != "" {
		supportsRanges = true
		parts := strings.Split(contentRange, "/")
		if len(parts) == 2 {
			var parsedTotal int64
			if _, scanErr := fmt.Sscanf(parts[1], "%d", &parsedTotal); scanErr == nil && parsedTotal > 0 {
				totalSize = parsedTotal
			}
		}
	} else if strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes") && totalSize > 0 {
		supportsRanges = true
	}

	return totalSize, supportsRanges, nil
}

// waitLimiters enforces per-task speed limit first, then global engine limit.
func (t *Task) waitLimiters(ctx context.Context, n int64) error {
	return waitOrderedLimiters(ctx, n, t.taskLimiter, t.limiter)
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
		AudioURL:        t.AudioURL,
		Category:        t.Category,
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
		Plan:            t.Plan.Clone(),
		ConvertFormat:   t.ConvertFormat,
		TargetFormat:    t.TargetFormat,
		OutputFormat:    t.OutputFormat,
		AudioBitrate:    t.AudioBitrate,
		YTDLPFormat:     t.YTDLPFormat,
		SpeedLimitKBps:  t.SpeedLimitKBps,
		NumSegments:     t.NumSegments,
		ErrorMessage:    t.ErrorMessage,
		Retries:         t.Retries,
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
				End:        seg.getEnd(),
				Downloaded: atomic.LoadInt64(&seg.Downloaded),
				Status:     seg.getStatus(),
			}
		}
	}
	return cp
}

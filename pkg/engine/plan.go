package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// maxPlanParts bounds a browser-supplied plan. A template-driven DASH
// presentation can list thousands of short segments; 4000 covers multi-hour
// videos (4s segments) while keeping memory and native-message size sane.
const maxPlanParts = 4000

// PlanPart is one piece of a media presentation. A part is either:
//   - a byte range [Start, End] (inclusive) of its URL,
//   - an open-ended range from Start to EOF (End == 0, Start > 0), or
//   - the whole resource (Start == 0, End == 0).
//
// Empty URL inherits DownloadPlan.URL. Parts are concatenated in slice order.
type PlanPart struct {
	URL   string `json:"url,omitempty"`
	Start int64  `json:"start,omitempty"`
	End   int64  `json:"end,omitempty"`
}

// DownloadPlan describes a media download assembled from byte ranges or
// segment URLs discovered in a page (DASH/MPD, MSE-fed players).
//
// Kind "range" means every part is a byte range of one URL (Facebook-style
// single-file DASH); "segments" means separate segment URLs (Weibo CDN .m4s).
// Both are concatenated into one output file, so no new on-disk format exists.
type DownloadPlan struct {
	Kind         string     `json:"kind,omitempty"`
	URL          string     `json:"url,omitempty"`
	Format       string     `json:"format,omitempty"`
	TargetFormat string     `json:"target_format,omitempty"`
	OutputFormat string     `json:"output_format,omitempty"`
	AudioBitrate string     `json:"audio_bitrate,omitempty"`
	Parts        []PlanPart `json:"parts"`
}

// ParseDownloadPlan decodes and validates a plan supplied by the browser
// extension. fallbackURL is the task URL and is inherited by parts without one.
func ParseDownloadPlan(raw []byte, fallbackURL string) (*DownloadPlan, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty download plan")
	}
	var p DownloadPlan
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid download plan JSON: %w", err)
	}
	return p.Normalize(fallbackURL)
}

// Normalize validates a plan, fills inherited URLs, and derives a bounded
// format label. It never mutates the receiver.
func (p *DownloadPlan) Normalize(fallbackURL string) (*DownloadPlan, error) {
	if p == nil {
		return nil, errors.New("empty download plan")
	}
	out := &DownloadPlan{
		Kind:         strings.ToLower(strings.TrimSpace(p.Kind)),
		URL:          strings.TrimSpace(p.URL),
		Format:       normalizePlanFormat(p.Format),
		TargetFormat: strings.ToLower(strings.TrimSpace(p.TargetFormat)),
		OutputFormat: strings.ToLower(strings.TrimSpace(p.OutputFormat)),
		AudioBitrate: strings.ToLower(strings.TrimSpace(p.AudioBitrate)),
	}
	if out.URL == "" {
		out.URL = strings.TrimSpace(fallbackURL)
	}
	if len(p.Parts) == 0 {
		return nil, errors.New("download plan has no parts")
	}
	if len(p.Parts) > maxPlanParts {
		return nil, fmt.Errorf("download plan has %d parts (max %d)", len(p.Parts), maxPlanParts)
	}

	seen := make(map[string]struct{})
	for i, part := range p.Parts {
		partURL := strings.TrimSpace(part.URL)
		if partURL == "" {
			partURL = out.URL
		}
		if partURL == "" {
			return nil, fmt.Errorf("part %d has no URL", i)
		}
		if err := validatePlanURL(partURL); err != nil {
			return nil, fmt.Errorf("part %d: %w", i, err)
		}
		if part.Start < 0 {
			return nil, fmt.Errorf("part %d: negative range start %d", i, part.Start)
		}
		if part.End < 0 {
			return nil, fmt.Errorf("part %d: negative range end %d", i, part.End)
		}
		if part.End != 0 && part.End < part.Start {
			return nil, fmt.Errorf("part %d: range end %d < start %d", i, part.End, part.Start)
		}
		if part.End != 0 && part.End-part.Start > 1<<40 {
			return nil, fmt.Errorf("part %d: range too large", i)
		}
		seen[partURL] = struct{}{}
		out.Parts = append(out.Parts, PlanPart{URL: partURL, Start: part.Start, End: part.End})
	}

	switch out.Kind {
	case "":
		if len(seen) == 1 {
			out.Kind = "range"
		} else {
			out.Kind = "segments"
		}
	case "range":
		if len(seen) != 1 {
			return nil, fmt.Errorf("range plan spans %d URLs", len(seen))
		}
	case "segments":
	default:
		return nil, fmt.Errorf("unknown plan kind %q", out.Kind)
	}
	return out, nil
}

func validatePlanURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("URL has no host")
	}
	return nil
}

// normalizePlanFormat keeps a short, safe extension token ("mp4", "ts", ...).
func normalizePlanFormat(format string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), ".")) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 8 {
		s = s[:8]
	}
	return s
}

// KnownSize returns the exact concatenated size when every part is a bounded
// byte range, and 0 when a full resource or open-ended range is present.
func (p *DownloadPlan) KnownSize() int64 {
	if p == nil || len(p.Parts) == 0 {
		return 0
	}
	var total int64
	for _, part := range p.Parts {
		if part.End == 0 {
			return 0
		}
		total += part.End - part.Start + 1
	}
	return total
}

// Extension returns the output extension implied by the plan, if any.
func (p *DownloadPlan) Extension() string {
	if p == nil {
		return ""
	}
	if p.Format != "" {
		return p.Format
	}
	for _, part := range p.Parts {
		if ext := mediaURLExtension(part.URL); ext != "" && ext != "m3u8" && ext != "mpd" {
			return ext
		}
	}
	return ""
}

// Clone deep-copies the plan (nil-safe) for task snapshots and persistence.
func (p *DownloadPlan) Clone() *DownloadPlan {
	if p == nil {
		return nil
	}
	cp := &DownloadPlan{Kind: p.Kind, URL: p.URL, Format: p.Format}
	if len(p.Parts) > 0 {
		cp.Parts = make([]PlanPart, len(p.Parts))
		copy(cp.Parts, p.Parts)
	}
	return cp
}

// FirstURL is used when the caller supplied no task URL.
func (p *DownloadPlan) FirstURL() string {
	if p == nil {
		return ""
	}
	if p.URL != "" {
		return p.URL
	}
	if len(p.Parts) > 0 {
		return p.Parts[0].URL
	}
	return ""
}

func mediaURLExtension(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(u.Path), "."))
	if len(ext) > 8 {
		return ""
	}
	switch ext {
	case "mp4", "m4s", "m4v", "m4a", "webm", "ts", "mkv", "mov", "mp3", "aac":
		return ext
	}
	return ""
}

// runPlanDownload fetches every plan part (resuming partially downloaded parts),
// then concatenates them in plan order into PartPath and finalizes atomically.
func (t *Task) runPlanDownload(ctx context.Context, client *http.Client, onUpdate func(*Task)) error {
	t.mu.RLock()
	plan := t.Plan
	partPath := t.PartPath
	t.mu.RUnlock()

	if plan == nil || len(plan.Parts) == 0 {
		err := errors.New("download plan is empty")
		t.fail(err.Error(), onUpdate)
		return err
	}

	partDir := partPath + ".plan_parts"
	if err := os.MkdirAll(partDir, 0755); err != nil {
		t.fail(fmt.Sprintf("failed to create plan temp dir: %v", err), onUpdate)
		return err
	}

	knownTotal := plan.KnownSize()
	if knownTotal > 0 {
		t.mu.Lock()
		t.TotalBytes = knownTotal
		t.mu.Unlock()
	}

	workers := t.PlanWorkerCount(len(plan.Parts))
	type job struct {
		idx  int
		part PlanPart
	}
	jobs := make(chan job, len(plan.Parts))
	for i, part := range plan.Parts {
		jobs <- job{idx: i, part: part}
	}
	close(jobs)

	var completed int64
	var wg sync.WaitGroup
	errChan := make(chan error, len(plan.Parts))

	onChunk := func(delta int64) {
		if delta <= 0 {
			return
		}
		newTotal := atomic.AddInt64(&t.DownloadedBytes, delta)
		est := knownTotal
		if est <= 0 {
			done := atomic.LoadInt64(&completed)
			if done > 0 {
				est = newTotal * int64(len(plan.Parts)) / done
			}
		}
		speed, eta := t.speedCalc.Record(newTotal, est)
		t.mu.Lock()
		if est > 0 {
			t.TotalBytes = est
		}
		t.Speed = speed
		t.ETA = eta
		t.mu.Unlock()
		if onUpdate != nil {
			onUpdate(t)
		}
	}
	onRetry := func() { t.noteRetry(onUpdate) }

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					return
				}
				dest := filepath.Join(partDir, fmt.Sprintf("part_%05d.bin", j.idx))
				if err := t.fetchPlanPart(ctx, client, j.part, dest, onChunk, onRetry); err != nil {
					if ctx.Err() == nil || !errors.Is(err, context.Canceled) {
						errChan <- fmt.Errorf("plan part %d: %w", j.idx, err)
					}
					return
				}
				atomic.AddInt64(&completed, 1)
			}
		}()
	}
	wg.Wait()
	close(errChan)

	if ctx.Err() != nil {
		t.markPaused(onUpdate)
		return ctx.Err()
	}
	if len(errChan) > 0 {
		firstErr := <-errChan
		t.fail(fmt.Sprintf("media segment download error: %v", firstErr), onUpdate)
		return firstErr
	}

	out, err := os.OpenFile(partPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		t.fail(fmt.Sprintf("failed to create output file: %v", err), onUpdate)
		return err
	}
	var totalWritten int64
	for i := range plan.Parts {
		partFile := filepath.Join(partDir, fmt.Sprintf("part_%05d.bin", i))
		in, openErr := os.Open(partFile)
		if openErr != nil {
			_ = out.Close()
			t.fail(fmt.Sprintf("missing media part %d: %v", i, openErr), onUpdate)
			return openErr
		}
		n, copyErr := io.Copy(out, in)
		_ = in.Close()
		if copyErr != nil {
			_ = out.Close()
			t.fail(fmt.Sprintf("media merge error: %v", copyErr), onUpdate)
			return copyErr
		}
		totalWritten += n
	}
	_ = out.Sync()
	_ = out.Close()
	_ = os.RemoveAll(partDir)

	if totalWritten == 0 {
		err := errors.New("downloaded media is empty")
		t.fail(err.Error(), onUpdate)
		return err
	}

	atomic.StoreInt64(&t.DownloadedBytes, totalWritten)
	t.mu.Lock()
	t.TotalBytes = totalWritten
	t.mu.Unlock()

	return t.finalizeFile(onUpdate)
}

// PlanWorkerCount picks a conservative fan-out: range plans usually have two
// parts (init + media) while segment plans have many short files.
func (t *Task) PlanWorkerCount(parts int) int {
	t.mu.RLock()
	workers := t.NumSegments
	kind := ""
	if t.Plan != nil {
		kind = t.Plan.Kind
	}
	t.mu.RUnlock()
	if workers <= 0 {
		workers = 8
	}
	if kind == "range" && workers > 2 {
		workers = 2
	}
	if workers > parts {
		workers = parts
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

// fetchPlanPart downloads one part with retries, resuming from the bytes that
// already reached dest. A server that ignores Range is handled by discarding
// the prefix it re-sends, so retries never corrupt the output.
func (t *Task) fetchPlanPart(ctx context.Context, client *http.Client, part PlanPart, dest string, onChunk func(int64), onRetry func()) error {
	maxRetries := t.retryBudget()
	var lastErr error
	for attempt := 0; ; attempt++ {
		retryable, err := t.fetchPlanPartOnce(ctx, client, part, dest, onChunk)
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
		if onRetry != nil {
			onRetry()
		}
		if werr := waitRetry(ctx, attempt+1); werr != nil {
			return werr
		}
	}
}

func (t *Task) fetchPlanPartOnce(ctx context.Context, client *http.Client, part PlanPart, dest string, onChunk func(int64)) (bool, error) {
	var want int64 = -1 // unknown total for this part
	if part.End > 0 {
		want = part.End - part.Start + 1
	}

	have := int64(0)
	if fi, statErr := os.Stat(dest); statErr == nil && fi.Size() > 0 {
		have = fi.Size()
	}
	if want > 0 && have >= want {
		return false, nil
	}

	reqStart := part.Start + have
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, part.URL, nil)
	if err != nil {
		return false, fmt.Errorf("request creation error: %w", err)
	}
	t.applyHeaders(req)
	if reqStart > 0 || part.End > 0 {
		rangeEnd := ""
		if part.End > 0 {
			rangeEnd = strconv.FormatInt(part.End, 10)
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%s", reqStart, rangeEnd))
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	skip := int64(0)
	switch resp.StatusCode {
	case http.StatusPartialContent:
		actualStart := parseContentRangeStart(resp.Header.Get("Content-Range"))
		if actualStart >= 0 && actualStart != reqStart {
			if actualStart > reqStart {
				return false, fmt.Errorf("server returned range starting at %d, wanted %d", actualStart, reqStart)
			}
			skip = reqStart - actualStart
		}
	case http.StatusOK:
		// Range ignored: the body starts at byte 0, so drop what we already have.
		skip = reqStart
	case http.StatusRequestedRangeNotSatisfiable:
		if have > 0 && (want < 0 || have >= want) {
			return false, nil
		}
		return false, fmt.Errorf("HTTP 416 for bytes=%d-", reqStart)
	default:
		return retryableStatus(resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if have > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(dest, flags, 0644)
	if err != nil {
		return false, err
	}

	if skip > 0 {
		if _, err := io.CopyN(io.Discard, resp.Body, skip); err != nil {
			_ = out.Close()
			if errors.Is(err, io.EOF) {
				return false, fmt.Errorf("truncated response while skipping %d bytes", skip)
			}
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return true, err
		}
	}

	buf := make([]byte, 64*1024)
	remaining := int64(-1)
	if want > 0 {
		remaining = want - have
	}
	var written int64
	for {
		readSize := int64(len(buf))
		if remaining > 0 && remaining < readSize {
			readSize = remaining
		}
		if readSize <= 0 {
			break
		}
		n, readErr := resp.Body.Read(buf[:readSize])
		if n > 0 {
			if waitErr := t.waitLimiters(ctx, int64(n)); waitErr != nil {
				_ = out.Close()
				return false, waitErr
			}
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				_ = out.Close()
				return false, writeErr
			}
			written += int64(n)
			if onChunk != nil {
				onChunk(int64(n))
			}
			if remaining > 0 {
				remaining -= int64(n)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			_ = out.Close()
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return true, readErr
		}
	}
	if err := out.Close(); err != nil {
		return false, err
	}

	if want > 0 {
		total := have + written
		if total < want {
			return true, fmt.Errorf("part ended early at %d of %d bytes", total, want)
		}
	}
	return false, nil
}

// parseContentRangeStart reads the first byte offset from "bytes 100-200/300".
func parseContentRangeStart(header string) int64 {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(header), "bytes") {
		return -1
	}
	rest := strings.TrimSpace(header[len("bytes"):])
	slash := strings.IndexByte(rest, '/')
	if slash >= 0 {
		rest = rest[:slash]
	}
	dash := strings.IndexByte(rest, '-')
	if dash < 0 {
		return -1
	}
	start, err := strconv.ParseInt(strings.TrimSpace(rest[:dash]), 10, 64)
	if err != nil || start < 0 {
		return -1
	}
	return start
}

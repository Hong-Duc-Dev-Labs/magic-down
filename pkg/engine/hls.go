package engine

import (
	"bufio"
	"context"
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

// IsHLSURL returns true when the URL or content-type indicates an HLS (.m3u8) playlist.
func IsHLSURL(rawURL, contentType string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "m3u8") {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return strings.Contains(strings.ToLower(rawURL), ".m3u8")
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
}

// HLSVariant represents a quality variant from an HLS master playlist.
type HLSVariant struct {
	URL        string
	Bandwidth  int
	Resolution string
}

// ParseM3U8 parses an M3U8 playlist body, returning either master variants or media segment URLs.
func ParseM3U8(baseURL string, body string) (variants []HLSVariant, segments []string, err error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid base URL: %w", err)
	}

	resolve := func(ref string) (string, error) {
		u, err := url.Parse(strings.TrimSpace(ref))
		if err != nil {
			return "", err
		}
		return base.ResolveReference(u).String(), nil
	}

	scanner := bufio.NewScanner(strings.NewReader(body))
	var pendingVariant *HLSVariant
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			attrs := strings.TrimPrefix(line, "#EXT-X-STREAM-INF:")
			v := &HLSVariant{}
			for _, part := range splitHLSAttrs(attrs) {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) != 2 {
					continue
				}
				key := strings.TrimSpace(kv[0])
				val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
				switch key {
				case "BANDWIDTH":
					v.Bandwidth, _ = strconv.Atoi(val)
				case "RESOLUTION":
					v.Resolution = val
				}
			}
			pendingVariant = v
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		resolved, err := resolve(line)
		if err != nil {
			continue
		}
		if pendingVariant != nil {
			pendingVariant.URL = resolved
			variants = append(variants, *pendingVariant)
			pendingVariant = nil
		} else {
			segments = append(segments, resolved)
		}
	}
	return variants, segments, scanner.Err()
}

func splitHLSAttrs(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' {
			inQuote = !inQuote
		}
		if ch == ',' && !inQuote {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

func (t *Task) fetchText(ctx context.Context, client *http.Client, targetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	t.applyHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d fetching playlist", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (t *Task) applyHeaders(req *http.Request) {
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
}

// runHLSDownload fetches an M3U8 playlist (resolving master -> highest variant if needed),
// downloads media segments concurrently, and concatenates them atomically into SavePath.
func (t *Task) runHLSDownload(ctx context.Context, client *http.Client, onUpdate func(*Task)) error {
	playlistURL := t.URL
	body, err := t.fetchText(ctx, client, playlistURL)
	if err != nil {
		if ctx.Err() != nil {
			t.markPaused(onUpdate)
			return ctx.Err()
		}
		t.fail(fmt.Sprintf("HLS playlist fetch failed: %v", err), onUpdate)
		return err
	}

	variants, segURLs, err := ParseM3U8(playlistURL, body)
	if err != nil {
		t.fail(fmt.Sprintf("HLS playlist parse failed: %v", err), onUpdate)
		return err
	}

	// If master playlist, pick the highest bandwidth variant
	if len(variants) > 0 && len(segURLs) == 0 {
		best := variants[0]
		for _, v := range variants[1:] {
			if v.Bandwidth > best.Bandwidth {
				best = v
			}
		}
		playlistURL = best.URL
		body, err = t.fetchText(ctx, client, playlistURL)
		if err != nil {
			if ctx.Err() != nil {
				t.markPaused(onUpdate)
				return ctx.Err()
			}
			t.fail(fmt.Sprintf("HLS variant fetch failed: %v", err), onUpdate)
			return err
		}
		_, segURLs, err = ParseM3U8(playlistURL, body)
		if err != nil {
			t.fail(fmt.Sprintf("HLS variant parse failed: %v", err), onUpdate)
			return err
		}
	}

	if len(segURLs) == 0 {
		err := errors.New("HLS playlist contains no media segments")
		t.fail(err.Error(), onUpdate)
		return err
	}

	// Ensure output filename has .ts (or .mp4 if user specified .mp4) instead of .m3u8
	t.mu.Lock()
	if strings.EqualFold(filepath.Ext(t.SavePath), ".m3u8") {
		t.SavePath = ReplaceExtension(t.SavePath, "ts")
		t.Filename = filepath.Base(t.SavePath)
		t.PartPath = t.SavePath + ".magicpart"
	}
	workers := t.NumSegments
	if workers <= 0 {
		workers = 8
	}
	if workers > len(segURLs) {
		workers = len(segURLs)
	}
	t.mu.Unlock()

	segDir := t.PartPath + ".hls_segs"
	if err := os.MkdirAll(segDir, 0755); err != nil {
		t.fail(fmt.Sprintf("failed to create HLS temp dir: %v", err), onUpdate)
		return err
	}

	type job struct {
		idx int
		url string
	}
	jobs := make(chan job, len(segURLs))
	for i, u := range segURLs {
		jobs <- job{idx: i, url: u}
	}
	close(jobs)

	var completedSegs int64
	var wg sync.WaitGroup
	errChan := make(chan error, len(segURLs))

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 32*1024)
			for j := range jobs {
				if ctx.Err() != nil {
					return
				}
				segFile := filepath.Join(segDir, fmt.Sprintf("seg_%06d.ts", j.idx))
				// Reuse already-downloaded segment on resume
				if fi, statErr := os.Stat(segFile); statErr == nil && fi.Size() > 0 {
					atomic.AddInt64(&completedSegs, 1)
					continue
				}

				req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, j.url, nil)
				if reqErr != nil {
					errChan <- reqErr
					return
				}
				t.applyHeaders(req)
				resp, doErr := client.Do(req)
				if doErr != nil {
					if ctx.Err() != nil {
						return
					}
					errChan <- fmt.Errorf("segment %d fetch failed: %w", j.idx, doErr)
					return
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					_ = resp.Body.Close()
					errChan <- fmt.Errorf("segment %d HTTP %d", j.idx, resp.StatusCode)
					return
				}

				tmpSeg := segFile + ".tmp"
				f, openErr := os.OpenFile(tmpSeg, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
				if openErr != nil {
					_ = resp.Body.Close()
					errChan <- openErr
					return
				}

				var segBytes int64
				for {
					n, rErr := resp.Body.Read(buf)
					if n > 0 {
						if _, wErr := f.Write(buf[:n]); wErr != nil {
							_ = f.Close()
							_ = resp.Body.Close()
							_ = os.Remove(tmpSeg)
							errChan <- wErr
							return
						}
						segBytes += int64(n)
						newTotal := atomic.AddInt64(&t.DownloadedBytes, int64(n))
						doneCount := atomic.LoadInt64(&completedSegs)
						estTotal := newTotal
						if doneCount > 0 {
							estTotal = (newTotal * int64(len(segURLs))) / doneCount
						}
						speed, eta := t.speedCalc.Record(newTotal, estTotal)
						t.mu.Lock()
						t.TotalBytes = estTotal
						t.Speed = speed
						t.ETA = eta
						t.mu.Unlock()
						if onUpdate != nil {
							onUpdate(t)
						}
					}
					if rErr != nil {
						if rErr == io.EOF {
							break
						}
						_ = f.Close()
						_ = resp.Body.Close()
						_ = os.Remove(tmpSeg)
						if ctx.Err() != nil {
							return
						}
						errChan <- rErr
						return
					}
				}
				_ = resp.Body.Close()
				_ = f.Close()
				if err := os.Rename(tmpSeg, segFile); err != nil {
					errChan <- err
					return
				}
				atomic.AddInt64(&completedSegs, 1)
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
		t.fail(fmt.Sprintf("HLS download error: %v", firstErr), onUpdate)
		return firstErr
	}

	// Concatenate segments in exact order into PartPath
	outFile, err := os.OpenFile(t.PartPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		t.fail(fmt.Sprintf("failed to create HLS output: %v", err), onUpdate)
		return err
	}
	var totalWritten int64
	for i := range segURLs {
		segFile := filepath.Join(segDir, fmt.Sprintf("seg_%06d.ts", i))
		in, openErr := os.Open(segFile)
		if openErr != nil {
			_ = outFile.Close()
			t.fail(fmt.Sprintf("missing HLS segment %d: %v", i, openErr), onUpdate)
			return openErr
		}
		n, copyErr := io.Copy(outFile, in)
		_ = in.Close()
		if copyErr != nil {
			_ = outFile.Close()
			t.fail(fmt.Sprintf("HLS merge error: %v", copyErr), onUpdate)
			return copyErr
		}
		totalWritten += n
	}
	_ = outFile.Sync()
	_ = outFile.Close()
	_ = os.RemoveAll(segDir)

	atomic.StoreInt64(&t.DownloadedBytes, totalWritten)
	t.mu.Lock()
	t.TotalBytes = totalWritten
	t.mu.Unlock()

	return t.finalizeFile(onUpdate)
}

func (t *Task) markPaused(onUpdate func(*Task)) {
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
}

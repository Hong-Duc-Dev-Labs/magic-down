package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// downloadSegment downloads a specific segment range and writes it into the target file at its offset.
// Transient network/5xx failures are retried up to maxRetries times, each attempt resuming from the
// bytes already written for the segment.
func downloadSegment(
	ctx context.Context,
	client *http.Client,
	taskURL string,
	headers map[string]string,
	cookies string,
	referrer string,
	seg *Segment,
	writer io.WriterAt,
	limiter *rateLimiter,
	maxRetries int,
	onChunkDownloaded func(delta int64),
	onRetry func(),
) error {
	seg.setStatus("downloading")

	var lastErr error
	for attempt := 0; ; attempt++ {
		retryable, err := downloadSegmentAttempt(ctx, client, taskURL, headers, cookies, referrer, seg, writer, limiter, onChunkDownloaded)
		if err == nil {
			seg.setStatus("done")
			return nil
		}
		lastErr = err

		if ctx.Err() != nil {
			seg.setStatus("idle")
			return ctx.Err()
		}
		if !retryable || attempt >= maxRetries {
			seg.setStatus("error")
			return lastErr
		}

		// Keep the segment visible as downloading while backing off.
		seg.setStatus("downloading")
		if onRetry != nil {
			onRetry()
		}
		if werr := waitRetry(ctx, attempt+1); werr != nil {
			seg.setStatus("idle")
			return werr
		}
	}
}

// downloadSegmentAttempt performs one ranged request. It reports whether the
// failure is worth retrying.
func downloadSegmentAttempt(
	ctx context.Context,
	client *http.Client,
	taskURL string,
	headers map[string]string,
	cookies string,
	referrer string,
	seg *Segment,
	writer io.WriterAt,
	limiter *rateLimiter,
	onChunkDownloaded func(delta int64),
) (retryable bool, err error) {
	rangeStart := seg.Start + atomic.LoadInt64(&seg.Downloaded)
	rangeEnd := seg.End

	// Segment already completely downloaded
	if rangeStart > rangeEnd {
		return false, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, taskURL, nil)
	if err != nil {
		return false, fmt.Errorf("segment %d failed to create request: %w", seg.Index, err)
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd))
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

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, fmt.Errorf("segment %d request failed: %w", seg.Index, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return retryableStatus(resp.StatusCode), fmt.Errorf("segment %d unexpected status code: %d", seg.Index, resp.StatusCode)
	}
	// A range request answered with a full 200 after progress would restart the
	// body at byte 0 and corrupt the file, so treat it as a hard failure.
	if resp.StatusCode == http.StatusOK && rangeStart > seg.Start {
		return false, fmt.Errorf("segment %d server ignored resume range at offset %d", seg.Index, rangeStart)
	}

	buf := make([]byte, 64*1024) // 64 KB buffer
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		downloaded := atomic.LoadInt64(&seg.Downloaded)
		remaining := seg.End - (seg.Start + downloaded) + 1
		if remaining <= 0 {
			break
		}
		readBuf := buf
		if int64(len(readBuf)) > remaining {
			readBuf = readBuf[:remaining]
		}

		n, readErr := resp.Body.Read(readBuf)
		if n > 0 {
			if limiter != nil {
				if waitErr := limiter.wait(ctx, int64(n)); waitErr != nil {
					return false, waitErr
				}
			}

			downloaded = atomic.LoadInt64(&seg.Downloaded)
			writeOffset := seg.Start + downloaded

			// Write chunk at exact offset in .part file
			if _, writeErr := writer.WriteAt(readBuf[:n], writeOffset); writeErr != nil {
				return false, fmt.Errorf("segment %d write failed at offset %d: %w", seg.Index, writeOffset, writeErr)
			}

			atomic.AddInt64(&seg.Downloaded, int64(n))
			if onChunkDownloaded != nil {
				onChunkDownloaded(int64(n))
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				if atomic.LoadInt64(&seg.Downloaded) < seg.End-seg.Start+1 {
					return true, fmt.Errorf("segment %d truncated at %d of %d bytes", seg.Index, atomic.LoadInt64(&seg.Downloaded), seg.End-seg.Start+1)
				}
				return false, nil
			}
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return true, fmt.Errorf("segment %d read error: %w", seg.Index, readErr)
		}
	}

	if atomic.LoadInt64(&seg.Downloaded) < seg.End-seg.Start+1 {
		return true, fmt.Errorf("segment %d ended early at %d of %d bytes", seg.Index, atomic.LoadInt64(&seg.Downloaded), seg.End-seg.Start+1)
	}
	return false, nil
}

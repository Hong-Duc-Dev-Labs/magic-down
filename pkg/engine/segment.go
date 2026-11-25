package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// downloadSegment downloads a specific segment range and writes it into the target file at its offset.
func downloadSegment(
	ctx context.Context,
	client *http.Client,
	taskURL string,
	headers map[string]string,
	cookies string,
	referrer string,
	seg *Segment,
	writer io.WriterAt,
	onChunkDownloaded func(delta int64),
) error {
	seg.setStatus("downloading")

	// Calculate remaining range to fetch for this segment
	rangeStart := seg.Start + atomic.LoadInt64(&seg.Downloaded)
	rangeEnd := seg.End

	// Segment already completely downloaded
	if rangeStart > rangeEnd {
		seg.setStatus("done")
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, taskURL, nil)
	if err != nil {
		seg.setStatus("error")
		return fmt.Errorf("segment %d failed to create request: %w", seg.Index, err)
	}

	// Set Range header
	rangeHeaderVal := fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd)
	req.Header.Set("Range", rangeHeaderVal)

	// User agent, cookies, referrer and custom headers
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
			seg.setStatus("idle")
			return ctx.Err()
		}
		seg.setStatus("error")
		return fmt.Errorf("segment %d request failed: %w", seg.Index, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		seg.setStatus("error")
		return fmt.Errorf("segment %d unexpected status code: %d", seg.Index, resp.StatusCode)
	}

	// Buffer for reading
	buf := make([]byte, 64*1024) // 64 KB buffer
	for {
		select {
		case <-ctx.Done():
			seg.setStatus("idle")
			return ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			currentDownloaded := atomic.LoadInt64(&seg.Downloaded)
			writeOffset := seg.Start + currentDownloaded

			// Write chunk at exact offset in .part file
			_, writeErr := writer.WriteAt(buf[:n], writeOffset)
			if writeErr != nil {
				seg.setStatus("error")
				return fmt.Errorf("segment %d write failed at offset %d: %w", seg.Index, writeOffset, writeErr)
			}

			atomic.AddInt64(&seg.Downloaded, int64(n))
			if onChunkDownloaded != nil {
				onChunkDownloaded(int64(n))
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			if ctx.Err() != nil {
				seg.setStatus("idle")
				return ctx.Err()
			}
			seg.setStatus("error")
			return fmt.Errorf("segment %d read error: %w", seg.Index, readErr)
		}
	}

	seg.setStatus("done")
	return nil
}

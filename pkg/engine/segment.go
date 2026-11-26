package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

const (
	// defaultMinStealRemaining is the minimum remaining bytes an active segment
	// must have before an idle worker steals its second half.
	defaultMinStealRemaining int64 = 2 * 1024 * 1024 // 2 MiB

	// stealSafetyMargin keeps the split boundary well ahead of an active
	// worker's in-flight 64 KiB read buffer so victim and thief never overlap.
	stealSafetyMargin int64 = 256 * 1024 // 256 KiB
)

// segmentCoordinator owns dynamic work-stealing over task.Segments under task.mu.
type segmentCoordinator struct {
	task         *Task
	minRemaining int64
	maxSegments  int
	onSplit      func()
}

func newSegmentCoordinator(task *Task, minRemaining int64, onSplit func()) *segmentCoordinator {
	if minRemaining <= 0 {
		minRemaining = defaultMinStealRemaining
	}
	task.mu.RLock()
	base := task.NumSegments
	cur := len(task.Segments)
	task.mu.RUnlock()

	if base <= 0 {
		base = cur
	}
	maxSegs := base * 2
	if maxSegs < cur {
		maxSegs = cur
	}
	return &segmentCoordinator{
		task:         task,
		minRemaining: minRemaining,
		maxSegments:  maxSegs,
		onSplit:      onSplit,
	}
}

// stealSegment finds the active downloading segment with the largest remaining
// byte range (>= minRemaining) and atomically splits its tail into a new
// segment under task.mu.Lock(). Returns nil if no eligible segment exists or
// maxSegments has been reached.
func (c *segmentCoordinator) stealSegment() *Segment {
	c.task.mu.Lock()
	if len(c.task.Segments) >= c.maxSegments {
		c.task.mu.Unlock()
		return nil
	}

	var victim *Segment
	var victimCurrentPos int64
	var victimEnd int64
	var maxRem int64

	minRequired := c.minRemaining
	if minRequired < 2*stealSafetyMargin {
		minRequired = 2 * stealSafetyMargin
	}

	for _, seg := range c.task.Segments {
		if seg.getStatus() != "downloading" {
			continue
		}
		downloaded := atomic.LoadInt64(&seg.Downloaded)
		end := seg.getEnd()
		currentPos := seg.Start + downloaded
		rem := end - currentPos + 1
		if rem >= minRequired && rem > maxRem {
			victim = seg
			victimCurrentPos = currentPos
			victimEnd = end
			maxRem = rem
		}
	}

	if victim == nil {
		c.task.mu.Unlock()
		return nil
	}

	mid := victimCurrentPos + maxRem/2
	if minMid := victimCurrentPos + stealSafetyMargin; mid < minMid {
		mid = minMid
	}
	if mid >= victimEnd {
		c.task.mu.Unlock()
		return nil
	}

	// Mutate victim's End and append the stolen segment in the same task.mu
	// critical section so Clone() / saveState() / Pause never observe a gap.
	oldEnd := victimEnd
	victim.setEnd(mid)
	stolen := &Segment{
		Index:      len(c.task.Segments),
		Start:      mid + 1,
		End:        oldEnd,
		Downloaded: 0,
		Status:     "downloading",
	}
	c.task.Segments = append(c.task.Segments, stolen)
	c.task.mu.Unlock()

	if c.onSplit != nil {
		c.onSplit()
	}
	return stolen
}

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
	rangeEnd := seg.getEnd()

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
		curEnd := seg.getEnd()
		remaining := curEnd - (seg.Start + downloaded) + 1
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
			curEnd = seg.getEnd()
			allowed := curEnd - (seg.Start + downloaded) + 1
			if allowed <= 0 {
				break
			}
			toWrite := int64(n)
			if toWrite > allowed {
				toWrite = allowed
			}
			writeOffset := seg.Start + downloaded

			// Write chunk at exact offset in .part file
			if _, writeErr := writer.WriteAt(readBuf[:toWrite], writeOffset); writeErr != nil {
				return false, fmt.Errorf("segment %d write failed at offset %d: %w", seg.Index, writeOffset, writeErr)
			}

			newDownloaded := atomic.AddInt64(&seg.Downloaded, toWrite)
			if onChunkDownloaded != nil {
				onChunkDownloaded(toWrite)
			}
			// Stop cleanly if the segment was dynamically shrunk to our current position.
			if toWrite < int64(n) || newDownloaded >= seg.getEnd()-seg.Start+1 {
				break
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				expected := seg.getEnd() - seg.Start + 1
				if atomic.LoadInt64(&seg.Downloaded) < expected {
					return true, fmt.Errorf("segment %d truncated at %d of %d bytes", seg.Index, atomic.LoadInt64(&seg.Downloaded), expected)
				}
				return false, nil
			}
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return true, fmt.Errorf("segment %d read error: %w", seg.Index, readErr)
		}
	}

	expected := seg.getEnd() - seg.Start + 1
	if atomic.LoadInt64(&seg.Downloaded) < expected {
		return true, fmt.Errorf("segment %d ended early at %d of %d bytes", seg.Index, atomic.LoadInt64(&seg.Downloaded), expected)
	}
	return false, nil
}

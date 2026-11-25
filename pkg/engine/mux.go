package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// ffmpegRunner is a package-level hook so unit tests can mock ffmpeg execution.
var ffmpegRunner = func(ctx context.Context, ffmpegPath string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.Bytes(), err
}

// ffmpegLocator is a package-level hook so unit tests can override ffmpeg discovery.
var ffmpegLocator = findFFmpegDefault

// FindFFmpeg locates the ffmpeg binary next to the current executable first,
// then falls back to PATH.
func FindFFmpeg() (string, error) {
	return ffmpegLocator()
}

func findFFmpegDefault() (string, error) {
	binName := "ffmpeg"
	if runtime.GOOS == "windows" {
		binName = "ffmpeg.exe"
	}

	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), binName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	if path, err := exec.LookPath("ffmpeg"); err == nil {
		return path, nil
	}

	return "", errors.New("ffmpeg not found: place ffmpeg next to MagicDown or add it to PATH to merge video+audio")
}

// MuxVideoAudio merges separate video and audio files into a single MP4 file.
// It tries fast stream copy first (-c copy -movflags +faststart) and falls back
// to AAC audio transcoding (-c:v copy -c:a aac) if the audio codec is not MP4-compatible.
func MuxVideoAudio(ctx context.Context, videoPath, audioPath, outputPath string) error {
	ffmpegPath, err := FindFFmpeg()
	if err != nil {
		return err
	}

	tmpOut := outputPath + ".muxing.mp4"
	_ = os.Remove(tmpOut)

	copyArgs := []string{
		"-y",
		"-i", videoPath,
		"-i", audioPath,
		"-c", "copy",
		"-movflags", "+faststart",
		tmpOut,
	}
	if _, err := ffmpegRunner(ctx, ffmpegPath, copyArgs); err != nil {
		_ = os.Remove(tmpOut)
		// Fallback for non-MP4 audio codecs (e.g., WebM/Opus -> AAC in MP4)
		aacArgs := []string{
			"-y",
			"-i", videoPath,
			"-i", audioPath,
			"-c:v", "copy",
			"-c:a", "aac",
			"-b:a", "192k",
			"-movflags", "+faststart",
			tmpOut,
		}
		if out, err2 := ffmpegRunner(ctx, ffmpegPath, aacArgs); err2 != nil {
			_ = os.Remove(tmpOut)
			msg := strings.TrimSpace(string(out))
			if len(msg) > 240 {
				msg = msg[len(msg)-240:]
			}
			if msg != "" {
				return fmt.Errorf("ffmpeg mux failed: %w (%s)", err2, msg)
			}
			return fmt.Errorf("ffmpeg mux failed: %w", err2)
		}
	}

	if _, err := os.Stat(outputPath); err == nil {
		_ = os.Remove(outputPath)
	}
	if err := os.Rename(tmpOut, outputPath); err != nil {
		_ = os.Remove(tmpOut)
		return fmt.Errorf("failed to finalize muxed output: %w", err)
	}
	return nil
}

// runMuxedDownload downloads the video track (t.URL) and audio track (t.AudioURL)
// in parallel, then muxes them into a single MP4 file at t.SavePath and cleans up
// temporary track files.
func (t *Task) runMuxedDownload(ctx context.Context, client *http.Client, onUpdate func(*Task)) error {
	t.mu.RLock()
	videoURL := t.URL
	audioURL := t.AudioURL
	partPath := t.PartPath
	savePath := t.SavePath
	t.mu.RUnlock()

	if videoURL == "" || audioURL == "" {
		err := errors.New("muxed download requires both video and audio URLs")
		t.fail(err.Error(), onUpdate)
		return err
	}

	// Ensure ffmpeg is available before spending bandwidth.
	if _, err := FindFFmpeg(); err != nil {
		t.fail(err.Error(), onUpdate)
		return err
	}

	videoTmp := partPath + ".video"
	audioTmp := partPath + ".audio"

	var totalKnown int64
	atomic.StoreInt64(&t.DownloadedBytes, 0)

	onChunk := func(delta int64) {
		if delta <= 0 {
			return
		}
		newTotal := atomic.AddInt64(&t.DownloadedBytes, delta)
		est := atomic.LoadInt64(&totalKnown)
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
	onKnown := func(size int64) {
		if size > 0 {
			est := atomic.AddInt64(&totalKnown, size)
			t.mu.Lock()
			t.TotalBytes = est
			t.mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := t.downloadTrackToFile(ctx, client, videoURL, videoTmp, onKnown, onChunk, onUpdate); err != nil {
			errCh <- fmt.Errorf("video track: %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := t.downloadTrackToFile(ctx, client, audioURL, audioTmp, onKnown, onChunk, onUpdate); err != nil {
			errCh <- fmt.Errorf("audio track: %w", err)
		}
	}()

	wg.Wait()
	close(errCh)

	if ctx.Err() != nil {
		t.markPaused(onUpdate)
		return ctx.Err()
	}
	if len(errCh) > 0 {
		firstErr := <-errCh
		t.fail(fmt.Sprintf("mux track download failed: %v", firstErr), onUpdate)
		return firstErr
	}

	t.mu.Lock()
	t.Status = StatusMuxing
	t.Speed = 0
	t.ETA = 0
	t.mu.Unlock()
	if onUpdate != nil {
		onUpdate(t)
	}

	if err := MuxVideoAudio(ctx, videoTmp, audioTmp, partPath); err != nil {
		if ctx.Err() != nil {
			t.markPaused(onUpdate)
			return ctx.Err()
		}
		t.fail(err.Error(), onUpdate)
		return err
	}

	// Clean up temp tracks after successful mux.
	_ = os.Remove(videoTmp)
	_ = os.Remove(audioTmp)

	if fi, err := os.Stat(partPath); err == nil && fi.Size() > 0 {
		atomic.StoreInt64(&t.DownloadedBytes, fi.Size())
		t.mu.Lock()
		t.TotalBytes = fi.Size()
		if !strings.EqualFold(filepath.Ext(savePath), ".mp4") {
			t.SavePath = ReplaceExtension(savePath, "mp4")
			t.Filename = filepath.Base(t.SavePath)
		}
		t.mu.Unlock()
	}

	return t.finalizeFile(onUpdate)
}

func (t *Task) downloadTrackToFile(
	ctx context.Context,
	client *http.Client,
	trackURL string,
	dest string,
	onKnown func(int64),
	onChunk func(int64),
	onUpdate func(*Task),
) error {
	maxRetries := t.retryBudget()
	var lastErr error
	for attempt := 0; ; attempt++ {
		retryable, err := t.downloadTrackOnce(ctx, client, trackURL, dest, onKnown, onChunk)
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
		t.noteRetry(onUpdate)
		if werr := waitRetry(ctx, attempt+1); werr != nil {
			return werr
		}
	}
}

func (t *Task) downloadTrackOnce(
	ctx context.Context,
	client *http.Client,
	trackURL string,
	dest string,
	onKnown func(int64),
	onChunk func(int64),
) (bool, error) {
	var startOffset int64
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		startOffset = fi.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trackURL, nil)
	if err != nil {
		return false, fmt.Errorf("request creation error: %w", err)
	}
	t.applyHeaders(req)
	if startOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, err
	}
	defer resp.Body.Close()

	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusPartialContent:
		flags |= os.O_APPEND
		if resp.ContentLength > 0 && onKnown != nil {
			onKnown(startOffset + resp.ContentLength)
		}
		if startOffset > 0 && onChunk != nil {
			onChunk(startOffset)
		}
	case http.StatusOK:
		startOffset = 0
		flags |= os.O_TRUNC
		if resp.ContentLength > 0 && onKnown != nil {
			onKnown(resp.ContentLength)
		}
	case http.StatusRequestedRangeNotSatisfiable:
		if startOffset > 0 {
			return false, nil
		}
		return false, fmt.Errorf("HTTP 416")
	default:
		return retryableStatus(resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.OpenFile(dest, flags, 0644)
	if err != nil {
		return false, err
	}
	buf := make([]byte, 64*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if t.limiter != nil {
				if waitErr := t.limiter.wait(ctx, int64(n)); waitErr != nil {
					_ = out.Close()
					return false, waitErr
				}
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				_ = out.Close()
				return false, werr
			}
			if onChunk != nil {
				onChunk(int64(n))
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
	return false, out.Close()
}

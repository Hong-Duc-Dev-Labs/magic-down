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
	hideCommandWindow(cmd)
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

	return "", errors.New("ffmpeg not found: install ffmpeg (e.g. winget install Gyan.FFmpeg) or place ffmpeg.exe next to MagicDown and add it to PATH")
}

// NormalizeOutputFormat validates and canonicalizes a requested audio/media output format.
func NormalizeOutputFormat(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(strings.TrimPrefix(format, "."))) {
	case "mp3":
		return "mp3", true
	default:
		return "", false
	}
}

// NormalizeAudioBitrate validates and canonicalizes an MP3 audio bitrate.
// Defaults to "192k" when empty or unrecognized.
func NormalizeAudioBitrate(bitrate string) string {
	switch strings.ToLower(strings.TrimSpace(bitrate)) {
	case "320k", "320", "320kbps":
		return "320k"
	case "192k", "192", "192kbps":
		return "192k"
	case "128k", "128", "128kbps":
		return "128k"
	default:
		return "192k"
	}
}

// ConvertToMP3 transcodes srcPath (audio or video file) into an MP3 file at dstPath
// using libmp3lame at the requested bitrate (320k, 192k, 128k) with ID3v2.3 tags.
func ConvertToMP3(ctx context.Context, srcPath, dstPath, bitrate string) error {
	ffmpegPath, err := FindFFmpeg()
	if err != nil {
		return err
	}

	br := NormalizeAudioBitrate(bitrate)
	tmpOut := dstPath + ".converting.mp3"
	_ = os.Remove(tmpOut)

	args := []string{
		"-y",
		"-i", srcPath,
		"-vn",
		"-c:a", "libmp3lame",
		"-b:a", br,
		"-id3v2_version", "3",
		tmpOut,
	}
	if out, err := ffmpegRunner(ctx, ffmpegPath, args); err != nil {
		_ = os.Remove(tmpOut)
		msg := strings.TrimSpace(string(out))
		if len(msg) > 240 {
			msg = msg[len(msg)-240:]
		}
		if msg != "" {
			return fmt.Errorf("ffmpeg mp3 conversion failed: %w (%s)", err, msg)
		}
		return fmt.Errorf("ffmpeg mp3 conversion failed: %w", err)
	}

	if _, err := os.Stat(dstPath); err == nil {
		_ = os.Remove(dstPath)
	}
	if err := os.Rename(tmpOut, dstPath); err != nil {
		_ = os.Remove(tmpOut)
		return fmt.Errorf("failed to finalize mp3 output: %w", err)
	}
	return nil
}

// MuxVideoAudio merges separate video and audio files into a single MP4 file.
// It tries fast stream copy first (-c copy -movflags +faststart), then falls back
// to AAC audio transcoding (-c:v copy -c:a aac) for WebM/Opus audio, and finally
// to H.264+AAC transcoding (-c:v libx264 -c:a aac) if the video codec cannot be
// stream-copied into an MP4 container.
func MuxVideoAudio(ctx context.Context, videoPath, audioPath, outputPath string) error {
	ffmpegPath, err := FindFFmpeg()
	if err != nil {
		return err
	}

	tmpOut := outputPath + ".muxing.mp4"
	_ = os.Remove(tmpOut)

	attempts := [][]string{
		{
			"-y",
			"-i", videoPath,
			"-i", audioPath,
			"-c", "copy",
			"-movflags", "+faststart",
			tmpOut,
		},
		{
			"-y",
			"-i", videoPath,
			"-i", audioPath,
			"-c:v", "copy",
			"-c:a", "aac",
			"-b:a", "192k",
			"-movflags", "+faststart",
			tmpOut,
		},
		{
			"-y",
			"-i", videoPath,
			"-i", audioPath,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "22",
			"-c:a", "aac",
			"-b:a", "192k",
			"-movflags", "+faststart",
			tmpOut,
		},
	}

	var lastOut []byte
	var lastErr error
	for _, args := range attempts {
		_ = os.Remove(tmpOut)
		out, runErr := ffmpegRunner(ctx, ffmpegPath, args)
		if runErr == nil {
			lastErr = nil
			break
		}
		lastOut = out
		lastErr = runErr
	}
	if lastErr != nil {
		_ = os.Remove(tmpOut)
		msg := strings.TrimSpace(string(lastOut))
		if len(msg) > 240 {
			msg = msg[len(msg)-240:]
		}
		if msg != "" {
			return fmt.Errorf("ffmpeg mux failed: %w (%s)", lastErr, msg)
		}
		return fmt.Errorf("ffmpeg mux failed: %w", lastErr)
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

	var (
		videoKnown atomic.Int64
		audioKnown atomic.Int64
		videoDown  atomic.Int64
		audioDown  atomic.Int64
	)
	atomic.StoreInt64(&t.DownloadedBytes, 0)

	updateProgress := func() {
		downloaded := videoDown.Load() + audioDown.Load()
		est := videoKnown.Load() + audioKnown.Load()
		atomic.StoreInt64(&t.DownloadedBytes, downloaded)
		speed, eta := t.speedCalc.Record(downloaded, est)
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
	makeTrackCallbacks := func(knownSlot, downSlot *atomic.Int64) (func(int64), func(int64)) {
		onKnown := func(size int64) {
			if size > 0 {
				knownSlot.Store(size)
				est := videoKnown.Load() + audioKnown.Load()
				t.mu.Lock()
				t.TotalBytes = est
				t.mu.Unlock()
			}
		}
		onProgress := func(downloaded int64) {
			if downloaded < 0 {
				downloaded = 0
			}
			downSlot.Store(downloaded)
			updateProgress()
		}
		return onKnown, onProgress
	}

	videoOnKnown, videoOnProgress := makeTrackCallbacks(&videoKnown, &videoDown)
	audioOnKnown, audioOnProgress := makeTrackCallbacks(&audioKnown, &audioDown)

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := t.downloadTrackToFile(ctx, client, videoURL, videoTmp, videoOnKnown, videoOnProgress, onUpdate); err != nil {
			errCh <- fmt.Errorf("video track: %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := t.downloadTrackToFile(ctx, client, audioURL, audioTmp, audioOnKnown, audioOnProgress, onUpdate); err != nil {
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
	written := startOffset
	switch resp.StatusCode {
	case http.StatusPartialContent:
		flags |= os.O_APPEND
		if resp.ContentLength > 0 && onKnown != nil {
			onKnown(startOffset + resp.ContentLength)
		}
		if startOffset > 0 && onChunk != nil {
			onChunk(written)
		}
	case http.StatusOK:
		startOffset = 0
		written = 0
		flags |= os.O_TRUNC
		if resp.ContentLength > 0 && onKnown != nil {
			onKnown(resp.ContentLength)
		}
		if onChunk != nil {
			onChunk(0)
		}
	case http.StatusRequestedRangeNotSatisfiable:
		if startOffset > 0 {
			if onKnown != nil {
				onKnown(startOffset)
			}
			if onChunk != nil {
				onChunk(startOffset)
			}
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
			if waitErr := t.waitLimiters(ctx, int64(n)); waitErr != nil {
				_ = out.Close()
				return false, waitErr
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				_ = out.Close()
				return false, werr
			}
			written += int64(n)
			if onChunk != nil {
				onChunk(written)
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

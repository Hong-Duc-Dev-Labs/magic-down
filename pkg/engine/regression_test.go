package engine

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Regression test for Critical 3: rate limiter must not livelock when speed limit < 256 KB/s
// and callers request 64 KB chunks (even with concurrent smaller waiters).
func TestRateLimiterBelow256KBpsDoesNotLivelock(t *testing.T) {
	// 128 KB/s => rate/4 = 32 KB < 64 KB chunk size.
	rl := newRateLimiter(128 * 1024)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		// 64 KB chunk must pass in ~0.5s at 128 KB/s without livelocking.
		if err := rl.wait(ctx, 64*1024); err != nil {
			errCh <- fmt.Errorf("64KB waiter failed: %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		// Concurrent smaller waiter must not clamp burst below 64 KB.
		for i := 0; i < 4; i++ {
			if err := rl.wait(ctx, 4*1024); err != nil {
				errCh <- fmt.Errorf("4KB waiter failed: %w", err)
				return
			}
		}
	}()

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

// Regression test for High 2: Engine.Subscribe() must deliver cloned *Task snapshots
// so subscribers reading Task fields concurrently with active downloads never race.
func TestEngineSubscribeReceivesIsolatedSnapshotsRaceFree(t *testing.T) {
	payload, _ := generateTestPayload(256 * 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servePayloadRange(w, r, payload)
	}))
	defer srv.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		MaxRetries:             1,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	sub := eng.Subscribe()
	doneReading := make(chan struct{})
	var seenUpdates atomic.Int32
	go func() {
		defer close(doneReading)
		for snap := range sub {
			// Read fields that download goroutines mutate concurrently under t.mu.
			_ = snap.Status
			_ = snap.Speed
			_ = snap.ETA
			_ = snap.TotalBytes
			_ = snap.DownloadedBytes
			_ = snap.ErrorMessage
			seenUpdates.Add(1)
		}
	}()

	task, err := eng.AddTask(AddTaskRequest{
		URL:         srv.URL + "/race-check.bin",
		Filename:    "race-check.bin",
		NumSegments: 4,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	waitForStatus(t, eng, task.ID, StatusCompleted, 10*time.Second)
	eng.Unsubscribe(sub)
	<-doneReading

	if seenUpdates.Load() == 0 {
		t.Fatalf("expected subscriber to receive at least one task snapshot")
	}
}

// Regression test for High 6: retrying a muxed track (206 resume or 200 restart)
// must not double-count TotalBytes or DownloadedBytes.
func TestRunMuxedDownloadRetryDoesNotDoubleCountProgress(t *testing.T) {
	videoData := bytes.Repeat([]byte("V"), 128*1024)
	audioData := bytes.Repeat([]byte("A"), 64*1024)
	expectedTotal := int64(len(videoData) + len(audioData))

	var videoAttempts atomic.Int32
	var audioAttempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/video":
			attempt := videoAttempts.Add(1)
			if attempt == 1 {
				// Send half the video, then drop connection to force a 206 resume retry.
				w.Header().Set("Content-Length", strconv.Itoa(len(videoData)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(videoData[:len(videoData)/2])
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				if hj, ok := w.(http.Hijacker); ok {
					if conn, _, err := hj.Hijack(); err == nil {
						_ = conn.Close()
					}
				}
				return
			}
			// Resume via HTTP 206 Partial Content.
			rng := r.Header.Get("Range")
			start := 0
			if strings.HasPrefix(rng, "bytes=") {
				parts := strings.Split(strings.TrimPrefix(rng, "bytes="), "-")
				if n, err := strconv.Atoi(parts[0]); err == nil {
					start = n
				}
			}
			rem := videoData[start:]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(videoData)-1, len(videoData)))
			w.Header().Set("Content-Length", strconv.Itoa(len(rem)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(rem)

		case "/audio":
			attempt := audioAttempts.Add(1)
			if attempt == 1 {
				// Send half the audio, then drop connection; next attempt returns 200 OK (full restart).
				w.Header().Set("Content-Length", strconv.Itoa(len(audioData)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(audioData[:len(audioData)/2])
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				if hj, ok := w.(http.Hijacker); ok {
					if conn, _, err := hj.Hijack(); err == nil {
						_ = conn.Close()
					}
				}
				return
			}
			// Ignore Range and return full 200 OK to test reset without double-counting.
			w.Header().Set("Content-Length", strconv.Itoa(len(audioData)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(audioData)

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	origLocator := ffmpegLocator
	origRunner := ffmpegRunner
	defer func() {
		ffmpegLocator = origLocator
		ffmpegRunner = origRunner
	}()

	ffmpegLocator = func() (string, error) { return "/mock/ffmpeg", nil }
	ffmpegRunner = func(ctx context.Context, ffmpegPath string, args []string) ([]byte, error) {
		vBytes, err := os.ReadFile(args[2])
		if err != nil {
			return nil, err
		}
		aBytes, err := os.ReadFile(args[4])
		if err != nil {
			return nil, err
		}
		out := args[len(args)-1]
		return nil, os.WriteFile(out, append(vBytes, aBytes...), 0644)
	}

	tmp := t.TempDir()
	cfg := DefaultConfig(tmp, filepath.Join(tmp, "state.json"))
	cfg.MaxRetries = 3
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	sub := eng.Subscribe()
	var maxObservedTotal atomic.Int64
	var maxObservedDown atomic.Int64
	doneSub := make(chan struct{})
	go func() {
		defer close(doneSub)
		for snap := range sub {
			for {
				cur := maxObservedTotal.Load()
				if snap.TotalBytes <= cur || maxObservedTotal.CompareAndSwap(cur, snap.TotalBytes) {
					break
				}
			}
			for {
				cur := maxObservedDown.Load()
				if snap.DownloadedBytes <= cur || maxObservedDown.CompareAndSwap(cur, snap.DownloadedBytes) {
					break
				}
			}
		}
	}()

	task, err := eng.AddTask(AddTaskRequest{
		URL:       srv.URL + "/video",
		AudioURL:  srv.URL + "/audio",
		Filename:  "retry_mux.mp4",
		AutoStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	final := waitForStatus(t, eng, task.ID, StatusCompleted, 15*time.Second)
	eng.Unsubscribe(sub)
	<-doneSub

	if videoAttempts.Load() < 2 || audioAttempts.Load() < 2 {
		t.Fatalf("expected both tracks to retry, got video=%d audio=%d", videoAttempts.Load(), audioAttempts.Load())
	}
	if maxObservedTotal.Load() > expectedTotal {
		t.Fatalf("TotalBytes inflated on retry: maxObserved=%d, expected<=%d", maxObservedTotal.Load(), expectedTotal)
	}
	if maxObservedDown.Load() > expectedTotal {
		t.Fatalf("DownloadedBytes inflated on retry: maxObserved=%d, expected<=%d", maxObservedDown.Load(), expectedTotal)
	}
	if final.TotalBytes != expectedTotal || final.DownloadedBytes != expectedTotal {
		t.Fatalf("unexpected final byte counts: downloaded=%d total=%d want=%d", final.DownloadedBytes, final.TotalBytes, expectedTotal)
	}
}

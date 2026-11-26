package engine

import (
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

// servePayloadRange answers HEAD and ranged GET requests for payload.
func servePayloadRange(w http.ResponseWriter, r *http.Request, payload []byte) {
	size := len(payload)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "application/octet-stream")

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.WriteHeader(http.StatusOK)
		return
	}

	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
		return
	}

	if !strings.HasPrefix(rangeHeader, "bytes=") {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	parts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
	if len(parts) != 2 {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	end := size - 1
	if parts[1] != "" {
		if parsed, err := strconv.Atoi(parts[1]); err == nil {
			end = parsed
		}
	}
	if start < 0 || end >= size || start > end {
		http.Error(w, "range out of bounds", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(payload[start : end+1])
}

func waitForStatus(t *testing.T, eng *Engine, id string, want TaskStatus, timeout time.Duration) *Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var latest *Task
	for time.Now().Before(deadline) {
		snap, err := eng.GetTask(id)
		if err != nil {
			t.Fatalf("task disappeared: %v", err)
		}
		latest = snap
		if snap.Status == want {
			return snap
		}
		if want != StatusFailed && snap.Status == StatusFailed {
			t.Fatalf("task failed before reaching %s: %s", want, snap.ErrorMessage)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task did not reach %s in %v (last: %+v)", want, timeout, latest)
	return nil
}

// TestRetryRecoversAfterTransientFailures checks that a segment retries 5xx and
// connection-reset failures and that the assembled file is byte-exact.
func TestRetryRecoversAfterTransientFailures(t *testing.T) {
	payload, expectedHash := generateTestPayload(256 * 1024)
	var getAttempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			switch atomic.AddInt32(&getAttempts, 1) {
			case 1:
				http.Error(w, "temporary failure", http.StatusInternalServerError)
				return
			case 2:
				if hj, ok := w.(http.Hijacker); ok {
					if conn, _, err := hj.Hijack(); err == nil {
						_ = conn.Close() // connection reset
						return
					}
				}
				http.Error(w, "temporary failure", http.StatusInternalServerError)
				return
			}
		}
		servePayloadRange(w, r, payload)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		MaxRetries:             3,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:         server.URL + "/flaky.bin",
		Filename:    "flaky.bin",
		NumSegments: 4,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	final := waitForStatus(t, eng, task.ID, StatusCompleted, 20*time.Second)
	if final.Retries < 1 {
		t.Fatalf("expected at least one recorded retry, got %d", final.Retries)
	}

	actualHash, err := computeFileSHA256(filepath.Join(tempDir, "flaky.bin"))
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if actualHash != expectedHash {
		t.Fatalf("hash mismatch! expected %s, got %s", expectedHash, actualHash)
	}
}

// TestRetriesExhaustedMarksFailed checks that failures beyond MaxRetries stop the task.
func TestRetriesExhaustedMarksFailed(t *testing.T) {
	payload, _ := generateTestPayload(128 * 1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			servePayloadRange(w, r, payload)
			return
		}
		http.Error(w, "always failing", http.StatusInternalServerError)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 1,
		DefaultSegments:        2,
		MaxRetries:             1,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:         server.URL + "/broken.bin",
		Filename:    "broken.bin",
		NumSegments: 2,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	final := waitForStatus(t, eng, task.ID, StatusFailed, 20*time.Second)
	if final.Retries < 1 {
		t.Fatalf("expected retry attempts to be recorded, got %d", final.Retries)
	}
	if !strings.Contains(final.ErrorMessage, "500") {
		t.Fatalf("expected the last error in the message, got %q", final.ErrorMessage)
	}
}

// TestProbeRetriesTransientFailure checks that probe failures are retried before
// the task is marked failed.
func TestProbeRetriesTransientFailure(t *testing.T) {
	payload, expectedHash := generateTestPayload(64 * 1024)
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) <= 2 {
			http.Error(w, "probe unavailable", http.StatusServiceUnavailable)
			return
		}
		servePayloadRange(w, r, payload)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 1,
		DefaultSegments:        2,
		MaxRetries:             2,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:       server.URL + "/probe.bin",
		Filename:  "probe.bin",
		AutoStart: true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	final := waitForStatus(t, eng, task.ID, StatusCompleted, 20*time.Second)
	if final.Retries < 1 {
		t.Fatalf("expected probe retry to be recorded, got %d", final.Retries)
	}

	actualHash, err := computeFileSHA256(filepath.Join(tempDir, "probe.bin"))
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if actualHash != expectedHash {
		t.Fatalf("hash mismatch! expected %s, got %s", expectedHash, actualHash)
	}
}

// TestGlobalSpeedLimit checks that one shared bucket caps aggregate throughput
// across all segments and that 0 means unlimited.
func TestGlobalSpeedLimit(t *testing.T) {
	payload, expectedHash := generateTestPayload(512 * 1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servePayloadRange(w, r, payload)
	}))
	defer server.Close()

	download := func(limitKBps int64, filename string) (time.Duration, *Task) {
		tempDir := t.TempDir()
		eng, err := NewEngine(Config{
			MaxConcurrentDownloads: 1,
			DefaultSegments:        4,
			MaxRetries:             0,
			SpeedLimitKBps:         limitKBps,
			DownloadDir:            tempDir,
			StateFilePath:          filepath.Join(tempDir, "state.json"),
		})
		if err != nil {
			t.Fatalf("failed to init engine: %v", err)
		}
		defer eng.Close()

		start := time.Now()
		task, err := eng.AddTask(AddTaskRequest{
			URL:         server.URL + "/" + filename,
			Filename:    filename,
			NumSegments: 4,
			AutoStart:   true,
		})
		if err != nil {
			t.Fatalf("failed to add task: %v", err)
		}
		final := waitForStatus(t, eng, task.ID, StatusCompleted, 30*time.Second)

		hash, err := computeFileSHA256(filepath.Join(tempDir, filename))
		if err != nil {
			t.Fatalf("failed to read %s: %v", filename, err)
		}
		if hash != expectedHash {
			t.Fatalf("%s hash mismatch after limited download", filename)
		}
		return time.Since(start), final
	}

	limitedDur, _ := download(256, "limited.bin")
	unlimitedDur, _ := download(0, "unlimited.bin")

	const ceiling = int64(256 * 1024)
	limitedThroughput := float64(len(payload)) / limitedDur.Seconds()
	if limitedThroughput > float64(ceiling)*1.5 {
		t.Fatalf("throughput %.0f B/s exceeded 1.5x ceiling %d B/s (took %v)", limitedThroughput, ceiling, limitedDur)
	}
	if limitedDur < time.Second {
		t.Fatalf("speed limit had no effect: 512 KiB finished in %v", limitedDur)
	}
	if unlimitedDur > limitedDur/2 {
		t.Fatalf("unlimited run (%v) was not clearly faster than limited run (%v)", unlimitedDur, limitedDur)
	}
}

// TestSpeedLimitUpdateAppliesWithoutRestart checks that UpdateConfig changes the
// ceiling of an in-flight download.
func TestSpeedLimitUpdateAppliesWithoutRestart(t *testing.T) {
	payload, _ := generateTestPayload(512 * 1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servePayloadRange(w, r, payload)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 1,
		DefaultSegments:        4,
		MaxRetries:             0,
		SpeedLimitKBps:         64, // would need ~8s for this payload
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	start := time.Now()
	task, err := eng.AddTask(AddTaskRequest{
		URL:         server.URL + "/respeed.bin",
		Filename:    "respeed.bin",
		NumSegments: 4,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	cfg := eng.Config()
	cfg.SpeedLimitKBps = 0
	eng.UpdateConfig(cfg)

	waitForStatus(t, eng, task.ID, StatusCompleted, 20*time.Second)
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("raising the limit did not take effect in flight (took %v)", elapsed)
	}
}

// TestSingleStreamRetry covers retry of a non-ranged whole-file download.
func TestSingleStreamRetry(t *testing.T) {
	payload, expectedHash := generateTestPayload(256 * 1024)
	var getAttempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Accept-Ranges: the engine must fall back to a single stream.
		if r.Method == http.MethodGet && atomic.AddInt32(&getAttempts, 1) == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(payload)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 1,
		MaxRetries:             2,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:       server.URL + "/single.bin",
		Filename:  "single.bin",
		AutoStart: true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	final := waitForStatus(t, eng, task.ID, StatusCompleted, 20*time.Second)
	if final.Retries < 1 {
		t.Fatalf("expected single-stream retry to be recorded, got %d", final.Retries)
	}
	actualHash, err := computeFileSHA256(filepath.Join(tempDir, "single.bin"))
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if actualHash != expectedHash {
		t.Fatalf("hash mismatch! expected %s, got %s", expectedHash, actualHash)
	}
}

// TestCompletionCallbackFires checks the notification hook runs once on success.
func TestCompletionCallbackFires(t *testing.T) {
	payload, _ := generateTestPayload(32 * 1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servePayloadRange(w, r, payload)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 1,
		MaxRetries:             0,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	done := make(chan string, 1)
	eng.SetOnTaskCompleted(func(task *Task) { done <- task.Filename })

	if _, err := eng.AddTask(AddTaskRequest{
		URL:       server.URL + "/notify.bin",
		Filename:  "notify.bin",
		AutoStart: true,
	}); err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	select {
	case name := <-done:
		if name != "notify.bin" {
			t.Fatalf("callback got filename %q", name)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("completion callback never fired")
	}
}

// TestSegmentResumeAfterTruncatedBody checks that a retried segment asks only for
// the bytes it has not written yet.
func TestSegmentResumeAfterTruncatedBody(t *testing.T) {
	payload, expectedHash := generateTestPayload(64 * 1024)
	half := len(payload) / 2

	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		first := len(ranges) == 1
		mu.Unlock()

		w.Header().Set("Accept-Ranges", "bytes")
		if first {
			// Claim and deliver only the first half of the requested range so the
			// client observes a clean EOF with bytes still missing.
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", half-1, len(payload)))
			w.Header().Set("Content-Length", strconv.Itoa(half))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[:half])
			return
		}
		servePayloadRange(w, r, payload)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "segment.part")
	file, err := os.OpenFile(outPath, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer file.Close()

	seg := &Segment{Index: 0, Start: 0, End: int64(len(payload) - 1), Status: "idle"}
	if err := downloadSegment(context.Background(), server.Client(), server.URL, nil, "", "", seg, file, nil, nil, 2, nil, nil); err != nil {
		t.Fatalf("downloadSegment failed: %v", err)
	}

	mu.Lock()
	gotRanges := append([]string(nil), ranges...)
	mu.Unlock()
	if len(gotRanges) != 2 {
		t.Fatalf("expected 2 requests, got %d (%v)", len(gotRanges), gotRanges)
	}
	wantResume := fmt.Sprintf("bytes=%d-%d", half, len(payload)-1)
	if gotRanges[1] != wantResume {
		t.Fatalf("retry range = %q, want %q", gotRanges[1], wantResume)
	}

	hash, err := computeFileSHA256(outPath)
	if err != nil {
		t.Fatalf("failed to hash part file: %v", err)
	}
	if hash != expectedHash {
		t.Fatalf("hash mismatch! expected %s, got %s", expectedHash, hash)
	}
}

// TestPauseWhileThrottled checks that a pause interrupts the rate-limited wait.
func TestPauseWhileThrottled(t *testing.T) {
	payload, _ := generateTestPayload(512 * 1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servePayloadRange(w, r, payload)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 1,
		DefaultSegments:        4,
		MaxRetries:             0,
		SpeedLimitKBps:         32,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:         server.URL + "/paused.bin",
		Filename:    "paused.bin",
		NumSegments: 4,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	if err := eng.PauseTask(task.ID); err != nil {
		t.Fatalf("failed to pause: %v", err)
	}
	waitForStatus(t, eng, task.ID, StatusPaused, 3*time.Second)
}

// TestClientErrorNotRetried checks that a 4xx response fails without retrying.
func TestClientErrorNotRetried(t *testing.T) {
	payload, _ := generateTestPayload(64 * 1024)
	var getAttempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			servePayloadRange(w, r, payload)
			return
		}
		atomic.AddInt32(&getAttempts, 1)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 1,
		DefaultSegments:        2,
		MaxRetries:             3,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:         server.URL + "/missing.bin",
		Filename:    "missing.bin",
		NumSegments: 2,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	final := waitForStatus(t, eng, task.ID, StatusFailed, 10*time.Second)
	if final.Retries != 0 {
		t.Fatalf("4xx response was retried %d time(s)", final.Retries)
	}
	if got := atomic.LoadInt32(&getAttempts); int(got) != len(final.Segments) {
		t.Fatalf("expected exactly one GET per segment (%d), got %d requests", len(final.Segments), got)
	}
}

// TestConfigPersistenceRoundTrip checks that tunable settings survive a restart.
func TestConfigPersistenceRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 1,
		DefaultSegments:        4,
		MaxRetries:             1,
		DownloadDir:            tempDir,
		StateFilePath:          statePath,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	cfg := eng.Config()
	cfg.MaxConcurrentDownloads = 5
	cfg.DefaultSegments = 16
	cfg.MaxRetries = 2
	cfg.SpeedLimitKBps = 2048
	eng.UpdateConfig(cfg)
	eng.Close()

	reloaded, err := NewEngine(Config{
		DownloadDir:   tempDir,
		StateFilePath: statePath,
	})
	if err != nil {
		t.Fatalf("failed to reload engine: %v", err)
	}
	defer reloaded.Close()

	got := reloaded.Config()
	if got.MaxConcurrentDownloads != 5 || got.DefaultSegments != 16 || got.MaxRetries != 2 || got.SpeedLimitKBps != 2048 {
		t.Fatalf("persisted config not restored: %+v", got)
	}
}

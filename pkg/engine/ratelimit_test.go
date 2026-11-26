package engine

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestPerTaskSpeedLimitThrottlesDynamically verifies that SetTaskSpeedLimit
// throttles an active download and can be updated live mid-download without
// deadlock or data race.
func TestPerTaskSpeedLimitThrottlesDynamically(t *testing.T) {
	const dataSize = 256 * 1024 // 256 KiB
	payload, expectedHash := generateTestPayload(dataSize)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(dataSize))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if rh := r.Header.Get("Range"); strings.HasPrefix(rh, "bytes=") {
			parts := strings.Split(strings.TrimPrefix(rh, "bytes="), "-")
			start, _ := strconv.Atoi(parts[0])
			end := dataSize - 1
			if parts[1] != "" {
				end, _ = strconv.Atoi(parts[1])
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, dataSize))
			w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start : end+1])
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        2,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	// Start with a tight 128 KB/s per-task limit (~2s for 256 KiB if unchanged).
	start := time.Now()
	task, err := eng.AddTask(AddTaskRequest{
		URL:            server.URL + "/throttled.bin",
		Filename:       "throttled.bin",
		NumSegments:    2,
		SpeedLimitKBps: 128,
		AutoStart:      true,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// After 250ms, lift the per-task limit to unlimited (0) live mid-download.
	time.Sleep(250 * time.Millisecond)
	if err := eng.SetTaskSpeedLimit(task.ID, 0); err != nil {
		t.Fatalf("SetTaskSpeedLimit failed: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var latest *Task
	for time.Now().Before(deadline) {
		latest, _ = eng.GetTask(task.ID)
		if latest.Status == StatusCompleted || latest.Status == StatusFailed {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	elapsed := time.Since(start)

	if latest == nil || latest.Status != StatusCompleted {
		t.Fatalf("expected throttled task to complete, got %v", latest)
	}
	if latest.SpeedLimitKBps != 0 {
		t.Errorf("expected updated SpeedLimitKBps=0, got %d", latest.SpeedLimitKBps)
	}
	// Lifting the cap at 250ms should let it finish well before the ~2s capped duration.
	if elapsed > 1800*time.Millisecond {
		t.Errorf("expected dynamic unthrottle to finish quickly, took %v", elapsed)
	}
	actualHash, err := computeFileSHA256(latest.SavePath)
	if err != nil || actualHash != expectedHash {
		t.Fatalf("SHA-256 mismatch: err=%v want=%s got=%s", err, expectedHash, actualHash)
	}
}

func TestCappedReadBufferSize(t *testing.T) {
	taskLim := newRateLimiter(64 * 1024)    // 64 KB/s -> /4 = 16 KB
	globalLim := newRateLimiter(128 * 1024) // 128 KB/s
	if got := cappedReadBufferSize(64*1024, taskLim, globalLim); got != 16*1024 {
		t.Fatalf("expected 16384, got %d", got)
	}
	// Very low rate clamps to 1024 minimum.
	lowLim := newRateLimiter(1024)
	if got := cappedReadBufferSize(64*1024, lowLim, nil); got != 1024 {
		t.Fatalf("expected 1024 minimum, got %d", got)
	}
	// Unlimited returns baseSize.
	if got := cappedReadBufferSize(64*1024, newRateLimiter(0), newRateLimiter(0)); got != 64*1024 {
		t.Fatalf("expected 65536 when unlimited, got %d", got)
	}
}

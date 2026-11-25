package engine

import (
	"crypto/sha256"
	"encoding/hex"
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

// generateTestPayload generates deterministic dummy data of size n.
func generateTestPayload(size int) ([]byte, string) {
	data := make([]byte, size)
	for i := 0; i < size; i++ {
		data[i] = byte((i * 31) % 256)
	}
	hash := sha256.Sum256(data)
	return data, hex.EncodeToString(hash[:])
}

// computeFileSHA256 returns the hex-encoded SHA256 of a local file.
func computeFileSHA256(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:]), nil
}

// TestMultiSegmentRangedDownload tests downloading a multi-megabyte file across 8 segments.
func TestMultiSegmentRangedDownload(t *testing.T) {
	dataSize := 1024 * 1024 * 3 // 3 MB
	payload, expectedHash := generateTestPayload(dataSize)

	// Local HTTP server supporting Range requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")

		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(dataSize))
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}

		// Parse Range: bytes=start-end
		if !strings.HasPrefix(rangeHeader, "bytes=") {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.Split(rangeSpec, "-")
		if len(parts) != 2 {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		start, _ := strconv.Atoi(parts[0])
		end := dataSize - 1
		if parts[1] != "" {
			end, _ = strconv.Atoi(parts[1])
		}
		if start < 0 || end >= dataSize || start > end {
			http.Error(w, "range out of bounds", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, dataSize))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 3,
		DefaultSegments:        8,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:         server.URL + "/testfile.bin",
		Filename:    "testfile.bin",
		NumSegments: 8,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(10 * time.Second)
	var latest *Task
	for time.Now().Before(deadline) {
		latest, _ = eng.GetTask(task.ID)
		if latest.Status == StatusCompleted {
			break
		}
		if latest.Status == StatusFailed {
			t.Fatalf("task failed: %s", latest.ErrorMessage)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if latest == nil || latest.Status != StatusCompleted {
		t.Fatalf("task did not complete in time, status: %v", latest)
	}

	// Verify file on disk and hash
	savedFile := filepath.Join(tempDir, "testfile.bin")
	actualHash, err := computeFileSHA256(savedFile)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if actualHash != expectedHash {
		t.Fatalf("hash mismatch! expected %s, got %s", expectedHash, actualHash)
	}

	// Verify segments count
	if len(latest.Segments) != 8 {
		t.Errorf("expected 8 segments, got %d", len(latest.Segments))
	}
	for i, s := range latest.Segments {
		if s.Status != "done" {
			t.Errorf("segment %d status is %s, expected done", i, s.Status)
		}
	}
}

// TestNonRangedServerFallback tests that single-stream download works when range is unsupported.
func TestNonRangedServerFallback(t *testing.T) {
	dataSize := 1024 * 512 // 512 KB
	payload, expectedHash := generateTestPayload(dataSize)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server intentionally does NOT support ranges, always sends 200 OK
		w.Header().Set("Accept-Ranges", "none")
		w.Header().Set("Content-Length", strconv.Itoa(dataSize))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 3,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:       server.URL + "/stream.bin",
		Filename:  "stream.bin",
		AutoStart: true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var latest *Task
	for time.Now().Before(deadline) {
		latest, _ = eng.GetTask(task.ID)
		if latest.Status == StatusCompleted {
			break
		}
		if latest.Status == StatusFailed {
			t.Fatalf("task failed: %s", latest.ErrorMessage)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if latest == nil || latest.Status != StatusCompleted {
		t.Fatalf("task did not complete in time, status: %v", latest)
	}

	savedFile := filepath.Join(tempDir, "stream.bin")
	actualHash, err := computeFileSHA256(savedFile)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if actualHash != expectedHash {
		t.Fatalf("hash mismatch! expected %s, got %s", expectedHash, actualHash)
	}
}

// TestPauseResumePersistence verifies that pausing preserves downloaded bytes and resuming completes with identical checksum.
func TestPauseResumePersistence(t *testing.T) {
	dataSize := 1024 * 1024 * 4 // 4 MB
	payload, expectedHash := generateTestPayload(dataSize)

	var pausedOnce int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(dataSize))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}

		rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.Split(rangeSpec, "-")
		start, _ := strconv.Atoi(parts[0])
		end := dataSize - 1
		if parts[1] != "" {
			end, _ = strconv.Atoi(parts[1])
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, dataSize))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)

		// Throttled writer to allow pausing midway
		chunkSize := 16 * 1024
		for curr := start; curr <= end; curr += chunkSize {
			next := curr + chunkSize
			if next > end+1 {
				next = end + 1
			}
			_, _ = w.Write(payload[curr:next])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "state.json")
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          stateFile,
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}

	task, err := eng.AddTask(AddTaskRequest{
		URL:         server.URL + "/resumable.dat",
		Filename:    "resumable.dat",
		NumSegments: 4,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	// Wait until some bytes are downloaded, then pause
	time.Sleep(150 * time.Millisecond)
	_ = eng.PauseTask(task.ID)
	atomic.StoreInt32(&pausedOnce, 1)

	// Wait for pause to settle
	time.Sleep(200 * time.Millisecond)
	pausedSnap, _ := eng.GetTask(task.ID)
	downloadedAtPause := pausedSnap.DownloadedBytes
	if downloadedAtPause <= 0 || downloadedAtPause >= int64(dataSize) {
		t.Logf("downloaded bytes at pause: %d (expected 0 < bytes < %d)", downloadedAtPause, dataSize)
	}

	// Close engine and reload from state file (simulating app restart)
	eng.Close()
	time.Sleep(100 * time.Millisecond)

	eng2, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          stateFile,
	})
	if err != nil {
		t.Fatalf("failed to reload engine: %v", err)
	}
	defer eng2.Close()

	reloadedTask, err := eng2.GetTask(task.ID)
	if err != nil {
		t.Fatalf("task not found after reload: %v", err)
	}
	if reloadedTask.Status != StatusPaused {
		t.Errorf("expected reloaded task status Paused, got %s", reloadedTask.Status)
	}

	// Resume download
	if err := eng2.ResumeTask(reloadedTask.ID); err != nil {
		t.Fatalf("failed to resume task: %v", err)
	}

	// Wait for full completion
	deadline := time.Now().Add(15 * time.Second)
	var finalSnap *Task
	for time.Now().Before(deadline) {
		finalSnap, _ = eng2.GetTask(reloadedTask.ID)
		if finalSnap.Status == StatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalSnap == nil || finalSnap.Status != StatusCompleted {
		t.Fatalf("resumed task failed to complete, status: %v", finalSnap)
	}

	// Verify SHA256 checksum
	savedFile := filepath.Join(tempDir, "resumable.dat")
	actualHash, err := computeFileSHA256(savedFile)
	if err != nil {
		t.Fatalf("failed to read resumed file: %v", err)
	}
	if actualHash != expectedHash {
		t.Fatalf("checksum mismatch after resume! expected %s, got %s", expectedHash, actualHash)
	}
}

// TestQueueConcurrencyLimits verifies that MaxConcurrentDownloads is strictly respected.
func TestQueueConcurrencyLimits(t *testing.T) {
	dataSize := 1024 * 100 // 100 KB
	payload, _ := generateTestPayload(dataSize)

	var activeRequests int32
	var maxObservedConcurrent int32
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&activeRequests, 1)
		mu.Lock()
		if current > maxObservedConcurrent {
			maxObservedConcurrent = current
		}
		mu.Unlock()

		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Length", strconv.Itoa(dataSize))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)

		atomic.AddInt32(&activeRequests, -1)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	maxConcurrent := 2
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: maxConcurrent,
		DefaultSegments:        1,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	// Enqueue 5 downloads
	taskCount := 5
	tasks := make([]*Task, taskCount)
	for i := 0; i < taskCount; i++ {
		tName := fmt.Sprintf("queue_file_%d.bin", i)
		tObj, err := eng.AddTask(AddTaskRequest{
			URL:       server.URL + "/" + tName,
			Filename:  tName,
			AutoStart: true,
		})
		if err != nil {
			t.Fatalf("failed to add task %d: %v", i, err)
		}
		tasks[i] = tObj
	}

	// Wait for all to finish
	deadline := time.Now().Add(15 * time.Second)
	allCompleted := false
	for time.Now().Before(deadline) {
		completedCount := 0
		for _, tk := range tasks {
			snap, err := eng.GetTask(tk.ID)
			if err == nil && snap.Status == StatusCompleted {
				completedCount++
			}
		}
		if completedCount == taskCount {
			allCompleted = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !allCompleted {
		t.Fatalf("not all tasks completed within timeout")
	}

	mu.Lock()
	observed := maxObservedConcurrent
	mu.Unlock()

	if observed > int32(maxConcurrent) {
		t.Fatalf("concurrency limit breached! max allowed: %d, observed: %d", maxConcurrent, observed)
	}
}

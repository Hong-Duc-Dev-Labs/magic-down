package engine

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestDynamicWorkStealingRebalancesSlowSegment simulates 1 artificially throttled
// segment alongside 3 fast segments and verifies:
// (a) work stealing splits the slow segment,
// (b) final file SHA-256 matches 100%,
// (c) total time is significantly faster than static split without work-stealing,
// (d) Pause/Resume mid-split produces exact SHA-256.
func TestDynamicWorkStealingRebalancesSlowSegment(t *testing.T) {
	// 12 MiB split across 4 initial segments -> 3 MiB per segment (>= 2 MiB steal threshold).
	const dataSize = 12 * 1024 * 1024
	const segSize = dataSize / 4
	payload, expectedHash := generateTestPayload(dataSize)

	// Throttle only requests that start at byte 0 (segment 0).
	// Stolen tail segments start at > 0 so they run at full speed.
	const slowChunkSize = 64 * 1024
	const slowChunkDelay = 25 * time.Millisecond // 3 MiB / 64 KiB = 48 chunks -> ~1.2s if unsplit

	var slowReqCount atomic.Int64

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

		rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
		parts := strings.Split(rangeSpec, "-")
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

		// Segment 0 starts at offset 0 and is artificially throttled.
		if start == 0 && end >= segSize-1 {
			slowReqCount.Add(1)
			flusher, _ := w.(http.Flusher)
			for off := start; off <= end; off += slowChunkSize {
				select {
				case <-r.Context().Done():
					return
				default:
				}
				chunkEnd := off + slowChunkSize - 1
				if chunkEnd > end {
					chunkEnd = end
				}
				if _, err := w.Write(payload[off : chunkEnd+1]); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(slowChunkDelay)
			}
			return
		}

		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()

	// Part 1: Verify work stealing splits the slow segment, finishes faster than static, and matches SHA-256.
	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	startTime := time.Now()
	task, err := eng.AddTask(AddTaskRequest{
		URL:         server.URL + "/large.bin",
		Filename:    "large.bin",
		NumSegments: 4,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("failed to add task: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	var latest *Task
	for time.Now().Before(deadline) {
		latest, _ = eng.GetTask(task.ID)
		if latest.Status == StatusCompleted {
			break
		}
		if latest.Status == StatusFailed {
			t.Fatalf("task failed: %s", latest.ErrorMessage)
		}
		time.Sleep(25 * time.Millisecond)
	}
	elapsed := time.Since(startTime)

	if latest == nil || latest.Status != StatusCompleted {
		t.Fatalf("task did not complete in time, status: %v", latest)
	}

	// (a) Work stealing must have split the slow segment (more than 4 segments total).
	if len(latest.Segments) <= 4 {
		t.Fatalf("expected work-stealing to split slow segment (> 4 segments), got %d", len(latest.Segments))
	}

	// (b) Final file SHA-256 must match 100%.
	actualHash, err := computeFileSHA256(latest.SavePath)
	if err != nil {
		t.Fatalf("failed to hash downloaded file: %v", err)
	}
	if actualHash != expectedHash {
		t.Fatalf("SHA-256 mismatch: want %s, got %s", expectedHash, actualHash)
	}

	// (c) Static split of 3 MiB at 64 KiB / 25 ms takes >= 1.2s; work-stealing should finish significantly faster.
	staticEstimate := time.Duration(segSize/slowChunkSize) * slowChunkDelay
	if elapsed >= staticEstimate {
		t.Fatalf("expected work-stealing (%v) to finish faster than static split estimate (%v)", elapsed, staticEstimate)
	}

	// Verify no gaps or overlaps across all final segments.
	var totalSegBytes int64
	for _, s := range latest.Segments {
		segLen := s.End - s.Start + 1
		if s.Downloaded != segLen {
			t.Errorf("segment %d downloaded %d != expected length %d", s.Index, s.Downloaded, segLen)
		}
		totalSegBytes += s.Downloaded
	}
	if totalSegBytes != dataSize {
		t.Errorf("sum of segment bytes %d != file size %d", totalSegBytes, dataSize)
	}

	// Part 2: Pause mid-split and resume -> must still produce exact SHA-256.
	tempDir2 := t.TempDir()
	eng2, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir2,
		StateFilePath:          filepath.Join(tempDir2, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine2: %v", err)
	}
	defer eng2.Close()

	task2, err := eng2.AddTask(AddTaskRequest{
		URL:         server.URL + "/large-pause.bin",
		Filename:    "large-pause.bin",
		NumSegments: 4,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("failed to add task2: %v", err)
	}

	// Wait until work-stealing has occurred, then pause immediately.
	pauseDeadline := time.Now().Add(5 * time.Second)
	pausedMidSplit := false
	for time.Now().Before(pauseDeadline) {
		snap, _ := eng2.GetTask(task2.ID)
		if snap.Status == StatusCompleted {
			break
		}
		if len(snap.Segments) > 4 && snap.Status == StatusDownloading {
			if err := eng2.PauseTask(task2.ID); err != nil {
				t.Fatalf("PauseTask failed: %v", err)
			}
			pausedMidSplit = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if pausedMidSplit {
		// Wait briefly for paused state to settle, then resume.
		time.Sleep(80 * time.Millisecond)
		if err := eng2.ResumeTask(task2.ID); err != nil {
			t.Fatalf("ResumeTask failed: %v", err)
		}
	}

	resumeDeadline := time.Now().Add(15 * time.Second)
	var latest2 *Task
	for time.Now().Before(resumeDeadline) {
		latest2, _ = eng2.GetTask(task2.ID)
		if latest2.Status == StatusCompleted {
			break
		}
		if latest2.Status == StatusFailed {
			t.Fatalf("task2 failed after resume: %s", latest2.ErrorMessage)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if latest2 == nil || latest2.Status != StatusCompleted {
		t.Fatalf("task2 did not complete after pause/resume: %v", latest2)
	}

	actualHash2, err := computeFileSHA256(latest2.SavePath)
	if err != nil {
		t.Fatalf("failed to hash task2 file: %v", err)
	}
	if actualHash2 != expectedHash {
		t.Fatalf("task2 SHA-256 mismatch after pause/resume: want %s, got %s", expectedHash, actualHash2)
	}
	if _, err := os.Stat(latest2.PartPath); !os.IsNotExist(err) {
		t.Errorf("expected .magicpart to be finalized and removed")
	}
}

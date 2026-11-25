package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/image/webp"
)

func makeSamplePNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 10), B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode sample png: %v", err)
	}
	return buf.Bytes()
}

func TestImageFormatConversion(t *testing.T) {
	samplePNG := makeSamplePNGBytes(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(samplePNG)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(samplePNG)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 3,
		DefaultSegments:        2,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("failed to init engine: %v", err)
	}
	defer eng.Close()

	cases := []struct {
		targetFmt string
		wantExt   string
		decode    func(data []byte) error
	}{
		{
			targetFmt: "jpg",
			wantExt:   ".jpg",
			decode: func(data []byte) error {
				if len(data) < 3 || data[0] != 0xFF || data[1] != 0xD8 {
					return fmt.Errorf("missing JPEG magic bytes")
				}
				_, err := jpeg.Decode(bytes.NewReader(data))
				return err
			},
		},
		{
			targetFmt: "webp",
			wantExt:   ".webp",
			decode: func(data []byte) error {
				if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
					return fmt.Errorf("missing RIFF/WEBP magic header")
				}
				_, err := webp.Decode(bytes.NewReader(data))
				return err
			},
		},
		{
			targetFmt: "png",
			wantExt:   ".png",
			decode: func(data []byte) error {
				_, err := png.Decode(bytes.NewReader(data))
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run("convert_to_"+tc.targetFmt, func(t *testing.T) {
			task, err := eng.AddTask(AddTaskRequest{
				URL:           server.URL + "/photo_" + tc.targetFmt + ".png",
				ConvertFormat: tc.targetFmt,
				AutoStart:     true,
			})
			if err != nil {
				t.Fatalf("AddTask failed: %v", err)
			}

			deadline := time.Now().Add(10 * time.Second)
			var latest *Task
			for time.Now().Before(deadline) {
				latest, _ = eng.GetTask(task.ID)
				if latest.Status == StatusCompleted || latest.Status == StatusFailed {
					break
				}
				time.Sleep(40 * time.Millisecond)
			}
			if latest == nil || latest.Status != StatusCompleted {
				t.Fatalf("conversion task did not complete: %+v", latest)
			}
			if filepath.Ext(latest.SavePath) != tc.wantExt {
				t.Fatalf("expected extension %s, got %s", tc.wantExt, latest.SavePath)
			}
			outBytes, err := os.ReadFile(latest.SavePath)
			if err != nil {
				t.Fatalf("failed to read converted file: %v", err)
			}
			if err := tc.decode(outBytes); err != nil {
				t.Fatalf("converted %s file failed decode verification: %v", tc.targetFmt, err)
			}
		})
	}
}

func TestHLSM3U8Download(t *testing.T) {
	// Prepare 6 distinct media segments
	segCount := 6
	segs := make([][]byte, segCount)
	var fullConcat bytes.Buffer
	for i := 0; i < segCount; i++ {
		chunk := make([]byte, 32*1024)
		for j := range chunk {
			chunk[j] = byte((i*53 + j*7) % 256)
		}
		segs[i] = chunk
		fullConcat.Write(chunk)
	}
	expectedSum := sha256.Sum256(fullConcat.Bytes())
	expectedHash := hex.EncodeToString(expectedSum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = fmt.Fprintln(w, "#EXTM3U")
			_, _ = fmt.Fprintln(w, `#EXT-X-STREAM-INF:BANDWIDTH=1200000,RESOLUTION=1280x720`)
			_, _ = fmt.Fprintln(w, "720p/index.m3u8")
			_, _ = fmt.Fprintln(w, `#EXT-X-STREAM-INF:BANDWIDTH=4500000,RESOLUTION=1920x1080`)
			_, _ = fmt.Fprintln(w, "1080p/index.m3u8")
		case "/1080p/index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = fmt.Fprintln(w, "#EXTM3U")
			_, _ = fmt.Fprintln(w, "#EXT-X-VERSION:3")
			for i := 0; i < segCount; i++ {
				_, _ = fmt.Fprintf(w, "#EXTINF:4.0,\nseg%d.ts\n", i)
			}
			_, _ = fmt.Fprintln(w, "#EXT-X-ENDLIST")
		default:
			var idx int
			if _, err := fmt.Sscanf(r.URL.Path, "/1080p/seg%d.ts", &idx); err == nil && idx >= 0 && idx < segCount {
				w.Header().Set("Content-Type", "video/mp2t")
				_, _ = w.Write(segs[idx])
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

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

	task, err := eng.AddTask(AddTaskRequest{
		URL:         server.URL + "/master.m3u8",
		Filename:    "demo_video.ts",
		NumSegments: 4,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var latest *Task
	for time.Now().Before(deadline) {
		latest, _ = eng.GetTask(task.ID)
		if latest.Status == StatusCompleted || latest.Status == StatusFailed {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}

	if latest == nil || latest.Status != StatusCompleted {
		t.Fatalf("HLS task failed to complete: %+v", latest)
	}

	actualHash, err := computeFileSHA256(latest.SavePath)
	if err != nil {
		t.Fatalf("failed to hash HLS output: %v", err)
	}
	if actualHash != expectedHash {
		t.Fatalf("HLS output hash mismatch! expected %s, got %s", expectedHash, actualHash)
	}
}

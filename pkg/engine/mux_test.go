package engine

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMuxVideoAudio_CopyAndAACFallback(t *testing.T) {
	tmp := t.TempDir()
	videoPath := filepath.Join(tmp, "v.mp4")
	audioPath := filepath.Join(tmp, "a.m4a")
	outPath := filepath.Join(tmp, "out.mp4")
	if err := os.WriteFile(videoPath, []byte("VIDEO"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, []byte("AUDIO"), 0644); err != nil {
		t.Fatal(err)
	}

	origLocator := ffmpegLocator
	origRunner := ffmpegRunner
	defer func() {
		ffmpegLocator = origLocator
		ffmpegRunner = origRunner
	}()

	ffmpegLocator = func() (string, error) { return "/mock/ffmpeg", nil }

	var calls [][]string
	ffmpegRunner = func(ctx context.Context, ffmpegPath string, args []string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		// First call (-c copy) fails to exercise AAC fallback
		if len(calls) == 1 {
			return []byte("opus not supported in mp4"), errors.New("exit status 1")
		}
		target := args[len(args)-1]
		return nil, os.WriteFile(target, []byte("MUXED_MP4"), 0644)
	}

	if err := MuxVideoAudio(context.Background(), videoPath, audioPath, outPath); err != nil {
		t.Fatalf("MuxVideoAudio failed: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 ffmpeg calls (copy + aac fallback), got %d", len(calls))
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	if string(data) != "MUXED_MP4" {
		t.Fatalf("unexpected muxed content: %q", string(data))
	}
}

func TestRunMuxedDownload_EndToEndAndCleanup(t *testing.T) {
	videoData := bytes.Repeat([]byte("V"), 64*1024)
	audioData := bytes.Repeat([]byte("A"), 16*1024)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/video":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(videoData)
		case "/audio":
			w.Header().Set("Content-Type", "audio/mp4")
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
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:       srv.URL + "/video",
		AudioURL:  srv.URL + "/audio",
		Filename:  "sample_1080p.mp4",
		AutoStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap, _ := eng.GetTask(task.ID)
		if snap.Status == StatusCompleted {
			// Verify final file exists and temp files are cleaned up
			got, err := os.ReadFile(snap.SavePath)
			if err != nil {
				t.Fatalf("read final muxed file: %v", err)
			}
			if len(got) != len(videoData)+len(audioData) {
				t.Fatalf("expected %d bytes, got %d", len(videoData)+len(audioData), len(got))
			}
			if _, err := os.Stat(snap.PartPath + ".video"); !os.IsNotExist(err) {
				t.Fatalf("expected temp video file to be cleaned up")
			}
			if _, err := os.Stat(snap.PartPath + ".audio"); !os.IsNotExist(err) {
				t.Fatalf("expected temp audio file to be cleaned up")
			}
			return
		}
		if snap.Status == StatusFailed {
			t.Fatalf("muxed download failed: %s", snap.ErrorMessage)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for muxed download to complete")
}

func TestRunMuxedDownload_MissingFFmpegFailsCleanly(t *testing.T) {
	origLocator := ffmpegLocator
	defer func() { ffmpegLocator = origLocator }()
	ffmpegLocator = func() (string, error) {
		return "", errors.New("ffmpeg not found: install ffmpeg")
	}

	tmp := t.TempDir()
	cfg := DefaultConfig(tmp, filepath.Join(tmp, "state.json"))
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:       "http://127.0.0.1:1/video",
		AudioURL:  "http://127.0.0.1:1/audio",
		Filename:  "clip.mp4",
		AutoStart: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snap, _ := eng.GetTask(task.ID)
		if snap.Status == StatusFailed {
			if !strings.Contains(snap.ErrorMessage, "ffmpeg not found") {
				t.Fatalf("expected ffmpeg error, got %q", snap.ErrorMessage)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for missing ffmpeg failure")
}

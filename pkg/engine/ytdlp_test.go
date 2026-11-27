package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateYouTubeURL(t *testing.T) {
	tests := []struct {
		raw       string
		wantURL   string
		wantID    string
		wantError bool
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", false},
		{"https://youtu.be/dQw4w9WgXcQ?si=abc", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ", false},
		{"https://m.youtube.com/shorts/abcDEF12345", "https://www.youtube.com/shorts/abcDEF12345", "abcDEF12345", false},
		{"https://music.youtube.com/watch?v=jNQXAC9IVRw", "https://www.youtube.com/watch?v=jNQXAC9IVRw", "jNQXAC9IVRw", false},
		{"https://www.youtube.com/live/LiveID_1234", "https://www.youtube.com/watch?v=LiveID_1234", "LiveID_1234", false},
		{"https://evil.example.com/watch?v=dQw4w9WgXcQ", "", "", true},
		{"file:///etc/passwd", "", "", true},
		{"--exec=calc.exe", "", "", true},
		{"https://www.youtube.com/", "", "", true},
	}

	for _, tc := range tests {
		gotURL, gotID, err := ValidateYouTubeURL(tc.raw)
		if tc.wantError {
			if err == nil {
				t.Errorf("ValidateYouTubeURL(%q) expected error, got (%q, %q)", tc.raw, gotURL, gotID)
			}
			continue
		}
		if err != nil {
			t.Errorf("ValidateYouTubeURL(%q) unexpected error: %v", tc.raw, err)
			continue
		}
		if gotURL != tc.wantURL || gotID != tc.wantID {
			t.Errorf("ValidateYouTubeURL(%q) = (%q, %q), want (%q, %q)", tc.raw, gotURL, gotID, tc.wantURL, tc.wantID)
		}
	}
}

func TestValidateYTDLPFormat(t *testing.T) {
	valid := []string{
		"",
		"bestaudio/best",
		"bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=1080]+bestaudio/best[height<=1080]",
	}
	for _, v := range valid {
		if _, err := ValidateYTDLPFormat(v); err != nil {
			t.Errorf("ValidateYTDLPFormat(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{
		"-f best",
		"best; rm -rf /",
		"best & calc",
		"best $(whoami)",
		"best video",
	}
	for _, v := range invalid {
		if _, err := ValidateYTDLPFormat(v); err == nil {
			t.Errorf("ValidateYTDLPFormat(%q) expected error, got nil", v)
		}
	}
}

func TestParseYTDLPInfoJSONAndResolveCache(t *testing.T) {
	ClearYouTubeResolveCache()
	origLocator := ytdlpLocator
	origRunner := ytdlpJSONRunner
	t.Cleanup(func() {
		ytdlpLocator = origLocator
		ytdlpJSONRunner = origRunner
		ClearYouTubeResolveCache()
	})

	sampleJSON := []byte(`{
		"id": "aqz-KE-bpKQ",
		"title": "Big Buck Bunny 60fps 4K",
		"thumbnail": "https://i.ytimg.com/vi/aqz-KE-bpKQ/maxresdefault.jpg",
		"duration": 634.5,
		"is_live": false,
		"formats": [
			{"format_id":"251","ext":"webm","vcodec":"none","acodec":"opus","abr":160,"filesize":12000000},
			{"format_id":"140","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","abr":128,"filesize":10000000},
			{"format_id":"315","ext":"webm","width":3840,"height":2160,"fps":60,"vcodec":"vp9","acodec":"none","tbr":18000,"filesize":500000000},
			{"format_id":"137","ext":"mp4","width":1920,"height":1080,"fps":30,"vcodec":"avc1.640028","acodec":"none","tbr":4000,"filesize":100000000},
			{"format_id":"299","ext":"mp4","width":1920,"height":1080,"fps":60,"vcodec":"avc1.64002a","acodec":"none","tbr":5500,"filesize":140000000},
			{"format_id":"18","ext":"mp4","width":640,"height":360,"fps":30,"vcodec":"avc1.42001E","acodec":"mp4a.40.2","tbr":600,"filesize":20000000},
			{"format_id":"sb0","ext":"mhtml","width":160,"height":90,"vcodec":"none","acodec":"none"}
		]
	}`)

	var calls atomic.Int32
	ytdlpLocator = func() (string, error) { return "mock-yt-dlp", nil }
	ytdlpJSONRunner = func(ctx context.Context, path string, args []string) ([]byte, error) {
		calls.Add(1)
		if path != "mock-yt-dlp" {
			t.Errorf("unexpected binary path: %s", path)
		}
		if len(args) < 2 || args[len(args)-2] != "--" {
			t.Errorf("expected '--' separator before URL in args: %v", args)
		}
		return sampleJSON, nil
	}

	res1, err := ResolveYouTube(context.Background(), "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	if err != nil {
		t.Fatalf("ResolveYouTube failed: %v", err)
	}
	if res1.VideoID != "aqz-KE-bpKQ" || res1.Title != "Big Buck Bunny 60fps 4K" {
		t.Fatalf("unexpected metadata: %+v", res1)
	}
	if len(res1.Variants) != 3 {
		t.Fatalf("expected 3 deduplicated heights (2160p60, 1080p60, 360p), got %d: %+v", len(res1.Variants), res1.Variants)
	}
	if res1.Variants[0].Quality != "2160p60" || res1.Variants[1].Quality != "1080p60" || res1.Variants[2].Quality != "360p" {
		t.Errorf("unexpected quality order: %q, %q, %q", res1.Variants[0].Quality, res1.Variants[1].Quality, res1.Variants[2].Quality)
	}
	if res1.Variants[1].Size != 152000000 { // 140MB video + 12MB best audio
		t.Errorf("expected muxed size 152000000, got %d", res1.Variants[1].Size)
	}
	if len(res1.MP3Variants) != 3 {
		t.Fatalf("expected 3 MP3 variants, got %d", len(res1.MP3Variants))
	}

	// Second call should hit cache without invoking yt-dlp again.
	res2, err := ResolveYouTube(context.Background(), "https://youtu.be/aqz-KE-bpKQ")
	if err != nil {
		t.Fatalf("second ResolveYouTube failed: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 yt-dlp call due to cache, got %d", calls.Load())
	}
	if res2.VideoID != res1.VideoID {
		t.Errorf("cached videoId mismatch")
	}

	// Missing yt-dlp returns sentinel error.
	ClearYouTubeResolveCache()
	ytdlpLocator = func() (string, error) { return "", ErrYTDLPNotFound }
	_, err = ResolveYouTube(context.Background(), "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	if !IsYTDLPMissingError(err) {
		t.Errorf("expected IsYTDLPMissingError(err) == true, got %v", err)
	}
}

func TestParseYTDLPProgressLine(t *testing.T) {
	ev, ok := parseYTDLPProgressLine("MAGICDOWN_PROGRESS:5242880|10485760|NA|1048576.5|5")
	if !ok {
		t.Fatal("expected progress line to parse")
	}
	if ev.Downloaded != 5242880 || ev.Total != 10485760 || ev.Speed != 1048576 || ev.ETA != 5 || ev.Muxing {
		t.Errorf("unexpected progress event: %+v", ev)
	}

	evMux, ok := parseYTDLPProgressLine(`[Merger] Merging formats into "output.mp4"`)
	if !ok || !evMux.Muxing {
		t.Errorf("expected Merger line to set Muxing=true, got %+v (ok=%v)", evMux, ok)
	}
}

func TestTaskRunYTDLPDownloadAndCancel(t *testing.T) {
	origYtdlp := ytdlpLocator
	origFFmpeg := ffmpegLocator
	origDownload := ytdlpDownloadRunner
	t.Cleanup(func() {
		ytdlpLocator = origYtdlp
		ffmpegLocator = origFFmpeg
		ytdlpDownloadRunner = origDownload
	})

	ytdlpLocator = func() (string, error) { return "mock-yt-dlp", nil }
	ffmpegLocator = func() (string, error) { return filepath.Join(t.TempDir(), "ffmpeg.exe"), nil }

	tmpDir := t.TempDir()
	savePath := filepath.Join(tmpDir, "demo.mp4")

	ytdlpDownloadRunner = func(ctx context.Context, path string, args []string, onLine func(string)) error {
		onLine("MAGICDOWN_PROGRESS:1024|2048|NA|512|2")
		onLine("[Merger] Merging formats into output.mp4")
		// Locate -o template and write a mock output file
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "-o" {
				outFile := strings.ReplaceAll(args[i+1], "%(ext)s", "mp4")
				return os.WriteFile(outFile, []byte("mock-mp4-content-12345"), 0644)
			}
		}
		return nil
	}

	task := &Task{
		ID:          "yt-test-1",
		URL:         "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		Filename:    "demo.mp4",
		SavePath:    savePath,
		PartPath:    savePath + ".magicpart",
		YTDLPFormat: "bestvideo[height<=1080]+bestaudio/best",
		Status:      StatusQueued,
	}

	var sawMuxing atomic.Bool
	if err := task.Start(context.Background(), nil, func(up *Task) {
		if up.GetStatus() == StatusMuxing {
			sawMuxing.Store(true)
		}
	}); err != nil {
		t.Fatalf("task.Start failed: %v", err)
	}
	if task.GetStatus() != StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %s (err=%s)", task.GetStatus(), task.ErrorMessage)
	}
	if !sawMuxing.Load() {
		t.Errorf("expected StatusMuxing callback during merger phase")
	}
	data, err := os.ReadFile(savePath)
	if err != nil || string(data) != "mock-mp4-content-12345" {
		t.Fatalf("expected finalized output file, got err=%v data=%q", err, string(data))
	}
	if _, err := os.Stat(task.PartPath + ".ytdlp_tmp"); !os.IsNotExist(err) {
		t.Errorf("expected temporary yt-dlp directory to be cleaned up")
	}

	// Verify cancellation cleans up temp dir and preserves StatusCancelled
	ctx, cancel := context.WithCancel(context.Background())
	ytdlpDownloadRunner = func(runCtx context.Context, path string, args []string, onLine func(string)) error {
		cancel()
		<-runCtx.Done()
		return runCtx.Err()
	}
	cancelSave := filepath.Join(tmpDir, "cancelled.mp4")
	cancelTask := &Task{
		ID:          "yt-test-cancel",
		URL:         "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		Filename:    "cancelled.mp4",
		SavePath:    cancelSave,
		PartPath:    cancelSave + ".magicpart",
		YTDLPFormat: "bestvideo[height<=720]+bestaudio/best",
		Status:      StatusQueued,
	}
	_ = cancelTask.Start(ctx, nil, nil)
	if cancelTask.GetStatus() != StatusCancelled {
		t.Errorf("expected StatusCancelled, got %s", cancelTask.GetStatus())
	}
	if _, err := os.Stat(cancelTask.PartPath + ".ytdlp_tmp"); !os.IsNotExist(err) {
		t.Errorf("expected cancelled temp directory to be cleaned up")
	}
	_ = time.Now()
}

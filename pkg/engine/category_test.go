package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCategoryForFilename(t *testing.T) {
	cases := []struct {
		filename string
		url      string
		want     string
	}{
		{"movie.mp4", "", CategoryVideo},
		{"", "https://cdn.example.com/clip.webm?token=1", CategoryVideo},
		{"song.flac", "", CategoryMusic},
		{"podcast.m4a", "", CategoryMusic},
		{"backup.tar.gz", "", CategoryCompressed},
		{"archive.7z", "", CategoryCompressed},
		{"report.pdf", "", CategoryDocuments},
		{"notes.md", "", CategoryDocuments},
		{"installer.exe", "", CategoryPrograms},
		{"ubuntu.iso", "", CategoryPrograms},
		{"readme.unknown", "", CategoryOther},
	}

	for _, tc := range cases {
		got := CategoryForFilename(tc.filename, tc.url)
		if got != tc.want {
			t.Errorf("CategoryForFilename(%q, %q) = %q, want %q", tc.filename, tc.url, got, tc.want)
		}
	}
}

func TestResolveSaveDir(t *testing.T) {
	base := filepath.Join("C:", "Downloads", "MagicDown")
	if got := ResolveSaveDir(base, CategoryVideo, true); got != filepath.Join(base, CategoryVideo) {
		t.Fatalf("got %q, want Video subfolder", got)
	}
	if got := ResolveSaveDir(filepath.Join(base, CategoryVideo), CategoryVideo, true); got != filepath.Join(base, CategoryVideo) {
		t.Fatalf("expected idempotent subfolder, got %q", got)
	}
	if got := ResolveSaveDir(base, CategoryVideo, false); got != base {
		t.Fatalf("expected base dir when autoCategorize=false, got %q", got)
	}
}

func TestUniqueFilePath(t *testing.T) {
	dir := t.TempDir()
	first := UniqueFilePath(dir, "video.mp4")
	if filepath.Base(first) != "video.mp4" {
		t.Fatalf("expected video.mp4, got %q", first)
	}
	if err := os.WriteFile(first, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	second := UniqueFilePath(dir, "video.mp4")
	if filepath.Base(second) != "video (1).mp4" {
		t.Fatalf("expected video (1).mp4, got %q", second)
	}
	if err := os.WriteFile(second+".magicpart", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	third := UniqueFilePath(dir, "video.mp4")
	if filepath.Base(third) != "video (2).mp4" {
		t.Fatalf("expected video (2).mp4, got %q", third)
	}
}

func TestAddTask_AutoCategoryAndDownloadLater(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig(tmp, filepath.Join(tmp, "state.json"))
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	task, err := eng.AddTask(AddTaskRequest{
		URL:         "https://example.com/setup.exe",
		Filename:    "setup.exe",
		AutoStart:   false,
		StartPaused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusPaused {
		t.Fatalf("expected Paused status for Download Later, got %q", task.Status)
	}
	if task.Category != CategoryPrograms {
		t.Fatalf("expected Programs category, got %q", task.Category)
	}
	wantDir := filepath.Join(tmp, CategoryPrograms)
	if filepath.Dir(task.SavePath) != wantDir {
		t.Fatalf("expected SavePath in %q, got %q", wantDir, task.SavePath)
	}
}

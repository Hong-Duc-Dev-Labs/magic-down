package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandBatchURLsWildcardsAndValidation(t *testing.T) {
	input := `
# Comment line
https://cdn.example.com/part_[01-05].zip
https://cdn.example.com/vol_[a-c].tar.gz
ftp://unsupported.example.com/skip.bin
https://cdn.example.com/part_03.zip
`
	urls, warnings, err := ExpandBatchURLs(input)
	if err != nil {
		t.Fatalf("ExpandBatchURLs failed: %v", err)
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning for ftp:// line, got %v", warnings)
	}
	// 5 numeric + 3 alpha (duplicate part_03.zip deduplicated) = 8 URLs.
	if len(urls) != 8 {
		t.Fatalf("expected 8 expanded URLs, got %d: %v", len(urls), urls)
	}
	if urls[0] != "https://cdn.example.com/part_01.zip" || urls[4] != "https://cdn.example.com/part_05.zip" {
		t.Errorf("unexpected zero-padded expansion: %v", urls[:5])
	}
	if urls[5] != "https://cdn.example.com/vol_a.tar.gz" || urls[7] != "https://cdn.example.com/vol_c.tar.gz" {
		t.Errorf("unexpected alpha expansion: %v", urls[5:])
	}

	// Cap enforcement: > 500 URLs must be rejected cleanly.
	if _, _, err := ExpandBatchURLs("https://cdn.example.com/item_[1-501].bin"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected max-500 cap error, got %v", err)
	}

	// Reversed/malformed patterns must be rejected.
	for _, bad := range []string{
		"https://cdn.example.com/item_[20-01].bin",
		"https://cdn.example.com/item_[z-a].bin",
		"https://cdn.example.com/item_[unclosed.bin",
	} {
		if _, _, err := ExpandBatchURLs(bad); err == nil {
			t.Errorf("expected error for malformed pattern %q", bad)
		}
	}
}

func TestAddTasksBatch500URLsSingleLock(t *testing.T) {
	tempDir := t.TempDir()
	eng, err := NewEngine(Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		DownloadDir:            tempDir,
		StateFilePath:          filepath.Join(tempDir, "state.json"),
	})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer eng.Close()

	reqs := make([]AddTaskRequest, MaxBatchURLs)
	for i := 0; i < MaxBatchURLs; i++ {
		reqs[i] = AddTaskRequest{
			URL:         fmt.Sprintf("https://cdn.example.com/file_%03d.bin", i+1),
			Filename:    fmt.Sprintf("file_%03d.bin", i+1),
			SaveDir:     tempDir,
			StartPaused: true,
		}
	}

	added, err := eng.AddTasksBatch(reqs)
	if err != nil {
		t.Fatalf("AddTasksBatch(500) failed: %v", err)
	}
	if len(added) != MaxBatchURLs {
		t.Fatalf("expected %d added tasks, got %d", MaxBatchURLs, len(added))
	}
	if got := len(eng.GetTasks()); got != MaxBatchURLs {
		t.Fatalf("expected %d tasks in engine, got %d", MaxBatchURLs, got)
	}

	// Colliding filenames within a single batch must receive distinct SavePaths
	// even before any .magicpart exists on disk.
	collideReqs := []AddTaskRequest{
		{URL: "https://a.example.com/same.zip", Filename: "same.zip", SaveDir: tempDir, StartPaused: true},
		{URL: "https://b.example.com/same.zip", Filename: "same.zip", SaveDir: tempDir, StartPaused: true},
	}
	collided, err := eng.AddTasksBatch(collideReqs)
	if err != nil {
		t.Fatalf("AddTasksBatch(colliding) failed: %v", err)
	}
	if collided[0].SavePath == collided[1].SavePath {
		t.Fatalf("expected distinct SavePaths for colliding batch filenames, both got %q", collided[0].SavePath)
	}
}

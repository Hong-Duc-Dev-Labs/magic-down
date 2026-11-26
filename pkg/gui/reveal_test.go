package gui

import (
	"os"
	"path/filepath"
	"testing"

	"magicdown/pkg/engine"
)

func TestResolveTaskRevealTarget_AutoCategoriesUnicodeAndSpecialChars(t *testing.T) {
	baseDir := t.TempDir()
	cfg := engine.Config{
		DownloadDir:    baseDir,
		AutoCategorize: true,
	}

	cases := []struct {
		name     string
		category string
		filename string
	}{
		{
			name:     "korean spaces and at-sign",
			category: engine.CategoryVideo,
			filename: "@8daytp - 이 영상 너무 귀엽고 ㅈㄴ중독돼서 계속 보는 중.mp4",
		},
		{
			name:     "vietnamese brackets commas and duplicate suffix",
			category: engine.CategoryDocuments,
			filename: "Tài liệu học tập [2026], bản cuối (1).pdf",
		},
		{
			name:     "square brackets and spaces in music",
			category: engine.CategoryMusic,
			filename: "[44.1-24 Tidal MQA] Nhạc Việt, Chill (2).mp3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			catDir := filepath.Join(baseDir, tc.category)
			if err := os.MkdirAll(catDir, 0755); err != nil {
				t.Fatalf("mkdir %s: %v", catDir, err)
			}
			fullPath := filepath.Join(catDir, tc.filename)
			if err := os.WriteFile(fullPath, []byte("payload"), 0644); err != nil {
				t.Fatalf("write %s: %v", fullPath, err)
			}

			task := &engine.Task{
				ID:       "task-" + tc.name,
				Category: tc.category,
				Filename: tc.filename,
				SavePath: fullPath,
				Status:   engine.StatusCompleted,
			}

			got, ok := resolveTaskRevealTarget(task, cfg)
			if !ok {
				t.Fatalf("expected reveal target to resolve")
			}
			wantAbs, err := filepath.Abs(fullPath)
			if err != nil {
				t.Fatalf("abs: %v", err)
			}
			if got.Path != filepath.Clean(wantAbs) {
				t.Fatalf("got Path %q, want %q", got.Path, filepath.Clean(wantAbs))
			}
			if !got.SelectFile {
				t.Fatalf("expected SelectFile=true for existing file %q", got.Path)
			}
		})
	}
}

func TestResolveTaskRevealTarget_EngineAutoCategoryAndDuplicateRename(t *testing.T) {
	baseDir := t.TempDir()
	stateFile := filepath.Join(baseDir, "state.json")

	eng, err := engine.NewEngine(engine.Config{
		MaxConcurrentDownloads: 2,
		DefaultSegments:        4,
		MaxRetries:             1,
		DownloadDir:            baseDir,
		AutoCategorize:         true,
		StateFilePath:          stateFile,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	const rawName = "@8daytp - 이 영상 너무 귀엽고 ㅈㄴ중독돼서 계속 보는 중.mp4"

	// Pre-create the original file in Video/ so AddTask triggers duplicate rename " (1)".
	videoDir := filepath.Join(baseDir, engine.CategoryVideo)
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	firstPath := filepath.Join(videoDir, rawName)
	if err := os.WriteFile(firstPath, []byte("first"), 0644); err != nil {
		t.Fatalf("write first: %v", err)
	}

	task, err := eng.AddTask(engine.AddTaskRequest{
		URL:         "https://video.twimg.com/ext_tw_video/12345/pu/vid/avc1/720x1280/clip.mp4",
		Filename:    rawName,
		StartPaused: true,
	})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	wantSecond := filepath.Join(videoDir, "@8daytp - 이 영상 너무 귀엽고 ㅈㄴ중독돼서 계속 보는 중 (1).mp4")
	if task.SavePath != wantSecond {
		t.Fatalf("expected duplicate-renamed SavePath %q, got %q", wantSecond, task.SavePath)
	}

	// Simulate completed download writing the duplicate-renamed file.
	if err := os.WriteFile(task.SavePath, []byte("second"), 0644); err != nil {
		t.Fatalf("write second: %v", err)
	}

	got, ok := resolveTaskRevealTarget(task, eng.Config())
	if !ok {
		t.Fatalf("expected reveal target to resolve")
	}
	if got.Path != wantSecond || !got.SelectFile {
		t.Fatalf("got %+v, want Path=%q SelectFile=true", got, wantSecond)
	}
}

func TestResolveTaskRevealTarget_FallbackToParentWhenFileMissing(t *testing.T) {
	baseDir := t.TempDir()
	videoDir := filepath.Join(baseDir, engine.CategoryVideo)
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	missingFile := filepath.Join(videoDir, "@8daytp - 이 영상 너무 귀엽고 [deleted].mp4")
	task := &engine.Task{
		ID:       "missing-1",
		Category: engine.CategoryVideo,
		Filename: filepath.Base(missingFile),
		SavePath: missingFile,
		Status:   engine.StatusCompleted,
	}

	got, ok := resolveTaskRevealTarget(task, engine.Config{
		DownloadDir:    baseDir,
		AutoCategorize: true,
	})
	if !ok {
		t.Fatalf("expected parent fallback to resolve")
	}
	if got.Path != videoDir {
		t.Fatalf("got fallback Path %q, want parent dir %q", got.Path, videoDir)
	}
	if got.SelectFile {
		t.Fatalf("expected SelectFile=false when file does not exist")
	}
}

func TestResolveTaskRevealTarget_FallbackToDownloadDirWhenParentMissing(t *testing.T) {
	baseDir := t.TempDir()
	missingParent := filepath.Join(baseDir, "DeletedSubfolder")
	missingFile := filepath.Join(missingParent, "clip.mp4")

	task := &engine.Task{
		ID:       "missing-parent",
		Category: engine.CategoryVideo,
		Filename: "clip.mp4",
		SavePath: missingFile,
	}

	got, ok := resolveTaskRevealTarget(task, engine.Config{
		DownloadDir:    baseDir,
		AutoCategorize: true,
	})
	if !ok {
		t.Fatalf("expected download dir fallback to resolve")
	}
	if got.Path != baseDir || got.SelectFile {
		t.Fatalf("got %+v, want Path=%q SelectFile=false", got, baseDir)
	}
}

func TestResolveTaskRevealTarget_NormalizesUncleanRelativePath(t *testing.T) {
	baseDir := t.TempDir()
	subDir := filepath.Join(baseDir, "Video")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	targetFile := filepath.Join(subDir, "Tiếng Việt [test].mp4")
	if err := os.WriteFile(targetFile, []byte("ok"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	unclean := filepath.ToSlash(filepath.Join(subDir, ".", "..", "Video", "Tiếng Việt [test].mp4"))
	got, ok := resolveRevealTargetPath(unclean, baseDir)
	if !ok {
		t.Fatalf("expected unclean path to resolve")
	}
	if got.Path != targetFile || !got.SelectFile {
		t.Fatalf("got %+v, want Path=%q SelectFile=true", got, targetFile)
	}
}

func TestResolveTaskRevealTarget_EmptySavePathUsesAutoCategory(t *testing.T) {
	baseDir := t.TempDir()
	musicDir := filepath.Join(baseDir, engine.CategoryMusic)
	if err := os.MkdirAll(musicDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mp3Path := filepath.Join(musicDir, "Bài hát, [remix].mp3")
	if err := os.WriteFile(mp3Path, []byte("mp3"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	task := &engine.Task{
		ID:           "empty-savepath",
		Filename:     "Bài hát, [remix].mp3",
		OutputFormat: "mp3",
	}

	got, ok := resolveTaskRevealTarget(task, engine.Config{
		DownloadDir:    baseDir,
		AutoCategorize: true,
	})
	if !ok {
		t.Fatalf("expected empty SavePath task to resolve via Auto Categories")
	}
	if got.Path != mp3Path || !got.SelectFile {
		t.Fatalf("got %+v, want Path=%q SelectFile=true", got, mp3Path)
	}
}

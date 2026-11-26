package gui

import (
	"os"
	"path/filepath"
	"strings"

	"magicdown/pkg/engine"
)

// revealTarget describes a normalized filesystem target to open or highlight in
// the OS file manager.
type revealTarget struct {
	Path       string
	SelectFile bool
}

// normalizeAbsPath cleans p and resolves it to an absolute path using OS-native
// path separators.
func normalizeAbsPath(p string) string {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.Clean(trimmed)
	if abs, err := filepath.Abs(cleaned); err == nil {
		return filepath.Clean(abs)
	}
	return cleaned
}

// resolveRevealTargetPath resolves a raw file or directory path into a clean
// absolute revealTarget. If the path points to an existing file, SelectFile is
// true so the file manager highlights it. If the path is an existing directory,
// or if the file no longer exists on disk, it falls back to the parent
// directory (or fallbackDir if the parent directory also does not exist).
func resolveRevealTargetPath(targetPath, fallbackDir string) (revealTarget, bool) {
	absPath := normalizeAbsPath(targetPath)
	if absPath != "" {
		if fi, err := os.Stat(absPath); err == nil {
			return revealTarget{
				Path:       absPath,
				SelectFile: !fi.IsDir(),
			}, true
		}

		parentDir := filepath.Dir(absPath)
		if parentDir != "" && parentDir != "." {
			if fi, err := os.Stat(parentDir); err == nil && fi.IsDir() {
				return revealTarget{
					Path:       parentDir,
					SelectFile: false,
				}, true
			}
		}

		if fb := normalizeAbsPath(fallbackDir); fb != "" {
			if fi, err := os.Stat(fb); err == nil && fi.IsDir() {
				return revealTarget{
					Path:       fb,
					SelectFile: false,
				}, true
			}
		}

		if parentDir != "" && parentDir != "." {
			return revealTarget{
				Path:       parentDir,
				SelectFile: false,
			}, true
		}
		return revealTarget{
			Path:       absPath,
			SelectFile: false,
		}, true
	}

	if fb := normalizeAbsPath(fallbackDir); fb != "" {
		return revealTarget{
			Path:       fb,
			SelectFile: false,
		}, true
	}
	return revealTarget{}, false
}

// resolveTaskRevealTarget computes the real on-disk reveal target for a task,
// accounting for Auto Categories, duplicate renaming, muxed MP4 / converted MP3
// paths persisted on the task, and parent-directory fallback when the file has
// been moved or deleted.
func resolveTaskRevealTarget(task *engine.Task, cfg engine.Config) (revealTarget, bool) {
	if task == nil {
		return revealTarget{}, false
	}

	category := strings.TrimSpace(task.Category)
	if category == "" && (task.Filename != "" || task.URL != "") {
		if outFmt, ok := engine.NormalizeOutputFormat(task.OutputFormat); ok && outFmt == "mp3" {
			category = engine.CategoryMusic
		} else if task.AudioURL != "" {
			category = engine.CategoryVideo
		} else {
			category = engine.CategoryForFilename(task.Filename, task.URL)
		}
	}

	fallbackDir := ""
	if strings.TrimSpace(cfg.DownloadDir) != "" {
		fallbackDir = engine.ResolveSaveDir(cfg.DownloadDir, category, cfg.AutoCategorize)
		if fi, err := os.Stat(fallbackDir); err != nil || !fi.IsDir() {
			fallbackDir = cfg.DownloadDir
		}
	}

	candidate := strings.TrimSpace(task.SavePath)
	if candidate == "" && strings.TrimSpace(task.Filename) != "" && strings.TrimSpace(cfg.DownloadDir) != "" {
		dir := engine.ResolveSaveDir(cfg.DownloadDir, category, cfg.AutoCategorize)
		candidate = filepath.Join(dir, engine.SanitizeFilename(task.Filename))
	}

	return resolveRevealTargetPath(candidate, fallbackDir)
}

func (a *App) revealTaskInFileManager(task *engine.Task) {
	if task == nil {
		return
	}
	latest := task
	if a.engine != nil && task.ID != "" {
		if fresh, err := a.engine.GetTask(task.ID); err == nil && fresh != nil {
			latest = fresh
		}
	}
	var cfg engine.Config
	if a.engine != nil {
		cfg = a.engine.Config()
	}
	target, ok := resolveTaskRevealTarget(latest, cfg)
	if !ok {
		return
	}
	go func() {
		_ = revealInFileManager(target)
	}()
}

func openInFileManager(targetPath string) {
	target, ok := resolveRevealTargetPath(targetPath, "")
	if !ok {
		return
	}
	go func() {
		_ = revealInFileManager(target)
	}()
}

package engine

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Auto-categorization folder names (IDM-style).
const (
	CategoryVideo      = "Video"
	CategoryMusic      = "Music"
	CategoryCompressed = "Compressed"
	CategoryDocuments  = "Documents"
	CategoryPrograms   = "Programs"
	CategoryOther      = "Other"
)

// AllCategories lists every supported auto-category in display order.
var AllCategories = []string{
	CategoryVideo,
	CategoryMusic,
	CategoryCompressed,
	CategoryDocuments,
	CategoryPrograms,
	CategoryOther,
}

// CategoryForFilename classifies a download into one of the standard categories
// based on its filename extension (falling back to the URL path extension).
func CategoryForFilename(filename, rawURL string) string {
	target := strings.TrimSpace(filename)
	if target == "" {
		if u, err := url.Parse(rawURL); err == nil {
			target = u.Path
		}
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(target), "."))
	switch ext {
	case "mp4", "mkv", "avi", "mov", "webm", "flv", "wmv", "m4v", "ts", "3gp", "mpeg", "mpg":
		return CategoryVideo
	case "mp3", "flac", "wav", "ogg", "m4a", "aac", "wma", "opus":
		return CategoryMusic
	case "zip", "rar", "7z", "tar", "gz", "bz2", "xz", "zst", "tgz":
		return CategoryCompressed
	case "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "md", "csv", "epub", "rtf":
		return CategoryDocuments
	case "exe", "msi", "dmg", "pkg", "deb", "rpm", "appimage", "apk", "iso", "img", "bin":
		return CategoryPrograms
	default:
		return CategoryOther
	}
}

// ResolveSaveDir returns the target directory for a download, appending the
// category subfolder when autoCategorize is enabled.
func ResolveSaveDir(baseDir, category string, autoCategorize bool) string {
	base := strings.TrimSpace(baseDir)
	if !autoCategorize || category == "" {
		return base
	}
	// Avoid duplicating the category folder if baseDir already ends with it.
	if strings.EqualFold(filepath.Base(base), category) {
		return base
	}
	return filepath.Join(base, category)
}

// UniqueFilePath returns a non-colliding file path inside dir for filename.
// If dir/filename (or its .magicpart) already exists, it appends " (1)", " (2)", etc.
func UniqueFilePath(dir, filename string) string {
	clean := SanitizeFilename(filename)
	candidate := filepath.Join(dir, clean)
	if !pathExists(candidate) && !pathExists(candidate+".magicpart") {
		return candidate
	}

	ext := filepath.Ext(clean)
	stem := strings.TrimSuffix(clean, ext)
	for i := 1; i < 10000; i++ {
		nextName := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		nextPath := filepath.Join(dir, nextName)
		if !pathExists(nextPath) && !pathExists(nextPath+".magicpart") {
			return nextPath
		}
	}
	return candidate
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

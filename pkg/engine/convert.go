package engine

import (
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/HugoSmits86/nativewebp"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// NormalizeImageFormat validates and canonicalizes a requested target image format.
// Returns "", false if format is empty or unsupported.
func NormalizeImageFormat(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(strings.TrimPrefix(format, "."))) {
	case "png":
		return "png", true
	case "jpg", "jpeg":
		return "jpg", true
	case "webp":
		return "webp", true
	default:
		return "", false
	}
}

// ReplaceExtension replaces the file extension of path with "." + newExt.
func ReplaceExtension(path, newExt string) string {
	ext := filepath.Ext(path)
	if ext != "" {
		path = strings.TrimSuffix(path, ext)
	}
	return path + "." + strings.TrimPrefix(newExt, ".")
}

// ConvertImageFile decodes srcPath (PNG/JPEG/GIF/WebP/BMP) and writes a real encoded
// image in targetFormat ("png", "jpg", "webp") atomically to dstPath.
func ConvertImageFile(srcPath, dstPath, targetFormat string) error {
	format, ok := NormalizeImageFormat(targetFormat)
	if !ok {
		return fmt.Errorf("unsupported target image format: %q", targetFormat)
	}

	inFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source image: %w", err)
	}
	img, _, err := image.Decode(inFile)
	_ = inFile.Close()
	if err != nil {
		return fmt.Errorf("failed to decode source image: %w", err)
	}

	tmpOut := dstPath + ".converting"
	outFile, err := os.OpenFile(tmpOut, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create output image: %w", err)
	}

	var encErr error
	switch format {
	case "png":
		encErr = png.Encode(outFile, img)
	case "jpg":
		encErr = jpeg.Encode(outFile, img, &jpeg.Options{Quality: 92})
	case "webp":
		encErr = nativewebp.Encode(outFile, img, nil)
	}

	syncErr := outFile.Sync()
	closeErr := outFile.Close()
	if encErr != nil {
		_ = os.Remove(tmpOut)
		return fmt.Errorf("failed to encode image as %s: %w", format, encErr)
	}
	if syncErr != nil {
		_ = os.Remove(tmpOut)
		return fmt.Errorf("failed to flush output image: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpOut)
		return fmt.Errorf("failed to close output image: %w", closeErr)
	}

	if _, err := os.Stat(dstPath); err == nil {
		_ = os.Remove(dstPath)
	}
	if err := os.Rename(tmpOut, dstPath); err != nil {
		_ = os.Remove(tmpOut)
		return fmt.Errorf("failed to move converted image: %w", err)
	}
	return nil
}

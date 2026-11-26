package engine

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

// FileHashes holds the computed cryptographic digests of a downloaded file.
type FileHashes struct {
	SHA256 string `json:"sha256"`
	MD5    string `json:"md5"`
	Size   int64  `json:"size"`
}

// ComputeFileHashes computes SHA-256 and MD5 digests in a single streaming pass.
// It checks ctx cancellation between chunks so closing the UI modal cancels
// computation cleanly on large files.
func ComputeFileHashes(ctx context.Context, path string) (FileHashes, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileHashes{}, err
	}
	defer f.Close()

	h256 := sha256.New()
	hMD5 := md5.New()
	mw := io.MultiWriter(h256, hMD5)

	buf := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return FileHashes{}, err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, werr := mw.Write(buf[:n]); werr != nil {
				return FileHashes{}, werr
			}
			total += int64(n)
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return FileHashes{}, readErr
		}
	}

	return FileHashes{
		SHA256: hex.EncodeToString(h256.Sum(nil)),
		MD5:    hex.EncodeToString(hMD5.Sum(nil)),
		Size:   total,
	}, nil
}

// NormalizeExpectedHash trims whitespace, lowercases hex characters, and strips
// optional "sha256:" or "md5:" prefixes.
func NormalizeExpectedHash(expected string) string {
	s := strings.ToLower(strings.TrimSpace(expected))
	s = strings.TrimPrefix(s, "sha256:")
	s = strings.TrimPrefix(s, "sha-256:")
	s = strings.TrimPrefix(s, "md5:")
	return strings.TrimSpace(s)
}

// CompareFileHash compares expected (SHA-256 or MD5, case-insensitive) against
// computed hashes, returning (matched, algorithm).
func CompareFileHash(hashes FileHashes, expected string) (bool, string) {
	norm := NormalizeExpectedHash(expected)
	if norm == "" {
		return false, ""
	}
	if len(norm) == 64 {
		return strings.EqualFold(hashes.SHA256, norm), "SHA-256"
	}
	if len(norm) == 32 {
		return strings.EqualFold(hashes.MD5, norm), "MD5"
	}
	if strings.EqualFold(hashes.SHA256, norm) {
		return true, "SHA-256"
	}
	if strings.EqualFold(hashes.MD5, norm) {
		return true, "MD5"
	}
	return false, ""
}

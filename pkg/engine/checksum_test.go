package engine

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeFileHashesAndComparison(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "sample.bin")
	content := []byte("MagicDown checksum verification payload 1234567890")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	want256Bytes := sha256.Sum256(content)
	want256 := hex.EncodeToString(want256Bytes[:])
	wantMD5Bytes := md5.Sum(content)
	wantMD5 := hex.EncodeToString(wantMD5Bytes[:])

	got, err := ComputeFileHashes(context.Background(), filePath)
	if err != nil {
		t.Fatalf("ComputeFileHashes failed: %v", err)
	}
	if got.SHA256 != want256 {
		t.Errorf("SHA256 = %q, want %q", got.SHA256, want256)
	}
	if got.MD5 != wantMD5 {
		t.Errorf("MD5 = %q, want %q", got.MD5, wantMD5)
	}
	if got.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", got.Size, len(content))
	}

	// Case-insensitive, whitespace-trimmed, prefix-tolerant comparisons.
	if ok, algo := CompareFileHash(got, "  SHA256:"+want256+"  "); !ok || algo != "SHA-256" {
		t.Errorf("expected SHA-256 match, got ok=%v algo=%q", ok, algo)
	}
	if ok, algo := CompareFileHash(got, "md5: "+wantMD5); !ok || algo != "MD5" {
		t.Errorf("expected MD5 match, got ok=%v algo=%q", ok, algo)
	}
	if ok, _ := CompareFileHash(got, "00000000000000000000000000000000"); ok {
		t.Errorf("expected mismatch for wrong hash")
	}

	// Cancelled context returns immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ComputeFileHashes(ctx, filePath); err == nil {
		t.Errorf("expected cancelled context error")
	}
}

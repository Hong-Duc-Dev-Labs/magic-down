package ipc

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	// TokenHeader is the required HTTP header carrying the per-session IPC auth token.
	TokenHeader = "X-MagicDown-Token"
	// MaxBodyBytes caps incoming IPC JSON request bodies (10 MB).
	MaxBodyBytes int64 = 10 * 1024 * 1024
)

// DefaultTokenPath returns MAGICDOWN_IPC_TOKEN_PATH if set, otherwise
// %LOCALAPPDATA%\MagicDown\ipc.token (or user config dir fallback).
func DefaultTokenPath() string {
	if custom := strings.TrimSpace(os.Getenv("MAGICDOWN_IPC_TOKEN_PATH")); custom != "" {
		return custom
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, "MagicDown", "ipc.token")
	}
	if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
		return filepath.Join(cfgDir, "MagicDown", "ipc.token")
	}
	return filepath.Join(os.TempDir(), "magicdown-ipc.token")
}

// GenerateAuthToken returns a cryptographically random 64-char hex token.
func GenerateAuthToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// WriteAuthToken persists token with restricted user-only permissions (0600).
func WriteAuthToken(path, token string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("token path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(token)), 0600)
}

// ReadAuthToken reads and trims the auth token from path.
func ReadAuthToken(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func constantTimeTokenEqual(a, b string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ValidateHTTPURL verifies that raw is a well-formed http:// or https:// URL with a host.
func ValidateHTTPURL(raw string) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return errors.New("missing url")
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported url scheme %q (only http and https allowed)", u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		return errors.New("url missing host")
	}
	return nil
}

// ValidateFilename rejects path traversal sequences or path separators in a filename.
func ValidateFilename(raw string) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	if strings.Contains(s, "\x00") {
		return errors.New("invalid filename: contains null byte")
	}
	if strings.Contains(s, "..") || strings.ContainsAny(s, `/\`) {
		return fmt.Errorf("invalid filename %q: path traversal not allowed", raw)
	}
	if filepath.IsAbs(s) {
		return fmt.Errorf("invalid filename %q: absolute path not allowed", raw)
	}
	return nil
}

// ValidateSaveDir ensures candidate (when non-empty) stays within baseDir and has no traversal.
func ValidateSaveDir(baseDir, candidate string) error {
	c := strings.TrimSpace(candidate)
	if c == "" {
		return nil
	}
	if strings.Contains(c, "..") {
		return fmt.Errorf("invalid save_dir %q: path traversal not allowed", candidate)
	}
	if strings.TrimSpace(baseDir) == "" {
		return nil
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return err
	}
	absCand, err := filepath.Abs(c)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absBase, absCand)
	if err != nil {
		return fmt.Errorf("invalid save_dir %q: %w", candidate, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("save_dir %q is outside allowed download directory", candidate)
	}
	return nil
}

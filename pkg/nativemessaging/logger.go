package nativemessaging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultLogMaxBytes caps native-host.log before rotating to native-host.log.1.
	DefaultLogMaxBytes int64 = 2 * 1024 * 1024
)

var sensitiveFieldKeys = map[string]struct{}{
	"cookie":        {},
	"cookies":       {},
	"token":         {},
	"auth":          {},
	"authorization": {},
	"password":      {},
	"secret":        {},
	"headers":       {},
	"apikey":        {},
	"api_key":       {},
}

// HostLogger writes structured, size-bounded JSON-lines logs for Native Messaging
// lifecycle events while redacting sensitive fields (cookies, tokens, headers).
type HostLogger struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
}

// DefaultLogPath returns %LOCALAPPDATA%\MagicDown\logs\native-host.log (or user config dir fallback).
func DefaultLogPath() string {
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, "MagicDown", "logs", "native-host.log")
	}
	if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
		return filepath.Join(cfgDir, "MagicDown", "logs", "native-host.log")
	}
	return filepath.Join(os.TempDir(), "magicdown-native-host.log")
}

// NewHostLogger creates a HostLogger writing to path (or DefaultLogPath if empty).
func NewHostLogger(path string, maxBytes int64) *HostLogger {
	if strings.TrimSpace(path) == "" {
		path = DefaultLogPath()
	}
	if maxBytes <= 0 {
		maxBytes = DefaultLogMaxBytes
	}
	return &HostLogger{
		path:     path,
		maxBytes: maxBytes,
	}
}

var defaultHostLogger = NewHostLogger("", DefaultLogMaxBytes)

// LogEvent writes a structured log entry using the default native-host logger.
func LogEvent(level, event string, fields map[string]any) {
	defaultHostLogger.Log(level, event, fields)
}

// RedactFields returns a shallow copy of fields with sensitive keys redacted.
func RedactFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		lk := strings.ToLower(strings.TrimSpace(k))
		if isSensitiveKey(lk) {
			out[k] = "[REDACTED]"
			continue
		}
		if s, ok := v.(string); ok {
			out[k] = sanitizeLogString(s)
		} else {
			out[k] = v
		}
	}
	return out
}

func isSensitiveKey(lowerKey string) bool {
	if _, ok := sensitiveFieldKeys[lowerKey]; ok {
		return true
	}
	for token := range sensitiveFieldKeys {
		if strings.Contains(lowerKey, token) {
			return true
		}
	}
	return false
}

func sanitizeLogString(s string) string {
	if len(s) > 512 {
		return s[:512] + "...(truncated)"
	}
	return s
}

// Log appends a single JSON-line event to the log file, rotating when maxBytes is exceeded.
func (l *HostLogger) Log(level, event string, fields map[string]any) {
	if l == nil || strings.TrimSpace(l.path) == "" {
		return
	}
	entry := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339),
		"level": strings.ToUpper(strings.TrimSpace(level)),
		"event": strings.TrimSpace(event),
	}
	if safe := RedactFields(fields); len(safe) > 0 {
		entry["fields"] = safe
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return
	}
	if info, err := os.Stat(l.path); err == nil && info.Size()+int64(len(line)) > l.maxBytes {
		backup := l.path + ".1"
		_ = os.Remove(backup)
		_ = os.Rename(l.path, backup)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(line)
}

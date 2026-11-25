package nativemessaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// HostName is the Chrome/Edge Native Messaging host identifier.
	HostName = "com.magicdown.downloader"
	// DefaultExtensionID is the unpacked/published MagicDown browser extension ID.
	DefaultExtensionID = "cndchhhjghchjigecgkjclembafgjaje"
)

// Manifest represents the Chrome/Edge Native Messaging host manifest JSON.
type Manifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

// BuildManifest creates a valid Native Messaging manifest for the given host binary path and extension ID.
func BuildManifest(hostBinaryPath, extensionID string) (Manifest, error) {
	bin := strings.TrimSpace(hostBinaryPath)
	if bin == "" {
		return Manifest{}, errors.New("host binary path is required")
	}
	extID := strings.TrimSpace(extensionID)
	if extID == "" {
		extID = DefaultExtensionID
	}
	return Manifest{
		Name:           HostName,
		Description:    "MagicDown Native Messaging Host",
		Path:           bin,
		Type:           "stdio",
		AllowedOrigins: []string{fmt.Sprintf("chrome-extension://%s/", extID)},
	}, nil
}

// DefaultManifestPath returns the standard per-user manifest path on Windows/other OSes.
func DefaultManifestPath() (string, error) {
	return ManifestPathForChannel("")
}

// ManifestPathForChannel returns the per-user Native Messaging manifest path for a browser
// store channel ("chrome" or "edge"). An empty channel returns the legacy shared path.
func ManifestPathForChannel(channel string) (string, error) {
	var baseDir string
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		baseDir = filepath.Join(localAppData, "MagicDown")
	} else {
		cfgDir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(cfgDir, "MagicDown")
	}
	ch := strings.ToLower(strings.TrimSpace(channel))
	if ch == "chrome" || ch == "edge" {
		return filepath.Join(baseDir, HostName+"."+ch+".json"), nil
	}
	return filepath.Join(baseDir, HostName+".json"), nil
}

// AllManagedManifestPaths returns all manifest file paths owned by MagicDown in baseDir.
// If baseDir is empty, it uses the default per-user MagicDown directory.
func AllManagedManifestPaths(baseDir string) ([]string, error) {
	if strings.TrimSpace(baseDir) == "" {
		legacy, err := DefaultManifestPath()
		if err != nil {
			return nil, err
		}
		baseDir = filepath.Dir(legacy)
	}
	return []string{
		filepath.Join(baseDir, HostName+".json"),
		filepath.Join(baseDir, HostName+".chrome.json"),
		filepath.Join(baseDir, HostName+".edge.json"),
	}, nil
}

// RemoveManagedManifests deletes ONLY MagicDown-owned Native Messaging manifest files
// inside baseDir, leaving all other files untouched.
func RemoveManagedManifests(baseDir string) ([]string, error) {
	paths, err := AllManagedManifestPaths(baseDir)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, p := range paths {
		if err := os.Remove(p); err == nil {
			removed = append(removed, p)
		} else if !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("remove manifest %q: %w", p, err)
		}
	}
	return removed, nil
}

// ValidateManifest validates a Native Messaging host manifest before writing Registry keys.
// It checks name, absolute existing executable path, type=stdio, and valid chrome-extension origins.
func ValidateManifest(m Manifest) error {
	if strings.TrimSpace(m.Name) != HostName {
		return fmt.Errorf("invalid manifest name %q (want %q)", m.Name, HostName)
	}
	if strings.TrimSpace(m.Type) != "stdio" {
		return fmt.Errorf("invalid manifest type %q (want stdio)", m.Type)
	}
	p := strings.TrimSpace(m.Path)
	if p == "" {
		return errors.New("manifest path is required")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("manifest path must be absolute: %q", p)
	}
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("host executable not found at %q: %w", p, err)
	}
	if info.IsDir() {
		return fmt.Errorf("host executable path is a directory: %q", p)
	}
	if len(m.AllowedOrigins) == 0 {
		return errors.New("manifest allowed_origins must not be empty")
	}
	for _, origin := range m.AllowedOrigins {
		if !strings.HasPrefix(origin, "chrome-extension://") || !strings.HasSuffix(origin, "/") {
			return fmt.Errorf("invalid allowed_origin format: %q", origin)
		}
		extID := strings.TrimSuffix(strings.TrimPrefix(origin, "chrome-extension://"), "/")
		if !IsValidExtensionID(extID) {
			return fmt.Errorf("invalid extension ID in allowed_origin: %q", origin)
		}
	}
	return nil
}

// ResolveHostBinary finds magicdown-host.exe next to the running magicdown executable,
// falling back to magicdown.exe itself if magicdown-host is not alongside it.
func ResolveHostBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	hostName := "magicdown-host"
	if runtime.GOOS == "windows" {
		hostName = "magicdown-host.exe"
	}
	candidate := filepath.Join(dir, hostName)
	if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
		return candidate, nil
	}
	return exe, nil
}

// WriteManifest writes the Native Messaging manifest JSON to manifestPath.
func WriteManifest(manifestPath string, m Manifest) error {
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, data, 0644)
}

// RegistrationStatus describes whether Native Messaging is registered for Chrome/Edge.
type RegistrationStatus struct {
	ManifestPath   string
	ManifestExists bool
	ChromeReady    bool
	EdgeReady      bool
}

// IsReady returns true if the manifest exists and at least one browser registry key points to it.
func (s RegistrationStatus) IsReady() bool {
	return s.ManifestExists && (s.ChromeReady || s.EdgeReady)
}

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
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, "MagicDown", HostName+".json"), nil
	}
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "MagicDown", HostName+".json"), nil
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

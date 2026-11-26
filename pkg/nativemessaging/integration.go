package nativemessaging

import (
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// IntegrationState represents the real, evidence-backed integration state of a browser.
type IntegrationState string

const (
	StateNotInstalled         IntegrationState = "not_installed"
	StateInstallationRequired IntegrationState = "installation_required"
	StateHostNotRegistered    IntegrationState = "host_not_registered"
	StateWaitingForConnection IntegrationState = "waiting_for_connection"
	StateConnected            IntegrationState = "connected"
	StateConnectionError      IntegrationState = "connection_error"
	StateUnsupportedBrowser   IntegrationState = "unsupported_browser"
)

// ExtensionConnectionState tracks real extension connectivity observed via IPC/handshake.
// Native Host registration alone must NEVER set Connected = true.
type ExtensionConnectionState struct {
	Connected        bool
	Waiting          bool
	LastError        string
	ExtensionVersion string
	LastHandshakeAt  time.Time
}

// BrowserIntegrationStatus combines browser discovery, host validation, and real extension status.
type BrowserIntegrationStatus struct {
	Browser      BrowserInfo
	State        IntegrationState
	HostValid    bool
	ManifestPath string
	StoreURL     string
	LogPath      string
	Connection   ExtensionConnectionState
}

// EvaluateBrowserIntegrationState computes the exact IntegrationState from real signals.
// It NEVER returns StateConnected solely because hostValid is true.
func EvaluateBrowserIntegrationState(b BrowserInfo, hostValid bool, conn ExtensionConnectionState) IntegrationState {
	if !b.Installed {
		return StateNotInstalled
	}
	if b.Family != BrowserFamilyChromium {
		return StateUnsupportedBrowser
	}
	if !hostValid {
		return StateHostNotRegistered
	}
	if conn.Connected {
		return StateConnected
	}
	if strings.TrimSpace(conn.LastError) != "" {
		return StateConnectionError
	}
	if conn.Waiting {
		return StateWaitingForConnection
	}
	return StateInstallationRequired
}

// ValidateManifestFile reads and validates a Native Messaging manifest JSON file on disk,
// including checking that its target host executable exists.
func ValidateManifestFile(manifestPath string) error {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	return ValidateManifest(m)
}

// BuildBrowserIntegrationStatuses enriches detected browsers with store URLs,
// real manifest/host validation, and real extension connection states.
func BuildBrowserIntegrationStatuses(
	browsers []BrowserInfo,
	cfg ExtensionIDConfig,
	connByBrowser map[string]ExtensionConnectionState,
) []BrowserIntegrationStatus {
	logPath := DefaultLogPath()
	out := make([]BrowserIntegrationStatus, 0, len(browsers))
	for _, b := range browsers {
		channel := StoreChannelForBrowser(b.ID)
		manifestPath, _ := ManifestPathForChannel(channel)
		hostValid := false
		if b.NativeRegistered {
			if manifestPath != "" && ValidateManifestFile(manifestPath) == nil {
				hostValid = true
			} else if legacyPath, err := DefaultManifestPath(); err == nil && ValidateManifestFile(legacyPath) == nil {
				manifestPath = legacyPath
				hostValid = true
			}
		}

		var conn ExtensionConnectionState
		if connByBrowser != nil {
			conn = connByBrowser[b.ID]
		}
		state := EvaluateBrowserIntegrationState(b, hostValid, conn)
		out = append(out, BrowserIntegrationStatus{
			Browser:      b,
			State:        state,
			HostValid:    hostValid,
			ManifestPath: manifestPath,
			StoreURL:     cfg.StoreURLForBrowser(b.ID),
			LogPath:      logPath,
			Connection:   conn,
		})
	}
	return out
}

// OpenStoreURL opens the browser's Store listing URL directly (no --load-extension / dev mode).
func OpenStoreURL(b BrowserInfo, storeURL string) error {
	u := strings.TrimSpace(storeURL)
	if u == "" {
		return os.ErrNotExist
	}
	if b.Installed && strings.TrimSpace(b.ExecutablePath) != "" {
		cmd := exec.Command(b.ExecutablePath, u)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	}
	return os.ErrInvalid
}

// OpenExtensionsPage opens the browser's native extensions management page
// (e.g. chrome://extensions, edge://extensions) so the user can Load unpacked.
func OpenExtensionsPage(b BrowserInfo) error {
	extURL := strings.TrimSpace(b.ExtensionsURL)
	if extURL == "" {
		if strings.EqualFold(strings.TrimSpace(b.ID), "edge") {
			extURL = "edge://extensions"
		} else {
			extURL = "chrome://extensions"
		}
	}
	if b.Installed && strings.TrimSpace(b.ExecutablePath) != "" {
		cmd := exec.Command(b.ExecutablePath, extURL)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", extURL).Start()
	}
	return os.ErrInvalid
}

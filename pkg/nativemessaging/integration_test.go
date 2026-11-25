package nativemessaging

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestEvaluateBrowserIntegrationStateAllSevenStates(t *testing.T) {
	chromium := BrowserInfo{ID: "chrome", Name: "Google Chrome", Installed: true, Family: BrowserFamilyChromium}
	firefox := BrowserInfo{ID: "firefox", Name: "Mozilla Firefox", Installed: true, Family: BrowserFamilyFirefox}
	notInstalled := BrowserInfo{ID: "edge", Name: "Microsoft Edge", Installed: false, Family: BrowserFamilyChromium}

	if got := EvaluateBrowserIntegrationState(notInstalled, false, ExtensionConnectionState{}); got != StateNotInstalled {
		t.Fatalf("not installed = %q, want %q", got, StateNotInstalled)
	}
	if got := EvaluateBrowserIntegrationState(firefox, false, ExtensionConnectionState{}); got != StateUnsupportedBrowser {
		t.Fatalf("unsupported = %q, want %q", got, StateUnsupportedBrowser)
	}
	if got := EvaluateBrowserIntegrationState(chromium, false, ExtensionConnectionState{}); got != StateHostNotRegistered {
		t.Fatalf("host missing = %q, want %q", got, StateHostNotRegistered)
	}
	// Host valid alone must NEVER report Connected
	if got := EvaluateBrowserIntegrationState(chromium, true, ExtensionConnectionState{}); got != StateInstallationRequired {
		t.Fatalf("host valid without extension = %q, want %q", got, StateInstallationRequired)
	}
	if got := EvaluateBrowserIntegrationState(chromium, true, ExtensionConnectionState{Waiting: true}); got != StateWaitingForConnection {
		t.Fatalf("waiting = %q, want %q", got, StateWaitingForConnection)
	}
	if got := EvaluateBrowserIntegrationState(chromium, true, ExtensionConnectionState{LastError: "handshake timeout"}); got != StateConnectionError {
		t.Fatalf("error = %q, want %q", got, StateConnectionError)
	}
	if got := EvaluateBrowserIntegrationState(chromium, true, ExtensionConnectionState{Connected: true, ExtensionVersion: "0.1.0", LastHandshakeAt: time.Now()}); got != StateConnected {
		t.Fatalf("connected = %q, want %q", got, StateConnected)
	}
}

func TestBuildBrowserIntegrationStatusesUsesConfigAndRealConnection(t *testing.T) {
	chromeProd := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	t.Setenv("MAGICDOWN_CHROME_PROD_EXT_ID", chromeProd)
	cfg, err := LoadExtensionIDConfig(filepath.Join("..", "..", "config", "extension-ids.json"))
	if err != nil {
		t.Fatalf("LoadExtensionIDConfig failed: %v", err)
	}

	browsers := []BrowserInfo{
		{ID: "chrome", Name: "Google Chrome", Installed: true, Family: BrowserFamilyChromium, NativeRegistered: false},
		{ID: "edge", Name: "Microsoft Edge", Installed: false, Family: BrowserFamilyChromium},
	}
	statuses := BuildBrowserIntegrationStatuses(browsers, cfg, nil)
	if len(statuses) != 2 {
		t.Fatalf("got %d statuses, want 2", len(statuses))
	}
	if statuses[0].State != StateHostNotRegistered {
		t.Fatalf("chrome state = %q, want %q", statuses[0].State, StateHostNotRegistered)
	}
	if statuses[0].StoreURL == "" {
		t.Fatalf("expected non-empty chrome StoreURL when prod ID is set")
	}
	if statuses[1].State != StateNotInstalled {
		t.Fatalf("edge state = %q, want %q", statuses[1].State, StateNotInstalled)
	}
}

func TestReadFileVersionOnWindowsSystemBinary(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only FileVersion test")
	}
	windir := os.Getenv("WINDIR")
	if windir == "" {
		t.Skip("WINDIR not set")
	}
	cmdExe := filepath.Join(windir, "System32", "cmd.exe")
	if _, err := os.Stat(cmdExe); err != nil {
		t.Skipf("cmd.exe not found: %v", err)
	}
	ver := ReadFileVersion(cmdExe)
	if ver == "" {
		t.Fatalf("expected non-empty FileVersion for %s", cmdExe)
	}
}

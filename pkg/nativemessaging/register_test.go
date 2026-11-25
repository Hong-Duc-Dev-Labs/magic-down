package nativemessaging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAndWriteManifest(t *testing.T) {
	hostBin := `C:\Users\alice\AppData\Local\Programs\MagicDown\magicdown-host.exe`
	m, err := BuildManifest(hostBin, "")
	if err != nil {
		t.Fatalf("BuildManifest failed: %v", err)
	}
	if m.Name != HostName {
		t.Fatalf("Name = %q, want %q", m.Name, HostName)
	}
	if m.Path != hostBin {
		t.Fatalf("Path = %q, want %q", m.Path, hostBin)
	}
	if m.Type != "stdio" {
		t.Fatalf("Type = %q, want stdio", m.Type)
	}
	wantOrigin := "chrome-extension://" + DefaultExtensionID + "/"
	if len(m.AllowedOrigins) != 1 || m.AllowedOrigins[0] != wantOrigin {
		t.Fatalf("AllowedOrigins = %v, want [%q]", m.AllowedOrigins, wantOrigin)
	}

	outPath := filepath.Join(t.TempDir(), "MagicDown", HostName+".json")
	if err := WriteManifest(outPath, m); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	var decoded Manifest
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded.Path != hostBin || decoded.Name != HostName {
		t.Fatalf("decoded manifest mismatch: %+v", decoded)
	}
}

func TestRegistrationStatusIsReady(t *testing.T) {
	if (RegistrationStatus{ManifestExists: false, ChromeReady: true}).IsReady() {
		t.Fatalf("expected not ready when manifest file is missing")
	}
	if (RegistrationStatus{ManifestExists: true, ChromeReady: false, EdgeReady: false}).IsReady() {
		t.Fatalf("expected not ready when no browser key is registered")
	}
	if !(RegistrationStatus{ManifestExists: true, ChromeReady: true}).IsReady() {
		t.Fatalf("expected ready when manifest exists and Chrome key is registered")
	}
	if !(RegistrationStatus{ManifestExists: true, EdgeReady: true}).IsReady() {
		t.Fatalf("expected ready when manifest exists and Edge key is registered")
	}
}

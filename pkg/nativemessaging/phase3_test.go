package nativemessaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createDummyExe(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "magicdown-host.exe")
	if err := os.WriteFile(p, []byte("MZ"), 0755); err != nil {
		t.Fatalf("failed to create dummy exe: %v", err)
	}
	return p
}

func TestValidateManifest(t *testing.T) {
	exe := createDummyExe(t)

	valid := Manifest{
		Name:           HostName,
		Description:    "MagicDown Native Messaging Host",
		Path:           exe,
		Type:           "stdio",
		AllowedOrigins: []string{"chrome-extension://" + DefaultExtensionID + "/"},
	}
	if err := ValidateManifest(valid); err != nil {
		t.Fatalf("ValidateManifest(valid) failed: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(m *Manifest)
	}{
		{"wrong name", func(m *Manifest) { m.Name = "com.other.host" }},
		{"wrong type", func(m *Manifest) { m.Type = "tcp" }},
		{"empty path", func(m *Manifest) { m.Path = "" }},
		{"relative path", func(m *Manifest) { m.Path = "magicdown-host.exe" }},
		{"missing exe", func(m *Manifest) { m.Path = filepath.Join(t.TempDir(), "missing.exe") }},
		{"directory path", func(m *Manifest) { m.Path = t.TempDir() }},
		{"empty origins", func(m *Manifest) { m.AllowedOrigins = nil }},
		{"bad origin scheme", func(m *Manifest) { m.AllowedOrigins = []string{"https://example.com/"} }},
		{"bad origin ext id", func(m *Manifest) { m.AllowedOrigins = []string{"chrome-extension://notvalid/"} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := valid
			tc.mutate(&m)
			if err := ValidateManifest(m); err == nil {
				t.Fatalf("ValidateManifest(%s) succeeded, want error", tc.name)
			}
		})
	}
}

func TestIndependentChromeAndEdgeManifests(t *testing.T) {
	exe := createDummyExe(t)
	chromeProd := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	edgeProd := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	t.Setenv("MAGICDOWN_CHROME_PROD_EXT_ID", chromeProd)
	t.Setenv("MAGICDOWN_EDGE_PROD_EXT_ID", edgeProd)

	cfg, err := LoadExtensionIDConfig(filepath.Join("..", "..", "config", "extension-ids.json"))
	if err != nil {
		t.Fatalf("LoadExtensionIDConfig failed: %v", err)
	}

	chromeManifest, err := BuildManifestForBrowser(exe, "chrome", BuildModeProd, cfg)
	if err != nil {
		t.Fatalf("BuildManifestForBrowser(chrome) failed: %v", err)
	}
	if err := ValidateManifest(chromeManifest); err != nil {
		t.Fatalf("ValidateManifest(chrome) failed: %v", err)
	}

	edgeManifest, err := BuildManifestForBrowser(exe, "edge", BuildModeProd, cfg)
	if err != nil {
		t.Fatalf("BuildManifestForBrowser(edge) failed: %v", err)
	}
	if err := ValidateManifest(edgeManifest); err != nil {
		t.Fatalf("ValidateManifest(edge) failed: %v", err)
	}

	if chromeManifest.AllowedOrigins[0] == edgeManifest.AllowedOrigins[0] {
		t.Fatalf("expected independent Chrome and Edge origins, both were %q", chromeManifest.AllowedOrigins[0])
	}
}

func TestRemoveManagedManifestsLeavesUserFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	managed, err := AllManagedManifestPaths(dir)
	if err != nil {
		t.Fatalf("AllManagedManifestPaths failed: %v", err)
	}
	for _, p := range managed {
		if err := os.WriteFile(p, []byte(`{}`), 0644); err != nil {
			t.Fatalf("write %q: %v", p, err)
		}
	}
	unrelated := filepath.Join(dir, "downloads.json")
	if err := os.WriteFile(unrelated, []byte(`{"tasks":[]}`), 0644); err != nil {
		t.Fatalf("write unrelated file: %v", err)
	}

	removed, err := RemoveManagedManifests(dir)
	if err != nil {
		t.Fatalf("RemoveManagedManifests failed: %v", err)
	}
	if len(removed) != len(managed) {
		t.Fatalf("removed %d files, want %d", len(removed), len(managed))
	}
	for _, p := range managed {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected managed manifest %q to be deleted", p)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated user file %q must not be deleted: %v", unrelated, err)
	}
}

func TestHostLoggerRedactionAndRotation(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "logs", "native-host.log")
	logger := NewHostLogger(logPath, 256)

	logger.Log("INFO", "host_startup", map[string]any{
		"ipc_port":      28888,
		"cookies":       "session=super-secret-cookie",
		"Authorization": "Bearer secret-jwt",
		"api_token":     "tok_12345",
		"headers":       map[string]string{"Cookie": "a=b"},
	})

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	content := string(raw)
	for _, secret := range []string{"super-secret-cookie", "secret-jwt", "tok_12345"} {
		if strings.Contains(content, secret) {
			t.Fatalf("log leaked sensitive value %q: %s", secret, content)
		}
	}
	if !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker in log: %s", content)
	}

	// Trigger rotation by writing beyond 256 bytes
	for i := 0; i < 6; i++ {
		logger.Log("INFO", "comm_error", map[string]any{"stage": "read", "detail": strings.Repeat("x", 60)})
	}
	if _, err := os.Stat(logPath + ".1"); err != nil {
		t.Fatalf("expected rotated backup log %q to exist: %v", logPath+".1", err)
	}
}

package nativemessaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsValidExtensionID(t *testing.T) {
	valid := []string{
		DefaultExtensionID,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"pppppppppppppppppppppppppppppppp",
	}
	for _, id := range valid {
		if !IsValidExtensionID(id) {
			t.Errorf("IsValidExtensionID(%q) = false, want true", id)
		}
	}

	invalid := []string{
		"",
		"short",
		"cndchhhjghchjigecgkjclembafgjajq", // contains 'q'
		"CNDCHHHJGHCHJIGECGKJCLEMBAFGJAJE", // uppercase
		"cndchhhjghchjigecgkjclembafgjaje1",
	}
	for _, id := range invalid {
		if IsValidExtensionID(id) {
			t.Errorf("IsValidExtensionID(%q) = true, want false", id)
		}
	}
}

func TestLoadExtensionIDConfigFromRepoFile(t *testing.T) {
	repoCfgPath := filepath.Join("..", "..", "config", "extension-ids.json")
	cfg, err := LoadExtensionIDConfig(repoCfgPath)
	if err != nil {
		t.Fatalf("LoadExtensionIDConfig(%q) failed: %v", repoCfgPath, err)
	}
	if cfg.HostName != HostName {
		t.Fatalf("HostName = %q, want %q", cfg.HostName, HostName)
	}
	if cfg.Browsers["chrome"].Dev != DefaultExtensionID {
		t.Fatalf("chrome.dev = %q, want %q", cfg.Browsers["chrome"].Dev, DefaultExtensionID)
	}
	if cfg.Browsers["edge"].Dev != DefaultExtensionID {
		t.Fatalf("edge.dev = %q, want %q", cfg.Browsers["edge"].Dev, DefaultExtensionID)
	}
	if cfg.Browsers["chrome"].Prod != "" || cfg.Browsers["edge"].Prod != "" {
		t.Fatalf("prod IDs must remain empty placeholders until Store submission, got chrome=%q edge=%q",
			cfg.Browsers["chrome"].Prod, cfg.Browsers["edge"].Prod)
	}
	if !cfg.Packaging.StripKeyInProd {
		t.Fatalf("StripKeyInProd = false, want true")
	}
}

func TestAllowedOriginsPerBrowserAndBuildMode(t *testing.T) {
	chromeProd := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	edgeProd := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	t.Setenv("MAGICDOWN_CHROME_PROD_EXT_ID", chromeProd)
	t.Setenv("MAGICDOWN_EDGE_PROD_EXT_ID", edgeProd)

	repoCfgPath := filepath.Join("..", "..", "config", "extension-ids.json")
	cfg, err := LoadExtensionIDConfig(repoCfgPath)
	if err != nil {
		t.Fatalf("LoadExtensionIDConfig failed: %v", err)
	}

	// Dev mode returns dev ID for both Chrome and Edge
	chromeDevOrigins, err := cfg.AllowedOriginsForBrowser("chrome", BuildModeDev)
	if err != nil {
		t.Fatalf("chrome dev origins failed: %v", err)
	}
	if len(chromeDevOrigins) != 1 || chromeDevOrigins[0] != "chrome-extension://"+DefaultExtensionID+"/" {
		t.Fatalf("chrome dev origins = %v", chromeDevOrigins)
	}

	// Prod mode returns independent prod IDs for Chrome vs Edge
	chromeProdOrigins, err := cfg.AllowedOriginsForBrowser("chrome", BuildModeProd)
	if err != nil {
		t.Fatalf("chrome prod origins failed: %v", err)
	}
	if len(chromeProdOrigins) != 1 || chromeProdOrigins[0] != "chrome-extension://"+chromeProd+"/" {
		t.Fatalf("chrome prod origins = %v, want [%s]", chromeProdOrigins, chromeProd)
	}

	edgeProdOrigins, err := cfg.AllowedOriginsForBrowser("edge", BuildModeProd)
	if err != nil {
		t.Fatalf("edge prod origins failed: %v", err)
	}
	if len(edgeProdOrigins) != 1 || edgeProdOrigins[0] != "chrome-extension://"+edgeProd+"/" {
		t.Fatalf("edge prod origins = %v, want [%s]", edgeProdOrigins, edgeProd)
	}

	// Other Chromium browsers (e.g. brave) map to chrome store channel
	braveProdOrigins, err := cfg.AllowedOriginsForBrowser("brave", BuildModeProd)
	if err != nil {
		t.Fatalf("brave prod origins failed: %v", err)
	}
	if len(braveProdOrigins) != 1 || braveProdOrigins[0] != "chrome-extension://"+chromeProd+"/" {
		t.Fatalf("brave prod origins = %v, want chrome prod origin", braveProdOrigins)
	}

	// Store URL resolution expands template when prod ID is present
	if got := cfg.StoreURLForBrowser("chrome"); !strings.Contains(got, chromeProd) {
		t.Fatalf("chrome StoreURL = %q, expected to contain %q", got, chromeProd)
	}
	if got := cfg.StoreURLForBrowser("edge"); !strings.Contains(got, edgeProd) {
		t.Fatalf("edge StoreURL = %q, expected to contain %q", got, edgeProd)
	}
}

func TestProdModeFailsWhenProdIDNotConfigured(t *testing.T) {
	t.Setenv("MAGICDOWN_CHROME_PROD_EXT_ID", "")
	t.Setenv("MAGICDOWN_EDGE_PROD_EXT_ID", "")

	cfg := DefaultExtensionIDConfig()
	if _, err := cfg.AllowedOriginsForBrowser("chrome", BuildModeProd); err == nil {
		t.Fatalf("expected error when chrome prod extension ID is empty")
	}
	if _, err := cfg.AllowedOriginsForBrowser("edge", BuildModeProd); err == nil {
		t.Fatalf("expected error when edge prod extension ID is empty")
	}
	if got := cfg.StoreURLForBrowser("chrome"); got != "" {
		t.Fatalf("expected empty store URL when prod ID is empty, got %q", got)
	}
}

func TestBuildManifestWithOriginsDeduplicatesAndValidates(t *testing.T) {
	idA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	idB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	m, err := BuildManifestWithOrigins(`C:\MagicDown\magicdown-host.exe`, []string{idA, idB, idA})
	if err != nil {
		t.Fatalf("BuildManifestWithOrigins failed: %v", err)
	}
	if len(m.AllowedOrigins) != 2 {
		t.Fatalf("AllowedOrigins len = %d, want 2 (%v)", len(m.AllowedOrigins), m.AllowedOrigins)
	}
	if m.AllowedOrigins[0] != "chrome-extension://"+idA+"/" || m.AllowedOrigins[1] != "chrome-extension://"+idB+"/" {
		t.Fatalf("unexpected AllowedOrigins: %v", m.AllowedOrigins)
	}

	if _, err := BuildManifestWithOrigins(`C:\MagicDown\magicdown-host.exe`, []string{"invalid-id"}); err == nil {
		t.Fatalf("expected error for invalid extension ID")
	}
}

func TestLoadExtensionIDConfigExplicitMissingFileErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	if _, err := os.Stat(missing); err == nil {
		t.Fatalf("expected temp file to not exist")
	}
	if _, err := LoadExtensionIDConfig(missing); err == nil {
		t.Fatalf("expected error for explicit missing config path")
	}
}

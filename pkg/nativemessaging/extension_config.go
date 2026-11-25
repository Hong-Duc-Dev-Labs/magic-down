package nativemessaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	BuildModeDev  = "dev"
	BuildModeProd = "prod"
)

// Build-time variables optionally injected via -ldflags for production builds:
//
//	-X magicdown/pkg/nativemessaging.BuildMode=prod
//	-X magicdown/pkg/nativemessaging.ChromeProdExtensionID=<32-char-id>
//	-X magicdown/pkg/nativemessaging.EdgeProdExtensionID=<32-char-id>
//	-X magicdown/pkg/nativemessaging.ChromeStoreURL=<url>
//	-X magicdown/pkg/nativemessaging.EdgeStoreURL=<url>
var (
	BuildMode             = BuildModeDev
	ChromeProdExtensionID = ""
	EdgeProdExtensionID   = ""
	ChromeStoreURL        = ""
	EdgeStoreURL          = ""
)

// BrowserExtensionConfig holds dev/prod Extension IDs and Store URLs for a browser store.
type BrowserExtensionConfig struct {
	Dev              string `json:"dev"`
	Prod             string `json:"prod"`
	StoreURL         string `json:"store_url"`
	StoreURLTemplate string `json:"store_url_template"`
}

// ExtensionPackagingConfig documents store packaging rules.
type ExtensionPackagingConfig struct {
	StripKeyInProd bool   `json:"strip_key_in_prod"`
	Notes          string `json:"notes,omitempty"`
}

// ExtensionIDConfig is the centralized Extension ID & Store URL configuration
// loaded from config/extension-ids.json with build-time and environment overrides.
type ExtensionIDConfig struct {
	SchemaVersion int                               `json:"schema_version"`
	HostName      string                            `json:"host_name"`
	Browsers      map[string]BrowserExtensionConfig `json:"browsers"`
	Packaging     ExtensionPackagingConfig          `json:"packaging"`
}

// IsValidExtensionID returns true if id is a valid 32-character Chromium/Edge
// extension ID (lowercase letters 'a' through 'p').
func IsValidExtensionID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 'a' || id[i] > 'p' {
			return false
		}
	}
	return true
}

// DefaultExtensionIDConfig returns the baseline config using DefaultExtensionID
// for dev and any build-time injected values for prod.
func DefaultExtensionIDConfig() ExtensionIDConfig {
	return ExtensionIDConfig{
		SchemaVersion: 1,
		HostName:      HostName,
		Browsers: map[string]BrowserExtensionConfig{
			"chrome": {
				Dev:              DefaultExtensionID,
				Prod:             strings.TrimSpace(ChromeProdExtensionID),
				StoreURL:         strings.TrimSpace(ChromeStoreURL),
				StoreURLTemplate: "https://chromewebstore.google.com/detail/magicdown-integration/{id}",
			},
			"edge": {
				Dev:              DefaultExtensionID,
				Prod:             strings.TrimSpace(EdgeProdExtensionID),
				StoreURL:         strings.TrimSpace(EdgeStoreURL),
				StoreURLTemplate: "https://microsoftedge.microsoft.com/addons/detail/magicdown-integration/{id}",
			},
		},
		Packaging: ExtensionPackagingConfig{
			StripKeyInProd: true,
		},
	}
}

// ResolveExtensionIDConfigPath locates config/extension-ids.json from env, executable dir, or cwd.
func ResolveExtensionIDConfigPath() string {
	if envPath := strings.TrimSpace(os.Getenv("MAGICDOWN_EXTENSION_IDS_PATH")); envPath != "" {
		return envPath
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "config", "extension-ids.json"),
			filepath.Join(filepath.Dir(exeDir), "config", "extension-ids.json"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "config", "extension-ids.json"))
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// LoadExtensionIDConfig loads ExtensionIDConfig from configPath (if non-empty or auto-discovered),
// falling back to DefaultExtensionIDConfig, and applies environment variable overrides.
func LoadExtensionIDConfig(configPath string) (ExtensionIDConfig, error) {
	cfg := DefaultExtensionIDConfig()

	path := strings.TrimSpace(configPath)
	if path == "" {
		path = ResolveExtensionIDConfigPath()
	}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			// If an explicit path was provided, return the error; otherwise fall back to defaults.
			if strings.TrimSpace(configPath) != "" {
				return ExtensionIDConfig{}, fmt.Errorf("read extension-ids config %q: %w", path, err)
			}
		} else {
			var fileCfg ExtensionIDConfig
			if err := json.Unmarshal(raw, &fileCfg); err != nil {
				return ExtensionIDConfig{}, fmt.Errorf("parse extension-ids config %q: %w", path, err)
			}
			if fileCfg.HostName != "" {
				cfg.HostName = fileCfg.HostName
			}
			if fileCfg.SchemaVersion > 0 {
				cfg.SchemaVersion = fileCfg.SchemaVersion
			}
			cfg.Packaging = fileCfg.Packaging
			for k, v := range fileCfg.Browsers {
				key := strings.ToLower(strings.TrimSpace(k))
				base := cfg.Browsers[key]
				if strings.TrimSpace(v.Dev) != "" {
					base.Dev = strings.TrimSpace(v.Dev)
				}
				if strings.TrimSpace(v.Prod) != "" {
					base.Prod = strings.TrimSpace(v.Prod)
				}
				if strings.TrimSpace(v.StoreURL) != "" {
					base.StoreURL = strings.TrimSpace(v.StoreURL)
				}
				if strings.TrimSpace(v.StoreURLTemplate) != "" {
					base.StoreURLTemplate = strings.TrimSpace(v.StoreURLTemplate)
				}
				cfg.Browsers[key] = base
			}
		}
	}

	applyExtensionIDEnvOverrides(&cfg)
	return cfg, nil
}

func applyExtensionIDEnvOverrides(cfg *ExtensionIDConfig) {
	if cfg.Browsers == nil {
		cfg.Browsers = make(map[string]BrowserExtensionConfig)
	}
	override := func(browserKey, devEnv, prodEnv, urlEnv string) {
		b := cfg.Browsers[browserKey]
		if v := strings.TrimSpace(os.Getenv(devEnv)); v != "" {
			b.Dev = v
		}
		if v := strings.TrimSpace(os.Getenv(prodEnv)); v != "" {
			b.Prod = v
		}
		if v := strings.TrimSpace(os.Getenv(urlEnv)); v != "" {
			b.StoreURL = v
		}
		cfg.Browsers[browserKey] = b
	}
	override("chrome", "MAGICDOWN_CHROME_DEV_EXT_ID", "MAGICDOWN_CHROME_PROD_EXT_ID", "MAGICDOWN_CHROME_STORE_URL")
	override("edge", "MAGICDOWN_EDGE_DEV_EXT_ID", "MAGICDOWN_EDGE_PROD_EXT_ID", "MAGICDOWN_EDGE_STORE_URL")
}

// EffectiveBuildMode returns the active build mode ("dev" or "prod"),
// honoring MAGICDOWN_BUILD_MODE when set.
func EffectiveBuildMode() string {
	if envMode := strings.ToLower(strings.TrimSpace(os.Getenv("MAGICDOWN_BUILD_MODE"))); envMode == BuildModeProd || envMode == BuildModeDev {
		return envMode
	}
	if strings.EqualFold(strings.TrimSpace(BuildMode), BuildModeProd) {
		return BuildModeProd
	}
	return BuildModeDev
}

// StoreChannelForBrowser maps a browser ID to its extension store channel ("edge" or "chrome").
func StoreChannelForBrowser(browserID string) string {
	if strings.EqualFold(strings.TrimSpace(browserID), "edge") {
		return "edge"
	}
	return "chrome"
}

// AllowedExtensionIDs returns the deduplicated list of valid Extension IDs for a given
// browserID ("chrome", "edge", or other Chromium browser) and build mode ("dev", "prod", or "all").
func (c ExtensionIDConfig) AllowedExtensionIDs(browserID, mode string) ([]string, error) {
	channel := StoreChannelForBrowser(browserID)
	bCfg, ok := c.Browsers[channel]
	if !ok {
		return nil, fmt.Errorf("no extension ID config for store channel %q", channel)
	}

	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		m = EffectiveBuildMode()
	}

	var rawIDs []string
	switch m {
	case BuildModeProd:
		if strings.TrimSpace(bCfg.Prod) == "" {
			return nil, fmt.Errorf("production extension ID for %s is not configured", channel)
		}
		rawIDs = append(rawIDs, bCfg.Prod)
	case BuildModeDev:
		if strings.TrimSpace(bCfg.Dev) == "" {
			return nil, fmt.Errorf("development extension ID for %s is not configured", channel)
		}
		rawIDs = append(rawIDs, bCfg.Dev)
	case "all":
		if strings.TrimSpace(bCfg.Prod) != "" {
			rawIDs = append(rawIDs, bCfg.Prod)
		}
		if strings.TrimSpace(bCfg.Dev) != "" {
			rawIDs = append(rawIDs, bCfg.Dev)
		}
		if len(rawIDs) == 0 {
			return nil, fmt.Errorf("no extension IDs configured for %s", channel)
		}
	default:
		return nil, fmt.Errorf("unsupported build mode %q (expected dev, prod, or all)", mode)
	}

	seen := make(map[string]struct{}, len(rawIDs))
	out := make([]string, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id := strings.ToLower(strings.TrimSpace(raw))
		if !IsValidExtensionID(id) {
			return nil, fmt.Errorf("invalid extension ID %q for %s", raw, channel)
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// AllowedOriginsForBrowser returns the chrome-extension://<id>/ origins for browserID and mode.
func (c ExtensionIDConfig) AllowedOriginsForBrowser(browserID, mode string) ([]string, error) {
	ids, err := c.AllowedExtensionIDs(browserID, mode)
	if err != nil {
		return nil, err
	}
	origins := make([]string, 0, len(ids))
	for _, id := range ids {
		origins = append(origins, fmt.Sprintf("chrome-extension://%s/", id))
	}
	return origins, nil
}

// StoreURLForBrowser resolves the store listing URL for browserID.
// Returns empty string when neither store_url nor a valid prod ID is configured.
func (c ExtensionIDConfig) StoreURLForBrowser(browserID string) string {
	channel := StoreChannelForBrowser(browserID)
	bCfg, ok := c.Browsers[channel]
	if !ok {
		return ""
	}
	if u := strings.TrimSpace(bCfg.StoreURL); u != "" {
		return u
	}
	prodID := strings.ToLower(strings.TrimSpace(bCfg.Prod))
	if IsValidExtensionID(prodID) && strings.Contains(bCfg.StoreURLTemplate, "{id}") {
		return strings.ReplaceAll(bCfg.StoreURLTemplate, "{id}", prodID)
	}
	return ""
}

// BuildManifestWithOrigins creates a Native Messaging manifest allowing multiple extension IDs.
func BuildManifestWithOrigins(hostBinaryPath string, extensionIDs []string) (Manifest, error) {
	bin := strings.TrimSpace(hostBinaryPath)
	if bin == "" {
		return Manifest{}, errors.New("host binary path is required")
	}
	if len(extensionIDs) == 0 {
		return Manifest{}, errors.New("at least one extension ID is required")
	}
	seen := make(map[string]struct{}, len(extensionIDs))
	origins := make([]string, 0, len(extensionIDs))
	for _, raw := range extensionIDs {
		id := strings.ToLower(strings.TrimSpace(raw))
		if !IsValidExtensionID(id) {
			return Manifest{}, fmt.Errorf("invalid extension ID %q", raw)
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		origins = append(origins, fmt.Sprintf("chrome-extension://%s/", id))
	}
	return Manifest{
		Name:           HostName,
		Description:    "MagicDown Native Messaging Host",
		Path:           bin,
		Type:           "stdio",
		AllowedOrigins: origins,
	}, nil
}

// BuildManifestForBrowser creates a Native Messaging manifest for a specific browser and build mode.
func BuildManifestForBrowser(hostBinaryPath, browserID, mode string, cfg ExtensionIDConfig) (Manifest, error) {
	ids, err := cfg.AllowedExtensionIDs(browserID, mode)
	if err != nil {
		return Manifest{}, err
	}
	return BuildManifestWithOrigins(hostBinaryPath, ids)
}

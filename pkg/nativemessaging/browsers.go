package nativemessaging

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	BrowserFamilyChromium = "chromium"
	BrowserFamilyFirefox  = "firefox"
	BrowserFamilyOther    = "other"

	defaultChromeNativeHostKey  = `Software\Google\Chrome\NativeMessagingHosts\` + HostName
	defaultFirefoxNativeHostKey = `Software\Mozilla\NativeMessagingHosts\` + HostName
)

// BrowserInfo describes a supported or dynamically discovered browser on Windows and its integration state.
type BrowserInfo struct {
	ID                       string
	Name                     string
	ExeName                  string
	ExecutablePath           string
	Installed                bool
	Family                   string
	NativeMessagingKey       string
	ExtraNativeMessagingKeys []string
	ExternalExtKey           string
	ExtensionsURL            string
	NativeRegistered         bool
}

// AllNativeMessagingKeys returns the deduplicated list of Registry keys to register
// for this browser (primary key first, followed by any browser-specific or fallback keys).
func (b BrowserInfo) AllNativeMessagingKeys() []string {
	seen := make(map[string]struct{}, 1+len(b.ExtraNativeMessagingKeys))
	out := make([]string, 0, 1+len(b.ExtraNativeMessagingKeys))
	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" {
			return
		}
		lower := strings.ToLower(k)
		if _, ok := seen[lower]; ok {
			return
		}
		seen[lower] = struct{}{}
		out = append(out, k)
	}
	add(b.NativeMessagingKey)
	for _, k := range b.ExtraNativeMessagingKeys {
		add(k)
	}
	return out
}

// BrowserInstallResult reports the outcome of integrating MagicDown with a single browser.
type BrowserInstallResult struct {
	BrowserID          string
	BrowserName        string
	NativeHostOK       bool
	OpenedExtensionsUI bool
	ExtensionDir       string
	StatusText         string
	Err                error
}

// StartMenuBrowserEntry represents a browser discovered from Windows Registry
// (HKLM/HKCU\SOFTWARE\Clients\StartMenuInternet) or MSIX/WindowsApps aliases.
type StartMenuBrowserEntry struct {
	Key         string
	DisplayName string
	Command     string
}

// SupportedBrowsers returns the built-in catalog of well-known browsers MagicDown supports.
// DetectInstalledBrowsers augments this list with any additional browsers registered on Windows.
func SupportedBrowsers() []BrowserInfo {
	return []BrowserInfo{
		{
			ID:                 "chrome",
			Name:               "Google Chrome",
			ExeName:            "chrome.exe",
			Family:             BrowserFamilyChromium,
			NativeMessagingKey: defaultChromeNativeHostKey,
			ExternalExtKey:     `Software\Google\Chrome\Extensions\` + DefaultExtensionID,
			ExtensionsURL:      "chrome://extensions",
		},
		{
			ID:                 "edge",
			Name:               "Microsoft Edge",
			ExeName:            "msedge.exe",
			Family:             BrowserFamilyChromium,
			NativeMessagingKey: `Software\Microsoft\Edge\NativeMessagingHosts\` + HostName,
			ExternalExtKey:     `Software\Microsoft\Edge\Extensions\` + DefaultExtensionID,
			ExtensionsURL:      "edge://extensions",
		},
		{
			ID:                       "dia",
			Name:                     "Dia",
			ExeName:                  "Dia.exe",
			Family:                   BrowserFamilyChromium,
			NativeMessagingKey:       defaultChromeNativeHostKey,
			ExtraNativeMessagingKeys: []string{`Software\TheBrowserCompany\Dia\NativeMessagingHosts\` + HostName},
			ExtensionsURL:            "chrome://extensions",
		},
		{
			ID:                       "arc",
			Name:                     "Arc",
			ExeName:                  "Arc.exe",
			Family:                   BrowserFamilyChromium,
			NativeMessagingKey:       defaultChromeNativeHostKey,
			ExtraNativeMessagingKeys: []string{`Software\ArcBrowser\Arc\NativeMessagingHosts\` + HostName},
			ExtensionsURL:            "chrome://extensions",
		},
		{
			ID:                       "brave",
			Name:                     "Brave",
			ExeName:                  "brave.exe",
			Family:                   BrowserFamilyChromium,
			NativeMessagingKey:       `Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\` + HostName,
			ExtraNativeMessagingKeys: []string{defaultChromeNativeHostKey},
			ExternalExtKey:           `Software\BraveSoftware\Brave-Browser\Extensions\` + DefaultExtensionID,
			ExtensionsURL:            "brave://extensions",
		},
		{
			ID:                       "vivaldi",
			Name:                     "Vivaldi",
			ExeName:                  "vivaldi.exe",
			Family:                   BrowserFamilyChromium,
			NativeMessagingKey:       `Software\Vivaldi\NativeMessagingHosts\` + HostName,
			ExtraNativeMessagingKeys: []string{defaultChromeNativeHostKey},
			ExternalExtKey:           `Software\Vivaldi\Extensions\` + DefaultExtensionID,
			ExtensionsURL:            "vivaldi://extensions",
		},
		{
			ID:                       "opera",
			Name:                     "Opera",
			ExeName:                  "opera.exe",
			Family:                   BrowserFamilyChromium,
			NativeMessagingKey:       `Software\Opera Software\Opera Stable\NativeMessagingHosts\` + HostName,
			ExtraNativeMessagingKeys: []string{defaultChromeNativeHostKey},
			ExternalExtKey:           `Software\Opera Software\Opera Stable\Extensions\` + DefaultExtensionID,
			ExtensionsURL:            "opera://extensions",
		},
		{
			ID:      "operagx",
			Name:    "Opera GX",
			ExeName: "opera.exe",
			Family:  BrowserFamilyChromium,
			NativeMessagingKey: `Software\Opera Software\Opera GX Stable\NativeMessagingHosts\` + HostName,
			ExtraNativeMessagingKeys: []string{
				`Software\Opera Software\Opera Stable\NativeMessagingHosts\` + HostName,
				defaultChromeNativeHostKey,
			},
			ExtensionsURL: "opera://extensions",
		},
		{
			ID:                       "coccoc",
			Name:                     "Cốc Cốc",
			ExeName:                  "browser.exe",
			Family:                   BrowserFamilyChromium,
			NativeMessagingKey:       `Software\CocCoc\Browser\NativeMessagingHosts\` + HostName,
			ExtraNativeMessagingKeys: []string{defaultChromeNativeHostKey},
			ExternalExtKey:           `Software\CocCoc\Browser\Extensions\` + DefaultExtensionID,
			ExtensionsURL:            "coccoc://extensions",
		},
		{
			ID:                 "comet",
			Name:               "Comet",
			ExeName:            "comet.exe",
			Family:             BrowserFamilyChromium,
			NativeMessagingKey: defaultChromeNativeHostKey,
			ExtensionsURL:      "chrome://extensions",
		},
		{
			ID:                       "thorium",
			Name:                     "Thorium",
			ExeName:                  "thorium.exe",
			Family:                   BrowserFamilyChromium,
			NativeMessagingKey:       defaultChromeNativeHostKey,
			ExtraNativeMessagingKeys: []string{`Software\Chromium\NativeMessagingHosts\` + HostName},
			ExtensionsURL:            "chrome://extensions",
		},
		{
			ID:                 "firefox",
			Name:               "Mozilla Firefox",
			ExeName:            "firefox.exe",
			Family:             BrowserFamilyFirefox,
			NativeMessagingKey: defaultFirefoxNativeHostKey,
			ExtensionsURL:      "about:addons",
		},
		{
			ID:                 "zen",
			Name:               "Zen Browser",
			ExeName:            "zen.exe",
			Family:             BrowserFamilyFirefox,
			NativeMessagingKey: defaultFirefoxNativeHostKey,
			ExtensionsURL:      "about:addons",
		},
	}
}

// ResolveExtensionDir locates the unpacked MagicDown browser extension folder
// (containing manifest.json) next to the running executable or current working directory.
func ResolveExtensionDir() string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "extension"),
			filepath.Join(filepath.Dir(exeDir), "extension"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "extension"))
	}
	for _, dir := range candidates {
		if info, err := os.Stat(filepath.Join(dir, "manifest.json")); err == nil && !info.IsDir() {
			return dir
		}
	}
	return ""
}

// CandidateBrowserPaths returns well-known Windows installation paths for a browser ID
// given standard environment directories (ProgramFiles, ProgramFiles(x86), LocalAppData).
func CandidateBrowserPaths(browserID, programFiles, programFilesX86, localAppData string) []string {
	var out []string
	add := func(base string, rel ...string) {
		if strings.TrimSpace(base) == "" {
			return
		}
		parts := append([]string{base}, rel...)
		out = append(out, filepath.Join(parts...))
	}
	switch browserID {
	case "chrome":
		add(programFiles, "Google", "Chrome", "Application", "chrome.exe")
		add(programFilesX86, "Google", "Chrome", "Application", "chrome.exe")
		add(localAppData, "Google", "Chrome", "Application", "chrome.exe")
	case "edge":
		add(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe")
		add(programFiles, "Microsoft", "Edge", "Application", "msedge.exe")
		add(localAppData, "Microsoft", "Edge", "Application", "msedge.exe")
	case "dia":
		add(localAppData, "Microsoft", "WindowsApps", "Dia.exe")
		add(localAppData, "Programs", "Dia", "Dia.exe")
	case "arc":
		add(localAppData, "Microsoft", "WindowsApps", "Arc.exe")
		add(localAppData, "Programs", "Arc", "Arc.exe")
	case "brave":
		add(programFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe")
		add(programFilesX86, "BraveSoftware", "Brave-Browser", "Application", "brave.exe")
		add(localAppData, "BraveSoftware", "Brave-Browser", "Application", "brave.exe")
	case "vivaldi":
		add(localAppData, "Vivaldi", "Application", "vivaldi.exe")
		add(programFiles, "Vivaldi", "Application", "vivaldi.exe")
		add(programFilesX86, "Vivaldi", "Application", "vivaldi.exe")
	case "opera":
		add(localAppData, "Programs", "Opera", "opera.exe")
		add(localAppData, "Programs", "Opera", "launcher.exe")
		add(programFiles, "Opera", "opera.exe")
	case "operagx":
		add(localAppData, "Programs", "Opera GX", "opera.exe")
		add(localAppData, "Programs", "Opera GX", "launcher.exe")
		add(programFiles, "Opera GX", "opera.exe")
	case "coccoc":
		add(programFiles, "CocCoc", "Browser", "Application", "browser.exe")
		add(programFilesX86, "CocCoc", "Browser", "Application", "browser.exe")
		add(localAppData, "CocCoc", "Browser", "Application", "browser.exe")
	case "comet":
		add(localAppData, "Perplexity", "Comet", "Application", "comet.exe")
		add(programFiles, "Perplexity", "Comet", "Application", "comet.exe")
	case "thorium":
		add(localAppData, "Thorium", "Application", "thorium.exe")
		add(programFiles, "Thorium", "Application", "thorium.exe")
	case "firefox":
		add(programFiles, "Mozilla Firefox", "firefox.exe")
		add(programFilesX86, "Mozilla Firefox", "firefox.exe")
		add(localAppData, "Mozilla Firefox", "firefox.exe")
	case "zen":
		add(programFiles, "Zen Browser", "zen.exe")
		add(localAppData, "Zen Browser", "zen.exe")
		add(localAppData, "Programs", "Zen Browser", "zen.exe")
	}
	return out
}

// ExtractExecutablePath extracts the executable path from a Windows Registry shell open command.
func ExtractExecutablePath(command string) string {
	s := strings.TrimSpace(command)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, `"`) {
		if end := strings.Index(s[1:], `"`); end >= 0 {
			return strings.TrimSpace(s[1 : 1+end])
		}
		return strings.Trim(s, `"`)
	}
	lower := strings.ToLower(s)
	if idx := strings.Index(lower, ".exe"); idx >= 0 {
		return strings.TrimSpace(s[:idx+4])
	}
	fields := strings.Fields(s)
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// ClassifyBrowserFamily determines whether a browser is Chromium-based, Firefox-based, or other.
func ClassifyBrowserFamily(id, name, exePath string) string {
	combined := strings.ToLower(id + " " + name + " " + filepath.Base(exePath))
	if strings.Contains(combined, "iexplore") || strings.Contains(combined, "internet explorer") {
		return BrowserFamilyOther
	}
	for _, token := range []string{"firefox", "zen", "waterfox", "librewolf", "floorp", "palemoon", "tor browser", "mullvad"} {
		if strings.Contains(combined, token) {
			return BrowserFamilyFirefox
		}
	}
	return BrowserFamilyChromium
}

func normalizeBrowserPath(p string) string {
	p = strings.TrimSpace(strings.Trim(p, `"`))
	if p == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(p))
}

func sanitizeBrowserID(raw string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// MergeDiscoveredBrowsers merges dynamically discovered browsers (from StartMenuInternet
// and MSIX/WindowsApps) into the built-in catalog, deduplicating by executable path and ID,
// and ordering installed browsers first.
func MergeDiscoveredBrowsers(catalog []BrowserInfo, discovered []StartMenuBrowserEntry, fileExists func(string) bool) []BrowserInfo {
	out := make([]BrowserInfo, len(catalog))
	copy(out, catalog)

	seenPaths := make(map[string]int, len(out))
	seenIDs := make(map[string]int, len(out))
	for i, b := range out {
		seenIDs[strings.ToLower(b.ID)] = i
		if b.Installed && b.ExecutablePath != "" {
			seenPaths[normalizeBrowserPath(b.ExecutablePath)] = i
		}
	}

	var dynamic []BrowserInfo
	for _, entry := range discovered {
		exePath := ExtractExecutablePath(entry.Command)
		if exePath == "" {
			continue
		}
		if fileExists != nil && !fileExists(exePath) {
			continue
		}
		normPath := normalizeBrowserPath(exePath)
		if _, exists := seenPaths[normPath]; exists {
			continue
		}

		exeBase := filepath.Base(exePath)
		displayName := strings.TrimSpace(entry.DisplayName)
		if displayName == "" {
			displayName = strings.TrimSpace(entry.Key)
		}
		if displayName == "" {
			displayName = strings.TrimSuffix(exeBase, filepath.Ext(exeBase))
		}

		family := ClassifyBrowserFamily(entry.Key, displayName, exePath)
		if family == BrowserFamilyOther {
			// Skip legacy Internet Explorer and non-extension browsers.
			continue
		}

		// Try to match an existing catalog browser that wasn't detected yet.
		matchedIdx := -1
		lowerName := strings.ToLower(displayName)
		lowerKey := strings.ToLower(entry.Key)
		lowerExe := strings.ToLower(exeBase)
		for i, b := range out {
			if b.Installed {
				continue
			}
			if strings.EqualFold(b.Name, displayName) ||
				strings.Contains(lowerName, strings.ToLower(b.Name)) ||
				strings.Contains(lowerKey, strings.ToLower(b.ID)) {
				matchedIdx = i
				break
			}
			// Only match by exeName when it's not ambiguous (e.g. opera.exe is shared by Opera & Opera GX).
			if lowerExe != "" && lowerExe != "opera.exe" && lowerExe != "browser.exe" && strings.EqualFold(b.ExeName, exeBase) {
				matchedIdx = i
				break
			}
		}

		if matchedIdx >= 0 {
			out[matchedIdx].ExecutablePath = exePath
			out[matchedIdx].Installed = true
			seenPaths[normPath] = matchedIdx
			continue
		}

		id := sanitizeBrowserID(displayName)
		if id == "" {
			id = sanitizeBrowserID(entry.Key)
		}
		if id == "" {
			id = sanitizeBrowserID(strings.TrimSuffix(exeBase, filepath.Ext(exeBase)))
		}
		if _, exists := seenIDs[id]; exists {
			continue
		}

		nativeKey := defaultChromeNativeHostKey
		extURL := "chrome://extensions"
		if family == BrowserFamilyFirefox {
			nativeKey = defaultFirefoxNativeHostKey
			extURL = "about:addons"
		}

		seenIDs[id] = len(out) + len(dynamic)
		seenPaths[normPath] = len(out) + len(dynamic)
		dynamic = append(dynamic, BrowserInfo{
			ID:                 id,
			Name:               displayName,
			ExeName:            exeBase,
			ExecutablePath:     exePath,
			Installed:          true,
			Family:             family,
			NativeMessagingKey: nativeKey,
			ExtensionsURL:      extURL,
		})
	}

	sort.Slice(dynamic, func(i, j int) bool {
		return strings.ToLower(dynamic[i].Name) < strings.ToLower(dynamic[j].Name)
	})

	// Order: installed catalog browsers first, then dynamically discovered browsers,
	// then uninstalled catalog browsers.
	result := make([]BrowserInfo, 0, len(out)+len(dynamic))
	for _, b := range out {
		if b.Installed {
			result = append(result, b)
		}
	}
	result = append(result, dynamic...)
	for _, b := range out {
		if !b.Installed {
			result = append(result, b)
		}
	}
	return result
}

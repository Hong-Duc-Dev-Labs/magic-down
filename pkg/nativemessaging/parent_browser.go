package nativemessaging

import (
	"path/filepath"
	"strings"
)

// MatchBrowserIDFromProcess maps a process executable name (and optional full path)
// to its canonical MagicDown browser ID. Returns "" if the process is not a known browser.
func MatchBrowserIDFromProcess(exeName, exePath string) string {
	base := strings.ToLower(strings.TrimSpace(exeName))
	if base == "" && strings.TrimSpace(exePath) != "" {
		base = strings.ToLower(filepath.Base(strings.TrimSpace(exePath)))
	}
	lowerPath := strings.ToLower(strings.TrimSpace(exePath))

	switch base {
	case "chrome.exe":
		return "chrome"
	case "msedge.exe":
		return "edge"
	case "dia.exe":
		return "dia"
	case "arc.exe":
		return "arc"
	case "brave.exe":
		return "brave"
	case "vivaldi.exe":
		return "vivaldi"
	case "comet.exe":
		return "comet"
	case "thorium.exe":
		return "thorium"
	case "firefox.exe":
		return "firefox"
	case "zen.exe":
		return "zen"
	case "opera.exe":
		if strings.Contains(lowerPath, "opera gx") {
			return "operagx"
		}
		return "opera"
	case "browser.exe":
		if strings.Contains(lowerPath, "coccoc") {
			return "coccoc"
		}
		return ""
	default:
		return ""
	}
}

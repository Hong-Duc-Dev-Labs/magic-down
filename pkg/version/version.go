package version

import "strings"

// Build-time variables injected via -ldflags:
//
//	-X magicdown/pkg/version.Version=1.0.0
//	-X magicdown/pkg/version.Commit=abc1234
//	-X magicdown/pkg/version.BuildDate=2026-11-25T12:00:00Z
//	-X magicdown/pkg/version.GitHubRepo=owner/repo
var (
	Version    = "0.1.0"
	Commit     = "dev"
	BuildDate  = "unknown"
	GitHubRepo = "hongducdev/MagicDown"
)

// NormalizedVersion returns Version with a leading "v" prefix (e.g. "v0.1.0").
func NormalizedVersion() string {
	v := strings.TrimSpace(Version)
	if v == "" {
		return "v0.0.0"
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return "v" + v[1:]
	}
	return "v" + v
}

// DisplayVersion returns a human-readable version string including short commit
// when available.
func DisplayVersion() string {
	v := NormalizedVersion()
	c := strings.TrimSpace(Commit)
	if c != "" && c != "dev" && c != "unknown" {
		return v + " (" + c + ")"
	}
	return v
}

// ResolveGitHubRepo returns the configured override repo if non-empty,
// otherwise the build-time GitHubRepo default.
func ResolveGitHubRepo(override string) string {
	if r := strings.TrimSpace(override); r != "" {
		return strings.Trim(r, "/")
	}
	return strings.Trim(strings.TrimSpace(GitHubRepo), "/")
}

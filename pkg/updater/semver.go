package updater

import (
	"fmt"
	"strconv"
	"strings"
)

type semver struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func parseSemver(raw string) (semver, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return semver{}, fmt.Errorf("empty version string")
	}
	if strings.HasPrefix(s, "v") || strings.HasPrefix(s, "V") {
		s = s[1:]
	}
	// Strip build metadata (+...)
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}
	var pre string
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return semver{}, fmt.Errorf("invalid semver %q", raw)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return semver{}, fmt.Errorf("invalid major version in %q", raw)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return semver{}, fmt.Errorf("invalid minor version in %q", raw)
	}
	patch := 0
	if len(parts) == 3 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil || patch < 0 {
			return semver{}, fmt.Errorf("invalid patch version in %q", raw)
		}
	}
	return semver{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: pre,
	}, nil
}

// CompareSemver compares two semantic version strings (with or without leading "v").
// Returns -1 if a < b, 0 if a == b, and 1 if a > b.
func CompareSemver(a, b string) (int, error) {
	va, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	vb, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	if va.major != vb.major {
		if va.major < vb.major {
			return -1, nil
		}
		return 1, nil
	}
	if va.minor != vb.minor {
		if va.minor < vb.minor {
			return -1, nil
		}
		return 1, nil
	}
	if va.patch != vb.patch {
		if va.patch < vb.patch {
			return -1, nil
		}
		return 1, nil
	}
	// Release > prerelease
	if va.prerelease == vb.prerelease {
		return 0, nil
	}
	if va.prerelease == "" {
		return 1, nil
	}
	if vb.prerelease == "" {
		return -1, nil
	}
	if va.prerelease < vb.prerelease {
		return -1, nil
	}
	return 1, nil
}

// IsNewer reports whether latest is strictly newer than current.
func IsNewer(current, latest string) bool {
	cmp, err := CompareSemver(current, latest)
	if err != nil {
		return false
	}
	return cmp < 0
}

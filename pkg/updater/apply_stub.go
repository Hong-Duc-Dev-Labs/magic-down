//go:build !windows

package updater

import "errors"

// BuildApplyScript returns an empty string on non-Windows platforms.
func BuildApplyScript(stagingDir, installDir string, currentPID int, relaunchExe string) string {
	return ""
}

// LaunchApplyScript is only supported on Windows.
func LaunchApplyScript(stagingDir, installDir string, currentPID int, relaunchExe string) (string, error) {
	return "", errors.New("self-update apply script is only supported on Windows")
}

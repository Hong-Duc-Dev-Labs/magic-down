//go:build !windows

package power

import (
	"errors"
	"os/exec"
	"runtime"
)

func defaultOpenFile(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func defaultSleep() error {
	return errors.New("system sleep is only supported on Windows")
}

func defaultShutdown() error {
	return errors.New("system shutdown is only supported on Windows")
}

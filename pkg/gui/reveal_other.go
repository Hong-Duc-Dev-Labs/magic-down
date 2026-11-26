//go:build !windows

package gui

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

func revealInFileManager(target revealTarget) error {
	if target.Path == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		if target.SelectFile {
			return exec.Command("open", "-R", target.Path).Start()
		}
		return exec.Command("open", target.Path).Start()
	default:
		if target.SelectFile {
			return exec.Command("xdg-open", filepath.Dir(target.Path)).Start()
		}
		return exec.Command("xdg-open", target.Path).Start()
	}
}

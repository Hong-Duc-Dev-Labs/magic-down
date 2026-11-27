//go:build windows

package engine

import (
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

const createNoWindow = 0x08000000

func hideCommandWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// configureProcessCleanup configures cmd so that when its context is cancelled,
// the entire Windows process tree (e.g. yt-dlp.exe + child ffmpeg.exe) is
// terminated cleanly without leaking background processes.
func configureProcessCleanup(cmd *exec.Cmd) {
	hideCommandWindow(cmd)
	if cmd == nil {
		return
	}
	cmd.WaitDelay = 3 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		killCmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		hideCommandWindow(killCmd)
		if err := killCmd.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}

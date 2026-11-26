//go:build windows

package power

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	shell32DLL          = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW   = shell32DLL.NewProc("ShellExecuteW")
	powrprofDLL         = syscall.NewLazyDLL("powrprof.dll")
	procSetSuspendState = powrprofDLL.NewProc("SetSuspendState")
)

const swShowNormal = 1

func defaultOpenFile(path string) error {
	verbPtr, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		0,
		swShowNormal,
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW failed with code %d", ret)
	}
	return nil
}

func defaultSleep() error {
	// SetSuspendState(bHibernate=0, bForce=0, bWakeupEventsDisabled=0)
	r1, _, err := procSetSuspendState.Call(0, 0, 0)
	if r1 == 0 {
		return fmt.Errorf("SetSuspendState failed: %w", err)
	}
	return nil
}

func defaultShutdown() error {
	return exec.Command("shutdown", "/s", "/t", "0").Run()
}

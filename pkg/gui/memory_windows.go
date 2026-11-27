//go:build windows

package gui

import (
	"runtime"
	"runtime/debug"
	"syscall"
)

var (
	memKernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentProcess        = memKernel32.NewProc("GetCurrentProcess")
	procSetProcessWorkingSetSize = memKernel32.NewProc("SetProcessWorkingSetSize")
)

// trimProcessMemory releases unused Go heap pages and hints Windows to trim the
// process working set while MagicDown is idle and hidden in the System Tray.
func trimProcessMemory() {
	runtime.GC()
	debug.FreeOSMemory()
	hProc, _, _ := procGetCurrentProcess.Call()
	if hProc != 0 {
		// Passing (SIZE_T)-1 for both min and max asks Windows to trim the working set.
		const trimAll = ^uintptr(0)
		_, _, _ = procSetProcessWorkingSetSize.Call(hProc, trimAll, trimAll)
	}
}

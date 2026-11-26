//go:build windows

package gui

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32DLL                    = syscall.NewLazyDLL("user32.dll")
	kernel32DLL                  = syscall.NewLazyDLL("kernel32.dll")
	procShowWindow               = user32DLL.NewProc("ShowWindow")
	procIsIconic                 = user32DLL.NewProc("IsIconic")
	procGetForegroundWindow      = user32DLL.NewProc("GetForegroundWindow")
	procSetForegroundWindow      = user32DLL.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32DLL.NewProc("BringWindowToTop")
	procSetActiveWindow          = user32DLL.NewProc("SetActiveWindow")
	procSetWindowPos             = user32DLL.NewProc("SetWindowPos")
	procAllowSetForegroundWindow = user32DLL.NewProc("AllowSetForegroundWindow")
	procGetWindowThreadProcessId = user32DLL.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32DLL.NewProc("AttachThreadInput")
	procFlashWindowEx            = user32DLL.NewProc("FlashWindowEx")
	procGetCurrentThreadId       = kernel32DLL.NewProc("GetCurrentThreadId")
)

const (
	swShow        = 5
	swRestore     = 9
	asfwAny       = ^uintptr(0) // (DWORD)-1
	hwndTopmost   = ^uintptr(0) // (HWND)-1
	hwndNoTopmost = ^uintptr(1) // (HWND)-2
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpShowWindow = 0x0040
	flashwAll     = 0x00000003
	flashwTimerNo = 0x0000000C
)

type flashWInfo struct {
	cbSize    uint32
	hwnd      uintptr
	dwFlags   uint32
	uCount    uint32
	dwTimeout uint32
}

// bringWindowToForeground restores a hidden/minimized Windows window and raises it
// to the foreground even when triggered from a background IPC request (IDM-style).
// If Windows still denies focus activation, it flashes the taskbar button as fallback.
func bringWindowToForeground(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	_, _, _ = procAllowSetForegroundWindow.Call(asfwAny)

	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
	} else {
		_, _, _ = procShowWindow.Call(hwnd, swShow)
	}

	fgHwnd, _, _ := procGetForegroundWindow.Call()
	var attachedCur, attachedWin bool
	var curThread, winThread, fgThread uintptr

	if fgHwnd != 0 && fgHwnd != hwnd {
		fgThread, _, _ = procGetWindowThreadProcessId.Call(fgHwnd, 0)
		winThread, _, _ = procGetWindowThreadProcessId.Call(hwnd, 0)
		curThread, _, _ = procGetCurrentThreadId.Call()

		if fgThread != 0 && curThread != 0 && curThread != fgThread {
			if r, _, _ := procAttachThreadInput.Call(curThread, fgThread, 1); r != 0 {
				attachedCur = true
			}
		}
		if fgThread != 0 && winThread != 0 && winThread != fgThread && winThread != curThread {
			if r, _, _ := procAttachThreadInput.Call(winThread, fgThread, 1); r != 0 {
				attachedWin = true
			}
		}
	}

	// Pulse HWND_TOPMOST -> HWND_NOTOPMOST to raise Z-order above browsers.
	flags := uintptr(swpNoMove | swpNoSize | swpShowWindow)
	_, _, _ = procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, flags)
	_, _, _ = procSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, flags)
	_, _, _ = procBringWindowToTop.Call(hwnd)
	_, _, _ = procSetForegroundWindow.Call(hwnd)
	_, _, _ = procSetActiveWindow.Call(hwnd)

	if attachedWin {
		_, _, _ = procAttachThreadInput.Call(winThread, fgThread, 0)
	}
	if attachedCur {
		_, _, _ = procAttachThreadInput.Call(curThread, fgThread, 0)
	}

	nowFg, _, _ := procGetForegroundWindow.Call()
	if nowFg == hwnd {
		return true
	}

	// Fallback if Windows strictly refused foreground focus: flash taskbar button.
	fwi := flashWInfo{
		cbSize:    uint32(unsafe.Sizeof(flashWInfo{})),
		hwnd:      hwnd,
		dwFlags:   flashwAll | flashwTimerNo,
		uCount:    3,
		dwTimeout: 0,
	}
	_, _, _ = procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fwi)))
	return false
}

//go:build windows

package gui

import (
	"fmt"
	"syscall"
	"unsafe"
)

const cfUnicodeText = 13

var (
	clipUser32                   = syscall.NewLazyDLL("user32.dll")
	clipKernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetClipboardSequenceNum  = clipUser32.NewProc("GetClipboardSequenceNumber")
	procOpenClipboard            = clipUser32.NewProc("OpenClipboard")
	procCloseClipboard           = clipUser32.NewProc("CloseClipboard")
	procIsClipboardFormatAvail   = clipUser32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData         = clipUser32.NewProc("GetClipboardData")
	procGlobalLock               = clipKernel32.NewProc("GlobalLock")
	procGlobalUnlock             = clipKernel32.NewProc("GlobalUnlock")
	procGlobalSize               = clipKernel32.NewProc("GlobalSize")
	procRtlMoveMemory            = clipKernel32.NewProc("RtlMoveMemory")
)

// clipboardSequenceNumber returns the Windows clipboard change counter without
// opening the clipboard or allocating memory.
func clipboardSequenceNumber() (uint32, bool) {
	r1, _, _ := procGetClipboardSequenceNum.Call()
	return uint32(r1), true
}

// readClipboardText reads CF_UNICODETEXT from the Windows clipboard.
// Callers should gate this behind clipboardSequenceNumber() so it only executes
// when the clipboard actually changes.
func readClipboardText() (string, error) {
	avail, _, _ := procIsClipboardFormatAvail.Call(cfUnicodeText)
	if avail == 0 {
		return "", nil
	}
	opened, _, err := procOpenClipboard.Call(0)
	if opened == 0 {
		return "", fmt.Errorf("OpenClipboard failed: %w", err)
	}
	defer procCloseClipboard.Call()

	hData, _, err := procGetClipboardData.Call(cfUnicodeText)
	if hData == 0 {
		return "", nil
	}
	ptr, _, err := procGlobalLock.Call(hData)
	if ptr == 0 {
		return "", fmt.Errorf("GlobalLock failed: %w", err)
	}
	defer procGlobalUnlock.Call(hData)

	sizeBytes, _, _ := procGlobalSize.Call(hData)
	if sizeBytes < 2 {
		return "", nil
	}
	// Cap read length defensively to avoid allocating huge strings for non-URL
	// clipboard payloads (e.g. megabytes of copied text).
	const maxClipboardBytes = 64 * 1024
	if sizeBytes > maxClipboardBytes {
		return "", nil
	}
	u16Len := int(sizeBytes / 2)
	buf := make([]uint16, u16Len)
	_, _, _ = procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), ptr, uintptr(u16Len*2))
	return syscall.UTF16ToString(buf), nil
}

//go:build !windows

package gui

func bringWindowToForeground(hwnd uintptr) bool {
	return false
}

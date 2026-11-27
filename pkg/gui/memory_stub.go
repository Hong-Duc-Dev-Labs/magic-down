//go:build !windows

package gui

import (
	"runtime"
	"runtime/debug"
)

func trimProcessMemory() {
	runtime.GC()
	debug.FreeOSMemory()
}

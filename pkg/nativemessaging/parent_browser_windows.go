//go:build windows

package nativemessaging

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	th32csSnapProcess              = 0x00000002
	processQueryLimitedInformation = 0x1000
	maxPathUTF16                   = 260
)

type processEntry32W struct {
	Size              uint32
	CntUsage          uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	CntThreads        uint32
	ParentProcessID   uint32
	PriClassBase      int32
	Flags             uint32
	ExeFile           [maxPathUTF16]uint16
}

var (
	kernel32DLL                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = kernel32DLL.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32DLL.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32DLL.NewProc("Process32NextW")
	procOpenProcess              = kernel32DLL.NewProc("OpenProcess")
	procQueryFullProcessImageW   = kernel32DLL.NewProc("QueryFullProcessImageNameW")
	procCloseHandle              = kernel32DLL.NewProc("CloseHandle")
)

type procSnapEntry struct {
	parentPID uint32
	exeName   string
}

// DetectParentBrowserID walks the ancestor process chain of the current native host
// process on Windows to identify the real calling browser (e.g. Dia.exe, msedge.exe,
// chrome.exe). This avoids misidentifying Chromium forks like Dia or Arc whose
// navigator.userAgent matches Google Chrome.
func DetectParentBrowserID() string {
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if snap == 0 || snap == uintptr(syscall.InvalidHandle) {
		return ""
	}
	defer procCloseHandle.Call(snap)

	procs := make(map[uint32]procSnapEntry, 256)
	var pe processEntry32W
	pe.Size = uint32(unsafe.Sizeof(pe))

	r1, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	for r1 != 0 {
		procs[pe.ProcessID] = procSnapEntry{
			parentPID: pe.ParentProcessID,
			exeName:   syscall.UTF16ToString(pe.ExeFile[:]),
		}
		r1, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
	}

	curPID := uint32(os.Getpid())
	visited := make(map[uint32]struct{}, 10)
	for depth := 0; depth < 8; depth++ {
		entry, ok := procs[curPID]
		if !ok || entry.parentPID == 0 || entry.parentPID == curPID {
			break
		}
		parentPID := entry.parentPID
		if _, seen := visited[parentPID]; seen {
			break
		}
		visited[parentPID] = struct{}{}

		parentEntry, ok := procs[parentPID]
		if !ok {
			break
		}
		exePath := ""
		lowerExe := strings.ToLower(strings.TrimSpace(parentEntry.exeName))
		if lowerExe == "opera.exe" || lowerExe == "browser.exe" {
			exePath = queryProcessImagePath(parentPID)
		}
		if id := MatchBrowserIDFromProcess(parentEntry.exeName, exePath); id != "" {
			return id
		}
		curPID = parentPID
	}
	return ""
}

func queryProcessImagePath(pid uint32) string {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)

	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r1, _, _ := procQueryFullProcessImageW.Call(
		h,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 == 0 || size == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

//go:build windows

package nativemessaging

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	versionDLL                 = syscall.NewLazyDLL("version.dll")
	procGetFileVersionInfoSize = versionDLL.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfo     = versionDLL.NewProc("GetFileVersionInfoW")
	procVerQueryValue          = versionDLL.NewProc("VerQueryValueW")
)

type vsFixedFileInfo struct {
	Signature        uint32
	StrucVersion     uint32
	FileVersionMS    uint32
	FileVersionLS    uint32
	ProductVersionMS uint32
	ProductVersionLS uint32
	FileFlagsMask    uint32
	FileFlags        uint32
	FileOS           uint32
	FileType         uint32
	FileSubtype      uint32
	FileDateMS       uint32
	FileDateLS       uint32
}

// ReadFileVersion reads the Windows FileVersion resource (major.minor.build.revision)
// from exePath without spawning external processes. Returns "" if unavailable.
func ReadFileVersion(exePath string) string {
	p := strings.TrimSpace(exePath)
	if p == "" {
		return ""
	}
	pathPtr, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return ""
	}
	var handle uint32
	size, _, _ := procGetFileVersionInfoSize.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&handle)),
	)
	if size == 0 {
		return ""
	}
	buf := make([]byte, size)
	r1, _, _ := procGetFileVersionInfo.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		size,
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if r1 == 0 {
		return ""
	}
	rootPtr, err := syscall.UTF16PtrFromString(`\`)
	if err != nil {
		return ""
	}
	var infoPtr uintptr
	var infoLen uint32
	r1, _, _ = procVerQueryValue.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&infoPtr)),
		uintptr(unsafe.Pointer(&infoLen)),
	)
	if r1 == 0 || infoPtr == 0 || infoLen < uint32(unsafe.Sizeof(vsFixedFileInfo{})) {
		return ""
	}
	info := (*vsFixedFileInfo)(unsafe.Pointer(infoPtr))
	major := (info.FileVersionMS >> 16) & 0xffff
	minor := info.FileVersionMS & 0xffff
	build := (info.FileVersionLS >> 16) & 0xffff
	rev := info.FileVersionLS & 0xffff
	if major == 0 && minor == 0 && build == 0 && rev == 0 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", major, minor, build, rev)
}

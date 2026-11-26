//go:build windows

package nativemessaging

import (
	"encoding/binary"
	"fmt"
	"runtime"
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
	basePtr := uintptr(unsafe.Pointer(&buf[0]))
	structSize := unsafe.Sizeof(vsFixedFileInfo{})
	if r1 == 0 || infoPtr == 0 || infoLen < uint32(structSize) || infoPtr < basePtr {
		runtime.KeepAlive(buf)
		runtime.KeepAlive(rootPtr)
		runtime.KeepAlive(pathPtr)
		return ""
	}
	offset := infoPtr - basePtr
	if offset > uintptr(len(buf)) || structSize > uintptr(len(buf))-offset {
		runtime.KeepAlive(buf)
		runtime.KeepAlive(rootPtr)
		runtime.KeepAlive(pathPtr)
		return ""
	}
	// VS_FIXEDFILEINFO layout:
	//   0..4:  dwSignature
	//   4..8:  dwStrucVersion
	//   8..12: dwFileVersionMS
	//  12..16: dwFileVersionLS
	fileVerMS := binary.LittleEndian.Uint32(buf[offset+8 : offset+12])
	fileVerLS := binary.LittleEndian.Uint32(buf[offset+12 : offset+16])
	runtime.KeepAlive(buf)
	runtime.KeepAlive(rootPtr)
	runtime.KeepAlive(pathPtr)
	major := (fileVerMS >> 16) & 0xffff
	minor := fileVerMS & 0xffff
	build := (fileVerLS >> 16) & 0xffff
	rev := fileVerLS & 0xffff
	if major == 0 && minor == 0 && build == 0 && rev == 0 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", major, minor, build, rev)
}

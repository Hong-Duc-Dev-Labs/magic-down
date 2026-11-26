//go:build windows

package engine

import (
	"net/http"
	"net/url"
	"syscall"
	"unsafe"
)

const (
	winHKeyCurrentUser     = uintptr(0x80000001)
	winKeyQueryValue       = 0x0001
	winInternetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

var (
	proxyAdvapi32            = syscall.NewLazyDLL("advapi32.dll")
	proxyProcRegOpenKeyExW   = proxyAdvapi32.NewProc("RegOpenKeyExW")
	proxyProcRegQueryValueEx = proxyAdvapi32.NewProc("RegQueryValueExW")
	proxyProcRegCloseKey     = proxyAdvapi32.NewProc("RegCloseKey")
)

func detectPlatformSystemProxy(req *http.Request) (*url.URL, error) {
	enabled, server, override := readWindowsInternetSettingsProxy()
	if !enabled || server == "" {
		return nil, nil
	}
	return parseWindowsProxyServer(server, override, req)
}

func readWindowsInternetSettingsProxy() (enabled bool, server string, override string) {
	subKeyPtr, err := syscall.UTF16PtrFromString(winInternetSettingsKey)
	if err != nil {
		return false, "", ""
	}
	var hKey uintptr
	r1, _, _ := proxyProcRegOpenKeyExW.Call(
		winHKeyCurrentUser,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		winKeyQueryValue,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r1 != 0 {
		return false, "", ""
	}
	defer proxyProcRegCloseKey.Call(hKey)

	if dw, ok := queryRegDWORD(hKey, "ProxyEnable"); !ok || dw == 0 {
		return false, "", ""
	}
	server, _ = queryRegString(hKey, "ProxyServer")
	override, _ = queryRegString(hKey, "ProxyOverride")
	return server != "", server, override
}

func queryRegDWORD(hKey uintptr, name string) (uint32, bool) {
	valNamePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, false
	}
	var valType uint32
	var data uint32
	bufSize := uint32(unsafe.Sizeof(data))
	r1, _, _ := proxyProcRegQueryValueEx.Call(
		hKey,
		uintptr(unsafe.Pointer(valNamePtr)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if r1 != 0 {
		return 0, false
	}
	return data, true
}

func queryRegString(hKey uintptr, name string) (string, bool) {
	valNamePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", false
	}
	var valType uint32
	var bufSize uint32
	r1, _, _ := proxyProcRegQueryValueEx.Call(
		hKey,
		uintptr(unsafe.Pointer(valNamePtr)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if r1 != 0 || bufSize == 0 {
		return "", false
	}
	buf := make([]uint16, (bufSize+1)/2)
	r1, _, _ = proxyProcRegQueryValueEx.Call(
		hKey,
		uintptr(unsafe.Pointer(valNamePtr)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if r1 != 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

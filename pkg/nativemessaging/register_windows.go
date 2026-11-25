//go:build windows

package nativemessaging

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	hkeyCurrentUser = uintptr(0x80000001)
	keyQueryValue   = 0x0001
	keySetValue     = 0x0002
	regSZ           = 1
	errFileNotFound = syscall.Errno(2)
)

var (
	advapi32            = syscall.NewLazyDLL("advapi32.dll")
	procRegCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	procRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

const (
	chromeNativeHostKey = `Software\Google\Chrome\NativeMessagingHosts\` + HostName
	edgeNativeHostKey   = `Software\Microsoft\Edge\NativeMessagingHosts\` + HostName
)

// RegisterHost writes the manifest JSON and registers HKCU keys for Chrome and Edge.
func RegisterHost(hostBinaryPath, extensionID string) (string, error) {
	if strings.TrimSpace(hostBinaryPath) == "" {
		resolved, err := ResolveHostBinary()
		if err != nil {
			return "", err
		}
		hostBinaryPath = resolved
	}
	manifestPath, err := DefaultManifestPath()
	if err != nil {
		return "", err
	}
	m, err := BuildManifest(hostBinaryPath, extensionID)
	if err != nil {
		return "", err
	}
	if err := WriteManifest(manifestPath, m); err != nil {
		return "", err
	}
	if err := setRegDefaultString(chromeNativeHostKey, manifestPath); err != nil {
		return "", fmt.Errorf("chrome registry: %w", err)
	}
	if err := setRegDefaultString(edgeNativeHostKey, manifestPath); err != nil {
		return "", fmt.Errorf("edge registry: %w", err)
	}
	return manifestPath, nil
}

// CheckRegistration inspects the manifest file and Chrome/Edge HKCU registry keys.
func CheckRegistration() RegistrationStatus {
	manifestPath, _ := DefaultManifestPath()
	st := RegistrationStatus{ManifestPath: manifestPath}
	if manifestPath != "" {
		if info, err := os.Stat(manifestPath); err == nil && !info.IsDir() {
			st.ManifestExists = true
		}
	}
	if val, ok := getRegDefaultString(chromeNativeHostKey); ok && strings.EqualFold(strings.TrimSpace(val), manifestPath) {
		st.ChromeReady = true
	}
	if val, ok := getRegDefaultString(edgeNativeHostKey); ok && strings.EqualFold(strings.TrimSpace(val), manifestPath) {
		st.EdgeReady = true
	}
	return st
}

func setRegDefaultString(subKey, value string) error {
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return err
	}
	var hKey uintptr
	r1, _, _ := procRegCreateKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0, 0, 0,
		keySetValue,
		0,
		uintptr(unsafe.Pointer(&hKey)),
		0,
	)
	if r1 != 0 {
		return syscall.Errno(r1)
	}
	defer procRegCloseKey.Call(hKey)

	valUTF16, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	r1, _, _ = procRegSetValueExW.Call(
		hKey,
		0, // (Default) value
		0,
		regSZ,
		uintptr(unsafe.Pointer(&valUTF16[0])),
		uintptr(len(valUTF16)*2),
	)
	if r1 != 0 {
		return syscall.Errno(r1)
	}
	return nil
}

func getRegDefaultString(subKey string) (string, bool) {
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return "", false
	}
	var hKey uintptr
	r1, _, _ := procRegOpenKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		keyQueryValue,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r1 != 0 {
		return "", false
	}
	defer procRegCloseKey.Call(hKey)

	var valType uint32
	var bufSize uint32
	r1, _, _ = procRegQueryValueEx.Call(
		hKey,
		0,
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if r1 != 0 || bufSize == 0 {
		return "", false
	}
	buf := make([]uint16, (bufSize+1)/2)
	r1, _, _ = procRegQueryValueEx.Call(
		hKey,
		0,
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

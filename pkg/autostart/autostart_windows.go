//go:build windows

package autostart

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	hkeyCurrentUser   = uintptr(0x80000001)
	keyQueryValue     = 0x0001
	keySetValue       = 0x0002
	regSZ             = 1
	errFileNotFound   = syscall.Errno(2)
	errMoreData       = syscall.Errno(234)
)

var (
	advapi32            = syscall.NewLazyDLL("advapi32.dll")
	procRegCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	procRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

type windowsRunStore struct {
	subKey string
}

func defaultStore() Store {
	return &windowsRunStore{subKey: RegistryRunPath}
}

func (s *windowsRunStore) Get(name string) (string, bool, error) {
	subKeyPtr, err := syscall.UTF16PtrFromString(s.subKey)
	if err != nil {
		return "", false, err
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
		if syscall.Errno(r1) == errFileNotFound {
			return "", false, nil
		}
		return "", false, fmt.Errorf("RegOpenKeyExW failed: %w", syscall.Errno(r1))
	}
	defer procRegCloseKey.Call(hKey)

	valNamePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", false, err
	}
	var valType uint32
	var bufSize uint32
	r1, _, _ = procRegQueryValueEx.Call(
		hKey,
		uintptr(unsafe.Pointer(valNamePtr)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if r1 != 0 {
		if syscall.Errno(r1) == errFileNotFound {
			return "", false, nil
		}
		return "", false, fmt.Errorf("RegQueryValueExW size query failed: %w", syscall.Errno(r1))
	}
	if bufSize == 0 {
		return "", true, nil
	}

	buf := make([]uint16, (bufSize+1)/2)
	r1, _, _ = procRegQueryValueEx.Call(
		hKey,
		uintptr(unsafe.Pointer(valNamePtr)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if r1 != 0 && syscall.Errno(r1) != errMoreData {
		return "", false, fmt.Errorf("RegQueryValueExW read failed: %w", syscall.Errno(r1))
	}
	return syscall.UTF16ToString(buf), true, nil
}

func (s *windowsRunStore) Set(name, value string) error {
	subKeyPtr, err := syscall.UTF16PtrFromString(s.subKey)
	if err != nil {
		return err
	}
	var hKey uintptr
	r1, _, _ := procRegCreateKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		0,
		0,
		keySetValue,
		0,
		uintptr(unsafe.Pointer(&hKey)),
		0,
	)
	if r1 != 0 {
		return fmt.Errorf("RegCreateKeyExW failed: %w", syscall.Errno(r1))
	}
	defer procRegCloseKey.Call(hKey)

	valNamePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	valUTF16, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	r1, _, _ = procRegSetValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(valNamePtr)),
		0,
		regSZ,
		uintptr(unsafe.Pointer(&valUTF16[0])),
		uintptr(len(valUTF16)*2),
	)
	if r1 != 0 {
		return fmt.Errorf("RegSetValueExW failed: %w", syscall.Errno(r1))
	}
	return nil
}

func (s *windowsRunStore) Delete(name string) error {
	subKeyPtr, err := syscall.UTF16PtrFromString(s.subKey)
	if err != nil {
		return err
	}
	var hKey uintptr
	r1, _, _ := procRegOpenKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		keySetValue,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r1 != 0 {
		if syscall.Errno(r1) == errFileNotFound {
			return nil
		}
		return fmt.Errorf("RegOpenKeyExW failed: %w", syscall.Errno(r1))
	}
	defer procRegCloseKey.Call(hKey)

	valNamePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	r1, _, _ = procRegDeleteValueW.Call(hKey, uintptr(unsafe.Pointer(valNamePtr)))
	if r1 != 0 && syscall.Errno(r1) != errFileNotFound {
		return fmt.Errorf("RegDeleteValueW failed: %w", syscall.Errno(r1))
	}
	return nil
}

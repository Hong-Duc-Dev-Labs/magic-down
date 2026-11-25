//go:build windows

package i18n

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32DLL         = syscall.NewLazyDLL("kernel32.dll")
	procGetUserLocale   = kernel32DLL.NewProc("GetUserDefaultLocaleName")
)

// DetectSystemLang inspects the Windows user locale (GetUserDefaultLocaleName)
// and falls back to LANG/LC_ALL environment variables.
func DetectSystemLang() Lang {
	buf := make([]uint16, 85) // LOCALE_NAME_MAX_LENGTH
	r, _, _ := procGetUserLocale.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r > 0 {
		locale := syscall.UTF16ToString(buf)
		if locale != "" {
			return Normalize(locale)
		}
	}
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(env); v != "" {
			return Normalize(v)
		}
	}
	return LangEN
}

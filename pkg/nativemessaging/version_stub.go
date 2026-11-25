//go:build !windows

package nativemessaging

// ReadFileVersion returns the executable file version when supported by the OS.
func ReadFileVersion(exePath string) string {
	return ""
}

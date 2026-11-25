//go:build windows

package nativemessaging

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

const hkeyLocalMachine = uintptr(0x80000002)

// DetectInstalledBrowsers scans Windows Registry App Paths and standard installation
// directories to find installed Chromium-based browsers and their Native Messaging state.
func DetectInstalledBrowsers() []BrowserInfo {
	pf := os.Getenv("ProgramFiles")
	pf86 := os.Getenv("ProgramFiles(x86)")
	localAppData := os.Getenv("LOCALAPPDATA")
	manifestPath, _ := DefaultManifestPath()

	browsers := SupportedBrowsers()
	for i := range browsers {
		b := &browsers[i]
		// 1. Check standard filesystem locations
		for _, p := range CandidateBrowserPaths(b.ID, pf, pf86, localAppData) {
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				b.ExecutablePath = p
				b.Installed = true
				break
			}
		}
		// 2. Fallback to Windows Registry App Paths (HKLM / HKCU)
		if !b.Installed && b.ExeName != "" {
			if regPath := lookupAppPath(b.ExeName); regPath != "" {
				if info, err := os.Stat(regPath); err == nil && !info.IsDir() {
					b.ExecutablePath = regPath
					b.Installed = true
				}
			}
		}
		// 3. Check Native Messaging registration for this browser
		if b.NativeMessagingKey != "" && manifestPath != "" {
			if val, ok := getRegDefaultString(b.NativeMessagingKey); ok && strings.EqualFold(strings.TrimSpace(val), manifestPath) {
				b.NativeRegistered = true
			}
		}
	}
	return browsers
}

// IntegrateBrowsers registers the Native Messaging host manifest and HKCU registry key
// for each selected browser, and optionally opens the browser's extensions page so the
// user can enable/load the unpacked extension immediately.
func IntegrateBrowsers(selected []BrowserInfo, hostBinaryPath, extensionID string, openBrowserUI bool) []BrowserInstallResult {
	if strings.TrimSpace(hostBinaryPath) == "" {
		if resolved, err := ResolveHostBinary(); err == nil {
			hostBinaryPath = resolved
		}
	}
	manifestPath, err := DefaultManifestPath()
	var manifestErr error
	if err != nil {
		manifestErr = err
	} else {
		m, buildErr := BuildManifest(hostBinaryPath, extensionID)
		if buildErr != nil {
			manifestErr = buildErr
		} else {
			manifestErr = WriteManifest(manifestPath, m)
		}
	}

	extDir := ResolveExtensionDir()
	results := make([]BrowserInstallResult, 0, len(selected))
	for _, b := range selected {
		res := BrowserInstallResult{
			BrowserID:    b.ID,
			BrowserName:  b.Name,
			ExtensionDir: extDir,
		}
		if manifestErr != nil {
			res.Err = manifestErr
			res.StatusText = "Manifest error: " + manifestErr.Error()
			results = append(results, res)
			continue
		}
		if b.NativeMessagingKey != "" {
			if regErr := setRegDefaultString(b.NativeMessagingKey, manifestPath); regErr != nil {
				res.Err = fmt.Errorf("registry error: %w", regErr)
				res.StatusText = "Failed to register Native Host"
				results = append(results, res)
				continue
			}
			res.NativeHostOK = true
		}

		// Best-effort extension setup: open the browser's extensions page (and pass
		// --load-extension when the local extension folder is available).
		if openBrowserUI && b.Installed && b.ExecutablePath != "" {
			args := []string{}
			if extDir != "" {
				args = append(args, "--load-extension="+extDir)
			}
			if b.ExtensionsURL != "" {
				args = append(args, b.ExtensionsURL)
			}
			cmd := exec.Command(b.ExecutablePath, args...)
			if startErr := cmd.Start(); startErr == nil {
				res.OpenedExtensionsUI = true
			}
		}

		if res.OpenedExtensionsUI {
			res.StatusText = "Native Host registered • Extensions page opened"
		} else if extDir != "" {
			res.StatusText = "Native Host registered • Load unpacked from extension/"
		} else {
			res.StatusText = "Native Host registered"
		}
		results = append(results, res)
	}
	return results
}

func lookupAppPath(exeName string) string {
	subKey := `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + exeName
	for _, root := range []uintptr{hkeyCurrentUser, hkeyLocalMachine} {
		if val, ok := getRegDefaultStringRoot(root, subKey); ok {
			cleaned := strings.Trim(strings.TrimSpace(val), `"`)
			if cleaned != "" {
				return cleaned
			}
		}
	}
	return ""
}

func getRegDefaultStringRoot(root uintptr, subKey string) (string, bool) {
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return "", false
	}
	var hKey uintptr
	r1, _, _ := procRegOpenKeyExW.Call(
		root,
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
		hKey, 0, 0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if r1 != 0 || bufSize == 0 {
		return "", false
	}
	buf := make([]uint16, (bufSize+1)/2)
	r1, _, _ = procRegQueryValueEx.Call(
		hKey, 0, 0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if r1 != 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

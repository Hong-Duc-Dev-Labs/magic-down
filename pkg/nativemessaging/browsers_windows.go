//go:build windows

package nativemessaging

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	hkeyLocalMachine    = uintptr(0x80000002)
	keyEnumerateSubKeys = 0x0008
)

var procRegEnumKeyExW = advapi32.NewProc("RegEnumKeyExW")

// DetectInstalledBrowsers scans standard installation directories, Windows Registry
// App Paths, StartMenuInternet (HKCU/HKLM), and MSIX/WindowsApps aliases to find
// installed browsers and their Native Messaging registration state.
func DetectInstalledBrowsers() []BrowserInfo {
	pf := os.Getenv("ProgramFiles")
	pf86 := os.Getenv("ProgramFiles(x86)")
	localAppData := os.Getenv("LOCALAPPDATA")
	manifestPath, _ := DefaultManifestPath()

	fileExists := func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	}

	browsers := SupportedBrowsers()
	for i := range browsers {
		b := &browsers[i]
		// 1. Check standard filesystem & MSIX/WindowsApps locations
		for _, p := range CandidateBrowserPaths(b.ID, pf, pf86, localAppData) {
			if fileExists(p) {
				b.ExecutablePath = p
				b.Installed = true
				break
			}
		}
		// 2. Fallback to Windows Registry App Paths (HKCU / HKLM)
		// Skip ambiguous exe names shared by multiple browsers (e.g. opera.exe).
		if !b.Installed && b.ExeName != "" && !strings.EqualFold(b.ExeName, "opera.exe") {
			if regPath := lookupAppPath(b.ExeName); regPath != "" && fileExists(regPath) {
				b.ExecutablePath = regPath
				b.Installed = true
			}
		}
	}

	// 3. Dynamically scan HKCU & HKLM StartMenuInternet + WindowsApps aliases
	discovered := scanStartMenuInternet()
	discovered = append(discovered, scanWindowsAppsBrowsers(localAppData)...)
	browsers = MergeDiscoveredBrowsers(browsers, discovered, fileExists)

	// 4. Populate browser version & check Native Messaging registration for every browser
	chromeManifestPath, _ := ManifestPathForChannel("chrome")
	edgeManifestPath, _ := ManifestPathForChannel("edge")
	for i := range browsers {
		b := &browsers[i]
		if b.Installed && b.ExecutablePath != "" && b.Version == "" {
			b.Version = ReadFileVersion(b.ExecutablePath)
		}
		expectedChannelPath := chromeManifestPath
		if StoreChannelForBrowser(b.ID) == "edge" {
			expectedChannelPath = edgeManifestPath
		}
		for _, key := range b.AllNativeMessagingKeys() {
			if val, ok := getRegDefaultString(key); ok {
				v := strings.TrimSpace(val)
				if (expectedChannelPath != "" && strings.EqualFold(v, expectedChannelPath)) ||
					(manifestPath != "" && strings.EqualFold(v, manifestPath)) {
					b.NativeRegistered = true
					break
				}
			}
		}
	}
	return browsers
}

// IntegrateBrowsers registers validated Native Messaging host manifests and HKCU registry keys
// for each selected browser (separating Chrome and Edge origins via ExtensionIDConfig), and
// optionally opens the browser's extensions page.
func IntegrateBrowsers(selected []BrowserInfo, hostBinaryPath, extensionID string, openBrowserUI bool) []BrowserInstallResult {
	if strings.TrimSpace(hostBinaryPath) == "" {
		if resolved, err := ResolveHostBinary(); err == nil {
			hostBinaryPath = resolved
		}
	}
	absBin, absErr := filepath.Abs(strings.TrimSpace(hostBinaryPath))
	if absErr == nil {
		hostBinaryPath = absBin
	}

	cfg, cfgErr := LoadExtensionIDConfig("")
	mode := EffectiveBuildMode()

	extDir := ResolveExtensionDir()
	results := make([]BrowserInstallResult, 0, len(selected))
	for _, b := range selected {
		res := BrowserInstallResult{
			BrowserID:    b.ID,
			BrowserName:  b.Name,
			ExtensionDir: extDir,
		}

		channel := StoreChannelForBrowser(b.ID)
		var manifestPath string
		var m Manifest
		var manifestErr error

		if strings.TrimSpace(extensionID) != "" {
			manifestPath, manifestErr = DefaultManifestPath()
			if manifestErr == nil {
				m, manifestErr = BuildManifest(hostBinaryPath, extensionID)
			}
		} else if cfgErr != nil {
			manifestErr = cfgErr
		} else {
			manifestPath, manifestErr = ManifestPathForChannel(channel)
			if manifestErr == nil {
				m, manifestErr = BuildManifestForBrowser(hostBinaryPath, b.ID, mode, cfg)
			}
		}

		if manifestErr == nil {
			manifestErr = ValidateManifest(m)
		}
		if manifestErr == nil {
			manifestErr = WriteManifest(manifestPath, m)
		}
		if manifestErr != nil {
			LogEvent("ERROR", "manifest_validate", map[string]any{"browser": b.ID, "error": manifestErr.Error()})
			res.Err = manifestErr
			res.StatusText = "Manifest error: " + manifestErr.Error()
			results = append(results, res)
			continue
		}

		keys := b.AllNativeMessagingKeys()
		if len(keys) > 0 {
			var regErr error
			for _, key := range keys {
				if err := setRegDefaultString(key, manifestPath); err != nil {
					regErr = err
					break
				}
			}
			if regErr != nil {
				LogEvent("ERROR", "registry_register", map[string]any{"browser": b.ID, "error": regErr.Error()})
				res.Err = fmt.Errorf("registry error: %w", regErr)
				res.StatusText = "Failed to register Native Host"
				results = append(results, res)
				continue
			}
			LogEvent("INFO", "registry_register", map[string]any{"browser": b.ID, "channel": channel, "manifest": manifestPath})
			res.NativeHostOK = true
		}

		// Best-effort extension setup: open the browser's extensions page (and pass
		// --load-extension for Chromium browsers when the local extension folder is available).
		if openBrowserUI && b.Installed && b.ExecutablePath != "" {
			args := []string{}
			if b.Family != BrowserFamilyFirefox && extDir != "" {
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

func scanStartMenuInternet() []StartMenuBrowserEntry {
	const baseKey = `SOFTWARE\Clients\StartMenuInternet`
	var entries []StartMenuBrowserEntry
	for _, root := range []uintptr{hkeyCurrentUser, hkeyLocalMachine} {
		subkeys := enumRegSubKeys(root, baseKey)
		for _, sub := range subkeys {
			fullKey := baseKey + `\` + sub
			name, _ := getRegDefaultStringRoot(root, fullKey)
			cmd, ok := getRegDefaultStringRoot(root, fullKey+`\shell\open\command`)
			if !ok || strings.TrimSpace(cmd) == "" {
				continue
			}
			entries = append(entries, StartMenuBrowserEntry{
				Key:         sub,
				DisplayName: strings.TrimSpace(name),
				Command:     cmd,
			})
		}
	}
	return entries
}

func scanWindowsAppsBrowsers(localAppData string) []StartMenuBrowserEntry {
	if strings.TrimSpace(localAppData) == "" {
		return nil
	}
	appsDir := filepath.Join(localAppData, "Microsoft", "WindowsApps")
	knownAliases := []struct {
		exe  string
		name string
	}{
		{exe: "Dia.exe", name: "Dia"},
		{exe: "Arc.exe", name: "Arc"},
	}
	var entries []StartMenuBrowserEntry
	for _, item := range knownAliases {
		p := filepath.Join(appsDir, item.exe)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			entries = append(entries, StartMenuBrowserEntry{
				Key:         item.name,
				DisplayName: item.name,
				Command:     `"` + p + `"`,
			})
		}
	}
	return entries
}

func enumRegSubKeys(root uintptr, subKey string) []string {
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return nil
	}
	var hKey uintptr
	r1, _, _ := procRegOpenKeyExW.Call(
		root,
		uintptr(unsafe.Pointer(subKeyPtr)),
		0,
		keyEnumerateSubKeys|keyQueryValue,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r1 != 0 {
		return nil
	}
	defer procRegCloseKey.Call(hKey)

	var names []string
	buf := make([]uint16, 256)
	for idx := uint32(0); ; idx++ {
		nameLen := uint32(len(buf))
		r1, _, _ := procRegEnumKeyExW.Call(
			hKey,
			uintptr(idx),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&nameLen)),
			0, 0, 0, 0,
		)
		if r1 != 0 {
			break
		}
		names = append(names, syscall.UTF16ToString(buf[:nameLen]))
	}
	return names
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

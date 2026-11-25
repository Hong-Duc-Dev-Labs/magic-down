//go:build windows

package nativemessaging

import (
	"fmt"
	"os"
	"path/filepath"
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
	procRegDeleteKeyW   = advapi32.NewProc("RegDeleteKeyW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

const (
	chromeNativeHostKey = `Software\Google\Chrome\NativeMessagingHosts\` + HostName
	edgeNativeHostKey   = `Software\Microsoft\Edge\NativeMessagingHosts\` + HostName
)

// RegisterHost writes validated per-browser manifests and registers HKCU keys for Chrome and Edge.
// When extensionID is empty, it loads Chrome and Edge IDs independently from ExtensionIDConfig.
func RegisterHost(hostBinaryPath, extensionID string) (string, error) {
	if strings.TrimSpace(hostBinaryPath) == "" {
		resolved, err := ResolveHostBinary()
		if err != nil {
			return "", err
		}
		hostBinaryPath = resolved
	}
	absBin, err := filepath.Abs(strings.TrimSpace(hostBinaryPath))
	if err != nil {
		return "", fmt.Errorf("resolve absolute host binary path: %w", err)
	}

	// Independent Chrome & Edge registration using centralized config when no single override ID is given.
	if strings.TrimSpace(extensionID) == "" {
		cfg, cfgErr := LoadExtensionIDConfig("")
		if cfgErr != nil {
			LogEvent("ERROR", "host_install", map[string]any{"error": cfgErr.Error()})
			return "", cfgErr
		}
		mode := EffectiveBuildMode()
		chromeManifestPath, err := registerChannel(absBin, "chrome", mode, cfg, []string{chromeNativeHostKey})
		if err != nil {
			return "", fmt.Errorf("chrome registration: %w", err)
		}
		if _, err := registerChannel(absBin, "edge", mode, cfg, []string{edgeNativeHostKey}); err != nil {
			return "", fmt.Errorf("edge registration: %w", err)
		}
		LogEvent("INFO", "host_install", map[string]any{
			"mode":          mode,
			"host_binary":   absBin,
			"chrome_target": chromeManifestPath,
		})
		return chromeManifestPath, nil
	}

	manifestPath, err := DefaultManifestPath()
	if err != nil {
		return "", err
	}
	m, err := BuildManifest(absBin, extensionID)
	if err != nil {
		return "", err
	}
	if err := ValidateManifest(m); err != nil {
		LogEvent("ERROR", "manifest_validate", map[string]any{"path": manifestPath, "error": err.Error()})
		return "", err
	}
	LogEvent("INFO", "manifest_validate", map[string]any{"path": manifestPath, "origins": len(m.AllowedOrigins)})
	if err := WriteManifest(manifestPath, m); err != nil {
		return "", err
	}
	if err := setRegDefaultString(chromeNativeHostKey, manifestPath); err != nil {
		return "", fmt.Errorf("chrome registry: %w", err)
	}
	if err := setRegDefaultString(edgeNativeHostKey, manifestPath); err != nil {
		return "", fmt.Errorf("edge registry: %w", err)
	}
	LogEvent("INFO", "registry_register", map[string]any{
		"manifest": manifestPath,
		"keys":     []string{chromeNativeHostKey, edgeNativeHostKey},
	})
	return manifestPath, nil
}

func registerChannel(absBin, channel, mode string, cfg ExtensionIDConfig, regKeys []string) (string, error) {
	manifestPath, err := ManifestPathForChannel(channel)
	if err != nil {
		return "", err
	}
	m, err := BuildManifestForBrowser(absBin, channel, mode, cfg)
	if err != nil {
		return "", err
	}
	if err := ValidateManifest(m); err != nil {
		LogEvent("ERROR", "manifest_validate", map[string]any{"channel": channel, "path": manifestPath, "error": err.Error()})
		return "", err
	}
	LogEvent("INFO", "manifest_validate", map[string]any{"channel": channel, "path": manifestPath, "origins": len(m.AllowedOrigins)})
	if err := WriteManifest(manifestPath, m); err != nil {
		return "", err
	}
	for _, key := range regKeys {
		if err := setRegDefaultString(key, manifestPath); err != nil {
			LogEvent("ERROR", "registry_register", map[string]any{"channel": channel, "key": key, "error": err.Error()})
			return "", err
		}
	}
	LogEvent("INFO", "registry_register", map[string]any{"channel": channel, "manifest": manifestPath, "keys": regKeys})
	return manifestPath, nil
}

// UninstallNativeHost removes ONLY MagicDown's Native Messaging HKCU registry keys
// and MagicDown-owned manifest files, without touching user config or download data.
func UninstallNativeHost() error {
	seen := make(map[string]struct{})
	var keys []string
	addKey := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" || !strings.HasSuffix(strings.ToLower(k), `\`+strings.ToLower(HostName)) {
			return
		}
		lk := strings.ToLower(k)
		if _, ok := seen[lk]; ok {
			return
		}
		seen[lk] = struct{}{}
		keys = append(keys, k)
	}
	addKey(chromeNativeHostKey)
	addKey(edgeNativeHostKey)
	for _, b := range SupportedBrowsers() {
		for _, k := range b.AllNativeMessagingKeys() {
			addKey(k)
		}
	}

	for _, key := range keys {
		if err := deleteRegKey(key); err != nil {
			LogEvent("ERROR", "registry_unregister", map[string]any{"key": key, "error": err.Error()})
			return fmt.Errorf("delete registry key %q: %w", key, err)
		}
	}
	removed, err := RemoveManagedManifests("")
	if err != nil {
		LogEvent("ERROR", "registry_unregister", map[string]any{"error": err.Error()})
		return err
	}
	LogEvent("INFO", "registry_unregister", map[string]any{
		"removed_manifests": removed,
		"checked_keys":      len(keys),
	})
	return nil
}

func deleteRegKey(subKey string) error {
	subKeyPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return err
	}
	r1, _, _ := procRegDeleteKeyW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(subKeyPtr)),
	)
	if r1 != 0 && syscall.Errno(r1) != errFileNotFound {
		return syscall.Errno(r1)
	}
	return nil
}

// CheckRegistration inspects the manifest file(s) and Chrome/Edge HKCU registry keys.
func CheckRegistration() RegistrationStatus {
	chromePath, _ := ManifestPathForChannel("chrome")
	edgePath, _ := ManifestPathForChannel("edge")
	legacyPath, _ := DefaultManifestPath()

	exists := func(p string) bool {
		if p == "" {
			return false
		}
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	}

	st := RegistrationStatus{ManifestPath: chromePath}
	if exists(chromePath) || exists(edgePath) || exists(legacyPath) {
		st.ManifestExists = true
		if !exists(chromePath) && exists(legacyPath) {
			st.ManifestPath = legacyPath
		}
	}
	if val, ok := getRegDefaultString(chromeNativeHostKey); ok {
		v := strings.TrimSpace(val)
		if (chromePath != "" && strings.EqualFold(v, chromePath)) || (legacyPath != "" && strings.EqualFold(v, legacyPath)) {
			st.ChromeReady = true
		}
	}
	if val, ok := getRegDefaultString(edgeNativeHostKey); ok {
		v := strings.TrimSpace(val)
		if (edgePath != "" && strings.EqualFold(v, edgePath)) || (legacyPath != "" && strings.EqualFold(v, legacyPath)) {
			st.EdgeReady = true
		}
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

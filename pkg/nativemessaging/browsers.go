package nativemessaging

import (
	"os"
	"path/filepath"
	"strings"
)

// BrowserInfo describes a supported Chromium-based browser on Windows and its integration state.
type BrowserInfo struct {
	ID                 string
	Name               string
	ExeName            string
	ExecutablePath     string
	Installed          bool
	NativeMessagingKey string
	ExternalExtKey     string
	ExtensionsURL      string
	NativeRegistered   bool
}

// BrowserInstallResult reports the outcome of integrating MagicDown with a single browser.
type BrowserInstallResult struct {
	BrowserID          string
	BrowserName        string
	NativeHostOK       bool
	OpenedExtensionsUI bool
	ExtensionDir       string
	StatusText         string
	Err                error
}

// SupportedBrowsers returns the catalog of Chromium-based browsers MagicDown supports.
func SupportedBrowsers() []BrowserInfo {
	return []BrowserInfo{
		{
			ID:                 "chrome",
			Name:               "Google Chrome",
			ExeName:            "chrome.exe",
			NativeMessagingKey: `Software\Google\Chrome\NativeMessagingHosts\` + HostName,
			ExternalExtKey:     `Software\Google\Chrome\Extensions\` + DefaultExtensionID,
			ExtensionsURL:      "chrome://extensions",
		},
		{
			ID:                 "edge",
			Name:               "Microsoft Edge",
			ExeName:            "msedge.exe",
			NativeMessagingKey: `Software\Microsoft\Edge\NativeMessagingHosts\` + HostName,
			ExternalExtKey:     `Software\Microsoft\Edge\Extensions\` + DefaultExtensionID,
			ExtensionsURL:      "edge://extensions",
		},
		{
			ID:                 "brave",
			Name:               "Brave",
			ExeName:            "brave.exe",
			NativeMessagingKey: `Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\` + HostName,
			ExternalExtKey:     `Software\BraveSoftware\Brave-Browser\Extensions\` + DefaultExtensionID,
			ExtensionsURL:      "brave://extensions",
		},
		{
			ID:                 "vivaldi",
			Name:               "Vivaldi",
			ExeName:            "vivaldi.exe",
			NativeMessagingKey: `Software\Vivaldi\NativeMessagingHosts\` + HostName,
			ExternalExtKey:     `Software\Vivaldi\Extensions\` + DefaultExtensionID,
			ExtensionsURL:      "vivaldi://extensions",
		},
		{
			ID:                 "opera",
			Name:               "Opera",
			ExeName:            "opera.exe",
			NativeMessagingKey: `Software\Opera Software\Opera Stable\NativeMessagingHosts\` + HostName,
			ExternalExtKey:     `Software\Opera Software\Opera Stable\Extensions\` + DefaultExtensionID,
			ExtensionsURL:      "opera://extensions",
		},
		{
			ID:                 "coccoc",
			Name:               "Cốc Cốc",
			ExeName:            "browser.exe",
			NativeMessagingKey: `Software\CocCoc\Browser\NativeMessagingHosts\` + HostName,
			ExternalExtKey:     `Software\CocCoc\Browser\Extensions\` + DefaultExtensionID,
			ExtensionsURL:      "coccoc://extensions",
		},
	}
}

// ResolveExtensionDir locates the unpacked MagicDown browser extension folder
// (containing manifest.json) next to the running executable or current working directory.
func ResolveExtensionDir() string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "extension"),
			filepath.Join(filepath.Dir(exeDir), "extension"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "extension"))
	}
	for _, dir := range candidates {
		if info, err := os.Stat(filepath.Join(dir, "manifest.json")); err == nil && !info.IsDir() {
			return dir
		}
	}
	return ""
}

// CandidateBrowserPaths returns well-known Windows installation paths for a browser ID
// given standard environment directories (ProgramFiles, ProgramFiles(x86), LocalAppData).
func CandidateBrowserPaths(browserID, programFiles, programFilesX86, localAppData string) []string {
	var out []string
	add := func(base string, rel ...string) {
		if strings.TrimSpace(base) == "" {
			return
		}
		parts := append([]string{base}, rel...)
		out = append(out, filepath.Join(parts...))
	}
	switch browserID {
	case "chrome":
		add(programFiles, "Google", "Chrome", "Application", "chrome.exe")
		add(programFilesX86, "Google", "Chrome", "Application", "chrome.exe")
		add(localAppData, "Google", "Chrome", "Application", "chrome.exe")
	case "edge":
		add(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe")
		add(programFiles, "Microsoft", "Edge", "Application", "msedge.exe")
		add(localAppData, "Microsoft", "Edge", "Application", "msedge.exe")
	case "brave":
		add(programFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe")
		add(programFilesX86, "BraveSoftware", "Brave-Browser", "Application", "brave.exe")
		add(localAppData, "BraveSoftware", "Brave-Browser", "Application", "brave.exe")
	case "vivaldi":
		add(localAppData, "Vivaldi", "Application", "vivaldi.exe")
		add(programFiles, "Vivaldi", "Application", "vivaldi.exe")
		add(programFilesX86, "Vivaldi", "Application", "vivaldi.exe")
	case "opera":
		add(localAppData, "Programs", "Opera", "opera.exe")
		add(localAppData, "Programs", "Opera GX", "opera.exe")
		add(programFiles, "Opera", "opera.exe")
	case "coccoc":
		add(programFiles, "CocCoc", "Browser", "Application", "browser.exe")
		add(programFilesX86, "CocCoc", "Browser", "Application", "browser.exe")
		add(localAppData, "CocCoc", "Browser", "Application", "browser.exe")
	}
	return out
}

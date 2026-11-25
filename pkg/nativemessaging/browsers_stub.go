//go:build !windows

package nativemessaging

// DetectInstalledBrowsers returns the catalog with Installed=false on non-Windows platforms.
func DetectInstalledBrowsers() []BrowserInfo {
	return SupportedBrowsers()
}

// IntegrateBrowsers is a no-op stub on non-Windows platforms.
func IntegrateBrowsers(selected []BrowserInfo, hostBinaryPath, extensionID string, openBrowserUI bool) []BrowserInstallResult {
	results := make([]BrowserInstallResult, 0, len(selected))
	for _, b := range selected {
		results = append(results, BrowserInstallResult{
			BrowserID:   b.ID,
			BrowserName: b.Name,
			StatusText:  "Browser integration is only supported on Windows",
		})
	}
	return results
}

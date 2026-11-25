//go:build !windows

package notify

// NotifyDownloadComplete is a no-op on platforms without a built-in notifier.
func NotifyDownloadComplete(filename string) {}

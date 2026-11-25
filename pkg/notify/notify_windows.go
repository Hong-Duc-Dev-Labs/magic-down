//go:build windows

// Package notify raises lightweight desktop notifications without cgo or extra
// dependencies: a hidden PowerShell process shows a toast and falls back to a
// tray balloon when Windows refuses the toast.
package notify

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

// NotifyDownloadComplete shows a non-blocking Windows notification naming the file.
func NotifyDownloadComplete(filename string) {
	script := buildScript(filename)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		return
	}
	go func() { _ = cmd.Wait() }()
}

func buildScript(filename string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		name = "Download"
	}
	name = strings.ReplaceAll(name, "\r", " ")
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "'", "''")

	return fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$title = 'MagicDown'
$body = 'Download complete: %s'
try {
  [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
  $t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
  $texts = $t.GetElementsByTagName('text')
  [void]$texts.Item(0).AppendChild($t.CreateTextNode($title))
  [void]$texts.Item(1).AppendChild($t.CreateTextNode($body))
  $toast = [Windows.UI.Notifications.ToastNotification]::new($t)
  [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe').Show($toast)
} catch {
  Add-Type -AssemblyName System.Windows.Forms
  Add-Type -AssemblyName System.Drawing
  $ni = New-Object System.Windows.Forms.NotifyIcon
  $ni.Icon = [System.Drawing.SystemIcons]::Information
  $ni.Visible = $true
  $ni.ShowBalloonTip(5000, $title, $body, [System.Windows.Forms.ToolTipIcon]::Info)
  Start-Sleep -Seconds 6
  $ni.Dispose()
}`, name)
}

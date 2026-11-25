//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

// BuildApplyScript generates a PowerShell script that waits for the running
// magicdown.exe PID to exit, stops magicdown-host.exe if needed, copies staged
// binaries and extension files into installDir, cleans up temp files, and
// relaunches magicdown.exe.
func BuildApplyScript(stagingDir, installDir string, currentPID int, relaunchExe string) string {
	esc := func(s string) string {
		return strings.ReplaceAll(s, "'", "''")
	}

	var b strings.Builder
	b.WriteString("$ErrorActionPreference = 'SilentlyContinue'\n")
	b.WriteString(fmt.Sprintf("$TargetPid = %d\n", currentPID))
	b.WriteString(fmt.Sprintf("$StagingDir = '%s'\n", esc(stagingDir)))
	b.WriteString(fmt.Sprintf("$InstallDir = '%s'\n", esc(installDir)))
	b.WriteString(fmt.Sprintf("$RelaunchExe = '%s'\n", esc(relaunchExe)))
	b.WriteString(`
# Wait up to 30 seconds for the current magicdown.exe process to exit
for ($i = 0; $i -lt 150; $i++) {
    if (-not (Get-Process -Id $TargetPid -ErrorAction SilentlyContinue)) {
        break
    }
    Start-Sleep -Milliseconds 200
}

# Stop magicdown-host.exe if running so it can be replaced cleanly
Get-Process -Name 'magicdown-host' -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Milliseconds 250

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

foreach ($bin in @('magicdown.exe', 'magicdown-host.exe', 'magicdown.ico')) {
    $src = Join-Path $StagingDir $bin
    if (Test-Path $src) {
        Copy-Item -Path $src -Destination (Join-Path $InstallDir $bin) -Force
    }
}

$srcExt = Join-Path $StagingDir 'extension'
if (Test-Path $srcExt) {
    $destExt = Join-Path $InstallDir 'extension'
    if (Test-Path $destExt) {
        Remove-Item -Path $destExt -Recurse -Force
    }
    Copy-Item -Path $srcExt -Destination $destExt -Recurse -Force
}

# Clean up staging directory
if (Test-Path $StagingDir) {
    Remove-Item -Path $StagingDir -Recurse -Force
}

# Relaunch MagicDown
if ($RelaunchExe -and (Test-Path $RelaunchExe)) {
    Start-Process -FilePath $RelaunchExe -WorkingDirectory $InstallDir
}

# Self-delete this helper script
Remove-Item -Path $MyInvocation.MyCommand.Path -Force
`)
	return b.String()
}

// LaunchApplyScript writes the PowerShell apply script to a temp file and
// launches it hidden (CREATE_NO_WINDOW) so it outlives the exiting process.
func LaunchApplyScript(stagingDir, installDir string, currentPID int, relaunchExe string) (string, error) {
	scriptContent := BuildApplyScript(stagingDir, installDir, currentPID, relaunchExe)
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("magicdown-apply-%d.ps1", currentPID))
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0644); err != nil {
		return "", fmt.Errorf("write apply script: %w", err)
	}

	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-File", scriptPath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	if err := cmd.Start(); err != nil {
		return scriptPath, fmt.Errorf("start apply script: %w", err)
	}
	return scriptPath, nil
}

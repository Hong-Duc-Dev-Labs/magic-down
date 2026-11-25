<#
.SYNOPSIS
    Uninstalls MagicDown from the current Windows user profile.
    - Stops running MagicDown processes.
    - Removes Startup entry, Native Messaging host registration, Start Menu & Desktop shortcuts,
      Apps & Features Uninstall entry, and %LOCALAPPDATA%\Programs\MagicDown.
    - Pass -RemoveData to also remove saved config and download history in %APPDATA%\MagicDown.
#>
param(
    [switch]$RemoveData
)

$ErrorActionPreference = "Stop"

$AppName = "MagicDown"
$HostName = "com.magicdown.downloader"
$InstallDir = Join-Path $env:LOCALAPPDATA "Programs\MagicDown"

# 1. Stop running processes
Get-Process -Name "magicdown", "magicdown-host" -ErrorAction SilentlyContinue | ForEach-Object {
    try { $_.CloseMainWindow() | Out-Null } catch {}
}
Start-Sleep -Milliseconds 400
Get-Process -Name "magicdown", "magicdown-host" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

# 2. Remove Windows Startup (Autostart) entry
$RunKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
if (Test-Path $RunKey) {
    Remove-ItemProperty -Path $RunKey -Name $AppName -ErrorAction SilentlyContinue
}

# 3. Unregister ONLY MagicDown's Native Messaging host keys and manifests
foreach ($RegPath in @(
    "HKCU:\Software\Google\Chrome\NativeMessagingHosts\$HostName",
    "HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\$HostName",
    "HKCU:\Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\$HostName",
    "HKCU:\Software\Vivaldi\NativeMessagingHosts\$HostName",
    "HKCU:\Software\Opera Software\Opera Stable\NativeMessagingHosts\$HostName",
    "HKCU:\Software\Opera Software\Opera GX Stable\NativeMessagingHosts\$HostName",
    "HKCU:\Software\CocCoc\Browser\NativeMessagingHosts\$HostName",
    "HKCU:\Software\TheBrowserCompany\Dia\NativeMessagingHosts\$HostName",
    "HKCU:\Software\ArcBrowser\Arc\NativeMessagingHosts\$HostName",
    "HKCU:\Software\Chromium\NativeMessagingHosts\$HostName"
)) {
    if (Test-Path $RegPath) {
        Remove-Item -Path $RegPath -Recurse -Force
    }
}
$LocalMagicDownDir = Join-Path $env:LOCALAPPDATA "MagicDown"
foreach ($mfName in @("$HostName.json", "$HostName.chrome.json", "$HostName.edge.json")) {
    $ManifestPath = Join-Path $LocalMagicDownDir $mfName
    if (Test-Path $ManifestPath) {
        Remove-Item -Path $ManifestPath -Force
    }
}

# 4. Remove Start Menu & Desktop shortcuts
$StartMenuShortcut = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\MagicDown.lnk"
if (Test-Path $StartMenuShortcut) {
    Remove-Item -Path $StartMenuShortcut -Force
}
$DesktopDir = [Environment]::GetFolderPath("Desktop")
if ($DesktopDir) {
    $DesktopShortcut = Join-Path $DesktopDir "MagicDown.lnk"
    if (Test-Path $DesktopShortcut) {
        Remove-Item -Path $DesktopShortcut -Force
    }
}

# 5. Remove Apps & Features Uninstall entry
$UninstallRegPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$AppName"
if (Test-Path $UninstallRegPath) {
    Remove-Item -Path $UninstallRegPath -Recurse -Force
}

# 6. Remove installed binaries
if (Test-Path $InstallDir) {
    Remove-Item -Path $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
}

# 7. Optionally remove user config/state
if ($RemoveData) {
    $AppDataDir = Join-Path $env:APPDATA "MagicDown"
    if (Test-Path $AppDataDir) {
        Remove-Item -Path $AppDataDir -Recurse -Force
    }
}

Write-Host "MagicDown uninstalled successfully." -ForegroundColor Yellow

<#
.SYNOPSIS
    Installs MagicDown for the current Windows user into %LOCALAPPDATA%\Programs\MagicDown.
    - Builds magicdown.exe (-H=windowsgui) and magicdown-host.exe unless -SkipBuild is passed.
    - Creates Start Menu and Desktop shortcuts.
    - Registers Chrome/Edge Native Messaging host pointing to the installed magicdown-host.exe.
    - Registers an Apps & Features Uninstall entry in HKCU\...\Uninstall\MagicDown.
    - Optionally enables Launch at Windows Startup (-EnableAutostart).
#>
param(
    [Parameter(Mandatory = $false)]
    [ValidatePattern("^[a-p]{32}$")]
    [string]$ExtensionId = "cndchhhjghchjigecgkjclembafgjaje",

    [switch]$SkipBuild,
    [switch]$NoDesktopShortcut,
    [switch]$EnableAutostart,
    [switch]$LaunchAfterInstall
)

$ErrorActionPreference = "Stop"

$AppName = "MagicDown"
$AppVersion = "1.0.0"
$Publisher = "MagicDown"
$HostName = "com.magicdown.downloader"
$RepoRoot = Split-Path -Parent $PSScriptRoot
$DistDir = Join-Path $RepoRoot "dist"
$InstallDir = Join-Path $env:LOCALAPPDATA "Programs\MagicDown"

if (-not $SkipBuild) {
    & (Join-Path $PSScriptRoot "build-release.ps1") -OutDir $DistDir
}

$SrcGuiExe = Join-Path $DistDir "magicdown.exe"
$SrcHostExe = Join-Path $DistDir "magicdown-host.exe"
$SrcIcon = Join-Path $RepoRoot "cmd\magicdown\magicdown.ico"

if (-not (Test-Path $SrcGuiExe)) {
    throw "magicdown.exe not found at $SrcGuiExe. Run without -SkipBuild or build first."
}
if (-not (Test-Path $SrcHostExe)) {
    throw "magicdown-host.exe not found at $SrcHostExe. Run without -SkipBuild or build first."
}

# Stop running instances before overwriting binaries
Get-Process -Name "magicdown", "magicdown-host" -ErrorAction SilentlyContinue | ForEach-Object {
    try { $_.CloseMainWindow() | Out-Null } catch {}
}
Start-Sleep -Milliseconds 400
Get-Process -Name "magicdown", "magicdown-host" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue

# Copy files into %LOCALAPPDATA%\Programs\MagicDown
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$InstalledGuiExe = Join-Path $InstallDir "magicdown.exe"
$InstalledHostExe = Join-Path $InstallDir "magicdown-host.exe"
$InstalledIcon = Join-Path $InstallDir "magicdown.ico"
$InstalledUninstall = Join-Path $InstallDir "uninstall.ps1"

Copy-Item -Path $SrcGuiExe -Destination $InstalledGuiExe -Force
Copy-Item -Path $SrcHostExe -Destination $InstalledHostExe -Force
if (Test-Path $SrcIcon) {
    Copy-Item -Path $SrcIcon -Destination $InstalledIcon -Force
}
Copy-Item -Path (Join-Path $PSScriptRoot "uninstall.ps1") -Destination $InstalledUninstall -Force

# Bundle ffmpeg.exe if present in repo root or dist/
foreach ($ffCandidate in @((Join-Path $DistDir "ffmpeg.exe"), (Join-Path $RepoRoot "ffmpeg.exe"))) {
    if (Test-Path $ffCandidate) {
        Copy-Item -Path $ffCandidate -Destination (Join-Path $InstallDir "ffmpeg.exe") -Force
        break
    }
}

# Create Start Menu & Desktop shortcuts
$WshShell = New-Object -ComObject WScript.Shell

$StartMenuShortcut = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\MagicDown.lnk"
$sc = $WshShell.CreateShortcut($StartMenuShortcut)
$sc.TargetPath = $InstalledGuiExe
$sc.WorkingDirectory = $InstallDir
$sc.Description = "MagicDown — Multi-Segment Download Manager"
if (Test-Path $InstalledIcon) { $sc.IconLocation = $InstalledIcon }
$sc.Save()

if (-not $NoDesktopShortcut) {
    $DesktopDir = [Environment]::GetFolderPath("Desktop")
    if ($DesktopDir) {
        $DesktopShortcut = Join-Path $DesktopDir "MagicDown.lnk"
        $dsc = $WshShell.CreateShortcut($DesktopShortcut)
        $dsc.TargetPath = $InstalledGuiExe
        $dsc.WorkingDirectory = $InstallDir
        $dsc.Description = "MagicDown — Multi-Segment Download Manager"
        if (Test-Path $InstalledIcon) { $dsc.IconLocation = $InstalledIcon }
        $dsc.Save()
    }
}

# Register Chrome & Edge Native Messaging host pointing to installed magicdown-host.exe
$ManifestDir = Join-Path $env:LOCALAPPDATA "MagicDown"
New-Item -ItemType Directory -Force -Path $ManifestDir | Out-Null
$ManifestPath = Join-Path $ManifestDir "$HostName.json"
$Manifest = [ordered]@{
    name            = $HostName
    description     = "MagicDown Native Messaging Host"
    path            = $InstalledHostExe
    type            = "stdio"
    allowed_origins = @(
        "chrome-extension://$ExtensionId/"
    )
}
$Manifest | ConvertTo-Json -Depth 4 | Set-Content -Path $ManifestPath -Encoding UTF8

foreach ($RegPath in @(
    "HKCU:\Software\Google\Chrome\NativeMessagingHosts\$HostName",
    "HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\$HostName"
)) {
    New-Item -Path $RegPath -Force | Out-Null
    Set-ItemProperty -Path $RegPath -Name "(Default)" -Value $ManifestPath
}

# Optional Autostart registration
if ($EnableAutostart) {
    $RunKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
    New-Item -Path $RunKey -Force | Out-Null
    Set-ItemProperty -Path $RunKey -Name $AppName -Value "`"$InstalledGuiExe`" --minimized"
}

# Register Windows Apps & Features Uninstall entry
$UninstallRegPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$AppName"
New-Item -Path $UninstallRegPath -Force | Out-Null
$UninstallCmd = "powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$InstalledUninstall`""
Set-ItemProperty -Path $UninstallRegPath -Name "DisplayName" -Value $AppName
Set-ItemProperty -Path $UninstallRegPath -Name "DisplayVersion" -Value $AppVersion
Set-ItemProperty -Path $UninstallRegPath -Name "Publisher" -Value $Publisher
Set-ItemProperty -Path $UninstallRegPath -Name "InstallLocation" -Value $InstallDir
Set-ItemProperty -Path $UninstallRegPath -Name "DisplayIcon" -Value $(if (Test-Path $InstalledIcon) { $InstalledIcon } else { $InstalledGuiExe })
Set-ItemProperty -Path $UninstallRegPath -Name "UninstallString" -Value $UninstallCmd
Set-ItemProperty -Path $UninstallRegPath -Name "NoModify" -Value 1 -Type DWord
Set-ItemProperty -Path $UninstallRegPath -Name "NoRepair" -Value 1 -Type DWord

Write-Host ""
Write-Host "MagicDown installed successfully!" -ForegroundColor Green
Write-Host "  Install Dir      : $InstallDir"
Write-Host "  Start Menu       : $StartMenuShortcut"
Write-Host "  Native Messaging : $ManifestPath -> $InstalledHostExe"
Write-Host "  Uninstall Entry  : $UninstallRegPath"

if ($LaunchAfterInstall) {
    Start-Process -FilePath $InstalledGuiExe
}

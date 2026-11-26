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

    [string]$Version = "0.1.0",
    [string]$GitHubRepo = "",

    [switch]$SkipBuild,
    [switch]$NoDesktopShortcut,
    [switch]$EnableAutostart,
    [switch]$LaunchAfterInstall
)

$ErrorActionPreference = "Stop"

$AppName = "MagicDown"
$AppVersion = $Version.TrimStart("v", "V")
$Publisher = "MagicDown"
$HostName = "com.magicdown.downloader"
$RepoRoot = Split-Path -Parent $PSScriptRoot
$DistDir = Join-Path $RepoRoot "dist"
$InstallDir = Join-Path $env:LOCALAPPDATA "Programs\MagicDown"

if (-not $SkipBuild) {
    $buildArgs = @{
        OutDir  = $DistDir
        Version = $AppVersion
        SkipZip = $true
    }
    if ($GitHubRepo) {
        $buildArgs["GitHubRepo"] = $GitHubRepo
    }
    & (Join-Path $PSScriptRoot "build-release.ps1") @buildArgs
}

$SrcGuiExe = if (Test-Path (Join-Path $PSScriptRoot "magicdown.exe")) { Join-Path $PSScriptRoot "magicdown.exe" } else { Join-Path $DistDir "magicdown.exe" }
$SrcHostExe = if (Test-Path (Join-Path $PSScriptRoot "magicdown-host.exe")) { Join-Path $PSScriptRoot "magicdown-host.exe" } else { Join-Path $DistDir "magicdown-host.exe" }
$SrcIcon = if (Test-Path (Join-Path $PSScriptRoot "magicdown.ico")) { Join-Path $PSScriptRoot "magicdown.ico" } else { Join-Path $RepoRoot "cmd\magicdown\magicdown.ico" }

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

# Bundle browser extension folder (keeping manifest.json "key", excluding *.test.js)
$SrcExtDir = if (Test-Path (Join-Path $PSScriptRoot "extension\manifest.json")) {
    Join-Path $PSScriptRoot "extension"
} else {
    Join-Path $RepoRoot "extension"
}
if (Test-Path $SrcExtDir) {
    $DestExtDir = Join-Path $InstallDir "extension"
    if (Test-Path $DestExtDir) {
        Remove-Item -Path $DestExtDir -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $DestExtDir | Out-Null
    Get-ChildItem -Path $SrcExtDir -Recurse -File | Where-Object {
        $_.Name -notmatch '\.test\.js$' -and $_.Name -ne '.DS_Store' -and $_.Name -ne 'Thumbs.db' -and $_.Extension -notin @('.map', '.log')
    } | ForEach-Object {
        $relPath = $_.FullName.Substring($SrcExtDir.Length).TrimStart('\', '/')
        $destFile = Join-Path $DestExtDir $relPath
        $destParent = Split-Path -Parent $destFile
        if (-not (Test-Path $destParent)) {
            New-Item -ItemType Directory -Force -Path $destParent | Out-Null
        }
        Copy-Item -Path $_.FullName -Destination $destFile -Force
    }
    $installedManifest = Get-Content -Path (Join-Path $DestExtDir "manifest.json") -Raw | ConvertFrom-Json
    if (-not $installedManifest.key) {
        throw "Installed extension/manifest.json is missing required 'key' field"
    }
}

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

# Register Chrome & Edge Native Messaging hosts independently using config/extension-ids.json
$InstalledHostExeAbs = (Resolve-Path $InstalledHostExe).Path
if (-not (Test-Path -LiteralPath $InstalledHostExeAbs -PathType Leaf)) {
    throw "Installed magicdown-host.exe not found at $InstalledHostExeAbs"
}

$ChromeExtId = $ExtensionId
$EdgeExtId = $ExtensionId
$ExtIdsConfigPath = if (Test-Path (Join-Path $PSScriptRoot "config\extension-ids.json")) {
    Join-Path $PSScriptRoot "config\extension-ids.json"
} else {
    Join-Path $RepoRoot "config\extension-ids.json"
}
if (Test-Path $ExtIdsConfigPath) {
    $ExtIdsCfg = Get-Content -Path $ExtIdsConfigPath -Raw | ConvertFrom-Json
    if ($env:MAGICDOWN_BUILD_MODE -eq "prod") {
        if ($env:MAGICDOWN_CHROME_PROD_EXT_ID) { $ChromeExtId = $env:MAGICDOWN_CHROME_PROD_EXT_ID }
        elseif ($ExtIdsCfg.browsers.chrome.prod) { $ChromeExtId = $ExtIdsCfg.browsers.chrome.prod }
        if ($env:MAGICDOWN_EDGE_PROD_EXT_ID) { $EdgeExtId = $env:MAGICDOWN_EDGE_PROD_EXT_ID }
        elseif ($ExtIdsCfg.browsers.edge.prod) { $EdgeExtId = $ExtIdsCfg.browsers.edge.prod }
    } else {
        if ($env:MAGICDOWN_CHROME_DEV_EXT_ID) { $ChromeExtId = $env:MAGICDOWN_CHROME_DEV_EXT_ID }
        elseif ($ExtIdsCfg.browsers.chrome.dev) { $ChromeExtId = $ExtIdsCfg.browsers.chrome.dev }
        if ($env:MAGICDOWN_EDGE_DEV_EXT_ID) { $EdgeExtId = $env:MAGICDOWN_EDGE_DEV_EXT_ID }
        elseif ($ExtIdsCfg.browsers.edge.dev) { $EdgeExtId = $ExtIdsCfg.browsers.edge.dev }
    }
}

$ManifestDir = Join-Path $env:LOCALAPPDATA "MagicDown"
New-Item -ItemType Directory -Force -Path $ManifestDir | Out-Null

$ChannelSpecs = @(
    @{ Channel = "chrome"; ExtId = $ChromeExtId; RegPath = "HKCU:\Software\Google\Chrome\NativeMessagingHosts\$HostName" },
    @{ Channel = "edge";   ExtId = $EdgeExtId;   RegPath = "HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\$HostName" }
)

$ManifestPath = Join-Path $ManifestDir "$HostName.chrome.json"
foreach ($spec in $ChannelSpecs) {
    if ($spec.ExtId -notmatch "^[a-p]{32}$") {
        throw "Invalid Extension ID for $($spec.Channel): '$($spec.ExtId)'"
    }
    $chManifestPath = Join-Path $ManifestDir "$HostName.$($spec.Channel).json"
    $Manifest = [ordered]@{
        name            = $HostName
        description     = "MagicDown Native Messaging Host"
        path            = $InstalledHostExeAbs
        type            = "stdio"
        allowed_origins = @(
            "chrome-extension://$($spec.ExtId)/"
        )
    }
    $Manifest | ConvertTo-Json -Depth 4 | Set-Content -Path $chManifestPath -Encoding UTF8
    # Validate written manifest JSON before updating Registry
    $null = Get-Content -Path $chManifestPath -Raw | ConvertFrom-Json
    New-Item -Path $spec.RegPath -Force | Out-Null
    Set-ItemProperty -Path $spec.RegPath -Name "(Default)" -Value $chManifestPath
}

# Also register additional Chromium browsers (Brave, Vivaldi, Opera, Opera GX, CocCoc, Dia, Arc)
$ExtraChromiumRegKeys = @(
    @{ Parent = "HKCU:\Software\BraveSoftware\Brave-Browser"; Key = "HKCU:\Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\$HostName" },
    @{ Parent = "HKCU:\Software\Vivaldi";                     Key = "HKCU:\Software\Vivaldi\NativeMessagingHosts\$HostName" },
    @{ Parent = "HKCU:\Software\Opera Software";              Key = "HKCU:\Software\Opera Software\Opera Stable\NativeMessagingHosts\$HostName" },
    @{ Parent = "HKCU:\Software\Opera Software";              Key = "HKCU:\Software\Opera Software\Opera GX Stable\NativeMessagingHosts\$HostName" },
    @{ Parent = "HKCU:\Software\CocCoc\Browser";              Key = "HKCU:\Software\CocCoc\Browser\NativeMessagingHosts\$HostName" },
    @{ Parent = "HKCU:\Software\TheBrowserCompany\Dia";       Key = "HKCU:\Software\TheBrowserCompany\Dia\NativeMessagingHosts\$HostName" },
    @{ Parent = "HKCU:\Software\ArcBrowser\Arc";              Key = "HKCU:\Software\ArcBrowser\Arc\NativeMessagingHosts\$HostName" }
)
foreach ($extra in $ExtraChromiumRegKeys) {
    if (Test-Path $extra.Parent) {
        New-Item -Path $extra.Key -Force | Out-Null
        Set-ItemProperty -Path $extra.Key -Name "(Default)" -Value $ManifestPath
    }
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

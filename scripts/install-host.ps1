<#
.SYNOPSIS
    Registers the MagicDown Native Messaging host manifest in Windows Registry (HKCU)
    for Google Chrome and Microsoft Edge.

.PARAMETER ExtensionId
    The Chrome/Edge extension ID shown in chrome://extensions or edge://extensions
    after loading the unpacked extension/ folder.

.PARAMETER HostBinaryPath
    Optional path to magicdown-host.exe. Defaults to building .\magicdown-host.exe
    in the repository root if not provided.
#>
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern("^[a-p]{32}$")]
    [string]$ExtensionId,

    [Parameter(Mandatory = $false)]
    [string]$HostBinaryPath
)

$ErrorActionPreference = "Stop"

$HostName = "com.magicdown.downloader"
$RepoRoot = Split-Path -Parent $PSScriptRoot

if (-not $HostBinaryPath) {
    $HostBinaryPath = Join-Path $RepoRoot "magicdown-host.exe"
    Write-Host "Building magicdown-host.exe..."
    Push-Location $RepoRoot
    try {
        go build -o $HostBinaryPath ./cmd/magicdown-host
    } finally {
        Pop-Location
    }
}

$ResolvedBinary = (Resolve-Path $HostBinaryPath).Path

$InstallDir = Join-Path $env:LOCALAPPDATA "MagicDown"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$ManifestPath = Join-Path $InstallDir "$HostName.json"
$Manifest = [ordered]@{
    name            = $HostName
    description     = "MagicDown Native Messaging Host"
    path            = $ResolvedBinary
    type            = "stdio"
    allowed_origins = @(
        "chrome-extension://$ExtensionId/"
    )
}

$Manifest | ConvertTo-Json -Depth 4 | Set-Content -Path $ManifestPath -Encoding UTF8

$RegPaths = @(
    "HKCU:\Software\Google\Chrome\NativeMessagingHosts\$HostName",
    "HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\$HostName"
)

foreach ($RegPath in $RegPaths) {
    New-Item -Path $RegPath -Force | Out-Null
    Set-ItemProperty -Path $RegPath -Name "(Default)" -Value $ManifestPath
    Write-Host "Registered: $RegPath -> $ManifestPath"
}

Write-Host ""
Write-Host "MagicDown Native Messaging Host installed successfully!" -ForegroundColor Green
Write-Host "Manifest : $ManifestPath"
Write-Host "Binary   : $ResolvedBinary"
Write-Host "Extension: chrome-extension://$ExtensionId/"

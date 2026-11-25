<#
.SYNOPSIS
    Unregisters the MagicDown Native Messaging host from Windows Registry (HKCU)
    for Google Chrome and Microsoft Edge.
#>
$ErrorActionPreference = "Stop"

$HostName = "com.magicdown.downloader"
$RegPaths = @(
    "HKCU:\Software\Google\Chrome\NativeMessagingHosts\$HostName",
    "HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\$HostName"
)

foreach ($RegPath in $RegPaths) {
    if (Test-Path $RegPath) {
        Remove-Item -Path $RegPath -Recurse -Force
        Write-Host "Removed: $RegPath"
    }
}

$ManifestPath = Join-Path (Join-Path $env:LOCALAPPDATA "MagicDown") "$HostName.json"
if (Test-Path $ManifestPath) {
    Remove-Item -Path $ManifestPath -Force
    Write-Host "Removed: $ManifestPath"
}

Write-Host "MagicDown Native Messaging Host uninstalled." -ForegroundColor Yellow

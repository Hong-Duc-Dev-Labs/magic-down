<#
.SYNOPSIS
    Unregisters the MagicDown Native Messaging host from Windows Registry (HKCU)
    for Google Chrome and Microsoft Edge.
#>
$ErrorActionPreference = "Stop"

$HostName = "com.magicdown.downloader"
$RegPaths = @(
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
)

foreach ($RegPath in $RegPaths) {
    if (Test-Path $RegPath) {
        Remove-Item -Path $RegPath -Recurse -Force
        Write-Host "Removed: $RegPath"
    }
}

$LocalMagicDownDir = Join-Path $env:LOCALAPPDATA "MagicDown"
foreach ($mfName in @("$HostName.json", "$HostName.chrome.json", "$HostName.edge.json")) {
    $ManifestPath = Join-Path $LocalMagicDownDir $mfName
    if (Test-Path $ManifestPath) {
        Remove-Item -Path $ManifestPath -Force
        Write-Host "Removed: $ManifestPath"
    }
}

Write-Host "MagicDown Native Messaging Host uninstalled." -ForegroundColor Yellow

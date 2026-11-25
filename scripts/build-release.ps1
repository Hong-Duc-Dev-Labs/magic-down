<#
.SYNOPSIS
    Builds MagicDown Windows release binaries into dist\.
    - magicdown.exe is built with -H=windowsgui so it never opens a console window.
    - magicdown-host.exe is built for Chrome/Edge Native Messaging stdio transport.
#>
param(
    [string]$OutDir
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
if (-not $OutDir) {
    $OutDir = Join-Path $RepoRoot "dist"
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$GuiExe = Join-Path $OutDir "magicdown.exe"
$HostExe = Join-Path $OutDir "magicdown-host.exe"

Push-Location $RepoRoot
try {
    Write-Host "Building magicdown.exe (Windows GUI subsystem, no console)..." -ForegroundColor Cyan
    go build -trimpath -ldflags="-s -w -H=windowsgui" -o $GuiExe ./cmd/magicdown

    Write-Host "Building magicdown-host.exe (Native Messaging stdio host)..." -ForegroundColor Cyan
    go build -trimpath -ldflags="-s -w" -o $HostExe ./cmd/magicdown-host
} finally {
    Pop-Location
}

$IconSrc = Join-Path $RepoRoot "cmd\magicdown\magicdown.ico"
if (Test-Path $IconSrc) {
    Copy-Item -Path $IconSrc -Destination (Join-Path $OutDir "magicdown.ico") -Force
}

Write-Host ""
Write-Host "Build complete!" -ForegroundColor Green
Write-Host "  GUI  : $GuiExe"
Write-Host "  Host : $HostExe"

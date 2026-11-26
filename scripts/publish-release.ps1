<#
.SYNOPSIS
    Builds MagicDown Windows release artifacts locally and publishes a GitHub Release
    using the `gh` CLI (no GitHub Actions required).
.EXAMPLE
    .\scripts\publish-release.ps1 -Version 0.2.0
    .\scripts\publish-release.ps1 -Version v0.2.0 -Draft -Notes "Release notes"
#>
param(
    [Parameter(Mandatory = $true)]
    [string]$Version,

    [string]$GitHubRepo = "Hong-Duc-Dev-Labs/magic-down",
    [ValidateSet("dev", "prod")]
    [string]$Mode = "dev",
    [string]$Notes = "",
    [switch]$Draft,
    [switch]$Prerelease
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$DistDir = Join-Path $RepoRoot "dist"
$CleanVersion = $Version.TrimStart("v", "V")
$Tag = "v$CleanVersion"

if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
    throw "GitHub CLI (gh) is required to publish releases."
}

Write-Host "Building release $Tag (mode=$Mode) for $GitHubRepo..." -ForegroundColor Cyan
& (Join-Path $PSScriptRoot "build-release.ps1") -OutDir $DistDir -Version $CleanVersion -GitHubRepo $GitHubRepo -Mode $Mode

$ZipPath = Join-Path $DistDir "MagicDown-windows-amd64.zip"
$SetupExe = Join-Path $DistDir "MagicDown-Setup-v$CleanVersion.exe"
$GuiExe = Join-Path $DistDir "magicdown.exe"
$ChecksumsPath = Join-Path $DistDir "checksums.txt"

foreach ($file in @($ZipPath, $GuiExe, $ChecksumsPath)) {
    if (-not (Test-Path $file)) {
        throw "Expected release artifact not found: $file"
    }
}

$assets = @($ZipPath, $GuiExe, $ChecksumsPath)
if (Test-Path $SetupExe) {
    $assets += $SetupExe
}

$ghArgs = @(
    "release", "create", $Tag
) + $assets + @(
    "--repo", $GitHubRepo,
    "--title", "MagicDown $Tag"
)

if ($Notes) {
    $ghArgs += @("--notes", $Notes)
} else {
    $ghArgs += "--generate-notes"
}
if ($Draft) {
    $ghArgs += "--draft"
}
if ($Prerelease) {
    $ghArgs += "--prerelease"
}

Write-Host "Publishing GitHub Release $Tag to $GitHubRepo..." -ForegroundColor Cyan
& gh @ghArgs
if ($LASTEXITCODE -ne 0) {
    throw "gh release create failed with exit code $LASTEXITCODE"
}

Write-Host "Published $Tag to https://github.com/$GitHubRepo/releases/tag/$Tag" -ForegroundColor Green

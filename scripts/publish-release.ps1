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

    [string]$GitHubRepo = "Hong-Duc-Dev-Labs/MagicDown",
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

Write-Host "Building release $Tag for $GitHubRepo..." -ForegroundColor Cyan
& (Join-Path $PSScriptRoot "build-release.ps1") -OutDir $DistDir -Version $CleanVersion -GitHubRepo $GitHubRepo

$ZipPath = Join-Path $DistDir "MagicDown-windows-amd64.zip"
$GuiExe = Join-Path $DistDir "magicdown.exe"
$ChecksumsPath = Join-Path $DistDir "checksums.txt"

foreach ($file in @($ZipPath, $GuiExe, $ChecksumsPath)) {
    if (-not (Test-Path $file)) {
        throw "Expected release artifact not found: $file"
    }
}

$ghArgs = @(
    "release", "create", $Tag,
    $ZipPath,
    $GuiExe,
    $ChecksumsPath,
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

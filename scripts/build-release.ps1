<#
.SYNOPSIS
    Builds MagicDown Windows release binaries, packages MagicDown-windows-amd64.zip,
    and generates checksums.txt compatible with pkg/updater.
    - magicdown.exe is built with -H=windowsgui so it never opens a console window.
    - magicdown-host.exe is built for Chrome/Edge Native Messaging stdio transport.
#>
param(
    [string]$OutDir,
    [string]$Version,
    [string]$GitHubRepo,
    [switch]$SkipZip
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
if (-not $OutDir) {
    $OutDir = Join-Path $RepoRoot "dist"
}

Push-Location $RepoRoot
try {
    if (-not $Version) {
        $tag = git describe --tags --exact-match 2>$null
        if ($LASTEXITCODE -eq 0 -and $tag) {
            $Version = $tag.Trim()
        } else {
            $Version = "0.1.0"
        }
    }
    $Version = $Version.TrimStart("v", "V")

    $Commit = git rev-parse --short HEAD 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $Commit) {
        $Commit = "dev"
    } else {
        $Commit = $Commit.Trim()
    }

    $BuildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    if (-not $GitHubRepo) {
        $remoteUrl = git remote get-url origin 2>$null
        if ($LASTEXITCODE -eq 0 -and $remoteUrl -match "github\.com[:/]([^/]+/[^/\s]+?)(?:\.git)?$") {
            $GitHubRepo = $Matches[1]
        } else {
            $GitHubRepo = "hongducdev/MagicDown"
        }
    }

    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

    $GuiExe = Join-Path $OutDir "magicdown.exe"
    $HostExe = Join-Path $OutDir "magicdown-host.exe"

    $VersionLdFlags = "-X magicdown/pkg/version.Version=$Version -X magicdown/pkg/version.Commit=$Commit -X magicdown/pkg/version.BuildDate=$BuildDate -X magicdown/pkg/version.GitHubRepo=$GitHubRepo"

    Write-Host "Building MagicDown v$Version ($Commit) for $GitHubRepo..." -ForegroundColor Cyan
    Write-Host "  Building magicdown.exe (Windows GUI subsystem, no console)..." -ForegroundColor Cyan
    go build -trimpath -ldflags="-s -w -H=windowsgui $VersionLdFlags" -o $GuiExe ./cmd/magicdown
    if ($LASTEXITCODE -ne 0) { throw "go build magicdown.exe failed" }

    Write-Host "  Building magicdown-host.exe (Native Messaging stdio host)..." -ForegroundColor Cyan
    go build -trimpath -ldflags="-s -w $VersionLdFlags" -o $HostExe ./cmd/magicdown-host
    if ($LASTEXITCODE -ne 0) { throw "go build magicdown-host.exe failed" }
} finally {
    Pop-Location
}

$IconSrc = Join-Path $RepoRoot "cmd\magicdown\magicdown.ico"
if (Test-Path $IconSrc) {
    Copy-Item -Path $IconSrc -Destination (Join-Path $OutDir "magicdown.ico") -Force
}

if (-not $SkipZip) {
    $StageDir = Join-Path $OutDir "_release_stage"
    if (Test-Path $StageDir) {
        Remove-Item -Path $StageDir -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $StageDir | Out-Null

    Copy-Item -Path $GuiExe -Destination (Join-Path $StageDir "magicdown.exe") -Force
    Copy-Item -Path $HostExe -Destination (Join-Path $StageDir "magicdown-host.exe") -Force
    if (Test-Path $IconSrc) {
        Copy-Item -Path $IconSrc -Destination (Join-Path $StageDir "magicdown.ico") -Force
    }
    $ExtSrc = Join-Path $RepoRoot "extension"
    if (Test-Path $ExtSrc) {
        Copy-Item -Path $ExtSrc -Destination (Join-Path $StageDir "extension") -Recurse -Force
    }

    $ZipPath = Join-Path $OutDir "MagicDown-windows-amd64.zip"
    if (Test-Path $ZipPath) {
        Remove-Item -Path $ZipPath -Force
    }
    Compress-Archive -Path (Join-Path $StageDir "*") -DestinationPath $ZipPath -Force
    Remove-Item -Path $StageDir -Recurse -Force

    # Generate sha256sum-compatible checksums.txt
    $ChecksumsPath = Join-Path $OutDir "checksums.txt"
    $lines = @()
    foreach ($artifact in @($ZipPath, $GuiExe)) {
        if (Test-Path $artifact) {
            $hash = (Get-FileHash -Path $artifact -Algorithm SHA256).Hash.ToLowerInvariant()
            $name = Split-Path -Leaf $artifact
            $lines += "$hash  $name"
        }
    }
    Set-Content -Path $ChecksumsPath -Value $lines -Encoding ASCII

    Write-Host ""
    Write-Host "Release package complete!" -ForegroundColor Green
    Write-Host "  Version   : v$Version ($Commit)"
    Write-Host "  Repo      : $GitHubRepo"
    Write-Host "  GUI       : $GuiExe"
    Write-Host "  Host      : $HostExe"
    Write-Host "  Archive   : $ZipPath"
    Write-Host "  Checksums : $ChecksumsPath"
} else {
    Write-Host ""
    Write-Host "Build complete!" -ForegroundColor Green
    Write-Host "  Version : v$Version ($Commit)"
    Write-Host "  Repo    : $GitHubRepo"
    Write-Host "  GUI     : $GuiExe"
    Write-Host "  Host    : $HostExe"
}

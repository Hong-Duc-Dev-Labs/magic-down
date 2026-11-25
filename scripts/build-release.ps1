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
    [ValidateSet("dev", "prod")]
    [string]$Mode = "dev",
    [string]$ChromeProdExtId = "",
    [string]$EdgeProdExtId = "",
    [string]$ChromeStoreUrl = "",
    [string]$EdgeStoreUrl = "",
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
            $GitHubRepo = "Hong-Duc-Dev-Labs/magic-down"
        }
    }

    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

    $GuiExe = Join-Path $OutDir "magicdown.exe"
    $HostExe = Join-Path $OutDir "magicdown-host.exe"

    $ExtConfigPath = Join-Path $RepoRoot "config\extension-ids.json"
    if (Test-Path $ExtConfigPath) {
        $extCfg = Get-Content -Path $ExtConfigPath -Raw | ConvertFrom-Json
        if (-not $ChromeProdExtId) { $ChromeProdExtId = if ($env:MAGICDOWN_CHROME_PROD_EXT_ID) { $env:MAGICDOWN_CHROME_PROD_EXT_ID } else { $extCfg.browsers.chrome.prod } }
        if (-not $EdgeProdExtId)   { $EdgeProdExtId   = if ($env:MAGICDOWN_EDGE_PROD_EXT_ID)   { $env:MAGICDOWN_EDGE_PROD_EXT_ID }   else { $extCfg.browsers.edge.prod } }
        if (-not $ChromeStoreUrl)  { $ChromeStoreUrl  = if ($env:MAGICDOWN_CHROME_STORE_URL)   { $env:MAGICDOWN_CHROME_STORE_URL }   else { $extCfg.browsers.chrome.store_url } }
        if (-not $EdgeStoreUrl)    { $EdgeStoreUrl    = if ($env:MAGICDOWN_EDGE_STORE_URL)     { $env:MAGICDOWN_EDGE_STORE_URL }     else { $extCfg.browsers.edge.store_url } }
    }

    $VersionLdFlags = "-X magicdown/pkg/version.Version=$Version -X magicdown/pkg/version.Commit=$Commit -X magicdown/pkg/version.BuildDate=$BuildDate -X magicdown/pkg/version.GitHubRepo=$GitHubRepo -X magicdown/pkg/nativemessaging.BuildMode=$Mode"
    if ($ChromeProdExtId) { $VersionLdFlags += " -X magicdown/pkg/nativemessaging.ChromeProdExtensionID=$ChromeProdExtId" }
    if ($EdgeProdExtId)   { $VersionLdFlags += " -X magicdown/pkg/nativemessaging.EdgeProdExtensionID=$EdgeProdExtId" }
    if ($ChromeStoreUrl)  { $VersionLdFlags += " -X magicdown/pkg/nativemessaging.ChromeStoreURL=$ChromeStoreUrl" }
    if ($EdgeStoreUrl)    { $VersionLdFlags += " -X magicdown/pkg/nativemessaging.EdgeStoreURL=$EdgeStoreUrl" }

    Write-Host "Building MagicDown v$Version ($Commit, mode=$Mode) for $GitHubRepo..." -ForegroundColor Cyan
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

# Copy install/uninstall scripts & extension-ids config into OutDir
foreach ($scriptName in @("install.ps1", "uninstall.ps1")) {
    $srcScript = Join-Path $PSScriptRoot $scriptName
    if (Test-Path $srcScript) {
        Copy-Item -Path $srcScript -Destination (Join-Path $OutDir $scriptName) -Force
    }
}
$ConfigOutDir = Join-Path $OutDir "config"
New-Item -ItemType Directory -Force -Path $ConfigOutDir | Out-Null
if (Test-Path (Join-Path $RepoRoot "config\extension-ids.json")) {
    Copy-Item -Path (Join-Path $RepoRoot "config\extension-ids.json") -Destination (Join-Path $ConfigOutDir "extension-ids.json") -Force
}

# Package clean Chrome & Edge Store extension ZIPs
& (Join-Path $PSScriptRoot "package-extension.ps1") -OutDir $OutDir -Version $Version

# Generate reference Native Messaging manifests for the selected mode
$ManifestOutDir = Join-Path $OutDir "manifests"
New-Item -ItemType Directory -Force -Path $ManifestOutDir | Out-Null
$HostName = "com.magicdown.downloader"
$extCfgFinal = Get-Content -Path (Join-Path $RepoRoot "config\extension-ids.json") -Raw | ConvertFrom-Json
$chromeIdForManifest = if ($Mode -eq "prod") { $ChromeProdExtId } else { $extCfgFinal.browsers.chrome.dev }
$edgeIdForManifest   = if ($Mode -eq "prod") { $EdgeProdExtId }   else { $extCfgFinal.browsers.edge.dev }
foreach ($pair in @(
    @{ Channel = "chrome"; ExtId = $chromeIdForManifest },
    @{ Channel = "edge";   ExtId = $edgeIdForManifest }
)) {
    if ($pair.ExtId -match "^[a-p]{32}$") {
        $mfObj = [ordered]@{
            name            = $HostName
            description     = "MagicDown Native Messaging Host"
            path            = $HostExe
            type            = "stdio"
            allowed_origins = @("chrome-extension://$($pair.ExtId)/")
        }
        $mfObj | ConvertTo-Json -Depth 4 | Set-Content -Path (Join-Path $ManifestOutDir "$HostName.$($pair.Channel).json") -Encoding UTF8
    }
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
    Copy-Item -Path (Join-Path $OutDir "install.ps1") -Destination (Join-Path $StageDir "install.ps1") -Force
    Copy-Item -Path (Join-Path $OutDir "uninstall.ps1") -Destination (Join-Path $StageDir "uninstall.ps1") -Force
    Copy-Item -Path $ConfigOutDir -Destination (Join-Path $StageDir "config") -Recurse -Force

    $ZipPath = Join-Path $OutDir "MagicDown-windows-amd64.zip"
    if (Test-Path $ZipPath) {
        Remove-Item -Path $ZipPath -Force
    }
    Compress-Archive -Path (Join-Path $StageDir "*") -DestinationPath $ZipPath -Force
    Remove-Item -Path $StageDir -Recurse -Force

    # Generate sha256sum-compatible checksums.txt
    $ChecksumsPath = Join-Path $OutDir "checksums.txt"
    $ChromeExtZip = Join-Path $OutDir "MagicDown-extension-chrome.zip"
    $EdgeExtZip = Join-Path $OutDir "MagicDown-extension-edge.zip"
    $lines = @()
    foreach ($artifact in @($ZipPath, $GuiExe, $HostExe, $ChromeExtZip, $EdgeExtZip)) {
        if (Test-Path $artifact) {
            $hash = (Get-FileHash -Path $artifact -Algorithm SHA256).Hash.ToLowerInvariant()
            $name = Split-Path -Leaf $artifact
            $lines += "$hash  $name"
        }
    }
    Set-Content -Path $ChecksumsPath -Value $lines -Encoding ASCII

    Write-Host ""
    Write-Host "Release package complete!" -ForegroundColor Green
    Write-Host "  Version   : v$Version ($Commit, mode=$Mode)"
    Write-Host "  Repo      : $GitHubRepo"
    Write-Host "  GUI       : $GuiExe"
    Write-Host "  Host      : $HostExe"
    Write-Host "  Archive   : $ZipPath"
    Write-Host "  ChromeExt : $ChromeExtZip"
    Write-Host "  EdgeExt   : $EdgeExtZip"
    Write-Host "  Checksums : $ChecksumsPath"
} else {
    Write-Host ""
    Write-Host "Build complete!" -ForegroundColor Green
    Write-Host "  Version : v$Version ($Commit, mode=$Mode)"
    Write-Host "  Repo    : $GitHubRepo"
    Write-Host "  GUI     : $GuiExe"
    Write-Host "  Host    : $HostExe"
}

<#
.SYNOPSIS
    Packages MagicDown MV3 browser extension for Chrome Web Store (Unlisted)
    and Microsoft Edge Add-ons, and verifies both output ZIP archives.

.DESCRIPTION
    - Produces:
        <OutDir>\MagicDown-extension-chrome.zip
        <OutDir>\MagicDown-extension-edge.zip
    - Includes ONLY runtime extension files; excludes *.test.js, docs, debug files, etc.
    - Strips "key" from manifest.json in production Store ZIPs (per config/extension-ids.json)
      because Chrome Web Store and Microsoft Edge Add-ons sign packages with their own keys.
    - Verifies each ZIP: valid MV3 manifest, no "key" in store build, no test/dev files,
      valid permissions, and version synchronized with pkg/version/version.go.
#>
param(
    [string]$OutDir,
    [string]$Version,
    [string]$ConfigPath,
    [switch]$KeepKey,
    [switch]$SkipVersionSyncCheck
)

$ErrorActionPreference = "Stop"

Add-Type -AssemblyName System.IO.Compression.FileSystem

$RepoRoot = Split-Path -Parent $PSScriptRoot
$ExtSrcDir = Join-Path $RepoRoot "extension"
if (-not $OutDir) {
    $OutDir = Join-Path $RepoRoot "dist"
}
if (-not $ConfigPath) {
    $ConfigPath = Join-Path $RepoRoot "config\extension-ids.json"
}

if (-not (Test-Path (Join-Path $ExtSrcDir "manifest.json"))) {
    throw "extension/manifest.json not found at $ExtSrcDir"
}

# Read app version from pkg/version/version.go for version sync verification
$AppVersion = ""
$VersionGoPath = Join-Path $RepoRoot "pkg\version\version.go"
if (Test-Path $VersionGoPath) {
    $verContent = Get-Content -Path $VersionGoPath -Raw
    if ($verContent -match 'Version\s*=\s*"([^"]+)"') {
        $AppVersion = $Matches[1].Trim().TrimStart("v", "V")
    }
}

$ExpectedVersion = if ($Version) { $Version.Trim().TrimStart("v", "V") } else { $AppVersion }

# Load centralized extension-ids.json
$StripKeyInProd = $true
if (Test-Path $ConfigPath) {
    $ExtConfig = Get-Content -Path $ConfigPath -Raw | ConvertFrom-Json
    if ($null -ne $ExtConfig.packaging -and $null -ne $ExtConfig.packaging.strip_key_in_prod) {
        $StripKeyInProd = [bool]$ExtConfig.packaging.strip_key_in_prod
    }
}
if ($KeepKey) {
    $StripKeyInProd = $false
}

# Explicit allowlist of runtime files/directories (strictly excludes *.test.js, README, debug files)
$RuntimeFiles = @(
    "background.js",
    "batch-picker.css",
    "batch-picker.html",
    "batch-picker.js",
    "batch-scanner.js",
    "bilibili.js",
    "content.js",
    "i18n.js",
    "inject-main.js",
    "popup.css",
    "popup.html",
    "popup.js",
    "thumbnail.js",
    "youtube.js"
)
$RuntimeDirs = @("icons")

$AllowedPermissions = @(
    "alarms",
    "downloads",
    "nativeMessaging",
    "storage",
    "contextMenus",
    "cookies",
    "activeTab",
    "webRequest"
)

function New-StoreExtensionZip {
    param(
        [string]$TargetBrowser,
        [string]$ZipPath
    )

    $StageDir = Join-Path $OutDir "_ext_stage_$TargetBrowser"
    if (Test-Path $StageDir) {
        Remove-Item -Path $StageDir -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $StageDir | Out-Null

    # Copy allowed runtime files
    foreach ($file in $RuntimeFiles) {
        $src = Join-Path $ExtSrcDir $file
        if (-not (Test-Path $src)) {
            throw "Required extension runtime file missing: $src"
        }
        Copy-Item -Path $src -Destination (Join-Path $StageDir $file) -Force
    }

    # Copy allowed runtime directories (icons)
    foreach ($dir in $RuntimeDirs) {
        $srcDir = Join-Path $ExtSrcDir $dir
        if (-not (Test-Path $srcDir)) {
            throw "Required extension directory missing: $srcDir"
        }
        $dstDir = Join-Path $StageDir $dir
        New-Item -ItemType Directory -Force -Path $dstDir | Out-Null
        Get-ChildItem -Path $srcDir -File | Where-Object {
            $_.Name -notmatch '^\.' -and $_.Name -ne 'Thumbs.db' -and $_.Extension -in @('.png', '.svg', '.ico')
        } | ForEach-Object {
            Copy-Item -Path $_.FullName -Destination (Join-Path $dstDir $_.Name) -Force
        }
    }

    # Build sanitized manifest.json for store submission
    $ManifestRaw = Get-Content -Path (Join-Path $ExtSrcDir "manifest.json") -Raw
    $ManifestObj = $ManifestRaw | ConvertFrom-Json

    if ($Version) {
        $ManifestObj.version = $ExpectedVersion
    }

    if ($StripKeyInProd -and ($ManifestObj.PSObject.Properties.Name -contains "key")) {
        $ManifestObj.PSObject.Properties.Remove("key")
    }

    $ManifestJson = $ManifestObj | ConvertTo-Json -Depth 10
    [System.IO.File]::WriteAllText((Join-Path $StageDir "manifest.json"), $ManifestJson, (New-Object System.Text.UTF8Encoding($false)))

    if (Test-Path $ZipPath) {
        Remove-Item -Path $ZipPath -Force
    }
    [System.IO.Compression.ZipFile]::CreateFromDirectory($StageDir, $ZipPath)
    Remove-Item -Path $StageDir -Recurse -Force
}

function Test-ExtensionZip {
    param(
        [string]$ZipPath,
        [string]$TargetBrowser
    )

    if (-not (Test-Path $ZipPath)) {
        throw "[$TargetBrowser] Expected ZIP not found: $ZipPath"
    }

    $zip = [System.IO.Compression.ZipFile]::OpenRead($ZipPath)
    try {
        $entryNames = @($zip.Entries | ForEach-Object { $_.FullName -replace '\\', '/' })

        # 1. Forbidden files check
        foreach ($name in $entryNames) {
            if ($name -match '\.test\.js$' -or $name -match '(^|/)README' -or $name -match '\.DS_Store$' -or $name -match 'Thumbs\.db$' -or $name -match '\.map$' -or $name -match '\.log$') {
                throw "[$TargetBrowser] Forbidden dev/test file found in ZIP: $name"
            }
        }

        # 2. manifest.json presence & validity
        $manifestEntry = $zip.Entries | Where-Object { ($_.FullName -replace '\\', '/') -eq "manifest.json" } | Select-Object -First 1
        if (-not $manifestEntry) {
            throw "[$TargetBrowser] manifest.json missing from ZIP root"
        }

        $reader = New-Object System.IO.StreamReader($manifestEntry.Open())
        try {
            $manifestText = $reader.ReadToEnd()
        } finally {
            $reader.Dispose()
        }

        $m = $manifestText | ConvertFrom-Json
        if ($m.manifest_version -ne 3) {
            throw "[$TargetBrowser] manifest_version must be 3, got $($m.manifest_version)"
        }
        if ($StripKeyInProd -and ($m.PSObject.Properties.Name -contains "key")) {
            throw "[$TargetBrowser] production Store ZIP must not contain 'key' in manifest.json"
        }

        # 3. Permissions validation
        if (-not $m.permissions -or $m.permissions.Count -eq 0) {
            throw "[$TargetBrowser] manifest permissions cannot be empty"
        }
        foreach ($perm in $m.permissions) {
            if ($AllowedPermissions -notcontains $perm) {
                throw "[$TargetBrowser] unexpected permission in manifest: $perm"
            }
        }
        if ($m.host_permissions -contains "<all_urls>") {
            throw "[$TargetBrowser] host_permissions should use explicit http://*/* and https://*/* instead of <all_urls>"
        }

        # 4. Version sync check
        if (-not $SkipVersionSyncCheck -and $ExpectedVersion -and ($m.version -ne $ExpectedVersion)) {
            throw "[$TargetBrowser] manifest version ($($m.version)) does not match app version ($ExpectedVersion)"
        }

        Write-Host "  [OK] $TargetBrowser verified: $($entryNames.Count) files, MV$($m.manifest_version), v$($m.version), key_stripped=$StripKeyInProd" -ForegroundColor Green
    } finally {
        $zip.Dispose()
    }
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$ChromeZip = Join-Path $OutDir "MagicDown-extension-chrome.zip"
$EdgeZip = Join-Path $OutDir "MagicDown-extension-edge.zip"

Write-Host "Packaging MagicDown extensions into $OutDir..." -ForegroundColor Cyan
New-StoreExtensionZip -TargetBrowser "chrome" -ZipPath $ChromeZip
New-StoreExtensionZip -TargetBrowser "edge" -ZipPath $EdgeZip

Write-Host "Verifying packaged extension archives..." -ForegroundColor Cyan
Test-ExtensionZip -ZipPath $ChromeZip -TargetBrowser "chrome"
Test-ExtensionZip -ZipPath $EdgeZip -TargetBrowser "edge"

Write-Host ""
Write-Host "Extension packaging complete!" -ForegroundColor Green
Write-Host "  Chrome Store ZIP : $ChromeZip"
Write-Host "  Edge Add-ons ZIP : $EdgeZip"

<#
.SYNOPSIS
    Verifies a built MagicDown release directory (dist/) before publishing.

.DESCRIPTION
    Checks:
    1. Required release artifacts exist (magicdown.exe, magicdown-host.exe,
       MagicDown-windows-amd64.zip, MagicDown-extension-chrome.zip,
       MagicDown-extension-edge.zip, checksums.txt, install.ps1, uninstall.ps1).
    2. SHA-256 checksums in checksums.txt match actual files.
    3. Chrome & Edge extension ZIPs contain valid MV3 manifest.json, stripped "key",
       no *.test.js/debug/README/.DS_Store files, valid permissions, and synced version.
    4. Extension IDs & Native Messaging manifests are valid; -StrictProd fails if
       production Chrome/Edge Extension IDs are not configured.
#>
param(
    [string]$DistDir,
    [ValidateSet("dev", "prod")]
    [string]$Mode = "dev",
    [switch]$StrictProd,
    [string]$ConfigPath
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.IO.Compression.FileSystem

$RepoRoot = Split-Path -Parent $PSScriptRoot
if (-not $DistDir) { $DistDir = Join-Path $RepoRoot "dist" }
if (-not $ConfigPath) { $ConfigPath = Join-Path $RepoRoot "config\extension-ids.json" }

Write-Host "Verifying MagicDown release in $DistDir (mode=$Mode, strictProd=$StrictProd)..." -ForegroundColor Cyan

# 1. Required artifacts
$RequiredFiles = @(
    "magicdown.exe",
    "magicdown-host.exe",
    "MagicDown-windows-amd64.zip",
    "MagicDown-extension-chrome.zip",
    "MagicDown-extension-edge.zip",
    "checksums.txt",
    "install.ps1",
    "uninstall.ps1"
)
foreach ($f in $RequiredFiles) {
    $p = Join-Path $DistDir $f
    if (-not (Test-Path -LiteralPath $p -PathType Leaf)) {
        throw "Missing required release artifact: $p"
    }
}
Write-Host "  [OK] All required release artifacts exist." -ForegroundColor Green

# 2. Checksums verification
$ChecksumsPath = Join-Path $DistDir "checksums.txt"
$checksumLines = Get-Content -Path $ChecksumsPath | Where-Object { $_.Trim() -ne "" }
if ($checksumLines.Count -eq 0) {
    throw "checksums.txt is empty"
}
foreach ($line in $checksumLines) {
    if ($line -notmatch '^([0-9a-f]{64})\s+(.+)$') {
        throw "Malformed checksum line: $line"
    }
    $expectedHash = $Matches[1]
    $fileName = $Matches[2].Trim()
    $targetPath = Join-Path $DistDir $fileName
    if (-not (Test-Path $targetPath)) {
        throw "Checksum references missing file: $fileName"
    }
    $actualHash = (Get-FileHash -Path $targetPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        throw "Checksum mismatch for ${fileName}: expected $expectedHash, got $actualHash"
    }
}
Write-Host "  [OK] All $($checksumLines.Count) SHA-256 checksums verified." -ForegroundColor Green

# 3. App version from pkg/version/version.go
$AppVersion = ""
$verGo = Join-Path $RepoRoot "pkg\version\version.go"
if (Test-Path $verGo) {
    if ((Get-Content -Path $verGo -Raw) -match 'Version\s*=\s*"([^"]+)"') {
        $AppVersion = $Matches[1].Trim().TrimStart("v", "V")
    }
}

# 4. Verify Chrome & Edge Extension ZIPs
$AllowedPerms = @("downloads", "nativeMessaging", "storage", "contextMenus", "cookies", "activeTab", "webRequest")
foreach ($browser in @("chrome", "edge")) {
    $zipPath = Join-Path $DistDir "MagicDown-extension-$browser.zip"
    $zip = [System.IO.Compression.ZipFile]::OpenRead($zipPath)
    try {
        foreach ($entry in $zip.Entries) {
            $n = $entry.FullName -replace '\\', '/'
            if ($n -match '\.test\.js$' -or $n -match '(^|/)README' -or $n -match '\.DS_Store$' -or $n -match '\.map$' -or $n -match '\.log$') {
                throw "[$browser] Forbidden file in extension ZIP: $n"
            }
        }
        $mfEntry = $zip.Entries | Where-Object { ($_.FullName -replace '\\', '/') -eq "manifest.json" } | Select-Object -First 1
        if (-not $mfEntry) { throw "[$browser] manifest.json missing" }
        $reader = New-Object System.IO.StreamReader($mfEntry.Open())
        try { $m = $reader.ReadToEnd() | ConvertFrom-Json } finally { $reader.Dispose() }
        if ($m.manifest_version -ne 3) { throw "[$browser] manifest_version != 3" }
        if ($m.PSObject.Properties.Name -contains "key") { throw "[$browser] Store ZIP must not contain 'key'" }
        if ($m.host_permissions -contains "<all_urls>") { throw "[$browser] host_permissions must not use <all_urls>" }
        foreach ($perm in $m.permissions) {
            if ($AllowedPerms -notcontains $perm) { throw "[$browser] unexpected permission: $perm" }
        }
        if ($AppVersion -and ($m.version -ne $AppVersion)) {
            throw "[$browser] manifest version ($($m.version)) != app version ($AppVersion)"
        }
    } finally {
        $zip.Dispose()
    }
}
Write-Host "  [OK] Chrome & Edge extension ZIPs verified (MV3, no key, no test files, v$AppVersion synced)." -ForegroundColor Green

# 5. Extension ID config & Native Messaging manifests
$cfg = Get-Content -Path $ConfigPath -Raw | ConvertFrom-Json
if ($Mode -eq "prod") {
    $cProd = if ($env:MAGICDOWN_CHROME_PROD_EXT_ID) { $env:MAGICDOWN_CHROME_PROD_EXT_ID } else { $cfg.browsers.chrome.prod }
    $eProd = if ($env:MAGICDOWN_EDGE_PROD_EXT_ID)   { $env:MAGICDOWN_EDGE_PROD_EXT_ID }   else { $cfg.browsers.edge.prod }
    if (($cProd -notmatch '^[a-p]{32}$') -or ($eProd -notmatch '^[a-p]{32}$')) {
        $msg = "Production Chrome/Edge Extension IDs are not configured yet (chrome='$cProd', edge='$eProd')."
        if ($StrictProd) {
            throw $msg
        } else {
            Write-Host "  [WARN] $msg" -ForegroundColor Yellow
        }
    }
}

$ManifestDir = Join-Path $DistDir "manifests"
if (Test-Path $ManifestDir) {
    Get-ChildItem -Path $ManifestDir -Filter "*.json" | ForEach-Object {
        $nm = Get-Content -Path $_.FullName -Raw | ConvertFrom-Json
        if ($nm.name -ne "com.magicdown.downloader" -or $nm.type -ne "stdio") {
            throw "Invalid Native Messaging manifest: $($_.Name)"
        }
        if (-not (Test-Path -LiteralPath $nm.path -PathType Leaf)) {
            throw "Native Messaging manifest $($_.Name) points to missing host binary: $($nm.path)"
        }
        foreach ($origin in $nm.allowed_origins) {
            if ($origin -notmatch '^chrome-extension://[a-p]{32}/$') {
                throw "Invalid allowed_origin in $($_.Name): $origin"
            }
        }
    }
    Write-Host "  [OK] Native Messaging manifests verified." -ForegroundColor Green
}

Write-Host ""
Write-Host "Release verification PASSED!" -ForegroundColor Green

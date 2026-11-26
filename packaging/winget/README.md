# Winget Packaging for MagicDown (`HongDucDevLabs.MagicDown`)

This directory contains the WinGet 1.6.0 manifest templates for submitting MagicDown to `microsoft/winget-pkgs`.

## Prerequisites
1. Build a Windows release including the Inno Setup installer:
   ```powershell
   .\scripts\build-release.ps1 -Version 0.1.2
   ```
   Requires [Inno Setup 6](https://jrsoftware.org/isinfo.php) (`ISCC.exe`) installed so `dist/MagicDown-Setup-v0.1.2.exe` is compiled and hashed in `dist/checksums.txt`.
2. Publish the GitHub Release so `MagicDown-Setup-v0.1.2.exe` is downloadable at:
   `https://github.com/Hong-Duc-Dev-Labs/magic-down/releases/download/v0.1.2/MagicDown-Setup-v0.1.2.exe`
3. Replace `REPLACE_WITH_RELEASE_SHA256` in `HongDucDevLabs.MagicDown.installer.yaml` with the SHA-256 hash from `dist/checksums.txt`.

## Validate & Submit
```powershell
winget validate --manifest .\packaging\winget
```
Submit the validated manifests to `manifests/h/HongDucDevLabs/MagicDown/0.1.2/` in `microsoft/winget-pkgs`.

# Plan: GitHub Releases Auto-Updater for MagicDown

Date: 2026-11-25
Status: In Progress

## Goal

Add a complete, safe, Windows-native auto-update mechanism for MagicDown powered by GitHub Releases:
1. Centralized version & build metadata (`pkg/version`) injected via `-ldflags`.
2. Self-update engine (`pkg/updater`) with GitHub Releases API polling, semver comparison, Windows asset selection, streaming download with progress callback, SHA256 checksum verification, and safe Windows self-replacement via hidden PowerShell helper (`CREATE_NO_WINDOW`).
3. Settings UI integration in MyGo Native UI (`pkg/gui`) with auto-check toggle, manual "Check for Updates", release notes preview, "Download & Install Update" with progress bar, and automatic restart.
4. Packaging & CI release pipeline (`scripts/build-release.ps1`, `scripts/install.ps1`, `.github/workflows/release.yml`) producing `MagicDown-windows-amd64.zip` and `checksums.txt`.

## Architecture & Files

### 1. Version Management (`pkg/version`)
- `pkg/version/version.go`:
  - `Version` (default `"0.1.0"`), `Commit` (default `"dev"`), `BuildDate` (default `"unknown"`), `GitHubRepo` (default `"hongducdev/MagicDown"`).
  - Helpers: `NormalizedVersion()`, `DisplayVersion()`, `ResolveGitHubRepo(override string)`.

### 2. Updater Package (`pkg/updater`)
- `pkg/updater/semver.go`:
  - `CompareSemver(a, b string) (int, error)` and `IsNewer(current, latest string) bool` supporting `v0.1.0`, `v1.2.3`, and pre-release suffixes.
- `pkg/updater/updater.go`:
  - `ReleaseInfo`, `CheckResult`, `Options`, `Updater`.
  - `CheckForUpdate(ctx)`: queries `GET <APIBaseURL>/repos/<owner>/<repo>/releases/latest` (or `/releases` when `AllowPrerelease` is enabled), skips draft/prerelease by default, selects Windows asset (`MagicDown-windows-amd64.zip` preferred, fallback `.zip` / `.exe`) and `checksums.txt`.
  - `DownloadAsset(ctx, info, destDir, onProgress)`: streams asset to temp file with progress callback `(downloaded, total int64)`.
  - `VerifySHA256(ctx, filePath, assetName, checksumURL)` & `ParseChecksums(r)`: verifies SHA256 digest when `checksums.txt` is present.
  - `StageUpdate(archivePath, stagingDir)`: safely extracts `.zip` (with ZipSlip protection) or copies `.exe` into staging directory.
- `pkg/updater/apply_windows.go` & `pkg/updater/apply_stub.go`:
  - `BuildApplyScript(stagingDir, installDir string, currentPID int, relaunchExe string) string`: generates PowerShell script that waits for `currentPID` to exit, stops `magicdown-host.exe` if running, overwrites `magicdown.exe`, `magicdown-host.exe`, `magicdown.ico`, and `extension/`, cleans up temp/staging files, and relaunches `magicdown.exe`.
  - `LaunchApplyScript(scriptPath string) error`: starts `powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File <script>` with `CREATE_NO_WINDOW`.
- `pkg/updater/updater_test.go`:
  - Full unit tests with `httptest.Server`: semver comparison, pre-release filtering, asset selection, download progress, SHA256 verification (pass & mismatch), zip staging (including ZipSlip rejection), and apply script generation.

### 3. Engine Config & MyGo Settings UI (`pkg/engine`, `pkg/gui`, `cmd/magicdown`)
- `pkg/engine/types.go` & `pkg/engine/engine.go`:
  - Add `AutoCheckUpdates bool` (`json:"auto_check_updates"`, default `true`) and `UpdateRepo string` (`json:"update_repo,omitempty"`) to `Config` and `persistedConfig`.
- `pkg/gui/app.go` & `cmd/magicdown/main.go`:
  - Wire `SetQuitFunc` so the UI can cleanly exit when applying an update.
  - Add background auto-check on startup when `AutoCheckUpdates` is enabled.
  - Add **Software Updates** section in `renderSettingsModal`:
    - Current version badge (`vX.Y.Z`)
    - Auto-check on startup toggle (`Enabled` / `Disabled`)
    - `Check for Updates` button + live status (Checking / Up to date / New version `vX.Y.Z` + release notes summary / Error)
    - `Download & Install Update` button + progress bar + restart notice when an update is available.
- `pkg/gui/settings_test.go`:
  - Add tests for `AutoCheckUpdates` persistence and Settings modal rendering with update states.

### 4. Packaging & GitHub Actions Release
- `scripts/build-release.ps1`:
  - Inject `-ldflags` for `pkg/version` (`Version`, `Commit`, `BuildDate`, `GitHubRepo` auto-detected from `git remote get-url origin` when available).
  - Package `magicdown.exe`, `magicdown-host.exe`, `magicdown.ico`, `extension/` into `MagicDown-windows-amd64.zip`.
  - Generate `checksums.txt` (SHA256) compatible with `pkg/updater`.
- `scripts/install.ps1`:
  - Pass `-Version` and `-GitHubRepo` to `build-release.ps1` and record resolved version in Windows Uninstall registry.
- `.github/workflows/release.yml`:
  - Trigger on tag `v*` push, build on `windows-latest`, run `go test ./...`, run `scripts/build-release.ps1`, and publish GitHub Release with `MagicDown-windows-amd64.zip`, `magicdown.exe`, and `checksums.txt`.

## Verification Plan
1. `go test ./...`
2. `node --test extension/*.test.js`
3. `powershell -ExecutionPolicy Bypass -File scripts/build-release.ps1` (verifies `-H=windowsgui` build, `-ldflags` injection, zip packaging, and `checksums.txt` generation).
4. Code review & clean Gitmoji + Conventional Commits commit (no push).

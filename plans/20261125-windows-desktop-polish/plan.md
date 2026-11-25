# Plan: Windows Desktop Polish (Autostart, System Tray Power-Save, First-Run Onboarding, Installer)

**Date:** 2026-11-25  
**Branch:** `hongducdev/agentkit-full-pipeline`  
**Status:** In Progress

## Overview

Complete MagicDown's Windows desktop experience across four pillars:
1. **Autostart & Single-Instance**: HKCU Run registry integration (`--minimized`) and single-instance lock that wakes the existing tray window instead of spawning duplicate processes.
2. **System Tray & GPU/CPU Power-Save**: Close/Minimize to Windows System Tray with native context menu (`Open MagicDown`, `Pause All`, `Resume All`, `Quit`) and full suspension of MyGo's GPU render loop while hidden, keeping IPC (`127.0.0.1:28888`), Native Messaging, and active downloads running uninterrupted.
3. **First-Run Onboarding (Welcome Setup)**: Guided first-run modal persisted via `onboarding_completed` in `config.json`, allowing quick configuration of download folder, Auto-Categorize, Windows Autostart, System Tray behavior, and one-click Chrome/Edge Native Messaging registration.
4. **No-Console Build & Windows Installer**: `-ldflags="-s -w -H=windowsgui"` GUI subsystem build, `CREATE_NO_WINDOW` (`HideWindow`) for all child processes (`ffmpeg`, etc.), and standard Windows `scripts/install.ps1`, `scripts/uninstall.ps1`, and `scripts/build-release.ps1` installing to `%LOCALAPPDATA%\Programs\MagicDown` with Start Menu/Desktop shortcuts and Apps & Features Uninstall registry entry.

## Architecture & File Changes

### Phase 1: Config, Autostart, Subprocess Hiding, Native Messaging Registration
- `pkg/engine/types.go` & `pkg/engine/engine.go`:
  - Extend `Config` and `persistedConfig` with:
    - `LaunchAtStartup bool` (`json:"launch_at_startup"`) — default `false`
    - `MinimizeToTray bool` (`json:"minimize_to_tray"`) — default `true`
    - `CloseToTray bool` (`json:"close_to_tray"`) — default `true`
    - `OnboardingCompleted bool` (`json:"onboarding_completed"`) — default `false`
- `pkg/autostart/`:
  - `autostart.go`: `FormatCommand`, `ParseCommand`, `Manager` with injectable `Store` interface for deterministic unit testing.
  - `autostart_windows.go`: Windows `advapi32.dll` registry implementation for `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` (`MagicDown` value).
  - `autostart_stub.go`: Non-Windows stub.
  - `autostart_test.go`: Unit tests for command formatting/parsing and enable/disable/status flows.
- `pkg/engine/cmd_windows.go` & `pkg/engine/cmd_stub.go` + `pkg/engine/mux.go`:
  - Apply `syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}` to `ffmpeg` execution on Windows so muxing never flashes a console window.
- `pkg/nativemessaging/register.go`, `register_windows.go`, `register_stub.go`, `register_test.go`:
  - Pure-Go manifest builder & HKCU Chrome/Edge NativeMessagingHosts registry installer/checker so Onboarding and Settings can register Native Messaging without spawning PowerShell.

### Phase 2: System Tray, Power-Save Render Loop, Single-Instance, Onboarding & Settings UI
- `pkg/gui/app.go`:
  - Extend `WindowController` with `Hide()`, `Show()`, `Focus()`, `Restore()`, `IsVisible() bool`, `Invalidate()`.
  - Add tray state (`inTray bool`, `quitting bool`) and `FrameScheduleMode()` (`FrameSchedulePaused`, `FrameScheduleActive`, `FrameScheduleIdle`).
  - When `inTray == true`, `View(c)` skips UI layout/draw and does **not** schedule `c.AnimationFrame()` or `c.After(...)`, eliminating background GPU/CPU wakeups.
  - `HideToTray()` / `WakeFromTray()` methods; `EnqueueIncomingDownload` automatically calls `WakeFromTray()` so incoming browser downloads pop up the Download Info Dialog immediately.
  - Add `renderOnboardingModal` (Welcome Setup) shown on first run (`!cfg.OnboardingCompleted`), with "Skip" and "Finish Setup" actions, plus a button in Settings to re-open it.
  - Add Autostart, Minimize to Tray, Close to Tray toggles and Native Messaging Register button in Settings modal.
- `cmd/magicdown/main.go`:
  - Add `--minimized` and `--tray` CLI flags.
  - Call `mygo.App.RequestSingleInstanceLock()` and wire `mygo.App.OnSecondInstance` to wake the main window.
  - Create `mygo.NewTray` with `appIconPNG`, tooltip, left-click wake, and menu (`Open MagicDown`, `Pause All`, `Resume All`, `Quit`).
  - Hook `w.OnClose` to hide to tray when `CloseToTray` is enabled and not quitting; hook `mygo.App.OnWindowAllClosed` to keep tray process alive.

### Phase 3: Windows Installer, Uninstaller & Build Scripts
- `scripts/build-release.ps1`: Builds `dist/magicdown.exe` (`-ldflags="-s -w -H=windowsgui"`) and `dist/magicdown-host.exe`.
- `scripts/install.ps1`: Copies binaries & icon to `%LOCALAPPDATA%\Programs\MagicDown`, creates Start Menu & Desktop shortcuts, registers Native Messaging host pointing to the installed path, and writes `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\MagicDown`.
- `scripts/uninstall.ps1`: Stops running processes, removes Autostart, Native Messaging registry & manifest, shortcuts, Uninstall entry, and install directory.

### Phase 4: Verification & Review
- Unit tests: `pkg/autostart`, `pkg/engine` (config persistence), `pkg/gui` (power-save frame scheduling, tray hide/wake, onboarding & settings), `pkg/nativemessaging` (manifest & registration).
- Run `go test ./...`, `node --test extension/*.test.js`, and build `magicdown.exe` with `-ldflags="-s -w -H=windowsgui"`.
- Self-review diff and commit with Gitmoji + Conventional Commits.

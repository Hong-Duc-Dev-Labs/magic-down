# MagicDown

A fast, IDM-inspired desktop download manager built in pure Go with **MyGo Native UI** (GPU-rendered, no webview) and a **Chrome/Edge Manifest V3** browser extension connected via Native Messaging.

## Features (MVP)

- **Multi-Segment Download Engine (`pkg/engine`)**
  - Automatic HTTP/HTTPS range probing (`Accept-Ranges`, `Content-Length`, `Content-Disposition`) with seamless fallback to single-stream download when range requests are unsupported.
  - Configurable concurrent segments (default `8`), real-time speed/ETA/progress tracking, and atomic file completion (`.magicpart` -> final file).
  - Pause, Resume, Cancel, Delete, and persisted JSON state so paused/queued downloads survive app restarts.
  - Automatic retry with exponential backoff (500ms → 10s) for transient network, 5xx, 408 and 429 failures; retried segments resume from the bytes already written. Configurable `max_retries` (default `3`, `0` disables retries), with a live retry counter per task.
  - One shared token bucket for a global speed limit (`speed_limit_kbps`, `0` = unlimited) enforced across every segment and HLS fragment; raising or lowering it applies to running downloads within ~250ms.
  - Download queue with configurable max concurrent active downloads (default `3`).
  - Tunable settings persist to `config.json` next to the state file.
- **Desktop App (`cmd/magicdown`, `pkg/gui`)**
  - Pure Go **MyGo Native UI** (`github.com/egoist/mygo` v0.10.0) with IDM-inspired toolbar, status sidebar, download list, Add URL modal, and Settings modal (concurrency, segments, retries, speed limit).
  - Windows toast notification when a download completes (no cgo; hidden PowerShell fallback to a tray balloon).
  - Built-in localhost HTTP IPC server (`127.0.0.1:28888`) receiving jobs from the Native Messaging host.
- **Native Messaging Host (`cmd/magicdown-host`, `pkg/nativemessaging`)**
  - Implements Chrome/Edge 4-byte little-endian length-prefixed JSON stdio protocol and forwards jobs to the desktop app IPC.
  - Also available as a subcommand: `magicdown.exe host`.
- **Chrome / Edge Extension (`extension/`)**
  - Manifest V3 extension that intercepts browser downloads (with enable/disable toggle and configurable file-type filter in popup) and forwards URL, filename, referrer, and cookies to MagicDown.
  - Right-click context menu: **Download with MagicDown**.
  - Popup showing desktop app connection status, interception toggle, extension filter, and quick URL sender.

## Project Structure

```text
MagicDown/
├── cmd/
│   ├── magicdown/        # Desktop app (MyGo Native UI + IPC + `host` subcommand)
│   └── magicdown-host/   # Standalone Native Messaging host executable
├── pkg/
│   ├── engine/           # Multi-segment downloader, queue, persistence, tests
│   ├── gui/              # MyGo Native UI views, modals, formatting helpers, tests
│   ├── ipc/              # Localhost HTTP IPC server & client, tests
│   └── nativemessaging/  # 4-byte LE stdio framing & message handler, tests
├── extension/            # Chrome/Edge Manifest V3 browser extension
└── scripts/
    ├── install-host.ps1  # Register Native Messaging host in HKCU (Chrome & Edge)
    └── uninstall-host.ps1
```

## Quick Start (Windows)

### 1. Build & Run the Desktop App

```powershell
# Run all tests (with race detector)
go test -race ./...

# Build desktop app and native messaging host
go build -o magicdown.exe ./cmd/magicdown
go build -o magicdown-host.exe ./cmd/magicdown-host

# Launch MagicDown desktop app
.\magicdown.exe
```

Optional flags:
- `-port 28888` — Local IPC port (default `28888`).
- `-dir C:\Path\To\Downloads` — Default download directory (defaults to `%USERPROFILE%\Downloads\MagicDown`).

### 2. Load the Unpacked Browser Extension (Chrome / Edge)

1. Open `chrome://extensions` (Chrome) or `edge://extensions` (Edge).
2. Enable **Developer mode**.
3. Click **Load unpacked** and select the `extension/` folder in this repository.
   - `extension/manifest.json` includes a fixed public `"key"`, so the unpacked extension always gets the deterministic Extension ID: `cndchhhjghchjigecgkjclembafgjaje`.

### 3. Register the Native Messaging Host

Run the PowerShell script (defaults to `cndchhhjghchjigecgkjclembafgjaje`):

```powershell
.\scripts\install-host.ps1
```

(You can also pass `-ExtensionId <custom-32-char-id>` or `-Rebuild` if needed.)

This writes `%LOCALAPPDATA%\MagicDown\com.magicdown.downloader.json` and registers it under `HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.magicdown.downloader` and `HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\com.magicdown.downloader`.

To unregister later:

```powershell
.\scripts\uninstall-host.ps1
```

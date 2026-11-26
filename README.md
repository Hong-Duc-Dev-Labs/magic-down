# MagicDown

A fast, IDM-inspired desktop download manager for Windows built in pure Go with **MyGo Native UI** (GPU-rendered, zero webview) and a **Manifest V3 Chromium Browser Extension** connected via Native Messaging with real-time `HELLO/WELCOME` handshake verification.

---

## Features

- **Multi-Segment Download Engine**
  - Automatic HTTP/HTTPS range probing (`Accept-Ranges`, `Content-Length`, `Content-Disposition`) with seamless single-stream fallback.
  - Configurable concurrent segments (default `8`), real-time speed/ETA/progress tracking, atomic `.magicpart` completion, and global speed limit (`speed_limit_kbps`).
  - **Pause, Resume, Cancel, Delete**, persistent queue with configurable max concurrent downloads (default `3`), and automatic exponential-backoff retries.
- **YouTube MP4 & MP3 Downloads**
  - Floating video detector with quality selector for **MP4** (H.264/avc1 preferred, automatic video+audio muxing via FFmpeg) and **MP3** audio extraction at **320 / 192 / 128 kbps**.
- **Social & Streaming Video Support**
  - Built-in detection and download plans for **Facebook**, **Weibo**, **Bilibili**, **Douyin**, HLS (`.m3u8` including `#EXT-X-MAP`), and DASH/MSE players.
- **Video Thumbnail Downloader**
  - One-click high-resolution thumbnail extraction across YouTube, Bilibili, Weibo, Facebook, Douyin, and standard HTML5 `<video>` pages.
- **Batch Download All**
  - Scan any web page for images, videos, audio, documents, and archives, filter by category/size/search, and send selected items in one batch to MagicDown.
- **Download Info Dialog & Auto-Categorization**
  - Review filename, estimated size, save directory, and category before starting, with automatic subfolder sorting (`Video`, `Music`, `Compressed`, `Documents`, `Programs`, `Other`).
- **System Tray, Autostart & Welcome Setup**
  - GPU/CPU-optimized System Tray mode (pauses UI rendering while hidden in tray, wakes instantly on incoming downloads), optional **Launch at Windows Startup**, and a guided first-run **Welcome Setup**.
- **Bilingual UI (English & Tiếng Việt)**
  - Instant switching between English and Vietnamese across both the Desktop App and Browser Extension.
- **GitHub Auto-Updater**
  - Built-in release checker and verified SHA-256 self-updater from GitHub Releases.
- **Secure Native Messaging & Browser Integration**
  - Localhost IPC with per-user auth token, `HELLO/WELCOME` handshake verification, automatic reconnect, and live 7-state Browser Integration diagnostics.

---

## Installing the Desktop App (Windows)

### Option 1: From GitHub Releases (Recommended)

1. Download `MagicDown-windows-amd64.zip` from the latest [GitHub Release](../../releases).
2. Extract the archive to a temporary folder.
3. Run the installer script in PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1 -LaunchAfterInstall
```

This installs MagicDown to `%LOCALAPPDATA%\Programs\MagicDown` (including the bundled `extension/` folder), creates Start Menu & Desktop shortcuts, registers the Native Messaging Host for Chrome, Edge, and detected Chromium browsers, and adds an Apps & Features uninstall entry.

### Option 2: Build & Install from Source

Requires **Go 1.22+** on Windows:

```powershell
.\scripts\install.ps1 -LaunchAfterInstall
```

Optional flags:
- `-EnableAutostart` — Start MagicDown minimized to System Tray with Windows.
- `-NoDesktopShortcut` — Skip creating a Desktop shortcut.
- `-SkipBuild` — Install prebuilt binaries from `dist/`.

To uninstall later, use Windows **Apps & Features** or run:

```powershell
& "$env:LOCALAPPDATA\Programs\MagicDown\uninstall.ps1"
```

---

## Installing the Browser Extension (Load Unpacked)

MagicDown bundles a ready-to-load Manifest V3 extension at:

```text
%LOCALAPPDATA%\Programs\MagicDown\extension
```

Supported Chromium browsers: **Google Chrome**, **Microsoft Edge**, **Dia**, **Brave**, **Vivaldi**, **Opera / Opera GX**, and **Cốc Cốc**.

> **Deterministic Extension ID:** `extension/manifest.json` includes a fixed public `"key"`, so loading it unpacked on any machine and any Chromium browser always produces the exact same Extension ID (`cndchhhjghchjigecgkjclembafgjaje`) matching the registered Native Messaging Host `allowed_origins`.

### Step-by-Step Setup

#### Fastest Way (Directly from MagicDown Settings)
1. Open **MagicDown → Settings → Browser Integration**.
2. Click **Install Extension** next to your browser.
   - MagicDown automatically registers the Native Messaging Host, copies `%LOCALAPPDATA%\Programs\MagicDown\extension` to your clipboard, and opens your browser's Extensions page.
3. Follow the 3 steps shown in the browser:
   1. Enable **Developer mode** (top-right toggle).
   2. Click **Load unpacked**.
   3. Paste the copied path (`%LOCALAPPDATA%\Programs\MagicDown\extension`) and select the folder.

#### Manual Way
1. Open your browser's Extensions page:
   - Chrome / Dia: `chrome://extensions`
   - Edge: `edge://extensions`
   - Brave: `brave://extensions`
   - Vivaldi: `vivaldi://extensions`
   - Opera: `opera://extensions`
   - Cốc Cốc: `coccoc://extensions`
2. Enable **Developer mode**.
3. Click **Load unpacked** and select `%LOCALAPPDATA%\Programs\MagicDown\extension`.

### Verifying Connection

Once loaded, the extension immediately connects to `magicdown-host.exe` and performs a `HELLO/WELCOME` handshake with the MagicDown desktop app:
- In **MagicDown → Settings → Browser Integration**, your browser status automatically changes from **Waiting for Connection** to **Connected**.
- Clicking the **MagicDown** toolbar icon in your browser also shows **Connected**.

---

## Development & Testing

```powershell
# Run all Go tests
go test ./...

# Run all Browser Extension tests (Node.js 18+)
node --test extension/*.test.js

# Build Windows GUI binary (no console window) and Native Messaging Host
go build -ldflags="-s -w -H=windowsgui" -o dist/magicdown.exe ./cmd/magicdown
go build -ldflags="-s -w" -o dist/magicdown-host.exe ./cmd/magicdown-host
```

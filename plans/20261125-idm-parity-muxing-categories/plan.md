# Plan: IDM-Parity Auto-Muxing, Auto Categories & Download Info Dialog

## Goal
Bring MagicDown closer to IDM with two core features across the browser extension, Go download engine, IPC/native messaging, and MyGo native UI:
1. **Auto Video + Audio Muxing (DASH -> MP4)**: Pair adaptive video tracks with the best audio track on YouTube & Facebook, download both tracks in parallel in Go, and mux into a single `.mp4` file via `ffmpeg` (auto-detected next to binary or in `PATH`) with temp cleanup and clear fallback errors.
2. **Auto Categories + Smart Filenames + Download Info Dialog**: Classify downloads into category subfolders (`Video`, `Music`, `Compressed`, `Documents`, `Programs`, `Other`), deduplicate filenames with `(1)`, `(2)`, and show a Download Info Dialog in MyGo UI for both manual URLs and incoming extension links with **Start Download** and **Download Later**.

## Architecture & Changes

### Phase 1: Auto Video + Audio Muxing
- **`extension/youtube.js`**: Pair adaptive video variants with the best compatible adaptive audio track (`audioUrl`, `audioSize`, `streamType: "muxed"`, combined size, `MP4` label). Keep `video-only` fallback if no audio track exists.
- **`extension/background.js`**:
  - Track Facebook audio-only DASH tracks per `(tabId, fbVideoId)` (keeping highest bitrate) without breaking existing `classifyMediaResponse` filtering.
  - Pair Facebook DASH video items with their matching `fbVideoId` audio track in `getTabVideoList`.
  - Forward `audioUrl` -> `audio_url` in `forwardDownload`.
- **`extension/content.js`**:
  - Display clear `MP4 · Video+Audio` label on paired DASH items.
  - Include `audioUrl` when sending `download` message to `background.js`.
- **`pkg/nativemessaging/host.go` & `pkg/ipc/server.go`**:
  - Add `AudioURL` (`audio_url`) to `InboundMessage` and `AddDownloadRequest`.
- **`pkg/engine/types.go`, `pkg/engine/mux.go`, `pkg/engine/task.go`, `pkg/engine/engine.go`**:
  - Add `StatusMuxing` and `Task.AudioURL`.
  - Add `FindFFmpeg()` (binary dir then `PATH`) and `MuxVideoAudio(ctx, videoPath, audioPath, outputPath)` with `-c copy -movflags +faststart` and AAC audio fallback.
  - Add `runMuxedDownload` to download video & audio tracks in parallel, mux to final `.mp4`, and clean up temp files.

### Phase 2: Auto Categories, Smart Filenames & Download Info Dialog
- **`pkg/engine/category.go`**:
  - `CategoryForFilename(filename, rawURL)` -> `Video`, `Music`, `Compressed`, `Documents`, `Programs`, `Other`.
  - `ResolveSaveDir(baseDir, category, autoCategorize)` -> subfolder when enabled.
  - `UniqueFilePath(dir, filename)` -> appends ` (1)`, ` (2)` on collision.
- **`pkg/engine/types.go`, `pkg/engine/engine.go`, `pkg/engine/task.go`**:
  - Add `Config.AutoCategorize` (default `true`, persisted in `config.json`), `Task.Category`, and `AddTaskRequest.Category` / `StartPaused`.
  - Apply auto-category subfolder and duplicate filename resolution in `AddTask` / `processProbeResponse`.
- **`pkg/ipc/server.go` & `cmd/magicdown/main.go`**:
  - Add optional `OnIncomingDownload` hook on `ipc.Server` so desktop UI can intercept extension downloads and open the Download Info Dialog without blocking the IPC response.
- **`pkg/gui/components.go` & `pkg/gui/app.go`**:
  - Align sidebar categories with `Video`, `Music`, `Compressed`, `Documents`, `Programs`, `Other`.
  - Upgrade the Add/Download Info Dialog to show URL, editable Filename, Category pills, Save Directory, Estimated Size / Auto-Mux badge, **Start Download**, and **Download Later** (adds as Paused).
  - Add **Auto-Categorize Downloads** toggle in Settings modal.

### Phase 3: Verification
- Go unit tests: `pkg/engine/mux_test.go`, `pkg/engine/category_test.go`, `pkg/ipc/ipc_test.go`.
- Extension unit tests: `extension/youtube.test.js`, `extension/background.test.js`.
- Run `go test ./...`, `node --test extension/*.test.js`, and `go build -o magicdown.exe ./cmd/magicdown`.

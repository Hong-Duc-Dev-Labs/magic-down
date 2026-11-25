# Plan: YouTube Standard MP4 Pairing & Multi-Quality MP3 Audio Download

## Goal
Add two media download capabilities across the browser extension (`extension/`) and Go desktop engine (`pkg/engine`, `pkg/ipc`, `pkg/nativemessaging`, `pkg/gui`):
1. **Standard MP4 on YouTube**: Always offer clear MP4 video+audio choices at each resolution (4K, 1440p, 1080p, 720p, 480p, 360p), preferring H.264 (`avc1`) / `av01` MP4 + M4A (`audio/mp4`) with fast `-c copy` muxing, and falling back to AAC/H.264 transcoding with `-movflags +faststart` when only WebM/Opus tracks exist.
2. **Multi-Quality MP3 Audio (320 / 192 / 128 kbps)**: Offer `MP3 · 320 kbps`, `MP3 · 192 kbps`, and `MP3 · 128 kbps` in the video overlay menu across YouTube, Bilibili, Facebook, Weibo, Douyin, and generic HTML5 videos. Download the optimal source track (audio-only on YouTube/Bilibili/Facebook DASH, main video URL on progressive sites), convert to MP3 via `ffmpeg` (`-vn -c:a libmp3lame -b:a <bitrate> -id3v2_version 3`, `CREATE_NO_WINDOW`), auto-categorize into `Music`, and clean up temp files.

## Implementation Phases

### Phase 1: Extension (YouTube MP4 Priority + Multi-Quality MP3)
- `extension/youtube.js`:
  - Parse codec preference from `mimeType` (`avc1` > `av01` > other MP4 > WebM) when deduplicating adaptive video per `(height, fps)`.
  - Pair each adaptive video tier with best M4A (`audio/mp4`), falling back to best WebM/Opus audio, and emit `format: "MP4"`, `ext: "mp4"`, `targetFormat: "mp4"`.
  - Generate 3 MP3 audio variants (`MP3 · 320 kbps`, `MP3 · 192 kbps`, `MP3 · 128 kbps`) from the highest-bitrate audio track (or progressive fallback) with `outputFormat: "mp3"`, `audioBitrate: "320k"|"192k"|"128k"`.
- `extension/bilibili.js`:
  - Generate 3 MP3 audio variants (`MP3 · 320 kbps`, `MP3 · 192 kbps`, `MP3 · 128 kbps`) from the highest-bitrate DASH audio track (or `durl` fallback), preserving Bilibili `referrer` and `headers`.
- `extension/content.js`:
  - Render MP3 audio items clearly in the video overlay dropdown.
  - For non-YouTube/Bilibili videos (Facebook, Weibo, Douyin, generic video), synthesize the 3 MP3 items from the best available audio URL (e.g. Facebook DASH `audioUrl`) or primary progressive video URL.
  - Forward `outputFormat`, `audioBitrate`, and `targetFormat` in the download message to `background.js`.
- `extension/background.js`:
  - Support `outputFormat` (`output_format`), `audioBitrate` (`audio_bitrate`), and `targetFormat` (`target_format`) in `buildDownloadPayload`, `forwardDownload`, `sanitizeDownloadPlan`, and `chrome.runtime.onMessage`.

### Phase 2: Go Engine, IPC, Native Messaging & GUI
- `pkg/nativemessaging/host.go`, `pkg/ipc/server.go`, `pkg/gui/app.go`:
  - Plumb `output_format`, `audio_bitrate`, and `target_format` through `InboundMessage`, `AddDownloadRequest`, `engine.AddTaskRequest`, and GUI confirmation dialog state.
- `pkg/engine/types.go`, `pkg/engine/plan.go`, `pkg/engine/engine.go`:
  - Add `OutputFormat`, `AudioBitrate`, `TargetFormat` to `Task`, `AddTaskRequest`, and `DownloadPlan`.
  - Normalize `OutputFormat` (`mp3`) and `AudioBitrate` (`320k`, `192k`, `128k`, default `192k`).
  - When `OutputFormat == "mp3"`, enforce `.mp3` filename extension and `CategoryMusic` auto-categorization.
- `pkg/engine/mux.go`, `pkg/engine/task.go`:
  - Enhance `MuxVideoAudio` with 3-tier MP4 compatibility: `-c copy` -> `-c:v copy -c:a aac -b:a 192k` -> `-c:v libx264 -preset veryfast -crf 22 -c:a aac -b:a 192k`, all with `-movflags +faststart`.
  - Add `ConvertToMP3(ctx, srcPath, dstPath, bitrate)` using `ffmpeg` (`-vn -c:a libmp3lame -b:a <bitrate> -id3v2_version 3`, `CREATE_NO_WINDOW`), atomic rename, and clear install instructions when `ffmpeg` is missing.
  - In `Task.Start` / `Task.finalizeFile`: preflight `FindFFmpeg()` for `mp3` tasks, convert downloaded temp file to `.mp3`, delete temp file, and update final size.

### Phase 3: Unit Tests, Verification & Commit
- `extension/youtube.test.js`, `extension/background.test.js`:
  - Test YouTube MP4 codec/container priority, WebM fallback with `targetFormat: "mp4"`, and generation of 3 MP3 variants (320/192/128 kbps).
  - Test `buildDownloadPayload` forwarding `output_format`, `audio_bitrate`, and `target_format`.
- `pkg/engine/mux_test.go`, `pkg/ipc/ipc_test.go`:
  - Test MP4 mux fast path and fallback paths, MP3 conversion with 320k/192k/128k bitrates, missing ffmpeg error message, Auto Categories -> `Music`, and IPC payload forwarding.
- Run `node --test extension/*.test.js`, `go test ./...`, and `go build -ldflags="-H=windowsgui" ./cmd/magicdown`.
- Commit cleanly with Gitmoji + Conventional Commits (no push).

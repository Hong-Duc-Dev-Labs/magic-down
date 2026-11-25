# Plan: Download All Links & Media on Page ("Tải toàn bộ trên trang web")

## Goal
Add an IDM-style "Download all with MagicDown" feature allowing users to scan all downloadable resources on the active web page (images, videos, audio, documents/archives/files), filter and pick items in a clean Batch Picker UI with image thumbnails and checkboxes, and enqueue selected items in batch to the MagicDown desktop app without spawning N Download Info Dialogs.

## Architecture & Changes

### 1. Extension Entry Points (`extension/manifest.json`, `extension/background.js`, `extension/popup.html`, `extension/popup.js`, `extension/popup.css`)
- **Context Menu**: Add `magicdown-download-all` ("Download all with MagicDown") in `ensureContextMenus()` with `contexts: ["page", "frame", "selection", "link", "image", "video", "audio"]`.
- **Popup Button**: Add a prominent "Quét & tải toàn bộ trang" (Scan Page / Download All) action button in `popup.html` / `popup.js` that triggers the same scan-and-open-picker flow for the active tab.
- **Content Scripts**: Include `batch-scanner.js` in `manifest.json` content scripts (before `content.js`) so the scanner is available on every page, and expose `batch-picker.html` as an extension page opened in a dedicated tab/window when scanning completes.

### 2. Page Resource Scanner (`extension/batch-scanner.js`, `extension/content.js`)
- Pure, testable module `extension/batch-scanner.js` (UMD/CommonJS + `window.MagicDownBatchScanner`):
  - `parseSrcsetBest(srcset, baseUrl)`: parses `srcset` (`w` and `x` descriptors) and returns the highest-resolution candidate URL.
  - `normalizeResourceUrl(rawUrl, baseUrl)`: resolves relative URLs to absolute `http:`/`https:` URLs, rejects `data:`, `blob:`, `javascript:`, `mailto:`, etc.
  - `classifyResource(url, hintType, elementMeta)`: classifies into `image`, `media` (video/audio), or `file` (pdf, zip, rar, 7z, tar, gz, iso, exe, msi, dmg, apk, docx, xlsx, pptx, csv, txt, epub, etc.).
  - `suggestFilename(url, fallbackTitle, ext)`: derives clean filename from URL path or fallback.
  - `suggestSubfolder(pageTitle, pageUrl)`: sanitizes page title or hostname into a safe subfolder name.
  - `scanDocument(doc, baseUrl, options)`: scans:
    - `<img>` (`currentSrc`, `src`, `srcset` highest quality, `data-src`, `data-original`, `data-lazy-src`, `data-srcset`), `<picture><source>`
    - CSS `background-image: url(...)` on visible elements
    - `<video>`, `<audio>`, `<source>`
    - `<a href>` pointing to images, media, or downloadable file extensions
    - Deduplicates by normalized URL, preserves `naturalWidth`/`naturalHeight` for images, and supports filtering tiny icons/tracking pixels (`minWidth`/`minHeight`).
  - `filterResources(items, filters)`: filters by category (`all` | `image` | `media` | `file`), minimum image dimension (e.g. 0 / 50 / 100 / 200 / 400 px), and search/extension query.
  - `buildBatchPayload(items, context)`: constructs the native messaging `batch_download` payload with `referrer`, `headers` (`User-Agent`), `subfolder`, and normalized item list.
- In `extension/content.js` (top frame): handle `chrome.runtime.onMessage` `{ type: "scanPageResources" }` by calling `window.MagicDownBatchScanner.scanDocument(document, location.href)` and returning `{ items, pageTitle, pageUrl }`.

### 3. Batch Picker UI (`extension/batch-picker.html`, `extension/batch-picker.css`, `extension/batch-picker.js`)
- Dedicated extension page (`batch-picker.html`) opened by `background.js` (passing a `scanId` stored in `chrome.storage.session` or `chrome.storage.local` / runtime message).
- Supports dark/light theme matching `popup.css`.
- Features:
  - Category filter tabs with counts: **Ảnh (Images)** | **Video/Audio** | **Tài liệu/File** | **Tất cả (All)**.
  - Minimum image size filter dropdown (`Tất cả kích thước`, `>= 50px`, `>= 100px` default to filter icons, `>= 200px`, `>= 400px`).
  - Search / extension filter input (e.g. `jpg`, `pdf`, `banner`).
  - Optional subfolder input pre-filled with sanitized page title.
  - Thumbnail grid view for images (showing resolution badge `WxH` + filename + checkbox) and clean list view for media/files.
  - Controls: **Chọn tất cả** (Select All in current filter), **Bỏ chọn** (Deselect All), live selected counter, **Tải các mục đã chọn (N)**, and **Tải toàn bộ**.
  - Sends selected items to `background.js` (`type: "batchDownload"`), displays progress/result toast.

### 4. Go Backend Batch Support (`pkg/ipc/server.go`, `pkg/ipc/client.go`, `pkg/nativemessaging/host.go`)
- **`pkg/ipc/server.go`**:
  - Add `SkipDialog bool` (`json:"skip_dialog,omitempty"`) and `Subfolder string` (`json:"subfolder,omitempty"`) to `AddDownloadRequest`.
  - When `req.SkipDialog` is true in `handleAddDownload`, bypass `onIncomingDownload` and enqueue directly into `s.engine.AddTask(...)`.
  - Add `BatchDownloadRequest` (`Items []AddDownloadRequest`, `Subfolder string`, `Referrer string`, `Cookies string`, `Headers map[string]string`, `AutoStart bool`) and `BatchDownloadResponse` (`Success bool`, `Added int`, `Failed int`, `TaskIDs []string`, `Errors []string`, `Message string`).
  - Register `POST /downloads/batch` (`handleAddBatchDownload`): sanitizes `Subfolder` (prevents path traversal), resolves save directory under `engine.Config().DownloadDir`, and adds each valid item directly to `s.engine.AddTask(...)` (never triggering `onIncomingDownload` dialogs).
- **`pkg/ipc/client.go`**:
  - Add `AddBatchDownload(req BatchDownloadRequest) (*BatchDownloadResponse, error)` posting to `/downloads/batch`.
- **`pkg/nativemessaging/host.go`**:
  - Support `Items []ipc.AddDownloadRequest`, `Subfolder string`, `SkipDialog bool` on `InboundMessage` and `Added int`, `Failed int` on `OutboundMessage`.
  - Handle `action: "batch_download"` by checking `IsAppRunning()` and calling `ipcClient.AddBatchDownload(...)`.

### 5. Testing & Verification
- Add `extension/batch-scanner.test.js` covering:
  - `parseSrcsetBest`, `normalizeResourceUrl`, `classifyResource`, `suggestFilename`, `suggestSubfolder`
  - `scanDocument` (DOM mock for `<img>`, `<picture><source>`, CSS `background-image`, `<video>`/`<audio>`, `<a href>`, deduplication, tracking pixel filtering)
  - `filterResources` (category, min image size, search/ext)
  - `buildBatchPayload` (headers, referrer, subfolder, item normalization)
- Add Go tests in `pkg/ipc/ipc_test.go` and `pkg/nativemessaging/protocol_test.go` (or `host_test.go`) for `/downloads/batch`, `skip_dialog`, subfolder sanitization, and `batch_download` native messaging action.
- Run `node --test extension/*.test.js` and `go test ./...`.
- Review all diffs and commit with Gitmoji + Conventional Commits (no push).

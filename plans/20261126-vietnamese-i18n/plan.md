# Plan: Vietnamese & English Internationalization (i18n) for MagicDown

**Date:** 2026-11-26  
**Scope:** Desktop App (Go + MyGo Native UI) & Browser Extension (Chrome/Edge MV3)

## 1. Objectives
1. **Desktop App (`pkg/i18n`, `pkg/engine`, `pkg/gui`, `cmd/magicdown`):**
   - Create `pkg/i18n` with full `en` (English) and `vi` (Tiếng Việt) dictionaries, language normalization, OS locale auto-detection (Windows `GetUserDefaultLocaleName` / env fallback), thread-safe active language state, and `T(key)` / `Tf(key, args...)` helpers.
   - Persist `language` (`"en"` | `"vi"`, empty = auto-detect) in `engine.Config` and `persistedConfig` (`config.json`).
   - Add immediate language switching (no restart required) in both **Welcome Setup** and **Settings**, plus dynamic System Tray menu label updates via a callback (`SetOnLanguageChange`) or rebuild.
   - Translate the entire Desktop UI naturally: Title bar, Toolbar (New Download, Resume, Pause, Delete, Clear Done, Search), Sidebar (Downloads statuses, Categories: All, Video, Music, Compressed, Documents, Programs, Other, Light/Dark Mode, Settings), Download Cards (status badges, segments, retries, empty state, status bar), Download Info / Add Dialog, Settings Modal, Welcome Setup Modal, Software Updates card, and System Tray menu.
   - **Vietnamese Font Support in MyGo Native UI:** Explicitly set `t.Font = "Segoe UI"` on Windows in `DarkPalette()` and `LightPalette()` (`pkg/gui/theme.go`) so DirectWrite uses Segoe UI (full Latin Extended Additional / Vietnamese Unicode coverage: ă, â, đ, ê, ô, ơ, ư + all 5 tone marks) with crisp ClearType rendering and zero missing-glyph tofu boxes.

2. **Browser Extension (`extension/`):**
   - Create `extension/i18n.js` (UMD/global `MagicDownI18n` + CommonJS export for Node tests) with `en` and `vi` dictionaries, browser language auto-detection (`chrome.i18n.getUILanguage()` / `navigator.language`), and `t(key, params)` helper.
   - Persist `language` (`"en"` | `"vi"`) in `chrome.storage.local` and expose a compact `VI / EN` language toggle in `popup.html`/`popup.js` with instant UI update + storage sync.
   - Translate all UI surfaces:
     - `popup.html` & `popup.js` (header, status pill, toggles, quick URL, Scan Page button, detected videos, Activity/Filters tabs, toasts, footer).
     - `background.js` right-click `contextMenus` (rebuild menus immediately when `language` changes in `chrome.storage.onChanged`).
     - `content.js` video overlay dropdown (header, sub-label, Download / Sent! / Offline pills, Thumb / MP3 / Video+Audio / Video only / Audio only / Dismiss tooltip).
     - `batch-picker.html` & `batch-picker.js` (title, subfolder, Rescan, category tabs Image/Video-Audio/Documents-Files/All, min size select, search placeholder, Select All / Deselect, empty state, footer summary, Download All / Download Selected, status banners).
   - Load `i18n.js` in `manifest.json` (`content_scripts`), `background.js` (`importScripts("i18n.js")`), `popup.html`, and `batch-picker.html`.

3. **Verification & Commit:**
   - Add `pkg/i18n/i18n_test.go` verifying 100% key parity between `en` and `vi`, non-empty values, Vietnamese diacritic coverage, and language switching.
   - Add `extension/i18n.test.js` verifying 100% key parity between `en` and `vi`, interpolation, and language detection/fallback.
   - Run `go test ./...`, `node --test extension/*.test.js`, and `go build -ldflags="-H=windowsgui" ./cmd/magicdown`.
   - Commit cleanly with Gitmoji + Conventional Commits (no push).

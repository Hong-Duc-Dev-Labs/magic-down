# AGENTS.md — Contributor & AI Agent Instructions

## Before Editing UI or Docs
1. **Read `DESIGN.md`** before making any visual, layout, color, or copy change in `pkg/gui/` or `extension/`.
2. **Keep tokens synchronized:** Any palette or typography update in `pkg/gui/theme.go` must be mirrored in `extension/popup.css`, `extension/batch-picker.css`, and the Shadow DOM stylesheet in `extension/content.js`.
3. **Preserve bilingual parity:** Every user-visible string must exist in both English and Vietnamese (`pkg/i18n/i18n.go` for desktop, `extension/i18n.js` for extension).
4. **Never hardcode versions:** Use `version.NormalizedVersion()` in Go and `chrome.runtime.getManifest().version` in the extension.

## Verification Requirements
- Run Go tests: `go test ./...`
- Run Extension tests: `node --test extension/*.test.js`
- Verify against `REVIEW.md`: focus-visible rings, `prefers-reduced-motion`, WCAG AA contrast, and responsive layouts (`1440×900`, `768×1024`, `375×812` for `extension/batch-picker.html`).

## Keeping AX Surfaces in Sync
- When adding major features, supported sites, or docs, update `README.md`, `README.vi.md`, and `llms.txt` in the same change so human readers and coding agents always see accurate capabilities.

# MagicDown UX / AX Review Checklist

Use this checklist before merging any UI, extension, or documentation PR.

## 1. Visual & Design Token Parity
- [ ] New or changed UI follows `DESIGN.md` tokens (no ad-hoc hex colors or inline styles).
- [ ] Dark and Light themes both maintain ≥ 4.5:1 contrast for body/metadata text (`--text-muted` / `TextMuted` ≥ 5:1).
- [ ] Version badges read dynamically (`version.NormalizedVersion()` in Go, `chrome.runtime.getManifest().version` in extension) — never hardcoded.
- [ ] Updated screenshots accompany visual UI changes.

## 2. Accessibility & Motion
- [ ] Every interactive element (button, switch, tab, input, card) has a visible `:focus-visible` indicator.
- [ ] All interactive hit targets are ≥ 24×24 CSS px.
- [ ] Status toasts and banners use `role="status" aria-live="polite"`.
- [ ] Overlays and modals dismiss cleanly with `Escape`.
- [ ] `@media (prefers-reduced-motion: reduce)` disables non-essential transitions.

## 3. Responsive & Layout Integrity
- [ ] Extension popup fits cleanly at `372px` width with zero horizontal scrollbar.
- [ ] Batch Picker (`extension/batch-picker.html`) renders without horizontal overflow or footer overlap at `1440×900`, `768×1024`, and `375×812`.
- [ ] Bilingual UI (`en` and `vi`) fits without clipped labels or text overlap.

## 4. Safety & Trust
- [ ] Destructive actions (delete download, delete file) require confirmation naming the item.
- [ ] Any new extension permission is documented in the Privacy & Security section of `README.md` and `README.vi.md`.

## 5. AX & Verification
- [ ] `README.md`, `README.vi.md`, and `llms.txt` stay in sync when features or docs change.
- [ ] `go test ./...` passes.
- [ ] `node --test extension/*.test.js` passes.

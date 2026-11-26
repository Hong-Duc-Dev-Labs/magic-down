# MagicDown Design System

**Brand essence:** Fast, trustworthy, zero-webview Windows download acceleration with Linear/Raycast-inspired clarity across native desktop and Chromium extension surfaces.

---

## Art Direction

- **Register:** Product utility — restrained, high-signal, low-chrome, instant legibility.
- **Signature motif:** Rounded blue lightning-bolt mark (`#3B82F6` dark / `#2563EB` light) paired with slate-navy dark surfaces (`#0B0D12` canvas, `#141923` cards) and crisp tabular metrics (`font-variant-numeric: tabular-nums`).
- **Consistency rule:** `pkg/gui/theme.go`, `extension/popup.css`, `extension/batch-picker.css`, and the Shadow DOM overlay in `extension/content.js` must stay in exact semantic token parity.

---

## Color Tokens

All text tokens meet WCAG 2.2 AA contrast (≥ 4.5:1 for normal text on both canvas and card surfaces).

| Semantic Token | Dark Mode | Light Mode | Usage |
|---|---|---|---|
| `Canvas` / `--canvas` | `#0B0D12` | `#F8FAFC` | Root window & page background |
| `Sidebar` / `--header` | `#10131B` | `#F1F5F9` / `#FFFFFF` | Navigation rail & top bar |
| `Card` / `--card` | `#141923` | `#FFFFFF` | Primary elevated cards & modals |
| `CardHover` / `--card-hover` | `#19202D` | `#F1F5F9` | Interactive hover surface |
| `InputBg` / `--input-bg` | `#0D1017` | `#F8FAFC` | Inputs, nested rows, chips |
| `Border` / `--border` | `#212838` | `#E2E8F0` | 1px hairlines |
| `BorderStrong` / `--border-strong` | `#2F3950` | `#CBD5E1` | Hover borders, inactive tracks |
| `TextPrimary` / `--text-primary` | `#F1F5F9` | `#0F172A` | Primary headings & filenames |
| `TextSecondary` / `--text-secondary` | `#94A3B8` | `#475569` | Secondary labels & domains |
| `TextMuted` / `--text-muted` | `#8494AB` | `#526075` | Captions, hints, version badges (WCAG AA ≥ 5:1) |
| `Accent` / `--accent` | `#3B82F6` | `#2563EB` | Active tab, focus ring, icon/text accent |
| `AccentHover` / `--accent-hover` | `#60A5FA` / `#2563EB` | `#1D4ED8` | Accent hover state |
| `AccentFill` / `--accent-fill` | `#2563EB` | `#2563EB` | Solid primary CTA button fill with `#FFFFFF` text (5.17:1 WCAG AA) |
| `AccentFillHover` / `--accent-fill-hover` | `#1D4ED8` | `#1D4ED8` | Solid primary CTA hover fill with `#FFFFFF` text (6.70:1 WCAG AA) |
| `Success` / `--success` | `#10B981` | `#059669` | Completed downloads, connected status |
| `Warning` / `--warning` | `#F59E0B` | `#D97706` | Paused, queued, offline status |
| `Danger` / `--danger` | `#EF4444` | `#DC2626` | Failed downloads, destructive actions |

---

## Typography

- **Desktop (Windows):** `Segoe UI` (full Vietnamese Unicode diacritic coverage: `ă, â, đ, ê, ô, ơ, ư` and all tone marks).
- **Extension:** `-apple-system, BlinkMacSystemFont, "Segoe UI", Inter, Roboto, sans-serif`.
- **Scale:** 10px uppercase section labels (`letter-spacing: 0.05em`), 11–12px metadata/inputs, 13px body/UI default, 14–16px headers/titles.
- **Metrics:** Always use `font-variant-numeric: tabular-nums` for download speeds, byte sizes, percentages, and item counts.

---

## Spacing, Radii & Targets

- **Base spacing unit:** `4px` (8, 10, 12, 14, 16, 20px rhythm).
- **Radii:** Cards & modals `12–15px`, inputs & buttons `8–12px`, status pills & segmented tabs `999px` (pill).
- **Hit targets:** Every interactive control (buttons, switches, chip remove `×`, dismiss buttons) must be at least **24×24 CSS px** (WCAG 2.2 AA).

---

## Motion & Accessibility

- **Transitions:** `140–180ms ease` for state changes (background, border-color, switch knob `transform`).
- **Reduced motion:** Every CSS stylesheet (`popup.css`, `batch-picker.css`, `content.js` Shadow DOM) must include `@media (prefers-reduced-motion: reduce)` disabling non-essential transitions.
- **Keyboard focus:** Never strip focus outlines without a `:focus-visible` replacement (`2px solid var(--accent)`, `outline-offset: 2px`).
- **Live feedback:** Status toasts and banners must declare `role="status" aria-live="polite"`.
- **Destructive actions:** Deleting a download requires explicit confirmation naming the affected file.

---

## Breakpoints (Extension Batch Picker)

- **Desktop:** `> 768px` — horizontal header, single-row toolbar, auto-fill grid (`minmax(176px, 1fr)`).
- **Tablet / Split-screen:** `≤ 768px` — full-width search and subfolder controls, relaxed padding.
- **Narrow / Mobile:** `≤ 480px` — stacked footer actions, compact grid (`minmax(140px, 1fr)`), zero horizontal overflow.

---

## Voice & Tone (Bilingual EN / VI)

- **Outcome-first, verb-led labels:** `"Start Download"`, `"Reset Filters"`, `"Scan & Download All on Page"`.
- **Honest status & diagnostics:** Explain what happened and the next step; keep internal protocol identifiers out of primary headlines.
- **Version parity:** Never hardcode version numbers in UI markup; read dynamically from `pkg/version` (Go) and `chrome.runtime.getManifest().version` (Extension).

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const extDir = __dirname;
const read = (name) => fs.readFileSync(path.join(extDir, name), "utf8");

test("extension surfaces include focus-visible, prefers-reduced-motion, and WCAG AA accent fill tokens", () => {
  for (const file of ["popup.css", "batch-picker.css", "content.js"]) {
    const src = read(file);
    assert.match(src, /:focus-visible/, `${file} must style :focus-visible`);
    assert.match(src, /prefers-reduced-motion:\s*reduce/, `${file} must honor prefers-reduced-motion`);
  }

  for (const file of ["popup.css", "batch-picker.css"]) {
    const src = read(file);
    assert.match(src, /--accent-fill:\s*#2563EB/i, `${file} must define --accent-fill token`);
    assert.match(src, /--text-muted:\s*#8494AB/i, `${file} must use WCAG AA dark muted text`);
    assert.match(src, /--text-muted:\s*#526075/i, `${file} must use WCAG AA light muted text`);
  }
});

test("popup.html and popup.js avoid hardcoded v1.0, sync manifest version, and expose ARIA live/labels", () => {
  const html = read("popup.html");
  const js = read("popup.js");

  assert.doesNotMatch(html, />v1\.0</, "popup.html must not hardcode v1.0");
  assert.match(html, /id="toast"[^>]*aria-live="polite"/, "toast must declare aria-live=polite");
  assert.match(html, /id="interceptToggle"[^>]*aria-labelledby="interceptTitle"/);
  assert.match(html, /id="excludeSiteToggle"[^>]*aria-labelledby="excludeSiteTitle"/);
  assert.match(js, /chrome\.runtime\.getManifest\(\)\.version/, "popup.js must sync version from manifest.json");
  assert.match(js, /setAttribute\("aria-label",\s*t\("popup\.lang_toggle"\)\)/);
  assert.match(js, /setAttribute\("aria-label",\s*t\("popup\.theme_toggle"\)\)/);
});

test("batch-picker defaults to English, announces status politely, and syncs localized aria-labels", () => {
  const html = read("batch-picker.html");
  const js = read("batch-picker.js");

  assert.match(html, /<html lang="en"/);
  assert.match(html, /id="statusBanner"[^>]*aria-live="polite"/);
  assert.match(html, /data-i18n-aria-label="batch\.min_size_title"/);
  assert.match(html, /data-i18n-aria-label="batch\.search_placeholder"/);
  assert.match(js, /data-i18n-aria-label/);
});

test("content.js Escape handler runs in capture phase, stops propagation, and resets dropdown when hidden", () => {
  const src = read("content.js");

  assert.match(src, /document\.addEventListener\("keydown"[\s\S]*?e\.key === "Escape"[\s\S]*?e\.preventDefault\(\)[\s\S]*?e\.stopPropagation\(\)[\s\S]*?\},\s*true\)/);
  assert.match(src, /if \(!isVideoRenderable\(activeVideo\)\)[\s\S]*?dropdownOpen = false;[\s\S]*?overlayEl\.classList\.add\("hidden"\)/);
});

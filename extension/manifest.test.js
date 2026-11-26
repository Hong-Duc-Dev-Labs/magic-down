const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");

const repoRoot = path.resolve(__dirname, "..");
const manifestPath = path.join(__dirname, "manifest.json");
const extConfigPath = path.join(repoRoot, "config", "extension-ids.json");
const versionGoPath = path.join(repoRoot, "pkg", "version", "version.go");

const EXT_ID_RE = /^[a-p]{32}$/;

test("extension/manifest.json is MV3 compliant and uses narrowed host_permissions", () => {
  const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  assert.equal(manifest.manifest_version, 3);
  assert.equal(manifest.background.service_worker, "background.js");
  assert.ok(Number(manifest.minimum_chrome_version) >= 111, "world: MAIN requires Chromium 111+");
  assert.ok(Array.isArray(manifest.host_permissions));
  assert.ok(!manifest.host_permissions.includes("<all_urls>"), "host_permissions should not use <all_urls>");
  assert.deepEqual(manifest.host_permissions, ["http://*/*", "https://*/*"]);
  assert.ok(manifest.permissions.includes("nativeMessaging"));
  assert.ok(manifest.permissions.includes("downloads"));
});

test("extension/manifest.json version stays synchronized with pkg/version/version.go", () => {
  const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  const versionGo = fs.readFileSync(versionGoPath, "utf8");
  const match = versionGo.match(/Version\s*=\s*"([^"]+)"/);
  assert.ok(match, "Version constant not found in pkg/version/version.go");
  assert.equal(manifest.version, match[1]);
});

test("config/extension-ids.json separates chrome/edge dev and prod IDs without guessed prod IDs", () => {
  const cfg = JSON.parse(fs.readFileSync(extConfigPath, "utf8"));
  assert.equal(cfg.schema_version, 1);
  assert.equal(cfg.host_name, "com.magicdown.downloader");
  assert.equal(cfg.packaging.strip_key_in_prod, true);

  for (const browser of ["chrome", "edge"]) {
    const entry = cfg.browsers[browser];
    assert.ok(entry, `missing browser config for ${browser}`);
    assert.match(entry.dev, EXT_ID_RE, `${browser}.dev must be a valid 32-char extension ID`);
    assert.equal(entry.prod, "", `${browser}.prod must remain empty placeholder until Store submission`);
    assert.ok(entry.store_url_template.includes("{id}"), `${browser}.store_url_template must include {id}`);
  }
});

test("extension/manifest.json public key derives the exact Extension ID in config/extension-ids.json", () => {
  const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  const cfg = JSON.parse(fs.readFileSync(extConfigPath, "utf8"));

  assert.equal(typeof manifest.key, "string", "manifest.json must contain a fixed public key for deterministic Load unpacked ID");
  assert.ok(manifest.key.trim().length > 0, "manifest.json key must not be empty");

  const der = Buffer.from(manifest.key, "base64");
  const digest = crypto.createHash("sha256").update(der).digest();
  let derivedId = "";
  for (let i = 0; i < 16; i++) {
    derivedId += String.fromCharCode(97 + (digest[i] >> 4));
    derivedId += String.fromCharCode(97 + (digest[i] & 0x0f));
  }

  assert.match(derivedId, EXT_ID_RE);
  assert.equal(derivedId, cfg.browsers.chrome.dev, "derived ID must match chrome.dev in config/extension-ids.json");
  assert.equal(derivedId, cfg.browsers.edge.dev, "derived ID must match edge.dev in config/extension-ids.json");
});

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const bg = require("./background.js");

test("detectBrowserId distinguishes Edge, Dia, Arc, Brave, Opera, Vivaldi, CocCoc, and Chrome", () => {
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"), "edge");
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Dia/1.0"), "dia");
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Arc/1.0"), "arc");
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Brave/1.70"), "brave");
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 OPR/115.0.0.0"), "opera");
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Vivaldi/7.0"), "vivaldi");
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 CocCoc/120.0"), "coccoc");
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36"), "chrome");
});

test("buildHelloPayload constructs valid HELLO handshake payload", () => {
  const payload = bg.buildHelloPayload({
    extensionVersion: "0.1.0",
    userAgent: "Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"
  });
  assert.equal(payload.type, "HELLO");
  assert.equal(payload.action, "hello");
  assert.equal(payload.protocolVersion, 1);
  assert.equal(payload.extensionVersion, "0.1.0");
  assert.equal(payload.browser, "edge");
});

test("handlePingMessage sends a real HELLO handshake so Desktop App reflects Connected state", async () => {
  let sentPayload = null;
  const mockSend = async (payload) => {
    sentPayload = payload;
    return {
      type: "WELCOME",
      action: "hello",
      protocolVersion: 1,
      hostVersion: "0.1.0",
      status: "connected",
      success: true,
      app_running: true
    };
  };

  const resp = await bg.handlePingMessage(mockSend, {
    extensionVersion: "0.1.0",
    userAgent: "Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"
  });

  assert.equal(sentPayload.type, "HELLO");
  assert.equal(sentPayload.action, "hello");
  assert.equal(sentPayload.browser, "edge");
  assert.equal(resp.success, true);
  assert.equal(resp.app_running, true);
  assert.equal(resp.status, "connected");
});

test("performHandshake reports offline when WELCOME indicates offline", async () => {
  const res = await bg.performHandshake(async () => ({
    type: "WELCOME",
    status: "offline",
    success: false,
    app_running: false,
    error: "MagicDown desktop app is not running"
  }));
  assert.equal(res.connected, false);
  assert.match(res.error, /not running/i);
});

test("extension/manifest.json declares alarms permission for MV3 heartbeat", () => {
  const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, "manifest.json"), "utf8"));
  assert.ok(manifest.permissions.includes("alarms"), "manifest.json must include alarms permission");
});

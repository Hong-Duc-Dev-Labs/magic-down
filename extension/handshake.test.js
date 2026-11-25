const test = require("node:test");
const assert = require("node:assert/strict");
const bg = require("./background.js");

test("detectBrowserId distinguishes Edge, Opera, Vivaldi, CocCoc, and Chrome", () => {
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"), "edge");
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 OPR/115.0.0.0"), "opera");
  assert.equal(bg.detectBrowserId("Mozilla/5.0 Chrome/131.0.0.0 Safari/537.36 Vivaldi/7.0"), "vivaldi");
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

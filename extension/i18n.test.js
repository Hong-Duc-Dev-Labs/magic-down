const test = require("node:test");
const assert = require("node:assert/strict");
const i18n = require("./i18n.js");

test("en and vi catalogs have 100% key parity and non-empty values", () => {
  const enKeys = Object.keys(i18n.CATALOGS.en);
  const viKeys = Object.keys(i18n.CATALOGS.vi);

  assert.ok(enKeys.length > 0, "en catalog must not be empty");
  assert.ok(viKeys.length > 0, "vi catalog must not be empty");

  for (const key of enKeys) {
    assert.ok(i18n.CATALOGS.en[key].trim().length > 0, `en key ${key} must be non-empty`);
    assert.ok(
      Object.prototype.hasOwnProperty.call(i18n.CATALOGS.vi, key),
      `missing vi translation for key: ${key}`
    );
    assert.ok(i18n.CATALOGS.vi[key].trim().length > 0, `vi key ${key} must be non-empty`);
  }

  for (const key of viKeys) {
    assert.ok(
      Object.prototype.hasOwnProperty.call(i18n.CATALOGS.en, key),
      `extra vi key not in en: ${key}`
    );
  }
});

test("normalizeLang, setLang, and interpolation work properly", () => {
  assert.equal(i18n.normalizeLang("vi-VN"), "vi");
  assert.equal(i18n.normalizeLang("VI"), "vi");
  assert.equal(i18n.normalizeLang("en-US"), "en");
  assert.equal(i18n.normalizeLang("ja-JP"), "en");

  i18n.setLang("vi");
  assert.equal(i18n.getLang(), "vi");
  assert.equal(i18n.t("overlay.download"), "Tải xuống");
  assert.equal(i18n.t("batch.download_selected", { n: 5 }), "Tải các mục đã chọn (5)");

  i18n.setLang("en");
  assert.equal(i18n.getLang(), "en");
  assert.equal(i18n.t("overlay.download"), "Download");
  assert.equal(i18n.t("batch.download_selected", { n: 5 }), "Download Selected (5)");
});

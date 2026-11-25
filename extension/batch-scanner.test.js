const test = require("node:test");
const assert = require("node:assert/strict");

const scanner = require("./batch-scanner.js");
const bg = require("./background.js");

test("normalizeResourceUrl resolves relative URLs and rejects non-http schemes", () => {
  const base = "https://gallery.example.com/albums/2026/index.html";
  assert.equal(
    scanner.normalizeResourceUrl("../img/photo.jpg#top", base),
    "https://gallery.example.com/albums/img/photo.jpg"
  );
  assert.equal(
    scanner.normalizeResourceUrl("//cdn.example.com/a.png", base),
    "https://cdn.example.com/a.png"
  );
  assert.equal(scanner.normalizeResourceUrl("data:image/png;base64,AAA", base), "");
  assert.equal(scanner.normalizeResourceUrl("blob:https://example.com/123", base), "");
  assert.equal(scanner.normalizeResourceUrl("javascript:void(0)", base), "");
});

test("parseSrcsetBest picks highest width or pixel density candidate", () => {
  const base = "https://example.com/post/";
  assert.equal(
    scanner.parseSrcsetBest("small.jpg 320w, medium.jpg 800w, large.jpg 1920w", base),
    "https://example.com/post/large.jpg"
  );
  assert.equal(
    scanner.parseSrcsetBest("hero@1x.webp 1x, hero@3x.webp 3x, hero@2x.webp 2x", base),
    "https://example.com/post/hero@3x.webp"
  );
});

test("scanDocument extracts img, picture, video, audio, links, CSS bg, deduplicates and drops tracking pixels", () => {
  const base = "https://example.com/page";

  function makeEl(tag, attrs = {}, extra = {}) {
    return {
      tagName: tag.toUpperCase(),
      getAttribute(k) {
        return k in attrs ? attrs[k] : null;
      },
      querySelectorAll(sel) {
        return extra.children && extra.children[sel] ? extra.children[sel] : [];
      },
      ...extra
    };
  }

  const imgs = [
    makeEl("img", {
      src: "/thumb.jpg",
      srcset: "/thumb.jpg 400w, /full-hd.jpg 1920w",
      alt: "Hero photo"
    }, { naturalWidth: 1920, naturalHeight: 1080 }),
    // Duplicate of full-hd.jpg should be deduplicated
    makeEl("img", { src: "https://example.com/full-hd.jpg" }, { naturalWidth: 1920, naturalHeight: 1080 }),
    // Lazy-loaded data-original
    makeEl("img", { "data-original": "/lazy-art.png", src: "data:image/gif;base64,R0lGODlhAQAB" }, { naturalWidth: 800, naturalHeight: 600 }),
    // 1x1 tracking pixel should be dropped
    makeEl("img", { src: "/pixel.gif" }, { naturalWidth: 1, naturalHeight: 1 })
  ];

  const pictureSources = [
    makeEl("source", { srcset: "/pic-1x.webp 1x, /pic-2x.webp 2x" })
  ];

  const mediaEls = [
    makeEl("video", { poster: "/poster.jpg" }, {
      children: {
        source: [makeEl("source", { src: "/clip.mp4" })]
      }
    }),
    makeEl("audio", { src: "/podcast.mp3" })
  ];

  const links = [
    makeEl("a", { href: "/docs/report.pdf" }, { textContent: "Annual Report" }),
    makeEl("a", { href: "/archive/bundle.zip" }, { textContent: "Bundle" }),
    makeEl("a", { href: "/about" }, { textContent: "About Us" }) // non-downloadable page link
  ];

  const bgDiv = makeEl("div", {}, { offsetWidth: 640, offsetHeight: 360 });

  const mockDoc = {
    querySelectorAll(sel) {
      if (sel === "img") return imgs;
      if (sel === "picture source") return pictureSources;
      if (sel === "video, audio") return mediaEls;
      if (sel === "a[href]") return links;
      if (sel.startsWith("div, section")) return [bgDiv];
      return [];
    }
  };

  const items = scanner.scanDocument(mockDoc, base, {
    getComputedStyle(el) {
      if (el === bgDiv) return { backgroundImage: 'url("/banner-bg.webp")' };
      return { backgroundImage: "none" };
    }
  });

  const urls = items.map((i) => i.url);
  assert.ok(urls.includes("https://example.com/full-hd.jpg"));
  assert.ok(urls.includes("https://example.com/lazy-art.png"));
  assert.ok(urls.includes("https://example.com/pic-2x.webp"));
  assert.ok(urls.includes("https://example.com/poster.jpg"));
  assert.ok(urls.includes("https://example.com/clip.mp4"));
  assert.ok(urls.includes("https://example.com/podcast.mp3"));
  assert.ok(urls.includes("https://example.com/docs/report.pdf"));
  assert.ok(urls.includes("https://example.com/archive/bundle.zip"));
  assert.ok(urls.includes("https://example.com/banner-bg.webp"));

  // Tracking pixel and normal HTML link must be excluded
  assert.ok(!urls.includes("https://example.com/pixel.gif"));
  assert.ok(!urls.includes("https://example.com/about"));

  // Deduplication check
  assert.equal(urls.filter((u) => u === "https://example.com/full-hd.jpg").length, 1);
});

test("filterResources filters by category, minImageSize, and search query", () => {
  const sample = [
    { url: "https://a.com/icon.png", category: "image", ext: "png", filename: "icon.png", width: 32, height: 32 },
    { url: "https://a.com/photo.jpg", category: "image", ext: "jpg", filename: "photo.jpg", width: 1200, height: 800 },
    { url: "https://a.com/video.mp4", category: "media", ext: "mp4", filename: "video.mp4", width: 0, height: 0 },
    { url: "https://a.com/spec.pdf", category: "file", ext: "pdf", filename: "spec.pdf", width: 0, height: 0 }
  ];

  const largeImages = scanner.filterResources(sample, { category: "image", minImageSize: 100 });
  assert.deepEqual(largeImages.map((i) => i.filename), ["photo.jpg"]);

  const pdfOnly = scanner.filterResources(sample, { category: "all", query: "pdf" });
  assert.deepEqual(pdfOnly.map((i) => i.filename), ["spec.pdf"]);
});

test("buildBatchPayload and background.buildBatchDownloadPayload produce valid batch_download payloads", () => {
  const items = [
    { url: "https://example.com/1.jpg", filename: "1.jpg" },
    { url: "https://example.com/1.jpg", filename: "dup.jpg" },
    { url: "https://example.com/2.pdf", filename: "2.pdf" }
  ];

  const payload = scanner.buildBatchPayload(items, {
    pageUrl: "https://example.com/gallery",
    subfolder: "My Gallery",
    userAgent: "MagicDownTestUA/1.0"
  });

  assert.equal(payload.action, "batch_download");
  assert.equal(payload.items.length, 2);
  assert.equal(payload.subfolder, "My Gallery");
  assert.equal(payload.referrer, "https://example.com/gallery");
  assert.equal(payload.headers["User-Agent"], "MagicDownTestUA/1.0");
  assert.equal(payload.items[0].skip_dialog, true);

  const bgPayload = bg.buildBatchDownloadPayload({
    items,
    pageUrl: "https://example.com/gallery",
    subfolder: "My Gallery",
    cookies: "sid=abc"
  });
  assert.equal(bgPayload.action, "batch_download");
  assert.equal(bgPayload.items.length, 2);
  assert.equal(bgPayload.cookies, "sid=abc");
  assert.equal(bgPayload.subfolder, "My Gallery");
});

test("suggestSubfolder sanitizes page titles safely", () => {
  assert.equal(
    scanner.suggestSubfolder('Gallery: Summer / 2026 * <Best>?', "https://example.com"),
    "Gallery Summer 2026 Best"
  );
  assert.equal(
    scanner.suggestSubfolder("", "https://www.photos.example.org/album"),
    "photos.example.org"
  );
});

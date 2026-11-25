(() => {
  const IMAGE_EXTS = new Set([
    "jpg", "jpeg", "png", "gif", "webp", "avif", "bmp", "svg", "ico", "tif", "tiff", "heic", "heif"
  ]);
  const VIDEO_EXTS = new Set([
    "mp4", "mkv", "webm", "mov", "avi", "m4v", "flv", "wmv", "ts", "m3u8", "mpd"
  ]);
  const AUDIO_EXTS = new Set([
    "mp3", "wav", "flac", "aac", "ogg", "m4a", "opus", "wma"
  ]);
  const FILE_EXTS = new Set([
    "pdf", "zip", "rar", "7z", "tar", "gz", "bz2", "xz", "iso", "dmg",
    "exe", "msi", "apk", "ipa", "deb", "rpm", "bin",
    "doc", "docx", "xls", "xlsx", "ppt", "pptx", "csv", "txt", "rtf", "epub", "mobi",
    "json", "xml", "sql", "psd", "ai", "fig", "sketch", "torrent"
  ]);

  const MAX_SCAN_ITEMS = 2000;

  function normalizeResourceUrl(rawUrl, baseUrl) {
    if (typeof rawUrl !== "string") return "";
    const trimmed = rawUrl.trim();
    if (!trimmed) return "";
    if (/^(data:|blob:|javascript:|mailto:|tel:|about:|chrome:|chrome-extension:|#)/i.test(trimmed)) {
      return "";
    }
    try {
      const resolved = baseUrl ? new URL(trimmed, baseUrl) : new URL(trimmed);
      if (resolved.protocol !== "http:" && resolved.protocol !== "https:") {
        return "";
      }
      resolved.hash = "";
      return resolved.toString();
    } catch {
      return "";
    }
  }

  function extractExtension(rawUrl) {
    try {
      const u = new URL(rawUrl);
      const pathname = u.pathname || "";
      const seg = pathname.split("/").filter(Boolean).pop() || "";
      const clean = seg.split("?")[0].split("#")[0];
      const dot = clean.lastIndexOf(".");
      if (dot > 0 && dot < clean.length - 1) {
        const ext = clean.slice(dot + 1).toLowerCase();
        if (/^[a-z0-9]{1,8}$/.test(ext)) {
          return ext;
        }
      }
      // Check common CDN format query params (e.g. ?format=jpg, ?fm=webp)
      const fmt = (u.searchParams.get("format") || u.searchParams.get("fm") || "").toLowerCase();
      if (fmt && /^[a-z0-9]{2,5}$/.test(fmt)) {
        return fmt;
      }
    } catch {
      // ignore
    }
    return "";
  }

  function parseSrcsetBest(srcset, baseUrl) {
    if (typeof srcset !== "string" || !srcset.trim()) return "";
    // Split on commas that are followed by optional whitespace and a URL-like token
    const entries = srcset
      .split(/,(?=\s*(?:https?:|\/\/|\/|\.{1,2}\/|[^\s,]+))/)
      .map((part) => part.trim())
      .filter(Boolean);

    let bestUrl = "";
    let bestScore = -1;

    for (const entry of entries) {
      const tokens = entry.split(/\s+/).filter(Boolean);
      if (tokens.length === 0) continue;
      const url = normalizeResourceUrl(tokens[0], baseUrl);
      if (!url) continue;

      let score = 1;
      if (tokens.length > 1) {
        const desc = tokens[1].toLowerCase();
        const wMatch = /^(\d+(?:\.\d+)?)w$/.exec(desc);
        const xMatch = /^(\d+(?:\.\d+)?)x$/.exec(desc);
        if (wMatch) {
          score = Number.parseFloat(wMatch[1]);
        } else if (xMatch) {
          score = Number.parseFloat(xMatch[1]) * 1000;
        }
      }

      if (score >= bestScore) {
        bestScore = score;
        bestUrl = url;
      }
    }
    return bestUrl;
  }

  function extractCssUrls(bgValue) {
    if (typeof bgValue !== "string" || !bgValue || bgValue === "none") return [];
    const results = [];
    const regex = /url\(\s*(['"]?)(.*?)\1\s*\)/gi;
    let match;
    while ((match = regex.exec(bgValue)) !== null) {
      if (match[2]) {
        results.push(match[2].trim());
      }
    }
    return results;
  }

  function classifyResource(url, hintType = "") {
    const ext = extractExtension(url);
    if (hintType === "image" || IMAGE_EXTS.has(ext)) {
      return { category: "image", ext: ext || "img" };
    }
    if (hintType === "media" || VIDEO_EXTS.has(ext) || AUDIO_EXTS.has(ext)) {
      return { category: "media", ext: ext || "media" };
    }
    if (hintType === "file" || FILE_EXTS.has(ext)) {
      return { category: "file", ext: ext || "bin" };
    }
    return { category: "", ext };
  }

  function sanitizeFilename(name) {
    return String(name || "")
      .replace(/[<>:"/\\|?*\x00-\x1f]/g, "_")
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 160);
  }

  function suggestFilename(url, fallbackLabel = "", ext = "") {
    try {
      const u = new URL(url);
      const seg = u.pathname.split("/").filter(Boolean).pop();
      if (seg) {
        const decoded = sanitizeFilename(decodeURIComponent(seg));
        if (decoded && decoded.length >= 2) {
          if (decoded.includes(".")) return decoded;
          if (ext && ext !== "img" && ext !== "media") return `${decoded}.${ext}`;
          return decoded;
        }
      }
    } catch {
      // ignore
    }
    const base = sanitizeFilename(fallbackLabel) || "resource";
    if (ext && ext !== "img" && ext !== "media" && !base.toLowerCase().endsWith("." + ext)) {
      return `${base}.${ext}`;
    }
    return base;
  }

  function suggestSubfolder(pageTitle, pageUrl) {
    const raw = String(pageTitle || "").trim();
    if (raw) {
      const cleaned = raw
        .replace(/[<>:"/\\|?*\x00-\x1f]/g, " ")
        .replace(/\s+/g, " ")
        .trim()
        .replace(/^\.+|\.+$/g, "")
        .slice(0, 80)
        .trim();
      if (cleaned) return cleaned;
    }
    try {
      const host = new URL(pageUrl).hostname.replace(/^www\./i, "");
      if (host) return sanitizeFilename(host);
    } catch {
      // ignore
    }
    return "MagicDown-Batch";
  }

  function isTrackingPixel(item) {
    if (item.category !== "image") return false;
    const w = item.width || 0;
    const h = item.height || 0;
    if (w > 0 && h > 0 && w <= 3 && h <= 3) return true;
    const lower = item.url.toLowerCase();
    if (/[/?&](pixel|beacon|track|spacer|1x1|blank)\.(gif|png|jpg)/i.test(lower)) return true;
    return false;
  }

  function scanDocument(doc, baseUrl, options = {}) {
    if (!doc) return [];
    const maxItems = typeof options.maxItems === "number" && options.maxItems > 0 ? options.maxItems : MAX_SCAN_ITEMS;
    const getComputedStyleFn = options.getComputedStyle || (typeof window !== "undefined" && typeof window.getComputedStyle === "function" ? window.getComputedStyle.bind(window) : null);

    const byUrl = new Map();

    function upsert(rawUrl, meta) {
      if (byUrl.size >= maxItems) return;
      const url = normalizeResourceUrl(rawUrl, baseUrl);
      if (!url) return;

      const classified = classifyResource(url, meta.hintType || "");
      if (!classified.category) return;

      const width = Number.isFinite(meta.width) && meta.width > 0 ? Math.round(meta.width) : 0;
      const height = Number.isFinite(meta.height) && meta.height > 0 ? Math.round(meta.height) : 0;

      const existing = byUrl.get(url);
      if (existing) {
        if (width > existing.width) existing.width = width;
        if (height > existing.height) existing.height = height;
        if (!existing.label && meta.label) existing.label = meta.label;
        return;
      }

      const item = {
        url,
        category: classified.category,
        ext: classified.ext,
        filename: suggestFilename(url, meta.label || "", classified.ext),
        label: String(meta.label || "").trim().slice(0, 120),
        width,
        height,
        source: meta.source || "dom"
      };

      if (isTrackingPixel(item)) return;
      byUrl.set(url, item);
    }

    // 1. <img> elements (highest quality srcset, currentSrc, src, lazy data-* attrs)
    const imgs = typeof doc.querySelectorAll === "function" ? doc.querySelectorAll("img") : [];
    for (const img of imgs) {
      const w = img.naturalWidth || img.width || 0;
      const h = img.naturalHeight || img.height || 0;
      const alt = (img.getAttribute && img.getAttribute("alt")) || img.alt || "";

      const srcsetBest =
        parseSrcsetBest(img.getAttribute ? img.getAttribute("srcset") : img.srcset, baseUrl) ||
        parseSrcsetBest(img.getAttribute ? img.getAttribute("data-srcset") : "", baseUrl);

      const candidates = [
        srcsetBest,
        img.getAttribute ? img.getAttribute("data-original") : "",
        img.getAttribute ? img.getAttribute("data-src") : "",
        img.getAttribute ? img.getAttribute("data-lazy-src") : "",
        img.currentSrc || "",
        img.getAttribute ? img.getAttribute("src") : img.src || ""
      ];

      let picked = "";
      for (const c of candidates) {
        const norm = normalizeResourceUrl(c, baseUrl);
        if (norm) {
          picked = norm;
          break;
        }
      }
      if (picked) {
        upsert(picked, { hintType: "image", width: w, height: h, label: alt, source: "img" });
      }
    }

    // 2. <picture><source srcset>
    const pictureSources = typeof doc.querySelectorAll === "function" ? doc.querySelectorAll("picture source") : [];
    for (const srcEl of pictureSources) {
      const srcset = srcEl.getAttribute ? (srcEl.getAttribute("srcset") || srcEl.getAttribute("data-srcset")) : "";
      const best = parseSrcsetBest(srcset, baseUrl);
      if (best) {
        upsert(best, { hintType: "image", source: "picture" });
      }
    }

    // 3. <video>, <audio>, <source>
    const mediaEls = typeof doc.querySelectorAll === "function" ? doc.querySelectorAll("video, audio") : [];
    for (const el of mediaEls) {
      const tag = (el.tagName || "").toLowerCase();
      const poster = tag === "video" && el.getAttribute ? el.getAttribute("poster") : "";
      if (poster) {
        upsert(poster, { hintType: "image", source: "poster" });
      }
      const src = el.currentSrc || (el.getAttribute ? el.getAttribute("src") : el.src) || "";
      if (src) {
        upsert(src, { hintType: "media", source: tag });
      }
      const sources = typeof el.querySelectorAll === "function" ? el.querySelectorAll("source") : [];
      for (const s of sources) {
        const sUrl = s.getAttribute ? s.getAttribute("src") : s.src;
        if (sUrl) {
          upsert(sUrl, { hintType: "media", source: `${tag}-source` });
        }
      }
    }

    // 4. <a href> pointing to images, media, or downloadable files
    const links = typeof doc.querySelectorAll === "function" ? doc.querySelectorAll("a[href]") : [];
    for (const a of links) {
      const href = a.getAttribute ? a.getAttribute("href") : a.href;
      if (!href) continue;
      const norm = normalizeResourceUrl(href, baseUrl);
      if (!norm) continue;
      const classified = classifyResource(norm, "");
      if (classified.category) {
        const label = (a.getAttribute && a.getAttribute("download")) || (a.textContent || "").trim() || "";
        upsert(norm, { hintType: classified.category, label, source: "link" });
      }
    }

    // 5. CSS background-image on elements
    if (getComputedStyleFn && typeof doc.querySelectorAll === "function") {
      const bgCandidates = doc.querySelectorAll("div, section, figure, span, a, header, article, li");
      const limit = Math.min(bgCandidates.length, 600);
      for (let i = 0; i < limit; i++) {
        const el = bgCandidates[i];
        let bg = "";
        try {
          bg = getComputedStyleFn(el).backgroundImage || "";
        } catch {
          continue;
        }
        if (!bg || bg === "none" || !bg.includes("url(")) continue;
        const urls = extractCssUrls(bg);
        const w = el.offsetWidth || 0;
        const h = el.offsetHeight || 0;
        for (const u of urls) {
          upsert(u, { hintType: "image", width: w, height: h, source: "css-bg" });
        }
      }
    }

    return Array.from(byUrl.values());
  }

  function filterResources(items, filters = {}) {
    if (!Array.isArray(items)) return [];
    const category = filters.category || "all";
    const minImageSize = typeof filters.minImageSize === "number" && filters.minImageSize > 0 ? filters.minImageSize : 0;
    const query = String(filters.query || "").trim().toLowerCase();

    return items.filter((item) => {
      if (!item || !item.url) return false;
      if (category !== "all" && item.category !== category) {
        return false;
      }
      if (item.category === "image" && minImageSize > 0) {
        const maxDim = Math.max(item.width || 0, item.height || 0);
        // Only filter out images whose dimensions are known and below threshold
        if (maxDim > 0 && maxDim < minImageSize) {
          return false;
        }
      }
      if (query) {
        const haystack = `${item.filename || ""} ${item.ext || ""} ${item.label || ""} ${item.url}`.toLowerCase();
        if (!haystack.includes(query)) {
          return false;
        }
      }
      return true;
    });
  }

  function buildBatchPayload(items, context = {}) {
    const safeItems = [];
    const seen = new Set();
    for (const raw of Array.isArray(items) ? items : []) {
      if (!raw || typeof raw.url !== "string") continue;
      const url = normalizeResourceUrl(raw.url, context.pageUrl || "");
      if (!url || seen.has(url)) continue;
      seen.add(url);
      safeItems.push({
        url,
        filename: sanitizeFilename(raw.filename || suggestFilename(url, raw.label || "", raw.ext || "")),
        referrer: raw.referrer || context.pageUrl || "",
        auto_start: true,
        skip_dialog: true
      });
    }

    const headers = {};
    if (context.userAgent && typeof context.userAgent === "string") {
      headers["User-Agent"] = context.userAgent;
    } else if (typeof navigator !== "undefined" && navigator.userAgent) {
      headers["User-Agent"] = navigator.userAgent;
    }

    return {
      action: "batch_download",
      items: safeItems,
      referrer: context.pageUrl || "",
      subfolder: typeof context.subfolder === "string" ? context.subfolder.trim() : "",
      headers,
      auto_start: true
    };
  }

  const api = {
    IMAGE_EXTS,
    VIDEO_EXTS,
    AUDIO_EXTS,
    FILE_EXTS,
    normalizeResourceUrl,
    extractExtension,
    parseSrcsetBest,
    extractCssUrls,
    classifyResource,
    sanitizeFilename,
    suggestFilename,
    suggestSubfolder,
    isTrackingPixel,
    scanDocument,
    filterResources,
    buildBatchPayload
  };

  if (typeof window !== "undefined") {
    window.MagicDownBatchScanner = api;
  }
  if (typeof module !== "undefined" && module.exports) {
    module.exports = api;
  }
})();

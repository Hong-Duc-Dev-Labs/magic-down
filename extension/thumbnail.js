// Pure video thumbnail extraction, URL normalization, and filename helpers.
// Loaded before content.js in the extension and directly importable in Node tests.
((root) => {
  "use strict";

  const FORMAT_BY_EXT = {
    jpg: { format: "JPG", ext: "jpg" },
    jpeg: { format: "JPG", ext: "jpg" },
    png: { format: "PNG", ext: "png" },
    webp: { format: "WEBP", ext: "webp" },
    gif: { format: "GIF", ext: "gif" },
    avif: { format: "AVIF", ext: "avif" }
  };

  function normalizeThumbnailUrl(rawUrl, baseUrl) {
    if (typeof rawUrl !== "string") return "";
    let trimmed = rawUrl.trim();
    if (!trimmed) return "";
    if (trimmed.startsWith("//")) {
      trimmed = `https:${trimmed}`;
    }
    if (/^(?:data:|blob:|javascript:|about:)/i.test(trimmed)) return "";
    try {
      const u = baseUrl ? new URL(trimmed, baseUrl) : new URL(trimmed);
      if (u.protocol !== "http:" && u.protocol !== "https:") return "";
      return u.toString();
    } catch {
      return "";
    }
  }

  function detectImageFormat(rawUrl) {
    const fallback = { format: "JPG", ext: "jpg" };
    if (typeof rawUrl !== "string" || !rawUrl.trim()) return fallback;
    try {
      const u = new URL(rawUrl, "https://example.com");
      const path = u.pathname.toLowerCase();
      const m = path.match(/\.([a-z0-9]{3,4})$/);
      if (m && FORMAT_BY_EXT[m[1]]) return FORMAT_BY_EXT[m[1]];
      for (const param of ["fm", "format", "f", "ext"]) {
        const val = (u.searchParams.get(param) || "").toLowerCase().trim();
        if (FORMAT_BY_EXT[val]) return FORMAT_BY_EXT[val];
      }
      // Bilibili/CDN style suffixes like @672w_378h_1c.webp
      const suffixMatch = path.match(/\.(jpg|jpeg|png|webp|gif|avif)(?:@|!|_|$)/i);
      if (suffixMatch && FORMAT_BY_EXT[suffixMatch[1].toLowerCase()]) {
        return FORMAT_BY_EXT[suffixMatch[1].toLowerCase()];
      }
    } catch {
      // ignore
    }
    return fallback;
  }

  function buildThumbnailFilename(preferredTitle, ext) {
    const cleanExt = (ext && FORMAT_BY_EXT[String(ext).toLowerCase()])
      ? FORMAT_BY_EXT[String(ext).toLowerCase()].ext
      : "jpg";
    const raw = String(preferredTitle || "video")
      .replace(/\s*-\s*YouTube\s*$/i, "")
      .replace(/_哔哩哔哩_bilibili\s*$/i, "")
      .replace(/\s*-\s*抖音\s*$/i, "")
      .replace(/[<>:"/\\|?*\x00-\x1f]/g, " ")
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 72);
    const base = raw || "video";
    return `${base}-thumbnail.${cleanExt}`;
  }

  function extractWeiboThumbnail(node, inheritedPagePic) {
    const candidates = [];
    const pushCandidate = (val, width, height) => {
      if (!val) return;
      if (typeof val === "string") {
        const url = normalizeThumbnailUrl(val);
        if (url) candidates.push({ url, width: Number(width) || 0, height: Number(height) || 0 });
      } else if (typeof val === "object") {
        const url = normalizeThumbnailUrl(val.url || val.pic_url || val.src || "");
        if (url) {
          candidates.push({
            url,
            width: Number(val.width || width) || 0,
            height: Number(val.height || height) || 0
          });
        }
      }
    };

    if (node && typeof node === "object") {
      if (node.big_pic_info && typeof node.big_pic_info === "object") {
        pushCandidate(node.big_pic_info.pic_big);
      }
      if (node.pic_info && typeof node.pic_info === "object") {
        pushCandidate(node.pic_info.pic_big);
      }
      pushCandidate(node.page_pic);
      pushCandidate(node.cover_image);
      pushCandidate(node.poster);
    }
    if (inheritedPagePic) {
      pushCandidate(inheritedPagePic);
    }

    if (candidates.length === 0) return null;
    const best = candidates[0];
    const fmt = detectImageFormat(best.url);
    return {
      url: best.url,
      width: best.width,
      height: best.height,
      format: fmt.format,
      ext: fmt.ext,
      source: "weibo"
    };
  }

  function isSingleVideoPage(pageUrl, videoCount) {
    if (typeof videoCount === "number" && videoCount > 1) return false;
    if (!pageUrl || typeof pageUrl !== "string") return videoCount <= 1;
    try {
      const u = new URL(pageUrl);
      const host = u.hostname.toLowerCase();
      const path = u.pathname.toLowerCase();

      // Multi-video feed homepages where og:image is a generic site logo/banner.
      if (path === "/" || path === "" || path === "/feed" || path === "/home") {
        if (
          host.endsWith("facebook.com") ||
          host.endsWith("weibo.com") ||
          host.endsWith("weibo.cn") ||
          host.endsWith("douyin.com") ||
          host.endsWith("tiktok.com") ||
          host.endsWith("youtube.com") ||
          host.endsWith("bilibili.com")
        ) {
          return false;
        }
      }
      if (host.endsWith("facebook.com") && (path === "/watch" || path === "/watch/") && !u.searchParams.get("v")) {
        return false;
      }
      return true;
    } catch {
      return videoCount <= 1;
    }
  }

  function extractCssBackgroundUrl(el) {
    if (!el) return "";
    let bg = "";
    try {
      if (el.style && el.style.backgroundImage) {
        bg = el.style.backgroundImage;
      } else if (typeof window !== "undefined" && typeof window.getComputedStyle === "function") {
        bg = window.getComputedStyle(el).backgroundImage || "";
      }
    } catch {
      return "";
    }
    if (!bg || bg === "none") return "";
    const m = bg.match(/url\((['"]?)(.*?)\1\)/i);
    return m && m[2] ? m[2].trim() : "";
  }

  function isLikelyIconOrAvatar(img) {
    const cls = String((img && img.className) || "").toLowerCase();
    const alt = String((img && img.alt) || "").toLowerCase();
    if (/(?:avatar|profile|icon|emoji|badge|logo)/i.test(cls)) return true;
    if (/(?:avatar|profile picture)/i.test(alt)) return true;
    return false;
  }

  function extractContainerCoverImage(videoEl, pageUrl) {
    if (!videoEl || !videoEl.parentElement) return null;
    let container = videoEl.parentElement;
    let best = null;
    let bestArea = 0;

    for (let depth = 0; depth < 4 && container; depth++) {
      // Stop climbing if this container holds multiple videos (feed container).
      if (typeof container.querySelectorAll === "function") {
        const videosInContainer = container.querySelectorAll("video");
        if (videosInContainer.length > 1) break;

        const imgs = container.querySelectorAll("img");
        for (const img of imgs) {
          if (isLikelyIconOrAvatar(img)) continue;
          const rawSrc = img.currentSrc || img.src || (typeof img.getAttribute === "function" ? img.getAttribute("src") : "");
          const url = normalizeThumbnailUrl(rawSrc, pageUrl);
          if (!url) continue;
          const w = Number(img.naturalWidth || img.width) || 0;
          const h = Number(img.naturalHeight || img.height) || 0;
          // Skip tiny icons/tracking pixels when dimensions are known.
          if ((w > 0 && w < 120) || (h > 0 && h < 80)) continue;
          const area = w > 0 && h > 0 ? w * h : 1;
          if (!best || area > bestArea) {
            bestArea = area;
            best = { url, width: w, height: h };
          }
        }

        if (!best) {
          const bgUrl = normalizeThumbnailUrl(extractCssBackgroundUrl(container), pageUrl);
          if (bgUrl) {
            best = { url: bgUrl, width: 0, height: 0 };
          }
        }
      }
      if (best) break;
      container = container.parentElement;
    }

    if (!best) return null;
    const fmt = detectImageFormat(best.url);
    return {
      url: best.url,
      width: best.width,
      height: best.height,
      format: fmt.format,
      ext: fmt.ext,
      source: "container"
    };
  }

  function extractMetaThumbnail(doc, pageUrl) {
    if (!doc || typeof doc.querySelector !== "function") return null;
    const selectors = [
      'meta[property="og:image:secure_url"]',
      'meta[property="og:image"]',
      'meta[name="twitter:image"]',
      'meta[property="twitter:image"]',
      'meta[itemprop="thumbnailUrl"]'
    ];
    for (const sel of selectors) {
      const el = doc.querySelector(sel);
      const raw = el && (el.content || (typeof el.getAttribute === "function" ? el.getAttribute("content") : ""));
      const url = normalizeThumbnailUrl(raw, pageUrl);
      if (!url) continue;
      const fmt = detectImageFormat(url);
      return {
        url,
        width: 0,
        height: 0,
        format: fmt.format,
        ext: fmt.ext,
        source: "meta"
      };
    }
    return null;
  }

  function extractDomVideoThumbnail(videoEl, doc, pageUrl, options) {
    const opts = options && typeof options === "object" ? options : {};
    const videoCount = typeof opts.videoCount === "number" ? opts.videoCount : 1;

    // 1. Direct <video poster="...">
    if (videoEl) {
      const rawPoster = videoEl.poster || (typeof videoEl.getAttribute === "function" ? videoEl.getAttribute("poster") : "");
      const posterUrl = normalizeThumbnailUrl(rawPoster, pageUrl);
      if (posterUrl) {
        const fmt = detectImageFormat(posterUrl);
        return {
          url: posterUrl,
          width: Number(videoEl.videoWidth) || 0,
          height: Number(videoEl.videoHeight) || 0,
          format: fmt.format,
          ext: fmt.ext,
          source: "poster"
        };
      }

      // 2. Cover/thumbnail image inside the video's own container
      const containerThumb = extractContainerCoverImage(videoEl, pageUrl);
      if (containerThumb) return containerThumb;
    }

    // 3. og:image / twitter:image only on single-video pages
    if (isSingleVideoPage(pageUrl, videoCount)) {
      return extractMetaThumbnail(doc, pageUrl);
    }

    return null;
  }

  const api = {
    normalizeThumbnailUrl,
    detectImageFormat,
    buildThumbnailFilename,
    extractWeiboThumbnail,
    isSingleVideoPage,
    extractContainerCoverImage,
    extractMetaThumbnail,
    extractDomVideoThumbnail
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = api;
  } else if (root) {
    root.MagicDownThumbnail = api;
  }
})(typeof globalThis !== "undefined" ? globalThis : typeof window !== "undefined" ? window : this);

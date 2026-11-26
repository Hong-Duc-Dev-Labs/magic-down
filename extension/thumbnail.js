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
      // Xiaohongshu / CDN transform suffixes like !nd_dft_wlteh_webp_3 or !nd_prv_wlteh_jpg_3
      const bangIdx = path.indexOf("!");
      if (bangIdx !== -1) {
        const afterBang = path.slice(bangIdx + 1);
        const xhsFmt = afterBang.match(/(?:^|[_-])(webp|jpg|jpeg|png|gif|avif)(?:[_-]|$)/i);
        if (xhsFmt && FORMAT_BY_EXT[xhsFmt[1].toLowerCase()]) {
          return FORMAT_BY_EXT[xhsFmt[1].toLowerCase()];
        }
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
      .replace(/^\(\d+\)\s*/, "")
      .replace(/\s*-\s*YouTube\s*$/i, "")
      .replace(/_哔哩哔哩_bilibili\s*$/i, "")
      .replace(/\s*-\s*抖音\s*$/i, "")
      .replace(/\s*-\s*小红书\s*$/i, "")
      .replace(/[<>:"/\\|?*\x00-\x1f]/g, " ")
      .replace(/\.\.+/g, ".")
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 72)
      .replace(/^[\s.-]+|[\s.-]+$/g, "");
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
      if (path === "/" || path === "" || path === "/feed" || path === "/home" || path === "/explore" || path === "/explore/") {
        if (
          host.endsWith("facebook.com") ||
          host.endsWith("weibo.com") ||
          host.endsWith("weibo.cn") ||
          host.endsWith("douyin.com") ||
          host.endsWith("tiktok.com") ||
          host.endsWith("youtube.com") ||
          host.endsWith("bilibili.com") ||
          host.endsWith("xiaohongshu.com")
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

  // --- Xiaohongshu (RedNote) outer post cover extraction ---------------------
  function isXiaohongshuHost(hostname) {
    const h = String(hostname || "").toLowerCase().trim();
    if (!h) return false;
    return h === "xiaohongshu.com" || h.endsWith(".xiaohongshu.com") || h === "xhslink.com" || h.endsWith(".xhslink.com");
  }

  function extractXiaohongshuNoteId(rawUrl) {
    if (!rawUrl || typeof rawUrl !== "string") return "";
    try {
      const u = new URL(rawUrl, "https://www.xiaohongshu.com");
      if (!isXiaohongshuHost(u.hostname)) return "";
      const parts = u.pathname.split("/").filter(Boolean);
      // /explore/<noteId>, /discovery/item/<noteId>, /note/<noteId>, /user/profile/<uid>/<noteId>
      if (parts.length >= 2 && (parts[0] === "explore" || parts[0] === "note")) {
        return /^[A-Za-z0-9_-]{16,36}$/.test(parts[1]) ? parts[1] : "";
      }
      if (parts.length >= 3 && parts[0] === "discovery" && parts[1] === "item") {
        return /^[A-Za-z0-9_-]{16,36}$/.test(parts[2]) ? parts[2] : "";
      }
      if (parts.length >= 4 && parts[0] === "user" && parts[1] === "profile") {
        return /^[A-Za-z0-9_-]{16,36}$/.test(parts[3]) ? parts[3] : "";
      }
      const qid = u.searchParams.get("note_id") || u.searchParams.get("noteId") || "";
      if (qid && /^[A-Za-z0-9_-]{16,36}$/.test(qid)) return qid;
    } catch {
      // ignore malformed URL
    }
    return "";
  }

  function unwrapVueRef(val) {
    if (val && typeof val === "object") {
      if ("_rawValue" in val && val._rawValue && typeof val._rawValue === "object") return val._rawValue;
      if ("value" in val && val.value && typeof val.value === "object" && !("note" in val) && !("imageList" in val) && !("image_list" in val)) {
        return val.value;
      }
    }
    return val;
  }

  function pickXiaohongshuImageUrl(imgObj) {
    if (!imgObj) return null;
    if (typeof imgObj === "string") {
      const url = normalizeThumbnailUrl(imgObj);
      return url ? { url, width: 0, height: 0 } : null;
    }
    if (typeof imgObj !== "object") return null;

    const width = Number(imgObj.width) || 0;
    const height = Number(imgObj.height) || 0;

    // 1. Direct urlDefault / url_default (full outer cover)
    const directDefault = normalizeThumbnailUrl(imgObj.urlDefault || imgObj.url_default || "");
    if (directDefault) return { url: directDefault, width, height };

    // 2. infoList / info_list (prefer WB_DFT default cover over WB_PRV preview)
    const infoList = Array.isArray(imgObj.infoList)
      ? imgObj.infoList
      : Array.isArray(imgObj.info_list)
        ? imgObj.info_list
        : [];
    if (infoList.length > 0) {
      const dft = infoList.find((it) => it && String(it.imageScene || it.image_scene || "").toUpperCase() === "WB_DFT" && it.url);
      if (dft) {
        const u = normalizeThumbnailUrl(dft.url);
        if (u) return { url: u, width, height };
      }
      for (const it of infoList) {
        if (!it || typeof it.url !== "string") continue;
        const u = normalizeThumbnailUrl(it.url);
        if (u) return { url: u, width, height };
      }
    }

    // 3. urlPre / url_pre / url fallback
    for (const candidate of [imgObj.urlPre, imgObj.url_pre, imgObj.url]) {
      const u = normalizeThumbnailUrl(candidate || "");
      if (u) return { url: u, width, height };
    }

    return null;
  }

  function extractXiaohongshuCoverFromNote(note) {
    const n = unwrapVueRef(note);
    if (!n || typeof n !== "object") return null;
    const imageList = Array.isArray(n.imageList)
      ? n.imageList
      : Array.isArray(n.image_list)
        ? n.image_list
        : [];
    let picked = null;
    if (imageList.length > 0) {
      picked = pickXiaohongshuImageUrl(imageList[0]);
    }
    if (!picked && n.cover) {
      picked = pickXiaohongshuImageUrl(n.cover);
    }
    if (!picked || !picked.url) return null;

    const fmt = detectImageFormat(picked.url);
    return {
      url: picked.url,
      width: picked.width,
      height: picked.height,
      format: fmt.format,
      ext: fmt.ext,
      source: "xiaohongshu"
    };
  }

  function extractXiaohongshuInitialState(state, pageUrl) {
    const st = unwrapVueRef(state);
    if (!st || typeof st !== "object") return null;
    const targetNoteId = extractXiaohongshuNoteId(pageUrl || "");

    const noteMod = unwrapVueRef(st.note);
    if (noteMod && typeof noteMod === "object") {
      const detailMap = unwrapVueRef(noteMod.noteDetailMap);
      const currentNoteId = typeof noteMod.currentNoteId === "string"
        ? noteMod.currentNoteId
        : (noteMod.currentNoteId && typeof noteMod.currentNoteId.value === "string" ? noteMod.currentNoteId.value : "");
      const lookupId = targetNoteId || currentNoteId;

      if (detailMap && typeof detailMap === "object") {
        const tryEntry = (id, entry) => {
          const e = unwrapVueRef(entry);
          if (!e || typeof e !== "object") return null;
          const noteObj = unwrapVueRef(e.note || e);
          const thumb = extractXiaohongshuCoverFromNote(noteObj);
          if (!thumb) return null;
          return {
            noteId: String(id || (noteObj && (noteObj.noteId || noteObj.note_id || noteObj.id)) || ""),
            title: String((noteObj && (noteObj.title || noteObj.desc)) || "").trim().slice(0, 200),
            thumbnail: thumb
          };
        };

        if (lookupId && detailMap[lookupId]) {
          const hit = tryEntry(lookupId, detailMap[lookupId]);
          if (hit) return hit;
        }
        const keys = Object.keys(detailMap);
        if (!targetNoteId && keys.length === 1) {
          const hit = tryEntry(keys[0], detailMap[keys[0]]);
          if (hit) return hit;
        }
      }
    }

    // Fallback to feed.feeds list in __INITIAL_STATE__ when matching targetNoteId
    const feedMod = unwrapVueRef(st.feed);
    const feeds = feedMod && Array.isArray(unwrapVueRef(feedMod.feeds)) ? unwrapVueRef(feedMod.feeds) : [];
    if (targetNoteId && feeds.length > 0) {
      for (const item of feeds) {
        if (!item || typeof item !== "object") continue;
        const id = String(item.id || item.noteId || item.note_id || "");
        if (id !== targetNoteId) continue;
        const card = item.noteCard || item.note_card || item.note;
        const thumb = extractXiaohongshuCoverFromNote(card);
        if (thumb) {
          return {
            noteId: id,
            title: String((card && (card.displayTitle || card.display_title || card.title || card.desc)) || "").trim().slice(0, 200),
            thumbnail: thumb
          };
        }
      }
    }

    return null;
  }

  function extractXiaohongshuFeedResponse(json) {
    if (!json || typeof json !== "object") return [];
    const data = json.data && typeof json.data === "object" ? json.data : json;
    const items = Array.isArray(data.items) ? data.items : [];
    const results = [];
    for (const item of items) {
      if (!item || typeof item !== "object") continue;
      const card = item.note_card || item.noteCard || item.note || item;
      const noteId = String(item.id || item.note_id || item.noteId || (card && (card.note_id || card.noteId || card.id)) || "");
      const thumb = extractXiaohongshuCoverFromNote(card);
      if (!thumb) continue;
      results.push({
        noteId,
        title: String((card && (card.title || card.display_title || card.displayTitle || card.desc)) || "").trim().slice(0, 200),
        thumbnail: thumb
      });
    }
    return results;
  }

  function isLikelyXhsAvatarOrFrame(img, url) {
    if (isLikelyIconOrAvatar(img)) return true;
    const cls = String((img && img.className) || "").toLowerCase();
    const alt = String((img && img.alt) || "").toLowerCase();
    if (/(?:author|user-head|head-img|avatar|comment|emoji|sticker)/i.test(cls)) return true;
    if (/(?:avatar|头像)/i.test(alt)) return true;
    const lowUrl = String(url || "").toLowerCase();
    if (/sns-avatar-qc\.xhscdn\.com|\/avatar\//i.test(lowUrl)) return true;
    return false;
  }

  function pickBestXhsImg(imgs, pageUrl) {
    let best = null;
    let bestArea = 0;
    for (const img of imgs || []) {
      if (!img) continue;
      const rawSrc = img.currentSrc || img.src || (typeof img.getAttribute === "function" ? img.getAttribute("src") : "");
      const url = normalizeThumbnailUrl(rawSrc, pageUrl);
      if (!url || isLikelyXhsAvatarOrFrame(img, url)) continue;
      const w = Number(img.naturalWidth || img.width) || 0;
      const h = Number(img.naturalHeight || img.height) || 0;
      if ((w > 0 && w < 120) || (h > 0 && h < 80)) continue;
      const area = w > 0 && h > 0 ? w * h : 1;
      if (!best || area > bestArea) {
        bestArea = area;
        best = { url, width: w, height: h };
      }
    }
    if (!best) return null;
    const fmt = detectImageFormat(best.url);
    return {
      url: best.url,
      width: best.width,
      height: best.height,
      format: fmt.format,
      ext: fmt.ext,
      source: "xiaohongshu-dom"
    };
  }

  function extractXiaohongshuDomCover(videoEl, doc, pageUrl) {
    const noteId = extractXiaohongshuNoteId(pageUrl || "");

    // 1. Prefer the outer feed card cover <a href=".../<noteId>"><img> when viewing a note modal over /explore
    if (noteId && doc && typeof doc.querySelectorAll === "function") {
      try {
        const cardImgs = doc.querySelectorAll(`a[href*="${noteId}"] img`);
        const hit = pickBestXhsImg(cardImgs, pageUrl);
        if (hit) return hit;
      } catch {
        // ignore selector errors
      }
    }

    // 2. Active note modal / detail cover container (skipping avatars and video frames)
    if (doc && typeof doc.querySelectorAll === "function") {
      const modalSelectors = [
        "#noteContainer .cover img",
        ".note-detail-mask .cover img",
        ".note-container .cover img",
        "#noteContainer .img-container img",
        ".note-detail-mask .swiper-slide img",
        ".media-container .cover img"
      ];
      for (const sel of modalSelectors) {
        try {
          const hit = pickBestXhsImg(doc.querySelectorAll(sel), pageUrl);
          if (hit) return hit;
        } catch {
          // ignore
        }
      }
    }

    // 3. Closest note card around the active <video>
    if (videoEl && typeof videoEl.closest === "function") {
      const card = videoEl.closest('section.note-item, .note-item, [class*="note-card"]');
      if (card && typeof card.querySelectorAll === "function") {
        const hit = pickBestXhsImg(card.querySelectorAll('a.cover img, .cover img, img[class*="cover"]'), pageUrl);
        if (hit) return hit;
      }
    }

    // 4. Single-note page og:image (only when a specific noteId is in the URL)
    if (noteId) {
      const meta = extractMetaThumbnail(doc, pageUrl);
      if (meta && !/sns-avatar-qc\.xhscdn\.com/i.test(meta.url)) {
        return { ...meta, source: "xiaohongshu-meta" };
      }
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
    extractDomVideoThumbnail,
    isXiaohongshuHost,
    extractXiaohongshuNoteId,
    extractXiaohongshuCoverFromNote,
    extractXiaohongshuInitialState,
    extractXiaohongshuFeedResponse,
    extractXiaohongshuDomCover
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = api;
  } else if (root) {
    root.MagicDownThumbnail = api;
  }
})(typeof globalThis !== "undefined" ? globalThis : typeof window !== "undefined" ? window : this);

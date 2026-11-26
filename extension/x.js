// Pure X / Twitter detection, URL classification, GraphQL/REST media parsing,
// thumbnail extraction, and tweet-level video scoping helpers.
// Loaded before content.js in the extension and directly importable in Node tests.
((root) => {
  "use strict";

  const X_HOSTS = new Set([
    "x.com",
    "www.x.com",
    "mobile.x.com",
    "m.x.com",
    "twitter.com",
    "www.twitter.com",
    "mobile.twitter.com",
    "m.twitter.com"
  ]);

  const MP3_PRESETS = [
    { quality: "MP3 · 320 kbps", bitrate: "320k", kbps: 320 },
    { quality: "MP3 · 192 kbps", bitrate: "192k", kbps: 192 },
    { quality: "MP3 · 128 kbps", bitrate: "128k", kbps: 128 }
  ];

  function isXHost(hostname) {
    const h = String(hostname || "").toLowerCase().trim();
    if (!h) return false;
    return X_HOSTS.has(h) || h.endsWith(".x.com") || h.endsWith(".twitter.com");
  }

  function isXCDNHost(hostname) {
    const h = String(hostname || "").toLowerCase().trim();
    if (!h) return false;
    return h === "twimg.com" || h.endsWith(".twimg.com");
  }

  function isXApiEndpoint(rawUrl) {
    if (!rawUrl || typeof rawUrl !== "string") return false;
    try {
      const u = new URL(rawUrl, "https://x.com");
      const host = u.hostname.toLowerCase();
      if (!isXHost(host)) return false;
      const p = u.pathname.toLowerCase();
      return (
        p.startsWith("/i/api/graphql/") ||
        p.startsWith("/i/api/1.1/") ||
        p.startsWith("/i/api/2/") ||
        p.startsWith("/graphql/")
      );
    } catch {
      return false;
    }
  }

  function isXMediaSegmentURL(rawUrl) {
    if (!rawUrl || typeof rawUrl !== "string") return false;
    try {
      const u = new URL(rawUrl);
      const host = u.hostname.toLowerCase();
      if (host !== "video.twimg.com" && !host.endsWith(".video.twimg.com")) {
        return false;
      }
      const path = u.pathname.toLowerCase();
      if (/\.(?:m4s|ts|aac|cmfv|cmfa)(?:$|\?)/i.test(path)) {
        return true;
      }
      // Segmented HLS/fMP4 audio/video chunks under /aud/ or /vid/ with segment paths
      if (/\/(?:aud|vid)\/[^/]*\/[^/]+\.(?:m4s|ts|aac)$/i.test(path)) {
        return true;
      }
    } catch {
      // ignore malformed URL
    }
    return false;
  }

  function extractXTweetId(rawUrl) {
    if (!rawUrl || typeof rawUrl !== "string") return "";
    try {
      const u = new URL(rawUrl, "https://x.com");
      if (!isXHost(u.hostname)) return "";
      const m = u.pathname.match(/\/(?:status|statuses)\/(\d{5,25})(?:\/|$)/i);
      return m ? m[1] : "";
    } catch {
      return "";
    }
  }

  function getXRequestHeaders(pageUrl, userAgent) {
    let referer = "https://x.com/";
    let origin = "https://x.com";
    if (typeof pageUrl === "string" && pageUrl.trim()) {
      try {
        const u = new URL(pageUrl.trim());
        if (isXHost(u.hostname)) {
          referer = u.toString();
          origin = u.origin;
        }
      } catch {
        // fallback to default x.com
      }
    }
    const headers = {
      Referer: referer,
      Origin: origin
    };
    if (typeof userAgent === "string" && userAgent.trim()) {
      headers["User-Agent"] = userAgent.trim();
    }
    return headers;
  }

  function detectXThumbFormat(rawUrl) {
    const fallback = { format: "JPG", ext: "jpg" };
    if (typeof rawUrl !== "string" || !rawUrl.trim()) return fallback;
    try {
      const u = new URL(rawUrl, "https://pbs.twimg.com");
      const fmtParam = (u.searchParams.get("format") || "").toLowerCase().trim();
      if (fmtParam === "png") return { format: "PNG", ext: "png" };
      if (fmtParam === "webp") return { format: "WEBP", ext: "webp" };
      if (fmtParam === "jpg" || fmtParam === "jpeg") return { format: "JPG", ext: "jpg" };
      const path = u.pathname.toLowerCase();
      if (path.endsWith(".png")) return { format: "PNG", ext: "png" };
      if (path.endsWith(".webp")) return { format: "WEBP", ext: "webp" };
      if (path.endsWith(".jpg") || path.endsWith(".jpeg")) return { format: "JPG", ext: "jpg" };
    } catch {
      // ignore
    }
    return fallback;
  }

  function upgradeXThumbnailUrl(rawUrl, sizeName = "orig") {
    if (typeof rawUrl !== "string") return "";
    let trimmed = rawUrl.trim();
    if (!trimmed) return "";
    if (trimmed.startsWith("//")) trimmed = `https:${trimmed}`;
    if (!/^https?:\/\//i.test(trimmed)) return "";
    try {
      const u = new URL(trimmed);
      const host = u.hostname.toLowerCase();
      if (host === "pbs.twimg.com" || host.endsWith(".twimg.com")) {
        // Strip legacy :small / :medium / :large / :orig path suffixes
        u.pathname = u.pathname.replace(/:(?:thumb|small|medium|large|orig)$/i, "");
        u.searchParams.set("name", sizeName || "orig");
      }
      return u.toString();
    } catch {
      return trimmed;
    }
  }

  function normalizeXPosterKey(rawUrl) {
    if (typeof rawUrl !== "string" || !rawUrl.trim()) return "";
    try {
      const u = new URL(rawUrl.trim(), "https://pbs.twimg.com");
      const cleanPath = u.pathname
        .replace(/:(?:thumb|small|medium|large|orig)$/i, "")
        .replace(/\.(?:jpg|jpeg|png|webp)$/i, "")
        .toLowerCase();
      return cleanPath || "";
    } catch {
      return "";
    }
  }

  function extractXThumbnail(mediaUrlHttps, fallbackPoster, originalInfo) {
    const raw = (typeof mediaUrlHttps === "string" && mediaUrlHttps.trim()) ||
      (typeof fallbackPoster === "string" && fallbackPoster.trim()) ||
      "";
    if (!raw) return null;
    const url = upgradeXThumbnailUrl(raw, "orig");
    if (!url) return null;
    const fmt = detectXThumbFormat(url);
    const width = originalInfo && Number(originalInfo.width) > 0 ? Number(originalInfo.width) : 0;
    const height = originalInfo && Number(originalInfo.height) > 0 ? Number(originalInfo.height) : 0;
    return {
      url,
      width,
      height,
      format: fmt.format,
      ext: fmt.ext,
      source: "x"
    };
  }

  function inferXVideoResolution(rawUrl, originalInfo) {
    if (typeof rawUrl === "string" && rawUrl) {
      try {
        const u = new URL(rawUrl);
        // X CDN video paths encode dimensions like:
        // /ext_tw_video/123/pu/vid/avc1/1280x720/abc.mp4
        // /amplify_video/123/vid/720x1280/xyz.mp4
        const m = u.pathname.match(/\/(\d{3,4})x(\d{3,4})(?:\/|$)/i);
        if (m) {
          const w = Number.parseInt(m[1], 10);
          const h = Number.parseInt(m[2], 10);
          if (w >= 144 && w <= 7680 && h >= 144 && h <= 7680) {
            const shortSide = Math.min(w, h);
            return {
              width: w,
              height: shortSide,
              quality: `${shortSide}p`
            };
          }
        }
        const pMatch = u.pathname.match(/(?:^|[_\-/])(2160|1440|1080|720|540|480|360|240)p(?:$|[_\-/])/i);
        if (pMatch) {
          const h = Number.parseInt(pMatch[1], 10);
          return { width: 0, height: h, quality: `${h}p` };
        }
      } catch {
        // ignore
      }
    }
    if (originalInfo && Number(originalInfo.width) > 0 && Number(originalInfo.height) > 0) {
      const w = Number(originalInfo.width);
      const h = Number(originalInfo.height);
      const shortSide = Math.min(w, h);
      if (shortSide >= 144 && shortSide <= 7680) {
        return { width: w, height: shortSide, quality: `${shortSide}p` };
      }
    }
    return { width: 0, height: 0, quality: "Original" };
  }

  function cleanTweetTitle(rawText, screenName, tweetId) {
    const cleaned = String(rawText || "")
      .replace(/https?:\/\/t\.co\/\S+/gi, "")
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 120);
    if (cleaned) {
      return screenName ? `@${screenName} - ${cleaned}` : cleaned;
    }
    if (screenName && tweetId) return `@${screenName} - ${tweetId}`;
    if (tweetId) return `X_${tweetId}`;
    return "";
  }

  function buildMp3Variants(sourceVariant, title) {
    if (!sourceVariant || typeof sourceVariant.url !== "string" || !sourceVariant.url) {
      return [];
    }
    return MP3_PRESETS.map((preset) => ({
      url: sourceVariant.url,
      quality: preset.quality,
      height: 0,
      width: 0,
      fps: 0,
      bandwidth: preset.kbps * 1000,
      size: 0,
      format: "MP3",
      ext: "mp3",
      outputFormat: "mp3",
      audioBitrate: preset.bitrate,
      isHLS: Boolean(sourceVariant.isHLS),
      source: "x",
      streamType: "audio-mp3",
      hasVideo: false,
      hasAudio: true,
      title: title || sourceVariant.title || "",
      referrer: sourceVariant.referrer || "",
      headers: sourceVariant.headers ? { ...sourceVariant.headers } : undefined
    }));
  }

  function buildXMediaEntry(rawEntry, meta) {
    if (!rawEntry || typeof rawEntry !== "object") return null;
    const rawVariants = Array.isArray(rawEntry.variants) ? rawEntry.variants : [];
    if (rawVariants.length === 0) return null;

    const tweetId = String(rawEntry.tweetId || "").trim();
    const mediaId = String(rawEntry.mediaId || "").trim();
    const mediaKey = String(rawEntry.mediaKey || "").trim();
    const posterUrl = typeof rawEntry.posterUrl === "string" ? rawEntry.posterUrl.trim() : "";
    const pageUrl = (meta && typeof meta.pageUrl === "string" && meta.pageUrl) ||
      (tweetId ? `https://x.com/i/status/${tweetId}` : "https://x.com/");
    const userAgent = (meta && typeof meta.userAgent === "string" && meta.userAgent) || "";
    const reqHeaders = getXRequestHeaders(pageUrl, userAgent);
    const referrer = reqHeaders.Referer;
    const title = (typeof rawEntry.title === "string" && rawEntry.title.trim()) ||
      cleanTweetTitle(rawEntry.text, rawEntry.screenName, tweetId);
    const originalInfo =
      (rawEntry.originalInfo && typeof rawEntry.originalInfo === "object" && rawEntry.originalInfo) ||
      (Number(rawEntry.width) > 0 && Number(rawEntry.height) > 0
        ? { width: Number(rawEntry.width), height: Number(rawEntry.height) }
        : null);

    const mp4Variants = [];
    const hlsVariants = [];
    const seenUrls = new Set();

    for (const v of rawVariants) {
      if (!v || typeof v !== "object") continue;
      const url = typeof v.url === "string" ? v.url.trim() : "";
      if (!url || !/^https?:\/\//i.test(url) || seenUrls.has(url)) continue;
      seenUrls.add(url);

      const ct = String(v.content_type || v.contentType || "").toLowerCase();
      const lowerPath = url.split("?")[0].split("#")[0].toLowerCase();
      const isMp4 = ct === "video/mp4" || lowerPath.endsWith(".mp4");
      const isHls = ct.includes("mpegurl") || lowerPath.endsWith(".m3u8");
      const bitrate = Number(v.bitrate) || 0;

      if (isMp4) {
        const res = inferXVideoResolution(url, null);
        const quality = res.height > 0
          ? res.quality
          : bitrate > 0
            ? `${(bitrate / 1000000).toFixed(1)} Mbps`
            : "Original";
        mp4Variants.push({
          url,
          quality,
          height: res.height,
          width: res.width,
          fps: 0,
          bandwidth: bitrate,
          size: 0,
          format: "MP4",
          ext: "mp4",
          isHLS: false,
          source: "x",
          streamType: "progressive",
          hasVideo: true,
          hasAudio: true,
          tweetId,
          mediaId,
          title,
          referrer,
          headers: { ...reqHeaders }
        });
      } else if (isHls) {
        hlsVariants.push({
          url,
          quality: "HLS",
          height: 0,
          width: 0,
          fps: 0,
          bandwidth: bitrate,
          size: 0,
          format: "HLS",
          ext: "ts",
          isHLS: true,
          source: "x",
          streamType: "hls",
          hasVideo: true,
          hasAudio: true,
          tweetId,
          mediaId,
          title,
          referrer,
          headers: { ...reqHeaders }
        });
      }
    }

    // Sort progressive MP4 variants by bitrate descending, then resolution descending.
    mp4Variants.sort((a, b) => {
      if ((b.bandwidth || 0) !== (a.bandwidth || 0)) return (b.bandwidth || 0) - (a.bandwidth || 0);
      return (b.height || 0) - (a.height || 0);
    });

    // Prefer progressive MP4; fall back to HLS only when no MP4 exists.
    const variants = mp4Variants.length > 0 ? mp4Variants : hlsVariants;
    if (variants.length === 0) return null;

    const mp3Source = mp4Variants[0] || variants[0] || null;
    const mp3Variants = buildMp3Variants(mp3Source, title);
    const thumbnail = extractXThumbnail(posterUrl, "", originalInfo);
    const posterKey = normalizeXPosterKey(posterUrl);

    return {
      key: `${tweetId || "unknown"}:${mediaId || mediaKey || posterKey || variants[0].url}`,
      tweetId,
      mediaId,
      mediaKey,
      posterUrl,
      posterKey,
      title,
      variants,
      mp3Variants,
      thumbnail,
      at: Date.now()
    };
  }

  function extractTweetContext(node) {
    if (!node || typeof node !== "object") return null;
    const target =
      node.__typename === "TweetWithVisibilityResults" && node.tweet && typeof node.tweet === "object"
        ? node.tweet
        : node;
    const legacy = target.legacy && typeof target.legacy === "object" ? target.legacy : target;
    const tweetId = String(target.rest_id || legacy.id_str || legacy.id || "").trim();
    const text = typeof legacy.full_text === "string"
      ? legacy.full_text
      : (typeof legacy.text === "string" ? legacy.text : "");

    let screenName = "";
    try {
      const userObj =
        (target.core && target.core.user_results && target.core.user_results.result) ||
        target.user ||
        null;
      const userLegacy = userObj && (userObj.legacy || userObj.core || userObj);
      if (userLegacy && typeof userLegacy.screen_name === "string") {
        screenName = userLegacy.screen_name.trim();
      }
    } catch {
      // ignore
    }

    if (!tweetId && !text) return null;
    return { tweetId, text, screenName };
  }

  function parseXTweetGraphQL(json, meta) {
    if (!json || typeof json !== "object") return [];
    const results = [];
    const seenKeys = new Set();
    const stack = [{ node: json, ctx: { tweetId: "", text: "", screenName: "" } }];
    let guard = 0;

    while (stack.length > 0 && guard < 80000) {
      guard++;
      const frame = stack.pop();
      const node = frame && frame.node;
      let ctx = frame ? frame.ctx : { tweetId: "", text: "", screenName: "" };
      if (!node || typeof node !== "object") continue;

      if (Array.isArray(node)) {
        for (let i = node.length - 1; i >= 0; i--) {
          stack.push({ node: node[i], ctx });
        }
        continue;
      }

      // Update tweet context when entering a tweet-shaped object.
      if (
        node.rest_id ||
        (node.legacy && (node.legacy.id_str || node.legacy.extended_entities)) ||
        (node.id_str && (node.extended_entities || node.full_text || node.text))
      ) {
        const extracted = extractTweetContext(node);
        if (extracted && extracted.tweetId) {
          ctx = {
            tweetId: extracted.tweetId,
            text: extracted.text || ctx.text,
            screenName: extracted.screenName || ctx.screenName
          };
        }
      }

      // Check for video_info.variants directly on a media object.
      if (node.video_info && Array.isArray(node.video_info.variants) && node.video_info.variants.length > 0) {
        const entry = buildXMediaEntry(
          {
            tweetId: ctx.tweetId,
            mediaId: String(node.id_str || node.id || ""),
            mediaKey: String(node.media_key || ""),
            posterUrl: typeof node.media_url_https === "string"
              ? node.media_url_https
              : (typeof node.media_url === "string" ? node.media_url : ""),
            text: ctx.text,
            screenName: ctx.screenName,
            originalInfo: node.original_info || null,
            variants: node.video_info.variants
          },
          meta
        );
        if (entry && !seenKeys.has(entry.key)) {
          seenKeys.add(entry.key);
          results.push(entry);
        }
        continue;
      }

      const keys = Object.keys(node);
      for (let i = keys.length - 1; i >= 0; i--) {
        const k = keys[i];
        // Skip noisy non-media subtrees to keep traversal fast
        if (k === "ext_media_color" || k === "features" || k === "clientEventInfo" || k === "feedbackInfo") {
          continue;
        }
        const val = node[k];
        if (val && typeof val === "object") {
          stack.push({ node: val, ctx });
        }
      }
    }

    return results;
  }

  function extractTweetIdFromVideoElement(videoEl, pageUrl) {
    if (videoEl) {
      // 1. Check closest quoted-tweet or tweet article container for /status/<id> links.
      let container = null;
      if (typeof videoEl.closest === "function") {
        container =
          videoEl.closest('[role="link"][tabindex="0"]') ||
          videoEl.closest('article[data-testid="tweet"]') ||
          videoEl.closest("article");
      }

      // If we hit a small inner link wrapper without a status link, climb to the article.
      const article =
        videoEl && typeof videoEl.closest === "function"
          ? videoEl.closest('article[data-testid="tweet"], article')
          : null;

      const scopes = [];
      if (container && container !== article) scopes.push(container);
      if (article) scopes.push(article);

      // Walk up a few parents if no article tag (e.g. photo/video modal lightbox)
      if (scopes.length === 0 && videoEl.parentElement) {
        let p = videoEl.parentElement;
        for (let depth = 0; depth < 8 && p; depth++) {
          if (typeof p.querySelectorAll === "function") {
            const videos = p.querySelectorAll("video");
            if (videos.length > 1) break;
            scopes.push(p);
          }
          p = p.parentElement;
        }
      }

      for (const scope of scopes) {
        if (!scope || typeof scope.querySelectorAll !== "function") continue;
        // Prefer <a href*="/status/"><time> which is always the tweet's own permalink.
        const timeLinks = scope.querySelectorAll('a[href*="/status/"] time');
        for (const tEl of timeLinks) {
          const a = tEl && typeof tEl.closest === "function" ? tEl.closest("a[href]") : tEl && tEl.parentElement;
          const href = a && (a.href || (typeof a.getAttribute === "function" ? a.getAttribute("href") : ""));
          const id = extractXTweetId(href || "");
          if (id) return id;
        }
        // Fallback: any <a href*="/status/"> inside this scope (skip /analytics, /photo, /video? no, /status/ID/video/1 still has ID)
        const links = scope.querySelectorAll('a[href*="/status/"]');
        for (const a of links) {
          const href = a && (a.href || (typeof a.getAttribute === "function" ? a.getAttribute("href") : ""));
          const id = extractXTweetId(href || "");
          if (id) return id;
        }
      }
    }

    // Fallback to page URL only if it's a single-tweet status page
    return extractXTweetId(pageUrl || "");
  }

  function matchXEntryForVideo(entriesSource, { tweetId = "", posterUrl = "", pageUrl = "" } = {}) {
    if (!entriesSource) return null;
    const list = entriesSource instanceof Map
      ? Array.from(entriesSource.values())
      : Array.isArray(entriesSource)
        ? entriesSource
        : [];
    if (list.length === 0) return null;

    // 1. Exact poster match (most specific: distinguishes multiple videos in one tweet or quoted tweets)
    const targetPosterKey = normalizeXPosterKey(posterUrl);
    if (targetPosterKey) {
      for (let i = list.length - 1; i >= 0; i--) {
        const entry = list[i];
        if (!entry) continue;
        const entryPosterKey = entry.posterKey || normalizeXPosterKey(entry.posterUrl || (entry.thumbnail && entry.thumbnail.url) || "");
        if (entryPosterKey && entryPosterKey === targetPosterKey) {
          return entry;
        }
      }
    }

    // 2. Match by DOM-scoped tweetId
    const cleanTweetId = String(tweetId || "").trim();
    if (cleanTweetId) {
      for (let i = list.length - 1; i >= 0; i--) {
        const entry = list[i];
        if (entry && entry.tweetId === cleanTweetId) {
          return entry;
        }
      }
    }

    // 3. Match by page URL /status/<id> only when no conflicting DOM tweetId was found
    if (!cleanTweetId) {
      const pageTweetId = extractXTweetId(pageUrl);
      if (pageTweetId) {
        for (let i = list.length - 1; i >= 0; i--) {
          const entry = list[i];
          if (entry && entry.tweetId === pageTweetId) {
            return entry;
          }
        }
      }
    }

    return null;
  }

  const api = {
    MP3_PRESETS,
    isXHost,
    isXCDNHost,
    isXApiEndpoint,
    isXMediaSegmentURL,
    extractXTweetId,
    getXRequestHeaders,
    detectXThumbFormat,
    upgradeXThumbnailUrl,
    normalizeXPosterKey,
    extractXThumbnail,
    inferXVideoResolution,
    cleanTweetTitle,
    buildMp3Variants,
    buildXMediaEntry,
    parseXTweetGraphQL,
    extractTweetIdFromVideoElement,
    matchXEntryForVideo
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = api;
  } else if (root) {
    root.MagicDownX = api;
  }
})(typeof globalThis !== "undefined" ? globalThis : typeof window !== "undefined" ? window : this);

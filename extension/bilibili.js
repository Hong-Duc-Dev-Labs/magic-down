// Pure Bilibili detection, URL classification, and __playinfo__/playurl parsing helpers.
// Loaded before content.js in the extension and directly importable in Node tests.
((root) => {
  "use strict";

  const BILIBILI_HOSTS = new Set([
    "bilibili.com",
    "www.bilibili.com",
    "m.bilibili.com",
    "live.bilibili.com",
    "bangumi.bilibili.com"
  ]);

  const BILIBILI_QUALITY_MAP = {
    127: { quality: "8K", height: 4320, fps: 0 },
    126: { quality: "Dolby Vision", height: 2160, fps: 0 },
    125: { quality: "HDR", height: 2160, fps: 0 },
    120: { quality: "4K", height: 2160, fps: 0 },
    116: { quality: "1080p60", height: 1080, fps: 60 },
    112: { quality: "1080p+", height: 1080, fps: 0 },
    80: { quality: "1080p", height: 1080, fps: 0 },
    74: { quality: "720p60", height: 720, fps: 60 },
    64: { quality: "720p", height: 720, fps: 0 },
    32: { quality: "480p", height: 480, fps: 0 },
    16: { quality: "360p", height: 360, fps: 0 },
    6: { quality: "240p", height: 240, fps: 0 }
  };

  function isBilibiliHost(hostname) {
    const h = String(hostname || "").toLowerCase().trim();
    if (!h) return false;
    return BILIBILI_HOSTS.has(h) || h.endsWith(".bilibili.com");
  }

  function isBilibiliCDNHost(hostname) {
    const h = String(hostname || "").toLowerCase().trim();
    if (!h) return false;
    return (
      h === "bilivideo.com" ||
      h.endsWith(".bilivideo.com") ||
      h === "bilivideo.cn" ||
      h.endsWith(".bilivideo.cn")
    );
  }

  function isBilibiliPlayUrlEndpoint(rawUrl) {
    if (!rawUrl || typeof rawUrl !== "string") return false;
    try {
      const u = new URL(rawUrl, "https://www.bilibili.com");
      const p = u.pathname.toLowerCase();
      return (
        p === "/x/player/wbi/playurl" ||
        p === "/x/player/playurl" ||
        p === "/x/player/v2/playurl" ||
        p === "/pgc/player/web/playurl" ||
        p === "/pgc/player/web/v2/playurl"
      );
    } catch {
      return false;
    }
  }

  function isBilibiliMediaChunkURL(rawUrl) {
    if (!rawUrl || typeof rawUrl !== "string") return false;
    try {
      const u = new URL(rawUrl);
      if (isBilibiliCDNHost(u.hostname)) return true;
      const path = u.pathname.toLowerCase();
      if (path.endsWith(".m4s")) return true;
    } catch {
      // ignore malformed URL
    }
    return false;
  }

  function extractBilibiliVideoId(rawUrl) {
    if (!rawUrl || typeof rawUrl !== "string") return "";
    try {
      const u = new URL(rawUrl);
      if (!isBilibiliHost(u.hostname)) return "";
      const parts = u.pathname.split("/").filter(Boolean);
      const pParam = u.searchParams.get("p");
      const pNum = pParam ? Number.parseInt(pParam, 10) : 0;
      const suffix = Number.isFinite(pNum) && pNum > 1 ? `_p${pNum}` : "";

      // /video/BV1xx411c7mD or /video/av170001
      const videoIdx = parts.indexOf("video");
      if (videoIdx >= 0 && parts[videoIdx + 1]) {
        const seg = parts[videoIdx + 1];
        if (/^(?:BV[0-9A-Za-z]{10,}|av\d+)$/i.test(seg)) {
          return `${seg}${suffix}`;
        }
      }

      // /bangumi/play/ep12345 or /bangumi/play/ss12345
      if (parts[0] === "bangumi" && parts[1] === "play" && parts[2]) {
        if (/^(?:ep|ss)\d+$/i.test(parts[2])) {
          return parts[2].toLowerCase();
        }
      }

      // /festival/... ?bvid=BV...
      const bvid = u.searchParams.get("bvid");
      if (bvid && /^BV[0-9A-Za-z]{10,}$/i.test(bvid)) {
        return `${bvid}${suffix}`;
      }
    } catch {
      // ignore malformed URL
    }
    return "";
  }

  function getBilibiliRequestHeaders(pageUrl, userAgent) {
    let referer = "https://www.bilibili.com/";
    if (typeof pageUrl === "string" && pageUrl.trim()) {
      try {
        const u = new URL(pageUrl.trim());
        if (isBilibiliHost(u.hostname)) {
          referer = u.toString();
        }
      } catch {
        // fallback to default referer
      }
    }
    const headers = {
      Referer: referer,
      Origin: "https://www.bilibili.com"
    };
    if (typeof userAgent === "string" && userAgent.trim()) {
      headers["User-Agent"] = userAgent.trim();
    }
    return headers;
  }

  function pickTrackUrl(track) {
    if (!track || typeof track !== "object") return "";
    const candidates = [
      track.baseUrl,
      track.base_url,
      track.url,
      ...(Array.isArray(track.backupUrl) ? track.backupUrl : []),
      ...(Array.isArray(track.backup_url) ? track.backup_url : [])
    ];
    for (const c of candidates) {
      if (typeof c === "string" && /^https?:\/\//i.test(c.trim())) {
        return c.trim();
      }
    }
    return "";
  }

  function parseFps(rawFps) {
    if (typeof rawFps === "number" && Number.isFinite(rawFps)) return Math.round(rawFps);
    if (typeof rawFps === "string" && rawFps.trim()) {
      const str = rawFps.trim();
      if (str.includes("/")) {
        const [num, den] = str.split("/").map((n) => Number.parseFloat(n));
        if (Number.isFinite(num) && Number.isFinite(den) && den > 0) {
          return Math.round(num / den);
        }
      }
      const parsed = Number.parseFloat(str);
      if (Number.isFinite(parsed)) return Math.round(parsed);
    }
    return 0;
  }

  function resolveQualityInfo(id, height, fps) {
    const qn = Number(id) || 0;
    if (BILIBILI_QUALITY_MAP[qn]) {
      const mapped = BILIBILI_QUALITY_MAP[qn];
      return {
        quality: mapped.quality,
        height: height > 0 ? height : mapped.height,
        fps: fps > 0 ? fps : mapped.fps
      };
    }
    if (height >= 2160) return { quality: "4K", height, fps };
    if (height >= 1080) return { quality: fps >= 50 ? "1080p60" : "1080p", height, fps };
    if (height >= 720) return { quality: fps >= 50 ? "720p60" : "720p", height, fps };
    if (height > 0) return { quality: `${height}p`, height, fps };
    return { quality: "Original", height: 0, fps };
  }

  function estimateTrackSize(bandwidth, durationSec) {
    if (bandwidth > 0 && durationSec > 0) {
      return Math.round((bandwidth * durationSec) / 8);
    }
    return 0;
  }

  function isAvcCodec(track) {
    const codecid = Number(track && track.codecid) || 0;
    if (codecid === 7) return true;
    const codecs = String((track && track.codecs) || "").toLowerCase();
    return codecs.startsWith("avc1");
  }

  function normalizeBilibiliCoverUrl(rawUrl) {
    if (typeof rawUrl !== "string") return "";
    let url = rawUrl.trim();
    if (!url) return "";
    if (url.startsWith("//")) url = `https:${url}`;
    if (!/^https?:\/\//i.test(url)) return "";
    return url;
  }

  function detectBilibiliThumbFormat(url) {
    const path = String(url || "").split("?")[0].split("#")[0].toLowerCase();
    if (path.endsWith(".webp") || /\.webp(?:@|!|_|$)/i.test(path)) return { format: "WEBP", ext: "webp" };
    if (path.endsWith(".png") || /\.png(?:@|!|_|$)/i.test(path)) return { format: "PNG", ext: "png" };
    return { format: "JPG", ext: "jpg" };
  }

  function extractBilibiliThumbnail(stateOrMeta, fallbackOgImage) {
    const rawCandidates = [];
    if (typeof stateOrMeta === "string") {
      rawCandidates.push(stateOrMeta);
    } else if (stateOrMeta && typeof stateOrMeta === "object") {
      rawCandidates.push(
        stateOrMeta.cover,
        stateOrMeta.videoData && stateOrMeta.videoData.pic,
        stateOrMeta.epInfo && stateOrMeta.epInfo.cover,
        stateOrMeta.mediaInfo && stateOrMeta.mediaInfo.cover,
        stateOrMeta.ogImage
      );
    }
    if (typeof fallbackOgImage === "string") {
      rawCandidates.push(fallbackOgImage);
    }

    for (const raw of rawCandidates) {
      const url = normalizeBilibiliCoverUrl(raw);
      if (!url) continue;
      const fmt = detectBilibiliThumbFormat(url);
      return {
        url,
        width: 0,
        height: 0,
        format: fmt.format,
        ext: fmt.ext,
        source: "bilibili"
      };
    }
    return null;
  }

  function parseBilibiliPlayInfo(rawPayload, meta) {
    if (!rawPayload || typeof rawPayload !== "object") return null;

    // Unwrap standard Bilibili API / __playinfo__ envelopes:
    // - Regular video: { code: 0, data: { dash, durl, ... } }
    // - Bangumi PGC:   { code: 0, result: { dash, durl, ... } }
    // - Direct object: { dash, durl, ... }
    const data =
      (rawPayload.data && typeof rawPayload.data === "object" && rawPayload.data) ||
      (rawPayload.result && typeof rawPayload.result === "object" && rawPayload.result) ||
      rawPayload;

    const dash = data.dash && typeof data.dash === "object" ? data.dash : null;
    const durl = Array.isArray(data.durl) ? data.durl : [];

    const pageUrl = (meta && typeof meta.pageUrl === "string" && meta.pageUrl) ||
      (typeof rawPayload.pageUrl === "string" && rawPayload.pageUrl) ||
      "https://www.bilibili.com/";
    const userAgent = (meta && typeof meta.userAgent === "string" && meta.userAgent) || "";
    const title = ((meta && typeof meta.title === "string" && meta.title) ||
      (typeof rawPayload.title === "string" && rawPayload.title) ||
      "").trim();
    const videoId =
      (meta && typeof meta.videoId === "string" && meta.videoId) ||
      extractBilibiliVideoId(pageUrl);

    const reqHeaders = getBilibiliRequestHeaders(pageUrl, userAgent);
    const referrer = reqHeaders.Referer;
    const durationSec =
      (dash && Number(dash.duration) > 0 ? Number(dash.duration) : 0) ||
      (Number(data.timelength) > 0 ? Number(data.timelength) / 1000 : 0);

    const variants = [];

    if (dash) {
      const rawAudios = [];
      if (Array.isArray(dash.audio)) rawAudios.push(...dash.audio);
      if (dash.flac && dash.flac.audio && typeof dash.flac.audio === "object") {
        rawAudios.push(dash.flac.audio);
      }
      if (dash.dolby && Array.isArray(dash.dolby.audio)) {
        rawAudios.push(...dash.dolby.audio);
      }

      const audios = [];
      const seenAudioUrls = new Set();
      for (const a of rawAudios) {
        const url = pickTrackUrl(a);
        if (!url || seenAudioUrls.has(url)) continue;
        seenAudioUrls.add(url);
        const bandwidth = Number(a.bandwidth) || 0;
        const size = Number(a.size) || estimateTrackSize(bandwidth, durationSec);
        const kbps = bandwidth > 0 ? `${Math.round(bandwidth / 1000)} kbps` : "";
        audios.push({
          url,
          id: Number(a.id) || 0,
          quality: kbps ? `Audio ${kbps}` : "Audio",
          height: 0,
          width: 0,
          fps: 0,
          bandwidth,
          size,
          format: "M4A",
          ext: "m4a",
          isHLS: false,
          source: "bilibili",
          streamType: "audio-only",
          hasVideo: false,
          hasAudio: true,
          title,
          referrer,
          headers: { ...reqHeaders }
        });
      }
      audios.sort((a, b) => (b.bandwidth || 0) - (a.bandwidth || 0));
      const bestAudio = audios[0] || null;

      // Deduplicate DASH video tracks by quality tier, preferring AVC (codecid 7)
      // for broad MP4 muxing compatibility and highest bandwidth within a tier.
      const bestVideoByTier = new Map();
      const rawVideos = Array.isArray(dash.video) ? dash.video : [];
      for (const v of rawVideos) {
        const url = pickTrackUrl(v);
        if (!url) continue;
        const width = Number(v.width) || 0;
        const height = Number(v.height) || 0;
        const fps = parseFps(v.frameRate || v.frame_rate);
        const bandwidth = Number(v.bandwidth) || 0;
        const qInfo = resolveQualityInfo(v.id, height, fps);
        const tierKey = qInfo.quality;
        const avc = isAvcCodec(v);
        const size = Number(v.size) || estimateTrackSize(bandwidth, durationSec);

        const candidate = {
          url,
          id: Number(v.id) || 0,
          quality: qInfo.quality,
          height: qInfo.height,
          width,
          fps: qInfo.fps,
          bandwidth,
          size,
          avc,
          format: "MP4",
          ext: "mp4",
          isHLS: false,
          source: "bilibili",
          streamType: "video-only",
          hasVideo: true,
          hasAudio: false,
          title,
          referrer,
          headers: { ...reqHeaders }
        };

        const prev = bestVideoByTier.get(tierKey);
        if (
          !prev ||
          (candidate.avc && !prev.avc) ||
          (candidate.avc === prev.avc && (candidate.bandwidth || 0) > (prev.bandwidth || 0))
        ) {
          bestVideoByTier.set(tierKey, candidate);
        }
      }

      const sortedVideos = Array.from(bestVideoByTier.values()).sort((a, b) => {
        if ((b.height || 0) !== (a.height || 0)) return (b.height || 0) - (a.height || 0);
        if ((b.fps || 0) !== (a.fps || 0)) return (b.fps || 0) - (a.fps || 0);
        return (b.bandwidth || 0) - (a.bandwidth || 0);
      });

      for (const v of sortedVideos) {
        const { avc, ...cleanVideo } = v;
        if (bestAudio) {
          const combinedSize =
            cleanVideo.size > 0 && bestAudio.size > 0
              ? cleanVideo.size + bestAudio.size
              : cleanVideo.size || 0;
          variants.push({
            ...cleanVideo,
            audioUrl: bestAudio.url,
            audioSize: bestAudio.size || 0,
            audioFormat: bestAudio.format || "M4A",
            size: combinedSize,
            format: "MP4",
            ext: "mp4",
            streamType: "muxed",
            hasAudio: true,
            needsMux: true
          });
        } else {
          variants.push(cleanVideo);
        }
      }

      if (bestAudio) {
        variants.push(bestAudio);
      }
    }

    // Fallback to durl (FLV/MP4 progressive) when DASH has no video variants.
    if (variants.length === 0 && durl.length > 0) {
      const qInfo = resolveQualityInfo(data.quality, 0, 0);
      const rawFmt = String(data.format || "").toLowerCase();
      for (const seg of durl) {
        const url = pickTrackUrl(seg);
        if (!url) continue;
        const lowerPath = url.split("?")[0].split("#")[0].toLowerCase();
        const isFlv = lowerPath.endsWith(".flv") || rawFmt.includes("flv");
        const format = isFlv ? "FLV" : "MP4";
        const ext = isFlv ? "flv" : "mp4";
        variants.push({
          url,
          id: Number(data.quality) || 0,
          quality: qInfo.quality,
          height: qInfo.height,
          width: 0,
          fps: qInfo.fps,
          bandwidth: 0,
          size: Number(seg.size) || 0,
          format,
          ext,
          isHLS: false,
          source: "bilibili",
          streamType: "progressive",
          hasVideo: true,
          hasAudio: true,
          title,
          referrer,
          headers: { ...reqHeaders }
        });
      }
    }

    if (variants.length === 0) return null;

    const thumbnail = extractBilibiliThumbnail(
      (meta && (meta.cover || meta.initialState)) || rawPayload.cover || rawPayload.initialState,
      meta && meta.ogImage
    );

    return {
      videoId,
      title,
      pageUrl,
      variants,
      thumbnail,
      at: Date.now()
    };
  }

  const api = {
    BILIBILI_QUALITY_MAP,
    isBilibiliHost,
    isBilibiliCDNHost,
    isBilibiliPlayUrlEndpoint,
    isBilibiliMediaChunkURL,
    extractBilibiliVideoId,
    getBilibiliRequestHeaders,
    extractBilibiliThumbnail,
    parseBilibiliPlayInfo
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = api;
  } else if (root) {
    root.MagicDownBilibili = api;
  }
})(typeof globalThis !== "undefined" ? globalThis : typeof window !== "undefined" ? window : this);

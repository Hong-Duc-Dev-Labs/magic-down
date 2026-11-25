// Pure YouTube detection, URL classification, and playerResponse parsing helpers.
// Loaded before content.js in the extension and directly importable in Node tests.
((root) => {
  "use strict";

  const YT_HOSTS = new Set([
    "youtube.com",
    "www.youtube.com",
    "m.youtube.com",
    "music.youtube.com",
    "youtube-nocookie.com",
    "www.youtube-nocookie.com"
  ]);

  function isYouTubeHost(hostname) {
    const h = String(hostname || "").toLowerCase().trim();
    if (!h) return false;
    return YT_HOSTS.has(h) || h.endsWith(".youtube.com") || h.endsWith(".youtube-nocookie.com");
  }

  function extractYouTubeVideoId(rawUrl) {
    if (!rawUrl || typeof rawUrl !== "string") return "";
    try {
      const u = new URL(rawUrl);
      const host = u.hostname.toLowerCase();
      if (host === "youtu.be") {
        const seg = u.pathname.split("/").filter(Boolean)[0] || "";
        return /^[A-Za-z0-9_-]{6,20}$/.test(seg) ? seg : "";
      }
      if (!isYouTubeHost(host)) return "";
      const v = u.searchParams.get("v");
      if (v && /^[A-Za-z0-9_-]{6,20}$/.test(v)) return v;
      const parts = u.pathname.split("/").filter(Boolean);
      if (parts.length >= 2 && (parts[0] === "shorts" || parts[0] === "embed" || parts[0] === "live" || parts[0] === "v")) {
        return /^[A-Za-z0-9_-]{6,20}$/.test(parts[1]) ? parts[1] : "";
      }
    } catch {
      // ignore malformed URL
    }
    return "";
  }

  function isGoogleVideoPlaybackURL(rawUrl) {
    if (!rawUrl || typeof rawUrl !== "string") return false;
    try {
      const u = new URL(rawUrl);
      const host = u.hostname.toLowerCase();
      return (host === "googlevideo.com" || host.endsWith(".googlevideo.com")) && u.pathname === "/videoplayback";
    } catch {
      return false;
    }
  }

  function isYouTubeRangeChunkURL(rawUrl) {
    if (!isGoogleVideoPlaybackURL(rawUrl)) return false;
    try {
      const u = new URL(rawUrl);
      return (
        u.searchParams.has("range") ||
        u.searchParams.has("ump") ||
        u.searchParams.has("rbuf") ||
        u.searchParams.has("rn")
      );
    } catch {
      return false;
    }
  }

  function parseMimeContainer(mimeType) {
    const raw = String(mimeType || "").toLowerCase();
    const base = raw.split(";")[0].trim();
    if (base === "video/mp4") return { kind: "video", format: "MP4", ext: "mp4" };
    if (base === "video/webm") return { kind: "video", format: "WebM", ext: "webm" };
    if (base === "video/3gpp") return { kind: "video", format: "3GP", ext: "3gp" };
    if (base === "audio/mp4") return { kind: "audio", format: "M4A", ext: "m4a" };
    if (base === "audio/webm") return { kind: "audio", format: "WebM", ext: "webm" };
    if (base.startsWith("video/")) return { kind: "video", format: "MP4", ext: "mp4" };
    if (base.startsWith("audio/")) return { kind: "audio", format: "M4A", ext: "m4a" };
    return { kind: "", format: "MP4", ext: "mp4" };
  }

  function parseFormatEntry(raw, isProgressive, title) {
    if (!raw || typeof raw !== "object") return { skip: true };
    if (raw.isDrm) return { skip: true, drm: true };
    const url = typeof raw.url === "string" ? raw.url.trim() : "";
    if (!url || !/^https?:\/\//i.test(url)) {
      return { skip: true, cipher: Boolean(raw.hasCipher || raw.signatureCipher || raw.cipher) };
    }
    const { kind, format, ext } = parseMimeContainer(raw.mimeType);
    if (!kind) return { skip: true };

    const height = Number(raw.height) || 0;
    const width = Number(raw.width) || 0;
    const fps = Number(raw.fps) || 0;
    const bitrate = Number(raw.bitrate) || 0;
    const size = Number(raw.contentLength) || 0;
    const itag = Number(raw.itag) || 0;

    if (isProgressive) {
      const quality = (typeof raw.qualityLabel === "string" && raw.qualityLabel) || (height > 0 ? `${height}p` : "Original");
      return {
        variant: {
          url,
          itag,
          quality,
          height,
          width,
          fps,
          bandwidth: bitrate,
          size,
          format,
          ext,
          isHLS: false,
          source: "youtube",
          streamType: "progressive",
          hasVideo: true,
          hasAudio: true,
          title: title || ""
        }
      };
    }

    if (kind === "video") {
      const quality = (typeof raw.qualityLabel === "string" && raw.qualityLabel) || (height > 0 ? `${height}p` : "Video");
      return {
        variant: {
          url,
          itag,
          quality,
          height,
          width,
          fps,
          bandwidth: bitrate,
          size,
          format,
          ext,
          isHLS: false,
          source: "youtube",
          streamType: "video-only",
          hasVideo: true,
          hasAudio: false,
          title: title || ""
        }
      };
    }

    const kbps = bitrate > 0 ? `${Math.round(bitrate / 1000)} kbps` : "";
    return {
      variant: {
        url,
        itag,
        quality: kbps ? `Audio ${kbps}` : "Audio",
        height: 0,
        width: 0,
        fps: 0,
        bandwidth: bitrate,
        size,
        format,
        ext,
        isHLS: false,
        source: "youtube",
        streamType: "audio-only",
        hasVideo: false,
        hasAudio: true,
        title: title || ""
      }
    };
  }

  function detectThumbFormat(url) {
    const path = String(url || "").split("?")[0].split("#")[0].toLowerCase();
    if (path.endsWith(".webp")) return { format: "WEBP", ext: "webp" };
    if (path.endsWith(".png")) return { format: "PNG", ext: "png" };
    return { format: "JPG", ext: "jpg" };
  }

  function isLetterboxedYouTubeThumb(url, width, height) {
    const lower = String(url || "").toLowerCase();
    if (/\/(?:maxresdefault|hq720|mqdefault)\.(?:jpg|webp)(?:$|\?)/i.test(lower)) return false;
    if (/\/(?:hqdefault|sddefault|default)\.(?:jpg|webp)(?:$|\?)/i.test(lower)) return true;
    if (width > 0 && height > 0) {
      const ratio = width / height;
      return ratio > 1.25 && ratio < 1.45; // 4:3 letterboxed
    }
    return false;
  }

  function extractYouTubeThumbnail(thumbnails, videoId) {
    const candidates = [];
    if (Array.isArray(thumbnails)) {
      for (const item of thumbnails) {
        if (!item || typeof item !== "object" || typeof item.url !== "string") continue;
        let url = item.url.trim();
        if (url.startsWith("//")) url = `https:${url}`;
        if (!/^https?:\/\//i.test(url)) continue;
        const width = Number(item.width) || 0;
        const height = Number(item.height) || 0;
        const area = width * height;
        const letterboxed = isLetterboxedYouTubeThumb(url, width, height);
        candidates.push({ url, width, height, area, letterboxed });
      }
    }

    if (candidates.length > 0) {
      candidates.sort((a, b) => {
        // Prefer non-letterboxed when both are high-res or same area
        if (b.area !== a.area) return b.area - a.area;
        if (a.letterboxed !== b.letterboxed) return a.letterboxed ? 1 : -1;
        return 0;
      });
      const best = candidates[0];
      const fmt = detectThumbFormat(best.url);
      return {
        url: best.url,
        width: best.width,
        height: best.height,
        format: fmt.format,
        ext: fmt.ext,
        source: "youtube"
      };
    }

    const cleanId = typeof videoId === "string" ? videoId.trim() : "";
    if (cleanId && /^[A-Za-z0-9_-]{6,20}$/.test(cleanId)) {
      return {
        url: `https://i.ytimg.com/vi/${cleanId}/maxresdefault.jpg`,
        width: 1280,
        height: 720,
        format: "JPG",
        ext: "jpg",
        source: "youtube"
      };
    }

    return null;
  }

  function parseYouTubePlayerPayload(payload) {
    if (!payload || typeof payload !== "object") return null;
    const videoId = typeof payload.videoId === "string" ? payload.videoId.trim() : "";
    const title = typeof payload.title === "string" ? payload.title.trim() : "";
    const isLive = Boolean(payload.isLive);
    const hlsManifestUrl = typeof payload.hlsManifestUrl === "string" ? payload.hlsManifestUrl.trim() : "";
    const dashManifestUrl = typeof payload.dashManifestUrl === "string" ? payload.dashManifestUrl.trim() : "";

    let cipherOnlyCount = 0;
    let drmDetected = false;

    const progressive = [];
    const progressiveSeen = new Set();
    for (const raw of Array.isArray(payload.formats) ? payload.formats : []) {
      const parsed = parseFormatEntry(raw, true, title);
      if (parsed.drm) drmDetected = true;
      if (parsed.cipher) cipherOnlyCount++;
      if (!parsed.variant) continue;
      if (progressiveSeen.has(parsed.variant.url)) continue;
      progressiveSeen.add(parsed.variant.url);
      progressive.push(parsed.variant);
    }

    // Deduplicate adaptive video by (height, fps, format) keeping highest bitrate,
    // and adaptive audio by container format keeping highest bitrate.
    const bestVideoByKey = new Map();
    const bestAudioByFormat = new Map();

    for (const raw of Array.isArray(payload.adaptiveFormats) ? payload.adaptiveFormats : []) {
      const parsed = parseFormatEntry(raw, false, title);
      if (parsed.drm) drmDetected = true;
      if (parsed.cipher) cipherOnlyCount++;
      if (!parsed.variant) continue;
      const v = parsed.variant;
      if (v.streamType === "video-only") {
        const key = `${v.height}|${v.fps}|${v.format}`;
        const prev = bestVideoByKey.get(key);
        if (!prev || (v.bandwidth || 0) > (prev.bandwidth || 0)) {
          bestVideoByKey.set(key, v);
        }
      } else if (v.streamType === "audio-only") {
        const prev = bestAudioByFormat.get(v.format);
        if (!prev || (v.bandwidth || 0) > (prev.bandwidth || 0)) {
          bestAudioByFormat.set(v.format, v);
        }
      }
    }

    progressive.sort((a, b) => (b.height || 0) - (a.height || 0) || (b.bandwidth || 0) - (a.bandwidth || 0));

    const adaptiveVideos = Array.from(bestVideoByKey.values()).sort((a, b) => {
      if ((b.height || 0) !== (a.height || 0)) return (b.height || 0) - (a.height || 0);
      if ((b.fps || 0) !== (a.fps || 0)) return (b.fps || 0) - (a.fps || 0);
      if (a.format !== b.format) return a.format === "MP4" ? -1 : 1;
      return (b.bandwidth || 0) - (a.bandwidth || 0);
    });

    const adaptiveAudios = Array.from(bestAudioByFormat.values()).sort(
      (a, b) => (b.bandwidth || 0) - (a.bandwidth || 0)
    );

    const bestM4A = bestAudioByFormat.get("M4A") || null;
    const bestAnyAudio = adaptiveAudios[0] || null;
    const pairedAdaptiveVideos = adaptiveVideos.map((v) => {
      const audio = bestM4A || bestAnyAudio;
      if (!audio) return v;
      const combinedSize = v.size > 0 && audio.size > 0 ? v.size + audio.size : (v.size || 0);
      return {
        ...v,
        audioUrl: audio.url,
        audioSize: audio.size || 0,
        audioFormat: audio.format || "M4A",
        size: combinedSize,
        format: "MP4",
        ext: "mp4",
        streamType: "muxed",
        hasAudio: true,
        needsMux: true
      };
    });

    const variants = [...progressive, ...pairedAdaptiveVideos, ...adaptiveAudios];
    const thumbnail = extractYouTubeThumbnail(payload.thumbnails, videoId);

    return {
      videoId,
      title,
      isLive,
      hlsManifestUrl,
      dashManifestUrl,
      variants,
      thumbnail,
      cipherOnlyCount,
      drmDetected,
      at: Date.now()
    };
  }

  const api = {
    isYouTubeHost,
    extractYouTubeVideoId,
    isGoogleVideoPlaybackURL,
    isYouTubeRangeChunkURL,
    parseMimeContainer,
    extractYouTubeThumbnail,
    parseYouTubePlayerPayload
  };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = api;
  } else if (root) {
    root.MagicDownYouTube = api;
  }
})(typeof globalThis !== "undefined" ? globalThis : typeof window !== "undefined" ? window : this);

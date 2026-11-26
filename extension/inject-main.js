// MAIN-world media sniffer for MagicDown.
//
// Facebook/Weibo-style players feed MSE from DASH/byte-range requests, so no
// plain media URL ever reaches chrome.webRequest. This script runs in the page's
// own JS world (it must patch the page's constructors) and only *reports*
// candidate URLs to content.js through window.postMessage.
//
// Rules: keep original return values, never throw into page code, no DOM work,
// bounded memory (last 200 URLs), one namespaced message type per batch.
(() => {
  "use strict";

  const FLAG = "__magicdownMainSniffer";
  if (window[FLAG]) return;
  Object.defineProperty(window, FLAG, { value: true, enumerable: false });

  const MSG = "magicdown:media-sniff:v1";
  const YT_MSG = "magicdown:yt-player:v1";
  const YT_PROBE_MSG = "magicdown:yt-probe:v1";
  const BILI_MSG = "magicdown:bili-playinfo:v1";
  const BILI_PROBE_MSG = "magicdown:bili-probe:v1";
  const XHS_MSG = "magicdown:xhs-note:v1";
  const XHS_PROBE_MSG = "magicdown:xhs-probe:v1";
  const X_MSG = "magicdown:x-media:v1";
  const X_PROBE_MSG = "magicdown:x-probe:v1";
  const MAX_ITEMS = 200;
  const MAX_URL = 4096;
  const MEDIA_PATH = /\.(?:mp4|m4s|m4v|m4a|m4p|webm|ts|m3u8|mpd|mov|mkv|mp3|aac)(?:$|[?#])/i;
  const RANGE_HINT = /(?:^|[?&])(?:bytestart|byteend|bytes|range)=/i;
  const IS_YOUTUBE = /(?:^|\.)(?:youtube\.com|youtube-nocookie\.com)$/i.test(location.hostname || "");
  const IS_BILIBILI = /(?:^|\.)bilibili\.com$/i.test(location.hostname || "");
  const IS_XHS = /(?:^|\.)xiaohongshu\.com$/i.test(location.hostname || "");
  const IS_X = /(?:^|\.)(?:x\.com|twitter\.com)$/i.test(location.hostname || "");

  const items = [];
  const pending = [];
  const ytPayloads = new Map(); // videoId -> compact payload
  const biliPayloads = new Map(); // pageUrl -> compact payload
  const xhsNotes = new Map(); // noteId -> { noteId, title, url, width, height }
  const xMediaCache = new Map(); // tweetId:mediaKey -> compact X media entry
  let flushTimer = 0;
  let lastFlush = 0;
  let seq = 0;

  function compactYTFormat(fmt) {
    if (!fmt || typeof fmt !== "object") return null;
    return {
      itag: Number(fmt.itag) || 0,
      url: typeof fmt.url === "string" ? fmt.url.slice(0, MAX_URL) : "",
      hasCipher: Boolean(fmt.signatureCipher || fmt.cipher),
      mimeType: typeof fmt.mimeType === "string" ? fmt.mimeType.slice(0, 120) : "",
      qualityLabel: typeof fmt.qualityLabel === "string" ? fmt.qualityLabel.slice(0, 32) : "",
      width: Number(fmt.width) || 0,
      height: Number(fmt.height) || 0,
      fps: Number(fmt.fps) || 0,
      bitrate: Number(fmt.bitrate) || 0,
      contentLength: Number(fmt.contentLength) || 0,
      isDrm: Boolean(fmt.drmFamilies && fmt.drmFamilies.length > 0)
    };
  }

  function emitYTPlayerResponse(pr) {
    try {
      if (!pr || typeof pr !== "object") return;
      const streamingData = pr.streamingData;
      const videoDetails = pr.videoDetails;
      if (!streamingData || typeof streamingData !== "object") return;
      const videoId = videoDetails && typeof videoDetails.videoId === "string" ? videoDetails.videoId.slice(0, 32) : "";
      const title = videoDetails && typeof videoDetails.title === "string" ? videoDetails.title.slice(0, 200) : "";
      const formats = Array.isArray(streamingData.formats)
        ? streamingData.formats.slice(0, 20).map(compactYTFormat).filter(Boolean)
        : [];
      const adaptiveFormats = Array.isArray(streamingData.adaptiveFormats)
        ? streamingData.adaptiveFormats.slice(0, 60).map(compactYTFormat).filter(Boolean)
        : [];
      const hlsManifestUrl =
        typeof streamingData.hlsManifestUrl === "string" ? streamingData.hlsManifestUrl.slice(0, MAX_URL) : "";
      const dashManifestUrl =
        typeof streamingData.dashManifestUrl === "string" ? streamingData.dashManifestUrl.slice(0, MAX_URL) : "";
      if (formats.length === 0 && adaptiveFormats.length === 0 && !hlsManifestUrl && !dashManifestUrl) return;

      const rawThumbs = videoDetails && videoDetails.thumbnail && Array.isArray(videoDetails.thumbnail.thumbnails)
        ? videoDetails.thumbnail.thumbnails
        : [];
      const thumbnails = rawThumbs
        .slice(0, 16)
        .filter((t) => t && typeof t.url === "string" && t.url.trim())
        .map((t) => ({
          url: t.url.trim().slice(0, MAX_URL),
          width: Number(t.width) || 0,
          height: Number(t.height) || 0
        }));

      const payload = {
        videoId,
        title,
        isLive: Boolean(videoDetails && videoDetails.isLiveContent),
        hlsManifestUrl,
        dashManifestUrl,
        formats,
        adaptiveFormats,
        thumbnails
      };
      const key = videoId || "_latest";
      ytPayloads.set(key, payload);
      if (ytPayloads.size > 10) {
        ytPayloads.delete(ytPayloads.keys().next().value);
      }
      window.postMessage({ __mdSniff: YT_MSG, payload }, "*");
    } catch {
      // never break page code
    }
  }

  function probeYouTubePlayers() {
    if (!IS_YOUTUBE) return;
    try {
      const playerIds = ["movie_player", "shorts-player", "c4-player"];
      for (const id of playerIds) {
        const el = document.getElementById(id);
        if (el && typeof el.getPlayerResponse === "function") {
          const pr = el.getPlayerResponse();
          if (pr) emitYTPlayerResponse(pr);
        }
      }
      if (window.ytInitialPlayerResponse) {
        emitYTPlayerResponse(window.ytInitialPlayerResponse);
      }
    } catch {
      // ignore
    }
  }

  function isBilibiliPlayUrl(url) {
    try {
      const u = new URL(url, location.href);
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

  function compactBiliTrack(track) {
    if (!track || typeof track !== "object") return null;
    const baseUrl =
      (typeof track.baseUrl === "string" && track.baseUrl) ||
      (typeof track.base_url === "string" && track.base_url) ||
      (typeof track.url === "string" && track.url) ||
      "";
    const backup = Array.isArray(track.backupUrl)
      ? track.backupUrl
      : Array.isArray(track.backup_url)
        ? track.backup_url
        : [];
    return {
      id: Number(track.id) || 0,
      baseUrl: baseUrl.slice(0, MAX_URL),
      backupUrl: backup.slice(0, 2).map((u) => (typeof u === "string" ? u.slice(0, MAX_URL) : "")).filter(Boolean),
      bandwidth: Number(track.bandwidth) || 0,
      mimeType: typeof track.mimeType === "string" ? track.mimeType.slice(0, 64) : (typeof track.mime_type === "string" ? track.mime_type.slice(0, 64) : ""),
      codecs: typeof track.codecs === "string" ? track.codecs.slice(0, 64) : "",
      width: Number(track.width) || 0,
      height: Number(track.height) || 0,
      frameRate: typeof track.frameRate === "string" ? track.frameRate.slice(0, 16) : (typeof track.frame_rate === "string" ? track.frame_rate.slice(0, 16) : ""),
      codecid: Number(track.codecid) || 0,
      size: Number(track.size) || 0
    };
  }

  function emitBilibiliPlayInfo(raw) {
    try {
      if (!raw || typeof raw !== "object") return;
      const data =
        (raw.data && typeof raw.data === "object" && raw.data) ||
        (raw.result && typeof raw.result === "object" && raw.result) ||
        raw;
      const dash = data.dash && typeof data.dash === "object" ? data.dash : null;
      const durl = Array.isArray(data.durl) ? data.durl : [];
      if (!dash && durl.length === 0) return;

      const compactDash = dash
        ? {
            duration: Number(dash.duration) || 0,
            video: Array.isArray(dash.video) ? dash.video.slice(0, 40).map(compactBiliTrack).filter(Boolean) : [],
            audio: Array.isArray(dash.audio) ? dash.audio.slice(0, 16).map(compactBiliTrack).filter(Boolean) : [],
            flac: dash.flac && dash.flac.audio ? { audio: compactBiliTrack(dash.flac.audio) } : null,
            dolby: dash.dolby && Array.isArray(dash.dolby.audio)
              ? { audio: dash.dolby.audio.slice(0, 4).map(compactBiliTrack).filter(Boolean) }
              : null
          }
        : null;

      const compactDurl = durl.slice(0, 10).map((seg) => ({
        url: typeof seg.url === "string" ? seg.url.slice(0, MAX_URL) : "",
        backup_url: Array.isArray(seg.backup_url)
          ? seg.backup_url.slice(0, 2).map((u) => (typeof u === "string" ? u.slice(0, MAX_URL) : "")).filter(Boolean)
          : [],
        size: Number(seg.size) || 0,
        length: Number(seg.length) || 0,
        order: Number(seg.order) || 0
      }));

      const pageUrl = location.href;
      let cover = "";
      try {
        const st = window.__INITIAL_STATE__;
        if (st && typeof st === "object") {
          const candidates = [
            st.videoData && st.videoData.pic,
            st.epInfo && st.epInfo.cover,
            st.mediaInfo && st.mediaInfo.cover
          ];
          for (const c of candidates) {
            if (typeof c === "string" && c.trim()) {
              cover = c.trim().slice(0, MAX_URL);
              break;
            }
          }
        }
      } catch {
        // ignore
      }
      const payload = {
        pageUrl,
        title: (document.title || "").slice(0, 200),
        cover,
        data: {
          quality: Number(data.quality) || 0,
          format: typeof data.format === "string" ? data.format.slice(0, 32) : "",
          timelength: Number(data.timelength) || 0,
          dash: compactDash,
          durl: compactDurl
        }
      };

      biliPayloads.set(pageUrl, payload);
      if (biliPayloads.size > 10) {
        biliPayloads.delete(biliPayloads.keys().next().value);
      }
      window.postMessage({ __mdSniff: BILI_MSG, payload }, "*");
    } catch {
      // never break page code
    }
  }

  function probeBilibiliPlayInfo() {
    if (!IS_BILIBILI) return;
    try {
      if (window.__playinfo__) {
        emitBilibiliPlayInfo(window.__playinfo__);
      }
    } catch {
      // ignore
    }
  }

  // --- Xiaohongshu (RedNote) outer post cover extraction ---------------------
  function isXhsApiUrl(url) {
    try {
      const u = new URL(url, location.href);
      const host = u.hostname.toLowerCase();
      if (host !== "xiaohongshu.com" && !host.endsWith(".xiaohongshu.com")) return false;
      return /\/api\/sns\/web\/v1\/(?:feed|homefeed|user_posted|search\/notes)(?:$|\?)/i.test(u.pathname);
    } catch {
      return false;
    }
  }

  function unwrapXhsRef(val) {
    if (val && typeof val === "object") {
      if ("_rawValue" in val && val._rawValue && typeof val._rawValue === "object") return val._rawValue;
      if ("value" in val && val.value && typeof val.value === "object" && !("note" in val) && !("imageList" in val) && !("image_list" in val)) {
        return val.value;
      }
    }
    return val;
  }

  function normalizeXhsCoverUrl(raw) {
    if (typeof raw !== "string") return "";
    let s = raw.trim();
    if (!s) return "";
    if (s.startsWith("//")) s = `https:${s}`;
    if (!/^https?:\/\//i.test(s)) return "";
    return s.slice(0, MAX_URL);
  }

  function pickXhsCoverFromImageObj(img) {
    if (!img) return null;
    if (typeof img === "string") {
      const url = normalizeXhsCoverUrl(img);
      return url ? { url, width: 0, height: 0 } : null;
    }
    if (typeof img !== "object") return null;
    const width = Number(img.width) || 0;
    const height = Number(img.height) || 0;
    const urlDefault = normalizeXhsCoverUrl(img.urlDefault || img.url_default || "");
    if (urlDefault) return { url: urlDefault, width, height };
    const infoList = Array.isArray(img.infoList) ? img.infoList : (Array.isArray(img.info_list) ? img.info_list : []);
    if (infoList.length > 0) {
      const dft = infoList.find((it) => it && String(it.imageScene || it.image_scene || "").toUpperCase() === "WB_DFT" && it.url);
      if (dft) {
        const u = normalizeXhsCoverUrl(dft.url);
        if (u) return { url: u, width, height };
      }
      for (const it of infoList) {
        if (!it || typeof it.url !== "string") continue;
        const u = normalizeXhsCoverUrl(it.url);
        if (u) return { url: u, width, height };
      }
    }
    for (const c of [img.urlPre, img.url_pre, img.url]) {
      const u = normalizeXhsCoverUrl(c || "");
      if (u) return { url: u, width, height };
    }
    return null;
  }

  function extractXhsNoteRecord(noteId, rawNote) {
    const note = unwrapXhsRef(rawNote);
    if (!note || typeof note !== "object") return null;
    const list = Array.isArray(note.imageList) ? note.imageList : (Array.isArray(note.image_list) ? note.image_list : []);
    const picked = (list.length > 0 ? pickXhsCoverFromImageObj(list[0]) : null) || (note.cover ? pickXhsCoverFromImageObj(note.cover) : null);
    if (!picked || !picked.url) return null;
    const id = String(noteId || note.noteId || note.note_id || note.id || "").slice(0, 48);
    if (!id) return null;
    const title = String(note.title || note.displayTitle || note.display_title || note.desc || "").trim().slice(0, 200);
    return { noteId: id, title, url: picked.url, width: picked.width, height: picked.height };
  }

  function emitXhsNotes(records) {
    if (!Array.isArray(records) || records.length === 0) return;
    const batch = [];
    for (const rec of records) {
      if (!rec || !rec.noteId || !rec.url) continue;
      xhsNotes.set(rec.noteId, rec);
      if (xhsNotes.size > 60) {
        xhsNotes.delete(xhsNotes.keys().next().value);
      }
      batch.push(rec);
    }
    if (batch.length > 0) {
      try {
        window.postMessage({ __mdSniff: XHS_MSG, notes: batch }, "*");
      } catch {
        // ignore
      }
    }
  }

  function probeXiaohongshuState() {
    if (!IS_XHS) return;
    try {
      const st = unwrapXhsRef(window.__INITIAL_STATE__);
      if (!st || typeof st !== "object") return;
      const found = [];
      const noteMod = unwrapXhsRef(st.note);
      const detailMap = noteMod && typeof noteMod === "object" ? unwrapXhsRef(noteMod.noteDetailMap) : null;
      if (detailMap && typeof detailMap === "object") {
        for (const [key, val] of Object.entries(detailMap)) {
          const entry = unwrapXhsRef(val);
          const rec = extractXhsNoteRecord(key, entry && entry.note ? entry.note : entry);
          if (rec) found.push(rec);
        }
      }
      const feedMod = unwrapXhsRef(st.feed);
      const feeds = feedMod && Array.isArray(unwrapXhsRef(feedMod.feeds)) ? unwrapXhsRef(feedMod.feeds) : [];
      for (const item of feeds.slice(0, 40)) {
        if (!item || typeof item !== "object") continue;
        const rec = extractXhsNoteRecord(item.id || item.noteId || item.note_id, item.noteCard || item.note_card || item.note);
        if (rec) found.push(rec);
      }
      emitXhsNotes(found);
    } catch {
      // ignore
    }
  }

  function inspectXhsApiJson(obj) {
    if (!obj || typeof obj !== "object") return;
    try {
      const data = obj.data && typeof obj.data === "object" ? obj.data : obj;
      const list = Array.isArray(data.items) ? data.items : [];
      const found = [];
      for (const item of list.slice(0, 40)) {
        if (!item || typeof item !== "object") continue;
        const card = item.note_card || item.noteCard || item.note || item;
        const rec = extractXhsNoteRecord(item.id || item.note_id || item.noteId, card);
        if (rec) found.push(rec);
      }
      emitXhsNotes(found);
    } catch {
      // ignore
    }
  }

  // --- X / Twitter GraphQL & REST media extraction ---------------------------
  function isXApiUrl(url) {
    try {
      const u = new URL(url, location.href);
      const host = u.hostname.toLowerCase();
      if (host !== "x.com" && !host.endsWith(".x.com") && host !== "twitter.com" && !host.endsWith(".twitter.com")) {
        return false;
      }
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

  function extractXTweetCtx(node) {
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
        screenName = userLegacy.screen_name.trim().slice(0, 64);
      }
    } catch {
      // ignore
    }
    if (!tweetId && !text) return null;
    return { tweetId: tweetId.slice(0, 32), text: text.slice(0, 280), screenName };
  }

  function extractXMediaEntries(data) {
    if (!data || typeof data !== "object") return [];
    const found = [];
    const seen = new Set();
    const stack = [{ node: data, ctx: { tweetId: "", text: "", screenName: "" } }];
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

      if (
        node.rest_id ||
        (node.legacy && (node.legacy.id_str || node.legacy.extended_entities)) ||
        (node.id_str && (node.extended_entities || node.full_text || node.text))
      ) {
        const extracted = extractXTweetCtx(node);
        if (extracted && extracted.tweetId) {
          ctx = {
            tweetId: extracted.tweetId,
            text: extracted.text || ctx.text,
            screenName: extracted.screenName || ctx.screenName
          };
        }
      }

      if (node.video_info && Array.isArray(node.video_info.variants) && node.video_info.variants.length > 0) {
        const variants = [];
        for (const v of node.video_info.variants.slice(0, 16)) {
          if (!v || typeof v.url !== "string" || !/^https?:\/\//i.test(v.url)) continue;
          variants.push({
            url: v.url.slice(0, MAX_URL),
            content_type: typeof v.content_type === "string" ? v.content_type.slice(0, 64) : "",
            bitrate: Number(v.bitrate) || 0
          });
        }
        if (variants.length > 0) {
          const mediaId = String(node.id_str || node.id || "").slice(0, 32);
          const mediaKey = String(node.media_key || "").slice(0, 48);
          const posterUrl = typeof node.media_url_https === "string"
            ? node.media_url_https.slice(0, MAX_URL)
            : (typeof node.media_url === "string" ? node.media_url.slice(0, MAX_URL) : "");
          const key = `${ctx.tweetId || "unknown"}:${mediaId || mediaKey || posterUrl || variants[0].url}`;
          if (!seen.has(key)) {
            seen.add(key);
            found.push({
              key,
              tweetId: ctx.tweetId,
              mediaId,
              mediaKey,
              posterUrl,
              text: ctx.text,
              screenName: ctx.screenName,
              width: Number(node.original_info && node.original_info.width) || 0,
              height: Number(node.original_info && node.original_info.height) || 0,
              variants
            });
          }
        }
        continue;
      }

      const keys = Object.keys(node);
      for (let i = keys.length - 1; i >= 0; i--) {
        const k = keys[i];
        if (k === "ext_media_color" || k === "features" || k === "clientEventInfo" || k === "feedbackInfo") {
          continue;
        }
        const val = node[k];
        if (val && typeof val === "object") stack.push({ node: val, ctx });
      }
    }
    return found;
  }

  function emitXMediaEntries(entries) {
    if (!Array.isArray(entries) || entries.length === 0) return;
    const batch = [];
    for (const rec of entries.slice(0, 40)) {
      if (!rec || !rec.key || !Array.isArray(rec.variants) || rec.variants.length === 0) continue;
      xMediaCache.set(rec.key, rec);
      if (xMediaCache.size > 80) {
        xMediaCache.delete(xMediaCache.keys().next().value);
      }
      batch.push(rec);
    }
    if (batch.length > 0) {
      try {
        window.postMessage({ __mdSniff: X_MSG, entries: batch }, "*");
      } catch {
        // ignore
      }
    }
  }

  function inspectXApiJson(obj) {
    if (!obj || typeof obj !== "object") return;
    try {
      emitXMediaEntries(extractXMediaEntries(obj));
    } catch {
      // ignore
    }
  }

  function inspectXApiText(text) {
    if (!text || text.length > 8 * 1024 * 1024 || !text.includes("video_info")) return;
    try {
      emitXMediaEntries(extractXMediaEntries(JSON.parse(text)));
    } catch {
      // ignore non-JSON responses
    }
  }

  function replayXMedia() {
    if (!IS_X || xMediaCache.size === 0) return;
    try {
      window.postMessage({ __mdSniff: X_MSG, replay: true, entries: Array.from(xMediaCache.values()) }, "*");
    } catch {
      // ignore
    }
  }

  function isMediaURL(value) {
    if (typeof value !== "string" || value.length === 0 || value.length > MAX_URL) return false;
    if (value.startsWith("blob:")) return true;
    if (!/^https?:/i.test(value)) return false;
    return MEDIA_PATH.test(value) || RANGE_HINT.test(value);
  }

  function flush() {
    flushTimer = 0;
    if (pending.length === 0) return;
    lastFlush = Date.now();
    const batch = pending.splice(0, pending.length);
    try {
      window.postMessage({ __mdSniff: MSG, seq: ++seq, items: batch }, "*");
    } catch {
      // page is tearing down
    }
  }

  function scheduleFlush() {
    if (flushTimer) return;
    const wait = Math.max(0, 150 - (Date.now() - lastFlush));
    flushTimer = setTimeout(flush, wait);
  }

  // kind: "fetch" | "xhr" | "src" | "load" | "beacon" | "perf" | "blob" | "mime" | "drm"
  // content.js starts at document_idle, after the page may already have fetched
  // its manifest. It announces itself and receives the buffered ring replay so
  // nothing discovered before injection is lost.
  function replay() {
    if (items.length > 0) {
      const batch = items.slice(-MAX_ITEMS);
      try {
        window.postMessage({ __mdSniff: MSG, seq: ++seq, replay: true, items: batch }, "*");
      } catch {
        // ignore
      }
    }
    if (IS_YOUTUBE) {
      for (const payload of ytPayloads.values()) {
        try {
          window.postMessage({ __mdSniff: YT_MSG, replay: true, payload }, "*");
        } catch {
          // ignore
        }
      }
      probeYouTubePlayers();
    }
    if (IS_BILIBILI) {
      for (const payload of biliPayloads.values()) {
        try {
          window.postMessage({ __mdSniff: BILI_MSG, replay: true, payload }, "*");
        } catch {
          // ignore
        }
      }
      probeBilibiliPlayInfo();
    }
    if (IS_XHS) {
      if (xhsNotes.size > 0) {
        try {
          window.postMessage({ __mdSniff: XHS_MSG, replay: true, notes: Array.from(xhsNotes.values()) }, "*");
        } catch {
          // ignore
        }
      }
      probeXiaohongshuState();
    }
    if (IS_X) {
      replayXMedia();
    }
  }

  try {
    window.addEventListener(
      "message",
      (event) => {
        if (event.source !== window) return;
        const data = event.data;
        if (!data) return;
        if (data.__mdSniff === "magicdown:media-sniff-ready:v1") {
          replay();
        } else if (data.__mdSniff === YT_PROBE_MSG) {
          probeYouTubePlayers();
        } else if (data.__mdSniff === BILI_PROBE_MSG) {
          probeBilibiliPlayInfo();
        } else if (data.__mdSniff === XHS_PROBE_MSG) {
          probeXiaohongshuState();
        } else if (data.__mdSniff === X_PROBE_MSG) {
          replayXMedia();
        }
      },
      false
    );
  } catch {
    // ignore
  }

  if (IS_YOUTUBE) {
    try {
      document.addEventListener("DOMContentLoaded", probeYouTubePlayers, { passive: true });
      window.addEventListener("yt-navigate-finish", probeYouTubePlayers, { passive: true });
      window.addEventListener("yt-page-data-updated", probeYouTubePlayers, { passive: true });
    } catch {
      // ignore
    }
  }
  if (IS_BILIBILI) {
    try {
      document.addEventListener("DOMContentLoaded", probeBilibiliPlayInfo, { passive: true });
    } catch {
      // ignore
    }
  }
  if (IS_XHS) {
    try {
      document.addEventListener("DOMContentLoaded", probeXiaohongshuState, { passive: true });
    } catch {
      // ignore
    }
  }

  function note(url, kind, mime) {
    try {
      const safeURL = typeof url === "string" ? url.slice(0, MAX_URL) : "";
      const safeMime = typeof mime === "string" ? mime.slice(0, 120) : "";
      const at = Date.now();

      if (kind === "mime" || kind === "drm") {
        items.push({ u: safeURL, k: kind, m: safeMime, t: at });
      } else {
        if (!isMediaURL(safeURL)) return;
        items.push({ u: safeURL, k: kind, m: safeMime, t: at });
      }
      if (items.length > MAX_ITEMS) items.splice(0, items.length - MAX_ITEMS);
      pending.push({ u: safeURL, k: kind, m: safeMime, t: at });
      if (pending.length > MAX_ITEMS) pending.splice(0, pending.length - MAX_ITEMS);
      scheduleFlush();
    } catch {
      // never let sniffing break page code
    }
  }

  function wrapMethod(owner, name, factory) {
    try {
      if (!owner) return;
      const original = owner[name];
      if (typeof original !== "function") return;
      const wrapped = factory(original);
      Object.defineProperty(owner, name, {
        value: wrapped,
        writable: true,
        configurable: true,
        enumerable: false
      });
    } catch {
      // frozen/patched API: skip quietly
    }
  }

  // --- Object URLs: link a MediaSource to the blob URL it is played as --------
  try {
    if (typeof URL !== "undefined" && typeof URL.createObjectURL === "function") {
      wrapMethod(URL, "createObjectURL", (original) =>
        function (object) {
          const url = original.apply(this, arguments);
          try {
            if (typeof MediaSource !== "undefined" && object instanceof MediaSource) {
              object.__mdUrl = url;
              note(url, "blob", object.__mdMime || "MediaSource");
            } else if (object && typeof object.type === "string" && object.type) {
              note(url, "blob", object.type);
            }
          } catch {
            // ignore
          }
          return url;
        }
      );
    }
  } catch {
    // ignore
  }

  // --- MediaSource: the MIME passed to addSourceBuffer is the best hint that a
  // --- player is DASH or HLS even when the manifest URL never reaches JS URLs.
  try {
    if (typeof MediaSource !== "undefined" && MediaSource.prototype) {
      wrapMethod(MediaSource.prototype, "addSourceBuffer", (original) =>
        function (mime) {
          try {
            const safeMime = typeof mime === "string" ? mime : String(mime || "");
            this.__mdMime = safeMime;
            note(this.__mdUrl || "", "mime", safeMime);
          } catch {
            // ignore
          }
          return original.apply(this, arguments);
        }
      );
    }
  } catch {
    // ignore
  }

  // --- HTMLMediaElement.src setter, setAttribute("src"), load() ---------------
  try {
    if (typeof HTMLMediaElement !== "undefined") {
      const descriptor = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, "src");
      if (descriptor && typeof descriptor.set === "function" && typeof descriptor.get === "function") {
        const originalSet = descriptor.set;
        const originalGet = descriptor.get;
        Object.defineProperty(HTMLMediaElement.prototype, "src", {
          configurable: descriptor.configurable,
          enumerable: descriptor.enumerable,
          get: originalGet,
          set(value) {
            try {
              note(String(value || ""), "src", "");
            } catch {
              // ignore
            }
            return originalSet.call(this, value);
          }
        });
      }

      wrapMethod(HTMLMediaElement.prototype, "load", (original) =>
        function () {
          try {
            note(this.currentSrc || this.src || "", "load", "");
          } catch {
            // ignore
          }
          return original.apply(this, arguments);
        }
      );
    }
  } catch {
    // ignore
  }

  // Some players only assign <source src> or call setAttribute on the video.
  try {
    if (typeof Element !== "undefined" && Element.prototype) {
      wrapMethod(Element.prototype, "setAttribute", (original) =>
        function (name, value) {
          try {
            if (typeof name === "string" && name.toLowerCase() === "src" && value) {
              const isMedia =
                (typeof HTMLMediaElement !== "undefined" && this instanceof HTMLMediaElement) ||
                (typeof HTMLSourceElement !== "undefined" && this instanceof HTMLSourceElement);
              if (isMedia) note(String(value), "src", "");
            }
          } catch {
            // ignore
          }
          return original.apply(this, arguments);
        }
      );
    }
  } catch {
    // ignore
  }

  // --- Weibo JSON API variant extraction -------------------------------------
  // Weibo feed/detail APIs return playback_list & media_info with all available
  // resolutions (1080p/720p/480p/360p) per post. Extracting them here lets
  // content.js show exact resolutions for the active video without stacking.
  function isWeiboApiURL(url) {
    try {
      const u = new URL(url, location.href);
      const host = u.hostname.toLowerCase();
      if (host !== "weibo.com" && !host.endsWith(".weibo.com") && host !== "weibo.cn" && !host.endsWith(".weibo.cn")) {
        return false;
      }
      return /\/(?:ajax|tv\/api|api)\//i.test(u.pathname);
    } catch {
      return false;
    }
  }

  function parseWeiboQuality(label, width, height) {
    const low = String(label || "").toLowerCase();
    const m = low.match(/(\d{3,4})p/);
    if (m) {
      const h = Number.parseInt(m[1], 10);
      if (h >= 144 && h <= 4320) return { quality: `${h}p`, height: h };
    }
    if (low.includes("4k")) return { quality: "2160p", height: 2160 };
    if (low.includes("2k")) return { quality: "1440p", height: 1440 };
    if (width > 0 && height > 0) {
      const h = Math.min(width, height);
      if (h >= 144 && h <= 4320) return { quality: `${h}p`, height: h };
    }
    if (low === "mp4_hd" || low.endsWith("_hd")) return { quality: "720p", height: 720 };
    if (low === "mp4_ld" || low.endsWith("_ld") || low === "mp4_sd" || low.endsWith("_sd")) {
      return { quality: "360p", height: 360 };
    }
    return { quality: "Original", height: 0 };
  }

  const WEIBO_SKIP_KEYS = new Set([
    "pic_infos",
    "user",
    "annotations",
    "title_source",
    "comment_manage_info",
    "number_display_strategy",
    "actionlog",
    "buttons",
    "icon_list"
  ]);

  function extractMediaIdFromUrl(rawUrl) {
    try {
      const u = new URL(rawUrl, location.href);
      return u.searchParams.get("media_id") || "";
    } catch {
      return "";
    }
  }

  function isWeiboNonVideoEntry(url, mime, label) {
    const m = String(mime || "").toLowerCase();
    const l = String(label || "").toLowerCase();
    if (m.startsWith("image/") || l.includes("scrubber") || l.includes("thumb")) return true;
    const path = String(url || "").split("?")[0].split("#")[0].toLowerCase();
    return /\.(?:jpg|jpeg|png|webp|gif|bmp)$/i.test(path);
  }

  function pickWeiboGroupThumb(node, inheritedPagePic) {
    const check = (val) => {
      if (!val) return null;
      if (typeof val === "string") {
        let u = val.trim();
        if (u.startsWith("//")) u = `https:${u}`;
        if (/^https?:\/\//i.test(u)) return { url: u.slice(0, MAX_URL), width: 0, height: 0 };
      } else if (typeof val === "object") {
        let u = typeof val.url === "string" ? val.url.trim() : (typeof val.pic_url === "string" ? val.pic_url.trim() : "");
        if (u.startsWith("//")) u = `https:${u}`;
        if (/^https?:\/\//i.test(u)) {
          return { url: u.slice(0, MAX_URL), width: Number(val.width) || 0, height: Number(val.height) || 0 };
        }
      }
      return null;
    };
    return (
      (node && node.big_pic_info && check(node.big_pic_info.pic_big)) ||
      (node && node.pic_info && check(node.pic_info.pic_big)) ||
      (node && check(node.page_pic)) ||
      (node && check(node.cover_image)) ||
      check(inheritedPagePic) ||
      null
    );
  }

  function extractWeiboGroups(data) {
    const groups = [];
    const stack = [{ node: data, pagePic: null }];
    let guard = 0;
    while (stack.length > 0 && guard < 50000) {
      guard++;
      const frame = stack.pop();
      const node = frame && frame.node;
      const inheritedPagePic = frame ? frame.pagePic : null;
      if (!node || typeof node !== "object") continue;
      if (Array.isArray(node)) {
        for (let i = 0; i < node.length; i++) stack.push({ node: node[i], pagePic: inheritedPagePic });
        continue;
      }

      const nextPagePic = node.page_pic || inheritedPagePic;

      if (Array.isArray(node.playback_list) && node.playback_list.length > 0) {
        const variants = [];
        let inferredMediaId = "";
        for (const entry of node.playback_list) {
          const info = entry && entry.play_info;
          const meta = entry && entry.meta;
          const url = info && typeof info.url === "string" ? info.url : "";
          if (!url || !/^https?:/i.test(url)) continue;
          const label = (meta && (meta.quality_label || meta.label)) || "";
          const mime = String((info && info.mime) || "");
          if (isWeiboNonVideoEntry(url, mime, label)) continue;
          if (!inferredMediaId) inferredMediaId = extractMediaIdFromUrl(url);
          const w = Number(info.width) || 0;
          const h = Number(info.height) || 0;
          const q = parseWeiboQuality(label, w, h);
          const isHLS = /\.m3u8(?:$|[?#])/i.test(url) || /mpegurl|mp2t/i.test(mime);
          variants.push({
            url: url.slice(0, MAX_URL),
            quality: q.quality,
            height: q.height,
            size: Number(info.size || (meta && meta.size)) || 0,
            bandwidth: Number(info.bitrate || (meta && meta.bitrate)) || 0,
            format: isHLS ? "HLS" : (/webm/i.test(mime) ? "WebM" : "MP4"),
            isHLS
          });
        }
        if (variants.length > 0) {
          const rawObjId = String(node.object_id || "");
          const objSuffix = rawObjId.includes(":") ? rawObjId.split(":").pop() : "";
          const mediaId = String(node.media_id || node.media_id_str || inferredMediaId || objSuffix || "");
          const id = mediaId || rawObjId || variants[0].url;
          const thumb = pickWeiboGroupThumb(node, nextPagePic);
          groups.push({ id, mediaId, variants, thumb });
        }
      }

      for (const key of Object.keys(node)) {
        if (WEIBO_SKIP_KEYS.has(key)) continue;
        const val = node[key];
        if (val && typeof val === "object") stack.push({ node: val, pagePic: nextPagePic });
      }
    }
    return groups;
  }

  function emitWeiboGroups(groups) {
    if (!groups || groups.length === 0) return;
    const at = Date.now();
    for (const g of groups.slice(0, 40)) {
      const entry = { k: "weibo-group", id: g.id, mid: g.mediaId || "", v: g.variants, thumb: g.thumb || null, t: at };
      items.push(entry);
      if (items.length > MAX_ITEMS) items.splice(0, items.length - MAX_ITEMS);
      pending.push(entry);
      if (pending.length > MAX_ITEMS) pending.splice(0, pending.length - MAX_ITEMS);
    }
    scheduleFlush();
  }

  function inspectWeiboJsonObject(obj) {
    if (!obj || typeof obj !== "object") return;
    try {
      emitWeiboGroups(extractWeiboGroups(obj));
    } catch {
      // ignore
    }
  }

  function inspectWeiboJsonText(text) {
    if (!text || text.length > 6 * 1024 * 1024 || !text.includes("playback_list")) return;
    try {
      const parsed = JSON.parse(text);
      emitWeiboGroups(extractWeiboGroups(parsed));
    } catch {
      // ignore non-JSON responses
    }
  }

  // --- Facebook inline MPD representation extraction -------------------------
  // Facebook embeds DASH MPD XML inside GraphQL/page JSON payloads. Extracting
  // Representation width/height/FBQualityLabel/bandwidth -> BaseURL pathname
  // lets content.js label adaptive q20..q90 tracks with exact resolutions.
  const IS_FACEBOOK = /(?:^|\.)(?:facebook\.com|fb\.watch)$/i.test(location.hostname || "");

  function extractFacebookMpdReps(text) {
    if (!text || !text.includes("FBQualityLabel")) return [];
    const reps = [];
    const seen = new Set();
    // Matches escaped or unescaped XML Representation blocks with BaseURL.
    const normalized = text.includes("\\u003C") ? text.replace(/\\u003C/g, "<").replace(/\\"/g, '"').replace(/\\\//g, "/") : text;
    const repRe = /<Representation\b([^>]+)>([\s\S]{0,1600}?)<BaseURL>([^<]+)<\/BaseURL>/gi;
    let m;
    while ((m = repRe.exec(normalized)) !== null && reps.length < 40) {
      const attrs = m[1];
      const rawUrl = m[3].replace(/&amp;/g, "&").trim();
      if (!/^https?:\/\/[^/]*fbcdn\.net\//i.test(rawUrl)) continue;
      let path = "";
      try {
        path = new URL(rawUrl).pathname;
      } catch {
        continue;
      }
      if (!path || seen.has(path)) continue;
      seen.add(path);
      const qLabel = (attrs.match(/FBQualityLabel="([^"]+)"/i) || [])[1] || "";
      const w = Number.parseInt((attrs.match(/\bwidth="(\d+)"/i) || [])[1] || "0", 10) || 0;
      const h = Number.parseInt((attrs.match(/\bheight="(\d+)"/i) || [])[1] || "0", 10) || 0;
      const bw = Number.parseInt((attrs.match(/\bbandwidth="(\d+)"/i) || [])[1] || "0", 10) || 0;
      const minDim = w > 0 && h > 0 ? Math.min(w, h) : h;
      const qMatch = qLabel.match(/(\d{3,4})p/i);
      const height = qMatch ? Number.parseInt(qMatch[1], 10) : minDim;
      const quality = qMatch ? `${height}p` : (height >= 144 && height <= 4320 ? `${height}p` : "");
      if (!quality && !bw) continue;
      reps.push({ p: path, q: quality, h: height, w, bw });
    }
    return reps;
  }

  function inspectFacebookText(text) {
    if (!IS_FACEBOOK || !text || text.length > 6 * 1024 * 1024 || !text.includes("FBQualityLabel")) return;
    try {
      const reps = extractFacebookMpdReps(text);
      if (reps.length === 0) return;
      const entry = { k: "fb-mpd", v: reps, t: Date.now() };
      items.push(entry);
      if (items.length > MAX_ITEMS) items.splice(0, items.length - MAX_ITEMS);
      pending.push(entry);
      if (pending.length > MAX_ITEMS) pending.splice(0, pending.length - MAX_ITEMS);
      scheduleFlush();
    } catch {
      // ignore
    }
  }

  // --- fetch / XHR / sendBeacon ----------------------------------------------
  try {
    if (typeof window.fetch === "function") {
      wrapMethod(window, "fetch", (original) =>
        function (input) {
          let url = "";
          try {
            url = typeof input === "string" ? input : input && input.url ? String(input.url) : "";
            if (url) note(url, "fetch", "");
          } catch {
            // ignore
          }
          const promise = original.apply(this, arguments);
          try {
            if (url && isWeiboApiURL(url) && promise && typeof promise.then === "function") {
              promise.then((resp) => {
                try {
                  if (resp && resp.ok && typeof resp.clone === "function") {
                    resp.clone().text().then(inspectWeiboJsonText).catch(() => {});
                  }
                } catch {
                  // ignore
                }
              }).catch(() => {});
            }
            if (IS_FACEBOOK && url && /\/api\/graphql/i.test(url) && promise && typeof promise.then === "function") {
              promise.then((resp) => {
                try {
                  if (resp && resp.ok && typeof resp.clone === "function") {
                    resp.clone().text().then(inspectFacebookText).catch(() => {});
                  }
                } catch {
                  // ignore
                }
              }).catch(() => {});
            }
            if (IS_YOUTUBE && url && /\/youtubei\/v1\/player(?:$|\?)/i.test(url) && promise && typeof promise.then === "function") {
              promise.then((resp) => {
                try {
                  if (resp && resp.ok && typeof resp.clone === "function") {
                    resp.clone().json().then(emitYTPlayerResponse).catch(() => {});
                  }
                } catch {
                  // ignore
                }
              }).catch(() => {});
            }
            if (IS_BILIBILI && url && isBilibiliPlayUrl(url) && promise && typeof promise.then === "function") {
              promise.then((resp) => {
                try {
                  if (resp && resp.ok && typeof resp.clone === "function") {
                    resp.clone().json().then(emitBilibiliPlayInfo).catch(() => {});
                  }
                } catch {
                  // ignore
                }
              }).catch(() => {});
            }
            if (IS_XHS && url && isXhsApiUrl(url) && promise && typeof promise.then === "function") {
              promise.then((resp) => {
                try {
                  if (resp && resp.ok && typeof resp.clone === "function") {
                    resp.clone().json().then(inspectXhsApiJson).catch(() => {});
                  }
                } catch {
                  // ignore
                }
              }).catch(() => {});
            }
            if (IS_X && url && isXApiUrl(url) && promise && typeof promise.then === "function") {
              promise.then((resp) => {
                try {
                  if (resp && resp.ok && typeof resp.clone === "function") {
                    resp.clone().text().then(inspectXApiText).catch(() => {});
                  }
                } catch {
                  // ignore
                }
              }).catch(() => {});
            }
          } catch {
            // ignore
          }
          return promise;
        }
      );
    }
  } catch {
    // ignore
  }

  try {
    if (typeof XMLHttpRequest !== "undefined" && XMLHttpRequest.prototype) {
      wrapMethod(XMLHttpRequest.prototype, "open", (original) =>
        function (method, url) {
          try {
            const safeUrl = url ? String(url) : "";
            if (safeUrl) note(safeUrl, "xhr", "");
            if (safeUrl && isWeiboApiURL(safeUrl) && !this.__mdWeiboHooked) {
              this.__mdWeiboHooked = true;
              this.addEventListener("load", () => {
                try {
                  if (this.responseType === "json" && this.response) {
                    inspectWeiboJsonObject(this.response);
                  } else if ((!this.responseType || this.responseType === "text") && typeof this.responseText === "string") {
                    inspectWeiboJsonText(this.responseText);
                  }
                } catch {
                  // ignore
                }
              });
            }
            if (IS_FACEBOOK && safeUrl && /\/api\/graphql/i.test(safeUrl) && !this.__mdFbHooked) {
              this.__mdFbHooked = true;
              this.addEventListener("load", () => {
                try {
                  if ((!this.responseType || this.responseType === "text") && typeof this.responseText === "string") {
                    inspectFacebookText(this.responseText);
                  }
                } catch {
                  // ignore
                }
              });
            }
            if (IS_YOUTUBE && /\/youtubei\/v1\/player(?:$|\?)/i.test(safeUrl)) {
              this.__mdYtPlayerXhr = true;
              if (!this.__mdYtPlayerHooked) {
                this.__mdYtPlayerHooked = true;
                this.addEventListener("load", () => {
                  try {
                    if (!this.__mdYtPlayerXhr) return;
                    if (this.responseType === "json" && this.response) {
                      emitYTPlayerResponse(this.response);
                    } else if ((!this.responseType || this.responseType === "text") && this.responseText) {
                      emitYTPlayerResponse(JSON.parse(this.responseText));
                    }
                  } catch {
                    // ignore
                  }
                });
              }
            } else {
              this.__mdYtPlayerXhr = false;
            }
            if (IS_BILIBILI && isBilibiliPlayUrl(safeUrl)) {
              this.__mdBiliPlayerXhr = true;
              if (!this.__mdBiliPlayerHooked) {
                this.__mdBiliPlayerHooked = true;
                this.addEventListener("load", () => {
                  try {
                    if (!this.__mdBiliPlayerXhr) return;
                    if (this.responseType === "json" && this.response) {
                      emitBilibiliPlayInfo(this.response);
                    } else if ((!this.responseType || this.responseType === "text") && this.responseText) {
                      emitBilibiliPlayInfo(JSON.parse(this.responseText));
                    }
                  } catch {
                    // ignore
                  }
                });
              }
            } else {
              this.__mdBiliPlayerXhr = false;
            }
            if (IS_XHS && isXhsApiUrl(safeUrl)) {
              this.__mdXhsApiXhr = true;
              if (!this.__mdXhsApiHooked) {
                this.__mdXhsApiHooked = true;
                this.addEventListener("load", () => {
                  try {
                    if (!this.__mdXhsApiXhr) return;
                    if (this.responseType === "json" && this.response) {
                      inspectXhsApiJson(this.response);
                    } else if ((!this.responseType || this.responseType === "text") && this.responseText) {
                      inspectXhsApiJson(JSON.parse(this.responseText));
                    }
                  } catch {
                    // ignore
                  }
                });
              }
            } else {
              this.__mdXhsApiXhr = false;
            }
            if (IS_X && isXApiUrl(safeUrl)) {
              this.__mdXApiXhr = true;
              if (!this.__mdXApiHooked) {
                this.__mdXApiHooked = true;
                this.addEventListener("load", () => {
                  try {
                    if (!this.__mdXApiXhr) return;
                    if (this.responseType === "json" && this.response) {
                      inspectXApiJson(this.response);
                    } else if ((!this.responseType || this.responseType === "text") && typeof this.responseText === "string") {
                      inspectXApiText(this.responseText);
                    }
                  } catch {
                    // ignore
                  }
                });
              }
            } else {
              this.__mdXApiXhr = false;
            }
          } catch {
            // ignore
          }
          return original.apply(this, arguments);
        }
      );
    }
  } catch {
    // ignore
  }

  try {
    if (typeof navigator !== "undefined" && typeof navigator.sendBeacon === "function") {
      const originalBeacon = navigator.sendBeacon.bind(navigator);
      Object.defineProperty(navigator, "sendBeacon", {
        configurable: true,
        writable: true,
        value: function (url, data) {
          try {
            note(String(url || ""), "beacon", "");
          } catch {
            // ignore
          }
          return originalBeacon(url, data);
        }
      });
    }
  } catch {
    // ignore
  }

  // --- EME: let content.js explain DRM instead of showing a dead tag ---------
  try {
    if (typeof navigator !== "undefined" && typeof navigator.requestMediaKeySystemAccess === "function") {
      const originalKeyAccess = navigator.requestMediaKeySystemAccess.bind(navigator);
      Object.defineProperty(navigator, "requestMediaKeySystemAccess", {
        configurable: true,
        writable: true,
        value: function (keySystem, configurations) {
          try {
            note("", "drm", String(keySystem || ""));
          } catch {
            // ignore
          }
          return originalKeyAccess(keySystem, configurations);
        }
      });
    }
  } catch {
    // ignore
  }

  // --- Resource timing: manifests/segments fetched before this script ran -----
  try {
    if (typeof PerformanceObserver === "function") {
      const observer = new PerformanceObserver((list) => {
        try {
          for (const entry of list.getEntries()) {
            if (entry && entry.name) note(entry.name, "perf", "");
          }
        } catch {
          // ignore
        }
      });
      observer.observe({ type: "resource", buffered: true });
    }
  } catch {
    // ignore
  }
})();

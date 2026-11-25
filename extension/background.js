if (typeof importScripts === "function") {
  try {
    importScripts("i18n.js");
  } catch {
    // ignore in non-worker test environments
  }
}
const I18N =
  (typeof self !== "undefined" && self.MagicDownI18n) ||
  (typeof globalThis !== "undefined" && globalThis.MagicDownI18n) ||
  (typeof require === "function" ? require("./i18n.js") : null);

function t(key, params) {
  return I18N ? I18N.t(key, params) : key;
}

const HOST_NAME = "com.magicdown.downloader";
const MAX_HISTORY = 25;
const MAX_TAB_VIDEOS = 40;

const DEFAULT_SETTINGS = {
  interceptEnabled: true,
  extensions: [
    "zip", "rar", "7z", "tar", "gz", "iso",
    "exe", "msi", "dmg", "apk", "bin",
    "mp4", "mkv", "avi", "mov", "webm",
    "mp3", "flac", "wav", "pdf"
  ],
  minFileSizeMB: 0,
  excludedDomains: [],
  theme: "dark"
};

// Per-tab detected videos: Map<tabId, Map<url, VideoItem>>
const tabVideos = new Map();
// Per-tab detected Facebook audio-only DASH tracks: Map<tabId, Map<fbVideoId, AudioItem>>
const tabFbAudios = new Map();
// Recent batch scan snapshots: Map<scanId, { tabId, pageTitle, pageUrl, items, createdAt }>
const batchScanResults = new Map();
const MAX_BATCH_SCANS = 10;

async function getSettings() {
  const stored = await chrome.storage.local.get([
    "interceptEnabled",
    "extensions",
    "minFileSizeMB",
    "excludedDomains",
    "theme"
  ]);
  return {
    interceptEnabled: stored.interceptEnabled !== undefined ? stored.interceptEnabled : DEFAULT_SETTINGS.interceptEnabled,
    extensions: Array.isArray(stored.extensions) ? stored.extensions : DEFAULT_SETTINGS.extensions,
    minFileSizeMB: typeof stored.minFileSizeMB === "number" ? stored.minFileSizeMB : DEFAULT_SETTINGS.minFileSizeMB,
    excludedDomains: Array.isArray(stored.excludedDomains) ? stored.excludedDomains : DEFAULT_SETTINGS.excludedDomains,
    theme: stored.theme || DEFAULT_SETTINGS.theme
  };
}

function extractHost(rawUrl) {
  try {
    return new URL(rawUrl).hostname.toLowerCase();
  } catch {
    return "";
  }
}

function extractFilename(rawUrl, fallback) {
  if (fallback) {
    const parts = fallback.replace(/\\/g, "/").split("/");
    const last = parts[parts.length - 1];
    if (last) return last;
  }
  try {
    const pathname = new URL(rawUrl).pathname;
    const base = pathname.split("/").filter(Boolean).pop();
    if (base) return decodeURIComponent(base);
  } catch {
    // ignore
  }
  return extractHost(rawUrl) || "download";
}

function getExtensionFromUrlOrFilename(url, filename) {
  const candidate = filename || url || "";
  const clean = candidate.split("?")[0].split("#")[0];
  const lastDot = clean.lastIndexOf(".");
  if (lastDot === -1 || lastDot === clean.length - 1) {
    return "";
  }
  return clean.slice(lastDot + 1).toLowerCase();
}

function isDomainExcluded(url, excludedDomains) {
  if (!Array.isArray(excludedDomains) || excludedDomains.length === 0) {
    return false;
  }
  const host = extractHost(url);
  if (!host) return false;
  return excludedDomains.some((domain) => {
    const d = String(domain || "").trim().toLowerCase();
    return d && (host === d || host.endsWith("." + d));
  });
}

function shouldIntercept(downloadItem, settings) {
  const url = downloadItem.finalUrl || downloadItem.url;
  if (!url || (!url.startsWith("http://") && !url.startsWith("https://"))) {
    return false;
  }

  if (isDomainExcluded(url, settings.excludedDomains)) {
    return false;
  }
  if (downloadItem.referrer && isDomainExcluded(downloadItem.referrer, settings.excludedDomains)) {
    return false;
  }

  if (settings.minFileSizeMB > 0 && typeof downloadItem.fileSize === "number" && downloadItem.fileSize > 0) {
    const minBytes = settings.minFileSizeMB * 1024 * 1024;
    if (downloadItem.fileSize < minBytes) {
      return false;
    }
  }

  const allowedExts = settings.extensions;
  if (!allowedExts || allowedExts.length === 0 || allowedExts.includes("*")) {
    return true;
  }
  const ext = getExtensionFromUrlOrFilename(url, downloadItem.filename);
  return ext !== "" && allowedExts.includes(ext);
}

async function getCookiesHeader(url) {
  try {
    const cookies = await chrome.cookies.getAll({ url });
    if (!cookies || cookies.length === 0) {
      return "";
    }
    return cookies.map((c) => `${c.name}=${c.value}`).join("; ");
  } catch {
    return "";
  }
}

async function recordActivity(entry) {
  try {
    const { recentDownloads } = await chrome.storage.local.get(["recentDownloads"]);
    const list = Array.isArray(recentDownloads) ? recentDownloads : [];
    list.unshift({
      id: entry.id || String(Date.now()),
      filename: entry.filename,
      host: entry.host,
      url: entry.url,
      status: entry.status,
      error: entry.error || "",
      timestamp: Date.now()
    });
    await chrome.storage.local.set({ recentDownloads: list.slice(0, MAX_HISTORY) });
  } catch {
    // ignore storage errors
  }
}

function sendNativeMessage(payload) {
  return new Promise((resolve) => {
    try {
      chrome.runtime.sendNativeMessage(HOST_NAME, payload, (response) => {
        if (chrome.runtime.lastError) {
          resolve({
            success: false,
            hostError: true,
            error: chrome.runtime.lastError.message
          });
          return;
        }
        resolve(response || { success: false, error: "empty response from native host" });
      });
    } catch (err) {
      resolve({
        success: false,
        hostError: true,
        error: String(err)
      });
    }
  });
}

function sanitizeHeaders(rawHeaders) {
  const out = {};
  if (rawHeaders && typeof rawHeaders === "object") {
    for (const [key, val] of Object.entries(rawHeaders)) {
      if (typeof key !== "string" || typeof val !== "string") continue;
      const k = key.trim();
      const v = val.trim();
      if (!k || !v || k.length > 64 || v.length > 1024) continue;
      out[k] = v;
    }
  }
  if (!out["User-Agent"] && typeof navigator !== "undefined" && navigator.userAgent) {
    out["User-Agent"] = navigator.userAgent;
  }
  return out;
}

function buildDownloadPayload({ url, audioUrl = "", filename = "", referrer = "", cookies = "", headers = null, convertFormat = "", targetFormat = "", outputFormat = "", audioBitrate = "", plan = null, size = 0 }) {
  const safeHeaders = sanitizeHeaders(headers);
  const payload = {
    action: "download",
    url,
    filename,
    referrer,
    cookies,
    headers: safeHeaders,
    auto_start: true
  };
  if (typeof audioUrl === "string" && /^https?:\/\//i.test(audioUrl.trim())) {
    payload.audio_url = audioUrl.trim();
  }
  if (typeof size === "number" && Number.isFinite(size) && size > 0) {
    payload.total_bytes = Math.floor(size);
  }
  if (convertFormat) {
    payload.convert_format = convertFormat;
  }
  if (typeof targetFormat === "string" && targetFormat.trim()) {
    payload.target_format = targetFormat.trim().toLowerCase();
  }
  if (typeof outputFormat === "string" && outputFormat.trim()) {
    payload.output_format = outputFormat.trim().toLowerCase();
  }
  if (typeof audioBitrate === "string" && audioBitrate.trim()) {
    payload.audio_bitrate = audioBitrate.trim().toLowerCase();
  }
  const safePlan = sanitizeDownloadPlan(plan);
  if (safePlan) {
    payload.plan = safePlan;
  }
  return payload;
}

async function forwardDownload({ url, audioUrl = "", filename = "", referrer = "", headers = null, convertFormat = "", targetFormat = "", outputFormat = "", audioBitrate = "", plan = null, size = 0 }) {
  const cookies = await getCookiesHeader(url);
  const payload = buildDownloadPayload({ url, audioUrl, filename, referrer, cookies, headers, convertFormat, targetFormat, outputFormat, audioBitrate, plan, size });

  const resp = await sendNativeMessage(payload);

  await recordActivity({
    id: resp && resp.taskId ? resp.taskId : String(Date.now()),
    filename: (resp && resp.filename) || extractFilename(url, filename),
    host: extractHost(url),
    url,
    status: resp && resp.success ? "sent" : "failed",
    error: resp && !resp.success ? (resp.error || "Failed to connect") : ""
  });

  return resp;
}

// --- Video & HLS Detection ---

const MAX_PLAN_PARTS = 4000;

// The engine re-validates everything; this pass only drops malformed shapes so
// a broken page cannot push absurd payloads through native messaging.
function sanitizeDownloadPlan(plan) {
  if (!plan || typeof plan !== "object") return null;
  const rawParts = Array.isArray(plan.parts) ? plan.parts.slice(0, MAX_PLAN_PARTS) : [];
  if (rawParts.length === 0) return null;

  const parts = [];
  for (const raw of rawParts) {
    if (!raw || typeof raw !== "object") continue;
    const part = {};
    if (typeof raw.url === "string" && raw.url) {
      part.url = raw.url.slice(0, 4096);
    }
    for (const key of ["start", "end"]) {
      const value = Number(raw[key]);
      if (Number.isFinite(value) && value > 0) {
        part[key] = Math.min(Math.floor(value), Number.MAX_SAFE_INTEGER);
      }
    }
    parts.push(part);
  }
  if (parts.length === 0) return null;

  const kind = plan.kind === "segments" ? "segments" : plan.kind === "range" ? "range" : "";
  const safe = {
    parts,
    kind: kind || (parts.every((p) => p.url && p.url === parts[0].url) ? "range" : "segments"),
    url: typeof plan.url === "string" ? plan.url.slice(0, 4096) : "",
    format: typeof plan.format === "string" ? plan.format.toLowerCase().replace(/[^a-z0-9]/g, "").slice(0, 8) : ""
  };
  return safe;
}

// Manifest text fetch stays in the worker: it has host permissions and the
// cookies permission, so DASH/HLS manifests behind CORS or login still parse.
async function fetchManifestText(url) {
  if (typeof url !== "string" || !/^https?:/i.test(url)) {
    return { ok: false, error: "unsupported manifest URL" };
  }
  try {
    const resp = await fetch(url, { credentials: "include", cache: "no-store" });
    if (!resp.ok) {
      return { ok: false, error: `HTTP ${resp.status}` };
    }
    const text = await resp.text();
    if (text.length > 4 * 1024 * 1024) {
      return { ok: false, error: "manifest is too large" };
    }
    return { ok: true, text, contentType: resp.headers.get("content-type") || "" };
  } catch (err) {
    return { ok: false, error: String((err && err.message) || err) };
  }
}

function getTabVideoList(tabId) {
  const map = tabVideos.get(tabId);
  if (!map) return [];
  const audioMap = tabFbAudios.get(tabId);
  return Array.from(map.values())
    .map((v) => {
      if (v && v.fbVideoId && v.isFbDash && !v.audioUrl && audioMap) {
        const audio = audioMap.get(v.fbVideoId);
        if (audio && audio.url) {
          const combinedSize = v.size > 0 && audio.size > 0 ? v.size + audio.size : (v.size || 0);
          return {
            ...v,
            audioUrl: audio.url,
            audioSize: audio.size || 0,
            size: combinedSize,
            format: "MP4",
            ext: "mp4",
            streamType: "muxed",
            hasAudio: true,
            needsMux: true
          };
        }
      }
      return v;
    })
    .sort((a, b) => {
      if ((b.height || 0) !== (a.height || 0)) return (b.height || 0) - (a.height || 0);
      if ((b.bandwidth || 0) !== (a.bandwidth || 0)) return (b.bandwidth || 0) - (a.bandwidth || 0);
      if ((b.size || 0) !== (a.size || 0)) return (b.size || 0) - (a.size || 0);
      return (b.detectedAt || 0) - (a.detectedAt || 0);
    });
}

function addDetectedFacebookAudio(tabId, audio) {
  if (tabId < 0 || !audio || !audio.url || !audio.fbVideoId) return;
  let map = tabFbAudios.get(tabId);
  if (!map) {
    map = new Map();
    tabFbAudios.set(tabId, map);
  }
  const existing = map.get(audio.fbVideoId);
  if (!existing || (audio.bandwidth || 0) >= (existing.bandwidth || 0)) {
    map.set(audio.fbVideoId, {
      ...audio,
      size: Math.max(audio.size || 0, (existing && existing.size) || 0),
      detectedAt: Date.now()
    });
    notifyTabVideos(tabId);
  }
}

function classifyFacebookAudioTrack(url, headers) {
  const { url: mediaUrl, strippedRange } = normalizeMediaUrl(url);
  const efg = parseFacebookEfg(mediaUrl);
  if (!efg || typeof efg.vencode_tag !== "string") return null;
  if (!(/audio/i.test(efg.vencode_tag) && !/video/i.test(efg.vencode_tag))) return null;
  const fbVideoId = String(efg.video_id || efg.xpv_asset_id || "");
  if (!fbVideoId) return null;
  let contentLength = 0;
  let contentRange = null;
  if (Array.isArray(headers)) {
    for (const h of headers) {
      const name = (h.name || "").toLowerCase();
      if (name === "content-length") contentLength = Number.parseInt(h.value || "0", 10) || 0;
      if (name === "content-range") contentRange = parseContentRange(h.value);
    }
  }
  const bandwidth = Number.isFinite(Number(efg.bitrate)) && Number(efg.bitrate) > 0 ? Number(efg.bitrate) : 0;
  const size = (contentRange && contentRange.total) || (strippedRange ? 0 : contentLength);
  return {
    kind: "fb-audio",
    url: mediaUrl,
    fbVideoId,
    bandwidth,
    size
  };
}

function updateTabBadge(tabId) {
  const list = getTabVideoList(tabId);
  const text = list.length > 0 ? String(Math.min(list.length, 99)) : "";
  try {
    chrome.action.setBadgeText({ tabId, text });
    if (text) {
      chrome.action.setBadgeBackgroundColor({ tabId, color: "#3B82F6" });
    }
  } catch {
    // tab may already be closed
  }
}

function notifyTabVideos(tabId) {
  const videos = getTabVideoList(tabId);
  updateTabBadge(tabId);
  try {
    chrome.tabs.sendMessage(tabId, { type: "videosUpdated", videos }).catch(() => {});
  } catch {
    // content script may not be ready
  }
}

function isGenericQuality(quality) {
  return !quality || quality === "Original" || quality === "HLS";
}

function addDetectedVideo(tabId, item) {
  if (tabId < 0 || !item || !item.url) return;
  let map = tabVideos.get(tabId);
  if (!map) {
    map = new Map();
    tabVideos.set(tabId, map);
  }
  const key = getMediaDedupKey(item.url);
  const existing = map.get(key);
  const merged = {
    ...existing,
    ...item,
    size: Math.max(item.size || 0, (existing && existing.size) || 0),
    detectedAt: Date.now()
  };
  if (existing) {
    // A later media-playlist probe or chunk of the same stream must not
    // downgrade the variant metadata (resolution/bandwidth) already known.
    merged.height = item.height || existing.height || 0;
    merged.bandwidth = item.bandwidth || existing.bandwidth || 0;
    merged.resolution = item.resolution || existing.resolution || "";
    merged.fbVideoId = item.fbVideoId || existing.fbVideoId || "";
    if (isGenericQuality(item.quality) && !isGenericQuality(existing.quality)) {
      merged.quality = existing.quality;
    }
  }
  map.set(key, merged);
  // A long feed can expose hundreds of media URLs; keep the newest entries.
  while (map.size > MAX_TAB_VIDEOS) {
    const oldestKey = map.keys().next().value;
    if (!oldestKey || oldestKey === key) break;
    map.delete(oldestKey);
  }
  notifyTabVideos(tabId);
}

function parseResolutionHeight(res) {
  if (!res) return 0;
  const parts = String(res).toLowerCase().split("x");
  if (parts.length === 2) {
    const h = Number.parseInt(parts[1], 10);
    return Number.isFinite(h) ? h : 0;
  }
  return 0;
}

function parseM3U8Master(baseUrl, text) {
  const lines = text.split(/\r?\n/);
  const variants = [];
  let pending = null;

  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (!line) continue;
    if (line.startsWith("#EXT-X-STREAM-INF:")) {
      const attrs = line.slice("#EXT-X-STREAM-INF:".length);
      const bwMatch = attrs.match(/BANDWIDTH=(\d+)/i);
      const resMatch = attrs.match(/RESOLUTION=(\d+x\d+)/i);
      const codecMatch = attrs.match(/CODECS="([^"]+)"/i);
      const resolution = resMatch ? resMatch[1] : "";
      const height = parseResolutionHeight(resolution);
      const bandwidth = bwMatch ? Number.parseInt(bwMatch[1], 10) : 0;
      pending = {
        resolution,
        height,
        bandwidth,
        codecs: codecMatch ? codecMatch[1] : "",
        quality: height > 0 ? `${height}p` : (bandwidth > 0 ? `${(bandwidth / 1000000).toFixed(1)} Mbps` : "HLS")
      };
      continue;
    }
    if (line.startsWith("#")) continue;
    if (pending) {
      try {
        const resolvedUrl = new URL(line, baseUrl).toString();
        variants.push({
          ...pending,
          url: resolvedUrl,
          format: "HLS",
          isHLS: true
        });
      } catch {
        // ignore invalid variant URL
      }
      pending = null;
    }
  }
  return variants;
}

async function inspectHLSPlaylist(tabId, playlistUrl) {
  try {
    const resp = await fetch(playlistUrl, { credentials: "include" });
    if (!resp.ok) return;
    const text = await resp.text();
    const variants = parseM3U8Master(playlistUrl, text);
    if (variants.length > 0) {
      for (const v of variants) {
        addDetectedVideo(tabId, v);
      }
    } else if (text.includes("#EXTINF:")) {
      addDetectedVideo(tabId, {
        url: playlistUrl,
        quality: "Original",
        format: "HLS",
        isHLS: true
      });
    }
  } catch {
    addDetectedVideo(tabId, {
      url: playlistUrl,
      quality: "Original",
      format: "HLS",
      isHLS: true
    });
  }
}

function parseContentRange(value) {
  if (!value) return null;
  const match = String(value).match(/bytes\s+(\d+)-(\d+)\/(\d+|\*)/i);
  if (!match) return null;
  const start = Number.parseInt(match[1], 10);
  const total = match[3] === "*" ? 0 : Number.parseInt(match[3], 10);
  if (!Number.isFinite(start) || start < 0) return null;
  return { start, total: Number.isFinite(total) && total > 0 ? total : 0 };
}

const RANGE_QUERY_PARAMS = new Set([
  "bytestart",
  "byteend",
  "byte_start",
  "byte_end",
  "range",
  "bytes"
]);

const EPHEMERAL_QUERY_PARAMS = new Set([
  ...RANGE_QUERY_PARAMS,
  "rn",
  "rbuf",
  "sq",
  "rid",
  "_nc_rid",
  "cb",
  "cachebuster",
  "_"
]);

function isFbcdnHost(host) {
  return host === "fbcdn.net" || host.endsWith(".fbcdn.net");
}

function parseFacebookEfg(rawUrl) {
  try {
    const u = new URL(rawUrl);
    if (!isFbcdnHost(u.hostname.toLowerCase())) return null;
    const raw = u.searchParams.get("efg");
    if (!raw || raw.length > 2048) return null;
    const base64 = raw.replace(/-/g, "+").replace(/_/g, "/");
    const padded = base64 + "=".repeat((4 - (base64.length % 4)) % 4);
    const decoded = atob(padded);
    const parsed = JSON.parse(decoded);
    return parsed && typeof parsed === "object" ? parsed : null;
  } catch {
    return null;
  }
}

function isDashSegmentUrl(rawUrl, contentType) {
  const lowerPath = String(rawUrl || "").toLowerCase().split("?")[0].split("#")[0];
  if (/\.(?:m4s|cmfv|cmfa|cmf|f4f|f4s|m4f)$/i.test(lowerPath)) {
    return true;
  }
  const ct = String(contentType || "").toLowerCase();
  if (ct.includes("iso.segment")) {
    return true;
  }
  return false;
}

// Facebook and similar DASH CDNs request one stream in byte-range chunks by
// appending bytestart/byteend/range/bytes to the same URL. Stripping them
// collapses hundreds of chunk requests into one candidate and yields the
// full-stream URL.
function normalizeMediaUrl(rawUrl) {
  try {
    const u = new URL(rawUrl);
    const host = u.hostname.toLowerCase();
    const isFbcdn = isFbcdnHost(host);
    const looksLikeMediaPath = /\.(?:mp4|m4v|m4s|webm|flv|mov|mkv|ts)$/i.test(u.pathname);
    const rangeKeys = [];
    for (const key of u.searchParams.keys()) {
      if (RANGE_QUERY_PARAMS.has(key.toLowerCase())) {
        rangeKeys.push(key);
      }
    }
    const hasFbRange = rangeKeys.some((k) => {
      const low = k.toLowerCase();
      return low === "bytestart" || low === "byteend" || low === "byte_start" || low === "byte_end";
    });
    if (rangeKeys.length > 0 && (isFbcdn || looksLikeMediaPath || hasFbRange)) {
      for (const key of rangeKeys) {
        u.searchParams.delete(key);
      }
      return { url: u.toString(), strippedRange: true };
    }
    return { url: u.toString(), strippedRange: false };
  } catch {
    return { url: rawUrl, strippedRange: false };
  }
}

// Canonical key for deduplicating detected media streams. Facebook serves
// chunks of the same stream across edge subdomains and with per-request
// tracking params, so key fbcdn.net assets by their pathname.
function getMediaDedupKey(rawUrl) {
  try {
    const u = new URL(rawUrl);
    const host = u.hostname.toLowerCase();
    if (isFbcdnHost(host) && u.pathname && u.pathname !== "/") {
      return `fbcdn.net${u.pathname}`;
    }
    const kept = [];
    for (const [k, v] of u.searchParams.entries()) {
      if (EPHEMERAL_QUERY_PARAMS.has(k.toLowerCase())) continue;
      kept.push([k, v]);
    }
    kept.sort((a, b) => (a[0] === b[0] ? (a[1] < b[1] ? -1 : a[1] > b[1] ? 1 : 0) : a[0] < b[0] ? -1 : 1));
    const search = kept.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join("&");
    return `${u.origin}${u.pathname}${search ? `?${search}` : ""}`;
  } catch {
    return String(rawUrl || "");
  }
}

// Weibo exposes rendition hints as ?label=mp4_720p, ?label=mp4_hd, ?label=mp4_ld,
// ?label=mp4_2k, ?label=mp4_4k or ?template=720x1280.24.0.
// Facebook exposes rendition hints inside base64-encoded ?efg= JSON.
function inferQualityFromUrl(rawUrl) {
  try {
    const u = new URL(rawUrl);
    const label = (u.searchParams.get("label") || "").toLowerCase();
    const labelMatch = label.match(/(\d{3,4})p/);
    if (labelMatch) {
      const height = Number.parseInt(labelMatch[1], 10);
      if (height >= 144 && height <= 4320) return { quality: `${height}p`, height };
    }
    if (label.includes("4k")) return { quality: "2160p", height: 2160 };
    if (label.includes("2k")) return { quality: "1440p", height: 1440 };
    if (label === "mp4_hd" || label.endsWith("_hd")) return { quality: "720p", height: 720 };
    if (label === "mp4_ld" || label.endsWith("_ld") || label === "mp4_sd" || label.endsWith("_sd")) {
      return { quality: "360p", height: 360 };
    }
    const template = u.searchParams.get("template") || "";
    const templateMatch = template.match(/(\d{3,4})x(\d{3,4})/i);
    if (templateMatch) {
      const height = Math.min(Number.parseInt(templateMatch[1], 10), Number.parseInt(templateMatch[2], 10));
      if (height >= 144 && height <= 4320) return { quality: `${height}p`, height };
    }
    const efg = parseFacebookEfg(rawUrl);
    if (efg && typeof efg.vencode_tag === "string") {
      const tag = efg.vencode_tag;
      const efgMatch = tag.match(/(\d{3,4})p/i);
      if (efgMatch) {
        const height = Number.parseInt(efgMatch[1], 10);
        if (height >= 144 && height <= 4320) return { quality: `${height}p`, height };
      }
      // Facebook progressive tags: xpv_progressive.FACEBOOK..C3.360.sve_sd
      const progMatch = tag.match(/\.C\d+\.(\d{3,4})\./i);
      if (progMatch) {
        const height = Number.parseInt(progMatch[1], 10);
        if (height >= 144 && height <= 4320) return { quality: `${height}p`, height };
      }
      // Facebook adaptive DASH quality tiers: dash_r2av1-r1gen2vp9-adv_q20..q90
      const qMatch = tag.match(/_q(\d{2,3})(?:$|_)/i);
      if (qMatch) {
        const q = Number.parseInt(qMatch[1], 10);
        const height =
          q >= 80 ? 1080 :
          q >= 60 ? 720 :
          q >= 50 ? 540 :
          q >= 40 ? 480 :
          q >= 30 ? 360 : 240;
        return { quality: `${height}p`, height };
      }
      if (/(?:^|[._-])hd(?:$|[._-])/i.test(tag)) return { quality: "720p", height: 720 };
      if (/(?:^|[._-])sd(?:$|[._-])/i.test(tag)) return { quality: "360p", height: 360 };
    }
    const generic = rawUrl.match(/(?:^|[_\-/=?&])(2160|1440|1080|720|540|480|360|240)p(?:$|[_\-/.&?])/i);
    if (generic) {
      const height = Number.parseInt(generic[1], 10);
      return { quality: `${height}p`, height };
    }
  } catch {
    // ignore
  }
  return { quality: "Original", height: 0 };
}

// Weibo/Facebook CDNs often serve video bytes without a media extension and as
// application/octet-stream; URL shape and range params are the only signals
// webRequest gives us.
function isKnownSocialVideoURL(rawUrl) {
  try {
    const u = new URL(rawUrl);
    const host = u.hostname.toLowerCase();
    const path = u.pathname.toLowerCase();
    const label = (u.searchParams.get("label") || "").toLowerCase();
    if (host === "weibocdn.com" || host.endsWith(".weibocdn.com")) {
      return (
        label.startsWith("mp4_") ||
        u.searchParams.has("template") ||
        path.includes("/o0/") ||
        path.includes("/o1/") ||
        path.includes(".mp4") ||
        path.includes(".flv")
      );
    }
    if (host.endsWith(".sinaimg.cn")) {
      return label.startsWith("mp4_") || path.includes("video");
    }
    if (host === "fbcdn.net" || host.endsWith(".fbcdn.net")) {
      return (
        path.includes(".mp4") ||
        host.startsWith("video") ||
        u.searchParams.has("bytestart") ||
        u.searchParams.has("byteend")
      );
    }
  } catch {
    // ignore
  }
  return false;
}

function isYouTubeRangeChunk(rawUrl) {
  try {
    const u = new URL(rawUrl);
    const host = u.hostname.toLowerCase();
    if (host !== "googlevideo.com" && !host.endsWith(".googlevideo.com")) return false;
    if (u.pathname !== "/videoplayback") return false;
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

function isBilibiliMediaChunk(rawUrl) {
  try {
    const u = new URL(rawUrl);
    const host = u.hostname.toLowerCase();
    if (
      host === "bilivideo.com" ||
      host.endsWith(".bilivideo.com") ||
      host === "bilivideo.cn" ||
      host.endsWith(".bilivideo.cn")
    ) {
      return true;
    }
  } catch {
    // ignore
  }
  return false;
}

function classifyMediaResponse(url, headers) {
  // YouTube MSE players fetch byte-range/UMP chunks from googlevideo.com;
  // those are partial fragments (not full files) and are superseded by the
  // playerResponse streams extracted in the page.
  if (isYouTubeRangeChunk(url)) {
    return null;
  }
  // Bilibili MSE players fetch .m4s/DASH chunks from bilivideo.com/bilivideo.cn;
  // those are superseded by the __playinfo__ / playurl streams extracted in-page.
  if (isBilibiliMediaChunk(url)) {
    return null;
  }
  const { url: mediaUrl, strippedRange } = normalizeMediaUrl(url);
  const lowerPath = mediaUrl.toLowerCase().split("?")[0].split("#")[0];
  let contentType = "";
  let contentLength = 0;
  let contentRange = null;

  if (Array.isArray(headers)) {
    for (const h of headers) {
      const name = (h.name || "").toLowerCase();
      if (name === "content-type") contentType = (h.value || "").toLowerCase();
      if (name === "content-length") contentLength = Number.parseInt(h.value || "0", 10) || 0;
      if (name === "content-range") contentRange = parseContentRange(h.value);
    }
  }

  // Audio-only DASH tracks are not a video candidate.
  if (contentType.startsWith("audio/") && !contentType.includes("mpegurl")) {
    return null;
  }

  // Individual DASH segment files (.m4s, .cmfv, etc.) are not standalone videos.
  if (isDashSegmentUrl(mediaUrl, contentType)) {
    return null;
  }

  // Facebook audio-only DASH tracks often arrive as video/mp4 or octet-stream
  // with an audio vencode_tag in the efg metadata.
  const efg = parseFacebookEfg(mediaUrl);
  if (efg && typeof efg.vencode_tag === "string" && /audio/i.test(efg.vencode_tag) && !/video/i.test(efg.vencode_tag)) {
    return null;
  }

  const fbBandwidth = efg && Number.isFinite(Number(efg.bitrate)) && Number(efg.bitrate) > 0 ? Number(efg.bitrate) : 0;
  const fbDuration = efg && Number.isFinite(Number(efg.duration_s)) && Number(efg.duration_s) > 0 ? Number(efg.duration_s) : 0;
  const fbVideoId = efg ? String(efg.video_id || efg.xpv_asset_id || "") : "";
  const isFbDash = Boolean(efg && typeof efg.vencode_tag === "string" && /^dash_/i.test(efg.vencode_tag));

  // A 206 response only reports the chunk length; Content-Range carries the
  // full resource size. When query range params (bytestart/byteend/range) were
  // stripped from a 200 OK chunk response, contentLength is only the chunk size
  // (e.g. 352 KB), not the full video size. Similarly, Facebook DASH preload
  // requests without explicit range params can return a partial chunk much
  // smaller than bitrate * duration_s / 8.
  let effectiveSize = (contentRange && contentRange.total) || (strippedRange ? 0 : contentLength);
  if (isFbDash && !contentRange && effectiveSize > 0 && fbBandwidth > 0 && fbDuration > 0) {
    const estimatedFull = (fbBandwidth * fbDuration) / 8;
    if (effectiveSize < estimatedFull * 0.5) {
      effectiveSize = 0;
    }
  }
  const quality = inferQualityFromUrl(mediaUrl);
  const extraMeta = {};
  if (fbBandwidth > 0) extraMeta.bandwidth = fbBandwidth;
  if (fbVideoId) extraMeta.fbVideoId = fbVideoId;
  if (isFbDash) extraMeta.isFbDash = true;
  const tiny = !strippedRange && effectiveSize > 0 && effectiveSize < 256 * 1024;

  if (lowerPath.endsWith(".m3u8") || contentType.includes("mpegurl") || contentType.includes("x-mpegurl")) {
    return { kind: "hls", url: mediaUrl, format: "HLS", size: effectiveSize, ...quality };
  }

  // DASH manifests are parsed by the page's content script (it has DOMParser and
  // the frame-local player); a bare MPD URL is not a downloadable file.
  if (lowerPath.endsWith(".mpd") || contentType.includes("dash+xml")) {
    return null;
  }

  const formatsByExt = [
    [".mp4", "MP4"],
    [".m4v", "MP4"],
    [".webm", "WebM"],
    [".mkv", "MKV"],
    [".mov", "MOV"],
    [".flv", "FLV"]
  ];

  if (contentType.startsWith("video/") && !contentType.includes("mp2t")) {
    let format = "MP4";
    if (contentType.includes("webm") || lowerPath.endsWith(".webm")) format = "WebM";
    else if (contentType.includes("matroska") || lowerPath.endsWith(".mkv")) format = "MKV";
    else if (contentType.includes("quicktime") || lowerPath.endsWith(".mov")) format = "MOV";
    else if (contentType.includes("flv") || lowerPath.endsWith(".flv")) format = "FLV";
    if (tiny) return null;
    return { kind: "direct", url: mediaUrl, format, size: effectiveSize, ...quality, ...extraMeta };
  }

  for (const [ext, format] of formatsByExt) {
    if (lowerPath.endsWith(ext)) {
      if (tiny) return null;
      return { kind: "direct", url: mediaUrl, format, size: effectiveSize, ...quality, ...extraMeta };
    }
  }

  // application/octet-stream (Weibo) or extension-less CDN chunks: only trust
  // known media hosts, explicit range params, or a rendition label.
  const looksBinary = contentType.includes("octet-stream") || contentType.includes("binary/") || contentType === "";
  if (looksBinary || strippedRange) {
    if (!isKnownSocialVideoURL(mediaUrl) && !strippedRange) return null;
    if (tiny) return null;
    const format = lowerPath.includes(".flv") ? "FLV" : "MP4";
    return { kind: "direct", url: mediaUrl, format, size: effectiveSize, ...quality, ...extraMeta };
  }

  if (isKnownSocialVideoURL(mediaUrl)) {
    if (tiny) return null;
    const format = lowerPath.includes(".flv") ? "FLV" : "MP4";
    return { kind: "direct", url: mediaUrl, format, size: effectiveSize, ...quality, ...extraMeta };
  }
  return null;
}

if (typeof chrome !== "undefined" && chrome.webRequest && chrome.webRequest.onHeadersReceived) {
  chrome.webRequest.onHeadersReceived.addListener(
    (details) => {
      if (details.tabId < 0 || details.statusCode < 200 || details.statusCode >= 300) {
        return;
      }
      const media = classifyMediaResponse(details.url, details.responseHeaders);
      if (!media) {
        const fbAudio = classifyFacebookAudioTrack(details.url, details.responseHeaders);
        if (fbAudio) {
          addDetectedFacebookAudio(details.tabId, fbAudio);
        }
        return;
      }

      if (media.kind === "hls") {
        inspectHLSPlaylist(details.tabId, media.url);
      } else {
        addDetectedVideo(details.tabId, {
          url: media.url,
          quality: media.quality || "Original",
          height: media.height || 0,
          bandwidth: media.bandwidth || 0,
          fbVideoId: media.fbVideoId || "",
          isFbDash: Boolean(media.isFbDash),
          format: media.format,
          size: media.size,
          isHLS: false
        });
      }
    },
    { urls: ["http://*/*", "https://*/*"] },
    ["responseHeaders"]
  );
}

if (typeof chrome !== "undefined" && chrome.tabs) {
  chrome.tabs.onRemoved.addListener((tabId) => {
    tabVideos.delete(tabId);
    tabFbAudios.delete(tabId);
  });

  chrome.tabs.onUpdated.addListener((tabId, changeInfo) => {
    if (changeInfo.status === "loading" && changeInfo.url) {
      tabVideos.delete(tabId);
      tabFbAudios.delete(tabId);
      updateTabBadge(tabId);
    }
  });
}

// --- Context Menus & Lifecycle ---

// create() reports "duplicate id" via runtime.lastError when the item already
// exists (menus survive service worker restarts). Swallow that one error so a
// cold start can heal a missing menu without wiping the registered set.
function createContextMenu(props) {
  chrome.contextMenus.create(props, () => {
    const err = chrome.runtime.lastError;
    if (err && !/duplicate id/i.test(err.message || "")) {
      console.warn("[MagicDown] context menu create failed:", props.id, err.message);
    }
  });
}

function ensureContextMenus() {
  createContextMenu({
    id: "magicdown-download",
    title: t("menu.download"),
    contexts: ["link", "video", "audio"]
  });

  createContextMenu({
    id: "magicdown-download-all",
    title: t("menu.download_all"),
    contexts: ["page", "frame", "selection", "link", "image", "video", "audio"]
  });

  // Nested MagicDown menu for right-clicked images with real format conversion
  createContextMenu({
    id: "magicdown-image-parent",
    title: t("menu.image_root"),
    contexts: ["image"]
  });
  createContextMenu({
    id: "magicdown-img-original",
    parentId: "magicdown-image-parent",
    title: t("menu.image_original"),
    contexts: ["image"]
  });
  createContextMenu({
    id: "magicdown-img-png",
    parentId: "magicdown-image-parent",
    title: t("menu.image_png"),
    contexts: ["image"]
  });
  createContextMenu({
    id: "magicdown-img-jpg",
    parentId: "magicdown-image-parent",
    title: t("menu.image_jpg"),
    contexts: ["image"]
  });
  createContextMenu({
    id: "magicdown-img-webp",
    parentId: "magicdown-image-parent",
    title: t("menu.image_webp"),
    contexts: ["image"]
  });
}

function setupContextMenus() {
  const apply = () => {
    chrome.contextMenus.removeAll(() => {
      ensureContextMenus();
    });
  };
  if (typeof chrome !== "undefined" && chrome.storage && chrome.storage.local && I18N) {
    chrome.storage.local.get(["language"], (stored) => {
      I18N.setLang(I18N.resolveLang(stored && stored.language));
      apply();
    });
    return;
  }
  apply();
}

// Re-assert menus on cold start and rebuild immediately when language changes.
if (typeof chrome !== "undefined" && chrome.contextMenus) {
  setupContextMenus();
}
if (typeof chrome !== "undefined" && chrome.storage && chrome.storage.onChanged) {
  chrome.storage.onChanged.addListener((changes, areaName) => {
    if (areaName === "local" && changes.language && I18N) {
      I18N.setLang(I18N.resolveLang(changes.language.newValue));
      if (chrome.contextMenus) {
        setupContextMenus();
      }
    }
  });
}

if (typeof chrome !== "undefined" && chrome.runtime && chrome.runtime.onInstalled) {
  chrome.runtime.onInstalled.addListener(async () => {
    const current = await chrome.storage.local.get([
      "interceptEnabled",
      "extensions",
      "minFileSizeMB",
      "excludedDomains",
      "theme"
    ]);
    await chrome.storage.local.set({
      interceptEnabled: current.interceptEnabled ?? DEFAULT_SETTINGS.interceptEnabled,
      extensions: current.extensions ?? DEFAULT_SETTINGS.extensions,
      minFileSizeMB: current.minFileSizeMB ?? DEFAULT_SETTINGS.minFileSizeMB,
      excludedDomains: current.excludedDomains ?? DEFAULT_SETTINGS.excludedDomains,
      theme: current.theme ?? DEFAULT_SETTINGS.theme
    });
    setupContextMenus();
  });
}

if (typeof chrome !== "undefined" && chrome.runtime && chrome.runtime.onStartup) {
  chrome.runtime.onStartup.addListener(setupContextMenus);
}

const IMAGE_MENU_FORMATS = {
  "magicdown-img-original": "",
  "magicdown-img-png": "png",
  "magicdown-img-jpg": "jpg",
  "magicdown-img-webp": "webp"
};

if (typeof chrome !== "undefined" && chrome.contextMenus && chrome.contextMenus.onClicked) {
  chrome.contextMenus.onClicked.addListener(async (info, tab) => {
    if (info.menuItemId === "magicdown-download-all") {
      if (tab && typeof tab.id === "number") {
        await scanAndOpenBatchPicker(tab.id);
      }
      return;
    }

    if (info.menuItemId in IMAGE_MENU_FORMATS) {
      const targetUrl = info.srcUrl || info.linkUrl;
      if (!targetUrl) return;
      await forwardDownload({
        url: targetUrl,
        referrer: info.pageUrl || "",
        convertFormat: IMAGE_MENU_FORMATS[info.menuItemId]
      });
      return;
    }

    if (info.menuItemId === "magicdown-download") {
      const targetUrl = info.linkUrl || info.srcUrl;
      if (!targetUrl) return;
      await forwardDownload({
        url: targetUrl,
        referrer: info.pageUrl || ""
      });
    }
  });
}

if (typeof chrome !== "undefined" && chrome.downloads && chrome.downloads.onCreated) {
  chrome.downloads.onCreated.addListener(async (downloadItem) => {
    const settings = await getSettings();
    if (!settings.interceptEnabled) {
      return;
    }
    if (!shouldIntercept(downloadItem, settings)) {
      return;
    }

    const url = downloadItem.finalUrl || downloadItem.url;

    try {
      await chrome.downloads.pause(downloadItem.id);
    } catch {
      // ignore
    }

    const resp = await forwardDownload({
      url,
      filename: downloadItem.filename || "",
      referrer: downloadItem.referrer || ""
    });

    if (resp && resp.success) {
      try {
        await chrome.downloads.cancel(downloadItem.id);
        await chrome.downloads.erase({ id: downloadItem.id });
      } catch {
        // ignore
      }
    } else {
      try {
        await chrome.downloads.resume(downloadItem.id);
      } catch {
        // ignore
      }
    }
  });
}

if (typeof chrome !== "undefined" && chrome.runtime && chrome.runtime.onMessage) {
  chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    if (message.type === "ping") {
      sendNativeMessage({ action: "ping" }).then(sendResponse);
      return true;
    }
    if (message.type === "download") {
      forwardDownload({
        url: message.url,
        audioUrl: message.audioUrl || "",
        filename: message.filename || "",
        referrer: message.referrer || "",
        headers: message.headers || null,
        convertFormat: message.convertFormat || "",
        targetFormat: message.targetFormat || "",
        outputFormat: message.outputFormat || "",
        audioBitrate: message.audioBitrate || "",
        plan: message.plan || null,
        size: typeof message.size === "number" ? message.size : 0
      }).then(sendResponse);
      return true;
    }
    if (message.type === "clearTabVideos") {
      const tabId = typeof message.tabId === "number" ? message.tabId : (sender.tab ? sender.tab.id : -1);
      if (tabId >= 0) {
        tabVideos.delete(tabId);
        tabFbAudios.delete(tabId);
        updateTabBadge(tabId);
      }
      sendResponse({ ok: true });
      return false;
    }
    if (message.type === "fetchManifestText") {
      fetchManifestText(message.url).then(sendResponse);
      return true;
    }
    if (message.type === "getDetectedVideos") {
      const tabId = typeof message.tabId === "number" ? message.tabId : (sender.tab ? sender.tab.id : -1);
      sendResponse({ videos: getTabVideoList(tabId) });
      return false;
    }
    if (message.type === "openBatchPicker") {
      const tabId = typeof message.tabId === "number" ? message.tabId : (sender.tab ? sender.tab.id : -1);
      scanAndOpenBatchPicker(tabId).then(sendResponse);
      return true;
    }
    if (message.type === "getBatchScanResult") {
      const data = batchScanResults.get(message.scanId || "");
      sendResponse(data ? { ok: true, data } : { ok: false, error: "scan not found" });
      return false;
    }
    if (message.type === "rescanTabResources") {
      const tabId = typeof message.tabId === "number" ? message.tabId : -1;
      scanTabResources(tabId).then((res) => {
        if (res && res.ok) {
          sendResponse({ ok: true, data: res.data });
        } else {
          sendResponse({ ok: false, error: (res && res.error) || "failed to rescan tab" });
        }
      });
      return true;
    }
    if (message.type === "batchDownload") {
      forwardBatchDownload({
        items: Array.isArray(message.items) ? message.items : [],
        pageUrl: message.pageUrl || "",
        subfolder: message.subfolder || ""
      }).then(sendResponse);
      return true;
    }
    return false;
  });
}

async function scanTabResources(tabId) {
  if (typeof tabId !== "number" || tabId < 0) {
    return { ok: false, error: "invalid tab" };
  }
  return new Promise((resolve) => {
    chrome.tabs.sendMessage(tabId, { type: "scanPageResources" }, { frameId: 0 }, (resp) => {
      if (chrome.runtime.lastError || !resp || !resp.ok) {
        resolve({
          ok: false,
          error: (chrome.runtime.lastError && chrome.runtime.lastError.message) || "Cannot scan this page"
        });
        return;
      }
      resolve({
        ok: true,
        data: {
          tabId,
          pageTitle: resp.pageTitle || "",
          pageUrl: resp.pageUrl || "",
          items: Array.isArray(resp.items) ? resp.items : [],
          createdAt: Date.now()
        }
      });
    });
  });
}

async function scanAndOpenBatchPicker(tabId) {
  const res = await scanTabResources(tabId);
  if (!res.ok) {
    return res;
  }
  const scanId = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  batchScanResults.set(scanId, res.data);
  while (batchScanResults.size > MAX_BATCH_SCANS) {
    const oldestKey = batchScanResults.keys().next().value;
    batchScanResults.delete(oldestKey);
  }
  const pickerUrl = chrome.runtime.getURL(`batch-picker.html?scanId=${encodeURIComponent(scanId)}`);
  await chrome.tabs.create({ url: pickerUrl });
  return { ok: true, scanId, count: res.data.items.length };
}

function buildBatchDownloadPayload({ items, pageUrl = "", subfolder = "", cookies = "", headers = null }) {
  const safeHeaders = sanitizeHeaders(headers);
  const normalizedItems = [];
  const seen = new Set();
  for (const item of Array.isArray(items) ? items : []) {
    if (!item || typeof item.url !== "string") continue;
    const url = item.url.trim();
    if (!/^https?:\/\//i.test(url) || seen.has(url)) continue;
    seen.add(url);
    normalizedItems.push({
      url,
      filename: typeof item.filename === "string" ? item.filename.trim() : "",
      referrer: item.referrer || pageUrl || "",
      auto_start: true,
      skip_dialog: true
    });
  }
  return {
    action: "batch_download",
    items: normalizedItems,
    referrer: pageUrl || "",
    cookies: cookies || "",
    headers: safeHeaders,
    subfolder: typeof subfolder === "string" ? subfolder.trim() : "",
    auto_start: true
  };
}

async function forwardBatchDownload({ items, pageUrl = "", subfolder = "", headers = null }) {
  if (!Array.isArray(items) || items.length === 0) {
    return { success: false, error: "no items selected" };
  }
  const cookies = pageUrl ? await getCookiesHeader(pageUrl) : "";
  const payload = buildBatchDownloadPayload({ items, pageUrl, subfolder, cookies, headers });
  if (payload.items.length === 0) {
    return { success: false, error: "no valid downloadable URLs" };
  }

  const resp = await sendNativeMessage(payload);
  await recordActivity({
    id: `batch-${Date.now()}`,
    filename: `Batch (${payload.items.length} items)${subfolder ? ` → ${subfolder}` : ""}`,
    host: extractHost(pageUrl || payload.items[0].url),
    url: pageUrl || payload.items[0].url,
    status: resp && resp.success ? "sent" : "failed",
    error: resp && !resp.success ? (resp.error || "Failed to connect") : ""
  });
  return resp;
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    normalizeMediaUrl,
    getMediaDedupKey,
    parseFacebookEfg,
    isDashSegmentUrl,
    isBilibiliMediaChunk,
    inferQualityFromUrl,
    classifyMediaResponse,
    classifyFacebookAudioTrack,
    addDetectedVideo,
    addDetectedFacebookAudio,
    getTabVideoList,
    buildDownloadPayload,
    buildBatchDownloadPayload,
    tabVideos,
    tabFbAudios
  };
}

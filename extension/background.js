const HOST_NAME = "com.magicdown.downloader";
const MAX_HISTORY = 25;

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

async function forwardDownload({ url, filename = "", referrer = "", convertFormat = "" }) {
  const cookies = await getCookiesHeader(url);
  const payload = {
    action: "download",
    url,
    filename,
    referrer,
    cookies,
    auto_start: true
  };
  if (convertFormat) {
    payload.convert_format = convertFormat;
  }

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

function getTabVideoList(tabId) {
  const map = tabVideos.get(tabId);
  if (!map) return [];
  return Array.from(map.values()).sort((a, b) => {
    if ((b.height || 0) !== (a.height || 0)) return (b.height || 0) - (a.height || 0);
    if ((b.bandwidth || 0) !== (a.bandwidth || 0)) return (b.bandwidth || 0) - (a.bandwidth || 0);
    return (b.size || 0) - (a.size || 0);
  });
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

function addDetectedVideo(tabId, item) {
  if (tabId < 0 || !item || !item.url) return;
  let map = tabVideos.get(tabId);
  if (!map) {
    map = new Map();
    tabVideos.set(tabId, map);
  }
  const existing = map.get(item.url);
  map.set(item.url, {
    ...existing,
    ...item,
    size: item.size || (existing && existing.size) || 0
  });
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

function classifyMediaResponse(url, headers) {
  const lowerUrl = url.toLowerCase().split("?")[0];
  let contentType = "";
  let contentLength = 0;

  if (Array.isArray(headers)) {
    for (const h of headers) {
      const name = (h.name || "").toLowerCase();
      if (name === "content-type") contentType = (h.value || "").toLowerCase();
      if (name === "content-length") contentLength = Number.parseInt(h.value || "0", 10) || 0;
    }
  }

  // HLS playlist
  if (
    lowerUrl.endsWith(".m3u8") ||
    contentType.includes("mpegurl") ||
    contentType.includes("x-mpegurl")
  ) {
    return { kind: "hls", format: "HLS", size: contentLength };
  }

  // Skip tiny segment/range probes (< 256 KB) for direct video files
  if (contentType.startsWith("video/") && !contentType.includes("mp2t")) {
    let format = "MP4";
    if (contentType.includes("webm") || lowerUrl.endsWith(".webm")) format = "WebM";
    else if (contentType.includes("matroska") || lowerUrl.endsWith(".mkv")) format = "MKV";
    else if (contentType.includes("quicktime") || lowerUrl.endsWith(".mov")) format = "MOV";
    if (contentLength > 0 && contentLength < 256 * 1024) return null;
    return { kind: "direct", format, size: contentLength };
  }

  for (const [ext, fmt] of [[".mp4", "MP4"], [".webm", "WebM"], [".mkv", "MKV"], [".mov", "MOV"]]) {
    if (lowerUrl.endsWith(ext)) {
      if (contentLength > 0 && contentLength < 256 * 1024) return null;
      return { kind: "direct", format: fmt, size: contentLength };
    }
  }
  return null;
}

if (chrome.webRequest && chrome.webRequest.onHeadersReceived) {
  chrome.webRequest.onHeadersReceived.addListener(
    (details) => {
      if (details.tabId < 0 || details.statusCode < 200 || details.statusCode >= 300) {
        return;
      }
      const media = classifyMediaResponse(details.url, details.responseHeaders);
      if (!media) return;

      if (media.kind === "hls") {
        inspectHLSPlaylist(details.tabId, details.url);
      } else {
        addDetectedVideo(details.tabId, {
          url: details.url,
          quality: "Original",
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

chrome.tabs.onRemoved.addListener((tabId) => {
  tabVideos.delete(tabId);
});

chrome.tabs.onUpdated.addListener((tabId, changeInfo) => {
  if (changeInfo.status === "loading" && changeInfo.url) {
    tabVideos.delete(tabId);
    updateTabBadge(tabId);
  }
});

// --- Context Menus & Lifecycle ---

function setupContextMenus() {
  chrome.contextMenus.removeAll(() => {
    chrome.contextMenus.create({
      id: "magicdown-download",
      title: "Download with MagicDown",
      contexts: ["link", "video", "audio"]
    });

    // Nested MagicDown menu for right-clicked images with real format conversion
    chrome.contextMenus.create({
      id: "magicdown-image-parent",
      title: "MagicDown",
      contexts: ["image"]
    });
    chrome.contextMenus.create({
      id: "magicdown-img-original",
      parentId: "magicdown-image-parent",
      title: "Download Image (Original)",
      contexts: ["image"]
    });
    chrome.contextMenus.create({
      id: "magicdown-img-png",
      parentId: "magicdown-image-parent",
      title: "Save Image as PNG",
      contexts: ["image"]
    });
    chrome.contextMenus.create({
      id: "magicdown-img-jpg",
      parentId: "magicdown-image-parent",
      title: "Save Image as JPG",
      contexts: ["image"]
    });
    chrome.contextMenus.create({
      id: "magicdown-img-webp",
      parentId: "magicdown-image-parent",
      title: "Save Image as WebP",
      contexts: ["image"]
    });
  });
}

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

chrome.runtime.onStartup.addListener(setupContextMenus);

const IMAGE_MENU_FORMATS = {
  "magicdown-img-original": "",
  "magicdown-img-png": "png",
  "magicdown-img-jpg": "jpg",
  "magicdown-img-webp": "webp"
};

chrome.contextMenus.onClicked.addListener(async (info) => {
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

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === "ping") {
    sendNativeMessage({ action: "ping" }).then(sendResponse);
    return true;
  }
  if (message.type === "download") {
    forwardDownload({
      url: message.url,
      filename: message.filename || "",
      referrer: message.referrer || "",
      convertFormat: message.convertFormat || ""
    }).then(sendResponse);
    return true;
  }
  if (message.type === "getDetectedVideos") {
    const tabId = typeof message.tabId === "number" ? message.tabId : (sender.tab ? sender.tab.id : -1);
    sendResponse({ videos: getTabVideoList(tabId) });
    return false;
  }
  return false;
});

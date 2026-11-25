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

  // Respect excluded domains (check both download URL and referrer)
  if (isDomainExcluded(url, settings.excludedDomains)) {
    return false;
  }
  if (downloadItem.referrer && isDomainExcluded(downloadItem.referrer, settings.excludedDomains)) {
    return false;
  }

  // Respect minimum file size threshold when known
  if (settings.minFileSizeMB > 0 && typeof downloadItem.fileSize === "number" && downloadItem.fileSize > 0) {
    const minBytes = settings.minFileSizeMB * 1024 * 1024;
    if (downloadItem.fileSize < minBytes) {
      return false;
    }
  }

  // Respect extension filter
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
      status: entry.status, // "sent" | "failed"
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

async function forwardDownload({ url, filename = "", referrer = "" }) {
  const cookies = await getCookiesHeader(url);
  const resp = await sendNativeMessage({
    action: "download",
    url,
    filename,
    referrer,
    cookies,
    auto_start: true
  });

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

  chrome.contextMenus.removeAll(() => {
    chrome.contextMenus.create({
      id: "magicdown-download",
      title: "Download with MagicDown",
      contexts: ["link", "video", "audio", "image"]
    });
  });
});

chrome.contextMenus.onClicked.addListener(async (info) => {
  if (info.menuItemId !== "magicdown-download") {
    return;
  }
  const targetUrl = info.linkUrl || info.srcUrl;
  if (!targetUrl) {
    return;
  }
  await forwardDownload({
    url: targetUrl,
    referrer: info.pageUrl || ""
  });
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

  // Pause browser download while handing off to MagicDown
  try {
    await chrome.downloads.pause(downloadItem.id);
  } catch {
    // Some downloads may not be pausable immediately
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
      // Ignore cleanup errors
    }
  } else {
    // Desktop app is offline or host not installed: let the browser continue
    try {
      await chrome.downloads.resume(downloadItem.id);
    } catch {
      // Ignore resume errors
    }
  }
});

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message.type === "ping") {
    sendNativeMessage({ action: "ping" }).then(sendResponse);
    return true;
  }
  if (message.type === "download") {
    forwardDownload({
      url: message.url,
      filename: message.filename || "",
      referrer: message.referrer || ""
    }).then(sendResponse);
    return true;
  }
  return false;
});

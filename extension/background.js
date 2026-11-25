const HOST_NAME = "com.magicdown.downloader";

const DEFAULT_SETTINGS = {
  interceptEnabled: true,
  extensions: [
    "zip", "rar", "7z", "tar", "gz", "iso",
    "exe", "msi", "dmg", "apk", "bin",
    "mp4", "mkv", "avi", "mov", "webm",
    "mp3", "flac", "wav", "pdf"
  ]
};

async function getSettings() {
  const stored = await chrome.storage.local.get(["interceptEnabled", "extensions"]);
  return {
    interceptEnabled: stored.interceptEnabled !== undefined ? stored.interceptEnabled : DEFAULT_SETTINGS.interceptEnabled,
    extensions: Array.isArray(stored.extensions) ? stored.extensions : DEFAULT_SETTINGS.extensions
  };
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

function shouldIntercept(url, filename, allowedExts) {
  if (!url || (!url.startsWith("http://") && !url.startsWith("https://"))) {
    return false;
  }
  if (!allowedExts || allowedExts.length === 0 || allowedExts.includes("*")) {
    return true;
  }
  const ext = getExtensionFromUrlOrFilename(url, filename);
  return ext !== "" && allowedExts.includes(ext);
}

async function getCookiesHeader(url) {
  try {
    const cookies = await chrome.cookies.getAll({ url });
    if (!cookies || cookies.length === 0) {
      return "";
    }
    return cookies.map(c => `${c.name}=${c.value}`).join("; ");
  } catch {
    return "";
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
  return sendNativeMessage({
    action: "download",
    url,
    filename,
    referrer,
    cookies,
    auto_start: true
  });
}

chrome.runtime.onInstalled.addListener(async () => {
  const current = await chrome.storage.local.get(["interceptEnabled", "extensions"]);
  if (current.interceptEnabled === undefined || current.extensions === undefined) {
    await chrome.storage.local.set({
      interceptEnabled: current.interceptEnabled ?? DEFAULT_SETTINGS.interceptEnabled,
      extensions: current.extensions ?? DEFAULT_SETTINGS.extensions
    });
  }

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

  const url = downloadItem.finalUrl || downloadItem.url;
  if (!shouldIntercept(url, downloadItem.filename, settings.extensions)) {
    return;
  }

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

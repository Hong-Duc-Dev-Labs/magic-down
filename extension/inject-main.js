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
  const MAX_ITEMS = 200;
  const MAX_URL = 4096;
  const MEDIA_PATH = /\.(?:mp4|m4s|m4v|m4a|m4p|webm|ts|m3u8|mpd|mov|mkv|mp3|aac)(?:$|[?#])/i;
  const RANGE_HINT = /(?:^|[?&])(?:bytestart|byteend|bytes|range)=/i;

  const items = [];
  const pending = [];
  let flushTimer = 0;
  let lastFlush = 0;
  let seq = 0;

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
    if (items.length === 0) return;
    const batch = items.slice(-MAX_ITEMS);
    try {
      window.postMessage({ __mdSniff: MSG, seq: ++seq, replay: true, items: batch }, "*");
    } catch {
      // ignore
    }
  }

  try {
    window.addEventListener(
      "message",
      (event) => {
        if (event.source !== window) return;
        const data = event.data;
        if (!data || data.__mdSniff !== "magicdown:media-sniff-ready:v1") return;
        replay();
      },
      false
    );
  } catch {
    // ignore
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

  function extractWeiboGroups(data) {
    const groups = [];
    const stack = [data];
    let guard = 0;
    while (stack.length > 0 && guard < 6000) {
      guard++;
      const node = stack.pop();
      if (!node || typeof node !== "object") continue;
      if (Array.isArray(node)) {
        for (let i = 0; i < node.length; i++) stack.push(node[i]);
        continue;
      }

      if (Array.isArray(node.playback_list) && node.playback_list.length > 0) {
        const variants = [];
        for (const entry of node.playback_list) {
          const info = entry && entry.play_info;
          const meta = entry && entry.meta;
          const url = info && typeof info.url === "string" ? info.url : "";
          if (!url || !/^https?:/i.test(url)) continue;
          const label = (meta && (meta.quality_label || meta.label)) || "";
          const w = Number(info.width) || 0;
          const h = Number(info.height) || 0;
          const q = parseWeiboQuality(label, w, h);
          variants.push({
            url: url.slice(0, MAX_URL),
            quality: q.quality,
            height: q.height,
            size: Number(info.size || (meta && meta.size)) || 0,
            bandwidth: Number(info.bitrate || (meta && meta.bitrate)) || 0,
            format: /webm/i.test(String(info.mime || "")) ? "WebM" : "MP4"
          });
        }
        if (variants.length > 0) {
          const id = String(node.media_id || node.media_id_str || node.object_id || variants[0].url);
          groups.push({ id, variants });
        }
      }

      for (const key of Object.keys(node)) {
        const val = node[key];
        if (val && typeof val === "object") stack.push(val);
      }
    }
    return groups;
  }

  function inspectWeiboJsonText(text) {
    if (!text || text.length > 4 * 1024 * 1024 || !text.includes("playback_list")) return;
    try {
      const parsed = JSON.parse(text);
      const groups = extractWeiboGroups(parsed);
      if (groups.length === 0) return;
      const at = Date.now();
      for (const g of groups.slice(0, 30)) {
        const entry = { k: "weibo-group", id: g.id, v: g.variants, t: at };
        items.push(entry);
        if (items.length > MAX_ITEMS) items.splice(0, items.length - MAX_ITEMS);
        pending.push(entry);
        if (pending.length > MAX_ITEMS) pending.splice(0, pending.length - MAX_ITEMS);
      }
      scheduleFlush();
    } catch {
      // ignore non-JSON responses
    }
  }

  // --- fetch / XHR / sendBeacon ----------------------------------------------
  try {
    if (typeof window.fetch === "function") {
      wrapMethod(window, "fetch", (original) =>
        function (input) {
          let url = "";
          try {
            url = typeof input === "string" ? input : (input && input.url) || "";
            if (url) note(String(url), "fetch", "");
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
            if (url) {
              const strUrl = String(url);
              note(strUrl, "xhr", "");
              if (isWeiboApiURL(strUrl) && !this.__mdWeiboHooked) {
                this.__mdWeiboHooked = true;
                this.addEventListener("load", () => {
                  try {
                    if (typeof this.responseText === "string") {
                      inspectWeiboJsonText(this.responseText);
                    }
                  } catch {
                    // ignore
                  }
                });
              }
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

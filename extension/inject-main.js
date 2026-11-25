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

  // --- fetch / XHR / sendBeacon ----------------------------------------------
  try {
    if (typeof window.fetch === "function") {
      wrapMethod(window, "fetch", (original) =>
        function (input) {
          try {
            const url = typeof input === "string" ? input : input && input.url;
            if (url) note(String(url), "fetch", "");
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
    if (typeof XMLHttpRequest !== "undefined" && XMLHttpRequest.prototype) {
      wrapMethod(XMLHttpRequest.prototype, "open", (original) =>
        function (method, url) {
          try {
            if (url) note(String(url), "xhr", "");
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

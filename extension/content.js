(() => {
  if (window.__magicdownContentLoaded) return;
  window.__magicdownContentLoaded = true;

  const isExtensionEnv = typeof chrome !== "undefined" && Boolean(chrome.runtime && chrome.runtime.id);

  let networkVideos = [];
  let dismissedVideos = new WeakSet();
  let activeVideo = null;
  let dropdownOpen = false;
  let hostEl = null;
  let shadow = null;
  let overlayEl = null;
  let pillBtn = null;
  let countBadge = null;
  let dropdownEl = null;
  let listEl = null;

  function formatSize(bytes) {
    if (!bytes || bytes <= 0) return "";
    const mb = bytes / (1024 * 1024);
    if (mb >= 1024) return `${(mb / 1024).toFixed(2)} GB`;
    if (mb >= 1) return `${mb.toFixed(1)} MB`;
    return `${Math.round(bytes / 1024)} KB`;
  }

  function formatBandwidth(bw) {
    if (!bw || bw <= 0) return "";
    return `${(bw / 1000000).toFixed(1)} Mbps`;
  }

  function sanitizeTitleFilename(ext) {
    const raw = (document.title || "video")
      .replace(/[<>:"/\\|?*\x00-\x1f]/g, " ")
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 80);
    const base = raw || "video";
    return `${base}.${ext}`;
  }

  function collectDOMVideos(videoEl) {
    const found = [];
    const seen = new Set();
    const pushSrc = (src, typeAttr) => {
      if (!src || src.startsWith("blob:") || src.startsWith("data:") || seen.has(src)) return;
      seen.add(src);
      let fmt = "MP4";
      const low = (typeAttr || src).toLowerCase();
      if (low.includes("webm")) fmt = "WebM";
      else if (low.includes("mkv") || low.includes("matroska")) fmt = "MKV";
      else if (low.includes("m3u8")) fmt = "HLS";
      const h = videoEl && videoEl.videoHeight ? videoEl.videoHeight : 0;
      found.push({
        url: src,
        quality: h > 0 ? `${h}p` : "Original",
        height: h,
        format: fmt,
        isHLS: fmt === "HLS",
        size: 0
      });
    };

    if (videoEl) {
      pushSrc(videoEl.currentSrc || videoEl.src, "");
      for (const s of videoEl.querySelectorAll("source")) {
        pushSrc(s.src, s.type);
      }
    }
    return found;
  }

  function getCombinedVideos() {
    const map = new Map();
    for (const v of networkVideos) {
      if (v && v.url) map.set(v.url, v);
    }
    for (const v of collectDOMVideos(activeVideo)) {
      if (!map.has(v.url)) map.set(v.url, v);
    }
    return Array.from(map.values());
  }

  function ensureShadowUI() {
    if (hostEl) return;

    hostEl = document.createElement("div");
    hostEl.id = "magicdown-video-overlay-root";
    hostEl.style.cssText = "position:fixed;inset:0;pointer-events:none;z-index:2147483647;";
    document.documentElement.appendChild(hostEl);

    shadow = hostEl.attachShadow({ mode: "open" });

    const style = document.createElement("style");
    style.textContent = `
      * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Inter, Roboto, sans-serif; }
      .md-wrap {
        position: fixed;
        pointer-events: auto;
        display: flex;
        flex-direction: column;
        align-items: flex-end;
        gap: 8px;
      }
      .md-wrap.hidden { display: none; }
      .md-pill-row {
        display: inline-flex;
        align-items: center;
        background: rgba(16, 19, 27, 0.92);
        backdrop-filter: blur(12px);
        -webkit-backdrop-filter: blur(12px);
        border: 1px solid rgba(59, 130, 246, 0.45);
        border-radius: 999px;
        padding: 4px 5px 4px 6px;
        gap: 6px;
        box-shadow: 0 8px 24px rgba(0, 0, 0, 0.42);
      }
      .md-pill-btn {
        display: inline-flex;
        align-items: center;
        gap: 7px;
        border: none;
        background: transparent;
        color: #F1F5F9;
        font-size: 12px;
        font-weight: 600;
        cursor: pointer;
        padding: 2px 4px 2px 0;
        letter-spacing: -0.01em;
      }
      .md-bolt {
        width: 22px;
        height: 22px;
        border-radius: 999px;
        background: #3B82F6;
        color: #FFFFFF;
        display: inline-flex;
        align-items: center;
        justify-content: center;
        flex-shrink: 0;
      }
      .md-count {
        font-size: 10.5px;
        font-weight: 700;
        font-variant-numeric: tabular-nums;
        padding: 1px 6px;
        border-radius: 999px;
        background: rgba(59, 130, 246, 0.22);
        color: #60A5FA;
      }
      .md-dismiss {
        width: 22px;
        height: 22px;
        border-radius: 999px;
        border: none;
        background: transparent;
        color: #94A3B8;
        font-size: 14px;
        line-height: 1;
        cursor: pointer;
        display: inline-flex;
        align-items: center;
        justify-content: center;
      }
      .md-dismiss:hover { background: rgba(255,255,255,0.08); color: #F1F5F9; }
      .md-dropdown {
        width: 320px;
        background: rgba(16, 19, 27, 0.96);
        backdrop-filter: blur(14px);
        -webkit-backdrop-filter: blur(14px);
        border: 1px solid #2F3950;
        border-radius: 15px;
        padding: 11px;
        box-shadow: 0 14px 36px rgba(0, 0, 0, 0.52);
        display: flex;
        flex-direction: column;
        gap: 8px;
      }
      .md-dropdown.hidden { display: none; }
      .md-dd-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 8px;
        padding: 0 4px;
      }
      .md-dd-title {
        font-size: 10px;
        font-weight: 700;
        letter-spacing: 0.05em;
        color: #94A3B8;
        white-space: nowrap;
      }
      .md-dd-sub {
        font-size: 10.5px;
        color: #64748B;
        white-space: nowrap;
      }
      .md-list {
        display: flex;
        flex-direction: column;
        gap: 6px;
        max-height: 240px;
        overflow-y: auto;
      }
      .md-item {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 10px;
        width: 100%;
        padding: 8px 10px;
        border-radius: 11px;
        border: 1px solid #212838;
        background: #141923;
        color: #F1F5F9;
        cursor: pointer;
        text-align: left;
        transition: background 0.14s ease, border-color 0.14s ease;
      }
      .md-item:hover {
        background: #19202D;
        border-color: #3B82F6;
      }
      .md-item-left {
        display: flex;
        align-items: center;
        gap: 8px;
        min-width: 0;
      }
      .md-q-badge {
        font-size: 11px;
        font-weight: 700;
        font-variant-numeric: tabular-nums;
        padding: 2px 7px;
        border-radius: 999px;
        background: rgba(59, 130, 246, 0.18);
        color: #60A5FA;
        flex-shrink: 0;
      }
      .md-meta {
        font-size: 11.5px;
        color: #CBD5E1;
        font-variant-numeric: tabular-nums;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
      }
      .md-dl-pill {
        font-size: 11px;
        font-weight: 600;
        padding: 3px 9px;
        border-radius: 999px;
        background: #3B82F6;
        color: #FFFFFF;
        flex-shrink: 0;
      }
      .md-dl-pill.sent {
        background: rgba(16, 185, 129, 0.2);
        color: #10B981;
      }
    `;
    shadow.appendChild(style);

    overlayEl = document.createElement("div");
    overlayEl.className = "md-wrap hidden";

    const pillRow = document.createElement("div");
    pillRow.className = "md-pill-row";

    pillBtn = document.createElement("button");
    pillBtn.className = "md-pill-btn";
    pillBtn.type = "button";
    pillBtn.innerHTML = `<span class="md-bolt"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2 3 14h9l-1 8 10-12h-9l1-8z"/></svg></span><span>MagicDown · Download video</span>`;

    countBadge = document.createElement("span");
    countBadge.className = "md-count";
    countBadge.textContent = "1";
    pillBtn.appendChild(countBadge);

    const dismissBtn = document.createElement("button");
    dismissBtn.className = "md-dismiss";
    dismissBtn.type = "button";
    dismissBtn.title = "Dismiss for this video";
    dismissBtn.textContent = "×";
    dismissBtn.addEventListener("click", (e) => {
      e.stopPropagation();
      if (activeVideo) dismissedVideos.add(activeVideo);
      dropdownOpen = false;
      overlayEl.classList.add("hidden");
    });

    pillBtn.addEventListener("click", (e) => {
      e.stopPropagation();
      dropdownOpen = !dropdownOpen;
      renderDropdown();
    });

    pillRow.appendChild(pillBtn);
    pillRow.appendChild(dismissBtn);

    dropdownEl = document.createElement("div");
    dropdownEl.className = "md-dropdown hidden";

    const ddHeader = document.createElement("div");
    ddHeader.className = "md-dd-header";
    ddHeader.innerHTML = `<span class="md-dd-title">SELECT QUALITY & FORMAT</span><span class="md-dd-sub">1-click to MagicDown</span>`;

    listEl = document.createElement("div");
    listEl.className = "md-list";

    dropdownEl.appendChild(ddHeader);
    dropdownEl.appendChild(listEl);

    overlayEl.appendChild(pillRow);
    overlayEl.appendChild(dropdownEl);
    shadow.appendChild(overlayEl);

    document.addEventListener("click", () => {
      if (dropdownOpen) {
        dropdownOpen = false;
        dropdownEl.classList.add("hidden");
      }
    });
  }

  function renderDropdown() {
    if (!dropdownEl || !listEl) return;
    if (!dropdownOpen) {
      dropdownEl.classList.add("hidden");
      return;
    }

    const videos = getCombinedVideos();
    listEl.textContent = "";

    for (const v of videos) {
      const btn = document.createElement("button");
      btn.className = "md-item";
      btn.type = "button";

      const left = document.createElement("div");
      left.className = "md-item-left";

      const q = document.createElement("span");
      q.className = "md-q-badge";
      q.textContent = v.quality || "Original";

      const metaParts = [v.format || "MP4"];
      if (v.size > 0) metaParts.push(formatSize(v.size));
      else if (v.bandwidth > 0) metaParts.push(formatBandwidth(v.bandwidth));

      const meta = document.createElement("span");
      meta.className = "md-meta";
      meta.textContent = metaParts.join(" · ");

      left.appendChild(q);
      left.appendChild(meta);

      const dlPill = document.createElement("span");
      dlPill.className = "md-dl-pill";
      dlPill.textContent = "Download";

      btn.appendChild(left);
      btn.appendChild(dlPill);

      btn.addEventListener("click", async (e) => {
        e.stopPropagation();
        const ext = v.isHLS ? "ts" : (v.format || "mp4").toLowerCase();
        const filename = sanitizeTitleFilename(ext);
        if (isExtensionEnv) {
          const resp = await chrome.runtime.sendMessage({
            type: "download",
            url: v.url,
            filename,
            referrer: window.location.href
          });
          if (resp && resp.success) {
            dlPill.textContent = "Sent!";
            dlPill.classList.add("sent");
          } else {
            dlPill.textContent = "Offline";
          }
        } else {
          dlPill.textContent = "Sent!";
          dlPill.classList.add("sent");
        }
      });

      listEl.appendChild(btn);
    }

    dropdownEl.classList.remove("hidden");
  }

  function positionOverlay() {
    if (!overlayEl || !activeVideo || !document.contains(activeVideo) || dismissedVideos.has(activeVideo)) {
      if (overlayEl) overlayEl.classList.add("hidden");
      return;
    }

    const videos = getCombinedVideos();
    if (videos.length === 0) {
      overlayEl.classList.add("hidden");
      return;
    }

    const rect = activeVideo.getBoundingClientRect();
    if (rect.width < 160 || rect.height < 90 || rect.bottom < 0 || rect.top > window.innerHeight) {
      overlayEl.classList.add("hidden");
      return;
    }

    countBadge.textContent = String(videos.length);
    const top = Math.max(8, rect.top + 10);
    const right = Math.max(8, window.innerWidth - rect.right + 10);
    overlayEl.style.top = `${top}px`;
    overlayEl.style.right = `${right}px`;
    overlayEl.classList.remove("hidden");
    renderDropdown();
  }

  function findPrimaryVideo() {
    const all = Array.from(document.querySelectorAll("video"));
    let best = null;
    let bestArea = 0;
    for (const v of all) {
      if (dismissedVideos.has(v)) continue;
      const r = v.getBoundingClientRect();
      const area = r.width * r.height;
      if (r.width >= 160 && r.height >= 90 && area > bestArea) {
        best = v;
        bestArea = area;
      }
    }
    return best;
  }

  function refresh() {
    ensureShadowUI();
    if (!activeVideo || !document.contains(activeVideo) || dismissedVideos.has(activeVideo)) {
      activeVideo = findPrimaryVideo();
    }
    positionOverlay();
  }

  // Hovering a video pins the floating tag to that player
  document.addEventListener(
    "mouseover",
    (e) => {
      const target = e.target && e.target.closest ? e.target.closest("video") : null;
      if (target && !dismissedVideos.has(target)) {
        activeVideo = target;
        refresh();
      }
    },
    true
  );

  window.addEventListener("scroll", positionOverlay, { passive: true });
  window.addEventListener("resize", positionOverlay, { passive: true });

  const observer = new MutationObserver(() => refresh());
  observer.observe(document.documentElement, { childList: true, subtree: true });

  if (isExtensionEnv) {
    chrome.runtime.onMessage.addListener((msg) => {
      if (msg && msg.type === "videosUpdated" && Array.isArray(msg.videos)) {
        networkVideos = msg.videos;
        refresh();
      }
    });
    chrome.runtime.sendMessage({ type: "getDetectedVideos" }, (resp) => {
      if (chrome.runtime.lastError) return;
      if (resp && Array.isArray(resp.videos)) {
        networkVideos = resp.videos;
        refresh();
      }
    });
  } else if (Array.isArray(window.__MAGICDOWN_PREVIEW_VIDEOS)) {
    // Local preview hook for visual screenshot verification
    networkVideos = window.__MAGICDOWN_PREVIEW_VIDEOS;
    refresh();
    if (window.__MAGICDOWN_PREVIEW_OPEN) {
      dropdownOpen = true;
      renderDropdown();
    }
  }

  refresh();
})();

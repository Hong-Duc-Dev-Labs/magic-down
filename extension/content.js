(() => {
  if (window.__magicdownContentLoaded) return;
  window.__magicdownContentLoaded = true;

  const isExtensionEnv = typeof chrome !== "undefined" && Boolean(chrome.runtime && chrome.runtime.id);
  const isTopFrame = window.top === window;

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
  let ddSubEl = null;

  // Media discovered by the MAIN-world sniffer (inject-main.js) in this frame.
  // Everything here is per-frame: a manifest seen in an iframe must not be
  // offered for a video in the top document.
  const SNIFF_MSG = "magicdown:media-sniff:v1";
  const YT_MSG = "magicdown:yt-player:v1";
  const YT_PROBE_MSG = "magicdown:yt-probe:v1";
  const YT = typeof window !== "undefined" && window.MagicDownYouTube ? window.MagicDownYouTube : null;
  const isYouTubePage = Boolean(YT && YT.isYouTubeHost(location.hostname));
  const ytPlayers = new Map(); // videoId -> parsed YouTube player entry
  let latestYtEntry = null;
  let lastHref = location.href;
  const sniffed = new Map(); // url -> { kind, mime, at }
  const weiboGroups = new Map(); // groupId -> { id, at, variants }
  const weiboUrlToGroup = new Map(); // dedupKey -> groupId
  const manifestCache = new Map(); // manifest url -> { at, ok, kind, variants, reason }
  const pendingManifests = new Set();
  const videoTiming = new WeakMap(); // video -> { at }
  const hookedVideos = new WeakSet();
  const videoIds = new WeakMap();
  let nextVideoId = 0;
  let sniffDrm = false;
  let sniffMime = "";
  let hoveredVideo = null;
  let positionFrame = 0;
  let lastRenderSignature = "";
  const MAX_MANIFEST_CACHE = 16;
  const MAX_PLAN_PARTS = 4000;

  function manifestKindFor(url, contentType) {
    let path = "";
    try {
      path = new URL(url, location.href).pathname.toLowerCase();
    } catch {
      path = String(url).split("?")[0].toLowerCase();
    }
    if (path.endsWith(".mpd")) return "dash";
    if (path.endsWith(".m3u8")) return "hls";
    const ct = String(contentType || "").toLowerCase();
    if (ct.includes("dash+xml")) return "dash";
    if (ct.includes("mpegurl")) return "hls";
    return "";
  }

  function isDirectMediaURL(url) {
    const path = String(url).split("?")[0].split("#")[0].toLowerCase();
    return /\.(mp4|webm|mkv|mov|m4v)$/.test(path);
  }

  function ingestSniff(item) {
    if (!item || typeof item !== "object") return;
    const kind = typeof item.k === "string" ? item.k : "";
    const mime = typeof item.m === "string" ? item.m.slice(0, 120) : "";
    const url = typeof item.u === "string" ? item.u.slice(0, 4096) : "";
    const at = Number.isFinite(item.t) ? item.t : Date.now();

    if (kind === "mime") {
      if (mime) sniffMime = mime;
      return;
    }
    if (kind === "drm") {
      sniffDrm = true;
      return;
    }
    if (kind === "weibo-group" && Array.isArray(item.v) && item.v.length > 0) {
      const id = String(item.id || item.v[0].url || at);
      const variants = item.v
        .filter((v) => v && typeof v.url === "string" && /^https?:/i.test(v.url))
        .map((v) => ({
          url: v.url,
          quality: v.quality || inferQualityFromUrl(v.url).quality,
          height: Number(v.height) || inferQualityFromUrl(v.url).height || 0,
          size: Number(v.size) || 0,
          bandwidth: Number(v.bandwidth) || 0,
          format: v.format || "MP4",
          isHLS: false,
          source: "weibo"
        }))
        .sort((a, b) => (b.height || 0) - (a.height || 0) || (b.bandwidth || 0) - (a.bandwidth || 0) || (b.size || 0) - (a.size || 0));
      if (variants.length > 0) {
        weiboGroups.set(id, { id, at, variants });
        for (const v of variants) {
          weiboUrlToGroup.set(mediaDedupKey(v.url), id);
        }
        while (weiboGroups.size > 60) {
          const oldestId = weiboGroups.keys().next().value;
          const oldest = weiboGroups.get(oldestId);
          weiboGroups.delete(oldestId);
          if (oldest) {
            for (const v of oldest.variants) {
              if (weiboUrlToGroup.get(mediaDedupKey(v.url)) === oldestId) {
                weiboUrlToGroup.delete(mediaDedupKey(v.url));
              }
            }
          }
        }
        scheduleRefresh();
      }
      return;
    }
    if (!url) return;
    if (!/^https?:/i.test(url) && !url.startsWith("blob:")) return;

    sniffed.set(url, { kind, mime, at });
    if (sniffed.size > 300) {
      // Drop the oldest entry; the ring is ordered by insertion on purpose.
      sniffed.delete(sniffed.keys().next().value);
    }

    const mediaKind = manifestKindFor(url, mime);
    if (mediaKind) {
      resolveManifest(url, mediaKind);
      scheduleRefresh();
    }
  }

  // Main-world messages are only trusted when this window sent them; a
  // cross-origin frame cannot forge event.source.
  window.addEventListener(
    "message",
    (event) => {
      if (event.source !== window) return;
      const data = event.data;
      if (!data) return;
      if (data.__mdSniff === YT_MSG && data.payload && YT) {
        const parsed = YT.parseYouTubePlayerPayload(data.payload);
        if (parsed) {
          if (parsed.videoId) {
            ytPlayers.set(parsed.videoId, parsed);
            if (ytPlayers.size > 16) ytPlayers.delete(ytPlayers.keys().next().value);
          }
          latestYtEntry = parsed;
          if (parsed.hlsManifestUrl) {
            resolveManifest(parsed.hlsManifestUrl, "hls");
          }
          scheduleRefresh();
        }
        return;
      }
      if (data.__mdSniff !== SNIFF_MSG || !Array.isArray(data.items)) return;
      for (const item of data.items) ingestSniff(item);
      if (data.replay) scheduleRefresh();
    },
    false
  );

  // The MAIN-world sniffer may have seen the manifest before this script ran;
  // ask it to replay its bounded ring buffer now that the listener exists.
  try {
    window.postMessage({ __mdSniff: "magicdown:media-sniff-ready:v1" }, "*");
  } catch {
    // ignore
  }

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

  function sanitizeTitleFilename(ext, preferredTitle) {
    const raw = (preferredTitle || document.title || "video")
      .replace(/\s*-\s*YouTube\s*$/i, "")
      .replace(/[<>:"/\\|?*\x00-\x1f]/g, " ")
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 80);
    const base = raw || "video";
    return `${base}.${ext}`;
  }

  function currentYouTubeEntry() {
    if (!isYouTubePage || !YT) return null;
    const videoId = YT.extractYouTubeVideoId(location.href);
    if (videoId && ytPlayers.has(videoId)) return ytPlayers.get(videoId);
    if (!videoId) return latestYtEntry;
    return latestYtEntry && latestYtEntry.videoId === videoId ? latestYtEntry : null;
  }

  function handleNavigationChange() {
    if (location.href === lastHref) return;
    lastHref = location.href;
    sniffDrm = false;
    dropdownOpen = false;
    lastRenderSignature = "";
    if (isExtensionEnv && isTopFrame) {
      networkVideos = [];
      try {
        chrome.runtime.sendMessage({ type: "clearTabVideos" }, () => {
          void chrome.runtime.lastError;
        });
      } catch {
        // ignore
      }
    }
    if (isYouTubePage) {
      try {
        window.postMessage({ __mdSniff: YT_PROBE_MSG }, "*");
      } catch {
        // ignore
      }
    }
    scheduleRefresh();
  }

  // Walk the document plus open shadow roots: players are often mounted inside
  // web components, where document.querySelectorAll("video") cannot see them.
  // Discovered roots are cached and only rediscovered on a short interval so a
  // busy feed does not pay a full "*" traversal every refresh.
  const shadowRoots = new Set();
  let lastShadowScan = 0;

  function discoverShadowRoots(scope) {
    const stack = [scope];
    let guard = 0;
    while (stack.length > 0 && guard < 4000) {
      guard++;
      const node = stack.pop();
      for (const el of node.querySelectorAll("*")) {
        if (!el.shadowRoot) continue;
        if (!shadowRoots.has(el.shadowRoot)) shadowRoots.add(el.shadowRoot);
        stack.push(el.shadowRoot);
      }
    }
  }

  function collectVideoElements(root) {
    const now = performance.now();
    if (now - lastShadowScan > 400) {
      lastShadowScan = now;
      discoverShadowRoots(root || document);
    }
    const found = [];
    const seen = new Set();
    const scopes = [root || document];
    for (const shadowRoot of shadowRoots) scopes.push(shadowRoot);
    for (const scope of scopes) {
      for (const video of scope.querySelectorAll("video")) {
        if (!video.isConnected || seen.has(video)) continue;
        seen.add(video);
        found.push(video);
      }
    }
    return found;
  }

  function inferQualityFromUrl(rawUrl) {
    try {
      const u = new URL(rawUrl, location.href);
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
      const inferred = inferQualityFromUrl(src);
      const domH = videoEl && videoEl.videoHeight ? videoEl.videoHeight : 0;
      const h = inferred.height || domH;
      found.push({
        url: src,
        quality: inferred.height > 0 ? inferred.quality : (domH > 0 ? `${domH}p` : "Original"),
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

  // --- Manifest resolution (DASH/MPD and HLS) ---------------------------------
  //
  // The page's own player fetches the manifest; the sniffer only sees the URL.
  // Fetching it needs the page's cookies, so the extension background worker
  // does the request (it holds host permissions and cookie access) and the
  // parsing happens here, where DOMParser exists.

  function intAttr(el, name, fallback) {
    const raw = el.getAttribute(name);
    if (raw === null) return fallback;
    const num = Number.parseInt(raw, 10);
    return Number.isFinite(num) ? num : fallback;
  }

  function kids(el, name) {
    if (!el) return [];
    const out = [];
    for (const child of el.children) {
      if (child.localName === name) out.push(child);
    }
    return out;
  }

  function firstKid(el, name) {
    return kids(el, name)[0] || null;
  }

  function dashBase(el, inherited) {
    const baseEl = firstKid(el, "BaseURL");
    if (!baseEl) return inherited;
    const text = (baseEl.textContent || "").trim();
    if (!text) return inherited;
    try {
      return new URL(text, inherited).toString();
    } catch {
      return inherited;
    }
  }

  function resolveDashURL(ref, base) {
    try {
      return new URL(ref, base).toString();
    } catch {
      return ref;
    }
  }

  // DASH byte ranges use "start-end" (no "bytes=" prefix).
  function parseDashRange(value) {
    if (!value) return null;
    const spec = String(value).trim().replace(/^bytes=/i, "");
    const dash = spec.indexOf("-");
    if (dash < 0) return null;
    const start = Number.parseInt(spec.slice(0, dash), 10);
    const end = Number.parseInt(spec.slice(dash + 1), 10);
    if (!Number.isFinite(start) || start < 0) return null;
    if (!Number.isFinite(end) || end < start) return { start, end: 0 };
    return { start, end };
  }

  // ISO-8601 duration ("PT1H2M3.5S") -> seconds.
  function parseISODuration(value) {
    if (!value) return 0;
    const match = /^P(?:(\d+(?:\.\d+)?)Y)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$/i.exec(
      String(value).trim()
    );
    if (!match) return 0;
    const num = (v) => (v ? Number.parseFloat(v) : 0);
    return (
      num(match[1]) * 365 * 24 * 3600 +
      num(match[2]) * 30 * 24 * 3600 +
      num(match[3]) * 24 * 3600 +
      num(match[4]) * 3600 +
      num(match[5]) * 60 +
      num(match[6])
    );
  }

  function expandDASH(template, ctx, segment) {
    return String(template)
      .replace(/\$\$/g, "\u0000")
      .replace(/\$([A-Za-z]+)(?:%0(\d+)d)?\$/g, (whole, name, width) => {
        let value = null;
        switch (name) {
          case "RepresentationID":
            value = ctx.repID || "";
            break;
          case "Bandwidth":
            value = String(ctx.bandwidth || 0);
            break;
          case "Number":
            value = String(segment ? segment.number : 0);
            break;
          case "Time":
            value = String(segment ? segment.time : 0);
            break;
          default:
            return whole;
        }
        if (width && (name === "Number" || name === "Time")) {
          return value.padStart(Number.parseInt(width, 10) || 0, "0");
        }
        return value;
      })
      .replace(/\u0000/g, "$");
  }

  function dashFormat(mime) {
    return String(mime || "").toLowerCase().includes("webm") ? "webm" : "mp4";
  }

  function isVideoRepresentation(rep, mime, contentType, codecs) {
    const m = String(mime || "").toLowerCase();
    const c = String(contentType || "").toLowerCase();
    const cd = String(codecs || "").toLowerCase();
    if (c === "text" || m.startsWith("text/")) return false;
    if (cd.startsWith("wvtt") || cd.startsWith("stpp") || cd.startsWith("tx3g")) return false;
    if (c === "audio" || m.startsWith("audio/")) return false;
    if (c === "video" || m.startsWith("video/")) return true;
    return rep.hasAttribute("height") || rep.hasAttribute("width");
  }

  function buildTemplatePlan(tmpl, base, ctx) {
    const mediaTemplate = tmpl.getAttribute("media");
    if (!mediaTemplate) return null;
    const initTemplate = tmpl.getAttribute("initialization");
    const startNumber = intAttr(tmpl, "startNumber", 1);
    const timescale = intAttr(tmpl, "timescale", 1) || 1;
    const segDuration = intAttr(tmpl, "duration", 0);

    const segments = [];
    const timeline = firstKid(tmpl, "SegmentTimeline");
    if (timeline) {
      let time = 0;
      let number = startNumber;
      outer: for (const s of kids(timeline, "S")) {
        if (s.hasAttribute("t")) {
          const raw = Number(s.getAttribute("t"));
          if (Number.isFinite(raw) && raw >= 0) time = raw;
        }
        const duration = Number(s.getAttribute("d"));
        if (!Number.isFinite(duration) || duration <= 0) continue;
        let repeat = Number(s.getAttribute("r"));
        if (!Number.isFinite(repeat) || repeat < 0) repeat = 0;
        for (let i = 0; i <= repeat; i++) {
          segments.push({ number, time });
          number++;
          time += duration;
          if (segments.length >= MAX_PLAN_PARTS) break outer;
        }
      }
    } else if (segDuration > 0 && ctx.duration > 0) {
      const count = Math.min(MAX_PLAN_PARTS, Math.ceil((ctx.duration * timescale) / segDuration));
      for (let i = 0; i < count; i++) {
        segments.push({ number: startNumber + i, time: i * segDuration });
      }
    }

    const parts = [];
    if (initTemplate) {
      parts.push({ url: resolveDashURL(expandDASH(initTemplate, ctx, null), base) });
    }
    for (const segment of segments) {
      parts.push({ url: resolveDashURL(expandDASH(mediaTemplate, ctx, segment), base) });
    }
    if (parts.length === 0) return null;
    return { kind: "segments", url: base, format: dashFormat(ctx.mime), parts };
  }

  function buildSegmentListPlan(list, base, mime) {
    const parts = [];
    const initEl = firstKid(list, "Initialization");
    if (initEl) {
      const source = initEl.getAttribute("sourceURL");
      const range = parseDashRange(initEl.getAttribute("range"));
      const part = { url: source ? resolveDashURL(source, base) : base };
      if (range) {
        part.start = range.start;
        if (range.end > 0) part.end = range.end;
      }
      parts.push(part);
    }
    for (const segEl of kids(list, "SegmentURL")) {
      const media = segEl.getAttribute("media");
      const range = parseDashRange(segEl.getAttribute("mediaRange"));
      const part = { url: media ? resolveDashURL(media, base) : base };
      if (range) {
        part.start = range.start;
        if (range.end > 0) part.end = range.end;
      }
      parts.push(part);
      if (parts.length >= MAX_PLAN_PARTS) break;
    }
    if (parts.length === 0) return null;
    return { kind: "segments", url: base, format: dashFormat(mime), parts };
  }

  function buildDASHPlan(ctx) {
    const { rep, adaptation, period, base, mime } = ctx;

    const segmentBase =
      firstKid(rep, "SegmentBase") || firstKid(adaptation, "SegmentBase") || firstKid(period, "SegmentBase");
    if (segmentBase) {
      const indexRange = parseDashRange(segmentBase.getAttribute("indexRange"));
      const initEl = firstKid(segmentBase, "Initialization");
      const initRange = parseDashRange(initEl && initEl.getAttribute("range"));
      let headerEnd = -1;
      if (indexRange) headerEnd = Math.max(headerEnd, indexRange.end);
      if (initRange) headerEnd = Math.max(headerEnd, initRange.end);
      // One URL, byte ranges only: header (ftyp/moov/sidx) plus the media tail.
      const parts =
        headerEnd >= 0
          ? [{ start: 0, end: headerEnd }, { start: headerEnd + 1 }]
          : [{ start: 0 }];
      return { kind: "range", url: base, format: dashFormat(mime), parts };
    }

    const template =
      firstKid(rep, "SegmentTemplate") ||
      firstKid(adaptation, "SegmentTemplate") ||
      firstKid(period, "SegmentTemplate");
    if (template) {
      const plan = buildTemplatePlan(template, base, ctx);
      if (plan) return plan;
    }

    const list =
      firstKid(rep, "SegmentList") || firstKid(adaptation, "SegmentList") || firstKid(period, "SegmentList");
    if (list) {
      const plan = buildSegmentListPlan(list, base, mime);
      if (plan) return plan;
    }

    // Progressive/on-demand representation: the BaseURL is the whole file.
    return { kind: "range", url: base, format: dashFormat(mime), parts: [{ start: 0 }] };
  }

  function parseDASHManifest(manifestURL, text) {
    let doc = null;
    try {
      doc = new DOMParser().parseFromString(text, "application/xml");
    } catch {
      return { ok: false, reason: "MPD could not be parsed" };
    }
    if (!doc || doc.getElementsByTagName("parsererror").length > 0) {
      return { ok: false, reason: "MPD is not valid XML" };
    }
    const mpd = doc.documentElement;
    if (!mpd || mpd.localName !== "MPD") {
      return { ok: false, reason: "response is not an MPD" };
    }

    const mpdBase = dashBase(mpd, manifestURL);
    const period = kids(mpd, "Period")[0];
    if (!period) return { ok: false, reason: "MPD has no Period" };
    const periodBase = dashBase(period, mpdBase);
    const duration =
      parseISODuration(period.getAttribute("duration")) ||
      parseISODuration(mpd.getAttribute("mediaPresentationDuration")) ||
      0;

    const variants = [];
    for (const adaptation of kids(period, "AdaptationSet")) {
      const adaptationBase = dashBase(adaptation, periodBase);
      const adaptationMime = adaptation.getAttribute("mimeType") || "";
      const adaptationContent = adaptation.getAttribute("contentType") || "";
      for (const rep of kids(adaptation, "Representation")) {
        const mime = rep.getAttribute("mimeType") || adaptationMime;
        const contentType = rep.getAttribute("contentType") || adaptationContent;
        const codecs = rep.getAttribute("codecs") || adaptation.getAttribute("codecs") || "";
        if (!isVideoRepresentation(rep, mime, contentType, codecs)) continue;

        const base = dashBase(rep, adaptationBase);
        const ctx = {
          rep,
          adaptation,
          period,
          base,
          mime,
          duration,
          repID: rep.getAttribute("id") || "",
          bandwidth: intAttr(rep, "bandwidth", 0)
        };
        const plan = buildDASHPlan(ctx);
        if (!plan) continue;

        const height = intAttr(rep, "height", intAttr(adaptation, "height", 0));
        const width = intAttr(rep, "width", intAttr(adaptation, "width", 0));
        const bandwidth = ctx.bandwidth;
        variants.push({
          url: plan.url,
          quality: height > 0 ? `${height}p` : bandwidth > 0 ? `${(bandwidth / 1000000).toFixed(1)} Mbps` : "DASH",
          height,
          width,
          bandwidth,
          format: dashFormat(mime) === "webm" ? "WebM" : "MP4",
          isHLS: false,
          source: "dash",
          plan
        });
        if (variants.length >= 40) break;
      }
    }

    if (variants.length === 0) {
      return { ok: false, reason: "MPD lists no video representation MagicDown can download" };
    }
    variants.sort((a, b) => (b.height || 0) - (a.height || 0) || (b.bandwidth || 0) - (a.bandwidth || 0));
    return { ok: true, kind: "dash", variants };
  }

  function parseResolutionHeight(resolution) {
    const parts = String(resolution || "").toLowerCase().split("x");
    if (parts.length !== 2) return 0;
    const h = Number.parseInt(parts[1], 10);
    return Number.isFinite(h) ? h : 0;
  }

  // HLS master variant parsing for manifests discovered by the sniffer. The
  // desktop engine performs the actual playlist download.
  function parseHLSManifest(manifestURL, text) {
    const variants = [];
    let pending = null;
    for (const rawLine of String(text).split(/\r?\n/)) {
      const line = rawLine.trim();
      if (!line) continue;
      if (line.startsWith("#EXT-X-STREAM-INF:")) {
        const attrs = line.slice("#EXT-X-STREAM-INF:".length);
        const bandwidth = /BANDWIDTH=(\d+)/i.exec(attrs);
        const resolution = /RESOLUTION=(\d+x\d+)/i.exec(attrs);
        const height = parseResolutionHeight(resolution ? resolution[1] : "");
        pending = {
          height,
          bandwidth: bandwidth ? Number.parseInt(bandwidth[1], 10) : 0
        };
        continue;
      }
      if (line.startsWith("#") || !pending) continue;
      try {
        variants.push({
          url: new URL(line, manifestURL).toString(),
          quality: pending.height > 0 ? `${pending.height}p` : pending.bandwidth > 0 ? `${(pending.bandwidth / 1000000).toFixed(1)} Mbps` : "HLS",
          height: pending.height,
          bandwidth: pending.bandwidth,
          format: "HLS",
          isHLS: true,
          source: "hls"
        });
      } catch {
        // ignore malformed variant URI
      }
      pending = null;
    }
    if (variants.length > 0) {
      variants.sort((a, b) => (b.height || 0) - (a.height || 0) || (b.bandwidth || 0) - (a.bandwidth || 0));
      return { ok: true, kind: "hls", variants };
    }
    if (String(text).includes("#EXTINF:")) {
      return {
        ok: true,
        kind: "hls",
        variants: [{ url: manifestURL, quality: "Original", format: "HLS", isHLS: true, source: "hls" }]
      };
    }
    return { ok: false, reason: "playlist has no variants or segments" };
  }

  async function fetchManifestText(url) {
    // Preferred: service worker fetch (page cookies, no CORS wall).
    if (isExtensionEnv) {
      try {
        const resp = await chrome.runtime.sendMessage({ type: "fetchManifestText", url });
        if (resp && resp.ok && typeof resp.text === "string" && resp.text) {
          return { text: resp.text, contentType: resp.contentType || "" };
        }
      } catch {
        // fall through to page fetch
      }
    }
    try {
      const resp = await fetch(url, { credentials: "include" });
      if (!resp.ok) return { text: "", contentType: "" };
      return { text: await resp.text(), contentType: resp.headers.get("content-type") || "" };
    } catch {
      return { text: "", contentType: "" };
    }
  }

  function videoKey(video) {
    if (!video) return "none";
    let id = videoIds.get(video);
    if (!id) {
      id = ++nextVideoId;
      videoIds.set(video, id);
    }
    return String(id);
  }

  async function resolveManifest(url, hintedKind) {
    if (manifestCache.has(url) || pendingManifests.has(url)) return;
    pendingManifests.add(url);
    try {
      const fetched = await fetchManifestText(url);
      let entry;
      if (!fetched.text) {
        entry = { ok: false, reason: "manifest could not be fetched with page credentials" };
      } else {
        const kind = manifestKindFor(url, fetched.contentType) || hintedKind;
        entry =
          kind === "dash"
            ? parseDASHManifest(url, fetched.text)
            : kind === "hls"
              ? parseHLSManifest(url, fetched.text)
              : { ok: false, reason: "unrecognised manifest type" };
      }
      entry.at = Date.now();
      entry.url = url;
      manifestCache.set(url, entry);
      while (manifestCache.size > MAX_MANIFEST_CACHE) {
        manifestCache.delete(manifestCache.keys().next().value);
      }
    } catch (err) {
      manifestCache.set(url, {
        ok: false,
        reason: `manifest parsing failed: ${String((err && err.message) || err)}`,
        at: Date.now(),
        url
      });
    } finally {
      pendingManifests.delete(url);
      scheduleRefresh();
    }
  }

  function resolvedManifests() {
    const list = [];
    for (const entry of manifestCache.values()) {
      if (entry && entry.ok) list.push(entry);
    }
    list.sort((a, b) => (b.at || 0) - (a.at || 0));
    return list;
  }

  // Video-scoped manifests: prefer ones discovered around the moment this
  // player started loading, so a feed with several videos offers the manifest
  // that belongs to the hovered/centred player rather than the newest tab-wide.
  function manifestsForVideo(video) {
    const all = resolvedManifests();
    if (all.length <= 1) return all;
    const timing = video ? videoTiming.get(video) : null;
    if (!timing || !timing.at) return all.slice(0, 3);
    const near = all.filter((entry) => entry.at >= timing.at - 3000 && entry.at <= timing.at + 60000);
    return (near.length > 0 ? near : all).slice(0, 3);
  }

  function mediaDedupKey(rawUrl) {
    try {
      const u = new URL(rawUrl);
      const host = u.hostname.toLowerCase();
      if ((host === "fbcdn.net" || host.endsWith(".fbcdn.net")) && u.pathname && u.pathname !== "/") {
        return `fbcdn.net${u.pathname}`;
      }
      const ephemeral = new Set(["bytestart", "byteend", "byte_start", "byte_end", "range", "bytes", "rn", "rbuf", "sq", "rid", "_nc_rid", "cb", "cachebuster", "_"]);
      const kept = [];
      for (const [k, v] of u.searchParams.entries()) {
        if (ephemeral.has(k.toLowerCase())) continue;
        kept.push([k, v]);
      }
      kept.sort((a, b) => (a[0] === b[0] ? (a[1] < b[1] ? -1 : a[1] > b[1] ? 1 : 0) : a[0] < b[0] ? -1 : 1));
      const search = kept.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`).join("&");
      return `${u.origin}${u.pathname}${search ? `?${search}` : ""}`;
    } catch {
      return String(rawUrl || "");
    }
  }

  function sniffedDirectVideos() {
    const found = [];
    for (const [url, meta] of sniffed) {
      if (!isDirectMediaURL(url)) continue;
      if (/[?&](?:bytestart|byteend|byte_start|byte_end|range|bytes)=/i.test(url)) continue;
      const format = url.toLowerCase().endsWith(".webm") ? "WebM" : "MP4";
      found.push({ url, quality: "Original", format, isHLS: false, source: meta.kind || "sniff", at: meta.at });
    }
    found.sort((a, b) => (b.at || 0) - (a.at || 0));
    return found.slice(0, 3);
  }

  function getCombinedVideos() {
    const map = new Map();
    const push = (key, item) => {
      if (!item || !item.url || map.has(key)) return;
      map.set(key, item);
    };

    const ytEntry = currentYouTubeEntry();
    if (ytEntry && ytEntry.variants.length > 0) {
      for (const variant of ytEntry.variants) {
        const key = `${variant.url}|${variant.quality}|${variant.streamType || ""}`;
        push(key, variant);
      }
    }

    const planURLs = new Set();
    let manifestCount = ytEntry && ytEntry.variants.length > 0 ? ytEntry.variants.length : 0;
    const manifestList = manifestsForVideo(activeVideo);
    const variantURLs = new Set();
    for (const manifest of manifestList) {
      for (const variant of manifest.variants) {
        if (variant.url !== manifest.url) variantURLs.add(variant.url);
      }
    }
    for (const manifest of manifestList) {
      // A media playlist that is also a variant of a resolved master is already
      // represented by that master's quality list; listing it as "Original"
      // would offer the same stream twice.
      if (
        manifest.variants.length === 1 &&
        manifest.variants[0].quality === "Original" &&
        variantURLs.has(manifest.url)
      ) {
        continue;
      }
      for (const variant of manifest.variants) {
        const key = `${variant.url}|${variant.quality}|${variant.height || 0}|${variant.bandwidth || 0}|${variant.plan ? "plan" : ""}`;
        push(key, { ...variant, manifestURL: manifest.url });
        manifestCount++;
        if (variant.plan) {
          for (const part of variant.plan.parts) {
            if (part.url) planURLs.add(part.url);
          }
        }
      }
    }

    // A resolved manifest is authoritative for this player. Raw webRequest /
    // sniffer candidates would only duplicate its byte-range or segment probes
    // (e.g. the same .mp4 seen mid-range-request) as bogus "Original" entries.
    const directCandidates = collectDOMVideos(activeVideo);
    const networkByKey = new Map();
    for (const item of networkVideos) {
      if (item && item.url) networkByKey.set(mediaDedupKey(item.url), item);
    }

    if (manifestCount === 0 && directCandidates.length > 0) {
      // The active <video> has a direct HTTP(S) src: scope strictly to this
      // video (and its Weibo multi-resolution group if known) so earlier feed
      // videos never stack into this player's menu.
      let matchedWeiboGroup = null;
      for (const item of directCandidates) {
        const gid = weiboUrlToGroup.get(mediaDedupKey(item.url));
        if (gid && weiboGroups.has(gid)) {
          matchedWeiboGroup = weiboGroups.get(gid);
          break;
        }
      }
      if (matchedWeiboGroup) {
        for (const variant of matchedWeiboGroup.variants) {
          push(mediaDedupKey(variant.url), variant);
        }
      }
      for (const item of directCandidates) {
        if (planURLs.has(item.url)) continue;
        const key = mediaDedupKey(item.url);
        const net = networkByKey.get(key);
        const enriched = net
          ? {
              ...item,
              size: item.size || net.size || 0,
              height: item.height || net.height || 0,
              quality: item.quality !== "Original" ? item.quality : (net.quality || item.quality)
            }
          : item;
        push(key, enriched);
      }
      return Array.from(map.values());
    }

    for (const item of directCandidates) {
      if (planURLs.has(item.url)) continue;
      push(mediaDedupKey(item.url), item);
    }

    if (manifestCount === 0) {
      // MSE/blob player with no direct src and no manifest: scope network
      // detections to the active video's load window so feed videos do not
      // accumulate across posts.
      const timing = activeVideo ? videoTiming.get(activeVideo) : null;
      let scopedNetwork = networkVideos;
      if (timing && timing.at && networkVideos.length > 1) {
        const near = networkVideos.filter(
          (item) => item && item.detectedAt >= timing.at - 4000 && item.detectedAt <= timing.at + 60000
        );
        if (near.length > 0) scopedNetwork = near;
      }
      for (const item of scopedNetwork) {
        const key = mediaDedupKey(item.url);
        const gid = weiboUrlToGroup.get(key);
        if (gid && weiboGroups.has(gid)) {
          for (const variant of weiboGroups.get(gid).variants) {
            push(mediaDedupKey(variant.url), variant);
          }
        }
        push(key, item);
      }
      for (const item of sniffedDirectVideos()) push(mediaDedupKey(item.url), item);
    }
    return Array.from(map.values());
  }

  function unavailableReason(video) {
    if (!video) return "No <video> element found in this frame. If the player is an iframe, scroll it into view to wake the frame.";
    if (isYouTubePage) {
      const ytEntry = currentYouTubeEntry();
      if (ytEntry) {
        if (ytEntry.drmDetected) {
          return "DRM-protected YouTube stream. MagicDown cannot download encrypted media.";
        }
        if (ytEntry.variants.length === 0 && ytEntry.cipherOnlyCount > 0) {
          return "YouTube streams use signatureCipher (encrypted URLs) which MagicDown cannot decipher yet.";
        }
      }
    }
    const src = video.currentSrc || video.src || "";
    const isMse = src.startsWith("blob:") || Boolean(video.srcObject);
    if (sniffDrm || video.mediaKeys) {
      return "DRM-protected (EME) stream. MagicDown cannot download encrypted media.";
    }
    if (!isMse) {
      return "No downloadable media URL found for this player yet. Press play, then reopen this menu.";
    }
    if (video.srcObject && !src.startsWith("blob:")) {
      return "srcObject/captureStream player (no file URL exists). Nothing to hand to the desktop engine.";
    }
    if (pendingManifests.size > 0) {
      return "Manifest found and still resolving. Reopen this menu in a moment.";
    }
    if (video.readyState === 0) {
      return "MSE player has not buffered media yet and no MPD/M3U8 has been seen. Press play, then reopen this menu.";
    }
    const mimeNote = sniffMime ? ` (MSE mime: ${sniffMime})` : "";
    return `MSE/blob stream with no MPD or M3U8 exposed by this page${mimeNote}. MagicDown has no file URL to hand to the desktop engine.`;
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
      .md-empty {
        font-size: 11.5px;
        line-height: 1.45;
        color: #94A3B8;
        background: #141923;
        border: 1px solid #212838;
        border-radius: 11px;
        padding: 9px 10px;
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
      if (dropdownOpen && isYouTubePage && !currentYouTubeEntry()) {
        try {
          window.postMessage({ __mdSniff: YT_PROBE_MSG }, "*");
        } catch {
          // ignore
        }
      }
      renderDropdown();
    });

    pillRow.appendChild(pillBtn);
    pillRow.appendChild(dismissBtn);

    dropdownEl = document.createElement("div");
    dropdownEl.className = "md-dropdown hidden";

    const ddHeader = document.createElement("div");
    ddHeader.className = "md-dd-header";
    ddHeader.innerHTML = `<span class="md-dd-title">SELECT QUALITY & FORMAT</span><span class="md-dd-sub">1-click to MagicDown</span>`;
    ddSubEl = ddHeader.querySelector(".md-dd-sub");

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
    const reason = videos.length === 0 ? unavailableReason(activeVideo) : "";
    const signature = `${videoKey(activeVideo)}::${reason}::${videos
      .map((v) => `${v.url}|${v.quality}|${v.format}|${v.plan ? "p" : ""}`)
      .join(";")}`;

    if (signature === lastRenderSignature) {
      dropdownEl.classList.remove("hidden");
      return;
    }
    lastRenderSignature = signature;

    if (ddSubEl) {
      const ytCount = videos.filter((v) => v.source === "youtube").length;
      const dashCount = videos.filter((v) => v.source === "dash").length;
      if (ytCount > 0) ddSubEl.textContent = `YouTube · ${ytCount} ${ytCount === 1 ? "stream" : "streams"}`;
      else if (dashCount > 0) ddSubEl.textContent = `DASH · ${dashCount} ${dashCount === 1 ? "quality" : "qualities"}`;
      else if (videos.some((v) => v.isHLS)) ddSubEl.textContent = "HLS · quality list";
      else if (videos.length > 0) ddSubEl.textContent = "1-click to MagicDown";
      else ddSubEl.textContent = sniffDrm || (activeVideo && activeVideo.mediaKeys) ? "DRM protected" : "Unresolved";
    }

    listEl.textContent = "";

    if (videos.length === 0) {
      const empty = document.createElement("div");
      empty.className = "md-empty";
      empty.textContent = reason;
      listEl.appendChild(empty);
    }

    for (const v of videos) {
      const btn = document.createElement("button");
      btn.className = "md-item";
      btn.type = "button";

      const left = document.createElement("div");
      left.className = "md-item-left";

      const q = document.createElement("span");
      q.className = "md-q-badge";
      q.textContent = v.quality || "Original";

      const metaParts = [];
      if (v.source === "dash") metaParts.push("DASH");
      else if (v.source === "hls" || v.isHLS) metaParts.push("HLS");
      metaParts.push(v.format || "MP4");
      if (v.streamType === "progressive") metaParts.push("Video+Audio");
      else if (v.streamType === "video-only") metaParts.push("Video only");
      else if (v.streamType === "audio-only") metaParts.push("Audio only");
      if (v.size > 0) metaParts.push(formatSize(v.size));
      else if (v.bandwidth > 0) metaParts.push(formatBandwidth(v.bandwidth));
      if (v.plan && v.plan.kind === "range") {
        const parts = v.plan.parts.length;
        metaParts.push(parts > 1 ? `${parts} ranges` : "1 range");
      } else if (v.plan && v.plan.kind === "segments") {
        metaParts.push(`${v.plan.parts.length} segments`);
      }

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
        let ext = v.ext || (v.isHLS ? "ts" : String(v.format || "mp4").toLowerCase());
        if (v.plan && v.plan.format) ext = v.plan.format;
        const filename = sanitizeTitleFilename(ext, v.title);
        if (isExtensionEnv) {
          const payload = {
            type: "download",
            url: v.url,
            filename,
            referrer: window.location.href,
            headers: {
              Origin: window.location.origin,
              "User-Agent": navigator.userAgent
            }
          };
          if (v.plan) payload.plan = v.plan;
          let resp = null;
          try {
            resp = await chrome.runtime.sendMessage(payload);
          } catch {
            resp = null;
          }
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

  function ensureHostParent() {
    if (!hostEl) return;
    const fsEl = document.fullscreenElement;
    // Native <video> fullscreen is drawn on a browser layer that page content
    // cannot overlay; for element fullscreen, move the overlay inside the
    // fullscreen subtree so it is actually painted there.
    const target = fsEl && fsEl.tagName !== "VIDEO" ? fsEl : document.documentElement;
    if (hostEl.parentElement !== target) target.appendChild(hostEl);
  }

  function visibleArea(rect) {
    const x = Math.max(0, Math.min(rect.right, window.innerWidth) - Math.max(rect.left, 0));
    const y = Math.max(0, Math.min(rect.bottom, window.innerHeight) - Math.max(rect.top, 0));
    return x * y;
  }

  // Feed videos can be partially scrolled, hidden by a collapsed container, or
  // removed without a mutation we notice; every positioning decision checks
  // this instead of trusting that a connected <video> is still on screen.
  function isVideoRenderable(video) {
    if (!video || !video.isConnected || dismissedVideos.has(video)) return false;
    const rect = video.getBoundingClientRect();
    if (rect.width < 160 || rect.height < 90) return false;
    if (visibleArea(rect) <= 0) return false;
    try {
      const style = window.getComputedStyle(video);
      if (style.display === "none" || style.visibility === "hidden" || Number.parseFloat(style.opacity) === 0) {
        return false;
      }
    } catch {
      // ignore
    }
    return true;
  }

  function isVideoPlaying(video) {
    return Boolean(video && !video.paused && !video.ended && video.readyState >= 2);
  }

  function positionOverlay() {
    positionFrame = 0;
    if (!overlayEl) return;

    if (!isVideoRenderable(activeVideo)) {
      // A feed scroll can leave the target off-screen. Retarget before hiding,
      // otherwise the tag hangs where the video no longer is.
      const candidate = findPrimaryVideo();
      if (candidate) activeVideo = candidate;
      if (!isVideoRenderable(activeVideo)) {
        overlayEl.classList.add("hidden");
        return;
      }
    }

    const rect = activeVideo.getBoundingClientRect();
    const viewportH = window.innerHeight;

    // Keep the control on players whose stream has no direct URL (blob/MSE):
    // the dropdown then explains why it cannot be downloaded instead of
    // silently leaving the user without any MagicDown affordance.
    const videos = getCombinedVideos();
    countBadge.textContent = String(videos.length);
    countBadge.style.display = videos.length > 0 ? "" : "none";

    // Pin inside the visible part of the active video's box (top-right). Staying
    // inside the box keeps the tag off a neighbouring feed video's controls, and
    // clamping to the visible area keeps tall portrait feed videos anchored.
    const visibleTop = Math.max(0, rect.top);
    const visibleRight = Math.min(window.innerWidth, rect.right);
    const top = Math.min(Math.max(8, visibleTop + 10), Math.max(8, viewportH - 64));
    const right = Math.max(8, window.innerWidth - visibleRight + 10);
    overlayEl.style.top = `${top}px`;
    overlayEl.style.right = `${right}px`;
    overlayEl.style.maxWidth = `${Math.max(200, Math.min(360, rect.width - 20))}px`;
    overlayEl.classList.remove("hidden");
    renderDropdown();
  }

  function schedulePosition() {
    if (positionFrame) return;
    positionFrame = requestAnimationFrame(positionOverlay);
  }

  // Feed pages stack transparent click/control layers over <video>, so a hover
  // event may never reach the element. Hit-test the video boxes by pointer
  // coordinates as a fallback.
  function findVideoAtPoint(clientX, clientY) {
    let best = null;
    let bestArea = Infinity;
    for (const video of collectVideoElements(document)) {
      if (!isVideoRenderable(video)) continue;
      const rect = video.getBoundingClientRect();
      if (clientX < rect.left || clientX > rect.right || clientY < rect.top || clientY > rect.bottom) continue;
      const area = rect.width * rect.height;
      if (area <= bestArea) {
        best = video;
        bestArea = area;
      }
    }
    return best;
  }

  // A feed can auto-play several players; prefer the hovered one, then a
  // playing video nearest the viewport centre, so the tag follows what the user
  // is actually watching rather than the largest box on the page.
  function findPrimaryVideo() {
    const visible = [];
    for (const video of collectVideoElements(document)) {
      if (!isVideoRenderable(video)) continue;
      visible.push({ video, rect: video.getBoundingClientRect() });
    }
    if (visible.length === 0) return null;
    if (hoveredVideo) {
      const hit = visible.find((entry) => entry.video === hoveredVideo);
      if (hit) return hit.video;
    }
    const centerX = window.innerWidth / 2;
    const centerY = window.innerHeight / 2;
    let best = null;
    let bestScore = -Infinity;
    for (const { video, rect } of visible) {
      const visibleCenterX = (Math.max(0, rect.left) + Math.min(window.innerWidth, rect.right)) / 2;
      const visibleCenterY = (Math.max(0, rect.top) + Math.min(window.innerHeight, rect.bottom)) / 2;
      const distance = Math.hypot(visibleCenterX - centerX, visibleCenterY - centerY) * 10;
      const score = (isVideoPlaying(video) ? 1e9 : 0) + visibleArea(rect) - distance;
      if (score > bestScore) {
        bestScore = score;
        best = video;
      }
    }
    return best;
  }

  function activateVideo(video) {
    if (dropdownOpen) return;
    if (!isVideoRenderable(video) || activeVideo === video) return;
    activeVideo = video;
    refresh();
  }

  function hookVideo(video) {
    if (!video || hookedVideos.has(video)) return;
    hookedVideos.add(video);
    const stamp = () => {
      videoTiming.set(video, { at: Date.now() });
      activateVideo(video);
      scheduleRefresh();
    };
    // Autoplay-muted feed videos announce themselves via canplay/playing; media
    // events from a shadow root do not reach document capture listeners, so the
    // element itself must be observed.
    for (const event of ["play", "playing", "canplay", "canplaythrough", "loadedmetadata", "loadeddata", "durationchange"]) {
      video.addEventListener(event, stamp, { passive: true });
    }
    video.addEventListener("resize", schedulePosition, { passive: true });
  }

  function refresh() {
    if (location.href !== lastHref) {
      handleNavigationChange();
    }
    ensureShadowUI();
    ensureHostParent();
    for (const video of collectVideoElements(document)) hookVideo(video);
    if (!isVideoRenderable(activeVideo)) {
      activeVideo = findPrimaryVideo();
    } else if (!dropdownOpen && hoveredVideo && isVideoRenderable(hoveredVideo) && hoveredVideo !== activeVideo) {
      activeVideo = hoveredVideo;
    } else if (!dropdownOpen && !isVideoPlaying(activeVideo)) {
      const candidate = findPrimaryVideo();
      if (candidate && candidate !== activeVideo && isVideoPlaying(candidate)) activeVideo = candidate;
    }
    // Position synchronously: requestAnimationFrame is paused while a window
    // is occluded/minimized, and a page load must not wait for a frame to show
    // the tag. Scroll bursts still go through the rAF coalescer below.
    positionOverlay();
  }

  let refreshTimer = 0;
  function scheduleRefresh() {
    if (refreshTimer) return;
    refreshTimer = setTimeout(() => {
      refreshTimer = 0;
      refresh();
    }, 150);
  }

  // Hovering a video pins the floating tag to that player. A page overlay may
  // swallow the event, so fall back to a coordinate hit-test against video boxes.
  let hoverThrottleTimer = 0;
  function handlePointerHover(e) {
    if (dropdownOpen) return;
    const path = typeof e.composedPath === "function" ? e.composedPath() : [];
    let target =
      path.find((node) => node && node.tagName === "VIDEO") ||
      (e.target && e.target.closest ? e.target.closest("video") : null);
    if (!target && Number.isFinite(e.clientX) && Number.isFinite(e.clientY)) {
      target = findVideoAtPoint(e.clientX, e.clientY);
    }
    if (!target) {
      const overOwnUI = path.some((node) => node === hostEl);
      if (!overOwnUI) hoveredVideo = null;
      return;
    }
    if (dismissedVideos.has(target) || !isVideoRenderable(target)) return;
    hoveredVideo = target;
    if (activeVideo !== target) {
      activeVideo = target;
      refresh();
    } else {
      schedulePosition();
    }
  }

  document.addEventListener("mouseover", handlePointerHover, true);
  document.addEventListener(
    "pointermove",
    (e) => {
      if (hoverThrottleTimer) return;
      hoverThrottleTimer = setTimeout(() => {
        hoverThrottleTimer = 0;
        handlePointerHover(e);
      }, 120);
    },
    { passive: true, capture: true }
  );

  // Playback/media events do not bubble, but a capture listener still sees them
  // on the way down for light-DOM players (shadow players are hooked directly).
  for (const event of ["play", "playing", "canplay", "canplaythrough"]) {
    document.addEventListener(
      event,
      (e) => {
        const target = e.target;
        if (target && target.tagName === "VIDEO") {
          videoTiming.set(target, { at: Date.now() });
          activateVideo(target);
        }
        scheduleRefresh();
      },
      { passive: true, capture: true }
    );
  }
  for (const event of ["pause", "ended", "loadedmetadata", "loadeddata", "durationchange"]) {
    document.addEventListener(event, scheduleRefresh, { passive: true, capture: true });
  }

  // Nested scroll containers do not bubble scroll events to window, so listen
  // in the capture phase. Each burst re-checks that the target video is still on
  // screen and retargets before painting, so the tag never drifts or hangs.
  function handleViewportChange() {
    if (positionFrame) return;
    positionFrame = requestAnimationFrame(() => {
      positionFrame = 0;
      if (!isVideoRenderable(activeVideo)) {
        const candidate = findPrimaryVideo();
        if (candidate) activeVideo = candidate;
      }
      positionOverlay();
    });
  }

  document.addEventListener("scroll", handleViewportChange, { passive: true, capture: true });
  window.addEventListener("scroll", handleViewportChange, { passive: true });
  window.addEventListener("resize", () => {
    scheduleRefresh();
    handleViewportChange();
  }, { passive: true });
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh();
  });
  document.addEventListener(
    "fullscreenchange",
    () => {
      ensureHostParent();
      refresh();
    },
    true
  );

  window.addEventListener("popstate", handleNavigationChange, { passive: true });
  if (isYouTubePage) {
    window.addEventListener("yt-navigate-finish", handleNavigationChange, { passive: true });
    window.addEventListener("yt-page-data-updated", handleNavigationChange, { passive: true });
  }

  // Coalesce bursts of DOM mutations: refresh() performs layout reads and a
  // deep <video> walk, which is too costly once per mutated node on busy pages.
  const observer = new MutationObserver(scheduleRefresh);
  observer.observe(document.documentElement, { childList: true, subtree: true });

  if (isExtensionEnv) {
    // Tab-level network detections are shared by every frame, so only the top
    // frame consumes them; a subframe would otherwise list streams that are not
    // part of the video it is pinning the tag to.
    if (isTopFrame) {
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
    }
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

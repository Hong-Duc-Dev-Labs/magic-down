const DEFAULT_EXTS = [
  "zip", "rar", "7z", "tar", "gz", "iso",
  "exe", "msi", "dmg", "apk", "bin",
  "mp4", "mkv", "avi", "mov", "webm",
  "mp3", "flac", "wav", "pdf"
];

const isExtensionEnv = typeof chrome !== "undefined" && Boolean(chrome.runtime && chrome.runtime.id);

const els = {
  root: document.documentElement,
  statusPill: document.getElementById("statusPill"),
  statusText: document.getElementById("statusText"),
  themeToggleBtn: document.getElementById("themeToggleBtn"),
  iconSun: document.getElementById("iconSun"),
  iconMoon: document.getElementById("iconMoon"),
  interceptToggle: document.getElementById("interceptToggle"),
  excludeSiteRow: document.getElementById("excludeSiteRow"),
  excludeSiteToggle: document.getElementById("excludeSiteToggle"),
  currentSiteLabel: document.getElementById("currentSiteLabel"),
  quickUrlInput: document.getElementById("quickUrlInput"),
  sendUrlBtn: document.getElementById("sendUrlBtn"),
  toast: document.getElementById("toast"),
  tabActivityBtn: document.getElementById("tabActivityBtn"),
  tabFiltersBtn: document.getElementById("tabFiltersBtn"),
  activityCount: document.getElementById("activityCount"),
  extCount: document.getElementById("extCount"),
  panelActivity: document.getElementById("panelActivity"),
  panelFilters: document.getElementById("panelFilters"),
  activityList: document.getElementById("activityList"),
  activityEmpty: document.getElementById("activityEmpty"),
  clearHistoryBtn: document.getElementById("clearHistoryBtn"),
  extChips: document.getElementById("extChips"),
  addExtInput: document.getElementById("addExtInput"),
  addExtBtn: document.getElementById("addExtBtn"),
  minSizeInput: document.getElementById("minSizeInput"),
  domainChips: document.getElementById("domainChips"),
  addDomainInput: document.getElementById("addDomainInput"),
  addDomainBtn: document.getElementById("addDomainBtn"),
  footerStatus: document.getElementById("footerStatus")
};

let state = {
  theme: "dark",
  interceptEnabled: true,
  extensions: [...DEFAULT_EXTS],
  minFileSizeMB: 0,
  excludedDomains: [],
  recentDownloads: [],
  currentHost: ""
};

let toastTimer = null;

function showToast(msg, kind = "info") {
  els.toast.textContent = msg;
  els.toast.className = "toast";
  if (kind === "success") els.toast.classList.add("toast-success");
  if (kind === "error") els.toast.classList.add("toast-error");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    els.toast.classList.add("hidden");
  }, 3200);
}

function applyTheme(theme) {
  state.theme = theme === "light" ? "light" : "dark";
  els.root.setAttribute("data-theme", state.theme);
  els.iconSun.classList.toggle("hidden", state.theme === "light");
  els.iconMoon.classList.toggle("hidden", state.theme === "dark");
}

function setConnectionStatus(kind, label) {
  els.statusPill.className = `status-pill status-${kind}`;
  els.statusText.textContent = label;
  if (kind === "online") {
    els.footerStatus.textContent = "IPC :28888 Connected";
  } else if (kind === "offline") {
    els.footerStatus.textContent = "Desktop App Offline";
  } else if (kind === "error") {
    els.footerStatus.textContent = "Host Not Registered";
  }
}

function setActiveTab(tab) {
  const isActivity = tab === "activity";
  els.tabActivityBtn.classList.toggle("active", isActivity);
  els.tabActivityBtn.setAttribute("aria-selected", String(isActivity));
  els.tabFiltersBtn.classList.toggle("active", !isActivity);
  els.tabFiltersBtn.setAttribute("aria-selected", String(!isActivity));
  els.panelActivity.classList.toggle("hidden", !isActivity);
  els.panelFilters.classList.toggle("hidden", isActivity);
}

function formatRelativeTime(ts) {
  const diffSec = Math.max(0, Math.floor((Date.now() - ts) / 1000));
  if (diffSec < 60) return "just now";
  const mins = Math.floor(diffSec / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

function renderActivity() {
  const items = state.recentDownloads || [];
  els.activityCount.textContent = String(items.length);
  els.activityList.textContent = "";

  if (items.length === 0) {
    els.activityList.classList.add("hidden");
    els.activityEmpty.classList.remove("hidden");
    els.clearHistoryBtn.classList.add("hidden");
    return;
  }

  els.activityList.classList.remove("hidden");
  els.activityEmpty.classList.add("hidden");
  els.clearHistoryBtn.classList.remove("hidden");

  for (const item of items) {
    const row = document.createElement("div");
    row.className = "activity-item";

    const main = document.createElement("div");
    main.className = "activity-main";

    const name = document.createElement("div");
    name.className = "activity-name";
    name.textContent = item.filename || item.url;
    name.title = item.url || "";

    const meta = document.createElement("div");
    meta.className = "activity-meta";
    meta.textContent = `${item.host || "direct"}  •  ${formatRelativeTime(item.timestamp)}`;

    main.appendChild(name);
    main.appendChild(meta);

    const pill = document.createElement("span");
    const isSent = item.status === "sent";
    pill.className = `activity-pill ${isSent ? "sent" : "failed"}`;
    pill.textContent = isSent ? "Sent" : "Failed";
    if (!isSent && item.error) pill.title = item.error;

    row.appendChild(main);
    row.appendChild(pill);
    els.activityList.appendChild(row);
  }
}

function renderChips(container, values, onRemove, emptyText) {
  container.textContent = "";
  if (!values || values.length === 0) {
    const empty = document.createElement("span");
    empty.className = "chips-empty";
    empty.textContent = emptyText;
    container.appendChild(empty);
    return;
  }

  for (const val of values) {
    const chip = document.createElement("span");
    chip.className = "chip";

    const text = document.createElement("span");
    text.textContent = val;

    const btn = document.createElement("button");
    btn.className = "chip-remove";
    btn.type = "button";
    btn.textContent = "×";
    btn.setAttribute("aria-label", `Remove ${val}`);
    btn.addEventListener("click", () => onRemove(val));

    chip.appendChild(text);
    chip.appendChild(btn);
    container.appendChild(chip);
  }
}

function renderFilters() {
  els.extCount.textContent = String(state.extensions.length);
  renderChips(els.extChips, state.extensions, removeExtension, "All files intercepted");
  els.minSizeInput.value = String(state.minFileSizeMB);
  renderChips(els.domainChips, state.excludedDomains, removeDomain, "No excluded domains");
  renderCurrentSiteToggle();
}

function renderCurrentSiteToggle() {
  if (!state.currentHost) {
    els.currentSiteLabel.textContent = "Not available on this page";
    els.excludeSiteToggle.checked = false;
    els.excludeSiteToggle.disabled = true;
    els.excludeSiteRow.classList.add("disabled");
    return;
  }

  els.currentSiteLabel.textContent = state.currentHost;
  els.excludeSiteToggle.disabled = false;
  els.excludeSiteRow.classList.remove("disabled");
  els.excludeSiteToggle.checked = state.excludedDomains.includes(state.currentHost);
}

async function persist(patch) {
  Object.assign(state, patch);
  if (isExtensionEnv) {
    await chrome.storage.local.set(patch);
  }
}

async function removeExtension(ext) {
  const next = state.extensions.filter((e) => e !== ext);
  await persist({ extensions: next });
  renderFilters();
}

async function addExtensionsFromInput() {
  const raw = els.addExtInput.value.trim();
  if (!raw) return;
  const incoming = raw
    .split(/[,\s]+/)
    .map((s) => s.trim().replace(/^\./, "").toLowerCase())
    .filter(Boolean);

  const merged = Array.from(new Set([...state.extensions, ...incoming]));
  els.addExtInput.value = "";
  await persist({ extensions: merged });
  renderFilters();
  showToast(`Added ${incoming.join(", ")}`, "success");
}

async function removeDomain(domain) {
  const next = state.excludedDomains.filter((d) => d !== domain);
  await persist({ excludedDomains: next });
  renderFilters();
}

async function addDomainFromInput() {
  let raw = els.addDomainInput.value.trim().toLowerCase();
  if (!raw) return;
  try {
    if (raw.includes("://")) {
      raw = new URL(raw).hostname.toLowerCase();
    } else {
      raw = raw.split("/")[0];
    }
  } catch {
    // keep cleaned string
  }
  if (!raw) return;

  const merged = Array.from(new Set([...state.excludedDomains, raw]));
  els.addDomainInput.value = "";
  await persist({ excludedDomains: merged });
  renderFilters();
  showToast(`Excluded ${raw}`, "success");
}

async function checkConnection() {
  if (!isExtensionEnv) {
    setConnectionStatus("online", "Connected");
    return;
  }
  setConnectionStatus("checking", "Checking...");
  const resp = await chrome.runtime.sendMessage({ type: "ping" });
  if (!resp || resp.hostError) {
    setConnectionStatus("error", "Host Missing");
    showToast("Run scripts/install-host.ps1 to register Native Host.", "error");
    return;
  }
  if (resp.success && resp.app_running) {
    setConnectionStatus("online", "Connected");
  } else {
    setConnectionStatus("offline", "App Offline");
  }
}

async function detectActiveTabHost() {
  if (!isExtensionEnv) {
    state.currentHost = "github.com";
    renderCurrentSiteToggle();
    return;
  }
  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (tab && tab.url && (tab.url.startsWith("http://") || tab.url.startsWith("https://"))) {
      state.currentHost = new URL(tab.url).hostname.toLowerCase();
    } else {
      state.currentHost = "";
    }
  } catch {
    state.currentHost = "";
  }
  renderCurrentSiteToggle();
}

async function loadState() {
  if (!isExtensionEnv) {
    // Preview mode when opened directly as file:// for visual verification
    const params = new URLSearchParams(window.location.search);
    state.theme = params.get("theme") === "light" ? "light" : "dark";
    state.excludedDomains = ["intranet.local", "docs.internal"];
    state.minFileSizeMB = 5;
    state.recentDownloads = [
      {
        id: "t1",
        filename: "ubuntu-24.04.2-desktop-amd64.iso",
        host: "releases.ubuntu.com",
        url: "https://releases.ubuntu.com/24.04/ubuntu-24.04.2-desktop-amd64.iso",
        status: "sent",
        timestamp: Date.now() - 2 * 60 * 1000
      },
      {
        id: "t2",
        filename: "DesignSystem-Kit-v4.2.zip",
        host: "cdn.example.com",
        url: "https://cdn.example.com/DesignSystem-Kit-v4.2.zip",
        status: "sent",
        timestamp: Date.now() - 18 * 60 * 1000
      },
      {
        id: "t3",
        filename: "Q4-Architecture-Spec.pdf",
        host: "docs.example.io",
        url: "https://docs.example.io/Q4-Architecture-Spec.pdf",
        status: "failed",
        error: "Desktop app offline",
        timestamp: Date.now() - 52 * 60 * 1000
      }
    ];
    applyTheme(state.theme);
    els.interceptToggle.checked = state.interceptEnabled;
    renderActivity();
    renderFilters();
    setActiveTab(params.get("tab") === "filters" ? "filters" : "activity");
    return;
  }

  const stored = await chrome.storage.local.get([
    "theme",
    "interceptEnabled",
    "extensions",
    "minFileSizeMB",
    "excludedDomains",
    "recentDownloads"
  ]);

  state.theme = stored.theme || "dark";
  state.interceptEnabled = stored.interceptEnabled !== false;
  state.extensions = Array.isArray(stored.extensions) ? stored.extensions : [...DEFAULT_EXTS];
  state.minFileSizeMB = typeof stored.minFileSizeMB === "number" ? stored.minFileSizeMB : 0;
  state.excludedDomains = Array.isArray(stored.excludedDomains) ? stored.excludedDomains : [];
  state.recentDownloads = Array.isArray(stored.recentDownloads) ? stored.recentDownloads : [];

  applyTheme(state.theme);
  els.interceptToggle.checked = state.interceptEnabled;
  renderActivity();
  renderFilters();
}

// Event bindings (strict MV3 CSP — zero inline handlers)
els.statusPill.addEventListener("click", checkConnection);

els.themeToggleBtn.addEventListener("click", async () => {
  const nextTheme = state.theme === "dark" ? "light" : "dark";
  applyTheme(nextTheme);
  await persist({ theme: nextTheme });
});

els.interceptToggle.addEventListener("change", async () => {
  const enabled = els.interceptToggle.checked;
  await persist({ interceptEnabled: enabled });
  showToast(enabled ? "Auto-interception enabled." : "Auto-interception paused.", enabled ? "success" : "info");
});

els.excludeSiteToggle.addEventListener("change", async () => {
  if (!state.currentHost) return;
  let next;
  if (els.excludeSiteToggle.checked) {
    next = Array.from(new Set([...state.excludedDomains, state.currentHost]));
    showToast(`Excluded ${state.currentHost} from interception.`);
  } else {
    next = state.excludedDomains.filter((d) => d !== state.currentHost);
    showToast(`Re-enabled interception on ${state.currentHost}.`, "success");
  }
  await persist({ excludedDomains: next });
  renderFilters();
});

els.tabActivityBtn.addEventListener("click", () => setActiveTab("activity"));
els.tabFiltersBtn.addEventListener("click", () => setActiveTab("filters"));

els.addExtBtn.addEventListener("click", addExtensionsFromInput);
els.addExtInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") addExtensionsFromInput();
});

els.addDomainBtn.addEventListener("click", addDomainFromInput);
els.addDomainInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") addDomainFromInput();
});

els.minSizeInput.addEventListener("change", async () => {
  const val = Math.max(0, Number.parseInt(els.minSizeInput.value, 10) || 0);
  els.minSizeInput.value = String(val);
  await persist({ minFileSizeMB: val });
  showToast(val > 0 ? `Minimum size set to ${val} MB.` : "Minimum size filter disabled.", "success");
});

els.clearHistoryBtn.addEventListener("click", async () => {
  await persist({ recentDownloads: [] });
  renderActivity();
  showToast("Cleared recent activity.");
});

async function handleSendUrl() {
  const url = els.quickUrlInput.value.trim();
  if (!url || (!url.startsWith("http://") && !url.startsWith("https://"))) {
    showToast("Enter a valid http:// or https:// URL.", "error");
    return;
  }

  if (!isExtensionEnv) {
    els.quickUrlInput.value = "";
    showToast("Preview mode: download queued.", "success");
    return;
  }

  const resp = await chrome.runtime.sendMessage({ type: "download", url });
  if (resp && resp.success) {
    els.quickUrlInput.value = "";
    showToast("Sent to MagicDown desktop app.", "success");
    await loadState();
  } else {
    showToast(resp?.error || "Failed to send download.", "error");
  }
}

els.sendUrlBtn.addEventListener("click", handleSendUrl);
els.quickUrlInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") handleSendUrl();
});

// Boot
loadState().then(() => {
  detectActiveTabHost();
  checkConnection();
});

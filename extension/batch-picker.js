(() => {
  const scanner = window.MagicDownBatchScanner;
  const I18N = window.MagicDownI18n;
  function t(key, params) {
    return I18N ? I18N.t(key, params) : key;
  }
  function applyLanguage(lang) {
    const resolved = I18N ? I18N.setLang(lang) : (lang === "vi" ? "vi" : "en");
    els.root.setAttribute("lang", resolved);
    document.title = t("batch.title");
    for (const el of document.querySelectorAll("[data-i18n]")) {
      el.textContent = t(el.dataset.i18n);
    }
    for (const el of document.querySelectorAll("[data-i18n-placeholder]")) {
      el.placeholder = t(el.dataset.i18nPlaceholder);
    }
    for (const el of document.querySelectorAll("[data-i18n-title]")) {
      el.title = t(el.dataset.i18nTitle);
    }
    updateFooterCounts(getFilteredItems());
  }
  const isExtensionEnv = typeof chrome !== "undefined" && Boolean(chrome.runtime && chrome.runtime.id);

  const els = {
    root: document.documentElement,
    pageSourceLabel: document.getElementById("pageSourceLabel"),
    subfolderInput: document.getElementById("subfolderInput"),
    rescanBtn: document.getElementById("rescanBtn"),
    catTabs: Array.from(document.querySelectorAll(".cat-tab")),
    countImage: document.getElementById("countImage"),
    countMedia: document.getElementById("countMedia"),
    countFile: document.getElementById("countFile"),
    countAll: document.getElementById("countAll"),
    minSizeSelect: document.getElementById("minSizeSelect"),
    searchInput: document.getElementById("searchInput"),
    selectAllBtn: document.getElementById("selectAllBtn"),
    deselectAllBtn: document.getElementById("deselectAllBtn"),
    statusBanner: document.getElementById("statusBanner"),
    resourceContainer: document.getElementById("resourceContainer"),
    emptyState: document.getElementById("emptyState"),
    visibleCount: document.getElementById("visibleCount"),
    selectedCount: document.getElementById("selectedCount"),
    downloadAllBtn: document.getElementById("downloadAllBtn"),
    downloadSelectedBtn: document.getElementById("downloadSelectedBtn")
  };

  const state = {
    tabId: -1,
    pageTitle: "",
    pageUrl: "",
    items: [],
    selectedUrls: new Set(),
    category: "image",
    minImageSize: 100,
    query: "",
    busy: false
  };

  function showStatus(msg, kind = "info") {
    els.statusBanner.textContent = msg;
    els.statusBanner.className = "status-banner";
    if (kind === "success") els.statusBanner.classList.add("success");
    if (kind === "error") els.statusBanner.classList.add("error");
  }

  function hideStatus() {
    els.statusBanner.classList.add("hidden");
  }

  function getFilteredItems() {
    return scanner.filterResources(state.items, {
      category: state.category,
      minImageSize: state.minImageSize,
      query: state.query
    });
  }

  function updateCategoryCounts() {
    const counts = { image: 0, media: 0, file: 0, all: state.items.length };
    for (const item of state.items) {
      if (item.category === "image") counts.image++;
      else if (item.category === "media") counts.media++;
      else if (item.category === "file") counts.file++;
    }
    els.countImage.textContent = String(counts.image);
    els.countMedia.textContent = String(counts.media);
    els.countFile.textContent = String(counts.file);
    els.countAll.textContent = String(counts.all);
  }

  function updateFooterCounts(visibleItems) {
    els.visibleCount.textContent = String(visibleItems.length);
    els.selectedCount.textContent = String(state.selectedUrls.size);
    els.downloadSelectedBtn.textContent = t("batch.download_selected", { n: state.selectedUrls.size });
    els.downloadSelectedBtn.disabled = state.busy || state.selectedUrls.size === 0;
    els.downloadAllBtn.textContent = t("batch.download_all", { n: visibleItems.length });
    els.downloadAllBtn.disabled = state.busy || visibleItems.length === 0;
  }

  function toggleSelection(url, cardEl, checkboxEl) {
    if (state.selectedUrls.has(url)) {
      state.selectedUrls.delete(url);
      cardEl.classList.remove("selected");
      checkboxEl.checked = false;
    } else {
      state.selectedUrls.add(url);
      cardEl.classList.add("selected");
      checkboxEl.checked = true;
    }
    updateFooterCounts(getFilteredItems());
  }

  function render() {
    updateCategoryCounts();
    const visible = getFilteredItems();
    els.resourceContainer.textContent = "";

    const useGrid = state.category === "image";
    els.resourceContainer.className = useGrid ? "resource-grid" : "resource-list";

    if (visible.length === 0) {
      els.emptyState.classList.remove("hidden");
      updateFooterCounts(visible);
      return;
    }
    els.emptyState.classList.add("hidden");

    const frag = document.createDocumentFragment();
    for (const item of visible) {
      const isSelected = state.selectedUrls.has(item.url);

      if (useGrid && item.category === "image") {
        const card = document.createElement("div");
        card.className = `img-card${isSelected ? " selected" : ""}`;

        const thumbWrap = document.createElement("div");
        thumbWrap.className = "img-thumb-wrap";

        const checkbox = document.createElement("input");
        checkbox.type = "checkbox";
        checkbox.className = "item-checkbox";
        checkbox.checked = isSelected;
        checkbox.addEventListener("click", (e) => {
          e.stopPropagation();
          toggleSelection(item.url, card, checkbox);
        });

        const img = document.createElement("img");
        img.className = "img-thumb";
        img.loading = "lazy";
        img.src = item.url;
        img.alt = item.filename || "";

        thumbWrap.appendChild(checkbox);
        thumbWrap.appendChild(img);

        if (item.width > 0 && item.height > 0) {
          const dim = document.createElement("span");
          dim.className = "dim-badge";
          dim.textContent = `${item.width}×${item.height}`;
          thumbWrap.appendChild(dim);
        }

        const meta = document.createElement("div");
        meta.className = "img-meta";

        const name = document.createElement("div");
        name.className = "item-name";
        name.textContent = item.filename;
        name.title = item.url;

        const sub = document.createElement("div");
        sub.className = "item-sub";
        sub.textContent = (item.ext || "IMG").toUpperCase();

        meta.appendChild(name);
        meta.appendChild(sub);

        card.appendChild(thumbWrap);
        card.appendChild(meta);
        card.addEventListener("click", () => toggleSelection(item.url, card, checkbox));
        frag.appendChild(card);
      } else {
        const row = document.createElement("div");
        row.className = `list-row${isSelected ? " selected" : ""}`;

        const checkbox = document.createElement("input");
        checkbox.type = "checkbox";
        checkbox.checked = isSelected;
        checkbox.addEventListener("click", (e) => {
          e.stopPropagation();
          toggleSelection(item.url, row, checkbox);
        });

        const ext = document.createElement("span");
        ext.className = "list-ext";
        ext.textContent = item.ext || item.category;

        const info = document.createElement("div");
        info.className = "list-info";

        const name = document.createElement("div");
        name.className = "item-name";
        name.textContent = item.filename;
        name.title = item.url;

        const sub = document.createElement("div");
        sub.className = "item-sub";
        sub.textContent = item.url;

        info.appendChild(name);
        info.appendChild(sub);

        row.appendChild(checkbox);
        row.appendChild(ext);
        row.appendChild(info);
        row.addEventListener("click", () => toggleSelection(item.url, row, checkbox));
        frag.appendChild(row);
      }
    }

    els.resourceContainer.appendChild(frag);
    updateFooterCounts(visible);
  }

  async function submitBatch(itemsToDownload) {
    if (!itemsToDownload.length || state.busy) return;
    state.busy = true;
    updateFooterCounts(getFilteredItems());
    showStatus(t("batch.status_sending", { n: itemsToDownload.length }), "info");

    try {
      const resp = await chrome.runtime.sendMessage({
        type: "batchDownload",
        items: itemsToDownload,
        pageUrl: state.pageUrl,
        subfolder: els.subfolderInput.value.trim()
      });

      if (resp && resp.success) {
        const added = typeof resp.added === "number" ? resp.added : itemsToDownload.length;
        const failed = typeof resp.failed === "number" ? resp.failed : 0;
        showStatus(
          failed > 0
            ? t("batch.status_added_partial", { added, failed })
            : t("batch.status_added_ok", { added }),
          "success"
        );
      } else {
        showStatus(resp?.error || t("batch.status_connect_err"), "error");
      }
    } catch (err) {
      showStatus(t("batch.status_send_err", { err: err?.message || err }), "error");
    } finally {
      state.busy = false;
      updateFooterCounts(getFilteredItems());
    }
  }

  function applyScanResult(data) {
    state.tabId = typeof data.tabId === "number" ? data.tabId : state.tabId;
    state.pageTitle = data.pageTitle || "";
    state.pageUrl = data.pageUrl || "";
    state.items = Array.isArray(data.items) ? data.items : [];
    state.selectedUrls.clear();

    els.pageSourceLabel.textContent = state.pageTitle
      ? `${state.pageTitle} — ${state.pageUrl}`
      : state.pageUrl || t("batch.unknown_source");
    els.subfolderInput.value = scanner.suggestSubfolder(state.pageTitle, state.pageUrl);

    // Pick best initial tab (default to image if images exist, otherwise all)
    const hasImages = state.items.some((i) => i.category === "image");
    state.category = hasImages ? "image" : "all";
    for (const tab of els.catTabs) {
      const active = tab.dataset.category === state.category;
      tab.classList.toggle("active", active);
      tab.setAttribute("aria-selected", String(active));
    }

    hideStatus();
    render();
  }

  async function boot() {
    if (!isExtensionEnv) {
      applyLanguage(I18N ? I18N.detectBrowserLang() : "en");
      return;
    }

    const stored = await chrome.storage.local.get(["theme", "language"]);
    els.root.setAttribute("data-theme", stored.theme === "light" ? "light" : "dark");
    applyLanguage(I18N ? I18N.resolveLang(stored.language) : (stored.language || "en"));

    if (chrome.storage && chrome.storage.onChanged) {
      chrome.storage.onChanged.addListener((changes, areaName) => {
        if (areaName === "local" && changes.language && I18N) {
          applyLanguage(I18N.resolveLang(changes.language.newValue));
        }
      });
    }

    const params = new URLSearchParams(window.location.search);
    const scanId = params.get("scanId") || "";
    const resp = await chrome.runtime.sendMessage({ type: "getBatchScanResult", scanId });
    if (resp && resp.ok && resp.data) {
      applyScanResult(resp.data);
    } else {
      showStatus(t("batch.no_scan_data"), "error");
    }
  }

  // Event listeners
  for (const tab of els.catTabs) {
    tab.addEventListener("click", () => {
      state.category = tab.dataset.category || "all";
      for (const t of els.catTabs) {
        const active = t === tab;
        t.classList.toggle("active", active);
        t.setAttribute("aria-selected", String(active));
      }
      render();
    });
  }

  els.minSizeSelect.addEventListener("change", () => {
    state.minImageSize = Number.parseInt(els.minSizeSelect.value, 10) || 0;
    render();
  });

  els.searchInput.addEventListener("input", () => {
    state.query = els.searchInput.value;
    render();
  });

  els.selectAllBtn.addEventListener("click", () => {
    for (const item of getFilteredItems()) {
      state.selectedUrls.add(item.url);
    }
    render();
  });

  els.deselectAllBtn.addEventListener("click", () => {
    for (const item of getFilteredItems()) {
      state.selectedUrls.delete(item.url);
    }
    render();
  });

  els.downloadSelectedBtn.addEventListener("click", () => {
    const selectedItems = state.items.filter((i) => state.selectedUrls.has(i.url));
    submitBatch(selectedItems);
  });

  els.downloadAllBtn.addEventListener("click", () => {
    submitBatch(getFilteredItems());
  });

  els.rescanBtn.addEventListener("click", async () => {
    if (!isExtensionEnv || state.tabId < 0) return;
    showStatus(t("batch.rescanning"), "info");
    const resp = await chrome.runtime.sendMessage({ type: "rescanTabResources", tabId: state.tabId });
    if (resp && resp.ok && resp.data) {
      applyScanResult(resp.data);
    } else {
      showStatus(resp?.error || t("batch.rescan_err"), "error");
    }
  });

  boot();
})();

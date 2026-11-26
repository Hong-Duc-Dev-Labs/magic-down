const test = require("node:test");
const assert = require("node:assert/strict");

const {
  DEFAULT_SETTINGS,
  LEGACY_DEFAULT_EXTENSIONS,
  getSettings,
  isLegacyDefaultExtensionList,
  clearPendingDownload,
  isInterceptableHttpUrl,
  sanitizeDownloadFilename,
  shouldIntercept,
  markDownloadBypass,
  isDownloadBypassed,
  clearDownloadBypass,
  handleDownloadInterception,
  onDownloadCreated,
  ON_CREATED_FALLBACK_DELAY_MS,
  buildDownloadPayload,
  handledDownloadIds,
  fallbackDownloadIds,
  pendingDownloads
} = require("./background.js");

function resetState() {
  clearDownloadBypass();
  handledDownloadIds.clear();
  fallbackDownloadIds.clear();
  for (const id of Array.from(pendingDownloads.keys())) {
    clearPendingDownload(id);
  }
}

test("DEFAULT_SETTINGS intercepts all browser downloads by default (IDM-style)", () => {
  assert.equal(DEFAULT_SETTINGS.interceptEnabled, true);
  assert.ok(DEFAULT_SETTINGS.extensions.includes("*"));
});

test("isInterceptableHttpUrl accepts http/https and rejects blob, data, file, chrome, edge, etc.", () => {
  assert.equal(isInterceptableHttpUrl("https://example.com/file.zip"), true);
  assert.equal(isInterceptableHttpUrl("http://downloads.example.org/release?id=42"), true);
  assert.equal(isInterceptableHttpUrl("HTTPS://CDN.EXAMPLE.COM/app.exe"), true);

  const rejected = [
    "",
    null,
    undefined,
    "blob:https://example.com/550e8400-e29b-41d4-a716-446655440000",
    "data:application/octet-stream;base64,SGVsbG8=",
    "file:///C:/Users/alice/Downloads/test.zip",
    "chrome://downloads/",
    "chrome-extension://abcdefg/popup.html",
    "edge://downloads/",
    "about:blank",
    "javascript:void(0)",
    "ftp://files.example.com/archive.tar.gz"
  ];
  for (const url of rejected) {
    assert.equal(isInterceptableHttpUrl(url), false, `expected ${String(url)} to be rejected`);
  }
});

test("sanitizeDownloadFilename strips directory paths and traversal sequences for IPC safety", () => {
  assert.equal(sanitizeDownloadFilename("report.pdf"), "report.pdf");
  assert.equal(sanitizeDownloadFilename("Downloads/setup.exe"), "setup.exe");
  assert.equal(sanitizeDownloadFilename("C:\\Users\\alice\\Downloads\\archive.zip"), "archive.zip");
  assert.equal(sanitizeDownloadFilename("../../etc/passwd"), "passwd");
  assert.equal(sanitizeDownloadFilename("my..weird..file.tar.gz"), "my.weird.file.tar.gz");
  assert.equal(sanitizeDownloadFilename(".."), "");
  assert.equal(sanitizeDownloadFilename(""), "");
});

test("buildDownloadPayload sanitizes filename and preserves referrer, cookies, User-Agent, and total_bytes", () => {
  const payload = buildDownloadPayload({
    url: "https://cdn.example.com/download?id=99",
    filename: "C:\\Users\\alice\\Downloads\\release-v2.zip",
    referrer: "https://example.com/releases",
    cookies: "session=xyz123",
    headers: { "User-Agent": "Mozilla/5.0 TestBrowser" },
    size: 10485760
  });
  assert.equal(payload.action, "download");
  assert.equal(payload.url, "https://cdn.example.com/download?id=99");
  assert.equal(payload.filename, "release-v2.zip");
  assert.equal(payload.referrer, "https://example.com/releases");
  assert.equal(payload.cookies, "session=xyz123");
  assert.equal(payload.headers["User-Agent"], "Mozilla/5.0 TestBrowser");
  assert.equal(payload.total_bytes, 10485760);
});

test("handleDownloadInterception intercepts HTTP/HTTPS downloads with Content-Disposition filename, cancels and erases native download", async () => {
  resetState();
  const forwarded = [];
  const calls = [];
  let suggestCount = 0;

  const result = await handleDownloadInterception(
    {
      id: 101,
      url: "https://example.com/redirect",
      finalUrl: "https://cdn.example.com/api/export?id=777",
      filename: "sub/quarterly-report.xlsx",
      referrer: "https://example.com/dashboard",
      fileSize: 5242880,
      state: "in_progress"
    },
    {
      getSettings: async () => ({ ...DEFAULT_SETTINGS }),
      forwardDownload: async (req) => {
        forwarded.push(req);
        return { success: true, taskId: "task-101", filename: req.filename };
      },
      downloadsApi: {
        cancel: async (id) => calls.push(["cancel", id]),
        erase: async (q) => calls.push(["erase", q.id]),
        pause: async (id) => calls.push(["pause", id]),
        resume: async (id) => calls.push(["resume", id])
      },
      suggest: () => {
        suggestCount++;
      }
    }
  );

  assert.equal(result.intercepted, true);
  assert.equal(forwarded.length, 1);
  assert.equal(forwarded[0].url, "https://cdn.example.com/api/export?id=777");
  assert.equal(forwarded[0].filename, "quarterly-report.xlsx");
  assert.equal(forwarded[0].referrer, "https://example.com/dashboard");
  assert.equal(forwarded[0].size, 5242880);
  assert.deepEqual(calls, [
    ["pause", 101],
    ["cancel", 101],
    ["erase", 101]
  ]);
  // suggest() must NOT be called after successful cancel so Chrome never opens Save As.
  assert.equal(suggestCount, 0);

  // Duplicate event for the same download ID must not re-intercept, loop, or trigger Save As.
  const dup = await handleDownloadInterception(
    {
      id: 101,
      url: "https://cdn.example.com/api/export?id=777",
      filename: "quarterly-report.xlsx"
    },
    {
      getSettings: async () => ({ ...DEFAULT_SETTINGS }),
      forwardDownload: async () => {
        throw new Error("must not forward duplicate download ID");
      },
      suggest: () => {
        suggestCount++;
      }
    }
  );
  assert.equal(dup.intercepted, false);
  assert.equal(dup.reason, "already_handled");
  assert.equal(suggestCount, 0);
});

test("handleDownloadInterception skips blob:, data:, chrome:, edge: URLs without calling native host", async () => {
  resetState();
  const urls = [
    "blob:https://example.com/abc",
    "data:text/plain;base64,SGVsbG8=",
    "file:///C:/temp/a.txt",
    "chrome://settings",
    "chrome-extension://abc/file.js",
    "edge://downloads"
  ];

  for (let i = 0; i < urls.length; i++) {
    let suggested = false;
    const res = await handleDownloadInterception(
      { id: 200 + i, url: urls[i], filename: "test.zip" },
      {
        getSettings: async () => ({ ...DEFAULT_SETTINGS }),
        forwardDownload: async () => {
          throw new Error("should not forward non-http URL");
        },
        suggest: () => {
          suggested = true;
        }
      }
    );
    assert.equal(res.intercepted, false);
    assert.equal(res.reason, "filtered");
    assert.equal(suggested, true);
  }
});

test("handleDownloadInterception bypasses when popup toggle is disabled", async () => {
  resetState();
  let suggested = false;
  const res = await handleDownloadInterception(
    { id: 301, url: "https://example.com/app.exe", filename: "app.exe" },
    {
      getSettings: async () => ({ ...DEFAULT_SETTINGS, interceptEnabled: false }),
      forwardDownload: async () => {
        throw new Error("should not forward when disabled");
      },
      suggest: () => {
        suggested = true;
      }
    }
  );
  assert.equal(res.intercepted, false);
  assert.equal(res.reason, "disabled");
  assert.equal(suggested, true);
});

test("Alt+click bypass skips interception even when onDeterminingFilename fires after redirect", async () => {
  resetState();
  const t0 = 1_000_000;
  markDownloadBypass({ url: "https://example.com/dl", ttlMs: 5000, now: t0 });
  assert.equal(isDownloadBypassed("https://example.com/dl", t0 + 1000), true);

  // onCreated captures the click-time bypass while Alt window is still active.
  onDownloadCreated(
    { id: 401, url: "https://example.com/dl", referrer: "https://example.com/page" },
    {
      now: t0 + 500,
      downloadsApi: { onDeterminingFilename: {} }
    }
  );

  // Even 8 seconds later (after TTL expired during slow redirect), bypass is still honored.
  let suggested = false;
  const res = await handleDownloadInterception(
    {
      id: 401,
      url: "https://example.com/dl",
      finalUrl: "https://cdn.example.com/real-file.zip",
      filename: "real-file.zip"
    },
    {
      now: t0 + 8000,
      getSettings: async () => ({ ...DEFAULT_SETTINGS }),
      forwardDownload: async () => {
        throw new Error("should not forward Alt-bypassed download");
      },
      suggest: () => {
        suggested = true;
      }
    }
  );
  assert.equal(res.intercepted, false);
  assert.equal(res.reason, "bypassed");
  assert.equal(suggested, true);
});

test("Fail-safe: when MagicDown Desktop is offline or native messaging fails, browser download is NOT cancelled and continues", async () => {
  resetState();
  const calls = [];
  let suggested = false;

  // Path 1: onDeterminingFilename path
  const res1 = await handleDownloadInterception(
    {
      id: 501,
      url: "https://example.com/ubuntu.iso",
      filename: "ubuntu.iso",
      fileSize: 4000000000
    },
    {
      getSettings: async () => ({ ...DEFAULT_SETTINGS }),
      forwardDownload: async () => ({
        success: false,
        hostError: true,
        error: "MagicDown desktop app is not running"
      }),
      downloadsApi: {
        cancel: async (id) => calls.push(["cancel", id]),
        erase: async (q) => calls.push(["erase", q.id]),
        pause: async (id) => calls.push(["pause", id]),
        resume: async (id) => calls.push(["resume", id])
      },
      suggest: () => {
        suggested = true;
      }
    }
  );

  assert.equal(res1.intercepted, false);
  assert.equal(res1.reason, "host_error");
  assert.equal(suggested, true);
  assert.deepEqual(calls, [
    ["pause", 501],
    ["resume", 501]
  ]);

  // Path 2: onCreated fallback path (no suggest) pauses then resumes on error
  const calls2 = [];
  const res2 = await handleDownloadInterception(
    {
      id: 502,
      url: "https://example.com/installer.msi",
      filename: "installer.msi"
    },
    {
      getSettings: async () => ({ ...DEFAULT_SETTINGS }),
      forwardDownload: async () => {
        throw new Error("Native host exited");
      },
      downloadsApi: {
        cancel: async (id) => calls2.push(["cancel", id]),
        erase: async (q) => calls2.push(["erase", q.id]),
        pause: async (id) => calls2.push(["pause", id]),
        resume: async (id) => calls2.push(["resume", id])
      }
    }
  );

  assert.equal(res2.intercepted, false);
  assert.equal(res2.reason, "host_error");
  assert.deepEqual(calls2, [
    ["pause", 502],
    ["resume", 502]
  ]);
});

test("shouldIntercept respects custom extension filters (using Content-Disposition filename), excluded domains, minFileSizeMB, and self-extension loop guard", () => {
  resetState();
  const customSettings = {
    interceptEnabled: true,
    extensions: ["zip", "pdf"],
    minFileSizeMB: 5,
    excludedDomains: ["intranet.local"]
  };

  // URL has no extension, but Content-Disposition filename is .pdf and size >= 5MB -> intercept
  assert.equal(
    shouldIntercept(
      {
        id: 601,
        url: "https://docs.example.com/download?id=12",
        filename: "manual.pdf",
        fileSize: 6 * 1024 * 1024
      },
      customSettings
    ),
    true
  );

  // Too small -> skip
  assert.equal(
    shouldIntercept(
      {
        id: 602,
        url: "https://docs.example.com/download?id=12",
        filename: "manual.pdf",
        fileSize: 1 * 1024 * 1024
      },
      customSettings
    ),
    false
  );

  // Extension not in custom list -> skip
  assert.equal(
    shouldIntercept(
      {
        id: 603,
        url: "https://docs.example.com/image.png",
        filename: "image.png",
        fileSize: 10 * 1024 * 1024
      },
      customSettings
    ),
    false
  );

  // Excluded domain -> skip
  assert.equal(
    shouldIntercept(
      {
        id: 604,
        url: "https://files.intranet.local/archive.zip",
        filename: "archive.zip",
        fileSize: 10 * 1024 * 1024
      },
      customSettings
    ),
    false
  );

  // Self-triggered download -> skip (prevent infinite loop)
  assert.equal(
    shouldIntercept(
      {
        id: 605,
        url: "https://example.com/archive.zip",
        filename: "archive.zip",
        fileSize: 10 * 1024 * 1024,
        byExtensionId: "magicdown-ext-id"
      },
      customSettings,
      { extensionId: "magicdown-ext-id" }
    ),
    false
  );
});

test("Google Drive drive.usercontent.google.com/download URL with unknown size (-1) is intercepted via onDeterminingFilename", async () => {
  resetState();
  const gdriveUrl =
    "https://drive.usercontent.google.com/download?id=10pgMGJGLBVCsH_tpl-OvWS444lPP5qHa&export=download&authuser=0";
  const forwarded = [];
  const calls = [];
  let suggestCount = 0;

  onDownloadCreated(
    {
      id: 701,
      url: gdriveUrl,
      filename: "",
      referrer: "https://drive.google.com/",
      totalBytes: -1,
      fileSize: -1,
      state: "in_progress"
    },
    {
      downloadsApi: { onDeterminingFilename: {} },
      fallbackDelayMs: 5000
    }
  );

  const res = await handleDownloadInterception(
    {
      id: 701,
      url: gdriveUrl,
      finalUrl: gdriveUrl,
      filename: "Project-Deck.pptx",
      referrer: "https://drive.google.com/",
      totalBytes: -1,
      fileSize: -1,
      state: "in_progress"
    },
    {
      getSettings: async () => ({ ...DEFAULT_SETTINGS, minFileSizeMB: 5 }),
      forwardDownload: async (req) => {
        forwarded.push(req);
        return { success: true, taskId: "task-gdrive-701", filename: req.filename };
      },
      downloadsApi: {
        pause: async (id) => calls.push(["pause", id]),
        cancel: async (id) => calls.push(["cancel", id]),
        erase: async (q) => calls.push(["erase", q.id]),
        resume: async (id) => calls.push(["resume", id])
      },
      suggest: () => {
        suggestCount++;
      }
    }
  );

  assert.equal(res.intercepted, true);
  assert.equal(forwarded.length, 1);
  assert.equal(forwarded[0].url, gdriveUrl);
  assert.equal(forwarded[0].filename, "Project-Deck.pptx");
  assert.equal(forwarded[0].referrer, "https://drive.google.com/");
  assert.equal(forwarded[0].size, 0);
  assert.deepEqual(calls, [
    ["pause", 701],
    ["cancel", 701],
    ["erase", 701]
  ]);
  assert.equal(suggestCount, 0);
  assert.equal(pendingDownloads.has(701), false);
});

test("getSettings auto-migrates legacy 20-extension list to ['*'] so Google Drive files are not skipped", async () => {
  resetState();
  const store = {
    interceptEnabled: true,
    extensions: [...LEGACY_DEFAULT_EXTENSIONS],
    minFileSizeMB: 0,
    excludedDomains: []
  };
  let setCalls = 0;
  const mockStorage = {
    get: async () => ({ ...store }),
    set: async (patch) => {
      setCalls++;
      Object.assign(store, patch);
    }
  };

  assert.equal(isLegacyDefaultExtensionList(store.extensions), true);
  const migrated = await getSettings(mockStorage);
  assert.deepEqual(migrated.extensions, ["*"]);
  assert.equal(setCalls, 1);
  assert.deepEqual(store.extensions, ["*"]);

  // Verify Google Drive URL with non-legacy extension (.docx) and empty filename both intercept
  const gdriveUrl =
    "https://drive.usercontent.google.com/download?id=10pgMGJGLBVCsH_tpl-OvWS444lPP5qHa&export=download&authuser=0";
  assert.equal(
    shouldIntercept({ id: 702, url: gdriveUrl, filename: "notes.docx", totalBytes: -1, fileSize: -1 }, migrated),
    true
  );
  assert.equal(
    shouldIntercept({ id: 703, url: gdriveUrl, filename: "", totalBytes: -1, fileSize: -1 }, migrated),
    true
  );
});

test("onDownloadCreated fallback timer intercepts Google Drive download if onDeterminingFilename never fires", async () => {
  resetState();
  const gdriveUrl =
    "https://drive.usercontent.google.com/download?id=10pgMGJGLBVCsH_tpl-OvWS444lPP5qHa&export=download&authuser=0";
  const forwarded = [];
  const calls = [];

  onDownloadCreated(
    {
      id: 704,
      url: gdriveUrl,
      filename: "",
      referrer: "https://drive.google.com/",
      totalBytes: -1,
      fileSize: -1,
      state: "in_progress"
    },
    {
      fallbackDelayMs: 25,
      getSettings: async () => ({ ...DEFAULT_SETTINGS }),
      forwardDownload: async (req) => {
        forwarded.push(req);
        return { success: true, taskId: "task-gdrive-704", filename: req.filename };
      },
      downloadsApi: {
        onDeterminingFilename: {},
        search: async ({ id }) => [
          {
            id,
            url: gdriveUrl,
            finalUrl: gdriveUrl,
            filename: "C:\\Users\\hongducdev\\Downloads\\GoogleDriveArchive.zip",
            referrer: "https://drive.google.com/",
            totalBytes: 2048000,
            fileSize: 2048000,
            state: "in_progress"
          }
        ],
        pause: async (id) => calls.push(["pause", id]),
        cancel: async (id) => calls.push(["cancel", id]),
        erase: async (q) => calls.push(["erase", q.id]),
        resume: async (id) => calls.push(["resume", id])
      }
    }
  );

  await new Promise((resolve) => setTimeout(resolve, 80));

  assert.equal(forwarded.length, 1);
  assert.equal(forwarded[0].url, gdriveUrl);
  assert.equal(forwarded[0].filename, "GoogleDriveArchive.zip");
  assert.equal(forwarded[0].size, 2048000);
  assert.deepEqual(calls, [
    ["pause", 704],
    ["cancel", 704],
    ["erase", 704]
  ]);
});

test("sanitizeDownloadFilename preserves extensions (.7z/.zip/.mp4) and valid punctuation (., -, _, ())", () => {
  assert.equal(
    sanitizeDownloadFilename("FLAC-.VA-Grand_Theft_Auto_VI_The_Album-OS.7z"),
    "FLAC-.VA-Grand_Theft_Auto_VI_The_Album-OS.7z"
  );
  assert.equal(
    sanitizeDownloadFilename("Downloads\\FLAC-.VA-Grand_Theft_Auto_VI_The_Album-OS (2026).7z"),
    "FLAC-.VA-Grand_Theft_Auto_VI_The_Album-OS (2026).7z"
  );
  assert.equal(sanitizeDownloadFilename("archive-v1.2_final(1).zip"), "archive-v1.2_final(1).zip");
  assert.equal(sanitizeDownloadFilename("clip_1080p_(hdr).mp4"), "clip_1080p_(hdr).mp4");
});

test("Google Drive /download?... with Content-Disposition .7z filename: exact filename forwarded, cancel+erase called, suggest NOT called on success and IS called on fail-safe", async () => {
  resetState();
  assert.ok(
    ON_CREATED_FALLBACK_DELAY_MS >= 2000,
    `expected ON_CREATED_FALLBACK_DELAY_MS >= 2000ms to avoid racing onDeterminingFilename, got ${ON_CREATED_FALLBACK_DELAY_MS}`
  );

  const gdriveUrl =
    "https://drive.usercontent.google.com/download?id=1abcXYZ_GTA6_Album&export=download&authuser=0&confirm=t";
  const realFilename = "FLAC-.VA-Grand_Theft_Auto_VI_The_Album-OS.7z";

  // Case 1: Intercept succeeds -> MagicDown receives exact .7z filename,
  // cancel + erase are called, and suggest() is NOT called (blocks Chrome Save As).
  const forwarded = [];
  const calls = [];
  let suggestCount = 0;

  onDownloadCreated(
    {
      id: 801,
      url: gdriveUrl,
      filename: "",
      referrer: "https://drive.google.com/",
      totalBytes: -1,
      fileSize: -1,
      state: "in_progress"
    },
    {
      downloadsApi: {
        onDeterminingFilename: {},
        search: async () => [
          {
            id: 801,
            url: gdriveUrl,
            filename: "",
            state: "in_progress"
          }
        ]
      },
      getSettings: async () => ({ ...DEFAULT_SETTINGS }),
      forwardDownload: async () => {
        throw new Error("onCreated fallback timer must not hijack before onDeterminingFilename");
      }
    }
  );

  // Simulate realistic network/header delay (> old 250ms timer) before onDeterminingFilename fires.
  await new Promise((resolve) => setTimeout(resolve, 320));

  const successRes = await handleDownloadInterception(
    {
      id: 801,
      url: gdriveUrl,
      finalUrl: gdriveUrl,
      filename: realFilename,
      referrer: "https://drive.google.com/",
      totalBytes: 524288000,
      fileSize: 524288000,
      state: "in_progress"
    },
    {
      getSettings: async () => ({ ...DEFAULT_SETTINGS }),
      forwardDownload: async (req) => {
        forwarded.push(req);
        return { success: true, taskId: "task-gdrive-801", filename: req.filename };
      },
      downloadsApi: {
        pause: async (id) => calls.push(["pause", id]),
        cancel: async (id) => calls.push(["cancel", id]),
        erase: async (q) => calls.push(["erase", q.id]),
        resume: async (id) => calls.push(["resume", id])
      },
      suggest: () => {
        suggestCount++;
      }
    }
  );

  assert.equal(successRes.intercepted, true);
  assert.equal(forwarded.length, 1);
  assert.equal(forwarded[0].filename, realFilename);
  assert.equal(forwarded[0].url, gdriveUrl);
  assert.deepEqual(calls, [
    ["pause", 801],
    ["cancel", 801],
    ["erase", 801]
  ]);
  assert.equal(suggestCount, 0, "suggest() must NOT be called when interception succeeds so Save As never opens");
  assert.equal(pendingDownloads.has(801), false);

  // Case 2: Fail-safe fallback when MagicDown is offline/rejects ->
  // browser download is resumed and suggest() IS called so Chrome proceeds normally.
  const fallbackCalls = [];
  let fallbackSuggestCount = 0;

  const fallbackRes = await handleDownloadInterception(
    {
      id: 802,
      url: gdriveUrl,
      finalUrl: gdriveUrl,
      filename: realFilename,
      referrer: "https://drive.google.com/",
      totalBytes: 524288000,
      fileSize: 524288000,
      state: "in_progress"
    },
    {
      getSettings: async () => ({ ...DEFAULT_SETTINGS }),
      forwardDownload: async () => ({
        success: false,
        hostError: true,
        error: "MagicDown desktop app is not running"
      }),
      downloadsApi: {
        pause: async (id) => fallbackCalls.push(["pause", id]),
        cancel: async (id) => fallbackCalls.push(["cancel", id]),
        erase: async (q) => fallbackCalls.push(["erase", q.id]),
        resume: async (id) => fallbackCalls.push(["resume", id])
      },
      suggest: () => {
        fallbackSuggestCount++;
      }
    }
  );

  assert.equal(fallbackRes.intercepted, false);
  assert.equal(fallbackRes.reason, "host_error");
  assert.deepEqual(fallbackCalls, [
    ["pause", 802],
    ["resume", 802]
  ]);
  assert.equal(fallbackSuggestCount, 1, "suggest() must be called on fail-safe fallback");
});

test("Google Drive folder ZIP download (takeout-download-drive.usercontent.google.com, bracketed filename, totalBytes = -1) is intercepted and preserves exact filename", async () => {
  resetState();
  const initialUrl = "https://drive.google.com/drive/folders/1AbCdEfGhIjKlMnOpQrStUvWxYz";
  const finalUrl =
    "https://takeout-download-drive.usercontent.google.com/download/j/1234567890?user=0&authuser=0";
  const folderZipName = "[44.1-24 Tidal MQA]-20261126T081600Z-001.zip";

  assert.equal(sanitizeDownloadFilename(folderZipName), folderZipName);
  assert.equal(
    sanitizeDownloadFilename(`C:\\Users\\hongducdev\\Downloads\\${folderZipName}`),
    folderZipName
  );

  // Unknown size (-1) must pass both default '*' and custom ['zip'] filter even with minFileSizeMB > 0.
  assert.equal(
    shouldIntercept(
      {
        id: 901,
        url: initialUrl,
        finalUrl,
        filename: folderZipName,
        referrer: "https://drive.google.com/",
        totalBytes: -1,
        fileSize: -1,
        state: "in_progress"
      },
      { ...DEFAULT_SETTINGS, extensions: ["zip"], minFileSizeMB: 10 }
    ),
    true
  );

  const forwarded = [];
  const calls = [];
  let suggestCount = 0;

  onDownloadCreated(
    {
      id: 901,
      url: initialUrl,
      finalUrl,
      filename: "",
      referrer: "https://drive.google.com/",
      totalBytes: -1,
      fileSize: -1,
      state: "in_progress"
    },
    {
      downloadsApi: { onDeterminingFilename: {} },
      fallbackDelayMs: 5000
    }
  );

  const res = await handleDownloadInterception(
    {
      id: 901,
      url: initialUrl,
      finalUrl,
      filename: folderZipName,
      referrer: "https://drive.google.com/",
      totalBytes: -1,
      fileSize: -1,
      state: "in_progress"
    },
    {
      getSettings: async () => ({ ...DEFAULT_SETTINGS, minFileSizeMB: 5 }),
      forwardDownload: async (req) => {
        forwarded.push(req);
        return { success: true, taskId: "task-gdrive-folder-901", filename: req.filename };
      },
      downloadsApi: {
        pause: async (id) => calls.push(["pause", id]),
        cancel: async (id) => calls.push(["cancel", id]),
        erase: async (q) => calls.push(["erase", q.id]),
        resume: async (id) => calls.push(["resume", id])
      },
      suggest: () => {
        suggestCount++;
      }
    }
  );

  assert.equal(res.intercepted, true);
  assert.equal(forwarded.length, 1);
  assert.equal(forwarded[0].url, finalUrl);
  assert.equal(forwarded[0].filename, folderZipName);
  assert.equal(forwarded[0].referrer, "https://drive.google.com/");
  assert.equal(forwarded[0].size, 0);

  const payload = buildDownloadPayload({
    url: forwarded[0].url,
    filename: forwarded[0].filename,
    referrer: forwarded[0].referrer,
    cookies: "SID=abc; HSID=def",
    size: forwarded[0].size
  });
  assert.equal(payload.url, finalUrl);
  assert.equal(payload.filename, folderZipName);
  assert.equal(payload.total_bytes, undefined);

  assert.deepEqual(calls, [
    ["pause", 901],
    ["cancel", 901],
    ["erase", 901]
  ]);
  assert.equal(suggestCount, 0);
  assert.equal(pendingDownloads.has(901), false);
});

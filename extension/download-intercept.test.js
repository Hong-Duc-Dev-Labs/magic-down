const test = require("node:test");
const assert = require("node:assert/strict");

const {
  DEFAULT_SETTINGS,
  isInterceptableHttpUrl,
  sanitizeDownloadFilename,
  shouldIntercept,
  markDownloadBypass,
  isDownloadBypassed,
  clearDownloadBypass,
  handleDownloadInterception,
  onDownloadCreated,
  buildDownloadPayload,
  handledDownloadIds,
  pendingDownloads
} = require("./background.js");

function resetState() {
  clearDownloadBypass();
  handledDownloadIds.clear();
  pendingDownloads.clear();
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
    ["cancel", 101],
    ["erase", 101]
  ]);
  assert.equal(suggestCount, 1);

  // Duplicate event for the same download ID must not re-intercept or loop.
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
  assert.equal(suggestCount, 2);
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
  assert.deepEqual(calls, []);

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

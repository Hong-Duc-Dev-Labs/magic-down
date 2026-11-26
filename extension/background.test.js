const test = require("node:test");
const assert = require("node:assert/strict");

const {
  normalizeMediaUrl,
  getMediaDedupKey,
  inferQualityFromUrl,
  classifyMediaResponse,
  addDetectedVideo,
  getTabVideoList,
  tabVideos
} = require("./background.js");

function encodeEfg(obj) {
  return Buffer.from(JSON.stringify(obj), "utf8").toString("base64");
}

test("Facebook byte-range chunks collapse into one downloadable stream without bogus chunk sizes", () => {
  tabVideos.clear();
  const tabId = 42;
  const efgVideo = encodeEfg({ vencode_tag: "dash_vp9-basic-gen2_720p" });

  const chunks = [
    { host: "video-sin6-1.xx.fbcdn.net", start: 0, end: 360447, len: 360448, rid: "req1" },
    { host: "video-sin6-1.xx.fbcdn.net", start: 360448, end: 708607, len: 348160, rid: "req2" },
    { host: "video-sin6-2.xx.fbcdn.net", start: 708608, end: 1045503, len: 336896, rid: "req3" },
    { host: "video.fsgn5-1.fna.fbcdn.net", start: 1045504, end: 1317887, len: 272384, rid: "req4" }
  ];

  for (const c of chunks) {
    const url =
      `https://${c.host}/v/t42.1790-2/123456789_987654321_n.mp4` +
      `?efg=${encodeURIComponent(efgVideo)}&_nc_ht=${c.host}&oh=00_AfToken&oe=6789ABCD` +
      `&bytestart=${c.start}&byteend=${c.end}&_nc_rid=${c.rid}`;
    const media = classifyMediaResponse(url, [
      { name: "content-type", value: "video/mp4" },
      { name: "content-length", value: String(c.len) }
    ]);
    assert.ok(media, "video chunk should classify as a media candidate");
    assert.equal(media.size, 0, "chunk content-length must not be stored as full video size");
    assert.equal(media.quality, "720p");
    assert.ok(!media.url.includes("bytestart="), "bytestart must be stripped from download URL");
    assert.ok(!media.url.includes("byteend="), "byteend must be stripped from download URL");
    assert.ok(media.url.includes("oh=00_AfToken"), "CDN auth params must be preserved");
    addDetectedVideo(tabId, {
      url: media.url,
      quality: media.quality,
      height: media.height,
      format: media.format,
      size: media.size,
      isHLS: false
    });
  }

  const list = getTabVideoList(tabId);
  assert.equal(list.length, 1, "all chunks for the same Facebook video must collapse to 1 entry");
  assert.equal(list[0].quality, "720p");
  assert.equal(list[0].size, 0);
});

test("Facebook audio-only DASH track in efg is filtered out", () => {
  const efgAudio = encodeEfg({ vencode_tag: "dash_ln_heaac_vbr3_audio" });
  const url =
    `https://video-sin6-1.xx.fbcdn.net/v/t42.1790-2/111111_222222_n.mp4` +
    `?efg=${encodeURIComponent(efgAudio)}&bytestart=0&byteend=65535`;
  const media = classifyMediaResponse(url, [
    { name: "content-type", value: "video/mp4" },
    { name: "content-length", value: "65536" }
  ]);
  assert.equal(media, null);
});

test("Generic DASH range/bytes params and ephemeral query params are normalized and deduplicated", () => {
  tabVideos.clear();
  const tabId = 7;

  const u1 = "https://cdn.example.com/media/clip.mp4?token=abc&range=0-352000&rn=1&rbuf=0";
  const u2 = "https://cdn.example.com/media/clip.mp4?rn=2&range=352001-704000&token=abc&rbuf=120";
  const u3 = "https://cdn.example.com/media/clip.mp4?token=abc&bytes=704001-1024000&sq=3";

  assert.equal(getMediaDedupKey(u1), getMediaDedupKey(u2));
  assert.equal(getMediaDedupKey(u2), getMediaDedupKey(u3));

  for (const raw of [u1, u2, u3]) {
    const norm = normalizeMediaUrl(raw);
    assert.equal(norm.strippedRange, true);
    assert.ok(!norm.url.includes("range=") && !norm.url.includes("bytes="));
    const media = classifyMediaResponse(raw, [
      { name: "content-type", value: "video/mp4" },
      { name: "content-length", value: "352000" }
    ]);
    assert.ok(media);
    assert.equal(media.size, 0);
    addDetectedVideo(tabId, media);
  }

  assert.equal(getTabVideoList(tabId).length, 1);
});

test("Individual DASH segment files (.m4s, .cmfv, video/iso.segment) are rejected", () => {
  assert.equal(
    classifyMediaResponse("https://cdn.example.com/dash/seg_001.m4s", [
      { name: "content-type", value: "video/mp4" },
      { name: "content-length", value: "350000" }
    ]),
    null
  );
  assert.equal(
    classifyMediaResponse("https://cdn.example.com/dash/chunk_12.cmfv", [
      { name: "content-type", value: "video/mp4" },
      { name: "content-length", value: "400000" }
    ]),
    null
  );
  assert.equal(
    classifyMediaResponse("https://cdn.example.com/dash/segment?id=5", [
      { name: "content-type", value: "video/iso.segment" },
      { name: "content-length", value: "400000" }
    ]),
    null
  );
});

test("Progressive MP4 with full Content-Length or Content-Range total preserves full size", () => {
  const media = classifyMediaResponse("https://cdn.example.com/videos/demo_1080p.mp4", [
    { name: "content-type", value: "video/mp4" },
    { name: "content-range", value: "bytes 0-524287/15728640" },
    { name: "content-length", value: "524288" }
  ]);
  assert.ok(media);
  assert.equal(media.size, 15728640);
  assert.equal(media.quality, "1080p");
});

test("Weibo URL quality inference handles mp4_1080p, mp4_720p, mp4_hd, mp4_ld, mp4_2k, mp4_4k and template", () => {
  assert.deepEqual(
    inferQualityFromUrl("https://f.video.weibocdn.com/o0/abc.mp4?label=mp4_1080p&template=1080x1920.24.0"),
    { quality: "1080p", height: 1080 }
  );
  assert.deepEqual(
    inferQualityFromUrl("https://f.video.weibocdn.com/o0/abc.mp4?label=mp4_hd"),
    { quality: "720p", height: 720 }
  );
  assert.deepEqual(
    inferQualityFromUrl("https://f.video.weibocdn.com/o0/abc.mp4?label=mp4_ld"),
    { quality: "360p", height: 360 }
  );
  assert.deepEqual(
    inferQualityFromUrl("https://f.video.weibocdn.com/o0/abc.mp4?label=mp4_2k"),
    { quality: "1440p", height: 1440 }
  );
  assert.deepEqual(
    inferQualityFromUrl("https://f.video.weibocdn.com/o0/abc.mp4?label=mp4_4k"),
    { quality: "2160p", height: 2160 }
  );
  assert.deepEqual(
    inferQualityFromUrl("https://f.video.weibocdn.com/o0/abc.mp4?template=720x1280.24.0"),
    { quality: "720p", height: 720 }
  );
});

test("Facebook adaptive q20..q90 and progressive C3 efg tags infer resolutions, bitrate, and fbVideoId", () => {
  const cases = [
    { tag: "dash_r2av1-r1gen2vp9-adv_q90", bitrate: 2874817, expectedQ: "1080p", expectedH: 1080 },
    { tag: "dash_r2av1-r1gen2vp9-adv_q70", bitrate: 1694688, expectedQ: "720p", expectedH: 720 },
    { tag: "dash_r2av1-r1gen2vp9-adv_q50", bitrate: 903355, expectedQ: "540p", expectedH: 540 },
    { tag: "dash_r2av1-r1gen2vp9-adv_q40", bitrate: 583077, expectedQ: "480p", expectedH: 480 },
    { tag: "dash_r2av1-r1gen2vp9-adv_q30", bitrate: 526697, expectedQ: "360p", expectedH: 360 },
    { tag: "dash_r2av1-r1gen2vp9-adv_q20", bitrate: 342903, expectedQ: "240p", expectedH: 240 },
    { tag: "xpv_progressive.FACEBOOK..C3.360.sve_sd", bitrate: 0, expectedQ: "360p", expectedH: 360 }
  ];

  for (const c of cases) {
    const efg = encodeEfg({
      vencode_tag: c.tag,
      video_id: 1810888224374101,
      xpv_asset_id: 2128231754475728,
      duration_s: 18,
      bitrate: c.bitrate
    });
    const url = `https://scontent.fhan15-1.fna.fbcdn.net/o1/v/t2/f2/m367/AQTest.mp4?efg=${encodeURIComponent(efg)}&bytestart=0&byteend=327679`;
    assert.deepEqual(inferQualityFromUrl(url), { quality: c.expectedQ, height: c.expectedH });
    const media = classifyMediaResponse(url, [
      { name: "content-type", value: "video/mp4" },
      { name: "content-length", value: "327680" }
    ]);
    assert.ok(media, `expected media for ${c.tag}`);
    assert.equal(media.quality, c.expectedQ);
    assert.equal(media.height, c.expectedH);
    assert.equal(media.fbVideoId, "1810888224374101");
    if (c.bitrate > 0) assert.equal(media.bandwidth, c.bitrate);
  }
});

test("Facebook DASH preload chunk without bytestart suppresses bogus partial content-length", () => {
  // 2.87 Mbps * 18s / 8 = ~6.46 MB full stream; a 336 KB preload response must not report 336 KB as full size.
  const efg = encodeEfg({
    vencode_tag: "dash_r2av1-r1gen2vp9-adv_q90",
    video_id: 1810888224374101,
    duration_s: 18,
    bitrate: 2874817
  });
  const url = `https://scontent.fhan15-1.fna.fbcdn.net/o1/v/t2/f2/m367/AQPreload.mp4?efg=${encodeURIComponent(efg)}`;
  const media = classifyMediaResponse(url, [
    { name: "content-type", value: "video/mp4" },
    { name: "content-length", value: String(336896) }
  ]);
  assert.ok(media);
  assert.equal(media.quality, "1080p");
  assert.equal(media.size, 0, "partial preload chunk size should be suppressed");
  assert.equal(media.bandwidth, 2874817);
  assert.equal(media.fbVideoId, "1810888224374101");
});

test("Facebook DASH video pairs with matching fbVideoId audio track and builds audio_url payload", () => {
  const {
    classifyFacebookAudioTrack,
    addDetectedFacebookAudio,
    buildDownloadPayload,
    tabFbAudios
  } = require("./background.js");

  const tabId = 777;
  tabVideos.delete(tabId);
  tabFbAudios.delete(tabId);

  const videoEfg = encodeEfg({
    vencode_tag: "dash_r2av1-r1gen2vp9-adv_q90",
    video_id: 999888777,
    duration_s: 20,
    bitrate: 3000000
  });
  const audioEfg = encodeEfg({
    vencode_tag: "dash_ln_heaac_vbr3_audio",
    video_id: 999888777,
    bitrate: 128000
  });

  const videoUrl = `https://video.fhan15-1.fna.fbcdn.net/v/t42/fb_v1080.mp4?efg=${encodeURIComponent(videoEfg)}&bytestart=0&byteend=65535`;
  const audioUrl = `https://video.fhan15-1.fna.fbcdn.net/v/t42/fb_a128.mp4?efg=${encodeURIComponent(audioEfg)}&bytestart=0&byteend=16383`;

  const videoMedia = classifyMediaResponse(videoUrl, [
    { name: "content-type", value: "video/mp4" },
    { name: "content-range", value: "bytes 0-65535/5000000" }
  ]);
  assert.ok(videoMedia);
  assert.equal(videoMedia.isFbDash, true);

  addDetectedVideo(tabId, {
    url: videoMedia.url,
    quality: videoMedia.quality,
    height: videoMedia.height,
    bandwidth: videoMedia.bandwidth,
    fbVideoId: videoMedia.fbVideoId,
    isFbDash: videoMedia.isFbDash,
    format: videoMedia.format,
    size: videoMedia.size,
    isHLS: false
  });

  const audioTrack = classifyFacebookAudioTrack(audioUrl, [
    { name: "content-range", value: "bytes 0-16383/500000" }
  ]);
  assert.ok(audioTrack);
  assert.equal(audioTrack.fbVideoId, "999888777");
  addDetectedFacebookAudio(tabId, audioTrack);

  const list = getTabVideoList(tabId);
  assert.equal(list.length, 1);
  assert.equal(list[0].streamType, "muxed");
  assert.equal(list[0].hasAudio, true);
  assert.equal(list[0].audioUrl, audioTrack.url);
  assert.equal(list[0].size, 5500000);

  const payload = buildDownloadPayload({
    url: list[0].url,
    audioUrl: list[0].audioUrl,
    filename: "facebook_1080p.mp4",
    size: list[0].size
  });
  assert.equal(payload.action, "download");
  assert.equal(payload.audio_url, audioTrack.url);
  assert.equal(payload.total_bytes, 5500000);

  tabVideos.delete(tabId);
  tabFbAudios.delete(tabId);
});

test("buildDownloadPayload forwards target_format, output_format, and audio_bitrate", () => {
  const { buildDownloadPayload } = require("./background.js");
  const mp4Payload = buildDownloadPayload({
    url: "https://rr1.googlevideo.com/videoplayback?itag=313",
    audioUrl: "https://rr1.googlevideo.com/videoplayback?itag=251",
    filename: "video_4k.mp4",
    targetFormat: "MP4"
  });
  assert.equal(mp4Payload.target_format, "mp4");
  assert.equal(mp4Payload.audio_url, "https://rr1.googlevideo.com/videoplayback?itag=251");

  const mp3Payload = buildDownloadPayload({
    url: "https://rr1.googlevideo.com/videoplayback?itag=251",
    filename: "song.mp3",
    outputFormat: "MP3",
    audioBitrate: "320K"
  });
  assert.equal(mp3Payload.output_format, "mp3");
  assert.equal(mp3Payload.audio_bitrate, "320k");
  assert.equal(mp3Payload.filename, "song.mp3");
});

test("fetchManifestText blocks forged SSRF/CSRF targets and private/local hosts", async () => {
  const fs = require("node:fs");
  const path = require("node:path");
  const {
    isPrivateOrLocalHost,
    isAllowedManifestFetch,
    recordObservedTabUrl,
    fetchManifestText,
    tabObservedUrls
  } = require("./background.js");

  tabObservedUrls.clear();
  const evilSender = {
    url: "https://evil.example/page.html",
    tab: { id: 91, url: "https://evil.example/page.html" }
  };

  // Private, loopback, metadata, and internal hosts must be rejected.
  const blockedHosts = [
    "localhost",
    "sub.localhost",
    "127.0.0.1",
    "10.0.0.1",
    "172.16.0.1",
    "192.168.1.1",
    "169.254.169.254",
    "0.0.0.0",
    "::1",
    "[::1]",
    "::ffff:127.0.0.1",
    "fc00::1",
    "fe80::1",
    "intranet",
    "api.internal",
    "printer.local"
  ];
  for (const h of blockedHosts) {
    assert.equal(isPrivateOrLocalHost(h), true, `expected ${h} to be classified as private/local`);
  }

  const ssrfUrls = [
    "http://127.0.0.1:28888/health",
    "http://localhost:8080/secret.m3u8",
    "http://169.254.169.254/latest/meta-data/",
    "http://10.0.0.5/internal.mpd",
    "http://192.168.1.1/router.m3u8",
    "https://bank.example/account/private.m3u8",
    "file:///C:/Windows/win.ini",
    "javascript:alert(1)"
  ];
  for (const u of ssrfUrls) {
    const res = await fetchManifestText(u, evilSender);
    assert.equal(res.ok, false, `expected fetchManifestText to block ${u}`);
  }

  // Missing sender.tab must be rejected.
  assert.equal(isAllowedManifestFetch("https://evil.example/stream.m3u8", null).ok, false);
  assert.equal(isAllowedManifestFetch("https://evil.example/stream.m3u8", {}).ok, false);

  // Same-site public manifest is allowed.
  assert.equal(isAllowedManifestFetch("https://cdn.evil.example/live.m3u8", evilSender).ok, true);

  // Cross-origin CDN manifest genuinely observed by chrome.webRequest on that tab is allowed.
  const cdnUrl = "https://edge-cdn.mediahost.net/hls/master.m3u8";
  assert.equal(isAllowedManifestFetch(cdnUrl, evilSender).ok, false, "unobserved cross-origin URL must be blocked");
  recordObservedTabUrl(91, cdnUrl);
  assert.equal(isAllowedManifestFetch(cdnUrl, evilSender).ok, true, "webRequest-observed URL should be allowed");

  // YouTube HLS manifest from googlevideo.com on a youtube.com tab is allowed.
  const ytSender = {
    url: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
    tab: { id: 92, url: "https://www.youtube.com/watch?v=dQw4w9WgXcQ" }
  };
  assert.equal(
    isAllowedManifestFetch("https://manifest.googlevideo.com/api/manifest/hls_variant/test.m3u8", ytSender).ok,
    true
  );

  // Verify content.js uses closed Shadow DOM so page scripts cannot inspect overlay data.
  const contentSrc = fs.readFileSync(path.join(__dirname, "content.js"), "utf8");
  assert.ok(contentSrc.includes('attachShadow({ mode: "closed" })'), "content.js must use closed Shadow DOM");
  assert.ok(!contentSrc.includes('attachShadow({ mode: "open" })'), "content.js must not use open Shadow DOM");

  tabObservedUrls.clear();
});

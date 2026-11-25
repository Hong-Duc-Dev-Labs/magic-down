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

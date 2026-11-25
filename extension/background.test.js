const test = require("node:test");
const assert = require("node:assert/strict");

const {
  normalizeMediaUrl,
  getMediaDedupKey,
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

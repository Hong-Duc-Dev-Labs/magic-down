"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const bili = require("./bilibili.js");
const bg = require("./background.js");

test("isBilibiliHost and isBilibiliCDNHost recognize Bilibili domains and reject others", () => {
  assert.equal(bili.isBilibiliHost("www.bilibili.com"), true);
  assert.equal(bili.isBilibiliHost("m.bilibili.com"), true);
  assert.equal(bili.isBilibiliHost("bilibili.com"), true);
  assert.equal(bili.isBilibiliHost("space.bilibili.com"), true);
  assert.equal(bili.isBilibiliHost("notbilibili.com"), false);
  assert.equal(bili.isBilibiliHost("youtube.com"), false);

  assert.equal(bili.isBilibiliCDNHost("upos-sz-mirrorcos.bilivideo.com"), true);
  assert.equal(bili.isBilibiliCDNHost("xy123x45x67x89xy.mcdn.bilivideo.cn"), true);
  assert.equal(bili.isBilibiliCDNHost("bilivideo.com"), true);
  assert.equal(bili.isBilibiliCDNHost("bilivideo.cn"), true);
  assert.equal(bili.isBilibiliCDNHost("example.com"), false);
});

test("isBilibiliPlayUrlEndpoint matches regular, wbi, and Bangumi playurl APIs", () => {
  assert.equal(bili.isBilibiliPlayUrlEndpoint("https://api.bilibili.com/x/player/wbi/playurl?bvid=BV1xx411c7mD&cid=123"), true);
  assert.equal(bili.isBilibiliPlayUrlEndpoint("https://api.bilibili.com/x/player/playurl?bvid=BV1xx411c7mD&cid=123"), true);
  assert.equal(bili.isBilibiliPlayUrlEndpoint("https://api.bilibili.com/pgc/player/web/playurl?ep_id=12345"), true);
  assert.equal(bili.isBilibiliPlayUrlEndpoint("https://api.bilibili.com/pgc/player/web/v2/playurl?ep_id=12345"), true);
  assert.equal(bili.isBilibiliPlayUrlEndpoint("https://api.bilibili.com/x/web-interface/view?bvid=BV1xx411c7mD"), false);
});

test("extractBilibiliVideoId handles BV, multi-part ?p=, av, Bangumi ep/ss, and festival URLs", () => {
  assert.equal(bili.extractBilibiliVideoId("https://www.bilibili.com/video/BV1xx411c7mD/"), "BV1xx411c7mD");
  assert.equal(bili.extractBilibiliVideoId("https://www.bilibili.com/video/BV1xx411c7mD/?p=1"), "BV1xx411c7mD");
  assert.equal(bili.extractBilibiliVideoId("https://www.bilibili.com/video/BV1xx411c7mD/?p=3"), "BV1xx411c7mD_p3");
  assert.equal(bili.extractBilibiliVideoId("https://www.bilibili.com/video/av170001"), "av170001");
  assert.equal(bili.extractBilibiliVideoId("https://www.bilibili.com/bangumi/play/ep123456"), "ep123456");
  assert.equal(bili.extractBilibiliVideoId("https://www.bilibili.com/bangumi/play/ss45678"), "ss45678");
  assert.equal(bili.extractBilibiliVideoId("https://www.bilibili.com/festival/demo?bvid=BV1ab411c7mD"), "BV1ab411c7mD");
  assert.equal(bili.extractBilibiliVideoId("https://www.bilibili.com/"), "");
  assert.equal(bili.extractBilibiliVideoId("https://example.com/video/BV1xx411c7mD"), "");
});

test("parseBilibiliPlayInfo extracts DASH resolutions, prefers AVC per tier, pairs best audio, and attaches headers", () => {
  const pageUrl = "https://www.bilibili.com/video/BV1xx411c7mD/";
  const ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36";

  const parsed = bili.parseBilibiliPlayInfo(
    {
      code: 0,
      data: {
        timelength: 120000,
        dash: {
          duration: 120,
          video: [
            {
              id: 120,
              baseUrl: "https://upos-sz-mirrorcos.bilivideo.com/v_4k_hevc.m4s",
              bandwidth: 12000000,
              width: 3840,
              height: 2160,
              frameRate: "30",
              codecid: 12,
              codecs: "hev1.1.6.L150.90"
            },
            {
              id: 116,
              baseUrl: "https://upos-sz-mirrorcos.bilivideo.com/v_1080p60_avc.m4s",
              bandwidth: 6000000,
              width: 1920,
              height: 1080,
              frameRate: "60",
              codecid: 7,
              codecs: "avc1.640032"
            },
            {
              id: 80,
              baseUrl: "https://upos-sz-mirrorcos.bilivideo.com/v_1080p_hevc.m4s",
              bandwidth: 4500000,
              width: 1920,
              height: 1080,
              frameRate: "30",
              codecid: 12,
              codecs: "hev1.1.6.L120.90"
            },
            {
              id: 80,
              baseUrl: "https://upos-sz-mirrorcos.bilivideo.com/v_1080p_avc.m4s",
              bandwidth: 3800000,
              width: 1920,
              height: 1080,
              frameRate: "30",
              codecid: 7,
              codecs: "avc1.640028"
            },
            {
              id: 64,
              baseUrl: "https://upos-sz-mirrorcos.bilivideo.com/v_720p.m4s",
              bandwidth: 2000000,
              width: 1280,
              height: 720,
              frameRate: "30",
              codecid: 7
            },
            {
              id: 32,
              baseUrl: "https://upos-sz-mirrorcos.bilivideo.com/v_480p.m4s",
              bandwidth: 1000000,
              width: 854,
              height: 480,
              frameRate: "30",
              codecid: 7
            },
            {
              id: 16,
              baseUrl: "https://upos-sz-mirrorcos.bilivideo.com/v_360p.m4s",
              bandwidth: 500000,
              width: 640,
              height: 360,
              frameRate: "30",
              codecid: 7
            }
          ],
          audio: [
            {
              id: 30216,
              baseUrl: "https://upos-sz-mirrorcos.bilivideo.com/a_64k.m4s",
              bandwidth: 64000
            },
            {
              id: 30280,
              baseUrl: "https://upos-sz-mirrorcos.bilivideo.com/a_192k.m4s",
              bandwidth: 192000
            }
          ]
        }
      }
    },
    { pageUrl, title: "Test Bilibili Video", userAgent: ua }
  );

  assert.ok(parsed);
  assert.equal(parsed.videoId, "BV1xx411c7mD");
  assert.equal(parsed.title, "Test Bilibili Video");

  // 6 deduplicated video tiers (4K, 1080p60, 1080p, 720p, 480p, 360p) + 1 best audio-only
  assert.equal(parsed.variants.length, 7);
  assert.deepEqual(
    parsed.variants.slice(0, 6).map((v) => v.quality),
    ["4K", "1080p60", "1080p", "720p", "480p", "360p"]
  );

  // 1080p tier should prefer AVC (codecid 7) over HEVC
  const v1080 = parsed.variants[2];
  assert.equal(v1080.url, "https://upos-sz-mirrorcos.bilivideo.com/v_1080p_avc.m4s");
  assert.equal(v1080.audioUrl, "https://upos-sz-mirrorcos.bilivideo.com/a_192k.m4s");
  assert.equal(v1080.streamType, "muxed");
  assert.equal(v1080.hasAudio, true);
  assert.equal(v1080.needsMux, true);
  assert.equal(v1080.format, "MP4");
  assert.equal(v1080.ext, "mp4");
  assert.equal(v1080.referrer, pageUrl);
  assert.equal(v1080.headers.Referer, pageUrl);
  assert.equal(v1080.headers.Origin, "https://www.bilibili.com");
  assert.equal(v1080.headers["User-Agent"], ua);

  // Audio-only track at the end
  const audioOnly = parsed.variants[6];
  assert.equal(audioOnly.streamType, "audio-only");
  assert.equal(audioOnly.url, "https://upos-sz-mirrorcos.bilivideo.com/a_192k.m4s");
  assert.equal(audioOnly.ext, "m4a");
  assert.equal(audioOnly.headers.Referer, pageUrl);
});

test("parseBilibiliPlayInfo supports Bangumi PGC result envelope", () => {
  const parsed = bili.parseBilibiliPlayInfo(
    {
      code: 0,
      result: {
        dash: {
          duration: 1440,
          video: [
            {
              id: 80,
              base_url: "https://upos-sz-mirrorhw.bilivideo.com/ep_1080p.m4s",
              bandwidth: 4000000,
              width: 1920,
              height: 1080,
              codecid: 7
            }
          ],
          audio: [
            {
              id: 30280,
              base_url: "https://upos-sz-mirrorhw.bilivideo.com/ep_audio.m4s",
              bandwidth: 192000
            }
          ]
        }
      }
    },
    { pageUrl: "https://www.bilibili.com/bangumi/play/ep123456" }
  );

  assert.ok(parsed);
  assert.equal(parsed.videoId, "ep123456");
  assert.equal(parsed.variants.length, 2);
  assert.equal(parsed.variants[0].quality, "1080p");
  assert.equal(parsed.variants[0].streamType, "muxed");
  assert.equal(parsed.variants[0].audioUrl, "https://upos-sz-mirrorhw.bilivideo.com/ep_audio.m4s");
});

test("parseBilibiliPlayInfo falls back to durl (FLV/MP4) when DASH is absent", () => {
  const flvParsed = bili.parseBilibiliPlayInfo(
    {
      code: 0,
      data: {
        quality: 64,
        format: "flv720",
        durl: [
          {
            order: 1,
            length: 60000,
            size: 15000000,
            url: "https://upos-sz-mirrorcos.bilivideo.com/video_720p.flv?token=abc"
          }
        ]
      }
    },
    { pageUrl: "https://www.bilibili.com/video/BV1xx411c7mD/" }
  );

  assert.ok(flvParsed);
  assert.equal(flvParsed.variants.length, 1);
  assert.equal(flvParsed.variants[0].format, "FLV");
  assert.equal(flvParsed.variants[0].ext, "flv");
  assert.equal(flvParsed.variants[0].quality, "720p");
  assert.equal(flvParsed.variants[0].streamType, "progressive");
  assert.equal(flvParsed.variants[0].size, 15000000);
  assert.equal(flvParsed.variants[0].headers.Referer, "https://www.bilibili.com/video/BV1xx411c7mD/");
});

test("background classifyMediaResponse filters out Bilibili bilivideo.com, bilivideo.cn, and .m4s chunks", () => {
  const m4sCom = "https://upos-sz-mirrorcos.bilivideo.com/upgcxcode/12/34/1234-1-30080.m4s?e=1700000000";
  const mp4Cn = "https://xy123x45x67x89xy.mcdn.bilivideo.cn:4483/v1/resource/1234.mp4?range=0-65535";

  assert.equal(bg.isBilibiliMediaChunk(m4sCom), true);
  assert.equal(bg.isBilibiliMediaChunk(mp4Cn), true);
  assert.equal(
    bg.classifyMediaResponse(m4sCom, [
      { name: "content-type", value: "video/mp4" },
      { name: "content-length", value: "500000" }
    ]),
    null
  );
  assert.equal(
    bg.classifyMediaResponse(mp4Cn, [
      { name: "content-type", value: "video/mp4" },
      { name: "content-length", value: "500000" }
    ]),
    null
  );
});

test("background buildDownloadPayload preserves Bilibili Referer, User-Agent, and audio_url", () => {
  const payload = bg.buildDownloadPayload({
    url: "https://upos-sz-mirrorcos.bilivideo.com/v_1080p.m4s",
    audioUrl: "https://upos-sz-mirrorcos.bilivideo.com/a_192k.m4s",
    filename: "Demo.mp4",
    referrer: "https://www.bilibili.com/video/BV1xx411c7mD/",
    headers: {
      Referer: "https://www.bilibili.com/video/BV1xx411c7mD/",
      Origin: "https://www.bilibili.com",
      "User-Agent": "TestUA/1.0"
    },
    size: 25000000
  });

  assert.equal(payload.action, "download");
  assert.equal(payload.audio_url, "https://upos-sz-mirrorcos.bilivideo.com/a_192k.m4s");
  assert.equal(payload.referrer, "https://www.bilibili.com/video/BV1xx411c7mD/");
  assert.equal(payload.headers.Referer, "https://www.bilibili.com/video/BV1xx411c7mD/");
  assert.equal(payload.headers.Origin, "https://www.bilibili.com");
  assert.equal(payload.headers["User-Agent"], "TestUA/1.0");
  assert.equal(payload.total_bytes, 25000000);
});

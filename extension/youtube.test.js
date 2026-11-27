"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const yt = require("./youtube.js");

test("isYouTubeHost recognizes YouTube domains and rejects others", () => {
  assert.equal(yt.isYouTubeHost("www.youtube.com"), true);
  assert.equal(yt.isYouTubeHost("m.youtube.com"), true);
  assert.equal(yt.isYouTubeHost("music.youtube.com"), true);
  assert.equal(yt.isYouTubeHost("youtube-nocookie.com"), true);
  assert.equal(yt.isYouTubeHost("www.youtube-nocookie.com"), true);
  assert.equal(yt.isYouTubeHost("notyoutube.com"), false);
  assert.equal(yt.isYouTubeHost("example.com"), false);
  assert.equal(yt.isYouTubeHost(""), false);
});

test("extractYouTubeVideoId handles watch, shorts, embed, live, and youtu.be URLs", () => {
  assert.equal(yt.extractYouTubeVideoId("https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s"), "dQw4w9WgXcQ");
  assert.equal(yt.extractYouTubeVideoId("https://www.youtube.com/shorts/abcDEF12345"), "abcDEF12345");
  assert.equal(yt.extractYouTubeVideoId("https://www.youtube-nocookie.com/embed/abcDEF12345?autoplay=1"), "abcDEF12345");
  assert.equal(yt.extractYouTubeVideoId("https://www.youtube.com/live/LiveID_1234"), "LiveID_1234");
  assert.equal(yt.extractYouTubeVideoId("https://youtu.be/dQw4w9WgXcQ?si=xyz"), "dQw4w9WgXcQ");
  assert.equal(yt.extractYouTubeVideoId("https://www.youtube.com/"), "");
  assert.equal(yt.extractYouTubeVideoId("https://example.com/watch?v=dQw4w9WgXcQ"), "");
});

test("isYouTubeRangeChunkURL identifies googlevideo MSE chunks vs full streams", () => {
  const chunkRange = "https://rr3---sn-abc.googlevideo.com/videoplayback?expire=1700000000&itag=137&range=0-65535";
  const chunkUmp = "https://rr3---sn-abc.googlevideo.com/videoplayback?expire=1700000000&itag=248&ump=1";
  const chunkRbuf = "https://rr3---sn-abc.googlevideo.com/videoplayback?expire=1700000000&itag=140&rbuf=1000";
  const fullStream = "https://rr3---sn-abc.googlevideo.com/videoplayback?expire=1700000000&itag=18&mime=video%2Fmp4";
  const otherHost = "https://cdn.example.com/videoplayback?range=0-100";

  assert.equal(yt.isGoogleVideoPlaybackURL(chunkRange), true);
  assert.equal(yt.isGoogleVideoPlaybackURL(fullStream), true);
  assert.equal(yt.isGoogleVideoPlaybackURL(otherHost), false);

  assert.equal(yt.isYouTubeRangeChunkURL(chunkRange), true);
  assert.equal(yt.isYouTubeRangeChunkURL(chunkUmp), true);
  assert.equal(yt.isYouTubeRangeChunkURL(chunkRbuf), true);
  assert.equal(yt.isYouTubeRangeChunkURL(fullStream), false);
  assert.equal(yt.isYouTubeRangeChunkURL(otherHost), false);
});

test("parseYouTubePlayerPayload extracts, deduplicates, and sorts progressive, adaptive video, and audio", () => {
  const parsed = yt.parseYouTubePlayerPayload({
    videoId: "dQw4w9WgXcQ",
    title: "Test Video",
    isLive: false,
    hlsManifestUrl: "",
    dashManifestUrl: "",
    formats: [
      {
        itag: 18,
        url: "https://rr1.googlevideo.com/videoplayback?itag=18",
        mimeType: 'video/mp4; codecs="avc1.42001E, mp4a.40.2"',
        qualityLabel: "360p",
        width: 640,
        height: 360,
        fps: 30,
        bitrate: 600000,
        contentLength: 12000000
      }
    ],
    adaptiveFormats: [
      {
        itag: 137,
        url: "https://rr1.googlevideo.com/videoplayback?itag=137",
        mimeType: 'video/mp4; codecs="avc1.640028"',
        qualityLabel: "1080p",
        width: 1920,
        height: 1080,
        fps: 30,
        bitrate: 3500000,
        contentLength: 80000000
      },
      {
        itag: 399,
        url: "https://rr1.googlevideo.com/videoplayback?itag=399",
        mimeType: 'video/mp4; codecs="av01.0.08M.08"',
        qualityLabel: "1080p",
        width: 1920,
        height: 1080,
        fps: 30,
        bitrate: 2100000,
        contentLength: 50000000
      },
      {
        itag: 248,
        url: "https://rr1.googlevideo.com/videoplayback?itag=248",
        mimeType: 'video/webm; codecs="vp9"',
        qualityLabel: "1080p",
        width: 1920,
        height: 1080,
        fps: 30,
        bitrate: 2500000,
        contentLength: 60000000
      },
      {
        itag: 140,
        url: "https://rr1.googlevideo.com/videoplayback?itag=140",
        mimeType: 'audio/mp4; codecs="mp4a.40.2"',
        bitrate: 130000,
        contentLength: 3000000
      },
      {
        itag: 251,
        url: "https://rr1.googlevideo.com/videoplayback?itag=251",
        mimeType: 'audio/webm; codecs="opus"',
        bitrate: 160000,
        contentLength: 3500000
      }
    ]
  });

  assert.equal(parsed.videoId, "dQw4w9WgXcQ");
  assert.equal(parsed.title, "Test Video");
  assert.equal(parsed.cipherOnlyCount, 0);
  assert.equal(parsed.drmDetected, false);

  // 1 progressive + 2 deduplicated 1080p (paired with best M4A audio for auto-mux) + 2 audio (WebM + M4A)
  assert.equal(parsed.variants.length, 5);
  assert.equal(parsed.variants[0].streamType, "progressive");
  assert.equal(parsed.variants[0].itag, 18);
  assert.equal(parsed.variants[1].streamType, "muxed");
  assert.equal(parsed.variants[1].itag, 137); // MP4 before WebM, highest bitrate kept
  assert.equal(parsed.variants[1].audioUrl, "https://rr1.googlevideo.com/videoplayback?itag=140");
  assert.equal(parsed.variants[1].hasAudio, true);
  assert.equal(parsed.variants[1].format, "MP4");
  assert.equal(parsed.variants[1].size, 83000000); // 80MB video + 3MB M4A audio
  assert.equal(parsed.variants[2].streamType, "muxed");
  assert.equal(parsed.variants[2].itag, 248); // WebM paired with M4A for MP4 mux
  assert.equal(parsed.variants[2].audioUrl, "https://rr1.googlevideo.com/videoplayback?itag=140");
  assert.equal(parsed.variants[3].streamType, "audio-only");
  assert.equal(parsed.variants[3].itag, 251); // 160kbps WebM
  assert.equal(parsed.variants[4].streamType, "audio-only");
  assert.equal(parsed.variants[4].itag, 140); // 130kbps M4A
  assert.equal(parsed.variants[4].ext, "m4a");
});

test("parseYouTubePlayerPayload tracks signatureCipher and DRM formats cleanly", () => {
  const parsed = yt.parseYouTubePlayerPayload({
    videoId: "cipherVid01",
    title: "Ciphered Video",
    formats: [
      { itag: 18, url: "", hasCipher: true, mimeType: "video/mp4", height: 360 }
    ],
    adaptiveFormats: [
      { itag: 137, url: "", hasCipher: true, mimeType: "video/mp4", height: 1080 },
      { itag: 248, url: "https://rr1.googlevideo.com/videoplayback?itag=248", isDrm: true, mimeType: "video/webm", height: 1080 }
    ]
  });

  assert.equal(parsed.variants.length, 0);
  assert.equal(parsed.cipherOnlyCount, 2);
  assert.equal(parsed.drmDetected, true);
});

test("parseYouTubePlayerPayload prefers H.264/avc1 MP4 over av01, pairs WebM fallback as MP4, and builds 3 MP3 variants", () => {
  const parsed = yt.parseYouTubePlayerPayload({
    videoId: "mp4Mp3Test1",
    title: "4K Music Video",
    adaptiveFormats: [
      {
        itag: 399,
        url: "https://rr1.googlevideo.com/videoplayback?itag=399",
        mimeType: 'video/mp4; codecs="av01.0.08M.08"',
        qualityLabel: "1080p",
        width: 1920,
        height: 1080,
        fps: 30,
        bitrate: 4500000,
        contentLength: 70000000
      },
      {
        itag: 137,
        url: "https://rr1.googlevideo.com/videoplayback?itag=137",
        mimeType: 'video/mp4; codecs="avc1.640028"',
        qualityLabel: "1080p",
        width: 1920,
        height: 1080,
        fps: 30,
        bitrate: 3200000,
        contentLength: 60000000
      },
      {
        itag: 313,
        url: "https://rr1.googlevideo.com/videoplayback?itag=313",
        mimeType: 'video/webm; codecs="vp9"',
        qualityLabel: "2160p",
        width: 3840,
        height: 2160,
        fps: 30,
        bitrate: 15000000,
        contentLength: 200000000
      },
      {
        itag: 251,
        url: "https://rr1.googlevideo.com/videoplayback?itag=251",
        mimeType: 'audio/webm; codecs="opus"',
        bitrate: 160000,
        contentLength: 4000000
      }
    ]
  });

  assert.ok(parsed);
  const muxed1080 = parsed.variants.find((v) => v.height === 1080 && v.streamType === "muxed");
  assert.ok(muxed1080);
  assert.equal(muxed1080.itag, 137); // avc1 preferred over higher-bitrate av01
  assert.equal(muxed1080.format, "MP4");
  assert.equal(muxed1080.ext, "mp4");
  assert.equal(muxed1080.targetFormat, "mp4");

  const muxed4k = parsed.variants.find((v) => v.height === 2160 && v.streamType === "muxed");
  assert.ok(muxed4k);
  assert.equal(muxed4k.itag, 313); // WebM-only 4K still outputs MP4 with targetFormat='mp4'
  assert.equal(muxed4k.format, "MP4");
  assert.equal(muxed4k.ext, "mp4");
  assert.equal(muxed4k.targetFormat, "mp4");
  assert.equal(muxed4k.audioUrl, "https://rr1.googlevideo.com/videoplayback?itag=251");

  assert.equal(parsed.mp3Variants.length, 3);
  assert.deepEqual(
    parsed.mp3Variants.map((m) => [m.quality, m.outputFormat, m.audioBitrate, m.ext]),
    [
      ["MP3 · 320 kbps", "mp3", "320k", "mp3"],
      ["MP3 · 192 kbps", "mp3", "192k", "mp3"],
      ["MP3 · 128 kbps", "mp3", "128k", "mp3"]
    ]
  );
  assert.equal(parsed.mp3Variants[0].url, "https://rr1.googlevideo.com/videoplayback?itag=251");
});

test("parseYouTubePlayerPayload detects SABR-only streamingData with zero plaintext URLs", () => {
  const parsed = yt.parseYouTubePlayerPayload({
    videoId: "jNQXAC9IVRw",
    title: "Me at the zoo",
    hasServerAbr: true,
    formats: [],
    adaptiveFormats: [
      { itag: 248, url: "", hasCipher: false, mimeType: 'video/webm; codecs="vp9"', height: 1080 }
    ]
  });
  assert.ok(parsed);
  assert.equal(parsed.sabrDetected, true);
  assert.equal(parsed.variants.length, 0);
  assert.equal(parsed.cipherOnlyCount, 0);
});

test("canonicalYouTubeUrl normalizes watch and shorts URLs cleanly", () => {
  assert.equal(
    yt.canonicalYouTubeUrl("https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=42s"),
    "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
  );
  assert.equal(
    yt.canonicalYouTubeUrl("https://www.youtube.com/shorts/abcDEF12345?feature=share"),
    "https://www.youtube.com/shorts/abcDEF12345"
  );
  assert.equal(yt.canonicalYouTubeUrl("https://www.youtube.com/", "jNQXAC9IVRw"), "https://www.youtube.com/watch?v=jNQXAC9IVRw");
});

test("resolveYouTubeAnchorRect falls back to #movie_player when YouTube shifts <video> to negative top", () => {
  const shiftedVideoRect = { top: -524, bottom: 68, left: 24, right: 813, width: 789, height: 592 };
  const playerRect = { top: 68, bottom: 549, left: 16, right: 872, width: 856, height: 481 };
  const chosen = yt.resolveYouTubeAnchorRect(shiftedVideoRect, playerRect, 1280, 800);
  assert.deepEqual(chosen, playerRect);

  // When <video> is aligned inside the player, keep videoRect
  const normalVideoRect = { top: 68, bottom: 549, left: 16, right: 872, width: 856, height: 481 };
  const chosenNormal = yt.resolveYouTubeAnchorRect(normalVideoRect, playerRect, 1280, 800);
  assert.deepEqual(chosenNormal, normalVideoRect);
});

test("normalizeResolvedYouTubeEntry builds MP4 and MP3 variants with ytdlpFormat", () => {
  const entry = yt.normalizeResolvedYouTubeEntry(
    {
      videoId: "aqz-KE-bpKQ",
      title: "Big Buck Bunny",
      thumbnail: "https://i.ytimg.com/vi/aqz-KE-bpKQ/maxresdefault.jpg",
      variants: [
        {
          url: "https://www.youtube.com/watch?v=aqz-KE-bpKQ",
          quality: "1080p60",
          height: 1080,
          fps: 60,
          size: 152000000,
          format: "MP4",
          ext: "mp4",
          ytdlpFormat: "bestvideo[height<=1080]+bestaudio/best"
        }
      ],
      mp3Variants: [
        {
          url: "https://www.youtube.com/watch?v=aqz-KE-bpKQ",
          quality: "MP3 · 320 kbps",
          audioBitrate: "320k",
          ytdlpFormat: "bestaudio/best"
        }
      ]
    },
    "aqz-KE-bpKQ",
    "Fallback"
  );
  assert.ok(entry);
  assert.equal(entry.resolvedViaYtdlp, true);
  assert.equal(entry.variants.length, 1);
  assert.equal(entry.variants[0].ytdlpFormat, "bestvideo[height<=1080]+bestaudio/best");
  assert.equal(entry.mp3Variants.length, 1);
  assert.equal(entry.mp3Variants[0].outputFormat, "mp3");
});

test("content.js avoids innerHTML (Trusted Types safe) and resets dismissedVideos on SPA navigation", () => {
  const fs = require("node:fs");
  const path = require("node:path");
  const src = fs.readFileSync(path.join(__dirname, "content.js"), "utf8");
  assert.equal(/\.innerHTML\s*=/.test(src), false, "content.js must not assign .innerHTML (Trusted Types CSP)");
  assert.match(src, /function handleNavigationChange\(\)[\s\S]{0,400}dismissedVideos\s*=\s*new WeakSet\(\)/);
});

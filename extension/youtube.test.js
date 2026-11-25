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

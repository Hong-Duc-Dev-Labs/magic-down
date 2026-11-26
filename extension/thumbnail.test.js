const test = require("node:test");
const assert = require("node:assert/strict");

const yt = require("./youtube.js");
const bili = require("./bilibili.js");
const thumb = require("./thumbnail.js");

test("YouTube extractYouTubeThumbnail picks highest-resolution thumbnail and detects format", () => {
  const result = yt.extractYouTubeThumbnail(
    [
      { url: "https://i.ytimg.com/vi/dQw4w9WgXcQ/default.jpg", width: 120, height: 90 },
      { url: "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg", width: 480, height: 360 },
      { url: "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg", width: 1280, height: 720 }
    ],
    "dQw4w9WgXcQ"
  );
  assert.equal(result.url, "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg");
  assert.equal(result.width, 1280);
  assert.equal(result.height, 720);
  assert.equal(result.format, "JPG");
  assert.equal(result.ext, "jpg");
  assert.equal(result.source, "youtube");
});

test("YouTube extractYouTubeThumbnail prefers non-letterboxed on equal area and falls back to maxresdefault by videoId", () => {
  const webpPref = yt.extractYouTubeThumbnail(
    [
      { url: "https://i.ytimg.com/vi/abc12345678/sddefault.jpg", width: 640, height: 480 },
      { url: "https://i.ytimg.com/vi_webp/abc12345678/hq720.webp", width: 640, height: 480 }
    ],
    "abc12345678"
  );
  assert.equal(webpPref.url, "https://i.ytimg.com/vi_webp/abc12345678/hq720.webp");
  assert.equal(webpPref.format, "WEBP");
  assert.equal(webpPref.ext, "webp");

  const fallback = yt.extractYouTubeThumbnail([], "dQw4w9WgXcQ");
  assert.equal(fallback.url, "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg");
  assert.equal(fallback.width, 1280);
  assert.equal(fallback.height, 720);

  const parsed = yt.parseYouTubePlayerPayload({
    videoId: "dQw4w9WgXcQ",
    title: "Test Video",
    formats: [{ itag: 18, url: "https://rr1.googlevideo.com/videoplayback?id=1", mimeType: "video/mp4", height: 360 }],
    thumbnails: [{ url: "//i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg", width: 1280, height: 720 }]
  });
  assert.equal(parsed.thumbnail.url, "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg");
});

test("Bilibili extractBilibiliThumbnail normalizes protocol-relative URLs and supports videoData/epInfo/mediaInfo/og:image", () => {
  const fromVideo = bili.extractBilibiliThumbnail({
    videoData: { pic: "//i0.hdslb.com/bfs/archive/abc123.jpg" }
  });
  assert.equal(fromVideo.url, "https://i0.hdslb.com/bfs/archive/abc123.jpg");
  assert.equal(fromVideo.format, "JPG");
  assert.equal(fromVideo.ext, "jpg");

  const fromEp = bili.extractBilibiliThumbnail({
    epInfo: { cover: "https://i0.hdslb.com/bfs/bangumi/ep1.png" }
  });
  assert.equal(fromEp.url, "https://i0.hdslb.com/bfs/bangumi/ep1.png");
  assert.equal(fromEp.format, "PNG");
  assert.equal(fromEp.ext, "png");

  const fromMedia = bili.extractBilibiliThumbnail({
    mediaInfo: { cover: "//i0.hdslb.com/bfs/bangumi/media.jpg@672w_378h.webp" }
  });
  assert.equal(fromMedia.url, "https://i0.hdslb.com/bfs/bangumi/media.jpg@672w_378h.webp");
  assert.equal(fromMedia.format, "WEBP");
  assert.equal(fromMedia.ext, "webp");

  const fromOg = bili.extractBilibiliThumbnail({}, "//i0.hdslb.com/bfs/archive/og-cover.jpg");
  assert.equal(fromOg.url, "https://i0.hdslb.com/bfs/archive/og-cover.jpg");
});

test("Weibo extractWeiboThumbnail prioritizes big_pic_info.pic_big and falls back to inherited page_pic", () => {
  const bigPic = thumb.extractWeiboThumbnail(
    {
      big_pic_info: {
        pic_big: { url: "https://wx1.sinaimg.cn/orj480/big-cover.jpg", width: 1280, height: 720 }
      }
    },
    "https://wx1.sinaimg.cn/orj480/fallback.jpg"
  );
  assert.equal(bigPic.url, "https://wx1.sinaimg.cn/orj480/big-cover.jpg");
  assert.equal(bigPic.width, 1280);
  assert.equal(bigPic.height, 720);
  assert.equal(bigPic.source, "weibo");

  const inherited = thumb.extractWeiboThumbnail({}, "//wx2.sinaimg.cn/orj480/post-pic.webp");
  assert.equal(inherited.url, "https://wx2.sinaimg.cn/orj480/post-pic.webp");
  assert.equal(inherited.format, "WEBP");
  assert.equal(inherited.ext, "webp");
});

test("Generic/Facebook/Douyin extractDomVideoThumbnail checks poster, container cover, and single-video og:image", () => {
  // 1. Direct <video poster>
  const videoWithPoster = { poster: "//cdn.example.com/poster.jpg", videoWidth: 1920, videoHeight: 1080 };
  const r1 = thumb.extractDomVideoThumbnail(videoWithPoster, null, "https://www.douyin.com/video/123", { videoCount: 1 });
  assert.equal(r1.url, "https://cdn.example.com/poster.jpg");
  assert.equal(r1.width, 1920);
  assert.equal(r1.height, 1080);
  assert.equal(r1.source, "poster");

  // 2. Container cover image (skips avatar, picks large cover)
  const container = {
    parentElement: null,
    querySelectorAll(sel) {
      if (sel === "video") return [videoNoPoster];
      if (sel === "img") {
        return [
          { className: "user-avatar", src: "https://cdn.example.com/avatar.jpg", naturalWidth: 64, naturalHeight: 64 },
          { className: "video-cover", src: "https://cdn.example.com/cover.webp", naturalWidth: 1080, naturalHeight: 1920 }
        ];
      }
      return [];
    }
  };
  const videoNoPoster = { poster: "", parentElement: container };
  const r2 = thumb.extractDomVideoThumbnail(videoNoPoster, null, "https://www.facebook.com/reel/123", { videoCount: 3 });
  assert.equal(r2.url, "https://cdn.example.com/cover.webp");
  assert.equal(r2.format, "WEBP");
  assert.equal(r2.source, "container");

  // 3. og:image only on single-video pages, blocked on multi-video feeds
  const mockDoc = {
    querySelector(sel) {
      if (sel === 'meta[property="og:image"]') {
        return { content: "https://cdn.example.com/og-thumb.png" };
      }
      return null;
    }
  };
  const bareVideo = { poster: "", parentElement: null };
  const rSingle = thumb.extractDomVideoThumbnail(bareVideo, mockDoc, "https://example.com/watch/999", { videoCount: 1 });
  assert.equal(rSingle.url, "https://cdn.example.com/og-thumb.png");
  assert.equal(rSingle.format, "PNG");
  assert.equal(rSingle.source, "meta");

  const rFeed = thumb.extractDomVideoThumbnail(bareVideo, mockDoc, "https://www.facebook.com/", { videoCount: 1 });
  assert.equal(rFeed, null);

  const rMulti = thumb.extractDomVideoThumbnail(bareVideo, mockDoc, "https://example.com/feed", { videoCount: 4 });
  assert.equal(rMulti, null);
});

test("buildThumbnailFilename sanitizes titles and appends -thumbnail.<ext>", () => {
  assert.equal(
    thumb.buildThumbnailFilename("My Cool Video: Part 1? - YouTube", "jpg"),
    "My Cool Video Part 1-thumbnail.jpg"
  );
  assert.equal(
    thumb.buildThumbnailFilename("Anime Ep 01_哔哩哔哩_bilibili", "webp"),
    "Anime Ep 01-thumbnail.webp"
  );
  assert.equal(thumb.buildThumbnailFilename("", "png"), "video-thumbnail.png");
});

test("YouTube thumbnail filename and download payload never contain '..' or forbidden characters", () => {
  const { buildDownloadPayload, sanitizeDownloadFilename } = require("./background.js");

  const trickyTitles = [
    "(3) Wait... What Happened Next?! - YouTube",
    "Top 10.. Tricks / Hacks \\ Secrets: Part 1... - YouTube",
    "...",
    "A".repeat(70) + "... - YouTube"
  ];

  for (const title of trickyTitles) {
    const filename = thumb.buildThumbnailFilename(title, "jpg");
    assert.ok(!filename.includes(".."), `filename must not contain '..': ${filename}`);
    assert.ok(!/[<>:"/\\|?*\x00-\x1f]/.test(filename), `filename must not contain forbidden chars: ${filename}`);
    assert.ok(filename.endsWith("-thumbnail.jpg"), `filename must end with -thumbnail.jpg: ${filename}`);

    const payload = buildDownloadPayload({
      url: "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg",
      filename,
      referrer: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
      headers: { Origin: "https://www.youtube.com", Referer: "https://www.youtube.com/watch?v=dQw4w9WgXcQ" }
    });
    assert.equal(payload.action, "download");
    assert.equal(payload.url, "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg");
    assert.equal(payload.filename, filename);
    assert.ok(!payload.filename.includes(".."));
  }

  assert.equal(sanitizeDownloadFilename("My Video...-thumbnail.jpg"), "My Video.-thumbnail.jpg");
  assert.equal(sanitizeDownloadFilename("foo:bar?baz*..jpg"), "foo bar baz .jpg");
});

test("Xiaohongshu (RedNote) host detection and noteId extraction", () => {
  assert.equal(thumb.isXiaohongshuHost("www.xiaohongshu.com"), true);
  assert.equal(thumb.isXiaohongshuHost("xiaohongshu.com"), true);
  assert.equal(thumb.isXiaohongshuHost("youtube.com"), false);

  assert.equal(
    thumb.extractXiaohongshuNoteId("https://www.xiaohongshu.com/explore/65a1b2c3000000001e03a1b2?xsec_token=abc"),
    "65a1b2c3000000001e03a1b2"
  );
  assert.equal(
    thumb.extractXiaohongshuNoteId("https://www.xiaohongshu.com/discovery/item/65a1b2c3000000001e03a1b2"),
    "65a1b2c3000000001e03a1b2"
  );
  assert.equal(
    thumb.extractXiaohongshuNoteId("https://www.xiaohongshu.com/user/profile/5f1234567890abcdef123456/65a1b2c3000000001e03a1b2"),
    "65a1b2c3000000001e03a1b2"
  );
  assert.equal(thumb.extractXiaohongshuNoteId("https://www.xiaohongshu.com/explore"), "");
});

test("Xiaohongshu extractXiaohongshuInitialState extracts outer cover from noteDetailMap imageList[0]", () => {
  const noteId = "65a1b2c3000000001e03a1b2";
  const state = {
    note: {
      noteDetailMap: {
        [noteId]: {
          note: {
            noteId,
            title: "My RedNote Post - 小红书",
            imageList: [
              {
                width: 1080,
                height: 1440,
                urlDefault: "http://sns-webpic-qc.xhscdn.com/20261126/cover01!nd_dft_wlteh_webp_3",
                urlPre: "http://sns-webpic-qc.xhscdn.com/20261126/cover01!nd_prv_wlteh_jpg_3"
              }
            ]
          }
        }
      }
    }
  };

  const res = thumb.extractXiaohongshuInitialState(state, `https://www.xiaohongshu.com/explore/${noteId}`);
  assert.ok(res);
  assert.equal(res.noteId, noteId);
  assert.equal(res.thumbnail.url, "http://sns-webpic-qc.xhscdn.com/20261126/cover01!nd_dft_wlteh_webp_3");
  assert.equal(res.thumbnail.width, 1080);
  assert.equal(res.thumbnail.height, 1440);
  assert.equal(res.thumbnail.format, "WEBP");
  assert.equal(res.thumbnail.ext, "webp");
  assert.equal(res.thumbnail.source, "xiaohongshu");

  // infoList prefers WB_DFT over WB_PRV when urlDefault is absent
  const infoListCover = thumb.extractXiaohongshuCoverFromNote({
    imageList: [
      {
        width: 720,
        height: 960,
        infoList: [
          { imageScene: "WB_PRV", url: "https://sns-webpic-qc.xhscdn.com/prv!nd_prv_wlteh_jpg_3" },
          { imageScene: "WB_DFT", url: "https://sns-webpic-qc.xhscdn.com/dft!nd_dft_wlteh_jpg_3" }
        ]
      }
    ]
  });
  assert.equal(infoListCover.url, "https://sns-webpic-qc.xhscdn.com/dft!nd_dft_wlteh_jpg_3");
  assert.equal(infoListCover.format, "JPG");
});

test("Xiaohongshu extractXiaohongshuFeedResponse parses /api/sns/web/v1/feed items", () => {
  const items = thumb.extractXiaohongshuFeedResponse({
    data: {
      items: [
        {
          id: "65a1b2c3000000001e03a1b2",
          note_card: {
            title: "Feed Video Cover",
            image_list: [
              {
                width: 1080,
                height: 1920,
                url_default: "https://sns-webpic-qc.xhscdn.com/feed-cover!nd_dft_wlteh_webp_3"
              }
            ]
          }
        }
      ]
    }
  });
  assert.equal(items.length, 1);
  assert.equal(items[0].noteId, "65a1b2c3000000001e03a1b2");
  assert.equal(items[0].title, "Feed Video Cover");
  assert.equal(items[0].thumbnail.url, "https://sns-webpic-qc.xhscdn.com/feed-cover!nd_dft_wlteh_webp_3");
  assert.equal(items[0].thumbnail.format, "WEBP");
});

test("Xiaohongshu extractXiaohongshuDomCover picks outer note card cover and skips avatars", () => {
  const noteId = "65a1b2c3000000001e03a1b2";
  const mockDoc = {
    querySelectorAll(sel) {
      if (sel === `a[href*="${noteId}"] img`) {
        return [
          { className: "author-avatar", src: "https://sns-avatar-qc.xhscdn.com/avatar/123.jpg", naturalWidth: 200, naturalHeight: 200 },
          { className: "note-cover-img", src: "https://sns-webpic-qc.xhscdn.com/outer-cover!nd_dft_wlteh_jpg_3", naturalWidth: 1080, naturalHeight: 1440 }
        ];
      }
      return [];
    },
    querySelector() {
      return null;
    }
  };
  const cover = thumb.extractXiaohongshuDomCover(null, mockDoc, `https://www.xiaohongshu.com/explore/${noteId}`);
  assert.ok(cover);
  assert.equal(cover.url, "https://sns-webpic-qc.xhscdn.com/outer-cover!nd_dft_wlteh_jpg_3");
  assert.equal(cover.source, "xiaohongshu-dom");

  // /explore homepage must not fall back to generic og:image
  assert.equal(thumb.isSingleVideoPage("https://www.xiaohongshu.com/explore", 1), false);
});

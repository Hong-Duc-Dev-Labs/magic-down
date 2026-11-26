"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const x = require("./x.js");
const bg = require("./background.js");
const thumb = require("./thumbnail.js");

test("isXHost, isXCDNHost, and isXApiEndpoint recognize X/Twitter domains and APIs", () => {
  assert.equal(x.isXHost("x.com"), true);
  assert.equal(x.isXHost("www.x.com"), true);
  assert.equal(x.isXHost("mobile.x.com"), true);
  assert.equal(x.isXHost("twitter.com"), true);
  assert.equal(x.isXHost("mobile.twitter.com"), true);
  assert.equal(x.isXHost("notx.com"), false);
  assert.equal(x.isXHost("example.com"), false);

  assert.equal(x.isXCDNHost("video.twimg.com"), true);
  assert.equal(x.isXCDNHost("pbs.twimg.com"), true);
  assert.equal(x.isXCDNHost("twimg.com"), true);
  assert.equal(x.isXCDNHost("example.com"), false);

  assert.equal(x.isXApiEndpoint("https://x.com/i/api/graphql/abc123/TweetDetail?variables=%7B%7D"), true);
  assert.equal(x.isXApiEndpoint("https://x.com/i/api/graphql/xyz/TweetResultByRestId"), true);
  assert.equal(x.isXApiEndpoint("https://x.com/i/api/graphql/xyz/HomeTimeline"), true);
  assert.equal(x.isXApiEndpoint("https://x.com/i/api/graphql/xyz/UserTweets"), true);
  assert.equal(x.isXApiEndpoint("https://x.com/i/api/graphql/xyz/SearchTimeline"), true);
  assert.equal(x.isXApiEndpoint("https://twitter.com/i/api/1.1/statuses/show.json?id=123"), true);
  assert.equal(x.isXApiEndpoint("https://x.com/i/api/2/search/adaptive.json"), true);
  assert.equal(x.isXApiEndpoint("https://x.com/8daytp/status/2125787477583794218"), false);
  assert.equal(x.isXApiEndpoint("https://example.com/i/api/graphql/abc/TweetDetail"), false);
});

test("extractXTweetId extracts status IDs from x.com and twitter.com URLs", () => {
  assert.equal(x.extractXTweetId("https://x.com/8daytp/status/2125787477583794218"), "2125787477583794218");
  assert.equal(x.extractXTweetId("https://twitter.com/user/status/1900123456789012345/video/1"), "1900123456789012345");
  assert.equal(x.extractXTweetId("https://x.com/i/web/status/1900123456789012345?s=20"), "1900123456789012345");
  assert.equal(x.extractXTweetId("/8daytp/status/2125787477583794218"), "2125787477583794218");
  assert.equal(x.extractXTweetId("https://x.com/home"), "");
  assert.equal(x.extractXTweetId("https://example.com/user/status/2125787477583794218"), "");
});

test("inferXVideoResolution handles landscape, portrait, and fallback dimensions", () => {
  const r1 = x.inferXVideoResolution("https://video.twimg.com/ext_tw_video/111/pu/vid/avc1/1280x720/abc.mp4?tag=12");
  assert.deepEqual(r1, { width: 1280, height: 720, quality: "720p" });

  const r2 = x.inferXVideoResolution("https://video.twimg.com/amplify_video/222/vid/720x1280/portrait.mp4");
  assert.deepEqual(r2, { width: 720, height: 720, quality: "720p" });

  const r3 = x.inferXVideoResolution("https://video.twimg.com/ext_tw_video/333/pu/vid/640x360/small.mp4");
  assert.deepEqual(r3, { width: 640, height: 360, quality: "360p" });

  const r4 = x.inferXVideoResolution("https://video.twimg.com/ext_tw_video/444/pu/vid/avc1/1920x1080/fullhd.mp4");
  assert.deepEqual(r4, { width: 1920, height: 1080, quality: "1080p" });

  const r5 = x.inferXVideoResolution("https://video.twimg.com/ext_tw_video/555/pu/vid/unknown.mp4");
  assert.deepEqual(r5, { width: 0, height: 0, quality: "Original" });
});

test("parseXTweetGraphQL parses TweetDetail GraphQL response with MP4 variants sorted by bitrate, MP3 presets, and orig thumbnail", () => {
  const tweetDetailJson = {
    data: {
      threaded_conversation_with_injections_v2: {
        instructions: [
          {
            type: "TimelineAddEntries",
            entries: [
              {
                entryId: "tweet-2125787477583794218",
                content: {
                  itemContent: {
                    tweet_results: {
                      result: {
                        __typename: "Tweet",
                        rest_id: "2125787477583794218",
                        core: {
                          user_results: {
                            result: {
                              legacy: { screen_name: "8daytp" }
                            }
                          }
                        },
                        legacy: {
                          id_str: "2125787477583794218",
                          full_text: "Awesome video clip! https://t.co/xyz123",
                          extended_entities: {
                            media: [
                              {
                                id_str: "2125787470000000001",
                                media_key: "7_2125787470000000001",
                                type: "video",
                                media_url_https: "https://pbs.twimg.com/ext_tw_video_thumb/2125787470000000001/pu/img/thumb_abc.jpg",
                                original_info: { width: 1280, height: 720 },
                                video_info: {
                                  duration_millis: 32500,
                                  variants: [
                                    {
                                      content_type: "application/x-mpegURL",
                                      url: "https://video.twimg.com/ext_tw_video/2125787470000000001/pu/pl/master.m3u8?tag=12"
                                    },
                                    {
                                      bitrate: 256000,
                                      content_type: "video/mp4",
                                      url: "https://video.twimg.com/ext_tw_video/2125787470000000001/pu/vid/avc1/480x270/low.mp4?tag=12"
                                    },
                                    {
                                      bitrate: 2176000,
                                      content_type: "video/mp4",
                                      url: "https://video.twimg.com/ext_tw_video/2125787470000000001/pu/vid/avc1/1280x720/hd.mp4?tag=12"
                                    },
                                    {
                                      bitrate: 832000,
                                      content_type: "video/mp4",
                                      url: "https://video.twimg.com/ext_tw_video/2125787470000000001/pu/vid/avc1/640x360/mid.mp4?tag=12"
                                    }
                                  ]
                                }
                              }
                            ]
                          }
                        }
                      }
                    }
                  }
                }
              }
            ]
          }
        ]
      }
    }
  };

  const entries = x.parseXTweetGraphQL(tweetDetailJson, {
    pageUrl: "https://x.com/8daytp/status/2125787477583794218"
  });
  assert.equal(entries.length, 1);

  const entry = entries[0];
  assert.equal(entry.tweetId, "2125787477583794218");
  assert.equal(entry.mediaId, "2125787470000000001");
  assert.equal(entry.title, "@8daytp - Awesome video clip!");

  // Only progressive MP4 variants (HLS excluded when MP4 exists), sorted high -> low bitrate
  assert.equal(entry.variants.length, 3);
  assert.deepEqual(
    entry.variants.map((v) => ({ quality: v.quality, bandwidth: v.bandwidth, format: v.format })),
    [
      { quality: "720p", bandwidth: 2176000, format: "MP4" },
      { quality: "360p", bandwidth: 832000, format: "MP4" },
      { quality: "270p", bandwidth: 256000, format: "MP4" }
    ]
  );
  assert.equal(entry.variants[0].source, "x");
  assert.equal(entry.variants[0].streamType, "progressive");
  assert.equal(entry.variants[0].headers.Origin, "https://x.com");

  // 3 MP3 presets generated from highest bitrate MP4 variant
  assert.equal(entry.mp3Variants.length, 3);
  assert.deepEqual(
    entry.mp3Variants.map((m) => m.audioBitrate),
    ["320k", "192k", "128k"]
  );
  assert.equal(entry.mp3Variants[0].url, entry.variants[0].url);
  assert.equal(entry.mp3Variants[0].outputFormat, "mp3");
  assert.equal(entry.mp3Variants[0].source, "x");

  // Thumbnail upgraded to ?name=orig
  assert.ok(entry.thumbnail);
  assert.equal(
    entry.thumbnail.url,
    "https://pbs.twimg.com/ext_tw_video_thumb/2125787470000000001/pu/img/thumb_abc.jpg?name=orig"
  );
  assert.equal(entry.thumbnail.width, 1280);
  assert.equal(entry.thumbnail.height, 720);
  assert.equal(entry.thumbnail.format, "JPG");
});

test("parseXTweetGraphQL falls back to HLS m3u8 when tweet has no progressive MP4", () => {
  const hlsOnlyPayload = {
    data: {
      tweetResult: {
        result: {
          __typename: "TweetWithVisibilityResults",
          tweet: {
            rest_id: "999888777666",
            legacy: {
              id_str: "999888777666",
              full_text: "Live replay clip",
              extended_entities: {
                media: [
                  {
                    id_str: "555444",
                    media_url_https: "https://pbs.twimg.com/amplify_video_thumb/555444/img/cover.png?name=small",
                    video_info: {
                      variants: [
                        {
                          content_type: "application/x-mpegURL",
                          url: "https://video.twimg.com/amplify_video/555444/pl/master.m3u8"
                        }
                      ]
                    }
                  }
                ]
              }
            }
          }
        }
      }
    }
  };

  const entries = x.parseXTweetGraphQL(hlsOnlyPayload);
  assert.equal(entries.length, 1);
  assert.equal(entries[0].tweetId, "999888777666");
  assert.equal(entries[0].variants.length, 1);
  assert.equal(entries[0].variants[0].format, "HLS");
  assert.equal(entries[0].variants[0].isHLS, true);
  assert.equal(entries[0].thumbnail.url, "https://pbs.twimg.com/amplify_video_thumb/555444/img/cover.png?name=orig");
  assert.equal(entries[0].thumbnail.format, "PNG");
  assert.equal(entries[0].mp3Variants.length, 3);
});

test("parseXTweetGraphQL parses multi-tweet HomeTimeline and scopes accurately by tweetId and poster", () => {
  const timelineJson = {
    data: {
      home: {
        home_timeline_urt: {
          instructions: [
            {
              entries: [
                {
                  content: {
                    itemContent: {
                      tweet_results: {
                        result: {
                          rest_id: "100001",
                          legacy: {
                            id_str: "100001",
                            full_text: "First tweet",
                            extended_entities: {
                              media: [
                                {
                                  id_str: "m1",
                                  media_url_https: "https://pbs.twimg.com/ext_tw_video_thumb/100001/pu/img/poster_one.jpg",
                                  video_info: {
                                    variants: [
                                      { bitrate: 1500000, content_type: "video/mp4", url: "https://video.twimg.com/ext_tw_video/100001/pu/vid/1280x720/one.mp4" }
                                    ]
                                  }
                                }
                              ]
                            }
                          }
                        }
                      }
                    }
                  }
                },
                {
                  content: {
                    itemContent: {
                      tweet_results: {
                        result: {
                          rest_id: "200002",
                          legacy: {
                            id_str: "200002",
                            full_text: "Second tweet",
                            extended_entities: {
                              media: [
                                {
                                  id_str: "m2",
                                  media_url_https: "https://pbs.twimg.com/ext_tw_video_thumb/200002/pu/img/poster_two.jpg",
                                  video_info: {
                                    variants: [
                                      { bitrate: 900000, content_type: "video/mp4", url: "https://video.twimg.com/ext_tw_video/200002/pu/vid/720x1280/two.mp4" }
                                    ]
                                  }
                                }
                              ]
                            }
                          }
                        }
                      }
                    }
                  }
                }
              ]
            }
          ]
        }
      }
    }
  };

  const entries = x.parseXTweetGraphQL(timelineJson);
  assert.equal(entries.length, 2);

  // Match by tweetId
  const hit1 = x.matchXEntryForVideo(entries, { tweetId: "100001", pageUrl: "https://x.com/home" });
  assert.ok(hit1);
  assert.equal(hit1.tweetId, "100001");
  assert.equal(hit1.variants[0].url, "https://video.twimg.com/ext_tw_video/100001/pu/vid/1280x720/one.mp4");

  // Match by poster even with different ?name= query param
  const hit2 = x.matchXEntryForVideo(entries, {
    posterUrl: "https://pbs.twimg.com/ext_tw_video_thumb/200002/pu/img/poster_two.jpg?name=small",
    pageUrl: "https://x.com/home"
  });
  assert.ok(hit2);
  assert.equal(hit2.tweetId, "200002");

  // Unrelated tweet on timeline must NOT return another tweet's video
  const miss = x.matchXEntryForVideo(entries, { tweetId: "999999", pageUrl: "https://x.com/home" });
  assert.equal(miss, null);
});

test("extractTweetIdFromVideoElement resolves tweetId from closest article status link", () => {
  const mockLink = {
    href: "https://x.com/8daytp/status/2125787477583794218",
    getAttribute: (name) => (name === "href" ? "/8daytp/status/2125787477583794218" : null)
  };
  const mockTime = {
    closest: (sel) => (sel === "a[href]" ? mockLink : null),
    parentElement: mockLink
  };
  const mockArticle = {
    querySelectorAll: (sel) => {
      if (sel === 'a[href*="/status/"] time') return [mockTime];
      if (sel === 'a[href*="/status/"]') return [mockLink];
      return [];
    }
  };
  const mockVideo = {
    closest: (sel) => (sel.includes("article") ? mockArticle : null)
  };

  assert.equal(
    x.extractTweetIdFromVideoElement(mockVideo, "https://x.com/home"),
    "2125787477583794218"
  );
});

test("background.js filters video.twimg.com .m4s/.ts/.aac segments and infers WxH resolution for MP4", () => {
  assert.equal(
    x.isXMediaSegmentURL("https://video.twimg.com/ext_tw_video/123/pu/vid/avc1/1280x720/seg_001.m4s"),
    true
  );
  assert.equal(
    x.isXMediaSegmentURL("https://video.twimg.com/ext_tw_video/123/pu/vid/1280x720/chunk0.ts"),
    true
  );
  assert.equal(
    x.isXMediaSegmentURL("https://video.twimg.com/ext_tw_video/123/pu/aud/mp4a/audio_001.aac"),
    true
  );
  assert.equal(
    x.isXMediaSegmentURL("https://video.twimg.com/ext_tw_video/123/pu/vid/avc1/1280x720/full.mp4"),
    false
  );

  // webRequest classifier must reject segments
  for (const segUrl of [
    "https://video.twimg.com/ext_tw_video/123/pu/vid/avc1/1280x720/seg_001.m4s",
    "https://video.twimg.com/ext_tw_video/123/pu/vid/1280x720/chunk0.ts",
    "https://video.twimg.com/ext_tw_video/123/pu/aud/mp4a/audio_001.aac"
  ]) {
    assert.equal(
      bg.classifyMediaResponse(segUrl, [
        { name: "content-type", value: "video/mp4" },
        { name: "content-length", value: "400000" }
      ]),
      null,
      `segment should be filtered: ${segUrl}`
    );
  }

  // Full progressive MP4 from video.twimg.com should classify with inferred resolution
  const mp4 = bg.classifyMediaResponse(
    "https://video.twimg.com/ext_tw_video/123/pu/vid/avc1/720x1280/full.mp4?tag=12",
    [
      { name: "content-type", value: "video/mp4" },
      { name: "content-length", value: "2500000" }
    ]
  );
  assert.ok(mp4);
  assert.equal(mp4.format, "MP4");
  assert.equal(mp4.quality, "720p");
  assert.equal(mp4.height, 720);
});

test("thumbnail.js treats x.com timelines as multi-video pages and strips / X suffix from filenames", () => {
  assert.equal(thumb.isSingleVideoPage("https://x.com/home", 1), false);
  assert.equal(thumb.isSingleVideoPage("https://x.com/8daytp", 1), false);
  assert.equal(thumb.isSingleVideoPage("https://x.com/8daytp/status/2125787477583794218", 1), true);
  assert.equal(thumb.buildThumbnailFilename("Cool post on X / X", "jpg"), "Cool post on X-thumbnail.jpg");
  assert.equal(thumb.buildThumbnailFilename("Cool post / Twitter", "png"), "Cool post-thumbnail.png");
});

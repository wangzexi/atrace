// Replays synthetic Weibo responses through the collector's conversion half and
// compares the documents with the synthetic ones. Run: bun test client/app/tests/collectors/
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { contractProblems, loadConversion } from "../testlib.mjs";

// The fixtures were synthetic for this account; a collector learns its own when it runs.
const RECORDED_FOR = { assign: { WEIBO_UID: "1000000001" } };

const here = new URL(".", import.meta.url).pathname;
const NAMES = [
  "postDocuments", "commentDocuments", "reactionDocuments", "likeDocument", "addDocument", "longTextOf", "textOf", "replyParent", "withPictureMarks", "postNumberOf",
];
const conversionOf = dir => loadConversion(`${here}../../../../cli/collector-examples/${dir}/index.mjs`, NAMES, RECORDED_FOR);
const c = conversionOf("weibo");

function convert({ kind, input }, conv = c) {
  switch (kind) {
    case "post": return conv.postDocuments(input.status, input.longtext ? conv.longTextOf(input.longtext) : conv.textOf(input.status));
    case "comment": return conv.commentDocuments(input.comment, input.post);
    case "like": return conv.reactionDocuments(input.status, conv.likeDocument);
    case "favorite": return conv.reactionDocuments(input.status, conv.addDocument);
    default: throw new Error(`unknown fixture kind ${kind}`);
  }
}

// weibo reads the signed-in account and weibo-user reads another account, but both turn a response into documents the
// same way: the same responses must give the same documents.
for (const dir of ["weibo", "weibo-user"]) {
  const conv = conversionOf(dir);
  for (const file of readdirSync(`${here}testdata`).filter(name => name.endsWith(".json"))) {
    const fixture = JSON.parse(readFileSync(`${here}testdata/${file}`, "utf8"));
    test(`${dir} ${file}`, () => {
      const entries = convert(fixture, conv);
      assert.deepEqual(entries, fixture.expected);
      assert.deepEqual(convert(fixture, conv), entries, "converting twice must give the same documents");
      assert.deepEqual(contractProblems(entries, "weibo"), []);
    });
  }
}

test("a long post uses the full text, not the shortened one", () => {
  const { input } = JSON.parse(readFileSync(`${here}testdata/post-long.json`, "utf8"));
  const post = convert({ kind: "post", input }).find(entry => entry.role === "post").document;
  assert.ok(post.content.length > c.textOf(input.status).length);
});

test("a repost with words of the user's own is their post quoting the cited one, which is kept as its own Note", () => {
  const { expected } = JSON.parse(readFileSync(`${here}testdata/repost.json`, "utf8"));
  assert.deepEqual(expected.map(entry => entry.role), ["author", "cited-author", "cited", "post", "create"]);
  const note = expected.find(entry => entry.role === "post").document;
  assert.equal(note.type, "Note");
  assert.equal(note.quote, expected.find(entry => entry.role === "cited").document.id);
  assert.ok(note.content);
});

test("a repost without words, only Weibo's stand-in text, is a plain Announce of the cited post", () => {
  const { expected } = JSON.parse(readFileSync(`${here}testdata/repost-plain.json`, "utf8"));
  assert.deepEqual(expected.map(entry => entry.role), ["author", "cited-author", "cited", "post"]);
  const announce = expected.find(entry => entry.role === "post").document;
  assert.equal(announce.type, "Announce");
  assert.equal(announce.content, undefined);
  assert.equal(announce.object, expected.find(entry => entry.role === "cited").document.id);
});

test("an image outside sinaimg.cn is rejected", () => {
  const status = { idstr: "1", user: { idstr: "2" }, pic_infos: { a: { largest: { url: "https://evil.example/a.jpg" } } } };
  assert.throws(() => c.postDocuments(status, "x"), /Unexpected Weibo image host/);
});

test("a video post keeps its MP4 attachment without the expiring signature", () => {
  // Field layout observed on weibo:post:5366437128701467.
  const status = {
    idstr: "5366437128701467", user: { idstr: "1720104335" },
    page_info: { object_type: "video", media_info: {
      stream_url: "http://f.video.weibocdn.com/o0/480.mp4?Expires=123&ssig=old",
      playback_list: [
        { play_info: { mime: "image/jpeg", url: "https://wx3.sinaimg.cn/large/scrubber.jpg" } },
        { play_info: { mime: "video/mp4", video_codecs: "avc1.640032", height: 1080, url: "https://f.video.weibocdn.com/o0/1080.mp4?Expires=123&ssig=signed" } },
      ],
    } },
  };
  const post = c.postDocuments(status, "video").find(entry => entry.role === "post").document;
  assert.deepEqual(post.attachment, [{ type: "Video", url: "https://f.video.weibocdn.com/o0/1080.mp4" }]);
  delete status.page_info.media_info.playback_list;
  assert.deepEqual(c.postDocuments(status, "video").find(entry => entry.role === "post").document.attachment,
    [{ type: "Video", url: "https://f.video.weibocdn.com/o0/480.mp4" }]);
});

test("a reply in a thread answers the comment it names, without Weibo's 回复@name: lead-in", () => {
  const post = { idstr: "100", user: { idstr: "1", screen_name: "me" } };
  const reply = { idstr: "300", rootidstr: "200", reply_comment: { idstr: "250" }, text_raw: "回复@someone:我也惊了", user: { idstr: "2", screen_name: "A" }, created_at: "Sun Jan 26 01:18:06 +0800 2020" };
  assert.equal(c.replyParent(reply, "200"), "250");
  assert.equal(c.replyParent({ ...reply, reply_comment: undefined }, "200"), "200");
  const note = c.commentDocuments(reply, post, c.replyParent(reply, "200")).find(entry => entry.role === "post").document;
  assert.equal(note.inReplyTo, "weibo:comment:250");
  assert.equal(note.content, "我也惊了");
  const direct = c.commentDocuments({ ...reply, text_raw: "回复@someone:plain" }, post).find(entry => entry.role === "post").document;
  assert.equal(direct.inReplyTo, "weibo:post:100");
  assert.equal(direct.content, "回复@someone:plain", "a comment on the post itself is left as written");
});

test("a comment's picture link becomes a [图片] mark, since Weibo gives no address for the picture", () => {
  const link = { url_title: "查看图片", short_url: "http://t.cn/AXlRaDM9" };
  assert.equal(c.withPictureMarks({ pic_num: 2, url_struct: [link] }, "这条思考很好。 http://t.cn/AXlRaDM9"), "这条思考很好。 [图片]");
  assert.equal(c.withPictureMarks({ pic_num: 1 }, "没有链接"), "没有链接 [图片]");
  assert.equal(c.withPictureMarks({ pic_num: 1 }, ""), "[图片]");
  assert.equal(c.withPictureMarks({ pic_num: 0, url_struct: [{ url_title: "网页链接", short_url: "http://t.cn/x" }] }, "看 http://t.cn/x"), "看 http://t.cn/x");
});

// Who is signed in: the x-log-uid of Weibo's answer, and the name from that user's profile.
const who = loadConversion(`${here}../../../../cli/collector-examples/weibo/index.mjs`, ["loggedInWeiboAccount"], RECORDED_FOR);
const profile = (uid, name, headers = { "x-log-uid": uid }) => ({ ok: true, status: 200, headers, body: JSON.stringify({ ok: 1, data: { user: { idstr: uid, screen_name: name } } }) });
const page = answers => ({
  fetch: async path => {
    if (!(path in answers)) throw new Error(`unexpected request ${path}`);
    return answers[path];
  },
});

test("the signed-in account is named by the answer's x-log-uid, and its name is on its profile", async () => {
  assert.deepEqual(await who.loggedInWeiboAccount(page({ "/ajax/profile/info?uid=1000000001": profile("1000000001", "示例账号") })), { uid: "1000000001", name: "示例账号" });
});

test("another account signed in is asked for its own profile", async () => {
  const answers = {
    "/ajax/profile/info?uid=1000000001": profile("1000000001", "someone else", { "x-log-uid": "42" }),
    "/ajax/profile/info?uid=42": profile("42", "the signed-in one"),
  };
  assert.deepEqual(await who.loggedInWeiboAccount(page(answers)), { uid: "42", name: "the signed-in one" });
});

test("an answer that is a page, not data, says the profile is not logged in, as the 404 page of a removed request did", async () => {
  const html = { ok: true, status: 200, headers: { "x-log-uid": "1000000001" }, body: "<!DOCTYPE html><html><title>404错误</title></html>" };
  await assert.rejects(who.loggedInWeiboAccount(page({ "/ajax/profile/info?uid=1000000001": html })), /not logged in to Weibo.*answered with a page/);
});

test("no signed-in uid is not logged in", async () => {
  for (const headers of [{}, { "x-log-uid": "0" }]) {
    await assert.rejects(who.loggedInWeiboAccount(page({ "/ajax/profile/info?uid=1000000001": profile("1000000001", "x", headers) })), /not logged in to Weibo/);
  }
});

test("an HTTP error is not taken for a missing login", async () => {
  await assert.rejects(who.loggedInWeiboAccount(page({ "/ajax/profile/info?uid=1000000001": { ok: false, status: 502, headers: {}, body: "" } })), /HTTP 502/);
});

// Weibo's image host answers a picture it will not hand out with a placeholder GIF; it must not be kept as the picture.
const imageType = loadConversion(`${here}../../../../cli/collector-examples/weibo/index.mjs`, ["imageTypeProblem"], RECORDED_FOR);

test("an image is the format its address names; the placeholder GIF for a .jpg is not the picture", () => {
  for (const [url, mime] of [
    ["https://wx4.sinaimg.cn/large/a.jpg", "image/jpeg"],
    ["https://wx4.sinaimg.cn/large/a.jpeg", "image/jpeg"],
    ["https://wx4.sinaimg.cn/large/a.png", "image/png"],
    ["https://wx4.sinaimg.cn/large/a.gif", "image/gif"],
    ["https://wx4.sinaimg.cn/large/a.webp", "image/webp"],
    ["https://wx4.sinaimg.cn/large/a.JPG?x=1", "image/jpeg"],
  ]) assert.equal(imageType.imageTypeProblem(url, mime), "", `${url} ${mime}`);
  assert.match(imageType.imageTypeProblem("https://wx4.sinaimg.cn/large/6686b58fly1i08qevnrflj20b406ojrh.jpg", "image/gif"), /image\/gif for a \.jpg/);
  assert.match(imageType.imageTypeProblem("https://wx1.sinaimg.cn/large/a.png", "image/jpeg"), /image\/jpeg for a \.png/);
});

test("a type nobody synthetic, or an address with no known extension, is not taken for a placeholder", () => {
  assert.equal(imageType.imageTypeProblem("https://wx4.sinaimg.cn/large/a.jpg", ""), "");
  assert.equal(imageType.imageTypeProblem("https://wx4.sinaimg.cn/large/a", "image/gif"), "");
  assert.equal(imageType.imageTypeProblem("https://wx4.sinaimg.cn/large/a.bmp", "image/gif"), "");
});

test("weibo: whoever wrote a post made it", () => {
  const { authorshipOf: authorship } = loadConversion(`${here}../../../../cli/collector-examples/weibo/index.mjs`, ["authorshipOf"], RECORDED_FOR);
  assert.deepEqual(authorship({ type: "Note", id: "weibo:post:1", attributedTo: "weibo:user:2", published: "2026-11-01T00:00:00Z" }), { type: "Create", actor: "weibo:user:2", object: "weibo:post:1", published: "2026-11-01T00:00:00Z" });
  assert.deepEqual(authorship({ type: "Video", id: "weibo:post:1", attributedTo: "weibo:user:2" }), { type: "Create", actor: "weibo:user:2", object: "weibo:post:1" });
  assert.equal(authorship({ type: "Person", id: "weibo:user:2", name: "someone" }), undefined, "a person is not a post");
  assert.equal(authorship({ type: "Note", id: "weibo:post:1" }), undefined, "a post nobody is known to have written");
});

test("the post to read again is named by the target the device gives, and by nothing else", () => {
  assert.equal(c.postNumberOf("weibo:post:5363186455019700"), "5363186455019700");
  for (const bad of ["", "x:post:123", "weibo:post:", "weibo:post:12a", "weibo:post:123/../4", "weibo:user:1"]) assert.equal(c.postNumberOf(bad), "");
});

test("Live Photos retain still-motion pairs and use signed download sources", () => {
  const live = loadConversion(`${here}../../../../cli/collector-examples/weibo/index.mjs`, ["attachmentsOf", "livePhotosOf", "motionMime"]);
  const still = "https://wx1.sinaimg.cn/large/live.jpg";
  const motion = "https://us.sinaimg.cn/motion.mov";
  const source = `https://video.weibo.com/media/play?livephoto=${encodeURIComponent(motion)}&Expires=123&ssig=signature`;
  const status = { pic_infos: { a: { type: "livephoto", largest: { url: still }, video: source }, b: { type: "pic", largest: { url: "https://wx1.sinaimg.cn/large/normal.jpg" } } } };
  assert.deepEqual(live.attachmentsOf(status), [
    { type: "Image", url: still },
    { type: "Image", url: "https://wx1.sinaimg.cn/large/normal.jpg" },
    { type: "Video", url: motion, preview: { type: "Image", url: still } },
  ]);
  assert.deepEqual(live.livePhotosOf(status), [{ url: motion, source, image: still }]);
  status.pic_infos.a.video = source.replace("video.weibo.com", "evil.example");
  assert.deepEqual(live.livePhotosOf(status), []);
  status.pic_infos.a.video = `https://video.weibo.com/media/play?livephoto=${encodeURIComponent("https://evil.example/motion.mov")}`;
  assert.deepEqual(live.livePhotosOf(status), []);
  const bytes = Uint8Array.from([0, 0, 0, 20, ...Buffer.from('ftypqt  ')]).buffer;
  assert.equal(live.motionMime(bytes), "video/quicktime");
  assert.equal(live.motionMime(Uint8Array.from(Buffer.from('<html>access denied')).buffer), "");
});

test("both Weibo collectors keep legacy Sina video URLs and reject lookalike domains", () => {
  for (const dir of ["weibo", "weibo-user"]) {
    const conv = conversionOf(dir);
    for (const [host, accepted] of [["f.us.sinaimg.cn", true], ["f.us.sinaimg.cn.example.com", false]]) {
      const status = { idstr: "123456789", text_raw: "A video", user: { idstr: "1000000001", screen_name: "Fixture" }, page_info: { media_info: { stream_url: `https://${host}/legacy.mp4?Expires=123` } } };
      const post = conv.postDocuments(status, "A video").find(entry => entry.role === "post").document;
      assert.equal((post.attachment || []).some(item => item.type === "Video"), accepted);
      if (accepted) assert.equal(post.attachment[0].url, "https://f.us.sinaimg.cn/legacy.mp4");
    }
  }
});

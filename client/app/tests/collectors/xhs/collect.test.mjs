// Replays synthetic Xiaohongshu notes and cards through the collector's conversion
// half and compares the documents with the synthetic ones. Run: bun test client/app/tests/collectors/
// Fixtures with a "note" are rebuilt or constructed from synthetic data because
// the account has no synthetic example yet (comments, a video note).
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { contractProblems, loadConversion } from "../testlib.mjs";

const here = new URL(".", import.meta.url).pathname;
const NAMES = [
  "ownNoteDocuments", "reactionDocuments", "likeDocument", "addDocument", "commentDocuments", "stableImageUrl", "imageOf", "commentTimes", "timeOfComment", "timeOfDateText", "threadTargets", "noteIdOf", "videoOf",
];
const conversionOf = dir => loadConversion(`${here}../../../../cli/collector-examples/${dir}/index.mjs`, NAMES);
const c = conversionOf("xiaohongshu");
const fixture = name => JSON.parse(readFileSync(`${here}testdata/${name}.json`, "utf8"));

async function convert({ kind, input }, conv = c) {
  switch (kind) {
    case "own-note": return conv.ownNoteDocuments(input.card, input.note, input.meId);
    case "like": return conv.reactionDocuments(input.card, input.meId, object => conv.likeDocument(input.meId, object));
    case "favorite": return conv.reactionDocuments(input.card, input.meId, object => conv.addDocument(input.meId, object));
    case "comment": return conv.commentDocuments(input.cardId, input.comment);
    default: throw new Error(`unknown fixture kind ${kind}`);
  }
}

// xiaohongshu reads the signed-in account and xiaohongshu-user reads another account, but both turn what the pages
// show into documents the same way: the same input must give the same documents.
for (const dir of ["xiaohongshu", "xiaohongshu-user"]) {
  const conv = conversionOf(dir);
  for (const file of readdirSync(`${here}testdata`).filter(name => name.endsWith(".json"))) {
    const synthetic = JSON.parse(readFileSync(`${here}testdata/${file}`, "utf8"));
    test(`${dir} ${file}`, async () => {
      const entries = await convert(synthetic, conv);
      assert.deepEqual(entries, synthetic.expected);
      assert.deepEqual(await convert(synthetic, conv), entries, "converting twice must give the same documents");
      assert.deepEqual(contractProblems(entries, "xiaohongshu"), []);
    });
  }
}

test("the same image with a new timestamp and signature gives the same document", async () => {
  const synthetic = fixture("favorite");
  const resigned = JSON.parse(JSON.stringify(synthetic.input));
  resigned.card.cover = resigned.card.cover.replace(/\/\d{12}\/[0-9a-f]{32}\//, "/209901010000/ffffffffffffffffffffffffffffffff/");
  assert.notEqual(resigned.card.cover, synthetic.input.card.cover);
  const again = await convert({ kind: "favorite", input: resigned });
  assert.deepEqual(again.map(entry => entry.document), synthetic.expected.map(entry => entry.document));
  assert.notDeepEqual(again.find(entry => entry.downloads)?.downloads, synthetic.expected.find(entry => entry.downloads)?.downloads, "only the address to download from may change");
});

test("the document address of an image holds only the file ID", () => {
  assert.equal(c.stableImageUrl("https://sns-webpic-qc.xhscdn.com/202611150516/abc/1040g008!nd_dft_wlteh_webp_3"), "https://sns-webpic-qc.xhscdn.com/1040g008");
  assert.equal(c.stableImageUrl("https://sns-webpic-qc.xhscdn.com/202611150516/abc/spectrum/1040g0k0!nc_n_webp_mw_1"), "https://sns-webpic-qc.xhscdn.com/1040g0k0");
});

test("images from other hosts are not taken", () => {
  assert.equal(c.imageOf("https://evil.example/a.jpg"), null);
  assert.equal(c.imageOf(""), null);
});

test("a video address is kept without its signature", async () => {
  const note = (await convert(fixture("own-note-video")))[0].document;
  const video = note.attachment.find(item => item.type === "Video");
  assert.equal(video.url, "https://sns-video-qc.xhscdn.com/stream/1/110/259/abc.mp4");
});

test("an own note has a Create and downloads every image once", async () => {
  const entries = await convert(fixture("own-note"));
  assert.deepEqual(entries.map(entry => entry.role), ["post", "create"]);
  assert.equal(entries[0].downloads.length, entries[0].document.attachment.length);
});

test("a like of the user's own note stores only the Like", async () => {
  const entries = await convert(fixture("like-own-note"));
  assert.deepEqual(entries.map(entry => entry.role), ["activity"]);
});

test("a comment ID is derived from the note, commenter and text, and a reply points at its parent's ID", async () => {
  const [, top] = await convert(fixture("comment"));
  const [, reply] = await convert(fixture("comment-reply"));
  assert.match(top.document.id, /^xiaohongshu:comment:[0-9a-f]{16}$/);
  assert.equal(top.document.inReplyTo, `xiaohongshu:note:${fixture("comment").input.cardId}`);
  assert.equal(reply.document.inReplyTo, top.document.id);
});

test("a rendered comment gets the time the page's own request gave it, matched by commenter and text", () => {
  const found = c.commentTimes([{ data: { comments: [
    { user_info: { user_id: "111111111111111111111111" }, content: "99 还考虑一下", create_time: 1_795_001_759_000, sub_comments: [
      { user_info: { user_id: "aaaaaaaaaaaaaaaaaaaaaaaa" }, content: "我先去搞一个体验下", create_time: 1_795_008_895_000 },
    ] },
    { user_info: { user_id: "222222222222222222222222" }, content: "@someone", create_time: 0 },
  ] } }]);
  assert.equal(found.length, 2, "a comment without a time is left out");
  assert.equal(c.timeOfComment(found, { profile: "/user/profile/111111111111111111111111", text: "99 还考虑一下" }), new Date(1_795_001_759_000).toISOString());
  assert.equal(c.timeOfComment(found, { profile: "/user/profile/aaaaaaaaaaaaaaaaaaaaaaaa", text: "回复 示例甲：我先去搞一个体验下" }), new Date(1_795_008_895_000).toISOString());
  assert.equal(c.timeOfComment(found, { profile: "/user/profile/333333333333333333333333", text: "99 还考虑一下" }), undefined);
});

test("a comment the request does not give is dated by what the page prints under it", () => {
  const now = new Date("2026-11-18T12:00:00+08:00");
  const at = text => c.timeOfDateText(text, now);
  assert.equal(at("4小时前澳大利亚"), "2026-11-18T00:00:00.000Z");
  assert.equal(at("昨天 21:08广东"), "2026-11-17T13:08:00.000Z");
  assert.equal(at("11-17 北京"), "2026-11-16T16:00:00.000Z");
  assert.equal(at("12-30 北京"), "2025-12-29T16:00:00.000Z", "a day that has not come yet this year was last year's");
  assert.equal(at("2025-03-04 上海"), "2025-03-03T16:00:00.000Z");
  assert.equal(at("刚刚"), now.toISOString());
  assert.equal(at("编辑于 3天前"), undefined);
  assert.equal(at(""), undefined);
  const found = [{ id: "a".repeat(24), content: "[doge]我先去搞一个体验下", time: "2026-11-17T01:00:00.000Z" }];
  const comment = { profile: `/user/profile/${"a".repeat(24)}`, text: "我先去搞一个体验下", date: "4小时前北京" };
  assert.equal(c.timeOfComment(found, comment, now), "2026-11-17T01:00:00.000Z", "emoji the page leaves out do not stop the match");
  assert.equal(c.timeOfComment([], comment, now), "2026-11-18T00:00:00.000Z");
});

test("a reply inside a thread keeps only its own words, under the comment it answers", async () => {
  const parent = { profile: `/user/profile/${"1".repeat(24)}`, author: "示例丁", text: "5mm多招笑厚度", reply: false };
  const reply = { profile: `/user/profile/${"a".repeat(24)}`, author: "示例戊", text: "回复 示例丁 : todoo的卖点", reply: true, parent, published: "2026-11-17T01:00:00.000Z" };
  const [top] = (await c.commentDocuments("n".repeat(24), parent)).filter(entry => entry.role === "post");
  const [answer] = (await c.commentDocuments("n".repeat(24), reply)).filter(entry => entry.role === "post");
  assert.equal(answer.document.content, "todoo的卖点");
  assert.equal(answer.document.inReplyTo, top.document.id);
  assert.equal(answer.document.published, "2026-11-17T01:00:00.000Z");
});

test("a reply that names someone answers the latest reply by them in its thread, else the comment", async () => {
  const profile = letter => `/user/profile/${letter.repeat(24)}`;
  const top = { profile: profile("1"), author: "示例甲", text: "99 还考虑一下", reply: false };
  const comments = [
    top,
    { profile: profile("a"), author: "示例丙", text: "回复 示例甲 : 示例回复一", reply: true, parent: top },
    { profile: profile("2"), author: "示例乙", text: "回复 示例丙 : 示例回复二", reply: true, parent: top },
    { profile: profile("b"), author: "路人", text: "没有指名的回复", reply: true, parent: top },
  ];
  c.threadTargets(comments);
  assert.equal(comments[1].target, undefined, "an answer to the comment itself needs no target");
  assert.equal(comments[2].target.author, "示例丙");
  assert.equal(comments[3].target, undefined);
})

test("a comment's own ID holds its time, and a picture in it is a mark, not a file", async () => {
  const id = "6afd8d9f0000000031017fc1";
  const comment = { id, profile: `/user/profile/${"1".repeat(24)}`, author: "示例丙", text: "", reply: false, pictures: ["http://sns-webpic-qc.xhscdn.com/202611190100/6b9cbe1f/comment/1040g00abc!nd_dft_wlteh_jpg_3"] };
  assert.equal(c.timeOfComment([], comment), new Date(1795001759000).toISOString());
  const post = (await c.commentDocuments("n".repeat(24), comment)).find(entry => entry.role === "post");
  assert.equal(post.document.content, "[图片]");
  assert.equal(post.document.attachment, undefined);
  assert.equal(post.downloads, undefined);
  const withWords = (await c.commentDocuments("n".repeat(24), { ...comment, text: "看这个", pictures: ["http://x/y"] })).find(entry => entry.role === "post");
  assert.equal(withWords.document.content, "看这个 [图片]");
});

// A topic is written into a note's text as #name[话题]#; the mark is the platform's, and the topic is also a tag of the note.
test("a topic in the text of a note is #name, with its mark taken away, and is still a tag", async () => {
  const { withoutTopicMarks } = loadConversion(`${here}../../../../cli/collector-examples/xiaohongshu/index.mjs`, ["withoutTopicMarks"]);
  assert.equal(withoutTopicMarks("#Claude[话题]#"), "#Claude");
  assert.equal(withoutTopicMarks("#蒙太奇[话题]# #天津工业大学游泳馆[话题]#"), "#蒙太奇 #天津工业大学游泳馆");
  assert.equal(withoutTopicMarks("#随手拍[话题]"), "#随手拍");
  assert.equal(withoutTopicMarks("哦对“次卧”[大笑R]，一个 # 号和 [话 题] 都不动"), "哦对“次卧”[大笑R]，一个 # 号和 [话 题] 都不动");
  assert.equal(withoutTopicMarks(""), "");
  assert.equal(withoutTopicMarks(undefined), "");
  const { input } = JSON.parse(readFileSync(`${here}testdata/own-note.json`, "utf8"));
  const post = (await convert({ kind: "own-note", input })).find(entry => entry.role === "post").document;
  assert.ok(input.note.desc.includes("[话题]"), "the synthetic response has the mark");
  assert.ok(!post.content.includes("[话题]") && post.content.endsWith("#Example"), post.content);
  assert.deepEqual(post.tag, [{ type: "Hashtag", name: "Example" }]);
});

test("xiaohongshu: whoever wrote a post made it", () => {
  const { authorshipOf: authorship } = loadConversion(`${here}../../../../cli/collector-examples/xiaohongshu/index.mjs`, ["authorshipOf"]);
  assert.deepEqual(authorship({ type: "Note", id: "xiaohongshu:note:1", attributedTo: "xiaohongshu:user:2", published: "2026-11-01T00:00:00Z" }), { type: "Create", actor: "xiaohongshu:user:2", object: "xiaohongshu:note:1", published: "2026-11-01T00:00:00Z" });
  assert.deepEqual(authorship({ type: "Video", id: "xiaohongshu:note:1", attributedTo: "xiaohongshu:user:2" }), { type: "Create", actor: "xiaohongshu:user:2", object: "xiaohongshu:note:1" });
  assert.equal(authorship({ type: "Person", id: "xiaohongshu:user:2", name: "someone" }), undefined, "a person is not a post");
  assert.equal(authorship({ type: "Note", id: "xiaohongshu:note:1" }), undefined, "a post nobody is known to have written");
});

test("the note to read again is named by the target the device gives, and by nothing else", () => {
  assert.equal(c.noteIdOf("xiaohongshu:note:65f0c9b1000000001a01b2c3"), "65f0c9b1000000001a01b2c3");
  for (const bad of ["", "x:post:123", "xiaohongshu:note:", "xiaohongshu:note:abc", "xiaohongshu:note:65f0c9b1000000001a01b2c3/../4", "xiaohongshu:user:65f0c9b1000000001a01b2c3", "xiaohongshu:collection:favorites"]) assert.equal(c.noteIdOf(bad), "");
});

test("renamed codec groups choose a standalone MP4 with audio instead of fMP4 video-only streams", () => {
  for (const dir of ["xiaohongshu", "xiaohongshu-user"]) {
    const c = conversionOf(dir);
    assert.equal(c.videoOf({ video: { media: { stream: {
      EF5: [{ masterUrl: "https://video.example/video-only.mp4", format: "fmp4", width: 1920, height: 1080 }],
      EF4: [{ masterUrl: "http://video.example/complete.mp4", format: "mp4", width: 1280, height: 720, audioCodec: "aac" }],
    } } } }), "https://video.example/complete.mp4");
    assert.equal(c.videoOf({ video: { media: { stream: { h264: [{ masterUrl: "https://video.example/legacy.mp4", width: 640, height: 360 }] } } } }), "https://video.example/legacy.mp4");
    assert.equal(c.videoOf({}), "");
  }
});

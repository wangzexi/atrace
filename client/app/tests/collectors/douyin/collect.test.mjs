// Replays synthetic Douyin videos through the collector's conversion half and
// compares the documents with the synthetic ones. Run: bun test client/app/tests/collectors/
// Synthetic videos are cut down to the fields the conversion reads. Fixtures
// with a "note" are constructed because no synthetic example exists yet.
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { contractProblems, loadConversion } from "../testlib.mjs";

const here = new URL(".", import.meta.url).pathname;
const c = loadConversion(`${here}../../../../cli/collector-examples/douyin/index.mjs`, [
  "videoDocuments", "coverCandidates", "createDocument", "likeDocument", "addDocument", "stableImageUrl", "imageCandidates", "seconds", "awemeIdOf",
]);
const fixture = name => JSON.parse(readFileSync(`${here}testdata/${name}.json`, "utf8"));

// What the collector stores for a video: the cover it managed to download is the
// first candidate here, and the activity depends on the tab it was found in.
function convert({ kind, input }) {
  const object = `douyin:video:${input.video.aweme_id}`;
  const activity = {
    work: { role: "create", document: c.createDocument(input.meId, input.video) },
    like: { role: "activity", document: c.likeDocument(input.meId, object) },
    favorite: { role: "activity", document: c.addDocument(input.meId, object) },
  }[kind];
  return [...c.videoDocuments(input.video, input.cover), activity];
}

for (const file of readdirSync(`${here}testdata`).filter(name => name.endsWith(".json"))) {
  const synthetic = JSON.parse(readFileSync(`${here}testdata/${file}`, "utf8"));
  test(`douyin ${file}`, () => {
    const entries = convert(synthetic);
    assert.deepEqual(entries, synthetic.expected);
    assert.deepEqual(convert(synthetic), entries, "converting twice must give the same documents");
    assert.deepEqual(contractProblems(entries, "douyin"), []);
  });
}

test("a signed cover address, whichever host it came from, gives the same document address", () => {
  const signed = "https://p9-pc-sign.douyinpic.com/tos-cn-i-1/abc~tplv-dy-360p.jpeg?x-expires=1&x-signature=aaa&l=1";
  const resigned = "https://p3-pc-sign.douyinpic.com/tos-cn-i-1/abc~tplv-dy-360p.jpeg?x-expires=2&x-signature=bbb&l=2";
  assert.equal(c.stableImageUrl(signed), "https://p3-pc-sign.douyinpic.com/tos-cn-i-1/abc~tplv-dy-360p.jpeg");
  assert.equal(c.stableImageUrl(signed), c.stableImageUrl(resigned));
});

test("only Douyin's image hosts over https are taken, and the signed address is kept to download from", () => {
  const found = c.imageCandidates(["https://evil.example/a.jpg", "", "http://p3-pc-sign.douyinpic.com/a.jpg?s=1", "not a url"]);
  assert.deepEqual(found, [{ url: "https://p3-pc-sign.douyinpic.com/a.jpg", source: "https://p3-pc-sign.douyinpic.com/a.jpg?s=1" }]);
});

test("a video without an author or cover is still stored", () => {
  const entries = convert(fixture("like-no-author-no-cover"));
  assert.deepEqual(entries.map(entry => entry.role), ["post", "activity"]);
  const video = entries[0].document;
  assert.equal(video.attributedTo, undefined);
  assert.equal(video.attachment, undefined);
});

test("hashtags become Hashtag tags and an image post uses its first image as cover", () => {
  const tagged = convert(fixture("like-tags")).find(entry => entry.role === "post").document;
  assert.ok(tagged.tag.length && tagged.tag.every(tag => tag.type === "Hashtag" && tag.name));
  const { video } = fixture("like-image-post").input;
  assert.ok(c.coverCandidates(video).length > 0);
});

test("a video must have an id", () => {
  assert.throws(() => c.videoDocuments({ desc: "x" }, null), /no aweme_id/);
});

test("times are read as seconds, and zero or junk is no time", () => {
  assert.equal(c.seconds(1700000000), "2023-11-14T22:13:20.000Z");
  assert.equal(c.seconds(0), undefined);
  assert.equal(c.seconds("x"), undefined);
});

test("douyin: whoever wrote a post made it", () => {
  const { authorshipOf: authorship } = loadConversion(`${here}../../../../cli/collector-examples/douyin/index.mjs`, ["authorshipOf"]);
  assert.deepEqual(authorship({ type: "Note", id: "douyin:video:1", attributedTo: "douyin:user:2", published: "2026-11-01T00:00:00Z" }), { type: "Create", actor: "douyin:user:2", object: "douyin:video:1", published: "2026-11-01T00:00:00Z" });
  assert.deepEqual(authorship({ type: "Video", id: "douyin:video:1", attributedTo: "douyin:user:2" }), { type: "Create", actor: "douyin:user:2", object: "douyin:video:1" });
  assert.equal(authorship({ type: "Person", id: "douyin:user:2", name: "someone" }), undefined, "a person is not a post");
  assert.equal(authorship({ type: "Note", id: "douyin:video:1" }), undefined, "a post nobody is known to have written");
});

test("the video to read again is named by the target the device gives, and by nothing else", () => {
  assert.equal(c.awemeIdOf("douyin:video:7300000000000000000"), "7300000000000000000");
  for (const bad of ["", "x:post:1", "douyin:video:", "douyin:video:12a", "douyin:video:1/../2", "douyin:user:1", "douyin:collection:favorites"]) assert.equal(c.awemeIdOf(bad), "");
});

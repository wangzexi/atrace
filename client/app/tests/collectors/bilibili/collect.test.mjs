// Replays synthetic Bilibili videos, folders and dynamics through the collector's
// conversion half and compares the documents with the synthetic ones.
// Run: bun test client/app/tests/collectors/
// Synthetic items are cut down to the fields the conversion reads. Fixtures with
// a "note" are constructed because no synthetic example exists yet.
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { contractProblems, loadConversion } from "../testlib.mjs";

const here = new URL(".", import.meta.url).pathname;
const c = loadConversion(`${here}../../../../cli/collector-examples/bilibili/index.mjs`, [
  "videoDocuments", "dynamicDocuments", "collectionDocument", "likeDocument", "coinDocument", "coinsCollectionDocument", "addDocument", "isVideoMedia", "imageOf", "seconds", "targetOf",
]);
const fixture = name => JSON.parse(readFileSync(`${here}testdata/${name}.json`, "utf8"));

function convert({ kind, input }) {
  switch (kind) {
    case "video": {
      const activity = input.mode === "favorite"
        ? c.addDocument(input.meId, input.video, "bilibili:collection:1")
        : input.mode === "coin" ? c.coinDocument(input.meId, input.video) : c.likeDocument(input.meId, input.video);
      return [...c.videoDocuments(input.video), { role: "activity", document: activity }];
    }
    case "collection": return [{ role: "collection", document: c.collectionDocument(input.folder, input.meId) }];
    case "dynamic": return c.dynamicDocuments(input.item, input.meId).entries;
    default: throw new Error(`unknown fixture kind ${kind}`);
  }
}

for (const file of readdirSync(`${here}testdata`).filter(name => name.endsWith(".json"))) {
  const synthetic = JSON.parse(readFileSync(`${here}testdata/${file}`, "utf8"));
  test(`bilibili ${file}`, () => {
    const entries = convert(synthetic);
    assert.deepEqual(entries, synthetic.expected);
    assert.deepEqual(convert(synthetic), entries, "converting twice must give the same documents");
    assert.deepEqual(contractProblems(entries, "bilibili"), []);
  });
}

const activity = name => convert(fixture(name)).find(entry => entry.role === "activity").document;

test("a coin is a keepsake in the coins collection, dated by when it was given; a plain like has no time", () => {
  const coin = activity("video-coin");
  assert.equal(coin.type, "Add");
  assert.equal(coin.target, "bilibili:collection:coins");
  assert.ok(coin.published);
  assert.equal(activity("video-like").type, "Like");
  assert.equal(activity("video-like").published, undefined);
});

test("a favorite is dated by when it was favorited and points at its folder", () => {
  const add = activity("video-favorite");
  assert.equal(add.type, "Add");
  assert.ok(add.published);
  assert.equal(add.target, "bilibili:collection:1");
});

test("a video is a Video of its uploader with its cover as an attachment to download", () => {
  const entries = convert(fixture("video-coin"));
  const video = entries.find(entry => entry.role === "post");
  assert.equal(video.document.type, "Video");
  assert.match(video.document.url, /^https:\/\/www\.bilibili\.com\/video\/BV/);
  assert.deepEqual(video.downloads, video.document.attachment.map(image => image.url));
});

test("a forward with words of the user's own is their post quoting the original, which is kept as a Note of its own author", () => {
  const entries = convert(fixture("forward"));
  assert.deepEqual(entries.map(entry => entry.role), ["orig-author", "orig", "post", "create"]);
  const note = entries.find(entry => entry.role === "post").document;
  assert.equal(note.type, "Note");
  assert.equal(note.quote, entries.find(entry => entry.role === "orig").document.id);
  assert.ok(note.content);
});

test("a forward without words, only Bilibili's stand-in text, is a plain Announce of the original", () => {
  const entries = convert(fixture("forward-plain"));
  assert.deepEqual(entries.map(entry => entry.role), ["orig-author", "orig", "post"]);
  const announce = entries.find(entry => entry.role === "post").document;
  assert.equal(announce.type, "Announce");
  assert.equal(announce.content, undefined);
});

test("the uploader's description of a video is its summary, not its text", () => {
  const video = convert(fixture("video-like")).find(entry => entry.role === "post").document;
  assert.equal(video.content, undefined);
  assert.ok(video.name);
});

test("a video without a description has no summary: Bilibili's lone \"-\" is no text", () => {
  const { input } = fixture("video-like");
  const summaryOf = patch => c.videoDocuments({ ...input.video, ...patch }).find(entry => entry.role === "post").document.summary;
  assert.equal(summaryOf({ desc: "-" }), undefined);
  assert.equal(summaryOf({ desc: " - ", intro: undefined }), undefined);
  assert.equal(summaryOf({ desc: "about the talk" }), "about the talk");
});

test("an own picture dynamic is a Note with only Bilibili-hosted images, and a Create", () => {
  const entries = convert(fixture("dynamic"));
  assert.deepEqual(entries.map(entry => entry.role), ["post", "create"]);
  const note = entries[0].document;
  assert.deepEqual(note.attachment, [{ type: "Image", url: "https://i0.hdslb.com/bfs/new_dyn/a.jpg" }]);
});

test("someone else's dynamic is not stored", () => {
  const { input } = fixture("dynamic");
  assert.equal(c.dynamicDocuments(input.item, "999"), null);
});

test("only plain videos with a bvid are taken from a folder", () => {
  assert.equal(c.isVideoMedia({ type: 2, bvid: "BV1" }), true);
  assert.equal(c.isVideoMedia({ type: 12, bvid: "BV1" }), false);
  assert.equal(c.isVideoMedia({ type: 2 }), false);
});

test("images from other hosts are not taken, protocol-relative addresses become https", () => {
  assert.equal(c.imageOf("https://evil.example/a.jpg"), null);
  assert.equal(c.imageOf(""), null);
  assert.deepEqual(c.imageOf("//i0.hdslb.com/bfs/a.jpg"), { type: "Image", url: "https://i0.hdslb.com/bfs/a.jpg" });
});

test("a video must have a bvid", () => {
  assert.throws(() => c.videoDocuments({ title: "x" }), /no bvid/);
});

test("times are read as seconds, and zero or junk is no time", () => {
  assert.equal(c.seconds(1700000000), "2023-11-14T22:13:20.000Z");
  assert.equal(c.seconds(0), undefined);
  assert.equal(c.seconds("x"), undefined);
});

test("bilibili: whoever wrote a post made it", () => {
  const { authorshipOf: authorship } = loadConversion(`${here}../../../../cli/collector-examples/bilibili/index.mjs`, ["authorshipOf"]);
  assert.deepEqual(authorship({ type: "Note", id: "bilibili:dynamic:1", attributedTo: "bilibili:user:2", published: "2026-11-01T00:00:00Z" }), { type: "Create", actor: "bilibili:user:2", object: "bilibili:dynamic:1", published: "2026-11-01T00:00:00Z" });
  assert.deepEqual(authorship({ type: "Video", id: "bilibili:dynamic:1", attributedTo: "bilibili:user:2" }), { type: "Create", actor: "bilibili:user:2", object: "bilibili:dynamic:1" });
  assert.equal(authorship({ type: "Person", id: "bilibili:user:2", name: "someone" }), undefined, "a person is not a post");
  assert.equal(authorship({ type: "Note", id: "bilibili:dynamic:1" }), undefined, "a post nobody is known to have written");
});

test("what to read again is named by the target the device gives, and by nothing else", () => {
  assert.deepEqual(c.targetOf("bilibili:video:BV1xx411c7mD"), { type: "video", id: "BV1xx411c7mD" });
  assert.deepEqual(c.targetOf("bilibili:dynamic:1234567890123"), { type: "dynamic", id: "1234567890123" });
  for (const bad of ["", "x:post:1", "bilibili:video:", "bilibili:video:BV1", "bilibili:video:av123", "bilibili:dynamic:12a", "bilibili:dynamic:1/../2", "bilibili:user:1", "bilibili:collection:coins"]) assert.equal(c.targetOf(bad), null);
});

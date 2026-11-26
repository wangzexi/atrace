// Replays synthetic Zhihu entries through the collector's conversion half and
// compares the documents with the synthetic ones. Run: bun test client/app/tests/collectors/
// Synthetic entries are cut down to the fields the conversion reads. The text of
// an answer or article is the page's own HTML-to-text output, synthetic in the
// fixture as "body". Fixtures with a "note" are constructed from a synthetic one.
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { contractProblems, loadConversion } from "../testlib.mjs";

const here = new URL(".", import.meta.url).pathname;
const c = loadConversion(`${here}../../../../cli/collector-examples/zhihu/index.mjs`, [
  "contentDocuments", "collectionDocument", "pinBody", "voteRows", "likeDocument", "addDocument", "createDocument", "highlightDocument", "highlightCollectionDocument", "timestamp", "platformID", "canonicalImage", "targetOf",
]);
const fixture = name => JSON.parse(readFileSync(`${here}testdata/${name}.json`, "utf8"));

function convert({ kind, input }) {
  switch (kind) {
    case "content": return c.contentDocuments(input.entry, input.body, input.actor).entries;
    case "collection": return [{ role: "collection", document: c.collectionDocument(input.folder, input.actor) }];
    default: throw new Error(`unknown fixture kind ${kind}`);
  }
}

for (const file of readdirSync(`${here}testdata`).filter(name => name.endsWith(".json"))) {
  const synthetic = JSON.parse(readFileSync(`${here}testdata/${file}`, "utf8"));
  test(`zhihu ${file}`, () => {
    const entries = convert(synthetic);
    assert.deepEqual(entries, synthetic.expected);
    assert.deepEqual(convert(synthetic), entries, "converting twice must give the same documents");
    assert.deepEqual(contractProblems(entries, "zhihu"), []);
  });
}

const post = name => convert(fixture(name)).find(entry => entry.role === "post").document;

test("an answer is a Note under its question, an article an Article on its own page, a pin a Note", () => {
  assert.equal(post("answer").type, "Note");
  assert.match(post("answer").url, /^https:\/\/www\.zhihu\.com\/question\/\d+\/answer\/\d+$/);
  assert.equal(post("article").type, "Article");
  assert.match(post("article").url, /^https:\/\/zhuanlan\.zhihu\.com\/p\/\d+$/);
  assert.match(post("pin").url, /^https:\/\/www\.zhihu\.com\/pin\/\d+$/);
});

test("another person's answer stores its author, the user's own does not", () => {
  assert.deepEqual(convert(fixture("answer")).map(entry => entry.role), ["author", "post"]);
  assert.deepEqual(convert(fixture("answer-by-user")).map(entry => entry.role), ["post"]);
});

test("a pin's body is built from its text and image blocks", () => {
  const { input } = fixture("pin");
  assert.deepEqual(c.pinBody(input.entry), input.body);
  assert.ok(input.body.attachment.every(image => image.url.startsWith("https://")));
});

test("images served from rotating hosts are stored on one host, so a re-collected answer is unchanged", () => {
  const images = post("answer-rotating-hosts").attachment;
  assert.deepEqual(images.map(image => new URL(image.url).hostname), ["pic1.zhimg.com", "pic1.zhimg.com"]);
  assert.equal(c.canonicalImage({ type: "Image", url: "https://pic3.zhimg.com/a.jpg" }).url, "https://pic1.zhimg.com/a.jpg");
});

test("only the first images are kept as local copies", () => {
  const { input } = fixture("answer");
  const many = { ...input.body, attachment: Array.from({ length: 12 }, (_, index) => ({ type: "Image", url: `https://pic1.zhimg.com/${index}.jpg` })) };
  const entry = c.contentDocuments(input.entry, many, input.actor).entries.find(item => item.role === "post");
  assert.equal(entry.document.attachment.length, 12);
  assert.equal(entry.downloads.length, 9);
});

test("deleted entries, other kinds and entries without an ID are not stored", () => {
  const { input } = fixture("answer");
  assert.equal(c.contentDocuments({ ...input.entry, is_deleted: true }, input.body, input.actor), null);
  assert.equal(c.contentDocuments({ ...input.entry, type: "zvideo" }, input.body, input.actor), null);
  assert.equal(c.contentDocuments({ ...input.entry, id: "bad id!" }, input.body, input.actor), null);
});

test("votes are taken only from the user's own vote rows on matching targets", () => {
  const me = "abc";
  const rows = [
    { verb: "MEMBER_VOTEUP_ANSWER", actor: { id: me }, target: { type: "answer", id: 1 } },
    { verb: "MEMBER_VOTEUP_ANSWER", actor: { id: "other" }, target: { type: "answer", id: 2 } },
    { verb: "MEMBER_VOTEUP_ANSWER", actor: { id: me }, target: { type: "article", id: 3 } },
    { verb: "MEMBER_CREATE_ANSWER", actor: { id: me }, target: { type: "answer", id: 4 } },
    { verb: "MEMBER_VOTE_PIN", actor: { id: me }, target: { type: "pin", id: 5 } },
  ];
  assert.deepEqual(c.voteRows(rows, me).map(row => row.target.id), [1, 5]);
});

test("times are read from seconds or ISO strings, and junk is no time", () => {
  assert.equal(c.timestamp(1700000000), "2023-11-14T22:13:20.000Z");
  assert.equal(c.timestamp("2023-11-14T22:13:20Z"), "2023-11-14T22:13:20.000Z");
  assert.equal(c.timestamp(""), undefined);
  assert.equal(c.timestamp("nonsense"), undefined);
  assert.equal(c.timestamp(null), undefined);
});

test("IDs are accepted only as plain tokens or positive integers", () => {
  assert.equal(c.platformID(12345), "12345");
  assert.equal(c.platformID("a_b-9"), "a_b-9");
  assert.equal(c.platformID("../x"), "");
  assert.equal(c.platformID(-1), "");
});

test("an underlined sentence is a Note quoting its answer, in one collection of highlights", () => {
  const segment = {id: "2099500473053197557", content: " 我们原无多大的不同。 ", source_content: {type: "answer", id: "802723318", token: "2098382313503699019", url: "https://www.zhihu.com/answer/2098382313503699019"}, created_at: 0};
  assert.deepEqual(c.highlightDocument(segment, "zhihu:user:1"), {
    type: "Note", id: "zhihu:highlight:2099500473053197557", content: "我们原无多大的不同。", attributedTo: "zhihu:user:1",
    url: "https://www.zhihu.com/answer/2098382313503699019", quote: "zhihu:answer:2098382313503699019",
  });
  assert.equal(c.highlightDocument({...segment, content: ""}, "zhihu:user:1"), null);
  assert.equal(c.highlightDocument({...segment, source_content: {type: "pin", id: "1"}}, "zhihu:user:1"), null);
  const collection = c.highlightCollectionDocument({url_token: "me"}, "zhihu:user:1");
  assert.equal(collection.id, "zhihu:collection:highlights");
  assert.equal(collection.name, "知乎划线");
});

// A source that cannot be had is skipped; a refusal is not, so a block is not taken for a few deleted answers.
test("a source that went away is told from Zhihu refusing", () => {
  const { sourceUnavailable } = loadConversion(`${here}../../../../cli/collector-examples/zhihu/index.mjs`, ["sourceUnavailable"]);
  for (const gone of [
    'page.fetch uses window.fetch and obeys browser CORS. Request "https://www.zhihu.com/api/v4/answers/1?include=content" from "https://www.zhihu.com" failed: TypeError: Failed to fetch',
    "Zhihu /api/v4/answers/1: HTTP 404",
    "Zhihu /api/v4/articles/1: HTTP 410",
  ]) assert.equal(sourceUnavailable(new Error(gone)), true, gone);
  for (const refusal of ["Zhihu /api/v4/answers/1: HTTP 403", "Zhihu /api/v4/answers/1: HTTP 429", "Sign in to Zhihu in the Ego profile before collecting", "Zhihu requires login to read activity history", "Zhihu /api/v4/answers/1: HTTP 4040"]) {
    assert.equal(sourceUnavailable(new Error(refusal)), false, refusal);
  }
});

test("zhihu: whoever wrote a post made it", () => {
  const { authorshipOf: authorship } = loadConversion(`${here}../../../../cli/collector-examples/zhihu/index.mjs`, ["authorshipOf"]);
  assert.deepEqual(authorship({ type: "Note", id: "zhihu:answer:1", attributedTo: "zhihu:user:2", published: "2026-11-01T00:00:00Z" }), { type: "Create", actor: "zhihu:user:2", object: "zhihu:answer:1", published: "2026-11-01T00:00:00Z" });
  assert.deepEqual(authorship({ type: "Video", id: "zhihu:answer:1", attributedTo: "zhihu:user:2" }), { type: "Create", actor: "zhihu:user:2", object: "zhihu:answer:1" });
  assert.equal(authorship({ type: "Person", id: "zhihu:user:2", name: "someone" }), undefined, "a person is not a post");
  assert.equal(authorship({ type: "Note", id: "zhihu:answer:1" }), undefined, "a post nobody is known to have written");
  assert.equal(authorship({ type: "Note", id: "zhihu:highlight:1", attributedTo: "zhihu:user:2", quote: "zhihu:answer:9" }), undefined, "a kept note was not published");
});

test("what to read again is named by the target the device gives, and by nothing else", () => {
  assert.deepEqual(c.targetOf("zhihu:answer:2100366994562348490"), { type: "answer", id: "2100366994562348490" });
  assert.deepEqual(c.targetOf("zhihu:article:1234"), { type: "article", id: "1234" });
  assert.deepEqual(c.targetOf("zhihu:pin:1234"), { type: "pin", id: "1234" });
  for (const bad of ["", "x:post:123", "zhihu:question:1", "zhihu:answer:", "zhihu:answer:12/../3", "zhihu:answer:a b", "zhihu:user:1", "zhihu:highlight:abc"]) assert.equal(c.targetOf(bad), null);
});

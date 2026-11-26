// Replays synthetic blog READMEs through the collector's conversion half and
// compares the documents with the synthetic ones. Run: bun test client/app/tests/collectors/
// Fixtures with a "note" are constructed to cover a case no article has yet.
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { contractProblems, loadConversion } from "../testlib.mjs";

const here = new URL(".", import.meta.url).pathname;
const file = `${here}collector.mjs`;
const globals = { ATRACE_CONFIG: {"repository":"example/blog","branch":"main","authorId":"github:user:example","authorName":"Example Author","authorUrl":"https://example.com/"} };
const c = loadConversion(file, ["articleDocuments", "parseArticle", "timeOf", "encodePath", "readmePaths", "authorDocument"], { globals });
const { parseTar } = loadConversion(file, ["parseTar"], { uncloned: ["parseTar"], globals });
const fixture = name => JSON.parse(readFileSync(`${here}testdata/${name}.json`, "utf8"));

for (const name of readdirSync(`${here}testdata`).filter(entry => entry.endsWith(".json"))) {
  const synthetic = JSON.parse(readFileSync(`${here}testdata/${name}`, "utf8"));
  test(`blog ${name}`, async () => {
    const { input } = synthetic;
    const result = await c.articleDocuments(input.directory, input.source);
    assert.deepEqual(result.entries, synthetic.expected);
    assert.deepEqual(result.images, synthetic.expectedImages);
    assert.deepEqual(await c.articleDocuments(input.directory, input.source), result, "converting twice must give the same documents");
    assert.deepEqual(contractProblems(result.entries, "github"), []);
  });
}

const article = async name => (await c.articleDocuments(fixture(name).input.directory, fixture(name).input.source));

test("an article is an Article with its Markdown source, and a Create by the author", async () => {
  const { entries } = await article("article-with-image");
  assert.deepEqual(entries.map(entry => entry.role), ["post", "create"]);
  const post = entries[0].document;
  assert.equal(post.type, "Article");
  assert.equal(post.mediaType, "text/markdown");
  assert.equal(post.attributedTo, c.authorDocument().id);
  assert.match(post.url, /^https:\/\/github\.com\/example\/blog\/tree\/main\//);
});

test("the ID is the front matter id when there is one, otherwise derived from the directory", async () => {
  assert.equal((await article("article-front-matter-id")).entries[0].document.id, "github:article:abc123");
  assert.match((await article("article-no-front-matter")).entries[0].document.id, /^github:article:[0-9a-f]{12}$/);
});

test("the title is the first heading, else the directory name", async () => {
  assert.equal((await article("article-front-matter-id")).entries[0].document.name, "A title");
  assert.equal((await c.articleDocuments("fallback dir", "no heading here")).entries[0].document.name, "fallback dir");
});

test("times come from the front matter in any offset", async () => {
  const post = (await article("article-front-matter-id")).entries[0].document;
  assert.equal(post.published, "2026-01-01T19:04:05.000Z");
  assert.equal(post.updated, "2026-02-03T04:05:06.000Z");
  assert.equal(c.timeOf("junk"), undefined);
});

test("images: spaced names decoded and encoded back, repeats once, img tags taken, outside images not", async () => {
  const { images, entries } = await article("article-images");
  assert.deepEqual(images.map(image => image.relative), ["assets/a b.png", "assets/c.JPG", "assets/d.svg"]);
  assert.deepEqual(images.map(image => image.mime), ["image/png", "image/jpeg", "image/svg+xml"]);
  assert.equal(images[0].url, "https://raw.githubusercontent.com/example/blog/main/pics/assets/a%20b.png");
  assert.deepEqual(entries[0].document.attachment.map(item => item.url), images.map(image => image.url));
});

test("front matter is optional", () => {
  assert.deepEqual(c.parseArticle("# T\n\nbody"), { meta: {}, body: "# T\n\nbody" });
  assert.deepEqual(c.parseArticle('---\na: "b"\n---\nbody'), { meta: { a: "b" }, body: "body" });
});

// A minimal tar archive: one ustar header per file, content padded to 512 bytes.
function tar(files) {
  const parts = [];
  const header = (name, size, type, prefix = "") => {
    const block = new Uint8Array(512);
    const put = (text, at) => block.set(new TextEncoder().encode(text), at);
    put(name, 0);
    put(size.toString(8).padStart(11, "0"), 124);
    put(type, 156);
    put(prefix, 345);
    return block;
  };
  const add = (block, body = new Uint8Array(0)) => {
    parts.push(block, body, new Uint8Array(Math.ceil(body.length / 512) * 512 - body.length));
  };
  for (const { name, text, type = "0", prefix } of files) {
    const body = new TextEncoder().encode(text);
    add(header(name, body.length, type, prefix), body);
  }
  const all = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0) + 1024);
  let at = 0;
  for (const part of parts) { all.set(part, at); at += part.length; }
  return all;
}

test("a tar archive is read by path without its top-level directory, with pax and prefix long names", () => {
  const long = `repo-main/${"d".repeat(120)}/README.md`;
  const archive = tar([
    { name: "repo-main/", text: "", type: "5" },
    { name: "repo-main/a/README.md", text: "A" },
    { name: "pax", text: `${String(`xx path=${long}\n`.length + 3).padStart(2, "0")} path=${long}\n`, type: "x" },
    { name: "truncated", text: "B" },
    { name: "b/README.md", text: "C", prefix: "repo-main/p" },
  ]);
  const files = parseTar(archive);
  const text = path => new TextDecoder().decode(files.get(path));
  assert.equal(text("a/README.md"), "A");
  assert.equal(text(`${"d".repeat(120)}/README.md`), "B");
  assert.equal(text("p/b/README.md"), "C");
  // only a README directly under a top-level directory is an article
  assert.deepEqual(c.readmePaths(files).sort(), ["a/README.md", `${"d".repeat(120)}/README.md`].sort());
});

// Replays synthetic GitHub star cards through the collector's conversion half and
// compares the documents with the synthetic ones. Run: bun test client/app/tests/collectors/
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { contractProblems, loadConversion } from "../testlib.mjs";

const here = new URL(".", import.meta.url).pathname;
const c = loadConversion(`${here}../../../../cli/collector-examples/github/index.mjs`, [
  "cardOf", "repoOfHref", "repoDocuments", "addDocument", "personDocument", "activityKey", "authorshipOf", "targetRepoOf",
]);

function convert({ kind, input }) {
  switch (kind) {
    case "star": {
      const card = c.cardOf(input.item);
      return [...c.repoDocuments(card), { role: "activity", document: c.addDocument(input.meLogin, card) }];
    }
    default: throw new Error(`unknown fixture kind ${kind}`);
  }
}

for (const file of readdirSync(`${here}testdata`).filter(name => name.endsWith(".json"))) {
  const synthetic = JSON.parse(readFileSync(`${here}testdata/${file}`, "utf8"));
  test(`github ${file}`, () => {
    const entries = convert(synthetic);
    assert.deepEqual(entries, synthetic.expected);
    assert.deepEqual(convert(synthetic), entries, "converting twice must give the same documents");
    assert.deepEqual(contractProblems(entries, "github"), []);
  });
}

test("what is not a repository gives no card", () => {
  for (const href of ["/example-user?tab=achievements", "/topics/typescript", "/orgs/example-org/people", "/example-org", "", undefined, "https://example.com/a/b"]) {
    assert.equal(c.cardOf({ href, description: "x" }), null, String(href));
  }
});

test("the star of a repository leaves an Add that the archive can be asked about", () => {
  const card = c.cardOf({ href: "/example-org/example-repo", description: "" });
  assert.deepEqual(c.activityKey([{ document: c.addDocument("example-user", card) }]), {
    kind: "Add", actor: "github:user:example-user", object: "github:repo:example-org/example-repo", target: "github:star:example-user",
  });
});

test("the owner of a repository is its author, and gets the Create", () => {
  const card = c.cardOf({ href: "/example-org/example-repo", description: "" });
  const [, post] = c.repoDocuments(card);
  assert.deepEqual(c.authorshipOf(post.document), { type: "Create", actor: "github:user:example-org", object: "github:repo:example-org/example-repo" });
});

test("only a github:repo target is read again", () => {
  assert.equal(c.targetRepoOf("github:repo:example-org/example-repo"), "example-org/example-repo");
  for (const target of ["github:article:abc", "weibo:post:1", "github:repo:", "github:repo:a", ""]) assert.equal(c.targetRepoOf(target), "", target);
});

test("the stars of an account are one list, named by the account", () => {
  const card = c.cardOf({ href: "/example-org/example-repo", description: "" });
  assert.equal(c.addDocument("example-user", card).target, "github:star:example-user");
  assert.equal(c.addDocument("other-user", card).target, "github:star:other-user");
});

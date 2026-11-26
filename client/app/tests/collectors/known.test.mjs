// What every collector does to read only what is new: a list read newest first is done where it runs
// into what the archive already has. The rule (activityKey, takeUnknown) is the same text in each
// collector, and what an item leaves in the archive (its key) is checked against the synthetic documents.
// Run: bun test client/app/tests/collectors/
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { loadConversion } from "./testlib.mjs";

const here = new URL(".", import.meta.url).pathname;
// Blog is not here: it reads one archive and stores what changed by content, and has no list to walk.
// Each folder is a collector; weibo-user and xiaohongshu-user read another account and walk lists the same way.
const PLATFORMS = ["weibo", "weibo-user", "x", "xiaohongshu", "xiaohongshu-user", "bilibili", "zhihu", "douyin", "github"];
const rule = Object.fromEntries(PLATFORMS.map(platform => [platform, loadConversion(`${here}../../../cli/collector-examples/${platform}/index.mjs`, ["activityKey", "newestFirst", "takeUnknown", "KNOWN_RUN"], { uncloned: ["KNOWN_RUN", "newestFirst", "takeUnknown"], ...(platform.startsWith("weibo") ? { assign: { WEIBO_UID: "1000000001" } } : {}) })]));

for (const platform of PLATFORMS) {
  const { activityKey, newestFirst } = rule[platform];
  // The collector's code runs in a context of its own: what it gives back is copied to plain data to compare.
  const take = (state, list, known) => JSON.parse(JSON.stringify(rule[platform].takeUnknown(state, list, known)));
  const items = n => Array.from({ length: n }, (_, i) => i);

  test(`${platform}: a list with nothing stored is read to its end`, () => {
    const state = newestFirst();
    assert.deepEqual(take(state, items(20), items(20).map(() => false)), items(20));
    assert.equal(state.done, false);
  });

  test(`${platform}: stored items at the top or between new ones do not end the list`, () => {
    const state = newestFirst();
    // 4 stored, a new one, 4 stored, a new one: the run starts over each time.
    const known = [true, true, true, true, false, true, true, true, true, false];
    assert.deepEqual(take(state, items(10), known), [4, 9]);
    assert.equal(state.done, false);
  });

  test(`${platform}: the run goes on across pages`, () => {
    const state = newestFirst();
    assert.deepEqual(take(state, items(3), [false, true, true]), [0]);
    assert.equal(state.done, false);
    assert.deepEqual(take(state, items(3), [true, true, true, false]), []);
    assert.equal(state.done, true);
  });

  test(`${platform}: an item that leaves no activity is kept and counts for neither`, () => {
    const state = newestFirst();
    const known = [true, true, null, true, true, true];
    assert.deepEqual(take(state, items(6), known), [2]);
    assert.equal(state.done, true);
    const fresh = newestFirst();
    assert.deepEqual(take(fresh, items(3), [false, null, false]), items(3));
    assert.equal(fresh.done, false);
  });

  test(`${platform}: the key of an item is its activity, as /collect/known asks`, () => {
    const entries = [
      { role: "author", document: { type: "Person", id: `${platform}:user:1` } },
      { role: "post", document: { type: "Note", id: `${platform}:post:1` } },
      { role: "activity", document: { type: "Add", actor: `${platform}:user:1`, object: `${platform}:post:1`, target: `${platform}:collection:c` } },
    ];
    assert.deepEqual(activityKey(entries), { kind: "Add", actor: `${platform}:user:1`, object: `${platform}:post:1`, target: `${platform}:collection:c` });
    // A repost is asked by its own ID.
    assert.deepEqual(activityKey([{ role: "post", document: { type: "Announce", id: `${platform}:post:9`, actor: `${platform}:user:1`, object: `${platform}:post:1` } }]),
      { kind: "Announce", actor: `${platform}:user:1`, object: `${platform}:post:1`, id: `${platform}:post:9` });
    assert.equal(activityKey([{ role: "post", document: { type: "Note", id: `${platform}:post:1` } }]), null);
  });
}

// What an item leaves in the archive, worked out from the item alone, is what recording it left.
function synthetic(file) {
  return JSON.parse(readFileSync(file, "utf8"));
}

function keyOfExpected(activityKey, entries) {
  return activityKey(entries);
}

test("weibo: the key of a post and of a like is the activity its synthetic documents hold", () => {
  const c = loadConversion(`${here}../../../cli/collector-examples/weibo/index.mjs`, ["statusKey", "reactionKey", "likeDocument", "addDocument", "activityKey"], { assign: { WEIBO_UID: "1000000001" } });
  let checked = 0;
  for (const file of readdirSync(`${here}weibo/testdata`)) {
    const fixture = synthetic(`${here}weibo/testdata/${file}`);
    if (fixture.kind === "post" && fixture.input.status && !fixture.input.longtext) {
      assert.deepEqual(c.statusKey(fixture.input.status), keyOfExpected(c.activityKey, fixture.expected), file);
      checked += 1;
    }
    if (fixture.kind === "like") {
      assert.deepEqual(c.reactionKey(fixture.input.status, c.likeDocument), keyOfExpected(c.activityKey, fixture.expected), file);
      checked += 1;
    }
    if (fixture.kind === "favorite") {
      assert.deepEqual(c.reactionKey(fixture.input.status, c.addDocument), keyOfExpected(c.activityKey, fixture.expected), file);
      checked += 1;
    }
  }
  assert.ok(checked >= 4, `only ${checked} fixtures checked`);
  // A repost with no words of its own is an Announce asked by its own ID; one with words is a Create.
  const plain = synthetic(`${here}weibo/testdata/repost-plain.json`);
  assert.equal(c.statusKey(plain.input.status).kind, "Announce");
  assert.match(c.statusKey(plain.input.status).id, /^weibo:post:/);
  assert.equal(c.statusKey(synthetic(`${here}weibo/testdata/repost.json`).input.status).kind, "Create");
});

test("x: the key of a tweet is the activity its synthetic documents hold; context and follow-ups have none", () => {
  const c = loadConversion(`${here}../../../cli/collector-examples/x/index.mjs`, ["tweetKey", "reactionKey", "likeDocument", "addDocument", "activityKey"]);
  let keyed = 0;
  for (const file of readdirSync(`${here}x/testdata`)) {
    const fixture = synthetic(`${here}x/testdata/${file}`);
    if (fixture.kind === "tweet") {
      const expected = keyOfExpected(c.activityKey, fixture.expected);
      assert.deepEqual(c.tweetKey(fixture.input.tweet, fixture.input.me), expected, file);
      if (expected) keyed += 1;
    }
    if (fixture.kind === "like") assert.deepEqual(c.reactionKey(fixture.input.tweet, object => c.likeDocument(fixture.input.me, object)), keyOfExpected(c.activityKey, fixture.expected), file);
    if (fixture.kind === "bookmark") assert.deepEqual(c.reactionKey(fixture.input.tweet, object => c.addDocument(fixture.input.me, object)), keyOfExpected(c.activityKey, fixture.expected), file);
  }
  assert.ok(keyed >= 4, `only ${keyed} tweets with an activity`);
  assert.equal(c.tweetKey(synthetic(`${here}x/testdata/context.json`).input.tweet, synthetic(`${here}x/testdata/context.json`).input.me), null);
  assert.equal(c.tweetKey(synthetic(`${here}x/testdata/reply-to-self.json`).input.tweet, synthetic(`${here}x/testdata/reply-to-self.json`).input.me), null);
});

test("xiaohongshu: the key of an own note, a like and a favorite is the activity its synthetic documents hold", () => {
  const c = loadConversion(`${here}../../../cli/collector-examples/xiaohongshu/index.mjs`, ["ownCardKey", "reactionKey", "likeDocument", "addDocument", "activityKey"]);
  let checked = 0;
  for (const file of readdirSync(`${here}xhs/testdata`)) {
    const { kind, input, expected } = synthetic(`${here}xhs/testdata/${file}`);
    const key = { "own-note": () => c.ownCardKey(input.card, input.meId), like: () => c.reactionKey(input.card, input.meId, object => c.likeDocument(input.meId, object)), favorite: () => c.reactionKey(input.card, input.meId, object => c.addDocument(input.meId, object)) }[kind];
    if (!key) continue;
    assert.deepEqual(key(), keyOfExpected(c.activityKey, expected), file);
    checked += 1;
  }
  assert.equal(checked, 5);
});

test("bilibili: the key of a coin, a like, a favorite and a dynamic is the activity its synthetic documents hold", () => {
  const c = loadConversion(`${here}../../../cli/collector-examples/bilibili/index.mjs`, ["keyOfDocument", "dynamicKey", "addDocument", "coinDocument", "likeDocument", "activityKey"]);
  let checked = 0;
  for (const file of readdirSync(`${here}bilibili/testdata`)) {
    const { kind, input, expected } = synthetic(`${here}bilibili/testdata/${file}`);
    if (kind === "video") {
      const document = input.mode === "favorite" ? c.addDocument(input.meId, input.video, "bilibili:collection:1") : input.mode === "coin" ? c.coinDocument(input.meId, input.video) : c.likeDocument(input.meId, input.video);
      assert.deepEqual(c.keyOfDocument(document), keyOfExpected(c.activityKey, expected.concat([{ role: "activity", document }])), file);
      checked += 1;
    }
    if (kind === "dynamic") {
      assert.deepEqual(c.dynamicKey(input.item, input.meId), keyOfExpected(c.activityKey, expected), file);
      checked += 1;
    }
  }
  assert.equal(checked, 6);
  // A forward with no words of its own is a repost known by its own ID; one with words is a post of the user's.
  const plain = synthetic(`${here}bilibili/testdata/forward-plain.json`);
  assert.equal(c.dynamicKey(plain.input.item, plain.input.meId).kind, "Announce");
  assert.equal(c.dynamicKey(synthetic(`${here}bilibili/testdata/forward.json`).input.item, plain.input.meId).kind, "Create");
});

test("zhihu: the object of an answer, an article or a pin is the one its documents are stored under", () => {
  const c = loadConversion(`${here}../../../cli/collector-examples/zhihu/index.mjs`, ["objectOf", "createKey", "likeKey", "addKey", "contentDocuments"]);
  let checked = 0;
  for (const file of readdirSync(`${here}zhihu/testdata`)) {
    const { kind, input } = synthetic(`${here}zhihu/testdata/${file}`);
    if (kind !== "content") continue;
    const object = c.contentDocuments(input.entry, input.body, input.actor).object;
    assert.equal(c.objectOf(input.entry), object, file);
    assert.deepEqual(c.createKey(input.actor, input.entry), { kind: "Create", actor: input.actor, object }, file);
    assert.deepEqual(c.likeKey(input.actor, input.entry), { kind: "Like", actor: input.actor, object }, file);
    assert.deepEqual(c.addKey(input.actor, input.entry, "zhihu:collection:1"), { kind: "Add", actor: input.actor, object, target: "zhihu:collection:1" }, file);
    checked += 1;
  }
  assert.equal(checked, 7);
  // What is deleted, or of another kind, leaves nothing to ask about.
  assert.equal(c.objectOf({ id: 1, type: "answer", is_deleted: true }), null);
  assert.equal(c.objectOf({ id: 1, type: "zvideo" }), null);
});

test("douyin: the key of a work, a like and a favorite is the activity its synthetic documents hold", () => {
  const c = loadConversion(`${here}../../../cli/collector-examples/douyin/index.mjs`, ["workKey", "reactionKey", "likeDocument", "addDocument", "activityKey"]);
  let checked = 0;
  for (const file of readdirSync(`${here}douyin/testdata`)) {
    const { kind, input, expected } = synthetic(`${here}douyin/testdata/${file}`);
    const key = { work: () => c.workKey(input.video, input.meId), like: () => c.reactionKey(input.video, object => c.likeDocument(input.meId, object)), favorite: () => c.reactionKey(input.video, object => c.addDocument(input.meId, object)) }[kind];
    assert.deepEqual(key(), keyOfExpected(c.activityKey, expected), file);
    checked += 1;
  }
  assert.equal(checked, 5);
});

for (const platform of PLATFORMS) {
  test(`${platform}: full scan skips stored activities and continues into older gaps`, () => {
    const c = rule[platform];
    const items = Array.from({ length: c.KNOWN_RUN + 2 }, (_, i) => i);
    const collected = new Set();
    for (const day of [items.slice(0, c.KNOWN_RUN), items]) {
      const state = c.newestFirst();
      const known = day.map((item) => collected.has(item));
      const missing = JSON.parse(JSON.stringify(c.takeUnknown(state, day, known, false)));
      assert.deepEqual(missing, day.filter((item) => !collected.has(item)));
      assert.equal(state.done, false);
      missing.forEach((item) => collected.add(item));
    }
    assert.equal(collected.size, items.length);
  });
}

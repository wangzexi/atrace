// Replays synthetic X tweets through the collector's conversion half and
// compares the documents with the synthetic ones. Run: bun test client/app/tests/collectors/
// Fixtures with a "note" are built from real tweets because the account has no
// synthetic example yet.
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { contractProblems, loadConversion } from "../testlib.mjs";

const here = new URL(".", import.meta.url).pathname;
const c = loadConversion(`${here}../../../../cli/collector-examples/x/index.mjs`, [
  "tweetDocuments", "reactionDocuments", "likeDocument", "addDocument", "tweetsOfEntry", "entriesOf", "cursorOf", "unwrap", "postNumberOf", "postPage", "replyDocuments"]);
const fixture = name => JSON.parse(readFileSync(`${here}testdata/${name}.json`, "utf8"));

function convert({ kind, input }) {
  switch (kind) {
    case "tweet": return c.tweetDocuments(input.tweet, { me: input.me });
    case "like": return { entries: c.reactionDocuments(input.tweet, object => c.likeDocument(input.me, object)) };
    case "bookmark": return { entries: c.reactionDocuments(input.tweet, object => c.addDocument(input.me, object)) };
    default: throw new Error(`unknown fixture kind ${kind}`);
  }
}

for (const file of readdirSync(`${here}testdata`).filter(name => name.endsWith(".json"))) {
  const synthetic = JSON.parse(readFileSync(`${here}testdata/${file}`, "utf8"));
  test(`x ${file}`, () => {
    const result = convert(synthetic);
    assert.deepEqual(result.entries, synthetic.expected);
    if (synthetic.expectedKind) assert.equal(result.kind, synthetic.expectedKind);
    assert.deepEqual(convert(synthetic), result, "converting twice must give the same documents");
    assert.deepEqual(contractProblems(result.entries, "x"), []);
  });
}

const roles = name => convert(fixture(name)).entries.map(entry => entry.role);
const post = name => convert(fixture(name)).entries.find(entry => entry.role === "post").document;

test("an own post is a Note and a Create", () => {
  assert.deepEqual(roles("post"), ["author", "post", "create"]);
});

test("a reply to someone else carries inReplyTo and a Create", () => {
  assert.ok(post("reply").inReplyTo.startsWith("x:post:"));
  assert.ok(roles("reply").includes("create"));
});

test("a reply to the user's own post is a follow-up: a Note without a Create", () => {
  assert.ok(post("reply-to-self").inReplyTo);
  assert.ok(!roles("reply-to-self").includes("create"));
});

test("someone else's tweet shown with a reply is kept as a Note of its author, with no Create", () => {
  const { kind, entries } = convert(fixture("context"));
  assert.equal(kind, "context");
  assert.deepEqual(entries.map(entry => entry.role), ["author", "post"]);
  assert.ok(!post("context").inReplyTo);
});

test("a quote is the user's own post that carries the quoted one, which is kept; it is a Create like any post", () => {
  assert.deepEqual(roles("quote"), ["author", "cited-author", "cited", "post", "create"]);
  const note = post("quote");
  assert.equal(note.type, "Note");
  assert.ok(note.content);
  assert.equal(note.quote, convert(fixture("quote")).entries.find(entry => entry.role === "cited").document.id);
  assert.ok(roles("quote").includes("create"));
});

test("a pure retweet is an Announce without text", () => {
  const { kind } = convert(fixture("retweet"));
  assert.equal(kind, "retweet");
  const announce = post("retweet");
  assert.equal(announce.type, "Announce");
  assert.equal(announce.content, undefined);
});

test("a photo is kept as an Image and a video keeps its mp4 URL", () => {
  const image = convert(fixture("like-image")).entries.find(entry => entry.role === "post").document.attachment;
  assert.ok(image.some(a => a.type === "Image" && a.url.startsWith("https://pbs.twimg.com/")));
  const video = convert(fixture("like-video")).entries.find(entry => entry.role === "post").document.attachment;
  assert.ok(video.some(a => a.type === "Video" && /\.mp4/.test(a.url)));
});

test("a timeline page yields its tweets, unwrapping visibility results and not descending into quotes", () => {
  const { input } = fixture("quote");
  const body = {
    data: {
      instructions: [{
        entries: [
          { entryId: "tweet-1", content: { itemContent: { tweet_results: { result: { __typename: "TweetWithVisibilityResults", tweet: input.tweet } } } } },
          { entryId: "cursor-bottom-1", content: { cursorType: "Bottom", value: "CURSOR" } },
        ],
      }],
    },
  };
  const tweets = c.entriesOf(body).flatMap(entry => c.tweetsOfEntry(entry.content));
  assert.equal(tweets.length, 1, "the quoted tweet inside must not be listed as its own tweet");
  assert.equal(tweets[0].rest_id, input.tweet.rest_id);
  assert.equal(c.cursorOf(body, "Bottom"), "CURSOR");
});

test("an image outside pbs.twimg.com is rejected", () => {
  const { input } = fixture("like-image");
  const tweet = JSON.parse(JSON.stringify(input.tweet));
  for (const media of tweet.legacy.extended_entities?.media || tweet.legacy.entities?.media || []) media.media_url_https = "https://evil.example/a.jpg";
  assert.throws(() => c.reactionDocuments(tweet, object => c.likeDocument(input.me, object)), /Unexpected X image host/);
});

test("x: whoever wrote a post made it", () => {
  const { authorshipOf: authorship } = loadConversion(`${here}../../../../cli/collector-examples/x/index.mjs`, ["authorshipOf"]);
  assert.deepEqual(authorship({ type: "Note", id: "x:post:1", attributedTo: "x:user:2", published: "2026-11-01T00:00:00Z" }), { type: "Create", actor: "x:user:2", object: "x:post:1", published: "2026-11-01T00:00:00Z" });
  assert.deepEqual(authorship({ type: "Video", id: "x:post:1", attributedTo: "x:user:2" }), { type: "Create", actor: "x:user:2", object: "x:post:1" });
  assert.equal(authorship({ type: "Person", id: "x:user:2", name: "someone" }), undefined, "a person is not a post");
  assert.equal(authorship({ type: "Note", id: "x:post:1" }), undefined, "a post nobody is known to have written");
});

// ---- one post: the page of a post, and the replies in its conversation ----

function tweetOf(id, user, parent, text = `text ${id}`) {
  return {
    __typename: "Tweet", rest_id: id,
    core: { user_results: { result: { rest_id: user, core: { screen_name: `u${user}`, name: `User ${user}` } } } },
    legacy: { full_text: text, created_at: "Mon Nov 23 10:00:00 +0000 2026", user_id_str: user, ...(parent ? { in_reply_to_status_id_str: parent } : {}) },
  };
}

const detailOf = (...tweets) => ({
  data: { thread: tweets.map(tweet => ({ entryId: `tweet-${tweet.rest_id}`, content: { itemContent: { tweet_results: { result: tweet } } } })) },
});

test("a post number is read from a target of X and from nothing else", () => {
  assert.equal(c.postNumberOf("x:post:123"), "123");
  for (const bad of ["", "weibo:post:123", "x:post:", "x:post:12a", "x:post:123/../4", "x:user:1"]) assert.equal(c.postNumberOf(bad), "");
});

test("the page of a post gives the post and the replies that lead back to it, deep ones too, in the order listed", () => {
  const detail = detailOf(
    tweetOf("90", "5", undefined, "what the post answers"),
    tweetOf("100", "1", "90", "the post"),
    tweetOf("102", "3", "101", "a reply to a reply, listed first"),
    tweetOf("101", "2", "100", "a reply"),
    tweetOf("200", "4", "999", "a reply to something else"),
  );
  const { post, replies } = c.postPage(detail, "100");
  assert.equal(post.rest_id, "100");
  assert.deepEqual(replies.map(tweet => tweet.rest_id), ["102", "101"]);
});

test("a page that does not hold the post says so, and the replies are cut at the limit", () => {
  assert.equal(c.postPage(detailOf(tweetOf("7", "1")), "100").post, null);
  const many = [tweetOf("100", "1"), ...Array.from({ length: 5 }, (_, i) => tweetOf(`${101 + i}`, "2", "100"))];
  assert.equal(c.postPage(detailOf(...many), "100", 3).replies.length, 3);
});

test("a reply is stored with its author, under the tweet it answers", () => {
  const entries = c.replyDocuments(tweetOf("101", "2", "100"));
  assert.deepEqual(entries.map(entry => entry.role), ["author", "reply"]);
  assert.equal(entries[0].document.id, "x:user:2");
  assert.equal(entries[1].document.id, "x:post:101");
  assert.equal(entries[1].document.inReplyTo, "x:post:100");
  assert.deepEqual(contractProblems(entries, "x"), []);
});

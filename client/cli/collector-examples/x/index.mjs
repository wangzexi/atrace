import { spawn } from 'node:child_process'
import { readFile } from 'node:fs/promises'
// The example version this copy came from; a copy may be edited, so compare by hand.
const VERSION = 1
const config = JSON.parse(await readFile(new URL('config.json', import.meta.url), 'utf8'))
const { ATRACE_COLLECT_TOKEN, ATRACE_SERVER, ATRACE_TARGET, ATRACE_EGO } = process.env
if (!ATRACE_COLLECT_TOKEN) {
  console.error('ATRACE_COLLECT_TOKEN is required')
  process.exit(2)
}
const self = await readFile(new URL(import.meta.url), 'utf8')
const marker = '\n// ==== collector body ====\n'
const source = self.slice(self.lastIndexOf(marker) + marker.length, self.lastIndexOf('\n} // collector body'))
const declarations = {
  ATRACE_TOKEN: ATRACE_COLLECT_TOKEN,
  ATRACE_SERVER: ATRACE_SERVER || 'http://127.0.0.1:8183',
  ATRACE_TARGET: ATRACE_TARGET || '',
  ATRACE_FULL: process.env.ATRACE_FULL === '1',
  ATRACE_CONFIG: config,
}
const preamble = Object.entries(declarations).map(([key, value]) => `const ${key} = ${JSON.stringify(value)};`).join('\n')
const child = spawn(ATRACE_EGO || 'ego-browser', ['nodejs'], { detached: true, stdio: ['pipe', 'inherit', 'inherit'] })
let stopping = false
let escalation
function signalGroup(signal) {
  if (child.pid) {
    try { process.kill(-child.pid, signal) } catch (error) { if (error.code !== 'ESRCH') throw error }
  }
}
function stop() {
  if (stopping) return
  stopping = true
  signalGroup('SIGTERM')
  escalation = setTimeout(() => signalGroup('SIGKILL'), 1500)
}
process.on('SIGTERM', stop)
process.on('SIGINT', stop)
child.on('error', (error) => { console.error(error.message); process.exitCode = 1 })
child.on('close', (code) => {
  if (stopping) signalGroup('SIGKILL')
  clearTimeout(escalation)
  process.exitCode = stopping ? 1 : (code ?? 1)
})
child.stdin.on('error', (error) => { if (error.code !== 'EPIPE') console.error(error.message) })
child.stdin.end(`${preamble}\nasync function checkCollectorAccount(id, name = "") {\n  console.log("ATRACE_ACCOUNT " + id + (name ? " " + name : ""));\n}\n${source}`)

async function collectorBody() {
// ==== collector body ====
// Run with: atrace collect x
// The Ego Lite profile supplies the logged-in X session. No token is read or
// stored: each X page is opened so the page makes its own GraphQL request, and
// that request (URL and headers) is replayed with a cursor to read further
// pages. Everything is written to a local atrace HTTP server as ActivityStreams
// documents:
//   own posts      Note + Create (a reply to someone else is a Note with
//                  inReplyTo + Create; a reply to your own post is only a Note,
//                  shown as a follow-up of that post)
//   quote posts    Announce with the user's own text, quoting the original
//   retweets       Announce of the original, without text
//   likes          Like            bookmarks   Add to x:collection:bookmarks
// Retweets and the user's likes/bookmarks of other people's posts keep the
// post as a Note of its own author. Images are downloaded and registered as
// local copies; videos keep their X URL.
//
// The file has two halves. Above the marker line are the constants and the
// functions that turn X responses into documents; they touch no browser or
// network, and the tests load only that half (collectors/AGENTS.md). Below it
// are the browser, the network and the order things are stored in.

const PROFILE_ID = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.profileId : "Default";
const TASK_SPACE = "atrace x collector";
const OBSERVED_BY = "x-ego";
let SCREEN_NAME = ""; // the signed-in handle, once it is known
const ATRACE_API = typeof ATRACE_SERVER === "string" ? ATRACE_SERVER : "http://127.0.0.1:8183";
// A full crawl (atrace collect <platform> --full puts `ATRACE_FULL=1` in the environment) reads every page and takes longer between them. An ordinary run reads from the newest back
// until it meets what is stored (see KNOWN_RUN); the page limits only bound a first import.
const FULL = typeof ATRACE_FULL !== "undefined" && ATRACE_FULL;
// `ATRACE_TARGET=x:post:123` in the environment reads that one post and the replies under it, and
// nothing else: it is what the device does when the owner opens the post. Without it the whole collection runs.
const TARGET = typeof ATRACE_TARGET === "string" ? ATRACE_TARGET : "";
const OWN_PAGES = FULL ? 1_000_000 : 10;      // pages of own posts and of replies
const REACTION_PAGES = FULL ? 1_000_000 : 3;  // pages of likes and of bookmarks, newest first
const PAGE_DELAY_MIN_MS = FULL ? 20_000 : 6_000;
const PAGE_DELAY_MAX_MS = FULL ? 35_000 : 10_000;
const BOOKMARKS = "x:collection:bookmarks";

// ---- tweet fields ---------------------------------------------------------

// A tweet may be wrapped when visibility rules apply to it.
function unwrap(result) {
  const tweet = result?.__typename === "TweetWithVisibilityResults" ? result.tweet : result;
  return tweet?.legacy ? tweet : null;
}

function userOf(tweet) {
  const user = tweet.core?.user_results?.result;
  const id = String(user?.rest_id || tweet.legacy.user_id_str || "");
  const screenName = user?.core?.screen_name || user?.legacy?.screen_name || "";
  const name = user?.core?.name || user?.legacy?.name || "";
  return { id, screenName, name };
}

function publishedOf(tweet) {
  const timestamp = Date.parse(tweet.legacy.created_at || "");
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : undefined;
}

function decodeEntities(value) {
  return value.replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&amp;/g, "&");
}

// The text a reader sees: long posts in full, the leading @mentions of a reply
// and the trailing media link dropped, t.co links replaced by their targets.
function textOf(tweet) {
  const long = tweet.note_tweet?.note_tweet_results?.result;
  let text = long?.text || tweet.legacy.full_text || "";
  let urls = long?.entity_set?.urls || tweet.legacy.entities?.urls || [];
  const media = tweet.legacy.entities?.media || [];
  if (!long) {
    const range = tweet.legacy.display_text_range;
    if (Array.isArray(range)) text = Array.from(text).slice(range[0], range[1]).join("");
  }
  for (const link of urls) text = text.split(link.url).join(link.expanded_url || link.url);
  for (const item of media) text = text.split(item.url).join("");
  return decodeEntities(text).trim();
}

function statusUrl(tweet) {
  const { screenName } = userOf(tweet);
  return `https://x.com/${screenName || "i"}/status/${tweet.rest_id}`;
}

function mediaOf(tweet) {
  const found = [];
  for (const item of tweet.legacy.extended_entities?.media || tweet.legacy.entities?.media || []) {
    if (item.type === "photo") {
      const url = `${item.media_url_https}?name=large`;
      found.push({ type: "Image", url, host: new URL(url).hostname, width: item.original_info?.width, height: item.original_info?.height });
    } else {
      const variants = (item.video_info?.variants || []).filter(v => v.content_type === "video/mp4");
      variants.sort((a, b) => (b.bitrate || 0) - (a.bitrate || 0));
      if (variants[0]) found.push({ type: "Video", url: variants[0].url, host: new URL(variants[0].url).hostname });
    }
  }
  return found;
}

// Tweets of one timeline entry, not descending into quoted tweets.
function tweetsOfEntry(node, out = []) {
  if (Array.isArray(node)) {
    node.forEach(item => tweetsOfEntry(item, out));
  } else if (node && typeof node === "object") {
    if (node.tweet_results?.result) {
      const tweet = unwrap(node.tweet_results.result);
      if (tweet) out.push(tweet);
      return out;
    }
    Object.values(node).forEach(value => tweetsOfEntry(value, out));
  }
  return out;
}

function entriesOf(body) {
  const entries = [];
  const walk = node => {
    if (Array.isArray(node)) node.forEach(walk);
    else if (node && typeof node === "object") {
      if (typeof node.entryId === "string" && node.content) entries.push(node);
      Object.values(node).forEach(walk);
    }
  };
  walk(body);
  return entries;
}

function cursorOf(body, type) {
  let found = "";
  const walk = node => {
    if (Array.isArray(node)) node.forEach(walk);
    else if (node && typeof node === "object") {
      if (node.cursorType === type && node.value) found = node.value;
      Object.values(node).forEach(walk);
    }
  };
  walk(body);
  return found;
}

function attachmentsOf(tweet) {
  const attachments = [];
  for (const item of mediaOf(tweet)) {
    if (item.type === "Image" && item.host !== "pbs.twimg.com") throw new Error(`Unexpected X image host: ${item.host}`);
    attachments.push({
      type: item.type, url: item.url,
      ...(item.width ? { width: item.width } : {}), ...(item.height ? { height: item.height } : {}),
    });
  }
  return attachments;
}

// An X Article is a tweet whose text is only the link to it; what X hands over of the article
// itself is its title, the opening of the text and the cover picture.
function articleOf(tweet) {
  const article = tweet.article?.article_results?.result;
  if (!article?.title) return null;
  const cover = article.cover_media?.media_info;
  return {
    title: article.title,
    preview: article.preview_text || "",
    ...(cover?.original_img_url ? { cover: { type: "Image", url: cover.original_img_url, width: cover.original_img_width, height: cover.original_img_height } } : {}),
  };
}

function withoutTrailingLink(text, link) {
  const trimmed = text.trimEnd();
  return trimmed.endsWith(link) ? trimmed.slice(0, -link.length).trimEnd() : text;
}

function noteDocument(tweet, attachments, inReplyTo, quote) {
  const user = userOf(tweet);
  const article = articleOf(tweet);
  const pictures = article?.cover ? [article.cover, ...attachments] : attachments;
  return {
    type: "Note",
    id: `x:post:${tweet.rest_id}`,
    ...(user.id ? { attributedTo: `x:user:${user.id}` } : {}),
    ...(inReplyTo ? { inReplyTo } : {}),
    ...(quote ? { quote } : {}),
    ...(article ? { name: article.title } : {}),
    content: article ? article.preview : textOf(tweet),
    url: statusUrl(tweet),
    ...(publishedOf(tweet) ? { published: publishedOf(tweet) } : {}),
    ...(pictures.length ? { attachment: pictures } : {}),
  };
}

// The author of a tweet, or null when the response names no author.
function personDocument(tweet) {
  const user = userOf(tweet);
  if (!user.id) return null;
  return {
    type: "Person",
    id: `x:user:${user.id}`,
    url: `https://x.com/${user.screenName}`,
    ...(user.name ? { name: user.name } : {}),
  };
}

// What one tweet from the user's timelines stores, in storage order; role says
// which raw response each entry is stored with. kind is what the tweet was:
// context (someone else's tweet shown with a reply), retweet, quote, reply or post. For a retweet or quote, cited is the
// original tweet.
function tweetDocuments(tweet, { me }) {
  const meRef = `x:user:${me}`;
  const published = publishedOf(tweet);
  const entries = [{ role: "author", document: personDocument(tweet) }];
  const done = (kind, cited) => ({ kind, entries: entries.filter(entry => entry.document), ...(cited ? { cited } : {}) });

  if (userOf(tweet).id !== me) {
    entries.push({ role: "post", document: noteDocument(tweet, attachmentsOf(tweet)) });
    return done("context");
  }

  const retweeted = unwrap(tweet.legacy.retweeted_status_result?.result);
  if (retweeted) {
    // A pure retweet: an Announce of the original, which is kept as a Note of its own author.
    entries.push(
      { role: "cited-author", document: personDocument(retweeted) },
      { role: "cited", document: noteDocument(retweeted, attachmentsOf(retweeted)) },
      {
        role: "post",
        document: {
          type: "Announce",
          id: `x:post:${tweet.rest_id}`,
          actor: meRef,
          object: `x:post:${retweeted.rest_id}`,
          url: statusUrl(tweet),
          ...(published ? { published } : {}),
        },
      },
    );
    return done("retweet", retweeted);
  }
  const quoted = tweet.legacy.is_quote_status ? unwrap(tweet.quoted_status_result?.result) : null;
  const attachments = attachmentsOf(tweet);
  const parent = tweet.legacy.in_reply_to_status_id_str;
  // A quote is the user's own post that carries the quoted post along (quote), and may also answer
  // another one (inReplyTo).
  if (quoted) {
    entries.push(
      { role: "cited-author", document: personDocument(quoted) },
      { role: "cited", document: noteDocument(quoted, attachmentsOf(quoted)) },
    );
  }
  const replyToSelf = parent && tweet.legacy.in_reply_to_user_id_str === me;
  const post = noteDocument(tweet, attachments, parent ? `x:post:${parent}` : undefined, quoted ? `x:post:${quoted.rest_id}` : undefined);
  // X ends a quote's text with the link of the quoted tweet; the quote is a field of its own.
  if (quoted) post.content = withoutTrailingLink(post.content, statusUrl(quoted));
  entries.push({ role: "post", document: post });
  // A follow-up under the user's own post continues that post, so it is not a
  // separate entry of its own.
  if (!replyToSelf) {
    entries.push({ role: "create", document: { type: "Create", actor: meRef, object: `x:post:${tweet.rest_id}`, ...(published ? { published } : {}) } });
  }
  return quoted ? done("quote", quoted) : done(parent ? "reply" : "post");
}

// An item from the likes or bookmarks list: its author, the post as a Note and
// the activity on it.
function reactionDocuments(tweet, activityOf) {
  return [
    { role: "author", document: personDocument(tweet) },
    { role: "post", document: noteDocument(tweet, attachmentsOf(tweet)) },
    { role: "activity", document: activityOf(`x:post:${tweet.rest_id}`) },
  ].filter(entry => entry.document);
}

function likeDocument(me, object) {
  return { type: "Like", actor: `x:user:${me}`, object };
}

function addDocument(me, object) {
  return { type: "Add", actor: `x:user:${me}`, object, target: BOOKMARKS };
}



// A list read newest first is done where it runs into what the archive already has: KNOWN_RUN items
// in a row that are stored. A pinned old post at the top does not end it, and a run that finds nothing
// stored (the first one) reads on up to the limits below.
const KNOWN_RUN = 5;

// What storing an item leaves in the archive: its activity, in the form /collect/known asks about.
function activityKey(entries) {
  const entry = entries.find(({ document }) => ["Create", "Like", "Add", "Announce"].includes(document.type));
  if (!entry) return null;
  const { type, actor, object, target, id } = entry.document;
  return { kind: type, actor, object, ...(target ? { target } : {}), ...(type === "Announce" && id ? { id } : {}) };
}

function newestFirst() {
  return { run: 0, done: false };
}

// takeUnknown gives the items of a page that are not stored, and says in state.done when the list has
// run into what is stored. known tells for each item whether it is: true, false, or null when there is
// nothing to compare (an item that leaves no activity); such an item is kept and counts for neither.
function takeUnknown(state, items, known, stopOnKnown = true) {
  const fresh = [];
  for (let i = 0; i < items.length; i += 1) {
    if (known[i] === null) {
      fresh.push(items[i]);
    } else if (known[i]) {
      state.run += 1;
      if (stopOnKnown && state.run >= KNOWN_RUN) {
        state.done = true;
        break;
      }
    } else {
      state.run = 0;
      fresh.push(items[i]);
    }
  }
  return fresh;
}

// The activity a tweet leaves in the archive; null for what leaves none (context, a follow-up to oneself).
function tweetKey(tweet, me) {
  try {
    return activityKey(tweetDocuments(tweet, { me }).entries);
  } catch {
    return null;
  }
}

function reactionKey(tweet, activityOf) {
  try {
    return activityKey(reactionDocuments(tweet, activityOf));
  } catch {
    return null;
  }
}

// Whoever wrote a post made it: the Create of its author goes in with the post, for the posts of others as well as
// one's own, and the core does not guess it. A note its author only kept (a sentence underlined in an answer) was
// not published, so it has none.
function authorshipOf(document) {
  if (!["Note", "Article", "Video"].includes(document.type) || !document.id || !document.attributedTo) return undefined;
  return { type: "Create", actor: document.attributedTo, object: document.id, ...(document.published ? { published: document.published } : {}) };
}

// The post number of a target such as "x:post:123", or "" when it is not one.
function postNumberOf(target) {
  return (/^x:post:(\d{1,25})$/.exec(target) || [])[1] || "";
}

// What the page of one post (the TweetDetail answer it gets itself) holds: the post, and the replies in its
// conversation. Every tweet there that answers another is a Note replying to it, as deep as the page shows, with its
// author; only the replies that lead back to the post count, not the tweets the post itself answers. At most limit
// replies, in the order the page lists them. post is null when the answer does not hold the post.
function postPage(detail, postNumber, limit = 100) {
  const tweets = [];
  const seen = new Set();
  for (const entry of entriesOf(detail)) {
    for (const tweet of tweetsOfEntry(entry.content)) {
      if (!tweet.rest_id || seen.has(tweet.rest_id)) continue;
      seen.add(tweet.rest_id);
      tweets.push(tweet);
    }
  }
  const post = tweets.find(tweet => tweet.rest_id === postNumber) || null;
  const replies = tweets.filter(tweet => tweet.legacy.in_reply_to_status_id_str);
  const ids = new Set([postNumber]);
  const inThread = [];
  // A reply may be listed before the one it answers, so the thread is grown until nothing more joins it.
  for (let grown = true; grown;) {
    grown = false;
    for (const tweet of replies) {
      if (!ids.has(tweet.rest_id) && ids.has(tweet.legacy.in_reply_to_status_id_str)) {
        ids.add(tweet.rest_id);
        inThread.push(tweet);
        grown = true;
      }
    }
  }
  const order = new Map(tweets.map((tweet, index) => [tweet.rest_id, index]));
  inThread.sort((a, b) => order.get(a.rest_id) - order.get(b.rest_id));
  return { post, replies: inThread.slice(0, limit) };
}

// A reply of the post's conversation as what is stored: its author and the Note, under the tweet it answers.
function replyDocuments(tweet) {
  return [
    { role: "author", document: personDocument(tweet) },
    { role: "reply", document: noteDocument(tweet, attachmentsOf(tweet), `x:post:${tweet.legacy.in_reply_to_status_id_str}`) },
  ].filter(entry => entry.document);
}

// ==== side effects below: the browser, the network and storage order; not loaded by tests ====

// atrace wants the collect token on every request to it. Whoever runs this script puts
// `ATRACE_TOKEN=…` in the environment.
// A request that finds atrace restarting (connection refused) is tried again a few times.
async function atraceFetch(url, init = {}) {
  if (!String(url).startsWith(ATRACE_API + "/")) return fetch(url, init);
  const sent = { ...init, headers: { ...init.headers, Authorization: `Bearer ${ATRACE_TOKEN}` } };
  for (let attempt = 1; ; attempt += 1) {
    try {
      return await fetch(url, sent);
    } catch (error) {
      if (attempt >= 6) throw error;
      await new Promise(resolve => setTimeout(resolve, 5_000));
    }
  }
}

function pause(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function randomDelay(min, max) {
  return Math.floor(min + Math.random() * (max - min + 1));
}

// ---- atrace ---------------------------------------------------------------

async function requestJson(url, init = {}) {
  const response = await atraceFetch(url, init);
  const body = await response.text();
  if (!response.ok) throw new Error(`${init.method || "GET"} ${url}: HTTP ${response.status}: ${body.slice(0, 400)}`);
  return JSON.parse(body);
}

async function apiPost(path, value) {
  return requestJson(`${ATRACE_API}${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(value),
  });
}

// knownOf asks which of the activities are stored; a key that could not be made gets null.
async function knownOf(keys) {
  const asked = keys.map((key, index) => ({ key, index })).filter(({ key }) => key);
  const known = keys.map(() => null);
  for (let from = 0; from < asked.length; from += 400) {
    const chunk = asked.slice(from, from + 400);
    const answer = await apiPost("/collect/known", { items: chunk.map(({ key }) => key) });
    chunk.forEach(({ index }, i) => { known[index] = answer.known[i] === true; });
  }
  return known;
}

// A post and, with it, the Create of its author.
async function ingest(document, raw) {
  const stored = await ingestDocument(document, raw);
  const authorship = authorshipOf(document);
  if (authorship) await ingestDocument(authorship);
  return stored;
}

async function ingestDocument(document, raw) {
  return apiPost("/collect/ingest", {
    observed_at: new Date().toISOString(),
    observed_by: OBSERVED_BY,
    ...(raw ? { raw } : {}),
    document,
  });
}

const downloaded = new Set();

// Download an X image once and register it as the local copy of its URL.
async function ensureLocalImage(url) {
  if (downloaded.has(url)) return;
  const known = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(url)}`);
  if (!known.ok) {
    await pause(randomDelay(1_000, 2_000));
    const response = await fetch(url, { signal: AbortSignal.timeout(30_000) });
    if (!response.ok) throw new Error(`image download failed: HTTP ${response.status} ${url}`);
    const mime = response.headers.get("content-type")?.split(";")[0] || "";
    if (!mime.startsWith("image/")) throw new Error(`unexpected image MIME ${mime}: ${url}`);
    const uploaded = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(url)}`, {
      method: "PUT",
      headers: { "Content-Type": mime },
      body: await response.arrayBuffer(),
    });
    if (!uploaded.ok) throw new Error(`atrace /collect/media: HTTP ${uploaded.status}: ${(await uploaded.text()).slice(0, 300)}`);
  }
  downloaded.add(url);
}

// Store the entries in order; rawOf gives the raw response each role keeps.
async function storeEntries(entries, rawByRole) {
  for (const entry of entries) {
    for (const attachment of entry.document.attachment || []) {
      if (attachment.type === "Image") await ensureLocalImage(attachment.url);
    }
  }
  for (const entry of entries) await ingest(entry.document, rawByRole[entry.role]);
}

function rawOf(source, tweet) {
  return { source, observed_at: new Date().toISOString(), response: tweet };
}

// ---- timelines ------------------------------------------------------------

// ego's Node drops all console output when the first asynchronous operation is
// a fetch; any timer first keeps it.
await pause(0);
const health = await requestJson(`${ATRACE_API}/healthz`);
if (!health.ok) throw new Error("atrace health check failed");

const spaces = await listTaskSpaces();
const existingSpace = spaces.find(space => space.name === TASK_SPACE);
if (existingSpace && existingSpace.ownership !== "agent") {
  throw new Error(`Task space ${TASK_SPACE} is user-owned; refusing to claim it automatically.`);
}
const task = existingSpace
  ? await taskSpace(existingSpace.id)
  : await taskSpace(TASK_SPACE, { profileId: PROFILE_ID });
const page = task.page("p1");
await page.cdp("Network.enable");

// Open an X page, wait for the page's own GraphQL request named operation, and
// return its first response plus a function that reads the following pages.
async function openTimeline(path, operation) {
  await page.events();
  await page.goto(`https://x.com${path}`, { timeout: 60_000 });
  let sent;
  const seen = [];
  for (let attempt = 0; attempt < 30 && !sent; attempt += 1) {
    await pause(1_000);
    seen.push(...(await page.events()));
    sent = seen.find(e => e.method === "Network.requestWillBeSent" && e.params.request.url.includes(`/${operation}?`));
  }
  if (!sent) throw new Error(`${path}: the page never requested ${operation}`);
  let first;
  for (let attempt = 0; attempt < 10 && !first; attempt += 1) {
    try {
      first = JSON.parse((await page.cdp("Network.getResponseBody", { requestId: sent.params.requestId })).body);
    } catch {
      await pause(1_000);
    }
  }
  if (!first) throw new Error(`${path}: could not read the ${operation} response`);

  const headers = {};
  for (const [name, value] of Object.entries(sent.params.request.headers)) {
    if (!/^(sec-|user-agent|referer|content-type)/i.test(name)) headers[name] = value;
  }
  const url = new URL(sent.params.request.url);
  const variables = JSON.parse(url.searchParams.get("variables") || "{}");
  const next = async cursor => {
    const query = new URL(url);
    query.searchParams.set("variables", JSON.stringify({ ...variables, cursor }));
    const response = await page.fetch(query.toString(), { headers, timeout: 30_000 });
    if (!response.ok) throw new Error(`${operation}: HTTP ${response.status}`);
    return JSON.parse(response.body);
  };
  const scripts = [...new Set(seen.filter(e => e.method === "Network.requestWillBeSent" && /abs\.twimg\.com\/.*\.js/.test(e.params.request.url)).map(e => e.params.request.url))];
  return { first, next, variables, request: { url, headers }, scripts };
}

// Some operations are never called by the page itself (UserTweets is the one
// that lists pure retweets). They are read by borrowing the request of an
// operation the page does call, with the operation's query ID taken from X's
// own scripts, so nothing is hard-coded.
async function openBorrowed(path, borrowedFrom, operation) {
  const base = await openTimeline(path, borrowedFrom);
  const id = await page.evaluate(async ({ urls, operation }) => {
    for (const url of urls) {
      try {
        const match = (await (await fetch(url)).text()).match(new RegExp(`queryId:"([^"]+)",operationName:"${operation}"`));
        if (match) return match[1];
      } catch {}
    }
    return "";
  }, { urls: base.scripts, operation });
  if (!id) throw new Error(`could not find the query ID of ${operation}`);
  const read = async cursor => {
    const query = new URL(base.request.url);
    query.pathname = query.pathname.replace(/\/graphql\/[^/]+\/[^/]+$/, `/graphql/${id}/${operation}`);
    query.searchParams.set("variables", JSON.stringify(cursor ? { ...base.variables, cursor } : base.variables));
    const response = await page.fetch(query.toString(), { headers: base.request.headers, timeout: 30_000 });
    if (!response.ok) throw new Error(`${operation}: HTTP ${response.status}`);
    return JSON.parse(response.body);
  };
  return { first: await read(""), next: read, variables: base.variables };
}

// Read up to maxPages pages, newest first, and return the tweets in the order
// they were listed. stop(tweets) ends the walk after a page; it may answer later.
async function readTimeline(path, operation, maxPages, stop = () => false, borrowedFrom = "") {
  const { first, next, variables } = borrowedFrom
    ? await openBorrowed(path, borrowedFrom, operation)
    : await openTimeline(path, operation);
  const tweets = [];
  let body = first;
  for (let pageNumber = 1; pageNumber <= maxPages; pageNumber += 1) {
    const found = entriesOf(body).flatMap(entry => tweetsOfEntry(entry.content));
    if (!found.length) break;
    if (!FULL) tweets.push(...found.map(tweet => ({ tweet, page: pageNumber })));
    console.log(`${operation} page ${pageNumber}: ${found.length} tweet(s).`);
    if (await stop(found, variables.userId)) break;
    const cursor = cursorOf(body, "Bottom");
    if (!cursor || pageNumber === maxPages) break;
    await pause(randomDelay(PAGE_DELAY_MIN_MS, PAGE_DELAY_MAX_MS));
    body = await next(cursor);
  }
  return { tweets, userId: variables.userId };
}

// ---- who is logged in -----------------------------------------------------

await page.goto("https://x.com/home", { timeout: 60_000 });
const twid = await page.evaluate(() => decodeURIComponent((document.cookie.match(/(?:^|; )twid=([^;]+)/) || [])[1] || ""));
const ME = (twid.match(/u=(\d+)/) || [])[1];
if (!ME) throw new Error("The Ego profile is not logged in to X.");
const meRef = `x:user:${ME}`;
if (typeof checkCollectorAccount === "function") {
  let handle = "";
  for (let i = 0; i < 10 && !handle; i++) {
    handle = await page.evaluate(() => document.querySelector('[data-testid="AppTabBar_Profile_Link"]')?.getAttribute("href") || "");
    if (!handle) await pause(500);
  }
  await checkCollectorAccount(meRef, handle.replace(/^\//, ""), task);
  if (!/^\/[A-Za-z0-9_]{1,15}$/.test(handle)) throw new Error("Could not identify the logged-in X handle.");
  SCREEN_NAME = handle.slice(1);
}


// ---- own posts and replies ------------------------------------------------

const counts = { posts: 0, quotes: 0, retweets: 0, replies: 0, context: 0, failed: 0, likes: 0, bookmarks: 0 };

const COUNT_OF = { context: "context", retweet: "retweets", quote: "quotes", reply: "replies", post: "posts" };

async function importTweet(tweet, source) {
  const { kind, entries, cited } = tweetDocuments(tweet, { me: ME });
  const raw = rawOf(source, tweet);
  const citedRaw = cited ? rawOf(`${source}.${kind === "retweet" ? "retweeted" : "quoted"}`, cited) : undefined;
  await storeEntries(entries, { author: raw, post: raw, "cited-author": citedRaw, cited: citedRaw });
  counts[COUNT_OF[kind]] += 1;
}

async function importOwn(path, operation, label, borrowedFrom = "", fallback = "") {
  // Newest first: a few tweets in a row that are already stored end the walk, and are not stored again.
  const stop = newestFirst();
  const stored = new Set();
  const seen = new Set();
  const write = async (tweet) => {
    if (seen.has(tweet.rest_id)) return;
    seen.add(tweet.rest_id);
    try {
      await importTweet(tweet, `x.graphql.${operation}`);
    } catch (error) {
      counts.failed += 1;
      console.warn(`Skipped ${label} ${tweet.rest_id}: ${error.message}`);
    }
  };
  const untilStored = async (found, userId) => {
    if (userId && userId !== ME) throw new Error(`Ego profile mismatch: expected X user ${ME}, got ${userId}`);
    const known = await knownOf(found.map(tweet => tweetKey(tweet, ME)));
    if (FULL) {
      for (const tweet of takeUnknown(stop, found, known, false)) await write(tweet);
    } else {
      found.forEach((tweet, i) => { if (known[i]) stored.add(tweet.rest_id); });
      takeUnknown(stop, found, known);
    }
    return stop.done;
  };
  let read;
  try {
    read = await readTimeline(path, operation, OWN_PAGES, untilStored, borrowedFrom);
  } catch (error) {
    if (!fallback) throw error;
    console.warn(`${operation} unavailable (${error.message}); reading ${fallback} instead.`);
    Object.assign(stop, newestFirst());
    stored.clear();
    operation = fallback;
    read = await readTimeline(path, operation, OWN_PAGES, untilStored);
  }
  for (const { tweet } of read.tweets) {
    if (!stored.has(tweet.rest_id)) await write(tweet);
  }
}

// ---- likes and bookmarks --------------------------------------------------

// Read reactions oldest first within a batch. Full scans commit each page before continuing.
async function importReactions(path, operation, label, activityOf) {
  // Newest first: what is stored already ends the list, so only the new ones are kept.
  const stop = newestFirst();
  const fresh = new Set();
  const seen = new Set();
  let stored = 0;
  const write = async (tweet) => {
    if (seen.has(tweet.rest_id)) return;
    seen.add(tweet.rest_id);
    try {
      const raw = rawOf(`x.graphql.${operation}`, tweet);
      await storeEntries(reactionDocuments(tweet, activityOf), { author: raw, post: raw });
      stored += 1;
    } catch (error) {
      counts.failed += 1;
      console.warn(`Skipped ${label} ${tweet.rest_id}: ${error.message}`);
    }
  };
  const untilStored = async (found) => {
    const known = await knownOf(found.map(tweet => reactionKey(tweet, activityOf)));
    const missing = takeUnknown(stop, found, known, !FULL);
    if (FULL) {
      for (const tweet of missing.reverse()) await write(tweet);
    } else {
      for (const tweet of missing) fresh.add(tweet.rest_id);
    }
    return stop.done;
  };
  const { tweets } = await readTimeline(path, operation, REACTION_PAGES, untilStored);
  const listed = new Set();
  const ordered = tweets.filter(({ tweet }) =>
    fresh.has(tweet.rest_id) && !listed.has(tweet.rest_id) && listed.add(tweet.rest_id)
  ).reverse();
  for (const { tweet } of ordered) await write(tweet);
  counts[label] = stored;
}

// ---- one post --------------------------------------------------------------

// Read one post again, and the replies under it: open its page, take what the page's own TweetDetail request
// answered, and store the post as a timeline's tweet is stored, each reply under the tweet it answers.
async function readPost(target) {
  const number = postNumberOf(target);
  if (!number) throw new Error(`not a post of X: ${target}`);
  const { first } = await openTimeline(`/i/status/${number}`, "TweetDetail");
  const { post, replies } = postPage(first, number);
  if (!post) throw new Error(`the page of ${target} does not hold the post`);
  await importTweet(post, "x.graphql.TweetDetail");
  let stored = 0;
  for (const tweet of replies) {
    try {
      const raw = rawOf("x.graphql.TweetDetail", tweet);
      await storeEntries(replyDocuments(tweet), { author: raw, reply: raw });
      stored += 1;
    } catch (error) {
      counts.failed += 1;
      console.warn(`Skipped reply ${tweet.rest_id}: ${error.message}`);
    }
  }
  console.log(JSON.stringify({ platform: "x", account: SCREEN_NAME, uid: ME, post: target, replies: stored, failed: counts.failed }));
}

if (TARGET) {
  await readPost(TARGET);
} else {
  await importOwn(`/${SCREEN_NAME}`, "UserTweets", "post", "UserOriginalsTimeline", "UserOriginalsTimeline");
  await pause(randomDelay(PAGE_DELAY_MIN_MS, PAGE_DELAY_MAX_MS));
  await importOwn(`/${SCREEN_NAME}/with_replies`, "UserRepliesTimeline", "reply");
  await pause(randomDelay(PAGE_DELAY_MIN_MS, PAGE_DELAY_MAX_MS));
  await importReactions(`/${SCREEN_NAME}/likes`, "Likes", "likes", object => likeDocument(ME, object));
  await ingest({ type: "Collection", id: BOOKMARKS, name: "X 书签", attributedTo: meRef });
  await pause(randomDelay(PAGE_DELAY_MIN_MS, PAGE_DELAY_MAX_MS));
  await importReactions("/i/bookmarks", "Bookmarks", "bookmarks", object => addDocument(ME, object));

  console.log(JSON.stringify({
    platform: "x",
    account: SCREEN_NAME,
    uid: ME,
    ...counts,
  }));
}

await task.finish({ keep: [] });
} // collector body

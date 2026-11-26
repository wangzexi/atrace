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
// Run with: atrace collect xiaohongshu
// The signed-in Ego Lite profile supplies the logged-in Xiaohongshu session. No
// token is read or stored, and Xiaohongshu's signed API is never called: the
// pages load their own data, and the collector reads it from the page state
// (window.__INITIAL_STATE__) and the rendered comments, the way the older
// xhs-archive script does. Everything is read-only and low-frequency.
//   own notes   Note + Create (only the last 31 days; each note's detail page is
//               opened for its text, images, tags and first rendered comments)
//   comments    Note with inReplyTo, only what the detail page already shows
//   likes       Like     favorites   Add to xiaohongshu:collection:favorites
// Likes and favorites are read from the cards the profile shows, scrolled on only until they run into
// ones that are stored. Notes of other people keep just what the card gives (title, author, cover):
// their detail pages are not opened. Images are downloaded and registered as local copies.
//
// The file has two halves. Above the marker line are the constants and the
// functions that turn what the pages show (a note, a card, a comment) into
// documents; they touch no browser or network, and the tests load only that
// half (collectors/AGENTS.md). Below it are the pages, the scripts that read
// them, the network and the order things are stored in. What those in-page
// scripts extract is the input of the first half and is not tested here.

const PROFILE_ID = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.profileId : "Default";
const TASK_SPACE = "atrace xhs collector";
const ATRACE_API = typeof ATRACE_SERVER === "string" ? ATRACE_SERVER : "http://127.0.0.1:8183";
// An ordinary run reads what the profile shows at first, and scrolls on only while that holds nothing
// the archive already has (see KNOWN_RUN), up to MAX_SCROLLS. A full crawl (atrace collect <platform>
// --full puts `ATRACE_FULL=1` in the environment) scrolls the profile down until no
// more cards come, and reads every own note.
const FULL = typeof ATRACE_FULL !== "undefined" && ATRACE_FULL;
// `ATRACE_TARGET=xiaohongshu:note:65f0c9b1000000001a01b2c3` in the environment reads that one note of yours and
// its comments and nothing else: it is what the device does when the owner opens it. Without it the whole
// collection runs.
const TARGET = typeof ATRACE_TARGET === "string" ? ATRACE_TARGET : "";
const MAX_OWN_NOTES = FULL ? Infinity : 10; // a first import reads this many
const MAX_SCROLLS = 5;
const PAGE_WAIT_MS = 7_000;
const DETAIL_DELAY_MIN_MS = 10_000;
const DETAIL_DELAY_MAX_MS = 20_000;
const FAVORITES = "xiaohongshu:collection:favorites";
const OBSERVED_BY = "xhs-ego";

// The note ID a target such as "xiaohongshu:note:65f0c9b1000000001a01b2c3" names; "" when it is not one.
function noteIdOf(target) {
  return (/^xiaohongshu:note:([0-9a-zA-Z]{8,40})$/.exec(target) || [])[1] || "";
}

async function sha256Hex(text) {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join("");
}

// An image URL, forced to https, when it is on Xiaohongshu's image hosts.
function imageUrl(value) {
  try {
    const url = new URL(String(value || "").replace(/^http:/, "https:"));
    return url.protocol === "https:" && /(^|\.)xhscdn\.com$/.test(url.hostname) ? url.href : "";
  } catch {
    return "";
  }
}

// Xiaohongshu image URLs carry a timestamp and signature that change with every
// response (https://host/<time>/<sign>/<fileId>!<variant>), which would make a
// re-collected note differ on every run. The documents keep one address per
// file, built from the file ID alone.
function stableImageUrl(source) {
  const fileId = (new URL(source).pathname.split("/").filter(Boolean).pop() || "").split("!")[0];
  return fileId ? `https://sns-webpic-qc.xhscdn.com/${fileId}` : source;
}

// The stable address to put in a document and the signed one to download from.
function imageOf(value) {
  const source = imageUrl(value);
  return source ? { url: stableImageUrl(source), source } : null;
}

function userIdOf(profile) {
  return (String(profile || "").match(/\/user\/profile\/([0-9a-f]{24})/) || [])[1] || "";
}

function personDocument(id, name) {
  return {
    type: "Person",
    id: `xiaohongshu:user:${id}`,
    url: `https://www.xiaohongshu.com/user/profile/${id}`,
    ...(name ? { name } : {}),
  };
}

// The user's own note and its Create. note is what the detail page shows, card
// the profile card it was found by. downloads lists the images to fetch: the
// stable address and the signed one to fetch it from.
function ownNoteDocuments(card, note, meId) {
  const meRef = `xiaohongshu:user:${meId}`;
  const id = `xiaohongshu:note:${card.id}`;
  const attachment = [];
  const downloads = [];
  for (const image of note.images) {
    const item = imageOf(image);
    if (item) {
      attachment.push({ type: "Image", url: item.url });
      downloads.push(item);
    }
  }
  if (note.video && /^https:/.test(note.video)) {
    // The video address is signed too; the document keeps it without the query.
    const video = new URL(note.video);
    attachment.push({ type: "Video", url: video.origin + video.pathname });
  }
  const published = note.time ? new Date(note.time).toISOString() : card.time ? new Date(card.time).toISOString() : undefined;
  const updated = note.updated && note.updated !== note.time ? new Date(note.updated).toISOString() : undefined;
  return [
    {
      role: "post",
      document: {
        type: "Note",
        id,
        attributedTo: meRef,
        ...(note.title || card.title ? { name: note.title || card.title } : {}),
        content: withoutTopicMarks(note.desc),
        url: `https://www.xiaohongshu.com/explore/${card.id}`,
        ...(published ? { published } : {}),
        ...(updated ? { updated } : {}),
        ...(note.tags.length ? { tag: note.tags.map(name => ({ type: "Hashtag", name })) } : {}),
        ...(attachment.length ? { attachment } : {}),
      },
      downloads,
    },
    { role: "create", document: { type: "Create", actor: meRef, object: id, ...(published ? { published } : {}) } },
  ];
}

// The commenter, text and time of every comment (and reply) in what the page's own request for the
// note's comments (/api/sns/web/v2/comment/page) answered. The rendered page shows only "2小时前".
function commentTimes(bodies) {
  const found = [];
  const add = comment => {
    const id = String(comment?.user_info?.user_id || "");
    const content = String(comment?.content || "").trim();
    const millis = Number(comment?.create_time);
    if (id && content && millis > 0) found.push({ id, content, time: new Date(millis).toISOString() });
  };
  for (const body of bodies) {
    for (const comment of body?.data?.comments || []) {
      add(comment);
      for (const reply of comment.sub_comments || []) add(reply);
    }
  }
  return found;
}

// The rendered page leaves its emoji out of a comment's text, where the request has them as [name].
function withoutEmoji(text) {
  return String(text || "").replace(/\[[^\[\]]{1,12}\]/g, "").trim();
}

// What the page prints under a comment ("4小时前澳大利亚", "昨天 21:08广东", "11-17 北京") as a time, when it
// is all there is: the hour or the day is as exact as the page is. Asia/Shanghai, as the page shows it.
function timeOfDateText(text, now = new Date()) {
  const label = String(text || "").trim();
  const shanghai = new Date(now.getTime() + 8 * 3_600_000);
  const day = (year, month, date, hour = 0, minute = 0) => new Date(Date.UTC(year, month, date, hour, minute) - 8 * 3_600_000).toISOString();
  let match;
  if (/^刚刚/.test(label)) return now.toISOString();
  if ((match = /^(\d+)分钟前/.exec(label))) return new Date(now.getTime() - Number(match[1]) * 60_000).toISOString();
  if ((match = /^(\d+)小时前/.exec(label))) return new Date(now.getTime() - Number(match[1]) * 3_600_000).toISOString();
  if ((match = /^(\d+)天前/.exec(label))) return new Date(now.getTime() - Number(match[1]) * 86_400_000).toISOString();
  if ((match = /^(昨天|前天) (\d{1,2}):(\d{2})/.exec(label))) {
    return day(shanghai.getUTCFullYear(), shanghai.getUTCMonth(), shanghai.getUTCDate() - (match[1] === "昨天" ? 1 : 2), Number(match[2]), Number(match[3]));
  }
  if ((match = /^(\d{4})-(\d{2})-(\d{2})/.exec(label))) return day(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  if ((match = /^(\d{2})-(\d{2})/.exec(label))) {
    const year = shanghai.getUTCFullYear();
    const thisYear = day(year, Number(match[1]) - 1, Number(match[2]));
    return Date.parse(thisYear) > now.getTime() ? day(year - 1, Number(match[1]) - 1, Number(match[2])) : thisYear;
  }
  return undefined;
}

// The time of a rendered comment. A comment's own ID is made like a note's, so it holds the time it was
// written; failing that, the one the page's request gave for the same commenter and text (a reply's
// rendered text starts with who it answers), else what the page prints under the comment.
function timeOfComment(found, comment, now = new Date()) {
  const own = noteIdTime(String(comment.id || ""));
  if (own) return own;
  const id = userIdOf(comment.profile);
  const text = withoutEmoji(comment.text);
  const exact = found.find(item => item.id === id && text.includes(withoutEmoji(item.content)));
  return exact ? exact.time : timeOfDateText(comment.date, now);
}

// Xiaohongshu shows a thread as the comment and, flat under it, every reply to it; a reply to another reply
// begins with "回复 name :". The reply that is answered is the latest one by that name before it in the thread
// (or the comment itself), which is where the reply goes. Each reply that names someone gets `target`.
function threadTargets(comments) {
  const key = comment => `${comment.profile}|${comment.text}`;
  const threads = new Map(); // the comment that opened a thread -> the comments of it so far
  for (const comment of comments) {
    if (!comment.reply) {
      threads.set(key(comment), [comment]);
      continue;
    }
    const thread = threads.get(key(comment.parent)) || [];
    const name = /^回复\s*(\S+)\s*[:：]/.exec(comment.text || "")?.[1];
    const target = name ? [...thread].reverse().find(earlier => earlier.author === name) : undefined;
    if (target && target !== thread[0]) comment.target = target;
    thread.push(comment);
  }
  return comments;
}

// A comment the detail page rendered. It carries no ID, so it gets a stable ID from the note, the
// commenter and the text; its time (published) is the one the page's own request answered, when known.
async function commentDocuments(cardId, comment) {
  const authorId = userIdOf(comment.profile);
  const commentId = `xiaohongshu:comment:${(await sha256Hex(`${cardId}|${authorId}|${comment.text}`)).slice(0, 16)}`;
  let inReplyTo = `xiaohongshu:note:${cardId}`;
  if (comment.reply) {
    const answered = comment.target || comment.parent;
    const parentId = userIdOf(answered.profile);
    inReplyTo = `xiaohongshu:comment:${(await sha256Hex(`${cardId}|${parentId}|${answered.text}`)).slice(0, 16)}`;
  }
  // Not kept: a comment's pictures are a mark, "[图片]", the same on every platform.
  // The text the page shows, emoji as [name]; the ID below is made from the plain text, which does not change.
  const shown = comment.rich || comment.text;
  const words = comment.reply ? shown.replace(/^回复\s*\S+\s*[:：]\s*/, "") : shown;
  const content = (comment.pictures || []).length ? `${words} [图片]`.trim() : words;
  return [
    ...(authorId ? [{ role: "author", document: personDocument(authorId, comment.author) }] : []),
    {
      role: "post",
      document: {
        type: "Note",
        id: commentId,
        ...(authorId ? { attributedTo: `xiaohongshu:user:${authorId}` } : {}),
        inReplyTo,
        content,
        url: `https://www.xiaohongshu.com/explore/${cardId}`,
        ...(comment.published ? { published: comment.published } : {}),
      },
    },
  ];
}

// A card from the likes or favorites tab: the author and the note as the card
// shows it (the user's own notes were stored in full elsewhere), and the
// activity on it.
// A note's ID carries the time it was created: the first four bytes are seconds since 1970 (what
// Xiaohongshu's profile cards give for a liked or favorited note, which has no time of its own).
function noteIdTime(noteId) {
  if (!/^[0-9a-f]{24}$/.test(noteId)) return undefined;
  const seconds = parseInt(noteId.slice(0, 8), 16);
  return seconds > 1_000_000_000 && seconds < 4_000_000_000 ? new Date(seconds * 1000).toISOString() : undefined;
}

function reactionDocuments(card, meId, activityOf) {
  const id = `xiaohongshu:note:${card.id}`;
  const entries = [];
  if (card.author.id !== meId) {
    if (card.author.id) entries.push({ role: "author", document: personDocument(card.author.id, card.author.name) });
    const cover = imageOf(card.cover);
    entries.push({
      role: "post",
      document: {
        type: "Note",
        id,
        ...(card.author.id ? { attributedTo: `xiaohongshu:user:${card.author.id}` } : {}),
        ...(card.title ? { name: card.title } : {}),
        content: "",
        url: `https://www.xiaohongshu.com/explore/${card.id}`,
        // Without it the entry would be dated by when it was collected.
        ...(card.time ? { published: new Date(card.time).toISOString() } : noteIdTime(card.id) ? { published: noteIdTime(card.id) } : {}),
        ...(cover ? { attachment: [{ type: "Image", url: cover.url }] } : {}),
      },
      downloads: cover ? [cover] : [],
    });
  }
  entries.push({ role: "activity", document: activityOf(id) });
  return entries;
}

function likeDocument(meId, object) {
  return { type: "Like", actor: `xiaohongshu:user:${meId}`, object };
}

function addDocument(meId, object) {
  return { type: "Add", actor: `xiaohongshu:user:${meId}`, object, target: FAVORITES };
}


// A list read newest first is done where it runs into what the archive already has: KNOWN_RUN items
// in a row that are stored. A pinned old post at the top does not end it, and a run that finds nothing
// stored (the first one) reads on up to the limits below.
const KNOWN_RUN = 5;

// Xiaohongshu writes a topic into the text of a note as #name[话题]#. The [话题]# is its own mark for the
// topic: the topics are also given on their own (the note's tagList, kept as the document's tags), and what is
// left is #name, the way its page shows it.
function withoutTopicMarks(text) {
  return String(text || "").replace(/\[话题\]#?/g, "");
}

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

// What a card leaves in the archive: an own note's Create, or the like or favorite of a note.
function ownCardKey(card, meId) {
  try {
    const note = { title: card.title, desc: "", time: card.time, updated: 0, images: [], video: "", tags: [] };
    return activityKey(ownNoteDocuments(card, note, meId));
  } catch {
    return null;
  }
}

function reactionKey(card, meId, activityOf) {
  try {
    return activityKey(reactionDocuments(card, meId, activityOf));
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

// ==== side effects below: the pages, the network and storage order; not loaded by tests ====

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

async function requestJson(url, init = {}) {
  const response = await atraceFetch(url, { signal: AbortSignal.timeout(30_000), ...init });
  const body = await response.text();
  if (!response.ok) throw new Error(`${init.method || "GET"} ${url}: HTTP ${response.status}: ${body.slice(0, 300)}`);
  return JSON.parse(body);
}

// knownOf asks which of the activities are stored; a key that could not be made gets null.
async function knownOf(keys) {
  const asked = keys.map((key, index) => ({ key, index })).filter(({ key }) => key);
  const known = keys.map(() => null);
  for (let from = 0; from < asked.length; from += 400) {
    const chunk = asked.slice(from, from + 400);
    const answer = await requestJson(`${ATRACE_API}/collect/known`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ items: chunk.map(({ key }) => key) }),
    });
    chunk.forEach(({ index }, i) => { known[index] = answer.known[i] === true; });
  }
  return known;
}

// The cards, newest first, that are not stored; the list is done where it runs into stored ones.
async function unknownCards(cards, keyOf) {
  const state = newestFirst();
  const fresh = takeUnknown(state, cards, await knownOf(cards.map(keyOf)), !FULL);
  return { fresh, done: state.done };
}

// A post and, with it, the Create of its author.
async function ingest(document, raw) {
  const stored = await ingestDocument(document, raw);
  const authorship = authorshipOf(document);
  if (authorship) await ingestDocument(authorship);
  return stored;
}

async function ingestDocument(document, raw) {
  return requestJson(`${ATRACE_API}/collect/ingest`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ observed_at: new Date().toISOString(), observed_by: OBSERVED_BY, ...(raw ? { raw } : {}), document }),
  });
}

const downloaded = new Set();

// Register a local copy of an image under url, downloading it from source (the
// URL it was seen at, which may carry a signature that url leaves out).
async function ensureLocalImage(url, source = url) {
  if (downloaded.has(url)) return;
  const known = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(url)}`);
  if (!known.ok) {
    await pause(randomDelay(2_000, 4_000));
    const response = await fetch(source, { headers: { Referer: "https://www.xiaohongshu.com/" }, signal: AbortSignal.timeout(30_000) });
    if (!response.ok) throw new Error(`image download failed: HTTP ${response.status} ${source}`);
    const mime = response.headers.get("content-type")?.split(";")[0] || "";
    if (!mime.startsWith("image/")) throw new Error(`unexpected image MIME ${mime}: ${source}`);
    const uploaded = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(url)}`, {
      method: "PUT",
      headers: { "Content-Type": mime },
      body: await response.arrayBuffer(),
    });
    if (!uploaded.ok) throw new Error(`atrace /collect/media: HTTP ${uploaded.status}: ${(await uploaded.text()).slice(0, 300)}`);
  }
  downloaded.add(url);
}

// Store the entries in order, fetching each entry's images first; rawByRole
// gives the raw response each role keeps.
async function storeEntries(entries, rawByRole) {
  for (const entry of entries) {
    for (const { url, source } of entry.downloads || []) await ensureLocalImage(url, source);
  }
  for (const entry of entries) await ingest(entry.document, rawByRole[entry.role]);
}

// ---- pages ---------------------------------------------------------------

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

// Shared by every page read: unwrap Vue refs, and report login walls and risk control.
const prelude = `
  const unwrap = v => (v && typeof v === "object" && "_value" in v ? v._value : v);
  const body = document.body?.innerText || "";
  const blocked = /安全限制|访问链接异常|访问频繁|验证码/.test(body) || /website-login\\/error|error_code=300017|error_code=300031/.test(location.href);
  const loginWall = /登录后查看|请登录/.test(body) || location.pathname.startsWith("/login");
`;

async function read(url, expression) {
  await page.goto(url, { timeout: 60_000 });
  await pause(PAGE_WAIT_MS);
  const result = JSON.parse(await page.evaluate(`(() => { ${prelude} ${expression} })()`));
  if (result.blocked) throw new Error(`Xiaohongshu risk control on ${url}; stopping so it can be handled by hand.`);
  if (result.loginWall) throw new Error("The Ego profile is not logged in to Xiaohongshu.");
  return result;
}

const me = await read("https://www.xiaohongshu.com/explore", `
  const user = unwrap(window.__INITIAL_STATE__?.user?.userInfo) || {};
  return JSON.stringify({ blocked, loginWall, id: user.userId || "", name: user.nickname || user.nickName || "" });
`);
if (!me.id) throw new Error("Could not tell which Xiaohongshu account is logged in.");
const meRef = `xiaohongshu:user:${me.id}`;
if (typeof checkCollectorAccount === "function") await checkCollectorAccount(meRef, me.name, task);
const profileUrl = `https://www.xiaohongshu.com/user/profile/${me.id}`;
await ingest({ type: "Person", id: meRef, name: me.name, url: profileUrl });

const counts = { notes: 0, cardOnly: 0, comments: 0, likes: 0, favorites: 0, failed: 0 };

// The cards of one profile tab, from the page state. Group 0 is the user's own
// notes, 1 the favorites, 2 the likes.
async function readCards(tab, group, keyOf) {
  const url = tab ? `${profileUrl}?tab=${tab}&subTab=note` : profileUrl;
  const expression = `
    const groups = unwrap(window.__INITIAL_STATE__?.user?.notes) || [];
    const cards = (unwrap(groups[${group}]) || []).map(unwrap).map(card => {
      const note = unwrap(card?.noteCard) || {};
      const user = unwrap(note.user) || {};
      const cover = unwrap(note.cover) || {};
      return {
        id: String(card?.id || note.noteId || ""),
        token: card?.xsecToken || note.xsecToken || "",
        title: note.displayTitle || "",
        type: note.type || "",
        time: Number(note.time || 0),
        author: { id: String(user.userId || ""), name: user.nickname || user.nickName || "" },
        cover: cover.urlDefault || cover.urlPre || (cover.infoList || [])[0]?.url || "",
      };
    }).filter(card => card.id);
    return JSON.stringify({ blocked, loginWall, cards });
  `;
  let result = await read(url, expression);
  // An ordinary run goes on only while the cards so far run into nothing that is stored.
  const reached = async () => !FULL && (await unknownCards(result.cards, keyOf)).done;
  if (!FULL && (await reached())) return result.cards;
  // The page loads more cards as it is scrolled; go on until two scrolls in a row bring none.
  for (let idle = 0, scrolls = 0; idle < 2 && (FULL || scrolls < MAX_SCROLLS); scrolls += 1) {
    await page.evaluate("window.scrollTo(0, document.body.scrollHeight)");
    await pause(randomDelay(DETAIL_DELAY_MIN_MS, DETAIL_DELAY_MAX_MS));
    const next = JSON.parse(await page.evaluate(`(() => { ${prelude} ${expression} })()`));
    if (next.blocked) throw new Error(`Xiaohongshu risk control on ${url}; stopping so it can be handled by hand.`);
    idle = next.cards.length > result.cards.length ? 0 : idle + 1;
    if (next.cards.length >= result.cards.length) result = next;
    console.log(`${tab || "own notes"}: ${result.cards.length} card(s) so far.`);
    if (await reached()) break;
  }
  return result.cards;
}

// ---- own notes -----------------------------------------------------------

const detailExpression = id => `
  const map = unwrap(window.__INITIAL_STATE__?.note?.noteDetailMap) || {};
  const note = unwrap(unwrap(map[${JSON.stringify(id)}])?.note) || {};
  const clean = el => (el?.textContent || "").replace(/\\s+/g, " ").trim();
  const streams = [...((note.video?.media?.stream?.h264) || []), ...((note.video?.media?.stream?.h265) || [])]
    .filter(item => item?.masterUrl).sort((a, b) => (b.width * b.height) - (a.width * a.height));
  // The emoji in a comment are pictures; their names are in the page's state, so a picture reads as its [name].
  const emojiNames = new Map();
  for (const [name, url] of Object.entries(unwrap(unwrap(window.__INITIAL_STATE__?.emojiData)?.emojiParseMap) || {})) {
    if (!emojiNames.has(url)) emojiNames.set(url, name);
  }
  const richText = el => {
    let out = "";
    const walk = node => {
      if (node.nodeType === 3) out += node.nodeValue;
      else if (node.nodeType === 1) {
        if (node.tagName === "IMG") out += emojiNames.get(node.src) || "";
        else node.childNodes.forEach(walk);
      }
    };
    if (el) walk(el);
    return out.replace(/\\s+/g, " ").trim();
  };
  const comments = [];
  // Only what the detail page rendered at first; the comment pane is never scrolled or expanded.
  for (const parent of document.querySelectorAll(".parent-comment")) {
    const item = parent.querySelector(":scope > .comment-item, .comment-item");
    if (!item) continue;
    const read = el => ({
      profile: el.querySelector('a[href*="/user/profile/"]')?.getAttribute("href") || "",
      author: clean(el.querySelector(".author-wrapper .name, .user-name, .name")),
      text: clean(el.querySelector(".content, .note-text")),
      rich: richText(el.querySelector(".content, .note-text")),
      date: clean(el.querySelector(".date")),
      id: (el.id || "").replace(/^comment-/, ""),
      pictures: [...el.querySelectorAll("img.inner")].map(image => image.src).filter(Boolean),
    });
    const top = read(item);
    if (top.text) comments.push({ ...top, reply: false });
    for (const sub of parent.querySelectorAll(".reply-container .comment-item-sub, .sub-comment-list .comment-item")) {
      const reply = read(sub);
      if (reply.text) comments.push({ ...reply, reply: true, parent: top });
    }
  }
  return JSON.stringify({
    blocked, loginWall,
    found: Boolean(note.noteId || note.title || note.desc),
    note: {
      title: note.title || "",
      desc: note.desc || "",
      type: note.type || "",
      time: Number(note.time || 0),
      updated: Number(note.lastUpdateTime || 0),
      ipLocation: note.ipLocation || "",
      tags: (note.tagList || []).map(tag => tag?.name || "").filter(Boolean),
      images: (note.imageList || []).map(image => image?.urlDefault || image?.urlPre || (image?.infoList || [])[0]?.url || "").filter(Boolean),
      video: streams[0]?.masterUrl || "",
      interact: note.interactInfo || {},
    },
    comments,
  });
`;

// The times of the comments the page just asked for (see commentTimes in index.mjs); the page's own
// request is read, not repeated.
// Loads the later pages of comments: the page asks for the next one when the comments are scrolled to
// the end. A few times at most, slowly, until a scroll brings no more.
async function scrollComments(maxScrolls) {
  let last = -1;
  for (let scrolls = 0; scrolls < maxScrolls; scrolls += 1) {
    const count = await page.evaluate(`(() => {
      const scroller = document.querySelector(".note-scroller") || document.scrollingElement;
      scroller.scrollTo(0, scroller.scrollHeight);
      window.scrollTo(0, document.body.scrollHeight);
      return document.querySelectorAll(".comment-item").length;
    })()`);
    if (count === last) return;
    last = count;
    await pause(randomDelay(2_000, 3_500));
  }
}

// Opens the folded replies under the comments ("展开 3 条回复", "展开更多回复"), one at a time and slowly, up to
// maxClicks; what the page then shows is read again. The page's own requests answer them.
async function expandReplies(maxClicks = 25) {
  for (let clicks = 0; clicks < maxClicks; clicks += 1) {
    const clicked = await page.evaluate(`(() => {
      const button = [...document.querySelectorAll(".show-more")].find(el => /^展开/.test((el.textContent || "").trim()));
      if (!button) return false;
      button.scrollIntoView({ block: "center" });
      button.click();
      return true;
    })()`);
    if (!clicked) return;
    await pause(randomDelay(1_500, 3_000));
  }
}

async function commentTimesOf() {
  const bodies = [];
  for (const event of (await page.events()).filter(e => e.method === "Network.responseReceived" && /\/api\/sns\/web\/v2\/comment\/(sub\/)?page/.test(e.params.response.url))) {
    try {
      bodies.push(JSON.parse((await page.cdp("Network.getResponseBody", { requestId: event.params.requestId })).body));
    } catch {
      // a response whose body is no longer available
    }
  }
  return commentTimes(bodies);
}

async function importOwnNote(card) {
  const url = `https://www.xiaohongshu.com/user/profile/${me.id}/${card.id}?xsec_token=${encodeURIComponent(card.token)}&xsec_source=pc_user`;
  await page.events(); // what came before is not about this note
  const detail = await read(url, detailExpression(card.id));
  if (!detail.found) throw new Error("the note page has no data");
  await scrollComments(6);
  await expandReplies();
  detail.comments = threadTargets(JSON.parse(await page.evaluate(`(() => { ${prelude} ${detailExpression(card.id)} })()`)).comments);
  const times = await commentTimesOf();
  for (const comment of detail.comments) comment.published = timeOfComment(times, comment);
  const raw = { source: "xiaohongshu.page.noteDetailMap", observed_at: new Date().toISOString(), response: detail.note };
  await storeEntries(ownNoteDocuments(card, detail.note, me.id), { post: raw });
  counts.notes += 1;

  for (const comment of detail.comments) {
    await storeEntries(await commentDocuments(card.id, comment), {
      post: { source: "xiaohongshu.page.comments", note: card.id, response: comment },
    });
    counts.comments += 1;
  }
}

// Read one of the user's notes again, and its comments: the note page as it is now, stored as the profile's notes
// are. The page needs the token the profile's card of the note holds, so the profile is read first. Notes of other
// people are not read again: only their cards are kept.
async function readTarget() {
  const id = noteIdOf(TARGET);
  if (!id) throw new Error(`not a note of Xiaohongshu: ${TARGET}`);
  const cards = await readCards("", 0, card => ownCardKey(card, me.id));
  const card = cards.find(found => found.id === id);
  if (!card) throw new Error(`${TARGET} is not among the notes of your profile`);
  await pause(randomDelay(DETAIL_DELAY_MIN_MS, DETAIL_DELAY_MAX_MS));
  await importOwnNote(card);
  console.log(JSON.stringify({ platform: "xiaohongshu", account: me.name, uid: me.id, post: TARGET, comments: counts.comments }));
}

if (TARGET) {
  await readTarget();
} else {
  // Newest first: the notes that are not stored yet, up to where the list runs into stored ones.
  const ownKey = card => ownCardKey(card, me.id);
  const newestOwn = (await readCards("", 0, ownKey)).sort((a, b) => b.time - a.time);
  const ownCards = (await unknownCards(newestOwn, ownKey)).fresh.slice(0, MAX_OWN_NOTES);
  console.log(`Own notes to read: ${ownCards.length}.`);
  for (const card of ownCards) {
    await pause(randomDelay(DETAIL_DELAY_MIN_MS, DETAIL_DELAY_MAX_MS));
    try {
      await importOwnNote(card);
    } catch (error) {
      if (/risk control|not logged in/.test(error.message)) throw error;
      if (/has no data/.test(error.message)) {
        // The detail page shows nothing: keep what the profile card says (title, cover, time), and
        // leave the note for a later run to read in full.
        try {
          const note = { title: card.title, desc: "", time: card.time, updated: 0, images: card.cover ? [card.cover] : [], video: "", tags: [] };
          await storeEntries(ownNoteDocuments(card, note, me.id), { post: { source: "xiaohongshu.page.profile.card", observed_at: new Date().toISOString(), response: card } });
          counts.cardOnly += 1;
          continue;
        } catch (cardError) {
          console.warn(`Skipped note ${card.id} (card): ${cardError.message}`);
        }
      }
      counts.failed += 1;
      console.warn(`Skipped note ${card.id}: ${error.message}`);
    }
  }

  // ---- likes and favorites -------------------------------------------------

  // Xiaohongshu does not say when a note was liked or favorited. Items are stored
  // oldest first so the order of observation matches the order the user acted in.
  async function importReactions(tab, group, label, activityOf) {
    await pause(randomDelay(DETAIL_DELAY_MIN_MS, DETAIL_DELAY_MAX_MS));
    const key = card => reactionKey(card, me.id, activityOf);
    const listed = await readCards(tab, group, key);
    // Newest first: only what is not stored yet is kept, and stored oldest first.
    const cards = (await unknownCards(listed, key)).fresh;
    console.log(`${label}: ${cards.length} new card(s) of ${listed.length}.`);
    let stored = 0;
    for (const card of cards.reverse()) {
      try {
        const raw = { source: `xiaohongshu.page.${tab}`, observed_at: new Date().toISOString(), response: card };
        await storeEntries(reactionDocuments(card, me.id, activityOf), { post: raw });
        stored += 1;
      } catch (error) {
        counts.failed += 1;
        console.warn(`Skipped ${label} ${card.id}: ${error.message}`);
      }
    }
    counts[label] = stored;
  }

  await importReactions("liked", 2, "likes", object => likeDocument(me.id, object));
  await ingest({ type: "Collection", id: FAVORITES, name: "小红书收藏", attributedTo: meRef });
  await importReactions("fav", 1, "favorites", object => addDocument(me.id, object));

  console.log(JSON.stringify({ platform: "xiaohongshu", account: me.name, uid: me.id, ...counts }));
}

await task.finish({ keep: [] });
} // collector body

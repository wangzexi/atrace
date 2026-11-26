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
// Run with: atrace collect zhihu
//
// The file has two halves. Above the marker line are the constants and the
// functions that turn Zhihu entries into documents; they touch no browser or
// network, and the tests load only that half (collectors/AGENTS.md). Below it are
// the pages, the network and the order things are stored in. The HTML-to-text
// step (normalizeBody) needs a DOM and runs in the page, so its output is the
// input of the first half and is not tested here.
const ATRACE_API = typeof ATRACE_SERVER === "string" ? ATRACE_SERVER : "http://127.0.0.1:8183";
const PROFILE_ID = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.profileId : "Default";
const ZHIHU = "https://www.zhihu.com";
const OBSERVED_BY = "zhihu-ego";
const TASK_SPACE = "atrace zhihu collector";
const PAGE_SIZE = 20;
const PREVIEW_IMAGES = 9;
// An ordinary run reads each list from the newest back until it meets what is stored (see KNOWN_RUN),
// at most MAX_PAGES pages: enough to catch up after a while, not to read everything. A full crawl
// (atrace collect <platform> --full puts `ATRACE_FULL=1` in the environment) reads
// every page, slowly: Zhihu answers 403 to requests that come close together.
const FULL = typeof ATRACE_FULL !== "undefined" && ATRACE_FULL;
// `ATRACE_TARGET=zhihu:answer:123` in the environment reads that one answer, article or pin and nothing
// else: it is what the device does when the owner opens it. Without it the whole collection runs.
const TARGET = typeof ATRACE_TARGET === "string" ? ATRACE_TARGET : "";
const MAX_PAGES = FULL ? Infinity : 10;
const PAGE_DELAY_MIN_MS = FULL ? 20_000 : 2_000;
const PAGE_DELAY_MAX_MS = FULL ? 40_000 : 4_000;

function platformID(value) {
  if (typeof value === "string" && /^[a-z0-9_-]+$/i.test(value)) return value;
  if (Number.isSafeInteger(value) && value > 0) return String(value);
  return "";
}

// What a target such as "zhihu:answer:123" names: the kind of content and its ID; null when it is not one.
function targetOf(target) {
  const found = /^zhihu:(answer|article|pin):([a-z0-9_-]{1,40})$/i.exec(target);
  return found ? { type: found[1], id: found[2] } : null;
}

function timestamp(value) {
  const date = typeof value === "number" ? new Date(value * 1000)
    : typeof value === "string" && value ? new Date(value) : null;
  return date && Number.isFinite(date.getTime()) ? date.toISOString() : undefined;
}

// Zhihu serves the same image from rotating hosts (pic1, picx, ...), so the
// address changes between responses. One fixed host keeps a re-collected
// document identical, instead of adding a version on every run.
function canonicalImage(attachment) {
  const url = new URL(attachment.url);
  url.hostname = url.hostname.replace(/^pic[0-9a-z]*\.zhimg\.com$/, "pic1.zhimg.com");
  return {...attachment, url: url.href};
}

const voteTargets = new Map([
  ["MEMBER_VOTEUP_ANSWER", "answer"],
  ["MEMBER_VOTEUP_ARTICLE", "article"],
  ["MEMBER_VOTE_PIN", "pin"],
]);

// The rows of the activity feed where the user voted up an answer, article or pin.
function voteRows(rows, meId) {
  return rows.filter(row =>
    voteTargets.has(row.verb) && voteTargets.get(row.verb) === row.target?.type
    && platformID(row.actor?.id) === platformID(meId));
}

// A pin's body is a list of text and image blocks rather than HTML.
function pinBody(entry) {
  const parts = [];
  const attachment = [];
  for (const block of entry.content || []) {
    if (block.type === "text") {
      parts.push(block.own_text ?? block.content ?? "");
    } else if (block.type === "image") {
      try {
        const url = new URL(block.original_url || block.url);
        if (url.protocol === "https:" && url.hostname.endsWith(".zhimg.com")) attachment.push({type: "Image", url: url.href});
      } catch {}
    }
  }
  return {content: parts.join("\n\n").trim(), attachment};
}

// One answer, article or pin: its author (when it is someone else) and the Note
// or Article, with the images to keep local copies of. Null when the entry is
// deleted, of another kind or has no ID. body is {content, attachment}, the
// entry's text and images.
function contentDocuments(entry, body, actor) {
  const entryID = platformID(entry?.id);
  if (!entryID || entry.is_deleted || !["answer", "article", "pin"].includes(entry.type)) return null;
  const object = `zhihu:${entry.type}:${entryID}`;
  const questionID = platformID(entry.question?.id);
  const title = entry.question?.title || entry.title || "";
  const url = entry.type === "article" ? `https://zhuanlan.zhihu.com/p/${entryID}`
    : entry.type === "pin" ? `${ZHIHU}/pin/${entryID}`
    : questionID ? `${ZHIHU}/question/${questionID}/answer/${entryID}` : `${ZHIHU}/answer/${entryID}`;
  const authorID = platformID(entry.author?.id);
  const attributedTo = authorID ? `zhihu:user:${authorID}` : undefined;
  const attachment = body.attachment.map(canonicalImage);
  const entries = [];
  if (attributedTo && attributedTo !== actor) {
    entries.push({ role: "author", document: {type: "Person", id: attributedTo, name: entry.author.name || ""} });
  }
  entries.push({
    role: "post",
    document: {
      type: entry.type === "article" ? "Article" : "Note",
      id: object, name: title || undefined, content: body.content, url, attributedTo,
      published: timestamp(entry.created_time ?? entry.created),
      updated: timestamp(entry.updated_time ?? entry.updated),
      attachment: attachment.length ? attachment : undefined,
    },
    downloads: attachment.slice(0, PREVIEW_IMAGES).map(image => image.url),
  });
  return { object, entries };
}

function personDocument(me, actor) {
  return {type: "Person", id: actor, name: me.name || "", url: `${ZHIHU}/people/${encodeURIComponent(me.url_token)}`};
}

function collectionDocument(folder, actor) {
  const id = platformID(folder.id);
  if (!id) throw new Error("Invalid Zhihu collection ID");
  return {type: "Collection", id: `zhihu:collection:${id}`, name: folder.title || "", attributedTo: actor, url: `${ZHIHU}/collection/${id}`};
}

function createDocument(actor, object, published) {
  return {type: "Create", actor, object, published};
}

function likeDocument(actor, object, published) {
  return {type: "Like", actor, object, published};
}

function addDocument(actor, object, target, published) {
  return {type: "Add", actor, object, target, published};
}


// The sentences the user underlined (划线): one collection of them, and each one a Note of the
// sentence that quotes the answer or article it was underlined in. Zhihu gives no time for them.
function highlightCollectionDocument(me, actor) {
  return {type: "Collection", id: "zhihu:collection:highlights", name: "知乎划线", attributedTo: actor, url: `${ZHIHU}/people/${encodeURIComponent(me.url_token)}/lineComments`};
}

function highlightDocument(segment, actor) {
  const id = platformID(segment?.id);
  const source = segment?.source_content;
  // An answer is known by its long token here, as everywhere else.
  const sourceID = platformID(source?.token ?? source?.id);
  const content = typeof segment?.content === "string" ? segment.content.trim() : "";
  if (!id || !content || !sourceID || !["answer", "article"].includes(source.type)) return null;
  return {type: "Note", id: `zhihu:highlight:${id}`, content, attributedTo: actor, url: source.url || undefined, quote: `zhihu:${source.type}:${sourceID}`};
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

// The activity an answer, article or pin leaves in the archive when the user created, liked or
// favorited it; null for what is deleted, of another kind or has no ID.
function objectOf(entry) {
  const id = platformID(entry?.id);
  return id && !entry.is_deleted && ["answer", "article", "pin"].includes(entry.type) ? `zhihu:${entry.type}:${id}` : null;
}

function keyOf(document) {
  try {
    return document ? activityKey([{ document }]) : null;
  } catch {
    return null;
  }
}

const createKey = (actor, entry) => objectOf(entry) && keyOf(createDocument(actor, objectOf(entry)));
const likeKey = (actor, entry) => objectOf(entry) && keyOf(likeDocument(actor, objectOf(entry)));
const addKey = (actor, entry, target) => objectOf(entry) && keyOf(addDocument(actor, objectOf(entry), target));

// Whether a failed read says that the one thing asked for is not to be had (an answer that was deleted or is
// hidden fails as "Failed to fetch" or 404/410), and not that Zhihu is refusing: 403, 429 and a login wall are not.
const UNAVAILABLE_RUN = 5;
function sourceUnavailable(error) {
  return /Failed to fetch|HTTP (404|410)\b/.test(String(error?.message || error));
}

// Whoever wrote a post made it: the Create of its author goes in with the post, for the posts of others as well as
// one's own, and the core does not guess it. A note its author only kept (a sentence underlined in an answer) was
// not published, so it has none.
function authorshipOf(document) {
  if (!["Note", "Article", "Video"].includes(document.type) || !document.id || !document.attributedTo) return undefined;
  if (document.id.startsWith("zhihu:highlight:")) return undefined;
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

// knownOf asks which of the activities are stored; a key that could not be made gets null.
async function knownOf(keys) {
  const asked = keys.map((key, index) => ({ key, index })).filter(({ key }) => key);
  const known = keys.map(() => null);
  for (let from = 0; from < asked.length; from += 400) {
    const chunk = asked.slice(from, from + 400);
    const response = await atraceFetch(ATRACE_API + "/collect/known", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({items: chunk.map(({key}) => key)}),
      signal: AbortSignal.timeout(30_000),
    });
    if (!response.ok) throw new Error(`Atrace known: HTTP ${response.status}: ${(await response.text()).slice(0, 300)}`);
    const answer = await response.json();
    chunk.forEach(({index}, i) => { known[index] = answer.known[i] === true; });
  }
  return known;
}

// The items of a page, newest first, that are not stored; state.done says the list ran into stored ones.
async function unknownOf(state, items, keyOfItem) {
  return takeUnknown(state, items, await knownOf(items.map(keyOfItem)), !FULL);
}

// A post and, with it, the Create of its author.
async function ingest(document, raw) {
  const stored = await ingestDocument(document, raw);
  const authorship = authorshipOf(document);
  if (authorship) await ingestDocument(authorship);
  return stored;
}

async function ingestDocument(document, raw) {
  const response = await atraceFetch(ATRACE_API + "/collect/ingest", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({observed_at: new Date().toISOString(), observed_by: OBSERVED_BY, raw, document}),
    signal: AbortSignal.timeout(30_000),
  });
  if (!response.ok) throw new Error(`Atrace ingest ${document.type}: HTTP ${response.status}: ${(await response.text()).slice(0, 300)}`);
  return response.json();
}

async function readZhihu(page, path) {
  const response = await page.fetch(path, {timeout: 30_000});
  if (response.status === 403) {
    throw new Error(`Zhihu risk control (HTTP 403) on ${path.split("?")[0]}; stopping so it can be retried later.`);
  }
  if (!response.ok || new URL(response.url).origin !== ZHIHU) {
    throw new Error(`Zhihu ${path.split("?")[0]}: HTTP ${response.status}`);
  }
  return JSON.parse(response.body);
}

async function* pages(page, path, query = `limit=${PAGE_SIZE}&offset=0`, maxPages = Infinity) {
  let next = new URL(query ? `${path}?${query}` : path, ZHIHU);
  const seen = new Set();
  while (true) {
    const url = next.href;
    if (next.origin !== ZHIHU || next.pathname !== path || seen.has(url)) {
      throw new Error("Invalid or repeated Zhihu pagination URL");
    }
    seen.add(url);
    const payload = await readZhihu(page, url);
    if (!Array.isArray(payload.data) || typeof payload.paging?.is_end !== "boolean") {
      throw new Error(`Unexpected Zhihu list response at ${path}`);
    }
    yield payload;
    if (payload.paging.is_end || seen.size >= maxPages) return;
    if (!payload.paging.next) throw new Error(`Missing next page at ${path}`);
    next = new URL(payload.paging.next, ZHIHU);
    await pause(PAGE_DELAY_MIN_MS + Math.random() * (PAGE_DELAY_MAX_MS - PAGE_DELAY_MIN_MS));
  }
}

async function normalizeBody(page, items) {
  return page.evaluate(bodies => bodies.map(body => {
    // Template content is inert. Traverse text only and keep media separately.
    const template = document.createElement("template");
    template.innerHTML = body;
    const images = new Map();
    const blocks = new Set(["P", "DIV", "LI", "BLOCKQUOTE", "PRE", "H1", "H2", "H3", "H4", "H5", "H6", "FIGURE", "FIGCAPTION", "TR"]);
    function text(node) {
      if (node.nodeType === Node.TEXT_NODE) return node.nodeValue;
      const tag = node.tagName;
      if (tag === "SCRIPT" || tag === "STYLE" || tag === "TEMPLATE" || tag === "NOSCRIPT") return "";
      if (tag === "BR") return "\n";
      if (tag === "IMG") {
        for (const source of [node.getAttribute("data-original"), node.getAttribute("src")]) {
          if (!source) continue;
          try {
            const url = new URL(source, location.origin);
            if (url.protocol !== "https:" || !url.hostname.endsWith(".zhimg.com")) continue;
            images.set(url.href, {type: "Image", url: url.href});
            break;
          } catch {}
        }
        return "";
      }
      const inner = Array.from(node.childNodes, text).join("");
      return blocks.has(tag) ? "\n" + inner + "\n" : inner;
    }
    return {
      content: text(template.content).replace(/[ \t]+\n/g, "\n").replace(/\n{3,}/g, "\n\n").trim(),
      attachment: Array.from(images.values()),
    };
  }), items.map(x => x?.content_html || (typeof x?.content === "string" && x.content ? x.content : x?.excerpt || "")));
}

const imageCopies = new Map();

// Register a local copy of an image once per run; throws when it cannot.
function ensureLocalImage(url) {
  if (!imageCopies.has(url)) {
    imageCopies.set(url, (async () => {
      const query = encodeURIComponent(url);
      const known = await atraceFetch(`${ATRACE_API}/collect/media?url=${query}`, {signal: AbortSignal.timeout(10_000)});
      if (known.ok) return;
      const source = await fetch(url, {redirect: "error", signal: AbortSignal.timeout(20_000)});
      const mime = source.headers.get("content-type")?.split(";")[0] || "";
      if (!source.ok || !mime.startsWith("image/")) throw new Error(`image download failed: HTTP ${source.status} ${mime}`);
      const saved = await atraceFetch(`${ATRACE_API}/collect/media?url=${query}`, {
        method: "PUT",
        headers: {"Content-Type": mime},
        body: await source.arrayBuffer(),
        signal: AbortSignal.timeout(30_000),
      });
      if (!saved.ok) throw new Error(`atrace /collect/media: HTTP ${saved.status}`);
    })());
  }
  return imageCopies.get(url);
}

async function saveContent(entry, body, raw, actor, stats) {
  const found = contentDocuments(entry, body, actor);
  if (!found) return "";
  const downloads = [];
  for (const { role, document, downloads: images } of found.entries) {
    await ingest(document, role === "post" ? raw : undefined);
    downloads.push(...(images || []));
  }
  for (let start = 0; start < downloads.length; start += 3) {
    const copied = await Promise.allSettled(downloads.slice(start, start + 3).map(ensureLocalImage));
    stats.imageFailures += copied.filter(result => result.status === "rejected").length;
  }
  return found.object;
}

async function collectLikes(page, me, actor, favorites, favoriteVotes) {
  const stats = {pages: 0, fromFeed: 0, fromFavorites: 0, newLikes: 0, missingTimes: 0, imageFailures: 0};
  const seen = new Set();
  const path = `/api/v3/moments/${encodeURIComponent(me.url_token)}/activities`;
  const stop = newestFirst();
  for await (const batch of pages(page, path, "", MAX_PAGES)) {
    stats.pages++;
    if (batch.paging.need_force_login) throw new Error("Zhihu requires login to read activity history");
    // Newest first: only the likes that are not stored yet are read, and the feed is done where it runs into stored ones.
    const rows = await unknownOf(stop, voteRows(batch.data, me.id), row => likeKey(actor, row.target));
    const bodies = await normalizeBody(page, rows.map(row => row.target));
    for (const [index, row] of rows.entries()) {
      const entry = row.target;
      const id = platformID(entry.id);
      if (!id) continue;
      const key = `zhihu:${entry.type}:${id}`;
      if (seen.has(key)) continue;
      // The collection endpoint already supplied a fuller snapshot for this answer.
      const object = favorites.has(key) ? key
        : await saveContent(entry, bodies[index], entry, actor, stats);
      if (!object) continue;
      const published = timestamp(row.created_time);
      const result = await ingest(likeDocument(actor, object, published), row);
      seen.add(object);
      stats.fromFeed++;
      if (!published) stats.missingTimes++;
      if (result.created) stats.newLikes++;
    }
    if (stop.done) break;
  }
  for (const [object, evidence] of favoriteVotes) {
    if (seen.has(object)) continue;
    const result = await ingest(likeDocument(actor, object),
      {source: "zhihu.collection", ...evidence});
    stats.fromFavorites++;
    stats.missingTimes++;
    if (result.created) stats.newLikes++;
  }
  return stats;
}

// The user's underlined sentences: the answer or article each is in is stored (the page's own
// copy, read once), then the sentence as a Note quoting it and an Add to the highlights collection.
async function collectHighlights(page, me, actor) {
  const stats = {pages: 0, returned: 0, newAdds: 0, skipped: 0, imageFailures: 0};
  const collection = highlightCollectionDocument(me, actor);
  await ingest(collection);
  const stored = new Map();
  const path = `/api/v4/members/${encodeURIComponent(me.id)}/segments`;
  const stop = newestFirst();
  // The sources that were not to be had one after another: a few are answers that went away, many are a refusal.
  let unavailable = 0;
  for await (const batch of pages(page, path, `content_type=all&offset=0&limit=${PAGE_SIZE}`, MAX_PAGES)) {
    stats.pages++;
    const segments = await unknownOf(stop, batch.data, segment => keyOf(highlightDocument(segment, actor) && addDocument(actor, highlightDocument(segment, actor).id, collection.id)));
    for (const segment of segments) {
      stats.returned++;
      const note = highlightDocument(segment, actor);
      if (!note) {
        stats.skipped++;
        continue;
      }
      if (!stored.has(note.quote)) {
        const source = segment.source_content;
        try {
          const entry = await readZhihu(page, `/api/v4/${source.type}s/${platformID(source.token ?? source.id)}?include=content,created_time,updated_time`);
          const [body] = await normalizeBody(page, [entry]);
          stored.set(note.quote, await saveContent(entry, body, entry, actor, stats));
          unavailable = 0;
        } catch (error) {
          if (!sourceUnavailable(error) || (unavailable += 1) >= UNAVAILABLE_RUN) throw error;
          console.warn(`The source of a highlight is not to be had, skipped: ${note.quote}: ${error.message}`);
          stored.set(note.quote, "");
        }
        await pause(PAGE_DELAY_MIN_MS + Math.random() * (PAGE_DELAY_MAX_MS - PAGE_DELAY_MIN_MS));
      }
      if (stored.get(note.quote) !== note.quote) {
        stats.skipped++;
        continue;
      }
      await ingest(note, segment);
      const add = await ingest(addDocument(actor, note.id, collection.id));
      if (add.created) stats.newAdds++;
    }
    if (stop.done) break;
  }
  return stats;
}

// The user's own answers, articles and pins: a Note or Article and a Create.
async function collectOwn(page, me, actor) {
  const stats = {answers: 0, articles: 0, pins: 0, skipped: 0, missingTimes: 0, imageFailures: 0};
  const token = encodeURIComponent(me.url_token);
  const include = encodeURIComponent("data[*].content,excerpt,created_time,updated_time");
  const sources = [
    ["answer", `/api/v4/members/${token}/answers`, `limit=${PAGE_SIZE}&offset=0&sort_by=created&include=${include}`],
    ["article", `/api/v4/members/${token}/articles`, `limit=${PAGE_SIZE}&offset=0&sort_by=created&include=${include}`],
    ["pin", `/api/v4/members/${token}/pins`, `limit=${PAGE_SIZE}&offset=0`],
  ];
  for (const [type, path, query] of sources) {
    const stop = newestFirst();
    for await (const batch of pages(page, path, query, MAX_PAGES)) {
      // Newest first: only what is not stored yet is read, and the list is done where it runs into stored ones.
      const fresh = await unknownOf(stop, batch.data, entry => createKey(actor, entry));
      const bodies = type === "pin" ? fresh.map(pinBody) : await normalizeBody(page, fresh);
      for (const [index, entry] of fresh.entries()) {
        const object = await saveContent(entry, bodies[index], entry, actor, stats);
        if (!object) {
          stats.skipped++;
          continue;
        }
        const published = timestamp(entry.created_time ?? entry.created);
        if (!published) stats.missingTimes++;
        await ingest(createDocument(actor, object, published));
        stats[`${type}s`]++;
      }
      if (stop.done) break;
    }
  }
  return stats;
}

// Read one answer, article or pin again: Zhihu's answer for it now, stored as the lists store it. Zhihu's comments are
// not collected on any page, so this is the content alone.
async function readTarget(page, actor) {
  const found = targetOf(TARGET);
  if (!found) throw new Error(`not an answer, article or pin of Zhihu: ${TARGET}`);
  const stats = { imageFailures: 0 };
  const include = encodeURIComponent("content,excerpt,created_time,updated_time");
  const entry = await readZhihu(page, `/api/v4/${found.type}s/${found.id}?include=${include}`);
  const [body] = found.type === "pin" ? [pinBody(entry)] : await normalizeBody(page, [entry]);
  const object = await saveContent(entry, body, entry, actor, stats);
  if (!object) throw new Error(`${TARGET} is gone, or Zhihu does not give it out`);
  return { post: TARGET, stored: object, imageFailures: stats.imageFailures };
}

async function collect(page) {
  const me = await readZhihu(page, "/api/v4/me");
  const userID = platformID(me.id);
  if (!userID || !me.url_token) throw new Error("Sign in to Zhihu in the Ego profile before collecting");
  const actor = `zhihu:user:${userID}`;
  if (typeof checkCollectorAccount === "function") await checkCollectorAccount(actor, me.name);
  await ingest(personDocument(me, actor));
  if (TARGET) return readTarget(page, actor);
  const stats = {collections: 0, reported: 0, returned: 0, imported: 0, newAdds: 0, skipped: 0, missingTimes: 0, imageFailures: 0};
  const favorites = new Set();
  const favoriteVotes = new Map();
  for await (const folders of pages(page, `/api/v4/members/${encodeURIComponent(me.url_token)}/favlists`, undefined, MAX_PAGES)) {
    for (const folder of folders.data) {
      const collection = collectionDocument(folder, actor);
      const id = platformID(folder.id);
      const target = collection.id;
      await ingest(collection, folder);
      stats.collections++;
      let reported = false;
      const stop = newestFirst();
      for await (const batch of pages(page, `/api/v4/collections/${id}/items`, undefined, MAX_PAGES)) {
        if (!reported) {
          stats.reported += batch.paging.totals ?? 0;
          reported = true;
        }
        stats.returned += batch.data.length;
        // Newest first: only what is not stored yet is read, and the folder is done where it runs into stored ones.
        const rows = await unknownOf(stop, batch.data, row => addKey(actor, row.content, target));
        const bodies = await normalizeBody(page, rows.map(row => row.content));
        for (const [index, row] of rows.entries()) {
          const entry = row.content;
          const object = await saveContent(entry, bodies[index], row, actor, stats);
          if (!object) {
            stats.skipped++;
            continue;
          }
          favorites.add(object);
          const published = timestamp(row.created);
          if (!published) stats.missingTimes++;
          const add = await ingest(addDocument(actor, object, target, published));
          if (add.created) stats.newAdds++;
          stats.imported++;
          if (entry.type === "answer" && entry.relationship?.voting === 1) {
            favoriteVotes.set(object, {folderId: id, voting: 1});
          }
        }
        if (stop.done) break;
      }
    }
  }
  stats.highlights = await collectHighlights(page, me, actor);
  stats.own = await collectOwn(page, me, actor);
  stats.likes = await collectLikes(page, me, actor, favorites, favoriteVotes);
  return stats;
}

// ego's Node drops all console output when the first asynchronous operation is
// a fetch; any timer first keeps it.
await pause(0);
const health = await atraceFetch(ATRACE_API + "/healthz", {signal: AbortSignal.timeout(5_000)});
if (!health.ok || !(await health.json()).ok) throw new Error("Atrace health check failed");
const existingSpace = (await listTaskSpaces()).find(space => space.name === TASK_SPACE);
if (existingSpace && existingSpace.ownership !== "agent") {
  throw new Error(`Task space ${TASK_SPACE} is user-owned; refusing to claim it automatically.`);
}
const task = existingSpace
  ? await taskSpace(existingSpace.id)
  : await taskSpace(TASK_SPACE, {profileId: PROFILE_ID});
const page = task.page("p1");
let stats;
try {
  await page.goto(ZHIHU + "/", {timeout: 60_000});
  stats = await collect(page);
} finally {
  await task.finish({keep: []});
}
console.log(JSON.stringify({platform: "zhihu", ...stats}));
} // collector body

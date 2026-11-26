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
// Run with: atrace collect bilibili
// The signed-in Ego Lite profile supplies the logged-in Bilibili session. No token
// is read or stored. Requests go to api.bilibili.com from inside a bilibili.com
// page with the page's cookies, so only unsigned endpoints are used.
//   coins       Add to bilibili:collection:coins, dated by the coin time (a coin is
//               a keepsake, not a like)
//   likes       Like (Bilibili gives no like time)
//   favorites   Add to the folder (a Collection) the video is in, dated by the
//               time it was favorited
//   dynamics    own posts: Note + Create; forwards: Announce of the original
// A video is stored as a Video of its uploader. Likes, coins and favorites show
// only the first page. Covers are downloaded and registered as local copies.
//
// The file has two halves. Above the marker line are the constants and the
// functions that turn what Bilibili returns (videos, favorites, dynamics) into
// documents; they touch no browser or network, and the tests load only that
// half (collectors/AGENTS.md). Below it are the page, the requests and the order
// things are stored in.

const PROFILE_ID = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.profileId : "Default";
const TASK_SPACE = "atrace bilibili collector";
const ATRACE_API = typeof ATRACE_SERVER === "string" ? ATRACE_SERVER : "http://127.0.0.1:8183";
const OBSERVED_BY = "bilibili-ego";
const PAGE_SIZE = 20;
// An ordinary run reads each list from the newest back until it meets what is stored (see KNOWN_RUN),
// at most MAX_PAGES pages. A full crawl (atrace collect <platform> --full puts
// `ATRACE_FULL=1` in the environment) reads every page of favorites and dynamics, with
// longer gaps.
const FULL = typeof ATRACE_FULL !== "undefined" && ATRACE_FULL;
// `ATRACE_TARGET=bilibili:video:BV1xx411c7mD` in the environment reads that one video or dynamic and
// nothing else: it is what the device does when the owner opens it. Without it the whole collection runs.
const TARGET = typeof ATRACE_TARGET === "string" ? ATRACE_TARGET : "";
const MAX_PAGES = FULL ? Infinity : 10;
const REQUEST_DELAY_MIN_MS = FULL ? 10_000 : 3_000;
const REQUEST_DELAY_MAX_MS = FULL ? 20_000 : 6_000;

// What a target such as "bilibili:video:BV1xx411c7mD" or "bilibili:dynamic:123" names; null when it is neither.
function targetOf(target) {
  const video = /^bilibili:video:(BV[0-9A-Za-z]{10})$/.exec(target);
  if (video) return { type: "video", id: video[1] };
  const dynamic = /^bilibili:dynamic:(\d{1,25})$/.exec(target);
  return dynamic ? { type: "dynamic", id: dynamic[1] } : null;
}

function seconds(value) {
  const time = Number(value);
  return Number.isFinite(time) && time > 0 ? new Date(time * 1000).toISOString() : undefined;
}

// An image URL on Bilibili's image host, forced to https; null for anything else.
function imageOf(value) {
  let url;
  try {
    url = new URL(String(value || "").replace(/^http:/, "https:").replace(/^\/\//, "https://"));
  } catch {
    return null;
  }
  if (url.protocol !== "https:" || !/(^|\.)hdslb\.com$/.test(url.hostname)) return null;
  return { type: "Image", url: url.href };
}

// A person from an owner or author object, or null when it has no mid.
function personDocument(owner) {
  const id = String(owner?.mid || "");
  if (!id) return null;
  return { type: "Person", id: `bilibili:user:${id}`, url: `https://space.bilibili.com/${id}`, ...(owner.name ? { name: owner.name } : {}) };
}

// The uploader's description of a video; Bilibili fills in a lone "-" when there is none, which is no description.
function descriptionOf(video) {
  const text = String(video.desc || video.intro || "").trim();
  return text === "-" ? "" : text;
}

// A video becomes a Video of its uploader. downloads lists the cover to fetch.
function videoDocuments(video) {
  const bvid = video.bvid;
  if (!bvid) throw new Error("video has no bvid");
  const owner = personDocument(video.owner || video.upper);
  const cover = imageOf(video.pic || video.cover);
  const published = seconds(video.pubdate || video.pubtime);
  return [
    ...(owner ? [{ role: "author", document: owner }] : []),
    {
      role: "post",
      document: {
        type: "Video",
        id: `bilibili:video:${bvid}`,
        ...(owner ? { attributedTo: owner.id } : {}),
        name: video.title || "",
        // The uploader's description is the summary of a video; the title is its name.
        ...(descriptionOf(video) ? { summary: descriptionOf(video) } : {}),
        url: `https://www.bilibili.com/video/${bvid}`,
        ...(published ? { published } : {}),
        ...(cover ? { attachment: [cover] } : {}),
      },
      downloads: cover ? [cover.url] : [],
    },
  ];
}

// The Like for a liked video; Bilibili gives no time for it.
function likeDocument(meId, video) {
  return { type: "Like", actor: `bilibili:user:${meId}`, object: `bilibili:video:${video.bvid}` };
}
// The coins a user gave are kept in a collection of their own, dated by the coin time.
const COINS = "bilibili:collection:coins";
function coinsCollectionDocument(meId) {
  return { type: "Collection", id: COINS, name: "B站投币", attributedTo: `bilibili:user:${meId}`, url: `https://space.bilibili.com/${meId}` };
}
function coinDocument(meId, video) {
  const published = seconds(video.time);
  return { type: "Add", actor: `bilibili:user:${meId}`, object: `bilibili:video:${video.bvid}`, target: COINS, ...(published ? { published } : {}) };
}
function addDocument(meId, media, target) {
  const published = seconds(media.fav_time);
  return { type: "Add", actor: `bilibili:user:${meId}`, object: `bilibili:video:${media.bvid}`, target, ...(published ? { published } : {}) };
}

function collectionDocument(folder, meId) {
  return {
    type: "Collection", id: `bilibili:collection:${folder.id}`, name: folder.title || "", attributedTo: `bilibili:user:${meId}`,
    url: `https://space.bilibili.com/${meId}/favlist?fid=${folder.id}`,
  };
}

// Whether a favorites entry is a plain video; audio, courses and invalid
// entries are skipped.
function isVideoMedia(media) {
  return media.type === 2 && Boolean(media.bvid);
}

function dynamicText(item) {
  const dynamic = item?.modules?.module_dynamic;
  return dynamic?.desc?.text || dynamic?.major?.opus?.summary?.text || dynamic?.major?.archive?.title || "";
}

function dynamicImages(item) {
  const dynamic = item?.modules?.module_dynamic;
  const sources = (dynamic?.major?.opus?.pics || dynamic?.major?.draw?.items || []).map(pic => pic.url || pic.src);
  return sources.map(imageOf).filter(Boolean);
}

// One of the user's own dynamics, or null when it belongs to someone else. kind
// is "forward" (an Announce of the original, which is kept as a Note of its own
// author) or "dynamic" (a Note and a Create).
// Whether a forward says anything of its own: Bilibili's stand-in text for a forward without words does not.
function hasOwnWords(text) {
  return !/^\s*(转发动态)?\s*$/.test(String(text || ""));
}

function dynamicDocuments(item, meId) {
  const author = item.modules?.module_author;
  if (String(author?.mid) !== meId) return null;
  const id = `bilibili:dynamic:${item.id_str}`;
  const published = seconds(author?.pub_ts);
  const url = `https://www.bilibili.com/opus/${item.id_str}`;
  const meRef = `bilibili:user:${meId}`;
  if (item.type === "DYNAMIC_TYPE_FORWARD" && item.orig?.id_str) {
    const original = item.orig;
    const originalAuthor = original.modules?.module_author;
    const originalOwner = personDocument({ mid: originalAuthor?.mid, name: originalAuthor?.name });
    const originalId = `bilibili:dynamic:${original.id_str}`;
    const originalImages = dynamicImages(original);
    return {
      kind: "forward",
      entries: [
        ...(originalOwner ? [{ role: "orig-author", document: originalOwner }] : []),
        {
          role: "orig",
          document: {
            type: "Note",
            id: originalId,
            ...(originalOwner ? { attributedTo: originalOwner.id } : {}),
            content: dynamicText(original),
            url: `https://www.bilibili.com/opus/${original.id_str}`,
            ...(seconds(originalAuthor?.pub_ts) ? { published: seconds(originalAuthor.pub_ts) } : {}),
            ...(originalImages.length ? { attachment: originalImages } : {}),
          },
          downloads: originalImages.map(image => image.url),
        },
        ...(hasOwnWords(dynamicText(item))
          // A forward with words of the user's own is their post quoting the original.
          ? [
            { role: "post", document: { type: "Note", id, attributedTo: meRef, quote: originalId, content: dynamicText(item), url, ...(published ? { published } : {}) } },
            { role: "create", document: { type: "Create", actor: meRef, object: id, ...(published ? { published } : {}) } },
          ]
          // A plain forward: Bilibili fills in "转发动态" when there are no words.
          : [{ role: "post", document: { type: "Announce", id, actor: meRef, object: originalId, url, ...(published ? { published } : {}) } }]),
      ],
    };
  }
  const attachment = dynamicImages(item);
  return {
    kind: "dynamic",
    entries: [
      {
        role: "post",
        document: {
          type: "Note", id, attributedTo: meRef, content: dynamicText(item), url,
          ...(published ? { published } : {}),
          ...(attachment.length ? { attachment } : {}),
        },
        downloads: attachment.map(image => image.url),
      },
      { role: "create", document: { type: "Create", actor: meRef, object: id, ...(published ? { published } : {}) } },
    ],
  };
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

// What a listed video, favorite or dynamic leaves in the archive; null for what leaves nothing.
function keyOfDocument(document) {
  try {
    return document ? activityKey([{ document }]) : null;
  } catch {
    return null;
  }
}

function dynamicKey(item, meId) {
  try {
    const found = dynamicDocuments(item, meId);
    return found ? activityKey(found.entries) : null;
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

// ==== side effects below: the page, the requests and storage order; not loaded by tests ====

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

// The items of a page, newest first, that are not stored; state.done says the list ran into stored ones.
async function unknownOf(state, items, keyOf) {
  return takeUnknown(state, items, await knownOf(items.map(keyOf)), !FULL);
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

// Download an image once and register it as the local copy of its URL.
async function ensureLocalImage(url) {
  if (downloaded.has(url)) return;
  const known = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(url)}`);
  if (!known.ok) {
    await pause(randomDelay(1_000, 2_500));
    const response = await fetch(url, { headers: { Referer: "https://www.bilibili.com/" }, signal: AbortSignal.timeout(30_000) });
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

// Store the entries in order, fetching each entry's images first; rawByRole
// gives the raw response each role keeps.
async function storeEntries(entries, rawByRole) {
  for (const entry of entries) {
    for (const url of entry.downloads || []) await ensureLocalImage(url);
  }
  for (const entry of entries) await ingest(entry.document, rawByRole[entry.role]);
}

// ---- Bilibili --------------------------------------------------------------

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
if (!(await page.url()).startsWith("https://www.bilibili.com/")) {
  await page.goto("https://www.bilibili.com/", { timeout: 60_000 });
  await pause(4_000);
}

// A cross-origin request from the page, with the page's cookies.
async function api(url) {
  await pause(randomDelay(REQUEST_DELAY_MIN_MS, REQUEST_DELAY_MAX_MS));
  const response = await page.evaluate(async address => {
    const result = await fetch(address, { credentials: "include" });
    return { status: result.status, body: await result.text() };
  }, url);
  if (response.status !== 200) throw new Error(`${url}: HTTP ${response.status}`);
  const payload = JSON.parse(response.body);
  if (payload.code === -101) throw new Error("The Ego profile is not logged in to Bilibili.");
  if (payload.code === -799 || payload.code === -412) throw new Error(`Bilibili is rate limiting requests (${payload.code}); try again later.`);
  if (payload.code !== 0) throw new Error(`${url}: code ${payload.code} ${payload.message}`);
  return payload.data;
}

const nav = await api("https://api.bilibili.com/x/web-interface/nav");
if (!nav.isLogin || !nav.mid) throw new Error("The Ego profile is not logged in to Bilibili.");
const MID = String(nav.mid);
if (typeof checkCollectorAccount === "function") await checkCollectorAccount(`bilibili:user:${MID}`, nav.uname, task);
await ingest(personDocument({ mid: MID, name: nav.uname || "" }));

// Read one video or dynamic again: what Bilibili gives for it now, stored as the lists store it. Bilibili's comments
// are not collected on any page, so this is the post alone.
async function readTarget() {
  const found = targetOf(TARGET);
  if (!found) throw new Error(`not a video or dynamic of Bilibili: ${TARGET}`);
  if (found.type === "video") {
    const video = await api(`https://api.bilibili.com/x/web-interface/view?bvid=${found.id}`);
    if (video?.bvid !== found.id) throw new Error(`the answer for ${TARGET} does not hold the video`);
    await saveVideo(video, "bilibili.web-interface.view");
  } else {
    const detail = await api(`https://api.bilibili.com/x/polymer/web-dynamic/v1/detail?id=${found.id}`);
    const item = detail?.item;
    if (item?.id_str !== found.id) throw new Error(`the answer for ${TARGET} does not hold the dynamic`);
    const stored = dynamicDocuments(item, MID);
    if (!stored) {
      // The original of a repost is somebody else's dynamic: only your own are collected, so there is nothing to read.
      console.log(JSON.stringify({ platform: "bilibili", account: nav.uname, uid: MID, post: TARGET, skipped: "not one of your own dynamics" }));
      return;
    }
    const raw = { source: "bilibili.dynamic.detail", observed_at: new Date().toISOString(), response: item };
    await storeEntries(stored.entries, { post: raw, orig: { source: "bilibili.dynamic.detail.orig", response: item.orig } });
  }
  console.log(JSON.stringify({ platform: "bilibili", account: nav.uname, uid: MID, post: TARGET }));
}

const counts = { coins: 0, likes: 0, favorites: 0, folders: 0, dynamics: 0, forwards: 0, skipped: 0, failed: 0 };

// A video becomes a Video of its uploader and its ID is returned.
async function saveVideo(video, source) {
  const entries = videoDocuments(video);
  await storeEntries(entries, { post: { source, observed_at: new Date().toISOString(), response: video } });
  return entries.find(entry => entry.role === "post").document.id;
}

// ---- coins and likes -------------------------------------------------------

// A coined video that was also liked is both: a coin in the coins collection and a like.
async function importLikes(path, label, source) {
  const data = await api(`https://api.bilibili.com/x/space/${path}?vmid=${MID}`);
  const listed = Array.isArray(data) ? data : data.list || [];
  const document = video => label === "coins" ? coinDocument(MID, video) : likeDocument(MID, video);
  // Newest first: only what is not stored yet is kept, and stored oldest first.
  const videos = await unknownOf(newestFirst(), listed, video => keyOfDocument(document(video)));
  console.log(`${label}: ${videos.length} new video(s) of ${listed.length}.`);
  for (const video of videos.reverse()) {
    try {
      await saveVideo(video, source);
      await ingest(document(video));
      counts[label] += 1;
    } catch (error) {
      counts.failed += 1;
      console.warn(`Skipped ${label} ${video.bvid || video.aid}: ${error.message}`);
    }
  }
}

if (TARGET) {
  await readTarget();
} else {
  await ingest(coinsCollectionDocument(MID));
  await importLikes("coin/video", "coins", "bilibili.space.coin.video");
  await importLikes("like/video", "likes", "bilibili.space.like.video");

  // ---- favorites -------------------------------------------------------------

  const folders = (await api(`https://api.bilibili.com/x/v3/fav/folder/created/list-all?up_mid=${MID}`)).list || [];
  for (const folder of folders) {
    const collection = collectionDocument(folder, MID);
    const target = collection.id;
    await ingest(collection, { source: "bilibili.fav.folder", response: folder });
    counts.folders += 1;
    const stop = newestFirst();
    for (let pn = 1; pn <= MAX_PAGES; pn += 1) {
      const data = await api(`https://api.bilibili.com/x/v3/fav/resource/list?media_id=${folder.id}&pn=${pn}&ps=${PAGE_SIZE}&platform=web`);
      const listed = data?.medias || [];
      // Newest first: what is stored already ends the folder.
      const medias = await unknownOf(stop, listed, media => isVideoMedia(media) ? keyOfDocument(addDocument(MID, media, target)) : null);
      console.log(`favorites of "${folder.title}": ${medias.length} new item(s) of ${listed.length}, ${data?.info?.media_count ?? "?"} in the folder.`);
      for (const media of medias.reverse()) {
        try {
          if (!isVideoMedia(media)) {
            counts.skipped += 1;
            continue;
          }
          await saveVideo(media, "bilibili.fav.resource.list");
          await ingest(addDocument(MID, media, target));
          counts.favorites += 1;
        } catch (error) {
          counts.failed += 1;
          console.warn(`Skipped favorite ${media.bvid || media.id}: ${error.message}`);
        }
      }
      if (stop.done || !data?.has_more || !listed.length) break;
    }
  }

  // ---- own dynamics ----------------------------------------------------------

  let offset = "";
  const dynamicsStop = newestFirst();
  for (let pn = 1; pn <= MAX_PAGES; pn += 1) {
    const feed = await api(`https://api.bilibili.com/x/polymer/web-dynamic/v1/feed/space?host_mid=${MID}${offset ? `&offset=${offset}` : ""}`);
    // Newest first: what is stored already ends the feed.
    const fresh = await unknownOf(dynamicsStop, feed.items || [], item => dynamicKey(item, MID));
    for (const item of fresh.reverse()) {
      try {
        const found = dynamicDocuments(item, MID);
        if (!found) continue;
        const raw = { source: "bilibili.dynamic.feed.space", observed_at: new Date().toISOString(), response: item };
        await storeEntries(found.entries, { post: raw, orig: { source: "bilibili.dynamic.feed.space.orig", response: item.orig } });
        counts[found.kind === "forward" ? "forwards" : "dynamics"] += 1;
      } catch (error) {
        counts.failed += 1;
        console.warn(`Skipped dynamic ${item.id_str}: ${error.message}`);
      }
    }
    offset = String(feed.offset || "");
    if (dynamicsStop.done || !feed.has_more || !offset || !(feed.items || []).length) break;
  }

  console.log(JSON.stringify({ platform: "bilibili", account: nav.uname, uid: MID, ...counts }));
}

await task.finish({ keep: [] });
} // collector body

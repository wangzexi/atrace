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
// Run with: atrace collect douyin
// The signed-in Ego Lite profile supplies the logged-in Douyin session. No token is
// read or stored, and Douyin's signed API is never called by the collector:
// the user's own profile page is opened, its tabs are clicked, and the
// responses the page itself receives (read over CDP) are used.
//   works       Video + Create (only the last 31 days)
//   likes       Like         favorites   Add to douyin:collection:favorites
//   recommends  Add to douyin:collection:recommended (the 推荐 tab: videos the user recommended)
// Each list is read newest first, until it runs into videos that are stored. Videos are
// stored as a Video of their author with the description, hashtags and a
// downloaded cover. Douyin gives no like or favorite time, so those items are
// stored oldest first to keep the order the user acted in.
//
// The file has two halves. Above the marker line are the constants and the
// functions that turn a video the page received into documents; they touch no
// browser or network, and the tests load only that half (collectors/AGENTS.md).
// Below it are the pages, the network and the order things are stored in.

const PROFILE_ID = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.profileId : "Default";
const TASK_SPACE = "atrace douyin collector";
const ATRACE_API = typeof ATRACE_SERVER === "string" ? ATRACE_SERVER : "http://127.0.0.1:8183";
const OBSERVED_BY = "douyin-ego";
// An ordinary run reads each list from the newest back until it meets what is stored (see KNOWN_RUN),
// at most MAX_PAGES pages. A full crawl (atrace collect <platform> --full puts
// `ATRACE_FULL=1` in the environment) scrolls the list down until no more videos come.
const FULL = typeof ATRACE_FULL !== "undefined" && ATRACE_FULL;
// `ATRACE_TARGET=douyin:video:7300000000000000000` in the environment reads that one video and nothing
// else: it is what the device does when the owner opens it. Without it the whole collection runs.
const TARGET = typeof ATRACE_TARGET === "string" ? ATRACE_TARGET : "";
const MAX_PAGES = FULL ? Infinity : 10;
const FAVORITES = "douyin:collection:favorites";
const RECOMMENDED = "douyin:collection:recommended"; // the 推荐 tab: videos the user recommended
const TAB_WAIT_MS = 9_000;
const IMAGE_HOSTS = /(^|\.)(douyinpic\.com|byteimg\.com|douyinstatic\.com|bytecdn\.cn)$/;

// The video ID a target such as "douyin:video:7300000000000000000" names; "" when it is not one.
function awemeIdOf(target) {
  return (/^douyin:video:(\d{1,25})$/.exec(target) || [])[1] || "";
}

function seconds(value) {
  const time = Number(value);
  return Number.isFinite(time) && time > 0 ? new Date(time * 1000).toISOString() : undefined;
}

// Douyin image URLs carry an expiry and a signature in their query, and rotate
// between hosts (p3-, p9-, ...), so they change with every response. The
// documents keep one address per image: the path without the query, on one host.
function stableImageUrl(source) {
  const url = new URL(source);
  url.search = "";
  url.hostname = url.hostname.replace(/^p\d+-/, "p3-");
  return url.href;
}

// The usable image URLs among candidates, in order: the stable address for the
// document and the one the video was seen at, to download from.
function imageCandidates(candidates) {
  const found = [];
  for (const candidate of candidates) {
    let url;
    try {
      url = new URL(String(candidate || "").replace(/^http:/, "https:"));
    } catch {
      continue;
    }
    if (url.protocol !== "https:" || !IMAGE_HOSTS.test(url.hostname)) continue;
    found.push({ url: stableImageUrl(url.href), source: url.href });
  }
  return found;
}

function coverCandidates(video) {
  return imageCandidates([
    ...(video.video?.cover?.url_list || []), ...(video.video?.origin_cover?.url_list || []),
    ...(video.images?.[0]?.url_list || []),
  ]);
}

function personDocument(user, fallbackUrl) {
  return {
    type: "Person",
    id: `douyin:user:${user.uid}`,
    name: user.nickname || "",
    url: user.sec_uid ? `https://www.douyin.com/user/${user.sec_uid}` : fallbackUrl,
  };
}

// A video (or image post): its author, when the response names one, and a Video
// of that author. cover is the image that was downloaded, or null.
function videoDocuments(video, cover) {
  const id = String(video.aweme_id || "");
  if (!id) throw new Error("video has no aweme_id");
  const author = video.author || {};
  const owner = author.uid ? `douyin:user:${author.uid}` : "";
  const published = seconds(video.create_time);
  const tags = (video.text_extra || []).map(item => item.hashtag_name).filter(Boolean);
  return [
    ...(owner ? [{ role: "author", document: personDocument(author, "https://www.douyin.com/") }] : []),
    {
      role: "post",
      document: {
        type: "Video",
        id: `douyin:video:${id}`,
        ...(owner ? { attributedTo: owner } : {}),
        content: video.desc || "",
        url: `https://www.douyin.com/video/${id}`,
        ...(published ? { published } : {}),
        ...(tags.length ? { tag: tags.map(name => ({ type: "Hashtag", name })) } : {}),
        ...(cover ? { attachment: [{ type: "Image", url: cover.url }] } : {}),
      },
    },
  ];
}

function createDocument(meId, video) {
  const published = seconds(video.create_time);
  return { type: "Create", actor: `douyin:user:${meId}`, object: `douyin:video:${video.aweme_id}`, ...(published ? { published } : {}) };
}

function likeDocument(meId, object) {
  return { type: "Like", actor: `douyin:user:${meId}`, object };
}

function addDocument(meId, object, target = FAVORITES) {
  return { type: "Add", actor: `douyin:user:${meId}`, object, target };
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

// What a listed video leaves in the archive when it is one of the user's works, or liked or favorited.
function activityKeyOf(document) {
  try {
    return activityKey([{ document }]);
  } catch {
    return null;
  }
}

const workKey = (video, meId) => activityKeyOf(createDocument(meId, video));
const reactionKey = (video, activityOf) => activityKeyOf(activityOf(`douyin:video:${video.aweme_id}`));

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

// The videos of a page, newest first, that are not stored; state.done says the list ran into stored ones.
async function unknownOf(state, videos, keyOf) {
  return takeUnknown(state, videos, await knownOf(videos.map(keyOf)), !FULL);
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
    await pause(1_000 + Math.random() * 1_500);
    const response = await fetch(source, { headers: { Referer: "https://www.douyin.com/" }, signal: AbortSignal.timeout(30_000) });
    if (!response.ok) throw new Error(`image download failed: HTTP ${response.status}`);
    const mime = response.headers.get("content-type")?.split(";")[0] || "";
    if (!mime.startsWith("image/")) throw new Error(`unexpected image MIME ${mime}`);
    const uploaded = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(url)}`, {
      method: "PUT",
      headers: { "Content-Type": mime },
      body: await response.arrayBuffer(),
    });
    if (!uploaded.ok) throw new Error(`atrace /collect/media: HTTP ${uploaded.status}: ${(await uploaded.text()).slice(0, 300)}`);
  }
  downloaded.add(url);
}

// The first candidate whose image can be downloaded and registered; null when
// there is none (a missing cover must not stop the collection).
async function chooseCover(candidates) {
  for (const candidate of candidates) {
    try {
      await ensureLocalImage(candidate.url, candidate.source);
      return candidate;
    } catch {
      continue;
    }
  }
  return null;
}

async function storeEntries(entries, rawByRole) {
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

// The bodies of the page's responses to a path, in the order they arrived;
// responses whose body is no longer available are skipped.
const pending = [];
async function responsesFor(path) {
  pending.push(...(await page.events()).filter(event => event.method === "Network.responseReceived"));
  const bodies = [];
  for (const event of pending) {
    if (!new URL(event.params.response.url).pathname.endsWith(path)) continue;
    try {
      bodies.push(JSON.parse((await page.cdp("Network.getResponseBody", { requestId: event.params.requestId })).body));
    } catch {}
  }
  return bodies;
}

async function openTab(label) {
  const clicked = await page.evaluate(text => {
    // The profile's own tabs first: the site's top navigation has a 推荐 of its own, which leaves the profile.
    const matches = node => (node.textContent || "").trim().startsWith(text) && node.children.length <= 2 && (node.textContent || "").trim().length < 12;
    const element = [...document.querySelectorAll("[role=tab]")].find(matches)
      || [...document.querySelectorAll("span, div")].find(matches);
    if (element) element.click();
    return Boolean(element);
  }, label);
  if (!clicked) throw new Error(`could not find the "${label}" tab`);
  await pause(TAB_WAIT_MS);
}

// The first batch of a tab: the first response that has videos.
async function firstBatch(path) {
  const found = (await responsesFor(path)).find(body => Array.isArray(body.aweme_list) && body.aweme_list.length);
  return found ? found.aweme_list : [];
}

// The videos of a list: the first batch, or in a full crawl every batch the page loads as it is
// scrolled, until three scrolls in a row bring none. Newest first, each video once.
async function allBatches(path) {
  if (!FULL) return firstBatch(path);
  const videos = new Map();
  const gather = async () => {
    for (const body of await responsesFor(path)) {
      for (const video of Array.isArray(body.aweme_list) ? body.aweme_list : []) videos.set(String(video.aweme_id), video);
    }
  };
  await gather();
  for (let idle = 0; idle < 3;) {
    const before = videos.size;
    await page.evaluate(() => {
      for (const element of document.querySelectorAll("*")) {
        if (element.scrollHeight > element.clientHeight + 200 && /auto|scroll/.test(getComputedStyle(element).overflowY)) element.scrollTop = element.scrollHeight;
      }
      window.scrollTo(0, document.body.scrollHeight);
    });
    await pause(10_000 + Math.random() * 10_000);
    await gather();
    console.log(`${path}: ${videos.size} video(s) so far.`);
    idle = videos.size > before ? 0 : idle + 1;
  }
  return [...videos.values()];
}

// ---- Douyin's own list requests ----------------------------------------------
// The lists are asked for the way the page asks for them, from inside the page: Douyin's own script signs a
// request made there, so the page's fetch is answered like its own. Slowly, a page of videos at a time (the
// answers are large). Each spec says where the list is, how a request for the page at a cursor is made and
// where the next cursor is in the answer. A list that cannot be read this way (a status_code that is not 0, a
// body that is not JSON) throws, and the caller falls back to the page's scrolling.

const LIST_PARAMS = { device_platform: "webapp", aid: "6383", channel: "channel_pc_web", pc_client_type: "1", version_code: "170400", version_name: "17.4.0", cookie_enabled: "true", browser_language: "zh-CN", browser_platform: "MacIntel", browser_name: "Chrome" };

const LISTS = {
  works: sec => ({ path: "/aweme/v1/web/aweme/post/", method: "GET", params: cursor => ({ sec_user_id: sec, max_cursor: cursor, count: "18", from_user_page: "0" }), next: body => body.max_cursor }),
  likes: sec => ({ path: "/aweme/v1/web/aweme/favorite/", method: "GET", params: cursor => ({ sec_user_id: sec, max_cursor: cursor, min_cursor: "0", count: "18" }), next: body => body.max_cursor }),
  favorites: () => ({ path: "/aweme/v1/web/aweme/listcollection/", method: "POST", params: () => ({}), form: cursor => `count=18&cursor=${cursor}`, next: body => body.cursor }),
  recommends: sec => ({ path: "/aweme/v1/web/familiar/recommend/feed/", method: "GET", params: cursor => ({ sec_user_id: sec, max_cursor: cursor, min_cursor: "0", whale_cut_token: "", count: "18", from: "1" }), next: body => body.max_cursor }),
};

// One page of a list at cursor: its videos, the next cursor and whether there is more.
async function listPage(spec, cursor) {
  const query = new URLSearchParams({ ...LIST_PARAMS, ...spec.params(String(cursor)) });
  const response = await page.evaluate(async ({ url, method, form }) => {
    const answer = await fetch(url, { method, credentials: "include", ...(form ? { headers: { "Content-Type": "application/x-www-form-urlencoded" }, body: form } : {}) });
    return { status: answer.status, text: await answer.text() };
  }, { url: `${spec.path}?${query}`, method: spec.method, form: spec.form ? spec.form(String(cursor)) : "" });
  let body;
  try {
    body = JSON.parse(response.text);
  } catch {
    throw new Error(`${spec.path}: HTTP ${response.status}, not JSON`);
  }
  if (body.status_code !== 0 || !Array.isArray(body.aweme_list)) throw new Error(`${spec.path}: status_code ${body.status_code}`);
  return { videos: body.aweme_list, cursor: Number(spec.next(body) || 0), more: Number(body.has_more) === 1 };
}

// Hands each page of a list to handle, newest first, until handle says the list is done (it returns true), the
// list ends or MAX_PAGES pages are read. A page after the first is waited for 8 to 15 seconds.
async function readList(spec, handle) {
  let cursor = 0;
  for (let pages = 1; ; pages += 1) {
    const result = await listPage(spec, cursor);
    const done = await handle(result.videos);
    cursor = result.cursor;
    if (done === true || !result.more || !result.videos.length || pages >= MAX_PAGES) return;
    await pause(8_000 + Math.random() * 7_000);
  }
}

await page.events();
await page.goto("https://www.douyin.com/user/self", { timeout: 60_000 });
await pause(TAB_WAIT_MS);
if (/login|passport/.test(await page.url())) throw new Error("The Ego profile is not logged in to Douyin.");

const profile = (await responsesFor("/aweme/v1/web/user/profile/self/"))[0]?.user;
if (!profile?.uid) throw new Error("Could not tell which Douyin account is logged in.");
const meRef = `douyin:user:${profile.uid}`;
if (typeof checkCollectorAccount === "function") await checkCollectorAccount(meRef, profile.nickname, task);
await ingest(personDocument(profile, "https://www.douyin.com/user/self"),
  { source: "douyin.user.profile.self", response: { uid: profile.uid, nickname: profile.nickname, sec_uid: profile.sec_uid } });

const counts = { works: 0, likes: 0, favorites: 0, recommends: 0, failed: 0 };

// A video (or image post) becomes a Video of its author and its ID is returned.
async function saveVideo(video, source) {
  const raw = { source, observed_at: new Date().toISOString(), response: video };
  const cover = await chooseCover(coverCandidates(video));
  await storeEntries(videoDocuments(video, cover), { post: raw });
  return `douyin:video:${video.aweme_id}`;
}

// ---- own works -------------------------------------------------------------

// Read one video again: what Douyin gives for it now, asked for from inside the page as the lists are, and stored
// as they are stored. Douyin's comments are not collected on any page, so this is the video alone.
async function readTarget() {
  const id = awemeIdOf(TARGET);
  if (!id) throw new Error(`not a video of Douyin: ${TARGET}`);
  const query = new URLSearchParams({ ...LIST_PARAMS, aweme_id: id });
  const response = await page.evaluate(async url => {
    const answer = await fetch(url, { credentials: "include" });
    return { status: answer.status, text: await answer.text() };
  }, `/aweme/v1/web/aweme/detail/?${query}`);
  let body;
  try {
    body = JSON.parse(response.text);
  } catch {
    throw new Error(`the detail of ${TARGET}: HTTP ${response.status}, not JSON`);
  }
  const video = body.aweme_detail;
  if (body.status_code !== 0 || String(video?.aweme_id) !== id) throw new Error(`${TARGET} is gone, or Douyin does not give it out (status_code ${body.status_code})`);
  await saveVideo(video, "douyin.aweme.detail");
  if (String(video.author?.uid) === String(profile.uid)) await ingest(createDocument(profile.uid, video));
  console.log(JSON.stringify({ platform: "douyin", account: profile.nickname, uid: String(profile.uid), post: TARGET }));
}

if (TARGET) {
  await readTarget();
} else {
  // The videos of a list: asked for as the page asks (readList), else what the page itself loads.
  async function listVideos(name, fallbackPath, keyOf) {
    const stop = newestFirst();
    try {
      const found = [];
      // Newest first: only what is not stored yet is kept, and the list is done where it runs into stored ones.
      await readList(LISTS[name](profile.sec_uid), async videos => {
        found.push(...await unknownOf(stop, videos, keyOf));
        return stop.done;
      });
      return found;
    } catch (error) {
      console.warn(`${name}: ${error.message}; reading what the page loads instead.`);
      return unknownOf(newestFirst(), await allBatches(fallbackPath), keyOf);
    }
  }

  const works = (await listVideos("works", "/aweme/v1/web/aweme/post/", video => workKey(video, profile.uid)))
    .filter(video => String(video.author?.uid) === String(profile.uid));
  console.log(`New own works: ${works.length}.`);
  for (const video of works.reverse()) {
    try {
      const object = await saveVideo(video, "douyin.aweme.post");
      await ingest(createDocument(profile.uid, video));
      counts.works += 1;
    } catch (error) {
      counts.failed += 1;
      console.warn(`Skipped work ${video.aweme_id}: ${error.message}`);
    }
  }


  // ---- likes and favorites ---------------------------------------------------

  async function saveReactions(videos, path, label, activityOf) {
    for (const video of [...videos].reverse()) {
      try {
        const object = await saveVideo(video, `douyin.${path.split("/").filter(Boolean).pop()}`);
        await ingest(activityOf(object));
        counts[label] += 1;
      } catch (error) {
        counts.failed += 1;
        console.warn(`Skipped ${label} ${video.aweme_id}: ${error.message}`);
      }
    }
  }

  // Read a page at a time, skipping activities already stored in atrace; fall back to page scrolling.
  async function importReactions(tab, path, label, activityOf) {
    let pages = 0;
    const key = video => reactionKey(video, activityOf);
    const stop = newestFirst();
    const found = [];
    try {
      await readList(LISTS[label](profile.sec_uid), async videos => {
        pages += 1;
        // Newest first: only what is not stored yet is kept, and the list is done where it runs into stored ones.
        const fresh = await unknownOf(stop, videos, key);
        console.log(`${label}: page ${pages}, ${fresh.length} new video(s) of ${videos.length}.`);
        // A full crawl stores each page as it is read; an ordinary run keeps the pages and stores them
        // together, oldest first, so the order of observation is the order the user acted in.
        if (FULL) await saveReactions(fresh, path, label, activityOf);
        else found.push(...fresh);
        return stop.done;
      });
      await saveReactions(found, path, label, activityOf);
    } catch (error) {
      if (pages > 0) throw error; // successfully stored pages will be skipped on the next scan
      console.warn(`${label}: ${error.message}; reading what the page loads instead.`);
      if (FULL) {
        // A page that has scrolled through thousands of videos hangs; start from a fresh load.
        await page.goto("https://www.douyin.com/user/self", { timeout: 60_000 });
        await pause(TAB_WAIT_MS);
      }
      await openTab(tab);
      const videos = await unknownOf(newestFirst(), await allBatches(path), key);
      console.log(`${label}: ${videos.length} new video(s).`);
      await saveReactions(videos, path, label, activityOf);
    }
  }

  await importReactions("喜欢", "/aweme/v1/web/aweme/favorite/", "likes", object => likeDocument(profile.uid, object));
  await ingest({ type: "Collection", id: FAVORITES, name: "抖音收藏", attributedTo: meRef });
  await importReactions("收藏", "/aweme/v1/web/aweme/listcollection/", "favorites", object => addDocument(profile.uid, object));
  await ingest({ type: "Collection", id: RECOMMENDED, name: "抖音推荐", attributedTo: meRef });
  await importReactions("推荐", "/aweme/v1/web/familiar/recommend/feed/", "recommends", object => addDocument(profile.uid, object, RECOMMENDED));

  console.log(JSON.stringify({ platform: "douyin", account: profile.nickname, uid: String(profile.uid), ...counts }));
}

await task.finish({ keep: [] });
} // collector body

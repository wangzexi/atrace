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
// Run with: atrace collect weibo
// The signed-in Ego Lite profile supplies the logged-in Weibo session. No Weibo
// token is read or stored; the collector only fetches the signed-in user's
// own timeline, first-page comments, likes and favorites, and writes
// ActivityStreams documents to a local atrace HTTP server: a Note per post, a
// Create for the user's own posts, an Announce for reposts, a Note with
// inReplyTo per comment, a Like per like and an Add per favorite. Every image and video
// is downloaded and registered as a local copy. Live Photos retain the still
// and their signed motion clip, paired by the Video attachment's preview.
//
// The file has two halves. Above the marker line are the constants and the
// functions that turn a Weibo response into documents; they touch no browser
// or network, and the tests load only that half (collectors/AGENTS.md). Below
// it are the browser, the network and the order things are stored in.

const PROFILE_ID = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.profileId : "Default";
const TASK_SPACE = "atrace weibo collector";
const OBSERVED_BY = "weibo-ego";
// The signed-in account's id once it is known. Until then it is the uid from config.json: the owner's own, which the
// collector checks against the signed-in user (Weibo answers with that user in x-log-uid, whoever is asked about).
let WEIBO_UID = typeof ATRACE_CONFIG === "object" ? String(ATRACE_CONFIG.uid || "") : "";
let WEIBO_NAME = "";
const ATRACE_API = typeof ATRACE_SERVER === "string" ? ATRACE_SERVER : "http://127.0.0.1:8183";
// A full crawl (atrace collect <platform> --full puts `ATRACE_FULL=1` in the environment) reads every page. An ordinary run reads from the newest back until it meets what is stored
// (see KNOWN_RUN); the page limits only bound a first import.
const FULL = typeof ATRACE_FULL !== "undefined" && ATRACE_FULL;
// `ATRACE_TARGET=weibo:post:123` in the environment reads that one post and its comments and nothing else:
// it is what the device does when the owner opens the post. Without it the whole collection runs.
const TARGET = typeof ATRACE_TARGET === "string" ? ATRACE_TARGET : "";
const MAX_PAGES = FULL ? 1_000_000 : 30;
const PAGE_DELAY_MIN_MS = 30_000;
const PAGE_DELAY_MAX_MS = 40_000;
const COMMENT_PAGES = FULL ? 20 : 1; // pages of comments under a post (20 each), newest first
const REACTION_PAGES = FULL ? 1_000_000 : 3; // pages of likes and of favorites to read, newest first
const FAVORITES = "weibo:collection:favorites";

function idOf(status) {
  return String(status?.idstr || status?.id || "");
}

// The post number of a target such as "weibo:post:123", or "" when it is not one.
function postNumberOf(target) {
  return (/^weibo:post:(\d{1,25})$/.exec(target) || [])[1] || "";
}

function uidOf(status) {
  return String(status?.user?.idstr || status?.user?.id || "");
}

function userRef(status) {
  const uid = uidOf(status);
  return uid ? `weibo:user:${uid}` : "";
}

function statusUrl(status, fallbackUID = WEIBO_UID) {
  const uid = uidOf(status) || fallbackUID;
  const id = status?.mblogid || idOf(status);
  return id ? `https://weibo.com/${uid}/${id}` : `https://weibo.com/u/${uid}`;
}

function stripHtml(value = "") {
  return String(value)
    .replace(/<br\s*\/?\s*>/gi, "\n")
    .replace(/<\/p\s*>/gi, "\n\n")
    .replace(/<[^>]*>/g, "")
    .replace(/&nbsp;/g, " ")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .trim();
}

function textOf(status) {
  return status.text_raw || stripHtml(status.text || "");
}

function publishedOf(status) {
  if (!status.created_at) return undefined;
  const timestamp = Date.parse(status.created_at);
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : undefined;
}

function imageURLsOf(status) {
  const attachments = [];
  for (const picture of Object.values(status.pic_infos || {})) {
    const url = picture?.largest?.url || picture?.large?.url || picture?.mw2000?.url || picture?.bmiddle?.url;
    if (typeof url === "string" && url.startsWith("https://")) {
      attachments.push({ url, width: picture?.largest?.width || picture?.large?.width || undefined, height: picture?.largest?.height || picture?.large?.height || undefined });
    }
  }
  return attachments;
}

// Signed playback URLs expire. Keep the stable MP4 address in the document and
// use the current signed address only while downloading its archive copy.
function videoOf(status) {
  const media = status?.page_info?.media_info;
  if (!media) return null;
  const playback = (media.playback_list || [])
    .map(item => item.play_info)
    .filter(item => item?.mime === "video/mp4" && item.url && (!item.video_codecs || item.video_codecs.startsWith("avc1")))
    .sort((a, b) => (b.height || 0) - (a.height || 0));
  const source = playback[0]?.url || media.mp4_720p_mp4 || media.mp4_hd_url || media.stream_url_hd || media.stream_url || media.mp4_sd_url;
  if (!source) return null;
  let url;
  try { url = new URL(source); } catch { return null; }
  if (!["http:", "https:"].includes(url.protocol) || !url.hostname.endsWith(".weibocdn.com") || !url.pathname.endsWith(".mp4")) return null;
  url.protocol = "https:";
  const download = url.href;
  url.search = "";
  url.hash = "";
  return { url: url.href, source: download };
}

// Live Photos carry their own signed motion URL alongside the still image.
// The decoded MOV URL identifies the copy; only the signed wrapper can download it.
function livePhotosOf(status) {
  const photos = [];
  for (const picture of Object.values(status?.pic_infos || {})) {
    if (picture?.type !== "livephoto" || !picture.video) continue;
    const image = imageURLsOf({ pic_infos: { picture } })[0];
    if (!image) continue;
    let source, url;
    try {
      source = new URL(picture.video);
      url = new URL(source.searchParams.get("livephoto"));
    } catch { continue; }
    if (source.protocol !== "https:" || source.hostname !== "video.weibo.com" ||
        url.protocol !== "https:" || !url.hostname.endsWith(".sinaimg.cn") || !/\.(mov|mp4)$/i.test(url.pathname)) continue;
    url.search = "";
    url.hash = "";
    photos.push({ url: url.href, source: source.href, image: image.url });
  }
  return photos;
}

// Weibo serves motion clips as octet-stream. Verify the ISO media header before
// assigning a playable MIME type; an HTML error must never become an archive copy.
function motionMime(bytes) {
  const data = new Uint8Array(bytes);
  if (data.length < 12) return "";
  const word = offset => String.fromCharCode(...data.slice(offset, offset + 4));
  if (word(4) !== "ftyp") return "";
  return word(8) === "qt  " ? "video/quicktime" : "video/mp4";
}

function attachmentsOf(status) {
  const attachments = [];
  for (const image of imageURLsOf(status)) {
    const url = new URL(image.url);
    if (!url.hostname.endsWith(".sinaimg.cn")) throw new Error(`Unexpected Weibo image host: ${url.hostname}`);
    attachments.push({ type: "Image", url: image.url, ...(image.width ? { width: image.width } : {}), ...(image.height ? { height: image.height } : {}) });
  }
  for (const photo of livePhotosOf(status)) {
    attachments.push({ type: "Video", url: photo.url, preview: { type: "Image", url: photo.image } });
  }
  const video = videoOf(status);
  if (video) attachments.push({ type: "Video", url: video.url });
  return attachments;
}

// Weibo serves an image in the format its address names. Another one (a GIF for a .jpg) is what its image host
// gives in place of a picture it will not hand out: it is not kept as the picture. "" when the type fits, or
// is not known.
const IMAGE_TYPES = { jpg: "image/jpeg", jpeg: "image/jpeg", png: "image/png", gif: "image/gif", webp: "image/webp" };
function imageTypeProblem(url, mime) {
  const extension = new URL(url).pathname.split(".").pop().toLowerCase();
  const expected = IMAGE_TYPES[extension];
  return expected && mime && mime !== expected ? `${mime} for a .${extension} address` : "";
}

function longTextOf(payload) {
  return stripHtml(payload.data.longTextContent);
}

function noteDocument(status, text, attachments, quote) {
  return {
    type: "Note",
    id: `weibo:post:${idOf(status)}`,
    ...(userRef(status) ? { attributedTo: userRef(status) } : {}),
    ...(quote ? { quote } : {}),
    content: text,
    url: statusUrl(status),
    ...(publishedOf(status) ? { published: publishedOf(status) } : {}),
    ...(attachments.length ? { attachment: attachments } : {}),
  };
}

// The author of a post or comment, or null when the response names no author.
function personDocument(entity) {
  const uid = uidOf(entity);
  if (!uid) return null;
  const user = entity.user || {};
  return {
    type: "Person",
    id: `weibo:user:${uid}`,
    url: `https://weibo.com/u/${uid}`,
    ...(user.screen_name || user.name ? { name: user.screen_name || user.name } : {}),
  };
}

// Own posts become a Note and a Create; reposts become an Announce of the
// original, which is kept as a Note of its own author. Entries come in storage
// order; role says which raw response each one is stored with.
// Whether a repost says anything of its own: Weibo's stand-in text for a repost without words does not.
function hasOwnWords(text) {
  return !/^\s*(转发微博|轉發微博|repost)?\s*$/i.test(String(text || ""));
}

function postDocuments(status, text) {
  const id = idOf(status);
  if (!id) throw new Error("Weibo post is missing its ID");
  const attachments = attachmentsOf(status);
  const entries = [{ role: "author", document: personDocument(status) }];
  const source = status.retweeted_status;
  if (source && idOf(source)) {
    // The cited post's own media and comments are intentionally not fetched.
    entries.push(
      { role: "cited-author", document: personDocument(source) },
      { role: "cited", document: noteDocument(source, textOf(source), attachmentsOf(source)) },
    );
    if (hasOwnWords(text)) {
      // A repost with words of the user's own is a post of their own that quotes the original.
      entries.push(
        { role: "post", document: noteDocument(status, text, attachments, `weibo:post:${idOf(source)}`) },
        {
          role: "create",
          document: {
            type: "Create",
            actor: `weibo:user:${WEIBO_UID}`,
            object: `weibo:post:${id}`,
            ...(publishedOf(status) ? { published: publishedOf(status) } : {}),
          },
        },
      );
    } else {
      // A plain repost: Weibo fills in its own words ("转发微博") when there are none.
      entries.push({
        role: "post",
        document: {
          type: "Announce",
          id: `weibo:post:${id}`,
          actor: `weibo:user:${WEIBO_UID}`,
          object: `weibo:post:${idOf(source)}`,
          url: statusUrl(status),
          ...(publishedOf(status) ? { published: publishedOf(status) } : {}),
        },
      });
    }
  } else {
    entries.push(
      { role: "post", document: noteDocument(status, text, attachments) },
      // Create is idempotent: atrace stores it only while the user has not
      // already published this post.
      {
        role: "create",
        document: {
          type: "Create",
          actor: `weibo:user:${WEIBO_UID}`,
          object: `weibo:post:${id}`,
          ...(publishedOf(status) ? { published: publishedOf(status) } : {}),
        },
      },
    );
  }
  return entries.filter(entry => entry.document);
}

// The comment a reply in a thread answers: the one it names (reply_comment), else the comment the thread is under.
function replyParent(reply, rootID) {
  return String(reply.reply_comment?.idstr || reply.rootidstr || rootID || "");
}

// Weibo gives no address for the pictures of a comment, only a "查看图片" link (a short link in the text and a
// url_struct entry for it): the link becomes a mark, "[图片]", so the comment shows that it had a picture. A
// comment that says it has pictures (pic_num) and no such link gets the mark at its end.
function withPictureMarks(comment, words) {
  let text = words;
  let marked = false;
  for (const link of comment.url_struct || []) {
    if (link?.url_title === "查看图片" && link.short_url && text.includes(link.short_url)) {
      text = text.split(link.short_url).join("[图片]");
      marked = true;
    }
  }
  if (!marked && Number(comment.pic_num) > 0) text = `${text} [图片]`.trim();
  return text;
}

// A comment under one of the user's posts: the commenter and a Note replying to the post, or, when
// parentID is given, to that comment (a reply in a thread; Weibo begins its text with "回复@name:",
// which the reply to the comment already says). Empty when the comment has no ID.
function commentDocuments(comment, status, parentID = "") {
  const commentID = String(comment.idstr || comment.id || "");
  if (!commentID) return [];
  const words = withPictureMarks(comment, comment.text_raw || stripHtml(comment.text || ""));
  return [
    { role: "author", document: personDocument(comment) },
    {
      role: "post",
      document: {
        type: "Note",
        id: `weibo:comment:${commentID}`,
        ...(userRef(comment) ? { attributedTo: userRef(comment) } : {}),
        inReplyTo: parentID ? `weibo:comment:${parentID}` : `weibo:post:${idOf(status)}`,
        content: parentID ? words.replace(/^回复@\S+?[:：]\s*/, "") : words,
        url: statusUrl(status),
        ...(publishedOf(comment) ? { published: publishedOf(comment) } : {}),
      },
    },
  ].filter(entry => entry.document);
}

function likeDocument(object) {
  return { type: "Like", actor: `weibo:user:${WEIBO_UID}`, object };
}

function addDocument(object) {
  return { type: "Add", actor: `weibo:user:${WEIBO_UID}`, object, target: FAVORITES };
}

// An item from the likes or favorites list: its author, the post as a Note
// (the list gives shortened text) and the activity on it.
function reactionDocuments(status, activityOf) {
  const id = idOf(status);
  if (!id) throw new Error("post has no id");
  return [
    { role: "author", document: personDocument(status) },
    { role: "post", document: noteDocument(status, textOf(status), attachmentsOf(status)) },
    { role: "activity", document: activityOf(`weibo:post:${id}`) },
  ].filter(entry => entry.document);
}


// One profile as Weibo's own page asks for it. An answer that is a page and not data means the session is not
// signed in, or that Weibo has moved the request.
async function weiboProfile(page, uid) {
  const response = await page.fetch(`/ajax/profile/info?uid=${uid}`, { timeout: 30_000 });
  if (!response.ok) throw new Error("Could not identify the Weibo account: HTTP " + response.status);
  let body;
  try {
    body = JSON.parse(response.body);
  } catch {
    throw new Error("The Ego profile is not logged in to Weibo, or Weibo changed /ajax/profile/info: it answered with a page, not data.");
  }
  return { loggedIn: String(response.headers["x-log-uid"] || ""), user: body.data?.user };
}

// Who is signed in: the x-log-uid of an /ajax answer, and that user's own profile for the name.
async function loggedInWeiboAccount(page) {
  const asked = await weiboProfile(page, WEIBO_UID);
  const uid = asked.loggedIn;
  if (!/^\d+$/.test(uid) || uid === "0") throw new Error("The Ego profile is not logged in to Weibo.");
  const user = uid === WEIBO_UID ? asked.user : (await weiboProfile(page, uid)).user;
  return { uid, name: user?.screen_name || "" };
}

async function checkWeiboAccount(page, task) {
  const { uid, name } = await loggedInWeiboAccount(page);
  await checkCollectorAccount(`weibo:user:${uid}`, name, task);
  WEIBO_UID = uid;
  WEIBO_NAME = name;
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

// The activity an own post leaves in the archive (whether it has words of its own decides which it is).
function statusKey(status) {
  try {
    return activityKey(postDocuments(status, status.text_raw || status.text || ""));
  } catch {
    return null;
  }
}

function reactionKey(status, activityOf) {
  try {
    return activityKey(reactionDocuments(status, activityOf));
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

const downloaded = new Set();

// Download Weibo media once and register the copy under its stable URL.
async function ensureLocalMedia(url, source = url, type = "Image", livePhoto = false) {
  if (downloaded.has(url)) return;
  const known = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(url)}`);
  // A copy of the wrong type (the placeholder an earlier run kept) is downloaded again.
  if (!known.ok || (type === "Image" && imageTypeProblem(url, (await known.json()).mime || ""))) {
    await pause(randomDelay(2_000, 4_000));
    const response = await fetch(source, {
      headers: { Referer: "https://weibo.com/" },
      signal: AbortSignal.timeout(type === "Video" ? 120_000 : 30_000),
    });
    if (!response.ok) throw new Error(`media download failed: HTTP ${response.status} ${url}`);
    let mime = response.headers.get("content-type")?.split(";")[0] || "";
    const bytes = await response.arrayBuffer();
    if (livePhoto) {
      mime = motionMime(bytes);
      if (!mime) throw new Error(`not a Live Photo motion clip: ${url}`);
    }
    if (!mime.startsWith(type === "Video" ? "video/" : "image/")) throw new Error(`unexpected ${type} MIME ${mime}: ${url}`);
    const problem = type === "Image" ? imageTypeProblem(url, mime) : "";
    if (problem) throw new Error(`not the picture (${problem}): ${url}`);
    await apiUploadFile(bytes, mime, url);
  }
  downloaded.add(url);
}

// Store the entries in order; rawOf gives the raw response each role keeps.
async function storeEntries(entries, rawOf) {
  for (const entry of entries) {
    // A picture that cannot be had (Weibo gives a placeholder for one it has removed) does not stop the post being stored.
    for (const attachment of entry.document.attachment || []) {
      const raw = rawOf[entry.role]?.response;
      const photo = attachment.type === "Video" ? livePhotosOf(raw).find(item => item.url === attachment.url) : null;
      const video = attachment.type === "Video" ? videoOf(raw) : null;
      await ensureLocalMedia(attachment.url, photo?.source || video?.source || attachment.url, attachment.type, !!photo).catch(error => console.warn(`No local copy of ${attachment.url}: ${error.message}`));
    }
  }
  let post;
  for (const entry of entries) {
    const result = await ingest(entry.document, rawOf[entry.role]);
    if (entry.role === "post") post = result;
  }
  return post;
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

async function requestJson(url, init = {}) {
  const response = await atraceFetch(url, init);
  const body = await response.text();
  if (!response.ok) throw new Error(`${init.method || "GET"} ${url}: HTTP ${response.status}: ${body.slice(0, 400)}`);
  try {
    return JSON.parse(body);
  } catch {
    throw new Error(`${init.method || "GET"} ${url}: invalid JSON: ${body.slice(0, 200)}`);
  }
}

async function apiPost(path, value, contentType = "application/json") {
  const response = await atraceFetch(`${ATRACE_API}${path}`, {
    method: "POST",
    headers: { "Content-Type": contentType },
    body: typeof value === "string" ? value : JSON.stringify(value),
  });
  const body = await response.text();
  if (!response.ok) throw new Error(`atrace ${path}: HTTP ${response.status}: ${body.slice(0, 500)}`);
  return JSON.parse(body);
}

async function apiUploadFile(bytes, contentType, sourceURL) {
  const response = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(sourceURL)}`, {
    method: "PUT",
    headers: { "Content-Type": contentType },
    body: bytes,
  });
  const body = await response.text();
  if (!response.ok) throw new Error(`atrace /collect/media: HTTP ${response.status}: ${body.slice(0, 500)}`);
  return JSON.parse(body);
}

async function fullTextIfNeeded(page, status, collectedLongText) {
  if (!status.isLongText && !status.is_long_text) return textOf(status);
  const id = idOf(status);
  if (!id) throw new Error("Long Weibo post has no id");
  await pause(randomDelay(15_000, 20_000));
  const response = await page.fetch(`/ajax/statuses/longtext?id=${encodeURIComponent(id)}`, { timeout: 30_000 });
  if (!response.ok) throw new Error(`longtext ${id}: HTTP ${response.status}`);
  const payload = JSON.parse(response.body);
  if (!payload.data?.longTextContent) {
    // Weibo has no long text for this post (it was cut down or is limited): keep the text the list gives.
    console.warn(`longtext ${id}: content missing; keeping the shortened text.`);
    return textOf(status);
  }
  collectedLongText[id] = payload;
  return longTextOf(payload);
}

async function importPost(status, raw, options = {}) {
  const source = status.retweeted_status;
  return storeEntries(postDocuments(status, options.text ?? textOf(status)), {
    author: { source: "weibo.ajax.statuses.mymblog.user", response: status.user || {} },
    "cited-author": { source: "weibo.ajax.statuses.mymblog.retweeted_status.user", response: source?.user || {} },
    cited: { source: "weibo.ajax.statuses.mymblog.retweeted_status", observed_at: new Date().toISOString(), response: source },
    post: raw,
  });
}

// One request for comments: a post's (level 0) or the replies under one comment (level 1, id is the
// comment). Weibo answers with data and, while there is more, max_id.
async function commentRequest(page, id, level, maxId) {
  const query = new URLSearchParams({
    is_reload: "1", id, is_show_bulletin: "2", is_mix: "0", count: "20", fetch_level: String(level), locale: "zh-CN", uid: WEIBO_UID,
    ...(maxId ? { max_id: String(maxId) } : {}),
  });
  const response = await page.fetch(`/ajax/statuses/buildComments?${query}`, { timeout: 30_000 });
  if (!response.ok) throw new Error(`comments ${id}: HTTP ${response.status}`);
  const payload = JSON.parse(response.body);
  if (payload.ok === 0 || payload.ok === false) throw new Error(`comments ${id}: ${payload.msg || "API error"}`);
  return { comments: payload.data || [], maxId: Number(payload.max_id || 0) };
}

async function storeComment(status, comment, parentID) {
  const entries = commentDocuments(comment, status, parentID);
  if (!entries.length) return 0;
  const raw = {
    source: "weibo.ajax.statuses.buildComments",
    post: idOf(status),
    observed_at: new Date().toISOString(),
    response: comment,
  };
  await storeEntries(entries, { author: raw, post: raw });
  return 1;
}

// The comments under one of the user's posts, COMMENT_PAGES pages of them, and the replies under each:
// the few the page shows with the comment, and the rest when there are more. Every comment is a Note replying to the
// post or to the comment it answers; the commenter is a Person.
async function importComments(page, status) {
  const id = idOf(status);
  if (!(status.comments_count > 0)) return 0;
  let count = 0;
  let maxId = 0;
  for (let pageNumber = 0; pageNumber < COMMENT_PAGES; pageNumber += 1) {
    await pause(randomDelay(15_000, 20_000));
    const top = await commentRequest(page, id, 0, maxId);
    for (const comment of top.comments) {
      const commentID = String(comment.idstr || comment.id || "");
      count += await storeComment(status, comment, "");
      let replies = comment.comments || [];
      if (commentID && Number(comment.total_number || 0) > replies.length) {
        replies = [];
        let replyMaxId = 0;
        for (let more = 0; more < 10; more += 1) {
          await pause(randomDelay(8_000, 12_000));
          const page1 = await commentRequest(page, commentID, 1, replyMaxId);
          replies.push(...page1.comments);
          replyMaxId = page1.maxId;
          if (!replyMaxId) break;
        }
      }
      for (const reply of replies) count += await storeComment(status, reply, replyParent(reply, commentID));
    }
    maxId = top.maxId;
    if (!maxId) break;
  }
  return count;
}

// The user's likes and favorites. Weibo does not say when an item was liked or
// favorited, so those activities carry no published time; atrace falls back to
// when they were first observed. The lists run newest first, so every page is
// read before anything is stored and items are stored oldest first: that makes
// the order of observation match the order the user acted in. The author's post
// is stored as a Note, long posts keep the shortened text the list gives, and
// only the first REACTION_PAGES pages are read, so later unlikes and
// unfavorites are not seen. A full crawl stores each page as it is read and skips activities already in atrace.
async function importReactions(page, path, label, activityOf) {
  let stored = 0;
  let failed = 0;
  // Items of pages already read, oldest first.
  const storeItems = async items => {
    for (const { status, page: pageNumber } of items) {
      const id = idOf(status);
      try {
        const raw = { source: `weibo.${path.slice(1).replaceAll("/", ".")}`, page: pageNumber, observed_at: new Date().toISOString(), response: status };
        await storeEntries(reactionDocuments(status, activityOf), { author: raw, post: raw });
        stored += 1;
      } catch (error) {
        failed += 1;
        console.warn(`Skipped ${label} ${id || "(no id)"}: ${error.message}`);
      }
    }
  };
  const items = [];
  const stop = newestFirst();
  for (let pageNumber = 1; pageNumber <= REACTION_PAGES; pageNumber += 1) {
    const response = await page.fetch(`${path}?uid=${WEIBO_UID}&page=${pageNumber}`, { timeout: 30_000 });
    if (!response.ok) throw new Error(`${label} page ${pageNumber}: HTTP ${response.status}`);
    const payload = JSON.parse(response.body);
    if (payload.ok === 0 || payload.ok === false) throw new Error(`${label} page ${pageNumber}: ${payload.msg || "API error"}`);
    const statuses = Array.isArray(payload.data) ? payload.data : payload.data?.list || [];
    if (!statuses.length) {
      break;
    }
    console.log(`${label} page ${pageNumber}: ${statuses.length} item(s).`);
    const known = await knownOf(statuses.map(status => reactionKey(status, activityOf)));
    const fresh = takeUnknown(stop, statuses, known, !FULL);
    if (FULL) await storeItems(fresh.map(status => ({ status, page: pageNumber })).reverse());
    else for (const status of fresh) items.push({ status, page: pageNumber });
    if (stop.done) {
      console.log(`${label} page ${pageNumber} runs into items that are already stored; stopped.`);
      break;
    }
    if (pageNumber < REACTION_PAGES) await pause(randomDelay(PAGE_DELAY_MIN_MS, PAGE_DELAY_MAX_MS));
  }
  await storeItems(items.reverse());
  return { stored, failed };
}

// ego's Node drops all console output when the first asynchronous operation is
// a fetch; any timer first keeps it.
await pause(0);
if (!/^\d+$/.test(WEIBO_UID)) throw new Error('config.json: set "uid" to your own Weibo uid');
const health = await requestJson(`${ATRACE_API}/healthz`);
if (!health.ok) throw new Error("atrace health check failed");

const spaces = await listTaskSpaces();
// Read one post again, and its first comments: the post as Weibo shows it now, stored as a timeline's post is stored.
async function readPost(page, target) {
  const id = postNumberOf(target);
  if (!id) throw new Error(`not a post of Weibo: ${target}`);
  const response = await page.fetch(`/ajax/statuses/show?id=${id}`, { timeout: 30_000 });
  if (!response.ok) throw new Error(`post ${id}: HTTP ${response.status}`);
  const payload = JSON.parse(response.body);
  if (payload.ok === 0 || payload.ok === false) throw new Error(`post ${id}: ${payload.msg || "API error"}`);
  const status = payload.data?.mid || payload.data?.idstr ? payload.data : payload;
  if (idOf(status) !== id) throw new Error(`the answer for ${target} does not hold the post`);
  const longtext = {};
  const text = await fullTextIfNeeded(page, status, longtext);
  const raw = {
    source: "weibo.ajax.statuses.show",
    observed_at: new Date().toISOString(),
    response: status,
    ...(longtext[id] ? { longtext: longtext[id] } : {}),
  };
  const result = await importPost(status, raw, { text });
  const comments = await importComments(page, status);
  console.log(JSON.stringify({ platform: "weibo", account: WEIBO_NAME, uid: WEIBO_UID, post: target, created: result.created, comments }));
}

const existingSpace = spaces.find(space => space.name === TASK_SPACE);
if (existingSpace && existingSpace.ownership !== "agent") {
  throw new Error(`Task space ${TASK_SPACE} is user-owned; refusing to claim it automatically.`);
}
const task = existingSpace
  ? await taskSpace(existingSpace.id)
  : await taskSpace(TASK_SPACE, { profileId: PROFILE_ID });
const page = task.page("p1");
if (typeof checkCollectorAccount === "function") {
  await page.goto("https://weibo.com/", { timeout: 60_000 });
  await checkWeiboAccount(page, task);
}
if (TARGET) {
  await readPost(page, TARGET);
} else {
  const profileURL = `https://weibo.com/u/${WEIBO_UID}`;
  if (!(await page.url()).startsWith(profileURL)) {
    await page.goto(profileURL, { timeout: 60_000 });
  }

  const imported = [];
  const seen = new Set();
  let cursor = "";
  let reachedKnown = false;
  const stop = newestFirst();

  for (let pageNumber = 1; pageNumber <= MAX_PAGES; pageNumber += 1) {
    const query = new URLSearchParams({ uid: WEIBO_UID, page: String(pageNumber), feature: "0" });
    if (cursor) query.set("since_id", cursor);
    const response = await page.fetch(`/ajax/statuses/mymblog?${query}`, { timeout: 30_000 });
    if (!response.ok) throw new Error(`mymblog page ${pageNumber}: HTTP ${response.status}`);
    const loginUID = response.headers["x-log-uid"] || "";
    if (loginUID && loginUID !== WEIBO_UID) throw new Error(`Ego profile mismatch: expected Weibo UID ${WEIBO_UID}, got ${loginUID}`);
    const payload = JSON.parse(response.body);
    if (payload.ok === false) throw new Error(`mymblog page ${pageNumber}: ${payload.msg || payload.message || "API error"}`);

    const statuses = payload.data?.list || [];
    if (!statuses.length) {
      console.log(`Page ${pageNumber} returned no posts; finished.`);
      break;
    }
    const novel = statuses.filter(status => !seen.has(idOf(status)));
    if (!novel.length) {
      console.log(`Page ${pageNumber} repeated prior post IDs; stopped to avoid a pagination loop.`);
      break;
    }
    for (const status of novel) seen.add(idOf(status));

    // The list is newest first: a few posts in a row that are already stored end it.
    const known = await knownOf(novel.map(statusKey));
    const fresh = takeUnknown(stop, novel, known, !FULL);

    if (fresh.length) {
      const longtext = {};
      // Resolve long posts from Weibo's signed-in page session before storing the
      // page snapshot, so the raw ref includes their full-text response too.
      const resolved = [];
      for (const status of fresh) {
        const uid = uidOf(status);
        if (uid && uid !== WEIBO_UID && !status.retweeted_status) {
          console.warn(`Skipped ${idOf(status)} because the timeline author UID is ${uid}.`);
          continue;
        }
        const text = await fullTextIfNeeded(page, status, longtext);
        resolved.push({ status, text });
      }
      if (resolved.length) {
        for (const item of resolved) {
          const id = idOf(item.status);
          const raw = {
            source: "weibo.ajax.statuses.mymblog.item",
            page: pageNumber,
            observed_at: new Date().toISOString(),
            response: item.status,
            ...(longtext[id] ? { longtext: longtext[id] } : {}),
          };
          const result = await importPost(item.status, raw, { text: item.text });
          const comments = await importComments(page, item.status);
          imported.push({ id: idOf(item.status), created: result.created, comments });
        }
        console.log(`Page ${pageNumber}: imported ${resolved.length} post(s).`);
      }
    }

    if (stop.done) {
      reachedKnown = true;
      console.log(`Page ${pageNumber} runs into posts that are already stored; stopped.`);
      break;
    }
    cursor = String(payload.data?.since_id || "");
    if (!cursor) {
      console.log(`Page ${pageNumber} returned no since_id cursor; finished.`);
      break;
    }
    if (pageNumber < MAX_PAGES) await pause(randomDelay(PAGE_DELAY_MIN_MS, PAGE_DELAY_MAX_MS));
  }

  await pause(randomDelay(PAGE_DELAY_MIN_MS, PAGE_DELAY_MAX_MS));
  const likes = await importReactions(page, "/ajax/statuses/likelist", "likes", likeDocument);
  await ingest({ type: "Collection", id: FAVORITES, name: "微博收藏", attributedTo: `weibo:user:${WEIBO_UID}` });
  await pause(randomDelay(PAGE_DELAY_MIN_MS, PAGE_DELAY_MAX_MS));
  const favorites = await importReactions(page, "/ajax/favorites/all_fav", "favorites", addDocument);

  console.log(JSON.stringify({
    platform: "weibo",
    account: WEIBO_NAME,
    uid: WEIBO_UID,
    reached_known: reachedKnown,
    posts_seen: seen.size,
    posts_submitted: imported.length,
    new_versions: imported.filter(item => item.created).length,
    comments_submitted: imported.reduce((sum, item) => sum + item.comments, 0),
    likes,
    favorites,
  }));
}

await task.finish({ keep: [] });
} // collector body

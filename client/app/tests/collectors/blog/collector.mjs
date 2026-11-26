import { readFile } from 'node:fs/promises'
// The example version this copy came from; a copy may be edited, so compare by hand.
const VERSION = 1
const config = JSON.parse(await readFile(new URL('config.json', import.meta.url), 'utf8'))
const ATRACE_CONFIG = config
const ATRACE_TOKEN = process.env.ATRACE_COLLECT_TOKEN
const ATRACE_SERVER = process.env.ATRACE_SERVER
const ATRACE_TARGET = process.env.ATRACE_TARGET
const ATRACE_FULL = process.env.ATRACE_FULL === '1'
if (!ATRACE_TOKEN) throw new Error('ATRACE_COLLECT_TOKEN is required')
function checkCollectorAccount(id, name) {
  console.log('ATRACE_ACCOUNT ' + id + (name ? ' ' + name : ''))
}

// ==== collector body ====
// Run with: atrace collect github
// Reads the published articles of the public GitHub repository named in config.json
// from its source archive: every top-level directory holds a README.md (the
// article, with created_at and updated_at in its front matter) and an assets/
// directory with its images. No login is needed, and the archive download is
// not subject to the API's hourly limit. Each article becomes an Article and a
// Create by the author; the Markdown source is the content. The directory's
// refs/ (source material) is not collected. Images are registered as local
// copies of their raw.githubusercontent.com URLs.
//
// The file has two halves. Above the marker line are the constants and the
// functions that turn the archive's files into documents; they touch no network,
// and the tests load only that half (collectors/AGENTS.md). Below it are the
// download, the uploads and the order things are stored in.

const ATRACE_API = typeof ATRACE_SERVER === "string" ? ATRACE_SERVER : "http://127.0.0.1:8183";
// These come from config.json.
const REPO = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.repository : "";
const BRANCH = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.branch : "";
// `ATRACE_TARGET=github:article:abc` in the environment stores that one article only (the archive is
// downloaded whole either way): it is what the device does when the owner opens it. Without it every article is.
const TARGET = typeof ATRACE_TARGET === "string" ? ATRACE_TARGET : "";
const AUTHOR = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.authorId : "";
const AUTHOR_NAME = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.authorName : "";
const AUTHOR_URL = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.authorUrl : "";
const OBSERVED_BY = "github";
const RAW = `https://raw.githubusercontent.com/${REPO}/${BRANCH}`;
const ARCHIVE = `https://codeload.github.com/${REPO}/tar.gz/refs/heads/${BRANCH}`;
const IMAGE_TYPES = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", svg: "image/svg+xml" };

function encodePath(path) {
  return path.split("/").map(encodeURIComponent).join("/");
}

// The front matter is a few `key: "value"` lines between two `---` lines.
function parseArticle(source) {
  const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/);
  const meta = {};
  if (match) {
    for (const line of match[1].split(/\r?\n/)) {
      const pair = line.match(/^([A-Za-z_][\w-]*):\s*"?(.*?)"?\s*$/);
      if (pair) meta[pair[1]] = pair[2];
    }
  }
  return { meta, body: (match ? match[2] : source).trim() };
}

// "2026-06-17 01:54:00 +0800" → ISO time.
function timeOf(value) {
  const match = String(value || "").match(/^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2}) ([+-]\d{2})(\d{2})$/);
  const parsed = match ? Date.parse(`${match[1]}T${match[2]}${match[3]}:${match[4]}`) : Date.parse(value || "");
  return Number.isFinite(parsed) ? new Date(parsed).toISOString() : undefined;
}

async function sha256Hex(text) {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join("");
}

// The files of an uncompressed tar archive as a map of path → bytes, with the
// archive's top-level directory removed. Long names come from pax headers or
// the ustar prefix field.
function parseTar(bytes) {
  const decoder = new TextDecoder();
  const field = (start, end) => decoder.decode(bytes.subarray(start, end)).replace(/\0.*$/s, "");
  const files = new Map();
  let offset = 0;
  let paxPath = "";
  while (offset + 512 <= bytes.length && bytes[offset] !== 0) {
    const size = parseInt(field(offset + 124, offset + 136).trim() || "0", 8);
    const type = String.fromCharCode(bytes[offset + 156]);
    const body = bytes.subarray(offset + 512, offset + 512 + size);
    if (type === "x") {
      const record = decoder.decode(body).match(/\d+ path=([^\n]*)\n/);
      paxPath = record ? record[1] : "";
    } else if (type === "0") {
      const prefix = field(offset + 345, offset + 500);
      const path = paxPath || (prefix ? `${prefix}/` : "") + field(offset, offset + 100);
      files.set(path.slice(path.indexOf("/") + 1), body);
      paxPath = "";
    }
    offset += 512 + Math.ceil(size / 512) * 512;
  }
  return files;
}

// The article directories: every top-level directory with a README.md.
function readmePaths(files) {
  return [...files.keys()].filter(path => /^[^/.][^/]*\/README\.md$/.test(path));
}

function authorDocument() {
  return { type: "Person", id: AUTHOR, name: AUTHOR_NAME, url: AUTHOR_URL };
}

// One article: an Article and a Create, and the images it uses from its own
// assets/ directory (the URL they are registered under, the type and the path in
// the archive). Formats outside IMAGE_TYPES keep their GitHub URL only and are not
// attached.
async function articleDocuments(directory, source) {
  const { meta, body } = parseArticle(source);
  const title = (body.match(/^#\s+(.+)$/m) || [])[1]?.trim() || directory;
  // The title is the document's own field: the text does not open with it again.
  const heading = `# ${title}`;
  const text = body.trimStart().startsWith(heading) ? body.trimStart().slice(heading.length).trim() : body;
  const images = [];
  const seen = new Set();
  for (const link of body.matchAll(/!\[[^\]]*\]\((assets\/[^)\s]+)\)|<img[^>]+src="(assets\/[^"]+)"/g)) {
    const relative = decodeURIComponent(link[1] || link[2]);
    if (seen.has(relative)) continue;
    seen.add(relative);
    const mime = IMAGE_TYPES[relative.split(".").pop().toLowerCase()];
    if (!mime) continue;
    images.push({ relative, mime, url: `${RAW}/${encodePath(`${directory}/${relative}`)}`, path: `${directory}/${relative}` });
  }
  const attachment = images.map(image => ({ type: "Image", url: image.url }));
  const id = `github:article:${meta.id || (await sha256Hex(directory)).slice(0, 12)}`;
  const published = timeOf(meta.created_at);
  const updated = timeOf(meta.updated_at);
  return {
    images,
    entries: [
      {
        role: "post",
        document: {
          type: "Article",
          id,
          name: title,
          attributedTo: AUTHOR,
          mediaType: "text/markdown",
          content: text,
          url: `https://github.com/${REPO}/tree/${BRANCH}/${encodePath(directory)}`,
          ...(published ? { published } : {}),
          ...(updated ? { updated } : {}),
          ...(attachment.length ? { attachment } : {}),
        },
      },
      { role: "create", document: { type: "Create", actor: AUTHOR, object: id, ...(published ? { published } : {}) } },
    ],
  };
}

// ==== side effects below: the download, the uploads and storage order; not loaded by tests ====

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

async function ingest(document, raw) {
  return requestJson(`${ATRACE_API}/collect/ingest`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ observed_at: new Date().toISOString(), observed_by: OBSERVED_BY, ...(raw ? { raw } : {}), document }),
  });
}

// The images found in the archive, by the URL they are registered under.
const archiveImages = new Map();

// Register the archive's bytes as the local copy of an image URL, unless it has one.
async function ensureLocalImage(url) {
  const known = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(url)}`);
  if (known.ok) return;
  const image = archiveImages.get(url);
  if (!image) throw new Error("not in the archive");
  const uploaded = await atraceFetch(`${ATRACE_API}/collect/media?url=${encodeURIComponent(url)}`, {
    method: "PUT",
    headers: { "Content-Type": image.mime },
    body: image.bytes,
  });
  if (!uploaded.ok) throw new Error(`atrace /collect/media: HTTP ${uploaded.status}: ${(await uploaded.text()).slice(0, 300)}`);
}

// The files of a gzip-compressed tar archive, by path.
async function readArchive(url) {
  const response = await fetch(url, { signal: AbortSignal.timeout(60_000) });
  if (!response.ok) throw new Error(`GET ${url}: HTTP ${response.status}`);
  return parseTar(new Uint8Array(await new Response(response.body.pipeThrough(new DecompressionStream("gzip"))).arrayBuffer()));
}

// ego's Node drops all console output when the first asynchronous operation is
// a fetch; any timer first keeps it.
await pause(0);
if (REPO === "OWNER/REPO" || AUTHOR.includes("YOUR_LOGIN")) throw new Error('config.json: set "repository", "authorId" and the rest to your own');
const health = await requestJson(`${ATRACE_API}/healthz`);
if (!health.ok) throw new Error("atrace health check failed");

const archive = await readArchive(ARCHIVE);

if (typeof checkCollectorAccount === "function") await checkCollectorAccount(AUTHOR, AUTHOR_NAME);
await ingest(authorDocument());

const counts = { articles: 0, newVersions: 0, images: 0, imageFailures: 0 };
for (const readme of readmePaths(archive)) {
  const directory = readme.slice(0, readme.indexOf("/"));
  const { images, entries } = await articleDocuments(directory, new TextDecoder().decode(archive.get(readme)));
  if (TARGET && !entries.some(entry => entry.document.id === TARGET)) continue;
  for (const image of images) {
    try {
      archiveImages.set(image.url, { mime: image.mime, bytes: archive.get(image.path) });
      await ensureLocalImage(image.url);
      counts.images += 1;
    } catch (error) {
      counts.imageFailures += 1;
      console.warn(`Image ${image.relative} of ${directory}: ${error.message}`);
    }
  }
  const raw = { source: "github.archive", repository: REPO, path: readme };
  let result;
  for (const entry of entries) {
    const stored = await ingest(entry.document, entry.role === "post" ? raw : undefined);
    if (entry.role === "post") result = stored;
  }
  counts.articles += 1;
  if (result.created) counts.newVersions += 1;
}

if (TARGET && !counts.articles) throw new Error(`${TARGET} is not an article of ${REPO}`);
console.log(JSON.stringify({ platform: "github", account: AUTHOR_NAME, repository: REPO, ...(TARGET ? { post: TARGET } : {}), ...counts }));

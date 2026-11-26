import { spawn } from 'node:child_process'
import { readFile } from 'node:fs/promises'
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
// Run with: atrace collect xiaohongshu --script=emoji
// Xiaohongshu writes its emoji into text as [name] ("[微笑R]"). This reads the list the page itself keeps (the
// emoji map in its state), keeps a local copy of every picture in atrace and prints one line of JSON,
// {"name": "sha256:…", …}: the name without brackets and the file it shows. The front end (client/web/App/Timeline/ActivityCard/Content/Emoji.tsx)
// puts the pictures in place of the tokens from client/web/App/Timeline/ActivityCard/Content/Xiaohongshu/emoji.json, which is that line. Run it again when
// Xiaohongshu adds emoji, and save the new line over the file.
//
// Self-contained like the other collectors; the pictures come from Xiaohongshu's CDN a few at a time.

const PROFILE_ID = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.profileId : "Default";
const TASK_SPACE = "atrace xhs emoji";
const ATRACE_API = typeof ATRACE_SERVER === "string" ? ATRACE_SERVER : "http://127.0.0.1:8183";

function pause(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function atraceFetch(url, init = {}) {
  const sent = { ...init, headers: { ...init.headers, Authorization: `Bearer ${ATRACE_TOKEN}` } };
  for (let attempt = 1; ; attempt += 1) {
    try {
      return await fetch(url, sent);
    } catch (error) {
      if (attempt >= 6) throw error;
      await pause(5_000);
    }
  }
}

// Every name and picture of the emoji map in the page state (names are like "[微笑R]").
const emojiExpression = `
  const unwrap = v => (v && typeof v === "object" && "_value" in v ? v._value : v);
  const state = window.__INITIAL_STATE__ || {};
  const map = unwrap(state.emojiData)?.emojiParseMap;
  const entries = Object.entries(unwrap(map) || {});
  return JSON.stringify(entries.filter(([name, url]) => typeof name === "string" && typeof url === "string"));
`;

// The file kept for url: the copy atrace has, else the picture downloaded and stored.
async function localCopy(url) {
  const query = encodeURIComponent(url);
  const known = await atraceFetch(`${ATRACE_API}/collect/media?url=${query}`);
  if (known.ok) return (await known.json()).ref;
  const source = new URL(url);
  if (source.protocol !== "https:" || !/(^|\.)(xiaohongshu|xhscdn)\.com$/.test(source.hostname)) throw new Error(`not a Xiaohongshu picture: ${url}`);
  const response = await fetch(url, { headers: { Referer: "https://www.xiaohongshu.com/" }, signal: AbortSignal.timeout(15_000) });
  const mime = response.headers.get("content-type")?.split(";")[0] || "";
  if (!response.ok || !mime.startsWith("image/")) throw new Error(`HTTP ${response.status} ${mime}: ${url}`);
  const stored = await atraceFetch(`${ATRACE_API}/collect/media?url=${query}`, {
    method: "PUT",
    headers: { "Content-Type": mime },
    body: await response.arrayBuffer(),
  });
  if (!stored.ok) throw new Error(`atrace /collect/media: HTTP ${stored.status}`);
  return (await stored.json()).ref;
}

await pause(0); // a first-async-op fetch makes ego drop console output
const spaces = await listTaskSpaces();
const existing = spaces.find(space => space.name === TASK_SPACE);
if (existing && existing.ownership !== "agent") throw new Error(`Task space ${TASK_SPACE} is user-owned; refusing to claim it automatically.`);
const task = existing ? await taskSpace(existing.id) : await taskSpace(TASK_SPACE, { profileId: PROFILE_ID });
const page = task.page("p1");
const map = {};
const failed = [];
const byUrl = new Map();
try {
  await page.goto("https://www.xiaohongshu.com/explore", { timeout: 60_000 });
  await pause(5_000);
  const entries = JSON.parse(await page.evaluate(`(() => { ${emojiExpression} })()`));
  if (entries.length < 100) throw new Error(`Xiaohongshu's page lists only ${entries.length} emoji`);
  for (const [token, url] of entries) {
    const name = /^\[([^\[\]]{1,20})\]$/.exec(token)?.[1];
    // The list also holds "objects…" stickers, pictures with no extension that are not written in text.
    if (!name || name.startsWith("objects")) continue;
    try {
      if (!byUrl.has(url)) {
        // A picture the CDN did not give is asked for once more after a pause, before it counts as failed.
        try {
          byUrl.set(url, await localCopy(url));
        } catch {
          await pause(3_000);
          byUrl.set(url, await localCopy(url));
        }
        await pause(300 + Math.random() * 500);
      }
      map[name] = byUrl.get(url);
    } catch (error) {
      failed.push(`${name}: ${error.message}`);
    }
  }
} finally {
  await task.finish({ keep: [] });
}
console.log(JSON.stringify({ failed: failed.length, sample: failed.slice(0, 40), total: Object.keys(map).length }));
console.log(JSON.stringify(map));
} // collector body

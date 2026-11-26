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
// Run with: atrace collect github
// The signed-in Ego Lite profile supplies the logged-in GitHub session. No token is read or stored and no API is
// called: the collector opens github.com/<you>?tab=stars like a browser and reads the cards the page shows.
//   stars   Add with the target github:star:<you>, which the page shows as a favorite (the page gives no time for
//           a star, so none is written)
// A starred repository is stored as an Article of its owner: owner/name is its name, the description its summary,
// and its owner gets the Create. What changes from visit to visit (stars, forks, update time) and what the document
// has no field for (language, topics) stay in the raw card; if they were in the document every run would write a
// new version of every repository.
//
// The file has two halves. Above the marker line are the constants and the functions that turn what the stars
// page shows into documents; they touch no browser or network, and the tests load only that half
// (collectors/AGENTS.md). Below it are the page, the requests and the order things are stored in.

const PROFILE_ID = typeof ATRACE_CONFIG === "object" ? ATRACE_CONFIG.profileId : "Default";
const TASK_SPACE = "atrace github collector";
const ATRACE_API = typeof ATRACE_SERVER === "string" ? ATRACE_SERVER : "http://127.0.0.1:8183";
const OBSERVED_BY = "github-ego";
// An ordinary run reads the stars from the newest back until it meets what is stored (see KNOWN_RUN), at most
// MAX_PAGES pages of 30. A full crawl (atrace collect <platform> --full puts `ATRACE_FULL=1` in the environment)
// reads every page, with longer gaps.
const FULL = typeof ATRACE_FULL !== "undefined" && ATRACE_FULL;
// `ATRACE_TARGET=github:repo:owner/name` in the environment reads that one repository again and nothing else:
// it is what the device does when the owner opens it. Without it the stars run.
const TARGET = typeof ATRACE_TARGET === "string" ? ATRACE_TARGET : "";
const MAX_PAGES = FULL ? Infinity : 10;
const PAGE_DELAY_MIN_MS = FULL ? 6_000 : 3_000;
const PAGE_DELAY_MAX_MS = FULL ? 12_000 : 6_000;

// github.com paths that look like owner/name but are not a repository.
const NOT_OWNERS = new Set(["topics", "orgs", "sponsors", "settings", "marketplace", "features", "search", "stars",
  "collections", "trending", "login", "logout", "notifications", "explore", "issues", "pulls", "codespaces", "users"]);

// The repository a github.com path names, or null.
function repoOfHref(href) {
  const found = /^\/([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)\/([A-Za-z0-9._-]+)$/.exec(String(href || "").split(/[?#]/)[0]);
  if (!found || NOT_OWNERS.has(found[1].toLowerCase())) return null;
  return { owner: found[1], name: found[2], fullName: `${found[1]}/${found[2]}` };
}

// A card from what the page shows for one starred repository; null when it is not a repository.
function cardOf(item) {
  const repo = repoOfHref(item?.href);
  if (!repo) return null;
  return { ...repo, description: String(item.description || "").replace(/\s+/g, " ").trim() };
}

// The repository a target such as "github:repo:owner/name" names; "" when it is not one.
function targetRepoOf(target) {
  const found = /^github:repo:([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\/[A-Za-z0-9._-]+)$/.exec(target);
  return found ? found[1] : "";
}

function repoIdOf(card) {
  return `github:repo:${card.fullName}`;
}

function personDocument(login) {
  return { type: "Person", id: `github:user:${login}`, name: login, url: `https://github.com/${login}` };
}

// A repository becomes an Article of its owner.
function repoDocuments(card) {
  const owner = personDocument(card.owner);
  return [
    { role: "author", document: owner },
    {
      role: "post",
      document: {
        type: "Article",
        id: repoIdOf(card),
        attributedTo: owner.id,
        name: card.fullName,
        ...(card.description ? { summary: card.description } : {}),
        url: `https://github.com/${card.fullName}`,
      },
      downloads: [],
    },
  ];
}

// The Add for a starred repository, to the stars of the account: github:star:<login> names that list, and nothing
// else is stored for it. GitHub gives no time for a star.
function addDocument(meLogin, card) {
  return { type: "Add", actor: `github:user:${meLogin}`, object: repoIdOf(card), target: `github:star:${meLogin}` };
}

// A list read newest first is done where it runs into what the archive already has: KNOWN_RUN items
// in a row that are stored. A run that finds nothing stored (the first one) reads on up to the limits above.
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

// Whoever wrote a post made it: the Create of its author goes in with the post, for the posts of others as well as
// one's own, and the core does not guess it.
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

// ---- GitHub ----------------------------------------------------------------

// ego's Node drops all console output when the first asynchronous operation is
// a fetch; any timer first keeps it.
await pause(0);
const health = await requestJson(`${ATRACE_API}/healthz`);
if (!health.ok) throw new Error("atrace health check failed");

const spaces = await listTaskSpaces();
const existingSpace = spaces.find(space => space.name === TASK_SPACE);
if (existingSpace && (existingSpace.ownership !== "agent" || existingSpace.profileId !== PROFILE_ID)) {
  throw new Error(`Task space ${TASK_SPACE} is user-owned or belongs to another profile; refusing to claim it automatically.`);
}
const task = existingSpace
  ? await taskSpace(existingSpace.id)
  : await taskSpace(TASK_SPACE, { profileId: PROFILE_ID });
const page = task.page("p1");

// Open a github.com page, after a pause, and give the account that is signed in with it.
async function open(url) {
  await pause(randomDelay(PAGE_DELAY_MIN_MS, PAGE_DELAY_MAX_MS));
  await page.goto(url, { timeout: 60_000 });
  await pause(3_000);
  return page.evaluate(() => document.querySelector('meta[name="user-login"]')?.content || "");
}

const LOGIN = await open("https://github.com/");
if (!LOGIN) throw new Error("The Ego profile is not logged in to GitHub.");
if (typeof checkCollectorAccount === "function") await checkCollectorAccount(`github:user:${LOGIN}`, LOGIN, task);
await ingest(personDocument(LOGIN));

// The stars a page shows, in the order GitHub lists them (the one starred last first), and the link to the next
// page, which GitHub marks with a cursor. Runs in the page.
function readStars() {
  const items = [...document.querySelectorAll("div.col-12.d-block.width-full")].filter(card => card.querySelector("h3 a[href]")).map(card => {
    const text = selector => card.querySelector(selector)?.textContent.replace(/\s+/g, " ").trim() || "";
    return {
      href: card.querySelector("h3 a[href]").getAttribute("href"),
      description: text('p[itemprop="description"]'),
      language: text('[itemprop="programmingLanguage"]'),
      topics: [...card.querySelectorAll("a.topic-tag")].map(topic => topic.textContent.trim()),
      updated: card.querySelector("relative-time")?.getAttribute("datetime") || "",
    };
  });
  const next = [...document.querySelectorAll(".paginate-container a")].find(link => /^next$/i.test(link.textContent.trim()))?.href || "";
  return { login: document.querySelector('meta[name="user-login"]')?.content || "", items, next };
}

const counts = { pages: 0, stars: 0, failed: 0 };

// Store a repository and the star on it, with the card the page gave as the raw record.
async function storeStar(card, item) {
  const raw = { source: "github.page.stars", observed_at: new Date().toISOString(), response: item };
  for (const entry of repoDocuments(card)) await ingest(entry.document, entry.role === "post" ? raw : undefined);
  await ingest(addDocument(LOGIN, card), raw);
}

if (TARGET) {
  // Read one repository again: what its page shows now, stored as the stars store it, without the star.
  const fullName = targetRepoOf(TARGET);
  if (!fullName) throw new Error(`${TARGET} is not a GitHub repository`);
  await open(`https://github.com/${fullName}`);
  const found = await page.evaluate(() => ({
    path: new URL(document.querySelector('meta[property="og:url"]')?.content || location.href).pathname,
    description: (document.querySelector("p.f4.my-3") || document.querySelector('[itemprop="about"]'))?.textContent || "",
  }));
  const card = cardOf({ href: found.path, description: found.description });
  if (!card) throw new Error(`${TARGET} is not a repository page`);
  const raw = { source: "github.page.repo", observed_at: new Date().toISOString(), response: found };
  for (const entry of repoDocuments(card)) await ingest(entry.document, entry.role === "post" ? raw : undefined);
  console.log(JSON.stringify({ platform: "github", account: LOGIN, post: TARGET }));
} else {
  const keyOf = card => activityKey([{ document: addDocument(LOGIN, card) }]);
  const state = newestFirst();
  const waiting = [];
  let url = `https://github.com/${LOGIN}?tab=stars`;
  while (url && counts.pages < MAX_PAGES) {
    await open(url);
    const read = await page.evaluate(readStars);
    if (read.login !== LOGIN) throw new Error("The Ego profile is not logged in to GitHub.");
    counts.pages += 1;
    const pairs = read.items.map(item => ({ item, card: cardOf(item) })).filter(({ card }) => card);
    const known = await knownOf(pairs.map(({ card }) => keyOf(card)));
    const fresh = takeUnknown(state, pairs, known, !FULL);
    console.log(`Page ${counts.pages}: ${pairs.length} star(s), ${fresh.length} new.`);
    if (FULL) {
      // A full crawl stores each page as it is read, the oldest of the page first.
      for (const { item, card } of fresh.reverse()) {
        try { await storeStar(card, item); counts.stars += 1; } catch (error) { counts.failed += 1; console.warn(`Skipped ${card.fullName}: ${error.message}`); }
      }
    } else {
      waiting.push(...fresh);
    }
    if (state.done) { console.log("The stars run into repositories that are already stored; stopped."); break; }
    url = read.next;
  }
  // The pages come newest first, so every page is read before anything is stored and the stars go in oldest
  // first: that makes the order of observation match the order the stars were given in.
  for (const { item, card } of waiting.reverse()) {
    try { await storeStar(card, item); counts.stars += 1; } catch (error) { counts.failed += 1; console.warn(`Skipped ${card.fullName}: ${error.message}`); }
  }
  console.log(JSON.stringify({ platform: "github", account: LOGIN, ...counts }));
}

await task.finish({ keep: [] });
} // collector body

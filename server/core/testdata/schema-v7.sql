-- ActivityStreams documents are the facts. Collector observations, media copies
-- and everything derived from them (timelines, current state) live beside them.
-- References between documents are plain IDs: the referenced object need not
-- have been collected.

-- Identity of a content object (Note, Article, Person, Collection, ...).
CREATE TABLE IF NOT EXISTS object (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL
);

-- One observed state of an object. The document is the full ActivityStreams
-- object; the other columns are copied from it at write time for querying.
CREATE TABLE IF NOT EXISTS object_version (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  object_id TEXT NOT NULL REFERENCES object (id),
  document TEXT NOT NULL CHECK (json_valid(document)),
  content_hash TEXT NOT NULL,
  attributed_to TEXT,
  in_reply_to TEXT,
  quote_of TEXT,
  published_at TEXT,
  updated_at TEXT
);

CREATE INDEX IF NOT EXISTS object_version_object ON object_version (object_id, id);
CREATE INDEX IF NOT EXISTS object_version_author ON object_version (attributed_to);
CREATE INDEX IF NOT EXISTS object_version_reply ON object_version (in_reply_to);
CREATE INDEX IF NOT EXISTS object_version_quote ON object_version (quote_of);

-- An activity never changes. Undoing one is a later Undo or Remove activity.
-- kind is the positive activity type a row belongs to (an Undo of a Like has
-- kind Like, a Remove has kind Add); current state is derived from the latest
-- row per (actor, kind, object, target), and per announce_id for Announce:
-- the document ID of an Announce (or of the one an Undo retracts), so two
-- quotes of the same post stay separate.
CREATE TABLE IF NOT EXISTS activity (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL CHECK (type IN ('Create', 'Like', 'Announce', 'Add', 'Undo', 'Remove')),
  kind TEXT NOT NULL CHECK (kind IN ('Create', 'Like', 'Announce', 'Add')),
  actor TEXT NOT NULL,
  object TEXT NOT NULL,
  target TEXT,
  published_at TEXT,
  announce_id TEXT,
  document TEXT NOT NULL CHECK (json_valid(document))
);

CREATE INDEX IF NOT EXISTS activity_state ON activity (actor, kind, object, id);
CREATE INDEX IF NOT EXISTS activity_object ON activity (object);
CREATE INDEX IF NOT EXISTS activity_announce ON activity (announce_id) WHERE announce_id IS NOT NULL;

-- The name an entry goes by outside the store, in addresses: a random string, so
-- that nobody can reach the entries around one by counting.
CREATE TABLE IF NOT EXISTS activity_ref (
  activity_id INTEGER PRIMARY KEY REFERENCES activity (id),
  ref TEXT NOT NULL UNIQUE
);

-- Timeline entries the owner deleted. A soft delete: the activity and everything
-- it points at stay stored, and readers leave hidden entries out, so they can
-- be restored.
CREATE TABLE IF NOT EXISTS activity_hidden (
  activity_id INTEGER PRIMARY KEY REFERENCES activity (id),
  hidden_at TEXT NOT NULL
);

-- Timeline entries the owner made public. Everything collected is private until
-- an entry is listed here; a like, a favorite or a repost is an entry of its own,
-- so each is chosen separately from the post it points at.
CREATE TABLE IF NOT EXISTS activity_public (
  activity_id INTEGER PRIMARY KEY REFERENCES activity (id),
  public_at TEXT NOT NULL
);

-- A readable address the owner gave an entry, /<alias>, besides its random ref.
CREATE TABLE IF NOT EXISTS activity_alias (
  activity_id INTEGER PRIMARY KEY REFERENCES activity (id),
  alias TEXT NOT NULL UNIQUE
);

-- Objects a re-check found deleted on their platform. The archive keeps them;
-- the owner may delete such an entry, and only such an entry.
CREATE TABLE IF NOT EXISTS object_gone (
  object_id TEXT PRIMARY KEY,
  gone_at TEXT NOT NULL
);

-- Likes and favorites a re-check found taken back on their platform. The entry stays and is
-- flagged; the owner may delete it, and a later re-check that finds it again clears the flag.
CREATE TABLE IF NOT EXISTS activity_dropped (
  activity_id INTEGER PRIMARY KEY REFERENCES activity (id),
  dropped_at TEXT NOT NULL
);

-- Comments the owner deleted: replies to a post or to another comment, which are objects without an
-- activity of their own. A soft delete, like activity_hidden; only a comment a re-check found gone on its
-- platform can be deleted.
CREATE TABLE IF NOT EXISTS object_hidden (
  object_id TEXT PRIMARY KEY,
  hidden_at TEXT NOT NULL
);

-- What a collector saw. Not part of the ActivityStreams document.
CREATE TABLE IF NOT EXISTS observation (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  version_id INTEGER REFERENCES object_version (id),
  activity_id INTEGER REFERENCES activity (id),
  observed_at TEXT NOT NULL,
  observed_by TEXT NOT NULL,
  raw TEXT CHECK (raw IS NULL OR json_valid(raw)),
  CHECK ((version_id IS NULL) <> (activity_id IS NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS observation_version ON observation (version_id) WHERE version_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS observation_activity ON observation (activity_id) WHERE activity_id IS NOT NULL;

-- Local copy of a platform media URL. Collectors decide what to download and
-- register; readers use the local copy when one exists.
CREATE TABLE IF NOT EXISTS media (
  url TEXT PRIMARY KEY,
  file_ref TEXT NOT NULL,
  mime TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL,
  stored_at TEXT NOT NULL
);

-- One run of a platform's collector, as the scheduler or the owner started it. status is running, ok,
-- error, blocked or login; summary is the JSON counts the collector printed; created is how many
-- records the run really added (observations made by its platform while it ran).
CREATE TABLE IF NOT EXISTS collector_run (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  plugin TEXT NOT NULL,
  mode TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL,
  summary TEXT,
  message TEXT,
  created INTEGER
);

CREATE INDEX IF NOT EXISTS collector_run_plugin ON collector_run (plugin, id);

-- What the scheduler keeps of a platform between runs: whether the owner paused it, why it stopped
-- itself (blocked or login), when it runs next and how many runs in a row failed.
CREATE TABLE IF NOT EXISTS collector_state (
  plugin TEXT PRIMARY KEY,
  paused INTEGER NOT NULL DEFAULT 0,
  blocked_reason TEXT,
  next_run_at TEXT,
  failures INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS site_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  title TEXT NOT NULL
);

-- An explicit profile supersedes the legacy inference from activity actors.
CREATE TABLE IF NOT EXISTS owner_profile (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  display_name TEXT NOT NULL,
  accounts TEXT NOT NULL CHECK (json_valid(accounts))
);

CREATE TABLE IF NOT EXISTS collector_account_seen (
  platform TEXT PRIMARY KEY,
  account TEXT NOT NULL,
  name TEXT NOT NULL,
  seen_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS collector_interval (
  platform TEXT PRIMARY KEY,
  seconds INTEGER NOT NULL CHECK (seconds BETWEEN 900 AND 2592000)
);

CREATE TABLE IF NOT EXISTS collector_cron (
  platform TEXT PRIMARY KEY,
  expression TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS collector_probe (
  platform TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  checked_at TEXT NOT NULL
);

-- ActivityStreams documents are the facts. Collector observations, media copies
-- and everything derived from them (timelines, current state) live beside them.
-- References between documents are plain IDs: the referenced object need not
-- have been collected.

-- One observed state of a content object (Note, Article, Person, Collection, ...). The document is the
-- full ActivityStreams object; type is read from it, the other columns are copied from it at write time
-- for querying.
CREATE TABLE IF NOT EXISTS object_version (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  object_id TEXT NOT NULL,
  document TEXT NOT NULL CHECK (json_valid(document)),
  type TEXT GENERATED ALWAYS AS (json_extract(document, '$.type')) VIRTUAL,
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

-- An activity never changes. ref is the name an entry goes by outside the store, in addresses: a random
-- string, so that nobody can reach the entries around one by counting. Undoing one is a later Undo or Remove activity.
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
  document TEXT NOT NULL CHECK (json_valid(document)),
  ref TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS activity_ref ON activity (ref);
CREATE INDEX IF NOT EXISTS activity_state ON activity (actor, kind, object, id);
CREATE INDEX IF NOT EXISTS activity_object ON activity (object);
CREATE INDEX IF NOT EXISTS activity_announce ON activity (announce_id) WHERE announce_id IS NOT NULL;

-- Entries the owner deleted, comments included: a comment is an entry (its Create) like any other. A soft delete: the activity and everything
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
  mime TEXT NOT NULL DEFAULT ''
);

-- A person is a name and the accounts, on any platform, that are that person's. It is the owner's own note,
-- not something a collector observes: only the owner makes one and links accounts to it, and an account
-- belongs to at most one person. An account that belongs to no person is shown under its own name.
CREATE TABLE IF NOT EXISTS person (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS person_account (
  account TEXT PRIMARY KEY,
  person_id INTEGER NOT NULL REFERENCES person (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS person_account_person ON person_account (person_id);

-- The site, one row: the title the owner gave it and which person the owner is. The owner's timeline is what
-- the accounts of that person did. A part the owner never saved is null.
CREATE TABLE IF NOT EXISTS site (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  title TEXT,
  owner INTEGER REFERENCES person (id)
);

-- The owner's login, one row: the salt and verifier of the password, and the key the login cookies are signed with.
CREATE TABLE IF NOT EXISTS owner_login (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  salt BLOB NOT NULL,
  verifier BLOB NOT NULL,
  session_key BLOB NOT NULL
);

-- Saved order of the people in settings and timeline filters.
CREATE TABLE IF NOT EXISTS person_position (
  person_id INTEGER PRIMARY KEY REFERENCES person(id) ON DELETE CASCADE,
  position INTEGER NOT NULL
);

-- Private text files that belong to an archived post.
CREATE TABLE IF NOT EXISTS archive_attachment (
  object_id TEXT NOT NULL,
  path TEXT NOT NULL,
  content TEXT NOT NULL,
  PRIMARY KEY (object_id,path)
);

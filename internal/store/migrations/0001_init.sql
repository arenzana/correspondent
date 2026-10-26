-- Correspondent schema v1.
--
-- Entry IDs are microsecond crawl timestamps (FreshRSS-compatible): they are
-- unique, monotonic, signed-64-bit, and double as the "crawl time" that the
-- Google Reader ot/nt parameters filter on. Imported FreshRSS IDs are kept
-- verbatim; new IDs are always allocated above the current maximum.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,            -- web login (bcrypt)
    api_hash      TEXT,                     -- separate API password (bcrypt); NULL disables the Reader API
    prefs         TEXT NOT NULL DEFAULT '{}',
    created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,            -- sha256 of the opaque cookie value
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,               -- 'web' | 'greader'
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE folders (
    id       INTEGER PRIMARY KEY,
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    UNIQUE (user_id, name)
);

CREATE TABLE feeds (
    id             INTEGER PRIMARY KEY,
    user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    folder_id      INTEGER REFERENCES folders(id) ON DELETE SET NULL,
    url            TEXT NOT NULL,
    site_url       TEXT NOT NULL DEFAULT '',
    title          TEXT NOT NULL DEFAULT '',
    description    TEXT NOT NULL DEFAULT '',
    icon_url       TEXT NOT NULL DEFAULT '',
    etag           TEXT NOT NULL DEFAULT '',
    last_modified  TEXT NOT NULL DEFAULT '',
    interval_sec   INTEGER NOT NULL DEFAULT 1800,
    next_fetch_at  INTEGER NOT NULL DEFAULT 0,
    last_fetch_at  INTEGER NOT NULL DEFAULT 0,
    last_status    INTEGER NOT NULL DEFAULT 0,
    error_count    INTEGER NOT NULL DEFAULT 0,
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    UNIQUE (user_id, url)
);
CREATE INDEX feeds_due ON feeds(next_fetch_at);

CREATE TABLE entries (
    id           INTEGER PRIMARY KEY,       -- microsecond crawl timestamp
    feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid         TEXT NOT NULL,
    url          TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL DEFAULT '',
    author       TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',  -- sanitized HTML, never raw feed HTML
    excerpt      TEXT NOT NULL DEFAULT '',
    image_url    TEXT NOT NULL DEFAULT '',
    enclosures   TEXT NOT NULL DEFAULT '[]',
    content_hash TEXT NOT NULL DEFAULT '',
    published_at INTEGER NOT NULL,          -- seconds, from the feed (or crawl time)
    updated_at   INTEGER NOT NULL,          -- seconds, last content change seen
    read_at      INTEGER,                   -- seconds; NULL = unread
    starred_at   INTEGER,                   -- seconds; NULL = not starred
    state_at     INTEGER NOT NULL DEFAULT 0,-- seconds, last user state change
    UNIQUE (feed_id, guid)
);
CREATE INDEX entries_feed_id ON entries(feed_id, id DESC);
CREATE INDEX entries_unread ON entries(id DESC) WHERE read_at IS NULL;
CREATE INDEX entries_starred ON entries(id DESC) WHERE starred_at IS NOT NULL;

-- Append-only journal of user mutations, so state changes can be replayed
-- into another backend on rollback.
CREATE TABLE journal (
    seq      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id  INTEGER NOT NULL,
    at       INTEGER NOT NULL,
    action   TEXT NOT NULL,                 -- read|unread|star|unstar
    entry_id INTEGER NOT NULL
);

CREATE VIRTUAL TABLE entries_fts USING fts5(
    title, excerpt, author,
    content='entries', content_rowid='id', tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER entries_ai AFTER INSERT ON entries BEGIN
    INSERT INTO entries_fts(rowid, title, excerpt, author) VALUES (new.id, new.title, new.excerpt, new.author);
END;
CREATE TRIGGER entries_ad AFTER DELETE ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, excerpt, author) VALUES ('delete', old.id, old.title, old.excerpt, old.author);
END;
CREATE TRIGGER entries_au AFTER UPDATE OF title, excerpt, author ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, excerpt, author) VALUES ('delete', old.id, old.title, old.excerpt, old.author);
    INSERT INTO entries_fts(rowid, title, excerpt, author) VALUES (new.id, new.title, new.excerpt, new.author);
END;

-- FreshRSS migration support.

-- Instance-wide settings (e.g. the FreshRSS salt, the cutover time).
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Accept the static token FreshRSS issued to Reeder, sha1(salt + user +
-- api_hash), until disabled. It changes whenever the API password changes.
ALTER TABLE users ADD COLUMN legacy_auth INTEGER NOT NULL DEFAULT 0;

-- Entries carried over from FreshRSS. After cutover, a polled item whose GUID
-- is unknown but whose link matches an imported entry is the same article.
ALTER TABLE entries ADD COLUMN imported INTEGER NOT NULL DEFAULT 0;
CREATE INDEX entries_imported_url ON entries(feed_id, url) WHERE imported = 1;

-- Feeds built from web pages that publish no feed.
--
-- A parser is the rules (pagefeed.Rules, JSON) that find the articles on
-- one page, worked out once (exactly, or by a reader's AI) and shared by
-- every reader who follows that page. Only the server writes parsers, after
-- checking them against the page; readers never submit rules.
CREATE TABLE parsers (
    id          INTEGER PRIMARY KEY,
    url         TEXT NOT NULL UNIQUE,  -- normalized page address
    rules       TEXT NOT NULL,
    method      TEXT NOT NULL,         -- hfeed, jsonld, links, css
    created_by  INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    builtin     INTEGER NOT NULL DEFAULT 0  -- shipped with the app (pagefeed/builtin.json)
);

-- A page feed reads its page with a parser; NULL for ordinary feeds.
ALTER TABLE feeds ADD COLUMN parser_id INTEGER REFERENCES parsers (id) ON DELETE SET NULL;

-- The reader's own AI setup, used to write parsers: ai_key is the API key
-- sealed (secretbox) with a key file kept out of backups; ai_config is the
-- provider, address, model and the key's last four characters
-- (pagefeed.Config, JSON), none of it secret.
ALTER TABLE users ADD COLUMN ai_key BLOB;
ALTER TABLE users ADD COLUMN ai_config TEXT NOT NULL DEFAULT '';

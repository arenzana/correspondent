-- Search covers article text. entries.search_text is the article as plain
-- text, kept by the store whenever content changes (see SearchText), and
-- indexed with title, excerpt and author. Rows stored before this are
-- filled in at startup (Store.FillSearchText).
ALTER TABLE entries ADD COLUMN search_text TEXT NOT NULL DEFAULT '';

DROP TRIGGER entries_ai;
DROP TRIGGER entries_ad;
DROP TRIGGER entries_au;
DROP TABLE entries_fts;
CREATE VIRTUAL TABLE entries_fts USING fts5(
    title, excerpt, author, search_text,
    content='entries', content_rowid='id', tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER entries_ai AFTER INSERT ON entries BEGIN
    INSERT INTO entries_fts(rowid, title, excerpt, author, search_text)
    VALUES (new.id, new.title, new.excerpt, new.author, new.search_text);
END;
CREATE TRIGGER entries_ad AFTER DELETE ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, excerpt, author, search_text)
    VALUES ('delete', old.id, old.title, old.excerpt, old.author, old.search_text);
END;
CREATE TRIGGER entries_au AFTER UPDATE OF title, excerpt, author, search_text ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, excerpt, author, search_text)
    VALUES ('delete', old.id, old.title, old.excerpt, old.author, old.search_text);
    INSERT INTO entries_fts(rowid, title, excerpt, author, search_text)
    VALUES (new.id, new.title, new.excerpt, new.author, new.search_text);
END;
INSERT INTO entries_fts(entries_fts) VALUES ('rebuild');

-- Article summaries, made on request with a reader's AI and shared: keyed
-- by a hash of the article's text, so the same article in anyone's feeds
-- is summarized once.
CREATE TABLE summaries (
    hash       TEXT PRIMARY KEY,
    summary    TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

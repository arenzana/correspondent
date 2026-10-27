-- Saved searches: a reader's search kept in the sidebar like a folder, with
-- its own unread count. The query is free text, matched like the search box.
CREATE TABLE searches (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    query      TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (user_id, query)
);

-- Services a reader sends articles to (a webhook, a read-later service,
-- their own Wallabag…): the kind, settings that aren't secret, and the
-- credentials sealed with the server's keyring. send_starred is the one
-- rule: every article the reader stars is sent too.
CREATE TABLE integrations (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    name         TEXT NOT NULL,
    config       TEXT NOT NULL DEFAULT '{}',
    secret       BLOB,
    send_starred INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    last_ok_at   INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX integrations_user ON integrations (user_id);

-- One article sent to one integration: queued, retried with growing waits,
-- then sent or failed. remote is the item's address at the service, when
-- it gives one.
CREATE TABLE deliveries (
    id             INTEGER PRIMARY KEY,
    integration_id INTEGER NOT NULL REFERENCES integrations (id) ON DELETE CASCADE,
    entry_id       INTEGER NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    state          TEXT NOT NULL CHECK (state IN ('pending', 'sent', 'failed')),
    attempts       INTEGER NOT NULL DEFAULT 0,
    next_at        INTEGER NOT NULL,
    error          TEXT NOT NULL DEFAULT '',
    remote         TEXT NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    sent_at        INTEGER NOT NULL DEFAULT 0,
    UNIQUE (integration_id, entry_id)
);
CREATE INDEX deliveries_due ON deliveries (state, next_at);
CREATE INDEX deliveries_entry ON deliveries (entry_id);

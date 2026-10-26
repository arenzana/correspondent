-- Web Push: a device subscribes; feeds opt in to notifications.
CREATE TABLE push_subscriptions (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   TEXT NOT NULL UNIQUE,
    p256dh     TEXT NOT NULL,
    auth       TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    failures   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX push_subscriptions_user ON push_subscriptions(user_id);

ALTER TABLE feeds ADD COLUMN notify INTEGER NOT NULL DEFAULT 0;

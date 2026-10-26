-- Several readers on one server: roles, invitations, passkeys.
--
-- users.role is 'admin' or 'user'; the first account becomes the admin.
-- A disabled account (disabled_at set) can't sign in, its sessions are
-- gone, and its feeds aren't polled. webauthn_handle is the random user
-- handle passkeys are bound to (never the username or ID).
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
ALTER TABLE users ADD COLUMN disabled_at INTEGER;
ALTER TABLE users ADD COLUMN last_seen_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN webauthn_handle BLOB;
UPDATE users SET role = 'admin' WHERE id = (SELECT MIN(id) FROM users);

-- Sessions remember the device they came from and when they were last
-- used, so readers can see and end them.
ALTER TABLE sessions ADD COLUMN client TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN last_seen_at INTEGER NOT NULL DEFAULT 0;

-- One-time links: an invitation to create an account, or a password reset
-- for an existing one. Only a hash of the token is stored.
CREATE TABLE invites (
    token_hash  TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,                     -- 'invite' | 'reset'
    user_id     INTEGER REFERENCES users (id) ON DELETE CASCADE, -- reset: whose
    role        TEXT NOT NULL DEFAULT 'user',      -- invite: the new account's role
    note        TEXT NOT NULL DEFAULT '',          -- invite: who it's for, for the admin
    created_by  INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    used_at     INTEGER,
    used_by     INTEGER REFERENCES users (id) ON DELETE SET NULL
);

CREATE TABLE passkeys (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id   BLOB NOT NULL UNIQUE,
    credential      TEXT NOT NULL,                 -- webauthn.Credential, JSON (public key, counters, flags)
    name            TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    last_used_at    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX passkeys_user ON passkeys (user_id);

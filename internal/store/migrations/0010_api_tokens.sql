-- Personal API tokens, for the corr CLI, scripts and AI agents.
--
-- Only a hash of each token is stored. scope 'read' allows reading only
-- (GET); 'write' allows everything the account can do in the app.
CREATE TABLE api_tokens (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    token_hash    TEXT NOT NULL UNIQUE,
    scope         TEXT NOT NULL,          -- 'read' | 'write'
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX api_tokens_user ON api_tokens (user_id);

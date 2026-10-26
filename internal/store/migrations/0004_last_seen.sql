-- When each entry was last present in its feed's document (seconds).
-- Retention only deletes entries that have left their feed, so a deleted
-- entry can never be fetched again as a "new" duplicate.
ALTER TABLE entries ADD COLUMN last_seen_at INTEGER NOT NULL DEFAULT 0;

-- Feeds the user has paused are kept, with their history, but not polled.
ALTER TABLE feeds ADD COLUMN paused INTEGER NOT NULL DEFAULT 0;

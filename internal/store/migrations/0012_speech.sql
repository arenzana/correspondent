-- Read aloud: each reader's own text-to-speech key (sealed) and setup, and
-- the audio made for an entry (a file name under the data directory's
-- audio/). Audio belongs to the reader who made it; it is never shared.
ALTER TABLE users ADD COLUMN speech_key BLOB;
ALTER TABLE users ADD COLUMN speech_config TEXT NOT NULL DEFAULT '';
ALTER TABLE entries ADD COLUMN audio TEXT;

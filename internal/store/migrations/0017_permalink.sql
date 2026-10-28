-- A link blog's post (Daring Fireball, Lex on Tech) points at the article
-- it's about; permalink is the post's own page, empty for ordinary items.
-- Its text is the commentary, which a fetched full article would replace.
ALTER TABLE entries ADD COLUMN permalink TEXT NOT NULL DEFAULT '';

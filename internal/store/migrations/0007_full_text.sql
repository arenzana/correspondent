-- Full articles fetched from the article's web page.
--
-- feeds.full_text: fetch every new article's page (for title-only or
-- summary feeds). entries.feed_content holds the feed's own HTML when
-- content was replaced by the fetched article (NULL otherwise), so the
-- switch can be undone. entries.full_error explains a failed fetch.
ALTER TABLE feeds ADD COLUMN full_text INTEGER NOT NULL DEFAULT 0;
ALTER TABLE entries ADD COLUMN feed_content TEXT;
ALTER TABLE entries ADD COLUMN full_error TEXT NOT NULL DEFAULT '';

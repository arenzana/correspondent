-- Reading order: newest published first, ties broken by arrival.
CREATE INDEX entries_published ON entries(published_at DESC, id DESC);

-- Sponsored posts, flagged at ingest (title prefix or feed category) so a
-- reader who skips them gets them marked read on arrival.
ALTER TABLE entries ADD COLUMN sponsored INTEGER NOT NULL DEFAULT 0;

-- Entries already stored have no categories left; flag them by title.
UPDATE entries SET sponsored = 1
 WHERE lower(ltrim(title)) LIKE 'sponsor:%'
    OR lower(ltrim(title)) LIKE 'sponsored:%'
    OR lower(ltrim(title)) LIKE 'sponsored by %'
    OR lower(ltrim(title)) LIKE 'sponsored post:%'
    OR lower(ltrim(title)) LIKE '[sponsor]%'
    OR lower(ltrim(title)) LIKE '[sponsored]%'
    OR lower(ltrim(title)) LIKE '(sponsor)%'
    OR lower(ltrim(title)) LIKE '(sponsored)%'
    OR lower(ltrim(title)) LIKE 'paid post:%'
    OR lower(ltrim(title)) LIKE 'partner content:%'
    OR lower(ltrim(title)) LIKE 'advertisement:%';

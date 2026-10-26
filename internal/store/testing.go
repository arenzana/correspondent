package store

import "context"

// TestSetEntryURL changes an entry's link. Tests only; entries normally take
// their link from the feed.
func (s *Store) TestSetEntryURL(ctx context.Context, id int64, url string) error {
	return checkAffected(s.db.ExecContext(ctx, `UPDATE entries SET url = ? WHERE id = ?`, url, id))
}

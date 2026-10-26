package store

import (
	"context"
	"database/sql"
	"errors"
)

// ServerStats is the admin's view of what the server holds and does.
type ServerStats struct {
	Accounts     int   `json:"accounts"`
	Disabled     int   `json:"disabled"`
	Feeds        int   `json:"feeds"`
	Failing      int   `json:"failing"`
	Paused       int   `json:"paused"`
	Articles     int   `json:"articles"`
	Parsers      int   `json:"parsers"`
	LastFetchAt  int64 `json:"lastFetchAt"` // newest poll, seconds
	PolledLastHr int   `json:"polledLastHour"`
}

// Stats counts across all accounts.
func (s *Store) Stats(ctx context.Context, now int64) (ServerStats, error) {
	var x ServerStats
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM users WHERE disabled_at IS NOT NULL),
		(SELECT COUNT(*) FROM feeds),
		(SELECT COUNT(*) FROM feeds WHERE error_count > 0 AND paused = 0),
		(SELECT COUNT(*) FROM feeds WHERE paused = 1),
		(SELECT COUNT(*) FROM entries),
		(SELECT COUNT(*) FROM parsers),
		(SELECT COALESCE(MAX(last_fetch_at), 0) FROM feeds),
		(SELECT COUNT(*) FROM feeds WHERE last_fetch_at > ?)`, now-3600).
		Scan(&x.Accounts, &x.Disabled, &x.Feeds, &x.Failing, &x.Paused, &x.Articles, &x.Parsers, &x.LastFetchAt, &x.PolledLastHr)
	return x, err
}

// FailingFeed is a feed that keeps failing, across accounts.
type FailingFeed struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Owner      string `json:"owner"`
	ErrorCount int    `json:"errorCount"`
	LastError  string `json:"lastError"`
}

// FailingFeeds lists the feeds failing most, across accounts.
func (s *Store) FailingFeeds(ctx context.Context, limit int) ([]FailingFeed, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.title, f.url, u.username, f.error_count, f.last_error
		FROM feeds f JOIN users u ON u.id = f.user_id WHERE f.error_count > 0 AND f.paused = 0
		ORDER BY f.error_count DESC, f.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FailingFeed
	for rows.Next() {
		var f FailingFeed
		if err := rows.Scan(&f.ID, &f.Title, &f.URL, &f.Owner, &f.ErrorCount, &f.LastError); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ParserInfo is a shared parser as the admin sees it.
type ParserInfo struct {
	ID        int64  `json:"id"`
	URL       string `json:"url"`
	Method    string `json:"method"`
	Builtin   bool   `json:"builtin"`
	CreatedBy string `json:"createdBy"`
	UpdatedAt int64  `json:"updatedAt"`
	Feeds     int    `json:"feeds"`   // readers following the page
	Failing   int    `json:"failing"` // of those, failing now
}

// ParserInfos lists every shared parser with who uses it.
func (s *Store) ParserInfos(ctx context.Context) ([]ParserInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id, p.url, p.method, p.builtin, COALESCE(u.username, ''), p.updated_at,
		(SELECT COUNT(*) FROM feeds f WHERE f.parser_id = p.id),
		(SELECT COUNT(*) FROM feeds f WHERE f.parser_id = p.id AND f.error_count > 0)
		FROM parsers p LEFT JOIN users u ON u.id = p.created_by ORDER BY p.url`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParserInfo
	for rows.Next() {
		var p ParserInfo
		if err := rows.Scan(&p.ID, &p.URL, &p.Method, &p.Builtin, &p.CreatedBy, &p.UpdatedAt, &p.Feeds, &p.Failing); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ErrInUse is returned for deleting something still in use.
var ErrInUse = errors.New("still in use")

// DeleteParser removes a parser no feed uses.
func (s *Store) DeleteParser(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM feeds WHERE parser_id = ?`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrInUse
		}
		return checkAffected(tx.ExecContext(ctx, `DELETE FROM parsers WHERE id = ?`, id))
	})
}

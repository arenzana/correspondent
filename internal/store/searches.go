package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Search is a saved search, with how many unread articles match it.
type Search struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Query  string `json:"query"`
	Unread int    `json:"unread"`
}

// MaxSearches is how many searches a reader can keep.
const MaxSearches = 50

// ErrTooManySearches: the reader already keeps MaxSearches.
var ErrTooManySearches = errors.New("too many saved searches")

// Searches lists a reader's saved searches, oldest first, with unread counts.
func (s *Store) Searches(ctx context.Context, userID int64) ([]Search, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, query FROM searches WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	var out []Search
	for rows.Next() {
		var x Search
		if err := rows.Scan(&x.ID, &x.Name, &x.Query); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		q := ftsQuery(out[i].Query)
		if q == "" {
			continue
		}
		err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM entries e JOIN feeds d ON d.id = e.feed_id
			WHERE d.user_id = ? AND e.read_at IS NULL
			  AND e.id IN (SELECT rowid FROM entries_fts WHERE entries_fts MATCH ?)`, userID, q).Scan(&out[i].Unread)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AddSearch saves a search; saving the same query again renames it.
func (s *Store) AddSearch(ctx context.Context, userID int64, name, query string) (Search, error) {
	name, query = strings.TrimSpace(name), strings.TrimSpace(query)
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM searches WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return Search{}, err
	}
	var x Search
	err := s.db.QueryRowContext(ctx, `INSERT INTO searches (user_id, name, query, created_at)
		SELECT ?, ?, ?, ? WHERE ? < ? OR EXISTS (SELECT 1 FROM searches WHERE user_id = ? AND query = ?)
		ON CONFLICT (user_id, query) DO UPDATE SET name = excluded.name
		RETURNING id, name, query`,
		userID, name, query, time.Now().Unix(), n, MaxSearches, userID, query).Scan(&x.ID, &x.Name, &x.Query)
	if errors.Is(err, sql.ErrNoRows) {
		return Search{}, ErrTooManySearches
	}
	return x, err
}

// RenameSearch renames one of a reader's saved searches.
func (s *Store) RenameSearch(ctx context.Context, userID, id int64, name string) error {
	return checkAffected(s.db.ExecContext(ctx, `UPDATE searches SET name = ? WHERE id = ? AND user_id = ?`,
		strings.TrimSpace(name), id, userID))
}

// DeleteSearch forgets one of a reader's saved searches.
func (s *Store) DeleteSearch(ctx context.Context, userID, id int64) error {
	return checkAffected(s.db.ExecContext(ctx, `DELETE FROM searches WHERE id = ? AND user_id = ?`, id, userID))
}

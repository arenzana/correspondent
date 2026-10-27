package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/arenzana/correspondent/internal/content"
)

// searchTextMax bounds an article's indexed text: long enough for any
// article, short enough that a pasted book doesn't bloat the index.
const searchTextMax = 64 << 10

// searchText is what search indexes for an article's HTML.
func searchText(html string) string { return content.SearchText(html, searchTextMax) }

// FillSearchText indexes the text of articles stored before search covered
// it (migration 0013). It runs at startup and returns how many it filled.
func (s *Store) FillSearchText(ctx context.Context) (int, error) {
	n := 0
	var after int64
	for {
		rows, err := s.db.QueryContext(ctx, `SELECT id, content FROM entries
			WHERE search_text = '' AND content <> '' AND id > ? ORDER BY id LIMIT 200`, after)
		if err != nil {
			return n, err
		}
		type row struct {
			id   int64
			html string
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.html); err != nil {
				rows.Close()
				return n, err
			}
			batch = append(batch, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return n, err
		}
		if len(batch) == 0 {
			return n, nil
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return n, err
		}
		for _, r := range batch {
			if _, err := tx.ExecContext(ctx, `UPDATE entries SET search_text = ? WHERE id = ?`, searchText(r.html), r.id); err != nil {
				tx.Rollback()
				return n, err
			}
		}
		if err := tx.Commit(); err != nil {
			return n, err
		}
		n += len(batch)
		after = batch[len(batch)-1].id
	}
}

// Summary returns the shared summary of an article's text, by its hash.
func (s *Store) Summary(ctx context.Context, hash string) (string, bool, error) {
	var text string
	err := s.db.QueryRowContext(ctx, `SELECT summary FROM summaries WHERE hash = ?`, hash).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return text, err == nil, err
}

// SaveSummary stores a summary for everyone who has the same article.
func (s *Store) SaveSummary(ctx context.Context, hash, text string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO summaries (hash, summary, created_at) VALUES (?, ?, ?)
		ON CONFLICT (hash) DO UPDATE SET summary = excluded.summary`, hash, text, time.Now().Unix())
	return err
}

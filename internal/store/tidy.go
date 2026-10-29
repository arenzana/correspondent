package store

import (
	"context"
	"database/sql"

	"github.com/arenzana/correspondent/internal/content"
)

// SettingTidied is the version of content.Tidy that stored articles have
// been through; TidyStored brings older ones up to TidyVersion.
const SettingTidied = "content.tidied"

// TidyVersion changes whenever content.Tidy changes what it keeps.
const TidyVersion = "2"

// TidyStored puts stored articles, and the feed text kept beside fetched
// ones, through content.Tidy once per TidyVersion, in batches, so articles
// stored before it read like new ones. It runs in the background at
// startup and returns how many changed.
func (s *Store) TidyStored(ctx context.Context) (int, error) {
	if v, err := s.Setting(ctx, SettingTidied); err == nil && v == TidyVersion {
		return 0, nil
	}
	n := 0
	var after int64
	for {
		rows, err := s.db.QueryContext(ctx, `SELECT id, title, content, feed_content FROM entries
			WHERE id > ? ORDER BY id LIMIT 200`, after)
		if err != nil {
			return n, err
		}
		type row struct {
			id          int64
			title, html string
			feedHTML    sql.NullString
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.title, &r.html, &r.feedHTML); err != nil {
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
			return n, s.SetSetting(ctx, SettingTidied, TidyVersion)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return n, err
		}
		for _, r := range batch {
			after = r.id
			html := content.Tidy(r.html, r.title)
			feedHTML := r.feedHTML
			if feedHTML.Valid {
				feedHTML.String = content.Tidy(feedHTML.String, r.title)
			}
			if html == r.html && feedHTML == r.feedHTML {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE entries SET content = ?, search_text = ?, feed_content = ? WHERE id = ?`,
				html, searchText(html), feedHTML, r.id); err != nil {
				tx.Rollback()
				return n, err
			}
			n++
		}
		if err := tx.Commit(); err != nil {
			return n, err
		}
	}
}

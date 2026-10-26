package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Folder struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Unread int    `json:"unread"`
}

type Feed struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"-"`
	FolderID    *int64 `json:"folderId"`
	URL         string `json:"url"`
	SiteURL     string `json:"siteUrl"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IconURL     string `json:"iconUrl"`
	LastFetchAt int64  `json:"lastFetchAt"`
	LastStatus  int    `json:"lastStatus"`
	ErrorCount  int    `json:"errorCount"`
	LastError   string `json:"lastError"`
	Paused      bool   `json:"paused"`
	Notify      bool   `json:"notify"`
	FullText    bool   `json:"fullText"`
	Unread      int    `json:"unread"`
	Total       int    `json:"total"`
	Starred     int    `json:"starred"`
	LatestAt    int64  `json:"latestAt"` // newest publication time (seconds), 0 if none
	// Cadence is the number of entries per day over the last 14 days, oldest
	// first; the UI draws it as the feed's pulse.
	Cadence []int `json:"cadence"`

	ETag         string `json:"-"`
	LastModified string `json:"-"`
	IntervalSec  int64  `json:"-"`
}

func (s *Store) Folders(ctx context.Context, userID int64) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.name,
		       (SELECT COUNT(*) FROM entries e JOIN feeds d ON d.id = e.feed_id
		         WHERE d.folder_id = f.id AND e.read_at IS NULL)
		  FROM folders f WHERE f.user_id = ? ORDER BY f.position, f.name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.Unread); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// EnsureFolder returns the ID of the named folder, creating it if needed.
func (s *Store) EnsureFolder(ctx context.Context, userID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("folder name is empty")
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO folders (user_id, name) VALUES (?, ?) ON CONFLICT (user_id, name) DO NOTHING`, userID, name); err != nil {
		return 0, err
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM folders WHERE user_id = ? AND name = ?`, userID, name).Scan(&id)
	return id, err
}

func (s *Store) RenameFolder(ctx context.Context, userID, folderID int64, name string) error {
	return checkAffected(s.db.ExecContext(ctx,
		`UPDATE folders SET name = ? WHERE id = ? AND user_id = ?`, strings.TrimSpace(name), folderID, userID))
}

func (s *Store) DeleteFolder(ctx context.Context, userID, folderID int64) error {
	return checkAffected(s.db.ExecContext(ctx, `DELETE FROM folders WHERE id = ? AND user_id = ?`, folderID, userID))
}

const feedColumns = `f.id, f.user_id, f.folder_id, f.url, f.site_url, f.title, f.description, f.icon_url,
	f.last_fetch_at, f.last_status, f.error_count, f.last_error, f.etag, f.last_modified, f.interval_sec, f.paused, f.notify, f.full_text`

func scanFeed(sc interface{ Scan(...any) error }, f *Feed, extra ...any) error {
	var folder sql.NullInt64
	dest := []any{&f.ID, &f.UserID, &folder, &f.URL, &f.SiteURL, &f.Title, &f.Description, &f.IconURL,
		&f.LastFetchAt, &f.LastStatus, &f.ErrorCount, &f.LastError, &f.ETag, &f.LastModified, &f.IntervalSec, &f.Paused, &f.Notify, &f.FullText}
	if err := sc.Scan(append(dest, extra...)...); err != nil {
		return err
	}
	if folder.Valid {
		f.FolderID = &folder.Int64
	}
	return nil
}

// Feeds lists a user's subscriptions with unread/total counts.
func (s *Store) Feeds(ctx context.Context, userID int64) ([]Feed, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+feedColumns+`,
		       (SELECT COUNT(*) FROM entries e WHERE e.feed_id = f.id AND e.read_at IS NULL),
		       (SELECT COUNT(*) FROM entries e WHERE e.feed_id = f.id),
		       (SELECT COUNT(*) FROM entries e WHERE e.feed_id = f.id AND e.starred_at IS NOT NULL),
		       (SELECT COALESCE(MAX(e.published_at), 0) FROM entries e WHERE e.feed_id = f.id)
		  FROM feeds f WHERE f.user_id = ? ORDER BY f.title COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Feed{}
	byID := map[int64]int{}
	for rows.Next() {
		var f Feed
		if err := scanFeed(rows, &f, &f.Unread, &f.Total, &f.Starred, &f.LatestAt); err != nil {
			return nil, err
		}
		f.Cadence = make([]int, 14)
		byID[f.ID] = len(out)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Per-day publishing cadence for the last two weeks, in one pass.
	now := time.Now()
	start := now.AddDate(0, 0, -14).Unix()
	crows, err := s.db.QueryContext(ctx, `
		SELECT e.feed_id, (e.published_at - ?) / 86400, COUNT(*)
		  FROM entries e JOIN feeds f ON f.id = e.feed_id
		 WHERE f.user_id = ? AND e.published_at >= ? AND e.published_at <= ?
		 GROUP BY 1, 2`, start, userID, start, now.Unix())
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var feedID, day int64
		var n int
		if err := crows.Scan(&feedID, &day, &n); err != nil {
			return nil, err
		}
		if i, ok := byID[feedID]; ok && day >= 0 && day < 14 {
			out[i].Cadence[day] = n
		}
	}
	return out, crows.Err()
}

func (s *Store) Feed(ctx context.Context, userID, feedID int64) (*Feed, error) {
	var f Feed
	err := scanFeed(s.db.QueryRowContext(ctx,
		`SELECT `+feedColumns+` FROM feeds f WHERE f.id = ? AND f.user_id = ?`, feedID, userID), &f)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &f, err
}

// AddFeed subscribes to url. If the subscription already exists its ID is
// returned unchanged.
func (s *Store) AddFeed(ctx context.Context, userID int64, url, title string, folderID *int64) (int64, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feeds (user_id, folder_id, url, title, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, url) DO NOTHING`, userID, folderID, url, title, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM feeds WHERE user_id = ? AND url = ?`, userID, url).Scan(&id)
	return id, err
}

// UpdateFeed changes the user-editable fields. A nil title leaves it as is;
// clearFolder moves the feed out of any folder.
func (s *Store) UpdateFeed(ctx context.Context, userID, feedID int64, title *string, folderID *int64, clearFolder bool) error {
	if title != nil {
		if err := checkAffected(s.db.ExecContext(ctx,
			`UPDATE feeds SET title = ? WHERE id = ? AND user_id = ?`, strings.TrimSpace(*title), feedID, userID)); err != nil {
			return err
		}
	}
	if folderID != nil || clearFolder {
		return checkAffected(s.db.ExecContext(ctx,
			`UPDATE feeds SET folder_id = ? WHERE id = ? AND user_id = ?`, folderID, feedID, userID))
	}
	return nil
}

func (s *Store) DeleteFeed(ctx context.Context, userID, feedID int64) error {
	return checkAffected(s.db.ExecContext(ctx, `DELETE FROM feeds WHERE id = ? AND user_id = ?`, feedID, userID))
}

// DueFeeds returns feeds whose next fetch time has passed, across all users.
func (s *Store) DueFeeds(ctx context.Context, now time.Time, limit int) ([]Feed, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+feedColumns+` FROM feeds f WHERE f.paused = 0 AND f.next_fetch_at <= ? ORDER BY f.next_fetch_at LIMIT ?`,
		now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Feed
	for rows.Next() {
		var f Feed
		if err := scanFeed(rows, &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ScheduleNow makes a feed due immediately.
func (s *Store) ScheduleNow(ctx context.Context, feedID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE feeds SET next_fetch_at = 0 WHERE id = ?`, feedID)
	return err
}

// FetchResult records the outcome of one poll.
type FetchResult struct {
	Status       int
	Err          string
	ETag         string
	LastModified string
	Title        string // only applied if the feed has no title yet
	SiteURL      string
	Description  string
	IconURL      string
	NextFetch    time.Time
}

func (s *Store) SaveFetchResult(ctx context.Context, feedID int64, r FetchResult) error {
	now := time.Now().Unix()
	if r.Err != "" {
		_, err := s.db.ExecContext(ctx, `
			UPDATE feeds SET last_fetch_at = ?, last_status = ?, last_error = ?, error_count = error_count + 1,
			       next_fetch_at = ? WHERE id = ?`, now, r.Status, r.Err, r.NextFetch.Unix(), feedID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE feeds SET last_fetch_at = ?, last_status = ?, last_error = '', error_count = 0, next_fetch_at = ?,
		       etag = ?, last_modified = ?,
		       title = CASE WHEN title = '' AND ? <> '' THEN ? ELSE title END,
		       site_url = CASE WHEN ? <> '' THEN ? ELSE site_url END,
		       description = CASE WHEN ? <> '' THEN ? ELSE description END,
		       icon_url = CASE WHEN ? <> '' THEN ? ELSE icon_url END
		 WHERE id = ?`,
		now, r.Status, r.NextFetch.Unix(), r.ETag, r.LastModified,
		r.Title, r.Title, r.SiteURL, r.SiteURL, r.Description, r.Description, r.IconURL, r.IconURL, feedID)
	return err
}

func checkAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ImportFeed inserts a feed keeping a legacy numeric ID (from FreshRSS) so
// that Google Reader clients see the same feed/<id> streams after cutover.
// A zero id lets SQLite allocate one. Existing subscriptions are left alone.
func (s *Store) ImportFeed(ctx context.Context, userID, id int64, url, title, siteURL string, folderID *int64) (int64, error) {
	var idArg any
	if id > 0 {
		idArg = id
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feeds (id, user_id, folder_id, url, site_url, title, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, idArg, userID, folderID, url, siteURL, title, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	var got int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM feeds WHERE user_id = ? AND url = ?`, userID, url).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("feed ID %d is already used by another subscription", id)
	}
	return got, err
}

// SetFeedSchedule sets a feed's polling interval (0 keeps the current one)
// and whether it is paused.
func (s *Store) SetFeedSchedule(ctx context.Context, userID, feedID int64, intervalSec int64, paused bool) error {
	return checkAffected(s.db.ExecContext(ctx, `
		UPDATE feeds SET paused = ?, interval_sec = CASE WHEN ? > 0 THEN ? ELSE interval_sec END
		 WHERE id = ? AND user_id = ?`, paused, intervalSec, intervalSec, feedID, userID))
}

// ErrDuplicate means the user already subscribes to that address.
var ErrDuplicate = errors.New("already subscribed")

// SetFeedURL moves a feed to a new address (a site that moved its feed),
// keeping its ID, history and settings. Conditional-request state is
// cleared and the feed is fetched again right away.
func (s *Store) SetFeedURL(ctx context.Context, userID, feedID int64, url string) error {
	var other int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM feeds WHERE user_id = ? AND url = ? AND id <> ?`, userID, url, feedID).Scan(&other)
	if err == nil {
		return ErrDuplicate
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return checkAffected(s.db.ExecContext(ctx, `
		UPDATE feeds SET url = ?, etag = '', last_modified = '', error_count = 0, last_error = '', next_fetch_at = 0
		 WHERE id = ? AND user_id = ?`, url, feedID, userID))
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type Enclosure struct {
	URL    string `json:"url"`
	Type   string `json:"type"`
	Length int64  `json:"length,omitempty"`
}

// Entry is one article. Content is only populated by Entry/EntriesByID;
// list queries leave it empty to keep summaries small.
type Entry struct {
	ID          int64  `json:"id,string"`
	FeedID      int64  `json:"feedId"`
	GUID        string `json:"-"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Content     string `json:"content,omitempty"`
	Excerpt     string `json:"excerpt"`
	ImageURL    string `json:"imageUrl"`
	Enclosures  string `json:"-"`
	PublishedAt int64  `json:"publishedAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	Read        bool   `json:"read"`
	Starred     bool   `json:"starred"`
	FullText    bool   `json:"fullText"`  // content is the article fetched from its page
	FullError   string `json:"fullError"` // why fetching the full article failed
	Audio       string `json:"-"`         // file of the audio read aloud, if made
	HasAudio    bool   `json:"audio"`
	Sponsored   bool   `json:"sponsored,omitempty"` // looks like a paid post (title or feed category)
	// Summary is the article's shared summary, when anyone has had one made
	// (filled in by the API for a single article, never in lists).
	Summary   string `json:"summary,omitempty"`
	StarredAt int64  `json:"-"`
	ReadAt    int64  `json:"-"`
}

// NewEntry is a normalized, sanitized item ready to store.
type NewEntry struct {
	GUID        string
	URL         string
	Title       string
	Author      string
	Content     string
	Excerpt     string
	ImageURL    string
	Enclosures  string
	ContentHash string
	PublishedAt time.Time // zero = unknown
	// FullText marks Content as the article fetched from its page; the
	// feed's own HTML (possibly empty, for title-only feeds) is FeedContent.
	FullText    bool
	FeedContent string
	FullError   string
	// Sponsored marks a paid post; for a reader who skips them (the
	// skipSponsored preference) it arrives already read.
	Sponsored bool
}

// UpsertEntries stores a feed's items in one short transaction. New items get
// fresh crawl-time IDs, allocated oldest-published first so a batch sorts
// the way the feed intended; items already seen are updated only when their
// content hash changed, keeping their ID and read/star state. New items
// published before readBefore are stored as already read (used on a feed's
// first poll, so its back catalogue doesn't flood Unread); on that poll,
// undated items count as back catalogue too, since their age is unknown.
func (s *Store) UpsertEntries(ctx context.Context, feedID int64, items []NewEntry, readBefore time.Time) (inserted, updated int, err error) {
	now := time.Now()
	items = slices.Clone(items)
	slices.SortStableFunc(items, func(a, b NewEntry) int { return a.PublishedAt.Compare(b.PublishedAt) })
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var skipSponsored bool
	if slices.ContainsFunc(items, func(it NewEntry) bool { return it.Sponsored }) {
		err := tx.QueryRowContext(ctx, `SELECT coalesce(json_extract(u.prefs, '$.skipSponsored'), 0) = 1
			FROM feeds f JOIN users u ON u.id = f.user_id WHERE f.id = ?`, feedID).Scan(&skipSponsored)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, 0, err
		}
	}
	for _, it := range items {
		var (
			id   int64
			hash string
		)
		err := tx.QueryRowContext(ctx, `SELECT id, content_hash FROM entries WHERE feed_id = ? AND guid = ?`,
			feedID, it.GUID).Scan(&id, &hash)
		if errors.Is(err, sql.ErrNoRows) && it.URL != "" {
			// An imported FreshRSS entry may carry a GUID we derive differently
			// (FreshRSS hashes some GUID-less items). Same feed and link means
			// same article: adopt our GUID so later polls match directly.
			err = tx.QueryRowContext(ctx, `SELECT id, content_hash FROM entries
				WHERE feed_id = ? AND url = ? AND imported = 1 LIMIT 1`, feedID, it.URL).Scan(&id, &hash)
			if err == nil {
				if _, err = tx.ExecContext(ctx, `UPDATE entries SET guid = ?, imported = 0 WHERE id = ?`, it.GUID, id); err != nil {
					return 0, 0, err
				}
			}
		}
		if err == nil {
			if _, err := tx.ExecContext(ctx, `UPDATE entries SET last_seen_at = ? WHERE id = ?`, now.Unix(), id); err != nil {
				return 0, 0, err
			}
		}
		switch {
		case errors.Is(err, sql.ErrNoRows):
			pub := it.PublishedAt
			// Missing or implausibly future dates fall back to crawl time.
			if pub.IsZero() || pub.After(now.Add(24*time.Hour)) {
				pub = now
			}
			var readAt any
			if pub.Before(readBefore) || (it.PublishedAt.IsZero() && !readBefore.IsZero()) || (it.Sponsored && skipSponsored) {
				readAt = now.Unix()
			}
			var feedContent any // NULL: content is the feed's own
			if it.FullText {
				feedContent = it.FeedContent
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO entries (id, feed_id, guid, url, title, author, content, excerpt, image_url,
				                     enclosures, content_hash, published_at, updated_at, read_at, last_seen_at,
				                     feed_content, full_error, sponsored, search_text)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				s.nextID(now), feedID, it.GUID, it.URL, it.Title, it.Author, it.Content, it.Excerpt,
				it.ImageURL, it.Enclosures, it.ContentHash, pub.Unix(), now.Unix(), readAt, now.Unix(),
				feedContent, it.FullError, it.Sponsored, searchText(it.Content))
			if err != nil {
				return 0, 0, fmt.Errorf("insert %q: %w", it.GUID, err)
			}
			inserted++
		case err != nil:
			return 0, 0, err
		case hash == "":
			// Imported and not polled since: record our fingerprint as the
			// baseline but keep the content the previous reader served, which
			// is what clients have cached. Later edits update normally.
			if _, err = tx.ExecContext(ctx, `UPDATE entries SET content_hash = ? WHERE id = ?`, it.ContentHash, id); err != nil {
				return 0, 0, err
			}
		case hash != it.ContentHash:
			// An entry showing the fetched full article keeps it; the feed's
			// new text goes to feed_content, to be shown if the user switches back.
			_, err = tx.ExecContext(ctx, `
				UPDATE entries SET url = ?, title = ?, author = ?,
				       content = CASE WHEN feed_content IS NULL THEN ? ELSE content END,
				       search_text = CASE WHEN feed_content IS NULL THEN ? ELSE search_text END,
				       feed_content = CASE WHEN feed_content IS NULL THEN NULL ELSE ? END,
				       excerpt = ?, image_url = CASE WHEN ? <> '' THEN ? ELSE image_url END,
				       enclosures = ?, content_hash = ?, updated_at = ? WHERE id = ?`,
				it.URL, it.Title, it.Author, it.Content, searchText(it.Content), it.Content, it.Excerpt, it.ImageURL, it.ImageURL,
				it.Enclosures, it.ContentHash, now.Unix(), id)
			if err != nil {
				return 0, 0, err
			}
			updated++
		}
	}
	return inserted, updated, tx.Commit()
}

type State string

const (
	StateAll     State = ""
	StateUnread  State = "unread"
	StateRead    State = "read"
	StateStarred State = "starred"
)

// Filter selects entries for both the modern API and the Reader facade.
type Filter struct {
	UserID   int64
	FeedID   int64
	FolderID int64
	State    State
	Exclude  State   // e.g. exclude read (Reader xt=read)
	Query    string  // FTS5 query over title, excerpt, author and article text
	IDs      []int64 // restrict to these IDs
	MinID    int64   // id >= MinID (crawl-time lower bound)
	MaxID    int64   // id <= MaxID (crawl-time upper bound)
	After    int64   // keyset cursor: id < After (newest first) or id > After (oldest first)
	Oldest   bool    // oldest first
	Limit    int

	// ByPublished orders by publication time instead of arrival (crawl ID),
	// for reading. The Reader API never sets it: Reeder's incremental sync
	// depends on arrival order. With it, the cursor is (AfterPub, After).
	ByPublished bool
	AfterPub    int64
}

func (f Filter) where() (string, []any) {
	conds := []string{"d.user_id = ?"}
	args := []any{f.UserID}
	if f.FeedID != 0 {
		conds = append(conds, "e.feed_id = ?")
		args = append(args, f.FeedID)
	}
	if f.FolderID != 0 {
		conds = append(conds, "d.folder_id = ?")
		args = append(args, f.FolderID)
	}
	for _, st := range []struct {
		s   State
		neg bool
	}{{f.State, false}, {f.Exclude, true}} {
		var c string
		switch st.s {
		case StateUnread:
			c = "e.read_at IS NULL"
		case StateRead:
			c = "e.read_at IS NOT NULL"
		case StateStarred:
			c = "e.starred_at IS NOT NULL"
		default:
			continue
		}
		if st.neg {
			c = "NOT (" + c + ")"
		}
		conds = append(conds, c)
	}
	if q := ftsQuery(f.Query); q != "" {
		conds = append(conds, "e.id IN (SELECT rowid FROM entries_fts WHERE entries_fts MATCH ?)")
		args = append(args, q)
	}
	if len(f.IDs) > 0 {
		conds = append(conds, "e.id IN ("+placeholders(len(f.IDs))+")")
		args = append(args, int64Args(f.IDs)...)
	}
	if f.MinID > 0 {
		conds = append(conds, "e.id >= ?")
		args = append(args, f.MinID)
	}
	if f.MaxID > 0 {
		conds = append(conds, "e.id <= ?")
		args = append(args, f.MaxID)
	}
	if f.After > 0 {
		op := "<"
		if f.Oldest {
			op = ">"
		}
		if f.ByPublished {
			conds = append(conds, "(e.published_at, e.id) "+op+" (?, ?)")
			args = append(args, f.AfterPub, f.After)
		} else {
			conds = append(conds, "e.id "+op+" ?")
			args = append(args, f.After)
		}
	}
	return strings.Join(conds, " AND "), args
}

func (f Filter) orderBy() string {
	dir := "DESC"
	if f.Oldest {
		dir = "ASC"
	}
	if f.ByPublished {
		return "e.published_at " + dir + ", e.id " + dir
	}
	return "e.id " + dir
}

// ftsQuery turns free text into a safe FTS5 prefix query: each word becomes
// a quoted prefix term, so user input can never be FTS syntax.
func ftsQuery(q string) string {
	var terms []string
	for _, w := range strings.Fields(q) {
		w = strings.ReplaceAll(w, `"`, "")
		if w != "" {
			terms = append(terms, `"`+w+`"*`)
		}
	}
	return strings.Join(terms, " ")
}

const entryColumns = `e.id, e.feed_id, e.guid, e.url, e.title, e.author, e.excerpt, e.image_url, e.enclosures,
	e.published_at, e.updated_at, COALESCE(e.read_at, 0), COALESCE(e.starred_at, 0),
	e.feed_content IS NOT NULL, e.full_error, COALESCE(e.audio, ''), e.sponsored`

func scanEntry(sc interface{ Scan(...any) error }, e *Entry, extra ...any) error {
	dest := []any{&e.ID, &e.FeedID, &e.GUID, &e.URL, &e.Title, &e.Author, &e.Excerpt, &e.ImageURL,
		&e.Enclosures, &e.PublishedAt, &e.UpdatedAt, &e.ReadAt, &e.StarredAt, &e.FullText, &e.FullError, &e.Audio, &e.Sponsored}
	if err := sc.Scan(append(dest, extra...)...); err != nil {
		return err
	}
	e.Read = e.ReadAt != 0
	e.Starred = e.StarredAt != 0
	e.HasAudio = e.Audio != ""
	return nil
}

// SetAudio records the audio file made for a user's entry ("" clears it).
func (s *Store) SetAudio(ctx context.Context, userID, entryID int64, file string) error {
	var v any
	if file != "" {
		v = file
	}
	return checkAffected(s.db.ExecContext(ctx, `UPDATE entries SET audio = ?
		WHERE id = ? AND feed_id IN (SELECT id FROM feeds WHERE user_id = ?)`, v, entryID, userID))
}

// AudioFiles returns every audio file still referenced by an entry, so
// files of deleted entries can be removed.
func (s *Store) AudioFiles(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT audio FROM entries WHERE audio IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out[f] = true
	}
	return out, rows.Err()
}

// Entries returns one page of entry summaries, by arrival (crawl ID) or, with
// ByPublished, by publication time. The returned cursor is the last entry's
// ID, non-zero only when another page exists; with ByPublished the caller
// pairs it with that entry's PublishedAt.
func (s *Store) Entries(ctx context.Context, f Filter, withContent bool) ([]Entry, int64, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 50
	}
	where, args := f.where()
	cols := entryColumns
	if withContent {
		cols += ", e.content"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM entries e JOIN feeds d ON d.id = e.feed_id
		WHERE `+where+` ORDER BY `+f.orderBy()+` LIMIT ?`, append(args, f.Limit+1)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var extra []any
		if withContent {
			extra = append(extra, &e.Content)
		}
		if err := scanEntry(rows, &e, extra...); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next int64
	if len(out) > f.Limit {
		out = out[:f.Limit]
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

// EntryIDs returns only IDs, which is what Reader clients use to reconcile
// state; it supports much larger pages than Entries.
func (s *Store) EntryIDs(ctx context.Context, f Filter) ([]int64, int64, error) {
	if f.Limit <= 0 || f.Limit > 10000 {
		f.Limit = 1000
	}
	where, args := f.where()
	order := "DESC"
	if f.Oldest {
		order = "ASC"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id FROM entries e JOIN feeds d ON d.id = e.feed_id
		WHERE `+where+` ORDER BY e.id `+order+` LIMIT ?`, append(args, f.Limit+1)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next int64
	if len(ids) > f.Limit {
		ids = ids[:f.Limit]
		next = ids[len(ids)-1]
	}
	return ids, next, nil
}

func (s *Store) Entry(ctx context.Context, userID, id int64) (*Entry, error) {
	var e Entry
	err := scanEntry(s.db.QueryRowContext(ctx, `SELECT `+entryColumns+`, e.content FROM entries e
		JOIN feeds d ON d.id = e.feed_id WHERE e.id = ? AND d.user_id = ?`, id, userID), &e, &e.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &e, err
}

// SetState applies a read/unread/star/unstar action to entries owned by the
// user. It is idempotent: entries already in the target state are untouched,
// so their timestamps do not move. Every change is journalled. It returns
// the IDs that actually changed.
func (s *Store) SetState(ctx context.Context, userID int64, action string, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Each SET clause takes the current time exactly once (as ?1).
	var set, cond string
	switch action {
	case "read":
		set, cond = "read_at = ?1", "read_at IS NULL"
	case "unread":
		set, cond = "read_at = NULL", "read_at IS NOT NULL"
	case "star":
		set, cond = "starred_at = ?1", "starred_at IS NULL"
	case "unstar":
		set, cond = "starred_at = NULL", "starred_at IS NOT NULL"
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	args := append([]any{now, userID}, int64Args(ids)...)
	rows, err := tx.QueryContext(ctx, `UPDATE entries SET `+set+`, state_at = ?1
		WHERE feed_id IN (SELECT id FROM feeds WHERE user_id = ?2) AND id IN (`+numbered(3, len(ids))+`)
		AND `+cond+` RETURNING id`, args...)
	if err != nil {
		return nil, err
	}
	var changed []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		changed = append(changed, id)
	}
	rows.Close()
	for _, id := range changed {
		if _, err := tx.ExecContext(ctx, `INSERT INTO journal (user_id, at, action, entry_id) VALUES (?, ?, ?, ?)`,
			userID, now, action, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if s.OnStateChange != nil && len(changed) > 0 {
		s.OnStateChange(userID, action, changed)
	}
	return changed, nil
}

// MarkAllRead marks every unread entry matching f with id <= maxID as read
// and returns the IDs it changed (so the UI can offer Undo). The boundary
// keeps items that arrive while the user is deciding unread.
// SkipSponsored marks a user's unread sponsored posts read, for when they
// turn on skipping them. It returns the IDs it changed.
func (s *Store) SkipSponsored(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id FROM entries e JOIN feeds d ON d.id = e.feed_id
		WHERE d.user_id = ? AND e.sponsored = 1 AND e.read_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var changed []int64
	for len(ids) > 0 {
		n := min(len(ids), 500)
		c, err := s.SetState(ctx, userID, "read", ids[:n])
		if err != nil {
			return changed, err
		}
		changed = append(changed, c...)
		ids = ids[n:]
	}
	return changed, nil
}

func (s *Store) MarkAllRead(ctx context.Context, f Filter, maxID int64) ([]int64, error) {
	f.State, f.MaxID, f.After, f.Limit = StateUnread, maxID, 0, 0
	where, args := f.where()
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id FROM entries e JOIN feeds d ON d.id = e.feed_id WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var changed []int64
	for len(ids) > 0 {
		n := min(len(ids), 500)
		c, err := s.SetState(ctx, f.UserID, "read", ids[:n])
		if err != nil {
			return changed, err
		}
		changed = append(changed, c...)
		ids = ids[n:]
	}
	return changed, nil
}

type Counts struct {
	Unread  int   `json:"unread"`
	Starred int   `json:"starred"`
	Today   int   `json:"today"`
	Newest  int64 `json:"newest,string"` // newest entry ID (crawl µs)
}

func (s *Store) Counts(ctx context.Context, userID int64) (Counts, error) {
	var c Counts
	midnight := time.Now().Truncate(24 * time.Hour).UnixMicro()
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(e.read_at IS NULL), 0), COALESCE(SUM(e.starred_at IS NOT NULL), 0),
		       COALESCE(SUM(e.id >= ? AND e.read_at IS NULL), 0), COALESCE(MAX(e.id), 0)
		  FROM entries e JOIN feeds d ON d.id = e.feed_id WHERE d.user_id = ?`, midnight, userID).
		Scan(&c.Unread, &c.Starred, &c.Today, &c.Newest)
	return c, err
}

// Retention deletes read, unstarred entries that arrived before the cutoff
// and have not appeared in their feed since it, in bounded batches. Entries
// still listed by their feed are never deleted: they would come straight back
// as new articles with new IDs, which a Reader client sees as duplicates.
func (s *Store) Retention(ctx context.Context, olderThan time.Time) (int64, error) {
	var total int64
	for {
		res, err := s.db.ExecContext(ctx, `
			DELETE FROM entries WHERE id IN (
				SELECT id FROM entries
				 WHERE read_at IS NOT NULL AND starred_at IS NULL
				   AND id < ? AND last_seen_at < ?
				 LIMIT 500)`, olderThan.UnixMicro(), olderThan.Unix())
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 500 {
			return total, nil
		}
	}
}

type FeedUnread struct {
	FeedID int64
	Count  int
	Newest int64
}

// UnreadByFeed returns unread counts and the newest unread entry ID per feed.
func (s *Store) UnreadByFeed(ctx context.Context, userID int64) ([]FeedUnread, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.feed_id, COUNT(*), MAX(e.id) FROM entries e JOIN feeds d ON d.id = e.feed_id
		 WHERE d.user_id = ? AND e.read_at IS NULL GROUP BY e.feed_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeedUnread
	for rows.Next() {
		var u FeedUnread
		if err := rows.Scan(&u.FeedID, &u.Count, &u.Newest); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// EntriesByID returns full entries (with content) owned by the user, in the
// order given; unknown IDs are omitted.
func (s *Store) EntriesByID(ctx context.Context, userID int64, ids []int64) ([]Entry, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	entries, _, err := s.Entries(ctx, Filter{UserID: userID, IDs: ids, Limit: len(ids)}, true)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Entry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
	}
	out := make([]Entry, 0, len(entries))
	for _, id := range ids {
		if e, ok := byID[id]; ok {
			out = append(out, e)
			delete(byID, id)
		}
	}
	return out, nil
}

// ImportedEntry is an entry carried over from another reader with its
// original numeric ID, which doubles as its crawl time.
type ImportedEntry struct {
	NewEntry
	ID       int64
	Read     bool
	Starred  bool
	StateAt  int64 // seconds; when the user last changed read/star (0 = now)
	LastSeen int64 // seconds; when the source last saw it in its feed
}

// ImportEntries inserts entries keeping their IDs, and makes sure new IDs are
// allocated above them. Entries whose (feed, GUID) or ID already exist are
// skipped, so imports can be re-run.
func (s *Store) ImportEntries(ctx context.Context, feedID int64, items []ImportedEntry) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	n := 0
	var maxID int64
	for _, it := range items {
		stateAt := it.StateAt
		if stateAt <= 0 {
			stateAt = now
		}
		var readAt, starredAt any
		if it.Read {
			readAt = stateAt
		}
		if it.Starred {
			starredAt = stateAt
		}
		pub := it.PublishedAt.Unix()
		if it.PublishedAt.IsZero() {
			pub = it.ID / 1_000_000
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO entries (id, feed_id, guid, url, title, author, content, excerpt, image_url, enclosures,
			                     content_hash, published_at, updated_at, read_at, starred_at, state_at, imported, last_seen_at,
			                     search_text)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?) ON CONFLICT DO NOTHING`,
			it.ID, feedID, it.GUID, it.URL, it.Title, it.Author, it.Content, it.Excerpt, it.ImageURL,
			it.Enclosures, it.ContentHash, pub, it.ID/1_000_000, readAt, starredAt, stateAt, it.LastSeen,
			searchText(it.Content))
		if err != nil {
			return n, err
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n++
		}
		maxID = max(maxID, it.ID)
	}
	if err := tx.Commit(); err != nil {
		return n, err
	}
	s.idMu.Lock()
	s.lastID = max(s.lastID, maxID)
	s.idMu.Unlock()
	return n, nil
}

// KnownGUIDs returns which of the given GUIDs a feed already has, so a poll
// can tell new articles apart before fetching their pages.
func (s *Store) KnownGUIDs(ctx context.Context, feedID int64, guids []string) (map[string]bool, error) {
	known := map[string]bool{}
	for start := 0; start < len(guids); start += 500 {
		batch := guids[start:min(start+500, len(guids))]
		args := append([]any{feedID}, make([]any, len(batch))...)
		for i, g := range batch {
			args[i+1] = g
		}
		rows, err := s.db.QueryContext(ctx, `SELECT guid FROM entries WHERE feed_id = ? AND guid IN (`+placeholders(len(batch))+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var g string
			if err := rows.Scan(&g); err != nil {
				rows.Close()
				return nil, err
			}
			known[g] = true
		}
		rows.Close()
	}
	return known, nil
}

// SetFullText replaces an entry's content with the article fetched from its
// page, keeping the feed's own HTML so the change can be undone. An empty
// excerpt or image is filled from the article.
func (s *Store) SetFullText(ctx context.Context, userID, entryID int64, html, excerpt, imageURL string) error {
	return checkAffected(s.db.ExecContext(ctx, `
		UPDATE entries SET feed_content = COALESCE(feed_content, content), content = ?, search_text = ?, full_error = '',
		       excerpt = CASE WHEN excerpt = '' THEN ? ELSE excerpt END,
		       image_url = CASE WHEN image_url = '' THEN ? ELSE image_url END
		 WHERE id = ? AND feed_id IN (SELECT id FROM feeds WHERE user_id = ?)`,
		html, searchText(html), excerpt, imageURL, entryID, userID))
}

// SetFullError records why fetching an entry's full article failed.
func (s *Store) SetFullError(ctx context.Context, userID, entryID int64, msg string) error {
	return checkAffected(s.db.ExecContext(ctx, `
		UPDATE entries SET full_error = ? WHERE id = ? AND feed_id IN (SELECT id FROM feeds WHERE user_id = ?)`,
		msg, entryID, userID))
}

// RevertFullText puts the feed's own content back.
func (s *Store) RevertFullText(ctx context.Context, userID, entryID int64) error {
	var feedHTML sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT feed_content FROM entries
		 WHERE id = ? AND feed_id IN (SELECT id FROM feeds WHERE user_id = ?)`, entryID, userID).Scan(&feedHTML)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !feedHTML.Valid) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return checkAffected(s.db.ExecContext(ctx, `
		UPDATE entries SET content = feed_content, search_text = ?, feed_content = NULL
		 WHERE id = ? AND feed_content IS NOT NULL AND feed_id IN (SELECT id FROM feeds WHERE user_id = ?)`,
		searchText(feedHTML.String), entryID, userID))
}

// WithoutFullText lists a feed's newest entries (unread first) that still
// show the feed's own content, for fetching full articles after the
// feed's switch is turned on.
func (s *Store) WithoutFullText(ctx context.Context, feedID int64, limit int) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryColumns+` FROM entries e
		WHERE e.feed_id = ? AND e.feed_content IS NULL AND e.url <> ''
		ORDER BY e.read_at IS NOT NULL, e.id DESC LIMIT ?`, feedID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := scanEntry(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetFeedFullText turns full-article fetching on or off for a feed.
func (s *Store) SetFeedFullText(ctx context.Context, userID, feedID int64, on bool) error {
	return checkAffected(s.db.ExecContext(ctx, `UPDATE feeds SET full_text = ? WHERE id = ? AND user_id = ?`, on, feedID, userID))
}

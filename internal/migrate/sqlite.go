package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/store"
)

// Export describes a FreshRSS account as produced by
//
//	./cli/export-sqlite-for-user.php --user NAME --filename NAME.sqlite
//
// plus FreshRSS's data/config.php (for the salt) and
// data/users/NAME/config.php (for the password hashes).
type Export struct {
	SQLite       string
	SystemConfig string
	UserConfig   string
	Username     string
	// LegacyAuth keeps the token FreshRSS issued to Reeder valid, so the
	// switch needs no client reconfiguration. It can be turned off later.
	LegacyAuth bool
}

// FullReport describes what an import did, for reconciliation.
type FullReport struct {
	Folders, Feeds, Paused, Entries, Unread, Starred int
	MinID, MaxID                                     int64
	Skipped                                          []string
}

// FromSQLite imports a complete FreshRSS account, preserving feed IDs, entry
// IDs, GUIDs, timestamps and read/star state. It is idempotent: re-running it
// on the same export changes nothing.
func FromSQLite(ctx context.Context, st *store.Store, ex Export) (*FullReport, error) {
	sys, err := phpConfig(ex.SystemConfig)
	if err != nil {
		return nil, fmt.Errorf("system config: %w", err)
	}
	usr, err := phpConfig(ex.UserConfig)
	if err != nil {
		return nil, fmt.Errorf("user config: %w", err)
	}
	salt, webHash, apiHash := sys["salt"], usr["passwordHash"], usr["apiPasswordHash"]
	if salt == "" {
		return nil, errors.New("system config has no salt")
	}
	if !store.IsBcrypt(webHash) {
		return nil, errors.New("user config has no bcrypt passwordHash")
	}
	if apiHash != "" && !store.IsBcrypt(apiHash) {
		return nil, errors.New("user config apiPasswordHash is not bcrypt")
	}

	src, err := sql.Open("sqlite", "file:"+ex.SQLite+"?mode=ro&immutable=1&_pragma=temp_store(MEMORY)")
	if err != nil {
		return nil, err
	}
	defer src.Close()
	if err := src.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open export: %w", err)
	}

	uid, err := st.ImportUser(ctx, ex.Username, webHash, apiHash)
	if err != nil {
		return nil, err
	}
	if err := st.SetSetting(ctx, store.SettingFreshRSSSalt, salt); err != nil {
		return nil, err
	}
	if err := st.SetLegacyAuth(ctx, uid, ex.LegacyAuth); err != nil {
		return nil, err
	}

	rep := &FullReport{}
	folders := map[int64]int64{} // FreshRSS category ID → folder ID
	rows, err := src.QueryContext(ctx, `SELECT id, name FROM category ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("categories: %w", err)
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		// FreshRSS stores names HTML-escaped and serves them decoded; Reeder
		// knows folders by the decoded name.
		fid, err := st.EnsureFolder(ctx, uid, phpDecode(name))
		if err != nil {
			rows.Close()
			return nil, err
		}
		folders[id] = fid
		rep.Folders++
	}
	rows.Close()

	type feedRow struct {
		id, category, kind, ttl int64
		url, name, website      string
		httpAuth                string
	}
	var feeds []feedRow
	rows, err = src.QueryContext(ctx, `SELECT id, url, COALESCE(category, 0), COALESCE(kind, 0), name,
		COALESCE(website, ''), COALESCE(httpAuth, ''), COALESCE(ttl, 0) FROM feed ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("feeds: %w", err)
	}
	for rows.Next() {
		var f feedRow
		if err := rows.Scan(&f.id, &f.url, &f.category, &f.kind, &f.name, &f.website, &f.httpAuth, &f.ttl); err != nil {
			rows.Close()
			return nil, err
		}
		feeds = append(feeds, f)
	}
	rows.Close()

	for _, f := range feeds {
		switch {
		case !isHTTP(f.url):
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("feed %d %q: not an http(s) address", f.id, f.name))
			continue
		case f.kind != 0:
			// XPath / JSON scraping sources are not supported; keep the
			// history but the feed will report errors when polled.
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("feed %d %q: FreshRSS scraping source (kind %d) is imported but cannot be polled", f.id, f.name, f.kind))
		case f.httpAuth != "":
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("feed %d %q: HTTP credentials are not migrated; re-add them", f.id, f.name))
		}
		var folderID *int64
		if fid, ok := folders[f.category]; ok {
			folderID = &fid
		}
		site := f.website
		if !isHTTP(site) {
			site = ""
		}
		got, err := st.ImportFeed(ctx, uid, f.id, f.url, phpDecode(f.name), site, folderID)
		if err != nil {
			return rep, fmt.Errorf("feed %d: %w", f.id, err)
		}
		if got != f.id {
			return rep, fmt.Errorf("feed %d imported as %d: IDs must be preserved for Reeder", f.id, got)
		}
		// FreshRSS keeps a per-feed refresh interval in ttl; a negative value
		// means automatic refresh is off. Keep both, so a paused feed stays
		// paused instead of suddenly delivering a backlog.
		interval := f.ttl
		if interval < 0 {
			interval = -interval
		}
		if err := st.SetFeedSchedule(ctx, uid, f.id, interval, f.ttl < 0); err != nil {
			return rep, err
		}
		if f.ttl < 0 {
			rep.Paused++
		}
		rep.Feeds++

		n, unread, starred, minID, maxID, err := importFeedEntries(ctx, src, st, f.id)
		if err != nil {
			return rep, fmt.Errorf("entries of feed %d: %w", f.id, err)
		}
		rep.Entries += n
		rep.Unread += unread
		rep.Starred += starred
		if minID > 0 && (rep.MinID == 0 || minID < rep.MinID) {
			rep.MinID = minID
		}
		rep.MaxID = max(rep.MaxID, maxID)
	}
	if err := st.SetSetting(ctx, store.SettingMigratedAt, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return rep, err
	}
	if err := st.SetSetting(ctx, store.SettingMaxLegacyID, fmt.Sprint(rep.MaxID)); err != nil {
		return rep, err
	}
	return rep, nil
}

func importFeedEntries(ctx context.Context, src *sql.DB, st *store.Store, feedID int64) (n, unread, starred int, minID, maxID int64, err error) {
	rows, err := src.QueryContext(ctx, `SELECT id, guid, title, COALESCE(author, ''), COALESCE(content, ''), link,
		COALESCE(date, 0), COALESCE(lastUserModified, 0), COALESCE(lastSeen, 0), is_read, is_favorite, COALESCE(attributes, '')
		FROM entry WHERE id_feed = ? ORDER BY id`, feedID)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	defer rows.Close()
	var batch []store.ImportedEntry
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		c, err := st.ImportEntries(ctx, feedID, batch)
		n += c
		batch = batch[:0]
		return err
	}
	for rows.Next() {
		var (
			e                  store.ImportedEntry
			title, author, raw string
			link, attrs        string
			date, userModified int64
			isRead, isFavorite bool
		)
		if err := rows.Scan(&e.ID, &e.GUID, &title, &author, &raw, &link, &date, &userModified, &e.LastSeen, &isRead, &isFavorite, &attrs); err != nil {
			return n, unread, starred, minID, maxID, err
		}
		// FreshRSS stores these with htmlspecialchars() applied and decodes
		// them when serving; GUIDs must match what a fresh poll yields, or
		// articles would be duplicated after cutover.
		if title == e.GUID {
			title = untitled(raw, e.GUID)
		}
		e.GUID, link = phpDecode(e.GUID), phpDecode(link)
		if !isHTTP(link) {
			link = ""
		}
		body := content.Process(raw, link)
		e.URL = link
		e.Title = phpDecode(title) // plain text, rendered as text; whitespace kept as FreshRSS has it
		e.Author = phpDecode(author)
		e.Content = body.HTML
		e.Excerpt = content.Excerpt(body.Text, 320)
		e.ImageURL = body.ImageURL
		e.Enclosures = enclosures(attrs)
		if date > 0 {
			e.PublishedAt = time.Unix(date, 0)
		}
		e.Read, e.Starred, e.StateAt = isRead, isFavorite, userModified
		if !isRead {
			unread++
		}
		if isFavorite {
			starred++
		}
		if minID == 0 || e.ID < minID {
			minID = e.ID
		}
		maxID = max(maxID, e.ID)
		batch = append(batch, e)
		if len(batch) == 500 {
			if err := flush(); err != nil {
				return n, unread, starred, minID, maxID, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return n, unread, starred, minID, maxID, err
	}
	return n, unread, starred, minID, maxID, flush()
}

// enclosures extracts FreshRSS's attributes.enclosures as our JSON shape.
func enclosures(attrs string) string {
	var a struct {
		Enclosures []struct {
			URL    string `json:"url"`
			Type   string `json:"type"`
			Length int64  `json:"length"`
		} `json:"enclosures"`
	}
	if attrs == "" || json.Unmarshal([]byte(attrs), &a) != nil {
		return "[]"
	}
	out := []store.Enclosure{}
	for _, en := range a.Enclosures {
		if isHTTP(en.URL) {
			out = append(out, store.Enclosure{URL: en.URL, Type: en.Type, Length: en.Length})
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

var phpString = regexp.MustCompile(`'([A-Za-z0-9_]+)'\s*=>\s*'((?:[^'\\]|\\.)*)'`)

// phpConfig reads the top-level string values of a FreshRSS config.php
// (a var_export'ed array). The file is parsed, never executed.
func phpConfig(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, m := range phpString.FindAllStringSubmatch(string(b), -1) {
		if _, seen := out[m[1]]; seen {
			continue // first (top-level) occurrence wins
		}
		out[m[1]] = strings.NewReplacer(`\\`, `\`, `\'`, `'`).Replace(m[2])
	}
	return out, nil
}

// phpDecode reverses PHP's htmlspecialchars(ENT_QUOTES), exactly as
// htmlspecialchars_decode does: only these entities, nothing else touched.
var phpDecode = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#039;", "'", "&#39;", "'", "&apos;", "'", "&amp;", "&").Replace

// untitled reproduces FreshRSS's Entry::title() for articles without a
// title, which it stores with title = guid: the first 75 characters of the
// tag-stripped content, plus "…" if there is more; the GUID if empty.
func untitled(rawContent, guid string) string {
	text := phpTrim(stripTags.ReplaceAllString(rawContent, ""))
	runes := []rune(text)
	title := phpTrim(string(runes[:min(len(runes), 75)]))
	if title == "" {
		return guid
	}
	if len(text) > len(title) {
		title += "…"
	}
	return title
}

var stripTags = regexp.MustCompile(`(?s)<!--.*?-->|<[^>]*>`)

// phpTrim trims what PHP's trim() trims: ASCII whitespace and NUL only.
func phpTrim(s string) string { return strings.Trim(s, " \t\n\r\x00\x0b") }

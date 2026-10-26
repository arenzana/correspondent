package migrate

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/arenzana/correspondent/internal/store"
)

// freshExport writes a FreshRSS-shaped SQLite export (the columns the
// importer reads, as in FreshRSS 1.30's install.sql.sqlite.php) and its two
// config files, and returns an Export pointing at them.
func freshExport(t *testing.T) (Export, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "u.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE category (id INTEGER PRIMARY KEY, name VARCHAR(191) NOT NULL);
CREATE TABLE feed (id INTEGER PRIMARY KEY, url TEXT NOT NULL, kind SMALLINT DEFAULT 0, category INTEGER DEFAULT 0,
  name TEXT NOT NULL, website TEXT, httpAuth TEXT, ttl INT NOT NULL DEFAULT 0);
CREATE TABLE entry (id BIGINT NOT NULL PRIMARY KEY, guid TEXT NOT NULL, title TEXT NOT NULL, author TEXT, content TEXT,
  link TEXT NOT NULL, date BIGINT, lastSeen BIGINT, lastUserModified BIGINT, is_read BOOLEAN NOT NULL DEFAULT 0,
  is_favorite BOOLEAN NOT NULL DEFAULT 0, id_feed INTEGER, attributes TEXT);
INSERT INTO category VALUES (1, 'Uncategorized'), (5, 'Notes &amp; Logs');
INSERT INTO feed VALUES
  (2, 'https://a.example/feed', 0, 5, 'A &amp; B', 'https://a.example/', NULL, -7200),
  (9, 'https://b.example/feed', 0, 1, 'B', 'javascript:alert(1)', 'user:pass', 0),
  (11, 'https://c.example/page', 2, 1, 'Scraped', '', NULL, 0),
  (12, 'file:///etc/passwd', 0, 1, 'Bad', '', NULL, 0);
INSERT INTO entry VALUES
  (1700000000000001, 'a-1', 'Read &amp; starred', 'Ann', '<p onclick="x()">Hi<script>alert(1)</script></p>',
   'https://a.example/1', 1699990000, 1700000400, 1700000500, 1, 1, 2,
   '{"enclosures":[{"url":"https://a.example/1.mp3","type":"audio/mpeg","length":99},{"url":"javascript:x","type":"audio/mpeg"}]}'),
  (1700000000000002, 'a-2', 'Unread', '', '<p>Body</p>', 'javascript:alert(2)', 1699990100, 0, 0, 0, 0, 2, NULL),
  (1700000000000003, 'https://b.example/1?a=1&amp;b=2', 'Other  feed', '', '', 'https://b.example/1?a=1&amp;b=2', 1699990200, 0, 0, 0, 1, 9, '');`)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := bcrypt.GenerateFromPassword([]byte("api-password-1"), 10)
	apiHash := "$2y$" + string(h[4:])
	w, _ := bcrypt.GenerateFromPassword([]byte("web-password-1"), 10)
	webHash := "$2y$" + string(w[4:])
	os.WriteFile(filepath.Join(dir, "config.php"), []byte(`<?php
return array (
  'environment' => 'production',
  'salt' => 'S4lt\'with\\quote',
  'db' => array ( 'type' => 'sqlite', 'salt' => 'not-this-one' ),
);`), 0o600)
	os.WriteFile(filepath.Join(dir, "user.php"), []byte(`<?php
return array (
  'passwordHash' => '`+webHash+`',
  'apiPasswordHash' => '`+apiHash+`',
);`), 0o600)
	return Export{
		SQLite: filepath.Join(dir, "u.sqlite"), SystemConfig: filepath.Join(dir, "config.php"),
		UserConfig: filepath.Join(dir, "user.php"), Username: "reader", LegacyAuth: true,
	}, apiHash
}

func TestFromSQLite(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ex, apiHash := freshExport(t)

	rep, err := FromSQLite(ctx, st, ex)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Feeds != 3 || rep.Entries != 3 || rep.Unread != 2 || rep.Starred != 2 || rep.MaxID != 1700000000000003 {
		t.Fatalf("report %+v", rep)
	}
	notes := strings.Join(rep.Skipped, "\n")
	for _, want := range []string{`"Bad": not an http(s) address`, "HTTP credentials", "scraping"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes missing %q:\n%s", want, notes)
		}
	}

	uid, _ := st.UserIDByName(ctx, "reader")
	// Folder and feed names are decoded, as FreshRSS's API serves them.
	folders, _ := st.Folders(ctx, uid)
	var names []string
	for _, f := range folders {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "Notes & Logs,Uncategorized" {
		t.Fatalf("folders %v", names)
	}
	f, err := st.Feed(ctx, uid, 2)
	if err != nil || f.Title != "A & B" {
		t.Fatalf("feed 2: %+v %v", f, err)
	}
	if !f.Paused || f.IntervalSec != 7200 || rep.Paused != 1 {
		t.Fatalf("FreshRSS ttl -7200 must import as paused with a 2h interval: paused=%v interval=%d", f.Paused, f.IntervalSec)
	}
	if due, _ := st.DueFeeds(ctx, time.Now(), 100); slices.ContainsFunc(due, func(d store.Feed) bool { return d.ID == 2 }) {
		t.Fatal("paused feed scheduled for polling")
	}
	if f9, _ := st.Feed(ctx, uid, 9); f9.SiteURL != "" {
		t.Fatalf("javascript: site URL kept: %q", f9.SiteURL)
	}

	e, err := st.Entry(ctx, uid, 1700000000000001)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Read || !e.Starred || e.Title != "Read & starred" || e.PublishedAt != 1699990000 || e.GUID != "a-1" {
		t.Fatalf("entry 1: %+v", e)
	}
	if strings.Contains(e.Content, "script") || strings.Contains(e.Content, "onclick") {
		t.Fatalf("content not sanitized: %s", e.Content)
	}
	if !strings.Contains(e.Enclosures, "1.mp3") || strings.Contains(e.Enclosures, "javascript") {
		t.Fatalf("enclosures: %s", e.Enclosures)
	}
	if e2, _ := st.Entry(ctx, uid, 1700000000000002); e2.URL != "" || e2.Read {
		t.Fatalf("entry 2: %+v", e2)
	}

	// Both FreshRSS passwords work, and the token FreshRSS issued is accepted.
	if _, err := st.CheckPassword(ctx, "reader", "web-password-1", false); err != nil {
		t.Fatal("web password from FreshRSS rejected")
	}
	if _, err := st.CheckPassword(ctx, "reader", "api-password-1", true); err != nil {
		t.Fatal("API password from FreshRSS rejected")
	}
	sum := sha1.Sum([]byte(`S4lt'with\quote` + "reader" + apiHash))
	if _, tok, _ := st.LegacyToken(ctx, "reader"); tok != hex.EncodeToString(sum[:]) {
		t.Fatalf("legacy token %q does not match FreshRSS's", tok)
	}

	// Idempotent: a second run adds nothing.
	rep2, err := FromSQLite(ctx, st, ex)
	if err != nil || rep2.Entries != 0 {
		t.Fatalf("second run: %+v %v", rep2, err)
	}

	// After cutover, a polled item with an unknown GUID but the same link as
	// an imported entry is recognised instead of duplicated.
	// FreshRSS stored the GUID and link escaped; a fresh poll yields them
	// decoded and must match, not duplicate.
	ins, _, err := st.UpsertEntries(ctx, 9, []store.NewEntry{{GUID: "https://b.example/1?a=1&b=2", URL: "https://b.example/1?a=1&b=2",
		Title: "Other feed", Enclosures: "[]", PublishedAt: time.Unix(1699990200, 0)}}, time.Time{})
	if err != nil || ins != 0 {
		t.Fatalf("escaped GUID re-polled as a new article: inserted %d (%v)", ins, err)
	}
	if e3, _ := st.Entry(ctx, uid, 1700000000000003); e3.Title != "Other  feed" || e3.URL != "https://b.example/1?a=1&b=2" {
		t.Fatalf("entry 3: title %q url %q", e3.Title, e3.URL)
	}
	// The first poll after import keeps FreshRSS's version (what Reeder has
	// cached) even if the feed now differs; later edits update as usual.
	edited := []store.NewEntry{{GUID: "a-1", URL: "https://a.example/1", Title: "Edited upstream", Enclosures: "[]", ContentHash: "h1"}}
	st.UpsertEntries(ctx, 2, edited, time.Time{})
	if e1, _ := st.Entry(ctx, uid, 1700000000000001); e1.Title != "Read & starred" {
		t.Fatalf("first poll after import rewrote the title to %q", e1.Title)
	}
	edited[0].ContentHash, edited[0].Title = "h2", "Edited again"
	if _, upd, _ := st.UpsertEntries(ctx, 2, edited, time.Time{}); upd != 1 {
		t.Fatal("a later edit was not applied")
	}
	// New items get IDs above everything imported.
	st.UpsertEntries(ctx, 9, []store.NewEntry{{GUID: "fresh", Enclosures: "[]"}}, time.Time{})
	ids, _, _ := st.EntryIDs(ctx, store.Filter{UserID: uid, FeedID: 9})
	if len(ids) != 2 || ids[0] <= 1700000000000003 {
		t.Fatalf("ids %v", ids)
	}
}

func TestFromSQLiteRejectsBadConfig(t *testing.T) {
	ctx := context.Background()
	st, _ := store.Open(ctx, filepath.Join(t.TempDir(), "c.db"))
	defer st.Close()
	ex, _ := freshExport(t)
	os.WriteFile(ex.UserConfig, []byte(`<?php return array ('passwordHash' => 'plaintext');`), 0o600)
	if _, err := FromSQLite(ctx, st, ex); err == nil {
		t.Fatal("non-bcrypt password hash accepted")
	}
}

func TestPlanRollback(t *testing.T) {
	ctx := context.Background()
	st, _ := store.Open(ctx, filepath.Join(t.TempDir(), "c.db"))
	defer st.Close()
	ex, _ := freshExport(t)
	if _, err := FromSQLite(ctx, st, ex); err != nil {
		t.Fatal(err)
	}
	uid, _ := st.UserIDByName(ctx, "reader")
	since := time.Now().Add(-time.Second)
	st.SetState(ctx, uid, "read", []int64{1700000000000002})
	st.SetState(ctx, uid, "unread", []int64{1700000000000002}) // net: unread
	st.SetState(ctx, uid, "unstar", []int64{1700000000000001})
	st.UpsertEntries(ctx, 9, []store.NewEntry{{GUID: "new", Enclosures: "[]"}}, time.Time{})
	newIDs, _, _ := st.EntryIDs(ctx, store.Filter{UserID: uid, FeedID: 9, Limit: 1})
	st.SetState(ctx, uid, "star", newIDs)

	p, err := PlanRollback(ctx, st, uid, since, 1700000000000003)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Read) != 0 || len(p.Unread) != 1 || len(p.Unstar) != 1 || len(p.Star) != 0 || p.Skipped != 1 {
		t.Fatalf("plan %+v", p)
	}
}

func TestUntitled(t *testing.T) {
	long := "<p>This year I spent a bit of time playing with Python, after\nhaving mostly ignored it for a decade.</p>"
	if got := untitled(long, "g"); got != "This year I spent a bit of time playing with Python, after\nhaving mostly ig…" {
		t.Fatalf("untitled = %q", got)
	}
	if got := untitled("<p>Short</p>", "g"); got != "Short" {
		t.Fatalf("short = %q", got)
	}
	if got := untitled("<img src=x>", "the-guid"); got != "the-guid" {
		t.Fatalf("empty = %q", got)
	}
}

func TestPHPDecodeAndRedact(t *testing.T) {
	if got := phpDecode(`Tom &amp; Jerry &lt;3 &quot;x&quot; &#039;y&#039; &eacute; &amp;amp;`); got != `Tom & Jerry <3 "x" 'y' &eacute; &amp;` {
		t.Fatalf("phpDecode = %q", got)
	}
	got := redactURL("https://paid.example/a?access_token=SECRET&p=1#frag")
	if strings.Contains(got, "SECRET") || !strings.HasPrefix(got, "https://paid.example/a?…") {
		t.Fatalf("redactURL = %q", got)
	}
}

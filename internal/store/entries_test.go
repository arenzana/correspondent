package store

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// fixture creates a user with one feed holding n entries published an hour
// apart, oldest first, and returns their IDs newest first.
func fixture(t *testing.T, st *Store, name string, n int) (uid, feedID int64, ids []int64) {
	t.Helper()
	ctx := context.Background()
	uid, err := st.CreateUser(ctx, name, "password-"+name)
	if err != nil {
		t.Fatal(err)
	}
	feedID, _ = st.AddFeed(ctx, uid, "https://"+name+".example/feed", name, nil)
	base := time.Now().Add(-time.Duration(n) * time.Hour)
	var items []NewEntry
	for i := range n {
		items = append(items, NewEntry{GUID: fmt.Sprint(i), Title: fmt.Sprint("post ", i), Content: "<p>x</p>",
			Enclosures: "[]", ContentHash: fmt.Sprint(i), PublishedAt: base.Add(time.Duration(i) * time.Hour)})
	}
	if ins, _, err := st.UpsertEntries(ctx, feedID, items, time.Time{}); err != nil || ins != n {
		t.Fatalf("upsert: %d %v", ins, err)
	}
	ids, _, err = st.EntryIDs(ctx, Filter{UserID: uid})
	if err != nil {
		t.Fatal(err)
	}
	return uid, feedID, ids
}

func TestUpsertIdentityAndUpdates(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, feedID, ids := fixture(t, st, "a", 3)

	// Re-polling identical items is a no-op.
	same := []NewEntry{{GUID: "0", Title: "post 0", Content: "<p>x</p>", Enclosures: "[]", ContentHash: "0"}}
	if ins, upd, _ := st.UpsertEntries(ctx, feedID, same, time.Time{}); ins != 0 || upd != 0 {
		t.Fatalf("identical re-poll: inserted %d updated %d", ins, upd)
	}
	// A changed hash updates in place, keeping ID and read state.
	st.SetState(ctx, uid, "read", []int64{ids[2]})
	changed := []NewEntry{{GUID: "0", Title: "post 0, revised", Content: "<p>y</p>", Enclosures: "[]", ContentHash: "0b"}}
	if ins, upd, _ := st.UpsertEntries(ctx, feedID, changed, time.Time{}); ins != 0 || upd != 1 {
		t.Fatalf("changed re-poll: inserted %d updated %d", ins, upd)
	}
	e, err := st.Entry(ctx, uid, ids[2])
	if err != nil || e.Title != "post 0, revised" || !e.Read {
		t.Fatalf("updated entry: %+v %v", e, err)
	}
}

func TestBatchOrderFollowsPublication(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	feedID, _ := st.AddFeed(ctx, uid, "https://x.example/f", "x", nil)
	now := time.Now()
	// Feeds list newest first; IDs must still put the newest on top.
	items := []NewEntry{
		{GUID: "new", Title: "new", Enclosures: "[]", PublishedAt: now.Add(-time.Hour)},
		{GUID: "old", Title: "old", Enclosures: "[]", PublishedAt: now.Add(-48 * time.Hour)},
	}
	st.UpsertEntries(ctx, feedID, items, time.Time{})
	list, _, _ := st.Entries(ctx, Filter{UserID: uid}, false)
	if list[0].Title != "new" || list[1].Title != "old" {
		t.Fatalf("order: %s, %s", list[0].Title, list[1].Title)
	}
}

func TestReadBeforeAndDateFallbacks(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	feedID, _ := st.AddFeed(ctx, uid, "https://x.example/f", "x", nil)
	now := time.Now()
	items := []NewEntry{
		{GUID: "ancient", Enclosures: "[]", PublishedAt: now.AddDate(-1, 0, 0)},
		{GUID: "recent", Enclosures: "[]", PublishedAt: now.Add(-time.Hour)},
		{GUID: "undated", Enclosures: "[]"},
		{GUID: "future", Enclosures: "[]", PublishedAt: now.AddDate(1, 0, 0)},
	}
	st.UpsertEntries(ctx, feedID, items, now.AddDate(0, 0, -14))
	unread, _, _ := st.Entries(ctx, Filter{UserID: uid, State: StateUnread}, false)
	if len(unread) != 2 {
		t.Fatalf("unread = %d, want 2 (the year-old and the undated item start read)", len(unread))
	}
	for _, e := range unread {
		if e.PublishedAt > now.Add(time.Minute).Unix() {
			t.Errorf("future date not clamped: %d", e.PublishedAt)
		}
	}
}

func TestSetStateIsIdempotentAndJournalled(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _, ids := fixture(t, st, "a", 2)
	for range 2 {
		st.SetState(ctx, uid, "star", ids[:1])
	}
	changed, err := st.SetState(ctx, uid, "star", ids)
	if err != nil || !slices.Equal(changed, ids[1:]) {
		t.Fatalf("second star changed %v, want only %v (%v)", changed, ids[1:], err)
	}
	var n int
	st.db.QueryRow(`SELECT COUNT(*) FROM journal WHERE action = 'star'`).Scan(&n)
	if n != 2 {
		t.Fatalf("journal rows = %d, want 2 (no-ops are not journalled)", n)
	}
	if _, err := st.SetState(ctx, uid, "explode", ids); err == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestSetStateScopedToUser(t *testing.T) {
	st, ctx := open(t), context.Background()
	_, _, aliceIDs := fixture(t, st, "alice", 1)
	bob, _, _ := fixture(t, st, "bob", 1)
	if changed, _ := st.SetState(ctx, bob, "read", aliceIDs); len(changed) != 0 {
		t.Fatal("bob changed alice's entry")
	}
	if _, err := st.Entry(ctx, bob, aliceIDs[0]); err != ErrNotFound {
		t.Fatalf("bob read alice's entry: %v", err)
	}
}

func TestMarkAllReadBoundary(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _, ids := fixture(t, st, "a", 5) // newest first
	changed, err := st.MarkAllRead(ctx, Filter{UserID: uid}, ids[2])
	if err != nil || len(changed) != 3 {
		t.Fatalf("changed %d %v", len(changed), err)
	}
	unread, _, _ := st.EntryIDs(ctx, Filter{UserID: uid, State: StateUnread})
	if !slices.Equal(unread, ids[:2]) {
		t.Fatalf("unread after boundary = %v, want %v", unread, ids[:2])
	}
}

func TestKeysetPagination(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _, ids := fixture(t, st, "a", 7)
	var got []int64
	cursor := int64(0)
	for {
		page, next, err := st.Entries(ctx, Filter{UserID: uid, Limit: 3, After: cursor}, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			got = append(got, e.ID)
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	if !slices.Equal(got, ids) {
		t.Fatalf("paged %v, want %v", got, ids)
	}
	// Oldest first walks the same set in reverse.
	asc, _, _ := st.EntryIDs(ctx, Filter{UserID: uid, Oldest: true})
	slices.Reverse(asc)
	if !slices.Equal(asc, ids) {
		t.Fatal("oldest-first order mismatch")
	}
}

func TestSearchIsSafeAgainstFTSSyntax(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _, _ := fixture(t, st, "a", 3)
	for _, q := range []string{`post`, `"post`, `post OR`, `NEAR(`, `*`, `title:post`, `-`} {
		if _, _, err := st.Entries(ctx, Filter{UserID: uid, Query: q}, false); err != nil {
			t.Errorf("query %q: %v", q, err)
		}
	}
	res, _, _ := st.Entries(ctx, Filter{UserID: uid, Query: "pos"}, false)
	if len(res) != 3 {
		t.Fatalf("prefix search found %d, want 3", len(res))
	}
}

func TestImportKeepsIDsAndAllocatesAbove(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	feedID, err := st.ImportFeed(ctx, uid, 82, "https://x.example/f", "x", "", nil)
	if err != nil || feedID != 82 {
		t.Fatalf("feed id %d %v", feedID, err)
	}
	future := time.Now().Add(time.Hour).UnixMicro() // an imported ID ahead of the clock
	n, err := st.ImportEntries(ctx, feedID, []ImportedEntry{{ID: future, Starred: true, NewEntry: NewEntry{GUID: "g", Enclosures: "[]"}}})
	if err != nil || n != 1 {
		t.Fatalf("import: %d %v", n, err)
	}
	if n, _ := st.ImportEntries(ctx, feedID, []ImportedEntry{{ID: future, NewEntry: NewEntry{GUID: "g", Enclosures: "[]"}}}); n != 0 {
		t.Fatal("re-import inserted a duplicate")
	}
	st.UpsertEntries(ctx, feedID, []NewEntry{{GUID: "fresh", Enclosures: "[]"}}, time.Time{})
	ids, _, _ := st.EntryIDs(ctx, Filter{UserID: uid})
	if len(ids) != 2 || ids[0] <= future {
		t.Fatalf("new ID %d not above imported %d", ids[0], future)
	}
	if _, err := st.ImportFeed(ctx, uid, 82, "https://other.example/f", "y", "", nil); err == nil {
		t.Fatal("ID collision with a different URL accepted")
	}
}

func TestRetentionKeepsWhatFeedsStillList(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, feedID, ids := fixture(t, st, "a", 4) // all seen just now
	st.SetState(ctx, uid, "read", ids)
	st.SetState(ctx, uid, "star", ids[3:]) // the oldest is starred

	// A cutoff after every entry arrived; all of them were seen in the feed
	// after it, so none may go.
	future := time.Now().Add(time.Hour)
	st.db.Exec(`UPDATE entries SET last_seen_at = ?`, future.Add(time.Hour).Unix())
	if n, _ := st.Retention(ctx, future); n != 0 {
		t.Fatalf("deleted %d entries still listed by their feed", n)
	}
	// Two entries leave the feed: a later poll lists only the others.
	st.db.Exec(`UPDATE entries SET last_seen_at = 1 WHERE id IN (?, ?, ?)`, ids[1], ids[2], ids[3])
	n, err := st.Retention(ctx, future)
	if err != nil || n != 2 {
		t.Fatalf("deleted %d (%v), want 2: unstarred, read, and gone from the feed", n, err)
	}
	left, _, _ := st.EntryIDs(ctx, Filter{UserID: uid})
	if !slices.Equal(left, []int64{ids[0], ids[3]}) {
		t.Fatalf("kept %v", left)
	}
	// A poll marks listed entries as seen.
	st.db.Exec(`UPDATE entries SET last_seen_at = 1 WHERE id = ?`, ids[0])
	st.UpsertEntries(ctx, feedID, []NewEntry{{GUID: "0", Title: "post 0", Content: "<p>x</p>", Enclosures: "[]", ContentHash: "0"}}, time.Time{})
	var seen int64
	st.db.QueryRow(`SELECT last_seen_at FROM entries WHERE guid = '0'`).Scan(&seen)
	if seen < time.Now().Add(-time.Minute).Unix() {
		t.Fatalf("poll did not refresh last_seen_at: %d", seen)
	}
}

func TestCounts(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _, ids := fixture(t, st, "a", 4)
	st.SetState(ctx, uid, "read", ids[:1])
	st.SetState(ctx, uid, "star", ids[:2])
	c, err := st.Counts(ctx, uid)
	if err != nil || c.Unread != 3 || c.Starred != 2 || c.Newest != ids[0] {
		t.Fatalf("counts %+v %v", c, err)
	}
}

func TestPublishedOrderAndCursor(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	a, _ := st.AddFeed(ctx, uid, "https://a.example/f", "a", nil)
	b, _ := st.AddFeed(ctx, uid, "https://b.example/f", "b", nil)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	// Feed b arrives later but its posts are older: arrival and publication disagree.
	st.UpsertEntries(ctx, a, []NewEntry{{GUID: "a25", Title: "a25", Enclosures: "[]", PublishedAt: day(25)}}, time.Time{})
	st.UpsertEntries(ctx, b, []NewEntry{
		{GUID: "b13", Title: "b13", Enclosures: "[]", PublishedAt: day(13)},
		{GUID: "b23", Title: "b23", Enclosures: "[]", PublishedAt: day(23)},
		{GUID: "b23x", Title: "b23x", Enclosures: "[]", PublishedAt: day(23)}, // same timestamp: tie broken by ID
	}, time.Time{})

	byArrival, _, _ := st.Entries(ctx, Filter{UserID: uid}, false)
	if byArrival[0].Title == "a25" {
		t.Fatal("test setup: arrival order should put feed b first")
	}
	var got []string
	f := Filter{UserID: uid, ByPublished: true, Limit: 2}
	for {
		page, next, err := st.Entries(ctx, f, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			got = append(got, e.Title)
		}
		if next == 0 {
			break
		}
		f.After, f.AfterPub = next, page[len(page)-1].PublishedAt
	}
	want := []string{"a25", "b23x", "b23", "b13"}
	if !slices.Equal(got, want) {
		t.Fatalf("published order %v, want %v", got, want)
	}
}

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/store"
)

// TestSavedSearchesAPI saves a search, sees it with its unread count in the
// tree, marks only its matches read, renames and removes it; another
// reader can't touch it.
func TestSavedSearchesAPI(t *testing.T) {
	e := newAIEnv(t)
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	ctx := context.Background()
	uid, _ := e.store.UserIDByName(ctx, "alice")
	fid, _ := e.store.AddFeed(ctx, uid, "https://x.example/f", "x", nil)
	e.store.UpsertEntries(ctx, fid, []store.NewEntry{
		{GUID: "a", Title: "Duo review", Content: "<p>The iPhone Duo folds.</p>", Enclosures: "[]", PublishedAt: time.Now()},
		{GUID: "b", Title: "Ships", Content: "<p>Harbours.</p>", Enclosures: "[]", PublishedAt: time.Now()},
	}, time.Time{})
	type tree struct {
		Searches []store.Search
		Counts   struct {
			Unread int
			Newest string
		}
	}

	if r := e.call(t, alice, "POST", "/api/v1/searches", map[string]string{"query": "  "}); r.StatusCode != http.StatusBadRequest {
		t.Errorf("empty query: %d", r.StatusCode)
	}
	saved := decodeInto[struct{ Search store.Search }](t, e.call(t, alice, "POST", "/api/v1/searches", map[string]string{"query": "duo"})).Search
	if saved.Name != "duo" {
		t.Errorf("default name = %q", saved.Name)
	}
	tr := decodeInto[tree](t, e.call(t, alice, "GET", "/api/v1/tree", nil))
	if len(tr.Searches) != 1 || tr.Searches[0].Unread != 1 {
		t.Fatalf("tree searches = %+v", tr.Searches)
	}

	// Mark all read in the search: only its match is read.
	e.call(t, alice, "POST", "/api/v1/entries/mark-all-read", map[string]string{"q": "duo", "before": tr.Counts.Newest})
	tr = decodeInto[tree](t, e.call(t, alice, "GET", "/api/v1/tree", nil))
	if tr.Searches[0].Unread != 0 || tr.Counts.Unread != 1 {
		t.Errorf("after mark all read: search %d unread, %d unread in all", tr.Searches[0].Unread, tr.Counts.Unread)
	}

	path := "/api/v1/searches/" + itoa(saved.ID)
	if r := e.call(t, bob, "PATCH", path, map[string]string{"name": "mine"}); r.StatusCode != http.StatusNotFound {
		t.Errorf("another reader renames: %d", r.StatusCode)
	}
	if r := e.call(t, alice, "PATCH", path, map[string]string{"name": "Foldables"}); r.StatusCode != http.StatusNoContent {
		t.Errorf("rename: %d", r.StatusCode)
	}
	if r := e.call(t, bob, "DELETE", path, nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("another reader deletes: %d", r.StatusCode)
	}
	if r := e.call(t, alice, "DELETE", path, nil); r.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", r.StatusCode)
	}
	if tr := decodeInto[tree](t, e.call(t, alice, "GET", "/api/v1/tree", nil)); len(tr.Searches) != 0 {
		t.Errorf("still listed: %+v", tr.Searches)
	}
}

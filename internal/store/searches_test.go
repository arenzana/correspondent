package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSavedSearches(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	other, _ := st.CreateUser(ctx, "v", "password-123")
	feedID, _ := st.AddFeed(ctx, uid, "https://x.example/f", "x", nil)
	st.UpsertEntries(ctx, feedID, []NewEntry{
		{GUID: "a", Title: "Duo review", Content: "<p>The iPhone Duo folds.</p>", Enclosures: "[]", PublishedAt: time.Now()},
		{GUID: "b", Title: "Gardens", Content: "<p>Roses and the Duo hinge.</p>", Enclosures: "[]", PublishedAt: time.Now()},
		{GUID: "c", Title: "Ships", Content: "<p>Harbours.</p>", Enclosures: "[]", PublishedAt: time.Now()},
	}, time.Time{})

	duo, err := st.AddSearch(ctx, uid, " iPhone Duo ", "duo")
	if err != nil || duo.Name != "iPhone Duo" {
		t.Fatalf("add: %+v %v", duo, err)
	}
	list, _ := st.Searches(ctx, uid)
	if len(list) != 1 || list[0].Unread != 2 {
		t.Fatalf("searches = %+v", list)
	}
	// Reading one lowers the count.
	ids, _, _ := st.EntryIDs(ctx, Filter{UserID: uid, Query: "folds"})
	st.SetState(ctx, uid, "read", ids)
	if list, _ := st.Searches(ctx, uid); list[0].Unread != 1 {
		t.Errorf("after reading: %+v", list)
	}
	// The same query again renames; another reader can't touch it.
	if again, _ := st.AddSearch(ctx, uid, "Duo", "duo"); again.ID != duo.ID || again.Name != "Duo" {
		t.Errorf("re-save = %+v", again)
	}
	if err := st.RenameSearch(ctx, other, duo.ID, "mine"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename by another reader: %v", err)
	}
	if err := st.DeleteSearch(ctx, other, duo.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete by another reader: %v", err)
	}
	st.RenameSearch(ctx, uid, duo.ID, "Foldables")
	if list, _ := st.Searches(ctx, uid); list[0].Name != "Foldables" {
		t.Errorf("rename: %+v", list)
	}
	// A reader keeps at most MaxSearches.
	for i := 1; i < MaxSearches; i++ {
		if _, err := st.AddSearch(ctx, uid, "s", fmt.Sprintf("word%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AddSearch(ctx, uid, "s", "one too many"); !errors.Is(err, ErrTooManySearches) {
		t.Errorf("over the limit: %v", err)
	}
	if err := st.DeleteSearch(ctx, uid, duo.ID); err != nil {
		t.Error(err)
	}
}

package store

import (
	"context"
	"testing"
	"time"
)

func TestTidyStored(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	uid, feedID, _ := fixture(t, st, "tidy", 0)
	item := NewEntry{GUID: "g", URL: "https://tidy.example/a", Title: "Hello", Content: "<div><h1>Hello</h1><p>Body.</p><p></p></div>",
		Enclosures: "[]", ContentHash: "h", PublishedAt: time.Now()}
	if _, _, err := st.UpsertEntries(ctx, feedID, []NewEntry{item}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if n, err := st.TidyStored(ctx); err != nil || n != 1 {
		t.Fatalf("tidied %d, %v", n, err)
	}
	list, _, _ := st.Entries(ctx, Filter{UserID: uid}, true)
	if list[0].Content != "<p>Body.</p>" {
		t.Fatalf("content %q", list[0].Content)
	}
	if n, _ := st.TidyStored(ctx); n != 0 {
		t.Fatalf("tidied again: %d", n)
	}
}

package store

import (
	"context"
	"testing"
	"time"
)

// TestSearchArticleText finds articles by words that appear only in their
// text, follows the text through full-article fetches and reverts, and
// indexes articles stored before search covered text.
func TestSearchArticleText(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	feedID, _ := st.AddFeed(ctx, uid, "https://x.example/f", "x", nil)
	st.UpsertEntries(ctx, feedID, []NewEntry{
		{GUID: "a", Title: "Harbours", Content: "<p>Pilots board ships.</p><p>Tugboats follow.</p>", Enclosures: "[]", PublishedAt: time.Now()},
		{GUID: "b", Title: "Gardens", Content: `<p>Roses <a href="https://tugboat.example">here</a></p>`, Enclosures: "[]", PublishedAt: time.Now()},
	}, time.Time{})
	find := func(q string) []string {
		t.Helper()
		es, _, err := st.Entries(ctx, Filter{UserID: uid, Query: q}, false)
		if err != nil {
			t.Fatal(err)
		}
		var titles []string
		for _, e := range es {
			titles = append(titles, e.Title)
		}
		return titles
	}
	if got := find("tugboat"); len(got) != 1 || got[0] != "Harbours" {
		t.Errorf("text search = %v, want [Harbours] (and never a link's address)", got)
	}
	if got := find("ships tugboats"); len(got) != 1 {
		t.Errorf("words across paragraphs = %v", got)
	}

	// The fetched full article is searchable; switching back restores the feed's text.
	ids, _, _ := st.EntryIDs(ctx, Filter{UserID: uid, Query: "roses"})
	if err := st.SetFullText(ctx, uid, ids[0], "<p>Tulips and dahlias</p>", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := find("dahlias"); len(got) != 1 || len(find("roses")) != 0 {
		t.Errorf("after full text: dahlias %v, roses %v", got, find("roses"))
	}
	if err := st.RevertFullText(ctx, uid, ids[0]); err != nil {
		t.Fatal(err)
	}
	if len(find("roses")) != 1 || len(find("dahlias")) != 0 {
		t.Errorf("after revert: roses %v, dahlias %v", find("roses"), find("dahlias"))
	}

	// Rows from before the migration have no text indexed until filled.
	st.db.ExecContext(ctx, `UPDATE entries SET search_text = ''`)
	if len(find("tugboats")) != 0 {
		t.Fatal("cleared text still found")
	}
	if n, err := st.FillSearchText(ctx); err != nil || n != 2 {
		t.Fatalf("filled %d, %v", n, err)
	}
	if len(find("tugboats")) != 1 {
		t.Error("filled text not found")
	}
}

func TestSummaries(t *testing.T) {
	st, ctx := open(t), context.Background()
	if _, ok, _ := st.Summary(ctx, "h1"); ok {
		t.Fatal("found a summary never saved")
	}
	st.SaveSummary(ctx, "h1", "First.")
	st.SaveSummary(ctx, "h1", "Second.")
	if s, ok, err := st.Summary(ctx, "h1"); !ok || err != nil || s != "Second." {
		t.Errorf("summary = %q %v %v", s, ok, err)
	}
}

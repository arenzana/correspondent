package api

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/store"
)

// TestSummaries has one reader's AI summarize an article on request, shares
// the summary with another reader who has the same article and no AI, and
// never asks the AI twice.
func TestSummaries(t *testing.T) {
	e := newAIEnv(t)
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	ctx := context.Background()
	entryOf := func(user string) string {
		uid, _ := e.store.UserIDByName(ctx, user)
		fid, _ := e.store.AddFeed(ctx, uid, "https://notes.example/feed", "Notes", nil)
		e.store.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "1", Title: "Ships", Content: "<p>Ships in the night.</p><p>Pilots guide them.</p>",
			Enclosures: "[]", PublishedAt: time.Now()}}, time.Time{})
		ids, _, _ := e.store.EntryIDs(ctx, store.Filter{UserID: uid})
		return itoa(ids[0])
	}
	aliceEntry, bobEntry := entryOf("alice"), entryOf("bob")
	path := func(id string) string { return "/api/v1/entries/" + id + "/summary" }

	if r := e.call(t, alice, "POST", path(aliceEntry), nil); r.StatusCode != http.StatusConflict {
		t.Errorf("without AI: %d", r.StatusCode)
	}
	// OpenAI's own service (no address of her choosing): what it writes is shared.
	if r := e.call(t, alice, "PUT", "/api/v1/ai", map[string]string{"provider": "openai", "model": "m",
		"key": "sk-test-key-0123456789"}); r.StatusCode != 200 {
		t.Fatalf("AI setup: %d", r.StatusCode)
	}
	e.answer.Store(`{"summary":"Pilots guide ships at night."}`)
	before := e.aiHits.Load()
	got := decodeInto[map[string]string](t, e.call(t, alice, "POST", path(aliceEntry), nil))
	if got["summary"] != "Pilots guide ships at night." || e.aiHits.Load() != before+1 {
		t.Fatalf("summary %v, %d AI requests", got, e.aiHits.Load()-before)
	}
	e.call(t, alice, "POST", path(aliceEntry), nil)

	// Bob has no AI, but the same article: he gets the summary, shown with
	// the article and on request, and the AI isn't asked again.
	opened := decodeInto[struct{ Entry struct{ Summary string } }](t, e.call(t, bob, "GET", "/api/v1/entries/"+bobEntry, nil))
	if opened.Entry.Summary != "Pilots guide ships at night." {
		t.Errorf("shared summary not with the article: %q", opened.Entry.Summary)
	}
	if got := decodeInto[map[string]string](t, e.call(t, bob, "POST", path(bobEntry), nil)); got["summary"] == "" {
		t.Error("shared summary not given on request")
	}
	if n := e.aiHits.Load() - before; n != 1 {
		t.Errorf("AI asked %d times, want 1", n)
	}
	// Nobody summarizes someone else's article.
	if r := e.call(t, bob, "POST", path(aliceEntry), nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("another reader's article: %d", r.StatusCode)
	}

	// Carol's AI is at an address she chose, which could answer anything:
	// her summary of a new article stays hers, and Bob, with the same
	// article, isn't shown it.
	carol := e.login(t, "carol")
	other := func(user string) string {
		uid, _ := e.store.UserIDByName(ctx, user)
		fid, _ := e.store.AddFeed(ctx, uid, "https://harbour.example/feed", "Harbour", nil)
		e.store.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "h", Title: "Tides", Content: "<p>Tides turn twice a day.</p>",
			Enclosures: "[]", PublishedAt: time.Now()}}, time.Time{})
		ids, _, _ := e.store.EntryIDs(ctx, store.Filter{UserID: uid, FeedID: fid})
		return itoa(ids[0])
	}
	carolEntry, bobTides := other("carol"), other("bob")
	e.call(t, carol, "PUT", "/api/v1/ai", map[string]string{"provider": "openai", "model": "m", "baseUrl": os.Getenv("TEST_AI_BASE"), "key": "sk-test-key-0123456789"})
	e.answer.Store(`{"summary":"Made up by Carol's server."}`)
	if got := decodeInto[map[string]string](t, e.call(t, carol, "POST", path(carolEntry), nil)); got["summary"] != "Made up by Carol's server." {
		t.Fatalf("carol's own summary: %v", got)
	}
	if got := decodeInto[map[string]string](t, e.call(t, carol, "POST", path(carolEntry), nil)); got["summary"] != "Made up by Carol's server." {
		t.Errorf("carol doesn't get her own summary back: %v", got)
	}
	opened = decodeInto[struct{ Entry struct{ Summary string } }](t, e.call(t, bob, "GET", "/api/v1/entries/"+bobTides, nil))
	if opened.Entry.Summary != "" {
		t.Errorf("a summary from a reader-chosen address was shared: %q", opened.Entry.Summary)
	}
}

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/extract"
	"github.com/arenzana/correspondent/internal/fetch"
	"github.com/arenzana/correspondent/internal/ingest"
	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/secretbox"
	"github.com/arenzana/correspondent/internal/store"
)

// A site with no feed whose articles share no path, heading or markup the
// exact methods recognise: only an AI-written parser reads it.
const lab = `<html><head><title>Lab notes</title></head><body><div class="grid">
<div class="tile"><a href="/2026/first-finding">On the first finding</a><span class="when">October 3, 2026</span></div>
<div class="tile"><a href="/notes/second-result">A second result</a><span class="when">October 2, 2026</span></div>
<div class="tile"><a href="/essays/third-thought">Third thoughts</a><span class="when">October 1, 2026</span></div>
</div></body></html>`

const article = `<html><head><meta property="og:type" content="article"><title>A</title></head><body><article><h1>A</h1><p>` +
	`This finding was long in the making and is described here at length, with methods, caveats, and the data. ` +
	`It took a year of patient work by a small team, and the results surprised everyone who saw them first. ` +
	`We describe what we did, why, and what we plan to do next, so that others can check and extend it.</p></article></body></html>`

type aiEnv struct {
	srv, site *httptest.Server
	store     *store.Store
	aiHits    atomic.Int32
	answer    atomic.Value // string
}

func newAIEnv(t *testing.T) *aiEnv {
	e := &aiEnv{}
	e.answer.Store(`{"item":"div.tile","link":"a","title":"a","date":".when","summary":"","image":"","reason":"tiles"}`)
	e.site = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(lab))
			return
		}
		w.Write([]byte(article))
	}))
	t.Cleanup(e.site.Close)
	ai := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test-key-0123456789" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[]}`)
			return
		}
		e.aiHits.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"finish_reason": "stop",
			"message": map[string]any{"role": "assistant", "content": e.answer.Load().(string)}}}})
	}))
	t.Cleanup(ai.Close)

	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e.store = st
	box, _ := secretbox.Load(filepath.Join(t.TempDir(), "keyring"))
	f := fetch.New(fetch.Options{AllowPrivate: true})
	log := slog.New(slog.DiscardHandler)
	ing := &ingest.Service{Store: st, Fetch: f, Log: log, Extract: &extract.Extractor{Fetch: f}}
	a := &API{Store: st, Ingest: ing, Log: log, Limiter: ratelimit.New(10, time.Minute), Keyring: box, AITransport: ai.Client().Transport}
	ing.AIFor = a.AIForApps
	e.srv = httptest.NewServer(a.Handler())
	t.Cleanup(e.srv.Close)
	t.Setenv("TEST_AI_BASE", ai.URL+"/v1")
	return e
}

func (e *aiEnv) login(t *testing.T, name string) *http.Client {
	t.Helper()
	if _, err := e.store.CreateUser(context.Background(), name, "password-"+name); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if r := e.call(t, c, "POST", "/api/v1/session", map[string]string{"username": name, "password": "password-" + name}); r.StatusCode != 200 {
		t.Fatalf("login: %d", r.StatusCode)
	}
	return c
}

func (e *aiEnv) call(t *testing.T, c *http.Client, method, path string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correspondent", "1")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeInto[T any](t *testing.T, r *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		t.Fatalf("decode (%d): %v", r.StatusCode, err)
	}
	return v
}

type candidates struct {
	Candidates []ingest.Candidate
	Error      string
}

func TestAIPageFeedFlow(t *testing.T) {
	e := newAIEnv(t)
	alice := e.login(t, "alice")
	aiBase := os.Getenv("TEST_AI_BASE")

	// No AI set up yet: the page has no feed and no exact reading.
	got := decodeInto[candidates](t, e.call(t, alice, "POST", "/api/v1/discover", map[string]string{"url": e.site.URL + "/"}))
	if len(got.Candidates) != 0 {
		t.Fatalf("exact methods read the lab page: %+v", got)
	}
	if r := e.call(t, alice, "POST", "/api/v1/discover/ai", map[string]string{"url": e.site.URL + "/"}); r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("AI without setup: %d", r.StatusCode)
	}

	// A wrong key is refused by the provider and not stored.
	setup := map[string]string{"provider": "openai", "model": "test-model", "baseUrl": aiBase, "key": "sk-wrong-key-0123456789"}
	if r := e.call(t, alice, "PUT", "/api/v1/ai", setup); r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("wrong key: %d", r.StatusCode)
	}
	setup["key"] = "sk-test-key-0123456789"
	if r := e.call(t, alice, "PUT", "/api/v1/ai", setup); r.StatusCode != 200 {
		t.Fatalf("save key: %d", r.StatusCode)
	}
	me := decodeInto[map[string]any](t, e.call(t, alice, "GET", "/api/v1/me", nil))
	raw, _ := json.Marshal(me)
	if strings.Contains(string(raw), "0123456789") || !strings.Contains(string(raw), `"keyHint":"6789"`) {
		t.Errorf("me leaks the key or lacks its hint: %s", raw)
	}
	// Stored sealed: the database never holds the key itself.
	uid, _ := e.store.UserIDByName(context.Background(), "alice")
	sealed, _, _ := e.store.AI(context.Background(), uid)
	if len(sealed) == 0 || bytes.Contains(sealed, []byte("sk-test-key")) {
		t.Errorf("key stored in the clear or not at all")
	}

	// The AI writes a parser once.
	got = decodeInto[candidates](t, e.call(t, alice, "POST", "/api/v1/discover/ai", map[string]string{"url": e.site.URL + "/"}))
	if len(got.Candidates) != 1 || !got.Candidates[0].Page || got.Candidates[0].Method != "css" || got.Candidates[0].ItemCount != 3 {
		t.Fatalf("%+v", got)
	}
	if e.aiHits.Load() != 1 {
		t.Fatalf("%d AI requests", e.aiHits.Load())
	}
	if r := e.call(t, alice, "POST", "/api/v1/feeds", map[string]any{"url": got.Candidates[0].URL, "page": true}); r.StatusCode != 200 {
		t.Fatalf("subscribe: %d", r.StatusCode)
	}

	// Another reader, without any AI, reuses it: no AI request.
	bob := e.login(t, "bob")
	got = decodeInto[candidates](t, e.call(t, bob, "POST", "/api/v1/discover", map[string]string{"url": e.site.URL}))
	if len(got.Candidates) != 1 || !got.Candidates[0].Shared {
		t.Fatalf("shared parser not found: %+v", got)
	}
	if r := e.call(t, bob, "POST", "/api/v1/feeds", map[string]any{"url": e.site.URL, "page": true}); r.StatusCode != 200 {
		t.Fatalf("bob subscribe: %d", r.StatusCode)
	}
	if e.aiHits.Load() != 1 {
		t.Errorf("AI asked again: %d", e.aiHits.Load())
	}
	tree := decodeInto[struct{ Feeds []store.Feed }](t, e.call(t, bob, "GET", "/api/v1/tree", nil))
	if len(tree.Feeds) != 1 || !tree.Feeds[0].Page || !tree.Feeds[0].FullText || tree.Feeds[0].Total != 3 || tree.Feeds[0].LastError != "" {
		t.Fatalf("bob's page feed: %+v", tree.Feeds)
	}
	// Articles were fetched in full from their pages.
	list := decodeInto[struct{ Entries []store.Entry }](t, e.call(t, bob, "GET", "/api/v1/entries?view=all&content=1", nil))
	if len(list.Entries) != 3 || !list.Entries[0].FullText || !strings.Contains(list.Entries[0].Content, "patient work") {
		t.Fatalf("entries: %+v", list.Entries)
	}

	// Rules can't be planted by a client: a subscribe naming a page with no
	// parser fails, and unknown fields are ignored.
	if r := e.call(t, bob, "POST", "/api/v1/feeds", map[string]any{"url": e.site.URL + "/other", "page": true,
		"rules": map[string]string{"method": "css", "item": "body"}}); r.StatusCode == 200 {
		t.Error("subscribed to a page with no parser")
	}

	// Rebuilding asks the AI again and every reader gets the new parser.
	id := tree.Feeds[0].ID
	e.answer.Store(`{"item":".grid > .tile","link":"a","title":"a","date":".when","summary":"","image":"","reason":"tiles"}`)
	if r := e.call(t, alice, "POST", fmt.Sprintf("/api/v1/feeds/%d/rebuild", id), nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("rebuild of another reader's feed: %d", r.StatusCode)
	}
	if r := e.call(t, bob, "POST", fmt.Sprintf("/api/v1/feeds/%d/rebuild", id), nil); r.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("rebuild without AI: %d", r.StatusCode)
	}
	aliceTree := decodeInto[struct{ Feeds []store.Feed }](t, e.call(t, alice, "GET", "/api/v1/tree", nil))
	if r := e.call(t, alice, "POST", fmt.Sprintf("/api/v1/feeds/%d/rebuild", aliceTree.Feeds[0].ID), nil); r.StatusCode != 200 {
		t.Fatalf("rebuild: %d", r.StatusCode)
	}
	p, _ := e.store.ParserFor(context.Background(), e.site.URL)
	if !strings.Contains(p.Rules, ".grid > .tile") || e.aiHits.Load() != 2 {
		t.Errorf("parser %q after %d AI requests", p.Rules, e.aiHits.Load())
	}

	// Removing the AI setup forgets the key.
	if r := e.call(t, alice, "DELETE", "/api/v1/ai", nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if me := decodeInto[map[string]any](t, e.call(t, alice, "GET", "/api/v1/me", nil)); me["ai"] != nil {
		t.Errorf("AI still set up: %v", me["ai"])
	}
}

func TestAIDailyLimit(t *testing.T) {
	e := newAIEnv(t)
	alice := e.login(t, "alice")
	base := os.Getenv("TEST_AI_BASE")
	e.call(t, alice, "PUT", "/api/v1/ai", map[string]string{"provider": "openai", "model": "m", "baseUrl": base, "key": "sk-test-key-0123456789"})
	for i := 0; i < AIPerDay; i++ {
		if r := e.call(t, alice, "POST", "/api/v1/discover/ai", map[string]any{"url": e.site.URL, "rebuild": true}); r.StatusCode != 200 {
			t.Fatalf("request %d: %d", i, r.StatusCode)
		}
	}
	r := e.call(t, alice, "POST", "/api/v1/discover/ai", map[string]any{"url": e.site.URL, "rebuild": true})
	if body, _ := io.ReadAll(r.Body); r.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "limit") {
		t.Errorf("past the limit: %d %s", r.StatusCode, body)
	}
	if e.aiHits.Load() != AIPerDay {
		t.Errorf("%d AI requests", e.aiHits.Load())
	}
}

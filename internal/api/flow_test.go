package api

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/push"
	"github.com/arenzana/correspondent/internal/ratelimit"
)

func decodeJSON[T any](t *testing.T, r *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", r.Request.URL.Path, err)
	}
	return v
}

func expect(t *testing.T, r *http.Response, status int) *http.Response {
	t.Helper()
	if r.StatusCode != status {
		b, _ := io.ReadAll(r.Body)
		t.Fatalf("%s %s: %d, want %d: %s", r.Request.Method, r.Request.URL.Path, r.StatusCode, status, b)
	}
	return r
}

// TestReaderFlow walks the calls the web reader makes, in order.
func TestReaderFlow(t *testing.T) {
	e := newEnv(t)
	c, entryID := e.user(t, "alice")

	me := decodeJSON[struct {
		Username       string
		HasAPIPassword bool `json:"hasApiPassword"`
	}](t, expect(t, e.do(t, c, "GET", "/api/v1/me", "", false), 200))
	if me.Username != "alice" || me.HasAPIPassword {
		t.Fatalf("me: %+v", me)
	}

	// Preferences are opaque JSON owned by the UI; invalid JSON is refused.
	expect(t, e.do(t, c, "PUT", "/api/v1/prefs", `{"theme":"evening"}`, true), 204)
	expect(t, e.do(t, c, "PUT", "/api/v1/prefs", `{theme`, true), 400)

	// API password: too short is refused, then set.
	expect(t, e.do(t, c, "PUT", "/api/v1/api-password", `{"password":"short"}`, true), 400)
	expect(t, e.do(t, c, "PUT", "/api/v1/api-password", `{"password":"long-enough-api-pw"}`, true), 204)

	// Folders and feeds.
	folder := decodeJSON[struct{ ID int64 }](t, expect(t, e.do(t, c, "POST", "/api/v1/folders", `{"name":"News"}`, true), 200))
	tree := decodeJSON[struct {
		Folders []struct{ ID int64 }
		Feeds   []struct {
			ID      int64
			Unread  int
			Cadence []int
		}
		Counts struct{ Unread int }
	}](t, expect(t, e.do(t, c, "GET", "/api/v1/tree", "", false), 200))
	if len(tree.Folders) != 1 || len(tree.Feeds) != 1 || tree.Counts.Unread != 1 || len(tree.Feeds[0].Cadence) != 14 {
		t.Fatalf("tree: %+v", tree)
	}
	feedID := tree.Feeds[0].ID
	expect(t, e.do(t, c, "PATCH", fmt.Sprintf("/api/v1/feeds/%d", feedID), fmt.Sprintf(`{"title":"Renamed","folderId":%d}`, folder.ID), true), 204)
	expect(t, e.do(t, c, "PATCH", fmt.Sprintf("/api/v1/folders/%d", folder.ID), `{"name":"World"}`, true), 204)
	expect(t, e.do(t, c, "PATCH", "/api/v1/feeds/999999", `{"title":"x"}`, true), 404)

	// Entries by view and folder; IDs travel as strings.
	list := decodeJSON[struct {
		Entries []struct {
			ID   string
			Read bool
		}
		Cursor string
	}](t, expect(t, e.do(t, c, "GET", fmt.Sprintf("/api/v1/entries?folder=%d&view=unread", folder.ID), "", false), 200))
	if len(list.Entries) != 1 || list.Entries[0].ID != entryID || list.Cursor != "" {
		t.Fatalf("folder entries: %+v", list)
	}
	withBody := decodeJSON[struct {
		Entries []struct{ Content string }
	}](t, expect(t, e.do(t, c, "GET", "/api/v1/entries?view=unread&content=1", "", false), 200))
	if len(withBody.Entries) != 1 {
		t.Fatalf("content=1: %+v", withBody)
	}
	full := decodeJSON[struct {
		Entry struct{ Title, Content string }
	}](t, expect(t, e.do(t, c, "GET", "/api/v1/entries/"+entryID, "", false), 200))
	if !strings.Contains(full.Entry.Title, "alice") {
		t.Fatalf("entry: %+v", full)
	}
	expect(t, e.do(t, c, "GET", "/api/v1/entries/not-a-number", "", false), 400)

	// Mark all read returns the changed IDs (for Undo); unread restores them.
	marked := decodeJSON[struct{ IDs []string }](t, expect(t,
		e.do(t, c, "POST", "/api/v1/entries/mark-all-read", fmt.Sprintf(`{"before":%q}`, entryID), true), 200))
	if len(marked.IDs) != 1 || marked.IDs[0] != entryID {
		t.Fatalf("mark all: %+v", marked)
	}
	expect(t, e.do(t, c, "POST", "/api/v1/entries/mark-all-read", `{}`, true), 400) // boundary required
	undo := decodeJSON[struct{ Changed int }](t, expect(t,
		e.do(t, c, "POST", "/api/v1/entries/state", fmt.Sprintf(`{"action":"unread","ids":[%q]}`, entryID), true), 200))
	if undo.Changed != 1 {
		t.Fatalf("undo changed %d", undo.Changed)
	}
	expect(t, e.do(t, c, "POST", "/api/v1/entries/state", `{"action":"read","ids":["abc"]}`, true), 400)

	// OPML export then import into the same account adds nothing new.
	r := expect(t, e.do(t, c, "GET", "/api/v1/opml", "", false), 200)
	opml, _ := io.ReadAll(r.Body)
	if !strings.Contains(string(opml), "alice.example/feed") || !strings.Contains(string(opml), `text="World"`) {
		t.Fatalf("export: %s", opml)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/v1/opml", strings.NewReader(string(opml)))
	req.Header.Set("X-Correspondent", "1")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	expect(t, resp, 200)
	expect(t, e.do(t, c, "POST", "/api/v1/opml", "not xml", true), 400)

	// Subscribing refuses non-http(s) addresses before any fetch happens.
	expect(t, e.do(t, c, "POST", "/api/v1/feeds", `{"url":"file:///etc/passwd"}`, true), 400)

	// Deleting cascades to entries; signing out ends the session.
	expect(t, e.do(t, c, "DELETE", fmt.Sprintf("/api/v1/feeds/%d", feedID), "", true), 204)
	expect(t, e.do(t, c, "GET", "/api/v1/entries/"+entryID, "", false), 404)
	expect(t, e.do(t, c, "DELETE", fmt.Sprintf("/api/v1/folders/%d", folder.ID), "", true), 204)
	expect(t, e.do(t, c, "DELETE", "/api/v1/session", "", true), 204)
	expect(t, e.do(t, c, "GET", "/api/v1/me", "", false), 401)
}

func TestManifestFollowsChosenIcon(t *testing.T) {
	e := newEnv(t)
	c, _ := e.user(t, "erin")
	manifest := func(cl *http.Client) string {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/manifest.webmanifest", nil)
		for _, ck := range cl.Jar.Cookies(mustURL(e.srv.URL)) {
			r.AddCookie(ck)
		}
		(&API{Store: e.store}).Manifest(rec, r)
		return rec.Body.String()
	}
	if m := manifest(c); !strings.Contains(m, "/icons/wire/icon-512.png") || !strings.Contains(m, `"maskable"`) {
		t.Fatalf("default manifest: %s", m)
	}
	expect(t, e.do(t, c, "PUT", "/api/v1/prefs", `{"appIcon":"morse"}`, true), 204)
	if m := manifest(c); !strings.Contains(m, "/icons/morse/icon-512.png") {
		t.Fatalf("manifest ignores the chosen icon: %s", m)
	}
	// Unknown values never reach a path.
	expect(t, e.do(t, c, "PUT", "/api/v1/prefs", `{"appIcon":"../../etc"}`, true), 204)
	if m := manifest(c); !strings.Contains(m, "/icons/wire/") || strings.Contains(m, "etc") {
		t.Fatalf("unsafe icon accepted: %s", m)
	}
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestLoginRequiresHeader(t *testing.T) {
	e := newEnv(t)
	e.user(t, "dave")
	r := e.do(t, &http.Client{}, "POST", "/api/v1/session", `{"username":"dave","password":"password-dave"}`, false)
	expect(t, r, http.StatusForbidden)
}

func TestHubLimitsAndOverflow(t *testing.T) {
	var h hub
	for i := range 16 {
		if _, ok := h.subscribe(1); !ok {
			t.Fatalf("stream %d refused", i)
		}
	}
	if _, ok := h.subscribe(1); ok {
		t.Fatal("17th stream accepted")
	}
	s, ok := h.subscribe(2)
	if !ok {
		t.Fatal("other user affected by the limit")
	}
	for range 200 { // far more than the buffer: must not block
		h.publish(2, event{name: "state", data: []byte("{}")})
	}
	if len(s.ch) != cap(s.ch) {
		t.Fatalf("buffer holds %d of %d", len(s.ch), cap(s.ch))
	}
	select {
	case <-s.overflowed:
	default:
		t.Fatal("overflow not signalled, the reader would silently miss changes")
	}
}

// TestStateEventsReachOpenReaders: a change made by any client (here through
// the store, as the Reader API does) is pushed to the user's open streams,
// and only to that user's.
func TestStateEventsReachOpenReaders(t *testing.T) {
	e := newEnv(t)
	a := &API{Store: e.store, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute)}
	e.store.OnStateChange = a.NotifyState
	srv := httptest.NewServer(a.Handler())
	// Cleanups run last-in first-out: open streams close before the server,
	// whose Close waits for active connections.
	t.Cleanup(srv.Close)
	e.srv = srv
	alice, entry := e.user(t, "alice")
	bob, _ := e.user(t, "bob")

	open := func(c *http.Client) *bufio.Reader {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/events", nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return bufio.NewReader(resp.Body)
	}
	aliceStream, bobStream := open(alice), open(bob)
	next := func(r *bufio.Reader) (string, string) {
		var name, data string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return name, data
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && name != "":
				return name, data
			}
		}
	}

	uid, _ := e.store.UserIDByName(context.Background(), "alice")
	id, _ := strconv.ParseInt(entry, 10, 64)
	if _, err := e.store.SetState(context.Background(), uid, "star", []int64{id}); err != nil {
		t.Fatal(err)
	}
	name, data := next(aliceStream)
	if name != "state" || !strings.Contains(data, `"action":"star"`) || !strings.Contains(data, entry) {
		t.Fatalf("alice got %q %q", name, data)
	}
	// A no-op change sends nothing; bob never hears about alice's articles.
	e.store.SetState(context.Background(), uid, "star", []int64{id})
	got := make(chan string, 1)
	go func() { n, _ := next(bobStream); got <- n }()
	select {
	case n := <-got:
		t.Fatalf("bob received %q", n)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestPushSubscriptionAPI(t *testing.T) {
	e := newEnv(t)
	a := &API{Store: e.store, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute),
		Push: &push.Notifier{Store: e.store, Log: slog.New(slog.DiscardHandler)}}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	e.srv = srv
	c, _ := e.user(t, "pat")

	st := decodeJSON[struct {
		PublicKey  string
		Subscribed bool
	}](t, expect(t, e.do(t, c, "GET", "/api/v1/push", "", false), 200))
	if len(st.PublicKey) < 80 || st.Subscribed {
		t.Fatalf("status: %+v", st)
	}
	key := base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
	auth := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	body := func(ep string) string {
		return fmt.Sprintf(`{"endpoint":%q,"keys":{"p256dh":%q,"auth":%q}}`, ep, key, auth)
	}
	// Endpoints outside the known push services are refused (SSRF).
	expect(t, e.do(t, c, "POST", "/api/v1/push/subscriptions", body("https://169.254.169.254/x"), true), 400)
	expect(t, e.do(t, c, "POST", "/api/v1/push/subscriptions",
		fmt.Sprintf(`{"endpoint":"https://fcm.googleapis.com/fcm/send/x","keys":{"p256dh":"short","auth":%q}}`, auth), true), 400)
	ep := "https://fcm.googleapis.com/fcm/send/device-1"
	expect(t, e.do(t, c, "POST", "/api/v1/push/subscriptions", body(ep), true), 204)
	st = decodeJSON[struct {
		PublicKey  string
		Subscribed bool
	}](t, expect(t, e.do(t, c, "GET", "/api/v1/push?endpoint="+url.QueryEscape(ep), "", false), 200))
	if !st.Subscribed {
		t.Fatal("device not registered")
	}
	expect(t, e.do(t, c, "DELETE", "/api/v1/push/subscriptions", fmt.Sprintf(`{"endpoint":%q}`, ep), true), 204)

	// Feeds opt in and out.
	tree := decodeJSON[struct{ Feeds []struct{ ID int64 } }](t, expect(t, e.do(t, c, "GET", "/api/v1/tree", "", false), 200))
	expect(t, e.do(t, c, "PATCH", fmt.Sprintf("/api/v1/feeds/%d", tree.Feeds[0].ID), `{"notify":true}`, true), 204)
	after := decodeJSON[struct{ Feeds []struct{ Notify bool } }](t, expect(t, e.do(t, c, "GET", "/api/v1/tree", "", false), 200))
	if !after.Feeds[0].Notify {
		t.Fatal("feed not opted in")
	}
}

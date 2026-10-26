package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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

func TestHubLimitsStreams(t *testing.T) {
	var h hub
	for i := range 16 {
		if _, ok := h.subscribe(1); !ok {
			t.Fatalf("stream %d refused", i)
		}
	}
	if _, ok := h.subscribe(1); ok {
		t.Fatal("17th stream accepted")
	}
	ch, ok := h.subscribe(2)
	if !ok {
		t.Fatal("other user affected by the limit")
	}
	h.publish(2)
	h.publish(2) // coalesces; must not block
	select {
	case <-ch:
	default:
		t.Fatal("no notification delivered")
	}
}

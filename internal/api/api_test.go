package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/store"
)

type env struct {
	srv   *httptest.Server
	store *store.Store
}

func newEnv(t *testing.T) *env {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &API{Store: st, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute)}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	return &env{srv: srv, store: st}
}

// user creates an account with one feed and one entry, and returns a
// signed-in client plus the entry ID.
func (e *env) user(t *testing.T, name string) (*http.Client, string) {
	ctx := context.Background()
	uid, err := e.store.CreateUser(ctx, name, "password-"+name)
	if err != nil {
		t.Fatal(err)
	}
	fid, _ := e.store.AddFeed(ctx, uid, "https://"+name+".example/feed", name, nil)
	e.store.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: name + "-1", Title: "secret of " + name, Enclosures: "[]"}}, time.Time{})
	ids, _, _ := e.store.EntryIDs(ctx, store.Filter{UserID: uid})
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp := e.do(t, c, "POST", "/api/v1/session", fmt.Sprintf(`{"username":%q,"password":%q}`, name, "password-"+name), true)
	if resp.StatusCode != 200 {
		t.Fatalf("login %s: %d", name, resp.StatusCode)
	}
	return c, fmt.Sprint(ids[0])
}

func (e *env) do(t *testing.T, c *http.Client, method, path, body string, csrf bool) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set("X-Correspondent", "1")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestCSRFHeaderRequired(t *testing.T) {
	e := newEnv(t)
	c, id := e.user(t, "alice")
	body := fmt.Sprintf(`{"action":"star","ids":[%q]}`, id)
	if r := e.do(t, c, "POST", "/api/v1/entries/state", body, false); r.StatusCode != http.StatusForbidden {
		t.Fatalf("mutation without header: %d, want 403", r.StatusCode)
	}
	if r := e.do(t, c, "POST", "/api/v1/entries/state", body, true); r.StatusCode != 200 {
		t.Fatalf("mutation with header: %d", r.StatusCode)
	}
}

func TestUsersAreIsolated(t *testing.T) {
	e := newEnv(t)
	alice, aliceEntry := e.user(t, "alice")
	bob, _ := e.user(t, "bob")

	if r := e.do(t, bob, "GET", "/api/v1/entries/"+aliceEntry, "", false); r.StatusCode != http.StatusNotFound {
		t.Fatalf("bob read alice's entry: %d", r.StatusCode)
	}
	r := e.do(t, bob, "POST", "/api/v1/entries/state", fmt.Sprintf(`{"action":"read","ids":[%q]}`, aliceEntry), true)
	var out struct{ Changed int }
	json.NewDecoder(r.Body).Decode(&out)
	if out.Changed != 0 {
		t.Fatal("bob changed alice's entry state")
	}
	r = e.do(t, bob, "GET", "/api/v1/entries?view=all", "", false)
	var list struct{ Entries []struct{ Title string } }
	json.NewDecoder(r.Body).Decode(&list)
	for _, en := range list.Entries {
		if strings.Contains(en.Title, "alice") {
			t.Fatal("alice's entry listed for bob")
		}
	}
	if r := e.do(t, alice, "GET", "/api/v1/entries/"+aliceEntry, "", false); r.StatusCode != 200 {
		t.Fatalf("alice cannot read her own entry: %d", r.StatusCode)
	}
}

func TestLoginThrottled(t *testing.T) {
	e := newEnv(t)
	e.store.CreateUser(context.Background(), "carol", "password-carol")
	c := &http.Client{}
	for i := range 10 {
		if r := e.do(t, c, "POST", "/api/v1/session", `{"username":"carol","password":"wrong"}`, true); r.StatusCode != 401 {
			t.Fatalf("attempt %d: %d", i, r.StatusCode)
		}
	}
	if r := e.do(t, c, "POST", "/api/v1/session", `{"username":"carol","password":"password-carol"}`, true); r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after 10 failures: %d, want 429", r.StatusCode)
	}
}

func TestUnauthenticated(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/v1/tree", "/api/v1/entries", "/api/v1/opml", "/api/v1/events"} {
		if r := e.do(t, &http.Client{}, "GET", p, "", false); r.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", p, r.StatusCode)
		}
	}
}

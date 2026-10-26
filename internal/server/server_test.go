package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/arenzana/correspondent/internal/api"
	"github.com/arenzana/correspondent/internal/greader"
	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/store"
)

func newServer(t *testing.T, secure bool) *httptest.Server {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.DiscardHandler)
	lim := ratelimit.New(10, 60)
	ui := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>app</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	h := New(st, &api.API{Store: st, Log: log, Limiter: lim}, &greader.Server{Store: st, Log: log, Limiter: lim}, ui, secure)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestSecurityHeaders(t *testing.T) {
	srv := newServer(t, true)
	h := get(t, srv.URL+"/").Header
	for k, want := range map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"X-Frame-Options":              "DENY",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Strict-Transport-Security":    "max-age=63072000; includeSubDomains",
		"Cross-Origin-Resource-Policy": "same-origin",
	} {
		if h.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, h.Get(k), want)
		}
	}
	csp := h.Get("Content-Security-Policy")
	for _, d := range []string{"script-src 'self'", "frame-ancestors 'none'", "object-src 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, d) {
			t.Errorf("CSP missing %q: %s", d, csp)
		}
	}
	if get(t, newServer(t, false).URL+"/").Header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent without HTTPS")
	}
}

func TestSPAFallbackAndCaching(t *testing.T) {
	srv := newServer(t, false)
	r := get(t, srv.URL+"/some/client/route")
	if r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("client route: %d %q", r.StatusCode, r.Header.Get("Cache-Control"))
	}
	r = get(t, srv.URL+"/assets/app.js")
	if !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset caching: %q", r.Header.Get("Cache-Control"))
	}
	// Path traversal resolves inside the UI filesystem and falls back to the app shell.
	r = get(t, srv.URL+"/assets/../../../etc/passwd")
	if body, _ := io.ReadAll(r.Body); !strings.Contains(string(body), "<title>app</title>") {
		t.Fatalf("traversal served %q", body)
	}
}

func TestRouting(t *testing.T) {
	srv := newServer(t, false)
	if r := get(t, srv.URL+"/ready"); r.StatusCode != 200 {
		t.Fatalf("/ready: %d", r.StatusCode)
	}
	if r := get(t, srv.URL+"/api/v1/tree"); r.StatusCode != 401 {
		t.Fatalf("/api/v1 without session: %d", r.StatusCode)
	}
	// The legacy Reader path routes its whole suffix and answers in its own
	// plain-text dialect, never with the SPA.
	r := get(t, srv.URL+greader.Prefix+"/reader/api/0/subscription/list?output=json")
	if r.StatusCode != 401 || r.Header.Get("Content-Security-Policy") != "" {
		t.Fatalf("reader path: %d csp=%q", r.StatusCode, r.Header.Get("Content-Security-Policy"))
	}
}

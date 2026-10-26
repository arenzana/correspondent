package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestAllowed(t *testing.T) {
	cases := map[string]bool{
		"93.184.215.14":   true,
		"2606:4700::1111": true,
		"127.0.0.1":       false,
		"10.0.0.5":        false,
		"172.16.3.4":      false,
		"192.168.1.1":     false,
		"169.254.169.254": false, // cloud metadata
		"100.100.100.100": false, // Tailscale / CGNAT
		"0.0.0.0":         false,
		"::1":             false,
		"fe80::1":         false,
		"fd00::1":         false,
		"::ffff:10.0.0.1": false, // IPv4-mapped private
		"64:ff9b::a00:1":  false, // NAT64 to 10.0.0.1
	}
	for addr, want := range cases {
		if got := Allowed(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Allowed(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestBlocksLoopbackServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secret"))
	}))
	defer srv.Close()
	_, err := New(Options{}).Get(context.Background(), Request{URL: srv.URL})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}
}

func TestRejectsSchemes(t *testing.T) {
	c := New(Options{})
	for _, u := range []string{"file:///etc/passwd", "gopher://x", "http://user:pw@example.com/"} {
		if _, err := c.Get(context.Background(), Request{URL: u}); err == nil {
			t.Errorf("%s: expected error", u)
		}
	}
}

func TestConditionalAndLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.URL.Path == "/big" {
			w.Write([]byte(strings.Repeat("x", 2048)))
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte("<rss/>"))
	}))
	defer srv.Close()
	c := New(Options{AllowPrivate: true, MaxBytes: 1024})

	r, err := c.Get(context.Background(), Request{URL: srv.URL})
	if err != nil || string(r.Body) != "<rss/>" || r.ETag != `"v1"` {
		t.Fatalf("first fetch: %+v %v", r, err)
	}
	r, err = c.Get(context.Background(), Request{URL: srv.URL, ETag: r.ETag})
	if err != nil || !r.NotModified {
		t.Fatalf("conditional fetch: %+v %v", r, err)
	}
	if _, err := c.Get(context.Background(), Request{URL: srv.URL + "/big"}); err == nil {
		t.Fatal("expected size limit error")
	}
}

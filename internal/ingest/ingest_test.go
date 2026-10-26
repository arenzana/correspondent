package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/fetch"
	"github.com/arenzana/correspondent/internal/store"
)

func rssBody(items ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>Test feed</title><link>https://t.example/</link>`)
	for _, it := range items {
		fmt.Fprintf(&b, `<item><title>%s</title><guid>%s</guid><pubDate>%s</pubDate></item>`, it, it, time.Now().Format(time.RFC1123Z))
	}
	b.WriteString(`</channel></rss>`)
	return b.String()
}

type feedServer struct {
	*httptest.Server
	status atomic.Int32
}

func newFeedServer(t *testing.T) *feedServer {
	fs := &feedServer{}
	fs.status.Store(200)
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			fmt.Fprintf(w, `<html><head><link rel="alternate" type="application/rss+xml" href="%s/feed"></head></html>`, fs.URL)
			return
		case "/nofeed":
			w.Write([]byte(`<!DOCTYPE html><html><body>hello</body></html>`))
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(int(fs.status.Load()))
		w.Write([]byte(rssBody("a", "b")))
	}))
	t.Cleanup(fs.Close)
	return fs
}

func newService(t *testing.T) (*Service, *store.Store, int64) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	svc := &Service{Store: st, Fetch: fetch.New(fetch.Options{AllowPrivate: true}), Log: slog.New(slog.DiscardHandler)}
	return svc, st, uid
}

func TestSubscribePollsAndRecordsMetadata(t *testing.T) {
	fs := newFeedServer(t)
	svc, st, uid := newService(t)
	ctx := context.Background()
	var notified atomic.Int32
	svc.Notify = func(int64) { notified.Add(1) }

	id, err := svc.Subscribe(ctx, uid, fs.URL+"/feed", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := st.Feed(ctx, uid, id)
	if f.Title != "Test feed" || f.ETag != `"v1"` || f.LastStatus != 200 || f.ErrorCount != 0 {
		t.Fatalf("feed after first poll: %+v", f)
	}
	entries, _, _ := st.Entries(ctx, store.Filter{UserID: uid}, false)
	if len(entries) != 2 || notified.Load() != 1 {
		t.Fatalf("entries %d, notified %d", len(entries), notified.Load())
	}

	// Conditional re-poll: 304, nothing new, no notification.
	svc.Poll(ctx, *f)
	f, _ = st.Feed(ctx, uid, id)
	if f.LastStatus != 304 || notified.Load() != 1 {
		t.Fatalf("after 304: status %d notified %d", f.LastStatus, notified.Load())
	}
}

func TestFailuresBackOffAndRecover(t *testing.T) {
	fs := newFeedServer(t)
	svc, st, uid := newService(t)
	ctx := context.Background()
	fs.status.Store(503)
	id, _ := st.AddFeed(ctx, uid, fs.URL+"/feed", "", nil)

	for i := 1; i <= 3; i++ {
		f, _ := st.Feed(ctx, uid, id)
		svc.Poll(ctx, *f)
		f, _ = st.Feed(ctx, uid, id)
		if f.ErrorCount != i || !strings.Contains(f.LastError, "503") {
			t.Fatalf("attempt %d: %+v", i, f)
		}
		// A failing feed is not due again right away.
		if due, _ := st.DueFeeds(ctx, time.Now(), 10); len(due) != 0 {
			t.Fatalf("attempt %d: failing feed rescheduled immediately", i)
		}
	}

	fs.status.Store(200)
	f, _ := st.Feed(ctx, uid, id)
	svc.Poll(ctx, *f)
	f, _ = st.Feed(ctx, uid, id)
	if f.ErrorCount != 0 || f.LastError != "" {
		t.Fatalf("did not recover: %+v", f)
	}
}

func TestNotAFeedIsAnError(t *testing.T) {
	fs := newFeedServer(t)
	svc, st, uid := newService(t)
	ctx := context.Background()
	id, _ := st.AddFeed(ctx, uid, fs.URL+"/nofeed", "", nil)
	f, _ := st.Feed(ctx, uid, id)
	svc.Poll(ctx, *f)
	f, _ = st.Feed(ctx, uid, id)
	if !strings.Contains(f.LastError, "not a feed") {
		t.Fatalf("last error = %q", f.LastError)
	}
}

func TestNextFetch(t *testing.T) {
	within := func(got time.Time, want time.Duration) bool {
		d := time.Until(got)
		return d > want*85/100 && d < want*115/100
	}
	f := store.Feed{IntervalSec: 1800}
	if got := nextFetch(f, false, nil); !within(got, 30*time.Minute) {
		t.Errorf("healthy: %v", time.Until(got))
	}
	f.ErrorCount = 2
	if got := nextFetch(f, true, nil); !within(got, 4*time.Hour) {
		t.Errorf("after 3rd failure: %v, want ~4h", time.Until(got))
	}
	f.ErrorCount = 50
	if got := nextFetch(f, true, nil); time.Until(got) > 27*time.Hour {
		t.Errorf("backoff not capped: %v", time.Until(got))
	}
	resp := &fetch.Response{RetryAfter: 6 * time.Hour}
	if got := nextFetch(store.Feed{IntervalSec: 1800}, false, resp); !within(got, 6*time.Hour) {
		t.Errorf("Retry-After ignored: %v", time.Until(got))
	}
}

func TestDiscover(t *testing.T) {
	fs := newFeedServer(t)
	svc, _, _ := newService(t)
	ctx := context.Background()

	got, err := svc.Discover(ctx, fs.URL+"/page")
	if err != nil || len(got) != 1 || got[0].URL != fs.URL+"/feed" || got[0].ItemCount != 2 {
		t.Fatalf("from page: %+v %v", got, err)
	}
	got, err = svc.Discover(ctx, fs.URL+"/feed")
	if err != nil || len(got) != 1 || got[0].Title != "Test feed" {
		t.Fatalf("direct feed: %+v %v", got, err)
	}
	if _, err := svc.Discover(ctx, fs.URL+"/nofeed"); err == nil {
		t.Fatal("page without feeds returned candidates")
	}
}

func TestRedact(t *testing.T) {
	got := redact(`Get "https://feeds.example/private.rss?token=s3cr3t": dial tcp: i/o timeout`)
	if strings.Contains(got, "s3cr3t") || strings.Contains(got, "feeds.example") {
		t.Fatalf("redact left the URL: %q", got)
	}
}

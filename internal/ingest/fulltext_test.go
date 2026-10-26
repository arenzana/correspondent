package ingest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/arenzana/correspondent/internal/extract"
	"github.com/arenzana/correspondent/internal/fetch"
	"github.com/arenzana/correspondent/internal/store"
)

func articleHTML(n string) string {
	return fmt.Sprintf(`<html><head><title>Article %s</title></head><body><nav>Home Shop Login</nav>
<article><h1>Article %s</h1><p>%s</p><p>%s</p></article><footer>Copyright</footer></body></html>`,
		n, n, strings.Repeat("The full text of article "+n+" lives only on its web page. ", 6),
		strings.Repeat("A second paragraph adds detail and length. ", 5))
}

// titleOnlySite serves a feed with titles and links only, plus the pages.
func titleOnlySite(t *testing.T, items *atomic.Value, pageHits *atomic.Int32) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed" {
			var b strings.Builder
			b.WriteString(`<rss version="2.0"><channel><title>Title Only</title>`)
			for _, it := range items.Load().([][2]string) {
				fmt.Fprintf(&b, `<item><title>%s</title><link>%s/a/%s</link><guid>%s</guid><description>%s</description></item>`,
					it[0], srv.URL, it[0], it[0], it[1])
			}
			b.WriteString(`</channel></rss>`)
			w.Write([]byte(b.String()))
			return
		}
		pageHits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(articleHTML(strings.TrimPrefix(r.URL.Path, "/a/"))))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFullTextFeeds(t *testing.T) {
	var items atomic.Value
	var hits atomic.Int32
	items.Store([][2]string{{"one", ""}, {"two", ""}})
	site := titleOnlySite(t, &items, &hits)
	svc, st, uid := newService(t)
	f := fetch.New(fetch.Options{AllowPrivate: true})
	svc.Fetch, svc.Extract = f, &extract.Extractor{Fetch: f}
	ctx := context.Background()

	id, _ := st.AddFeed(ctx, uid, site.URL+"/feed", "", nil)
	st.SetFeedFullText(ctx, uid, id, true)
	feed, _ := st.Feed(ctx, uid, id)
	svc.Poll(ctx, *feed)

	list, _, _ := st.Entries(ctx, store.Filter{UserID: uid}, false)
	if len(list) != 2 || hits.Load() != 2 {
		t.Fatalf("entries %d, page fetches %d", len(list), hits.Load())
	}
	e, _ := st.Entry(ctx, uid, list[0].ID)
	if !e.FullText || !strings.Contains(e.Content, "lives only on its web page") || strings.Contains(e.Content, "Copyright") {
		t.Fatalf("full article not stored: fullText=%v %q", e.FullText, e.Content)
	}
	if e.Excerpt == "" {
		t.Fatal("excerpt not filled from the article")
	}

	// Polling again fetches no pages for articles already stored.
	feed, _ = st.Feed(ctx, uid, id)
	svc.Poll(ctx, *feed)
	if hits.Load() != 2 {
		t.Fatalf("re-poll refetched pages: %d", hits.Load())
	}

	// An upstream edit keeps the full article; the feed's text is kept aside.
	items.Store([][2]string{{"one", "edited summary"}, {"two", ""}})
	st.ScheduleNow(ctx, id)
	feed, _ = st.Feed(ctx, uid, id)
	feed.ETag, feed.LastModified = "", ""
	svc.Poll(ctx, *feed)
	for _, x := range list {
		e, _ := st.Entry(ctx, uid, x.ID)
		if !strings.Contains(e.Content, "lives only on its web page") {
			t.Fatalf("an upstream edit replaced the full article: %q", e.Content)
		}
	}

	// Switching back shows the feed's own text again.
	if err := st.RevertFullText(ctx, uid, list[0].ID); err != nil {
		t.Fatal(err)
	}
	e, _ = st.Entry(ctx, uid, list[0].ID)
	if e.FullText || strings.Contains(e.Content, "lives only") {
		t.Fatalf("revert: %+v", e)
	}
}

func TestBackfillAfterTurningOn(t *testing.T) {
	var items atomic.Value
	var hits atomic.Int32
	items.Store([][2]string{{"one", "summary"}, {"two", "summary"}, {"three", "summary"}})
	site := titleOnlySite(t, &items, &hits)
	svc, st, uid := newService(t)
	f := fetch.New(fetch.Options{AllowPrivate: true})
	svc.Fetch, svc.Extract = f, &extract.Extractor{Fetch: f}
	ctx := context.Background()

	id, _ := st.AddFeed(ctx, uid, site.URL+"/feed", "", nil)
	feed, _ := st.Feed(ctx, uid, id)
	svc.Poll(ctx, *feed) // switch off: no pages fetched
	if hits.Load() != 0 {
		t.Fatal("fetched pages for a feed without full articles")
	}
	st.SetFeedFullText(ctx, uid, id, true)
	feed, _ = st.Feed(ctx, uid, id)
	svc.Backfill(ctx, *feed)
	if hits.Load() != 3 {
		t.Fatalf("backfill fetched %d pages, want 3", hits.Load())
	}
	list, _, _ := st.Entries(ctx, store.Filter{UserID: uid}, false)
	for _, e := range list {
		if !e.FullText {
			t.Fatalf("entry %d not backfilled", e.ID)
		}
	}
}

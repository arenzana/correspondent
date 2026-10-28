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

// A link blog's feed: one post about another site's article (its own page
// as the GUID, as Lex on Tech does), one aggregator-style item with no
// commentary, and one whole article of its own whose page extracts as
// something else.
func TestFullTextKeepsLinkPostsAndWholeArticles(t *testing.T) {
	var pages atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(articleHTML(strings.TrimPrefix(r.URL.Path, "/a/"))))
	}))
	t.Cleanup(other.Close)
	// Another host, as far as link posts go: localhost rather than 127.0.0.1.
	elsewhere := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)

	commentary := strings.Repeat("My thoughts on this linked article, which is worth your time. ", 4)
	essay := strings.Repeat("An essay published whole in the feed, every word of it. ", 20)
	var blog *httptest.Server
	blog = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed" {
			fmt.Fprintf(w, `<rss version="2.0"><channel><title>Links</title><link>%[1]s/</link>
<item><title>Link</title><link>%[2]s/a/linked</link><guid>%[1]s/link</guid><description>&lt;p&gt;%[3]s&lt;/p&gt;</description></item>
<item><title>Aggregated</title><link>%[2]s/a/aggregated</link><guid>%[1]s/item/7</guid><description>Comments</description></item>
<item><title>Essay</title><link>%[1]s/a/essay</link><guid>%[1]s/a/essay</guid><description>&lt;p&gt;%[4]s&lt;/p&gt;</description></item>
</channel></rss>`, blog.URL, elsewhere, commentary, essay)
			return
		}
		pages.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(articleHTML("essay-page"))) // not the essay the feed carries
	}))
	t.Cleanup(blog.Close)

	svc, st, uid := newService(t)
	f := fetch.New(fetch.Options{AllowPrivate: true})
	svc.Fetch, svc.Extract = f, &extract.Extractor{Fetch: f}
	ctx := context.Background()
	id, _ := st.AddFeed(ctx, uid, blog.URL+"/feed", "", nil)
	st.SetFeedFullText(ctx, uid, id, true)
	feed, _ := st.Feed(ctx, uid, id)
	svc.Poll(ctx, *feed)

	list, _, _ := st.Entries(ctx, store.Filter{UserID: uid}, true)
	got := map[string]store.Entry{}
	for _, e := range list {
		got[e.Title] = e
	}
	if e := got["Link"]; e.FullText || !strings.Contains(e.Content, "My thoughts") || e.Permalink != blog.URL+"/link" {
		t.Fatalf("link post: fullText=%v permalink=%q %q", e.FullText, e.Permalink, e.Content)
	}
	if e := got["Aggregated"]; !e.FullText || e.Permalink == "" {
		t.Fatalf("aggregator item not fetched: %+v", e)
	}
	if e := got["Essay"]; e.FullText || !strings.Contains(e.Content, "every word of it") {
		t.Fatalf("whole article replaced: %q", e.Content)
	}
	if pages.Load() != 2 { // the aggregated article and the essay's page
		t.Fatalf("fetched %d pages, want 2", pages.Load())
	}
}

func TestKeepFeedText(t *testing.T) {
	whole := "<p>" + strings.Repeat("Every sentence of the essay is here in the feed already. ", 20) + "</p>"
	longer := &extract.Result{Text: strings.Repeat("Every sentence of the essay is here in the feed already. ", 20) + strings.Repeat("And the page has a further part. ", 30)}
	same := &extract.Result{Text: strings.Repeat("Every sentence of the essay is here in the feed already. ", 20)}
	other := &extract.Result{Text: strings.Repeat("Another article entirely, from the second half. ", 40)}
	for _, c := range []struct {
		name string
		feed string
		r    *extract.Result
		keep bool
	}{
		{"short summary", "<p>A short summary…</p>", other, false},
		{"opening, page has it", "<p>" + strings.Repeat("Every sentence of the essay is here in the feed already. ", 8) + "Read more</p>", longer, false},
		{"opening, page lacks it", "<p>" + strings.Repeat("Every sentence of the essay is here in the feed already. ", 8) + "Read more</p>", other, true},
		{"whole, page adds to it", whole, longer, false},
		{"whole, page adds nothing", whole, same, true},
		{"whole, page is other text", whole, other, true},
	} {
		if got := keepFeedText(c.feed, c.r); got != c.keep {
			t.Errorf("%s: keep %v, want %v", c.name, got, c.keep)
		}
	}
}

package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/extract"
	"github.com/arenzana/correspondent/internal/pagefeed"
	"github.com/arenzana/correspondent/internal/store"
)

// summarySite serves a feed of one-line summaries, a listing page with no
// feed (articles under /posts/), and the article pages.
func summarySite(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			var b strings.Builder
			b.WriteString(`<rss version="2.0"><channel><title>Summaries</title>`)
			for _, n := range []string{"1", "2", "3", "4"} {
				fmt.Fprintf(&b, `<item><title>Post %s</title><link>%s/posts/%s</link><guid>%s</guid><description>Short teaser %s… Read more</description></item>`, n, srv.URL, n, n, n)
			}
			b.WriteString(`</channel></rss>`)
			w.Write([]byte(b.String()))
		case "/blog":
			b := `<html><head><title>Blog</title></head><body>`
			for _, n := range []string{"1", "2", "3"} {
				b += fmt.Sprintf(`<div><a href="/posts/%s"><h2>A post worth reading, number %s</h2></a><time datetime="2026-10-0%s">x</time></div>`, n, n, n)
			}
			w.Write([]byte(b + `</body></html>`))
		default:
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(articleHTML(strings.TrimPrefix(r.URL.Path, "/posts/"))))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAutoFullTextForSummaryFeeds(t *testing.T) {
	site := summarySite(t)
	svc, st, uid := newService(t)
	svc.Extract = &extract.Extractor{Fetch: svc.Fetch}
	ctx := context.Background()
	f, err := svc.SubscribeAuto(ctx, uid, site.URL+"/feed", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.Page || !f.FullText {
		t.Fatalf("summary feed: %+v", f)
	}
	// The backfill runs in the background.
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, _, _ := st.Entries(ctx, store.Filter{UserID: uid, FeedID: f.ID, Limit: 10}, true)
		full := 0
		for _, e := range entries {
			if e.FullText && strings.Contains(e.Content, "lives only on its web page") {
				full++
			}
		}
		if full == len(entries) && full > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d entries in full", full, len(entries))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSubscribeAutoPages(t *testing.T) {
	site := summarySite(t)
	svc, st, uid := newService(t)
	svc.Extract = &extract.Extractor{Fetch: svc.Fetch}
	ctx := context.Background()

	// No AI: the link pattern is used, and the parser saved for everyone.
	f, err := svc.SubscribeAuto(ctx, uid, site.URL+"/blog", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.ParserFor(ctx, site.URL+"/blog/")
	if err != nil || !f.Page || p.Method != pagefeed.MethodLinks || p.CreatedBy != uid {
		t.Fatalf("feed %+v parser %+v %v", f, p, err)
	}
	tree, _ := st.Feeds(ctx, uid)
	if len(tree) != 1 || tree[0].Total != 3 {
		t.Fatalf("%+v", tree)
	}

	// With AI set up, an app subscription prefers the AI's parser over a
	// link pattern; AI failures fall back to the pattern.
	other, _ := st.CreateUser(ctx, "v", "password-123")
	var asked atomic.Int32
	svc.AIFor = func(ctx context.Context, userID int64) (*pagefeed.Suggester, error) {
		asked.Add(1)
		return nil, errors.New("AI is down")
	}
	if _, err := svc.SubscribeAuto(ctx, other, site.URL+"/blog", "", nil); err != nil {
		t.Fatal(err)
	}
	// The pattern was already shared, so the AI wasn't needed.
	if asked.Load() != 0 {
		t.Errorf("AI asked %d times for a page with a shared parser", asked.Load())
	}

	// A page nobody can read: the AI's error is reported.
	if _, err := svc.SubscribeAuto(ctx, other, site.URL+"/nothing-here", "", nil); err == nil || !strings.Contains(err.Error(), "AI is down") {
		t.Errorf("unreadable page: %v", err)
	}
	svc.AIFor = nil
	if _, err := svc.SubscribeAuto(ctx, other, site.URL+"/nothing-here", "", nil); !errors.Is(err, ErrNoFeed) {
		t.Errorf("no AI: %v", err)
	}
}

func TestBuiltinParsers(t *testing.T) {
	svc, st, uid := newService(t)
	ctx := context.Background()
	if err := svc.SeedBuiltins(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := st.ParserFor(ctx, "https://www.anthropic.com/news")
	if err != nil || !p.Builtin || p.Method != pagefeed.MethodCSS {
		t.Fatalf("%+v %v", p, err)
	}
	// A reader's rebuild replaces it and is never overwritten by seeding.
	if _, err := st.SaveParser(ctx, "https://anthropic.com/news/", `{"method":"links","prefix":"/news/"}`, "links", uid); err != nil {
		t.Fatal(err)
	}
	svc.SeedBuiltins(ctx)
	if p, _ := st.ParserFor(ctx, "https://www.anthropic.com/news"); p.Builtin || p.Method != "links" {
		t.Errorf("seeding overwrote a rebuilt parser: %+v", p)
	}
	for in, want := range map[string]string{
		"openai.com":              "https://openai.com/news/rss.xml",
		"https://anthropic.com/":  "https://www.anthropic.com/news",
		"https://example.com/abc": "https://example.com/abc",
	} {
		if got := resolveBuiltin(in); got != want {
			t.Errorf("%s → %s", in, got)
		}
	}
	if store.ParserKey("HTTPS://WWW.Example.com/Blog/?p=2#x") != "https://example.com/Blog?p=2" {
		t.Errorf("key %s", store.ParserKey("HTTPS://WWW.Example.com/Blog/?p=2#x"))
	}
}

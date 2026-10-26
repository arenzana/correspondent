package pagefeed

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/arenzana/correspondent/internal/feed"
	"github.com/arenzana/correspondent/internal/fetch"
)

// TestSurvey reports, per page, whether it advertises a feed and what exact
// detection finds: CORRESPONDENT_SURVEY="https://…" go test -run Survey ./internal/pagefeed
func TestSurvey(t *testing.T) {
	urls := strings.Fields(os.Getenv("CORRESPONDENT_SURVEY"))
	if len(urls) == 0 {
		t.Skip("set CORRESPONDENT_SURVEY")
	}
	f := fetch.New(fetch.Options{})
	for _, u := range urls {
		resp, err := f.Get(context.Background(), fetch.Request{URL: u})
		if err != nil {
			t.Logf("%-50s fetch error: %v", u, err)
			continue
		}
		feeds := feed.Discover(resp.Body, resp.FinalURL)
		r, p, err := Detect(resp.Body, resp.FinalURL)
		n, first := 0, ""
		if err == nil {
			n = len(p.Items)
			first = p.Items[0].Title
		}
		t.Logf("%-50s feeds=%v detect=%s%s items=%d first=%q", u, feeds, r.Method, r.Prefix, n, first)
	}
}

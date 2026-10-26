package extract

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/fetch"
)

// TestLive extracts real pages. It runs only when CORRESPONDENT_LIVE_URLS is
// set (space-separated), since it needs the internet.
func TestLive(t *testing.T) {
	urls := strings.Fields(os.Getenv("CORRESPONDENT_LIVE_URLS"))
	if len(urls) == 0 {
		t.Skip("set CORRESPONDENT_LIVE_URLS to run")
	}
	x := &Extractor{Fetch: fetch.New(fetch.Options{})}
	for _, u := range urls {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		r, err := x.Article(ctx, u)
		cancel()
		if err != nil {
			t.Errorf("%s: %v", u, err)
			continue
		}
		words := len(strings.Fields(r.Text))
		t.Logf("%s\n    %d words, image=%v, starts: %q", u, words, r.ImageURL != "", r.Text[:min(len(r.Text), 140)])
	}
}

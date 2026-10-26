package pagefeed

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/arenzana/correspondent/internal/fetch"
)

// TestLive runs detection against real pages:
// CORRESPONDENT_LIVE_PAGES="https://example.com/blog https://…" go test -run Live ./internal/pagefeed
func TestLive(t *testing.T) {
	urls := strings.Fields(os.Getenv("CORRESPONDENT_LIVE_PAGES"))
	if len(urls) == 0 {
		t.Skip("set CORRESPONDENT_LIVE_PAGES")
	}
	f := fetch.New(fetch.Options{})
	for _, u := range urls {
		resp, err := f.Get(context.Background(), fetch.Request{URL: u})
		if err != nil {
			t.Errorf("%s: %v", u, err)
			continue
		}
		r, p, err := Detect(resp.Body, resp.FinalURL)
		if err != nil {
			t.Logf("%s: %v (article page: %v, parents %v)", u, err, IsArticle(resp.Body), Parents(resp.FinalURL))
			continue
		}
		t.Logf("%s → %s %q: %q, %d items", u, r.Method, r.Prefix, p.Title, len(p.Items))
		for _, it := range p.Items[:min(len(p.Items), 5)] {
			t.Logf("    %s | %s | %s", it.PublishedAt.Format("2006-01-02"), it.Title, it.URL)
		}
	}
}

// TestLiveAI asks the AI to read real pages (it costs a little):
// CORRESPONDENT_LIVE_AI_PAGES="https://…" AI_API_KEY=… [AI_PROVIDER=openai AI_MODEL=… AI_BASE_URL=…]
// go test -run LiveAI ./internal/pagefeed
func TestLiveAI(t *testing.T) {
	urls := strings.Fields(os.Getenv("CORRESPONDENT_LIVE_AI_PAGES"))
	if len(urls) == 0 || os.Getenv("AI_API_KEY") == "" {
		t.Skip("set CORRESPONDENT_LIVE_AI_PAGES and AI_API_KEY")
	}
	provider := os.Getenv("AI_PROVIDER")
	if provider == "" {
		provider = ProviderAnthropic
	}
	f := fetch.New(fetch.Options{})
	s, err := NewSuggester(Config{Provider: provider, Model: os.Getenv("AI_MODEL"), BaseURL: os.Getenv("AI_BASE_URL")},
		os.Getenv("AI_API_KEY"), f.Transport())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, u := range urls {
		resp, err := f.Get(context.Background(), fetch.Request{URL: u})
		if err != nil {
			t.Errorf("%s: %v", u, err)
			continue
		}
		outline, _ := Outline(resp.Body)
		r, p, err := s.Suggest(context.Background(), resp.Body, resp.FinalURL)
		if err != nil {
			t.Errorf("%s (outline %d bytes): %v", u, len(outline), err)
			continue
		}
		t.Logf("%s (outline %d bytes) → %+v, %d items", u, len(outline), r, len(p.Items))
		for _, it := range p.Items[:min(len(p.Items), 5)] {
			t.Logf("    %s | %s | %s", it.PublishedAt.Format("2006-01-02"), it.Title, it.URL)
		}
	}
}

package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/arenzana/correspondent/internal/typesafe"
)

// fakeJev answers that a block is the website's when its text says
// "SITE", unsure when it says "MAYBE", and the article's otherwise.
func fakeJev(t *testing.T, calls *atomic.Int32) *typesafe.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			State struct{ Block string } `json:"state"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		p := 0.02
		switch {
		case strings.Contains(req.State.Block, "SITE"):
			p = 0.97
		case strings.Contains(req.State.Block, "MAYBE"):
			p = 0.7
		}
		json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"material": map[string]any{"type": "noul", "noul": p}}})
	}))
	t.Cleanup(srv.Close)
	return &typesafe.Client{Key: "k", URL: srv.URL}
}

func TestDropMaterialPeelsEnds(t *testing.T) {
	var calls atomic.Int32
	svc, _, _ := newService(t)
	svc.Jev = fakeJev(t, &calls)
	body := strings.Repeat("words of the article ", 5)
	html := strings.Join([]string{
		`<audio controls src="https://a.example/a.mp3"></audio>`,
		"<p>SITE byline</p>",
		"<p>" + body + "one</p>",
		"<p>SITE a quote about the site, in the middle</p>",
		"<p>" + body + "two</p>",
		"<p>" + body + "three</p>",
		"<p>" + body + "four</p>",
		"<p>MAYBE a closing thought</p>",
		"<p>SITE newsletter</p>",
		"<p></p>",
		"<p>SITE related</p>",
	}, "\n")
	got := svc.dropMaterial(context.Background(), "T", "Site", html)
	for _, want := range []string{"<audio", "one", "in the middle", "four", "MAYBE"} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q: %s", want, got)
		}
	}
	for _, gone := range []string{"newsletter", "related"} {
		if strings.Contains(got, gone) {
			t.Errorf("kept %q: %s", gone, got)
		}
	}

	// Without a key nothing is asked.
	svc.Jev = nil
	if got := svc.dropMaterial(context.Background(), "T", "Site", html); got != html {
		t.Error("changed without a key")
	}
}

func TestDropMaterialKeepsOnwardLinks(t *testing.T) {
	var calls atomic.Int32
	svc, _, _ := newService(t)
	svc.Jev = fakeJev(t, &calls)
	html := "<p>" + strings.Repeat("A summary of the article. ", 4) + "</p>\n<p>Two.</p>\n<p>Three.</p>\n<p>SITE Read more</p>"
	if got := svc.dropMaterial(context.Background(), "T", "Site", html); !strings.Contains(got, "Read more") {
		t.Fatalf("dropped the onward link: %s", got)
	}
}

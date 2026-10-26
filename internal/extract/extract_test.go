package extract

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arenzana/correspondent/internal/fetch"
)

const page = `<!doctype html><html><head><title>BlueBird 757</title>
<meta property="og:image" content="/img/hero.jpg"></head><body>
<nav><a href="/">Home</a> <a href="/shop">Shop</a> <a href="/login">Log in</a></nav>
<div class="cookie-banner">We use cookies. <button onclick="accept()">Accept</button></div>
<article>
<h1>BlueBird Simulations 757 Passenger</h1>
<p>The long-awaited BlueBird Simulations 757 Passenger add-on for Microsoft Flight Simulator is now available.
It brings a fully modelled cabin, custom systems and a new flight model developed with real pilots.</p>
<p>The aircraft ships with three liveries and a livery kit, and supports both MSFS 2020 and MSFS 2024.
Owners of the freighter version receive a discount, and updates are free for the life of the product.</p>
<p><img src="/img/cockpit.jpg" alt="Cockpit"> The flight deck has working overhead panels.</p>
<script>track()</script>
</article>
<aside>Related products: headsets, yokes, rudder pedals, and more.</aside>
<footer>Copyright Just Flight. All rights reserved.</footer>
</body></html>`

func server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/article":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(page))
		case "/pdf":
			w.Header().Set("Content-Type", "application/pdf")
			w.Write([]byte("%PDF-1.7"))
		case "/stub":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body><p>Subscribe to read.</p></body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestArticle(t *testing.T) {
	srv := server(t)
	x := &Extractor{Fetch: fetch.New(fetch.Options{AllowPrivate: true})}
	r, err := x.Article(context.Background(), srv.URL+"/article")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fully modelled cabin", "three liveries", `src="` + srv.URL + `/img/cockpit.jpg"`} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("missing %q in:\n%s", want, r.HTML)
		}
	}
	for _, unwanted := range []string{"<script", "track()", "onclick", "Log in", "rudder pedals", "Copyright"} {
		if strings.Contains(r.HTML, unwanted) {
			t.Errorf("kept %q:\n%s", unwanted, r.HTML)
		}
	}
	if r.ImageURL == "" {
		t.Error("no lead image")
	}
}

func TestRefusals(t *testing.T) {
	srv := server(t)
	x := &Extractor{Fetch: fetch.New(fetch.Options{AllowPrivate: true})}
	ctx := context.Background()
	if _, err := x.Article(ctx, srv.URL+"/pdf"); err == nil || !strings.Contains(err.Error(), "isn't HTML") {
		t.Errorf("pdf: %v", err)
	}
	if _, err := x.Article(ctx, srv.URL+"/stub"); !errors.Is(err, ErrNotArticle) {
		t.Errorf("stub: %v", err)
	}
	if _, err := x.Article(ctx, srv.URL+"/missing"); err == nil {
		t.Error("404 accepted")
	}
	if _, err := x.Article(ctx, "javascript:alert(1)"); err == nil {
		t.Error("non-http address accepted")
	}
	// The default fetcher refuses private addresses, here 127.0.0.1.
	strict := &Extractor{Fetch: fetch.New(fetch.Options{})}
	if _, err := strict.Article(ctx, srv.URL+"/article"); !errors.Is(err, fetch.ErrBlocked) {
		t.Errorf("private address: %v", err)
	}
}

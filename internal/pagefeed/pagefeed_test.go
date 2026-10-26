package pagefeed

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const base = "https://example.com/research"

func titles(t *testing.T, body, method string, r Rules) []string {
	t.Helper()
	p, err := Parse([]byte(body), base, r)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	var out []string
	for _, it := range p.Items {
		out = append(out, it.Title)
	}
	return out
}

func TestHFeed(t *testing.T) {
	page := `<html><head><title>Notes</title></head><body><div class="h-feed">
	<article class="h-entry"><a class="u-url p-name" href="/notes/one">First note title</a>
	  <time class="dt-published" datetime="2026-10-01T10:00:00Z">Oct 1</time><p class="p-summary">About one.</p></article>
	<article class="h-entry"><h2 class="p-name">Second note title</h2><a class="u-url" href="https://example.com/notes/two">link</a></article>
	<article class="h-entry"><a href="javascript:alert(1)" class="u-url p-name">Script link</a></article>
	<article class="h-entry"><a href="https://elsewhere.org/x" class="u-url p-name">Another site</a></article>
	</div></body></html>`
	r, p, err := Detect([]byte(page), base)
	if err == nil {
		t.Fatalf("two valid entries detected as a feed: %+v", r)
	}
	p, err = Parse([]byte(page), base, Rules{Method: MethodHFeed})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Items[0].URL != "https://example.com/notes/one" || p.Items[1].Title != "Second note title" {
		t.Fatalf("%+v", p.Items)
	}
	if !p.Items[0].PublishedAt.Equal(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)) || p.Items[0].Excerpt != "About one." {
		t.Errorf("%+v", p.Items[0])
	}
	if p.Title != "Notes" || p.SiteURL != "https://example.com/" {
		t.Errorf("page %q %q", p.Title, p.SiteURL)
	}
}

func TestJSONLD(t *testing.T) {
	page := `<html><body><script type="application/ld+json">{"@context":"https://schema.org","@graph":[
	  {"@type":"WebSite","name":"Example"},
	  {"@type":"ItemList","itemListElement":[
	    {"@type":"ListItem","position":1,"item":{"@type":"BlogPosting","headline":"Paper one","url":"/research/one","datePublished":"2026-09-01","image":{"url":"/img/1.png"}}},
	    {"@type":"ListItem","position":2,"url":"/research/two","name":"Paper two"},
	    {"@type":"ListItem","position":3,"item":{"@type":["Article"],"name":"Paper three","url":"https://example.com/research/three"}}
	  ]}]}</script><script type="application/ld+json">not json</script></body></html>`
	r, p, err := Detect([]byte(page), base)
	if err != nil || r.Method != MethodJSONLD {
		t.Fatalf("%+v %v", r, err)
	}
	if got := len(p.Items); got != 3 {
		t.Fatalf("%d items", got)
	}
	if p.Items[0].ImageURL != "https://example.com/img/1.png" || p.Items[0].PublishedAt.IsZero() || p.Items[1].Title != "Paper two" {
		t.Errorf("%+v", p.Items)
	}
}

const listing = `<html><head><title>Research \ Example</title><meta name="description" content="Our papers."></head><body>
<nav><a href="/research/team">Meet the research team behind it all</a><a href="/about">About this organisation</a></nav>
<main>
 <div class="card"><a href="/research/alpha"><img src="/a.png"><h3>Alpha: a careful study</h3></a><time datetime="2026-10-03">Oct 3</time></div>
 <div class="card"><a href="/research/beta"><h3>Beta results, explained</h3></a><time datetime="2026-10-02">Oct 2</time>
   <p>We revisit <a href="/research/alpha">the alpha study we wrote</a>.</p></div>
 <div class="card"><a href="/research/gamma#top"><h3>Gamma in the wild</h3></a><span>October 1, 2026</span></div>
 <div class="card"><a href="/tag/safety">All posts tagged safety here</a></div>
</main></body></html>`

func TestLinks(t *testing.T) {
	r, p, err := Detect([]byte(listing), base)
	if err != nil || r.Method != MethodLinks || r.Prefix != "/research/" {
		t.Fatalf("%+v %v", r, err)
	}
	got := []string{}
	for _, it := range p.Items {
		got = append(got, it.Title+" <"+it.URL+">")
	}
	// One item per post, headline titles, navigation and mentions dropped.
	want := []string{
		"Alpha: a careful study <https://example.com/research/alpha>",
		"Beta results, explained <https://example.com/research/beta>",
		"Gamma in the wild <https://example.com/research/gamma>",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
	if p.Items[0].ImageURL != "https://example.com/a.png" || p.Items[0].PublishedAt.Format("2006-01-02") != "2026-10-03" {
		t.Errorf("%+v", p.Items[0])
	}
	if p.Title != `Research \ Example` || p.Description != "Our papers." {
		t.Errorf("page %q %q", p.Title, p.Description)
	}
	for _, it := range p.Items {
		if it.GUID != it.URL || it.Enclosures != "[]" || it.ContentHash == "" {
			t.Errorf("%+v", it)
		}
	}
}

func TestCSS(t *testing.T) {
	r := Rules{Method: MethodCSS, Item: "div.card", Link: "a", Title: "h3", Date: "time", Summary: "p"}
	got := titles(t, listing, "css", r)
	// Every card matches, the tag card too: CSS rules are taken as given.
	if len(got) != 4 || got[2] != "Gamma in the wild" {
		t.Fatalf("%q", got)
	}
	p, _ := Parse([]byte(listing), base, r)
	if p.Items[1].Excerpt != "We revisit the alpha study we wrote." || !strings.Contains(p.Items[1].Content, "We revisit") {
		t.Errorf("%+v", p.Items[1])
	}
	// The item itself as the link; titles default to the link's text.
	got = titles(t, listing, "css", Rules{Method: MethodCSS, Item: "main .card > a"})
	if len(got) != 4 || got[0] != "Alpha: a careful study" {
		t.Errorf("%q", got)
	}
	if _, err := Parse([]byte(listing), base, Rules{Method: MethodCSS, Item: "li.none"}); !errors.Is(err, ErrNoArticles) {
		t.Errorf("no match: %v", err)
	}
}

func TestValidate(t *testing.T) {
	bad := []Rules{
		{},
		{Method: "script"},
		{Method: MethodCSS},
		{Method: MethodCSS, Item: "div[", Link: ""},
		{Method: MethodCSS, Item: "div", Title: "h3:contains("},
		{Method: MethodCSS, Item: strings.Repeat("div ", 100)},
		{Method: MethodLinks, Prefix: "research"},
	}
	for _, r := range bad {
		if r.Validate() == nil {
			t.Errorf("accepted %+v", r)
		}
	}
	good := Rules{Method: MethodCSS, Item: "article.post", Link: "h2 > a", Date: "time"}
	back, err := Decode(good.Encode())
	if err != nil || back != good {
		t.Errorf("%+v %v", back, err)
	}
	if _, err := Decode(`{"method":"css"}`); err == nil {
		t.Error("decoded invalid rules")
	}
}

func TestArticlesAndParents(t *testing.T) {
	if !IsArticle([]byte(`<meta property="og:type" content="article">`)) || IsArticle([]byte(`<meta property="og:type" content="website">`)) {
		t.Error("IsArticle")
	}
	got := Parents("https://example.com/research/some-post?x=1#y")
	if strings.Join(got, " ") != "https://example.com/research/ https://example.com/" {
		t.Errorf("%q", got)
	}
	if len(Parents("https://example.com/")) != 0 {
		t.Error("parents of the root")
	}
}

func TestParseDate(t *testing.T) {
	for in, want := range map[string]string{
		"2026-10-03":            "2026-10-03",
		"2026-10-03T08:00:00Z":  "2026-10-03",
		"October 3, 2026":       "2026-10-03",
		"Published Oct 3, 2026": "2026-10-03",
		"3 Oct 2026":            "2026-10-03",
		"yesterday":             "0001-01-01",
		"2099-01-01":            "0001-01-01", // future dates are not trusted
	} {
		if got := parseDate(in).Format("2006-01-02"); got != want {
			t.Errorf("%q → %s", in, got)
		}
	}
}

func TestOutline(t *testing.T) {
	page := `<html><head><title>T</title><script>secret()</script><style>.x{}</style></head><body>
	<div class="list" id="main" onclick="evil()" data-x="1"><a href="/p/1" style="color:red">` + strings.Repeat("word ", 50) + `</a>
	<svg><path d="M0"/></svg><time datetime="2026-10-01">Oct 1</time><form><input name="q"></form></div></body></html>`
	o, err := Outline([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<div class="list" id="main">`, `<a href="/p/1">`, `<time datetime="2026-10-01">`, "…"} {
		if !strings.Contains(o, want) {
			t.Errorf("missing %q in\n%s", want, o)
		}
	}
	for _, unwanted := range []string{"secret", ".x{}", "onclick", "evil", "data-x", "style", "<svg", "<form", "<input"} {
		if strings.Contains(o, unwanted) {
			t.Errorf("kept %q in\n%s", unwanted, o)
		}
	}
	big, _ := Outline([]byte("<div>" + strings.Repeat(`<p class="x">text</p>`, 20000) + "</div>"))
	if len(big) > maxOutline+1000 {
		t.Errorf("outline %d bytes", len(big))
	}
}

func TestBuiltins(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range Builtins() {
		if b.Name == "" || len(b.Match) == 0 || (b.Feed == "") == (b.Page == "") {
			t.Errorf("%+v: needs a name, matches, and a feed or a page", b)
		}
		if b.Page != "" && (b.Rules == nil || b.Rules.Validate() != nil) {
			t.Errorf("%s: invalid rules", b.Name)
		}
		for _, m := range b.Match {
			if seen[m] || m != matchKey(m) {
				t.Errorf("%s: match %q duplicated or not normalized", b.Name, m)
			}
			seen[m] = true
		}
	}
	for in, want := range map[string]string{
		"openai.com":                         "OpenAI News",
		"https://www.Anthropic.com/news/":    "Anthropic News",
		"anthropic.com/research":             "Anthropic Research",
		"https://anthropic.com/research/foo": "",
		"example.com":                        "",
	} {
		got := ""
		if b := LookupBuiltin(in); b != nil {
			got = b.Name
		}
		if got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
}

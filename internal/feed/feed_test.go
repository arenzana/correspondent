package feed

import (
	"strings"
	"testing"
)

const rss = `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>Example &amp; Co</title><link>https://example.com/</link><description>Notes</description>
<item><title>First</title><link>/posts/1</link><guid>urn:1</guid>
<pubDate>Mon, 26 Oct 2026 09:00:00 +0000</pubDate>
<description><![CDATA[<p>Hello <img src="/a.png"></p>]]></description>
<enclosure url="https://cdn.example.com/ep1.mp3" type="audio/mpeg" length="123"/></item>
<item><title>No guid</title><link>https://example.com/posts/2</link></item>
<item><description>Only a body, no title or link</description></item>
</channel></rss>`

func TestParseRSS(t *testing.T) {
	p, err := Parse([]byte(rss), "https://example.com/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Example & Co" || p.SiteURL != "https://example.com/" {
		t.Fatalf("feed meta: %+v", p)
	}
	if len(p.Items) != 3 {
		t.Fatalf("items = %d", len(p.Items))
	}
	a := p.Items[0]
	if a.GUID != "urn:1" || a.URL != "https://example.com/posts/1" || a.ImageURL != "https://example.com/a.png" {
		t.Errorf("item 0: %+v", a)
	}
	if !strings.Contains(a.Enclosures, "ep1.mp3") || a.PublishedAt.IsZero() {
		t.Errorf("item 0 enclosure/date: %+v", a)
	}
	if p.Items[1].GUID != "https://example.com/posts/2" {
		t.Errorf("link fallback guid: %q", p.Items[1].GUID)
	}
	c := p.Items[2]
	if !strings.HasPrefix(c.GUID, "sha256:") || c.Title != "Only a body, no title or link" {
		t.Errorf("hash fallback: %+v", c)
	}
	// Re-parsing must produce identical identities and hashes.
	again, _ := Parse([]byte(rss), "https://example.com/feed.xml")
	for i := range p.Items {
		if p.Items[i].GUID != again.Items[i].GUID || p.Items[i].ContentHash != again.Items[i].ContentHash {
			t.Errorf("item %d identity not stable", i)
		}
	}
}

func TestDuplicateGUIDsKeepFirst(t *testing.T) {
	doc := `<rss version="2.0"><channel><title>t</title>
<item><title>August update</title><link>https://x.example/page</link></item>
<item><title>Older update</title><link>https://x.example/page</link></item>
<item><title>Other</title><link>https://x.example/other</link></item></channel></rss>`
	p, err := Parse([]byte(doc), "https://x.example/feed")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Items[0].Title != "August update" {
		t.Fatalf("items %+v", p.Items)
	}
}

func TestParseRejectsHTML(t *testing.T) {
	if _, err := Parse([]byte("<!DOCTYPE html><html><body>hi</body></html>"), "https://x.example/"); err != ErrNotFeed {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscover(t *testing.T) {
	page := `<html><head>
<link rel="alternate" type="application/rss+xml" href="/feed.xml">
<link rel="alternate" type="application/feed+json" href="https://x.example/feed.json">
<link rel="stylesheet" href="/s.css"></head><body></body></html>`
	got := Discover([]byte(page), "https://x.example/blog/")
	if len(got) != 2 || got[0] != "https://x.example/feed.xml" {
		t.Fatalf("got %v", got)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(rss))
	f.Add([]byte(`{"version":"https://jsonfeed.org/version/1.1","items":[{"id":"1","content_html":"<b>x</b>"}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		Parse(b, "https://example.com/feed")
	})
}

func TestSponsoredItems(t *testing.T) {
	p, err := Parse([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>B</title>
<item><title>Sponsor: Audio Hijack</title><link>https://b.example/1</link></item>
<item><title>A product we like</title><category>Sponsored</category><link>https://b.example/2</link></item>
<item><title>On sponsors</title><category>Business</category><link>https://b.example/3</link></item>
</channel></rss>`), "https://b.example/feed")
	if err != nil {
		t.Fatal(err)
	}
	var got []bool
	for _, it := range p.Items {
		got = append(got, it.Sponsored)
	}
	if len(got) != 3 || !got[0] || !got[1] || got[2] {
		t.Errorf("sponsored = %v, want [true true false]", got)
	}
}

func TestLinkPostPermalinks(t *testing.T) {
	atom := `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Links</title>
<link rel="alternate" type="text/html" href="https://www.links.example/"/>
<entry><title>About Apple's news</title><id>tag:links.example,2026:/linked//1</id>
<link rel="alternate" type="text/html" href="https://www.apple.com/newsroom/x"/>
<link rel="shorturl" type="text/html" href="http://lnk.example/a"/>
<link rel="related" type="text/html" href="https://links.example/linked/2026/10/27/x"/>
<content type="html">&lt;p&gt;Commentary.&lt;/p&gt;</content></entry>
<entry><title>An article</title><id>tag:links.example,2026:/2</id>
<link rel="alternate" type="text/html" href="https://links.example/2026/10/article"/>
<content type="html">&lt;p&gt;Words.&lt;/p&gt;</content></entry>
</feed>`
	p, err := Parse([]byte(atom), "https://links.example/feeds/main")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Items[0].Permalink; got != "https://links.example/linked/2026/10/27/x" {
		t.Errorf("rel=related permalink %q", got)
	}
	if got := p.Items[1].Permalink; got != "" {
		t.Errorf("own article has permalink %q", got)
	}

	rss := `<rss version="2.0"><channel><title>Lex</title><link>https://lex.example/</link>
<item><title>Linked</title><link>https://news.example/story?code=1</link><guid>https://lex.example/linked</guid><description>Hm.</description></item>
<item><title>Opaque</title><link>https://news.example/other</link><guid isPermaLink="false">urn:7</guid><description>Hm.</description></item>
</channel></rss>`
	if p, err = Parse([]byte(rss), "https://lex.example/feed"); err != nil {
		t.Fatal(err)
	}
	if got := p.Items[0].Permalink; got != "https://lex.example/linked" {
		t.Errorf("GUID permalink %q", got)
	}
	if got := p.Items[1].Permalink; got != "" {
		t.Errorf("no own page, permalink %q", got)
	}
}

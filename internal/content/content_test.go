package content

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func TestProcessSanitizes(t *testing.T) {
	raw := `<p onclick="x()">Hello <a href="/post">world</a></p>
<script>alert(1)</script>
<img src="pix.gif" width="1" height="1">
<img src="img/a.jpg" alt="A">
<a href="javascript:alert(1)">bad</a>
<iframe src="https://evil.example"></iframe>`
	r := Process(raw, "https://blog.example/2026/10/")
	for _, bad := range []string{"onclick", "<script", "alert(1)", "javascript:", "<iframe", "pix.gif"} {
		if strings.Contains(r.HTML, bad) {
			t.Errorf("sanitized HTML still contains %q:\n%s", bad, r.HTML)
		}
	}
	for _, good := range []string{`href="https://blog.example/post"`, `src="https://blog.example/2026/10/img/a.jpg"`, `loading="lazy"`, `rel="nofollow noreferrer noopener"`} {
		if !strings.Contains(r.HTML, good) {
			t.Errorf("sanitized HTML missing %q:\n%s", good, r.HTML)
		}
	}
	if r.ImageURL != "https://blog.example/2026/10/img/a.jpg" {
		t.Errorf("ImageURL = %q", r.ImageURL)
	}
	if r.Text != "Hello world bad" {
		t.Errorf("Text = %q", r.Text)
	}
}

func TestExcerpt(t *testing.T) {
	if got := Excerpt("short", 10); got != "short" {
		t.Errorf("got %q", got)
	}
	if got := Excerpt("the quick brown fox jumps over", 18); got != "the quick brown…" {
		t.Errorf("got %q", got)
	}
}

func TestPlainText(t *testing.T) {
	if got := PlainText("Tom &amp; <b>Jerry</b>"); got != "Tom & Jerry" {
		t.Errorf("got %q", got)
	}
}

func FuzzProcess(f *testing.F) {
	f.Add(`<p>hi <img src=x onerror=alert(1)></p>`, "https://a.example/")
	f.Add(`<a href="javascript:alert(1)">x</a><svg onload=alert(1)>`, "")
	f.Add(`<iframe src=//evil></iframe><style>*{}</style>onerror=`, "https://a.example/")
	f.Fuzz(func(t *testing.T, raw, base string) {
		r := Process(raw, base)
		if problem := unsafeHTML(r.HTML); problem != "" {
			t.Fatalf("unsafe output (%s): %q", problem, r.HTML)
		}
	})
}

// unsafeHTML parses sanitized output the way a browser would and reports
// anything that could execute: dangerous elements, event handlers, or URL
// attributes with a scheme other than http(s)/mailto. Text that merely
// looks like markup ("onerror=") is fine; it renders as text.
func unsafeHTML(s string) string {
	nodes, err := html.ParseFragment(strings.NewReader(s), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "unparseable"
	}
	var problem string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Script, atom.Iframe, atom.Object, atom.Embed, atom.Style, atom.Form, atom.Base, atom.Meta, atom.Link:
				problem = "element " + n.Data
			}
			for _, a := range n.Attr {
				key := strings.ToLower(a.Key)
				if strings.HasPrefix(key, "on") {
					problem = "attribute " + key
				}
				switch key {
				case "href", "src", "action", "formaction", "poster", "srcset", "xlink:href":
					v := strings.ToLower(strings.TrimSpace(a.Val))
					if i := strings.Index(v, ":"); i >= 0 && !strings.HasPrefix(v, "http:") && !strings.HasPrefix(v, "https:") && !strings.HasPrefix(v, "mailto:") {
						if j := strings.IndexAny(v, "/?#"); j < 0 || j > i {
							problem = key + "=" + v
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return problem
}

func TestUnsafeHTMLOracle(t *testing.T) {
	for in, unsafe := range map[string]bool{
		`onerror=`:                       false,
		`<p>text</p>`:                    false,
		`<a href="https://x/a:b">x</a>`:  false,
		`<a href="/rel?x=a:b">x</a>`:     false,
		`<img onerror="x">`:              true,
		`<script>x</script>`:             true,
		`<a href="javascript:x">y</a>`:   true,
		`<a href=" JavaScript:x">y</a>`:  true,
		`<img src="data:image/png,AAA">`: true,
	} {
		if got := unsafeHTML(in) != ""; got != unsafe {
			t.Errorf("unsafeHTML(%q) = %v, want %v", in, got, unsafe)
		}
	}
}

func TestProcessDropsUnvalidatedURLAttributes(t *testing.T) {
	r := Process(`<img src="data:image/png;base64,AAAA" srcset="javascript:alert(1) 1x"><video poster="javascript:x" src="https://a.example/v.mp4"></video>`, "https://a.example/")
	for _, bad := range []string{"srcset", "poster", "javascript:", "data:"} {
		if strings.Contains(r.HTML, bad) {
			t.Errorf("output contains %q: %s", bad, r.HTML)
		}
	}
	if r.ImageURL != "" {
		t.Errorf("non-http lead image accepted: %q", r.ImageURL)
	}
}

func TestLooksPartial(t *testing.T) {
	words := func(n int) string { return strings.Repeat("word ", n) }
	for html, want := range map[string]bool{
		"":                           true,
		"<p>A short teaser.</p>":     true,
		"<p>" + words(150) + "…</p>": true,
		"<p>" + words(150) + "</p><p><a href=x>Continue reading →</a></p><p><a href=y>Get the full-text RSS feed →</a></p>": true,
		"<p>" + words(150) + ".</p>":                                  false,
		"<p>" + words(600) + "…</p>":                                  false,
		"<p>" + words(60) + " read more here " + words(150) + ".</p>": false,
	} {
		if got := LooksPartial(html); got != want {
			t.Errorf("LooksPartial(%.60q…) = %v", html, got)
		}
	}
}

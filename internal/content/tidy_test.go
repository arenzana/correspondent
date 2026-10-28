package content

import (
	"strconv"
	"strings"
	"testing"
)

func TestTidyStructure(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"wrappers flatten", `<div><section><p>One.</p><div><p>Two.</p></div></section></div>`, "<p>One.</p>\n<p>Two.</p>"},
		{"loose text gathered", `<div>Loose <em>text</em><br><p>Para.</p></div>`, "<p>Loose <em>text</em></p>\n<p>Para.</p>"},
		{"empty blocks dropped", `<p> </p><p>Kept.</p><ul><li></li></ul><p><img src="https://x.example/a.png"></p>`,
			"<p>Kept.</p>\n<p><img src=\"https://x.example/a.png\"/></p>"},
		{"edge breaks dropped", `<p><br>Text.<br><br></p>`, "<p>Text.</p>"},
		{"title heading dropped, levels from h2", `<h1>The Title!</h1><p>a</p><h3>Part</h3><p>b</p><h5>Sub</h5>`,
			"<p>a</p>\n<h2>Part</h2>\n<p>b</p>\n<h3>Sub</h3>"},
		{"h1 sections become h2", `<p>a</p><h1>One</h1><p>b</p><h2>Two</h2>`, "<p>a</p>\n<h2>One</h2>\n<p>b</p>\n<h3>Two</h3>"},
	} {
		if got := Tidy(c.in, "The title"); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestTidyWebsiteMaterial(t *testing.T) {
	n := 0
	body := func() string {
		n++
		return "<p>" + strings.Repeat("The article's own words, at some length. ", 8) + strconv.Itoa(n) + "</p>"
	}
	in := strings.Join([]string{
		`<ul><li><a href="https://s.example/">Home</a></li><li><a href="https://s.example/a">About</a></li><li><a href="https://s.example/e">Ethics</a></li></ul>`,
		"<p>by Jane Doe</p>",
		"<p>Oct 28, 2026, 2:47 PM UTC</p>",
		body(), body(),
		"<p>We may earn a commission when you buy through links on our site.</p>",
		body(), body(), body(),
		"<p>Continue reading…</p>",
		"<p>Tags: one, two</p>",
		"<h2>Related articles</h2>",
		`<ul><li><a href="https://s.example/1">One story</a></li><li><a href="https://s.example/2">Another</a></li></ul>`,
	}, "")
	got := Tidy(in, "A title")
	for _, gone := range []string{"Home", "Jane Doe", "2:47", "commission", "Tags", "Related", "One story"} {
		if strings.Contains(got, gone) {
			t.Errorf("kept %q", gone)
		}
	}
	if !strings.Contains(got, "Continue reading") || strings.Count(got, "own words") != 40 {
		t.Errorf("lost the article or its onward link:\n%s", got)
	}
}

func FuzzTidy(f *testing.F) {
	f.Add(`<div><h1>T</h1>loose<p>x <img src=x onerror=alert(1)></p><ul><li></li></ul></div>`, "T")
	f.Add(`<span><a href="javascript:alert(1)">by Someone</a></span><svg onload=alert(1)>`, "")
	f.Fuzz(func(t *testing.T, raw, title string) {
		out := Tidy(Process(raw, "https://a.example/").HTML, title)
		if problem := unsafeHTML(out); problem != "" {
			t.Fatalf("unsafe output (%s): %q", problem, out)
		}
		if again := Tidy(out, title); again != out {
			t.Fatalf("not stable:\n%q\n%q", out, again)
		}
	})
}

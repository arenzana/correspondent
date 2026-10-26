package content

import (
	"strings"
	"testing"
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
	f.Fuzz(func(t *testing.T, raw, base string) {
		r := Process(raw, base)
		if strings.Contains(strings.ToLower(r.HTML), "<script") || strings.Contains(strings.ToLower(r.HTML), "onerror=") {
			t.Fatalf("unsafe output: %q", r.HTML)
		}
	})
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

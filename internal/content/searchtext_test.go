package content

import "testing"

func TestSearchText(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"<p>The end.</p><p>Next&nbsp;one &amp; more</p>", "The end. Next one & more"},
		{"<h2>Title</h2><script>var x = 1</script><style>p{}</style>body<br>line", "Title body line"},
		{`<a href="https://github.com/x">link</a> text`, "link text"},
		{"", ""},
	} {
		if got := SearchText(c.in, 1000); got != c.want {
			t.Errorf("SearchText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := SearchText("<p>"+"héllo wörld "+"</p>", 7); got != "héllo" {
		t.Errorf("cut = %q", got)
	}
}

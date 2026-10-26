package textify

import (
	"strings"
	"testing"
)

func TestMarkdown(t *testing.T) {
	in := `<h1>Title</h1><p>Hello <strong>bold</strong> and <em>soft</em> words, with <a href="/more">a link</a>
and <code>x := 1</code>.</p>
<p><img src="/img/abc/1600/x" alt="A chart"></p>
<blockquote><p>A quotation,<br>in two lines.</p></blockquote>
<ul><li>One</li><li>Two<ul><li>Two and a half</li></ul></li></ul>
<ol><li>First</li><li>Second</li></ol>
<pre><code>for {
	loop()
}</code></pre>
<figure><img src="https://cdn.example/p.jpg" alt=""><figcaption>The caption</figcaption></figure>
<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2|3</td></tr></table>
<p><a href="javascript:alert(1)">bad</a> <a href="https://x.example/">https://x.example/</a></p><hr><p>End.</p>`
	got := Markdown(in, "https://rss.example.org/")
	for _, want := range []string{
		"## Title\n",
		"Hello **bold** and _soft_ words, with [a link](https://rss.example.org/more) and `x := 1`.",
		"![A chart](https://rss.example.org/img/abc/1600/x)",
		"> A quotation,\n> in two lines.",
		"- One\n- Two\n  - Two and a half\n",
		"1. First\n2. Second\n",
		"```\nfor {\n\tloop()\n}\n```",
		"![](https://cdn.example/p.jpg)",
		"_The caption_",
		"| A | B |\n| --- | --- |\n| 1 | 2\\|3 |",
		"bad <https://x.example/>",
		"---\n",
		"End.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "javascript") || strings.Contains(got, "\n\n\n") {
		t.Errorf("unsafe link or loose spacing:\n%s", got)
	}
}

func TestPlainParagraphs(t *testing.T) {
	if got := Markdown("Just text", ""); got != "Just text\n" {
		t.Errorf("%q", got)
	}
	if got := Markdown(`<div>Loose <a href="https://a.example/">link</a> text<p>Then a paragraph.</p></div>`, ""); !strings.Contains(got, "Loose [link](https://a.example/) text\n\nThen a paragraph.") {
		t.Errorf("%q", got)
	}
}

func TestNumbered(t *testing.T) {
	got := Numbered(`<p>See <a href="/a">this</a>, <a href="https://b.example/">that</a> and <a href="/a">this again</a>.</p><p><img src="/img/p" alt="Chart"></p>`, "https://rss.example.org/")
	for _, want := range []string{
		`See this \[1\], that \[2\] and this again \[1\].`,
		`_\[Chart\]_ \[3\]`,
		"**Links**\n\n1. <https://rss.example.org/a>\n2. <https://b.example/>\n3. <https://rss.example.org/img/p>\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// Package content turns untrusted feed HTML into the sanitized HTML that is
// stored and rendered. Transforms (URL resolution, tracking-pixel removal,
// lazy images) run first on a parsed tree; bluemonday runs last, so nothing
// a transform produces can bypass the allowlist.
package content

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// Only attributes whose URLs bluemonday validates (src, href) may carry
	// URLs; srcset and poster are deliberately not allowed.
	p.AllowElements("figure", "figcaption", "mark", "small", "time", "details", "summary")
	p.AllowAttrs("loading", "decoding", "width", "height").OnElements("img")
	p.AllowElements("audio", "video")
	p.AllowAttrs("controls", "preload").OnElements("audio", "video")
	p.AllowAttrs("src").OnElements("audio", "video")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireParseableURLs(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.RequireNoReferrerOnFullyQualifiedLinks(true)
	return p
}()

// Result is the processed form of one article body.
type Result struct {
	HTML     string // sanitized, safe to render
	Text     string // plain text, for excerpts and search
	ImageURL string // first meaningful image, if any
}

// Process sanitizes raw HTML from a feed, resolving relative URLs against base.
func Process(raw, base string) Result {
	baseURL, _ := url.Parse(base)
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return Result{HTML: policy.Sanitize(raw)}
	}
	var (
		text  strings.Builder
		image string
		buf   bytes.Buffer
	)
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode && isTrackingPixel(c) {
				n.RemoveChild(c)
				c = next
				continue
			}
			c = next
		}
		switch n.Type {
		case html.TextNode:
			text.WriteString(n.Data)
		case html.ElementNode:
			switch n.DataAtom {
			case atom.Script, atom.Style, atom.Noscript:
				return
			case atom.P, atom.Br, atom.Div, atom.Li, atom.H1, atom.H2, atom.H3, atom.H4, atom.Blockquote:
				text.WriteByte(' ')
			}
			for i, a := range n.Attr {
				switch a.Key {
				case "href", "src":
					n.Attr[i].Val = resolve(baseURL, a.Val)
				}
			}
			if n.DataAtom == atom.Img {
				setAttr(n, "loading", "lazy")
				setAttr(n, "decoding", "async")
				if src := attr(n, "src"); image == "" && isHTTP(src) {
					image = src
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	// Hang the fragment off a root so top-level nodes get the same filtering.
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	walk(root)
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		html.Render(&buf, c)
	}
	return Result{
		HTML:     strings.TrimSpace(policy.Sanitize(buf.String())),
		Text:     collapse(text.String()),
		ImageURL: image,
	}
}

// PlainText strips all markup, for titles and author names.
func PlainText(s string) string {
	return collapse(html.UnescapeString(bluemonday.StrictPolicy().Sanitize(s)))
}

// sponsoredTitle matches how blogs label paid posts: "Sponsor: …",
// "[Sponsor] …", "Sponsored by …" and the like. Migration 0011 flags stored
// entries with the same prefixes.
var sponsoredTitle = regexp.MustCompile(`(?i)^\s*(?:\[(?:sponsor|sponsored)\]|\((?:sponsor|sponsored)\)|(?:sponsor|sponsored|sponsored post|paid post|partner content|advertisement)\s*:|sponsored by\s)`)

// Sponsored reports whether an item is a paid post, by its title or by a
// feed category such as "Sponsor" or "Sponsored".
func Sponsored(title string, categories []string) bool {
	if sponsoredTitle.MatchString(title) {
		return true
	}
	for _, c := range categories {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "sponsor", "sponsors", "sponsored", "sponsorship", "sponsored post", "sponsored content",
			"paid post", "partner content", "advertisement", "advertorial":
			return true
		}
	}
	return false
}

// Excerpt returns up to max runes of text, cut at a word boundary.
func Excerpt(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	r := []rune(text)[:max]
	cut := string(r)
	if i := strings.LastIndexByte(cut, ' '); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-") + "…"
}

var spaces = regexp.MustCompile(`\s+`)

func collapse(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

func isTrackingPixel(n *html.Node) bool {
	if n.DataAtom != atom.Img {
		return false
	}
	w, h := attr(n, "width"), attr(n, "height")
	return (w == "1" || w == "0") && (h == "1" || h == "0")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *html.Node, key, val string) {
	for i, a := range n.Attr {
		if a.Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

func resolve(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if base == nil || ref == "" {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return base.ResolveReference(u).String()
}

func isHTTP(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

// Summaries are recognised as in the reader (ui/src/lib/partial.ts): short
// bodies, or bodies that end by sending the reader onward.
const (
	partialMinFull   = 600
	partialMaxTeaser = 2500
	partialMaxOnward = 8000
	partialTail      = 300
)

var (
	ellipsisEnd = regexp.MustCompile(`(…|\.\.\.|\[…\]|\[\.\.\.\])[\s.»→›)\]]*$`)
	onward      = regexp.MustCompile(`(?i)\b(read more|continue reading|read the (full|rest)|keep reading|the post .{1,200} appeared first on)\b`)
)

// LooksPartial reports whether article HTML looks like a summary of a longer
// article on the web.
func LooksPartial(html string) bool {
	text := []rune(PlainText(html))
	if len(text) < partialMinFull {
		return true
	}
	if len(text) < partialMaxOnward && onward.MatchString(string(text[max(0, len(text)-partialTail):])) {
		return true
	}
	return len(text) < partialMaxTeaser && ellipsisEnd.MatchString(string(text))
}

// Covers reports how much of text a appears in text b: the share of a's
// four-word runs found in b, near 1 when b contains a and near 0 when b is
// a different text. A text too short to judge counts as covered.
func Covers(a, b string) float64 {
	as := shingles(a)
	if len(as) < 8 {
		return 1
	}
	bs := shingles(b)
	n := 0
	for s := range as {
		if bs[s] {
			n++
		}
	}
	return float64(n) / float64(len(as))
}

func shingles(text string) map[string]bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) // "don't" and "don’t" alike
	})
	out := map[string]bool{}
	for i := 0; i+4 <= len(words); i++ {
		out[strings.Join(words[i:i+4], " ")] = true
	}
	return out
}

// SearchText is an article's words as plain text, for search and for
// summaries: block boundaries become spaces (so "end.</p><p>Next" stays
// two words), and scripts and styles are dropped. It is cut at max bytes.
func SearchText(htmlText string, max int) string {
	z := html.NewTokenizer(strings.NewReader(htmlText))
	var b strings.Builder
	skip := 0
	for b.Len() < max+64 {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken, html.EndTagToken:
			name, _ := z.TagName()
			if a := atom.Lookup(name); a == atom.Script || a == atom.Style {
				if tt == html.StartTagToken {
					skip++
				} else if skip > 0 {
					skip--
				}
			}
			b.WriteByte(' ')
		case html.SelfClosingTagToken:
			b.WriteByte(' ')
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
	}
	s := collapse(b.String())
	if len(s) > max {
		s = s[:max]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return strings.TrimSpace(s)
}

package content

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Tidy gives sanitized article HTML one shape, whether it came from a feed
// or was extracted from a page: a flat run of paragraphs, headings, lists,
// figures and the like, without the wrappers and empty blocks sites leave
// behind, with headings starting at h2 below the title, and without a
// heading that only repeats the title. It removes and renames elements but
// never adds any, so its input's sanitizing holds; the result is sanitized
// again all the same.
func Tidy(htmlText, title string) string {
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(htmlText), root)
	if err != nil {
		return htmlText
	}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	flatten(root)
	wrapInline(root)
	dropEmpty(root)
	dropMaterial(root)
	dropRepeats(root)
	levelHeadings(root, title)
	var buf bytes.Buffer
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode && strings.TrimSpace(c.Data) == "" {
			continue
		}
		html.Render(&buf, c)
		buf.WriteByte('\n')
	}
	return strings.TrimSpace(policy.Sanitize(buf.String()))
}

// Blocks returns tidied HTML's top-level blocks, rendered.
func Blocks(tidied string) []string {
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(tidied), root)
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range nodes {
		if n.Type == html.TextNode && strings.TrimSpace(n.Data) == "" {
			continue
		}
		var buf bytes.Buffer
		html.Render(&buf, n)
		out = append(out, buf.String())
	}
	return out
}

// Describe gives one block as a model reads it: its element, how many
// images it has, and its first 400 characters of text.
func Describe(block string) string {
	tag := "p"
	if strings.HasPrefix(block, "<") {
		if end := strings.IndexAny(block[1:], " >/"); end > 0 {
			tag = block[1 : end+1]
		}
	}
	d := "<" + tag + ">"
	if n := strings.Count(block, "<img"); n == 1 {
		d += " (1 image)"
	} else if n > 1 {
		d += fmt.Sprintf(" (%d images)", n)
	}
	text := strings.Join(strings.Fields(PlainText(block)), " ")
	if r := []rune(text); len(r) > 400 {
		text = string(r[:400]) + "…"
	}
	return d + " " + text
}

// wrappers carry no meaning of their own in an article: their children
// take their place.
var wrappers = map[atom.Atom]bool{
	atom.Div: true, atom.Section: true, atom.Article: true, atom.Main: true,
	atom.Header: true, atom.Footer: true, atom.Hgroup: true, atom.Span: true,
}

func flatten(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		flatten(c)
		if c.Type == html.ElementNode && wrappers[c.DataAtom] && !(c.DataAtom == atom.Span && len(c.Attr) > 0) {
			for gc := c.FirstChild; gc != nil; {
				gnext := gc.NextSibling
				c.RemoveChild(gc)
				n.InsertBefore(gc, c)
				gc = gnext
			}
			n.RemoveChild(c)
		}
		c = next
	}
}

// blockLevel elements stand on their own at the top level; anything else
// (text, links, emphasis, images) is gathered into paragraphs.
var blockLevel = map[atom.Atom]bool{
	atom.P: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Ul: true, atom.Ol: true, atom.Dl: true, atom.Pre: true, atom.Blockquote: true, atom.Figure: true,
	atom.Table: true, atom.Hr: true, atom.Aside: true, atom.Details: true, atom.Video: true, atom.Audio: true,
	// These close a paragraph they open in, so can't be gathered into one.
	atom.Li: true, atom.Dd: true, atom.Dt: true, atom.Figcaption: true, atom.Summary: true,
	atom.Caption: true, atom.Thead: true, atom.Tbody: true, atom.Tfoot: true, atom.Tr: true, atom.Td: true, atom.Th: true,
}

func wrapInline(root *html.Node) {
	var run *html.Node
	for c := root.FirstChild; c != nil; {
		next := c.NextSibling
		switch {
		case c.Type == html.ElementNode && blockLevel[c.DataAtom]:
			run = nil
		case c.Type == html.ElementNode && c.DataAtom == atom.Br && run == nil:
			root.RemoveChild(c) // a break between blocks
		case c.Type == html.TextNode && run == nil && strings.TrimSpace(c.Data) == "":
			// whitespace between blocks
		default:
			if run == nil {
				run = &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P}
				root.InsertBefore(run, c)
			}
			root.RemoveChild(c)
			run.AppendChild(c)
		}
		c = next
	}
}

// media keep an element that has no text.
var media = map[atom.Atom]bool{
	atom.Img: true, atom.Video: true, atom.Audio: true, atom.Hr: true, atom.Br: true,
	atom.Td: true, atom.Th: true, atom.Tr: true,
}

func dropEmpty(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode {
			dropEmpty(c)
			if !media[c.DataAtom] && empty(c) {
				n.RemoveChild(c)
			}
		}
		c = next
	}
	trimBreaks(n)
}

func empty(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.TextNode && strings.TrimSpace(c.Data) != "":
			return false
		case c.Type == html.ElementNode && (c.DataAtom == atom.Img || c.DataAtom == atom.Video || c.DataAtom == atom.Audio || c.DataAtom == atom.Hr):
			return false
		case c.Type == html.ElementNode && !empty(c):
			return false
		}
	}
	return true
}

// trimBreaks drops line breaks that open or close a block, which only add
// space.
func trimBreaks(n *html.Node) {
	if n.Type != html.ElementNode || n.DataAtom == atom.Pre {
		return
	}
	for _, edge := range []func() *html.Node{func() *html.Node { return n.FirstChild }, func() *html.Node { return n.LastChild }} {
		for c := edge(); c != nil; c = edge() {
			if c.Type == html.TextNode && strings.TrimSpace(c.Data) == "" || c.Type == html.ElementNode && c.DataAtom == atom.Br {
				if n.FirstChild == n.LastChild && c.Type == html.TextNode {
					break
				}
				n.RemoveChild(c)
				continue
			}
			break
		}
	}
}

func dropMaterial(root *html.Node) {
	var blocks []*html.Node
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			blocks = append(blocks, c)
		}
	}
	for i, d := range websiteMaterial(blocks) {
		if d {
			root.RemoveChild(blocks[i])
		}
	}
}

// dropRepeats drops a block that repeats the one before it, as a page's
// standfirst shown twice. Short lines (a scene break, a one-word reply)
// may repeat on purpose.
func dropRepeats(root *html.Node) {
	var prev string
	for c := root.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode {
			text := strings.Join(strings.Fields(textOf(c)), " ")
			if text == prev && len(strings.Fields(text)) >= 8 {
				root.RemoveChild(c)
			}
			prev = text
		}
		c = next
	}
}

var headingLevel = map[atom.Atom]int{atom.H1: 1, atom.H2: 2, atom.H3: 3, atom.H4: 4, atom.H5: 5, atom.H6: 6}
var headingAtom = []atom.Atom{0, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6}

// levelHeadings drops a first heading that repeats the title, then
// renumbers the levels in use from h2 down, keeping their order: an
// article's h1/h3 becomes h2/h3, a feed's lone h4 sections become h2.
func levelHeadings(root *html.Node, title string) {
	used := map[int]bool{}
	first := true
	for c := root.FirstChild; c != nil; {
		next := c.NextSibling
		if l := headingLevel[c.DataAtom]; c.Type == html.ElementNode && l > 0 {
			if first && sameText(textOf(c), title) {
				root.RemoveChild(c)
				c = next
				continue
			}
			used[l] = true
		}
		if c.Type == html.ElementNode {
			first = false
		}
		c = next
	}
	to, next := map[int]int{}, 2
	for l := 1; l <= 6; l++ {
		if used[l] {
			to[l] = min(next, 6)
			next++
		}
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if l := headingLevel[c.DataAtom]; c.Type == html.ElementNode && l > 0 && to[l] != l {
			c.DataAtom = headingAtom[to[l]]
			c.Data = c.DataAtom.String()
		}
	}
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// sameText compares two texts by their words, ignoring case and
// punctuation.
func sameText(a, b string) bool {
	norm := func(s string) string {
		return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}), " ")
	}
	na := norm(a)
	return na != "" && na == norm(b)
}

// Website material: what sites put around their articles, in the article's
// first or last blocks, found by what it says. The patterns are generic
// (seen across many sites, never one site's markup) and each block they
// match is short, so a paragraph that merely mentions subscribing stays.
var (
	// A block that is one of these, whole.
	materialWhole = regexp.MustCompile(`(?i)^(` +
		`skip to (main )?content|posted:?|in brief|share( this( (post|article|story))?)?|comments?|\d+ comments?|` +
		`subscribe( now)?|sign in( subscribe)?|sign up|log in to listen|listen to this (post|article|story):?|` +
		`report a typo|email \(required\)|advertisement|presented by|` +
		`by|by\s*\p{Lu}[\p{L}.'’ -]{1,60}|(the )?[\p{L} ]{0,30}newsletter|` +
		`(updated )?\d+ (minutes?|hours?|days?) ago|` +
		`(posted |updated |published )?((mon|tue|wed|thu|fri|sat|sun)\w*,? )?` +
		`((\d{1,2}(st|nd|rd|th)? )?(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\w* (\d{1,2}(st|nd|rd|th)?,? )?\d{4}|\d{4}-\d\d-\d\d)` +
		`(,? (at )?\d{1,2}[:.]\d\d( ?[ap]\.?m\.?)?( [a-z]{2,4})?)?|` +
		`\d{1,2}[:.]\d\d ?([ap]\.?m\.?)?( [a-z]{2,4})? ?[·|,-]? ?((jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\w* \d{1,2},? \d{4})` +
		`)\W*$`)
	// A block that starts like this.
	materialStart = regexp.MustCompile(`(?i)^(` +
		`tags|topics|more about|filed under|follow topics and authors|` +
		`subscribe (to|for|and) |sign up (for|to) |get (our|the) .{0,40}newsletter|` +
		`if you (appreciate|enjoy|like|liked|enjoyed) (articles|posts|stories|this)|` +
		`(support|become a member|join) .{0,40}(for \$|\$\d)|` +
		`(next|previous) (article|post|story): |\[\s*read: |.{0,30}unlimited access to |` +
		`get .{0,60}delivered to your (email )?inbox` +
		`)`)
	// Affiliate disclosures, wherever they are.
	affiliate = regexp.MustCompile(`(?i)(we (may|might) (earn|receive|get) (a )?(small )?(commission|share of)|` +
		`income earning .{0,20}affiliate links|we use affiliate links|contains affiliate links)`)
	// A heading that opens the website's section after an article: it
	// goes, with everything after it, when everything after it is short.
	materialHeading = regexp.MustCompile(`(?i)^(` +
		`(recent|related|latest|popular|trending|more|top) (articles|posts|stories|reads|news|from|in|on)\b.*|` +
		`more from .*|read more|you (might|may) also like|also in .*|next article|previous article|` +
		`most (read|popular|viewed)|about the author|comment on this article|comments|get in touch|` +
		`support this show|support (us|our work)|follow us|share this( (post|article|story))?|newsletter|sign up.*|subscribe.*|` +
		`become a (member|subscriber|patron).*|access .{0,30}(content|perks)` +
		`)\W*$`)
)

const (
	edgeBlocks    = 6   // blocks at each end where website material may be
	materialChars = 280 // the longest block taken for website material
)

// isMaterial reports whether one block, by what it says, is the website's.
func isMaterial(n *html.Node) bool {
	text := strings.Join(strings.Fields(textOf(n)), " ")
	if text == "" || len([]rune(text)) > materialChars || Onward(text) {
		return false
	}
	return materialWhole.MatchString(text) || materialStart.MatchString(text) || affiliate.MatchString(text)
}

// websiteMaterial finds the website's material among an article's blocks:
// matching blocks at either end, affiliate disclosures anywhere, and a
// trailing section (a "Related articles" heading and its list) whose
// heading is the website's and whose blocks are all short.
func websiteMaterial(blocks []*html.Node) []bool {
	drop := make([]bool, len(blocks))
	n := len(blocks)
	for i, b := range blocks {
		text := strings.Join(strings.Fields(textOf(b)), " ")
		edge := i < edgeBlocks || i >= n-edgeBlocks
		if edge && isMaterial(b) || len([]rune(text)) <= materialChars && affiliate.MatchString(text) {
			drop[i] = true
		}
	}
	for i := max(1, n-2*edgeBlocks, n/2); i < n; i++ {
		if headingLevel[blocks[i].DataAtom] == 0 || !materialHeading.MatchString(strings.TrimSpace(textOf(blocks[i]))) {
			continue
		}
		// What follows is the section's: short blocks and lists.
		rest := true
		for _, b := range blocks[i+1:] {
			if len([]rune(strings.TrimSpace(textOf(b)))) > 2*materialChars && b.DataAtom != atom.Ul && b.DataAtom != atom.Ol {
				rest = false
				break
			}
		}
		if rest {
			for j := i; j < n; j++ {
				drop[j] = true
			}
			break
		}
	}
	// A site's navigation opening the page: a list of a few one- or
	// two-word links.
	for i := 0; i < min(n, 3); i++ {
		if navigation(blocks[i]) {
			drop[i] = true
		}
	}
	// A rule left alone between dropped blocks, or at an end, goes too.
	for i, b := range blocks {
		if b.DataAtom == atom.Hr && (i == 0 || drop[i-1]) && (i == n-1 || drop[i+1] || allDropped(drop[i+1:])) {
			drop[i] = true
		}
	}
	return drop
}

func allDropped(d []bool) bool {
	for _, x := range d {
		if !x {
			return false
		}
	}
	return true
}

// navigation reports whether a block is a list of three or more links of
// a word or two each.
func navigation(n *html.Node) bool {
	if n.DataAtom != atom.Ul && n.DataAtom != atom.Ol || !linkList(n) {
		return false
	}
	items := 0
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode {
			continue
		}
		if len(strings.Fields(textOf(li))) > 2 {
			return false
		}
		items++
	}
	return items >= 3
}

// linkList reports whether a block is a list made of links: the shape of
// "related articles".
func linkList(n *html.Node) bool {
	if n.DataAtom != atom.Ul && n.DataAtom != atom.Ol {
		return false
	}
	all, linked := len(strings.TrimSpace(textOf(n))), 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.A {
			linked += len(strings.TrimSpace(textOf(n)))
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return all > 0 && linked*10 >= all*8
}

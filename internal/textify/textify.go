// Package textify turns sanitized article HTML into Markdown, for reading
// in a terminal and for AI agents: headings, paragraphs, links, lists,
// quotations, code and images keep their meaning; everything else is text.
package textify

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Markdown converts HTML to Markdown. base resolves relative links and
// images (proxied images are /img/… on the server).
func Markdown(s, base string) string { out, _ := convert(s, base, false, false); return out }

// Numbered converts HTML to Markdown for reading in a terminal: link text
// is followed by a number, and the addresses are listed at the end, so
// paragraphs aren't interrupted by long URLs.
func Numbered(s, base string) string { out, _ := convert(s, base, true, false); return out }

// Picture is an image that stands on its own in the article. Pictured
// writes it as Placeholder(i), a paragraph of its own, for the terminal to
// draw in its place; Label is what to show if it can't.
type Picture struct {
	URL, Label string
}

// Placeholder is the word standing for picture i in Pictured's Markdown.
func Placeholder(i int) string { return "CORRPICTURE" + strconv.Itoa(i) }

// PlaceholderAt is the picture a line of rendered text stands for, if any.
func PlaceholderAt(line string) (int, bool) {
	m := placeholder.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	i, err := strconv.Atoi(m[1])
	return i, err == nil
}

// Pictured is Numbered, with each image that stands on its own (not one in
// the middle of a sentence) left as a placeholder to draw.
func Pictured(s, base string) (string, []Picture) { return convert(s, base, true, true) }

func convert(s, base string, numbered, pictures bool) (string, []Picture) {
	nodes, err := html.ParseFragment(strings.NewReader(s), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return s, nil
	}
	b, _ := url.Parse(base)
	w := &writer{base: b, numbered: numbered, pictures: pictures}
	for _, n := range nodes {
		w.block(n)
	}
	out := strings.TrimSpace(blankLines.ReplaceAllString(w.out.String(), "\n\n")) + "\n"
	if pictures {
		// A picture in a sentence, a list or a table is shown as its label.
		lines := strings.Split(out, "\n")
		for i, l := range lines {
			if !placeholder.MatchString(l) {
				lines[i] = inText.ReplaceAllStringFunc(l, func(m string) string {
					n, _ := strconv.Atoi(inText.FindStringSubmatch(m)[1])
					return w.pics[n].Label
				})
			}
		}
		out = strings.Join(lines, "\n")
	}
	if len(w.refs) > 0 {
		out += "\n---\n\n**Links**\n\n"
		for i, r := range w.refs {
			out += fmt.Sprintf("%d. <%s>\n", i+1, r)
		}
	}
	return out, w.pics
}

// ref numbers an address, reusing the number of one seen before.
func (w *writer) ref(u string) int {
	for i, r := range w.refs {
		if r == u {
			return i + 1
		}
	}
	w.refs = append(w.refs, u)
	return len(w.refs)
}

var (
	blankLines  = regexp.MustCompile(`\n{3,}`)
	spaces      = regexp.MustCompile(`[ \t\r\n]+`)
	placeholder = regexp.MustCompile(`^\s*CORRPICTURE(\d+)\s*$`)
	// A placeholder, and the link number after it when the image is a link.
	inText = regexp.MustCompile(`CORRPICTURE(\d+)(?: \\\[\d+\\\])?`)
)

type writer struct {
	out      strings.Builder
	base     *url.URL
	quote    int // blockquote depth
	numbered bool
	refs     []string
	pictures bool
	pics     []Picture
}

func (w *writer) para(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	// Pictures with nothing else in the paragraph (a link's number aside)
	// get a line each, outside any quotation.
	if w.pictures && strings.TrimSpace(inText.ReplaceAllString(text, "")) == "" {
		for _, m := range inText.FindAllStringSubmatch(text, -1) {
			w.out.WriteString("\nCORRPICTURE" + m[1] + "\n\n")
		}
		return
	}
	prefix := strings.Repeat("> ", w.quote)
	for _, line := range strings.Split(text, "\n") {
		w.out.WriteString(strings.TrimRight(prefix+line, " ") + "\n")
	}
	w.out.WriteString(strings.TrimRight(prefix, " ") + "\n")
}

func (w *writer) block(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		w.para(w.inline(n))
		return
	case html.ElementNode:
	default:
		return
	}
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Noscript, atom.Template:
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		w.para(strings.Repeat("#", max(level, 2)) + " " + w.inline(n))
	case atom.P, atom.Figcaption, atom.Summary, atom.Dt, atom.Dd:
		text := w.inline(n)
		if n.DataAtom == atom.Figcaption {
			text = "_" + strings.TrimSpace(text) + "_"
		}
		w.para(text)
	case atom.Ul, atom.Ol:
		w.list(n, 0)
		w.out.WriteString("\n")
	case atom.Blockquote:
		w.quote++
		w.children(n)
		w.quote--
	case atom.Pre:
		w.para("```\n" + strings.TrimRight(textOf(n), "\n") + "\n```")
	case atom.Hr:
		w.para("---")
	case atom.Table:
		w.table(n)
	case atom.Img:
		w.para(w.inline(n))
	case atom.Div, atom.Section, atom.Article, atom.Figure, atom.Header, atom.Footer, atom.Main, atom.Aside, atom.Details, atom.Dl, atom.Nav:
		w.children(n)
	default:
		// Inline content at block level (a bare <a>, <span>, <em>…).
		if hasBlockChild(n) {
			w.children(n)
		} else {
			w.para(w.inline(n))
		}
	}
}

func (w *writer) children(n *html.Node) {
	// Runs of inline nodes form one paragraph.
	var run []*html.Node
	flush := func() {
		if len(run) == 0 {
			return
		}
		var b strings.Builder
		for _, c := range run {
			b.WriteString(w.inline(c))
		}
		w.para(b.String())
		run = nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if isBlock(c) {
			flush()
			w.block(c)
		} else {
			run = append(run, c)
		}
	}
	flush()
}

func (w *writer) list(n *html.Node, depth int) {
	i := 0
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.DataAtom != atom.Li {
			continue
		}
		i++
		marker := "- "
		if n.DataAtom == atom.Ol {
			marker = strconv.Itoa(i) + ". "
		}
		var text strings.Builder
		var nested []*html.Node
		for c := li.FirstChild; c != nil; c = c.NextSibling {
			if c.DataAtom == atom.Ul || c.DataAtom == atom.Ol {
				nested = append(nested, c)
			} else if isBlock(c) {
				text.WriteString(" " + w.inline(c))
			} else {
				text.WriteString(w.inline(c))
			}
		}
		prefix := strings.Repeat("> ", w.quote) + strings.Repeat("  ", depth)
		w.out.WriteString(prefix + marker + strings.TrimSpace(spaces.ReplaceAllString(text.String(), " ")) + "\n")
		for _, nl := range nested {
			w.list(nl, depth+1)
		}
	}
}

func (w *writer) table(n *html.Node) {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.DataAtom == atom.Tr {
			var cells []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.DataAtom == atom.Td || c.DataAtom == atom.Th {
					cells = append(cells, strings.ReplaceAll(strings.TrimSpace(w.inline(c)), "|", "\\|"))
				}
			}
			rows = append(rows, cells)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	if len(rows) == 0 {
		return
	}
	var b strings.Builder
	for i, r := range rows {
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
		if i == 0 {
			b.WriteString("|" + strings.Repeat(" --- |", len(r)) + "\n")
		}
	}
	w.para(b.String())
}

// inline renders a node's inline content on one logical line.
func (w *writer) inline(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(spaces.ReplaceAllString(n.Data, " "))
			return
		case html.ElementNode:
		default:
			return
		}
		inner := func() string {
			var sb strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				sb.WriteString(w.inline(c))
			}
			return sb.String()
		}
		switch n.DataAtom {
		case atom.Script, atom.Style:
		case atom.Br:
			b.WriteString("\n")
		case atom.A:
			text := strings.TrimSpace(inner())
			href := w.resolve(attr(n, "href"))
			switch {
			case href == "" || strings.HasPrefix(href, "javascript:"):
				b.WriteString(text)
			case w.numbered && text != "":
				b.WriteString(fmt.Sprintf("%s \\[%d\\]", text, w.ref(href)))
			case text == "" || text == href:
				b.WriteString("<" + href + ">")
			default:
				b.WriteString("[" + text + "](" + href + ")")
			}
		case atom.Img:
			if src := w.resolve(attr(n, "src")); src != "" {
				alt := strings.TrimSpace(attr(n, "alt"))
				if w.numbered {
					if alt == "" {
						alt = "picture"
					}
					label := fmt.Sprintf("_\\[%s\\]_ \\[%d\\]", alt, w.ref(src))
					if w.pictures {
						w.pics = append(w.pics, Picture{URL: src, Label: label})
						label = Placeholder(len(w.pics) - 1)
					}
					b.WriteString(label)
				} else {
					b.WriteString("![" + alt + "](" + src + ")")
				}
			}
		case atom.Strong, atom.B:
			if t := strings.TrimSpace(inner()); t != "" {
				b.WriteString("**" + t + "**")
			}
		case atom.Em, atom.I, atom.Cite:
			if t := strings.TrimSpace(inner()); t != "" {
				b.WriteString("_" + t + "_")
			}
		case atom.Code, atom.Kbd, atom.Samp:
			if t := textOf(n); t != "" {
				b.WriteString("`" + t + "`")
			}
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			if isBlock(n) {
				b.WriteString(" ")
			}
		}
	}
	walk(n)
	return b.String()
}

func (w *writer) resolve(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || w.base == nil {
		return ref
	}
	u, err := w.base.Parse(ref)
	if err != nil {
		return ref
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto" {
		return ""
	}
	return u.String()
}

func isBlock(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.DataAtom {
	case atom.P, atom.Div, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol, atom.Li,
		atom.Blockquote, atom.Pre, atom.Hr, atom.Table, atom.Figure, atom.Figcaption, atom.Section, atom.Article,
		atom.Header, atom.Footer, atom.Main, atom.Aside, atom.Details, atom.Summary, atom.Dl, atom.Dt, atom.Dd, atom.Nav:
		return true
	}
	return false
}

func hasBlockChild(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if isBlock(c) || hasBlockChild(c) {
			return true
		}
	}
	return false
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

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

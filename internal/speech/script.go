package speech

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Script is what's read aloud: the title, the author, and the article's
// prose. Code, tables, captions, images and embeds are left out; they don't
// survive being spoken.
func Script(title, author, content string) string {
	var paras []string
	if t := clean(title); t != "" {
		paras = append(paras, ensureStop(t))
	}
	if a := clean(author); a != "" {
		paras = append(paras, "By "+ensureStop(a))
	}
	doc, err := html.Parse(strings.NewReader(content))
	if err == nil {
		var cur strings.Builder
		flush := func() {
			if p := clean(cur.String()); p != "" {
				paras = append(paras, p)
			}
			cur.Reset()
		}
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			switch n.Type {
			case html.TextNode:
				cur.WriteString(n.Data)
				return
			case html.ElementNode:
				switch n.DataAtom {
				case atom.Pre, atom.Code, atom.Table, atom.Figure, atom.Figcaption, atom.Img, atom.Picture,
					atom.Svg, atom.Math, atom.Iframe, atom.Video, atom.Audio, atom.Script, atom.Style,
					atom.Noscript, atom.Button, atom.Nav, atom.Sup, atom.Form, atom.Object, atom.Embed:
					if n.DataAtom == atom.Code && !isBlockCode(n) {
						break // inline code reads fine
					}
					return
				case atom.Br:
					cur.WriteString(" ")
					return
				}
				if block[n.DataAtom] {
					flush()
					defer flush()
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(doc)
		flush()
	}
	return strings.Join(paras, "\n\n")
}

var block = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Li: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.H5: true, atom.H6: true, atom.Blockquote: true, atom.Section: true, atom.Article: true,
	atom.Header: true, atom.Footer: true, atom.Aside: true, atom.Dd: true, atom.Dt: true, atom.Hr: true,
	atom.Ul: true, atom.Ol: true, atom.Dl: true, atom.Main: true,
}

// isBlockCode tells a code block (inside <pre>, or alone in its paragraph)
// from code inline in a sentence.
func isBlockCode(n *html.Node) bool {
	p := n.Parent
	if p == nil {
		return true
	}
	if p.DataAtom == atom.Pre {
		return true
	}
	return n.PrevSibling == nil && n.NextSibling == nil && p.DataAtom != atom.P && p.DataAtom != atom.Li
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

// ensureStop ends a heading-like line with a full stop, so it's read as its
// own sentence.
func ensureStop(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if unicode.IsPunct(r[len(r)-1]) {
		return s
	}
	return s + "."
}

// Chunks splits a script into pieces of at most max bytes, at paragraph
// breaks, else at sentence ends, else between words.
func Chunks(script string, max int) []string {
	var out []string
	var cur strings.Builder
	add := func(piece, sep string) {
		if cur.Len() > 0 && cur.Len()+len(sep)+len(piece) > max {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString(sep)
		}
		cur.WriteString(piece)
	}
	for _, para := range strings.Split(script, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if len(para) <= max {
			add(para, "\n\n")
			continue
		}
		for _, sentence := range sentences(para) {
			if len(sentence) <= max {
				add(sentence, " ")
				continue
			}
			for _, w := range strings.Fields(sentence) {
				if len(w) > max {
					w = w[:max]
				}
				add(w, " ")
			}
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// sentences splits after ". ", "! " and "? ".
func sentences(p string) []string {
	var out []string
	start := 0
	for i := 0; i < len(p)-1; i++ {
		if (p[i] == '.' || p[i] == '!' || p[i] == '?') && p[i+1] == ' ' {
			out = append(out, p[start:i+1])
			start = i + 2
		}
	}
	if start < len(p) {
		out = append(out, p[start:])
	}
	return out
}

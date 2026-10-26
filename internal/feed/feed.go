// Package feed normalizes RSS, Atom and JSON Feed documents into entries
// ready for storage, and discovers feed links in HTML pages.
package feed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/store"
)

const maxItems = 500

var ErrNotFeed = errors.New("document is not a feed")

type Parsed struct {
	Title       string
	SiteURL     string
	Description string
	IconURL     string
	Items       []store.NewEntry
}

// Parse normalizes a fetched feed body. feedURL resolves relative links.
func Parse(body []byte, feedURL string) (*Parsed, error) {
	if bytes.Contains(bytes.ToUpper(firstBytes(body, 2048)), []byte("<!DOCTYPE HTML")) {
		return nil, ErrNotFeed
	}
	f, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		if errors.Is(err, gofeed.ErrFeedTypeNotDetected) {
			return nil, ErrNotFeed
		}
		return nil, err
	}
	p := &Parsed{
		Title:       content.PlainText(f.Title),
		SiteURL:     absolute(feedURL, f.Link),
		Description: content.Excerpt(content.PlainText(f.Description), 280),
	}
	if f.Image != nil {
		p.IconURL = absolute(feedURL, f.Image.URL)
	}
	if p.IconURL == "" && p.SiteURL != "" {
		if u, err := url.Parse(p.SiteURL); err == nil {
			p.IconURL = u.Scheme + "://" + u.Host + "/favicon.ico"
		}
	}
	base := p.SiteURL
	if base == "" {
		base = feedURL
	}
	items := f.Items
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	// Some feeds list the same GUID more than once (sites that reuse one URL
	// for an evolving page). Keep the first occurrence, as FreshRSS does, so
	// one item doesn't overwrite another within a single poll.
	seen := map[string]bool{}
	for _, it := range items {
		e := normalize(it, base)
		if seen[e.GUID] {
			continue
		}
		seen[e.GUID] = true
		p.Items = append(p.Items, e)
	}
	return p, nil
}

func normalize(it *gofeed.Item, base string) store.NewEntry {
	link := absolute(base, it.Link)
	raw := it.Content
	if strings.TrimSpace(raw) == "" {
		raw = it.Description
	}
	body := content.Process(raw, firstNonEmpty(link, base))
	excerpt := body.Text
	if raw == it.Content && it.Description != "" {
		if d := content.PlainText(it.Description); d != "" {
			excerpt = d
		}
	}
	e := store.NewEntry{
		URL:     link,
		Title:   content.PlainText(it.Title),
		Content: body.HTML,
		Excerpt: content.Excerpt(excerpt, 320),
	}
	if e.Title == "" {
		e.Title = content.Excerpt(body.Text, 90)
	}
	if it.Author != nil {
		e.Author = content.PlainText(it.Author.Name)
	} else if len(it.Authors) > 0 && it.Authors[0] != nil {
		e.Author = content.PlainText(it.Authors[0].Name)
	}
	switch {
	case it.PublishedParsed != nil:
		e.PublishedAt = *it.PublishedParsed
	case it.UpdatedParsed != nil:
		e.PublishedAt = *it.UpdatedParsed
	}

	var encl []store.Enclosure
	for _, en := range it.Enclosures {
		if en == nil || en.URL == "" {
			continue
		}
		length, _ := strconv.ParseInt(en.Length, 10, 64)
		u := absolute(base, en.URL)
		encl = append(encl, store.Enclosure{URL: u, Type: en.Type, Length: length})
		if e.ImageURL == "" && strings.HasPrefix(en.Type, "image/") {
			e.ImageURL = u
		}
	}
	if b, err := json.Marshal(encl); err == nil && encl != nil {
		e.Enclosures = string(b)
	} else {
		e.Enclosures = "[]"
	}
	if it.Image != nil && it.Image.URL != "" {
		e.ImageURL = absolute(base, it.Image.URL)
	}
	if e.ImageURL == "" {
		e.ImageURL = body.ImageURL
	}

	// Identity: GUID, else link, else a hash of what we have. The fallback
	// must be stable across polls, so it never includes the crawl time.
	e.GUID = strings.TrimSpace(it.GUID)
	if e.GUID == "" {
		e.GUID = link
	}
	if e.GUID == "" {
		e.GUID = "sha256:" + hash(e.Title, raw, e.PublishedAt.UTC().Format(time.RFC3339))
	}
	e.ContentHash = hash(e.Title, e.URL, e.Content, e.Author, e.Enclosures)
	return e
}

// Discover returns feed URLs advertised by an HTML page via <link rel=alternate>.
func Discover(body []byte, pageURL string) []string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Link {
			var rel, typ, href string
			for _, a := range n.Attr {
				switch a.Key {
				case "rel":
					rel = strings.ToLower(a.Val)
				case "type":
					typ = strings.ToLower(a.Val)
				case "href":
					href = a.Val
				}
			}
			if strings.Contains(rel, "alternate") && href != "" &&
				(strings.Contains(typ, "rss") || strings.Contains(typ, "atom") || strings.Contains(typ, "feed+json")) {
				out = append(out, absolute(pageURL, href))
			}
		}
		if n.DataAtom == atom.Body {
			return // feed links live in <head>
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func absolute(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	u := b.ResolveReference(r)
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstBytes(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

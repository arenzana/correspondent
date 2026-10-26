// Package pagefeed turns a web page that lists articles (a blog index, a
// research page) into feed items, for sites that publish no feed.
//
// A page feed is described by Rules, worked out once per site: by exact
// methods (h-feed microformats, JSON-LD article lists, or a repeated link
// pattern such as /research/<slug>), or by an AI model that proposes CSS
// selectors. Rules are plain data and are applied the same way on every
// poll; nothing here runs code from a page or a model.
package pagefeed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/cascadia"

	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/feed"
	"github.com/arenzana/correspondent/internal/store"
)

// Methods.
const (
	MethodHFeed  = "hfeed"  // microformats2 h-entry
	MethodJSONLD = "jsonld" // schema.org Article/BlogPosting objects
	MethodLinks  = "links"  // links under one path prefix
	MethodCSS    = "css"    // selectors, usually proposed by the AI
)

const (
	maxItems     = 100
	minItems     = 3
	maxSelector  = 300
	minTitleText = 12
)

var ErrNoArticles = errors.New("no list of articles found on the page")

// Rules describe how to find articles on a page.
type Rules struct {
	Method string `json:"method"`
	// MethodLinks: the path prefix article links share, e.g. "/research/".
	Prefix string `json:"prefix,omitempty"`
	// MethodCSS: Item selects one element per article; the others are
	// relative to it. Link defaults to the first a[href] (or the item itself
	// when it is a link), Title to the link's text.
	Item    string `json:"item,omitempty"`
	Link    string `json:"link,omitempty"`
	Title   string `json:"title,omitempty"`
	Date    string `json:"date,omitempty"`
	Summary string `json:"summary,omitempty"`
	Image   string `json:"image,omitempty"`
}

// Validate checks rules from storage or a client: a known method and
// selectors that parse.
func (r Rules) Validate() error {
	switch r.Method {
	case MethodHFeed, MethodJSONLD:
	case MethodLinks:
		if !strings.HasPrefix(r.Prefix, "/") || len(r.Prefix) > 200 {
			return errors.New("invalid link prefix")
		}
	case MethodCSS:
		if r.Item == "" {
			return errors.New("an item selector is required")
		}
		for _, s := range []string{r.Item, r.Link, r.Title, r.Date, r.Summary, r.Image} {
			if len(s) > maxSelector {
				return errors.New("selector too long")
			}
			if s == "" {
				continue
			}
			if _, err := cascadia.ParseGroup(s); err != nil {
				return fmt.Errorf("invalid selector %q: %w", s, err)
			}
		}
	default:
		return fmt.Errorf("unknown method %q", r.Method)
	}
	return nil
}

// Encode returns the rules as stored.
func (r Rules) Encode() string {
	// Selectors stay readable in the database: no \u003e for ">".
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(r)
	return strings.TrimSpace(b.String())
}

// Decode parses and validates stored rules.
func Decode(s string) (Rules, error) {
	var r Rules
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return r, err
	}
	return r, r.Validate()
}

// Parse applies rules to a fetched page.
func Parse(body []byte, pageURL string, r Rules) (*feed.Parsed, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	p := pageInfo(doc, base)
	var items []item
	switch r.Method {
	case MethodHFeed:
		items = hfeed(doc, base)
	case MethodJSONLD:
		items = jsonld(doc, base)
	case MethodLinks:
		items = links(doc, base, r.Prefix)
	case MethodCSS:
		items = css(doc, base, r)
	}
	p.Items = entries(items, base)
	if len(p.Items) == 0 {
		return nil, ErrNoArticles
	}
	return p, nil
}

// Detect tries the exact methods in order of reliability and returns the
// first that finds enough articles.
func Detect(body []byte, pageURL string) (Rules, *feed.Parsed, error) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return Rules{}, nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return Rules{}, nil, err
	}
	try := func(r Rules) (*feed.Parsed, bool) {
		p, err := Parse(body, pageURL, r)
		return p, err == nil && len(p.Items) >= minItems
	}
	for _, r := range []Rules{{Method: MethodHFeed}, {Method: MethodJSONLD}} {
		if p, ok := try(r); ok {
			return r, p, nil
		}
	}
	if prefix := linkPrefix(doc, base); prefix != "" {
		r := Rules{Method: MethodLinks, Prefix: prefix}
		if p, ok := try(r); ok {
			return r, p, nil
		}
	}
	return Rules{}, nil, ErrNoArticles
}

// IsArticle reports whether a page describes itself as a single article
// (Open Graph type "article"), so discovery should look at its parent.
func IsArticle(body []byte) bool {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return false
	}
	t, _ := doc.Find(`meta[property="og:type"]`).Attr("content")
	return strings.EqualFold(strings.TrimSpace(t), "article")
}

// Parents returns the listing pages above an article address, nearest first:
// /research/some-post → /research/, then /.
func Parents(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	var out []string
	p := strings.TrimSuffix(u.Path, "/")
	for p != "" && p != "/" && len(out) < 2 {
		p = path.Dir(p)
		v := *u
		v.Path, v.RawQuery, v.Fragment = strings.TrimSuffix(p, "/")+"/", "", ""
		out = append(out, v.String())
	}
	return out
}

type item struct {
	url, title, summary, image string
	published                  time.Time
}

func pageInfo(doc *goquery.Document, base *url.URL) *feed.Parsed {
	// The page title names the section ("Research \ Anthropic"); the site
	// name is the fallback.
	title := content.PlainText(doc.Find("title").First().Text())
	if og, ok := doc.Find(`meta[property="og:site_name"]`).Attr("content"); ok && title == "" {
		title = content.PlainText(og)
	}
	desc, _ := doc.Find(`meta[name="description"]`).Attr("content")
	icon := ""
	doc.Find(`link[rel~="icon"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if h, ok := s.Attr("href"); ok {
			icon = resolve(base, h)
		}
		return icon == ""
	})
	if icon == "" {
		icon = base.Scheme + "://" + base.Host + "/favicon.ico"
	}
	return &feed.Parsed{
		Title:       title,
		SiteURL:     base.Scheme + "://" + base.Host + "/",
		Description: content.Excerpt(content.PlainText(desc), 280),
		IconURL:     icon,
	}
}

// entries normalizes items: absolute same-site http(s) links only, one entry
// per address, page order kept, at most maxItems.
func entries(items []item, base *url.URL) []store.NewEntry {
	seen := map[string]bool{}
	var out []store.NewEntry
	for _, it := range items {
		u, err := url.Parse(it.url)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !sameSite(u.Host, base.Host) {
			continue
		}
		u.Fragment = ""
		link := u.String()
		if seen[link] || link == base.String() {
			continue
		}
		seen[link] = true
		title := content.PlainText(it.title)
		if title == "" {
			continue
		}
		e := store.NewEntry{
			GUID:        link,
			URL:         link,
			Title:       content.Excerpt(title, 200),
			Excerpt:     content.Excerpt(content.PlainText(it.summary), 320),
			ImageURL:    it.image,
			PublishedAt: it.published,
			Enclosures:  "[]",
		}
		if e.Excerpt != "" {
			e.Content = content.Process("<p>"+htmlEscape(e.Excerpt)+"</p>", link).HTML
		}
		// The page's own listing is the identity; content arrives with the
		// full article, so the hash covers what the page shows.
		e.ContentHash = hashOf(e.Title, e.URL, e.Excerpt)
		out = append(out, e)
		if len(out) == maxItems {
			break
		}
	}
	return out
}

// sameSite accepts the page's host and its subdomains or parent domain
// (blog.example.com listing www.example.com posts).
func sameSite(host, base string) bool {
	host, base = strings.TrimPrefix(strings.ToLower(host), "www."), strings.TrimPrefix(strings.ToLower(base), "www.")
	return host == base || strings.HasSuffix(host, "."+base) || strings.HasSuffix(base, "."+host)
}

// --- h-feed --------------------------------------------------------------

func hfeed(doc *goquery.Document, base *url.URL) []item {
	var out []item
	doc.Find(".h-entry").Each(func(_ int, s *goquery.Selection) {
		it := item{}
		if u, ok := s.Find(".u-url").First().Attr("href"); ok {
			it.url = resolve(base, u)
		} else if u, ok := s.Find("a[href]").First().Attr("href"); ok {
			it.url = resolve(base, u)
		}
		it.title = text(s.Find(".p-name").First())
		if it.title == "" {
			it.title = text(s.Find("a[href]").First())
		}
		it.summary = text(s.Find(".p-summary").First())
		it.published = dateOf(s.Find(".dt-published").First())
		it.image = imageOf(s.Find(".u-photo, img").First(), base)
		out = append(out, it)
	})
	return out
}

// --- JSON-LD -------------------------------------------------------------

var articleTypes = []string{"Article", "BlogPosting", "NewsArticle", "ScholarlyArticle", "TechArticle", "Report", "PodcastEpisode"}

func jsonld(doc *goquery.Document, base *url.URL) []item {
	var out []item
	var visit func(v any, inList bool)
	visit = func(v any, inList bool) {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				visit(e, inList)
			}
		case map[string]any:
			if g, ok := x["@graph"]; ok {
				visit(g, inList)
			}
			if l, ok := x["itemListElement"]; ok {
				visit(l, true)
			}
			if b, ok := x["blogPost"]; ok {
				visit(b, true)
			}
			if inList {
				if it, ok := x["item"]; ok {
					visit(it, true)
				}
			}
			if !hasType(x, articleTypes) && !(inList && x["url"] != nil && x["name"] != nil) {
				return
			}
			link := str(x["url"])
			if link == "" {
				link = str(x["@id"])
			}
			title := str(x["headline"])
			if title == "" {
				title = str(x["name"])
			}
			it := item{url: resolve(base, link), title: title, summary: str(x["description"])}
			it.published = parseDate(str(x["datePublished"]))
			it.image = resolve(base, imageField(x["image"]))
			out = append(out, it)
		}
	}
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		var v any
		if json.Unmarshal([]byte(s.Text()), &v) == nil {
			visit(v, false)
		}
	})
	return out
}

func hasType(m map[string]any, types []string) bool {
	switch t := m["@type"].(type) {
	case string:
		return slices.Contains(types, t)
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && slices.Contains(types, s) {
				return true
			}
		}
	}
	return false
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func imageField(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		if len(x) > 0 {
			return imageField(x[0])
		}
	case map[string]any:
		return str(x["url"])
	}
	return ""
}

// --- link pattern --------------------------------------------------------

// Sections that list things other than articles.
var notArticles = []string{"/tag/", "/tags/", "/category/", "/categories/", "/author/", "/authors/", "/page/", "/search", "/topics/", "/archive"}

// linkPrefix finds the path prefix shared by the most article-like links:
// same site, one segment below the prefix, with a real title.
func linkPrefix(doc *goquery.Document, base *url.URL) string {
	count := map[string]map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		h, _ := s.Attr("href")
		u, err := base.Parse(strings.TrimSpace(h))
		if err != nil || !sameSite(u.Host, base.Host) || len(linkTitle(s)) < minTitleText {
			return
		}
		p := strings.TrimSuffix(u.Path, "/")
		if p == "" || p == strings.TrimSuffix(base.Path, "/") {
			return
		}
		prefix := path.Dir(p)
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		for _, bad := range notArticles {
			if strings.Contains(prefix+path.Base(p), bad) {
				return
			}
		}
		if count[prefix] == nil {
			count[prefix] = map[string]bool{}
		}
		count[prefix][p] = true
	})
	best, n := "", 0
	for prefix, links := range count {
		// Prefer deeper prefixes on ties: /research/ over /.
		if len(links) > n || (len(links) == n && len(prefix) > len(best)) {
			best, n = prefix, len(links)
		}
	}
	if n < minItems {
		return ""
	}
	return best
}

func links(doc *goquery.Document, base *url.URL, prefix string) []item {
	// A post is often linked several times (image, headline, "read more",
	// a mention in another post's text): keep one item per address, in page
	// order, with the best title seen for it.
	var order []string
	byURL := map[string]*item{}
	heading := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		h, _ := s.Attr("href")
		u, err := base.Parse(strings.TrimSpace(h))
		if err != nil || !sameSite(u.Host, base.Host) {
			return
		}
		u.Fragment = ""
		p := strings.TrimSuffix(u.Path, "/")
		dir := path.Dir(p)
		if !strings.HasSuffix(dir, "/") {
			dir += "/"
		}
		if dir != prefix || p == strings.TrimSuffix(base.Path, "/") {
			return
		}
		key := u.String()
		it, ok := byURL[key]
		if !ok {
			it = &item{url: key}
			byURL[key] = it
			order = append(order, key)
		}
		title, isHeading := linkTitle(s), s.Find("h1, h2, h3, h4, h5, h6").Length() > 0 || s.ParentsFiltered("h1, h2, h3, h4, h5, h6").Length() > 0
		better := (isHeading && !heading[key]) || (isHeading == heading[key] && len(title) > len(it.title))
		if !better {
			return
		}
		it.title, heading[key] = title, isHeading
		// Date and image come from the card around the best link: the
		// nearest ancestor holding a date, at most three levels up.
		card := s
		for i := 0; i < 3 && card.Find("time").Length() == 0 && card.Parent().Length() > 0; i++ {
			card = card.Parent()
		}
		it.published = dateOf(card.Find("time").First())
		it.image = imageOf(card.Find("img").First(), base)
	})
	// Where articles are listed under headlines, a link without one is
	// navigation or a mention in running text.
	headlines := 0
	for _, k := range order {
		if heading[k] {
			headlines++
		}
	}
	var out []item
	for _, k := range order {
		if it := byURL[k]; len(it.title) >= minTitleText && (heading[k] || headlines < minItems) {
			out = append(out, *it)
		}
	}
	return out
}

// linkTitle is a link's headline: a heading inside it, else its text.
func linkTitle(s *goquery.Selection) string {
	if h := text(s.Find("h1, h2, h3, h4, h5, h6").First()); h != "" {
		return h
	}
	return text(s)
}

// --- CSS selectors -------------------------------------------------------

func css(doc *goquery.Document, base *url.URL, r Rules) []item {
	var out []item
	doc.Find(r.Item).Each(func(_ int, s *goquery.Selection) {
		it := item{}
		link := s
		if r.Link != "" {
			link = s.Find(r.Link).First()
		} else if !s.Is("a[href]") {
			link = s.Find("a[href]").First()
		}
		if h, ok := link.Attr("href"); ok {
			it.url = resolve(base, h)
		}
		if r.Title != "" {
			it.title = text(s.Find(r.Title).First())
		}
		if it.title == "" {
			it.title = linkTitle(link)
		}
		if r.Date != "" {
			it.published = dateOf(s.Find(r.Date).First())
		} else {
			it.published = dateOf(s.Find("time").First())
		}
		if r.Summary != "" {
			it.summary = text(s.Find(r.Summary).First())
		}
		if r.Image != "" {
			it.image = imageOf(s.Find(r.Image).First(), base)
		} else {
			it.image = imageOf(s.Find("img").First(), base)
		}
		out = append(out, it)
	})
	return out
}

// --- helpers -------------------------------------------------------------

func text(s *goquery.Selection) string {
	return strings.Join(strings.Fields(s.Text()), " ")
}

func resolve(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := base.Parse(ref)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.String()
}

func imageOf(s *goquery.Selection, base *url.URL) string {
	for _, attr := range []string{"src", "data-src"} {
		if v, ok := s.Attr(attr); ok && !strings.HasPrefix(strings.TrimSpace(v), "data:") {
			if u := resolve(base, v); u != "" {
				return u
			}
		}
	}
	return ""
}

func dateOf(s *goquery.Selection) time.Time {
	if s.Length() == 0 {
		return time.Time{}
	}
	for _, attr := range []string{"datetime", "content", "data-date"} {
		if v, ok := s.Attr(attr); ok {
			if t := parseDate(v); !t.IsZero() {
				return t
			}
		}
	}
	return parseDate(text(s))
}

var dateLayouts = []string{
	time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02",
	"January 2, 2006", "Jan 2, 2006", "2 January 2006", "2 Jan 2006", "January 2 2006", "Jan 2 2006",
	"02/01/2006", "2006/01/02", "Monday, January 2, 2006", "Mon, 02 Jan 2006 15:04:05 MST",
}

// parseDate reads the common date forms; unknown or future dates are zero.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "Published"))
	s = strings.TrimSpace(strings.TrimPrefix(s, "Updated"))
	if s == "" || len(s) > 64 {
		return time.Time{}
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			if t.After(time.Now().Add(48 * time.Hour)) {
				return time.Time{}
			}
			return t
		}
	}
	return time.Time{}
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func hashOf(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

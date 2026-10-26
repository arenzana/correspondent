package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/arenzana/correspondent/internal/feed"
	"github.com/arenzana/correspondent/internal/fetch"
	"github.com/arenzana/correspondent/internal/pagefeed"
	"github.com/arenzana/correspondent/internal/store"
)

// ErrNoFeed means a site has no feed and no article list the exact methods
// or an existing parser can read; an AI may still write a parser for it.
var ErrNoFeed = errors.New("no feed found at that address")

// Discover resolves a site or feed URL to the feeds it offers. A site with
// no feed is read as a page feed: by the parser the server already has for
// it, or by one worked out exactly and saved for every reader. An
// article's address leads to the listing above it.
func (s *Service) Discover(ctx context.Context, userID int64, raw string) ([]Candidate, error) {
	raw = resolveBuiltin(raw)
	resp, err := s.get(ctx, raw)
	if err != nil {
		return nil, err
	}
	if p, err := feed.Parse(resp.Body, resp.FinalURL); err == nil {
		return []Candidate{candidate(resp.FinalURL, p)}, nil
	}
	var out []Candidate
	links := feed.Discover(resp.Body, resp.FinalURL)
	for _, l := range links[:min(len(links), 4)] {
		r, err := s.Fetch.Get(ctx, fetch.Request{URL: l})
		if err != nil {
			continue
		}
		if p, err := feed.Parse(r.Body, r.FinalURL); err == nil {
			out = append(out, candidate(l, p))
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	for _, page := range s.listings(ctx, resp) {
		if c, ok := s.existing(ctx, page); ok {
			out = append(out, c)
			continue
		}
		if r, p, err := pagefeed.Detect(page.Body, page.FinalURL); err == nil {
			if _, err := s.Store.SaveParser(ctx, page.FinalURL, r.Encode(), r.Method, userID); err != nil {
				return nil, err
			}
			out = append(out, pageCandidate(page.FinalURL, r.Method, p, false))
		}
	}
	if len(out) == 0 {
		return nil, ErrNoFeed
	}
	return out, nil
}

// GetAI returns the reader's AI client; it is only called when a parser
// must be written, so the AI is never asked about a page twice.
type GetAI func() (*pagefeed.Suggester, error)

// DiscoverAI finds a parser for a page with no feed, writing one with the
// reader's AI only when the server has none that still reads the page (or
// rebuild is set). The AI's parser is saved for every reader.
func (s *Service) DiscoverAI(ctx context.Context, userID int64, raw string, getAI GetAI, rebuild bool) (Candidate, error) {
	raw = resolveBuiltin(raw)
	resp, err := s.get(ctx, raw)
	if err != nil {
		return Candidate{}, err
	}
	page := s.listings(ctx, resp)[0]
	// A working parser is reused; a rebuild replaces it, except a built-in
	// one that still reads the page.
	if c, ok := s.existing(ctx, page); ok && (!rebuild || c.builtin) {
		return c, nil
	}
	ai, err := getAI()
	if err != nil {
		return Candidate{}, err
	}
	r, p, err := ai.Suggest(ctx, page.Body, page.FinalURL)
	if err != nil {
		return Candidate{}, err
	}
	if _, err := s.Store.SaveParser(ctx, page.FinalURL, r.Encode(), r.Method, userID); err != nil {
		return Candidate{}, err
	}
	return pageCandidate(page.FinalURL, r.Method, p, false), nil
}

// resolveBuiltin maps an address a reader typed to a built-in entry's feed
// or page ("openai.com" → its news feed, "anthropic.com" → its news page).
func resolveBuiltin(raw string) string {
	b := pagefeed.LookupBuiltin(raw)
	switch {
	case b == nil:
		return raw
	case b.Feed != "":
		return b.Feed
	default:
		return b.Page
	}
}

// SeedBuiltins installs the parsers shipped with the app.
func (s *Service) SeedBuiltins(ctx context.Context) error {
	for _, b := range pagefeed.Builtins() {
		if b.Rules == nil {
			continue
		}
		if err := s.Store.SeedParser(ctx, b.Page, b.Rules.Encode(), b.Rules.Method); err != nil {
			return fmt.Errorf("built-in parser %s: %w", b.Name, err)
		}
	}
	return nil
}

// Rebuild writes a new parser for a page whose parser stopped reading it:
// exactly if the page allows, else with the reader's AI. Every reader of
// the page gets the new parser.
func (s *Service) Rebuild(ctx context.Context, userID int64, raw string, getAI GetAI) (Candidate, error) {
	resp, err := s.get(ctx, raw)
	if err != nil {
		return Candidate{}, err
	}
	if r, p, err := pagefeed.Detect(resp.Body, resp.FinalURL); err == nil {
		if _, err := s.Store.SaveParser(ctx, resp.FinalURL, r.Encode(), r.Method, userID); err != nil {
			return Candidate{}, err
		}
		return pageCandidate(resp.FinalURL, r.Method, p, false), nil
	}
	return s.DiscoverAI(ctx, userID, resp.FinalURL, getAI, true)
}

// existing tries the parser the server already has for a page.
func (s *Service) existing(ctx context.Context, page *fetch.Response) (Candidate, bool) {
	parser, err := s.Store.ParserFor(ctx, page.FinalURL)
	if err != nil {
		return Candidate{}, false
	}
	r, err := pagefeed.Decode(parser.Rules)
	if err != nil {
		return Candidate{}, false
	}
	p, err := pagefeed.Parse(page.Body, page.FinalURL, r)
	if err != nil {
		return Candidate{}, false
	}
	c := pageCandidate(page.FinalURL, r.Method, p, true)
	c.builtin = parser.Builtin
	return c, true
}

// SubscribePage follows a page with the server's parser for it (found or
// written by Discover or DiscoverAI) and polls it immediately.
func (s *Service) SubscribePage(ctx context.Context, userID int64, url, title string, folderID *int64) (int64, error) {
	parser, err := s.Store.ParserFor(ctx, url)
	if errors.Is(err, store.ErrNotFound) {
		return 0, errors.New("there's no parser for that page yet; find its articles first")
	} else if err != nil {
		return 0, err
	}
	id, err := s.Store.AddPageFeed(ctx, userID, url, title, folderID, parser.ID)
	if err != nil {
		return 0, err
	}
	f, err := s.Store.Feed(ctx, userID, id)
	if err != nil {
		return 0, err
	}
	s.Poll(ctx, *f)
	return id, nil
}

func (s *Service) get(ctx context.Context, raw string) (*fetch.Response, error) {
	u := strings.TrimSpace(raw)
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	resp, err := s.Fetch.Get(ctx, fetch.Request{URL: u})
	if err != nil {
		return nil, fmt.Errorf("couldn't load %s: %w", u, err)
	}
	return resp, nil
}

// listings returns the pages that may list articles: the page itself, or,
// for an article, the listing pages above it.
func (s *Service) listings(ctx context.Context, resp *fetch.Response) []*fetch.Response {
	if !pagefeed.IsArticle(resp.Body) {
		return []*fetch.Response{resp}
	}
	var out []*fetch.Response
	for _, u := range pagefeed.Parents(resp.FinalURL) {
		if r, err := s.Fetch.Get(ctx, fetch.Request{URL: u}); err == nil {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return []*fetch.Response{resp}
	}
	return out
}

func pageCandidate(url, method string, p *feed.Parsed, shared bool) Candidate {
	c := candidate(url, p)
	c.Page, c.Method, c.Shared = true, method, shared
	for _, it := range p.Items[:min(len(p.Items), 5)] {
		c.Sample = append(c.Sample, it.Title)
	}
	return c
}

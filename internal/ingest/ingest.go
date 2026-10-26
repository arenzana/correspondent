// Package ingest polls due feeds, stores new entries, and subscribes to new
// feeds (with discovery from HTML pages). It is the only writer of feed
// content; a single in-process scheduler replaces a job queue because there
// is exactly one application replica.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/arenzana/correspondent/internal/feed"
	"github.com/arenzana/correspondent/internal/fetch"
	"github.com/arenzana/correspondent/internal/store"
)

type Service struct {
	Store       *store.Store
	Fetch       *fetch.Client
	Log         *slog.Logger
	Concurrency int
	Tick        time.Duration
	// Notify is called after new entries arrive for a user (for live UI updates).
	Notify func(userID int64)
	// RetentionDays deletes read, unstarred articles older than this that
	// have left their feed. Zero keeps everything (the default).
	RetentionDays int

	inflight sync.Map // feed ID → struct{}: prevents overlapping polls of one feed
}

// Run polls until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	if s.Concurrency == 0 {
		s.Concurrency = 6
	}
	if s.Tick == 0 {
		s.Tick = 30 * time.Second
	}
	ticker := time.NewTicker(s.Tick)
	defer ticker.Stop()
	// Never prune at startup: the first pass waits a day, so a fresh deploy
	// or a migration can be checked before anything is deleted.
	lastRetention := time.Now()
	for {
		s.pollDue(ctx)
		if time.Since(lastRetention) > 24*time.Hour {
			if s.RetentionDays > 0 {
				n, err := s.Store.Retention(ctx, time.Now().AddDate(0, 0, -s.RetentionDays))
				if err != nil {
					s.Log.Error("retention", "err", err)
				} else if n > 0 {
					s.Log.Info("retention", "deleted", n)
				}
			}
			if _, err := s.Store.PurgeSessions(ctx); err != nil {
				s.Log.Error("purge sessions", "err", err)
			}
			lastRetention = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) pollDue(ctx context.Context) {
	feeds, err := s.Store.DueFeeds(ctx, time.Now(), 50)
	if err != nil {
		s.Log.Error("due feeds", "err", err)
		return
	}
	sem := make(chan struct{}, s.Concurrency)
	var wg sync.WaitGroup
	for _, f := range feeds {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			s.Poll(ctx, f)
		}()
	}
	wg.Wait()
}

// Poll fetches one feed and records the result. Safe to call concurrently;
// overlapping polls of the same feed are skipped.
func (s *Service) Poll(ctx context.Context, f store.Feed) {
	if _, busy := s.inflight.LoadOrStore(f.ID, struct{}{}); busy {
		return
	}
	defer s.inflight.Delete(f.ID)

	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	resp, err := s.Fetch.Get(ctx, fetch.Request{URL: f.URL, ETag: f.ETag, LastModified: f.LastModified})
	res := store.FetchResult{}
	if resp != nil {
		res.Status = resp.Status
	}
	var inserted, updated int
	switch {
	case err != nil:
		res.Err = err.Error()
	case resp.NotModified:
		res.ETag, res.LastModified = f.ETag, f.LastModified
	default:
		p, perr := feed.Parse(resp.Body, resp.FinalURL)
		if perr != nil {
			res.Err = "could not read the feed: " + perr.Error()
			break
		}
		// Write in short transactions, never across the network fetch.
		var readBefore time.Time
		if f.LastFetchAt == 0 {
			readBefore = time.Now().AddDate(0, 0, -14)
		}
		inserted, updated, err = s.Store.UpsertEntries(ctx, f.ID, p.Items, readBefore)
		if err != nil {
			res.Err = "could not save entries: " + err.Error()
			break
		}
		res.ETag, res.LastModified = resp.ETag, resp.LastModified
		res.Title, res.SiteURL, res.Description, res.IconURL = p.Title, p.SiteURL, p.Description, p.IconURL
	}
	res.NextFetch = nextFetch(f, res.Err != "", resp)
	if err := s.Store.SaveFetchResult(context.WithoutCancel(ctx), f.ID, res); err != nil {
		s.Log.Error("save fetch result", "feed", f.ID, "err", err)
	}
	// Feed URLs can embed private tokens, so logs carry the feed ID and a
	// URL-free error only. The full message stays in the database for the user.
	s.Log.Info("poll", "feed", f.ID, "status", res.Status, "inserted", inserted, "updated", updated,
		"ms", time.Since(start).Milliseconds(), "error", redact(res.Err))
	if inserted > 0 && s.Notify != nil {
		s.Notify(f.UserID)
	}
}

// nextFetch schedules the next poll: the feed's interval with ±10% jitter,
// exponential backoff after errors (capped at a day), and Retry-After honoured.
func nextFetch(f store.Feed, failed bool, resp *fetch.Response) time.Time {
	interval := time.Duration(f.IntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	if failed {
		backoff := interval << min(f.ErrorCount+1, 6)
		interval = min(backoff, 24*time.Hour)
	}
	if resp != nil && resp.RetryAfter > interval {
		interval = min(resp.RetryAfter, 48*time.Hour)
	}
	jitter := time.Duration(rand.Int64N(int64(interval)/5)) - interval/10
	return time.Now().Add(interval + jitter)
}

// Candidate is a feed found while resolving a URL to subscribe to.
type Candidate struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	SiteURL     string `json:"siteUrl"`
	IconURL     string `json:"iconUrl"`
	ItemCount   int    `json:"itemCount"`
	LatestAt    int64  `json:"latestAt"`
}

// Discover resolves a site or feed URL to the feeds it offers.
func (s *Service) Discover(ctx context.Context, raw string) ([]Candidate, error) {
	u := strings.TrimSpace(raw)
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	resp, err := s.Fetch.Get(ctx, fetch.Request{URL: u})
	if err != nil {
		return nil, fmt.Errorf("couldn't load %s: %w", u, err)
	}
	if p, err := feed.Parse(resp.Body, resp.FinalURL); err == nil {
		return []Candidate{candidate(resp.FinalURL, p)}, nil
	}
	links := feed.Discover(resp.Body, resp.FinalURL)
	if len(links) == 0 {
		return nil, errors.New("no feed found at that address")
	}
	var out []Candidate
	for _, l := range links[:min(len(links), 4)] {
		r, err := s.Fetch.Get(ctx, fetch.Request{URL: l})
		if err != nil {
			continue
		}
		if p, err := feed.Parse(r.Body, r.FinalURL); err == nil {
			out = append(out, candidate(l, p))
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the page lists feeds, but none could be read")
	}
	return out, nil
}

func candidate(url string, p *feed.Parsed) Candidate {
	c := Candidate{URL: url, Title: p.Title, Description: p.Description, SiteURL: p.SiteURL,
		IconURL: p.IconURL, ItemCount: len(p.Items)}
	for _, it := range p.Items {
		if t := it.PublishedAt.Unix(); !it.PublishedAt.IsZero() && t > c.LatestAt {
			c.LatestAt = t
		}
	}
	return c
}

// Subscribe adds a feed for the user and polls it immediately.
func (s *Service) Subscribe(ctx context.Context, userID int64, url, title string, folderID *int64) (int64, error) {
	id, err := s.Store.AddFeed(ctx, userID, url, title, folderID)
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

var urlPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"']+`)

// redact replaces any URL in a log message with a placeholder.
func redact(msg string) string {
	return urlPattern.ReplaceAllString(msg, "<url>")
}

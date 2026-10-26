// Package ingest polls due feeds, stores new entries, and subscribes to new
// feeds (with discovery from HTML pages). It is the only writer of feed
// content; a single in-process scheduler replaces a job queue because there
// is exactly one application replica.
package ingest

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"sync"
	"time"

	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/extract"
	"github.com/arenzana/correspondent/internal/feed"
	"github.com/arenzana/correspondent/internal/fetch"
	"github.com/arenzana/correspondent/internal/pagefeed"
	"github.com/arenzana/correspondent/internal/store"
)

type Service struct {
	Store *store.Store
	Fetch *fetch.Client
	// Extract fetches full articles for feeds that publish only summaries.
	Extract *extract.Extractor
	// AIFor gives a reader's own AI client, for pages with no feed added by
	// apps (SubscribeAuto). Nil leaves AI to the web app.
	AIFor       AIForUser
	Log         *slog.Logger
	Concurrency int
	Tick        time.Duration
	// Notify is called after new entries arrive for a user (for live UI updates).
	Notify func(userID int64)
	// OnNewEntries is called (in its own goroutine) after a poll inserted
	// entries, with the crawl ID they are all above; used for notifications.
	OnNewEntries func(ctx context.Context, f store.Feed, since int64)
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
		p, perr := s.parse(ctx, resp.Body, resp.FinalURL, f)
		if perr != nil {
			res.Err = perr.Error()
			break
		}
		if f.FullText && s.Extract != nil {
			s.fetchFullArticles(ctx, f, p.Items)
		}
		// Write in short transactions, never across the network fetch.
		since := time.Now().UnixMicro() - 1 // every ID allocated below is above this
		var readBefore time.Time
		if f.LastFetchAt == 0 {
			readBefore = time.Now().AddDate(0, 0, -14)
		}
		inserted, updated, err = s.Store.UpsertEntries(ctx, f.ID, p.Items, readBefore)
		if err != nil {
			res.Err = "could not save entries: " + err.Error()
			break
		}
		if inserted > 0 && s.OnNewEntries != nil {
			go s.OnNewEntries(context.WithoutCancel(ctx), f, since)
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

// parse reads a feed, or a page feed's listing with its parser.
func (s *Service) parse(ctx context.Context, body []byte, url string, f store.Feed) (*feed.Parsed, error) {
	if f.ParserID == 0 {
		p, err := feed.Parse(body, url)
		if err != nil {
			return nil, errors.New("could not read the feed: " + err.Error())
		}
		return p, nil
	}
	parser, err := s.Store.Parser(ctx, f.ParserID)
	if err != nil {
		return nil, errors.New("this page feed has lost its parser; rebuild it in Subscriptions")
	}
	r, err := pagefeed.Decode(parser.Rules)
	if err != nil {
		return nil, errors.New("this page feed's parser is invalid; rebuild it in Subscriptions")
	}
	p, err := pagefeed.Parse(body, url, r)
	if err != nil {
		return nil, errors.New("the page no longer lists its articles the way it did; rebuild it in Subscriptions")
	}
	return p, nil
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
	// Page candidates are web pages with no feed, read by a parser: Method
	// says how it finds the articles, Shared that the server already had it
	// (written for another reader), Sample shows a few titles.
	Page   bool     `json:"page,omitempty"`
	Method string   `json:"method,omitempty"`
	Shared bool     `json:"shared,omitempty"`
	Sample []string `json:"sample,omitempty"`

	builtin bool
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
	s.autoFullText(ctx, *f)
	return id, nil
}

var urlPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"']+`)

// redact replaces any URL in a log message with a placeholder.
func redact(msg string) string {
	return urlPattern.ReplaceAllString(msg, "<url>")
}

const (
	fullPerPoll     = 20 // full articles fetched per poll at most
	fullConcurrency = 3
	fullTimeout     = 20 * time.Second
)

// fetchFullArticles replaces the content of new items with the article
// fetched from their page, keeping the feed's HTML. Items already stored
// are left alone; failures are recorded per item and never fail the poll.
func (s *Service) fetchFullArticles(ctx context.Context, f store.Feed, items []store.NewEntry) {
	guids := make([]string, len(items))
	for i, it := range items {
		guids[i] = it.GUID
	}
	known, err := s.Store.KnownGUIDs(ctx, f.ID, guids)
	if err != nil {
		s.Log.Error("full text", "feed", f.ID, "err", err)
		return
	}
	sem := make(chan struct{}, fullConcurrency)
	var wg sync.WaitGroup
	n := 0
	for i := range items {
		it := &items[i]
		if known[it.GUID] || it.URL == "" || n >= fullPerPoll {
			continue
		}
		n++
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			actx, cancel := context.WithTimeout(ctx, fullTimeout)
			defer cancel()
			r, err := s.Extract.Article(actx, it.URL)
			if err != nil {
				it.FullError = err.Error()
				return
			}
			it.FullText, it.FeedContent, it.Content = true, it.Content, r.HTML
			if it.Excerpt == "" {
				it.Excerpt = content.Excerpt(r.Text, 320)
			}
			if it.ImageURL == "" {
				it.ImageURL = r.ImageURL
			}
			if it.PublishedAt.IsZero() {
				it.PublishedAt = r.Published
			}
		}()
	}
	wg.Wait()
}

// Backfill fetches full articles for a feed's newest stored entries, after
// its full-article switch is turned on.
func (s *Service) Backfill(ctx context.Context, f store.Feed) {
	if s.Extract == nil {
		return
	}
	entries, err := s.Store.WithoutFullText(ctx, f.ID, fullPerPoll)
	if err != nil {
		s.Log.Error("full text backfill", "feed", f.ID, "err", err)
		return
	}
	sem := make(chan struct{}, fullConcurrency)
	var wg sync.WaitGroup
	for _, e := range entries {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			if _, err := s.FullArticle(ctx, f.UserID, e); err != nil {
				s.Log.Info("full text", "entry", e.ID, "error", redact(err.Error()))
			}
		}()
	}
	wg.Wait()
	if s.Notify != nil {
		s.Notify(f.UserID)
	}
}

// FullArticle fetches one entry's full article and stores it, or records
// why it couldn't.
func (s *Service) FullArticle(ctx context.Context, userID int64, e store.Entry) (*extract.Result, error) {
	if s.Extract == nil {
		return nil, errors.New("full articles are not available")
	}
	actx, cancel := context.WithTimeout(ctx, fullTimeout)
	defer cancel()
	r, err := s.Extract.Article(actx, e.URL)
	if err != nil {
		s.Store.SetFullError(context.WithoutCancel(ctx), userID, e.ID, err.Error())
		return nil, err
	}
	return r, s.Store.SetFullText(ctx, userID, e.ID, r.HTML, content.Excerpt(r.Text, 320), r.ImageURL)
}

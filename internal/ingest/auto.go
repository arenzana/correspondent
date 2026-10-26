package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/pagefeed"
	"github.com/arenzana/correspondent/internal/store"
)

// SubscribeAuto subscribes to whatever an address offers, choosing for the
// reader, for apps (Reeder) that can only send an address: its feed if it
// has one; else its article list read exactly; else, when the reader has
// set up AI, the list as the AI reads it. folderID may be nil.
func (s *Service) SubscribeAuto(ctx context.Context, userID int64, raw, title string, folderID *int64) (*store.Feed, error) {
	getAI := func() (*pagefeed.Suggester, error) {
		if s.AIFor == nil {
			return nil, pagefeed.ErrAIUnavailable
		}
		return s.AIFor(ctx, userID)
	}
	cands, err := s.Discover(ctx, userID, raw)
	switch {
	case errors.Is(err, ErrNoFeed):
		var c Candidate
		if c, err = s.DiscoverAI(ctx, userID, raw, getAI, false); err != nil {
			return nil, fmt.Errorf("%w, and %w", ErrNoFeed, err)
		}
		cands = []Candidate{c}
	case err == nil && cands[0].Page && !cands[0].Shared && cands[0].Method == pagefeed.MethodLinks && s.AIFor != nil:
		// A link pattern is the weakest exact reading; with no one to look
		// at the result, prefer a parser from the reader's AI when set up.
		if c, aerr := s.DiscoverAI(ctx, userID, raw, getAI, true); aerr == nil {
			cands = []Candidate{c}
		}
	}
	if err != nil {
		return nil, err
	}
	c := cands[0]
	if title == "" {
		title = c.Title
	}
	var id int64
	if c.Page {
		id, err = s.SubscribePage(ctx, userID, c.URL, title, folderID)
	} else {
		id, err = s.Subscribe(ctx, userID, c.URL, title, folderID)
	}
	if err != nil {
		return nil, err
	}
	return s.Store.Feed(ctx, userID, id)
}

// autoFullText turns full articles on for a newly added feed whose first
// articles are summaries, and fetches the latest in full. Reeder and other
// apps then get whole articles without anyone flipping a switch.
func (s *Service) autoFullText(ctx context.Context, f store.Feed) {
	if f.FullText || s.Extract == nil {
		return
	}
	entries, _, err := s.Store.Entries(ctx, store.Filter{UserID: f.UserID, FeedID: f.ID, Limit: 10}, true)
	if err != nil {
		return
	}
	linked, partial := 0, 0
	for _, e := range entries {
		if e.URL == "" {
			continue
		}
		linked++
		if content.LooksPartial(e.Content) {
			partial++
		}
	}
	if linked < 3 || partial*2 < linked {
		return
	}
	if err := s.Store.SetFeedFullText(ctx, f.UserID, f.ID, true); err != nil {
		s.Log.Error("auto full text", "feed", f.ID, "err", err)
		return
	}
	s.Log.Info("auto full text", "feed", f.ID, "partial", partial, "of", linked)
	f.FullText = true
	go s.Backfill(context.WithoutCancel(ctx), f)
}

// AIForUser returns the reader's AI client, or an error explaining why
// there is none (no key saved, daily limit reached).
type AIForUser func(ctx context.Context, userID int64) (*pagefeed.Suggester, error)

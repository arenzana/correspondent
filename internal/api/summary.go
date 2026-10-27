package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/pagefeed"
	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/store"
)

// Summaries. A reader asks for one, and their own AI writes it, once:
// summaries are stored by the article's text and shared, so anyone else
// with the same article gets it without using their AI.

// SummariesPerDay is how many new summaries a reader's AI writes a day.
const SummariesPerDay = 100

var (
	summaryOnce  sync.Once
	summaryLimit *ratelimit.Limiter
	summarizing  sync.Map // text hash → *sync.Mutex
)

func (a *API) summaryLimiter() *ratelimit.Limiter {
	summaryOnce.Do(func() { summaryLimit = ratelimit.New(SummariesPerDay, 24*time.Hour) })
	return summaryLimit
}

// summaryText is what a summary is made from, and summaryHash names it.
func summaryText(e *store.Entry) string { return content.SearchText(e.Content, pagefeed.SummaryInput) }

func summaryHash(title, text string) string {
	sum := sha256.Sum256([]byte(title + "\x00" + text))
	return hex.EncodeToString(sum[:])[:20]
}

// sharedSummary adds an article's summary, if anyone has had one made.
func (a *API) sharedSummary(ctx context.Context, e *store.Entry) {
	if s, ok, err := a.Store.Summary(ctx, summaryHash(e.Title, summaryText(e))); err == nil && ok {
		e.Summary = s
	}
}

// summarize returns an article's summary, made with the reader's AI if no
// one has had it made yet.
func (a *API) summarize(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusNotFound, "No such article.")
		return
	}
	e, err := a.Store.Entry(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "No such article.")
		return
	} else if err != nil {
		a.internal(w, err)
		return
	}
	text := summaryText(e)
	if text == "" {
		fail(w, http.StatusUnprocessableEntity, "This article has no text to summarize.")
		return
	}
	hash := summaryHash(e.Title, text)
	mu, _ := summarizing.LoadOrStore(hash, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	if s, ok, err := a.Store.Summary(r.Context(), hash); err != nil {
		a.internal(w, err)
		return
	} else if ok {
		reply(w, map[string]any{"summary": s})
		return
	}
	ai, err := a.suggester(r.Context(), uid)
	switch {
	case errors.Is(err, pagefeed.ErrAIUnavailable):
		fail(w, http.StatusNotImplemented, "AI isn't available on this server.")
		return
	case errors.Is(err, errNoAI):
		fail(w, http.StatusConflict, "Set up AI in Settings first.")
		return
	case err != nil:
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	}
	limitKey := fmt.Sprintf("summary:%d", uid)
	if !a.summaryLimiter().Allow(limitKey) {
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("Your AI has written %d summaries today, the daily limit. Try again tomorrow.", SummariesPerDay))
		return
	}
	a.summaryLimiter().Fail(limitKey)
	// Finish even if the reader moves on: the provider is paid either way.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	s, err := ai.Summarize(ctx, e.Title, text)
	if err != nil {
		fail(w, http.StatusBadGateway, sentence(err.Error()))
		return
	}
	if err := a.Store.SaveSummary(r.Context(), hash, s); err != nil {
		a.internal(w, err)
		return
	}
	a.Log.Info("summarized", "entry", id)
	reply(w, map[string]any{"summary": s})
}

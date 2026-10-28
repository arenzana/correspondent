package integrations

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/arenzana/correspondent/internal/secretbox"
	"github.com/arenzana/correspondent/internal/store"
	"github.com/arenzana/correspondent/internal/textify"
)

// Service queues articles for a reader's integrations and sends them.
type Service struct {
	Store   *store.Store
	Keyring *secretbox.Box
	// HTTP carries every request to a service: the fetcher's guarded
	// transport, so a reader's address can't reach the server's network.
	HTTP *http.Client
	Log  *slog.Logger
	// PlainHTTP allows http:// addresses (the admin let integrations reach
	// networks of their own, where plain HTTP is common).
	PlainHTTP bool

	kick chan struct{}
}

// secretContext binds a sealed secret to its reader.
const secretContext = "user:%d:integration"

// Seal seals a reader's credentials.
func (s *Service) Seal(uid int64, sec Secret) ([]byte, error) {
	b, err := json.Marshal(sec)
	if err != nil {
		return nil, err
	}
	return s.Keyring.Seal(b, fmt.Sprintf(secretContext, uid)), nil
}

// Open opens a reader's sealed credentials.
func (s *Service) Open(uid int64, sealed []byte) (Secret, error) {
	var sec Secret
	if sealed == nil {
		return sec, nil
	}
	plain, err := s.Keyring.Open(sealed, fmt.Sprintf(secretContext, uid))
	if err != nil {
		return sec, err
	}
	return sec, json.Unmarshal(plain, &sec)
}

// NewSigningKey makes a webhook's signing key.
func NewSigningKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "whsec_" + base64.RawURLEncoding.EncodeToString(b)
}

// Connector returns a stored integration's connector.
func (s *Service) Connector(ctx context.Context, uid int64, i *store.Integration) (Connector, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(i.Config), &cfg); err != nil {
		return nil, err
	}
	sealed, err := s.Store.IntegrationSecret(ctx, uid, i.ID)
	if err != nil {
		return nil, err
	}
	sec, err := s.Open(uid, sealed)
	if err != nil {
		return nil, err
	}
	return New(i.Kind, cfg, sec, s.HTTP)
}

// ArticleFor builds what's sent for a reader's entry.
func (s *Service) ArticleFor(ctx context.Context, uid, entryID int64) (Article, error) {
	e, err := s.Store.Entry(ctx, uid, entryID)
	if err != nil {
		return Article{}, err
	}
	a := Article{ID: strconv.FormatInt(e.ID, 10), Title: e.Title, URL: e.URL, Author: e.Author, Summary: e.Excerpt,
		HTML: e.Content, PublishedAt: time.Unix(e.PublishedAt, 0), Starred: e.Starred}
	if e.Summary != "" {
		a.Summary = e.Summary
	}
	if e.Content != "" {
		a.Markdown = textify.Markdown(e.Content, e.URL)
	}
	if f, err := s.Store.Feed(ctx, uid, e.FeedID); err == nil {
		a.Feed = f.Title
	}
	if a.Title == "" {
		a.Title = a.URL
	}
	return a, nil
}

// Kick wakes the worker: a reader just asked for something to be sent.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Starred queues what a reader starred for their send-starred rules.
func (s *Service) Starred(ctx context.Context, uid int64, ids []int64) {
	if n, err := s.Store.QueueStarred(ctx, uid, ids); err != nil {
		s.Log.Warn("queue starred", "error", err)
	} else if n > 0 {
		s.Kick()
	}
}

// backoff is how long to wait after each failed try; past the last, the
// delivery has failed.
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// Run sends due deliveries until ctx ends: every half minute, and at once
// when kicked.
func (s *Service) Run(ctx context.Context) {
	if s.kick == nil {
		s.kick = make(chan struct{}, 1)
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		s.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.kick:
		}
	}
}

// Init prepares a Service that isn't Run (tests send with RunOnce).
func (s *Service) Init() {
	if s.kick == nil {
		s.kick = make(chan struct{}, 1)
	}
}

// RunOnce sends what's due now.
func (s *Service) RunOnce(ctx context.Context) {
	due, err := s.Store.DueDeliveries(ctx, time.Now(), 20)
	if err != nil {
		s.Log.Warn("deliveries", "error", err)
		return
	}
	for _, d := range due {
		s.deliver(ctx, d)
	}
}

func (s *Service) deliver(ctx context.Context, d store.Delivery) {
	fail := func(err error) {
		retry, after := Retryable(err)
		var at time.Time
		if retry && d.Attempts < len(backoff) {
			at = time.Now().Add(max(backoff[d.Attempts], after))
		}
		s.Store.DeliveryFailed(ctx, d.ID, sentence(err), at)
		if errors.Is(err, ErrRejected) || at.IsZero() {
			s.Store.IntegrationResult(ctx, d.IntegrationID, sentence(err))
		}
		s.Log.Info("delivery failed", "delivery", d.ID, "integration", d.IntegrationID, "retry", !at.IsZero(), "error", err)
	}
	i, err := s.Store.Integration(ctx, d.UserID, d.IntegrationID)
	if err != nil {
		fail(err)
		return
	}
	c, err := s.Connector(ctx, d.UserID, i)
	if err != nil {
		fail(err)
		return
	}
	a, err := s.ArticleFor(ctx, d.UserID, d.EntryID)
	if err != nil {
		fail(err)
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	remote, err := c.Send(sendCtx, a)
	if err != nil {
		fail(err)
		return
	}
	s.Store.DeliverySent(ctx, d.ID, remote)
	s.Store.IntegrationResult(ctx, d.IntegrationID, "")
	s.Log.Info("sent", "delivery", d.ID, "integration", i.Kind, "entry", d.EntryID)
}

// sentence makes an error read as one: capitalised, ending with a stop.
func sentence(err error) string {
	r := []rune(err.Error())
	if len(r) == 0 {
		return ""
	}
	r[0] = unicode.ToUpper(r[0])
	m := string(r)
	if !strings.HasSuffix(m, ".") {
		m += "."
	}
	return m
}

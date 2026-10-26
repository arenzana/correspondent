// Package api implements the modern JSON API consumed by the web reader.
// It is deliberately unrelated to the Google Reader representation, which
// lives in package greader.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-webauthn/webauthn/webauthn"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arenzana/correspondent/internal/imgproxy"
	"github.com/arenzana/correspondent/internal/ingest"
	"github.com/arenzana/correspondent/internal/opml"
	"github.com/arenzana/correspondent/internal/pagefeed"
	"github.com/arenzana/correspondent/internal/push"
	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/secretbox"
	"github.com/arenzana/correspondent/internal/store"
	"github.com/arenzana/correspondent/internal/version"
)

const (
	cookieName = "correspondent_session"
	sessionTTL = 30 * 24 * time.Hour
	// Mutations must carry this header. Browsers cannot add a custom header
	// cross-origin without a CORS preflight, which we never grant.
	csrfHeader = "X-Correspondent"
)

type API struct {
	Store    *store.Store
	Ingest   *ingest.Service
	Log      *slog.Logger
	Secure   bool // set the Secure flag on cookies (behind HTTPS)
	Limiter  *ratelimit.Limiter
	ClientIP ratelimit.ClientIP
	Push     *push.Notifier
	// Images, when set, routes article images, thumbnails and feed icons
	// through the server so third parties never see the reader.
	Images *imgproxy.Proxy
	// Keyring seals readers' AI keys; nil turns AI page reading off.
	Keyring *secretbox.Box
	// AITransport carries requests to AI providers (the fetcher's guarded
	// transport); AIOptions add Anthropic client options (tests).
	AITransport http.RoundTripper
	AIOptions   []option.RequestOption
	// WebAuthn signs readers in with passkeys; nil when the server has no
	// public address configured.
	WebAuthn *webauthn.WebAuthn
	// DataDir is where the database lives, for the admin's server view.
	DataDir string
	// PublicURL is the address readers use (CORRESPONDENT_PUBLIC_URL).
	PublicURL string

	hub        hub
	ceremonies ceremonies
	aiOnce     sync.Once
	ai         *ratelimit.Limiter
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/session", a.login)
	mux.HandleFunc("DELETE /api/v1/session", a.auth(a.logout))
	mux.HandleFunc("GET /api/v1/me", a.auth(a.me))
	mux.HandleFunc("GET /api/v1/sessions", a.auth(a.sessions))
	mux.HandleFunc("GET /api/v1/tokens", a.auth(a.tokens))
	mux.HandleFunc("POST /api/v1/tokens", a.session(a.createToken))
	mux.HandleFunc("DELETE /api/v1/tokens/{id}", a.session(a.revokeToken))
	mux.HandleFunc("POST /api/v1/tokens/login", a.tokenLogin)
	mux.HandleFunc("DELETE /api/v1/tokens/current", a.auth(a.revokeCurrentToken))
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", a.session(a.endSession))
	mux.HandleFunc("PUT /api/v1/password", a.session(a.changePassword))
	mux.HandleFunc("GET /api/v1/invites/{token}", a.invite)
	mux.HandleFunc("POST /api/v1/invites/{token}", a.acceptInvite)
	mux.HandleFunc("GET /api/v1/admin/accounts", a.admin(a.adminAccounts))
	mux.HandleFunc("POST /api/v1/admin/invites", a.admin(a.createInvite))
	mux.HandleFunc("DELETE /api/v1/admin/invites/{id}", a.admin(a.revokeInvite))
	mux.HandleFunc("PATCH /api/v1/admin/accounts/{id}", a.admin(a.updateAccount))
	mux.HandleFunc("POST /api/v1/admin/accounts/{id}/reset", a.admin(a.resetLink))
	mux.HandleFunc("DELETE /api/v1/admin/accounts/{id}", a.admin(a.deleteAccount))
	mux.HandleFunc("GET /api/v1/admin/server", a.admin(a.adminServer))
	mux.HandleFunc("GET /api/v1/admin/parsers", a.admin(a.adminParsers))
	mux.HandleFunc("POST /api/v1/admin/parsers/{id}/test", a.admin(a.testParser))
	mux.HandleFunc("POST /api/v1/admin/parsers/{id}/rebuild", a.admin(a.rebuildParser))
	mux.HandleFunc("DELETE /api/v1/admin/parsers/{id}", a.admin(a.deleteParser))
	mux.HandleFunc("GET /api/v1/auth", a.authOptions)
	mux.HandleFunc("POST /api/v1/session/passkey", a.passkeyLoginBegin)
	mux.HandleFunc("POST /api/v1/session/passkey/finish", a.passkeyLoginFinish)
	mux.HandleFunc("GET /api/v1/passkeys", a.auth(a.passkeys))
	mux.HandleFunc("POST /api/v1/passkeys", a.session(a.passkeyRegisterBegin))
	mux.HandleFunc("POST /api/v1/passkeys/finish", a.session(a.passkeyRegisterFinish))
	mux.HandleFunc("PATCH /api/v1/passkeys/{id}", a.session(a.renamePasskey))
	mux.HandleFunc("DELETE /api/v1/passkeys/{id}", a.session(a.deletePasskey))
	mux.HandleFunc("PUT /api/v1/prefs", a.auth(a.putPrefs))
	mux.HandleFunc("PUT /api/v1/api-password", a.session(a.putAPIPassword))
	mux.HandleFunc("GET /api/v1/tree", a.auth(a.tree))
	mux.HandleFunc("GET /api/v1/entries", a.auth(a.entries))
	mux.HandleFunc("GET /api/v1/entries/{id}", a.auth(a.entry))
	mux.HandleFunc("POST /api/v1/entries/state", a.auth(a.setState))
	mux.HandleFunc("POST /api/v1/entries/{id}/full", a.auth(a.fullArticle))
	mux.HandleFunc("DELETE /api/v1/entries/{id}/full", a.auth(a.revertFullArticle))
	mux.HandleFunc("POST /api/v1/entries/mark-all-read", a.auth(a.markAllRead))
	mux.HandleFunc("POST /api/v1/discover", a.auth(a.discover))
	mux.HandleFunc("POST /api/v1/discover/ai", a.auth(a.discoverAI))
	mux.HandleFunc("PUT /api/v1/ai", a.session(a.putAI))
	mux.HandleFunc("DELETE /api/v1/ai", a.session(a.deleteAI))
	mux.HandleFunc("POST /api/v1/feeds", a.auth(a.subscribe))
	mux.HandleFunc("PATCH /api/v1/feeds/{id}", a.auth(a.updateFeed))
	mux.HandleFunc("DELETE /api/v1/feeds/{id}", a.auth(a.deleteFeed))
	mux.HandleFunc("POST /api/v1/feeds/{id}/refresh", a.auth(a.refreshFeed))
	mux.HandleFunc("POST /api/v1/feeds/{id}/rebuild", a.auth(a.rebuildFeed))
	mux.HandleFunc("POST /api/v1/folders", a.auth(a.createFolder))
	mux.HandleFunc("PATCH /api/v1/folders/{id}", a.auth(a.renameFolder))
	mux.HandleFunc("DELETE /api/v1/folders/{id}", a.auth(a.deleteFolder))
	mux.HandleFunc("POST /api/v1/opml", a.auth(a.importOPML))
	mux.HandleFunc("GET /api/v1/opml", a.auth(a.exportOPML))
	mux.HandleFunc("GET /api/v1/events", a.auth(a.events))
	mux.HandleFunc("GET /api/v1/push", a.auth(a.pushStatus))
	mux.HandleFunc("POST /api/v1/push/subscriptions", a.auth(a.pushSubscribe))
	mux.HandleFunc("DELETE /api/v1/push/subscriptions", a.auth(a.pushUnsubscribe))
	mux.HandleFunc("POST /api/v1/push/test", a.auth(a.pushTest))
	return mux
}

// Notify tells a user's open readers that new entries arrived.
func (a *API) Notify(userID int64) { a.hub.publish(userID, event{name: "refresh", data: []byte("{}")}) }

type ctxKey struct{}

func userID(r *http.Request) int64 { return r.Context().Value(ctxKey{}).(int64) }

func (a *API) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Personal API tokens (the corr CLI, scripts, agents): no cookie, so
		// no cross-site request can carry them and no CSRF header is needed.
		if h := r.Header.Get("Authorization"); h != "" {
			token, ok := strings.CutPrefix(h, "Bearer ")
			if !ok {
				fail(w, http.StatusUnauthorized, "Use an Authorization: Bearer token.")
				return
			}
			ip := a.ClientIP.Of(r)
			if !a.Limiter.Allow(ip) {
				fail(w, http.StatusTooManyRequests, "Too many failed attempts. Wait a minute and try again.")
				return
			}
			uid, scope, err := a.Store.TokenUser(r.Context(), strings.TrimSpace(token))
			if err != nil {
				a.Limiter.Fail(ip)
				fail(w, http.StatusUnauthorized, "That token isn't valid: it was revoked, or the account is disabled.")
				return
			}
			if scope != store.ScopeWrite && r.Method != http.MethodGet {
				fail(w, http.StatusForbidden, "This token can only read.")
				return
			}
			ctx := context.WithValue(r.Context(), ctxKey{}, uid)
			next(w, r.WithContext(context.WithValue(ctx, viaTokenKey{}, true)))
			return
		}
		c, err := r.Cookie(cookieName)
		if err != nil {
			fail(w, http.StatusUnauthorized, "Sign in to continue.")
			return
		}
		uid, err := a.Store.SessionUser(r.Context(), c.Value, "web")
		if err != nil {
			fail(w, http.StatusUnauthorized, "Your session has ended. Sign in again.")
			return
		}
		if r.Method != http.MethodGet && r.Header.Get(csrfHeader) != "1" {
			fail(w, http.StatusForbidden, "Missing request header.")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, uid)))
	}
}

type viaTokenKey struct{}

// session wraps auth for changes to the account itself (password,
// passkeys, tokens, AI key, sessions): only a signed-in browser may make
// them, never an API token, so a leaked token can't take over an account.
func (a *API) session(next http.HandlerFunc) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(viaTokenKey{}) != nil {
			fail(w, http.StatusForbidden, "Sign in to the app to change this; API tokens can't.")
			return
		}
		next(w, r)
	})
}

// sessionToken is the browser session's token, "" with an API token.
func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(csrfHeader) != "1" {
		fail(w, http.StatusForbidden, "Missing request header.")
		return
	}
	ip := a.ClientIP.Of(r)
	if !a.Limiter.Allow(ip) {
		fail(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a minute and try again.")
		return
	}
	var in struct{ Username, Password string }
	if !decode(w, r, &in) {
		return
	}
	u, err := a.Store.CheckPassword(r.Context(), in.Username, in.Password, false)
	if errors.Is(err, store.ErrDisabled) {
		fail(w, http.StatusForbidden, "This account is disabled. Ask your admin.")
		return
	}
	if err != nil {
		a.Limiter.Fail(ip)
		fail(w, http.StatusUnauthorized, "That username and password don't match.")
		return
	}
	if !a.startSession(w, r, u.ID) {
		return
	}
	reply(w, map[string]any{"username": u.Username})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		a.Store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.Secure})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	u, err := a.Store.UserByID(r.Context(), userID(r))
	if err != nil {
		a.internal(w, err)
		return
	}
	cfg, _, hasAI, err := a.aiConfig(r.Context(), u.ID)
	if err != nil {
		a.internal(w, err)
		return
	}
	var ai any
	if hasAI {
		ai = cfg
	}
	reply(w, map[string]any{"username": u.Username, "hasApiPassword": u.HasAPI, "prefs": json.RawMessage(u.Prefs),
		"aiAvailable": a.Keyring != nil, "ai": ai, "role": u.Role, "passkeysAvailable": a.WebAuthn != nil,
		// Only signed-in users learn the version, so it isn't advertised publicly.
		"version": version.Version, "commit": version.Commit})
}

func (a *API) putPrefs(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
	if err != nil || !json.Valid(body) {
		fail(w, http.StatusBadRequest, "Preferences must be JSON.")
		return
	}
	if err := a.Store.SetPrefs(r.Context(), userID(r), string(body)); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) putAPIPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string }
	if !decode(w, r, &in) {
		return
	}
	if len(in.Password) < 10 {
		fail(w, http.StatusBadRequest, "Use at least 10 characters.")
		return
	}
	if err := a.Store.SetAPIPassword(r.Context(), userID(r), in.Password); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) tree(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	folders, err := a.Store.Folders(r.Context(), uid)
	if err != nil {
		a.internal(w, err)
		return
	}
	feeds, err := a.Store.Feeds(r.Context(), uid)
	if err != nil {
		a.internal(w, err)
		return
	}
	counts, err := a.Store.Counts(r.Context(), uid)
	if err != nil {
		a.internal(w, err)
		return
	}
	for i := range feeds {
		feeds[i].IconURL = a.image(feeds[i].IconURL, iconWidth)
	}
	reply(w, map[string]any{"folders": folders, "feeds": feeds, "counts": counts})
}

// filterFrom maps reader query parameters to a store filter.
func filterFrom(r *http.Request) store.Filter {
	q := r.URL.Query()
	// The reader lists by publication time; the cursor is "published.id".
	f := store.Filter{UserID: userID(r), Query: q.Get("q"), Oldest: q.Get("order") == "oldest", ByPublished: true}
	f.FeedID, _ = strconv.ParseInt(q.Get("feed"), 10, 64)
	f.FolderID, _ = strconv.ParseInt(q.Get("folder"), 10, 64)
	if pub, id, ok := strings.Cut(q.Get("cursor"), "."); ok {
		f.AfterPub, _ = strconv.ParseInt(pub, 10, 64)
		f.After, _ = strconv.ParseInt(id, 10, 64)
	}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	switch q.Get("view") {
	case "unread":
		f.State = store.StateUnread
	case "starred":
		f.State = store.StateStarred
	case "today":
		f.MinID = time.Now().Truncate(24 * time.Hour).UnixMicro()
	}
	return f
}

func (a *API) entries(w http.ResponseWriter, r *http.Request) {
	f := filterFrom(r)
	// content=1 includes full article HTML, for offline reading; pages are
	// smaller then because each article can be large.
	withContent := r.URL.Query().Get("content") == "1"
	maxLimit := 200
	if withContent {
		maxLimit = 100
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	entries, next, err := a.Store.Entries(r.Context(), f, withContent)
	if err != nil {
		a.internal(w, err)
		return
	}
	var cursor string
	if next != 0 {
		last := entries[len(entries)-1]
		cursor = fmt.Sprintf("%d.%d", last.PublishedAt, last.ID)
	}
	for i := range entries {
		a.proxyEntry(&entries[i])
	}
	reply(w, map[string]any{"entries": entries, "cursor": cursor})
}

func (a *API) entry(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "Invalid entry ID.")
		return
	}
	e, err := a.Store.Entry(r.Context(), userID(r), id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "That article no longer exists.")
		return
	} else if err != nil {
		a.internal(w, err)
		return
	}
	a.proxyEntry(e)
	var encl []store.Enclosure
	json.Unmarshal([]byte(e.Enclosures), &encl)
	reply(w, map[string]any{"entry": e, "enclosures": encl})
}

func (a *API) setState(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	ids := make([]int64, 0, len(in.IDs))
	for _, s := range in.IDs {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			fail(w, http.StatusBadRequest, "Invalid entry ID.")
			return
		}
		ids = append(ids, id)
	}
	if len(ids) > 1000 {
		fail(w, http.StatusBadRequest, "Too many entries in one request.")
		return
	}
	changed, err := a.Store.SetState(r.Context(), userID(r), in.Action, ids)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	reply(w, map[string]any{"changed": len(changed)})
}

func (a *API) markAllRead(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Feed   int64  `json:"feed,string"`
		Folder int64  `json:"folder,string"`
		View   string `json:"view"`
		Before string `json:"before"` // newest entry ID the user saw
	}
	if !decode(w, r, &in) {
		return
	}
	before, err := strconv.ParseInt(in.Before, 10, 64)
	if err != nil || before <= 0 {
		fail(w, http.StatusBadRequest, "Missing boundary.")
		return
	}
	f := store.Filter{UserID: userID(r), FeedID: in.Feed, FolderID: in.Folder}
	if in.View == "today" {
		f.MinID = time.Now().Truncate(24 * time.Hour).UnixMicro()
	}
	changed, err := a.Store.MarkAllRead(r.Context(), f, before)
	if err != nil {
		a.internal(w, err)
		return
	}
	ids := make([]string, len(changed))
	for i, id := range changed {
		ids[i] = strconv.FormatInt(id, 10)
	}
	reply(w, map[string]any{"ids": ids})
}

func (a *API) discover(w http.ResponseWriter, r *http.Request) {
	var in struct{ URL string }
	if !decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	c, err := a.Ingest.Discover(ctx, userID(r), in.URL)
	if err != nil && !errors.Is(err, ingest.ErrNoFeed) {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	a.replyCandidates(w, c)
}

// discoverAI finds a parser for a page with no feed: the server's own if
// it has one that works, else one written by the reader's AI (which costs
// money, so each reader gets a few a day).
func (a *API) discoverAI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string
		// Rebuild asks for a new parser even if the server has one (which
		// read the page poorly); a working built-in one is kept.
		Rebuild bool
	}
	if !decode(w, r, &in) {
		return
	}
	uid := userID(r)
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	c, err := a.Ingest.DiscoverAI(ctx, uid, in.URL, func() (*pagefeed.Suggester, error) { return a.AIForApps(ctx, uid) }, in.Rebuild)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, sentence(err.Error()))
		return
	}
	a.replyCandidates(w, []ingest.Candidate{c})
}

// rebuildFeed writes a new parser for a page feed whose page changed:
// exactly if possible, else with the reader's AI. Every reader of the page
// gets the new parser.
func (a *API) rebuildFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	uid := userID(r)
	f, err := a.Store.Feed(r.Context(), uid, id)
	if err != nil {
		a.result(w, err)
		return
	}
	if !f.Page {
		fail(w, http.StatusBadRequest, "Only feeds built from a page can be rebuilt.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	_, err = a.Ingest.Rebuild(ctx, uid, f.URL, func() (*pagefeed.Suggester, error) { return a.AIForApps(ctx, uid) })
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, sentence(err.Error()))
		return
	}
	if _, err := a.Ingest.SubscribePage(ctx, uid, f.URL, f.Title, f.FolderID); err != nil {
		a.internal(w, err)
		return
	}
	f, _ = a.Store.Feed(r.Context(), uid, id)
	reply(w, map[string]any{"feed": f})
}

// AIPerDay is how many AI page readings each user may ask for per day.
const AIPerDay = 20

func (a *API) aiLimit() *ratelimit.Limiter {
	a.aiOnce.Do(func() { a.ai = ratelimit.New(AIPerDay, 24*time.Hour) })
	return a.ai
}

func (a *API) replyCandidates(w http.ResponseWriter, c []ingest.Candidate) {
	for i := range c {
		c[i].IconURL = a.image(c[i].IconURL, iconWidth)
	}
	if c == nil {
		c = []ingest.Candidate{}
	}
	reply(w, map[string]any{"candidates": c})
}

const aiContext = "user:%d:ai-key"

// aiConfig reads a user's AI setup; ok is false when none is saved.
func (a *API) aiConfig(ctx context.Context, uid int64) (cfg pagefeed.Config, sealed []byte, ok bool, err error) {
	sealed, raw, err := a.Store.AI(ctx, uid)
	if err != nil || sealed == nil || raw == "" {
		return cfg, nil, false, err
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, nil, false, err
	}
	return cfg, sealed, true, nil
}

// suggester returns an AI client with the user's own key.
func (a *API) suggester(ctx context.Context, uid int64) (*pagefeed.Suggester, error) {
	if a.Keyring == nil {
		return nil, pagefeed.ErrAIUnavailable
	}
	cfg, sealed, ok, err := a.aiConfig(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("set up AI in Settings first")
	}
	key, err := a.Keyring.Open(sealed, fmt.Sprintf(aiContext, uid))
	if err != nil {
		return nil, err
	}
	return pagefeed.NewSuggester(cfg, string(key), a.aiTransport(), a.AIOptions...)
}

func (a *API) aiTransport() http.RoundTripper {
	if a.AITransport != nil {
		return a.AITransport
	}
	return http.DefaultTransport
}

// AIForApps gives apps (Reeder adding a site) the reader's AI, within the
// same daily limit as the web app.
func (a *API) AIForApps(ctx context.Context, uid int64) (*pagefeed.Suggester, error) {
	ai, err := a.suggester(ctx, uid)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("ai:%d", uid)
	if !a.aiLimit().Allow(key) {
		return nil, errors.New("the AI's limit for today is reached")
	}
	a.aiLimit().Fail(key)
	return ai, nil
}

// putAI checks a reader's AI setup with the provider and stores it, the
// key sealed. The key is never sent back; only its last four characters
// are shown. Leaving the key empty keeps the saved one.
func (a *API) putAI(w http.ResponseWriter, r *http.Request) {
	var in struct {
		pagefeed.Config
		Key string `json:"key"`
	}
	if !decode(w, r, &in) {
		return
	}
	if a.Keyring == nil {
		fail(w, http.StatusNotImplemented, "AI isn't available on this server.")
		return
	}
	uid := userID(r)
	cfg, err := in.Config.Normalize()
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	}
	key := strings.TrimSpace(in.Key)
	if key == "" {
		old, sealed, ok, err := a.aiConfig(r.Context(), uid)
		if err != nil || !ok {
			fail(w, http.StatusBadRequest, "Enter your API key.")
			return
		}
		plain, err := a.Keyring.Open(sealed, fmt.Sprintf(aiContext, uid))
		if err != nil {
			fail(w, http.StatusBadRequest, sentence(err.Error()))
			return
		}
		key, cfg.KeyHint = string(plain), old.KeyHint
	} else if len(key) < 20 || len(key) > 400 || strings.ContainsAny(key, " \t\r\n") {
		fail(w, http.StatusBadRequest, "That doesn't look like an API key.")
		return
	} else {
		cfg.KeyHint = key[len(key)-4:]
	}
	ai, err := pagefeed.NewSuggester(cfg, key, a.aiTransport(), a.AIOptions...)
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := ai.Verify(ctx); err != nil {
		fail(w, http.StatusUnprocessableEntity, sentence(err.Error()))
		return
	}
	raw, _ := json.Marshal(cfg)
	if err := a.Store.SetAI(r.Context(), uid, a.Keyring.Seal([]byte(key), fmt.Sprintf(aiContext, uid)), string(raw)); err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"ai": cfg})
}

func (a *API) deleteAI(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.SetAI(r.Context(), userID(r), nil, ""); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sentence capitalizes an error and ends it with a full stop.
func sentence(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func (a *API) subscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL        string `json:"url"`
		Title      string `json:"title"`
		FolderID   *int64 `json:"folderId"`
		FolderName string `json:"folderName"`
		// Page follows a web page that has no feed, with the parser that
		// discovery found or wrote for it.
		Page bool `json:"page"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
		fail(w, http.StatusBadRequest, "Enter a full http or https address.")
		return
	}
	uid := userID(r)
	if in.FolderName != "" {
		id, err := a.Store.EnsureFolder(r.Context(), uid, in.FolderName)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		in.FolderID = &id
	}
	var id int64
	var err error
	if in.Page {
		id, err = a.Ingest.SubscribePage(r.Context(), uid, in.URL, in.Title, in.FolderID)
	} else {
		id, err = a.Ingest.Subscribe(r.Context(), uid, in.URL, in.Title, in.FolderID)
	}
	if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"id": id})
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "Invalid ID.")
		return 0, false
	}
	return id, true
}

func (a *API) updateFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Title      *string `json:"title"`
		FolderID   *int64  `json:"folderId"`
		FolderName string  `json:"folderName"`
		NoFolder   bool    `json:"noFolder"`
		Paused     *bool   `json:"paused"`
		Notify     *bool   `json:"notify"`
		URL        *string `json:"url"`
		FullText   *bool   `json:"fullText"`
	}
	if !decode(w, r, &in) {
		return
	}
	uid := userID(r)
	if in.FolderName != "" {
		fid, err := a.Store.EnsureFolder(r.Context(), uid, in.FolderName)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		in.FolderID = &fid
	}
	if in.URL != nil {
		u := strings.TrimSpace(*in.URL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") || len(u) > 2048 {
			fail(w, http.StatusBadRequest, "Enter a full http or https address.")
			return
		}
		switch err := a.Store.SetFeedURL(r.Context(), uid, id, u); {
		case errors.Is(err, store.ErrDuplicate):
			fail(w, http.StatusConflict, "You already subscribe to that address.")
			return
		case err != nil:
			a.result(w, err)
			return
		}
	}
	if in.FullText != nil {
		if err := a.Store.SetFeedFullText(r.Context(), uid, id, *in.FullText); err != nil {
			a.result(w, err)
			return
		}
		if *in.FullText && a.Ingest != nil {
			// Fetch full articles for what's already there, in the background.
			if f, err := a.Store.Feed(r.Context(), uid, id); err == nil {
				go a.Ingest.Backfill(context.WithoutCancel(r.Context()), *f)
			}
		}
	}
	if in.Notify != nil {
		if err := a.Store.SetFeedNotify(r.Context(), uid, id, *in.Notify); err != nil {
			a.result(w, err)
			return
		}
	}
	if in.Paused != nil {
		if err := a.Store.SetFeedSchedule(r.Context(), uid, id, 0, *in.Paused); err != nil {
			a.result(w, err)
			return
		}
	}
	a.result(w, a.Store.UpdateFeed(r.Context(), uid, id, in.Title, in.FolderID, in.NoFolder))
}

func (a *API) deleteFeed(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		a.result(w, a.Store.DeleteFeed(r.Context(), userID(r), id))
	}
}

func (a *API) refreshFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	f, err := a.Store.Feed(r.Context(), userID(r), id)
	if err != nil {
		a.result(w, err)
		return
	}
	a.Ingest.Poll(r.Context(), *f)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) createFolder(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name string }
	if !decode(w, r, &in) {
		return
	}
	id, err := a.Store.EnsureFolder(r.Context(), userID(r), in.Name)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	reply(w, map[string]any{"id": id})
}

func (a *API) renameFolder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct{ Name string }
	if !decode(w, r, &in) {
		return
	}
	a.result(w, a.Store.RenameFolder(r.Context(), userID(r), id, in.Name))
}

func (a *API) deleteFolder(w http.ResponseWriter, r *http.Request) {
	if id, ok := pathID(w, r); ok {
		a.result(w, a.Store.DeleteFolder(r.Context(), userID(r), id))
	}
}

func (a *API) importOPML(w http.ResponseWriter, r *http.Request) {
	subs, err := opml.Parse(r.Body)
	if err != nil {
		fail(w, http.StatusBadRequest, "That file isn't valid OPML.")
		return
	}
	uid := userID(r)
	added := 0
	for _, s := range subs {
		var folder *int64
		if s.Folder != "" {
			id, err := a.Store.EnsureFolder(r.Context(), uid, s.Folder)
			if err != nil {
				continue
			}
			folder = &id
		}
		// New feeds are due immediately; the poller picks them up.
		if _, err := a.Store.AddFeed(r.Context(), uid, s.URL, s.Title, folder); err == nil {
			added++
		}
	}
	reply(w, map[string]any{"added": added})
}

func (a *API) exportOPML(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	folders, err := a.Store.Folders(r.Context(), uid)
	if err != nil {
		a.internal(w, err)
		return
	}
	names := map[int64]string{}
	for _, f := range folders {
		names[f.ID] = f.Name
	}
	feeds, err := a.Store.Feeds(r.Context(), uid)
	if err != nil {
		a.internal(w, err)
		return
	}
	subs := make([]opml.Subscription, 0, len(feeds))
	for _, f := range feeds {
		s := opml.Subscription{Title: f.Title, URL: f.URL, SiteURL: f.SiteURL}
		if f.FolderID != nil {
			s.Folder = names[*f.FolderID]
		}
		subs = append(subs, s)
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="correspondent.opml"`)
	opml.Write(w, "Correspondent subscriptions", subs)
}

// fullArticle fetches the entry's web page and shows the article in place
// of the feed's text.
func (a *API) fullArticle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	uid := userID(r)
	e, err := a.Store.Entry(r.Context(), uid, id)
	if err != nil {
		a.result(w, err)
		return
	}
	if a.Ingest == nil {
		fail(w, http.StatusNotImplemented, "Full articles are not available on this server.")
		return
	}
	if _, err := a.Ingest.FullArticle(r.Context(), uid, *e); err != nil {
		fail(w, http.StatusUnprocessableEntity, "Couldn't get the full article: "+err.Error()+".")
		return
	}
	a.entry(w, r)
}

func (a *API) revertFullArticle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.Store.RevertFullText(r.Context(), userID(r), id); err != nil {
		a.result(w, err)
		return
	}
	a.entry(w, r)
}

// Image widths: icons show at up to 20px and thumbnails at 64px (cropped to
// a square), so both leave room for high-density screens.
const (
	iconWidth  = 64
	thumbWidth = 256
	bodyWidth  = 1600
)

// image returns the proxied address of an image, or raw unchanged when the
// proxy is off or raw isn't a web address.
func (a *API) image(raw string, width int) string {
	if a.Images == nil {
		return raw
	}
	if u := a.Images.URL(raw, width); u != "" {
		return u
	}
	return raw
}

func (a *API) proxyEntry(e *store.Entry) {
	if a.Images == nil {
		return
	}
	e.ImageURL = a.image(e.ImageURL, thumbWidth)
	if e.Content != "" {
		e.Content = a.Images.RewriteHTML(e.Content, bodyWidth)
	}
}

func (a *API) result(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "Not found.")
	case err != nil:
		a.internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) internal(w http.ResponseWriter, err error) {
	a.Log.Error("api", "err", err)
	fail(w, http.StatusInternalServerError, "Something went wrong on the server. Check the logs.")
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "Invalid request body.")
		return false
	}
	return true
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

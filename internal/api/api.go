// Package api implements the modern JSON API consumed by the web reader.
// It is deliberately unrelated to the Google Reader representation, which
// lives in package greader.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arenzana/correspondent/internal/imgproxy"
	"github.com/arenzana/correspondent/internal/ingest"
	"github.com/arenzana/correspondent/internal/opml"
	"github.com/arenzana/correspondent/internal/push"
	"github.com/arenzana/correspondent/internal/ratelimit"
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

	hub hub
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/session", a.login)
	mux.HandleFunc("DELETE /api/v1/session", a.auth(a.logout))
	mux.HandleFunc("GET /api/v1/me", a.auth(a.me))
	mux.HandleFunc("PUT /api/v1/prefs", a.auth(a.putPrefs))
	mux.HandleFunc("PUT /api/v1/api-password", a.auth(a.putAPIPassword))
	mux.HandleFunc("GET /api/v1/tree", a.auth(a.tree))
	mux.HandleFunc("GET /api/v1/entries", a.auth(a.entries))
	mux.HandleFunc("GET /api/v1/entries/{id}", a.auth(a.entry))
	mux.HandleFunc("POST /api/v1/entries/state", a.auth(a.setState))
	mux.HandleFunc("POST /api/v1/entries/{id}/full", a.auth(a.fullArticle))
	mux.HandleFunc("DELETE /api/v1/entries/{id}/full", a.auth(a.revertFullArticle))
	mux.HandleFunc("POST /api/v1/entries/mark-all-read", a.auth(a.markAllRead))
	mux.HandleFunc("POST /api/v1/discover", a.auth(a.discover))
	mux.HandleFunc("POST /api/v1/feeds", a.auth(a.subscribe))
	mux.HandleFunc("PATCH /api/v1/feeds/{id}", a.auth(a.updateFeed))
	mux.HandleFunc("DELETE /api/v1/feeds/{id}", a.auth(a.deleteFeed))
	mux.HandleFunc("POST /api/v1/feeds/{id}/refresh", a.auth(a.refreshFeed))
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
	if err != nil {
		a.Limiter.Fail(ip)
		fail(w, http.StatusUnauthorized, "That username and password don't match.")
		return
	}
	token, err := a.Store.CreateSession(r.Context(), u.ID, "web", sessionTTL)
	if err != nil {
		a.internal(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: a.Secure, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds())})
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
	reply(w, map[string]any{"username": u.Username, "hasApiPassword": u.HasAPI, "prefs": json.RawMessage(u.Prefs),
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	c, err := a.Ingest.Discover(ctx, in.URL)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	for i := range c {
		c[i].IconURL = a.image(c[i].IconURL, iconWidth)
	}
	reply(w, map[string]any{"candidates": c})
}

func (a *API) subscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL        string `json:"url"`
		Title      string `json:"title"`
		FolderID   *int64 `json:"folderId"`
		FolderName string `json:"folderName"`
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
	id, err := a.Ingest.Subscribe(r.Context(), uid, in.URL, in.Title, in.FolderID)
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

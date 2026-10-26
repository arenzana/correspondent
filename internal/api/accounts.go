package api

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arenzana/correspondent/internal/fetch"
	"github.com/arenzana/correspondent/internal/pagefeed"
	"github.com/arenzana/correspondent/internal/store"
	"github.com/arenzana/correspondent/internal/version"
)

const (
	inviteTTL = 7 * 24 * time.Hour
	resetTTL  = 48 * time.Hour
	minPass   = 10
)

var started = time.Now()

// startSession signs a reader in on this device.
func (a *API) startSession(w http.ResponseWriter, r *http.Request, uid int64) bool {
	token, err := a.Store.CreateSession(r.Context(), uid, "web", sessionTTL, clientName(r.UserAgent()))
	if err != nil {
		a.internal(w, err)
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: a.Secure, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds())})
	return true
}

// clientName describes a device from its User-Agent ("Safari on iPhone"),
// for the reader's list of sessions. It is a label, never trusted.
func clientName(ua string) string {
	browser := "Browser"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	device := ""
	switch {
	case strings.Contains(ua, "iPhone"):
		device = "iPhone"
	case strings.Contains(ua, "iPad"):
		device = "iPad"
	case strings.Contains(ua, "Android"):
		device = "Android"
	case strings.Contains(ua, "Macintosh"):
		device = "Mac"
	case strings.Contains(ua, "Windows"):
		device = "Windows"
	case strings.Contains(ua, "Linux"):
		device = "Linux"
	}
	if device == "" {
		return browser
	}
	return browser + " on " + device
}

// admin wraps auth and requires the admin role, checked on every request.
func (a *API) admin(next http.HandlerFunc) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.Store.UserByID(r.Context(), userID(r))
		if err != nil || u.Role != store.RoleAdmin {
			fail(w, http.StatusForbidden, "Only admins can do that.")
			return
		}
		next(w, r)
	})
}

// --- your own account ------------------------------------------------------

func (a *API) sessions(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.Sessions(r.Context(), userID(r), sessionToken(r))
	if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"sessions": orEmpty(list)})
}

func (a *API) endSession(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.Store.EndSession(r.Context(), userID(r), id); err != nil {
		a.result(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Current, New string }
	if !decode(w, r, &in) {
		return
	}
	uid := userID(r)
	key := "password:" + strconv.FormatInt(uid, 10)
	if !a.Limiter.Allow(key) {
		fail(w, http.StatusTooManyRequests, "Too many attempts. Wait a minute and try again.")
		return
	}
	if !a.Store.CheckUserPassword(r.Context(), uid, in.Current) {
		a.Limiter.Fail(key)
		fail(w, http.StatusBadRequest, "Your current password isn't right.")
		return
	}
	if len(in.New) < minPass {
		fail(w, http.StatusBadRequest, "Use at least 10 characters.")
		return
	}
	if err := a.Store.SetPassword(r.Context(), uid, in.New, sessionToken(r)); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- invitations and resets (public) -------------------------------------

func (a *API) invite(w http.ResponseWriter, r *http.Request) {
	inv, err := a.Store.PendingInvite(r.Context(), r.PathValue("token"))
	if err != nil {
		fail(w, http.StatusNotFound, "This link has expired or was already used. Ask for a new one.")
		return
	}
	reply(w, map[string]any{"kind": inv.Kind, "username": inv.Username, "invitedBy": inv.CreatedBy, "expiresAt": inv.ExpiresAt})
}

func (a *API) acceptInvite(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(csrfHeader) != "1" {
		fail(w, http.StatusForbidden, "Missing request header.")
		return
	}
	ip := a.ClientIP.Of(r)
	if !a.Limiter.Allow(ip) {
		fail(w, http.StatusTooManyRequests, "Too many attempts. Wait a minute and try again.")
		return
	}
	var in struct{ Username, Password string }
	if !decode(w, r, &in) {
		return
	}
	if len(in.Password) < minPass {
		fail(w, http.StatusBadRequest, "Use at least 10 characters.")
		return
	}
	uid, err := a.Store.AcceptInvite(r.Context(), r.PathValue("token"), strings.TrimSpace(in.Username), in.Password)
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.Limiter.Fail(ip)
		fail(w, http.StatusNotFound, "This link has expired or was already used. Ask for a new one.")
		return
	case errors.Is(err, store.ErrUsernameTaken):
		fail(w, http.StatusConflict, "That username is taken. Try another.")
		return
	case err != nil && strings.HasPrefix(err.Error(), "usernames"):
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	case err != nil:
		a.internal(w, err)
		return
	}
	if !a.startSession(w, r, uid) {
		return
	}
	u, _ := a.Store.UserByID(r.Context(), uid)
	reply(w, map[string]any{"username": u.Username})
}

// --- admin: accounts -------------------------------------------------------

func (a *API) adminAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := a.Store.Accounts(r.Context())
	if err != nil {
		a.internal(w, err)
		return
	}
	invites, err := a.Store.PendingInvites(r.Context())
	if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"accounts": orEmpty(accounts), "invites": orEmpty(invites)})
}

// createInvite returns the token once; the page turns it into a link.
func (a *API) createInvite(w http.ResponseWriter, r *http.Request) {
	var in struct{ Role, Note string }
	if !decode(w, r, &in) {
		return
	}
	if in.Role == "" {
		in.Role = store.RoleUser
	}
	if len([]rune(in.Note)) > 120 {
		fail(w, http.StatusBadRequest, "Keep the note short.")
		return
	}
	token, err := a.Store.CreateInvite(r.Context(), store.InviteAccount, 0, in.Role, in.Note, userID(r), inviteTTL)
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	}
	reply(w, map[string]any{"token": token, "expiresAt": time.Now().Add(inviteTTL).Unix()})
}

func (a *API) revokeInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.Store.RevokeInvite(r.Context(), id); err != nil {
		a.result(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) updateAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Role     *string
		Disabled *bool
	}
	if !decode(w, r, &in) {
		return
	}
	var err error
	if in.Role != nil {
		err = a.Store.SetRole(r.Context(), id, *in.Role)
	}
	if err == nil && in.Disabled != nil {
		if *in.Disabled && id == userID(r) {
			fail(w, http.StatusBadRequest, "You can't disable your own account.")
			return
		}
		err = a.Store.SetDisabled(r.Context(), id, *in.Disabled)
	}
	a.accountResult(w, err)
}

// resetLink makes a one-time link that lets the person choose a new
// password; the admin never sees or sets it.
func (a *API) resetLink(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := a.Store.UserByID(r.Context(), id); err != nil {
		a.result(w, err)
		return
	}
	token, err := a.Store.CreateInvite(r.Context(), store.InviteReset, id, store.RoleUser, "", userID(r), resetTTL)
	if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"token": token, "expiresAt": time.Now().Add(resetTTL).Unix()})
}

func (a *API) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if id == userID(r) {
		fail(w, http.StatusBadRequest, "You can't delete your own account here.")
		return
	}
	a.accountResult(w, a.Store.DeleteAccount(r.Context(), id))
}

func (a *API) accountResult(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrLastAdmin):
		fail(w, http.StatusConflict, "The server needs at least one active admin.")
	case err != nil:
		a.result(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- admin: server ---------------------------------------------------------

// dirSize is cached briefly: walking the image cache isn't free.
var sizeCache struct {
	sync.Mutex
	at    time.Time
	sizes map[string]int64
}

func dirSize(path string) int64 {
	sizeCache.Lock()
	defer sizeCache.Unlock()
	if time.Since(sizeCache.at) > time.Minute {
		sizeCache.sizes, sizeCache.at = map[string]int64{}, time.Now()
	}
	if n, ok := sizeCache.sizes[path]; ok {
		return n
	}
	var n int64
	filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	sizeCache.sizes[path] = n
	return n
}

func fileSize(paths ...string) int64 {
	var n int64
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			n += info.Size()
		}
	}
	return n
}

func (a *API) adminServer(w http.ResponseWriter, r *http.Request) {
	stats, err := a.Store.Stats(r.Context(), time.Now().Unix())
	if err != nil {
		a.internal(w, err)
		return
	}
	failing, err := a.Store.FailingFeeds(r.Context(), 20)
	if err != nil {
		a.internal(w, err)
		return
	}
	out := map[string]any{
		"version": version.Version, "commit": version.Commit, "go": runtime.Version(),
		"startedAt": started.Unix(), "stats": stats, "failing": orEmpty(failing),
		"passkeys": a.WebAuthn != nil,
	}
	if a.DataDir != "" {
		db := filepath.Join(a.DataDir, "correspondent.db")
		out["databaseBytes"] = fileSize(db, db+"-wal", db+"-shm")
		out["imageCacheBytes"] = dirSize(filepath.Join(a.DataDir, "imgcache"))
		if info, err := os.Stat(filepath.Join(a.DataDir, "backup", "correspondent.db")); err == nil {
			out["backupAt"] = info.ModTime().Unix()
		}
	}
	reply(w, out)
}

// --- admin: shared parsers -------------------------------------------------

func (a *API) adminParsers(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.ParserInfos(r.Context())
	if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"parsers": orEmpty(list)})
}

// testParser runs a parser against its page now.
func (a *API) testParser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := a.Store.Parser(r.Context(), id)
	if err != nil {
		a.result(w, err)
		return
	}
	rules, err := pagefeed.Decode(p.Rules)
	if err != nil {
		reply(w, map[string]any{"ok": false, "error": "The parser's rules are invalid."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	resp, err := a.Ingest.Fetch.Get(ctx, fetch.Request{URL: p.URL})
	if err != nil {
		reply(w, map[string]any{"ok": false, "error": "Couldn't load the page: " + err.Error() + "."})
		return
	}
	parsed, err := pagefeed.Parse(resp.Body, resp.FinalURL, rules)
	if err != nil {
		reply(w, map[string]any{"ok": false, "error": "It no longer finds articles on the page."})
		return
	}
	var sample []string
	for _, it := range parsed.Items[:min(len(parsed.Items), 5)] {
		sample = append(sample, it.Title)
	}
	reply(w, map[string]any{"ok": true, "items": len(parsed.Items), "sample": sample})
}

func (a *API) rebuildParser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := a.Store.Parser(r.Context(), id)
	if err != nil {
		a.result(w, err)
		return
	}
	uid := userID(r)
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	c, err := a.Ingest.Rebuild(ctx, uid, p.URL, func() (*pagefeed.Suggester, error) { return a.AIForApps(ctx, uid) })
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, sentence(err.Error()))
		return
	}
	reply(w, map[string]any{"method": c.Method, "items": c.ItemCount, "sample": c.Sample})
}

func (a *API) deleteParser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := a.Store.DeleteParser(r.Context(), id); {
	case errors.Is(err, store.ErrInUse):
		fail(w, http.StatusConflict, "Feeds still read this page with it. Remove those feeds first.")
	case err != nil:
		a.result(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// --- personal API tokens ---------------------------------------------------

func (a *API) tokens(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.Tokens(r.Context(), userID(r))
	if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"tokens": orEmpty(list)})
}

// createToken returns the token once, from a signed-in browser.
func (a *API) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Scope string }
	if !decode(w, r, &in) {
		return
	}
	token, t, err := a.Store.CreateToken(r.Context(), userID(r), in.Name, in.Scope)
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	}
	reply(w, map[string]any{"token": token, "info": t})
}

func (a *API) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.Store.RevokeToken(r.Context(), userID(r), id); err != nil {
		a.result(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// tokenLogin exchanges a username and password for a token (corr auth
// login), throttled like signing in. Disabled accounts get none.
func (a *API) tokenLogin(w http.ResponseWriter, r *http.Request) {
	ip := a.ClientIP.Of(r)
	if !a.Limiter.Allow(ip) {
		fail(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a minute and try again.")
		return
	}
	var in struct{ Username, Password, Name, Scope string }
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
	if in.Scope == "" {
		in.Scope = store.ScopeWrite
	}
	token, t, err := a.Store.CreateToken(r.Context(), u.ID, in.Name, in.Scope)
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	}
	reply(w, map[string]any{"token": token, "info": t, "username": u.Username})
}

// revokeCurrentToken lets a token revoke itself (corr auth logout).
func (a *API) revokeCurrentToken(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		fail(w, http.StatusBadRequest, "Only an API token can revoke itself.")
		return
	}
	if err := a.Store.RevokeTokenBySecret(r.Context(), strings.TrimSpace(token)); err != nil {
		a.result(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

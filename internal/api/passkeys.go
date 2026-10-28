package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/arenzana/correspondent/internal/store"
)

// NewWebAuthn configures passkeys for the server's public address, such as
// https://rss.example.org. Passkeys are bound to that exact site.
func NewWebAuthn(publicURL string) (*webauthn.WebAuthn, error) {
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Hostname() != "localhost") {
		return nil, errors.New("CORRESPONDENT_PUBLIC_URL must be the https address readers use")
	}
	return webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: "Correspondent",
		RPOrigins:     []string{u.Scheme + "://" + u.Host},
	})
}

// ceremonies hold WebAuthn challenges between the two halves of a
// registration or sign-in, for a few minutes, keyed by a cookie.
type ceremonies struct {
	mu sync.Mutex
	m  map[string]ceremony
}

type ceremony struct {
	data    webauthn.SessionData
	userID  int64 // registration: who is adding the passkey
	expires time.Time
}

const (
	ceremonyCookie = "correspondent_passkey"
	ceremonyTTL    = 5 * time.Minute
)

func (c *ceremonies) put(w http.ResponseWriter, secure bool, x ceremony) {
	buf := make([]byte, 24)
	rand.Read(buf)
	id := base64.RawURLEncoding.EncodeToString(buf)
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]ceremony{}
	}
	now := time.Now()
	for k, v := range c.m {
		if now.After(v.expires) {
			delete(c.m, k)
		}
	}
	x.expires = now.Add(ceremonyTTL)
	c.m[id] = x
	c.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: ceremonyCookie, Value: id, Path: "/api/v1/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(ceremonyTTL.Seconds())})
}

// take returns a ceremony once; it can't be replayed.
func (c *ceremonies) take(r *http.Request) (ceremony, bool) {
	ck, err := r.Cookie(ceremonyCookie)
	if err != nil {
		return ceremony{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	x, ok := c.m[ck.Value]
	delete(c.m, ck.Value)
	return x, ok && time.Now().Before(x.expires)
}

// waUser adapts an account to webauthn.User.
type waUser struct {
	id     int64
	name   string
	handle []byte
	creds  []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return u.handle }
func (u *waUser) WebAuthnName() string                       { return u.name }
func (u *waUser) WebAuthnDisplayName() string                { return u.name }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (a *API) waUser(ctx context.Context, uid int64) (*waUser, error) {
	u, err := a.Store.UserByID(ctx, uid)
	if err != nil {
		return nil, err
	}
	handle, err := a.Store.WebAuthnHandle(ctx, uid)
	if err != nil {
		return nil, err
	}
	keys, err := a.Store.Passkeys(ctx, uid)
	if err != nil {
		return nil, err
	}
	wu := &waUser{id: uid, name: u.Username, handle: handle}
	for _, k := range keys {
		var c webauthn.Credential
		if json.Unmarshal(k.Credential, &c) == nil {
			wu.creds = append(wu.creds, c)
		}
	}
	return wu, nil
}

func (a *API) passkeysOn(w http.ResponseWriter) bool {
	if a.WebAuthn == nil {
		fail(w, http.StatusNotImplemented, "Passkeys aren't set up on this server.")
		return false
	}
	return true
}

// --- registration (signed in) -------------------------------------------

func (a *API) passkeys(w http.ResponseWriter, r *http.Request) {
	keys, err := a.Store.Passkeys(r.Context(), userID(r))
	if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"passkeys": orEmpty(keys)})
}

func (a *API) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !a.passkeysOn(w) {
		return
	}
	uid := userID(r)
	wu, err := a.waUser(r.Context(), uid)
	if err != nil {
		a.internal(w, err)
		return
	}
	var exclude []protocol.CredentialDescriptor
	for _, c := range wu.creds {
		exclude = append(exclude, c.Descriptor())
	}
	options, data, err := a.WebAuthn.BeginRegistration(wu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationPreferred,
		}),
		webauthn.WithExclusions(exclude))
	if err != nil {
		a.internal(w, err)
		return
	}
	a.ceremonies.put(w, a.Secure, ceremony{data: *data, userID: uid})
	reply(w, options)
}

func (a *API) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !a.passkeysOn(w) {
		return
	}
	uid := userID(r)
	x, ok := a.ceremonies.take(r)
	if !ok || x.userID != uid {
		fail(w, http.StatusBadRequest, "That took too long. Try adding the passkey again.")
		return
	}
	wu, err := a.waUser(r.Context(), uid)
	if err != nil {
		a.internal(w, err)
		return
	}
	cred, err := a.WebAuthn.FinishRegistration(wu, x.data, r)
	if err != nil {
		fail(w, http.StatusBadRequest, "Your device's passkey couldn't be verified.")
		return
	}
	raw, _ := json.Marshal(cred)
	name := r.URL.Query().Get("name")
	if name == "" {
		name = clientName(r.UserAgent())
	}
	id, err := a.Store.AddPasskey(r.Context(), uid, cred.ID, raw, name)
	if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"id": id})
}

func (a *API) renamePasskey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct{ Name string }
	if !decode(w, r, &in) {
		return
	}
	if err := a.Store.RenamePasskey(r.Context(), userID(r), id, in.Name); err != nil {
		a.result(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deletePasskey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.Store.DeletePasskey(r.Context(), userID(r), id); err != nil {
		a.result(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- sign-in (signed out) --------------------------------------------------

// authOptions tells the sign-in page which ways to sign in exist.
func (a *API) authOptions(w http.ResponseWriter, r *http.Request) {
	reply(w, map[string]any{"passkeys": a.WebAuthn != nil})
}

// passkeyLoginBegin starts a sign-in with any passkey for this site: the
// device offers the reader's passkeys, no username needed.
func (a *API) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(csrfHeader) != "1" {
		fail(w, http.StatusForbidden, "Missing request header.")
		return
	}
	if !a.passkeysOn(w) {
		return
	}
	if !a.Limiter.Allow(a.ClientIP.Of(r)) {
		fail(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a minute and try again.")
		return
	}
	options, data, err := a.WebAuthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		a.internal(w, err)
		return
	}
	a.ceremonies.put(w, a.Secure, ceremony{data: *data})
	reply(w, options)
}

func (a *API) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(csrfHeader) != "1" {
		fail(w, http.StatusForbidden, "Missing request header.")
		return
	}
	if !a.passkeysOn(w) {
		return
	}
	ip := a.ClientIP.Of(r)
	if !a.Limiter.Attempt(ip) {
		fail(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a minute and try again.")
		return
	}
	x, ok := a.ceremonies.take(r)
	if !ok {
		fail(w, http.StatusBadRequest, "That took too long. Try again.")
		return
	}
	var who *waUser
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := a.Store.UserByWebAuthnHandle(r.Context(), userHandle)
		if err != nil {
			return nil, err
		}
		who, err = a.waUser(r.Context(), u.ID)
		return who, err
	}
	cred, err := a.WebAuthn.FinishDiscoverableLogin(handler, x.data, r)
	if err != nil || who == nil {
		fail(w, http.StatusUnauthorized, "That passkey isn't one this server knows.")
		return
	}
	a.Limiter.Forgive(ip)
	raw, _ := json.Marshal(cred)
	if err := a.Store.UsePasskey(r.Context(), who.id, cred.ID, raw); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.internal(w, err)
		return
	}
	if !a.startSession(w, r, who.id) {
		return
	}
	reply(w, map[string]any{"username": who.name})
}

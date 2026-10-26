package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/arenzana/correspondent/internal/push"
)

// pushStatus returns the key a browser needs to subscribe, and whether the
// device identified by ?endpoint= is registered.
func (a *API) pushStatus(w http.ResponseWriter, r *http.Request) {
	if a.Push == nil {
		fail(w, http.StatusNotImplemented, "Notifications are not available on this server.")
		return
	}
	key, err := a.Push.PublicKey(r.Context())
	if err != nil {
		a.internal(w, err)
		return
	}
	subscribed := false
	if ep := r.URL.Query().Get("endpoint"); ep != "" {
		subscribed, _ = a.Store.HasPushSubscription(r.Context(), userID(r), ep)
	}
	reply(w, map[string]any{"publicKey": key, "subscribed": subscribed})
}

func (a *API) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	if a.Push == nil {
		fail(w, http.StatusNotImplemented, "Notifications are not available on this server.")
		return
	}
	var in struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !push.AllowedEndpoint(in.Endpoint) || len(in.Endpoint) > 2048 {
		fail(w, http.StatusBadRequest, "That isn't a push service this server sends to.")
		return
	}
	// A P-256 public key is 65 bytes; the auth secret 16.
	if p, err := base64.RawURLEncoding.DecodeString(in.Keys.P256dh); err != nil || len(p) != 65 {
		fail(w, http.StatusBadRequest, "Invalid subscription key.")
		return
	}
	if p, err := base64.RawURLEncoding.DecodeString(in.Keys.Auth); err != nil || len(p) != 16 {
		fail(w, http.StatusBadRequest, "Invalid subscription secret.")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		a.Push.SetSubject(r.Context(), origin)
	}
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	if err := a.Store.SavePushSubscription(r.Context(), userID(r), in.Endpoint, in.Keys.P256dh, in.Keys.Auth, ua); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if !decode(w, r, &in) {
		return
	}
	a.result(w, a.Store.DeletePushSubscription(r.Context(), userID(r), in.Endpoint))
}

// pushTest sends a notification to the user's devices, to check the setup.
func (a *API) pushTest(w http.ResponseWriter, r *http.Request) {
	if a.Push == nil {
		fail(w, http.StatusNotImplemented, "Notifications are not available on this server.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	n, err := a.Push.Send(ctx, userID(r), push.Payload{
		Title: "Correspondent", Body: "Notifications are working on this device.", URL: "/", Tag: "test",
		Icon: "/icons/wire/icon-192.png",
	})
	if err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"sent": n})
}

// Package push sends Web Push notifications for new articles in feeds the
// user opted in to, to the devices that turned notifications on.
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/arenzana/correspondent/internal/store"
)

const (
	settingPublicKey  = "push.vapid_public"
	settingPrivateKey = "push.vapid_private"
	settingSubject    = "push.subject"
)

// pushServices are the hosts browsers hand out subscription endpoints on.
// Anything else is refused: an endpoint is a URL the server will POST to, so
// accepting arbitrary ones would let a client aim the server at any address.
var pushServices = []string{
	"fcm.googleapis.com",        // Chrome, Edge (Android), Brave, …
	"push.services.mozilla.com", // Firefox (updates.push.services.mozilla.com)
	"web.push.apple.com",        // Safari, iOS home-screen apps
	"notify.windows.com",        // Edge on Windows (*.notify.windows.com)
	"push.apple.com",            // older Safari endpoints
}

// AllowedEndpoint reports whether endpoint is an HTTPS URL on a known push service.
func AllowedEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, s := range pushServices {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// Payload is what the service worker shows.
type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"` // in-app link to open on click
	Tag   string `json:"tag"` // notifications with the same tag replace each other
	Icon  string `json:"icon"`
}

type Notifier struct {
	Store *store.Store
	Log   *slog.Logger
	HTTP  *http.Client
	// Allowed overrides AllowedEndpoint (tests only).
	Allowed func(string) bool
}

func (n *Notifier) allowed(endpoint string) bool {
	if n.Allowed != nil {
		return n.Allowed(endpoint)
	}
	return AllowedEndpoint(endpoint)
}

// PublicKey returns the VAPID public key, generating the key pair on first use.
func (n *Notifier) PublicKey(ctx context.Context) (string, error) {
	pub, _, err := n.keys(ctx)
	return pub, err
}

func (n *Notifier) keys(ctx context.Context) (pub, priv string, err error) {
	pub, err1 := n.Store.Setting(ctx, settingPublicKey)
	priv, err2 := n.Store.Setting(ctx, settingPrivateKey)
	if err1 == nil && err2 == nil {
		return pub, priv, nil
	}
	if !errors.Is(err1, store.ErrNotFound) && err1 != nil {
		return "", "", err1
	}
	priv, pub, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", err
	}
	if err := n.Store.SetSetting(ctx, settingPrivateKey, priv); err != nil {
		return "", "", err
	}
	return pub, priv, n.Store.SetSetting(ctx, settingPublicKey, pub)
}

// SetSubject records the contact URL push services may use (the site's own
// origin), from the first subscription request.
func (n *Notifier) SetSubject(ctx context.Context, origin string) error {
	if _, err := n.Store.Setting(ctx, settingSubject); err == nil {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("invalid origin %q", origin)
	}
	return n.Store.SetSetting(ctx, settingSubject, u.Scheme+"://"+u.Host)
}

// Send delivers a notification to every device the user registered. Gone
// subscriptions are removed; it returns how many devices accepted it.
func (n *Notifier) Send(ctx context.Context, userID int64, p Payload) (int, error) {
	subs, err := n.Store.PushSubscriptions(ctx, userID)
	if err != nil || len(subs) == 0 {
		return 0, err
	}
	pub, priv, err := n.keys(ctx)
	if err != nil {
		return 0, err
	}
	subject, err := n.Store.Setting(ctx, settingSubject)
	if err != nil {
		subject = "https://localhost"
	}
	body, _ := json.Marshal(p)
	client := n.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	sent := 0
	for _, s := range subs {
		if !n.allowed(s.Endpoint) {
			n.Store.ForgetPushEndpoint(ctx, s.Endpoint)
			continue
		}
		resp, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{
			Endpoint: s.Endpoint,
			Keys:     webpush.Keys{P256dh: s.P256dh, Auth: s.Auth},
		}, &webpush.Options{
			HTTPClient:      client,
			Subscriber:      subject,
			VAPIDPublicKey:  pub,
			VAPIDPrivateKey: priv,
			TTL:             24 * 3600,
			Topic:           topic(p.Tag),
			Urgency:         webpush.UrgencyNormal,
		})
		if err != nil {
			n.Log.Warn("push", "err", err)
			n.Store.PushFailed(ctx, s.Endpoint)
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			n.Store.ForgetPushEndpoint(ctx, s.Endpoint) // the device unsubscribed
		case resp.StatusCode >= 400:
			n.Log.Warn("push", "status", resp.StatusCode)
			n.Store.PushFailed(ctx, s.Endpoint)
		default:
			sent++
		}
	}
	return sent, nil
}

// topic lets push services replace an undelivered notification with a newer
// one for the same feed (RFC 8030: up to 32 URL-safe characters).
func topic(tag string) string {
	var b strings.Builder
	for _, r := range tag {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	return s[:min(len(s), 32)]
}

// NewArticles notifies about unread articles that arrived in a feed since
// the given crawl ID, if the feed is opted in. Several at once become one
// notification; the per-feed tag makes it replace the previous one.
func (n *Notifier) NewArticles(ctx context.Context, f store.Feed, since int64) {
	if !f.Notify {
		return
	}
	entries, err := n.Store.NewUnread(ctx, f.ID, since, 20)
	if err != nil || len(entries) == 0 {
		return
	}
	p := BuildPayload(f, entries, n.iconFor(ctx, f.UserID))
	if _, err := n.Send(ctx, f.UserID, p); err != nil {
		n.Log.Warn("push", "feed", f.ID, "err", err)
	}
}

// BuildPayload words the notification for a batch of new articles.
func BuildPayload(f store.Feed, entries []store.Entry, icon string) Payload {
	feed := strconv.FormatInt(f.ID, 10)
	p := Payload{Title: f.Title, Tag: "feed-" + feed, Icon: icon}
	if len(entries) == 1 {
		p.Body = entries[0].Title
		p.URL = "/#/feed/" + feed + "/e/" + strconv.FormatInt(entries[0].ID, 10)
	} else {
		p.Body = fmt.Sprintf("%d new articles, the latest: %s", len(entries), entries[0].Title)
		p.URL = "/#/feed/" + feed
	}
	if p.Title == "" {
		p.Title = "Correspondent"
	}
	return p
}

func (n *Notifier) iconFor(ctx context.Context, userID int64) string {
	icon := "wire"
	if u, err := n.Store.UserByID(ctx, userID); err == nil {
		var prefs struct {
			AppIcon string `json:"appIcon"`
		}
		if json.Unmarshal([]byte(u.Prefs), &prefs) == nil && (prefs.AppIcon == "byline" || prefs.AppIcon == "morse") {
			icon = prefs.AppIcon
		}
	}
	return "/icons/" + icon + "/icon-192.png"
}

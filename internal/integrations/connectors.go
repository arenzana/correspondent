package integrations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// --- webhook --------------------------------------------------------------

// webhook POSTs each article as JSON. The body is signed so the receiver
// can tell it came from this server and is recent:
//
//	Correspondent-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">
//
// with the integration's signing key.
type webhook struct {
	*client
	url, key string
}

// Event is the webhook's body.
type Event struct {
	Event   string   `json:"event"` // "article" or "ping"
	SentAt  int64    `json:"sentAt"`
	Article *Article `json:"article,omitempty"`
}

func (w *webhook) post(ctx context.Context, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	h := http.Header{"Content-Type": {"application/json"}}
	h.Set("Correspondent-Signature", Sign(w.key, body, time.Unix(ev.SentAt, 0)))
	h.Set("Correspondent-Event", ev.Event)
	_, err = w.do(ctx, http.MethodPost, w.url, h, strings.NewReader(string(body)), nil)
	return err
}

// Sign is the webhook's signature header for a body sent at t.
func Sign(key string, body []byte, t time.Time) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

func (w *webhook) Verify(ctx context.Context) error {
	return w.post(ctx, Event{Event: "ping", SentAt: time.Now().Unix()})
}

func (w *webhook) Send(ctx context.Context, a Article) (string, error) {
	return "", w.post(ctx, Event{Event: "article", SentAt: time.Now().Unix(), Article: &a})
}

// --- Readwise Reader (readwise.io/reader_api) ------------------------------

type readwise struct {
	*client
	token string
	tags  []string
}

func (r *readwise) header() http.Header { return http.Header{"Authorization": {"Token " + r.token}} }

func (r *readwise) Verify(ctx context.Context) error {
	_, err := r.json(ctx, http.MethodGet, "https://readwise.io/api/v2/auth/", r.header(), nil, nil)
	return err
}

func (r *readwise) Send(ctx context.Context, a Article) (string, error) {
	in := map[string]any{"url": a.URL, "title": a.Title, "saved_using": "Correspondent", "category": "article", "location": "new"}
	if a.HTML != "" {
		in["html"] = a.HTML
		in["should_clean_html"] = true
	}
	if a.Author != "" {
		in["author"] = a.Author
	}
	if a.Summary != "" {
		in["summary"] = a.Summary
	}
	if !a.PublishedAt.IsZero() {
		in["published_date"] = a.PublishedAt.UTC().Format(time.RFC3339)
	}
	if len(r.tags) > 0 {
		in["tags"] = r.tags
	}
	var out struct{ URL string }
	_, err := r.json(ctx, http.MethodPost, "https://readwise.io/api/v3/save/", r.header(), in, &out)
	return out.URL, err
}

// --- Instapaper (the simple API: instapaper.com/api/simple) ----------------

type instapaper struct {
	*client
	username, password string
}

func (i *instapaper) form(ctx context.Context, path string, v url.Values) error {
	h := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	req := strings.NewReader(v.Encode())
	hr, _ := http.NewRequest(http.MethodPost, "https://www.instapaper.com", nil)
	hr.SetBasicAuth(i.username, i.password)
	h.Set("Authorization", hr.Header.Get("Authorization"))
	_, err := i.do(ctx, http.MethodPost, "https://www.instapaper.com"+path, h, req, nil)
	// Instapaper answers 400 for a bad request and for its rate limit
	// alike: worth trying again later.
	if e, ok := err.(*Error); ok && e.Status == http.StatusBadRequest {
		e.Retry = true
	}
	return err
}

func (i *instapaper) Verify(ctx context.Context) error {
	return i.form(ctx, "/api/authenticate", url.Values{})
}

func (i *instapaper) Send(ctx context.Context, a Article) (string, error) {
	v := url.Values{"url": {a.URL}, "title": {a.Title}}
	if a.Summary != "" {
		v.Set("selection", a.Summary)
	}
	return "", i.form(ctx, "/api/add", v)
}

// --- Raindrop.io (developer.raindrop.io) -----------------------------------

type raindrop struct {
	*client
	token string
	tags  []string
}

func (r *raindrop) Verify(ctx context.Context) error {
	_, err := r.json(ctx, http.MethodGet, "https://api.raindrop.io/rest/v1/user", bearer(r.token), nil, nil)
	return err
}

func (r *raindrop) Send(ctx context.Context, a Article) (string, error) {
	in := map[string]any{"link": a.URL, "title": a.Title, "pleaseParse": map[string]any{}}
	if a.Summary != "" {
		in["excerpt"] = a.Summary
	}
	if len(r.tags) > 0 {
		in["tags"] = r.tags
	}
	var out struct{ Result bool }
	if _, err := r.json(ctx, http.MethodPost, "https://api.raindrop.io/rest/v1/raindrop", bearer(r.token), in, &out); err != nil {
		return "", err
	}
	if !out.Result {
		return "", &Error{Message: "Raindrop didn't save the article"}
	}
	return "", nil
}

// --- Wallabag (doc.wallabag.org/developer/api) ------------------------------

// wallabag asks for a token (an hour's) with the reader's client and
// password before each send: articles are sent one at a time, now and
// then, so there's nothing to gain from keeping one.
type wallabag struct {
	*client
	base   string
	secret Secret
	tags   []string
}

func (w *wallabag) token(ctx context.Context) (string, error) {
	v := url.Values{"grant_type": {"password"}, "client_id": {w.secret.ClientID}, "client_secret": {w.secret.ClientSecret},
		"username": {w.secret.Username}, "password": {w.secret.Password}}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	h := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}, "Accept": {"application/json"}}
	_, err := w.do(ctx, http.MethodPost, w.base+"/oauth/v2/token", h, strings.NewReader(v.Encode()), &out)
	// A wrong password or client is a 400 (invalid_grant, invalid_client).
	if e, ok := err.(*Error); ok && e.Status == http.StatusBadRequest {
		return "", ErrRejected
	}
	if err == nil && out.AccessToken == "" {
		err = &Error{Message: "Wallabag gave no token"}
	}
	return out.AccessToken, err
}

func (w *wallabag) Verify(ctx context.Context) error {
	_, err := w.token(ctx)
	return err
}

func (w *wallabag) Send(ctx context.Context, a Article) (string, error) {
	tok, err := w.token(ctx)
	if err != nil {
		return "", err
	}
	in := map[string]any{"url": a.URL, "title": a.Title}
	if a.HTML != "" {
		in["content"] = a.HTML // saves Wallabag fetching the page again
	}
	if a.Author != "" {
		in["authors"] = a.Author
	}
	if !a.PublishedAt.IsZero() {
		in["published_at"] = a.PublishedAt.UTC().Format(time.RFC3339)
	}
	if len(w.tags) > 0 {
		in["tags"] = strings.Join(w.tags, ",")
	}
	var out struct{ ID int64 }
	if _, err := w.json(ctx, http.MethodPost, w.base+"/api/entries.json", bearer(tok), in, &out); err != nil {
		return "", err
	}
	if out.ID == 0 {
		return "", nil
	}
	return fmt.Sprintf("%s/view/%d", w.base, out.ID), nil
}

// --- Linkding (linkding.link/api) ------------------------------------------

type linkding struct {
	*client
	base, token string
	tags        []string
}

func (l *linkding) header() http.Header { return http.Header{"Authorization": {"Token " + l.token}} }

func (l *linkding) Verify(ctx context.Context) error {
	_, err := l.json(ctx, http.MethodGet, l.base+"/api/user/profile/", l.header(), nil, nil)
	return err
}

func (l *linkding) Send(ctx context.Context, a Article) (string, error) {
	in := map[string]any{"url": a.URL, "title": a.Title, "unread": true, "tag_names": l.tags}
	if l.tags == nil {
		in["tag_names"] = []string{}
	}
	if a.Summary != "" {
		in["description"] = a.Summary
	}
	_, err := l.json(ctx, http.MethodPost, l.base+"/api/bookmarks/", l.header(), in, nil)
	return "", err
}

// --- Karakeep, formerly Hoarder (its OpenAPI spec) -------------------------

type karakeep struct {
	*client
	base, token string
	tags        []string
}

func (k *karakeep) Verify(ctx context.Context) error {
	_, err := k.json(ctx, http.MethodGet, k.base+"/api/v1/users/me", bearer(k.token), nil, nil)
	return err
}

func (k *karakeep) Send(ctx context.Context, a Article) (string, error) {
	in := map[string]any{"type": "link", "url": a.URL, "source": "api"}
	if t := []rune(a.Title); len(t) > 0 {
		in["title"] = string(t[:min(len(t), 1000)])
	}
	var out struct{ ID string }
	if _, err := k.json(ctx, http.MethodPost, k.base+"/api/v1/bookmarks", bearer(k.token), in, &out); err != nil {
		return "", err
	}
	// Tags go on in a second call; the bookmark is saved either way.
	if len(k.tags) > 0 && out.ID != "" {
		tags := make([]map[string]string, len(k.tags))
		for i, t := range k.tags {
			tags[i] = map[string]string{"tagName": t}
		}
		_, _ = k.json(ctx, http.MethodPost, k.base+"/api/v1/bookmarks/"+url.PathEscape(out.ID)+"/tags", bearer(k.token), map[string]any{"tags": tags}, nil)
	}
	return "", nil
}

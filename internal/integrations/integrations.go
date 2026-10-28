// Package integrations sends articles to services a reader connects: a
// webhook of their own, read-later services (Readwise Reader, Instapaper,
// Raindrop) and self-hosted ones (Wallabag, Linkding, Karakeep). Each
// connector checks its credentials without saving anything and sends one
// article; the worker queues, retries and records the result.
package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Article is what's sent: the same for every service, each taking what
// it can use.
type Article struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Author      string    `json:"author,omitempty"`
	Feed        string    `json:"feed,omitempty"`
	Summary     string    `json:"summary,omitempty"` // an AI summary, or the feed's excerpt
	HTML        string    `json:"html,omitempty"`    // sanitized article HTML, original image addresses
	Markdown    string    `json:"markdown,omitempty"`
	PublishedAt time.Time `json:"publishedAt"`
	Starred     bool      `json:"starred"`
}

// Kinds of integration.
const (
	Webhook    = "webhook"
	Readwise   = "readwise"
	Instapaper = "instapaper"
	Raindrop   = "raindrop"
	Wallabag   = "wallabag"
	Linkding   = "linkding"
	Karakeep   = "karakeep"
)

// Kind describes a kind for the settings form: what to ask for.
type Kind struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	SelfHosted bool     `json:"selfHosted"` // asks for the instance's address
	Secrets    []string `json:"secrets"`    // credential fields the form asks for
	Tags       bool     `json:"tags"`       // can tag what it saves
	Help       string   `json:"help"`
}

// Kinds lists every kind, in the order the settings offer them.
var Kinds = []Kind{
	{ID: Readwise, Label: "Readwise Reader", Secrets: []string{"token"}, Tags: true,
		Help: "Your access token, from readwise.io/access_token."},
	{ID: Instapaper, Label: "Instapaper", Secrets: []string{"username", "password"},
		Help: "Your Instapaper email or username, and password (leave it empty if your account has none)."},
	{ID: Raindrop, Label: "Raindrop.io", Secrets: []string{"token"}, Tags: true,
		Help: "A test token: in Raindrop's Settings → Integrations, create an app and copy its test token."},
	{ID: Wallabag, Label: "Wallabag", SelfHosted: true, Secrets: []string{"clientId", "clientSecret", "username", "password"}, Tags: true,
		Help: "In Wallabag, Developer → Create a new client gives the client ID and secret; then your username and password."},
	{ID: Linkding, Label: "Linkding", SelfHosted: true, Secrets: []string{"token"}, Tags: true,
		Help: "The REST API token from Linkding's Settings → Integrations."},
	{ID: Karakeep, Label: "Karakeep", SelfHosted: true, Secrets: []string{"token"}, Tags: true,
		Help: "An API key from Karakeep's Settings → API Keys."},
	{ID: Webhook, Label: "Webhook", SelfHosted: true,
		Help: "Each article is POSTed as JSON, signed with a key shown once when you add it."},
}

// KindByID finds a kind.
func KindByID(id string) (Kind, bool) {
	for _, k := range Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// Config is an integration's settings that aren't secret.
type Config struct {
	BaseURL string   `json:"baseUrl,omitempty"` // a self-hosted instance, or the webhook's address
	Tags    []string `json:"tags,omitempty"`
	KeyHint string   `json:"keyHint,omitempty"` // last characters of the token, to recognise it
}

// Secret is an integration's credentials, stored sealed.
type Secret struct {
	Token        string `json:"token,omitempty"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	SigningKey   string `json:"signingKey,omitempty"` // the webhook's, made by the server
}

// Normalize checks the settings for a kind. plainHTTP allows http://
// addresses, for an admin who let integrations reach their own network.
func (c Config) Normalize(kind string, plainHTTP bool) (Config, error) {
	k, ok := KindByID(kind)
	if !ok {
		return c, errors.New("unknown kind of integration")
	}
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if k.SelfHosted {
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Host == "" || u.User != nil || len(c.BaseURL) > 500 || (u.Scheme != "https" && !(plainHTTP && u.Scheme == "http")) {
			if plainHTTP {
				return c, errors.New("the address must be a full http:// or https:// URL")
			}
			return c, errors.New("the address must be a full https:// URL")
		}
	} else {
		c.BaseURL = ""
	}
	var tags []string
	for _, t := range c.Tags {
		if t = strings.TrimSpace(t); t != "" && len(t) <= 60 && len(tags) < 10 {
			tags = append(tags, t)
		}
	}
	c.Tags = tags
	if !k.Tags {
		c.Tags = nil
	}
	return c, nil
}

// Check makes sure the credentials a kind needs are there.
func (s Secret) Check(kind string) error {
	k, _ := KindByID(kind)
	for _, f := range k.Secrets {
		var v string
		switch f {
		case "token":
			v = s.Token
		case "username":
			v = s.Username
		case "clientId":
			v = s.ClientID
		case "clientSecret":
			v = s.ClientSecret
		case "password":
			continue // Instapaper accounts may have none
		}
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("enter the %s", map[string]string{"token": "token", "username": "username", "clientId": "client ID", "clientSecret": "client secret"}[f])
		}
		if len(v) > 500 {
			return errors.New("that credential is too long")
		}
	}
	return nil
}

// Connector is one service.
type Connector interface {
	// Verify checks the credentials without saving anything (the webhook
	// is sent a ping).
	Verify(ctx context.Context) error
	// Send saves one article, returning its address at the service if any.
	Send(ctx context.Context, a Article) (remote string, err error)
}

// New returns the connector for a kind.
func New(kind string, c Config, s Secret, hc *http.Client) (Connector, error) {
	h := &client{http: hc}
	switch kind {
	case Webhook:
		return &webhook{client: h, url: c.BaseURL, key: s.SigningKey}, nil
	case Readwise:
		return &readwise{client: h, token: s.Token, tags: c.Tags}, nil
	case Instapaper:
		return &instapaper{client: h, username: s.Username, password: s.Password}, nil
	case Raindrop:
		return &raindrop{client: h, token: s.Token, tags: c.Tags}, nil
	case Wallabag:
		return &wallabag{client: h, base: c.BaseURL, secret: s, tags: c.Tags}, nil
	case Linkding:
		return &linkding{client: h, base: c.BaseURL, token: s.Token, tags: c.Tags}, nil
	case Karakeep:
		return &karakeep{client: h, base: c.BaseURL, token: s.Token, tags: c.Tags}, nil
	}
	return nil, errors.New("unknown kind of integration")
}

// ErrRejected is a service refusing the credentials: retrying won't help
// until the reader changes them.
var ErrRejected = errors.New("the service rejected the credentials; check them in Settings")

// Error is a failed request: Retry says whether trying again later may
// work (rate limits, outages), RetryAfter when the service said how long.
type Error struct {
	Status     int
	Message    string
	Retry      bool
	RetryAfter time.Duration
}

func (e *Error) Error() string { return e.Message }

// Retryable reports whether a failed send may work later, and when the
// service asked to wait.
func Retryable(err error) (bool, time.Duration) {
	var e *Error
	if errors.As(err, &e) {
		return e.Retry, e.RetryAfter
	}
	return !errors.Is(err, ErrRejected), 0 // timeouts, refused connections
}

type client struct{ http *http.Client }

// do sends a request and decodes a JSON reply into out (when not nil),
// turning statuses into errors the worker can act on.
func (c *client) do(ctx context.Context, method, target string, header http.Header, body io.Reader, out any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("User-Agent", "Correspondent (+https://github.com/arenzana/correspondent)")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return resp, ErrRejected
	case resp.StatusCode == http.StatusTooManyRequests:
		e := &Error{Status: resp.StatusCode, Message: "the service is limiting requests; trying again later", Retry: true}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			e.RetryAfter = time.Duration(s) * time.Second
		}
		return resp, e
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
		return resp, &Error{Status: resp.StatusCode, Message: fmt.Sprintf("the service had a problem (%d); trying again later", resp.StatusCode), Retry: true}
	case resp.StatusCode >= 400:
		return resp, &Error{Status: resp.StatusCode, Message: fmt.Sprintf("the service refused the article (%d): %s", resp.StatusCode, brief(data))}
	case resp.StatusCode >= 300:
		return resp, &Error{Status: resp.StatusCode, Message: fmt.Sprintf("the service answered with a redirect (%d); check the address", resp.StatusCode)}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp, &Error{Status: resp.StatusCode, Message: "the service's reply wasn't understood"}
		}
	}
	return resp, nil
}

func (c *client) json(ctx context.Context, method, target string, header http.Header, in, out any) (*http.Response, error) {
	if header == nil {
		header = http.Header{}
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
		header.Set("Content-Type", "application/json")
	}
	header.Set("Accept", "application/json")
	return c.do(ctx, method, target, header, body, out)
}

// brief is the start of an error reply, for the reader to see.
func brief(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	if s == "" {
		return "no details"
	}
	return s
}

func bearer(token string) http.Header {
	return http.Header{"Authorization": {"Bearer " + token}}
}

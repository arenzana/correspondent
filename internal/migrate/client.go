package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ReaderClient speaks the Google Reader API, as Reeder does. It is used to
// compare FreshRSS with Correspondent and to replay state back on rollback.
type ReaderClient struct {
	Base string // e.g. https://rss.example.org/api/greader.php
	HTTP *http.Client
	auth string
	tok  string
}

func NewReaderClient(base string) *ReaderClient {
	return &ReaderClient{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// Login authenticates with ClientLogin and fetches the write token.
func (c *ReaderClient) Login(ctx context.Context, user, password string) error {
	form := url.Values{"Email": {user}, "Passwd": {password}}
	body, err := c.do(ctx, http.MethodPost, "/accounts/ClientLogin", form, false)
	if err != nil {
		return fmt.Errorf("sign in: %w", err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if v, ok := strings.CutPrefix(line, "Auth="); ok {
			c.auth = strings.TrimSpace(v)
		}
	}
	if c.auth == "" {
		return fmt.Errorf("sign in: no Auth in response")
	}
	tok, err := c.do(ctx, http.MethodGet, "/reader/api/0/token", nil, false)
	if err != nil {
		return err
	}
	c.tok = strings.TrimSpace(string(tok))
	return nil
}

// UseAuth authenticates with an existing token instead of a password, which
// is how Reeder continues after a backend switch.
func (c *ReaderClient) UseAuth(auth string) { c.auth = auth }

func (c *ReaderClient) do(ctx context.Context, method, path string, form url.Values, write bool) ([]byte, error) {
	var body io.Reader
	u := c.Base + path
	if form == nil {
		form = url.Values{}
	}
	if write {
		form.Set("T", c.tok)
	}
	if method == http.MethodPost {
		body = strings.NewReader(form.Encode())
	} else if len(form) > 0 {
		u += "?" + form.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.auth != "" {
		req.Header.Set("Authorization", "GoogleLogin auth="+c.auth)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return b, nil
}

func (c *ReaderClient) getJSON(ctx context.Context, path string, q url.Values, v any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("output", "json")
	b, err := c.do(ctx, http.MethodGet, path, q, false)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

type Subscription struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Categories []struct {
		ID string `json:"id"`
	} `json:"categories"`
}

func (c *ReaderClient) Subscriptions(ctx context.Context) ([]Subscription, error) {
	var out struct{ Subscriptions []Subscription }
	return out.Subscriptions, c.getJSON(ctx, "/reader/api/0/subscription/list", nil, &out)
}

func (c *ReaderClient) Tags(ctx context.Context) ([]string, error) {
	var out struct{ Tags []struct{ ID string } }
	if err := c.getJSON(ctx, "/reader/api/0/tag/list", nil, &out); err != nil {
		return nil, err
	}
	ids := make([]string, len(out.Tags))
	for i, t := range out.Tags {
		ids[i] = t.ID
	}
	return ids, nil
}

type UnreadCount struct {
	ID     string `json:"id"`
	Count  int    `json:"count"`
	Newest string `json:"newestItemTimestampUsec"`
}

func (c *ReaderClient) UnreadCounts(ctx context.Context) ([]UnreadCount, error) {
	var out struct{ Unreadcounts []UnreadCount }
	return out.Unreadcounts, c.getJSON(ctx, "/reader/api/0/unread-count", nil, &out)
}

// IDs returns every item ID in a stream, following continuations, up to limit.
func (c *ReaderClient) IDs(ctx context.Context, stream, exclude string, limit int) ([]string, error) {
	var all []string
	cont := ""
	for {
		q := url.Values{"s": {stream}, "n": {"10000"}}
		if exclude != "" {
			q.Set("xt", exclude)
		}
		if cont != "" {
			q.Set("c", cont)
		}
		var page struct {
			ItemRefs     []struct{ ID string }
			Continuation string
		}
		if err := c.getJSON(ctx, "/reader/api/0/stream/items/ids", q, &page); err != nil {
			return nil, err
		}
		for _, r := range page.ItemRefs {
			all = append(all, r.ID)
		}
		if page.Continuation == "" || (limit > 0 && len(all) >= limit) {
			if limit > 0 && len(all) > limit {
				all = all[:limit]
			}
			return all, nil
		}
		cont = page.Continuation
	}
}

type Item struct {
	ID            string   `json:"id"`
	CrawlTimeMsec string   `json:"crawlTimeMsec"`
	TimestampUsec string   `json:"timestampUsec"`
	Published     int64    `json:"published"`
	Title         string   `json:"title"`
	Categories    []string `json:"categories"`
	Canonical     []struct {
		Href string `json:"href"`
	} `json:"canonical"`
	Origin struct {
		StreamID string `json:"streamId"`
	} `json:"origin"`
}

// Items fetches full items by ID, using repeated i fields as Reeder does.
func (c *ReaderClient) Items(ctx context.Context, ids []string) ([]Item, error) {
	var out []Item
	for start := 0; start < len(ids); start += 250 {
		form := url.Values{"i": ids[start:min(start+250, len(ids))]}
		b, err := c.do(ctx, http.MethodPost, "/reader/api/0/stream/items/contents?output=json", form, false)
		if err != nil {
			return nil, err
		}
		var page struct{ Items []Item }
		if err := json.Unmarshal(b, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
	}
	return out, nil
}

// EditTag adds or removes a state tag on items, in batches.
func (c *ReaderClient) EditTag(ctx context.Context, ids []string, add, remove string) error {
	for start := 0; start < len(ids); start += 100 {
		form := url.Values{"i": ids[start:min(start+100, len(ids))]}
		if add != "" {
			form.Set("a", add)
		}
		if remove != "" {
			form.Set("r", remove)
		}
		b, err := c.do(ctx, http.MethodPost, "/reader/api/0/edit-tag", form, true)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(b)) != "OK" {
			return fmt.Errorf("edit-tag: unexpected response %q", b)
		}
	}
	return nil
}

// Auth returns the token from the last Login, the value a client like Reeder
// stores and sends on every request.
func (c *ReaderClient) Auth() string { return c.auth }

// Package client talks to a Correspondent server's /api/v1 with a personal
// API token. It is what the corr CLI and its MCP server use; the types here
// are the wire format, kept independent of the server's internals.
package client

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

type Client struct {
	Server string // e.g. https://rss.example.org
	Token  string // corr_…
	HTTP   *http.Client
	// UserAgent identifies the client to the server.
	UserAgent string
}

// Error is an error the server returned.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Kinds of failure, for exit codes.
var (
	ErrAuth        = errors.New("authentication")
	ErrNotFound    = errors.New("not found")
	ErrUnavailable = errors.New("server unavailable")
)

// Is maps HTTP statuses onto the kinds above.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrAuth:
		return e.Status == 401 || e.Status == 403
	case ErrNotFound:
		return e.Status == 404
	case ErrUnavailable:
		return e.Status >= 500 || e.Status == 429
	}
	return false
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	if c.Server == "" {
		return &Error{Status: 401, Message: "no server set: run corr auth login, or set CORR_SERVER and CORR_TOKEN"}
	}
	var body io.Reader
	contentType := "application/json"
	switch v := in.(type) {
	case nil:
	case []byte: // sent as is (OPML)
		body, contentType = bytes.NewReader(v), "text/xml"
	default:
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Server, "/")+"/api/v1"+path, body)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if in != nil {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = fmt.Sprintf("the server answered %d", resp.StatusCode)
		}
		return &Error{Status: resp.StatusCode, Message: e.Error}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = data
		return nil
	}
	return json.Unmarshal(data, out)
}

// --- types (the wire format) -------------------------------------------------

type Me struct {
	Username string          `json:"username"`
	Role     string          `json:"role"`
	Version  string          `json:"version"`
	Prefs    json.RawMessage `json:"prefs"`
}

// Theme is the reader's web theme: auto, sun, day, evening or night.
func (m Me) Theme() string {
	var p struct{ Theme string }
	json.Unmarshal(m.Prefs, &p)
	return p.Theme
}

type Folder struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Unread int    `json:"unread"`
}

type Feed struct {
	ID          int64  `json:"id"`
	FolderID    *int64 `json:"folderId"`
	URL         string `json:"url"`
	SiteURL     string `json:"siteUrl"`
	Title       string `json:"title"`
	Description string `json:"description"`
	LastFetchAt int64  `json:"lastFetchAt"`
	ErrorCount  int    `json:"errorCount"`
	LastError   string `json:"lastError"`
	Paused      bool   `json:"paused"`
	Notify      bool   `json:"notify"`
	FullText    bool   `json:"fullText"`
	Page        bool   `json:"page"`
	Unread      int    `json:"unread"`
	Total       int    `json:"total"`
	Starred     int    `json:"starred"`
	LatestAt    int64  `json:"latestAt"`
}

type Counts struct {
	Unread  int    `json:"unread"`
	Starred int    `json:"starred"`
	Today   int    `json:"today"`
	Newest  string `json:"newest"`
}

type Tree struct {
	Folders []Folder `json:"folders"`
	Feeds   []Feed   `json:"feeds"`
	Counts  Counts   `json:"counts"`
}

type Entry struct {
	ID          string `json:"id"`
	FeedID      int64  `json:"feedId"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Content     string `json:"content,omitempty"`
	Excerpt     string `json:"excerpt"`
	ImageURL    string `json:"imageUrl"`
	PublishedAt int64  `json:"publishedAt"`
	Read        bool   `json:"read"`
	Starred     bool   `json:"starred"`
	FullText    bool   `json:"fullText"`
	Audio       bool   `json:"audio"` // read-aloud audio exists
}

type Candidate struct {
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	SiteURL     string   `json:"siteUrl"`
	ItemCount   int      `json:"itemCount"`
	LatestAt    int64    `json:"latestAt"`
	Page        bool     `json:"page,omitempty"`
	Method      string   `json:"method,omitempty"`
	Shared      bool     `json:"shared,omitempty"`
	Sample      []string `json:"sample,omitempty"`
}

// --- calls -------------------------------------------------------------------

func (c *Client) Me(ctx context.Context) (Me, error) {
	var m Me
	return m, c.do(ctx, "GET", "/me", nil, &m)
}

func (c *Client) Tree(ctx context.Context) (Tree, error) {
	var t Tree
	return t, c.do(ctx, "GET", "/tree", nil, &t)
}

// Query selects articles: View is unread, starred, today or all; FeedID or
// FolderID narrow it; Search matches title, excerpt and author.
type Query struct {
	View     string
	FeedID   int64
	FolderID int64
	Search   string
	Limit    int
	Cursor   string
	Oldest   bool
	Content  bool
}

type Page struct {
	Entries []Entry `json:"entries"`
	Cursor  string  `json:"cursor"`
}

func (c *Client) Entries(ctx context.Context, q Query) (Page, error) {
	v := url.Values{}
	if q.View != "" {
		v.Set("view", q.View)
	}
	if q.FeedID != 0 {
		v.Set("feed", strconv.FormatInt(q.FeedID, 10))
	}
	if q.FolderID != 0 {
		v.Set("folder", strconv.FormatInt(q.FolderID, 10))
	}
	if q.Search != "" {
		v.Set("q", q.Search)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Cursor != "" {
		v.Set("cursor", q.Cursor)
	}
	if q.Oldest {
		v.Set("order", "oldest")
	}
	if q.Content {
		v.Set("content", "1")
	}
	var p Page
	if err := c.do(ctx, "GET", "/entries?"+v.Encode(), nil, &p); err != nil {
		return p, err
	}
	if p.Entries == nil {
		p.Entries = []Entry{}
	}
	return p, nil
}

func (c *Client) Entry(ctx context.Context, id string) (Entry, error) {
	var r struct{ Entry Entry }
	return r.Entry, c.do(ctx, "GET", "/entries/"+url.PathEscape(id), nil, &r)
}

// FullArticle fetches the article from its web page.
func (c *Client) FullArticle(ctx context.Context, id string) (Entry, error) {
	var r struct{ Entry Entry }
	return r.Entry, c.do(ctx, "POST", "/entries/"+url.PathEscape(id)+"/full", nil, &r)
}

// SetState applies read, unread, star or unstar; it returns how many changed.
func (c *Client) SetState(ctx context.Context, action string, ids []string) (int, error) {
	var r struct{ Changed int }
	return r.Changed, c.do(ctx, "POST", "/entries/state", map[string]any{"action": action, "ids": ids}, &r)
}

// MarkAllRead marks everything in a feed, folder or view read, up to the
// newest article now on the server.
func (c *Client) MarkAllRead(ctx context.Context, feedID, folderID int64, view string) (int, error) {
	t, err := c.Tree(ctx)
	if err != nil {
		return 0, err
	}
	body := map[string]string{"before": t.Counts.Newest, "view": view}
	if feedID != 0 {
		body["feed"] = strconv.FormatInt(feedID, 10)
	}
	if folderID != 0 {
		body["folder"] = strconv.FormatInt(folderID, 10)
	}
	var r struct{ IDs []string }
	return len(r.IDs), c.do(ctx, "POST", "/entries/mark-all-read", body, &r)
}

func (c *Client) Discover(ctx context.Context, address string) ([]Candidate, error) {
	var r struct{ Candidates []Candidate }
	return r.Candidates, c.do(ctx, "POST", "/discover", map[string]string{"url": address}, &r)
}

// DiscoverAI asks the server for a parser written by the reader's AI.
func (c *Client) DiscoverAI(ctx context.Context, address string) ([]Candidate, error) {
	var r struct{ Candidates []Candidate }
	return r.Candidates, c.do(ctx, "POST", "/discover/ai", map[string]string{"url": address}, &r)
}

// Subscribe follows a feed (or, with page, a page read by its parser).
func (c *Client) Subscribe(ctx context.Context, address, title, folder string, page bool) (int64, error) {
	var r struct{ ID int64 }
	return r.ID, c.do(ctx, "POST", "/feeds", map[string]any{"url": address, "title": title, "folderName": folder, "page": page}, &r)
}

// FeedPatch changes a feed; nil fields are left as they are.
type FeedPatch struct {
	Title      *string `json:"title,omitempty"`
	FolderName string  `json:"folderName,omitempty"`
	NoFolder   bool    `json:"noFolder,omitempty"`
	Paused     *bool   `json:"paused,omitempty"`
	Notify     *bool   `json:"notify,omitempty"`
	FullText   *bool   `json:"fullText,omitempty"`
	URL        *string `json:"url,omitempty"`
}

func (c *Client) UpdateFeed(ctx context.Context, id int64, p FeedPatch) error {
	return c.do(ctx, "PATCH", fmt.Sprintf("/feeds/%d", id), p, nil)
}

func (c *Client) DeleteFeed(ctx context.Context, id int64) error {
	return c.do(ctx, "DELETE", fmt.Sprintf("/feeds/%d", id), nil, nil)
}

func (c *Client) RefreshFeed(ctx context.Context, id int64) error {
	return c.do(ctx, "POST", fmt.Sprintf("/feeds/%d/refresh", id), nil, nil)
}

func (c *Client) CreateFolder(ctx context.Context, name string) (int64, error) {
	var r struct{ ID int64 }
	return r.ID, c.do(ctx, "POST", "/folders", map[string]string{"name": name}, &r)
}

func (c *Client) RenameFolder(ctx context.Context, id int64, name string) error {
	return c.do(ctx, "PATCH", fmt.Sprintf("/folders/%d", id), map[string]string{"name": name}, nil)
}

func (c *Client) DeleteFolder(ctx context.Context, id int64) error {
	return c.do(ctx, "DELETE", fmt.Sprintf("/folders/%d", id), nil, nil)
}

// ExportOPML returns the reader's subscriptions as OPML.
func (c *Client) ExportOPML(ctx context.Context) ([]byte, error) {
	var b []byte
	return b, c.do(ctx, "GET", "/opml", nil, &b)
}

// Audio returns the article read aloud, as MP3: existing audio, or new
// audio made with the reader's own text-to-speech key (which can take a
// minute for a long article).
func (c *Client) Audio(ctx context.Context, id string) ([]byte, error) {
	var r struct{ URL string }
	if err := c.do(ctx, "POST", "/entries/"+url.PathEscape(id)+"/audio", nil, &r); err != nil {
		return nil, err
	}
	var mp3 []byte
	return mp3, c.do(ctx, "GET", "/entries/"+url.PathEscape(id)+"/audio", nil, &mp3)
}

// Login exchanges a username and password for a token.
func (c *Client) Login(ctx context.Context, username, password, name, scope string) (token, user string, err error) {
	var r struct {
		Token    string
		Username string
	}
	err = c.do(ctx, "POST", "/tokens/login", map[string]string{"username": username, "password": password, "name": name, "scope": scope}, &r)
	return r.Token, r.Username, err
}

// Logout revokes the client's own token.
func (c *Client) Logout(ctx context.Context) error {
	return c.do(ctx, "DELETE", "/tokens/current", nil, nil)
}

// ImportOPML subscribes to every feed in an OPML file.
func (c *Client) ImportOPML(ctx context.Context, opml []byte) (int, error) {
	var r struct{ Added int }
	return r.Added, c.do(ctx, "POST", "/opml", opml, &r)
}

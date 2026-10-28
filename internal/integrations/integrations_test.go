package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// seen is one request a fake service received.
type seen struct {
	Method, Path, Auth, ContentType string
	Body                            map[string]any
	Form                            url.Values
}

// fake is a stand-in for every service: it records requests and answers
// each path with the status and body the test sets.
type fake struct {
	*httptest.Server
	mu      sync.Mutex
	reqs    []seen
	answers map[string]func(w http.ResponseWriter)
}

func newFake(t *testing.T) *fake {
	f := &fake{answers: map[string]func(w http.ResponseWriter){}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s := seen{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), ContentType: r.Header.Get("Content-Type")}
		if strings.HasPrefix(s.ContentType, "application/json") {
			json.Unmarshal(b, &s.Body)
		} else {
			s.Form, _ = url.ParseQuery(string(b))
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, s)
		answer := f.answers[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		if answer == nil {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "{}")
			return
		}
		answer(w)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fake) last() seen {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func (f *fake) answer(route string, status int, body string, header ...string) {
	f.answers[route] = func(w http.ResponseWriter) {
		for i := 0; i+1 < len(header); i += 2 {
			w.Header().Set(header[i], header[i+1])
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}
}

// hosted sends requests for the hosted services to the fake.
type hosted struct{ to string }

func (h hosted) RoundTrip(r *http.Request) (*http.Response, error) {
	u, _ := url.Parse(h.to)
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host, r.Host = u.Scheme, u.Host, u.Host
	return http.DefaultTransport.RoundTrip(r)
}

var article = Article{ID: "7", Title: "Ships in the night", URL: "https://notes.example/ships", Author: "Ada",
	Summary: "Pilots guide them.", HTML: "<p>Ships.</p>", Markdown: "Ships.\n", PublishedAt: time.Unix(1_790_000_000, 0)}

func TestConnectorsSendWhatEachServiceExpects(t *testing.T) {
	f := newFake(t)
	hc := &http.Client{Transport: hosted{f.URL}}
	tags := []string{"rss", "later"}
	ctx := context.Background()

	cases := []struct {
		kind   string
		cfg    Config
		sec    Secret
		verify string // the route Verify calls
		send   string // the route Send saves with
		auth   string
		check  func(t *testing.T, s seen)
	}{
		{Readwise, Config{Tags: tags}, Secret{Token: "rw"}, "GET /api/v2/auth/", "POST /api/v3/save/", "Token rw", func(t *testing.T, s seen) {
			if s.Body["url"] != article.URL || s.Body["html"] != article.HTML || s.Body["location"] != "new" || len(s.Body["tags"].([]any)) != 2 {
				t.Errorf("readwise body %v", s.Body)
			}
		}},
		{Instapaper, Config{}, Secret{Username: "ada@example.org", Password: "pw"}, "POST /api/authenticate", "POST /api/add", "Basic YWRhQGV4YW1wbGUub3JnOnB3", func(t *testing.T, s seen) {
			if s.Form.Get("url") != article.URL || s.Form.Get("title") != article.Title || s.Form.Get("selection") != article.Summary {
				t.Errorf("instapaper form %v", s.Form)
			}
		}},
		{Raindrop, Config{Tags: tags}, Secret{Token: "rd"}, "GET /rest/v1/user", "POST /rest/v1/raindrop", "Bearer rd", func(t *testing.T, s seen) {
			if s.Body["link"] != article.URL || s.Body["excerpt"] != article.Summary || s.Body["pleaseParse"] == nil {
				t.Errorf("raindrop body %v", s.Body)
			}
		}},
		{Linkding, Config{BaseURL: f.URL, Tags: tags}, Secret{Token: "ld"}, "GET /api/user/profile/", "POST /api/bookmarks/", "Token ld", func(t *testing.T, s seen) {
			if s.Body["url"] != article.URL || s.Body["unread"] != true || len(s.Body["tag_names"].([]any)) != 2 {
				t.Errorf("linkding body %v", s.Body)
			}
		}},
		{Karakeep, Config{BaseURL: f.URL}, Secret{Token: "kk"}, "GET /api/v1/users/me", "POST /api/v1/bookmarks", "Bearer kk", func(t *testing.T, s seen) {
			if s.Body["type"] != "link" || s.Body["url"] != article.URL || s.Body["title"] != article.Title {
				t.Errorf("karakeep body %v", s.Body)
			}
		}},
	}
	f.answer("POST /rest/v1/raindrop", 200, `{"result":true,"item":{"_id":1}}`)
	f.answer("POST /api/v3/save/", 201, `{"id":"abc","url":"https://read.readwise.io/new/read/abc"}`)
	f.answer("GET /api/v2/auth/", 204, "")
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			conn, err := New(c.kind, c.cfg, c.sec, hc)
			if err != nil {
				t.Fatal(err)
			}
			if err := conn.Verify(ctx); err != nil {
				t.Fatalf("verify: %v", err)
			}
			if s := f.last(); s.Method+" "+s.Path != c.verify || s.Auth != c.auth {
				t.Errorf("verify asked %s %s with %q", s.Method, s.Path, s.Auth)
			}
			remote, err := conn.Send(ctx, article)
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			s := f.last()
			if s.Method+" "+s.Path != c.send || s.Auth != c.auth {
				t.Errorf("send asked %s %s with %q", s.Method, s.Path, s.Auth)
			}
			c.check(t, s)
			if c.kind == Readwise && remote != "https://read.readwise.io/new/read/abc" {
				t.Errorf("readwise remote %q", remote)
			}
		})
	}
}

func TestWallabagSignsInThenSaves(t *testing.T) {
	f := newFake(t)
	f.answer("POST /oauth/v2/token", 200, `{"access_token":"tok","expires_in":3600,"token_type":"bearer"}`)
	f.answer("POST /api/entries.json", 200, `{"id":42,"url":"https://notes.example/ships"}`)
	conn, _ := New(Wallabag, Config{BaseURL: f.URL, Tags: []string{"rss"}}, Secret{ClientID: "c", ClientSecret: "s", Username: "ada", Password: "pw"}, f.Client())
	remote, err := conn.Send(context.Background(), article)
	if err != nil {
		t.Fatal(err)
	}
	tok := f.reqs[len(f.reqs)-2]
	if tok.Form.Get("grant_type") != "password" || tok.Form.Get("client_id") != "c" || tok.Form.Get("username") != "ada" {
		t.Errorf("token form %v", tok.Form)
	}
	if s := f.last(); s.Auth != "Bearer tok" || s.Body["content"] != article.HTML || s.Body["tags"] != "rss" {
		t.Errorf("entry %q %v", s.Auth, s.Body)
	}
	if remote != f.URL+"/view/42" {
		t.Errorf("remote %q", remote)
	}

	// A wrong password is invalid_grant, a 400: the credentials, not a retry.
	f.answer("POST /oauth/v2/token", 400, `{"error":"invalid_grant"}`)
	if err := conn.Verify(context.Background()); !errors.Is(err, ErrRejected) {
		t.Errorf("wrong password: %v", err)
	}
}

func TestErrorsSayWhetherToRetry(t *testing.T) {
	f := newFake(t)
	conn, _ := New(Linkding, Config{BaseURL: f.URL}, Secret{Token: "x"}, f.Client())
	for _, c := range []struct {
		status int
		header []string
		retry  bool
		after  time.Duration
		reject bool
	}{
		{401, nil, false, 0, true},
		{429, []string{"Retry-After", "120"}, true, 2 * time.Minute, false},
		{503, nil, true, 0, false},
		{400, nil, false, 0, false},
	} {
		f.answer("POST /api/bookmarks/", c.status, `{"detail":"no"}`, c.header...)
		_, err := conn.Send(context.Background(), article)
		retry, after := Retryable(err)
		if retry != c.retry || after != c.after || errors.Is(err, ErrRejected) != c.reject {
			t.Errorf("%d: retry %v after %v rejected %v (%v)", c.status, retry, after, errors.Is(err, ErrRejected), err)
		}
	}
}

func TestWebhookIsSigned(t *testing.T) {
	var got []byte
	var sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		sig = r.Header.Get("Correspondent-Signature")
	}))
	defer srv.Close()
	conn, _ := New(Webhook, Config{BaseURL: srv.URL}, Secret{SigningKey: "whsec_test"}, srv.Client())
	if _, err := conn.Send(context.Background(), article); err != nil {
		t.Fatal(err)
	}
	var ev Event
	json.Unmarshal(got, &ev)
	if ev.Event != "article" || ev.Article == nil || ev.Article.Markdown != article.Markdown {
		t.Fatalf("event %s", got)
	}
	// The receiver checks it the same way.
	if want := Sign("whsec_test", got, time.Unix(ev.SentAt, 0)); sig != want {
		t.Errorf("signature %q, want %q", sig, want)
	}
	if Sign("other", got, time.Unix(ev.SentAt, 0)) == sig {
		t.Error("another key signs the same")
	}
}

func TestSettingsAreChecked(t *testing.T) {
	if _, err := (Config{BaseURL: "http://wallabag.lan"}).Normalize(Wallabag, false); err == nil {
		t.Error("plain http allowed without the admin's networks")
	}
	if c, err := (Config{BaseURL: "http://wallabag.lan/"}).Normalize(Wallabag, true); err != nil || c.BaseURL != "http://wallabag.lan" {
		t.Errorf("http with the admin's networks: %v %q", err, c.BaseURL)
	}
	if c, _ := (Config{BaseURL: "https://x.example", Tags: []string{" a ", ""}}).Normalize(Readwise, false); c.BaseURL != "" || len(c.Tags) != 1 {
		t.Errorf("hosted service keeps %+v", c)
	}
	if err := (Secret{}).Check(Wallabag); err == nil {
		t.Error("wallabag without its client")
	}
	if err := (Secret{Username: "ada"}).Check(Instapaper); err != nil {
		t.Errorf("instapaper without a password: %v", err)
	}
}

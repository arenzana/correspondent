package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/store"
)

type headerTransport struct {
	token string
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestMCP(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &API{Store: st, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute), PublicURL: "https://rss.example.org"}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", a.Handler())
	mux.Handle("/mcp", a.MCPHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	uid, _ := st.CreateUser(ctx, "alice", "password-alice")
	fid, _ := st.AddFeed(ctx, uid, "https://alice.example/feed", "Alice's Notes", nil)
	st.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "1", Title: "A first note", URL: "https://alice.example/1",
		Content: `<p>Hello <a href="/about">about</a> <img src="/img/x/1600/y" alt="pic"></p>`, Enclosures: "[]", PublishedAt: time.Now()}}, time.Time{})
	write, _, _ := st.CreateToken(ctx, uid, "agent", store.ScopeWrite)
	read, _, _ := st.CreateToken(ctx, uid, "reader", store.ScopeRead)

	// No token, or a bad one: 401 with a challenge, before any MCP.
	for _, tok := range []string{"", "corr_forged"} {
		req, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{}`))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("token %q: %d %q", tok, resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
		}
	}

	connect := func(token string) *mcp.ClientSession {
		c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp",
			HTTPClient: &http.Client{Transport: headerTransport{token}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cs.Close() })
		return cs
	}
	cs := connect(write)

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	hints := map[string]*mcp.ToolAnnotations{}
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
		hints[tl.Name] = tl.Annotations
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "find_feeds,list_articles,list_feeds,mark_all_read,read_article,set_article_state,subscribe,unsubscribe,update_feed" {
		t.Fatalf("tools: %v", names)
	}
	if !hints["list_articles"].ReadOnlyHint || hints["unsubscribe"].DestructiveHint == nil || !*hints["unsubscribe"].DestructiveHint {
		t.Error("tool hints")
	}

	call := func(cs *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out != nil && !res.IsError {
			b, _ := json.Marshal(res.StructuredContent)
			json.Unmarshal(b, out)
		}
		return res
	}

	var list struct {
		Articles []struct{ ID, Title, Feed string }
	}
	call(cs, "list_articles", map[string]any{}, &list)
	if len(list.Articles) != 1 || list.Articles[0].Title != "A first note" || list.Articles[0].Feed != "Alice's Notes" {
		t.Fatalf("list: %+v", list)
	}
	id := list.Articles[0].ID

	var art struct {
		Markdown string
		Read     bool
	}
	call(cs, "read_article", map[string]any{"id": id}, &art)
	if !strings.Contains(art.Markdown, "[about](https://rss.example.org/about)") || !strings.Contains(art.Markdown, "![pic](https://rss.example.org/img/x/1600/y)") || art.Read {
		t.Errorf("read: %+v", art)
	}

	// A read-only token reads but can't change anything.
	ro := connect(read)
	call(ro, "list_feeds", nil, nil)
	if res := call(ro, "set_article_state", map[string]any{"action": "star", "ids": []string{id}}, nil); !res.IsError {
		t.Error("read-only token changed state")
	}
	if res := call(cs, "set_article_state", map[string]any{"action": "star", "ids": []string{id}}, nil); res.IsError {
		t.Errorf("write token: %+v", res.Content)
	}
	if e, _ := st.Entry(ctx, uid, mustID(id)); !e.Starred {
		t.Error("star not applied")
	}
}

func mustID(s string) int64 {
	var n int64
	for _, c := range s {
		n = n*10 + int64(c-'0')
	}
	return n
}

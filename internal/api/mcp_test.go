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
	if strings.Join(names, ",") != "find_feeds,list_articles,list_feeds,list_integrations,mark_all_read,read_article,remove_search,save_search,send_article,set_article_state,subscribe,summarize_article,unsubscribe,update_feed" {
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

	// Saved searches: saved, listed with the feeds, used to list and to
	// mark only their matches read, and removed.
	st.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "2", Title: "Harbour log", Content: "<p>Pilots board at dawn.</p>",
		Enclosures: "[]", PublishedAt: time.Now()}}, time.Time{})
	var saved struct{ ID int64 }
	if res := call(ro, "save_search", map[string]any{"query": "pilots"}, nil); !res.IsError {
		t.Error("read-only token saved a search")
	}
	call(cs, "save_search", map[string]any{"query": "pilots", "name": "Harbour"}, &saved)
	var tree struct{ Searches []struct{ ID, Unread int64 } }
	call(cs, "list_feeds", nil, &tree)
	if len(tree.Searches) != 1 || tree.Searches[0].ID != saved.ID || tree.Searches[0].Unread != 1 {
		t.Fatalf("searches in list_feeds: %+v", tree.Searches)
	}
	list.Articles = nil
	call(cs, "list_articles", map[string]any{"saved_search_id": saved.ID}, &list)
	if len(list.Articles) != 1 || list.Articles[0].Title != "Harbour log" {
		t.Errorf("list by saved search: %+v", list)
	}
	var changed struct{ Changed int }
	call(cs, "mark_all_read", map[string]any{"saved_search_id": saved.ID}, &changed)
	if changed.Changed != 1 {
		t.Errorf("mark all read in a search: %d", changed.Changed)
	}
	if e, _ := st.Entry(ctx, uid, mustID(id)); e.Read {
		t.Error("marking a search read touched an article outside it")
	}
	if res := call(cs, "list_articles", map[string]any{"saved_search_id": 999}, nil); !res.IsError {
		t.Error("an unknown saved search listed")
	}
	call(cs, "remove_search", map[string]any{"id": saved.ID}, nil)
	call(cs, "list_feeds", nil, &tree)
	if len(tree.Searches) != 0 {
		t.Errorf("removed search still listed: %+v", tree.Searches)
	}

	// Summaries need AI (this server has none): a clear error, not a hang.
	res := call(cs, "summarize_article", map[string]any{"id": id}, nil)
	if !res.IsError || !strings.Contains(textOf(res), "AI") {
		t.Errorf("summarize without AI: %+v", res.Content)
	}
}

func mustID(s string) int64 {
	var n int64
	for _, c := range s {
		n = n*10 + int64(c-'0')
	}
	return n
}

func textOf(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

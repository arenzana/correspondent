package main

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/pagefeed"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/api"
	"github.com/arenzana/correspondent/internal/client"
	"github.com/arenzana/correspondent/internal/integrations"
	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/secretbox"
	"github.com/arenzana/correspondent/internal/speech"
	"github.com/arenzana/correspondent/internal/store"
)

func TestGlobalFlags(t *testing.T) {
	rest, asJSON, server, token, theme, err := globalFlags([]string{"list", "--json", "--server=https://a.example", "--view", "starred", "--token", "corr_x", "--theme", "day"})
	if err != nil || !asJSON || server != "https://a.example" || token != "corr_x" || theme != "day" || strings.Join(rest, " ") != "list --view starred" {
		t.Fatalf("%v %v %q %q %q %v", rest, asJSON, server, token, theme, err)
	}
	if _, _, _, _, _, err := globalFlags([]string{"--server"}); exitCode(err) != exitUsage {
		t.Errorf("missing value: %v", err)
	}
}

func TestExitCodes(t *testing.T) {
	for err, want := range map[error]int{
		nil:                          exitOK,
		usagef("bad"):                exitUsage,
		&client.Error{Status: 401}:   exitAuth,
		&client.Error{Status: 403}:   exitAuth,
		&client.Error{Status: 404}:   exitNotFound,
		&client.Error{Status: 503}:   exitUnavailable,
		&client.Error{Status: 429}:   exitUnavailable,
		&client.Error{Status: 400}:   exitError,
		errors.New("something else"): exitError,
		client.ErrUnavailable:        exitUnavailable,
	} {
		if got := exitCode(err); got != want {
			t.Errorf("%v → %d, want %d", err, got, want)
		}
	}
}

func TestConfigFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CORR_SERVER", "")
	t.Setenv("CORR_TOKEN", "")
	if err := saveConfig(config{Server: "https://a.example", Token: "corr_secret"}); err != nil {
		t.Fatal(err)
	}
	p, _ := configPath()
	if info, _ := os.Stat(p); info.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v", info.Mode().Perm())
	}
	t.Setenv("CORR_TOKEN", "corr_env")
	c, _ := loadConfig()
	if c.Server != "https://a.example" || c.Token != "corr_env" {
		t.Errorf("%+v", c)
	}
	removeConfig()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("config not removed")
	}
}

func TestThemes(t *testing.T) {
	if got := mix("#ffffff", "#000000", 0.5); got != "#808080" {
		t.Errorf("mix %s", got)
	}
	for in, want := range map[[2]string]string{
		{"night", "day"}: "night", // --theme wins
		{"", "evening"}:  "evening",
		{"", "day"}:      "day",
		{"light", ""}:    "day",
	} {
		if got := chooseTheme(in[0], in[1]).name; got != want {
			t.Errorf("%v → %s", in, got)
		}
	}
	p := palettes["evening"]
	g := p.glamourStyle()
	if *g.H1.Color != p.accent || *g.LinkText.Color != p.accent || g.H1.BackgroundColor != nil {
		t.Error("glamour style not in the palette")
	}
}

func TestReadIDs(t *testing.T) {
	got := readIDs(strings.NewReader("\"123\"\n 456 \n\n789\n"))
	if strings.Join(got, ",") != "123,456,789" {
		t.Errorf("%q", got)
	}
}

// TestCommands runs commands against a real API in-process.
func TestCommands(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &api.API{Store: st, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute)}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	uid, _ := st.CreateUser(ctx, "alice", "password-alice")
	fid, _ := st.AddFeed(ctx, uid, "https://notes.example/feed", "Notes", nil)
	st.UpsertEntries(ctx, fid, []store.NewEntry{
		{GUID: "1", Title: "First", URL: "https://notes.example/1", Content: `<p>Hello <a href="https://x.example/">there</a>.</p>`, Enclosures: "[]", PublishedAt: time.Now()},
		{GUID: "2", Title: "Second", URL: "https://notes.example/2", Content: "<p>Two</p>", Enclosures: "[]", PublishedAt: time.Now().Add(-time.Hour)},
	}, time.Time{})
	token, _, _ := st.CreateToken(ctx, uid, "test", store.ScopeWrite)

	run := func(args ...string) (map[string]any, error) {
		t.Helper()
		app := &app{ctx: ctx, asJSON: true}
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if err := app.setup(srv.URL, token); err != nil {
			t.Fatal(err)
		}
		r, w, _ := os.Pipe()
		old := os.Stdout
		os.Stdout = w
		err := app.run(args)
		w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		io.Copy(&buf, r)
		var out map[string]any
		json.Unmarshal(buf.Bytes(), &out)
		return out, err
	}

	out, err := run("list")
	if err != nil {
		t.Fatal(err)
	}
	entries := out["entries"].([]any)
	if len(entries) != 2 || entries[0].(map[string]any)["title"] != "First" || entries[0].(map[string]any)["feed"] != "Notes" {
		t.Fatalf("list: %v", out)
	}
	id := entries[0].(map[string]any)["id"].(string)

	out, err = run("read", id)
	if err != nil || !strings.Contains(out["markdown"].(string), "[there](https://x.example/)") || out["read"] != true {
		t.Fatalf("read: %v %v", out, err)
	}
	if out, err = run("mark", "star", id); err != nil || out["changed"].(float64) != 1 {
		t.Errorf("mark: %v %v", out, err)
	}
	if out, _ = run("list", "--view", "starred"); len(out["entries"].([]any)) != 1 {
		t.Errorf("starred: %v", out)
	}
	if out, _ = run("feeds"); len(out["feeds"].([]any)) != 1 {
		t.Errorf("feeds: %v", out)
	}
	if _, err := run("read", "999"); exitCode(err) != exitNotFound {
		t.Errorf("missing article: %v", err)
	}
	if _, err := run("list", "--view", "nope"); exitCode(err) != exitUsage {
		t.Errorf("bad view: %v", err)
	}
	if _, err := run("frobnicate"); exitCode(err) != exitUsage {
		t.Errorf("unknown command: %v", err)
	}
	token = "corr_wrong"
	if _, err := run("list"); exitCode(err) != exitAuth {
		t.Errorf("bad token: %v", err)
	}
}

// TestWatch streams new articles as NDJSON over the live connection.
func TestWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &api.API{Store: st, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute)}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	uid, _ := st.CreateUser(ctx, "alice", "password-alice")
	fid, _ := st.AddFeed(ctx, uid, "https://notes.example/feed", "Notes", nil)
	st.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "old", Title: "Before watching", Enclosures: "[]", PublishedAt: time.Now()}}, time.Time{})
	token, _, _ := st.CreateToken(ctx, uid, "test", store.ScopeRead)

	app := &app{ctx: ctx, asJSON: true}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app.setup(srv.URL, token)
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	done := make(chan error)
	go func() { done <- app.run([]string{"watch"}) }()

	// Once watching, an article arrives.
	time.Sleep(300 * time.Millisecond)
	st.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "new", Title: "Fresh off the wire", URL: "https://notes.example/new", Enclosures: "[]", PublishedAt: time.Now()}}, time.Time{})
	a.Notify(uid)

	line := make(chan string)
	go func() {
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)
		line <- string(buf[:n])
	}()
	var got string
	select {
	case got = <-line:
	case <-time.After(5 * time.Second):
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("watch ended with %v", err)
	}
	w.Close()
	os.Stdout = old
	var ev map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &ev); err != nil || ev["title"] != "Fresh off the wire" || ev["feed"] != "Notes" {
		t.Fatalf("watch printed %q (%v)", got, err)
	}
	if strings.Contains(got, "Before watching") {
		t.Error("watch replayed an article from before it started")
	}
}

// TestListen saves an article's existing audio with -o.
func TestListen(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	data := t.TempDir()
	box, _ := secretbox.Load(filepath.Join(t.TempDir(), "keyring"))
	a := &api.API{Store: st, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute), DataDir: data, Keyring: box}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	uid, _ := st.CreateUser(ctx, "alice", "password-alice")
	fid, _ := st.AddFeed(ctx, uid, "https://notes.example/feed", "Notes", nil)
	st.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "1", Title: "Ships", Content: "<p>Ships.</p>", Enclosures: "[]", PublishedAt: time.Now()}}, time.Time{})
	ids, _, _ := st.EntryIDs(ctx, store.Filter{UserID: uid})
	id := strconv.FormatInt(ids[0], 10)
	token, _, _ := st.CreateToken(ctx, uid, "test", store.ScopeWrite)

	run := func(args ...string) error {
		app := &app{ctx: ctx, asJSON: true}
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		app.setup(srv.URL, token)
		old := os.Stdout
		os.Stdout, _ = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		defer func() { os.Stdout = old }()
		return app.run(args)
	}
	// No audio and no read-aloud setup: a clear failure, not a hang.
	if err := run("listen", id, "-o", filepath.Join(t.TempDir(), "x.mp3")); err == nil || !strings.Contains(err.Error(), "Settings") {
		t.Errorf("without setup: %v", err)
	}
	// Audio someone already made of this text is fetched as is.
	os.MkdirAll(filepath.Join(data, "audio"), 0o700)
	file := speech.ScriptHash(speech.Script("Ships", "", "<p>Ships.</p>")) + "-other.mp3"
	os.WriteFile(filepath.Join(data, "audio", file), []byte("mp3-bytes"), 0o600)
	out := filepath.Join(t.TempDir(), "ships.mp3")
	if err := run("listen", id, "-o", out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "mp3-bytes" {
		t.Errorf("saved %q", b)
	}
}

// TestSummaryAndSearches covers corr summary (the shared summary; a clear
// failure without AI) and saving, listing, using and removing searches.
func TestSummaryAndSearches(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secretbox.Load(filepath.Join(t.TempDir(), "keyring"))
	a := &api.API{Store: st, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute), DataDir: t.TempDir(), Keyring: box}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	uid, _ := st.CreateUser(ctx, "alice", "password-alice")
	fid, _ := st.AddFeed(ctx, uid, "https://notes.example/feed", "Notes", nil)
	st.UpsertEntries(ctx, fid, []store.NewEntry{
		{GUID: "1", Title: "Ships", Content: "<p>Pilots guide ships.</p>", Enclosures: "[]", PublishedAt: time.Now()},
		{GUID: "2", Title: "Gardens", Content: "<p>Roses.</p>", Enclosures: "[]", PublishedAt: time.Now()},
	}, time.Time{})
	ids, _, _ := st.EntryIDs(ctx, store.Filter{UserID: uid, Query: "pilots"})
	ships := strconv.FormatInt(ids[0], 10)
	token, _, _ := st.CreateToken(ctx, uid, "test", store.ScopeWrite)

	run := func(args ...string) (string, error) {
		app := &app{ctx: ctx, asJSON: true}
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		app.setup(srv.URL, token)
		f, _ := os.CreateTemp(t.TempDir(), "out")
		old := os.Stdout
		os.Stdout = f
		err := app.run(args)
		os.Stdout = old
		f.Close()
		b, _ := os.ReadFile(f.Name())
		return string(b), err
	}

	if _, err := run("summary", ships); err == nil || !strings.Contains(err.Error(), "Settings") {
		t.Errorf("summary without AI: %v", err)
	}
	// A summary someone already had made of this text comes back as is.
	text := content.SearchText("<p>Pilots guide ships.</p>", pagefeed.SummaryInput)
	sum := sha256.Sum256([]byte("Ships\x00" + text))
	st.SaveSummary(ctx, hex.EncodeToString(sum[:])[:20], "Pilots guide ships into port.")
	if out, err := run("summary", ships); err != nil || !strings.Contains(out, "Pilots guide ships into port.") {
		t.Errorf("shared summary: %q %v", out, err)
	}

	// In the reader: S shows the summary above the article, S again hides it.
	ra := &app{ctx: ctx}
	ra.setup(srv.URL, token)
	chooseTheme("day", "").apply()
	m := newReader(ra)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	e, _ := ra.api.Entry(ctx, ships)
	m.Update(m.openArticle(e, false)())
	// The shared summary came with the article, so S shows it at once.
	if cmd := m.summary(); cmd != nil {
		m.Update(cmd())
	}
	if !m.showSummary || !strings.Contains(m.vp.View(), "Pilots guide ships into port.") {
		t.Errorf("reader summary not shown:\n%s", m.vp.View())
	}
	m.summary()
	if m.showSummary || strings.Contains(m.vp.View(), "into port") {
		t.Error("S again didn't hide the summary")
	}
	// Opening it in the reader read it; unread again for what follows.
	if _, err := run("mark", "unread", ships); err != nil {
		t.Fatal(err)
	}

	out, err := run("searches", "save", "pilots", "--name", "Harbour")
	if err != nil || !strings.Contains(out, `"name": "Harbour"`) {
		t.Fatalf("save: %q %v", out, err)
	}
	var saved struct{ ID int64 }
	json.Unmarshal([]byte(out), &saved)
	if out, _ := run("searches"); !strings.Contains(out, `"unread": 1`) {
		t.Errorf("list: %q", out)
	}
	// The reader lists it among its sources, and its list is its matches.
	m.Update(m.loadTree()())
	var at = -1
	for i, src := range m.sources {
		if src.label == "Harbour" && src.search == "pilots" {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("saved search not in the reader's sources: %+v", m.sources)
	}
	m.src = at
	if q := m.query_(); q.Search != "pilots" {
		t.Errorf("reader query for a saved search: %+v", q)
	}
	id := strconv.FormatInt(saved.ID, 10)
	if out, _ := run("list", "--saved", id); !strings.Contains(out, "Ships") || strings.Contains(out, "Gardens") {
		t.Errorf("list --saved: %q", out)
	}
	if out, _ := run("mark-all-read", "--saved", id); !strings.Contains(out, `"changed": 1`) {
		t.Errorf("mark-all-read --saved: %q", out)
	}
	if out, _ := run("list"); !strings.Contains(out, "Gardens") {
		t.Errorf("mark-all-read --saved read more than its matches: %q", out)
	}
	if _, err := run("searches", "remove", id); err != nil {
		t.Error(err)
	}
	if _, err := run("list", "--saved", id); err == nil {
		t.Error("a removed search still lists")
	}
}

// TestSend sends an article to a webhook with corr send, which waits to
// say it arrived; with no service named and two set up, it asks which.
func TestSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secretbox.Load(filepath.Join(t.TempDir(), "keyring"))
	integ := &integrations.Service{Store: st, Keyring: box, Log: slog.New(slog.DiscardHandler), HTTP: http.DefaultClient, PlainHTTP: true}
	go integ.Run(ctx)
	a := &api.API{Store: st, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute), Keyring: box, Integrations: integ}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	var got atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got.Add(1) }))
	t.Cleanup(hook.Close)

	uid, _ := st.CreateUser(ctx, "alice", "password-alice")
	fid, _ := st.AddFeed(ctx, uid, "https://notes.example/feed", "Notes", nil)
	st.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "1", Title: "First", URL: "https://notes.example/1", Enclosures: "[]", PublishedAt: time.Now()}}, time.Time{})
	ids, _, _ := st.EntryIDs(ctx, store.Filter{UserID: uid})
	sealed, _ := integ.Seal(uid, integrations.Secret{SigningKey: "whsec_x"})
	st.CreateIntegration(ctx, uid, "webhook", "Hook", `{"baseUrl":"`+hook.URL+`"}`, sealed, false)
	token, _, _ := st.CreateToken(ctx, uid, "test", store.ScopeWrite)

	run := func(args ...string) error {
		app := &app{ctx: ctx, asJSON: true}
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if err := app.setup(srv.URL, token); err != nil {
			t.Fatal(err)
		}
		old := os.Stdout
		os.Stdout, _ = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		defer func() { os.Stdout = old }()
		return app.run(args)
	}
	id := strconv.FormatInt(ids[0], 10)
	if err := run("send", id); err != nil || got.Load() != 1 {
		t.Fatalf("send: %v, %d requests", err, got.Load())
	}
	st.CreateIntegration(ctx, uid, "webhook", "Other", `{"baseUrl":"`+hook.URL+`"}`, sealed, false)
	if err := run("send", id); exitCode(err) != exitUsage {
		t.Errorf("two services, none named: %v", err)
	}
	if err := run("send", id, "--to", "other"); err != nil || got.Load() != 2 {
		t.Errorf("--to other: %v, %d requests", err, got.Load())
	}
	if err := run("send", id, "--to", "nope"); err == nil {
		t.Error("an unknown service")
	}
}

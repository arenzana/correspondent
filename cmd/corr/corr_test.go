package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/api"
	"github.com/arenzana/correspondent/internal/client"
	"github.com/arenzana/correspondent/internal/ratelimit"
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

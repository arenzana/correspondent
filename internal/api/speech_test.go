package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/secretbox"
	"github.com/arenzana/correspondent/internal/store"
)

// TestReadAloud makes audio with one reader's key, shares it with another
// reader who has none, and serves it to apps through a private link.
func TestReadAloud(t *testing.T) {
	var spoken atomic.Int32
	tts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-speech-key-0123456789" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		spoken.Add(1)
		w.Header().Set("Content-Type", "audio/mpeg")
		io.WriteString(w, "ID3-fake-mp3")
	}))
	t.Cleanup(tts.Close)

	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secretbox.Load(filepath.Join(t.TempDir(), "keyring"))
	a := &API{Store: st, Log: slog.New(slog.DiscardHandler), Limiter: ratelimit.New(10, time.Minute), Keyring: box,
		AITransport: tts.Client().Transport, DataDir: t.TempDir(), PublicURL: "https://rss.example"}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	e := &aiEnv{srv: srv, store: st}
	alice, bob := e.login(t, "alice"), e.login(t, "bob")

	// The same article in both readers' feeds.
	entryOf := func(user string) string {
		uid, _ := st.UserIDByName(ctx, user)
		fid, _ := st.AddFeed(ctx, uid, "https://notes.example/feed", "Notes", nil)
		st.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "1", Title: "Ships", Content: "<p>Ships in the night.</p>",
			Enclosures: "[]", PublishedAt: time.Now()}}, time.Time{})
		ids, _, _ := st.EntryIDs(ctx, store.Filter{UserID: uid})
		return itoa(ids[0])
	}
	aliceEntry, bobEntry := entryOf("alice"), entryOf("bob")

	if r := e.call(t, alice, "POST", "/api/v1/entries/"+aliceEntry+"/audio", nil); r.StatusCode != http.StatusConflict {
		t.Errorf("without setup: %d", r.StatusCode)
	}
	setup := map[string]string{"provider": "openai", "baseUrl": tts.URL, "key": "sk-wrong-key-0123456789xx"}
	if r := e.call(t, alice, "PUT", "/api/v1/speech", setup); r.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("wrong key accepted: %d", r.StatusCode)
	}
	setup["key"] = "sk-speech-key-0123456789"
	if r := e.call(t, alice, "PUT", "/api/v1/speech", setup); r.StatusCode != 200 {
		t.Fatalf("setup: %d", r.StatusCode)
	}
	me := decodeInto[map[string]any](t, e.call(t, alice, "GET", "/api/v1/me", nil))
	if sp, _ := me["speech"].(map[string]any); sp["keyHint"] != "6789" || sp["model"] != "gpt-4o-mini-tts" {
		t.Errorf("me.speech = %v", me["speech"])
	}
	before := spoken.Load()

	// Alice's key makes it; asking again makes nothing new.
	got := decodeInto[map[string]string](t, e.call(t, alice, "POST", "/api/v1/entries/"+aliceEntry+"/audio", nil))
	if got["url"] != "/api/v1/entries/"+aliceEntry+"/audio" || spoken.Load() != before+1 {
		t.Fatalf("make: %v, %d requests", got, spoken.Load()-before)
	}
	e.call(t, alice, "POST", "/api/v1/entries/"+aliceEntry+"/audio", nil)
	r := e.call(t, alice, "GET", got["url"], nil)
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "audio/mpeg" || string(body) != "ID3-fake-mp3" || spoken.Load() != before+1 {
		t.Errorf("serve: %d %q, %d requests", r.StatusCode, body, spoken.Load()-before)
	}

	// Bob, with no key, finds it ready when he opens the article, and plays it.
	opened := decodeInto[struct{ Entry struct{ Audio bool } }](t, e.call(t, bob, "GET", "/api/v1/entries/"+bobEntry, nil))
	if !opened.Entry.Audio {
		t.Error("shared audio not offered to another reader")
	}
	if r := e.call(t, bob, "POST", "/api/v1/entries/"+bobEntry+"/audio", nil); r.StatusCode != 200 || spoken.Load() != before+1 {
		t.Errorf("shared make: %d, %d requests", r.StatusCode, spoken.Load()-before)
	}
	// But not Alice's entry itself.
	if r := e.call(t, bob, "GET", "/api/v1/entries/"+aliceEntry+"/audio", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("another reader's entry: %d", r.StatusCode)
	}

	// Apps get a private link that needs no sign-in, and a forged one fails.
	uid, _ := st.UserIDByName(ctx, "alice")
	link := a.AudioLink(uid, mustInt(aliceEntry))
	if !strings.HasPrefix(link, "https://rss.example/api/v1/audio/") {
		t.Fatalf("link %q", link)
	}
	path := strings.TrimPrefix(link, "https://rss.example")
	if r, _ := http.Get(srv.URL + path); r.StatusCode != 200 {
		t.Errorf("private link: %d", r.StatusCode)
	}
	if r, _ := http.Get(srv.URL + path[:len(path)-8] + "AAAA.mp3"); r.StatusCode != http.StatusNotFound {
		t.Errorf("forged link: %d", r.StatusCode)
	}

	// The sweep keeps audio entries still use.
	if n, _ := a.SweepAudio(ctx); n != 0 {
		t.Errorf("swept %d files still in use", n)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func mustInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/integrations"
	"github.com/arenzana/correspondent/internal/secretbox"
	"github.com/arenzana/correspondent/internal/store"
)

// TestIntegrationFlow adds a webhook, sends an article to it through the
// worker, signed, and has the send-starred rule send another; credentials
// never come back, API tokens can't add integrations, and one reader can't
// use another's.
func TestIntegrationFlow(t *testing.T) {
	e := newEnv(t)
	box, _ := secretbox.Load(filepath.Join(t.TempDir(), "keyring"))
	integ := &integrations.Service{Store: e.store, Keyring: box, Log: e.api.Log, HTTP: http.DefaultClient, PlainHTTP: true}
	integ.Init()
	e.api.Keyring, e.api.Integrations = box, integ
	ctx := context.Background()
	e.store.OnStateChange = func(uid int64, action string, ids []int64) {
		if action == "star" {
			integ.Starred(ctx, uid, ids)
		}
	}

	// The receiver: records what arrives; answers with status.
	var mu sync.Mutex
	var events []integrations.Event
	var sigs []string
	status := http.StatusOK
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var ev integrations.Event
		json.Unmarshal(b, &ev)
		mu.Lock()
		events = append(events, ev)
		sigs = append(sigs, r.Header.Get("Correspondent-Signature")+"|"+string(b))
		w.WriteHeader(status)
		mu.Unlock()
	}))
	defer hook.Close()

	alice, aliceEntry := e.user(t, "alice")
	bob, bobEntry := e.user(t, "bob")

	// Adding it pings the address; the signing key is shown this once.
	r := e.do(t, alice, "POST", "/api/v1/integrations", `{"kind":"webhook","name":"My hook","baseUrl":"`+hook.URL+`"}`, true)
	created := decodeJSON[struct {
		Integration struct{ ID int64 }
		SigningKey  string
	}](t, expect(t, r, 200))
	if !strings.HasPrefix(created.SigningKey, "whsec_") || len(events) != 1 || events[0].Event != "ping" {
		t.Fatalf("created %+v, events %+v", created, events)
	}
	id := itoa(created.Integration.ID)

	// The list never carries credentials.
	list := e.do(t, alice, "GET", "/api/v1/integrations", "", false)
	body, _ := io.ReadAll(list.Body)
	if strings.Contains(string(body), created.SigningKey) || !strings.Contains(string(body), `"name":"My hook"`) {
		t.Errorf("list: %s", body)
	}

	// An API token can send, but can't add or change integrations.
	tok := decodeJSON[struct{ Token string }](t, expect(t, e.do(t, alice, "POST", "/api/v1/tokens", `{"name":"cli","scope":"write"}`, true), 200))
	if r := bearer(t, e, tok.Token, "POST", "/api/v1/integrations", `{"kind":"webhook","baseUrl":"`+hook.URL+`"}`); r.StatusCode != http.StatusForbidden {
		t.Errorf("token adds an integration: %d", r.StatusCode)
	}

	// Sending queues it; the worker sends it, signed with that key.
	sent := decodeJSON[struct{ Delivery store.Delivery }](t, expect(t, bearer(t, e, tok.Token, "POST", "/api/v1/entries/"+aliceEntry+"/send", `{"integration":"`+id+`"}`), 200))
	if sent.Delivery.State != store.DeliveryPending {
		t.Fatalf("queued %+v", sent.Delivery)
	}
	integ.RunOnce(ctx)
	mu.Lock()
	last, sig := events[len(events)-1], sigs[len(sigs)-1]
	mu.Unlock()
	if last.Event != "article" || last.Article == nil || last.Article.Title != "secret of alice" {
		t.Fatalf("delivered %+v", last)
	}
	head, raw, _ := strings.Cut(sig, "|")
	if head != integrations.Sign(created.SigningKey, []byte(raw), time.Unix(last.SentAt, 0)) {
		t.Errorf("signature %q doesn't match", head)
	}
	got := decodeJSON[struct{ Deliveries []store.Delivery }](t, expect(t, e.do(t, alice, "GET", "/api/v1/entries/"+aliceEntry+"/deliveries", "", false), 200))
	if len(got.Deliveries) != 1 || got.Deliveries[0].State != store.DeliverySent {
		t.Errorf("deliveries %+v", got.Deliveries)
	}

	// Nobody sends to someone else's integration, or someone else's article.
	expect(t, e.do(t, bob, "POST", "/api/v1/entries/"+bobEntry+"/send", `{"integration":"`+id+`"}`, true), 404)
	expect(t, e.do(t, alice, "POST", "/api/v1/entries/"+bobEntry+"/send", `{"integration":"`+id+`"}`, true), 404)
	expect(t, e.do(t, bob, "DELETE", "/api/v1/integrations/"+id, "", true), 404)

	// The rule: starring sends; the service down is retried, not dropped.
	expect(t, e.do(t, alice, "PATCH", "/api/v1/integrations/"+id, `{"name":"My hook","baseUrl":"`+hook.URL+`","sendStarred":true}`, true), 200)
	uid, _ := e.store.UserIDByName(ctx, "alice")
	fid, _ := e.store.AddFeed(ctx, uid, "https://more.example/feed", "More", nil)
	e.store.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "m", Title: "Starred one", Enclosures: "[]"}}, time.Time{})
	ids, _, _ := e.store.EntryIDs(ctx, store.Filter{UserID: uid, FeedID: fid})
	mu.Lock()
	status = http.StatusServiceUnavailable
	mu.Unlock()
	expect(t, e.do(t, alice, "POST", "/api/v1/entries/state", `{"action":"star","ids":["`+itoa(ids[0])+`"]}`, true), 200)
	integ.RunOnce(ctx)
	waiting := decodeJSON[struct{ Deliveries []store.Delivery }](t, expect(t, e.do(t, alice, "GET", "/api/v1/entries/"+itoa(ids[0])+"/deliveries", "", false), 200))
	if len(waiting.Deliveries) != 1 || waiting.Deliveries[0].State != store.DeliveryPending || waiting.Deliveries[0].Attempts != 1 || waiting.Deliveries[0].Error == "" {
		t.Fatalf("after an outage %+v", waiting.Deliveries)
	}

	// Credentials refused: failed at once, and Settings says why.
	mu.Lock()
	status = http.StatusUnauthorized
	mu.Unlock()
	expect(t, e.do(t, alice, "POST", "/api/v1/entries/"+aliceEntry+"/send", `{"integration":"`+id+`"}`, true), 200)
	integ.RunOnce(ctx)
	got = decodeJSON[struct{ Deliveries []store.Delivery }](t, expect(t, e.do(t, alice, "GET", "/api/v1/entries/"+aliceEntry+"/deliveries", "", false), 200))
	if got.Deliveries[0].State != store.DeliveryFailed {
		t.Errorf("rejected delivery %+v", got.Deliveries[0])
	}
	i, _ := e.store.Integration(ctx, uid, created.Integration.ID)
	if i.LastError == "" {
		t.Error("Settings isn't told the credentials were refused")
	}

	// The note Obsidian gets.
	note := decodeJSON[struct{ Markdown string }](t, expect(t, e.do(t, alice, "GET", "/api/v1/entries/"+aliceEntry+"/markdown", "", false), 200))
	if !strings.HasPrefix(note.Markdown, "---\ntitle: \"secret of alice\"") || !strings.Contains(note.Markdown, "# secret of alice") {
		t.Errorf("note:\n%s", note.Markdown)
	}

	expect(t, e.do(t, alice, "DELETE", "/api/v1/integrations/"+id, "", true), 204)
}

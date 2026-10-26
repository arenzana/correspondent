package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/store"
)

func TestAllowedEndpoint(t *testing.T) {
	for ep, want := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc":              true,
		"https://updates.push.services.mozilla.com/wpush/v2/x": true,
		"https://web.push.apple.com/QH0c":                      true,
		"https://wns2-par02p.notify.windows.com/w/?token=x":    true,
		"http://fcm.googleapis.com/fcm/send/abc":               false, // not HTTPS
		"https://fcm.googleapis.com.evil.example/x":            false,
		"https://evilfcm.googleapis.com.example/x":             false,
		"https://169.254.169.254/latest/meta-data":             false, // SSRF attempt
		"https://localhost/x":                                  false,
		"https://user:pw@fcm.googleapis.com/x":                 false,
		"https://fcm.googleapis.com:8443/x":                    false,
		"not a url":                                            false,
	} {
		if got := AllowedEndpoint(ep); got != want {
			t.Errorf("AllowedEndpoint(%q) = %v, want %v", ep, got, want)
		}
	}
}

func TestBuildPayloadAndTopic(t *testing.T) {
	f := store.Feed{ID: 7, Title: "Irreal"}
	one := BuildPayload(f, []store.Entry{{ID: 99, Title: "Emacs news"}}, "/icons/wire/icon-192.png")
	if one.Title != "Irreal" || one.Body != "Emacs news" || one.URL != "/#/feed/7/e/99" || one.Tag != "feed-7" {
		t.Fatalf("single: %+v", one)
	}
	many := BuildPayload(f, []store.Entry{{ID: 3, Title: "Newest"}, {ID: 2}, {ID: 1}}, "")
	if !strings.HasPrefix(many.Body, "3 new articles") || many.URL != "/#/feed/7" {
		t.Fatalf("batch: %+v", many)
	}
	if got := topic("feed-7 ünïcode/../" + strings.Repeat("x", 40)); len(got) > 32 || strings.ContainsAny(got, " /.") {
		t.Fatalf("topic %q", got)
	}
}

// pushService fakes a browser push service: it checks what a real one
// checks and answers with the status the test chooses.
type pushService struct {
	*httptest.Server
	status   atomic.Int32
	requests atomic.Int32
	lastAuth atomic.Value
}

func newPushService(t *testing.T) *pushService {
	ps := &pushService{}
	ps.status.Store(http.StatusCreated)
	ps.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps.requests.Add(1)
		ps.lastAuth.Store(r.Header.Get("Authorization"))
		if r.Method != http.MethodPost || r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(int(ps.status.Load()))
	}))
	t.Cleanup(ps.Close)
	return ps
}

// deviceKeys are what a browser's PushManager.subscribe() produces.
func deviceKeys(t *testing.T) (p256dh, auth string) {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 16)
	rand.Read(secret)
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(secret)
}

func setup(t *testing.T) (*Notifier, *store.Store, int64, *pushService) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	ps := newPushService(t)
	n := &Notifier{Store: st, Log: slog.New(slog.DiscardHandler), HTTP: ps.Client(),
		Allowed: func(ep string) bool { return strings.HasPrefix(ep, ps.URL) }}
	n.SetSubject(ctx, "https://rss.example.org")
	p, a := deviceKeys(t)
	if err := st.SavePushSubscription(ctx, uid, ps.URL+"/device-1", p, a, "test"); err != nil {
		t.Fatal(err)
	}
	return n, st, uid, ps
}

func TestSendSignsAndEncrypts(t *testing.T) {
	n, _, uid, ps := setup(t)
	sent, err := n.Send(context.Background(), uid, Payload{Title: "t", Body: "b", Tag: "feed-1"})
	if err != nil || sent != 1 {
		t.Fatalf("sent %d, %v", sent, err)
	}
	if auth, _ := ps.lastAuth.Load().(string); !strings.HasPrefix(auth, "vapid t=") || !strings.Contains(auth, ", k=") {
		t.Fatalf("missing VAPID authorization: %q", auth)
	}
	// The key pair is generated once and reused.
	k1, _ := n.PublicKey(context.Background())
	k2, _ := n.PublicKey(context.Background())
	if k1 == "" || k1 != k2 {
		t.Fatal("VAPID key not stable")
	}
}

func TestGoneSubscriptionsAreRemoved(t *testing.T) {
	n, st, uid, ps := setup(t)
	ps.status.Store(http.StatusGone)
	n.Send(context.Background(), uid, Payload{Title: "t"})
	if subs, _ := st.PushSubscriptions(context.Background(), uid); len(subs) != 0 {
		t.Fatal("subscription the push service reported gone was kept")
	}
}

func TestDisallowedStoredEndpointIsNeverCalled(t *testing.T) {
	n, st, uid, ps := setup(t)
	n.Allowed = nil // real allowlist: the fake service's 127.0.0.1 URL is not a push service
	n.Send(context.Background(), uid, Payload{Title: "t"})
	if ps.requests.Load() != 0 {
		t.Fatal("server posted to an address outside the push-service allowlist")
	}
	if subs, _ := st.PushSubscriptions(context.Background(), uid); len(subs) != 0 {
		t.Fatal("disallowed subscription kept")
	}
}

func TestNewArticlesRespectsOptIn(t *testing.T) {
	n, st, uid, ps := setup(t)
	ctx := context.Background()
	fid, _ := st.AddFeed(ctx, uid, "https://x.example/f", "X", nil)
	since := time.Now().UnixMicro() - 1
	st.UpsertEntries(ctx, fid, []store.NewEntry{{GUID: "a", Title: "A", Enclosures: "[]"}, {GUID: "b", Title: "B", Enclosures: "[]"}}, time.Time{})
	f, _ := st.Feed(ctx, uid, fid)

	n.NewArticles(ctx, *f, since) // not opted in
	if ps.requests.Load() != 0 {
		t.Fatal("notified for a feed that is not opted in")
	}
	st.SetFeedNotify(ctx, uid, fid, true)
	f, _ = st.Feed(ctx, uid, fid)
	n.NewArticles(ctx, *f, since)
	if ps.requests.Load() != 1 {
		t.Fatalf("want one batched notification, got %d", ps.requests.Load())
	}
}

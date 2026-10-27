package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/apns"
	"github.com/arenzana/correspondent/internal/push"
	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/store"
)

// TestAppleNotifications registers an Apple app's device, delivers the test
// notification to it through APNs, and forgets it when Apple says it's gone.
func TestAppleNotifications(t *testing.T) {
	var mu sync.Mutex
	var delivered []string
	gone := false
	fake := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		delivered = append(delivered, strings.TrimPrefix(r.URL.Path, "/3/device/"))
		if gone {
			w.WriteHeader(http.StatusGone)
		}
	}))
	t.Cleanup(fake.Close)

	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	ac, err := apns.New(apns.Config{Key: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), KeyID: "K", TeamID: "T", Topic: "org.arenzana.correspondent"})
	if err != nil {
		t.Fatal(err)
	}
	ac.HTTP, ac.ProductionHost, ac.SandboxHost = fake.Client(), fake.URL, fake.URL

	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.DiscardHandler)
	a := &API{Store: st, Log: log, Limiter: ratelimit.New(10, time.Minute), Push: &push.Notifier{Store: st, Log: log, APNS: ac}}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	e := &aiEnv{srv: srv, store: st}
	alice := e.login(t, "alice")

	token := strings.Repeat("ab", 32)
	if r := e.call(t, alice, "POST", "/api/v1/push/apple", map[string]string{"token": "not-hex!", "environment": "production"}); r.StatusCode != http.StatusBadRequest {
		t.Errorf("bad token: %d", r.StatusCode)
	}
	if r := e.call(t, alice, "POST", "/api/v1/push/apple", map[string]string{"token": token, "environment": "sandbox"}); r.StatusCode >= 300 {
		t.Fatalf("register: %d", r.StatusCode)
	}
	if got := decodeInto[map[string]any](t, e.call(t, alice, "GET", "/api/v1/push", nil)); got["apple"] != true {
		t.Errorf("status: %v", got)
	}
	// An account with only the Apple app (no browser subscription) still gets it.
	got := decodeInto[map[string]int](t, e.call(t, alice, "POST", "/api/v1/push/test", nil))
	if got["sent"] != 1 || len(delivered) != 1 || delivered[0] != token {
		t.Fatalf("test notification: %v, delivered %v", got, delivered)
	}
	gone = true
	e.call(t, alice, "POST", "/api/v1/push/test", nil)
	if d, _ := st.APNSDevices(ctx, mustUser(t, st, "alice")); len(d) != 0 {
		t.Errorf("a gone device was kept: %v", d)
	}
}

func mustUser(t *testing.T, st *store.Store, name string) int64 {
	t.Helper()
	id, err := st.UserIDByName(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

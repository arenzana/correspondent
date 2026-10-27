package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	return k, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestSend(t *testing.T) {
	key, p8 := newKey(t)
	var got struct {
		path, topic, collapse, auth string
		body                        map[string]any
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.topic, got.collapse, got.auth = r.URL.Path, r.Header.Get("apns-topic"), r.Header.Get("apns-collapse-id"), r.Header.Get("authorization")
		json.NewDecoder(r.Body).Decode(&got.body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/gone"):
			w.WriteHeader(http.StatusGone)
			w.Write([]byte(`{"reason":"Unregistered"}`))
		case strings.HasSuffix(r.URL.Path, "/bad"):
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"reason":"BadDeviceToken"}`))
		case strings.HasSuffix(r.URL.Path, "/busy"):
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	c, err := New(Config{Key: p8, KeyID: "KEY123", TeamID: "TEAM456", Topic: "org.arenzana.correspondent"})
	if err != nil {
		t.Fatal(err)
	}
	c.HTTP = srv.Client()
	c.ProductionHost, c.SandboxHost = srv.URL, srv.URL+"/sandbox"

	r, err := c.Send(context.Background(), "abc", "production", Notification{Title: "Six Colors", Body: "A new post", Collapse: "feed-7", Entry: "17930", Feed: 7})
	if r != Sent || err != nil {
		t.Fatalf("send: %v %v", r, err)
	}
	if got.path != "/3/device/abc" || got.topic != "org.arenzana.correspondent" || got.collapse != "feed-7" {
		t.Errorf("request: %+v", got)
	}
	aps := got.body["aps"].(map[string]any)
	if aps["alert"].(map[string]any)["title"] != "Six Colors" || got.body["entry"] != "17930" || aps["thread-id"] != "feed-7" {
		t.Errorf("payload: %v", got.body)
	}
	// The JWT is ES256 over header.claims, with the key ID and team ID.
	parts := strings.Split(strings.TrimPrefix(got.auth, "bearer "), ".")
	head, _ := base64.RawURLEncoding.DecodeString(parts[0])
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !strings.Contains(string(head), `"kid":"KEY123"`) || !strings.Contains(string(claims), `"iss":"TEAM456"`) ||
		!ecdsa.Verify(&key.PublicKey, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Errorf("jwt: %s %s", head, claims)
	}

	if _, _ = c.Send(context.Background(), "abc", "sandbox", Notification{}); !strings.HasPrefix(got.path, "/sandbox/3/device/") {
		t.Errorf("sandbox host: %s", got.path)
	}
	for device, want := range map[string]Result{"gone": Gone, "bad": Gone, "busy": Failed} {
		if r, _ := c.Send(context.Background(), device, "production", Notification{}); r != want {
			t.Errorf("%s: %v, want %v", device, r, want)
		}
	}
	if _, err := New(Config{Key: []byte("nope"), KeyID: "k", TeamID: "t", Topic: "x"}); err == nil {
		t.Error("a bad key was accepted")
	}
}

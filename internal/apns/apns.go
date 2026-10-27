// Package apns sends notifications to the Apple apps through Apple's Push
// Notification service, with token-based authentication: an APNs key (.p8,
// from the developer account), its key ID and the team ID sign a short
// JWT that is reused for 40 minutes.
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// Hosts for the two APNs environments: development builds register in the
// sandbox, TestFlight and App Store builds in production.
const (
	Production = "https://api.push.apple.com"
	Sandbox    = "https://api.sandbox.push.apple.com"
)

type Config struct {
	Key    []byte // the .p8 file (PKCS #8, P-256)
	KeyID  string
	TeamID string
	Topic  string // the app's bundle ID
}

type Client struct {
	cfg  Config
	key  *ecdsa.PrivateKey
	HTTP *http.Client
	// Hosts, replaceable in tests.
	ProductionHost, SandboxHost string

	mu     sync.Mutex
	jwt    string
	issued time.Time
}

func New(cfg Config) (*Client, error) {
	block, _ := pem.Decode(cfg.Key)
	if block == nil {
		return nil, errors.New("apns: the key isn't a PEM file (.p8)")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: %w", err)
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns: the key isn't an EC key")
	}
	if cfg.KeyID == "" || cfg.TeamID == "" || cfg.Topic == "" {
		return nil, errors.New("apns: key ID, team ID and topic are required")
	}
	return &Client{cfg: cfg, key: ec, HTTP: &http.Client{Timeout: 15 * time.Second},
		ProductionHost: Production, SandboxHost: Sandbox}, nil
}

// token is the provider JWT, renewed every 40 minutes (Apple accepts up to an hour).
func (c *Client) token() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.jwt != "" && time.Since(c.issued) < 40*time.Minute {
		return c.jwt, nil
	}
	enc := base64.RawURLEncoding.EncodeToString
	head, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": c.cfg.KeyID})
	claims, _ := json.Marshal(map[string]any{"iss": c.cfg.TeamID, "iat": time.Now().Unix()})
	signing := enc(head) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, c.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := append(pad(r), pad(s)...)
	c.jwt, c.issued = signing+"."+enc(sig), time.Now()
	return c.jwt, nil
}

func pad(n *big.Int) []byte {
	b := n.Bytes()
	return append(make([]byte, 32-len(b)), b...)
}

// Notification is what a device shows: title and body, grouped and
// replaced per Collapse (a feed), and the article to open on a tap.
type Notification struct {
	Title, Body string
	Collapse    string // up to 64 bytes: a newer one replaces the last
	Entry       string // the article to open, if one
	Feed        int64
}

// Result says what to do with the device after sending.
type Result int

const (
	Sent   Result = iota
	Gone          // the app was removed or the token is invalid: forget it
	Failed        // try again with the next notification
)

// Send delivers one notification to one device. environment is
// "production" or "sandbox".
func (c *Client) Send(ctx context.Context, device, environment string, n Notification) (Result, error) {
	jwt, err := c.token()
	if err != nil {
		return Failed, err
	}
	payload := map[string]any{
		"aps": map[string]any{
			"alert":     map[string]string{"title": n.Title, "body": n.Body},
			"sound":     "default",
			"thread-id": n.Collapse,
		},
		"feed": n.Feed,
	}
	if n.Entry != "" {
		payload["entry"] = n.Entry
	}
	body, _ := json.Marshal(payload)
	host := c.ProductionHost
	if environment == "sandbox" {
		host = c.SandboxHost
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/3/device/"+device, bytes.NewReader(body))
	if err != nil {
		return Failed, err
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", c.cfg.Topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("apns-expiration", fmt.Sprint(time.Now().Add(24*time.Hour).Unix()))
	if n.Collapse != "" {
		req.Header.Set("apns-collapse-id", n.Collapse[:min(len(n.Collapse), 64)])
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Failed, err
	}
	defer resp.Body.Close()
	var out struct{ Reason string }
	json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out)
	switch {
	case resp.StatusCode == http.StatusOK:
		return Sent, nil
	case resp.StatusCode == http.StatusGone,
		resp.StatusCode == http.StatusBadRequest && (out.Reason == "BadDeviceToken" || out.Reason == "DeviceTokenNotForTopic"):
		return Gone, nil
	default:
		return Failed, fmt.Errorf("apns: %d %s", resp.StatusCode, out.Reason)
	}
}

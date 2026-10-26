// Package fetch is the only way Correspondent reaches the network. It
// enforces an HTTP(S)-only policy, refuses private, loopback, link-local and
// metadata destinations on every resolved connection (including redirects),
// bounds response size, and performs conditional GETs.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"
	"time"
)

const (
	DefaultMaxBytes = 8 << 20
	userAgent       = "Correspondent/0.1 (+self-hosted feed reader)"
)

var ErrBlocked = errors.New("destination address is not allowed")

type Client struct {
	http     *http.Client
	maxBytes int64
}

type Options struct {
	// AllowPrivate disables the destination check; only for tests and for
	// explicitly trusted local development.
	AllowPrivate bool
	MaxBytes     int64
	Timeout      time.Duration
}

func New(o Options) *Client {
	if o.MaxBytes == 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	if o.Timeout == 0 {
		o.Timeout = 30 * time.Second
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !o.AllowPrivate {
		// Control runs after DNS resolution, on the actual address being
		// dialled, so DNS rebinding cannot slip a private address through.
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if !Allowed(ip) {
				return fmt.Errorf("%w: %s", ErrBlocked, ip)
			}
			return nil
		}
	}
	transport := &http.Transport{
		Proxy:                 nil, // never route feed fetches through an ambient proxy
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}
	return &Client{
		maxBytes: o.MaxBytes,
		http: &http.Client{
			Transport: transport,
			Timeout:   o.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return checkScheme(req.URL)
			},
		},
	}
}

// Allowed reports whether ip is a public unicast address.
func Allowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT, also Tailscale
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can reach IPv4 private space
	netip.MustParsePrefix("2001:db8::/32"),
}

func checkScheme(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.User != nil {
		return errors.New("credentials in URL are not supported")
	}
	return nil
}

// Request describes one conditional fetch.
type Request struct {
	URL          string
	ETag         string
	LastModified string
}

type Response struct {
	Status       int
	NotModified  bool
	Body         []byte
	ContentType  string
	FinalURL     string
	ETag         string
	LastModified string
	RetryAfter   time.Duration
}

// Get fetches req.URL. A non-2xx/304 status is returned as a Response with
// an error so callers can record it.
func (c *Client) Get(ctx context.Context, req Request) (*Response, error) {
	u, err := url.Parse(req.URL)
	if err != nil {
		return nil, err
	}
	if err := checkScheme(u); err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("User-Agent", userAgent)
	hreq.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/feed+json, application/json;q=0.9, application/xml;q=0.9, text/xml;q=0.8, text/html;q=0.7, */*;q=0.5")
	if req.ETag != "" {
		hreq.Header.Set("If-None-Match", req.ETag)
	}
	if req.LastModified != "" {
		hreq.Header.Set("If-Modified-Since", req.LastModified)
	}
	resp, err := c.http.Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out := &Response{
		Status:       resp.StatusCode,
		ContentType:  resp.Header.Get("Content-Type"),
		FinalURL:     resp.Request.URL.String(),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		RetryAfter:   parseRetryAfter(resp.Header.Get("Retry-After")),
	}
	switch {
	case resp.StatusCode == http.StatusNotModified:
		out.NotModified = true
		return out, nil
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return out, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return out, err
	}
	if int64(len(body)) > c.maxBytes {
		return out, fmt.Errorf("response larger than %d bytes", c.maxBytes)
	}
	out.Body = body
	return out, nil
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

// Transport returns the guarded transport, for other clients (such as an AI
// provider at a reader-chosen address) that must obey the same address rules.
func (c *Client) Transport() http.RoundTripper { return c.http.Transport }

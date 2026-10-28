// Package ratelimit throttles failed sign-ins per client, and resolves the
// real client address when running behind a trusted reverse proxy.
package ratelimit

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Limiter allows Max failures per key within Window.
type Limiter struct {
	Max    int
	Window time.Duration

	mu       sync.Mutex
	failures map[string][]time.Time
	lastSwep time.Time
}

func New(max int, window time.Duration) *Limiter {
	return &Limiter{Max: max, Window: window, failures: map[string][]time.Time{}}
}

// Allow reports whether key may attempt another sign-in.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.sweep(now)
	return len(l.recent(key, now)) < l.Max
}

// Attempt reports whether key may try to sign in, and counts the try at
// once, before the slow password check: checking with Allow and counting
// with Fail afterwards lets a burst of parallel guesses all through.
// Forgive takes the count back when the sign-in succeeds.
func (l *Limiter) Attempt(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.sweep(now)
	recent := l.recent(key, now)
	if len(recent) >= l.Max {
		return false
	}
	l.failures[key] = append(recent, now)
	return true
}

// Forgive takes back the latest attempt counted for key.
func (l *Limiter) Forgive(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := len(l.failures[key]); n > 1 {
		l.failures[key] = l.failures[key][:n-1]
	} else {
		delete(l.failures, key)
	}
}

// Fail records a failed attempt.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.failures[key] = append(l.recent(key, now), now)
}

func (l *Limiter) recent(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.Window)
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = kept
	return kept
}

// sweep drops expired keys so memory stays bounded.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSwep) < l.Window {
		return
	}
	l.lastSwep = now
	for k := range l.failures {
		l.recent(k, now)
	}
}

// ClientIP returns the client address. X-Forwarded-For is honoured only when
// the direct peer is a trusted proxy, and then the right-most untrusted hop
// is used, so clients cannot spoof their address.
type ClientIP struct {
	Trusted []netip.Prefix
}

func (c ClientIP) Of(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !c.trusted(peer) {
		return host
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !c.trusted(ip) {
			return ip.String()
		}
	}
	return host
}

func (c ClientIP) trusted(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range c.Trusted {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

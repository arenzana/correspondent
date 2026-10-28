package ratelimit

import (
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := New(3, time.Minute)
	for range 3 {
		if !l.Allow("a") {
			t.Fatal("blocked too early")
		}
		l.Fail("a")
	}
	if l.Allow("a") {
		t.Fatal("not blocked after max failures")
	}
	if !l.Allow("b") {
		t.Fatal("other keys must not be affected")
	}
}

func TestClientIP(t *testing.T) {
	c := ClientIP{Trusted: []netip.Prefix{netip.MustParsePrefix("10.42.0.0/16")}}
	cases := []struct{ remote, xff, want string }{
		{"203.0.113.9:1234", "1.2.3.4", "203.0.113.9"},                // untrusted peer: header ignored
		{"10.42.0.7:1234", "198.51.100.2", "198.51.100.2"},            // trusted proxy
		{"10.42.0.7:1234", "6.6.6.6, 198.51.100.2", "198.51.100.2"},   // spoofed left-most hop ignored
		{"10.42.0.7:1234", "198.51.100.2, 10.42.0.3", "198.51.100.2"}, // chain of trusted proxies
		{"10.42.0.7:1234", "", "10.42.0.7"},                           // no header
		{"10.42.0.7:1234", "garbage", "10.42.0.7"},                    // malformed header
	}
	for _, tc := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := c.Of(r); got != tc.want {
			t.Errorf("remote %s xff %q: got %s, want %s", tc.remote, tc.xff, got, tc.want)
		}
	}
}

// A burst of parallel sign-ins gets only Max tries between them, not one
// each: the try is counted when it's let in, not after the password check.
func TestAttemptCountsBeforeTheCheck(t *testing.T) {
	l := New(10, time.Minute)
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 500 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Attempt("1.2.3.4") {
				admitted.Add(1)
				time.Sleep(10 * time.Millisecond) // the password check
			}
		}()
	}
	wg.Wait()
	if n := admitted.Load(); n != 10 {
		t.Fatalf("admitted %d of a parallel burst, want 10", n)
	}

	// A successful sign-in takes its try back.
	l2 := New(2, time.Minute)
	for range 5 {
		if !l2.Attempt("k") {
			t.Fatal("successful sign-ins used up the allowance")
		}
		l2.Forgive("k")
	}
}

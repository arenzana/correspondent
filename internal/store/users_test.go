package store

import (
	"context"
	"testing"
	"time"
)

func TestPasswordsAreSeparate(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _ := st.CreateUser(ctx, "u", "web-password-1")
	if _, err := st.CheckPassword(ctx, "u", "web-password-1", true); err == nil {
		t.Fatal("API login allowed before an API password was set")
	}
	st.SetAPIPassword(ctx, uid, "api-password-1")
	cases := []struct {
		pw  string
		api bool
		ok  bool
	}{
		{"web-password-1", false, true},
		{"api-password-1", true, true},
		{"api-password-1", false, false}, // API password never signs in to the web
		{"web-password-1", true, false},  // and vice versa
		{"wrong", false, false},
	}
	for _, c := range cases {
		_, err := st.CheckPassword(ctx, "u", c.pw, c.api)
		if (err == nil) != c.ok {
			t.Errorf("CheckPassword(%q, api=%v) ok=%v", c.pw, c.api, err == nil)
		}
	}
	if _, err := st.CheckPassword(ctx, "nobody", "x", false); err != ErrNotFound {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestSessions(t *testing.T) {
	st, ctx := open(t), context.Background()
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	token, err := st.CreateSession(ctx, uid, "web", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.SessionUser(ctx, token, "web"); err != nil || got != uid {
		t.Fatalf("session user %d %v", got, err)
	}
	if _, err := st.SessionUser(ctx, token, "greader"); err == nil {
		t.Fatal("web session accepted as an API session")
	}
	// Only the hash is stored.
	var n int
	st.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, token).Scan(&n)
	if n != 0 {
		t.Fatal("raw token stored")
	}
	st.DeleteSession(ctx, token)
	if _, err := st.SessionUser(ctx, token, "web"); err == nil {
		t.Fatal("deleted session still valid")
	}

	expired, _ := st.CreateSession(ctx, uid, "web", -time.Minute)
	if _, err := st.SessionUser(ctx, expired, "web"); err == nil {
		t.Fatal("expired session accepted")
	}
	if n, _ := st.PurgeSessions(ctx); n != 1 {
		t.Fatalf("purged %d, want 1", n)
	}
}

func TestBcryptLengthLimit(t *testing.T) {
	st, ctx := open(t), context.Background()
	long := make([]byte, 100)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := st.CreateUser(ctx, "u", string(long)); err == nil {
		t.Fatal("password over bcrypt's 72-byte limit accepted (it would be silently truncated)")
	}
}

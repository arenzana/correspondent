package store

import (
	"context"
	"errors"
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

// Rotating a credential signs out what was signed in with it: a new app
// password ends Reeder sessions; a reset ends everything, tokens included.
func TestRotationSignsOut(t *testing.T) {
	st, ctx := open(t), context.Background()
	admin, _ := st.CreateUser(ctx, "ada", "password-ada-1")
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	web, _ := st.CreateSession(ctx, uid, "web", time.Hour)
	reeder, _ := st.CreateSession(ctx, uid, "greader", time.Hour)
	token, _, _ := st.CreateToken(ctx, uid, "corr", "write")

	if err := st.SetAPIPassword(ctx, uid, "new-app-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionUser(ctx, reeder, "greader"); err == nil {
		t.Error("Reeder session survived a new app password")
	}
	if _, err := st.SessionUser(ctx, web, "web"); err != nil {
		t.Error("a new app password signed out the web")
	}

	reeder, _ = st.CreateSession(ctx, uid, "greader", time.Hour)
	link, err := st.CreateInvite(ctx, InviteReset, uid, RoleUser, "", admin, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcceptInvite(ctx, link, "", "password-456"); err != nil {
		t.Fatal(err)
	}
	for kind, tok := range map[string]string{"web": web, "greader": reeder} {
		if _, err := st.SessionUser(ctx, tok, kind); err == nil {
			t.Errorf("%s session survived a reset", kind)
		}
	}
	if _, _, err := st.TokenUser(ctx, token); err == nil {
		t.Error("API token survived a reset")
	}
}

// A feed can only be filed in its own reader's folders.
func TestFoldersAreOwnersOnly(t *testing.T) {
	st, ctx := open(t), context.Background()
	ada, _ := st.CreateUser(ctx, "ada", "password-ada-1")
	bob, _ := st.CreateUser(ctx, "bob", "password-bob-1")
	folder, err := st.EnsureFolder(ctx, ada, "News")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddFeed(ctx, bob, "https://example.org/feed", "", &folder); !errors.Is(err, ErrNotFound) {
		t.Errorf("subscribe into another reader's folder: %v", err)
	}
	if _, err := st.AddPageFeed(ctx, bob, "https://example.org/page", "", &folder, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("page feed into another reader's folder: %v", err)
	}
	if _, err := st.AddFeed(ctx, ada, "https://example.org/feed", "", &folder); err != nil {
		t.Errorf("subscribe into own folder: %v", err)
	}
}

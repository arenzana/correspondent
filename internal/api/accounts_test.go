package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/arenzana/correspondent/internal/store"
)

func signIn(t *testing.T, e *env, name, password string) (*http.Client, int) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	r := e.do(t, c, "POST", "/api/v1/session", fmt.Sprintf(`{"username":%q,"password":%q}`, name, password), true)
	return c, r.StatusCode
}

func body(t *testing.T, r *http.Response) string {
	t.Helper()
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

func TestInvitesAndAdmin(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.user(t, "ada") // first account: admin
	bob, _ := e.user(t, "bob")
	ctx := context.Background()

	var me struct{ Role string }
	json.NewDecoder(e.do(t, admin, "GET", "/api/v1/me", "", false).Body).Decode(&me)
	if me.Role != "admin" {
		t.Fatalf("first account role %q", me.Role)
	}

	// Admin endpoints refuse readers, checked on every request.
	for _, req := range [][2]string{
		{"GET", "/api/v1/admin/accounts"}, {"GET", "/api/v1/admin/server"}, {"GET", "/api/v1/admin/parsers"},
		{"POST", "/api/v1/admin/invites"}, {"DELETE", "/api/v1/admin/accounts/1"}, {"POST", "/api/v1/admin/accounts/1/reset"},
	} {
		if r := e.do(t, bob, req[0], req[1], `{}`, true); r.StatusCode != http.StatusForbidden {
			t.Errorf("reader %s %s: %d", req[0], req[1], r.StatusCode)
		}
	}

	// An invitation: a one-time link to choose a username and password.
	r := e.do(t, admin, "POST", "/api/v1/admin/invites", `{"role":"user","note":"for Carol"}`, true)
	var inv struct{ Token string }
	json.NewDecoder(r.Body).Decode(&inv)
	if inv.Token == "" {
		t.Fatalf("invite: %d", r.StatusCode)
	}
	anon := &http.Client{}
	if r := e.do(t, anon, "GET", "/api/v1/invites/"+inv.Token, "", false); r.StatusCode != 200 || !strings.Contains(body(t, r), `"invitedBy":"ada"`) {
		t.Errorf("invite details: %d", r.StatusCode)
	}
	jar, _ := cookiejar.New(nil)
	carol := &http.Client{Jar: jar}
	if r := e.do(t, carol, "POST", "/api/v1/invites/"+inv.Token, `{"username":"carol","password":"short"}`, true); r.StatusCode != 400 {
		t.Errorf("short password: %d", r.StatusCode)
	}
	if r := e.do(t, carol, "POST", "/api/v1/invites/"+inv.Token, `{"username":"bob","password":"password-carol-1"}`, true); r.StatusCode != http.StatusConflict {
		t.Errorf("taken name: %d", r.StatusCode)
	}
	if r := e.do(t, carol, "POST", "/api/v1/invites/"+inv.Token, `{"username":"carol","password":"password-carol-1"}`, true); r.StatusCode != 200 {
		t.Fatalf("accept: %d %s", r.StatusCode, body(t, r))
	}
	// Signed in at once, as a reader.
	if r := e.do(t, carol, "GET", "/api/v1/me", "", false); !strings.Contains(body(t, r), `"role":"user"`) {
		t.Error("new account not signed in as a reader")
	}
	if r := e.do(t, anon, "POST", "/api/v1/invites/"+inv.Token, `{"username":"carol2","password":"password-carol-1"}`, true); r.StatusCode != 404 {
		t.Errorf("invite reused: %d", r.StatusCode)
	}

	var list struct {
		Accounts []store.Account
		Invites  []store.Invite
	}
	json.NewDecoder(e.do(t, admin, "GET", "/api/v1/admin/accounts", "", false).Body).Decode(&list)
	if len(list.Accounts) != 3 || len(list.Invites) != 0 {
		t.Fatalf("%+v", list)
	}
	carolID, _ := e.store.UserIDByName(ctx, "carol")
	bobID, _ := e.store.UserIDByName(ctx, "bob")
	adaID, _ := e.store.UserIDByName(ctx, "ada")

	// Disabling signs the person out everywhere and stops sign-in.
	if r := e.do(t, admin, "PATCH", fmt.Sprintf("/api/v1/admin/accounts/%d", carolID), `{"disabled":true}`, true); r.StatusCode != 204 {
		t.Fatalf("disable: %d", r.StatusCode)
	}
	if r := e.do(t, carol, "GET", "/api/v1/me", "", false); r.StatusCode != 401 {
		t.Errorf("disabled session still works: %d", r.StatusCode)
	}
	if _, code := signIn(t, e, "carol", "password-carol-1"); code != http.StatusForbidden {
		t.Errorf("disabled sign-in: %d", code)
	}
	if _, code := signIn(t, e, "carol", "wrong-password-1"); code != http.StatusUnauthorized {
		t.Errorf("a wrong password must not reveal a disabled account: %d", code)
	}
	e.do(t, admin, "PATCH", fmt.Sprintf("/api/v1/admin/accounts/%d", carolID), `{"disabled":false}`, true)

	// Admins can't lock themselves out.
	if r := e.do(t, admin, "PATCH", fmt.Sprintf("/api/v1/admin/accounts/%d", adaID), `{"disabled":true}`, true); r.StatusCode != 400 {
		t.Errorf("self-disable: %d", r.StatusCode)
	}
	if r := e.do(t, admin, "PATCH", fmt.Sprintf("/api/v1/admin/accounts/%d", adaID), `{"role":"user"}`, true); r.StatusCode != http.StatusConflict {
		t.Errorf("demote last admin: %d", r.StatusCode)
	}
	if r := e.do(t, admin, "DELETE", fmt.Sprintf("/api/v1/admin/accounts/%d", adaID), "", true); r.StatusCode != 400 {
		t.Errorf("self-delete: %d", r.StatusCode)
	}

	// A reset link lets the person choose a new password; the admin never does.
	r = e.do(t, admin, "POST", fmt.Sprintf("/api/v1/admin/accounts/%d/reset", bobID), "", true)
	var reset struct{ Token string }
	json.NewDecoder(r.Body).Decode(&reset)
	if r := e.do(t, anon, "GET", "/api/v1/invites/"+reset.Token, "", false); !strings.Contains(body(t, r), `"username":"bob"`) {
		t.Error("reset link doesn't name the account")
	}
	if r := e.do(t, anon, "POST", "/api/v1/invites/"+reset.Token, `{"password":"password-bob-new"}`, true); r.StatusCode != 200 {
		t.Fatalf("reset: %d", r.StatusCode)
	}
	if r := e.do(t, bob, "GET", "/api/v1/me", "", false); r.StatusCode != 401 {
		t.Error("old session survived a reset")
	}
	if _, code := signIn(t, e, "bob", "password-bob-new"); code != 200 {
		t.Errorf("new password: %d", code)
	}

	// Deleting an account removes it and its data.
	if r := e.do(t, admin, "DELETE", fmt.Sprintf("/api/v1/admin/accounts/%d", carolID), "", true); r.StatusCode != 204 {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if _, code := signIn(t, e, "carol", "password-carol-1"); code != 401 {
		t.Errorf("deleted account signs in: %d", code)
	}

	// The server view.
	var srv struct {
		Version string
		Stats   store.ServerStats
	}
	json.NewDecoder(e.do(t, admin, "GET", "/api/v1/admin/server", "", false).Body).Decode(&srv)
	if srv.Stats.Accounts != 2 || srv.Stats.Feeds != 2 {
		t.Errorf("%+v", srv)
	}
}

func TestOwnAccount(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user(t, "alice")
	other, _ := signIn(t, e, "alice", "password-alice")
	var s struct{ Sessions []store.Session }
	json.NewDecoder(e.do(t, alice, "GET", "/api/v1/sessions", "", false).Body).Decode(&s)
	if len(s.Sessions) != 2 {
		t.Fatalf("%+v", s)
	}
	var otherID int64
	for _, x := range s.Sessions {
		if !x.Current {
			otherID = x.ID
		}
	}
	if r := e.do(t, alice, "DELETE", fmt.Sprintf("/api/v1/sessions/%d", otherID), "", true); r.StatusCode != 204 {
		t.Fatalf("end session: %d", r.StatusCode)
	}
	if r := e.do(t, other, "GET", "/api/v1/me", "", false); r.StatusCode != 401 {
		t.Error("ended session works")
	}

	if r := e.do(t, alice, "PUT", "/api/v1/password", `{"current":"wrong","new":"a-new-password-1"}`, true); r.StatusCode != 400 {
		t.Errorf("wrong current password: %d", r.StatusCode)
	}
	if r := e.do(t, alice, "PUT", "/api/v1/password", `{"current":"password-alice","new":"short"}`, true); r.StatusCode != 400 {
		t.Errorf("short password: %d", r.StatusCode)
	}
	if r := e.do(t, alice, "PUT", "/api/v1/password", `{"current":"password-alice","new":"a-new-password-1"}`, true); r.StatusCode != 204 {
		t.Fatalf("change: %d", r.StatusCode)
	}
	if r := e.do(t, alice, "GET", "/api/v1/me", "", false); r.StatusCode != 200 {
		t.Error("changing the password signed this device out")
	}
	if _, code := signIn(t, e, "alice", "a-new-password-1"); code != 200 {
		t.Errorf("new password: %d", code)
	}
}

func TestPasskeyEndpoints(t *testing.T) {
	e := newEnv(t)
	alice, _ := e.user(t, "alice")
	anon := &http.Client{}
	// Off without a public address.
	if r := e.do(t, anon, "POST", "/api/v1/session/passkey", "", true); r.StatusCode != http.StatusNotImplemented {
		t.Errorf("passkeys without setup: %d", r.StatusCode)
	}
	wa, err := NewWebAuthn("https://rss.example.org")
	if err != nil {
		t.Fatal(err)
	}
	e.api.WebAuthn = wa
	for _, bad := range []string{"http://rss.example.org", "rss.example.org", ""} {
		if _, err := NewWebAuthn(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}

	// Sign-in starts without a username; finishing needs that challenge.
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	r := e.do(t, c, "POST", "/api/v1/session/passkey", "", true)
	var opts struct {
		PublicKey struct {
			Challenge string
			RpID      string
		}
	}
	json.NewDecoder(r.Body).Decode(&opts)
	if r.StatusCode != 200 || opts.PublicKey.Challenge == "" || opts.PublicKey.RpID != "rss.example.org" {
		t.Fatalf("begin: %d %+v", r.StatusCode, opts)
	}
	if r := e.do(t, anon, "POST", "/api/v1/session/passkey/finish", `{}`, true); r.StatusCode != 400 {
		t.Errorf("finish without a challenge: %d", r.StatusCode)
	}
	if r := e.do(t, c, "POST", "/api/v1/session/passkey/finish", `{"id":"x","rawId":"x","type":"public-key","response":{}}`, true); r.StatusCode != 401 {
		t.Errorf("forged assertion: %d", r.StatusCode)
	}
	// The challenge was used up by that attempt.
	if r := e.do(t, c, "POST", "/api/v1/session/passkey/finish", `{}`, true); r.StatusCode != 400 {
		t.Errorf("challenge replayed: %d", r.StatusCode)
	}

	// Registration needs a session and offers the account's random handle.
	if r := e.do(t, anon, "POST", "/api/v1/passkeys", "", true); r.StatusCode != 401 {
		t.Errorf("register signed out: %d", r.StatusCode)
	}
	r = e.do(t, alice, "POST", "/api/v1/passkeys", "", true)
	var reg struct {
		PublicKey struct {
			User                   struct{ ID, Name string }
			AuthenticatorSelection struct{ ResidentKey string }
		}
	}
	json.NewDecoder(r.Body).Decode(&reg)
	if r.StatusCode != 200 || reg.PublicKey.User.Name != "alice" || reg.PublicKey.User.ID == "" ||
		reg.PublicKey.User.ID == "alice" || reg.PublicKey.AuthenticatorSelection.ResidentKey != "required" {
		t.Fatalf("register: %d %+v", r.StatusCode, reg)
	}

	// Listing, renaming and removing are per account.
	ctx := context.Background()
	uid, _ := e.store.UserIDByName(ctx, "alice")
	id, _ := e.store.AddPasskey(ctx, uid, []byte("cred-1"), []byte(`{}`), "  ")
	bob, _ := e.user(t, "bob")
	if r := e.do(t, bob, "PATCH", fmt.Sprintf("/api/v1/passkeys/%d", id), `{"name":"mine"}`, true); r.StatusCode != 404 {
		t.Errorf("renamed another's passkey: %d", r.StatusCode)
	}
	e.do(t, alice, "PATCH", fmt.Sprintf("/api/v1/passkeys/%d", id), `{"name":"Work laptop"}`, true)
	var keys struct{ Passkeys []store.Passkey }
	json.NewDecoder(e.do(t, alice, "GET", "/api/v1/passkeys", "", false).Body).Decode(&keys)
	if len(keys.Passkeys) != 1 || keys.Passkeys[0].Name != "Work laptop" {
		t.Fatalf("%+v", keys)
	}
	if raw := body(t, e.do(t, alice, "GET", "/api/v1/passkeys", "", false)); strings.Contains(raw, "credential") {
		t.Errorf("credential material listed: %s", raw)
	}
	if r := e.do(t, alice, "DELETE", fmt.Sprintf("/api/v1/passkeys/%d", id), "", true); r.StatusCode != 204 {
		t.Errorf("delete: %d", r.StatusCode)
	}
}

func TestClientName(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1": "Safari on iPhone",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36":                       "Chrome on Mac",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0":                                                        "Firefox on Windows",
		"curl/8.0": "Browser",
	} {
		if got := clientName(ua); got != want {
			t.Errorf("%s → %s", want, got)
		}
	}
}

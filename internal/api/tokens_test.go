package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func bearer(t *testing.T, e *env, token, method, path, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAPITokens(t *testing.T) {
	e := newEnv(t)
	alice, entry := e.user(t, "alice")
	anon := &http.Client{}

	// corr auth login: a password for a token, throttled like sign-in.
	r := e.do(t, anon, "POST", "/api/v1/tokens/login", `{"username":"alice","password":"nope","name":"corr"}`, false)
	if r.StatusCode != 401 {
		t.Fatalf("bad password: %d", r.StatusCode)
	}
	r = e.do(t, anon, "POST", "/api/v1/tokens/login", `{"username":"alice","password":"password-alice","name":"corr on laptop"}`, false)
	var login struct {
		Token string
		Info  struct{ Scope string }
	}
	json.NewDecoder(r.Body).Decode(&login)
	if !strings.HasPrefix(login.Token, "corr_") || login.Info.Scope != "write" {
		t.Fatalf("login: %d %+v", r.StatusCode, login)
	}

	// A write token reads and changes, without any CSRF header.
	if r := bearer(t, e, login.Token, "GET", "/api/v1/tree", ""); r.StatusCode != 200 {
		t.Errorf("read: %d", r.StatusCode)
	}
	if r := bearer(t, e, login.Token, "POST", "/api/v1/entries/state", fmt.Sprintf(`{"action":"star","ids":[%q]}`, entry)); r.StatusCode != 200 {
		t.Errorf("write: %d", r.StatusCode)
	}

	// A read-only token, made in the app, can't change anything.
	r = e.do(t, alice, "POST", "/api/v1/tokens", `{"name":"agent","scope":"read"}`, true)
	var made struct{ Token string }
	json.NewDecoder(r.Body).Decode(&made)
	if r := bearer(t, e, made.Token, "GET", "/api/v1/entries?view=all", ""); r.StatusCode != 200 {
		t.Errorf("read-only read: %d", r.StatusCode)
	}
	if r := bearer(t, e, made.Token, "POST", "/api/v1/entries/state", fmt.Sprintf(`{"action":"read","ids":[%q]}`, entry)); r.StatusCode != 403 {
		t.Errorf("read-only write: %d", r.StatusCode)
	}

	// No token, however scoped, changes the account itself.
	for _, req := range [][3]string{
		{"PUT", "/api/v1/password", `{"current":"password-alice","new":"stolen-password-1"}`},
		{"POST", "/api/v1/tokens", `{"name":"more","scope":"write"}`},
		{"PUT", "/api/v1/ai", `{"provider":"anthropic","key":"x"}`},
		{"PUT", "/api/v1/api-password", `{"password":"stolen-password-1"}`},
		{"POST", "/api/v1/passkeys", ``},
	} {
		if r := bearer(t, e, login.Token, req[0], req[1], req[2]); r.StatusCode != 403 {
			t.Errorf("token %s %s: %d", req[0], req[1], r.StatusCode)
		}
	}

	// Listed (never the secret), revoked from the app or by itself.
	r = e.do(t, alice, "GET", "/api/v1/tokens", "", false)
	raw := body(t, r)
	if !strings.Contains(raw, `"corr on laptop"`) || !strings.Contains(raw, `"agent"`) || strings.Contains(raw, "corr_") {
		t.Errorf("list: %s", raw)
	}
	if r := bearer(t, e, made.Token, "DELETE", "/api/v1/tokens/current", ""); r.StatusCode != 403 {
		t.Errorf("a read-only token revoked itself with DELETE: %d", r.StatusCode)
	}
	if r := bearer(t, e, login.Token, "DELETE", "/api/v1/tokens/current", ""); r.StatusCode != 204 {
		t.Errorf("logout: %d", r.StatusCode)
	}
	if r := bearer(t, e, login.Token, "GET", "/api/v1/tree", ""); r.StatusCode != 401 {
		t.Errorf("revoked token works: %d", r.StatusCode)
	}

	// Disabling the account stops its tokens.
	ctx := context.Background()
	uid, _ := e.store.UserIDByName(ctx, "alice")
	e.user(t, "bob")
	e.store.CreateUser(ctx, "admin2", "password-admin2")
	e.store.SetRole(ctx, uid+2, "admin")
	if err := e.store.SetDisabled(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if r := bearer(t, e, made.Token, "GET", "/api/v1/tree", ""); r.StatusCode != 401 {
		t.Errorf("disabled account's token: %d", r.StatusCode)
	}
	if r := bearer(t, e, "corr_forged", "GET", "/api/v1/tree", ""); r.StatusCode != 401 {
		t.Errorf("forged token: %d", r.StatusCode)
	}
	if r := bearer(t, e, "", "GET", "/api/v1/tree", ""); r.StatusCode != 401 {
		t.Errorf("empty token: %d", r.StatusCode)
	}
}

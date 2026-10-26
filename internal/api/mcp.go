package api

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arenzana/correspondent/internal/client"
	"github.com/arenzana/correspondent/internal/mcpserver"
	"github.com/arenzana/correspondent/internal/version"
)

// MCPHandler serves the Model Context Protocol at /mcp, for AI tools
// (Streamable HTTP, stateless, JSON responses). Callers present a personal
// API token, and every tool calls this API in-process with that same token,
// so an agent can do exactly what the token allows and nothing more.
func (a *API) MCPHandler() http.Handler {
	inner := a.Handler()
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		c := &client.Client{
			Server: "http://correspondent.internal",
			Token:  strings.TrimSpace(token),
			HTTP:   &http.Client{Transport: inProcess{h: inner, from: r}},
		}
		return mcpserver.New(c, a.baseURL(r), version.Version)
	}, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// The guard against DNS rebinding rejects requests that arrive on a
		// loopback address, which is every request behind a reverse proxy.
		// It isn't needed: /mcp only answers to a bearer token, which a page
		// in a browser can't send cross-site.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        1 << 20,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		ip := a.ClientIP.Of(r)
		if !a.Limiter.Allow(ip) {
			fail(w, http.StatusTooManyRequests, "Too many failed attempts. Wait a minute and try again.")
			return
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Correspondent"`)
			fail(w, http.StatusUnauthorized, "Connect with a personal API token: Authorization: Bearer corr_… (Settings → Account).")
			return
		}
		if _, _, err := a.Store.TokenUser(r.Context(), strings.TrimSpace(token)); err != nil {
			a.Limiter.Fail(ip)
			w.Header().Set("WWW-Authenticate", `Bearer realm="Correspondent", error="invalid_token"`)
			fail(w, http.StatusUnauthorized, "That token isn't valid: it was revoked, or the account is disabled.")
			return
		}
		h.ServeHTTP(w, r)
	})
}

// baseURL is the address readers use, for absolute links in articles.
func (a *API) baseURL(r *http.Request) string {
	if a.PublicURL != "" {
		return strings.TrimRight(a.PublicURL, "/") + "/"
	}
	scheme := "http"
	if a.Secure {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/"
}

// inProcess sends a tool's API calls straight to the API handler, as if
// from the MCP caller (same address, for rate limits).
type inProcess struct {
	h    http.Handler
	from *http.Request
}

func (t inProcess) RoundTrip(req *http.Request) (*http.Response, error) {
	req.RemoteAddr = t.from.RemoteAddr
	if xff := t.from.Header.Get("X-Forwarded-For"); xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

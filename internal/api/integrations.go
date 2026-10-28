package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/arenzana/correspondent/internal/integrations"
	"github.com/arenzana/correspondent/internal/store"
	"github.com/arenzana/correspondent/internal/textify"
)

// integrationView is an integration as its owner sees it: settings and a
// hint of the key, never the credentials.
type integrationView struct {
	store.Integration
	BaseURL string   `json:"baseUrl,omitempty"`
	Tags    []string `json:"tags"`
	KeyHint string   `json:"keyHint,omitempty"`
}

func viewOf(i store.Integration) integrationView {
	var cfg integrations.Config
	json.Unmarshal([]byte(i.Config), &cfg)
	if cfg.Tags == nil {
		cfg.Tags = []string{}
	}
	return integrationView{Integration: i, BaseURL: cfg.BaseURL, Tags: cfg.Tags, KeyHint: cfg.KeyHint}
}

func (a *API) integrationsOn(w http.ResponseWriter) bool {
	if a.Integrations == nil || a.Keyring == nil {
		fail(w, http.StatusNotImplemented, "Integrations aren't available on this server.")
		return false
	}
	return true
}

// listIntegrations answers with the reader's integrations and the kinds
// they can add.
func (a *API) listIntegrations(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.Integrations(r.Context(), userID(r))
	if err != nil {
		a.internal(w, err)
		return
	}
	views := make([]integrationView, len(list))
	for n, i := range list {
		views[n] = viewOf(i)
	}
	reply(w, map[string]any{"integrations": views, "kinds": integrations.Kinds,
		"available": a.Integrations != nil && a.Keyring != nil, "plainHttp": a.Integrations != nil && a.Integrations.PlainHTTP})
}

// integrationInput is what the settings form sends. Empty credentials
// keep the saved ones when editing.
type integrationInput struct {
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	BaseURL      string   `json:"baseUrl"`
	Tags         []string `json:"tags"`
	SendStarred  bool     `json:"sendStarred"`
	Token        string   `json:"token"`
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	ClientID     string   `json:"clientId"`
	ClientSecret string   `json:"clientSecret"`
	NewKey       bool     `json:"newKey"` // a webhook's new signing key
}

func (in integrationInput) secret() integrations.Secret {
	t := strings.TrimSpace
	return integrations.Secret{Token: t(in.Token), Username: t(in.Username), Password: in.Password,
		ClientID: t(in.ClientID), ClientSecret: t(in.ClientSecret)}
}

func (in integrationInput) hasSecret() bool {
	s := in.secret()
	return s.Token != "" || s.Username != "" || s.Password != "" || s.ClientID != "" || s.ClientSecret != ""
}

// checkIntegration normalizes the settings and checks the credentials
// with the service, which saves nothing (a webhook gets a ping). It
// answers the reader itself when something's wrong.
func (a *API) checkIntegration(w http.ResponseWriter, r *http.Request, kind string, cfg integrations.Config, sec integrations.Secret) (integrations.Config, bool) {
	cfg, err := cfg.Normalize(kind, a.Integrations.PlainHTTP)
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return cfg, false
	}
	if err := sec.Check(kind); err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return cfg, false
	}
	c, err := integrations.New(kind, cfg, sec, a.Integrations.HTTP)
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return cfg, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := c.Verify(ctx); err != nil {
		fail(w, http.StatusUnprocessableEntity, "That didn't work: "+lowerFirst(sentence(err.Error())))
		return cfg, false
	}
	if sec.Token != "" && len(sec.Token) > 8 {
		cfg.KeyHint = sec.Token[len(sec.Token)-4:]
	} else if sec.Username != "" {
		cfg.KeyHint = sec.Username
	}
	return cfg, true
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func (a *API) createIntegration(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w) {
		return
	}
	var in integrationInput
	if !decode(w, r, &in) {
		return
	}
	kind, ok := integrations.KindByID(in.Kind)
	if !ok {
		fail(w, http.StatusBadRequest, "Choose a kind of integration.")
		return
	}
	uid := userID(r)
	if list, err := a.Store.Integrations(r.Context(), uid); err == nil && len(list) >= 20 {
		fail(w, http.StatusBadRequest, "That's the most integrations one reader can have.")
		return
	}
	sec := in.secret()
	if kind.ID == integrations.Webhook {
		sec = integrations.Secret{SigningKey: integrations.NewSigningKey()}
	}
	cfg, ok := a.checkIntegration(w, r, kind.ID, integrations.Config{BaseURL: in.BaseURL, Tags: in.Tags}, sec)
	if !ok {
		return
	}
	sealed, err := a.Integrations.Seal(uid, sec)
	if err != nil {
		a.internal(w, err)
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = kind.Label
	}
	raw, _ := json.Marshal(cfg)
	i, err := a.Store.CreateIntegration(r.Context(), uid, kind.ID, name, string(raw), sealed, in.SendStarred)
	if err != nil {
		a.internal(w, err)
		return
	}
	out := map[string]any{"integration": viewOf(i)}
	if kind.ID == integrations.Webhook {
		out["signingKey"] = sec.SigningKey // shown this once
	}
	reply(w, out)
}

func (a *API) updateIntegration(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in integrationInput
	if !decode(w, r, &in) {
		return
	}
	uid := userID(r)
	i, err := a.Store.Integration(r.Context(), uid, id)
	if err != nil {
		a.result(w, err)
		return
	}
	var cfg integrations.Config
	json.Unmarshal([]byte(i.Config), &cfg)
	sealed, err := a.Store.IntegrationSecret(r.Context(), uid, id)
	if err != nil {
		a.internal(w, err)
		return
	}
	sec, err := a.Integrations.Open(uid, sealed)
	if err != nil {
		a.internal(w, err)
		return
	}
	changed := in.hasSecret() || in.NewKey || strings.TrimRight(strings.TrimSpace(in.BaseURL), "/") != cfg.BaseURL
	if i.Kind == integrations.Webhook && in.NewKey {
		sec.SigningKey = integrations.NewSigningKey()
	} else if in.hasSecret() {
		sec = in.secret()
	}
	next := integrations.Config{BaseURL: in.BaseURL, Tags: in.Tags, KeyHint: cfg.KeyHint}
	if changed {
		// A new address or credentials: check them with the service first.
		if next, ok = a.checkIntegration(w, r, i.Kind, next, sec); !ok {
			return
		}
		if !in.hasSecret() {
			next.KeyHint = cfg.KeyHint
		}
		if sealed, err = a.Integrations.Seal(uid, sec); err != nil {
			a.internal(w, err)
			return
		}
	} else {
		if next, err = next.Normalize(i.Kind, a.Integrations.PlainHTTP); err != nil {
			fail(w, http.StatusBadRequest, sentence(err.Error()))
			return
		}
		next.KeyHint, sealed = cfg.KeyHint, nil
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = i.Name
	}
	raw, _ := json.Marshal(next)
	if err := a.Store.UpdateIntegration(r.Context(), uid, id, name, string(raw), sealed, in.SendStarred); err != nil {
		a.result(w, err)
		return
	}
	i, _ = a.Store.Integration(r.Context(), uid, id)
	out := map[string]any{"integration": viewOf(*i)}
	if i.Kind == integrations.Webhook && in.NewKey {
		out["signingKey"] = sec.SigningKey
	}
	reply(w, out)
}

func (a *API) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a.result(w, a.Store.DeleteIntegration(r.Context(), userID(r), id))
}

// testIntegration checks saved credentials again, without saving anything.
func (a *API) testIntegration(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	uid := userID(r)
	i, err := a.Store.Integration(r.Context(), uid, id)
	if err != nil {
		a.result(w, err)
		return
	}
	c, err := a.Integrations.Connector(r.Context(), uid, i)
	if err != nil {
		a.internal(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := c.Verify(ctx); err != nil {
		a.Store.IntegrationResult(r.Context(), id, sentence(err.Error()))
		fail(w, http.StatusUnprocessableEntity, "That didn't work: "+lowerFirst(sentence(err.Error())))
		return
	}
	a.Store.IntegrationResult(r.Context(), id, "")
	w.WriteHeader(http.StatusNoContent)
}

// sendEntry queues an article for one of the reader's integrations; the
// worker sends it in a moment and retries if the service is down.
func (a *API) sendEntry(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Integration int64 `json:"integration,string"`
	}
	if !decode(w, r, &in) {
		return
	}
	d, err := a.Store.QueueDelivery(r.Context(), userID(r), in.Integration, id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "No such article or integration.")
		return
	} else if err != nil {
		a.internal(w, err)
		return
	}
	a.Integrations.Kick()
	reply(w, map[string]any{"delivery": d})
}

func (a *API) entryDeliveries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := a.Store.EntryDeliveries(r.Context(), userID(r), id)
	if err != nil {
		a.internal(w, err)
		return
	}
	if list == nil {
		list = []store.Delivery{}
	}
	reply(w, map[string]any{"deliveries": list})
}

// entryMarkdown answers with an article as a Markdown note, for clients
// that hand it to an app on the device (Obsidian).
func (a *API) entryMarkdown(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	e, err := a.Store.Entry(r.Context(), userID(r), id)
	if err != nil {
		a.result(w, err)
		return
	}
	feed := ""
	if f, err := a.Store.Feed(r.Context(), userID(r), e.FeedID); err == nil {
		feed = f.Title
	}
	reply(w, map[string]any{"title": e.Title, "markdown": Note(e, feed)})
}

// Note is an article as a Markdown note: properties Obsidian reads, the
// summary as a quotation, then the text.
func Note(e *store.Entry, feed string) string {
	var b strings.Builder
	q := func(s string) string {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(s) + `"`
	}
	b.WriteString("---\n")
	b.WriteString("title: " + q(e.Title) + "\n")
	b.WriteString("source: " + q(e.URL) + "\n")
	if e.Author != "" {
		b.WriteString("author: " + q(e.Author) + "\n")
	}
	if feed != "" {
		b.WriteString("feed: " + q(feed) + "\n")
	}
	b.WriteString("published: " + time.Unix(e.PublishedAt, 0).UTC().Format("2006-01-02") + "\n")
	b.WriteString("saved: " + time.Now().UTC().Format("2006-01-02") + "\n")
	b.WriteString("tags: [correspondent]\n---\n\n")
	b.WriteString("# " + e.Title + "\n\n")
	if e.Summary != "" {
		b.WriteString("> " + strings.ReplaceAll(strings.TrimSpace(e.Summary), "\n", "\n> ") + "\n\n")
	}
	if e.Content != "" {
		b.WriteString(textify.Markdown(e.Content, e.URL))
	} else if e.Excerpt != "" {
		b.WriteString(e.Excerpt + "\n")
	}
	b.WriteString("\n[Read the original](" + e.URL + ")\n")
	return b.String()
}

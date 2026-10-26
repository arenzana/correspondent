package pagefeed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// answers are the selector JSON a fake model gives, one per request.
type fakeAI struct {
	mu       sync.Mutex
	answers  []string
	requests []map[string]any
	status   int    // non-200 answers every request
	reject   string // a response_format type the server refuses with 400
	refuse   bool
}

func (f *fakeAI) next(body map[string]any) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, body)
	if len(f.answers) == 0 {
		return "", false
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a, true
}

func (f *fakeAI) anthropic(t *testing.T) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "sk-test-key-0123456789" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"secret detail"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/models/") {
			io.WriteString(w, `{"id":"claude-test","type":"model","display_name":"Test","created_at":"2026-01-01T00:00:00Z"}`)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		answer, _ := f.next(body)
		stop := "end_turn"
		if f.refuse {
			stop, answer = "refusal", ""
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-test",
			"content":     []map[string]any{{"type": "text", "text": answer}},
			"stop_reason": stop, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeAI) openAI(t *testing.T) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test-key-0123456789" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[]}`)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if format, _ := body["response_format"].(map[string]any); f.reject != "" && format["type"] == f.reject {
			f.mu.Lock()
			f.requests = append(f.requests, body)
			f.mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		answer, _ := f.next(body)
		msg := map[string]any{"role": "assistant", "content": answer}
		if f.refuse {
			msg["refusal"] = "no"
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"finish_reason": "stop", "message": msg}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

const goodAnswer = `{"item":"div.card","link":"a","title":"h3","date":"time","summary":"","image":"","reason":"cards"}`

func suggester(t *testing.T, provider string, srv *httptest.Server) *Suggester {
	t.Helper()
	base := srv.URL
	if provider == ProviderOpenAI {
		base += "/v1"
	}
	s, err := NewSuggester(Config{Provider: provider, Model: "test-model", BaseURL: base}, "sk-test-key-0123456789", srv.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSuggestBothProviders(t *testing.T) {
	for _, provider := range []string{ProviderAnthropic, ProviderOpenAI} {
		t.Run(provider, func(t *testing.T) {
			f := &fakeAI{answers: []string{`{"item":"li.none","link":"","title":"","date":"","summary":"","image":"","reason":"x"}`, "```json\n" + goodAnswer + "\n```"}}
			srv := f.anthropic(t)
			if provider == ProviderOpenAI {
				srv = f.openAI(t)
			}
			s := suggester(t, provider, srv)
			if err := s.Verify(context.Background()); err != nil {
				t.Fatal(err)
			}
			r, p, err := s.Suggest(context.Background(), []byte(listing), base)
			if err != nil {
				t.Fatal(err)
			}
			if r.Method != MethodCSS || r.Item != "div.card" || len(p.Items) != 4 {
				t.Fatalf("%+v %d", r, len(p.Items))
			}
			// The first answer matched nothing: the model was told and asked again,
			// with the conversation so far.
			if len(f.requests) != 2 {
				t.Fatalf("%d requests", len(f.requests))
			}
			second, _ := json.Marshal(f.requests[1]["messages"])
			if !strings.Contains(string(second), "match no articles") || !strings.Contains(string(second), "li.none") {
				t.Errorf("correction missing: %s", second)
			}
			first, _ := json.Marshal(f.requests[0])
			if !strings.Contains(string(first), "Alpha: a careful study") || !strings.Contains(string(first), "untrusted page content") {
				t.Errorf("first request lacks the outline or the injection warning")
			}
			if provider == ProviderAnthropic {
				// A compatible service (custom address) gets no Anthropic-only betas.
				if _, ok := f.requests[0]["fallbacks"]; ok {
					t.Error("fallbacks sent to a compatible service")
				}
				if oc, _ := f.requests[0]["output_config"].(map[string]any); oc["format"] == nil {
					t.Error("no output format")
				}
			}
		})
	}
}

func TestSuggestFailures(t *testing.T) {
	ctx := context.Background()
	f := &fakeAI{answers: []string{goodAnswer}, refuse: true}
	if _, _, err := suggester(t, ProviderAnthropic, f.anthropic(t)).Suggest(ctx, []byte(listing), base); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Errorf("refusal: %v", err)
	}
	f = &fakeAI{answers: []string{goodAnswer}, refuse: true}
	if _, _, err := suggester(t, ProviderOpenAI, f.openAI(t)).Suggest(ctx, []byte(listing), base); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Errorf("openai refusal: %v", err)
	}
	f = &fakeAI{answers: []string{"not json"}}
	if _, _, err := suggester(t, ProviderOpenAI, f.openAI(t)).Suggest(ctx, []byte(listing), base); err == nil || !strings.Contains(err.Error(), "couldn't be read") {
		t.Errorf("bad json: %v", err)
	}
	bad := `{"item":"li.none","link":"","title":"","date":"","summary":"","image":"","reason":"x"}`
	f = &fakeAI{answers: []string{bad, bad}}
	if _, _, err := suggester(t, ProviderOpenAI, f.openAI(t)).Suggest(ctx, []byte(listing), base); err == nil || len(f.requests) != 2 {
		t.Errorf("two bad answers: %v after %d requests", err, len(f.requests))
	}
	// Errors are the reader's, never the provider's response body.
	f = &fakeAI{status: http.StatusInternalServerError}
	err := suggester(t, ProviderAnthropic, f.anthropic(t)).Verify(ctx)
	if err == nil || strings.Contains(err.Error(), "secret detail") || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("server error: %v", err)
	}
	for _, provider := range []string{ProviderAnthropic, ProviderOpenAI} {
		f = &fakeAI{}
		srv := f.anthropic(t)
		if provider == ProviderOpenAI {
			srv = f.openAI(t)
		}
		s, _ := NewSuggester(Config{Provider: provider, Model: "m", BaseURL: srv.URL + map[string]string{ProviderOpenAI: "/v1"}[provider]}, "sk-wrong-key-0123456789", srv.Client().Transport)
		if err := s.Verify(ctx); err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s wrong key: %v", provider, err)
		}
	}
}

func TestOpenAIPlainJSONFallback(t *testing.T) {
	f := &fakeAI{answers: []string{goodAnswer}, reject: "json_schema"}
	if _, _, err := suggester(t, ProviderOpenAI, f.openAI(t)).Suggest(context.Background(), []byte(listing), base); err != nil {
		t.Fatal(err)
	}
	if len(f.requests) != 2 || f.requests[1]["response_format"].(map[string]any)["type"] != "json_object" {
		t.Errorf("%+v", f.requests)
	}
}

func TestConfig(t *testing.T) {
	c, err := Config{Provider: ProviderAnthropic}.Normalize()
	if err != nil || c.Model != DefaultAnthropicModel {
		t.Errorf("%+v %v", c, err)
	}
	for _, bad := range []Config{
		{Provider: "gemini", Model: "x"},
		{Provider: ProviderOpenAI},
		{Provider: ProviderOpenAI, Model: "m", BaseURL: "http://api.example.com/v1"},
		{Provider: ProviderOpenAI, Model: "m", BaseURL: "https://user:pw@api.example.com/v1"},
		{Provider: ProviderOpenAI, Model: "m", BaseURL: "ftp://x"},
	} {
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	c, err = Config{Provider: ProviderOpenAI, Model: " gpt-test ", BaseURL: "https://api.example.com/v1/ "}.Normalize()
	if err != nil || c.Model != "gpt-test" || c.BaseURL != "https://api.example.com/v1" {
		t.Errorf("%+v %v", c, err)
	}
	if _, err := NewSuggester(Config{Provider: ProviderAnthropic}, "  ", nil); err == nil {
		t.Error("empty key accepted")
	}
}

func TestOfficialAnthropicRequest(t *testing.T) {
	f := &fakeAI{answers: []string{goodAnswer}}
	srv := f.anthropic(t)
	var beta string
	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			beta = r.Header.Get("Anthropic-Beta")
		}
		inner.ServeHTTP(w, r)
	})
	// No address: Anthropic's own API (redirected here for the test).
	s, err := NewSuggester(Config{Provider: ProviderAnthropic}, "sk-test-key-0123456789", srv.Client().Transport, option.WithBaseURL(srv.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Suggest(context.Background(), []byte(listing), base); err != nil {
		t.Fatal(err)
	}
	req := f.requests[0]
	if req["model"] != DefaultAnthropicModel || req["fallbacks"] != "default" || !strings.Contains(beta, "server-side-fallback-2026-07-01") {
		t.Errorf("model %v fallbacks %v beta %q", req["model"], req["fallbacks"], beta)
	}
	if oc, _ := req["output_config"].(map[string]any); oc["effort"] != "medium" {
		t.Errorf("output_config %v", oc)
	}
	if _, ok := req["temperature"]; ok {
		t.Error("sampling parameter sent")
	}
}

package speech

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestScript(t *testing.T) {
	got := Script("Ships in the night", "Ada", `<p>First <a href="#">para</a>, with <code>x := 1</code> inline.</p>
<figure><img src="a.png"><figcaption>A caption</figcaption></figure>
<pre><code>func main() {}</code></pre>
<h2>Second part</h2><ul><li>One</li><li>Two<sup>1</sup></li></ul><table><tr><td>cell</td></tr></table>`)
	want := "Ships in the night.\n\nBy Ada.\n\nFirst para, with x := 1 inline.\n\nSecond part\n\nOne\n\nTwo"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestChunks(t *testing.T) {
	long := strings.Repeat("A sentence of words. ", 30) // ~630 bytes, one paragraph
	script := "Short title.\n\n" + long + "\n\nEnd."
	chunks := Chunks(script, 200)
	if len(chunks) < 4 {
		t.Fatalf("%d chunks", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 200 {
			t.Errorf("chunk of %d bytes", len(c))
		}
	}
	joined := strings.Join(chunks, " ")
	if strings.Count(joined, "A sentence of words.") != 30 || !strings.HasPrefix(joined, "Short title.") || !strings.HasSuffix(joined, "End.") {
		t.Errorf("text lost or reordered: %q", joined)
	}
}

func TestNormalize(t *testing.T) {
	c, err := Config{Provider: ProviderDeepgram, BaseURL: "https://evil.example"}.Normalize()
	if err != nil || c.Model != "aura-2-thalia-en" || c.BaseURL != "" {
		t.Errorf("%+v %v", c, err)
	}
	if _, err := (Config{Provider: ProviderOpenAI, BaseURL: "http://plain.example"}).Normalize(); err == nil {
		t.Error("plain http address accepted")
	}
	if _, err := (Config{Provider: ProviderOpenAI, Voice: "a/../b"}).Normalize(); err == nil {
		t.Error("voice with a path accepted")
	}
	if _, err := (Config{Provider: "anthropic"}).Normalize(); err == nil {
		t.Error("unknown provider accepted")
	}
	a, b := Config{Provider: "openai", Voice: "alloy"}, Config{Provider: "openai", Voice: "nova"}
	if a.VoiceKey() == b.VoiceKey() || ScriptHash("x") == ScriptHash("y") {
		t.Error("voice key ignores the voice, or script hash the text")
	}
}

// fake answers like each provider and records what it was sent.
type fake struct {
	srv   *httptest.Server
	calls atomic.Int32
}

func newFake(t *testing.T, status int) *fake {
	f := &fake{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		text := body["input"] + body["text"]
		if status != 200 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("[" + text + "]"))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// redirect sends every request to the fake, keeping path and query.
type redirect struct {
	to   *url.URL
	base http.RoundTripper
	seen func(*http.Request)
}

func (r redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	r.seen(req)
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.to.Scheme, r.to.Host
	return r.base.RoundTrip(req)
}

func TestProviders(t *testing.T) {
	for _, tc := range []struct {
		provider, wantPath, wantAuth string
	}{
		{ProviderOpenAI, "api.openai.com/v1/audio/speech", "Authorization=Bearer sk-test"},
		{ProviderElevenLabs, "api.elevenlabs.io/v1/text-to-speech/JBFqnCBsd6RMkjVDRZzb?output_format=mp3_44100_128", "Xi-Api-Key=sk-test"},
		{ProviderDeepgram, "api.deepgram.com/v1/speak?model=aura-2-thalia-en&encoding=mp3", "Authorization=Token sk-test"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			f := newFake(t, 200)
			u, _ := url.Parse(f.srv.URL)
			var (
				mu   sync.Mutex
				seen []string
			)
			tr := redirect{to: u, base: f.srv.Client().Transport, seen: func(r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				path := r.URL.Host + r.URL.Path
				if r.URL.RawQuery != "" {
					path += "?" + r.URL.RawQuery
				}
				name, _, _ := strings.Cut(tc.wantAuth, "=")
				seen = append(seen, path, name+"="+r.Header.Get(name))
			}}
			s, err := New(Config{Provider: tc.provider}, "sk-test", tr)
			if err != nil {
				t.Fatal(err)
			}
			script := strings.Repeat("Words here. ", 400) // several chunks for every provider
			mp3, err := s.Speak(context.Background(), script)
			if err != nil {
				t.Fatal(err)
			}
			if seen[0] != tc.wantPath || seen[1] != tc.wantAuth {
				t.Errorf("sent %v", seen[:2])
			}
			if f.calls.Load() < 2 {
				t.Errorf("%d requests; expected the script in chunks", f.calls.Load())
			}
			// Chunks come back in order, joined.
			got := strings.NewReplacer("][", " ", "[", "", "]", "").Replace(string(mp3))
			if strings.Join(strings.Fields(got), " ") != strings.TrimSpace(script) {
				t.Error("audio chunks out of order or missing")
			}
		})
	}
}

func TestProviderErrors(t *testing.T) {
	f := newFake(t, 401)
	s, _ := New(Config{Provider: ProviderOpenAI, BaseURL: f.srv.URL}, "sk-bad", f.srv.Client().Transport)
	err := s.Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refused the key: Incorrect API key provided") {
		t.Errorf("%v", err)
	}
	if _, err := s.Speak(context.Background(), strings.Repeat("x ", MaxScript)); err == nil {
		t.Error("over-long script accepted")
	}
}

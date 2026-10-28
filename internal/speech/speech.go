// Package speech reads articles aloud with a reader's own text-to-speech
// provider: OpenAI or a compatible service, ElevenLabs, or Deepgram. The
// article is split into chunks each provider accepts, spoken in parallel,
// and joined into one MP3.
package speech

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	ProviderOpenAI     = "openai"
	ProviderElevenLabs = "elevenlabs"
	ProviderDeepgram   = "deepgram"
)

// Defaults, used when the reader leaves model or voice empty.
var defaults = map[string]struct{ base, model, voice string }{
	ProviderOpenAI:     {"https://api.openai.com/v1", "gpt-4o-mini-tts", "alloy"},
	ProviderElevenLabs: {"https://api.elevenlabs.io", "eleven_multilingual_v2", "JBFqnCBsd6RMkjVDRZzb"},
	ProviderDeepgram:   {"https://api.deepgram.com", "aura-2-thalia-en", ""},
}

// chunkLimits are the most characters each provider takes in one request
// (OpenAI 4096, Deepgram 2000, ElevenLabs more), with some room to spare.
var chunkLimits = map[string]int{ProviderOpenAI: 4000, ProviderElevenLabs: 4500, ProviderDeepgram: 1900}

// MaxScript is the longest article read aloud, about two hours of speech.
const MaxScript = 120_000

// Config is a reader's text-to-speech setup. The key is stored apart,
// sealed; only its last four characters are kept here.
type Config struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl,omitempty"`
	Model    string `json:"model"`
	Voice    string `json:"voice,omitempty"`
	KeyHint  string `json:"keyHint,omitempty"`
}

// Normalize fills defaults and checks the config.
func (c Config) Normalize() (Config, error) {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Model = strings.TrimSpace(c.Model)
	c.Voice = strings.TrimSpace(c.Voice)
	d, ok := defaults[c.Provider]
	if !ok {
		return c, errors.New("choose OpenAI or a compatible service, ElevenLabs, or Deepgram")
	}
	if c.Model == "" {
		c.Model = d.model
	}
	if c.Voice == "" {
		c.Voice = d.voice
	}
	if c.Provider != ProviderOpenAI {
		c.BaseURL = "" // fixed services; only OpenAI-compatible ones have an address
	}
	for _, v := range []string{c.Model, c.Voice} {
		if len(v) > 100 || strings.ContainsAny(v, " \t\r\n/?#&") {
			return c, errors.New("that model or voice name isn't valid")
		}
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		// Keys are sent there: only over HTTPS.
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(c.BaseURL) > 500 {
			return c, errors.New("the address must be a full https:// URL")
		}
	}
	return c, nil
}

// ScriptHash identifies an article's text: the same article in two
// readers' feeds has the same hash, so its audio can be shared.
func ScriptHash(script string) string {
	sum := sha256.Sum256([]byte(script))
	return hex.EncodeToString(sum[:])[:20]
}

// Shared reports whether audio made with this config may be shared with
// other readers: only from a provider's own service, not from an address
// the reader chose, which could return anything.
func (c Config) Shared() bool { return c.BaseURL == "" }

// VoiceKey identifies the voice a config speaks with.
func (c Config) VoiceKey() string {
	h := sha256.New()
	for _, s := range []string{c.Provider, c.BaseURL, c.Model, c.Voice} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// Speaker speaks with one reader's provider and key.
type Speaker struct {
	cfg    Config
	key    string
	client *http.Client
}

// New connects to the configured provider with the reader's key.
func New(cfg Config, key string, transport http.RoundTripper) (*Speaker, error) {
	cfg, err := cfg.Normalize()
	if err != nil {
		return nil, err
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Speaker{cfg: cfg, key: key, client: &http.Client{Transport: transport, Timeout: 2 * time.Minute}}, nil
}

// Verify speaks one word, to check the key, model and voice.
func (s *Speaker) Verify(ctx context.Context) error {
	_, err := s.speak(ctx, "Ready.")
	return err
}

// Speak turns a script into one MP3, speaking up to three chunks at once.
func (s *Speaker) Speak(ctx context.Context, script string) ([]byte, error) {
	if len(script) > MaxScript {
		return nil, errors.New("this article is too long to read aloud")
	}
	chunks := Chunks(script, chunkLimits[s.cfg.Provider])
	if len(chunks) == 0 {
		return nil, errors.New("this article has no text to read")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	parts := make([][]byte, len(chunks))
	var (
		wg      sync.WaitGroup
		once    sync.Once
		failure error
	)
	sem := make(chan struct{}, 3)
	for i, c := range chunks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			b, err := s.speak(ctx, c)
			if err != nil {
				once.Do(func() { failure = err; cancel() })
				return
			}
			parts[i] = b
		}()
	}
	wg.Wait()
	if failure != nil {
		return nil, failure
	}
	// MP3 is a stream of frames: the chunks join end to end.
	return bytes.Join(parts, nil), nil
}

// speak sends one chunk to the provider and returns its MP3.
func (s *Speaker) speak(ctx context.Context, text string) ([]byte, error) {
	d := defaults[s.cfg.Provider]
	var (
		endpoint string
		body     any
		header   = http.Header{"Content-Type": {"application/json"}, "Accept": {"audio/mpeg"}}
	)
	switch s.cfg.Provider {
	case ProviderOpenAI:
		base := s.cfg.BaseURL
		if base == "" {
			base = d.base
		}
		endpoint = base + "/audio/speech"
		body = map[string]string{"model": s.cfg.Model, "voice": s.cfg.Voice, "input": text, "response_format": "mp3"}
		header.Set("Authorization", "Bearer "+s.key)
	case ProviderElevenLabs:
		endpoint = d.base + "/v1/text-to-speech/" + url.PathEscape(s.cfg.Voice) + "?output_format=mp3_44100_128"
		body = map[string]string{"text": text, "model_id": s.cfg.Model}
		header.Set("xi-api-key", s.key)
	case ProviderDeepgram:
		// Deepgram's model names the voice too (aura-2-thalia-en).
		endpoint = d.base + "/v1/speak?model=" + url.QueryEscape(s.cfg.Model) + "&encoding=mp3"
		body = map[string]string{"text": text}
		header.Set("Authorization", "Token "+s.key)
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header = header
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach the speech service: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, providerError(resp.StatusCode, data)
	}
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "text/") {
		return nil, errors.New("the speech service didn't send audio")
	}
	return data, nil
}

// providerError turns a provider's refusal into a sentence for the reader,
// keeping the provider's own message when it gives one.
func providerError(status int, body []byte) error {
	var msg string
	var e struct {
		Error   any    `json:"error"`
		Detail  any    `json:"detail"`
		Message string `json:"message"`
		ErrMsg  string `json:"err_msg"`
	}
	if json.Unmarshal(body, &e) == nil {
		msg = firstText(e.Error, e.Detail, e.Message, e.ErrMsg)
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("the speech service refused the key%s", suffix(msg))
	case http.StatusTooManyRequests, http.StatusPaymentRequired:
		return fmt.Errorf("the speech service's limit or credit is used up%s", suffix(msg))
	default:
		return fmt.Errorf("the speech service answered %d%s", status, suffix(msg))
	}
}

func suffix(msg string) string {
	if msg == "" {
		return ""
	}
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return ": " + strings.TrimRight(msg, ".")
}

// firstText finds a message in the shapes providers use: a string, or an
// object with a "message".
func firstText(vs ...any) string {
	for _, v := range vs {
		switch t := v.(type) {
		case string:
			if t != "" {
				return t
			}
		case map[string]any:
			if m, ok := t["message"].(string); ok && m != "" {
				return m
			}
		}
	}
	return ""
}

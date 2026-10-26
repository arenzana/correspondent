package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/speech"
	"github.com/arenzana/correspondent/internal/store"
)

// Read aloud. Readers bring their own text-to-speech key. Audio is stored
// by the article's text (audio/<script hash>-<voice>.mp3) and shared: once
// anyone on the server has had an article read aloud, everyone following it
// can listen, without a key, and nobody's key is spent on it again.

const (
	speechContext = "user:%d:speech-key"
	audioContext  = "audio-link"
	// SpeechPerDay is how many articles a reader can have read aloud a day.
	SpeechPerDay = 60
)

var (
	speechOnce  sync.Once
	speechLimit *ratelimit.Limiter
	// making serializes work on one text, so two taps (or two readers)
	// make one file.
	making sync.Map // script hash → *sync.Mutex
)

func (a *API) speechLimiter() *ratelimit.Limiter {
	speechOnce.Do(func() { speechLimit = ratelimit.New(SpeechPerDay, 24*time.Hour) })
	return speechLimit
}

func (a *API) audioDir() string { return filepath.Join(a.DataDir, "audio") }

func (a *API) speechConfig(ctx context.Context, uid int64) (cfg speech.Config, sealed []byte, ok bool, err error) {
	sealed, raw, err := a.Store.Speech(ctx, uid)
	if err != nil || sealed == nil || raw == "" {
		return cfg, nil, false, err
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, nil, false, err
	}
	return cfg, sealed, true, nil
}

// putSpeech checks a reader's text-to-speech setup with the provider and
// stores it, the key sealed. Leaving the key empty keeps the saved one.
func (a *API) putSpeech(w http.ResponseWriter, r *http.Request) {
	var in struct {
		speech.Config
		Key string `json:"key"`
	}
	if !decode(w, r, &in) {
		return
	}
	if a.Keyring == nil {
		fail(w, http.StatusNotImplemented, "Reading aloud isn't available on this server.")
		return
	}
	uid := userID(r)
	cfg, err := in.Config.Normalize()
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	}
	key := strings.TrimSpace(in.Key)
	if key == "" {
		old, sealed, ok, err := a.speechConfig(r.Context(), uid)
		if err != nil || !ok {
			fail(w, http.StatusBadRequest, "Enter your API key.")
			return
		}
		plain, err := a.Keyring.Open(sealed, fmt.Sprintf(speechContext, uid))
		if err != nil {
			fail(w, http.StatusBadRequest, sentence(err.Error()))
			return
		}
		key, cfg.KeyHint = string(plain), old.KeyHint
	} else if len(key) < 20 || len(key) > 400 || strings.ContainsAny(key, " \t\r\n") {
		fail(w, http.StatusBadRequest, "That doesn't look like an API key.")
		return
	} else {
		cfg.KeyHint = key[len(key)-4:]
	}
	sp, err := speech.New(cfg, key, a.aiTransport())
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := sp.Verify(ctx); err != nil {
		fail(w, http.StatusUnprocessableEntity, sentence(err.Error()))
		return
	}
	raw, _ := json.Marshal(cfg)
	if err := a.Store.SetSpeech(r.Context(), uid, a.Keyring.Seal([]byte(key), fmt.Sprintf(speechContext, uid)), string(raw)); err != nil {
		a.internal(w, err)
		return
	}
	reply(w, map[string]any{"speech": cfg})
}

// deleteSpeech forgets the key. Audio already made stays playable.
func (a *API) deleteSpeech(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.SetSpeech(r.Context(), userID(r), nil, ""); err != nil {
		a.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// makeAudio answers with an article's audio: the reader's own, or audio
// someone else already made of the same text, or else new audio made with
// the reader's key.
func (a *API) makeAudio(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusNotFound, "No such article.")
		return
	}
	e, err := a.Store.Entry(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "No such article.")
		return
	} else if err != nil {
		a.internal(w, err)
		return
	}
	script := speech.Script(e.Title, e.Author, e.Content)
	hash := speech.ScriptHash(script)
	mu, _ := making.LoadOrStore(hash, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	cfg, sealed, ok, err := a.speechConfig(r.Context(), uid)
	if err != nil {
		a.internal(w, err)
		return
	}
	voice := ""
	if ok {
		voice = cfg.VoiceKey()
	}
	// Audio already linked plays as is, unless the text changed and the
	// reader can make new audio of it.
	if e.Audio != "" && (!ok || strings.HasPrefix(e.Audio, hash+"-")) {
		if _, err := os.Stat(filepath.Join(a.audioDir(), e.Audio)); err == nil {
			reply(w, map[string]any{"url": audioPath(id)})
			return
		}
	}
	if file := a.findAudio(hash, voice); file != "" {
		if e.Audio != file {
			if err := a.Store.SetAudio(r.Context(), uid, id, file); err != nil {
				a.internal(w, err)
				return
			}
		}
		reply(w, map[string]any{"url": audioPath(id)})
		return
	}
	if a.Keyring == nil {
		fail(w, http.StatusNotImplemented, "Reading aloud isn't available on this server.")
		return
	}
	if !ok {
		fail(w, http.StatusConflict, "Set up reading aloud in Settings first.")
		return
	}
	limitKey := fmt.Sprintf("speech:%d", uid)
	if !a.speechLimiter().Allow(limitKey) {
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("You've had %d articles read aloud today, the daily limit. Try again tomorrow.", SpeechPerDay))
		return
	}
	key, err := a.Keyring.Open(sealed, fmt.Sprintf(speechContext, uid))
	if err != nil {
		a.internal(w, err)
		return
	}
	sp, err := speech.New(cfg, string(key), a.aiTransport())
	if err != nil {
		fail(w, http.StatusBadRequest, sentence(err.Error()))
		return
	}
	a.speechLimiter().Fail(limitKey)
	// Finish even if the reader navigates away: the provider is paid either way.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()
	mp3, err := sp.Speak(ctx, script)
	if err != nil {
		fail(w, http.StatusBadGateway, sentence(err.Error()))
		return
	}
	file := hash + "-" + voice + ".mp3"
	if err := a.saveAudio(file, mp3); err != nil {
		a.internal(w, err)
		return
	}
	if err := a.Store.SetAudio(r.Context(), uid, id, file); err != nil {
		a.internal(w, err)
		return
	}
	a.Log.Info("read aloud", "entry", id, "provider", cfg.Provider, "bytes", len(mp3))
	reply(w, map[string]any{"url": audioPath(id)})
}

// findAudio returns the file of existing audio of a text, in the given
// voice if there is one, else in any voice; "" if there's none.
func (a *API) findAudio(hash, voice string) string {
	if voice != "" {
		if _, err := os.Stat(filepath.Join(a.audioDir(), hash+"-"+voice+".mp3")); err == nil {
			return hash + "-" + voice + ".mp3"
		}
	}
	matches, _ := filepath.Glob(filepath.Join(a.audioDir(), hash+"-*.mp3"))
	if len(matches) == 0 {
		return ""
	}
	return filepath.Base(matches[0])
}

// sharedAudio links audio someone else made of this article's text to the
// reader's copy, so it shows as ready to play.
func (a *API) sharedAudio(ctx context.Context, uid int64, e *store.Entry) {
	if e.Audio != "" || e.Content == "" {
		return
	}
	if file := a.findAudio(speech.ScriptHash(speech.Script(e.Title, e.Author, e.Content)), ""); file != "" {
		if a.Store.SetAudio(ctx, uid, e.ID, file) == nil {
			e.Audio, e.HasAudio = file, true
		}
	}
}

func audioPath(id int64) string { return "/api/v1/entries/" + strconv.FormatInt(id, 10) + "/audio" }

func (a *API) saveAudio(file string, mp3 []byte) error {
	if err := os.MkdirAll(a.audioDir(), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(a.audioDir(), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(mp3); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(a.audioDir(), file))
}

// audio serves a reader's own audio for an entry.
func (a *API) audio(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusNotFound, "No such article.")
		return
	}
	a.serveAudio(w, r, userID(r), id)
}

// audioLink serves audio to apps through a private link: players fetch
// enclosures without the app's sign-in, so the link itself is the
// credential, sealed by the server and naming one reader's entry.
func (a *API) audioLink(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := a.openAudioLink(strings.TrimSuffix(r.PathValue("token"), ".mp3"))
	if !ok {
		fail(w, http.StatusNotFound, "No such audio.")
		return
	}
	a.serveAudio(w, r, uid, id)
}

func (a *API) serveAudio(w http.ResponseWriter, r *http.Request, uid, id int64) {
	e, err := a.Store.Entry(r.Context(), uid, id)
	if err != nil || e.Audio == "" {
		fail(w, http.StatusNotFound, "This article hasn't been read aloud.")
		return
	}
	f, err := os.Open(filepath.Join(a.audioDir(), e.Audio))
	if err != nil {
		fail(w, http.StatusNotFound, "This article hasn't been read aloud.")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// AudioLink is the private address apps use for an entry's audio, or ""
// when there's no public address to put in it.
func (a *API) AudioLink(uid, entryID int64) string {
	if a.Keyring == nil || a.PublicURL == "" {
		return ""
	}
	sealed := a.Keyring.Seal([]byte(fmt.Sprintf("%d:%d", uid, entryID)), audioContext)
	return strings.TrimRight(a.PublicURL, "/") + "/api/v1/audio/" + base64.RawURLEncoding.EncodeToString(sealed) + ".mp3"
}

func (a *API) openAudioLink(token string) (uid, id int64, ok bool) {
	if a.Keyring == nil {
		return 0, 0, false
	}
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, 0, false
	}
	plain, err := a.Keyring.Open(sealed, audioContext)
	if err != nil {
		return 0, 0, false
	}
	u, e, found := strings.Cut(string(plain), ":")
	uid, err1 := strconv.ParseInt(u, 10, 64)
	id, err2 := strconv.ParseInt(e, 10, 64)
	return uid, id, found && err1 == nil && err2 == nil
}

// SweepAudio removes audio files no entry refers to any more (every entry
// with that text was deleted), leaving files still being written.
func (a *API) SweepAudio(ctx context.Context) (int, error) {
	keep, err := a.Store.AudioFiles(ctx)
	if err != nil {
		return 0, err
	}
	dirents, err := os.ReadDir(a.audioDir())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	removed := 0
	for _, d := range dirents {
		info, err := d.Info()
		if err != nil || keep[d.Name()] || time.Since(info.ModTime()) < time.Hour {
			continue
		}
		if os.Remove(filepath.Join(a.audioDir(), d.Name())) == nil {
			removed++
		}
	}
	return removed, nil
}

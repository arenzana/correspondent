package imgproxy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/fetch"
)

func pngOf(w, h int, alpha uint8) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 128, alpha})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// bomb is a PNG header claiming 100,000 × 100,000 pixels.
func bomb() []byte {
	b := pngOf(1, 1, 255)
	// IHDR width and height are bytes 16–23.
	copy(b[16:], []byte{0, 1, 0x86, 0xa0, 0, 1, 0x86, 0xa0})
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	return b
}

type fixture struct {
	srv   *httptest.Server
	p     *Proxy
	hits  atomic.Int32
	proxy http.Handler
}

func setup(t *testing.T) *fixture {
	f := &fixture{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		switch r.URL.Path {
		case "/photo.png":
			w.Write(pngOf(800, 400, 255))
		case "/clear.png":
			w.Write(pngOf(800, 400, 100))
		case "/small.png":
			w.Write(pngOf(20, 20, 255))
		case "/page":
			w.Header().Set("Content-Type", "image/png") // lies
			w.Write([]byte("<!doctype html><script>alert(1)</script>"))
		case "/logo.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/bomb.png":
			w.Write(bomb())
		case "/slow.png":
			time.Sleep(100 * time.Millisecond)
			w.Write(pngOf(800, 400, 255))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	f.p = &Proxy{
		Fetch:    fetch.New(fetch.Options{AllowPrivate: true, MaxBytes: MaxBytes}),
		Dir:      t.TempDir(),
		MaxCache: 1 << 20,
		Key:      []byte("0123456789abcdef0123456789abcdef"),
		Log:      slog.New(slog.DiscardHandler),
	}
	return f
}

func (f *fixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.p.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestResize(t *testing.T) {
	f := setup(t)
	rec := f.get(t, f.p.URL(f.srv.URL+"/photo.png", 128))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	cfg, _, err := image.DecodeConfig(rec.Body)
	if err != nil || cfg.Width != 128 || cfg.Height != 64 {
		t.Fatalf("got %+v %v", cfg, err)
	}
	for k, want := range map[string]string{
		"Cache-Control":           "public, max-age=31536000, immutable",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": sandbox,
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q", k, got)
		}
	}
}

func TestTransparencyKeepsPNG(t *testing.T) {
	f := setup(t)
	rec := f.get(t, f.p.URL(f.srv.URL+"/clear.png", 256))
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type %s", ct)
	}
	if _, err := png.Decode(rec.Body); err != nil {
		t.Fatal(err)
	}
}

func TestSmallAndOriginalPassThrough(t *testing.T) {
	f := setup(t)
	small := pngOf(20, 20, 255)
	if rec := f.get(t, f.p.URL(f.srv.URL+"/small.png", 640)); !bytes.Equal(rec.Body.Bytes(), small) {
		t.Error("small image was re-encoded")
	}
	rec := f.get(t, f.p.URL(f.srv.URL+"/photo.png", 0))
	if cfg, _, _ := image.DecodeConfig(rec.Body); cfg.Width != 800 {
		t.Errorf("original width %d", cfg.Width)
	}
}

func TestSignature(t *testing.T) {
	f := setup(t)
	good := f.p.URL(f.srv.URL+"/photo.png", 128)
	parts := strings.Split(good, "/") // "", "img", sig, width, url
	other := f.p.URL(f.srv.URL+"/small.png", 128)
	cases := map[string]string{
		"other width":  strings.Join([]string{"", "img", parts[2], "256", parts[4]}, "/"),
		"other url":    strings.Join([]string{"", "img", parts[2], parts[3], strings.Split(other, "/")[4]}, "/"),
		"no signature": strings.Join([]string{"", "img", "", parts[3], parts[4]}, "/"),
		"other key":    (&Proxy{Key: []byte("another key")}).URL(f.srv.URL+"/photo.png", 128),
		"forged":       "/img/AAAAAAAAAAAAAAAAAAAAAAAA/128/" + parts[4],
	}
	for name, path := range cases {
		if rec := f.get(t, path); rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if f.hits.Load() != 0 {
		t.Errorf("unsigned requests reached the origin %d times", f.hits.Load())
	}
	for _, path := range []string{"/img/x", "/img/sig/77/" + parts[4], "/img/sig/abc/" + parts[4], "/img/sig/128/!!!"} {
		if rec := f.get(t, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestURL(t *testing.T) {
	p := &Proxy{Key: []byte("k")}
	for _, raw := range []string{"", "data:image/png;base64,AAAA", "javascript:alert(1)", "/relative.png", "file:///etc/passwd"} {
		if got := p.URL(raw, 128); got != "" {
			t.Errorf("%q → %q", raw, got)
		}
	}
	for in, want := range map[int]string{-1: "/0/", 0: "/0/", 1: "/32/", 100: "/128/", 1600: "/1600/", 5000: "/0/"} {
		if got := p.URL("https://example.com/a.png", in); !strings.Contains(got, want) {
			t.Errorf("width %d → %s", in, got)
		}
	}
}

func TestRefusals(t *testing.T) {
	f := setup(t)
	for _, path := range []string{"/page", "/bomb.png", "/missing.png"} {
		if rec := f.get(t, f.p.URL(f.srv.URL+path, 128)); rec.Code != http.StatusBadGateway {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	// The default fetcher refuses private addresses such as the test server's.
	strict := &Proxy{Fetch: fetch.New(fetch.Options{}), Dir: t.TempDir(), Key: f.p.Key, Log: f.p.Log}
	rec := httptest.NewRecorder()
	strict.ServeHTTP(rec, httptest.NewRequest("GET", strict.URL(f.srv.URL+"/photo.png", 128), nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("private address: %d", rec.Code)
	}
	if _, _, err := strict.fetch(t.Context(), f.srv.URL+"/photo.png", 128); !errors.Is(err, fetch.ErrBlocked) {
		t.Errorf("private address: %v", err)
	}
}

func TestSVGIsSandboxed(t *testing.T) {
	f := setup(t)
	rec := f.get(t, f.p.URL(f.srv.URL+"/logo.svg", 32))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("csp %q", csp)
	}
}

func TestCacheAndSingleFlight(t *testing.T) {
	f := setup(t)
	path := f.p.URL(f.srv.URL+"/slow.png", 64)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if rec := f.get(t, path); rec.Code != 200 {
				t.Errorf("status %d", rec.Code)
			}
		})
	}
	wg.Wait()
	f.get(t, path)
	if n := f.hits.Load(); n != 1 {
		t.Errorf("origin fetched %d times", n)
	}
	// A restart (a new Proxy on the same directory) still hits the cache.
	again := &Proxy{Fetch: f.p.Fetch, Dir: f.p.Dir, Key: f.p.Key, Log: f.p.Log}
	rec := httptest.NewRecorder()
	again.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code != 200 || f.hits.Load() != 1 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("after restart: %d, %d hits, %s", rec.Code, f.hits.Load(), rec.Header().Get("Content-Type"))
	}
}

func TestPrune(t *testing.T) {
	dir := t.TempDir()
	p := &Proxy{Dir: dir, MaxCache: 2500}
	old := time.Now().Add(-time.Hour)
	for i, name := range []string{"a", "b", "c", "d"} {
		path := filepath.Join(dir, name[:1], name)
		writeCached(path, make([]byte, 1000), "image/png")
		mod := old.Add(time.Duration(i) * time.Minute)
		os.Chtimes(path, mod, mod)
	}
	removed, err := p.Prune()
	if err != nil || removed != 2 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	for name, want := range map[string]bool{"a": false, "b": false, "c": true, "d": true} {
		if _, err := os.Stat(filepath.Join(dir, name, name)); (err == nil) != want {
			t.Errorf("%s exists = %v", name, err == nil)
		}
	}
}

func TestRewriteHTML(t *testing.T) {
	p := &Proxy{Key: []byte("k")}
	in := `<p>Hi &amp; bye</p><img src="https://example.com/a.png?x=1&amp;y=2" alt="A &quot;b&quot;" loading="lazy"><img src="data:image/png;base64,AA"><a href="https://example.com/a.png">link</a>`
	out := p.RewriteHTML(in, 1600)
	want := p.URL("https://example.com/a.png?x=1&y=2", 1600)
	if !strings.Contains(out, `src="`+want+`"`) {
		t.Errorf("not proxied:\n%s", out)
	}
	for _, keep := range []string{`<p>Hi &amp; bye</p>`, `alt="A &#34;b&#34;"`, `src="data:image/png;base64,AA"`, `href="https://example.com/a.png"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %q:\n%s", keep, out)
		}
	}
	if s := "<p>no images</p>"; p.RewriteHTML(s, 1600) != s {
		t.Error("changed HTML without images")
	}
}

func TestJPEGInput(t *testing.T) {
	f := setup(t)
	var b bytes.Buffer
	jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 400, 300)), nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b.Bytes()) }))
	defer srv.Close()
	rec := f.get(t, f.p.URL(srv.URL+"/x.jpg", 128))
	if cfg, _, err := image.DecodeConfig(rec.Body); err != nil || cfg.Width != 128 || cfg.Height != 96 {
		t.Errorf("%+v %v", cfg, err)
	}
}

// What a decode costs depends on the pixels' depth, not just their count:
// a 16-bit PNG well under the pixel cap is still too costly to resize.
func TestDecodedSize(t *testing.T) {
	deep := image.Config{ColorModel: color.NRGBA64Model, Width: 6300, Height: 6300}
	if deep.Width*deep.Height > maxPixels || decodedSize(deep) <= maxDecodeBytes {
		t.Errorf("16-bit 6300²: %d bytes, should pass the pixel cap but not the decode cap", decodedSize(deep))
	}
	photo := image.Config{ColorModel: color.YCbCrModel, Width: 4000, Height: 3000}
	if decodedSize(photo) > maxDecodeBytes {
		t.Errorf("a 12-megapixel JPEG should still be resized: %d bytes", decodedSize(photo))
	}
}

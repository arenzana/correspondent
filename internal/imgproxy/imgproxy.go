// Package imgproxy serves article images, thumbnails and feed icons through
// Correspondent, so third-party sites never see the reader's address and
// thumbnails arrive at the size they're shown.
//
// Only URLs the server signed can be fetched (HMAC-SHA256 over the address
// and width), so the proxy cannot be used to reach arbitrary addresses.
// Fetching goes through the SSRF-safe client; responses must really be
// images, are bounded in size and pixels, and are served with a sandboxing
// policy so an SVG can never run script on this origin.
package imgproxy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/arenzana/correspondent/internal/fetch"
)

// Widths are the sizes images can be requested at (0 = original). A fixed
// set bounds how many variants of one image can be cached.
var Widths = []int{0, 32, 64, 128, 256, 640, 960, 1280, 1600}

// MaxBytes is the largest image fetched; give the fetcher this limit.
const MaxBytes = 15 << 20

const (
	maxPixels = 40_000_000
	// The memory one decode may take. A small file can decode huge (a
	// 6,300-pixel 16-bit PNG is about 320 MB), so larger ones are served as
	// they are rather than resized, and only two decode at once.
	maxDecodeBytes = 100 << 20
	decoders       = 2
	fetchTimeout   = 20 * time.Second
	// Responses are served with this policy: an image needs nothing, and an
	// SVG opened directly must not run script on this origin.
	sandbox = "default-src 'none'; img-src data:; style-src 'unsafe-inline'; sandbox"
)

type Proxy struct {
	Fetch    *fetch.Client
	Dir      string // on-disk cache
	MaxCache int64  // bytes; oldest files are removed beyond this
	Key      []byte // signing key
	Log      *slog.Logger

	once     sync.Once
	sem      chan struct{}
	decoding chan struct{}
	mu       sync.Mutex
	inflight map[string]*call
}

type call struct {
	done chan struct{}
	data []byte
	ct   string
	err  error
}

func (p *Proxy) init() {
	p.once.Do(func() {
		p.sem = make(chan struct{}, 8)
		p.decoding = make(chan struct{}, decoders)
		p.inflight = map[string]*call{}
	})
}

func (p *Proxy) sign(raw string, width int) string {
	m := hmac.New(sha256.New, p.Key)
	fmt.Fprintf(m, "%d\x00%s", width, raw)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:18])
}

// URL returns the proxied address of an image at the given width, or "" if
// raw isn't an http(s) URL. Widths not in Widths round up to the next one.
func (p *Proxy) URL(raw string, width int) string {
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
		return ""
	}
	width = snap(width)
	return fmt.Sprintf("/img/%s/%d/%s", p.sign(raw, width), width, base64.RawURLEncoding.EncodeToString([]byte(raw)))
}

func snap(w int) int {
	if w <= 0 {
		return 0
	}
	for _, v := range Widths[1:] {
		if w <= v {
			return v
		}
	}
	return 0
}

// ServeHTTP handles /img/{sig}/{width}/{url}.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.init()
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/img/"), "/", 3)
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	width, err := strconv.Atoi(parts[1])
	rawBytes, err2 := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || err2 != nil || !slices.Contains(Widths, width) {
		http.NotFound(w, r)
		return
	}
	raw := string(rawBytes)
	if !hmac.Equal([]byte(parts[0]), []byte(p.sign(raw, width))) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	data, ct, err := p.get(r.Context(), raw, width)
	if err != nil {
		p.Log.Info("image", "error", err.Error())
		http.Error(w, "Image unavailable", http.StatusBadGateway)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Content-Security-Policy", sandbox)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	// The URL is signed and names the source and width: it never changes.
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(data)
}

func (p *Proxy) cachePath(raw string, width int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", width, raw)))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(p.Dir, name[:2], name)
}

// get returns the image from the cache or fetches it, once per key at a time.
func (p *Proxy) get(ctx context.Context, raw string, width int) ([]byte, string, error) {
	path := p.cachePath(raw, width)
	if data, ct, ok := readCached(path); ok {
		return data, ct, nil
	}
	p.mu.Lock()
	if c, ok := p.inflight[path]; ok {
		p.mu.Unlock()
		<-c.done
		return c.data, c.ct, c.err
	}
	c := &call{done: make(chan struct{})}
	p.inflight[path] = c
	p.mu.Unlock()

	c.data, c.ct, c.err = p.fetch(context.WithoutCancel(ctx), raw, width)
	if c.err == nil {
		writeCached(path, c.data, c.ct)
	}
	close(c.done)
	p.mu.Lock()
	delete(p.inflight, path)
	p.mu.Unlock()
	return c.data, c.ct, c.err
}

func (p *Proxy) fetch(ctx context.Context, raw string, width int) ([]byte, string, error) {
	p.sem <- struct{}{}
	defer func() { <-p.sem }()
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	resp, err := p.Fetch.Get(ctx, fetch.Request{URL: raw})
	if err != nil {
		return nil, "", err
	}
	if len(resp.Body) > MaxBytes {
		return nil, "", errors.New("image too large")
	}
	// Trust the bytes, not the server's Content-Type.
	ct := http.DetectContentType(resp.Body)
	if strings.Contains(strings.ToLower(resp.ContentType), "svg") && looksLikeSVG(resp.Body) {
		return resp.Body, "image/svg+xml", nil
	}
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", fmt.Errorf("not an image (%s)", ct)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(resp.Body))
	if err != nil {
		// A format the decoders don't know (AVIF, ICO…): pass it through.
		return resp.Body, ct, nil
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, "", errors.New("image has too many pixels")
	}
	// Originals, animations, images already small enough, and ones too
	// costly to decode pass through.
	if width == 0 || format == "gif" || cfg.Width <= width || decodedSize(cfg) > maxDecodeBytes {
		return resp.Body, ct, nil
	}
	p.decoding <- struct{}{}
	defer func() { <-p.decoding }()
	img, _, err := image.Decode(bytes.NewReader(resp.Body))
	if err != nil {
		return resp.Body, ct, nil
	}
	return resize(img, width)
}

// decodedSize is about how many bytes decoding an image takes.
func decodedSize(cfg image.Config) int64 {
	perPixel := int64(4)
	switch cfg.ColorModel {
	case color.RGBA64Model, color.NRGBA64Model:
		perPixel = 8
	case color.Gray16Model:
		perPixel = 2
	case color.GrayModel, color.AlphaModel:
		perPixel = 1
	case color.YCbCrModel:
		perPixel = 3
	}
	return int64(cfg.Width) * int64(cfg.Height) * perPixel
}

func looksLikeSVG(b []byte) bool {
	head := strings.ToLower(string(b[:min(len(b), 1024)]))
	return strings.Contains(head, "<svg")
}

// resize scales img to width (keeping its aspect ratio): JPEG for opaque
// images, PNG when transparency must survive.
func resize(img image.Image, width int) ([]byte, string, error) {
	b := img.Bounds()
	height := max(1, b.Dy()*width/b.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	var out bytes.Buffer
	if opaque(dst) {
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 82}); err != nil {
			return nil, "", err
		}
		return out.Bytes(), "image/jpeg", nil
	}
	if err := png.Encode(&out, dst); err != nil {
		return nil, "", err
	}
	return out.Bytes(), "image/png", nil
}

func opaque(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0xff {
			return false
		}
	}
	return true
}

// Cached files are "<content type>\n<bytes>".
func readCached(path string) ([]byte, string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false
	}
	i := bytes.IndexByte(b, '\n')
	if i <= 0 {
		return nil, "", false
	}
	now := time.Now()
	os.Chtimes(path, now, now) // recently used: last to be evicted
	return b[i+1:], string(b[:i]), true
}

func writeCached(path string, data []byte, ct string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(ct+"\n"), data...), 0o600); err == nil {
		os.Rename(tmp, path)
	}
}

// Prune removes the least recently used cached images beyond MaxCache.
func (p *Proxy) Prune() (removed int, err error) {
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	err = filepath.WalkDir(p.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, file{path, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	if err != nil || total <= p.MaxCache {
		return 0, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= p.MaxCache {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
			removed++
		}
	}
	return removed, nil
}

// RewriteHTML points every <img src> in sanitized article HTML at the proxy.
// Everything else is copied through byte for byte.
func (p *Proxy) RewriteHTML(s string, width int) string {
	if !strings.Contains(s, "<img") {
		return s
	}
	var out strings.Builder
	z := html.NewTokenizer(strings.NewReader(s))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out.String()
		}
		raw := z.Raw()
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			if tok := z.Token(); tok.DataAtom == atom.Img {
				for i, a := range tok.Attr {
					if a.Key == "src" {
						if prox := p.URL(a.Val, width); prox != "" {
							tok.Attr[i].Val = prox
						}
					}
				}
				out.WriteString(tok.String())
				continue
			}
		}
		out.Write(raw)
	}
}

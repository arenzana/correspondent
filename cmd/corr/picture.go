package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/arenzana/correspondent/internal/textify"
)

// Pictures are drawn one of two ways. In terminals that speak the Kitty
// graphics protocol (Ghostty, Kitty, and herdr inside them) the terminal
// draws the image itself, at full resolution, over placeholder cells that
// scroll like text. Elsewhere each cell is a half block (▀) whose
// foreground is one pixel and background the one below it, which shows in
// any terminal with colour, inside tmux and the like too.

// picture is an article image, fetched and decoded (nil: it couldn't be).
type picture struct {
	img  image.Image // at most keepWidth() wide
	w, h int         // its size as published
	id   int         // the image's number in the terminal (Kitty graphics)
	sent bool        // whether the terminal has it
}

const (
	maxRows    = 32               // the tallest a picture is drawn, in lines
	cellPixels = 8                // a published pixel width per column: small images stay small
	maxPicture = 15 << 20         // bytes, as the server's image proxy
	fetchWait  = 15 * time.Second // per picture
	// A cell's width over its height, near enough for most fonts.
	cellAspect = 0.5
)

var pictureClient = &http.Client{Timeout: fetchWait}

// kittyGraphics reports whether the terminal draws images itself.
// CORR_IMAGES=blocks asks for half blocks anyway, and =kitty for images.
var kittyGraphics func() bool = sync.OnceValue(func() bool {
	switch os.Getenv("CORR_IMAGES") {
	case "kitty":
		return true
	case "blocks":
		return false
	}
	if os.Getenv("TMUX") != "" || strings.HasPrefix(os.Getenv("TERM"), "screen") {
		return false
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty", "kitty":
		return true
	case "herdr":
		// herdr shows pane images in a Kitty-compatible terminal.
		return os.Getenv("GHOSTTY_RESOURCES_DIR") != "" || os.Getenv("KITTY_WINDOW_ID") != ""
	}
	t := os.Getenv("TERM")
	return t == "xterm-kitty" || t == "xterm-ghostty" || os.Getenv("KITTY_WINDOW_ID") != ""
})

// keepWidth is how many pixels across a picture keeps: enough for the
// widest measure, in pixels for the terminal, in columns for half blocks.
func keepWidth() int {
	if kittyGraphics() {
		return 1400
	}
	return 240
}

// pictureIDs numbers images for the terminal, 1–255, so the number fits
// a 256-colour foreground.
var pictureIDs atomic.Int32

// fetchPictures fetches an article's pictures, a few at a time. Images on
// Correspondent come through its proxy, so the sites never see the reader.
func fetchPictures(ctx context.Context, pics []textify.Picture) []*picture {
	out := make([]*picture, len(pics))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, p := range pics {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = fetchPicture(ctx, p.URL)
		}()
	}
	wg.Wait()
	return out
}

func fetchPicture(ctx context.Context, u string) *picture {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "image/webp,image/png,image/jpeg,image/gif")
	resp, err := pictureClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, maxPicture))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	p := &picture{img: img, w: b.Dx(), h: b.Dy(), id: int(pictureIDs.Add(1)-1)%255 + 1}
	if keep := keepWidth(); p.w > keep {
		small := image.NewRGBA(image.Rect(0, 0, keep, max(1, p.h*keep/p.w)))
		xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, b, draw.Src, nil)
		p.img = small
	}
	return p
}

// tiny is a tracking pixel or a spacer: not worth a line.
func (p *picture) tiny() bool { return p.w < 16 || p.h < 16 }

// size is the picture's size in cells, in at most cols columns.
func (p *picture) size(cols int) (int, int) {
	cols = min(cols, max(4, (p.w+cellPixels-1)/cellPixels))
	rows := max(1, int(float64(cols)*float64(p.h)/float64(p.w)*cellAspect+0.5))
	if rows > maxRows {
		rows = maxRows
		cols = max(1, int(float64(rows)*float64(p.w)/float64(p.h)/cellAspect+0.5))
	}
	return cols, rows
}

// blocks is the picture in half blocks, over the page's colour.
func (p *picture) blocks(cols, rows int, page color.Color) []string {
	dst := image.NewRGBA(image.Rect(0, 0, cols, rows*2))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(page), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), p.img, p.img.Bounds(), draw.Over, nil)
	lines := make([]string, rows)
	for y := range rows {
		var b strings.Builder
		var last string
		for x := range cols {
			t, u := dst.RGBAAt(x, 2*y), dst.RGBAAt(x, 2*y+1)
			code := fmt.Sprintf("\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm", t.R, t.G, t.B, u.R, u.G, u.B)
			if code != last {
				b.WriteString(code)
				last = code
			}
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m")
		lines[y] = b.String()
	}
	return lines
}

// placeholders are the cells the terminal draws the picture over: each
// names its row and column, and its colour names the image.
func (p *picture) placeholders(cols, rows int) []string {
	lines := make([]string, rows)
	for y := range rows {
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[38;5;%dm", p.id)
		for x := range cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(y))
			b.WriteRune(kitty.Diacritic(x))
		}
		b.WriteString("\x1b[39m")
		lines[y] = b.String()
	}
	return lines
}

// place is what the terminal needs before showing the picture at this
// size: the image the first time, then where it goes.
func (p *picture) place(cols, rows int) string {
	var b strings.Builder
	o := &kitty.Options{ID: p.id, Columns: cols, Rows: rows, VirtualPlacement: true, Quiet: 2}
	if !p.sent {
		o.Action, o.Format, o.Transmission, o.Chunk = kitty.TransmitAndPut, kitty.PNG, kitty.Direct, true
		if kitty.EncodeGraphics(&b, p.img, o) != nil {
			return ""
		}
		p.sent = true
		return b.String()
	}
	o.Action = kitty.Put
	kitty.EncodeGraphics(&b, nil, o)
	return b.String()
}

// placePictures draws the pictures where their placeholders are in an
// article rendered for the terminal (renderMarkdown at width), at the
// text's indent and measure; a picture still loading or that couldn't be
// had shows its label instead. got is nil while the pictures are being
// fetched. raw is what to send the terminal first, for Kitty graphics.
func placePictures(rendered string, pics []textify.Picture, got []*picture, width int) (text, raw string) {
	if len(pics) == 0 {
		return rendered, ""
	}
	page := lipgloss.Color(current.sheet)
	label := strings.NewReplacer("_", "", `\`, "")
	var out []string
	var send strings.Builder
	for _, l := range strings.Split(rendered, "\n") {
		plain := ansi.Strip(l)
		i, ok := textify.PlaceholderAt(plain)
		if !ok || i >= len(pics) {
			out = append(out, l)
			continue
		}
		indent := strings.Repeat(" ", len(plain)-len(strings.TrimLeft(plain, " ")))
		switch {
		case got == nil:
			out = append(out, indent+sDim.Render(label.Replace(pics[i].Label)+" (loading)"))
		case got[i] == nil:
			out = append(out, indent+sDim.Render(label.Replace(pics[i].Label)))
		case got[i].tiny():
			// Nothing to show.
		default:
			cols, rows := got[i].size(textMeasure(width) - len(indent))
			var lines []string
			if kittyGraphics() {
				send.WriteString(got[i].place(cols, rows))
				lines = got[i].placeholders(cols, rows)
			} else {
				lines = got[i].blocks(cols, rows, page)
			}
			for _, row := range lines {
				out = append(out, indent+row)
			}
		}
	}
	return strings.Join(out, "\n"), send.String()
}

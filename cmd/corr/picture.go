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
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/arenzana/correspondent/internal/textify"
)

// Pictures are drawn in text: each cell is a half block (▀) whose
// foreground is one pixel and background the one below it, so they show
// in any terminal with colour, inside tmux and the like too.

// picture is an article image, fetched and decoded (nil: it couldn't be).
type picture struct {
	img  image.Image // at most keepWidth wide
	w, h int         // its size as published
}

const (
	keepWidth  = 240              // pixels kept per picture, enough for any measure
	maxRows    = 32               // the tallest a picture is drawn, in lines
	cellPixels = 8                // a published pixel width per column: small images stay small
	maxPicture = 15 << 20         // bytes, as the server's image proxy
	fetchWait  = 15 * time.Second // per picture
)

var pictureClient = &http.Client{Timeout: fetchWait}

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
	p := &picture{img: img, w: b.Dx(), h: b.Dy()}
	if p.w > keepWidth {
		small := image.NewRGBA(image.Rect(0, 0, keepWidth, max(1, p.h*keepWidth/p.w)))
		xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, b, draw.Src, nil)
		p.img = small
	}
	return p
}

// tiny is a tracking pixel or a spacer: not worth a line.
func (p *picture) tiny() bool { return p.w < 16 || p.h < 16 }

// draw is the picture in at most cols columns, over the page's colour.
func (p *picture) draw(cols int, page color.Color) []string {
	cols = min(cols, max(4, (p.w+cellPixels-1)/cellPixels))
	rows := max(1, cols*p.h/p.w/2)
	if rows > maxRows {
		rows = maxRows
		cols = max(1, rows*2*p.w/p.h)
	}
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

// placePictures draws the pictures where their placeholders are in an
// article rendered for the terminal (renderMarkdown at width), at the
// text's indent and measure; a
// picture still loading or that couldn't be had shows its label instead.
// got is nil while the pictures are being fetched.
func placePictures(rendered string, pics []textify.Picture, got []*picture, width int) string {
	if len(pics) == 0 {
		return rendered
	}
	page := lipgloss.Color(current.sheet)
	label := strings.NewReplacer("_", "", `\`, "")
	var out []string
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
			for _, row := range got[i].draw(textMeasure(width)-len(indent), page) {
				out = append(out, indent+row)
			}
		}
	}
	return strings.Join(out, "\n")
}

package main

import (
	"fmt"
	"image/color"
	"strconv"
	"time"

	"charm.land/glamour/v2/ansi"
	gstyles "charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
)

// palette is one of the web app's themes (ui/src/app.css), token for token.
type palette struct {
	name                                               string
	dark                                               bool
	fog, sheet, ink, slate, rule, accent, seal, danger string
}

var palettes = map[string]palette{
	"day":     {"day", false, "#eef0f3", "#fcfcfb", "#2a3349", "#5f6879", "#d9dee7", "#2742c7", "#b7791f", "#b4322b"},
	"evening": {"evening", false, "#e6dcc8", "#f3ebdc", "#3b3123", "#6a5c48", "#d8cbb2", "#2f4a8a", "#9a6516", "#b4322b"},
	"night":   {"night", true, "#141a25", "#1b2230", "#c5ccd8", "#8a94a7", "#2b3446", "#8a9cf0", "#d9a24a", "#ff8a7a"},
}

// current is the palette in use; chooseTheme sets it.
var current = palettes["night"]

// chooseTheme picks the palette: --theme if given, else the reader's web
// theme (Day, Evening, Night desk; Sun by the time of day), else the
// terminal's own background (the web app's "System").
func chooseTheme(override, web string) palette {
	for _, t := range []string{override, web} {
		switch t {
		case "day", "light":
			return palettes["day"]
		case "evening":
			return palettes["evening"]
		case "night", "dark":
			return palettes["night"]
		case "sun":
			// Without the reader's location here, by the clock: day until
			// 19:00, evening until 22:00, night until 07:00.
			switch h := time.Now().Hour(); {
			case h >= 7 && h < 19:
				return palettes["day"]
			case h >= 19 && h < 22:
				return palettes["evening"]
			default:
				return palettes["night"]
			}
		}
	}
	if darkBackground() {
		return palettes["night"]
	}
	return palettes["day"]
}

// apply makes p the palette for the reader and for command output.
func (p palette) apply() {
	current = p
	accent, slate, seal, danger = lipgloss.Color(p.accent), lipgloss.Color(p.slate), lipgloss.Color(p.seal), lipgloss.Color(p.danger)
	sDim = lipgloss.NewStyle().Foreground(slate)
	sAccent = lipgloss.NewStyle().Foreground(accent)
	sSeal = lipgloss.NewStyle().Foreground(seal)
	sBold = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.ink))
	sErr = lipgloss.NewStyle().Foreground(danger)
	sTitle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	styles.accent, styles.dim, styles.err, styles.star = sAccent, sDim, sErr, sSeal
	styles.tag = sDim.Italic(true)
}

func (p palette) color(hex string) color.Color { return lipgloss.Color(hex) }

// wash is the selection tint: the accent at 14% over the sheet, as on the web.
func (p palette) wash() color.Color { return lipgloss.Color(mix(p.accent, p.sheet, 0.14)) }

func mix(a, b string, t float64) string {
	pa, pb := rgb(a), rgb(b)
	var out [3]int
	for i := range out {
		out[i] = int(float64(pa[i])*t + float64(pb[i])*(1-t) + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2])
}

func rgb(hex string) [3]int {
	var c [3]int
	for i := range c {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c[i] = int(v)
	}
	return c
}

// glamourStyle is Glamour's style in the palette's colours: headings in the
// reading ink, the title in accent, links and quotes as on the web.
func (p palette) glamourStyle() ansi.StyleConfig {
	s := gstyles.LightStyleConfig
	if p.dark {
		s = gstyles.DarkStyleConfig
	}
	str := func(v string) *string { return &v }
	yes := true
	s.Document.Color = str(p.ink)
	s.Document.BackgroundColor = nil
	s.Text.Color = str(p.ink)
	s.Paragraph.Color = str(p.ink)
	s.Heading.Color = str(p.ink)
	s.Heading.BackgroundColor = nil
	s.Heading.Bold = &yes
	s.H1.Color = str(p.accent)
	s.H1.BackgroundColor = nil
	s.H1.Prefix, s.H1.Suffix = "", ""
	s.H2.Prefix, s.H3.Prefix, s.H4.Prefix, s.H5.Prefix, s.H6.Prefix = "", "", "", "", ""
	s.Link.Color = str(p.slate)
	s.LinkText.Color = str(p.accent)
	s.Image.Color = str(p.slate)
	s.ImageText.Color = str(p.slate)
	s.BlockQuote.Color = str(p.slate)
	s.BlockQuote.IndentToken = str("│ ")
	s.HorizontalRule.Color = str(p.rule)
	s.Code.Color = str(p.ink)
	s.Code.BackgroundColor = str(p.fog)
	s.Strong.Color = str(p.ink)
	s.Emph.Color = str(p.ink)
	s.Item.Color = str(p.ink)
	s.Enumeration.Color = str(p.slate)
	return s
}

package main

import (
	"os"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
)

var darkBackground = sync.OnceValue(func() bool {
	if !stdoutTTY {
		return true
	}
	return lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
})

// renderMarkdown renders an article for the terminal, wrapped to width (at
// most a comfortable reading measure). Off a terminal it stays Markdown.
func renderMarkdown(md string, width int) string {
	if !stdoutTTY {
		return md
	}
	r, err := glamour.NewTermRenderer(glamour.WithStyles(current.glamourStyle()), glamour.WithWordWrap(min(width-4, 92)))
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}

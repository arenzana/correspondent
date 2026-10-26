package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"golang.org/x/term"

	"github.com/arenzana/correspondent/internal/client"
)

// Exit codes, documented in docs/cli.md.
const (
	exitOK          = 0
	exitError       = 1
	exitUsage       = 2
	exitAuth        = 3
	exitNotFound    = 4
	exitUnavailable = 5
)

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

func exitCode(err error) int {
	var u usageError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &u):
		return exitUsage
	case errors.Is(err, client.ErrAuth):
		return exitAuth
	case errors.Is(err, client.ErrNotFound):
		return exitNotFound
	case errors.Is(err, client.ErrUnavailable):
		return exitUnavailable
	}
	return exitError
}

var stdoutTTY = term.IsTerminal(int(os.Stdout.Fd()))

// emit writes v as JSON (with --json, or when stdout isn't a terminal and
// no human form exists), else calls human.
func emit(asJSON bool, v any, human func(w io.Writer)) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(v)
	}
	human(os.Stdout)
	return nil
}

// fail prints an error: JSON on stderr with --json, a sentence otherwise.
func printError(asJSON bool, err error) {
	if asJSON {
		b, _ := json.Marshal(map[string]any{"error": err.Error(), "code": exitCode(err)})
		fmt.Fprintln(os.Stderr, string(b))
		return
	}
	fmt.Fprintln(os.Stderr, styles.err.Render("corr: "+err.Error()))
}

// The app's palette: accent blue, ink, slate.
var styles = struct {
	accent, dim, err, title, ok, star, tag lipgloss.Style
}{
	accent: lipgloss.NewStyle().Foreground(lipgloss.Color("#6b82f0")),
	dim:    lipgloss.NewStyle().Foreground(lipgloss.Color("#8a94a7")),
	err:    lipgloss.NewStyle().Foreground(lipgloss.Color("#e5675c")),
	title:  lipgloss.NewStyle().Bold(true),
	ok:     lipgloss.NewStyle().Foreground(lipgloss.Color("#5aa56a")),
	star:   lipgloss.NewStyle().Foreground(lipgloss.Color("#d9a24a")),
	tag:    lipgloss.NewStyle().Foreground(lipgloss.Color("#8a94a7")).Italic(true),
}

func line(w io.Writer, s string) { lipgloss.Fprintln(w, s) }

// ago is a short relative time.
func ago(unix int64) string {
	if unix == 0 {
		return "never"
	}
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return time.Unix(unix, 0).Format("Jan 2, 2006")
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n || n < 2 {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return 100
}

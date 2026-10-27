package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	tea "charm.land/bubbletea/v2"
)

// listen reads an article aloud: it saves the audio with -o (- for stdout),
// or else plays it with the system's player until it ends or Ctrl-C.
func (a *app) listen(args []string) error {
	var out string
	_, pos, err := flags("listen", args, func(fs *flag.FlagSet) {
		fs.StringVar(&out, "o", "", "")
	})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("listen needs one article ID")
	}
	if out == "" && !a.asJSON {
		status("Preparing the audio. A long article can take a minute.")
	}
	mp3, err := a.api.Audio(a.ctx, pos[0])
	if err != nil {
		return err
	}
	if out == "-" {
		_, err := os.Stdout.Write(mp3)
		return err
	}
	play := out == ""
	if play {
		out = filepath.Join(os.TempDir(), "corr-"+pos[0]+".mp3")
	}
	if err := os.WriteFile(out, mp3, 0o600); err != nil {
		return err
	}
	if play && !a.asJSON {
		if cmd := player(a.ctx, out); cmd != nil {
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			err := cmd.Run()
			os.Remove(out)
			if errors.Is(a.ctx.Err(), context.Canceled) {
				return nil // Ctrl-C
			}
			return err
		}
		status("No audio player found (afplay, mpv, ffplay or mpg123); saved the audio instead.")
	}
	return emit(a.asJSON, map[string]any{"id": pos[0], "file": out, "bytes": len(mp3)}, func(w io.Writer) {
		line(w, out)
	})
}

// player returns a command that plays an MP3 file and exits at its end, or
// nil when there's no player.
func player(ctx context.Context, file string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		return exec.CommandContext(ctx, "afplay", file)
	}
	if runtime.GOOS == "windows" {
		// The file travels as an environment variable, never as script.
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			`Add-Type -AssemblyName presentationCore; $p = New-Object System.Windows.Media.MediaPlayer; $p.Open([uri]$env:CORR_FILE);
			 while (-not $p.NaturalDuration.HasTimeSpan) { Start-Sleep -Milliseconds 100 }; $p.Play();
			 Start-Sleep -Seconds $p.NaturalDuration.TimeSpan.TotalSeconds`)
		cmd.Env = append(os.Environ(), "CORR_FILE="+file)
		return cmd
	}
	for _, p := range [][]string{{"mpv", "--no-video", "--really-quiet"}, {"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet"}, {"mpg123", "-q"}} {
		if _, err := exec.LookPath(p[0]); err == nil {
			return exec.CommandContext(ctx, p[0], append(p[1:], file)...)
		}
	}
	return nil
}

// status tells a person at the terminal what's happening, on stderr.
func status(msg string) {
	fmt.Fprintln(os.Stderr, sDim.Render(msg))
}

// --- in the reader (tui.go) ------------------------------------------------------

// listen reads the open article aloud in the background, with the system's
// player; l again stops it. Audio is made only for the article asked for.
func (m *reader) listen() tea.Cmd {
	e := m.article
	if e == nil {
		return nil
	}
	if m.playingID != "" {
		was := m.playingID
		m.stopListening()
		if was == e.ID {
			m.status = "Stopped reading aloud."
			return nil
		}
	}
	m.playSeq++
	seq, id := m.playSeq, e.ID
	m.status = "Preparing the audio. A long article can take a minute."
	return func() tea.Msg {
		mp3, err := m.app.api.Audio(m.ctx, id)
		if err != nil {
			return errMsg{err}
		}
		f, err := os.CreateTemp("", "corr-*.mp3")
		if err != nil {
			return errMsg{err}
		}
		_, err = f.Write(mp3)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(f.Name())
			return errMsg{err}
		}
		return playMsg{id: id, file: f.Name(), seq: seq}
	}
}

// played starts the audio that's ready, or notes that it ended.
func (m *reader) played(msg playMsg) tea.Cmd {
	if msg.seq != m.playSeq {
		// Asked for again, or stopped, since.
		if !msg.done {
			os.Remove(msg.file)
		}
		return nil
	}
	if msg.done {
		m.playingID, m.stopPlay = "", nil
		m.status = "Finished reading aloud."
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	cmd := player(ctx, msg.file)
	if cmd == nil {
		cancel()
		m.status = "No audio player found (afplay, mpv, ffplay or mpg123). The audio is in " + msg.file
		return nil
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		os.Remove(msg.file)
		return func() tea.Msg { return errMsg{err} }
	}
	m.playingID, m.stopPlay = msg.id, cancel
	m.status = "Reading aloud. Press l to stop."
	return func() tea.Msg {
		cmd.Wait()
		os.Remove(msg.file)
		return playMsg{id: msg.id, seq: msg.seq, done: true}
	}
}

// stopListening stops the audio playing, if any.
func (m *reader) stopListening() {
	if m.stopPlay != nil {
		m.stopPlay()
	}
	m.playingID, m.stopPlay = "", nil
	m.playSeq++
}

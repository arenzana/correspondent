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

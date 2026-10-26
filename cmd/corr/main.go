// Command corr reads and manages a Correspondent server from the terminal.
//
// Run it with no arguments for the interactive reader. Every other command
// is made for scripts and AI agents as much as for people: --json prints
// stable JSON (docs/cli.md), nothing ever prompts except "corr auth login"
// on a terminal, and exit codes say what went wrong:
//
//	0 ok, 1 error, 2 usage, 3 not signed in or not allowed, 4 not found,
//	5 server unavailable or busy.
//
// AI tools connect to the server itself for MCP: https://<server>/mcp with
// an API token (see docs/cli.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/arenzana/correspondent/internal/client"
	"github.com/arenzana/correspondent/internal/version"
)

const usage = `corr reads and manages a Correspondent server.

Usage:
  corr                                    open the reader (on a terminal)
  corr auth login [--server URL] [--username NAME] [--password-stdin] [--read-only]
  corr auth status | logout
  corr list [--view unread|starred|today|all] [--feed ID] [--folder ID]
            [--search TEXT] [--limit N] [--cursor C] [--oldest]
  corr read ID [--full] [--html] [--keep-unread]
  corr mark read|unread|star|unstar ID...
  corr mark-all-read [--feed ID | --folder ID | --view today]
  corr feeds | folders
  corr discover URL [--ai]
  corr subscribe URL [--title T] [--folder F] [--ai]
  corr unsubscribe FEED-ID
  corr feed FEED-ID rename TITLE | move FOLDER | unfile | pause | resume |
                    refresh | full-text on|off | notify on|off | url ADDRESS
  corr opml export [FILE] | opml import FILE
  corr watch [--feed ID] [--folder ID] [--notify]   new articles as they arrive
  corr version

Global flags (before or after the command):
  --json            print JSON (stable; see docs/cli.md)
  --server URL      server address (default: saved, or $CORR_SERVER)
  --token TOKEN     API token (default: saved, or $CORR_TOKEN)
  --theme NAME      day, evening or night (default: your web theme)

Exit codes: 0 ok, 1 error, 2 usage, 3 auth, 4 not found, 5 unavailable.
`

type app struct {
	ctx    context.Context
	cfg    config
	api    *client.Client
	asJSON bool
	theme  string // --theme: day, evening or night
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	args, asJSON, server, token, theme, err := globalFlags(os.Args[1:])
	a := &app{ctx: ctx, asJSON: asJSON, theme: theme}
	if err == nil {
		err = a.setup(server, token)
	}
	if err == nil {
		err = a.run(args)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print(usage)
			os.Exit(exitOK)
		}
		printError(asJSON, err)
		if exitCode(err) == exitUsage && !asJSON {
			fmt.Fprintln(os.Stderr, "Run corr --help for usage.")
		}
		os.Exit(exitCode(err))
	}
}

// globalFlags takes --json, --server and --token from anywhere in the
// arguments, so they work before or after the command.
func globalFlags(in []string) (rest []string, asJSON bool, server, token, theme string, err error) {
	for i := 0; i < len(in); i++ {
		arg := in[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") {
			rest = append(rest, arg)
			continue
		}
		switch name {
		case "json":
			asJSON = true
		case "server", "token", "theme":
			if !hasValue {
				if i+1 >= len(in) {
					return nil, false, "", "", "", usagef("--%s needs a value", name)
				}
				i++
				value = in[i]
			}
			switch name {
			case "server":
				server = value
			case "token":
				token = value
			default:
				theme = value
			}
		case "h", "help":
			return nil, false, "", "", "", flag.ErrHelp
		default:
			rest = append(rest, arg)
		}
	}
	return rest, asJSON, server, token, theme, nil
}

func (a *app) setup(server, token string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if server != "" {
		cfg.Server = server
	}
	if token != "" {
		cfg.Token = token
	}
	cfg.Server = strings.TrimRight(cfg.Server, "/")
	a.cfg = cfg
	a.api = &client.Client{Server: cfg.Server, Token: cfg.Token, UserAgent: "corr/" + version.Version}
	// Command output takes the theme too, on a terminal.
	if stdoutTTY && !a.asJSON {
		chooseTheme(a.theme, "").apply()
	}
	return nil
}

func (a *app) run(args []string) error {
	if len(args) == 0 {
		if stdoutTTY && !a.asJSON {
			return a.tui()
		}
		return flag.ErrHelp
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "auth":
		return a.auth(rest)
	case "list", "ls":
		return a.list(rest)
	case "read", "show":
		return a.read(rest)
	case "mark":
		return a.mark(rest)
	case "mark-all-read":
		return a.markAll(rest)
	case "feeds":
		return a.feeds(rest)
	case "folders":
		return a.folders(rest)
	case "discover":
		return a.discover(rest)
	case "subscribe", "add":
		return a.subscribe(rest)
	case "unsubscribe", "remove":
		return a.unsubscribe(rest)
	case "feed":
		return a.feed(rest)
	case "opml":
		return a.opml(rest)
	case "watch":
		return a.watch(rest)
	case "version":
		return emit(a.asJSON, map[string]string{"version": version.Version, "commit": version.Commit}, func(w io.Writer) {
			line(w, "corr "+version.String())
		})
	case "help":
		return flag.ErrHelp
	}
	return usagef("unknown command %q", cmd)
}

// flags parses a command's own flags, allowing them after positionals.
func flags(name string, args []string, define func(*flag.FlagSet)) (*flag.FlagSet, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	define(fs)
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, nil, usagef("%s: %v", name, err)
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return fs, positional, nil
}

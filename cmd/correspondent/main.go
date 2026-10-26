// Command correspondent runs the feed reader.
//
//	correspondent serve [-addr :8080] [-data ./data]
//	correspondent user add <username>          (password read from stdin)
//	correspondent user api-password <username> (password read from stdin)
//	correspondent demo <username>              (subscribe to a starter set of feeds)
//	correspondent import-freshrss <username> -opml feeds.opml.xml [-starred starred.json] [-subscriptions subs.json]
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/arenzana/correspondent/internal/api"
	"github.com/arenzana/correspondent/internal/fetch"
	"github.com/arenzana/correspondent/internal/greader"
	"github.com/arenzana/correspondent/internal/ingest"
	"github.com/arenzana/correspondent/internal/migrate"
	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/server"
	"github.com/arenzana/correspondent/internal/store"
	"github.com/arenzana/correspondent/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "correspondent:", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("correspondent", flag.ExitOnError)
	addr := fs.String("addr", envOr("CORRESPONDENT_ADDR", ":8080"), "listen address")
	dataDir := fs.String("data", envOr("CORRESPONDENT_DATA", "./data"), "directory for the SQLite database")
	insecure := fs.Bool("insecure-cookies", os.Getenv("CORRESPONDENT_INSECURE_COOKIES") == "1", "allow session cookies over plain HTTP (local development only)")
	trusted := fs.String("trusted-proxies", os.Getenv("CORRESPONDENT_TRUSTED_PROXIES"), "comma-separated CIDRs of reverse proxies whose X-Forwarded-For is trusted")
	opmlPath := fs.String("opml", "", "import-freshrss: OPML export")
	starredPath := fs.String("starred", "", "import-freshrss: starred JSON export")
	subsPath := fs.String("subscriptions", "", "import-freshrss: saved subscription/list JSON, to keep feed IDs")
	allowPrivate := fs.Bool("allow-private-feeds", false, "allow fetching feeds on private networks (development only)")

	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var sub string
	if cmd == "user" && len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	var positional []string
	for len(args) > 0 {
		if !strings.HasPrefix(args[0], "-") {
			positional, args = append(positional, args[0]), args[1:]
			continue
		}
		fs.Parse(args)
		args = fs.Args()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return err
	}
	st, err := store.Open(ctx, filepath.Join(*dataDir, "correspondent.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	fetcher := fetch.New(fetch.Options{AllowPrivate: *allowPrivate})
	ing := &ingest.Service{Store: st, Fetch: fetcher, Log: log}

	switch cmd {
	case "serve":
		proxies, err := parsePrefixes(*trusted)
		if err != nil {
			return err
		}
		return serve(ctx, log, st, ing, *addr, !*insecure, ratelimit.ClientIP{Trusted: proxies})
	case "user":
		if len(positional) != 1 {
			return errors.New("usage: correspondent user add|api-password <username>")
		}
		pw, err := readPassword()
		if err != nil {
			return err
		}
		switch sub {
		case "add":
			_, err = st.CreateUser(ctx, positional[0], pw)
		case "api-password":
			var id int64
			if id, err = st.UserIDByName(ctx, positional[0]); err == nil {
				err = st.SetAPIPassword(ctx, id, pw)
			}
		default:
			return fmt.Errorf("unknown user command %q", sub)
		}
		return err
	case "demo":
		if len(positional) != 1 {
			return errors.New("usage: correspondent demo <username>")
		}
		return demo(ctx, st, ing, positional[0])
	case "import-freshrss":
		if len(positional) != 1 || *opmlPath == "" {
			return errors.New("usage: correspondent import-freshrss <username> -opml FILE [-starred FILE] [-subscriptions FILE]")
		}
		return importFreshRSS(ctx, st, positional[0], *opmlPath, *starredPath, *subsPath)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func serve(ctx context.Context, log *slog.Logger, st *store.Store, ing *ingest.Service, addr string, secure bool, ip ratelimit.ClientIP) error {
	// One limiter for both sign-in paths: 10 failures per client per 10 minutes.
	limiter := ratelimit.New(10, 10*time.Minute)
	a := &api.API{Store: st, Ingest: ing, Log: log, Secure: secure, Limiter: limiter, ClientIP: ip}
	ing.Notify = a.Notify
	g := &greader.Server{Store: st, Log: log, Limiter: limiter, ClientIP: ip}
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(st, a, g, web.Dist(), secure),
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    64 << 10,
		IdleTimeout:       120 * time.Second,
	}
	go ing.Run(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("listening", "addr", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func readPassword() (string, error) {
	fmt.Fprint(os.Stderr, "Password: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	pw := strings.TrimRight(line, "\r\n")
	if len(pw) < 10 {
		return "", errors.New("use a password of at least 10 characters")
	}
	return pw, nil
}

// demo subscribes a user to a small, varied starter set so the reader has
// something to show on a fresh install.
func demo(ctx context.Context, st *store.Store, ing *ingest.Service, username string) error {
	uid, err := st.UserIDByName(ctx, username)
	if err != nil {
		return fmt.Errorf("user %q: %w", username, err)
	}
	starter := map[string][]string{
		"Technology": {"https://daringfireball.net/feeds/main", "https://feeds.arstechnica.com/arstechnica/index", "https://lwn.net/headlines/rss", "https://simonwillison.net/atom/everything/"},
		"Design":     {"https://www.smashingmagazine.com/feed/", "https://alistapart.com/main/feed/"},
		"Reading":    {"https://kottke.org/feed/json", "https://www.theverge.com/rss/index.xml"},
		"Faith":      {"https://www.challies.com/feed/", "https://www.desiringgod.org/blog.rss"},
	}
	for folder, urls := range starter {
		fid, err := st.EnsureFolder(ctx, uid, folder)
		if err != nil {
			return err
		}
		for _, u := range urls {
			if _, err := ing.Subscribe(ctx, uid, u, "", &fid); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", u, err)
			}
		}
	}
	return nil
}

func importFreshRSS(ctx context.Context, st *store.Store, username, opmlPath, starredPath, subsPath string) error {
	uid, err := st.UserIDByName(ctx, username)
	if err != nil {
		return fmt.Errorf("user %q: %w", username, err)
	}
	open := func(p string) (*os.File, error) {
		if p == "" {
			return nil, nil
		}
		return os.Open(p)
	}
	var src migrate.Sources
	o, err := open(opmlPath)
	if err != nil {
		return err
	}
	defer o.Close()
	src.OPML = o
	if f, err := open(starredPath); err != nil {
		return err
	} else if f != nil {
		defer f.Close()
		src.Starred = f
	}
	if f, err := open(subsPath); err != nil {
		return err
	} else if f != nil {
		defer f.Close()
		src.Subscriptions = f
	}
	rep, err := migrate.FreshRSS(ctx, st, uid, src)
	if rep != nil {
		fmt.Printf("folders %d, feeds %d (FreshRSS IDs kept: %d), starred articles %d\n", rep.Folders, rep.Feeds, rep.FeedIDsKept, rep.Starred)
		for _, w := range rep.Warnings {
			fmt.Println("warning:", w)
		}
	}
	return err
}

func parsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", part, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

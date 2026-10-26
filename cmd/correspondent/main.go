// Command correspondent runs the feed reader.
//
//	correspondent serve [-addr :8080] [-data ./data]
//	correspondent healthcheck                  (exit 0 if the local server is ready; for Docker)
//	correspondent backup <file>                (consistent online snapshot of the database)
//	correspondent user add <username>          (password read from stdin)
//	correspondent user api-password <username> (password read from stdin)
//	correspondent user role <username> admin|user
//	correspondent invite [-admin] [-public-url URL]  (a one-time link for a new account)
//	correspondent demo <username>              (subscribe to a starter set of feeds)
//	correspondent import-freshrss <username> -opml feeds.opml.xml [-starred starred.json] [-subscriptions subs.json]
//
// FreshRSS migration (see docs/migration.md):
//
//	correspondent migrate import <username> -export U.sqlite -system-config config.php -user-config users/U/config.php
//	correspondent migrate verify <username> -freshrss URL -correspondent URL -api-password-file FILE
//	correspondent migrate rollback <username> -freshrss URL -api-password-file FILE [-apply]
//	correspondent legacy-auth on|off <username>
//	correspondent feeds list <username>
//	correspondent feeds resume <username> [feed-id...]   (no IDs: every paused feed)
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/go-webauthn/webauthn/webauthn"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/arenzana/correspondent/internal/api"
	"github.com/arenzana/correspondent/internal/extract"
	"github.com/arenzana/correspondent/internal/fetch"
	"github.com/arenzana/correspondent/internal/greader"
	"github.com/arenzana/correspondent/internal/imgproxy"
	"github.com/arenzana/correspondent/internal/ingest"
	"github.com/arenzana/correspondent/internal/migrate"
	"github.com/arenzana/correspondent/internal/push"
	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/secretbox"
	"github.com/arenzana/correspondent/internal/server"
	"github.com/arenzana/correspondent/internal/store"
	"github.com/arenzana/correspondent/internal/version"
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
	retention := fs.Int("retention-days", envInt("CORRESPONDENT_RETENTION_DAYS", 0), "delete read, unstarred articles older than this that have left their feed (0 = keep everything)")
	imageCache := fs.Int("image-cache-mb", envInt("CORRESPONDENT_IMAGE_CACHE_MB", 512), "disk space for proxied images (0 = load images directly from their sites)")
	publicURL := fs.String("public-url", os.Getenv("CORRESPONDENT_PUBLIC_URL"), "the https address readers use (enables passkeys; used in invite links)")
	inviteAdmin := fs.Bool("admin", false, "invite: the new account is an admin")
	noPoll := fs.Bool("no-poll", os.Getenv("CORRESPONDENT_NO_POLL") == "1", "serve without polling feeds (used during migration)")
	exportPath := fs.String("export", "", "migrate import: FreshRSS SQLite export")
	sysConfig := fs.String("system-config", "", "migrate import: FreshRSS data/config.php")
	userConfig := fs.String("user-config", "", "migrate import: FreshRSS data/users/<name>/config.php")
	noLegacy := fs.Bool("no-legacy-auth", false, "migrate import: do not accept the token FreshRSS issued to apps")
	freshURL := fs.String("freshrss", "", "migrate verify/rollback: FreshRSS Google Reader API base, e.g. https://rss.example.org/api/greader.php")
	corrURL := fs.String("correspondent", "http://127.0.0.1:8080/api/greader.php", "migrate verify: Correspondent Google Reader API base")
	apiPWFile := fs.String("api-password-file", "", "migrate verify/rollback: file holding the API (app) password")
	apply := fs.Bool("apply", false, "migrate rollback: send the changes (default: only show them)")
	full := fs.Bool("full", false, "migrate verify: compare every item rather than a sample")

	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var sub string
	if (cmd == "user" || cmd == "migrate" || cmd == "legacy-auth" || cmd == "feeds") && len(args) > 0 {
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
	// The health check must not touch the database: it runs alongside the
	// server, often every few seconds.
	if cmd == "healthcheck" {
		return healthcheck(ctx, *addr)
	}
	if cmd == "version" {
		fmt.Println("correspondent", version.String())
		return nil
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return err
	}
	st, err := store.Open(ctx, filepath.Join(*dataDir, "correspondent.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	fetcher := fetch.New(fetch.Options{AllowPrivate: *allowPrivate})
	ing := &ingest.Service{Store: st, Fetch: fetcher, Log: log, RetentionDays: *retention,
		Extract: &extract.Extractor{Fetch: fetcher}}
	switch cmd {
	case "serve":
		proxies, err := parsePrefixes(*trusted)
		if err != nil {
			return err
		}
		if err := bootstrap(ctx, log, st); err != nil {
			return err
		}
		if err := ing.SeedBuiltins(ctx); err != nil {
			return err
		}
		var images *imgproxy.Proxy
		if *imageCache > 0 {
			key, err := st.Secret(ctx, store.SettingImageKey)
			if err != nil {
				return err
			}
			images = &imgproxy.Proxy{
				Fetch:    fetch.New(fetch.Options{AllowPrivate: *allowPrivate, MaxBytes: imgproxy.MaxBytes}),
				Dir:      filepath.Join(*dataDir, "imgcache"),
				MaxCache: int64(*imageCache) << 20,
				Key:      key,
				Log:      log,
			}
		}
		// Readers' AI keys are sealed with a key file beside the database,
		// which backups leave out (see README).
		keyring, err := secretbox.Load(filepath.Join(*dataDir, "keyring"))
		if err != nil {
			return err
		}
		var wa *webauthn.WebAuthn
		if *publicURL != "" {
			if wa, err = api.NewWebAuthn(*publicURL); err != nil {
				return err
			}
		} else {
			log.Warn("passkeys are off: set CORRESPONDENT_PUBLIC_URL to the address readers use")
		}
		return serve(ctx, log, st, ing, images, keyring, wa, *dataDir, *publicURL, *addr, !*insecure, ratelimit.ClientIP{Trusted: proxies}, !*noPoll)
	case "user":
		if sub == "role" {
			if len(positional) != 2 {
				return errors.New("usage: correspondent user role <username> admin|user")
			}
			id, err := st.UserIDByName(ctx, positional[0])
			if err != nil {
				return err
			}
			return st.SetRole(ctx, id, positional[1])
		}
		if len(positional) != 1 {
			return errors.New("usage: correspondent user add|api-password|role <username>")
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
	case "invite":
		// An invitation link from the command line, for the first readers or
		// when no admin can sign in.
		role := store.RoleUser
		if *inviteAdmin {
			role = store.RoleAdmin
		}
		token, err := st.CreateInvite(ctx, store.InviteAccount, 0, role, "from the command line", 0, 7*24*time.Hour)
		if err != nil {
			return err
		}
		base := strings.TrimRight(*publicURL, "/")
		if base == "" {
			base = "https://<your server>"
		}
		fmt.Printf("%s/#/invite/%s\n(valid for 7 days, once)\n", base, token)
		return nil
	case "backup":
		if len(positional) != 1 {
			return errors.New("usage: correspondent backup <file>")
		}
		if err := st.Backup(ctx, positional[0]); err != nil {
			return err
		}
		fmt.Println("backup written and verified:", positional[0])
		return nil
	case "migrate":
		if len(positional) != 1 {
			return errors.New("usage: correspondent migrate import|verify|rollback <username> [flags]")
		}
		m := migration{st: st, user: positional[0], freshURL: *freshURL, corrURL: *corrURL, apiPWFile: *apiPWFile, full: *full}
		switch sub {
		case "import":
			return m.importExport(ctx, migrate.Export{SQLite: *exportPath, SystemConfig: *sysConfig, UserConfig: *userConfig,
				Username: positional[0], LegacyAuth: !*noLegacy})
		case "verify":
			return m.verify(ctx)
		case "rollback":
			return m.rollback(ctx, *apply)
		default:
			return fmt.Errorf("unknown migrate command %q", sub)
		}
	case "feeds":
		if len(positional) < 1 {
			return errors.New("usage: correspondent feeds list|resume <username> [feed-id...]")
		}
		return feedsCommand(ctx, st, sub, positional[0], positional[1:])
	case "legacy-auth":
		if len(positional) != 1 || (sub != "on" && sub != "off") {
			return errors.New("usage: correspondent legacy-auth on|off <username>")
		}
		id, err := st.UserIDByName(ctx, positional[0])
		if err != nil {
			return err
		}
		if err := st.SetLegacyAuth(ctx, id, sub == "on"); err != nil {
			return err
		}
		fmt.Printf("FreshRSS app token for %s: %s\n", positional[0], sub)
		return nil
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

func serve(ctx context.Context, log *slog.Logger, st *store.Store, ing *ingest.Service, images *imgproxy.Proxy, keyring *secretbox.Box, wa *webauthn.WebAuthn, dataDir, publicURL, addr string, secure bool, ip ratelimit.ClientIP, poll bool) error {
	// One limiter for both sign-in paths: 10 failures per client per 10 minutes.
	limiter := ratelimit.New(10, 10*time.Minute)
	notifier := &push.Notifier{Store: st, Log: log}
	a := &api.API{Store: st, Ingest: ing, Log: log, Secure: secure, Limiter: limiter, ClientIP: ip, Push: notifier, Images: images,
		Keyring: keyring, AITransport: ing.Fetch.Transport(), WebAuthn: wa, DataDir: dataDir, PublicURL: publicURL}
	ing.AIFor = a.AIForApps
	ing.Notify = a.Notify
	ing.OnNewEntries = notifier.NewArticles
	st.OnStateChange = a.NotifyState
	g := &greader.Server{Store: st, Log: log, Limiter: limiter, ClientIP: ip, Subscriber: ing, AudioLink: a.AudioLink}
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(st, a, g, imageHandler(images), web.Dist(), secure),
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    64 << 10,
		IdleTimeout:       120 * time.Second,
	}
	if images != nil {
		go pruneImages(ctx, log, images)
	}
	go sweepAudio(ctx, log, a)
	if poll {
		go ing.Run(ctx)
	} else {
		log.Warn("feed polling is off (-no-poll)")
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("listening", "addr", addr, "version", version.Version, "commit", version.Short())
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// imageHandler avoids handing server.New a typed nil.
func imageHandler(p *imgproxy.Proxy) http.Handler {
	if p == nil {
		return nil
	}
	return p
}

// pruneImages keeps the image cache within its size, hourly.
func pruneImages(ctx context.Context, log *slog.Logger, p *imgproxy.Proxy) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if n, err := p.Prune(); err != nil {
			log.Warn("image cache", "error", err.Error())
		} else if n > 0 {
			log.Info("image cache pruned", "removed", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sweepAudio removes read-aloud audio of deleted articles, daily.
func sweepAudio(ctx context.Context, log *slog.Logger, a *api.API) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		if n, err := a.SweepAudio(ctx); err != nil {
			log.Warn("audio sweep", "error", err.Error())
		} else if n > 0 {
			log.Info("audio swept", "removed", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// bootstrap creates the first account from Docker-style secret files, so a
// fresh container is usable without an interactive step. Passwords are only
// ever read from files, never from environment variables, which leak into
// process listings and `docker inspect`.
func bootstrap(ctx context.Context, log *slog.Logger, st *store.Store) error {
	username := os.Getenv("CORRESPONDENT_USER")
	if username == "" {
		return nil
	}
	if _, err := st.UserIDByName(ctx, username); err == nil {
		return nil // already set up; secrets are not re-applied on restart
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	pw, err := secretFile("CORRESPONDENT_PASSWORD_FILE")
	if err != nil {
		return err
	}
	if pw == "" {
		return errors.New("CORRESPONDENT_USER is set but CORRESPONDENT_PASSWORD_FILE is not")
	}
	id, err := st.CreateUser(ctx, username, pw)
	if err != nil {
		return fmt.Errorf("create %s: %w", username, err)
	}
	apiPW, err := secretFile("CORRESPONDENT_API_PASSWORD_FILE")
	if err != nil {
		return err
	}
	if apiPW != "" {
		if err := st.SetAPIPassword(ctx, id, apiPW); err != nil {
			return err
		}
	}
	log.Info("created account from secrets", "user", username, "api", apiPW != "")
	return nil
}

func secretFile(env string) (string, error) {
	path := os.Getenv(env)
	if path == "" {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", env, err)
	}
	pw := strings.TrimRight(string(b), "\r\n")
	if len(pw) < 10 {
		return "", fmt.Errorf("%s: use a password of at least 10 characters", env)
	}
	return pw, nil
}

// healthcheck asks the local server whether it is ready.
func healthcheck(ctx context.Context, addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/ready", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: HTTP %d", resp.StatusCode)
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

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

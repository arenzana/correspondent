package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/huh/v2"
	"golang.org/x/term"

	"github.com/arenzana/correspondent/internal/client"
	"github.com/arenzana/correspondent/internal/textify"
)

// --- auth ---------------------------------------------------------------------

func (a *app) auth(args []string) error {
	if len(args) == 0 {
		return usagef("auth needs login, logout or status")
	}
	switch args[0] {
	case "login":
		return a.login(args[1:])
	case "logout":
		if a.cfg.Token != "" {
			if err := a.api.Logout(a.ctx); err != nil && !errors.Is(err, client.ErrAuth) {
				return err
			}
		}
		if err := removeConfig(); err != nil {
			return err
		}
		return emit(a.asJSON, map[string]bool{"signedOut": true}, func(w io.Writer) { line(w, "Signed out; the token was revoked.") })
	case "status":
		me, err := a.api.Me(a.ctx)
		if err != nil {
			return err
		}
		return emit(a.asJSON, map[string]any{"server": a.cfg.Server, "username": me.Username, "role": me.Role, "serverVersion": me.Version},
			func(w io.Writer) {
				line(w, fmt.Sprintf("Signed in to %s as %s (%s).", styles.accent.Render(a.cfg.Server), styles.title.Render(me.Username), me.Role))
			})
	}
	return usagef("unknown auth command %q", args[0])
}

func (a *app) login(args []string) error {
	var username, name string
	var passwordStdin, readOnly bool
	server := a.cfg.Server
	_, _, err := flags("auth login", args, func(fs *flag.FlagSet) {
		fs.StringVar(&username, "username", "", "")
		fs.StringVar(&name, "name", "", "")
		fs.BoolVar(&passwordStdin, "password-stdin", false, "")
		fs.BoolVar(&readOnly, "read-only", false, "")
	})
	if err != nil {
		return err
	}
	var password string
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && !passwordStdin
	if passwordStdin {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
		if err != nil {
			return err
		}
		password = strings.TrimRight(string(b), "\r\n")
	}
	if interactive {
		fields := []huh.Field{}
		if server == "" {
			fields = append(fields, huh.NewInput().Title("Server").Placeholder("https://rss.example.org").Value(&server).
				Validate(func(s string) error {
					if !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "http://") {
						return errors.New("a full address, starting with https://")
					}
					return nil
				}))
		}
		if username == "" {
			fields = append(fields, huh.NewInput().Title("Username").Value(&username))
		}
		fields = append(fields, huh.NewInput().Title("Password").EchoMode(huh.EchoModePassword).Value(&password))
		if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
			return usagef("login cancelled")
		}
	}
	if server == "" || username == "" || password == "" {
		return usagef("auth login needs --server, --username and --password-stdin when not on a terminal")
	}
	if name == "" {
		host, _ := os.Hostname()
		name = "corr on " + strings.Split(host, ".")[0]
	}
	scope := "write"
	if readOnly {
		scope = "read"
	}
	a.api.Server = strings.TrimRight(server, "/")
	token, user, err := a.api.Login(a.ctx, username, password, name, scope)
	if err != nil {
		return err
	}
	if err := saveConfig(config{Server: a.api.Server, Token: token, Username: user}); err != nil {
		return err
	}
	p, _ := configPath()
	return emit(a.asJSON, map[string]any{"server": a.api.Server, "username": user, "scope": scope, "config": p}, func(w io.Writer) {
		line(w, fmt.Sprintf("Signed in to %s as %s. The %s token is saved in %s.", styles.accent.Render(a.api.Server), styles.title.Render(user), scope, p))
	})
}

// --- reading -------------------------------------------------------------------

func (a *app) list(args []string) error {
	var q client.Query
	fs, pos, err := flags("list", args, func(fs *flag.FlagSet) {
		fs.StringVar(&q.View, "view", "unread", "")
		fs.Int64Var(&q.FeedID, "feed", 0, "")
		fs.Int64Var(&q.FolderID, "folder", 0, "")
		fs.StringVar(&q.Search, "search", "", "")
		fs.IntVar(&q.Limit, "limit", 30, "")
		fs.StringVar(&q.Cursor, "cursor", "", "")
		fs.BoolVar(&q.Oldest, "oldest", false, "")
	})
	if err != nil {
		return err
	}
	_ = fs
	if len(pos) > 0 {
		return usagef("list takes no arguments (use --search)")
	}
	switch q.View {
	case "unread", "starred", "today", "all":
	default:
		return usagef("--view is unread, starred, today or all")
	}
	page, err := a.api.Entries(a.ctx, q)
	if err != nil {
		return err
	}
	tree, err := a.api.Tree(a.ctx)
	if err != nil {
		return err
	}
	titles := feedTitles(tree)
	type out struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Feed        string `json:"feed"`
		FeedID      int64  `json:"feedId"`
		URL         string `json:"url"`
		Excerpt     string `json:"excerpt"`
		PublishedAt string `json:"publishedAt"`
		Read        bool   `json:"read"`
		Starred     bool   `json:"starred"`
	}
	items := make([]out, 0, len(page.Entries))
	for _, e := range page.Entries {
		items = append(items, out{e.ID, e.Title, titles[e.FeedID], e.FeedID, e.URL, e.Excerpt,
			time.Unix(e.PublishedAt, 0).UTC().Format(time.RFC3339), e.Read, e.Starred})
	}
	return emit(a.asJSON, map[string]any{"entries": items, "cursor": page.Cursor}, func(w io.Writer) {
		if len(items) == 0 {
			line(w, styles.dim.Render("Nothing here."))
			return
		}
		width := termWidth()
		for _, e := range page.Entries {
			dot := styles.accent.Render("●")
			if e.Read {
				dot = styles.dim.Render("○")
			}
			star := " "
			if e.Starred {
				star = styles.star.Render("★")
			}
			when := time.Unix(e.PublishedAt, 0).Format("Jan 02 15:04")
			title := truncate(e.Title, max(20, width-48))
			line(w, fmt.Sprintf("%s %s %s  %s  %s", dot, star, styles.dim.Render(when), title, styles.dim.Render(truncate(titles[e.FeedID], 22))))
			line(w, styles.dim.Render(fmt.Sprintf("        %s", e.ID)))
		}
		if page.Cursor != "" {
			line(w, styles.dim.Render("More: corr list --cursor "+page.Cursor))
		}
	})
}

func feedTitles(t client.Tree) map[int64]string {
	m := make(map[int64]string, len(t.Feeds))
	for _, f := range t.Feeds {
		m[f.ID] = f.Title
	}
	return m
}

func (a *app) read(args []string) error {
	var full, asHTML, keep bool
	_, pos, err := flags("read", args, func(fs *flag.FlagSet) {
		fs.BoolVar(&full, "full", false, "")
		fs.BoolVar(&asHTML, "html", false, "")
		fs.BoolVar(&keep, "keep-unread", false, "")
	})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("read needs one article ID")
	}
	var e client.Entry
	if full {
		e, err = a.api.FullArticle(a.ctx, pos[0])
	} else {
		e, err = a.api.Entry(a.ctx, pos[0])
	}
	if err != nil {
		return err
	}
	if !keep && !e.Read {
		if _, err := a.api.SetState(a.ctx, "read", []string{e.ID}); err == nil {
			e.Read = true
		}
	}
	md := textify.Markdown(e.Content, a.cfg.Server+"/")
	return emit(a.asJSON, map[string]any{
		"id": e.ID, "title": e.Title, "author": e.Author, "url": e.URL, "feedId": e.FeedID,
		"publishedAt": time.Unix(e.PublishedAt, 0).UTC().Format(time.RFC3339),
		"read":        e.Read, "starred": e.Starred, "fullText": e.FullText, "markdown": md,
	}, func(w io.Writer) {
		if asHTML {
			fmt.Fprintln(w, e.Content)
			return
		}
		doc := "# " + e.Title + "\n\n"
		if e.Author != "" {
			doc += "_" + e.Author + "_  \n"
		}
		doc += time.Unix(e.PublishedAt, 0).Format("Monday, January 2, 2006") + " · <" + e.URL + ">\n\n---\n\n" + md
		fmt.Fprint(w, renderMarkdown(doc, termWidth()))
	})
}

func (a *app) mark(args []string) error {
	if len(args) < 2 {
		return usagef("mark needs read, unread, star or unstar, and article IDs")
	}
	action := args[0]
	switch action {
	case "read", "unread", "star", "unstar":
	default:
		return usagef("mark needs read, unread, star or unstar")
	}
	ids := args[1:]
	if len(ids) == 1 && ids[0] == "-" {
		ids = readIDs(os.Stdin)
	}
	n, err := a.api.SetState(a.ctx, action, ids)
	if err != nil {
		return err
	}
	return emit(a.asJSON, map[string]any{"action": action, "changed": n}, func(w io.Writer) {
		line(w, fmt.Sprintf("Marked %d %s %s.", n, plural(n, "article", "articles"), action))
	})
}

// readIDs reads article IDs, one per line (corr list --json | jq … | corr mark read -).
func readIDs(r io.Reader) []string {
	var ids []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if s := strings.Trim(strings.TrimSpace(sc.Text()), `"`); s != "" {
			ids = append(ids, s)
		}
	}
	return ids
}

func (a *app) markAll(args []string) error {
	var feed, folder int64
	var view string
	_, pos, err := flags("mark-all-read", args, func(fs *flag.FlagSet) {
		fs.Int64Var(&feed, "feed", 0, "")
		fs.Int64Var(&folder, "folder", 0, "")
		fs.StringVar(&view, "view", "", "")
	})
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("mark-all-read takes flags only")
	}
	n, err := a.api.MarkAllRead(a.ctx, feed, folder, view)
	if err != nil {
		return err
	}
	return emit(a.asJSON, map[string]int{"changed": n}, func(w io.Writer) {
		line(w, fmt.Sprintf("Marked %d %s read.", n, plural(n, "article", "articles")))
	})
}

// --- subscriptions -------------------------------------------------------------

func (a *app) feeds(args []string) error {
	if len(args) > 0 {
		return usagef("feeds takes no arguments")
	}
	t, err := a.api.Tree(a.ctx)
	if err != nil {
		return err
	}
	folders := map[int64]string{}
	for _, f := range t.Folders {
		folders[f.ID] = f.Name
	}
	type out struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		Folder   string `json:"folder"`
		Unread   int    `json:"unread"`
		Total    int    `json:"total"`
		Paused   bool   `json:"paused"`
		FullText bool   `json:"fullText"`
		Page     bool   `json:"page"`
		Notify   bool   `json:"notify"`
		Error    string `json:"error"`
		LatestAt string `json:"latestAt,omitempty"`
	}
	items := make([]out, 0, len(t.Feeds))
	for _, f := range t.Feeds {
		o := out{ID: f.ID, Title: f.Title, URL: f.URL, Unread: f.Unread, Total: f.Total, Paused: f.Paused,
			FullText: f.FullText, Page: f.Page, Notify: f.Notify}
		if f.FolderID != nil {
			o.Folder = folders[*f.FolderID]
		}
		if f.ErrorCount > 0 {
			o.Error = f.LastError
		}
		if f.LatestAt > 0 {
			o.LatestAt = time.Unix(f.LatestAt, 0).UTC().Format(time.RFC3339)
		}
		items = append(items, o)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Folder != items[j].Folder {
			return items[i].Folder < items[j].Folder
		}
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
	return emit(a.asJSON, map[string]any{"feeds": items}, func(w io.Writer) {
		folder := "\x00"
		for _, f := range items {
			if f.Folder != folder {
				folder = f.Folder
				name := folder
				if name == "" {
					name = "No folder"
				}
				line(w, "\n"+styles.title.Render(name))
			}
			var tags []string
			if f.Paused {
				tags = append(tags, "paused")
			}
			if f.FullText {
				tags = append(tags, "full articles")
			}
			if f.Page {
				tags = append(tags, "page")
			}
			if f.Error != "" {
				tags = append(tags, styles.err.Render("failing"))
			}
			unread := styles.dim.Render(fmt.Sprintf("%4d", f.Unread))
			if f.Unread > 0 {
				unread = styles.accent.Render(fmt.Sprintf("%4d", f.Unread))
			}
			line(w, fmt.Sprintf("%s  %s %s  %s", unread, styles.dim.Render(fmt.Sprintf("%5d", f.ID)), f.Title, styles.tag.Render(strings.Join(tags, ", "))))
		}
	})
}

func (a *app) folders(args []string) error {
	if len(args) > 0 {
		return usagef("folders takes no arguments")
	}
	t, err := a.api.Tree(a.ctx)
	if err != nil {
		return err
	}
	if t.Folders == nil {
		t.Folders = []client.Folder{}
	}
	return emit(a.asJSON, map[string]any{"folders": t.Folders}, func(w io.Writer) {
		for _, f := range t.Folders {
			line(w, fmt.Sprintf("%s  %s %s", styles.accent.Render(fmt.Sprintf("%4d", f.Unread)), styles.dim.Render(fmt.Sprintf("%5d", f.ID)), f.Name))
		}
	})
}

func (a *app) discover(args []string) error {
	var ai bool
	_, pos, err := flags("discover", args, func(fs *flag.FlagSet) { fs.BoolVar(&ai, "ai", false, "") })
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("discover needs one address")
	}
	cands, err := a.candidates(pos[0], ai)
	if err != nil {
		return err
	}
	return emit(a.asJSON, map[string]any{"candidates": cands}, func(w io.Writer) { printCandidates(w, cands) })
}

func (a *app) candidates(address string, ai bool) ([]client.Candidate, error) {
	var cands []client.Candidate
	var err error
	if ai {
		cands, err = a.api.DiscoverAI(a.ctx, address)
	} else {
		cands, err = a.api.Discover(a.ctx, address)
	}
	if err == nil && len(cands) == 0 {
		err = &client.Error{Status: 404, Message: "no feed found there; try --ai to have your AI read the page"}
	}
	if cands == nil {
		cands = []client.Candidate{}
	}
	return cands, err
}

func printCandidates(w io.Writer, cands []client.Candidate) {
	for _, c := range cands {
		line(w, styles.title.Render(c.Title)+"  "+styles.dim.Render(c.URL))
		info := fmt.Sprintf("%d recent %s", c.ItemCount, plural(c.ItemCount, "article", "articles"))
		if c.Page {
			info += "; no feed, read from the page (" + c.Method + ")"
			if c.Shared {
				info += ", already set up on this server"
			}
		}
		line(w, "  "+styles.dim.Render(info))
		for _, s := range c.Sample {
			line(w, "  · "+s)
		}
	}
}

func (a *app) subscribe(args []string) error {
	var title, folder string
	var ai bool
	_, pos, err := flags("subscribe", args, func(fs *flag.FlagSet) {
		fs.StringVar(&title, "title", "", "")
		fs.StringVar(&folder, "folder", "", "")
		fs.BoolVar(&ai, "ai", false, "")
	})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("subscribe needs one address")
	}
	cands, err := a.candidates(pos[0], ai)
	if err != nil {
		return err
	}
	c := cands[0]
	if title == "" {
		title = c.Title
	}
	id, err := a.api.Subscribe(a.ctx, c.URL, title, folder, c.Page)
	if err != nil {
		return err
	}
	return emit(a.asJSON, map[string]any{"id": id, "url": c.URL, "title": title, "page": c.Page}, func(w io.Writer) {
		line(w, fmt.Sprintf("Subscribed to %s (feed %d).", styles.title.Render(title), id))
	})
}

func (a *app) unsubscribe(args []string) error {
	if len(args) != 1 {
		return usagef("unsubscribe needs one feed ID (see corr feeds)")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return usagef("feed IDs are numbers (see corr feeds)")
	}
	if err := a.api.DeleteFeed(a.ctx, id); err != nil {
		return err
	}
	return emit(a.asJSON, map[string]any{"id": id, "deleted": true}, func(w io.Writer) { line(w, fmt.Sprintf("Unsubscribed from feed %d.", id)) })
}

func (a *app) feed(args []string) error {
	if len(args) < 2 {
		return usagef("feed needs a feed ID and an action")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return usagef("feed IDs are numbers (see corr feeds)")
	}
	action, rest := args[1], args[2:]
	onOff := func() (*bool, error) {
		if len(rest) != 1 || (rest[0] != "on" && rest[0] != "off") {
			return nil, usagef("%s needs on or off", action)
		}
		v := rest[0] == "on"
		return &v, nil
	}
	var p client.FeedPatch
	yes, no := true, false
	switch action {
	case "rename":
		if len(rest) == 0 {
			return usagef("rename needs a title")
		}
		t := strings.Join(rest, " ")
		p.Title = &t
	case "move":
		if len(rest) == 0 {
			return usagef("move needs a folder name")
		}
		p.FolderName = strings.Join(rest, " ")
	case "unfile":
		p.NoFolder = true
	case "pause":
		p.Paused = &yes
	case "resume":
		p.Paused = &no
	case "full-text":
		if p.FullText, err = onOff(); err != nil {
			return err
		}
	case "notify":
		if p.Notify, err = onOff(); err != nil {
			return err
		}
	case "url":
		if len(rest) != 1 {
			return usagef("url needs the new address")
		}
		p.URL = &rest[0]
	case "refresh":
		if err := a.api.RefreshFeed(a.ctx, id); err != nil {
			return err
		}
		return emit(a.asJSON, map[string]any{"id": id, "refreshed": true}, func(w io.Writer) { line(w, "Checked for new articles.") })
	default:
		return usagef("unknown feed action %q", action)
	}
	if err := a.api.UpdateFeed(a.ctx, id, p); err != nil {
		return err
	}
	return emit(a.asJSON, map[string]any{"id": id, "updated": action}, func(w io.Writer) { line(w, "Done.") })
}

func (a *app) opml(args []string) error {
	if len(args) == 0 {
		return usagef("opml needs export or import")
	}
	switch args[0] {
	case "export":
		b, err := a.api.ExportOPML(a.ctx)
		if err != nil {
			return err
		}
		if len(args) > 1 {
			return os.WriteFile(args[1], b, 0o644)
		}
		_, err = os.Stdout.Write(b)
		return err
	case "import":
		if len(args) != 2 {
			return usagef("opml import needs a file (or - for stdin)")
		}
		var b []byte
		var err error
		if args[1] == "-" {
			b, err = io.ReadAll(io.LimitReader(os.Stdin, 8<<20))
		} else {
			b, err = os.ReadFile(args[1])
		}
		if err != nil {
			return err
		}
		n, err := a.api.ImportOPML(a.ctx, b)
		if err != nil {
			return err
		}
		return emit(a.asJSON, map[string]int{"added": n}, func(w io.Writer) {
			line(w, fmt.Sprintf("Added %d %s.", n, plural(n, "feed", "feeds")))
		})
	}
	return usagef("opml needs export or import")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

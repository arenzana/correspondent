package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/arenzana/correspondent/internal/client"
)

// watch prints articles as they arrive, until interrupted: a line each,
// or with --json one JSON object per line (NDJSON) for scripts and agents.
// --notify also shows a desktop notification.
func (a *app) watch(args []string) error {
	var feed, folder int64
	var notify bool
	_, pos, err := flags("watch", args, func(fs *flag.FlagSet) {
		fs.Int64Var(&feed, "feed", 0, "")
		fs.Int64Var(&folder, "folder", 0, "")
		fs.BoolVar(&notify, "notify", false, "")
	})
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usagef("watch takes flags only")
	}
	tree, err := a.api.Tree(a.ctx)
	if err != nil {
		return err
	}
	// Only articles newer than now: nothing from before watch started.
	newest, _ := strconv.ParseInt(tree.Counts.Newest, 10, 64)
	titles := feedTitles(tree)
	if !a.asJSON {
		line(os.Stderr, styles.dim.Render("Watching for new articles. Ctrl-C to stop."))
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	check := func() {
		page, err := a.api.Entries(a.ctx, client.Query{View: "unread", FeedID: feed, FolderID: folder, Limit: 100})
		if err != nil {
			return
		}
		var fresh []client.Entry
		for _, e := range page.Entries {
			if id, _ := strconv.ParseInt(e.ID, 10, 64); id > newest {
				fresh = append(fresh, e)
			}
		}
		// Oldest first, as they happened.
		for i := len(fresh) - 1; i >= 0; i-- {
			e := fresh[i]
			if id, _ := strconv.ParseInt(e.ID, 10, 64); id > newest {
				newest = id
			}
			if _, ok := titles[e.FeedID]; !ok {
				if t, err := a.api.Tree(a.ctx); err == nil {
					titles = feedTitles(t)
				}
			}
			if a.asJSON {
				enc.Encode(map[string]any{"id": e.ID, "title": e.Title, "feed": titles[e.FeedID], "feedId": e.FeedID,
					"url": e.URL, "excerpt": e.Excerpt, "publishedAt": time.Unix(e.PublishedAt, 0).UTC().Format(time.RFC3339)})
			} else {
				line(os.Stdout, fmt.Sprintf("%s  %s  %s  %s", styles.dim.Render(time.Now().Format("15:04")),
					styles.accent.Render(truncate(titles[e.FeedID], 24)), e.Title, styles.dim.Render(e.ID)))
			}
			if notify {
				desktopNotify(titles[e.FeedID], e.Title)
			}
		}
	}
	err = a.api.Watch(a.ctx, func(ev client.Event) {
		if ev.Name == "refresh" || ev.Name == "resync" {
			check()
		}
	})
	if errors.Is(err, context.Canceled) {
		return nil // Ctrl-C
	}
	return err
}

// desktopNotify shows a notification with the system's own tool.
func desktopNotify(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %s with title %s", strconv.Quote(body), strconv.Quote(title))
		exec.Command("osascript", "-e", script).Run()
	case "linux":
		exec.Command("notify-send", "--app-name=Correspondent", title, body).Run()
	}
}

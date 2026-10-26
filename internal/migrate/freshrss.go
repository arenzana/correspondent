// Package migrate imports a FreshRSS account. It preserves the identifiers
// Google Reader clients have cached (feed and item IDs), because Reeder
// reconciles by set difference and remapped IDs cause duplicates or lost
// state.
package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/greader"
	"github.com/arenzana/correspondent/internal/opml"
	"github.com/arenzana/correspondent/internal/store"
)

// Sources are the files a FreshRSS export provides. Subscriptions is the
// optional output of /reader/api/0/subscription/list, the only export that
// carries FreshRSS's numeric feed IDs.
type Sources struct {
	OPML          io.Reader
	Starred       io.Reader // starred_<date>.json from the FreshRSS export, optional
	Subscriptions io.Reader // optional
}

type Report struct {
	Folders, Feeds, FeedIDsKept, Starred int
	Warnings                             []string
}

func FreshRSS(ctx context.Context, st *store.Store, userID int64, src Sources) (*Report, error) {
	rep := &Report{}
	subs, err := opml.Parse(src.OPML)
	if err != nil {
		return nil, fmt.Errorf("OPML: %w", err)
	}

	legacy := map[string]int64{} // feed URL → FreshRSS feed ID
	if src.Subscriptions != nil {
		var list struct {
			Subscriptions []struct{ ID, URL string }
		}
		if err := json.NewDecoder(src.Subscriptions).Decode(&list); err != nil {
			return nil, fmt.Errorf("subscription list: %w", err)
		}
		for _, s := range list.Subscriptions {
			if id, err := strconv.ParseInt(strings.TrimPrefix(s.ID, "feed/"), 10, 64); err == nil {
				legacy[s.URL] = id
			}
		}
	}

	folders := map[string]int64{}
	byURL := map[string]int64{}
	for _, s := range subs {
		if !isHTTP(s.URL) {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%q: not an http(s) feed address; skipped", s.Title))
			continue
		}
		var folderID *int64
		if s.Folder != "" {
			id, ok := folders[s.Folder]
			if !ok {
				if id, err = st.EnsureFolder(ctx, userID, s.Folder); err != nil {
					return rep, err
				}
				folders[s.Folder] = id
				rep.Folders++
			}
			folderID = &id
		}
		siteURL := s.SiteURL
		if !isHTTP(siteURL) {
			siteURL = ""
		}
		want := legacy[s.URL]
		id, err := st.ImportFeed(ctx, userID, want, s.URL, s.Title, siteURL, folderID)
		if err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: %v", s.URL, err))
			continue
		}
		if want != 0 && id == want {
			rep.FeedIDsKept++
		}
		byURL[s.URL] = id
		rep.Feeds++
	}
	if src.Subscriptions != nil && rep.FeedIDsKept != len(legacy) {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("kept %d of %d FreshRSS feed IDs", rep.FeedIDsKept, len(legacy)))
	}

	if src.Starred != nil {
		n, warns, err := importStarred(ctx, st, src.Starred, byURL, legacy)
		rep.Starred = n
		rep.Warnings = append(rep.Warnings, warns...)
		if err != nil {
			return rep, err
		}
	}
	return rep, nil
}

type exportItem struct {
	FrssID    string `json:"frss:id"`
	ID        string `json:"id"`
	GUID      string `json:"guid"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Published int64  `json:"published"`
	Canonical []struct {
		Href string `json:"href"`
	} `json:"canonical"`
	Categories []string `json:"categories"`
	Origin     struct {
		StreamID string `json:"streamId"`
		FeedURL  string `json:"feedUrl"`
		HTMLURL  string `json:"htmlUrl"`
	} `json:"origin"`
	Content struct {
		Content string `json:"content"`
	} `json:"content"`
	Summary struct {
		Content string `json:"content"`
	} `json:"summary"`
}

func importStarred(ctx context.Context, st *store.Store, r io.Reader, byURL, legacy map[string]int64) (int, []string, error) {
	var doc struct{ Items []exportItem }
	if err := json.NewDecoder(io.LimitReader(r, 256<<20)).Decode(&doc); err != nil {
		return 0, nil, fmt.Errorf("starred export: %w", err)
	}
	// Map FreshRSS feed IDs to ours (identical when IDs were kept).
	byLegacy := map[int64]int64{}
	for url, old := range legacy {
		if id, ok := byURL[url]; ok {
			byLegacy[old] = id
		}
	}
	perFeed := map[int64][]store.ImportedEntry{}
	var warns []string
	for _, it := range doc.Items {
		id, err := itemID(it)
		if err != nil || id <= 0 {
			warns = append(warns, fmt.Sprintf("%q: invalid item ID", it.Title))
			continue
		}
		feedID := byURL[it.Origin.FeedURL]
		if feedID == 0 {
			if old, err := strconv.ParseInt(strings.TrimPrefix(it.Origin.StreamID, "feed/"), 10, 64); err == nil {
				feedID = byLegacy[old]
			}
		}
		if feedID == 0 {
			warns = append(warns, fmt.Sprintf("%q: its feed (%s) is not in the OPML; skipped", it.Title, it.Origin.StreamID))
			continue
		}
		raw := it.Content.Content
		if raw == "" {
			raw = it.Summary.Content
		}
		var link string
		if len(it.Canonical) > 0 && isHTTP(it.Canonical[0].Href) {
			link = it.Canonical[0].Href // never carry javascript: or data: links into hrefs
		}
		// The export is untrusted input: content goes through the same
		// sanitizer as freshly fetched feeds.
		body := content.Process(raw, link)
		guid := it.GUID
		if guid == "" {
			guid = link
		}
		if guid == "" {
			guid = "frss:" + strconv.FormatInt(id, 10)
		}
		e := store.ImportedEntry{ID: id, Starred: true}
		for _, c := range it.Categories {
			if c == "user/-/state/com.google/read" {
				e.Read = true
			}
		}
		e.GUID, e.URL, e.Title, e.Author = guid, link, content.PlainText(it.Title), content.PlainText(it.Author)
		e.Content, e.Excerpt, e.ImageURL, e.Enclosures = body.HTML, content.Excerpt(body.Text, 320), body.ImageURL, "[]"
		if it.Published > 0 {
			e.PublishedAt = time.Unix(it.Published, 0)
		}
		perFeed[feedID] = append(perFeed[feedID], e)
	}
	total := 0
	for feedID, items := range perFeed {
		n, err := st.ImportEntries(ctx, feedID, items)
		total += n
		if err != nil {
			return total, warns, err
		}
	}
	return total, warns, nil
}

func itemID(it exportItem) (int64, error) {
	if it.FrssID != "" {
		return strconv.ParseInt(it.FrssID, 10, 64)
	}
	return greader.ParseItemID(it.ID)
}

func isHTTP(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

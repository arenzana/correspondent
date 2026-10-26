package greader

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/arenzana/correspondent/internal/store"
)

// Subscriber adds subscriptions for apps, choosing a feed, a page read
// exactly, or a page read by the reader's AI (ingest.SubscribeAuto).
type Subscriber interface {
	SubscribeAuto(ctx context.Context, userID int64, raw, title string, folderID *int64) (*store.Feed, error)
}

// Adding a site can mean fetching it, its article pages and asking an AI.
const subscribeTimeout = 4 * time.Minute

// quickadd subscribes to an address (Reeder's "Add Subscription").
func (s *Server) quickadd(ctx context.Context, w http.ResponseWriter, r *http.Request, sess session) {
	raw := strings.TrimPrefix(strings.TrimSpace(r.Form.Get("quickadd")), feedPrefix)
	if raw == "" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	f, err := s.subscribe(ctx, sess, raw, "", "")
	if err != nil {
		jsonOut(w, map[string]any{"numResults": 0, "query": raw, "error": err.Error()})
		return
	}
	jsonOut(w, map[string]any{"numResults": 1, "query": raw,
		"streamId": feedPrefix + strconv.FormatInt(f.ID, 10), "streamName": UnicodeAlternative(f.Title, true)})
}

// subscriptionEdit subscribes, unsubscribes, renames and moves feeds:
// ac=subscribe|unsubscribe|edit, s=feed/<id or address> (repeatable),
// t=title, a=label to move into, r=label to leave.
func (s *Server) subscriptionEdit(ctx context.Context, w http.ResponseWriter, r *http.Request, sess session) {
	streams := r.Form["s"]
	if len(streams) == 0 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	title := r.Form.Get("t")
	add := strings.TrimPrefix(r.Form.Get("a"), labelPrefix)
	remove := strings.TrimPrefix(r.Form.Get("r"), labelPrefix)
	for _, stream := range streams {
		ref := strings.TrimPrefix(stream, feedPrefix)
		switch r.Form.Get("ac") {
		case "subscribe":
			if _, err := s.subscribe(ctx, sess, ref, title, add); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			continue
		case "unsubscribe", "edit":
		default:
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		id, err := s.feedID(ctx, sess, ref)
		if err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if r.Form.Get("ac") == "unsubscribe" {
			err = s.Store.DeleteFeed(ctx, sess.userID, id)
		} else {
			err = s.edit(ctx, sess, id, title, add, remove)
		}
		if err != nil {
			s.fail(w, err)
			return
		}
	}
	plain(w, "OK")
}

func (s *Server) subscribe(ctx context.Context, sess session, raw, title, label string) (*store.Feed, error) {
	if s.Subscriber == nil {
		return nil, errors.New("subscribing from apps is not available")
	}
	// A bare "example.com" means https; any other scheme is refused.
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("not a web address")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	if u, err := url.Parse(raw); err != nil || u.Host == "" {
		return nil, errors.New("not a web address")
	}
	var folder *int64
	if label != "" {
		id, err := s.Store.EnsureFolder(ctx, sess.userID, label)
		if err != nil {
			return nil, err
		}
		folder = &id
	}
	ctx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	defer cancel()
	return s.Subscriber.SubscribeAuto(ctx, sess.userID, raw, strings.TrimSpace(title), folder)
}

// feedID resolves a stream reference: a numeric ID or a feed address.
func (s *Server) feedID(ctx context.Context, sess session, ref string) (int64, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		if _, err := s.Store.Feed(ctx, sess.userID, id); err != nil {
			return 0, err
		}
		return id, nil
	}
	feeds, err := s.Store.Feeds(ctx, sess.userID)
	if err != nil {
		return 0, err
	}
	for _, f := range feeds {
		if f.URL == ref {
			return f.ID, nil
		}
	}
	return 0, store.ErrNotFound
}

func (s *Server) edit(ctx context.Context, sess session, id int64, title, add, remove string) error {
	var t *string
	if title = strings.TrimSpace(title); title != "" {
		t = &title
	}
	var folder *int64
	if add != "" {
		fid, err := s.Store.EnsureFolder(ctx, sess.userID, add)
		if err != nil {
			return err
		}
		folder = &fid
	}
	return s.Store.UpdateFeed(ctx, sess.userID, id, t, folder, add == "" && remove != "")
}

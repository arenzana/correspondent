package greader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/arenzana/correspondent/internal/ratelimit"
	"github.com/arenzana/correspondent/internal/store"
)

// Prefix is the base path Reeder is configured with. It must never change.
const Prefix = "/api/greader.php"

const tokenTTL = 365 * 24 * time.Hour

type Server struct {
	Store    *store.Store
	Log      *slog.Logger
	Limiter  *ratelimit.Limiter
	ClientIP ratelimit.ClientIP
}

type session struct {
	userID   int64
	username string
	auth     string
	legacy   string // FreshRSS token, when authenticated by it
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.EscapedPath(), Prefix)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if path == "/accounts/ClientLogin" {
		s.clientLogin(w, r)
		return
	}
	sess, ok := s.authenticate(r)
	if !ok {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Unauthorized!")
		return
	}
	ctx := r.Context()
	switch {
	case path == "/reader/api/0/token":
		plain(w, editToken(sess)+"\n")
	case path == "/reader/api/0/user-info":
		jsonOut(w, map[string]string{
			"userId": strconv.FormatInt(sess.userID, 10), "userName": sess.username,
			"userProfileId": strconv.FormatInt(sess.userID, 10), "userEmail": "",
		})
	case path == "/reader/api/0/subscription/list":
		s.subscriptionList(ctx, w, sess)
	case path == "/reader/api/0/tag/list":
		s.tagList(ctx, w, sess)
	case path == "/reader/api/0/unread-count":
		s.unreadCount(ctx, w, sess)
	case path == "/reader/api/0/stream/items/ids":
		s.streamIDs(ctx, w, r, sess)
	case path == "/reader/api/0/stream/items/contents":
		if !s.checkT(w, r, sess, false) {
			return
		}
		s.itemsContents(ctx, w, r, sess)
	case strings.HasPrefix(path, "/reader/api/0/stream/contents"):
		stream := strings.TrimPrefix(path, "/reader/api/0/stream/contents")
		stream, _ = url.PathUnescape(strings.TrimPrefix(stream, "/"))
		if stream == "" {
			stream = r.Form.Get("s")
		}
		s.streamContents(ctx, w, r, sess, stream)
	case path == "/reader/api/0/edit-tag" && r.Method == http.MethodPost:
		if s.checkT(w, r, sess, true) {
			s.editTag(ctx, w, r, sess)
		}
	case path == "/reader/api/0/mark-all-as-read" && r.Method == http.MethodPost:
		if s.checkT(w, r, sess, true) {
			s.markAllRead(ctx, w, r, sess)
		}
	default:
		http.Error(w, "Not Implemented", http.StatusNotImplemented)
	}
}

func (s *Server) clientLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.ClientIP.Of(r)
	if s.Limiter != nil && !s.Limiter.Allow(ip) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "Error=TooManyAttempts\n")
		return
	}
	user, pass := r.Form.Get("Email"), r.Form.Get("Passwd")
	u, err := s.Store.CheckPassword(r.Context(), user, pass, true)
	if err != nil {
		if s.Limiter != nil {
			s.Limiter.Fail(ip)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Error=BadAuthentication\n")
		return
	}
	// While legacy auth is on, answer exactly as FreshRSS did, so a client
	// that signs in again gets the token it already has.
	_, token, err := s.Store.LegacyToken(r.Context(), u.Username)
	if err == nil && token == "" {
		token, err = s.Store.CreateSession(r.Context(), u.ID, "greader", tokenTTL)
	}
	if err != nil {
		s.Log.Error("greader login", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	auth := u.Username + "/" + token
	if r.Form.Get("output") == "json" {
		jsonOut(w, map[string]string{"SID": auth, "LSID": "null", "Auth": auth})
		return
	}
	plain(w, "SID="+auth+"\nLSID=null\nAuth="+auth+"\n")
}

func (s *Server) authenticate(r *http.Request) (session, bool) {
	h := r.Header.Get("Authorization")
	auth, ok := strings.CutPrefix(h, "GoogleLogin auth=")
	if !ok {
		return session{}, false
	}
	username, token, ok := strings.Cut(auth, "/")
	if !ok {
		return session{}, false
	}
	if uid, err := s.Store.SessionUser(r.Context(), token, "greader"); err == nil {
		if id, err := s.Store.UserIDByName(r.Context(), username); err == nil && id == uid {
			return session{userID: uid, username: username, auth: auth}, true
		}
		return session{}, false
	}
	// The static token FreshRSS issued, accepted after migration so Reeder
	// keeps working without being reconfigured.
	if uid, legacy, err := s.Store.LegacyToken(r.Context(), username); err == nil && store.EqualTokens(token, legacy) {
		return session{userID: uid, username: username, auth: auth, legacy: legacy}, true
	}
	return session{}, false
}

// editToken is the write token (T) for a session, 57 characters like
// FreshRSS's. For a legacy session it is exactly FreshRSS's token.
func editToken(sess session) string {
	if sess.legacy != "" {
		return padToken(sess.legacy)
	}
	sum := sha256.Sum256([]byte("edit:" + sess.auth))
	return padToken(hex.EncodeToString(sum[:]))
}

func padToken(t string) string {
	return (t + strings.Repeat("Z", 57))[:57]
}

// checkT validates the T write token. Reeder sends T=x on requests that are
// already authenticated by header, which FreshRSS accepts; so do we. Writes
// require T; item contents only checks it when present.
func (s *Server) checkT(w http.ResponseWriter, r *http.Request, sess session, required bool) bool {
	t := r.Form.Get("T")
	if (t == "" && !required) || t == "x" || store.EqualTokens(t, editToken(sess)) {
		return true
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprint(w, "Unauthorized!")
	return false
}

func (s *Server) folderNames(ctx context.Context, uid int64) (map[int64]string, []store.Folder, error) {
	folders, err := s.Store.Folders(ctx, uid)
	if err != nil {
		return nil, nil, err
	}
	names := make(map[int64]string, len(folders))
	for _, f := range folders {
		names[f.ID] = f.Name
	}
	return names, folders, nil
}

func (s *Server) subscriptionList(ctx context.Context, w http.ResponseWriter, sess session) {
	names, _, err := s.folderNames(ctx, sess.userID)
	if err != nil {
		s.fail(w, err)
		return
	}
	feeds, err := s.Store.Feeds(ctx, sess.userID)
	if err != nil {
		s.fail(w, err)
		return
	}
	type category struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	type sub struct {
		ID         string     `json:"id"`
		Title      string     `json:"title"`
		Categories []category `json:"categories"`
		URL        string     `json:"url"`
		HTMLURL    string     `json:"htmlUrl"`
		IconURL    string     `json:"iconUrl"`
	}
	out := make([]sub, 0, len(feeds))
	for _, f := range feeds {
		cats := []category{}
		if f.FolderID != nil {
			n := names[*f.FolderID]
			cats = append(cats, category{ID: labelPrefix + n, Label: n})
		}
		out = append(out, sub{ID: feedPrefix + strconv.FormatInt(f.ID, 10), Title: f.Title,
			Categories: cats, URL: f.URL, HTMLURL: f.SiteURL, IconURL: f.IconURL})
	}
	jsonOut(w, map[string]any{"subscriptions": out})
}

func (s *Server) tagList(ctx context.Context, w http.ResponseWriter, sess session) {
	_, folders, err := s.folderNames(ctx, sess.userID)
	if err != nil {
		s.fail(w, err)
		return
	}
	type tag struct {
		ID   string `json:"id"`
		Type string `json:"type,omitempty"`
	}
	tags := []tag{{ID: stateStar}, {ID: stateList}}
	for _, f := range folders {
		tags = append(tags, tag{ID: labelPrefix + f.Name, Type: "folder"})
	}
	jsonOut(w, map[string]any{"tags": tags})
}

func (s *Server) unreadCount(ctx context.Context, w http.ResponseWriter, sess session) {
	names, _, err := s.folderNames(ctx, sess.userID)
	if err != nil {
		s.fail(w, err)
		return
	}
	feeds, err := s.Store.Feeds(ctx, sess.userID)
	if err != nil {
		s.fail(w, err)
		return
	}
	folderOf := map[int64]int64{}
	for _, f := range feeds {
		if f.FolderID != nil {
			folderOf[f.ID] = *f.FolderID
		}
	}
	unread, err := s.Store.UnreadByFeed(ctx, sess.userID)
	if err != nil {
		s.fail(w, err)
		return
	}
	type count struct {
		ID     string `json:"id"`
		Count  int    `json:"count"`
		Newest string `json:"newestItemTimestampUsec"`
	}
	// FreshRSS lists every feed and folder, including those with nothing
	// unread; clients may rely on the presence of each stream.
	byFeed := make(map[int64]store.FeedUnread, len(unread))
	for _, u := range unread {
		byFeed[u.FeedID] = u
	}
	var (
		out          []count
		total        int
		newest       int64
		folderCount  = map[int64]int{}
		folderNewest = map[int64]int64{}
	)
	for _, f := range feeds {
		u := byFeed[f.ID]
		out = append(out, count{ID: feedPrefix + strconv.FormatInt(f.ID, 10), Count: u.Count, Newest: strconv.FormatInt(u.Newest, 10)})
		total += u.Count
		newest = max(newest, u.Newest)
		if fid, ok := folderOf[f.ID]; ok {
			folderCount[fid] += u.Count
			folderNewest[fid] = max(folderNewest[fid], u.Newest)
		}
	}
	for fid, name := range names {
		out = append(out, count{ID: labelPrefix + name, Count: folderCount[fid], Newest: strconv.FormatInt(folderNewest[fid], 10)})
	}
	out = append(out, count{ID: stateList, Count: total, Newest: strconv.FormatInt(newest, 10)})
	jsonOut(w, map[string]any{"max": total, "unreadcounts": out})
}

// filter maps a stream and the standard query parameters to a store filter.
func (s *Server) filter(ctx context.Context, r *http.Request, sess session, stream string) (store.Filter, error) {
	f := store.Filter{UserID: sess.userID}
	stream = normalizeStream(stream)
	switch {
	case stream == "" || stream == stateList:
	case stream == stateStar:
		f.State = store.StateStarred
	case stream == stateRead:
		f.State = store.StateRead
	case stream == "user/-/state/com.google/unread":
		f.State = store.StateUnread
	case strings.HasPrefix(stream, feedPrefix):
		id, err := strconv.ParseInt(strings.TrimPrefix(stream, feedPrefix), 10, 64)
		if err != nil {
			return f, fmt.Errorf("unknown stream %q", stream)
		}
		f.FeedID = id
	case strings.HasPrefix(stream, labelPrefix):
		name := strings.TrimPrefix(stream, labelPrefix)
		_, folders, err := s.folderNames(ctx, sess.userID)
		if err != nil {
			return f, err
		}
		f.FolderID = -1 // matches nothing unless the folder exists
		for _, fo := range folders {
			if fo.Name == name {
				f.FolderID = fo.ID
			}
		}
	default:
		return f, fmt.Errorf("unknown stream %q", stream)
	}
	q := r.Form
	switch normalizeStream(q.Get("xt")) {
	case stateRead:
		f.Exclude = store.StateRead
	case stateStar:
		f.Exclude = store.StateStarred
	}
	switch normalizeStream(q.Get("it")) {
	case stateRead:
		f.State = store.StateRead
	case stateStar:
		f.State = store.StateStarred
	}
	if v, err := strconv.ParseInt(q.Get("ot"), 10, 64); err == nil && v > 0 {
		f.MinID = microseconds(v)
	}
	if v, err := strconv.ParseInt(q.Get("nt"), 10, 64); err == nil && v > 0 {
		f.MaxID = microseconds(v) - 1
	}
	f.Oldest = q.Get("r") == "o"
	if c := q.Get("c"); c != "" {
		v, err := strconv.ParseInt(c, 10, 64)
		if err != nil {
			return f, errors.New("invalid continuation")
		}
		f.After = v
	}
	f.Limit, _ = strconv.Atoi(q.Get("n"))
	if f.Limit <= 0 {
		f.Limit = 20
	}
	return f, nil
}

func (s *Server) streamIDs(ctx context.Context, w http.ResponseWriter, r *http.Request, sess session) {
	f, err := s.filter(ctx, r, sess, r.Form.Get("s"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Reeder asks for n=10000; serve at most 10000 with continuation.
	f.Limit = min(f.Limit, 10000)
	ids, next, err := s.Store.EntryIDs(ctx, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	type ref struct {
		ID string `json:"id"`
	}
	refs := make([]ref, len(ids))
	for i, id := range ids {
		refs[i] = ref{ID: strconv.FormatInt(id, 10)}
	}
	out := map[string]any{"itemRefs": refs}
	if next != 0 {
		out["continuation"] = strconv.FormatInt(next, 10)
	}
	jsonOut(w, out)
}

func (s *Server) itemsContents(ctx context.Context, w http.ResponseWriter, r *http.Request, sess session) {
	raw := r.Form["i"]
	if len(raw) > 1000 {
		http.Error(w, "too many items", http.StatusBadRequest)
		return
	}
	ids := make([]int64, 0, len(raw))
	for _, v := range raw {
		id, err := ParseItemID(v)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	entries, err := s.Store.EntriesByID(ctx, sess.userID, ids)
	if err != nil {
		s.fail(w, err)
		return
	}
	items, err := s.items(ctx, sess, entries)
	if err != nil {
		s.fail(w, err)
		return
	}
	jsonOut(w, map[string]any{"id": stateList, "updated": time.Now().Unix(), "items": items})
}

func (s *Server) streamContents(ctx context.Context, w http.ResponseWriter, r *http.Request, sess session, stream string) {
	f, err := s.filter(ctx, r, sess, stream)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.Limit = min(f.Limit, 1000)
	entries, next, err := s.Store.Entries(ctx, f, true)
	if err != nil {
		s.fail(w, err)
		return
	}
	items, err := s.items(ctx, sess, entries)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := map[string]any{"id": stream, "updated": time.Now().Unix(), "items": items}
	if next != 0 {
		out["continuation"] = strconv.FormatInt(next, 10)
	}
	jsonOut(w, out)
}

type link struct {
	Href string `json:"href"`
	Type string `json:"type,omitempty"`
}

type item struct {
	ID            string            `json:"id"`
	CrawlTimeMsec string            `json:"crawlTimeMsec"`
	TimestampUsec string            `json:"timestampUsec"`
	Published     int64             `json:"published"`
	Title         string            `json:"title"`
	Author        string            `json:"author,omitempty"`
	Canonical     []link            `json:"canonical"`
	Alternate     []link            `json:"alternate"`
	Categories    []string          `json:"categories"`
	Origin        map[string]string `json:"origin"`
	Summary       map[string]string `json:"summary"`
	Enclosure     []map[string]any  `json:"enclosure,omitempty"`
}

func (s *Server) items(ctx context.Context, sess session, entries []store.Entry) ([]item, error) {
	names, _, err := s.folderNames(ctx, sess.userID)
	if err != nil {
		return nil, err
	}
	feeds, err := s.Store.Feeds(ctx, sess.userID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]store.Feed, len(feeds))
	for _, f := range feeds {
		byID[f.ID] = f
	}
	out := make([]item, 0, len(entries))
	for _, e := range entries {
		f := byID[e.FeedID]
		cats := []string{stateList}
		if e.Read {
			cats = append(cats, stateRead)
		}
		if e.Starred {
			cats = append(cats, stateStar)
		}
		if f.FolderID != nil {
			cats = append(cats, labelPrefix+names[*f.FolderID])
		}
		it := item{
			ID:            LongID(e.ID),
			CrawlTimeMsec: strconv.FormatInt(e.ID/1000, 10),
			TimestampUsec: strconv.FormatInt(e.ID, 10),
			Published:     e.PublishedAt,
			Title:         e.Title,
			Author:        e.Author,
			Canonical:     []link{{Href: e.URL}},
			Alternate:     []link{{Href: e.URL}},
			Categories:    cats,
			Origin:        map[string]string{"streamId": feedPrefix + strconv.FormatInt(f.ID, 10), "title": f.Title, "htmlUrl": f.SiteURL},
			Summary:       map[string]string{"content": e.Content},
		}
		var encl []store.Enclosure
		json.Unmarshal([]byte(e.Enclosures), &encl)
		for _, en := range encl {
			m := map[string]any{"href": en.URL, "type": en.Type}
			if en.Length > 0 {
				m["length"] = en.Length
			}
			it.Enclosure = append(it.Enclosure, m)
		}
		out = append(out, it)
	}
	return out, nil
}

func (s *Server) editTag(ctx context.Context, w http.ResponseWriter, r *http.Request, sess session) {
	ids := make([]int64, 0, len(r.Form["i"]))
	for _, v := range r.Form["i"] {
		id, err := ParseItemID(v)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	apply := func(tags []string, onRead, onStar string) error {
		for _, t := range tags {
			var action string
			switch normalizeStream(t) {
			case stateRead:
				action = onRead
			case stateStar:
				action = onStar
			default:
				continue // labels and other tags are not supported yet
			}
			if _, err := s.Store.SetState(ctx, sess.userID, action, ids); err != nil {
				return err
			}
		}
		return nil
	}
	if err := apply(r.Form["a"], "read", "star"); err != nil {
		s.fail(w, err)
		return
	}
	if err := apply(r.Form["r"], "unread", "unstar"); err != nil {
		s.fail(w, err)
		return
	}
	plain(w, "OK")
}

func (s *Server) markAllRead(ctx context.Context, w http.ResponseWriter, r *http.Request, sess session) {
	stream := r.Form.Get("s")
	f, err := s.filter(ctx, &http.Request{Form: url.Values{}}, sess, stream)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	boundary := int64(1<<63 - 1)
	if v, err := strconv.ParseInt(r.Form.Get("ts"), 10, 64); err == nil && v > 0 {
		boundary = microseconds(v)
	}
	if _, err := s.Store.MarkAllRead(ctx, f, boundary); err != nil {
		s.fail(w, err)
		return
	}
	plain(w, "OK")
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	s.Log.Error("greader", "err", err)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}

func plain(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, body)
}

func jsonOut(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

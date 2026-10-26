package greader

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func TestParseItemID(t *testing.T) {
	const id = int64(1761480000123456)
	hex := fmt.Sprintf("%016x", id)
	for _, in := range []string{strconv.FormatInt(id, 10), hex, LongID(id), "  " + LongID(id)} {
		got, err := ParseItemID(in)
		if err != nil || got != id {
			t.Errorf("ParseItemID(%q) = %d, %v", in, got, err)
		}
	}
	if LongID(id) != "tag:google.com,2005:reader/item/"+hex {
		t.Errorf("LongID = %s", LongID(id))
	}
	for _, bad := range []string{"", "xyz", "tag:google.com,2005:reader/item/"} {
		if _, err := ParseItemID(bad); err == nil {
			t.Errorf("ParseItemID(%q) should fail", bad)
		}
	}
}

func TestMicroseconds(t *testing.T) {
	for in, want := range map[int64]int64{1761480000: 1761480000000000, 1761480000123: 1761480000123000, 1761480000123456: 1761480000123456} {
		if got := microseconds(in); got != want {
			t.Errorf("microseconds(%d) = %d, want %d", in, got, want)
		}
	}
}

type client struct {
	t    *testing.T
	base string
	auth string
}

func (c *client) do(method, path string, form url.Values) (int, string) {
	c.t.Helper()
	var body io.Reader
	if form != nil && method == http.MethodPost {
		body = strings.NewReader(form.Encode())
	} else if form != nil {
		path += "?" + form.Encode()
	}
	req, _ := http.NewRequest(method, c.base+Prefix+path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.auth != "" {
		req.Header.Set("Authorization", "GoogleLogin auth="+c.auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (c *client) json(method, path string, form url.Values, v any) {
	c.t.Helper()
	// Decode into a zero value: omitted fields must not keep earlier values.
	if p, ok := v.(interface{ reset() }); ok {
		p.reset()
	}
	code, body := c.do(method, path, form)
	if code != 200 {
		c.t.Fatalf("%s %s: %d %s", method, path, code, body)
	}
	if err := json.Unmarshal([]byte(body), v); err != nil {
		c.t.Fatalf("%s: %v: %s", path, err, body)
	}
}

type idRefs struct {
	ItemRefs     []struct{ ID string }
	Continuation string
}

func (r *idRefs) reset() { *r = idRefs{} }

func setup(t *testing.T) (*client, *store.Store, []int64) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	uid, _ := st.CreateUser(ctx, "isma", "web-password-123")
	st.SetAPIPassword(ctx, uid, "api-password-123")
	folder, _ := st.EnsureFolder(ctx, uid, "Tech & Co/Ops")
	feedID, _ := st.AddFeed(ctx, uid, "https://example.com/feed", "Example", &folder)
	var items []store.NewEntry
	for i := range 5 {
		items = append(items, store.NewEntry{GUID: fmt.Sprint("g", i), Title: fmt.Sprint("Post ", i),
			Content: "<p>body</p>", Enclosures: "[]", ContentHash: fmt.Sprint(i),
			PublishedAt: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)}) // old publication date
	}
	if _, _, err := st.UpsertEntries(ctx, feedID, items, time.Time{}); err != nil {
		t.Fatal(err)
	}
	ids, _, _ := st.EntryIDs(ctx, store.Filter{UserID: uid, Oldest: true})
	srv := httptest.NewServer(&Server{Store: st, Log: slog.New(slog.DiscardHandler)})
	t.Cleanup(srv.Close)
	return &client{t: t, base: srv.URL}, st, ids
}

func TestReaderFlow(t *testing.T) {
	c, _, ids := setup(t)

	// Unauthenticated and wrong-password requests get a plain 401.
	if code, body := c.do("GET", "/reader/api/0/subscription/list", url.Values{"output": {"json"}}); code != 401 || body != "Unauthorized!" {
		t.Fatalf("unauthenticated: %d %q", code, body)
	}
	if code, _ := c.do("POST", "/accounts/ClientLogin", url.Values{"Email": {"isma"}, "Passwd": {"web-password-123"}}); code != 401 {
		t.Fatal("web password must not authenticate the API")
	}

	code, body := c.do("POST", "/accounts/ClientLogin", url.Values{"Email": {"isma"}, "Passwd": {"api-password-123"}})
	if code != 200 || !strings.Contains(body, "LSID=null") {
		t.Fatalf("login: %d %s", code, body)
	}
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(line, "Auth="); ok {
			c.auth = v
		}
	}
	_, token := c.do("GET", "/reader/api/0/token", nil)
	token = strings.TrimSpace(token)
	if len(token) != 57 {
		t.Fatalf("token length %d", len(token))
	}

	var subs struct {
		Subscriptions []struct {
			ID         string
			Categories []struct{ ID, Label string }
		}
	}
	c.json("GET", "/reader/api/0/subscription/list", url.Values{"output": {"json"}}, &subs)
	if len(subs.Subscriptions) != 1 || subs.Subscriptions[0].Categories[0].ID != "user/-/label/Tech & Co/Ops" {
		t.Fatalf("subscriptions: %+v", subs)
	}

	// Item IDs come back as decimal strings, all unread.
	var refs idRefs
	c.json("GET", "/reader/api/0/stream/items/ids", url.Values{"s": {stateList}, "xt": {stateRead}, "n": {"3"}, "output": {"json"}}, &refs)
	if len(refs.ItemRefs) != 3 || refs.Continuation == "" {
		t.Fatalf("page 1: %+v", refs)
	}
	seen := map[string]bool{}
	for _, r := range refs.ItemRefs {
		seen[r.ID] = true
	}
	c.json("GET", "/reader/api/0/stream/items/ids", url.Values{"s": {stateList}, "xt": {stateRead}, "n": {"3"}, "c": {refs.Continuation}}, &refs)
	for _, r := range refs.ItemRefs {
		if seen[r.ID] {
			t.Fatalf("duplicate across pages: %s", r.ID)
		}
		seen[r.ID] = true
	}
	if len(seen) != 5 || refs.Continuation != "" {
		t.Fatalf("pagination: %d ids, continuation %q", len(seen), refs.Continuation)
	}

	// Entries published in 2019 but crawled now are found by ot (crawl time).
	c.json("GET", "/reader/api/0/stream/items/ids", url.Values{"s": {stateList}, "ot": {strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)}, "n": {"100"}}, &refs)
	if len(refs.ItemRefs) != 5 {
		t.Fatalf("ot filter: got %d", len(refs.ItemRefs))
	}

	// Contents accepts every ID form via repeated i fields.
	form := url.Values{"i": {strconv.FormatInt(ids[0], 10), fmt.Sprintf("%016x", ids[1]), LongID(ids[2])}}
	var contents struct {
		Items []struct {
			ID, TimestampUsec, CrawlTimeMsec string
			Published                        int64
			Categories                       []string
		}
	}
	c.json("POST", "/reader/api/0/stream/items/contents", form, &contents)
	if len(contents.Items) != 3 || contents.Items[1].ID != LongID(ids[1]) {
		t.Fatalf("contents: %+v", contents)
	}
	it := contents.Items[0]
	if it.TimestampUsec != strconv.FormatInt(ids[0], 10) || it.CrawlTimeMsec != strconv.FormatInt(ids[0]/1000, 10) || it.Published != 1546300800 {
		t.Fatalf("timestamps: %+v", it)
	}

	// edit-tag is idempotent and returns exactly OK.
	edit := url.Values{"T": {"x"}, "i": {fmt.Sprintf("%016x", ids[0])}, "a": {stateRead, stateStar}}
	for range 2 {
		if code, body := c.do("POST", "/reader/api/0/edit-tag", edit); code != 200 || body != "OK" {
			t.Fatalf("edit-tag: %d %q", code, body)
		}
	}
	if code, _ := c.do("POST", "/reader/api/0/edit-tag", url.Values{"T": {"wrong"}, "i": {"1"}, "a": {stateRead}}); code != 401 {
		t.Fatal("bad T accepted")
	}
	c.json("GET", "/reader/api/0/stream/items/ids", url.Values{"s": {stateStar}}, &refs)
	if len(refs.ItemRefs) != 1 || refs.ItemRefs[0].ID != strconv.FormatInt(ids[0], 10) {
		t.Fatalf("starred: %+v", refs)
	}

	var counts struct {
		Max          int
		Unreadcounts []struct {
			ID     string
			Count  int
			Newest string `json:"newestItemTimestampUsec"`
		}
	}
	c.json("GET", "/reader/api/0/unread-count", url.Values{"output": {"json"}}, &counts)
	if counts.Max != 4 {
		t.Fatalf("unread max = %d", counts.Max)
	}

	// mark-all-as-read honours the boundary: only items at/before ts change.
	boundary := ids[2]
	if code, body := c.do("POST", "/reader/api/0/mark-all-as-read", url.Values{"T": {token}, "s": {"user/-/label/Tech & Co/Ops"}, "ts": {strconv.FormatInt(boundary, 10)}}); code != 200 || body != "OK" {
		t.Fatalf("mark-all: %d %q", code, body)
	}
	c.json("GET", "/reader/api/0/stream/items/ids", url.Values{"s": {stateList}, "xt": {stateRead}, "n": {"100"}}, &refs)
	if len(refs.ItemRefs) != 2 {
		t.Fatalf("after mark-all: %d unread, want 2", len(refs.ItemRefs))
	}
}

// TestLegacyFreshRSSToken checks that a token issued by FreshRSS keeps working
// after migration, computed independently as FreshRSS does:
// sha1(salt . user . apiPasswordHash), with T padded to 57 characters by "Z".
func TestLegacyFreshRSSToken(t *testing.T) {
	c, st, ids := setup(t)
	ctx := context.Background()
	uid, _ := st.UserIDByName(ctx, "isma")
	const salt = "0123456789abcdef0123456789abcdef01234567"
	// A PHP-style $2y$ hash for the API password, as FreshRSS stores it.
	h, _ := bcrypt.GenerateFromPassword([]byte("api-password-123"), 10)
	apiHash := "$2y$" + string(h[4:])
	webHash, _ := bcrypt.GenerateFromPassword([]byte("web-password-123"), 10)
	if _, err := st.ImportUser(ctx, "isma", string(webHash), apiHash); err != nil {
		t.Fatal(err)
	}
	st.SetSetting(ctx, store.SettingFreshRSSSalt, salt)
	sum := sha1.Sum([]byte(salt + "isma" + apiHash))
	freshToken := hex.EncodeToString(sum[:])
	c.auth = "isma/" + freshToken

	// Off by default: the FreshRSS token is refused.
	if code, _ := c.do("GET", "/reader/api/0/user-info", nil); code != 401 {
		t.Fatalf("legacy token accepted while disabled: %d", code)
	}
	st.SetLegacyAuth(ctx, uid, true)
	if code, _ := c.do("GET", "/reader/api/0/user-info", nil); code != 200 {
		t.Fatalf("legacy token refused: %d", code)
	}
	// T is FreshRSS's token, padded exactly as FreshRSS pads it.
	_, tok := c.do("GET", "/reader/api/0/token", nil)
	if want := freshToken + strings.Repeat("Z", 57-len(freshToken)); strings.TrimSpace(tok) != want {
		t.Fatalf("T = %q, want %q", tok, want)
	}
	edit := url.Values{"T": {strings.TrimSpace(tok)}, "i": {strconv.FormatInt(ids[0], 10)}, "a": {stateStar}}
	if code, body := c.do("POST", "/reader/api/0/edit-tag", edit); code != 200 || body != "OK" {
		t.Fatalf("write with legacy T: %d %q", code, body)
	}
	// Signing in again returns the same token FreshRSS would.
	login := &client{t: t, base: c.base}
	_, body := login.do("POST", "/accounts/ClientLogin", url.Values{"Email": {"isma"}, "Passwd": {"api-password-123"}})
	if !strings.Contains(body, "Auth=isma/"+freshToken+"\n") {
		t.Fatalf("ClientLogin under legacy auth: %q", body)
	}
	// A wrong token for the right user, or the right token for another user, fails.
	bad := &client{t: t, base: c.base, auth: "isma/" + strings.Repeat("0", 40)}
	if code, _ := bad.do("GET", "/reader/api/0/user-info", nil); code != 401 {
		t.Fatal("wrong legacy token accepted")
	}
	// Changing the API password retires the token, as in FreshRSS.
	st.SetAPIPassword(ctx, uid, "a-brand-new-api-password")
	if code, _ := c.do("GET", "/reader/api/0/user-info", nil); code != 401 {
		t.Fatal("legacy token still valid after password change")
	}
	// And it can be switched off outright.
	st.ImportUser(ctx, "isma", string(webHash), apiHash)
	st.SetLegacyAuth(ctx, uid, false)
	if code, _ := c.do("GET", "/reader/api/0/user-info", nil); code != 401 {
		t.Fatal("legacy token valid after disabling")
	}
}

func TestUnicodeAlternative(t *testing.T) {
	cases := []struct {
		in       string
		extended bool
		want     string
	}{
		{"Trash & Finds <b>", false, "Trash ＆ Finds ＜b＞"},
		{"  spaced  title  ", false, "spaced  title"}, // trimmed, inner spaces kept
		{"What? A/B, C; D\\E", false, "What? A/B, C; D\\E"},
		{"What? A/B, C; D\\E", true, "What？ A／B， C； D＼E"},
		{`It's "quoted" ^`, true, `It's "quoted" ^`}, // FreshRSS's += quirk leaves these
	}
	for _, c := range cases {
		if got := UnicodeAlternative(c.in, c.extended); got != c.want {
			t.Errorf("UnicodeAlternative(%q, %v) = %q, want %q", c.in, c.extended, got, c.want)
		}
	}
}

package greader

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/arenzana/correspondent/internal/store"
)

// fakeSubscriber adds plain feeds, as ingest.SubscribeAuto would after
// working out what an address offers.
type fakeSubscriber struct {
	st    *store.Store
	calls []string
}

func (f *fakeSubscriber) SubscribeAuto(ctx context.Context, userID int64, raw, title string, folderID *int64) (*store.Feed, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s|%s|%v", raw, title, folderID != nil))
	if strings.Contains(raw, "nothing") {
		return nil, errors.New("no feed found at that address")
	}
	if title == "" {
		title = "Found: " + raw
	}
	id, err := f.st.AddFeed(ctx, userID, raw, title, folderID)
	if err != nil {
		return nil, err
	}
	return f.st.Feed(ctx, userID, id)
}

func login(t *testing.T, c *client) string {
	t.Helper()
	_, body := c.do("POST", "/accounts/ClientLogin", url.Values{"Email": {"reader"}, "Passwd": {"api-password-123"}})
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(line, "Auth="); ok {
			c.auth = v
		}
	}
	_, token := c.do("GET", "/reader/api/0/token", nil)
	return strings.TrimSpace(token)
}

func TestSubscriptionsFromApps(t *testing.T) {
	c, st, _ := setup(t)
	sub := &fakeSubscriber{st: st}
	c.server.Subscriber = sub
	tok := login(t, c)

	// Writes need the T token.
	if code, _ := c.do("POST", "/reader/api/0/subscription/quickadd", url.Values{"quickadd": {"https://lab.example/"}}); code != 401 {
		t.Errorf("quickadd without T: %d", code)
	}
	var added struct {
		NumResults int
		StreamID   string
		StreamName string
		Error      string
	}
	c.json("POST", "/reader/api/0/subscription/quickadd", url.Values{"quickadd": {"feed/lab.example"}, "T": {tok}}, &added)
	if added.NumResults != 1 || !strings.HasPrefix(added.StreamID, "feed/") || !strings.HasPrefix(added.StreamName, "Found: https:") {
		t.Fatalf("%+v", added)
	}
	c.json("POST", "/reader/api/0/subscription/quickadd", url.Values{"quickadd": {"nothing.example"}, "T": {tok}}, &added)
	if added.NumResults != 0 || !strings.Contains(added.Error, "no feed") {
		t.Fatalf("%+v", added)
	}
	for _, bad := range []string{"javascript:alert(1)", "file:///etc/passwd"} {
		c.json("POST", "/reader/api/0/subscription/quickadd", url.Values{"quickadd": {bad}, "T": {tok}}, &added)
		if added.NumResults != 0 {
			t.Errorf("%s accepted", bad)
		}
	}

	// subscribe with a title and folder; rename and move; unsubscribe.
	if code, body := c.do("POST", "/reader/api/0/subscription/edit", url.Values{"ac": {"subscribe"}, "s": {"feed/https://news.example/"},
		"t": {"News"}, "a": {"user/-/label/Reading"}, "T": {tok}}); code != 200 || body != "OK" {
		t.Fatalf("subscribe: %d %s", code, body)
	}
	if got := sub.calls[len(sub.calls)-1]; got != "https://news.example/|News|true" {
		t.Errorf("subscriber got %q", got)
	}
	var subs struct {
		Subscriptions []struct {
			ID, Title, URL string
			Categories     []struct{ Label string }
		}
	}
	c.json("GET", "/reader/api/0/subscription/list", url.Values{"output": {"json"}}, &subs)
	var newsID string
	for _, s := range subs.Subscriptions {
		if s.URL == "https://news.example/" {
			newsID = s.ID
			if s.Title != "News" || len(s.Categories) != 1 || s.Categories[0].Label != "Reading" {
				t.Errorf("%+v", s)
			}
		}
	}
	if newsID == "" {
		t.Fatalf("not listed: %+v", subs)
	}
	c.do("POST", "/reader/api/0/subscription/edit", url.Values{"ac": {"edit"}, "s": {newsID}, "t": {"Daily news"},
		"r": {"user/-/label/Reading"}, "T": {tok}})
	c.json("GET", "/reader/api/0/subscription/list", url.Values{"output": {"json"}}, &subs)
	for _, s := range subs.Subscriptions {
		if s.ID == newsID && (s.Title != "Daily news" || len(s.Categories) != 0) {
			t.Errorf("after edit: %+v", s)
		}
	}
	// By address as well as by ID.
	if code, _ := c.do("POST", "/reader/api/0/subscription/edit", url.Values{"ac": {"unsubscribe"}, "s": {"feed/https://news.example/"}, "T": {tok}}); code != 200 {
		t.Errorf("unsubscribe: %d", code)
	}
	c.json("GET", "/reader/api/0/subscription/list", url.Values{"output": {"json"}}, &subs)
	for _, s := range subs.Subscriptions {
		if s.ID == newsID {
			t.Error("still subscribed")
		}
	}
	if code, _ := c.do("POST", "/reader/api/0/subscription/edit", url.Values{"ac": {"unsubscribe"}, "s": {"feed/999999"}, "T": {tok}}); code != 404 {
		t.Errorf("unknown feed: %d", code)
	}
	if code, _ := c.do("POST", "/reader/api/0/subscription/edit", url.Values{"ac": {"explode"}, "s": {newsID}, "T": {tok}}); code != 400 {
		t.Errorf("unknown action: %d", code)
	}
}

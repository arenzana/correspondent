package migrate

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arenzana/correspondent/internal/store"
)

const opmlDoc = `<?xml version="1.0"?><opml version="2.0"><body>
<outline text="Tech">
  <outline text="Releases" xmlUrl="https://r.example/feed" htmlUrl="https://r.example/"/>
  <outline text="Bad" xmlUrl="javascript:alert(1)"/>
</outline>
<outline text="Blog" xmlUrl="https://b.example/feed" htmlUrl="javascript:alert(2)"/>
</body></opml>`

const subsDoc = `{"subscriptions":[
 {"id":"feed/1","url":"https://r.example/feed"},
 {"id":"feed/82","url":"https://b.example/feed"}]}`

const starredDoc = `{"items":[
 {"frss:id":"1698096807589386","id":"tag:google.com,2005:reader/item/00060868f902b60a",
  "guid":"g1","title":"Release &amp; notes","published":1698442320,
  "canonical":[{"href":"https://r.example/1"}],
  "categories":["user/-/state/com.google/reading-list","user/-/state/com.google/read","user/-/state/com.google/starred"],
  "origin":{"streamId":"feed/1"},
  "content":{"content":"<p onclick=\"x()\">Hi<script>alert(1)</script></p>"}},
 {"id":"tag:google.com,2005:reader/item/00060868f902b60b","title":"Unread star",
  "canonical":[{"href":"javascript:alert(3)"}],
  "categories":["user/-/state/com.google/starred"],
  "origin":{"streamId":"feed/82"},"summary":{"content":"<p>x</p>"}},
 {"frss:id":"5","title":"Orphan","origin":{"streamId":"feed/999"}}
]}`

func TestFreshRSSImport(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	uid, _ := st.CreateUser(ctx, "u", "password-123")

	rep, err := FreshRSS(ctx, st, uid, Sources{
		OPML:          strings.NewReader(opmlDoc),
		Starred:       strings.NewReader(starredDoc),
		Subscriptions: strings.NewReader(subsDoc),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Feeds != 2 || rep.FeedIDsKept != 2 || rep.Folders != 1 || rep.Starred != 2 {
		t.Fatalf("report %+v", rep)
	}
	if len(rep.Warnings) != 2 { // the javascript: feed and the orphan item
		t.Fatalf("warnings %v", rep.Warnings)
	}

	f, err := st.Feed(ctx, uid, 82)
	if err != nil || f.URL != "https://b.example/feed" || f.SiteURL != "" {
		t.Fatalf("feed 82: %+v %v (javascript: site URL must be dropped)", f, err)
	}

	e, err := st.Entry(ctx, uid, 1698096807589386)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Starred || !e.Read || e.FeedID != 1 || e.Title != "Release & notes" || e.PublishedAt != 1698442320 {
		t.Fatalf("entry 1: %+v", e)
	}
	if strings.Contains(e.Content, "script") || strings.Contains(e.Content, "onclick") {
		t.Fatalf("imported content not sanitized: %s", e.Content)
	}

	e2, err := st.Entry(ctx, uid, 0x00060868f902b60b)
	if err != nil {
		t.Fatal(err)
	}
	if e2.Read || !e2.Starred || e2.URL != "" {
		t.Fatalf("entry 2: %+v (javascript: link must be dropped, read state kept)", e2)
	}

	// Re-running the import changes nothing.
	rep2, err := FreshRSS(ctx, st, uid, Sources{
		OPML: strings.NewReader(opmlDoc), Starred: strings.NewReader(starredDoc), Subscriptions: strings.NewReader(subsDoc),
	})
	if err != nil || rep2.Starred != 0 || rep2.FeedIDsKept != 2 {
		t.Fatalf("second run: %+v %v", rep2, err)
	}
}

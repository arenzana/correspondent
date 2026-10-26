package opml

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseNestedAndFlat(t *testing.T) {
	doc := `<?xml version="1.0"?><opml version="2.0"><body>
<outline text="Loose" xmlUrl="https://a.example/feed"/>
<outline text="Tech">
  <outline title="B &amp; Co" text="ignored" xmlUrl="https://b.example/feed" htmlUrl="https://b.example/"/>
  <outline text="Deeper"><outline text="C" xmlUrl="https://c.example/feed"/></outline>
</outline>
<outline text="Empty folder"/>
</body></opml>`
	subs, err := Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	want := []Subscription{
		{Folder: "", Title: "Loose", URL: "https://a.example/feed"},
		{Folder: "Tech", Title: "B & Co", URL: "https://b.example/feed", SiteURL: "https://b.example/"},
		{Folder: "Tech", Title: "C", URL: "https://c.example/feed"}, // nested folders collapse outward
	}
	if len(subs) != len(want) {
		t.Fatalf("got %+v", subs)
	}
	for i := range want {
		if subs[i] != want[i] {
			t.Errorf("sub %d = %+v, want %+v", i, subs[i], want[i])
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := []Subscription{
		{Folder: "News", Title: "A <&> B", URL: "https://a.example/feed?x=1&y=2"},
		{Folder: "", Title: "Loose", URL: "https://l.example/feed"},
		{Folder: "News", Title: "C", URL: "https://c.example/feed"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, "t", in); err != nil {
		t.Fatal(err)
	}
	out, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Subscription{}
	for _, s := range out {
		got[s.URL] = s
	}
	for _, s := range in {
		if got[s.URL].Title != s.Title || got[s.URL].Folder != s.Folder {
			t.Errorf("round trip lost %+v, got %+v", s, got[s.URL])
		}
	}
}

func TestRejectsGarbage(t *testing.T) {
	if _, err := Parse(strings.NewReader("not xml at all")); err == nil {
		t.Fatal("expected an error")
	}
}

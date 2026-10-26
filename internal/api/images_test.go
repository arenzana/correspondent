package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/arenzana/correspondent/internal/imgproxy"
	"github.com/arenzana/correspondent/internal/store"
)

func TestImagesAreProxied(t *testing.T) {
	e := newEnv(t)
	p := &imgproxy.Proxy{Key: []byte("test key")}
	e.api.Images = p
	ctx := context.Background()
	uid, _ := e.store.CreateUser(ctx, "carol", "password-carol")
	fid, _ := e.store.AddFeed(ctx, uid, "https://carol.example/feed", "Carol", nil)
	e.store.SaveFetchResult(ctx, fid, store.FetchResult{Status: 200, IconURL: "https://carol.example/icon.png", NextFetch: time.Now()})
	e.store.UpsertEntries(ctx, fid, []store.NewEntry{{
		GUID: "c-1", Title: "Pictures", Enclosures: "[]",
		ImageURL: "https://cdn.example/lead.jpg",
		Content:  `<p>Look</p><img src="https://cdn.example/lead.jpg" alt=""><img src="data:image/gif;base64,R0lGOD">`,
	}}, time.Time{})
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	e.do(t, c, "POST", "/api/v1/session", `{"username":"carol","password":"password-carol"}`, true)

	var tree struct{ Feeds []struct{ IconURL string } }
	json.NewDecoder(e.do(t, c, "GET", "/api/v1/tree", "", false).Body).Decode(&tree)
	if want := p.URL("https://carol.example/icon.png", iconWidth); len(tree.Feeds) != 1 || tree.Feeds[0].IconURL != want {
		t.Errorf("icon %+v, want %s", tree.Feeds, want)
	}

	var list struct {
		Entries []struct {
			ID       string
			ImageURL string
			Content  string
		}
	}
	json.NewDecoder(e.do(t, c, "GET", "/api/v1/entries?view=all&content=1", "", false).Body).Decode(&list)
	if len(list.Entries) != 1 {
		t.Fatalf("entries %+v", list.Entries)
	}
	got := list.Entries[0]
	if want := p.URL("https://cdn.example/lead.jpg", thumbWidth); got.ImageURL != want {
		t.Errorf("thumbnail %s, want %s", got.ImageURL, want)
	}
	if body := p.URL("https://cdn.example/lead.jpg", bodyWidth); !strings.Contains(got.Content, `src="`+body+`"`) ||
		strings.Contains(got.Content, `src="https://`) || !strings.Contains(got.Content, `src="data:image/gif`) {
		t.Errorf("content %s", got.Content)
	}

	var one struct{ Entry struct{ Content string } }
	json.NewDecoder(e.do(t, c, "GET", "/api/v1/entries/"+got.ID, "", false).Body).Decode(&one)
	if strings.Contains(one.Entry.Content, "cdn.example") {
		t.Errorf("single entry not proxied: %s", one.Entry.Content)
	}

	// The stored article is untouched: only responses are rewritten.
	id := fmt.Sprint(got.ID)
	var n int64
	fmt.Sscan(id, &n)
	stored, _ := e.store.Entry(ctx, uid, n)
	if !strings.Contains(stored.Content, "https://cdn.example/lead.jpg") {
		t.Errorf("stored content changed: %s", stored.Content)
	}
}

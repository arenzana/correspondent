// Package extract fetches an article's web page and pulls out the article
// itself (Readability, as in Firefox's Reader View), for feeds that publish
// only titles or summaries. The page is fetched with the SSRF-safe client
// and the result goes through the same sanitizer as feed content.
package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"

	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/fetch"
)

// minText is the shortest extraction accepted as an article; shorter
// usually means a paywall, a cookie wall, or a page that isn't an article.
const minText = 200

var ErrNotArticle = errors.New("the page has no readable article")

type Result struct {
	HTML     string // sanitized
	Text     string
	ImageURL string
}

type Extractor struct {
	Fetch *fetch.Client
}

// Article fetches pageURL and extracts its main article.
func (x *Extractor) Article(ctx context.Context, pageURL string) (*Result, error) {
	if !strings.HasPrefix(pageURL, "https://") && !strings.HasPrefix(pageURL, "http://") {
		return nil, errors.New("the article has no web address")
	}
	resp, err := x.Fetch.Get(ctx, fetch.Request{URL: pageURL})
	if err != nil {
		return nil, fmt.Errorf("couldn't load the page: %w", err)
	}
	if ct := strings.ToLower(resp.ContentType); ct != "" && !strings.Contains(ct, "html") {
		return nil, fmt.Errorf("the page isn't HTML (%s)", ct)
	}
	final, err := url.Parse(resp.FinalURL)
	if err != nil {
		return nil, err
	}
	// Parse only: readability.FromURL would fetch on its own, bypassing the
	// fetcher's address checks and limits.
	article, err := readability.FromReader(bytes.NewReader(resp.Body), final)
	if err != nil {
		return nil, ErrNotArticle
	}
	var raw bytes.Buffer
	if err := article.RenderHTML(&raw); err != nil {
		return nil, err
	}
	body := content.Process(raw.String(), final.String())
	if len([]rune(body.Text)) < minText {
		return nil, ErrNotArticle
	}
	r := &Result{HTML: body.HTML, Text: body.Text, ImageURL: body.ImageURL}
	if r.ImageURL == "" {
		if img := article.ImageURL(); strings.HasPrefix(img, "https://") || strings.HasPrefix(img, "http://") {
			r.ImageURL = img
		}
	}
	return r, nil
}

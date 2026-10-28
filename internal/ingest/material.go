package ingest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/arenzana/correspondent/internal/content"
	"github.com/arenzana/correspondent/internal/feed"
	"github.com/arenzana/correspondent/internal/store"
	"github.com/arenzana/correspondent/internal/typesafe"
)

// Website material the rules can't recognise (a site's own newsletter
// box, an author bio, a podcast's membership plug) is found, when the
// admin has given the server a TypeSafe key, by asking Jev about each
// block at the article's start and end. Blocks go from the outside in,
// stopping at the first one Jev doesn't confidently call the website's,
// so nothing in the middle of an article is ever removed and one doubtful
// block keeps everything inside it. Without a key, articles keep what the
// rules leave.
const (
	materialEdge  = 6   // blocks asked about at each end, at most
	materialSure  = 0.9 // how sure Jev must be that a block is the website's
	materialChars = 600 // longer blocks are the article's, unasked
	materialWait  = 30 * time.Second
)

var materialQuestion = typesafe.Noul{
	Instructions: "`block` is website material around the article rather than part of the article.",
	True: "Website material around the article: newsletter or subscription prompts, membership offers, " +
		"share/follow/comment prompts, boilerplate author bios, timestamps and bylines, topic tags, lists of " +
		"related, recent, latest or popular articles, shopping or deal plugs, affiliate disclosures, ads, " +
		"navigation, cookie or paywall notices, or a line saying where the post first appeared.",
	False: "Anything the author wrote for this article: its paragraphs, headings, quotations, lists, images " +
		"and captions, notes and corrections, even near the end.",
}

// jevSlots bounds the requests to TypeSafe in flight across all feeds.
var jevSlots = make(chan struct{}, 8)

// dropMaterials asks about a poll's new articles, the newest first and no
// more of them than full articles are fetched.
func (s *Service) dropMaterials(ctx context.Context, p *feed.Parsed, known map[string]bool) {
	if s.Jev == nil {
		return
	}
	var wg sync.WaitGroup
	n := 0
	for i := range p.Items {
		it := &p.Items[i]
		if known[it.GUID] || n >= fullPerPoll {
			continue
		}
		n++
		wg.Add(1)
		go func() {
			defer wg.Done()
			it.Content = s.dropMaterial(ctx, it.Title, p.Title, it.Content)
		}()
	}
	wg.Wait()
}

func guidsOf(items []store.NewEntry) []string {
	guids := make([]string, len(items))
	for i, it := range items {
		guids[i] = it.GUID
	}
	return guids
}

// dropMaterial removes the website material Jev finds at an article's
// ends, returning the article unchanged without a key or on any failure.
func (s *Service) dropMaterial(ctx context.Context, title, site, htmlText string) string {
	if s.Jev == nil || htmlText == "" {
		return htmlText
	}
	blocks := content.Blocks(htmlText)
	if len(blocks) < 3 {
		return htmlText
	}
	described := make([]string, len(blocks))
	for i, b := range blocks {
		described[i] = content.Describe(b)
	}
	ctx, cancel := context.WithTimeout(ctx, materialWait)
	defer cancel()
	// Each end is peeled in turn, one block at a time; the two ends run
	// together. Neither may take more than a third of the article.
	limit := min(materialEdge, len(blocks)/3)
	var head, tail int
	var wg sync.WaitGroup
	peel := func(n *int, index func(k int) int) {
		defer wg.Done()
		for *n < limit {
			i := index(*n)
			if !s.isMaterial(ctx, title, site, described, blocks[i], i) {
				return
			}
			*n++
		}
	}
	wg.Add(2)
	go peel(&head, func(k int) int { return k })
	go peel(&tail, func(k int) int { return len(blocks) - 1 - k })
	wg.Wait()
	if head == 0 && tail == 0 {
		return htmlText
	}
	s.Log.Info("website material", "title", title, "start", head, "end", tail)
	return strings.Join(blocks[head:len(blocks)-tail], "\n")
}

// isMaterial asks Jev whether block i is the website's. Blocks it needn't
// ask about answer for themselves: an empty one or a rule goes with its
// neighbours, a long one or one leading on to the rest of the article
// stays.
func (s *Service) isMaterial(ctx context.Context, title, site string, described []string, block string, i int) bool {
	text := content.PlainText(block)
	switch {
	case strings.TrimSpace(text) == "" && !hasMedia(block):
		return true
	case len([]rune(text)) > materialChars || content.Onward(text):
		return false
	}
	select {
	case jevSlots <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	defer func() { <-jevSlots }()
	state := map[string]any{
		"article_title": title,
		"site":          site,
		"position":      positionOf(i, len(described)),
		"before":        described[max(0, i-2):i],
		"block":         described[i],
		"after":         described[i+1 : min(len(described), i+3)],
	}
	ps, err := s.Jev.Ask(ctx, state, map[string]typesafe.Noul{"material": materialQuestion})
	if err != nil {
		s.Log.Warn("website material", "error", err)
		return false
	}
	return ps["material"] >= materialSure
}

// hasMedia reports whether a block shows something besides text.
func hasMedia(block string) bool {
	for _, tag := range []string{"<img", "<audio", "<video", "<iframe", "<table"} {
		if strings.Contains(block, tag) {
			return true
		}
	}
	return false
}

func positionOf(i, n int) string {
	return fmt.Sprintf("block %d of %d", i+1, n)
}

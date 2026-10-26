package migrate

import (
	"context"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/arenzana/correspondent/internal/store"
)

const (
	streamAll     = "user/-/state/com.google/reading-list"
	streamRead    = "user/-/state/com.google/read"
	streamStarred = "user/-/state/com.google/starred"
)

// Verify compares what a Reader client sees on FreshRSS (old) and
// Correspondent (new). Any blocking difference means Reeder would duplicate,
// lose or misfile something, so the cutover must not go ahead. Run it with
// polling paused on both sides, right after the final import.
func Verify(ctx context.Context, old, new *ReaderClient, out io.Writer, full bool) (ok bool, err error) {
	ok = true
	fail := func(format string, a ...any) {
		ok = false
		fmt.Fprintf(out, "  ✗ "+format+"\n", a...)
	}
	pass := func(format string, a ...any) { fmt.Fprintf(out, "  ✓ "+format+"\n", a...) }

	fmt.Fprintln(out, "Subscriptions")
	so, err := old.Subscriptions(ctx)
	if err != nil {
		return false, fmt.Errorf("FreshRSS: %w", err)
	}
	sn, err := new.Subscriptions(ctx)
	if err != nil {
		return false, fmt.Errorf("Correspondent: %w", err)
	}
	subKey := func(s Subscription) string {
		var cats []string
		for _, c := range s.Categories {
			cats = append(cats, c.ID)
		}
		slices.Sort(cats)
		return s.Title + " | " + redactURL(s.URL) + " | " + strings.Join(cats, ",")
	}
	sameSub := func(a, b Subscription) bool {
		// Compare full URLs, but never print them: feed URLs can carry tokens.
		return a.URL == b.URL && subKey(a) == subKey(b)
	}
	byID := func(list []Subscription) map[string]Subscription {
		m := map[string]Subscription{}
		for _, s := range list {
			m[s.ID] = s
		}
		return m
	}
	mo, mn := byID(so), byID(sn)
	for id, s := range mo {
		t, found := mn[id]
		switch {
		case !found:
			fail("%s (%s) is missing", id, s.Title)
		case !sameSub(s, t):
			fail("%s differs: %q vs %q", id, subKey(s), subKey(t))
		}
	}
	for id, s := range mn {
		if _, found := mo[id]; !found {
			fail("%s (%s) exists only in Correspondent", id, s.Title)
		}
	}
	if len(mo) == len(mn) && ok {
		pass("%d feeds with the same IDs, addresses and folders", len(mo))
	}

	fmt.Fprintln(out, "Folders")
	to, err := old.Tags(ctx)
	if err != nil {
		return false, err
	}
	tn, err := new.Tags(ctx)
	if err != nil {
		return false, err
	}
	labels := func(ids []string) []string {
		var l []string
		for _, id := range ids {
			if strings.Contains(id, "/label/") {
				l = append(l, id)
			}
		}
		slices.Sort(l)
		return l
	}
	if lo, ln := labels(to), labels(tn); slices.Equal(lo, ln) {
		pass("%d folders", len(lo))
	} else {
		fail("folders differ: %v vs %v", lo, ln)
	}

	fmt.Fprintln(out, "Unread and starred sets")
	sets := []struct{ name, stream, exclude string }{
		{"unread", streamAll, streamRead},
		{"starred", streamStarred, ""},
	}
	var sample []string
	for _, s := range sets {
		a, err := old.IDs(ctx, s.stream, s.exclude, 0)
		if err != nil {
			return false, err
		}
		b, err := new.IDs(ctx, s.stream, s.exclude, 0)
		if err != nil {
			return false, err
		}
		missing, extra := diff(a, b)
		if len(missing)+len(extra) == 0 {
			pass("%d %s items, identical IDs", len(a), s.name)
		} else {
			fail("%s: %d missing in Correspondent %v, %d extra %v", s.name, len(missing), head(missing), len(extra), head(extra))
		}
		sample = append(sample, a...)
	}

	fmt.Fprintln(out, "Unread counts")
	co, err := old.UnreadCounts(ctx)
	if err != nil {
		return false, err
	}
	cn, err := new.UnreadCounts(ctx)
	if err != nil {
		return false, err
	}
	counts := func(list []UnreadCount) map[string]int {
		m := map[string]int{}
		for _, c := range list {
			m[c.ID] = c.Count
		}
		return m
	}
	ko, kn := counts(co), counts(cn)
	mismatch := 0
	for _, id := range slices.Sorted(maps.Keys(ko)) {
		if ko[id] != kn[id] {
			mismatch++
			fail("%s: %d vs %d", id, ko[id], kn[id])
		}
	}
	if mismatch == 0 {
		pass("%d streams match", len(ko))
	}

	fmt.Fprintln(out, "Recent history")
	ro, err := old.IDs(ctx, streamAll, "", 2000)
	if err != nil {
		return false, err
	}
	rn, err := new.IDs(ctx, streamAll, "", 0)
	if err != nil {
		return false, err
	}
	if missing, _ := diff(ro, rn); len(missing) == 0 {
		pass("the newest %d FreshRSS items all exist with the same IDs", len(ro))
	} else {
		fail("%d recent FreshRSS items are missing: %v", len(missing), head(missing))
	}

	fmt.Fprintln(out, "Item fields")
	if full {
		// Every item FreshRSS has, not a sample.
		if sample, err = old.IDs(ctx, streamAll, "", 0); err != nil { // every item
			return false, err
		}
	} else {
		sample = append(sample, ro[:min(len(ro), 200)]...)
		slices.Sort(sample)
		sample = slices.Compact(sample)
		rand.Shuffle(len(sample), func(i, j int) { sample[i], sample[j] = sample[j], sample[i] })
		sample = sample[:min(len(sample), 300)]
	}
	io_, err := old.Items(ctx, sample)
	if err != nil {
		return false, fmt.Errorf("FreshRSS: %w", err)
	}
	in, err := new.Items(ctx, sample)
	if err != nil {
		return false, fmt.Errorf("Correspondent: %w", err)
	}
	newByID := map[string]Item{}
	for _, it := range in {
		newByID[it.ID] = it
	}
	bad := 0
	for _, a := range io_ {
		b, found := newByID[a.ID]
		if !found {
			bad++
			fail("item %s missing", a.ID)
			continue
		}
		if problems := compareItems(a, b); len(problems) > 0 {
			bad++
			if bad <= 10 {
				fail("item %s: %s", a.ID, strings.Join(problems, "; "))
			}
		}
	}
	if bad == 0 {
		what := "sampled"
		if full {
			what = "of all"
		}
		pass("%d %s items match (IDs, timestamps, title, link, feed, state, folder)", len(io_), what)
	} else if bad > 10 {
		fail("…and %d more items differ", bad-10)
	}
	return ok, nil
}

func compareItems(a, b Item) []string {
	var p []string
	if a.CrawlTimeMsec != b.CrawlTimeMsec || a.TimestampUsec != b.TimestampUsec {
		p = append(p, fmt.Sprintf("timestamps %s/%s vs %s/%s", a.CrawlTimeMsec, a.TimestampUsec, b.CrawlTimeMsec, b.TimestampUsec))
	}
	if a.Published != b.Published {
		p = append(p, fmt.Sprintf("published %d vs %d", a.Published, b.Published))
	}
	if strings.TrimSpace(a.Title) != strings.TrimSpace(b.Title) {
		p = append(p, fmt.Sprintf("title %q vs %q", a.Title, b.Title))
	}
	if a.Origin.StreamID != b.Origin.StreamID {
		p = append(p, fmt.Sprintf("feed %s vs %s", a.Origin.StreamID, b.Origin.StreamID))
	}
	if href(a) != href(b) {
		p = append(p, fmt.Sprintf("link %s vs %s", redactURL(href(a)), redactURL(href(b))))
	}
	// Compare the categories Reeder acts on; FreshRSS adds its own states.
	relevant := func(cats []string) []string {
		var out []string
		for _, c := range cats {
			c = strings.Replace(c, "user/-/", "", 1)
			if strings.HasPrefix(c, "state/com.google/") || strings.HasPrefix(c, "label/") {
				out = append(out, c)
			}
		}
		slices.Sort(out)
		return out
	}
	if ca, cb := relevant(a.Categories), relevant(b.Categories); !slices.Equal(ca, cb) {
		p = append(p, fmt.Sprintf("categories %v vs %v", ca, cb))
	}
	return p
}

func href(it Item) string {
	if len(it.Canonical) == 0 {
		return ""
	}
	return it.Canonical[0].Href
}

func diff(a, b []string) (missing, extra []string) {
	inB := map[string]bool{}
	for _, x := range b {
		inB[x] = true
	}
	inA := map[string]bool{}
	for _, x := range a {
		inA[x] = true
		if !inB[x] {
			missing = append(missing, x)
		}
	}
	for _, x := range b {
		if !inA[x] {
			extra = append(extra, x)
		}
	}
	return
}

func head(s []string) []string { return s[:min(len(s), 5)] }

// RollbackPlan is the net state change to apply to FreshRSS.
type RollbackPlan struct {
	Read, Unread, Star, Unstar []string
	Skipped                    int // entries that only exist in Correspondent
}

// PlanRollback collapses the mutation journal since the cutover into the
// final read and star state of each entry that FreshRSS also has (IDs at or
// below maxLegacyID).
func PlanRollback(ctx context.Context, st *store.Store, userID int64, since time.Time, maxLegacyID int64) (*RollbackPlan, error) {
	journal, err := st.Journal(ctx, userID, since)
	if err != nil {
		return nil, err
	}
	read := map[int64]bool{}
	star := map[int64]bool{}
	plan := &RollbackPlan{}
	skipped := map[int64]bool{}
	for _, j := range journal {
		if j.EntryID > maxLegacyID {
			skipped[j.EntryID] = true
			continue
		}
		switch j.Action {
		case "read":
			read[j.EntryID] = true
		case "unread":
			read[j.EntryID] = false
		case "star":
			star[j.EntryID] = true
		case "unstar":
			star[j.EntryID] = false
		}
	}
	plan.Skipped = len(skipped)
	for _, id := range slices.Sorted(maps.Keys(read)) {
		if read[id] {
			plan.Read = append(plan.Read, fmt.Sprint(id))
		} else {
			plan.Unread = append(plan.Unread, fmt.Sprint(id))
		}
	}
	for _, id := range slices.Sorted(maps.Keys(star)) {
		if star[id] {
			plan.Star = append(plan.Star, fmt.Sprint(id))
		} else {
			plan.Unstar = append(plan.Unstar, fmt.Sprint(id))
		}
	}
	return plan, nil
}

// ApplyRollback sends the plan to FreshRSS. Each edit is idempotent, so a
// failed run can simply be repeated.
func ApplyRollback(ctx context.Context, freshrss *ReaderClient, p *RollbackPlan) error {
	steps := []struct {
		ids         []string
		add, remove string
	}{
		{p.Read, streamRead, ""},
		{p.Unread, "", streamRead},
		{p.Star, streamStarred, ""},
		{p.Unstar, "", streamStarred},
	}
	for _, s := range steps {
		if len(s.ids) == 0 {
			continue
		}
		if err := freshrss.EditTag(ctx, s.ids, s.add, s.remove); err != nil {
			return err
		}
	}
	return nil
}

// redactURL shows where a URL points without its query or fragment, which
// may hold access tokens (paid feeds often put one in every link).
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(unparseable URL)"
	}
	out := u.Scheme + "://" + u.Host + u.Path
	if u.RawQuery != "" || u.Fragment != "" {
		out += fmt.Sprintf("?… (%d-character query)", len(u.RawQuery)+len(u.Fragment))
	}
	return out
}

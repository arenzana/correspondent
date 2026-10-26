package pagefeed

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"strings"
)

// Built-in entries for popular sites, mostly AI labs: feeds that exist but
// aren't advertised (or sit behind pages that refuse feed readers), and
// parsers for sites with no feed at all. They are shared like any parser,
// so nobody spends an AI request on them.
//
//go:embed builtin.json
var builtinJSON []byte

type Builtin struct {
	Name string `json:"name"`
	// Match lists the addresses (host and path, without scheme or "www.")
	// a reader might type for this entry.
	Match []string `json:"match"`
	// Either Feed, the feed to follow, or Page and Rules, a parser for it.
	Feed  string `json:"feed,omitempty"`
	Page  string `json:"page,omitempty"`
	Rules *Rules `json:"rules,omitempty"`
}

var builtins = func() []Builtin {
	var b []Builtin
	if err := json.Unmarshal(builtinJSON, &b); err != nil {
		panic("pagefeed: builtin.json: " + err.Error())
	}
	return b
}()

// Builtins returns the built-in entries.
func Builtins() []Builtin { return builtins }

// LookupBuiltin returns the entry for an address a reader typed, if any.
func LookupBuiltin(raw string) *Builtin {
	key := matchKey(raw)
	for i, b := range builtins {
		for _, m := range b.Match {
			if m == key {
				return &builtins[i]
			}
		}
	}
	return nil
}

// matchKey reduces an address to host and path: "https://www.Anthropic.com/news/"
// → "anthropic.com/news".
func matchKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") + strings.TrimSuffix(u.Path, "/")
}

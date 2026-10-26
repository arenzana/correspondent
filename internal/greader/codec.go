// Package greader is the Google Reader–compatible facade used by Reeder and
// other clients, mounted at the FreshRSS path /api/greader.php. It adapts
// legacy wire formats onto the store; nothing else depends on it.
package greader

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	itemPrefix  = "tag:google.com,2005:reader/item/"
	stateRead   = "user/-/state/com.google/read"
	stateStar   = "user/-/state/com.google/starred"
	stateList   = "user/-/state/com.google/reading-list"
	labelPrefix = "user/-/label/"
	feedPrefix  = "feed/"
)

// ParseItemID accepts the three forms clients send: the long tag form, bare
// zero-padded hexadecimal (Reeder), and bare decimal. Following FreshRSS, a
// value is decimal only if it is all digits and does not start with '0'.
func ParseItemID(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, itemPrefix)
	if s == "" {
		return 0, errors.New("empty item id")
	}
	if isDigits(s) && s[0] != '0' {
		return strconv.ParseInt(s, 10, 64)
	}
	u, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid item id %q", s)
	}
	return int64(u), nil
}

// LongID formats an ID as the long tag form with 16 hex digits.
func LongID(id int64) string {
	return fmt.Sprintf("%s%016x", itemPrefix, uint64(id))
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// normalizeStream rewrites "user/<id>/..." to "user/-/..." so streams can be
// compared regardless of which user form a client uses.
func normalizeStream(s string) string {
	if strings.HasPrefix(s, "user/") {
		rest := s[len("user/"):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			return "user/-/" + rest[i+1:]
		}
	}
	return s
}

// microseconds converts a client timestamp in seconds, milliseconds or
// microseconds to microseconds, since clients disagree on the unit.
func microseconds(v int64) int64 {
	switch {
	case v <= 0:
		return 0
	case v < 100_000_000_000: // seconds until year 5138
		return v * 1_000_000
	case v < 100_000_000_000_000:
		return v * 1_000
	default:
		return v
	}
}

// UnicodeAlternative reproduces FreshRSS's escapeToUnicodeAlternative(),
// which its Reader API applies to titles and authors (extended=false) and to
// feed names (extended=true): characters that trip up Reader clients become
// their fullwidth look-alikes. Reeder has cached FreshRSS's output, so we
// match it exactly, including a quirk: FreshRSS builds the extended list with
// PHP's array union (+=), which keeps keys 0–2 of the base list, so of the
// extended characters only ? \ / , ; are actually replaced.
func UnicodeAlternative(s string, extended bool) string {
	r := unicodeAlt
	if extended {
		r = unicodeAltExtended
	}
	return strings.Trim(r.Replace(s), " \t\n\r\x00\x0b") // PHP trim(): ASCII whitespace only
}

var (
	unicodeAlt         = strings.NewReplacer("&", "＆", "<", "＜", ">", "＞")
	unicodeAltExtended = strings.NewReplacer("&", "＆", "<", "＜", ">", "＞",
		"?", "？", `\`, "＼", "/", "／", ",", "，", ";", "；")
)

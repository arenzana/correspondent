package store

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"
)

// Parser reads one web page's article list; see migration 0008.
type Parser struct {
	ID        int64
	URL       string
	Rules     string
	Method    string
	CreatedBy int64 // 0 when the author's account is gone
	UpdatedAt int64
	Builtin   bool // shipped with the app
}

// ParserKey normalizes a page address so readers who type it differently
// share one parser: lower-case scheme and host, no "www.", fragment or
// trailing slash. The query is kept; it can select a different list.
func ParserKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	return strings.ToLower(u.Scheme) + "://" + host + strings.TrimSuffix(u.EscapedPath(), "/") + func() string {
		if u.RawQuery != "" {
			return "?" + u.RawQuery
		}
		return ""
	}()
}

// ParserFor returns the parser for a page, if one exists.
func (s *Store) ParserFor(ctx context.Context, pageURL string) (*Parser, error) {
	return s.parser(ctx, `WHERE url = ?`, ParserKey(pageURL))
}

// Parser returns a parser by ID.
func (s *Store) Parser(ctx context.Context, id int64) (*Parser, error) {
	return s.parser(ctx, `WHERE id = ?`, id)
}

func (s *Store) parser(ctx context.Context, where string, arg any) (*Parser, error) {
	var p Parser
	var by sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, url, rules, method, created_by, updated_at, builtin FROM parsers `+where, arg).
		Scan(&p.ID, &p.URL, &p.Rules, &p.Method, &by, &p.UpdatedAt, &p.Builtin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	p.CreatedBy = by.Int64
	return &p, err
}

// ParserUsedByOthers reports whether readers other than userID follow a
// page through this parser.
func (s *Store) ParserUsedByOthers(ctx context.Context, parserID, userID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM feeds WHERE parser_id = ? AND user_id <> ?`, parserID, userID).Scan(&n)
	return n > 0, err
}

// SaveParser stores the parser for a page, replacing the old one (which no
// longer read the page, or the caller wouldn't have written a new one).
// Every feed of that page, for every reader, uses the new rules.
func (s *Store) SaveParser(ctx context.Context, pageURL, rules, method string, userID int64) (int64, error) {
	now := time.Now().Unix()
	var by any
	if userID != 0 {
		by = userID
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO parsers (url, rules, method, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (url) DO UPDATE SET rules = excluded.rules, method = excluded.method,
			created_by = excluded.created_by, updated_at = excluded.updated_at, builtin = 0
		RETURNING id`, ParserKey(pageURL), rules, method, by, now, now).Scan(&id)
	return id, err
}

// SeedParser installs a parser shipped with the app. A newer release's rules
// replace an older built-in, but never a parser a reader rebuilt.
func (s *Store) SeedParser(ctx context.Context, pageURL, rules, method string) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO parsers (url, rules, method, created_at, updated_at, builtin) VALUES (?, ?, ?, ?, ?, 1)
		ON CONFLICT (url) DO UPDATE SET rules = excluded.rules, method = excluded.method, updated_at = excluded.updated_at
		WHERE parsers.builtin = 1 AND parsers.rules <> excluded.rules`, ParserKey(pageURL), rules, method, now, now)
	return err
}

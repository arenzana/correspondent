package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// Token scopes.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

// TokenPrefix marks Correspondent tokens, so they're recognisable in a
// secret scanner or a leaked log.
const TokenPrefix = "corr_"

// APIToken is a personal token as its owner sees it (never the secret).
type APIToken struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	CreatedAt  int64  `json:"createdAt"`
	LastUsedAt int64  `json:"lastUsedAt"`
}

// CreateToken issues a personal API token; it is returned once.
func (s *Store) CreateToken(ctx context.Context, userID int64, name, scope string) (string, APIToken, error) {
	if scope != ScopeRead && scope != ScopeWrite {
		return "", APIToken{}, errors.New("scope must be read or write")
	}
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 60 {
		name = string(r[:60])
	}
	if name == "" {
		name = "Token"
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", APIToken{}, err
	}
	token := TokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `INSERT INTO api_tokens (user_id, name, token_hash, scope, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, name, hashToken(token), scope, now)
	if err != nil {
		return "", APIToken{}, err
	}
	id, _ := res.LastInsertId()
	return token, APIToken{ID: id, Name: name, Scope: scope, CreatedAt: now}, nil
}

// TokenUser returns the enabled account and scope a token belongs to, and
// notes its use (at most every few minutes).
func (s *Store) TokenUser(ctx context.Context, token string) (userID int64, scope string, err error) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return 0, "", ErrNotFound
	}
	var id, used int64
	h := hashToken(token)
	err = s.db.QueryRowContext(ctx, `SELECT t.id, t.user_id, t.scope, t.last_used_at FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND u.disabled_at IS NULL`, h).Scan(&id, &userID, &scope, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrNotFound
	} else if err != nil {
		return 0, "", err
	}
	if now := time.Now().Unix(); now-used > 300 {
		s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, now, id)
		s.db.ExecContext(ctx, `UPDATE users SET last_seen_at = ? WHERE id = ?`, now, userID)
	}
	return userID, scope, nil
}

// Tokens lists a user's tokens, newest first.
func (s *Store) Tokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, scope, created_at, last_used_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.Name, &t.Scope, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken deletes a token; it stops working at once.
func (s *Store) RevokeToken(ctx context.Context, userID, id int64) error {
	return checkAffected(s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID))
}

// RevokeTokenBySecret deletes the token presented (corr auth logout).
func (s *Store) RevokeTokenBySecret(ctx context.Context, token string) error {
	return checkAffected(s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE token_hash = ?`, hashToken(token)))
}

package store

import (
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Setting keys.
const (
	SettingFreshRSSSalt = "freshrss.salt"
	SettingMigratedAt   = "freshrss.migrated_at" // RFC 3339; the rollback replay boundary
	SettingMaxLegacyID  = "freshrss.max_id"      // highest imported entry ID; newer ones exist only here
)

func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ImportUser creates or updates an account with existing bcrypt hashes from
// FreshRSS (PHP's $2y$ format verifies in Go), so both passwords keep working.
func (s *Store) ImportUser(ctx context.Context, username, webHash, apiHash string) (int64, error) {
	var api any
	if apiHash != "" {
		api = apiHash
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, api_hash, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (username) DO UPDATE SET password_hash = excluded.password_hash, api_hash = excluded.api_hash`,
		username, webHash, api, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return s.UserIDByName(ctx, username)
}

// SetLegacyAuth turns acceptance of the FreshRSS-issued Reader token on or off.
func (s *Store) SetLegacyAuth(ctx context.Context, userID int64, on bool) error {
	v := 0
	if on {
		v = 1
	}
	return checkAffected(s.db.ExecContext(ctx, `UPDATE users SET legacy_auth = ? WHERE id = ?`, v, userID))
}

// LegacyToken is FreshRSS's Reader token for a user: sha1(salt + user + api
// hash), hex-encoded. It is empty when legacy auth is off or unavailable.
func (s *Store) LegacyToken(ctx context.Context, username string) (userID int64, token string, err error) {
	salt, err := s.Setting(ctx, SettingFreshRSSSalt)
	if errors.Is(err, ErrNotFound) {
		return 0, "", nil
	} else if err != nil {
		return 0, "", err
	}
	var (
		apiHash sql.NullString
		on      bool
	)
	err = s.db.QueryRowContext(ctx, `SELECT id, api_hash, legacy_auth FROM users WHERE username = ?`, username).
		Scan(&userID, &apiHash, &on)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	if err != nil || !on || !apiHash.Valid || salt == "" {
		return userID, "", err
	}
	sum := sha1.Sum([]byte(salt + username + apiHash.String))
	return userID, hex.EncodeToString(sum[:]), nil
}

// EqualTokens compares tokens in constant time.
func EqualTokens(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// JournalEntry is one recorded user mutation.
type JournalEntry struct {
	Seq     int64
	At      int64
	Action  string
	EntryID int64
}

// Journal returns a user's mutations recorded at or after since, in order.
func (s *Store) Journal(ctx context.Context, userID int64, since time.Time) ([]JournalEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, at, action, entry_id FROM journal WHERE user_id = ? AND at >= ? ORDER BY seq`, userID, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalEntry
	for rows.Next() {
		var j JournalEntry
		if err := rows.Scan(&j.Seq, &j.At, &j.Action, &j.EntryID); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// IsBcrypt reports whether h looks like a bcrypt hash, including PHP's $2y$.
func IsBcrypt(h string) bool {
	return strings.HasPrefix(h, "$2y$") || strings.HasPrefix(h, "$2a$") || strings.HasPrefix(h, "$2b$")
}

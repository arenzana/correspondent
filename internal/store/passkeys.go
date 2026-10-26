package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Passkey is a reader's passkey as listed in Settings; the credential
// itself (public key, counters, flags) stays in Credential, for WebAuthn.
type Passkey struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"createdAt"`
	LastUsedAt int64  `json:"lastUsedAt"`
	Credential []byte `json:"-"` // webauthn.Credential, JSON
}

// WebAuthnHandle returns the random handle a user's passkeys are bound to,
// creating it on first use.
func (s *Store) WebAuthnHandle(ctx context.Context, userID int64) ([]byte, error) {
	var h []byte
	if err := s.db.QueryRowContext(ctx, `SELECT webauthn_handle FROM users WHERE id = ?`, userID).Scan(&h); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if len(h) > 0 {
		return h, nil
	}
	h = make([]byte, 32)
	rand.Read(h)
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET webauthn_handle = ? WHERE id = ? AND webauthn_handle IS NULL`, h, userID); err != nil {
		return nil, err
	}
	err := s.db.QueryRowContext(ctx, `SELECT webauthn_handle FROM users WHERE id = ?`, userID).Scan(&h)
	return h, err
}

// UserByWebAuthnHandle finds the enabled account a passkey belongs to.
func (s *Store) UserByWebAuthnHandle(ctx context.Context, handle []byte) (*User, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE webauthn_handle = ? AND disabled_at IS NULL`, handle).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return s.UserByID(ctx, id)
}

// Passkeys lists a user's passkeys, newest first.
func (s *Store) Passkeys(ctx context.Context, userID int64) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at, last_used_at, credential FROM passkeys WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		var p Passkey
		var cred string
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.LastUsedAt, &cred); err != nil {
			return nil, err
		}
		p.Credential = []byte(cred)
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddPasskey stores a newly registered passkey.
func (s *Store) AddPasskey(ctx context.Context, userID int64, credentialID, credential []byte, name string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO passkeys (user_id, credential_id, credential, name, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, credentialID, string(credential), passkeyName(name), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UsePasskey records a sign-in with a passkey (its updated counters).
func (s *Store) UsePasskey(ctx context.Context, userID int64, credentialID, credential []byte) error {
	return checkAffected(s.db.ExecContext(ctx, `UPDATE passkeys SET credential = ?, last_used_at = ? WHERE credential_id = ? AND user_id = ?`,
		string(credential), time.Now().Unix(), credentialID, userID))
}

// RenamePasskey changes a passkey's name.
func (s *Store) RenamePasskey(ctx context.Context, userID, id int64, name string) error {
	return checkAffected(s.db.ExecContext(ctx, `UPDATE passkeys SET name = ? WHERE id = ? AND user_id = ?`, passkeyName(name), id, userID))
}

// DeletePasskey removes a passkey; it can't sign in again.
func (s *Store) DeletePasskey(ctx context.Context, userID, id int64) error {
	return checkAffected(s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ? AND user_id = ?`, id, userID))
}

func passkeyName(name string) string {
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 60 {
		name = string(r[:60])
	}
	if name == "" {
		name = "Passkey"
	}
	return name
}

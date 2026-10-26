package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       int64
	Username string
	HasAPI   bool
	Prefs    string // opaque JSON owned by the UI
}

func (s *Store) CreateUser(ctx context.Context, username, password string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		username, string(hash), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetAPIPassword sets the separate password used by Reeder and other Google
// Reader clients. The web password never authenticates the API.
func (s *Store) SetAPIPassword(ctx context.Context, userID int64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET api_hash = ? WHERE id = ?`, string(hash), userID)
	return err
}

// dummyHash keeps failed logins for unknown users as slow as for known ones.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("correspondent"), bcrypt.DefaultCost)

// CheckPassword verifies a web (api=false) or API (api=true) password.
func (s *Store) CheckPassword(ctx context.Context, username, password string, api bool) (*User, error) {
	var (
		u       User
		webHash string
		apiHash sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, api_hash, prefs FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &webHash, &apiHash, &u.Prefs)
	if errors.Is(err, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	hash := webHash
	if api {
		if !apiHash.Valid {
			bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
			return nil, ErrNotFound
		}
		hash = apiHash.String
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrNotFound
	}
	u.HasAPI = apiHash.Valid
	return &u, nil
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	var (
		u       User
		apiHash sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, username, api_hash, prefs FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &apiHash, &u.Prefs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	u.HasAPI = apiHash.Valid
	return &u, err
}

func (s *Store) UserIDByName(ctx context.Context, username string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, username).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (s *Store) SetPrefs(ctx context.Context, userID int64, prefs string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET prefs = ? WHERE id = ?`, prefs, userID)
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession issues an opaque random token; only its hash is stored.
func (s *Store) CreateSession(ctx context.Context, userID int64, kind string, ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, kind, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		hashToken(token), userID, kind, now.Unix(), now.Add(ttl).Unix())
	return token, err
}

func (s *Store) SessionUser(ctx context.Context, token, kind string) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id FROM sessions WHERE token_hash = ? AND kind = ? AND expires_at > ?`,
		hashToken(token), kind, time.Now().Unix()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return userID, err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

// PurgeSessions deletes expired sessions.
func (s *Store) PurgeSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

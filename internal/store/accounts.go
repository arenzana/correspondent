package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// ErrLastAdmin protects the server from losing its last administrator.
var ErrLastAdmin = errors.New("there must be at least one active admin")

// ErrUsernameTaken is returned when an account name is in use.
var ErrUsernameTaken = errors.New("that username is taken")

// Account is a user as the admin sees it.
type Account struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	Disabled   bool   `json:"disabled"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	Feeds      int    `json:"feeds"`
	Articles   int    `json:"articles"`
	Passkeys   int    `json:"passkeys"`
	HasAPI     bool   `json:"hasApiPassword"`
	HasAI      bool   `json:"hasAI"`
}

// ValidUsername checks a new account name: 2-40 letters, digits, dots,
// dashes and underscores, starting with a letter or digit.
func ValidUsername(name string) error {
	if len(name) < 2 || len(name) > 40 {
		return errors.New("usernames are 2 to 40 characters")
	}
	for i, r := range name {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r) || (i > 0 && strings.ContainsRune("._-", r))
		if !ok || r > unicode.MaxASCII && !unicode.IsLetter(r) {
			return errors.New("usernames use letters, digits, dots, dashes and underscores")
		}
	}
	return nil
}

// Accounts lists every user with what they use.
func (s *Store) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.role, u.disabled_at IS NOT NULL, u.created_at, u.last_seen_at,
		       (SELECT COUNT(*) FROM feeds f WHERE f.user_id = u.id),
		       (SELECT COUNT(*) FROM entries e JOIN feeds f ON f.id = e.feed_id WHERE f.user_id = u.id),
		       (SELECT COUNT(*) FROM passkeys p WHERE p.user_id = u.id),
		       u.api_hash IS NOT NULL, u.ai_key IS NOT NULL
		  FROM users u ORDER BY u.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Username, &a.Role, &a.Disabled, &a.CreatedAt, &a.LastSeenAt,
			&a.Feeds, &a.Articles, &a.Passkeys, &a.HasAPI, &a.HasAI); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// activeAdmins counts enabled admins other than except (0 counts all).
func activeAdmins(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, except int64) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL AND id <> ?`, except).Scan(&n)
	return n, err
}

// SetRole makes an account an admin or a user. The last active admin stays
// an admin.
func (s *Store) SetRole(ctx context.Context, userID int64, role string) error {
	if role != RoleAdmin && role != RoleUser {
		return errors.New("unknown role")
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if role == RoleUser {
			if n, err := activeAdmins(ctx, tx, userID); err != nil {
				return err
			} else if n == 0 {
				return ErrLastAdmin
			}
		}
		return checkAffected(tx.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, role, userID))
	})
}

// SetDisabled disables or enables an account. Disabling ends its sessions
// (web and apps) at once; its data is kept.
func (s *Store) SetDisabled(ctx context.Context, userID int64, disabled bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if disabled {
			if n, err := activeAdmins(ctx, tx, userID); err != nil {
				return err
			} else if n == 0 {
				var role string
				tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, userID).Scan(&role)
				if role == RoleAdmin {
					return ErrLastAdmin
				}
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
				return err
			}
			return checkAffected(tx.ExecContext(ctx, `UPDATE users SET disabled_at = ? WHERE id = ?`, time.Now().Unix(), userID))
		}
		return checkAffected(tx.ExecContext(ctx, `UPDATE users SET disabled_at = NULL WHERE id = ?`, userID))
	})
}

// DeleteAccount removes a user and everything that is theirs: feeds,
// articles, folders, sessions, passkeys, push subscriptions and history.
// Shared parsers they wrote stay, unattributed.
func (s *Store) DeleteAccount(ctx context.Context, userID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var role string
		if err := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, userID).Scan(&role); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if role == RoleAdmin {
			if n, err := activeAdmins(ctx, tx, userID); err != nil {
				return err
			} else if n == 0 {
				return ErrLastAdmin
			}
		}
		// The journal has no foreign key (it outlives entries by design).
		if _, err := tx.ExecContext(ctx, `DELETE FROM journal WHERE user_id = ?`, userID); err != nil {
			return err
		}
		return checkAffected(tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID))
	})
}

// SetPassword sets a user's web password and ends their other web sessions
// (keep is the session token to keep, "" for none).
func (s *Store) SetPassword(ctx context.Context, userID int64, password, keep string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkAffected(tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), userID)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND kind = 'web' AND token_hash <> ?`, userID, hashToken(keep))
		return err
	})
}

// CheckUserPassword verifies a user's current web password.
func (s *Store) CheckUserPassword(ctx context.Context, userID int64, password string) bool {
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// --- sessions ------------------------------------------------------------

// Session is one signed-in device, as its owner sees it.
type Session struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"` // web | greader
	Client     string `json:"client"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	Current    bool   `json:"current"`
}

// Sessions lists a user's current sessions; current is the caller's token.
func (s *Store) Sessions(ctx context.Context, userID int64, current string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT rowid, kind, client, created_at, last_seen_at, token_hash = ? FROM sessions
		 WHERE user_id = ? AND expires_at > ? ORDER BY last_seen_at DESC`, hashToken(current), userID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var x Session
		if err := rows.Scan(&x.ID, &x.Kind, &x.Client, &x.CreatedAt, &x.LastSeenAt, &x.Current); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// EndSession signs one of a user's sessions out.
func (s *Store) EndSession(ctx context.Context, userID, id int64) error {
	return checkAffected(s.db.ExecContext(ctx, `DELETE FROM sessions WHERE rowid = ? AND user_id = ?`, id, userID))
}

// --- invitations and password resets -------------------------------------

// Invite kinds.
const (
	InviteAccount = "invite"
	InviteReset   = "reset"
)

// Invite is a one-time link.
type Invite struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	UserID    int64  `json:"userId,omitempty"`   // reset: whose password
	Username  string `json:"username,omitempty"` // reset: their name
	Role      string `json:"role"`
	Note      string `json:"note"`
	CreatedBy string `json:"createdBy"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
}

// CreateInvite issues a one-time token for a new account (kind invite) or
// a user's password reset (kind reset); only its hash is stored.
func (s *Store) CreateInvite(ctx context.Context, kind string, userID int64, role, note string, by int64, ttl time.Duration) (string, error) {
	if kind != InviteAccount && kind != InviteReset {
		return "", errors.New("unknown invite kind")
	}
	if role != RoleAdmin && role != RoleUser {
		return "", errors.New("unknown role")
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := time.Now()
	var forUser, creator any
	if by != 0 {
		creator = by // 0: made from the command line, by no account
	}
	if kind == InviteReset {
		forUser = userID
		// A new reset link replaces any earlier one for that account.
		if _, err := s.db.ExecContext(ctx, `DELETE FROM invites WHERE kind = 'reset' AND user_id = ? AND used_at IS NULL`, userID); err != nil {
			return "", err
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO invites (token_hash, kind, user_id, role, note, created_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, hashToken(token), kind, forUser, role, strings.TrimSpace(note), creator, now.Unix(), now.Add(ttl).Unix())
	return token, err
}

const inviteColumns = `i.rowid, i.kind, COALESCE(i.user_id, 0), COALESCE(t.username, ''), i.role, i.note,
	COALESCE(c.username, ''), i.created_at, i.expires_at`

func scanInvite(sc interface{ Scan(...any) error }) (Invite, error) {
	var x Invite
	err := sc.Scan(&x.ID, &x.Kind, &x.UserID, &x.Username, &x.Role, &x.Note, &x.CreatedBy, &x.CreatedAt, &x.ExpiresAt)
	return x, err
}

// PendingInvite returns a link's details if it is unused and unexpired.
func (s *Store) PendingInvite(ctx context.Context, token string) (*Invite, error) {
	x, err := scanInvite(s.db.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM invites i
		LEFT JOIN users t ON t.id = i.user_id LEFT JOIN users c ON c.id = i.created_by
		WHERE i.token_hash = ? AND i.used_at IS NULL AND i.expires_at > ?`, hashToken(token), time.Now().Unix()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

// PendingInvites lists unused, unexpired links, for the admin.
func (s *Store) PendingInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inviteColumns+` FROM invites i
		LEFT JOIN users t ON t.id = i.user_id LEFT JOIN users c ON c.id = i.created_by
		WHERE i.used_at IS NULL AND i.expires_at > ? ORDER BY i.created_at DESC`, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		x, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// RevokeInvite cancels an unused link.
func (s *Store) RevokeInvite(ctx context.Context, id int64) error {
	return checkAffected(s.db.ExecContext(ctx, `DELETE FROM invites WHERE rowid = ? AND used_at IS NULL`, id))
}

// AcceptInvite uses a link once: an invitation creates the account (with
// username and password), a reset sets the account's password and ends
// its web sessions. It returns the account's ID.
func (s *Store) AcceptInvite(ctx context.Context, token, username, password string) (int64, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var kind, role string
		var forUser sql.NullInt64
		th := hashToken(token)
		err := tx.QueryRowContext(ctx, `SELECT kind, user_id, role FROM invites WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
			th, time.Now().Unix()).Scan(&kind, &forUser, &role)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		now := time.Now().Unix()
		if kind == InviteReset {
			id = forUser.Int64
			if err := checkAffected(tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ? AND disabled_at IS NULL`, string(hash), id)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND kind = 'web'`, id); err != nil {
				return err
			}
		} else {
			if err := ValidUsername(username); err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash, created_at, role) VALUES (?, ?, ?, ?)`,
				username, string(hash), now, role)
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					return ErrUsernameTaken
				}
				return err
			}
			if id, err = res.LastInsertId(); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE invites SET used_at = ?, used_by = ? WHERE token_hash = ?`, now, id, th)
		return err
	})
	return id, err
}

// tx runs fn in a transaction.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

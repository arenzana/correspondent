package store

import (
	"context"
	"time"
)

// PushSubscription is one device registered for Web Push.
type PushSubscription struct {
	ID       int64
	UserID   int64
	Endpoint string
	P256dh   string
	Auth     string
}

// SavePushSubscription registers (or re-registers) a device for a user. An
// endpoint belongs to one user: re-subscribing from another account moves it.
func (s *Store) SavePushSubscription(ctx context.Context, userID int64, endpoint, p256dh, auth, userAgent string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET user_id = excluded.user_id, p256dh = excluded.p256dh,
		       auth = excluded.auth, user_agent = excluded.user_agent, failures = 0`,
		userID, endpoint, p256dh, auth, userAgent, time.Now().Unix())
	return err
}

// DeletePushSubscription removes a device; only its owner can.
func (s *Store) DeletePushSubscription(ctx context.Context, userID int64, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ?`, userID, endpoint)
	return err
}

// ForgetPushEndpoint removes a subscription the push service says is gone.
func (s *Store) ForgetPushEndpoint(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

// PushFailed counts a failed delivery; a device failing repeatedly is dropped.
func (s *Store) PushFailed(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE push_subscriptions SET failures = failures + 1 WHERE endpoint = ?`, endpoint)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ? AND failures >= 10`, endpoint)
	return err
}

func (s *Store) PushSubscriptions(ctx context.Context, userID int64) ([]PushSubscription, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, endpoint, p256dh, auth FROM push_subscriptions WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSubscription
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256dh, &p.Auth); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// HasPushSubscription reports whether an endpoint is registered for the user.
func (s *Store) HasPushSubscription(ctx context.Context, userID int64, endpoint string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ? AND endpoint = ?`, userID, endpoint).Scan(&n)
	return n > 0, err
}

// SetFeedNotify opts a feed in or out of notifications.
func (s *Store) SetFeedNotify(ctx context.Context, userID, feedID int64, on bool) error {
	return checkAffected(s.db.ExecContext(ctx, `UPDATE feeds SET notify = ? WHERE id = ? AND user_id = ?`, on, feedID, userID))
}

// NewUnread lists unread entries in a feed with IDs above after, newest first.
func (s *Store) NewUnread(ctx context.Context, feedID, after int64, limit int) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+entryColumns+` FROM entries e
		WHERE e.feed_id = ? AND e.id > ? AND e.read_at IS NULL ORDER BY e.id DESC LIMIT ?`, feedID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := scanEntry(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

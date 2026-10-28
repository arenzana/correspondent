package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Integration is a service a reader sends articles to, as its owner sees
// it: never the sealed credentials, which only Secret returns.
type Integration struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Config      string `json:"-"` // JSON; the API decodes it per kind
	SendStarred bool   `json:"sendStarred"`
	CreatedAt   int64  `json:"createdAt"`
	LastOKAt    int64  `json:"lastOkAt"`
	LastError   string `json:"lastError"`
}

const integrationColumns = `id, kind, name, config, send_starred, created_at, last_ok_at, last_error`

func scanIntegration(row interface{ Scan(...any) error }) (Integration, error) {
	var i Integration
	err := row.Scan(&i.ID, &i.Kind, &i.Name, &i.Config, &i.SendStarred, &i.CreatedAt, &i.LastOKAt, &i.LastError)
	return i, err
}

// Integrations lists a reader's integrations, oldest first.
func (s *Store) Integrations(ctx context.Context, userID int64) ([]Integration, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+integrationColumns+` FROM integrations WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Integration
	for rows.Next() {
		i, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// Integration returns one of a reader's integrations.
func (s *Store) Integration(ctx context.Context, userID, id int64) (*Integration, error) {
	i, err := scanIntegration(s.db.QueryRowContext(ctx,
		`SELECT `+integrationColumns+` FROM integrations WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &i, err
}

// IntegrationSecret returns an integration's sealed credentials.
func (s *Store) IntegrationSecret(ctx context.Context, userID, id int64) ([]byte, error) {
	var sealed []byte
	err := s.db.QueryRowContext(ctx, `SELECT secret FROM integrations WHERE id = ? AND user_id = ?`, id, userID).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sealed, err
}

func integrationName(name string) string {
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 60 {
		name = string(r[:60])
	}
	return name
}

// CreateIntegration stores a new integration.
func (s *Store) CreateIntegration(ctx context.Context, userID int64, kind, name, config string, sealed []byte, sendStarred bool) (Integration, error) {
	now := time.Now().Unix()
	name = integrationName(name)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO integrations (user_id, kind, name, config, secret, send_starred, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, kind, name, config, sealed, sendStarred, now)
	if err != nil {
		return Integration{}, err
	}
	id, _ := res.LastInsertId()
	return Integration{ID: id, Kind: kind, Name: name, Config: config, SendStarred: sendStarred, CreatedAt: now}, nil
}

// UpdateIntegration replaces an integration's name, settings and rule, and
// its credentials when sealed isn't nil.
func (s *Store) UpdateIntegration(ctx context.Context, userID, id int64, name, config string, sealed []byte, sendStarred bool) error {
	if sealed != nil {
		return checkAffected(s.db.ExecContext(ctx,
			`UPDATE integrations SET name = ?, config = ?, secret = ?, send_starred = ?, last_error = '' WHERE id = ? AND user_id = ?`,
			integrationName(name), config, sealed, sendStarred, id, userID))
	}
	return checkAffected(s.db.ExecContext(ctx,
		`UPDATE integrations SET name = ?, config = ?, send_starred = ? WHERE id = ? AND user_id = ?`,
		integrationName(name), config, sendStarred, id, userID))
}

// DeleteIntegration removes an integration and its deliveries.
func (s *Store) DeleteIntegration(ctx context.Context, userID, id int64) error {
	return checkAffected(s.db.ExecContext(ctx, `DELETE FROM integrations WHERE id = ? AND user_id = ?`, id, userID))
}

// IntegrationResult notes how the last delivery went: an error the reader
// should see in Settings ("key rejected"), or "" when it worked.
func (s *Store) IntegrationResult(ctx context.Context, id int64, errText string) error {
	if errText == "" {
		_, err := s.db.ExecContext(ctx, `UPDATE integrations SET last_ok_at = ?, last_error = '' WHERE id = ?`, time.Now().Unix(), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE integrations SET last_error = ? WHERE id = ?`, errText, id)
	return err
}

// Delivery is one article sent, or being sent, to one integration.
type Delivery struct {
	ID            int64  `json:"id"`
	IntegrationID int64  `json:"integrationId"`
	EntryID       int64  `json:"-"`
	UserID        int64  `json:"-"`
	State         string `json:"state"` // pending, sent or failed
	Attempts      int    `json:"attempts"`
	Error         string `json:"error,omitempty"`
	Remote        string `json:"remote,omitempty"`
	SentAt        int64  `json:"sentAt,omitempty"`
}

// Delivery states.
const (
	DeliveryPending = "pending"
	DeliverySent    = "sent"
	DeliveryFailed  = "failed"
)

// QueueDelivery asks for an entry to be sent to an integration, both the
// reader's. Asking again for one that failed or was sent queues it anew;
// one already waiting stays as it is.
func (s *Store) QueueDelivery(ctx context.Context, userID, integrationID, entryID int64) (Delivery, error) {
	var ok int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM integrations i, entries e JOIN feeds f ON f.id = e.feed_id
		WHERE i.id = ? AND i.user_id = ? AND e.id = ? AND f.user_id = ?`, integrationID, userID, entryID, userID).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrNotFound
	} else if err != nil {
		return Delivery{}, err
	}
	now := time.Now().Unix()
	d := Delivery{IntegrationID: integrationID, EntryID: entryID, UserID: userID}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO deliveries (integration_id, entry_id, state, next_at, created_at) VALUES (?, ?, 'pending', ?, ?)
		ON CONFLICT (integration_id, entry_id) DO UPDATE SET
			state = CASE WHEN state = 'pending' THEN state ELSE 'pending' END,
			attempts = CASE WHEN state = 'pending' THEN attempts ELSE 0 END,
			next_at = CASE WHEN state = 'pending' THEN next_at ELSE excluded.next_at END,
			error = CASE WHEN state = 'pending' THEN error ELSE '' END
		RETURNING id, state, attempts, error, remote, sent_at`, integrationID, entryID, now, now).
		Scan(&d.ID, &d.State, &d.Attempts, &d.Error, &d.Remote, &d.SentAt)
	return d, err
}

// QueueStarred queues the entries a reader just starred for every
// integration of theirs with the send-starred rule. Entries already sent
// there aren't sent again.
func (s *Store) QueueStarred(ctx context.Context, userID int64, entryIDs []int64) (int, error) {
	if len(entryIDs) == 0 {
		return 0, nil
	}
	now := time.Now().Unix()
	n := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		for _, id := range entryIDs {
			res, err := tx.ExecContext(ctx, `
				INSERT INTO deliveries (integration_id, entry_id, state, next_at, created_at)
				SELECT i.id, e.id, 'pending', ?, ? FROM integrations i, entries e JOIN feeds f ON f.id = e.feed_id
				 WHERE i.user_id = ? AND i.send_starred = 1 AND e.id = ? AND f.user_id = ?
				ON CONFLICT (integration_id, entry_id) DO NOTHING`, now, now, userID, id, userID)
			if err != nil {
				return err
			}
			k, _ := res.RowsAffected()
			n += int(k)
		}
		return nil
	})
	return n, err
}

// DueDeliveries returns deliveries waiting to be tried, oldest first.
func (s *Store) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.integration_id, d.entry_id, i.user_id, d.state, d.attempts, d.error, d.remote, d.sent_at
		  FROM deliveries d JOIN integrations i ON i.id = d.integration_id JOIN users u ON u.id = i.user_id
		 WHERE d.state = 'pending' AND d.next_at <= ? AND u.disabled_at IS NULL
		 ORDER BY d.next_at LIMIT ?`, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.IntegrationID, &d.EntryID, &d.UserID, &d.State, &d.Attempts, &d.Error, &d.Remote, &d.SentAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeliverySent records a delivery that worked.
func (s *Store) DeliverySent(ctx context.Context, id int64, remote string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deliveries SET state = 'sent', attempts = attempts + 1, error = '', remote = ?, sent_at = ? WHERE id = ?`,
		remote, time.Now().Unix(), id)
	return err
}

// DeliveryFailed records a failed try: tried again at retryAt, or given up
// on when retryAt is zero.
func (s *Store) DeliveryFailed(ctx context.Context, id int64, errText string, retryAt time.Time) error {
	if retryAt.IsZero() {
		_, err := s.db.ExecContext(ctx, `UPDATE deliveries SET state = 'failed', attempts = attempts + 1, error = ? WHERE id = ?`, errText, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE deliveries SET attempts = attempts + 1, error = ?, next_at = ? WHERE id = ?`,
		errText, retryAt.Unix(), id)
	return err
}

// EntryDeliveries lists where a reader's entry was sent, or is being sent.
func (s *Store) EntryDeliveries(ctx context.Context, userID, entryID int64) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.integration_id, d.entry_id, d.state, d.attempts, d.error, d.remote, d.sent_at
		  FROM deliveries d JOIN integrations i ON i.id = d.integration_id
		 WHERE i.user_id = ? AND d.entry_id = ? ORDER BY d.id`, userID, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		d := Delivery{UserID: userID}
		if err := rows.Scan(&d.ID, &d.IntegrationID, &d.EntryID, &d.State, &d.Attempts, &d.Error, &d.Remote, &d.SentAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

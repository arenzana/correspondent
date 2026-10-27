// Package store owns the SQLite database: schema migrations, entry-ID
// allocation, and every query the application runs. SQL is written by hand
// and kept here so that transactions, upserts and cursors are reviewable.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB

	idMu   sync.Mutex
	lastID int64

	// OnStateChange, if set, is called after every committed read/unread/
	// star/unstar change with the IDs that actually changed, whichever API
	// or client caused it. It must not block.
	OnStateChange func(userID int64, action string, ids []int64)
}

// Open opens (creating if needed) the database at path and applies pending
// migrations. The database must live on local storage: WAL does not work
// over network filesystems.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// temp_store=MEMORY: the container root is read-only, so SQLite must not
	// spill sorts to temp files. One writer; a handful of readers is plenty.
	db.SetMaxOpenConns(4)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if _, err := s.FillSearchText(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("index article text: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM entries`).Scan(&s.lastID); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Ping verifies the database is reachable; used by /ready.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) migrate(ctx context.Context) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i, name := range names {
		if i < version {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// nextID returns a new entry ID: the current time in microseconds, bumped
// past the last allocated ID so IDs stay unique and strictly increasing even
// across imports or clock skew.
func (s *Store) nextID(now time.Time) int64 {
	s.idMu.Lock()
	defer s.idMu.Unlock()
	id := now.UnixMicro()
	if id <= s.lastID {
		id = s.lastID + 1
	}
	s.lastID = id
	return id
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// numbered returns n numbered placeholders starting at ?from, for queries
// that also reuse earlier numbered parameters.
func numbered(from, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("?%d", from+i)
	}
	return strings.Join(parts, ",")
}

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// Backup writes a consistent snapshot of the live database to dest using
// VACUUM INTO (safe while the server is writing, unlike copying the file and
// its WAL), then verifies the copy with an integrity check.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return err
	}
	copyDB, err := sql.Open("sqlite", "file:"+dest+"?mode=ro&_pragma=temp_store(MEMORY)")
	if err != nil {
		return err
	}
	defer copyDB.Close()
	var result string
	if err := copyDB.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("backup failed integrity check: %s", result)
	}
	return os.Chmod(dest, 0o600)
}

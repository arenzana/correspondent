package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/arenzana/correspondent/internal/migrate"
	"github.com/arenzana/correspondent/internal/store"
)

type migration struct {
	st        *store.Store
	user      string
	freshURL  string
	corrURL   string
	apiPWFile string
	full      bool
}

func (m migration) importExport(ctx context.Context, ex migrate.Export) error {
	if ex.SQLite == "" || ex.SystemConfig == "" || ex.UserConfig == "" {
		return errors.New("migrate import needs -export, -system-config and -user-config")
	}
	start := time.Now()
	rep, err := migrate.FromSQLite(ctx, m.st, ex)
	if rep != nil {
		fmt.Printf("Imported %s in %s\n", ex.Username, time.Since(start).Round(time.Millisecond))
		fmt.Printf("  folders %d, feeds %d (IDs preserved), %d paused as in FreshRSS\n", rep.Folders, rep.Feeds, rep.Paused)
		fmt.Printf("  articles %d (IDs %d to %d), unread %d, starred %d\n", rep.Entries, rep.MinID, rep.MaxID, rep.Unread, rep.Starred)
		fmt.Printf("  FreshRSS app token accepted: %v\n", ex.LegacyAuth)
		for _, s := range rep.Skipped {
			fmt.Println("  note:", s)
		}
	}
	return err
}

func (m migration) password() (string, error) {
	if m.apiPWFile == "" {
		return "", errors.New("-api-password-file is required")
	}
	b, err := os.ReadFile(m.apiPWFile)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

func (m migration) verify(ctx context.Context) error {
	if m.freshURL == "" {
		return errors.New("-freshrss is required")
	}
	pw, err := m.password()
	if err != nil {
		return err
	}
	old := migrate.NewReaderClient(m.freshURL)
	if err := old.Login(ctx, m.user, pw); err != nil {
		return fmt.Errorf("FreshRSS: %w", err)
	}
	cur := migrate.NewReaderClient(m.corrURL)
	if err := cur.Login(ctx, m.user, pw); err != nil {
		return fmt.Errorf("Correspondent: %w", err)
	}
	ok, err := migrate.Verify(ctx, old, cur, os.Stdout, m.full)
	if err != nil {
		return err
	}

	// The token Reeder already holds must work on Correspondent unchanged.
	fmt.Println("Existing app sign-in")
	reeder := migrate.NewReaderClient(m.corrURL)
	reeder.UseAuth(old.Auth())
	if _, err := reeder.Tags(ctx); err != nil {
		ok = false
		fmt.Println("  ✗ the token FreshRSS issued is refused; Reeder would have to sign in again:", err)
	} else {
		fmt.Println("  ✓ the token FreshRSS issued is accepted; Reeder needs no changes")
	}

	if !ok {
		return errors.New("verification failed: do not switch over")
	}
	fmt.Println("\nAll checks passed. Safe to switch over.")
	return nil
}

func (m migration) rollback(ctx context.Context, apply bool) error {
	uid, err := m.st.UserIDByName(ctx, m.user)
	if err != nil {
		return err
	}
	at, err := m.st.Setting(ctx, store.SettingMigratedAt)
	if err != nil {
		return errors.New("this database was not created by `migrate import`")
	}
	since, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return err
	}
	maxIDs, err := m.st.Setting(ctx, store.SettingMaxLegacyID)
	if err != nil {
		return err
	}
	maxID, _ := strconv.ParseInt(maxIDs, 10, 64)
	plan, err := migrate.PlanRollback(ctx, m.st, uid, since, maxID)
	if err != nil {
		return err
	}
	fmt.Printf("Changes made in Correspondent since %s:\n", since.Local().Format(time.RFC1123))
	fmt.Printf("  mark read %d, mark unread %d, star %d, unstar %d\n", len(plan.Read), len(plan.Unread), len(plan.Star), len(plan.Unstar))
	if plan.Skipped > 0 {
		fmt.Printf("  %d changes concern articles FreshRSS never had; they are not replayed\n", plan.Skipped)
	}
	if !apply {
		fmt.Println("\nNothing sent. Re-run with -apply to write these changes to FreshRSS.")
		return nil
	}
	if m.freshURL == "" {
		return errors.New("-freshrss is required")
	}
	pw, err := m.password()
	if err != nil {
		return err
	}
	fresh := migrate.NewReaderClient(m.freshURL)
	if err := fresh.Login(ctx, m.user, pw); err != nil {
		return fmt.Errorf("FreshRSS: %w", err)
	}
	if err := migrate.ApplyRollback(ctx, fresh, plan); err != nil {
		return err
	}
	fmt.Println("\nFreshRSS now has every read and star change made in Correspondent.")
	return nil
}

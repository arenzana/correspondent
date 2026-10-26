package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(ctx, filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	uid, _ := st.CreateUser(ctx, "u", "password-123")
	fid, _ := st.AddFeed(ctx, uid, "https://x.example/feed", "X", nil)
	st.UpsertEntries(ctx, fid, []NewEntry{{GUID: "1", Title: "one", Enclosures: "[]"}}, time.Time{})

	dest := filepath.Join(dir, "backup.db")
	if err := st.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	if err := st.Backup(ctx, dest); err == nil {
		t.Fatal("backup must refuse to overwrite")
	}
	copy, err := Open(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	entries, _, err := copy.Entries(ctx, Filter{UserID: uid}, false)
	if err != nil || len(entries) != 1 || entries[0].Title != "one" {
		t.Fatalf("restored entries: %+v %v", entries, err)
	}
}

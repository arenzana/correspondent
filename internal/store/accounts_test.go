package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAccounts(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	admin, _ := s.CreateUser(ctx, "ada", "password-ada-1")
	bob, _ := s.CreateUser(ctx, "bob", "password-bob-1")
	if u, _ := s.UserByID(ctx, admin); u.Role != RoleAdmin {
		t.Fatalf("first account is %q", u.Role)
	}
	if u, _ := s.UserByID(ctx, bob); u.Role != RoleUser {
		t.Fatalf("second account is %q", u.Role)
	}

	// The last admin can't be demoted, disabled or deleted.
	for name, err := range map[string]error{
		"demote":  s.SetRole(ctx, admin, RoleUser),
		"disable": s.SetDisabled(ctx, admin, true),
		"delete":  s.DeleteAccount(ctx, admin),
	} {
		if !errors.Is(err, ErrLastAdmin) {
			t.Errorf("%s last admin: %v", name, err)
		}
	}
	if err := s.SetRole(ctx, bob, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRole(ctx, admin, RoleUser); err != nil {
		t.Errorf("demote with another admin: %v", err)
	}
	s.SetRole(ctx, admin, RoleAdmin)
	s.SetRole(ctx, bob, RoleUser)

	// Disabling ends every session and stops sign-in, polling and app access.
	web, _ := s.CreateSession(ctx, bob, "web", time.Hour, "Safari on iPhone")
	app, _ := s.CreateSession(ctx, bob, "greader", time.Hour)
	fid, _ := s.AddFeed(ctx, bob, "https://bob.example/feed", "Bob", nil)
	if err := s.SetDisabled(ctx, bob, true); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{web, app} {
		if _, err := s.SessionUser(ctx, tok, "web"); err == nil {
			t.Error("session survived disabling")
		}
	}
	if _, err := s.CheckPassword(ctx, "bob", "password-bob-1", false); !errors.Is(err, ErrDisabled) {
		t.Errorf("disabled sign-in: %v", err)
	}
	if _, err := s.CheckPassword(ctx, "bob", "wrong-password", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("a wrong password must not reveal the account is disabled: %v", err)
	}
	if due, _ := s.DueFeeds(ctx, time.Now().Add(time.Hour), 10); len(due) != 0 {
		t.Errorf("disabled account's feeds polled: %+v", due)
	}
	s.SetDisabled(ctx, bob, false)
	if due, _ := s.DueFeeds(ctx, time.Now().Add(time.Hour), 10); len(due) != 1 || due[0].ID != fid {
		t.Errorf("re-enabled account's feeds: %+v", due)
	}

	// Sessions: listed with their device, ended one at a time.
	cur, _ := s.CreateSession(ctx, bob, "web", time.Hour, "Firefox on Mac")
	other, _ := s.CreateSession(ctx, bob, "web", time.Hour, "Safari on iPhone")
	list, _ := s.Sessions(ctx, bob, cur)
	if len(list) != 2 {
		t.Fatalf("%+v", list)
	}
	var otherID int64
	for _, x := range list {
		if x.Client == "Safari on iPhone" {
			otherID = x.ID
			if x.Current {
				t.Error("wrong current session")
			}
		}
	}
	if err := s.EndSession(ctx, admin, otherID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ended another user's session: %v", err)
	}
	s.EndSession(ctx, bob, otherID)
	if _, err := s.SessionUser(ctx, other, "web"); err == nil {
		t.Error("ended session still works")
	}

	// A new password ends the other web sessions, keeping this one.
	again, _ := s.CreateSession(ctx, bob, "web", time.Hour)
	if err := s.SetPassword(ctx, bob, "a-new-password-1", cur); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, cur, "web"); err != nil {
		t.Error("current session ended by a password change")
	}
	if _, err := s.SessionUser(ctx, again, "web"); err == nil {
		t.Error("other session survived a password change")
	}
	if !s.CheckUserPassword(ctx, bob, "a-new-password-1") || s.CheckUserPassword(ctx, bob, "password-bob-1") {
		t.Error("password not changed")
	}

	// Deleting removes everything that was theirs.
	s.UpsertEntries(ctx, fid, []NewEntry{{GUID: "1", Title: "x", Enclosures: "[]"}}, time.Time{})
	if err := s.DeleteAccount(ctx, bob); err != nil {
		t.Fatal(err)
	}
	accounts, _ := s.Accounts(ctx)
	if len(accounts) != 1 || accounts[0].Username != "ada" {
		t.Fatalf("%+v", accounts)
	}
	var left int
	s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM feeds) + (SELECT COUNT(*) FROM entries) + (SELECT COUNT(*) FROM sessions WHERE user_id = ?) + (SELECT COUNT(*) FROM journal WHERE user_id = ?)`, bob, bob).Scan(&left)
	if left != 0 {
		t.Errorf("%d rows left behind", left)
	}
}

func TestInvites(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	admin, _ := s.CreateUser(ctx, "ada", "password-ada-1")
	tok, err := s.CreateInvite(ctx, InviteAccount, 0, RoleUser, "for Carol", admin, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := s.PendingInvite(ctx, tok)
	if err != nil || inv.Note != "for Carol" || inv.CreatedBy != "ada" {
		t.Fatalf("%+v %v", inv, err)
	}
	if _, err := s.AcceptInvite(ctx, tok, "ada", "password-123456"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("taken name: %v", err)
	}
	if _, err := s.AcceptInvite(ctx, tok, "no spaces", "password-123456"); err == nil {
		t.Error("bad name accepted")
	}
	carol, err := s.AcceptInvite(ctx, tok, "carol", "password-carol-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptInvite(ctx, tok, "carol2", "password-carol-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("link used twice: %v", err)
	}
	if u, _ := s.CheckPassword(ctx, "carol", "password-carol-1", false); u == nil || u.ID != carol || u.Role != RoleUser {
		t.Errorf("%+v", u)
	}

	// Expired links don't work; revoked ones disappear.
	old, _ := s.CreateInvite(ctx, InviteAccount, 0, RoleAdmin, "", admin, -time.Minute)
	if _, err := s.PendingInvite(ctx, old); !errors.Is(err, ErrNotFound) {
		t.Error("expired invite pending")
	}
	keep, _ := s.CreateInvite(ctx, InviteAccount, 0, RoleAdmin, "", admin, time.Hour)
	pending, _ := s.PendingInvites(ctx)
	if len(pending) != 1 || pending[0].Role != RoleAdmin {
		t.Fatalf("%+v", pending)
	}
	s.RevokeInvite(ctx, pending[0].ID)
	if _, err := s.PendingInvite(ctx, keep); err == nil {
		t.Error("revoked invite works")
	}

	// Resets set the password and sign the account out; a newer link
	// replaces an older one.
	sess, _ := s.CreateSession(ctx, carol, "web", time.Hour)
	first, _ := s.CreateInvite(ctx, InviteReset, carol, RoleUser, "", admin, time.Hour)
	second, _ := s.CreateInvite(ctx, InviteReset, carol, RoleUser, "", admin, time.Hour)
	if _, err := s.PendingInvite(ctx, first); err == nil {
		t.Error("older reset link still works")
	}
	if inv, _ := s.PendingInvite(ctx, second); inv == nil || inv.Username != "carol" {
		t.Errorf("%+v", inv)
	}
	if id, err := s.AcceptInvite(ctx, second, "ignored", "password-reset-1"); err != nil || id != carol {
		t.Fatalf("%d %v", id, err)
	}
	if _, err := s.SessionUser(ctx, sess, "web"); err == nil {
		t.Error("session survived a reset")
	}
	if !s.CheckUserPassword(ctx, carol, "password-reset-1") {
		t.Error("reset didn't set the password")
	}
}

func TestValidUsername(t *testing.T) {
	for name, ok := range map[string]bool{"carol": true, "c.d-e_f": true, "Ana": true, "x": false, ".dot": false, "a b": false, "a/b": false, "émilie": true} {
		if (ValidUsername(name) == nil) != ok {
			t.Errorf("%q", name)
		}
	}
}

// TestInviteFromCommandLine: `correspondent invite` makes one with no
// creator (by 0), which must not trip the foreign key.
func TestInviteFromCommandLine(t *testing.T) {
	s, ctx := open(t), context.Background()
	tok, err := s.CreateInvite(ctx, InviteAccount, 0, RoleUser, "from the command line", 0, time.Hour)
	if err != nil || tok == "" {
		t.Fatalf("invite from the command line: %q %v", tok, err)
	}
}

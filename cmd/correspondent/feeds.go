package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/arenzana/correspondent/internal/store"
)

func feedsCommand(ctx context.Context, st *store.Store, sub, username string, ids []string) error {
	uid, err := st.UserIDByName(ctx, username)
	if err != nil {
		return fmt.Errorf("user %q: %w", username, err)
	}
	feeds, err := st.Feeds(ctx, uid)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		for _, f := range feeds {
			state := "active"
			if f.Paused {
				state = "paused"
			} else if f.ErrorCount > 0 {
				state = "failing"
			}
			fmt.Printf("%5d  %-7s  %4d unread  %s\n", f.ID, state, f.Unread, f.Title)
		}
		return nil
	case "resume":
		want := map[int64]bool{}
		for _, s := range ids {
			id, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return fmt.Errorf("feed ID %q: %w", s, err)
			}
			want[id] = true
		}
		n := 0
		for _, f := range feeds {
			if !f.Paused || (len(want) > 0 && !want[f.ID]) {
				continue
			}
			if err := st.SetFeedSchedule(ctx, uid, f.ID, 0, false); err != nil {
				return err
			}
			if err := st.ScheduleNow(ctx, f.ID); err != nil {
				return err
			}
			fmt.Printf("resumed %d  %s\n", f.ID, f.Title)
			n++
		}
		fmt.Printf("%d feed(s) resumed; they update on the next poll\n", n)
		return nil
	default:
		return fmt.Errorf("unknown feeds command %q", sub)
	}
}

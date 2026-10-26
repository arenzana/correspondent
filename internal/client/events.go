package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Event is a live change on the server: Name is refresh (new articles),
// state (articles marked: Action and IDs), resync (reload everything; also
// sent after a reconnect, since changes may have been missed).
type Event struct {
	Name   string
	Action string
	IDs    []string
}

// Watch follows the server's live events until ctx ends, reconnecting with
// backoff when the connection drops. It returns early only for an
// authentication failure.
func (c *Client) Watch(ctx context.Context, on func(Event)) error {
	backoff := time.Second
	first := true
	for ctx.Err() == nil {
		err := c.stream(ctx, on, !first)
		var e *Error
		if errors.As(err, &e) && (e.Status == 401 || e.Status == 403) {
			return err
		}
		first = false
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
	}
	return ctx.Err()
}

func (c *Client) stream(ctx context.Context, on func(Event), reconnected bool) error {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.Server, "/")+"/api/v1/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "text/event-stream")
	// No overall timeout: the stream stays open; keepalives arrive every 25 s.
	hc := &http.Client{}
	if c.HTTP != nil {
		hc = &http.Client{Transport: c.HTTP.Transport}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &Error{Status: resp.StatusCode, Message: "live updates refused"}
	}
	if reconnected {
		on(Event{Name: "resync"})
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var name, data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if name != "" {
				ev := Event{Name: name}
				if name == "state" {
					var d struct {
						Action string   `json:"action"`
						IDs    []string `json:"ids"`
					}
					json.Unmarshal([]byte(data), &d)
					ev.Action, ev.IDs = d.Action, d.IDs
				}
				on(ev)
			}
			name, data = "", ""
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	return sc.Err()
}

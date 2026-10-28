package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Server-sent events keep every open reader in step, whatever made the
// change: Reeder, another tab, another device.
//
//	event: refresh   new articles arrived
//	event: state     {"action":"read","ids":["…"]}; read, unread, star or unstar
//	event: resync    this reader fell behind; reload counts and visible state
//	event: integrations  the services articles can be sent to changed

type event struct {
	name string
	data []byte
}

// NotifyState tells a user's open readers that entries changed state. It is
// wired to the store, which calls it for every committed change.
func (a *API) NotifyState(userID int64, action string, ids []int64) {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = strconv.FormatInt(id, 10)
	}
	data, _ := json.Marshal(map[string]any{"action": action, "ids": strs})
	a.hub.publish(userID, event{name: "state", data: data})
}

// NotifyIntegrations tells a user's open readers that their services
// changed, so the apps' Send to menus show a new one at once.
func (a *API) NotifyIntegrations(userID int64) {
	a.hub.publish(userID, event{name: "integrations", data: []byte("{}")})
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "Streaming unsupported.")
		return
	}
	sub, ok := a.hub.subscribe(userID(r))
	if !ok {
		fail(w, http.StatusTooManyRequests, "Too many open windows.")
		return
	}
	defer a.hub.unsubscribe(userID(r), sub)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, "retry: 5000\n\n")
	flusher.Flush()
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-sub.ch:
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, ev.data)
		case <-sub.overflowed:
			// Events were dropped while this reader was slow; drain what's
			// queued and ask it to reload instead of applying a partial stream.
			for len(sub.ch) > 0 {
				<-sub.ch
			}
			fmt.Fprint(w, "event: resync\ndata: {}\n\n")
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
		}
		flusher.Flush()
	}
}

// subscriber is one open event stream.
type subscriber struct {
	ch         chan event
	overflowed chan struct{} // signalled (capacity 1) when an event was dropped
	dropped    atomic.Bool
}

// hub fans out events to a user's open streams. Publishing never blocks: a
// stream that can't keep up is told to resync.
type hub struct {
	mu   sync.Mutex
	subs map[int64]map[*subscriber]struct{}
}

// subscribe registers a stream; each user may hold at most 16 at once.
func (h *hub) subscribe(uid int64) (*subscriber, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs == nil {
		h.subs = map[int64]map[*subscriber]struct{}{}
	}
	if h.subs[uid] == nil {
		h.subs[uid] = map[*subscriber]struct{}{}
	}
	if len(h.subs[uid]) >= 16 {
		return nil, false
	}
	s := &subscriber{ch: make(chan event, 64), overflowed: make(chan struct{}, 1)}
	h.subs[uid][s] = struct{}{}
	return s, true
}

func (h *hub) unsubscribe(uid int64, s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[uid], s)
}

func (h *hub) publish(uid int64, ev event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[uid] {
		select {
		case s.ch <- ev:
		default:
			select {
			case s.overflowed <- struct{}{}:
			default:
			}
		}
	}
}

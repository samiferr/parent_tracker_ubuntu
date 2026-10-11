// Package live holds the latest state per monitored user in memory and fans it
// out to dashboard clients (docs/04 §4.11).
//
// Publishing never blocks: each client has a small buffer, and a client whose
// buffer is full is dropped rather than waited for, so a stuck browser tab can
// never slow the sampling path (NFR-1.9).
package live

import (
	"encoding/json"
	"errors"
	"sync"
)

// App is the foreground application. Title is present only when title capture
// is switched on (SEC-16, AC-86).
type App struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
}

// Totals are today's running figures, including the in-progress interval
// clamped to the last good sample.
type Totals struct {
	ActiveMs int64 `json:"active_ms"`
	IdleMs   int64 `json:"idle_ms"`
	LockedMs int64 `json:"locked_ms"`
	GapMs    int64 `json:"gap_ms"`
}

// Hour is the current local hour so far.
type Hour struct {
	HourStart int64 `json:"hour_start"`
	ActiveMs  int64 `json:"active_ms"`
}

// Degraded describes a collector that is not fully working.
type Degraded struct {
	Collector   string `json:"collector"`
	Session     string `json:"session,omitempty"`
	SessionType string `json:"session_type,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
	NextStep    string `json:"next_step,omitempty"`
}

// Snapshot is the live payload (docs/04 §4.11). Nothing outside these fields is
// ever sent.
type Snapshot struct {
	User         string     `json:"user"`
	UID          int        `json:"uid"`
	State        string     `json:"state"`
	Since        int64      `json:"since"`
	SessionType  string     `json:"session_type,omitempty"`
	Provider     string     `json:"provider,omitempty"`
	App          *App       `json:"app"`
	AppStatus    string     `json:"app_status"`
	Today        Totals     `json:"today"`
	Hour         Hour       `json:"hour"`
	LastSampleAt int64      `json:"last_sample_at"`
	IntervalMs   int64      `json:"interval_ms"`
	ServerTime   int64      `json:"server_time"`
	Degraded     []Degraded `json:"degraded"`
}

// Event is one server-sent event.
type Event struct {
	Name string
	Data []byte
}

// Errors returned by Subscribe.
var (
	ErrDisabled = errors.New("live view is off")
	ErrTooMany  = errors.New("too many live viewers")
)

const clientBuffer = 8

// Client is one subscriber.
type Client struct {
	hub  *Hub
	uid  int
	ch   chan Event
	once sync.Once
}

// Events delivers events until the client is closed or dropped; the channel is
// then closed.
func (c *Client) Events() <-chan Event { return c.ch }

// Close unsubscribes.
func (c *Client) Close() { c.hub.drop(c) }

// Hub is the in-memory live state.
type Hub struct {
	mu      sync.Mutex
	snaps   map[int]Snapshot
	clients map[*Client]struct{}
	max     int
	enabled bool
	dropped int
}

// New creates a hub.
func New(maxClients int, enabled bool) *Hub {
	return &Hub{snaps: map[int]Snapshot{}, clients: map[*Client]struct{}{}, max: maxClients, enabled: enabled}
}

// Configure applies reloadable settings. Turning the live view off closes every
// open stream with a final "bye" event (AC-87).
func (h *Hub) Configure(maxClients int, enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.max = maxClients
	if h.enabled && !enabled {
		for c := range h.clients {
			h.send(c, Event{Name: "bye", Data: []byte(`{"reason":"live view turned off"}`)})
			h.closeLocked(c)
		}
	}
	h.enabled = enabled
}

// Enabled reports whether the live view is on.
func (h *Hub) Enabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enabled
}

// Publish stores the latest snapshot for a user and sends it to that user's
// subscribers as an event of the given name ("state" or "tick").
func (h *Hub) Publish(uid int, s Snapshot, event string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snaps[uid] = s
	if !h.enabled || len(h.clients) == 0 {
		return
	}
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	for c := range h.clients {
		if c.uid != uid {
			continue
		}
		if !h.send(c, Event{Name: event, Data: data}) {
			h.dropped++
			h.closeLocked(c)
		}
	}
}

// Remove forgets a user (no longer monitored).
func (h *Hub) Remove(uid int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.snaps, uid)
}

// Snapshot returns the latest snapshot for a user.
func (h *Hub) Snapshot(uid int) (Snapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.snaps[uid]
	return s, ok
}

// Clients is the number of open subscriptions.
func (h *Hub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Dropped is how many slow clients have been dropped.
func (h *Hub) Dropped() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dropped
}

// Subscribe opens a stream for one user. The first event is the current
// snapshot, when there is one.
func (h *Hub) Subscribe(uid int) (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.enabled {
		return nil, ErrDisabled
	}
	if len(h.clients) >= h.max {
		return nil, ErrTooMany
	}
	c := &Client{hub: h, uid: uid, ch: make(chan Event, clientBuffer)}
	h.clients[c] = struct{}{}
	if s, ok := h.snaps[uid]; ok {
		if data, err := json.Marshal(s); err == nil {
			c.ch <- Event{Name: "snapshot", Data: data}
		}
	}
	return c, nil
}

func (h *Hub) send(c *Client, e Event) bool {
	select {
	case c.ch <- e:
		return true
	default:
		return false
	}
}

func (h *Hub) drop(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closeLocked(c)
}

func (h *Hub) closeLocked(c *Client) {
	c.once.Do(func() {
		delete(h.clients, c)
		close(c.ch)
	})
}

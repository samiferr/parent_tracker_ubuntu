package live

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func recv(t *testing.T, c *Client) Event {
	t.Helper()
	select {
	case e, ok := <-c.Events():
		if !ok {
			t.Fatal("channel closed")
		}
		return e
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	return Event{}
}

func TestSubscribeGetsSnapshotThenUpdates(t *testing.T) {
	h := New(10, true)
	h.Publish(1000, Snapshot{User: "ilyes", UID: 1000, State: "active"}, "tick")
	c, err := h.Subscribe(1000)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if e := recv(t, c); e.Name != "snapshot" || !strings.Contains(string(e.Data), `"state":"active"`) {
		t.Fatalf("first event = %s %s", e.Name, e.Data)
	}
	h.Publish(1000, Snapshot{User: "ilyes", UID: 1000, State: "locked"}, "state")
	h.Publish(2000, Snapshot{User: "maram", UID: 2000, State: "active"}, "state") // another user: not delivered
	if e := recv(t, c); e.Name != "state" || !strings.Contains(string(e.Data), `"state":"locked"`) {
		t.Fatalf("event = %s %s", e.Name, e.Data)
	}
	select {
	case e := <-c.Events():
		t.Fatalf("leaked another user's event: %s", e.Data)
	default:
	}
}

// AC-85: opening live_max_clients+1 streams fails for the extra one without
// disturbing the others.
func TestClientCap(t *testing.T) {
	h := New(2, true)
	a, _ := h.Subscribe(1)
	b, _ := h.Subscribe(1)
	if _, err := h.Subscribe(1); err != ErrTooMany {
		t.Fatalf("err = %v", err)
	}
	a.Close()
	c, err := h.Subscribe(1)
	if err != nil {
		t.Fatalf("a slot should free up: %v", err)
	}
	b.Close()
	c.Close()
	if h.Clients() != 0 {
		t.Fatalf("clients = %d", h.Clients())
	}
}

// NFR-1.9: a stuck client is dropped, publishing never blocks.
func TestSlowClientIsDroppedAndNeverBlocksPublish(t *testing.T) {
	h := New(10, true)
	slow, _ := h.Subscribe(1)
	fast, _ := h.Subscribe(1)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			h.Publish(1, Snapshot{UID: 1, LastSampleAt: int64(i)}, "tick")
			select { // the fast client keeps up
			case <-fast.Events():
			default:
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a stuck client")
	}
	if h.Dropped() != 1 {
		t.Fatalf("dropped = %d, want 1", h.Dropped())
	}
	// the slow client's channel ends after its buffered events
	n := 0
	for range slow.Events() {
		n++
	}
	if n == 0 || n > clientBuffer {
		t.Fatalf("slow client received %d events", n)
	}
	if h.Clients() != 1 {
		t.Fatalf("clients = %d", h.Clients())
	}
}

// AC-87: turning the live view off sends a final bye and refuses new streams.
func TestDisableSendsByeAndRefuses(t *testing.T) {
	h := New(10, true)
	c, _ := h.Subscribe(1)
	h.Configure(10, false)
	e := recv(t, c)
	if e.Name != "bye" {
		t.Fatalf("event = %s", e.Name)
	}
	if _, ok := <-c.Events(); ok {
		t.Fatal("stream should end after bye")
	}
	if _, err := h.Subscribe(1); err != ErrDisabled {
		t.Fatalf("err = %v", err)
	}
	h.Configure(10, true) // re-enabling restores streaming without a restart
	if _, err := h.Subscribe(1); err != nil {
		t.Fatal(err)
	}
}

// AC-86: the payload has exactly the documented fields, and a title only when set.
func TestPayloadFields(t *testing.T) {
	b, _ := json.Marshal(Snapshot{User: "u", App: &App{ID: "code", Name: "Code"}, Degraded: []Degraded{}})
	var m map[string]any
	json.Unmarshal(b, &m)
	want := []string{"user", "uid", "state", "since", "app", "app_status", "today", "hour", "last_sample_at", "interval_ms", "server_time", "degraded"}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("missing field %q", k)
		}
	}
	allowed := map[string]bool{"session_type": true, "provider": true}
	for k := range m {
		found := allowed[k]
		for _, w := range want {
			found = found || w == k
		}
		if !found {
			t.Errorf("undocumented field %q in the live payload", k)
		}
	}
	if strings.Contains(string(b), "title") {
		t.Fatal("title must be omitted when empty")
	}
}

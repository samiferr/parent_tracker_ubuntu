package collector

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Fake is a scriptable Source: the "fake session harness" of docs/08 §8.3. It is
// used by tests and by the dashboard's demo mode.
type Fake struct {
	mu       sync.Mutex
	sessions map[string]SessionInfo
	pids     map[int]string
	events   chan Event
	err      error
}

// NewFake creates an empty fake source.
func NewFake() *Fake {
	return &Fake{sessions: map[string]SessionInfo{}, pids: map[int]string{}, events: make(chan Event, 64)}
}

// Set adds or replaces a session.
func (f *Fake) Set(s SessionInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.ID] = s
}

// Update changes a session in place.
func (f *Fake) Update(id string, fn func(*SessionInfo)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[id]
	fn(&s)
	f.sessions[id] = s
}

// Remove deletes a session (logout).
func (f *Fake) Remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
}

// MapPID makes SessionOfPID return a session id for a process.
func (f *Fake) MapPID(pid int, session string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pids[pid] = session
}

// FailWith makes Sessions fail (nil clears it).
func (f *Fake) FailWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Emit delivers a wake-up event.
func (f *Fake) Emit(e Event) {
	select {
	case f.events <- e:
	default:
	}
}

// Sessions implements Source.
func (f *Fake) Sessions(context.Context) ([]SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]SessionInfo, 0, len(f.sessions))
	for _, s := range f.sessions {
		out = append(out, s)
	}
	return out, nil
}

// SessionOfPID implements Source.
func (f *Fake) SessionOfPID(_ context.Context, pid int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.pids[pid]; ok {
		return id, nil
	}
	return "", fmt.Errorf("no session for pid %d", pid)
}

// Events implements Source.
func (f *Fake) Events() <-chan Event { return f.events }

// Close implements Source.
func (f *Fake) Close() error { return nil }

// UserSession is a convenience constructor for a normal local graphical session.
func UserSession(id string, uid int, user string, typ string, desktop string) SessionInfo {
	return SessionInfo{ID: id, UID: uid, User: user, Seat: "seat0", Type: ParseType(typ), Class: "user",
		Desktop: desktop, Active: true, State: "active", IdleSince: time.Time{}}
}

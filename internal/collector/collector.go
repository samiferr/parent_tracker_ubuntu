// Package collector reads session state. The core metrics — active, idle,
// locked and suspended time — come from systemd-logind and are the same on
// Wayland and X11 sessions (FR-3.10, NFR-3.9).
package collector

import (
	"context"
	"strings"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// SessionInfo is one logind session.
type SessionInfo struct {
	ID         string
	UID        int
	User       string
	Seat       string
	Type       model.SessionType
	Class      string // user, greeter, lock-screen, background, ...
	Desktop    string
	Remote     bool
	Active     bool // this session is the one on the seat's screen
	LockedHint bool
	IdleHint   bool
	IdleSince  time.Time // zero when logind does not know
	State      string    // online, active, closing
}

// EventKind classifies a wake-up from the source.
type EventKind int

const (
	// EventChanged means some session property changed: re-evaluate now.
	EventChanged EventKind = iota
	// EventSleep means the machine is about to suspend.
	EventSleep
	// EventResume means the machine has resumed.
	EventResume
)

// Event is a wake-up from the source.
type Event struct {
	Kind EventKind
	At   time.Time
}

// Source provides session state.
type Source interface {
	// Sessions lists every logind session.
	Sessions(ctx context.Context) ([]SessionInfo, error)
	// SessionOfPID maps a process to its logind session id; used to identify
	// which session an agent belongs to without trusting what the agent says.
	SessionOfPID(ctx context.Context, pid int) (string, error)
	// Events delivers wake-ups. Delivery is best effort: the tracker also polls.
	Events() <-chan Event
	Close() error
}

// Eligible reports whether a session may be recorded at all: a real, local,
// user-class session of an allowlisted user (FR-2.1, FR-2.8, FR-2.9, AC-102).
func Eligible(s SessionInfo, cfg *config.Config) bool {
	if s.Class != "user" || s.Remote || s.State == "closing" {
		return false
	}
	return cfg.IsMonitored(s.User)
}

// IsGnome reports whether logind's Desktop value names a GNOME session. On
// Ubuntu the value is "ubuntu" (XDG_SESSION_DESKTOP).
func IsGnome(desktop string) bool {
	d := strings.ToLower(desktop)
	return strings.Contains(d, "gnome") || strings.HasPrefix(d, "ubuntu")
}

// ProviderFor picks the foreground-app provider of a session (docs/04 §4.13.3).
// The X11 provider is never chosen for a Wayland session: under Wayland an X11
// tool only sees XWayland clients, which are not necessarily the focused window,
// so its results would be silently wrong (FR-3.9, AC-104).
func ProviderFor(s SessionInfo) model.Provider {
	switch s.Type {
	case model.SessionX11:
		return model.ProviderX11
	case model.SessionWayland:
		if IsGnome(s.Desktop) {
			return model.ProviderGnomeWayland
		}
	}
	return model.ProviderNone
}

// ParseType maps logind's Type property.
func ParseType(t string) model.SessionType {
	switch t {
	case "wayland":
		return model.SessionWayland
	case "x11":
		return model.SessionX11
	case "tty":
		return model.SessionTTY
	}
	return model.SessionUnknown
}

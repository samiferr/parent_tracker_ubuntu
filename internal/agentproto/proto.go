// Package agentproto is the wire format between parent-tracker-agent (running as
// the monitored user) and parent-trackerd. Messages are newline-delimited JSON
// over a Unix socket; the daemon identifies the peer with SO_PEERCRED, never by
// what the agent claims (SEC-13).
package agentproto

import "encoding/json"

// Version of the protocol.
const Version = 1

// MaxLine bounds one message so a misbehaving agent cannot exhaust memory.
const MaxLine = 16 << 10

// Report statuses. Anything but StatusOK means the foreground sensor is not
// working, and the daemon says so (FR-3.11, SEC-35).
const (
	StatusOK          = "ok"
	StatusMissing     = "missing"      // the GNOME Shell extension is not loaded
	StatusDisabled    = "disabled"     // the extension is installed but turned off
	StatusNoHeartbeat = "no-heartbeat" // the extension is loaded but not answering
	StatusUnreachable = "unreachable"  // the X server cannot be reached
	StatusNoEWMH      = "no-ewmh"      // X is up but the window manager does not report the active window (yet)
	StatusError       = "error"
)

// Hello is the first message an agent sends.
type Hello struct {
	Type string `json:"type"` // "hello"
	// Version of the protocol the agent speaks.
	Version int `json:"version"`
	// SessionHint is the agent's XDG_SESSION_ID. The daemon only uses it to pick
	// between several graphical sessions of the same user, and checks that the
	// session belongs to the peer's uid.
	SessionHint string `json:"session_hint,omitempty"`
	// SessionType is the agent's XDG_SESSION_TYPE, a cross-check only; on
	// disagreement logind wins and the difference is logged.
	SessionType string `json:"session_type,omitempty"`
}

// Welcome is the daemon's answer to Hello.
type Welcome struct {
	Type       string `json:"type"` // "welcome"
	Monitored  bool   `json:"monitored"`
	Reason     string `json:"reason,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Provider   string `json:"provider,omitempty"` // x11 | gnome-wayland | none
	Title      bool   `json:"title"`
	IntervalMs int64  `json:"interval_ms"`
}

// Config is pushed to a connected agent when the daemon reloads its settings.
type Config struct {
	Type       string `json:"type"` // "config"
	Provider   string `json:"provider"`
	Title      bool   `json:"title"`
	IntervalMs int64  `json:"interval_ms"`
}

// App is the foreground application.
type App struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	PID  int    `json:"pid,omitempty"`
}

// Report is one observation, sent at least once per sampling interval; it is
// also the agent's heartbeat.
type Report struct {
	Type     string `json:"type"` // "report"
	Provider string `json:"provider"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
	App      *App   `json:"app,omitempty"`
	// Title is sent only when the daemon said title capture is on.
	Title string `json:"title,omitempty"`
}

// Kind returns the "type" field of a raw message.
func Kind(line []byte) string {
	var k struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(line, &k)
	return k.Type
}

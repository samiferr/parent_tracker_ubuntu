// Package agent is the per-session foreground-app collector. It runs as the
// monitored user, inside their graphical session, and reports upwards to the
// daemon over a Unix socket (docs/04 §4.5).
package agent

import (
	"context"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// Provider reads the foreground application of one session.
type Provider interface {
	// Name is the provider id the daemon assigned (x11 or gnome-wayland).
	Name() model.Provider
	// Poll reads the focused application. titles says whether the window title
	// may be read; the provider must not read it otherwise (SEC-17).
	Poll(ctx context.Context, titles bool) agentproto.Report
	// Close releases the provider's connection.
	Close()
}

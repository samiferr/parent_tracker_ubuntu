package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/appmap"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// Names of the project's GNOME Shell extension and its D-Bus interface. They
// must match gnome-extension/metadata.json and extension.js.
const (
	ExtensionUUID = "parent-tracker@samiferr.github.io"
	SensorName    = "org.parenttracker.Sensor"
	SensorPath    = dbus.ObjectPath("/org/parenttracker/Sensor")
	SensorIface   = "org.parenttracker.Sensor"

	shellName  = "org.gnome.Shell"
	shellPath  = dbus.ObjectPath("/org/gnome/Shell")
	shellExtIf = "org.gnome.Shell.Extensions"
)

// GnomeWayland reads the focused application from the GNOME Shell extension.
// On Wayland GNOME hides other clients' windows from every program, a root
// daemon included, and org.gnome.Shell.Introspect answers only the desktop
// portals, so an extension running inside the shell is the one supported route
// (docs/04 §4.3).
type GnomeWayland struct {
	// Conn is the session bus connection (tests inject a private bus).
	Conn *dbus.Conn

	mu   sync.Mutex
	once sync.Once
	err  error
}

// NewGnomeWayland creates the provider on the user's session bus.
func NewGnomeWayland() *GnomeWayland { return &GnomeWayland{} }

// Name implements Provider.
func (g *GnomeWayland) Name() model.Provider { return model.ProviderGnomeWayland }

// Close implements Provider. The shared session bus connection is left open.
func (g *GnomeWayland) Close() {}

func (g *GnomeWayland) conn() (*dbus.Conn, error) {
	g.once.Do(func() {
		if g.Conn == nil {
			g.Conn, g.err = dbus.SessionBus()
		}
	})
	return g.Conn, g.err
}

func (g *GnomeWayland) report(status, detail string) agentproto.Report {
	return agentproto.Report{Type: "report", Provider: string(model.ProviderGnomeWayland), Status: status, Detail: detail}
}

// Poll implements Provider. The call to the extension doubles as the heartbeat:
// an answer means it is alive, silence means "no-heartbeat" (SEC-35).
func (g *GnomeWayland) Poll(ctx context.Context, titles bool) agentproto.Report {
	g.mu.Lock()
	defer g.mu.Unlock()
	conn, err := g.conn()
	if err != nil {
		return g.report(agentproto.StatusError, "cannot reach the session bus: "+err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var owned bool
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, SensorName).Store(&owned); err != nil {
		return g.report(agentproto.StatusError, err.Error())
	}
	if !owned {
		return g.extensionState(ctx, conn)
	}

	obj := conn.Object(SensorName, SensorPath)
	// The extension reads window titles only while told to (SEC-17).
	if err := obj.CallWithContext(ctx, SensorIface+".SetTitleCapture", 0, titles).Err; err != nil {
		return g.report(agentproto.StatusNoHeartbeat, err.Error())
	}
	var (
		avail         bool
		id, name, ttl string
		pid           uint32
	)
	if err := obj.CallWithContext(ctx, SensorIface+".GetFocus", 0).Store(&avail, &id, &name, &pid, &ttl); err != nil {
		return g.report(agentproto.StatusNoHeartbeat, err.Error())
	}
	rep := g.report(agentproto.StatusOK, "")
	if avail && id != "" {
		rep.App = &agentproto.App{ID: appmap.Normalize(id), Name: name, PID: int(pid)}
		if titles {
			rep.Title = ttl
		}
	}
	return rep
}

// extensionState explains why the extension's bus name is absent, using the
// shell's own extension manager when it will answer.
func (g *GnomeWayland) extensionState(ctx context.Context, conn *dbus.Conn) agentproto.Report {
	var info map[string]dbus.Variant
	err := conn.Object(shellName, shellPath).CallWithContext(ctx, shellExtIf+".GetExtensionInfo", 0, ExtensionUUID).Store(&info)
	if err != nil || len(info) == 0 {
		return g.report(agentproto.StatusMissing, "the extension is not installed or not loaded in this session")
	}
	state, _ := info["state"].Value().(float64)
	switch int(state) {
	case 1: // enabled in the shell, but its D-Bus name is not up
		return g.report(agentproto.StatusNoHeartbeat, "the extension is enabled but not answering")
	case 3:
		return g.report(agentproto.StatusDisabled, "the extension failed to load")
	case 4:
		return g.report(agentproto.StatusDisabled, "the extension is out of date for this GNOME Shell version")
	case 6: // initialised, not yet enabled: the shell has not been restarted since install
		return g.report(agentproto.StatusMissing, "the extension is installed but not loaded yet: log out and back in")
	}
	return g.report(agentproto.StatusDisabled, fmt.Sprintf("the extension is turned off (state %d)", int(state)))
}

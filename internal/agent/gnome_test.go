package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/testutil"
)

// mockSensor plays the GNOME Shell extension.
type mockSensor struct {
	titles atomic.Bool
	fail   atomic.Bool
	avail  atomic.Bool
}

func (m *mockSensor) SetTitleCapture(on bool) *dbus.Error { m.titles.Store(on); return nil }
func (m *mockSensor) GetFocus() (bool, string, string, uint32, string, *dbus.Error) {
	if m.fail.Load() {
		return false, "", "", 0, "", dbus.NewError("org.parenttracker.Failed", []any{"boom"})
	}
	title := ""
	if m.titles.Load() {
		title = "Bank statement"
	}
	return m.avail.Load(), "firefox_firefox.desktop", "Firefox", 1234, title, nil
}

// mockShell plays org.gnome.Shell.Extensions.
type mockShell struct{ state atomic.Int64 }

func (m *mockShell) GetExtensionInfo(uuid string) (map[string]dbus.Variant, *dbus.Error) {
	st := m.state.Load()
	if st == 0 {
		return map[string]dbus.Variant{}, nil
	}
	return map[string]dbus.Variant{"state": dbus.MakeVariant(float64(st))}, nil
}

func ownName(t *testing.T, addr, name string, path dbus.ObjectPath, iface string, obj any) *dbus.Conn {
	t.Helper()
	c, err := testutil.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.Export(obj, path, iface); err != nil {
		t.Fatal(err)
	}
	if r, err := c.RequestName(name, dbus.NameFlagDoNotQueue); err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName(%s): %v %v", name, r, err)
	}
	return c
}

// AC-100 (provider part), AC-105: the extension's answer is the heartbeat, and
// each way it can be absent is told apart.
func TestGnomeProviderStates(t *testing.T) {
	_, addr := testutil.PrivateBus(t)
	client, _ := testutil.Dial(addr)
	defer client.Close()
	g := &GnomeWayland{Conn: client}
	ctx := context.Background()

	// nothing on the bus: the extension is not loaded in this session
	rep := g.Poll(ctx, false)
	if rep.Status != agentproto.StatusMissing || rep.Provider != "gnome-wayland" {
		t.Fatalf("absent: %+v", rep)
	}

	// the shell knows the extension, but it is turned off
	sh := &mockShell{}
	sh.state.Store(2)
	ownName(t, addr, shellName, shellPath, shellExtIf, sh)
	if rep = g.Poll(ctx, false); rep.Status != agentproto.StatusDisabled {
		t.Fatalf("disabled: %+v", rep)
	}
	sh.state.Store(6)
	if rep = g.Poll(ctx, false); rep.Status != agentproto.StatusMissing {
		t.Fatalf("installed but not loaded yet: %+v", rep)
	}
	sh.state.Store(1)
	if rep = g.Poll(ctx, false); rep.Status != agentproto.StatusNoHeartbeat {
		t.Fatalf("enabled but silent: %+v", rep)
	}

	// the extension runs
	sensor := &mockSensor{}
	sensor.avail.Store(true)
	ownName(t, addr, SensorName, SensorPath, SensorIface, sensor)
	rep = g.Poll(ctx, false)
	if rep.Status != agentproto.StatusOK || rep.App == nil || rep.App.ID != "firefox_firefox" || rep.App.Name != "Firefox" || rep.App.PID != 1234 {
		t.Fatalf("ok: %+v", rep)
	}
	if rep.Title != "" || sensor.titles.Load() {
		t.Fatal("titles must be off unless the daemon allowed them (SEC-17)")
	}
	rep = g.Poll(ctx, true)
	if rep.Title != "Bank statement" || !sensor.titles.Load() {
		t.Fatalf("titles on: %+v", rep)
	}
	rep = g.Poll(ctx, false)
	if rep.Title != "" || sensor.titles.Load() {
		t.Fatal("turning title capture off must reach the extension")
	}

	sensor.avail.Store(false) // nothing focused
	if rep = g.Poll(ctx, false); rep.Status != agentproto.StatusOK || rep.App != nil {
		t.Fatalf("no focus: %+v", rep)
	}

	sensor.fail.Store(true) // the extension is loaded but broken
	if rep = g.Poll(ctx, false); rep.Status != agentproto.StatusNoHeartbeat {
		t.Fatalf("failing: %+v", rep)
	}
}

func TestGnomeProviderTimeoutIsNoHeartbeat(t *testing.T) {
	_, addr := testutil.PrivateBus(t)
	client, _ := testutil.Dial(addr)
	defer client.Close()
	g := &GnomeWayland{Conn: client}
	// owns the name but never answers: a hung shell
	c, _ := testutil.Dial(addr)
	defer c.Close()
	if r, _ := c.RequestName(SensorName, dbus.NameFlagDoNotQueue); r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatal("cannot own the name")
	}
	start := time.Now()
	rep := g.Poll(context.Background(), false)
	if rep.Status != agentproto.StatusNoHeartbeat {
		t.Fatalf("%+v", rep)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("poll took %v", time.Since(start))
	}
}

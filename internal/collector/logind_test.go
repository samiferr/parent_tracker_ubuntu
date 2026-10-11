package collector

import (
	"context"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/testutil"
)

// A mock org.freedesktop.login1 on a private bus, so the real D-Bus code path is
// exercised without a desktop (docs/08 §8.3 "fake session harness").

type mockManager struct {
	sessions []listedSession
	pidMap   map[uint32]dbus.ObjectPath
}

func (m *mockManager) ListSessions() ([]listedSession, *dbus.Error) { return m.sessions, nil }
func (m *mockManager) GetSessionByPID(pid uint32) (dbus.ObjectPath, *dbus.Error) {
	if p, ok := m.pidMap[pid]; ok {
		return p, nil
	}
	return "", dbus.NewError("org.freedesktop.login1.NoSessionForPID", []any{"no session"})
}

type mockLogind struct {
	conn  *dbus.Conn
	props map[dbus.ObjectPath]*prop.Properties
}

func startMockLogind(t *testing.T, addr string) *mockLogind {
	t.Helper()
	conn, err := testutil.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	m := &mockLogind{conn: conn, props: map[dbus.ObjectPath]*prop.Properties{}}
	mgr := &mockManager{pidMap: map[uint32]dbus.ObjectPath{}}

	add := func(id string, uid uint32, user, typ, class, desktop string, remote, active bool, pid uint32) {
		path := dbus.ObjectPath("/org/freedesktop/login1/session/_" + id)
		mgr.sessions = append(mgr.sessions, listedSession{ID: id, UID: uid, User: user, Seat: "seat0", Path: path})
		if pid != 0 {
			mgr.pidMap[pid] = path
		}
		spec := map[string]map[string]*prop.Prop{sessionIface: {
			"Id": {Value: id, Emit: prop.EmitTrue}, "Name": {Value: user, Emit: prop.EmitTrue},
			"Type": {Value: typ, Emit: prop.EmitTrue}, "Class": {Value: class, Emit: prop.EmitTrue},
			"Desktop": {Value: desktop, Emit: prop.EmitTrue}, "Remote": {Value: remote, Emit: prop.EmitTrue},
			"Active": {Value: active, Emit: prop.EmitTrue, Writable: true}, "State": {Value: "active", Emit: prop.EmitTrue},
			"LockedHint": {Value: false, Emit: prop.EmitTrue, Writable: true}, "IdleHint": {Value: false, Emit: prop.EmitTrue, Writable: true},
			"IdleSinceHint": {Value: uint64(0), Emit: prop.EmitTrue, Writable: true},
		}}
		p, err := prop.Export(conn, path, spec)
		if err != nil {
			t.Fatal(err)
		}
		m.props[path] = p
	}
	add("2", 1000, "ilyes", "wayland", "user", "ubuntu", false, true, 4242)
	add("3", 1001, "maram", "x11", "user", "ubuntu-xorg", false, false, 0)
	add("c1", 125, "gdm", "wayland", "greeter", "gnome", false, true, 0)
	add("4", 1000, "ilyes", "tty", "user", "", true, false, 0) // an ssh session

	if err := conn.Export(mgr, logindPath, managerIface); err != nil {
		t.Fatal(err)
	}
	reply, err := conn.RequestName(logindName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("cannot own %s: %v %v", logindName, reply, err)
	}
	return m
}

func (m *mockLogind) set(id, name string, v any) {
	m.props[dbus.ObjectPath("/org/freedesktop/login1/session/_"+id)].SetMust(sessionIface, name, v)
}

func waitEvent(t *testing.T, l *Logind, want EventKind) Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-l.Events():
			if e.Kind == want {
				return e
			}
		case <-deadline:
			t.Fatalf("no event of kind %d", want)
		}
	}
}

func TestLogindListsSessionsWithTypeAndHints(t *testing.T) {
	_, addr := testutil.PrivateBus(t)
	mock := startMockLogind(t, addr)

	client, err := testutil.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	l, err := NewLogind(client)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	ctx := context.Background()
	ss, err := l.Sessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]SessionInfo{}
	for _, s := range ss {
		by[s.ID] = s
	}
	if len(by) != 4 {
		t.Fatalf("sessions = %+v", ss)
	}
	// AC-102: the session type comes from logind
	if by["2"].Type != model.SessionWayland || by["3"].Type != model.SessionX11 || by["4"].Type != model.SessionTTY {
		t.Fatalf("types = %v %v %v", by["2"].Type, by["3"].Type, by["4"].Type)
	}
	if by["2"].User != "ilyes" || by["2"].UID != 1000 || !by["2"].Active || by["2"].Desktop != "ubuntu" {
		t.Fatalf("session 2 = %+v", by["2"])
	}
	cfg := testConfig("ilyes", "maram")
	if !Eligible(by["2"], cfg) || !Eligible(by["3"], cfg) {
		t.Fatal("local user sessions should be eligible")
	}
	if Eligible(by["c1"], cfg) {
		t.Fatal("the greeter must never be tracked (FR-2.9)")
	}
	if Eligible(by["4"], cfg) {
		t.Fatal("a remote (ssh) session must be excluded (FR-2.8)")
	}

	// provider choice: the X11 provider is never used on Wayland (FR-3.9)
	if ProviderFor(by["2"]) != model.ProviderGnomeWayland || ProviderFor(by["3"]) != model.ProviderX11 || ProviderFor(by["4"]) != model.ProviderNone {
		t.Fatalf("providers = %v %v %v", ProviderFor(by["2"]), ProviderFor(by["3"]), ProviderFor(by["4"]))
	}

	// hints change → a wake-up arrives and the new value is visible
	mock.set("2", "LockedHint", true)
	waitEvent(t, l, EventChanged)
	ss, _ = l.Sessions(ctx)
	for _, s := range ss {
		if s.ID == "2" && !s.LockedHint {
			t.Fatal("LockedHint not visible after change")
		}
	}
	since := time.Now().Add(-4 * time.Minute).UnixMicro()
	mock.set("2", "IdleSinceHint", uint64(since))
	mock.set("2", "IdleHint", true)
	waitEvent(t, l, EventChanged)
	ss, _ = l.Sessions(ctx)
	for _, s := range ss {
		if s.ID == "2" && (!s.IdleHint || s.IdleSince.UnixMicro() != since) {
			t.Fatalf("idle hint / since not read: %+v", s)
		}
	}
}

func TestLogindSessionOfPIDAndSleepSignals(t *testing.T) {
	_, addr := testutil.PrivateBus(t)
	mock := startMockLogind(t, addr)
	client, _ := testutil.Dial(addr)
	defer client.Close()
	l, err := NewLogind(client)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	id, err := l.SessionOfPID(context.Background(), 4242)
	if err != nil || id != "2" {
		t.Fatalf("SessionOfPID = %q, %v", id, err)
	}
	if _, err := l.SessionOfPID(context.Background(), 1); err == nil {
		t.Fatal("an unknown pid must not map to a session")
	}

	if err := mock.conn.Emit(logindPath, managerIface+".PrepareForSleep", true); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, l, EventSleep)
	if err := mock.conn.Emit(logindPath, managerIface+".PrepareForSleep", false); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, l, EventResume)
}

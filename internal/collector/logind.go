package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	logindName    = "org.freedesktop.login1"
	logindPath    = dbus.ObjectPath("/org/freedesktop/login1")
	managerIface  = "org.freedesktop.login1.Manager"
	sessionIface  = "org.freedesktop.login1.Session"
	propsIface    = "org.freedesktop.DBus.Properties"
	sessionPrefix = "/org/freedesktop/login1/session"
)

// Logind is the systemd-logind Source (system bus). It needs no privileges.
type Logind struct {
	conn   *dbus.Conn
	events chan Event
	sig    chan *dbus.Signal
	done   chan struct{}
}

// ConnectSystem connects to the system bus and logind.
func ConnectSystem() (*Logind, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect to the system bus: %w", err)
	}
	l, err := NewLogind(conn)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// NewLogind builds a Source on an existing connection (tests use a private bus).
func NewLogind(conn *dbus.Conn) (*Logind, error) {
	l := &Logind{conn: conn, events: make(chan Event, 32), sig: make(chan *dbus.Signal, 64), done: make(chan struct{})}
	if err := conn.AddMatchSignal(
		dbus.WithMatchSender(logindName), dbus.WithMatchObjectPath(logindPath), dbus.WithMatchInterface(managerIface),
	); err != nil {
		return nil, err
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchSender(logindName), dbus.WithMatchInterface(propsIface),
		dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchPathNamespace(dbus.ObjectPath(sessionPrefix)),
	); err != nil {
		return nil, err
	}
	conn.Signal(l.sig)
	go l.loop()
	return l, nil
}

func (l *Logind) loop() {
	for {
		select {
		case <-l.done:
			return
		case s, ok := <-l.sig:
			if !ok {
				return
			}
			var e Event
			switch s.Name {
			case managerIface + ".PrepareForSleep":
				going, _ := firstBool(s.Body)
				e = Event{Kind: EventResume, At: time.Now()}
				if going {
					e.Kind = EventSleep
				}
			case managerIface + ".SessionNew", managerIface + ".SessionRemoved", propsIface + ".PropertiesChanged":
				e = Event{Kind: EventChanged, At: time.Now()}
			default:
				continue
			}
			select {
			case l.events <- e:
			default: // the tracker also polls; a full buffer only delays re-evaluation
			}
		}
	}
}

func firstBool(body []any) (bool, bool) {
	if len(body) == 0 {
		return false, false
	}
	b, ok := body[0].(bool)
	return b, ok
}

// Events implements Source.
func (l *Logind) Events() <-chan Event { return l.events }

// Close stops the signal loop.
func (l *Logind) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	l.conn.RemoveSignal(l.sig)
	return nil // the system bus connection is shared; the caller owns it
}

type listedSession struct {
	ID   string
	UID  uint32
	User string
	Seat string
	Path dbus.ObjectPath
}

// Sessions implements Source.
func (l *Logind) Sessions(ctx context.Context) ([]SessionInfo, error) {
	var list []listedSession
	obj := l.conn.Object(logindName, logindPath)
	if err := obj.CallWithContext(ctx, managerIface+".ListSessions", 0).Store(&list); err != nil {
		return nil, fmt.Errorf("logind ListSessions: %w", err)
	}
	out := make([]SessionInfo, 0, len(list))
	for _, ls := range list {
		var props map[string]dbus.Variant
		err := l.conn.Object(logindName, ls.Path).CallWithContext(ctx, propsIface+".GetAll", 0, sessionIface).Store(&props)
		if err != nil {
			continue // the session ended between the two calls
		}
		out = append(out, sessionFromProps(ls, props))
	}
	return out, nil
}

func sessionFromProps(ls listedSession, p map[string]dbus.Variant) SessionInfo {
	s := SessionInfo{
		ID: ls.ID, UID: int(ls.UID), User: ls.User, Seat: ls.Seat,
		Type:       ParseType(str(p, "Type")),
		Class:      str(p, "Class"),
		Desktop:    str(p, "Desktop"),
		Remote:     boolean(p, "Remote"),
		Active:     boolean(p, "Active"),
		LockedHint: boolean(p, "LockedHint"),
		IdleHint:   boolean(p, "IdleHint"),
		State:      str(p, "State"),
	}
	if name := str(p, "Name"); name != "" {
		s.User = name
	}
	if us := u64(p, "IdleSinceHint"); us > 0 {
		s.IdleSince = time.UnixMicro(int64(us))
	}
	return s
}

// SessionOfPID implements Source.
func (l *Logind) SessionOfPID(ctx context.Context, pid int) (string, error) {
	var path dbus.ObjectPath
	if err := l.conn.Object(logindName, logindPath).CallWithContext(ctx, managerIface+".GetSessionByPID", 0, uint32(pid)).Store(&path); err != nil {
		return "", fmt.Errorf("logind GetSessionByPID(%d): %w", pid, err)
	}
	var v dbus.Variant
	if err := l.conn.Object(logindName, path).CallWithContext(ctx, propsIface+".Get", 0, sessionIface, "Id").Store(&v); err != nil {
		return "", err
	}
	id, _ := v.Value().(string)
	if id == "" {
		return "", fmt.Errorf("logind returned no session id for pid %d", pid)
	}
	return id, nil
}

func str(p map[string]dbus.Variant, k string) string {
	if v, ok := p[k]; ok {
		s, _ := v.Value().(string)
		return s
	}
	return ""
}

func boolean(p map[string]dbus.Variant, k string) bool {
	if v, ok := p[k]; ok {
		b, _ := v.Value().(bool)
		return b
	}
	return false
}

func u64(p map[string]dbus.Variant, k string) uint64 {
	if v, ok := p[k]; ok {
		n, _ := v.Value().(uint64)
		return n
	}
	return 0
}

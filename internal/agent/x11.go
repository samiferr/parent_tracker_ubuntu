package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/appmap"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// X11 reads the active window through EWMH (_NET_ACTIVE_WINDOW), inside the
// user's own X session so DISPLAY and XAUTHORITY are right without any help.
//
// It is only ever started for sessions logind reports as x11 (FR-3.9): under
// Wayland an X11 client only sees XWayland windows, which are not necessarily
// the focused one, so its answer would be silently wrong.
type X11 struct {
	// Display overrides $DISPLAY (tests).
	Display string
	Apps    *appmap.Index

	mu    sync.Mutex
	conn  *xgb.Conn
	root  xproto.Window
	atoms map[string]xproto.Atom
}

// NewX11 creates the provider; it connects on first use.
func NewX11(apps *appmap.Index) *X11 {
	if apps == nil {
		apps = appmap.New()
	}
	return &X11{Apps: apps, atoms: map[string]xproto.Atom{}}
}

// Name implements Provider.
func (x *X11) Name() model.Provider { return model.ProviderX11 }

// Close implements Provider.
func (x *X11) Close() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.drop()
}

func (x *X11) drop() {
	if x.conn != nil {
		x.conn.Close()
		x.conn = nil
	}
	x.atoms = map[string]xproto.Atom{}
}

func (x *X11) connect() error {
	if x.conn != nil {
		return nil
	}
	display := x.Display
	if display == "" {
		display = os.Getenv("DISPLAY")
	}
	if display == "" {
		return fmt.Errorf("DISPLAY is not set")
	}
	c, err := xgb.NewConnDisplay(display)
	if err != nil {
		return err
	}
	x.conn = c
	x.root = xproto.Setup(c).DefaultScreen(c).Root
	return nil
}

func (x *X11) atom(name string) (xproto.Atom, error) {
	if a, ok := x.atoms[name]; ok {
		return a, nil
	}
	r, err := xproto.InternAtom(x.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	x.atoms[name] = r.Atom
	return r.Atom, nil
}

func (x *X11) prop(w xproto.Window, name string) (*xproto.GetPropertyReply, error) {
	a, err := x.atom(name)
	if err != nil {
		return nil, err
	}
	return xproto.GetProperty(x.conn, false, w, a, xproto.GetPropertyTypeAny, 0, 1024).Reply()
}

func (x *X11) unreachable(detail string) agentproto.Report {
	return agentproto.Report{Type: "report", Provider: string(model.ProviderX11), Status: agentproto.StatusUnreachable, Detail: detail}
}

// Poll implements Provider.
func (x *X11) Poll(_ context.Context, titles bool) agentproto.Report {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.connect(); err != nil {
		return x.unreachable(err.Error())
	}
	rep, err := x.read(titles)
	if err != nil {
		x.drop() // reconnect on the next poll
		return x.unreachable(err.Error())
	}
	return rep
}

func (x *X11) read(titles bool) (agentproto.Report, error) {
	rep := agentproto.Report{Type: "report", Provider: string(model.ProviderX11), Status: agentproto.StatusOK}

	// A window manager that does not advertise _NET_ACTIVE_WINDOW cannot tell us
	// what is focused; say so rather than reporting "nothing".
	sup, err := x.prop(x.root, "_NET_SUPPORTED")
	if err != nil {
		return rep, err
	}
	ok, err := x.supports(sup, "_NET_ACTIVE_WINDOW")
	if err != nil {
		return rep, err
	}
	if !ok {
		rep.Status, rep.Detail = agentproto.StatusUnreachable, "the window manager does not support _NET_ACTIVE_WINDOW"
		return rep, nil
	}

	ap, err := x.prop(x.root, "_NET_ACTIVE_WINDOW")
	if err != nil {
		return rep, err
	}
	if ap.Format != 32 || len(ap.Value) < 4 {
		return rep, nil // nothing focused
	}
	win := xproto.Window(xgb.Get32(ap.Value))
	if win == 0 {
		return rep, nil
	}

	var (
		gtkID, class, instance, title string
		pid                           int
	)
	if p, err := x.prop(win, "_GTK_APPLICATION_ID"); err == nil {
		gtkID = strings.TrimRight(string(p.Value), "\x00")
	}
	if p, err := x.prop(win, "WM_CLASS"); err == nil {
		parts := strings.Split(strings.TrimRight(string(p.Value), "\x00"), "\x00")
		if len(parts) > 0 {
			instance = parts[0]
		}
		if len(parts) > 1 {
			class = parts[1]
		}
	}
	if p, err := x.prop(win, "_NET_WM_PID"); err == nil && p.Format == 32 && len(p.Value) >= 4 {
		pid = int(xgb.Get32(p.Value))
	}
	if titles {
		if p, err := x.prop(win, "_NET_WM_NAME"); err == nil && len(p.Value) > 0 {
			title = string(p.Value)
		} else if p, err := x.prop(win, "WM_NAME"); err == nil {
			title = string(p.Value)
		}
	}
	if gtkID == "" && class == "" && instance == "" && pid == 0 {
		return rep, nil // a window we cannot name (destroyed meanwhile, or an override-redirect popup)
	}
	id, name := x.Apps.Resolve(gtkID, class, instance, exeName(pid))
	if id == "" {
		return rep, nil
	}
	rep.App = &agentproto.App{ID: id, Name: name, PID: pid}
	rep.Title = title
	return rep, nil
}

func (x *X11) supports(p *xproto.GetPropertyReply, name string) (bool, error) {
	want, err := x.atom(name)
	if err != nil || p.Format != 32 {
		return false, err
	}
	for i := 0; i+4 <= len(p.Value); i += 4 {
		if xproto.Atom(xgb.Get32(p.Value[i:])) == want {
			return true, nil
		}
	}
	return false, nil
}

// exeName is the executable's base name, read from /proc (the agent runs as the
// same user, so it may); "" when unknown.
func exeName(pid int) string {
	if pid <= 0 {
		return ""
	}
	p, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	return filepath.Base(strings.TrimSuffix(p, " (deleted)"))
}

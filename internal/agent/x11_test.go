package agent

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/appmap"
)

// startXvfb starts a throwaway X server on a display of its own choosing
// (-displayfd, so parallel packages and stale sockets cannot collide) and
// returns the display name.
func startXvfb(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("Xvfb")
	if err != nil {
		t.Skip("Xvfb not installed")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path, "-displayfd", "3", "-screen", "0", "640x480x24", "-nolisten", "tcp")
	cmd.ExtraFiles = []*os.File{w}
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start Xvfb:", err)
	}
	w.Close()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM) // lets Xvfb remove its socket and lock
		_ = cmd.Wait()
		r.Close()
	})
	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(r).ReadString('\n')
		line <- strings.TrimSpace(s)
	}()
	select {
	case n := <-line:
		if n == "" {
			t.Skip("Xvfb did not report a display")
		}
		return ":" + n
	case <-time.After(10 * time.Second):
		t.Skip("Xvfb did not come up")
	}
	return ""
}

// xclient lets a test play both the window manager and an application.
type xclient struct {
	t    *testing.T
	c    *xgb.Conn
	root xproto.Window
}

func newXClient(t *testing.T, display string) *xclient {
	c, err := xgb.NewConnDisplay(display)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return &xclient{t: t, c: c, root: xproto.Setup(c).DefaultScreen(c).Root}
}

func (x *xclient) atom(name string) xproto.Atom {
	r, err := xproto.InternAtom(x.c, false, uint16(len(name)), name).Reply()
	if err != nil {
		x.t.Fatal(err)
	}
	return r.Atom
}

func (x *xclient) set(w xproto.Window, name, typ string, format byte, data []byte) {
	n := uint32(len(data))
	if format == 32 {
		n /= 4
	}
	if err := xproto.ChangePropertyChecked(x.c, xproto.PropModeReplace, w, x.atom(name), x.atom(typ), format, n, data).Check(); err != nil {
		x.t.Fatal(err)
	}
}

func u32(v uint32) []byte { b := make([]byte, 4); xgb.Put32(b, v); return b }

func (x *xclient) window(class, gtkID, title string, pid uint32) xproto.Window {
	w, err := xproto.NewWindowId(x.c)
	if err != nil {
		x.t.Fatal(err)
	}
	if err := xproto.CreateWindowChecked(x.c, 0, w, x.root, 0, 0, 10, 10, 0, xproto.WindowClassInputOutput, 0, 0, nil).Check(); err != nil {
		x.t.Fatal(err)
	}
	x.set(w, "WM_CLASS", "STRING", 8, []byte("inst\x00"+class+"\x00"))
	if gtkID != "" {
		x.set(w, "_GTK_APPLICATION_ID", "UTF8_STRING", 8, []byte(gtkID))
	}
	x.set(w, "_NET_WM_NAME", "UTF8_STRING", 8, []byte(title))
	x.set(w, "_NET_WM_PID", "CARDINAL", 32, u32(pid))
	return w
}

func (x *xclient) focus(w xproto.Window) {
	x.set(x.root, "_NET_ACTIVE_WINDOW", "WINDOW", 32, u32(uint32(w)))
}

func (x *xclient) advertiseEWMH() {
	x.set(x.root, "_NET_SUPPORTED", "ATOM", 32, u32(uint32(x.atom("_NET_ACTIVE_WINDOW"))))
}

func appIndex(t *testing.T) *appmap.Index {
	dir := t.TempDir()
	os.WriteFile(dir+"/firefox.desktop", []byte("[Desktop Entry]\nType=Application\nName=Firefox Web Browser\nExec=firefox %u\nStartupWMClass=Navigator\n"), 0o644)
	os.WriteFile(dir+"/org.gnome.Nautilus.desktop", []byte("[Desktop Entry]\nType=Application\nName=Files\nExec=nautilus\n"), 0o644)
	return appmap.New(dir)
}

// AC-101 / FR-3.1: the X11 provider reads the active window, maps it to an
// application, and reads the title only when allowed.
func TestX11ReadsTheActiveWindow(t *testing.T) {
	display := startXvfb(t)
	x := newXClient(t, display)
	p := NewX11(appIndex(t))
	p.Display = display
	defer p.Close()
	ctx := context.Background()

	// no window manager yet: say so rather than guess (FR-3.11)
	rep := p.Poll(ctx, false)
	if rep.Status != agentproto.StatusNoEWMH || rep.Provider != "x11" || rep.Detail == "" {
		t.Fatalf("without EWMH: %+v", rep)
	}

	x.advertiseEWMH()
	rep = p.Poll(ctx, false)
	if rep.Status != agentproto.StatusOK || rep.App != nil {
		t.Fatalf("nothing focused: %+v", rep)
	}

	ff := x.window("Navigator", "", "Bank statement — Mozilla Firefox", uint32(os.Getpid()))
	x.focus(ff)
	rep = p.Poll(ctx, false)
	if rep.Status != agentproto.StatusOK || rep.App == nil || rep.App.ID != "firefox" || rep.App.Name != "Firefox Web Browser" || rep.App.PID != os.Getpid() {
		t.Fatalf("firefox: %+v", rep)
	}
	if rep.Title != "" {
		t.Fatal("the title must not be read while capture is off (SEC-17)")
	}
	rep = p.Poll(ctx, true)
	if rep.Title != "Bank statement — Mozilla Firefox" {
		t.Fatalf("title = %q", rep.Title)
	}

	files := x.window("Org.gnome.Nautilus", "org.gnome.Nautilus", "Home", 1)
	x.focus(files)
	rep = p.Poll(ctx, false)
	if rep.App == nil || rep.App.ID != "org.gnome.nautilus" || rep.App.Name != "Files" {
		t.Fatalf("nautilus via _GTK_APPLICATION_ID: %+v", rep)
	}

	unknown := x.window("SomeGame", "", "Level 3", 1)
	x.focus(unknown)
	rep = p.Poll(ctx, false)
	if rep.App == nil || rep.App.ID != "somegame" {
		t.Fatalf("an app without a desktop entry must still be tracked: %+v", rep)
	}

	x.focus(0)
	if rep = p.Poll(ctx, false); rep.Status != agentproto.StatusOK || rep.App != nil {
		t.Fatalf("focus cleared: %+v", rep)
	}
}

func TestX11ReconnectsAfterTheServerGoes(t *testing.T) {
	display := startXvfb(t)
	newXClient(t, display).advertiseEWMH()
	p := NewX11(appIndex(t))
	p.Display = display
	defer p.Close()
	if rep := p.Poll(context.Background(), false); rep.Status != agentproto.StatusOK {
		t.Fatalf("%+v", rep)
	}
	p.Display = ":119" // unreachable
	p.mu.Lock()
	p.drop()
	p.mu.Unlock()
	rep := p.Poll(context.Background(), false)
	if rep.Status != agentproto.StatusUnreachable || rep.Detail == "" {
		t.Fatalf("%+v", rep)
	}
}

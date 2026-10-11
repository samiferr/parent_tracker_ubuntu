package agentsrv

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/peercred"
)

type rig struct {
	t    *testing.T
	srv  *Server
	src  *collector.Fake
	cfgp atomic.Pointer[config.Config] // swapped like the daemon does on reload
	path string
	uid  atomic.Int64
}

// setCfg replaces the config with a modified copy, as a reload does.
func (r *rig) setCfg(fn func(*config.Config)) {
	c := r.cfgp.Load().Clone()
	fn(c)
	r.cfgp.Store(c)
}

func newRig(t *testing.T, uid int) *rig {
	t.Helper()
	r := &rig{t: t, src: collector.NewFake()}
	r.uid.Store(int64(uid))
	c := config.Default()
	c.MonitoredUsers = []string{"ilyes"}
	c.DisclosureAcknowledged = "2026-10-01T00:00:00Z"
	r.cfgp.Store(c)
	r.srv = &Server{Src: r.src, Cfg: r.cfgp.Load,
		Cred: func(net.Conn) (peercred.Cred, error) { return peercred.Cred{UID: int(r.uid.Load()), PID: 4242}, nil }}
	r.path = filepath.Join(t.TempDir(), "agent.sock")
	ln, err := peercred.Listen(r.path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go r.srv.Serve(ctx, ln)
	return r
}

type client struct {
	c  net.Conn
	sc *bufio.Scanner
}

func (r *rig) dial(hello agentproto.Hello) (*client, agentproto.Welcome) {
	r.t.Helper()
	c, err := net.Dial("unix", r.path)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { c.Close() })
	hello.Type, hello.Version = "hello", agentproto.Version
	json.NewEncoder(c).Encode(hello)
	cl := &client{c: c, sc: bufio.NewScanner(c)}
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if !cl.sc.Scan() {
		r.t.Fatal("no welcome")
	}
	var w agentproto.Welcome
	json.Unmarshal(cl.sc.Bytes(), &w)
	return cl, w
}

func (c *client) send(v any) { json.NewEncoder(c.c).Encode(v) }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestSocketAcceptsAnyoneButTheDaemonDecides(t *testing.T) {
	r := newRig(t, 1000)
	st, err := peercredStat(r.path)
	if err != nil || st != 0o666 {
		t.Fatalf("socket mode = %o, %v", st, err)
	}
}

func TestMonitoredUserGetsTheRightProvider(t *testing.T) {
	r := newRig(t, 1000)
	r.src.Set(collector.UserSession("2", 1000, "ilyes", "wayland", "ubuntu"))
	cl, w := r.dial(agentproto.Hello{SessionType: "wayland"})
	if !w.Monitored || w.SessionID != "2" || w.Provider != "gnome-wayland" || w.Title || w.IntervalMs != 5000 {
		t.Fatalf("welcome = %+v", w)
	}
	cl.send(agentproto.Report{Type: "report", Provider: "gnome-wayland", Status: "ok", App: &agentproto.App{ID: "Firefox", Name: "Firefox", PID: 7}, Title: "secret"})
	waitFor(t, func() bool { _, _, ok := r.srv.Latest("2"); return ok })
	rep, recv, _ := r.srv.Latest("2")
	if rep.App.ID != "firefox" || rep.Title != "" || time.Since(recv) > time.Second {
		t.Fatalf("report = %+v (titles are dropped while capture is off; ids are lower-cased)", rep)
	}
	if !r.srv.Connected("2") {
		t.Fatal("should be connected")
	}
}

// SEC-13, AC-20: an unmonitored user, a user without a graphical session, and
// the case where recording is not enabled are all refused and collect nothing.
func TestRefusals(t *testing.T) {
	r := newRig(t, 1001) // a different local user
	r.src.Set(collector.UserSession("2", 1000, "ilyes", "wayland", "ubuntu"))
	if _, w := r.dial(agentproto.Hello{}); w.Monitored {
		t.Fatalf("a user who is not on the allowlist must be refused: %+v", w)
	}

	r.uid.Store(1000)
	r.src.Remove("2")
	r.src.Set(collector.UserSession("5", 1000, "ilyes", "tty", ""))
	if _, w := r.dial(agentproto.Hello{}); w.Monitored || w.Reason != "no graphical session" {
		t.Fatalf("tty only: %+v", w)
	}

	r.src.Set(collector.UserSession("2", 1000, "ilyes", "wayland", "ubuntu"))
	r.setCfg(func(c *config.Config) { c.DisclosureAcknowledged = "" })
	if _, w := r.dial(agentproto.Hello{}); w.Monitored {
		t.Fatalf("before the disclosure is acknowledged nothing is collected: %+v", w)
	}
}

// FR-3.9: a report for the wrong provider is dropped, so an agent cannot feed
// X11 data into a Wayland session.
func TestWrongProviderReportsAreIgnored(t *testing.T) {
	r := newRig(t, 1000)
	r.src.Set(collector.UserSession("2", 1000, "ilyes", "wayland", "ubuntu"))
	cl, w := r.dial(agentproto.Hello{})
	if w.Provider != "gnome-wayland" {
		t.Fatalf("welcome = %+v", w)
	}
	cl.send(agentproto.Report{Type: "report", Provider: "x11", Status: "ok", App: &agentproto.App{ID: "xclock"}})
	time.Sleep(150 * time.Millisecond)
	if _, _, ok := r.srv.Latest("2"); ok {
		t.Fatal("an x11 report on a Wayland session must be ignored")
	}
}

// With two graphical sessions the agent's hint picks one, but only among that
// user's own sessions.
func TestSessionHintCannotReachAnotherUsersSession(t *testing.T) {
	r := newRig(t, 1000)
	a := collector.UserSession("2", 1000, "ilyes", "wayland", "ubuntu")
	a.Active = false
	b := collector.UserSession("4", 1000, "ilyes", "x11", "ubuntu-xorg")
	other := collector.UserSession("3", 1001, "maram", "x11", "ubuntu-xorg")
	r.src.Set(a)
	r.src.Set(b)
	r.src.Set(other)
	r.setCfg(func(c *config.Config) { c.MonitoredUsers = []string{"ilyes", "maram"} })

	if _, w := r.dial(agentproto.Hello{SessionHint: "2"}); w.SessionID != "2" {
		t.Fatalf("hint for own session: %+v", w)
	}
	if _, w := r.dial(agentproto.Hello{SessionHint: "3"}); w.SessionID == "3" {
		t.Fatalf("hint for another user's session was honoured: %+v", w)
	}
	if _, w := r.dial(agentproto.Hello{}); w.SessionID != "4" {
		t.Fatalf("without a hint the active session wins: %+v", w)
	}
	// logind's own pid mapping beats the hint
	r.src.MapPID(4242, "2")
	if _, w := r.dial(agentproto.Hello{SessionHint: "4"}); w.SessionID != "2" {
		t.Fatalf("pid mapping: %+v", w)
	}
}

func TestReconfigurePushesNewSettings(t *testing.T) {
	r := newRig(t, 1000)
	r.src.Set(collector.UserSession("2", 1000, "ilyes", "x11", "ubuntu-xorg"))
	cl, w := r.dial(agentproto.Hello{})
	if w.Title {
		t.Fatal("titles should start off")
	}
	r.setCfg(func(c *config.Config) {
		c.Collectors.WindowTitle = true
		c.SampleInterval = config.Duration(10 * time.Second)
	})
	waitFor(t, func() bool { return r.srv.Connected("2") })
	r.srv.Reconfigure(context.Background())
	cl.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if !cl.sc.Scan() {
		t.Fatal("no config pushed")
	}
	var c agentproto.Config
	json.Unmarshal(cl.sc.Bytes(), &c)
	if c.Type != "config" || !c.Title || c.IntervalMs != 10000 || c.Provider != "x11" {
		t.Fatalf("config = %+v", c)
	}
}

func TestOversizedOrGarbledInputIsHarmless(t *testing.T) {
	r := newRig(t, 1000)
	r.src.Set(collector.UserSession("2", 1000, "ilyes", "x11", "ubuntu-xorg"))
	cl, _ := r.dial(agentproto.Hello{})
	cl.c.Write([]byte("not json\n"))
	cl.c.Write([]byte(string(make([]byte, 0)) + "{\"type\":\"report\",\"provider\":\"x11\",\"status\":\"ok\"}\n"))
	waitFor(t, func() bool { _, _, ok := r.srv.Latest("2"); return ok })
	big := make([]byte, agentproto.MaxLine+10)
	for i := range big {
		big[i] = 'a'
	}
	cl.c.Write(append(big, '\n'))
	time.Sleep(100 * time.Millisecond) // the connection is dropped; the daemon keeps running
	if _, _, ok := r.srv.Latest("2"); !ok {
		t.Fatal("earlier report should remain available")
	}
}

func TestClipKeepsUTF8Valid(t *testing.T) {
	s := clip("héllo wörld", 2)
	if s != "h" {
		t.Fatalf("clip = %q", s)
	}
}

func peercredStat(p string) (uint32, error) {
	st, err := osStat(p)
	if err != nil {
		return 0, err
	}
	return uint32(st.Mode().Perm()), nil
}

var osStat = os.Stat

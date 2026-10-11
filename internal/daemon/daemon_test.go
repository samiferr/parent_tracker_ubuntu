package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

type env struct {
	t       *testing.T
	dir     string
	cfgPath string
	src     *collector.Fake
	d       *Daemon
	cancel  context.CancelFunc
	done    chan error
	base    string
	client  *http.Client
}

func writeConfig(t *testing.T, dir string, mutate func(*config.Config)) string {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw-for-tests"), bcrypt.MinCost)
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.ControlSocket = filepath.Join(dir, "control.sock")
	cfg.AgentSocket = filepath.Join(dir, "agent.sock")
	cfg.MonitoredUsers = []string{"ilyes"}
	cfg.DisclosureAcknowledged = "2026-10-01T00:00:00Z"
	cfg.SampleInterval = config.Duration(time.Second)
	cfg.Server.Listen = "127.0.0.1:0"
	cfg.Server.PasswordHash = string(hash)
	if mutate != nil {
		mutate(cfg)
	}
	p := filepath.Join(dir, "config.toml")
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func start(t *testing.T, dir string, src *collector.Fake) *env {
	t.Helper()
	e := &env{t: t, dir: dir, src: src}
	if dir == "" {
		e.dir = t.TempDir()
		e.cfgPath = writeConfig(t, e.dir, nil)
	} else {
		e.cfgPath = filepath.Join(dir, "config.toml")
	}
	e.boot()
	return e
}

func (e *env) boot() {
	e.t.Helper()
	ready := make(chan string, 1)
	d, err := New(Options{ConfigPath: e.cfgPath, Version: "test", Source: e.src, Loc: time.UTC, Ready: func(a string) { ready <- a }})
	if err != nil {
		e.t.Fatal(err)
	}
	e.d = d
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan error, 1)
	go func() { e.done <- d.Run(ctx) }()
	select {
	case addr := <-ready:
		e.base = "http://" + addr
	case err := <-e.done:
		e.t.Fatalf("daemon exited early: %v", err)
	case <-time.After(10 * time.Second):
		e.t.Fatal("daemon did not start")
	}
	jar, _ := cookiejar.New(nil)
	e.client = &http.Client{Jar: jar}
}

func (e *env) stop() {
	e.t.Helper()
	e.cancel()
	select {
	case err := <-e.done:
		if err != nil {
			e.t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		e.t.Fatal("daemon did not shut down")
	}
}

func (e *env) req(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (e *env) login() {
	e.t.Helper()
	if code, b := e.req("POST", "/api/login", map[string]string{"username": "parent", "password": "pw-for-tests"}); code != 200 {
		e.t.Fatalf("login %d %s", code, b)
	}
}

func eventually(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func liveState(e *env) string {
	_, b := e.req("GET", "/api/live?user=ilyes", nil)
	var s struct {
		Available bool
		State     string
	}
	json.Unmarshal(b, &s)
	return s.State
}

func TestDaemonEndToEnd(t *testing.T) {
	src := collector.NewFake()
	src.Set(collector.UserSession("2", os.Getuid(), "ilyes", "wayland", "ubuntu"))
	e := start(t, "", src)
	e.login()

	// the daemon's first tick records the session and publishes it live
	eventually(t, "the Active state", 5*time.Second, func() bool { return liveState(e) == "active" })

	// AC-80: a lock shows up on an open stream without a reload
	req, _ := http.NewRequest("GET", e.base+"/api/live/stream?user=ilyes", nil)
	resp, err := e.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("stream: %v %v", resp, err)
	}
	defer resp.Body.Close()
	events := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				events <- strings.TrimPrefix(sc.Text(), "data: ")
			}
		}
		close(events)
	}()
	src.Update("2", func(s *collector.SessionInfo) { s.LockedHint = true })
	src.Emit(collector.Event{Kind: collector.EventChanged, At: time.Now()})
	locked := false
	deadline := time.After(5 * time.Second)
	for !locked {
		select {
		case ev := <-events:
			locked = strings.Contains(ev, `"state":"locked"`)
		case <-deadline:
			t.Fatal("the lock never reached the open stream")
		}
	}

	// the child's self-view over the control socket (root asks for ilyes). The
	// control server resolves callers through /etc/passwd, and "ilyes" is not a
	// real account, so only root may ask on behalf of another user. The rule that
	// a normal user sees only their own data is tested in internal/control.
	if os.Getuid() == 0 {
		c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", e.d.Config().ControlSocket)
		}}}
		r, err := c.Get("http://control/self?user=ilyes")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 || !strings.Contains(string(body), `"parent_sees_live":true`) {
			t.Fatalf("self-view: %d %s", r.StatusCode, body)
		}
	}

	// SIGHUP-style reload: turning the live view off closes the stream (AC-87)
	cfg, _ := config.Load(e.cfgPath)
	cfg.Server.Live = false
	cfg.Collectors.WindowTitle = true
	cfg.MonitoredUsers = []string{"ilyes", "maram"}
	if err := cfg.Save(e.cfgPath); err != nil {
		t.Fatal(err)
	}
	if err := e.d.Reload(); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.req("GET", "/api/live?user=ilyes", nil); code != 404 {
		t.Fatalf("/api/live with the live view off = %d", code)
	}
	sawBye := false
	timeout := time.After(5 * time.Second)
drain:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break drain // the server ended the stream
			}
			sawBye = sawBye || strings.Contains(ev, "live view turned off")
		case <-timeout:
			t.Fatal("the stream stayed open after the live view was turned off")
		}
	}
	if !sawBye {
		t.Fatal("the stream must end with a final bye event (AC-87)")
	}
	ev, _ := e.d.DB().AuditEvents(0, time.Now().Add(time.Hour).UnixMilli(), 50)
	var users, title bool
	for _, a := range ev {
		users = users || (a.Action == model.AuditUsersChanged && strings.Contains(a.Detail, "added: maram"))
		title = title || strings.Contains(a.Detail, "collectors.window_title: false -> true")
	}
	if !users || !title {
		t.Fatalf("a reload must audit user and collector changes (SEC-14): %+v", ev)
	}

	// an invalid file is refused and the old settings stay in force
	os.WriteFile(e.cfgPath, []byte("sample_intervall = 1\n"), 0o640)
	if err := e.d.Reload(); err == nil {
		t.Fatal("a broken config must not be applied")
	}
	if !e.d.Config().Collectors.WindowTitle {
		t.Fatal("the previous settings should remain")
	}
	cfg.Server.Live = true
	cfg.Save(e.cfgPath)

	// AC-53: stopping leaves everything closed and the next start shows a gap
	e.stop()
	db, err := storage.Open(e.d.Config().DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if open, _ := db.LoadOpenIntervals(); len(open) != 0 {
		t.Fatalf("intervals left open: %+v", open)
	}
	if v, _, _ := db.GetState("clean_shutdown"); v != "1" {
		t.Fatalf("clean_shutdown = %q", v)
	}
	db.Close()

	time.Sleep(1200 * time.Millisecond) // the service is down for a moment
	e2 := &env{t: t, dir: e.dir, cfgPath: e.cfgPath, src: src}
	e2.boot()
	defer e2.stop()
	e2.login()
	eventually(t, "the tracker to come back", 5*time.Second, func() bool { return liveState(e2) == "locked" })
	gaps, _ := e2.d.DB().Gaps(0, 0, time.Now().Add(time.Hour).UnixMilli())
	if len(gaps) != 1 || gaps[0].Kind != model.GapServiceStopped {
		t.Fatalf("gaps after a clean stop = %+v", gaps)
	}
}

func TestDashboardOnlyMode(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, nil)
	ready := make(chan string, 1)
	d, err := New(Options{ConfigPath: cfgPath, Version: "test", NoCollectors: true, Loc: time.UTC, Ready: func(a string) { ready <- a }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	addr := <-ready
	resp, err := http.Get("http://" + addr + "/api/session")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
	resp.Body.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPortInUseIsAClearError(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, func(c *config.Config) { c.Server.Listen = ln.Addr().String() })
	d, err := New(Options{ConfigPath: cfgPath, Version: "test", NoCollectors: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "cannot listen on") {
		t.Fatalf("err = %v", err)
	}
}

func TestDiffSnapshots(t *testing.T) {
	a, b := config.Default(), config.Default()
	b.MonitoredUsers = []string{"ilyes"}
	b.Collectors.App = false
	b.Collectors.WindowTitle = true
	b.Alerts.NtfyURL = "https://ntfy.sh/very-private"
	b.Server.PasswordHash = "$2a$new"
	b.SampleInterval = config.Duration(10 * time.Second)
	got := map[string]string{}
	var all []string
	for _, c := range DiffSnapshots(Snapshot(a), Snapshot(b)) {
		got[c.Action] += c.Detail + "|"
		all = append(all, c.Detail)
	}
	joined := strings.Join(all, "\n")
	if !strings.Contains(got[model.AuditUsersChanged], "added: ilyes; removed: none") ||
		!strings.Contains(got[model.AuditCollectorDisabled], "collectors.app: true -> false") ||
		!strings.Contains(joined, "collectors.window_title: false -> true") ||
		!strings.Contains(joined, "sample_interval: 5s -> 10s") ||
		!strings.Contains(joined, "dashboard password changed") {
		t.Fatalf("diff = %s", joined)
	}
	if strings.Contains(joined, "very-private") || strings.Contains(joined, "$2a$new") {
		t.Fatalf("a secret leaked into the audit detail: %s", joined)
	}
	if len(DiffSnapshots(Snapshot(a), Snapshot(a.Clone()))) != 0 {
		t.Fatal("identical configs must produce no changes")
	}
}

// Settings changed while the service was stopped are audited at the next start.
func TestChangesMadeWhileStoppedAreAudited(t *testing.T) {
	src := collector.NewFake()
	e := start(t, "", src)
	e.stop()
	cfg, _ := config.Load(e.cfgPath)
	cfg.MonitoredUsers = []string{"ilyes", "maram"}
	cfg.Collectors.App = false
	if err := cfg.Save(e.cfgPath); err != nil {
		t.Fatal(err)
	}
	e2 := &env{t: t, dir: e.dir, cfgPath: e.cfgPath, src: src}
	e2.boot()
	defer e2.stop()
	ev, _ := e2.d.DB().AuditEvents(0, time.Now().Add(time.Hour).UnixMilli(), 50)
	var users, off bool
	for _, a := range ev {
		users = users || (a.Action == model.AuditUsersChanged && strings.Contains(a.Detail, "added: maram"))
		off = off || (a.Action == model.AuditCollectorDisabled && strings.Contains(a.Detail, "collectors.app"))
	}
	if !users || !off {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestStatusLine(t *testing.T) {
	cfg := config.Default()
	if got := statusLine(cfg); !strings.HasPrefix(got, "Not recording: the disclosure") {
		t.Errorf("unacknowledged: %q", got)
	}
	cfg.DisclosureAcknowledged = "2026-10-11T10:00:00Z"
	if got := statusLine(cfg); !strings.HasPrefix(got, "Not recording: no users") {
		t.Errorf("no users: %q", got)
	}
	cfg.MonitoredUsers = []string{"ben"}
	if got := statusLine(cfg); got != "Recording 1 monitored user" {
		t.Errorf("one user: %q", got)
	}
	cfg.MonitoredUsers = []string{"ben", "cleo"}
	if got := statusLine(cfg); got != "Recording 2 monitored users" {
		t.Errorf("two users: %q", got)
	}
	cfg.Collectors.Session = false
	if got := statusLine(cfg); !strings.Contains(got, "session collector is turned off") {
		t.Errorf("collector off: %q", got)
	}
}

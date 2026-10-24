// Package daemon wires the collectors, storage, rollups, alerts and the
// dashboard into the parent-trackerd process (docs/04 §4.2).
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/user"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentsrv"
	"github.com/samiferr/parent_tracker_ubuntu/internal/alert"
	"github.com/samiferr/parent_tracker_ubuntu/internal/auditlog"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/control"
	"github.com/samiferr/parent_tracker_ubuntu/internal/live"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/peercred"
	"github.com/samiferr/parent_tracker_ubuntu/internal/retention"
	"github.com/samiferr/parent_tracker_ubuntu/internal/rollup"
	"github.com/samiferr/parent_tracker_ubuntu/internal/server"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
)

// rollupEvery is how often the hourly and daily aggregates are recomputed.
const rollupEvery = 5 * time.Minute

// Options configures a daemon. Only ConfigPath (or Config) and Version are
// needed in production; the rest lets tests and demo mode substitute parts.
type Options struct {
	ConfigPath string
	Config     *config.Config // used instead of reading ConfigPath when set
	Version    string
	Log        *slog.Logger
	LogLevel   *slog.LevelVar

	Source collector.Source  // nil: systemd-logind
	Agents tracker.AgentFeed // nil: the real agent socket
	Now    func() time.Time
	Loc    *time.Location

	// NoCollectors serves the dashboard from the existing database only
	// (`parent-tracker serve`).
	NoCollectors bool
	// NoSockets skips the agent and control sockets (demo mode, tests).
	NoSockets bool
	Demo      bool
	// Ready is called with the dashboard's address once it is listening.
	Ready func(addr string)
}

// Daemon is a running parent-trackerd.
type Daemon struct {
	o   Options
	cfg atomic.Pointer[config.Config]
	log *slog.Logger

	db     *storage.DB
	hub    *live.Hub
	trk    *tracker.Tracker
	agents *agentsrv.Server
	alerts *alert.Engine
	roll   *rollup.Rollup
	src    collector.Source

	reloadMu sync.Mutex
	listen   string
	addr     atomic.Value // string
}

// New loads the configuration and opens the database.
func New(o Options) (*Daemon, error) {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Loc == nil {
		o.Loc = time.Local
	}
	cfg := o.Config
	if cfg == nil {
		var err error
		if cfg, err = config.Load(o.ConfigPath); err != nil {
			return nil, err
		}
	}
	d := &Daemon{o: o, log: o.Log}
	d.cfg.Store(cfg)
	setLevel(o.LogLevel, cfg.LogLevel)
	db, err := storage.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open the database: %w", err)
	}
	d.db = db
	// warnings and errors also go to the audit log, for the dashboard's Audit log
	d.log = slog.New(auditlog.Wrap(o.Log.Handler(), db, o.Now))
	d.hub = live.New(cfg.Server.LiveMaxClients, cfg.Server.Live)
	d.listen = cfg.Server.Listen
	return d, nil
}

// Config returns the current configuration.
func (d *Daemon) Config() *config.Config { return d.cfg.Load() }

// DB exposes the database (demo seeding, tests).
func (d *Daemon) DB() *storage.DB { return d.db }

// Hub exposes the live hub.
func (d *Daemon) Hub() *live.Hub { return d.hub }

// Addr is the dashboard's listen address, once known.
func (d *Daemon) Addr() string {
	a, _ := d.addr.Load().(string)
	return a
}

func setLevel(lv *slog.LevelVar, name string) {
	if lv == nil {
		return
	}
	switch name {
	case "debug":
		lv.Set(slog.LevelDebug)
	case "warn":
		lv.Set(slog.LevelWarn)
	case "error":
		lv.Set(slog.LevelError)
	default:
		lv.Set(slog.LevelInfo)
	}
}

func lookupUser(name string) (int, bool) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, false
	}
	uid := 0
	fmt.Sscanf(u.Uid, "%d", &uid)
	return uid, true
}

// Run starts everything and blocks until ctx is cancelled, then shuts down
// cleanly: open intervals are closed and the stop is recorded, so the next start
// can show it as a tracking gap (SEC-30, AC-53).
func (d *Daemon) Run(ctx context.Context) error {
	cfg := d.Config()
	var wg sync.WaitGroup
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	d.roll = &rollup.Rollup{DB: d.db, Cfg: d.Config, Loc: d.o.Loc, Now: d.o.Now}
	d.alerts = alert.New(&alert.Engine{DB: d.db, Cfg: d.Config, Loc: d.o.Loc, Now: d.o.Now, Log: d.log})

	var healthFn func() tracker.Health
	if !d.o.NoCollectors {
		src := d.o.Source
		if src == nil {
			l, err := collector.ConnectSystem()
			if err != nil {
				return fmt.Errorf("systemd-logind is not reachable: %w", err)
			}
			src = l
		}
		d.src = src
		var feed tracker.AgentFeed = d.o.Agents
		if feed == nil && !d.o.NoSockets {
			d.agents = &agentsrv.Server{Src: src, Cfg: d.Config, Log: d.log}
			feed = d.agents
		}
		d.alerts.Sessions = src.Sessions
		d.trk = tracker.New(tracker.Options{Cfg: d.Config, Source: src, DB: d.db, Agents: feed, Hub: d.hub, Loc: d.o.Loc,
			Log: d.log, Now: d.o.Now, Notify: d.alerts.Notify})
		if err := d.trk.SyncUsers(lookupUser); err != nil {
			return err
		}
		d.auditChanges(cfg, true) // settings changed while the service was stopped
		if err := d.trk.Recover(); err != nil {
			return fmt.Errorf("recover from the previous run: %w", err)
		}
		healthFn = d.trk.Health
	}

	// dashboard
	var httpSrv *http.Server
	if cfg.Server.Enabled {
		if cfg.Server.PasswordHash == "" {
			d.log.Warn("the dashboard login is not set up; run `sudo parent-tracker setup`")
		}
		if !config.IsLoopbackHost(hostOf(cfg.Server.Listen)) {
			d.log.Warn("the dashboard is reachable beyond this PC; use a strong password and restrict the port to your LAN", "listen", cfg.Server.Listen)
		}
		ln, err := net.Listen("tcp", cfg.Server.Listen)
		if err != nil {
			return fmt.Errorf("cannot listen on %s: %w", cfg.Server.Listen, err)
		}
		d.addr.Store(ln.Addr().String())
		srv := server.New(server.Deps{Cfg: d.Config, CfgPath: d.o.ConfigPath, DB: d.db, Hub: d.hub, Health: healthFn,
			Reload: func() error { return d.reload("dashboard") }, Loc: d.o.Loc, Now: d.o.Now, Log: d.log,
			Version: d.o.Version, Demo: d.o.Demo})
		httpSrv = &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
			ErrorLog: slog.NewLogLogger(d.log.Handler(), slog.LevelWarn)}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				d.log.Error("dashboard stopped", "err", err)
			}
		}()
		d.log.Info("dashboard listening", "addr", ln.Addr().String())
		if d.o.Ready != nil {
			d.o.Ready(ln.Addr().String())
		}
	}

	if !d.o.NoCollectors {
		if !d.o.NoSockets {
			if err := d.startSockets(runCtx, &wg); err != nil {
				return err
			}
		}
		wg.Add(1)
		go func() { defer wg.Done(); d.trk.Run(runCtx) }()
		wg.Add(1)
		go func() { defer wg.Done(); d.rollupLoop(runCtx) }()
		pruner := &retention.Pruner{DB: d.db, Cfg: d.Config, Loc: d.o.Loc, Now: d.o.Now, Log: d.log, Notify: d.alerts.Notify}
		wg.Add(1)
		go func() { defer wg.Done(); pruner.Run(runCtx) }()
		wg.Add(1)
		go func() { defer wg.Done(); d.alerts.Run(runCtx) }()
	}

	for _, k := range cfg.NotCollected() {
		d.log.Warn("this collector is switched on but not implemented in this release; nothing is recorded for it", "key", k)
	}
	sdNotify("READY=1\nSTATUS=" + statusLine(cfg))
	d.log.Info("parent-trackerd running", "version", d.o.Version, "monitored_users", len(cfg.MonitoredUsers), "recording", cfg.Recording())
	<-ctx.Done()

	sdNotify("STOPPING=1")
	d.log.Info("shutting down")
	cancel()
	if httpSrv != nil {
		shutdown, c := context.WithTimeout(context.Background(), 3*time.Second)
		_ = httpSrv.Shutdown(shutdown)
		c()
		_ = httpSrv.Close() // ends long-lived event streams
	}
	d.hub.Configure(d.Config().Server.LiveMaxClients, false)
	wg.Wait()
	var err error
	if d.trk != nil {
		err = d.trk.Stop()
	}
	if d.src != nil {
		d.src.Close()
	}
	if cerr := d.db.Checkpoint(); cerr != nil && err == nil {
		d.log.Warn("checkpoint failed", "err", cerr)
	}
	if cerr := d.db.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return err
}

func hostOf(listen string) string {
	h, _, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	return h
}

func (d *Daemon) startSockets(ctx context.Context, wg *sync.WaitGroup) error {
	cfg := d.Config()
	if d.agents != nil {
		ln, err := peercred.Listen(cfg.AgentSocket)
		if err != nil {
			return fmt.Errorf("agent socket: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.agents.Serve(ctx, ln); err != nil {
				d.log.Error("agent socket stopped", "err", err)
			}
		}()
	}
	ln, err := peercred.Listen(cfg.ControlSocket)
	if err != nil {
		return fmt.Errorf("control socket: %w", err)
	}
	cs := &control.Server{DB: d.db, Cfg: d.Config, Loc: d.o.Loc, Now: d.o.Now, Health: d.trk.Health}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := cs.Serve(ctx, ln); err != nil {
			d.log.Error("control socket stopped", "err", err)
		}
	}()
	return nil
}

func (d *Daemon) rollupLoop(ctx context.Context) {
	if d.roll.BackfillPending() {
		d.log.Info("rebuilding hourly history from existing data")
		if err := d.roll.Backfill(ctx); err != nil && ctx.Err() == nil {
			d.log.Error("hourly backfill failed; it will be retried at the next start", "err", err)
		}
	}
	t := time.NewTicker(rollupEvery)
	defer t.Stop()
	for {
		if err := d.roll.Run(ctx); err != nil && ctx.Err() == nil {
			d.log.Error("rollup failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Reload re-reads the configuration (SIGHUP, `systemctl reload`). Monitored
// users, collectors, retention, alerts and the live switch apply at once; a
// changed listen address needs a restart.
func (d *Daemon) Reload() error { return d.reload("signal") }

func (d *Daemon) reload(origin string) error {
	d.reloadMu.Lock()
	defer d.reloadMu.Unlock()
	if d.o.ConfigPath == "" {
		return nil
	}
	next, err := config.Load(d.o.ConfigPath)
	if err != nil {
		d.log.Error("not reloading: the configuration is invalid; keeping the previous settings", "err", err)
		return err
	}
	d.cfg.Store(next)
	setLevel(d.o.LogLevel, next.LogLevel)
	d.hub.Configure(next.Server.LiveMaxClients, next.Server.Live)
	if next.Server.Listen != d.listen {
		d.log.Warn("server.listen changed; restart the service to apply it", "was", d.listen)
	}
	if d.trk != nil {
		_ = d.trk.SyncUsers(lookupUser)
	}
	if d.agents != nil {
		d.agents.Reconfigure(context.Background())
	}
	d.auditChanges(next, origin != "dashboard") // the dashboard audits its own changes
	sdNotify("STATUS=" + statusLine(next))
	d.log.Info("configuration reloaded")
	return nil
}

// statusLine is what `systemctl status parent-trackerd` shows (FR-1.7).
func statusLine(cfg *config.Config) string {
	n := len(cfg.MonitoredUsers)
	switch {
	case cfg.DisclosureAcknowledged == "":
		return "Not recording: the disclosure notice has not been acknowledged (run parent-tracker setup)"
	case n == 0:
		return "Not recording: no users are monitored (run parent-tracker users add)"
	case !cfg.Collectors.Session:
		return "Not recording: the session collector is turned off"
	case n == 1:
		return "Recording 1 monitored user"
	}
	return fmt.Sprintf("Recording %d monitored users", n)
}

// Change is one audited configuration change.
type Change struct{ Action, Detail string }

var secret = map[string]bool{"alerts.ntfy_url": true, "alerts.webhook_url": true}

// Snapshot flattens a configuration into comparable key/value pairs. The
// password hash is reduced to a short fingerprint so a change is visible
// without the hash itself being copied around.
func Snapshot(c *config.Config) map[string]string {
	m := map[string]string{}
	for _, k := range config.Keys() {
		m[k], _ = c.Get(k)
	}
	sum := sha256.Sum256([]byte(c.Server.PasswordHash))
	m["server.password"] = hex.EncodeToString(sum[:6])
	return m
}

// DiffSnapshots lists the changes between two snapshots for the audit log
// (SEC-14). Secrets are named but never written.
func DiffSnapshots(old, cur map[string]string) []Change {
	var out []Change
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, ",")
	}
	in := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	var added, removed []string
	oldUsers, curUsers := split(old["monitored_users"]), split(cur["monitored_users"])
	for _, u := range curUsers {
		if !in(oldUsers, u) {
			added = append(added, u)
		}
	}
	for _, u := range oldUsers {
		if !in(curUsers, u) {
			removed = append(removed, u)
		}
	}
	if len(added)+len(removed) > 0 {
		out = append(out, Change{model.AuditUsersChanged, fmt.Sprintf("added: %s; removed: %s", orNone(added), orNone(removed))})
	}
	if old["server.password"] != cur["server.password"] {
		out = append(out, Change{model.AuditConfigChanged, "dashboard password changed"})
	}
	keys := make([]string, 0, len(cur))
	for k := range cur {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "monitored_users" || k == "server.password" {
			continue
		}
		va, vb := old[k], cur[k]
		if va == vb {
			continue
		}
		action := model.AuditConfigChanged
		if strings.HasPrefix(k, "collectors.") && vb == "false" {
			action = model.AuditCollectorDisabled
		}
		detail := fmt.Sprintf("%s: %s -> %s", k, va, vb)
		if secret[k] {
			detail = k + " changed (value not logged)"
		}
		out = append(out, Change{action, detail})
	}
	return out
}

// auditChanges compares the configuration with the snapshot stored by the
// previous start or reload, writes what changed to the audit log, and stores
// the new snapshot. Changes made while the service was stopped are therefore
// caught at the next start too. With record false (the dashboard audits its own
// changes) only the snapshot is updated.
func (d *Daemon) auditChanges(cfg *config.Config, record bool) {
	cur := Snapshot(cfg)
	if raw, ok, _ := d.db.GetState("config_snapshot"); ok && record {
		var old map[string]string
		if json.Unmarshal([]byte(raw), &old) == nil {
			for _, c := range DiffSnapshots(old, cur) {
				_ = d.db.Audit(d.o.Now().UnixMilli(), "admin", c.Action, c.Detail)
			}
		}
	}
	if b, err := json.Marshal(cur); err == nil {
		_ = d.db.SetState("config_snapshot", string(b))
	}
}

func orNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}

// sdNotify tells systemd about state changes (Type=notify).
func sdNotify(state string) {
	path := os.Getenv("NOTIFY_SOCKET")
	if path == "" {
		return
	}
	if strings.HasPrefix(path, "@") {
		path = "\x00" + path[1:]
	}
	c, err := net.Dial("unixgram", path)
	if err != nil {
		return
	}
	defer c.Close()
	_, _ = c.Write([]byte(state))
}

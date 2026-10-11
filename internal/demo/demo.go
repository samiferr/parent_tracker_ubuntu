// Package demo runs the real daemon against made-up data, for trying the
// dashboard and for checking changes to it. Nothing here touches a real
// session: the sessions come from a scripted fake source, the history is
// generated, and the database lives in a temporary directory.
package demo

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/daemon"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

// Credentials of the demo dashboard.
const (
	Username = "parent"
	Password = "demo-password"
)

// Options configures a demo run.
type Options struct {
	Listen  string // default 127.0.0.1:8787
	Version string
	Log     *slog.Logger
	Out     io.Writer
	// Dir overrides the temporary directory (tests).
	Dir string
	// Ready is called with the address once the dashboard is listening.
	Ready func(addr string)
}

type person struct {
	name    string
	uid     int
	typ     string
	desktop string
	apps    []weighted
	// ext is false for a Wayland session whose GNOME extension is not loaded.
	ext bool
}

type weighted struct {
	id, name string
	w        int
}

var people = []person{
	{"alice", 2001, "wayland", "ubuntu", []weighted{{"firefox", "Firefox Web Browser", 4}, {"libreoffice-writer", "LibreOffice Writer", 3}, {"code", "Visual Studio Code", 2}, {"org.gnome.nautilus", "Files", 1}}, true},
	{"ben", 2002, "x11", "ubuntu-xorg", []weighted{{"minecraft", "Minecraft Launcher", 5}, {"discord", "Discord", 3}, {"vlc", "VLC media player", 2}, {"firefox", "Firefox Web Browser", 2}}, true},
	{"cleo", 2003, "wayland", "ubuntu", []weighted{{"firefox", "Firefox Web Browser", 3}, {"vlc", "VLC media player", 1}}, false},
}

// Run starts the demo and blocks until ctx ends.
func Run(ctx context.Context, o Options) error {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	dir := o.Dir
	if dir == "" {
		d, err := os.MkdirTemp("", "parent-tracker-demo-")
		if err != nil {
			return err
		}
		dir = d
		defer os.RemoveAll(dir)
	}
	listen := o.Listen
	if listen == "" {
		listen = "127.0.0.1:8787"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.SampleInterval = config.Duration(2 * time.Second)
	cfg.MonitoredUsers = []string{"alice", "ben", "cleo"}
	cfg.DisclosureAcknowledged = time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339)
	cfg.Alerts.Bedtime = "21:00"
	cfg.Server.Listen = listen
	cfg.Server.Username = Username
	cfg.Server.PasswordHash = string(hash)
	cfgPath := filepath.Join(dir, "config.toml")
	if err := cfg.Save(cfgPath); err != nil {
		return err
	}

	src := collector.NewFake()
	feed := &feed{reports: map[string]agentproto.Report{}}
	d, err := daemon.New(daemon.Options{Config: cfg, ConfigPath: cfgPath, Version: o.Version, Log: o.Log, Source: src, Agents: feed,
		NoSockets: true, Demo: true, Loc: time.Local, Ready: func(addr string) {
			if o.Out != nil {
				fmt.Fprintf(o.Out, "\nDemo dashboard: http://%s\nSign in as %q with password %q.\nEverything shown is made up; nothing is recorded. Press Ctrl+C to stop.\n\n", addr, Username, Password)
			}
			if o.Ready != nil {
				o.Ready(addr)
			}
		}})
	if err != nil {
		return err
	}
	if err := Seed(d.DB(), cfg, time.Now(), time.Local, 1); err != nil {
		return err
	}
	go Script(ctx, src, feed, 1)
	return d.Run(ctx)
}

// feed is an AgentFeed that answers from a script instead of a real agent.
type feed struct {
	mu      sync.Mutex
	reports map[string]agentproto.Report
}

func (f *feed) set(session string, r agentproto.Report) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports[session] = r
}

// Latest implements tracker.AgentFeed; scripted reports are always fresh.
func (f *feed) Latest(session string) (agentproto.Report, time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.reports[session]
	return r, time.Now(), ok
}

// Script drives the fake sessions: each person alternates between active, idle
// and locked, and the app in front changes every so often.
func Script(ctx context.Context, src *collector.Fake, f *feed, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	type cursor struct {
		state    int // 0 active 1 idle 2 locked
		until    time.Time
		appUntil time.Time
		app      weighted
	}
	cur := make([]cursor, len(people))
	for i, p := range people {
		s := collector.UserSession(fmt.Sprint(i+2), p.uid, p.name, p.typ, p.desktop)
		src.Set(s)
		cur[i].until = time.Now().Add(time.Duration(60+rng.Intn(90)) * time.Second)
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for i, p := range people {
				c := &cur[i]
				id := fmt.Sprint(i + 2)
				if now.After(c.until) {
					switch c.state {
					case 0:
						c.state = []int{1, 2}[rng.Intn(2)]
						c.until = now.Add(time.Duration(25+rng.Intn(35)) * time.Second)
					default:
						c.state = 0
						c.until = now.Add(time.Duration(70+rng.Intn(110)) * time.Second)
					}
					st := c.state
					src.Update(id, func(s *collector.SessionInfo) {
						s.LockedHint, s.IdleHint, s.IdleSince = st == 2, st == 1, time.Time{}
						if st == 1 {
							s.IdleSince = now
						}
					})
					src.Emit(collector.Event{Kind: collector.EventChanged, At: now})
				}
				if now.After(c.appUntil) {
					c.app = pick(rng, p.apps)
					c.appUntil = now.Add(time.Duration(15+rng.Intn(35)) * time.Second)
				}
				prov := string(collector.ProviderFor(collector.UserSession(id, p.uid, p.name, p.typ, p.desktop)))
				if !p.ext {
					f.set(id, agentproto.Report{Type: "report", Provider: prov, Status: agentproto.StatusMissing, Detail: "the extension is not loaded in this session"})
					continue
				}
				f.set(id, agentproto.Report{Type: "report", Provider: prov, Status: agentproto.StatusOK, App: &agentproto.App{ID: c.app.id, Name: c.app.name, PID: 1000 + i}})
			}
		}
	}
}

func pick(rng *rand.Rand, apps []weighted) weighted {
	total := 0
	for _, a := range apps {
		total += a.w
	}
	n := rng.Intn(total)
	for _, a := range apps {
		if n < a.w {
			return a
		}
		n -= a.w
	}
	return apps[0]
}

// Seed writes 30 days of plausible history for the demo people into db, through
// the real storage code, then lets the real rollup build the hourly and daily
// aggregates from it. now is "today"; nothing is generated after it.
func Seed(db *storage.DB, cfg *config.Config, now time.Time, loc *time.Location, seed int64) error {
	rng := rand.New(rand.NewSource(seed))
	nowMs := now.UnixMilli()
	for _, p := range people {
		if err := db.UpsertUser(p.uid, p.name, true, nowMs-40*24*3600*1000); err != nil {
			return err
		}
		for back := 30; back >= 0; back-- {
			day := time.Date(now.Year(), now.Month(), now.Day()-back, 0, 0, 0, 0, loc)
			if err := seedDay(db, cfg, p, day, now, loc, rng, back); err != nil {
				return err
			}
		}
	}
	// a few alerts and audit entries for the Alerts screen
	_, _ = db.InsertAlert(model.Alert{CreatedAt: nowMs - 26*3600*1000, RuleID: "bedtime", UID: 2002, Severity: "warn",
		Message: "ben was active at 21:35, after bedtime (21:00).", Delivered: false})
	_ = db.Audit(nowMs-20*24*3600*1000, "admin", model.AuditUsersChanged, "added: cleo; removed: none")
	_ = db.Audit(nowMs-3*24*3600*1000, "system", model.AuditSensorLost, "ben: x11 session 3: agent_silent")
	_ = db.Audit(nowMs-3*24*3600*1000+90*1000, "system", model.AuditSensorRestored, "ben: x11 session 3")
	return nil
}

var blocks = map[bool][][2]float64{
	false: {{16.2, 17.7}, {18.5, 19.8}, {21.05, 21.8}}, // weekday: after school, evening, sometimes late
	true:  {{10.0, 12.3}, {14.0, 17.5}, {19.5, 21.7}},  // weekend
}

func seedDay(db *storage.DB, cfg *config.Config, p person, day, now time.Time, loc *time.Location, rng *rand.Rand, back int) error {
	weekend := day.Weekday() == time.Saturday || day.Weekday() == time.Sunday
	at := func(h float64) int64 {
		return day.Add(time.Duration(h * float64(time.Hour))).UnixMilli()
	}
	nowMs := now.UnixMilli()
	for bi, b := range blocks[weekend] {
		if bi == 2 && !weekend && rng.Intn(3) != 0 { // late use only some evenings
			continue
		}
		if rng.Intn(9) == 0 { // a day off
			continue
		}
		start, end := at(b[0]+rng.Float64()*0.3), at(b[1]-rng.Float64()*0.3)
		for cur := start; cur < end && cur < nowMs; {
			run := int64((6 + rng.Intn(30)) * 60 * 1000)
			runEnd := min64(cur+run, end, nowMs)
			if err := writeInterval(db, p, model.StateActive, cur, runEnd, rng, true); err != nil {
				return err
			}
			cur = runEnd
			if cur >= end || cur >= nowMs {
				break
			}
			gap := int64((2 + rng.Intn(7)) * 60 * 1000)
			st := model.StateIdle
			if rng.Intn(4) == 0 {
				st, gap = model.StateLocked, int64((8+rng.Intn(12))*60*1000)
			}
			gapEnd := min64(cur+gap, end, nowMs)
			if err := writeInterval(db, p, st, cur, gapEnd, rng, st == model.StateIdle); err != nil {
				return err
			}
			cur = gapEnd
		}
	}
	if back == 9 && p.name == "ben" { // one tracking gap
		return db.InsertGap(model.Gap{UID: 0, Start: at(19.0), End: at(19.7), Kind: model.GapCrash}, "demo")
	}
	return nil
}

func min64(a int64, rest ...int64) int64 {
	for _, v := range rest {
		if v < a {
			a = v
		}
	}
	return a
}

func writeInterval(db *storage.DB, p person, st model.State, start, end int64, rng *rand.Rand, withApps bool) error {
	if end <= start {
		return nil
	}
	id, err := db.OpenInterval(p.uid, st, start)
	if err != nil {
		return err
	}
	if err := db.CloseInterval(id, end); err != nil {
		return err
	}
	if !withApps || !p.ext {
		return nil
	}
	var samples []model.Sample
	names := map[string]string{}
	app := pick(rng, p.apps)
	next := start + int64((3+rng.Intn(12))*60*1000)
	prov := "gnome-wayland"
	if p.typ == "x11" {
		prov = "x11"
	}
	for ts := start; ts < end; ts += 10_000 {
		if ts >= next {
			app = pick(rng, p.apps)
			next = ts + int64((3+rng.Intn(12))*60*1000)
		}
		samples = append(samples, model.Sample{UID: p.uid, TS: ts, AppID: app.id, State: st, Source: prov})
		names[app.id] = app.name
	}
	return db.InsertSamples(samples, names)
}

package tracker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/live"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

const (
	sec = int64(1000)
	min = 60 * sec
)

type fakeFeed struct {
	reports map[string]agentproto.Report
	recv    map[string]time.Time
}

func (f *fakeFeed) Latest(id string) (agentproto.Report, time.Time, bool) {
	r, ok := f.reports[id]
	return r, f.recv[id], ok
}

type harness struct {
	t       *testing.T
	db      *storage.DB
	src     *collector.Fake
	feed    *fakeFeed
	hub     *live.Hub
	cfg     *config.Config
	tr      *Tracker
	now     time.Time
	notices []Notice
	path    string
}

func newHarness(t *testing.T, users ...string) *harness {
	t.Helper()
	h := &harness{t: t, src: collector.NewFake(), feed: &fakeFeed{reports: map[string]agentproto.Report{}, recv: map[string]time.Time{}},
		hub: live.New(10, true), now: time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC), path: filepath.Join(t.TempDir(), "t.db")}
	db, err := storage.Open(h.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h.db = db
	h.cfg = config.Default()
	h.cfg.MonitoredUsers = users
	h.cfg.DisclosureAcknowledged = "2026-10-01T00:00:00Z"
	h.tr = h.newTracker()
	return h
}

func (h *harness) newTracker() *Tracker {
	return New(Options{
		Cfg: func() *config.Config { return h.cfg }, Source: h.src, DB: h.db, Agents: h.feed, Hub: h.hub,
		Loc: time.UTC, Now: func() time.Time { return h.now }, Notify: func(n Notice) { h.notices = append(h.notices, n) },
	})
}

func (h *harness) tick() { h.tr.Tick(context.Background()) }

// run advances the clock by d in sampling-interval steps, ticking at each step.
func (h *harness) run(d time.Duration) {
	steps := int(d / h.cfg.SampleInterval.D())
	for i := 0; i < steps; i++ {
		h.now = h.now.Add(h.cfg.SampleInterval.D())
		h.tick()
	}
}

func (h *harness) report(session string, r agentproto.Report) {
	r.Type = "report"
	h.feed.reports[session] = r
	h.feed.recv[session] = h.now
}

func (h *harness) totals(uid int) aggregate.Totals {
	h.t.Helper()
	ivs, err := h.db.Intervals(uid, 0, 1<<60)
	if err != nil {
		h.t.Fatal(err)
	}
	return aggregate.ClipTotals(ivs, 0, 1<<60)
}

func near(got, want, tol int64) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func (h *harness) wantMin(what string, got int64, wantMin int64) {
	h.t.Helper()
	if !near(got, wantMin*min, 6*sec) {
		h.t.Errorf("%s = %.1f min, want %d min", what, float64(got)/float64(min), wantMin)
	}
}

func login(h *harness, id string, uid int, user, typ, desktop string) {
	h.src.Set(collector.UserSession(id, uid, user, typ, desktop))
}

// AC-1..AC-3: a session row opens and closes; locking stops active time.
func TestLoginLockUnlockLogout(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(10 * time.Minute)
	h.src.Update("2", func(s *collector.SessionInfo) { s.LockedHint = true })
	h.run(30 * time.Minute)
	h.src.Update("2", func(s *collector.SessionInfo) { s.LockedHint = false })
	h.run(5 * time.Minute)

	tot := h.totals(1000)
	h.wantMin("active", tot.Active, 15)
	h.wantMin("locked (AC-3: adds 0 active minutes)", tot.Locked, 30)

	ss, _ := h.db.Sessions(1000, 0, 1<<60)
	if len(ss) != 1 || ss[0].SessionType != model.SessionWayland || ss[0].EndedAt != 0 {
		t.Fatalf("sessions = %+v", ss)
	}
	h.src.Remove("2") // logout
	h.run(5 * time.Second)
	ss, _ = h.db.Sessions(1000, 0, 1<<60)
	if ss[0].EndedAt == 0 {
		t.Fatal("logging out must close the session row (AC-1)")
	}
	if ivs, _ := h.db.LoadOpenIntervals(); len(ivs) != 0 {
		t.Fatalf("open intervals after logout: %+v", ivs)
	}
}

func TestIdleIsBackDatedToWhenItBegan(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "x11", "ubuntu-xorg")
	h.tick()
	h.run(10 * time.Minute)
	// the desktop notices at +10 min that input stopped at +6 min
	since := h.now.Add(-4 * time.Minute)
	h.src.Update("2", func(s *collector.SessionInfo) { s.IdleHint, s.IdleSince = true, since })
	h.run(5 * time.Second)
	h.run(5 * time.Minute)
	tot := h.totals(1000)
	h.wantMin("active (idle detection delay must not count as use)", tot.Active, 6)
	h.wantMin("idle", tot.Idle, 9)
}

// AC-5: suspend/resume across two hours shows a suspended interval, not 2 h of use.
func TestSuspendAndResume(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(5 * time.Minute)
	h.tr.OnSleep(h.now)
	h.now = h.now.Add(2 * time.Hour)
	h.tr.OnResume(h.now)
	h.run(5 * time.Minute)

	tot := h.totals(1000)
	h.wantMin("active", tot.Active, 10)
	if !near(tot.Suspended, 120*min, 6*sec) {
		t.Fatalf("suspended = %d min", tot.Suspended/min)
	}
	if gaps, _ := h.db.Gaps(1000, 0, 1<<60); len(gaps) != 0 {
		t.Fatalf("a suspend is explained by its interval, not a gap: %+v", gaps)
	}
}

// AC-6: a clock jump never produces a negative duration.
func TestClockJumpBackNeverNegative(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(10 * time.Minute)
	h.now = h.now.Add(-time.Hour) // NTP steps the clock back
	h.tick()
	h.run(time.Minute)
	h.now = h.now.Add(2 * time.Hour) // and forward again, well past the stall threshold
	h.tick()
	h.run(time.Minute)

	ivs, _ := h.db.Intervals(1000, 0, 1<<60)
	for _, iv := range ivs {
		if iv.End < iv.Start {
			t.Fatalf("negative interval: %+v", iv)
		}
	}
	ev, _ := h.db.AuditEvents(0, 1<<60, 100)
	var skew bool
	for _, e := range ev {
		skew = skew || e.Action == model.AuditClockSkew
	}
	if !skew {
		t.Fatal("clock skew should be audit-logged")
	}
	tot := h.totals(1000)
	if tot.Active > 15*min {
		t.Fatalf("a clock jump must not create usage: active = %d min", tot.Active/min)
	}
}

// A long silence that was not announced by a suspend event is a gap, not use.
func TestUnexplainedSilenceBecomesAGap(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(5 * time.Minute)
	h.now = h.now.Add(40 * time.Minute)
	h.tick()
	h.run(5 * time.Minute)
	tot := h.totals(1000)
	h.wantMin("active", tot.Active, 10)
	gaps, _ := h.db.Gaps(1000, 0, 1<<60)
	if len(gaps) != 1 || gaps[0].Kind != model.GapStall || !near(gaps[0].End-gaps[0].Start, 40*min, 6*sec) {
		t.Fatalf("gaps = %+v", gaps)
	}
}

// AC-40: after a kill -9, prior data is intact and the interrupted session is
// closed with a gap marker.
func TestCrashRecovery(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(20 * time.Minute)
	crashAt := h.now
	// kill -9: no Stop(); a new process starts 1 h later
	h.now = h.now.Add(time.Hour)
	h.tr = h.newTracker()
	if err := h.tr.Recover(); err != nil {
		t.Fatal(err)
	}
	if open, _ := h.db.LoadOpenIntervals(); len(open) != 0 {
		t.Fatalf("recovery left open intervals: %+v", open)
	}
	ivs, _ := h.db.Intervals(1000, 0, 1<<60)
	if len(ivs) != 1 || !near(ivs[0].End, crashAt.UnixMilli(), 10*sec) {
		t.Fatalf("interval must be closed at the last good instant, got %+v (crash at %d)", ivs, crashAt.UnixMilli())
	}
	gaps, _ := h.db.Gaps(1000, 0, 1<<60)
	if len(gaps) != 1 || gaps[0].Kind != model.GapCrash || !near(gaps[0].End-gaps[0].Start, 60*min, 15*sec) {
		t.Fatalf("gaps = %+v", gaps)
	}
	ss, _ := h.db.Sessions(1000, 0, 1<<60)
	if len(ss) != 1 || ss[0].EndReason != "unclean" || ss[0].EndedAt == 0 {
		t.Fatalf("sessions = %+v", ss)
	}
	var told bool
	for _, n := range h.notices {
		told = told || n.Rule == "tracking_gap"
	}
	if !told {
		t.Fatal("an unclean stop should raise a tracking_gap notice (notify_on_service_stop)")
	}
	// and tracking resumes by itself
	h.tick()
	h.run(5 * time.Minute)
	h.wantMin("active after recovery", h.totals(1000).Active, 25)
}

// AC-53: stopping the service creates a visible gap and audit entry.
func TestCleanStopLeavesAGapAndAnAuditEntry(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(10 * time.Minute)
	if err := h.tr.Stop(); err != nil {
		t.Fatal(err)
	}
	if open, _ := h.db.LoadOpenIntervals(); len(open) != 0 {
		t.Fatal("Stop must close intervals")
	}
	h.now = h.now.Add(30 * time.Minute) // service is down
	h.tr = h.newTracker()
	h.tr.Recover()
	gaps, _ := h.db.Gaps(1000, 0, 1<<60)
	if len(gaps) != 1 || gaps[0].Kind != model.GapServiceStopped || !near(gaps[0].End-gaps[0].Start, 30*min, 10*sec) {
		t.Fatalf("gaps = %+v", gaps)
	}
	ev, _ := h.db.AuditEvents(0, 1<<60, 100)
	var stopped, started bool
	for _, e := range ev {
		stopped = stopped || e.Action == model.AuditServiceStopped
		started = started || e.Action == model.AuditServiceStarted
	}
	if !stopped || !started {
		t.Fatalf("audit = %+v", ev)
	}
}

// AC-20, AC-102: only allowlisted users are recorded; greeter and SSH sessions
// produce no rows.
func TestOnlyAllowlistedLocalUserSessionsAreRecorded(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	login(h, "3", 1001, "guest", "x11", "ubuntu-xorg")
	greeter := collector.UserSession("c1", 125, "gdm", "wayland", "gnome")
	greeter.Class = "greeter"
	h.src.Set(greeter)
	ssh := collector.UserSession("9", 1000, "ilyes", "tty", "")
	ssh.Remote, ssh.Active = true, false
	h.src.Set(ssh)

	h.tick()
	h.run(5 * time.Minute)

	for _, uid := range []int{1001, 125} {
		if ivs, _ := h.db.Intervals(uid, 0, 1<<60); len(ivs) != 0 {
			t.Fatalf("uid %d must produce zero rows (AC-20): %+v", uid, ivs)
		}
	}
	ss, _ := h.db.Sessions(1000, 0, 1<<60)
	if len(ss) != 1 || ss[0].LogindID != "2" {
		t.Fatalf("the ssh session must not be recorded: %+v", ss)
	}
	for _, u := range h.tr.Health().Users {
		if u.UID != 1000 {
			t.Fatalf("health lists a non-monitored user: %+v", u)
		}
	}
}

// AC-71: nothing is recorded until the disclosure has been acknowledged.
func TestNothingIsRecordedBeforeDisclosureAcknowledged(t *testing.T) {
	h := newHarness(t, "ilyes")
	h.cfg.DisclosureAcknowledged = ""
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(5 * time.Minute)
	if ivs, _ := h.db.Intervals(1000, 0, 1<<60); len(ivs) != 0 {
		t.Fatalf("recorded without acknowledgement: %+v", ivs)
	}
	if h.tr.Health().Recording {
		t.Fatal("health should say recording is off")
	}
	h.cfg.DisclosureAcknowledged = "2026-10-11T10:06:00Z"
	h.run(5 * time.Minute)
	h.wantMin("active once acknowledged", h.totals(1000).Active, 5)
}

// AC-21, AC-106: fast user switching attributes time to the right user, with the
// two users in different session types at the same time.
func TestFastUserSwitching(t *testing.T) {
	h := newHarness(t, "ilyes", "maram")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	login(h, "3", 1001, "maram", "x11", "ubuntu-xorg")
	h.src.Update("3", func(s *collector.SessionInfo) { s.Active = false }) // maram's session is in the background
	h.tick()
	h.run(10 * time.Minute)
	h.src.Update("2", func(s *collector.SessionInfo) { s.Active = false })
	h.src.Update("3", func(s *collector.SessionInfo) { s.Active = true })
	h.run(20 * time.Minute)

	a, b := h.totals(1000), h.totals(1001)
	h.wantMin("ilyes active", a.Active, 10)
	h.wantMin("ilyes background (counts as locked)", a.Locked, 20)
	h.wantMin("maram active", b.Active, 20)
	h.wantMin("maram background", b.Locked, 10)
	ss1, _ := h.db.Sessions(1000, 0, 1<<60)
	ss2, _ := h.db.Sessions(1001, 0, 1<<60)
	if ss1[0].SessionType != model.SessionWayland || ss2[0].SessionType != model.SessionX11 {
		t.Fatal("each session keeps its own type")
	}
}

// AC-103 / NFR-3.9: the same scripted lock / idle / suspend sequence yields
// identical totals on a Wayland and an X11 session.
func TestWaylandAndX11GiveIdenticalTotals(t *testing.T) {
	script := func(typ, desktop string) aggregate.Totals {
		h := newHarness(t, "ilyes")
		login(h, "2", 1000, "ilyes", typ, desktop)
		h.tick()
		h.run(12 * time.Minute)
		h.src.Update("2", func(s *collector.SessionInfo) { s.LockedHint = true })
		h.run(7 * time.Minute)
		h.src.Update("2", func(s *collector.SessionInfo) { s.LockedHint = false })
		h.run(3 * time.Minute)
		h.src.Update("2", func(s *collector.SessionInfo) { s.IdleHint, s.IdleSince = true, h.now })
		h.run(9 * time.Minute)
		h.src.Update("2", func(s *collector.SessionInfo) { s.IdleHint = false })
		h.run(4 * time.Minute)
		h.tr.OnSleep(h.now)
		h.now = h.now.Add(45 * time.Minute)
		h.tr.OnResume(h.now)
		h.run(6 * time.Minute)
		return h.totals(1000)
	}
	w, x := script("wayland", "ubuntu"), script("x11", "ubuntu-xorg")
	if w != x {
		t.Fatalf("wayland %+v != x11 %+v", w, x)
	}
	if w.Active == 0 || w.Locked == 0 || w.Idle == 0 || w.Suspended == 0 {
		t.Fatalf("script did not exercise every state: %+v", w)
	}
}

func okReport(provider, id, name, title string) agentproto.Report {
	return agentproto.Report{Provider: provider, Status: agentproto.StatusOK, App: &agentproto.App{ID: id, Name: name, PID: 42}, Title: title}
}

// AC-13, AC-86: titles are neither stored nor sent live while capture is off.
func TestWindowTitlesFollowTheCollectorSwitch(t *testing.T) {
	for _, titles := range []bool{false, true} {
		h := newHarness(t, "ilyes")
		h.cfg.Collectors.WindowTitle = titles
		login(h, "2", 1000, "ilyes", "x11", "ubuntu-xorg")
		h.tick()
		for i := 0; i < 6; i++ {
			h.report("2", okReport("x11", "firefox", "Firefox", "Bank statement - Mozilla Firefox"))
			h.now = h.now.Add(5 * time.Second)
			h.tick()
		}
		samples, _ := h.db.Samples(1000, 0, 1<<60)
		if len(samples) == 0 {
			t.Fatal("no samples written")
		}
		snap, _ := h.hub.Snapshot(1000)
		stored := samples[0].Title
		if titles {
			if stored == "" || snap.App == nil || snap.App.Title == "" {
				t.Fatalf("title capture on, but title missing: stored=%q live=%+v", stored, snap.App)
			}
		} else {
			if stored != "" || snap.App == nil || snap.App.Title != "" {
				t.Fatalf("title capture off, but a title leaked: stored=%q live=%+v", stored, snap.App)
			}
		}
	}
}

// AC-10: foreground samples are attributed to the app in front, with its name.
func TestAppSamplesCarryProviderAndName(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	for i := 0; i < 4; i++ {
		h.report("2", okReport("gnome-wayland", "code", "Visual Studio Code", ""))
		h.now = h.now.Add(5 * time.Second)
		h.tick()
	}
	ss, _ := h.db.Samples(1000, 0, 1<<60)
	if len(ss) != 4 || ss[0].AppID != "code" || ss[0].AppName != "Visual Studio Code" || ss[0].Source != "gnome-wayland" {
		t.Fatalf("samples = %+v", ss)
	}
	snap, _ := h.hub.Snapshot(1000)
	if snap.AppStatus != "ok" || snap.App == nil || snap.App.ID != "code" || len(snap.Degraded) != 0 {
		t.Fatalf("snapshot = %+v", snap)
	}
}

// AC-12, AC-104: on Wayland without the extension nothing is guessed, no app
// rows are written, screen time is still recorded, and the banner names the
// session type and the next step. An X11-provider report is never accepted.
func TestWaylandWithoutExtensionIsDegradedNotGuessed(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.report("2", agentproto.Report{Provider: "gnome-wayland", Status: agentproto.StatusMissing, Detail: "bus name absent"})
	for i := 0; i < 6; i++ {
		h.now = h.now.Add(5 * time.Second)
		h.report("2", agentproto.Report{Provider: "gnome-wayland", Status: agentproto.StatusMissing})
		h.tick()
	}
	if ss, _ := h.db.Samples(1000, 0, 1<<60); len(ss) != 0 {
		t.Fatalf("no app rows may be written: %+v", ss)
	}
	snap, _ := h.hub.Snapshot(1000)
	if snap.AppStatus != "degraded" || snap.App != nil || len(snap.Degraded) != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	d := snap.Degraded[0]
	if d.Reason != ReasonExtensionMissing || !strings.Contains(d.Message, "Wayland session") ||
		!strings.Contains(d.NextStep, "log out and back in") || !strings.Contains(d.NextStep, "parent-tracker doctor") {
		t.Fatalf("degraded = %+v", d)
	}
	if h.totals(1000).Active == 0 {
		t.Fatal("screen time must still be recorded")
	}

	// a report from the X11 provider on a Wayland session is ignored (FR-3.9)
	h.report("2", okReport("x11", "xwayland-app", "Some XWayland App", ""))
	h.now = h.now.Add(5 * time.Second)
	h.tick()
	if ss, _ := h.db.Samples(1000, 0, 1<<60); len(ss) != 0 {
		t.Fatalf("the X11 provider must not be used on a Wayland session: %+v", ss)
	}
	snap, _ = h.hub.Snapshot(1000)
	if snap.Degraded[0].Reason != ReasonProviderMismatch {
		t.Fatalf("reason = %s", snap.Degraded[0].Reason)
	}
}

// AC-105: killing the agent or disabling the extension mid-session produces an
// audit entry and an alert within 3 intervals; restoring logs a restore. Screen
// time is unaffected throughout.
func TestSensorLostAndRestored(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	for i := 0; i < 6; i++ {
		h.report("2", okReport("gnome-wayland", "firefox", "Firefox", ""))
		h.now = h.now.Add(5 * time.Second)
		h.tick()
	}
	// the extension is disabled: reports stop being "ok"
	lostAt := h.now
	for i := 0; i < 5; i++ {
		h.now = h.now.Add(5 * time.Second)
		h.report("2", agentproto.Report{Provider: "gnome-wayland", Status: agentproto.StatusDisabled})
		h.tick()
		if h.now.Sub(lostAt) <= 15*time.Second && i < 0 {
			t.Fatal("unreachable")
		}
	}
	ev, _ := h.db.AuditEvents(0, 1<<60, 100)
	var lost *model.AuditEvent
	for i := range ev {
		if ev[i].Action == model.AuditSensorLost {
			lost = &ev[i]
		}
	}
	if lost == nil {
		t.Fatalf("no app_sensor_lost audit entry: %+v", ev)
	}
	if time.UnixMilli(lost.TS).Sub(lostAt) > 15*time.Second {
		t.Fatalf("sensor loss was noticed after %v, want within 3 × the interval", time.UnixMilli(lost.TS).Sub(lostAt))
	}
	var alerted bool
	for _, n := range h.notices {
		alerted = alerted || (n.Rule == "sensor_lost" && strings.Contains(n.Message, "turned off"))
	}
	if !alerted {
		t.Fatalf("no alert: %+v", h.notices)
	}
	activeDuringLoss := h.totals(1000).Active

	// the agent itself dies: reports stop altogether
	h2 := h.now
	h.run(30 * time.Second)
	if snap, _ := h.hub.Snapshot(1000); snap.Degraded[0].Reason != ReasonExtensionDisabled && snap.Degraded[0].Reason != ReasonAgentSilent {
		t.Fatalf("reason = %s", snap.Degraded[0].Reason)
	}
	if h.totals(1000).Active <= activeDuringLoss || h.now.Sub(h2) == 0 {
		t.Fatal("screen time must keep accruing while the app sensor is down")
	}

	// restored
	h.report("2", okReport("gnome-wayland", "firefox", "Firefox", ""))
	h.now = h.now.Add(5 * time.Second)
	h.tick()
	ev, _ = h.db.AuditEvents(0, 1<<60, 100)
	var restored bool
	for _, e := range ev {
		restored = restored || e.Action == model.AuditSensorRestored
	}
	if !restored {
		t.Fatal("no app_sensor_restored audit entry")
	}
	snap, _ := h.hub.Snapshot(1000)
	if snap.AppStatus != "ok" || len(snap.Degraded) != 0 {
		t.Fatalf("snapshot after restore = %+v", snap)
	}
}

func TestNoAgentWithinGraceIsQuietThenAnnounced(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "x11", "ubuntu-xorg")
	h.tick()
	h.run(20 * time.Second)
	if snap, _ := h.hub.Snapshot(1000); len(snap.Degraded) != 0 {
		t.Fatalf("no banner during the start-up grace period: %+v", snap.Degraded)
	}
	h.run(2 * time.Minute)
	snap, _ := h.hub.Snapshot(1000)
	if len(snap.Degraded) != 1 || snap.Degraded[0].Reason != ReasonAgentNotRunning {
		t.Fatalf("degraded = %+v", snap.Degraded)
	}
}

func TestTTYAndUnsupportedDesktopsAreExplained(t *testing.T) {
	h := newHarness(t, "ilyes", "maram")
	login(h, "2", 1000, "ilyes", "tty", "")
	login(h, "3", 1001, "maram", "wayland", "KDE")
	h.tick()
	h.run(10 * time.Second)
	s1, _ := h.hub.Snapshot(1000)
	s2, _ := h.hub.Snapshot(1001)
	if len(s1.Degraded) != 1 || s1.Degraded[0].Reason != ReasonTTY || s1.AppStatus != "off" {
		t.Fatalf("tty = %+v", s1)
	}
	if len(s2.Degraded) != 1 || s2.Degraded[0].Reason != ReasonUnsupportedDesk {
		t.Fatalf("kde = %+v", s2)
	}
	// still recorded
	if h.totals(1000).Active == 0 || h.totals(1001).Active == 0 {
		t.Fatal("screen time must be recorded on every session type")
	}
}

// FR-6.12: live totals include the in-progress interval and match the stored
// figures.
func TestLiveTotalsMatchStoredFigures(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(10 * time.Minute)
	h.src.Update("2", func(s *collector.SessionInfo) { s.LockedHint = true })
	h.run(4 * time.Minute)
	h.src.Update("2", func(s *collector.SessionInfo) { s.LockedHint = false })
	h.run(3 * time.Minute)

	snap, ok := h.hub.Snapshot(1000)
	if !ok {
		t.Fatal("no snapshot")
	}
	ds, de := aggregate.DayBounds(time.UTC, h.now.UnixMilli())
	ivs, _ := h.db.Intervals(1000, ds, de)
	stored := aggregate.ClipTotals(ivs, ds, de)
	if !near(snap.Today.ActiveMs, stored.Active, 5*sec) || !near(snap.Today.LockedMs, stored.Locked, 5*sec) {
		t.Fatalf("live %+v != stored %+v", snap.Today, stored)
	}
	if snap.State != "active" || snap.LastSampleAt != h.now.UnixMilli() || snap.IntervalMs != 5000 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if !near(snap.Hour.ActiveMs, stored.Active, 5*sec) { // all of it is within 10:00–11:00
		t.Fatalf("hour = %+v", snap.Hour)
	}
	// the active counter stops while locked (AC-82)
	h.src.Update("2", func(s *collector.SessionInfo) { s.LockedHint = true })
	h.run(10 * time.Second)
	a, _ := h.hub.Snapshot(1000)
	h.run(60 * time.Second)
	b, _ := h.hub.Snapshot(1000)
	if a.State != "locked" || b.Today.ActiveMs != a.Today.ActiveMs || b.Today.LockedMs <= a.Today.LockedMs {
		t.Fatalf("active must stop while locked: %+v -> %+v", a.Today, b.Today)
	}
}

func TestStateEventsOnlyOnChange(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	c, err := h.hub.Subscribe(1000)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	<-c.Events() // initial snapshot
	h.run(10 * time.Second)
	e1 := <-c.Events()
	e2 := <-c.Events()
	if e1.Name != "tick" || e2.Name != "tick" {
		t.Fatalf("events = %s %s", e1.Name, e2.Name)
	}
	h.src.Update("2", func(s *collector.SessionInfo) { s.LockedHint = true })
	h.run(5 * time.Second)
	if e := <-c.Events(); e.Name != "state" {
		t.Fatalf("event = %s", e.Name)
	}
}

func TestRemovingAUserFromTheAllowlistStopsRecording(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(5 * time.Minute)
	h.cfg.MonitoredUsers = nil
	h.run(5 * time.Minute)
	h.wantMin("active", h.totals(1000).Active, 5)
	if open, _ := h.db.LoadOpenIntervals(); len(open) != 0 {
		t.Fatal("interval left open after removing the user")
	}
}

func TestSourceFailureDoesNotCrashOrInventTime(t *testing.T) {
	h := newHarness(t, "ilyes")
	login(h, "2", 1000, "ilyes", "wayland", "ubuntu")
	h.tick()
	h.run(5 * time.Minute)
	h.src.FailWith(context.DeadlineExceeded)
	h.run(20 * time.Second)
	h.src.FailWith(nil)
	h.run(5 * time.Minute)
	// a 20 s outage is shorter than the stall threshold, so the state is assumed unchanged
	if got := h.totals(1000).Active; !near(got, 10*min, 30*sec) {
		t.Fatalf("active = %.1f min", float64(got)/float64(min))
	}
	if h.tr.Health().LastError != "" {
		t.Fatal("error should clear once the source recovers")
	}
}

func TestSyncUsersRecordsAllowlistedAccounts(t *testing.T) {
	h := newHarness(t, "ilyes", "maram")
	err := h.tr.SyncUsers(func(name string) (int, bool) {
		return map[string]int{"ilyes": 1000, "maram": 1001}[name], name != "ghost"
	})
	if err != nil {
		t.Fatal(err)
	}
	us, _ := h.db.Users()
	if len(us) != 2 || !us[0].Monitored {
		t.Fatalf("users = %+v", us)
	}
	h.cfg.MonitoredUsers = []string{"maram"}
	h.tr.SyncUsers(func(name string) (int, bool) { return 1001, true })
	us, _ = h.db.Users()
	if us[0].Monitored || !us[1].Monitored {
		t.Fatalf("users = %+v", us)
	}
}

func TestDescribeNamesTheSessionAndTheNextStep(t *testing.T) {
	s := collector.UserSession("2", 1000, "ilyes", "wayland", "ubuntu")
	msg, next := Describe(ReasonExtensionMissing, "ilyes", s)
	if !strings.Contains(msg, "Wayland session") || !strings.Contains(next, "ilyes to log out and back in") {
		t.Fatalf("%q / %q", msg, next)
	}
	// X11 is never mentioned as a problem on a Wayland session (docs/07 §7.8)
	for _, r := range []string{ReasonExtensionMissing, ReasonExtensionDisabled, ReasonExtensionNoBeat, ReasonAgentNotRunning, ReasonAgentSilent} {
		m, n := Describe(r, "ilyes", s)
		if strings.Contains(m+n, "X11") || strings.Contains(m+n, "xprop") {
			t.Errorf("%s mentions X11: %q %q", r, m, n)
		}
	}
	// copy rules (DS-5): no exclamation marks
	for _, r := range []string{ReasonExtensionMissing, ReasonX11Unreachable, ReasonTTY, ReasonUnsupportedDesk, "other"} {
		m, n := Describe(r, "ilyes", s)
		if strings.Contains(m+n, "!") {
			t.Errorf("%s: exclamation mark in %q %q", r, m, n)
		}
	}
}

func TestUnsupportedDesktopMessageNamesTheDesktop(t *testing.T) {
	s := collector.UserSession("2", 1000, "maram", "wayland", "plasma")
	msg, _ := Describe(ReasonUnsupportedDesk, "maram", s)
	if !strings.Contains(msg, `Wayland session on this desktop ("plasma")`) {
		t.Fatalf("the message should say which desktop logind reported: %q", msg)
	}
	s.Desktop = ""
	if msg, _ := Describe(ReasonUnsupportedDesk, "maram", s); strings.Contains(msg, "(") {
		t.Fatalf("no desktop, nothing to quote: %q", msg)
	}
}

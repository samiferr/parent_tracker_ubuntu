// Package tracker is the sampling engine: it turns logind session state and the
// agents' foreground-app reports into activity intervals, samples, gaps and the
// live snapshot.
//
// Time is injected (Now) and Tick is explicit, so every behaviour is testable
// without a desktop or a real clock.
package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/live"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

// AgentFeed gives the tracker the latest report of the agent of a session.
type AgentFeed interface {
	Latest(sessionID string) (agentproto.Report, time.Time, bool)
}

// Notice is something the alert engine may want to tell the parent about.
type Notice struct {
	Rule     string
	UID      int
	User     string
	Severity string
	Message  string
}

// Options wires the tracker.
type Options struct {
	Cfg    func() *config.Config
	Source collector.Source
	DB     *storage.DB
	Agents AgentFeed // may be nil: no app tracking
	Hub    *live.Hub // may be nil
	Loc    *time.Location
	Log    *slog.Logger
	Now    func() time.Time
	// Notify must not block; it is called with the tracker's lock held.
	Notify func(Notice)
}

type sessRow struct {
	rowID int64
	uid   int
}

type userState struct {
	uid  int
	name string

	has     bool
	primary collector.SessionInfo

	state   model.State
	since   int64
	ivID    int64
	ivStart int64

	dayStart, dayEnd   int64
	closed             aggregate.Totals
	gapMs              int64
	hourStart, hourEnd int64
	hourClosedActive   int64

	lastSample int64
	app        *live.App
	appStatus  model.AppStatus
	published  string // change detection: state|app
}

// Tracker is the engine.
type Tracker struct {
	o Options

	mu       sync.Mutex
	users    map[int]*userState
	sessRows map[string]sessRow
	sensors  map[string]*sensorState
	lastTick int64
	sleeping bool
	skewed   bool
	warned   bool
	lastErr  string
	lastBeat int64
}

// New creates a tracker.
func New(o Options) *Tracker {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Loc == nil {
		o.Loc = time.Local
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Tracker{o: o, users: map[int]*userState{}, sessRows: map[string]sessRow{}, sensors: map[string]*sensorState{}}
}

func (t *Tracker) now() int64 { return t.o.Now().UnixMilli() }

func (t *Tracker) notify(n Notice) {
	if t.o.Notify != nil {
		t.o.Notify(n)
	}
}

func stallThreshold(cfg *config.Config) int64 {
	v := 3 * cfg.SampleInterval.D()
	if v < 30*time.Second {
		v = 30 * time.Second
	}
	return v.Milliseconds()
}

// SyncUsers records the allowlisted accounts so the dashboard can list them
// before they first sign in. lookup maps a user name to a uid.
func (t *Tracker) SyncUsers(lookup func(name string) (int, bool)) error {
	cfg := t.o.Cfg()
	now := t.now()
	known, err := t.o.DB.Users()
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, name := range cfg.MonitoredUsers {
		want[name] = true
		if uid, ok := lookup(name); ok {
			if err := t.o.DB.UpsertUser(uid, name, true, now); err != nil {
				return err
			}
		}
	}
	for _, u := range known {
		if u.Monitored && !want[u.Username] {
			if err := t.o.DB.UpsertUser(u.UID, u.Username, false, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// Recover closes what a previous run left open and records the time in between
// as a tracking gap (NFR-2.3, AC-40, AC-53).
func (t *Tracker) Recover() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	db, now := t.o.DB, t.now()
	cfg := t.o.Cfg()

	clean, _, _ := db.GetState("clean_shutdown")
	prevClean := clean == "1"
	lastAlive := db.GetStateInt("last_alive_at")

	open, err := db.LoadOpenIntervals()
	if err != nil {
		return err
	}
	for _, iv := range open {
		if err := db.CloseInterval(iv.ID, iv.End); err != nil { // end = last good instant
			return err
		}
		if iv.End > lastAlive {
			lastAlive = iv.End
		}
	}
	sessions, err := db.OpenSessions()
	if err != nil {
		return err
	}
	reason := "unclean"
	if prevClean {
		reason = "normal"
	}
	for _, s := range sessions {
		end := lastAlive
		if end < s.StartedAt {
			end = s.StartedAt
		}
		if err := db.CloseSession(s.ID, end, reason); err != nil {
			return err
		}
	}

	detail := "first start"
	if lastAlive > 0 {
		kind := model.GapCrash
		detail = "previous run ended unexpectedly"
		if prevClean {
			kind = model.GapServiceStopped
			detail = "previous run was stopped"
		}
		if now-lastAlive > 2*cfg.SampleInterval.Milliseconds() {
			if err := db.InsertGap(model.Gap{UID: 0, Start: lastAlive, End: now, Kind: kind}, detail); err != nil {
				return err
			}
			if cfg.Alerts.NotifyOnServiceStop && (!prevClean || len(sessions) > 0 || len(open) > 0) {
				t.notify(Notice{Rule: "tracking_gap", Severity: "warn",
					Message: fmt.Sprintf("Tracking was interrupted from %s to %s. %s.", t.clock(lastAlive), t.clock(now), capitalize(detail))})
			}
		}
	}
	if err := db.Audit(now, "system", model.AuditServiceStarted, detail); err != nil {
		return err
	}
	if err := db.SetState("clean_shutdown", "0"); err != nil {
		return err
	}
	return db.SetState("last_alive_at", fmt.Sprint(now))
}

func (t *Tracker) clock(ms int64) string {
	return time.UnixMilli(ms).In(t.o.Loc).Format("2006-01-02 15:04")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Stop closes everything cleanly. The next start records the time until then as
// a "service stopped" gap.
func (t *Tracker) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if now < t.lastTick {
		now = t.lastTick
	}
	t.closeAll(now, "normal")
	if err := t.o.DB.Audit(now, "system", model.AuditServiceStopped, ""); err != nil {
		return err
	}
	if err := t.o.DB.SetState("last_alive_at", fmt.Sprint(now)); err != nil {
		return err
	}
	return t.o.DB.SetState("clean_shutdown", "1")
}

// Run ticks until ctx ends: once per sampling interval, and immediately when
// logind reports a change, a suspend or a resume.
func (t *Tracker) Run(ctx context.Context) {
	cur := t.o.Cfg().SampleInterval.D()
	ticker := time.NewTicker(cur)
	defer ticker.Stop()
	t.Tick(ctx)
	var lastEval time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if want := t.o.Cfg().SampleInterval.D(); want != cur {
				cur = want
				ticker.Reset(cur)
			}
			t.Tick(ctx)
		case e := <-t.o.Source.Events():
			switch e.Kind {
			case collector.EventSleep:
				t.OnSleep(e.At)
			case collector.EventResume:
				t.OnResume(e.At)
			default:
				if time.Since(lastEval) > 250*time.Millisecond {
					lastEval = time.Now()
					t.Evaluate(ctx)
				}
			}
		}
	}
}

// OnSleep records the start of a suspend: open intervals are closed and a
// suspended interval begins (AC-5).
func (t *Tracker) OnSleep(at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ms := at.UnixMilli()
	if ms < t.lastTick {
		ms = t.lastTick
	}
	t.sleeping = true
	for _, u := range t.sortedUsers() {
		if u.ivID == 0 || u.state == model.StateSuspended {
			continue
		}
		t.closeInterval(u, ms)
		t.openInterval(u, model.StateSuspended, ms)
	}
	t.lastTick = ms
}

// OnResume ends the suspended interval.
func (t *Tracker) OnResume(at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ms := at.UnixMilli()
	if ms < t.lastTick {
		ms = t.lastTick
	}
	for _, u := range t.sortedUsers() {
		if u.ivID != 0 && u.state == model.StateSuspended {
			t.closeInterval(u, ms)
			u.state = model.StateLoggedOut
			u.since = ms
		}
	}
	t.sleeping = false
	t.lastTick = ms
}

// Tick takes one sample of everything.
func (t *Tracker) Tick(ctx context.Context) { t.tick(ctx, true) }

// Evaluate re-reads session state without taking an app sample.
func (t *Tracker) Evaluate(ctx context.Context) { t.tick(ctx, false) }

func (t *Tracker) sortedUsers() []*userState {
	out := make([]*userState, 0, len(t.users))
	for _, u := range t.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].uid < out[j].uid })
	return out
}

func (t *Tracker) tick(ctx context.Context, sample bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg := t.o.Cfg()
	now := t.now()

	if t.sleeping { // the suspended interval is open; nothing to sample
		return
	}
	if !cfg.Recording() {
		if !t.warned {
			t.warned = true
			t.o.Log.Warn("not recording: the disclosure has not been acknowledged or the session collector is off; run `sudo parent-tracker setup`")
		}
		t.closeAll(now, "normal")
		return
	}
	t.warned = false

	// Clock handling: never produce a negative duration (AC-6), and treat a long
	// silence we were not told about (e.g. a missed suspend) as a gap, not usage.
	if t.lastTick != 0 {
		if now < t.lastTick {
			if !t.skewed {
				t.skewed = true
				_ = t.o.DB.Audit(now, "system", model.AuditClockSkew, fmt.Sprintf("clock moved back by %s", time.Duration(t.lastTick-now)*time.Millisecond))
			}
			now = t.lastTick
		} else {
			t.skewed = false
			if now-t.lastTick > stallThreshold(cfg) {
				t.handleStall(t.lastTick, now)
			}
		}
	}
	t.lastTick = now

	infos, err := t.o.Source.Sessions(ctx)
	if err != nil {
		t.lastErr = err.Error()
		t.o.Log.Warn("cannot read sessions", "err", err)
		_ = t.o.DB.SetCollectorHealth(storage.CollectorHealth{Collector: "session", LastError: err.Error(), Enabled: true})
		return // keep the open intervals as they are; the stall logic covers long outages
	}
	t.lastErr = ""

	// Group the recordable sessions by user.
	byUID := map[int][]collector.SessionInfo{}
	seen := map[string]bool{}
	for _, s := range infos {
		if collector.Eligible(s, cfg) {
			byUID[s.UID] = append(byUID[s.UID], s)
			seen[s.ID] = true
		}
	}
	t.syncSessionRows(byUID, seen, now)

	for uid, list := range byUID {
		u := t.users[uid]
		if u == nil {
			u = &userState{uid: uid, name: list[0].User, state: model.StateLoggedOut, since: now}
			t.users[uid] = u
			_ = t.o.DB.UpsertUser(uid, u.name, true, now)
		}
		u.name = list[0].User
		u.has = true
		u.primary = choosePrimary(list)
	}
	for _, u := range t.users {
		if _, ok := byUID[u.uid]; !ok {
			u.has = false
		}
	}

	var samples []model.Sample
	names := map[string]string{}
	for _, u := range t.sortedUsers() {
		t.ensureWindows(u, now)
		want, idleSince := model.StateLoggedOut, int64(0)
		if u.has {
			want, idleSince = wantedState(u.primary)
		}
		t.apply(u, want, u.has, idleSince, now)
		if u.has && sample {
			t.sampleApp(cfg, u, now, &samples, names)
		} else if !u.has {
			u.app, u.appStatus = nil, model.AppOff
		}
		u.lastSample = now
		t.publish(u, now, cfg)
	}
	if len(samples) > 0 {
		if err := t.o.DB.InsertSamples(samples, names); err != nil {
			t.o.Log.Error("cannot store app samples", "err", err)
		}
	}
	if now-t.lastBeat >= 1000 {
		t.lastBeat = now
		_ = t.o.DB.SetState("last_alive_at", fmt.Sprint(now))
	}
}

// choosePrimary picks the session whose state counts: the one on the seat's
// screen if there is one, otherwise the oldest.
func choosePrimary(list []collector.SessionInfo) collector.SessionInfo {
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	for _, s := range list {
		if s.Active {
			return s
		}
	}
	return list[0]
}

// wantedState derives the state of a user's primary session. A session that is
// not on the screen (the user switched away) counts as locked. The result is the
// same for Wayland and X11 sessions (NFR-3.9). idleSince is the instant logind
// says the idle period began, so detection delay is not counted as use.
func wantedState(s collector.SessionInfo) (model.State, int64) {
	switch {
	case !s.Active:
		return model.StateLocked, 0
	case s.LockedHint:
		return model.StateLocked, 0
	case s.IdleHint:
		if s.IdleSince.IsZero() {
			return model.StateIdle, 0
		}
		return model.StateIdle, s.IdleSince.UnixMilli()
	}
	return model.StateActive, 0
}

func (t *Tracker) syncSessionRows(byUID map[int][]collector.SessionInfo, seen map[string]bool, now int64) {
	db := t.o.DB
	for id, row := range t.sessRows {
		if !seen[id] {
			_ = db.CloseSession(row.rowID, now, "normal")
			delete(t.sessRows, id)
		}
	}
	for _, list := range byUID {
		for _, s := range list {
			if _, ok := t.sessRows[s.ID]; ok {
				continue
			}
			host := "local"
			id, err := db.InsertSession(model.Session{UID: s.UID, LogindID: s.ID, StartedAt: now, State: model.StateActive,
				HostType: host, SessionType: s.Type, Desktop: s.Desktop})
			if err != nil {
				t.o.Log.Error("cannot store session", "err", err)
				continue
			}
			t.sessRows[s.ID] = sessRow{rowID: id, uid: s.UID}
		}
	}
}

func (t *Tracker) handleStall(from, to int64) {
	t.o.Log.Warn("no tick for a long time; recording a gap", "from", t.clock(from), "to", t.clock(to))
	for _, u := range t.sortedUsers() {
		if u.ivID != 0 {
			t.closeInterval(u, from)
		}
	}
	_ = t.o.DB.InsertGap(model.Gap{UID: 0, Start: from, End: to, Kind: model.GapStall}, "no tick")
	for _, u := range t.users {
		u.gapMs = 0 // reloaded with the day window below
		u.dayEnd = 0
	}
}

// apply moves a user to the wanted state, closing and opening intervals.
func (t *Tracker) apply(u *userState, want model.State, has bool, idleSince, now int64) {
	if !has {
		if u.ivID != 0 {
			t.closeInterval(u, now)
		}
		if u.state != model.StateLoggedOut {
			u.state, u.since = model.StateLoggedOut, now
		}
		return
	}
	if u.ivID == 0 {
		t.openInterval(u, want, now)
		return
	}
	if want != u.state {
		boundary := now
		if want == model.StateIdle && idleSince > u.ivStart && idleSince < now {
			boundary = idleSince // back-date idle to when it actually began
		}
		t.closeInterval(u, boundary)
		t.openInterval(u, want, boundary)
		if row, ok := t.sessRows[u.primary.ID]; ok {
			_ = t.o.DB.UpdateSessionState(row.rowID, want)
		}
		return
	}
	if err := t.o.DB.TouchInterval(u.ivID, now); err != nil {
		t.o.Log.Error("cannot extend interval", "err", err)
	}
}

func (t *Tracker) openInterval(u *userState, st model.State, at int64) {
	id, err := t.o.DB.OpenInterval(u.uid, st, at)
	if err != nil {
		t.o.Log.Error("cannot open interval", "err", err)
		return
	}
	u.ivID, u.ivStart, u.state, u.since = id, at, st, at
}

func (t *Tracker) closeInterval(u *userState, at int64) {
	if u.ivID == 0 {
		return
	}
	if at < u.ivStart {
		at = u.ivStart
	}
	if err := t.o.DB.CloseInterval(u.ivID, at); err != nil {
		t.o.Log.Error("cannot close interval", "err", err)
	}
	t.account(u, u.state, u.ivStart, at)
	u.ivID = 0
}

// account adds a closed interval to the running totals of the current windows.
func (t *Tracker) account(u *userState, st model.State, a, b int64) {
	if ms := aggregate.Overlap(a, b, u.dayStart, u.dayEnd); ms > 0 {
		u.closed.Add(st, ms)
	}
	if st == model.StateActive {
		u.hourClosedActive += aggregate.Overlap(a, b, u.hourStart, u.hourEnd)
	}
}

func (t *Tracker) closeAll(now int64, reason string) {
	for _, u := range t.sortedUsers() {
		if u.ivID != 0 {
			t.closeInterval(u, now)
		}
		if u.state != model.StateLoggedOut {
			u.state, u.since = model.StateLoggedOut, now
		}
		u.has = false
	}
	for id, row := range t.sessRows {
		_ = t.o.DB.CloseSession(row.rowID, now, reason)
		delete(t.sessRows, id)
	}
}

// ensureWindows keeps the "today" and "this hour" accumulators current,
// reloading them from the database at start-up and at midnight / each hour.
func (t *Tracker) ensureWindows(u *userState, now int64) {
	if u.dayEnd == 0 || now >= u.dayEnd || now < u.dayStart {
		u.dayStart, u.dayEnd = aggregate.DayBounds(t.o.Loc, now)
		u.closed = aggregate.Totals{}
		u.gapMs = 0
		ivs, _ := t.o.DB.Intervals(u.uid, u.dayStart, u.dayEnd)
		for _, iv := range ivs {
			if !iv.Open {
				u.closed = u.closed.Plus(aggregate.ClipTotals([]model.Interval{iv}, u.dayStart, u.dayEnd))
			}
		}
		gaps, _ := t.o.DB.Gaps(u.uid, u.dayStart, u.dayEnd)
		for _, g := range gaps {
			if model.CountsAsTrackingGap(g.Kind) {
				u.gapMs += aggregate.Overlap(g.Start, g.End, u.dayStart, u.dayEnd)
			}
		}
		u.hourEnd = 0
	}
	if u.hourEnd == 0 || now >= u.hourEnd || now < u.hourStart {
		b := aggregate.BucketAt(t.o.Loc, now)
		u.hourStart, u.hourEnd = b.Start, b.End
		u.hourClosedActive = 0
		ivs, _ := t.o.DB.Intervals(u.uid, b.Start, b.End)
		for _, iv := range ivs {
			if !iv.Open && iv.State == model.StateActive {
				u.hourClosedActive += aggregate.Overlap(iv.Start, iv.End, b.Start, b.End)
			}
		}
	}
}

// Health reports what the tracker currently sees.
type Health struct {
	Recording              bool         `json:"recording"`
	DisclosureAcknowledged bool         `json:"disclosure_acknowledged"`
	LastTickAt             int64        `json:"last_tick_at"`
	LastError              string       `json:"last_error,omitempty"`
	IntervalMs             int64        `json:"interval_ms"`
	Users                  []UserHealth `json:"users"`
}

// UserHealth is the per-user part of Health.
type UserHealth struct {
	User        string         `json:"user"`
	UID         int            `json:"uid"`
	State       string         `json:"state"`
	SessionID   string         `json:"session_id,omitempty"`
	SessionType string         `json:"session_type,omitempty"`
	Desktop     string         `json:"desktop,omitempty"`
	Provider    string         `json:"provider,omitempty"`
	AppStatus   string         `json:"app_status"`
	Degraded    *live.Degraded `json:"degraded,omitempty"`
}

// Health returns the tracker's current view, for /api/health and doctor.
func (t *Tracker) Health() Health {
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg := t.o.Cfg()
	h := Health{Recording: cfg.Recording(), DisclosureAcknowledged: cfg.DisclosureAcknowledged != "",
		LastTickAt: t.lastTick, LastError: t.lastErr, IntervalMs: cfg.SampleInterval.Milliseconds(), Users: []UserHealth{}}
	for _, u := range t.sortedUsers() {
		uh := UserHealth{User: u.name, UID: u.uid, State: string(u.state), AppStatus: string(u.appStatus)}
		if uh.AppStatus == "" {
			uh.AppStatus = string(model.AppOff)
		}
		if u.has {
			uh.SessionID, uh.SessionType = u.primary.ID, string(u.primary.Type)
			uh.Desktop, uh.Provider = u.primary.Desktop, string(collector.ProviderFor(u.primary))
			uh.Degraded = t.degradedFor(cfg, u)
		}
		h.Users = append(h.Users, uh)
	}
	return h
}

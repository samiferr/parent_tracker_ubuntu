package tracker

import (
	"fmt"
	"strings"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/live"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

// Reasons an app sensor is not working. They are stable codes; the messages
// below are what the dashboard and `doctor` show.
const (
	ReasonAgentNotRunning   = "agent_not_running"
	ReasonAgentSilent       = "agent_silent"
	ReasonExtensionMissing  = "extension_missing"
	ReasonExtensionDisabled = "extension_disabled"
	ReasonExtensionNoBeat   = "extension_no_heartbeat"
	ReasonX11Unreachable    = "x11_unreachable"
	ReasonProviderError     = "provider_error"
	ReasonProviderMismatch  = "provider_mismatch"
	ReasonTTY               = "tty"
	ReasonUnsupportedDesk   = "unsupported_desktop"
)

// sensorState tracks the foreground-app sensor of one session (SEC-35).
type sensorState struct {
	firstSeen int64
	ok        bool
	announced bool
	lostAt    int64
	reason    string
}

func (t *Tracker) sensorFor(sessionID string, now int64) *sensorState {
	ss := t.sensors[sessionID]
	if ss == nil {
		ss = &sensorState{firstSeen: now}
		t.sensors[sessionID] = ss
	}
	return ss
}

// sampleApp reads the agent's latest report for the user's primary session and
// queues an app sample when the sensor is healthy and the user is at the PC.
func (t *Tracker) sampleApp(cfg *config.Config, u *userState, now int64, out *[]model.Sample, names map[string]string) {
	u.app = nil
	provider := collector.ProviderFor(u.primary)
	if !cfg.Collectors.App || provider == model.ProviderNone {
		u.appStatus = model.AppOff
		return
	}
	ss := t.sensorFor(u.primary.ID, now)

	var (
		rep   agentproto.Report
		recv  time.Time
		found bool
	)
	if t.o.Agents != nil {
		rep, recv, found = t.o.Agents.Latest(u.primary.ID)
	}
	interval := cfg.SampleInterval.Milliseconds()
	fresh := found && now-recv.UnixMilli() <= 3*interval
	healthy := fresh && rep.Status == agentproto.StatusOK && rep.Provider == string(provider)

	if healthy {
		t.sensorOK(u, ss, provider, now)
		u.appStatus = model.AppOK
		if rep.App != nil && rep.App.ID != "" {
			u.app = &live.App{ID: rep.App.ID, Name: nameOr(rep.App.Name, rep.App.ID)}
			title := ""
			if cfg.Collectors.WindowTitle { // defence in depth: titles never pass when capture is off (AC-13, AC-86)
				title = rep.Title
				u.app.Title = title
			}
			if u.state == model.StateActive || u.state == model.StateIdle {
				*out = append(*out, model.Sample{UID: u.uid, TS: now, AppID: rep.App.ID, Title: title, PID: rep.App.PID,
					State: u.state, Source: string(provider)})
				names[rep.App.ID] = u.app.Name
			}
		}
		return
	}

	u.appStatus = model.AppDegraded
	t.sensorBad(u, ss, provider, now, reasonFor(found, fresh, rep, provider))
}

func nameOr(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

func reasonFor(found, fresh bool, rep agentproto.Report, want model.Provider) string {
	switch {
	case !found:
		return ReasonAgentNotRunning
	case !fresh:
		return ReasonAgentSilent
	case rep.Provider != string(want):
		return ReasonProviderMismatch
	}
	switch rep.Status {
	case agentproto.StatusMissing:
		return ReasonExtensionMissing
	case agentproto.StatusDisabled:
		return ReasonExtensionDisabled
	case agentproto.StatusNoHeartbeat:
		return ReasonExtensionNoBeat
	case agentproto.StatusUnreachable:
		return ReasonX11Unreachable
	}
	return ReasonProviderError
}

func definitive(reason string) bool {
	switch reason {
	case ReasonExtensionMissing, ReasonExtensionDisabled, ReasonX11Unreachable:
		return true
	}
	return false
}

func (t *Tracker) sensorOK(u *userState, ss *sensorState, p model.Provider, now int64) {
	ss.reason = ""
	wasOK := ss.ok
	ss.ok = true
	if ss.announced {
		ss.announced = false
		if ss.lostAt > 0 {
			_ = t.o.DB.InsertGap(model.Gap{UID: u.uid, Start: ss.lostAt, End: now, Kind: model.GapAppSensor}, string(p))
		}
		_ = t.o.DB.Audit(now, "system", model.AuditSensorRestored, fmt.Sprintf("%s: %s session %s", u.name, p, u.primary.ID))
		t.notify(Notice{Rule: "sensor_restored", UID: u.uid, User: u.name, Severity: "info",
			Message: fmt.Sprintf("App tracking for %s is working again.", u.name)})
	}
	if !wasOK {
		_ = t.o.DB.SetCollectorHealth(storage.CollectorHealth{Collector: collectorName(p, u.primary.ID), LastOKAt: now, Enabled: true})
	}
}

func (t *Tracker) sensorBad(u *userState, ss *sensorState, p model.Provider, now int64, reason string) {
	ss.reason = reason
	name := collectorName(p, u.primary.ID)
	if ss.ok { // it was working: this is a loss (AC-105)
		ss.ok, ss.announced, ss.lostAt = false, true, now
		_ = t.o.DB.Audit(now, "system", model.AuditSensorLost, fmt.Sprintf("%s: %s session %s: %s", u.name, p, u.primary.ID, reason))
		_ = t.o.DB.SetCollectorHealth(storage.CollectorHealth{Collector: name, LastError: reason, Enabled: true})
		msg, _ := Describe(reason, u.name, u.primary)
		t.notify(Notice{Rule: "sensor_lost", UID: u.uid, User: u.name, Severity: "warn", Message: msg})
		return
	}
	if ss.announced {
		return
	}
	grace := t.o.Cfg().SampleInterval.Milliseconds()*3 + 30_000
	if definitive(reason) || now-ss.firstSeen >= grace {
		ss.announced, ss.lostAt = true, 0
		_ = t.o.DB.Audit(now, "system", model.AuditSensorUnavailable, fmt.Sprintf("%s: %s session %s: %s", u.name, p, u.primary.ID, reason))
		_ = t.o.DB.SetCollectorHealth(storage.CollectorHealth{Collector: name, LastError: reason, Enabled: true})
		msg, _ := Describe(reason, u.name, u.primary)
		t.notify(Notice{Rule: "sensor_missing", UID: u.uid, User: u.name, Severity: "warn", Message: msg})
	}
}

func collectorName(p model.Provider, session string) string {
	return fmt.Sprintf("foreground:%s:%s", p, session)
}

// Describe turns a reason code into the sentence shown to the parent and the
// next step, naming the session type (docs/07 §7.8).
func Describe(reason, user string, s collector.SessionInfo) (message, nextStep string) {
	kind := "session"
	switch s.Type {
	case model.SessionWayland:
		kind = "Wayland session"
	case model.SessionX11:
		kind = "X11 session"
	case model.SessionTTY:
		kind = "text console session"
	}
	switch reason {
	case ReasonExtensionMissing:
		return fmt.Sprintf("App tracking limited in a Wayland session. The GNOME Shell extension is not enabled for %s.", user),
			fmt.Sprintf("Ask %s to log out and back in, then run `parent-tracker doctor`.", user)
	case ReasonExtensionDisabled:
		return fmt.Sprintf("App tracking limited in a Wayland session. The GNOME Shell extension is turned off in %s's session.", user),
			fmt.Sprintf("Ask %s to turn it back on, or to log out and back in, then run `parent-tracker doctor`.", user)
	case ReasonExtensionNoBeat:
		return fmt.Sprintf("App tracking limited in a Wayland session. The GNOME Shell extension is not answering in %s's session.", user),
			fmt.Sprintf("Ask %s to log out and back in, then run `parent-tracker doctor`.", user)
	case ReasonAgentNotRunning:
		return fmt.Sprintf("App tracking limited in %s's %s. The tracker's agent is not running.", user, kind),
			fmt.Sprintf("Ask %s to log out and back in, then run `parent-tracker doctor`.", user)
	case ReasonAgentSilent:
		return fmt.Sprintf("App tracking limited in %s's %s. The tracker's agent stopped reporting.", user, kind),
			fmt.Sprintf("Ask %s to log out and back in, then run `parent-tracker doctor`.", user)
	case ReasonX11Unreachable:
		return fmt.Sprintf("App tracking limited in an X11 session. The agent cannot read the active window in %s's session.", user),
			"Check that the window manager supports the standard active-window property, then run `parent-tracker doctor`."
	case ReasonTTY:
		return "App tracking is not available in a text console session.", "Screen time is still recorded."
	case ReasonUnsupportedDesk:
		on := "on this desktop"
		if d := strings.TrimSpace(s.Desktop); d != "" {
			on = fmt.Sprintf("on this desktop (%q)", d)
		}
		return fmt.Sprintf("App tracking is not available in %s's %s %s.", user, kind, on), "Screen time is still recorded."
	}
	return fmt.Sprintf("App tracking limited in %s's %s.", user, kind), "Run `parent-tracker doctor` for details."
}

// degradedFor describes a user's current app-tracking problem, or nil.
func (t *Tracker) degradedFor(cfg *config.Config, u *userState) *live.Degraded {
	if !cfg.Collectors.App || !u.has {
		return nil
	}
	p := collector.ProviderFor(u.primary)
	d := func(reason string) *live.Degraded {
		msg, next := Describe(reason, u.name, u.primary)
		return &live.Degraded{Collector: "foreground", Session: u.primary.ID, SessionType: string(u.primary.Type),
			Provider: string(p), Reason: reason, Message: msg, NextStep: next}
	}
	if p == model.ProviderNone {
		if u.primary.Type == model.SessionTTY {
			return d(ReasonTTY)
		}
		return d(ReasonUnsupportedDesk)
	}
	ss := t.sensors[u.primary.ID]
	if ss == nil || ss.ok || ss.reason == "" {
		return nil
	}
	if !ss.announced && !definitive(ss.reason) { // still within the start-up grace period
		return nil
	}
	return d(ss.reason)
}

// publish sends the user's snapshot to the live hub.
func (t *Tracker) publish(u *userState, now int64, cfg *config.Config) {
	if t.o.Hub == nil {
		return
	}
	tot := u.closed
	hourActive := u.hourClosedActive
	if u.ivID != 0 {
		tot.Add(u.state, aggregate.Overlap(u.ivStart, now, u.dayStart, u.dayEnd))
		if u.state == model.StateActive {
			hourActive += aggregate.Overlap(u.ivStart, now, u.hourStart, u.hourEnd)
		}
	}
	snap := live.Snapshot{
		User: u.name, UID: u.uid, State: string(u.state), Since: u.since, App: u.app, AppStatus: string(u.appStatus),
		Today:        live.Totals{ActiveMs: tot.Active, IdleMs: tot.Idle, LockedMs: tot.Locked, GapMs: u.gapMs},
		Hour:         live.Hour{HourStart: u.hourStart, ActiveMs: hourActive},
		LastSampleAt: now, IntervalMs: cfg.SampleInterval.Milliseconds(), ServerTime: now, Degraded: []live.Degraded{},
	}
	if snap.AppStatus == "" {
		snap.AppStatus = string(model.AppOff)
	}
	if u.has {
		snap.SessionType = string(u.primary.Type)
		snap.Provider = string(collector.ProviderFor(u.primary))
		if d := t.degradedFor(cfg, u); d != nil {
			snap.Degraded = append(snap.Degraded, *d)
		}
	}
	key := snap.State + "|"
	if snap.App != nil {
		key += snap.App.ID + "|" + snap.App.Title
	}
	event := "tick"
	if key != u.published {
		event, u.published = "state", key
	}
	t.o.Hub.Publish(u.uid, snap, event)
}

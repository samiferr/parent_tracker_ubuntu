package view

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// Event is one entry of the Alerts and tracking gaps timeline.
type Event struct {
	TS          int64  `json:"ts"`
	Kind        string `json:"kind"`    // alert | gap | audit
	Variant     string `json:"variant"` // neutral | informative | notice | negative
	Title       string `json:"title"`
	Description string `json:"description"`
	User        string `json:"user,omitempty"`
}

// EventsView is the Alerts screen's data.
type EventsView struct {
	Events []Event        `json:"events"`
	Counts map[string]int `json:"counts"`
}

var alertTitles = map[string]string{
	"daily_limit":    "Daily limit reached",
	"bedtime":        "Activity after bedtime",
	"tracking_gap":   "Tracking interrupted",
	"sensor_lost":    "App sensor lost",
	"sensor_missing": "App tracking limited",
	"disk_low":       "Disk space is low",
}

var auditTitles = map[string]struct{ title, variant string }{
	model.AuditServiceStarted:    {"Service started", "informative"},
	model.AuditServiceStopped:    {"Service stopped", "notice"},
	model.AuditConfigChanged:     {"Settings changed", "informative"},
	model.AuditUsersChanged:      {"Monitored users changed", "informative"},
	model.AuditCollectorDisabled: {"Collector turned off", "notice"},
	model.AuditDataDeleted:       {"Recorded data deleted", "notice"},
	model.AuditSensorLost:        {"App sensor lost", "negative"},
	model.AuditSensorRestored:    {"App sensor restored", "informative"},
	model.AuditSensorUnavailable: {"App tracking limited", "negative"},
	model.AuditClockSkew:         {"Clock changed", "notice"},
	model.AuditDisclosureAck:     {"Disclosure acknowledged", "informative"},
	model.AuditSetup:             {"Setup run", "informative"},
	model.AuditError:             {"Service error", "negative"},
	model.AuditWarning:           {"Service warning", "notice"},
}

var gapReasons = map[string]string{
	model.GapServiceStopped: "The service was stopped.",
	model.GapCrash:          "The service stopped unexpectedly.",
	model.GapUnclean:        "A session ended without a clean logout.",
	model.GapStall:          "The service was not running for a while.",
}

// Events merges alerts, tracking gaps and audit entries for [from,to), newest
// first. uid 0 means every user.
func (v *Viewer) Events(uid int, from, to int64, limit int) (*EventsView, error) {
	names := map[int]string{}
	users, err := v.DB.Users()
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		names[u.UID] = u.Username
	}
	var evs []Event

	alerts, err := v.DB.Alerts(from, to, limit)
	if err != nil {
		return nil, err
	}
	for _, a := range alerts {
		if uid != 0 && a.UID != 0 && a.UID != uid {
			continue
		}
		title := alertTitles[a.RuleID]
		if title == "" {
			title = a.RuleID
		}
		variant := "notice"
		if a.Severity == "critical" {
			variant = "negative"
		} else if a.Severity == "info" {
			variant = "informative"
		}
		evs = append(evs, Event{TS: a.CreatedAt, Kind: "alert", Variant: variant, Title: title, Description: a.Message, User: names[a.UID]})
	}

	if uid != 0 {
		gaps, err := v.DB.Gaps(uid, from, to)
		if err != nil {
			return nil, err
		}
		for _, g := range gaps {
			if !model.CountsAsTrackingGap(g.Kind) {
				continue
			}
			evs = append(evs, Event{TS: g.Start, Kind: "gap", Variant: "notice", Title: "Tracking gap",
				Description: fmt.Sprintf("Nothing was recorded from %s to %s (%s). %s",
					time.UnixMilli(g.Start).In(v.Loc).Format("2006-01-02 15:04"), time.UnixMilli(g.End).In(v.Loc).Format("15:04"),
					FmtHMSpaced(g.End-g.Start), gapReasons[g.Kind]), User: names[uid]})
		}
	}

	audit, err := v.DB.AuditEvents(from, to, limit)
	if err != nil {
		return nil, err
	}
	for _, a := range audit {
		t, ok := auditTitles[a.Action]
		if !ok {
			t.title, t.variant = a.Action, "neutral"
		}
		evs = append(evs, Event{TS: a.TS, Kind: "audit", Variant: t.variant, Title: t.title, Description: auditDetail(a)})
	}

	sort.SliceStable(evs, func(i, j int) bool { return evs[i].TS > evs[j].TS })
	if limit > 0 && len(evs) > limit {
		evs = evs[:limit]
	}
	out := &EventsView{Events: evs, Counts: map[string]int{"alert": 0, "gap": 0, "audit": 0}}
	if out.Events == nil {
		out.Events = []Event{}
	}
	for _, e := range evs {
		out.Counts[e.Kind]++
	}
	return out, nil
}

// sensorReasons are the plain-language forms of the tracker's sensor reason
// codes, as they appear in a sentence ("because ...").
var sensorReasons = map[string]string{
	"agent_not_running":      "the tracker's agent is not running in the session",
	"agent_silent":           "the tracker's agent stopped reporting",
	"extension_missing":      "the GNOME Shell extension is not enabled",
	"extension_disabled":     "the GNOME Shell extension was turned off",
	"extension_no_heartbeat": "the GNOME Shell extension stopped answering",
	"x11_unreachable":        "the agent cannot read the active window",
	"x11_no_ewmh":            "the window manager does not report the active window",
	"provider_error":         "the foreground-app sensor reported an error",
	"provider_mismatch":      "the session type changed",
	"tty":                    "text console sessions have no foreground app",
	"unsupported_desktop":    "this desktop is not supported",
}

var providerNames = map[string]string{"x11": "X11", "gnome-wayland": "Wayland (GNOME)"}

// sensorDetail is "user: provider session N[: reason]", as written by the tracker.
var sensorDetail = regexp.MustCompile(`(?s)^([^:]+): ([a-z0-9-]+) session ([^:]+)(?:: (.+))?$`)

func auditDetail(a model.AuditEvent) string {
	if a.Detail == "" || a.Detail == "{}" {
		return ""
	}
	switch a.Action {
	case model.AuditSensorLost, model.AuditSensorRestored, model.AuditSensorUnavailable:
		if m := sensorDetail.FindStringSubmatch(a.Detail); m != nil {
			prov := m[2]
			if n, ok := providerNames[prov]; ok {
				prov = n
			}
			who := fmt.Sprintf("%s's %s session", m[1], prov)
			if a.Action == model.AuditSensorRestored {
				return who + " is reporting again."
			}
			code, tech, _ := strings.Cut(m[4], sensorTechSep)
			tech = strings.Join(strings.Fields(tech), " ")
			why, ok := sensorReasons[code]
			if !ok {
				why = strings.ReplaceAll(code, "_", " ")
			}
			if code == "" {
				return who + "."
			}
			out := who + ": " + why + "."
			if tech != "" {
				out += " The agent said: " + tech
			}
			return out
		}
	}
	return a.Detail
}

// sensorTechSep separates the reason code from the agent's own error text in
// an app-sensor audit entry (see tracker.auditSensor).
const sensorTechSep = " | "

// AuditEntry is one row of the Audit log.
type AuditEntry struct {
	ID          int64  `json:"id"`
	TS          int64  `json:"ts"`
	Level       string `json:"level"` // error | warning | info
	Actor       string `json:"actor"`
	Action      string `json:"action"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Detail is the stored text as written, for support and for the export.
	Detail string `json:"detail"`
}

// AuditView is the Audit log screen's data: the entries of a period, newest
// first, and how many entries of each level the period holds.
type AuditView struct {
	From      string         `json:"from"`
	To        string         `json:"to"`
	Level     string         `json:"level"`
	Entries   []AuditEntry   `json:"entries"`
	Counts    map[string]int `json:"counts"` // by level, before the level filter
	Total     int            `json:"total"`  // entries matching the level filter
	Truncated bool           `json:"truncated"`
}

// MaxAuditRows bounds how many rows one request reads.
const MaxAuditRows = 100_000

// Audit lists the audit log for fromDay..toDay inclusive (local days). level is
// "", "all", "error", "warning" or "info". limit caps the entries returned
// (0 for no cap other than MaxAuditRows); Counts and Total always cover the
// whole period.
func (v *Viewer) Audit(fromDay, toDay, level string, limit int) (*AuditView, error) {
	fs, _, err := aggregate.ParseDay(v.Loc, fromDay)
	if err != nil {
		return nil, fmt.Errorf("from must be YYYY-MM-DD: %w", err)
	}
	_, te, err := aggregate.ParseDay(v.Loc, toDay)
	if err != nil {
		return nil, fmt.Errorf("to must be YYYY-MM-DD: %w", err)
	}
	if te <= fs {
		return nil, fmt.Errorf("to must not be before from")
	}
	if (te-fs)/(24*3600*1000) > 400 {
		return nil, fmt.Errorf("the range is longer than 400 days")
	}
	switch level {
	case "", "all":
		level = "all"
	case model.LevelError, model.LevelWarning, model.LevelInfo:
	default:
		return nil, fmt.Errorf("level must be all, error, warning or info")
	}
	rows, err := v.DB.AuditEvents(fs, te, MaxAuditRows)
	if err != nil {
		return nil, err
	}
	out := &AuditView{From: fromDay, To: toDay, Level: level, Entries: []AuditEntry{},
		Counts: map[string]int{model.LevelError: 0, model.LevelWarning: 0, model.LevelInfo: 0}, Truncated: len(rows) >= MaxAuditRows}
	for _, a := range rows {
		lv := model.AuditLevel(a.Action)
		out.Counts[lv]++
		if level != "all" && lv != level {
			continue
		}
		out.Total++
		if limit > 0 && len(out.Entries) >= limit {
			continue
		}
		t, ok := auditTitles[a.Action]
		if !ok {
			t.title = a.Action
		}
		out.Entries = append(out.Entries, AuditEntry{ID: a.ID, TS: a.TS, Level: lv, Actor: a.Actor, Action: a.Action,
			Title: t.title, Description: auditDetail(a), Detail: a.Detail})
	}
	return out, nil
}

// DefaultEventRange is the window shown by default: the last 30 days.
func (v *Viewer) DefaultEventRange() (from, to int64) {
	now := v.now()
	_, de := aggregate.DayBounds(v.Loc, now)
	return de - 30*24*3600*1000, de
}

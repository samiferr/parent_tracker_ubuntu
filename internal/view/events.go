package view

import (
	"fmt"
	"sort"
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
	model.AuditSensorLost:        {"App sensor lost", "notice"},
	model.AuditSensorRestored:    {"App sensor restored", "informative"},
	model.AuditSensorUnavailable: {"App tracking limited", "notice"},
	model.AuditClockSkew:         {"Clock changed", "notice"},
	model.AuditDisclosureAck:     {"Disclosure acknowledged", "informative"},
	model.AuditSetup:             {"Setup run", "informative"},
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
					FmtHM(g.End-g.Start), gapReasons[g.Kind]), User: names[uid]})
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

func auditDetail(a model.AuditEvent) string {
	if a.Detail == "" || a.Detail == "{}" {
		return ""
	}
	return a.Detail
}

// DefaultEventRange is the window shown by default: the last 30 days.
func (v *Viewer) DefaultEventRange() (from, to int64) {
	now := v.now()
	_, de := aggregate.DayBounds(v.Loc, now)
	return de - 30*24*3600*1000, de
}

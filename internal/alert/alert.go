// Package alert records alerts and delivers them to the parent (docs/07 §7.4).
//
// Tamper-evidence alerts — tracking gaps, a lost app sensor, a full disk — are
// always recorded so they appear in the dashboard (SEC-30, SEC-35); whether and
// where they are *sent* is up to alerts.enabled and alerts.channel. Threshold
// rules (daily limit, bedtime) run only when alerts are enabled. Every rule is
// limited to one alert per user per day (FR-7.3).
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
)

// alwaysRecorded are the rules recorded even when alerts are not enabled.
var alwaysRecorded = map[string]bool{"tracking_gap": true, "sensor_lost": true, "sensor_missing": true, "disk_low": true}

// maxAttempts bounds delivery retries of one alert.
const maxAttempts = 5

// Engine evaluates rules and delivers alerts.
type Engine struct {
	DB       *storage.DB
	Cfg      func() *config.Config
	Loc      *time.Location
	Now      func() time.Time
	Log      *slog.Logger
	HTTP     *http.Client
	Sessions func(ctx context.Context) ([]collector.SessionInfo, error)
	// Exec runs a command; replaced in tests.
	Exec func(ctx context.Context, name string, args ...string) error

	in       chan tracker.Notice
	mu       sync.Mutex
	attempts map[int64]int
}

// New finishes setting up an engine (defaults, queue) and returns it.
func New(e *Engine) *Engine {
	e.in = make(chan tracker.Notice, 64)
	e.attempts = map[int64]int{}
	if e.HTTP == nil {
		e.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.Loc == nil {
		e.Loc = time.Local
	}
	if e.Exec == nil {
		e.Exec = func(ctx context.Context, name string, args ...string) error {
			return exec.CommandContext(ctx, name, args...).Run()
		}
	}
	return e
}

func (e *Engine) log() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.Default()
}

// Notify queues a notice from the tracker or pruner. It never blocks.
func (e *Engine) Notify(n tracker.Notice) {
	select {
	case e.in <- n:
	default:
		e.log().Warn("alert queue is full; dropping a notice", "rule", n.Rule)
	}
}

// Run processes notices and evaluates rules every minute until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-e.in:
			e.Handle(ctx, n)
		case <-tick.C:
			e.Evaluate(ctx)
		}
	}
}

// Handle records a notice as an alert (once per rule per user per day) and
// delivers it.
func (e *Engine) Handle(ctx context.Context, n tracker.Notice) {
	if n.Rule == "sensor_restored" { // an audit entry is enough
		return
	}
	cfg := e.Cfg()
	if !cfg.Alerts.Enabled && !alwaysRecorded[n.Rule] {
		return
	}
	ds, de := aggregate.DayBounds(e.Loc, e.Now().UnixMilli())
	e.raise(ctx, cfg, n, ds, de)
}

func (e *Engine) raise(ctx context.Context, cfg *config.Config, n tracker.Notice, from, to int64) {
	if ok, err := e.DB.AlertExists(n.Rule, n.UID, from, to); err != nil || ok {
		return
	}
	sev := n.Severity
	if sev == "" {
		sev = "warn"
	}
	id, err := e.DB.InsertAlert(model.Alert{CreatedAt: e.Now().UnixMilli(), RuleID: n.Rule, UID: n.UID, Severity: sev, Message: n.Message})
	if err != nil {
		e.log().Error("cannot store alert", "err", err)
		return
	}
	e.deliver(ctx, cfg, id, n.Rule, n.User, sev, n.Message)
}

func (e *Engine) deliver(ctx context.Context, cfg *config.Config, id int64, rule, user, sev, msg string) {
	if !cfg.Alerts.Enabled || cfg.Alerts.Channel == "none" {
		return
	}
	e.mu.Lock()
	e.attempts[id]++
	e.mu.Unlock()
	var err error
	switch cfg.Alerts.Channel {
	case "ntfy":
		err = e.sendNtfy(ctx, cfg.Alerts.NtfyURL, rule, sev, msg)
	case "webhook":
		err = e.sendWebhook(ctx, cfg.Alerts.WebhookURL, rule, user, sev, msg)
	case "desktop":
		err = e.sendDesktop(ctx, cfg, msg)
	}
	if err != nil {
		e.log().Warn("alert not delivered", "rule", rule, "channel", cfg.Alerts.Channel, "err", redact(err))
		return
	}
	_ = e.DB.MarkAlertDelivered(id, cfg.Alerts.Channel)
}

// redact removes URLs (which can hold a private ntfy topic) from an error
// (SEC-22).
func redact(err error) string {
	var ue *url.Error
	if asURLError(err, &ue) {
		return ue.Op + ": " + ue.Err.Error()
	}
	return err.Error()
}

func asURLError(err error, target **url.Error) bool {
	for err != nil {
		if ue, ok := err.(*url.Error); ok {
			*target = ue
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func (e *Engine) sendNtfy(ctx context.Context, target, rule, sev, msg string) error {
	if target == "" {
		return fmt.Errorf("alerts.ntfy_url is not set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(msg))
	if err != nil {
		return fmt.Errorf("bad ntfy url")
	}
	req.Header.Set("Title", "Parent Tracker")
	req.Header.Set("Tags", "eyes")
	req.Header.Set("Priority", map[string]string{"critical": "5", "warn": "4", "info": "3"}[sev])
	return e.do(req)
}

func (e *Engine) sendWebhook(ctx context.Context, target, rule, user, sev, msg string) error {
	if target == "" {
		return fmt.Errorf("alerts.webhook_url is not set")
	}
	body, _ := json.Marshal(map[string]any{"source": "parent-tracker", "rule": rule, "user": user, "severity": sev,
		"message": msg, "time": e.Now().UTC().Format(time.RFC3339)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("bad webhook url")
	}
	req.Header.Set("Content-Type", "application/json")
	return e.do(req)
}

func (e *Engine) do(req *http.Request) error {
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("the server answered %s", resp.Status)
	}
	return nil
}

// sendDesktop shows a notification to every local, non-monitored user who is
// logged in graphically: the parent, not the child (notify-send, per user bus).
func (e *Engine) sendDesktop(ctx context.Context, cfg *config.Config, msg string) error {
	if e.Sessions == nil {
		return fmt.Errorf("no session source")
	}
	sessions, err := e.Sessions(ctx)
	if err != nil {
		return err
	}
	sent := 0
	seen := map[int]bool{}
	for _, s := range sessions {
		if s.Class != "user" || s.Remote || cfg.IsMonitored(s.User) || seen[s.UID] ||
			(s.Type != model.SessionX11 && s.Type != model.SessionWayland) {
			continue
		}
		seen[s.UID] = true
		bus := "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/" + strconv.Itoa(s.UID) + "/bus"
		if err := e.Exec(ctx, "runuser", "-u", s.User, "--", "env", bus, "notify-send", "--app-name=Parent Tracker", "Parent Tracker", msg); err == nil {
			sent++
		}
	}
	if sent == 0 {
		return fmt.Errorf("no signed-in parent session to notify")
	}
	return nil
}

// Evaluate runs the threshold rules and retries undelivered alerts.
func (e *Engine) Evaluate(ctx context.Context) {
	cfg := e.Cfg()
	now := e.Now()
	nowMs := now.UnixMilli()
	if cfg.Alerts.Enabled {
		users, err := e.DB.Users()
		if err != nil {
			return
		}
		for _, u := range users {
			if !u.Monitored || !cfg.Recording() {
				continue
			}
			e.dailyLimit(ctx, cfg, u, nowMs)
			e.bedtime(ctx, cfg, u, nowMs)
		}
	}
	e.retry(ctx, cfg, nowMs)
}

func (e *Engine) dailyLimit(ctx context.Context, cfg *config.Config, u model.User, now int64) {
	limit := cfg.Alerts.DailyActiveLimit.D()
	if limit <= 0 {
		return
	}
	ds, de := aggregate.DayBounds(e.Loc, now)
	ivs, err := e.DB.Intervals(u.UID, ds, de)
	if err != nil {
		return
	}
	active := time.Duration(aggregate.ClipTotals(ivs, ds, now).Active) * time.Millisecond
	if active < limit {
		return
	}
	e.raise(ctx, cfg, tracker.Notice{Rule: "daily_limit", UID: u.UID, User: u.Username, Severity: "warn",
		Message: fmt.Sprintf("%s has used the PC for %s today, which reaches the daily limit of %s.", u.Username, hm(active), hm(limit))}, ds, de)
}

func (e *Engine) bedtime(ctx context.Context, cfg *config.Config, u model.User, now int64) {
	if cfg.Alerts.Bedtime == "" {
		return
	}
	bed, err := config.ParseClock(cfg.Alerts.Bedtime)
	if err != nil || !aggregate.InBedtime(e.Loc, now, bed) {
		return
	}
	ivs, err := e.DB.Intervals(u.UID, now-3*60*1000, now+1)
	if err != nil {
		return
	}
	active := false
	for _, iv := range ivs {
		active = active || (iv.State == model.StateActive && iv.End >= now-90*1000)
	}
	if !active {
		return
	}
	// The night runs from 06:00 to 06:00, so use after midnight belongs to the
	// evening that began it.
	ds, _ := aggregate.DayBounds(e.Loc, now-6*3600*1000)
	from := ds + 6*3600*1000
	e.raise(ctx, cfg, tracker.Notice{Rule: "bedtime", UID: u.UID, User: u.Username, Severity: "warn",
		Message: fmt.Sprintf("%s was active at %s, after bedtime (%s).", u.Username, time.UnixMilli(now).In(e.Loc).Format("15:04"), cfg.Alerts.Bedtime)}, from, from+24*3600*1000)
}

func (e *Engine) retry(ctx context.Context, cfg *config.Config, now int64) {
	if !cfg.Alerts.Enabled || cfg.Alerts.Channel == "none" {
		return
	}
	users, _ := e.DB.Users()
	names := map[int]string{}
	for _, u := range users {
		names[u.UID] = u.Username
	}
	recent, err := e.DB.Alerts(now-3600*1000, now+1, 50)
	if err != nil {
		return
	}
	for _, a := range recent {
		if a.Delivered {
			continue
		}
		e.mu.Lock()
		n := e.attempts[a.ID]
		e.mu.Unlock()
		if n == 0 || n >= maxAttempts { // never attempted (stored while delivery was off) or given up
			continue
		}
		e.deliver(ctx, cfg, a.ID, a.RuleID, names[a.UID], a.Severity, a.Message)
	}
}

func hm(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m >= 60 {
		if m%60 == 0 {
			return fmt.Sprintf("%d h", m/60)
		}
		return fmt.Sprintf("%d h %02d m", m/60, m%60)
	}
	return fmt.Sprintf("%d m", m)
}

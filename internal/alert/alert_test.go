package alert

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
)

type rig struct {
	t   *testing.T
	db  *storage.DB
	cfg *config.Config
	now time.Time
	e   *Engine
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	r := &rig{t: t, db: db, cfg: config.Default(), now: time.Date(2026, 10, 11, 22, 30, 0, 0, time.UTC)}
	r.cfg.MonitoredUsers = []string{"ilyes"}
	r.cfg.DisclosureAcknowledged = "2026-10-01T00:00:00Z"
	db.UpsertUser(1000, "ilyes", true, 1)
	r.e = New(&Engine{DB: db, Cfg: func() *config.Config { return r.cfg }, Loc: time.UTC, Now: func() time.Time { return r.now }})
	return r
}

func (r *rig) alerts() []model.Alert {
	a, _ := r.db.Alerts(0, 1<<60, 100)
	return a
}

func (r *rig) active(from, to time.Time) {
	id, _ := r.db.OpenInterval(1000, model.StateActive, from.UnixMilli())
	r.db.CloseInterval(id, to.UnixMilli())
}

func TestTamperEvidenceIsRecordedEvenWhenAlertsAreOff(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.e.Handle(ctx, tracker.Notice{Rule: "sensor_lost", UID: 1000, User: "ilyes", Severity: "warn", Message: "App tracking limited in a Wayland session."})
	r.e.Handle(ctx, tracker.Notice{Rule: "tracking_gap", Severity: "warn", Message: "Tracking was interrupted."})
	r.e.Handle(ctx, tracker.Notice{Rule: "daily_limit", UID: 1000, Message: "should not be recorded while disabled"})
	got := r.alerts()
	if len(got) != 2 {
		t.Fatalf("alerts = %+v", got)
	}
	for _, a := range got {
		if a.Delivered {
			t.Fatal("nothing is sent while alerts.enabled is false")
		}
	}
}

// FR-7.3: one alert per rule per user per day.
func TestRateLimitedPerRulePerUserPerDay(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	n := tracker.Notice{Rule: "sensor_lost", UID: 1000, User: "ilyes", Message: "lost"}
	r.e.Handle(ctx, n)
	r.e.Handle(ctx, n)
	r.e.Handle(ctx, tracker.Notice{Rule: "sensor_lost", UID: 1001, User: "maram", Message: "lost"})
	if got := r.alerts(); len(got) != 2 {
		t.Fatalf("alerts = %+v", got)
	}
	r.now = r.now.Add(24 * time.Hour)
	r.e.Handle(ctx, n)
	if got := r.alerts(); len(got) != 3 {
		t.Fatalf("a new day allows a new alert: %+v", got)
	}
	r.e.Handle(ctx, tracker.Notice{Rule: "sensor_restored", UID: 1000, Message: "back"})
	if got := r.alerts(); len(got) != 3 {
		t.Fatal("restored is an audit entry, not an alert")
	}
}

func TestNtfyDelivery(t *testing.T) {
	r := newRig(t)
	var mu sync.Mutex
	var gotBody, gotTitle, gotPrio string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		mu.Lock()
		gotBody, gotTitle, gotPrio = string(b), req.Header.Get("Title"), req.Header.Get("Priority")
		mu.Unlock()
	}))
	defer srv.Close()
	r.cfg.Alerts.Enabled, r.cfg.Alerts.Channel, r.cfg.Alerts.NtfyURL = true, "ntfy", srv.URL+"/my-private-topic"

	r.e.Handle(context.Background(), tracker.Notice{Rule: "sensor_lost", UID: 1000, User: "ilyes", Severity: "warn", Message: "App tracking limited."})
	mu.Lock()
	defer mu.Unlock()
	if gotBody != "App tracking limited." || gotTitle != "Parent Tracker" || gotPrio != "4" {
		t.Fatalf("ntfy got %q %q %q", gotBody, gotTitle, gotPrio)
	}
	a := r.alerts()
	if len(a) != 1 || !a[0].Delivered || a[0].DeliveredVia != "ntfy" {
		t.Fatalf("alerts = %+v", a)
	}
}

func TestWebhookDeliveryAndRetry(t *testing.T) {
	r := newRig(t)
	var mu sync.Mutex
	calls := 0
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		json.NewDecoder(req.Body).Decode(&payload)
	}))
	defer srv.Close()
	r.cfg.Alerts.Enabled, r.cfg.Alerts.Channel, r.cfg.Alerts.WebhookURL = true, "webhook", srv.URL

	ctx := context.Background()
	r.e.Handle(ctx, tracker.Notice{Rule: "tracking_gap", Severity: "warn", Message: "gap"})
	if a := r.alerts(); len(a) != 1 || a[0].Delivered {
		t.Fatalf("first attempt should fail: %+v", a)
	}
	// the failed alert is recorded regardless of delivery (docs/USER-GUIDE §9) and retried
	r.now = r.now.Add(time.Minute)
	r.e.Evaluate(ctx)
	a := r.alerts()
	if len(a) != 1 || !a[0].Delivered || a[0].DeliveredVia != "webhook" {
		t.Fatalf("retry: %+v", a)
	}
	mu.Lock()
	defer mu.Unlock()
	if payload["rule"] != "tracking_gap" || payload["message"] != "gap" || payload["source"] != "parent-tracker" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestRetryGivesUp(t *testing.T) {
	r := newRig(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; http.Error(w, "no", 500) }))
	defer srv.Close()
	r.cfg.Alerts.Enabled, r.cfg.Alerts.Channel, r.cfg.Alerts.WebhookURL = true, "webhook", srv.URL
	ctx := context.Background()
	r.e.Handle(ctx, tracker.Notice{Rule: "tracking_gap", Message: "gap"})
	for i := 0; i < 20; i++ {
		r.now = r.now.Add(time.Minute)
		r.e.Evaluate(ctx)
	}
	if calls != maxAttempts {
		t.Fatalf("delivery attempts = %d, want %d", calls, maxAttempts)
	}
}

func TestDesktopNotifiesTheParentNotTheChild(t *testing.T) {
	r := newRig(t)
	r.cfg.Alerts.Enabled, r.cfg.Alerts.Channel = true, "desktop"
	var cmds [][]string
	r.e.Exec = func(_ context.Context, name string, args ...string) error {
		cmds = append(cmds, append([]string{name}, args...))
		return nil
	}
	r.e.Sessions = func(context.Context) ([]collector.SessionInfo, error) {
		return []collector.SessionInfo{
			collector.UserSession("1", 1000, "ilyes", "wayland", "ubuntu"), // the child
			collector.UserSession("2", 1500, "sami", "wayland", "ubuntu"),  // the parent
			{ID: "3", UID: 1500, User: "sami", Class: "user", Type: model.SessionTTY},
		}, nil
	}
	r.e.Handle(context.Background(), tracker.Notice{Rule: "sensor_lost", UID: 1000, User: "ilyes", Message: "App tracking limited."})
	if len(cmds) != 1 {
		t.Fatalf("commands = %v", cmds)
	}
	joined := strings.Join(cmds[0], " ")
	for _, want := range []string{"runuser -u sami", "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1500/bus", "notify-send", "App tracking limited."} {
		if !strings.Contains(joined, want) {
			t.Errorf("command %q lacks %q", joined, want)
		}
	}
	if strings.Contains(joined, "ilyes ") && strings.Contains(joined, "-u ilyes") {
		t.Fatal("the monitored child must never receive the parent's alerts")
	}
	if a := r.alerts(); len(a) != 1 || !a[0].Delivered || a[0].DeliveredVia != "desktop" {
		t.Fatalf("alerts = %+v", a)
	}

	// nobody to notify: recorded, not delivered
	r.e.Sessions = func(context.Context) ([]collector.SessionInfo, error) { return nil, nil }
	r.now = r.now.Add(24 * time.Hour)
	r.e.Handle(context.Background(), tracker.Notice{Rule: "sensor_lost", UID: 1000, User: "ilyes", Message: "again"})
	if a := r.alerts(); len(a) != 2 || a[0].Delivered {
		t.Fatalf("alerts = %+v", a)
	}
}

func TestDailyLimitFiresOncePerDay(t *testing.T) {
	r := newRig(t)
	r.cfg.Alerts.Enabled, r.cfg.Alerts.DailyActiveLimit = true, config.Duration(3*time.Hour)
	ctx := context.Background()
	day := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

	r.active(day.Add(16*time.Hour), day.Add(18*time.Hour)) // 2 h so far
	r.now = day.Add(18 * time.Hour)
	r.e.Evaluate(ctx)
	if len(r.alerts()) != 0 {
		t.Fatal("below the limit")
	}
	r.active(day.Add(19*time.Hour), day.Add(20*time.Hour+10*time.Minute)) // 3 h 10 m
	r.now = day.Add(20*time.Hour + 10*time.Minute)
	r.e.Evaluate(ctx)
	r.e.Evaluate(ctx)
	a := r.alerts()
	if len(a) != 1 || a[0].RuleID != "daily_limit" || !strings.Contains(a[0].Message, "3 h 10 m") || !strings.Contains(a[0].Message, "limit of 3 h") {
		t.Fatalf("alerts = %+v", a)
	}
	// disabled → nothing
	r2 := newRig(t)
	r2.cfg.Alerts.DailyActiveLimit = config.Duration(time.Minute)
	r2.active(day, day.Add(5*time.Hour))
	r2.e.Evaluate(ctx)
	if len(r2.alerts()) != 0 {
		t.Fatal("threshold rules need alerts.enabled")
	}
}

func TestBedtimeAlertUsesTheNightNotTheCalendarDay(t *testing.T) {
	r := newRig(t)
	r.cfg.Alerts.Enabled, r.cfg.Alerts.Bedtime = true, "21:00"
	ctx := context.Background()
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }

	r.active(at(11, 20, 0), at(11, 20, 59)) // before bedtime
	r.now = at(11, 20, 59)
	r.e.Evaluate(ctx)
	if len(r.alerts()) != 0 {
		t.Fatal("not yet bedtime")
	}
	r.active(at(11, 21, 0), at(11, 22, 40))
	r.now = at(11, 22, 40)
	r.e.Evaluate(ctx)
	r.active(at(12, 0, 10), at(12, 0, 50)) // still the same night
	r.now = at(12, 0, 50)
	r.e.Evaluate(ctx)
	if a := r.alerts(); len(a) != 1 || a[0].RuleID != "bedtime" || !strings.Contains(a[0].Message, "22:40") {
		t.Fatalf("alerts = %+v", a)
	}
	// an idle child after bedtime is not "usage"
	r.now = at(12, 5, 0)
	r.db.UpsertUser(1001, "maram", true, 1)
	r.e.Evaluate(ctx)
	if len(r.alerts()) != 1 {
		t.Fatal("no activity, no alert")
	}
	// the next evening is a new night
	r.active(at(12, 21, 30), at(12, 21, 59))
	r.now = at(12, 21, 59)
	r.e.Evaluate(ctx)
	if len(r.alerts()) != 2 {
		t.Fatalf("alerts = %+v", r.alerts())
	}
}

func TestErrorsDoNotLeakThePrivateTopic(t *testing.T) {
	r := newRig(t)
	r.cfg.Alerts.Enabled, r.cfg.Alerts.Channel, r.cfg.Alerts.NtfyURL = true, "ntfy", "http://127.0.0.1:1/very-secret-topic"
	var logged strings.Builder
	r.e.Log = slog.New(slog.NewTextHandler(&logged, nil))
	r.e.Handle(context.Background(), tracker.Notice{Rule: "tracking_gap", Message: "gap"})
	if strings.Contains(logged.String(), "very-secret-topic") {
		t.Fatalf("the ntfy topic leaked into the log (SEC-22): %s", logged.String())
	}
	if !strings.Contains(logged.String(), "not delivered") {
		t.Fatalf("log = %s", logged.String())
	}
}

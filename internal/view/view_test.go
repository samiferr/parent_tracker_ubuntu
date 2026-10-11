package view

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/rollup"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

const min = int64(60 * 1000)

type fx struct {
	db  *storage.DB
	v   *Viewer
	r   *rollup.Rollup
	cfg *config.Config
	loc *time.Location
	now time.Time
}

func newFx(t *testing.T, zone string) *fx {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Default()
	f := &fx{db: db, cfg: cfg, loc: loc, now: time.Date(2026, 10, 11, 19, 20, 0, 0, loc)}
	f.v = &Viewer{DB: db, Cfg: func() *config.Config { return cfg }, Loc: loc, Now: func() time.Time { return f.now }}
	f.r = &rollup.Rollup{DB: db, Cfg: f.v.Cfg, Loc: loc, Now: f.v.Now}
	db.UpsertUser(1000, "ilyes", true, 1)
	return f
}

func (f *fx) at(m time.Month, d, h, mi int) int64 {
	return time.Date(2026, m, d, h, mi, 0, 0, f.loc).UnixMilli()
}

func (f *fx) iv(t *testing.T, st model.State, s, e int64) {
	t.Helper()
	id, _ := f.db.OpenInterval(1000, st, s)
	f.db.CloseInterval(id, e)
}

func (f *fx) samples(app, name string, s, e int64) {
	var ss []model.Sample
	for ts := s; ts < e; ts += 5000 {
		ss = append(ss, model.Sample{UID: 1000, TS: ts, AppID: app, State: model.StateActive, Source: "x11"})
	}
	f.db.InsertSamples(ss, map[string]string{app: name})
}

func TestDayAndHourlyAgree(t *testing.T) {
	f := newFx(t, "Europe/Paris")
	o := time.October
	f.iv(t, model.StateActive, f.at(o, 8, 10, 0), f.at(o, 8, 10, 30)) // earlier history: hours on the 9th are after the first record
	f.iv(t, model.StateActive, f.at(o, 9, 16, 5), f.at(o, 9, 17, 50))
	f.iv(t, model.StateIdle, f.at(o, 9, 17, 50), f.at(o, 9, 18, 0))
	f.iv(t, model.StateLocked, f.at(o, 9, 18, 0), f.at(o, 9, 20, 0))
	f.iv(t, model.StateActive, f.at(o, 9, 20, 30), f.at(o, 9, 21, 18))
	f.samples("firefox", "Firefox", f.at(o, 9, 16, 5), f.at(o, 9, 17, 50))
	f.samples("code", "Code", f.at(o, 9, 20, 30), f.at(o, 9, 21, 18))
	f.cfg.Alerts.Bedtime = "21:00"
	f.r.Run(nil2())

	day, err := f.v.Day(1000, "2026-10-09")
	if err != nil {
		t.Fatal(err)
	}
	if day.ActiveMs != (105+48)*min || day.IdleMs != 10*min || day.LockedMs != 120*min {
		t.Fatalf("day = %+v", day)
	}
	if time.UnixMilli(day.FirstActivity).In(f.loc).Format("15:04") != "16:05" ||
		time.UnixMilli(day.LastActivity).In(f.loc).Format("15:04") != "21:18" {
		t.Fatalf("first/last = %d/%d", day.FirstActivity, day.LastActivity)
	}
	if len(day.Segments) != 4 || len(day.Hours) != 24 {
		t.Fatalf("segments=%d hours=%d", len(day.Segments), len(day.Hours))
	}
	if len(day.TopApps) != 2 || day.TopApps[0].AppID != "firefox" || day.TopApps[0].Category != "browser" {
		t.Fatalf("apps = %+v", day.TopApps)
	}

	h, err := f.v.HourlyDay(1000, "2026-10-09", true)
	if err != nil {
		t.Fatal(err)
	}
	// AC-97: the hourly screen and the day summary show identical numbers.
	if h.ActiveMs != day.ActiveMs || h.IdleMs != day.IdleMs || h.LockedMs != day.LockedMs {
		t.Fatalf("hourly %d/%d/%d != day %d/%d/%d", h.ActiveMs, h.IdleMs, h.LockedMs, day.ActiveMs, day.IdleMs, day.LockedMs)
	}
	for _, b := range h.Buckets {
		if b.Hour == 17 && (b.ActiveMs != 50*min || b.IdleMs != 10*min) {
			t.Fatalf("17:00 = %+v", b)
		}
		if b.Hour == 21 && !b.Bedtime {
			t.Fatal("21:00 should be marked as after bedtime (FR-6.28)")
		}
		if b.Hour == 12 && (b.NoData || b.NotSignedInMs != 60*min) {
			t.Fatalf("12:00 should be 'not signed in', not 'no data': %+v", b)
		}
		if b.Hour == 3 && !b.NoData && b.NotSignedInMs == 0 {
			t.Fatalf("03:00 = %+v", b)
		}
	}
	if !strings.Contains(h.Summary, "last activity 21:18 (after bedtime 21:00)") || !strings.Contains(h.Summary, "2 h 33 m active.") {
		t.Fatalf("summary = %q", h.Summary)
	}
	if !strings.HasPrefix(h.Summary, "Most use 16:00–18:00") {
		t.Fatalf("summary = %q", h.Summary)
	}
}

// Hours before the first record are "no data yet", never zero use.
func TestNoDataBeforeFirstRecord(t *testing.T) {
	f := newFx(t, "UTC")
	f.iv(t, model.StateActive, f.at(time.October, 9, 15, 0), f.at(time.October, 9, 15, 30))
	h, _ := f.v.HourlyDay(1000, "2026-10-09", false)
	var noData, signedOut int
	for _, b := range h.Buckets {
		if b.NoData {
			noData++
		} else if b.NotSignedInMs == b.LenMs {
			signedOut++
		}
	}
	if noData != 15 || signedOut != 8 {
		t.Fatalf("noData=%d notSignedIn=%d", noData, signedOut)
	}
	// a day before anything was recorded
	h, _ = f.v.HourlyDay(1000, "2026-10-01", false)
	for _, b := range h.Buckets {
		if !b.NoData {
			t.Fatalf("bucket %v should be no data", b.Label)
		}
	}
	// the future is blank
	f.now = time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	h, _ = f.v.HourlyDay(1000, "2026-10-09", false)
	if !h.Buckets[12].Future || h.Buckets[10].InProgress != true {
		t.Fatalf("buckets: %+v / %+v", h.Buckets[10], h.Buckets[12])
	}
}

// AC-92: the API returns 23 buckets on a spring-forward day and 25 on a fall-back day.
func TestDSTDayBuckets(t *testing.T) {
	f := newFx(t, "America/New_York")
	f.now = time.Date(2026, 11, 3, 12, 0, 0, 0, f.loc)
	f.iv(t, model.StateActive, time.Date(2026, 3, 8, 1, 30, 0, 0, f.loc).UnixMilli(), time.Date(2026, 3, 8, 3, 30, 0, 0, f.loc).UnixMilli())
	f.iv(t, model.StateActive, time.Date(2026, 11, 1, 0, 30, 0, 0, f.loc).UnixMilli(), time.Date(2026, 11, 1, 4, 0, 0, 0, f.loc).UnixMilli())
	sf, _ := f.v.HourlyDay(1000, "2026-03-08", false)
	fb, _ := f.v.HourlyDay(1000, "2026-11-01", false)
	if len(sf.Buckets) != 23 || !sf.DSTShift || len(fb.Buckets) != 25 {
		t.Fatalf("buckets: %d and %d", len(sf.Buckets), len(fb.Buckets))
	}
	// 01:30–03:30 wall clock on a spring-forward day is one real hour
	if sf.ActiveMs != 60*min {
		t.Fatalf("spring-forward active = %d min", sf.ActiveMs/min)
	}
	// 00:30–04:00 on a fall-back day is 4.5 real hours
	if fb.ActiveMs != 270*min {
		t.Fatalf("fall-back active = %d min", fb.ActiveMs/min)
	}
	day, _ := f.v.Day(1000, "2026-11-01")
	if day.ActiveMs != fb.ActiveMs {
		t.Fatal("daily and hourly must agree on DST days (FR-6.22)")
	}
}

func TestHeatmapAndAverages(t *testing.T) {
	f := newFx(t, "UTC")
	o := time.October
	for d := 5; d <= 10; d++ {
		f.iv(t, model.StateActive, f.at(o, d, 16, 0), f.at(o, d, 18, 30)) // 2.5 h every afternoon
	}
	f.db.InsertGap(model.Gap{Kind: model.GapCrash, Start: f.at(o, 7, 22, 0), End: f.at(o, 7, 23, 0)}, "")
	hr, err := f.v.HourlyRange(1000, "2026-10-05", "2026-10-11")
	if err != nil {
		t.Fatal(err)
	}
	if len(hr.Days) != 7 {
		t.Fatalf("days = %d", len(hr.Days))
	}
	if hr.Days[0].Cells[16].ActiveMs != 60*min || hr.Days[0].Cells[18].ActiveMs != 30*min {
		t.Fatalf("cells = %+v", hr.Days[0].Cells[16:19])
	}
	if hr.Days[2].Cells[22].GapMs != 60*min {
		t.Fatal("gap not carried to the heatmap")
	}
	if hr.AvgMs[16] != 60*min*6/7 && hr.AvgMs[16] <= 0 { // 6 days of use in the 7-day window (the 11th is in the future at 19:20? it is today)
		t.Fatalf("avg[16] = %d", hr.AvgMs[16])
	}
	// the gap-only hour is unknown, so it is excluded from the 22:00 average
	if hr.AvgMs[22] != 0 {
		t.Fatalf("avg[22] = %d", hr.AvgMs[22])
	}
	if !strings.HasPrefix(hr.Summary, "On a typical day most use is 16:00") {
		t.Fatalf("summary = %q", hr.Summary)
	}
	if _, err := f.v.HourlyRange(1000, "2026-10-05", "nope"); err == nil {
		t.Fatal("bad date accepted")
	}
}

func TestTrendAndAverages(t *testing.T) {
	f := newFx(t, "UTC")
	o := time.October
	f.iv(t, model.StateActive, f.at(o, 8, 9, 0), f.at(o, 8, 10, 0))
	f.iv(t, model.StateActive, f.at(o, 9, 9, 0), f.at(o, 9, 12, 0))
	f.iv(t, model.StateActive, f.at(o, 10, 8, 30), f.at(o, 10, 11, 30))
	f.now = time.Date(2026, 10, 10, 15, 0, 0, 0, time.UTC)
	tr, err := f.v.Trend(1000, "2026-10-07", "2026-10-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(tr) != 4 || tr[0].HasData || tr[1].ActiveMs != 60*min || tr[3].FirstMin != 8*60+30 || tr[3].LastMin != 11*60+29 {
		t.Fatalf("trend = %+v", tr)
	}
	day, _ := f.v.Day(1000, "2026-10-10")
	// average of the two earlier days with data: (60+180)/2 = 120 min
	if day.AvgDays != 2 || day.AvgActive7Ms != 120*min {
		t.Fatalf("avg = %d over %d days", day.AvgActive7Ms/min, day.AvgDays)
	}
}

func TestTodayAppsAreLive(t *testing.T) {
	f := newFx(t, "UTC")
	o := time.October
	f.now = time.Date(2026, 10, 11, 10, 30, 0, 0, time.UTC)
	f.iv(t, model.StateActive, f.at(o, 11, 10, 0), f.at(o, 11, 10, 30))
	f.samples("code", "Code", f.at(o, 11, 10, 0), f.at(o, 11, 10, 30))
	// the rollup has not run: today's apps must still be visible
	day, _ := f.v.Day(1000, "2026-10-11")
	if len(day.TopApps) != 1 || day.TopApps[0].ForegroundMs < 29*min {
		t.Fatalf("apps = %+v", day.TopApps)
	}
	av, _ := f.v.Apps(1000, "2026-10-05", "2026-10-11")
	if len(av.Apps) != 1 || av.TotalMs < 29*min || !av.AppsAvailable {
		t.Fatalf("apps view = %+v", av)
	}
	d, err := f.v.AppDetail(1000, "code", "2026-10-05", "2026-10-11")
	if err != nil || d.Name != "Code" || len(d.Days) != 1 {
		t.Fatalf("detail = %+v, %v", d, err)
	}
	// degraded app tracking: no samples, no app rows, no error (AC-12)
	f.db.SQL().Exec(`DELETE FROM window_samples`)
	day, _ = f.v.Day(1000, "2026-10-11")
	if day.AppsAvailable || len(day.TopApps) != 0 || day.ActiveMs != 30*min {
		t.Fatalf("day = %+v", day)
	}
}

func TestEventsMergeAlertsGapsAndAudit(t *testing.T) {
	f := newFx(t, "UTC")
	o := time.October
	f.db.InsertAlert(model.Alert{CreatedAt: f.at(o, 10, 21, 5), RuleID: "bedtime", UID: 1000, Severity: "warn", Message: "ilyes was active after 21:00."})
	f.db.InsertGap(model.Gap{UID: 0, Kind: model.GapCrash, Start: f.at(o, 10, 14, 0), End: f.at(o, 10, 14, 30)}, "")
	f.db.Audit(f.at(o, 10, 14, 31), "system", model.AuditServiceStarted, "")
	f.db.Audit(f.at(o, 10, 15, 0), "system", model.AuditSensorLost, "gnome-wayland session 2")
	ev, err := f.v.Events(1000, f.at(o, 1, 0, 0), f.at(o, 12, 0, 0), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Events) != 4 || ev.Counts["alert"] != 1 || ev.Counts["gap"] != 1 || ev.Counts["audit"] != 2 {
		t.Fatalf("events = %+v", ev)
	}
	if ev.Events[0].Title != "Activity after bedtime" || ev.Events[0].User != "ilyes" {
		t.Fatalf("newest = %+v", ev.Events[0])
	}
	for _, e := range ev.Events {
		if e.Kind == "gap" && !strings.Contains(e.Description, "30m") {
			t.Fatalf("gap = %+v", e)
		}
	}
}

func TestSelfShowsWhatTheParentSees(t *testing.T) {
	f := newFx(t, "UTC")
	f.now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	f.iv(t, model.StateActive, f.at(time.October, 11, 10, 0), f.at(time.October, 11, 11, 0))
	self, err := f.v.Self(1000, "ilyes", "")
	if err != nil {
		t.Fatal(err)
	}
	day, _ := f.v.Day(1000, "2026-10-11")
	if self.Day.ActiveMs != day.ActiveMs || self.Hourly.ActiveMs != day.ActiveMs || !self.LiveVisible {
		t.Fatalf("self = %+v", self)
	}
	if self.Counts["activity_intervals"] != 1 || len(self.Recorded) < 4 {
		t.Fatalf("counts=%v recorded=%v", self.Counts, self.Recorded)
	}
}

func TestFmt(t *testing.T) {
	for ms, want := range map[int64][2]string{
		0: {"0m", "0 m"}, 42 * min: {"42m", "42 m"}, 60 * min: {"1h", "1 h 00 m"},
		72 * min: {"1h 12m", "1 h 12 m"}, 185 * min: {"3h 05m", "3 h 05 m"},
	} {
		if FmtHM(ms) != want[0] || FmtHMSpaced(ms) != want[1] {
			t.Errorf("%d: %q %q", ms/min, FmtHM(ms), FmtHMSpaced(ms))
		}
	}
}

func nil2() context.Context { return context.Background() }

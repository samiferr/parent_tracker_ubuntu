package rollup

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

const min = int64(60 * 1000)

type fixture struct {
	db  *storage.DB
	r   *Rollup
	cfg *config.Config
	now time.Time
	loc *time.Location
}

func newFixture(t *testing.T, maxVersion int) *fixture {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		loc = time.UTC
	}
	db, err := storage.OpenVersion(filepath.Join(t.TempDir(), "t.db"), maxVersion)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Default()
	f := &fixture{db: db, cfg: cfg, loc: loc, now: time.Date(2026, 10, 11, 18, 30, 0, 0, loc)}
	f.r = &Rollup{DB: db, Cfg: func() *config.Config { return cfg }, Loc: loc, Now: func() time.Time { return f.now }}
	db.UpsertUser(1000, "ilyes", true, 1)
	return f
}

func (f *fixture) at(day, h, m int) int64 {
	return time.Date(2026, 10, day, h, m, 0, 0, f.loc).UnixMilli()
}

func (f *fixture) interval(t *testing.T, st model.State, s, e int64) {
	t.Helper()
	id, err := f.db.OpenInterval(1000, st, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.CloseInterval(id, e); err != nil {
		t.Fatal(err)
	}
}

// samples writes one sample every 5 s for [s,e) attributed to app.
func (f *fixture) samples(t *testing.T, app, name string, s, e int64) {
	t.Helper()
	var ss []model.Sample
	for ts := s; ts < e; ts += 5000 {
		ss = append(ss, model.Sample{UID: 1000, TS: ts, AppID: app, State: model.StateActive, Source: "x11"})
	}
	if err := f.db.InsertSamples(ss, map[string]string{app: name}); err != nil {
		t.Fatal(err)
	}
}

func within(got, want, tol int64) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestRunStoresCompletedHoursOnly(t *testing.T) {
	f := newFixture(t, storage.LatestVersion)
	// active 17:50–18:20 spans the 17:00 and 18:00 hours; "now" is 18:30
	f.interval(t, model.StateActive, f.at(11, 17, 50), f.at(11, 18, 20))
	if err := f.r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.db.HourRows(1000, f.at(11, 0, 0), f.at(12, 0, 0))
	if len(rows) != 1 || rows[0].Hour != 17 || rows[0].ActiveMs != 10*min {
		t.Fatalf("only the completed 17:00 hour may be stored (AC-90): %+v", rows)
	}
	// once 18:00 has completed the next run stores it too, 20 min
	f.now = time.UnixMilli(f.at(11, 19, 5)).In(f.loc)
	f.r.Run(context.Background())
	rows, _ = f.db.HourRows(1000, f.at(11, 0, 0), f.at(12, 0, 0))
	if len(rows) != 2 || rows[1].ActiveMs != 20*min {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	f := newFixture(t, storage.LatestVersion)
	f.interval(t, model.StateActive, f.at(10, 20, 0), f.at(10, 21, 30))
	f.samples(t, "code", "Code", f.at(10, 20, 0), f.at(10, 21, 30))
	for i := 0; i < 3; i++ {
		if err := f.r.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := f.db.HourRows(1000, 0, f.now.UnixMilli())
	apps, _ := f.db.HourAppRows(1000, 0, f.now.UnixMilli())
	days, _ := f.db.AppDays(1000, "2026-10-01", "2026-10-31")
	if len(rows) != 2 || len(apps) != 2 || len(days) != 1 {
		t.Fatalf("rows=%d apps=%d days=%d", len(rows), len(apps), len(days))
	}
	if !within(days[0].ForegroundMs, 90*min, 10_000) {
		t.Fatalf("daily = %+v", days[0])
	}
	if days[0].ForegroundMs != apps[0].ForegroundMs+apps[1].ForegroundMs {
		t.Fatalf("daily %d != sum of hourly %d + %d", days[0].ForegroundMs, apps[0].ForegroundMs, apps[1].ForegroundMs)
	}
}

// AC-95 / AC-10: two apps used 10 and 20 minutes in the same hour.
func TestTwoAppsInOneHour(t *testing.T) {
	f := newFixture(t, storage.LatestVersion)
	f.interval(t, model.StateActive, f.at(10, 16, 0), f.at(10, 16, 30))
	f.samples(t, "code", "Code", f.at(10, 16, 0), f.at(10, 16, 10))
	f.samples(t, "firefox", "Firefox Web Browser", f.at(10, 16, 10), f.at(10, 16, 30))
	f.r.Run(context.Background())
	apps, _ := f.db.HourAppRows(1000, f.at(10, 16, 0), f.at(10, 17, 0))
	got := map[string]int64{}
	for _, a := range apps {
		got[a.AppName] = a.ForegroundMs
	}
	if !within(got["Code"], 10*min, 1*min) || !within(got["Firefox Web Browser"], 20*min, 2*min) {
		t.Fatalf("apps = %v (want ≈10 and ≈20 minutes, ±10%%)", got)
	}
	if apps[0].AppName != "Firefox Web Browser" {
		t.Fatalf("rows must be ordered by time spent: %+v", apps)
	}
	if apps[0].Category != "browser" {
		t.Fatalf("category = %q", apps[0].Category)
	}
}

// AC-91: for every day in the fixture the daily active time from intervals
// equals the sum of the stored hourly rows, exactly.
func TestDailyActiveEqualsHourlySum(t *testing.T) {
	f := newFixture(t, storage.LatestVersion)
	f.interval(t, model.StateActive, f.at(9, 22, 40), f.at(10, 1, 15)) // crosses midnight
	f.interval(t, model.StateIdle, f.at(10, 1, 15), f.at(10, 1, 50))
	f.interval(t, model.StateActive, f.at(10, 8, 0), f.at(10, 9, 59))
	f.interval(t, model.StateLocked, f.at(10, 9, 59), f.at(10, 12, 0))
	f.interval(t, model.StateActive, f.at(10, 15, 3), f.at(10, 17, 41))
	f.now = time.UnixMilli(f.at(11, 12, 0)).In(f.loc)
	if err := f.r.Recompute(1000, f.at(9, 0, 0), f.at(11, 0, 0)); err != nil {
		t.Fatal(err)
	}
	for _, day := range []string{"2026-10-09", "2026-10-10"} {
		ds, de, _ := aggregate.ParseDay(f.loc, day)
		ivs, _ := f.db.Intervals(1000, ds, de)
		want := aggregate.ClipTotals(ivs, ds, de)
		var got int64
		rows, _ := f.db.HourRows(1000, ds, de)
		for _, r := range rows {
			got += r.ActiveMs
		}
		if got != want.Active {
			t.Errorf("%s: hourly sum %d != daily %d", day, got, want.Active)
		}
	}
}

// AC-93: tracking gaps show up in gap_ms, not as quiet usage.
func TestGapsAreStoredPerHour(t *testing.T) {
	f := newFixture(t, storage.LatestVersion)
	f.interval(t, model.StateActive, f.at(10, 14, 0), f.at(10, 14, 20))
	f.db.InsertGap(model.Gap{Kind: model.GapCrash, Start: f.at(10, 14, 20), End: f.at(10, 14, 50)}, "")
	f.r.Run(context.Background())
	rows, _ := f.db.HourRows(1000, f.at(10, 14, 0), f.at(10, 15, 0))
	if len(rows) != 1 || rows[0].GapMs != 30*min || rows[0].ActiveMs != 20*min {
		t.Fatalf("rows = %+v", rows)
	}
	if aggregate.NotSignedIn(rows[0]) != 10*min {
		t.Fatalf("not signed in = %d", aggregate.NotSignedIn(rows[0]))
	}
}

// AC-94: after raw samples and intervals have been pruned, hourly totals and
// per-app hourly data survive for hours inside retention.hourly_usage and a
// later rollup does not erase them.
func TestAggregatesSurvivePruningAndRecompute(t *testing.T) {
	f := newFixture(t, storage.LatestVersion)
	old := func(h, m int) int64 { return time.Date(2026, 8, 20, h, m, 0, 0, f.loc).UnixMilli() }
	f.now = time.Date(2026, 8, 21, 0, 30, 0, 0, f.loc)
	f.interval(t, model.StateActive, old(20, 0), old(21, 0))
	f.samples(t, "code", "Code", old(20, 0), old(21, 0))
	if err := f.r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := f.db.HourAppRows(1000, 0, 1<<60)
	if len(before) != 1 {
		t.Fatalf("setup failed: %+v", before)
	}

	// 52 days later the raw data is gone
	f.now = time.Date(2026, 10, 11, 12, 0, 0, 0, f.loc)
	n := f.now.UnixMilli()
	_, err := f.db.Prune(storage.PruneCutoffs{
		WindowSamples: n - f.cfg.Retention.WindowSamples.D().Milliseconds(),
		Intervals:     n - 20*24*3600*1000, // pruned sooner than the default, to make the point
		AppUsageDay:   "2025-10-01",
		HourlyUsage:   n - f.cfg.Retention.HourlyUsage.D().Milliseconds(),
		Alerts:        0, Sessions: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.db.HourRows(1000, 0, 1<<60)
	apps, _ := f.db.HourAppRows(1000, 0, 1<<60)
	days, _ := f.db.AppDays(1000, "2026-08-01", "2026-08-31")
	if len(rows) != 1 || rows[0].ActiveMs != 60*min || len(apps) != 1 || len(days) != 1 {
		t.Fatalf("aggregates lost: rows=%+v apps=%+v days=%+v", rows, apps, days)
	}
	// and a full backfill over pruned data must not touch them either
	if err := f.r.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows2, _ := f.db.HourRows(1000, 0, 1<<60)
	apps2, _ := f.db.HourAppRows(1000, 0, 1<<60)
	if len(rows2) != 1 || len(apps2) != 1 {
		t.Fatalf("backfill erased aggregates: %+v %+v", rows2, apps2)
	}
	// beyond the hourly retention the rows are removed
	f.db.Prune(storage.PruneCutoffs{HourlyUsage: n, AppUsageDay: "9999"})
	if rows, _ := f.db.HourRows(1000, 0, 1<<60); len(rows) != 0 {
		t.Fatal("hourly rows must be removed after retention.hourly_usage")
	}
}

// AC-96: an upgraded database with data is backfilled: state time from
// intervals, per-app time from samples, existing data untouched, idempotent.
func TestBackfillAfterUpgrade(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Paris")
	if loc == nil {
		loc = time.UTC
	}
	path := filepath.Join(t.TempDir(), "t.db")
	old, err := storage.OpenVersion(path, 1) // as the release before hourly history left it
	if err != nil {
		t.Fatal(err)
	}
	old.UpsertUser(1000, "ilyes", true, 1)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, loc)
	var wantActive int64
	for i := 1; i <= 35; i++ { // 35 days of history, 2 h of use each evening
		s := time.Date(2026, 10, 11-i, 18, 0, 0, 0, loc).UnixMilli()
		id, _ := old.OpenInterval(1000, model.StateActive, s)
		old.CloseInterval(id, s+120*min)
		wantActive += 120 * min
		if now.UnixMilli()-s < 30*24*3600*1000 { // raw samples only for the last 30 days
			var ss []model.Sample
			for ts := s; ts < s+120*min; ts += 5000 {
				ss = append(ss, model.Sample{UID: 1000, TS: ts, AppID: "firefox", State: model.StateActive, Source: "x11"})
			}
			old.InsertSamples(ss, map[string]string{"firefox": "Firefox"})
		}
	}
	old.Close()

	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Default()
	r := &Rollup{DB: db, Cfg: func() *config.Config { return cfg }, Loc: loc, Now: func() time.Time { return now }}
	if !r.BackfillPending() {
		t.Fatal("upgrade must flag the backfill")
	}
	ivBefore, _ := db.Intervals(1000, 0, 1<<60)
	if err := r.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.BackfillPending() {
		t.Fatal("backfill flag not cleared")
	}
	rows, _ := db.HourRows(1000, 0, 1<<60)
	var gotActive int64
	for _, h := range rows {
		gotActive += h.ActiveMs
	}
	if gotActive != wantActive {
		t.Fatalf("backfilled active %d != intervals %d", gotActive, wantActive)
	}
	apps, _ := db.HourAppRows(1000, 0, 1<<60)
	if len(apps) == 0 || len(apps) >= len(rows) {
		t.Fatalf("per-app detail should exist only for the sample window: %d app rows, %d hour rows", len(apps), len(rows))
	}
	oldest := apps[0].HourStart
	if now.UnixMilli()-oldest > 31*24*3600*1000 {
		t.Fatalf("per-app detail reaches beyond the raw samples: %s", time.UnixMilli(oldest))
	}
	ivAfter, _ := db.Intervals(1000, 0, 1<<60)
	if len(ivAfter) != len(ivBefore) {
		t.Fatal("existing intervals were changed")
	}
	// idempotent
	if err := r.Backfill(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows2, _ := db.HourRows(1000, 0, 1<<60)
	if len(rows2) != len(rows) {
		t.Fatalf("second backfill changed the row count: %d -> %d", len(rows), len(rows2))
	}
}

// Sample caps: a stalled sensor must not turn into hours of foreground time.
func TestStalledSensorDoesNotInflateAppTime(t *testing.T) {
	f := newFixture(t, storage.LatestVersion)
	f.interval(t, model.StateActive, f.at(10, 10, 0), f.at(10, 12, 0))
	f.samples(t, "code", "Code", f.at(10, 10, 0), f.at(10, 10, 1)) // one minute of samples, then the agent dies
	f.r.Run(context.Background())
	days, _ := f.db.AppDays(1000, "2026-10-10", "2026-10-10")
	if len(days) != 1 || days[0].ForegroundMs > 90_000 /* 1 min + one sample cap */ {
		t.Fatalf("daily = %+v", days)
	}
}

func TestSampleCap(t *testing.T) {
	c := config.Default()
	if got := SampleCap(c); got != 12_500 {
		t.Fatalf("cap = %d", got)
	}
	c.SampleInterval = config.Duration(time.Second)
	if got := SampleCap(c); got != 10_000 {
		t.Fatalf("cap floor = %d", got)
	}
}

package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "tracker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpenCreatesPrivateWALDatabase(t *testing.T) {
	d := openTemp(t)
	st, err := os.Stat(d.Path())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %v, want 0600 (NFR-4.2)", st.Mode().Perm())
	}
	var mode string
	if err := d.SQL().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}
	if d.SchemaVersion() != LatestVersion {
		t.Fatalf("schema = %d", d.SchemaVersion())
	}
}

// AC-96 (storage part): an old database is migrated additively, a backup is
// kept, existing rows are untouched, and the backfill flag is raised.
func TestUpgradeFromV1KeepsDataAndBacksUp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tracker.db")
	old, err := OpenVersion(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.OpenInterval(1000, model.StateActive, 1_000); err != nil {
		t.Fatal(err)
	}
	if err := old.UpsertUser(1000, "ilyes", true, 5); err != nil {
		t.Fatal(err)
	}
	old.Close()

	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.SchemaVersion() != 2 {
		t.Fatalf("schema = %d", d.SchemaVersion())
	}
	ivs, _ := d.LoadOpenIntervals()
	if len(ivs) != 1 || ivs[0].Start != 1_000 {
		t.Fatalf("existing data changed: %+v", ivs)
	}
	if v, _, _ := d.GetState("hourly_backfill_pending"); v != "1" {
		t.Fatalf("backfill flag = %q", v)
	}
	if _, err := os.Stat(p + ".bak-v1"); err != nil {
		t.Fatalf("no backup made before migrating: %v", err)
	}
	// Re-opening a current database is a no-op.
	d.Close()
	d2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	d2.Close()
}

func TestRefusesToDowngrade(t *testing.T) {
	d := openTemp(t)
	if _, err := d.SQL().Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (99, 0)`); err != nil {
		t.Fatal(err)
	}
	d.Close()
	if _, err := Open(d.Path()); err == nil {
		t.Fatal("opening a database from a newer release must fail")
	}
}

// AC-40 (storage part): the open interval's end tracks the last good instant, so
// a crash loses at most one sample interval and the interval can be closed there.
func TestOpenIntervalSurvivesACrash(t *testing.T) {
	d := openTemp(t)
	id, err := d.OpenInterval(1000, model.StateActive, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []int64{15_000, 20_000, 18_000 /* out of order: must not shrink */} {
		if err := d.TouchInterval(id, at); err != nil {
			t.Fatal(err)
		}
	}
	open, err := d.LoadOpenIntervals()
	if err != nil || len(open) != 1 || open[0].End != 20_000 || !open[0].Open {
		t.Fatalf("open = %+v, %v", open, err)
	}
	if err := d.CloseInterval(id, open[0].End); err != nil {
		t.Fatal(err)
	}
	ivs, _ := d.Intervals(1000, 0, 100_000)
	if len(ivs) != 1 || ivs[0].Open || ivs[0].End != 20_000 {
		t.Fatalf("closed = %+v", ivs)
	}
	// closing before the start never yields a negative duration (AC-6)
	id2, _ := d.OpenInterval(1000, model.StateIdle, 50_000)
	if err := d.CloseInterval(id2, 40_000); err != nil {
		t.Fatal(err)
	}
	var dur int64
	d.SQL().QueryRow(`SELECT duration_ms FROM activity_intervals WHERE id = ?`, id2).Scan(&dur)
	if dur != 0 {
		t.Fatalf("duration = %d", dur)
	}
}

func TestReplaceHourlyIsIdempotent(t *testing.T) {
	d := openTemp(t)
	rows := []model.HourRow{{UID: 1, HourStart: 3_600_000, Day: "1970-01-01", Hour: 1, HourLenMs: 3_600_000, ActiveMs: 100}}
	apps := []model.HourAppRow{{UID: 1, HourStart: 3_600_000, AppID: "code", AppName: "Code", Category: "school", ForegroundMs: 90}}
	for i := 0; i < 3; i++ {
		if err := d.ReplaceHourly(1, 0, 0, 10_000_000, rows, apps); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := d.HourRows(1, 0, 10_000_000)
	ga, _ := d.HourAppRows(1, 0, 10_000_000)
	if len(got) != 1 || len(ga) != 1 || got[0].ActiveMs != 100 {
		t.Fatalf("rows=%+v apps=%+v", got, ga)
	}
	// a recompute that finds nothing removes the stale rows
	if err := d.ReplaceHourly(1, 0, 0, 10_000_000, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.HourRows(1, 0, 10_000_000); len(got) != 0 {
		t.Fatalf("stale rows remain: %+v", got)
	}
}

// AC-94: raw data is pruned by its own retention while aggregates survive.
func TestPruneKeepsAggregatesLonger(t *testing.T) {
	d := openTemp(t)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC).UnixMilli()
	old := now - 40*24*3600*1000
	if err := d.InsertSamples([]model.Sample{{UID: 1, TS: old, AppID: "code", State: model.StateActive, Source: "x11"}, {UID: 1, TS: now, AppID: "code", State: model.StateActive, Source: "x11"}}, nil); err != nil {
		t.Fatal(err)
	}
	id, _ := d.OpenInterval(1, model.StateActive, old)
	d.CloseInterval(id, old+1000)
	d.ReplaceHourly(1, 0, 0, now+1, []model.HourRow{{UID: 1, HourStart: old, Day: "x", HourLenMs: 3_600_000, ActiveMs: 5}}, nil)

	p, err := d.Prune(PruneCutoffs{
		WindowSamples: now - 30*24*3600*1000, Intervals: now - 180*24*3600*1000, AppUsageDay: "2024-10-11",
		HourlyUsage: now - 365*24*3600*1000, Alerts: now - 365*24*3600*1000, Sessions: now - 365*24*3600*1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Samples != 1 || p.Intervals != 0 || p.Hourly != 0 {
		t.Fatalf("pruned = %+v", p)
	}
	if hr, _ := d.HourRows(1, 0, now); len(hr) != 1 {
		t.Fatal("hourly aggregate must outlive raw samples")
	}
	// beyond the hourly retention it goes too
	p, _ = d.Prune(PruneCutoffs{WindowSamples: 0, Intervals: 0, AppUsageDay: "0000", HourlyUsage: now + 1, Alerts: 0, Sessions: 0})
	if p.Hourly != 1 {
		t.Fatalf("pruned = %+v", p)
	}
}

// AC-98 / FR-10.4: delete removes hourly rows too, for that user only, and the
// export bundle contains what remains.
func TestDeleteUserBeforeIsRealAndScoped(t *testing.T) {
	d := openTemp(t)
	d.UpsertUser(1, "ilyes", true, 1)
	d.UpsertUser(2, "maram", true, 1)
	for _, uid := range []int{1, 2} {
		id, _ := d.OpenInterval(uid, model.StateActive, 1_000)
		d.CloseInterval(id, 5_000)
		id, _ = d.OpenInterval(uid, model.StateActive, 100_000)
		d.CloseInterval(id, 105_000)
		d.InsertSamples([]model.Sample{{UID: uid, TS: 2_000, AppID: "secret-app", Title: map[int]string{1: "private title of user one", 2: "kept title of user two"}[uid], State: model.StateActive, Source: "x11"}}, map[string]string{"secret-app": "Secret"})
		d.ReplaceHourly(uid, 0, 0, 1_000_000_000, []model.HourRow{{UID: uid, HourStart: 0, Day: "1970-01-01", HourLenMs: 3_600_000, ActiveMs: 4_000}}, nil)
		d.ReplaceAppDays(uid, []string{"1970-01-01"}, []model.AppDay{{UID: uid, Day: "1970-01-01", AppID: "secret-app", AppName: "Secret", Category: "other", ForegroundMs: 4000, OpenMs: 4000, SessionCount: 1}})
	}
	counts, err := d.DeleteUserBefore(1, 50_000_000, "1970-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if counts["window_samples"] != 1 || counts["hourly_usage"] != 1 || counts["app_usage"] != 1 || counts["activity_intervals"] != 2 {
		t.Fatalf("counts = %v", counts)
	}
	if ivs, _ := d.Intervals(1, 0, 1e12); len(ivs) != 0 {
		t.Fatal("user 1 intervals remain")
	}
	if ivs, _ := d.Intervals(2, 0, 1e12); len(ivs) != 2 {
		t.Fatal("another user's data must not be touched")
	}
	// the title text is not recoverable from the file after the delete
	raw, _ := os.ReadFile(d.Path())
	wal, _ := os.ReadFile(d.Path() + "-wal")
	if containsTwice(append(raw, wal...), "private title of user one", 1) {
		t.Fatal("deleted title still present in the database file")
	}

	if !containsTwice(append(append([]byte{}, raw...), wal...), "kept title of user two", 1) {
		t.Fatal("the other user's title should still be stored")
	}

	b, err := d.Export([]int{1}, 0, 1e13, "0000-00-00", "9999-12-31", "test", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Datasets.Intervals) != 0 || b.Manifest.Checksum == "" || len(b.Manifest.Users) != 1 {
		t.Fatalf("export = %+v", b.Manifest)
	}
	all, _ := d.Export(nil, 0, 1e13, "0000-00-00", "9999-12-31", "test", time.Unix(0, 0))
	if all.Manifest.Counts["activity_intervals"] != 2 || all.Manifest.Counts["hourly_usage"] != 1 {
		t.Fatalf("counts = %v", all.Manifest.Counts)
	}
}

func containsTwice(b []byte, s string, n int) bool {
	c := 0
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			c++
			if c >= n {
				return true
			}
		}
	}
	return false
}

func TestAlertsGapsAuditAndState(t *testing.T) {
	d := openTemp(t)
	id, err := d.InsertAlert(model.Alert{CreatedAt: 1000, RuleID: "daily_limit", UID: 7, Severity: "warn", Message: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.AlertExists("daily_limit", 7, 0, 2000); !ok {
		t.Fatal("alert not found")
	}
	if ok, _ := d.AlertExists("daily_limit", 8, 0, 2000); ok {
		t.Fatal("alert leaked to another user")
	}
	d.MarkAlertDelivered(id, "ntfy")
	al, _ := d.Alerts(0, 5000, 10)
	if len(al) != 1 || !al[0].Delivered || al[0].DeliveredVia != "ntfy" {
		t.Fatalf("alerts = %+v", al)
	}

	d.InsertGap(model.Gap{UID: 0, Start: 10, End: 20, Kind: model.GapCrash}, "")
	d.InsertGap(model.Gap{UID: 5, Start: 30, End: 40, Kind: model.GapUnclean}, "")
	d.InsertGap(model.Gap{UID: 5, Start: 50, End: 50, Kind: model.GapUnclean}, "") // empty: ignored
	gs, _ := d.Gaps(5, 0, 100)
	if len(gs) != 2 {
		t.Fatalf("gaps = %+v", gs)
	}
	if gs, _ := d.Gaps(6, 0, 100); len(gs) != 1 {
		t.Fatalf("user 6 should only see the all-user gap, got %+v", gs)
	}

	d.Audit(100, "system", model.AuditServiceStarted, "{}")
	d.Audit(200, "system", model.AuditServiceStopped, "{}")
	ev, _ := d.AuditEvents(0, 1000, 10)
	if len(ev) != 2 || ev[0].Action != model.AuditServiceStopped {
		t.Fatalf("audit = %+v", ev)
	}

	d.SetState("k", "v")
	d.SetState("k", "w")
	if v, ok, _ := d.GetState("k"); !ok || v != "w" {
		t.Fatalf("state = %q %v", v, ok)
	}
	if _, ok, _ := d.GetState("missing"); ok {
		t.Fatal("missing key reported present")
	}
}

func TestReadOnlyOpen(t *testing.T) {
	d := openTemp(t)
	d.UpsertUser(1, "a", true, 1)
	ro, err := OpenReadOnly(d.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	us, err := ro.Users()
	if err != nil || len(us) != 1 {
		t.Fatalf("users = %+v, %v", us, err)
	}
	if err := ro.UpsertUser(2, "b", true, 1); err == nil {
		t.Fatal("a read-only handle must refuse writes")
	}
}

package retention

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
)

func TestOnceHonoursEachRetention(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := config.Default()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	day := int64(24 * 3600 * 1000)
	n := now.UnixMilli()

	db.InsertSamples([]model.Sample{
		{UID: 1, TS: n - 40*day, AppID: "old", State: model.StateActive, Source: "x11"},
		{UID: 1, TS: n - 10*day, AppID: "new", State: model.StateActive, Source: "x11"},
	}, nil)
	for _, age := range []int64{200, 100} { // intervals: 180 days
		id, _ := db.OpenInterval(1, model.StateActive, n-age*day)
		db.CloseInterval(id, n-age*day+1000)
	}
	db.ReplaceHourly(1, 0, 0, n+1, []model.HourRow{
		{UID: 1, HourStart: n - 400*day, Day: "x", HourLenMs: 3600000, ActiveMs: 1},
		{UID: 1, HourStart: n - 300*day, Day: "x", HourLenMs: 3600000, ActiveMs: 1}}, nil)
	db.ReplaceAppDays(1, []string{"2024-01-01", "2026-09-01"}, []model.AppDay{
		{UID: 1, Day: "2024-01-01", AppID: "a", AppName: "a", Category: "other", OpenMs: 1},
		{UID: 1, Day: "2026-09-01", AppID: "a", AppName: "a", Category: "other", OpenMs: 1}})

	var notices []tracker.Notice
	p := &Pruner{DB: db, Cfg: func() *config.Config { return cfg }, Loc: time.UTC, Now: func() time.Time { return now },
		Notify: func(n tracker.Notice) { notices = append(notices, n) }, FreePercent: func(string) (float64, error) { return 50, nil }}
	got, err := p.Once()
	if err != nil {
		t.Fatal(err)
	}
	if got.Samples != 1 || got.Intervals != 1 || got.Hourly != 1 || got.AppUsage != 1 {
		t.Fatalf("pruned = %+v", got)
	}
	// raw samples (30 d) go before hourly rows (1 y) before daily app rows (2 y)
	if hr, _ := db.HourRows(1, 0, n); len(hr) != 1 {
		t.Fatalf("hourly rows = %d", len(hr))
	}
	if len(notices) != 0 {
		t.Fatal("no disk alert expected")
	}
}

func TestLowDiskRaisesAnAlert(t *testing.T) {
	db, _ := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()
	cfg := config.Default()
	var notices []tracker.Notice
	p := &Pruner{DB: db, Cfg: func() *config.Config { return cfg }, Loc: time.UTC,
		Notify:      func(n tracker.Notice) { notices = append(notices, n) },
		FreePercent: func(string) (float64, error) { return 4, nil }}
	if _, err := p.Once(); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || notices[0].Rule != "disk_low" {
		t.Fatalf("notices = %+v", notices)
	}
}

func TestFreePercentOnARealPath(t *testing.T) {
	pct, err := freePercent(t.TempDir())
	if err != nil || pct <= 0 || pct > 100 {
		t.Fatalf("pct=%v err=%v", pct, err)
	}
}

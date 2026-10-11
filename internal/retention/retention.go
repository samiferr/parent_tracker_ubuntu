// Package retention enforces the retention policy with real deletes and guards
// the disk (docs/05 §5.2, NFR-7.4).
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sys/unix"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
)

// LowDiskPercent is the free-space threshold below which an alert is raised.
const LowDiskPercent = 10.0

// vacuumAfter is how many removed rows justify rewriting the database file.
const vacuumAfter = 5000

// Pruner removes data past its retention.
type Pruner struct {
	DB     *storage.DB
	Cfg    func() *config.Config
	Loc    *time.Location
	Now    func() time.Time
	Log    *slog.Logger
	Notify func(tracker.Notice)
	// FreePercent reports free disk space for a path; overridden in tests.
	FreePercent func(path string) (float64, error)
}

func (p *Pruner) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Once prunes and checks the disk a single time.
func (p *Pruner) Once() (storage.Pruned, error) {
	cfg, now := p.Cfg(), p.now()
	ms := now.UnixMilli()
	r := cfg.Retention
	cut := storage.PruneCutoffs{
		WindowSamples: ms - r.WindowSamples.Milliseconds(),
		Intervals:     ms - r.Intervals.Milliseconds(),
		AppUsageDay:   aggregate.DayString(p.Loc, ms-r.AppUsage.Milliseconds()),
		HourlyUsage:   ms - r.HourlyUsage.Milliseconds(),
		Alerts:        ms - r.Alerts.Milliseconds(),
		Sessions:      ms - r.Sessions.Milliseconds(),
	}
	pruned, err := p.DB.Prune(cut)
	if err != nil {
		return pruned, err
	}
	if pruned.Total() >= vacuumAfter {
		if err := p.DB.Vacuum(); err != nil && p.Log != nil {
			p.Log.Warn("vacuum failed", "err", err)
		}
	}
	p.checkDisk(cfg)
	return pruned, nil
}

func (p *Pruner) checkDisk(cfg *config.Config) {
	free := p.FreePercent
	if free == nil {
		free = freePercent
	}
	pct, err := free(cfg.DataDir)
	if err != nil || pct >= LowDiskPercent || p.Notify == nil {
		return
	}
	p.Notify(tracker.Notice{Rule: "disk_low", Severity: "warn",
		Message: fmt.Sprintf("Only %.0f%% of the disk holding %s is free. Old data is pruned on schedule; free some space or shorten the retention settings.", pct, cfg.DataDir)})
}

func freePercent(path string) (float64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	if st.Blocks == 0 {
		return 100, nil
	}
	return float64(st.Bavail) / float64(st.Blocks) * 100, nil
}

// Run prunes at start and then every retention.prune_interval.
func (p *Pruner) Run(ctx context.Context) {
	for {
		if pruned, err := p.Once(); err != nil {
			if p.Log != nil {
				p.Log.Error("retention run failed", "err", err)
			}
		} else if p.Log != nil && pruned.Total() > 0 {
			p.Log.Info("retention removed old rows", "rows", pruned.Total())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.Cfg().Retention.PruneInterval.D()):
		}
	}
}

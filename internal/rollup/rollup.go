// Package rollup turns stored intervals and app samples into the hourly and
// daily aggregates (docs/04 §4.12).
//
// Every run recomputes a trailing window with an idempotent replace, so an
// interrupted run, a late flush or an orphan interval closed after a crash is
// absorbed by the next run and nothing is ever counted twice.
package rollup

import (
	"context"
	"sort"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

// Window is how far back every run recomputes.
const Window = 48 * time.Hour

// BackfillChunk bounds the memory used by a backfill.
const BackfillChunk = 7 * 24 * time.Hour

// Rollup computes and stores aggregates.
type Rollup struct {
	DB  *storage.DB
	Cfg func() *config.Config
	Loc *time.Location
	Now func() time.Time
}

func (r *Rollup) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// SampleCap is how long one app sample may stand in for the app being in front.
func SampleCap(cfg *config.Config) int64 {
	c := cfg.SampleInterval.D() * 5 / 2
	if c < 10*time.Second {
		c = 10 * time.Second
	}
	return c.Milliseconds()
}

// Run recomputes the trailing window for every known user.
func (r *Rollup) Run(ctx context.Context) error {
	now := r.now().UnixMilli()
	return r.recomputeAll(ctx, now-Window.Milliseconds(), now)
}

func (r *Rollup) recomputeAll(ctx context.Context, from, to int64) error {
	users, err := r.DB.Users()
	if err != nil {
		return err
	}
	for _, u := range users {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.Recompute(u.UID, from, to); err != nil {
			return err
		}
	}
	return nil
}

// BackfillPending reports whether the migration that introduced hourly history
// still has to rebuild old hours.
func (r *Rollup) BackfillPending() bool {
	v, ok, _ := r.DB.GetState("hourly_backfill_pending")
	return ok && v == "1"
}

// Backfill rebuilds hourly and per-app rows from the data that still exists:
// state time from intervals (up to retention.intervals) and per-app time from
// raw samples (up to retention.window_samples). Older hours therefore have
// totals but no per-app detail (docs/04 §4.12, AC-96). It is idempotent and
// works in bounded chunks, so collection is never blocked for long.
func (r *Rollup) Backfill(ctx context.Context) error {
	cfg := r.Cfg()
	now := r.now().UnixMilli()
	users, err := r.DB.Users()
	if err != nil {
		return err
	}
	for _, u := range users {
		first, err := r.DB.FirstIntervalStart(u.UID)
		if err != nil {
			return err
		}
		if first == 0 {
			continue
		}
		floor := now - cfg.Retention.Intervals.D().Milliseconds()
		if first < floor {
			first = floor
		}
		for from := aggregate.BucketAt(r.Loc, first).Start; from < now; from += BackfillChunk.Milliseconds() {
			if err := ctx.Err(); err != nil {
				return err
			}
			to := from + BackfillChunk.Milliseconds()
			if to > now {
				to = now
			}
			if err := r.Recompute(u.UID, from, to); err != nil {
				return err
			}
		}
	}
	return r.DB.SetState("hourly_backfill_pending", "0")
}

// Recompute replaces one user's hourly and daily rows for the completed hours
// in [from,to). The hour in progress is never stored half-finished.
func (r *Rollup) Recompute(uid int, from, to int64) error {
	cfg := r.Cfg()
	now := r.now().UnixMilli()

	if cur := aggregate.BucketAt(r.Loc, now).Start; to > cur {
		to = cur
	}
	from = aggregate.BucketAt(r.Loc, from).Start
	if to <= from {
		return nil
	}

	// State rows can only be rebuilt where intervals still exist; per-app rows
	// only where raw samples still exist.
	stateFrom := maxI(from, aggregate.BucketAt(r.Loc, now-cfg.Retention.Intervals.D().Milliseconds()).Start)
	appFrom := maxI(from, aggregate.BucketAt(r.Loc, now-cfg.Retention.WindowSamples.D().Milliseconds()).Start)
	if appFrom < stateFrom {
		appFrom = stateFrom
	}
	if to <= stateFrom {
		return nil
	}

	ivs, err := r.DB.Intervals(uid, stateFrom, to)
	if err != nil {
		return err
	}
	aggregate.SortIntervals(ivs)
	gaps, err := r.DB.Gaps(uid, stateFrom, to)
	if err != nil {
		return err
	}
	rows := aggregate.HourRows(uid, r.Loc, ivs, gaps, stateFrom, to)

	var appRows []model.HourAppRow
	if appFrom < to {
		capMs := SampleCap(cfg)
		samples, err := r.DB.Samples(uid, appFrom-capMs, to+capMs)
		if err != nil {
			return err
		}
		res := ComputeApps(cfg, r.Loc, samples, ivs, capMs, appFrom, to)
		appRows = res.Hourly(uid)
	}
	if err := r.DB.ReplaceHourly(uid, stateFrom, appFrom, to, rows, appRows); err != nil {
		return err
	}
	return r.recomputeDays(uid, cfg, appFrom, to)
}

// recomputeDays rewrites the per-day app aggregates for every local day that
// [from,to) touches, provided the whole day's raw samples are still available.
func (r *Rollup) recomputeDays(uid int, cfg *config.Config, from, to int64) error {
	now := r.now().UnixMilli()
	rawFloor := now - cfg.Retention.WindowSamples.D().Milliseconds()
	capMs := SampleCap(cfg)

	d0, _ := aggregate.DayBounds(r.Loc, from)
	var days []string
	for s := d0; s < to; {
		ds, de := aggregate.DayBounds(r.Loc, s)
		if ds >= rawFloor { // the whole day is still backed by raw samples
			days = append(days, aggregate.DayString(r.Loc, ds))
		}
		s = de
	}
	if len(days) == 0 {
		return nil
	}
	first, _, _ := aggregate.ParseDay(r.Loc, days[0])
	_, last, _ := aggregate.ParseDay(r.Loc, days[len(days)-1])

	ivs, err := r.DB.Intervals(uid, first, last)
	if err != nil {
		return err
	}
	aggregate.SortIntervals(ivs)
	samples, err := r.DB.Samples(uid, first-capMs, last+capMs)
	if err != nil {
		return err
	}
	res := ComputeApps(cfg, r.Loc, samples, ivs, capMs, first, last)
	return r.DB.ReplaceAppDays(uid, days, res.Daily(uid))
}

func maxI(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ---- pure computation ----------------------------------------------------

type appKey struct {
	hour int64 // hour_start for hourly, day index for daily
	app  string
}

// AppResult is the per-app time computed from samples and intervals.
type AppResult struct {
	cfg    *config.Config
	names  map[string]string
	hourly map[appKey]int64
	daily  map[string]map[string]*model.AppDay // day -> app -> row
}

// ComputeApps attributes foreground time to apps for [from,to): each sample
// holds until the next one (capped), and only the part of that span that lies
// inside an active interval counts as foreground; the active+idle part counts
// as "open" (FR-3.6, AC-11).
func ComputeApps(cfg *config.Config, loc *time.Location, samples []storage.SampleRow, ivs []model.Interval, capMs, from, to int64) *AppResult {
	res := &AppResult{
		cfg: cfg, names: map[string]string{},
		hourly: map[appKey]int64{},
		daily:  map[string]map[string]*model.AppDay{},
	}
	ss := make([]model.Sample, 0, len(samples))
	for _, s := range samples {
		ss = append(ss, s.Sample)
		if s.AppName != "" {
			res.names[s.AppID] = s.AppName
		}
	}
	spans := aggregate.MergeSpans(aggregate.BuildSpans(ss, capMs))

	day := func(d, app string) *model.AppDay {
		m := res.daily[d]
		if m == nil {
			m = map[string]*model.AppDay{}
			res.daily[d] = m
		}
		row := m[app]
		if row == nil {
			row = &model.AppDay{Day: d, AppID: app, AppName: res.nameOf(app), Category: cfg.CategoryFor(app)}
			m[app] = row
		}
		return row
	}

	aggregate.IntersectSpans(spans, ivs, func(p aggregate.Piece) {
		a, b := maxI(p.Start, from), p.End
		if b > to {
			b = to
		}
		if b <= a {
			return
		}
		aggregate.SplitDays(loc, a, b, func(d string, ms int64) {
			row := day(d, p.AppID)
			row.OpenMs += ms
			if p.State == model.StateActive {
				row.ForegroundMs += ms
			}
		})
		if p.State == model.StateActive {
			aggregate.SplitHours(loc, a, b, func(bk aggregate.Bucket, ms int64) {
				res.hourly[appKey{bk.Start, p.AppID}] += ms
			})
		}
	})

	// One visit = one uninterrupted stay in an app that included active time.
	for _, sp := range spans {
		if sp.Start < from || sp.Start >= to {
			continue
		}
		used := false
		aggregate.IntersectSpans([]aggregate.Span{sp}, ivs, func(p aggregate.Piece) {
			if p.State == model.StateActive {
				used = true
			}
		})
		if used {
			day(aggregate.DayString(loc, sp.Start), sp.AppID).SessionCount++
		}
	}
	return res
}

func (r *AppResult) nameOf(app string) string {
	if n := r.names[app]; n != "" {
		return n
	}
	return app
}

// Hourly returns the per-hour per-app rows, ordered by hour then time spent.
func (r *AppResult) Hourly(uid int) []model.HourAppRow {
	var out []model.HourAppRow
	for k, ms := range r.hourly {
		if ms <= 0 {
			continue
		}
		out = append(out, model.HourAppRow{UID: uid, HourStart: k.hour, AppID: k.app, AppName: r.nameOf(k.app),
			Category: r.cfg.CategoryFor(k.app), ForegroundMs: ms})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HourStart != out[j].HourStart {
			return out[i].HourStart < out[j].HourStart
		}
		if out[i].ForegroundMs != out[j].ForegroundMs {
			return out[i].ForegroundMs > out[j].ForegroundMs
		}
		return out[i].AppID < out[j].AppID
	})
	return out
}

// Daily returns the per-day per-app rows.
func (r *AppResult) Daily(uid int) []model.AppDay {
	var out []model.AppDay
	for _, m := range r.daily {
		for _, row := range m {
			if row.OpenMs <= 0 {
				continue
			}
			row.UID = uid
			out = append(out, *row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		if out[i].ForegroundMs != out[j].ForegroundMs {
			return out[i].ForegroundMs > out[j].ForegroundMs
		}
		return out[i].AppID < out[j].AppID
	})
	return out
}

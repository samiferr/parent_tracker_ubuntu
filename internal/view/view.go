// Package view answers the questions the dashboard, the child's self-view and
// `parent-tracker report` ask. All three call this package, so they show the
// same numbers for the same user and day (AC-97).
package view

import (
	"fmt"
	"sort"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/rollup"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

// Viewer reads aggregates from the database.
type Viewer struct {
	DB  *storage.DB
	Cfg func() *config.Config
	Loc *time.Location
	Now func() time.Time
}

func (v *Viewer) now() int64 {
	if v.Now != nil {
		return v.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}

// Segment is a coloured stretch of the day timeline.
type Segment struct {
	Start int64  `json:"start"`
	End   int64  `json:"end"`
	State string `json:"state"`
}

// AppTotal is time spent in one application.
type AppTotal struct {
	AppID        string `json:"app_id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	ForegroundMs int64  `json:"foreground_ms"`
	OpenMs       int64  `json:"open_ms"`
	Sessions     int64  `json:"sessions"`
}

// CategoryTotal is time spent in one category.
type CategoryTotal struct {
	Category     string `json:"category"`
	ForegroundMs int64  `json:"foreground_ms"`
}

// HourBucket is one local hour of one day.
type HourBucket struct {
	HourStart     int64      `json:"hour_start"`
	HourEnd       int64      `json:"hour_end"`
	Day           string     `json:"day"`
	Hour          int        `json:"hour"`
	Label         string     `json:"label"`
	LenMs         int64      `json:"len_ms"`
	ActiveMs      int64      `json:"active_ms"`
	IdleMs        int64      `json:"idle_ms"`
	LockedMs      int64      `json:"locked_ms"`
	GapMs         int64      `json:"gap_ms"`
	NotSignedInMs int64      `json:"not_signed_in_ms"`
	InProgress    bool       `json:"in_progress"`
	Future        bool       `json:"future"`
	NoData        bool       `json:"no_data"`
	Bedtime       bool       `json:"bedtime"`
	Apps          []AppTotal `json:"apps,omitempty"`
}

// DaySummary is the Today screen's data for one day.
type DaySummary struct {
	Date          string          `json:"date"`
	DayStart      int64           `json:"day_start"`
	DayEnd        int64           `json:"day_end"`
	NowMs         int64           `json:"now"`
	IsToday       bool            `json:"is_today"`
	NoData        bool            `json:"no_data"`
	FromHourly    bool            `json:"from_hourly"`
	ActiveMs      int64           `json:"active_ms"`
	IdleMs        int64           `json:"idle_ms"`
	LockedMs      int64           `json:"locked_ms"`
	FirstActivity int64           `json:"first_activity"`
	LastActivity  int64           `json:"last_activity"`
	AvgActive7Ms  int64           `json:"avg_active_7d_ms"`
	AvgDays       int             `json:"avg_days"`
	Segments      []Segment       `json:"segments"`
	Gaps          []Segment       `json:"gaps"`
	TopApps       []AppTotal      `json:"top_apps"`
	Categories    []CategoryTotal `json:"categories"`
	AppsAvailable bool            `json:"apps_available"`
	Hours         []HourBucket    `json:"hours"`
}

func sortedIntervals(ivs []model.Interval) []model.Interval {
	aggregate.SortIntervals(ivs)
	return ivs
}

// Day builds the Today screen for a local date.
func (v *Viewer) Day(uid int, date string) (*DaySummary, error) {
	ds, de, err := aggregate.ParseDay(v.Loc, date)
	if err != nil {
		return nil, fmt.Errorf("date must be YYYY-MM-DD: %w", err)
	}
	now := v.now()
	cfg := v.Cfg()
	out := &DaySummary{Date: date, DayStart: ds, DayEnd: de, NowMs: now, IsToday: now >= ds && now < de,
		Segments: []Segment{}, Gaps: []Segment{}, TopApps: []AppTotal{}, Categories: []CategoryTotal{}}

	ivs, err := v.DB.Intervals(uid, ds, de)
	if err != nil {
		return nil, err
	}
	sortedIntervals(ivs)
	tot := aggregate.ClipTotals(ivs, ds, de)

	if len(ivs) == 0 {
		// Older than the interval retention: fall back to the stored hourly rows.
		rows, err := v.DB.HourRows(uid, ds, de)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			tot.Active += r.ActiveMs
			tot.Idle += r.IdleMs
			tot.Locked += r.LockedMs
		}
		out.FromHourly = len(rows) > 0
	}
	out.ActiveMs, out.IdleMs, out.LockedMs = tot.Active, tot.Idle, tot.Locked

	for _, iv := range ivs {
		a, b := maxI(iv.Start, ds), minI(iv.End, de)
		if b <= a {
			continue
		}
		if iv.State == model.StateActive {
			if out.FirstActivity == 0 || a < out.FirstActivity {
				out.FirstActivity = a
			}
			if b > out.LastActivity {
				out.LastActivity = b
			}
		}
		if iv.State != model.StateSuspended {
			out.Segments = append(out.Segments, Segment{Start: a, End: b, State: string(iv.State)})
		}
	}
	gaps, err := v.DB.Gaps(uid, ds, de)
	if err != nil {
		return nil, err
	}
	for _, g := range gaps {
		if model.CountsAsTrackingGap(g.Kind) {
			out.Gaps = append(out.Gaps, Segment{Start: maxI(g.Start, ds), End: minI(g.End, de), State: g.Kind})
		}
	}
	out.NoData = len(ivs) == 0 && !out.FromHourly && len(out.Gaps) == 0

	// "vs. usual": the average active time of the seven days before this one,
	// over those days that have any recorded data.
	pds := ds - 7*24*3600*1000
	prev, err := v.DB.Intervals(uid, pds, ds)
	if err != nil {
		return nil, err
	}
	sortedIntervals(prev)
	seen := map[string]int64{}
	for _, iv := range prev {
		for t := maxI(iv.Start, pds); t < minI(iv.End, ds); {
			_, dend := aggregate.DayBounds(v.Loc, t)
			end := minI(minI(iv.End, ds), dend)
			day := aggregate.DayString(v.Loc, t)
			if _, ok := seen[day]; !ok {
				seen[day] = 0
			}
			if iv.State == model.StateActive {
				seen[day] += end - t
			}
			t = end
		}
	}
	for _, ms := range seen {
		out.AvgActive7Ms += ms
	}
	if out.AvgDays = len(seen); out.AvgDays > 0 {
		out.AvgActive7Ms /= int64(out.AvgDays)
	}

	apps, err := v.appsForDay(uid, cfg, date, ds, de, out.IsToday, ivs)
	if err != nil {
		return nil, err
	}
	out.TopApps, out.Categories = topApps(apps, 5), categories(apps)
	out.AppsAvailable = len(apps) > 0

	hb, err := v.hourBuckets(uid, ds, de, false)
	if err != nil {
		return nil, err
	}
	out.Hours = hb
	return out, nil
}

// appsForDay returns per-app totals for one local day. Today is computed from
// the raw samples so it includes the minutes since the last rollup; other days
// come from the stored daily aggregate.
func (v *Viewer) appsForDay(uid int, cfg *config.Config, date string, ds, de int64, today bool, ivs []model.Interval) ([]AppTotal, error) {
	var rows []model.AppDay
	if today {
		capMs := rollup.SampleCap(cfg)
		samples, err := v.DB.Samples(uid, ds-capMs, de+capMs)
		if err != nil {
			return nil, err
		}
		rows = rollup.ComputeApps(cfg, v.Loc, samples, ivs, capMs, ds, de).Daily(uid)
	} else {
		var err error
		if rows, err = v.DB.AppDays(uid, date, date); err != nil {
			return nil, err
		}
	}
	return mergeApps(rows), nil
}

func mergeApps(rows []model.AppDay) []AppTotal {
	m := map[string]*AppTotal{}
	var order []string
	for _, r := range rows {
		t := m[r.AppID]
		if t == nil {
			t = &AppTotal{AppID: r.AppID, Name: r.AppName, Category: r.Category}
			m[r.AppID] = t
			order = append(order, r.AppID)
		}
		if r.AppName != "" && r.AppName != r.AppID {
			t.Name = r.AppName
		}
		t.ForegroundMs += r.ForegroundMs
		t.OpenMs += r.OpenMs
		t.Sessions += r.SessionCount
	}
	out := make([]AppTotal, 0, len(m))
	for _, id := range order {
		out = append(out, *m[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ForegroundMs != out[j].ForegroundMs {
			return out[i].ForegroundMs > out[j].ForegroundMs
		}
		return out[i].AppID < out[j].AppID
	})
	return out
}

func topApps(apps []AppTotal, n int) []AppTotal {
	out := []AppTotal{}
	for _, a := range apps {
		if a.ForegroundMs <= 0 {
			continue
		}
		out = append(out, a)
		if len(out) == n {
			break
		}
	}
	return out
}

func categories(apps []AppTotal) []CategoryTotal {
	m := map[string]int64{}
	for _, a := range apps {
		m[a.Category] += a.ForegroundMs
	}
	out := []CategoryTotal{}
	for c, ms := range m {
		if ms > 0 {
			out = append(out, CategoryTotal{Category: c, ForegroundMs: ms})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ForegroundMs != out[j].ForegroundMs {
			return out[i].ForegroundMs > out[j].ForegroundMs
		}
		return out[i].Category < out[j].Category
	})
	return out
}

// hourBuckets builds the hourly view of [from,to). Completed hours come from the
// stored rows; the hour in progress, and any hour the rollup has not reached
// yet, is computed from the intervals (which include the open one, up to the
// last good sample). Missing data is never shown as zero use (FR-6.25).
func (v *Viewer) hourBuckets(uid int, from, to int64, withApps bool) ([]HourBucket, error) {
	cfg := v.Cfg()
	now := v.now()
	bs := aggregate.BucketsBetween(v.Loc, from, to)
	if len(bs) == 0 {
		return []HourBucket{}, nil
	}
	stored, err := v.DB.HourRows(uid, bs[0].Start, bs[len(bs)-1].End)
	if err != nil {
		return nil, err
	}
	byStart := map[int64]model.HourRow{}
	for _, r := range stored {
		byStart[r.HourStart] = r
	}
	ivs, err := v.DB.Intervals(uid, bs[0].Start, bs[len(bs)-1].End)
	if err != nil {
		return nil, err
	}
	sortedIntervals(ivs)
	gaps, err := v.DB.Gaps(uid, bs[0].Start, bs[len(bs)-1].End)
	if err != nil {
		return nil, err
	}
	first, err := v.DB.FirstIntervalStart(uid)
	if err != nil {
		return nil, err
	}
	if len(stored) > 0 && (first == 0 || stored[0].HourStart < first) {
		first = stored[0].HourStart
	}

	bed := -1
	if cfg.Alerts.Bedtime != "" {
		if m, err := config.ParseClock(cfg.Alerts.Bedtime); err == nil {
			bed = m
		}
	}

	out := make([]HourBucket, 0, len(bs))
	var unstoredFrom, unstoredTo int64
	for _, b := range bs {
		hb := HourBucket{HourStart: b.Start, HourEnd: b.End, Day: b.Day, Hour: b.Hour, LenMs: b.Len(),
			Label:      fmt.Sprintf("%02d:00", b.Hour),
			InProgress: b.Start <= now && now < b.End, Future: b.Start > now}
		if bed >= 0 {
			hb.Bedtime = aggregate.InBedtime(v.Loc, b.Start, bed)
		}
		row, ok := byStart[b.Start]
		if !ok {
			if rs := aggregate.HourRows(uid, v.Loc, ivs, gaps, b.Start, b.End); len(rs) == 1 {
				row, ok = rs[0], true
			}
			if !hb.Future && (unstoredFrom == 0 || b.Start < unstoredFrom) {
				unstoredFrom = b.Start
			}
			if !hb.Future && b.End > unstoredTo {
				unstoredTo = b.End
			}
		}
		if ok {
			hb.ActiveMs, hb.IdleMs, hb.LockedMs, hb.GapMs = row.ActiveMs, row.IdleMs, row.LockedMs, row.GapMs
			hb.NotSignedInMs = aggregate.NotSignedIn(row)
		} else if !hb.Future {
			if first == 0 || b.End <= first {
				hb.NoData = true
			} else {
				hb.NotSignedInMs = b.Len()
			}
		}
		out = append(out, hb)
	}

	if withApps {
		stored, err := v.DB.HourAppRows(uid, bs[0].Start, bs[len(bs)-1].End)
		if err != nil {
			return nil, err
		}
		byHour := map[int64][]AppTotal{}
		for _, r := range stored {
			byHour[r.HourStart] = append(byHour[r.HourStart], AppTotal{AppID: r.AppID, Name: r.AppName, Category: r.Category, ForegroundMs: r.ForegroundMs})
		}
		if unstoredTo > unstoredFrom {
			capMs := rollup.SampleCap(cfg)
			samples, err := v.DB.Samples(uid, unstoredFrom-capMs, unstoredTo+capMs)
			if err != nil {
				return nil, err
			}
			for _, r := range rollup.ComputeApps(cfg, v.Loc, samples, ivs, capMs, unstoredFrom, unstoredTo).Hourly(uid) {
				if _, has := byStart[r.HourStart]; !has {
					byHour[r.HourStart] = append(byHour[r.HourStart], AppTotal{AppID: r.AppID, Name: r.AppName, Category: r.Category, ForegroundMs: r.ForegroundMs})
				}
			}
		}
		for i := range out {
			apps := byHour[out[i].HourStart]
			sort.SliceStable(apps, func(a, b int) bool { return apps[a].ForegroundMs > apps[b].ForegroundMs })
			if len(apps) > 10 {
				apps = apps[:10]
			}
			out[i].Apps = apps
		}
	}
	return out, nil
}

// HourlyDay is the Hourly history screen's day view.
type HourlyDay struct {
	Date          string       `json:"date"`
	DayStart      int64        `json:"day_start"`
	DayEnd        int64        `json:"day_end"`
	NowMs         int64        `json:"now"`
	Buckets       []HourBucket `json:"buckets"`
	ActiveMs      int64        `json:"active_ms"`
	IdleMs        int64        `json:"idle_ms"`
	LockedMs      int64        `json:"locked_ms"`
	GapMs         int64        `json:"gap_ms"`
	DSTShift      bool         `json:"dst_shift"`
	LastActivity  int64        `json:"last_activity"`
	Bedtime       string       `json:"bedtime"`
	AppDetailFrom int64        `json:"app_detail_from"`
	Summary       string       `json:"summary"`
}

// HourlyDay returns the 24 (23 or 25 on DST days) hour buckets of one day.
func (v *Viewer) HourlyDay(uid int, date string, withApps bool) (*HourlyDay, error) {
	ds, de, err := aggregate.ParseDay(v.Loc, date)
	if err != nil {
		return nil, fmt.Errorf("date must be YYYY-MM-DD: %w", err)
	}
	bs, err := v.hourBuckets(uid, ds, de, withApps)
	if err != nil {
		return nil, err
	}
	out := &HourlyDay{Date: date, DayStart: ds, DayEnd: de, NowMs: v.now(), Buckets: bs, DSTShift: len(bs) != 24,
		Bedtime: v.Cfg().Alerts.Bedtime}
	for _, b := range bs {
		out.ActiveMs += b.ActiveMs
		out.IdleMs += b.IdleMs
		out.LockedMs += b.LockedMs
		out.GapMs += b.GapMs
	}
	ivs, err := v.DB.Intervals(uid, ds, de)
	if err != nil {
		return nil, err
	}
	for _, iv := range ivs {
		if iv.State == model.StateActive && minI(iv.End, de) > out.LastActivity {
			out.LastActivity = minI(iv.End, de)
		}
	}
	if out.AppDetailFrom, err = v.DB.EarliestHourApp(uid); err != nil {
		return nil, err
	}
	out.Summary = v.summarize(bs, out.ActiveMs, out.LastActivity)
	return out, nil
}

// summarize writes the plain-language line above the chart, e.g.
// "Most use 16:00–19:00 · last activity 22:40 (after bedtime 21:00) · 3 h 05 m active."
func (v *Viewer) summarize(bs []HourBucket, active, last int64) string {
	if active == 0 {
		return "No active time recorded."
	}
	bestStart, bestSum := -1, int64(0)
	const win = 3
	for i := 0; i+1 <= len(bs); i++ {
		var s int64
		for j := i; j < i+win && j < len(bs); j++ {
			s += bs[j].ActiveMs
		}
		if s > bestSum {
			bestStart, bestSum = i, s
		}
	}
	text := ""
	if bestStart >= 0 {
		end := bestStart + win
		if end > len(bs) {
			end = len(bs)
		}
		// trim idle edges so the range names hours that were actually used
		for bestStart < end && bs[bestStart].ActiveMs == 0 {
			bestStart++
		}
		for end > bestStart && bs[end-1].ActiveMs == 0 {
			end--
		}
		if end > bestStart {
			endLabel := bs[end-1].HourEnd
			text = fmt.Sprintf("Most use %s–%s", bs[bestStart].Label, time.UnixMilli(endLabel).In(v.Loc).Format("15:04"))
		}
	}
	if last > 0 {
		lt := time.UnixMilli(last).In(v.Loc).Format("15:04")
		note := ""
		if bed := v.Cfg().Alerts.Bedtime; bed != "" {
			if m, err := config.ParseClock(bed); err == nil && aggregate.InBedtime(v.Loc, last-1, m) {
				note = fmt.Sprintf(" (after bedtime %s)", bed)
			}
		}
		if text != "" {
			text += " · "
		}
		text += "last activity " + lt + note
	}
	if text != "" {
		text += " · "
	}
	return text + FmtHMSpaced(active) + " active."
}

// HeatCell is one day × hour-of-day cell of the heatmap.
type HeatCell struct {
	Hour     int   `json:"hour"`
	ActiveMs int64 `json:"active_ms"`
	IdleMs   int64 `json:"idle_ms"`
	LockedMs int64 `json:"locked_ms"`
	GapMs    int64 `json:"gap_ms"`
	NoData   bool  `json:"no_data"`
	Future   bool  `json:"future"`
	Skipped  bool  `json:"skipped"` // the hour does not exist on this (spring-forward) day
	Bedtime  bool  `json:"bedtime"`
}

// HeatDay is one row of the heatmap.
type HeatDay struct {
	Date     string     `json:"date"`
	Cells    []HeatCell `json:"cells"`
	ActiveMs int64      `json:"active_ms"`
}

// HourlyRange is the heatmap over several days plus the typical day.
type HourlyRange struct {
	From    string    `json:"from"`
	To      string    `json:"to"`
	Days    []HeatDay `json:"days"`
	AvgMs   []int64   `json:"avg_active_ms"` // per hour of day 0..23
	Bedtime string    `json:"bedtime"`
	Summary string    `json:"summary"`
	NowMs   int64     `json:"now"`
}

// HourlyRange builds the day × hour heatmap for fromDay..toDay inclusive.
func (v *Viewer) HourlyRange(uid int, fromDay, toDay string) (*HourlyRange, error) {
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
	bs, err := v.hourBuckets(uid, fs, te, false)
	if err != nil {
		return nil, err
	}
	out := &HourlyRange{From: fromDay, To: toDay, Bedtime: v.Cfg().Alerts.Bedtime, NowMs: v.now(), Days: []HeatDay{}}
	idx := map[string]int{}
	for _, b := range bs {
		i, ok := idx[b.Day]
		if !ok {
			cells := make([]HeatCell, 24)
			for h := range cells {
				cells[h] = HeatCell{Hour: h, Skipped: true}
			}
			out.Days = append(out.Days, HeatDay{Date: b.Day, Cells: cells})
			i = len(out.Days) - 1
			idx[b.Day] = i
		}
		c := &out.Days[i].Cells[b.Hour]
		if c.Skipped { // first bucket carrying this hour label
			*c = HeatCell{Hour: b.Hour, NoData: b.NoData, Future: b.Future, Bedtime: b.Bedtime}
		} else { // repeated hour on a fall-back day: combine
			c.NoData = c.NoData && b.NoData
			c.Future = c.Future && b.Future
		}
		c.ActiveMs += b.ActiveMs
		c.IdleMs += b.IdleMs
		c.LockedMs += b.LockedMs
		c.GapMs += b.GapMs
		out.Days[i].ActiveMs += b.ActiveMs
	}
	sum := make([]int64, 24)
	n := make([]int64, 24)
	for _, d := range out.Days {
		for _, c := range d.Cells {
			if c.Skipped || c.NoData || c.Future {
				continue
			}
			if c.GapMs > 0 && c.ActiveMs == 0 && c.IdleMs == 0 && c.LockedMs == 0 {
				continue // unknown, not quiet
			}
			sum[c.Hour] += c.ActiveMs
			n[c.Hour]++
		}
	}
	out.AvgMs = make([]int64, 24)
	var best, bestStart int64 = -1, -1
	for h := 0; h < 24; h++ {
		if n[h] > 0 {
			out.AvgMs[h] = sum[h] / n[h]
		}
	}
	for h := 0; h < 24; h++ {
		var s int64
		for k := 0; k < 3 && h+k < 24; k++ {
			s += out.AvgMs[h+k]
		}
		if s > best {
			best, bestStart = s, int64(h)
		}
	}
	if best > 0 {
		out.Summary = fmt.Sprintf("On a typical day most use is %02d:00–%02d:00.", bestStart, minI(bestStart+3, 24)%24)
	} else {
		out.Summary = "No active time recorded in this range."
	}
	return out, nil
}

// DayPoint is one day of the trend charts.
type DayPoint struct {
	Date     string `json:"date"`
	ActiveMs int64  `json:"active_ms"`
	IdleMs   int64  `json:"idle_ms"`
	LockedMs int64  `json:"locked_ms"`
	// FirstMin and LastMin are minutes after local midnight of the first and last
	// active moment; -1 when the day has no active time.
	FirstMin int  `json:"first_min"`
	LastMin  int  `json:"last_min"`
	HasData  bool `json:"has_data"`
}

// Trend returns one point per local day in fromDay..toDay inclusive.
func (v *Viewer) Trend(uid int, fromDay, toDay string) ([]DayPoint, error) {
	fs, _, err := aggregate.ParseDay(v.Loc, fromDay)
	if err != nil {
		return nil, fmt.Errorf("from must be YYYY-MM-DD: %w", err)
	}
	_, te, err := aggregate.ParseDay(v.Loc, toDay)
	if err != nil {
		return nil, fmt.Errorf("to must be YYYY-MM-DD: %w", err)
	}
	if te <= fs || (te-fs)/(24*3600*1000) > 400 {
		return nil, fmt.Errorf("the range must be between 1 and 400 days")
	}
	ivs, err := v.DB.Intervals(uid, fs, te)
	if err != nil {
		return nil, err
	}
	sortedIntervals(ivs)
	hours, err := v.DB.HourRows(uid, fs, te)
	if err != nil {
		return nil, err
	}
	hourByDay := map[string][3]int64{}
	for _, h := range hours {
		a := hourByDay[h.Day]
		a[0] += h.ActiveMs
		a[1] += h.IdleMs
		a[2] += h.LockedMs
		hourByDay[h.Day] = a
	}
	var out []DayPoint
	for s := fs; s < te; {
		_, de := aggregate.DayBounds(v.Loc, s)
		day := aggregate.DayString(v.Loc, s)
		p := DayPoint{Date: day, FirstMin: -1, LastMin: -1}
		var any bool
		var first, last int64
		for _, iv := range ivs {
			a, b := maxI(iv.Start, s), minI(iv.End, de)
			if b <= a {
				continue
			}
			any = true
			switch iv.State {
			case model.StateActive:
				p.ActiveMs += b - a
				if first == 0 || a < first {
					first = a
				}
				if b > last {
					last = b
				}
			case model.StateIdle:
				p.IdleMs += b - a
			case model.StateLocked, model.StateScreenOff:
				p.LockedMs += b - a
			}
		}
		if !any {
			if h, ok := hourByDay[day]; ok {
				p.ActiveMs, p.IdleMs, p.LockedMs, any = h[0], h[1], h[2], true
			}
		}
		if first != 0 {
			p.FirstMin = localMinutes(v.Loc, first)
			p.LastMin = localMinutes(v.Loc, last-1)
			if last >= de { // reaches midnight
				p.LastMin = 24*60 - 1
			}
		}
		p.HasData = any
		out = append(out, p)
		s = de
	}
	return out, nil
}

func localMinutes(loc *time.Location, ms int64) int {
	t := time.UnixMilli(ms).In(loc)
	return t.Hour()*60 + t.Minute()
}

// AppsView lists application usage over a range.
type AppsView struct {
	From          string          `json:"from"`
	To            string          `json:"to"`
	Apps          []AppTotal      `json:"apps"`
	Categories    []CategoryTotal `json:"categories"`
	TotalMs       int64           `json:"total_foreground_ms"`
	AppsAvailable bool            `json:"apps_available"`
}

// Apps totals application usage over fromDay..toDay inclusive. Stored daily
// aggregates are used for every day except today, which is computed live.
func (v *Viewer) Apps(uid int, fromDay, toDay string) (*AppsView, error) {
	rows, err := v.appRows(uid, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	apps := mergeApps(rows)
	out := &AppsView{From: fromDay, To: toDay, Apps: apps, Categories: categories(apps), AppsAvailable: len(apps) > 0}
	if out.Apps == nil {
		out.Apps = []AppTotal{}
	}
	for _, a := range apps {
		out.TotalMs += a.ForegroundMs
	}
	return out, nil
}

func (v *Viewer) appRows(uid int, fromDay, toDay string) ([]model.AppDay, error) {
	if _, _, err := aggregate.ParseDay(v.Loc, fromDay); err != nil {
		return nil, fmt.Errorf("from must be YYYY-MM-DD: %w", err)
	}
	if _, _, err := aggregate.ParseDay(v.Loc, toDay); err != nil {
		return nil, fmt.Errorf("to must be YYYY-MM-DD: %w", err)
	}
	rows, err := v.DB.AppDays(uid, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	now := v.now()
	ts, te := aggregate.DayBounds(v.Loc, now)
	today := aggregate.DayString(v.Loc, now)
	if today >= fromDay && today <= toDay {
		kept := rows[:0]
		for _, r := range rows {
			if r.Day != today {
				kept = append(kept, r)
			}
		}
		cfg := v.Cfg()
		ivs, err := v.DB.Intervals(uid, ts, te)
		if err != nil {
			return nil, err
		}
		sortedIntervals(ivs)
		capMs := rollup.SampleCap(cfg)
		samples, err := v.DB.Samples(uid, ts-capMs, te+capMs)
		if err != nil {
			return nil, err
		}
		rows = append(kept, rollup.ComputeApps(cfg, v.Loc, samples, ivs, capMs, ts, te).Daily(uid)...)
	}
	return rows, nil
}

// AppDayPoint is one day of an app's usage.
type AppDayPoint struct {
	Date         string `json:"date"`
	ForegroundMs int64  `json:"foreground_ms"`
	OpenMs       int64  `json:"open_ms"`
	Sessions     int64  `json:"sessions"`
}

// AppDetail is the App detail view.
type AppDetail struct {
	AppID        string        `json:"app_id"`
	Name         string        `json:"name"`
	Category     string        `json:"category"`
	ForegroundMs int64         `json:"foreground_ms"`
	OpenMs       int64         `json:"open_ms"`
	Sessions     int64         `json:"sessions"`
	Days         []AppDayPoint `json:"days"`
	HourOfDayMs  []int64       `json:"hour_of_day_ms"` // typical time of day, 0..23
}

// AppDetail reports one application's usage over a range.
func (v *Viewer) AppDetail(uid int, appID, fromDay, toDay string) (*AppDetail, error) {
	rows, err := v.appRows(uid, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	out := &AppDetail{AppID: appID, Days: []AppDayPoint{}, HourOfDayMs: make([]int64, 24)}
	for _, r := range rows {
		if r.AppID != appID {
			continue
		}
		out.Name, out.Category = r.AppName, r.Category
		out.ForegroundMs += r.ForegroundMs
		out.OpenMs += r.OpenMs
		out.Sessions += r.SessionCount
		out.Days = append(out.Days, AppDayPoint{Date: r.Day, ForegroundMs: r.ForegroundMs, OpenMs: r.OpenMs, Sessions: r.SessionCount})
	}
	sort.Slice(out.Days, func(i, j int) bool { return out.Days[i].Date < out.Days[j].Date })
	fs, _, _ := aggregate.ParseDay(v.Loc, fromDay)
	_, te, _ := aggregate.ParseDay(v.Loc, toDay)
	hrows, err := v.DB.HourAppRows(uid, fs, te)
	if err != nil {
		return nil, err
	}
	for _, h := range hrows {
		if h.AppID == appID {
			out.HourOfDayMs[time.UnixMilli(h.HourStart).In(v.Loc).Hour()] += h.ForegroundMs
		}
	}
	if out.Name == "" {
		out.Name = appID
	}
	return out, nil
}

func maxI(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minI(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

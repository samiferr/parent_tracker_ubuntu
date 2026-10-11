// Package aggregate holds the pure arithmetic behind daily and hourly figures.
//
// Everything here is derived from the same activity intervals, clipped with
// exact integer-millisecond arithmetic, so a day's total is by construction the
// sum of its hourly buckets (FR-6.22, AC-91). Hour boundaries are real instants
// computed from the time-zone database, so DST days have 23 or 25 buckets and
// half-hour-offset zones bucket by local hour (FR-6.25, AC-92).
package aggregate

import (
	"sort"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// Totals is time per state. Locked includes screen-off time, as in the hourly
// tables; suspended time is kept separately and is not "signed in" time.
type Totals struct {
	Active    int64
	Idle      int64
	Locked    int64
	Suspended int64
}

// Add adds ms of state s.
func (t *Totals) Add(s model.State, ms int64) {
	switch s {
	case model.StateActive:
		t.Active += ms
	case model.StateIdle:
		t.Idle += ms
	case model.StateLocked, model.StateScreenOff:
		t.Locked += ms
	case model.StateSuspended:
		t.Suspended += ms
	}
}

// Sum is the total of all states.
func (t Totals) Sum() int64 { return t.Active + t.Idle + t.Locked + t.Suspended }

// Plus returns t + o.
func (t Totals) Plus(o Totals) Totals {
	return Totals{t.Active + o.Active, t.Idle + o.Idle, t.Locked + o.Locked, t.Suspended + o.Suspended}
}

// overlap is the length of [a1,a2) ∩ [b1,b2), never negative.
func overlap(a1, a2, b1, b2 int64) int64 {
	lo, hi := a1, a2
	if b1 > lo {
		lo = b1
	}
	if b2 < hi {
		hi = b2
	}
	if hi <= lo {
		return 0
	}
	return hi - lo
}

// Overlap is exported for callers that clip gaps.
func Overlap(a1, a2, b1, b2 int64) int64 { return overlap(a1, a2, b1, b2) }

// SortIntervals orders intervals by start (then end), in place.
func SortIntervals(ivs []model.Interval) {
	sort.SliceStable(ivs, func(i, j int) bool {
		if ivs[i].Start != ivs[j].Start {
			return ivs[i].Start < ivs[j].Start
		}
		return ivs[i].End < ivs[j].End
	})
}

// ClipTotals sums the part of each interval that lies inside [from,to).
func ClipTotals(ivs []model.Interval, from, to int64) Totals {
	var t Totals
	for _, iv := range ivs {
		if ms := overlap(iv.Start, iv.End, from, to); ms > 0 {
			t.Add(iv.State, ms)
		}
	}
	return t
}

// Bucket is one local-time hour. End-Start is the hour's real length: 3 600 000
// ms normally, shorter or longer only for unusual DST shifts.
type Bucket struct {
	Start int64
	End   int64
	Hour  int    // local hour of day at Start (a label; not unique on a fall-back day)
	Day   string // local date YYYY-MM-DD at Start
}

// Len is the bucket length in ms.
func (b Bucket) Len() int64 { return b.End - b.Start }

const hourMs = int64(3600 * 1000)

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// hourStartOf is the first instant of the local hour containing t.
func hourStartOf(t time.Time) time.Time {
	_, off := t.Zone()
	start, _ := t.ZoneBounds()
	s := floorDiv(t.Unix()+int64(off), 3600)*3600 - int64(off)
	st := time.Unix(s, 0).In(t.Location())
	if !start.IsZero() && st.Before(start) {
		return start
	}
	return st
}

// nextHourBoundary is the first local-hour boundary strictly after t. Inside a
// zone period the offset is constant, so boundaries fall where (unix+offset) is
// a multiple of one hour; a zone transition always ends the current bucket.
func nextHourBoundary(t time.Time) time.Time {
	_, off := t.Zone()
	_, zend := t.ZoneBounds()
	next := (floorDiv(t.Unix()+int64(off), 3600)+1)*3600 - int64(off)
	nt := time.Unix(next, 0).In(t.Location())
	if !zend.IsZero() && nt.After(zend) {
		return zend
	}
	return nt
}

// BucketAt returns the hour bucket containing instant ms.
func BucketAt(loc *time.Location, ms int64) Bucket {
	t := time.UnixMilli(ms).In(loc)
	s := hourStartOf(t)
	e := nextHourBoundary(s)
	return Bucket{Start: s.UnixMilli(), End: e.UnixMilli(), Hour: s.Hour(), Day: s.Format("2006-01-02")}
}

// BucketsBetween lists every hour bucket that overlaps [from,to).
func BucketsBetween(loc *time.Location, from, to int64) []Bucket {
	if to <= from {
		return nil
	}
	var out []Bucket
	b := BucketAt(loc, from)
	for b.Start < to {
		out = append(out, b)
		next := BucketAt(loc, b.End)
		if next.Start <= b.Start { // cannot happen; refuse to loop forever if it ever does
			break
		}
		b = next
	}
	return out
}

// DayBounds returns the instants at which the local day containing ms begins
// and ends.
func DayBounds(loc *time.Location, ms int64) (start, end int64) {
	t := time.UnixMilli(ms).In(loc)
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc).UnixMilli(), time.Date(y, m, d+1, 0, 0, 0, 0, loc).UnixMilli()
}

// ParseDay parses YYYY-MM-DD as a local date and returns the day's bounds.
func ParseDay(loc *time.Location, day string) (start, end int64, err error) {
	t, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return 0, 0, err
	}
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc).UnixMilli(), time.Date(y, m, d+1, 0, 0, 0, 0, loc).UnixMilli(), nil
}

// DayBuckets lists the hour buckets of one local day: 24 normally, 23 on a
// spring-forward day and 25 on a fall-back day.
func DayBuckets(loc *time.Location, day string) ([]Bucket, error) {
	s, e, err := ParseDay(loc, day)
	if err != nil {
		return nil, err
	}
	return BucketsBetween(loc, s, e), nil
}

// DayString formats an instant as its local date.
func DayString(loc *time.Location, ms int64) string {
	return time.UnixMilli(ms).In(loc).Format("2006-01-02")
}

// Span is a period during which one application was in front.
type Span struct {
	AppID string
	Start int64
	End   int64
}

// BuildSpans turns app samples (sorted by time) into spans. A sample holds
// until the next sample, but never longer than capMs, so a stalled sensor
// cannot inflate usage. The last sample holds for capMs.
func BuildSpans(samples []model.Sample, capMs int64) []Span {
	spans := make([]Span, 0, len(samples))
	for i, s := range samples {
		end := s.TS + capMs
		if i+1 < len(samples) && samples[i+1].TS < end {
			end = samples[i+1].TS
		}
		if end <= s.TS || s.AppID == "" {
			continue
		}
		spans = append(spans, Span{AppID: s.AppID, Start: s.TS, End: end})
	}
	return spans
}

// MergeSpans joins consecutive spans of the same app that touch, so that one
// uninterrupted stay counts as one visit.
func MergeSpans(spans []Span) []Span {
	var out []Span
	for _, sp := range spans {
		if n := len(out); n > 0 && out[n-1].AppID == sp.AppID && sp.Start <= out[n-1].End {
			if sp.End > out[n-1].End {
				out[n-1].End = sp.End
			}
			continue
		}
		out = append(out, sp)
	}
	return out
}

// Piece is the part of an app span that fell in one state.
type Piece struct {
	AppID string
	State model.State
	Start int64
	End   int64
}

// IntersectSpans cuts each span against the intervals (which must be sorted by
// start and non-overlapping, as the tracker writes them) and reports one piece
// per overlap. Only states that count as "app open" are reported.
func IntersectSpans(spans []Span, ivs []model.Interval, fn func(Piece)) {
	for _, sp := range spans {
		// first interval that ends after the span starts
		i := sort.Search(len(ivs), func(k int) bool { return ivs[k].End > sp.Start })
		for ; i < len(ivs) && ivs[i].Start < sp.End; i++ {
			iv := ivs[i]
			if iv.State != model.StateActive && iv.State != model.StateIdle {
				continue
			}
			a, b := sp.Start, sp.End
			if iv.Start > a {
				a = iv.Start
			}
			if iv.End < b {
				b = iv.End
			}
			if b > a {
				fn(Piece{AppID: sp.AppID, State: iv.State, Start: a, End: b})
			}
		}
	}
}

// SplitHours calls fn for each part of [a,b) that falls in one hour bucket.
func SplitHours(loc *time.Location, a, b int64, fn func(Bucket, int64)) {
	for a < b {
		bk := BucketAt(loc, a)
		end := bk.End
		if end > b {
			end = b
		}
		if end <= a {
			return
		}
		fn(bk, end-a)
		a = end
	}
}

// SplitDays calls fn for each part of [a,b) that falls in one local day.
func SplitDays(loc *time.Location, a, b int64, fn func(day string, ms int64)) {
	for a < b {
		_, de := DayBounds(loc, a)
		end := de
		if end > b {
			end = b
		}
		if end <= a {
			return
		}
		fn(DayString(loc, a), end-a)
		a = end
	}
}

// HourRows builds the per-hour state rows for one user over [from,to). Gaps are
// the user's tracking gaps (kinds that count as tracking gaps only). Buckets in
// which nothing at all was recorded produce no row.
func HourRows(uid int, loc *time.Location, ivs []model.Interval, gaps []model.Gap, from, to int64) []model.HourRow {
	var rows []model.HourRow
	for _, b := range BucketsBetween(loc, from, to) {
		t := ClipTotals(ivs, b.Start, b.End)
		var gapMs int64
		for _, g := range gaps {
			if model.CountsAsTrackingGap(g.Kind) {
				gapMs += overlap(g.Start, g.End, b.Start, b.End)
			}
		}
		if gapMs > b.Len() {
			gapMs = b.Len()
		}
		if t.Active == 0 && t.Idle == 0 && t.Locked == 0 && gapMs == 0 {
			continue
		}
		rows = append(rows, model.HourRow{
			UID: uid, HourStart: b.Start, Day: b.Day, Hour: b.Hour, HourLenMs: b.Len(),
			ActiveMs: t.Active, IdleMs: t.Idle, LockedMs: t.Locked, GapMs: gapMs,
		})
	}
	return rows
}

// NotSignedIn is the part of an hour that is none of active, idle, locked or
// gap: not signed in, suspended or powered off (docs/05 §5.1). It is derived at
// read time and never stored.
func NotSignedIn(r model.HourRow) int64 {
	ms := r.HourLenMs - r.ActiveMs - r.IdleMs - r.LockedMs - r.GapMs
	if ms < 0 {
		return 0
	}
	return ms
}

// InBedtime reports whether the local clock time of ms lies in the bedtime
// window: from the configured bedtime until 06:00 the next morning.
func InBedtime(loc *time.Location, ms int64, bedtimeMinutes int) bool {
	const endMin = 6 * 60
	t := time.UnixMilli(ms).In(loc)
	m := t.Hour()*60 + t.Minute()
	if bedtimeMinutes >= endMin {
		return m >= bedtimeMinutes || m < endMin
	}
	return m >= bedtimeMinutes && m < endMin
}

package aggregate

import (
	"math/rand"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tzdata for %s not available: %v", name, err)
	}
	return loc
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func iv(state model.State, s, e time.Time) model.Interval {
	return model.Interval{UID: 1000, State: state, Start: ms(s), End: ms(e)}
}

// AC-90: an active interval 10:50–11:20 yields 10 min in the 10:00 bucket and
// 20 min in the 11:00 bucket.
func TestIntervalIsSplitAtTheHourBoundary(t *testing.T) {
	loc := mustLoc(t, "Europe/Paris")
	d := func(h, m int) time.Time { return time.Date(2026, 10, 9, h, m, 0, 0, loc) }
	ivs := []model.Interval{iv(model.StateActive, d(10, 50), d(11, 20))}
	rows := HourRows(1000, loc, ivs, nil, ms(d(9, 0)), ms(d(13, 0)))
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Hour != 10 || rows[0].ActiveMs != 10*60*1000 {
		t.Fatalf("10:00 bucket = %+v", rows[0])
	}
	if rows[1].Hour != 11 || rows[1].ActiveMs != 20*60*1000 {
		t.Fatalf("11:00 bucket = %+v", rows[1])
	}
}

func TestDayBucketCountsAcrossDST(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	cases := []struct {
		loc  *time.Location
		day  string
		want int
	}{
		{ny, "2026-03-08", 23}, // spring forward
		{ny, "2026-11-01", 25}, // fall back
		{ny, "2026-06-15", 24},
		{mustLoc(t, "Asia/Kolkata"), "2026-10-09", 24},
		{mustLoc(t, "Europe/Paris"), "2026-03-29", 23},
		{mustLoc(t, "Europe/Paris"), "2026-10-25", 25},
		{time.UTC, "2026-10-09", 24},
	}
	for _, c := range cases {
		bs, err := DayBuckets(c.loc, c.day)
		if err != nil {
			t.Fatal(err)
		}
		if len(bs) != c.want {
			t.Errorf("%s %s: %d buckets, want %d", c.loc, c.day, len(bs), c.want)
		}
		// buckets tile the day exactly
		s, e, _ := ParseDay(c.loc, c.day)
		if bs[0].Start != s || bs[len(bs)-1].End != e {
			t.Errorf("%s %s: buckets do not span the day", c.loc, c.day)
		}
		for i := 1; i < len(bs); i++ {
			if bs[i].Start != bs[i-1].End {
				t.Errorf("%s %s: hole between bucket %d and %d", c.loc, c.day, i-1, i)
			}
		}
	}
}

// AC-92: on a fall-back day there are two buckets labelled 01:00 with distinct
// hour_start values.
func TestFallBackDayHasTwoOneOClockBuckets(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	bs, _ := DayBuckets(ny, "2026-11-01")
	var ones []Bucket
	for _, b := range bs {
		if b.Hour == 1 {
			ones = append(ones, b)
		}
	}
	if len(ones) != 2 || ones[0].Start == ones[1].Start {
		t.Fatalf("expected two distinct 01:00 buckets, got %+v", ones)
	}
	for _, b := range ones {
		if b.Len() != hourMs {
			t.Errorf("bucket length = %d", b.Len())
		}
	}
	// spring-forward: there is no 02:00 bucket
	sf, _ := DayBuckets(ny, "2026-03-08")
	for _, b := range sf {
		if b.Hour == 2 {
			t.Fatalf("spring-forward day must skip 02:00, got %+v", b)
		}
	}
}

// AC-92: a half-hour-offset zone buckets by local hour.
func TestHalfHourOffsetZone(t *testing.T) {
	ist := mustLoc(t, "Asia/Kolkata")
	// 16:10 IST is 10:40 UTC; its bucket is 16:00–17:00 IST = 10:30–11:30 UTC.
	at := time.Date(2026, 10, 9, 16, 10, 0, 0, ist)
	b := BucketAt(ist, ms(at))
	if b.Hour != 16 || b.Len() != hourMs {
		t.Fatalf("bucket = %+v", b)
	}
	if got := time.UnixMilli(b.Start).UTC().Format("15:04"); got != "10:30" {
		t.Fatalf("bucket starts at %s UTC, want 10:30", got)
	}
}

// AC-91 / FR-6.22: the 24 hourly buckets of a day sum exactly to the day's
// figure, on ordinary and DST days, with intervals that cross hour and
// midnight boundaries.
func TestDailyEqualsSumOfHourly(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	states := []model.State{model.StateActive, model.StateIdle, model.StateLocked, model.StateScreenOff}
	for _, tc := range []struct{ zone, day string }{
		{"America/New_York", "2026-03-08"}, {"America/New_York", "2026-11-01"},
		{"Asia/Kolkata", "2026-10-09"}, {"Europe/Paris", "2026-10-25"}, {"UTC", "2026-10-09"},
	} {
		loc := mustLoc(t, tc.zone)
		ds, de, _ := ParseDay(loc, tc.day)
		var ivs []model.Interval
		cur := ds - 3*hourMs // start before the day so an interval crosses midnight
		for cur < de+3*hourMs {
			length := int64(rng.Intn(110*60*1000) + 1000)
			ivs = append(ivs, model.Interval{UID: 1, State: states[rng.Intn(len(states))], Start: cur, End: cur + length})
			cur += length + int64(rng.Intn(20*60*1000)) // leave holes
		}
		daily := ClipTotals(ivs, ds, de)
		var sum Totals
		for _, r := range HourRows(1, loc, ivs, nil, ds, de) {
			sum = sum.Plus(Totals{Active: r.ActiveMs, Idle: r.IdleMs, Locked: r.LockedMs})
		}
		if sum.Active != daily.Active || sum.Idle != daily.Idle || sum.Locked != daily.Locked {
			t.Errorf("%s %s: hourly sum %+v != daily %+v", tc.zone, tc.day, sum, daily)
		}
	}
}

// AC-93: locked time adds no active minutes; a tracking gap is reported as a gap.
func TestLockedAddsNoActiveAndGapsAreReported(t *testing.T) {
	loc := time.UTC
	d := func(h, m int) time.Time { return time.Date(2026, 10, 9, h, m, 0, 0, loc) }
	ivs := []model.Interval{
		iv(model.StateLocked, d(14, 0), d(14, 30)),
		iv(model.StateActive, d(15, 0), d(15, 20)),
	}
	gaps := []model.Gap{
		{Kind: model.GapCrash, Start: ms(d(15, 20)), End: ms(d(15, 45))},
		{Kind: "app_sensor", Start: ms(d(15, 0)), End: ms(d(15, 10))}, // app-only gap: ignored
	}
	rows := HourRows(1, loc, ivs, gaps, ms(d(14, 0)), ms(d(16, 0)))
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].ActiveMs != 0 || rows[0].LockedMs != 30*60*1000 {
		t.Fatalf("14:00 = %+v", rows[0])
	}
	if rows[1].ActiveMs != 20*60*1000 || rows[1].GapMs != 25*60*1000 {
		t.Fatalf("15:00 = %+v", rows[1])
	}
	// 15:00 hour: 20 active + 25 gap leaves 15 not signed in
	if got := NotSignedIn(rows[1]); got != 15*60*1000 {
		t.Fatalf("not signed in = %d", got)
	}
}

func TestHoursWithoutDataProduceNoRow(t *testing.T) {
	rows := HourRows(1, time.UTC, nil, nil, 0, 5*hourMs)
	if len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestSuspendedIsNeitherActiveNorLocked(t *testing.T) {
	d := func(h int) int64 { return time.Date(2026, 10, 9, h, 0, 0, 0, time.UTC).UnixMilli() }
	ivs := []model.Interval{{State: model.StateSuspended, Start: d(10), End: d(12)}}
	tot := ClipTotals(ivs, d(0), d(23))
	if tot.Active != 0 || tot.Locked != 0 || tot.Suspended != 2*hourMs {
		t.Fatalf("totals = %+v", tot)
	}
	if rows := HourRows(1, time.UTC, ivs, nil, d(0), d(23)); len(rows) != 0 {
		t.Fatalf("a suspended hour is 'not signed in', not a stored row: %+v", rows)
	}
}

func TestBuildSpansCapsStaleSamples(t *testing.T) {
	s := func(ts int64, app string) model.Sample { return model.Sample{TS: ts, AppID: app} }
	const sec = 1000
	spans := BuildSpans([]model.Sample{s(0, "a"), s(5*sec, "a"), s(10*sec, "b"), s(600*sec, "b")}, 10*sec)
	want := []Span{{"a", 0, 5 * sec}, {"a", 5 * sec, 10 * sec}, {"b", 10 * sec, 20 * sec}, {"b", 600 * sec, 610 * sec}}
	if len(spans) != len(want) {
		t.Fatalf("spans = %+v", spans)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}
	merged := MergeSpans(spans)
	if len(merged) != 3 || merged[0].End != 10*sec {
		t.Fatalf("merged = %+v", merged)
	}
}

// AC-95 / AC-10: two apps used 10 and 20 minutes report those values, and only
// the active part of a span counts as foreground.
func TestAppTimeFollowsActiveIntervals(t *testing.T) {
	const min = int64(60 * 1000)
	var samples []model.Sample
	for ts := int64(0); ts < 30*min; ts += 5000 {
		app := "firefox"
		if ts < 10*min {
			app = "code"
		}
		samples = append(samples, model.Sample{TS: ts, AppID: app})
	}
	// user is idle for minutes 20–25 (firefox stays in front)
	ivs := []model.Interval{
		{State: model.StateActive, Start: 0, End: 20 * min},
		{State: model.StateIdle, Start: 20 * min, End: 25 * min},
		{State: model.StateActive, Start: 25 * min, End: 30 * min},
	}
	fg := map[string]int64{}
	open := map[string]int64{}
	IntersectSpans(MergeSpans(BuildSpans(samples, 10000)), ivs, func(p Piece) {
		open[p.AppID] += p.End - p.Start
		if p.State == model.StateActive {
			fg[p.AppID] += p.End - p.Start
		}
	})
	if fg["code"] != 10*min {
		t.Errorf("code foreground = %d min", fg["code"]/min)
	}
	if fg["firefox"] != 15*min { // 20 min in front minus 5 idle
		t.Errorf("firefox foreground = %d min, want 15", fg["firefox"]/min)
	}
	if open["firefox"] != 20*min {
		t.Errorf("firefox open = %d min, want 20 (AC-11: open is not used)", open["firefox"]/min)
	}
}

func TestInBedtime(t *testing.T) {
	at := func(h, m int) int64 { return time.Date(2026, 10, 9, h, m, 0, 0, time.UTC).UnixMilli() }
	cases := []struct {
		bed  int
		h, m int
		want bool
	}{
		{21 * 60, 20, 59, false}, {21 * 60, 21, 0, true}, {21 * 60, 23, 59, true},
		{21 * 60, 0, 30, true}, {21 * 60, 5, 59, true}, {21 * 60, 6, 0, false}, {21 * 60, 12, 0, false},
		{30, 0, 29, false}, {30, 0, 30, true}, {30, 5, 0, true}, {30, 7, 0, false},
	}
	for _, c := range cases {
		if got := InBedtime(time.UTC, at(c.h, c.m), c.bed); got != c.want {
			t.Errorf("bed=%d at %02d:%02d = %v", c.bed, c.h, c.m, got)
		}
	}
}

func TestSplitDaysAndHours(t *testing.T) {
	loc := time.UTC
	a := time.Date(2026, 10, 9, 23, 30, 0, 0, loc)
	b := time.Date(2026, 10, 10, 0, 30, 0, 0, loc)
	got := map[string]int64{}
	SplitDays(loc, ms(a), ms(b), func(day string, d int64) { got[day] += d })
	if got["2026-10-09"] != 30*60*1000 || got["2026-10-10"] != 30*60*1000 {
		t.Fatalf("days = %v", got)
	}
	var n int
	SplitHours(loc, ms(a), ms(b), func(Bucket, int64) { n++ })
	if n != 2 {
		t.Fatalf("hour pieces = %d", n)
	}
}

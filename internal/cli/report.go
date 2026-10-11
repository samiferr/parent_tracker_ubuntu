package cli

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/view"
)

func minutes(ms int64) string { return strconv.FormatFloat(float64(ms)/60000, 'f', 1, 64) }

func clockMin(m int) string {
	if m < 0 {
		return "—"
	}
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

func dash(ms int64) string {
	if ms == 0 {
		return "—"
	}
	return view.FmtHM(ms)
}

func (a *App) cmdReport(args []string) int {
	fs, cfgPath := a.newFlags("report")
	userName := fs.String("user", "", "monitored user")
	from := fs.String("from", "", "first day, YYYY-MM-DD (default: 6 days ago)")
	to := fs.String("to", "", "last day, YYYY-MM-DD (default: today)")
	format := fs.String("format", "text", "text or csv")
	by := fs.String("by", "day", "day or hour")
	date := fs.String("date", "", "with --by hour: one day, YYYY-MM-DD (default: today)")
	if _, err := parse(fs, args); err != nil {
		return 2
	}
	if *format != "text" && *format != "csv" {
		return a.errorf("--format must be text or csv")
	}
	if *by != "day" && *by != "hour" {
		return a.errorf("--by must be day or hour")
	}
	cfg, err := a.loadConfig(*cfgPath)
	if err != nil {
		return a.errorf("%v", err)
	}
	db, err := a.openRead(cfg)
	if err != nil {
		return a.errorf("%v", err)
	}
	defer db.Close()
	u, err := a.pickUser(cfg, db, *userName)
	if err != nil {
		return a.errorf("%v", err)
	}
	v := a.viewer(cfg, db)

	today := a.today()
	ago := func(n int) string { return aggregate.DayString(a.Loc, a.Now().UnixMilli()-int64(n)*24*3600*1000) }
	fromDay, toDay := *from, *to
	if *by == "hour" && *date != "" {
		fromDay, toDay = *date, *date
	}
	if toDay == "" {
		toDay = today
	}
	if fromDay == "" {
		if *by == "hour" {
			fromDay = toDay
		} else {
			fromDay = ago(6)
		}
	}
	if *by == "hour" {
		return a.reportHours(v, u.UID, u.Username, fromDay, toDay, *format)
	}
	return a.reportDays(v, u.UID, u.Username, fromDay, toDay, *format)
}

func (a *App) reportDays(v *view.Viewer, uid int, name, from, to, format string) int {
	days, err := v.DailyReport(uid, from, to)
	if err != nil {
		return a.errorf("%v", err)
	}
	if format == "csv" {
		w := csv.NewWriter(a.Stdout)
		w.Write([]string{"user", "date", "active_minutes", "idle_minutes", "locked_minutes", "first_activity", "last_activity", "top_app", "top_app_minutes"})
		for _, d := range days {
			first, last := "", ""
			if d.FirstMin >= 0 {
				first, last = clockMin(d.FirstMin), clockMin(d.LastMin)
			}
			w.Write([]string{name, d.Date, minutes(d.ActiveMs), minutes(d.IdleMs), minutes(d.LockedMs), first, last, d.TopApp, minutes(d.TopAppMs)})
		}
		w.Flush()
		return 0
	}
	a.printf("%s — %s to %s\n", name, from, to)
	tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "date\tactive\tidle\tlocked\tfirst\tlast\ttop app")
	var tot struct{ act, idle, lock int64 }
	for _, d := range days {
		top := "—"
		if d.TopApp != "" {
			top = fmt.Sprintf("%s (%s)", d.TopApp, view.FmtHM(d.TopAppMs))
		}
		if !d.HasData {
			fmt.Fprintf(tw, "%s\tno data\t\t\t\t\t\n", d.Date)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", d.Date, dash(d.ActiveMs), dash(d.IdleMs), dash(d.LockedMs), clockMin(d.FirstMin), clockMin(d.LastMin), top)
		tot.act += d.ActiveMs
		tot.idle += d.IdleMs
		tot.lock += d.LockedMs
	}
	fmt.Fprintf(tw, "total\t%s\t%s\t%s\t\t\t\n", view.FmtHM(tot.act), view.FmtHM(tot.idle), view.FmtHM(tot.lock))
	tw.Flush()
	return 0
}

func (a *App) reportHours(v *view.Viewer, uid int, name, from, to, format string) int {
	fs, _, err := aggregate.ParseDay(a.Loc, from)
	if err != nil {
		return a.errorf("dates must look like 2026-10-09")
	}
	_, te, err := aggregate.ParseDay(a.Loc, to)
	if err != nil {
		return a.errorf("dates must look like 2026-10-09")
	}
	if te <= fs {
		return a.errorf("--to must not be before --from")
	}
	var csvw *csv.Writer
	if format == "csv" {
		csvw = csv.NewWriter(a.Stdout)
		csvw.Write([]string{"user", "date", "hour", "hour_start", "active_minutes", "idle_minutes", "locked_minutes", "gap_minutes", "top_app", "top_app_minutes"})
		defer csvw.Flush()
	}
	for s := fs; s < te; {
		_, de := aggregate.DayBounds(a.Loc, s)
		day := aggregate.DayString(a.Loc, s)
		hd, err := v.HourlyDay(uid, day, true)
		if err != nil {
			return a.errorf("%v", err)
		}
		s = de
		var rows []view.HourBucket
		var tot struct{ act, idle, lock, gap int64 }
		for _, b := range hd.Buckets {
			if b.ActiveMs == 0 && b.IdleMs == 0 && b.LockedMs == 0 && b.GapMs == 0 {
				continue
			}
			rows = append(rows, b)
			tot.act, tot.idle, tot.lock, tot.gap = tot.act+b.ActiveMs, tot.idle+b.IdleMs, tot.lock+b.LockedMs, tot.gap+b.GapMs
		}
		if csvw != nil {
			for _, b := range rows {
				top, topMs := "", int64(0)
				if len(b.Apps) > 0 {
					top, topMs = appLabel(b.Apps[0]), b.Apps[0].ForegroundMs
				}
				csvw.Write([]string{name, b.Day, b.Label, strconv.FormatInt(b.HourStart, 10), minutes(b.ActiveMs), minutes(b.IdleMs), minutes(b.LockedMs), minutes(b.GapMs), top, minutes(topMs)})
			}
			continue
		}
		if len(rows) == 0 && fs+24*3600*1000 < te {
			continue // in a range, skip days with nothing
		}
		a.printf("%s — %s (by hour, local time)\n", name, day)
		tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "hour\tactive\tidle\tlocked\tgap\ttop app")
		for _, b := range rows {
			top := "—"
			if len(b.Apps) > 0 {
				top = fmt.Sprintf("%s (%s)", appLabel(b.Apps[0]), view.FmtHM(b.Apps[0].ForegroundMs))
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", b.Label, shortM(b.ActiveMs), shortM(b.IdleMs), shortM(b.LockedMs), gapM(b.GapMs), top)
		}
		fmt.Fprintf(tw, "total\t%s\t%s\t%s\t%s\t\n", view.FmtHM(tot.act), view.FmtHM(tot.idle), view.FmtHM(tot.lock), view.FmtHM(tot.gap))
		tw.Flush()
		a.println()
	}
	return 0
}

func appLabel(t view.AppTotal) string {
	if t.Name != "" {
		return t.Name
	}
	return t.AppID
}

func shortM(ms int64) string { return view.FmtHM(ms) }

func gapM(ms int64) string {
	if ms == 0 {
		return "—"
	}
	return view.FmtHM(ms)
}

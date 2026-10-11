package view

import "github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"

// SelfView is everything the child can see about their own account: the same
// day summary and hour-by-hour history the parent sees (SEC-3, SEC-43).
type SelfView struct {
	User   string      `json:"user"`
	Date   string      `json:"date"`
	Day    *DaySummary `json:"day"`
	Hourly *HourlyDay  `json:"hourly"`
	Apps   *AppsView   `json:"apps_7d"`
	Web    []string    `json:"websites"`
	// LiveVisible is always true: the parent can see the current state and the
	// app in front, live (SEC-6).
	LiveVisible bool             `json:"parent_sees_live"`
	Recorded    []string         `json:"recorded"`
	Counts      map[string]int64 `json:"counts"`
}

// Self builds the child's read-only view for today (or the given date).
func (v *Viewer) Self(uid int, user, date string) (*SelfView, error) {
	if date == "" {
		date = aggregate.DayString(v.Loc, v.now())
	}
	day, err := v.Day(uid, date)
	if err != nil {
		return nil, err
	}
	hourly, err := v.HourlyDay(uid, date, true)
	if err != nil {
		return nil, err
	}
	ds, _, _ := aggregate.ParseDay(v.Loc, date)
	from := aggregate.DayString(v.Loc, ds-6*24*3600*1000)
	apps, err := v.Apps(uid, from, date)
	if err != nil {
		return nil, err
	}
	cfg := v.Cfg()
	rec := []string{
		"When your session is active, idle or locked, and when you sign in and out",
		"Which application is in front, and for how long",
		"Your usage hour by hour",
		"When recording was interrupted",
	}
	if cfg.Collectors.WindowTitle {
		rec = append(rec, "The titles of your windows")
	}
	if cfg.Collectors.Web {
		rec = append(rec, "The websites you visit")
	}
	counts, err := v.countsFor(uid)
	if err != nil {
		return nil, err
	}
	return &SelfView{User: user, Date: date, Day: day, Hourly: hourly, Apps: apps, Web: []string{}, LiveVisible: true, Recorded: rec, Counts: counts}, nil
}

func (v *Viewer) countsFor(uid int) (map[string]int64, error) {
	out := map[string]int64{}
	for name, q := range map[string]string{
		"activity_intervals": `SELECT COUNT(*) FROM activity_intervals WHERE uid = ?`,
		"window_samples":     `SELECT COUNT(*) FROM window_samples WHERE uid = ?`,
		"hourly_usage":       `SELECT COUNT(*) FROM hourly_usage WHERE uid = ?`,
		"hourly_app_usage":   `SELECT COUNT(*) FROM hourly_app_usage WHERE uid = ?`,
		"app_usage":          `SELECT COUNT(*) FROM app_usage WHERE uid = ?`,
		"sessions":           `SELECT COUNT(*) FROM sessions WHERE uid = ?`,
	} {
		var n int64
		if err := v.DB.SQL().QueryRow(q, uid).Scan(&n); err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, nil
}

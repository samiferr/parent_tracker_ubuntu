package report

import (
	"bytes"
	"fmt"
	htmltpl "html/template"
	"strings"
	texttpl "text/template"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/view"
)

// Report is the content of one daily report, ready to render.
type Report struct {
	Date     string // YYYY-MM-DD
	DateLong string // "Monday 23 November 2026"
	Host     string
	Users    []UserReport
	System   []AlertRow // alerts not tied to one user (low disk, ...)
	Notes    []string   // service-wide remarks (recording paused, ...)
}

// UserReport is one monitored user's day.
type UserReport struct {
	Name           string
	NoData         bool
	ActiveMs       int64
	Active         string
	Idle           string
	Locked         string
	VsUsual        string // "42 m more than usual", empty without history
	First, Last    string // "08:12", empty without activity
	Apps           []Row
	Categories     []Row
	AppsAvailable  bool
	Hours          []HourCell
	Alerts         []AlertRow
	GapCount       int
	GapTotal       string
	FromHourlyOnly bool
}

// Row is one line of a ranked list.
type Row struct {
	Name     string
	Category string
	Time     string
	Pct      int // share of the largest row, for the bar width
}

// HourCell is one hour of the activity strip.
type HourCell struct {
	Label   string
	Minutes int64
	Color   string
	Tick    bool // show the hour label under the strip
}

// AlertRow is one alert of the day.
type AlertRow struct {
	Time     string
	Severity string
	Message  string
}

// Build collects the report for one local date from the database.
func Build(db *storage.DB, cfg *config.Config, loc *time.Location, now func() time.Time, date, host string) (*Report, error) {
	ds, de, err := aggregate.ParseDay(loc, date)
	if err != nil {
		return nil, fmt.Errorf("date must be YYYY-MM-DD: %w", err)
	}
	t, _ := time.ParseInLocation("2006-01-02", date, loc)
	rep := &Report{Date: date, DateLong: t.Format("Monday 2 January 2006"), Host: host}
	if cfg.DisclosureAcknowledged == "" {
		rep.Notes = append(rep.Notes, "Recording has not started: the disclosure notice has not been acknowledged (run sudo parent-tracker setup).")
	} else if !cfg.Collectors.Session {
		rep.Notes = append(rep.Notes, "The session collector is turned off, so no screen time is recorded.")
	}

	v := &view.Viewer{DB: db, Cfg: func() *config.Config { return cfg }, Loc: loc, Now: now}
	users, err := db.Users()
	if err != nil {
		return nil, err
	}
	alerts, err := db.Alerts(ds, de, 1000)
	if err != nil {
		return nil, err
	}
	clock := func(ms int64) string { return time.UnixMilli(ms).In(loc).Format("15:04") }
	// oldest first reads better in an e-mail
	for i, j := 0, len(alerts)-1; i < j; i, j = i+1, j-1 {
		alerts[i], alerts[j] = alerts[j], alerts[i]
	}
	for _, a := range alerts {
		if a.UID == 0 {
			rep.System = append(rep.System, AlertRow{clock(a.CreatedAt), a.Severity, a.Message})
		}
	}

	for _, u := range users {
		if !u.Monitored || !cfg.IsMonitored(u.Username) {
			continue
		}
		day, err := v.Day(u.UID, date)
		if err != nil {
			return nil, err
		}
		ur := UserReport{Name: u.Username, NoData: day.NoData, ActiveMs: day.ActiveMs, Active: view.FmtHM(day.ActiveMs),
			Idle: view.FmtHM(day.IdleMs), Locked: view.FmtHM(day.LockedMs), AppsAvailable: day.AppsAvailable,
			FromHourlyOnly: day.FromHourly}
		if day.FirstActivity > 0 {
			ur.First, ur.Last = clock(day.FirstActivity), clock(day.LastActivity)
		}
		if day.AvgDays > 0 {
			diff := day.ActiveMs - day.AvgActive7Ms
			switch {
			case diff >= 60_000:
				ur.VsUsual = view.FmtHM(diff) + " more than usual"
			case diff <= -60_000:
				ur.VsUsual = view.FmtHM(-diff) + " less than usual"
			default:
				ur.VsUsual = "about the same as usual"
			}
			ur.VsUsual += fmt.Sprintf(" (average %s over %d day%s)", view.FmtHM(day.AvgActive7Ms), day.AvgDays, plural(day.AvgDays))
		}
		var top int64
		for _, a := range day.TopApps {
			top = max(top, a.ForegroundMs)
		}
		for _, a := range day.TopApps {
			name := a.Name
			if name == "" {
				name = a.AppID
			}
			ur.Apps = append(ur.Apps, Row{Name: name, Category: a.Category, Time: view.FmtHM(a.ForegroundMs), Pct: pct(a.ForegroundMs, top)})
		}
		top = 0
		for _, c := range day.Categories {
			top = max(top, c.ForegroundMs)
		}
		for _, c := range day.Categories {
			ur.Categories = append(ur.Categories, Row{Name: c.Category, Time: view.FmtHM(c.ForegroundMs), Pct: pct(c.ForegroundMs, top)})
		}
		for _, hb := range day.Hours {
			m := hb.ActiveMs / 60_000
			ur.Hours = append(ur.Hours, HourCell{Label: fmt.Sprintf("%02d", hb.Hour), Minutes: m, Color: shade(m), Tick: hb.Hour%6 == 0})
		}
		if ur.GapCount = len(day.Gaps); ur.GapCount > 0 {
			ur.GapTotal = view.FmtHM(sumGaps(day.Gaps))
		}
		for _, a := range alerts {
			if a.UID == u.UID {
				ur.Alerts = append(ur.Alerts, AlertRow{clock(a.CreatedAt), a.Severity, a.Message})
			}
		}
		rep.Users = append(rep.Users, ur)
	}
	return rep, nil
}

func sumGaps(gs []view.Segment) int64 {
	var t int64
	for _, g := range gs {
		if g.End > g.Start {
			t += g.End - g.Start
		}
	}
	return t
}

func pct(v, top int64) int {
	if top <= 0 {
		return 0
	}
	p := int(v * 100 / top)
	return max(p, 2)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// shade maps active minutes in an hour to a colour, light to dark.
func shade(m int64) string {
	switch {
	case m <= 0:
		return "#eef1f5"
	case m < 10:
		return "#cfe0fb"
	case m < 20:
		return "#9cc0f5"
	case m < 35:
		return "#5f97ea"
	case m < 50:
		return "#2f6fd6"
	}
	return "#1a4fa8"
}

// Subject is the e-mail's subject line.
func (r *Report) Subject() string {
	t, _ := time.Parse("2006-01-02", r.Date)
	var parts []string
	for _, u := range r.Users {
		parts = append(parts, u.Name+" "+u.Active)
	}
	s := "Daily report, " + t.Format("Mon 2 Jan")
	if len(parts) > 0 {
		s += ": " + strings.Join(parts, ", ")
	}
	if r.Host != "" {
		s += " (" + r.Host + ")"
	}
	return s
}

// HTML renders the report as an HTML e-mail (inline styles, tables only, so it
// looks the same in every mail client).
func (r *Report) HTML() (string, error) {
	var b bytes.Buffer
	err := htmlTpl.Execute(&b, r)
	return b.String(), err
}

// Text renders the plain-text alternative.
func (r *Report) Text() (string, error) {
	var b bytes.Buffer
	err := textTpl.Execute(&b, r)
	return b.String(), err
}

var funcs = map[string]any{
	"width": func(p int) string { return fmt.Sprintf("%d%%", p) },
	"sevColor": func(s string) string {
		switch s {
		case "critical":
			return "#c9252d"
		case "warn":
			return "#b35c00"
		}
		return "#4b5563"
	},
}

var htmlTpl = htmltpl.Must(htmltpl.New("html").Funcs(funcs).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Daily report</title></head>
<body style="margin:0;padding:0;background:#f4f5f7;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1f2328;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f5f7;"><tr><td align="center" style="padding:24px 12px;">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%;background:#ffffff;border-radius:8px;">
<tr><td style="padding:24px 24px 8px 24px;">
  <div style="font-size:13px;color:#6b7280;">Parent Tracker{{if .Host}} · {{.Host}}{{end}}</div>
  <h1 style="margin:4px 0 0 0;font-size:22px;line-height:1.3;">Daily report</h1>
  <div style="font-size:15px;color:#4b5563;">{{.DateLong}}</div>
</td></tr>
{{range .Notes}}<tr><td style="padding:8px 24px;"><div style="background:#fff7e6;border-left:4px solid #b35c00;padding:10px 12px;font-size:14px;">{{.}}</div></td></tr>
{{end}}
{{if not .Users}}<tr><td style="padding:16px 24px;font-size:15px;">No users are monitored, so there is nothing to report.</td></tr>{{end}}
{{range .Users}}
<tr><td style="padding:20px 24px 4px 24px;border-top:1px solid #e5e7eb;">
  <h2 style="margin:0;font-size:18px;">{{.Name}}</h2>
</td></tr>
{{if .NoData}}<tr><td style="padding:4px 24px 16px 24px;font-size:15px;color:#4b5563;">Nothing was recorded on this day.</td></tr>{{else}}
<tr><td style="padding:8px 24px;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>
    <td style="padding:8px 0;width:34%;vertical-align:top;"><div style="font-size:12px;color:#6b7280;text-transform:uppercase;letter-spacing:.04em;">Active</div><div style="font-size:24px;font-weight:700;">{{.Active}}</div></td>
    <td style="padding:8px 0;width:33%;vertical-align:top;"><div style="font-size:12px;color:#6b7280;text-transform:uppercase;letter-spacing:.04em;">Idle</div><div style="font-size:18px;">{{.Idle}}</div></td>
    <td style="padding:8px 0;width:33%;vertical-align:top;"><div style="font-size:12px;color:#6b7280;text-transform:uppercase;letter-spacing:.04em;">Locked</div><div style="font-size:18px;">{{.Locked}}</div></td>
  </tr></table>
  {{if .VsUsual}}<div style="font-size:14px;color:#4b5563;">{{.VsUsual}}</div>{{end}}
  {{if .First}}<div style="font-size:14px;color:#4b5563;">First activity {{.First}}, last {{.Last}}.</div>{{end}}
</td></tr>
{{if .Hours}}<tr><td style="padding:8px 24px;">
  <div style="font-size:13px;font-weight:600;margin-bottom:6px;">Active minutes by hour</div>
  <table role="presentation" width="100%" cellpadding="0" cellspacing="1" style="table-layout:fixed;"><tr>
  {{range .Hours}}<td width="4%" title="{{.Label}}:00 — {{.Minutes}} min" style="width:4%;height:22px;background:{{.Color}};font-size:0;line-height:0;">&nbsp;</td>{{end}}
  </tr><tr>
  {{range .Hours}}<td width="4%" style="width:4%;font-size:10px;color:#6b7280;text-align:left;overflow:visible;white-space:nowrap;">{{if .Tick}}{{.Label}}{{end}}</td>{{end}}
  </tr></table>
</td></tr>{{end}}
<tr><td style="padding:8px 24px;">
  <div style="font-size:13px;font-weight:600;margin-bottom:6px;">Top applications</div>
  {{if .Apps}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0">
  {{range .Apps}}<tr>
    <td style="padding:3px 8px 3px 0;font-size:14px;width:40%;">{{.Name}}{{if .Category}} <span style="color:#6b7280;font-size:12px;">{{.Category}}</span>{{end}}</td>
    <td style="padding:3px 8px 3px 0;"><div style="background:#2f6fd6;height:8px;border-radius:4px;width:{{width .Pct}};"></div></td>
    <td style="padding:3px 0;font-size:14px;text-align:right;white-space:nowrap;width:70px;">{{.Time}}</td>
  </tr>{{end}}
  </table>{{else}}<div style="font-size:14px;color:#6b7280;">No application data for this day.</div>{{end}}
</td></tr>
{{if .Categories}}<tr><td style="padding:8px 24px;">
  <div style="font-size:13px;font-weight:600;margin-bottom:6px;">By category</div>
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
  {{range .Categories}}<tr>
    <td style="padding:3px 8px 3px 0;font-size:14px;width:40%;">{{.Name}}</td>
    <td style="padding:3px 8px 3px 0;"><div style="background:#7c5cc4;height:8px;border-radius:4px;width:{{width .Pct}};"></div></td>
    <td style="padding:3px 0;font-size:14px;text-align:right;white-space:nowrap;width:70px;">{{.Time}}</td>
  </tr>{{end}}
  </table>
</td></tr>{{end}}
{{end}}
{{if or .Alerts .GapCount}}<tr><td style="padding:8px 24px 16px 24px;">
  <div style="font-size:13px;font-weight:600;margin-bottom:6px;">Alerts and tracking gaps</div>
  {{range .Alerts}}<div style="font-size:14px;padding:2px 0;"><span style="color:{{sevColor .Severity}};font-weight:600;">{{.Time}}</span> {{.Message}}</div>{{end}}
  {{if .GapCount}}<div style="font-size:14px;padding:2px 0;color:#b35c00;">{{.GapCount}} tracking gap{{if gt .GapCount 1}}s{{end}}, {{.GapTotal}} in total: the service was not recording then.</div>{{end}}
</td></tr>{{end}}
{{end}}
{{if .System}}<tr><td style="padding:16px 24px;border-top:1px solid #e5e7eb;">
  <div style="font-size:13px;font-weight:600;margin-bottom:6px;">Service alerts</div>
  {{range .System}}<div style="font-size:14px;padding:2px 0;"><span style="color:{{sevColor .Severity}};font-weight:600;">{{.Time}}</span> {{.Message}}</div>{{end}}
</td></tr>{{end}}
<tr><td style="padding:16px 24px 24px 24px;border-top:1px solid #e5e7eb;font-size:12px;color:#6b7280;line-height:1.5;">
  A summary only: no screen content, keystrokes, messages or window titles. Open the dashboard on the PC for details.
  To change or stop this e-mail, use Settings &rarr; Daily report e-mail, or <code>sudo parent-tracker config set report.enabled false</code>.
</td></tr>
</table></td></tr></table>
</body></html>
`))

var textTpl = texttpl.Must(texttpl.New("text").Funcs(funcs).Parse(`Parent Tracker daily report{{if .Host}} ({{.Host}}){{end}}
{{.DateLong}}
{{range .Notes}}
! {{.}}
{{end}}{{if not .Users}}
No users are monitored, so there is nothing to report.
{{end}}{{range .Users}}
== {{.Name}} ==
{{if .NoData}}Nothing was recorded on this day.
{{else}}Active {{.Active}} · idle {{.Idle}} · locked {{.Locked}}
{{if .VsUsual}}{{.VsUsual}}
{{end}}{{if .First}}First activity {{.First}}, last {{.Last}}.
{{end}}
Top applications:
{{range .Apps}}  {{printf "%-8s" .Time}} {{.Name}}{{if .Category}} ({{.Category}}){{end}}
{{else}}  No application data for this day.
{{end}}{{if .Categories}}
By category:
{{range .Categories}}  {{printf "%-8s" .Time}} {{.Name}}
{{end}}{{end}}{{end}}{{if or .Alerts .GapCount}}
Alerts and tracking gaps:
{{range .Alerts}}  {{.Time}} {{.Message}}
{{end}}{{if .GapCount}}  {{.GapCount}} tracking gap(s), {{.GapTotal}} in total: the service was not recording then.
{{end}}{{end}}{{end}}{{if .System}}
Service alerts:
{{range .System}}  {{.Time}} {{.Message}}
{{end}}{{end}}
--
A summary only: no screen content, keystrokes, messages or window titles.
To stop this e-mail: sudo parent-tracker config set report.enabled false
`))

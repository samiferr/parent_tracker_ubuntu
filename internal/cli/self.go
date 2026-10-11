package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/view"
)

// cmdSelf shows a monitored person everything recorded about them. It needs no
// root: the service identifies the caller by the kernel and returns only that
// caller's own records (docs/USER-GUIDE §11).
func (a *App) cmdSelf(args []string) int {
	fs, cfgPath := a.newFlags("self")
	asJSON := fs.Bool("json", false, "print the raw data as JSON")
	userName := fs.String("user", "", "admin only: show a specific account")
	date := fs.String("date", "", "a specific day, YYYY-MM-DD (default: today)")
	if _, err := parse(fs, args); err != nil {
		return 2
	}
	socket := a.Socket
	if cfg, err := config.Load(*cfgPath); err == nil { // readable only by root
		socket = cfg.ControlSocket
	}
	q := url.Values{}
	if *userName != "" {
		q.Set("user", *userName)
	}
	if *date != "" {
		q.Set("date", *date)
	}
	resp, err := controlClient(socket).Get("http://control/self?" + q.Encode())
	if err != nil {
		return a.errorf("%v", socketErr(err, socket))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var e struct{ Error string }
		json.Unmarshal(body, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return a.errorf("%s", e.Error)
	}
	if *asJSON {
		a.Stdout.Write(body)
		return 0
	}
	var sv view.SelfView
	if err := json.Unmarshal(body, &sv); err != nil {
		return a.errorf("unexpected answer from the service")
	}
	a.renderSelf(&sv)
	return 0
}

func (a *App) renderSelf(sv *view.SelfView) {
	d := sv.Day
	hm := func(ms int64) string { return view.FmtHM(ms) }
	clock := func(ms int64) string {
		if ms == 0 {
			return "—"
		}
		return time.UnixMilli(ms).In(a.Loc).Format("15:04")
	}
	a.printf("Recorded activity for %s — %s\n\n", sv.User, sv.Date)
	a.printf("  Active time:      %s\n", hm(d.ActiveMs))
	a.printf("  Idle time:        %s\n", hm(d.IdleMs))
	a.printf("  Locked / off:     %s\n", hm(d.LockedMs))
	a.printf("  First activity:   %s\n", clock(d.FirstActivity))
	a.printf("  Last activity:    %s\n\n", clock(d.LastActivity))

	a.println("  By hour (" + dayWord(sv.Hourly.Date, a.today()) + ", active minutes):")
	any := false
	for _, b := range sv.Hourly.Buckets {
		if b.ActiveMs > 0 || b.IdleMs > 0 || b.LockedMs > 0 {
			a.printf("    %s   %s\n", b.Label, hm(b.ActiveMs))
			any = true
		}
	}
	if !any {
		a.println("    nothing recorded yet")
	}
	if sv.Hourly.GapMs > 0 {
		a.printf("    (%s of this day was not recorded because the service was not running)\n", hm(sv.Hourly.GapMs))
	}

	a.println("\n  Applications (last 7 days):")
	if len(sv.Apps.Apps) == 0 {
		a.println("    none recorded")
	}
	for i, ap := range sv.Apps.Apps {
		if i == 10 {
			break
		}
		name := ap.Name
		if name == "" {
			name = ap.AppID
		}
		a.printf("    %-26s %s\n", name, hm(ap.ForegroundMs))
	}
	a.println("\n  Websites (last 7 days):")
	if len(sv.Web) == 0 {
		a.println("    not recorded (browser history is off)")
	}

	if len(sv.Counts) > 0 {
		var keys []string
		for k := range sv.Counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", strings.ReplaceAll(k, "_", " "), sv.Counts[k]))
		}
		a.println("\n  Stored about you: " + strings.Join(parts, ", ") + ".")
	}
	a.println()
	a.println("Your parent can see your current state and the app in front, live, and your")
	a.println("usage hour by hour. It never sees your screen, keystrokes or messages.")
	a.println("This is everything Parent Tracker records about you. It stays on this computer.")
}

func dayWord(date, today string) string {
	if date == today {
		return "today"
	}
	return date
}

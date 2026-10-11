package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

const reloadHint = "Apply it with: sudo systemctl reload parent-trackerd"

func (a *App) cmdUsers(args []string) int {
	fs, cfgPath := a.newFlags("users")
	pos, err := parse(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) == 0 {
		return a.errorf("usage: parent-tracker users list|add|remove [NAME...]")
	}
	cfg, err := a.loadConfig(*cfgPath)
	if err != nil {
		return a.errorf("%v", err)
	}
	switch pos[0] {
	case "list":
		if len(cfg.MonitoredUsers) == 0 {
			a.println("No users are monitored. Add one with: sudo parent-tracker users add NAME")
			return 0
		}
		for _, u := range cfg.MonitoredUsers {
			if ac, ok := a.LookupUser(u); ok {
				a.printf("  %-20s uid %d\n", u, ac.UID)
			} else {
				a.printf("! %-20s no such account on this computer\n", u)
			}
		}
		return 0
	case "add":
		if len(pos) < 2 {
			return a.errorf("usage: parent-tracker users add NAME [NAME...]")
		}
		if !a.IsRoot() {
			return a.errorf("changing who is monitored needs root: use sudo")
		}
		for _, name := range pos[1:] {
			if _, ok := a.isOrdinary(name); !ok {
				return a.errorf("%q is not an ordinary user account on this computer (system accounts and root cannot be monitored)", name)
			}
			if !cfg.IsMonitored(name) {
				cfg.MonitoredUsers = append(cfg.MonitoredUsers, name)
			}
		}
		if err := cfg.Save(*cfgPath); err != nil {
			return a.errorf("%v", err)
		}
		a.println("Now monitoring:", strings.Join(cfg.MonitoredUsers, ", "))
		a.println(reloadHint)
		if cfg.DisclosureAcknowledged == "" {
			a.println("Nothing is recorded until you run `sudo parent-tracker setup` and acknowledge the disclosure.")
		}
		return 0
	case "remove":
		if len(pos) < 2 {
			return a.errorf("usage: parent-tracker users remove NAME")
		}
		if !a.IsRoot() {
			return a.errorf("changing who is monitored needs root: use sudo")
		}
		var keep []string
		for _, u := range cfg.MonitoredUsers {
			remove := false
			for _, n := range pos[1:] {
				remove = remove || u == n
			}
			if !remove {
				keep = append(keep, u)
			}
		}
		if len(keep) == len(cfg.MonitoredUsers) {
			return a.errorf("%s is not monitored", strings.Join(pos[1:], ", "))
		}
		cfg.MonitoredUsers = keep
		if err := cfg.Save(*cfgPath); err != nil {
			return a.errorf("%v", err)
		}
		a.println("Stopped monitoring", strings.Join(pos[1:], ", ")+". Existing data is kept; `parent-tracker delete` removes it.")
		a.println(reloadHint)
		return 0
	}
	return a.errorf("unknown users command %q (use list, add or remove)", pos[0])
}

var hashLine = regexp.MustCompile(`(?m)^(password_hash = ).*$`)

func (a *App) cmdConfig(args []string) int {
	fs, cfgPath := a.newFlags("config")
	pos, err := parse(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) == 0 {
		return a.errorf("usage: parent-tracker config list|get KEY|set KEY VALUE")
	}
	cfg, err := a.loadConfig(*cfgPath)
	if err != nil {
		return a.errorf("%v", err)
	}
	switch pos[0] {
	case "list":
		b, err := cfg.Marshal()
		if err != nil {
			return a.errorf("%v", err)
		}
		b = hashLine.ReplaceAll(b, []byte(`${1}"(set; not shown)"`))
		a.Stdout.Write(b)
		return 0
	case "get":
		if len(pos) != 2 {
			return a.errorf("usage: parent-tracker config get KEY")
		}
		v, err := cfg.Get(pos[1])
		if err != nil {
			return a.errorf("%v", err)
		}
		a.println(v)
		return 0
	case "set":
		if len(pos) != 3 {
			return a.errorf("usage: parent-tracker config set KEY VALUE")
		}
		if !a.IsRoot() {
			return a.errorf("changing settings needs root: use sudo")
		}
		if err := cfg.Set(pos[1], pos[2]); err != nil {
			return a.errorf("%v", err)
		}
		if err := cfg.Save(*cfgPath); err != nil {
			return a.errorf("%v", err)
		}
		a.printf("%s set.\n%s\n", pos[1], reloadHint)
		if pos[1] == "server.listen" {
			a.println("A new listen address needs a full restart: sudo systemctl restart parent-trackerd")
		}
		return 0
	}
	return a.errorf("unknown config command %q (use list, get or set)", pos[0])
}

func (a *App) cmdExport(args []string) int {
	fs, cfgPath := a.newFlags("export")
	userName := fs.String("user", "", "only this user")
	from := fs.String("from", "", "first day, YYYY-MM-DD")
	to := fs.String("to", "", "last day, YYYY-MM-DD")
	out := fs.String("out", "", "file to write (default: standard output)")
	if _, err := parse(fs, args); err != nil {
		return 2
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

	fromMs, toMs := int64(0), int64(1)<<60
	fromDay, toDay := "0000-00-00", "9999-12-31"
	if *from != "" {
		s, _, err := aggregate.ParseDay(a.Loc, *from)
		if err != nil {
			return a.errorf("--from must look like 2026-10-01")
		}
		fromMs, fromDay = s, *from
	}
	if *to != "" {
		_, e, err := aggregate.ParseDay(a.Loc, *to)
		if err != nil {
			return a.errorf("--to must look like 2026-10-07")
		}
		toMs, toDay = e, *to
	}
	var uids []int
	if *userName != "" {
		u, found, err := db.UserByName(*userName)
		if err != nil || !found {
			return a.errorf("%q is not a known user", *userName)
		}
		uids = []int{u.UID}
	}
	bundle, err := db.Export(uids, fromMs, toMs, fromDay, toDay, a.Version, a.Now())
	if err != nil {
		return a.errorf("%v", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(bundle); err != nil {
		return a.errorf("%v", err)
	}
	if *out == "" {
		a.Stdout.Write(buf.Bytes())
		return 0
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o600); err != nil {
		return a.errorf("%v", err)
	}
	_ = os.Chmod(*out, 0o600)
	a.printf("Wrote %s (%d bytes, mode 0600).\n", *out, buf.Len())
	a.println("This file holds everything recorded about the exported users. Keep it somewhere private.")
	return 0
}

func (a *App) cmdDelete(args []string) int {
	fs, cfgPath := a.newFlags("delete")
	userName := fs.String("user", "", "user whose data to delete (required)")
	before := fs.String("before", "", "delete everything recorded before this day, YYYY-MM-DD (required)")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if _, err := parse(fs, args); err != nil {
		return 2
	}
	if *userName == "" || *before == "" {
		return a.errorf("usage: parent-tracker delete --user NAME --before YYYY-MM-DD [--yes]")
	}
	cutoff, _, err := aggregate.ParseDay(a.Loc, *before)
	if err != nil {
		return a.errorf("--before must look like 2026-01-01")
	}
	if !a.IsRoot() {
		return a.errorf("deleting data needs root: use sudo")
	}
	cfg, err := a.loadConfig(*cfgPath)
	if err != nil {
		return a.errorf("%v", err)
	}
	db, err := storage.Open(cfg.DBPath())
	if err != nil {
		return a.errorf("%v", err)
	}
	defer db.Close()
	u, found, err := db.UserByName(*userName)
	if err != nil || !found {
		return a.errorf("%q is not a known user", *userName)
	}
	if !*yes {
		a.printf("This permanently deletes everything recorded about %s before %s: sessions, activity,\n", *userName, *before)
		a.println("app usage and the hour-by-hour history. It cannot be undone.")
		ans, err := a.ask(fmt.Sprintf("Type the user name (%s) to confirm: ", *userName))
		if err != nil || ans != *userName {
			return a.errorf("nothing was deleted")
		}
	}
	counts, err := db.DeleteUserBefore(u.UID, cutoff, *before)
	if err != nil {
		return a.errorf("%v", err)
	}
	var total int64
	for _, n := range counts {
		total += n
	}
	_ = db.Audit(a.Now().UnixMilli(), "admin", model.AuditDataDeleted, fmt.Sprintf("%s before %s: %d rows", *userName, *before, total))
	a.printf("Deleted %d rows about %s before %s (the database file was rewritten).\n", total, *userName, *before)
	for _, t := range []string{"activity_intervals", "hourly_usage", "hourly_app_usage", "app_usage", "window_samples", "sessions", "tracking_gaps", "alerts"} {
		if counts[t] > 0 {
			a.printf("  %-20s %d\n", t, counts[t])
		}
	}
	return 0
}

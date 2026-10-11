package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

const minPasswordLength = 8

func (a *App) cmdSetup(args []string) int {
	fs, cfgPath := a.newFlags("setup")
	var users stringList
	fs.Var(&users, "user", "user to monitor (repeatable)")
	dashUser := fs.String("dashboard-user", "", "dashboard user name (default: parent)")
	dashPass := fs.String("dashboard-password", "", "dashboard password (at least 8 characters)")
	channel := fs.String("alert-channel", "", "alert channel: none, desktop, ntfy or webhook")
	ntfy := fs.String("ntfy-url", "", "ntfy topic URL (with --alert-channel ntfy)")
	ack := fs.Bool("acknowledge-disclosure", false, "acknowledge the disclosure notice (required with --non-interactive the first time)")
	nonInteractive := fs.Bool("non-interactive", false, "take everything from flags and ask nothing")
	reset := fs.Bool("reset-config", false, "if the existing configuration cannot be read, keep it as a backup and start from the defaults")
	if _, err := parse(fs, args); err != nil {
		return 2
	}
	if !a.IsRoot() {
		return a.errorf("setup writes the configuration and must run as root: use `sudo parent-tracker setup`")
	}
	cfg, err := a.loadOrDefault(*cfgPath)
	if err != nil && *reset && !isPermissionOrUsage(err) {
		bak := fmt.Sprintf("%s.bak-%s", *cfgPath, a.Now().UTC().Format("20060102-150405"))
		if rerr := os.Rename(*cfgPath, bak); rerr != nil {
			return a.errorf("cannot move %s aside: %v", *cfgPath, rerr)
		}
		a.printf("The existing configuration could not be used (%s).\nIt was moved to %s and setup starts from the defaults.\n\n",
			firstLine(err.Error()), bak)
		cfg, err = a.newConfig(), nil
	}
	if err != nil {
		return a.errorf("%v", err)
	}
	alreadyAcked := cfg.DisclosureAcknowledged != ""
	selected := append([]string(nil), users...)

	if *nonInteractive {
		if !alreadyAcked && !*ack {
			return a.errorf("pass --acknowledge-disclosure to confirm you have read the notice (parent-tracker setup shows it)")
		}
		if *dashPass == "" && cfg.Server.PasswordHash == "" {
			return a.errorf("pass --dashboard-password: the dashboard has no password yet")
		}
	} else {
		if sel, rc := a.pickUsers(cfg.MonitoredUsers, selected); rc != 0 {
			return rc
		} else {
			selected = sel
		}
		if *dashPass == "" {
			pw, rc := a.askPassword(cfg.Server.PasswordHash != "")
			if rc != 0 {
				return rc
			}
			*dashPass = pw
		}
		if !alreadyAcked || !*ack {
			a.println()
			a.print(DisclosureText)
			if !alreadyAcked {
				ans, err := a.ask(`Type "yes" to confirm you have read this and will tell your child: `)
				if err != nil || strings.ToLower(ans) != "yes" {
					return a.errorf("setup stopped. Nothing is recorded until the notice is acknowledged")
				}
				*ack = true
			}
		}
	}

	// validate everything before touching the file
	for _, u := range selected {
		if _, ok := a.isOrdinary(u); !ok {
			return a.errorf("%q is not an ordinary user account on this computer (system accounts and root cannot be monitored)", u)
		}
	}
	if *dashPass != "" && len(*dashPass) < minPasswordLength {
		return a.errorf("the dashboard password must have at least %d characters", minPasswordLength)
	}
	if *dashUser != "" {
		cfg.Server.Username = *dashUser
	}
	if *dashPass != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(*dashPass), 12)
		if err != nil {
			return a.errorf("cannot hash the password: %v", err)
		}
		cfg.Server.PasswordHash = string(h)
	}
	for _, u := range selected {
		if !cfg.IsMonitored(u) {
			cfg.MonitoredUsers = append(cfg.MonitoredUsers, u)
		}
	}
	newlyAcked := !alreadyAcked && *ack
	if newlyAcked {
		cfg.DisclosureAcknowledged = a.Now().UTC().Format(time.RFC3339)
	}
	if *channel != "" {
		cfg.Alerts.Channel = *channel
		cfg.Alerts.Enabled = *channel != "none"
	}
	if *ntfy != "" {
		cfg.Alerts.NtfyURL = *ntfy
	}
	if err := cfg.Save(*cfgPath); err != nil {
		return a.errorf("%v", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return a.errorf("cannot create %s: %v", cfg.DataDir, err)
	}

	// record the setup in the audit log, and the acknowledgement as its own entry
	if db, err := storage.Open(cfg.DBPath()); err == nil {
		now := a.Now().UnixMilli()
		_ = db.Audit(now, "admin", model.AuditSetup, "monitored: "+strings.Join(cfg.MonitoredUsers, ", "))
		if newlyAcked {
			_ = db.Audit(now, "admin", model.AuditDisclosureAck, "")
		}
		for _, u := range selected {
			if ac, ok := a.LookupUser(u); ok {
				_ = db.UpsertUser(ac.UID, u, true, now)
			}
		}
		db.Close()
	} else {
		a.println("Note: the database could not be opened yet:", err)
	}

	for _, u := range selected {
		a.notifyChild(u)
	}

	a.println()
	a.println("Saved", *cfgPath)
	if len(cfg.MonitoredUsers) > 0 {
		a.println("Monitored users:", strings.Join(cfg.MonitoredUsers, ", "))
	}
	a.println()
	a.println("Next steps:")
	a.println("  sudo systemctl enable --now parent-trackerd")
	a.println("  parent-tracker doctor")
	a.printf("  open http://%s and sign in as %q\n", cfg.Server.Listen, cfg.Server.Username)
	a.println()
	a.println("If a monitored user signs in to a Wayland session, ask them to log out and back in")
	a.println("once so GNOME Shell loads the app-tracking extension.")
	return 0
}

// pickUsers shows the numbered picker of real accounts (docs/USER-GUIDE §4.1).
func (a *App) pickUsers(current, fromFlags []string) ([]string, int) {
	if len(fromFlags) > 0 {
		return fromFlags, 0
	}
	accts, err := a.ListAccounts()
	if err != nil {
		return nil, a.errorf("cannot list the user accounts: %v", err)
	}
	if len(accts) == 0 {
		a.println("There are no ordinary user accounts to monitor yet.")
		return nil, 0
	}
	a.println("Local user accounts found (system accounts excluded):")
	for i, ac := range accts {
		mark := ""
		for _, c := range current {
			if c == ac.Name {
				mark = "  (already monitored)"
			}
		}
		a.printf("  %2d) %-18s uid %-6d %s%s\n", i+1, ac.Name, ac.UID, ac.Home, mark)
	}
	a.println()
	line, err := a.ask("Select users to monitor by number (e.g. 1,3): ")
	if err != nil {
		return nil, a.errorf("%v", err)
	}
	if line == "" {
		return nil, 0
	}
	var picked []string
	for _, part := range strings.Split(line, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > len(accts) {
			return nil, a.errorf("%q is not a number from the list", strings.TrimSpace(part))
		}
		picked = append(picked, accts[n-1].Name)
	}
	return picked, 0
}

func (a *App) askPassword(haveOne bool) (string, int) {
	prompt := "Choose a dashboard password (at least 8 characters)"
	if haveOne {
		prompt += ", or press Enter to keep the current one"
	}
	a.printf("%s: ", prompt)
	read := func() (string, error) {
		if a.IsTerminal() {
			return a.ReadPassword()
		}
		line, err := a.reader().ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	pw, err := read()
	if err != nil {
		return "", a.errorf("%v", err)
	}
	if pw == "" && haveOne {
		return "", 0
	}
	if len(pw) < minPasswordLength {
		return "", a.errorf("the dashboard password must have at least %d characters", minPasswordLength)
	}
	a.print("Type it again: ")
	again, err := read()
	if err != nil || again != pw {
		return "", a.errorf("the two passwords do not match")
	}
	return pw, 0
}

func (a *App) print(s string) { fmt.Fprint(a.Stdout, s) }

// notifyChild shows a one-time notification to a monitored user who is logged
// in, so tracking is never hidden (SEC-2). It is best effort.
func (a *App) notifyChild(name string) {
	ac, ok := a.LookupUser(name)
	if !ok {
		return
	}
	bus := "/run/user/" + strconv.Itoa(ac.UID) + "/bus"
	if _, err := os.Stat(bus); err != nil {
		return // not logged in: the autostart notice will show at their next login
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = a.Exec(ctx, "runuser", "-u", name, "--", "env", "DBUS_SESSION_BUS_ADDRESS=unix:path="+bus,
		"notify-send", "--app-name=Parent Tracker", "Parent Tracker is recording", ChildNotice)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// isPermissionOrUsage reports errors that --reset-config must not paper over:
// a file that exists but cannot be read by this user is not a broken file.
func isPermissionOrUsage(err error) bool {
	return strings.Contains(err.Error(), "run this command with sudo")
}

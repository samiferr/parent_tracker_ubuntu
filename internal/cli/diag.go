package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agent"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/osrel"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
)

// serviceHealth asks the running service (root only).
func (a *App) serviceHealth(socket string) (*tracker.Health, error) {
	resp, err := controlClient(socket).Get("http://control/health")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("the service answered %s", resp.Status)
	}
	var out struct {
		Tracker *tracker.Health `json:"tracker"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Tracker, nil
}

func (a *App) cmdStatus(args []string) int {
	fs, cfgPath := a.newFlags("status")
	if _, err := parse(fs, args); err != nil {
		return 2
	}
	cfg, err := a.loadConfig(*cfgPath)
	if err != nil {
		return a.errorf("%v", err)
	}
	a.printf("Parent Tracker %s\n\n", a.Version)
	a.printf("Config:          %s\n", *cfgPath)
	a.printf("Database:        %s\n", cfg.DBPath())
	a.printf("Sampling:        every %s\n", cfg.SampleInterval.D())
	rec := "yes"
	if cfg.DisclosureAcknowledged == "" {
		rec = "no — the disclosure has not been acknowledged (run `sudo parent-tracker setup`)"
	} else if !cfg.Collectors.Session {
		rec = "no — collectors.session is off"
	}
	a.printf("Recording:       %s\n", rec)
	if len(cfg.MonitoredUsers) == 0 {
		a.println("Monitored users: none")
	} else {
		a.printf("Monitored users: %s\n", strings.Join(cfg.MonitoredUsers, ", "))
	}

	h, herr := a.serviceHealth(cfg.ControlSocket)
	a.println()
	if herr != nil {
		a.printf("Service:         not reachable (%v)\n", socketErr(herr, cfg.ControlSocket))
	} else {
		a.println("Service:         running")
		if h != nil {
			if h.LastError != "" {
				a.printf("  last problem:  %s\n", h.LastError)
			}
			for _, u := range h.Users {
				a.printf("  %-14s %s", u.User, u.State)
				if u.SessionID != "" {
					a.printf(", session %s, %s session, provider %s", u.SessionID, u.SessionType, u.Provider)
				}
				a.printf(", app tracking: %s\n", u.AppStatus)
				if u.Degraded != nil {
					a.printf("    %s %s\n", u.Degraded.Message, u.Degraded.NextStep)
				}
			}
		}
	}

	db, err := a.openRead(cfg)
	if err != nil {
		a.printf("\nStorage:         %v\n", err)
		return 0
	}
	defer db.Close()
	a.printf("\nSchema version:  %d\n", db.SchemaVersion())
	counts, err := db.Counts()
	if err == nil {
		a.println("Rows:")
		for _, t := range storage.Tables {
			a.printf("  %-20s %d\n", t, counts[t])
		}
	}
	if hs, err := db.CollectorHealths(); err == nil && len(hs) > 0 {
		a.println("Collector health:")
		for _, c := range hs {
			state := "ok"
			if c.LastError != "" {
				state = "DEGRADED: " + c.LastError
			}
			a.printf("  %-34s %s\n", c.Collector, state)
		}
	}
	return 0
}

// ---- doctor --------------------------------------------------------------

type finding struct {
	level, title, detail, fix string // level: ok, warn, FAIL, info
}

type doctor struct {
	a        *App
	findings []finding
}

func (d *doctor) add(level, title, detail, fix string) {
	d.findings = append(d.findings, finding{level, title, detail, fix})
}

func (a *App) cmdDoctor(args []string) int {
	fs, cfgPath := a.newFlags("doctor")
	if _, err := parse(fs, args); err != nil {
		return 2
	}
	d := &doctor{a: a}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	d.checkOS()
	d.checkSystemd(ctx)
	cfg := d.checkConfig(*cfgPath)
	var health *tracker.Health
	if cfg != nil {
		d.checkSettings(cfg)
		d.checkDatabase(cfg)
		health = d.checkService(cfg)
		d.checkSessions(ctx, cfg, health)
	}
	d.checkPackaging()

	fails := 0
	for _, f := range d.findings {
		a.printf("%-5s %-22s %s\n", f.level, f.title, f.detail)
		if f.fix != "" {
			a.printf("%-5s %-22s fix: %s\n", "", "", f.fix)
		}
		if f.level == "FAIL" {
			fails++
		}
	}
	a.println()
	if fails > 0 {
		a.printf("%d problem(s) need attention.\n", fails)
		return 1
	}
	a.println("No problems found.")
	return 0
}

func (d *doctor) checkOS() {
	in, err := osrel.Read(d.a.OSRelease)
	if err != nil {
		d.add("warn", "OS support", "Cannot read "+d.a.OSRelease+": "+err.Error(), "")
		return
	}
	switch v, msg := osrel.Judge(in); v {
	case osrel.Supported:
		d.add("ok", "OS support", msg, "")
	case osrel.BestEffort:
		d.add("warn", "OS support", msg, "")
	default:
		d.add("FAIL", "OS support", msg, "")
	}
}

var systemdVer = regexp.MustCompile(`systemd (\d+)`)

func (d *doctor) checkSystemd(ctx context.Context) {
	out, err := d.a.Exec(ctx, "systemctl", "--version")
	if err != nil {
		d.add("warn", "systemd", "Cannot run systemctl: "+err.Error(), "")
		return
	}
	m := systemdVer.FindStringSubmatch(string(out))
	if m == nil {
		d.add("warn", "systemd", "Cannot read the systemd version.", "")
		return
	}
	n, _ := strconv.Atoi(m[1])
	if n < 255 {
		d.add("FAIL", "systemd", fmt.Sprintf("systemd %d is older than the 255 that Ubuntu 24.04 ships.", n), "Upgrade Ubuntu to 24.04 or newer.")
		return
	}
	d.add("ok", "systemd", fmt.Sprintf("systemd %d.", n), "")
}

func (d *doctor) checkConfig(path string) *config.Config {
	cfg, err := config.Load(path)
	switch {
	case err == nil:
	case os.IsNotExist(err) || strings.Contains(err.Error(), "no such file"):
		d.add("FAIL", "Configuration", "There is no configuration at "+path+".", "Run `sudo parent-tracker setup`.")
		return nil
	case os.IsPermission(err):
		d.add("FAIL", "Configuration", "Cannot read "+path+" (it is readable by root only).", "Run `sudo parent-tracker doctor`.")
		return nil
	default:
		d.add("FAIL", "Configuration", err.Error(), "Fix the file, or run `parent-tracker config list` to see valid keys.")
		return nil
	}
	if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o007 != 0 {
		d.add("FAIL", "Configuration", fmt.Sprintf("%s is readable by everyone (mode %o).", path, st.Mode().Perm()), "sudo chmod 0640 "+path)
	} else {
		d.add("ok", "Configuration", "Valid, and not world-readable.", "")
	}
	return cfg
}

func (d *doctor) checkSettings(cfg *config.Config) {
	if cfg.DisclosureAcknowledged == "" {
		d.add("FAIL", "Disclosure", "The disclosure notice has not been acknowledged, so nothing is recorded.", "Run `sudo parent-tracker setup`.")
	} else {
		d.add("ok", "Disclosure", "Acknowledged on "+cfg.DisclosureAcknowledged+".", "")
	}
	if len(cfg.MonitoredUsers) == 0 {
		d.add("warn", "Monitored users", "No user is monitored.", "sudo parent-tracker users add NAME")
	}
	for _, u := range cfg.MonitoredUsers {
		if _, ok := d.a.LookupUser(u); !ok {
			d.add("warn", "Monitored users", u+" is on the list but has no account on this computer.", "sudo parent-tracker users remove "+u)
		}
	}
	if cfg.Server.Enabled {
		switch {
		case cfg.Server.PasswordHash == "":
			d.add("FAIL", "Dashboard", "The dashboard login is not set up.", "Run `sudo parent-tracker setup`.")
		case !config.IsLoopbackHost(hostPart(cfg.Server.Listen)):
			d.add("warn", "Dashboard", "Reachable beyond this PC on "+cfg.Server.Listen+".", "Use a strong password and restrict the port to your LAN.")
		default:
			d.add("ok", "Dashboard", "Listening on "+cfg.Server.Listen+" only, with a password.", "")
		}
	}
	for name, on := range map[string]bool{"collectors.screenshots": cfg.Collectors.Screenshots, "collectors.web": cfg.Collectors.Web} {
		if on {
			d.add("info", "Not available", name+" is on, but this release does not collect it.", "")
		}
	}
}

func hostPart(listen string) string {
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		return strings.Trim(listen[:i], "[]")
	}
	return listen
}

func (d *doctor) checkDatabase(cfg *config.Config) {
	st, err := os.Stat(cfg.DBPath())
	if err != nil {
		d.add("warn", "Database", "No database at "+cfg.DBPath()+" yet.", "sudo systemctl enable --now parent-trackerd")
		return
	}
	if st.Mode().Perm()&0o027 != 0 {
		d.add("FAIL", "Database", fmt.Sprintf("%s is too open (mode %o).", cfg.DBPath(), st.Mode().Perm()), "sudo chmod 0640 "+cfg.DBPath())
		return
	}
	db, err := storage.OpenReadOnly(cfg.DBPath())
	if err != nil {
		d.add("FAIL", "Database", err.Error(), "")
		return
	}
	defer db.Close()
	d.add("ok", "Database", fmt.Sprintf("Schema version %d, mode %o.", db.SchemaVersion(), st.Mode().Perm()), "")
}

func (d *doctor) checkService(cfg *config.Config) *tracker.Health {
	h, err := d.a.serviceHealth(cfg.ControlSocket)
	if err != nil {
		d.add("warn", "Service", "Not reachable: "+socketErr(err, cfg.ControlSocket).Error(), "sudo systemctl enable --now parent-trackerd")
		return nil
	}
	d.add("ok", "Service", "parent-trackerd is running.", "")
	return h
}

// checkSessions reports, per monitored session, its type and whether its
// foreground provider can work (FR-8.5, docs/07 §7.8).
func (d *doctor) checkSessions(ctx context.Context, cfg *config.Config, h *tracker.Health) {
	if d.a.Sessions == nil {
		return
	}
	sessions, err := d.a.Sessions(ctx)
	if err != nil {
		d.add("FAIL", "systemd-logind", "Cannot read sessions: "+err.Error(), "Check that systemd-logind is running.")
		return
	}
	d.add("ok", "systemd-logind", "Reachable.", "")
	x11 := d.x11SessionInstalled()
	if !x11 {
		d.add("info", "X11 sessions", "No X11 session is installed on this system. That is expected on Ubuntu 25.10 and later, not a problem.", "")
	}
	seen := false
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })
	for _, s := range sessions {
		if !collector.Eligible(s, cfg) {
			continue
		}
		seen = true
		p := collector.ProviderFor(s)
		title := "Session " + s.ID
		desc := fmt.Sprintf("%s: %s session on %q, provider %s.", s.User, s.Type, s.Desktop, p)
		switch p {
		case model.ProviderGnomeWayland:
			d.add("info", title, desc, "")
			d.checkExtension(s, h)
		case model.ProviderX11:
			d.add("info", title, desc, "")
			d.checkAgent(s, h)
		default:
			msg, next := tracker.Describe(map[bool]string{true: tracker.ReasonTTY, false: tracker.ReasonUnsupportedDesk}[s.Type == model.SessionTTY], s.User, s)
			d.add("warn", title, desc+" "+msg, next)
		}
	}
	if !seen && len(cfg.MonitoredUsers) > 0 {
		d.add("info", "Sessions", "No monitored user is signed in right now.", "")
	}
}

func (d *doctor) x11SessionInstalled() bool {
	files, _ := filepath.Glob(filepath.Join(d.a.Root, "/usr/share/xsessions/*.desktop"))
	return len(files) > 0
}

func (d *doctor) checkExtension(s collector.SessionInfo, h *tracker.Health) {
	ext := filepath.Join(d.a.Root, "/usr/share/gnome-shell/extensions", agent.ExtensionUUID, "metadata.json")
	if _, err := os.Stat(ext); err != nil {
		d.add("FAIL", "GNOME extension", "The app-tracking extension is not installed.", "Reinstall the parent-tracker package.")
		return
	}
	d.add("ok", "GNOME extension", "Installed system-wide.", "")
	if deg := degradedOf(h, s.User); deg != nil {
		d.add("warn", "App tracking", deg.Message, deg.NextStep)
	} else if h != nil {
		d.add("ok", "App tracking", "The extension is reporting for "+s.User+".", "")
	} else {
		d.add("info", "App tracking", "Start the service to check that the extension reports.", "")
	}
}

func (d *doctor) checkAgent(s collector.SessionInfo, h *tracker.Health) {
	if deg := degradedOf(h, s.User); deg != nil {
		d.add("warn", "App tracking", deg.Message, deg.NextStep)
	} else if h != nil {
		d.add("ok", "App tracking", "The X11 provider is reporting for "+s.User+".", "")
	} else {
		d.add("info", "App tracking", "Start the service to check that the agent reports.", "")
	}
}

func degradedOf(h *tracker.Health, user string) *struct{ Message, NextStep string } {
	if h == nil {
		return nil
	}
	for _, u := range h.Users {
		if u.User == user && u.Degraded != nil {
			return &struct{ Message, NextStep string }{u.Degraded.Message, u.Degraded.NextStep}
		}
	}
	return nil
}

func (d *doctor) checkPackaging() {
	unit := filepath.Join(d.a.Root, "/usr/lib/systemd/user/parent-tracker-agent.service")
	if _, err := fs.Stat(os.DirFS("/"), strings.TrimPrefix(unit, "/")); err != nil {
		d.add("info", "Agent unit", "The agent's systemd user unit is not installed at "+unit+" (normal when running from source).", "")
		return
	}
	d.add("ok", "Agent unit", "Installed.", "")
}

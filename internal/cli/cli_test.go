package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/samiferr/parent_tracker_ubuntu/internal/live"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/control"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/peercred"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
)

type rig struct {
	t        *testing.T
	app      *App
	out      *bytes.Buffer
	errb     *bytes.Buffer
	dir      string
	cfg      string
	root     atomic.Bool
	execs    [][]string
	osrel    string
	stdin    *bytes.Buffer
	now      time.Time
	sessions []collector.SessionInfo
}

var accounts = []Account{
	{"sami", 1000, "/home/sami", "/bin/bash"},
	{"maram", 1001, "/home/maram", "/bin/bash"},
	{"ilyes", 1002, "/home/ilyes", "/bin/bash"},
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, out: &bytes.Buffer{}, errb: &bytes.Buffer{}, stdin: &bytes.Buffer{}, dir: t.TempDir(),
		now: time.Date(2026, 10, 9, 21, 30, 0, 0, time.UTC)}
	r.cfg = filepath.Join(r.dir, "etc", "config.toml")
	r.root.Store(true)
	r.osrel = filepath.Join(r.dir, "os-release")
	os.WriteFile(r.osrel, []byte("ID=ubuntu\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04.5 LTS\"\n"), 0o644)
	r.app = &App{Stdin: r.stdin, Stdout: r.out, Stderr: r.errb, Version: "test", Now: func() time.Time { return r.now }, Loc: time.UTC,
		IsRoot: r.root.Load, IsTerminal: func() bool { return false }, OSRelease: r.osrel, ConfigPath: r.cfg,
		Socket: filepath.Join(r.dir, "none.sock"), Root: r.dir,
		ListAccounts: func() ([]Account, error) { return accounts, nil },
		LookupUser: func(n string) (Account, bool) {
			for _, a := range accounts {
				if a.Name == n {
					return a, true
				}
			}
			return Account{}, false
		},
		Exec: func(_ context.Context, name string, args ...string) ([]byte, error) {
			r.execs = append(r.execs, append([]string{name}, args...))
			if name == "systemctl" {
				return []byte("systemd 255 (255.4-1ubuntu8)\n"), nil
			}
			return nil, nil
		},
		Sessions: func(context.Context) ([]collector.SessionInfo, error) { return r.sessions, nil }}
	return r
}

func (r *rig) run(args ...string) int {
	r.t.Helper()
	r.out.Reset()
	r.errb.Reset()
	return Run(args, r.app)
}

func (r *rig) mustRun(args ...string) string {
	r.t.Helper()
	if rc := r.run(args...); rc != 0 {
		r.t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, rc, r.out, r.errb)
	}
	return r.out.String()
}

// configured writes a ready config (acknowledged, with a password) and returns it.
func (r *rig) configured(users ...string) *config.Config {
	r.t.Helper()
	c := config.Default()
	c.DataDir = filepath.Join(r.dir, "data")
	c.ControlSocket = filepath.Join(r.dir, "control.sock")
	c.MonitoredUsers = users
	c.DisclosureAcknowledged = "2026-10-01T00:00:00Z"
	h, _ := bcrypt.GenerateFromPassword([]byte("secret-pass"), bcrypt.MinCost)
	c.Server.PasswordHash = string(h)
	if err := c.Save(r.cfg); err != nil {
		r.t.Fatal(err)
	}
	return c
}

func (r *rig) db(c *config.Config) *storage.DB {
	r.t.Helper()
	db, err := storage.Open(c.DBPath())
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { db.Close() })
	return db
}

func TestUsageAndUnknownCommands(t *testing.T) {
	r := newRig(t)
	if rc := r.run(); rc != 2 || !strings.Contains(r.out.String(), "Usage: parent-tracker") {
		t.Fatalf("no args: %d %s", rc, r.out)
	}
	if rc := r.run("frobnicate"); rc != 2 || !strings.Contains(r.errb.String(), "unknown command") {
		t.Fatalf("%d %s", rc, r.errb)
	}
	if got := r.mustRun("version"); got != "parent-tracker test\n" {
		t.Fatalf("version = %q", got)
	}
	if got := r.mustRun("--version"); !strings.Contains(got, "test") {
		t.Fatalf("--version = %q", got)
	}
}

// AC-71: setup requires an explicit acknowledgement; scripted installs must pass the flag.
func TestSetupNonInteractive(t *testing.T) {
	r := newRig(t)
	if rc := r.run("setup", "--non-interactive", "--user", "ilyes", "--dashboard-password", "a-strong-password"); rc == 0 ||
		!strings.Contains(r.errb.String(), "--acknowledge-disclosure") {
		t.Fatalf("without the flag: %d %s", rc, r.errb)
	}
	if _, err := os.Stat(r.cfg); err == nil {
		t.Fatal("nothing may be written without an acknowledgement")
	}
	r.mustRun("setup", "--non-interactive", "--user", "ilyes", "--dashboard-user", "parent", "--dashboard-password", "a-strong-password",
		"--acknowledge-disclosure", "--alert-channel", "ntfy", "--ntfy-url", "https://ntfy.sh/topic")
	c, err := config.Load(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c.DisclosureAcknowledged == "" || len(c.MonitoredUsers) != 1 || c.MonitoredUsers[0] != "ilyes" || !c.Alerts.Enabled || c.Alerts.Channel != "ntfy" {
		t.Fatalf("config = %+v", c)
	}
	if bcrypt.CompareHashAndPassword([]byte(c.Server.PasswordHash), []byte("a-strong-password")) != nil {
		t.Fatal("the password must be stored as a bcrypt hash")
	}
	if strings.Contains(r.out.String(), "a-strong-password") {
		t.Fatal("the password must never be echoed")
	}
	if st, _ := os.Stat(r.cfg); st.Mode().Perm() != 0o640 {
		t.Fatalf("config mode = %v", st.Mode().Perm())
	}
	db := r.db(c)
	ev, _ := db.AuditEvents(0, 1<<60, 10)
	var ack, setup bool
	for _, e := range ev {
		ack = ack || e.Action == model.AuditDisclosureAck
		setup = setup || e.Action == model.AuditSetup
		if strings.Contains(e.Detail, "a-strong-password") {
			t.Fatal("password in the audit log")
		}
	}
	if !ack || !setup {
		t.Fatalf("audit = %+v", ev)
	}
	if us, _ := db.Users(); len(us) != 1 || us[0].UID != 1002 {
		t.Fatalf("users = %+v", us)
	}

	// resetting the password later needs neither --user nor --acknowledge-disclosure
	r.mustRun("setup", "--non-interactive", "--dashboard-user", "parent", "--dashboard-password", "another-password")
	c2, _ := config.Load(r.cfg)
	if bcrypt.CompareHashAndPassword([]byte(c2.Server.PasswordHash), []byte("another-password")) != nil || len(c2.MonitoredUsers) != 1 {
		t.Fatalf("password reset: %+v", c2.MonitoredUsers)
	}
	if c2.DisclosureAcknowledged != c.DisclosureAcknowledged {
		t.Fatal("the original acknowledgement time must be kept")
	}
}

func TestSetupValidation(t *testing.T) {
	r := newRig(t)
	if rc := r.run("setup", "--non-interactive", "--user", "root", "--dashboard-password", "long-enough-pw", "--acknowledge-disclosure"); rc == 0 ||
		!strings.Contains(r.errb.String(), "not an ordinary user account") {
		t.Fatalf("root accepted: %d %s", rc, r.errb)
	}
	if rc := r.run("setup", "--non-interactive", "--user", "ilyes", "--dashboard-password", "short", "--acknowledge-disclosure"); rc == 0 ||
		!strings.Contains(r.errb.String(), "at least 8") {
		t.Fatalf("short password accepted: %d %s", rc, r.errb)
	}
	r.root.Store(false)
	if rc := r.run("setup", "--non-interactive"); rc == 0 || !strings.Contains(r.errb.String(), "root") {
		t.Fatalf("setup without root: %d", rc)
	}
}

func TestSetupInteractive(t *testing.T) {
	r := newRig(t)
	r.stdin.WriteString("3\nhunter2-hunter2\nhunter2-hunter2\nyes\n")
	out := r.mustRun("setup")
	for _, want := range []string{"Local user accounts found", "ilyes", "uid 1002", "What is never recorded", "parent-tracker self", "Monitored users: ilyes", "systemctl enable --now parent-trackerd", "log out and back in"} {
		if !strings.Contains(out, want) {
			t.Errorf("setup output lacks %q", want)
		}
	}
	c, err := config.Load(r.cfg)
	if err != nil || c.DisclosureAcknowledged == "" || c.Server.PasswordHash == "" || c.MonitoredUsers[0] != "ilyes" {
		t.Fatalf("config = %+v, %v", c, err)
	}

	// declining the notice stops everything
	r2 := newRig(t)
	r2.stdin.WriteString("3\nhunter2-hunter2\nhunter2-hunter2\nno\n")
	if rc := r2.run("setup"); rc == 0 || !strings.Contains(r2.errb.String(), "Nothing is recorded until") {
		t.Fatalf("declined: %d %s", rc, r2.errb)
	}
	if _, err := os.Stat(r2.cfg); err == nil {
		t.Fatal("declining must not write a config")
	}
	// mismatching passwords
	r3 := newRig(t)
	r3.stdin.WriteString("3\nhunter2-hunter2\nsomething-else1\n")
	if rc := r3.run("setup"); rc == 0 || !strings.Contains(r3.errb.String(), "do not match") {
		t.Fatalf("mismatch: %d %s", rc, r3.errb)
	}
	// a bad number
	r4 := newRig(t)
	r4.stdin.WriteString("9\n")
	if rc := r4.run("setup"); rc == 0 || !strings.Contains(r4.errb.String(), "not a number from the list") {
		t.Fatalf("bad pick: %d %s", rc, r4.errb)
	}
}

func TestAccountsFilter(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	os.MkdirAll(home, 0o755)
	passwd := filepath.Join(dir, "passwd")
	os.WriteFile(passwd, []byte(strings.Join([]string{
		"root:x:0:0:root:/root:/bin/bash",
		"daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin",
		"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin",
		"sami:x:1000:1000:Sami:" + home + ":/bin/bash",
		"svc:x:1001:1001::" + home + ":/usr/sbin/nologin",
		"ghost:x:1002:1002::/no/such/home:/bin/bash",
		"high:x:61000:61000::" + home + ":/bin/bash",
		"# comment",
	}, "\n")), 0o644)
	defs := filepath.Join(dir, "login.defs")
	os.WriteFile(defs, []byte("# x\nUID_MIN 1000\nUID_MAX 60000\n"), 0o644)
	got, err := accountsFrom(passwd, defs)
	if err != nil || len(got) != 1 || got[0].Name != "sami" {
		t.Fatalf("accounts = %+v, %v", got, err)
	}
}

func TestUsersCommands(t *testing.T) {
	r := newRig(t)
	if rc := r.run("users", "list"); rc == 0 || !strings.Contains(r.errb.String(), "parent-tracker setup") {
		t.Fatalf("without config: %d %s", rc, r.errb)
	}
	r.configured()
	if out := r.mustRun("users", "list"); !strings.Contains(out, "No users are monitored") {
		t.Fatalf("%s", out)
	}
	out := r.mustRun("users", "add", "ilyes", "maram")
	if !strings.Contains(out, "ilyes, maram") || !strings.Contains(out, "systemctl reload") {
		t.Fatalf("%s", out)
	}
	if rc := r.run("users", "add", "root"); rc == 0 {
		t.Fatal("root must not be addable")
	}
	r.mustRun("users", "add", "ilyes") // already there: no duplicate
	c, _ := config.Load(r.cfg)
	if len(c.MonitoredUsers) != 2 {
		t.Fatalf("users = %v", c.MonitoredUsers)
	}
	c.MonitoredUsers = append(c.MonitoredUsers, "gone")
	c.Save(r.cfg)
	if out := r.mustRun("users", "list"); !strings.Contains(out, "! gone") || !strings.Contains(out, "uid 1002") {
		t.Fatalf("stale users must be marked with !: %s", out)
	}
	if out := r.mustRun("users", "remove", "maram"); !strings.Contains(out, "Existing data is kept") {
		t.Fatalf("%s", out)
	}
	if rc := r.run("users", "remove", "nobody-here"); rc == 0 {
		t.Fatal("removing an unlisted user should fail")
	}
	r.root.Store(false)
	if rc := r.run("users", "add", "maram"); rc == 0 || !strings.Contains(r.errb.String(), "root") {
		t.Fatal("adding users needs root")
	}
	// flags may follow the arguments
	r.root.Store(true)
	r.mustRun("users", "add", "maram", "--config", r.cfg)
}

func TestConfigCommands(t *testing.T) {
	r := newRig(t)
	r.configured("ilyes")
	if out := r.mustRun("config", "get", "sample_interval"); out != "5s\n" {
		t.Fatalf("%q", out)
	}
	out := r.mustRun("config", "set", "sample_interval", "10s")
	if !strings.Contains(out, "sample_interval set.") || !strings.Contains(out, "systemctl reload") {
		t.Fatalf("%s", out)
	}
	if out := r.mustRun("config", "set", "server.listen", "127.0.0.1:9000"); !strings.Contains(out, "full restart") {
		t.Fatalf("%s", out)
	}
	if rc := r.run("config", "set", "alerts.channel", "pigeon"); rc == 0 {
		t.Fatal("invalid value accepted")
	}
	if rc := r.run("config", "set", "nonsense.key", "1"); rc == 0 || !strings.Contains(r.errb.String(), "config list") {
		t.Fatalf("unknown key: %s", r.errb)
	}
	if rc := r.run("config", "set", "retention.web_visits", "100h"); rc == 0 || !strings.Contains(r.errb.String(), "edit the config file") {
		t.Fatalf("non-settable: %s", r.errb)
	}
	list := r.mustRun("config", "list")
	if !strings.Contains(list, `sample_interval = '10s'`) || strings.Contains(list, "$2a$") || !strings.Contains(list, "not shown") {
		t.Fatalf("list must show settings but hide the password hash:\n%s", list)
	}
	r.root.Store(false)
	if rc := r.run("config", "set", "log_level", "debug"); rc == 0 {
		t.Fatal("set needs root")
	}
	if out := r.mustRun("config", "get", "log_level"); out != "info\n" {
		t.Fatalf("%q", out)
	}
}

// seedData records an afternoon for ilyes on 2026-10-09 and returns the db.
func (r *rig) seedData(c *config.Config) *storage.DB {
	db := r.db(c)
	db.UpsertUser(1002, "ilyes", true, 1)
	at := func(h, m int) int64 { return time.Date(2026, 10, 9, h, m, 0, 0, time.UTC).UnixMilli() }
	iv := func(st model.State, a, b int64) {
		id, _ := db.OpenInterval(1002, st, a)
		db.CloseInterval(id, b)
	}
	iv(model.StateActive, at(16, 0), at(16, 42))
	iv(model.StateIdle, at(16, 42), at(16, 48))
	iv(model.StateLocked, at(16, 48), at(17, 0))
	iv(model.StateActive, at(17, 0), at(17, 51))
	iv(model.StateActive, at(21, 0), at(21, 18))
	var ss []model.Sample
	app := func(id string, a, b int64) {
		for ts := a; ts < b; ts += 5000 {
			ss = append(ss, model.Sample{UID: 1002, TS: ts, AppID: id, State: model.StateActive, Source: "x11"})
		}
	}
	app("firefox", at(16, 0), at(16, 30))
	app("code", at(16, 30), at(16, 42))
	app("firefox", at(17, 0), at(17, 35))
	app("code", at(17, 35), at(17, 51))
	app("code", at(21, 0), at(21, 18))
	db.InsertSamples(ss, map[string]string{"firefox": "Firefox Web Browser", "code": "Visual Studio Code"})
	return db
}

// AC-97: report --by hour matches the dashboard's numbers.
func TestReportByHourMatchesTheExample(t *testing.T) {
	r := newRig(t)
	c := r.configured("ilyes")
	r.seedData(c)
	out := r.mustRun("report", "--by", "hour", "--date", "2026-10-09")
	for _, want := range []string{"ilyes — 2026-10-09 (by hour, local time)", "16:00", "42m", "6m", "12m", "17:00", "51m", "Firefox Web Browser (30m)", "21:00", "18m", "total"} {
		if !strings.Contains(out, want) {
			t.Errorf("hourly report lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "18:00") || strings.Contains(out, "03:00") {
		t.Fatalf("hours without data must not be listed:\n%s", out)
	}
	if !strings.Contains(out, "1h 51m") { // 42m + 51m + 18m
		t.Fatalf("total missing:\n%s", out)
	}

	csvOut := r.mustRun("report", "--by", "hour", "--from", "2026-10-09", "--to", "2026-10-09", "--format", "csv")
	rows, err := csv.NewReader(strings.NewReader(csvOut)).ReadAll()
	if err != nil || len(rows) != 4 || rows[0][2] != "hour" {
		t.Fatalf("csv = %v, %v", rows, err)
	}
	if rows[1][2] != "16:00" || rows[1][4] != "42.0" || rows[1][5] != "6.0" || rows[1][6] != "12.0" || rows[1][8] != "Firefox Web Browser" {
		t.Fatalf("16:00 row = %v", rows[1])
	}
	var sum float64
	for _, row := range rows[1:] {
		var v float64
		fmt.Sscanf(row[4], "%f", &v)
		sum += v
	}
	if sum != 111 {
		t.Fatalf("active minutes in the CSV add to %v, want 111 (1h 51m)", sum)
	}
}

func TestReportByDay(t *testing.T) {
	r := newRig(t)
	c := r.configured("ilyes")
	r.seedData(c)
	out := r.mustRun("report", "--user", "ilyes", "--from", "2026-10-08", "--to", "2026-10-09")
	for _, want := range []string{"ilyes — 2026-10-08 to 2026-10-09", "2026-10-08", "no data", "2026-10-09", "1h 51m", "16:00", "21:18", "Firefox Web Browser (1h 05m)", "total"} {
		if !strings.Contains(out, want) {
			t.Errorf("daily report lacks %q:\n%s", want, out)
		}
	}
	csvOut := r.mustRun("report", "--from", "2026-10-09", "--to", "2026-10-09", "--format", "csv")
	rows, _ := csv.NewReader(strings.NewReader(csvOut)).ReadAll()
	if len(rows) != 2 || rows[1][2] != "111.0" || rows[1][5] != "16:00" || rows[1][7] != "Firefox Web Browser" {
		t.Fatalf("csv = %v", rows)
	}
	if rc := r.run("report", "--user", "stranger"); rc == 0 || !strings.Contains(r.errb.String(), "not a monitored user") {
		t.Fatalf("unknown user: %s", r.errb)
	}
	if rc := r.run("report", "--format", "pdf"); rc == 0 {
		t.Fatal("pdf is not supported yet")
	}
}

func TestReportNeedsAUserWhenSeveralAreMonitored(t *testing.T) {
	r := newRig(t)
	c := r.configured("ilyes", "maram")
	db := r.db(c)
	db.UpsertUser(1002, "ilyes", true, 1)
	db.UpsertUser(1001, "maram", true, 1)
	if rc := r.run("report"); rc == 0 || !strings.Contains(r.errb.String(), "choose a user with --user (ilyes, maram)") {
		t.Fatalf("%d %s", rc, r.errb)
	}
}

func TestExport(t *testing.T) {
	r := newRig(t)
	c := r.configured("ilyes")
	r.seedData(c)
	out := filepath.Join(r.dir, "backup.json")
	msg := r.mustRun("export", "--out", out)
	if !strings.Contains(msg, "mode 0600") {
		t.Fatalf("%s", msg)
	}
	st, _ := os.Stat(out)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("export mode = %v (SEC-24)", st.Mode().Perm())
	}
	var b storage.Bundle
	raw, _ := os.ReadFile(out)
	if err := json.Unmarshal(raw, &b); err != nil || b.Manifest.Checksum == "" || b.Manifest.Counts["activity_intervals"] != 5 || len(b.Datasets.Samples) == 0 {
		t.Fatalf("bundle = %+v, %v", b.Manifest, err)
	}
	stdout := r.mustRun("export", "--user", "ilyes", "--from", "2026-10-09", "--to", "2026-10-09")
	if !strings.Contains(stdout, `"hourly_usage"`) || !strings.Contains(stdout, `"checksum_sha256"`) {
		t.Fatalf("stdout export lacks datasets")
	}
	if rc := r.run("export", "--from", "yesterday"); rc == 0 {
		t.Fatal("bad date accepted")
	}
}

func TestDeleteAsksForConfirmation(t *testing.T) {
	r := newRig(t)
	c := r.configured("ilyes")
	r.seedData(c)
	r.stdin.WriteString("wrong\n")
	if rc := r.run("delete", "--user", "ilyes", "--before", "2026-10-10"); rc == 0 || !strings.Contains(r.errb.String(), "nothing was deleted") {
		t.Fatalf("%d %s", rc, r.errb)
	}
	db := r.db(c)
	if ivs, _ := db.Intervals(1002, 0, 1<<60); len(ivs) != 5 {
		t.Fatal("a refused delete must delete nothing")
	}
	r.stdin.Reset()
	r.stdin.WriteString("ilyes\n")
	out := r.mustRun("delete", "--user", "ilyes", "--before", "2026-10-10")
	if !strings.Contains(out, "Deleted") || !strings.Contains(out, "activity_intervals") {
		t.Fatalf("%s", out)
	}
	if ivs, _ := db.Intervals(1002, 0, 1<<60); len(ivs) != 0 {
		t.Fatal("data remains after delete (FR-10.4)")
	}
	ev, _ := db.AuditEvents(0, 1<<60, 10)
	if len(ev) == 0 || ev[0].Action != model.AuditDataDeleted {
		t.Fatalf("a delete must be audited: %+v", ev)
	}
	// --yes skips the prompt; a missing user or date is an error
	r.mustRun("delete", "--user", "ilyes", "--before", "2026-01-01", "--yes")
	if rc := r.run("delete", "--user", "ilyes"); rc == 0 {
		t.Fatal("--before is required")
	}
	if rc := r.run("delete", "--user", "ghost", "--before", "2026-01-01", "--yes"); rc == 0 {
		t.Fatal("unknown user")
	}
}

func TestDoctorOSVerdicts(t *testing.T) {
	r := newRig(t)
	r.configured("ilyes")
	// AC-64: supported on 24.04
	rc := r.run("doctor")
	out := r.out.String()
	if !strings.Contains(out, "ok    OS support") || !strings.Contains(out, "Ubuntu 24.04.5 LTS is supported") {
		t.Fatalf("doctor on 24.04:\n%s", out)
	}
	_ = rc
	// AC-64: "unsupported OS" and a non-zero exit on 22.04
	os.WriteFile(r.osrel, []byte("ID=ubuntu\nVERSION_ID=\"22.04\"\nPRETTY_NAME=\"Ubuntu 22.04.4 LTS\"\n"), 0o644)
	if rc := r.run("doctor"); rc == 0 {
		t.Fatal("doctor must exit non-zero on an unsupported OS")
	}
	out = r.out.String()
	if !strings.Contains(out, "FAIL  OS support") || !strings.Contains(out, "Unsupported OS") || !strings.Contains(out, "do-release-upgrade") {
		t.Fatalf("doctor on 22.04:\n%s", out)
	}
}

func TestDoctorConfigChecks(t *testing.T) {
	r := newRig(t)
	if rc := r.run("doctor"); rc == 0 || !strings.Contains(r.out.String(), "There is no configuration") {
		t.Fatalf("no config:\n%s", r.out)
	}
	c := r.configured("ilyes", "ghost")
	c.DisclosureAcknowledged = ""
	c.Server.PasswordHash = ""
	os.WriteFile(r.cfg, mustMarshal(t, c), 0o640)
	os.Chmod(r.cfg, 0o644) // world-readable
	rc := r.run("doctor")
	out := r.out.String()
	for _, want := range []string{"readable by everyone", "chmod 0640", "not been acknowledged", "dashboard login is not set up", "ghost is on the list"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor lacks %q:\n%s", want, out)
		}
	}
	if rc == 0 {
		t.Fatal("exit status should be non-zero")
	}
}

func mustMarshal(t *testing.T, c *config.Config) []byte {
	b, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// FR-8.5, FR-3.11, NFR-3.10: sessions are reported with their type and provider;
// a missing X11 session is expected, not a problem.
func TestDoctorSessions(t *testing.T) {
	r := newRig(t)
	r.configured("ilyes")
	os.MkdirAll(filepath.Join(r.dir, "usr/share/gnome-shell/extensions/parent-tracker@samiferr.github.io"), 0o755)
	os.WriteFile(filepath.Join(r.dir, "usr/share/gnome-shell/extensions/parent-tracker@samiferr.github.io/metadata.json"), []byte("{}"), 0o644)
	r.sessions = []collector.SessionInfo{collector.UserSession("2", 1002, "ilyes", "wayland", "ubuntu")}

	r.run("doctor")
	out := r.out.String()
	for _, want := range []string{"Session 2", "wayland session", "provider gnome-wayland", "ok    GNOME extension", "No X11 session is installed", "expected"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "FAIL  X11") {
		t.Fatal("a missing X11 session is not a failure")
	}

	// extension not installed on a Wayland session
	os.RemoveAll(filepath.Join(r.dir, "usr"))
	r.run("doctor")
	if out := r.out.String(); !strings.Contains(out, "FAIL  GNOME extension") {
		t.Fatalf("missing extension must be reported:\n%s", out)
	}

	// a tty session: explained, screen time still recorded
	r.sessions = []collector.SessionInfo{collector.UserSession("5", 1002, "ilyes", "tty", "")}
	r.run("doctor")
	if out := r.out.String(); !strings.Contains(out, "text console session") || !strings.Contains(out, "Screen time is still recorded") {
		t.Fatalf("tty:\n%s", out)
	}
}

// startControl serves the control socket for the self-view tests.
func (r *rig) startControl(c *config.Config, uid *atomic.Int64) {
	r.t.Helper()
	db := r.seedData(c)
	srv := &control.Server{DB: db, Cfg: func() *config.Config { return c }, Loc: time.UTC, Now: func() time.Time { return r.now },
		Health: func() tracker.Health {
			return tracker.Health{Recording: true, Users: []tracker.UserHealth{{User: "ilyes", UID: 1002, State: "active", SessionID: "2",
				SessionType: "wayland", Provider: "gnome-wayland", AppStatus: "degraded",
				Degraded: &liveDegraded}}}
		},
		LookupUID: func(u int) (string, error) {
			for _, a := range accounts {
				if a.UID == u {
					return a.Name, nil
				}
			}
			if u == 0 {
				return "root", nil
			}
			return "", fmt.Errorf("unknown")
		},
		Cred: func(net.Conn) (peercred.Cred, error) { return peercred.Cred{UID: int(uid.Load())}, nil }}
	sock := filepath.Join(r.dir, "control.sock")
	ln, err := peercred.Listen(sock)
	if err != nil {
		r.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.t.Cleanup(cancel)
	go srv.Serve(ctx, ln)
	r.app.Socket = sock
}

func TestSelfShowsTheChildTheirOwnData(t *testing.T) {
	r := newRig(t)
	c := r.configured("ilyes")
	var uid atomic.Int64
	uid.Store(1002)
	r.startControl(c, &uid)
	r.app.ConfigPath = filepath.Join(r.dir, "unreadable.toml") // a normal user cannot read the config

	out := r.mustRun("self", "--date", "2026-10-09")
	for _, want := range []string{
		"Recorded activity for ilyes — 2026-10-09", "Active time:      1h 51m", "Idle time:        6m", "Locked / off:     12m",
		"First activity:   16:00", "Last activity:    21:18", "By hour (today, active minutes):", "16:00   42m", "17:00   51m", "21:00   18m",
		"Applications (last 7 days):", "Firefox Web Browser", "Visual Studio Code", "Websites (last 7 days):",
		"Your parent can see your current state and the app in front, live",
		"This is everything Parent Tracker records about you. It stays on this computer.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("self output lacks %q:\n%s", want, out)
		}
	}
	// the same numbers as the report the parent runs (AC-97)
	r.app.ConfigPath = r.cfg
	rep := r.mustRun("report", "--by", "hour", "--date", "2026-10-09")
	for _, want := range []string{"42m", "51m", "18m", "1h 51m"} {
		if !strings.Contains(rep, want) {
			t.Fatalf("the parent's hourly report differs from the child's view: lacks %q", want)
		}
	}
	// JSON
	r.app.ConfigPath = filepath.Join(r.dir, "unreadable.toml")
	var sv struct {
		User   string
		Hourly struct {
			ActiveMs int64 `json:"active_ms"`
		}
	}
	if err := json.Unmarshal([]byte(r.mustRun("self", "--json", "--date", "2026-10-09")), &sv); err != nil || sv.User != "ilyes" || sv.Hourly.ActiveMs != 111*60000 {
		t.Fatalf("json = %+v, %v", sv, err)
	}
	// a child cannot ask for someone else
	if rc := r.run("self", "--user", "maram"); rc == 0 || !strings.Contains(r.errb.String(), "only see your own") {
		t.Fatalf("%d %s", rc, r.errb)
	}
	// an unmonitored account is told so
	uid.Store(1000)
	if rc := r.run("self"); rc == 0 || !strings.Contains(r.errb.String(), "not recording anything about this account") {
		t.Fatalf("%d %s", rc, r.errb)
	}
}

func TestSelfWhenTheServiceIsDown(t *testing.T) {
	r := newRig(t)
	r.app.ConfigPath = filepath.Join(r.dir, "unreadable.toml")
	if rc := r.run("self"); rc == 0 || !strings.Contains(r.errb.String(), "service is not running") {
		t.Fatalf("%d %s", rc, r.errb)
	}
}

func TestStatusShowsSessionsAndHealth(t *testing.T) {
	r := newRig(t)
	c := r.configured("ilyes")
	var uid atomic.Int64
	uid.Store(0)
	r.startControl(c, &uid)
	out := r.mustRun("status")
	for _, want := range []string{"Recording:       yes", "Monitored users: ilyes", "Service:         running", "session 2, wayland session, provider gnome-wayland",
		"app tracking: degraded", "App tracking limited in a Wayland session", "Schema version:  2", "activity_intervals"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	// without the disclosure
	c.DisclosureAcknowledged = ""
	c.Save(r.cfg)
	if out := r.mustRun("status"); !strings.Contains(out, "the disclosure has not been acknowledged") {
		t.Fatalf("%s", out)
	}
}

func TestNotice(t *testing.T) {
	r := newRig(t)
	if out := r.mustRun("notice"); !strings.Contains(out, "parent-tracker self") || strings.Contains(out, "!") {
		t.Fatalf("%q", out)
	}
	// --notify is silent for an account that is not monitored / when nothing runs
	r.execs = nil
	r.mustRun("notice", "--notify")
	if len(r.execs) != 0 {
		t.Fatalf("a notification was sent although nothing is recorded: %v", r.execs)
	}
	// and shows it for a monitored account
	c := r.configured("ilyes")
	var uid atomic.Int64
	uid.Store(1002)
	r.startControl(c, &uid)
	r.mustRun("notice", "--notify")
	if len(r.execs) != 1 || r.execs[0][0] != "notify-send" {
		t.Fatalf("execs = %v", r.execs)
	}
}

func TestCompletion(t *testing.T) {
	r := newRig(t)
	for _, sh := range []string{"bash", "zsh", "fish"} {
		out := r.mustRun("completion", sh)
		if !strings.Contains(out, "doctor") || !strings.Contains(out, "report") {
			t.Errorf("%s completion lacks commands", sh)
		}
	}
	if rc := r.run("completion", "tcsh"); rc == 0 {
		t.Fatal("unknown shell accepted")
	}
}

// DS-5 / AC-115 for the CLI: factual, no exclamation marks, no emoji.
func TestOutputFollowsTheCopyRules(t *testing.T) {
	r := newRig(t)
	for _, text := range []string{DisclosureText, ChildNotice} {
		if strings.Contains(text, "!") {
			t.Errorf("exclamation mark in: %s", text)
		}
		for _, ch := range text {
			if ch > 0x2FFF {
				t.Errorf("non-text symbol %q in: %s", ch, text)
			}
		}
	}
	_ = r
}

var liveDegraded = live.Degraded{Collector: "foreground", Session: "2", SessionType: "wayland", Provider: "gnome-wayland",
	Reason: "extension_missing", Message: "App tracking limited in a Wayland session. The GNOME Shell extension is not enabled for ilyes.",
	NextStep: "Ask ilyes to log out and back in, then run `parent-tracker doctor`."}

func TestUnimplementedCollectorsAreSaidSo(t *testing.T) {
	r := newRig(t)
	r.configured("ilyes")
	out := r.mustRun("config", "set", "collectors.web", "true")
	if !strings.Contains(out, "does not collect that yet") {
		t.Fatalf("turning on web history must say it records nothing:\n%s", out)
	}
	if out := r.mustRun("config", "set", "collectors.web", "false"); strings.Contains(out, "does not collect") {
		t.Fatalf("turning it off needs no warning:\n%s", out)
	}
	if out := r.mustRun("config", "set", "collectors.window_title", "true"); strings.Contains(out, "does not collect") {
		t.Fatalf("window titles are implemented:\n%s", out)
	}
	r.mustRun("config", "set", "collectors.screenshots", "true")
	r.run("doctor")
	if got := r.out.String(); !strings.Contains(got, "collectors.screenshots is on, but this release does not collect it") {
		t.Fatalf("doctor should flag it:\n%s", got)
	}
}

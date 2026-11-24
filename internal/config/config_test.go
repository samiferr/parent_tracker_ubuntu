package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultsAreValidAndSafe(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Collectors.WindowTitle || c.Collectors.Web || c.Collectors.WebFullURL || c.Collectors.Screenshots {
		t.Fatal("sensitive collectors must default to off (AC-70)")
	}
	if c.Server.Listen != "127.0.0.1:8787" || c.Server.AllowLAN {
		t.Fatal("dashboard must default to loopback")
	}
	if c.Recording() {
		t.Fatal("nothing may be recorded before the disclosure is acknowledged (AC-71)")
	}
	if got := c.Retention.HourlyUsage.D(); got != 8760*time.Hour {
		t.Fatalf("hourly retention = %v, want 1 year", got)
	}
	if len(c.MonitoredUsers) != 0 {
		t.Fatal("the allowlist must start empty")
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	_, err := Parse([]byte("sample_intervall = \"5s\"\n"))
	if err == nil {
		t.Fatal("unknown keys must be rejected")
	}
}

func TestParseOverridesDefaults(t *testing.T) {
	c, err := Parse([]byte(`
monitored_users = ["ilyes"]
sample_interval = "10s"
disclosure_acknowledged = "2026-10-11T10:00:00Z"
[collectors]
window_title = true
session = true
app = true
[[categories]]
name = "school"
patterns = ["code"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.SampleInterval.D() != 10*time.Second || !c.Collectors.WindowTitle || !c.Recording() {
		t.Fatalf("overrides not applied: %+v", c)
	}
	// A [[categories]] section replaces the defaults entirely.
	if got := c.CategoryFor("steam"); got != "other" {
		t.Fatalf("steam = %q, want other once categories are customised", got)
	}
	if got := c.CategoryFor("code"); got != "school" {
		t.Fatalf("code = %q, want school", got)
	}
	// Untouched sections keep their defaults.
	if c.Retention.WindowSamples.D() != 720*time.Hour {
		t.Fatal("retention default lost")
	}
}

func TestValidateErrors(t *testing.T) {
	cases := map[string]func(*Config){
		"sample_interval":         func(c *Config) { c.SampleInterval = Duration(100 * time.Millisecond) },
		"loopback":                func(c *Config) { c.Server.Listen = "0.0.0.0:8787" },
		"window_samples":          func(c *Config) { c.Retention.WindowSamples = Duration(24 * time.Hour) },
		"web_full_url":            func(c *Config) { c.Collectors.WebFullURL = true },
		"alerts.channel":          func(c *Config) { c.Alerts.Channel = "carrier-pigeon" },
		"alerts.bedtime":          func(c *Config) { c.Alerts.Bedtime = "9pm" },
		"live_max_clients":        func(c *Config) { c.Server.LiveMaxClients = 0 },
		"monitored_users":         func(c *Config) { c.MonitoredUsers = []string{"a", "a"} },
		"disclosure_acknowledged": func(c *Config) { c.DisclosureAcknowledged = "yesterday" },
	}
	for name, mutate := range cases {
		c := Default()
		mutate(c)
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestLANRequiresAllowLAN(t *testing.T) {
	c := Default()
	c.Server.Listen = "0.0.0.0:8787"
	c.Server.AllowLAN = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSetAndGet(t *testing.T) {
	c := Default()
	if err := c.Set("sample_interval", "10s"); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.Get("sample_interval"); v != "10s" {
		t.Fatalf("got %q", v)
	}
	if err := c.Set("alerts.daily_active_limit", "3h"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("server.live", "false"); err != nil {
		t.Fatal(err)
	}
	if c.Server.Live {
		t.Fatal("server.live not applied")
	}

	before := c.Clone()
	if err := c.Set("sample_interval", "forever"); err == nil {
		t.Fatal("bad duration accepted")
	}
	if err := c.Set("alerts.channel", "pigeon"); err == nil {
		t.Fatal("invalid channel accepted")
	}
	if err := c.Set("server.password_hash", "x"); err == nil {
		t.Fatal("unknown key accepted")
	}
	if err := c.Set("retention.web_visits", "100h"); err == nil || !strings.Contains(err.Error(), "edit the config file") {
		t.Fatalf("non-settable key: %v", err)
	}
	if c.SampleInterval != before.SampleInterval || c.Alerts.Channel != before.Alerts.Channel {
		t.Fatal("a failed Set must not change the config")
	}
}

func TestSettableKeysMatchTheUserGuide(t *testing.T) {
	want := []string{
		"log_level", "sample_interval", "web_import_interval", "control_socket", "server.enabled", "server.listen",
		"server.allow_lan", "server.live", "server.live_max_clients", "collectors.session", "collectors.app",
		"collectors.window_title", "collectors.web", "collectors.web_full_url", "collectors.screenshots",
		"alerts.enabled", "alerts.channel", "alerts.ntfy_url", "alerts.webhook_url", "alerts.daily_active_limit",
		"alerts.bedtime", "retention.window_samples", "retention.intervals", "retention.app_usage",
		"retention.hourly_usage", "retention.prune_interval", "report.enabled", "report.send_at",
		"report.resend_api_key", "report.from", "report.recipients",
	}
	got := map[string]bool{}
	for _, k := range Settable() {
		got[k] = true
	}
	for _, k := range want {
		if !got[k] {
			t.Errorf("%s should be settable", k)
		}
	}
	if len(got) != len(want) {
		t.Errorf("settable keys = %v", Settable())
	}
}

func TestSaveRoundTripAndMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "etc", "config.toml")
	c := Default()
	c.MonitoredUsers = []string{"ilyes"}
	c.DisclosureAcknowledged = "2026-10-11T10:00:00Z"
	c.Server.PasswordHash = "$2a$10$abcdefghijklmnopqrstuv"
	c.Alerts.Bedtime = "21:00"
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640 (FR-1.4)", st.Mode().Perm())
	}
	back, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if back.Server.PasswordHash != c.Server.PasswordHash || back.Alerts.Bedtime != "21:00" ||
		back.SampleInterval != c.SampleInterval || len(back.MonitoredUsers) != 1 {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestCategoryFor(t *testing.T) {
	c := Default()
	for id, want := range map[string]string{
		"steam": "games", "Minecraft": "games", "libreoffice-writer": "school",
		"firefox_firefox": "browser", "discord": "social", "vlc": "video", "gedit": "other",
	} {
		if got := c.CategoryFor(id); got != want {
			t.Errorf("CategoryFor(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestUnknownKeysAreNamed(t *testing.T) {
	_, err := Parse([]byte("monitored_users = []\nmystery = 1\n[web]\nenabled = true\n"))
	if err == nil {
		t.Fatal("unknown keys must be rejected")
	}
	for _, want := range []string{`line 2: "mystery"`, `"web"`, "not a setting this version knows"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestReportSettings(t *testing.T) {
	c := Default()
	if c.Report.Enabled || c.Report.SendAt != "07:00" || len(c.Report.Recipients) != 0 {
		t.Fatalf("report defaults = %+v", c.Report)
	}
	if p := c.ReportProblems(); len(p) != 3 {
		t.Fatalf("problems = %v", p)
	}
	// enabling before the rest is filled in is allowed, so the settings can be
	// entered one at a time
	if err := c.Set("report.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("report.recipients", "mum@example.com, dad@example.org\nGran@Example.net"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get("report.recipients"); got != "mum@example.com,dad@example.org,Gran@Example.net" {
		t.Fatalf("recipients = %q", got)
	}
	if err := c.Set("report.from", "Parent Tracker <reports@example.com>"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("report.resend_api_key", " re_123abc "); err != nil || c.Report.ResendAPIKey != "re_123abc" {
		t.Fatalf("key = %q, %v", c.Report.ResendAPIKey, err)
	}
	if p := c.ReportProblems(); len(p) != 0 {
		t.Fatalf("problems = %v", p)
	}
	for key, bad := range map[string]string{
		"report.send_at":        "7am",
		"report.recipients":     "mum@example.com, not-an-address",
		"report.from":           "nobody",
		"report.resend_api_key": "re_1 23",
	} {
		before := c.Clone()
		if err := c.Set(key, bad); err == nil {
			t.Errorf("%s = %q should be rejected", key, bad)
		}
		if c.Report.SendAt != before.Report.SendAt || len(c.Report.Recipients) != len(before.Report.Recipients) {
			t.Fatal("a rejected value must not change the config")
		}
	}
	if err := c.Set("report.recipients", "a@example.com,A@example.com"); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate: %v", err)
	}
	if err := c.Set("report.recipients", "Mum <mum@example.com>"); err == nil {
		t.Fatal("a display name is not a bare address")
	}
	many := make([]string, MaxReportRecipients+1)
	for i := range many {
		many[i] = fmt.Sprintf("p%d@example.com", i)
	}
	if err := c.Set("report.recipients", strings.Join(many, ",")); err == nil {
		t.Fatal("too many recipients must be rejected")
	}
	if !IsSecret("report.resend_api_key") || IsSecret("report.from") {
		t.Fatal("the API key is a secret")
	}

	// the list survives a save and a load, and a clone does not share it
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	back, err := Load(p)
	if err != nil || len(back.Report.Recipients) != 3 || back.Report.ResendAPIKey != "re_123abc" || !back.Report.Enabled {
		t.Fatalf("loaded = %+v, %v", back.Report, err)
	}
	cl := back.Clone()
	cl.Report.Recipients[0] = "x@example.com"
	if back.Report.Recipients[0] == "x@example.com" {
		t.Fatal("Clone must copy the recipients")
	}
	// a file without a [report] section keeps the defaults
	old, err := Parse([]byte("monitored_users = []\n"))
	if err != nil || old.Report.SendAt != "07:00" || old.Report.Recipients == nil {
		t.Fatalf("old file = %+v, %v", old.Report, err)
	}
}

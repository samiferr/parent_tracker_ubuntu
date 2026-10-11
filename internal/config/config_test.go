package config

import (
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
		"retention.hourly_usage", "retention.prune_interval",
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

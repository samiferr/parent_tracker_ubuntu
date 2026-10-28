// Package config loads, validates and edits /etc/parent-tracker/config.toml.
//
// Unknown keys are errors (docs/USER-GUIDE §8). Sensitive collectors are off by
// default, and only the users in monitored_users are ever recorded.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// DefaultPath is where the packaged service reads its configuration.
const DefaultPath = "/etc/parent-tracker/config.toml"

// Duration is a time.Duration that reads and writes as a Go duration string
// ("5s", "720h") in TOML.
type Duration time.Duration

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// Milliseconds returns the value in milliseconds.
func (d Duration) Milliseconds() int64 { return time.Duration(d).Milliseconds() }

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// String formats the duration compactly: "5s", "15m", "720h" (the form the
// documentation and `config get` use), falling back to Go's form otherwise.
func (d Duration) String() string {
	v := time.Duration(d)
	switch {
	case v == 0:
		return "0s"
	case v%time.Hour == 0:
		return fmt.Sprintf("%dh", v/time.Hour)
	case v%time.Minute == 0:
		return fmt.Sprintf("%dm", v/time.Minute)
	case v%time.Second == 0:
		return fmt.Sprintf("%ds", v/time.Second)
	}
	return v.String()
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(strings.TrimSpace(string(b)))
	if err != nil {
		return fmt.Errorf("%q is not a duration such as 5s or 720h", string(b))
	}
	*d = Duration(v)
	return nil
}

// Config is the whole configuration file.
type Config struct {
	DataDir           string   `toml:"data_dir"`
	DBName            string   `toml:"db_name"`
	LogLevel          string   `toml:"log_level"`
	SampleInterval    Duration `toml:"sample_interval"`
	WebImportInterval Duration `toml:"web_import_interval"`
	ControlSocket     string   `toml:"control_socket"`
	AgentSocket       string   `toml:"agent_socket"`
	MonitoredUsers    []string `toml:"monitored_users"`
	// DisclosureAcknowledged is the RFC 3339 time at which the administrator
	// acknowledged the disclosure notice. Nothing is recorded until it is set
	// (SEC-1, AC-71).
	DisclosureAcknowledged string `toml:"disclosure_acknowledged"`

	Collectors Collectors `toml:"collectors"`
	Retention  Retention  `toml:"retention"`
	Server     Server     `toml:"server"`
	Alerts     Alerts     `toml:"alerts"`
	Categories []Category `toml:"categories"`
}

// Collectors switches what is collected. Sensitive ones default to off.
type Collectors struct {
	Session     bool `toml:"session"`
	App         bool `toml:"app"`
	WindowTitle bool `toml:"window_title"`
	Web         bool `toml:"web"`
	WebFullURL  bool `toml:"web_full_url"`
	Screenshots bool `toml:"screenshots"`
	Devices     bool `toml:"devices"`
}

// Retention is how long each kind of data is kept.
type Retention struct {
	WindowSamples Duration `toml:"window_samples"`
	Intervals     Duration `toml:"intervals"`
	AppUsage      Duration `toml:"app_usage"`
	HourlyUsage   Duration `toml:"hourly_usage"`
	WebVisits     Duration `toml:"web_visits"`
	Alerts        Duration `toml:"alerts"`
	Sessions      Duration `toml:"sessions"`
	PruneInterval Duration `toml:"prune_interval"`
}

// Server configures the dashboard.
type Server struct {
	Enabled        bool   `toml:"enabled"`
	Listen         string `toml:"listen"`
	Username       string `toml:"username"`
	PasswordHash   string `toml:"password_hash"`
	AllowLAN       bool   `toml:"allow_lan"`
	Live           bool   `toml:"live"`
	LiveMaxClients int    `toml:"live_max_clients"`
}

// Alerts configures the alert engine.
type Alerts struct {
	Enabled             bool     `toml:"enabled"`
	Channel             string   `toml:"channel"`
	NtfyURL             string   `toml:"ntfy_url"`
	WebhookURL          string   `toml:"webhook_url"`
	DailyActiveLimit    Duration `toml:"daily_active_limit"`
	Bedtime             string   `toml:"bedtime"`
	NotifyOnServiceStop bool     `toml:"notify_on_service_stop"`
}

// Category maps application ids to a human category with glob patterns.
type Category struct {
	Name     string   `toml:"name"`
	Patterns []string `toml:"patterns"`
}

// Channels the alert engine can use.
var Channels = []string{"none", "desktop", "ntfy", "webhook"}

// Default returns the documented defaults.
func Default() *Config {
	h := func(hours int) Duration { return Duration(time.Duration(hours) * time.Hour) }
	return &Config{
		DataDir:           "/var/lib/parent-tracker",
		DBName:            "tracker.db",
		LogLevel:          "info",
		SampleInterval:    Duration(5 * time.Second),
		WebImportInterval: Duration(15 * time.Minute),
		ControlSocket:     "/run/parent-tracker/control.sock",
		AgentSocket:       "/run/parent-tracker/agent.sock",
		MonitoredUsers:    []string{},
		Collectors: Collectors{
			Session: true,
			App:     true,
		},
		Retention: Retention{
			WindowSamples: h(720),
			Intervals:     h(4320),
			AppUsage:      h(17520),
			HourlyUsage:   h(8760),
			WebVisits:     h(2160),
			Alerts:        h(8760),
			Sessions:      h(8760),
			PruneInterval: h(6),
		},
		Server: Server{
			Enabled:        true,
			Listen:         "127.0.0.1:8787",
			Username:       "parent",
			Live:           true,
			LiveMaxClients: 10,
		},
		Alerts: Alerts{
			Channel:             "none",
			NotifyOnServiceStop: true,
		},
		Categories: DefaultCategories(),
	}
}

// DefaultCategories are used when the file defines no [[categories]].
func DefaultCategories() []Category {
	return []Category{
		{Name: "games", Patterns: []string{"steam", "*game*", "minecraft", "lutris", "heroic", "roblox*", "org.prismlauncher*"}},
		{Name: "school", Patterns: []string{"libreoffice*", "code", "zotero", "anki", "org.gnome.evince", "org.kde.okular", "geogebra*"}},
		{Name: "social", Patterns: []string{"discord", "telegram*", "org.telegram*", "signal*", "whatsapp*", "slack", "element*"}},
		{Name: "video", Patterns: []string{"vlc", "mpv", "org.gnome.totem", "totem", "celluloid", "io.github.celluloid_player*", "*youtube*", "*netflix*"}},
		{Name: "browser", Patterns: []string{"firefox*", "chromium*", "google-chrome*", "brave*", "microsoft-edge*", "org.mozilla.firefox", "epiphany", "org.gnome.epiphany"}},
	}
}

// DBPath is the full database path.
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, c.DBName) }

// Clone returns a deep copy.
func (c *Config) Clone() *Config {
	n := *c
	n.MonitoredUsers = append([]string(nil), c.MonitoredUsers...)
	n.Categories = make([]Category, len(c.Categories))
	for i, cat := range c.Categories {
		n.Categories[i] = Category{Name: cat.Name, Patterns: append([]string(nil), cat.Patterns...)}
	}
	return &n
}

// Load reads and validates a config file. A missing file is an error.
func Load(p string) (*Config, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse decodes TOML on top of the defaults, rejecting unknown keys.
func Parse(raw []byte) (*Config, error) {
	cfg := Default()
	dec := toml.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	// A [[categories]] section in the file replaces the defaults entirely.
	cfg.Categories = nil
	if err := dec.Decode(cfg); err != nil {
		var missing *toml.StrictMissingError
		if errors.As(err, &missing) {
			// Name every key this version does not know, so a file written by
			// another version (or a typo) can be fixed without guessing.
			var found []string
			for i := range missing.Errors {
				row, _ := missing.Errors[i].Position()
				found = append(found, fmt.Sprintf("line %d: %q is not a setting this version knows", row, strings.Join(missing.Errors[i].Key(), ".")))
			}
			return nil, fmt.Errorf("config: %s", strings.Join(found, "; "))
		}
		if de, ok := err.(*toml.DecodeError); ok {
			row, col := de.Position()
			return nil, fmt.Errorf("config: line %d, column %d: %s", row, col, de.Error())
		}
		return nil, fmt.Errorf("config: %w", err)
	}
	if len(cfg.Categories) == 0 {
		cfg.Categories = DefaultCategories()
	}
	if cfg.MonitoredUsers == nil {
		cfg.MonitoredUsers = []string{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks values that would make the service unsafe or useless.
func (c *Config) Validate() error {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	if c.DataDir == "" || !filepath.IsAbs(c.DataDir) {
		add("data_dir must be an absolute path")
	}
	if c.DBName == "" || strings.ContainsAny(c.DBName, `/\`) {
		add("db_name must be a file name")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		add("log_level must be debug, info, warn or error")
	}
	if d := c.SampleInterval.D(); d < time.Second || d > time.Minute {
		add("sample_interval must be between 1s and 1m")
	}
	if c.WebImportInterval.D() < time.Minute {
		add("web_import_interval must be at least 1m")
	}
	seen := map[string]bool{}
	for _, u := range c.MonitoredUsers {
		if strings.TrimSpace(u) == "" || strings.ContainsAny(u, " \t/") {
			add("monitored_users contains an invalid user name %q", u)
		}
		if seen[u] {
			add("monitored_users lists %q twice", u)
		}
		seen[u] = true
	}
	if c.DisclosureAcknowledged != "" {
		if _, err := time.Parse(time.RFC3339, c.DisclosureAcknowledged); err != nil {
			add("disclosure_acknowledged must be an RFC 3339 time")
		}
	}
	if c.Collectors.WebFullURL && !c.Collectors.Web {
		add("collectors.web_full_url needs collectors.web")
	}

	// Raw samples must outlive the 48 h rollup window, or the rollup would
	// recompute hours from missing data.
	if c.Retention.WindowSamples.D() < 72*time.Hour {
		add("retention.window_samples must be at least 72h")
	}
	if c.Retention.PruneInterval.D() < time.Minute {
		add("retention.prune_interval must be at least 1m")
	}
	for name, d := range map[string]Duration{
		"retention.intervals": c.Retention.Intervals, "retention.app_usage": c.Retention.AppUsage,
		"retention.hourly_usage": c.Retention.HourlyUsage, "retention.web_visits": c.Retention.WebVisits,
		"retention.alerts": c.Retention.Alerts, "retention.sessions": c.Retention.Sessions,
	} {
		if d.D() < 24*time.Hour {
			add("%s must be at least 24h", name)
		}
	}

	if c.Server.LiveMaxClients < 1 || c.Server.LiveMaxClients > 1000 {
		add("server.live_max_clients must be between 1 and 1000")
	}
	if c.Server.Enabled {
		host, _, err := net.SplitHostPort(c.Server.Listen)
		if err != nil {
			add("server.listen must be host:port")
		} else if !IsLoopbackHost(host) && !c.Server.AllowLAN {
			add("server.listen %q is not a loopback address: set server.allow_lan to true first", c.Server.Listen)
		}
	}
	if c.Server.Username == "" {
		add("server.username must not be empty")
	}

	ok := false
	for _, ch := range Channels {
		if c.Alerts.Channel == ch {
			ok = true
		}
	}
	if !ok {
		add("alerts.channel must be one of %s", strings.Join(Channels, ", "))
	}
	if c.Alerts.Bedtime != "" {
		if _, err := ParseClock(c.Alerts.Bedtime); err != nil {
			add("alerts.bedtime must be HH:MM (24-hour) or empty")
		}
	}
	if c.Alerts.DailyActiveLimit.D() < 0 {
		add("alerts.daily_active_limit must not be negative")
	}
	for _, cat := range c.Categories {
		if cat.Name == "" {
			add("a [[categories]] entry has no name")
		}
		for _, p := range cat.Patterns {
			if _, err := path.Match(p, "x"); err != nil {
				add("category %q: bad pattern %q", cat.Name, p)
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("config: %s", strings.Join(problems, "; "))
	}
	return nil
}

// IsLoopbackHost reports whether host is a loopback name or address.
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ParseClock parses "HH:MM" into minutes after midnight.
func ParseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

// CategoryFor returns the category of an application id, or "other".
func (c *Config) CategoryFor(appID string) string {
	id := strings.ToLower(appID)
	for _, cat := range c.Categories {
		for _, p := range cat.Patterns {
			if ok, _ := path.Match(strings.ToLower(p), id); ok {
				return cat.Name
			}
		}
	}
	return "other"
}

// IsMonitored reports whether a user name is on the allowlist.
func (c *Config) IsMonitored(user string) bool {
	for _, u := range c.MonitoredUsers {
		if u == user {
			return true
		}
	}
	return false
}

// Recording reports whether the service may record anything: the disclosure
// has been acknowledged and the session collector is on.
func (c *Config) Recording() bool {
	return c.DisclosureAcknowledged != "" && c.Collectors.Session
}

// NotCollected lists the collectors that are switched on in c but that this
// release does not implement: turning them on records nothing. The keys exist so
// the file format is stable (docs/11).
func (c *Config) NotCollected() []string {
	var keys []string
	for _, k := range []struct {
		key string
		on  bool
	}{
		{"collectors.web", c.Collectors.Web},
		{"collectors.web_full_url", c.Collectors.WebFullURL},
		{"collectors.screenshots", c.Collectors.Screenshots},
		{"collectors.devices", c.Collectors.Devices},
	} {
		if k.on {
			keys = append(keys, k.key)
		}
	}
	return keys
}

// IsNotCollected reports whether key names a collector this release does not
// implement.
func IsNotCollected(key string) bool {
	switch key {
	case "collectors.web", "collectors.web_full_url", "collectors.screenshots", "collectors.devices":
		return true
	}
	return false
}

// Marshal renders the config as TOML.
func (c *Config) Marshal() ([]byte, error) {
	b, err := toml.Marshal(c)
	if err != nil {
		return nil, err
	}
	return append([]byte("# Parent Tracker configuration. See /usr/share/doc/parent-tracker/config.toml.example.\n"), b...), nil
}

// Save validates and writes the config atomically with mode 0640.
func (c *Config) Save(p string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := c.Marshal()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o640); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

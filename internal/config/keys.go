package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type keySpec struct {
	get func(*Config) string
	set func(*Config, string) error // nil: not settable from the CLI
}

func boolKey(get func(*Config) *bool) keySpec {
	return keySpec{
		get: func(c *Config) string { return strconv.FormatBool(*get(c)) },
		set: func(c *Config, v string) error {
			b, err := strconv.ParseBool(strings.TrimSpace(v))
			if err != nil {
				return fmt.Errorf("%q is not true or false", v)
			}
			*get(c) = b
			return nil
		},
	}
}

func durKey(get func(*Config) *Duration) keySpec {
	return keySpec{
		get: func(c *Config) string { return get(c).String() },
		set: func(c *Config, v string) error {
			var d Duration
			if err := d.UnmarshalText([]byte(v)); err != nil {
				return err
			}
			*get(c) = d
			return nil
		},
	}
}

func strKey(get func(*Config) *string) keySpec {
	return keySpec{
		get: func(c *Config) string { return *get(c) },
		set: func(c *Config, v string) error { *get(c) = strings.TrimSpace(v); return nil },
	}
}

var keyTable = map[string]keySpec{
	"data_dir":            {get: func(c *Config) string { return c.DataDir }},
	"db_name":             {get: func(c *Config) string { return c.DBName }},
	"log_level":           strKey(func(c *Config) *string { return &c.LogLevel }),
	"sample_interval":     durKey(func(c *Config) *Duration { return &c.SampleInterval }),
	"web_import_interval": durKey(func(c *Config) *Duration { return &c.WebImportInterval }),
	"control_socket":      strKey(func(c *Config) *string { return &c.ControlSocket }),
	"monitored_users": {get: func(c *Config) string {
		return strings.Join(c.MonitoredUsers, ",")
	}},
	"disclosure_acknowledged": {get: func(c *Config) string { return c.DisclosureAcknowledged }},

	"collectors.session":      boolKey(func(c *Config) *bool { return &c.Collectors.Session }),
	"collectors.app":          boolKey(func(c *Config) *bool { return &c.Collectors.App }),
	"collectors.window_title": boolKey(func(c *Config) *bool { return &c.Collectors.WindowTitle }),
	"collectors.web":          boolKey(func(c *Config) *bool { return &c.Collectors.Web }),
	"collectors.web_full_url": boolKey(func(c *Config) *bool { return &c.Collectors.WebFullURL }),
	"collectors.screenshots":  boolKey(func(c *Config) *bool { return &c.Collectors.Screenshots }),
	"collectors.devices":      {get: func(c *Config) string { return strconv.FormatBool(c.Collectors.Devices) }},

	"retention.window_samples": durKey(func(c *Config) *Duration { return &c.Retention.WindowSamples }),
	"retention.intervals":      durKey(func(c *Config) *Duration { return &c.Retention.Intervals }),
	"retention.app_usage":      durKey(func(c *Config) *Duration { return &c.Retention.AppUsage }),
	"retention.hourly_usage":   durKey(func(c *Config) *Duration { return &c.Retention.HourlyUsage }),
	"retention.web_visits":     {get: func(c *Config) string { return c.Retention.WebVisits.String() }},
	"retention.alerts":         {get: func(c *Config) string { return c.Retention.Alerts.String() }},
	"retention.sessions":       {get: func(c *Config) string { return c.Retention.Sessions.String() }},
	"retention.prune_interval": durKey(func(c *Config) *Duration { return &c.Retention.PruneInterval }),

	"server.enabled":   boolKey(func(c *Config) *bool { return &c.Server.Enabled }),
	"server.listen":    strKey(func(c *Config) *string { return &c.Server.Listen }),
	"server.username":  {get: func(c *Config) string { return c.Server.Username }},
	"server.allow_lan": boolKey(func(c *Config) *bool { return &c.Server.AllowLAN }),
	"server.live":      boolKey(func(c *Config) *bool { return &c.Server.Live }),
	"server.live_max_clients": {
		get: func(c *Config) string { return strconv.Itoa(c.Server.LiveMaxClients) },
		set: func(c *Config, v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return fmt.Errorf("%q is not a whole number", v)
			}
			c.Server.LiveMaxClients = n
			return nil
		},
	},

	"alerts.enabled":            boolKey(func(c *Config) *bool { return &c.Alerts.Enabled }),
	"alerts.channel":            strKey(func(c *Config) *string { return &c.Alerts.Channel }),
	"alerts.ntfy_url":           strKey(func(c *Config) *string { return &c.Alerts.NtfyURL }),
	"alerts.webhook_url":        strKey(func(c *Config) *string { return &c.Alerts.WebhookURL }),
	"alerts.daily_active_limit": durKey(func(c *Config) *Duration { return &c.Alerts.DailyActiveLimit }),
	"alerts.bedtime":            strKey(func(c *Config) *string { return &c.Alerts.Bedtime }),
	"alerts.notify_on_service_stop": {get: func(c *Config) string {
		return strconv.FormatBool(c.Alerts.NotifyOnServiceStop)
	}},
}

// Get returns one value as text.
func (c *Config) Get(key string) (string, error) {
	k, ok := keyTable[key]
	if !ok {
		return "", fmt.Errorf("unknown key %q: run `parent-tracker config list` to see valid keys", key)
	}
	return k.get(c), nil
}

// Set changes one settable key and validates the result. On error the receiver
// is left unchanged.
func (c *Config) Set(key, value string) error {
	k, ok := keyTable[key]
	if !ok {
		return fmt.Errorf("unknown key %q: run `parent-tracker config list` to see valid keys", key)
	}
	if k.set == nil {
		return fmt.Errorf("%q cannot be set from the command line: edit the config file instead", key)
	}
	n := c.Clone()
	if err := k.set(n, value); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if err := n.Validate(); err != nil {
		return err
	}
	*c = *n
	return nil
}

// Settable lists the keys that can be set from the CLI, sorted.
func Settable() []string {
	var keys []string
	for k, v := range keyTable {
		if v.set != nil {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// Keys lists every known key, sorted.
func Keys() []string {
	keys := make([]string, 0, len(keyTable))
	for k := range keyTable {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

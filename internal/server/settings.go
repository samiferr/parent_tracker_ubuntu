package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// editable are the settings the dashboard may change (FR-8.3). Who is
// monitored, the listen address, LAN exposure and the password stay in the CLI:
// they decide who can be watched and who can see it.
var editable = map[string]string{
	"sample_interval":           "duration",
	"collectors.app":            "bool",
	"collectors.window_title":   "bool",
	"server.live":               "bool",
	"server.live_max_clients":   "int",
	"alerts.enabled":            "bool",
	"alerts.channel":            "string",
	"alerts.ntfy_url":           "string",
	"alerts.webhook_url":        "string",
	"alerts.daily_active_limit": "duration",
	"alerts.bedtime":            "string",
	"retention.window_samples":  "duration",
	"retention.intervals":       "duration",
	"retention.app_usage":       "duration",
	"retention.hourly_usage":    "duration",
}

// secretKeys hold private topics or URLs: their values are never written to the
// audit log (SEC-22).
var secretKeys = map[string]bool{"alerts.ntfy_url": true, "alerts.webhook_url": true}

func (s *Server) settingsBody() map[string]any {
	cfg := s.d.Cfg()
	vals := map[string]any{}
	for k, typ := range editable {
		raw, _ := cfg.Get(k)
		switch typ {
		case "bool":
			vals[k] = raw == "true"
		case "int":
			n, _ := strconv.Atoi(raw)
			vals[k] = n
		default:
			vals[k] = raw
		}
	}
	return map[string]any{
		"values":   vals,
		"editable": s.d.CfgPath != "",
		"read_only": map[string]any{
			"monitored_users":  cfg.MonitoredUsers,
			"server.listen":    cfg.Server.Listen,
			"server.allow_lan": cfg.Server.AllowLAN,
			"channels":         config.Channels,
		},
	}
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settingsBody())
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	if s.d.CfgPath == "" {
		apiError(w, http.StatusForbidden, "Settings cannot be changed here.", "Use `sudo parent-tracker config set KEY VALUE` on the PC.")
		return
	}
	var in struct {
		Values map[string]any `json:"values"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil || len(in.Values) == 0 {
		apiError(w, http.StatusBadRequest, "Send the settings to change.", "")
		return
	}
	next := s.d.Cfg().Clone()
	var changed []string
	for _, k := range sortedKeys(in.Values) {
		typ, ok := editable[k]
		if !ok {
			apiError(w, http.StatusBadRequest, fmt.Sprintf("%q cannot be changed from the dashboard.", k), "Use `parent-tracker config set` on the PC.")
			return
		}
		text, ok := asText(in.Values[k], typ)
		if !ok {
			apiError(w, http.StatusBadRequest, fmt.Sprintf("%s has the wrong type of value.", k), "")
			return
		}
		old, _ := next.Get(k)
		if err := next.Set(k, text); err != nil {
			apiError(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if now, _ := next.Get(k); now != old {
			changed = append(changed, k)
		}
	}
	if len(changed) == 0 {
		writeJSON(w, http.StatusOK, s.settingsBody())
		return
	}
	if err := next.Save(s.d.CfgPath); err != nil {
		s.d.Log.Error("cannot save settings", "err", err)
		apiError(w, http.StatusInternalServerError, "The settings could not be saved.", "Check the permissions of the config file with `parent-tracker doctor`.")
		return
	}
	if s.d.Reload != nil {
		if err := s.d.Reload(); err != nil {
			apiError(w, http.StatusInternalServerError, "Saved, but the service could not apply the settings.", "Run `sudo systemctl reload parent-trackerd`.")
			return
		}
	}
	var names []string
	for _, k := range changed {
		if secretKeys[k] {
			names = append(names, k+" (value not logged)")
			continue
		}
		v, _ := next.Get(k)
		names = append(names, k+"="+v)
	}
	_ = s.d.DB.Audit(s.d.Now().UnixMilli(), "dashboard", model.AuditConfigChanged, strings.Join(names, ", "))
	writeJSON(w, http.StatusOK, s.settingsBody())
}

func asText(v any, typ string) (string, bool) {
	switch typ {
	case "bool":
		b, ok := v.(bool)
		return strconv.FormatBool(b), ok
	case "int":
		f, ok := v.(float64)
		return strconv.Itoa(int(f)), ok && f == float64(int(f))
	}
	s, ok := v.(string)
	return s, ok
}

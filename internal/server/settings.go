package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"

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
	"report.enabled":            "bool",
	"report.send_at":            "string",
	"report.from":               "string",
	"report.recipients":         "list",
	"report.resend_api_key":     "secret",
}

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
		case "list":
			vals[k] = config.SplitList(raw)
		case "secret":
			// never sent back: the page only learns whether one is set
			vals[k] = ""
		default:
			vals[k] = raw
		}
	}
	return map[string]any{
		"values":   vals,
		"editable": s.d.CfgPath != "",
		"secrets_set": map[string]bool{
			"report.resend_api_key": cfg.Report.ResendAPIKey != "",
		},
		"report": map[string]any{
			"problems":  orEmpty(cfg.ReportProblems()),
			"can_test":  s.d.SendReport != nil,
			"last_sent": s.lastSentDay(),
			"timezone":  zoneName(s.d.Loc),
			"max_to":    config.MaxReportRecipients,
		},
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
		if typ == "secret" && text == "" { // empty keeps the saved secret
			continue
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
		if config.IsSecret(k) {
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
	case "list":
		if s, ok := v.(string); ok {
			return s, true
		}
		items, ok := v.([]any)
		if !ok {
			return "", false
		}
		parts := make([]string, 0, len(items))
		for _, it := range items {
			s, ok := it.(string)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ","), true
	case "secret":
		if v == nil {
			return "", true
		}
	}
	s, ok := v.(string)
	return s, ok
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Server) lastSentDay() string {
	v, _, _ := s.d.DB.GetState("report_last_day")
	return v
}

// handleReportTest e-mails a report now, to check the Resend settings. It
// covers today so far unless a date is given, and does not count as the
// scheduled report.
func (s *Server) handleReportTest(w http.ResponseWriter, r *http.Request) {
	if s.d.SendReport == nil {
		apiError(w, http.StatusServiceUnavailable, "Reports cannot be sent from here.", "Use the dashboard of the running service.")
		return
	}
	var in struct {
		Date string `json:"date"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		apiError(w, http.StatusBadRequest, "Send a JSON body, for example {}.", "")
		return
	}
	if in.Date == "" {
		in.Date = s.today()
	} else if _, _, err := aggregate.ParseDay(s.d.Loc, in.Date); err != nil {
		apiError(w, http.StatusBadRequest, "date must be YYYY-MM-DD.", "Use dates like 2026-10-09.")
		return
	}
	s.reportMu.Lock()
	if wait := 30*time.Second - s.d.Now().Sub(s.lastReport); wait > 0 && !s.lastReport.IsZero() {
		s.reportMu.Unlock()
		apiError(w, http.StatusTooManyRequests, "A report was sent a moment ago.", fmt.Sprintf("Try again in %d seconds.", int(wait.Seconds())+1))
		return
	}
	s.lastReport = s.d.Now()
	s.reportMu.Unlock()

	n, err := s.d.SendReport(r.Context(), in.Date, "")
	if err != nil {
		s.reportMu.Lock()
		s.lastReport = time.Time{}
		s.reportMu.Unlock()
		s.d.Log.Warn("test report not sent", "err", err.Error())
		hint := "Check the API key, the sender address (its domain must be verified in Resend) and the recipients."
		if strings.Contains(err.Error(), "cannot reach Resend") {
			hint = "Check that this PC can reach api.resend.com."
		}
		apiError(w, http.StatusBadGateway, "The report was not sent: "+err.Error(), hint)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent_to": n, "date": in.Date})
}

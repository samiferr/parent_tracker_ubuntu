package server

import (
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/live"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// monitoredUsers lists the allowlisted accounts the service knows about.
func (s *Server) monitoredUsers() ([]model.User, error) {
	all, err := s.d.DB.Users()
	if err != nil {
		return nil, err
	}
	cfg := s.d.Cfg()
	var out []model.User
	for _, u := range all {
		if u.Monitored && cfg.IsMonitored(u.Username) {
			out = append(out, u)
		}
	}
	return out, nil
}

// userFrom resolves ?user= to a monitored account (the first when omitted).
func (s *Server) userFrom(w http.ResponseWriter, r *http.Request) (model.User, bool) {
	users, err := s.monitoredUsers()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "Could not read the database.", "Run `parent-tracker doctor`.")
		return model.User{}, false
	}
	name := r.URL.Query().Get("user")
	if len(users) == 0 {
		apiError(w, http.StatusNotFound, "No monitored users yet.", "Run `sudo parent-tracker users add NAME`, then reload the service.")
		return model.User{}, false
	}
	if name == "" {
		return users[0], true
	}
	for _, u := range users {
		if u.Username == name {
			return u, true
		}
	}
	apiError(w, http.StatusNotFound, "That user is not monitored.", "")
	return model.User{}, false
}

func (s *Server) today() string { return aggregate.DayString(s.d.Loc, s.d.Now().UnixMilli()) }

func (s *Server) dateParam(r *http.Request, key, def string) string {
	if v := r.URL.Query().Get(key); v != "" {
		return v
	}
	return def
}

func (s *Server) daysAgo(n int) string {
	return aggregate.DayString(s.d.Loc, s.d.Now().UnixMilli()-int64(n)*24*3600*1000)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	msg := err.Error()
	// view errors for malformed input read "… must be YYYY-MM-DD: …"
	if containsAny(msg, "must be YYYY-MM-DD", "must not be before", "between 1 and 400", "longer than 400", "level must be") {
		apiError(w, http.StatusBadRequest, msg, "Use dates like 2026-10-09.")
		return
	}
	s.d.Log.Error("api error", "err", err)
	apiError(w, http.StatusInternalServerError, "Something went wrong reading the data.", "Run `parent-tracker doctor`.")
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

type userResp struct {
	Name  string `json:"name"`
	UID   int    `json:"uid"`
	State string `json:"state,omitempty"`
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.monitoredUsers()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]userResp, 0, len(users))
	for _, u := range users {
		ur := userResp{Name: u.Username, UID: u.UID}
		if s.d.Hub != nil {
			if snap, ok := s.d.Hub.Snapshot(u.UID); ok {
				ur.State = snap.State
			}
		}
		out = append(out, ur)
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFrom(w, r)
	if !ok {
		return
	}
	day, err := s.view.Day(u.UID, s.dateParam(r, "date", s.today()))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u.Username, "day": day})
}

func (s *Server) handleHourly(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFrom(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if q.Get("from") != "" || q.Get("to") != "" {
		from := s.dateParam(r, "from", s.daysAgo(6))
		to := s.dateParam(r, "to", s.today())
		hr, err := s.view.HourlyRange(u.UID, from, to)
		if err != nil {
			s.fail(w, err)
			return
		}
		if q.Get("shape") == "avg" {
			writeJSON(w, http.StatusOK, map[string]any{"user": u.Username, "avg_active_ms": hr.AvgMs, "summary": hr.Summary})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user": u.Username, "range": hr})
		return
	}
	hd, err := s.view.HourlyDay(u.UID, s.dateParam(r, "date", s.today()), q.Get("apps") == "1")
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u.Username, "day": hd})
}

func (s *Server) handleTrend(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFrom(w, r)
	if !ok {
		return
	}
	pts, err := s.view.Trend(u.UID, s.dateParam(r, "from", s.daysAgo(13)), s.dateParam(r, "to", s.today()))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u.Username, "days": pts})
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFrom(w, r)
	if !ok {
		return
	}
	av, err := s.view.Apps(u.UID, s.dateParam(r, "from", s.daysAgo(6)), s.dateParam(r, "to", s.today()))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u.Username, "apps": av})
}

func (s *Server) handleAppDetail(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userFrom(w, r)
	if !ok {
		return
	}
	app := r.URL.Query().Get("app")
	if app == "" {
		apiError(w, http.StatusBadRequest, "Choose an application.", "")
		return
	}
	d, err := s.view.AppDetail(u.UID, app, s.dateParam(r, "from", s.daysAgo(6)), s.dateParam(r, "to", s.today()))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u.Username, "app": d})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	uid := 0
	if r.URL.Query().Get("user") != "" {
		u, ok := s.userFrom(w, r)
		if !ok {
			return
		}
		uid = u.UID
	} else if users, _ := s.monitoredUsers(); len(users) > 0 {
		uid = users[0].UID
	}
	from, to := s.view.DefaultEventRange()
	limit := 200
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	ev, err := s.view.Events(uid, from, to, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

type liveResp struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	*live.Snapshot
}

func (s *Server) liveEnabled() bool {
	return s.d.Hub != nil && s.d.Hub.Enabled() && s.d.Cfg().Server.Live
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if !s.liveEnabled() {
		apiError(w, http.StatusNotFound, "The live view is off.", "Turn it on with `sudo parent-tracker config set server.live true`.")
		return
	}
	u, ok := s.userFrom(w, r)
	if !ok {
		return
	}
	snap, found := s.d.Hub.Snapshot(u.UID)
	if !found {
		writeJSON(w, http.StatusOK, liveResp{Reason: "The collectors are not reporting yet."})
		return
	}
	snap.ServerTime = s.d.Now().UnixMilli()
	writeJSON(w, http.StatusOK, liveResp{Available: true, Snapshot: &snap})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"version": s.d.Version, "time": s.d.Now().UnixMilli(), "collectors_running": s.d.Health != nil}
	var degraded []live.Degraded
	if s.d.Health != nil {
		h := s.d.Health()
		out["tracker"] = h
		for _, u := range h.Users {
			if u.Degraded != nil {
				degraded = append(degraded, *u.Degraded)
			}
		}
	}
	if degraded == nil {
		degraded = []live.Degraded{}
	}
	out["degraded"] = degraded
	cfg := s.d.Cfg()
	out["recording"] = cfg.Recording()
	out["disclosure_acknowledged"] = cfg.DisclosureAcknowledged != ""
	counts, err := s.d.DB.Counts()
	if err != nil {
		s.fail(w, err)
		return
	}
	out["database"] = map[string]any{"schema_version": s.d.DB.SchemaVersion(), "counts": counts}
	ch, _ := s.d.DB.CollectorHealths()
	out["collectors"] = ch
	if s.d.Hub != nil {
		out["live"] = map[string]any{"enabled": s.liveEnabled(), "clients": s.d.Hub.Clients(), "max_clients": cfg.Server.LiveMaxClients}
	}
	writeJSON(w, http.StatusOK, out)
}

type collectedItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	State       string `json:"state"` // on | off | unavailable
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	cfg := s.d.Cfg()
	on := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	collected := []collectedItem{
		{"Session activity", "When a monitored account is signed in, and whether the session is active, idle, locked or asleep. It comes from systemd-logind and is the same on Wayland and X11.", on(cfg.Collectors.Session)},
		{"Foreground application", "Which application is in front, and for how long. Read directly on X11 sessions and through a GNOME Shell extension on Wayland sessions.", on(cfg.Collectors.App)},
		{"Window titles", "The title of the window in front. This is private, so it is off unless you turn it on.", on(cfg.Collectors.WindowTitle)},
		{"Browser history", "Not available in this release.", "unavailable"},
		{"Screenshots", "Not available in this release, and never recorded without a clear indicator.", "unavailable"},
	}
	notices := ""
	if b, err := fs.ReadFile(s.files, "notices.txt"); err == nil {
		notices = string(b)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   s.d.Version,
		"platform":  "Ubuntu 24.04 LTS or newer, on Wayland or X11 sessions.",
		"collected": collected,
		"never":     []string{"What is on the screen", "Keystrokes", "Message content", "Network traffic"},
		"retention": map[string]string{
			"raw samples":    cfg.Retention.WindowSamples.D().String(),
			"by the hour":    cfg.Retention.HourlyUsage.D().String(),
			"daily app time": cfg.Retention.AppUsage.D().String(),
		},
		"notices": notices,
	})
}

// sortedKeys is a small helper for deterministic output.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/view"
)

// auditShown is how many entries the Audit log tab lists; an export has all of
// them.
const auditShown = 500

// handleAudit serves the Audit log: ?from=&to= (local days, inclusive, default
// the last 7 days), ?level=all|error|warning|info, and ?format=csv or json to
// download the whole period as a file instead of the first auditShown entries.
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to := s.dateParam(r, "from", s.daysAgo(6)), s.dateParam(r, "to", s.today())
	format := q.Get("format")
	if format != "" && format != "csv" && format != "json" {
		apiError(w, http.StatusBadRequest, "format must be csv or json.", "")
		return
	}
	limit := auditShown
	if format != "" {
		limit = 0
	}
	av, err := s.view.Audit(from, to, q.Get("level"), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	if format == "" {
		writeJSON(w, http.StatusOK, av)
		return
	}

	name := fmt.Sprintf("parent-tracker-audit-%s_to_%s", from, to)
	if av.Level != "all" {
		name += "-" + av.Level
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+"."+format+`"`)
	w.Header().Set("Content-Type", map[string]string{"csv": "text/csv; charset=utf-8", "json": "application/json; charset=utf-8"}[format])
	if format == "json" {
		s.writeAuditJSON(w, av)
		return
	}
	s.writeAuditCSV(w, av)
}

type auditExportRow struct {
	Time       string `json:"time"`
	Level      string `json:"level"`
	Actor      string `json:"actor"`
	Event      string `json:"event"`
	Message    string `json:"message"`
	Code       string `json:"code"`
	Detail     string `json:"detail"`
	TimeUnixMs int64  `json:"time_unix_ms"`
}

func (s *Server) exportRows(av *view.AuditView) []auditExportRow {
	rows := make([]auditExportRow, 0, len(av.Entries))
	for _, e := range av.Entries {
		rows = append(rows, auditExportRow{Time: time.UnixMilli(e.TS).In(s.d.Loc).Format(time.RFC3339), Level: e.Level, Actor: e.Actor,
			Event: e.Title, Message: e.Description, Code: e.Action, Detail: e.Detail, TimeUnixMs: e.TS})
	}
	return rows
}

func (s *Server) writeAuditJSON(w http.ResponseWriter, av *view.AuditView) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"from": av.From, "to": av.To, "level": av.Level, "timezone": s.d.Loc.String(),
		"exported_at": s.d.Now().In(s.d.Loc).Format(time.RFC3339), "truncated": av.Truncated,
		"counts": av.Counts, "entries": s.exportRows(av),
	})
}

func (s *Server) writeAuditCSV(w http.ResponseWriter, av *view.AuditView) {
	cw := csv.NewWriter(w)
	_, _ = w.Write([]byte("\xef\xbb\xbf")) // a byte-order mark makes Excel read the file as UTF-8
	_ = cw.Write([]string{"time", "level", "actor", "event", "message", "code", "detail"})
	for _, r := range s.exportRows(av) {
		_ = cw.Write([]string{r.Time, r.Level, r.Actor, csvSafe(r.Event), csvSafe(r.Message), r.Code, csvSafe(r.Detail)})
	}
	cw.Flush()
}

// csvSafe stops a spreadsheet from running a cell as a formula. The text of an
// entry can come from outside the service (an agent's error message).
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

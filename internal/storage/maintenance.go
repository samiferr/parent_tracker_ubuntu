package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// Tables lists every data table, in the order used by Counts and Export.
var Tables = []string{
	"users", "sessions", "activity_intervals", "window_samples", "app_usage",
	"hourly_usage", "hourly_app_usage", "tracking_gaps", "alerts", "audit_log", "collector_health",
}

// Counts returns the number of rows per data table.
func (d *DB) Counts() (map[string]int64, error) {
	out := make(map[string]int64, len(Tables))
	for _, t := range Tables {
		var n int64
		if err := d.sql.QueryRow(`SELECT COUNT(*) FROM ` + t).Scan(&n); err != nil {
			return nil, err
		}
		out[t] = n
	}
	return out, nil
}

// PruneCutoffs are the instants before which each kind of data is removed.
type PruneCutoffs struct {
	WindowSamples int64
	Intervals     int64
	AppUsageDay   string // days strictly before this YYYY-MM-DD are removed
	HourlyUsage   int64
	Alerts        int64 // governs alerts and the audit log
	Sessions      int64
}

// Pruned counts the rows removed by Prune.
type Pruned struct {
	Samples, Intervals, AppUsage, Hourly, HourlyApps, Alerts, Audit, Sessions, Gaps int64
}

// Total is the number of rows removed.
func (p Pruned) Total() int64 {
	return p.Samples + p.Intervals + p.AppUsage + p.Hourly + p.HourlyApps + p.Alerts + p.Audit + p.Sessions + p.Gaps
}

// Prune enforces retention with real DELETEs. Aggregates outlive the raw rows
// they were computed from (AC-94).
func (d *DB) Prune(c PruneCutoffs) (Pruned, error) {
	var p Pruned
	err := d.Write(func(tx *sql.Tx) error {
		del := func(dst *int64, q string, args ...any) error {
			res, err := tx.Exec(q, args...)
			if err != nil {
				return err
			}
			*dst, _ = res.RowsAffected()
			return nil
		}
		steps := []struct {
			dst  *int64
			q    string
			args []any
		}{
			{&p.Samples, `DELETE FROM window_samples WHERE ts < ?`, []any{c.WindowSamples}},
			{&p.Intervals, `DELETE FROM activity_intervals WHERE open = 0 AND ended_at < ?`, []any{c.Intervals}},
			{&p.Gaps, `DELETE FROM tracking_gaps WHERE ended_at < ?`, []any{c.Intervals}},
			{&p.AppUsage, `DELETE FROM app_usage WHERE day < ?`, []any{c.AppUsageDay}},
			{&p.Hourly, `DELETE FROM hourly_usage WHERE hour_start < ?`, []any{c.HourlyUsage}},
			{&p.HourlyApps, `DELETE FROM hourly_app_usage WHERE hour_start < ?`, []any{c.HourlyUsage}},
			{&p.Alerts, `DELETE FROM alerts WHERE created_at < ?`, []any{c.Alerts}},
			{&p.Audit, `DELETE FROM audit_log WHERE ts < ?`, []any{c.Alerts}},
			{&p.Sessions, `DELETE FROM sessions WHERE ended_at IS NOT NULL AND ended_at < ?`, []any{c.Sessions}},
		}
		for _, s := range steps {
			if err := del(s.dst, s.q, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	return p, err
}

// Vacuum rewrites the database file so deleted data is not recoverable from it
// (docs/05 §5.2), then truncates the WAL.
func (d *DB) Vacuum() error {
	d.wmu.Lock()
	defer d.wmu.Unlock()
	if _, err := d.sql.Exec(`VACUUM`); err != nil {
		return err
	}
	_, err := d.sql.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// DeleteCounts counts the rows removed by DeleteUserBefore.
type DeleteCounts map[string]int64

// DeleteUserBefore permanently deletes everything recorded for a user before the
// cutoff — raw samples, intervals, sessions, gaps, alerts and the daily and
// hourly aggregates (FR-10.4, FR-10.5, AC-98) — and vacuums the file. Rows that
// straddle the cutoff are removed only when they end before it.
func (d *DB) DeleteUserBefore(uid int, before int64, beforeDay string) (DeleteCounts, error) {
	out := DeleteCounts{}
	err := d.Write(func(tx *sql.Tx) error {
		steps := []struct{ table, q string }{
			{"window_samples", `DELETE FROM window_samples WHERE uid = ? AND ts < ?`},
			{"activity_intervals", `DELETE FROM activity_intervals WHERE uid = ? AND open = 0 AND ended_at <= ?`},
			{"hourly_usage", `DELETE FROM hourly_usage WHERE uid = ? AND hour_start + hour_len_ms <= ?`},
			{"hourly_app_usage", `DELETE FROM hourly_app_usage WHERE uid = ? AND hour_start < ?`},
			{"sessions", `DELETE FROM sessions WHERE uid = ? AND ended_at IS NOT NULL AND ended_at <= ?`},
			{"tracking_gaps", `DELETE FROM tracking_gaps WHERE uid = ? AND ended_at <= ?`},
			{"alerts", `DELETE FROM alerts WHERE uid = ? AND created_at < ?`},
		}
		for _, s := range steps {
			res, err := tx.Exec(s.q, uid, before)
			if err != nil {
				return err
			}
			out[s.table], _ = res.RowsAffected()
		}
		res, err := tx.Exec(`DELETE FROM app_usage WHERE uid = ? AND day < ?`, uid, beforeDay)
		if err != nil {
			return err
		}
		out["app_usage"], _ = res.RowsAffected()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, d.Vacuum()
}

// ---- export --------------------------------------------------------------

// Manifest describes an export bundle (docs/05 §5.5).
type Manifest struct {
	Tool          string         `json:"tool"`
	Version       string         `json:"version"`
	SchemaVersion int            `json:"schema_version"`
	CreatedAt     string         `json:"created_at"`
	From          string         `json:"from,omitempty"`
	To            string         `json:"to,omitempty"`
	Users         []string       `json:"users"`
	Counts        map[string]int `json:"counts"`
	// Checksum is the SHA-256 of the canonical JSON of Datasets.
	Checksum string `json:"checksum_sha256"`
}

// Datasets holds every recorded dataset in an export.
type Datasets struct {
	Users          []model.User       `json:"users"`
	Sessions       []model.Session    `json:"sessions"`
	Intervals      []model.Interval   `json:"activity_intervals"`
	Samples        []SampleRow        `json:"window_samples"`
	AppUsage       []model.AppDay     `json:"app_usage"`
	HourlyUsage    []model.HourRow    `json:"hourly_usage"`
	HourlyAppUsage []model.HourAppRow `json:"hourly_app_usage"`
	Gaps           []model.Gap        `json:"tracking_gaps"`
	Alerts         []model.Alert      `json:"alerts"`
	Audit          []model.AuditEvent `json:"audit_log"`
}

// Bundle is the JSON document written by `parent-tracker export`.
type Bundle struct {
	Manifest Manifest `json:"manifest"`
	Datasets Datasets `json:"datasets"`
}

// Export collects the recorded data of the given users (all when uids is empty)
// in [from,to). Timestamps stay Unix milliseconds.
func (d *DB) Export(uids []int, from, to int64, fromDay, toDay, version string, now time.Time) (*Bundle, error) {
	users, err := d.Users()
	if err != nil {
		return nil, err
	}
	want := map[int]bool{}
	for _, u := range uids {
		want[u] = true
	}
	var ds Datasets
	var names []string
	for _, u := range users {
		if len(want) > 0 && !want[u.UID] {
			continue
		}
		ds.Users = append(ds.Users, u)
		names = append(names, u.Username)
		ss, err := d.Sessions(u.UID, from, to)
		if err != nil {
			return nil, err
		}
		ds.Sessions = append(ds.Sessions, ss...)
		ivs, err := d.Intervals(u.UID, from, to)
		if err != nil {
			return nil, err
		}
		ds.Intervals = append(ds.Intervals, ivs...)
		sm, err := d.Samples(u.UID, from, to)
		if err != nil {
			return nil, err
		}
		ds.Samples = append(ds.Samples, sm...)
		ad, err := d.AppDays(u.UID, fromDay, toDay)
		if err != nil {
			return nil, err
		}
		ds.AppUsage = append(ds.AppUsage, ad...)
		hr, err := d.HourRows(u.UID, from, to)
		if err != nil {
			return nil, err
		}
		ds.HourlyUsage = append(ds.HourlyUsage, hr...)
		ha, err := d.HourAppRows(u.UID, from, to)
		if err != nil {
			return nil, err
		}
		ds.HourlyAppUsage = append(ds.HourlyAppUsage, ha...)
		gp, err := d.Gaps(u.UID, from, to)
		if err != nil {
			return nil, err
		}
		ds.Gaps = append(ds.Gaps, gp...)
	}
	al, err := d.Alerts(from, to, 1_000_000)
	if err != nil {
		return nil, err
	}
	for _, a := range al {
		if len(want) == 0 || a.UID == 0 || want[a.UID] {
			ds.Alerts = append(ds.Alerts, a)
		}
	}
	if ds.Audit, err = d.AuditEvents(from, to, 1_000_000); err != nil {
		return nil, err
	}

	raw, err := json.Marshal(ds)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	b := &Bundle{Datasets: ds}
	b.Manifest = Manifest{
		Tool: "parent-tracker", Version: version, SchemaVersion: d.SchemaVersion(),
		CreatedAt: now.UTC().Format(time.RFC3339), Users: names,
		Counts: map[string]int{
			"users": len(ds.Users), "sessions": len(ds.Sessions), "activity_intervals": len(ds.Intervals),
			"window_samples": len(ds.Samples), "app_usage": len(ds.AppUsage), "hourly_usage": len(ds.HourlyUsage),
			"hourly_app_usage": len(ds.HourlyAppUsage), "tracking_gaps": len(ds.Gaps), "alerts": len(ds.Alerts), "audit_log": len(ds.Audit),
		},
		Checksum: hex.EncodeToString(sum[:]),
	}
	if fromDay != "" && fromDay != "0000-00-00" {
		b.Manifest.From = fromDay
	}
	if toDay != "" && toDay != "9999-12-31" {
		b.Manifest.To = toDay
	}
	return b, nil
}

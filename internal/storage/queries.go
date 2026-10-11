package storage

import (
	"database/sql"
	"errors"
	"strconv"

	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// ---- users ---------------------------------------------------------------

// UpsertUser records a local account the service has seen.
func (d *DB) UpsertUser(uid int, name string, monitored bool, now int64) error {
	return d.Write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO users(uid, username, monitored, added_at) VALUES (?,?,?,?)
			ON CONFLICT(uid) DO UPDATE SET username = excluded.username, monitored = excluded.monitored`,
			uid, name, boolInt(monitored), now)
		return err
	})
}

// Users lists known accounts ordered by uid.
func (d *DB) Users() ([]model.User, error) {
	rows, err := d.sql.Query(`SELECT uid, username, monitored, added_at FROM users ORDER BY uid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		var u model.User
		var m int
		if err := rows.Scan(&u.UID, &u.Username, &m, &u.AddedAt); err != nil {
			return nil, err
		}
		u.Monitored = m != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserByName looks an account up by name.
func (d *DB) UserByName(name string) (model.User, bool, error) {
	var u model.User
	var m int
	err := d.sql.QueryRow(`SELECT uid, username, monitored, added_at FROM users WHERE username = ?`, name).
		Scan(&u.UID, &u.Username, &m, &u.AddedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, false, nil
	}
	u.Monitored = m != 0
	return u, err == nil, err
}

// ---- sessions ------------------------------------------------------------

// InsertSession opens a session row and returns its id.
func (d *DB) InsertSession(s model.Session) (int64, error) {
	var id int64
	err := d.Write(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO sessions(uid, logind_session_id, started_at, state, host_type, session_type, desktop)
			VALUES (?,?,?,?,?,?,?)`, s.UID, s.LogindID, s.StartedAt, string(s.State), s.HostType, string(s.SessionType), s.Desktop)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// UpdateSessionState stores the latest state of an open session.
func (d *DB) UpdateSessionState(id int64, st model.State) error {
	return d.Write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE sessions SET state = ? WHERE id = ?`, string(st), id)
		return err
	})
}

// CloseSession ends a session.
func (d *DB) CloseSession(id, at int64, reason string) error {
	return d.Write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE sessions SET ended_at = ?, state = 'logged_out', end_reason = ? WHERE id = ? AND ended_at IS NULL`, at, reason, id)
		return err
	})
}

// OpenSessions lists sessions that have no end yet.
func (d *DB) OpenSessions() ([]model.Session, error) {
	return d.querySessions(`WHERE ended_at IS NULL`)
}

// Sessions lists sessions of a user that overlap [from,to).
func (d *DB) Sessions(uid int, from, to int64) ([]model.Session, error) {
	return d.querySessions(`WHERE uid = ? AND started_at < ? AND COALESCE(ended_at, ?) > ?`, uid, to, to, from)
}

func (d *DB) querySessions(where string, args ...any) ([]model.Session, error) {
	rows, err := d.sql.Query(`SELECT id, uid, logind_session_id, started_at, COALESCE(ended_at,0), state, host_type,
		session_type, COALESCE(desktop,''), COALESCE(end_reason,'') FROM sessions `+where+` ORDER BY started_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Session
	for rows.Next() {
		var s model.Session
		var st, stype string
		if err := rows.Scan(&s.ID, &s.UID, &s.LogindID, &s.StartedAt, &s.EndedAt, &st, &s.HostType, &stype, &s.Desktop, &s.EndReason); err != nil {
			return nil, err
		}
		s.State, s.SessionType = model.State(st), model.SessionType(stype)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- activity intervals --------------------------------------------------

// OpenInterval starts an interval that is persisted as open; End tracks the
// last known good instant until CloseInterval is called (NFR-2.1, AC-40).
func (d *DB) OpenInterval(uid int, st model.State, start int64) (int64, error) {
	var id int64
	err := d.Write(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO activity_intervals(uid, state, started_at, ended_at, duration_ms, open) VALUES (?,?,?,?,0,1)`,
			uid, string(st), start, start)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// TouchInterval advances the end of an open interval.
func (d *DB) TouchInterval(id, end int64) error {
	return d.Write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE activity_intervals SET ended_at = MAX(ended_at, ?), duration_ms = MAX(ended_at, ?) - started_at WHERE id = ? AND open = 1`, end, end, id)
		return err
	})
}

// CloseInterval ends an interval at end (never before it started).
func (d *DB) CloseInterval(id, end int64) error {
	return d.Write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE activity_intervals SET ended_at = MAX(started_at, ?), duration_ms = MAX(started_at, ?) - started_at, open = 0 WHERE id = ?`, end, end, id)
		return err
	})
}

// LoadOpenIntervals returns intervals left open by a previous run.
func (d *DB) LoadOpenIntervals() ([]model.Interval, error) {
	return d.queryIntervals(`WHERE open = 1`)
}

// Intervals returns a user's intervals overlapping [from,to), ordered by start.
func (d *DB) Intervals(uid int, from, to int64) ([]model.Interval, error) {
	return d.queryIntervals(`WHERE uid = ? AND started_at < ? AND ended_at > ?`, uid, to, from)
}

func (d *DB) queryIntervals(where string, args ...any) ([]model.Interval, error) {
	rows, err := d.sql.Query(`SELECT id, uid, state, started_at, ended_at, open FROM activity_intervals `+where+` ORDER BY started_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Interval
	for rows.Next() {
		var iv model.Interval
		var st string
		var open int
		if err := rows.Scan(&iv.ID, &iv.UID, &st, &iv.Start, &iv.End, &open); err != nil {
			return nil, err
		}
		iv.State, iv.Open = model.State(st), open != 0
		out = append(out, iv)
	}
	return out, rows.Err()
}

// FirstIntervalStart is the start of the user's earliest recorded interval, or
// 0 when nothing has been recorded.
func (d *DB) FirstIntervalStart(uid int) (int64, error) {
	var v sql.NullInt64
	if err := d.sql.QueryRow(`SELECT MIN(started_at) FROM activity_intervals WHERE uid = ?`, uid).Scan(&v); err != nil {
		return 0, err
	}
	return v.Int64, nil
}

// LastIntervalEnd is the end of the user's most recent recorded interval.
func (d *DB) LastIntervalEnd(uid int) (int64, error) {
	var v sql.NullInt64
	if err := d.sql.QueryRow(`SELECT MAX(ended_at) FROM activity_intervals WHERE uid = ?`, uid).Scan(&v); err != nil {
		return 0, err
	}
	return v.Int64, nil
}

// ---- window samples ------------------------------------------------------

// InsertSamples stores foreground-app samples in one transaction.
func (d *DB) InsertSamples(ss []model.Sample, names map[string]string) error {
	if len(ss) == 0 {
		return nil
	}
	return d.Write(func(tx *sql.Tx) error {
		st, err := tx.Prepare(`INSERT INTO window_samples(uid, ts, app_id, app_name, window_title, pid, state, source) VALUES (?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer st.Close()
		for _, s := range ss {
			var title any
			if s.Title != "" {
				title = s.Title
			}
			if _, err := st.Exec(s.UID, s.TS, s.AppID, names[s.AppID], title, s.PID, string(s.State), s.Source); err != nil {
				return err
			}
		}
		return nil
	})
}

// SampleRow is a stored sample with its app name.
type SampleRow struct {
	model.Sample
	AppName string
}

// Samples returns a user's samples in [from,to) ordered by time.
func (d *DB) Samples(uid int, from, to int64) ([]SampleRow, error) {
	rows, err := d.sql.Query(`SELECT uid, ts, app_id, app_name, COALESCE(window_title,''), pid, state, source
		FROM window_samples WHERE uid = ? AND ts >= ? AND ts < ? ORDER BY ts, id`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SampleRow
	for rows.Next() {
		var r SampleRow
		var st string
		if err := rows.Scan(&r.UID, &r.TS, &r.AppID, &r.AppName, &r.Title, &r.PID, &st, &r.Source); err != nil {
			return nil, err
		}
		r.State = model.State(st)
		out = append(out, r)
	}
	return out, rows.Err()
}

// EarliestSample is the timestamp of the oldest raw sample of a user (0 if none).
func (d *DB) EarliestSample(uid int) (int64, error) {
	var v sql.NullInt64
	if err := d.sql.QueryRow(`SELECT MIN(ts) FROM window_samples WHERE uid = ?`, uid).Scan(&v); err != nil {
		return 0, err
	}
	return v.Int64, nil
}

// ---- gaps ----------------------------------------------------------------

// InsertGap records a period in which nothing was recorded. uid 0 means all users.
func (d *DB) InsertGap(g model.Gap, detail string) error {
	if g.End <= g.Start {
		return nil
	}
	return d.Write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO tracking_gaps(uid, started_at, ended_at, kind, detail) VALUES (?,?,?,?,?)`,
			g.UID, g.Start, g.End, g.Kind, detail)
		return err
	})
}

// Gaps returns the gaps that apply to a user (their own and the all-user ones)
// overlapping [from,to).
func (d *DB) Gaps(uid int, from, to int64) ([]model.Gap, error) {
	rows, err := d.sql.Query(`SELECT id, uid, started_at, ended_at, kind FROM tracking_gaps
		WHERE (uid = ? OR uid = 0) AND started_at < ? AND ended_at > ? ORDER BY started_at`, uid, to, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Gap
	for rows.Next() {
		var g model.Gap
		if err := rows.Scan(&g.ID, &g.UID, &g.Start, &g.End, &g.Kind); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ---- audit and alerts ----------------------------------------------------

// Audit appends an audit_log row.
func (d *DB) Audit(ts int64, actor, action, detail string) error {
	return d.Write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO audit_log(ts, actor, action, detail) VALUES (?,?,?,?)`, ts, actor, action, detail)
		return err
	})
}

// AuditEvents returns audit rows in [from,to), newest first.
func (d *DB) AuditEvents(from, to int64, limit int) ([]model.AuditEvent, error) {
	rows, err := d.sql.Query(`SELECT id, ts, actor, action, COALESCE(detail,'') FROM audit_log WHERE ts >= ? AND ts < ? ORDER BY ts DESC, id DESC LIMIT ?`, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AuditEvent
	for rows.Next() {
		var e model.AuditEvent
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// InsertAlert stores an alert and returns its id.
func (d *DB) InsertAlert(a model.Alert) (int64, error) {
	var id int64
	err := d.Write(func(tx *sql.Tx) error {
		var uid any
		if a.UID != 0 {
			uid = a.UID
		}
		res, err := tx.Exec(`INSERT INTO alerts(created_at, rule_id, uid, severity, message, delivered, delivered_via) VALUES (?,?,?,?,?,?,?)`,
			a.CreatedAt, a.RuleID, uid, a.Severity, a.Message, boolInt(a.Delivered), a.DeliveredVia)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// MarkAlertDelivered records the outcome of sending an alert.
func (d *DB) MarkAlertDelivered(id int64, via string) error {
	return d.Write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE alerts SET delivered = 1, delivered_via = ? WHERE id = ?`, via, id)
		return err
	})
}

// Alerts returns alerts in [from,to), newest first.
func (d *DB) Alerts(from, to int64, limit int) ([]model.Alert, error) {
	rows, err := d.sql.Query(`SELECT id, created_at, rule_id, COALESCE(uid,0), severity, message, delivered, COALESCE(delivered_via,'')
		FROM alerts WHERE created_at >= ? AND created_at < ? ORDER BY created_at DESC, id DESC LIMIT ?`, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Alert
	for rows.Next() {
		var a model.Alert
		var del int
		if err := rows.Scan(&a.ID, &a.CreatedAt, &a.RuleID, &a.UID, &a.Severity, &a.Message, &del, &a.DeliveredVia); err != nil {
			return nil, err
		}
		a.Delivered = del != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// AlertExists reports whether an alert of a rule already exists for a user in
// [from,to). The engine uses it to send at most one per rule per window.
func (d *DB) AlertExists(rule string, uid int, from, to int64) (bool, error) {
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM alerts WHERE rule_id = ? AND COALESCE(uid,0) = ? AND created_at >= ? AND created_at < ?`,
		rule, uid, from, to).Scan(&n)
	return n > 0, err
}

// ---- daemon state and collector health -----------------------------------

// SetState stores a small key/value used by the daemon (clean-shutdown flag, ...).
func (d *DB) SetState(key, value string) error {
	return d.Write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO daemon_state(key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
		return err
	})
}

// GetState reads a daemon_state value; ok is false when it is not set.
func (d *DB) GetState(key string) (string, bool, error) {
	var v string
	err := d.sql.QueryRow(`SELECT value FROM daemon_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// GetStateInt reads an integer daemon_state value, 0 when unset.
func (d *DB) GetStateInt(key string) int64 {
	v, ok, err := d.GetState(key)
	if err != nil || !ok {
		return 0
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

// CollectorHealth is one collector_health row.
type CollectorHealth struct {
	Collector string
	LastOKAt  int64
	LastError string
	Enabled   bool
}

// SetCollectorHealth records the latest outcome of a collector.
func (d *DB) SetCollectorHealth(h CollectorHealth) error {
	return d.Write(func(tx *sql.Tx) error {
		var ok any
		if h.LastOKAt != 0 {
			ok = h.LastOKAt
		}
		_, err := tx.Exec(`INSERT INTO collector_health(collector, last_ok_at, last_error, enabled) VALUES (?,?,?,?)
			ON CONFLICT(collector) DO UPDATE SET last_ok_at = COALESCE(excluded.last_ok_at, last_ok_at),
			last_error = excluded.last_error, enabled = excluded.enabled`, h.Collector, ok, h.LastError, boolInt(h.Enabled))
		return err
	})
}

// CollectorHealths lists all collector_health rows.
func (d *DB) CollectorHealths() ([]CollectorHealth, error) {
	rows, err := d.sql.Query(`SELECT collector, COALESCE(last_ok_at,0), COALESCE(last_error,''), enabled FROM collector_health ORDER BY collector`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CollectorHealth
	for rows.Next() {
		var h CollectorHealth
		var en int
		if err := rows.Scan(&h.Collector, &h.LastOKAt, &h.LastError, &en); err != nil {
			return nil, err
		}
		h.Enabled = en != 0
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---- aggregates ----------------------------------------------------------

// ReplaceHourly replaces a user's stored hourly state rows with hour_start in
// [from,to) and per-app rows with hour_start in [appFrom,to), in one
// transaction. The app range may start later than the state range: raw samples
// are pruned sooner than intervals, and per-app detail that can no longer be
// recomputed must be left alone. Re-running with the same input changes
// nothing (idempotent rollup, docs/04 §4.12).
func (d *DB) ReplaceHourly(uid int, from, appFrom, to int64, rows []model.HourRow, appRows []model.HourAppRow) error {
	return d.Write(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM hourly_usage WHERE uid = ? AND hour_start >= ? AND hour_start < ?`, uid, from, to); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM hourly_app_usage WHERE uid = ? AND hour_start >= ? AND hour_start < ?`, uid, appFrom, to); err != nil {
			return err
		}
		for _, r := range rows {
			if _, err := tx.Exec(`INSERT INTO hourly_usage(uid, hour_start, day, hour, hour_len_ms, active_ms, idle_ms, locked_ms, gap_ms)
				VALUES (?,?,?,?,?,?,?,?,?)`, r.UID, r.HourStart, r.Day, r.Hour, r.HourLenMs, r.ActiveMs, r.IdleMs, r.LockedMs, r.GapMs); err != nil {
				return err
			}
		}
		for _, r := range appRows {
			if _, err := tx.Exec(`INSERT INTO hourly_app_usage(uid, hour_start, app_id, app_name, category, foreground_ms) VALUES (?,?,?,?,?,?)`,
				r.UID, r.HourStart, r.AppID, r.AppName, r.Category, r.ForegroundMs); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceAppDays replaces a user's per-day app rows for the given days.
func (d *DB) ReplaceAppDays(uid int, days []string, rows []model.AppDay) error {
	return d.Write(func(tx *sql.Tx) error {
		for _, day := range days {
			if _, err := tx.Exec(`DELETE FROM app_usage WHERE uid = ? AND day = ?`, uid, day); err != nil {
				return err
			}
		}
		for _, r := range rows {
			if _, err := tx.Exec(`INSERT INTO app_usage(uid, day, app_id, app_name, category, foreground_ms, open_ms, session_count) VALUES (?,?,?,?,?,?,?,?)`,
				r.UID, r.Day, r.AppID, r.AppName, r.Category, r.ForegroundMs, r.OpenMs, r.SessionCount); err != nil {
				return err
			}
		}
		return nil
	})
}

// HourRows returns a user's stored hourly rows with hour_start in [from,to).
func (d *DB) HourRows(uid int, from, to int64) ([]model.HourRow, error) {
	rows, err := d.sql.Query(`SELECT uid, hour_start, day, hour, hour_len_ms, active_ms, idle_ms, locked_ms, gap_ms
		FROM hourly_usage WHERE uid = ? AND hour_start >= ? AND hour_start < ? ORDER BY hour_start`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.HourRow
	for rows.Next() {
		var r model.HourRow
		if err := rows.Scan(&r.UID, &r.HourStart, &r.Day, &r.Hour, &r.HourLenMs, &r.ActiveMs, &r.IdleMs, &r.LockedMs, &r.GapMs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// HourAppRows returns stored per-hour app rows with hour_start in [from,to).
func (d *DB) HourAppRows(uid int, from, to int64) ([]model.HourAppRow, error) {
	rows, err := d.sql.Query(`SELECT uid, hour_start, app_id, app_name, category, foreground_ms
		FROM hourly_app_usage WHERE uid = ? AND hour_start >= ? AND hour_start < ? ORDER BY hour_start, foreground_ms DESC`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.HourAppRow
	for rows.Next() {
		var r model.HourAppRow
		if err := rows.Scan(&r.UID, &r.HourStart, &r.AppID, &r.AppName, &r.Category, &r.ForegroundMs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AppDays returns stored per-day app rows for days in [fromDay,toDay] (YYYY-MM-DD).
func (d *DB) AppDays(uid int, fromDay, toDay string) ([]model.AppDay, error) {
	rows, err := d.sql.Query(`SELECT uid, day, app_id, app_name, category, foreground_ms, open_ms, session_count
		FROM app_usage WHERE uid = ? AND day >= ? AND day <= ? ORDER BY day, foreground_ms DESC`, uid, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AppDay
	for rows.Next() {
		var r model.AppDay
		if err := rows.Scan(&r.UID, &r.Day, &r.AppID, &r.AppName, &r.Category, &r.ForegroundMs, &r.OpenMs, &r.SessionCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// EarliestHourApp is the first hour for which per-app detail is stored (0 if none).
func (d *DB) EarliestHourApp(uid int) (int64, error) {
	var v sql.NullInt64
	if err := d.sql.QueryRow(`SELECT MIN(hour_start) FROM hourly_app_usage WHERE uid = ?`, uid).Scan(&v); err != nil {
		return 0, err
	}
	return v.Int64, nil
}

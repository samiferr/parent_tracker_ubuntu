// Package storage is the SQLite (WAL) persistence layer.
//
// The daemon is the single writer; the dashboard and CLI are readers. Every
// instant is stored as Unix milliseconds in UTC.
package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver: static binaries, easy arm64 builds
)

// DB wraps the database handle.
type DB struct {
	sql  *sql.DB
	path string
	wmu  sync.Mutex // serialises write transactions
}

// Open opens (creating if needed) the database at path and migrates it to the
// latest schema. The file is created with mode 0600.
func Open(path string) (*DB, error) { return OpenVersion(path, LatestVersion) }

// OpenVersion migrates only up to maxVersion. It exists so tests can build a
// database as an older release would have left it.
func OpenVersion(path string, maxVersion int) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		f.Close()
	}
	d, err := open(path, false)
	if err != nil {
		return nil, err
	}
	if err := d.migrate(maxVersion); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// OpenReadOnly opens an existing, already migrated database for reading.
func OpenReadOnly(path string) (*DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	d, err := open(path, true)
	if err != nil {
		return nil, err
	}
	if v := d.SchemaVersion(); v < LatestVersion {
		d.Close()
		return nil, fmt.Errorf("database schema is version %d, this build needs %d: start parent-trackerd once to migrate it", v, LatestVersion)
	}
	return d, nil
}

func open(path string, readOnly bool) (*DB, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(ON)")
	if !readOnly {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
		q.Set("_txlock", "immediate")
	} else {
		q.Set("mode", "ro")
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: strings.ReplaceAll(q.Encode(), "+", "%20")}).String()
	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s.SetMaxOpenConns(4)
	if err := s.Ping(); err != nil {
		s.Close()
		return nil, err
	}
	return &DB{sql: s, path: path}, nil
}

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }

// Path returns the database file path.
func (d *DB) Path() string { return d.path }

// SQL exposes the handle for read-only ad-hoc queries (tests, doctor).
func (d *DB) SQL() *sql.DB { return d.sql }

// Write runs fn in one write transaction. Writers are serialised, so a long
// read elsewhere never causes SQLITE_BUSY for the sampling path.
func (d *DB) Write(fn func(tx *sql.Tx) error) error {
	d.wmu.Lock()
	defer d.wmu.Unlock()
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SchemaVersion is the highest applied migration, or 0 for an empty database.
func (d *DB) SchemaVersion() int {
	var v sql.NullInt64
	if err := d.sql.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0
	}
	return int(v.Int64)
}

// Backup writes a consistent copy of the database to dst using VACUUM INTO.
func (d *DB) Backup(dst string) error {
	_ = os.Remove(dst)
	if _, err := d.sql.Exec(`VACUUM INTO ?`, dst); err != nil {
		return err
	}
	return os.Chmod(dst, 0o600)
}

// Checkpoint folds the WAL into the main file.
func (d *DB) Checkpoint() error {
	_, err := d.sql.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

type migration struct {
	version int
	sql     string
}

// LatestVersion is the schema version this build expects.
const LatestVersion = 2

// Migrations are additive (NFR-6.9): later versions only add tables or columns.
var migrations = []migration{
	{1, `
CREATE TABLE users (
  uid       INTEGER PRIMARY KEY,
  username  TEXT    NOT NULL,
  monitored INTEGER NOT NULL DEFAULT 1,
  added_at  INTEGER NOT NULL
);
CREATE TABLE sessions (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  uid               INTEGER NOT NULL,
  logind_session_id TEXT    NOT NULL,
  started_at        INTEGER NOT NULL,
  ended_at          INTEGER,
  state             TEXT    NOT NULL,
  host_type         TEXT    NOT NULL DEFAULT 'local',
  session_type      TEXT    NOT NULL DEFAULT 'unknown',
  desktop           TEXT,
  end_reason        TEXT
);
CREATE INDEX sessions_uid_started ON sessions(uid, started_at);
CREATE TABLE activity_intervals (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  uid         INTEGER NOT NULL,
  state       TEXT    NOT NULL,
  started_at  INTEGER NOT NULL,
  ended_at    INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  open        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX intervals_uid_start ON activity_intervals(uid, started_at);
CREATE INDEX intervals_uid_end   ON activity_intervals(uid, ended_at);
CREATE INDEX intervals_open      ON activity_intervals(open) WHERE open = 1;
CREATE TABLE app_usage (
  uid           INTEGER NOT NULL,
  day           TEXT    NOT NULL,
  app_id        TEXT    NOT NULL,
  app_name      TEXT    NOT NULL,
  category      TEXT    NOT NULL,
  foreground_ms INTEGER NOT NULL,
  open_ms       INTEGER NOT NULL,
  session_count INTEGER NOT NULL,
  PRIMARY KEY (uid, day, app_id)
);
CREATE TABLE window_samples (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  uid          INTEGER NOT NULL,
  ts           INTEGER NOT NULL,
  app_id       TEXT    NOT NULL,
  app_name     TEXT    NOT NULL DEFAULT '',
  window_title TEXT,
  pid          INTEGER NOT NULL DEFAULT 0,
  state        TEXT    NOT NULL,
  source       TEXT    NOT NULL
);
CREATE INDEX window_samples_uid_ts     ON window_samples(uid, ts);
CREATE INDEX window_samples_uid_app_ts ON window_samples(uid, app_id, ts);
CREATE TABLE alerts (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at    INTEGER NOT NULL,
  rule_id       TEXT    NOT NULL,
  uid           INTEGER,
  severity      TEXT    NOT NULL,
  message       TEXT    NOT NULL,
  delivered     INTEGER NOT NULL DEFAULT 0,
  delivered_via TEXT
);
CREATE INDEX alerts_created ON alerts(created_at);
CREATE TABLE audit_log (
  id     INTEGER PRIMARY KEY AUTOINCREMENT,
  ts     INTEGER NOT NULL,
  actor  TEXT    NOT NULL,
  action TEXT    NOT NULL,
  detail TEXT
);
CREATE INDEX audit_ts ON audit_log(ts);
CREATE TABLE collector_health (
  collector  TEXT PRIMARY KEY,
  last_ok_at INTEGER,
  last_error TEXT,
  enabled    INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE tracking_gaps (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  uid        INTEGER NOT NULL DEFAULT 0,
  started_at INTEGER NOT NULL,
  ended_at   INTEGER NOT NULL,
  kind       TEXT    NOT NULL,
  detail     TEXT
);
CREATE INDEX gaps_start ON tracking_gaps(started_at);
CREATE TABLE daemon_state (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`},
	{2, `
CREATE TABLE hourly_usage (
  uid         INTEGER NOT NULL,
  hour_start  INTEGER NOT NULL,
  day         TEXT    NOT NULL,
  hour        INTEGER NOT NULL,
  hour_len_ms INTEGER NOT NULL,
  active_ms   INTEGER NOT NULL,
  idle_ms     INTEGER NOT NULL,
  locked_ms   INTEGER NOT NULL,
  gap_ms      INTEGER NOT NULL,
  PRIMARY KEY (uid, hour_start)
);
CREATE INDEX hourly_usage_day ON hourly_usage(uid, day);
CREATE TABLE hourly_app_usage (
  uid           INTEGER NOT NULL,
  hour_start    INTEGER NOT NULL,
  app_id        TEXT    NOT NULL,
  app_name      TEXT    NOT NULL,
  category      TEXT    NOT NULL,
  foreground_ms INTEGER NOT NULL,
  PRIMARY KEY (uid, hour_start, app_id)
);
CREATE INDEX hourly_app_usage_hour ON hourly_app_usage(uid, hour_start);
INSERT INTO daemon_state(key, value) VALUES ('hourly_backfill_pending', '1')
  ON CONFLICT(key) DO UPDATE SET value = '1';
`},
}

func (d *DB) migrate(max int) error {
	if _, err := d.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	cur := d.SchemaVersion()
	if cur > LatestVersion {
		return fmt.Errorf("database schema is version %d but this build only understands up to %d: refusing to downgrade", cur, LatestVersion)
	}
	var todo []migration
	for _, m := range migrations {
		if m.version > cur && m.version <= max {
			todo = append(todo, m)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	if cur > 0 { // upgrading an existing database: keep a copy first (NFR-6.4)
		bak := fmt.Sprintf("%s.bak-v%d", d.path, cur)
		if err := d.Backup(bak); err != nil {
			return fmt.Errorf("back up database before migrating: %w", err)
		}
	}
	for _, m := range todo {
		err := d.Write(func(tx *sql.Tx) error {
			if _, err := tx.Exec(m.sql); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, m.version, time.Now().UnixMilli())
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
	}
	return nil
}

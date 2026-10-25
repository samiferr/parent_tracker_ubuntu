package auditlog

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

type fixture struct {
	db  *storage.DB
	now time.Time
	log *slog.Logger
	buf *bytes.Buffer
}

func newFixture(t *testing.T, level slog.Level) *fixture {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &fixture{db: db, now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), buf: &bytes.Buffer{}}
	inner := slog.NewTextHandler(f.buf, &slog.HandlerOptions{Level: level})
	f.log = slog.New(Wrap(inner, db, func() time.Time { return f.now }))
	return f
}

func (f *fixture) rows(t *testing.T) []model.AuditEvent {
	t.Helper()
	ev, err := f.db.AuditEvents(0, 1<<60, 100)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestWarningsAndErrorsAreRecorded(t *testing.T) {
	f := newFixture(t, slog.LevelInfo)
	f.log.Info("dashboard listening", "addr", "127.0.0.1:8787")
	f.log.Warn("checkpoint failed", "err", errors.New("disk I/O error"))
	f.log.With("component", "rollup").Error("rollup failed", "err", "database is locked")

	ev := f.rows(t)
	if len(ev) != 2 {
		t.Fatalf("want the warning and the error only, got %+v", ev)
	}
	// newest first
	if ev[0].Action != model.AuditError || ev[0].Detail != "rollup failed: component=rollup, err=database is locked" {
		t.Fatalf("error row = %+v", ev[0])
	}
	if ev[1].Action != model.AuditWarning || ev[1].Detail != "checkpoint failed: err=disk I/O error" || ev[1].Actor != "system" {
		t.Fatalf("warning row = %+v", ev[1])
	}
	if !strings.Contains(f.buf.String(), "dashboard listening") || !strings.Contains(f.buf.String(), "rollup failed") {
		t.Fatalf("the journal must still get the records: %q", f.buf.String())
	}
}

func TestRecordedEvenWhenTheJournalLevelHidesThem(t *testing.T) {
	f := newFixture(t, slog.LevelError) // log_level = "error"
	f.log.Warn("agent socket stopped soon")
	if len(f.rows(t)) != 1 {
		t.Fatal("a warning the journal hides must still reach the audit log")
	}
	if strings.Contains(f.buf.String(), "agent socket") {
		t.Fatalf("a hidden warning must stay out of the journal: %q", f.buf.String())
	}
}

func TestIdenticalEntriesAreStoredOncePerQuietPeriod(t *testing.T) {
	f := newFixture(t, slog.LevelInfo)
	for i := 0; i < 5; i++ {
		f.log.Error("rollup failed", "err", "boom")
		f.now = f.now.Add(time.Minute)
	}
	if n := len(f.rows(t)); n != 1 {
		t.Fatalf("a repeating failure stored %d times in 5 minutes, want 1", n)
	}
	f.log.Error("rollup failed", "err", "a different reason")
	if n := len(f.rows(t)); n != 2 {
		t.Fatalf("a different message must be stored, got %d rows", n)
	}
	f.now = f.now.Add(Quiet)
	f.log.Error("rollup failed", "err", "boom")
	if n := len(f.rows(t)); n != 3 {
		t.Fatalf("after the quiet period the failure is stored again, got %d rows", n)
	}
}

func TestEntryIsOneBoundedLine(t *testing.T) {
	f := newFixture(t, slog.LevelInfo)
	f.log.Error("bad\ninput", "err", strings.Repeat("é", 1000))
	d := f.rows(t)[0].Detail
	if strings.ContainsAny(d, "\n\r") {
		t.Fatalf("detail spans lines: %q", d)
	}
	if len(d) > maxDetail+len("…") || !strings.HasSuffix(d, "…") {
		t.Fatalf("detail not clipped: %d bytes", len(d))
	}
}

func TestLevels(t *testing.T) {
	for action, want := range map[string]string{
		model.AuditError: model.LevelError, model.AuditSensorLost: model.LevelError, model.AuditSensorUnavailable: model.LevelError,
		model.AuditWarning: model.LevelWarning, model.AuditClockSkew: model.LevelWarning, model.AuditDataDeleted: model.LevelWarning,
		model.AuditServiceStarted: model.LevelInfo, model.AuditSensorRestored: model.LevelInfo, "something_new": model.LevelInfo,
	} {
		if got := model.AuditLevel(action); got != want {
			t.Errorf("AuditLevel(%q) = %q, want %q", action, got, want)
		}
	}
}

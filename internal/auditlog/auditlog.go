// Package auditlog copies what the service logs as a warning or an error into
// the audit log, so the dashboard's Audit log shows failures (a rollup that
// failed, a socket that stopped, a database that could not be written) without
// the parent opening the journal.
//
// It wraps a slog.Handler. Everything still goes to the journal as before; only
// records at warning level or above are also stored, and the same message is
// stored at most once every Quiet, so a failure that repeats every few seconds
// does not fill the table.
package auditlog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

// Quiet is how long an identical entry is not stored again.
const Quiet = 15 * time.Minute

// maxDetail bounds one stored entry.
const maxDetail = 500

// maxSeen bounds the memory used to remember recent entries.
const maxSeen = 256

type sink struct {
	db  *storage.DB
	now func() time.Time

	mu   sync.Mutex
	seen map[string]time.Time
}

// Handler is a slog.Handler that also records warnings and errors.
type Handler struct {
	inner  slog.Handler
	sink   *sink
	attrs  []slog.Attr
	groups []string
}

// Wrap returns a handler that passes everything to inner and records warnings
// and errors in db's audit log. A nil now means time.Now.
func Wrap(inner slog.Handler, db *storage.DB, now func() time.Time) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{inner: inner, sink: &sink{db: db, now: now, seen: map[string]time.Time{}}}
}

// Enabled is true for warnings and errors whatever the journal's level is: the
// Audit log keeps them even when log_level is "error" or "warn" hides them.
func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelWarn || h.inner.Enabled(ctx, l)
}

// Handle writes the record to the journal if its level allows, then records it.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	if h.inner.Enabled(ctx, r.Level) {
		err = h.inner.Handle(ctx, r)
	}
	if r.Level >= slog.LevelWarn {
		h.sink.record(r.Level, h.describe(r))
	}
	return err
}

// WithAttrs returns a handler with the attributes added.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.inner = h.inner.WithAttrs(attrs)
	c.attrs = append(append([]slog.Attr(nil), h.attrs...), qualify(h.groups, attrs)...)
	return &c
}

// WithGroup returns a handler that nests later attributes under name.
func (h *Handler) WithGroup(name string) slog.Handler {
	c := *h
	c.inner = h.inner.WithGroup(name)
	if name != "" {
		c.groups = append(append([]string(nil), h.groups...), name)
	}
	return &c
}

func qualify(groups []string, attrs []slog.Attr) []slog.Attr {
	if len(groups) == 0 {
		return attrs
	}
	prefix := strings.Join(groups, ".") + "."
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = slog.Attr{Key: prefix + a.Key, Value: a.Value}
	}
	return out
}

// describe renders "message: key=value, key=value" on one line.
func (h *Handler) describe(r slog.Record) string {
	var b strings.Builder
	b.WriteString(r.Message)
	sep := ": "
	add := func(a slog.Attr) {
		if a.Equal(slog.Attr{}) {
			return
		}
		fmt.Fprintf(&b, "%s%s=%s", sep, a.Key, a.Value.Resolve().String())
		sep = ", "
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		add(qualify(h.groups, []slog.Attr{a})[0])
		return true
	})
	return oneLine(b.String(), maxDetail)
}

// oneLine collapses white space (an error text may hold new lines) and clips.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(strings.ToValidUTF8(s, "")), " ")
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 { // do not cut a UTF-8 sequence in two
		n--
	}
	return s[:n] + "…"
}

func (s *sink) record(l slog.Level, detail string) {
	action := model.AuditWarning
	if l >= slog.LevelError {
		action = model.AuditError
	}
	now := s.now()
	key := action + "|" + detail

	s.mu.Lock()
	if last, ok := s.seen[key]; ok && now.Sub(last) < Quiet {
		s.mu.Unlock()
		return
	}
	if len(s.seen) >= maxSeen {
		for k, t := range s.seen {
			if now.Sub(t) >= Quiet {
				delete(s.seen, k)
			}
		}
		if len(s.seen) >= maxSeen { // a flood of different messages: start over
			s.seen = map[string]time.Time{}
		}
	}
	s.seen[key] = now
	s.mu.Unlock()

	// A failure to store the entry cannot be reported anywhere better, and
	// logging it here would loop.
	_ = s.db.Audit(now.UnixMilli(), "system", action, detail)
}

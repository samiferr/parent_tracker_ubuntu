package report

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

const apiKey = "re_test_SECRET_123"

func TestResendSend(t *testing.T) {
	var got struct {
		auth, idem, ctype string
		body              Email
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth, got.idem, got.ctype = r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.Write([]byte(`{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`))
	}))
	defer ts.Close()
	c := &Resend{Endpoint: ts.URL}
	id, err := c.Send(context.Background(), apiKey, "daily-report/2026-11-23", Email{
		From: "PT <r@example.com>", To: []string{"a@example.com", "b@example.com"}, Subject: "S", HTML: "<p>x</p>", Text: "x"})
	if err != nil || id != "49a3999c-0ce1-4ea6-ab68-afcd6dc2e794" {
		t.Fatalf("send = %q, %v", id, err)
	}
	if got.auth != "Bearer "+apiKey || got.idem != "daily-report/2026-11-23" || got.ctype != "application/json" {
		t.Fatalf("headers = %+v", got)
	}
	if len(got.body.To) != 2 || got.body.From != "PT <r@example.com>" || got.body.HTML != "<p>x</p>" || got.body.Text != "x" {
		t.Fatalf("body = %+v", got.body)
	}
}

func TestResendErrors(t *testing.T) {
	status, body := 0, ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	defer ts.Close()
	c := &Resend{Endpoint: ts.URL}
	send := func() error {
		_, err := c.Send(context.Background(), apiKey, "", Email{From: "r@example.com", To: []string{"a@example.com"}})
		return err
	}

	status, body = 403, `{"statusCode":403,"name":"validation_error","message":"The example.com domain is not verified."}`
	err := send()
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "domain is not verified") {
		t.Fatalf("403 = %v", err)
	}
	status, body = 401, `{"message":"API key `+apiKey+` is invalid"}`
	if err = send(); strings.Contains(err.Error(), apiKey) {
		t.Fatalf("the API key leaked into an error: %v", err)
	}
	status, body = 429, `{"message":"Too many requests"}`
	if err = send(); err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("429 should be retryable: %v", err)
	}
	status, body = 500, ``
	if err = send(); err == nil || errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "500") {
		t.Fatalf("500 = %v", err)
	}
	if _, err := c.Send(context.Background(), "", "", Email{}); err == nil {
		t.Fatal("no key must be an error")
	}
	ts.Close()
	if err = send(); err == nil || strings.Contains(err.Error(), apiKey) {
		t.Fatalf("unreachable = %v", err)
	}
}

// fixture is a database with one monitored user and a day of activity.
type fixture struct {
	db  *storage.DB
	cfg *config.Config
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Default()
	cfg.MonitoredUsers = []string{"ilyes", "sara"}
	cfg.DisclosureAcknowledged = "2026-10-01T00:00:00Z"
	cfg.Report = config.Report{Enabled: true, SendAt: "07:00", ResendAPIKey: apiKey,
		From: "Parent Tracker <reports@example.com>", Recipients: []string{"mum@example.com", "dad@example.com"}}
	db.UpsertUser(1000, "ilyes", true, 1)
	db.UpsertUser(1001, "sara", true, 1)
	db.UpsertUser(1002, "parent", false, 1)
	return &fixture{db: db, cfg: cfg, now: time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)}
}

func (f *fixture) seed(day time.Time) {
	at := func(h, m int) int64 {
		return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute).UnixMilli()
	}
	id, _ := f.db.OpenInterval(1000, model.StateActive, at(16, 0))
	f.db.CloseInterval(id, at(17, 30))
	var ss []model.Sample
	for ts := at(16, 0); ts < at(17, 30); ts += 5000 {
		app := "firefox"
		if ts >= at(17, 0) {
			app = "minecraft"
		}
		ss = append(ss, model.Sample{UID: 1000, TS: ts, AppID: app, State: model.StateActive, Source: "x11", Title: "secret window title"})
	}
	f.db.InsertSamples(ss, map[string]string{"firefox": "Firefox <Web> Browser"})
	f.db.InsertAlert(model.Alert{CreatedAt: at(17, 10), RuleID: "daily_limit", UID: 1000, Severity: "warn", Message: "ilyes reached the daily limit."})
	f.db.InsertAlert(model.Alert{CreatedAt: at(9, 0), RuleID: "disk_low", UID: 0, Severity: "critical", Message: "Disk space is low."})
}

func TestBuildAndRender(t *testing.T) {
	f := newFixture(t)
	f.seed(time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC))
	rep, err := Build(f.db, f.cfg, time.UTC, func() time.Time { return f.now }, "2026-11-24", "kids-pc")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Users) != 2 || rep.Users[0].Name != "ilyes" || rep.Users[1].Name != "sara" {
		t.Fatalf("users = %+v", rep.Users)
	}
	u := rep.Users[0]
	if u.Active != "1h 30m" || u.First != "16:00" || u.Last != "17:30" || len(u.Apps) != 2 || len(u.Alerts) != 1 || !rep.Users[1].NoData {
		t.Fatalf("ilyes = %+v", u)
	}
	if len(u.Hours) != 24 || u.Hours[16].Minutes != 60 || u.Hours[17].Minutes != 30 || u.Hours[3].Minutes != 0 {
		t.Fatalf("hours = %+v", u.Hours)
	}
	if len(rep.System) != 1 || rep.System[0].Message != "Disk space is low." {
		t.Fatalf("system = %+v", rep.System)
	}
	if s := rep.Subject(); s != "Daily report, Tue 24 Nov: ilyes 1h 30m, sara 0m (kids-pc)" {
		t.Fatalf("subject = %q", s)
	}

	html, err := rep.HTML()
	if err != nil {
		t.Fatal(err)
	}
	text, err := rep.Text()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Tuesday 24 November 2026", "ilyes", "1h 30m", "Firefox &lt;Web&gt; Browser", "minecraft", "games",
		"ilyes reached the daily limit.", "Disk space is low.", "Nothing was recorded on this day."} {
		if !strings.Contains(html, want) {
			t.Errorf("html lacks %q", want)
		}
	}
	for _, want := range []string{"== ilyes ==", "Active 1h 30m", "Firefox <Web> Browser", "17:10 ilyes reached the daily limit.", "== sara =="} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q", want)
		}
	}
	for _, body := range []string{html, text} {
		if strings.Contains(body, "secret window title") || strings.Contains(body, "parent") && strings.Contains(body, "== parent") {
			t.Fatal("the report must hold neither window titles nor unmonitored users")
		}
	}
	if strings.Contains(html, "ZgotmplZ") {
		t.Fatal("a style value was rejected by html/template")
	}
	if dir := os.Getenv("REPORT_SAMPLE_DIR"); dir != "" {
		os.WriteFile(filepath.Join(dir, "report.html"), []byte(html), 0o644)
		os.WriteFile(filepath.Join(dir, "report.txt"), []byte(text), 0o644)
	}
	if strings.Contains(html, "<Web>") {
		t.Fatal("application names must be escaped")
	}

	if _, err := Build(f.db, f.cfg, time.UTC, nil, "24/11/2026", ""); err == nil {
		t.Fatal("a bad date must be an error")
	}
	f.cfg.DisclosureAcknowledged = ""
	rep, _ = Build(f.db, f.cfg, time.UTC, func() time.Time { return f.now }, "2026-11-24", "")
	if len(rep.Notes) != 1 || !strings.Contains(rep.Notes[0], "disclosure") {
		t.Fatalf("notes = %v", rep.Notes)
	}
}

type fakeMailer struct {
	mu    sync.Mutex
	sent  []Email
	keys  []string
	idems []string
	err   error
}

func (m *fakeMailer) Send(_ context.Context, key, idem string, e Email) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return "", m.err
	}
	m.sent = append(m.sent, e)
	m.keys = append(m.keys, key)
	m.idems = append(m.idems, idem)
	return "id", nil
}

func (m *fakeMailer) count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.sent) }

func TestSchedulerSendsYesterdayOnceAfterSendAt(t *testing.T) {
	f := newFixture(t)
	f.seed(time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC))
	now := time.Date(2026, 11, 24, 6, 59, 0, 0, time.UTC)
	m := &fakeMailer{}
	s := &Scheduler{DB: f.db, Cfg: func() *config.Config { return f.cfg }, Loc: time.UTC, Now: func() time.Time { return now }, Mailer: m, Host: "pc"}
	ctx := context.Background()

	s.Tick(ctx)
	if m.count() != 0 {
		t.Fatal("nothing is sent before report.send_at")
	}
	now = now.Add(time.Minute) // 07:00
	s.Tick(ctx)
	if m.count() != 1 {
		t.Fatalf("sent = %d", m.count())
	}
	e := m.sent[0]
	if len(e.To) != 2 || e.From != "Parent Tracker <reports@example.com>" || !strings.Contains(e.Subject, "Mon 23 Nov") ||
		m.keys[0] != apiKey || m.idems[0] != "daily-report/2026-11-23" || !strings.Contains(e.HTML, "Monday 23 November 2026") {
		t.Fatalf("email = %+v %v %v", e.Subject, m.keys, m.idems)
	}
	if v, _, _ := f.db.GetState(stateLastDay); v != "2026-11-23" {
		t.Fatalf("last day = %q", v)
	}
	ev, _ := f.db.AuditEvents(0, 1<<60, 10)
	if len(ev) != 1 || ev[0].Action != model.AuditReportSent || !strings.Contains(ev[0].Detail, "2 recipients") || strings.Contains(ev[0].Detail, "mum@") {
		t.Fatalf("audit = %+v", ev)
	}

	// later the same day, and after a restart (a new scheduler), nothing more
	now = now.Add(5 * time.Hour)
	s.Tick(ctx)
	s2 := &Scheduler{DB: f.db, Cfg: func() *config.Config { return f.cfg }, Loc: time.UTC, Now: func() time.Time { return now }, Mailer: m}
	s2.Tick(ctx)
	if m.count() != 1 {
		t.Fatalf("sent twice: %d", m.count())
	}
	// the next morning sends the next day
	now = time.Date(2026, 11, 25, 7, 30, 0, 0, time.UTC)
	s.Tick(ctx)
	if m.count() != 2 || m.idems[1] != "daily-report/2026-11-24" {
		t.Fatalf("next day: %d %v", m.count(), m.idems)
	}
	// switched off: nothing
	f.cfg.Report.Enabled = false
	now = time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	s.Tick(ctx)
	if m.count() != 2 {
		t.Fatal("a disabled report must not be sent")
	}
}

func TestSchedulerRetriesAndGivesUp(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 11, 24, 7, 0, 0, 0, time.UTC)
	m := &fakeMailer{err: errors.New("network down")}
	s := &Scheduler{DB: f.db, Cfg: func() *config.Config { return f.cfg }, Loc: time.UTC, Now: func() time.Time { return now }, Mailer: m}
	ctx := context.Background()

	s.Tick(ctx) // fails
	m.err = nil
	now = now.Add(5 * time.Minute)
	s.Tick(ctx)
	if m.count() != 0 {
		t.Fatal("a retry waits 15 minutes")
	}
	now = now.Add(10 * time.Minute)
	s.Tick(ctx)
	if m.count() != 1 {
		t.Fatalf("retry after the wait: %d", m.count())
	}

	// a rejection (bad key, unverified domain) is not retried until the
	// settings change (Reset is called by a reload)
	f.db.SetState(stateLastDay, "")
	m.err = ErrRejected
	now = time.Date(2026, 11, 25, 7, 0, 0, 0, time.UTC)
	s.Tick(ctx)
	m.err = nil
	now = now.Add(time.Hour)
	s.Tick(ctx)
	if m.count() != 1 {
		t.Fatal("a rejected report must not be retried")
	}
	s.Reset()
	s.Tick(ctx)
	if m.count() != 2 {
		t.Fatal("after a reload the report is tried again")
	}

	// incomplete settings: nothing is sent, nothing crashes
	f.cfg.Report.Recipients = nil
	now = time.Date(2026, 11, 26, 7, 0, 0, 0, time.UTC)
	s.Tick(ctx)
	if m.count() != 2 {
		t.Fatal("no recipients, no e-mail")
	}
	if _, err := s.Send(ctx, "2026-11-25", ""); err == nil || !strings.Contains(err.Error(), "recipients") {
		t.Fatalf("send without recipients = %v", err)
	}
}

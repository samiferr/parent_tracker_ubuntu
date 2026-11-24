package report

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
)

// stateLastDay is the daemon_state key holding the last date whose report was
// sent, so a restart never sends it twice.
const stateLastDay = "report_last_day"

// Retry policy for the scheduled report: a failed send is retried every
// retryEvery, at most maxTries times per day.
const (
	retryEvery = 15 * time.Minute
	maxTries   = 8
)

// Mailer sends one message; *Resend implements it.
type Mailer interface {
	Send(ctx context.Context, apiKey, idempotencyKey string, m Email) (string, error)
}

// Scheduler sends the daily report and on-demand test reports.
type Scheduler struct {
	DB     *storage.DB
	Cfg    func() *config.Config
	Loc    *time.Location
	Now    func() time.Time
	Log    *slog.Logger
	Mailer Mailer // nil: Resend's API
	Host   string // shown in the report; empty: the host name

	mu      sync.Mutex
	tries   map[string]int // date -> failed attempts
	nextTry time.Time
	sending sync.Mutex
}

func (s *Scheduler) init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tries == nil {
		s.tries = map[string]int{}
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Loc == nil {
		s.Loc = time.Local
	}
	if s.Log == nil {
		s.Log = slog.Default()
	}
	if s.Mailer == nil {
		s.Mailer = &Resend{}
	}
	if s.Host == "" {
		s.Host, _ = os.Hostname()
	}
}

// Run checks once a minute whether the report is due, until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	s.init()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Due returns the date whose report should be sent now, or "" when nothing is
// due: the previous local day, once the local time has passed report.send_at
// and that day's report has not been sent yet.
func (s *Scheduler) Due() string {
	s.init()
	cfg := s.Cfg()
	if !cfg.Report.Enabled {
		return ""
	}
	at, err := config.ParseClock(cfg.Report.SendAt)
	if err != nil {
		return ""
	}
	now := s.Now().In(s.Loc)
	if now.Hour()*60+now.Minute() < at {
		return ""
	}
	ds, _ := aggregate.DayBounds(s.Loc, now.UnixMilli())
	yesterday := aggregate.DayString(s.Loc, ds-1)
	if last, ok, _ := s.DB.GetState(stateLastDay); ok && last >= yesterday {
		return ""
	}
	return yesterday
}

// Tick sends the scheduled report when it is due, retrying failures later.
func (s *Scheduler) Tick(ctx context.Context) {
	day := s.Due()
	if day == "" {
		return
	}
	cfg := s.Cfg()
	s.mu.Lock()
	tries, wait := s.tries[day], s.Now().Before(s.nextTry)
	s.mu.Unlock()
	if tries >= maxTries || wait {
		return
	}
	if p := cfg.ReportProblems(); len(p) > 0 {
		s.fail(day, cfg, fmt.Errorf("the daily report is on but cannot be sent: %s", strings.Join(p, "; ")), true)
		return
	}
	n, err := s.Send(ctx, day, "daily-report/"+day)
	if err != nil {
		s.fail(day, cfg, err, errors.Is(err, ErrRejected))
		return
	}
	_ = s.DB.SetState(stateLastDay, day)
	s.mu.Lock()
	delete(s.tries, day)
	s.mu.Unlock()
	s.Log.Info("daily report sent", "date", day, "recipients", n)
}

// fail logs a failed scheduled send once per attempt; final gives up on the
// day (the configuration must change first).
func (s *Scheduler) fail(day string, cfg *config.Config, err error, final bool) {
	s.mu.Lock()
	s.tries[day]++
	if final {
		s.tries[day] = maxTries
	}
	n := s.tries[day]
	s.nextTry = s.Now().Add(retryEvery)
	s.mu.Unlock()
	if n >= maxTries {
		s.Log.Error("the daily report was not sent; giving up for this day", "date", day, "err", err.Error())
		return
	}
	s.Log.Warn("the daily report was not sent; retrying in 15 minutes", "date", day, "attempt", n, "err", err.Error())
}

// Reset forgets failed attempts, so a changed configuration is tried at once.
func (s *Scheduler) Reset() {
	s.init()
	s.mu.Lock()
	s.tries = map[string]int{}
	s.nextTry = time.Time{}
	s.mu.Unlock()
}

// Send builds the report for date and e-mails it to every recipient now. It
// returns the number of recipients. The send is recorded in the audit log.
func (s *Scheduler) Send(ctx context.Context, date, idempotencyKey string) (int, error) {
	s.init()
	s.sending.Lock()
	defer s.sending.Unlock()
	cfg := s.Cfg()
	if p := cfg.ReportProblems(); len(p) > 0 {
		return 0, fmt.Errorf("the report cannot be sent: %s", strings.Join(p, "; "))
	}
	rep, err := Build(s.DB, cfg, s.Loc, s.Now, date, s.Host)
	if err != nil {
		return 0, err
	}
	html, err := rep.HTML()
	if err != nil {
		return 0, err
	}
	text, err := rep.Text()
	if err != nil {
		return 0, err
	}
	to := append([]string(nil), cfg.Report.Recipients...)
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if _, err := s.Mailer.Send(ctx, cfg.Report.ResendAPIKey, idempotencyKey, Email{
		From: cfg.Report.From, To: to, Subject: rep.Subject(), HTML: html, Text: text,
	}); err != nil {
		return 0, err
	}
	_ = s.DB.Audit(s.Now().UnixMilli(), "service", model.AuditReportSent,
		fmt.Sprintf("daily report for %s sent to %d recipient%s", date, len(to), plural(len(to))))
	return len(to), nil
}

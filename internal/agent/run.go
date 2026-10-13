package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

// ErrNotMonitored is returned when the daemon says this user is not monitored
// (or recording is not enabled). The agent then exits having collected nothing
// (NFR-5.3).
type ErrNotMonitored struct{ Reason string }

func (e ErrNotMonitored) Error() string { return "not monitored: " + e.Reason }

// Options configures Run.
type Options struct {
	Socket      string
	Log         *slog.Logger
	NewProvider func(model.Provider) Provider
	SessionHint string // $XDG_SESSION_ID
	SessionType string // $XDG_SESSION_TYPE (cross-check only)
	// Backoff returns the wait before reconnect attempt n (0-based).
	Backoff func(n int) time.Duration
}

func (o *Options) log() *slog.Logger {
	if o.Log != nil {
		return o.Log
	}
	return slog.Default()
}

// DefaultBackoff doubles from 1 s up to 30 s.
func DefaultBackoff(n int) time.Duration {
	d := time.Second << uint(min(n, 5))
	return min(d, 30*time.Second)
}

// Run keeps a connection to the daemon and reports until ctx ends. It returns
// ErrNotMonitored if the daemon refuses this user.
func Run(ctx context.Context, o Options) error {
	if o.Backoff == nil {
		o.Backoff = DefaultBackoff
	}
	for attempt := 0; ; attempt++ {
		err := runOnce(ctx, &o)
		var nm ErrNotMonitored
		if errors.As(err, &nm) {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		o.log().Info("disconnected from the daemon; will retry", "err", err)
		if err == nil {
			attempt = 0 // we were connected for a while
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(o.Backoff(attempt)):
		}
	}
}

func runOnce(ctx context.Context, o *Options) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.DialContext(ctx, "unix", o.Socket)
	if err != nil {
		return err
	}
	defer c.Close()
	enc := json.NewEncoder(c)
	if err := enc.Encode(agentproto.Hello{Type: "hello", Version: agentproto.Version, SessionHint: o.SessionHint, SessionType: o.SessionType}); err != nil {
		return err
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 4096), agentproto.MaxLine)
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if !sc.Scan() {
		return fmt.Errorf("no welcome from the daemon")
	}
	_ = c.SetReadDeadline(time.Time{})
	var w agentproto.Welcome
	if err := json.Unmarshal(sc.Bytes(), &w); err != nil || agentproto.Kind(sc.Bytes()) != "welcome" {
		return fmt.Errorf("unexpected reply from the daemon")
	}
	if !w.Monitored {
		return ErrNotMonitored{Reason: w.Reason}
	}
	o.log().Info("connected to the daemon", "session", w.SessionID, "provider", w.Provider, "interval_ms", w.IntervalMs,
		"display", os.Getenv("DISPLAY"), "session_type", o.SessionType)

	var (
		mu       sync.Mutex
		provName = model.Provider(w.Provider)
		titles   = w.Title
		interval = time.Duration(w.IntervalMs) * time.Millisecond
		reconf   = make(chan struct{}, 1)
	)
	if interval < time.Second {
		interval = 5 * time.Second
	}
	connClosed := make(chan struct{})
	go func() { // the daemon pushes new settings on reload
		defer close(connClosed)
		for sc.Scan() {
			if agentproto.Kind(sc.Bytes()) != "config" {
				continue
			}
			var cf agentproto.Config
			if json.Unmarshal(sc.Bytes(), &cf) != nil {
				continue
			}
			mu.Lock()
			provName, titles, interval = model.Provider(cf.Provider), cf.Title, time.Duration(cf.IntervalMs)*time.Millisecond
			if interval < time.Second {
				interval = 5 * time.Second
			}
			mu.Unlock()
			select {
			case reconf <- struct{}{}:
			default:
			}
		}
	}()

	var prov Provider
	var provFor model.Provider
	var lastStatus, lastDetail string
	defer func() {
		if prov != nil {
			prov.Close()
		}
	}()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-connClosed:
			return nil
		case <-reconf:
		case <-timer.C:
		}
		mu.Lock()
		want, tt, iv := provName, titles, interval
		mu.Unlock()
		timer.Reset(iv)

		if want == model.ProviderNone || want == "" {
			if prov != nil {
				prov.Close()
				prov, provFor = nil, ""
			}
			continue // idle: nothing to collect in this session
		}
		if prov == nil || provFor != want {
			if prov != nil {
				prov.Close()
			}
			if prov = o.NewProvider(want); prov == nil {
				return fmt.Errorf("no provider for %q", want)
			}
			provFor = want
		}
		rep := prov.Poll(ctx, tt)
		rep.Type = "report"
		if rep.Status != lastStatus || rep.Detail != lastDetail {
			// Only changes are logged: this is how a sensor that never worked is
			// told apart from one that stopped, in `journalctl --user -u parent-tracker-agent`.
			if rep.Status == agentproto.StatusOK {
				o.log().Info("foreground sensor is working", "provider", rep.Provider)
			} else {
				o.log().Warn("foreground sensor is not working", "provider", rep.Provider, "status", rep.Status, "detail", rep.Detail)
			}
			lastStatus, lastDetail = rep.Status, rep.Detail
		}
		if err := enc.Encode(rep); err != nil {
			return err
		}
	}
}

// Package agentsrv is the daemon's end of the agent socket (docs/04 §4.5).
//
// The agent runs as the monitored user, so it is untrusted: the daemon learns
// who it is from SO_PEERCRED, decides which of that user's sessions it serves,
// tells it which provider to run, and ignores anything it sends that does not
// match (SEC-13, SEC-17, FR-3.9).
package agentsrv

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/peercred"
)

// Server accepts agent connections.
type Server struct {
	Src collector.Source
	Cfg func() *config.Config
	Log *slog.Logger
	Now func() time.Time
	// Cred overrides peer identification in tests.
	Cred func(net.Conn) (peercred.Cred, error)

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	rep  agentproto.Report
	recv time.Time
	conn *agentConn
}

type agentConn struct {
	c        net.Conn
	enc      *json.Encoder
	wmu      sync.Mutex
	session  string
	provider model.Provider
	uid      int
}

func (a *agentConn) send(v any) error {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	_ = a.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return a.enc.Encode(v)
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Latest returns the most recent report of the agent of a session.
func (s *Server) Latest(sessionID string) (agentproto.Report, time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[sessionID]
	if e == nil || e.recv.IsZero() { // connected but nothing reported yet
		return agentproto.Report{}, time.Time{}, false
	}
	return e.rep, e.recv, true
}

// Connected reports whether an agent is currently connected for a session.
func (s *Server) Connected(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[sessionID]
	return e != nil && e.conn != nil
}

// Serve accepts connections until ctx ends. When it ends, every open agent
// connection is closed too, so agents notice the daemon went away and reconnect
// when it is back.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	var (
		cmu   sync.Mutex
		conns = map[net.Conn]struct{}{}
	)
	go func() {
		<-ctx.Done()
		ln.Close()
		cmu.Lock()
		for c := range conns {
			c.Close()
		}
		cmu.Unlock()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		cmu.Lock()
		conns[c] = struct{}{}
		cmu.Unlock()
		go func() {
			defer func() {
				cmu.Lock()
				delete(conns, c)
				cmu.Unlock()
			}()
			s.handle(ctx, c)
		}()
	}
}

// Reconfigure pushes the current settings to every connected agent (SIGHUP).
func (s *Server) Reconfigure(ctx context.Context) {
	cfg := s.Cfg()
	infos, _ := s.Src.Sessions(ctx)
	byID := map[string]collector.SessionInfo{}
	for _, i := range infos {
		byID[i.ID] = i
	}
	s.mu.Lock()
	var conns []*agentConn
	for _, e := range s.entries {
		if e.conn != nil {
			conns = append(conns, e.conn)
		}
	}
	s.mu.Unlock()
	for _, ac := range conns {
		p := model.ProviderNone
		if info, ok := byID[ac.session]; ok && cfg.Collectors.App && collector.Eligible(info, cfg) {
			p = collector.ProviderFor(info)
		}
		ac.provider = p
		_ = ac.send(agentproto.Config{Type: "config", Provider: string(p), Title: cfg.Collectors.WindowTitle, IntervalMs: cfg.SampleInterval.Milliseconds()})
	}
}

func (s *Server) handle(ctx context.Context, c net.Conn) {
	defer c.Close()
	cred := peercred.Of
	if s.Cred != nil {
		cred = s.Cred
	}
	pc, err := cred(c)
	if err != nil {
		s.log().Warn("agent: cannot identify peer", "err", err)
		return
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 4096), agentproto.MaxLine)
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if !sc.Scan() {
		return
	}
	var hello agentproto.Hello
	if json.Unmarshal(sc.Bytes(), &hello) != nil || hello.Type != "hello" {
		return
	}
	ac := &agentConn{c: c, enc: json.NewEncoder(c), uid: pc.UID}

	cfg := s.Cfg()
	sess, reason := s.resolve(ctx, cfg, pc, hello)
	if sess == nil {
		_ = ac.send(agentproto.Welcome{Type: "welcome", Monitored: false, Reason: reason})
		s.log().Debug("agent refused", "uid", pc.UID, "reason", reason)
		return
	}
	if hello.SessionType != "" && hello.SessionType != string(sess.Type) {
		s.log().Warn("agent's XDG_SESSION_TYPE disagrees with logind; trusting logind",
			"agent", hello.SessionType, "logind", sess.Type, "session", sess.ID)
	}
	ac.session = sess.ID
	ac.provider = model.ProviderNone
	if cfg.Collectors.App {
		ac.provider = collector.ProviderFor(*sess)
	}
	_ = ac.send(agentproto.Welcome{Type: "welcome", Monitored: true, SessionID: sess.ID, Provider: string(ac.provider),
		Title: cfg.Collectors.WindowTitle, IntervalMs: cfg.SampleInterval.Milliseconds()})
	_ = c.SetReadDeadline(time.Time{})

	s.mu.Lock()
	if s.entries == nil {
		s.entries = map[string]*entry{}
	}
	if old := s.entries[sess.ID]; old != nil && old.conn != nil && old.conn != ac {
		old.conn.c.Close() // one agent per session
	}
	e := s.entries[sess.ID]
	if e == nil {
		e = &entry{}
		s.entries[sess.ID] = e
	}
	e.conn = ac
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if e.conn == ac {
			e.conn = nil
		}
		s.mu.Unlock()
	}()

	var last time.Time
	warned := false
	for sc.Scan() {
		now := s.now()
		if now.Sub(last) < 100*time.Millisecond { // rate limit
			continue
		}
		var r agentproto.Report
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.Type != "report" {
			continue
		}
		if r.Provider != string(ac.provider) {
			if !warned {
				warned = true
				s.log().Warn("agent reported for a provider that is not allowed in this session; ignoring",
					"session", sess.ID, "got", r.Provider, "allowed", ac.provider)
			}
			continue
		}
		last = now
		cur := s.Cfg()
		r = sanitize(r, cur.Collectors.WindowTitle)
		s.mu.Lock()
		e.rep, e.recv = r, now
		s.mu.Unlock()
	}
}

// resolve picks the session an agent serves, from the peer's uid. A systemd
// *user* service is not inside the login session's scope, so logind cannot map
// its pid to a session; the uid and the (verified) hint do the job instead.
func (s *Server) resolve(ctx context.Context, cfg *config.Config, pc peercred.Cred, h agentproto.Hello) (*collector.SessionInfo, string) {
	if !cfg.Recording() {
		return nil, "recording is not enabled"
	}
	infos, err := s.Src.Sessions(ctx)
	if err != nil {
		return nil, "cannot read sessions"
	}
	var mine []collector.SessionInfo
	userSeen := false
	for _, i := range infos {
		if i.UID != pc.UID {
			continue
		}
		if cfg.IsMonitored(i.User) {
			userSeen = true
		}
		if collector.Eligible(i, cfg) && (i.Type == model.SessionX11 || i.Type == model.SessionWayland) {
			mine = append(mine, i)
		}
	}
	if len(mine) == 0 {
		if !userSeen {
			return nil, "this user is not monitored"
		}
		return nil, "no graphical session"
	}
	if id, err := s.Src.SessionOfPID(ctx, pc.PID); err == nil {
		for i := range mine {
			if mine[i].ID == id {
				return &mine[i], ""
			}
		}
	}
	if h.SessionHint != "" {
		for i := range mine {
			if mine[i].ID == h.SessionHint {
				return &mine[i], ""
			}
		}
	}
	for i := range mine {
		if mine[i].Active {
			return &mine[i], ""
		}
	}
	return &mine[0], ""
}

func sanitize(r agentproto.Report, titles bool) agentproto.Report {
	r.Detail = clip(r.Detail, 200)
	if r.App != nil {
		a := *r.App
		a.ID, a.Name = strings.ToLower(clip(strings.TrimSpace(a.ID), 200)), clip(strings.TrimSpace(a.Name), 200)
		r.App = &a
	}
	if titles {
		r.Title = clip(r.Title, 500)
	} else {
		r.Title = ""
	}
	return r
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

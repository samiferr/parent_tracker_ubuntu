// Package control serves the local control socket: the child's read-only
// self-view and, for root, the tracker's health.
//
// The caller is identified by the kernel (SO_PEERCRED), never by what the
// request says, and the service returns only that caller's own records. A
// non-root user cannot ask for another user's data (docs/USER-GUIDE §11, SEC-3).
package control

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/user"
	"strconv"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/peercred"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
	"github.com/samiferr/parent_tracker_ubuntu/internal/view"
)

type credKey struct{}

// Server is the control-socket service.
type Server struct {
	DB     *storage.DB
	Cfg    func() *config.Config
	Loc    *time.Location
	Now    func() time.Time
	Health func() tracker.Health // nil when the collectors are not running
	// LookupUID maps a uid to a user name; os/user in production.
	LookupUID func(uid int) (string, error)
	// Cred overrides peer identification (tests).
	Cred func(net.Conn) (peercred.Cred, error)
}

func (s *Server) lookup(uid int) (string, error) {
	if s.LookupUID != nil {
		return s.LookupUID(uid)
	}
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

// Serve answers requests on ln until ctx ends.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /self", s.handleSelf)
	mux.HandleFunc("GET /health", s.handleHealth)
	cred := peercred.Of
	if s.Cred != nil {
		cred = s.Cred
	}
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			pc, err := cred(c)
			if err != nil {
				pc = peercred.Cred{UID: -1}
			}
			return context.WithValue(ctx, credKey{}, pc)
		},
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func callerOf(r *http.Request) peercred.Cred {
	c, _ := r.Context().Value(credKey{}).(peercred.Cred)
	return c
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleSelf(w http.ResponseWriter, r *http.Request) {
	caller := callerOf(r)
	if caller.UID < 0 {
		errJSON(w, http.StatusForbidden, "Could not tell who is asking.")
		return
	}
	name, err := s.lookup(caller.UID)
	if err != nil && caller.UID != 0 {
		errJSON(w, http.StatusForbidden, "Could not tell who is asking.")
		return
	}
	if want := r.URL.Query().Get("user"); want != "" {
		if caller.UID != 0 && want != name {
			errJSON(w, http.StatusForbidden, "You can only see your own recorded data.")
			return
		}
		name = want
	} else if caller.UID == 0 {
		errJSON(w, http.StatusBadRequest, "Choose a user with --user.")
		return
	}

	u, found, err := s.DB.UserByName(name)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, "Could not read the database.")
		return
	}
	if !found || !u.Monitored || !s.Cfg().IsMonitored(name) {
		errJSON(w, http.StatusNotFound, "Parent Tracker is not recording anything about this account.")
		return
	}
	v := &view.Viewer{DB: s.DB, Cfg: s.Cfg, Loc: s.Loc, Now: s.Now}
	date := r.URL.Query().Get("date")
	if date != "" {
		if _, _, err := aggregate.ParseDay(s.Loc, date); err != nil {
			errJSON(w, http.StatusBadRequest, "The date must look like 2026-10-09.")
			return
		}
	}
	self, err := v.Self(u.UID, name, date)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, "Could not read your recorded data.")
		return
	}
	writeJSON(w, http.StatusOK, self)
}

// handleHealth is for root only: it is what `status` and `doctor` ask the
// running service.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if callerOf(r).UID != 0 {
		errJSON(w, http.StatusForbidden, "Only root can read the service health.")
		return
	}
	out := map[string]any{"running": true}
	if s.Health != nil {
		out["tracker"] = s.Health()
	}
	writeJSON(w, http.StatusOK, out)
}

package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/live"
)

const keepaliveEvery = 15 * time.Second

// handleLiveStream serves Server-Sent Events (docs/04 §4.11): a snapshot on
// connect, "state" when the state or app changes, "tick" once per sampling
// interval, and a keep-alive comment every 15 s. Compression is off and
// X-Accel-Buffering is set so a reverse proxy does not hold events back.
func (s *Server) handleLiveStream(w http.ResponseWriter, r *http.Request) {
	if !s.liveEnabled() {
		apiError(w, http.StatusNotFound, "The live view is off.", "Turn it on with `sudo parent-tracker config set server.live true`.")
		return
	}
	u, ok := s.userFrom(w, r)
	if !ok {
		return
	}
	c, err := s.d.Hub.Subscribe(u.UID)
	switch err {
	case nil:
	case live.ErrTooMany:
		w.Header().Set("Retry-After", "5")
		apiError(w, http.StatusServiceUnavailable, "Too many live viewers.", "Close a dashboard tab, or raise server.live_max_clients. This page will refresh by polling instead.")
		return
	default:
		apiError(w, http.StatusNotFound, "The live view is off.", "")
		return
	}
	defer c.Close()

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // the stream is long-lived
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	if _, found := s.d.Hub.Snapshot(u.UID); !found {
		fmt.Fprint(w, "event: unavailable\ndata: {}\n\n")
	}
	if rc.Flush() != nil {
		return
	}

	ka := time.NewTicker(keepaliveEvery)
	defer ka.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-c.Events():
			if !ok { // dropped as a slow client, or the live view was turned off
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, ev.Data); err != nil {
				return
			}
			if rc.Flush() != nil || ev.Name == "bye" {
				return
			}
		case <-ka.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		}
	}
}

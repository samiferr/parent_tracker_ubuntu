// Package server is the authenticated dashboard: the embedded React + Lumen
// front end, a JSON API and the live Server-Sent Events stream.
//
// It binds to loopback by default, requires a password for every /api route
// except sign-in, is same-origin only (no CORS), and sends a content security
// policy that lets the page load nothing from any other origin (NFR-4.3,
// NFR-5.2, SEC-15).
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/live"
	"github.com/samiferr/parent_tracker_ubuntu/internal/server/web"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
	"github.com/samiferr/parent_tracker_ubuntu/internal/view"
)

// Deps are the server's collaborators.
type Deps struct {
	Cfg     func() *config.Config
	CfgPath string // where settings are saved; empty makes settings read-only
	DB      *storage.DB
	Hub     *live.Hub
	Health  func() tracker.Health // nil when the collectors are not running (serve mode)
	Reload  func() error          // applies a saved config in the daemon
	Loc     *time.Location
	Now     func() time.Time
	Log     *slog.Logger
	Version string
	Demo    bool
	// Files overrides the embedded front end (tests).
	Files fs.FS
}

// Server serves the dashboard.
type Server struct {
	d     Deps
	view  *view.Viewer
	auth  *authState
	files fs.FS
	etags map[string]string
}

// New builds the server.
func New(d Deps) *Server {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Loc == nil {
		d.Loc = time.Local
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Files == nil {
		d.Files = web.FS
	}
	s := &Server{d: d, auth: newAuth(d.Now), files: d.Files, etags: map[string]string{}}
	s.view = &view.Viewer{DB: d.DB, Cfg: d.Cfg, Loc: d.Loc, Now: d.Now}
	_ = fs.WalkDir(s.files, ".", func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			if b, err := fs.ReadFile(s.files, p); err == nil {
				sum := sha256.Sum256(b)
				s.etags[p] = `"` + hex.EncodeToString(sum[:8]) + `"`
			}
		}
		return nil
	})
	return s
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// open: the page itself, the sign-in state and sign-in
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("POST /api/login", s.handleLogin)

	// everything else under /api needs a signed-in session
	api := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.requireAuth(h)) }
	api("POST /api/logout", s.handleLogout)
	api("GET /api/users", s.handleUsers)
	api("GET /api/summary", s.handleSummary)
	api("GET /api/hourly", s.handleHourly)
	api("GET /api/trend", s.handleTrend)
	api("GET /api/apps", s.handleApps)
	api("GET /api/apps/detail", s.handleAppDetail)
	api("GET /api/events", s.handleEvents)
	api("GET /api/audit", s.handleAudit)
	api("GET /api/live", s.handleLive)
	api("GET /api/live/stream", s.handleLiveStream)
	api("GET /api/health", s.handleHealth)
	api("GET /api/settings", s.handleSettingsGet)
	api("PUT /api/settings", s.handleSettingsPut)
	api("GET /api/about", s.handleAbout)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		apiError(w, http.StatusNotFound, "There is nothing at this address.", "")
	})
	mux.HandleFunc("/", s.handleStatic)

	return s.secure(mux)
}

// csp lets the page load only from its own origin: no CDN, no web fonts, no
// analytics (AC-112). Inline style attributes are needed by React/Lumen; inline
// scripts are not allowed.
const csp = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}

		if !s.hostAllowed(r) { // DNS-rebinding guard
			apiError(w, http.StatusMisdirectedRequest, "This address is not allowed.", "Open the dashboard at http://127.0.0.1:8787.")
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
				apiError(w, http.StatusForbidden, "Cross-origin requests are not allowed.", "")
				return
			}
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				apiError(w, http.StatusUnsupportedMediaType, "Send JSON.", "")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed rejects requests whose Host header is not the loopback names when
// the dashboard is loopback-only, which stops a web page on another site from
// reaching it through DNS rebinding. With allow_lan any host is accepted, since
// the LAN names cannot be enumerated.
func (s *Server) hostAllowed(r *http.Request) bool {
	cfg := s.d.Cfg()
	if cfg.Server.AllowLAN {
		return true
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return config.IsLoopbackHost(host)
}

func sameOrigin(origin, host string) bool {
	return strings.HasSuffix(origin, "://"+host)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if name == "notices.txt" { // served through /api/about, not as a file
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(s.files, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	etag := s.etags[name]
	if etag != "" {
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	// vendored files change only with a Lumen/React/font upgrade; the app's own
	// files are revalidated by ETag on every load
	if strings.HasPrefix(name, "vendor/") {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("Content-Type", contentType(name))
	_, _ = w.Write(b)
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	case ".json":
		return "application/json"
	}
	return "application/octet-stream"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// apiError writes {"error", "hint"}: what happened and what to do next (DS-5).
func apiError(w http.ResponseWriter, status int, msg, hint string) {
	body := map[string]string{"error": msg}
	if hint != "" {
		body["hint"] = hint
	}
	writeJSON(w, status, body)
}

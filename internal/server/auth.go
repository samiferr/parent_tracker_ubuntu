package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName  = "pt_session"
	sessionTTL  = 12 * time.Hour
	maxFailures = 5
	failWindow  = 10 * time.Minute
)

type authState struct {
	now      func() time.Time
	mu       sync.Mutex
	sessions map[string]time.Time
	fails    map[string][]time.Time
	dummy    []byte
	dummyOne sync.Once
}

func newAuth(now func() time.Time) *authState {
	return &authState{now: now, sessions: map[string]time.Time{}, fails: map[string][]time.Time{}}
}

func (a *authState) create() (string, time.Time) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("no randomness: " + err.Error())
	}
	tok := hex.EncodeToString(b)
	exp := a.now().Add(sessionTTL)
	a.mu.Lock()
	defer a.mu.Unlock()
	for t, e := range a.sessions { // drop expired sessions
		if e.Before(a.now()) {
			delete(a.sessions, t)
		}
	}
	a.sessions[tok] = exp
	return tok, exp
}

func (a *authState) valid(tok string) bool {
	if tok == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[tok]
	if !ok || exp.Before(a.now()) {
		delete(a.sessions, tok)
		return false
	}
	return true
}

func (a *authState) revoke(tok string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, tok)
}

// throttled reports whether an address has failed too often recently, and how
// long until it may try again.
func (a *authState) throttled(ip string) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	recent := a.fails[ip][:0]
	for _, t := range a.fails[ip] {
		if now.Sub(t) < failWindow {
			recent = append(recent, t)
		}
	}
	a.fails[ip] = recent
	if len(recent) >= maxFailures {
		return failWindow - now.Sub(recent[0]), true
	}
	return 0, false
}

func (a *authState) fail(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fails[ip] = append(a.fails[ip], a.now())
}

func (a *authState) clear(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.fails, ip)
}

// dummyHash is compared against when the user name is wrong, so a wrong user
// name and a wrong password take the same time.
func (a *authState) dummyHash() []byte {
	a.dummyOne.Do(func() {
		a.dummy, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)
	})
	return a.dummy
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Server) authenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && s.auth.valid(c.Value)
}

func (s *Server) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authenticated(r) {
			apiError(w, http.StatusUnauthorized, "Sign in to continue.", "")
			return
		}
		next(w, r)
	})
}

type sessionResp struct {
	Authenticated bool   `json:"authenticated"`
	Configured    bool   `json:"configured"`
	Username      string `json:"username,omitempty"`
	Version       string `json:"version"`
	Live          bool   `json:"live"`
	Demo          bool   `json:"demo"`
	Recording     bool   `json:"recording"`
	// SettingsEditable is true when the dashboard may save settings.
	SettingsEditable bool `json:"settings_editable"`
	// Today and Timezone let the page show the PC's calendar day and hours even
	// when the browser is in another timezone (e.g. a phone on the home network).
	Today    string `json:"today,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	cfg := s.d.Cfg()
	resp := sessionResp{Configured: cfg.Server.PasswordHash != "", Version: s.d.Version, Demo: s.d.Demo}
	if s.authenticated(r) {
		resp.Authenticated = true
		resp.Username = cfg.Server.Username
		resp.Live = cfg.Server.Live && s.d.Hub != nil && s.d.Hub.Enabled()
		resp.Recording = cfg.Recording()
		resp.SettingsEditable = s.d.CfgPath != ""
		resp.Today = s.today()
		resp.Timezone = zoneName(s.d.Loc)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	cfg := s.d.Cfg()
	if cfg.Server.PasswordHash == "" {
		apiError(w, http.StatusServiceUnavailable, "Dashboard sign-in is not set up.", "Run `sudo parent-tracker setup` to choose a password.")
		return
	}
	ip := clientIP(r)
	if wait, blocked := s.auth.throttled(ip); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		apiError(w, http.StatusTooManyRequests, "Too many attempts.", "Wait a few minutes and try again.")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		apiError(w, http.StatusBadRequest, "Enter your username and password.", "")
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(in.Username), []byte(cfg.Server.Username)) == 1
	hash := []byte(cfg.Server.PasswordHash)
	if !userOK {
		hash = s.auth.dummyHash()
	}
	passOK := bcrypt.CompareHashAndPassword(hash, []byte(in.Password)) == nil
	if !userOK || !passOK {
		s.auth.fail(ip)
		s.d.Log.Info("dashboard sign-in failed", "from", ip) // never the password, never the user name typed
		apiError(w, http.StatusUnauthorized, "Username or password is incorrect.", "")
		return
	}
	s.auth.clear(ip)
	tok, exp := s.auth.create()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/", Expires: exp, MaxAge: int(sessionTTL.Seconds()), HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": cfg.Server.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.auth.revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// zoneName gives the IANA name of a location. time.Local reports itself as
// "Local", so its name is read from /etc/timezone or the /etc/localtime link.
func zoneName(loc *time.Location) string {
	if n := loc.String(); n != "Local" && n != "" {
		return n
	}
	if tz := os.Getenv("TZ"); tz != "" && !strings.HasPrefix(tz, ":/") {
		return strings.TrimPrefix(tz, ":")
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if n := strings.TrimSpace(string(b)); n != "" {
			return n
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			return link[i+len("zoneinfo/"):]
		}
	}
	return "UTC"
}

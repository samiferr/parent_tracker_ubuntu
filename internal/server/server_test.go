package server

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/live"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
)

const password = "correct horse battery"

type rig struct {
	t       *testing.T
	srv     *Server
	ts      *httptest.Server
	db      *storage.DB
	hub     *live.Hub
	cfg     *config.Config
	cfgMu   sync.Mutex
	cfgPath string
	now     time.Time
	reloads int
	client  *http.Client
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	r := &rig{t: t, db: db, cfg: config.Default(), hub: live.New(2, true), now: time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)}
	r.cfg.MonitoredUsers = []string{"ilyes"}
	r.cfg.DisclosureAcknowledged = "2026-10-01T00:00:00Z"
	r.cfg.Server.PasswordHash = string(hash)
	r.cfgPath = filepath.Join(t.TempDir(), "config.toml")
	if err := r.cfg.Save(r.cfgPath); err != nil {
		t.Fatal(err)
	}
	db.UpsertUser(1000, "ilyes", true, 1)
	files := fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><title>Dashboard</title>")},
		"js/main.js":         {Data: []byte("// app")},
		"vendor/lumen/x.css": {Data: []byte("/* lumen */")},
		"notices.txt":        {Data: []byte("Lumen is Apache-2.0")},
	}
	r.srv = New(Deps{
		Cfg: func() *config.Config { r.cfgMu.Lock(); defer r.cfgMu.Unlock(); return r.cfg }, CfgPath: r.cfgPath, DB: db, Hub: r.hub,
		Loc: time.UTC, Now: func() time.Time { return r.now }, Version: "test", Files: files,
		Health: func() tracker.Health { return tracker.Health{Recording: true, Users: []tracker.UserHealth{}} },
		Reload: func() error {
			c, err := config.Load(r.cfgPath)
			if err != nil {
				return err
			}
			r.cfgMu.Lock()
			r.cfg = c
			r.reloads++
			r.cfgMu.Unlock()
			return nil
		},
	})
	r.ts = httptest.NewServer(r.srv.Handler())
	t.Cleanup(r.ts.Close)
	jar, _ := cookiejar.New(nil)
	r.client = &http.Client{Jar: jar}
	return r
}

func (r *rig) do(method, path string, body any) (*http.Response, []byte) {
	r.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, r.ts.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func (r *rig) login() {
	r.t.Helper()
	resp, b := r.do("POST", "/api/login", map[string]string{"username": "parent", "password": password})
	if resp.StatusCode != 200 {
		r.t.Fatalf("login: %d %s", resp.StatusCode, b)
	}
}

func (r *rig) seed() {
	at := func(h, m int) int64 { return time.Date(2026, 10, 11, h, m, 0, 0, time.UTC).UnixMilli() }
	id, _ := r.db.OpenInterval(1000, model.StateActive, at(16, 0))
	r.db.CloseInterval(id, at(17, 30))
	var ss []model.Sample
	for ts := at(16, 0); ts < at(17, 30); ts += 5000 {
		ss = append(ss, model.Sample{UID: 1000, TS: ts, AppID: "firefox", State: model.StateActive, Source: "x11"})
	}
	r.db.InsertSamples(ss, map[string]string{"firefox": "Firefox Web Browser"})
}

func decode(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("bad json %q: %v", b, err)
	}
}

// AC-52, AC-85: every /api route except sign-in refuses an unauthenticated caller.
func TestAPIRequiresAuthentication(t *testing.T) {
	r := newRig(t)
	for _, p := range []string{"/api/users", "/api/summary", "/api/hourly", "/api/trend", "/api/apps", "/api/apps/detail?app=x",
		"/api/events", "/api/audit", "/api/live", "/api/live/stream", "/api/health", "/api/settings", "/api/about"} {
		resp, b := r.do("GET", p, nil)
		if resp.StatusCode != 401 {
			t.Errorf("GET %s = %d %s, want 401", p, resp.StatusCode, b)
		}
	}
	resp, _ := r.do("PUT", "/api/settings", map[string]any{"values": map[string]any{"server.live": false}})
	if resp.StatusCode != 401 {
		t.Errorf("PUT settings unauthenticated = %d", resp.StatusCode)
	}
	resp, b := r.do("GET", "/api/session", nil)
	var s sessionResp
	decode(t, b, &s)
	if resp.StatusCode != 200 || s.Authenticated || !s.Configured || s.Username != "" {
		t.Fatalf("session = %s", b)
	}
}

func TestLoginLogoutAndCookie(t *testing.T) {
	r := newRig(t)
	resp, b := r.do("POST", "/api/login", map[string]string{"username": "parent", "password": "wrong"})
	if resp.StatusCode != 401 || !strings.Contains(string(b), "Username or password is incorrect.") {
		t.Fatalf("wrong password: %d %s", resp.StatusCode, b)
	}
	// the same message for a wrong user name: nothing tells which field was wrong
	resp2, b2 := r.do("POST", "/api/login", map[string]string{"username": "nobody", "password": password})
	if resp2.StatusCode != 401 || string(b) != string(b2) {
		t.Fatalf("wrong user: %d %s", resp2.StatusCode, b2)
	}
	r.login()
	var sc *http.Cookie
	resp3, _ := r.do("GET", "/api/session", nil)
	_ = resp3
	for _, c := range r.client.Jar.Cookies(mustURL(r.ts.URL)) {
		if c.Name == cookieName {
			sc = c
		}
	}
	if sc == nil {
		t.Fatal("no session cookie")
	}
	// inspect the Set-Cookie flags on a fresh login
	req, _ := http.NewRequest("POST", r.ts.URL+"/api/login", strings.NewReader(`{"username":"parent","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	raw, _ := http.DefaultClient.Do(req)
	raw.Body.Close()
	set := raw.Header.Get("Set-Cookie")
	if !strings.Contains(set, "HttpOnly") || !strings.Contains(set, "SameSite=Strict") {
		t.Fatalf("Set-Cookie = %q", set)
	}
	_, b = r.do("GET", "/api/session", nil)
	var s sessionResp
	decode(t, b, &s)
	if !s.Authenticated || s.Username != "parent" || !s.Recording {
		t.Fatalf("session = %s", b)
	}
	if resp, _ := r.do("POST", "/api/logout", map[string]any{}); resp.StatusCode != 200 {
		t.Fatal("logout failed")
	}
	if resp, _ := r.do("GET", "/api/users", nil); resp.StatusCode != 401 {
		t.Fatal("the session must end at logout")
	}
}

func mustURL(s string) *urlT { u, _ := parseURL(s); return u }

func TestLoginThrottling(t *testing.T) {
	r := newRig(t)
	for i := 0; i < maxFailures; i++ {
		r.do("POST", "/api/login", map[string]string{"username": "parent", "password": "nope"})
	}
	resp, b := r.do("POST", "/api/login", map[string]string{"username": "parent", "password": password})
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("expected throttling, got %d %s", resp.StatusCode, b)
	}
	r.now = r.now.Add(failWindow + time.Minute)
	if resp, _ := r.do("POST", "/api/login", map[string]string{"username": "parent", "password": password}); resp.StatusCode != 200 {
		t.Fatalf("throttling should lift: %d", resp.StatusCode)
	}
}

func TestLoginNotConfigured(t *testing.T) {
	r := newRig(t)
	r.cfgMu.Lock()
	r.cfg.Server.PasswordHash = ""
	r.cfgMu.Unlock()
	resp, b := r.do("POST", "/api/login", map[string]string{"username": "parent", "password": "x"})
	if resp.StatusCode != 503 || !strings.Contains(string(b), "parent-tracker setup") {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
}

// DNS rebinding and cross-site requests.
func TestHostAndOriginGuards(t *testing.T) {
	r := newRig(t)
	req, _ := http.NewRequest("GET", r.ts.URL+"/api/session", nil)
	req.Host = "evil.example.com"
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("foreign Host = %d", resp.StatusCode)
	}
	for _, host := range []string{"localhost:8787", "127.0.0.1:8787", "[::1]:8787"} {
		req, _ = http.NewRequest("GET", r.ts.URL+"/api/session", nil)
		req.Host = host
		resp, _ = http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("Host %s = %d", host, resp.StatusCode)
		}
	}
	req, _ = http.NewRequest("POST", r.ts.URL+"/api/login", strings.NewReader(`{"username":"parent","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("cross-origin POST = %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", r.ts.URL+"/api/login", strings.NewReader(`username=parent`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 415 {
		t.Fatalf("form POST = %d", resp.StatusCode)
	}
	// with allow_lan any Host is accepted (the LAN names cannot be listed)
	r.cfgMu.Lock()
	r.cfg.Server.AllowLAN = true
	r.cfgMu.Unlock()
	req, _ = http.NewRequest("GET", r.ts.URL+"/api/session", nil)
	req.Host = "pc.lan:8787"
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("LAN host = %d", resp.StatusCode)
	}
}

// AC-112: the page can load nothing from another origin.
func TestStaticFilesAndContentSecurityPolicy(t *testing.T) {
	r := newRig(t)
	resp, b := r.do("GET", "/", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "Dashboard") || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("/ = %d %q", resp.StatusCode, b)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "font-src 'self'", "connect-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP lacks %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "http") || strings.Contains(csp, "*") {
		t.Errorf("CSP allows a foreign origin: %s", csp)
	}
	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
	// ETag revalidation
	etag := resp.Header.Get("ETag")
	req, _ := http.NewRequest("GET", r.ts.URL+"/", nil)
	req.Header.Set("If-None-Match", etag)
	resp2, _ := r.client.Do(req)
	resp2.Body.Close()
	if etag == "" || resp2.StatusCode != 304 {
		t.Fatalf("etag=%q, revalidation = %d", etag, resp2.StatusCode)
	}
	resp, _ = r.do("GET", "/vendor/lumen/x.css", nil)
	if !strings.Contains(resp.Header.Get("Cache-Control"), "max-age") || resp.Header.Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("vendor headers: %v", resp.Header)
	}
	if resp, _ = r.do("GET", "/notices.txt", nil); resp.StatusCode != 404 {
		t.Fatal("notices are served through /api/about only")
	}
	if resp, _ = r.do("GET", "/../etc/passwd", nil); resp.StatusCode == 200 {
		t.Fatal("path traversal")
	}
	if resp, _ = r.do("GET", "/nope", nil); resp.StatusCode != 404 {
		t.Fatalf("/nope = %d", resp.StatusCode)
	}
}

func TestSummaryHourlyTrendAppsEvents(t *testing.T) {
	r := newRig(t)
	r.seed()
	r.login()

	_, b := r.do("GET", "/api/users", nil)
	var users struct{ Users []userResp }
	decode(t, b, &users)
	if len(users.Users) != 1 || users.Users[0].Name != "ilyes" {
		t.Fatalf("users = %s", b)
	}

	_, b = r.do("GET", "/api/summary?user=ilyes&date=2026-10-11", nil)
	var sum struct {
		User string
		Day  struct {
			ActiveMs int64                   `json:"active_ms"`
			TopApps  []struct{ Name string } `json:"top_apps"`
			Hours    []any
		}
	}
	decode(t, b, &sum)
	if sum.User != "ilyes" || sum.Day.ActiveMs != 90*60*1000 || len(sum.Day.TopApps) != 1 || sum.Day.TopApps[0].Name != "Firefox Web Browser" || len(sum.Day.Hours) != 24 {
		t.Fatalf("summary = %s", b)
	}

	_, b = r.do("GET", "/api/hourly?user=ilyes&date=2026-10-11&apps=1", nil)
	var hd struct {
		Day struct {
			ActiveMs int64 `json:"active_ms"`
			Buckets  []struct {
				Hour     int
				ActiveMs int64 `json:"active_ms"`
				Apps     []struct {
					AppID string `json:"app_id"`
				}
			}
			Summary string
		}
	}
	decode(t, b, &hd)
	if hd.Day.ActiveMs != sum.Day.ActiveMs || len(hd.Day.Buckets) != 24 || hd.Day.Buckets[16].ActiveMs != 60*60*1000 ||
		len(hd.Day.Buckets[16].Apps) != 1 || hd.Day.Buckets[16].Apps[0].AppID != "firefox" || !strings.Contains(hd.Day.Summary, "Most use 16:00–18:00") {
		t.Fatalf("hourly = %s", b)
	}

	_, b = r.do("GET", "/api/hourly?user=ilyes&from=2026-10-05&to=2026-10-11", nil)
	var rg struct {
		Range struct {
			Days []struct{ Date string } `json:"days"`
			Avg  []int64                 `json:"avg_active_ms"`
		}
	}
	decode(t, b, &rg)
	if len(rg.Range.Days) != 7 || len(rg.Range.Avg) != 24 {
		t.Fatalf("range = %s", b)
	}
	_, b = r.do("GET", "/api/hourly?user=ilyes&from=2026-10-05&to=2026-10-11&shape=avg", nil)
	if !strings.Contains(string(b), "avg_active_ms") || strings.Contains(string(b), `"days"`) {
		t.Fatalf("avg shape = %s", b)
	}

	_, b = r.do("GET", "/api/trend?user=ilyes&from=2026-10-09&to=2026-10-11", nil)
	var tr struct {
		Days []struct {
			ActiveMs int64 `json:"active_ms"`
		}
	}
	decode(t, b, &tr)
	if len(tr.Days) != 3 || tr.Days[2].ActiveMs != 90*60*1000 {
		t.Fatalf("trend = %s", b)
	}

	_, b = r.do("GET", "/api/apps?user=ilyes&from=2026-10-05&to=2026-10-11", nil)
	if !strings.Contains(string(b), `"firefox"`) {
		t.Fatalf("apps = %s", b)
	}
	_, b = r.do("GET", "/api/apps/detail?user=ilyes&app=firefox&from=2026-10-05&to=2026-10-11", nil)
	if !strings.Contains(string(b), `"hour_of_day_ms"`) {
		t.Fatalf("detail = %s", b)
	}

	r.db.InsertAlert(model.Alert{CreatedAt: r.now.UnixMilli(), RuleID: "bedtime", UID: 1000, Severity: "warn", Message: "ilyes was active at 22:41."})
	_, b = r.do("GET", "/api/events", nil)
	if !strings.Contains(string(b), "Activity after bedtime") {
		t.Fatalf("events = %s", b)
	}

	_, b = r.do("GET", "/api/health", nil)
	var hl struct {
		CollectorsRunning bool `json:"collectors_running"`
		Recording         bool
		Degraded          []any
		Database          struct {
			SchemaVersion int `json:"schema_version"`
		}
	}
	decode(t, b, &hl)
	if !hl.CollectorsRunning || !hl.Recording || hl.Degraded == nil || hl.Database.SchemaVersion != storage.LatestVersion {
		t.Fatalf("health = %s", b)
	}
}

func TestBadInputIsAClearError(t *testing.T) {
	r := newRig(t)
	r.login()
	for path, want := range map[string]int{
		"/api/summary?date=11-10-2026":              400,
		"/api/summary?user=stranger":                404,
		"/api/hourly?from=2026-10-11&to=2026-10-01": 400,
		"/api/apps/detail":                          400,
	} {
		resp, b := r.do("GET", path, nil)
		if resp.StatusCode != want || !strings.Contains(string(b), `"error"`) {
			t.Errorf("%s = %d %s, want %d", path, resp.StatusCode, b, want)
		}
	}
	// no monitored users at all
	r.db.UpsertUser(1000, "ilyes", false, 1)
	resp, b := r.do("GET", "/api/summary", nil)
	if resp.StatusCode != 404 || !strings.Contains(string(b), "users add") {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
}

func TestLiveSnapshotAndSwitch(t *testing.T) {
	r := newRig(t)
	r.login()
	_, b := r.do("GET", "/api/live", nil)
	if !strings.Contains(string(b), `"available":false`) {
		t.Fatalf("before the collectors report: %s", b)
	}
	r.hub.Publish(1000, live.Snapshot{User: "ilyes", UID: 1000, State: "active", AppStatus: "ok", App: &live.App{ID: "code", Name: "Code"}, Degraded: []live.Degraded{}}, "state")
	_, b = r.do("GET", "/api/live?user=ilyes", nil)
	var snap struct {
		Available  bool
		State      string
		ServerTime int64 `json:"server_time"`
		App        struct{ Name string }
	}
	decode(t, b, &snap)
	if !snap.Available || snap.State != "active" || snap.App.Name != "Code" || snap.ServerTime != r.now.UnixMilli() {
		t.Fatalf("live = %s", b)
	}
	// AC-87: server.live = false → /api/live* return 404
	r.hub.Configure(2, false)
	for _, p := range []string{"/api/live", "/api/live/stream"} {
		if resp, _ := r.do("GET", p, nil); resp.StatusCode != 404 {
			t.Errorf("%s with the live view off = %d", p, resp.StatusCode)
		}
	}
	r.hub.Configure(2, true)
	if resp, _ := r.do("GET", "/api/live", nil); resp.StatusCode != 200 {
		t.Fatal("re-enabling must restore the endpoint without a restart")
	}
}

type sse struct {
	resp *http.Response
	sc   *bufio.Scanner
}

func (r *rig) openStream() (*sse, int) {
	req, _ := http.NewRequest("GET", r.ts.URL+"/api/live/stream?user=ilyes", nil)
	resp, err := r.client.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, resp.StatusCode
	}
	r.t.Cleanup(func() { resp.Body.Close() })
	return &sse{resp: resp, sc: bufio.NewScanner(resp.Body)}, 200
}

// next reads the next event (name, data), skipping comments and retry hints.
func (s *sse) next(t *testing.T) (string, string) {
	t.Helper()
	var name, data string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for s.sc.Scan() {
			line := s.sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && name != "":
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3s")
	}
	return name, data
}

// AC-80, AC-85, AC-87: the stream pushes updates, the extra viewer gets 503,
// and turning the live view off ends the stream with a bye.
func TestLiveStream(t *testing.T) {
	r := newRig(t)
	r.login()
	r.hub.Publish(1000, live.Snapshot{User: "ilyes", UID: 1000, State: "active", Degraded: []live.Degraded{}}, "state")

	a, code := r.openStream()
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if ct := a.resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	if a.resp.Header.Get("X-Accel-Buffering") != "no" || !strings.Contains(a.resp.Header.Get("Cache-Control"), "no-transform") {
		t.Fatalf("buffering headers: %v", a.resp.Header)
	}
	if name, data := a.next(t); name != "snapshot" || !strings.Contains(data, `"state":"active"`) {
		t.Fatalf("first event = %s %s", name, data)
	}
	r.hub.Publish(1000, live.Snapshot{User: "ilyes", UID: 1000, State: "locked", Degraded: []live.Degraded{}}, "state")
	if name, data := a.next(t); name != "state" || !strings.Contains(data, `"state":"locked"`) {
		t.Fatalf("update = %s %s", name, data)
	}

	b, _ := r.openStream() // 2 viewers allowed
	if b == nil {
		t.Fatal("second viewer should be accepted")
	}
	b.next(t)
	c, code := r.openStream()
	if c != nil || code != 503 {
		t.Fatalf("the third viewer must get 503, got %d", code)
	}
	req, _ := http.NewRequest("GET", r.ts.URL+"/api/live/stream?user=ilyes", nil)
	resp, _ := r.client.Do(req)
	resp.Body.Close()
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("503 needs Retry-After")
	}
	// the others keep working
	r.hub.Publish(1000, live.Snapshot{User: "ilyes", UID: 1000, State: "idle", Degraded: []live.Degraded{}}, "state")
	if _, data := a.next(t); !strings.Contains(data, "idle") {
		t.Fatal("an over-limit viewer disturbed the others")
	}

	r.hub.Configure(2, false)
	if name, _ := a.next(t); name != "bye" {
		t.Fatalf("expected bye, got %s", name)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	r := newRig(t)
	r.login()
	_, b := r.do("GET", "/api/settings", nil)
	var got struct {
		Values   map[string]any
		Editable bool
		ReadOnly map[string]any `json:"read_only"`
	}
	decode(t, b, &got)
	if !got.Editable || got.Values["server.live"] != true || got.Values["sample_interval"] != "5s" || got.Values["collectors.window_title"] != false {
		t.Fatalf("settings = %s", b)
	}
	if got.ReadOnly["server.listen"] != "127.0.0.1:8787" {
		t.Fatalf("read-only = %v", got.ReadOnly)
	}

	resp, b := r.do("PUT", "/api/settings", map[string]any{"values": map[string]any{
		"alerts.enabled": true, "alerts.channel": "ntfy", "alerts.ntfy_url": "https://ntfy.sh/private-topic-xyz",
		"alerts.bedtime": "21:00", "alerts.daily_active_limit": "3h", "server.live_max_clients": 5}})
	if resp.StatusCode != 200 {
		t.Fatalf("PUT = %d %s", resp.StatusCode, b)
	}
	saved, err := config.Load(r.cfgPath)
	if err != nil || !saved.Alerts.Enabled || saved.Alerts.Bedtime != "21:00" || saved.Server.LiveMaxClients != 5 || saved.Alerts.DailyActiveLimit.D() != 3*time.Hour {
		t.Fatalf("saved = %+v, %v", saved.Alerts, err)
	}
	if st, _ := os.Stat(r.cfgPath); st.Mode().Perm() != 0o640 {
		t.Fatalf("config mode = %v", st.Mode().Perm())
	}
	if r.reloads != 1 {
		t.Fatalf("reloads = %d", r.reloads)
	}
	ev, _ := r.db.AuditEvents(0, 1<<60, 10)
	if len(ev) != 1 || ev[0].Action != model.AuditConfigChanged || !strings.Contains(ev[0].Detail, "alerts.bedtime=21:00") {
		t.Fatalf("audit = %+v", ev)
	}
	if strings.Contains(ev[0].Detail, "private-topic-xyz") || !strings.Contains(ev[0].Detail, "alerts.ntfy_url (value not logged)") {
		t.Fatalf("a private URL leaked into the audit log: %q", ev[0].Detail)
	}
	// the password hash is never part of the settings payload
	if _, b = r.do("GET", "/api/settings", nil); strings.Contains(string(b), "$2a$") {
		t.Fatal("the password hash must never be returned")
	}

	// no-op saves do nothing
	before := r.reloads
	r.do("PUT", "/api/settings", map[string]any{"values": map[string]any{"alerts.bedtime": "21:00"}})
	if r.reloads != before {
		t.Fatal("unchanged settings must not trigger a reload")
	}
}

func TestSettingsRejectUnsafeOrInvalidChanges(t *testing.T) {
	r := newRig(t)
	r.login()
	for name, vals := range map[string]map[string]any{
		"who is monitored":      {"monitored_users": "root"},
		"listen address":        {"server.listen": "0.0.0.0:8787"},
		"lan exposure":          {"server.allow_lan": true},
		"password":              {"server.password_hash": "x"},
		"browser history":       {"collectors.web": true},
		"bad duration":          {"sample_interval": "never"},
		"too fast":              {"sample_interval": "10ms"},
		"bad channel":           {"alerts.channel": "pigeon"},
		"wrong type":            {"server.live": "yes"},
		"retention below floor": {"retention.window_samples": "1h"},
	} {
		resp, b := r.do("PUT", "/api/settings", map[string]any{"values": vals})
		if resp.StatusCode != 400 || !strings.Contains(string(b), `"error"`) {
			t.Errorf("%s: %d %s", name, resp.StatusCode, b)
		}
	}
	if r.reloads != 0 {
		t.Fatal("nothing invalid may be applied")
	}
	saved, _ := config.Load(r.cfgPath)
	if saved.Server.Listen != "127.0.0.1:8787" || saved.SampleInterval.D() != 5*time.Second {
		t.Fatal("a rejected change must not be saved")
	}
}

func TestSettingsReadOnlyWithoutAConfigPath(t *testing.T) {
	r := newRig(t)
	r.srv.d.CfgPath = ""
	r.login()
	resp, b := r.do("PUT", "/api/settings", map[string]any{"values": map[string]any{"server.live": false}})
	if resp.StatusCode != 403 || !strings.Contains(string(b), "config set") {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	_, b = r.do("GET", "/api/session", nil)
	if strings.Contains(string(b), `"settings_editable":true`) {
		t.Fatal("session must say settings are not editable")
	}
}

func TestAbout(t *testing.T) {
	r := newRig(t)
	r.login()
	_, b := r.do("GET", "/api/about", nil)
	var a struct {
		Collected []collectedItem
		Never     []string
		Notices   string
	}
	decode(t, b, &a)
	states := map[string]string{}
	for _, c := range a.Collected {
		states[c.Title] = c.State
	}
	if states["Session activity"] != "on" || states["Window titles"] != "off" || states["Browser history"] != "unavailable" || len(a.Never) < 4 || !strings.Contains(a.Notices, "Apache-2.0") {
		t.Fatalf("about = %s", b)
	}
}

type urlT = url.URL

func parseURL(s string) (*url.URL, error) { return url.Parse(s) }

func TestAuditLogFilterAndExport(t *testing.T) {
	r := newRig(t) // now = 2026-10-11 18:00 UTC
	at := func(day, h int) int64 { return time.Date(2026, 10, day, h, 0, 0, 0, time.UTC).UnixMilli() }
	r.db.Audit(at(4, 9), "system", model.AuditServiceStarted, "first start")
	r.db.Audit(at(9, 10), "system", model.AuditSensorUnavailable, "ilyes: x11 session 3: x11_unreachable | dial unix /tmp/.X11-unix/X0: connect: permission denied")
	r.db.Audit(at(10, 11), "system", model.AuditError, "rollup failed: err=database is locked")
	r.db.Audit(at(10, 12), "system", model.AuditWarning, `=HYPERLINK("http://evil.example")`)
	r.db.Audit(at(11, 8), "admin", model.AuditConfigChanged, "alerts.bedtime")
	r.login()

	var av struct {
		From, To string
		Level    string
		Total    int
		Counts   map[string]int
		Entries  []struct {
			Level, Title, Description, Detail string
			ID                                int64
		}
	}
	// default period: the last 7 days (10-05 .. 10-11), so the first start (10-04) is out
	_, b := r.do("GET", "/api/audit", nil)
	decode(t, b, &av)
	if av.From != "2026-10-05" || av.To != "2026-10-11" || av.Total != 4 || av.Counts["error"] != 2 || av.Counts["warning"] != 1 || av.Counts["info"] != 1 {
		t.Fatalf("default = %s", b)
	}
	if av.Entries[0].Title != "Settings changed" || av.Entries[len(av.Entries)-1].Level != "error" {
		t.Fatalf("order/titles = %s", b)
	}

	// a custom period reaches further back
	_, b = r.do("GET", "/api/audit?from=2026-10-01&to=2026-10-11", nil)
	decode(t, b, &av)
	if av.Total != 5 {
		t.Fatalf("custom period total = %d: %s", av.Total, b)
	}

	// errors only: the sensor failure carries what the agent said
	_, b = r.do("GET", "/api/audit?level=error", nil)
	decode(t, b, &av)
	if av.Total != 2 || len(av.Entries) != 2 || av.Counts["info"] != 1 {
		t.Fatalf("errors = %s", b)
	}
	if d := av.Entries[1].Description; !strings.Contains(d, "cannot read the active window") || !strings.Contains(d, "permission denied") {
		t.Fatalf("sensor description = %q", d)
	}

	// export as CSV: the whole period, as a download, with spreadsheet formulas defused
	resp, b := r.do("GET", "/api/audit?from=2026-10-05&to=2026-10-11&format=csv", nil)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") ||
		resp.Header.Get("Content-Disposition") != `attachment; filename="parent-tracker-audit-2026-10-05_to_2026-10-11.csv"` {
		t.Fatalf("csv headers: %d %v", resp.StatusCode, resp.Header)
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\xef\xbb\xbf"))).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v\n%s", err, b)
	}
	if len(recs) != 5 || strings.Join(recs[0], ",") != "time,level,actor,event,message,code,detail" {
		t.Fatalf("csv = %q", recs)
	}
	if recs[1][0] != "2026-10-11T08:00:00Z" || recs[1][1] != "info" || recs[1][2] != "admin" {
		t.Fatalf("first row = %q", recs[1])
	}
	for _, row := range recs[1:] {
		for _, cell := range row[3:] {
			if strings.HasPrefix(cell, "=") {
				t.Fatalf("a cell would run as a formula: %q", cell)
			}
		}
	}

	// a level filter applies to the export, and the file name says so
	resp, b = r.do("GET", "/api/audit?level=error&format=json", nil)
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="parent-tracker-audit-2026-10-05_to_2026-10-11-error.json"` {
		t.Fatalf("json name = %q", cd)
	}
	var ex struct {
		Entries []struct{ Level, Code, Detail string }
	}
	decode(t, b, &ex)
	if len(ex.Entries) != 2 || ex.Entries[0].Level != "error" || ex.Entries[1].Code != "app_sensor_unavailable" || !strings.Contains(ex.Entries[1].Detail, "x11_unreachable") {
		t.Fatalf("json export = %s", b)
	}

	// bad input is a 400 with words, not a 500
	for _, q := range []string{"from=nope", "level=fatal", "format=pdf", "from=2026-10-11&to=2026-10-01", "from=2024-01-01&to=2026-10-11"} {
		resp, b := r.do("GET", "/api/audit?"+q, nil)
		if resp.StatusCode != 400 || !strings.Contains(string(b), `"error"`) {
			t.Errorf("GET /api/audit?%s = %d %s, want 400", q, resp.StatusCode, b)
		}
	}
}

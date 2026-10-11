package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/peercred"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/tracker"
)

type rig struct {
	t      *testing.T
	uid    atomic.Int64
	client *http.Client
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := config.Default()
	cfg.MonitoredUsers = []string{"ilyes", "maram"}
	cfg.DisclosureAcknowledged = "2026-10-01T00:00:00Z"
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	for uid, name := range map[int]string{1000: "ilyes", 1001: "maram"} {
		db.UpsertUser(uid, name, true, 1)
		id, _ := db.OpenInterval(uid, model.StateActive, now.Add(-90*time.Minute).UnixMilli())
		db.CloseInterval(id, now.Add(-30*time.Minute).UnixMilli()+int64(uid-1000)*60000)
	}
	db.UpsertUser(1002, "guest", false, 1)

	r := &rig{t: t}
	srv := &Server{DB: db, Cfg: func() *config.Config { return cfg }, Loc: time.UTC, Now: func() time.Time { return now },
		Health: func() tracker.Health { return tracker.Health{Recording: true, Users: []tracker.UserHealth{}} },
		LookupUID: func(uid int) (string, error) {
			n := map[int]string{0: "root", 1000: "ilyes", 1001: "maram", 1002: "guest", 1500: "sami"}[uid]
			if n == "" {
				return "", fmt.Errorf("no such user")
			}
			return n, nil
		},
		Cred: func(net.Conn) (peercred.Cred, error) { return peercred.Cred{UID: int(r.uid.Load())}, nil }}
	path := filepath.Join(t.TempDir(), "control.sock")
	ln, err := peercred.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Serve(ctx, ln)
	r.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}, DisableKeepAlives: true}}
	return r
}

func (r *rig) get(uid int, path string) (int, map[string]any) {
	r.t.Helper()
	r.uid.Store(int64(uid))
	resp, err := r.client.Get("http://control" + path)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	json.Unmarshal(b, &m)
	return resp.StatusCode, m
}

// SEC-3, SEC-43: a monitored user sees their own record, including the hourly
// breakdown and the statement that the parent sees the current state live.
func TestChildSeesOwnData(t *testing.T) {
	r := newRig(t)
	code, m := r.get(1000, "/self")
	if code != 200 || m["user"] != "ilyes" || m["parent_sees_live"] != true {
		t.Fatalf("%d %v", code, m)
	}
	day := m["day"].(map[string]any)
	if day["active_ms"].(float64) != 60*60*1000 {
		t.Fatalf("active = %v", day["active_ms"])
	}
	if hourly := m["hourly"].(map[string]any); len(hourly["buckets"].([]any)) != 24 {
		t.Fatal("the self-view must include the hour-by-hour history")
	}
	if len(m["recorded"].([]any)) < 4 {
		t.Fatalf("recorded = %v", m["recorded"])
	}
}

// A user cannot read another user's data, with or without naming them.
func TestNobodyCanReadAnotherUsersData(t *testing.T) {
	r := newRig(t)
	code, _ := r.get(1000, "/self?user=maram")
	if code != 403 {
		t.Fatalf("asking for someone else = %d, want 403", code)
	}
	code, m := r.get(1001, "/self")
	if code != 200 || m["user"] != "maram" {
		t.Fatalf("maram sees %v", m["user"])
	}
	day := m["day"].(map[string]any)
	if day["active_ms"].(float64) != 61*60*1000 {
		t.Fatalf("maram's figure %v is not her own", day["active_ms"])
	}
}

func TestUnmonitoredAndUnknownCallers(t *testing.T) {
	r := newRig(t)
	if code, m := r.get(1002, "/self"); code != 404 || m["error"] == nil {
		t.Fatalf("an unmonitored account: %d %v", code, m)
	}
	if code, _ := r.get(4242, "/self"); code != 403 {
		t.Fatalf("an unknown uid = %d", code)
	}
	if code, _ := r.get(1500, "/self"); code != 404 { // the parent's own account is not monitored
		t.Fatalf("parent = %d", code)
	}
}

func TestRootMustNameAUserAndMayReadAnyone(t *testing.T) {
	r := newRig(t)
	if code, _ := r.get(0, "/self"); code != 400 {
		t.Fatalf("root without --user = %d", code)
	}
	code, m := r.get(0, "/self?user=maram&date=2026-10-11")
	if code != 200 || m["user"] != "maram" {
		t.Fatalf("%d %v", code, m)
	}
	if code, _ := r.get(0, "/self?user=maram&date=garbage"); code != 400 {
		t.Fatalf("bad date = %d", code)
	}
}

func TestHealthIsRootOnly(t *testing.T) {
	r := newRig(t)
	if code, _ := r.get(1000, "/health"); code != 403 {
		t.Fatalf("a normal user reading health = %d", code)
	}
	code, m := r.get(0, "/health")
	if code != 200 || m["running"] != true || m["tracker"] == nil {
		t.Fatalf("%d %v", code, m)
	}
}

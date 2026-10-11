package agent

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agentproto"
	"github.com/samiferr/parent_tracker_ubuntu/internal/agentsrv"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/peercred"
)

type scripted struct {
	name model.Provider
	mu   sync.Mutex
	n    int
	last bool
}

func (s *scripted) Name() model.Provider { return s.name }
func (s *scripted) Close()               {}
func (s *scripted) Poll(_ context.Context, titles bool) agentproto.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	s.last = titles
	t := ""
	if titles {
		t = "a title"
	}
	return agentproto.Report{Provider: string(s.name), Status: agentproto.StatusOK, App: &agentproto.App{ID: "code", Name: "Code"}, Title: t}
}

func startServer(t *testing.T, cfg *config.Config, src *collector.Fake) (string, *agentsrv.Server, context.CancelFunc) {
	t.Helper()
	srv := &agentsrv.Server{Src: src, Cfg: func() *config.Config { return cfg },
		Cred: func(net.Conn) (peercred.Cred, error) { return peercred.Cred{UID: 1000, PID: 7}, nil }}
	path := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := peercred.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Serve(ctx, ln)
	t.Cleanup(cancel)
	return path, srv, cancel
}

func testCfg() *config.Config {
	c := config.Default()
	c.MonitoredUsers = []string{"ilyes"}
	c.DisclosureAcknowledged = "2026-10-01T00:00:00Z"
	c.SampleInterval = config.Duration(time.Second)
	return c
}

func TestAgentReportsToTheDaemon(t *testing.T) {
	src := collector.NewFake()
	src.Set(collector.UserSession("2", 1000, "ilyes", "x11", "ubuntu-xorg"))
	path, srv, _ := startServer(t, testCfg(), src)

	prov := &scripted{name: model.ProviderX11}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Socket: path, SessionType: "x11", NewProvider: func(model.Provider) Provider { return prov }})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if rep, _, ok := srv.Latest("2"); ok {
			if rep.App == nil || rep.App.ID != "code" || rep.Title != "" {
				t.Fatalf("report = %+v", rep)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no report reached the daemon")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// NFR-5.3: an agent of a user who is not monitored exits having collected nothing.
func TestAgentExitsWhenNotMonitored(t *testing.T) {
	src := collector.NewFake()
	src.Set(collector.UserSession("2", 1000, "someone-else", "x11", "ubuntu-xorg"))
	path, srv, _ := startServer(t, testCfg(), src)
	prov := &scripted{name: model.ProviderX11}
	err := Run(context.Background(), Options{Socket: path, NewProvider: func(model.Provider) Provider { return prov }})
	var nm ErrNotMonitored
	if !errors.As(err, &nm) {
		t.Fatalf("err = %v", err)
	}
	if prov.n != 0 {
		t.Fatalf("the provider was polled %d times; an unmonitored user's windows must never be read", prov.n)
	}
	if _, _, ok := srv.Latest("2"); ok {
		t.Fatal("nothing may be stored")
	}
}

func TestAgentReconnectsAndFollowsConfigPushes(t *testing.T) {
	src := collector.NewFake()
	src.Set(collector.UserSession("2", 1000, "ilyes", "wayland", "ubuntu"))
	cfg := testCfg()
	path, srv, cancelServer := startServer(t, cfg, src)

	prov := &scripted{name: model.ProviderGnomeWayland}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, Options{Socket: path, Backoff: func(int) time.Duration { return 50 * time.Millisecond },
		NewProvider: func(p model.Provider) Provider {
			if p != model.ProviderGnomeWayland {
				t.Errorf("asked for provider %s on a Wayland session", p)
			}
			return prov
		}})
	waitReport := func() {
		t.Helper()
		start := time.Now()
		for {
			if _, recv, ok := srv.Latest("2"); ok && recv.After(start) {
				return
			}
			if time.Since(start) > 5*time.Second {
				t.Fatal("no fresh report")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitReport()

	// title capture switched on by the admin and reloaded: pushed to the agent
	cfg.Collectors.WindowTitle = true
	srv.Reconfigure(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for {
		prov.mu.Lock()
		on := prov.last
		prov.mu.Unlock()
		if on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the agent never received the new settings")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// the daemon goes away and comes back at the same path: the agent reconnects
	cancelServer()
	time.Sleep(100 * time.Millisecond)
	ln, err := peercred.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	srv2 := &agentsrv.Server{Src: src, Cfg: func() *config.Config { return cfg },
		Cred: func(net.Conn) (peercred.Cred, error) { return peercred.Cred{UID: 1000, PID: 7}, nil }}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go srv2.Serve(ctx2, ln)
	start := time.Now()
	for {
		if _, _, ok := srv2.Latest("2"); ok {
			break
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("agent did not reconnect")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	prev := time.Duration(0)
	for n := 0; n < 10; n++ {
		d := DefaultBackoff(n)
		if d < prev || d > 30*time.Second {
			t.Fatalf("n=%d d=%v", n, d)
		}
		prev = d
	}
	if DefaultBackoff(20) != 30*time.Second {
		t.Fatal("cap")
	}
}

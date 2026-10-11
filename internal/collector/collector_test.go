package collector

import (
	"testing"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
)

func testConfig(users ...string) *config.Config {
	c := config.Default()
	c.MonitoredUsers = users
	return c
}

func TestProviderSelection(t *testing.T) {
	cases := []struct {
		typ, desktop string
		want         model.Provider
	}{
		{"x11", "ubuntu-xorg", model.ProviderX11},
		{"x11", "xfce", model.ProviderX11},
		{"wayland", "ubuntu", model.ProviderGnomeWayland},
		{"wayland", "gnome", model.ProviderGnomeWayland},
		{"wayland", "KDE", model.ProviderNone}, // FR-3.13: not in the first release
		{"tty", "", model.ProviderNone},
		{"mir", "", model.ProviderNone},
	}
	for _, c := range cases {
		s := UserSession("1", 1000, "u", c.typ, c.desktop)
		if got := ProviderFor(s); got != c.want {
			t.Errorf("%s/%s = %s, want %s", c.typ, c.desktop, got, c.want)
		}
	}
}

func TestEligible(t *testing.T) {
	cfg := testConfig("ilyes")
	ok := UserSession("1", 1000, "ilyes", "wayland", "ubuntu")
	if !Eligible(ok, cfg) {
		t.Fatal("expected eligible")
	}
	for name, mutate := range map[string]func(*SessionInfo){
		"not on the allowlist": func(s *SessionInfo) { s.User = "guest" },
		"greeter":              func(s *SessionInfo) { s.Class = "greeter" },
		"remote":               func(s *SessionInfo) { s.Remote = true },
		"closing":              func(s *SessionInfo) { s.State = "closing" },
	} {
		s := ok
		mutate(&s)
		if Eligible(s, cfg) {
			t.Errorf("%s should not be eligible", name)
		}
	}
}

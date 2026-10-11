// Package testutil holds helpers shared by tests only.
package testutil

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

// PrivateBus starts a throwaway dbus-daemon and returns a connection to it. The
// test is skipped when dbus-daemon is not installed.
func PrivateBus(t *testing.T) (*dbus.Conn, string) {
	t.Helper()
	path, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}
	cmd := exec.Command(path, "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start dbus-daemon:", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	addr = strings.TrimSpace(addr)
	conn, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, addr
}

// Dial opens another connection to a private bus address.
func Dial(addr string) (*dbus.Conn, error) {
	conn, err := dbus.Connect(addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockFile is where the agent of one user takes its single-instance lock. It is
// in the user's runtime directory, which is private to them and cleared at the
// end of their last session.
func LockFile(runtimeDir string, uid int) string {
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", uid)
	}
	return filepath.Join(runtimeDir, "parent-tracker-agent.lock")
}

// Lock takes an exclusive, non-blocking lock on path. It reports false, with no
// error, when another agent already holds it. The lock lives as long as the
// returned release function has not been called and the process has not exited,
// so a crashed agent never leaves a stale lock behind.
//
// Two starters exist on purpose (the systemd user unit that graphical-session.target
// pulls in, and the XDG autostart entry for desktops such as Xfce that never
// reach that target), and the daemon serves one agent per session by closing the
// older connection. Without this lock two agents would keep closing each other's
// connection.
func Lock(path string) (release func(), got bool, err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, true, nil
}

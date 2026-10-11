// Package peercred identifies the process on the other end of a Unix socket
// using the kernel's SO_PEERCRED, which a local user cannot forge (SEC-13).
package peercred

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Cred is the peer's identity.
type Cred struct {
	UID int
	GID int
	PID int
}

// Of returns the credentials of the peer of a Unix-socket connection.
func Of(c net.Conn) (Cred, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return Cred{}, fmt.Errorf("not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return Cred{}, err
	}
	var cred *unix.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return Cred{}, err
	}
	if serr != nil {
		return Cred{}, serr
	}
	return Cred{UID: int(cred.Uid), GID: int(cred.Gid), PID: int(cred.Pid)}, nil
}

// Listen creates a Unix socket at path that any local user may connect to. The
// caller authenticates each connection with Of; the socket's own mode is not a
// security boundary.
func Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		_ = os.Remove(path) // stale socket from a previous run
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o666); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

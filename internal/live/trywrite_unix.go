//go:build unix

package live

import (
	"net"
	"syscall"
)

// tryWriter returns a function that writes to a socket what it takes at
// once and reports how much that was, or nil if the connection is not one
// whose socket can be reached (TLS ended in this process, a pipe in a
// test). Go's sockets do not block: a write to a full one answers "try
// again" straight away, and the net package then waits for room. This is
// that same write without the wait.
func tryWriter(nc net.Conn) func([]byte) (int, error) {
	sc, ok := nc.(syscall.Conn)
	if !ok {
		return nil
	}
	rc, err := sc.SyscallConn()
	if err != nil {
		return nil
	}
	return func(p []byte) (n int, err error) {
		cerr := rc.Write(func(fd uintptr) bool {
			for {
				n, err = syscall.Write(int(fd), p)
				if err != syscall.EINTR {
					return true // one try, whatever came of it
				}
			}
		})
		if n < 0 {
			n = 0
		}
		if cerr != nil {
			return n, cerr // the socket is closed
		}
		if err == syscall.EAGAIN {
			return n, nil
		}
		return n, err
	}
}

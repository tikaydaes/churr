//go:build !unix

package live

import "net"

// tryWriter has no way to offer a socket a write here, so every connection
// is written by a goroutine of its own (flush.go).
func tryWriter(net.Conn) func([]byte) (int, error) { return nil }

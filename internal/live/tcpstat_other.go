//go:build !linux

package live

// tcpSegments is not known on this system (tcpstat_linux.go).
func tcpSegments() (out, retransmitted int64, ok bool) { return 0, 0, false }

//go:build !linux

package live

// sendQueues is not known on this system (qdisc_linux.go).
func sendQueues() (dropped, waiting int64, ok bool) { return 0, 0, false }

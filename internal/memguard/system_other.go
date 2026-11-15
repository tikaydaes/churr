//go:build !linux

package memguard

// System is nil where there is no /proc to read: there is then no guard,
// and the server runs as it would without one.
func System(override int64) Source { return nil }

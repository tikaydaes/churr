package sqlstore

// ReadPool is how many database connections plain reads may use at once.
// The live hub sizes its resume queue from it (internal/live/resume.go).
func (d *DB) ReadPool() int {
	return d.r.Stats().MaxOpenConnections
}

package memguard

// System is the memory of the machine and the cgroup the process runs in.
// override, if not 0, is the operator's own limit on what the process may
// hold.
func System(override int64) Source { return newProcSource("/", override) }

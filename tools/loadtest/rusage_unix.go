//go:build unix

package main

import (
	"runtime"
	"syscall"
	"time"
)

// cpuUsed is the CPU time this process has used, user and system together.
func cpuUsed() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// peakRSS is the most memory this process has had resident, in bytes.
func peakRSS() int64 {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return int64(ru.Maxrss) // bytes there, kilobytes everywhere else
	}
	return int64(ru.Maxrss) * 1024
}

// raiseFileLimit lifts this process's limit on open files as far as it is
// allowed to, and returns the limit it then has. A generator holds one
// file for each connection.
func raiseFileLimit() uint64 {
	var lim syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim) != nil {
		return 0
	}
	if lim.Cur < lim.Max {
		want := lim
		want.Cur = lim.Max
		if syscall.Setrlimit(syscall.RLIMIT_NOFILE, &want) != nil && runtime.GOOS == "darwin" {
			// macOS says "unlimited" and means a number it does not tell.
			want.Cur = min(lim.Max, 65536)
			syscall.Setrlimit(syscall.RLIMIT_NOFILE, &want)
		}
		syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim)
	}
	return uint64(lim.Cur)
}

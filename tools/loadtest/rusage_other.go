//go:build !unix

package main

import "time"

// Where the system does not say what a process used, the report says zero.

func cpuUsed() time.Duration { return 0 }

func peakRSS() int64 { return 0 }

func raiseFileLimit() uint64 { return 0 }

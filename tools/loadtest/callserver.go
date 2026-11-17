package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// What the call mode reads of the server's own process, when -pid names it
// (the server is then on this machine), and the heap profile -heap saves.

// procCPU is the CPU time a process has used, user and system together,
// or false when it cannot be read.
func procCPU(pid int) (time.Duration, bool) {
	if pid == 0 {
		return 0, false
	}
	if runtime.GOOS == "linux" {
		// /proc/PID/stat: the fields after the command, which is in
		// brackets and may hold spaces; utime and stime are the 14th and
		// 15th of the line, in ticks of a hundredth of a second.
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			return 0, false
		}
		s := string(b)
		f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
		if len(f) < 13 {
			return 0, false
		}
		u, err1 := strconv.ParseInt(f[11], 10, 64)
		k, err2 := strconv.ParseInt(f[12], 10, 64)
		if err1 != nil || err2 != nil {
			return 0, false
		}
		return time.Duration(u+k) * 10 * time.Millisecond, true
	}
	out, err := exec.Command("ps", "-o", "time=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, false
	}
	return parsePsTime(strings.TrimSpace(string(out)))
}

// parsePsTime reads the CPU time `ps -o time=` prints: [[days-]hours:]
// minutes:seconds, the seconds with hundredths on macOS and the BSDs.
func parsePsTime(s string) (time.Duration, bool) {
	if s == "" {
		return 0, false
	}
	var days int64
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.ParseInt(d, 10, 64)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	total := 0.0
	for i, part := range strings.Split(s, ":") {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil || i > 2 {
			return 0, false
		}
		total = total*60 + v
	}
	return time.Duration((total + float64(days)*86400) * float64(time.Second)), true
}

// idleMemory is the server's memory before any call. The server is asked
// for its front page first: what it loads once, on its first request of
// any kind, is not the call's.
func idleMemory(cfg callConfig) int64 {
	if res, err := (&http.Client{Timeout: 10 * time.Second}).Get(cfg.base + "/"); err == nil {
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		time.Sleep(time.Second)
	}
	return rss(cfg.pid)
}

// saveHeap writes the server's heap profile, taken after a collection, to
// a file. The server must be a `-tags diag` build (cmd/churr/diag.go), and
// its profile address reachable from here.
func saveHeap(diag, path string) error {
	res, err := (&http.Client{Timeout: time.Minute}).Get(strings.TrimSuffix(diag, "/") + "/debug/pprof/heap?gc=1")
	if err != nil {
		return fmt.Errorf("fetching the heap profile (is the server a `-tags diag` build, with its profiles at %s?): %w", diag, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("fetching the heap profile was answered " + res.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, res.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

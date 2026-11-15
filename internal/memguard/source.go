package memguard

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procSource reads Linux's own account of memory: /proc/meminfo for the
// machine, and the cgroup the process is in (and every cgroup that one is
// inside) for a limit set on the service or the container. root is "/",
// except in tests.
//
// The machine: MemTotal is the limit and MemAvailable what is left. That is
// the kernel's own estimate of what can still be taken without swapping,
// page cache that can be dropped included.
//
// A cgroup: the limit is the lower of memory.max, where the kernel kills,
// and memory.high, where it slows the process to a crawl; neither is worth
// reaching. What is used is memory.current less the file pages that are
// not in active use (inactive_file), which the kernel drops first: the
// figure container tools call the working set. Version 1 has the same
// under other names.
//
// override is the operator's own figure for what the process may hold
// (docs/OPERATIONS.md), held against the process's resident size. It can
// only tighten what the system says.
type procSource struct {
	root     string
	override int64
	pageSize int64
	groups   []cgroupDir
}

// cgroupDir is one cgroup directory and the names its files have there.
type cgroupDir struct {
	dir      string
	limits   []string // files holding a limit
	usage    string
	inactive string // the line of memory.stat to take off
}

// newProcSource finds the cgroups once; what is in them is read each time.
func newProcSource(root string, override int64) *procSource {
	s := &procSource{root: root, override: override, pageSize: int64(os.Getpagesize())}
	data, err := os.ReadFile(filepath.Join(root, "proc/self/cgroup"))
	if err != nil {
		return s
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		// "0::/system.slice/churr.service" is version 2;
		// "9:memory:/docker/abc" is version 1's memory controller.
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		var base string
		var proto cgroupDir
		switch {
		case parts[0] == "0" && parts[1] == "":
			base = filepath.Join(root, "sys/fs/cgroup")
			proto = cgroupDir{limits: []string{"memory.max", "memory.high"}, usage: "memory.current", inactive: "inactive_file"}
		case contains(strings.Split(parts[1], ","), "memory"):
			base = filepath.Join(root, "sys/fs/cgroup/memory")
			proto = cgroupDir{limits: []string{"memory.limit_in_bytes"}, usage: "memory.usage_in_bytes", inactive: "total_inactive_file"}
		default:
			continue
		}
		// The cgroup itself and every one above it: a limit on any of
		// them holds. In a container the path may be the host's, which
		// is not there to be read; the top directory then is the
		// container's own.
		rel := filepath.Clean("/" + parts[2])
		for {
			d := proto
			d.dir = filepath.Join(base, rel)
			if _, err := os.Stat(filepath.Join(d.dir, d.usage)); err == nil {
				s.groups = append(s.groups, d)
			}
			if rel == "/" {
				break
			}
			rel = filepath.Dir(rel)
		}
	}
	return s
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// noLimit is the least a limit file can hold that means "none": version 1
// writes a number near the top of an int64, rounded to the page.
const noLimit = 1 << 60

func (s *procSource) Read() (Reading, bool) {
	var r Reading
	ok := false
	if total, avail, err := readMeminfo(filepath.Join(s.root, "proc/meminfo")); err == nil {
		r, ok = Reading{Total: total, Headroom: avail, From: "the machine's memory"}, true
	}
	for _, g := range s.groups {
		limit := int64(noLimit)
		for _, name := range g.limits {
			if n, err := readNumber(filepath.Join(g.dir, name)); err == nil && n > 0 && n < limit {
				limit = n
			}
		}
		if limit >= noLimit {
			continue
		}
		used, err := readNumber(filepath.Join(g.dir, g.usage))
		if err != nil {
			continue
		}
		if inactive, err := readStat(filepath.Join(g.dir, "memory.stat"), g.inactive); err == nil {
			used = max(used-inactive, 0)
		}
		left := limit - used
		if !ok || limit < r.Total {
			r.Total, r.From, r.Hard = limit, "the cgroup's memory limit ("+g.dir+")", true
		}
		if !ok || left < r.Headroom {
			r.Headroom = left
		}
		ok = true
	}
	if s.override > 0 {
		if rss, err := readResident(filepath.Join(s.root, "proc/self/statm"), s.pageSize); err == nil {
			left := s.override - rss
			if !ok || s.override < r.Total {
				r.Total, r.From = s.override, EnvLimit
			}
			if !ok || left < r.Headroom {
				r.Headroom = left
			}
			ok = true
		}
	}
	return r, ok
}

// readMeminfo returns MemTotal and MemAvailable in bytes.
func readMeminfo(path string) (total, available int64, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	total, available = -1, -1
	for len(data) > 0 && (total < 0 || available < 0) {
		var line []byte
		line, data, _ = bytes.Cut(data, []byte("\n"))
		name, rest, found := bytes.Cut(line, []byte(":"))
		if !found {
			continue
		}
		var into *int64
		switch string(name) {
		case "MemTotal":
			into = &total
		case "MemAvailable":
			into = &available
		default:
			continue
		}
		f := bytes.Fields(rest)
		if len(f) == 0 {
			continue
		}
		kb, err := strconv.ParseInt(string(f[0]), 10, 64)
		if err != nil {
			return 0, 0, err
		}
		*into = kb << 10
	}
	if total <= 0 || available < 0 {
		return 0, 0, errors.New("memguard: MemTotal or MemAvailable is missing")
	}
	return total, available, nil
}

// readNumber reads a file holding one number; "max" is no limit.
func readNumber(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v := strings.TrimSpace(string(data))
	if v == "max" {
		return noLimit, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		// Version 1 can hold a number past an int64's end.
		if _, uerr := strconv.ParseUint(v, 10, 64); uerr == nil {
			return noLimit, nil
		}
		return 0, err
	}
	return n, nil
}

// readStat reads one line of a memory.stat file.
func readStat(path, key string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for len(data) > 0 {
		var line []byte
		line, data, _ = bytes.Cut(data, []byte("\n"))
		if name, v, found := bytes.Cut(line, []byte(" ")); found && string(name) == key {
			return strconv.ParseInt(string(bytes.TrimSpace(v)), 10, 64)
		}
	}
	return 0, errors.New("memguard: no " + key + " in " + path)
}

// readResident is the process's resident size from /proc/self/statm, whose
// second field is that in pages.
func readResident(path string, pageSize int64) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	f := bytes.Fields(data)
	if len(f) < 2 {
		return 0, errors.New("memguard: statm is not as expected")
	}
	pages, err := strconv.ParseInt(string(f[1]), 10, 64)
	return pages * pageSize, err
}

// EnvLimit names the environment variable an operator sets to say how much
// memory the server may hold, or "off" for no guard (docs/OPERATIONS.md
// section 16).
const EnvLimit = "CHURR_MEMORY_LIMIT"

// ParseLimit reads the value of EnvLimit: a number of bytes, or a number
// followed by K, M or G (KiB, MiB, GiB; powers of 1024 whichever is
// written), or "off". Empty is no setting.
func ParseLimit(v string) (bytes int64, off bool, err error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false, nil
	}
	if strings.EqualFold(v, "off") {
		return 0, true, nil
	}
	i := 0
	for i < len(v) && (v[i] >= '0' && v[i] <= '9' || v[i] == '.') {
		i++
	}
	n, err := strconv.ParseFloat(v[:i], 64)
	if err != nil || n <= 0 {
		return 0, false, errors.New("not a size")
	}
	shift := 0
	switch strings.ToLower(strings.TrimSpace(v[i:])) {
	case "", "b":
	case "k", "kb", "kib":
		shift = 10
	case "m", "mb", "mib":
		shift = 20
	case "g", "gb", "gib":
		shift = 30
	default:
		return 0, false, errors.New("not a size")
	}
	return int64(n * float64(int64(1)<<shift)), false, nil
}

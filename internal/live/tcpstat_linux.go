//go:build linux

package live

import (
	"os"
	"strconv"
	"strings"
)

// tcpSegments returns how many TCP segments this machine has sent and how
// many of them were sent again, since it started, from /proc/net/snmp. ok
// is false if that cannot be read.
func tcpSegments() (out, retransmitted int64, ok bool) {
	b, err := os.ReadFile("/proc/net/snmp")
	if err != nil {
		return 0, 0, false
	}
	return parseTCPSegments(string(b))
}

// parseTCPSegments reads the two "Tcp:" lines of /proc/net/snmp: the names
// of the counters, then their values.
func parseTCPSegments(snmp string) (out, retransmitted int64, ok bool) {
	var names []string
	for _, line := range strings.Split(snmp, "\n") {
		if !strings.HasPrefix(line, "Tcp:") {
			continue
		}
		fields := strings.Fields(line)[1:]
		if names == nil {
			names = fields
			continue
		}
		found := 0
		for i, name := range names {
			if i >= len(fields) {
				break
			}
			switch name {
			case "OutSegs":
				out, _ = strconv.ParseInt(fields[i], 10, 64)
				found++
			case "RetransSegs":
				retransmitted, _ = strconv.ParseInt(fields[i], 10, 64)
				found++
			}
		}
		return out, retransmitted, found == 2
	}
	return 0, 0, false
}

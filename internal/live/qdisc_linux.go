//go:build linux

package live

import (
	"encoding/binary"
	"syscall"
)

// sendQueues returns what this machine's own transmit queues (the queueing
// disciplines of its network interfaces, what `tc -s qdisc` shows) say:
// how many packets they have dropped since the machine started, and how
// many are waiting in them now. It is asked of the kernel over a route
// netlink socket, which needs no privilege. ok is false if it cannot be.
//
// This is the one count that says packets are being lost on this machine's
// way out and nowhere else: a packet lost further along, or at a client,
// shows only as a segment sent again, which looks the same from here
// whether the cause is this machine or a phone in a tunnel.
func sendQueues() (dropped, waiting int64, ok bool) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_ROUTE)
	if err != nil {
		return 0, 0, false
	}
	defer syscall.Close(fd)
	// A read that never comes must not hold the caller for good.
	syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Sec: 1})
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return 0, 0, false
	}
	// "List every queueing discipline": a header and an empty tcmsg.
	const tcmsgLen = 20
	req := make([]byte, syscall.NLMSG_HDRLEN+tcmsgLen)
	binary.NativeEndian.PutUint32(req[0:], uint32(len(req)))
	binary.NativeEndian.PutUint16(req[4:], syscall.RTM_GETQDISC)
	binary.NativeEndian.PutUint16(req[6:], syscall.NLM_F_REQUEST|syscall.NLM_F_DUMP)
	binary.NativeEndian.PutUint32(req[8:], 1)
	if err := syscall.Sendto(fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return 0, 0, false
	}
	buf := make([]byte, 64<<10)
	for {
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil || n < syscall.NLMSG_HDRLEN {
			return 0, 0, false
		}
		msgs, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return 0, 0, false
		}
		for _, m := range msgs {
			switch m.Header.Type {
			case syscall.NLMSG_DONE:
				return dropped, waiting, true
			case syscall.NLMSG_ERROR:
				return 0, 0, false
			case syscall.RTM_NEWQDISC:
				d, w := qdiscStats(m.Data)
				dropped += d
				waiting += w
			}
		}
	}
}

// qdiscStats reads the drops and the queue length out of one queueing
// discipline's message: a tcmsg, then attributes, of which TCA_STATS is a
// struct tc_stats.
func qdiscStats(data []byte) (dropped, waiting int64) {
	const (
		tcmsgLen = 20
		tcaStats = 3
		// struct tc_stats: bytes u64, packets, drops, overlimits, bps,
		// pps, qlen, backlog u32.
		dropsAt, qlenAt, statsLen = 12, 28, 36
	)
	if len(data) < tcmsgLen {
		return 0, 0
	}
	for attrs := data[tcmsgLen:]; len(attrs) >= 4; {
		l := int(binary.NativeEndian.Uint16(attrs[0:]))
		typ := binary.NativeEndian.Uint16(attrs[2:])
		if l < 4 || l > len(attrs) {
			break
		}
		if typ == tcaStats && l-4 >= statsLen {
			v := attrs[4:l]
			return int64(binary.NativeEndian.Uint32(v[dropsAt:])), int64(binary.NativeEndian.Uint32(v[qlenAt:]))
		}
		attrs = attrs[min((l+3)&^3, len(attrs)):]
	}
	return 0, 0
}

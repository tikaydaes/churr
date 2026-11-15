//go:build linux

package live

import "testing"

func TestParseTCPSegments(t *testing.T) {
	const snmp = "Ip: Forwarding DefaultTTL\nIp: 2 64\n" +
		"Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors\n" +
		"Tcp: 1 200 120000 -1 10 20 0 3 5 1000 395645313 27264604 0 9 0\nUdp: InDatagrams\nUdp: 4\n"
	out, re, ok := parseTCPSegments(snmp)
	if !ok || out != 395645313 || re != 27264604 {
		t.Fatalf("got %d sent, %d again, %v", out, re, ok)
	}
	if _, _, ok := parseTCPSegments("Udp: a\nUdp: 1\n"); ok {
		t.Fatal("counters were found where there are none")
	}
}

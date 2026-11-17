package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseSweep(t *testing.T) {
	got, err := parseSweep("5, 25/8,25/8/20")
	want := []callShape{{5, 0, 0}, {25, 8, 0}, {25, 8, 20}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSweep = %v, %v", got, err)
	}
	for _, bad := range []string{"", "1", "5/6", "5/1/2/3", "five", "5,,10", "5/-1"} {
		if _, err := parseSweep(bad); err == nil {
			t.Errorf("parseSweep(%q) was taken", bad)
		}
	}
}

func TestParsePsTime(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"0:01.23":    1230 * time.Millisecond, // macOS
		"12:34.50":   12*time.Minute + 34500*time.Millisecond,
		"00:00:07":   7 * time.Second, // procps
		"01:02:03":   time.Hour + 2*time.Minute + 3*time.Second,
		"2-01:00:00": 49 * time.Hour,
		"1:02:03.25": time.Hour + 2*time.Minute + 3250*time.Millisecond,
	} {
		if got, ok := parsePsTime(in); !ok || got != want {
			t.Errorf("parsePsTime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "abc", "1:2:3:4", "x-00:00:01"} {
		if _, ok := parsePsTime(bad); ok {
			t.Errorf("parsePsTime(%q) was taken", bad)
		}
	}
}

func TestSweepTableAndBudget(t *testing.T) {
	rows := []*callSummary{
		{people: 10, in: 10, audioLoss: 0.001, worstAudioLoss: 0.004, audioDelay: [4]time.Duration{time.Millisecond, 2 * time.Millisecond, 3500 * time.Microsecond, 9 * time.Millisecond},
			mbitOut: 1.5, haveServer: true, serverCPUs: 0.2, serverIdle: 40 << 20, serverPeak: 60 << 20, generatorCPUs: 0.1},
		{people: 25, cameras: 8, listeners: 5, in: 30, videoLoss: 0.02, worstVideoLoss: 0.05, videoDelay: [4]time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond},
			haveServer: true, serverCPUs: 1.5, serverIdle: 40 << 20, serverPeak: 190 << 20, problems: 2},
	}
	var b strings.Builder
	printSweep(&b, rows)
	out := b.String()
	for _, want := range []string{
		"| 10 | 0 | 0 | 10 | 0.100% (0.400%) | - | 1.0 / 2.0 / 3.5 | - | 1.5 | 0.20 | 0.020 | 60.0 MiB | 2.0 MiB | 0.10 | 0 |",
		"| 25 | 8 | 5 | 30 | 0.000% (0.000%) | 2.000% (5.000%) | - | 1.0 / 1.0 / 1.0 | 0.0 | 1.50 | 0.050 | 190.0 MiB | 5.0 MiB | 0.00 | 2 |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the table lacks the row\n%s\nin\n%s", want, out)
		}
	}
	if err := overBudget(rows, 0); err != nil {
		t.Errorf("no budget: %v", err)
	}
	if err := overBudget(rows, 5); err != nil {
		t.Errorf("a budget of 5 MiB that both calls meet: %v", err)
	}
	if err := overBudget(rows, 4); err == nil || !strings.Contains(err.Error(), "25 people") {
		t.Errorf("a budget of 4 MiB that the second call passes: %v", err)
	}
	if err := overBudget([]*callSummary{{people: 2, in: 2}}, 4); err == nil {
		t.Error("a budget with no -pid was taken")
	}
	// A call held after a larger one is marked, and not judged: the
	// server still has the larger one's memory.
	rows[1].leftover = true
	b.Reset()
	printSweep(&b, rows)
	if !strings.Contains(b.String(), "| ≤ 190.0 MiB | ≤ 5.0 MiB |") {
		t.Errorf("a call after a larger one is not marked:\n%s", b.String())
	}
	if err := overBudget(rows, 4); err != nil {
		t.Errorf("a call after a larger one was judged: %v", err)
	}
}

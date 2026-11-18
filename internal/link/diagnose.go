package link

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Diagnose writes down what this server's link holds that a test waiting in
// vain would want to see: its counters of rejections and drops by reason,
// and for every stream its record version, key epoch and counters of each
// origin, what it holds aside and what each held event waits for, the
// requests for history it has sent and not had answered, the requests it
// keeps to answer later, what a holder keeps ahead of its record, and the
// holder's lists of history owed and short histories given. It is for the
// failure output of tests (testkit) and costs nothing until it is called.
// The result is at most max bytes.
func (s *Service) Diagnose(ctx context.Context, max int) string {
	var b strings.Builder
	w := func(format string, a ...any) {
		if b.Len() < max {
			fmt.Fprintf(&b, format, a...)
		}
	}
	st := s.StatsNow()
	w("stats: rejected %v refused %v dropped %v gaps %d held %d held_expired %d held_shed %d reopened %d history_asked %d apply_failed %d snapshots %d\n",
		st.Rejected, st.Refused, st.Dropped, st.Gaps, st.Held, st.HeldExpired, st.HeldShed, st.Reopened, st.HistoryAsked, st.ApplyFailed, st.Snapshots)
	streams, err := s.store.Streams().List(ctx, "")
	if err != nil {
		w("streams: %v\n", err)
		return clip(b.String(), max)
	}
	for _, sm := range streams {
		w("stream %s: state %s kind %s sequencer %s home_hub %q key_epoch %d last_seq %d servers_version %d", sm.ID, sm.State, sm.Kind, sm.Sequencer, sm.HomeHub, sm.KeyEpoch, sm.LastSeq, sm.ServersVersion)
		if rec, err := s.channelOf(ctx, s.store, sm.ID); err == nil {
			w(" record_version %d record_home %q", rec.Version, rec.HomeHub)
		}
		w("\n")
		if servers, err := s.store.Streams().Servers(ctx, sm.ID); err == nil {
			for _, ss := range servers {
				w("  origin %s: state %s last_n %d logged_n %d\n", ss.Name, ss.State, ss.LastN, ss.LoggedN)
			}
		}
		if owed := s.HistoryOwed(ctx, sm.ID); len(owed) > 0 {
			w("  history owed by %v\n", owed)
		}
		if short := s.ShortGiven(ctx, sm.ID); len(short) > 0 {
			w("  short history given to %v\n", short)
		}
		if _, found, err := s.store.Settings().Get(ctx, historyPendingKey(sm.ID)); err == nil && found {
			w("  waiting for a whole history (history_pending)\n")
		}
		s.mu.Lock()
		rt := s.rts[sm.ID]
		s.mu.Unlock()
		if rt == nil {
			continue
		}
		rt.mu.Lock()
		w("  runtime: catching %v asked %v cursor %d sent %d acked %d stalled %v history_here %v announcing %v inflight %d unread_from %d\n",
			rt.catching, rt.asked, rt.cursor, rt.sent, rt.acked, rt.stalled, rt.historyHere, rt.announcing, rt.inflight, rt.unreadFrom)
		for msg, of := range rt.firstAsked {
			if !rt.firstAnswered[msg] {
				w("  history requested of %s and not answered whole: msg %s\n", of, msg)
			}
		}
		if len(rt.keyAsked) > 0 || len(rt.hubAsked) > 0 {
			w("  keys asked for: %v, of the home hub: %v\n", sortedKeys(rt.keyAsked), len(rt.hubAsked))
		}
		for _, e := range rt.earlyAsks {
			w("  request kept to answer later: %s %s from msg %s, until %s\n", e.m.Kind, e.m.Re, e.m.MsgID, time.Until(e.until).Round(time.Millisecond))
		}
		for _, e := range rt.earlyKeys {
			w("  key kept ahead of its record: msg %s, until %s\n", e.m.MsgID, time.Until(e.until).Round(time.Millisecond))
		}
		if len(rt.early) > 0 || rt.earlyLost || rt.earlyTaken {
			w("  envelopes kept ahead of the record: %d (lost %v, taken %v)\n", len(rt.early), rt.earlyLost, rt.earlyTaken)
			for j, e := range rt.early {
				if j == 20 {
					w("    ...\n")
					break
				}
				w("    %s n %d from peer %s\n", e.env.Origin, e.env.N, e.peer)
			}
		}
		jobs := rt.jobs
		rt.mu.Unlock()
		// What is held is the stream's own goroutine's to touch: it is read
		// there, or said to be busy.
		got := make(chan string, 1)
		select {
		case jobs <- func() { got <- describeHold(rt) }:
			select {
			case h := <-got:
				w("%s", h)
			case <-time.After(2 * time.Second):
				w("  hold: the stream's goroutine is busy and did not answer\n")
			}
		default:
			w("  hold: the stream's queue is full (%d)\n", len(jobs))
		}
	}
	return clip(b.String(), max)
}

// describeHold writes what a stream holds aside; run on its goroutine.
func describeHold(rt *streamRT) string {
	h := rt.hold
	if h == nil || (len(h.events) == 0 && len(h.shed) == 0 && len(h.failShed) == 0) {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  held: %d events; seen %v shed %v fail_shed %v\n", len(h.events), h.seen, h.shed, h.failShed)
	for j, e := range h.events {
		if j == 20 {
			b.WriteString("    ...\n")
			break
		}
		waits := "message " + string(e.target)
		if e.history {
			waits = "the channel's earlier history"
		}
		fmt.Fprintf(&b, "    %s n %d (%s) from peer %s waits for %s; failures %d; until %s\n",
			e.env.Origin, e.env.N, e.o.in.Type, e.peer, waits, e.failures, time.Until(e.until).Round(time.Millisecond))
	}
	return b.String()
}

func sortedKeys(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n[cut]\n"
}

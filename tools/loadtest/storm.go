package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The reconnect storm (-storm): what a server restart looks like from the
// server's side. Every connection is dropped at the same moment and every
// member reconnects at once, each with a `resume` from the positions it had,
// while messages go on being posted at the same rate. It reports how long it
// took until everyone was ready again, and what happened to the messages
// posted meanwhile.
//
// At once is the worst case on purpose. Real clients wait a random time
// before they reconnect (docs/CLIENTS.md section 7); -storm-spread makes
// each member wait as a client does on its first attempt, a random time
// between half a second and one second more than the spread given.

type stormRun struct {
	// spread is the `reconnect_spread_seconds` the members act on; zero
	// reconnects them all at once.
	spread time.Duration
	// startedAt is when the storm began, in nanoseconds; zero before.
	// A message posted from then on is counted here and not in the run's
	// own numbers.
	startedAt atomic.Int64

	connect, post, delivery        samples
	posted, refused, expected, got atomic.Int64
	gaps, repeats, redials         atomic.Int64
	reconnected, failed, tooMany   atomic.Int64
	lostBefore, lostAfter          int64
	members                        int
	allReady, dropTook             time.Duration
	ran                            bool
}

// frame takes one frame a member received: it keeps the member's position in
// the frame's conversation, checks that the conversation's events arrive in
// order with none missing and none twice, and counts the frame's delivery if
// it is a message posted during the storm (and then returns true).
func (s *stormRun) frame(m *member, frame []byte) bool {
	if bytes.Contains(frame, []byte(`"seq":`)) {
		var e struct {
			ConversationID model.ConversationID `json:"conversation_id"`
			Seq            int64                `json:"seq"`
		}
		if json.Unmarshal(frame, &e) == nil && e.ConversationID != "" && e.Seq > 0 {
			m.mu.Lock()
			if m.pos == nil {
				m.pos = map[model.ConversationID]int64{}
			}
			if last, known := m.pos[e.ConversationID]; known {
				switch {
				case e.Seq <= last:
					s.repeats.Add(1)
				case e.Seq != last+1:
					s.gaps.Add(1)
				}
			}
			if e.Seq > m.pos[e.ConversationID] {
				m.pos[e.ConversationID] = e.Seq
			}
			m.mu.Unlock()
		}
	}
	start := s.startedAt.Load()
	if start == 0 {
		return false
	}
	sent, ok := sentAt(frame)
	if !ok || sent < start {
		return false
	}
	s.got.Add(1)
	s.delivery.add(time.Duration(time.Now().UnixNano() - sent))
	return true
}

// resume opens a member's connection again and sends `resume` with the
// positions it holds. Frames that arrive before `ready` are the replay, and
// are handed on like any others.
func (s *stormRun) resume(base string, m *member, arrived func(*member, []byte)) error {
	// Thousands of connections opened in the same instant overflow the
	// operating system's queue of connections waiting to be accepted, and
	// it refuses the rest before the server ever sees them. A client tries
	// again, and so does this, a moment later, for up to a minute.
	var ws *wsConn
	for began := time.Now(); ; {
		var err error
		ws, err = dialWS("ws"+strings.TrimPrefix(base, "http")+"/api/v1/events", m.token, live.Subprotocol, 60*time.Second)
		if err == nil {
			break
		}
		refused := errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
		if !refused || time.Since(began) > time.Minute {
			return err
		}
		s.redials.Add(1)
		time.Sleep(50*time.Millisecond + time.Duration(rand.Int63n(int64(150*time.Millisecond))))
	}
	ws.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	if _, err := ws.read(); err != nil { // hello
		ws.close()
		return err
	}
	m.mu.Lock()
	resume, _ := json.Marshal(map[string]any{"type": "resume", "positions": m.pos})
	m.mu.Unlock()
	if err := ws.writeText(resume); err != nil {
		// The server closed first; what it said is still to be read.
		if _, why := ws.read(); errors.Is(why, errClosed) {
			err = why
		}
		ws.close()
		return err
	}
	// The heartbeat is owed from `hello` on, and a resume may wait its turn
	// for a while (docs/EVENTS.md section 2).
	waited := make(chan struct{})
	defer close(waited)
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-waited:
				return
			case <-t.C:
				ws.writeText([]byte(`{"type":"ping"}`))
			}
		}
	}()
	for {
		frame, err := ws.read()
		if err != nil {
			ws.close()
			return err
		}
		if bytes.Contains(frame, []byte(`"type":"ready"`)) {
			break
		}
		arrived(m, frame)
	}
	ws.conn.SetReadDeadline(time.Time{})
	m.ws = ws
	return nil
}

func (s *stormRun) run(ctx context.Context, data, db, base string, members []*member, client *http.Client, interval time.Duration,
	pick func() (*member, model.ConversationID), inConv map[model.ConversationID]int, arrived func(*member, []byte), listen func(*member), dropped *atomic.Int64) error {
	// A client knows where every one of its conversations stands, having
	// loaded them over HTTP. For a conversation nothing was posted in
	// during the run, that is what the database says now.
	stt, err := store.Open(ctx, data, db)
	if err != nil {
		return fmt.Errorf("opening the server's database: %w", err)
	}
	last := map[model.ConversationID]int64{}
	var live []*member
	for _, m := range members {
		if m.ws == nil {
			continue
		}
		live = append(live, m)
		m.mu.Lock()
		if m.pos == nil {
			m.pos = map[model.ConversationID]int64{}
		}
		for _, c := range m.convs {
			if _, known := m.pos[c]; known {
				continue
			}
			seq, read := last[c]
			if !read {
				conv, err := stt.Conversations().Get(ctx, c)
				if err != nil {
					m.mu.Unlock()
					stt.Close()
					return err
				}
				seq = conv.LastSeq
				last[c] = seq
			}
			m.pos[c] = seq
		}
		m.mu.Unlock()
	}
	stt.Close()
	s.members = len(live)
	s.lostBefore = dropped.Load()

	fmt.Printf("Reconnect storm: dropping %d connections and reconnecting them all %s, posting going on...\n", len(live), s.how())
	s.startedAt.Store(time.Now().UnixNano())
	stopPosting := make(chan struct{})
	var posts sync.WaitGroup
	posts.Add(1)
	go func() {
		defer posts.Done()
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-stopPosting:
				return
			case <-tick.C:
			}
			m, conv := pick()
			posts.Add(1)
			go func() {
				defer posts.Done()
				body, _ := json.Marshal(map[string]any{"text": fmt.Sprintf("%s%d from %s", marker, time.Now().UnixNano(), m.id)})
				req, _ := http.NewRequest("POST", base+"/api/v1/conversations/"+string(conv)+"/messages", bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+m.token)
				req.Header.Set("Content-Type", "application/json")
				t := time.Now()
				resp, err := client.Do(req)
				if err != nil {
					s.refused.Add(1)
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusCreated {
					if s.refused.Add(1) <= 3 {
						fmt.Fprintln(os.Stderr, "  a post during the storm was answered", resp.StatusCode)
					}
					return
				}
				s.post.add(time.Since(t))
				s.posted.Add(1)
				s.expected.Add(int64(inConv[conv]))
			}()
		}
	}()
	// A second of posting to connections that are still there, so the
	// drop lands in the middle of traffic.
	time.Sleep(time.Second)

	start := time.Now()
	for _, m := range live {
		m.ws.cut.Store(true)
		m.ws.close()
		m.ws = nil
	}
	s.dropTook = time.Since(start)
	var wg sync.WaitGroup
	for _, m := range live {
		wg.Add(1)
		go func(m *member) {
			defer wg.Done()
			if s.spread > 0 {
				// The clients' rule, first attempt: between half the
				// one-second interval and the interval plus the spread.
				low, high := 500*time.Millisecond, time.Second+s.spread
				time.Sleep(low + time.Duration(rand.Int63n(int64(high-low))))
			}
			if err := s.resume(base, m, arrived); err != nil {
				if errors.Is(err, errClosed) && strings.Contains(err.Error(), "4029") {
					s.tooMany.Add(1)
				}
				if s.failed.Add(1) <= 3 {
					fmt.Fprintln(os.Stderr, "  a reconnect failed:", err)
				}
				return
			}
			s.connect.add(time.Since(start))
			s.reconnected.Add(1)
			listen(m)
		}(m)
	}
	wg.Wait()
	s.allReady = time.Since(start)
	fmt.Printf("  %d ready again, %d failed, in %s\n", s.reconnected.Load(), s.failed.Load(), s.allReady.Round(time.Millisecond))
	// Two more seconds of posting to the reconnected members, then let the
	// last messages arrive.
	time.Sleep(2 * time.Second)
	close(stopPosting)
	posts.Wait()
	for i := 0; i < 100 && s.got.Load() < s.expected.Load(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	s.lostAfter = dropped.Load()
	s.ran = true
	return nil
}

// how says when the members reconnect.
func (s *stormRun) how() string {
	if s.spread <= 0 {
		return "at once"
	}
	return fmt.Sprintf("as clients do, each after a random 0.5 to %g seconds", (time.Second + s.spread).Seconds())
}

func (s *stormRun) report() {
	if !s.ran {
		return
	}
	fmt.Println()
	fmt.Printf("Reconnect storm:             %d connections dropped at once (in %s) and reconnected %s\n", s.members, s.dropTook.Round(time.Millisecond), s.how())
	fmt.Printf("  All ready again after:     %s (%d of %d; %d failed, %d of those told to come back later with 4029)\n",
		s.allReady.Round(time.Millisecond), s.reconnected.Load(), s.members, s.failed.Load(), s.tooMany.Load())
	s.connect.report("  Drop to ready:")
	fmt.Printf("  Refused by the system:     %d connection attempts turned away before the server saw them, and made again\n", s.redials.Load())
	fmt.Printf("  Messages posted meanwhile: %d (%d refused or failed)\n", s.posted.Load(), s.refused.Load())
	s.post.report("  Post (request to answer):")
	fmt.Printf("  Deliveries:                %d of %d expected\n", s.got.Load(), s.expected.Load())
	s.delivery.report("  Delivery (post to arrival):")
	fmt.Printf("  Events out of order:       %d missing, %d twice (over the whole run)\n", s.gaps.Load(), s.repeats.Load())
	fmt.Printf("  Connections lost:          %d during the storm (not counting the drop itself)\n", s.lostAfter-s.lostBefore)
}

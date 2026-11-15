package main

import (
	"bytes"
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
//
// With several generators the drop is at the same instant on all of them:
// it is a fixed time after the start they were all given (run.go). Each
// then goes on posting until it is ready again and -storm-window is over,
// so that all of them end together and each receives what the others post.

type stormRun struct {
	g   *generator
	res *stormResult

	posted, refused                       atomic.Int64
	reconnected, failed, tooMany, redials atomic.Int64
}

// lastSeqs asks the server where each conversation stands, as a client
// knows from having loaded its conversations over HTTP. It is asked as one
// of the conversation's members.
func (s *stormRun) lastSeqs(members []*member) (map[string]int64, error) {
	g := s.g
	as := map[string]*member{}
	var convs []string
	for _, m := range members {
		for _, c := range m.convs {
			if as[c] == nil {
				as[c] = m
				convs = append(convs, c)
			}
		}
	}
	var (
		mu       sync.Mutex
		out      = make(map[string]int64, len(convs))
		firstErr error
		wg       sync.WaitGroup
		gate     = make(chan struct{}, 16)
	)
	for _, c := range convs {
		wg.Add(1)
		gate <- struct{}{}
		go func(c string) {
			defer wg.Done()
			defer func() { <-gate }()
			seq, err := func() (int64, error) {
				req, _ := http.NewRequest("GET", g.cfg.base+"/api/v1/conversations/"+c, nil)
				req.Header.Set("Authorization", "Bearer "+as[c].token)
				resp, err := g.client.Do(req)
				if err != nil {
					return 0, err
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				if resp.StatusCode != http.StatusOK {
					return 0, fmt.Errorf("asking where conversation %s stands, the server answered %d", c, resp.StatusCode)
				}
				var got struct {
					Conversation struct {
						LastSeq *int64 `json:"last_seq"`
					} `json:"conversation"`
				}
				if err := json.Unmarshal(body, &got); err != nil || got.Conversation.LastSeq == nil {
					return 0, fmt.Errorf("conversation %s came back without a last_seq", c)
				}
				return *got.Conversation.LastSeq, nil
			}()
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			out[c] = seq
		}(c)
	}
	wg.Wait()
	return out, firstErr
}

// resume opens a member's connection again and sends `resume` with the
// positions it holds. Frames that arrive before `ready` are the replay, and
// are handed on like any others.
func (s *stormRun) resume(m *member) error {
	g := s.g
	// Thousands of connections opened in the same instant overflow the
	// operating system's queue of connections waiting to be accepted, and
	// it refuses the rest before the server ever sees them. A client tries
	// again, and so does this, a moment later, for up to a minute.
	var ws *wsConn
	for began := time.Now(); ; {
		var err error
		ws, err = dialWS(g.wsURL, m.token, live.Subprotocol, 60*time.Second, g.cfg.compress)
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
	resume := make([]byte, 0, 64+64*len(m.convs))
	resume = append(resume, `{"type":"resume","positions":{`...)
	m.mu.Lock()
	for i, c := range m.convs {
		if m.pos[i] == 0 {
			continue
		}
		if resume[len(resume)-1] != '{' {
			resume = append(resume, ',')
		}
		resume = append(resume, '"')
		resume = append(resume, c...)
		resume = append(resume, `":`...)
		resume = appendInt(resume, m.pos[i])
	}
	m.mu.Unlock()
	resume = append(resume, `}}`...)
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
		t := time.NewTicker(pingEvery)
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
		if h, ok := scanFrame(frame); ok && bytes.Equal(h.typ, typeReady) {
			break
		}
		g.arrived(m, frame)
	}
	ws.conn.SetReadDeadline(time.Time{})
	g.setWS(m, ws)
	return nil
}

// run is the storm. It is called when the run's own posting has ended, and
// begins posting again at `begin`; the drop is stormLead after that.
func (s *stormRun) run(begin time.Time) error {
	g := s.g
	var online []*member
	for _, m := range g.members {
		if g.wsOf(m) != nil {
			online = append(online, m)
		}
	}
	s.res.Members = int64(len(online))
	// While nothing is being posted, by this generator or any other, learn
	// where every conversation stands. A member that saw nothing arrive in
	// a conversation during the run resumes from there.
	time.Sleep(time.Second)
	seqs, err := s.lastSeqs(online)
	if err != nil {
		return err
	}
	drop := begin.Add(stormLead)
	if time.Now().After(begin) {
		return fmt.Errorf("asking where %d conversations stand took until after the storm was to begin", len(seqs))
	}
	lostBefore := g.lostCount.Load()

	sleepUntil(begin)
	fmt.Printf("Reconnect storm: dropping %d connections and reconnecting them all %s, posting going on...\n", len(online), stormHow(g.cfg.stormSpread))
	stopPosting := make(chan struct{})
	postingDone := make(chan struct{})
	go func() {
		defer close(postingDone)
		g.postUntil(begin, time.Time{}, stopPosting, true)
	}()

	sleepUntil(drop)
	start := time.Now()
	for _, m := range online {
		ws := g.wsOf(m)
		ws.cut.Store(true)
		ws.close()
		m.mu.Lock()
		m.ws = nil
		for i, c := range m.convs {
			if m.pos[i] == 0 {
				m.pos[i] = seqs[c]
			}
		}
		m.mu.Unlock()
	}
	s.res.DropTook = time.Since(start)
	var wg sync.WaitGroup
	for _, m := range online {
		wg.Add(1)
		go func(m *member) {
			defer wg.Done()
			if spread := g.cfg.stormSpread; spread > 0 {
				// The clients' rule, first attempt: between half the
				// one-second interval and the interval plus the spread.
				low, high := 500*time.Millisecond, time.Second+spread
				time.Sleep(low + time.Duration(rand.Int63n(int64(high-low))))
			}
			if err := s.resume(m); err != nil {
				if errors.Is(err, errClosed) && strings.Contains(err.Error(), "4029") {
					s.tooMany.Add(1)
				}
				if s.failed.Add(1) <= 3 {
					fmt.Fprintln(os.Stderr, "  a reconnect failed:", err)
				}
				return
			}
			s.res.Connect.add(time.Since(start))
			s.reconnected.Add(1)
			g.listen(m)
		}(m)
	}
	wg.Wait()
	s.res.AllReady = time.Since(start)
	fmt.Printf("  %d ready again, %d failed, in %s\n", s.reconnected.Load(), s.failed.Load(), s.res.AllReady.Round(time.Millisecond))

	// Posting goes on for two more seconds to the reconnected members, and
	// to the end of the window, whichever is later.
	end := time.Now().Add(2 * time.Second)
	if window := drop.Add(g.cfg.stormWindow); g.cfg.stormWindow > 0 {
		if end.After(window) {
			s.res.Overran = 1
		} else {
			end = window
		}
	}
	sleepUntil(end)
	close(stopPosting)
	<-postingDone
	// Let the last messages arrive: until they have, for one generator
	// alone; for a fixed time when others are posting too.
	if g.cfg.of == 1 {
		for i := 0; i < 100; i++ {
			g.mu.Lock()
			want := expected(g.stormPostsByCon, g.res.ConnectedByConv)
			g.mu.Unlock()
			if s.res.Delivery.count() >= want {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	} else {
		sleepUntil(end.Add(settle))
	}
	s.res.Lost = g.lostCount.Load() - lostBefore
	return nil
}

// finish moves the storm's counts into its result.
func (s *stormRun) finish() {
	r := s.res
	r.Posted, r.Refused = s.posted.Load(), s.refused.Load()
	r.Reconnected, r.Failed, r.TooMany, r.Redials = s.reconnected.Load(), s.failed.Load(), s.tooMany.Load(), s.redials.Load()
	r.Received = r.Delivery.count()
}

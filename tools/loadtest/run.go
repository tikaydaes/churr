package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/tikaydaes/churr/internal/wirepb"
)

// A generator: the part of a load test that acts as members. It connects
// its slice of the online members, posts its share of the messages, and
// measures. It needs the members file and the server's address, and no
// database.
//
// Every generator of a run follows one timetable, counted from the moment
// posting starts (-start-at, or two seconds after the last connection when
// there is one generator and no time was given):
//
//	start                       posting begins
//	start + duration            posting ends
//	  + 10s (settle)            late messages have arrived; a run without -storm ends
//	  + 10s                     -storm: posting begins again
//	  + 11s                     -storm: every connection is dropped, and reconnects
//	  ...                       until everyone is ready again and the window is over
//
// The generators agree on these moments through their clocks alone, so the
// machines' clocks must agree (NTP or chrony) to within what is being
// measured.

const (
	settle     = 10 * time.Second // from the last post to the end, or to the storm
	stormLead  = time.Second      // posting before the drop, so it lands in traffic
	dialers    = 50               // handshakes at a time, for each generator
	pingEvery  = 25 * time.Second
	settleLone = 100 * time.Millisecond
)

type config struct {
	base         string
	shard, of    int
	rate         float64 // of the whole run
	duration     time.Duration
	generalShare float64
	startAt      time.Time // zero: as soon as everyone is connected
	pid          int
	storm        bool
	stormSpread  time.Duration
	stormWindow  time.Duration
	out          string
	// compress asks the server to compress, as apps and browsers do.
	compress bool
	// proto speaks protobuf, on the live connections and in the posts,
	// always compressed (-wire protobuf).
	proto bool
	// idle leaves out the `activity` frames a client sends while its
	// person is doing something.
	idle bool
	// viewing is the share of members that have #general open. At 1
	// nobody says what they have open, as clients from before lazy
	// delivery do not; below 1 every member says, and this share names
	// #general.
	viewing float64
	// cross is the name the members' one conversation is counted under
	// when it is a channel shared between servers, each with a generator
	// of its own: every server has its own identifier for it.
	cross string
}

type member struct {
	id    string
	token string
	// convs are the member's conversations, #general first.
	convs []string
	ws    *wsConn
	// viewer says this member has #general open (config.viewing).
	viewer bool

	// pos is the last seq seen in each of convs, as a client keeps it for
	// `resume`; 0 is none yet.
	mu  sync.Mutex
	pos []int64
}

type generator struct {
	cfg     config
	wsURL   string
	general string
	members []*member
	res     *result
	client  *http.Client
	rng     *rand.Rand

	stop chan struct{}
	// stormAt is when the storm's posting begins, in Unix nanoseconds, once
	// that is known. A message sent from then on is counted with the storm.
	stormAt atomic.Int64
	storm   *stormRun

	frames, frameBytes, unread atomic.Int64
	// signals counts conversation.changed frames, and fulls the events of
	// a conversation that arrived in full.
	signals, fulls atomic.Int64
	// wireBytes is what the frames took on the sockets, as the server
	// sent them: compressed if it compresses, with their headers.
	wireBytes                    atomic.Int64
	gaps, repeats, early         atomic.Int64
	posted, refused              atomic.Int64
	connectFailed                atomic.Int64
	mu                           sync.Mutex // guards the maps below
	lost                         map[string]int64
	lostCount                    atomic.Int64
	postsByConv, stormPostsByCon map[string]int64
}

// membersOf turns a slice of the members file into members to act as.
func membersOf(f *membersFile, slice []fileMember) []*member {
	out := make([]*member, len(slice))
	for i, fm := range slice {
		m := &member{id: fm.ID, token: fm.Token, convs: make([]string, 0, len(fm.In)+1)}
		m.convs = append(m.convs, f.General)
		for _, c := range fm.In {
			m.convs = append(m.convs, f.Channels[c])
		}
		m.pos = make([]int64, len(m.convs))
		out[i] = m
	}
	return out
}

// rss is a process's resident memory in bytes, as `ps` reports it, or 0.
func rss(pid int) int64 {
	if pid == 0 {
		return 0
	}
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	kb, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return kb * 1024
}

// closeReason says in a few words what ended a connection, the same words
// for the same cause, so that causes can be counted.
func closeReason(err error) string {
	var op *net.OpError
	switch {
	case errors.Is(err, errClosed):
		return "closed by the server: " + strings.TrimSpace(strings.TrimPrefix(err.Error(), errClosed.Error()+":"))
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "ended without a close frame"
	case errors.Is(err, syscall.ECONNRESET):
		return "reset"
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "nothing arrived in time"
	case errors.As(err, &op):
		return op.Op + ": " + op.Err.Error() // without the addresses, which differ for each
	}
	return err.Error()
}

// arrived takes one frame from a member's connection.
func (g *generator) arrived(m *member, frame []byte) {
	g.frames.Add(1)
	g.frameBytes.Add(int64(len(frame)))
	var convText [36]byte
	h, ok := scanAny(g.cfg.proto, frame, &convText)
	if !ok {
		g.unread.Add(1)
		return
	}
	if bytes.Equal(h.typ, typeConversationChanged) {
		// Told that a conversation it is not showing changed. The seq
		// in it is not an event's: nothing is held to be in order.
		g.signals.Add(1)
		return
	}
	// The conversation's events must arrive in order, none missing and
	// none twice.
	if h.seq > 0 && len(h.conv) > 0 {
		g.fulls.Add(1)
		m.mu.Lock()
		for i, c := range m.convs {
			if string(h.conv) != c {
				continue
			}
			if last := m.pos[i]; last != 0 {
				switch {
				case h.seq <= last:
					g.repeats.Add(1)
				case h.seq != last+1:
					g.gaps.Add(1)
				}
			}
			if h.seq > m.pos[i] {
				m.pos[i] = h.seq
			}
			break
		}
		m.mu.Unlock()
	}
	if h.message == nil || !bytes.Equal(h.typ, typeMessageCreated) {
		return
	}
	sent, from, ok := sentAt(h.message, h.packed)
	if !ok {
		return
	}
	took := time.Duration(time.Now().UnixNano() - sent)
	if took < 0 {
		g.early.Add(1)
		took = 0
	}
	if at := g.stormAt.Load(); at != 0 && sent >= at && g.storm != nil {
		g.storm.res.Delivery.add(took)
		return
	}
	g.res.Delivery.add(took)
	if from == g.cfg.shard {
		g.res.DeliveryOwn.add(took)
	} else {
		g.res.DeliveryOther.add(took)
	}
}

// connect opens a member's live connection and waits for `ready`.
func (g *generator) connect(m *member) error {
	ws, err := dialWS(g.wsURL, m.token, subprotocolFor(g.cfg.proto), 20*time.Second, g.cfg.compress)
	if err != nil {
		return err
	}
	// A server that never says hello or ready is a failed connection, not
	// one to wait for without end.
	ws.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	if _, err := ws.read(); err != nil { // hello
		ws.close()
		return err
	}
	if err := ws.writeFrame(g.firstResume(m)); err != nil {
		ws.close()
		return err
	}
	for {
		frame, err := ws.read()
		if err != nil {
			ws.close()
			return err
		}
		var conv [36]byte
		if h, ok := scanAny(g.cfg.proto, frame, &conv); ok && bytes.Equal(h.typ, typeReady) {
			break
		}
	}
	ws.conn.SetReadDeadline(time.Time{})
	g.setWS(m, ws)
	return nil
}

// openList is the `open` field of a member's resume frame, with the comma
// before it: nothing when nobody says what they have open, #general for a
// viewer, and an empty list for the others.
func (g *generator) openList(m *member) string {
	switch {
	case g.cfg.viewing >= 1:
		return ""
	case m.viewer:
		return `,"open":["` + g.general + `"]`
	}
	return `,"open":[]`
}

func (g *generator) firstResume(m *member) []byte {
	return []byte(`{"type":"resume","positions":{}` + g.openList(m) + `}`)
}

// markViewers chooses who has #general open: the share asked for, spread
// evenly over the members so every generator has its part of them.
func (g *generator) markViewers() {
	if g.cfg.viewing >= 1 {
		return
	}
	shown := 0.0
	for _, m := range g.members {
		shown += g.cfg.viewing
		if shown >= 1 {
			shown--
			m.viewer = true
		}
	}
}

// listen reads a member's connection until it ends. Every connection is
// read from the moment it is ready, as a client reads: while the others
// connect, the server tells it of each one coming online, and a connection
// that reads nothing is cut off as a slow consumer.
func (g *generator) listen(m *member) {
	ws := g.wsOf(m)
	go func() {
		for {
			frame, err := ws.read()
			if err != nil {
				select {
				case <-g.stop:
				default:
					if ws.cut.Load() {
						return // dropped on purpose, by the storm
					}
					why := closeReason(err)
					g.mu.Lock()
					g.lost[why]++
					g.mu.Unlock()
					if g.lostCount.Add(1) <= 3 {
						fmt.Fprintln(os.Stderr, "  a connection was lost:", why)
					}
				}
				return
			}
			g.wireBytes.Add(int64(ws.wire))
			g.arrived(m, frame)
		}
	}()
}

// heartbeats sends the ping each client owes the server, from one place: a
// hundredth of the connections every quarter second, so each is pinged
// every 25 seconds and the pings never come in a burst. Each is written on
// its own: a write waits while a connection's socket is full, and one
// connection that is slow to take its ping must not keep the others'
// pings from being sent, or the server closes them all for no heartbeat.
func (g *generator) heartbeats() {
	const parts = 100
	t := time.NewTicker(pingEvery / parts)
	defer t.Stop()
	ping := frameOut(g.cfg.proto, []byte(`{"type":"ping"}`))
	// Someone who is posting is at their keyboard, and their client says
	// so, at most once a minute: here with every second ping. Without it
	// the server takes every member to have gone away five minutes after
	// connecting and tells everyone about each of them, which at fifty
	// thousand members is fifty thousand times fifty thousand presence
	// changes, hundreds of gigabytes that no posting caused.
	activity := frameOut(g.cfg.proto, []byte(`{"type":"activity"}`))
	for part, round := 0, 0; ; part = (part + 1) % parts {
		if part == 0 {
			round++
		}
		select {
		case <-g.stop:
			return
		case <-t.C:
		}
		for i := part; i < len(g.members); i += parts {
			if ws := g.wsOf(g.members[i]); ws != nil && !ws.cut.Load() {
				if !g.cfg.idle && round%2 == 0 {
					go func() { ws.writeMade(ping); ws.writeMade(activity) }()
					continue
				}
				go ws.writeMade(ping)
			}
		}
	}
}

// wsOf is a member's connection as it is now. The storm replaces it.
func (g *generator) wsOf(m *member) *wsConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ws
}

func (g *generator) setWS(m *member, ws *wsConn) {
	m.mu.Lock()
	m.ws = ws
	m.mu.Unlock()
}

// pick chooses who posts next and where: a member of this generator's
// slice, in #general for the share given and otherwise in one of the
// member's own channels.
func (g *generator) pick() (*member, string) {
	m := g.members[g.rng.Intn(len(g.members))]
	conv := g.general
	if g.rng.Float64() >= g.cfg.generalShare && len(m.convs) > 1 {
		conv = m.convs[1+g.rng.Intn(len(m.convs)-1)]
	}
	return m, conv
}

// post posts one message as a member and counts how it went: in the storm's
// numbers if storm is true, in the run's own otherwise.
func (g *generator) post(m *member, conv string, storm bool) {
	body := make([]byte, 0, 128)
	body = append(body, `{"text":"`...)
	body = postText(body, time.Now().UnixNano(), g.cfg.shard, m.id)
	body = append(body, `"}`...)
	contentType := "application/json"
	if g.cfg.proto {
		// As an app posts: the body and the answer in protobuf, both
		// compressed (the client asks for gzip and unpacks it unasked).
		packed, err := postBody(body)
		if err != nil {
			fmt.Fprintln(os.Stderr, "  a post has no protobuf form:", err)
			return
		}
		body, contentType = packed, wirepb.ContentType(wirepb.SchemaVersion)
	}
	req, _ := http.NewRequest("POST", g.cfg.base+"/api/v1/conversations/"+conv+"/messages", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+m.token)
	req.Header.Set("Content-Type", contentType)
	if g.cfg.proto {
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Accept", contentType)
	}
	refused, posted, took, byConv := &g.refused, &g.posted, g.res.Post, g.postsByConv
	if storm {
		refused, posted, took, byConv = &g.storm.refused, &g.storm.posted, g.storm.res.Post, g.stormPostsByCon
	}
	t := time.Now()
	resp, err := g.client.Do(req)
	if err != nil {
		if refused.Add(1) <= 3 {
			fmt.Fprintln(os.Stderr, "  a post failed:", err)
		}
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		if refused.Add(1) <= 3 {
			fmt.Fprintln(os.Stderr, "  a post was answered", resp.StatusCode)
		}
		return
	}
	took.add(time.Since(t))
	posted.Add(1)
	g.mu.Lock()
	byConv[g.countedAs(conv)]++
	g.mu.Unlock()
}

// countedAs is the name a conversation's posts and connections are counted
// under: its identifier, or for a channel shared between servers the one
// name every generator was given for it.
func (g *generator) countedAs(conv string) string {
	if g.cfg.cross != "" && conv == g.general {
		return g.cfg.cross
	}
	return conv
}

// postUntil posts this generator's share of the rate from `from` until
// `until` or until stop is closed, and returns when the last post has been
// answered. The generators' posts are spaced so that together they come at
// even intervals: each posts every n-th message of the run.
func (g *generator) postUntil(from, until time.Time, stop <-chan struct{}, storm bool) {
	every := time.Duration(float64(time.Second) * float64(g.cfg.of) / g.cfg.rate)
	first := from.Add(every * time.Duration(g.cfg.shard-1) / time.Duration(g.cfg.of))
	var posts sync.WaitGroup
	defer posts.Wait()
	// A generator that starts late posts from now on, and does not make up
	// what it missed in a burst.
	k := int64(0)
	if behind := time.Since(first); behind > every {
		k = int64(behind/every) + 1
	}
	for ; ; k++ {
		at := first.Add(time.Duration(k) * every)
		if !until.IsZero() && !at.Before(until) {
			return
		}
		select {
		case <-stop:
			return
		case <-time.After(time.Until(at)):
		}
		m, conv := g.pick()
		posts.Add(1)
		go func() {
			defer posts.Done()
			g.post(m, conv, storm)
		}()
	}
}

// expectedAlone is how many deliveries this generator's own posts should
// have made to its own connections: the whole of it when there is one
// generator, and not otherwise.
func (g *generator) expectedAlone() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return expected(g.postsByConv, g.res.ConnectedByConv)
}

func sleepUntil(t time.Time) { time.Sleep(time.Until(t)) }

// runGenerator is the run step: everything after the members exist.
func runGenerator(cfg config, f *membersFile, sh shape) (*result, error) {
	g := &generator{cfg: cfg, general: f.General, res: newResult(), stop: make(chan struct{}),
		wsURL:  "ws" + strings.TrimPrefix(cfg.base, "http") + "/api/v1/events",
		client: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 64, MaxConnsPerHost: 64}},
		rng:    rand.New(rand.NewSource(int64(cfg.shard))),
		lost:   map[string]int64{}, postsByConv: map[string]int64{}, stormPostsByCon: map[string]int64{}}
	if cfg.cross != "" {
		// The file is this generator's own server's: all of it.
		g.members = membersOf(f, shardOf(f.Members, sh.Online, 1, 1))
	} else {
		g.members = membersOf(f, shardOf(f.Members, sh.Online, cfg.shard, cfg.of))
	}
	g.markViewers()
	if len(g.members) == 0 {
		return nil, fmt.Errorf("shard %d/%d has no members: %d are online", cfg.shard, cfg.of, sh.Online)
	}
	res := g.res
	res.Shard, res.Of, res.Shape, res.Slice = cfg.shard, cfg.of, sh, int64(len(g.members))
	if cfg.storm {
		g.storm = &stormRun{g: g, res: newStormResult()}
	}

	// One file for each connection, some for posting, and a few besides.
	if limit, need := raiseFileLimit(), uint64(len(g.members)+200); limit != 0 && limit < need {
		return nil, fmt.Errorf("this process may open %d files and needs %d, one for each connection: raise the limit (ulimit -n %d) and run again", limit, need, need)
	}
	if resp, err := http.Get(cfg.base + "/healthz"); err != nil {
		return nil, fmt.Errorf("the server does not answer at %s: %w", cfg.base, err)
	} else {
		resp.Body.Close()
	}
	idle := rss(cfg.pid)

	fmt.Printf("Connecting %d live connections...\n", len(g.members))
	started := time.Now()
	var wg sync.WaitGroup
	gate := make(chan struct{}, dialers)
	for _, m := range g.members {
		wg.Add(1)
		gate <- struct{}{}
		go func(m *member) {
			defer wg.Done()
			defer func() { <-gate }()
			t := time.Now()
			if err := g.connect(m); err != nil {
				if g.connectFailed.Add(1) <= 3 {
					fmt.Fprintln(os.Stderr, "  a connection failed:", err)
				}
				return
			}
			res.Connect.add(time.Since(t))
			g.listen(m)
		}(m)
	}
	wg.Wait()
	for _, m := range g.members {
		if g.wsOf(m) != nil {
			res.Connected++
			for _, c := range m.convs {
				// Of a #general that is delivered lazily only those
				// who have it open are sent its messages.
				if c == g.general && cfg.viewing < 1 && !m.viewer {
					continue
				}
				res.ConnectedByConv[g.countedAs(c)]++
			}
		}
	}
	res.ConnectFailed = g.connectFailed.Load()
	fmt.Printf("  %d connected, %d failed, in %s\n", res.Connected, res.ConnectFailed, time.Since(started).Round(time.Millisecond))
	if res.Connected == 0 {
		return nil, errors.New("nothing connected")
	}
	go g.heartbeats()

	// The timetable.
	start := cfg.startAt
	var late time.Duration
	if start.IsZero() {
		start = time.Now().Add(2 * time.Second)
	} else if late = time.Since(start); late > 0 {
		if late >= cfg.duration {
			return nil, fmt.Errorf("connecting ended %s after -start-at, when the run was to be over: start the generators earlier, or -start-at later", late.Round(time.Second))
		}
		fmt.Fprintf(os.Stderr, "  WARNING: connecting ended %s after -start-at; posting starts late\n", late.Round(time.Millisecond))
	} else {
		late = 0
		fmt.Printf("  waiting %s for -start-at\n", time.Until(start).Round(time.Second))
	}
	postEnd := start.Add(cfg.duration)
	stormStart := postEnd.Add(settle)
	res.StartAt = start.UnixNano()
	if g.storm != nil {
		g.stormAt.Store(stormStart.UnixNano())
	}
	sleepUntil(start)
	afterConnect := rss(cfg.pid)

	// This process's CPU and the server's memory, once a second from here.
	var serverPeak atomic.Int64
	serverPeak.Store(afterConnect)
	var busiest atomic.Int64 // millionths of all CPUs
	cpus := runtime.GOMAXPROCS(0)
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		lastCPU, lastAt := cpuUsed(), time.Now()
		for {
			select {
			case <-g.stop:
				return
			case <-t.C:
			}
			cpu, now := cpuUsed(), time.Now()
			if wall := now.Sub(lastAt); wall > 0 {
				if share := int64(float64(cpu-lastCPU) / float64(wall) / float64(cpus) * 1e6); share > busiest.Load() {
					busiest.Store(share)
				}
			}
			lastCPU, lastAt = cpu, now
			if v := rss(cfg.pid); v > serverPeak.Load() {
				serverPeak.Store(v)
			}
		}
	}()

	fmt.Printf("Posting %.1f messages a second for %s", cfg.rate, cfg.duration)
	if cfg.of > 1 {
		fmt.Printf(" (this generator: every %s message)", ordinal(cfg.of))
	}
	fmt.Println("...")
	cpuAtStart, framesAtStart, postingBegan := cpuUsed(), g.frames.Load(), time.Now()
	g.postUntil(start, postEnd, g.stop, false)
	posting := time.Since(postingBegan)
	busy := float64(cpuUsed()-cpuAtStart) / float64(posting) / float64(cpus)
	framesASecond := float64(g.frames.Load()-framesAtStart) / posting.Seconds()

	// Let the last messages arrive. One generator alone knows what to
	// expect and stops waiting when it has it; one of several cannot, and
	// waits the whole time.
	if g.storm != nil {
		if err := g.storm.run(stormStart); err != nil {
			return nil, err
		}
	} else {
		for time.Now().Before(stormStart) {
			if cfg.of == 1 && res.Delivery.count() >= g.expectedAlone() {
				break
			}
			time.Sleep(settleLone)
		}
	}
	atEnd := rss(cfg.pid)
	close(g.stop)
	for _, m := range g.members {
		if ws := g.wsOf(m); ws != nil {
			ws.close()
		}
	}

	res.Posted, res.Refused = g.posted.Load(), g.refused.Load()
	res.Received = res.Delivery.count()
	res.Gaps, res.Repeats, res.Unread, res.Early = g.gaps.Load(), g.repeats.Load(), g.unread.Load(), g.early.Load()
	res.Signals, res.Fulls, res.Viewing = g.signals.Load(), g.fulls.Load(), cfg.viewing
	g.mu.Lock()
	res.PostsByConv, res.Lost = g.postsByConv, g.lost
	if g.storm != nil {
		g.storm.finish()
		res.Storm = g.storm.res
		res.Storm.PostsByConv = g.stormPostsByCon
	}
	g.mu.Unlock()
	if cfg.pid != 0 {
		res.Server = &serverMemory{Idle: idle, AfterConnect: afterConnect, Peak: serverPeak.Load(), AtEnd: atEnd}
	}
	host, _ := os.Hostname()
	res.Generators = []genStats{{Shard: cfg.shard, Of: cfg.of, Host: host, CPUs: cpus, CPUSeconds: cpuUsed().Seconds(), Busy: busy,
		BusiestSecond: float64(busiest.Load()) / 1e6, PeakRSS: peakRSS(), Late: late, Frames: g.frames.Load(), FrameBytes: g.frameBytes.Load(), WireBytes: g.wireBytes.Load(), Wire: wireName(g.cfg.proto), FramesASecond: framesASecond}}
	return res, nil
}

func ordinal(n int) string {
	switch n {
	case 2:
		return "second"
	case 3:
		return "third"
	}
	return strconv.Itoa(n) + "th"
}

// wireName is what a result says of how its generator spoke.
func wireName(proto bool) string {
	if proto {
		return "protobuf"
	}
	return ""
}

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/calls/calltest"
	"github.com/tikaydaes/churr/internal/live"
)

// The call mode (-call): some of the file's members hold one call in one
// channel, over the real API, live connection and media port, and the
// report says what each of them received.
//
// Every participant is a Pion client (internal/calls/calltest) sending
// made-up media of the real weight: Opus-sized packets every 20 ms while
// the person talks and nothing while they do not, and for those with a
// camera the three simulcast sizes of docs/CALLS.md section 7 at their
// documented frame and bit rates. Each shows every camera but their own in
// a grid of 640x360 tiles, as a client with the call open does, and stops
// sending a size nobody is shown when the server says so. Nothing is
// encoded or decoded: what a call costs a server is forwarding, and that
// is what this measures.

type callConfig struct {
	base    string
	people  int
	cameras int
	// listeners are people beyond `people` who join, publish nothing and
	// are forwarded everything: an audience.
	listeners int
	steady    time.Duration
	talk      float64
	compress  bool
	// pid is the server's process, when it is on this machine; 0 reports
	// nothing of the server.
	pid int
	// heap, if set, is where the server's heap profile is saved at the end
	// of the measured stretch, fetched from diag.
	heap, diag string
	// brief leaves the row for each person out of the report.
	brief bool
	// idle is the server's memory before any call of this run, in bytes;
	// 0 measures it now.
	idle int64
}

// callSummary is one call's figures, as a row of the sweep's table.
type callSummary struct {
	people, cameras, listeners, in int
	seconds                        float64
	joinMedian, joinSlowest        time.Duration
	audioLoss, videoLoss           float64          // shares, of what was forwarded
	worstAudioLoss, worstVideoLoss float64          // the person who lost most
	audioDelay, videoDelay         [4]time.Duration // p50, p95, p99, worst
	mbitOut                        float64
	problems                       int
	generatorCPUs                  float64
	// Of the server, when -pid was given: serverCPUs is CPU seconds a
	// second; serverIdle and serverPeak its memory before any call and
	// at its highest during this one.
	haveServer             bool
	serverCPUs             float64
	serverIdle, serverPeak int64
	// leftover marks a call of a sweep held after a larger one: the
	// server still has memory from that one, so this call's memory is
	// at most what was read.
	leftover bool
}

// perSeat is the server's memory for the call, for each person in it.
func (c *callSummary) perSeat() float64 {
	if !c.haveServer || c.in == 0 {
		return 0
	}
	return float64(c.serverPeak-c.serverIdle) / float64(c.in)
}

// callSettle is how long after the last subscription the measuring waits,
// for key frames to arrive and layers to be chosen.
const callSettle = 6 * time.Second

// callFailure is what a calltest client's Fatalf panics with here, where
// there is no test to fail: the participant's goroutine recovers it.
type callFailure string

// callRun is what the participants share.
type callRun struct {
	cfg  callConfig
	conv string
	// ours is the participants by account, to tell a participant of this
	// run leaving from anyone else.
	mu      sync.Mutex
	left    map[string]string // account -> reason, when not "left"
	errs    map[string]int    // signalling errors, by what they said
	leaving atomic.Bool
	// The delay of every packet received while measuring is on, from the
	// publishing client making it to the receiving one having it.
	measuring              atomic.Bool
	audioDelay, videoDelay hist
}

// delay counts one packet's delay, while the steady stretch is measured.
func (r *callRun) delay(video bool, d time.Duration) {
	if !r.measuring.Load() {
		return
	}
	if video {
		r.videoDelay.add(d)
	} else {
		r.audioDelay.add(d)
	}
}

func (r *callRun) note(what string) {
	r.mu.Lock()
	r.errs[what]++
	n := r.errs[what]
	r.mu.Unlock()
	if n <= 3 {
		fmt.Fprintln(os.Stderr, "  signalling:", what)
	}
}

// callT is the calltest client's testing.TB.
type callT struct{}

func (callT) Helper()                           {}
func (callT) Logf(string, ...any)               {}
func (callT) Fatalf(format string, args ...any) { panic(callFailure(fmt.Sprintf(format, args...))) }

// callPerson is one participant.
type callPerson struct {
	run    *callRun
	m      fileMember
	camera bool
	// listener publishes nothing: no microphone, no send connection.
	listener bool
	ws       *wsConn
	c        *calltest.Client
	// cameraID is the track id of the person's own camera, once published.
	cameraID atomic.Value
	joined   time.Duration
	failed   string
	// lost is set when the live connection ended before the call did.
	lost atomic.Bool
}

// Send and Next make a live connection the calltest client's Signaller.
func (p *callPerson) Send(v any) {
	b, err := json.Marshal(v)
	if err == nil {
		err = p.ws.writeText(b)
	}
	if err != nil && !p.run.leaving.Load() {
		p.run.note("a frame could not be sent: " + err.Error())
	}
}

var callPrefix = []byte("call.")

// Next hands the client the call's frames and drops the rest: a member's
// connection also carries the chat of every channel they are in, which a
// call client has no use for and calltest would keep for ever.
func (p *callPerson) Next(time.Duration) (map[string]any, bool) {
	for {
		frame, err := p.ws.read()
		if err != nil {
			if !p.run.leaving.Load() {
				p.lost.Store(true)
				p.run.note("a live connection ended: " + closeReason(err))
			}
			return nil, false
		}
		h, ok := scanFrame(frame)
		if !ok {
			continue
		}
		if bytes.Equal(h.typ, []byte("error")) {
			p.run.note("the connection's error frame: " + string(frame))
			continue
		}
		if !bytes.HasPrefix(h.typ, callPrefix) {
			continue
		}
		var f map[string]any
		if json.Unmarshal(frame, &f) != nil {
			continue
		}
		p.saw(f)
		return f, true
	}
}

// saw does what a client does on the frames calltest only records.
func (p *callPerson) saw(f map[string]any) {
	switch f["type"] {
	case "call.error":
		// Once people are leaving, an answer still on its way from
		// someone who has just left is refused, and that is no fault.
		if p.run.leaving.Load() {
			return
		}
		p.run.note(fmt.Sprintf("call.error %v: %v", f["code"], f["message"]))
	case "call.participant_left":
		if reason, _ := f["reason"].(string); reason != "left" && f["conversation_id"] == p.run.conv {
			account, _ := f["account_id"].(string)
			p.run.mu.Lock()
			p.run.left[account] = reason
			p.run.mu.Unlock()
		}
	case "call.demand_changed":
		// Stop sending the sizes nobody is shown, as a client stops
		// encoding them.
		if id, _ := p.cameraID.Load().(string); id != "" && f["track_id"] == id {
			want := map[string]bool{}
			if l, ok := f["layers"].([]any); ok {
				for _, v := range l {
					want[fmt.Sprint(v)] = true
				}
			}
			for _, rid := range []string{"q", "h", "f"} {
				p.c.DropLayer(rid, !want[rid])
			}
		}
	}
}

// connect opens the person's live connection and waits for `ready`.
func (p *callPerson) connect() error {
	ws, err := dialWS("ws"+strings.TrimPrefix(p.run.cfg.base, "http")+"/api/v1/events", p.m.Token, live.Subprotocol, 20*time.Second, p.run.cfg.compress)
	if err != nil {
		return err
	}
	ws.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	if _, err := ws.read(); err != nil { // hello
		ws.close()
		return err
	}
	if err := ws.writeText([]byte(`{"type":"resume","positions":{},"open":[]}`)); err != nil {
		ws.close()
		return err
	}
	for {
		frame, err := ws.read()
		if err != nil {
			ws.close()
			return err
		}
		if h, ok := scanFrame(frame); ok && bytes.Equal(h.typ, typeReady) {
			break
		}
	}
	ws.conn.SetReadDeadline(time.Time{})
	p.ws = ws
	return nil
}

// join takes the person into the call: the API request for a ticket,
// `call.join`, and their media published. Joined, for a person, is when
// their own media is connected.
func (p *callPerson) join() (err error) {
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(callFailure)
			if !ok {
				panic(r)
			}
			err = errors.New(string(f))
		}
	}()
	if err := p.connect(); err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	p.c = calltest.New(callT{}, p, calltest.Options{Simulcast: true, Realistic: true, Wait: 30 * time.Second, Delay: p.run.delay})
	at := time.Now()
	req, _ := http.NewRequest("POST", p.run.cfg.base+"/api/v1/conversations/"+p.run.conv+"/call/join", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+p.m.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	var got struct {
		Call struct {
			ID string `json:"id"`
		} `json:"call"`
		Ticket string         `json:"ticket"`
		Error  map[string]any `json:"error"`
	}
	json.NewDecoder(res.Body).Decode(&got)
	res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("joining the call was answered %d %v", res.StatusCode, got.Error)
	}
	if f := p.c.Join(got.Call.ID, got.Ticket); f["type"] != "call.ok" {
		return fmt.Errorf("call.join was answered %v %v", f["type"], f["code"])
	}
	if p.listener {
		// Nothing to publish. In, for a listener, is when the connection
		// the server sends on is up: it offers one as soon as there is
		// anything to hear.
		for p.c.RecvState().String() != "connected" {
			if time.Since(at) > 30*time.Second {
				return errors.New("no media connection after 30s")
			}
			time.Sleep(5 * time.Millisecond)
		}
		p.joined = time.Since(at)
		return nil
	}
	sources := []string{"microphone"}
	if p.camera {
		sources = append(sources, "camera")
	}
	if f := p.c.Publish(sources...); f["type"] != "call.answer" {
		return fmt.Errorf("publishing was answered %v %v", f["type"], f["code"])
	}
	// Nobody talks until talk says so.
	p.c.Pause("microphone", true)
	p.cameraID.Store(p.c.TrackIDs["camera"])
	for p.c.SendState().String() != "connected" {
		if time.Since(at) > 30*time.Second {
			return errors.New("no media connection after 30s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.joined = time.Since(at)
	return nil
}

// talk makes the person speak in spurts of one to five seconds for the
// share of the time asked, and send no audio at all in between, as a
// microphone with silence suppression does.
func (p *callPerson) talk(share float64, stop <-chan struct{}) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	wait := func(d time.Duration) bool {
		select {
		case <-stop:
			return false
		case <-time.After(d):
			return true
		}
	}
	if share >= 1 {
		p.c.SetLevel(30)
		p.c.Pause("microphone", false)
		return
	}
	for {
		spurt := time.Second + time.Duration(rng.Int63n(int64(4*time.Second)))
		// The silences average what makes the talking the share asked.
		quiet := time.Duration(float64(3*time.Second) * (1 - share) / share * (0.5 + rng.Float64()))
		if !wait(quiet) {
			return
		}
		p.c.SetLevel(30)
		p.c.Pause("microphone", false)
		if !wait(spurt) {
			return
		}
		p.c.Pause("microphone", true)
		p.c.SetLevel(127)
	}
}

// callCount is what one person had received of the others at one moment.
type callCount struct {
	audioPackets, audioBytes, audioLost int64
	videoPackets, videoBytes, videoLost int64
	sentAudio, sentVideo                int64
}

func runCall(cfg callConfig, f *membersFile) (*callSummary, error) {
	if cfg.people < 2 || cfg.cameras < 0 || cfg.cameras > cfg.people || cfg.listeners < 0 {
		return nil, errors.New("-call-audio is at least 2, -call-video from 0 to -call-audio, and -call-listeners not negative")
	}
	if cfg.talk <= 0 || cfg.talk > 1 {
		return nil, errors.New("-call-talk is above 0 and at most 1")
	}
	all := cfg.people + cfg.listeners
	// The channel with the most of the file's members in it.
	in := make([]int, len(f.Channels))
	for _, m := range f.Members {
		for _, c := range m.In {
			in[c]++
		}
	}
	ch := 0
	for i := range in {
		if in[i] > in[ch] {
			ch = i
		}
	}
	if len(in) == 0 || in[ch] < all {
		return nil, fmt.Errorf("no channel of the members file has %d members in it", all)
	}
	run := &callRun{cfg: cfg, conv: f.Channels[ch], left: map[string]string{}, errs: map[string]int{}}
	var people []*callPerson
	for _, m := range f.Members {
		for _, c := range m.In {
			if c == ch && len(people) < all {
				people = append(people, &callPerson{run: run, m: m, camera: len(people) < cfg.cameras, listener: len(people) >= cfg.people})
			}
		}
	}
	raiseFileLimit()
	fmt.Printf("A call of %d people, %d with a camera, and %d more only listening, in a channel of %d members; each of the %d talks %s of the time.\n",
		cfg.people, cfg.cameras, cfg.listeners, in[ch], cfg.people, pct(cfg.talk))
	sum := &callSummary{people: cfg.people, cameras: cfg.cameras, listeners: cfg.listeners}
	if cfg.pid != 0 {
		if sum.serverIdle = cfg.idle; sum.serverIdle == 0 {
			sum.serverIdle = idleMemory(cfg)
		}
		if sum.serverIdle == 0 {
			return nil, fmt.Errorf("-pid %d: no such process on this machine", cfg.pid)
		}
		sum.haveServer = true
	}

	// The first person starts the call; the rest arrive five at a time, as
	// people arriving at a meeting do.
	stop := make(chan struct{})
	began := time.Now()
	people[0].failed = errText(people[0].join())
	var wg sync.WaitGroup
	gate := make(chan struct{}, 5)
	for _, p := range people[1:] {
		wg.Add(1)
		go func(p *callPerson) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			p.failed = errText(p.join())
		}(p)
	}
	wg.Wait()
	var in2 []*callPerson
	for i, p := range people {
		if p.failed != "" {
			fmt.Printf("  person %d did not get in: %s\n", i+1, p.failed)
			continue
		}
		in2 = append(in2, p)
	}
	fmt.Printf("  %d of %d in after %s\n", len(in2), len(people), time.Since(began).Round(time.Millisecond))
	defer func() {
		run.leaving.Store(true)
		close(stop)
		for _, p := range in2 {
			func() {
				defer func() { recover() }()
				p.c.Leave()
			}()
			p.c.Close()
			p.ws.close()
		}
	}()
	if len(in2) < 2 {
		return nil, errors.New("fewer than two people got into the call")
	}
	// Each client owes the server a ping every 25 seconds.
	go func() {
		t := time.NewTicker(pingEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				for _, p := range in2 {
					go func(ws *wsConn) {
						ws.writeText([]byte(`{"type":"ping"}`))
						ws.writeText([]byte(`{"type":"activity"}`))
					}(p.ws)
				}
			}
		}
	}()
	// Everyone shows every camera but their own: 640x360 tiles, layer h.
	subscribeFailed := 0
	for _, p := range in2 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					subscribeFailed++
					run.note(fmt.Sprint("call.subscribe: ", r))
				}
			}()
			var tiles [][3]any
			for _, o := range in2 {
				if id := o.c.TrackIDs["camera"]; o != p && id != "" {
					tiles = append(tiles, [3]any{id, 640, 360})
				}
			}
			if len(tiles) > 0 {
				p.c.Subscribe(tiles...)
			}
		}()
	}
	for _, p := range in2 {
		if !p.listener {
			go p.talk(cfg.talk, stop)
		}
	}
	time.Sleep(callSettle)

	count := func(p *callPerson) (n callCount) {
		for _, o := range in2 {
			if o == p {
				continue
			}
			if id := o.c.TrackIDs["microphone"]; id != "" {
				s := p.c.Stream(id)
				n.audioPackets += s.Packets.Load()
				n.audioBytes += s.Bytes.Load()
				n.audioLost += s.Lost()
			}
			if id := o.c.TrackIDs["camera"]; id != "" {
				s := p.c.Stream(id)
				n.videoPackets += s.Packets.Load()
				n.videoBytes += s.Bytes.Load()
				n.videoLost += s.Lost()
			}
		}
		n.sentAudio, n.sentVideo = p.c.SentAudio.Load(), p.c.SentVideo.Load()
		return n
	}
	// Loss and delay are counted from here: what happened while people
	// were still arriving is not the steady call.
	before := make([]callCount, len(in2))
	for i, p := range in2 {
		for _, o := range in2 {
			for _, id := range o.c.TrackIDs {
				if o != p {
					p.c.Stream(id).ResetStats()
				}
			}
		}
		before[i] = count(p)
	}
	run.measuring.Store(true)
	cpu0, at0 := cpuUsed(), time.Now()
	server0, _ := procCPU(cfg.pid)
	fmt.Printf("  measuring for %s from %s (%d)\n", cfg.steady, at0.UTC().Format("15:04:05"), at0.Unix())
	// The server's memory is read every two seconds; its highest counts.
	for end := at0.Add(cfg.steady); ; {
		if v := rss(cfg.pid); v > sum.serverPeak {
			sum.serverPeak = v
		}
		left := time.Until(end)
		if left <= 0 {
			break
		}
		time.Sleep(min(left, 2*time.Second))
	}
	secs := time.Since(at0).Seconds()
	run.measuring.Store(false)
	fmt.Printf("  measured until %d\n", time.Now().Unix())
	cpu := cpuUsed() - cpu0
	if server1, ok := procCPU(cfg.pid); ok {
		sum.serverCPUs = (server1 - server0).Seconds() / secs
	}
	if cfg.heap != "" {
		// With everyone still in: this is what the call holds.
		if err := saveHeap(cfg.diag, cfg.heap); err != nil {
			fmt.Fprintln(os.Stderr, "  -heap:", err)
		} else {
			fmt.Printf("  wrote the server's heap profile to %s\n", cfg.heap)
		}
	}
	after := make([]callCount, len(in2))
	for i, p := range in2 {
		after[i] = count(p)
	}

	// The report.
	joins := make([]time.Duration, 0, len(in2))
	for _, p := range in2 {
		joins = append(joins, p.joined)
	}
	sort.Slice(joins, func(i, j int) bool { return joins[i] < joins[j] })
	sum.in, sum.seconds = len(in2), secs
	sum.joinMedian, sum.joinSlowest = joins[len(joins)/2], joins[len(joins)-1]
	fmt.Printf("\nCall: %d people, %d cameras, %d only listening, measured for %.0fs\n", len(in2), cfg.cameras, cfg.listeners, secs)
	fmt.Printf("  time to join (ticket to media connected): median %s, slowest %s\n",
		joins[len(joins)/2].Round(time.Millisecond), joins[len(joins)-1].Round(time.Millisecond))
	if !cfg.brief {
		fmt.Println("  who   sent a/v pkts   audio got/lost  kbit/s   video got/lost   kbit/s  tiles  delay mean/worst  media")
	}
	var tot callCount
	var worstDelay, sumDelay time.Duration
	notConnected, frozen := 0, 0
	for i, p := range in2 {
		d := callCount{
			audioPackets: after[i].audioPackets - before[i].audioPackets, audioBytes: after[i].audioBytes - before[i].audioBytes, audioLost: after[i].audioLost,
			videoPackets: after[i].videoPackets - before[i].videoPackets, videoBytes: after[i].videoBytes - before[i].videoBytes, videoLost: after[i].videoLost,
			sentAudio: after[i].sentAudio - before[i].sentAudio, sentVideo: after[i].sentVideo - before[i].sentVideo,
		}
		// Tiles with a moving picture, of those the person shows; and
		// the delay over everything they received.
		tiles, want := 0, 0
		var dsum time.Duration
		var dn int
		var dworst time.Duration
		for _, o := range in2 {
			if o == p {
				continue
			}
			for source, id := range o.c.TrackIDs {
				s := p.c.Stream(id)
				if mean, worst := s.Delay(); worst > 0 {
					dsum += mean
					dn++
					if worst > dworst {
						dworst = worst
					}
				}
				if source == "camera" {
					want++
					// A picture is moving if packets were still arriving
					// in the last moments: at least a frame's worth more
					// than a second's fewer than a steady layer h sends.
					if s.Packets.Load() > 0 && s.Last() != 0 {
						tiles++
					}
				}
			}
		}
		mean := time.Duration(0)
		if dn > 0 {
			mean = dsum / time.Duration(dn)
		}
		state := "ok"
		if s, r := p.c.SendState().String(), p.c.RecvState().String(); p.listener && r == "connected" {
			state = "ok, listening"
		} else if s != "connected" || r != "connected" {
			state = "send " + s + ", recv " + r
			notConnected++
		}
		if p.lost.Load() {
			state += ", live connection lost"
		}
		if tiles < want {
			frozen++
		}
		if !cfg.brief {
			fmt.Printf("  %3d  %6d/%-7d  %7d/%-5d  %6.0f  %8d/%-5d  %7.0f  %2d/%-2d  %7s/%-7s  %s\n", i+1, d.sentAudio, d.sentVideo,
				d.audioPackets, d.audioLost, float64(d.audioBytes)*8/1000/secs, d.videoPackets, d.videoLost, float64(d.videoBytes)*8/1000/secs,
				tiles, want, mean.Round(100*time.Microsecond), dworst.Round(time.Millisecond), state)
		}
		if d.audioLost+d.audioPackets > 0 {
			sum.worstAudioLoss = max(sum.worstAudioLoss, float64(d.audioLost)/float64(d.audioLost+d.audioPackets))
		}
		if d.videoLost+d.videoPackets > 0 {
			sum.worstVideoLoss = max(sum.worstVideoLoss, float64(d.videoLost)/float64(d.videoLost+d.videoPackets))
		}
		tot.audioPackets += d.audioPackets
		tot.audioBytes += d.audioBytes
		tot.audioLost += d.audioLost
		tot.videoPackets += d.videoPackets
		tot.videoBytes += d.videoBytes
		tot.videoLost += d.videoLost
		tot.sentAudio += d.sentAudio
		tot.sentVideo += d.sentVideo
		sumDelay += mean
		if dworst > worstDelay {
			worstDelay = dworst
		}
	}
	n := float64(len(in2))
	share := func(lost, got int64) string {
		if lost+got == 0 {
			return "-"
		}
		return fmt.Sprintf("%.3f%%", float64(lost)/float64(lost+got)*100)
	}
	fmt.Printf("  sent: %d audio packets, %d video packets (the sizes someone is shown)\n", tot.sentAudio, tot.sentVideo)
	// Every audio packet sent goes to everyone else.
	fmt.Printf("  audio: %d received of %d forwarded to someone, %d lost by the gaps (%s); %.0f kbit/s a person\n",
		tot.audioPackets, tot.sentAudio*int64(len(in2)-1), tot.audioLost, share(tot.audioLost, tot.audioPackets), float64(tot.audioBytes)*8/1000/secs/n)
	fmt.Printf("  video: %d received, %d lost by the gaps (%s); %.0f kbit/s a person, %.1f Mbit/s out of the server in all\n",
		tot.videoPackets, tot.videoLost, share(tot.videoLost, tot.videoPackets), float64(tot.videoBytes)*8/1000/secs/n, float64(tot.videoBytes+tot.audioBytes)*8/1e6/secs)
	// Which sizes arrived since the call began, and how often a stream's
	// numbering did not follow on.
	layers := map[byte]int{}
	var seqBreaks, picBreaks, badSwitches int
	var late int64
	for _, p := range in2 {
		for _, o := range in2 {
			if id := o.c.TrackIDs["camera"]; o != p && id != "" {
				st := p.c.Stream(id)
				for k, v := range st.Layers() {
					layers[k] += v
				}
				a, b, _, d := st.Breaks()
				seqBreaks, picBreaks, badSwitches = seqBreaks+a, picBreaks+b, badSwitches+d
				late += st.Late()
			}
		}
	}
	fmt.Printf("  video since the call began: %d packets of size q, %d of h, %d of f; %d breaks in packet numbers (%d packets out of order), %d in picture numbers, %d switches not at a key frame\n",
		layers['q'], layers['h'], layers['f'], seqBreaks, late, picBreaks, badSwitches)
	fmt.Printf("  delay through the server: mean %s, worst %s\n", (sumDelay / time.Duration(len(in2))).Round(100*time.Microsecond), worstDelay.Round(time.Millisecond))
	// Every packet's delay, from the publishing client making it to the
	// receiving one having it: both are this program, so one clock.
	quantiles := func(name string, h *hist) (q [4]time.Duration) {
		if h.count() == 0 {
			return q
		}
		q = [4]time.Duration{h.quantile(0.50), h.quantile(0.95), h.quantile(0.99), time.Duration(h.max.Load()) * time.Microsecond}
		fmt.Printf("  %s delay over %d packets: half within %s, 95 in 100 within %s, 99 in 100 within %s, worst %s\n", name, h.count(),
			q[0].Round(10*time.Microsecond), q[1].Round(10*time.Microsecond), q[2].Round(10*time.Microsecond), q[3].Round(time.Millisecond))
		return q
	}
	sum.audioDelay, sum.videoDelay = quantiles("audio", &run.audioDelay), quantiles("video", &run.videoDelay)
	lossOf := func(lost, got int64) float64 {
		if lost+got == 0 {
			return 0
		}
		return float64(lost) / float64(lost+got)
	}
	sum.audioLoss, sum.videoLoss = lossOf(tot.audioLost, tot.audioPackets), lossOf(tot.videoLost, tot.videoPackets)
	fmt.Printf("  the person who lost most: %.3f%% of their audio, %.3f%% of their video\n", sum.worstAudioLoss*100, sum.worstVideoLoss*100)
	sum.mbitOut = float64(tot.videoBytes+tot.audioBytes) * 8 / 1e6 / secs
	run.mu.Lock()
	dropped := 0
	for _, p := range in2 {
		if reason, gone := run.left[p.m.ID]; gone {
			dropped++
			fmt.Printf("  dropped by the server: a participant, reason %s\n", reason)
		}
	}
	nerr := 0
	for what, k := range run.errs {
		nerr += k
		fmt.Printf("  signalling error, %d times: %s\n", k, what)
	}
	run.mu.Unlock()
	fmt.Printf("  did not get in: %d; dropped by the server: %d; media not connected at the end: %d; people missing a picture: %d; signalling errors: %d\n",
		len(people)-len(in2), dropped, notConnected, frozen, nerr+subscribeFailed*0)
	sum.problems = len(people) - len(in2) + dropped + notConnected + frozen + nerr
	sum.generatorCPUs = cpu.Seconds() / secs
	fmt.Printf("  this generator: %.1f CPU seconds in %.0fs, %.2f CPUs; peak memory %s\n",
		cpu.Seconds(), secs, sum.generatorCPUs, mib(peakRSS()))
	if sum.haveServer {
		n := float64(sum.in)
		fmt.Printf("  the server (pid %d): %.2f CPUs, %.3f a person; memory %s before any call, %s at its highest in this one: %s a person\n",
			cfg.pid, sum.serverCPUs, sum.serverCPUs/n, mib(sum.serverIdle), mib(sum.serverPeak), mib(int64(sum.perSeat())))
	}
	return sum, nil
}

// callShape is one entry of -call-sweep.
type callShape struct{ people, cameras, listeners int }

// parseSweep reads -call-sweep: calls separated by commas, each PEOPLE,
// PEOPLE/CAMERAS or PEOPLE/CAMERAS/LISTENERS.
func parseSweep(s string) ([]callShape, error) {
	var out []callShape
	for _, entry := range strings.Split(s, ",") {
		var c callShape
		parts := strings.Split(strings.TrimSpace(entry), "/")
		if len(parts) > 3 {
			return nil, fmt.Errorf("-call-sweep: %q is not PEOPLE, PEOPLE/CAMERAS or PEOPLE/CAMERAS/LISTENERS", entry)
		}
		for i, dst := range []*int{&c.people, &c.cameras, &c.listeners}[:len(parts)] {
			n, err := strconv.Atoi(parts[i])
			if err != nil || n < 0 {
				return nil, fmt.Errorf("-call-sweep: %q is not PEOPLE, PEOPLE/CAMERAS or PEOPLE/CAMERAS/LISTENERS", entry)
			}
			*dst = n
		}
		if c.people < 2 || c.cameras > c.people {
			return nil, fmt.Errorf("-call-sweep: %q needs at least 2 people, and no more cameras than people", entry)
		}
		out = append(out, c)
	}
	return out, nil
}

// sweepPause is how long the sweep waits between two calls, for the server
// to end the one and let go of what it held.
const sweepPause = 10 * time.Second

// runSweep holds one call after another, each of the next size, and prints
// a table of them. Memory is counted from before the first call: a process
// does not hand memory back to the system the moment it is freed, so each
// call's figure is what the server had grown to by then. It is the call's
// own only when no larger call came before it; otherwise the table marks
// it as an upper bound.
func runSweep(cfg callConfig, shapes []callShape, f *membersFile, budget float64) error {
	cfg.brief = true
	if cfg.pid != 0 {
		cfg.idle = idleMemory(cfg)
	}
	heap := cfg.heap
	var rows []*callSummary
	for i, sh := range shapes {
		if i > 0 {
			time.Sleep(sweepPause)
		}
		cfg.people, cfg.cameras, cfg.listeners = sh.people, sh.cameras, sh.listeners
		if heap != "" {
			// One profile for each call: FILE-25-8-0.
			cfg.heap = fmt.Sprintf("%s-%d-%d-%d", heap, sh.people, sh.cameras, sh.listeners)
		}
		fmt.Printf("\n== %d of %d ==\n", i+1, len(shapes))
		sum, err := runCall(cfg, f)
		if err != nil {
			return err
		}
		for _, before := range rows {
			if before.in > sum.in || before.cameras > sum.cameras {
				sum.leftover = true
			}
		}
		rows = append(rows, sum)
	}
	fmt.Println()
	printSweep(os.Stdout, rows)
	return overBudget(rows, budget)
}

// printSweep writes the table of a sweep: a row for each call.
func printSweep(w io.Writer, rows []*callSummary) {
	ms := func(d time.Duration) string { return fmt.Sprintf("%.1f", float64(d)/float64(time.Millisecond)) }
	delays := func(q [4]time.Duration) string {
		if q[3] == 0 {
			return "-"
		}
		return ms(q[0]) + " / " + ms(q[1]) + " / " + ms(q[2])
	}
	fmt.Fprintln(w, "| People | Cameras | Listening only | In | Audio lost (worst person) | Video lost (worst person) | Audio delay p50 / p95 / p99, ms | Video delay p50 / p95 / p99, ms | Out, Mbit/s | Server CPUs | CPUs a person | Server memory | Memory a person | Generator CPUs | Problems |")
	fmt.Fprintln(w, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, r := range rows {
		cpus, perCPU, mem, perMem := "-", "-", "-", "-"
		if r.haveServer {
			cpus, perCPU = fmt.Sprintf("%.2f", r.serverCPUs), fmt.Sprintf("%.3f", r.serverCPUs/float64(max(r.in, 1)))
			mem, perMem = mib(r.serverPeak), mib(int64(r.perSeat()))
			if r.leftover {
				mem, perMem = "≤ "+mem, "≤ "+perMem
			}
		}
		video, videoDelay := "-", "-"
		if r.cameras > 0 {
			video, videoDelay = fmt.Sprintf("%.3f%% (%.3f%%)", r.videoLoss*100, r.worstVideoLoss*100), delays(r.videoDelay)
		}
		fmt.Fprintf(w, "| %d | %d | %d | %d | %.3f%% (%.3f%%) | %s | %s | %s | %.1f | %s | %s | %s | %s | %.2f | %d |\n",
			r.people, r.cameras, r.listeners, r.in, r.audioLoss*100, r.worstAudioLoss*100, video, delays(r.audioDelay), videoDelay,
			r.mbitOut, cpus, perCPU, mem, perMem, r.generatorCPUs, r.problems)
	}
	fmt.Fprintln(w, "\nMemory a person is the server's growth since before the first call, over the people in the call; ≤ marks a call held after a larger one, whose memory the server had not yet given back. Problems counts people who did not get in, were dropped, ended without media or missed a picture, and signalling errors. A generator above 0.8 of its CPUs measures itself.")
}

// overBudget is the error -call-budget ends with: a call whose memory for
// each person passed the budget, in MiB.
func overBudget(rows []*callSummary, budget float64) error {
	if budget <= 0 {
		return nil
	}
	for _, r := range rows {
		if !r.haveServer {
			return errors.New("-call-budget needs -pid, to read the server's memory")
		}
		if r.leftover {
			continue // an upper bound proves nothing
		}
		if got := r.perSeat() / (1 << 20); got > budget {
			return fmt.Errorf("the call of %d people, %d cameras and %d listening held %.1f MiB a person, over the budget of %.1f", r.people, r.cameras, r.listeners, got, budget)
		}
	}
	return nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

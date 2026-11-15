package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sort"
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
	base     string
	people   int
	cameras  int
	steady   time.Duration
	talk     float64
	compress bool
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
	ws     *wsConn
	c      *calltest.Client
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
	p.c = calltest.New(callT{}, p, calltest.Options{Simulcast: true, Realistic: true, Wait: 30 * time.Second})
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

func runCall(cfg callConfig, f *membersFile) error {
	if cfg.people < 2 || cfg.cameras < 0 || cfg.cameras > cfg.people {
		return errors.New("-call-audio is at least 2, and -call-video from 0 to -call-audio")
	}
	if cfg.talk <= 0 || cfg.talk > 1 {
		return errors.New("-call-talk is above 0 and at most 1")
	}
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
	if len(in) == 0 || in[ch] < cfg.people {
		return fmt.Errorf("no channel of the members file has %d members in it", cfg.people)
	}
	run := &callRun{cfg: cfg, conv: f.Channels[ch], left: map[string]string{}, errs: map[string]int{}}
	var people []*callPerson
	for _, m := range f.Members {
		for _, c := range m.In {
			if c == ch && len(people) < cfg.people {
				people = append(people, &callPerson{run: run, m: m, camera: len(people) < cfg.cameras})
			}
		}
	}
	raiseFileLimit()
	fmt.Printf("A call of %d people, %d with a camera, in a channel of %d members; each talks %s of the time.\n",
		cfg.people, cfg.cameras, in[ch], pct(cfg.talk))

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
		return errors.New("fewer than two people got into the call")
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
		go p.talk(cfg.talk, stop)
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
	cpu0, at0 := cpuUsed(), time.Now()
	fmt.Printf("  measuring for %s from %s (%d)\n", cfg.steady, at0.UTC().Format("15:04:05"), at0.Unix())
	time.Sleep(cfg.steady)
	secs := time.Since(at0).Seconds()
	fmt.Printf("  measured until %d\n", time.Now().Unix())
	cpu := cpuUsed() - cpu0
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
	fmt.Printf("\nCall: %d people, %d cameras, measured for %.0fs\n", len(in2), cfg.cameras, secs)
	fmt.Printf("  time to join (ticket to media connected): median %s, slowest %s\n",
		joins[len(joins)/2].Round(time.Millisecond), joins[len(joins)-1].Round(time.Millisecond))
	fmt.Println("  who   sent a/v pkts   audio got/lost  kbit/s   video got/lost   kbit/s  tiles  delay mean/worst  media")
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
		if s, r := p.c.SendState().String(), p.c.RecvState().String(); s != "connected" || r != "connected" {
			state = "send " + s + ", recv " + r
			notConnected++
		}
		if p.lost.Load() {
			state += ", live connection lost"
		}
		if tiles < want {
			frozen++
		}
		fmt.Printf("  %3d  %6d/%-7d  %7d/%-5d  %6.0f  %8d/%-5d  %7.0f  %2d/%-2d  %7s/%-7s  %s\n", i+1, d.sentAudio, d.sentVideo,
			d.audioPackets, d.audioLost, float64(d.audioBytes)*8/1000/secs, d.videoPackets, d.videoLost, float64(d.videoBytes)*8/1000/secs,
			tiles, want, mean.Round(100*time.Microsecond), dworst.Round(time.Millisecond), state)
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
	fmt.Printf("  this generator: %.1f CPU seconds in %.0fs, %.2f of its CPUs' worth each second; peak memory %s\n",
		cpu.Seconds(), secs, cpu.Seconds()/secs, mib(peakRSS()))
	return nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

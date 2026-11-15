package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// What one generator measured, as it writes it to -out, and what several
// such files add up to. The report is always printed from a merged result,
// of one generator or of many, so a run in shards and a run in one piece
// read the same.

const resultVersion = 1

// shape says what load the run put on the server. Every generator of a run
// must have been given the same.
type shape struct {
	Prefix string `json:"prefix"`
	// Members exist; Online of them hold a connection.
	Members      int     `json:"members"`
	Online       int     `json:"online"`
	Channels     int     `json:"channels"`
	PerMember    int     `json:"per_member"`
	GeneralShare float64 `json:"general_share"`
	// Rate is messages a second over all generators.
	Rate        float64       `json:"rate"`
	Duration    time.Duration `json:"duration_ns"`
	Storm       bool          `json:"storm"`
	StormSpread time.Duration `json:"storm_spread_ns"`
	StormWindow time.Duration `json:"storm_window_ns"`
}

// genStats is what a generator used itself. A generator short of CPU reads
// its frames late, and the latencies it reports are then its own.
type genStats struct {
	Shard int    `json:"shard"`
	Of    int    `json:"of"`
	Host  string `json:"host"`
	CPUs  int    `json:"cpus"`
	// CPUSeconds is user and system time of the whole process.
	CPUSeconds float64 `json:"cpu_seconds"`
	// Busy is the share of its CPUs it used while posting, and BusiestSecond
	// the same over the worst single second from the start of posting on.
	Busy          float64 `json:"busy"`
	BusiestSecond float64 `json:"busiest_second"`
	PeakRSS       int64   `json:"peak_rss_bytes"`
	// Late is how long after -start-at it was ready to post; zero is on time.
	Late time.Duration `json:"late_ns"`
	// Frames is everything received over the live connections.
	Frames     int64 `json:"frames"`
	FrameBytes int64 `json:"frame_bytes"`
	// FramesASecond is the rate of them while posting.
	FramesASecond float64 `json:"frames_a_second_while_posting"`
}

// serverMemory is the server's resident memory, when -pid gave a process
// on the generator's own machine.
type serverMemory struct {
	Idle         int64 `json:"idle_bytes"`
	AfterConnect int64 `json:"after_connect_bytes"`
	Peak         int64 `json:"peak_bytes"`
	AtEnd        int64 `json:"at_end_bytes"`
}

type stormResult struct {
	Members     int64            `json:"members"`
	DropTook    time.Duration    `json:"drop_took_ns"`
	AllReady    time.Duration    `json:"all_ready_ns"`
	Reconnected int64            `json:"reconnected"`
	Failed      int64            `json:"failed"`
	TooMany     int64            `json:"told_to_come_back_later"`
	Redials     int64            `json:"redials"`
	Posted      int64            `json:"posted"`
	Refused     int64            `json:"refused"`
	Received    int64            `json:"received"`
	PostsByConv map[string]int64 `json:"posts_by_conversation"`
	Lost        int64            `json:"connections_lost"`
	// Overran counts the generators not ready again inside the window.
	Overran  int   `json:"overran_window"`
	Connect  *hist `json:"connect"`
	Post     *hist `json:"post"`
	Delivery *hist `json:"delivery"`
}

type result struct {
	Version int `json:"version"`
	// Shard is 0 in a merged result.
	Shard int `json:"shard"`
	Of    int `json:"of"`
	// StartAt is when posting began, in Unix nanoseconds.
	StartAt int64 `json:"start_at"`
	Shape   shape `json:"shape"`

	Generators []genStats `json:"generators"`

	// Slice is how many members the generator (or all of them) was to connect.
	Slice         int64 `json:"members_to_connect"`
	Connected     int64 `json:"connected"`
	ConnectFailed int64 `json:"connect_failed"`
	Posted        int64 `json:"posted"`
	Refused       int64 `json:"refused"`
	Received      int64 `json:"received"`
	// PostsByConv and ConnectedByConv are what the deliveries to expect are
	// worked out from once every generator's file is in: a message reaches
	// every connected member of its conversation, whichever generator
	// posted it and whichever hold the connections.
	PostsByConv     map[string]int64 `json:"posts_by_conversation"`
	ConnectedByConv map[string]int64 `json:"connected_by_conversation"`
	// Lost counts connections that ended without this program ending them,
	// by what ended them.
	Lost map[string]int64 `json:"connections_lost"`
	// Gaps and Repeats count events that arrived out of order.
	Gaps    int64 `json:"events_missing"`
	Repeats int64 `json:"events_twice"`
	// Unread counts frames the cheap reader could not follow.
	Unread int64 `json:"frames_unread"`
	// Early counts messages that arrived before they were sent, by the
	// clocks: a sign that two generators' clocks disagree.
	Early int64 `json:"arrived_before_sent"`

	Connect  *hist `json:"connect"`
	Post     *hist `json:"post"`
	Delivery *hist `json:"delivery"`
	// DeliveryOwn is the part of Delivery where the same generator posted
	// and received, which no difference between clocks can touch.
	DeliveryOwn *hist `json:"delivery_same_generator"`

	Server *serverMemory `json:"server_memory,omitempty"`
	Storm  *stormResult  `json:"storm,omitempty"`

	// part is set to print one generator of several on its own: it cannot
	// say how many deliveries to expect.
	part bool
}

func newResult() *result {
	return &result{Version: resultVersion, PostsByConv: map[string]int64{}, ConnectedByConv: map[string]int64{}, Lost: map[string]int64{},
		Connect: &hist{}, Post: &hist{}, Delivery: &hist{}, DeliveryOwn: &hist{}}
}

func newStormResult() *stormResult {
	return &stormResult{PostsByConv: map[string]int64{}, Connect: &hist{}, Post: &hist{}, Delivery: &hist{}}
}

func (r *result) write(path string) error {
	data, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func readResult(path string) (*result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := newResult()
	if err := json.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("%s is not a results file: %w", path, err)
	}
	if r.Version != resultVersion {
		return nil, fmt.Errorf("%s is a results file of version %d; this program reads version %d", path, r.Version, resultVersion)
	}
	if r.Connect == nil || r.Post == nil || r.Delivery == nil || r.DeliveryOwn == nil ||
		(r.Storm != nil && (r.Storm.Connect == nil || r.Storm.Post == nil || r.Storm.Delivery == nil)) {
		return nil, fmt.Errorf("%s is a results file with a histogram missing", path)
	}
	return r, nil
}

func addCounts(to, from map[string]int64) {
	for k, v := range from {
		to[k] += v
	}
}

// merge adds generators' results into the result of the run. It refuses
// files that are not of one run, and returns, beside the result, what the
// reader should know about how the files fit together.
func merge(parts []*result) (*result, []string, error) {
	if len(parts) == 0 {
		return nil, nil, errors.New("nothing to merge")
	}
	var notes []string
	first := parts[0]
	out := newResult()
	out.Of, out.StartAt, out.Shape = first.Of, first.StartAt, first.Shape
	seen := map[int]bool{}
	for _, p := range parts {
		if p.Shard == 0 {
			return nil, nil, errors.New("a merged results file cannot be merged again; merge the generators' own files")
		}
		if p.Of != first.Of || p.Shape != first.Shape {
			return nil, nil, fmt.Errorf("shard %d/%d was run with other settings than shard %d/%d: these are not files of one run", p.Shard, p.Of, first.Shard, first.Of)
		}
		if seen[p.Shard] {
			return nil, nil, fmt.Errorf("shard %d/%d is given twice", p.Shard, p.Of)
		}
		seen[p.Shard] = true
		if p.StartAt != first.StartAt {
			notes = append(notes, fmt.Sprintf("shard %d/%d began posting %s apart from shard %d/%d: the generators were not started with one -start-at, and their phases do not line up",
				p.Shard, p.Of, time.Duration(p.StartAt-first.StartAt).Abs().Round(time.Millisecond), first.Shard, first.Of))
		}
		out.Generators = append(out.Generators, p.Generators...)
		out.Slice += p.Slice
		out.Connected += p.Connected
		out.ConnectFailed += p.ConnectFailed
		out.Posted += p.Posted
		out.Refused += p.Refused
		out.Received += p.Received
		out.Gaps += p.Gaps
		out.Repeats += p.Repeats
		out.Unread += p.Unread
		out.Early += p.Early
		addCounts(out.PostsByConv, p.PostsByConv)
		addCounts(out.ConnectedByConv, p.ConnectedByConv)
		addCounts(out.Lost, p.Lost)
		out.Connect.merge(p.Connect)
		out.Post.merge(p.Post)
		out.Delivery.merge(p.Delivery)
		out.DeliveryOwn.merge(p.DeliveryOwn)
		if p.Server != nil && out.Server == nil {
			s := *p.Server
			out.Server = &s
		}
		if p.Storm != nil {
			if out.Storm == nil {
				out.Storm = newStormResult()
			}
			s, o := p.Storm, out.Storm
			o.Members += s.Members
			o.DropTook = max(o.DropTook, s.DropTook)
			o.AllReady = max(o.AllReady, s.AllReady)
			o.Reconnected += s.Reconnected
			o.Failed += s.Failed
			o.TooMany += s.TooMany
			o.Redials += s.Redials
			o.Posted += s.Posted
			o.Refused += s.Refused
			o.Received += s.Received
			o.Lost += s.Lost
			o.Overran += s.Overran
			addCounts(o.PostsByConv, s.PostsByConv)
			o.Connect.merge(s.Connect)
			o.Post.merge(s.Post)
			o.Delivery.merge(s.Delivery)
		}
	}
	if len(parts) != first.Of {
		var missing []int
		for i := 1; i <= first.Of; i++ {
			if !seen[i] {
				missing = append(missing, i)
			}
		}
		notes = append(notes, fmt.Sprintf("%d of %d generators' files are here (missing: %v): the counts below leave out what the others posted and received", len(parts), first.Of, missing))
	}
	sort.Slice(out.Generators, func(i, j int) bool { return out.Generators[i].Shard < out.Generators[j].Shard })
	return out, notes, nil
}

// expected is how many deliveries the posts counted should have made: each
// message once to every connected member of its conversation.
func expected(posts, connected map[string]int64) int64 {
	var n int64
	for conv, p := range posts {
		n += p * connected[conv]
	}
	return n
}

func mib(b int64) string { return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20)) }

func pct(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }

// busyLimit is the share of its CPUs above which a generator is taken to
// have been in its own way.
const busyLimit = 0.8

// report prints a run: the result of merge, of one generator or several.
func report(r *result, notes []string) {
	sh := r.Shape
	fmt.Println()
	for _, n := range notes {
		fmt.Println("NOTE:", n)
	}
	perChannel := 0
	if sh.Channels > 0 {
		perChannel = sh.Members * sh.PerMember / sh.Channels
	}
	fmt.Printf("Shape:                       %d members, %d of them online (%s); #general with everyone, and %d channels of about %d members, %d for each member\n",
		sh.Members, sh.Online, pct(float64(sh.Online)/float64(max(1, sh.Members))), sh.Channels, perChannel, sh.PerMember)
	// What a message costs the server in frames: everyone online for one in
	// #general, the online share of a channel's members for any other.
	fan := sh.GeneralShare*float64(sh.Online) + (1-sh.GeneralShare)*float64(sh.Online)*float64(sh.PerMember)/float64(max(1, sh.Channels))
	if sh.Channels == 0 || sh.PerMember == 0 {
		fan = float64(sh.Online)
	}
	fmt.Printf("                             %.1f messages a second for %s, %s of them in #general: about %.0f deliveries a second; %d generator(s)\n",
		sh.Rate, sh.Duration, pct(sh.GeneralShare), sh.Rate*fan, r.Of)
	fmt.Printf("Members connected:           %d of %d\n", r.Connected, r.Slice)
	fmt.Printf("Messages posted:             %d (%d refused or failed), %.1f a second\n", r.Posted, r.Refused, float64(r.Posted)/sh.Duration.Seconds())
	if r.part {
		fmt.Printf("Deliveries to connections:   %d (how many to expect is known once every generator's file is merged)\n", r.Received)
	} else {
		fmt.Printf("Deliveries to connections:   %d of %d expected\n", r.Received, expected(r.PostsByConv, r.ConnectedByConv))
	}
	var lost int64
	reasons := make([]string, 0, len(r.Lost))
	for why, n := range r.Lost {
		lost += n
		reasons = append(reasons, why)
	}
	sort.Slice(reasons, func(i, j int) bool {
		return r.Lost[reasons[i]] > r.Lost[reasons[j]] || (r.Lost[reasons[i]] == r.Lost[reasons[j]] && reasons[i] < reasons[j])
	})
	fmt.Printf("Connections lost:            %d\n", lost)
	for _, why := range reasons {
		fmt.Printf("  %26d   %s\n", r.Lost[why], why)
	}
	r.Connect.report("Connect (to ready):")
	r.Post.report("Post (request to answer):")
	r.Delivery.report("Delivery (post to arrival):")
	if r.Of > 1 {
		// Across machines a delivery time includes the difference between
		// two clocks. Within one generator it does not.
		r.DeliveryOwn.report("  posted by the same generator:")
	}
	if r.Early > 0 {
		fmt.Printf("                             %d messages arrived before they were sent, by the generators' clocks: the clocks disagree, and delivery times across generators are off by as much\n", r.Early)
	}
	if r.Gaps+r.Repeats+r.Unread > 0 {
		fmt.Printf("Events out of order:         %d missing, %d twice; %d frames could not be read\n", r.Gaps, r.Repeats, r.Unread)
	}
	if s := r.Server; s != nil {
		fmt.Printf("Server memory (resident):    %s before, %s with everyone connected, %s at its highest, %s at the end\n",
			mib(s.Idle), mib(s.AfterConnect), mib(s.Peak), mib(s.AtEnd))
		if r.Connected > 0 && s.AfterConnect > s.Idle {
			fmt.Printf("                             about %.0f KiB for each connected member\n", float64(s.AfterConnect-s.Idle)/1024/float64(r.Connected))
		}
	}
	var warn []string
	for _, g := range r.Generators {
		fmt.Printf("%-28s %d CPUs, %s busy while posting (busiest second %s), %.1f CPU seconds in all, %s at its highest; %.0f frames a second received while posting\n",
			fmt.Sprintf("Generator %d/%d (%s):", g.Shard, g.Of, g.Host), g.CPUs, pct(g.Busy), pct(g.BusiestSecond), g.CPUSeconds, mib(g.PeakRSS), g.FramesASecond)
		if g.Busy > busyLimit || g.BusiestSecond > busyLimit {
			warn = append(warn, fmt.Sprintf("generator %d/%d used %s of its %d CPUs while posting and %s in its busiest second. Above %s a generator reads its frames late: the latencies here are overstated. Use more generators.",
				g.Shard, g.Of, pct(g.Busy), g.CPUs, pct(g.BusiestSecond), pct(busyLimit)))
		}
		if g.Late > 0 {
			warn = append(warn, fmt.Sprintf("generator %d/%d was still connecting at -start-at and began posting %s late: it posted less than its share, and members it had not connected missed messages. Give -start-at more time.",
				g.Shard, g.Of, g.Late.Round(time.Millisecond)))
		}
	}
	for _, w := range warn {
		fmt.Println("WARNING:", w)
	}
	if s := r.Storm; s != nil {
		fmt.Println()
		fmt.Printf("Reconnect storm:             %d connections dropped at once (in %s) and reconnected %s\n", s.Members, s.DropTook.Round(time.Millisecond), stormHow(sh.StormSpread))
		fmt.Printf("  All ready again after:     %s (%d of %d; %d failed, %d of those told to come back later with 4029)\n",
			s.AllReady.Round(time.Millisecond), s.Reconnected, s.Members, s.Failed, s.TooMany)
		s.Connect.report("  Drop to ready:")
		fmt.Printf("  Refused by the system:     %d connection attempts turned away before the server saw them, and made again\n", s.Redials)
		fmt.Printf("  Messages posted meanwhile: %d (%d refused or failed)\n", s.Posted, s.Refused)
		s.Post.report("  Post (request to answer):")
		if r.part {
			fmt.Printf("  Deliveries:                %d\n", s.Received)
		} else {
			fmt.Printf("  Deliveries:                %d of %d expected\n", s.Received, expected(s.PostsByConv, r.ConnectedByConv))
		}
		s.Delivery.report("  Delivery (post to arrival):")
		fmt.Printf("  Events out of order:       %d missing, %d twice (over the whole run)\n", r.Gaps, r.Repeats)
		fmt.Printf("  Connections lost:          %d during the storm (not counting the drop itself)\n", s.Lost)
		if s.Overran > 0 {
			fmt.Printf("  NOTE: %d generator(s) were not ready again inside -storm-window and went on after the others had finished: the storm's deliveries are undercounted\n", s.Overran)
		}
	}
}

// stormHow says when the members reconnect.
func stormHow(spread time.Duration) string {
	if spread <= 0 {
		return "at once"
	}
	return fmt.Sprintf("as clients do, each after a random 0.5 to %g seconds", (time.Second + spread).Seconds())
}

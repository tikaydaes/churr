// Command loadtest drives a running Churr server as its members would and
// measures how it holds up: how long posting takes, how long each message
// takes to reach every connected member, what happens when everyone
// reconnects at once. It is for sizing: 500 members on a small machine with
// SQLite, up to tens of thousands on PostgreSQL with several machines
// generating the load.
//
// In one go, on the server's own machine:
//
//	churr serve -dev -data ./lt-data &
//	  (open the setup link once, so the server has an owner and #general;
//	   or, before starting it: loadtest -bootstrap -data ./lt-data)
//	loadtest -data ./lt-data -url http://localhost:8472 -members 500 -pid <server pid>
//
// In steps, for a run from other machines:
//
//	loadtest -prepare -data DIR [-db URL] -members 50000 -channels 400 -per-member 6 -members-file members.json
//	  on the database's machine or beside it: makes the members and the
//	  channels, and writes down each member's token and channels
//	loadtest -members-file members.json -url https://chat.example -shard 1/3 -start-at 2026-01-02T15:04:00Z -online 0.3 -rate 50 -duration 5m -out g1.json
//	  on each generator, with its own -shard and the same everything else:
//	  connects its slice of the online members, posts its share, measures
//	loadtest -merge g1.json g2.json g3.json -out run.json
//	  anywhere: the report of the whole run
//
// tools/loadtest/sample.sh, run on the server meanwhile, records what the
// server and its database used.
//
// With -storm a run then drops every connection at once and reconnects them
// all, each with a `resume` from where it was, while the posting goes on
// (storm.go): at the same instant, or with -storm-spread 20s as clients
// spread themselves.
//
// It measures one set of machines at one moment. The numbers say nothing
// about any other, and nothing here is a benchmark of anything but the run
// it printed.
//
// Members are made behind the running server's back, through the store
// layer but straight in its database, because making fifty thousand
// passkeys is not what is being measured. On SQLite that works while the
// server runs; nobody is told about the new accounts, which a load test
// does not need.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/tikaydaes/churr/internal/store/postgres"
	_ "github.com/tikaydaes/churr/internal/store/sqlite"
)

func main() {
	var (
		data      = flag.String("data", "./data", "the server's data directory (to make members; not needed with -members-file)")
		db        = flag.String("db", "", "the server's database: empty for SQLite in the data directory, or a postgres:// address (to make members; not needed with -members-file)")
		base      = flag.String("url", "http://localhost:8472", "where the server answers")
		members   = flag.Int("members", 500, "how many members to make")
		channels  = flag.Int("channels", 20, "how many channels to make besides #general; hundreds for a large server")
		perMember = flag.Int("per-member", 3, "how many of those channels each member is in")
		online    = flag.Float64("online", 1, "the share of members who are online, from 0 to 1: only they connect and post. The rest exist and are in their channels, as people who are away are")
		rate      = flag.Float64("rate", 5, "messages posted a second, over all members and all generators")
		duration  = flag.Duration("duration", time.Minute, "how long to post for")
		share     = flag.Float64("general-share", 0.1, "the share of messages posted in #general, which every member is in")
		pid       = flag.Int("pid", 0, "the server's process id, when it runs on this machine, to report its memory; 0 reports none")
		prefix    = flag.String("prefix", "", "what the usernames and channel names made start with; empty picks one for this run")
		spread    = flag.Duration("storm-spread", 0, "with -storm: reconnect as clients do instead of all at once, each after a random time between 0.5s and 1s more than this (a server tells its clients 20s at 5,000 connections)")
		storm     = flag.Bool("storm", false, "after the posting, with posting going on at the same rate: drop every connection at once, reconnect them all at the same moment with `resume` from where each was, and report how that went")
		window    = flag.Duration("storm-window", -1, "with -storm: how long after the drop posting goes on, at least; every generator must be ready again inside it. Default: 60s with several generators; with one, until two seconds after everyone is ready again")

		doPrepare   = flag.Bool("prepare", false, "make the members and channels in the server's database, write -members-file, and exit. Run it on the database's machine or beside it; for SQLite, on the server's machine")
		membersPath = flag.String("members-file", "", "with -prepare: where to write the members. Otherwise: run as the members in this file, with no access to the database")
		shard       = flag.String("shard", "1/1", "i/n: be the i-th of n generators. Each connects its own n-th of the online members and posts an n-th of -rate")
		startAt     = flag.String("start-at", "", "when posting starts, such as 2026-01-02T15:04:05Z: the same on every generator, and late enough for all of them to have connected first. Empty: as soon as everyone is connected")
		out         = flag.String("out", "", "write what was measured to this file as JSON, for -merge")
		doMerge     = flag.Bool("merge", false, "read the results files named after the flags, print the report of the whole run, and with -out write the merged result")
	)
	compress := flag.Bool("compress", true, "ask the server to compress what it sends over the live connections (permessage-deflate), as apps and browsers do; false reads it plain, to compare")
	wire := flag.String("wire", "json", "how the generator speaks to the server: json, or protobuf as an app does (the protobuf subprotocol on the live connections, posts in protobuf, and always compressed, whatever -compress says)")
	idle := flag.Bool("idle", false, "members never say they are active, so the server takes each to have gone away five minutes after it connected and tells everyone; without it they say so every 50 seconds, as the client of someone at their keyboard does")
	bootstrap := flag.Bool("bootstrap", false, "with the server stopped: give an empty data directory an owner nobody can sign in as and a #general, in place of first-run setup, and exit")
	ownerToken := flag.String("owner-token", "", "make a personal token for the server's owner and write it to this `file`, for a machine with no browser to sign in from (a test of links between servers needs the owner's API); with -data and -db, the server running or stopped, and exit")
	into := flag.String("into", "", "with -prepare: put every member in this existing `conversation` (a channel shared with other servers, say) and make it the one the generators post in, in place of #general")
	cross := flag.String("cross", "", "for a channel shared between servers, each with its own generator and members file: connect every online member of the file (-shard i/n then only numbers the generator and gives it an n-th of -rate), and count the file's conversation under this `name`, the same on every generator, so that -merge adds the servers up")
	viewing := flag.Float64("viewing", 1, "the share of online members who have #general open, from 0 to 1, for a #general the server delivers lazily (above its large channel limit, or set so): they are sent its messages in full, the others are told that it changed and nothing more. 1, the default, is every member connecting as clients did before lazy delivery, which never say what they have open and are sent everything")
	call := flag.Bool("call", false, "in place of posting: the first members of the file's fullest channel hold one call there for -call-for, and the report says what each of them received. Needs -members-file and -url")
	callAudio := flag.Int("call-audio", 25, "with -call: how many people are in the call, each with a microphone")
	callVideo := flag.Int("call-video", 8, "with -call: how many of them also have a camera on, in the three sizes of docs/CALLS.md section 7")
	callFor := flag.Duration("call-for", time.Minute, "with -call: how long the call is measured for, once everyone is in and their pictures have settled")
	callTalk := flag.Float64("call-talk", 0.25, "with -call: the share of the time each person talks, in spurts of a few seconds, sending nothing in between; 1 is everyone talking all the time")
	flag.Parse()
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
	switch {
	case *bootstrap:
		if err := makeOwner(*data, *db); err != nil {
			fail(err)
		}
		fmt.Println("The data directory has an owner and #general. Start the server on it, then run the load test.")
		return
	case *ownerToken != "":
		if err := makeOwnerToken(*data, *db, *ownerToken); err != nil {
			fail(err)
		}
		fmt.Printf("Wrote %s. It holds a token that can do whatever the owner can: keep it to yourself.\n", *ownerToken)
		return
	case *doMerge:
		if err := mergeFiles(flag.Args(), *out); err != nil {
			fail(err)
		}
		return
	}
	if flag.NArg() > 0 {
		fail(fmt.Errorf("%q is not a flag", flag.Arg(0)))
	}
	if *prefix == "" {
		*prefix = fmt.Sprintf("lt%d", time.Now().Unix()%100000)
	}
	if *members < 1 || *channels < 0 || *perMember < 0 {
		fail(errors.New("-members must be at least 1, and -channels and -per-member not negative"))
	}

	ctx := context.Background()
	var file *membersFile
	if *doPrepare || *membersPath == "" {
		fmt.Printf("Making %d members and %d channels in the server's database...\n", *members, *channels)
		started := time.Now()
		var err error
		file, err = prepare(ctx, *data, *db, *prefix, *members, *channels, *perMember, *into, progressLine(*members))
		if err != nil {
			fail(err)
		}
		fmt.Printf("  done in %s\n", time.Since(started).Round(time.Millisecond))
	}
	if *doPrepare {
		if *membersPath == "" {
			fail(errors.New("-prepare needs -members-file, the file to write"))
		}
		if err := file.write(*membersPath); err != nil {
			fail(err)
		}
		fmt.Printf("Wrote %s. It holds every member's token: copy it to the generators and nowhere else.\n", *membersPath)
		return
	}
	if file == nil {
		var err error
		if file, err = readMembersFile(*membersPath); err != nil {
			fail(err)
		}
	}

	if *call {
		if *wire != "json" {
			fail(errors.New("-call speaks JSON only; leave -wire out"))
		}
		if err := runCall(callConfig{base: strings.TrimSuffix(*base, "/"), people: *callAudio, cameras: *callVideo, steady: *callFor, talk: *callTalk, compress: *compress}, file); err != nil {
			fail(err)
		}
		return
	}

	if *wire != "json" && *wire != "protobuf" {
		fail(fmt.Errorf("-wire is json or protobuf, not %q", *wire))
	}
	if *wire == "protobuf" {
		*compress = true // protobuf is never sent uncompressed
	}
	cfg := config{proto: *wire == "protobuf", base: strings.TrimSuffix(*base, "/"), rate: *rate, duration: *duration, generalShare: *share, pid: *pid,
		storm: *storm, stormSpread: *spread, stormWindow: *window, out: *out, compress: *compress, idle: *idle,
		viewing: *viewing, cross: *cross}
	var err error
	if cfg.shard, cfg.of, err = parseShard(*shard); err != nil {
		fail(err)
	}
	if *startAt != "" {
		if cfg.startAt, err = time.Parse(time.RFC3339, *startAt); err != nil {
			fail(fmt.Errorf("-start-at takes a time such as 2026-01-02T15:04:05Z or 2026-01-02T15:04:05+10:00: %w", err))
		}
	} else if cfg.of > 1 {
		fail(errors.New("-shard needs -start-at, the same on every generator, so that they post and stop together"))
	}
	if *viewing < 0 || *viewing > 1 {
		fail(errors.New("-viewing is from 0 to 1"))
	}
	if *online <= 0 || *online > 1 || *rate <= 0 || *duration <= 0 || *share < 0 || *share > 1 {
		fail(errors.New("-online is above 0 and at most 1, -general-share from 0 to 1, and -rate and -duration above 0"))
	}
	if cfg.stormWindow < 0 {
		cfg.stormWindow = 0
		if cfg.of > 1 {
			cfg.stormWindow = time.Minute
		}
	}
	sh := shape{Prefix: file.Prefix, Members: len(file.Members), Online: onlineCount(len(file.Members), *online), Channels: len(file.Channels),
		PerMember: file.PerMember, GeneralShare: *share, Rate: *rate, Duration: *duration, Storm: *storm}
	if *cross != "" {
		// Each server's members have a prefix of their own; the run is
		// known by the channel's name.
		sh.Prefix = *cross
	}
	if *storm {
		sh.StormSpread, sh.StormWindow = cfg.stormSpread, cfg.stormWindow
	}
	res, err := runGenerator(cfg, file, sh)
	if err != nil {
		fail(err)
	}
	if *out != "" {
		if err := res.write(*out); err != nil {
			fail(err)
		}
	}
	merged, notes, err := merge([]*result{res})
	if err != nil {
		fail(err)
	}
	if cfg.of > 1 {
		// One generator's file is a part of a run, and reads like one.
		merged.part = true
		notes = []string{fmt.Sprintf("this is generator %d of %d alone. It counts what it posted and what its own connections received; `loadtest -merge` with every generator's -out file gives the run", cfg.shard, cfg.of)}
	}
	report(merged, notes)
	if *out != "" {
		fmt.Printf("\nWrote %s.\n", *out)
	}
}

// progressLine returns what prepare calls as members are made: it says how
// far along a long one is, every ten thousand members.
func progressLine(total int) func(int) {
	if total < 20000 {
		return nil
	}
	return func(done int) {
		if done%10000 < prepareBatch && done >= 10000 {
			fmt.Printf("  %d of %d\n", done/10000*10000, total)
		}
	}
}

// mergeFiles is -merge.
func mergeFiles(paths []string, out string) error {
	if len(paths) == 0 {
		return errors.New("-merge takes the generators' results files, after the flags: loadtest -merge -out run.json g1.json g2.json g3.json")
	}
	var parts []*result
	for i := 0; i < len(paths); i++ {
		p := paths[i]
		// -out is taken after the files as well as before them, since the
		// flag package stops reading flags at the first file.
		if name := strings.TrimLeft(p, "-"); name != p && (name == "out" || strings.HasPrefix(name, "out=")) {
			v, given := strings.CutPrefix(name, "out=")
			if !given && i+1 < len(paths) {
				i++
				v = paths[i]
			}
			if v == "" {
				return errors.New("-out needs a file name")
			}
			out = v
			continue
		}
		if strings.HasPrefix(p, "-") {
			return fmt.Errorf("%s comes after the results files; flags other than -out go before them", p)
		}
		r, err := readResult(p)
		if err != nil {
			return err
		}
		parts = append(parts, r)
	}
	merged, notes, err := merge(parts)
	if err != nil {
		return err
	}
	report(merged, notes)
	if out != "" {
		if err := merged.write(out); err != nil {
			return err
		}
		fmt.Printf("\nWrote %s.\n", out)
	}
	return nil
}

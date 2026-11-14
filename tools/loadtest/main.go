// Command loadtest drives the targets of BRIEF.md against a running Churr
// server: 500 members on a small machine with SQLite, 5,000 on a mid-sized
// one with PostgreSQL. It makes the members directly in the server's
// database, connects one live connection for each, posts messages at a set
// rate for a set time, and reports how long posting took, how long each
// message took to reach every connected member, and how much memory the
// server used.
//
//	churr serve -dev -data ./lt-data &
//	  (open the setup link once, so the server has an owner and #general)
//	go run ./tools/loadtest -data ./lt-data -url http://localhost:8472 -members 500 -pid <server pid>
//
// It measures one machine at one moment. The numbers say nothing about any
// other machine, and nothing here is a benchmark of anything but the run it
// printed.
//
// Members are made behind the running server's back, straight in its
// database, because making five thousand passkeys is not what is being
// measured. On SQLite that works while the server runs; nobody is told about
// the new accounts, which a load test does not need.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tikaydaes/churr/internal/live"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
	_ "github.com/tikaydaes/churr/internal/store/postgres"
	_ "github.com/tikaydaes/churr/internal/store/sqlite"
)

type member struct {
	id    model.AccountID
	token string
	convs []model.ConversationID
	ws    *wsConn
}

func main() {
	var (
		data     = flag.String("data", "./data", "the server's data directory")
		db       = flag.String("db", "", "the server's database: empty for SQLite in the data directory, or a postgres:// address")
		base     = flag.String("url", "http://localhost:8472", "where the server answers")
		members  = flag.Int("members", 500, "how many members to make and connect")
		channels = flag.Int("channels", 20, "how many channels besides #general; each member is in three of them")
		rate     = flag.Float64("rate", 5, "messages posted a second, over all members")
		duration = flag.Duration("duration", time.Minute, "how long to post for")
		share    = flag.Float64("general-share", 0.1, "the share of messages posted in #general, which every member is in")
		pid      = flag.Int("pid", 0, "the server's process id, to report its memory; 0 reports none")
		prefix   = flag.String("prefix", "", "what the usernames made start with; empty picks one for this run")
	)
	bootstrap := flag.Bool("bootstrap", false, "with the server stopped: give an empty data directory an owner nobody can sign in as and a #general, in place of first-run setup, and exit")
	flag.Parse()
	if *bootstrap {
		if err := makeOwner(*data, *db); err != nil {
			fmt.Fprintln(os.Stderr, "loadtest:", err)
			os.Exit(1)
		}
		fmt.Println("The data directory has an owner and #general. Start the server on it, then run the load test.")
		return
	}
	if *prefix == "" {
		*prefix = fmt.Sprintf("lt%d", time.Now().Unix()%100000)
	}
	if err := run(*data, *db, strings.TrimSuffix(*base, "/"), *prefix, *members, *channels, *rate, *duration, *share, *pid); err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
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

func mib(b int64) string { return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20)) }

// makeOwner does to an empty database what first-run setup does, without the
// passkey: an owner account, and #general with @everyone in it. It is for a
// machine with no browser to open the setup link in. Nobody can sign in as
// that owner, so a directory made this way is good for a load test and
// nothing else.
func makeOwner(data, db string) error {
	ctx := context.Background()
	if err := os.MkdirAll(data, 0o700); err != nil {
		return err
	}
	st, err := store.Open(ctx, data, db)
	if err != nil {
		return err
	}
	defer st.Close()
	if _, found, err := st.Accounts().Owner(ctx); err != nil {
		return err
	} else if found {
		return errors.New("this server already has an owner")
	}
	if err := st.Groups().EnsureBuiltin(ctx); err != nil {
		return err
	}
	now := model.Now()
	return st.Tx(ctx, func(tx store.Tx) error {
		owner := model.Account{Username: "loadtest-owner", DisplayName: "Load test owner", Role: model.RoleOwner, CreatedAt: now}
		if _, err := tx.Accounts().Create(ctx, &owner); err != nil {
			return err
		}
		general := model.Conversation{Kind: model.KindChannel, Name: "general", CreatedByID: owner.ID, CreatedAt: now}
		if err := tx.Conversations().Create(ctx, &general); err != nil {
			return err
		}
		if _, err := tx.Members().AddDirect(ctx, general.ID, owner.ID, model.LevelManager, now); err != nil {
			return err
		}
		everyone, err := tx.Groups().Builtin(ctx, model.GroupEveryone)
		if err != nil {
			return err
		}
		if _, _, err := tx.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: general.ID, GroupID: everyone.ID, Level: model.LevelMember, AddedByID: owner.ID, AddedAt: now}); err != nil {
			return err
		}
		if err := tx.Settings().Set(ctx, store.SettingServerName, "Load test", ""); err != nil {
			return err
		}
		return tx.Settings().Set(ctx, store.SettingSetupDoneAt, int64(now), "")
	})
}

// prepare makes the members, their tokens and the channels, in the server's
// database.
func prepare(ctx context.Context, data, db, prefix string, n, channels int) ([]*member, model.ConversationID, error) {
	st, err := store.Open(ctx, data, db)
	if err != nil {
		return nil, "", fmt.Errorf("opening the server's database: %w", err)
	}
	defer st.Close()
	owner, found, err := st.Accounts().Owner(ctx)
	if err != nil {
		return nil, "", err
	}
	if !found {
		return nil, "", errors.New("the server has no owner yet: open its setup link first")
	}
	general, err := st.Conversations().ByName(ctx, "general")
	if err != nil {
		return nil, "", fmt.Errorf("the server has no #general: %w", err)
	}
	now := model.Now()
	var convs []model.ConversationID
	for i := 0; i < channels; i++ {
		c := model.Conversation{Kind: model.KindChannel, Name: fmt.Sprintf("%s-room-%02d", prefix, i), CreatedByID: owner.ID, CreatedAt: now}
		err := st.Tx(ctx, func(tx store.Tx) error {
			if err := tx.Conversations().Create(ctx, &c); err != nil {
				return err
			}
			_, err := tx.Members().AddDirect(ctx, c.ID, owner.ID, model.LevelManager, now)
			return err
		})
		if err != nil {
			return nil, "", err
		}
		convs = append(convs, c.ID)
	}
	out := make([]*member, 0, n)
	for i := 0; i < n; i++ {
		acc := model.Account{Username: fmt.Sprintf("%s-%05d", prefix, i), DisplayName: fmt.Sprintf("Load %d", i), Role: model.RoleMember, CreatedAt: now}
		token, hash := secret.NewToken(secret.PrefixPersonal)
		m := &member{token: token, convs: []model.ConversationID{general.ID}}
		err := st.Tx(ctx, func(tx store.Tx) error {
			// A claimed member is in @everyone, and so in #general.
			if _, err := tx.Accounts().Create(ctx, &acc); err != nil {
				return err
			}
			ses := model.Session{AccountID: acc.ID, Kind: model.SessionPersonal, Label: "load test", CreatedAt: now}
			if err := tx.Sessions().Create(ctx, &ses, hash, nil); err != nil {
				return err
			}
			for k := 0; k < 3 && len(convs) > 0; k++ {
				c := convs[(i+k*7)%len(convs)]
				if _, err := tx.Members().AddDirect(ctx, c, acc.ID, model.LevelMember, now); err != nil {
					if errors.Is(err, model.ErrAlreadyExists) {
						continue
					}
					return err
				}
				m.convs = append(m.convs, c)
			}
			return nil
		})
		if err != nil {
			return nil, "", fmt.Errorf("making member %d: %w", i, err)
		}
		m.id = acc.ID
		out = append(out, m)
	}
	return out, general.ID, nil
}

// connect opens a member's live connection and waits for `ready`.
func connect(base string, m *member) error {
	ws, err := dialWS("ws"+strings.TrimPrefix(base, "http")+"/api/v1/events", m.token, live.Subprotocol, 20*time.Second)
	if err != nil {
		return err
	}
	if _, err := ws.read(); err != nil { // hello
		ws.close()
		return err
	}
	if err := ws.writeText([]byte(`{"type":"resume","positions":{}}`)); err != nil {
		ws.close()
		return err
	}
	for {
		frame, err := ws.read()
		if err != nil {
			ws.close()
			return err
		}
		if bytes.Contains(frame, []byte(`"type":"ready"`)) {
			break
		}
	}
	m.ws = ws
	return nil
}

// marker starts the text of every message the test posts, followed by the
// moment it was sent in nanoseconds.
const marker = "loadtest "

// sentAt reads the moment a message.created frame's message was posted, or
// false for any other frame.
func sentAt(frame []byte) (int64, bool) {
	if !bytes.Contains(frame, []byte(`"type":"message.created"`)) {
		return 0, false
	}
	i := bytes.Index(frame, []byte(`"text":"`+marker))
	if i < 0 {
		return 0, false
	}
	rest := frame[i+len(`"text":"`+marker):]
	end := bytes.IndexAny(rest, ` "`)
	if end < 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(string(rest[:end]), 10, 64)
	return n, err == nil
}

type samples struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *samples) add(d time.Duration) {
	s.mu.Lock()
	s.d = append(s.d, d)
	s.mu.Unlock()
}

func (s *samples) report(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.d) == 0 {
		fmt.Printf("%-28s no samples\n", name)
		return
	}
	sort.Slice(s.d, func(i, j int) bool { return s.d[i] < s.d[j] })
	at := func(p float64) time.Duration { return s.d[min(len(s.d)-1, int(p*float64(len(s.d))))] }
	r := func(d time.Duration) string { return d.Round(100 * time.Microsecond).String() }
	fmt.Printf("%-28s n=%d  p50=%s  p95=%s  p99=%s  max=%s\n", name, len(s.d), r(at(0.50)), r(at(0.95)), r(at(0.99)), r(s.d[len(s.d)-1]))
}

func run(data, db, base, prefix string, n, channels int, rate float64, duration time.Duration, share float64, pid int) error {
	ctx := context.Background()
	if resp, err := http.Get(base + "/healthz"); err != nil {
		return fmt.Errorf("the server does not answer at %s: %w", base, err)
	} else {
		resp.Body.Close()
	}
	idle := rss(pid)
	fmt.Printf("Making %d members and %d channels in the server's database...\n", n, channels)
	started := time.Now()
	members, general, err := prepare(ctx, data, db, prefix, n, channels)
	if err != nil {
		return err
	}
	fmt.Printf("  done in %s\n", time.Since(started).Round(time.Millisecond))

	fmt.Printf("Connecting %d live connections...\n", n)
	started = time.Now()
	var connectTimes samples
	var failed atomic.Int64
	var wg sync.WaitGroup
	gate := make(chan struct{}, 50) // fifty handshakes at a time
	for _, m := range members {
		wg.Add(1)
		gate <- struct{}{}
		go func(m *member) {
			defer wg.Done()
			defer func() { <-gate }()
			t := time.Now()
			if err := connect(base, m); err != nil {
				if failed.Add(1) <= 3 {
					fmt.Fprintln(os.Stderr, "  a connection failed:", err)
				}
				return
			}
			connectTimes.add(time.Since(t))
		}(m)
	}
	wg.Wait()
	connected := 0
	inConv := map[model.ConversationID]int{}
	for _, m := range members {
		if m.ws != nil {
			connected++
			for _, c := range m.convs {
				inConv[c]++
			}
		}
	}
	fmt.Printf("  %d connected, %d failed, in %s\n", connected, failed.Load(), time.Since(started).Round(time.Millisecond))
	if connected == 0 {
		return errors.New("nothing connected")
	}
	time.Sleep(2 * time.Second)
	afterConnect := rss(pid)

	// Readers: every connection counts the test's messages as they arrive.
	var delivery samples
	var received, dropped atomic.Int64
	stop := make(chan struct{})
	for _, m := range members {
		if m.ws == nil {
			continue
		}
		go func(m *member) {
			for {
				frame, err := m.ws.read()
				if err != nil {
					select {
					case <-stop:
					default:
						dropped.Add(1)
					}
					return
				}
				if sent, ok := sentAt(frame); ok {
					received.Add(1)
					delivery.add(time.Duration(time.Now().UnixNano() - sent))
				}
			}
		}(m)
		// The heartbeat a client owes the server.
		go func(m *member) {
			t := time.NewTicker(25 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					m.ws.writeText([]byte(`{"type":"ping"}`))
				}
			}
		}(m)
	}

	// Memory is sampled once a second while messages flow.
	var peak atomic.Int64
	peak.Store(afterConnect)
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if v := rss(pid); v > peak.Load() {
					peak.Store(v)
				}
			}
		}
	}()

	fmt.Printf("Posting %.1f messages a second for %s...\n", rate, duration)
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 64, MaxConnsPerHost: 64}}
	var post samples
	var posted, refused, expected atomic.Int64
	rng := rand.New(rand.NewSource(1))
	interval := time.Duration(float64(time.Second) / rate)
	tick := time.NewTicker(interval)
	end := time.After(duration)
	var posts sync.WaitGroup
loop:
	for {
		select {
		case <-end:
			break loop
		case <-tick.C:
		}
		m := members[rng.Intn(len(members))]
		conv := general
		if rng.Float64() >= share && len(m.convs) > 1 {
			conv = m.convs[1+rng.Intn(len(m.convs)-1)]
		}
		posts.Add(1)
		go func(m *member, conv model.ConversationID) {
			defer posts.Done()
			body, _ := json.Marshal(map[string]any{"text": fmt.Sprintf("%s%d from %s", marker, time.Now().UnixNano(), m.id)})
			req, _ := http.NewRequest("POST", base+"/api/v1/conversations/"+string(conv)+"/messages", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+m.token)
			req.Header.Set("Content-Type", "application/json")
			t := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				refused.Add(1)
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
			post.add(time.Since(t))
			posted.Add(1)
			expected.Add(int64(inConv[conv]))
		}(m, conv)
	}
	tick.Stop()
	posts.Wait()
	// Let the last messages arrive.
	for i := 0; i < 50 && received.Load() < expected.Load(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	atEnd := rss(pid)
	close(stop)
	for _, m := range members {
		if m.ws != nil {
			m.ws.close()
		}
	}

	fmt.Println()
	fmt.Printf("Members connected:           %d of %d\n", connected, n)
	fmt.Printf("Messages posted:             %d (%d refused or failed), %.1f a second\n", posted.Load(), refused.Load(), float64(posted.Load())/duration.Seconds())
	fmt.Printf("Deliveries to connections:   %d of %d expected\n", received.Load(), expected.Load())
	fmt.Printf("Connections lost during run: %d\n", dropped.Load())
	connectTimes.report("Connect (to ready):")
	post.report("Post (request to answer):")
	delivery.report("Delivery (post to arrival):")
	if pid != 0 {
		fmt.Printf("Server memory (resident):    %s before, %s with everyone connected, %s at its highest, %s at the end\n",
			mib(idle), mib(afterConnect), mib(peak.Load()), mib(atEnd))
		if connected > 0 && afterConnect > idle {
			fmt.Printf("                             about %.0f KiB for each connected member\n", float64(afterConnect-idle)/1024/float64(connected))
		}
	}
	return nil
}

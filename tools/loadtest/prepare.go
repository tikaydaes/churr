package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// The prepare step: the only part of a load test that touches the server's
// database. It makes the members, their tokens and the channels through the
// store layer, as the server itself would, and writes down what a generator
// needs to act as those members. A generator given that file needs nothing
// but the server's address.

// membersFile is what -prepare writes and a generator reads. It holds every
// member's token, so it is written for its owner's eyes only.
type membersFile struct {
	Version int `json:"version"`
	// Prefix starts the usernames and channel names of this set.
	Prefix    string `json:"prefix"`
	PerMember int    `json:"per_member"`
	// General is #general, which every member is in.
	General string `json:"general"`
	// Channels are the channels made for the test, besides #general.
	Channels []string `json:"channels"`
	// Members are in a fixed order. A run with -online takes the first
	// share of them; a shard takes every n-th of those.
	Members []fileMember `json:"members"`
}

type fileMember struct {
	ID    string `json:"id"`
	Token string `json:"token"`
	// In are the member's channels besides #general, as places in Channels.
	In []int `json:"in"`
}

const membersFileVersion = 1

func (f *membersFile) write(path string) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func readMembersFile(path string) (*membersFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f membersFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s is not a members file: %w", path, err)
	}
	if f.Version != membersFileVersion {
		return nil, fmt.Errorf("%s is a members file of version %d; this program reads version %d", path, f.Version, membersFileVersion)
	}
	if f.General == "" || len(f.Members) == 0 {
		return nil, fmt.Errorf("%s names no #general or no members", path)
	}
	for i, m := range f.Members {
		if m.ID == "" || m.Token == "" {
			return nil, fmt.Errorf("%s: member %d has no id or no token", path, i)
		}
		for _, c := range m.In {
			if c < 0 || c >= len(f.Channels) {
				return nil, fmt.Errorf("%s: member %d is in channel %d of %d", path, i, c, len(f.Channels))
			}
		}
	}
	return &f, nil
}

// onlineCount is how many of n members connect when the share given is
// online: at least one, at most all.
func onlineCount(n int, share float64) int {
	k := int(float64(n)*share + 0.5)
	return max(1, min(n, k))
}

// shardOf picks the members of one generator: of the first `online` members,
// every n-th, starting at the i-th (i counts from 1). The slices of the n
// generators are the same size to within one, share no member, and together
// are all the online members.
func shardOf(members []fileMember, online, i, n int) []fileMember {
	online = min(online, len(members))
	out := make([]fileMember, 0, online/n+1)
	for j := i - 1; j < online; j += n {
		out = append(out, members[j])
	}
	return out
}

// parseShard reads "i/n".
func parseShard(s string) (i, n int, err error) {
	if _, err := fmt.Sscanf(s, "%d/%d", &i, &n); err != nil || fmt.Sprintf("%d/%d", i, n) != s {
		return 0, 0, fmt.Errorf("-shard takes i/n, such as 2/3, not %q", s)
	}
	if n < 1 || i < 1 || i > n {
		return 0, 0, fmt.Errorf("-shard %s: the first number goes from 1 to the second", s)
	}
	return i, n, nil
}

// channelsFor picks which channels each of n members is in: perMember
// different ones each, chosen at random with a fixed seed, so every channel
// ends up with about n*perMember/channels members and who is in which does
// not depend on a member's place in the list.
func channelsFor(n, channels, perMember int) [][]int {
	perMember = min(perMember, channels)
	rng := rand.New(rand.NewSource(1))
	out := make([][]int, n)
	for i := range out {
		in := make([]int, 0, perMember)
	pick:
		for len(in) < perMember {
			c := rng.Intn(channels)
			for _, have := range in {
				if have == c {
					continue pick
				}
			}
			in = append(in, c)
		}
		out[i] = in
	}
	return out
}

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

// How members are made quickly without leaving the store layer, which has no
// bulk insert and is not this program's to add one to:
//
//   - many rows to a transaction, so the database commits (and on a real
//     disk, syncs) once per batch and not once per member. On SQLite, where
//     one writer works at a time, this is the whole gain, and a batch is
//     small enough that a running server waits only a moment for its turn;
//   - on PostgreSQL, several transactions at once on separate connections.
//     A member costs some thirty-five short queries, and the time goes on
//     the round trips, so eight at once is close to eight times as fast.
//
// Adding a member to a channel locks the channel's row until the
// transaction ends. Transactions that each added members to several
// channels would wait for one another, and deadlock. So the work is done in
// two passes: first the accounts and their tokens, in batches side by side;
// then the channels, each channel's members in transactions of its own, so
// that no transaction ever holds two channels and no two are in the same
// one.
//
// It should run on the database's machine or one beside it: every
// millisecond between here and the database is 35 ms a member.
const (
	prepareBatch   = 100 // accounts to a transaction
	joinBatch      = 500 // members added to a channel in one transaction
	prepareWorkers = 8
)

// prepare makes n members, their tokens and the channels in the server's
// database, and returns what a generator needs. progress, if not nil, is
// told how many members exist so far.
func prepare(ctx context.Context, data, db, prefix string, n, channels, perMember int, into string, progress func(done int)) (*membersFile, error) {
	st, err := store.Open(ctx, data, db)
	if err != nil {
		return nil, fmt.Errorf("opening the server's database: %w", err)
	}
	defer st.Close()
	owner, found, err := st.Accounts().Owner(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("the server has no owner yet: open its setup link first, or use -bootstrap")
	}
	general, err := st.Conversations().ByName(ctx, "general")
	if err != nil {
		return nil, fmt.Errorf("the server has no #general: %w", err)
	}
	if into != "" {
		// The members' one conversation is a channel that exists
		// already: one shared with other servers, which only the
		// server itself can make.
		if general, err = st.Conversations().Get(ctx, model.ConversationID(into)); err != nil {
			return nil, fmt.Errorf("-into %s: %w", into, err)
		}
	}
	now := model.Now()
	out := &membersFile{Version: membersFileVersion, Prefix: prefix, PerMember: min(perMember, channels), General: string(general.ID),
		Channels: make([]string, channels), Members: make([]fileMember, n)}

	for from := 0; from < channels; from += prepareBatch {
		to := min(channels, from+prepareBatch)
		err := st.Tx(ctx, func(tx store.Tx) error {
			for i := from; i < to; i++ {
				c := model.Conversation{Kind: model.KindChannel, Name: fmt.Sprintf("%s-room-%03d", prefix, i), CreatedByID: owner.ID, CreatedAt: now}
				if err := tx.Conversations().Create(ctx, &c); err != nil {
					return err
				}
				if _, err := tx.Members().AddDirect(ctx, c.ID, owner.ID, model.LevelManager, now); err != nil {
					return err
				}
				out.Channels[i] = string(c.ID)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("making the channels: %w", err)
		}
	}

	in := channelsFor(n, channels, perMember)
	// batch makes accounts from..to, each with a token, in one transaction.
	// It fills its own part of out.Members only once the transaction is in,
	// so a batch that is tried again leaves nothing of its first attempt.
	batch := func(from, to int) error {
		made := make([]fileMember, 0, to-from)
		err := st.Tx(ctx, func(tx store.Tx) error {
			made = made[:0]
			for i := from; i < to; i++ {
				acc := model.Account{Username: fmt.Sprintf("%s-%05d", prefix, i), DisplayName: fmt.Sprintf("Load %d", i), Role: model.RoleMember, CreatedAt: now}
				// A claimed member is in @everyone, and so in #general.
				if _, err := tx.Accounts().Create(ctx, &acc); err != nil {
					return fmt.Errorf("member %d: %w", i, err)
				}
				token, hash := secret.NewToken(secret.PrefixPersonal)
				ses := model.Session{AccountID: acc.ID, Kind: model.SessionPersonal, Label: "load test", CreatedAt: now}
				if err := tx.Sessions().Create(ctx, &ses, hash, nil); err != nil {
					return fmt.Errorf("member %d: %w", i, err)
				}
				made = append(made, fileMember{ID: string(acc.ID), Token: token, In: in[i]})
			}
			return nil
		})
		if err != nil {
			return err
		}
		copy(out.Members[from:to], made)
		return nil
	}
	// join puts a channel's members in it, a transaction for every few
	// hundred.
	inChannel := make([][]int, channels)
	for i, cs := range in {
		for _, c := range cs {
			inChannel[c] = append(inChannel[c], i)
		}
	}
	join := func(c int) error {
		conv := model.ConversationID(out.Channels[c])
		for from := 0; from < len(inChannel[c]); from += joinBatch {
			part := inChannel[c][from:min(len(inChannel[c]), from+joinBatch)]
			err := st.Tx(ctx, func(tx store.Tx) error {
				for _, i := range part {
					if _, err := tx.Members().AddDirect(ctx, conv, model.AccountID(out.Members[i].ID), model.LevelMember, now); err != nil {
						return fmt.Errorf("member %d into channel %d: %w", i, c, err)
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	}

	workers := 1
	if st.Dialect() == "postgres" {
		workers = prepareWorkers
	}
	// each runs do(0), do(1), ... do(count-1) on the workers and returns
	// the first failure.
	each := func(count int, do func(k int) error) error {
		var (
			wg       sync.WaitGroup
			next     atomic.Int64
			failMu   sync.Mutex
			firstErr error
		)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					k := int(next.Add(1)) - 1
					failMu.Lock()
					stop := firstErr != nil
					failMu.Unlock()
					if k >= count || stop {
						return
					}
					if err := do(k); err != nil {
						failMu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						failMu.Unlock()
						return
					}
				}
			}()
		}
		wg.Wait()
		return firstErr
	}
	var done atomic.Int64
	err = each((n+prepareBatch-1)/prepareBatch, func(k int) error {
		from, to := k*prepareBatch, min(n, (k+1)*prepareBatch)
		if err := batch(from, to); err != nil {
			return err
		}
		if progress != nil {
			progress(int(done.Add(int64(to - from))))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("making the members: %w", err)
	}
	if err := each(channels, join); err != nil {
		return nil, fmt.Errorf("putting the members in their channels: %w", err)
	}
	if into != "" {
		for from := 0; from < n; from += joinBatch {
			to := min(n, from+joinBatch)
			err := st.Tx(ctx, func(tx store.Tx) error {
				for i := from; i < to; i++ {
					if _, err := tx.Members().AddDirect(ctx, general.ID, model.AccountID(out.Members[i].ID), model.LevelMember, now); err != nil {
						return fmt.Errorf("member %d into %s: %w", i, into, err)
					}
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// makeOwnerToken gives the server's owner a personal token and writes it to
// path, for its owner's eyes only. An owner made with -bootstrap has no
// passkey and can sign in nowhere; what only an owner can do through the
// API (linking to a hub, sharing a channel) is done with this.
func makeOwnerToken(data, db, path string) error {
	ctx := context.Background()
	st, err := store.Open(ctx, data, db)
	if err != nil {
		return fmt.Errorf("opening the server's database: %w", err)
	}
	defer st.Close()
	owner, found, err := st.Accounts().Owner(ctx)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("the server has no owner yet: open its setup link first, or use -bootstrap")
	}
	token, hash := secret.NewToken(secret.PrefixPersonal)
	ses := model.Session{AccountID: owner.ID, Kind: model.SessionPersonal, Label: "load test owner", CreatedAt: model.Now()}
	if err := st.Sessions().Create(ctx, &ses, hash, nil); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token+"\n"), 0o600)
}

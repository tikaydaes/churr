package storetest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("broad", testBroad) }

// words writes values with spaces between, to compare several at once.
func words(v ...any) string { return strings.TrimSpace(fmt.Sprintln(v...)) }

// testBroad covers the rows a broad mention writes (ruling R64): who gets
// one, who does not, and that the count asked beforehand is the same.
func testBroad(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	carol := newAccount(t, s, "carol", model.RoleMember)
	deaf := newAccount(t, s, "deaf", model.RoleMember)   // ignores broad mentions here
	cross := newAccount(t, s, "cross", model.RoleMember) // has blocked ann
	gone := newAccount(t, s, "gone", model.RoleMember)   // removed
	guest := newAccount(t, s, "guest", model.RoleGuest)
	outside := newAccount(t, s, "outside", model.RoleMember) // not in the channel
	pub := newChannel(t, s, "broad", false, ann.ID)
	priv := newChannel(t, s, "broad-private", true, ann.ID)
	for _, c := range []model.Conversation{pub, priv} {
		for _, a := range []model.Account{bob, carol, deaf, cross, gone, guest} {
			join(t, s, c.ID, a.ID, model.LevelMember)
		}
		_, err := s.Members().UpdateOwn(ctx, c.ID, deaf.ID, func(m *model.Member) error { m.IgnoreBroad = true; return nil })
		must(t, err)
	}
	must(t, s.Blocks().Add(ctx, cross.ID, ann.ID, model.Now()))
	_, err := s.Accounts().Remove(ctx, gone.ID, ann.ID, model.Now())
	must(t, err)

	m, found, err := s.Members().Get(ctx, pub.ID, deaf.ID)
	must(t, err)
	eq(t, "the choice is stored", found && m.IgnoreBroad, true)
	m, _, err = s.Members().Get(ctx, pub.ID, bob.ID)
	must(t, err)
	eq(t, "and starts off", m.IgnoreBroad, false)

	rows := func(msg model.Message) map[model.AccountID]string {
		t.Helper()
		list, err := s.Messages().Mentions(ctx, msg.ID)
		must(t, err)
		out := map[model.AccountID]string{}
		for _, r := range list {
			out[r.AccountID] = r.Kind
		}
		return out
	}
	// write stores a message and its broad mention, and holds that the
	// count asked beforehand is the number of rows written.
	write := func(conv model.Conversation, b store.BroadMention, first ...model.Mention) map[model.AccountID]string {
		t.Helper()
		reach, err := s.Messages().BroadReach(ctx, conv, ann.ID, b)
		must(t, err)
		msg := post(t, s, conv.ID, ann.ID, "", "broad", first...)
		n, err := s.Messages().MentionBroadly(ctx, msg, b)
		must(t, err)
		got := rows(msg)
		eq(t, b.Scope+": rows written", int(n), len(got)-len(first))
		if len(first) == 0 {
			eq(t, b.Scope+": the count asked beforehand", reach, n)
		}
		return got
	}

	// @channel: the members, less the author, the one who ignores, the
	// one who blocked and the one removed.
	got := write(pub, store.BroadMention{Scope: store.BroadChannel, Blocking: true})
	eq(t, "@channel", words(len(got), got[bob.ID], got[carol.ID], got[guest.ID]), "3 channel channel channel")
	// With blocking off for the server a block stops nothing.
	got = write(pub, store.BroadMention{Scope: store.BroadChannel})
	eq(t, "@channel with blocking off", words(len(got), got[cross.ID]), "4 channel")

	// @here: only those the caller says are online, and of them only
	// members. More than one statement's worth of names is taken.
	online := []model.AccountID{bob.ID, deaf.ID, outside.ID, ann.ID}
	for i := 0; i < 4500; i++ {
		online = append(online, model.NewID[model.AccountID]())
	}
	online = append(online, guest.ID)
	got = write(pub, store.BroadMention{Scope: store.BroadHere, Online: online, Blocking: true})
	eq(t, "@here", words(len(got), got[bob.ID], got[guest.ID]), "2 here here")
	got = write(pub, store.BroadMention{Scope: store.BroadHere, Blocking: true})
	eq(t, "@here with nobody online", len(got), 0)

	// @everyone: the built-in group, so no guest, and only the people in
	// the conversation: in a public channel too, those who have not joined
	// are not reached (review finding H2).
	got = write(pub, store.BroadMention{Scope: store.BroadEveryone, Blocking: true})
	eq(t, "@everyone in a public channel", words(len(got), got[bob.ID], got[carol.ID], got[outside.ID] == ""), "2 group group true")
	got = write(priv, store.BroadMention{Scope: store.BroadEveryone, Blocking: true})
	eq(t, "@everyone in a private channel", words(len(got), got[bob.ID], got[carol.ID]), "2 group group")

	// A row the message already has keeps its kind: someone named and
	// someone who ignores broad mentions but was named are both told by
	// name.
	got = write(pub, store.BroadMention{Scope: store.BroadChannel, Blocking: true},
		model.Mention{AccountID: bob.ID, Kind: model.MentionAccount}, model.Mention{AccountID: deaf.ID, Kind: model.MentionAccount})
	eq(t, "named beside @channel", words(len(got), got[bob.ID], got[deaf.ID], got[carol.ID]), "4 account account channel")

	// The channel's own answer is stored, and null reads as "server".
	c, err := s.Conversations().Get(ctx, pub.ID)
	must(t, err)
	eq(t, "a channel follows the server until it says otherwise", c.BroadMentions, model.BroadServer)
	for _, v := range []string{model.BroadManagers, model.BroadAnyone, model.BroadServer} {
		c, err = s.Conversations().Update(ctx, pub.ID, func(c *model.Conversation) error { c.BroadMentions = v; return nil })
		must(t, err)
		eq(t, "returned", c.BroadMentions, v)
		c, err = s.Conversations().Get(ctx, pub.ID)
		must(t, err)
		eq(t, "stored", c.BroadMentions, v)
	}
}

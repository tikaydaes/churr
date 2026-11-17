package storetest

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("first joins", testFirstJoins) }

// testFirstJoins is what a newcomer to a private channel may read (ruling
// R87): the first join kept for good, the member row's copy of it, history,
// threads, search, bookmarks and mentions filtered by it, and nothing of
// the kind in public, knock or converted channels.
func testFirstJoins(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleOwner)
	bob := newAccount(t, s, "bob", model.RoleMember)
	cat := newAccount(t, s, "cat", model.RoleMember)
	base := model.Now()
	at := func(n int) model.Time { return base + model.Time(n*1000) }
	postAt := func(conv model.ConversationID, parent model.MessageID, when model.Time, text string, mentions ...model.Mention) model.Message {
		t.Helper()
		m := model.Message{ID: model.NewID[model.MessageID](), ConversationID: conv, AuthorID: ann.ID, ParentID: parent,
			Text: text, SearchText: text, CreatedAt: when}
		must(t, s.Tx(ctx, func(tx store.Tx) error {
			seq, err := tx.Conversations().NextSeq(ctx, conv)
			if err != nil {
				return err
			}
			m.Seq = seq
			for i := range mentions {
				mentions[i].MessageID = m.ID
			}
			return tx.Messages().Insert(ctx, &m, mentions)
		}))
		return m
	}
	joinAt := func(conv model.ConversationID, account model.AccountID, when model.Time) model.Member {
		t.Helper()
		_, err := s.Members().AddDirect(ctx, conv, account, model.LevelMember, when)
		must(t, err)
		m, _, err := s.Members().Get(ctx, conv, account)
		must(t, err)
		return m
	}
	ids := func(msgs []model.Message) string {
		out := ""
		for _, m := range msgs {
			out += m.Text + " "
		}
		return out
	}

	priv := newChannel(t, s, "council", true, ann.ID)
	old := postAt(priv.ID, "", at(1), "oldsecret")
	oldReply := postAt(priv.ID, old.ID, at(2), "oldreply")

	t.Run("first join and the member row", func(t *testing.T) {
		m := joinAt(priv.ID, bob.ID, at(10))
		eq(t, "visible from the first join", m.VisibleFrom, at(10))
		st, err := s.Members().Standing(ctx, priv.ID, bob.ID, model.Now())
		must(t, err)
		eq(t, "standing carries it", st.Member.VisibleFrom, at(10))
		r, err := s.Members().Roster(ctx, priv.ID)
		must(t, err)
		rm, _ := r.Find(bob.ID)
		eq(t, "the roster carries it", rm.VisibleFrom, at(10))
		eq(t, "and its latest floor", r.LatestFloor, at(10))
	})

	fresh := postAt(priv.ID, "", at(20), "freshnews", model.Mention{AccountID: bob.ID, Kind: model.MentionAccount})
	freshReply := postAt(priv.ID, old.ID, at(21), "latereply")

	t.Run("history and threads", func(t *testing.T) {
		list, _, err := s.Messages().List(ctx, priv.ID, store.MessageList{SeqPage: store.SeqPage{Limit: 50, From: at(10)}})
		must(t, err)
		eq(t, "history from the first join", ids(list), "freshnews latereply ")
		list, _, err = s.Messages().List(ctx, priv.ID, store.MessageList{SeqPage: store.SeqPage{Limit: 50, From: at(10)}, Around: oldReply.Seq})
		must(t, err)
		eq(t, "around an old message too", ids(list), "freshnews latereply ")
		replies, _, err := s.Messages().Replies(ctx, old.ID, store.SeqPage{Limit: 50, From: at(10)})
		must(t, err)
		eq(t, "replies from the first join", ids(replies), "latereply ")
		all, _, err := s.Messages().List(ctx, priv.ID, store.MessageList{SeqPage: store.SeqPage{Limit: 50}})
		must(t, err)
		eq(t, "no floor, everything", len(all), 4)
		_ = freshReply
	})

	t.Run("search", func(t *testing.T) {
		find := func(who model.AccountID, word string) int {
			got, _, err := s.Search().Messages(ctx, store.SearchQuery{Scope: store.Scope{AccountID: who, PublicChannels: true},
				Terms: []store.SearchTerm{{Text: word}}}, store.Page{Limit: 10})
			must(t, err)
			return len(got)
		}
		eq(t, "the newcomer does not find what came before", find(bob.ID, "oldsecret"), 0)
		eq(t, "nor an old reply", find(bob.ID, "oldreply"), 0)
		eq(t, "finds what came after", find(bob.ID, "freshnews"), 1)
		eq(t, "the founder finds both", find(ann.ID, "oldsecret")+find(ann.ID, "freshnews"), 2)
	})

	t.Run("bookmarks", func(t *testing.T) {
		_, _, err := s.Bookmarks().Add(ctx, bob.ID, old.ID, model.Now())
		must(t, err)
		_, _, err = s.Bookmarks().Add(ctx, bob.ID, fresh.ID, model.Now())
		must(t, err)
		list, _, err := s.Bookmarks().List(ctx, bob.ID, store.Scope{AccountID: bob.ID, PublicChannels: true}, store.Page{Limit: 10})
		must(t, err)
		eq(t, "only the bookmark from after the first join", len(list) == 1 && list[0].MessageID == fresh.ID, true)
	})

	t.Run("mentions written by an edit", func(t *testing.T) {
		_, err := s.Messages().Edit(ctx, store.MessageEdit{ID: old.ID, Text: "@bob @cat", By: ann.ID, At: model.Now(),
			Mentions: []model.Mention{{AccountID: bob.ID, Kind: model.MentionAccount}, {AccountID: ann.ID, Kind: model.MentionAccount}}})
		must(t, err)
		mentions, err := s.Messages().Mentions(ctx, old.ID)
		must(t, err)
		eq(t, "the newcomer is not mentioned on an old message", len(mentions) == 1 && mentions[0].AccountID == ann.ID, true)
		n, err := s.Messages().MentionBroadly(ctx, old, store.BroadMention{Scope: store.BroadChannel})
		must(t, err)
		eq(t, "nor broadly", n, int64(0))
	})

	t.Run("coming back within the window keeps it, later does not", func(t *testing.T) {
		_, err := s.Members().RemoveDirect(ctx, priv.ID, bob.ID)
		must(t, err)
		m := joinAt(priv.ID, bob.ID, at(30))
		eq(t, "back soon: still the first join", m.VisibleFrom, at(10))
		_, err = s.Members().RemoveDirect(ctx, priv.ID, bob.ID)
		must(t, err)
		late := model.Now().Add(model.RejoinWindow + time.Minute)
		m = joinAt(priv.ID, bob.ID, late)
		eq(t, "back after the window: a newcomer again", m.VisibleFrom, late)
	})

	t.Run("a group brings someone in from their join", func(t *testing.T) {
		g := newGroup(t, s, "late", cat.ID)
		_, _, err := s.Members().AddGroup(ctx, model.ChannelGroup{ConversationID: priv.ID, GroupID: g.ID, Level: model.LevelMember, AddedAt: at(40)})
		must(t, err)
		accept(t, s, priv.ID, cat.ID)
		m, found, err := s.Members().Get(ctx, priv.ID, cat.ID)
		must(t, err)
		eq(t, "in through the group, with a floor", found && m.VisibleFrom > 0, true)
	})

	t.Run("public, knock and ad-hoc channels have none", func(t *testing.T) {
		pub := newChannel(t, s, "lobby", false, ann.ID)
		eq(t, "public", joinAt(pub.ID, bob.ID, at(50)).VisibleFrom, model.Time(0))
		knock := model.Conversation{Kind: model.KindChannel, Name: "door", Private: true, Knock: true, CreatedByID: ann.ID}
		must(t, s.Conversations().Create(ctx, &knock))
		eq(t, "knock", joinAt(knock.ID, bob.ID, at(50)).VisibleFrom, model.Time(0))
		adhoc := newAdhoc(t, s, ann.ID, cat.ID)
		eq(t, "ad-hoc", joinAt(adhoc.ID, bob.ID, at(50)).VisibleFrom, model.Time(0))
	})

	t.Run("made private: members keep what they had, newcomers start at their join", func(t *testing.T) {
		c := newChannel(t, s, "was-public", false, ann.ID)
		joinAt(c.ID, bob.ID, at(60))
		joinAt(c.ID, cat.ID, at(60))
		postAt(c.ID, "", at(61), "said in public")
		_, err := s.Members().RemoveDirect(ctx, c.ID, cat.ID)
		must(t, err)
		_, err = s.Conversations().MakePrivate(ctx, c.ID, false)
		must(t, err)
		m, _, err := s.Members().Get(ctx, c.ID, bob.ID)
		must(t, err)
		eq(t, "a member from before keeps everything", m.VisibleFrom, model.Time(0))
		eq(t, "someone added after starts at their join", joinAt(c.ID, cat.ID, at(70)).VisibleFrom, at(70))
		_, err = s.Members().RemoveDirect(ctx, c.ID, bob.ID)
		must(t, err)
		eq(t, "a member from before who steps out and back keeps everything", joinAt(c.ID, bob.ID, at(80)).VisibleFrom, model.Time(0))
	})
}

package storetest

import (
	"errors"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The helpers in this file make the things most tests start from. They fail
// the test on any error, so a test reads as what it checks.

// must fails the test if err is not nil.
func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// wantErr fails the test unless err has the wanted code.
func wantErr(t testing.TB, err error, want *model.Error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got error %v, want %s", err, want.Code)
	}
}

// eq fails the test unless got equals want.
func eq[T comparable](t testing.TB, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

// newAccount makes a local, claimed account with the given role.
func newAccount(t testing.TB, s store.Areas, username string, role model.Role) model.Account {
	t.Helper()
	a := model.Account{Username: username, DisplayName: username, Role: role}
	_, err := s.Accounts().Create(ctx, &a)
	must(t, err)
	return a
}

// newChannel makes a named channel with its creator as manager.
func newChannel(t testing.TB, s store.Areas, name string, private bool, creator model.AccountID) model.Conversation {
	t.Helper()
	c := model.Conversation{Kind: model.KindChannel, Name: name, Private: private, CreatedByID: creator}
	must(t, s.Conversations().Create(ctx, &c))
	_, err := s.Members().AddDirect(ctx, c.ID, creator, model.LevelManager, model.Now())
	must(t, err)
	return c
}

// newAdhoc makes an ad-hoc channel with these members; the first is its
// manager.
func newAdhoc(t testing.TB, s store.Areas, ids ...model.AccountID) model.Conversation {
	t.Helper()
	c := model.Conversation{Kind: model.KindChannel, Private: true, CreatedByID: ids[0], MemberKey: store.AdhocMemberKey(ids)}
	must(t, s.Conversations().Create(ctx, &c))
	for i, a := range ids {
		level := model.LevelMember
		if i == 0 {
			level = model.LevelManager
		}
		_, err := s.Members().AddDirect(ctx, c.ID, a, level, model.Now())
		must(t, err)
	}
	return c
}

// join adds an account to a conversation at a level.
func join(t testing.TB, s store.Areas, conv model.ConversationID, account model.AccountID, level model.Level) {
	t.Helper()
	_, err := s.Members().AddDirect(ctx, conv, account, level, model.Now())
	must(t, err)
}

// newGroup makes an ordinary group with these members.
func newGroup(t testing.TB, s store.Areas, handle string, members ...model.AccountID) model.Group {
	t.Helper()
	g := model.Group{Handle: handle, Name: handle}
	must(t, s.Groups().Create(ctx, &g))
	for _, a := range members {
		_, err := s.Groups().AddMember(ctx, g.ID, a, "", model.Now())
		must(t, err)
	}
	return g
}

// post stores a message the way the chat service does: in one transaction
// it takes the next seq, inserts the message with its mentions, and appends
// the message.created event. parent is "" for a top-level message.
func post(t testing.TB, s store.Store, conv model.ConversationID, author model.AccountID, parent model.MessageID, text string, mentions ...model.Mention) model.Message {
	t.Helper()
	m := model.Message{
		ID: model.NewID[model.MessageID](), ConversationID: conv, AuthorID: author, ParentID: parent,
		Text: text, CreatedAt: model.Now(),
	}
	must(t, s.Tx(ctx, func(tx store.Tx) error {
		seq, err := tx.Conversations().NextSeq(ctx, conv)
		if err != nil {
			return err
		}
		m.Seq = seq
		for i := range mentions {
			mentions[i].MessageID = m.ID
		}
		if err := tx.Messages().Insert(ctx, &m, mentions); err != nil {
			return err
		}
		return tx.Events().Append(ctx, model.Event{
			Type: model.EventMessageCreated, At: m.CreatedAt, ConversationID: conv, Seq: seq,
			MessageID: m.ID, Payload: map[string]any{"message": m},
		})
	}))
	return m
}

// level returns an account's level in a conversation, or "" if it is not a
// member.
func level(t testing.TB, s store.Areas, conv model.ConversationID, account model.AccountID) model.Level {
	t.Helper()
	m, found, err := s.Members().Get(ctx, conv, account)
	must(t, err)
	if !found {
		return ""
	}
	return m.Level
}

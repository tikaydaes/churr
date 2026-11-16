package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// A direct or sealed chat that was closed, or whose request waits or was
// declined, is read and not written in (rulings R78, R79).
func TestClosedAndWaitingChats(t *testing.T) {
	adhoc := model.Conversation{ID: "a", Kind: model.KindChannel, Private: true}
	sealed := model.Conversation{ID: "s", Kind: model.KindSealed, Private: true}
	state := func(c model.Conversation) State {
		return State{Actor: account(model.RoleMember), Conversation: c, Settings: DefaultSettings(), Member: &model.Member{Level: model.LevelMember}}
	}
	closed := func(c model.Conversation) model.Conversation { c.ClosedAt = 5; return c }
	waiting := func(c model.Conversation, s string) model.Conversation { c.RequestState = s; return c }
	for _, c := range []struct {
		name string
		st   State
		a    Action
		want model.Code
	}{
		{"an open chat takes posts", state(adhoc), Post, ""},
		{"a closed chat takes none", state(closed(adhoc)), Post, model.CodeForbidden},
		{"nor reactions", state(closed(adhoc)), React, model.CodeForbidden},
		{"nor typing", state(closed(adhoc)), Typing, model.CodeForbidden},
		{"nor new members", state(closed(adhoc)), AddAccount, model.CodeForbidden},
		{"a closed chat is still read", state(closed(adhoc)), Read, ""},
		{"and marked read", state(closed(adhoc)), MarkRead, ""},
		{"a closed sealed chat takes no sealed message", state(closed(sealed)), Post, model.CodeForbidden},
		{"a waiting chat takes no first message", state(waiting(adhoc, model.ChatWaiting)), Post, model.CodeForbidden},
		{"a waiting sealed chat neither", state(waiting(sealed, model.ChatWaiting)), Post, model.CodeForbidden},
		{"nor a file", state(waiting(adhoc, model.ChatWaiting)), Upload, model.CodeForbidden},
		{"a declined chat neither", state(waiting(adhoc, model.ChatDeclined)), Post, model.CodeForbidden},
		{"a waiting chat is read", state(waiting(adhoc, model.ChatWaiting)), Read, ""},
	} {
		if got := code(Check(c.st, c.a)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

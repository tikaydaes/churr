package storetest

import (
	"errors"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

func init() { register("modules", testModules) }

func testModules(t *testing.T, s store.Store) {
	ann := newAccount(t, s, "ann", model.RoleMember)
	owner := newAccount(t, s, "owner", model.RoleOwner)
	ch := newChannel(t, s, "games", false, ann.ID)
	other := newChannel(t, s, "other", false, ann.ID)
	inst := model.ModuleKey{Module: "dice", Scope: model.ModuleScopeInstance, Key: "k"}
	here := model.ModuleKey{Module: "dice", Scope: model.ModuleScopeConversation, Conversation: ch.ID, Key: "k"}
	there := here
	there.Conversation = other.ID
	mine := model.ModuleKey{Module: "dice", Scope: model.ModuleScopeAccount, Account: ann.ID, Key: "k"}
	value := func(k model.ModuleKey) string {
		t.Helper()
		v, found, err := s.Modules().Value(ctx, k)
		must(t, err)
		if !found {
			return "(none)"
		}
		return string(v)
	}

	t.Run("each scope keeps its own value", func(t *testing.T) {
		for i, k := range []model.ModuleKey{inst, here, there, mine} {
			must(t, s.Modules().PutValue(ctx, k, []byte{byte('a' + i)}, 0, 0))
		}
		eq(t, "instance", value(inst), "a")
		eq(t, "conversation", value(here), "b")
		eq(t, "another conversation", value(there), "c")
		eq(t, "account", value(mine), "d")
		elsewhere := inst
		elsewhere.Module = "cards"
		eq(t, "another module", value(elsewhere), "(none)")

		must(t, s.Modules().PutValue(ctx, here, []byte("replaced"), 0, 0))
		eq(t, "replaced", value(here), "replaced")
		must(t, s.Modules().DeleteValue(ctx, inst))
		must(t, s.Modules().DeleteValue(ctx, inst))
		eq(t, "deleted, twice", value(inst), "(none)")
	})

	t.Run("limits count what the value replaces", func(t *testing.T) {
		big := model.ModuleKey{Module: "limits", Scope: model.ModuleScopeConversation, Conversation: ch.ID, Key: "a"}
		must(t, s.Modules().PutValue(ctx, big, make([]byte, 6), 10, 100))
		// Replacing six bytes with ten fits in ten.
		must(t, s.Modules().PutValue(ctx, big, make([]byte, 10), 10, 100))
		second := big
		second.Key = "b"
		if err := s.Modules().PutValue(ctx, second, []byte("x"), 10, 100); !errors.Is(err, model.ErrTooLarge) {
			t.Fatalf("a conversation over its limit: %v", err)
		}
		eq(t, "nothing stored when refused", value(second), "(none)")
		// Another conversation has room, but the module as a whole is
		// held to its own limit.
		far := big
		far.Conversation = other.ID
		if err := s.Modules().PutValue(ctx, far, make([]byte, 10), 10, 15); !errors.Is(err, model.ErrTooLarge) {
			t.Fatalf("a module over its limit: %v", err)
		}
		must(t, s.Modules().PutValue(ctx, far, make([]byte, 5), 10, 15))
	})

	t.Run("values go with their conversation and their person", func(t *testing.T) {
		_, err := s.Conversations().Delete(ctx, other.ID)
		must(t, err)
		eq(t, "the conversation's", value(there), "(none)")
		_, err = s.Accounts().Remove(ctx, ann.ID, owner.ID, model.Now())
		must(t, err)
		eq(t, "the person's", value(mine), "(none)")
		eq(t, "the rest stay", value(here), "replaced")
	})
}

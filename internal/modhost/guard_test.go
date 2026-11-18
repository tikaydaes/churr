package modhost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// doing is a handler that runs whatever it is given.
type doing struct {
	command func(ctx context.Context, h *Host) (Reply, error)
	resolve func(ctx context.Context, h *Host) (Value, error)
}

func (d doing) Command(ctx context.Context, h *Host, _ Invocation) (Reply, error) {
	return d.command(ctx, h)
}
func (d doing) Resolve(ctx context.Context, h *Host, _ Marked) (Value, error) {
	return d.resolve(ctx, h)
}

// memStore keeps values in memory, with the limits it was last given.
type memStore struct {
	mu        sync.Mutex
	values    map[model.ModuleKey][]byte
	perConv   int64
	perModule int64
}

func (m *memStore) Value(_ context.Context, k model.ModuleKey) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[k]
	return v, ok, nil
}

func (m *memStore) PutValue(ctx context.Context, k model.ModuleKey, v []byte, perConv, perModule int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = map[model.ModuleKey][]byte{}
	}
	m.values[k], m.perConv, m.perModule = v, perConv, perModule
	return nil
}

func (m *memStore) DeleteValue(_ context.Context, k model.ModuleKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, k)
	return nil
}

// failures collects what the registry reports.
type failures struct {
	mu  sync.Mutex
	got []Failure
}

func (f *failures) add(_ context.Context, x Failure) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, x)
}

func (f *failures) list() []Failure {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Failure(nil), f.got...)
}

// A module keeps values in its own store, per scope: the conversation and
// the person are the call's, and another module's values are not its own.
func TestHostStore(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	ann := model.Account{ID: model.NewID[model.AccountID]()}
	bob := model.Account{ID: model.NewID[model.AccountID]()}
	here, there := model.NewID[model.ConversationID](), model.NewID[model.ConversationID]()
	var got string
	keep := func(scope model.ModuleScope, value string) func(ctx context.Context, h *Host) (Reply, error) {
		return func(_ context.Context, h *Host) (Reply, error) {
			old, _, err := h.Get(scope, "last")
			if err != nil {
				return Reply{}, err
			}
			got = string(old)
			return Reply{Text: "kept"}, h.Put(scope, "last", []byte(value))
		}
	}
	h := &doing{}
	other := &doing{command: func(_ context.Context, h *Host) (Reply, error) {
		v, ok, err := h.Get(model.ModuleScopeInstance, "last")
		got = string(v)
		if ok || err != nil {
			return Reply{}, errors.New("another module's value was seen")
		}
		return Reply{Text: "none"}, nil
	}}
	r, err := New(Options{Store: st}, Module{Name: "keeper", Handler: h, Commands: []Command{{Name: "keep"}}},
		Module{Name: "other", Handler: other, Commands: []Command{{Name: "peek"}}})
	if err != nil {
		t.Fatal(err)
	}
	run := func(who model.Account, conv model.ConversationID, scope model.ModuleScope, value string) string {
		t.Helper()
		h.command = keep(scope, value)
		if _, err := r.Command(ctx, who, conv, "keep", ""); err != nil {
			t.Fatal(err)
		}
		return got
	}
	run(ann, here, model.ModuleScopeConversation, "a")
	if g := run(bob, here, model.ModuleScopeConversation, "b"); g != "a" {
		t.Errorf("the conversation's value was %q", g)
	}
	if g := run(ann, there, model.ModuleScopeConversation, "c"); g != "" {
		t.Errorf("another conversation saw %q", g)
	}
	run(ann, here, model.ModuleScopeAccount, "mine")
	if g := run(bob, there, model.ModuleScopeAccount, "his"); g != "" {
		t.Errorf("another person saw %q", g)
	}
	if g := run(ann, there, model.ModuleScopeAccount, "mine again"); g != "mine" {
		t.Errorf("a person's value in another conversation was %q", g)
	}
	run(ann, here, model.ModuleScopeInstance, "all")
	if g := run(bob, there, model.ModuleScopeInstance, "all"); g != "all" {
		t.Errorf("the instance's value was %q", g)
	}
	if _, err := r.Command(ctx, ann, here, "peek", ""); err != nil {
		t.Error(err)
	}
	k := model.ModuleKey{Module: "keeper", Scope: model.ModuleScopeConversation, Conversation: here, Key: "last"}
	if string(st.values[k]) != "b" {
		t.Errorf("stored as %v", st.values)
	}

	h.command = func(_ context.Context, h *Host) (Reply, error) {
		for _, key := range []string{"", strings.Repeat("k", MaxKeyBytes+1), "\xff"} {
			if h.Put(model.ModuleScopeInstance, key, nil) == nil {
				return Reply{}, errors.New("the key " + key + " was taken")
			}
		}
		if h.Put("everywhere", "k", nil) == nil {
			return Reply{}, errors.New("a scope of the module's own was taken")
		}
		if err := h.Delete(model.ModuleScopeInstance, "last"); err != nil {
			return Reply{}, err
		}
		return Reply{Text: "ok"}, nil
	}
	if _, err := r.Command(ctx, ann, here, "keep", ""); err != nil {
		t.Error(err)
	}
	if _, ok := st.values[model.ModuleKey{Module: "keeper", Scope: model.ModuleScopeInstance, Key: "last"}]; ok {
		t.Error("the value was not deleted")
	}
	if err := NewHost(HostOptions{Module: "x"}).Put(model.ModuleScopeInstance, "k", nil); err == nil {
		t.Error("a host with no store kept something")
	}
}

// The host refuses a value over its limit itself, and hands the store the
// limits of a conversation and of a module to keep.
func TestHostStoreLimits(t *testing.T) {
	st := &memStore{}
	h := NewHost(HostOptions{Module: "keeper", Store: st, Conversation: model.NewID[model.ConversationID]()})
	if err := h.Put(model.ModuleScopeConversation, "k", make([]byte, MaxValueBytes+1)); !errors.Is(err, model.ErrTooLarge) {
		t.Errorf("a value over 64 KB: %v", err)
	}
	if len(st.values) != 0 {
		t.Error("it was stored")
	}
	if err := h.Put(model.ModuleScopeConversation, "k", make([]byte, MaxValueBytes)); err != nil {
		t.Fatal(err)
	}
	if st.perConv != 1<<20 || st.perModule != 64<<20 {
		t.Errorf("the store was given %d and %d", st.perConv, st.perModule)
	}
}

// A module that panics gives the person an ordinary refusal, is reported,
// and the server carries on; a resolve that panics leaves its text as text.
func TestPanicIsRefused(t *testing.T) {
	ctx := context.Background()
	var f failures
	h := doing{
		command: func(context.Context, *Host) (Reply, error) { panic("the dice fell off the table") },
		resolve: func(context.Context, *Host) (Value, error) { panic("again") },
	}
	r, err := New(Options{Failed: f.add}, Module{Name: "dice", Title: "Dice", Handler: h, Commands: []Command{{Name: "roll"}},
		Claims: []Claim{{Kind: model.KindDiceRoll, Open: "[[", Close: "]]", Pattern: `[0-9d]+`}}})
	if err != nil {
		t.Fatal(err)
	}
	ann := model.Account{ID: model.NewID[model.AccountID]()}
	_, err = r.Command(ctx, ann, "", "roll", "")
	var refusal *model.Error
	if !errors.As(err, &refusal) || refusal.Code != model.CodeUnavailable || !strings.Contains(refusal.Message, "Dice") {
		t.Fatalf("the refusal was %v", err)
	}
	if _, ok := r.Resolve(ctx, ann, "", model.KindDiceRoll, "1d6"); ok {
		t.Error("a resolve that panicked gave a value")
	}
	got := f.list()
	if len(got) != 2 || got[0].Entry != "/roll" || got[0].Panic != "the dice fell off the table" || !strings.Contains(got[0].Stack, "guard_test.go") ||
		got[0].Actor.ID != ann.ID || got[1].Entry != model.KindDiceRoll || got[1].Module != "dice" {
		t.Errorf("reported %+v", got)
	}
}

// A module has a deadline: one that does not answer in time is refused and
// reported, even if it never looks at its context, and its store refuses
// once the call is over. A request that goes away is not the module's fault.
func TestDeadline(t *testing.T) {
	var f failures
	st := &memStore{}
	release := make(chan struct{})
	late := make(chan error, 1)
	h := doing{command: func(_ context.Context, h *Host) (Reply, error) {
		<-release
		late <- h.Put(model.ModuleScopeInstance, "k", []byte("late"))
		return Reply{Text: "too late"}, nil
	}}
	r, err := New(Options{Failed: f.add, Store: st, Deadline: 20 * time.Millisecond},
		Module{Name: "slow", Handler: h, Commands: []Command{{Name: "wait"}}})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = r.Command(context.Background(), model.Account{}, "", "wait", "")
	if !errors.Is(err, model.ErrUnavailable) || time.Since(start) > time.Second {
		t.Fatalf("after %v: %v", time.Since(start), err)
	}
	close(release)
	if err := <-late; err == nil || len(st.values) != 0 {
		t.Errorf("the store took a value after the deadline: %v", err)
	}
	if got := f.list(); len(got) != 1 || got[0].Panic != nil || got[0].Entry != "/wait" {
		t.Errorf("reported %+v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	waits := doing{command: func(ctx context.Context, _ *Host) (Reply, error) {
		cancel()
		<-ctx.Done()
		return Reply{}, ctx.Err()
	}}
	r, _ = New(Options{Failed: f.add}, Module{Name: "waits", Handler: waits, Commands: []Command{{Name: "wait"}}})
	if _, err := r.Command(ctx, model.Account{}, "", "wait", ""); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled request: %v", err)
	}
	if len(f.list()) != 1 {
		t.Error("a cancelled request was reported as the module's failure")
	}
}

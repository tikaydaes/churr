package modhost

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// Defensive, though the code is ours (docs/DESIGN-MODULES.md section 6.2):
// every call into a module runs under a deadline, and a panic in it is
// recovered. Either way the person gets an ordinary refusal, the server
// carries on, and the registry's Failed hears of it, to log, audit and act.

// Deadline is how long a command or a resolve may take.
const Deadline = 2 * time.Second

// Failure is a call into a module that went wrong in a way no module means
// to: it panicked, or it gave no answer in time.
type Failure struct {
	Module string
	// Entry is what was called: "/roll", or the kind being resolved.
	Entry        string
	Actor        model.Account
	Conversation model.ConversationID
	// Panic is what the module panicked with, and Stack where; both are
	// empty when it ran out of time instead.
	Panic any
	Stack string
}

// String says what happened, for the log.
func (f Failure) String() string {
	if f.Panic != nil {
		return fmt.Sprintf("module %s panicked in %s: %v", f.Module, f.Entry, f.Panic)
	}
	return fmt.Sprintf("module %s gave no answer to %s within its deadline", f.Module, f.Entry)
}

// panicked carries a recovered panic out of the call's goroutine.
type panicked struct {
	value any
	stack string
}

func (p *panicked) Error() string { return fmt.Sprint(p.value) }

// call runs fn, one entry into module m, with a Host for the call, under
// the deadline and with panics recovered. The module runs on a goroutine of
// its own, so that one which ignores its context still cannot hold the
// person up past the deadline; what it does after that is thrown away, and
// its store refuses. A failure is reported and comes back as a refusal in
// the module's name; a cancelled request comes back as the context's error.
func (r *Registry) call(ctx context.Context, m *Module, actor model.Account, conv model.ConversationID, entry string, draws Draws, fn func(ctx context.Context, h *Host) error) error {
	callCtx, cancel := context.WithTimeout(ctx, r.deadline)
	defer cancel()
	h := NewHost(HostOptions{Module: m.Name, Actor: actor, Conversation: conv, Draws: draws, Store: r.store, Context: callCtx})
	done := make(chan error, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- &panicked{value: p, stack: string(debug.Stack())}
			}
		}()
		done <- fn(callCtx, h)
	}()
	fail := Failure{Module: m.Name, Entry: entry, Actor: actor, Conversation: conv}
	select {
	case err := <-done:
		p, ok := err.(*panicked)
		if !ok {
			return err
		}
		fail.Panic, fail.Stack = p.value, p.stack
		r.report(ctx, fail)
		return model.ErrUnavailable.With("The " + title(m) + " module failed and could not answer.")
	case <-callCtx.Done():
		if ctx.Err() != nil {
			// The request went away; the module did nothing wrong.
			return ctx.Err()
		}
		r.report(ctx, fail)
		return model.ErrUnavailable.With("The " + title(m) + " module did not answer in time.")
	}
}

// report hands a failure to Failed, if there is one.
func (r *Registry) report(ctx context.Context, f Failure) {
	if r.failed != nil {
		r.failed(context.WithoutCancel(ctx), f)
	}
}

// title is a module's name for people.
func title(m *Module) string {
	if m.Title != "" {
		return m.Title
	}
	return m.Name
}

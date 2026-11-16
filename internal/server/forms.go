package server

import (
	"context"

	"github.com/tikaydaes/churr/internal/forms"
	"github.com/tikaydaes/churr/internal/sched"
)

// wireForms builds forms, the scheduler and commands, and connects them:
//
//   - chat checks a form that arrives with a message, and fills in what
//     each reader may see of it, through chat.Forms, which forms implements;
//   - a `group` form's buttons join and leave groups through people;
//   - the scheduler posts through chat and closes forms whose time has come
//     through forms;
//   - bot forms, dialogs and bot commands reach bots through forms.Bots and
//     sched.BotCommands, which the bots package hands over with
//     Forms.SetBots and Commands.SetBots.
//
// It runs after live, people and chat exist.
func (s *Server) wireForms() {
	s.Forms = forms.New(forms.Deps{Store: s.Store, Perm: s.Perm, Chat: s.Chat, Groups: s.People, Log: s.Log})
	s.Chat.SetForms(s.Forms)
	s.Sched = sched.New(sched.Deps{Store: s.Store, Perm: s.Perm, Chat: s.Chat, Forms: s.Forms, Hub: s.Live, Log: s.Log,
		Around: s.People})
	// The watches (ruling R76) hear when someone becomes around, here or
	// on another server, and when someone blocks someone.
	s.People.SetWatchers(s.Sched)
	s.Chat.OnRemoteAround(s.Sched.Around)
	s.Commands = sched.NewCommands(s.Store, s.Perm, s.Chat)
}

// runForms starts the loop that fires scheduled items and closes forms. It
// stops when ctx ends or the server closes, whichever is first.
func (s *Server) runForms(ctx context.Context) {
	ctx, s.stopSched = context.WithCancel(ctx)
	// The waiting watches are held in memory by the person watched.
	if err := s.Sched.LoadWatches(ctx); err != nil {
		s.Log.Error("the waiting watches could not be read; none will fire until the next start", "err", err)
	}
	go s.Sched.Run(ctx)
}

// closeForms stops the scheduler and waits for a run it is in the middle of,
// so the database is not closed under it.
func (s *Server) closeForms() {
	if s.stopSched != nil {
		s.stopSched()
		s.Sched.Wait()
	}
}

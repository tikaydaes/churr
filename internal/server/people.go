package server

import (
	"context"
	"time"

	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/people"
)

// wirePeople builds the notify package and connects the people area:
//
//   - notify decides what each message means to each person; chat asks it
//     through chat.Notifier, and the status endpoint reads its totals;
//   - people tells others about accounts through the hub, changes
//     memberships (groups, roles, removal) through chat, and is told by the
//     hub when someone's presence changes;
//   - the `typing` frame of the live connection does what the operation
//     does;
//   - chat requests show who is asking as people shows an account, and
//     follow blocks made and lifted (rulings R77, R78).
//
// It runs after live, people and chat exist.
func (s *Server) wirePeople() {
	s.Notify = notify.New(s.Store, s.Perm)
	s.Chat.SetNotifier(s.Notify)
	s.People.Connect(people.Deps{Hub: s.Live, Chat: s.Chat, Blobs: s.Blobs, Totals: s.Notify, Log: s.Log})
	s.Live.OnPresence(s.People.PresenceChanged)
	s.Live.Handle("typing", s.Chat.TypingFrame)
	s.Live.Handle("presence.subscribe", s.People.PresenceFrame)
	s.Chat.SetAccountShower(s.People.Show)
	s.People.OnBlock(s.Chat.BlockChanged)
}

// sweepEvery is how often state that changes with the clock alone is looked
// at: a status or a mute running out, quiet hours starting or ending.
const sweepEvery = 20 * time.Second

// runPeople runs the people area's background work until ctx ends.
func (s *Server) runPeople(ctx context.Context) {
	go s.People.Run(ctx, sweepEvery)
	go func() {
		t := time.NewTicker(sweepEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := s.Chat.SweepMutes(ctx); err != nil && ctx.Err() == nil {
					s.Log.Error("the sweep of mutes that ran out failed", "err", err)
				}
			}
		}
	}()
}

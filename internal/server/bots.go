package server

import (
	"github.com/tikaydaes/churr/internal/bots"
)

// wireBots builds the bots package and connects it:
//
//   - a bot's form and dialog answers reach it through forms.Bots, and its
//     commands through sched.BotCommands, both of which bots implements;
//   - a dialog's form is checked and kept by forms, through bots.Dialogs;
//   - events addressed to one account (command.invoked, form.answered,
//     interaction.responded, webhook.requested, webhook.decided) go out
//     through the hub, and a revoked token's connections are closed there.
//
// An image a webhook names by address is fetched through bots.Images, the
// guarded path of link previews; whoever builds that hands it over with
// Bots.SetImages. Until then such an image is dropped, as it is when link
// previews are off.
//
// It runs after live, people, chat and forms exist.
func (s *Server) wireBots() {
	s.Bots = bots.New(bots.Deps{
		Store: s.Store, Perm: s.Perm, People: s.People, Chat: s.Chat, Hub: s.Live, Blobs: s.Blobs, Keys: s.Keys,
		Origin: s.Config.Origin(), Dialogs: s.Forms, Limits: s.opts.BotLimits, Log: s.Log,
	})
	s.Forms.SetBots(s.Bots)
	s.Commands.SetBots(s.Bots)
}

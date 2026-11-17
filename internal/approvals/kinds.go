package approvals

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// The kinds the instance has. Purge requests stay in the admin console:
// they have three outcomes (show again, keep hidden, erase), not two.
// Knocks and requests to join a channel on a hub are the channel's
// moderators' and managers', not the instance's, and stay in the channel.

// Webhooks is what the queue needs of the bots service, which keeps
// webhook requests (bots.Service implements it; packages of one layer do
// not import each other).
type Webhooks interface {
	Webhooks(ctx context.Context, actor model.Account, state string, p store.Page) ([]model.Webhook, string, error)
	ApproveWebhook(ctx context.Context, actor model.Account, id model.WebhookID) (model.Webhook, error)
	DeclineWebhook(ctx context.Context, actor model.Account, id model.WebhookID) (model.Webhook, error)
}

// RegisterBuiltin registers registrations, channel requests and webhook
// requests. The first two may be decided by the holders of the flag as
// well as by Owners and Admins; webhooks stay with Admins, as they do in
// the console (docs/FEATURES.md section 31).
func (s *Service) RegisterBuiltin(c *chat.Service, b Webhooks) {
	s.chat = c
	s.Register(Kind{Name: model.ApprovalRegistration, Flag: true,
		Approve: func(ctx context.Context, actor model.Account, a model.Approval) error {
			// The account is claimed: it signs in, and is in the
			// built-in groups and so in their channels, as anyone who
			// registers is.
			return c.Tx(ctx, func(tx store.Tx, rec *chat.Recorder) error {
				res, err := tx.Accounts().Claim(ctx, a.RequesterID, s.now())
				if err != nil {
					return err
				}
				return rec.Membership(ctx, res, "", "")
			})
		},
		// Declined, the account stays as it is, signing in to nothing: its
		// applicant is told when they next try, and its name stays taken.
	})
	s.Register(Kind{Name: model.ApprovalChannel, Flag: true,
		Describe: func(ctx context.Context, a *model.Approval) {
			var in chat.NewChannel
			if Payload(*a, &in) == nil {
				a.Summary = in.Name
			}
		},
		Approve: func(ctx context.Context, actor model.Account, a model.Approval) error {
			var in chat.NewChannel
			if err := Payload(a, &in); err != nil {
				return err
			}
			requester, err := s.store.Accounts().Get(ctx, a.RequesterID)
			if err != nil {
				return err
			}
			if !requester.Active() {
				return model.ErrConflict.With("The account that asked for it can no longer make channels.")
			}
			// Made in the requester's name, with them its first manager,
			// as if they had made it themselves then.
			_, err = c.CreateApprovedChannel(ctx, requester, in)
			return err
		},
	})
	if b != nil {
		s.Register(Kind{Name: model.ApprovalWebhook,
			List: func(ctx context.Context, actor model.Account) ([]model.Approval, error) {
				list, _, err := b.Webhooks(ctx, actor, "waiting", store.Page{Limit: model.MaxLimit})
				if err != nil {
					return nil, err
				}
				out := make([]model.Approval, 0, len(list))
				for _, w := range list {
					summary := w.Name
					if conv, err := s.store.Conversations().Get(ctx, w.ConversationID); err == nil && conv.Name != "" {
						summary += " in #" + conv.Name
					}
					out = append(out, model.Approval{ID: string(w.ID), Kind: model.ApprovalWebhook, RequesterID: w.RequestedByID,
						Summary: summary, Note: w.Purpose, State: model.ApprovalWaiting, CreatedAt: w.RequestedAt})
				}
				return out, nil
			},
			Approve: func(ctx context.Context, actor model.Account, a model.Approval) error {
				_, err := b.ApproveWebhook(ctx, actor, model.WebhookID(a.ID))
				return err
			},
			Decline: func(ctx context.Context, actor model.Account, a model.Approval, _ string) error {
				_, err := b.DeclineWebhook(ctx, actor, model.WebhookID(a.ID))
				return err
			},
		})
	}
}

// RequestChannel puts a member's request for a new channel in the queue,
// checked as making it would be. It is refused (conflict) for someone whose
// channels need no approval: they make it.
func (s *Service) RequestChannel(ctx context.Context, actor model.Account, in chat.NewChannel, note string) (model.Approval, error) {
	if s.chat == nil {
		return model.Approval{}, model.ErrUnavailable
	}
	wait, err := s.chat.CreationNeedsApproval(ctx, actor)
	if err != nil {
		return model.Approval{}, err
	}
	if !wait {
		return model.Approval{}, model.ErrConflict.With("Your channels need no approval: make it.")
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > model.MaxApprovalNote {
		return model.Approval{}, model.Invalid("note", "A note is at most 500 characters.")
	}
	if in, err = s.chat.CheckNewChannel(ctx, actor, in); err != nil {
		return model.Approval{}, err
	}
	n, err := s.store.Approvals().CountWaiting(ctx, []string{model.ApprovalChannel}, s.now())
	if err != nil {
		return model.Approval{}, err
	}
	if n[model.ApprovalChannel] >= model.MaxWaitingRegistrations {
		return model.Approval{}, model.RateLimited(3600).With("Too many channels are waiting for approval already. Try again later.")
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return model.Approval{}, err
	}
	a := model.Approval{Kind: model.ApprovalChannel, RequesterID: actor.ID, Note: note, Payload: payload}
	if err := s.Submit(ctx, &a); err != nil {
		return a, err
	}
	a.Summary = in.Name
	return a, nil
}

// Package people is accounts and everything attached to one: profiles as
// others see them, the caller's own account, invites, and making an account.
//
// A method whose name ends in Tx takes the caller's transaction and
// publishes nothing: registration (in auth) makes the account, its passkey,
// its session and its memberships together, and sends events after.
package people

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/notify"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/secret"
	"github.com/tikaydaes/churr/internal/store"
)

// Presence is what people needs of the live hub to show an account.
type Presence interface {
	Presence(account model.AccountID) model.Presence
}

// Service is the accounts service.
type Service struct {
	store    store.Store
	perm     *perm.Checker
	presence Presence
	// origin is the server's own origin, for the links of invites.
	origin string
	now    func() model.Time

	// What Connect supplies; see connect.go.
	hub    Hub
	convs  Conversations
	blobs  Blobs
	totals Totals
	log    *slog.Logger

	// views is what was last announced about each account, so a change is
	// sent once; see announce.go.
	viewsMu sync.Mutex
	views   map[model.AccountID]view

	// calls says who is in a call, set with SetCalls (calls.go).
	calls InCall
	// link is told when a profile changes (link.go).
	link Profiles
	// remote is the presence of accounts of other servers (link.go).
	remote RemotePresence
	// watchers and onShown are told when someone becomes around, and of
	// blocks (watch.go).
	watchers Watchers
	onShown  func(account model.AccountID)
	// onBlock is told of blocks made and lifted (watch.go).
	onBlock func(ctx context.Context, blocker, blocked model.AccountID, on bool)
}

// New makes the service. Connect gives it the rest once the services above
// it exist.
func New(s store.Store, p *perm.Checker, presence Presence, origin string) *Service {
	return &Service{store: s, perm: p, presence: presence, origin: origin, now: model.Now,
		hub: noHub{}, log: slog.Default(), views: map[model.AccountID]view{}}
}

// show fills in the public fields of an account for one viewer. Presence,
// status, availability and the do not disturb mark go only to accounts that
// share a conversation with the person (docs/PERMISSIONS.md section 4.3).
func (s *Service) show(a model.Account, shares bool) model.Account {
	now := s.now()
	a.Presence = model.PresenceOffline
	a.Status, a.Availability, a.DND, a.Claimed = nil, nil, false, nil
	if a.ClaimedAt.IsZero() && a.Local() {
		no := false
		a.Claimed = &no
	}
	if a.AvatarFileID != "" {
		a.AvatarURL = "/api/v1/files/" + string(a.AvatarFileID) + "/avatar"
	}
	if a.Removed() || !shares {
		return a
	}
	v := s.viewOf(a, now)
	a.Presence, a.Status, a.Availability, a.DND = v.presence, v.status, v.availability, v.dnd
	a.InCall = s.inCall(a.ID)
	return a
}

// Show returns an account as one viewer sees it. The caller has already
// established that the viewer may see it.
func (s *Service) Show(ctx context.Context, viewer, a model.Account) (model.Account, error) {
	shares, err := s.store.Accounts().SharesConversation(ctx, viewer.ID, a.ID)
	if err != nil {
		return a, err
	}
	return s.seenBy(ctx, viewer.ID, s.show(a, shares), shares)
}

// Get returns one account the actor can see, with the groups it is in.
func (s *Service) Get(ctx context.Context, actor model.Account, id model.AccountID) (model.Account, error) {
	a, err := s.store.Accounts().Get(ctx, id)
	if err != nil {
		return a, err
	}
	if err := s.perm.CanSeeAccount(ctx, actor, a); err != nil {
		return model.Account{}, err
	}
	hidden := notify.ReadPrefs(a).HideLastSeen
	shares, err := s.store.Accounts().SharesConversation(ctx, actor.ID, a.ID)
	if err != nil {
		return a, err
	}
	a = s.show(a, shares)
	// Roughly when the person was last here, for whoever is shown their
	// presence, unless they hide it (ruling R66). Never a time. An
	// account of another server has no sessions here to tell from.
	if (shares || actor.ID == a.ID) && !hidden && !a.Removed() && a.Local() && a.Role != model.RoleBot {
		a.LastSeen = s.lastSeen(ctx, a)
	}
	// Someone who has blocked the caller is online to them, and nothing
	// more (ruling R77).
	if a, err = s.seenBy(ctx, actor.ID, a, shares); err != nil {
		return a, err
	}
	a.GroupIDs, err = s.store.Groups().OfAccount(ctx, a.ID)
	return a, err
}

// lastSeen is the coarse answer to when an account was last here: online
// while others are shown it online, recently while it is connected or was
// in the last three days, then within a week, then a while ago. It comes
// from the account's sessions, whose last use the server notes at most once
// a minute; signing out everywhere therefore reads as a while ago.
func (s *Service) lastSeen(ctx context.Context, a model.Account) string {
	switch a.Presence {
	case model.PresenceOnline:
		return model.LastSeenOnline
	case model.PresenceAway:
		if s.presence != nil && s.presence.Presence(a.ID) != model.PresenceOffline {
			return model.LastSeenRecently
		}
	}
	now := s.now()
	sessions, err := s.store.Sessions().ForAccount(ctx, a.ID, now)
	if err != nil {
		return ""
	}
	var seen model.Time
	for _, ses := range sessions {
		if ses.LastSeenAt.After(seen) {
			seen = ses.LastSeenAt
		}
	}
	return model.LastSeenBucket(seen, now)
}

// List returns the accounts the actor can see that match the filter.
func (s *Service) List(ctx context.Context, actor model.Account, f store.AccountFilter, p store.Page) ([]model.Account, string, error) {
	if !actor.Active() {
		return nil, "", model.ErrUnauthenticated
	}
	vis := perm.AccountFilter(actor)
	f.SharedWith, f.LocalOrSharedWith = vis.SharedWith, vis.LocalOrSharedWith
	list, next, err := s.store.Accounts().List(ctx, f, p)
	if err != nil {
		return nil, "", err
	}
	// One query says who on the page shares a conversation with the
	// viewer, which decides who is shown with presence and status.
	shared, err := s.store.Accounts().SharedWith(ctx, actor.ID)
	if err != nil {
		return nil, "", err
	}
	shares := make(map[model.AccountID]bool, len(shared)+1)
	shares[actor.ID] = true
	for _, id := range shared {
		shares[id] = true
	}
	blockers, err := s.blockersOf(ctx, actor.ID)
	if err != nil {
		return nil, "", err
	}
	for i, a := range list {
		list[i] = s.show(a, shares[a.ID])
		if blockers[a.ID] && shares[a.ID] && a.ID != actor.ID && !a.Removed() {
			list[i] = blockedView(list[i])
		}
	}
	return list, next, nil
}

// prefsOf is the Prefs object of an account.
func prefsOf(a model.Account) model.Prefs {
	p := notify.ReadPrefs(a)
	return model.Prefs{ThreadView: p.ThreadView, MeetingPauses: p.MeetingPauses, AllowNotifyAnyway: a.AllowNotifyAnyway, HideLastSeen: p.HideLastSeen,
		AutoApproveDirect: p.AutoApproveDirect, AutoApproveSealed: p.AutoApproveSealed}
}

// quietOf is the QuietHours object of an account, or nil without a
// schedule.
func quietOf(a model.Account) *model.QuietHours {
	if a.QuietStart == "" {
		return nil
	}
	return &model.QuietHours{Start: a.QuietStart, End: a.QuietEnd, Days: a.QuietDays, TimeZone: a.TimeZone, SkipUntil: a.QuietSkipUntil}
}

// Me returns the caller's own account: the public shape plus what only they
// see.
func (s *Service) Me(ctx context.Context, a model.Account) (model.Me, error) {
	now := s.now()
	me := model.Me{Account: s.show(a, true)}
	var err error
	if me.GroupIDs, err = s.store.Groups().OfAccount(ctx, a.ID); err != nil {
		return me, err
	}
	me.Prefs = prefsOf(a)
	me.DndState = notify.DND(a, now)
	if a.AwayUntil != nil {
		me.AppearAway = &model.AppearAway{On: a.AwayUntil.IsZero() || a.AwayUntil.After(now), Until: *a.AwayUntil}
	}
	me.QuietHours = quietOf(a)
	if me.BlockedIDs, err = s.store.Blocks().List(ctx, a.ID); err != nil {
		return me, err
	}
	// Sign-in details come from one query over the tables that hold them.
	details, err := s.store.Accounts().AdminDetails(ctx, []model.AccountID{a.ID})
	if err != nil {
		return me, err
	}
	d := details[a.ID]
	me.PasskeyCount, me.HasTotp, me.IdentityCount = d.PasskeyCount, d.HasTotp, int64(len(d.ProviderIDs))
	return me, nil
}

// NewAccount is what CreateTx takes.
type NewAccount struct {
	Username       string
	DisplayName    string
	Role           model.Role
	WebAuthnHandle []byte
}

// CleanNames checks a username and display name and returns them in the
// form they are stored in. It is used before a passkey ceremony begins, so
// a bad name is refused before the person has made a passkey.
func CleanNames(username, displayName string) (string, string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !model.ValidUsername(username) {
		return "", "", model.Invalid("username", "A username is 2 to 32 characters: letters, digits, dot, - and _, starting with a letter or digit.")
	}
	if displayName == "" {
		displayName = username
	}
	clean, ok := model.CleanDisplayName(displayName)
	if !ok {
		return "", "", model.Invalid("display_name", "A display name is 1 to 64 characters with no control characters.")
	}
	return username, clean, nil
}

// CreateTx makes a local, claimed account inside the caller's transaction
// and returns the channel memberships it gained through the built-in groups.
func (s *Service) CreateTx(ctx context.Context, tx store.Tx, in NewAccount) (model.Account, store.MembershipResult, error) {
	username, display, err := CleanNames(in.Username, in.DisplayName)
	if err != nil {
		return model.Account{}, store.MembershipResult{}, err
	}
	a := model.Account{Username: username, DisplayName: display, Role: in.Role, WebAuthnHandle: in.WebAuthnHandle, CreatedAt: s.now()}
	res, err := tx.Accounts().Create(ctx, &a)
	return a, res, err
}

// NewInvite is what CreateInvite takes.
type NewInvite struct {
	Role             model.Role
	GroupIDs         []model.GroupID
	ConversationIDs  []model.ConversationID
	MaxUses          *int
	ExpiresInSeconds *int64
}

const inviteDefaultLife = 7 * 24 * time.Hour

func (s *Service) withURL(i model.Invite) model.Invite {
	i.URL = s.origin + "/join/" + i.Code
	if i.GroupIDs == nil {
		i.GroupIDs = []model.GroupID{}
	}
	if i.ConversationIDs == nil {
		i.ConversationIDs = []model.ConversationID{}
	}
	return i
}

// CreateInvite makes an invite link (docs/AUTH.md section 4.2).
func (s *Service) CreateInvite(ctx context.Context, actor model.Account, in NewInvite) (model.Invite, error) {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return model.Invite{}, err
	}
	if in.Role == "" {
		in.Role = model.RoleMember
	}
	now := s.now()
	inv := model.Invite{Role: in.Role, GroupIDs: in.GroupIDs, ConversationIDs: in.ConversationIDs, MaxUses: 1,
		ExpiresAt: now.Add(inviteDefaultLife), CreatedByID: actor.ID, CreatedAt: now}
	if in.MaxUses != nil {
		if *in.MaxUses < 0 {
			return inv, model.Invalid("max_uses", "max_uses is 0 for no limit, or a positive number.")
		}
		inv.MaxUses = *in.MaxUses
	}
	if in.ExpiresInSeconds != nil {
		if *in.ExpiresInSeconds < 0 {
			return inv, model.Invalid("expires_in_seconds", "expires_in_seconds is 0 for no expiry, or a positive number.")
		}
		inv.ExpiresAt = 0
		if *in.ExpiresInSeconds > 0 {
			inv.ExpiresAt = now.Add(time.Duration(*in.ExpiresInSeconds) * time.Second)
		}
	}
	switch in.Role {
	case model.RoleMember:
		if err := perm.CheckServer(actor, perm.CreateMemberInvite, set); err != nil {
			return inv, err
		}
		if !actor.Role.IsAdmin() {
			// A member's invite is single-use, lasts at most seven
			// days and carries nothing.
			if len(in.GroupIDs) > 0 || len(in.ConversationIDs) > 0 || inv.MaxUses != 1 ||
				inv.ExpiresAt.IsZero() || inv.ExpiresAt.After(now.Add(inviteDefaultLife)) {
				return inv, model.ErrForbidden.With("You can make single-use invites for members that last up to seven days.")
			}
		}
	case model.RoleGuest:
		if err := perm.CheckServer(actor, perm.CreateGuestInvite, set); err != nil {
			return inv, err
		}
		if len(in.ConversationIDs) == 0 {
			return inv, model.Invalid("conversation_ids", "A guest invite must name at least one channel, since a guest sees nothing else.")
		}
	default:
		return inv, model.Invalid("role", "An invite is for a member or a guest.")
	}
	for _, g := range in.GroupIDs {
		grp, err := s.store.Groups().Get(ctx, g, "")
		if err != nil {
			return inv, err
		}
		if grp.Builtin != "" || grp.ProviderID != "" {
			return inv, model.Invalid("group_ids", "Built-in groups and groups filled from a provider cannot be on an invite.")
		}
	}
	for _, c := range in.ConversationIDs {
		st, err := s.perm.Can(ctx, actor, perm.AddAccount, c)
		if err != nil {
			return inv, err
		}
		if !st.Conversation.Named() {
			return inv, model.Invalid("conversation_ids", "An invite can name named channels only.")
		}
	}
	inv.Code = secret.NewInviteCode()
	err = s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Invites().Create(ctx, &inv); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(actor.ID), ActorKind: "account",
			Action: "invite.created", TargetKind: "invite", TargetID: string(inv.ID),
			Detail: map[string]any{"role": inv.Role, "max_uses": inv.MaxUses, "expires_at": inv.ExpiresAt}})
	})
	return s.withURL(inv), err
}

// ListInvites lists every invite for an admin, and a member's own.
func (s *Service) ListInvites(ctx context.Context, actor model.Account, p store.Page) ([]model.Invite, string, error) {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return nil, "", err
	}
	by := actor.ID
	if perm.CheckServer(actor, perm.ListAllInvites, set) == nil {
		by = ""
	} else if !actor.Role.IsFullMember() {
		return nil, "", model.ErrForbidden
	}
	list, next, err := s.store.Invites().List(ctx, by, p)
	for i := range list {
		list[i] = s.withURL(list[i])
	}
	return list, next, err
}

// RevokeInvite withdraws an invite: any for an admin, their own for a member.
func (s *Service) RevokeInvite(ctx context.Context, actor model.Account, id model.InviteID) error {
	set, err := s.perm.Settings(ctx)
	if err != nil {
		return err
	}
	inv, err := s.store.Invites().Get(ctx, id)
	if err != nil {
		return err
	}
	if perm.CheckServer(actor, perm.RevokeAnyInvite, set) != nil && inv.CreatedByID != actor.ID {
		// An invite someone else made does not exist for a member.
		return model.ErrNotFound
	}
	now := s.now()
	return s.store.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Invites().Revoke(ctx, id, now); err != nil {
			return err
		}
		return tx.Audit().Append(ctx, model.AuditEntry{At: now, ActorID: string(actor.ID), ActorKind: "account",
			Action: "invite.revoked", TargetKind: "invite", TargetID: string(id), Detail: map[string]any{"role": inv.Role, "uses": inv.Uses}})
	})
}

// CheckInvite returns a usable invite by its code. Checking uses nothing up.
func (s *Service) CheckInvite(ctx context.Context, code string) (model.Invite, error) {
	inv, err := s.store.Invites().ByCode(ctx, code)
	if err != nil {
		return inv, err
	}
	if !inv.Usable(s.now()) {
		return model.Invite{}, model.ErrNotFound
	}
	return inv, nil
}

package perm

import (
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// ServerAction is something done to the server as a whole (section 9.1).
type ServerAction string

// The rows of section 9.1 that depend only on the actor and the settings.
const (
	Browse             ServerAction = "browse"
	CreateChannel      ServerAction = "create_channel"
	StartAdhoc         ServerAction = "start_adhoc"
	StartSealed        ServerAction = "start_sealed"
	CreateMemberInvite ServerAction = "create_member_invite"
	CreateGuestInvite  ServerAction = "create_guest_invite"
	ListAllInvites     ServerAction = "list_all_invites"
	RevokeAnyInvite    ServerAction = "revoke_any_invite"
	JoinOpenGroup      ServerAction = "join_open_group"
	ManageGroups       ServerAction = "manage_groups"
	ManageBots         ServerAction = "manage_bots"
	ManageIntegrations ServerAction = "manage_integrations"
	ChangeSettings     ServerAction = "change_settings"
	DefineSections     ServerAction = "define_sections"
	// DefineReferencePatterns: the server's reference patterns (R73).
	DefineReferencePatterns ServerAction = "define_reference_patterns"
	AddEmoji                ServerAction = "add_emoji"
	RemoveAnyEmoji          ServerAction = "remove_any_emoji"
	ManageCommunityReplies  ServerAction = "manage_community_replies"
	ManageFilters           ServerAction = "manage_filters"
	DecidePurgeRequests     ServerAction = "decide_purge_requests"
	ReadAuditLog            ServerAction = "read_audit_log"
	RunExport               ServerAction = "run_export"
	ManageLinks             ServerAction = "manage_links"
	RegisterCommands        ServerAction = "register_commands"
	MakePersonalToken       ServerAction = "make_personal_token"
	OwnProfile              ServerAction = "own_profile"
	OwnPersonalThings       ServerAction = "own_personal_things"
	ChangeRole              ServerAction = "change_role"
	UseAdminConsole         ServerAction = "use_admin_console"
	CreateBlock             ServerAction = "create_block"
	Knock                   ServerAction = "knock"
	SendBroadcast           ServerAction = "send_broadcast"
	// ManageModules: switch built-in modules on and off for the instance,
	// and order and rename commands (docs/DESIGN-MODULES.md section 6.5).
	ManageModules ServerAction = "manage_modules"
)

// CheckServer answers a server-wide question that depends only on the actor
// and the settings.
func CheckServer(actor model.Account, a ServerAction, set Settings) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	r := actor.Role
	ok := false
	switch a {
	case Browse, CreateChannel, JoinOpenGroup, MakePersonalToken, Knock:
		ok = r.IsFullMember()
	case StartAdhoc:
		// Guests and bots too, with accounts they can already see
		// (decision P13, ruling R7); a remote account's home server
		// starts its chats.
		ok = r != model.RoleRemote
	case StartSealed:
		if !set.SealedEnabled {
			return model.ErrFeatureOff
		}
		ok = r.IsFullMember() || r == model.RoleGuest
	case CreateMemberInvite:
		ok = r.IsAdmin() || (r == model.RoleMember && set.MembersMayInvite)
	case AddEmoji:
		ok = r.IsAdmin() || (r == model.RoleMember && set.MembersMayAddEmoji)
	case CreateGuestInvite, ListAllInvites, RevokeAnyInvite, ManageGroups, ManageBots, ManageIntegrations, ChangeSettings,
		DefineSections, DefineReferencePatterns, RemoveAnyEmoji, ManageCommunityReplies, ManageFilters, DecidePurgeRequests, ReadAuditLog, UseAdminConsole,
		SendBroadcast, ManageModules:
		ok = r.IsAdmin()
	case ChangeRole:
		// An Admin changes a guest's or a member's role between the two;
		// the rest is an Owner's (CheckSetRole, ruling R104).
		ok = r.IsAdmin()
	case RunExport, ManageLinks:
		ok = r == model.RoleOwner
	case RegisterCommands:
		ok = r == model.RoleBot
	case OwnProfile:
		ok = r != model.RoleRemote
	case OwnPersonalThings:
		ok = r.IsFullMember() || r == model.RoleGuest
	case CreateBlock:
		if !set.BlockingEnabled {
			return model.ErrFeatureOff
		}
		ok = r.IsFullMember() || r == model.RoleGuest
	}
	if !ok {
		return forbidden("Your role on this server does not allow that.")
	}
	return nil
}

// AccountAction is something an admin does to another account.
type AccountAction string

// The rows of section 9.1 that depend on who the target is (decision P15).
const (
	RemoveAccount    AccountAction = "remove_account"
	BanAccount       AccountAction = "ban_account"
	TimeOutAccount   AccountAction = "time_out_account"
	MakeRecoveryLink AccountAction = "make_recovery_link"
	AddToGroup       AccountAction = "add_to_group"
)

// CheckOnAccount answers whether actor may act on target: admins act on
// members, guests and bots; only an owner acts on an admin; nobody removes,
// bans or times out an owner, another owner included.
func CheckOnAccount(actor model.Account, a AccountAction, target model.Account) error {
	if !actor.Active() {
		return model.ErrUnauthenticated
	}
	if !actor.Role.IsAdmin() {
		return forbidden("Only admins can do that.")
	}
	if a == AddToGroup {
		// Section 9.3: remote accounts cannot be in groups.
		if target.Role == model.RoleRemote || !target.Active() {
			return forbidden("Only a local, active account can be in a group.")
		}
		return nil
	}
	switch target.Role {
	case model.RoleOwner:
		// An owner makes a recovery link for themselves only: one for
		// another owner would let them become that owner. The operator
		// has churr recovery for that.
		if a == MakeRecoveryLink && actor.ID == target.ID {
			return nil
		}
		return forbidden("Nobody can do that to an owner.")
	case model.RoleAdmin:
		if actor.Role != model.RoleOwner {
			return forbidden("Only the owner can do that to an admin.")
		}
	case model.RoleRemote:
		if a != TimeOutAccount {
			return forbidden("That account lives on another server.")
		}
	}
	return nil
}

// AccountFilter restricts Accounts.List to what the actor may see (section
// 4.3): owner, admin and member see every local account and the remote ones
// they share a conversation with; guests and bots only accounts in a
// conversation with them.
func AccountFilter(actor model.Account) store.AccountFilter {
	if actor.Role.IsFullMember() {
		return store.AccountFilter{LocalOrSharedWith: actor.ID}
	}
	return store.AccountFilter{SharedWith: actor.ID}
}

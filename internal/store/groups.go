package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Groups is the `groups` and `group_members` tables.
//
// The two built-in groups have no group_members rows: @everyone is every
// local owner, admin and member that is claimed and not removed, and @admins
// is the owner and the admins. Every method here answers for them from
// accounts.role.
//
// Whenever who is in a group changes, the store works out `members` again
// for every channel the group is in (docs/SCHEMA.md section 5) and returns
// what changed.
type Groups interface {
	// EnsureBuiltin creates the two built-in groups if they do not exist.
	// It is called at every start.
	EnsureBuiltin(ctx context.Context) error
	// Create inserts an ordinary group. An empty ID and zero CreatedAt are
	// filled in. The handle must be free among group handles and local
	// usernames and not reserved (model.ErrNameTaken).
	Create(ctx context.Context, g *model.Group) error
	// Get returns one group with MemberCount filled in. IsMember is filled
	// for the viewer; pass "" to leave it false.
	Get(ctx context.Context, id model.GroupID, viewer model.AccountID) (model.Group, error)
	// ByHandle finds a group by its handle.
	ByHandle(ctx context.Context, handle string) (model.Group, error)
	// Builtin returns one of the two built-in groups: model.GroupEveryone
	// or model.GroupAdmins.
	Builtin(ctx context.Context, name string) (model.Group, error)
	// List returns every group ordered by handle, with MemberCount, and
	// IsMember for the viewer.
	List(ctx context.Context, viewer model.AccountID) ([]model.Group, error)
	// Update reads an ordinary group, lets fn change its handle (checked
	// as in Create), name, description, colour, open flag and provider
	// mapping, and writes it back. A built-in group cannot be changed
	// (model.ErrConflict).
	Update(ctx context.Context, id model.GroupID, fn func(g *model.Group) error) (model.Group, error)
	// Delete removes an ordinary group; the channels it was in lose the
	// members it alone brought. A built-in group cannot be deleted
	// (model.ErrConflict).
	Delete(ctx context.Context, id model.GroupID) (MembershipResult, error)

	// AddMember puts a local, active account in an ordinary group.
	// model.ErrAlreadyExists if it is in it; model.ErrConflict for a
	// built-in group or for a remote, removed or unclaimed account.
	AddMember(ctx context.Context, group model.GroupID, account, by model.AccountID, at model.Time) (MembershipResult, error)
	// RemoveMember takes an account out of an ordinary group.
	// model.ErrNotFound if it is not in it.
	RemoveMember(ctx context.Context, group model.GroupID, account model.AccountID) (MembershipResult, error)
	// MemberIDs returns who is in a group, built-in ones included.
	MemberIDs(ctx context.Context, group model.GroupID) ([]model.AccountID, error)
	// IsMember reports whether an account is in a group, built-in ones
	// included.
	IsMember(ctx context.Context, group model.GroupID, account model.AccountID) (bool, error)
	// OfAccount returns the ordinary groups an account is in. Built-in
	// groups are left out, as the account object leaves them out.
	OfAccount(ctx context.Context, account model.AccountID) ([]model.GroupID, error)
	// Channels returns the channels a group has been added to.
	Channels(ctx context.Context, group model.GroupID) ([]model.ChannelGroup, error)

	// SeenBy reports whether a guest or bot may see a group: it is in it,
	// or the group has been added to a conversation the account is in
	// (docs/PERMISSIONS.md decision P6). Owners, admins and members see
	// every group and are not asked about here.
	SeenBy(ctx context.Context, group model.GroupID, account model.AccountID) (bool, error)
}

package perm

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
)

// Who may be in a call: the rows of docs/PERMISSIONS.md section 7 for
// "Start or join a call" and docs/CALLS.md section 3, and the same rule for
// someone on another server joining a call this server hosts (section 16).
func TestCallRules(t *testing.T) {
	const (
		member, guest, bot, remote = model.RoleMember, model.RoleGuest, model.RoleBot, model.RoleRemote
		none                       = model.Level("")
		reader, lmember, manager   = model.LevelReader, model.LevelMember, model.LevelManager
	)
	for _, c := range []struct {
		name string
		st   State
		want model.Code
	}{
		{"a member starts or joins", state(member, "PUB", lmember), ok},
		{"a manager", state(member, "PRIV", manager), ok},
		{"a guest at level member", state(guest, "PRIV", lmember), ok},
		{"a bot at level member", state(bot, "PUB", lmember), ok},
		{"a reader sees the call and cannot join", state(member, "PUB", reader), forbid},
		{"someone reading a public channel must join it first", state(member, "PUB", none), forbid},
		{"not in a private channel", state(member, "PRIV", none), notFound},
		{"archived", state(member, "PUB", lmember, archived), isArch},
		{"timed out", state(member, "PUB", lmember, timedOut), isTimed},
		{"calls switched off", state(member, "PUB", lmember, callsOff), featOff},
		{"a sealed chat's two people", state(member, "SEALED", lmember), ok},
		{"a remote account does not act here", state(remote, "PUB", lmember, shared), unauth},
	} {
		want(t, c.name, Check(c.st, StartCall), c.want)
	}

	away := func(st *State) { st.Actor.ServerID = "0193a000-0000-7000-8000-00000000beef" }
	for _, c := range []struct {
		name string
		st   State
		want model.Code
	}{
		{"a remote member of a shared channel", state(remote, "PUB", lmember, shared, away), ok},
		{"a remote manager", state(remote, "PUB", manager, shared, away), ok},
		{"a remote reader", state(remote, "PUB", reader, shared, away), forbid},
		{"a remote account that is not in the conversation", state(remote, "PUB", none, shared, away), forbid},
		{"a remote member, timed out here", state(remote, "PUB", lmember, shared, away, timedOut), isTimed},
		{"a remote member, archived", state(remote, "PUB", lmember, shared, away, archived), isArch},
		{"a remote member, calls off", state(remote, "PUB", lmember, shared, away, callsOff), featOff},
		{"a removed remote account", state(remote, "PUB", lmember, shared, away, removed), forbid},
		{"a local account cannot use the remote rule", state(member, "PUB", lmember, shared), forbid},
	} {
		want(t, c.name, CheckRemoteCall(c.st), c.want)
	}
}

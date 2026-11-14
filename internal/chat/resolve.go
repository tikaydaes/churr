package chat

import (
	"context"
	"strconv"
	"strings"

	"github.com/tikaydaes/churr/internal/markdown"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/perm"
	"github.com/tikaydaes/churr/internal/store"
)

// resolver looks names up for markdown.Resolve on behalf of one author in
// one conversation. A lookup that fails for any reason leaves the text as
// typed; the first database error is kept and returned by read.
type resolver struct {
	ctx   context.Context
	a     store.Areas
	perm  *perm.Checker
	st    perm.State
	err   error
	names map[model.AccountID]bool // accounts mentioned by name
}

func (r *resolver) fail(err error) {
	if r.err == nil && model.CodeOf(err) == model.CodeInternal {
		r.err = err
	}
}

func (r *resolver) Account(username string) (model.AccountID, bool) {
	acc, err := r.a.Accounts().ByUsername(r.ctx, username)
	if err != nil {
		r.fail(err)
		return "", false
	}
	// A removed account cannot be mentioned, an unclaimed one is given no
	// mentions, and a name the author may not know stays plain text.
	if !acc.Active() || r.perm.CanSeeAccount(r.ctx, r.st.Actor, acc) != nil {
		return "", false
	}
	r.names[acc.ID] = true
	return acc.ID, true
}

// RemoteAccount resolves `@username@server.name`: an account of another
// server, and only in a conversation shared with that server (ruling R29,
// docs/LINK-PROTOCOL.md section 11). Anywhere else it stays plain text.
func (r *resolver) RemoteAccount(username, server string) (model.AccountID, bool) {
	sv, ok := r.sharedServer(server)
	if !ok {
		return "", false
	}
	acc, err := r.a.Streams().Shadow(r.ctx, sv, username)
	if err != nil {
		r.fail(err)
		return "", false
	}
	if acc.Removed() {
		return "", false
	}
	r.names[acc.ID] = true
	return acc.ID, true
}

// sharedServer finds a server by name if the conversation being written in
// is shared with it.
func (r *resolver) sharedServer(name string) (model.ServerID, bool) {
	conv := r.st.Conversation
	if !conv.IsShared() {
		return "", false
	}
	servers, err := r.a.Streams().Servers(r.ctx, conv.StreamID)
	if err != nil {
		r.fail(err)
		return "", false
	}
	for _, ss := range servers {
		if ss.Name == name && ss.State == store.StreamServerActive {
			return ss.ServerID, true
		}
	}
	return "", false
}

func (r *resolver) Group(handle string) (model.GroupID, bool) {
	// @everyone is a broadcast in all but name (docs/FEATURES.md 2.5).
	if handle == model.GroupEveryone && !r.Broadcast("channel") {
		return "", false
	}
	g, err := r.a.Groups().ByHandle(r.ctx, handle)
	if err != nil {
		r.fail(err)
		return "", false
	}
	return g.ID, true
}

func (r *resolver) Broadcast(scope string) bool { return perm.Check(r.st, perm.Broadcast) == nil }

// Channel looks up a channel link, `<#conversation-id>` (ruling R52). It
// resolves for a named channel the author can see, and for nothing else, so
// a message cannot be used to find out which private channels exist. Names
// are never looked up: `#ops` typed as characters is plain text.
func (r *resolver) Channel(id model.ConversationID) (string, bool) {
	st, err := r.perm.Load(r.ctx, r.st.Actor, id)
	if err != nil {
		r.fail(err)
		return "", false
	}
	if !st.CanSee() || !st.Conversation.Named() {
		return "", false
	}
	return st.Conversation.Name, true
}

// Emoji resolves one of this server's own custom emoji, or, written
// `:name@server.name:`, one of another server's in a conversation shared
// with that server (ruling R28).
func (r *resolver) Emoji(name, server string) (model.EmojiID, bool) {
	if server != "" {
		sv, ok := r.sharedServer(server)
		if !ok {
			return "", false
		}
		e, err := r.a.Streams().RemoteEmoji(r.ctx, sv, name)
		if err != nil {
			r.fail(err)
			return "", false
		}
		return e.ID, true
	}
	e, err := r.a.Emoji().ByName(r.ctx, name)
	if err != nil {
		r.fail(err)
		return "", false
	}
	return e.ID, true
}

// content is what the server works out from a message's text.
type content struct {
	entities   []model.Entity
	tasks      []markdown.Task
	mentions   []model.Mention
	cannotRead []model.AccountID
	named      []model.AccountID // accounts mentioned by name that got a row
}

// read parses a text, resolves its entities and works out its mention rows
// (docs/FEATURES.md section 2.5).
func (s *Service) read(ctx context.Context, a store.Areas, st perm.State, text string) (content, error) {
	var out content
	r := &resolver{ctx: ctx, a: a, perm: s.perm.With(a), st: st, names: map[model.AccountID]bool{}}
	doc := markdown.Parse(text)
	out.entities = markdown.Resolve(doc, r)
	out.tasks = markdown.Tasks(doc)
	if r.err != nil {
		return out, r.err
	}
	if len(out.tasks) > model.MaxChecklistItems {
		return out, model.Invalid("text", "A message holds at most 100 checklist items.")
	}

	conv := st.Conversation
	members, err := a.Members().IDs(ctx, conv.ID)
	if err != nil {
		return out, err
	}
	isMember := make(map[model.AccountID]bool, len(members))
	for _, id := range members {
		isMember[id] = true
	}
	// The first kind in the order account, group, channel, here wins.
	rank := map[string]int{model.MentionAccount: 0, model.MentionGroup: 1, model.MentionChannel: 2, model.MentionHere: 3}
	kinds := map[model.AccountID]string{}
	add := func(id model.AccountID, kind string) {
		if cur, ok := kinds[id]; !ok || rank[kind] < rank[cur] {
			kinds[id] = kind
		}
	}
	for _, e := range out.entities {
		switch e.Type {
		case model.EntityAccount:
			add(e.AccountID, model.MentionAccount)
		case model.EntityGroup:
			ids, err := a.Groups().MemberIDs(ctx, e.GroupID)
			if err != nil {
				return out, err
			}
			for _, id := range ids {
				add(id, model.MentionGroup)
			}
		case model.EntityBroadcast:
			for _, id := range members {
				if e.Scope == "here" {
					if s.hub.Presence(id) == model.PresenceOnline {
						add(id, model.MentionHere)
					}
				} else {
					add(id, model.MentionChannel)
				}
			}
		}
	}
	if len(kinds) == 0 {
		return out, nil
	}
	ids := make([]model.AccountID, 0, len(kinds))
	for id := range kinds {
		ids = append(ids, id)
	}
	accounts, err := a.Accounts().GetMany(ctx, ids)
	if err != nil {
		return out, err
	}
	for _, acc := range accounts {
		kind := kinds[acc.ID]
		canRead := isMember[acc.ID] || (conv.Public() && acc.Role.IsFullMember())
		if kind == model.MentionAccount && !canRead && acc.ID != st.Actor.ID {
			// The entity is stored and drawn; the author is told this
			// person will not be notified.
			out.cannotRead = append(out.cannotRead, acc.ID)
		}
		// No row for the author, for someone who cannot read the
		// conversation, or for an account that cannot be told.
		if acc.ID == st.Actor.ID || !canRead || !acc.Active() {
			continue
		}
		if st.Settings.BlockingEnabled {
			blocked, err := a.Blocks().Has(ctx, acc.ID, st.Actor.ID)
			if err != nil {
				return out, err
			}
			if blocked {
				continue
			}
		}
		out.mentions = append(out.mentions, model.Mention{AccountID: acc.ID, Kind: kind})
		if kind == model.MentionAccount {
			out.named = append(out.named, acc.ID)
		}
	}
	return out, nil
}

// checklist turns the tasks of a text into stored items and sets the item
// ids on the task entities. had is what the message carried before (nil for
// a new message): an item whose text is unchanged keeps its identifier and
// state, a reworded item keeps them if it is in the same position, and any
// other item is new (docs/MESSAGE-FORMAT.md section 4).
func checklist(entities []model.Entity, tasks []markdown.Task, had []store.ChecklistItem) []store.ChecklistItem {
	used := map[string]bool{}
	next := 0
	for _, h := range had {
		// New identifiers never reuse one the message has had, removed
		// items included, so an item's history stays its own.
		if n, err := strconv.Atoi(strings.TrimPrefix(h.ItemID, "t")); err == nil && n > next {
			next = n
		}
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		for _, h := range had {
			if h.RemovedAt.IsZero() && !used[h.ItemID] && h.Text == t.Text && (ids[i] == "" || h.Position == i) {
				ids[i] = h.ItemID
				if h.Position == i {
					break
				}
			}
		}
		if ids[i] != "" {
			used[ids[i]] = true
		}
	}
	for i := range tasks {
		if ids[i] != "" {
			continue
		}
		for _, h := range had {
			if h.RemovedAt.IsZero() && !used[h.ItemID] && h.Position == i {
				ids[i] = h.ItemID
				used[h.ItemID] = true
				break
			}
		}
		if ids[i] == "" {
			next++
			ids[i] = "t" + strconv.Itoa(next)
		}
	}
	items := make([]store.ChecklistItem, len(tasks))
	for i, t := range tasks {
		items[i] = store.ChecklistItem{ItemID: ids[i], Position: i, Text: t.Text, Checked: t.Checked}
	}
	n := 0
	for i := range entities {
		if entities[i].Type == model.EntityTask && n < len(ids) {
			entities[i].ItemID = ids[n]
			n++
		}
	}
	return items
}

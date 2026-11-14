package api

import (
	"strings"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/people"
	"github.com/tikaydaes/churr/internal/store"
)

func init() {
	register(func(a *API) {
		a.handle("getMe", a.getMe)
		a.handle("listAccounts", a.listAccounts)
		a.handle("getAccount", a.getAccount)
		a.handle("listInvites", a.listInvites)
		a.handle("createInvite", a.createInvite)
		a.handle("revokeInvite", a.revokeInvite)
	})
}

func (a *API) getMe(r *Request) error {
	me, err := a.People.Me(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"account": me})
}

func (a *API) listAccounts(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	q := r.R.URL.Query()
	f := store.AccountFilter{Query: q.Get("q"), Role: model.Role(q.Get("role"))}
	if f.Role != "" && !f.Role.Valid() {
		return model.Invalid("role", "That is not a role.")
	}
	if g := q.Get("group_id"); g != "" {
		if f.GroupID, err = BodyID[model.GroupID]("group_id", g); err != nil {
			return err
		}
	}
	if ids := q.Get("ids"); ids != "" {
		parts := strings.Split(ids, ",")
		if len(parts) > 100 {
			return model.Invalid("ids", "At most 100 identifiers.")
		}
		for _, p := range parts {
			id, err := BodyID[model.AccountID]("ids", strings.TrimSpace(p))
			if err != nil {
				return err
			}
			f.IDs = append(f.IDs, id)
		}
	}
	if f.IncludeRemoved, err = r.queryBool("include_removed"); err != nil {
		return err
	}
	accounts, next, err := a.People.List(r.Ctx(), r.Actor(), f, page)
	if err != nil {
		return err
	}
	return r.JSON(list("accounts", orEmpty(accounts), next))
}

func (a *API) getAccount(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	acc, err := a.People.Get(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"account": acc})
}

func (a *API) listInvites(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	invites, next, err := a.People.ListInvites(r.Ctx(), r.Actor(), page)
	if err != nil {
		return err
	}
	return r.JSON(list("invites", orEmpty(invites), next))
}

func (a *API) createInvite(r *Request) error {
	var body struct {
		Role             model.Role `json:"role"`
		GroupIDs         []string   `json:"group_ids"`
		ConversationIDs  []string   `json:"conversation_ids"`
		MaxUses          *int       `json:"max_uses"`
		ExpiresInSeconds *int64     `json:"expires_in_seconds"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	in := people.NewInvite{Role: body.Role, MaxUses: body.MaxUses, ExpiresInSeconds: body.ExpiresInSeconds}
	for _, g := range body.GroupIDs {
		id, err := BodyID[model.GroupID]("group_ids", g)
		if err != nil {
			return err
		}
		in.GroupIDs = append(in.GroupIDs, id)
	}
	for _, c := range body.ConversationIDs {
		id, err := BodyID[model.ConversationID]("conversation_ids", c)
		if err != nil {
			return err
		}
		in.ConversationIDs = append(in.ConversationIDs, id)
	}
	inv, err := a.People.CreateInvite(r.Ctx(), r.Actor(), in)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"invite": inv})
}

func (a *API) revokeInvite(r *Request) error {
	id, err := PathID[model.InviteID](r, "id")
	if err != nil {
		return err
	}
	if err := a.People.RevokeInvite(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

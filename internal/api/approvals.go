package api

import (
	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/model"
)

// The approvals queue (docs/FEATURES.md section 31).

func init() {
	register(func(a *API) {
		a.handle("getApprovalSummary", a.getApprovalSummary)
		a.handle("listApprovals", a.listApprovals)
		a.handle("decideApproval", a.decideApproval)
		a.handle("requestChannel", a.requestChannel)
		a.handle("setMayApprove", a.setMayApprove)
	})
}

// applied answers a registration that waits for approval: the account is
// made and nobody is signed in. Those who may approve are told.
func (a *API) applied(r *Request, in auth.SignIn) error {
	if a.Approvals != nil {
		a.Approvals.Created(r.Ctx(), *in.Applied)
	}
	return model.ErrApprovalPending.With("Your request to join has been sent. You can sign in once it is approved.")
}

func (a *API) getApprovalSummary(r *Request) error {
	s, err := a.Approvals.Summarize(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"summary": s})
}

func (a *API) listApprovals(r *Request) error {
	list, err := a.Approvals.List(r.Ctx(), r.Actor(), r.R.URL.Query().Get("kind"))
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"approvals": list})
}

func (a *API) decideApproval(r *Request) error {
	var body struct {
		Approve *bool  `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.Approve == nil {
		return model.Invalid("approve", "Say whether to approve.")
	}
	out, err := a.Approvals.Decide(r.Ctx(), r.Actor(), r.R.PathValue("kind"), r.R.PathValue("id"), *body.Approve, body.Reason)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"approval": out})
}

func (a *API) requestChannel(r *Request) error {
	in, note, err := newChannelBody(r)
	if err != nil {
		return err
	}
	out, err := a.Approvals.RequestChannel(r.Ctx(), r.Actor(), in, note)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"approval": out})
}

func (a *API) setMayApprove(r *Request) error {
	id, err := PathID[model.AccountID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		MayApprove *bool `json:"may_approve"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	if body.MayApprove == nil {
		return model.Invalid("may_approve", "Say whether they may approve requests.")
	}
	if err := a.Approvals.SetApprover(r.Ctx(), r.Actor(), id, *body.MayApprove); err != nil {
		return err
	}
	return r.JSON(map[string]any{"may_approve": *body.MayApprove})
}

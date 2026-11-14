package api

import (
	"github.com/tikaydaes/churr/internal/mod"
	"github.com/tikaydaes/churr/internal/model"
)

// Moderation: time-outs, reports, the word filter, and purging.

func init() {
	register(func(a *API) {
		a.handle("listChannelTimeouts", a.listChannelTimeouts)
		a.handle("timeOutMember", a.timeOutMember)
		a.handle("liftChannelTimeout", a.liftChannelTimeout)
		a.handle("listTimeouts", a.listTimeouts)
		a.handle("createTimeout", a.createTimeout)
		a.handle("liftTimeout", a.liftTimeout)
		a.handle("listReports", a.listReports)
		a.handle("reportMessage", a.reportMessage)
		a.handle("handleReport", a.handleReport)
		a.handle("listWordFilters", a.listWordFilters)
		a.handle("createWordFilter", a.createWordFilter)
		a.handle("updateWordFilter", a.updateWordFilter)
		a.handle("deleteWordFilter", a.deleteWordFilter)
		a.handle("purgeMessage", a.purgeMessage)
		a.handle("listPurgeRequests", a.listPurgeRequests)
		a.handle("decidePurgeRequest", a.decidePurgeRequest)
	})
}

// timeoutInput is the contract's TimeoutInput.
type timeoutInput struct {
	AccountID string `json:"account_id"`
	Seconds   int64  `json:"seconds"`
	Reason    string `json:"reason"`
}

func (a *API) listChannelTimeouts(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	list, err := a.Mod.ChannelTimeouts(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"timeouts": orEmpty(list)})
}

func (a *API) timeOutMember(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	var body timeoutInput
	if err := r.Decode(&body); err != nil {
		return err
	}
	account, err := BodyID[model.AccountID]("account_id", body.AccountID)
	if err != nil {
		return err
	}
	t, err := a.Mod.TimeOut(r.Ctx(), r.Actor(), id, account, body.Seconds, body.Reason)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"timeout": t})
}

func (a *API) liftChannelTimeout(r *Request) error {
	id, err := PathID[model.ConversationID](r, "id")
	if err != nil {
		return err
	}
	timeout, err := PathID[string](r, "timeout_id")
	if err != nil {
		return err
	}
	if err := a.Mod.LiftChannelTimeout(r.Ctx(), r.Actor(), id, timeout); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listTimeouts(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	list, next, err := a.Mod.AdminTimeouts(r.Ctx(), r.Actor(), page)
	if err != nil {
		return err
	}
	return r.JSON(list2("timeouts", orEmpty(list), next))
}

// list2 is list under a name that does not collide with a local called
// list.
func list2(name string, items any, next string) map[string]any { return list(name, items, next) }

func (a *API) createTimeout(r *Request) error {
	var body timeoutInput
	if err := r.Decode(&body); err != nil {
		return err
	}
	account, err := BodyID[model.AccountID]("account_id", body.AccountID)
	if err != nil {
		return err
	}
	t, err := a.Mod.TimeOutEverywhere(r.Ctx(), r.Actor(), account, body.Seconds, body.Reason)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"timeout": t})
}

func (a *API) liftTimeout(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.Mod.AdminLiftTimeout(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) listReports(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	var conv model.ConversationID
	if v := r.R.URL.Query().Get("conversation_id"); v != "" {
		if conv, err = BodyID[model.ConversationID]("conversation_id", v); err != nil {
			return err
		}
	}
	waiting, err := r.queryBool("waiting")
	if err != nil {
		return err
	}
	reports, next, err := a.Mod.Reports(r.Ctx(), r.Actor(), conv, waiting, page)
	if err != nil {
		return err
	}
	return r.JSON(list("reports", orEmpty(reports), next))
}

func (a *API) reportMessage(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Reason string `json:"reason"`
	}
	// The body is optional: a report needs no reason.
	if r.R.ContentLength != 0 {
		if err := r.Decode(&body); err != nil {
			return err
		}
	}
	report, err := a.Mod.Report(r.Ctx(), r.Actor(), id, body.Reason)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"report_id": report.ID})
}

func (a *API) handleReport(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Outcome        string `json:"outcome"`
		TimeoutSeconds int64  `json:"timeout_seconds"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	report, err := a.Mod.HandleReport(r.Ctx(), r.Actor(), id, body.Outcome, body.TimeoutSeconds)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"report": report})
}

func (a *API) listWordFilters(r *Request) error {
	filters, err := a.Mod.Filters(r.Ctx(), r.Actor())
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"word_filters": orEmpty(filters)})
}

func (a *API) createWordFilter(r *Request) error {
	var body struct {
		Pattern string `json:"pattern"`
		IsRegex bool   `json:"is_regex"`
		Action  string `json:"action"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	f, err := a.Mod.CreateFilter(r.Ctx(), r.Actor(), model.WordFilter{Pattern: body.Pattern, IsRegex: body.IsRegex, Action: body.Action})
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"word_filter": f})
}

func (a *API) updateWordFilter(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Pattern *string `json:"pattern"`
		IsRegex *bool   `json:"is_regex"`
		Action  *string `json:"action"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	f, err := a.Mod.UpdateFilter(r.Ctx(), r.Actor(), id, mod.FilterChange{Pattern: body.Pattern, IsRegex: body.IsRegex, Action: body.Action})
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"word_filter": f})
}

func (a *API) deleteWordFilter(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	if err := a.Mod.DeleteFilter(r.Ctx(), r.Actor(), id); err != nil {
		return err
	}
	return r.NoContent()
}

func (a *API) purgeMessage(r *Request) error {
	id, err := PathID[model.MessageID](r, "id")
	if err != nil {
		return err
	}
	state, m, err := a.Chat.Purge(r.Ctx(), r.Actor(), id)
	if err != nil {
		return err
	}
	// Only that it was asked for or done: the outcome of a request is
	// never returned to anyone but an admin.
	return r.JSON(map[string]any{"state": state, "message": m})
}

func (a *API) listPurgeRequests(r *Request) error {
	page, err := r.Page()
	if err != nil {
		return err
	}
	waiting, err := r.queryBool("waiting")
	if err != nil {
		return err
	}
	requests, next, err := a.Chat.PurgeRequests(r.Ctx(), r.Actor(), waiting, page)
	if err != nil {
		return err
	}
	return r.JSON(list("purge_requests", orEmpty(requests), next))
}

func (a *API) decidePurgeRequest(r *Request) error {
	id, err := PathID[string](r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := r.Decode(&body); err != nil {
		return err
	}
	req, err := a.Chat.DecidePurgeRequest(r.Ctx(), r.Actor(), id, body.Outcome)
	if err != nil {
		return err
	}
	return r.JSON(map[string]any{"purge_request": req})
}

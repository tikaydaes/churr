package api_test

import (
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The approvals queue (docs/FEATURES.md section 31): registrations that
// wait, approved by someone given the flag who is no admin; channels that
// wait; who sees the queue; every decision audited and told.
func TestApprovals(t *testing.T) {
	ts := testkit.NewServer(t)
	owner := ts.Owner
	ann, bob := ts.Member("ann"), ts.Member("bob")
	settings := "/api/v1/admin/settings"
	owner.Must(200, "PATCH", settings, map[string]any{"registration.mode": "open", "registration.approval": true})

	// An applicant makes their account and waits; signing in says so.
	gil := ts.Browser()
	if status, out := gil.Apply("gil", "I run the bakery downstairs"); status != 403 || obj(out, "error")["code"] != "approval_pending" {
		t.Fatalf("applying: %d %v", status, out)
	}
	if status, out := gil.Login(); status != 403 || obj(out, "error")["code"] != "approval_pending" {
		t.Fatalf("signing in while waiting: %d %v", status, out)
	}
	// An invite still goes straight in.
	ts.Member("ivy")

	// A member sees no queue.
	bob.Refused(403, model.CodeForbidden, "GET", "/api/v1/approvals", nil)
	if s := obj(bob.Must(200, "GET", "/api/v1/approvals/summary", nil), "summary"); s["may_approve"] != false || len(arr(s, "kinds")) != 0 {
		t.Fatalf("a member's summary: %v", s)
	}
	// Given the flag, ann approves registrations and channels, and nothing
	// else: not webhooks, not the admin console.
	bob.Refused(403, model.CodeForbidden, "PUT", "/api/v1/admin/accounts/"+string(ann.ID)+"/may-approve", map[string]any{"may_approve": true})
	annStream, _ := ann.Events(nil)
	owner.Must(200, "PUT", "/api/v1/admin/accounts/"+string(ann.ID)+"/may-approve", map[string]any{"may_approve": true})
	if e := annStream.Expect("approvals.changed"); e["may_approve"] != true {
		t.Fatalf("approvals.changed: %v", e)
	}
	if !auditedFor(t, ts, "approver.granted", string(ann.ID)) {
		t.Error("giving the flag is in the audit log")
	}
	if a := obj(owner.Must(200, "GET", "/api/v1/admin/accounts/"+string(ann.ID), nil), "account"); a["may_approve"] != true {
		t.Fatalf("the console shows the flag: %v", a)
	}
	s := obj(ann.Must(200, "GET", "/api/v1/approvals/summary", nil), "summary")
	if s["may_approve"] != true || strings.Join(strs(arr(s, "kinds")), ",") != "registration,channel" || obj(s, "counts")["registration"] != float64(1) {
		t.Fatalf("the approver's summary: %v", s)
	}
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/approvals?kind=webhook", nil)
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/admin/accounts", nil)
	if s := obj(owner.Must(200, "GET", "/api/v1/approvals/summary", nil), "summary"); len(arr(s, "kinds")) != 3 {
		t.Fatalf("an owner decides every kind: %v", s)
	}

	// A new application reaches the approver live.
	hal := ts.Browser()
	hal.Apply("hal", "")
	if e := annStream.Expect("approval.created"); obj(e, "approval")["kind"] != "registration" {
		t.Fatalf("approval.created: %v", e)
	}
	list := arr(ann.Must(200, "GET", "/api/v1/approvals?kind=registration", nil), "approvals")
	if len(list) != 2 {
		t.Fatalf("two waiting: %v", list)
	}
	first := list[0].(map[string]any)
	if first["note"] != "I run the bakery downstairs" || obj(first, "requester")["username"] != "gil" {
		t.Fatalf("the oldest first, with its note and requester: %v", first)
	}
	gilID, halID := first["id"].(string), list[1].(map[string]any)["id"].(string)

	// Declined: told why when next signing in.
	ann.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/approvals/registration/"+halID, map[string]any{})
	ann.Must(200, "POST", "/api/v1/approvals/registration/"+halID, map[string]any{"approve": false, "reason": "We only take neighbours."})
	if status, out := hal.Login(); status != 403 || !strings.Contains(obj(out, "error")["message"].(string), "We only take neighbours.") {
		t.Fatalf("a declined applicant: %d %v", status, out)
	}
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/approvals/registration/"+halID, map[string]any{"approve": true})
	// Approved: gil signs in as a member.
	out := obj(ann.Must(200, "POST", "/api/v1/approvals/registration/"+gilID, map[string]any{"approve": true}), "approval")
	if out["state"] != "approved" || out["decided_by_id"] != string(ann.ID) {
		t.Fatalf("approved: %v", out)
	}
	status, in := gil.Login()
	if status != 200 {
		t.Fatalf("an approved applicant signs in: %d %v", status, in)
	}
	gil.SignedIn(in)
	if me := obj(gil.Must(200, "GET", "/api/v1/me", nil), "account"); me["role"] != "member" {
		t.Fatalf("the approved applicant: %v", me)
	}
	if !auditedFor(t, ts, "approval.approved", string(gil.ID)) || !auditedFor(t, ts, "approval.declined", "") {
		t.Error("decisions are in the audit log with who decided")
	}

	// Channels that wait.
	owner.Must(200, "PATCH", settings, map[string]any{"channels.creation_approval": true})
	body := map[string]any{"name": "bakery", "private": false}
	bob.Refused(403, model.CodeApprovalRequired, "POST", "/api/v1/channels", body)
	bob.Refused(400, model.CodeInvalidValue, "POST", "/api/v1/approvals/channels", map[string]any{"name": "", "private": false})
	ask := map[string]any{"name": "bakery", "private": false, "note": "for the bread rota"}
	req := obj(bob.Must(201, "POST", "/api/v1/approvals/channels", ask), "approval")
	if req["summary"] != "bakery" || req["state"] != "waiting" {
		t.Fatalf("the request: %v", req)
	}
	if e := annStream.Expect("approval.created"); obj(e, "approval")["summary"] != "bakery" {
		t.Fatalf("the approver is told: %v", e)
	}
	// Those who may approve make channels themselves.
	ann.Must(201, "POST", "/api/v1/channels", map[string]any{"name": "ann's", "private": false})
	owner.Must(201, "POST", "/api/v1/channels", map[string]any{"name": "owner's", "private": false})
	ann.Refused(409, model.CodeConflict, "POST", "/api/v1/approvals/channels", ask)
	bobStream, _ := bob.Events(nil)
	ann.Must(200, "POST", "/api/v1/approvals/channel/"+req["id"].(string), map[string]any{"approve": true})
	e := bobStream.Expect("approval.decided")
	if a := obj(e, "approval"); a["state"] != "approved" || a["decided_by_id"] != nil {
		t.Fatalf("the requester is told, not by whom: %v", e)
	}
	var made map[string]any
	for _, c := range arr(bob.Must(200, "GET", "/api/v1/conversations", nil), "conversations") {
		if c.(map[string]any)["name"] == "bakery" {
			made = c.(map[string]any)
		}
	}
	if made == nil || obj(made, "membership")["level"] != "manager" {
		t.Fatalf("the channel is made with its requester its manager: %v", made)
	}
	// Declined, with a reason.
	req = obj(bob.Must(201, "POST", "/api/v1/approvals/channels", map[string]any{"name": "secret", "private": true}), "approval")
	owner.Must(200, "POST", "/api/v1/approvals/channel/"+req["id"].(string), map[string]any{"approve": false, "reason": "Use #bakery."})
	if a := obj(bobStream.Expect("approval.decided"), "approval"); a["state"] != "declined" || a["reason"] != "Use #bakery." {
		t.Fatalf("declined: %v", a)
	}

	// The flag taken away, the queue is gone.
	owner.Must(200, "PUT", "/api/v1/admin/accounts/"+string(ann.ID)+"/may-approve", map[string]any{"may_approve": false})
	ann.Refused(403, model.CodeForbidden, "GET", "/api/v1/approvals", nil)
	ann.Refused(403, model.CodeApprovalRequired, "POST", "/api/v1/channels", map[string]any{"name": "again", "private": false})
	// Never to a guest.
	guest := ts.Guest("gus", made["id"].(string))
	owner.Refused(409, model.CodeConflict, "PUT", "/api/v1/admin/accounts/"+string(guest.ID)+"/may-approve", map[string]any{"may_approve": true})
}

func strs(v []any) []string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i], _ = x.(string)
	}
	return out
}

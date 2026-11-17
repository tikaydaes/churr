package api_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// reportsOf lists the reports c may handle, by id.
func reportsOf(c *testkit.Client, query string) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, r := range arr(c.Must(200, "GET", "/api/v1/reports"+query, nil), "reports") {
		m := r.(map[string]any)
		out[m["id"].(string)] = m
	}
	return out
}

// TestReportsInChatsReachAdmins holds review findings H1 and H5: a report
// made in a direct or group chat goes to the instance's admins, never to the
// person reported, and keeps a snapshot of the message that outlives its
// deletion, its purge and the chat. The admins are shown the snapshot and
// nothing else of the chat.
func TestReportsInChatsReachAdmins(t *testing.T) {
	ts := requestServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	autoApprove(bob)
	direct := ask(ann, 201, bob)["id"].(string)
	annS, _ := ann.Events(nil)
	adminS, _ := admin.Events(nil)

	file := ann.Upload(direct, "proof.txt", []byte("a file\n"))
	sent := obj(ann.Must(201, "POST", conv+direct+"/messages", map[string]any{"text": "you will regret this", "file_ids": []any{file["id"]}}), "message")
	id := bob.Must(201, "POST", msgs+sent["id"].(string)+"/report", map[string]any{"reason": "a threat"})["report_id"].(string)
	if e := adminS.Expect("report.created"); obj(e, "report")["id"] != id || obj(e, "report")["to_admins"] != true {
		t.Fatalf("report.created to an admin: %v", e)
	}
	// The reported person is never told, nor handles it.
	quiet(t, annS, 200*time.Millisecond, func(f map[string]any) bool { return strings.HasPrefix(fmt.Sprint(f["type"]), "report.") })
	if got := reportsOf(ann, ""); len(got) != 0 {
		t.Fatalf("the reported person sees the report: %v", got)
	}
	ann.Refused(404, model.CodeNotFound, "POST", "/api/v1/reports/"+id+"/handle", map[string]any{"outcome": "dismissed"})

	// The admin sees the snapshot and not the chat.
	r := reportsOf(admin, "")[id]
	snap := obj(r, "snapshot")
	if r == nil || r["message"] != nil || snap["text"] != "you will regret this" || snap["author_id"] != string(ann.ID) ||
		snap["place"] != "chat" || arr(snap, "file_names")[0] != "proof.txt" || snap["created_at"] == nil || r["account_id"] != string(ann.ID) {
		t.Fatalf("what the admin is shown: %v", r)
	}
	admin.Refused(404, model.CodeNotFound, "GET", conv+direct+"/messages", nil)
	admin.Refused(404, model.CodeNotFound, "GET", msgs+sent["id"].(string), nil)

	// An admin cannot delete in a chat they cannot read.
	admin.Refused(403, model.CodeForbidden, "POST", "/api/v1/reports/"+id+"/handle", map[string]any{"outcome": "deleted"})
	// The author deleting the message does not take the evidence away.
	ann.Must(200, "DELETE", msgs+sent["id"].(string), nil)
	if snap := obj(reportsOf(admin, "")[id], "snapshot"); snap["text"] != "you will regret this" {
		t.Fatalf("the snapshot after a delete: %v", snap)
	}

	// Both leave: the chat waits for its report, then goes.
	cat := ts.Member("cat")
	group := mkchat(ann, bob, cat)
	m := ann.Post(group, "in a group")["id"].(string)
	gid := bob.Must(201, "POST", msgs+m+"/report", nil)["report_id"].(string)
	for _, c := range []*testkit.Client{ann, bob, cat} {
		c.Must(204, "POST", conv+group+"/leave", nil)
	}
	if _, err := ts.Server.Store.Conversations().Get(context.Background(), model.ConversationID(group)); err != nil {
		t.Fatalf("the chat went while its report waited: %v", err)
	}
	done := obj(admin.Must(200, "POST", "/api/v1/reports/"+gid+"/handle", map[string]any{"outcome": "timed_out", "timeout_seconds": 600}), "report")
	if done["outcome"] != "timed_out" {
		t.Fatalf("handled: %v", done)
	}
	if _, err := ts.Server.Store.Conversations().Get(context.Background(), model.ConversationID(group)); err == nil {
		t.Fatal("the chat outlived its last report")
	}
	// The time-out is on the whole instance.
	ann.Refused(403, model.CodeTimedOut, "POST", conv+direct+"/messages", map[string]any{"text": "x"})
	if snap := obj(reportsOf(admin, "?waiting=false")[gid], "snapshot"); snap["text"] != "in a group" {
		t.Fatalf("the report outlived the chat: %v", snap)
	}
}

// TestReportsInPrivateChannels holds review finding H5: a report in a
// private channel goes to its moderators and managers; when it is about one
// of them, or the channel has none, to the instance's admins too, who see
// the snapshot and not the channel. A moderator or manager who joined later
// is not shown reports of messages from before their first join (review
// finding H4).
func TestReportsInPrivateChannels(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	admin := ts.Admin("adm")
	priv := ann.Channel("priv", true, bob, cat)
	ann.Must(200, "PATCH", conv+priv+"/members/"+string(cat.ID), map[string]any{"level": "moderator"})

	// About a member: the moderators and managers only.
	plain := bob.Post(priv, "rude")["id"].(string)
	r1 := cat.Must(201, "POST", msgs+plain+"/report", nil)["report_id"].(string)
	if got := reportsOf(ann, ""); got[r1] == nil || got[r1]["to_admins"] != false || got[r1]["message"] == nil {
		t.Fatalf("the manager's list: %v", got)
	}
	if reportsOf(admin, "")[r1] != nil {
		t.Fatal("an admin was shown a private channel's report that its moderators can handle")
	}
	// About its manager: the admins too, and never the manager.
	own := ann.Post(priv, "the manager is rude")["id"].(string)
	r2 := bob.Must(201, "POST", msgs+own+"/report", nil)["report_id"].(string)
	if reportsOf(ann, "")[r2] != nil {
		t.Fatal("the manager sees the report about themselves")
	}
	if got := reportsOf(cat, "")[r2]; got == nil || got["to_admins"] != true {
		t.Fatalf("the other moderator: %v", got)
	}
	got := reportsOf(admin, "")[r2]
	if got == nil || got["message"] != nil || obj(got, "snapshot")["text"] != "the manager is rude" || obj(got, "snapshot")["place"] != "private" {
		t.Fatalf("the admin is shown the snapshot only: %v", got)
	}
	admin.Refused(404, model.CodeNotFound, "GET", conv+priv+"/messages", nil)
	admin.Must(200, "POST", "/api/v1/reports/"+r2+"/handle", map[string]any{"outcome": "dismissed"})

	// A manager who joins later is not shown what is from before.
	time.Sleep(3 * time.Millisecond)
	dan := ts.Member("dan")
	ann.Must(200, "POST", conv+priv+"/members", map[string]any{"account_ids": []string{string(dan.ID)}, "level": "manager"})
	if got := reportsOf(dan, ""); got[r1] != nil {
		t.Fatalf("a newcomer manager is shown a report of a message from before their first join: %v", got[r1])
	}
	dan.Refused(404, model.CodeNotFound, "POST", "/api/v1/reports/"+r1+"/handle", map[string]any{"outcome": "dismissed"})
	after := bob.Post(priv, "rude again")["id"].(string)
	r3 := cat.Must(201, "POST", msgs+after+"/report", nil)["report_id"].(string)
	if reportsOf(dan, "")[r3] == nil {
		t.Fatal("the newcomer manager is not shown a report from after their join")
	}

}

// TestReportAPerson holds review finding H5: a person may be reported, for
// their name, status or picture, to the instance's admins.
func TestReportAPerson(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	admin := ts.Admin("adm")
	ann.Must(200, "PATCH", "/api/v1/me", map[string]any{"display_name": "A Rude Name"})
	id := bob.Must(201, "POST", "/api/v1/accounts/"+string(ann.ID)+"/report", map[string]any{"reason": "the name"})["report_id"].(string)
	bob.Refused(409, model.CodeAlreadyExists, "POST", "/api/v1/accounts/"+string(ann.ID)+"/report", nil)
	bob.Refused(403, model.CodeForbidden, "POST", "/api/v1/accounts/"+string(bob.ID)+"/report", nil)
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/accounts/"+string(model.NewID[model.AccountID]())+"/report", nil)
	r := reportsOf(admin, "")[id]
	if r == nil || r["kind"] != "account" || r["account_id"] != string(ann.ID) || obj(r, "snapshot")["display_name"] != "A Rude Name" || r["to_admins"] != true {
		t.Fatalf("a report of a person: %v", r)
	}
	if len(reportsOf(ann, "")) != 0 || len(reportsOf(bob, "")) != 0 {
		t.Fatal("a report of a person reaches someone other than the admins")
	}
	ann.Must(200, "PATCH", "/api/v1/me", map[string]any{"display_name": "Ann"})
	if obj(reportsOf(admin, "")[id], "snapshot")["display_name"] != "A Rude Name" {
		t.Fatal("the snapshot followed the profile")
	}
	admin.Refused(409, model.CodeConflict, "POST", "/api/v1/reports/"+id+"/handle", map[string]any{"outcome": "deleted"})
	done := obj(admin.Must(200, "POST", "/api/v1/reports/"+id+"/handle", map[string]any{"outcome": "dismissed"}), "report")
	if done["outcome"] != "dismissed" {
		t.Fatalf("handled: %v", done)
	}
}

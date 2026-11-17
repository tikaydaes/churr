package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/chat"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
	"github.com/tikaydaes/churr/internal/testkit"
)

// The helpers of the forms, scheduled and commands tests start with `f`.

var fctx = context.Background()

func fMsgs(id string) string { return "/api/v1/conversations/" + id + "/messages" }
func fForm(id string) string { return "/api/v1/messages/" + id + "/form" }

// fPoll is a poll with two options and the settings given.
func fPoll(settings map[string]any) map[string]any {
	form := map[string]any{
		"preset": "poll", "title": "Lunch?", "action": map[string]any{"type": "tally"},
		"fields": []any{map[string]any{"id": "choice", "type": "choice", "label": "Where", "required": true,
			"options": []any{map[string]any{"id": "o1", "label": "Noodles"}, map[string]any{"id": "o2", "label": "Tacos"}}}},
	}
	for k, v := range settings {
		form[k] = v
	}
	return form
}

// fPost posts a message carrying a form and returns the message.
func fPost(c *testkit.Client, conv string, form map[string]any) map[string]any {
	return obj(c.Must(201, "POST", fMsgs(conv), map[string]any{"form": form}), "message")
}

func fID(m map[string]any) string { return m["id"].(string) }

func fPick(option ...string) map[string]any {
	return map[string]any{"answers": map[string]any{"choice": option}}
}

// fAdd puts an account into a conversation at a level, straight in the
// store: the tests here are not about how people get into channels.
func fAdd(t *testing.T, ts *testkit.TestServer, conv string, c *testkit.Client, level model.Level) {
	t.Helper()
	if _, err := ts.Server.Store.Members().AddDirect(fctx, model.ConversationID(conv), c.ID, level, model.Now()); err != nil {
		t.Fatal(err)
	}
}

// fGroup makes a group with these members.
func fGroup(t *testing.T, ts *testkit.TestServer, handle string, open bool, members ...*testkit.Client) string {
	t.Helper()
	g := model.Group{Handle: handle, Name: handle, Open: open}
	if err := ts.Server.Store.Groups().Create(fctx, &g); err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if _, err := ts.Server.Store.Groups().AddMember(fctx, g.ID, m.ID, "", model.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return string(g.ID)
}

func fTally(form map[string]any, field, option string) any {
	return obj(form, "tally", field)[option]
}

// TestPolls covers posting a poll, answering, changing and withdrawing an
// answer, who may answer, the events, and closing.
func TestPolls(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	ops := ann.Channel("ops", false, bob)
	fAdd(t, ts, ops, dan, model.LevelReader)

	// The checks on a form that arrives with a message.
	for name, c := range map[string]struct {
		form map[string]any
		code model.Code
	}{
		"no fields and no buttons": {map[string]any{"fields": []any{}, "action": map[string]any{"type": "collect"}}, model.CodeInvalidRequest},
		"results on a collect form": {map[string]any{"results": "live", "action": map[string]any{"type": "collect"},
			"fields": []any{map[string]any{"id": "a", "type": "text", "label": "A"}}}, model.CodeInvalidRequest},
		"an unknown field type": {map[string]any{"action": map[string]any{"type": "collect"},
			"fields": []any{map[string]any{"id": "a", "type": "slider", "label": "A"}}}, model.CodeInvalidValue},
		"a closing time in the past":  {fPoll(map[string]any{"closes_at": "2020-01-01T00:00:00.000Z"}), model.CodeInvalidValue},
		"a group that does not exist": {fPoll(map[string]any{"answerer_group_id": "0193a0d1-0000-7000-8000-000000000001"}), model.CodeInvalidValue},
	} {
		if e := ann.Refused(400, c.code, "POST", fMsgs(ops), map[string]any{"form": c.form}); e["message"] == "" {
			t.Errorf("%s: no sentence in the refusal", name)
		}
	}
	dan.Refused(403, model.CodeForbidden, "POST", fMsgs(ops), map[string]any{"form": fPoll(nil)})    // a reader cannot post one
	bob.Refused(403, model.CodeForbidden, "POST", fMsgs(ops), map[string]any{"form": map[string]any{ // case 63
		"action": map[string]any{"type": "bot"}, "buttons": []any{map[string]any{"id": "go", "label": "Go"}}}})

	annStream, _ := ann.Events(nil)
	bobStream, _ := bob.Events(nil)
	msg := fPost(ann, ops, fPoll(nil))
	form := obj(msg, "form")
	if form["answer_count"] != 0.0 || form["results"] != "live" || form["one_answer"] != true || fTally(form, "choice", "o1") != 0.0 {
		t.Fatalf("a new poll: %v", form)
	}
	if msg["fallback"] != "Lunch?\nWhere\nNoodles\nTacos" {
		t.Fatalf("fallback: %q", msg["fallback"])
	}
	if created := annStream.Expect("message.created"); obj(created, "message", "form")["id"] != form["id"] {
		t.Fatalf("message.created should carry the form: %v", created)
	}
	id := fID(msg)

	// Answering: the answerer gets my_answer; everyone gets the tally.
	out := obj(bob.Must(200, "PUT", fForm(id)+"/answer", fPick("o1")), "form")
	if fTally(out, "choice", "o1") != 1.0 || out["answer_count"] != 1.0 || len(arr(obj(out, "my_answer"), "choice")) != 1 {
		t.Fatalf("after answering: %v", out)
	}
	if v := arr(obj(out, "voters", "choice"), "o1"); len(v) != 1 || v[0] != string(bob.ID) {
		t.Fatalf("voters: %v", out["voters"])
	}
	e := annStream.Expect("form.answered")
	if e["account_id"] != string(bob.ID) || e["message_id"] != id || fTally(obj(e, "form"), "choice", "o1") != 1.0 {
		t.Fatalf("form.answered for another member: %v", e)
	}
	if _, has := obj(e, "form")["my_answer"]; has {
		t.Fatalf("my_answer was sent to someone else: %v", e)
	}
	if e := bobStream.Expect("form.answered"); len(arr(obj(e, "form", "my_answer"), "choice")) != 1 {
		t.Fatalf("form.answered for the answerer should carry my_answer: %v", e)
	}

	// Answering again replaces the answer.
	out = obj(bob.Must(200, "PUT", fForm(id)+"/answer", fPick("o2")), "form")
	if fTally(out, "choice", "o1") != 0.0 || fTally(out, "choice", "o2") != 1.0 || out["answer_count"] != 1.0 {
		t.Fatalf("after changing the answer: %v", out)
	}

	// An answer must fit the form.
	if e := bob.Refused(400, model.CodeInvalidValue, "PUT", fForm(id)+"/answer", fPick("o9")); e["field"] != "choice" {
		t.Fatalf("the refusal should name the field: %v", e)
	}
	bob.Refused(400, model.CodeInvalidValue, "PUT", fForm(id)+"/answer", fPick("o1", "o2"))
	bob.Refused(400, model.CodeInvalidValue, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{}})
	bob.Refused(400, model.CodeInvalidValue, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"other": "x"}})
	bob.Refused(400, model.CodeInvalidRequest, "PUT", fForm(id)+"/answer", map[string]any{})
	bob.Refused(404, model.CodeNotFound, "POST", fForm(id)+"/buttons/nope", map[string]any{})

	// Who may answer: any member, readers included; someone reading a
	// public channel without having joined joins first.
	dan.Must(200, "PUT", fForm(id)+"/answer", fPick("o1"))                         // case 52
	cat.Refused(403, model.CodeForbidden, "PUT", fForm(id)+"/answer", fPick("o1")) // case 205
	cat.Refused(403, model.CodeForbidden, "POST", fForm(id)+"/options", map[string]any{"field_id": "choice", "label": "Soup"})
	ts.Owner.Refused(403, model.CodeForbidden, "PUT", fForm(id)+"/answer", fPick("o1")) // case 207
	cat.Must(200, "POST", "/api/v1/conversations/"+ops+"/join", nil)
	cat.Must(200, "PUT", fForm(id)+"/answer", fPick("o1")) // case 206

	// Every read fills the form in for the reader.
	got := obj(ann.Must(200, "GET", "/api/v1/messages/"+id, nil), "message", "form")
	if got["answer_count"] != 3.0 || fTally(got, "choice", "o1") != 2.0 || len(arr(obj(got, "voters", "choice"), "o1")) != 2 {
		t.Fatalf("the form on a read: %v", got)
	}
	if _, has := got["my_answer"]; has {
		t.Fatalf("someone who has not answered has no my_answer: %v", got)
	}
	listed := arr(bob.Must(200, "GET", fMsgs(ops), nil), "messages")
	if mine := obj(listed[len(listed)-1].(map[string]any), "form", "my_answer"); len(arr(mine, "choice")) != 1 {
		t.Fatalf("the form in a list of messages: %v", listed[len(listed)-1])
	}
	rows := arr(ann.Must(200, "GET", fForm(id)+"/results", nil), "answers")
	if len(rows) != 3 || rows[0].(map[string]any)["account_id"] == nil {
		t.Fatalf("a named poll's answers carry the account: %v", rows)
	}

	// Withdrawing.
	out = obj(bob.Must(200, "DELETE", fForm(id)+"/answer", nil), "form")
	if out["answer_count"] != 2.0 || out["my_answer"] != nil || fTally(out, "choice", "o2") != 0.0 {
		t.Fatalf("after withdrawing: %v", out)
	}
	if e := annStream.Expect("form.answer_withdrawn"); e["account_id"] != string(bob.ID) || obj(e, "form")["answer_count"] != 2.0 {
		t.Fatalf("form.answer_withdrawn: %v", e)
	}
	bob.Refused(404, model.CodeNotFound, "DELETE", fForm(id)+"/answer", nil)

	// This poll does not take added options.
	bob.Refused(403, model.CodeForbidden, "POST", fForm(id)+"/options", map[string]any{"field_id": "choice", "label": "Soup"})

	// Once anyone has answered, an edit cannot change the form.
	ann.Refused(409, model.CodeConflict, "PATCH", "/api/v1/messages/"+id, map[string]any{"form": fPoll(map[string]any{"title": "Dinner?"})})
	ann.Must(200, "PATCH", "/api/v1/messages/"+id, map[string]any{"text": "Vote by noon"})

	// Closing: the author or a manager, once.
	bob.Refused(403, model.CodeForbidden, "POST", fForm(id)+"/close", nil) // case 72
	closed := obj(ann.Must(200, "POST", fForm(id)+"/close", nil), "form")
	if closed["closed_at"] == nil {
		t.Fatalf("a closed form has closed_at: %v", closed)
	}
	if e := bobStream.Expect("form.closed"); e["closed_by_id"] != string(ann.ID) || obj(e, "form")["closed_at"] == nil {
		t.Fatalf("form.closed: %v", e)
	}
	ann.Refused(409, model.CodeConflict, "POST", fForm(id)+"/close", nil)
	bob.Refused(409, model.CodeConflict, "PUT", fForm(id)+"/answer", fPick("o1"))
	bob.Refused(409, model.CodeConflict, "DELETE", fForm(id)+"/answer", nil)

	// Before anyone answers, an edit may replace the form; never whether
	// it is anonymous.
	fresh := fID(fPost(ann, ops, fPoll(nil)))
	edited := obj(ann.Must(200, "PATCH", "/api/v1/messages/"+fresh, map[string]any{"form": fPoll(map[string]any{"title": "Dinner?"})}), "message")
	if obj(edited, "form")["title"] != "Dinner?" || !strings.HasPrefix(edited["fallback"].(string), "Dinner?") {
		t.Fatalf("an edited form: %v", edited)
	}
	ann.Refused(400, model.CodeInvalidValue, "PATCH", "/api/v1/messages/"+fresh, map[string]any{"form": fPoll(map[string]any{"anonymous": true})})

	// A message without a form has none to answer, and a stranger to a
	// private channel learns nothing.
	plain := fID(ann.Post(ops, "no form here"))
	bob.Refused(404, model.CodeNotFound, "PUT", fForm(plain)+"/answer", fPick("o1"))
	priv := ann.Channel("quiet", true)
	hidden := fID(fPost(ann, priv, fPoll(nil)))
	for _, path := range []string{"/answer", "/close", "/results"} {
		method := map[string]string{"/answer": "PUT", "/close": "POST", "/results": "GET"}[path]
		var body any
		if method == "PUT" {
			body = fPick("o1")
		}
		bob.Refused(404, model.CodeNotFound, method, fForm(hidden)+path, body)
	}

	// A form that closes itself: the scheduler closes it and says so.
	soon := fPost(ann, ops, fPoll(map[string]any{"closes_at": time.Now().Add(700 * time.Millisecond).UTC().Format(time.RFC3339Nano)}))
	ts.Server.Sched.Wake()
	e = bobStream.Expect("form.closed")
	for e["message_id"] != fID(soon) {
		e = bobStream.Expect("form.closed")
	}
	if _, has := e["closed_by_id"]; has {
		t.Fatalf("a form that closed itself names nobody: %v", e)
	}
	bob.Refused(409, model.CodeConflict, "PUT", fForm(fID(soon))+"/answer", fPick("o1"))

	// Archived: nobody answers (case 125).
	open := fID(fPost(ann, ops, fPoll(nil)))
	if _, err := ts.Server.Store.Conversations().Update(fctx, model.ConversationID(ops), func(c *model.Conversation) error {
		c.ArchivedAt = model.Now()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	bob.Refused(403, model.CodeArchived, "PUT", fForm(open)+"/answer", fPick("o1"))
}

// TestPollResults covers the three `results` settings and sign-up places.
func TestPollResults(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	ops := ann.Channel("ops", false, bob, cat)
	annStream, _ := ann.Events(nil)
	bobStream, _ := bob.Events(nil)

	// after_answer: the result is for those who have answered.
	id := fID(fPost(ann, ops, fPoll(map[string]any{"results": "after_answer"})))
	out := obj(bob.Must(200, "PUT", fForm(id)+"/answer", fPick("o1")), "form")
	if fTally(out, "choice", "o1") != 1.0 || out["voters"] == nil {
		t.Fatalf("the answerer sees the result: %v", out)
	}
	e := annStream.Expect("form.answered")
	if f := obj(e, "form"); f["tally"] != nil || f["voters"] != nil || e["account_id"] != nil || f["answer_count"] != 1.0 {
		t.Fatalf("someone who has not answered gets the count only: %v", e)
	}
	if e := bobStream.Expect("form.answered"); fTally(obj(e, "form"), "choice", "o1") != 1.0 {
		t.Fatalf("the answerer's event carries the result: %v", e)
	}
	if f := obj(ann.Must(200, "GET", "/api/v1/messages/"+id, nil), "message", "form"); f["tally"] != nil || f["voters"] != nil {
		t.Fatalf("a read by someone who has not answered: %v", f)
	}
	ann.Refused(403, model.CodeForbidden, "GET", fForm(id)+"/results", nil)
	ann.Refused(403, model.CodeForbidden, "GET", fForm(id)+"/results.csv", nil)
	bob.Must(200, "GET", fForm(id)+"/results", nil)
	// Another answer reaches bob, who has answered, with the result.
	cat.Must(200, "PUT", fForm(id)+"/answer", fPick("o1"))
	if e := bobStream.Expect("form.answered"); fTally(obj(e, "form"), "choice", "o1") != 2.0 || obj(e, "form")["my_answer"] == nil {
		t.Fatalf("a later answer, as an earlier answerer sees it: %v", e)
	}

	// after_close: nobody until it closes, then everybody.
	id = fID(fPost(ann, ops, fPoll(map[string]any{"results": "after_close"})))
	out = obj(bob.Must(200, "PUT", fForm(id)+"/answer", fPick("o2")), "form")
	if out["tally"] != nil || out["voters"] != nil || out["my_answer"] == nil {
		t.Fatalf("before it closes even the answerer sees no result: %v", out)
	}
	bob.Refused(403, model.CodeForbidden, "GET", fForm(id)+"/results", nil)
	ann.Must(200, "POST", fForm(id)+"/close", nil)
	if e := bobStream.Expect("form.closed"); fTally(obj(e, "form"), "choice", "o2") != 1.0 || obj(e, "form")["voters"] == nil {
		t.Fatalf("form.closed is the first event with the result: %v", e)
	}
	cat.Must(200, "GET", fForm(id)+"/results", nil)

	// A sign-up sheet: an option with places is full when they are taken.
	slots := fPoll(map[string]any{"preset": "signup", "fields": []any{map[string]any{"id": "choice", "type": "choice", "label": "Slot",
		"required": true, "options": []any{map[string]any{"id": "o1", "label": "Morning", "max": 1}, map[string]any{"id": "o2", "label": "Evening"}}}}})
	id = fID(fPost(ann, ops, slots))
	bob.Must(200, "PUT", fForm(id)+"/answer", fPick("o1"))
	bob.Must(200, "PUT", fForm(id)+"/answer", fPick("o1")) // one's own place does not count against one
	if e := cat.Refused(400, model.CodeInvalidValue, "PUT", fForm(id)+"/answer", fPick("o1")); e["field"] != "choice" {
		t.Fatalf("a full slot: %v", e)
	}
	bob.Must(200, "PUT", fForm(id)+"/answer", fPick("o2"))
	cat.Must(200, "PUT", fForm(id)+"/answer", fPick("o1"))

	// Added options.
	id = fID(fPost(ann, ops, fPoll(map[string]any{"allow_added_options": true})))
	added := obj(bob.Must(201, "POST", fForm(id)+"/options", map[string]any{"field_id": "choice", "label": " Soup "}), "form")
	options := arr(arr(added, "fields")[0].(map[string]any), "options")
	last := options[len(options)-1].(map[string]any)
	if len(options) != 3 || last["label"] != "Soup" || last["added_by_id"] != string(bob.ID) || last["id"] != "a1" {
		t.Fatalf("an added option: %v", options)
	}
	if e := annStream.Expect("form.option_added"); len(arr(arr(obj(e, "form"), "fields")[0].(map[string]any), "options")) != 3 {
		t.Fatalf("form.option_added: %v", e)
	}
	bob.Must(200, "PUT", fForm(id)+"/answer", fPick("a1"))
	bob.Refused(409, model.CodeConflict, "POST", fForm(id)+"/options", map[string]any{"field_id": "choice", "label": "Soup"})
	bob.Refused(400, model.CodeInvalidValue, "POST", fForm(id)+"/options", map[string]any{"field_id": "nope", "label": "Stew"})
	bob.Refused(400, model.CodeInvalidValue, "POST", fForm(id)+"/options", map[string]any{"field_id": "choice", "label": strings.Repeat("x", 101)})
	bob.Refused(400, model.CodeInvalidRequest, "POST", fForm(id)+"/options", map[string]any{"field_id": "choice"})
}

// TestAnonymousForms checks that nothing says who answered what on an
// anonymous form, to anyone (docs/PERMISSIONS.md case 70).
func TestAnonymousForms(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	admin := ts.Admin("ada")
	ops := ann.Channel("ops", false, bob, cat)
	fAdd(t, ts, ops, admin, model.LevelManager)
	fAdd(t, ts, ops, ts.Owner, model.LevelManager)
	annStream, _ := ann.Events(nil)

	id := fID(fPost(ann, ops, fPoll(map[string]any{"anonymous": true})))
	bob.Must(200, "PUT", fForm(id)+"/answer", fPick("o1"))
	raw, _ := json.Marshal(annStream.Expect("form.answered"))
	if strings.Contains(string(raw), string(bob.ID)) || strings.Contains(string(raw), "voters") {
		t.Fatalf("the event of an anonymous answer names the answerer: %s", raw)
	}
	time.Sleep(20 * time.Millisecond)
	out := obj(cat.Must(200, "PUT", fForm(id)+"/answer", fPick("o2")), "form")
	if out["voters"] != nil || out["my_answer"] == nil || fTally(out, "choice", "o2") != 1.0 {
		t.Fatalf("the answerer's own view of an anonymous poll: %v", out)
	}
	for name, c := range map[string]*testkit.Client{"its author": ann, "a manager who is an admin": admin, "the owner": ts.Owner, "another answerer": bob} {
		for _, path := range []string{"/api/v1/messages/" + id, fForm(id) + "/results", fMsgs(ops)} {
			raw, _ := json.Marshal(c.Must(200, "GET", path, nil))
			who := string(cat.ID)
			if c == bob {
				who = string(cat.ID)
			}
			if strings.Contains(string(raw), `"voters"`) || (path != fMsgs(ops) && strings.Contains(string(raw), who)) {
				t.Fatalf("%s learns who answered an anonymous poll from %s: %s", name, path, raw)
			}
		}
	}
	// The rows carry no account, share one time, and their identifiers
	// do not say when each was given.
	rows := arr(ann.Must(200, "GET", fForm(id)+"/results", nil), "answers")
	if len(rows) != 2 {
		t.Fatalf("rows: %v", rows)
	}
	a, b := rows[0].(map[string]any), rows[1].(map[string]any)
	if a["account_id"] != nil || a["updated_at"] != nil || a["created_at"] != b["created_at"] || a["id"].(string)[:13] != b["id"].(string)[:13] {
		t.Fatalf("anonymous rows give something away: %v", rows)
	}
	// Paging keeps to the same order and gives nothing away either.
	first := ann.Must(200, "GET", fForm(id)+"/results?limit=1", nil)
	second := ann.Must(200, "GET", fForm(id)+"/results?limit=1&cursor="+first["next_cursor"].(string), nil)
	if arr(first, "answers")[0].(map[string]any)["id"] == arr(second, "answers")[0].(map[string]any)["id"] || second["next_cursor"] != nil {
		t.Fatalf("paging anonymous rows: %v %v", first, second)
	}

	// An anonymous collect form: the same for its answers and its file.
	collect := map[string]any{"title": "Feedback", "anonymous": true, "action": map[string]any{"type": "collect"},
		"fields": []any{map[string]any{"id": "say", "type": "longtext", "label": "Say"}}}
	id = fID(fPost(ann, ops, collect))
	bob.Must(200, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"say": "=more biscuits"}})
	raw, _ = json.Marshal(annStream.Expect("form.answered"))
	if strings.Contains(string(raw), string(bob.ID)) || strings.Contains(string(raw), "biscuits") {
		t.Fatalf("the event of an anonymous collect answer: %s", raw)
	}
	for _, c := range []*testkit.Client{ann, admin, ts.Owner} {
		raw, _ := json.Marshal(c.Must(200, "GET", fForm(id)+"/results", nil))
		if strings.Contains(string(raw), string(bob.ID)) || !strings.Contains(string(raw), "biscuits") {
			t.Fatalf("anonymous collect answers: %s", raw)
		}
		csv := c.Must(200, "GET", fForm(id)+"/results.csv", nil)["_raw"].(string)
		if strings.Contains(csv, "bob") || strings.Contains(csv, "Answered") || !strings.Contains(csv, "'=more biscuits") {
			t.Fatalf("the CSV of an anonymous form: %q", csv)
		}
	}
	// Anonymous goes only with tally and collect.
	ann.Refused(400, model.CodeInvalidRequest, "POST", fMsgs(ops), map[string]any{"form": map[string]any{"anonymous": true,
		"action": map[string]any{"type": "thread"}, "fields": []any{map[string]any{"id": "a", "type": "text", "label": "A"}}}})
}

// TestCollectAndThreadForms covers the `collect` and `thread` actions.
func TestCollectAndThreadForms(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("dan")
	ops := ann.Channel("ops", false, bob)
	fAdd(t, ts, ops, dan, model.LevelReader)
	annStream, _ := ann.Events(nil)

	fields := []any{
		map[string]any{"id": "name", "type": "text", "label": "Name", "required": true},
		map[string]any{"id": "size", "type": "number", "label": "Size", "min": 1, "max": 5},
		map[string]any{"id": "who", "type": "account", "label": "Buddy"},
		map[string]any{"id": "where", "type": "conversation", "label": "Channel"},
		map[string]any{"id": "ok", "type": "checkbox", "label": "Agreed"},
	}
	id := fID(fPost(ann, ops, map[string]any{"title": "Order", "fields": fields, "action": map[string]any{"type": "collect"}}))
	answer := map[string]any{"answers": map[string]any{"name": "Bob", "size": 3, "who": []string{string(ann.ID)}, "where": []string{ops}, "ok": true}}
	out := obj(bob.Must(200, "PUT", fForm(id)+"/answer", answer), "form")
	if out["answer_count"] != 1.0 || out["tally"] != nil || obj(out, "my_answer")["name"] != "Bob" {
		t.Fatalf("a collect answer: %v", out)
	}
	e := annStream.Expect("form.answered")
	if f := obj(e, "form"); f["answer_count"] != 1.0 || f["tally"] != nil || f["my_answer"] != nil || e["account_id"] != nil {
		t.Fatalf("members get the new count only: %v", e)
	}
	// An account or channel the answerer cannot see is no answer.
	priv := ann.Channel("quiet", true)
	bob.Refused(400, model.CodeInvalidValue, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"name": "Bob", "where": []string{priv}}})
	bob.Refused(400, model.CodeInvalidValue, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"name": "Bob", "size": 9}})
	dan.Must(200, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"name": "Dan"}})

	// The author and managers read the answers; nobody else (case 69).
	rows := arr(ann.Must(200, "GET", fForm(id)+"/results", nil), "answers")
	if len(rows) != 2 || rows[0].(map[string]any)["account_id"] != string(bob.ID) || obj(rows[0].(map[string]any), "answers")["name"] != "Bob" {
		t.Fatalf("collect answers: %v", rows)
	}
	bob.Refused(403, model.CodeForbidden, "GET", fForm(id)+"/results", nil)
	bob.Refused(403, model.CodeForbidden, "GET", fForm(id)+"/results.csv", nil)
	status, header, body := ann.DoFull("GET", fForm(id)+"/results.csv", nil)
	csv, _ := body["_raw"].(string)
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if status != 200 || !strings.HasPrefix(header.Get("Content-Type"), "text/csv") || len(lines) != 3 ||
		!strings.HasPrefix(lines[0], "Name,Size,Buddy,Channel,Agreed,Answered by,Answered at") || !strings.HasPrefix(lines[1], "Bob,3,@ann,#ops,Yes,bob,") {
		t.Fatalf("the CSV: %d %v %q", status, header, csv)
	}
	bob.Must(200, "DELETE", fForm(id)+"/answer", nil)
	if n := len(arr(ann.Must(200, "GET", fForm(id)+"/results", nil), "answers")); n != 1 {
		t.Fatalf("after a withdrawal there are %d answers", n)
	}

	// thread: each answer is a reply under the answerer's name, even a
	// reader's, and cannot be changed.
	check := map[string]any{"preset": "checkin", "title": "Stand-up", "action": map[string]any{"type": "thread"}, "fields": []any{
		map[string]any{"id": "did", "type": "text", "label": "Yesterday", "required": true},
		map[string]any{"id": "will", "type": "longtext", "label": "Today"}}}
	id = fID(fPost(ann, ops, check))
	out = obj(dan.Must(200, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"did": "Reviewed", "will": "Ship"}}), "form")
	if out["answer_count"] != 1.0 || obj(out, "my_answer")["did"] != "Reviewed" {
		t.Fatalf("a thread answer: %v", out)
	}
	thread := ann.Must(200, "GET", "/api/v1/messages/"+id+"/thread", nil)
	replies := arr(thread, "messages")
	if len(replies) != 1 {
		t.Fatalf("the answer should be a reply: %v", thread)
	}
	reply := replies[0].(map[string]any)
	card := arr(reply, "cards")[0].(map[string]any)
	if reply["author_id"] != string(dan.ID) || obj(reply, "via")["kind"] != "command" || obj(reply, "via")["name"] != "Stand-up" ||
		reply["text"] != nil || card["title"] != "Stand-up" || len(arr(card, "fields")) != 2 ||
		arr(card, "fields")[0].(map[string]any)["value"] != "Reviewed" {
		t.Fatalf("the reply a thread answer becomes: %v", reply)
	}
	dan.Refused(409, model.CodeAlreadyExists, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"did": "Again"}})
	dan.Refused(409, model.CodeConflict, "DELETE", fForm(id)+"/answer", nil)
	// The answer can be found by searching for what it says.
	if found := arr(ann.Must(200, "GET", "/api/v1/search?q=Reviewed", nil), "results"); len(found) != 1 {
		t.Fatalf("searching for an answer: %v", found)
	}
}

// TestTicketAndGroupForms covers the `channel` and `group` actions and
// answerer_group_id.
func TestTicketAndGroupForms(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	admin := ts.Admin("ada")
	help := ann.Channel("help", false, bob, cat)
	guest := ts.Guest("gil", help)
	fAdd(t, ts, help, admin, model.LevelMember)
	support := fGroup(t, ts, "support", false, cat)

	ticket := map[string]any{"preset": "ticket", "title": "Ask support", "one_answer": false,
		"action": map[string]any{"type": "channel", "name_prefix": "ticket", "group_id": support, "level": "manager"},
		"fields": []any{map[string]any{"id": "what", "type": "text", "label": "What is wrong?", "required": true}}}
	bob.Refused(403, model.CodeForbidden, "POST", fMsgs(help), map[string]any{"form": ticket}) // case 61
	id := fID(fPost(ann, help, ticket))

	bobStream, _ := bob.Events(nil)
	out := bob.Must(200, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"what": "Printer"}})
	made, _ := out["conversation_id"].(string)
	if made == "" || obj(out, "form")["answer_count"] != 1.0 || obj(out, "form")["my_answer"] != nil {
		t.Fatalf("opening a ticket: %v", out)
	}
	if joined := bobStream.Expect("member.joined"); obj(joined, "conversation")["name"] != "ticket-1" {
		t.Fatalf("the answerer is sent the new channel: %v", joined)
	}
	c := obj(bob.Must(200, "GET", "/api/v1/conversations/"+made, nil), "conversation")
	if c["name"] != "ticket-1" || c["private"] != true || c["ticket_number"] != 1.0 || c["created_by_id"] != string(bob.ID) ||
		obj(c, "membership")["level"] != "manager" {
		t.Fatalf("the ticket channel: %v", c)
	}
	// The group is offered it at the action's level (ruling R103); nobody
	// else can see it.
	cat.AcceptOffer(made)
	if lvl := obj(cat.Must(200, "GET", "/api/v1/conversations/"+made, nil), "conversation", "membership"); lvl["level"] != "manager" || lvl["via_group"] != true {
		t.Fatalf("the group's member in the ticket channel: %v", lvl)
	}
	ann.Refused(404, model.CodeNotFound, "GET", "/api/v1/conversations/"+made, nil)
	first := arr(bob.Must(200, "GET", fMsgs(made), nil), "messages")
	if len(first) != 2 || first[0].(map[string]any)["author_id"] != string(bob.ID) || obj(first[1].(map[string]any), "system")["kind"] != "member_joined" ||
		arr(arr(first[0].(map[string]any), "cards")[0].(map[string]any), "fields")[0].(map[string]any)["value"] != "Printer" {
		t.Fatalf("the ticket's first message: %v", first)
	}
	// Many answers, each its own channel; none can be withdrawn.
	out = bob.Must(200, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"what": "Scanner"}})
	if n := obj(bob.Must(200, "GET", "/api/v1/conversations/"+out["conversation_id"].(string), nil), "conversation")["name"]; n != "ticket-2" {
		t.Fatalf("the second ticket is %v", n)
	}
	bob.Refused(409, model.CodeConflict, "DELETE", fForm(id)+"/answer", nil)
	out = guest.Must(200, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"what": "Badge"}})
	third := out["conversation_id"].(string)
	if n := obj(guest.Must(200, "GET", "/api/v1/conversations/"+third, nil), "conversation")["name"]; n != "ticket-3" {
		t.Fatalf("the third ticket is %v", n)
	}
	// A ticket is a sub-channel of the channel holding the form, and of
	// that channel's parent when it is itself a sub-channel (R49).
	if p, err := ts.Server.Store.Forms().TicketParent(fctx, model.ConversationID(third)); err != nil || string(p) != help {
		t.Fatalf("a ticket's parent is %q (%v), want the channel holding the form", p, err)
	}

	// group: buttons join and leave open groups.
	birds := fGroup(t, ts, "birders", true)
	closedGroup := fGroup(t, ts, "board", false)
	roles := func(group string) map[string]any {
		return map[string]any{"preset": "roles", "title": "Pick your groups", "action": map[string]any{"type": "group"}, "buttons": []any{
			map[string]any{"id": "join", "label": "Join birders", "group_id": group, "group_action": "join"},
			map[string]any{"id": "leave", "label": "Leave birders", "group_id": group, "group_action": "leave"}}}
	}
	ann.Refused(403, model.CodeForbidden, "POST", fMsgs(help), map[string]any{"form": roles(birds)})            // case 62
	admin.Refused(400, model.CodeInvalidValue, "POST", fMsgs(help), map[string]any{"form": roles(closedGroup)}) // the group must be open
	id = fID(fPost(admin, help, roles(birds)))
	in := func(c *testkit.Client) bool {
		is, err := ts.Server.Store.Groups().IsMember(fctx, model.GroupID(birds), c.ID)
		if err != nil {
			t.Fatal(err)
		}
		return is
	}
	out = bob.Must(200, "POST", fForm(id)+"/buttons/join", map[string]any{})
	if !in(bob) || obj(out, "form")["answer_count"] != 0.0 {
		t.Fatalf("pressing join: in the group %v, %v", in(bob), out)
	}
	bob.Must(200, "POST", fForm(id)+"/buttons/join", map[string]any{}) // already in: nothing happens
	bob.Must(200, "POST", fForm(id)+"/buttons/leave", map[string]any{})
	bob.Must(200, "POST", fForm(id)+"/buttons/leave", map[string]any{})
	if in(bob) {
		t.Fatal("pressing leave left bob in the group")
	}
	guest.Refused(403, model.CodeForbidden, "POST", fForm(id)+"/buttons/join", map[string]any{}) // guests are put in by an admin
	bob.Refused(400, model.CodeInvalidRequest, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{}})
	if _, err := ts.Server.Store.Groups().Update(fctx, model.GroupID(birds), func(g *model.Group) error { g.Open = false; return nil }); err != nil {
		t.Fatal(err)
	}
	bob.Refused(409, model.CodeConflict, "POST", fForm(id)+"/buttons/join", map[string]any{})

	// answerer_group_id: only a manager may set it, and then only the
	// group's members answer or press (cases 208 and 209).
	approve := map[string]any{"preset": "approve", "title": "Ship it?", "action": map[string]any{"type": "tally"}, "answerer_group_id": support,
		"buttons": []any{map[string]any{"id": "approve", "label": "Approve", "style": "primary"}, map[string]any{"id": "decline", "label": "Decline", "style": "danger"}}}
	bob.Refused(403, model.CodeForbidden, "POST", fMsgs(help), map[string]any{"form": approve})
	id = fID(fPost(ann, help, approve))
	bob.Refused(403, model.CodeForbidden, "POST", fForm(id)+"/buttons/approve", map[string]any{})
	out = obj(cat.Must(200, "POST", fForm(id)+"/buttons/approve", map[string]any{}), "form")
	if fTally(out, "_button", "approve") != 1.0 || fTally(out, "_button", "decline") != 0.0 || obj(out, "my_answer")["_button"] != "approve" ||
		arr(obj(out, "voters", "_button"), "approve")[0] != string(cat.ID) {
		t.Fatalf("a button press on an approval: %v", out)
	}
	out = obj(cat.Must(200, "POST", fForm(id)+"/buttons/decline", map[string]any{}), "form")
	if fTally(out, "_button", "approve") != 0.0 || fTally(out, "_button", "decline") != 1.0 || out["answer_count"] != 1.0 {
		t.Fatalf("pressing the other button replaces the press: %v", out)
	}
	cat.Refused(400, model.CodeInvalidRequest, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{}})
}

// fBots stands in for the bots package: it records what forms hands it.
type fBots struct {
	mu   sync.Mutex
	got  []model.FormSubmission
	down bool
}

func (b *fBots) FormAnswered(_ context.Context, sub model.FormSubmission) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.down {
		return "", model.ErrUnavailable.With("The bot is not connected.")
	}
	b.got = append(b.got, sub)
	return "0193c300-0000-7000-8000-00000000000" + string(rune('0'+len(b.got))), nil
}

// TestBotFormsAndDialogs covers the `bot` action and dialogs up to the point
// where the bots package takes over.
func TestBotFormsAndDialogs(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ops := ann.Channel("ops", false, bob)
	s := ts.Server
	bot := model.Account{Username: "deploybot", DisplayName: "Deploy", Role: model.RoleBot}
	if _, err := s.Store.Accounts().Create(fctx, &bot); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Bots().Create(fctx, store.BotInfo{AccountID: bot.ID, CreatedBy: ts.Owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Members().AddDirect(fctx, model.ConversationID(ops), bot.ID, model.LevelMember, model.Now()); err != nil {
		t.Fatal(err)
	}
	fake := &fBots{down: true}
	s.Forms.SetBots(fake)

	posted, err := s.Chat.Post(fctx, bot, chat.NewMessage{ConversationID: model.ConversationID(ops), Text: "Deploy?", Form: &model.FormInput{
		Title:   "Deploy",
		Fields:  []model.FormField{{ID: "reason", Type: "text", Label: "Reason", Required: true}},
		Buttons: []model.FormButton{{ID: "go", Label: "Go", Style: "primary"}},
		Action:  model.FormAction{Type: model.FormBot},
	}})
	if err != nil {
		t.Fatal(err)
	}
	id := string(posted.Message.ID)
	annStream, _ := ann.Events(nil)

	// The bot is not connected: the person is told so.
	bob.Refused(503, model.CodeUnavailable, "POST", fForm(id)+"/buttons/go", map[string]any{"answers": map[string]any{"reason": "Hotfix"}})
	fake.down = false
	// An answer that does not fit never reaches the bot.
	bob.Refused(400, model.CodeInvalidValue, "POST", fForm(id)+"/buttons/go", map[string]any{"answers": map[string]any{}})
	out := bob.Must(200, "POST", fForm(id)+"/buttons/go", map[string]any{"answers": map[string]any{"reason": "Hotfix"}})
	if out["interaction_id"] == nil || obj(out, "form")["answer_count"] != 0.0 || obj(out, "form")["my_answer"] != nil {
		t.Fatalf("pressing a bot form's button: %v", out)
	}
	bob.Must(200, "PUT", fForm(id)+"/answer", map[string]any{"answers": map[string]any{"reason": "Again"}}) // any number of times
	if len(fake.got) != 2 || fake.got[0].BotID != bot.ID || fake.got[0].AccountID != bob.ID || fake.got[0].ButtonID != "go" ||
		fake.got[0].Answers["reason"] != "Hotfix" || string(fake.got[0].MessageID) != id || fake.got[0].Dialog || fake.got[1].ButtonID != "" {
		t.Fatalf("what the bot was handed: %+v", fake.got)
	}
	// Nothing is stored and the other members hear nothing.
	if rows := arr(ann.Must(200, "GET", fForm(id)+"/results", nil), "answers"); len(rows) != 0 {
		t.Fatalf("a bot form stored answers: %v", rows)
	}
	bob.Refused(404, model.CodeNotFound, "DELETE", fForm(id)+"/answer", nil)
	for {
		f, ok := annStream.Next(300 * time.Millisecond)
		if !ok {
			break
		}
		if kind, _ := f["type"].(string); strings.HasPrefix(kind, "form.") || strings.HasPrefix(kind, "message.") {
			t.Fatalf("the other members were told of an answer to a bot form: %v", f)
		}
	}

	// A dialog: stored for one person, answered once, then gone.
	bobAccount, err := s.Store.Accounts().Get(fctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	annAccount, _ := s.Store.Accounts().Get(fctx, ann.ID)
	why := model.FormInput{Title: "Roll back", Fields: []model.FormField{{ID: "why", Type: "longtext", Label: "Why?", Required: true}},
		Buttons: []model.FormButton{{ID: "ok", Label: "Roll back", Style: "danger"}}}
	d, err := s.Forms.OpenDialog(fctx, bot, bob.ID, model.ConversationID(ops), why)
	if err != nil || d.BotID != bot.ID || len(d.Fields) != 1 || string(d.ConversationID) != ops {
		t.Fatalf("opening a dialog: %+v %v", d, err)
	}
	if _, err := s.Forms.OpenDialog(fctx, bot, bob.ID, model.ConversationID(ops), model.FormInput{Anonymous: true, Fields: why.Fields}); model.CodeOf(err) != model.CodeInvalidRequest {
		t.Fatalf("a dialog with a setting: %v", err)
	}
	if _, err := s.Forms.SubmitDialog(fctx, annAccount, d.ID, "", "", "ok", map[string]any{"why": "x"}); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("someone else's dialog: %v", err)
	}
	if _, err := s.Forms.SubmitDialog(fctx, bobAccount, d.ID, "", "", "ok", map[string]any{}); model.CodeOf(err) != model.CodeInvalidValue {
		t.Fatalf("a dialog answer that does not fit: %v", err)
	}
	if _, err := s.Forms.SubmitDialog(fctx, bobAccount, d.ID, "", "", "nope", map[string]any{"why": "x"}); model.CodeOf(err) != model.CodeInvalidValue {
		t.Fatalf("a button the dialog does not have: %v", err)
	}
	interaction, err := s.Forms.SubmitDialog(fctx, bobAccount, d.ID, model.ConversationID(ops), "", "ok", map[string]any{"why": "Broken"})
	if err != nil || interaction == "" {
		t.Fatalf("submitting a dialog: %v %v", interaction, err)
	}
	last := fake.got[len(fake.got)-1]
	if !last.Dialog || last.FormID != d.ID || last.MessageID != "" || last.Answers["why"] != "Broken" || last.ButtonID != "ok" {
		t.Fatalf("what the bot was handed for a dialog: %+v", last)
	}
	if _, err := s.Forms.SubmitDialog(fctx, bobAccount, d.ID, "", "", "ok", map[string]any{"why": "Twice"}); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("a dialog is gone once answered: %v", err)
	}
	d2, _ := s.Forms.OpenDialog(fctx, bot, bob.ID, model.ConversationID(ops), why)
	if err := s.Forms.DismissDialog(fctx, annAccount, d2.ID); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("dismissing someone else's dialog: %v", err)
	}
	if err := s.Forms.DismissDialog(fctx, bobAccount, d2.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Forms.DismissDialog(fctx, bobAccount, d2.ID); model.CodeOf(err) != model.CodeNotFound {
		t.Fatalf("a dismissed dialog is gone: %v", err)
	}
	// A bot cannot answer a form (case 82).
	poll, err := s.Chat.Post(fctx, annAccount, chat.NewMessage{ConversationID: model.ConversationID(ops), Form: &model.FormInput{
		Fields: []model.FormField{{ID: "choice", Type: "choice", Label: "Pick", Options: []model.FormOption{{ID: "o1", Label: "A"}}}},
		Action: model.FormAction{Type: model.FormTally}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Forms.Answer(fctx, bot, poll.Message.ID, map[string]any{"choice": []any{"o1"}}); model.CodeOf(err) != model.CodeForbidden {
		t.Fatalf("a bot answering a form: %v", err)
	}
}

// TestSharedConversationForms: in a conversation shared with other servers
// only polls are allowed (ruling R12; cases 150 and 199).
func TestSharedConversationForms(t *testing.T) {
	ts := testkit.NewServer(t)
	ann := ts.Member("ann")
	ops := ann.Channel("ops", false)
	if _, err := ts.Server.Store.Conversations().Update(fctx, model.ConversationID(ops), func(c *model.Conversation) error {
		c.StreamID = model.NewID[model.StreamID]()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fPost(ann, ops, fPoll(nil))
	fPost(ann, ops, fPoll(map[string]any{"anonymous": true}))
	field := []any{map[string]any{"id": "a", "type": "text", "label": "A"}}
	for _, action := range []map[string]any{{"type": "collect"}, {"type": "thread"},
		{"type": "channel", "name_prefix": "ticket", "group_id": "0193a0d1-0000-7000-8000-000000000001"}} {
		ann.Refused(403, model.CodeForbidden, "POST", fMsgs(ops), map[string]any{"form": map[string]any{"action": action, "fields": field}})
	}
}

// TestChecklists covers ticking and an item's history.
func TestChecklists(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	ops := ann.Channel("ops", false, bob)
	fAdd(t, ts, ops, dan, model.LevelReader)
	msg := ann.Post(ops, "Before release:\n\n- [ ] Tag it\n- [x] Tell support")
	id := fID(msg)
	item := func(n string) string { return "/api/v1/messages/" + id + "/checklist/" + n }
	if tasks := arr(msg, "tasks"); len(tasks) != 2 || tasks[1].(map[string]any)["checked"] != true {
		t.Fatalf("tasks: %v", msg["tasks"])
	}
	annStream, _ := ann.Events(nil)

	task := obj(bob.Must(200, "PUT", item("t1"), map[string]any{"checked": true}), "task")
	if task["id"] != "t1" || task["checked"] != true || task["checked_by_id"] != string(bob.ID) || task["checked_at"] == nil {
		t.Fatalf("a ticked item: %v", task)
	}
	e := annStream.Expect("task.changed")
	if e["message_id"] != id || obj(e, "task")["checked_by_id"] != string(bob.ID) || e["seq"] == nil {
		t.Fatalf("task.changed: %v", e)
	}
	// The same state again changes nothing and writes no history.
	bob.Must(200, "PUT", item("t1"), map[string]any{"checked": true})
	ann.Must(200, "PUT", item("t1"), map[string]any{"checked": false})
	changes := arr(dan.Must(200, "GET", item("t1")+"/history", nil), "changes")
	if len(changes) != 2 || changes[0].(map[string]any)["account_id"] != string(bob.ID) || changes[1].(map[string]any)["checked"] != false {
		t.Fatalf("history: %v", changes)
	}
	// Ticking is not an edit.
	after := obj(ann.Must(200, "GET", "/api/v1/messages/"+id, nil), "message")
	if after["edited_at"] != nil || after["has_history"] == true || arr(after, "tasks")[0].(map[string]any)["checked"] != false {
		t.Fatalf("after ticking: %v", after)
	}

	dan.Refused(403, model.CodeForbidden, "PUT", item("t1"), map[string]any{"checked": true}) // case 51
	cat.Refused(403, model.CodeForbidden, "PUT", item("t1"), map[string]any{"checked": true}) // case 204
	cat.Must(200, "GET", item("t1")+"/history", nil)                                          // anyone who can read
	cat.Must(200, "POST", "/api/v1/conversations/"+ops+"/join", nil)
	cat.Must(200, "PUT", item("t1"), map[string]any{"checked": true}) // case 206
	bob.Refused(404, model.CodeNotFound, "PUT", item("t9"), map[string]any{"checked": true})
	bob.Refused(404, model.CodeNotFound, "GET", item("t9")+"/history", nil)
	bob.Refused(400, model.CodeInvalidRequest, "PUT", item("t1"), map[string]any{})
	priv := ann.Channel("quiet", true)
	hidden := fID(ann.Post(priv, "- [ ] secret"))
	bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+hidden+"/checklist/t1", map[string]any{"checked": true})
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+hidden+"/checklist/t1/history", nil)

	// A deleted message's items cannot be ticked.
	gone := fID(ann.Post(ops, "- [ ] soon gone"))
	time.Sleep(10 * time.Millisecond)
	ann.Must(200, "DELETE", "/api/v1/messages/"+gone, nil)
	bob.Refused(404, model.CodeNotFound, "PUT", "/api/v1/messages/"+gone+"/checklist/t1", map[string]any{"checked": true})
}

// TestTicketTellsTheGroup covers the people a ticket form's group brings
// into a new ticket. Each is sent the channel as they have it, with their
// own membership: a client takes a conversation that comes without one for
// a channel the reader is not in, and offered to join a ticket its reader
// was already in. An answerer who is in the group is told of both ways
// they are in. And a form whose group has nobody in it opens a ticket all
// the same: the answerer is alone in it and so its manager.
func TestTicketTellsTheGroup(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	help := ann.Channel("help", false, bob, cat)
	support := fGroup(t, ts, "support", false, cat)
	ticket := func(group, prefix string) map[string]any {
		return map[string]any{"preset": "ticket", "title": "Ask support", "one_answer": false,
			"action": map[string]any{"type": "channel", "name_prefix": prefix, "group_id": group, "level": "manager"},
			"fields": []any{map[string]any{"id": "what", "type": "text", "label": "What is wrong?", "required": true}}}
	}
	answer := map[string]any{"answers": map[string]any{"what": "Printer"}}
	id := fID(fPost(ann, help, ticket(support, "ticket")))

	// Someone outside the group opens one: the group's member is sent the
	// channel with her membership.
	catStream, _ := cat.Events(nil)
	out := bob.Must(200, "PUT", fForm(id)+"/answer", answer)
	added := catStream.Expect("channel_group.added")
	if m := obj(added, "conversation", "membership"); obj(added, "conversation")["id"] != out["conversation_id"] ||
		m["level"] != "manager" || m["via_group"] != true || m["direct"] == true {
		t.Fatalf("the group's member is sent the ticket with her membership: %v", added)
	}

	// The group's member opens one herself: she is in it through the
	// group and in her own right, and is told so.
	out = cat.Must(200, "PUT", fForm(id)+"/answer", answer)
	added = catStream.Expect("channel_group.added")
	if m := obj(added, "conversation", "membership"); obj(added, "conversation")["id"] != out["conversation_id"] ||
		m["level"] != "manager" || m["via_group"] != true || m["direct"] != true {
		t.Fatalf("an answerer in the group is sent the ticket with her membership: %v", added)
	}

	// A group with nobody in it. The store deletes a private channel left
	// with no members, so the answerer goes in first.
	nobody := fGroup(t, ts, "nobody", false)
	id = fID(fPost(ann, help, ticket(nobody, "quiet")))
	bobStream, _ := bob.Events(nil)
	out = bob.Must(200, "PUT", fForm(id)+"/answer", answer)
	made, _ := out["conversation_id"].(string)
	if joined := bobStream.Expect("member.joined"); obj(joined, "conversation")["name"] != "quiet-1" {
		t.Fatalf("the answerer is sent the new channel: %v", joined)
	}
	c := obj(bob.Must(200, "GET", "/api/v1/conversations/"+made, nil), "conversation")
	if c["name"] != "quiet-1" || c["member_count"] != 1.0 || obj(c, "membership")["level"] != "manager" {
		t.Fatalf("a ticket whose group brought nobody: %v", c)
	}
	// The group is in the ticket: whoever joins the group later is in it.
	if _, err := ts.Server.Store.Groups().AddMember(fctx, model.GroupID(nobody), cat.ID, "", model.Now()); err != nil {
		t.Fatal(err)
	}
	if lvl := obj(cat.Must(200, "GET", "/api/v1/conversations/"+made, nil), "conversation", "membership"); lvl["level"] != "manager" || lvl["via_group"] != true {
		t.Fatalf("someone who joined the group later, in the ticket: %v", lvl)
	}
}

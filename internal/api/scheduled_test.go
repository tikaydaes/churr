package api_test

import (
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

const fScheduled = "/api/v1/scheduled"

// fIn is a moment a little way off, as a client writes it.
func fIn(d time.Duration) string { return time.Now().Add(d).UTC().Format("2006-01-02T15:04:05.000Z") }

// fFired waits for the scheduled.fired event of one item.
func fFired(t *testing.T, s *testkit.Stream, id string) map[string]any {
	t.Helper()
	for {
		e := s.Expect("scheduled.fired")
		if obj(e, "scheduled")["id"] == id {
			return e
		}
	}
}

// TestSendLater covers scheduling a message, changing it, cancelling it,
// sending it now, and what happens when it cannot be posted.
func TestSendLater(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	ops := ann.Channel("ops", false, bob)
	fAdd(t, ts, ops, dan, model.LevelReader)
	priv := ann.Channel("quiet", true)
	item := func(text, at string) map[string]any {
		return map[string]any{"conversation_id": ops, "kind": "message", "content": map[string]any{"text": text}, "run_at": at}
	}

	// The request is checked as if the message were posted now.
	bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, item("late", fIn(-time.Minute)))
	bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, map[string]any{"conversation_id": ops, "kind": "message", "content": map[string]any{"text": "when?"}})
	bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, map[string]any{"conversation_id": ops, "kind": "memo", "content": map[string]any{"text": "x"}, "run_at": fIn(time.Hour)})
	bob.Refused(400, model.CodeInvalidRequest, "POST", fScheduled, map[string]any{"conversation_id": ops, "kind": "message", "content": map[string]any{"text": "x"},
		"run_at": fIn(time.Hour), "repeat": map[string]any{"every": "day", "at": "09:00", "time_zone": "UTC"}})
	bob.Refused(400, model.CodeInvalidRequest, "POST", fScheduled, item("", fIn(time.Hour)))
	bob.Refused(400, model.CodeInvalidRequest, "POST", fScheduled, map[string]any{"conversation_id": ops, "kind": "message",
		"content": map[string]any{"text": "x", "sealed": map[string]any{"key_epoch": 1, "ciphertext": "AA=="}}, "run_at": fIn(time.Hour)})
	bob.Refused(400, model.CodeInvalidRequest, "POST", fScheduled, map[string]any{"kind": "message", "content": map[string]any{"text": "x"}})
	bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, map[string]any{"conversation_id": ops, "kind": "message",
		"content": map[string]any{"text": "x", "client_id": "0193a0d1-0000-7000-8000-000000000001"}, "run_at": fIn(time.Hour)})
	dan.Refused(403, model.CodeForbidden, "POST", fScheduled, item("a reader", fIn(time.Hour)))
	cat.Refused(403, model.CodeForbidden, "POST", fScheduled, item("not joined", fIn(time.Hour)))
	bob.Refused(404, model.CodeNotFound, "POST", fScheduled, map[string]any{"conversation_id": priv, "kind": "message", "content": map[string]any{"text": "x"}, "run_at": fIn(time.Hour)})
	bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, map[string]any{"conversation_id": ops, "kind": "message", "run_at": fIn(time.Hour),
		"content": map[string]any{"form": fPoll(map[string]any{"results": "never"})}})

	bobStream, _ := bob.Events(nil)
	annStream, _ := ann.Events(nil)
	made := obj(bob.Must(201, "POST", fScheduled, item("See you at **five**", fIn(900*time.Millisecond))), "scheduled")
	id := made["id"].(string)
	if made["kind"] != "message" || made["account_id"] != string(bob.ID) || made["next_run_at"] == nil || obj(made, "content")["text"] != "See you at **five**" {
		t.Fatalf("a scheduled message: %v", made)
	}
	if e := bobStream.Expect("scheduled.created"); obj(e, "scheduled")["id"] != id {
		t.Fatalf("scheduled.created: %v", e)
	}
	if got := obj(bob.Must(200, "GET", fScheduled+"/"+id, nil), "scheduled"); got["id"] != id {
		t.Fatalf("get: %v", got)
	}
	ann.Refused(404, model.CodeNotFound, "GET", fScheduled+"/"+id, nil) // a manager sees only recurring forms
	ann.Refused(404, model.CodeNotFound, "DELETE", fScheduled+"/"+id, nil)
	ann.Refused(404, model.CodeNotFound, "POST", fScheduled+"/"+id+"/send", nil)
	if list := arr(bob.Must(200, "GET", fScheduled+"?kind=message&conversation_id="+ops, nil), "scheduled"); len(list) != 1 {
		t.Fatalf("list: %v", list)
	}
	if list := arr(ann.Must(200, "GET", fScheduled+"?conversation_id="+ops, nil), "scheduled"); len(list) != 0 {
		t.Fatalf("someone else's scheduled message was listed: %v", list)
	}
	bob.Refused(400, model.CodeInvalidValue, "GET", fScheduled+"?kind=memo", nil)

	// It runs: an ordinary message, marked as scheduled.
	created := annStream.Expect("message.created")
	m := obj(created, "message")
	if m["text"] != "See you at **five**" || m["author_id"] != string(bob.ID) || obj(m, "via")["kind"] != "scheduled" {
		t.Fatalf("the message a scheduled item becomes: %v", created)
	}
	fired := fFired(t, bobStream, id)
	if fired["message_id"] != m["id"] || obj(fired, "scheduled")["next_run_at"] != nil || obj(fired, "scheduled")["last_run_at"] == nil {
		t.Fatalf("scheduled.fired: %v", fired)
	}
	bob.Refused(409, model.CodeConflict, "PATCH", fScheduled+"/"+id, map[string]any{"run_at": fIn(time.Hour)})
	bob.Refused(409, model.CodeConflict, "POST", fScheduled+"/"+id+"/send", nil)

	// There is no entity_hints field anywhere (ruling R52): a channel is
	// linked in the text itself, which a scheduled item keeps as written.
	hinted := item("See <#"+ops+">", fIn(time.Hour))
	hinted["content"].(map[string]any)["entity_hints"] = []any{map[string]any{"at": 4, "len": 4, "conversation_id": ops}}
	bob.Refused(400, model.CodeInvalidRequest, "POST", fScheduled, hinted)

	// Changing, sending now and cancelling.
	id = obj(bob.Must(201, "POST", fScheduled, item("draft", fIn(time.Hour))), "scheduled")["id"].(string)
	changed := obj(bob.Must(200, "PATCH", fScheduled+"/"+id, map[string]any{"content": map[string]any{"text": "final"}, "run_at": fIn(2 * time.Hour)}), "scheduled")
	if obj(changed, "content")["text"] != "final" {
		t.Fatalf("a changed item: %v", changed)
	}
	if e := bobStream.Expect("scheduled.updated"); obj(e, "scheduled", "content")["text"] != "final" {
		t.Fatalf("scheduled.updated: %v", e)
	}
	bob.Refused(400, model.CodeInvalidValue, "PATCH", fScheduled+"/"+id, map[string]any{"run_at": fIn(-time.Hour)})
	bob.Refused(400, model.CodeInvalidRequest, "PATCH", fScheduled+"/"+id, map[string]any{"repeat": map[string]any{"every": "day", "at": "09:00", "time_zone": "UTC"}})
	sent := bob.Must(200, "POST", fScheduled+"/"+id+"/send", nil)
	if obj(sent, "message")["text"] != "final" || obj(sent, "scheduled")["next_run_at"] != nil || obj(sent, "message", "via")["kind"] != "scheduled" {
		t.Fatalf("sending now: %v", sent)
	}
	bob.Refused(409, model.CodeConflict, "POST", fScheduled+"/"+id+"/send", nil)

	id = obj(bob.Must(201, "POST", fScheduled, item("never mind", fIn(time.Hour))), "scheduled")["id"].(string)
	bob.Must(204, "DELETE", fScheduled+"/"+id, nil)
	if e := bobStream.Expect("scheduled.cancelled"); obj(e, "scheduled")["id"] != id {
		t.Fatalf("scheduled.cancelled: %v", e)
	}
	bob.Refused(404, model.CodeNotFound, "GET", fScheduled+"/"+id, nil)
	bob.Refused(404, model.CodeNotFound, "DELETE", fScheduled+"/"+id, nil)
	bob.Refused(404, model.CodeNotFound, "POST", fScheduled+"/"+id+"/send", nil)

	// A reply in a thread, with a form, can be scheduled like anything
	// a person could post now.
	parent := fID(ann.Post(ops, "Lunch plans"))
	id = obj(bob.Must(201, "POST", fScheduled, map[string]any{"conversation_id": ops, "kind": "message", "run_at": fIn(time.Hour),
		"content": map[string]any{"parent_id": parent, "form": fPoll(nil)}}), "scheduled")["id"].(string)
	sent = bob.Must(200, "POST", fScheduled+"/"+id+"/send", nil)
	if obj(sent, "message")["parent_id"] != parent || obj(sent, "message", "form")["title"] != "Lunch?" {
		t.Fatalf("a scheduled reply with a form: %v", sent)
	}

	// When it is due and its owner may no longer post, it fails: the
	// content stays, and the owner is told why.
	id = obj(bob.Must(201, "POST", fScheduled, item("too late", fIn(900*time.Millisecond))), "scheduled")["id"].(string)
	if _, err := ts.Server.Store.Members().SetDirectLevel(fctx, model.ConversationID(ops), bob.ID, model.LevelReader); err != nil {
		t.Fatal(err)
	}
	fired = fFired(t, bobStream, id)
	if fired["message_id"] != nil || obj(fired, "error")["code"] != "forbidden" {
		t.Fatalf("a failed run: %v", fired)
	}
	failed := obj(bob.Must(200, "GET", fScheduled+"/"+id, nil), "scheduled")
	if failed["failed_at"] == nil || failed["failure"] != "forbidden" || failed["next_run_at"] != nil || obj(failed, "content")["text"] != "too late" {
		t.Fatalf("a failed item: %v", failed)
	}
	bob.Refused(403, model.CodeForbidden, "POST", fScheduled+"/"+id+"/send", nil)
	if _, err := ts.Server.Store.Members().SetDirectLevel(fctx, model.ConversationID(ops), bob.ID, model.LevelMember); err != nil {
		t.Fatal(err)
	}
	// Changing it without a new time does not say when; with one, the
	// failure is cleared.
	bob.Refused(400, model.CodeInvalidValue, "PATCH", fScheduled+"/"+id, map[string]any{"content": map[string]any{"text": "still late"}})
	cleared := obj(bob.Must(200, "PATCH", fScheduled+"/"+id, map[string]any{"run_at": fIn(time.Hour)}), "scheduled")
	if cleared["failed_at"] != nil || cleared["failure"] != nil || cleared["next_run_at"] == nil {
		t.Fatalf("a failed item after a change: %v", cleared)
	}
	// And a failed item can be sent by hand.
	id = obj(bob.Must(201, "POST", fScheduled, item("by hand", fIn(time.Hour))), "scheduled")["id"].(string)
	if _, err := ts.Server.Store.Scheduled().Update(fctx, id, func(s *model.ScheduledItem) error {
		s.NextRunAt, s.FailedAt, s.Failure = 0, model.Now(), "archived"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sent = bob.Must(200, "POST", fScheduled+"/"+id+"/send", nil)
	if obj(sent, "scheduled")["failed_at"] != nil || obj(sent, "message")["text"] != "by hand" {
		t.Fatalf("sending a failed item by hand: %v", sent)
	}

	// At most 200 waiting items per account (decision F13).
	for i := 0; i < model.MaxWaitingScheduled; i++ {
		row := model.ScheduledItem{AccountID: cat.ID, ConversationID: model.ConversationID(ops), Kind: model.ScheduledMessage,
			Content: model.MessageInput{Text: "x"}, NextRunAt: model.Now().Add(time.Hour)}
		if err := ts.Server.Store.Scheduled().Create(fctx, &row); err != nil {
			t.Fatal(err)
		}
	}
	cat.Must(200, "POST", "/api/v1/conversations/"+ops+"/join", nil)
	cat.Refused(409, model.CodeConflict, "POST", fScheduled, item("one too many", fIn(time.Hour)))
	page := cat.Must(200, "GET", fScheduled+"?limit=150", nil)
	if len(arr(page, "scheduled")) != 150 || page["next_cursor"] == nil {
		t.Fatalf("paging the list: %d items, cursor %v", len(arr(page, "scheduled")), page["next_cursor"])
	}
	if rest := arr(cat.Must(200, "GET", fScheduled+"?limit=150&cursor="+page["next_cursor"].(string), nil), "scheduled"); len(rest) != 50 {
		t.Fatalf("the second page has %d items", len(rest))
	}
}

// TestReminders covers reminders: to oneself, about a message, repeating,
// and one that fell due while the server was down.
func TestReminders(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob := ts.Member("ann"), ts.Member("bob")
	ops := ann.Channel("ops", false, bob)

	// A reminder to yourself arrives in your own one-person channel and
	// notifies you although you are its author.
	self := obj(bob.Must(201, "POST", "/api/v1/chats", map[string]any{"account_ids": []string{}}), "conversation")["id"].(string)
	bobStream, _ := bob.Events(nil)
	about := fID(ann.Post(ops, "The report is due on Friday"))
	made := obj(bob.Must(201, "POST", fScheduled, map[string]any{"conversation_id": self, "kind": "reminder",
		"content": map[string]any{"text": "Send the report"}, "about_message_id": about, "run_at": fIn(900 * time.Millisecond)}), "scheduled")
	id := made["id"].(string)
	if made["about_message_id"] != about {
		t.Fatalf("a reminder about a message: %v", made)
	}
	var created map[string]any
	for created == nil {
		if e := bobStream.Expect("message.created"); e["conversation_id"] == self {
			created = e
		}
	}
	m := obj(created, "message")
	if m["text"] != "Send the report" || obj(m, "via")["kind"] != "reminder" || m["author_id"] != string(bob.ID) ||
		obj(m, "quote")["message_id"] != about || m["parent_id"] != nil {
		t.Fatalf("the message a reminder becomes: %v", created)
	}
	if created["notify"] != "notify" {
		t.Fatalf("a reminder notifies its own author, got notify=%v", created["notify"])
	}
	fFired(t, bobStream, id)
	if mem := obj(bob.Must(200, "GET", "/api/v1/conversations/"+self, nil), "conversation", "membership"); mem["unread_count"] != 1.0 {
		t.Fatalf("a reminder counts as unread for its author: %v", mem)
	}

	// What a reminder may be.
	reminder := func(content map[string]any, extra map[string]any) map[string]any {
		body := map[string]any{"conversation_id": self, "kind": "reminder", "content": content, "run_at": fIn(time.Hour)}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}
	bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, reminder(map[string]any{}, nil))
	bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, reminder(map[string]any{"text": "x", "form": fPoll(nil)}, nil))
	bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, reminder(map[string]any{"text": "x"}, map[string]any{"about_message_id": "0193a0d1-0000-7000-8000-000000000001"}))
	priv := ann.Channel("quiet", true)
	hidden := fID(ann.Post(priv, "not for bob"))
	bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, reminder(map[string]any{"text": "x"}, map[string]any{"about_message_id": hidden}))
	for field, repeat := range map[string]map[string]any{
		"repeat.every":     {"every": "year", "at": "09:00", "time_zone": "UTC"},
		"repeat.at":        {"every": "day", "at": "9am", "time_zone": "UTC"},
		"repeat.time_zone": {"every": "day", "at": "09:00", "time_zone": "Mars/Olympus"},
		"repeat.days":      {"every": "day", "days": "0000000", "at": "09:00", "time_zone": "UTC"},
	} {
		body := reminder(map[string]any{"text": "x"}, map[string]any{"repeat": repeat})
		delete(body, "run_at")
		if e := bob.Refused(400, model.CodeInvalidValue, "POST", fScheduled, body); e["field"] != field {
			t.Fatalf("a bad repeat: got field %v, want %s", e["field"], field)
		}
	}
	// "About this message" alone is enough, with no text.
	bob.Must(201, "POST", fScheduled, reminder(map[string]any{}, map[string]any{"about_message_id": about}))

	// A repeating reminder in a channel: it runs, and then waits for the
	// next moment its rule gives, in the rule's own time zone.
	zone, _ := time.LoadLocation("Asia/Kolkata")
	clock := time.Now().In(zone).Add(-time.Hour).Format("15:04")
	body := map[string]any{"conversation_id": ops, "kind": "reminder", "content": map[string]any{"text": "Stand-up in five minutes"},
		"run_at": fIn(900 * time.Millisecond), "repeat": map[string]any{"every": "day", "at": clock, "time_zone": "Asia/Kolkata"}}
	made = obj(bob.Must(201, "POST", fScheduled, body), "scheduled")
	if obj(made, "repeat")["days"] != "1111111" {
		t.Fatalf("every day is stored as all seven days: %v", made)
	}
	fired := fFired(t, bobStream, made["id"].(string))
	next, err := time.Parse(time.RFC3339, obj(fired, "scheduled")["next_run_at"].(string))
	if err != nil || fired["message_id"] == nil {
		t.Fatalf("a repeating reminder's run: %v", fired)
	}
	if until := time.Until(next); next.In(zone).Format("15:04") != clock || until < 22*time.Hour || until > 24*time.Hour {
		t.Fatalf("the next run is %s (%s from now), want %s tomorrow in Kolkata", next.In(zone), until, clock)
	}
	// A repeat with no run_at starts at the next moment the rule gives.
	weekly := reminder(map[string]any{"text": "Expenses"}, map[string]any{"repeat": map[string]any{"every": "week", "at": clock, "time_zone": "Asia/Kolkata"}})
	delete(weekly, "run_at")
	made = obj(bob.Must(201, "POST", fScheduled, weekly), "scheduled")
	first, _ := time.Parse(time.RFC3339, made["next_run_at"].(string))
	if first.In(zone).Format("15:04") != clock || time.Until(first) > 24*time.Hour {
		t.Fatalf("a weekly repeat first runs at %s", first.In(zone))
	}

	// Rows that fell due while the server was down each run once, and a
	// repeating one then moves to its next future moment: not once for
	// every moment missed.
	missed := model.ScheduledItem{AccountID: bob.ID, ConversationID: model.ConversationID(ops), Kind: model.ScheduledReminder,
		Content: model.MessageInput{Text: "Missed while away"}, NextRunAt: model.Now().Add(-72 * time.Hour),
		Repeat: &model.Repeat{Every: "day", Days: "1111111", At: clock, TimeZone: "Asia/Kolkata"}}
	if err := ts.Server.Store.Scheduled().Create(fctx, &missed); err != nil {
		t.Fatal(err)
	}
	ts.Server.Sched.Wake()
	fired = fFired(t, bobStream, missed.ID)
	next, _ = time.Parse(time.RFC3339, obj(fired, "scheduled")["next_run_at"].(string))
	if !next.After(time.Now()) {
		t.Fatalf("after a missed run the next is %s, which is not in the future", next)
	}
	time.Sleep(300 * time.Millisecond)
	count := 0
	for _, raw := range arr(bob.Must(200, "GET", fMsgs(ops), nil), "messages") {
		if raw.(map[string]any)["text"] == "Missed while away" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("a row missed three days ago ran %d times, want once", count)
	}

	// Leaving the conversation deletes one's rows there.
	bob.Must(204, "POST", "/api/v1/conversations/"+ops+"/leave", nil)
	for _, raw := range arr(bob.Must(200, "GET", fScheduled, nil), "scheduled") {
		if raw.(map[string]any)["conversation_id"] == ops {
			t.Fatalf("a scheduled item outlived its owner's membership: %v", raw)
		}
	}
}

// TestRecurringForms covers check-in forms posted on a schedule and who
// manages them.
func TestRecurringForms(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat := ts.Member("ann"), ts.Member("bob"), ts.Member("cat")
	ops := ann.Channel("ops", false, bob)
	fAdd(t, ts, ops, cat, model.LevelManager)
	checkin := func() map[string]any {
		return map[string]any{"conversation_id": ops, "kind": "form", "run_at": fIn(900 * time.Millisecond),
			"repeat": map[string]any{"every": "day", "days": "1111111", "at": "09:30", "time_zone": "Europe/Dublin"},
			"content": map[string]any{"text": "Morning!", "form": map[string]any{"preset": "checkin", "title": "Stand-up", "action": map[string]any{"type": "thread"},
				"fields": []any{map[string]any{"id": "q1", "type": "text", "label": "Yesterday", "required": true}, map[string]any{"id": "q2", "type": "longtext", "label": "Today"}}}}}
	}
	bob.Refused(403, model.CodeForbidden, "POST", fScheduled, checkin()) // managers only
	for name, change := range map[string]func(map[string]any){
		"no schedule":        func(b map[string]any) { delete(b, "repeat") },
		"no form":            func(b map[string]any) { delete(obj(b, "content"), "form") },
		"not a thread form":  func(b map[string]any) { obj(b, "content", "form")["action"] = map[string]any{"type": "collect"} },
		"a choice question":  func(b map[string]any) { obj(b, "content", "form")["fields"] = fPoll(nil)["fields"] },
		"many answers each":  func(b map[string]any) { obj(b, "content", "form")["one_answer"] = false },
		"its own close time": func(b map[string]any) { obj(b, "content", "form")["closes_at"] = fIn(time.Hour) },
	} {
		body := checkin()
		change(body)
		if status, out := ann.Do("POST", fScheduled, body); status != 400 {
			t.Fatalf("a recurring form with %s: %d %v", name, status, out)
		}
	}

	annStream, _ := ann.Events(nil)
	bobStream, _ := bob.Events(nil)
	made := obj(ann.Must(201, "POST", fScheduled, checkin()), "scheduled")
	id := made["id"].(string)
	fired := fFired(t, annStream, id)
	nextRun := obj(fired, "scheduled")["next_run_at"]
	msg := obj(ann.Must(200, "GET", "/api/v1/messages/"+fired["message_id"].(string), nil), "message")
	form := obj(msg, "form")
	if msg["text"] != "Morning!" || msg["author_id"] != string(ann.ID) || obj(msg, "via")["kind"] != "scheduled" || obj(msg, "via")["name"] != "Stand-up" ||
		form["preset"] != "checkin" || form["closes_at"] != nextRun || nextRun == nil {
		t.Fatalf("a run of a recurring form: %v (next run %v)", msg, nextRun)
	}
	// Each answer is a reply in that run's thread.
	bob.Must(200, "PUT", fForm(fID(msg))+"/answer", map[string]any{"answers": map[string]any{"q1": "Reviewed"}})
	if e := bobStream.Expect("form.answered"); e["account_id"] != string(bob.ID) {
		t.Fatalf("a thread form's event names the answerer: %v", e)
	}
	bob.Refused(409, model.CodeAlreadyExists, "PUT", fForm(fID(msg))+"/answer", map[string]any{"answers": map[string]any{"q1": "Again"}})

	// Any account acting as manager of the channel sees and changes it;
	// whoever changes it becomes its owner. Others cannot tell it exists.
	if list := arr(cat.Must(200, "GET", fScheduled+"?conversation_id="+ops, nil), "scheduled"); len(list) != 1 || list[0].(map[string]any)["id"] != id {
		t.Fatalf("a manager lists the channel's recurring forms: %v", list)
	}
	if list := arr(cat.Must(200, "GET", fScheduled, nil), "scheduled"); len(list) != 0 {
		t.Fatalf("without a conversation only one's own are listed: %v", list)
	}
	if list := arr(bob.Must(200, "GET", fScheduled+"?conversation_id="+ops, nil), "scheduled"); len(list) != 0 {
		t.Fatalf("a member who is not a manager was shown a recurring form: %v", list)
	}
	bob.Refused(404, model.CodeNotFound, "GET", fScheduled+"/"+id, nil)
	bob.Refused(404, model.CodeNotFound, "PATCH", fScheduled+"/"+id, map[string]any{"run_at": fIn(time.Hour)})
	bob.Refused(404, model.CodeNotFound, "DELETE", fScheduled+"/"+id, nil)
	cat.Must(200, "GET", fScheduled+"/"+id, nil)
	cat.Refused(404, model.CodeNotFound, "POST", fScheduled+"/"+id+"/send", nil) // only its owner sends it now
	changed := obj(cat.Must(200, "PATCH", fScheduled+"/"+id, map[string]any{"repeat": map[string]any{"every": "week", "at": "10:00", "time_zone": "Europe/Dublin"}}), "scheduled")
	if changed["account_id"] != string(cat.ID) || obj(changed, "repeat")["every"] != "week" {
		t.Fatalf("a recurring form after another manager changed it: %v", changed)
	}
	if e := annStream.Expect("scheduled.updated"); obj(e, "scheduled")["account_id"] != string(cat.ID) {
		t.Fatalf("the earlier owner is told it changed hands: %v", e)
	}

	// If its owner can no longer act as manager when a run is due, it
	// fails, and another manager takes it over by changing it.
	// The owner is lowered and is listening before the row falls due: the
	// loop may notice a due row by itself, without being woken.
	if _, err := ts.Server.Store.Members().SetDirectLevel(fctx, model.ConversationID(ops), cat.ID, model.LevelMember); err != nil {
		t.Fatal(err)
	}
	catStream, _ := cat.Events(nil)
	if _, err := ts.Server.Store.Scheduled().Update(fctx, id, func(s *model.ScheduledItem) error {
		s.NextRunAt = model.Now().Add(-time.Second)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ts.Server.Sched.Wake()
	if fired := fFired(t, catStream, id); obj(fired, "error")["code"] != "forbidden" || fired["message_id"] != nil {
		t.Fatalf("a run whose owner is no longer a manager: %v", fired)
	}
	taken := obj(ann.Must(200, "PATCH", fScheduled+"/"+id, map[string]any{"content": checkin()["content"]}), "scheduled")
	if taken["account_id"] != string(ann.ID) || taken["failed_at"] != nil || taken["next_run_at"] == nil {
		t.Fatalf("a failed recurring form taken over: %v", taken)
	}
	ann.Must(204, "DELETE", fScheduled+"/"+id, nil)
}

// TestCommandsAndSavedReplies covers the command menu, saved replies, custom
// commands and running them.
func TestCommandsAndSavedReplies(t *testing.T) {
	ts := testkit.NewServer(t)
	ann, bob, cat, dan := ts.Member("ann"), ts.Member("bob"), ts.Member("cat"), ts.Member("dan")
	admin := ts.Admin("ada")
	ops := ann.Channel("ops", false, bob)
	fAdd(t, ts, ops, dan, model.LevelReader)
	const replies, community = "/api/v1/saved-replies", "/api/v1/admin/saved-replies"

	// Personal saved replies.
	shrug := obj(bob.Must(201, "POST", replies, map[string]any{"name": "meh", "text": "¯\\_(ツ)_/¯"}), "saved_reply")
	if shrug["community"] != false || shrug["name"] != "meh" || shrug["id"] == nil {
		t.Fatalf("a saved reply: %v", shrug)
	}
	bob.Refused(409, model.CodeNameTaken, "POST", replies, map[string]any{"name": "meh", "text": "again"})
	bob.Refused(400, model.CodeInvalidValue, "POST", replies, map[string]any{"name": "Shrug It", "text": "x"})
	bob.Refused(400, model.CodeInvalidValue, "POST", replies, map[string]any{"name": "blank", "text": "  "})
	bob.Refused(400, model.CodeInvalidRequest, "POST", replies, map[string]any{"name": "half"})
	// Built-in names and the names held back cannot be taken (decision F17).
	for _, name := range []string{"poll", "remind", "help", "topic", "shrug", "roll"} {
		if e := bob.Refused(409, model.CodeNameTaken, "POST", replies, map[string]any{"name": name, "text": "x"}); e["message"] != "That name isn't available." {
			t.Fatalf("a reserved name: %v", e)
		}
	}
	ann.Must(201, "POST", replies, map[string]any{"name": "meh", "text": "ann's own"}) // names are per person
	id := shrug["id"].(string)
	updated := obj(bob.Must(200, "PATCH", replies+"/"+id, map[string]any{"text": "Shrug."}), "saved_reply")
	if updated["text"] != "Shrug." || updated["name"] != "meh" {
		t.Fatalf("an updated reply: %v", updated)
	}
	ann.Refused(404, model.CodeNotFound, "PATCH", replies+"/"+id, map[string]any{"text": "mine now"})
	ann.Refused(404, model.CodeNotFound, "DELETE", replies+"/"+id, nil)
	admin.Refused(404, model.CodeNotFound, "PATCH", community+"/"+id, map[string]any{"text": "the admin's now"})
	bob.Refused(409, model.CodeNameTaken, "PATCH", replies+"/"+id, map[string]any{"name": "poll"})

	// Custom commands: admins make them, everyone uses them.
	bob.Refused(403, model.CodeForbidden, "POST", community, map[string]any{"name": "welcome", "text": "Welcome! Read #ops first."})
	welcome := obj(admin.Must(201, "POST", community, map[string]any{"name": "welcome", "text": "Welcome! Read #ops first."}), "saved_reply")
	if welcome["community"] != true {
		t.Fatalf("a custom command: %v", welcome)
	}
	admin.Refused(409, model.CodeNameTaken, "POST", community, map[string]any{"name": "welcome", "text": "again"})
	admin.Refused(409, model.CodeNameTaken, "POST", community, map[string]any{"name": "status", "text": "x"})
	bob.Refused(404, model.CodeNotFound, "PATCH", replies+"/"+welcome["id"].(string), map[string]any{"text": "bob's now"})
	bob.Refused(403, model.CodeForbidden, "PATCH", community+"/"+welcome["id"].(string), map[string]any{"text": "bob's now"})
	bob.Refused(403, model.CodeForbidden, "DELETE", community+"/"+welcome["id"].(string), nil)
	ts.Owner.Must(200, "PATCH", community+"/"+welcome["id"].(string), map[string]any{"name": "hello"})
	admin.Must(201, "POST", community, map[string]any{"name": "meh", "text": "the community's shrug"})
	if list := arr(bob.Must(200, "GET", replies, nil), "saved_replies"); len(list) != 3 {
		t.Fatalf("the community's replies and one's own: %v", list)
	}

	// The menu: built-ins, saved replies, custom commands; a person's
	// own wins over a custom command of the same name.
	menu := arr(bob.Must(200, "GET", "/api/v1/commands?conversation_id="+ops, nil), "commands")
	byName := map[string]map[string]any{}
	builtins := 0
	for _, raw := range menu {
		c := raw.(map[string]any)
		if c["kind"] == "builtin" {
			builtins++
		}
		if _, twice := byName[c["name"].(string)]; twice {
			t.Fatalf("%v is listed twice: %v", c["name"], menu)
		}
		byName[c["name"].(string)] = c
	}
	if builtins != len(model.BuiltinCommands) || byName["poll"]["hint"] == nil || byName["hello"]["kind"] != "saved_reply" ||
		byName["meh"]["saved_reply_id"] != id || byName["meh"]["description"] != "Shrug." {
		t.Fatalf("the command menu: %v", menu)
	}
	cat.Must(200, "GET", "/api/v1/commands?conversation_id="+ops, nil) // anyone who can read the channel
	priv := ann.Channel("quiet", true)
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/commands?conversation_id="+priv, nil)
	bob.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/commands", nil)
	bob.Refused(400, model.CodeInvalidValue, "GET", "/api/v1/commands?conversation_id=ops", nil)

	// Running: the text is posted as the person who typed it.
	run := "/api/v1/conversations/" + ops + "/commands"
	annStream, _ := ann.Events(nil)
	m := obj(bob.Must(200, "POST", run, map[string]any{"name": "meh"}), "message")
	if m["text"] != "Shrug." || m["author_id"] != string(bob.ID) || obj(m, "via")["kind"] != "command" || obj(m, "via")["name"] != "meh" {
		t.Fatalf("a saved reply's message: %v", m)
	}
	if e := annStream.Expect("message.created"); obj(e, "message")["id"] != m["id"] {
		t.Fatalf("message.created for a saved reply: %v", e)
	}
	// ann's own shrug, and for cat the community's.
	if m := obj(ann.Must(200, "POST", run, map[string]any{"name": "meh", "text": "  "}), "message"); m["text"] != "ann's own" {
		t.Fatalf("one's own reply wins: %v", m)
	}
	cat.Must(200, "POST", "/api/v1/conversations/"+ops+"/join", nil)
	if m := obj(cat.Must(200, "POST", run, map[string]any{"name": "meh"}), "message"); m["text"] != "the community's shrug" {
		t.Fatalf("the custom command for someone with none of their own: %v", m)
	}
	// In a thread.
	parent := fID(ann.Post(ops, "How do I start?"))
	if m := obj(bob.Must(200, "POST", run, map[string]any{"name": "hello", "parent_id": parent}), "message"); m["parent_id"] != parent {
		t.Fatalf("a saved reply in a thread: %v", m)
	}
	// Nothing may follow the name (decision F15).
	if e := bob.Refused(400, model.CodeInvalidValue, "POST", run, map[string]any{"name": "meh", "text": "at this"}); e["field"] != "text" {
		t.Fatalf("text after a saved reply: %v", e)
	}
	// Built-ins are the client's; unknown names are nothing.
	bob.Refused(400, model.CodeInvalidValue, "POST", run, map[string]any{"name": "poll", "text": "Lunch? | A | B"})
	bob.Refused(400, model.CodeInvalidValue, "POST", run, map[string]any{"name": "Not A Name"})
	bob.Refused(400, model.CodeInvalidRequest, "POST", run, map[string]any{"text": "no name"})
	bob.Refused(404, model.CodeNotFound, "POST", run, map[string]any{"name": "nothing"})
	// A saved reply posts a message, so it needs the level member.
	dan.Refused(403, model.CodeForbidden, "POST", run, map[string]any{"name": "hello"})
	ts.Member("eve").Refused(403, model.CodeForbidden, "POST", run, map[string]any{"name": "hello"})
	bob.Refused(404, model.CodeNotFound, "POST", "/api/v1/conversations/"+priv+"/commands", map[string]any{"name": "hello"})

	bob.Must(204, "DELETE", replies+"/"+id, nil)
	bob.Refused(404, model.CodeNotFound, "DELETE", replies+"/"+id, nil)
	admin.Must(204, "DELETE", community+"/"+welcome["id"].(string), nil)
	bob.Refused(404, model.CodeNotFound, "POST", run, map[string]any{"name": "hello"})
}

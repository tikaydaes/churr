package link_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func pollBody(text string, more map[string]any) map[string]any {
	form := map[string]any{"action": map[string]any{"type": "tally"}, "fields": []any{map[string]any{"id": "q", "type": "choice", "label": "Ship it?",
		"options": []any{map[string]any{"id": "y", "label": "Yes"}, map[string]any{"id": "n", "label": "No"}}}}}
	for k, v := range more {
		form[k] = v
	}
	return map[string]any{"text": text, "form": form}
}

// formOf is the form on a message as one client sees it.
func formOf(c *testkit.Client, id string) map[string]any {
	m := c.Must(200, "GET", "/api/v1/messages/"+id, nil)["message"].(map[string]any)
	f, _ := m["form"].(map[string]any)
	return f
}

// count is one option's count in a form as shown, or -1 when the tally is
// not shown.
func count(form map[string]any, option string) int {
	tally, _ := form["tally"].(map[string]any)
	q, _ := tally["q"].(map[string]any)
	if q == nil {
		return -1
	}
	n, _ := q[option].(float64)
	return int(n)
}

func answer(c *testkit.Client, id, option string) map[string]any {
	return c.Must(200, "PUT", "/api/v1/messages/"+id+"/form/answer", map[string]any{"answers": map[string]any{"q": []string{option}}})
}

// Cases 48 and 49, directly and through a hub: a poll posted on A arrives
// on the other servers with its form; a member of B answers; the answer
// travels sealed to A, which keeps the tally and tells everyone; named and
// anonymous both work; the other servers learn that someone answered and
// never what; an answer numbered after form.closed is not counted; and no
// other kind of form can be posted, by a person or a webhook.
func TestPollsAcrossServers(t *testing.T) {
	for _, hub := range []bool{false, true} {
		t.Run(fmt.Sprintf("hub=%v", hub), func(t *testing.T) {
			n := 2
			if hub {
				n = 3
			}
			s := share(t, n, hub, nil)
			a, b := s.Servers[0], s.Servers[1]
			for i := range s.Servers {
				s.link(i).TestTallyFold(50 * time.Millisecond)
			}
			bea := b.Member("bea")
			s.keeper(1).Add(s.conv[1], "member", bea)
			testkit.Eventually(t, "a knows bea", func() bool { return s.isMember(0, bea.ID) })
			msgs := "/api/v1/conversations/" + s.conv[0] + "/messages"

			// A named poll.
			named := s.ann.Must(201, "POST", msgs, pollBody("named", nil))["message"].(map[string]any)["id"].(string)
			for i := 1; i < n; i++ {
				s.Servers[i].Owner.HasMessage(s.conv[i], "named")
				f := formOf(s.Servers[i].Owner, named)
				if f == nil || f["action"].(map[string]any)["type"] != "tally" || len(f["fields"].([]any)) != 1 {
					t.Fatalf("%s holds the poll's message without its form: %v", testkit.ServerName(i), f)
				}
			}
			if mine := answer(b.Owner, named, "y")["form"].(map[string]any)["my_answer"]; mine == nil {
				t.Fatal("b's member is not shown their own answer")
			}
			answer(bea, named, "y")
			answer(s.ann, named, "n")
			answer(bea, named, "n") // changing an answer replaces it
			for i := range s.Servers {
				who := s.Servers[i].Owner
				testkit.Eventually(t, "the named tally on "+testkit.ServerName(i), func() bool {
					f := formOf(who, named)
					return count(f, "y") == 1 && count(f, "n") == 2 && f["answer_count"] == float64(3)
				})
				voters := formOf(who, named)["voters"].(map[string]any)["q"].(map[string]any)
				if fmt.Sprint(voters["y"]) != fmt.Sprint([]any{string(b.Owner.ID)}) || len(voters["n"].([]any)) != 2 {
					t.Fatalf("voters on %s: %v", testkit.ServerName(i), voters)
				}
			}
			// Taking an answer back.
			bea.Must(200, "DELETE", "/api/v1/messages/"+named+"/form/answer", nil)
			for i := range s.Servers {
				who := s.Servers[i].Owner
				testkit.Eventually(t, "the tally after a withdrawal on "+testkit.ServerName(i), func() bool {
					f := formOf(who, named)
					return count(f, "y") == 1 && count(f, "n") == 1 && f["answer_count"] == float64(2)
				})
			}

			// An anonymous poll: counts everywhere, names nowhere, and
			// only the home server holds what anyone answered.
			anon := s.ann.Must(201, "POST", msgs, pollBody("anonymous", map[string]any{"anonymous": true}))["message"].(map[string]any)["id"].(string)
			b.Owner.HasMessage(s.conv[1], "anonymous")
			answer(b.Owner, anon, "y")
			answer(s.ann, anon, "n")
			for i := range s.Servers {
				who := s.Servers[i].Owner
				testkit.Eventually(t, "the anonymous tally on "+testkit.ServerName(i), func() bool {
					f := formOf(who, anon)
					return count(f, "y") == 1 && count(f, "n") == 1 && f["answer_count"] == float64(2)
				})
				if f := formOf(who, anon); f["voters"] != nil {
					t.Fatalf("an anonymous poll names voters on %s: %v", testkit.ServerName(i), f["voters"])
				}
				rows, err := s.Servers[i].Server.Store.Forms().AllAnswers(ctx, model.FormID(formOf(who, anon)["id"].(string)))
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					acc, _ := s.Servers[i].Server.Store.Accounts().Get(ctx, row.AccountID)
					if i != 0 && !acc.Local() {
						t.Fatalf("%s holds the answer of another server's account %s", testkit.ServerName(i), row.AccountID)
					}
				}
				if want := map[int]int{0: 2, 1: 1, 2: 0}[i]; len(rows) != want {
					t.Fatalf("%s holds %d answers to the anonymous poll, not %d", testkit.ServerName(i), len(rows), want)
				}
			}
			// Nothing b stored about a's member says what they answered.
			namedForm := model.FormID(formOf(b.Owner, named)["id"].(string))
			rows, _ := b.Server.Store.Forms().AllAnswers(ctx, namedForm)
			for _, row := range rows {
				if row.AccountID == s.ann.ID && fmt.Sprint(row.Answers) != fmt.Sprint(map[string]any{"q": []any{"n"}}) {
					t.Fatalf("b holds more of ann's answer than the tally published: %v", row.Answers)
				}
			}

			// Only the home server closes. 49: an answer numbered after
			// form.closed is not counted, by anyone.
			b.Owner.Refused(403, model.CodeForbidden, "POST", "/api/v1/messages/"+anon+"/form/close", nil)
			s.ann.Must(200, "POST", "/api/v1/messages/"+anon+"/form/close", nil)
			testkit.Eventually(t, "b learns the poll is closed", func() bool { return formOf(b.Owner, anon)["closed_at"] != nil })
			bea.Refused(409, model.CodeConflict, "PUT", "/api/v1/messages/"+anon+"/form/answer", map[string]any{"answers": map[string]any{"q": []string{"y"}}})
			box, err := s.link(1).TestAnswerBox("a.localhost", "", map[string]any{"q": []any{"y"}})
			if err != nil {
				t.Fatal(err)
			}
			s.submit(1, "form.answered", map[string]any{"message_id": anon, "form_id": formOf(b.Owner, anon)["id"], "account_id": bea.ID, "answer_box": box})
			s.settle("after the late answer")
			time.Sleep(200 * time.Millisecond)
			for i := range s.Servers {
				if f := formOf(s.Servers[i].Owner, anon); count(f, "y") != 1 || count(f, "n") != 1 || f["answer_count"] != float64(2) {
					t.Fatalf("an answer numbered after form.closed was counted on %s: %v", testkit.ServerName(i), f)
				}
			}
			// A forged answer (b answering for a's member) changes nothing.
			forged, _ := s.link(1).TestAnswerBox("a.localhost", "", map[string]any{"q": []any{"y"}})
			s.submit(1, "form.answered", map[string]any{"message_id": named, "form_id": namedForm, "account_id": s.ann.ID, "answer_box": forged})
			s.rejected(0, "forged_author", 1)
			// A tally from a server that is not the poll's home is refused.
			s.submit(1, "form.tallied", map[string]any{"message_id": named, "form_id": namedForm, "answer_count": 99, "tally": map[string]any{"q": map[string]any{"y": 99}}})
			s.rejected(0, "forged_author", 2)
			if f := formOf(a.Owner, named); count(f, "y") != 1 {
				t.Fatalf("a forged tally was taken: %v", f)
			}

			// No other kind of form: not from a person, not from a webhook.
			field := []any{map[string]any{"id": "a", "type": "text", "label": "A"}}
			for _, action := range []string{"collect", "thread"} {
				s.ann.Refused(403, model.CodeForbidden, "POST", msgs, map[string]any{"form": map[string]any{"action": map[string]any{"type": action}, "fields": field}})
			}
			_, path, _ := s.ann.Webhook(s.conv[0], "ci")
			for _, action := range []string{"collect", "thread"} {
				body, _ := json.Marshal(map[string]any{"form": map[string]any{"action": map[string]any{"type": action}, "fields": field}})
				status, _, raw := a.Raw("POST", path, http.Header{"Content-Type": {"application/json"}}, body)
				var out struct {
					Error struct {
						Code model.Code `json:"code"`
					} `json:"error"`
				}
				json.Unmarshal(raw, &out)
				if status != 403 || out.Error.Code != model.CodeForbidden {
					t.Fatalf("a webhook posting a %s form in a shared channel: %d %s", action, status, raw)
				}
			}
			body, _ := json.Marshal(pollBody("from the webhook", nil))
			if status, _, raw := a.Raw("POST", path, http.Header{"Content-Type": {"application/json"}}, body); status/100 != 2 {
				t.Fatalf("a webhook's poll in a shared channel: %d %s", status, raw)
			}
			hooked := b.Owner.HasMessage(s.conv[1], "from the webhook")["id"].(string)
			answer(bea, hooked, "y")
			testkit.Eventually(t, "the webhook's poll counts b's answer on a", func() bool { return count(formOf(a.Owner, hooked), "y") == 1 })
		})
	}
}

package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

// Message links (ruling R73): a link to one of this server's messages
// becomes a `message_link` entity naming it and holding nothing of it.
// Each reader's client fetches the message with getMessage, which answers
// only readers who may see it.

func entitiesOfType(m map[string]any, typ string) []map[string]any {
	var out []map[string]any
	ents, _ := m["entities"].([]any)
	for _, e := range ents {
		if em := e.(map[string]any); em["type"] == typ {
			out = append(out, em)
		}
	}
	return out
}

func TestMessageLinks(t *testing.T) {
	ts := testkit.NewServer(t)
	owner, sam, bob := ts.Owner, ts.Member("sam"), ts.Member("bob")
	secret := owner.Channel("secret", true, sam)
	open := owner.Channel("open", false, sam, bob)
	target := sam.Post(secret, "the plan is ready")
	id := target["id"].(string)
	link := ts.Origin + "/chat/c/" + secret + "/m/" + id

	m := sam.Post(open, "see "+link+" and churr://"+ts.Origin[len("http://"):]+"/chat/c/"+secret+"/t/"+id)
	got := entitiesOfType(m, "message_link")
	if len(got) != 2 {
		t.Fatalf("want two message links, got %v", m["entities"])
	}
	e := got[0]
	if e["message_id"] != id || e["conversation_id"] != secret || e["url"] != link || int(e["at"].(float64)) != 4 {
		t.Fatalf("message link: %v", e)
	}
	if got[1]["message_id"] != id {
		t.Fatalf("churr form: %v", got[1])
	}
	// Nothing of the message is in the entity or the message.
	for k := range e {
		switch k {
		case "type", "at", "len", "url", "conversation_id", "message_id":
		default:
			t.Fatalf("unexpected field %q in %v", k, e)
		}
	}
	if len(arr(m, "cards")) != 0 {
		t.Fatalf("a message link gets no link preview: %v", m["cards"])
	}

	// The preview is the reader's: sam may see the message, bob may not.
	if got := obj(sam.Must(200, "GET", "/api/v1/messages/"+id, nil), "message"); got["text"] != "the plan is ready" {
		t.Fatalf("sam's preview: %v", got)
	}
	bob.Refused(404, model.CodeNotFound, "GET", "/api/v1/messages/"+id, nil)

	// Kept plain, it stays a link; another server's is a link.
	p := sam.Must(201, "POST", convPath(open)+"/messages", map[string]any{"text": link,
		"object_hints": []map[string]any{{"start": 0, "end": len(link), "kind": "plain"}}})["message"].(map[string]any)
	if len(entitiesOfType(p, "message_link")) != 0 || len(entitiesOfType(p, "link")) != 1 {
		t.Fatalf("kept plain: %v", p["entities"])
	}
	o := sam.Post(open, "https://other.example/chat/c/"+secret+"/m/"+id)
	if len(entitiesOfType(o, "message_link")) != 0 {
		t.Fatalf("another server's link: %v", o["entities"])
	}
}

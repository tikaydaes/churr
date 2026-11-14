package api_test

import (
	"testing"

	"github.com/tikaydaes/churr/internal/testkit"
)

// Fix: message.sent is stored. A client that was not connected when the
// time reference acknowledged its server's message learns it on catch-up,
// as a stored event after the message.created that showed it unsent.
func TestMessageSentReachesAClientThatWasAway(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ch := a.Owner.Channel("shared", false)
	_, local := l.Share(0, ch, "shared", nil, 1)
	conv := local[1]

	// b's owner posts with no live connection open; a is the time
	// reference, so the message waits to be acknowledged.
	posted := b.Owner.Post(conv, "said on b")
	id := posted["id"].(string)
	seq := int64(posted["seq"].(float64))
	testkit.Eventually(t, "the message is acknowledged", func() bool {
		return b.Owner.HasMessage(conv, "said on b")["unsent"] != true
	})

	// The client comes back holding everything before the message.
	_, frames := b.Owner.Events(map[string]int64{conv: seq - 1})
	var created, sent map[string]any
	for _, f := range frames {
		switch {
		case f["type"] == "message.created" && obj(f, "message")["id"] == id:
			created = f
		case f["type"] == "message.sent" && f["message_id"] == id:
			sent = f
		}
	}
	if created == nil || sent == nil {
		t.Fatalf("catch-up: created %v, sent %v; frames %v", created, sent, frames)
	}
	if obj(created, "message")["unsent"] != true {
		t.Fatalf("the stored message.created did not show it unsent: %v", created)
	}
	if sent["conversation_id"] != conv || sent["seq"].(float64) <= float64(seq) {
		t.Fatalf("message.sent is not stored after the message: %v", sent)
	}
}

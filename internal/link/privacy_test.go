package link_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/testkit"
)

func textsOf(msgs []map[string]any) string {
	var out []string
	for _, m := range msgs {
		if s, _ := m["text"].(string); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, " | ")
}

// A private channel shared over a direct link (ruling R87): the other
// server is sent nothing from before its people joined, and there too a
// newcomer reads only from their first join.
func TestSharedPrivateChannelKeepsItsHistory(t *testing.T) {
	l := testkit.NewLinked(t, 2, false)
	a, b := l.Servers[0], l.Servers[1]
	ann := a.Member("ann")
	ch := a.Owner.Channel("vault", true, ann)
	ann.Post(ch, "said before sharing")
	_, local := l.Share(0, ch, "vault-here", map[string]any{"history_days": 30}, 1)
	copyID := local[1]

	ann.Post(ch, "said after sharing")
	b.Owner.HasMessage(copyID, "said after sharing")
	if got := textsOf(b.Owner.Messages(copyID)); strings.Contains(got, "before sharing") {
		t.Fatalf("b was sent history from before it joined: %s", got)
	}

	// A newcomer on b reads from their first join there.
	bea := b.Member("bea")
	time.Sleep(5 * time.Millisecond)
	b.Owner.Must(200, "POST", "/api/v1/conversations/"+copyID+"/members", map[string]any{"account_ids": []string{string(bea.ID)}})
	time.Sleep(5 * time.Millisecond)
	ann.Post(ch, "said once bea was in")
	bea.HasMessage(copyID, "said once bea was in")
	if got := textsOf(bea.Messages(copyID)); strings.Contains(got, "after sharing") {
		t.Fatalf("bea reads b's copy from before she joined: %s", got)
	}
	if n := len(bea.Must(200, "GET", "/api/v1/search?q=sharing", nil)["results"].([]any)); n != 0 {
		t.Fatalf("bea finds %d messages from before she joined", n)
	}
}

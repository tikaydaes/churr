package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/model"
)

// LoopbackClient returns an HTTP client that connects to 127.0.0.1 whatever
// host a URL names, keeping the port. Servers in one test process have
// made-up names (a.localhost, hub.localhost) that need not resolve.
func LoopbackClient() *http.Client {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
	}
	// No connection is kept for later: one left open and unused would hold
	// up the shutdown of whichever server it leads to.
	return &http.Client{Transport: &http.Transport{DialContext: dial, DisableKeepAlives: true}}
}

// Linked is two or three servers in one process, linked and ready.
type Linked struct {
	t testing.TB
	// Servers are named a.localhost, b.localhost, c.localhost.
	Servers []*TestServer
	// Peers[i] is server i's peer id: its direct link to the other server,
	// or its connection to the hub.
	Peers []string
	// Hub is the hub they are linked through, or nil for a direct link.
	Hub *TestHub
	// skews are what is added to each server's link clock, in milliseconds.
	skews []*atomic.Int64
}

// Skew sets how far server i's link clock is from the true time. The
// servers it is linked to measure the change within moments.
func (l *Linked) Skew(i int, d time.Duration) { l.skews[i].Store(int64(d / time.Millisecond)) }

// ServerName is the name of the i-th server of NewLinked.
func ServerName(i int) string { return string(rune('a'+i)) + ".localhost" }

// NewLinked starts n servers and links them: two servers directly (the
// first issues the invitation and listens, the second dials), or any number
// through a hub, which a direct link cannot do for more than two.
func NewLinked(t testing.TB, n int, withHub bool) *Linked {
	t.Helper()
	return NewLinkedSkewed(t, n, withHub)
}

// NewLinkedSkewed is NewLinked with servers whose clocks are wrong from the
// start: skews[i] is added to server i's link clock.
func NewLinkedSkewed(t testing.TB, n int, withHub bool, skews ...time.Duration) *Linked {
	t.Helper()
	if !withHub && n != 2 {
		t.Fatal("a direct link joins exactly two servers; three or more need a hub")
	}
	l := &Linked{t: t, Peers: make([]string, n)}
	for i := 0; i < n; i++ {
		l.skews = append(l.skews, &atomic.Int64{})
		if i < len(skews) {
			l.Skew(i, skews[i])
		}
		l.Servers = append(l.Servers, NewServer(t, Options{Hostname: ServerName(i), LinkSkew: l.skews[i]}))
	}
	if withHub {
		l.Hub = NewHub(t)
		for i, ts := range l.Servers {
			peer := ts.Owner.Must(201, "POST", "/api/v1/admin/peers", map[string]any{"invitation": l.Hub.Invite("")})["peer"].(map[string]any)
			l.Peers[i] = peer["id"].(string)
		}
		// Every server has the roster that lists all the others.
		for i, ts := range l.Servers {
			Eventually(t, "the hub's roster reaches "+ServerName(i), func() bool {
				peer := ts.Owner.Must(200, "GET", "/api/v1/admin/peers/"+l.Peers[i], nil)["peer"].(map[string]any)
				servers, _ := peer["servers"].([]any)
				return len(servers) == n-1
			})
		}
		return l
	}
	a, b := l.Servers[0], l.Servers[1]
	made := a.Owner.Must(201, "POST", "/api/v1/admin/peers/invitations", nil)
	l.Peers[0] = made["peer"].(map[string]any)["id"].(string)
	l.Peers[1] = b.Owner.Must(201, "POST", "/api/v1/admin/peers", map[string]any{"invitation": made["invitation"]})["peer"].(map[string]any)["id"].(string)
	Eventually(t, "both peers are active", func() bool {
		pa := a.Owner.Must(200, "GET", "/api/v1/admin/peers/"+l.Peers[0], nil)["peer"].(map[string]any)
		pb := b.Owner.Must(200, "GET", "/api/v1/admin/peers/"+l.Peers[1], nil)["peer"].(map[string]any)
		return pa["state"] == "active" && pb["state"] == "active" && pa["connected_at"] != nil && pb["connected_at"] != nil
	})
	return l
}

// Patience is how long a test waits for something that is on its way
// before it calls it lost. It is long for a fast machine: a CI runner of
// two to four processors runs as many packages at once, each test with
// three or four instances and hubs of its own, and setting a channel up
// across a hub cluster there has taken longer than ten seconds. What never
// comes still fails the test, only later.
const Patience = 30 * time.Second

// Eventually polls until ok reports true, and fails the test if it has not
// within Patience. Things that cross a link take a moment.
func Eventually(t testing.TB, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(Patience)
	for {
		if ok() {
			return
		}
		if time.Now().After(deadline) {
			// What every server of the test held then, so that a wait
			// that runs out on a slow machine says what was stuck.
			t.Fatalf("timed out waiting until %s%s", what, diagnosis(t))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ServerID returns the id server i knows server j by.
func (l *Linked) ServerID(i, j int) string {
	l.t.Helper()
	var id string
	Eventually(l.t, fmt.Sprintf("%s knows %s", ServerName(i), ServerName(j)), func() bool {
		for _, raw := range l.Servers[i].Owner.Must(200, "GET", "/api/v1/admin/servers", nil)["servers"].([]any) {
			sv := raw.(map[string]any)
			if sv["name"] == ServerName(j) {
				id = sv["id"].(string)
			}
		}
		return id != ""
	})
	return id
}

// Share shares a channel of server `from` with the servers `to`, accepts it
// on each as a public channel called name, and waits until every one of
// them is in the stream. It returns the stream id and, by server index, the
// local conversation id on each server.
func (l *Linked) Share(from int, conversation, name string, body map[string]any, to ...int) (stream string, local map[int]string) {
	l.t.Helper()
	req := map[string]any{"conversation_id": conversation, "peer_id": l.Peers[from]}
	for k, v := range body {
		req[k] = v
	}
	if l.Hub != nil {
		var ids []any
		for _, j := range to {
			ids = append(ids, l.ServerID(from, j))
		}
		req["server_ids"] = ids
	}
	owner := l.Servers[from].Owner
	stream = owner.Must(201, "POST", "/api/v1/admin/streams", req)["stream"].(map[string]any)["id"].(string)
	local = map[int]string{from: conversation}
	for _, j := range to {
		local[j] = l.Accept(j, stream, name)
	}
	for _, j := range to {
		l.Joined(j, stream)
	}
	return stream, local
}

// Accept waits for an offer to reach server i and accepts it as a public
// channel. It returns the new local conversation's id.
func (l *Linked) Accept(i int, stream, name string) string {
	l.t.Helper()
	o := l.Servers[i].Owner
	Eventually(l.t, "the offer reaches "+ServerName(i), func() bool { return l.StreamOn(i, stream) != nil })
	out := o.Must(200, "POST", "/api/v1/admin/streams/"+stream+"/accept", map[string]any{"name": name})
	return out["conversation"].(map[string]any)["id"].(string)
}

// Joined waits until server i is in a stream: active, with the key of the
// epoch that began when it joined.
func (l *Linked) Joined(i int, stream string) {
	l.t.Helper()
	Eventually(l.t, ServerName(i)+" is in the stream", func() bool {
		st := l.StreamOn(i, stream)
		return st != nil && st["state"] == "active" && st["key_epoch"].(float64) >= 2
	})
}

// StreamOn returns a stream as server i's owner sees it, or nil.
func (l *Linked) StreamOn(i int, stream string) map[string]any {
	l.t.Helper()
	for _, raw := range l.Servers[i].Owner.Must(200, "GET", "/api/v1/admin/streams", nil)["streams"].([]any) {
		st := raw.(map[string]any)
		if st["id"] == stream {
			return st
		}
	}
	return nil
}

// Messages returns the messages of a conversation as a client sees them,
// oldest first.
func (c *Client) Messages(conversation string) []map[string]any {
	c.t.Helper()
	out := c.Must(200, "GET", "/api/v1/conversations/"+conversation+"/messages?limit=100", nil)
	var msgs []map[string]any
	for _, raw := range out["messages"].([]any) {
		msgs = append(msgs, raw.(map[string]any))
	}
	return msgs
}

// HasMessage waits until a conversation holds a message with this text and
// returns it.
func (c *Client) HasMessage(conversation, text string) map[string]any {
	c.t.Helper()
	var found map[string]any
	Eventually(c.t, fmt.Sprintf("%q arrives", text), func() bool {
		for _, m := range c.Messages(conversation) {
			if m["text"] == text {
				found = m
			}
		}
		return found != nil
	})
	return found
}

// AccountID is the client's account as a model identifier.
func (c *Client) AccountID() model.AccountID { return c.ID }

// Try is Do for a request that may find its server gone, as one made while
// the server restarts does: a failure to get any answer comes back as an
// error and does not fail the test. It may be called from any goroutine.
func (c *Client) Try(method, path string, body any) (int, map[string]any, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
	} else if !c.NoOrigin {
		req.Header.Set("Origin", c.ts.Origin)
	}
	hc := c.http
	if c.Bearer != "" {
		hc = &http.Client{}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out := map[string]any{}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	if len(raw) > 0 && json.Unmarshal(raw, &out) != nil {
		out = map[string]any{"_raw": string(raw)}
	}
	return resp.StatusCode, out, nil
}

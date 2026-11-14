package api_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/tikaydaes/churr/internal/testkit"
)

// TestManyPostsAtOnce posts to one channel from more requests at once than
// the PostgreSQL pool has connections. Each post waits its turn to be fanned
// out; when it waited with its transaction still open, the waiting posts
// held every connection, the fan-out at the front could read nothing, and
// all of them stood still until their requests were given up. Every post
// must be accepted, and a reader must get every message once, in seq order,
// with numbers that add up.
func TestManyPostsAtOnce(t *testing.T) {
	ts := testkit.NewServer(t)
	const posters, each = 6, 8 // 48 at once; the pool has 20 connections
	var who []*testkit.Client
	for i := 0; i < posters; i++ {
		who = append(who, ts.Member(fmt.Sprintf("poster%d", i)))
	}
	reader := ts.Member("reader")
	room := ts.Owner.Channel("busy", false, append([]*testkit.Client{reader}, who...)...)
	// On any activity every message notifies the reader and counts.
	reader.Must(200, "PATCH", "/api/v1/conversations/"+room+"/membership", map[string]any{"mode": "any"})
	stream, _ := reader.Events(nil)
	defer stream.Close()

	// The tokens are made first: once the posts are on their way nothing
	// else must need the server, so that what fails is the posts.
	var scripts []*testkit.Client
	for _, p := range who {
		scripts = append(scripts, p.Token())
	}
	answers := make(chan int, posters*each)
	for _, script := range scripts {
		for k := 0; k < each; k++ {
			go func(k int) {
				status, _ := script.Do("POST", "/api/v1/conversations/"+room+"/messages", map[string]any{"text": fmt.Sprintf("message %d", k)})
				answers <- status
			}(k)
		}
	}
	deadline := time.After(30 * time.Second)
	for i := 0; i < posters*each; i++ {
		select {
		case status := <-answers:
			if status != 201 {
				t.Fatalf("a post was answered %d", status)
			}
		case <-deadline:
			t.Fatalf("only %d of %d posts were answered in 30 seconds: the posts are waiting for each other", i, posters*each)
		}
	}

	// The reader hears of each message once and in order, and the last
	// numbers it is sent count them all.
	var lastSeq float64
	created, unread := 0, float64(0)
	for created < posters*each || unread != posters*each {
		f, ok := stream.Next(10 * time.Second)
		if !ok {
			t.Fatalf("the reader got %d of %d messages and an unread count of %v", created, posters*each, unread)
		}
		switch f["type"] {
		case "message.created":
			seq, _ := f["seq"].(float64)
			if seq <= lastSeq {
				t.Fatalf("message.created arrived out of order: seq %v after %v", seq, lastSeq)
			}
			lastSeq = seq
			created++
			if f["notify"] != "notify" {
				t.Fatalf("a top-level message in a channel on any activity notifies; got %v", f["notify"])
			}
		case "badge.changed":
			if f["conversation_id"] == room {
				unread, _ = f["unread_count"].(float64)
			}
		}
	}
}

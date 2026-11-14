package link_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/testkit"
)

func pngBytes(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < 32; i++ {
		img.Set(i, i, color.RGBA{200, 80, 220, 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func entities(m map[string]any) []map[string]any {
	var out []map[string]any
	for _, e := range arr(m["entities"]) {
		out = append(out, e.(map[string]any))
	}
	return out
}

// Case 47: a custom emoji used on A is drawn on B and stored with A as its
// source. Case 62b: @name@server.name and :name@server.name: typed on B
// resolve to A's account and emoji in a channel shared with A, and stay
// plain text in a channel that is not.
func TestCustomEmojiAndLongNames(t *testing.T) {
	s := share(t, 2, false, nil)
	a, b := s.Servers[0], s.Servers[1]
	status, made := a.Owner.Form("POST", "/api/v1/emoji", testkit.Part{Field: "name", Content: []byte("party")},
		testkit.Part{Field: "file", FileName: "party.png", Content: pngBytes(t)})
	if status != 201 {
		t.Fatalf("making the emoji: %d %v", status, made)
	}
	emojiID := made["emoji"].(map[string]any)["id"].(string)
	s.ann.Post(s.conv[0], "well done :party:")
	got := b.Owner.HasMessage(s.conv[1], "well done :party:")
	ents := entities(got)
	if len(ents) != 1 || ents[0]["type"] != "emoji" || ents[0]["emoji_id"] != emojiID {
		t.Fatalf("the emoji entity on b: %v", ents)
	}
	var onB map[string]any
	testkit.Eventually(t, "the emoji and its image are on b", func() bool {
		for _, raw := range arr(b.Owner.Must(200, "GET", "/api/v1/emoji", nil)["emoji"]) {
			e := raw.(map[string]any)
			if e["id"] == emojiID {
				onB = e
			}
		}
		if onB == nil {
			return false
		}
		f, err := b.Server.Store.Files().Get(ctx, model.FileID(onB["file_id"].(string)))
		return err == nil && f.BlobKey != ""
	})
	if onB["name"] != "party" || onB["server_name"] != "a.localhost" {
		t.Fatalf("the emoji on b: %v", onB)
	}
	// b serves the image itself.
	if st, _, body := b.Owner.Get(onB["url"].(string), nil); st != 200 || len(body) == 0 {
		t.Fatalf("b serves the emoji's image: %d, %d bytes", st, len(body))
	}
	// A reaction with it, from a.
	s.ann.Must(200, "PUT", "/api/v1/messages/"+got["id"].(string)+"/reactions/:party:", nil)
	testkit.Eventually(t, "the custom reaction shows on b under a's name for it", func() bool {
		m := b.Owner.Must(200, "GET", "/api/v1/messages/"+got["id"].(string), nil)["message"].(map[string]any)
		r := arr(m["reactions"])
		return len(r) == 1 && r[0].(map[string]any)["emoji"] == ":party@a.localhost:"
	})

	// 62b, in the shared channel.
	text := "ping @ann@a.localhost with :party@a.localhost:"
	posted := b.Owner.Post(s.conv[1], text)
	sent := b.Owner.HasMessage(s.conv[1], text)
	var account, emoji bool
	for _, e := range entities(sent) {
		account = account || (e["type"] == "account" && e["account_id"] == string(s.ann.ID))
		emoji = emoji || (e["type"] == "emoji" && e["emoji_id"] == emojiID)
	}
	if !account || !emoji {
		t.Fatalf("the long forms did not resolve on b: %v", entities(sent))
	}
	// They arrive on a as ordinary entities naming a's own account and
	// emoji.
	onA := s.ann.HasMessage(s.conv[0], text)
	if onA["id"] != posted["id"] || len(entities(onA)) != 2 {
		t.Fatalf("on a: %v", onA)
	}
	// And in a channel that is not shared with a they stay plain text.
	local := b.Owner.Channel("local-only", false)
	plain := b.Owner.Post(local, text)
	if len(entities(plain)) != 0 {
		t.Fatalf("the long forms resolved in a channel not shared with a: %v", entities(plain))
	}
	// A server that is not in the stream is not a name at all.
	nowhere := b.Owner.Post(s.conv[1], "ping @ann@c.localhost")
	if len(entities(nowhere)) != 0 {
		t.Fatalf("a name on a server not in the stream resolved: %v", entities(nowhere))
	}
}

package api_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/tikaydaes/churr/internal/api"
	"github.com/tikaydaes/churr/internal/testkit"
	"github.com/tikaydaes/churr/internal/wirepb"
)

// seeded is a server with an owner, a member, a channel with a message in
// it, and a token for the owner, the same on every server it is made on.
type seeded struct {
	ts                    *testkit.TestServer
	token                 string
	conv, message, member string
}

func seed(t *testing.T) seeded {
	ts := testkit.NewServer(t)
	sam := ts.Member("sam")
	conv := ts.Owner.Channel("dev", false, sam)
	msg := ts.Owner.Post(conv, "Hello @sam, Zoë 🚀")
	ts.Owner.Must(200, "PUT", "/api/v1/messages/"+msg["id"].(string)+"/reactions/%F0%9F%91%8D", nil)
	return seeded{ts: ts, token: ts.Owner.Token().Bearer, conv: conv, message: msg["id"].(string), member: string(sam.ID)}
}

// path fills an operation's path with things the seeded server has.
func (s seeded) path(op api.Operation) string {
	parts := strings.Split(op.Path, "/")
	for i, p := range parts {
		if !strings.HasPrefix(p, "{") {
			continue
		}
		switch {
		case p == "{message_id}" || parts[i-1] == "messages":
			parts[i] = s.message
		case p == "{account_id}" || parts[i-1] == "accounts" || parts[i-1] == "members":
			parts[i] = s.member
		case p == "{conversation_id}" || parts[i-1] == "conversations":
			parts[i] = s.conv
		case p == "{emoji}":
			parts[i] = "%F0%9F%91%8D"
		case p == "{id}" || strings.HasSuffix(p, "_id}") || p == "{provider}":
			parts[i] = "0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f"
		default:
			parts[i] = "1"
		}
	}
	return strings.Join(parts, "/")
}

var (
	reID     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reTime   = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z$`)
	reOpaque = regexp.MustCompile(`[A-Za-z0-9_-]{20,}|(127\.0\.0\.1|localhost):\d+`)
)

// mask replaces what differs between two servers seeded alike (identifiers,
// times, secrets, the port) and leaves the shape and everything else.
func mask(v any) any {
	switch x := v.(type) {
	case string:
		switch {
		case reID.MatchString(x):
			return "<id>"
		case reTime.MatchString(x):
			return "<time>"
		}
		return reOpaque.ReplaceAllString(x, "<opaque>")
	case []any:
		for i := range x {
			x[i] = mask(x[i])
		}
	case map[string]any:
		for k, e := range x {
			if strings.HasSuffix(k, "_seconds") || strings.HasSuffix(k, "_bytes") || k == "newest" {
				x[k] = "<number>"
			} else {
				x[k] = mask(e)
			}
		}
	}
	return v
}

func masked(t *testing.T, text []byte) string {
	if len(text) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(text, &v); err != nil {
		return "not JSON: " + string(text)
	}
	b, _ := json.Marshal(mask(v))
	return string(b)
}

// last holds the operations that end the session or remove what the others
// need; they are run after everything else.
var last = regexp.MustCompile(`(?i)logout|session|revoke|remove|delete|transfer|leave|ban|archive|lock|purge|export|import|setup|reset`)

// TestProtobufEveryOperation makes the same request of every operation of
// the contract twice, on two servers seeded alike: once in JSON, once in
// Protocol Buffers. The two answers must be the same answer: the same
// status, and the same body once the protobuf one is read back, errors
// included. Most requests here are refused (an empty body, a thing that
// does not exist), which is the point for half of them: an error is an
// answer too.
func TestProtobufEveryOperation(t *testing.T) {
	inJSON, inProto := seed(t), seed(t)
	ops := append([]api.Operation(nil), api.Operations...)
	sort.SliceStable(ops, func(i, j int) bool {
		rank := func(op api.Operation) int {
			switch {
			case last.MatchString(op.ID):
				return 2
			case op.Method == "GET":
				return 0
			}
			return 1
		}
		return rank(ops[i]) < rank(ops[j])
	})
	bodies, protoAnswers, protoBodies, jsonAnswers := 0, 0, 0, 0
	for _, op := range ops {
		if op.ID == "openEvents" {
			continue // the live connection, which is not a request and an answer
		}
		var body []byte
		if op.Method != "GET" && op.Method != "DELETE" && wirepb.HasRequest(op.ID) {
			body = []byte(`{}`)
		}
		// In JSON.
		h := http.Header{"Authorization": {"Bearer " + inJSON.token}}
		if body != nil {
			h.Set("Content-Type", "application/json")
		}
		wantStatus, wantHeader, wantBody := inJSON.ts.Raw(op.Method, inJSON.path(op), h, body)

		// In protobuf.
		h = http.Header{"Authorization": {"Bearer " + inProto.token}, "Accept": {wirepb.MediaType}}
		sent := body
		if body != nil {
			pb, err := wirepb.EncodeRequest(op.ID, body)
			if err != nil {
				t.Errorf("%s: an empty request has no protobuf form: %v", op.ID, err)
				continue
			}
			sent = pb
			h.Set("Content-Type", wirepb.MediaType)
			protoBodies++
		}
		status, header, got := inProto.ts.Raw(op.Method, inProto.path(op), h, sent)
		if status != wantStatus {
			t.Errorf("%s: JSON answers %d, protobuf %d (%s)", op.ID, wantStatus, status, wantBody)
			continue
		}
		isJSON := strings.HasPrefix(wantHeader.Get("Content-Type"), "application/json")
		if !isJSON || len(wantBody) == 0 {
			// A page, a file, a redirect or nothing: passed through as it is.
			if header.Get("Content-Type") != wantHeader.Get("Content-Type") {
				t.Errorf("%s: Content-Type %q became %q", op.ID, wantHeader.Get("Content-Type"), header.Get("Content-Type"))
			}
			continue
		}
		bodies++
		if version, is := wirepb.BodyVersion(header.Get("Content-Type")); is {
			if version != wirepb.SchemaVersion {
				t.Errorf("%s: answered in version %d", op.ID, version)
			}
			protoAnswers++
			text, err := wirepb.DecodeResponse(op.ID, status, got)
			if err != nil {
				t.Errorf("%s: the protobuf answer cannot be read: %v", op.ID, err)
				continue
			}
			got = text
		} else {
			// Allowed, and counted: the schema could not hold the answer.
			jsonAnswers++
			_, why := wirepb.EncodeResponse(op.ID, status, got)
			t.Logf("%s %d was answered in JSON: %v", op.ID, status, why)
		}
		if a, b := masked(t, got), masked(t, wantBody); a != b {
			t.Errorf("%s %d:\nprotobuf %s\n    JSON %s", op.ID, status, a, b)
		}
	}
	t.Logf("%d operations; %d answers with a body, %d of them in protobuf, %d in JSON; %d requests sent as protobuf",
		len(ops), bodies, protoAnswers, jsonAnswers, protoBodies)
	if protoAnswers < 250 || protoBodies < 100 {
		t.Errorf("too little was exercised")
	}
	// The two OAuth answers the contract leaves open (RFC 8414 metadata, an empty revocation).
	if jsonAnswers > 2 {
		t.Errorf("%d answers had no protobuf form", jsonAnswers)
	}
}

// TestProtobufNegotiation covers the headers: what asks for protobuf, what
// does not, and the schema version.
func TestProtobufNegotiation(t *testing.T) {
	s := seed(t)
	get := func(accept string) (int, http.Header, []byte) {
		h := http.Header{"Authorization": {"Bearer " + s.token}}
		if accept != "" {
			h.Set("Accept", accept)
		}
		return s.ts.Raw("GET", "/api/v1/me", h, nil)
	}
	_, plain, want := get("")
	for accept, proto := range map[string]bool{
		"":                               false,
		"application/json":               false,
		"*/*":                            false,
		"application/xml":                false, // unknown: JSON, as before
		"application/x-protobuf;q=0":     false,
		"application/x-protobuf; v=zero": false,
		"garbage;;;,":                    false,
		"application/x-protobuf":         true,
		"application/x-protobuf; v=1":    true,
		"application/x-protobuf; v=7":    true, // a newer client: answered in ours
		"application/json, application/x-protobuf;q=0.9": true,
	} {
		status, h, body := get(accept)
		if status != 200 {
			t.Fatalf("Accept %q: %d", accept, status)
		}
		if !proto {
			if h.Get("Content-Type") != plain.Get("Content-Type") || masked(t, body) != masked(t, want) {
				t.Errorf("Accept %q: not the JSON answer: %s %s", accept, h.Get("Content-Type"), body)
			}
			continue
		}
		if h.Get("Content-Type") != "application/x-protobuf; v=1" || !strings.Contains(h.Get("Vary"), "Accept") {
			t.Errorf("Accept %q: Content-Type %q, Vary %q", accept, h.Get("Content-Type"), h.Get("Vary"))
			continue
		}
		text, err := wirepb.DecodeResponse("getMe", 200, body)
		if err != nil || masked(t, text) != masked(t, want) {
			t.Errorf("Accept %q: %v %s", accept, err, text)
		}
		if h.Get("RateLimit-Limit") == "" {
			t.Errorf("Accept %q: the rate-limit headers are gone", accept)
		}
	}

	// A protobuf body with a JSON answer, and the other way about.
	auth := "Bearer " + s.token
	post := "/api/v1/conversations/" + s.conv + "/messages"
	pb, err := wirepb.EncodeRequest("postMessage", []byte(`{"text":"over protobuf"}`))
	if err != nil {
		t.Fatal(err)
	}
	status, h, body := s.ts.Raw("POST", post, http.Header{"Authorization": {auth}, "Content-Type": {"application/x-protobuf; v=1"}}, pb)
	if status != 201 || !strings.HasPrefix(h.Get("Content-Type"), "application/json") || !strings.Contains(string(body), "over protobuf") {
		t.Errorf("protobuf in, JSON out: %d %s %s", status, h.Get("Content-Type"), body)
	}

	// Errors come back in protobuf too, in the one error shape.
	refused := func(name string, contentType string, sent []byte, code string) {
		status, h, body := s.ts.Raw("POST", post, http.Header{"Authorization": {auth}, "Content-Type": {contentType}, "Accept": {wirepb.MediaType}}, sent)
		text, err := wirepb.DecodeResponse("postMessage", status, body)
		var e struct{ Error struct{ Code string } }
		json.Unmarshal(text, &e)
		if status != 400 || err != nil || e.Error.Code != code || !strings.HasPrefix(h.Get("Content-Type"), wirepb.MediaType) {
			t.Errorf("%s: %d %s %v %s", name, status, h.Get("Content-Type"), err, text)
		}
	}
	refused("bytes that are not protobuf", wirepb.MediaType, []byte{0xff, 0xff, 0xff}, "invalid_request")
	refused("a field the operation does not take", wirepb.MediaType, append(append([]byte(nil), pb...), 0xc8, 0xb2, 0x04, 0x01), "invalid_request")
	refused("a schema version from the future", "application/x-protobuf; v=2", pb, "invalid_request")
	refused("an empty message", wirepb.MediaType, nil, "invalid_request")

	// A protobuf body where the operation takes none.
	status, _, body = s.ts.Raw("PUT", "/api/v1/conversations/"+s.conv+"/pins/"+s.message,
		http.Header{"Authorization": {auth}, "Content-Type": {wirepb.MediaType}}, []byte{})
	if status >= 500 {
		t.Errorf("a protobuf body on an operation without one: %d %s", status, body)
	}
}

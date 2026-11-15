package testkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/tikaydaes/churr/internal/api"
	"github.com/tikaydaes/churr/internal/wirepb"
)

// With CHURR_TEST_WIRE=protobuf every request a test Client makes speaks
// Protocol Buffers to the server (docs/WIRE-PROTOBUF.md): the body is sent
// as the operation's request message where it fits one, protobuf is asked
// for back, and the answer is turned into the JSON the test expects. So
// the whole of the API's tests can be run a second time over protobuf:
//
//	CHURR_TEST_WIRE=protobuf go test ./internal/api/
//
// and passing means every operation they reach answers the same either way.
// CHURR_TEST_WIRE_LOG names a file that is given a line for each answer the
// schema could not hold, which the server then sent as JSON.
var wireProtobuf = os.Getenv("CHURR_TEST_WIRE") == "protobuf"

var wireLog sync.Mutex

// OperationFor finds the operation a method and path belong to.
func OperationFor(method, path string) (api.Operation, bool) {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	parts := strings.Split(path, "/")
	var found api.Operation
	best := -1
	for _, op := range api.Operations {
		if op.Method != method {
			continue
		}
		want := strings.Split(op.Path, "/")
		if len(want) != len(parts) {
			continue
		}
		fixed := 0
		for i := range want {
			if strings.HasPrefix(want[i], "{") {
				continue
			}
			if want[i] != parts[i] {
				fixed = -1
				break
			}
			fixed++
		}
		// The most literal match wins, as the router has it.
		if fixed > best {
			found, best = op, fixed
		}
	}
	return found, best >= 0
}

// protoRequest turns a JSON request into a protobuf one where it can.
func protoRequest(req *http.Request, body []byte) *http.Request {
	op, ok := OperationFor(req.Method, req.URL.Path)
	if !ok {
		return req
	}
	req.Header.Set("Accept", wirepb.ContentType(wirepb.SchemaVersion))
	if body != nil && wirepb.HasRequest(op.ID) {
		// A body a test made wrong on purpose has no protobuf form and
		// goes as the JSON it is.
		if pb, err := wirepb.EncodeRequest(op.ID, body); err == nil {
			req.Body = http.NoBody
			req2, _ := http.NewRequest(req.Method, req.URL.String(), bytes.NewReader(pb))
			req2.Header = req.Header
			req2.Header.Set("Content-Type", wirepb.ContentType(wirepb.SchemaVersion))
			return req2
		}
	}
	return req
}

// protoResponse turns a protobuf answer back into JSON.
func protoResponse(req *http.Request, resp *http.Response, raw []byte) ([]byte, error) {
	op, _ := OperationFor(req.Method, req.URL.Path)
	if _, is := wirepb.BodyVersion(resp.Header.Get("Content-Type")); is {
		return wirepb.DecodeResponse(op.ID, resp.StatusCode, raw)
	}
	if path := os.Getenv("CHURR_TEST_WIRE_LOG"); path != "" && len(raw) > 0 && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		_, why := wirepb.EncodeResponse(op.ID, resp.StatusCode, raw)
		wireLog.Lock()
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "%s %d: %v\n", op.ID, resp.StatusCode, why)
			f.Close()
		}
		wireLog.Unlock()
	}
	return raw, nil
}

// checkFrame holds every frame a test's live connection receives to the
// protobuf schema, in the protobuf mode: the frame must come back from
// protobuf as it went in, and one the schema could not hold (and so
// carried as JSON) is noted in the log file.
func checkFrame(t testing.TB, frame []byte) {
	if !wireProtobuf {
		return
	}
	b, typed, why := wirepb.EncodeFrameJSON(frame)
	back, err := wirepb.DecodeFrameJSON(b)
	if err != nil {
		t.Errorf("a frame did not survive protobuf: %v: %s", err, frame)
		return
	}
	var want, got any
	json.Unmarshal(frame, &want)
	json.Unmarshal(back, &got)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("a frame came back from protobuf changed:\n sent %s\n back %s", frame, back)
	}
	if path := os.Getenv("CHURR_TEST_WIRE_LOG"); !typed && path != "" {
		wireLog.Lock()
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "frame: %v\n", why)
			f.Close()
		}
		wireLog.Unlock()
	}
}

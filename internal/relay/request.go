package relay

import (
	"bytes"
	"encoding/json"
	"mime"
	"strconv"
	"unicode/utf8"
)

// The request of POST /v1/wake (docs/PUSH.md section 5). Nothing but these
// six fields is accepted.

// invalid is the counter for requests that fail the shape check.
const invalid = "invalid"

type wakeRequest struct {
	PushAddress *string `json:"push_address"`
	Platform    *string `json:"platform"`
	EventID     *string `json:"event_id"`
	CollapseKey *string `json:"collapse_key"`
	Priority    *string `json:"priority"`
	// Badge is kept as written, so that "7" in quotes is not taken for 7.
	Badge json.RawMessage `json:"badge"`
}

// charset reports whether s is 1 to max characters, all from the set.
func charset(s string, max int, ok func(c byte) bool) bool {
	if len(s) < 1 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}

func alnum(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// parseWake checks a request body against the strict shape and returns the
// wake-up and its platform, or the error to answer with. platform is ""
// when the request did not get as far as naming one.
func parseWake(contentType string, body []byte) (Wake, string, *apiError) {
	bad := func(msg string) (Wake, string, *apiError) {
		return Wake{}, "", &apiError{Code: "invalid_request", Message: msg}
	}
	if mt, _, err := mime.ParseMediaType(contentType); err != nil || mt != "application/json" {
		return bad("The request must be application/json.")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	// An unknown field is refused, so a server cannot send text by
	// mistake.
	dec.DisallowUnknownFields()
	var in wakeRequest
	if err := dec.Decode(&in); err != nil {
		return bad("The request must be one JSON object with only push_address, platform, event_id, collapse_key, priority and badge.")
	}
	if dec.More() {
		return bad("The request must be one JSON object.")
	}
	if in.PushAddress == nil || in.Platform == nil || in.EventID == nil || in.CollapseKey == nil || in.Priority == nil {
		return bad("push_address, platform, event_id, collapse_key and priority are needed.")
	}
	platform := ""
	if *in.Platform == "ios" || *in.Platform == "android" {
		platform = *in.Platform
	}
	field := func(name, msg string) (Wake, string, *apiError) {
		return Wake{}, platform, &apiError{Code: "invalid_value", Message: msg, Field: name}
	}
	if platform == "" {
		return field("platform", "platform is ios or android.")
	}
	if n := utf8.RuneCountInString(*in.PushAddress); n < 1 || n > 4096 || !utf8.ValidString(*in.PushAddress) {
		return field("push_address", "push_address is 1 to 4,096 characters.")
	}
	if !charset(*in.EventID, 256, func(c byte) bool { return alnum(c) || c == '_' || c == '.' || c == '-' }) {
		return field("event_id", "event_id is 1 to 256 characters from A-Z, a-z, 0-9, _, . and -.")
	}
	if !charset(*in.CollapseKey, 64, func(c byte) bool { return alnum(c) || c == '_' || c == '-' }) {
		return field("collapse_key", "collapse_key is 1 to 64 characters from A-Z, a-z, 0-9, _ and -.")
	}
	w := Wake{PushAddress: *in.PushAddress, EventID: *in.EventID, CollapseKey: *in.CollapseKey}
	switch *in.Priority {
	case "high":
		w.High = true
	case "normal":
	default:
		return field("priority", "priority is high or normal.")
	}
	if raw := string(in.Badge); raw != "" && raw != "null" {
		// A whole number from 0 to 99,999, written as one: digits only.
		if !charset(raw, 5, func(c byte) bool { return c >= '0' && c <= '9' }) {
			return field("badge", "badge is a whole number from 0 to 99,999.")
		}
		b, _ := strconv.Atoi(raw)
		w.Badge = &b
	}
	return w, platform, nil
}

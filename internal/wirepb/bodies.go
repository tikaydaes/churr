package wirepb

import (
	"encoding/json"
	"fmt"
)

// HasRequest reports whether an operation takes a JSON body that has a
// protobuf message.
func HasRequest(op string) bool {
	o, known := operations[op]
	return known && o.req >= 0
}

// responseMessage is the message for an operation's answer with a status:
// the one the contract gives, and the error shape for any other failure.
func responseMessage(op string, status int) (int, bool) {
	o, known := operations[op]
	if mi, ok := o.res[status]; known && ok {
		return mi, true
	}
	if status >= 400 {
		return errorMessage, true
	}
	return 0, false
}

// EncodeResponse writes the JSON body an operation answered with as
// protobuf. An error means the body has no exact protobuf form (the reason
// is in it) and is to be sent as the JSON it is.
func EncodeResponse(op string, status int, text []byte) ([]byte, error) {
	mi, ok := responseMessage(op, status)
	if !ok {
		return nil, fmt.Errorf("wirepb: %s has no message for status %d", op, status)
	}
	return encodeBody(mi, text)
}

// DecodeResponse reads a protobuf answer back into its JSON.
func DecodeResponse(op string, status int, b []byte) ([]byte, error) {
	mi, ok := responseMessage(op, status)
	if !ok {
		return nil, fmt.Errorf("wirepb: %s has no message for status %d", op, status)
	}
	return decodeBody(mi, b, false)
}

// EncodeRequest writes an operation's JSON request body as protobuf, for
// clients and tests. An error means it must be sent as JSON.
func EncodeRequest(op string, text []byte) ([]byte, error) {
	o, known := operations[op]
	if !known || o.req < 0 {
		return nil, fmt.Errorf("wirepb: %s takes no body with a message", op)
	}
	return encodeBody(o.req, text)
}

// DecodeRequest reads an operation's protobuf request body into the JSON
// its handler reads. It is strict: a field the schema does not have is
// ErrUnknownField, as an unknown JSON field is refused.
func DecodeRequest(op string, b []byte) ([]byte, error) {
	o, known := operations[op]
	if !known || o.req < 0 {
		return nil, fmt.Errorf("wirepb: %s takes no body with a message", op)
	}
	return decodeBody(o.req, b, true)
}

func encodeBody(mi int, text []byte) ([]byte, error) {
	v, err := parseJSON(text)
	if err != nil {
		return nil, err
	}
	b, err := appendMessage(make([]byte, 0, len(text)), mi, v)
	if err != nil {
		return nil, at(messages[mi].name, err)
	}
	return b, nil
}

func decodeBody(mi int, b []byte, strict bool) ([]byte, error) {
	v, err := reader{strict: strict}.readMessage(mi, b)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

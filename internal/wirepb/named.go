package wirepb

import (
	"fmt"
	"sync"
)

// This file lets another schema carry this one's objects. The link protocol
// (proto/churr/link/v1, docs/LINK-PROTOCOL.md) has its own messages for what
// only servers and hubs say to each other, and uses this schema's messages
// for the parts of an event that are the same as what a client is sent: a
// quote, a card, a form. It names the message and hands over the JSON value.

var (
	byNameOnce sync.Once
	byName     map[string]int
)

func messageIndex(name string) (int, bool) {
	byNameOnce.Do(func() {
		byName = make(map[string]int, len(messages))
		for i := range messages {
			byName[messages[i].name] = i
		}
	})
	mi, ok := byName[name]
	return mi, ok
}

// HasMessage reports whether the schema has a message with this name.
func HasMessage(name string) bool {
	_, ok := messageIndex(name)
	return ok
}

// AppendNamed writes the content of the named message for a JSON value that
// has already been parsed (objects as map[string]any, whole numbers as
// json.Number, int64 or int). Like every encoder here it is exact or it
// refuses: the error says what has no place in the schema.
func AppendNamed(b []byte, name string, v any) ([]byte, error) {
	mi, ok := messageIndex(name)
	if !ok {
		return nil, fmt.Errorf("wirepb: the schema has no message %s", name)
	}
	out, err := appendMessage(b, mi, v)
	if err != nil {
		return nil, at(name, err)
	}
	return out, nil
}

// ReadNamed reads the content of the named message back into a JSON value.
// Fields this schema does not know are skipped.
func ReadNamed(name string, b []byte) (any, error) {
	mi, ok := messageIndex(name)
	if !ok {
		return nil, fmt.Errorf("wirepb: the schema has no message %s", name)
	}
	return reader{}.readMessage(mi, b)
}

// AppendValue writes the content of a Value message: any JSON value.
func AppendValue(b []byte, v any) ([]byte, error) { return appendValue(b, v) }

// ReadValue reads the content of a Value message.
func ReadValue(b []byte) (any, error) { return readValue(b) }

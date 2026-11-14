package model

import (
	"encoding/json"
	"fmt"
)

// Event names used by the foundation. Every name is `thing.happened`
// (docs/EVENTS.md lists them all; feature packages add theirs beside these).
const (
	EventMessageCreated      = "message.created"
	EventMessageEdited       = "message.edited"
	EventMessageDeleted      = "message.deleted"
	EventMessagePurged       = "message.purged"
	EventReactionAdded       = "reaction.added"
	EventReactionRemoved     = "reaction.removed"
	EventTaskChanged         = "task.changed"
	EventMemberJoined        = "member.joined"
	EventMemberLeft          = "member.left"
	EventMemberLevelChanged  = "member.level_changed"
	EventChannelGroupAdded   = "channel_group.added"
	EventChannelGroupChanged = "channel_group.changed"
	EventChannelGroupRemoved = "channel_group.removed"
	EventConversationUpdated = "conversation.updated"
	EventPinAdded            = "pin.added"
	EventPinRemoved          = "pin.removed"
	EventReadPositionMoved   = "read_position.moved"
	EventMembershipUpdated   = "membership.updated"
	EventThreadStateChanged  = "thread_state.changed"
	EventAccountUpdated      = "account.updated"
	EventPresenceChanged     = "presence.changed"
)

// Event is one thing that happened, as the live connection and the catch-up
// log carry it: `type` and `at`, `conversation_id` and `seq` when it belongs
// to a conversation, and the fields of Payload beside them at the top level.
//
// Payload holds Go values when a service builds the event and plain JSON
// values (maps, slices, strings, float64) after it has been stored and read
// back. Code that looks inside a payload, such as a live.Tailor, must work on
// the JSON form; Normalize gives it.
type Event struct {
	Type           string
	At             Time
	ConversationID ConversationID
	Seq            int64
	Payload        map[string]any

	// MessageID is the message this event carries, if any. It fills
	// events.message_id so erasing a message can find the rows; it is not
	// part of what is sent.
	MessageID MessageID

	// Only holds extra top-level fields for single accounts, known when
	// the event is published and never stored: the author's client_id on
	// message.created, the conversation on member.joined for the joiner.
	Only map[AccountID]map[string]any

	// AlsoTo names accounts that receive this event although they are not
	// (or no longer) among the conversation's recipients, such as the
	// account a member.left is about.
	AlsoTo []AccountID
}

// reservedEventKeys may not be used as payload fields.
var reservedEventKeys = map[string]bool{"type": true, "at": true, "conversation_id": true, "seq": true}

// MarshalJSON writes the event as one flat object.
func (e Event) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(e.Payload)+4)
	for k, v := range e.Payload {
		if reservedEventKeys[k] {
			return nil, fmt.Errorf("event payload may not use the field %q", k)
		}
		out[k] = v
	}
	out["type"] = e.Type
	out["at"] = e.At
	if e.ConversationID != "" {
		out["conversation_id"] = e.ConversationID
	}
	if e.Seq != 0 {
		out["seq"] = e.Seq
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads the flat object back. Payload then holds plain JSON
// values.
func (e *Event) UnmarshalJSON(b []byte) error {
	var head struct {
		Type           string         `json:"type"`
		At             Time           `json:"at"`
		ConversationID ConversationID `json:"conversation_id"`
		Seq            int64          `json:"seq"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	var all map[string]any
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	for k := range reservedEventKeys {
		delete(all, k)
	}
	*e = Event{Type: head.Type, At: head.At, ConversationID: head.ConversationID, Seq: head.Seq, Payload: all}
	return nil
}

// Normalize returns the event with its payload in plain JSON form, and the
// bytes it marshals to. MessageID, Only and AlsoTo are carried over.
func (e Event) Normalize() (Event, []byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return Event{}, nil, err
	}
	var n Event
	if err := json.Unmarshal(b, &n); err != nil {
		return Event{}, nil, err
	}
	n.MessageID, n.Only, n.AlsoTo = e.MessageID, e.Only, e.AlsoTo
	return n, b, nil
}

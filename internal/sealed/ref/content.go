package ref

import (
	"encoding/base64"
	"encoding/json"
)

// What is inside a sealed message (docs/SEALED.md section 8.3,
// docs/MESSAGE-FORMAT.md section 9): a JSON object the two people's devices
// understand. It is encrypted and signed as bytes by its one author and
// only ever parsed by others, so it needs no canonical form; the functions
// here simply give the reference one fixed form.
//
// Every reference to another message holds that message's reference (the
// identifier its sender made, also its client_id), never the identifier
// the server gave its row. Bytes are base64url without padding.

// The kinds of content. Every kind but KindMessage is sent quiet.
const (
	KindMessage  = "message"
	KindEdit     = "edit"
	KindDelete   = "delete"
	KindReaction = "reaction"
	KindTask     = "task"
	KindHistory  = "history"
)

// B64 writes bytes the way JSON carries them.
func B64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// UnB64 reads them back; nil if the text is not base64url.
func UnB64(s string) []byte {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

// FileThumb is the encrypted thumbnail of an image or video.
type FileThumb struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// FileEntry is a file inside a sealed message. It carries what the server
// was not given: the key, the real name and type, and the thumbnail.
type FileEntry struct {
	ID        string     `json:"id"`
	Key       string     `json:"key"`
	Name      string     `json:"name"`
	MediaType string     `json:"media_type"`
	Kind      string     `json:"kind"`
	Size      int64      `json:"size"`
	Width     int        `json:"width,omitempty"`
	Height    int        `json:"height,omitempty"`
	Caption   string     `json:"caption,omitempty"`
	Thumb     *FileThumb `json:"thumb,omitempty"`
}

// Quote is what a reply quotes: the quoted message's reference and, for
// part of it, a range in UTF-16 code units.
type Quote struct {
	MessageID string `json:"message_id"`
	Start     *int   `json:"start,omitempty"`
	End       *int   `json:"end,omitempty"`
}

// HistorySecretEntry is one epoch's history secret in a `history` message.
type HistorySecretEntry struct {
	KeyEpoch uint64 `json:"key_epoch"`
	Secret   string `json:"secret"`
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func refOrNil(u *UUID) any {
	if u == nil {
		return nil
	}
	return u.String()
}

// MessageContent is a `message`: text in Churr Markdown with the entities
// the sending device worked out, files, and the thread link.
func MessageContent(text string, entities []json.RawMessage, files []FileEntry, parent *UUID, quote *Quote) []byte {
	if entities == nil {
		entities = []json.RawMessage{}
	}
	if files == nil {
		files = []FileEntry{}
	}
	return mustJSON(struct {
		Kind     string            `json:"kind"`
		Text     string            `json:"text"`
		Entities []json.RawMessage `json:"entities"`
		Files    []FileEntry       `json:"files"`
		ParentID any               `json:"parent_id"`
		Quote    *Quote            `json:"quote"`
	}{KindMessage, text, entities, files, refOrNil(parent), quote})
}

// EditContent is an `edit`: new text and entities for a message the sender
// wrote. Its files stay.
func EditContent(target UUID, text string, entities []json.RawMessage) []byte {
	if entities == nil {
		entities = []json.RawMessage{}
	}
	return mustJSON(struct {
		Kind      string            `json:"kind"`
		MessageID string            `json:"message_id"`
		Text      string            `json:"text"`
		Entities  []json.RawMessage `json:"entities"`
	}{KindEdit, target.String(), text, entities})
}

// DeleteContent is a `delete` of a message the sender wrote.
func DeleteContent(target UUID) []byte {
	return mustJSON(struct {
		Kind      string `json:"kind"`
		MessageID string `json:"message_id"`
	}{KindDelete, target.String()})
}

// ReactionContent is a `reaction`: one emoji put on or taken off a message.
func ReactionContent(target UUID, emoji string, on bool) []byte {
	return mustJSON(struct {
		Kind      string `json:"kind"`
		MessageID string `json:"message_id"`
		Emoji     string `json:"emoji"`
		On        bool   `json:"on"`
	}{KindReaction, target.String(), emoji, on})
}

// TaskContent is a `task`: one checklist item ticked or unticked.
func TaskContent(target UUID, item string, checked bool) []byte {
	return mustJSON(struct {
		Kind      string `json:"kind"`
		MessageID string `json:"message_id"`
		ItemID    string `json:"item_id"`
		Checked   bool   `json:"checked"`
	}{KindTask, target.String(), item, checked})
}

// HistoryContent is a `history`: history secrets for epochs the other
// account had no device in (section 12.5). It is never drawn.
func HistoryContent(secrets []HistorySecretEntry) []byte {
	return mustJSON(struct {
		Kind    string               `json:"kind"`
		Secrets []HistorySecretEntry `json:"secrets"`
	}{KindHistory, secrets})
}

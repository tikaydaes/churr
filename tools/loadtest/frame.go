package main

import (
	"bytes"
)

// Reading a frame without decoding it. A generator takes a hundred thousand
// frames a second, and of each it wants four things: its type, its
// conversation, its sequence number, and, of a message this test posted,
// when and by which generator. One pass over the bytes finds the first
// three among the top-level fields; nothing is copied and nothing allocated.

// marker starts the text of every message the test posts. After it come the
// moment the message was sent, in nanoseconds, and the number of the
// generator that sent it.
const marker = "loadtest "

var (
	typeMessageCreated = []byte("message.created")
	typeReady          = []byte("ready")
	// typeConversationChanged is what a connection that is not showing a
	// lazily delivered conversation is sent in place of its events.
	typeConversationChanged = []byte("conversation.changed")
	textMarker              = []byte(`"text":"` + marker)
)

// frameHead is what scanFrame finds. The slices point into the frame.
type frameHead struct {
	typ  []byte // the value of "type", without its quotes
	conv []byte // the value of "conversation_id", without its quotes
	seq  int64  // the value of "seq", or 0
	// message is the raw value of "message", or nil.
	message []byte
}

// scanFrame walks the top-level fields of a JSON object. It reports false
// for anything it cannot follow, and the frame is then counted as unread
// and otherwise ignored.
func scanFrame(b []byte) (h frameHead, ok bool) {
	i := skipSpace(b, 0)
	if i >= len(b) || b[i] != '{' {
		return h, false
	}
	i = skipSpace(b, i+1)
	if i < len(b) && b[i] == '}' {
		return h, true
	}
	for {
		if i >= len(b) || b[i] != '"' {
			return h, false
		}
		keyEnd := stringEnd(b, i)
		if keyEnd < 0 {
			return h, false
		}
		key := b[i+1 : keyEnd-1]
		i = skipSpace(b, keyEnd)
		if i >= len(b) || b[i] != ':' {
			return h, false
		}
		i = skipSpace(b, i+1)
		end := valueEnd(b, i)
		if end < 0 {
			return h, false
		}
		val := b[i:end]
		switch string(key) { // compared in place; no string is made
		case "type":
			h.typ = unquoted(val)
		case "conversation_id":
			h.conv = unquoted(val)
		case "seq":
			h.seq = digits(val)
		case "message":
			h.message = val
		}
		i = skipSpace(b, end)
		if i >= len(b) {
			return h, false
		}
		if b[i] == '}' {
			return h, true
		}
		if b[i] != ',' {
			return h, false
		}
		i = skipSpace(b, i+1)
	}
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\n' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	return i
}

// stringEnd is the place just after the string that starts at b[i], or -1.
func stringEnd(b []byte, i int) int {
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return -1
}

// valueEnd is the place just after the JSON value that starts at b[i], or -1.
func valueEnd(b []byte, i int) int {
	if i >= len(b) {
		return -1
	}
	switch b[i] {
	case '"':
		return stringEnd(b, i)
	case '{', '[':
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				i = stringEnd(b, i)
				if i < 0 {
					return -1
				}
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return -1
	}
	// A number, true, false or null runs to the next comma or brace.
	for j := i; j < len(b); j++ {
		switch b[j] {
		case ',', '}', ']', ' ', '\n', '\t', '\r':
			if j == i {
				return -1
			}
			return j
		}
	}
	return -1
}

func unquoted(val []byte) []byte {
	if len(val) >= 2 && val[0] == '"' {
		return val[1 : len(val)-1]
	}
	return nil
}

// digits reads a whole number that is not negative, or gives 0.
func digits(val []byte) int64 {
	var n int64
	if len(val) == 0 || len(val) > 19 {
		return 0
	}
	for _, c := range val {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// postText is the text of a message the test posts.
func postText(buf []byte, sentNanos int64, shard int, from string) []byte {
	buf = append(buf, marker...)
	buf = appendInt(buf, sentNanos)
	buf = append(buf, ' ')
	buf = appendInt(buf, int64(shard))
	buf = append(buf, " from "...)
	return append(buf, from...)
}

func appendInt(buf []byte, n int64) []byte {
	var tmp [20]byte
	i := len(tmp)
	for {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	return append(buf, tmp[i:]...)
}

// sentAt reads, from the "message" of a message.created frame, the moment
// the message was posted and the generator that posted it. It reports false
// for a message this test did not post.
func sentAt(message []byte) (nanos int64, shard int, ok bool) {
	i := bytes.Index(message, textMarker)
	if i < 0 {
		return 0, 0, false
	}
	rest := message[i+len(textMarker):]
	end := bytes.IndexByte(rest, ' ')
	if end <= 0 {
		return 0, 0, false
	}
	nanos = digits(rest[:end])
	rest = rest[end+1:]
	end = bytes.IndexByte(rest, ' ')
	if end <= 0 || nanos == 0 {
		return 0, 0, false
	}
	return nanos, int(digits(rest[:end])), true
}

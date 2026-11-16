package chat

import "github.com/tikaydaes/churr/internal/markdown"

// Message links (ruling R73, docs/MESSAGE-FORMAT.md section 3.2): a link to
// a message of this server, as "Copy link" writes it, becomes a
// `message_link` entity naming the conversation and the message. The
// entity holds nothing of the message: each reader's client fetches it
// with getMessage, which answers only a reader who may see it.

// SetOrigin gives chat the server's own origin ("https://chat.example"),
// which a link to one of its messages starts with. Without it no message
// link is found.
func (s *Service) SetOrigin(origin string) { s.origin = origin }

// Origin implements markdown.OriginSource.
func (r *resolver) Origin() string { return r.origin }

// ownMessageLink reports whether a link names a message of this server,
// which gets no link preview: clients draw it from the message itself.
func (s *Service) ownMessageLink(link string) bool {
	_, _, ok := markdown.MessageLink(link, s.origin)
	return ok
}

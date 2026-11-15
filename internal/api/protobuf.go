package api

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/tikaydaes/churr/internal/wirepb"
)

// Protocol Buffers on the HTTP API (ruling R65, docs/WIRE-PROTOBUF.md).
//
// JSON is the default and nothing here touches a request that does not ask
// for anything else. A client that sends `Accept: application/x-protobuf`
// is answered in protobuf, errors included, and one that sends
// `Content-Type: application/x-protobuf` may send its body so. Any client
// may ask: an app, a bot, a script.
//
// It is one layer around every handler and no handler knows of it. A
// handler reads and writes JSON as it always has; this file turns a
// protobuf body into that JSON before the handler reads it, and the JSON
// answer into protobuf after the handler wrote it, by the operation's
// messages in the generated schema. An answer the schema cannot hold
// exactly (it has fallen behind a handler) is sent as the JSON it is, with
// JSON's Content-Type, so the client is never given less than it asked for.

// negotiate looks at a request's Accept and Content-Type. It returns the
// writer and request the handler is to use, and what to call when the
// handler has returned.
func (a *API) negotiate(op Operation, w http.ResponseWriter, r *http.Request) (http.ResponseWriter, func()) {
	done := func() {}
	// A WebSocket upgrade takes the connection over; it has no body to turn.
	if r.Header.Get("Upgrade") != "" {
		return w, done
	}
	if _, is := wirepb.BodyVersion(r.Header.Get("Content-Type")); is {
		r.Body = &protoBody{src: r.Body, op: op.ID, contentType: r.Header.Get("Content-Type"), large: largeBody[op.ID]}
		r.Header.Set("Content-Type", "application/json")
		r.ContentLength = -1
	}
	if version, ok := wirepb.Accepts(r.Header.Get("Accept")); ok && version > 0 {
		pw := &protoWriter{ResponseWriter: w, op: op.ID, version: version, log: a.Log}
		return pw, pw.finish
	}
	return w, done
}

// protoBody is a protobuf request body read as the JSON the handler
// expects. Nothing is read until the handler reads, so a request is
// authenticated and limited before its body is looked at, as with JSON.
type protoBody struct {
	src         io.ReadCloser
	op          string
	contentType string
	large       bool
	json        *bytes.Reader
	err         error
}

// errProtoBody is what a handler's Decode reports for a protobuf body that
// cannot be read. Its text is shown to the client.
type errProtoBody struct{ why string }

func (e errProtoBody) Error() string { return e.why }

func (b *protoBody) Read(p []byte) (int, error) {
	if b.json == nil && b.err == nil {
		b.err = b.load()
	}
	if b.err != nil {
		return 0, b.err
	}
	return b.json.Read(p)
}

func (b *protoBody) load() error {
	version, _ := wirepb.BodyVersion(b.contentType)
	if version < 1 || version > wirepb.SchemaVersion {
		return errProtoBody{"this server reads protobuf schema version 1 to " + strconv.Itoa(wirepb.SchemaVersion)}
	}
	if !wirepb.HasRequest(b.op) {
		return errProtoBody{"this operation takes no protobuf body"}
	}
	src := io.Reader(b.src)
	if !b.large {
		src = io.LimitReader(src, maxBody+1)
	}
	raw, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	if !b.large && len(raw) > maxBody {
		return &http.MaxBytesError{Limit: maxBody}
	}
	text, err := wirepb.DecodeRequest(b.op, raw)
	switch {
	case errors.Is(err, wirepb.ErrUnknownField):
		return errProtoBody{"the protobuf body has a field this operation does not take"}
	case err != nil:
		return errProtoBody{"the protobuf body is not the operation's request message"}
	}
	b.json = bytes.NewReader(text)
	return nil
}

func (b *protoBody) Close() error { return b.src.Close() }

// protoWriter holds a handler's JSON answer back until the handler has
// returned, then sends it as protobuf. Anything that is not JSON (a file,
// a page, a redirect, an answer with no body) goes straight through.
type protoWriter struct {
	http.ResponseWriter
	op      string
	version int
	log     *slog.Logger

	started bool
	held    bool // the answer is JSON and is being kept in body
	status  int
	body    bytes.Buffer
}

func (w *protoWriter) WriteHeader(status int) {
	if w.started {
		return
	}
	w.started, w.status = true, status
	if strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		w.held = true
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *protoWriter) Write(p []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	if w.held {
		return w.body.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

// Flush lets a handler that streams something else keep doing so.
func (w *protoWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok && !w.held {
		f.Flush()
	}
}

// Unwrap gives http.ResponseController the writer underneath.
func (w *protoWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *protoWriter) finish() {
	if !w.held {
		return
	}
	body := w.body.Bytes()
	h := w.Header()
	if pb, err := wirepb.EncodeResponse(w.op, w.status, body); err == nil {
		h.Set("Content-Type", wirepb.ContentType(w.version))
		h.Add("Vary", "Accept")
		h.Del("Content-Length")
		body = pb
	} else if w.log != nil {
		// The contract has fallen behind the handler. The client gets the
		// JSON; someone should bring the schema up to date.
		w.log.Warn("an answer has no protobuf form and was sent as JSON", "operation", w.op, "status", w.status, "why", err)
	}
	w.ResponseWriter.WriteHeader(w.status)
	w.ResponseWriter.Write(body)
}

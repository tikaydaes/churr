package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/tikaydaes/churr/internal/auth"
	"github.com/tikaydaes/churr/internal/model"
	"github.com/tikaydaes/churr/internal/store"
)

// Request is one request as a handler sees it: who is asking, and helpers
// to read the request and write the answer in the shapes of
// docs/CONVENTIONS.md.
type Request struct {
	W  http.ResponseWriter
	R  *http.Request
	Op Operation
	// Who is the session and account, when SignedIn is true.
	Who      auth.Who
	SignedIn bool
	// Addr is the client's address, for limits.
	Addr string
	api  *API
}

// Ctx is the request's context.
func (r *Request) Ctx() context.Context { return r.R.Context() }

// Actor is the account making the request.
func (r *Request) Actor() model.Account { return r.Who.Account }

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// JSON writes a successful answer with the status the contract gives the
// operation.
func (r *Request) JSON(v any) error { return r.JSONStatus(r.Op.Success, v) }

// JSONStatus writes an answer with another status, for the few operations
// that have two (200 for found, 201 for made).
func (r *Request) JSONStatus(status int, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	r.W.WriteHeader(status)
	r.W.Write(b)
	return nil
}

// NoContent writes the answer of an operation that returns nothing.
func (r *Request) NoContent() error {
	r.W.WriteHeader(http.StatusNoContent)
	return nil
}

// Decode reads the JSON body into v. Unknown fields, trailing data and a
// body that is not one JSON value are refused with invalid_request, as the
// conventions require of servers.
func (r *Request) Decode(v any) error {
	dec := json.NewDecoder(r.R.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		var proto errProtoBody
		switch {
		case errors.As(err, &proto):
			return model.BadRequest("The request body could not be read: " + proto.why + ".")
		case errors.As(err, &tooBig):
			return model.ErrTooLarge.With("The request body is too large.")
		case errors.Is(err, io.EOF):
			return model.BadRequest("This request needs a JSON body.")
		}
		return model.BadRequest("The request body could not be read: " + plain(err))
	}
	if dec.More() {
		return model.BadRequest("The request body must be one JSON object.")
	}
	return nil
}

// plain makes a decoding error fit to show: the standard library's wording
// with its package prefix taken off.
func plain(err error) string {
	s := err.Error()
	for _, prefix := range []string{"json: ", "model: "} {
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			s = s[len(prefix):]
		}
	}
	return s + "."
}

// PathID reads a path parameter that must be an identifier. One that is not
// could name nothing, so the answer is not_found, the same as for an
// identifier nothing has.
func PathID[T ~string](r *Request, name string) (T, error) {
	v := r.R.PathValue(name)
	if !model.ValidID(v) {
		return "", model.ErrNotFound
	}
	return T(v), nil
}

// BodyID checks an identifier that arrived in a body or query field.
func BodyID[T ~string](field, v string) (T, error) {
	if !model.ValidID(v) {
		return "", model.Invalid(field, "That is not an identifier.")
	}
	return T(v), nil
}

// queryInt reads a whole-number query parameter. found is false if absent.
func (r *Request) queryInt(name string) (v int64, found bool, err error) {
	s := r.R.URL.Query().Get(name)
	if s == "" {
		return 0, false, nil
	}
	v, perr := strconv.ParseInt(s, 10, 64)
	if perr != nil {
		return 0, false, model.Invalid(name, "That must be a whole number.")
	}
	return v, true, nil
}

// queryBool reads a true-or-false query parameter; absent is false.
func (r *Request) queryBool(name string) (bool, error) {
	switch r.R.URL.Query().Get(name) {
	case "", "false", "0":
		return false, nil
	case "true", "1":
		return true, nil
	}
	return false, model.Invalid(name, "That must be true or false.")
}

// queryTime reads an RFC 3339 time query parameter; absent is the zero time.
func (r *Request) queryTime(name string) (model.Time, error) {
	s := r.R.URL.Query().Get(name)
	if s == "" {
		return 0, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0, model.Invalid(name, "That must be an RFC 3339 time.")
	}
	return model.FromTime(t), nil
}

// limit reads `limit`: 50 by default, capped at 200.
func (r *Request) limit() (int, error) {
	v, found, err := r.queryInt("limit")
	if err != nil || !found {
		return model.DefaultLimit, err
	}
	if v < 1 {
		return 0, model.Invalid("limit", "limit is at least 1.")
	}
	if v > model.MaxLimit {
		v = model.MaxLimit
	}
	return int(v), nil
}

// Page reads `limit` and `cursor`, for lists that are not ordered by seq.
func (r *Request) Page() (store.Page, error) {
	l, err := r.limit()
	return store.Page{Limit: l, Cursor: r.R.URL.Query().Get("cursor")}, err
}

// SeqPage reads `before`, `after` and `limit`, for lists ordered by seq.
func (r *Request) SeqPage() (store.SeqPage, error) {
	var p store.SeqPage
	var err error
	if p.Limit, err = r.limit(); err != nil {
		return p, err
	}
	if p.Before, _, err = r.queryInt("before"); err != nil {
		return p, err
	}
	if p.After, _, err = r.queryInt("after"); err != nil {
		return p, err
	}
	if p.Before < 0 || p.After < 0 {
		return p, model.Invalid("before", "before and after are 0 or more.")
	}
	return p, nil
}

// list is the answer of a cursor-paged list: the items under their name,
// and next_cursor when there is more.
func list(name string, items any, next string) map[string]any {
	out := map[string]any{name: items}
	if next != "" {
		out["next_cursor"] = next
	}
	return out
}

// orEmpty makes a nil slice an empty one, so a list is [] and never null.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// writeError writes any error in the one error shape. An error that is not
// a model.Error is a failure of the server: it is logged, and the client is
// told only that.
func writeError(w http.ResponseWriter, log *slog.Logger, err error) {
	e, ok := model.AsError(err)
	if !ok {
		log.Error("request failed", "err", err)
		e = model.ErrInternal
	}
	if e.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(e.RetryAfterSeconds))
	}
	b, _ := json.Marshal(map[string]any{"error": e})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status())
	w.Write(b)
}

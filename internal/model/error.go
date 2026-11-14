package model

import (
	"errors"
	"net/http"
)

// Code is what a client acts on when a request fails (docs/CONVENTIONS.md).
type Code string

// Every error code the API can return.
const (
	CodeInvalidRequest       Code = "invalid_request"
	CodeInvalidValue         Code = "invalid_value"
	CodeUnauthenticated      Code = "unauthenticated"
	CodeSecondFactorRequired Code = "second_factor_required"
	CodeForbidden            Code = "forbidden"
	CodeTimedOut             Code = "timed_out"
	CodeArchived             Code = "archived"
	CodeLocked               Code = "locked"
	CodeFeatureOff           Code = "feature_off"
	CodeFreshCheckRequired   Code = "fresh_check_required"
	CodeNotFound             Code = "not_found"
	CodeConflict             Code = "conflict"
	CodeNameTaken            Code = "name_taken"
	CodeAlreadyExists        Code = "already_exists"
	CodeTooLarge             Code = "too_large"
	CodeBlockedByFilter      Code = "blocked_by_filter"
	CodeRateLimited          Code = "rate_limited"
	CodeSlowMode             Code = "slow_mode"
	CodeInternal             Code = "internal"
	CodeUnavailable          Code = "unavailable"
)

// statuses maps each code to its HTTP status.
var statuses = map[Code]int{
	CodeInvalidRequest:       http.StatusBadRequest,
	CodeInvalidValue:         http.StatusBadRequest,
	CodeUnauthenticated:      http.StatusUnauthorized,
	CodeSecondFactorRequired: http.StatusUnauthorized,
	CodeForbidden:            http.StatusForbidden,
	CodeTimedOut:             http.StatusForbidden,
	CodeArchived:             http.StatusForbidden,
	CodeLocked:               http.StatusForbidden,
	CodeFeatureOff:           http.StatusForbidden,
	CodeFreshCheckRequired:   http.StatusForbidden,
	CodeNotFound:             http.StatusNotFound,
	CodeConflict:             http.StatusConflict,
	CodeNameTaken:            http.StatusConflict,
	CodeAlreadyExists:        http.StatusConflict,
	CodeTooLarge:             http.StatusRequestEntityTooLarge,
	CodeBlockedByFilter:      http.StatusUnprocessableEntity,
	CodeRateLimited:          http.StatusTooManyRequests,
	CodeSlowMode:             http.StatusTooManyRequests,
	CodeInternal:             http.StatusInternalServerError,
	CodeUnavailable:          http.StatusServiceUnavailable,
}

// Codes lists every code, in the order of docs/CONVENTIONS.md.
func Codes() []Code {
	return []Code{
		CodeInvalidRequest, CodeInvalidValue, CodeUnauthenticated, CodeSecondFactorRequired,
		CodeForbidden, CodeTimedOut, CodeArchived, CodeLocked, CodeFeatureOff, CodeFreshCheckRequired,
		CodeNotFound, CodeConflict, CodeNameTaken, CodeAlreadyExists, CodeTooLarge, CodeBlockedByFilter,
		CodeRateLimited, CodeSlowMode, CodeInternal, CodeUnavailable,
	}
}

// Error is the one error shape the API returns. Services and perm return it,
// and the API layer writes it as it is. Any other error reaching the API
// layer is logged and sent as `internal`.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	// Field names the request field that was wrong, with invalid_value.
	Field string `json:"field,omitempty"`
	// RetryAfterSeconds goes with rate_limited and slow_mode.
	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
}

// Error makes *Error an error. The text is for logs; clients get the JSON.
func (e *Error) Error() string {
	if e.Field != "" {
		return string(e.Code) + " (" + e.Field + "): " + e.Message
	}
	return string(e.Code) + ": " + e.Message
}

// Status is the HTTP status that goes with the code.
func (e *Error) Status() int {
	if s, ok := statuses[e.Code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// Is lets errors.Is(err, model.ErrNotFound) match any error with that code,
// whatever its message.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// With returns a copy of e with another message, for a caller that can say
// something more useful than the default sentence.
func (e *Error) With(message string) *Error {
	c := *e
	c.Message = message
	return &c
}

// The usual errors, each with a sentence a client may show. Compare with
// errors.Is; make a more specific one with With or one of the helpers below.
var (
	ErrInvalidRequest       = &Error{Code: CodeInvalidRequest, Message: "The request could not be read."}
	ErrUnauthenticated      = &Error{Code: CodeUnauthenticated, Message: "Sign in to do this."}
	ErrSecondFactorRequired = &Error{Code: CodeSecondFactorRequired, Message: "A second sign-in step is needed first."}
	ErrForbidden            = &Error{Code: CodeForbidden, Message: "You may not do this."}
	ErrTimedOut             = &Error{Code: CodeTimedOut, Message: "You are timed out here for now."}
	ErrArchived             = &Error{Code: CodeArchived, Message: "This channel is archived."}
	ErrLocked               = &Error{Code: CodeLocked, Message: "This channel is locked. Nobody can be added to it."}
	ErrFeatureOff           = &Error{Code: CodeFeatureOff, Message: "This feature is switched off on this server."}
	ErrFreshCheckRequired   = &Error{Code: CodeFreshCheckRequired, Message: "Confirm with your passkey or authenticator app first."}
	ErrNotFound             = &Error{Code: CodeNotFound, Message: "Not found."}
	ErrConflict             = &Error{Code: CodeConflict, Message: "That cannot be done as things stand."}
	// ErrNameTaken always carries this sentence, whatever the reason
	// (ruling R3): a taken name and a reserved name must look the same.
	ErrNameTaken       = &Error{Code: CodeNameTaken, Message: "That name isn't available."}
	ErrAlreadyExists   = &Error{Code: CodeAlreadyExists, Message: "That already exists."}
	ErrTooLarge        = &Error{Code: CodeTooLarge, Message: "That is too large."}
	ErrBlockedByFilter = &Error{Code: CodeBlockedByFilter, Message: "The message was stopped by this server's word filter."}
	ErrRateLimited     = &Error{Code: CodeRateLimited, Message: "Too many requests. Try again shortly."}
	ErrSlowMode        = &Error{Code: CodeSlowMode, Message: "Slow mode is on here. Wait before posting again."}
	ErrInternal        = &Error{Code: CodeInternal, Message: "Something went wrong on the server."}
	ErrUnavailable     = &Error{Code: CodeUnavailable, Message: "The server is not ready for this yet."}
)

// Invalid makes an invalid_value error naming the field that was wrong.
func Invalid(field, message string) *Error {
	return &Error{Code: CodeInvalidValue, Message: message, Field: field}
}

// BadRequest makes an invalid_request error with its own sentence.
func BadRequest(message string) *Error {
	return &Error{Code: CodeInvalidRequest, Message: message}
}

// RateLimited makes a rate_limited error that says when to try again.
func RateLimited(retryAfterSeconds int) *Error {
	e := *ErrRateLimited
	e.RetryAfterSeconds = retryAfterSeconds
	return &e
}

// AsError finds the *Error inside err, if there is one.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// CodeOf returns the code of err, or "" for nil and `internal` for an error
// that is not one of ours.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	if e, ok := AsError(err); ok {
		return e.Code
	}
	return CodeInternal
}

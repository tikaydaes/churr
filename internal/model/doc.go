// Package model holds the types every other package speaks in: identifiers,
// times, errors, names and the objects the API returns.
//
// It imports nothing else of ours and has no behaviour beyond validation and
// JSON. The JSON tags follow docs/openapi.yaml, so a value of one of these
// types marshals to the shape the contract promises. Fields tagged `json:"-"`
// are stored but never sent as they are; a service turns them into the public
// fields beside them (an account's status columns become its Status object).
package model

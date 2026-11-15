package wirepb

import (
	"mime"
	"strconv"
	"strings"
)

// The schema has a version (docs/WIRE-PROTOBUF.md, "Versions"). A client
// says which it speaks and the server answers with the one it will use:
// the lower of the client's and its own. A client that names none speaks
// version 1.

// UseVersion gives the version to use with a client that speaks version
// theirs: the lower of the two, or 0 if theirs is not a version at all.
func UseVersion(theirs int) int {
	if theirs < 1 {
		return 0
	}
	if theirs > SchemaVersion {
		return SchemaVersion
	}
	return theirs
}

// ContentType is the Content-Type of a protobuf body in a version.
func ContentType(version int) string {
	return MediaType + "; v=" + strconv.Itoa(version)
}

// mediaVersion reads one media type with its parameters. ok is false if it
// is not protobuf, or names a version that is not a number from 1.
func mediaVersion(value string) (version int, ok bool) {
	mt, params, err := mime.ParseMediaType(value)
	if err != nil || mt != MediaType {
		return 0, false
	}
	if q, has := params["q"]; has {
		if f, err := strconv.ParseFloat(q, 64); err == nil && f == 0 {
			return 0, false
		}
	}
	v, has := params["v"]
	if !has {
		return 1, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// Accepts reads an Accept header. ok is true when the client asks for
// protobuf, and version is then the version to answer in. Anything else,
// including a header that cannot be read, means JSON.
func Accepts(accept string) (version int, ok bool) {
	for _, part := range strings.Split(accept, ",") {
		if v, is := mediaVersion(strings.TrimSpace(part)); is {
			return UseVersion(v), true
		}
	}
	return 0, false
}

// BodyVersion reads a Content-Type header. is is true when the body is
// protobuf; version is the version it is written in, 0 if it names one
// that is not a number from 1.
func BodyVersion(contentType string) (version int, is bool) {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil || mt != MediaType {
		return 0, false
	}
	v, _ := mediaVersion(contentType)
	return v, true
}

// subprotocolPrefix starts the name of the live connection's protobuf
// subprotocol; the schema version follows it.
const subprotocolPrefix = Subprotocol + "."

// SubprotocolFor is the subprotocol name for a version: "churr.v1+proto.1".
func SubprotocolFor(version int) string { return subprotocolPrefix + strconv.Itoa(version) }

// ChooseSubprotocol picks, from the subprotocols a client offered on the
// upgrade, the protobuf one to answer with: the highest version offered
// that this server speaks. ok is false when the client offered none, and
// the connection is then the JSON one.
func ChooseSubprotocol(offered []string) (name string, version int, ok bool) {
	for _, o := range offered {
		if !strings.HasPrefix(o, subprotocolPrefix) {
			continue
		}
		n, err := strconv.Atoi(o[len(subprotocolPrefix):])
		if err == nil && n >= 1 && n <= SchemaVersion && n > version {
			name, version, ok = o, n, true
		}
	}
	return name, version, ok
}

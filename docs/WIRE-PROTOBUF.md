# Protocol Buffers

Everything a Churr server says to an app or a bot, and everything they
say to it, can travel as Protocol Buffers instead of JSON (ruling R65):
every request, response and error body of the HTTP API, and every frame
of the live connection.

JSON stays the default and the contract of record. A client that asks
for nothing gets JSON exactly as before; the web chat, webhooks and
scripts need change nothing. Protobuf is the same contract in a second
encoding, never a second contract. Any client may ask for it: an app,
a bot, a tool.

Not covered, and staying as they are: file bytes (uploads, downloads),
the server's own pages, form-encoded and multipart requests, and what
other products send to a webhook address (Slack, Discord and GitHub
payloads). The server makes no outgoing calls to bots; a bot hears
from it over the live connection and the answers to its own requests.

## 1. One source

`proto/churr/v1/churr.proto` is written by `tools/openapi-gen` from
the same sources as `docs/openapi.yaml`. Nobody edits it.

| Source | Gives |
|--------|-------|
| `schemas.py` | A message for every object (`Message`, `Conversation`, …) |
| `ops_*.py` | For every operation, a message for its request body and one for each answer, named after it: `PostMessageRequest`, `PostMessageResponse` |
| `events.py` | For every frame of the live connection, a message (`MessageCreatedFrame`) and a place in the `Envelope` |

`events.py` is new with this: the frames were only described in words
in `EVENTS.md` and `CALLS.md`. It says the same thing in the build's
notation. A new frame or field goes there (and in `EVENTS.md`).

To regenerate after changing the sources: `tools/protogen.sh`. It runs
the build, runs `protoc` for the app's classes, and rewrites the test
vectors. Everything it writes is checked in, so building and testing
need neither Python nor `protoc`.

| Written | What it is |
|---------|------------|
| `proto/churr/v1/churr.proto` | The schema |
| `proto/churr/v1/churr.lock.json` | Every number ever given out (section 5) |
| `internal/wirepb/schema_gen.go` | The schema as a table for the server's codec |
| `app/lib/core/wire/schema.g.dart` | The same table for the app |
| `app/lib/core/wire/gen/` | The app's message classes, by `protoc` |
| `internal/wirepb/testdata/churr.binpb` | The schema as `protoc` read it; the server's codec is tested against it |
| `testdata/wirepb/` | Vectors: JSON and its bytes, for every frame and every message |

## 2. How JSON maps

A field has the name it has in JSON. The rules:

| JSON | Protobuf | Why |
|------|----------|-----|
| An identifier (UUID) | `bytes`, exactly 16 | 16 bytes for 36. JSON keeps the hyphenated hex text; there is no other form |
| A time | `int64`, microseconds since the Unix epoch, UTC | One number of 8 bytes. `google.protobuf.Timestamp` is a nested message of two numbers, about 13 bytes, and brings nanoseconds nobody here has. Microseconds leave room below the API's milliseconds |
| base64url text (keys, records, ciphertext) | `bytes` | Sealed things are opaque bytes; base64 is JSON's way of carrying them |
| A whole number | `int64` | `seq` is 64-bit |
| Another number | `double` | |
| An enumeration | An `enum` whose zero is `…_UNSPECIFIED` | The zero is never sent, so an absent field is never mistaken for a value |
| An object | A message | |
| A list | `repeated` | |
| An object used as a map | `map<string, …>`; `repeated` entries with a `bytes` key where the keys are identifiers | Protobuf's maps cannot have `bytes` keys |
| A list or map inside a map | A small message holding it (`FormTallyValue`) | Protobuf cannot nest them directly |
| Anything (`answers`, a form field's `min`, a provider's `config`) | `Value` | See below |
| An event inside a body (`listEvents`) | `Envelope` | The same message the live connection sends |

**Absent, null and empty.** JSON tells four things apart that
protobuf by itself does not: a field that is absent, one that is
`null`, an empty list, and a zero. Nothing is lost:

- Every single field has explicit presence (`optional`). A field
  absent in JSON is absent on the wire, and `false`, `0` and `""` are
  sent when they are there.
- Every message has `null_fields`: the numbers of its fields that are
  `null` in JSON. `CONVENTIONS.md` says absent and `null` mean the
  same, and a reader may ignore this list; it is there because a few
  requests use `null` to clear something, and so that a body comes
  back exactly as it went.
- Every message with a list or a map has `empty_fields`: the numbers
  of those that are present and empty. Protobuf cannot say "an empty
  list"; this does.

**Open-ended values.** Where the contract says "any JSON value", the
field is a `Value`: a small union of null, boolean, whole number,
other number, text, list and map. It was chosen over
`google.protobuf.Struct` because that one keeps every number as a
double, which loses whole numbers above 2^53 and the difference
between `3` and `3.0`, and is larger. An object that has known fields
and may have others (`SystemNotice`) is a message with its known
fields typed and an `extra_fields` map of `Value` for the rest.

**The envelope.** Every frame of the live connection is one
`Envelope`: `at`, `conversation_id` and `seq` where the frame has
them, and one body out of a `oneof`, which says what the frame is.
`type` is not sent beside a body; the body is the type.

## 3. Nothing is dropped

The encoder is exact or it does not encode. If a value has no exact
place in the schema (a field or an enumeration value the schema does
not have yet, a time not in the contract's form), then:

- a **frame** travels whole as JSON text in `Envelope.json`, with its
  `type` beside it;
- an **HTTP answer** is sent as the JSON it is, with JSON's
  `Content-Type`. A client reads an answer by its `Content-Type`.

So a schema that has fallen behind a handler costs bytes and never
correctness, and a test (`TestProtobufEveryOperation`) counts how
often it happens.

Readers skip what they do not know:

- An unknown field number is ignored, as an unknown JSON field is.
- An unknown enumeration number reads as the field being absent.
- An `Envelope` with a body the reader does not know and no `json` is
  an event type from a newer server: skip it.

A server reading a **request** is strict, as it is with JSON: a field
number the operation does not take is `invalid_request`.

## 4. Asking for it

### HTTP

    Accept: application/x-protobuf; v=1
    Content-Type: application/x-protobuf; v=1

- `Accept` naming `application/x-protobuf` gets the answer as the
  operation's response message, and errors as `Error`. Any other
  `Accept`, or one the server cannot read, gets JSON.
- `Content-Type: application/x-protobuf` on a request sends the body
  as the operation's request message. The two headers are independent.
- The answer's `Content-Type` is `application/x-protobuf; v=1`, with
  `Vary: Accept`. Status codes, the rate-limit headers and everything
  else are the same.
- `docs/openapi.yaml` lists the media type beside `application/json`
  on every body that has a message, and names the message in
  `x-protobuf-message`.
- Two answers have no message and are always JSON: the OAuth metadata
  document and the empty answer of a token revocation.

A client that may meet an older server sends its first request with
`Accept` only, and sends protobuf bodies once it has had a protobuf
answer: a server from before this ignores `Accept` and answers JSON,
but could not read a protobuf body.

### The live connection

To be wired (the codec is in place; the connection does not offer it
yet). A client offers the subprotocol `churr.v1+proto.1` ahead of
`churr.v1`. A server that knows it answers with it, and every frame in
both directions is then a binary WebSocket message holding one
`Envelope`. A server that does not know it answers `churr.v1`, and the
connection is the JSON one.

### Versions

The schema has a version: 1. The client says the version it speaks
(`v=` on the media type; the number at the end of the subprotocol),
and the server answers with the one it will use, which is the lower
of the client's and its own. A client that names none speaks 1. A
request body written in a version higher than the server's is refused
with `invalid_request`.

## 5. Changing the schema

Within a version only additions are made, and every one of them is
safe for a reader that has not heard of it:

| Change | Old reader |
|--------|-----------|
| A new field | Ignores it |
| A new operation or frame | Never asks for it; skips the frame |
| A new enumeration value | Reads the field as absent. If old clients must be told something, add a field instead |
| A field no longer sent | Reads it as absent. Its number is retired |

Numbers never change and are never used twice. The generator reads
`churr.lock.json` first: a field in it keeps its number, a new field
takes the next number after the highest its message ever used, and a
field that has gone stays in the lock and is written as `reserved`.
Regenerating with no change to the sources changes no file. Do not
edit the lock by hand, and never delete from it.

What may not be done within a version: changing a field's type,
moving a field between messages, renaming a frame's `type`, or
changing what a value means. If one of these is ever needed, add a
new field and retire the old one. The version goes up only for a
change an older reader would misread rather than skip; a server then
keeps answering the older version to clients that ask for it.

## 6. The proof

- `testdata/wirepb/` holds, for every frame type and every message,
  JSON values and their bytes, written by the server's codec: three
  made-up values of each (everything present; every other field with
  nulls and empty lists; nothing) and hand-written realistic frames
  (`fixtures.json`).
- Go (`internal/wirepb`): each vector goes JSON → protobuf → JSON and
  comes back the same, also through `model.Event`; the standard
  protobuf library, given the schema as `protoc` read it, parses each
  with no unknown field and writes the same bytes.
- Dart (`app/test/core/wire_test.dart`): for each vector, reading the
  bytes gives exactly what `jsonDecode` gives for the JSON, type for
  type, and writing gives the server's bytes.
- The API (`internal/api/protobuf_test.go`): every operation is called
  in JSON and in protobuf on two servers seeded alike, and answers the
  same. `CHURR_TEST_WIRE=protobuf go test ./internal/api/` runs the
  whole of the API's tests over protobuf.

## 7. Sizes

Bytes of one frame; "deflated" is the frame compressed by itself, as
the live connection does (`EVENTS.md` section 1).

| Frame | JSON | Protobuf | JSON deflated | Protobuf deflated |
|-------|-----:|---------:|--------------:|------------------:|
| `message.created`, 200 bytes of text | 588 | 319 | 324 | 262 |
| `reaction.added` | 411 | 134 | 203 | 81 |
| `presence.changed` | 145 | 33 | 116 | 40 |
| `presence.batch`, 3 accounts | 511 | 110 | 171 | 117 |
| `presence.batch`, 2,000 accounts | 296,069 | 66,013 | 9,479 | 8,436 |
| `typing.started` | 166 | 47 | 127 | 54 |
| `read_position.moved` | 263 | 48 | 172 | 55 |

Small protobuf frames are best sent as they are: deflate makes them
longer. `go test -v -run TestSizes ./internal/wirepb` prints the table.

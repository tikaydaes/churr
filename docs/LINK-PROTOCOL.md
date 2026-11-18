# The link protocol, version 3

How two Churr servers share conversations, directly or through a hub.
This is the detail behind DESIGN.md section 11. `CONVENTIONS.md` gives
the rules for identifiers and times used here, `MESSAGE-FORMAT.md`
gives the message shapes that travel, and
`proto/churr/link/v1/link.proto` is the schema of everything this
document puts on the wire.

Shared conversations are ordered by a clock their servers share and
not by anybody's numbering (ruling R57): section 5 says how the
clock is measured, section 9 how every event is stamped with it, and
section 13 what the order is and who checks what.

A channel shared through a hub belongs to the hub (ruling R56):
section 23a says what the hub records and signs, how it holds the
whole channel, who a channel's managers and moderators are, how an
instance joins and who is in a private one. A channel two servers share
directly can be moved onto a hub (ruling R55): section 21a. Hubs can
be joined by trunks into a cluster, so that a channel reaches servers
on several hubs (ruling R58): section 23b.

This is version 3 of the protocol: everything servers and hubs say
to each other is compressed Protocol Buffers (ruling R70; section 2).
Versions 1 and 2 said the same things as JSON text with base64 inside
it. Neither was released, nothing of them is kept, and a side that
speaks only one of them is refused at the handshake (section 5). The
path `/link/v1` and the labels inside signatures keep their names:
they are names, not version numbers.

The paths this document fixes are `/link/v1`, where a server or hub
accepts link connections, and `/.well-known/churr`, where it publishes
its name and keys. Admin operations are described in words;
`docs/openapi.yaml` gives them their paths.

## 1. Terms

| Term        | Meaning                                                     |
|-------------|-------------------------------------------------------------|
| Server      | A Churr server. Named by the host name of its public address, lowercase. |
| Hub         | A `churr-hub`. It relays between servers and has no members. |
| Peer        | A lasting, mutually accepted link between this server and one other server (a direct link) or one hub. Made by the owner. |
| Stream      | One conversation shared over one peer. Has a UUIDv7, the `stream_id`. |
| Owner server | The server that shared the conversation over a direct link, or that started an ad-hoc channel or sealed chat. It decides which servers are in the stream and holds the power to remove any message. A channel on a hub has no owner server: the hub owns it. |
| Hub-owned channel | A stream of kind `channel` on a hub. The hub keeps its record, is the only party that signs a change to it (section 23a), and makes every key epoch's stream key (section 10). |
| Manager (of a hub-owned channel) | An account, named with its home instance in a hub-owned channel's record, that holds the channel as an owner would: its instances, name, description, listing, managers and moderators, closing it (section 23a). |
| Moderator   | An account, named with its home instance in a hub-owned channel's record, that keeps the peace in the channel on every instance in it: it removes messages and people, and admits people to a private one. A manager may do all a moderator may. |
| Time reference | The party whose clock a stream's events are timed by, and which keeps the stream's log: the stream's home hub when the peer is a hub, otherwise the owner server. It orders nothing (section 13). |
| Home hub    | The hub a stream was opened on, or for a channel the hub its operator last moved it to. It owns the channel, signs its record, makes and keeps its stream keys and is its time reference. |
| Cluster     | Hubs joined by trunks, in a tree. A hub that is joined to no other is a cluster of one (section 23b). |
| Trunk       | A lasting, mutually authenticated link connection between two hubs of a cluster. |
| Edge hub    | For a server and a channel whose home is another hub: the hub the server is connected to, which is its relay. It holds none of the channel's keys. |
| Stamp       | What an origin puts on every event it makes, under its signature: `at`, `n` and `offset` (section 9). |
| Envelope    | One signed, encrypted event in a stream.                    |
| Origin      | The server that made an envelope.                           |
| Home server | The server an account belongs to.                           |
| Direct message | A sealed, signed message from one server to one other server, outside any stream's log (section 7). |

A conversation is in at most one stream, and a stream runs over
exactly one peer. A stream over a direct link therefore has exactly
two servers. Sharing one channel with three or more servers needs a
hub: the channel is shared to the hub in the first place (section
23a), or moved there from the direct link it began on (section 21a).

Each server keeps its own full copy of a shared conversation under its
own local conversation identifier, and serves its own members from it.
The `stream_id` is the only name for the conversation on the wire.

## 2. Encodings and algorithms

- **Everything that travels is Protocol Buffers** (proto3). The
  schema is `proto/churr/link/v1/link.proto`. It is written by
  `tools/openapi-gen` from `link.py`, in the same build that writes
  the client schema (`WIRE-PROTOBUF.md`), and its numbers are kept
  in `link.lock.json` beside it: a number never changes and is never
  used twice. Three messages are the roots, and each carries one
  body out of a `oneof`; the body that is set says what it is.

  | Message  | Is                                             | Its bodies         |
  |----------|------------------------------------------------|--------------------|
  | `Frame`  | One frame of a connection (section 6)          | One per frame name |
  | `Direct` | The plaintext of a direct message (section 7)  | One per `kind`     |
  | `Event`  | The plaintext of an envelope (section 11)      | One per `type`     |

  A field has the same number in every body of one root that carries
  it, so a body is a list of the fields it carries and nothing more.
- **Frames** are one `Frame` each, one per binary WebSocket message.
  There are no text messages. A frame is at most 1,048,576 bytes,
  counted before compression.
- **Compression.** Every link connection uses WebSocket compression
  (`permessage-deflate`, RFC 7692), each message compressed by
  itself (`client_no_context_takeover` and
  `server_no_context_takeover`). A dialler must offer the extension
  and a listener must accept it: a listener refuses the upgrade of a
  dialler that does not offer it (HTTP 400), and a dialler closes at
  once on a listener that does not agree to it. A frame is sent
  deflated when that makes it smaller, and as it is when it does not
  (a single envelope is mostly ciphertext and a signature, which do
  not compress). The size limit is on the frame as inflated.
- **Bytes** (keys, signatures, ciphertext, file chunks) are `bytes`,
  as they are. Nothing is base64 on the wire.
- **Identifiers** are UUIDs and travel as their 16 bytes. A reader
  refuses any other length, and a writer cannot write an identifier
  that is not a UUID in the canonical text form. In byte layouts an
  identifier is the same 16 bytes, written `id16(x)`.
- **Times** are `int64` microseconds since 1970, UTC, as in the
  client schema. The link's own times are whole milliseconds (the
  stamp's `at`, a ping's `t0`), and in a frame or a signed block a
  reader refuses a time that is not. Durations (`offset`, anything
  ending `_ms`) are milliseconds as they are.
- **Absent and zero.** A field that is absent means what its zero
  would: no text, 0, false, an empty list. Where the difference
  matters the field is sent even when it is zero: `t0`, `t1` and
  `t2` of a ping and pong, `name` and `description` of a manager's
  action, and every field of a `Direct` or an `Event` body that its
  sender set. `null` does not exist.
- **What a reader does not know.** A reader skips a field number it
  does not know, in any message. A `Frame` whose body it does not
  know is answered with an `error` frame, code `bad_frame`, and the
  connection stays open. A `Direct` whose body it does not know is
  dropped; an `Event` whose body it does not know changes nothing
  (section 9, step 10).
- **What the schema cannot hold.** `Direct` and `Event` each have a
  field `json`. A sender whose event has a field or a type its own
  schema has no place for sends the whole object as JSON text there,
  with no body, and a receiver reads it exactly as it would read the
  body. Nothing this document defines is sent that way; it exists so
  that a schema that has fallen behind its server's code costs bytes
  and loses nothing. The suite fails if it happens (section 25).
- **Objects the client API also has.** Where an event carries an
  object that is the same in the client API (a quote, a card, a
  form, a via, a chance result, a sealed payload, a task, a form's
  option, tally and voters, a call and its participant, and the
  fields of an entity), the schema uses the client schema's message
  for it (`churr.v1.Quote` and so on), written as
  `WIRE-PROTOBUF.md` says. Open-ended values (a system notice's
  fields, call signalling, a presence status) are `churr.v1.Value`.
- **How this document writes them.** The examples show a message as
  an object with the schema's field names:
  `{"frame": "ack", "stream_id": "…", "cursor": 4822}`. That is
  notation, not bytes. `frame` (and `kind` in a direct message,
  `type` in an event) stands for the body that is set; an identifier
  is shown as text and a time as text or as milliseconds, whichever
  reads better.
- **Byte layouts** for signing use these pieces: a label in ASCII
  followed by one zero byte; `u16(n)` and `u32(n)`, unsigned
  big-endian integers; `i64(n)`, 8 bytes, big-endian, two's
  complement; `lp(s)`, a UTF-8 string preceded by `u16` of its
  length in bytes; and `||` for joining. A time in a layout is in
  milliseconds, whatever unit carried it.

### What is signed, and how it travels

No signature in this protocol is checked against a protobuf message
that the checker wrote out again. Field order, the forms a number may
take, and fields a reader does not know all make "the same message"
many different byte strings, and a signature is over one of them.
There are three kinds, and each says exactly which bytes:

1. **Signed blocks.** The signer writes the content once, as the
   bytes of one message of the schema, and signs those bytes
   (section 6, "Signed blocks"). They travel as a `bytes` field,
   `signed`, with the signature in `sig` beside it. **Whoever
   carries a block carries those bytes**: a hub relaying it, a
   server passing it on inside another frame or a direct message, a
   database keeping it. Nobody but the signer ever writes them. A
   verifier checks the signature over the bytes as it received them
   and only then reads them as the message. The same content written
   as other bytes is another block, and does not verify.

   The signer writes the canonical form: fields in the order of
   their numbers, map entries in the order of their keys, lists of
   numbers packed, no field the schema lacks. It is what the
   reference protobuf libraries write when asked for deterministic
   output, and the suite compares the two byte for byte. A verifier
   does not depend on it, and must not: it verifies whatever bytes
   arrived.
2. **Byte layouts.** An envelope, a direct message, a box key, an
   invitation proof and a fingerprint are signed (or hashed) over a
   layout built from their values with the pieces above: fixed
   widths, lengths in front, nothing optional. The values travel as
   ordinary fields; the verifier builds the layout from the values
   it read and checks the signature over that. The layout does not
   depend on how the fields were encoded, so a party that reads an
   envelope and writes it again (a hub taking it into its log and
   delivering it, a trunk relaying it) cannot break the signature
   and cannot keep it while changing a value: any other value is
   another layout. Sections 3, 5, 7 and 9 give each layout.
3. **The handshake.** Each side signs a hash over the exact bytes of
   the two `hello` frames as they were sent: the payloads of those
   two WebSocket messages, before compression (section 5).

Why two forms and not one: a block's content is rich and grows
(lists, maps, fields a later version adds), is checked by parties
that did not make it, and passes through hubs that may be older than
its signer, so the only safe thing to sign is the bytes themselves.
An envelope's signed part is small and fixed, and a hub reads those
very values on every envelope it takes (whose it is, its counter,
its time); signing a layout of the values means there is no second
copy of them inside signed bytes that could say something else.

What is encrypted (`ciphertext` in an envelope, `body` in a direct
message) is opaque bytes to everyone without the key, and is passed
on as it came.

### Sizes

Bytes of one frame. "Version 2" is the JSON this protocol was before,
which was sent uncompressed; "sent" is what version 3 puts on the
wire, the smaller of the two columns before it.

| Frame                                   | Version 2, JSON | deflated | Version 3, protobuf | deflated | Sent    |
|-----------------------------------------|----------------:|---------:|--------------------:|---------:|--------:|
| `deliver`, one message of 200 bytes of text |         1,051 |      828 |                 503 |      509 |     503 |
| `deliver`, 100 such messages            |         102,133 |   64,737 |              50,104 |   43,713 |  43,713 |
| `ack`                                   |              81 |       87 |                  26 |       32 |      26 |
| `accepted`, one envelope                |              75 |       81 |                  23 |       29 |      23 |
| `direct`, a 256 KiB chunk of a file     |         349,973 |  350,004 |             262,460 |  262,486 | 262,460 |

The event inside that message is 477 bytes as JSON and 302 as
protobuf before it is sealed; sealing adds 40, and the envelope
around it (two identifiers, the origin's name, the stamp, the
signature, its place in the log) about 160.
`go test -v -run TestSizes ./internal/link/wire` prints the table.

### Algorithms

| Purpose                        | Algorithm                              |
|--------------------------------|----------------------------------------|
| Server signatures              | Ed25519 (RFC 8032), no prehash         |
| Key agreement                  | X25519 (RFC 7748)                      |
| Encryption                     | XChaCha20-Poly1305, 24-byte nonce, 16-byte tag |
| Key derivation                 | HKDF-SHA-256 (RFC 5869)                |
| Hashing                        | SHA-256                                |
| Invitation proof               | HMAC-SHA-256                           |

All of these are in the Go standard library or `golang.org/x/crypto`,
so nothing here needs cgo.

## 3. Server identity

At first start a server makes two key pairs and writes the private
halves to `keys/` in the data directory:

| Key        | Type    | Used for                                        |
|------------|---------|-------------------------------------------------|
| `sign_key` | Ed25519 | Proving who it is; signing envelopes and direct messages |
| `box_key`  | X25519  | Receiving stream keys and direct messages       |

A hub makes the same two. It is sealed the direct messages that ask it
for the key of a channel it is the home of (section 10).

There is no key change in this protocol. A server that loses its keys is
a new server to everyone it was linked with: its peers remove the old
link and link again.

### `/.well-known/churr`

Served over HTTPS at the server's public address, with
`Content-Type: application/json` and `Cache-Control: max-age=3600`.
This document and the invitation (section 4) are the two things in
the protocol that are JSON, with bytes as base64url without padding
(RFC 4648 section 5; a reader rejects padding and characters outside
the alphabet). Both are read before any connection exists: the
document by a plain HTTPS fetch, by people and their tools as well
as by servers, and it is what tells a reader which protocol versions
are spoken at all; the invitation by a person, who pastes it.

```json
{
  "churr": 1,
  "kind": "server",
  "name": "chat.example.org",
  "sign_key": "mJ0nQ3lH1n3vJmUqkW0oZ8m6mUu0b0mXvVQyqg0dT1I",
  "box_key": "pX8Qw2s8mX0q7nJm2c1t3nVh2mZk8o1yQeXv5b0aL2c",
  "box_key_sig": "…86 characters…",
  "link": {"versions": [3], "url": "wss://chat.example.org/link/v1"}
}
```

| Field           | Meaning                                              |
|-----------------|------------------------------------------------------|
| `churr`         | Version of this document's format. Always 1.         |
| `kind`          | `server` or `hub`                                    |
| `name`          | The server's name. Must equal the host the document was fetched from. |
| `sign_key`      | The Ed25519 public key, 32 bytes                     |
| `box_key`       | The X25519 public key, 32 bytes                      |
| `box_key_sig`   | Ed25519 signature by `sign_key` over `"churr-box-key-v1" 0x00 \|\| lp(name) \|\| box_key` |
| `link.versions` | Link protocol versions spoken                        |
| `link.url`      | Where to dial                                        |

The signature ties the encryption key to the signing key, so pinning
the signing key pins both.

### Fingerprints

A server's fingerprint is the first 20 bytes of

    SHA-256("churr-fp-v1" 0x00 || sign_key)

It is shown as 32 characters of base32 (the RFC 4648 alphabet,
upper case, no padding) in eight groups of four:

    K7QF-2M3X-PLDA-6TZN-W4HB-EJ5R-QC2V-YU7G

The admin console shows a server's own fingerprint on its links page,
and the fingerprint of every peer and of every server reached through
a hub. Two owners who want to be sure there is nobody in between read
the groups to each other.

## 4. Peers and invitations

A peer is made by an invitation. The side that issues the invitation
listens; the side that accepts it dials. This never changes for the
life of the peer.

| Link         | Issued by         | Accepted by      | Who must be reachable |
|--------------|-------------------|------------------|-----------------------|
| Direct       | Owner of server A | Owner of server B | A                    |
| Through a hub | The hub's operator | A server's owner | The hub only         |

Only the owner of a server can issue or accept an invitation, remove
a peer, or share a channel. Each of these is written to the audit log.

### The invitation

An invitation is one string, safe to paste:

    churr_link_eyJ2IjoxLCJraW5kIjoic2VydmVyIiwibmFtZSI6ImEuZXhhbXBsZSIs…

It is `churr_link_` followed by the base64url of this JSON:

```json
{
  "v": 1,
  "kind": "server",
  "name": "a.example",
  "url": "wss://a.example/link/v1",
  "fp": "V_kF0mZ2y3Pq1c7N8tQeXw4hB9I",
  "secret": "3q2-7wAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
  "expires_at": "2026-11-20T09:30:00.000Z",
  "for": null
}
```

| Field        | Meaning                                                 |
|--------------|---------------------------------------------------------|
| `v`          | Invitation format. Always 1.                            |
| `kind`       | `server` for a direct link, `hub` for a hub             |
| `name`       | The issuer's name                                       |
| `url`        | Where to dial                                           |
| `fp`         | The issuer's fingerprint, 20 bytes                      |
| `secret`     | 32 random bytes. Works once.                            |
| `expires_at` | Seven days after it was made, unless the issuer chose sooner |
| `for`        | Optional. The one server name that may use it. Hubs can set this. |

- The issuer stores `SHA-256(secret)` and the expiry
  (`peers.invite_hash`, `peers.invite_expires_at`) and a peer row in
  state `invited`. It does not keep the secret.
- The owner who accepts pastes the string into their admin console,
  which shows the issuer's name and fingerprint and asks for
  confirmation before dialling.
- An invitation is used up by the first successful handshake that
  presents it. It can be revoked before then.

## 5. The connection

### Dialling

The accepting side opens a WebSocket to the `url` it was given, over
TLS with ordinary certificate checks, offering the subprotocol
`churr-link.v3` and the `permessage-deflate` extension (section 2).
Plain `ws://` is accepted only on loopback, for tests. One
connection carries every stream of the peer.

### Handshake

Five frames, in this order. Anything else, or more than 10 seconds
between the first and the last, ends the connection.

    dialler                                listener
       │ ── hello ───────────────────────────▶ │
       │ ◀─────────────────────────── hello ── │
       │ ◀──────────────────────────── auth ── │
       │ ── auth ────────────────────────────▶ │
       │ ◀─────────────────────────── ready ── │

**1. `hello` from the dialler**

```json
{
  "frame": "hello",
  "versions": [3],
  "kind": "server",
  "name": "b.example",
  "sign_key": "…",
  "box_key": "…",
  "box_key_sig": "…",
  "nonce": "…32 random bytes…",
  "software": "churr/1.0.0",
  "limits": {"max_frame_bytes": 1048576, "max_envelope_bytes": 262144,
             "submits_per_minute": 600, "ephemeral_per_minute": 1200,
             "direct_per_minute": 600, "blob_bytes_per_hour": 1073741824}
}
```

**2. `hello` from the listener.** The same fields, with `version` (one
number, the highest it shares with the dialler's `versions`) in place
of `versions`, and its own `kind`, which may be `hub`. A hub sends no
`box_key`.

**A version mismatch is refused here, before anything else is said.**
This document is version 3 and a side that implements it speaks
version 3 only. A listener whose dialler's `versions` does not hold 3
answers the dialler's `hello` with

    {"frame": "error", "code": "version_unsupported",
     "message": "This side speaks version 3 only."}

and closes; it sends no `hello` of its own, so the dialler learns
nothing but the refusal. A dialler that does not offer the
subprotocol `churr-link.v3`, or does not offer compression, never
gets that far: the upgrade itself fails, which is all a JSON dialler
of an unreleased version could have read anyway. A dialler whose
listener answers with any
other `version` than 3 closes without sending `auth`. Neither side
falls back, and a peer that is refused this way stays as it was: the
dialler keeps the refusal as the peer's `last_error` and tries again
with its usual wait, which succeeds once both sides run the same
version.

`limits` is what the sender of the `hello` will accept from the other
side. Each side enforces its own.

**3. `auth` from the listener**

    {"frame": "auth", "sig": "…"}

**4. `auth` from the dialler**

    {"frame": "auth", "sig": "…", "invite_proof": "…"}

`invite_proof` is present only on the first connection of a new peer.

**5. `ready` from the listener**

    {"frame": "ready"}

**What is signed.** Let `h1` and `h2` be the exact bytes of the two
`hello` frames as sent: the `Frame` messages, as each side wrote
them and before compression. Neither side writes the other's again
to check it. The transcript hash is

    th = SHA-256("churr-link-hs-v1" 0x00
                 || u32(len(h1)) || h1 || u32(len(h2)) || h2)

| Value          | Computed as                                           |
|----------------|-------------------------------------------------------|
| Listener `sig` | Ed25519 by the listener's `sign_key` over `"churr-link-auth-v1" 0x00 \|\| "listener" 0x00 \|\| th` |
| Dialler `sig`  | Ed25519 by the dialler's `sign_key` over `"churr-link-auth-v1" 0x00 \|\| "dialler" 0x00 \|\| th` |
| `invite_proof` | HMAC-SHA-256 with key `SHA-256(secret)` over `"churr-link-invite-v1" 0x00 \|\| th` |

Both nonces and both keys are inside `th`, so a signature or proof
from one connection is useless on another.

**What the dialler checks**, before sending its own `auth`:

1. `name` and `kind` are what the invitation or the stored peer says.
2. First connection: the fingerprint of `sign_key` equals the
   invitation's `fp`. Later connections: `sign_key` equals the pinned
   key.
3. `box_key_sig` is valid (not for a hub).
4. The listener's `sig` is valid.

Only then does it reveal anything derived from the invitation secret.

**What the listener checks** on the dialler's `auth`:

1. `box_key_sig` and `sig` are valid.
2. If `name` is a peer in state `active`: `sign_key` and `box_key`
   equal the pinned keys.
3. Otherwise: `invite_proof` matches an invitation that is unused and
   not expired, and the invitation's `for`, if set, equals `name`. The
   invitation is then used up, the keys are pinned, and the peer
   becomes `active` on both sides.
4. `name` is not already pinned with different keys, and is not
   blocked.

A failure is answered with an `error` frame (`auth_failed`,
`invite_invalid`, `invite_expired`, `key_mismatch` or `blocked`) and
the connection is closed.

After a first handshake both admin consoles show the other side's
name and fingerprint. Where the other side's `/.well-known/churr` can
be fetched, the server fetches it once and compares name and keys; a
mismatch removes the new peer and tells the owner, and an unreachable
address is shown as "name not confirmed". That fetch is covered by the
same switch as the peer itself.

### After `ready`

- A hub sends a `roster` frame (section 6).
- The time reference begins pinging (below). A server that has never
  been told its clock offset by a stream's time reference makes no
  events in that stream until it has been, which on a new link is one
  round trip away.
- For each active stream whose log someone else keeps, the server
  sends `catch_up` with what it holds of each origin (section 13),
  then submits whatever is in its outbox, oldest first.
- A second authenticated connection for the same peer replaces the
  first, which is closed with an `error` frame, code `replaced`.

### Keeping it open, and reconnecting

- The heartbeat is the link's own `ping` and `pong` (below), not the
  WebSocket's. A side that has heard nothing at all for 90 seconds
  closes the connection.
- Only the dialler reconnects. It waits 1 second, then doubles the
  wait after each failure up to 5 minutes, with a random 20 per cent
  added or taken away each time. The wait goes back to 1 second once
  a connection has stayed up for 60 seconds.
- `peers.connected_at` and `peers.last_error` record the outcome for
  the admin console.
- A side that is shutting down or removing the peer sends
  `{"frame": "bye", "reason": "shutdown" | "removed"}` first. After
  `removed` the dialler does not reconnect.

### The shared clock: `ping` and `pong`

A shared conversation is ordered by time (section 13), so its servers
must agree what time it is. They do not trust their own clocks for
that. Each stream has a **time reference**: its home hub, or on a
direct link its owner server. The reference measures the clock of
every server connected to it and tells each one how far its clock is
from the reference's. A server then reads "the reference's time" as
its own clock minus that offset.

Who pings: a hub pings every server connected to it. On a direct
link each server owns some streams and is the reference of those, so
both servers ping, each measuring the other. A server never measures
a hub.

    reference                               server
       │ ── ping  t0 ───────────────────────▶ │  arrives at t1
       │ ◀──────────────── pong  t0, t1, t2 ── │  leaves at t2
       │  arrives at t3

    {"frame": "ping", "id": 7, "t0": 1794557400120,
     "clock": {"offset_ms": -412, "latency_ms": 38, "jitter_ms": 3,
               "uncertainty_ms": 20, "forward_ms": 19, "forward_jitter_ms": 1,
               "back_ms": 19, "back_jitter_ms": 2, "asymmetry_ms": 0,
               "samples": 16, "measured_at": "2026-11-13T09:30:00.120Z"}}

    {"frame": "pong", "re": 7, "t0": 1794557400120,
     "t1": 1794557399727, "t2": 1794557399728}

All four times are milliseconds since 1970 (and travel, like every
time, as microseconds). `t0` and `t3` are read
from the reference's clock, `t1` and `t2` from the server's; `t3` is
never sent. The server reads `t1` before it does anything else with
the frame and `t2` as the answer leaves. A `pong` echoes `t0`; one
that answers no ping the reference sent, or echoes another `t0`, is
ignored. From one exchange:

    offset     = ((t1 − t0) + (t2 − t3)) / 2     the server's clock minus the reference's
    round trip = (t3 − t0) − (t2 − t1)

An exchange that cannot be true (`t3` before `t0`, `t2` before `t1`,
a negative round trip) is dropped.

**The window.** The reference keeps the last 16 exchanges per
connected server and works out, after each one:

| Figure           | How                                                        |
|------------------|------------------------------------------------------------|
| `offset_ms`      | The offset of the exchange with the lowest round trip, an older exchange counting for less by 50 parts per million of its age (two clocks drift). This is the offset the reference applies and tells. |
| `latency_ms`     | The lowest round trip in the window.                        |
| `jitter_ms`      | The mean by which the window's round trips exceed the lowest. |
| `uncertainty_ms` | Half the round trip of the exchange used, rounded up, plus the drift allowed since it, plus 1 for rounding, plus whatever was taken out of the raw offset as asymmetry (below). Never less than half the lowest round trip, except in synchronised mode. An offset not measured for a while is trusted less: 50 parts per million of the time since `measured_at` are added when it is used. |
| `forward_ms`, `back_ms` | The lowest one-way figures in the window, `t1 − t0` and `t3 − t2`, with the offset taken out. |
| `forward_jitter_ms`, `back_jitter_ms` | The mean by which each direction's figures exceed its own lowest. |
| `asymmetry_ms`   | An estimate of how much longer the forward direction takes than the back one. 0 unless there is something to base one on (below). |

**When.** A new connection is pinged at once. As soon as the first
`pong` is in, the next `ping` leaves, carrying `clock`: a server is
told its offset one round trip after the handshake. Four more pings
follow 150 milliseconds apart, then one every 30 seconds. `clock` is
on every `ping` once there is one to tell.

**Each direction by itself.** The two one-way figures each contain
the clock offset as well as a delay, with opposite signs, and a
delay is never less than nothing. So the reference watches the
lowest of each over the newest four exchanges against what it was:

- One direction's lowest figure moves and the other's stays: that
  direction's path changed. The clocks did not. The reference does
  not let the offset follow: it adds the change to `asymmetry_ms`
  and takes half of that out of the raw offset. To be believed, the
  move must be more than six times that direction's own jitter (and
  more than 10 ms) and rest on at least eight older exchanges, so a
  path that simply jitters in one direction is not mistaken for one
  that keeps changing.
- Both move by the same amount in opposite senses: a clock was set.
  The exchanges from before describe a clock that no longer exists
  and are dropped at once, so the new offset is told on the next
  ping and not when the old exchanges have aged out.
- A mixture is split the same way: what the two share with opposite
  signs is the clock, the rest is the path.

When an exchange looks unlike the ones before it, the reference
pings four more times, 150 ms apart, at most once per ordinary ping,
to see which of these it is.

**What cannot be known.** A *constant* difference between the two
directions cannot be told from a clock offset by any exchange of
four timestamps. If the forward path always takes 80 ms and the way
back 20 ms, the measured offset is 30 ms more than the true one, and
nothing in the figures shows it. What is certain is only that
neither direction took less than no time: the true offset lies
within half the round trip of the measured one. That bound is the
floor of `uncertainty_ms`, and it is the error this protocol
accepts. Its effect on order is that two events made on different
servers within that many milliseconds of each other may be shown in
either order, the same on every server.

**Synchronised mode.** An operator who knows the clocks are
synchronised by other means (NTP on every machine, say) can tell the
reference so: `churr-hub serve -clock-synced` for a hub,
`churr serve -clock-synced` for a server in its role as the
reference of the streams it owns. It is off unless given and is
never worked out by the program. With it the reference applies no
more than 10 ms of offset either way. Whatever more it measures it
reports as `asymmetry_ms` (twice the excess, forward minus back) and
does not apply; `uncertainty_ms` is then at most 20 ms and the
figures carry `"synced": true`.

**What each side keeps.** A server stores what each peer told it
(`peers.clock_told`) and, on a direct link, what it measured of the
other server (`peers.clock_measured`); a hub stores what it measured
of each server. One set of figures per peer, whatever the number of
conversations, kept while disconnected and across restarts. The
owner sees both in the peer view of the API (`told_clock`,
`measured_clock`); a hub's operator sees each server's figures with
`churr-hub servers`, and the status page shows totals.

## 6. Frames

Every frame is a `Frame` message with one body set, and the body's
name is the frame's: what this document writes as `frame`. A frame
that the receiver may answer carries `id`, a positive integer that
only grows on that connection for that sender; the answer carries
`re` with the same number. `id` and `re` are the `Frame`'s own
fields; everything else is in the body, and a frame cannot be sent
with a field its body does not have.

| `frame`         | Sent by → to                 | Purpose                      |
|-----------------|------------------------------|------------------------------|
| `hello`, `auth`, `ready` | Section 5           | Handshake                    |
| `ping`, `pong`  | Reference → server, and back | Heartbeat and the shared clock (section 5) |
| `roster`        | Hub → server                 | The servers on the hub, and on the other hubs of its cluster, and their keys |
| `cluster`       | Hub → server, hub ↔ hub      | The hubs of the cluster and their keys (section 23b) |
| `cluster_join`, `hub_servers`, `hub_clocks`, `sync`, `relay`, `relay_ack`, `ask`, `forward`, `rehome`, `stream_ctl` | Hub ↔ hub | What travels over a trunk (section 23b) |
| `channel_create` | Server → hub                | Share a channel to the hub, or have it prepare one for a move (sections 23a, 21a) |
| `channel`       | Hub → servers                | A hub-owned channel's record, whole, signed by the hub |
| `channel_action` | Server → hub                | A manager's action, signed by the manager's home instance |
| `join`          | Server → hub                 | This server wishes to be in a hub-owned channel |
| `directory`     | Server → hub, and back       | The hub's listed channels    |
| `channel_commit` | Server → hub                | A move from a direct link has been committed (section 21a). Beside the signed block, in `inner`, the prepared record the hub signed |
| `stream_open`   | Owner → reference            | A new stream exists          |
| `server_joined` | Owner → reference → servers  | A server was added to a stream |
| `server_left`   | Owner → reference → servers  | A server left or was removed |
| `leave`         | Server → reference → owner   | This server is leaving a stream |
| `owner_changed` | Owner → reference → servers  | The stream has a new owner   |
| `stream_close`  | Owner → reference → servers  | The stream is over           |
| `submit`        | Server → reference           | Here are envelopes I made    |
| `accepted`      | Reference → server           | These were taken; this one was not, and why |
| `deliver`       | Reference → server           | Here are envelopes others made |
| `ack`           | Server → reference           | Taken up to this place in your log |
| `catch_up`      | Server → reference           | Send what I am missing       |
| `caught_up`     | Reference → server           | That was all                 |
| `ephemeral`     | Server → reference → servers | Typing and presence          |
| `direct`        | Server → (hub →) server      | A direct message (section 7) |
| `error`         | Either                       | Something was refused        |
| `bye`           | Either                       | Closing on purpose           |

"Reference" in this table is whoever keeps the stream's log and is
its time reference: the hub, or on a direct link the owner server.
There "owner → reference" happens inside one program; the frame is
still sent to the other server so both hold the same record. Nothing
in the frames assumes that the party a server submits to and
receives from is the stream's time reference: `submit` and `deliver`
are between neighbours, and the reference is named by the stream.

### Signed blocks

Frames that a third party must be able to check carry their content
as bytes with a signature, so that no two implementations can disagree
about what was signed:

    {"frame": "<name>", "id": 12, "signed": <bytes>, "sig": <bytes>}

`signed` is the bytes of one message of the schema, written by the
signer and by nobody else (section 2, "What is signed, and how it
travels"). `sig` is Ed25519 over

    "churr-signed-v1" 0x00 || lp(<name>) || u32(len(signed)) || signed

A receiver verifies over the bytes as received and only then reads
them. A relay passes `signed` and `sig` on unchanged, and whoever
keeps a block keeps those two. `<name>` is the frame's name, or the
block's own name where it travels inside something else:

| `<name>`                                  | `signed` is a     | Signed by                     |
|-------------------------------------------|-------------------|-------------------------------|
| `roster`, `hub_servers`                   | `Roster`          | The hub                       |
| `stream_open`, `server_joined`, `server_left`, `leave`, `owner_changed`, `stream_close` | `Control` | The stream's owner; for `leave`, the leaver |
| `channel`                                 | `ChannelRecord`   | The channel's home hub        |
| `channel_create`                          | `ChannelCreate`   | The server that shares the channel |
| `channel_action`, `join`                  | `ChannelAction`   | The manager's home instance; the joining instance |
| `directory`                               | `Directory`       | The hub                       |
| `move_accept`                             | `MoveAccept`      | The other server of a direct link (section 21a) |
| `channel_commit`                          | `MoveCommit`      | The former owner server       |
| `cluster_member`, `cluster_gone`          | `ClusterMember`, `ClusterGone` | Section 23b      |
| `rehome_answer`                           | `RehomeAnswer`    | The hub that was offered a home |

The content of each is shown below as an object with the message's
field names.

### `roster`

Signed by the hub. Sent after `ready` and whenever it changes. In a
cluster it lists the servers of every hub (section 23b), and is
followed by `cluster`.

    signed: {"hub": "hub.example", "version": 14,
             "at": "2026-11-13T09:30:00.000Z",
             "servers": [{"name": "a.example", "sign_key": "…",
                          "box_key": "…", "box_key_sig": "…",
                          "joined_at": "…"}, …]}

- `version` grows by one with each change. A server ignores a roster
  that is not newer than the one it has.
- A server pins the keys of each name the first time it sees it, in
  `servers` and `peer_servers`. A later roster that gives a pinned
  name different keys is refused as a whole and the owner is told.
- Where a listed server's `/.well-known/churr` can be fetched, the
  server compares it with the roster the first time it sees the name
  and once a day after. A mismatch blocks that server and tells the
  owner.
- A name that disappears from the roster has been removed from the
  hub (section 21).

### Stream control frames

These are the frames of a stream a server owns: any stream on a
direct link, and an ad-hoc channel or sealed chat on a hub. In a
channel a hub owns the only one of them a server sends is `leave`;
everything else there is section 23a's.

Signed blocks. The reference checks the signature and that the
signer is the stream's owner (for `leave`, the leaving server),
applies the change, and relays the frame to every server in the
stream, to a server just added and to a server just removed. When
relaying it adds two things outside the signed part:

- `have`: for each origin, the counter `n` of that origin's last
  envelope in the reference's log at the moment it applied the
  change. A server that is being added starts from there: it takes
  from the log only what comes after, and learns what came before as
  history (section 13).
- `home_hub`: on a hub, the name of the hub that is the stream's
  home and its time reference.

| `frame`         | Signed by | `signed` holds                             |
|-----------------|-----------|--------------------------------------------|
| `stream_open`   | Owner     | `stream_id`, `owner`, `servers` (the owner alone), `version` (1), `at` |
| `server_joined` | Owner     | `stream_id`, `owner`, `server`, `servers` (the whole list after the change), `version`, `at` |
| `server_left`   | Owner     | `stream_id`, `owner`, `server`, `reason` (`left` or `removed`), `servers`, `version`, `at` |
| `leave`         | The leaver | `stream_id`, `server`, `at`               |
| `owner_changed` | Old owner | `stream_id`, `owner` (old), `new_owner`, `servers`, `version`, `at` |
| `stream_close`  | Owner     | `stream_id`, `owner`, `version`, `at`      |

- `version` is the version of the stream's server list, kept in
  `streams.servers_version`. It starts at
  1 and each change must carry exactly the previous version plus one.
  A reference or server that sees any other number refuses the frame
  with `conflict`. The server list is ordered by this number and by
  nothing else: it is its signer's to version, not the clock's.
- The first `stream_open` for a `stream_id` fixes its owner, and on a
  hub its home hub. A second one is refused.
- These frames carry no channel name and nothing about content. A hub
  learns from them which servers are in which stream, and nothing
  else.

### `submit`, `accepted`, `deliver`, `ack`

    {"frame": "submit", "id": 31, "envelopes": [{…}, {…}]}

    {"frame": "accepted", "re": 31, "ids": ["0193d2a0-…"],
     "code": "clock_ahead", "message": "…", "stream_id": "…",
     "origin_event_id": "0193d2a1-…", "clock": {…}}

    {"frame": "deliver", "envelope": {…, "cursor": 4822, "prev": 117}}

    {"frame": "ack", "stream_id": "…", "cursor": 4822}

The envelope is described in section 9. A `submit` or a `deliver`
carries one envelope in `envelope`, or several in `envelopes`, which
may be of several streams; the receiver takes them one by one in the
order given.

A server submits its own events for one stream oldest first, and
sends the next `submit` for that stream when the last has been
answered, so an origin's events reach the reference in the order
they were made. When an answer says the reference took what it was
sent, the next frame leaves at once, whatever wait an earlier
attempt left on the event at its head: that wait is for trying again
after a silence or a refusal.

The reference, for each envelope of a `submit`:

1. Checks it is well formed, stamped (`at` and `n` above 0), and
   within the size limit.
2. Checks `origin` is the server on this connection.
3. Checks the stream exists and the origin is in its server list.
4. Checks the envelope's signature against the origin's pinned key.
5. If its log already holds an envelope with this stream and
   `origin_event_id`, counts it as taken and goes on to the next,
   whatever key epoch it comes under this time. An origin that did
   not hear its event was taken before the epoch changed submits it
   again under the new epoch, with the same identifier and stamp; it
   is in the log and its servers have it. (Refused, the origin would
   stamp it again, and it would stand at one stamp on the servers
   that were delivered it and at another on its own.)
6. Checks the origin's rate limit.
7. Checks the key epoch (section 10): `stale_epoch` if it is below
   the stream's current one; `bad_frame` if it is above it, unless the
   origin is the owner server of a stream a server owns, whose
   envelope under a new epoch begins it.
8. Checks the stamp (section 13): `clock_unknown`, `clock_offset`,
   `clock_ahead`, `clock_behind`. Only the stream's time reference
   does this.
9. Checks that `n` and `at` are both above those of the last
   envelope it took from this origin in this stream:
   `not_monotonic`.
10. Writes it to the end of the stream's log, unchanged, and sends
    `deliver` to every other server in the stream, in log order on
    each connection. The origin is not sent its own envelope back.

One `accepted` frame answers the whole `submit`. `ids` names the
envelopes taken. If one was refused, `code`, `message`, `stream_id`
and `origin_event_id` say which and why, `retry_after_seconds` is
there for `rate_limited`, and for a refusal about the stamp `clock`
carries what the reference holds of the origin's clock, so the origin
can stamp again at once. After a refusal nothing further of that
stream is taken from the same frame: an origin's events never
overtake one another. A `submit` that cannot be read at all is
answered with an `error` frame.

`cursor` and `prev` are added to an envelope by whoever delivers it
from a log, outside the signature. `cursor` is the envelope's
position in that log. `prev` is the `n` of the same origin's
envelope before it in that log, 0 if there never was one. Neither
says anything about where the event is shown (section 13): a log
position is a delivery cursor.

A server sends `ack` after it has stored an envelope, at most once a
second per stream, naming the highest `cursor` it has taken. An owner
server keeping a direct link's log has at most 1,000 unacknowledged
envelopes in flight on one stream; beyond that it stops sending on
that stream until an `ack` arrives. A hub does not wait for `ack`.

### `catch_up` and `caught_up`

    {"frame": "catch_up", "id": 4, "stream_id": "…",
     "have": {"a.example": 211, "c.example": 40}}

`have` is, for each origin, the highest `n` up to which the asker
holds everything the log has given it. The reference sends `deliver`
for every envelope in its log whose `n` is above the asker's figure
for its origin, in log order, the asker's own envelopes excepted. On
the first of each origin it sets `prev` to the asker's own figure:
"this is the next one this log has after what you hold". Then:

    {"frame": "caught_up", "re": 4, "stream_id": "…",
     "have": {"a.example": 230, "b.example": 96, "c.example": 41},
     "floor": {"a.example": 180, "b.example": 0, "c.example": 12}}

`have` is what the log holds of each origin: the `n` of its last
envelope there. `floor` is how far back the log still reaches for
each origin: the `n` up to which that origin's envelopes are gone
from it. If `floor` is above what the asker said it held for an
origin, part of what it missed is no longer in the log, and it
recovers it as section 13 describes.

### `ephemeral`

    {"frame": "ephemeral", "envelope": {…}}

An envelope with no stamp. The reference checks it as it checks a
`submit` (steps 2 to 4 and a separate rate limit), then passes it to
the other servers of the stream that are connected. It is not
logged, not sent back to the origin and not kept for anyone who is
offline.

### `direct`

Section 7.

### `error`

    {"frame": "error", "re": 31, "code": "rate_limited",
     "message": "Too many envelopes.", "retry_after_seconds": 20,
     "stream_id": "…"}

`re` is absent when the error answers no particular frame.

| `code`                | Meaning                                       |
|-----------------------|-----------------------------------------------|
| `bad_frame`           | Malformed, unknown `frame`, or out of place   |
| `version_unsupported` | No shared protocol version                    |
| `auth_failed`         | A handshake signature did not verify          |
| `invite_invalid`, `invite_expired` | The invitation proof matched nothing usable |
| `key_mismatch`        | A name arrived with keys other than the pinned ones |
| `blocked`             | This server has been blocked or removed       |
| `replaced`            | A newer connection for the same peer took over |
| `unknown_server`      | The `to` of a direct message is not on this link |
| `unknown_stream`      | No such stream here                           |
| `not_in_stream`       | The sender is not in the stream's server list |
| `not_owner`           | A control frame came from a server that is not the owner, or tried to change a channel a hub owns |
| `forbidden`           | A manager's action named an account that is not on the channel's manager list; or asked to list a private channel; or an instance asked to join a channel that is joined by invitation; or its request was declined; or a record would make a private channel not private |
| `not_found`           | A manager's action named an instance that has not asked to join, or is not in the channel; or an account that is not on the list it names |
| `conflict`            | A control frame's `version` is not the next one; or a manager's action would leave a channel with no manager; or a hub dialled a hub whose cluster it is in already (a loop) |
| `bad_signature`       | A signed block or envelope did not verify     |
| `too_large`           | Over a size limit                             |
| `rate_limited`        | Over a rate limit; carries `retry_after_seconds` |
| `unavailable`         | The server a direct message is for is not connected; or a change was asked of a channel whose home hub cannot be reached from the hub that was asked (section 23b) |
| `internal`            | The receiver failed                           |
| `clock_ahead`         | An envelope's `at` is more than 30 seconds ahead of the reference clock |
| `clock_behind`        | An envelope's `at` is more than 30 seconds behind it, and its origin was connected then |
| `clock_offset`        | The `offset` an envelope was stamped with is not the one the reference measured |
| `clock_unknown`       | The reference has not measured the origin's clock yet |
| `not_monotonic`       | An envelope's `at` or `n` is not above the origin's last |
| `stale_epoch`         | An envelope is under a key epoch that has ended |

The last six are refusals of a submitted envelope and come in
`accepted`, not in an `error` frame.

## 7. Direct messages

Some things pass between two servers and belong in no stream's log:
an offer to share, a stream key, a file, a call pass. Through a hub
these cross a party that must not read them, so every one is sealed to
the receiving server and signed by the sending server. The same form
is used on a direct link, so there is one code path. A hub sends and
is sent direct messages of its own in the same form: the keys of the
channels it is the home of, and the requests for them.

    {"frame": "direct", "id": 17, "from": "a.example", "to": "b.example",
     "body": <bytes>, "sig": <bytes>}

### Sealing

To seal `plaintext` from server F to server T:

1. Make a fresh X25519 key pair `(e_priv, e_pub)`.
2. `shared = X25519(e_priv, T.box_key)`. If it is all zero bytes,
   stop.
3. `info = "churr-seal-v1" 0x00 || lp(F.name) || lp(T.name)`
4. `key = HKDF-SHA-256(ikm = shared, salt = e_pub || T.box_key,
   info = info)`, 32 bytes.
5. `ct = XChaCha20-Poly1305(key, nonce = 24 zero bytes, plaintext,
   associated data = info)`. The key is used once, so a fixed nonce is
   safe.
6. `body = e_pub || ct`
7. `sig` = Ed25519 by F's `sign_key` over
   `"churr-direct-v1" 0x00 || lp(F.name) || lp(T.name) || u32(len(body)) || body`

To open: check `to` is this server and `from` is a server it knows;
verify `sig` with F's pinned key; derive the key from its own
`box_key` and the first 32 bytes of `body`; decrypt. Any failure drops
the message. The sender's name is inside both the key derivation and
the signature, so a message cannot be re-signed and passed off as
another server's.

A hub checks that `from` is the server on the connection, that `sig`
verifies, and that `to` is on its roster and connected, then passes
the frame on unchanged. A message whose `to` is the hub itself it
opens; one whose `to` is another hub of its cluster it carries there in
`ask` (section 23b). A server takes a direct message from a hub only
over its link to that hub's cluster, under the key the roster gives the
hub, and only of kind `key` or `error`. It answers the sender with `error`,
`unavailable`, if `to` is not connected; it never holds a direct
message for later. It sees the two names and the size.

### Inside

The plaintext is a `Direct` message. Its body is the `kind`; it is
shown here as an object:

    {"kind": "offer", "msg_id": "0193d000-…",
     "at": "2026-11-13T09:30:00.000Z", …}

| Field    | Meaning                                                    |
|----------|------------------------------------------------------------|
| `kind`   | What it is (table below)                                   |
| `msg_id` | A UUIDv7 the sender makes                                  |
| `at`     | The sender's clock                                         |
| `re`     | The `msg_id` this answers, if it is an answer              |

A receiver drops a direct message whose `at` is more than 5 minutes
from its own clock, or whose `msg_id` it has seen in the last 10
minutes. Servers therefore need clocks that are roughly right.

`msg_id`, `at` and `re` are the `Direct`'s own fields; the rest are
the body's. A direct message of a kind the receiver's schema does not
have is dropped.

A request that cannot be met is answered with
`{"kind": "error", "re": …, "code": …, "message": …}`, using the codes
of section 6 plus `forbidden`, `not_found` and `feature_off`.

| `kind`                 | From → to           | Other fields               |
|------------------------|---------------------|----------------------------|
| `offer`                | Owner → invited server | Section 8               |
| `accept`               | Invited → owner     | `stream_id`                |
| `decline`              | Invited → owner     | `stream_id`, `reason`: `declined`, `not_allowed`, `blocked` or `feature_off` |
| `key`                  | Owner → server; in a hub-owned channel, home hub → server | `stream_id`, `epoch`, `key` (32 bytes), `version` (the server list version, or in a hub-owned channel the record version, the epoch belongs to) |
| `key_request`          | Server → owner; in a hub-owned channel, server → home hub | `stream_id`, `epoch`       |
| `history`              | Owner → server; in a hub-owned channel, home hub → an instance that asked, and an instance → its home hub that asked | Section 13 |
| `history_request`      | Server → owner      | `stream_id`, `after_at`, `through_at` (times of the stream's reference clock, in milliseconds; `through_at` 0 for "to now"). In a hub-owned channel, an instance asks its home hub, and with `first: true` in place of the times asks for the snapshot a joining instance is owed (section 23a); the home hub asks the channel's instances, with `first: true`, for all they hold when it becomes the channel's home |
| `history_more`         | Home hub → an instance it gave a short snapshot to | `stream_id`: the hub now holds the channel's earlier history whole; ask for the snapshot again (section 13) |
| `open_request`         | Server → a server that holds the key; in a hub-owned channel, the home hub first | `stream_id`, `envelope`: an envelope of the stream, as delivered, under a key epoch from before the asker joined (section 13) |
| `opened`               | That server → the asker | `stream_id`, `plain`: what the envelope says, as the bytes that were sealed in it (an `Event`), `re` the request |
| `blob_request`         | Server → server; in a hub-owned channel, instance → home hub, and home hub → the file's origin | `stream_id`, `blob_id`, `from_index` |
| `blob_chunk`           | Server → server, the same ways | `stream_id`, `blob_id`, `index`, `last`, `data` (section 15) |
| `call_pass_request`    | Server → call host  | `stream_id`, `call_id`, `account_id` |
| `call_pass`            | Call host → server  | `call_id`, `account_id`, `pass`, `expires_at`, `media` (section 18) |
| `call_signal`          | Either way          | `stream_id`, `call_id`, `account_id`, `pass`, `signal` |
| `device_list`          | Home server → the other server, unasked at every change, or as an answer | `account_id`, `versions`: list of `{"version", "signed_list"}` |
| `device_list_request`  | Server → the account's home server | `account_id`, `by_account_id` (the local member asking), `after_version` |
| `move_propose`         | Owner → the other server of a direct link | `move_id`, `stream_id`, `hub`, `hub_fingerprint`, `expires_at` (section 21a) |
| `move_accept`          | The other server → owner | `move_id`, `stream_id`, `accept` (a signed `move_accept` block), `accept_sig` |
| `move_decline`         | The other server → owner | `move_id`, `stream_id`  |
| `move_abandon`         | Owner → the other server | `move_id`, `stream_id`, `reason` |
| `deaf_ears`            | Server → server, unasked, or as an answer | `deaf_ears`: list of `{"account_id", "account_ids"}`, an account of the sender and the receiver's accounts it has blocked, the whole of what the sender has for the receiver; `ask`: send yours back (section 17a) |
| `error`                | Answer              | `code`, `message`          |

## 8. Streams

### Kinds

| `kind`    | The conversation                          | Owner server          |
|-----------|-------------------------------------------|-----------------------|
| `channel` | A named channel an owner chose to share   | Over a direct link: the server that shared it. On a hub: none; the hub owns it (section 23a) |
| `adhoc`   | An ad-hoc channel with members on more than one server | The server of the member who first added someone from another server |
| `sealed`  | A sealed chat between two people on two servers | The server of the person who started it |

The stream's kind is known to its servers. A hub knows a channel it
owns for what it is, since it keeps its record; of an ad-hoc channel
or a sealed chat it carries it knows no kind (ruling R59).

### Sharing a channel

What follows is sharing over a direct link, and the offer and the
adding of a server are also how ad-hoc channels and sealed chats
start, on a direct link or a hub. Sharing a named channel to a hub is
section 23a: there is no offer from an owner server there, but a
manager's invitation or the hub's directory.

1. The owner of server A opens a channel and chooses "Share", a peer,
   the servers to invite (on a hub; on a direct link there is one),
   how much history to send (0 to 90 days), and the largest file the
   stream will carry.
2. A makes the `stream_id` and the first stream key (epoch 1), and
   sends `stream_open`.
3. A sends each invited server an `offer`.
4. Each invited server's owner sees the offer in their admin console
   with A's name and fingerprint, and accepts or declines. Accepting
   creates a new local channel for it; the accepting owner picks its
   local name, section, and whether it is public or private there. An
   offer with `private` true is always private there, and stays so
   (ruling R87).
5. On `accept`, A adds the server (below).

An offer that is neither accepted nor declined lapses after seven
days. A re-sends an unanswered offer when the invited server was not
connected.

### `offer`

```json
{
  "kind": "offer", "msg_id": "…", "at": "…",
  "stream_id": "0193d100-…",
  "stream_kind": "channel",
  "owner": "a.example",
  "name": "launch",
  "topic": "Getting 2.0 out",
  "private": false,
  "servers": ["a.example", "c.example"],
  "invited": ["b.example"],
  "history_days": 30,
  "max_blob_bytes": 104857600,
  "expires_at": "2026-11-20T09:30:00.000Z"
}
```

`private` is true when the channel is private on the owner server. A
server whose offer details were lost (it restarted before accepting)
takes the channel as private. A private channel can also be shared
through a hub, where it stays private for good (section 23a, "Private
channels").

For `adhoc` and `sealed` there is no `name`, `topic` or `private`, and the offer
also carries `by_account` (the profile of the person adding, in the
shape of section 14), `account_ids` (the accounts on the receiving
server being added) and `members` (the profiles of everyone already in
it). An offer of kind `sealed` also carries `device_lists`, the
starter's device list versions (`account_id`, `versions`), and
`epoch_record`, the chat's epoch record for `key_epoch` 1. Section 16 says how such an
offer is answered without an owner.

### Adding a server

When a server accepts, the owner server does these in order:

1. Sends `server_joined` with the new server list. The reference
   relays it with `have` (section 6) and from then on delivers the
   stream to the new server too.
2. Makes a new stream key and sends `key`, with the next epoch, to
   every server in the new list (section 10).
3. Makes a `stream.servers_changed` event under the new epoch
   (section 11) and waits for the reference to take it. From there on
   the reference takes nothing under an older epoch.
4. Sends the new server `history`: what the owner chose to share of
   everything it holds at that moment (section 13). For a private
   channel, only what was posted from the first join of the earliest
   of the new server's accounts in it, and nothing while it has none
   there: its people read from their first join (PERMISSIONS.md
   section 4.4).

The new server cannot open an envelope it is delivered under an
epoch below the one it joined at: those were encrypted under a key
it was never given, and what it is meant to have of them arrives as
history. It announces its own members as they join the new local
channel (`member.joined`), once it knows its offset from the stream's
time reference. What its members said in the channel between its
owner accepting the offer and the owner server adding it waited in
its outbox; it is stamped again, after those announcements, and sent
then, since the other servers accept nothing from an account they
have not been told has joined.

### A stream's life on each server

`streams.state` is one of `offered`, `active`, `left` (this server
left) and `removed` (the owner removed it, or closed the stream).
Section 21 covers the ends.

## 9. The envelope

```json
{
  "stream_id": "0193d100-7c1e-7b3a-9f0e-2a4d5c6b7e8f",
  "key_epoch": 3,
  "origin": "b.example",
  "origin_event_id": "0193d2a0-11c4-7e02-b6a1-90c2f4a7d310",
  "at": 1794557400532,
  "n": 118,
  "offset": -412,
  "ciphertext": <bytes>,
  "sig": <bytes>,
  "cursor": 4822,
  "prev": 117
}
```

| Field             | Set by    | Meaning                                  |
|-------------------|-----------|------------------------------------------|
| `stream_id`       | Origin    | The stream                               |
| `key_epoch`       | Origin    | Which stream key encrypted it. From 1.   |
| `origin`          | Origin    | The name of the server that made it      |
| `origin_event_id` | Origin    | A UUIDv7, new for each event. Names the event for duplicate checks. |
| `at`              | Origin    | The stamp, part one: the time of the stream's reference clock, in milliseconds since 1970, as the origin read it when it made the event: its own clock minus `offset`. |
| `n`               | Origin    | The stamp, part two: the origin's counter in this stream. It only grows. |
| `offset`          | Origin    | The stamp, part three: the clock offset the origin used, in milliseconds, as the reference told it (section 5). May be negative. |
| `ciphertext`      | Origin    | 24 bytes of nonce, then the XChaCha20-Poly1305 output |
| `sig`             | Origin    | Ed25519 signature by the origin          |
| `cursor`, `prev`  | Whoever delivers | The envelope's position in the deliverer's log, and the `n` of the same origin's envelope before it there. Absent on `submit` and `ephemeral`. Not signed. |

`at`, `n` and `offset` are absent, and count as 0, on an `ephemeral`
envelope. At most 262,144 bytes of `ciphertext`.

The envelope is the schema's `Envelope` message, and these are its
fields: none of it is opaque to a carrier but `ciphertext`. A log
keeps it as that message, and whoever delivers it from a log sends
what it kept with `cursor` and `prev` added.

### Encryption

    header = id16(stream_id) || u32(key_epoch) || lp(origin)
             || id16(origin_event_id)
             || i64(at) || i64(n) || i64(offset)

    nonce  = 24 bytes from the system's random source, new every time

    sealed = XChaCha20-Poly1305(key   = the stream key of key_epoch,
                                nonce = nonce,
                                plaintext = the inner event, an Event
                                            message (section 11),
                                associated data =
                                  "churr-envelope-aad-v1" 0x00 || header)

    ciphertext = nonce || sealed

`at` is in milliseconds here and in the signature, as the origin
read it; on the wire it is the same instant in microseconds. The
associated data ties the ciphertext to its stream, epoch, origin, event identifier
and stamp, so it cannot be lifted into another envelope or moved in
time.

### Signature

    sig = Ed25519(origin's sign_key,
                  "churr-envelope-v1" 0x00 || header
                  || u32(len(ciphertext)) || ciphertext)

This byte layout is what is signed: not the `Envelope` message, and
not any bytes a carrier could write differently (section 2). A
verifier builds it from the fields it read. **The stamp is under the
origin's signature.** Whoever carries
an envelope, a hub included, can refuse it or lose it; it cannot give
it another time, another counter or another offset, because any of
those breaks both the signature and the seal. `cursor` and `prev`
are the carrier's own words about its own log and are not signed.

### The stamp

The server that makes an event stamps it, in the transaction that
makes the event:

- `offset` is the offset the stream's time reference last told this
  server (`peers.clock_told`). A server that has never been told one
  by that reference cannot make events in the stream: the change
  fails with `unavailable`. When the server is itself the reference,
  `offset` is 0.
- `at` is the server's clock minus `offset`. Two rules on top: `at`
  is greater than every `at` this server has already applied or made
  in the stream (`streams.max_at`), so a reply always sorts after
  what it answers; and so it is greater than this server's own last
  `at`.
- `n` is one more than the server's last (`stream_servers.last_n`).
  A stamp taken for an event that turns out to stay on its own
  server (a pin, a level) is simply not used: counters may skip.

An event keeps its stamp for good. Encrypted again under a newer key
epoch it has the same `origin_event_id`, `at`, `n` and `offset`. Only
a refusal of the stamp itself gives it a new one (section 13).

### What a server checks on `deliver`

In this order. "Reject" means: nothing is stored or shown, the
envelope still counts as held (the origin's counter moves past it),
and a `link.event_rejected` line goes to the audit log with the
reason (at most one line per stream, origin and reason per hour). In
a channel a hub owns, an event this server failed to apply because it
failed itself is not rejected but kept and tried again (section 13,
"When this server fails").

| Step | Check                                             | If it fails        |
|------|---------------------------------------------------|--------------------|
| 1    | The stream is known and `active`, and the frame came on its peer's connection | Drop the frame |
| 2    | `prev` is not above what this server holds of the origin (`stream_servers.last_n`) | Something of this origin was missed. Drop the envelope and send `catch_up`; it comes again |
| 3    | `origin` is in the stream's server list           | Reject             |
| 4    | `sig` verifies against the origin's pinned key    | Reject             |
| 5    | The key for `key_epoch` is held                   | If this server holds no key at all yet (it has just joined), wait for the first. If `key_epoch` is below every epoch held, reject quietly: it is from before this server joined. Otherwise hold for up to 60 seconds and send `key_request`; then reject |
| 6    | It decrypts                                       | Reject             |
| 7    | The plaintext is an `Event` this server can read  | Reject             |
| 8    | `origin_event_id` is not in `stream_seen`         | Reject quietly as a duplicate |
| 9    | `at` is not more than 30 seconds, plus this server's uncertainty, ahead of the reference clock as this server reads it; and not more than 35 days behind it | Reject. A server that has no reading of the reference clock skips this |
| 10   | `type` is known                                   | Store nothing, so a newer server's events do not trouble an older one |
| 11   | The origin has authority for this event (section 12) | Reject          |
| 12   | The event is valid: the limits of CONVENTIONS.md, the shapes of MESSAGE-FORMAT.md, the things it refers to exist here | Reject. In a stream that comes through a hub, an event about a message that is not here is first held for it (section 13) |

An event that passes is applied in one transaction: the change is
stored in the local conversation under this server's own next `seq`,
a message it creates takes its place from the stamp (section 13),
`origin_event_id` is written to `stream_seen`, and the origin's
`last_n` and `last_at` move. Then it is sent to the local members as
a live event and acknowledged. A rejected event moves the origin's
`last_n` and nothing else: a time this server would not accept does
not push its own stamps forward.

Envelopes are applied as they arrive. Arrival order decides nothing
about where an event is shown.

A server applies its own events when it makes them, not when they
come back: a reference does not deliver an origin its own envelopes.

## 10. Stream keys and epochs

- A stream key is 32 random bytes. The owner server makes it; in a
  channel a hub owns, the home hub does.
- Keys are numbered by **epoch**, from 1. Each server keeps every
  epoch's key it was given (`stream_keys`, sealed at rest), so it can
  read what it already holds.
- The owner sends a key to a server in a `key` direct message, which
  is sealed to that server's `box_key` and signed by the owner
  (section 7). A `key` message is accepted only from the stream's
  owner.
- A server that is delivered an envelope under an epoch it has no key
  for asks the owner with `key_request`. The owner answers with `key`
  if the asker was in the server list for that epoch, and with an
  error otherwise.

### When the epoch changes

The owner starts a new epoch on every change to the server list, a
server joining included, so a server never holds a key from before it
joined or after it left. The owner may also start one at any time,
for example after a suspected leak. Epochs are the owner's to number
and are not ordered by the clock.

1. The owner sends `key` for epoch E to every server in the list.
2. The owner makes a `stream.servers_changed` event encrypted under
   E, applies it, and submits it.

A stream's **current epoch** is, for each party, the highest one
whose `stream.servers_changed` it has seen: for the owner the one it
made last; for any other server the one it applied last; for a
reference, the one it took into its log last. A hub sees the
`key_epoch` each envelope names, and takes the owner's word, and only
the owner's, for a new one: its current epoch is the highest
`key_epoch` on an envelope it has taken from the stream's owner.

- Servers encrypt under the stream's current epoch as they know it.
  A server that has been given the key of an epoch that has not
  started yet does not use it until it has.
- A reference refuses an envelope under an epoch below the current
  one with `stale_epoch`. Its origin encrypts the same event again
  under the current epoch, with the same `origin_event_id` and the
  same stamp, once it has applied the `stream.servers_changed` it was
  missing, and submits it again.
- A reference refuses an envelope under an epoch above the current
  one, from anyone but the owner, with `bad_frame`: no server that
  keeps this section sends one.
- So in a reference's log nothing under an older epoch follows the
  event that started a newer one, and a server that reads the log in
  order never meets an epoch before its start.

**In a channel a hub owns** the home hub owns the epochs and makes
every key (ruling R109). On every change to the server list it signs
a record that calls for the next epoch (section 23a), and in the
transaction that signs it the hub makes the epoch's key, keeps it
sealed at rest beside every earlier one with the server list it was
made for, and makes the epoch the stream's current one: from that
moment it takes nothing under an older epoch (`stale_epoch`) and
nothing under a later one (`bad_frame`). There is no
`stream.servers_changed` in such a channel; an instance rejects one. The
hub sends `key` to every instance in the list, sealed to each, ahead of
the record, again with the record to an instance that reconnects, and
over a trunk in `forward` to an instance on another hub of the cluster.
A hub that a channel was handed to sends each instance the record by
which the old home handed it over first, then the key, then its own
record: the instance takes the key only once it knows that hub for the
home. To an instance on another hub of the cluster the key and the
records travel different ways (the key in `forward`, the records hop
by hop, the old home's after it steps down), so a key may still arrive
first. An instance therefore keeps, for 60 seconds, a key of a
hub-owned channel from a hub it does not know for the channel's home
yet, and takes it when a record it takes names that hub the home; a
hub's holder likewise keeps a key of a channel it does not hold yet
until the record that makes its hub the home arrives. Only a key from
a hub that could be the home is kept so: the hub the key came by, or
another hub of its cluster under the key the roster gives it. A key
from anyone else, a member instance above all, is dropped at once
(`key_not_from_owner`), as is a kept key whose sender no record names
the home.
An instance takes such a channel's key from its home hub, under the key
the cluster roster gives that hub, and from nobody else. Its current
epoch is the one the newest record it holds calls for, once it holds
that epoch's key; until then it goes on under the one before, and
what the hub refuses as `stale_epoch` waits for the key. An instance
that holds a record without its epoch's key, or meets an envelope
under an epoch it has no key for, asks the home hub with
`key_request`. The hub answers an instance in the channel now that was
in the list the epoch was made for, and refuses any other with
`forbidden`. An edge hub holds none of a channel's keys (section
23b).

A removed server is not sent the new key, and the reference stops
delivering to it. Envelopes sent under the old epoch between the
removal and the reference taking the new `stream.servers_changed`
can still be read by the removed server if someone passes them to
it; that window is as short as the owner's connection to the
reference makes it.

## 11. Inner events

The plaintext of an envelope is one event: an `Event` message whose
body is the event's type. It is shown here as an object with

| Field  | Meaning                                                     |
|--------|-------------------------------------------------------------|
| `type` | The event name. The names are those of the local system.    |
| `at`   | When it happened, by the origin's own clock, as a time. For display and for fields that default to it (`deleted_at`). It orders nothing: the envelope's stamp does. |

followed by the fields in the tables below, which are the fields of
the type's body (`at` is the `Event`'s own). A receiver turns an
inner event into a local event by adding its own `conversation_id`
and its own next `seq`.

An event's place in the conversation and its origin's counter are
not in here. They are in the envelope's stamp (section 9), where the
time reference can check them without reading the event and where
the origin's signature covers them.

An event that stays on the server it was made on (a pin, a level, an
archive, a server's own name for its copy of a channel) is not sent
at all.

### Wire shapes

**A message on the wire** (the schema's `Message`; its `quote`,
`cards`, `form`, `via`, `sealed`, `chance` and `view` are the client
schema's messages, and each entity is the client schema's with
`stream_id` beside it) is the message object of
`MESSAGE-FORMAT.md` section 1 with these differences:

- Left out, because each server works them out for itself:
  `conversation_id`, `seq`, `order`, `late`, `unsent`, `reactions`,
  `thread`, `pinned`, `has_history`, `deleted_at`, `deleted_by_id`,
  and inside `form`:
  `tally`, `voters`, `my_answer` and `answer_count`.
- Left out of every file entry: `url` and `thumb_url`. Added to every
  file entry: `sha256`, the hash of the file's bytes.
- `entities` are rewritten by the origin: `account`, `broadcast`,
  `link` and `task` stay as they are (account identifiers are the
  same on every server); `emoji` stays and the emoji is listed in
  `blobs`; `group` is removed, since groups do not cross servers, and
  the text shows as typed; `conversation` is removed unless it is the
  stream's own channel, in which case it carries `stream_id` in place
  of `conversation_id` and each receiver puts its local identifier
  back.
- `text` is never rewritten; it travels exactly as the author sent
  it. Names in it mean what they meant on the author's server, and
  the entities say what each resolved to. A member who wants to name
  something of another server types the long forms of
  `MESSAGE-FORMAT.md`: `@name@server.name` for an account and
  `:name@server.name:` for a custom emoji. Each resolves, on the
  author's home server, only in a conversation shared with that
  server, and becomes an ordinary `account` or `emoji` entity.
- `action` (a `/me` message) and `chance` (a result the origin's
  server drew, ruling R67) travel as they are. The receiver checks that
  a `chance` holds together (`MESSAGE-FORMAT.md` section 1.2: known
  kind, cards from the named deck) and refuses the event as `invalid`
  if not; it cannot tell whether the draw was fair, and takes the
  origin's word for that. A server too old to know them shows the text,
  or the `fallback`.
- `view` (the message as a value of the catalogue, such as a roll) and
  each `object` entity's value travel as the origin holds them, in
  events, in history and from a hub's holder. Every receiver holds each
  value to its kind's check (`DESIGN-MODULES.md` section 4.2; for
  `dice_roll`, `testdata/types/dice_roll.json`). One that fails falls
  back a step: to its `alt`, checked the same way, or to nothing, so a
  view is left off and an object entity is dropped, and its characters
  read as text. That is no refusal: the message still arrives, with its
  text. A value of a kind the receiver does not know, or of a level above
  the one its check reads, is carried unchanged, its data the bytes it
  came as. As with `chance`, the check is of the shape, not the
  fairness. A sealed message carries none: its values travel inside its
  payload.

**`blobs`** lists every file an event refers to: attachments, card
images, the via icon, custom emoji, an avatar.

    {"id": "0193a700-…", "sha256": "…", "size": 482113,
     "media_type": "image/png", "purpose": "attachment"}

`purpose` is `attachment`, `card`, `emoji` or `avatar`. An emoji entry
also has `name`. Section 15 says how the bytes follow.

**A profile on the wire** is section 14's shape.

### Messages and what hangs on them

| `type`                    | Fields                                    |
|---------------------------|-------------------------------------------|
| `message.created`         | `message` (wire shape), `blobs`           |
| `message.edited`          | `message` (the whole new content, wire shape), `blobs`. If `message.edited_at` is absent the edit was a quick correction: receivers replace the content and keep no earlier version. |
| `message.deleted`         | `message_id`, `deleted_by_id` (an account), `deleted_at` |
| `message.removed`         | `message_id`, `removed_by_id` (an account of the owner server; in a hub-owned channel, a manager or moderator who is an account of the origin), `removed_at`, and in a hub-owned channel `removed_by` (their wire profile) and `blobs` |
| `message.purged`          | `message_id`, `erase`: `history` (earlier versions only) or `content` (everything but the record that a message was there) |
| `messages.expired`        | `through_at` (a time of the stream's reference clock, in milliseconds), `scope`: `own` or `all` (section 22) |
| `preview.added`           | `message_id`, `cards` (the link-preview cards), `blobs` |
| `preview.removed`         | `message_id`, `url`                       |
| `reaction.added`, `reaction.removed` | `message_id`, `account_id`, `emoji`: a standard emoji as a string, or `{"id", "name", "server"}` for a custom one, which is then also in `blobs`. A receiver stores a custom reaction as `:name:` if the emoji is its own and as `:name@server.name:` otherwise. |
| `task.changed`            | `message_id`, `item_id`, `checked`, `account_id` |

**Threads** need no events of their own. A reply is a
`message.created` whose message has `parent_id` (and `quote`). Each
server builds the parent's reply count, last reply and marker from the
replies it holds. Who follows or ignores a thread, and each person's
read position, are personal and stay on their home server.

### Forms

| `type`                | Fields                                        |
|-----------------------|-----------------------------------------------|
| `form.answered`       | `message_id`, `form_id`, `account_id`, `answer_box` |
| `form.answer_withdrawn` | `message_id`, `form_id`, `account_id`       |
| `form.answer_refused` | `message_id`, `form_id`, `account_id`, `answer_event_id` (the `origin_event_id` of the answer), `reason`: `closed`, `full` or `invalid` |
| `form.option_added`   | `message_id`, `form_id`, `field_id`, `option` (`id`, `label`), `added_by_id` |
| `form.tallied`        | `message_id`, `form_id`, `answer_count`, `tally`, `voters` (named forms only) |
| `form.closed`         | `message_id`, `form_id`, `closed_by_id`, `closed_at`, and the final `answer_count`, `tally` and `voters` |

Section 19 explains them.

### People and the conversation

| `type`                 | Fields                                       |
|------------------------|----------------------------------------------|
| `member.joined`        | `account` (wire profile), `blobs`, `joined_at` |
| `member.left`          | `account_id`, `reason`: `left`, `removed` or `account_removed` |
| `member.invited`       | `account_id`, `by_account_id`. Ad-hoc streams only (section 16). |
| `member.requested`     | `account` (wire profile), `blobs`. Someone asks to be admitted to a private channel a hub owns; their home instance sends it (section 23a) |
| `member.admitted`      | `account_id`, `server` (their home instance), `by_account_id`, `by_account` (wire profile), `blobs`. A moderator or manager admits someone to a private channel a hub owns; the acting account's home instance sends it |
| `member.request_declined` | The same fields. A moderator or manager declines a request to join a private channel a hub owns |
| `member.removed`       | The same fields. A moderator or manager removes someone from a channel a hub owns, on every instance |
| `account.updated`      | `account` (wire profile), `blobs`            |
| `conversation.updated` | `name`, `topic`. Only the fields that changed. |

### Calls

| `type`         | Fields                                               |
|----------------|------------------------------------------------------|
| `call.started` | `call_id`, `started_by_id`, `host` (a server name), `message_id` (the "call started" notice) |
| `call.participant_joined`, `call.participant_left` | `call_id`, `account_id` |
| `call.ended`   | `call_id`, `ended_at`                                |

### The stream itself

| `type`                   | Fields                                     |
|--------------------------|--------------------------------------------|
| `stream.servers_changed` | `version`, `servers` (the whole list), `joined` and `left` (lists of names, either may be empty), `epoch` |
| `stream.owner_changed`   | `new_owner`, `version`                     |
| `stream.limits_changed`  | `max_blob_bytes`                           |
| `stream.closed`          |                                            |
| `stream.moved`           | `move_id`, `hub`, `record` and `record_sig` (the hub's signed record of the channel), `commit` and `commit_sig` (the owner server's signed commit). The owner server's last event in the direct log of a stream it has moved onto a hub (section 21a) |

`stream.servers_changed` is not used in a hub-owned channel: the
home hub begins every epoch there itself (section 10), and the server
list is its record's. A server rejects one there (`hub_channel`).

On `stream.servers_changed`, a server marks every account of each
server in `left` as having left the conversation. Those
accounts' home server is no longer there to say so itself.

### Sealed chats

In a `sealed` stream the events carry what the two people's devices
made, and no server can read the inside. `SEALED.md` is the authority
on what they mean.

| `type`                 | Fields                                       |
|------------------------|----------------------------------------------|
| `message.created`      | `message`: the sealed message object of `MESSAGE-FORMAT.md` section 9 without `conversation_id`, `seq` and `order`, that is `id`, `author_id`, `client_id`, `sealed` (`key_epoch`, `ciphertext`), `quiet`, `created_at`; `blobs` for the encrypted files it carries |
| `sealed.epoch_started` | `key_epoch`, `account_id` and `device_id` (the device that made the record), `record` (the epoch record, as bytes) |

- `key_epoch` here is the sealed chat's own: a plain counter from 1,
  one per epoch record. It has nothing to do with the envelope's
  `key_epoch`, which is the stream key's.
- An epoch record is made by one device and holds the new epoch
  secret sealed separately to every device on both accounts' current
  device lists. Servers carry it and store it (`sealed_epochs`) and
  cannot open it. Creating the chat is epoch 1; a device makes a new
  epoch when either account's device list changes, or when the
  current epoch is more than 7 days old as someone sends.
- Edits, deletes, reactions and ticks in a sealed chat are sealed
  messages too, marked `quiet`. No other message event is used in a
  `sealed` stream.
- There is one record per `key_epoch`. A `sealed.epoch_started` for
  the key epoch one above the chat's current one is stored and makes
  that epoch current. A server refuses to make a second record
  itself for an epoch it holds one for, and tells its own member's
  device, which starts again from the record that stands.
- **Two records for one key epoch.** Devices on the two servers may
  each make the next record before either has the other's, and then
  each server holds its own. Nobody numbers events, so the stamp
  settles it, the same on both servers: **the record whose event has
  the earlier stamp stands, and replaces a later one already held.**
  A server that is delivered a `sealed.epoch_started` for a key
  epoch it already holds another record for compares the order of
  the two stamps (section 13; a server keeps the stamp with each
  record, `sealed_epochs.ord`). If the one that arrived is later, it
  is rejected quietly. If it is earlier, and it would have been the
  chat's next record when the chat stood at the epoch before, it
  takes the place of the record held: the chat's current epoch does
  not move, and the server's own devices are sent
  `sealed.epoch_started` again with the record that stands and
  `replaced: true`. The first record of a chat is its starter's and
  is never replaced. Which server is the stream's owner or its time
  reference gives a record no standing.
- What a device does when its record is replaced is as when it loses
  the race on its own server (`SEALED.md` section 5.5): it takes the
  record that stands. What it had already sent under its own record
  cannot be opened by the other person's devices, which never held
  that record; the device sends it again under the record that
  stands.
- A `message.created` whose `sealed.key_epoch` is not the chat's
  current one where it arrives is rejected quietly, and if it came
  from this server's own member the device is told to send again.

### Ephemeral events

Carried in `ephemeral` frames, never stored.

| `type`              | Fields                                          |
|---------------------|-------------------------------------------------|
| `typing.started`    | `account_id`, `parent_id` (when typing in a thread). Lasts 8 seconds unless repeated. |
| `presence.changed`  | `account_id`, `presence` (`online`, `away`, `offline`), `in_call`, `dnd`, `status` (`emoji`, `text`, `until`), `availability` (`value`, `until`) |
| `presence.snapshot` | `accounts`: a list of the same objects, for every member of this stream on the origin that is not offline |

## 12. Who may say what

Every receiving server checks every event itself. It relies neither
on the time reference nor on the sender being honest. An envelope is
attributed to its `origin`, whose signature has been verified; the
table says which origin an event is accepted from.

"Home of X" means X's home server. A server knows an account's home
from the profile that came with its `member.joined`, and an account's
home never changes.

| Event                    | Accepted only when the origin is             | And only if                                  |
|--------------------------|----------------------------------------------|----------------------------------------------|
| `message.created`        | Home of `message.author_id`                  | The author is a member of the conversation; a form, if any, uses `tally`; for `adhoc` and `sealed`, section 16 |
| `message.edited`         | Home of the message's author, as stored      | The message exists and is not deleted        |
| `message.deleted`        | Home of the message's author                 | `deleted_by_id` is an account of that same server |
| `message.removed`        | The owner server. In a hub-owned channel: the home instance of a manager or moderator | `removed_by_id` is an account of the origin; in a hub-owned channel it is on the hub-signed manager or moderator list as an account of the origin |
| `message.purged`         | Home of the message's author, or the owner server |                                         |
| `messages.expired`       | Any server for `scope` `own`; the owner server for `scope` `all` |                           |
| `preview.added`, `preview.removed` | Home of the message's author | At most three preview cards   |
| `reaction.added`, `reaction.removed` | Home of `account_id`             | The account is a member                      |
| `task.changed`           | Home of `account_id` (who ticked, not who wrote the list) | The account is a member         |
| `form.answered`, `form.answer_withdrawn` | Home of `account_id`         | The account is a member                      |
| `form.answer_refused`    | Home of the form's message's author          |                                              |
| `form.option_added`, `form.tallied`, `form.closed` | Home of the form's message's author |                             |
| `member.joined`          | Home of `account.id`, which must be the origin named in the profile | For `adhoc`, a matching `member.invited` came first; for `sealed`, never after the stream's first two |
| `member.left`            | Home of `account_id`                         |                                              |
| `member.invited`         | Home of `by_account_id`                      | `adhoc` streams; `by_account_id` is a member |
| `member.requested`       | Home of `account.id`                         | A private channel a hub owns; not a bot      |
| `member.admitted`, `member.request_declined` | Home of `by_account_id`  | A private channel a hub owns; `by_account_id` is on the hub-signed manager or moderator list as an account of the origin (`not_moderator` otherwise) |
| `member.removed`         | Home of `by_account_id`                      | A channel a hub owns; `by_account_id` is on the hub-signed manager or moderator list as an account of the origin (`not_moderator` otherwise) |
| `account.updated`        | Home of `account.id`                         | The profile's `version` is higher than the one held |
| `conversation.updated`   | The owner server                             | `channel` streams only                       |
| `call.started`           | Home of `started_by_id`, which must equal `host` | No call is live in the conversation         |
| `call.participant_joined`, `call.participant_left`, `call.ended` | The call's `host` | The call is live              |
| `stream.servers_changed` | The owner server. In a hub-owned channel: nobody (`hub_channel`) | `version` is the next one; `epoch` is higher than the last; it is encrypted under `epoch`; it agrees with the `server_joined` and `server_left` frames received |
| `stream.moved`           | The owner server                             | This server has agreed to the move named; the hub's record verifies under an accepted key of the hub agreed to and lists exactly the two servers; the commit is signed by the owner server (section 21a) |
| `stream.owner_changed`   | The owner server (the old one)               | `new_owner` is in the server list            |
| `stream.limits_changed`, `stream.closed` | The owner server             |                                              |
| `message.created` in a `sealed` stream | Home of `message.author_id`    | The author is one of the two; the message has the sealed shape and nothing else; section 16 |
| `sealed.epoch_started`   | Home of `account_id`                         | The account is one of the two; `key_epoch` is the chat's current one plus one, or one a record is held for and this one's stamp is earlier (section 11) |
| `typing.started`, `presence.changed`, `presence.snapshot` | Home of every account it names | Each account is a member of this stream; never stored |

What a receiver does with an event that fails: it rejects it as
section 9 defines. Nothing is stored, nothing is shown to members,
and the audit log gets a line. Particular cases:

| Failure                                         | Also                               |
|-------------------------------------------------|------------------------------------|
| An event names an account of another server (forged authorship) | The audit line names both servers. After 100 such rejections from one origin in an hour, the stream's page in the admin console shows a warning. The server does not leave by itself: through a hub it cannot tell whether the origin or the hub is at fault. |
| `stream.servers_changed` or a `key` not from the owner (in a hub-owned channel, a `key` not from its home hub) | The server list and the epoch stay as they were |
| `message.removed` not from the owner, or in a hub-owned channel by an account that is not on the manager or moderator list as the origin's | The message stays |
| A `member.joined` with no matching invitation in an `adhoc` stream | The account is not added; its later messages fail the membership check and are rejected too |
| An event about a message this server does not hold (it was before this server's history) | Rejected quietly, with no audit line |
| A server would reject an event of its own making | It does not make it: the change fails on the server that tried it, and nothing is sent |

A server may always act on its own copy: hide a message locally, mute
a remote account, leave a stream, drop a peer. None of those travel,
and none of them need anyone's authority.

**Levels do not travel.** Each server keeps the levels of its own
accounts in its own copy and enforces them on its own members. Remote
accounts are held at level `member`. A manager on a server that is
not the owner can manage that server's own members and hide messages
locally; only the owner server can remove a message for everyone. In
a hub-owned channel no instance has that power and the channel's
managers and moderators do, each through their own home instance,
whatever their level there (section 23a). A remote account is never a
manager of another instance's copy.
Slow mode, the word filter, time-outs, pins, archiving, channel modes
and retention settings are likewise each server's own.

## 13. Order, the checks on time, unsent messages, duplicates and catching up

### Order

A shared conversation is ordered by a clock all its servers share.
Nobody hands out positions.

- Every stream has a time reference: its home hub, or on a direct
  link its owner server. Every server keeps the offset of its own
  clock from the reference's, as the reference measured it
  (section 5), refreshed at least every 30 seconds while connected
  and kept while it is not.
- Every event carries a stamp its origin made and signed (section 9).
- **The order of events in a shared conversation is by
  (`at`, origin server name, `n`).** `at` is compared as a number,
  the name byte by byte, `n` as a number. Every server and every hub
  works this out identically from the envelopes alone.
- A server shows a conversation in that order whatever order the
  events arrived in. Each server still gives everything it stores
  its own next `seq`: that is its arrival number, which serves the
  events log, clients' catch-up, read positions and unread counts,
  and no longer says where a message is shown. A `seq` means nothing
  on another server.

**`order`.** Every message carries `order`, a text that sorts as the
order does when compared character by character. Clients sort by it
and read nothing out of it. A server makes it as

    order = hex16(at) "!" origin "!" hex16(n)

sixteen lowercase hexadecimal digits each for `at` and `n`; `!`
sorts below every character a server's name can hold, so a name that
begins another sorts first. A message that no stamp places takes
`at` = F, an empty origin and `n` = its `seq`: F is 0 in a
conversation that was never shared, so what a channel held before it
was shared comes before everything stamped, in the order it had; and
when a stream ends, F for that conversation becomes one more than the
highest `at` it held (`conversations.order_floor`), so what is posted
in the local copy afterwards comes after all of it. In a
conversation that is not shared, `order` therefore follows `seq`.

**`late`.** A message that arrives on a server below the highest
`order` of any message already delivered there
(`streams.top_ord`) is marked `late`. It takes its place by `order`
all the same. Its `seq` is that server's next, so it is unread like
any new message. `late` is each server's own observation: the same
message may be late on one server and not on another.

**How close is close.** Two events made on different servers within
their combined clock uncertainty of each other may be shown in either
order. It is the same order on every server.

**What is not ordered by the clock.** Changes that need one
authority are versioned by their signer and applied by version: the
server list (`version` on control frames and in
`stream.servers_changed`), key epochs, ownership. Two edits to one
message are settled by the later stamp, whichever arrives first: a
server remembers the stamp of the edit a message's content came from
(`messages.edit_ord`) and ignores an edit with an earlier one. A
delete stands whenever it arrives.

### The checks on time

**The time reference**, on every envelope submitted to it, against
what it has itself measured of the origin's clock:

| Check | Refusal |
|-------|---------|
| It has measured the origin's clock at least once. | `clock_unknown` |
| `offset` is the offset it measured for that origin, within a tolerance of three times its own uncertainty about that origin, and never less than 100 ms. | `clock_offset` |
| `at` is not more than 30 seconds ahead of its clock. | `clock_ahead` |
| `at` is not more than 30 seconds behind its clock, unless `at` falls inside a stretch the origin was disconnected for, give or take 30 seconds. | `clock_behind` |
| `at` and `n` are both above those of the last envelope it took from this origin in this stream. | `not_monotonic` |

A stretch of disconnection runs from when the reference last
measured the origin's clock before the connection was lost (at most
30 seconds before it was) to when the origin's next connection was
accepted. The reference keeps the last 8 per server
(`peers.away`; on a hub, its own table) and lets one excuse an old
`at` for 24 hours after it ended. Where the reference is a hub of a
cluster and the origin is on another hub, the path to the origin is
that server's connection to its own hub and the trunks between: its
own hub makes these checks, and section 23b says what the hubs on
the way, the reference among them, check after it.

**Every other server**, on every envelope delivered to it: `at` is
not more than 30 seconds, plus its own uncertainty, ahead of the
reference clock as it reads it. It cannot judge "behind": it does
not know when another server was disconnected, and the reference
does.

**The origin, when refused.** The refusal comes back in `accepted`
(section 6). For `clock_ahead`, `clock_behind`, `clock_offset` and
`not_monotonic` the origin stamps the event again and submits it
again:

1. It takes the reference's figures for its clock from the refusal
   as its new offset.
2. The refused stamp and every stamp it gave after it are void:
   nobody else has seen them. `streams.max_at` goes back to the
   highest `at` that stands: the last one the reference acknowledged
   (`streams.acked_at`) or the last applied from another server.
3. The refused event, and every event of the stream waiting behind
   it, is given a new `at` and a new `n`, in the order they were
   made. `origin_event_id` stays.
4. A message among them moves to where its new stamp puts it, and
   local members are told with `message.placed`.

If the refusal was for the time and the offset in it is the one the
origin used, the origin's clock has moved since the reference
measured it. Stamping again at once would give the same wrong time,
so the origin waits until a `ping` tells it a different offset,
which is a few pings away, and stamps again then. After five
such refusals of one event it waits as for any other refusal, twice
as long each time from 5 seconds up to 5 minutes, and stamps the
event again when it is refused after that wait: a stamp only grows
older, so the event is never left to be offered as it is for good.
Only refusals for the stamp count towards the five. Being told to
slow down (`rate_limited`) does not, nor does being sent again after
a wait.

### Unsent

When a local member changes anything in a shared conversation:

1. The server makes the change at once, exactly as in a conversation
   that is not shared, with its own next `seq`. Each event the change
   produces is stamped as it is produced. A message is stored with
   the `order` of its event's stamp and is shown in its place
   immediately.
2. In the same transaction each event that travels is written to
   `outbox`, unencrypted, with its stamp; or, when this server is
   the stream's time reference, straight into the stream's log.
3. The server submits what is in the outbox, oldest first. When the
   reference answers `accepted`, the row is deleted.

A message is **unsent** while its event is in the outbox, and that is
all "unsent" means: the link has not acknowledged it yet. The message
object carries `unsent: true`, and `message.sent` tells local members
when it no longer applies. A message of the reference's own is never
unsent.

- If the reference cannot be reached, everything waits in the outbox
  and goes at once on reconnection, and otherwise with a wait that
  doubles from 5 seconds to 5 minutes (`outbox.attempts`,
  `outbox.next_try_at`). Nothing is refused for want of a
  connection: edits, reactions, ticks and form answers queue like
  messages.
- An edit or delete of an unsent message is an ordinary event behind
  it in the outbox.
- An event is encrypted when it is submitted, under the stream's
  current epoch, so one that waits through a change of epoch leaves
  under the new one with the stamp it was given.
- While the oldest row waits out a refusal nothing passes it.

### Duplicates

- A reference takes the same `(stream_id, origin_event_id)` into its
  log once. A second `submit` of it is acknowledged again and
  nothing else happens.
- Each server writes every applied `origin_event_id` to `stream_seen`
  and rejects an envelope whose identifier is already there, however
  it arrives. A server writes its own events there when it makes
  them.
- `stream_seen` rows may be dropped after 35 days, by their `at`. An event older
  than that fails the age check of section 9, step 9, so it cannot
  be replayed once its row is gone.

### Catching up

Catching up is by per-origin counters. After every reconnection a
server sends `catch_up` for each active stream whose log someone
else keeps, saying for each origin the highest `n` it holds
everything up to; the reference sends what its log has above that,
and says in `caught_up` what its log holds and how far back it
reaches (section 6). Of an origin the request does not name and that
is no longer in the stream's list, a hub sends nothing: it left before
the asker knew it, and what it said reaches the asker as history (a
hub's own holder is sent everything). The server, for its part, submits again
everything in its outbox: the reference takes what it has not got
and acknowledges the rest again.

Between catch-ups a server watches `prev` on each delivery. If it is
above what the server holds of that origin, something was missed:
the server drops the envelope and sends `catch_up`, which brings
both. A log position (`cursor`) is used for `ack` and for nothing
else; it is the deliverer's own, and a server that came to be served
from another log would catch up by the same counters.

- The log holds 30 days. A hub keeps one for every stream it carries.
  An owner server that is the reference of a direct-link stream keeps
  the same log itself, in `stream_log`.
- If the server was away longer than the log reaches, `caught_up`
  shows a `floor` for some origin above what the server held. It
  takes what the log has and asks the owner for the rest with
  `history_request` (below), naming the `at` of the last event it
  held from that origin; in a hub-owned channel it asks the home hub.
  If the server that fell behind is the owner, or the home hub's
  holder, it asks the other servers in name order until one answers.
  The audit log gets `link.log_gap`.

### Events that refer to others

A reaction, an edit, a deletion, a reply, a tick, a poll answer names a
message. One origin's events reach a server in the order they were
made (`prev`), but two origins' need not: a hub asks a neighbour again
for what it missed of one origin while it goes on taking another's, a
server that drops a delivery asks for it again in the same way, and a
hub may deliver in any order it likes. So an event can arrive before
the message it is about.

In a stream that comes through a hub, a server that would reject an
event because the message it names is not there (`unknown_message`)
**holds** it instead, and applies it when the message arrives, by
delivery or as history. Events held for one message are applied in the
order they came; a reply that was held lets in what was held for the
reply. The origin's other events are not kept waiting.

| Bound | What happens at it |
|-------|--------------------|
| An event is held for at most 60 seconds | It is rejected as it would have been at once (`unknown_message`, quietly): the message is one this server was never going to be given (it was before its history, or purged) |
| A stream holds at most 256 events | When one more comes, the one held longest is rejected in the same way |
| A restart | What is held is in memory only. The counter a server keeps of each origin does not pass a held event of that origin, so after a restart the hub's log is asked for everything above it: the held event comes again with what followed it, and is held anew if it must be. What was applied the first time is recognised by its `origin_event_id` |

While something of an origin is held, the server knows in memory how
far that origin's deliveries have reached, and a later delivery whose
`prev` is covered by that is not a gap.

**Waiting for the history.** In a channel a hub owns, a server that
does not yet have the channel's earlier history whole holds, in the
same way, an event it would reject because its author or the account
it names is unknown (`unknown_account`), is not a member
(`not_a_member`), or because the message it names is not there: what
that history brings would let it in. That is an instance from the
moment it joins until the home hub has sent it a snapshot that is not
`short` ("History on joining", below), and the home hub's holder until
nobody owes it the channel's earlier history. Such an event has no
time limit: it waits until the history is whole, is then tried again,
and is rejected only if it still fails, or waits the 60 seconds above
if it names a message. When a stream holds 256 events, one that waits
for the history is let go from memory instead of rejected: the
counter of its origin stays below it, and once the history is whole
the server sends `catch_up` and is delivered it again. After a restart
the history still owed is known from the database (the instance's
`link.history_pending`, the holder's list), so what the log delivers
again waits again. A message of the channel is never lost for arriving
before the history that names its author.

**Waiting for a record.** In a channel a hub owns, an event from an
origin that is not on the server list this server holds
(`origin_not_in_stream`) is held as one about a missing message is, for
at most 60 seconds, and tried again whenever this server takes a record
of the channel: the hub sends a record to each server in turn, and over
a trunk by another way than the envelopes, so a server let in may speak
before the record that says so reaches everyone. The home hub sends each record to its own holder first. On a direct link there are two
servers and one log, and nothing is held. A hub's log holds nothing
either; the home hub's holder, which applies a channel's envelopes as
an instance does (section 23a), holds events as an instance holds
them.

**When this server fails.** An event that fails to apply because the
server itself failed (its database, its disk: rejection `internal`),
and goes on failing when tried again three times within a fraction of
a second, is not passed over in a channel a hub owns: the hub owns
the channel, and no copy of it, the holder's or an instance's, may be
short of a message. It is **kept** as a held event is, the counter of
its origin stays below it, and it is tried again after 5 seconds, then
after a wait that doubles up to 5 minutes, for as long as it takes. It
has no time limit and is never rejected for waiting. Each failure is
logged as an error, and the audit log gets `link.event_rejected` with
reason `internal`, `kept: true`, the event's `n`, `type` and the number
of failures (at most one line per stream and origin an hour), so an
operator sees a channel that is stuck. The origin's other events go on
meanwhile. What is kept is in memory: after a restart the counter
brings it again from the hub's log, as above. When a stream holds 256
events and the one to give way is kept for a failure, it is let go
from memory as one waiting for the history is, and the log is asked
for it again (`catch_up`) when it would have been tried again. In
every other stream an event that cannot be applied is rejected after
the three tries, and its origin's counter passes it.

### An envelope under a key from before a server joined

A server is delivered a stream from the moment it is let in. An
envelope made after that moment can still be under the key epoch
before: its origin had not heard of the new epoch (it was on the far
side of a split, section 23b; or its own hub took the envelope in the
moment the epoch began on another hub). On one hub the hub refuses
such an envelope (`stale_epoch`) and the origin sends it again under
the new key. Across hubs the origin's hub has already taken it, every
server that holds the old key applies it, and the new server cannot
read it.

The new server does not reject it and move on. It sends the envelope,
as delivered, to a server that holds that epoch's key: the stream's
other servers, longest-standing first, up to three of them:

    {"kind": "open_request", "msg_id": "…", "at": "…",
     "stream_id": "…", "envelope": { … }}

Whoever is asked answers only a server in the stream's list, only for
an envelope whose signature is its origin's and which it has itself
taken (`origin_event_id` in `stream_seen`), and sends back what it
says, sealed to the asker as every direct message is:

    {"kind": "opened", "re": "…", "stream_id": "…", "plain": "…"}

The asker has checked the origin's signature on the envelope itself.
It applies the event as if it had opened it: the authority rules of
section 12 are applied to the origin as always, the stamp is the
envelope's, and an event about a message not yet there is held as
above. **The plaintext is vouched for by the server that opened it,
as history is** (section 24). No key of an earlier epoch is given
out. If nobody opens it, the server notes the time of the earliest
such envelope and, when two seconds have passed without another, asks
for the messages since then as history (`history_request`), which
recovers messages and their reactions but not other events.

### History on joining

The owner chooses, when sharing, how many days of history a joining
server receives: 0 to 90. Ad-hoc streams always send everything, since
whoever is added to an ad-hoc channel sees its whole history. Sealed
streams send nothing; the devices handle their own history.

In a channel a hub owns, the home hub is the owner of its history and
sends it (section 23a, "The hub holds the whole channel"). The person
who shares the channel chooses the number of days, and the hub's
record says it to every instance (`history_days`). An instance that
has been let in asks the home hub for its snapshot at once, with
`history_request` carrying `first: true`, and asks again after 5
seconds while the hub has not answered (a net for a request or answer
lost on the way: the hub answers every request it is given, at once or
as soon as it can, below). It asks no other instance, and
takes a hub-owned channel's history from the home hub and from nobody
else (`history_not_from_home` otherwise), under the key the cluster
roster gives that hub, as it takes the channel's keys; an answer to its
own request from the hub that was the home when it asked is taken too,
though the home has changed hands since. An instance that still waits
for its history when a record tells it the home has changed hands asks
the new home at once. The hub answers
an instance in the record's list. A request that reaches the holder
before the record that lets the asker in, or before the holder has made
its copy of the channel, is kept for a minute and answered the moment
the holder takes a record of the channel; it is not refused. It cuts the snapshot once it holds
every origin's events up to the `have` it sent beside the record that
let the asker in, waking as its own counters move: from that moment it
took nothing under an older epoch. If it does not hold that much
within 10 seconds it answers with what it has, marked `short` (below),
and tells the instance with `history_more` the moment it does. The joining instance takes from the
snapshot only the messages stamped at or below where the hub's log
started for it (the `have` beside the record that let it in); one
stamped above that reaches it by the log, and is taken from there.
Nobody sends a hub-owned channel's history unasked, and an instance
takes none that it did not ask for. So a joining instance is given its
history with every other instance of the channel switched off.

The holder may not yet hold all that was said before its hub became
the home (below). It answers with what it has, and marks every part
`short: true`; it notes the instance, and whenever the history grows
(one that owed it some has given it all it holds), and when nobody owes
it the channel's earlier history any more, it sends each instance it
noted

    {"kind": "history_more", "msg_id": "…", "at": "…", "stream_id": "…"}

at once, again whenever that instance connects to the hub, and after
the hub restarts, until the instance has asked again and been given a
snapshot that is not short. An instance takes `history_more` only from
its channel's home hub. Until it has a snapshot that is not short it
still waits for its history: it asks the home again on every new
connection to the hub, and holds what it cannot apply without it
("Events that refer to others", above). A holder that answers the hub
it handed a channel to also marks its answer short if it was itself
still owed some of the channel's history; the new home then goes on
asking the channel's instances.

When a hub becomes the home of a channel (shared to it, moved onto it
from a direct link, or handed to it by another hub of its cluster), it
writes down who may hold what was said before: the hub it was handed
over from if there is one (section 23b), then every instance in the
record, each of which may hold a part. It asks each of them, with
`history_request` carrying `first: true`, and keeps asking, in rounds
from 5 seconds apart doubling to 2 minutes, at once when one of them
connects to the hub or a trunk comes up, and again after the hub
restarts, until each has answered. The rounds are a net: an instance
that is asked before it can answer (it does not yet hold the record
that makes the hub its channel's home, as when the channel is moving
onto the hub) keeps the request for a minute and answers the moment it
takes a record of the channel, rather than refusing it; one still
applying what it was delivered answers once it has. One is taken off the list when the
last part of its answer is stored, when it answers `unknown_stream`
(it holds nothing of the channel), or when it is on neither the hub
nor its cluster any more; the answer of the hub it was handed over
from, which holds the channel whole, settles the list. The channel's
earlier history is complete when the list is empty. An instance
answers its channel's home hub with everything it holds of the
channel, whatever the channel's days of history, and in a private
channel from its first message, not from its people's first joins; it
does so also from the copy it kept after leaving the channel or being
taken out of it.
Wherever this section says "the owner" of history, read the home hub;
for what the hub was given when it became the home, the instance that
answered it.

History is a snapshot, not a replay. The owner sends one or more
`history` direct messages, each at most 262,144 bytes before sealing:

```json
{
  "kind": "history", "msg_id": "…", "at": "…", "re": null,
  "stream_id": "…",
  "part": 1, "last": false,
  "conversation": {"name": "launch", "topic": "Getting 2.0 out"},
  "accounts": [ {wire profile}, … ],
  "member_ids": ["…", "…"],
  "messages": [
    {"order": "0000019ee41c2a14!a.example!0000000000000076",
     "message": {wire message},
     "reactions": [{"emoji": "👍", "account_ids": ["…"]}],
     "tasks": [{"id": "t1", "checked": true, "checked_by_id": "…",
                "checked_at": "…"}],
     "form": {"answer_count": 7, "tally": {…}, "voters": {…},
              "closed_at": null, "closed_by_id": null}}
  ],
  "blobs": [ … ]
}
```

- `conversation`, `accounts`, `member_ids` and `requests` are in the
  first part only. `requests` are, in a private channel a hub owns, the
  requests to be admitted that wait for a moderator
  (`{"account_id", "at"}`, section 23a): the receiver keeps each, for
  an account not of its own and not a member, as if `member.requested`
  had reached it, so its moderators see requests made before it held
  the channel. The requesters are among `accounts`. `accounts` holds every current member and every author of a
  message in the snapshot. Whenever the sender holds their bytes, a
  profile's picture, and a message's custom emoji of its author's own
  server, are among the part's `blobs`, whichever server the account
  or the emoji is of, and the receiver fetches them from the sender
  (section 15).
- Messages are in `order` across parts, in their current state.
  Earlier versions and tick histories are not sent, nor are deleted
  messages, but for a deleted message with replies: it goes as a bare
  marker, `{"order", "message": {"id", "author_id", "created_at"},
  "deleted": {"at", "by_id"}}`, so that its thread keeps its parent,
  and the receiver stores it deleted.
- A poll's `form` is its counts as the sender holds them (`answer_count`,
  `tally`, and for a named poll `voters`, whose accounts are among
  `accounts`) and `closed_at` with `closed_by_id` once it is closed:
  what `form.tallied` and `form.closed` said to every server. A
  receiver that is not the poll's home server takes them for a message
  it did not hold before; later `form.tallied` events replace them.
- `order` is the message's order string on the server that sends the
  snapshot. The receiver keeps it as it is and gives each message its
  own next `seq`, so every server shows the history in one order:
  what the channel held before it was shared first, in the owner's
  order from then, and after it everything stamped.
- The owner cuts the snapshot after the reference has taken the
  `stream.servers_changed` that let the new server in, and after it
  has itself applied everything delivered to it before then. From
  that point nothing under an older key epoch enters the log
  (section 10), so the snapshot holds everything the new server
  cannot read for itself. A message that is in both the snapshot and
  the log is stored once.
- **History is vouched for by the server that sent it and by nobody
  else.** The original envelopes are not kept, so the authority rules
  of section 12 cannot be applied to it. A joining server trusts the
  owner for the past, as it trusts the owner for the choice of what to
  share.
- `history_request` (`stream_id`, `after_at`, `through_at`, the
  second 0 for "to now") gets the same shape for the messages stamped
  in that range of the reference clock. It recovers messages created
  in the range. Edits, deletes and reactions that happened in the
  range to older messages are not recovered. A server that needs a
  clean copy asks the owner to remove and add it again.

## 14. Remote accounts

An account from another server appears locally as a shadow account,
shown as `username@server.name`. It is a row in `accounts` with
`server_id` set and role `remote`. It cannot sign in here, holds no
local role or level beyond `member` in the conversations it is in, and
cannot be put in a local group.

A shadow account has the same `id` as the account on its home server,
so entities, authors and reactions line up on every server without
translation.

### The profile on the wire

```json
{
  "id": "0193a001-…",
  "server": "b.example",
  "username": "lee",
  "display_name": "Lee Park",
  "title": "Support",
  "pronouns": "they/them",
  "time_zone": "Europe/Berlin",
  "bot": false,
  "avatar_file_id": "0193a7f0-…",
  "version": 12,
  "removed": false
}
```

- `version` is the home server's clock when it built the profile, and
  above every version it gave that account's profile before, so two
  changes made in one millisecond carry two versions. A receiver keeps the newest it has applied in
  `accounts.profile_version`.
- A shadow account is created or updated by `member.joined`, by
  `account.updated`, and by the `accounts` of a history snapshot or
  an offer.
- When a profile changes, its home server submits `account.updated`
  in every stream the account is a member of. A receiver applies it
  if `version` is higher than what it holds and ignores it otherwise,
  so several streams carrying the same change do no harm.
- When an account is removed on its home server, that server submits
  `member.left` with reason `account_removed` in each stream, and one
  `account.updated` with `removed` true. Other servers then show it as
  a former member. Its messages stay.
- A shadow account that is no longer in any shared conversation is
  kept, because messages point at it. It gets no more updates.
- Status, availability, presence and the do not disturb mark are not
  part of the profile. They travel as ephemeral events (section 17).

## 15. Files and custom emoji

A file's description travels in the event that uses it (`blobs`,
section 11). Its bytes travel separately, as chunks in direct
messages, so that a hub's log never holds a file. Each server ends up
with its own copy and serves its own members from it; nothing is
fetched from another server when a member reads. In a channel a hub
owns, the home hub keeps every file too, and serves it: nothing of
the channel's files depends on the instance they were posted on.

### Fetching

When a server applies an event with `blobs` it does not already hold
(by `id`), it asks the event's origin for each; in a channel a hub
owns, it asks the channel's home hub:

    {"kind": "blob_request", "msg_id": "…", "at": "…",
     "stream_id": "…", "blob_id": "0193a700-…", "from_index": 0}

The origin checks that the asker is in the stream's server list, or
is the home hub of a channel the hub owns, and that the blob belongs
to something in that stream, then answers with chunks:

    {"kind": "blob_chunk", "msg_id": "…", "at": "…", "re": "<request>",
     "stream_id": "…", "blob_id": "0193a700-…",
     "index": 0, "last": false, "data": <bytes>}

- A chunk holds 262,144 bytes of the file, except the last, which may
  be shorter. `index` counts from 0. Chunks are sent in order. The
  bytes travel as they are: a chunk's frame is the chunk and about
  260 bytes more.
- Each chunk is encrypted by the sealing of the direct message it
  rides in (section 7), to the asking server alone.
- The receiver stops and discards if the total passes the blob's
  declared `size` or the stream's `max_blob_bytes`.
- When the last chunk is in, the receiver checks the length against
  `size` and the SHA-256 against `sha256`. Both were inside the
  origin's signed envelope, so a relay cannot swap the file. A file
  that fails is discarded.
- The receiver then treats the file as an upload of its own: it
  decides `kind` and `media_type` from the content, makes its own
  thumbnail, and stores it. It keeps the file's `id`.
- **In a channel a hub owns.** The home hub's holder (section 23a)
  asks the origin for every file of the channel, its custom emoji and
  the pictures of everyone in it, at the moment the event that names
  it passes, checks it as any receiver does, and keeps it, whatever its
  size: what an instance sends the hub is bounded by the hub's limit
  on file bytes an hour (section 23). An instance asks the home hub,
  which answers an instance in the channel's list; one it is still
  fetching it answers once the file has come, waiting at most a
  minute. A picture or an emoji in a hub-owned channel's history whose
  origin shares no stream with the asker any more is asked of the home
  hub of one of its hub-owned channels, the next one at each try. The
  receiver takes the chunks from the server it asked, and checks them
  against the size and hash the origin's signed event gave, or the
  hub's history gave.
- A file's row exists from the moment its event is applied, with
  `files.state` of `arriving` and no `blob_key`; it becomes `ready`
  when the bytes are stored, `unavailable` when the tries run out,
  and `too_large` when it is over this server's or the stream's
  limit.
- Until a file has arrived the message shows it as "arriving". If the
  origin is not connected the server tries again with a wait that
  doubles from 10 seconds to 1 hour, for 7 days, and then shows the
  file as unavailable. A transfer cut short resumes with
  `from_index`.

### Size

- The stream's owner sets `max_blob_bytes` in the offer and can
  change it with `stream.limits_changed`. The default is the owner
  server's own file size limit.
- A server refuses a local upload to a shared conversation that is
  over the stream's limit, with `too_large`.
- A server whose own limit is lower than the stream's does not fetch
  a larger file and shows it as too large for this server.

### Custom emoji

A custom emoji used in a message or as a reaction is listed in the
event's `blobs` with purpose `emoji`, its `id` and its `name`. A
receiver fetches the image once and stores the emoji with the server
it came from (`emoji.server_id`). It is drawn wherever that `id`
appears. Custom emoji are unique on server and name together, so a
remote `:party:` and a local `:party:` sit side by side. In a
conversation shared with the emoji's server, members of other servers
write it as `:name@server.name:`, in text and as a reaction. It cannot
be used anywhere else.

### Sealed chats

A file in a sealed chat was encrypted on the device before upload.
The blob that travels is that ciphertext, and its `sha256` is of the
ciphertext.

## 16. Ad-hoc channels and sealed chats across servers

### The rule

A local member may start an ad-hoc channel or a sealed chat with a
remote account, or add a remote account to an ad-hoc channel, only
while the two of them **share a shared channel**: both are members of
at least one active stream of kind `channel`. The new conversation
must run over the same peer as that shared channel, and every server
in it must be on that one peer: one hub, or one direct link.

### Who checks

| Server                           | Checks                                |
|----------------------------------|---------------------------------------|
| The home server of the person adding | Before it sends anything: the rule above for each account added. A block is never a refusal (ruling R77) |
| The home server of each account being added | On the `offer` or the `member.invited`: the rule above for the adder and its own account; for a sealed chat, that sealed chats are on, that the account is not a bot, and that no sealed chat between the two exists already. If its account has blocked the adder, an `offer` is accepted all the same and its copy of the conversation leaves the account out (a sealed one keeps no member key); a `member.invited` is not followed by a join. A `device_list_request` is answered as for anyone |
| Every server in the conversation | That each `member.joined` in an ad-hoc stream follows a `member.invited` naming that account |

A server can see every shared channel its own accounts are in, so the
first two checks are exact. A third server in the same ad-hoc channel
cannot see channels it is not part of, and accepts what the two
servers concerned have each checked.

These offers need no owner's approval. The receiving server answers
`accept` by itself when its checks pass, and `decline` with reason
`not_allowed` or `feature_off` otherwise. (`blocked` is no longer
sent: a block is deaf ears, not a refusal.)

### Starting an ad-hoc channel

1. Sam on A starts a chat with Lee on B. A checks the rule.
2. A becomes the stream's owner: `stream_open`, then an `offer` of
   kind `adhoc` to B with `by_account` (Sam), `account_ids` (Lee) and
   `members`.
3. B checks, accepts, creates the local ad-hoc channel and puts Lee
   in it. A adds B to the stream as in section 8, with the whole
   history.
4. B submits `member.joined` for Lee.

Adding someone later: the adder's home server submits
`member.invited`. If the invited account's server is already in the
stream, that server checks the rule and submits `member.joined`, or
does nothing. If it is not, the owner server sends it an offer naming
the adder, and adds it when it accepts. The limit of 50 members
applies.

A cross-server ad-hoc channel cannot be given a name. A named channel
shared between servers is something only owners set up.

### Starting a sealed chat

1. Sam's device asks A for Lee's device list. A checks the rule and
   sends B a `device_list_request` naming Lee and Sam. B applies the
   checks in the table and answers with `device_list`. A stores the
   versions against Lee's shadow account and serves them to Sam's
   devices. A list is signed by the account's own key, so neither
   server and no hub can alter one unnoticed.
2. Sam's device makes epoch record 1, sealed to every device on both
   lists, and asks A to start the sealed chat with it. A creates the
   chat, opens a stream of kind `sealed`, and sends B the offer,
   which carries Sam's device list versions (as `device_lists`, in
   the shape of the `device_list` message) and the `epoch_record`.
3. B applies its checks again, accepts, and creates its local sealed
   chat for the pair with that record as `key_epoch` 1. A adds B to
   the stream and submits the same record as `sealed.epoch_started`,
   the stream's first event, so both servers hold the same record.
4. From then on the stream carries sealed `message.created` events
   and a `sealed.epoch_started` for each later epoch (section 11).
5. Whenever either account's device list changes, its home server
   sends the new versions to the other server in a `device_list`
   message, unasked. The other server can also ask with
   `device_list_request`.

Private device keys, locked copies and backup records are kept only
on an account's home server and never cross the link. Neither server
and no hub can read a sealed chat's messages or open an epoch record.
Each server sees who is in it, which device sent each item, when, how
large, and whether it was `quiet`. A `device_list_request` is
answered, and device lists are pushed, only while the rule still
holds.

### When the rule stops being true

Each server works out, for each of its own members in a cross-server
ad-hoc channel or sealed chat, whether that member still shares a
shared channel on the same peer with at least one member of the
conversation from another server.

- A member for whom this is no longer true cannot post, react, tick
  or answer there. For them the conversation is read-only. In a
  conversation of two people that makes it read-only for both.
- A receiving server applies the same test to incoming events where
  it can see both sides: it rejects an event from a remote account
  that shares no shared channel with any of the receiver's own
  members in that conversation, when those are the only other
  members.
- The history stays readable. Nothing is deleted.
- It becomes writable again as soon as the rule holds again.
- The same happens when the peer is removed or a server leaves the
  hub: the conversation stays on each server as a read-only copy.

## 17. Presence and typing

Both are ephemeral: sent in `ephemeral` frames, never stamped, never
logged, never stored, and lost for a server that is not connected.

- A server sends presence for an account only inside streams of kind
  `channel` that the account is a member of. The envelope is
  encrypted under that stream's key, so it reaches exactly the
  servers that share that channel with the account, and a hub cannot
  read it.
- `presence.changed` is sent when an account's presence, in-a-call
  mark, do not disturb mark, status or availability changes, at most
  once per account per stream every 15 seconds; changes inside that
  time are folded into the next one.
- `presence.snapshot` is sent on each stream after a reconnection and
  when a server joins a stream. After a reconnection the snapshots are
  not sent together: each stream's goes after a random delay of up to
  5 seconds, chosen by itself, and says what is true when it is sent.
  When a hub restarts every server on it reconnects in the same
  moment; the delay keeps them from sending every snapshot at once. A
  snapshot is not sent if the connection has gone again by its time.
  The snapshot for a server that has just joined a stream goes at
  once, on that stream alone.
- **Presence is state.** The newest value for an account stands; a
  server or a hub may delay one and lose one, and the next change or
  snapshot puts it right. Nothing depends on every ephemeral frame
  arriving. A server merges the presence it shows its own clients
  when a great deal changes at once (`EVENTS.md` section 5); that is
  local and nothing of it is on the wire here. Presence stays per
  stream: there is no frame that carries one account's presence for
  every channel two servers share, because a hub would then see that
  they share them.
- **A hub may drop ephemeral frames, and only those.** It does not
  open them, so it cannot merge them; under pressure it drops them, in two
  places (section 23). Envelopes in `submit` and `deliver`, control
  frames and direct messages are never dropped this way.
- A receiver keeps the newest value per account by `at`, ignores one
  older than what it has or more than 2 minutes old, and accepts it
  only from the account's home server for an account that is a member
  of that stream. It shows remote presence to local accounts that
  share a conversation with the remote account.
- A remote account with no value yet, or whose server is not
  connected, is shown with no presence at all rather than as offline.
- The presence a server sends is what its own people are shown of the
  account (`FEATURES.md` section 3.1), so someone who appears away is
  sent as away; it is sent again when that changes, by appearing away
  or back as well as by connecting. A receiving server's watches
  (`FEATURES.md` section 9.7) are decided from it, on that server
  alone: nothing about them is sent.
- `typing.started` is sent in the stream of the conversation being
  typed in, of any kind, and shown for 8 seconds.
- **Sending can be turned off.** One admin setting stops a server
  sending presence and typing to other servers. Its accounts then
  show no presence elsewhere. It still receives what others send.
- A receiving server shows a remote account's presence and in-a-call
  mark wherever it shows its own people's: in presence events, in the
  first answer to a subscription, and in the account object. Status,
  availability and do not disturb travel but are not shown.

## 17a. Blocks: deaf ears across servers

A block is deaf ears (ruling R77, `FEATURES.md` section 20): when X
has blocked Y, Y is shown X online with nothing else, and none of X's
typing, and Y is never told. Presence and typing go to a server as a
whole, so when Y is on another server, that server must be told which
of its people to show X that way. Nothing else of a block travels: a
chat Y starts with X is taken and leaves X out on X's own server
(section 16), which needs nobody else's help.

- **What is sent.** A `deaf_ears` direct message (section 7) to Y's
  server, and to no other: for each account of the sender, the
  receiver's accounts it has blocked. It is the whole of what the
  sender has for that receiver, so an empty list says there are none;
  the newest by `at` stands and an older one is ignored. With blocking
  off on the sender, the list is empty.
- **When.** When a block of one of the receiver's accounts is made or
  lifted, over the first connected peer that reaches the receiver.
  When a connection comes up, to every server sharing an active stream
  over it, with `ask: true`, and before any presence snapshot; a server
  sent `ask` answers with its own, so a server that restarted is told
  again even when the other side's connection never dropped. To a
  server that joins a stream, when `stream.servers_changed` names it
  (and, by the joining server, to every server already there). To
  every server sharing a stream when blocking is turned on or off.
- **What the receiver does.** It keeps only its own accounts as
  `account_ids`, and uses an entry only while the account it names is
  one whose home is the server that sent it. It holds the lists in
  memory only, never in its database or its logs, and nothing it shows
  any admin includes them. While X has blocked Y:
  - Y is sent no `presence.changed` about X, whether from the link or
    from a call on Y's server, and no `typing.started` by X, in any
    conversation;
  - the first answer to Y's subscription, the account object and the
    account list show X `online`, not in a call, with nothing else;
  - Y's watch for X answers at once that X is around, and one set
    before the block never fires.

  When the list changes, Y is sent at once what is now true: X online
  when the block arrives, and what X's server last said (or offline,
  when nothing has been heard) when it is lifted.
- **What it reveals, and to whom.** Y is told nothing, and sees X as
  anyone sees someone who blocked them on their own server. Y's
  server, and so its operator, learns that X has blocked Y: it cannot
  show Y the difference without knowing it. That is all it learns: it
  is not told X's other blocks, the message is sealed to it so a hub
  cannot read it, the other servers of a shared channel never see it,
  and it is not stored. A list carried in the presence frames of a
  stream would have reached every server in the channel, and would
  have been sent again with every change.
- **What stays distinguishable.** As on one server, X is always
  online to Y. Between X's block and the receiver hearing of it, and
  after a receiver restarts until it is told again, Y may be shown X's
  presence as anyone is. A server that does not honour `deaf_ears` is
  not prevented from showing Y the truth; no server can be made to.

## 18. Calls

A call in a shared conversation lives on the server of whoever
started it, the **host**. Members of other servers send their media
straight to the host's media port. Their own server gets them in.

1. Sam on A starts a call. A posts the "call started" notice (a
   `message.created`) and submits `call.started` with `host` set to
   A.
2. Lee on B chooses to join. Lee's client asks B, over its own
   WebSocket, exactly as for a local call.
3. B sends A a `call_pass_request` for Lee.
4. A checks that the call is live, that Lee is a member of the
   conversation in A's copy, and that B is Lee's home server. It
   answers with a `call_pass`:

       {"kind": "call_pass", "msg_id": "…", "at": "…", "re": "…",
        "call_id": "…", "account_id": "…",
        "pass": "…32 random bytes…",
        "expires_at": "2026-11-13T09:31:00.000Z",
        "media": {"host": "a.example", "udp_port": 24877,
                  "tcp_port": 24877}}

5. Signalling for Lee runs between Lee's client and B over the
   WebSocket, and between B and A as `call_signal` direct messages.
   `signal` is the same object the client and server exchange for a
   local call (`CALLS.md`). The first `call_signal` from B carries the
   `pass`.
6. Lee's client connects its media to A's media port. A forwards it
   like any participant's.
7. A submits `call.participant_joined` and `call.participant_left`
   as people come and go, and `call.ended` when the last one leaves.

- A pass is 32 random bytes, valid for 60 seconds, for one account
  and one call, and works once. It authorises the signalling exchange
  that sets up the media session; A answers no `call_signal` for an
  account without one.
- The host's operator can see the network address of everyone in the
  call, and a remote member's client connects to a server other than
  its own. The client says so before joining.
- **Two calls started at once.** If two people on different servers
  start a call in one conversation at the same moment, each before
  the other's `call.started` has arrived, the stamp settles it, the
  same on every server: **the `call.started` with the earlier stamp
  is the call.** A server that is delivered a `call.started` while
  it takes another call to be live in the conversation compares the
  two stamps. If the one that arrived is later it is rejected
  quietly: its host will end that call when the earlier one reaches
  it. If it is earlier, it is the call: the call held ends on this
  server (if this server hosts it, it submits `call.ended` and its
  people are offered the real one), and the earlier one is adopted.
  A server that only hears of both, through a hub, takes the same
  one whichever reached it first. Two `call.started` whose stamps
  are more than 60 seconds apart are not two starts of one moment:
  the later one is a new call. If a server's own `call.started` is
  stamped again (section 13) the comparison is made again with the
  new stamp.
  A server whose own local call loses in this way ends it with reason
  `server` (`CALLS.md`): its members see the call end for a reason that
  is not theirs, and are offered the one that stands.
- **A participant who drops.** Between the two servers a participant
  whose connection drops is reported with the `call.disconnected`
  signal, inside `call_signal`, by the participant's own server to
  the host. A remote participant whose session ends on its own server
  (signed out, session revoked) is **not detected by the host as
  such**: the host sees that participant's media and signalling stop
  and holds the place for 30 seconds, as for any drop.
- If the host server goes away, the others end the call locally after
  90 seconds without it.
- In a sealed chat the media frames are encrypted with keys derived
  from the chat's epoch secret, so the host forwards what it cannot
  decode.
- There are no calls between servers outside a shared conversation.

## 19. Forms and polls

**Only polls work across servers.** A form in a shared conversation
must use the `tally` action, named or anonymous. A server refuses to
post any other form there (`collect`, `thread`, `channel`, `group`,
`bot`) with `invalid_value`, and a receiver rejects a
`message.created` or `message.edited` whose form has another action.
A form there cannot carry `answerer_group_id` either, since a group
is a set of one server's accounts.

A form belongs to its message, and the message's author has a home
server. That server keeps the answers and the tally, for everyone.

1. A member of any server answers. Their home server checks the
   answer against the form, keeps it as the member's `my_answer`, and
   submits `form.answered`.
2. `answer_box` is the answer sealed to the form's home server with
   the sealing of section 7 (steps 1 to 6, from the answerer's server
   to the home server). Its plaintext is
   `{"button_id": …, "answers": {…}, "added_options": […]}`. Other
   servers in the stream can see that the account answered and
   cannot see what. `form.answer_withdrawn` takes an answer back.
3. The home server opens the box, applies the form's rules (one
   answer per account, the latest replacing the earlier; places per
   option; closed or not), submits `form.option_added` for each
   option the answerer added, and submits `form.tallied` with the new
   counts. For a named form it includes `voters`. Changes within one
   second are folded into one `form.tallied`.
4. If it does not accept the answer it submits `form.answer_refused`,
   and the answerer's server clears `my_answer` and tells the person.
5. Each server shows the tally to its own readers under the form's
   `results` rule. `after_answer` is judged by each server for its
   own members, from the answers it holds for them.
6. An answer that reaches the form's home server after it closed
   the form is not counted, by anyone: the home server keeps the
   answers, refuses it with `form.answer_refused`, and its tally is
   the tally.

- The form's home server alone learns what each person answered, and
  it must: it holds people to one answer. For an anonymous form it
  never puts names in `voters`, and other servers learn only who
  answered at all.
- Only the home server closes a form: the author, or a manager or
  admin there.
- A channel that already holds forms of other kinds can still be
  shared. Those forms are sent in history as closed, with no answers,
  and stop taking answers on the owner server from the moment the
  stream opens.

## 20. What a shared channel cannot do

| Not available                         | Why                                  |
|---------------------------------------|--------------------------------------|
| Moving a thread out into its own channel | Refused on every server while the channel is shared. The replies would have to move on every server at once, and the new channel would need sharing by every owner. |
| Making the channel private            | Refused on every server while the channel is shared. Each owner chose public or private for their copy when accepting, and that choice stands until the server leaves the stream. |
| Locking                               | Refused while shared, for shared channels and cross-server ad-hoc channels alike. A lock is one server's promise that nobody new gets in, and it cannot bind the other servers. |
| Forms other than polls                | Section 19                           |
| Sealing                               | Channels are never sealed, anywhere  |
| A bot's commands and dialogs for people on other servers | A command reaches a bot over the bot's own server's live connection |
| Mentioning a group from another server, or limiting a form to a group | Groups are local sets of local accounts |
| Removing another server's message for everyone, from a server that is not the owner | Section 12. It can be hidden locally. |
| Setting a remote member's level, timing them out, or removing them alone | Each server manages its own members. A local manager can mute a remote account locally; the owner can remove a whole server. In a channel a hub owns, its managers and moderators remove a person on every instance (`member.removed`, section 23a). |
| Shared pins, slow mode, word filter and retention settings | Each server's own |
| Naming a cross-server ad-hoc channel  | Section 16                           |
| Search across servers                 | Each server searches its own copy    |

## 21. Leaving and removing

This section is about streams a server owns. In a channel a hub owns
an instance leaves by sending the hub `leave`, a manager has an
instance removed or closes the channel with `channel_action`, and
nothing can be handed over; section 23a has it all.

### A server leaves a stream

1. Its owner chooses "Stop receiving" on the channel.
2. The server sends `leave` to the reference, which stops delivering
   to it and passes the frame to the owner server. What still waited
   in the leaver's outbox is dropped.
3. The owner server sends `server_left` with reason `left`, starts a
   new key epoch for the servers that remain, and submits
   `stream.servers_changed` with the leaver in `left`.

If the owner server cannot be reached, steps 1 and 2 are enough for
the leaver; the owner finishes when it returns.

### The owner removes a server

The owner server sends `server_left` with reason `removed`, starts a
new epoch and submits `stream.servers_changed`. The reference relays
`server_left` to the removed server, which is how it learns.

### The owner stops sharing

The owner server submits `stream.closed` and sends `stream_close`.
Every server detaches. A hub keeps the log for its 30 days and then
drops it.

### Handing over ownership

The owner server may hand a stream to another server in it, with that
server's owner's agreement. It submits `stream.owner_changed` and
sends `owner_changed`. The new owner starts a new key epoch. On a
direct link the new owner also becomes the stream's time reference
and keeps its log from the handover on: each server then stamps by
the offset the new owner tells it, which on a direct link it has
been measuring all along. Ownership cannot be handed to a hub this
way: a channel is brought onto a hub by the move of section 21a, and
a channel a hub owns stays the hub's.

### Removing a peer

The owner of a server removes a peer in the admin console.

- For each stream on that peer that the server does not own, it
  leaves as above.
- For each stream it owns, it must first hand it over or close it. On
  a direct link there is nobody to hand to but the other server.
- It then sends `bye` with reason `removed` and marks the peer
  `removed`. The other side marks the peer `removed` when it gets the
  `bye`, or when its owner removes it too.
- The pinned keys stay in `servers`, so that if the same name is
  linked again with different keys the owner is warned.

### A hub removes a server

The hub's operator removes a server (section 23). The hub closes its
connection, refuses it from then on, and sends everyone a new
`roster`.

- For each stream the removed server was in, that stream's owner
  removes it as above, without being asked.
- A stream the removed server owned has nobody to run it. The hub
  marks it closed and tells its servers with an unsigned
  `stream_close` naming the reason `owner_gone`; they detach.
- From each channel the hub owns it takes the server out itself, in
  the same step: a new record and a new key epoch, with a key of the
  hub's, for the servers that remain (section 23a). The removed instance's
  accounts stop being managers and moderators.

### What each server keeps

| After                    | This server keeps                           | The others keep           |
|--------------------------|---------------------------------------------|---------------------------|
| It left, or was removed  | Its whole copy, as a local channel that is archived. Its admins can unarchive it and carry on locally; what is posted from then on is ordered after everything the channel held while it was shared (section 13). Remote accounts in it show as former members. It gets nothing further and has no later key. | Every message its members posted. Its accounts show as having left. |
| The stream was closed    | The same, on every server                   |                           |
| A peer was removed       | The same for every stream on it. Cross-server ad-hoc channels and sealed chats become read-only copies. | The same |

No server can make another delete its copy. `streams.state` records
`left` or `removed`; `stream_keys` are kept so the copy can still be
read; `stream_seen` and `outbox` rows for the stream are deleted.

## 21a. Moving a channel from a direct link onto a hub

A direct link joins exactly two servers, and an owner server never
relays between two others (ruling R10). To bring in a third server, a
channel the two share directly is moved onto a hub both are on
(ruling R55), or onto the owner server's hub when the other server is
on another hub of the same cluster (section 23b): the hub named is
then the owner server's, the other server knows it by name and key
from the roster its own hub sent, and reaches it through its own hub. The move hands over four things at once: the channel's
**ownership** (owner server → hub), its **time reference** (owner
server → hub), its **relay and log** (direct link → hub), and its
**key epoch** (a new one, the hub's first, with a key it makes). The
stream keeps its `stream_id`, each server keeps its one local channel
with everything in it, and the direct link stays for the two servers'
other streams. It is all or nothing.

Nothing in either copy changes. Both keep their people, their levels,
their first joins and their history. The managers of the channel on
both instances become its managers on the hub's list at the commit.
A private channel stays private: the hub's record says so for good
(section 23a). A private channel with groups in it is not moved
(`conflict`): its groups are removed first.

Below, O is the stream's owner server, P the other server, H the hub.

### What is not handed over, and why nothing is frozen

Nobody numbers events (section 13). An event's place is the stamp its
origin signed; a stamp anyone else has seen never changes; every
server drops an event it already holds (`origin_event_id`); and each
origin's events are taken in the order of its counter with no gaps
(`prev`). So there is no numbering to continue on the hub and no
moment at which the stream must stand still. What must be exact is
one thing only: **which of each server's events the direct log took,
and which it did not.** The move fixes that with one event in the
direct log.

### The steps

1. **Propose.** O's owner proposes, naming the hub by its peer. O
   requires: the stream is a `channel` it owns over a direct link; O
   and P are both on the hub's roster; no other move of the stream is
   open. It stores the move (`stream_moves`) and sends P, over the
   direct link:

       {"kind": "move_propose", "msg_id": "…", "at": "…",
        "move_id": "0193e000-…", "stream_id": "…",
        "hub": "hub.example", "hub_fingerprint": "ABCD-…",
        "expires_at": "2026-11-15T09:30:00.000Z"}

   A proposal stands for one day unless O's owner says otherwise (a
   minute to seven days). It is sent again when the link reconnects.

2. **Accept or decline.** P's owner sees the proposal with the hub's
   name and fingerprint. P can accept only if it has its own active
   link to a hub of that name and fingerprint, O is on that hub's
   roster as P sees it, and the hub has told P its clock offset.
   Accepting signs a block, name `move_accept`:

       signed: {"move_id": "…", "stream_id": "…", "hub": "hub.example",
                "hub_key": "…", "owner": "a.example", "server": "b.example",
                "managers": [{"account_id": "…", "server": "b.example",
                              "username": "bea"}, …],
                "expires_at": "…", "at": "…"}

   `managers` are the managers of the channel on P: every active
   person's account whose level is manager. P sends O `move_accept` carrying `accept` (the signed
   bytes) and `accept_sig`, and sends it again on every reconnection
   until it knows the outcome. Declining sends `move_decline`;
   nothing has changed.

3. **Prepare.** O sends H a `channel_create` (section 23a) with
   `move_id`, both servers, the managers-to-be, `epoch` (the
   stream's key epoch plus one) and P's `accept` and `accept_sig`. H
   checks P's signature against P's pinned key and that P agreed to
   this move, of this stream, onto this hub, proposed by this
   server, and has not expired; and that the managers named for P
   are exactly the managers P signed for. A private channel's
   `channel_create` carries `private: true`. H then records the channel
   as **prepared**: it signs a record with `prepared: true`, sends
   it to O and to nobody else, lists the channel nowhere, and
   carries nothing in it (a `submit` is refused with
   `unknown_stream`). A second `channel_create` for the same move
   gets the same record again. Until step 4, O may take the channel
   back with `channel_action` `close`.

4. **Commit.** O commits in **one local transaction**, which

   - checks that the move is still accepted and has not expired,
     that the stream is still O's over that link, and that O's clock
     is not more than 15 seconds ahead of H's as H has told O;
   - stamps and appends `stream.moved` to the direct log as O's own
     event, under the stream's current key epoch. It carries H's
     prepared record and a block O signs, name `channel_commit`:

         signed: {"move_id": "…", "stream_id": "…", "hub": "hub.example",
                  "owner": "a.example",
                  "have":    {"a.example": 412, "b.example": 96},
                  "have_at": {"a.example": 1794557400532,
                              "b.example": 1794557399871},
                  "epoch": 3, "managers": […], "at": "…"}

     `have` is, for O, the counter of `stream.moved` itself, and for
     P the counter of P's last envelope in the direct log; `have_at`
     the `at` of each. `managers` are O's managers of the channel,
     read in this transaction;
   - re-points O's copy of the stream: its peer becomes O's link to
     H, its owner H, its time reference H;
   - records the move as `committed`, with the commit block.

   The transaction holds the stream's row, which every writer to the
   direct log also takes. So the log's last envelope of each origin
   is exactly what `have` says, and nothing is appended after
   `stream.moved`: an envelope P submits afterwards is refused with
   `unknown_stream`, and whatever O's own members do afterwards is
   stamped by H's clock and waits in O's outbox for H.

   **This transaction is the commit point.** Before it the move can
   be abandoned. After it the move cannot be abandoned.

5. **Activate.** O sends H the `channel_commit` block as a frame. H
   checks O's signature and that it is the commit of the move it
   prepared, sets each origin's last counter and time from `have`
   and `have_at`, sets the channel's managers to P's managers (as
   signed in step 2) and O's (as signed in the commit), and signs the
   record anew: no longer prepared, calling for key epoch `epoch` plus
   one, which H begins in the same transaction with a key it makes
   (section 10). From here H delivers and logs. **Either server may
   hand H the block**: P does so when it switches (step 6), so the
   channel does not wait on a server that committed and then went
   away. A second `channel_commit` changes nothing. A server on
   another hub of the cluster hands the block to its own hub, which
   carries it to H in `ask`.

   **A hub that has lost what it prepared.** Beside the block the
   server sends, in `inner`, the prepared record H signed in step 3.
   If H holds no stream of that identifier at all (its database was
   lost, or put back from a copy older than the move), it prepares
   the channel again from that record and this commit, and activates
   it in the same transaction. It does so only for a prepared record
   under its own present signature, of this stream, no more than 30
   days old, whose two servers are both still on the hub or its
   cluster, with a commit signed by the server the record names
   first. Without this the two servers would hold a committed
   move, bound to the hub for good, with nowhere to send it. A hub
   that has lost its key, or its list of servers, is a new hub: its
   servers must join it again, and this does not help them.

6. **Switch.** O delivers `stream.moved` to P over the direct link,
   and goes on serving the direct log there for as long as it keeps
   it (30 days) to a P that asks with `catch_up`. P applies an
   origin's events in counter order with no gaps, so when it
   applies `stream.moved` it holds everything O made before it. In
   the transaction that applies it, P

   - checks that it agreed to this move, that the record verifies
     under an accepted key of the hub it agreed to and lists exactly
     O and P, and that the commit is signed by O;
   - deletes from its outbox every waiting event of its own whose
     counter is at or below `have` for P: O took those, and they are
     not sent again;
   - re-points its copy of the stream at H, as O did;
   - stamps again, by H's clock and after `stream.moved`, in the
     order they were made, what still waits in its outbox. O never
     took those, and P has sent H nothing of the stream before this
     transaction commits, so nobody has seen their stamps;
   - records the move as `done`.

   Then P hands H the commit block, sends it what waits, and catches
   up with H from O's counter at `stream.moved`. If applying
   `stream.moved` fails on P, it does not count as held: P asks for
   it again.

7. **The new key epoch.** H sends both servers the key of the epoch
   its activated record calls for, ahead of the record. A server that
   switches after the key came (P, whose stream still ran over the
   direct link) asks H for it with `key_request` when it takes the
   record. Until each holds it, what it sends under the last key of
   the stream's direct life is refused (`stale_epoch`) and waits.

O marks the move `done` when H's activated record has reached it and
P has acknowledged the direct log up to `stream.moved` (P answers a
re-sent `stream.moved` with `ack` once it has switched).

### Abandoning

Until its commit transaction O abandons the move when: P declines; O's
owner withdraws it; it expires; H refuses to prepare it; O finds at
the commit that the stream is no longer its own over that link, that
it is no longer on the hub, or that its clock is too far ahead of
H's; or P leaves the stream. H being unreachable is not a refusal: O
tries again until the move expires. Abandoning is one transaction on
O that takes the same row lock as the commit, so exactly one of the
two happens. O then sends P `move_abandon` (and sends it again, on
reconnection and whenever P's `move_accept` arrives for a move that
is off), and closes the prepared channel on H, asking again until H
says it is closed. The stream carries on directly as before: nothing
that anyone acted on had changed.

P never abandons a move it has accepted. It learns the outcome from
O alone: `stream.moved` in the direct log, or `move_abandon`.

### Why the outcome is the same on all three, whoever fails when

O's decision is one row written in one transaction: `committed` or
`abandoned`, never both, never neither for long. Everyone else
follows it, and every step that follows it can be taken again.

| What fails, and when | What happens |
|----------------------|--------------|
| P never gets the proposal, or O never gets the answer | Both are sent again on reconnection. An unanswered proposal expires on both servers independently; nothing had changed. |
| O fails or restarts before step 3 or during it | The move is still `accepted` in O's database. O prepares again when it returns (H answers a repeat with the same record), or abandons if the move has expired. |
| H refuses to prepare | O abandons. P is told. Nothing had changed. |
| H cannot be reached at step 3 | O tries again until the move expires, then abandons. |
| O fails or restarts after H prepared, before the commit | As before step 3: the prepared record is in O's database, so O goes straight to the commit, or abandons and closes the prepared channel. A prepared channel carries nothing and tells nobody, so P and every other server are as they were either way. |
| O fails during the commit transaction | The database rolls it back: not committed. |
| O fails or restarts right after the commit | The move is `committed` in O's database with the commit block. O delivers `stream.moved` and hands H the commit when it returns. Meanwhile P is where it would be if O were simply down: what its members say waits as unsent. |
| H cannot be reached after the commit | O and P keep handing it the commit; their events wait in their outboxes. Nothing is lost, and nothing can be undone: the commit stands. |
| The direct link is down after the commit | P does not switch on H's word: it switches only on `stream.moved`, which only the direct log holds. When the link returns P asks, gets everything up to it, and switches. What H delivered to P meanwhile P did not take, and gets again when it catches up. |
| P fails or restarts before applying `stream.moved` | P's move is still `accepted`. On reconnection it catches up on the direct log and switches. |
| P fails while applying `stream.moved` | The transaction rolls back and the event does not count as held; it comes again. |
| P fails or restarts after switching | Its stream points at H in its database. It hands H the commit again and catches up. |
| O committed, and P's `move_accept` arrives again | O sends `stream.moved` again. |
| O abandoned, and P still thinks the move is on | P's `move_accept` on reconnection gets `move_abandon`. |
| H loses nothing it prepared | A hub keeps a prepared channel until its creator closes it or commits it; it never drops one by itself. |

**Nothing lost.** Every event of O's up to `stream.moved` is in the
direct log, which P reads without gaps before it switches. Every
event of P's with a counter at or below `have` is in the direct log,
and O holds it. Every event of P's above `have` is still in P's
outbox when it switches, and goes to H. Every event of O's after
`stream.moved` is in O's outbox, and goes to H.

**Nothing duplicated.** P deletes what O took in the same
transaction that switches, so it never sends H an event O holds; if
it did, O would drop it by its `origin_event_id`, but it would stand
at one stamp on O and another elsewhere, which is why the deletion
and the switch are one transaction. H drops a repeated envelope, and
every server drops a repeated event.

**Nothing reordered.** The only stamps that change are those of
events nobody but their origin has seen (P's, above `have`: they are
stamped again in the order they were made, in the transaction that
switches, so none can have left for the hub under its old stamp). Each server stamps
its next event after everything it holds, so everything said after
the move stands after everything said before it.

**One channel on each server.** The switch changes three columns of
the stream's row. No stream and no conversation is created.

### An offline server, and a third server

A P that was offline through the whole move finds, when it returns,
a direct log that ends in `stream.moved`; it reads it and switches as
above. It then catches up with H by per-origin counters from where
the direct log left each origin, which is where H's own counters
begin: the two logs join without overlap or gap.

After the move further servers join through the hub in the ordinary
way (section 23a): by a manager's invitation, or from the directory
if a manager lists the channel (a private channel is never listed). A server that joins asks for the
history the channel gives, and that includes what the channel held in
its direct days.

### What is refused while a move is in progress

On O from the proposal, and on P from its acceptance, until the move
is done or abandoned, these are refused with `conflict`:

- a second move of the stream;
- handing the stream over, closing it, or removing its other server
  (O); leaving it (P);
- removing the direct link, or the link to the hub;
- on P: any change of a member's level in the channel, and a manager
  of the channel leaving it. The managers P signed for become the
  managers on the hub's list at the commit, so they stay the managers
  until then.

Messages, and everything else members do in the channel, are never
refused: they flow over the direct link until the commit and over
the hub after it, and wait as unsent in between.

### What the hub learns

What it learns of any channel it owns (section 23a): the stream's
identifier, its name and description, its two servers, its
managers, and whether it is private; and from the activation on it
makes and keeps the channel's keys. `stream.moved` is an envelope like
any other and stays in the direct log, which the hub never sees, nor
the keys of the channel's direct life.

## 22. Purge and retention

Deletion on one server is a request to the others. They are expected
to honour it and cannot be forced to.

- **Delete.** `message.deleted` asks every server to replace the
  message with its marker and keep its content as history, as a local
  delete does.
- **Purge.** `message.purged` asks every server to erase: with
  `history`, the earlier versions; with `content`, everything but the
  record that a message was there, who posted it and when. Erasing
  means the database, the search index and the file store.
  - A purge request from an author goes to the admins of the author's
    own server, under that server's rules for the kind of channel it
    is there. If the outcome is to erase, that server submits
    `message.purged`.
  - The owner server may purge any message in the stream.
- **Retention.** Each server runs its own retention on its own copy.
  When it deletes, it submits `messages.expired`:
  - with `scope` `own`, asking others to erase the messages authored
    by its accounts stamped at or before `through_at`;
  - with `scope` `all`, from the owner server only, asking others to
    erase every message stamped at or before `through_at`. The owner
    server's retention period for the channel is thereby the
    channel's, as far as others honour it.
- **Removal by the owner.** `message.removed` asks every server to
  treat the message as deleted by a moderator.
- A server that honours these does so at once on applying the event.
  A Churr server always honours them, and so does a hub's holder,
  which applies them to its copy of a channel the hub owns as an
  instance does. A modified server, a server that has left, a backup,
  and a hub's 30-day log of ciphertext are all outside anyone's
  reach.
- A hub's log is encrypted. When a server leaves and the epoch
  changes, the leaver's old key still opens old entries if it can get
  them; the hub serves the log only to servers in a stream's current
  list.

## 23. The hub

`churr-hub` is the link package with a log and a clock, and the
owner of the channels shared through it (section 23a): it makes and
keeps their stream keys (section 10), and holds each of them whole,
contents, history and files, in a copy kept by the same code an
instance keeps a copy with. It has no accounts of its own, no search
and no API beyond `/link/v1`, `/.well-known/churr` and a status page. Ad-hoc channels and sealed
chats that cross it are not its own: they stay under the servers
involved, and the hub only carries them (ruling R59).

A hub is the time reference of every stream opened on it (its
**home hub**): it measures each server's clock (section 5), checks
the stamp of every envelope submitted to it (section 13), and keeps
the log. It orders nothing. Where an event stands is in the stamp
its origin signed, which the hub can refuse and cannot change.
Anything a hub signs (the roster, a channel's record, the directory)
names the hub and, for a channel, its home hub.

### What it stores

In one data directory:

| Thing              | Contents                                        | Kept          |
|--------------------|-------------------------------------------------|---------------|
| Its keys           | `keys/`: the signing key and the box key        | For good      |
| Servers            | Name, keys, when joined, state, limits          | Until removed |
| Invitations        | Hash of the secret, expiry, the name it is for  | Until used or expired |
| Streams            | `stream_id`, owner (a server, or the hub itself for a channel it owns), home hub, server list and its version, the highest key epoch begun, the next log position, state (`open`, `prepared`, `closed`) | While open, then 30 days |
| Channels           | For each channel the hub owns: its record as last signed (name, description, listing mode, whether it is private, servers, managers, moderators, invitations, requests, key epoch, days of history), the server that brought it, who invited whom, and for a moved channel the move's identifier | While the stream is |
| Stream keys        | For each channel it is the home of: every key epoch's key, sealed at rest under the hub's key file, with the server list the epoch began for | While the stream is |
| The envelope log   | Per stream: the position, the envelope (an `Envelope` message, as it will be delivered), its origin and `n`, the `n` of the same origin's envelope before it, the time | 30 days |
| Origins            | Per stream and origin: the `n` and `at` of the last envelope taken | While the stream is |
| Clocks             | Per server: the figures of section 5, as last measured | Until removed |
| Absences           | Per server: the last 8 stretches it was disconnected | 24 hours each |
| Counters           | Per server: envelopes, bytes, refusals          | 30 days       |
| The holder         | `holder/`: for each channel it is the home of, the channel whole (section 23a, "The hub holds the whole channel"): a database with the instance's schema and migrations (`holder/churr.db`), holding every message as it stands, edited and deleted, its reactions, ticks and forms, the members and the profile of everyone who wrote or is a member, and the channel's files, custom emoji and pictures (`holder/files/`), each checked by its hash | For as long as the hub has the channel: a closed channel's copy goes when its stream does, 30 days after it closed |

What it keeps of the link itself (envelopes, frames kept for a server,
its channels' records, roster entries) it keeps as the bytes that
travel: the messages of section 2, and a signed block as the bytes its
signer wrote. The database is marked with the form (3, the protocol's
version). A hub refuses to start on a database that holds anything in
another form: the only other one there has been is the JSON of
versions 1 and 2, never released, and what other parties signed as
JSON then cannot be turned into what they would sign now. A server
does the same for what it keeps of its links (a stream's log, a
hub's records of its channels, the agreements of a move), except that
it starts and says in its log, at every start, that those links must
be removed and made again.

### What it can and cannot see

| It sees                                              | It cannot see                           |
|------------------------------------------------------|-----------------------------------------|
| Each server's name, keys and network address         | Any message or file of a stream it does not own, or the name of anyone who is in no channel it owns |
| Which servers are in which stream                    | The kind of a stream it does not own, or what conversation it is |
| Of each channel it owns: name, description, listing mode, whether it is private, servers, managers and moderators (account identifier, username, home instance), which servers are invited or asking, every key epoch's key, and the whole channel: every message and file, its members and their profiles | Each server's own name for it, levels, pins |
| For each envelope: stream, origin, size, its stamp (`at`, `n`, `offset`), its key epoch; in a channel it owns, all it says, since it holds the key | In a stream it does not own: who posted, or what kind of event it is |
| Each server's clock offset, and the latency and jitter of its path, each direction by itself | |
| For each direct message: from, to, size, time        | What kind it is: an offer, a key, a file chunk and a call pass look alike |
| That ephemeral traffic is flowing in a stream        | Who is typing or online                 |

It can refuse, delay and drop what it carries, deliver it in any
order, and close a stream. It cannot change where an event stands.
Over the channels it owns its operator can do more, by ruling R56:
admit and remove instances and appoint and dismiss managers. The hub
owns the channel and is trusted as its owner, and nothing in it is
kept from the hub: it makes and keeps every key of the channel, so
**the operator of a hub can read every message of every channel the
hub owns**, as it passes and for as long as the hub has the channel:
its holder opens every envelope of such a channel and keeps the
channel whole (section 23a). An instance that joins is given the key of every epoch from
then on by the hub, and every instance in the channel sees the server
list change. Ad-hoc channels and sealed chats it carries stay closed
to it (ruling R59).

### Operator commands

| Command                                  | Does                                 |
|------------------------------------------|--------------------------------------|
| `churr-hub serve [-clock-synced]`        | Runs the hub. `-clock-synced` declares its clock and its servers' synchronised (section 5); off unless given |
| `churr-hub fingerprint`                  | Prints its name and fingerprint      |
| `churr-hub invite [--for NAME] [--expires DURATION]` | Prints an invitation string. Seven days unless told otherwise. |
| `churr-hub invites`                      | Lists invitations not yet used       |
| `churr-hub revoke-invite ID`             | Cancels one                          |
| `churr-hub servers`                      | Lists servers: name, fingerprint, connected or not, streams, traffic, and each server's clock as the hub last measured it: offset and uncertainty, round trip and jitter, each direction's one-way time and jitter, and any asymmetry estimate |
| `churr-hub remove NAME`                  | Removes a server (section 21)        |
| `churr-hub streams`                      | Lists streams: identifier, owner, home hub, servers, log position, log size |
| `churr-hub channels`                     | Lists the channels the hub owns: identifier, name, description, listing mode (with "(private)" for a private one), servers, managers, moderators, servers invited and asking, key epoch, envelopes carried |
| `churr-hub appoint STREAM ACCOUNT-ID USERNAME@SERVER` | Makes an account a manager of a channel. The instance must be in the channel. The last resort when a channel has no manager who will act |
| `churr-hub dismiss STREAM ACCOUNT-ID`    | Takes an account off a channel's managers |
| `churr-hub expel STREAM SERVER`          | Removes a server from one channel, leaving it on the hub |
| `churr-hub limits [NAME] [--submits N] [--blob-bytes N] [--streams N]` | Shows or sets limits, for one server or as the default |
| `churr-hub status`                       | Prints what the status page shows    |
| `churr-hub trunk …`, `churr-hub cluster …`, `churr-hub rehome STREAM HUB` | Joining hubs into a cluster, and moving a channel's home: section 23b |

The hub has no admin console and no accounts. Whoever can run these
commands on its machine is its operator.

### Invitations

Section 4. A hub's invitation has `kind` `hub`. With `--for` it can
be used only by a server of that name.

### Limits per server

Defaults, each settable per server. Over a rate limit the hub answers
`rate_limited` with `retry_after_seconds`; over a size limit,
`too_large`.

| Limit                              | Default            |
|------------------------------------|--------------------|
| Frame size                         | 1,048,576 bytes    |
| Envelope ciphertext                | 262,144 bytes      |
| Envelopes submitted                | 600 a minute       |
| Ephemeral frames                   | 1,200 a minute     |
| Direct messages other than file chunks | 600 a minute   |
| File chunk bytes                   | 1 GiB an hour      |
| Streams a server may own           | 1,000              |
| Servers in one stream              | 100                |

A Churr server applies the first five to each peer of its own in the
same way.

### Ephemeral frames under pressure

Typing and presence are never stored and nothing is ordered after
them (section 17), so a hub that is pressed drops them before it lets
them delay anything else. It counts what it drops; the sender is told
nothing, and has done nothing wrong.

- **On arrival.** A hub takes at most 2,000 ephemeral frames a second
  from all its servers together, after a burst of 2,000. A frame past
  that is dropped before its signature is checked or anything is read
  for it, so a burst of presence from every server at once does not
  make the envelopes on the same connections wait. This is besides
  each server's own limit above, which is answered with
  `rate_limited`; a frame dropped here gets no answer.
- **On the way out.** What a hub sends a server is queued for that
  server's connection (at most 64 MiB, section 26). When more than
  8 MiB is waiting for a server, ephemeral frames for it are dropped
  rather than put behind what is waiting; the other servers of the
  stream are still passed them.

Ephemeral frames that come over a trunk from another hub of the
cluster were admitted by that hub and are passed on to this hub's
servers by the second rule alone.

### The status page

Served over plain HTTP on an address of its own, which `-status`
sets (`127.0.0.1:8475` unless told otherwise, so it is reachable from
the hub's own machine only), as plain HTML with no scripts. It is not
at the hub's public address, which serves `/link/v1` and
`/.well-known/churr` and nothing else. It shows totals only: the hub's name and
fingerprint, its software version, how long it has been up, how many
servers are joined and how many are connected, how many streams are
open, how many channels the hub owns in each listing mode (three
numbers and no names, ruling R59), how many hubs its cluster has, how
many of its trunks are connected and how many channels of other hubs
it carries or lists (three numbers, no hub named), envelopes and bytes carried in
the last hour and day, the size
of the log, how many typing and presence frames it has passed on and
how many it has dropped under pressure since it started (on arrival,
and for servers that were behind), and of the clocks: whether the hub runs in synchronised
mode, how many servers have a measured clock, the lowest, middle and
highest round trip among them, and the largest offset, uncertainty,
one-way jitter and asymmetry estimate. It names no server, no
stream, no channel, no manager and no moderator: each server's own
figures are for the operator, with `churr-hub servers`; channel
names, managers, moderators and member servers are shown only to the operator, with
`churr-hub channels`, and through the directory to member servers
for listed channels. An operator can turn it off.

## 23a. Channels a hub owns

A named channel shared through a hub belongs to the hub (ruling R56).
The hub keeps one **record** per channel and is the only party whose
signature changes it. Instances ask for changes; the hub decides,
signs the record whole at the next version, and sends it. It makes
and keeps every key epoch's stream key of the channel (section 10).

**Trust.** The hub owns the channel and is trusted as its owner, and
nothing in the channel is kept from it (ruling R109). The instances it
joins and approves are trusted to keep the channel's contents from
people who are not allowed to see them; even a private channel is not
encrypted against them or against the hub, and sealed chats are for
that (`SEALED.md`).

### The record: `channel`

A signed block from the hub (section 6):

    signed: {"stream_id": "0193d100-…", "hub": "hub.example",
             "hub_key": "…", "home_hub": "hub.example", "version": 7,
             "name": "launch", "description": "Getting 2.0 out",
             "listing": "request", "private": false,
             "servers": ["a.example", "b.example", "c.example"],
             "managers": [{"account_id": "…", "server": "a.example",
                           "username": "sam"}, …],
             "moderators": [{"account_id": "…", "server": "b.example",
                             "username": "bea"}, …],
             "invited": ["d.example"], "requests": [],
             "epoch": 4, "joined": ["c.example"], "left": [],
             "history_days": 30, "at": "2026-11-14T09:30:00.000Z"}

| Field | Meaning |
|-------|---------|
| `hub`, `hub_key` | The hub that signed it and the key it signed with |
| `home_hub` | The hub that owns the channel and is its time reference. The same as `hub`, except in the one record by which an operator hands the channel to another hub of the cluster (section 23b) |
| `version` | From 1, one more with every change. An instance ignores a record that is not newer than the one it holds |
| `name`, `description` | The channel's name and description on the hub. Each instance's own name for its copy stays its own |
| `listing` | `unlisted`, `request` or `open` (below) |
| `private` | The channel is private, for good. Its listing is `unlisted` and stays so ("Private channels", below) |
| `servers` | The member instances, longest-standing first |
| `managers` | Accounts (`ChannelAccount`: `account_id`, `server`, `username`) that hold the channel as an owner would. At least one |
| `moderators` | Accounts that keep the peace in the channel on every instance in it |
| `invited`, `requests` | Instances a manager has invited, and instances asking to join |
| `epoch` | The key epoch the hub began for this server list, with a key it made (section 10) |
| `joined`, `left` | What the list change that began the epoch was |
| `history_days` | How many days of history a joining instance is given, 0 to 90 |
| `closed` | The channel is over for every instance |
| `prepared` | Held for a move from a direct link that is not committed yet (section 21a) |

An instance that leaves or is removed takes its accounts off both
lists.

Beside the block, unsigned, the hub adds `have` (per origin, the
last counter in its log at that moment) and `home_hub`, as it does
for a relayed control frame (section 6).

**How an instance takes it.** The frame must arrive over the
instance's connection to a hub; `hub` must be that hub or a member of
the cluster roster it sent (section 23b); `hub_key` must be the key
pinned for that peer or the roster's key for that member; the
signature must verify under that key over the bytes as received; and
for a channel the instance already holds, `hub` must be the channel's
home as the last record it took said. Anything else is refused with
`bad_signature`, changes nothing, and goes to the audit log. A record
with `private` true and a listing other than `unlisted` is refused
with `bad_frame`. A record that makes a channel the instance holds as
private not private is refused with `forbidden`, goes to the audit
log, and changes nothing. From a record it accepts, and from nothing
else, an instance takes the channel's server list, its managers and
moderators, its listing mode, name and description, whether it is
private, its key epoch and its days of history. No frame or event
from an instance changes any of them.

The hub sends a record to the channel's instances (those that have
just joined first), each preceded by the key of the epoch it calls
for, and to the instances invited to it; to an instance
that was removed, once; and to an instance that reconnects, for each
channel it is in or invited to. An instance that is only asking is
sent nothing but, in the end, the answer.

### Sharing a channel to a hub: `channel_create`

The Owner of instance A opens a channel and shares it over A's link
to the hub, choosing the listing mode (`unlisted` unless they say),
instances to invite if any, and how much history a joining instance
is given. A makes the `stream_id` and sends

    signed: {"stream_id": "…", "name": "launch",
             "description": "Getting 2.0 out", "listing": "unlisted",
             "creator": {"account_id": "…", "server": "a.example",
                         "username": "sam"},
             "managers": [{"account_id": "…", "server": "a.example",
                           "username": "kim"}, …],
             "private": false, "history_days": 30, "at": "…"}

signed by A. `managers` are every manager of the channel on A: each
active person's account whose level is manager, as for a move
(section 21a). An Owner or an Admin is one only as anyone is. The hub checks the signature,
that `creator.server` is the sender, that every manager is an account
of the sender, the name (1 to 64 characters, no control characters),
the description (at most 1,000), the days of history (0 to 90), and
A's limit on channels. A
`channel_create` with `private` true and a listing other than
`unlisted` is refused with `bad_frame`. The hub then owns the
channel: record version 1, A its only instance, the person who shared
it and every manager named its **managers**, key epoch 1, begun with
the hub's key, which it sends A ahead of the record. On A the stream's
owner is the hub from the start. An
instance's own row for a hub in `servers` stands for it there, so
every rule that asks "is this the owner server" answers no for every
instance.

The channel stays as it is on A: its members, their levels and first
joins, its settings, pins and history. Nobody is announced.

A then invites each instance named, as its manager's first actions.

A knock channel and an announcement channel are not shared. A private
channel with groups in it is not shared through a hub (`conflict`):
its groups are removed first.

An instance never offers a named channel as its own over a hub: an
`offer` of kind `channel` that arrives over a hub is declined with
`not_allowed` and nothing is stored.

### Managers, moderators and their actions: `channel_action`

**Managers** hold the channel as an owner would. A manager may:
invite an instance; approve or decline an instance's request to join;
have an instance removed; change the name and the description; set
the listing mode; add and remove managers and moderators; and close
the channel. A channel keeps at least one manager.

**Moderators** keep the peace. A moderator may delete any message for
everyone (`message.removed`); admit people to a private channel and
decline their requests; remove people on every instance
(`member.removed`); and on their own instance's copy, time people
out, set slow mode and delete others' messages. A manager may do all
a moderator may.

Authority comes from the hub's two lists and from nothing else: not
from a role or a level on any instance. The hub checks it for its own
actions. Every instance checks it for the events, as an account of
the envelope's origin.

A manager's actions are asked of the hub:

    signed: {"stream_id": "…", "action_id": "…", "action": "approve",
             "by": {"account_id": "…", "server": "a.example",
                    "username": "sam"},
             "server": "c.example", "at": "…"}

| `action` | Also carries | The hub |
|----------|--------------|---------|
| `invite` | `server` | Adds it to `invited`; if it had asked, admits it |
| `approve` | `server` | Admits it (`not_found` if it has not asked) |
| `decline` | `server` | Takes it off `requests` and tells it `forbidden` |
| `remove_server` | `server` | Takes it out, with its managers and moderators; or withdraws its invitation |
| `describe` | `name`, `description`, either or both | Changes them |
| `set_listing` | `listing` | Changes it. For a private channel only `unlisted` (`forbidden` otherwise, whoever asks) |
| `add_manager` | `account` | Adds the account to the managers. Its instance must be in the channel |
| `remove_manager` | `account` | Removes it. A channel keeps at least one (`conflict`) |
| `add_moderator` | `account` | Adds the account to the moderators. Its instance must be in the channel |
| `remove_moderator` | `account` | Removes it |
| `close` | | Closes the channel for every instance |

**A manager's action is signed by the manager's home instance and
accepted when that account is on the hub-signed manager list.** The
home instance signs only for an active account of its own that the
record it holds names as a manager of its own. The hub verifies the
signature against the key of the instance on the connection, and
accepts the action only if `by.server` is that instance and
`by.account_id` is on the list as an account of that instance
(`forbidden` otherwise). So an instance cannot act for another
instance's manager, and an account cannot act through an instance
that is not its home. An action the hub accepts becomes the next
record. `join` (below) uses the same signed block.

**Deleting a message for everyone** is not asked of the hub: it is an
event in the channel like any other, which the hub's holder applies as
every instance does. The manager or moderator deletes the message on their
own instance as any member deletes one. Their instance lets them,
whatever their role and level there (`PERMISSIONS.md` section 14.1),
and submits `message.removed` naming them, with their profile.
**Every instance that receives it checks for itself** that
`removed_by_id` is on the manager or moderator list of the record it
holds, as an account of the envelope's origin, and rejects it
otherwise (`not_moderator`). Nobody else deletes another's message
in a copy: its managers are those on the hub's list, and an Owner or
Admin on neither list is no manager of it.

The hub's operator can appoint and dismiss managers and remove an
instance from a channel, as the last resort (section 23). Those too
reach the instances as a record the hub signs.

### Listing modes, the directory, and joining

| `listing` | Who sees the channel | How an instance joins |
|-----------|----------------------|--------------------|
| `unlisted` | Its instances, and instances a manager invites | By a manager's invitation |
| `request` | Every instance on the hub, in the directory | Its Owner asks; any manager approves or declines. Or by invitation |
| `open` | Every instance on the hub, in the directory | Its Owner joins, without anyone's leave |

A private channel is `unlisted` for good.

**The directory.** An instance asks with `{"frame": "directory",
"id": 9}` and the hub answers with a signed block, `re` 9:

    signed: {"hub": "hub.example", "hub_key": "…", "at": "…",
             "channels": [{"stream_id": "…", "name": "launch",
                           "description": "…", "listing": "request",
                           "managers": […], "servers": 3,
                           "yours": "requested"}, …]}

It lists the channels whose mode is `request` or `open`, with name,
description, mode, managers and the number of instances: not their
names. `yours` is the asking instance's own place (`member`,
`invited`, `requested`, or absent). An unlisted channel is never in
it, and a private channel is never in it. An instance takes a
directory only under an accepted hub key.

**Joining.** An instance's Owner accepts an invitation, or picks a
channel from the directory, and gives the local channel its name,
section and privacy as for any offer (section 8). The instance then
sends `join`:

    signed: {"stream_id": "…", "action_id": "…",
             "server": "c.example", "at": "…"}

| The instance | The hub |
|------------|---------|
| is invited | Admits it |
| is not, and the channel is `open` | Admits it |
| is not, and the channel is `request` | Records the request; the managers see it in the record. On approval admits it |
| is not, and the channel is `unlisted` | Refuses: `forbidden` |

An instance that asked is told `forbidden` when a manager declines
(at once, or when it next connects); its local channel stays as an
archived local channel. An instance gives up an invitation or a
request, or leaves a channel it is in, with `leave` (section 6).

The Owner who accepts is put in the new copy as a member, and as a
manager only if the hub's manager list names them. In a private
channel the copy starts with nobody, not even that Owner (below).

**Admitting** an instance is one change to the record: it joins
`servers`, and `epoch` goes up by one, begun with a new key of the
hub's. The hub delivers the stream to it from that moment, and notes
what its log held of each origin at that moment. Whenever the hub
sends that instance the record, then or when it next connects, the
`have` beside it is that note: the instance starts from there, and
asks for what came before as history (section 13).

**An instance that was away when it was admitted** was sent nothing:
a key is a direct message, and nobody keeps one for an instance that
is not connected. When it returns it is sent the key and the record,
and is in the channel; a key it still lacks it asks the hub for with
`key_request` (section 10). It asks the home hub for its history as
any instance let in does, with `history_request` carrying `first:
true`, and the hub answers an instance in the record's list with the
snapshot a joining instance is owed, members included. Until it has
that, it knows none of the channel's members and takes nothing they
say.

### The hub holds the whole channel

The home hub owns a channel in full: its keys, its contents and
history, its members, and its settings in the record. It holds the
channel whole, in the hub's **holder**: the same code an instance
holds a copy of a channel with (the link, the conversation service,
the permission rules and a file store), run inside `churr-hub` against
a database of the hub's own with the instance's schema and migrations
(section 23, "What it stores"). There is no second set of rules for
applying an event. Nothing of a hub-owned channel depends on any
instance staying: an instance that leaves or drops off for good takes
nothing of the channel's history with it, and an instance that joins
is given it by the hub with every other instance switched off.

- **On the link.** The holder dials its own hub on an internal
  connection under the hub's own name and keys, and speaks the frames
  an instance speaks. The hub knows it by those keys, which nobody else
  holds. It is in no record's server list and is shown nowhere as an
  instance of a channel.
- **What it is sent.** Every record of each channel the hub is the
  home of, with the key of the epoch it calls for ahead of it; every
  envelope of such a channel, from the hub's own servers and over the
  trunks; and the log, by `catch_up`, as a server is. A key it lacks
  it asks the hub for, and is given it whatever the epoch.
- **What it does with an envelope.** It opens it and applies it as an
  instance in the channel would: the origin's signature is checked
  against the key the hub's roster gives the origin, the authority
  rules of section 12 are applied to the origin, and an event about a
  message not yet there is held. An event the holder fails to apply
  because its own database or disk failed is kept and tried again until
  it is applied, and is in its log and audit log meanwhile (section 13,
  "When this server fails"): the channel's whole copy is never short of
  it. Messages are kept as they stand:
  edited, deleted, reacted to, ticked, answered. Members join and
  leave as `member.joined`, `member.left` and the record say; the
  members of an instance that leaves or is removed leave the copy, and
  what they wrote stays, with their profiles.
- **When the hub becomes the home** of a channel shared to it or moved
  onto it from a direct link, the holder asks the channel's instances
  for all they hold of it (section 13, "History on joining") and takes
  from the log what came after. What the log delivers before that
  history has arrived and cannot be applied without it waits for it
  (section 13, "Events that refer to others"); nothing is lost for
  arriving first. Of a channel handed to it by another
  hub of its cluster it asks that hub's holder first, which holds it
  whole (section 23b, "Failure of a home hub, and moving a home", step
  6). It keeps in its database whom it still waits on, and asks them
  until each has answered: after a restart, and whenever one of them
  may have come back.
- **Files.** It fetches every file the channel's events name (an
  attachment, a card's image, a custom emoji, a member's picture) from
  its origin when the event passes, checks it by its hash and keeps it
  (section 15).
- **What it answers.** The direct messages addressed to the hub that
  are not `key_request` the hub hands to its holder: `history_request`
  and `blob_request` from an instance in the record's list,
  `open_request`, and the answers to what the holder asked. The
  answers leave as the hub's own direct messages, signed with the
  hub's key and sealed to the asker. An instance takes them from the
  channel's home hub as it takes the channel's keys from it. What comes
  for the holder while it is not connected to its hub (the hub has just
  started, and an instance connected first) the hub keeps, the last 256
  messages, and hands it the moment it connects.
- **What it never does.** It originates nothing: it has no accounts,
  and no event of any channel is its own. It holds nothing of a
  channel homed on another hub, and nothing of an ad-hoc channel or a
  sealed chat (ruling R59).
- **How long.** For as long as the hub has the channel. Nothing ages
  out of the copy: it is not the 30 days of the log. Each instance's
  retention is its own copy's (section 22); a `messages.expired` an
  instance sends asks the others, the hub among them, to erase its
  own people's messages, as it always has. A closed channel's copy
  goes when the hub sweeps the stream, 30 days after it closed.

### Who is in a channel

The hub joins and approves an instance; each instance's people are
then in its own copy. In a channel that is not private they join and
are added as in any shared channel, under the instance's own rules,
except that the copy's managers are those on the hub's manager list
(`PERMISSIONS.md` section 14.1). In a private channel they are
admitted by its moderators and managers (below).

In any channel a hub owns, a moderator or manager removes someone on
every instance. Their home instance submits `member.removed`
(`account_id`, `server`, `by_account_id`, `by_account`, `blobs`), and
every instance that receives it checks that `by_account_id` is on the
manager or moderator list as an account of the origin (`not_moderator`
otherwise) and takes the person out of its copy.

### Private channels

A private channel may be shared through a hub (ruling R94), or moved
onto one from a direct link (section 21a). `channel_create` and the
record carry `private: true`, for good.

- **Never listed.** Its listing is `unlisted` and stays so. The hub
  refuses `set_listing` to `request` or `open` from anyone
  (`forbidden`) and never puts it in the directory. Nobody outside it
  sees it on any instance.
- **Instances join only by a manager's invitation.** An instance that
  joins keeps its copy private however its Owner accepts, and its copy
  starts with nobody of its own, not even the Owner who accepted.
- **No groups.** Sharing or moving a private channel with groups in it
  is refused (`conflict`: remove its groups first), and groups cannot
  be added to it.
- **Nobody adds people on any instance.** Not a local manager who is
  not on the hub's lists, not an Admin, not the Owner (`forbidden`).
  People are admitted only by a moderator or manager, from any
  instance in the channel, in two ways:
  1. A moderator or manager admits directly someone their instance
     knows: its own people, or people of other instances in the
     channel it has met in a shared conversation.
  2. The person asks, on their own instance, with the join code: the
     channel's `stream_id`, which a moderator or manager gives them.
     Owner, Admin and Member accounts may ask. Ten requests an hour
     that were not accepted, counted with knocks. Any moderator or
     manager approves or declines.

The events, inside the channel's envelopes (section 11):

| Event | Sent by | Every instance |
|-------|---------|----------------|
| `member.requested` (`account`, `blobs`) | The person's home instance | Keeps the request as a knock on its copy, so moderators and managers everywhere see it; the home hub's holder keeps it too, and gives the requests still waiting to an instance that joins later in its snapshot (section 13) |
| `member.admitted` (`account_id`, `server`, `by_account_id`, `by_account`, `blobs`) | The acting moderator's or manager's home instance | Checks the acting account (`not_moderator` otherwise) and answers the request if one waits. The admitted person's home instance adds them to its copy |
| `member.request_declined` (the same fields) | The same | Checks the acting account and answers the request |

On `member.admitted`, the admitted person's home instance adds them to
its copy. That is an ordinary join: it travels as `member.joined`, and
that instance posts the `member_joined` notice naming who admitted
them (`by_id`). The notice is posted once and seen on every instance.
First-join visibility, the three-hour rejoin window, pins and quotes
apply on each instance to its own members (`PERMISSIONS.md` section
4.4). `member.removed` takes a person out on every instance, as in any
channel a hub owns.

### Keys

The home hub makes every key (section 10). On every change to the
server list (an instance admitted, leaving, removed by a manager or by
the operator, or the hub's first epoch for a channel moved onto it or
handed to it) it signs the record at the next epoch and, in the same
transaction, makes that epoch's key and begins the epoch. It sends the
key to every instance in the list ahead of the record. Nothing waits
on any instance: an instance that is away asks for its key when it
returns, and the channel goes on without it. An instance is never
given the key of an epoch from before it joined or after it left.

### What stays each server's own

Each server's levels, pins, slow mode, word filter, time-outs,
archiving, retention, its name for its copy, the largest file it
takes: all local, as in any shared channel (section 12), with two
exceptions: who is a manager of the copy follows the hub's manager
list (`PERMISSIONS.md` section 14.1), and in a private channel nobody
adds people but its moderators and managers. Levels do not travel: a
remote account is never a manager of another instance's copy, and a
manager acts on another instance only through the hub's actions and
the events above. The instance that shared a channel keeps no special
role once it has.

### Ad-hoc channels and sealed chats across a hub

They are not the hub's (ruling R59). They have no record, no name
on the hub, no listing and no managers or moderators; they keep the
owner-server frames of section 6 and the rules of section 16, and
the hub carries them without knowing what they are.

## 23b. Trunks between hubs

Hubs can be joined to one another by **trunks** into a **cluster**
(ruling R58). A channel then reaches servers on any hub of the cluster.
Everything a server says to its own hub, and everything its hub says
to it, is as in the sections before; this section is what is new
between hubs, and the three things a server sees of it: the cluster's
roster, a channel record signed by a hub it is not connected to, and
its clock against that hub.

**What a cluster does not do, said first.**

- **It does not re-home a channel by itself.** If a channel's home
  hub fails, the channel is read-and-post only among the servers that
  can still reach each other until that hub returns. Nobody's server
  list, managers, moderators, listing or record change in that time, and no hub
  takes the channel over. The only way a home moves is an operator's
  command, when both hubs are up (below).
- It does not make an ad-hoc channel or a sealed chat its own. Those
  cross a cluster as they cross a hub: owned by a server, carried by
  the hubs on the way, none of which knows what they are ("Streams
  servers own", below).

### The cluster and its roster

A hub that has never been joined to another is a cluster of one,
which it founded. A cluster is named by the fingerprint of its
founder's signing key (section 3, the 20 bytes).

The **roster** is a set of entries, one per admission of a hub. An
entry is two signed blocks at most and three signatures:

    member:  {"cluster": "<20 bytes>", "hub": "hub-b.example",
              "sign_key": "…", "box_key": "…",
              "url": "wss://hub-b.example/link/v1",
              "via": "hub-a.example", "joined_at": "…"}
    sig      by the hub named, over the block as `cluster_member`
    via_sig  by the hub named in `via`, over the same bytes as
             `cluster_member/via`. Absent on the founder's entry.

    gone:    {"cluster": "…", "hub": "hub-b.example", "joined_at": "…",
              "by": "hub-b.example", "at": "…"}
    gone_sig by `by`, over the block as `cluster_gone`. `by` is the
             hub itself (it left) or its `via` (it was removed).

`via` is the member the hub trunked to when it joined. **The roster is
therefore the shape of the cluster, and that shape is a tree**: a hub
is let in over one trunk, to one member, and has no other.

A roster is checked whole, by hubs and by servers alike, and refused
whole (`bad_signature`) if any of this fails:

1. Every entry names the same cluster and carries a signing key and a
   box key. Exactly one has no `via`: the founder, and the fingerprint
   of its key is the cluster's name.
2. Every entry is signed by the key it names.
3. Every other entry is countersigned by the hub it names as `via`,
   under the key of an entry of that hub with an earlier `joined_at`.
4. A `gone` block is about the entry it rides on and is signed by that
   hub or by its `via`.
5. No hub has two entries that are not gone.

The members are the entries that are not gone and are joined to the
founder by a chain of entries that are not gone. A hub whose `via` is
gone is gone with it.

Entries are only ever added, or ended. Two rosters of one cluster are
merged by taking every entry of both (an ended one over the same one
not ended), and the result is checked again before it is kept. Hubs
send the whole roster in a `cluster` frame over every trunk when it
comes up and whenever it changes.

### Joining, leaving, removing

| Command | Where | Does |
|---------|-------|------|
| `churr-hub trunk invite [--for NAME] [--expires DURATION]` | A member | Prints an invitation, as for a server, that only a hub can use |
| `churr-hub trunk join INVITATION` | The hub that joins | Dials the member and asks to be let in |
| `churr-hub trunk cancel` | The hub that joins | Gives up a join that has not happened |
| `churr-hub cluster` | Any | Lists the hubs: fingerprint, `via`, this hub's clock against each, the trunks |
| `churr-hub cluster leave` | A hub with nothing joined through it | Ends its own admission |
| `churr-hub cluster remove HUB` | The `via` of that hub | Ends that hub's admission |
| `churr-hub rehome STREAM HUB` | A channel's home hub | Moves the channel's home (below) |

**A trunk is the link's own connection** (section 5): the same
`/link/v1`, the same five handshake frames, with `kind` `hub` on both
sides. Both ends prove their signing keys over the
transcript, so the authentication is mutual; the invited hub dials and
reconnects, as a server does; the keys are pinned at the first
handshake, the dialler's against the invitation's fingerprint and the
listener's by the invitation's proof. A server's invitation is refused
to a hub and a hub's to a server.

After `ready` each end sends `cluster`. The hub that dialled and is
not yet a member answers the roster it is shown with

    {"frame": "cluster_join", "id": 3, "cluster": [ <its own entry,
      naming the listener as via and the listener's cluster> ]}

and the listener checks it (signed by the trunk's pinned key, naming
this trunk's two hubs, later than its own entry), countersigns it,
adds it and sends the new roster to every neighbour and every server.
The new hub takes the roster that lists it in place of its own.

**Loops are refused by both ends.** `trunk join` refuses on a hub
whose cluster has another member ("a hub is in one cluster at a
time"), naming the loop when the invitation is from its own cluster.
The listener refuses the handshake of a hub that is in its cluster
and has no trunk to it with `conflict`, before any invitation is
used up, and refuses a `cluster_join` from one the same way.

**Leaving.** `cluster leave` is refused on the founder while others
remain, on a hub others joined through, and on a hub that is the home
of a channel with a server on another hub (move the home or remove
the server first). The same holds for a stream one of its servers owns
that has a server on another hub. Otherwise the hub signs `gone` on
its own entry, sends the roster over its trunk, closes the trunk with
`bye` `removed`, and is a cluster of one again under a new entry.
**If the hub is not serving when the command is run, or its trunk is
down, it has still left**, and the roster that says so is owed: the
hub keeps it, goes on dialling the hub it left through, and the first
thing it says over the trunk when it next comes up is that roster,
and `bye`. Until then the other hubs still list it.
`cluster remove`, on the `via`, signs the same block in the `via`'s
name for a hub that is gone for good; the removed hub learns it when
it next sees a roster.

When a hub leaves the cluster every other hub forgets its servers,
and each home hub takes those servers out of its channels as it does
a server its operator removed (section 21). The copies those servers
hold of channels and streams homed elsewhere stop there, **and the hub
that left tells them so**: for each stream it carried for another hub
it sends each of its servers that was in it or invited to it

    {"frame": "stream_close", "stream_id": "…", "reason": "hub_left",
     "hub": "hub-b.example"}

unsigned, at once if the server is connected and otherwise whenever it
next connects. A server takes it only from its own hub and only for a
stream whose home is another hub; its copy becomes an archived local
channel (or a read-only chat), as when it is removed, and its audit
log gets `link.removed_from_stream` (with the hub and the reason), the
same line it writes when the owner server or the home hub removes it.
The hub takes nothing more of those streams.

### How servers learn the roster, and each other

A hub sends its servers the `cluster` frame after `ready`, before any
channel record, and whenever the roster changes:

    {"frame": "cluster", "hub": "hub-b.example", "cluster": [ entries ]}

A server checks it by the five rules above, and two more: it lists
the server's own hub under the key pinned for that peer, and it gives
no hub the server has met a different key. A roster that fails is
refused whole, the one held before stays in force, and the audit log
gets `link.cluster_refused`. **The keys of the roster's members are
the hub keys the server accepts on that peer** (section 23a): a
record is taken if it names one of those hubs with that hub's key
and the key signed it; the box keys are what a server seals a direct
message to a hub of the cluster with. A server keeps the roster in
memory; it is sent again on every connection.

The `roster` frame (section 6) lists, in a cluster, the servers of
every hub. Each hub signs the list of its own servers (`hub_servers`,
the content of a roster) and the other hubs pass it on unchanged; a
hub checks each list under its hub's key from the cluster roster and
then signs the whole for its own servers as before. A server
therefore trusts its own hub for other servers' keys exactly as it
did (section 24). A server name is on one hub of a cluster: a list
that claims a name another hub has loses that name, and a server
that dials a second hub of the cluster is refused (`key_mismatch`).

### The home hub

Every hub-owned channel has one **home hub**: the hub it was shared
to. The home hub owns the channel. It alone signs the record (`hub`
and `hub_key` name it, and `home_hub` equals `hub`), it is the
channel's time reference, it makes and keeps the channel's keys
(section 10), and its operator
has the last-resort powers of section 23. **A server connected to
another hub of the cluster uses that hub as its relay**: it submits
to it, is delivered to by it and catches up from it, with the frames
of section 6.

- **The record** travels from the home hub over trunks as the home
  signed it, to every hub with a server it concerns (in the channel,
  invited, or just taken out) behind it, and, when the channel is
  listed (`request` or `open`), to every hub, whose servers see it in
  the directory. Each hub keeps the newest version it has verified
  under the home hub's key from the roster, gives it to its own
  servers, and derives its routes from the server list. Beside it,
  unsigned, the home sends each hub the `have` of the servers behind
  it that have just joined (`joined_have`).
- **A server takes a record** if the hub that signed it is a member
  of the roster under the key the record names, and, for a channel it
  already holds, only if that hub is the channel's home as the last
  record said. Anything else is `bad_signature` and the audit log.
- **Requests** (`channel_action`, `join`, `leave`) are the home hub's to
  answer. A hub that is not the home passes the server's frame on,
  whole, in

      {"frame": "ask", "hub": "<home>", "server": "<who asks>",
       "inner": { the frame }}

  hop by hop. The home verifies the frame's signature under the
  asking server's key and treats it as from that server. A refusal
  comes back to the server as the `error` frame it would have had,
  with the same `re`. **If the next trunk on the way to the home is
  down, the hub that finds it so answers `unavailable` at once.**
- **Direct messages** to a server of another hub (the home hub's keys
  among them), and any frame for one server (a refusal, the word that a request was declined), travel
  in `{"frame": "forward", "server": "…", "inner": {…}}`. A direct
  message for a server that is not connected is not kept, and its
  sender is told `unavailable` (section 7); anything else for a server
  that is not connected is kept by its own hub, at most 100 a server,
  and sent when it connects.
- **Keys.** The home hub makes them and sends them in `forward`. A
  server asks the home hub for one with a `direct` addressed to that
  hub, which its own hub carries there in `ask`. An edge hub holds
  none of a channel's keys.

### The clock, composed

A channel is timed by its home hub's clock (section 13). A server is
measured only by its own hub (section 5), so its offset from another
hub is made of pieces:

- **Each trunk is measured by the same `ping` and `pong`.** The hub
  that listens pings and measures the hub that dialled; the `clock`
  on its pings tells the other the figures. Both ends then hold the
  two clocks' difference and its uncertainty.
- **Each hub holds its clock against every other hub's**: the sum of
  the offsets of the trunks on the way, and the sum of their
  uncertainties. It learns them from `hub_clocks`, in which a hub
  tells a neighbour its own figure for every hub that is not on that
  neighbour's side (`offset_ms`, `uncertainty_ms`, and `age_ms`, how
  old the oldest measurement in it is); the neighbour adds its own
  trunk. A figure is kept while a trunk is down and across restarts,
  and trusted less with age, by 50 parts per million, like any other.
  The same frame carries `reach`: the names of the hubs the sender can
  reach now that are not on the receiver's side. It is sent again
  whenever a trunk comes up or drops, so every hub knows within a
  moment which hubs it can reach, and answers `unavailable` for a home
  hub it cannot without trying.
- **A server is told the composed figure by its own hub**, with every
  `ping`, beside `clock`:

      "homes": {"hub-a.example": {"offset_ms": 700,
                 "uncertainty_ms": 96, "measured_at": "…"}}

  one line per other hub of the cluster: the server's clock minus
  that hub's, which is the hub's measurement of the server plus the
  hub's own figure for that hub, the uncertainties added. An event
  in a channel whose home is that hub is stamped with it: `offset` is
  that figure and `at` the server's clock minus it. A server
  writes these down as it is told them (setting
  `link.home_clocks.<peer>`), as it does its clock against its own
  hub, and stamps by them after a restart, trusting them less for
  their age, without waiting for its hub's first ping.

**The checks on `at`, at the edge hub.** The hub a server is
connected to makes every check of section 13 on what that server
submits, whoever the channel's home is, because it is the one that
measured the server:

| Check | Refusal |
|-------|---------|
| It has measured the origin, and has a figure for the home hub's clock. | `clock_unknown` |
| `offset` is its composed figure for that origin and that home, within three times the composed uncertainty and never less than 100 ms. | `clock_offset`, with the composed figure in `clock` |
| `at` is not more than 30 seconds ahead of the home hub's clock as this hub reads it. | `clock_ahead` |
| `at` is not more than 30 seconds behind it, unless inside a stretch the origin was disconnected from this hub. | `clock_behind` |
| `at` and `n` are above the origin's last. | `not_monotonic` |

It then writes the envelope to its own log, answers `accepted`,
delivers to the channel's other servers of its own, and ships it on.
The key-epoch rules of section 6 (steps 7) are applied here too,
against the record and the highest epoch this hub has seen.

**What a hub does with an envelope that reaches it over a trunk, the
home hub included.** The edge hub's judgement of an envelope's time
stands across the cluster. **No hub further on refuses an envelope a
correctly behaving edge hub accepted**, so that an envelope which has
been acknowledged to its origin and delivered on one side is never
missing on another. One of four things happens, and only the last is
a refusal:

| Outcome | When |
|---------|------|
| Taken: logged, delivered to this hub's servers of the stream, shipped on | Otherwise |
| Held already: counted as taken, nothing more | Its `origin_event_id` is in this hub's log, or its `n` is not above the origin's last here |
| Put off: nothing written, the origin's counter unmoved, the stream asked for again (`sync`) the moment what it waited for arrives (a record of the stream, or a hub's list of servers); a missing envelope is asked for at once. As a net under that, it is asked for again after 300 ms, then less and less often up to 38 s | This hub has not yet been sent the stream's record or control frame, or the origin's key (its hub's list of servers); or `prev` shows an envelope of the origin is missing |
| Refused: not logged, the origin's counter moved past it, reported in `relay_ack` and back to the origin's hub | It is malformed, unstamped or over size; its signature is not its origin's; its origin is not on the side it came from; its `at` is not above the origin's last here |

Every reason for a refusal is a check the edge hub made first, on the
same bytes, under the same key, against counters that only grow and
that this hub has only from that edge. So a refusal here means the
neighbour did not keep the protocol; an origin behind an edge hub that
does is never refused further on. A stream that is closed here takes
nothing more: there is nobody to give it to.

**A hub further on still compares the time, and counts.** With u =
three times this hub's uncertainty about the home hub's clock and the
origin's hub's, at least 100 ms, and h = 60 seconds for each trunk
between the origin's hub and this one, an envelope is **suspect** if
`at` is more than 30 s + u ahead of the home hub's clock as this hub
reads it; or more than 30 s + u + h behind it and not, give or take 30
s + u + h, inside a stretch the trunk it arrived over was down, nor
inside a stretch the sending hub names beside it (`window`, by the
home hub's clock, ended no more than 24 hours ago and not in the
future). A suspect envelope is taken like any other, counted for the
operator and written to the hub's log with the trunk it came over,
and never reported to its origin. A hub that took an old
envelope under a stretch (an origin of its own that was away, a trunk
of its own, or one named to it) names that stretch beside the envelope
when it ships it on. A hub does not check `offset` (it did not measure
the origin) or the key epoch.

Why the time is not refused further on: no bound a later hub could
apply is one a correct edge is certain to have kept. Its reading of
the home hub's clock is an estimate; a trunk may be down for longer
than any stretch is remembered; and the envelope has by then been
applied by the servers of the edge's side, which nothing takes back.
Section 26 says what was chosen and what it costs.

### Routing, batches and flow control

Each hub works out, for each channel it holds a record of, from the
record's server list and from which hub each server is on:

- its own servers in the channel, to which it delivers;
- **the trunks that lead to a server in the channel**, and the trunk
  that leads to the home hub, which is sent everything whether or not
  it has a server in the channel.

**Nothing of a channel is sent over any other trunk.** A hub with no
server of a channel behind it, that is not its home and does not lie
between two that are, is sent no envelope of it and stores none; of
an unlisted channel it is not sent the record either.

A frame that crosses a trunk on another's behalf (`ask`, `forward`,
`stream_ctl`, and the record beside a `channel_commit`) is carried
in `inner` as the bytes of that `Frame`, exactly as its sender wrote
them, and handed on as they came: no hub on the way writes them
again. An envelope in a `relay` is the `Envelope` message as the
sending hub's log holds it.

Every hub that does carry a channel keeps its own log of it, 30 days
like any hub's, and serves its own servers' `catch_up` from it.
**A trunk ships a hub's log**: every row that did not arrive over
that trunk, in the order it was written, of whichever streams the
trunk carries, in

    {"frame": "relay", "id": 41, "items": [
       {"envelope": {…}, "prev": 117, "window": [from, to]}, … ]}

    {"frame": "relay_ack", "re": 41,
     "refused": [{"stream_id": "…", "origin_event_id": "…",
                  "origin": "…", "code": "clock_behind", "message": "…"}]}

- `prev` is the `n` of the same origin's envelope before it in the
  sender's log; `window` is present only as described above.
- **Limits per trunk:** a batch holds at most 256 envelopes and
  524,288 bytes of them, of any number of streams. At most **8
  batches** may be unanswered on one trunk; the sender ships no more
  until a `relay_ack` arrives. The receiver writes a batch to its log
  in one transaction and then answers.
- Nothing waits in memory for a trunk: the log is the queue. A sender
  that has had no room for 60 seconds closes the trunk. When it
  returns, that was a stretch of disconnection like any other.
- A cluster is a tree, so there is one way from any origin to any
  hub and an origin's envelopes reach every hub in the order it made
  them. A receiver that is shown a `prev` above what it holds of an
  origin it already has rows of drops the envelope and says what it
  holds (`sync`, below); it comes again in its place.

`ephemeral` envelopes follow the same routes and are logged nowhere.

### Catching up between hubs

When a trunk comes up, after `cluster`, each hub sends the other its
own servers and the lists it holds from its side, its clocks, every
record the other side should have, and then

    {"frame": "sync", "streams": {"<stream_id>": {"v": 7,
        "have": {"a.example": 211, "c.example": 40}}, …}, "done": true}

(in several frames if need be, the last with `done`): for every
channel it holds, the record's version and, per origin, the `n` of
the last envelope in its log. The other hub

- sends again any record it holds a newer version of;
- ships, for each stream the trunk carries, every row of its log
  whose `n` is above the figure given for its origin. For a stream
  the other hub holds nothing of, it starts instead from where the
  servers on that side start: the `have` the home hub noted when it
  let each of them in (section 23a). The same is done, without a
  `sync`, when a record brings a trunk into a channel's routes while
  it is up.

This is the only catch-up between hubs, and it is by per-origin
counters as a server's is (section 13). An envelope a hub already
holds (its `origin_event_id` is in its log, or its `n` is not above
the origin's last there) is counted as taken and nothing more happens:
**nothing is duplicated**, whatever is sent twice.

### Splits

When a trunk drops, the cluster is in two parts, and each goes on by
itself, as IRC does.

- **Both sides keep delivering among their own servers.** A hub takes
  what its servers submit, checks it as above, logs it, delivers it
  and ships it over the trunks that are up.
- **Changes to a channel's server list, managers, moderators, listing
  or record happen only on the side that can reach the home hub.** On
  the other side `channel_action`, `join` and `leave` are answered
  `unavailable`.
  The home side may change all of them, including removing a server
  that is on the far side, and a new key epoch may begin there.
- **What a server on the far side may stamp.** Exactly what it may
  when the cluster is whole, judged by its own hub: `offset` equal to
  the composed figure its hub last told it; `at` within 30 seconds of
  the home hub's clock as that hub reads it from the figure it held
  when the trunk dropped, and above the origin's own last. The hub's
  reading drifts from the truth by at most 50 parts per million of
  the split's length, and its tolerance on `offset` widens by three
  times that. Nothing on the far side can be stamped into the time
  before the split, or ahead of it.
- **On rejoin** the two hubs at the ends of the trunk each note the
  stretch it was down: from when its clocks were last compared before
  it dropped to when the new connection was made. They `sync`, and
  each ships what the other side missed. The hub next to the break
  takes an old envelope if its `at` lies in that stretch, by its own
  knowledge; every hub beyond, **the home hub included, checks `at`
  against the stretch that hub names**, by the rule above: inside it,
  give or take 30 s + u + h; ended within the last day; not ahead.
  What does not fit is counted as suspect and taken all the same.
- **Late messages take their place by `at`.** Each server applies
  what arrives as section 13 says: a message below the highest
  `order` already delivered there is marked `late`, sorts where its
  stamp puts it, and is unread. Every server ends with the same
  messages in the same order.
- Records the far side missed reach it by the `sync`'s versions. A
  server the home side removed during the split is told then. A
  server the home side admitted during the split is sent, after the
  rejoin, what the far side said during it under the epoch before its
  own. It has each such envelope opened by a server that holds that
  key (`open_request`, section 13) and applies it: it ends with what
  the far side said, reactions, edits and replies included, without
  its owner asking for anything.

### Failure of a home hub, and moving a home

**If a home hub is down or cut off, its channels are read-and-post
only among the servers that can reach each other until it returns.**
Envelopes flow, by the rules for a split. Every change to the
channel is answered `unavailable`. No key epoch begins. **There is no
automatic re-homing in this version**: no hub takes over a channel
whose home has failed, however long it is gone. When it returns it
is sent what it missed, and is the home as before.

An operator can move a channel's home to another hub of the cluster
**when both are up**: `churr-hub rehome STREAM HUB`, run on the
current home. **The move is all or nothing**: at every moment exactly
one hub is the channel's home, and the move happens in one
transaction on the new one.

1. The home refuses if the other hub cannot be reached, if it has no
   figure for that hub's clock, or if the two clocks are more than 15
   seconds apart (events are timed by the new home's clock from then
   on, and an origin's `at` only grows).
2. **Offer.** The home signs the next version of the record with
   `home_hub` naming the other hub and `hub` still its own (the
   **handover**), does not put it in force, and sends it to the other
   hub, hop by hop, with what it keeps beside the record (who shared
   the channel, who invited whom, where each server started from, the
   highest epoch begun, and in `keys` every stream key it holds of the
   channel with the server list each epoch began for, as a
   `HandoffKeys` message sealed from the old home to the other hub as a
   direct message is, so that no hub on the way can open it):

       {"frame": "rehome", "from": "<home>", "to": "<other hub>",
        "step": "offer", "offer": "<id>", "stream_id": "…",
        "signed": …, "sig": …, "state": {…}}

   From then until the offer is settled the old home is still the
   home and **signs nothing for the channel**: a manager's action,
   a join or a leave is answered `unavailable`, and no key epoch
   begins.
3. **Take.** The other hub, on an offer it has not seen settled,
   checks the old home's signature and the clocks as in step 1, and
   in one transaction takes the channel: it stores the handover, owns
   the channel, keeps the keys it was handed, sealed at rest under its
   own key file, and signs the next version itself, at a new key epoch
   it begins with its own key. From then on it answers `key_request`
   for every epoch of the channel, those of the old home's included.
   From that transaction it is the home in every way. Its holder
   (section 23a) takes the channel's contents from the old home's
   holder (step 6). It answers `step` `taken` with that record. An offer it will not take it refuses
   (`step` `refused`) and remembers as dead.
4. **Step down.** The old home, on a record of the channel later
   than its offer under the other hub's signature (in the answer, or
   arriving as any record does), stores it, deletes every key it held
   of the channel (the new home holds them), tells its holder by the
   handover and that record, and carries the channel from then as any
   hub does. The new home sends the handover before
   its own record to every hub and server the channel concerns, so
   each follows the chain: a server takes the handover because the
   old home signed it, and from then takes records only from the new
   home. A hub keeps a channel's last four handovers.
5. **No answer.** If none comes within 5 seconds the old home calls
   the offer off: whenever it can reach the other hub it sends `step`
   `cancel`. The other hub answers `taken`, with its record, if it
   had taken the offer; otherwise it marks the offer dead, so that a
   copy arriving late is refused, and answers `cancelled`. `refused`
   and `cancelled` carry a block signed by the hub that sends them
   (`rehome_answer`: the offer, the stream, `dead`), and only that
   lets the old home sign for the channel again.
6. **The contents.** The keys travel with the offer; the contents do
   not, since a channel's history and files can be any size. The new
   home's holder fetches them from the old home's holder, which holds
   the channel whole, the moment it is sent the handover (with, beside
   it, what the new home's log held of each origin then): it asks the
   old home with `history_request` carrying `first: true`, and the
   channel's instances after it if the old home does not answer or
   answers `short`, and
   asks for each file from the old home and from the file's origin by
   turns. Those direct messages go between the two hubs' holders as the
   hubs' own, over the trunks in `forward`. The new home goes on asking
   the old home until it has answered (section 13, "History on
   joining"). The old home's holder takes nothing more of the channel,
   answers only the new home for it, keeps the copy as it stood, its
   members and their profiles with it, and forgets it 7 days after
   it learned of the handover (the time a file is asked for before it
   is given up). A holder that was not connected to its hub at the
   handover learns of it when it connects: its hub sends it, for every
   channel it handed over, the record by which it did and the new
   home's newest. From then a hub that only carries the channel holds
   none of it.

Until one of those answers is in the old home's hands it cannot know
which hub is the home, and goes on signing nothing; the servers that
can reach the new home, if the offer was taken, are served by it
meanwhile. That is the only wait, it ends when the two hubs next
reach each other, and in it the channel is read-and-post only on the
old home's side exactly as during any split. There is no state in
which the old home has stepped down and the new one has not taken
over.

### Streams servers own, across a cluster

An ad-hoc channel or a sealed chat is a stream a server owns (section
16, ruling R59). **Its home is the hub it was opened on, the owner
server's own**: that hub keeps its server list, checks the owner's
control frames (section 6), and is its time reference. A server of
another hub of the cluster may be in it; `server_joined` naming one
is taken, where it was refused (`unknown_server`) before.

- **The control frame travels as the owner signed it.** The home
  sends it, with `have` and `home_hub` beside it as it does to a
  server, over each trunk that leads to a server it concerns, in

      {"frame": "stream_ctl", "inner": { the control frame },
       "joined_have": {…}, "also": ["<a server just removed>"]}

  A hub that receives one checks that it came from the side the home
  is on and that the signature is that of the server it names as
  owner (whose key it has from that server's hub), takes it only if
  its version is above the one it holds, keeps its own copy of the
  list for routing, gives the frame to its own servers that it
  concerns, and passes it on. It was the home that checked that the
  signer is the owner and the version the next. When a trunk comes
  up, each hub sends the last control frame of every such stream
  with a server on that side.
- **A control frame that reaches a hub which is not the home** (a
  server on another hub leaving; an owner on another hub, after
  ownership changed hands) is carried to the home in `ask`, and
  applied there as if the server were its own.
- **Envelopes** follow the rules of a channel's: checked by the hub
  the origin is on, against the home hub's clock as composed; logged
  by every hub that carries the stream; shipped, caught up and
  de-duplicated as above. A server that joins starts from the `have`
  the home noted when it was let in, on whichever hub it is. The
  server stamps by its clock against the stream's home hub, which
  the hub names beside the control frame that lets it in.
- **Offers, keys, history** are direct messages, carried by
  `forward` as any are.

A hub on the way learns what a hub learns of any stream: its
identifier, which servers are in it, and sizes. Not what kind of
stream it is. A stream that never leaves its hub is not told to any
other.

A move from a direct link (section 21a) crosses a cluster too: the
channel is prepared on the owner server's hub, the other server's
signed agreement names that hub, and the other server's hub carries
its `channel_commit` there.

### What a hub stores, and what the other hubs can see

Beside section 23's table: the roster entries, for good; its trunks
(name, address, pinned key, the trunk's clock, its last 8 stretches
of disconnection); each other hub's signed list of servers; its clock
against each hub; for each channel of another hub that it carries or
lists, the record as signed, the handovers, and where the servers
behind it start from; in the log, per row, the trunk it came over and
the stretch that excused its time; frames kept for a server that
was not connected; of each stream a server owns that crosses a trunk,
the owner's last control frame and where its servers start from; a
channel's home on offer, until it is settled, and the offers this hub
has taken or seen die; and the roster it owes a hub it has left
through, until it has been sent.

A hub of a cluster sees, of a channel homed elsewhere, exactly what
section 23 says a hub sees, and only for the channels it carries or
lists. It holds no key of it and no plaintext: keys, history and files
pass through it as direct messages it cannot open. The home hub alone
holds the channel's keys and its contents; an old home keeps its copy
for 7 days after handing the channel over, for the new home to fetch
(step 6 above).

## 24. Threats

| Threat                | What an attacker could try                    | What stops it, or does not |
|-----------------------|-----------------------------------------------|----------------------------|
| A malicious peer server | Post as an account of another server        | Stopped. Every server checks that an event's origin is the home server of the account it names (section 12). |
|                       | Remove messages, rename the channel, add servers | Stopped, unless it is the owner server, which holds those powers by design. |
|                       | Post as its own accounts whatever it likes, invent accounts, ignore levels | Not stopped. A server speaks for its own members. The owner can remove the server. |
|                       | Keep, copy or publish everything it was sent  | Nothing stops this. Sharing a channel with a server is trusting its operator with that channel. |
|                       | Ignore deletes, purges and retention          | Nothing stops this.        |
|                       | Send a file that differs from the one described | Stopped by the hash in the signed event. |
|                       | Flood                                         | Rate and size limits per peer; the owner can remove it. |
| A malicious hub       | Read content                                  | Not stopped for the channels it owns: it holds their keys, and keeps their whole contents. That is the trust placed in a hub as a channel's owner (ruling R109). Stopped for ad-hoc channels and sealed chats it carries, and for channels homed on another hub of its cluster: it holds none of their keys, nor any server's private key. |
|                       | Alter or forge an envelope or direct message  | Stopped by the origin's signature. |
|                       | Drop, delay or withhold from some servers     | Not stopped. A server that is delivered one of an origin's envelopes without the one before it sees it from `prev` and asks again (section 13); a hub that lies about `prev` as well simply withholds, and nothing repairs that. |
|                       | Give its own keys in the roster in place of a server's, and sit in the middle | Stopped where that server's `/.well-known/churr` can be fetched, or where the two owners compare fingerprints. If neither happens, nothing stops it. |
|                       | Learn who talks to whom                       | Not stopped at the level of servers: it sees which servers exchange how much and when. It does not see accounts. |
|                       | Close streams or remove servers               | Not stopped. That is the operator's power. Each server keeps its copy. |
|                       | Withhold or alter a channel's history from a joining instance | Not stopped for the channels it owns: a joining instance takes their history from the home hub alone, which vouches for it as an owner does (section 13). That is the trust placed in a hub as a channel's owner. |
|                       | Admit an instance to a channel it owns | The hub's place as the channel's owner (ruling R56). It gives the new instance the key of the new epoch and later ones, and the channel's history, which it keeps. Every member instance sees the server list change in the record. |
|                       | Appoint a manager of its choosing | The operator's last resort (`churr-hub appoint`). The manager must be an account of an instance in the channel, and its actions must be signed by that instance. Every instance sees the manager list change. |
|                       | Leak a key, or give one to an instance outside the channel | Not stopped. The hub owns the channel and holds its keys; its operator is trusted with it. Every member instance sees in the record which instances the channel is in. |
| A forged hub record   | A server, or anyone on the path, sends a `channel` record it made | Stopped. A record is taken only over the link to the hub, only if it names an accepted hub key, and only if that key signed it. |
| A hub of a cluster    | Sign a record for a channel whose home is another hub | Stopped for every server and hub that holds the channel: a record is taken only from the channel's home as the last record named it. Not stopped for a server or hub that has never held the channel: it takes the first record it is shown from any member. |
|                       | Add a hub nobody admitted to the roster, or give a member another key | Stopped. Every entry must be signed by its own key and countersigned by the member it joined through, back to the founder whose key names the cluster; a roster with one bad entry is refused whole. |
|                       | Admit a hub of its own choosing | Not stopped: any member's operator may let a hub in, as any hub's operator may let a server in. Every hub and server sees the roster change. |
|                       | Lie about its own servers' keys | Not stopped beyond what stops a single hub: section 3's fetch, and fingerprints. It cannot speak for another hub's servers: each hub's list is signed by that hub. |
|                       | Take envelopes its servers stamp outside what the checks allow | Not stopped by the hubs further on, which take what an edge hub took so that every server of the channel holds the same events; each of them that would have judged otherwise counts the envelope for its operator. Stopped for the future by every server for itself: an event more than 30 seconds ahead of the reference clock as that server reads it is rejected there (section 9). Not stopped for the past: such a message arrives marked `late` and unread, as one from a server that was disconnected does. |
|                       | Name a stretch of disconnection that never was, to pass an old envelope on | Not stopped beyond its bounds: the stretch must have ended within a day and not in the future, and `at` must be above the origin's last. A hub can also simply drop its trunk. |
|                       | Tell its neighbours a false clock | Not stopped. Every hub beyond it composes a wrong figure for the hubs on its far side, and the servers there stamp by it; the edge and the home then disagree by the lie, and events are refused at one or the other. |
|                       | Withhold a channel, a record or a roster from the hubs beyond it | Not stopped. A tree has one way through. |
| A split               | Use a split to change a channel behind its managers' backs | Stopped. Only the home hub signs a record, and only the side that reaches it can ask. Nothing re-homes a channel without the home hub's own signature. |
| A forged manager or moderator | An instance signs an action for another instance's manager or moderator, or for an account that is not one | Stopped by the hub for actions on the record, and by every instance for `message.removed`, `member.admitted`, `member.request_declined` and `member.removed`: the account must be on the hub-signed list as an account of the instance that signed. |
|                       | A manager's or moderator's home instance acts in their name without their knowledge | Not stopped. An instance speaks for its own accounts. |
| A move (section 21a)  | The owner server moves a channel onto a hub the other server did not agree to | Stopped. The hub checks the other server's signed agreement, which names the hub by name and key; the other server checks the hub's record against the hub it agreed to before it switches. |
|                       | The owner server says a move was committed and it was not, or the reverse | It is the owner server's to decide, and it decides once. A server that told the other "abandoned" and the hub "committed" would leave the other on the direct link while the hub lists it: the hub delivers, the other server takes none of it, and its owner sees the stream is gone from the direct link. Nothing is lost that the owner server could not have withheld anyway. |
| Replay                | Send an old envelope again                    | Stopped. A repeat of an `origin_event_id` is rejected, and an event older than 35 days fails the age check. An envelope cannot be replayed into another stream or epoch because both are signed. |
|                       | Send an old direct message again              | Stopped by `msg_id` and the 5-minute window. |
|                       | Replay a handshake                            | Stopped. Both nonces are inside what is signed. |
| Reordering            | A hub or owner server delivers events in the order it likes | Stopped as far as what is shown: an event's place is in the stamp its origin signed, and every server shows the same order whatever order things arrive in. Stopped for a minute: an event that refers to a message not yet seen is held for it for up to 60 seconds (section 13). A hub that holds the message back for longer makes the event fail on that server, as dropping the message would. One origin's own events cannot be passed around each other without `prev` showing it. |
|                       | A hub or owner server moves an event in time  | Stopped. `at`, `n` and `offset` are under the origin's signature and in the seal. |
| Lying about time      | A server stamps its events in the future, to stay at the bottom of a conversation | Stopped beyond 30 seconds by the time reference, and by every server that receives the event. Within 30 seconds it is not stopped by those checks; the offset check narrows it to the reference's uncertainty about that server, unless the server also lies in its `pong`. |
|                       | A server stamps its events in the past, to slip them in above what others have read | Stopped beyond 30 seconds while it is connected. A server that was disconnected may stamp anything inside that stretch, and a server can disconnect on purpose: this is not stopped. Such a message arrives marked `late` and unread wherever later ones had been delivered. Its `at` must still be above its own last in that stream. |
|                       | A server answers `ping` with false times, so the reference measures a false offset for it | Not stopped. The reference then believes that server's clock to be where the server says. The effect is the same as the two rows above: that server's own events are placed where it wants within what the checks allow. It cannot move anyone else's events. |
|                       | A hub tells servers false offsets             | Not stopped: the hub is the time reference, and time on its streams is what it says. Every server it tells is skewed alike only if it lies to all alike; lying to one shifts that server's messages against the others by the amount of the lie. The server sees the offset it was told in its admin console. |
|                       | A time reference refuses events it should take | Not stopped. Refusing is within a hub's power, as dropping is. |
| An opened envelope    | A server asked to open an envelope under an old key (`open_request`) answers with something else | Not stopped: what the envelope says is vouched for only by the server that opened it, as history is. The origin's signature, the stamp and the authority rules still bind the event to its origin: the opener can make the asker apply a false event of that origin's, at that stamp, and nothing it could not have put in history. |
| Forged authorship     | An envelope claiming another origin           | Stopped by the signature, checked against the pinned key. |
|                       | An account identifier belonging to another server inside a validly signed envelope | Stopped by the authority table. |
|                       | Forged history sent to a joining server       | Not stopped. History is vouched for only by the server that sends it (section 13). |
| Key compromise        | A stream key leaks                            | Everything under that epoch can be read by whoever also has the ciphertext. The owner starts a new epoch; earlier content stays exposed. |
|                       | A server's `box_key` private half leaks       | Every stream key and direct message ever sent to it can be opened by whoever recorded them, a hub for instance. There is no forward secrecy for direct messages. |
|                       | A server's `sign_key` private half leaks      | The holder can speak as that server. There is no key change in version 1: every peer must remove the link and make a new one with new keys. |
|                       | The transport is intercepted (a wrong certificate is accepted, or a reverse proxy is hostile) | Envelopes, direct messages and control frames are signed and, where they carry content, encrypted, so they cannot be read or forged. The interceptor can drop frames and can alter `cursor` and `prev`, which are the deliverer's unsigned words about its own log and decide nothing about what is shown. |
| A removed server      | Keep reading                                  | Stopped for new content: it is sent nothing and has no later key. |
|                       | Read what was sent between its removal and the new epoch | Only if a reference keeps delivering to it. An honest one does not. |
|                       | Keep what it had                              | Nothing stops this.        |
|                       | Come back under the same name with new keys   | Stopped. The name is pinned to its old keys and the owner is warned. |

## 25. Test cases

The suite starts two or three servers and a hub in one process and
drives them over loopback. Each case names what must be proved.

**Identity and connection**

1. `/.well-known/churr` has exactly the fields of section 3, and
   `box_key_sig` verifies.
2. A fingerprint computed from the published `sign_key` equals the
   one in an invitation and the one the admin console shows.
3. A direct link made from an invitation ends with both peers
   `active` and each holding the other's keys.
4. An invitation works once: a second server presenting it gets
   `invite_invalid`.
5. An expired invitation gets `invite_expired`.
6. A dialler meeting a listener whose key does not match the
   invitation's fingerprint stops before sending `invite_proof`.
7. A reconnecting server whose keys differ from the pinned ones gets
   `key_mismatch`.
8. A dialler offering only an unknown version gets
   `version_unsupported`.
9. A handshake signature copied from another connection fails.
10. After the connection is cut the dialler reconnects with growing
    waits, and a second connection replaces the first.
11. A hub invitation made with `--for` is refused for another name.
12. A roster that changes a pinned server's keys is refused and the
    owner is told.

**Sharing and membership**

13. A channel shared over a direct link: a message posted on A
    appears on B with the same message identifier and `order`. A
    message posted on B is stored and shown there at once, `unsent`
    until A has taken it.
14. The same through a hub with three servers: all three hold the
    same messages with the same `order`.
15. A server receives nothing, neither envelopes nor ephemeral
    frames, from a stream it is not in.
16. A declined offer leaves no stream on the invited server and no
    key with it.
17. A server joining with `history_days` of 30 holds messages from
    the last 30 days in the place they have on the owner (the same
    `order`) and nothing older.
18. A joining server cannot decrypt an envelope from before it
    joined, even when handed the ciphertext.
19. A second `stream_open` for an existing `stream_id` is refused.
20. A control frame with a `version` that is not the next one is
    refused with `conflict`.

**Envelopes and authority**

21. An envelope whose signature is wrong is rejected and shows on no
    server.
22. An envelope validly signed by B that carries a message authored
    by an account of A is rejected by every receiver.
23. A reaction, a tick and a form answer naming an account of
    another server are each rejected.
24. `message.removed` from a server that is not the owner is
    rejected; from the owner it removes the message everywhere.
25. `conversation.updated` from a server that is not the owner is
    rejected.
26. `stream.servers_changed` or `key` from a server that is not the
    owner changes nothing.
27. A ciphertext moved into an envelope with a different stream,
    epoch or origin fails to decrypt.
28. An envelope submitted with an `origin` other than the submitting
    server is refused by the time reference.
29. An event of unknown `type` is held and changes nothing, and later
    events still apply.

**Order, unsent messages, duplicates, catch-up**

30. A server is cut off from the others, directly and through a hub.
    Both sides go on writing; each shows its own messages at once, in
    place, the cut-off side's `unsent`. When the link returns every
    server shows the same messages with the same `order`; a message
    that arrives below what a server had already shown is `late`
    there and counts as unread; a reaction made while cut off
    arrives.
31. A message edited while it was unsent arrives edited; one deleted
    while it was unsent never shows on the other servers.
32. The same envelope submitted twice is taken into the log once.
33. An envelope delivered twice is applied once.
34. A server that was offline catches up, by per-origin counters, to
    exactly what the log holds.
35. A server handed an envelope while it lacks the one before it from
    the same origin does not take it, catches up, and ends with both.
36. A server offline longer than the log is told the log's `floor`
    is beyond what it holds, recovers the rest by `history_request`,
    and ends with every message in the place it has on the owner.
37. The time reference refuses, on a direct link and on a hub: an
    `at` five minutes ahead (`clock_ahead`); an `at` five minutes
    behind from a server that was connected throughout
    (`clock_behind`); a stamp whose `offset` is two seconds from the
    one it measured (`clock_offset`); an `n` or `at` not above the
    origin's last (`not_monotonic`). Nothing refused is in the log or
    shown anywhere. A two-minute-old `at` from a server that was
    disconnected for those two minutes is taken.
37a. A server that has never been told its offset by a stream's time
    reference cannot post there (`unavailable`).
37b. A server whose clock is set five minutes forward has its next
    message refused, is told its new offset, stamps the message
    again, and the message is taken and moves to its new place; its
    next stamp is not dragged ahead by the refused one.
37c. A reference that lets through an event stamped five minutes
    ahead does not get it shown: the receiving server rejects it.
37d. A hub hands a server everything it missed in reverse order: the
    server shows the same order as the others. An envelope whose
    `at` or `n` was changed on the way is rejected for its signature.
37e. Of two edits to one message, the one with the later stamp
    stands when the later one arrives first.
37f. One call that makes three events while another server writes,
    and a `submit` cut short after its first envelope: the call
    succeeds at once, and every server ends with the same members.
37g. The clock: offsets wrong by minutes; a symmetric delay that
    varies; a constant asymmetric delay (the offset is wrong by half
    the difference and the uncertainty covers it, and no asymmetry is
    claimed); jitter in one direction only; a sudden change in one
    direction and back; a clock set while connected; synchronised
    mode.

**Keys**

38. Removing a server starts a new epoch; the removed server is
    delivered nothing further and cannot decrypt a later envelope.
39. An envelope under the old epoch that is submitted after the
    change is refused by the time reference (`stale_epoch`), and its
    origin's resubmission under the new epoch, with the same stamp,
    is taken once. An envelope under an epoch the owner has not
    started is refused (`bad_frame`) and taken once it has.
40. A server that missed its `key` message gets it by `key_request`;
    a server that was never in the stream gets an error.
41. A direct message opened by a server other than `to` fails, and
    one whose `from` was changed fails.
42. A direct message replayed after 10 minutes, or with a clock 6
    minutes out, is dropped.

**People, files, forms, presence, calls**

43. A remote member appears as `name@server` with the same account
    identifier as at home, and a profile change reaches the other
    server once however many streams they share.
44. A file posted on A arrives on B in chunks, matches its hash, and
    is served by B with nothing fetched from A when a member of B
    opens it.
45. A file with one byte changed in transit is discarded.
46. A file over `max_blob_bytes` is refused at upload on the sending
    server and never fetched by a receiver.
47. A custom emoji used on A is drawn on B and stored with A as its
    source.
48. A poll posted on A and answered on B: the tally on both servers
    matches, B's database holds no other server's answers in
    readable form, and an anonymous poll never yields names.
49. An answer that reaches the form's home server after `form.closed` is not counted.
50. Presence for an account reaches only servers that share a
    channel with it, and nothing is sent when sending is off.
    After a reconnection a server's snapshots arrive spread over
    some seconds, one for each stream, and all of them arrive. A hub
    whose server has fallen behind drops ephemeral frames for it and
    delivers it every envelope, in order, when it reads again.
51. A typing or presence frame naming another server's account is
    dropped.
51a. A block a person makes of someone of another server reaches that
    server alone. There the blocked person is shown the blocker
    online with nothing more, by every path presence takes, and none
    of the blocker's typing, while everyone else sees the truth.
    Lifting the block restores the truth at once; a reconnection, a
    restart of the receiver and a server joining a stream each leave
    the receiver holding what the sender has. A `deaf_ears` naming an
    account of a third server, or one of the sender's that is older
    than what is held, changes nothing.
52. A member of B joins a call hosted on A with a pass; an expired
    pass, a reused pass and a pass for another account are refused.

**Ad-hoc, sealed, leaving, the hub**

53. A member of A can start an ad-hoc channel with a member of B
    they share a channel with, and cannot with one they do not.
54. B declines an ad-hoc offer naming an adder who shares no channel
    with B's member, even though A sent it.
55. When the last shared channel between two people ends, their
    ad-hoc channel and sealed chat become read-only on both servers
    and writable again when a shared channel returns.
56. A sealed chat between A and B: neither server's database nor the
    hub's log holds readable text, and search and export on both
    return nothing from it.
57. A bot cannot be put in a cross-server sealed chat, and a second
    sealed chat for the same pair is refused.
57a. A cross-server sealed chat starts with epoch record 1 held by
    both servers. Of two `sealed.epoch_started` for one `key_epoch`,
    the one a server holds first stands there and it will not make
    or take a second, and a sealed message under a `key_epoch` that
    is not the chat's current one is rejected. (Two made at the same
    moment on two servers: section 11, not settled.)
57b. A device list change on A reaches B as a new version, and a
    `device_list_request` is refused once the two people share no
    channel.
58. After a server leaves, its copy is an archived local channel,
    the others show its accounts as having left, and its messages
    remain.
59. After `stream_close`, every server detaches and keeps its copy.
60. `message.purged` from the author's home server erases the
    message's content from the database, search index and file store
    of every other server.
61. `messages.expired` with `scope` `all` from a server that is not
    the owner is rejected.
62. Thread-to-channel, making the channel private and locking are
    each refused in a shared channel, on the owner server and on the
    others.
62a. A `collect`, `thread`, `channel`, `group` or `bot` form is
    refused at posting in a shared channel, and a `message.created`
    carrying one from another server is rejected.
62b. `@name@server.name` and `:name@server.name:` typed on B resolve
    to A's account and emoji in a channel shared with A, and stay
    plain text in a channel that is not.
63. The hub's own storage of the link (its log, records and keys),
    searched byte for byte, contains no message text or username,
    and no key in the clear; its holder's database holds every
    message of a channel it owns, and who said it.
63a. An instance of a hub-owned channel drops off for good (switched
    off and removed from the hub), every other instance goes offline,
    and an instance that joins afterwards is given by the hub alone the
    whole history, the names and pictures of the departed instance's
    people, their custom emoji and their files, and rejects nothing.
64. A server over the hub's submit limit gets `rate_limited` with
    `retry_after_seconds`, and an oversized envelope gets
    `too_large`.
65. `churr-hub remove` disconnects the server, a new roster reaches
    the others, and the owners of its streams remove it.
66. The hub's log holds nothing older than 30 days after the janitor
    runs.
67. The status page names no server and no stream.

**Channels a hub owns (section 23a)**

68. A server that is not on a channel's list is delivered no
    envelope and no ephemeral frame of it, is sent no record, is
    refused the log (`not_in_stream`), and is refused when it asks
    to join an unlisted channel (`forbidden`).
69. A `channel_action` that an instance signs in the name of another
    instance's manager is refused by the hub, as is one for the same
    account claimed as its own, one with a bad signature, and one by
    an account that is not on the list; the record does not change.
70. `message.removed` signed by an instance for an account that is not
    on the manager or moderator list as its own is rejected by every
    other instance, and the message stays.
71. A `channel` record changed after the hub signed it, and one
    signed by a key that is not an accepted hub key, are refused
    with `bad_signature` and change nothing.
72. An `open` channel is joined without approval; a `request`
    channel only once a manager approves, and a declined server is
    told and gets nothing; an `unlisted` channel is not in the
    directory and is joined only by invitation.
73. The directory shows name, description, mode, managers and the
    number of servers of listed channels, and each asking server's
    own place.
74. The hub makes every key of a channel it owns: every server holds
    the hub's key of each epoch it was in and no other, none from
    before it joined or after it was removed; and every key, in every
    encoding the protocol uses, and every message text, searched for
    in everything the hub stores and everything it is sent, is not
    there in the clear.
75. A key a member server sends for a hub-owned channel is taken by
    nobody. The hub answers `key_request` for an epoch the asker was
    in, and refuses one from before it joined, or for a channel it was
    never in.
75a. An instance leaves a hub-owned channel for good; an instance that
    joins afterwards is given everything its people wrote, with their
    profiles, as history from an instance that remains.
75b. A channel handed to another hub of a cluster goes on under a key
    epoch the new home began with its own key; the new home holds and
    gives out the keys of the old home's epochs too, which it was
    handed sealed, and the old home holds no key of it any more.
76. A moderator deletes another instance's message and it is deleted
    on every instance; an Owner or Admin on neither list deletes no
    one else's message in the copy.
77. A manager's change of name, description, listing mode, managers
    and moderators reaches every instance as a record; the operator's
    `appoint`, `dismiss` and `expel` do the same. Removing the last
    manager is refused (`conflict`).
78. The status page gives the number of channels by listing mode and
    no channel's name, manager, moderator or server; an ad-hoc channel
    across the hub has no record and takes no manager's action.
78a. A server that is not connected when it is admitted finds, when
    it returns, the record; asks for and gets the key of the epoch
    it joined at and its history; holds what was said before it
    asked and while it was away; and is then in the channel.
78b. A channel shared from one instance comes to the hub with every
    manager of it there on the manager list, and its members, levels,
    first joins, pins and history are as they were.
78c. On each instance the copy's managers are its accounts on the
    hub's manager list; setting someone else to manager is refused
    (`conflict`), and a manager on the list is not lowered locally.
78d. A private channel shared through a hub is `private` in the
    record, never in the directory, and `set_listing` to `request` or
    `open` is refused (`forbidden`); a record that makes it not
    private is refused by every instance (`forbidden`) and changes
    nothing.
78e. In a private channel an instance that joins has nobody in its
    copy; nobody there adds anyone, the Owner included (`forbidden`);
    a request made on one instance is seen on every instance, and an
    admission by a moderator of another instance adds the person on
    their home instance, announced once with who admitted them.
78f. `member.removed` by a moderator takes the person out on every
    instance; the same event by an account on neither list is
    rejected (`not_moderator`).
78b. An `offer` of kind `channel` sent by a server over a hub is
    declined with `not_allowed` and leaves no stream behind.

**Ties (sections 11 and 18)**

79. Two epoch records made for one key epoch of a sealed chat, on
    two servers that cannot hear each other: when they can, both
    hold the one with the earlier stamp, the server that held the
    other replaced it, and its device goes on from the one that
    stands.
80. Two calls started in one conversation on two servers that cannot
    hear each other: when they can, every server shows the call with
    the earlier stamp, and the other has ended.

**Moving a channel onto a hub (section 21a)**

81. With messages posted on both servers throughout: after the move
    both servers and the hub hold one stream, the hub owns it, every
    message is on both servers once under one order string, each
    author's messages are in the order they were said, the
    managers are the managers of both servers, a new key epoch has
    begun, and the direct link's other stream is untouched.
82. A third server joins afterwards through the hub and holds what
    was said before and after the move, in the same order.
83. Each of these ends with the same outcome on both servers and the
    hub, and no message lost, duplicated or out of order: the
    proposal lost and sent again; the acceptance lost and sent
    again; the hub refusing to prepare; the hub out of reach when
    the owner server is to prepare; the hub not handed the commit
    for a while by either server; the owner server restarted
    before preparing, after preparing, and right after the commit;
    the owner server withdrawing after the hub prepared; the owner
    server never handing the hub the commit; the other server cut
    off on both links through the commit; the other server failing
    at the switch; the other server restarted before the commit and
    after it.
84. A proposal that expires, before or after it was accepted,
    leaves the stream on the direct link, on both servers, with
    nothing on the hub.
85. While a move is in progress the things section 21a lists are
    refused with `conflict`, and messages are not.
85a. An event the owner server took before the commit, whose
    acknowledgement the other server never got, is not sent to the
    hub after the move: it stands under the stamp it had, on both
    servers, and the hub's log does not hold it.
85b. The hub refuses to prepare a moved channel without the other
    server's agreement; with one the owner server signed itself;
    with a real one used for another move or another stream; and
    with managers for the other server that it did not name, or
    without those it did.
85c. `stream.moved` for a move the other server has not heard of, has
    not agreed to, or with a record the agreed hub did not sign
    (whatever key it names), is rejected, and the stream stays on
    the direct link.

**Trunks between hubs** (section 23b; R58)

85d. Three hubs joined in a line and in a star list the same three
    members, and each holds its clock against the other two.
85e. A roster with an entry its `via` did not countersign, or one
    that gives a member another key, is refused whole by hubs and by
    servers; a record signed with the forged key is refused.
85f. A hub that is in a cluster cannot join it again: its own command
    refuses, and the member it dials refuses the handshake with
    `conflict`.
85g. A channel with servers on three hubs, in a line and in a star:
    every server shows the same messages in the same order.
85h. A hub with no server of a channel behind it is sent no envelope
    of it, stores none, and for an unlisted channel is not sent the
    record.
85i. A split with messages on both sides: each side delivers among
    its own; `channel_action`, `join` and `leave` on the far side get
    `unavailable` and succeed on the home side; after the rejoin every
    server holds everything once, in one order, with the other side's
    messages marked `late`.
85j. After a rejoin a hub is sent exactly what the other side took
    during the split, and no log holds an event twice.
85k. A hub that does not check times takes an envelope stamped before
    the split began and one stamped ahead: the next hub refuses both,
    the home hub never holds them, both hubs count the refusals and
    each origin is told. An honest envelope of the split, older than
    30 seconds when it arrives, is taken.
85l. With skew on every clock and delay on the trunks, a server two
    trunks from the home hub is told its offset from that hub within
    the composed uncertainty; an event stamped with it is taken, one
    stamped with its offset from its own hub gets `clock_offset` with
    the composed figure.
85m. A burst over a trunk travels in batches, never more than 8
    unanswered, and arrives once and in order.
85n. With the home hub stopped, servers on the other hubs go on
    exchanging messages, changes get `unavailable`, and no hub claims
    the channel; restarted, the home hub is sent what it missed.
85o. `rehome` is refused while the other hub cannot be reached; when
    both are up the channel's records are signed by the new home and
    every server takes them.
85p. No hub of a cluster holds a stream key or a message's text, in
    its files or in any frame it was sent.
85q. A hub restarted between preparing a moved channel and its commit
    still holds the prepared channel, and the move completes.
85r. An envelope its edge hub took and a hub further on would not have
    is taken by every hub and reaches every server; it is counted as
    suspect, and nothing is refused.
85s. A reaction, a reply and a reaction to the reply reach a server
    before the message: all are held, and applied when it arrives. An
    event whose message never comes is rejected after the wait. A
    server that restarts while it holds one ends as the others did.
85t. A server admitted on the home side during a split ends with what
    the far side said during it, opened for it by a server that holds
    the old key; and so does a server that joins in the moment an
    envelope is taken under the old key elsewhere.
85u. A server restarted while its hub is down posts in a channel homed
    on another hub, stamped by the figure it had written down.
85v. A hub that leaves its cluster tells its servers, connected or
    not, that the channels homed elsewhere have stopped; run while it
    is not serving, or cut off, the hub it left through hears when it
    next can.
85w. A home's move whose offer is lost leaves the old home the home; one
    whose answer is lost leaves the new home the home at once and the
    old one silent until it hears. No moment has two homes or none.
85x. A hub that has lost a prepared channel prepares it again from its
    own record and the commit.
85y. An ad-hoc channel, a sealed chat and a move from a direct link
    work between servers on different hubs of a cluster.
85z. Each command of section 23b is run through the built program.

**The version**

86. A dialler that offers only version 2 is refused with
    `version_unsupported` before the listener says anything else, and
    a dialler stops at a listener of another version having sent
    nothing but its `hello`.

**The encoding**

87. The codec's table of the schema says what `link.proto` says, as
    `protoc` read it; a frame of every kind, with every field it
    carries, survives the wire exactly, is read by the standard
    protobuf library with no byte left over, and is the bytes that
    library writes deterministically.
88. A frame cannot be written with a field its kind does not carry
    or an identifier that is not a UUID; a reader skips fields it
    does not know, and refuses an identifier of another length and a
    time finer than a millisecond.
89. A signed block read and written again by three carriers, once
    inside another frame, arrives as the bytes its signer wrote and
    verifies; with any one bit of the bytes or the signature changed
    it does not; and the same content written as other bytes (its
    fields in another order) reads as the same record and does not
    verify.
90. With every single bit of a `submit` frame flipped in turn, the
    envelope that is read either is the one that was signed or does
    not verify; and an envelope kept as a hub keeps it and sent as a
    hub sends it verifies and opens.
91. A direct message and an event of each shape come back from the
    codec as the object that went in, with no JSON and no base64 in
    between; one the schema cannot hold is carried whole; and across
    the whole suite nothing but the event type that is unknown on
    purpose (case 29) travels that way.
92. A dialler that does not offer `permessage-deflate` is refused the
    upgrade, a listener that does not agree to it is left at once,
    and of three frames sent through a relay that records them, the
    one deflate makes smaller is deflated and the two it does not
    are sent as they are.

## 26. Choices made in this document

Where DESIGN.md left a gap, the simplest thing consistent with the
rest was chosen.

1. Frames are Protocol Buffers only, deflated (ruling R70). The
   schema comes from the same generator as the client's; the codec
   is the link's own and walks a table of the schema, so there is no
   generated message code and no second description of a frame to
   keep in step. The well-known document and the invitation stay
   JSON: they are read before there is a connection, by people too.
2. Signatures are over fixed byte layouts or over bytes as received,
   never over a message written out again (section 2). Envelopes and
   direct messages kept their layouts when the frames became
   protobuf, so no signature of theirs depends on the encoding.
3. The envelope's nonce is random and travels at the front of
   `ciphertext`.
4. Everything between two servers that is not an envelope is a
   sealed, signed direct message, on direct links too. A hub cannot
   tell an offer from a file chunk.
5. A conversation is in one stream over one peer. More than two
   servers means a hub.
6. Nobody numbers a stream's events. Each event is stamped by its
   origin with the stream's reference time, and a conversation is
   shown by the stamps (ruling R57). Each server's `seq` is its own
   arrival number. What a channel held before it was shared keeps
   its order, before everything stamped.
7. A new key epoch starts on every change to the server list,
   joining included.
8. The owner announces a server-list change twice: a control frame
   the reference can read, and a `stream.servers_changed` event in
   the log.
9. History on joining is a snapshot vouched for by the owner.
10. Files are fetched from the event's origin on arrival of the
    event, not pushed through the log.
11. Only `tally` forms exist in shared conversations. An answer
    travels as an envelope whose content is sealed to the form's home
    server.
12. Every change a member makes in a shared conversation is made at
    once and queued while the reference is unreachable; nothing is
    refused for want of a connection.
13. Levels, pins, slow mode, the word filter, archiving and time-outs
    are each server's own.
14. Presence and typing share one off switch.
15. Call signalling for a remote member is relayed between the two
    servers; only media goes to the host directly.
16. A cross-server ad-hoc channel cannot be named, and nothing that
    is shared can be locked or have its privacy changed.
17. The read-only rule is judged per member by that member's home
    server.
18. A server that leaves keeps its copy as an archived local
    channel.
19. A hub closes streams whose owner it removed.
20. No key change for a server's identity in version 1.
21. Each origin counts its events (`n`). The counter is the tie-break
    of the order, what catching up is done by, and, with `prev`, how
    a server sees that it was not delivered something.
22. The numeric limits and waits given throughout.
23. The clock is measured by the reference, in the link's own
    heartbeat, and the server is told; a server never measures its
    reference. The stamp carries the offset used, so the reference
    can check the server did as it was told.
24. An event's stamp is in the clear, in the envelope, under the
    origin's signature: a hub must be able to check a time it cannot
    be allowed to change. A hub therefore sees when each event was
    made, to the millisecond, which it saw anyway to within its own
    latency.
25. The status page shows totals of the clock figures and names no
    server; each server's figures are an operator command's.
26. A refused stamp voids every stamp its server gave after it in
    that stream, and they are all given again in order.
27. A hub learns the current key epoch of a stream a server owns from
    the envelopes of the stream's owner, and from nobody else's; that
    of a channel it is the home of it begins itself, and an edge hub
    learns it from the home hub's record.
28. The envelope and frame layouts changed with ruling R57 and again
    with R55 and R56, and the protocol version is 2. A side speaks
    one version; a mismatch is refused at the handshake.
29. A hub-owned channel's record is one signed block, whole, at a
    version. Servers take the server list, managers, moderators, listing,
    name, description, key epoch and days of history from it and from
    nothing else.
30. On a server, the hub has a row in `servers` and is the owner of
    the streams it owns, so every owner-server rule refuses every
    server without a second set of rules.
31. Deleting a message for everyone in a hub-owned channel is an
    event in the stream that every server checks against the
    hub-signed manager and moderator lists; the hub cannot check what it cannot
    read.
32. The home hub makes every key of a channel it owns and begins each
    epoch itself, in the transaction that signs the record calling for
    it (ruling R109). No instance makes a key of such a channel, and
    nothing waits on one that is away.
33. Channel name and description live in the hub's record; each
    server's name for its copy stays local.
34. A server that asks to join a `request` channel is sent nothing
    until it is admitted or declined.
35. The managers that go to the hub's manager list when a channel
    is shared or moved are the members with level manager and the
    Owner and Admins who are members, on each instance.
36. A move's commit point is one transaction on the owner server,
    and the other server switches only on `stream.moved` in the
    direct log, never on the hub's word alone.
37. The other server's managers are fixed at its acceptance and held
    until the move is over; the owner server's are read in the
    commit.
38. A move is refused at the commit if the owner server's clock is
    more than 15 seconds ahead of the hub's.
38a. A reference that has taken an event takes it as taken whenever
    it comes again, under whatever key epoch.
38b. After an accepted frame the next one leaves at once.
39. A hub never drops a prepared channel by itself.
40. A cluster's shape is fixed by the order hubs joined in: a hub is
    joined through one member, the roster records which, and the
    founder cannot leave while others remain.
41. The cluster roster carries no outer signature: every entry
    carries its own. Servers keep it in memory.
42. Every hub that carries a channel keeps its own log of it, the
    hubs between two others included. A trunk ships the log; there is
    no other queue.
43. The edge hub takes an envelope on its own word, and that word
    stands across the cluster: no hub further on refuses it for its
    time. The other way to make every server hold the same events
    (carry a later hub's refusal back, have the edge side withdraw
    the event and its origin stamp it again) needs every server that
    has applied an event to undo it, whatever kind of event it is,
    and can go round for ever between two hubs whose clocks disagree.
    What it costs: a hub that does not check its servers' times is no
    longer stopped by the next hub, only counted there.
44. A hub passes on what it has taken within 60 seconds or closes the
    trunk; that figure is what each hub on the way adds to how old an
    `at` may be.
45. A batch is at most 256 envelopes and 512 KiB; at most 8 are
    unanswered on a trunk.
46. A server writes down its clock against the cluster's other hubs,
    in its settings, one value per hub peer.
47. A home moves only between hubs whose clocks are within 15 seconds.
48. A stream a server owns is homed on its owner's hub, and its
    home does not move, whoever owns it later.
49. An event waits for the message it refers to for 60 seconds, 256 to
    a stream, in memory; nothing waits on a direct link or on a hub.
50. A server that cannot read an envelope it was owed has it opened by
    a server that can, and is given no earlier key.
51. A home is offered, not handed over: the new home's transaction is
    the move, and the old home signs nothing until it has the other
    hub's signed answer.
52. A hub that leaves a cluster still owes the hub it left through the
    roster that says so, and goes on dialling until it has sent it.
53. A hub's word that a stream homed elsewhere has stopped is enough
    for its own servers.
54. A hub writes the envelopes submitted to it in groups, one
    transaction for all that arrived while the last was being
    written; each is acknowledged, delivered and shipped only after
    the transaction that holds it has committed. What it sends a
    server is queued for that server's connection, at most 64 MiB,
    beyond which the connection is closed and the server catches up.
55. A channel a hub owns has two lists: managers, who hold it as an
    owner would, and moderators, who keep the peace. A copy's managers
    are its instance's accounts on the manager list, and nobody else.
56. A private channel on a hub is private and unlisted for good
    (ruling R94). Its people are admitted by its moderators and
    managers, from any instance, as events in the channel that every
    instance checks; the hub keeps nothing new for it.

## 27. To verify

- That the XChaCha20-Poly1305 and HKDF packages in
  `golang.org/x/crypto` are acceptable under the no-cgo rule for all
  three programs (they are pure Go).
- The sealing construction of section 7 and the epoch rules of
  section 10 should be read by someone outside the project before
  anyone relies on them.
- That the fields of `sealed.epoch_started` and the offer's
  `epoch_record` match the epoch record as the rewritten `SEALED.md`
  defines it.

Settled since the last version of this document: two
`sealed.epoch_started` for one key epoch (section 11) and two
`call.started` in one conversation (section 18) are both decided by
the earlier stamp, on every server alike. To verify there: that the
sealed-chat clients re-send, under the record that stands, what a
device sent under a record that was replaced (`SEALED.md` is the
authority, and says so for a race lost on one server).

Still open:

- A server taken out of a channel on the home side while its own hub
  has not yet heard: an envelope it submits in that moment is taken
  by its hub, applied by the servers that have not yet applied the
  change of list, and rejected (`origin_not_in_stream`) by those that
  have. On one hub the hub refuses it. Nothing makes the two sides
  agree afterwards.
- What a server is given by `open_request` is the event, not proof of
  it; and when nobody answers, the fallback to history recovers
  messages and their reactions only.
- A server whose hub has left the cluster stays in the list of a
  stream another server owns until that owner removes it. Nothing is
  delivered to it.
- On the server that has agreed to a move, a change of an account's
  server role (an admin made or unmade) between its agreement and the
  commit is not reflected in the managers it signed for. Levels in
  the channel are held; roles are not the link's to hold.
- The history of a hub-owned channel comes from its member
  instances: a joining instance is given it only while another
  instance of the channel is connected. If every other instance is
  away, it gets its history when one returns and answers its request.
  Files are fetched from their author's home instance (section 15), so
  a file whose home instance has left the channel can no longer be
  fetched by an instance that does not hold it already.

Trunks between hubs (R58) are section 23b. Still to verify there:
that 60 seconds is a sound bound on how long a hub may hold what it
has taken, on real trunks under real load.

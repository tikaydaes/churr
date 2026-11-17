# Calls

Voice, video and screen sharing in a conversation, carried by the
Churr server itself. `DESIGN.md` section 13 is the outline this
document fills in. Nothing here is built yet, and every number about
capacity is a goal to test, not a result.

## 1. Shape

- A call belongs to a conversation. A conversation has at most one
  call at a time. There are no voice channels and no calls outside a
  conversation.
- Each person in a call sends their microphone, camera and screen to
  the server, and the server forwards them to the others. This is a
  selective forwarding unit (SFU). The server never decodes, mixes or
  stores media.
- Apps and browsers never connect to each other, so no member learns
  another member's network address.
- Signalling, the messages that set a call up, runs over the WebSocket
  each client already has open.

Words used below:

| Word | Meaning |
|------|---------|
| Participant | An account that has joined the call. One device per account at a time |
| Track | One stream of media from one participant: `microphone`, `camera` or `screen` |
| Publish | Send a track to the server |
| Subscribe | Ask the server to forward someone's video track |
| Layer | One of up to three sizes a camera is sent in at once |

## 2. Life of a call

### Start

A member starts a call from the conversation header. The client calls
the call join operation (`DESIGN.md` section 7, "Calls"). If the
conversation has no call in progress, joining starts one:

1. `perm` checks the account may join (section 3).
2. A row is added to `calls` with who started it, `started_at` and no
   `ended_at`. The unique index on live calls makes two people starting
   at the same moment end up in one call: the second insert fails and
   that person joins the first call.
3. The server posts a system message in the conversation (below) and
   stores its identifier in `calls.message_id`.
4. If the conversation is one where calls ring, the ring starts
   (section 15).
5. Every member of the conversation receives `call.started`.

### Join

Joining takes two steps: one HTTP request, then messages on the
WebSocket.

1. The call join operation checks permission and answers with the call
   as it stands and a **ticket**: a random 256-bit value, good for 60
   seconds and for one use, tied to the account, the session and the
   call.
2. The client sends `call.join` with the ticket on its WebSocket. From
   then on, that connection is the participant's signalling connection.
3. The server adds a row to `call_participants` and tells the
   conversation's members `call.participant_joined`.
4. Media is set up (sections 4 and 10).

The ticket step exists so that the permission check, the creation of
the call and, for a call hosted by another server, the request for a
pass (section 16) all happen in an ordinary API request with ordinary
errors.

An account is in a call on one device. Sending `call.join` from a
second device moves the call there: the first device receives
`call.participant_left` for itself with reason `joined_elsewhere` and
drops its media.

**Which device.** The server names the device each participant is in
the call on, as `device_id`: in every participant of the call object,
in `call.participant_joined`, in `call.track_published`, and beside
each track of a `call.offer` on `recv`. It is the device of the session
that sent `call.join`. It is left out when that session has no device
row, and for a participant whose signalling another server relays
(section 16); the call events a host sends to the other servers of a
shared conversation do not carry it either. When a call moves to an account's other device nobody is
told with an event of its own: the tracks the new device publishes
carry its `device_id`, and the call object does from then on. A
sealed chat's call uses it to choose keys (section 14).

### Leave

The client sends `call.leave` and closes its media. The server sets
`left_at` on the participant's row and tells the conversation
`call.participant_left`. Someone who rejoins gets a new row.

A participant also leaves when:

- their signalling and their media have both been gone for 30 seconds
  (section 11), reason `timeout`;
- they are removed from the conversation, lose level `member`, or are
  timed out there, reason `removed`;
- their session ends, reason `signed_out`.

### End

A call ends when any of these happens:

| Cause | `reason` |
|-------|----------|
| The last participant leaves | `empty` |
| The last participant dropped without leaving and did not return in 30 seconds | `empty` |
| Nobody entered the call within 90 seconds of its start | `empty` |
| A local call lost to a call started at the same moment on another server (section 16) | `server` |
| One participant has been alone in the call for 60 minutes | `alone` |
| The call has lasted 24 hours | `too_long` |
| The conversation is archived or deleted | `closed` |
| The server is restarting, or calls were switched off | `server` |

The server sets `calls.ended_at`, updates the system message and tells
the conversation `call.ended`. Media state is held in memory only. A
server that starts and finds calls with no `ended_at` ends them, with
`ended_at` set to the start-up time.

### The system message

One message per call. `MESSAGE-FORMAT.md` section 1 defines system
messages; a call uses two of its kinds on the same message.

While the call is live:

    "system": {"kind": "call_started", "call_id": "0193b100-…"}

After it ends the server replaces that with:

    "system": {"kind": "call_ended", "call_id": "0193b100-…",
               "duration_seconds": 1284,
               "participant_ids": ["0193a000-…", "0193a001-…"]}

- `author_id` is the account that started the call.
- The change is sent as an ordinary message update. The message is not
  marked as edited and keeps no history.
- Clients word it themselves: "Sam started a call" with a Join button
  while live, then "Call, 21 minutes, 4 people". A person who was rung
  and is not in `participant_ids` sees "Missed call from Sam".
- `participant_ids` holds at most fifty.

When retention removes the system message, the janitor removes the
`calls` row and its participants with it.

## 3. Who may join

`perm` answers. An account may start or join a call when all of these
are true:

- it is a member of the conversation at level `member` or above. A
  reader may see that a call is on and who is in it, and cannot join.
  Someone reading a public channel without having joined it must join
  the channel first;
- the conversation is not archived;
- the account is not timed out in that conversation or on the server;
- calls are switched on for the server (`calls.enabled`);
- in a sealed chat, its device can do frame encryption (section 14);
- the call is not full (section 12).

Guests, and people from other servers in a shared channel, join on the
same rule. A bot at level `member` may join like any other client if it
brings its own WebRTC stack; nothing in signalling is for people only.
Bots cannot be in sealed chats, so never in their calls.

Nobody can remove another person from a call or mute them. A manager
who removes someone from the channel removes them from its call as a
consequence. A participant can always mute someone for themselves, in
their own client.

## 4. The media server

The `calls` package is built on Pion WebRTC, which is pure Go, so the
no-cgo rule holds.

- **A room per live call**, in memory: its participants, their
  published tracks, and who is forwarded what.
- **Two connections per participant.** Each client opens two WebRTC
  peer connections to the server:

  | Connection | Carries | Who makes offers |
  |------------|---------|------------------|
  | `send` | The participant's own tracks, to the server | The client |
  | `recv` | Everyone else's tracks, from the server | The server |

  Splitting them means each side changes only the connection it makes
  offers on, so two offers can never cross. The cost is a second
  handshake, on the same port. The server sends a participant at most
  one `recv` offer every 150 milliseconds, folding the changes in
  between into the next, so that many people joining at once does not
  push the client past its signalling limit.
- **Forwarding.** For each published track the server reads RTP
  packets and writes them to the `recv` connection of each participant
  who should have that track, rewriting sequence numbers, timestamps
  and the VP8 picture numbers so each subscriber sees one continuous
  stream when the layer changes.
- **Feedback.** The server answers lost-packet requests (NACK) for
  video itself, so a subscriber's loss does not reach the publisher.
  Each layer of a published video track keeps its newest packets once,
  about two seconds of them (64 for `q`, 128 for `h`, 256 for `f`, 512
  for a screen); for each subscriber the server keeps only a note of how
  the last 512 packets it sent were numbered. A packet is not sent
  again twice within a round trip to the subscriber and 5 milliseconds,
  and what is sent again on a track is bounded by the layer's own bit
  rate, after a burst of up to a second of it. Audio is not kept. It
  turns a subscriber's request for a key
  frame into one request to the publisher, at most once a second per
  layer.
- **Congestion.** Each `recv` connection keeps a limit on the
  subscriber's downlink, set from loss alone: the share of packets
  the subscriber reports lost. When more than a tenth are lost the
  limit is cut to 85% of what was being sent (at most once every 1.5
  seconds, never below 300 kbit/s); under 2% it grows again. The
  server keeps what it forwards inside that limit (section 7).
- **Encryption.** Each connection is DTLS-SRTP between that client and
  the server. Outside sealed chats the server holds the media keys and
  could read the media; it does not. Inside sealed chats it cannot
  (section 14).

## 5. Network

### One port

The server listens for media on one port number, on UDP and on TCP.
The default is **24877**, set with `-media` (`OPERATIONS.md`). Every
call and every participant shares it: packets are told apart by the
ICE user names inside them, not by port.

- **UDP** is used whenever it works.
- **ICE over TCP** on the same number is the fallback for networks
  that block UDP. The client connects out to the server; the server
  never connects to a client. Media over TCP stalls under loss, so it
  is a fallback and the client shows a "poor connection" mark while on
  it.
- The media port does not go through a reverse proxy. Behind a proxy
  it is published directly (`OPERATIONS.md` section 7).

### The public address

Clients must be told where to send media. The server puts its media
addresses in every offer and answer as host candidates.

- `-media-address` gives them: one or more IP addresses or a host
  name, comma-separated. Use it behind NAT, behind a proxy on another
  machine, or in a container.
- When it is not set, the server uses the addresses its own host name
  (`-hostname`) resolves to, looked up when a call starts and kept for
  ten minutes.
- If the server has an IPv6 address, it is offered too.
- A second address can be listed for people on the same local network
  as the server, where the router does not loop traffic back.
- When every address is a private one, calls can only connect on the
  server's own network. The server logs a warning once and the admin
  Overview shows it (`OPERATIONS.md` section 7, "Behind NAT").

### ICE-lite

The server runs **ICE-lite**, not full ICE.

- A Churr server is by design reachable at a fixed public address on a
  known port. Full ICE exists to discover a path when neither side
  knows that. Here the client always knows where the server is.
- A lite agent only answers connectivity checks. It never sends checks
  to addresses a client names. A full agent would send packets to any
  address a client put in a candidate, which lets a member aim the
  server at a third party. This is the same rule as "the server never
  calls an address a member supplied".
- It gathers nothing, so there is no STUN server to configure and no
  start-up delay.
- Clients are full ICE agents as usual and do all the checking.

What it costs: a server whose media port is not reachable from the
internet cannot host calls at all. There is no fallback for that, and
the admin console's call test says so (section 12).

### No TURN

TURN relays media between two peers that cannot reach each other. In
Churr every peer talks only to the server, which is already a public
relay, so a TURN server would add nothing. A network that blocks both
UDP and outbound TCP to the media port cannot be used for calls. The
client says "This network blocks calls" when ICE fails on both, and
the person can still read and write.

## 6. Codecs

| Kind | Codec | Notes |
|------|-------|-------|
| Audio | Opus | 48 kHz, mono, about 32 kbit/s, in-band error correction on, silence suppression (DTX) on |
| Video | VP8 | Preferred |
| Video | H.264 | Constrained baseline, profile `42e01f` only, packetisation mode 1. Fallback |

**VP8 is preferred.** The server lists it first in every offer and
answer, and clients publish VP8 unless they cannot.

- Every WebRTC implementation Churr targets (the library the apps use,
  Firefox, Chrome and Safari) can encode and decode VP8 in software,
  with simulcast, on every device.
- H.264 depends on the device. Some Android phones have no usable
  H.264 encoder, hardware encoders differ in which profiles and how
  many simultaneous sizes they manage, and simulcast with them is
  uneven. A forwarding server cannot convert between codecs, so the
  codec a publisher picks must be one every subscriber can decode. VP8
  is the one that is always there.
- VP8 has no patent pool to think about.

H.264 stays in the list so that a client which can only encode H.264
can still publish. A subscriber that cannot decode a track's codec is
told `unavailable` for that track (section 10) and shows the person's
avatar.

Left out of version 1: VP9, AV1, stereo or music-quality audio, and
audio from a shared screen.

## 7. Simulcast and what gets forwarded

### Layers

A camera is published in up to three sizes at once, each a separate
encoding with its own identifier (`rid`):

| Layer | Size | Frame rate | Bit rate, about |
|-------|------|------------|-----------------|
| `q` | 320 × 180 | 15 | 150 kbit/s |
| `h` | 640 × 360 | 30 | 500 kbit/s |
| `f` | 1280 × 720 | 30 | 1,200 kbit/s |

- Desktop apps and desktop browsers publish all three when the camera
  and uplink allow. Phones publish `q` and `h`.
- A client that cannot do simulcast publishes one layer and says so in
  `call.join`. The server forwards that one layer to everyone who asks
  for the track, whatever size they wanted.
- A screen is published as a single layer (section 9).

### What a subscriber asks for

A client tells the server which video tracks it is showing and how big
each tile is, in physical pixels, with `call.subscribe`. The list
replaces the one before. A track not in the list is not forwarded.
Audio is not in the list: every unmuted microphone is forwarded to
everyone (section 8).

Clients send a new list when the layout changes: someone joins or
leaves, a tile is pinned, the window is resized, the app goes to the
background (an empty list). They wait 200 milliseconds after the last
change so a window being dragged does not flood the server.

### How the server picks a layer

For each track in a subscriber's list:

1. **By size.** Take the smallest layer whose height is at least 80%
   of the tile's height. If none is that tall, take the tallest there
   is. A 150-pixel tile gets `q`; a 400-pixel tile gets `h`; a
   full-window tile gets `f`.
2. **By what exists.** If the publisher is not sending that layer
   (a phone has no `f`, or the publisher's uplink dropped it), take the
   nearest one below, then the nearest above.
3. **By bandwidth.** Add up the chosen layers and compare with the
   subscriber's downlink limit (section 4), less audio, less a tenth
   held back.
   If it does not fit, lower layers one step at a time in this order
   until it does: the smallest tiles first, the current speaker last,
   a shared screen never. A track already at its lowest layer is
   paused. When the estimate recovers, tracks are raised in the reverse
   order, one step every five seconds, so the picture does not flap.

The server switches a subscriber to a new layer only at a key frame of
that layer. It asks the publisher for one and keeps forwarding the old
layer until it arrives.

Each change is reported to the subscriber with `call.layer_changed`, so
the client can show "video paused" instead of a frozen picture.

### What a publisher is told

The server tells each publisher which of its layers anyone is
receiving, with `call.demand_changed`. A client stops encoding a layer
nobody wants and starts again when asked, which saves the publisher's
battery and uplink, and the server's inbound traffic. A camera nobody
is showing costs almost nothing.

## 8. Audio and the active speaker

- Every participant's microphone is forwarded to every other
  participant, as `DESIGN.md` says. A muted microphone sends nothing,
  and silence suppression means a quiet one sends almost nothing.
- Clients publish the audio-level header extension (RFC 6464) on every
  audio packet. It states how loud that packet is, so the server can
  tell who is speaking without decoding any audio.
- **Detection.** For each microphone the server averages the level
  over 300-millisecond windows; a window with fewer than 3 packets
  counts as silent. A participant is speaking when two windows running
  are above the threshold (louder than −40 dBov). They
  stop being a speaker after 1.5 seconds below it.
- **Reporting.** The server sends `call.speakers_changed` to
  participants when the set of speakers changes: up to three account
  identifiers, loudest first, at most three messages a second.
- **Use.** Clients mark speakers' tiles, and move the loudest to the
  main tile in the speaker layout. The server uses the loudest as "the
  current speaker" in section 7.

## 9. Screen sharing

A shared screen is a second video track from the same participant,
with source `screen`, beside their camera. It is published on the
`send` connection like any track.

| | |
|---|---|
| Encoding | VP8, one layer, up to 1920 × 1080, up to 2,500 kbit/s, 5 to 15 frames a second, tuned for sharp detail over smooth motion |
| How many | One screen at a time per call. A second person who tries gets `screen_in_use` and their client says who is sharing |
| Forwarding | Subscribed like a camera. Never lowered or paused for bandwidth before cameras are |
| Sound | The screen's sound is not shared |

Where a screen can be shared from:

| Client | How |
|--------|-----|
| Desktop apps | A picker for a screen or a window |
| Desktop browsers | The browser's own picker |
| Android app | The system's screen capture prompt, with a notification while sharing |
| iPhone app | The system's broadcast picker, through a broadcast extension in the app. The whole screen only |
| Phone browsers | Not possible |

## 10. Signalling

### Frames

Signalling messages are JSON objects on the client's WebSocket, beside
the events in `docs/EVENTS.md`. Every one has a `type` starting
`call.`.

- **Requests** go from client to server. They have an imperative
  name (`call.join`) and an `id` the client chooses, unique on that
  connection. The server answers each with `call.ok` or `call.error`
  carrying the same value as `reply_to`. One exception: the server
  answers a `call.offer` with `call.answer`.
- **Events** go from server to client. They follow the event rules in
  `CONVENTIONS.md`: a past-tense name, `type`, `at` and
  `conversation_id`. They have no `seq`, are never stored and are not
  replayed on catch-up. A client that connects late reads the current
  call from the conversation (section 19).
- Four events about a call go to **every member** of its conversation,
  so the header can show that a call is on: `call.started`,
  `call.ended`, `call.participant_joined` and
  `call.participant_left`. Everything else goes only to participants.

Identifiers: `call_id` is a UUIDv7. A track's `id` is a short
identifier unique within the call, made by the server (`t7`). `mid` is
the media line's identifier from the SDP.

### The call object

Returned by the call join operation, carried by `call.started`, and
included in a conversation when it has a live call.

```json
{
  "id": "0193b100-2c1e-7b3a-9f0e-2a4d5c6b7e8f",
  "conversation_id": "0193a6f0-1111-7aaa-8bbb-000000000001",
  "message_id": "0193b100-2c20-7000-8000-000000000002",
  "started_by_id": "0193a000-0000-7000-8000-000000000001",
  "started_at": "2026-11-13T09:30:00.000Z",
  "sealed": false,
  "hosted_by": null,
  "participants": [
    {
      "account_id": "0193a000-0000-7000-8000-000000000001",
      "device_id": "0193a0d0-0000-7000-8000-000000000001",
      "joined_at": "2026-11-13T09:30:00.412Z",
      "tracks": [
        {"id": "t1", "source": "microphone", "kind": "audio", "muted": false},
        {"id": "t2", "source": "camera", "kind": "video", "muted": false,
         "codec": "vp8", "layers": ["q", "h", "f"], "width": 1280, "height": 720}
      ]
    }
  ]
}
```

`hosted_by` is the name of the server carrying the media when that is
not this one (section 16). `tracks` is present only in copies sent to
participants.

### Sequence

    client                                   server
      │  call join operation (HTTP)  ───────▶│  perm, create call, ticket
      │◀─────────────────  call, ticket      │
      │  call.join  ────────────────────────▶│
      │◀──────────────────────────  call.ok  │  full state, limits
      │  call.offer (send)  ────────────────▶│
      │◀────────────────  call.answer (send) │  track ids
      │  ICE and DTLS on the media port  ◀══▶│
      │◀─────  call.track_published (to all) │
      │◀─────────────────  call.offer (recv) │  others' audio
      │  call.answer (recv)  ───────────────▶│
      │  call.subscribe  ───────────────────▶│  which video, what size
      │◀─────────────────  call.offer (recv) │  video added
      │  call.answer (recv)  ───────────────▶│

### Requests

**`call.join`**

```json
{
  "type": "call.join",
  "id": "c1",
  "call_id": "0193b100-2c1e-7b3a-9f0e-2a4d5c6b7e8f",
  "ticket": "kq3v…43 characters…",
  "client": {"simulcast": true, "video_codecs": ["vp8", "h264"], "frame_encryption": true}
}
```

Answer:

```json
{
  "type": "call.ok",
  "reply_to": "c1",
  "call": {"id": "0193b100-…", "participants": ["…as in the call object…"]},
  "resume_token": "9fd0…43 characters…",
  "limits": {"max_participants": 50, "max_cameras": 12, "max_subscriptions": 16,
             "audio_kbps": 32, "screen_kbps": 2500,
             "layers": {"q": 150, "h": 500, "f": 1200}}
}
```

**`call.offer`**, on the `send` connection, whenever the client adds
or removes a track or restarts ICE:

```json
{
  "type": "call.offer",
  "id": "c2",
  "pc": "send",
  "sdp": "v=0\r\no=- 46117317 2 IN IP4 127.0.0.1\r\n…",
  "ice_restart": false,
  "tracks": [
    {"mid": "0", "source": "microphone"},
    {"mid": "1", "source": "camera"},
    {"mid": "2", "source": "screen"}
  ]
}
```

`tracks` lists every media line that is sending. A line that was there
before and is missing now has been unpublished. A video entry may
carry `width` and `height`, the size the client sends. A track sent in
one size only (no simulcast) is reported with layer `f` and no
`layers`. An offer the server refuses changes nothing: none of its
tracks is applied. The answer:

```json
{
  "type": "call.answer",
  "reply_to": "c2",
  "pc": "send",
  "sdp": "v=0\r\no=- 7718391 2 IN IP4 0.0.0.0\r\na=ice-lite\r\n…",
  "tracks": [
    {"mid": "0", "track_id": "t7"},
    {"mid": "1", "track_id": "t8"},
    {"mid": "2", "track_id": "t9"}
  ]
}
```

**`call.answer`**, the client's answer to a server offer on `recv`:

```json
{"type": "call.answer", "id": "c3", "pc": "recv", "offer_id": 4, "sdp": "v=0\r\n…"}
```

**`call.candidate`**, one per ICE candidate the client finds, and one
with `null` when it has finished:

```json
{"type": "call.candidate", "id": "c4", "pc": "send",
 "candidate": "candidate:842163049 1 udp 1677729535 203.0.113.7 51234 typ srflx …",
 "sdp_mid": "0", "sdp_mline_index": 0}
```

The server never sends `call.candidate`. Its candidates are fixed and
are in every SDP it sends. It takes the client's for diagnostics; as
an ICE-lite agent it learns the working path from the client's checks.

**`call.subscribe`**

```json
{
  "type": "call.subscribe",
  "id": "c5",
  "video": [
    {"track_id": "t8", "width": 640, "height": 360},
    {"track_id": "t9", "width": 1920, "height": 1080}
  ]
}
```

Answer:

```json
{
  "type": "call.ok",
  "reply_to": "c5",
  "video": [
    {"track_id": "t8", "state": "forwarding", "layer": "h"},
    {"track_id": "t9", "state": "forwarding", "layer": "f"}
  ]
}
```

`state` is `forwarding`, `paused` (no bandwidth), `muted` (the
publisher turned it off) or `unavailable` (this client cannot decode
it, or the track has gone).

**`call.mute`**, for the client's own tracks only:

```json
{"type": "call.mute", "id": "c6", "source": "microphone", "muted": true}
```

A muted track stays published and sends nothing. Turning a camera off
is a mute. Stopping a screen share is an unpublish: a new `call.offer`
without that line.

**`call.restart`**, asks the server for a fresh offer with new ICE
credentials on `recv` (section 11):

```json
{"type": "call.restart", "id": "c7", "pc": "recv"}
```

**`call.resume`**, on a new WebSocket after the old one dropped:

```json
{"type": "call.resume", "id": "c1", "call_id": "0193b100-…", "resume_token": "9fd0…"}
```

Answered like `call.join`, with a new `resume_token`.

**`call.decline`**, from someone being rung:

```json
{"type": "call.decline", "id": "c8", "call_id": "0193b100-…"}
```

**`call.leave`**

```json
{"type": "call.leave", "id": "c9", "call_id": "0193b100-…"}
```

### Server to client

**`call.offer`**, on `recv`. The server sends one at a time and waits
for its answer; changes that happen meanwhile go into the next one.

```json
{
  "type": "call.offer",
  "pc": "recv",
  "offer_id": 4,
  "sdp": "v=0\r\n…a=ice-lite\r\n…",
  "ice_restart": false,
  "tracks": [
    {"mid": "0", "track_id": "t1", "account_id": "0193a000-…", "device_id": "0193a0d0-…"},
    {"mid": "1", "track_id": "t2", "account_id": "0193a000-…", "device_id": "0193a0d0-…"}
  ]
}
```

**`call.started`** and **`call.ended`**, to every member:

```json
{"type": "call.started", "at": "2026-11-13T09:30:00.000Z",
 "conversation_id": "0193a6f0-…", "call": {"id": "0193b100-…", "…": "the call object"}}

{"type": "call.ended", "at": "2026-11-13T09:51:24.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "reason": "empty", "duration_seconds": 1284}
```

**`call.participant_joined`** and **`call.participant_left`**, to every
member:

```json
{"type": "call.participant_joined", "at": "2026-11-13T09:31:02.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "participant": {"account_id": "0193a001-…", "device_id": "0193a0d1-…",
                 "joined_at": "2026-11-13T09:31:02.000Z"}}

{"type": "call.participant_left", "at": "2026-11-13T09:40:10.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "account_id": "0193a001-…", "reason": "left"}
```

`reason` is `left`, `timeout`, `joined_elsewhere`, `removed` or
`signed_out`.

**`call.track_published`** and **`call.track_unpublished`**:

```json
{"type": "call.track_published", "at": "2026-11-13T09:31:04.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "account_id": "0193a001-…", "device_id": "0193a0d1-…",
 "track": {"id": "t8", "source": "camera", "kind": "video", "muted": false,
           "codec": "vp8", "layers": ["q", "h"], "width": 640, "height": 360}}

{"type": "call.track_unpublished", "at": "2026-11-13T09:38:00.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "account_id": "0193a001-…", "track_id": "t9"}
```

`call.track_published` goes to every participant, the publisher
included. When a participant leaves the call no
`call.track_unpublished` is sent for its tracks:
`call.participant_left` stands for them all. (When the account joins
from another device, its old device's tracks are unpublished one by
one.)

**`call.mute_changed`**:

```json
{"type": "call.mute_changed", "at": "2026-11-13T09:32:00.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "account_id": "0193a001-…", "track_id": "t7", "source": "microphone", "muted": true}
```

**`call.speakers_changed`**:

```json
{"type": "call.speakers_changed", "at": "2026-11-13T09:32:05.300Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "account_ids": ["0193a000-…", "0193a001-…"]}
```

**`call.layer_changed`**, to one subscriber:

```json
{"type": "call.layer_changed", "at": "2026-11-13T09:33:00.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "track_id": "t8", "state": "forwarding", "layer": "q", "reason": "bandwidth"}
```

`reason` is `size`, `bandwidth` or `publisher`.

**`call.demand_changed`**, to one publisher:

```json
{"type": "call.demand_changed", "at": "2026-11-13T09:33:00.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "track_id": "t8", "layers": ["q"]}
```

An empty list means nobody is showing the track.

**`call.ring_started`**, **`call.ring_stopped`** and
**`call.declined`** (section 15):

```json
{"type": "call.ring_started", "at": "2026-11-13T09:30:00.100Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "from_account_id": "0193a000-…", "until": "2026-11-13T09:30:45.100Z"}

{"type": "call.ring_stopped", "at": "2026-11-13T09:30:12.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "reason": "joined_elsewhere"}

{"type": "call.declined", "at": "2026-11-13T09:30:12.000Z",
 "conversation_id": "0193a6f0-…", "call_id": "0193b100-…",
 "account_id": "0193a001-…"}
```

`call.ring_stopped` goes to the account that was being rung, with
reason `joined_elsewhere`, `declined_elsewhere`, `ended` or `timeout`.
`call.declined` goes to the call's participants.

### Errors

```json
{"type": "call.error", "reply_to": "c5", "code": "not_in_call",
 "message": "You are not in this call."}
```

A `call.error` without `reply_to` reports something the server found
by itself, such as media that never arrived. A frame dropped by the
live connection's rate limit gets the connection's generic `error`
frame (`EVENTS.md` section 5), not `call.error`.

| Code | Meaning |
|------|---------|
| `invalid_request` | Malformed message, or an SDP the server cannot use |
| `ticket_invalid` | The ticket or resume token is unknown, used or too old. Call the join operation again |
| `forbidden` | No longer allowed in this call |
| `not_in_call` | A request that needs `call.join` first |
| `call_full` | The call has the most participants allowed |
| `cameras_full` | The most cameras allowed are already on. The microphone still works |
| `screen_in_use` | Someone else is sharing a screen. `account_id` says who |
| `too_many_subscriptions` | More video tracks asked for than allowed |
| `frame_encryption_required` | A sealed chat's call, and the client did not say it can encrypt frames |
| `media_timeout` | Signalling worked and no media connection was made within 15 seconds. Usually a blocked port |
| `host_unreachable` | The server hosting a shared channel's call cannot be reached over the link |
| `rate_limited` | Too many signalling messages. `retry_after_seconds` is given |
| `feature_off` | Calls are switched off on this server |
| `internal` | The media server failed |

The call join operation itself uses the HTTP errors in
`CONVENTIONS.md`: `forbidden`, `archived`, `timed_out`, `feature_off`,
`not_found`, and `conflict` with detail `call_full`.

## 11. Reconnection

Signalling and media fail separately and are mended separately.

**The WebSocket drops, media still flows.** People keep hearing each
other. The server holds the participant for 30 seconds. The client
reconnects its WebSocket as it always does, sends `call.resume`, and
carries on. Offers that were waiting are sent again.

**Media stalls, the WebSocket is fine.** This is a change of network
(Wi-Fi to mobile data), a new address, or a long loss. When a
connection's ICE state has been `disconnected` for three seconds, or
becomes `failed`, the client restarts ICE on both connections:

- on `send`, it makes a new offer with new ICE credentials and sends
  `call.offer` with `ice_restart: true`;
- on `recv`, it sends `call.restart`, and the server sends a
  `call.offer` with `ice_restart: true`.

The DTLS session and the tracks survive an ICE restart, so the others
see a freeze, not a leave. A client tries the restart up to three
times, five seconds apart, then tears both connections down and joins
again with a new ticket.

**Both gone.** After 30 seconds with neither, the participant has
left, reason `timeout`. If they were the last, the call ends.

**The server restarted.** Rooms are in memory, so the call is over.
Clients find their WebSocket gone and their media dead, reconnect, see
no live call on the conversation, and show "The call ended because the
server restarted". Anyone can start a new one.

**The app goes to the background on a phone.** Audio continues, under
the platform's call audio mode. The client subscribes to no video and
mutes its camera, and puts both back when it returns.

## 12. Limits and capacity

### Limits

| Limit | Default | Setting |
|-------|---------|---------|
| Participants in a call | 50 | `calls.max_participants` |
| Cameras on at once in a call | 12 | `calls.max_cameras` |
| Screens shared at once in a call | 1 | Fixed |
| Video tracks one client may subscribe to | 16 | Fixed |
| Signalling messages per participant | 20 a second | Fixed |
| Longest call | 24 hours | Fixed |
| Alone in a call | 60 minutes | Fixed |
| Ring | 45 seconds | Fixed |
| Join ticket | 60 seconds, one use | Fixed |
| Hold after losing signalling and media | 30 seconds | Fixed |

### Goals to test

From `BRIEF.md`. None of these has been measured.

- **A call with 25 people on audio and 8 on video on the mid-sized
  server** (4 CPUs, 8 GB) while it also serves 5,000 members on
  PostgreSQL.
- **Calls on the small server** (1 CPU, 1 GB). `DESIGN.md` section 21
  expects this may hold for audio only. The test should find the
  number of people at which a voice call, and then a call with
  cameras, stops being usable.

What the first goal means in traffic, worked out from the numbers in
sections 6 and 7. These are estimates to check against a test.

| Flow | Estimate |
|------|----------|
| Into the server: 25 microphones | Under 1 Mbit/s |
| Into the server: 8 cameras, all layers wanted | About 15 Mbit/s. Less when nobody shows a large tile |
| Out to one participant: 24 microphones | Under 1 Mbit/s, much less with most people silent |
| Out to one participant: 8 tiles at layer `h` | About 4 Mbit/s |
| Out of the server, all 25 showing 8 tiles at `h` | About 100 Mbit/s |

Forwarding costs the server mostly bandwidth, then the CPU to encrypt
each packet once per subscriber. The test records, for each goal: CPU
and memory on the server, packet loss and delay seen by clients, time
to join, and what happens when one subscriber's connection is poor.
The result goes in the README whatever it is.

### The call test

The admin console has a "Test calls" button (`testCalls`). The
admin's browser makes a media connection to the server, which keeps
it for 30 seconds, and reports which of UDP and TCP worked and which
address was used. It is the quickest way to find a
closed port or a wrong `-media-address`.

## 13. Calls in the web chat

The web chat joins the same calls with the same signalling, using the
browser's built-in WebRTC. The server does not tell a browser from an
app.

What browsers impose, all accepted in `BRIEF.md`:

| Limit | Effect |
|-------|--------|
| Secure pages only | Camera and microphone work only when the page is served over HTTPS, or from localhost. A server reached over plain HTTP on a network address has no calls in the web chat |
| Permission prompts | The browser asks for the microphone and camera, per site, and may ask again. A refusal means joining to listen only |
| No screen sharing from phone browsers | The Share screen button is not shown there |
| Nothing when closed | An incoming call shows only while the page is open (`PUSH.md` section 13) |
| iPhone background | The audio may stop when the tab goes to the background or the phone locks. The page says so when a call is joined on an iPhone |
| Sound needs a touch | A browser may refuse to play the ring sound, or the other people's audio, until the person has touched the page. The join button counts |
| Sealed calls need a recent browser | Frame encryption needs the browser's encoded-transform interface. A browser without it cannot join a sealed chat's call, and is told why |

The web chat's call code is in `internal/web/static/chat/call/`
(`CLIENTS.md` section 21).

## 14. Calls in a sealed chat

A sealed chat's call is between its two people, through the server
like any other call. On top of the DTLS-SRTP every call has, each
audio and video frame is encrypted on the sending device with a key
the server never has. `docs/SEALED.md` is the authority on the keys;
this is what it means for the call.

- **Where the key comes from.** A sealed chat has key epochs, each
  with a 32-byte epoch secret that only the two people's devices
  hold. Each device derives the call's frame keys from the current
  epoch secret with HKDF-SHA-256, with a label of their own and the
  call's identifier and the sender's device identifier as context.
  The exact label and layout are in `docs/SEALED.md`.
- **Format.** The frame format is the one libwebrtc's frame cryptor
  uses, and `docs/CALL-FRAMES.md` is its definition: AES-128-GCM over
  each encoded frame, before it is split into packets, under a key
  derived from the sender's call sender key, with the IV and a key
  index carried after the ciphertext. The apps get it from their
  WebRTC plugin, which does it natively. The web chat implements the
  same format in JavaScript on encoded frames, in a worker. Both are
  checked against the same vectors (`testdata/call-frames/vectors.json`).
  Video in a sealed chat's call is VP8 only.
- **Whose key.** Each device sends under its own call sender key, so
  a receiver must know which device a track comes from. The server
  says: `device_id` on `call.track_published` and beside each track of
  a `call.offer` (section 10). The receiver uses that device's key if
  the device is in the current epoch, and otherwise has no key for the
  track: nothing from it decrypts. The server is trusted for nothing
  here; naming the wrong device only makes frames fail to decrypt.
- **Rotation.** A new epoch starts when either person's device list
  changes, or when the epoch is more than seven days old at the time
  someone sends (`sealed.epoch_started`). Each device then derives
  new frame keys and sends with them under the next key number.
  Receivers keep the previous keys for ten seconds so frames already
  on their way still decrypt.
- **Never stored.** Frame keys have their own derivation label, are
  held in memory only, are not written to the key backup, and are
  dropped when the call ends.
- **What the server sees.** Who is in the call, when, how much is
  sent, which layer each frame belongs to, and the audio-level
  extension: so it knows when each person is speaking and how loudly.
  The first bytes of a video frame, which say whether it is a key
  frame, stay readable so the server can switch layers at key
  frames. It cannot decode a frame. It forwards them unchanged.
- **Who can listen.** Any device that holds the epoch secret can
  derive the keys. That is the two people's own devices and nobody
  else.
- **Failure.** A frame that does not decrypt is dropped. If none
  decrypts for five seconds the client shows "Cannot decrypt this
  call" instead of noise or a frozen picture, and waits for the next
  epoch.
- **Clients.** A client states `frame_encryption: true` in
  `call.join`, and the server refuses a sealed chat's call without
  it. The server cannot check that a client really encrypts; the
  other person's device finds out, because nothing decrypts.

The derivation and the frame format are among the things the outside
review of sealed chats should look at.

## 15. Ringing and do not disturb

Starting a call can ring people. Joining one that is already going
never does.

**The rule is one rule for every conversation** (`docs/FEATURES.md`,
decision F7). A call starting rings an account when all of these are
true:

- it is a member of the conversation at level `member` or above;
- its mode for the conversation is Any activity;
- it has not blocked the person who started the call;
- it is not in do not disturb, from any cause: a manual one, quiet
  hours, a status that pauses notifications, or a meeting set to pause
  them. Nothing pushes a ring through; notify anyway is for messages;
- it is not already in this call.

What that means for each kind of conversation comes from the default
modes:

| Conversation | Default mode | So, when a call starts |
|--------------|--------------|------------------------|
| Sealed chat | Any activity | The other person is rung, unless they changed the mode |
| Ad-hoc channel | Any activity | Every other member is rung, unless they changed the mode |
| Named channel | Mentions | Nobody is rung except members who chose Any activity for it. Everyone else sees the system message and "Join" in the header |

The system message itself notifies nobody. The ring is the
notification.

The caller sees who is being rung, and sees a do not disturb mark on
anyone who is not, before and after starting.

**How a ring runs.**

1. The server sends `call.ring_started` to every connected client of
   each person being rung, and sends a wake-up to their phones on the
   rule in `PUSH.md` section 2.
2. Clients ring (`PUSH.md` section 13) until one of these:
   - the person joins, on any device: their other devices get
     `call.ring_stopped`, reason `joined_elsewhere`;
   - the person declines: `call.decline`, their other devices get
     `call.ring_stopped` with `declined_elsewhere`, and the call's
     participants get `call.declined`;
   - the call ends: `ended`;
   - 45 seconds pass: `timeout`.
3. A ring that was not answered leaves the call going. The caller
   stays in it until they leave. The person rung sees "Missed call" on
   the system message once the call has ended, and the message counts
   as unread like any other.

Ring state is held in memory. It is not in the database.

**While in a call**, a person's presence carries the in-a-call mark
(`DESIGN.md` section 3). Being in a call does not put them in do not
disturb. A second ring shows as a banner without the ring sound.

**Do not disturb never blocks joining.** Someone in do not disturb
sees the call in the conversation and can join it whenever they look.

## 16. Calls in a shared channel

A call in a conversation shared with other servers lives on one
server: the home server of whoever started it, the host. This covers
shared channels, and the ad-hoc channels and sealed chats that run
between servers over the same peer. `docs/LINK-PROTOCOL.md` section
18 has the frames.

- **Hosting.** The host server creates the room and the `calls` row
  with no `host_server_id`. It posts the system message and submits
  `call.started` to the stream, naming itself as host. Each other
  server adds its own `calls` row with `host_server_id` set.
- **The pass.** When a member of another server calls the join
  operation on their own server, that server sends the host a
  `call_pass_request`: "my account A wants to join call C". The host
  checks that the call is live, that A's home server is the one
  asking, and that A is a member of the conversation at level
  `member` or above in the host's own copy. It answers with a
  `call_pass`: 32 random bytes, good for 60 seconds and one use, tied
  to that account and call. The member's server returns it to the
  client as the ticket.
- **Signalling** is relayed. The client talks only to its own server,
  as always. Its server forwards each `call.*` request to the host as
  a `call_signal` and forwards the host's messages back. These are
  **sealed direct frames between the two servers**: each is sealed to
  the receiving server's key and signed by the sending server
  (`docs/LINK-PROTOCOL.md` section 7). They are not under the
  stream's key, are not numbered and are never stored. A hub passes
  them on without being able to read them. The first `call_signal`
  carries the pass, and the host answers none for an account without
  one.
- **Media** goes straight from the member's device to the host
  server's media port. The addresses are in the SDP the host sends.
  The ICE credentials in that SDP are what admit the media, and they
  are only given to a client that presented a valid pass.
- **Events.** The host submits `call.participant_joined`,
  `call.participant_left` and `call.ended` to the stream, and each
  server tells its own members.

What follows from this:

- The host server's operator learns the network address of every
  remote participant's device, and the device connects to a server
  that is not its own. The client says so before joining. Other
  members still learn nobody's address.
- The host's media port must be reachable from the internet. A server
  that sits behind a hub with no public media port can join calls and
  cannot host them. When one of its members starts a call in a shared
  conversation, remote members are told `host_unreachable` and local
  members can still join.
- If two people on different servers start a call at the same moment,
  the `call.started` that comes first in the shared-clock order
  (`DESIGN.md` section 11) is the call, the same on every server. The
  other server ends its local call with reason `server` and offers its
  member the real one.
- If the link to the host drops, media already flowing carries on,
  and signalling waits. A server that has heard nothing from the host
  for 90 seconds ends the call locally.
- Between the two servers a participant who drops is reported with
  the `call.disconnected` signal. A remote participant whose session
  ends on its own server is not detected by the host as such: the
  host sees the media and signalling stop and holds the participant
  for 30 seconds, as for any drop.
- The host's limits and settings apply to everyone in the call.
- Ringing is decided by each person's own server, from its own copy of
  their mode and do not disturb.
- A sealed chat between servers encrypts frames exactly as in section
  14. Neither server and no hub can decode them.
- Calls that span servers without a shared conversation do not exist.

## 17. Nothing is recorded

- The server never writes media to disk and never decodes it. It
  holds about two seconds of packets in memory per published video
  layer, to answer retransmission requests.
- There is no recording feature, no transcript and no server-side
  mixing.
- What is stored about a call is the `calls` row (conversation, who
  started it, start and end) and the `call_participants` rows (who,
  joined, left). They are removed with the system message under
  retention. A sealed chat's call stores the same.
- The server logs a call's start, end and participant count, and media
  errors. It does not log network addresses of participants at the
  default log level.
- A participant can of course record with their own device. Nothing
  can prevent that, and the apps do not offer it.

## 18. Decisions recorded here

- Two peer connections per participant, to avoid crossed offers.
- ICE-lite, so the server never sends packets to an address a client
  named.
- The media port defaults to 24877.
- One system message per call, changed from `call_started` to
  `call_ended`.
- One device per account in a call. Joining elsewhere moves the call.
- A call ends when one person has been alone in it for an hour, and
  after 24 hours in any case.
- One shared screen at a time. No sound from a shared screen.
- Bots may join calls. They need their own media stack.
- Nobody can mute or remove another participant.
- A starting call rings members who have the conversation on Any
  activity, in every kind of conversation. Named channels start in
  Mentions, so they rarely ring.
- Notify anyway does not apply to calls.
- Signalling for a call hosted on another server is relayed through
  the member's own server as sealed direct frames, so clients never
  open a WebSocket to a server that is not theirs.
- Frame encryption uses the format the apps' WebRTC plugin already
  has, with keys derived from the chat's epoch secret.

## 19. Needs from other documents

- **API:** the call join operation answers with the call object and a
  `ticket`. A conversation object includes its live `call` when there
  is one, so a client that connects late can show it.
- **Events:** `docs/EVENTS.md` must allow client-to-server messages
  with a `type` and `id`, and the `call.*` names here.
- **Message format:** the `call_started` and `call_ended` system
  objects carry the fields in section 2.
- **Link protocol:** `call_pass_request`, `call_pass` and
  `call_signal` as sealed direct frames, and the stream's call
  events, all as `docs/LINK-PROTOCOL.md` section 18 has them. Its
  example pass shows port 3478; the default is 24877.
- **Schema:** nothing new. The call object's `started_by_id` is the
  `calls.started_by` column.

## 20. To verify before building

- That Pion's ICE-lite mode works together with one shared UDP port
  and ICE over TCP on the same number.
- Pion's send-side bandwidth estimator: whether it is good enough for
  section 7, step 3, or whether loss reports must stand in for it.
- The exact frame format of the frame cryptor in the apps' WebRTC
  plugin: where the counter and key number sit, which leading bytes
  of a VP8 or Opus frame stay readable, how a key is set per sender
  and key number, and that it works on all five platforms.
- That a browser worker can produce and read that format, and which
  versions of Firefox, Chrome and Safari have the encoded-transform
  interface it needs.
- H.264 availability in the apps' WebRTC library on each of the five
  platforms.
- Screen capture from the apps on Linux under Wayland, and the iPhone
  broadcast extension.
- That 24877 is not registered to something operators are likely to
  run.

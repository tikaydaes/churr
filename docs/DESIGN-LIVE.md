# Live rooms: design of the base infrastructure

The design behind `docs/ROADMAP-LIVE.md`: one room model, one media
architecture and one data model that carry today's calls, voice channels
with stage mode, meetings, streams and captions. It is a proposal. Nothing
here is built, and nothing is built until the owner approves it. Section 13
lists the decisions the owner must make; every number about capacity is
marked as measured or as an estimate.

Words: a **room** is a place where live media happens. A **session** is
one continuous use of a room. A **space** is a group inside a session whose
members hear and see each other. A **seat** is one person in a session. A
**media node** is the program that carries the media. Each is defined in
section 2.

## 1. What exists today

Read from the code and documents at `6a4981a`.

**The media server** (`internal/calls`, `docs/CALLS.md`):

- A selective forwarding unit on Pion (`go.mod`: `pion/webrtc/v4` v4.2.26),
  pure Go, inside the main server process. `internal/calls` is the only
  package that imports Pion (`docs/STRUCTURE.md` section 3).
- One media port, UDP and TCP, default 24877, ICE-lite, no TURN
  (`media.go` `openMedia`). `-media off` runs with no media port at all.
- Two peer connections per participant, `send` and `recv` (`room.go`
  `participant`). The server offers on `recv`, at most once every 150 ms.
- Forwarding: one goroutine per published layer reads RTP and writes it to
  each subscriber (`forward.go` `pubTrack.receive`, `downTrack.write`),
  rewriting sequence numbers, timestamps and VP8 picture numbers, and
  switching layers only at key frames. Simulcast in three layers `q`, `h`,
  `f` (`layers.go`), chosen per subscriber by tile size and a loss-based
  downlink limit (`downlink.go`).
- Audio: every unmuted microphone is forwarded to every participant, one
  media line each. Speakers are found from the RFC 6464 audio-level header
  without decoding (`speaker.go`), and `call.speakers_changed` goes to
  participants only.
- Codecs: Opus, VP8 preferred, H.264 constrained baseline as fallback
  (`media.go`).
- Signalling is `call.*` JSON frames on the client's one WebSocket
  (`signal.go`, `docs/EVENTS.md` section 11). Joining is an HTTP operation
  that returns a 60-second, one-use ticket, then `call.join`
  (`server.go` `Join`, `tickets.go`).
- A call belongs to a conversation, one live call per conversation
  (`calls_live` unique index, `docs/schema.sql`). Tables: `calls`,
  `call_participants`. One system message per call (`call_started`, then
  `call_ended`).
- Permission: one action, `start_call`, level `member` or above, not
  archived, not timed out (`internal/perm/perm.go`). Nobody can mute or
  remove another participant (`docs/CALLS.md` section 3).
- Limits: `calls.enabled`, `calls.max_participants` (50),
  `calls.max_cameras` (12), 16 video subscriptions, one screen.
- Clean-up: a tick every 250 ms drops participants whose signalling and
  media have both been gone 30 s, ends a call after one person has been
  alone 60 minutes or after 24 hours; a watch every 5 s removes anyone who
  may no longer be there, including a **locked session** (`server.go`
  `watch`, `ses.LockedAt`). Rooms are in memory; a restart ends every call.
- Sealed chats: two people, frames encrypted on the device with keys from
  the epoch secret (`docs/CALL-FRAMES.md`, `docs/CALLS.md` section 14).
- Linked servers: the call lives on the starter's server; other servers'
  members get a pass over the link, their signalling is relayed as sealed
  direct frames, and their media goes straight to the host's media port
  (`link.go`, `docs/LINK-PROTOCOL.md` section 18).
- The memory guard (`internal/memguard`) refuses live connections when
  memory is tight. `internal/calls` does not consult it.
- Nothing is recorded, decoded or mixed (`docs/CALLS.md` section 17).

**Measured** (`BENCHMARKS.md`, "A call beside chat"): one call of 25 on
audio and 8 on video, on 4 vCPU / 8 GB, took about 1.2 CPUs, sent about
113 Mbit/s, and the server held about 860 MB during it (99 MB before).
"Why a call takes that much is not known." No other machine size and no
second call has been measured.

**Around it:**

- Conversations: `kind` is `channel` or `sealed`; public, knock, private
  and ad-hoc come from `private`, `knock` and `name`
  (`internal/model/conversation.go`). Channel levels are `reader`,
  `member`, `manager`; per-channel switches are columns
  (`announcement`, `slow_mode_seconds`, `members.may_post`).
- The private-channel contract (R87: a new member never sees earlier
  content) is not in this tree; it is being built. This design depends on
  it and says where (section 8).
- Identifiers: every stored thing uses a UUIDv7 from `model.NewID`
  (`internal/model/id.go`, `docs/CONVENTIONS.md`); small things owned by a
  parent use short identifiers unique within it (form fields, tracks
  `t7`). Secrets given to people are random codes stored as hashes
  (`invites.code`, tokens).
- Files: `internal/blob` stores bytes on disk, in an S3 bucket or mixed;
  the `files` table; downloads honour Range. There are no quotas. The
  janitor (`internal/janitor`) applies retention hourly.
- Live events: one WebSocket; presence is sent by interest
  (`presence.subscribe`, `internal/live/interest.go`, R62); large channels
  are delivered only to connections that have them open (R66).
- Push wakes a phone only for a message or a ring (`docs/PUSH.md`).
  There is no "follow a channel"; a member's mode is Any activity,
  Mentions or Nothing.
- Scheduling: `internal/sched` runs scheduled messages, reminders and
  forms from the `scheduled` table. `model.Repeat` is day, week or month
  at a wall-clock time in a zone; it has no exceptions and is not RRULE.
- Forms: `/poll` makes a form on a message; `form.answered` carries live
  tallies (`internal/forms`).
- Blocks ("deaf ears", R77): the blocked person is never told. A call
  they start does not ring the blocker; nothing else in calls knows about
  blocks.
- Slow mode exists (`conversations.slow_mode_seconds`).
- Email: the server sends none (`DESIGN.md` section 1). Guests exist as a
  role invited into chosen conversations (`accounts.role = guest`,
  `invites`); there is no account without a passkey.
- Clients: the web chat's call code is plain modules in
  `internal/web/static/chat/call/`, with simulcast and encoded transforms;
  the app uses `flutter_webrtc` (`app/lib/core/calls/`) and already ships
  `media_kit_video`, which plays HLS. The web vendors one third-party
  library (highlight.js).
- Tests: `internal/calls/calltest` is a Pion client that speaks the real
  signalling with made-up media; `e2e/calls-web.mjs` drives Chrome with
  fake devices; `tools/loadtest -call` holds one call and reports loss,
  bit rate and delay.

## 2. The room model

### 2.1 Entities

| Entity | What it is | Stored |
|---|---|---|
| **Room** | The place for live media in one conversation, and its settings: mode, cap, video allowed, who may go live. One room per conversation, made the first time it is needed (for a voice channel, when the channel is made) | `rooms` |
| **Session** | One continuous use of a room, from the first person entering to the end. At most one live session per room. It lives on exactly one media node | `room_sessions` |
| **Space** | A group inside a session. Media is forwarded only inside a space. Every session has `main`; a session may also have `lobby`, `backstage` and breakout spaces | In memory; named on seat rows |
| **Seat** | One account, or one guest, in a session, on one device, with a role and a space | `room_seats` (also the attendance record) |
| **Track** | Microphone, camera or screen of a seat, as today | In memory |
| **Output** | Something the node makes from a session's media: a low-delay feed for viewers, an HLS feed, a recording, captions | `recordings` and in memory |
| **Meeting** | A schedule, an invite list and a policy (lobby, guests, co-hosts) for sessions of a room | `meetings` |

One conversation still has at most one live session, as it has at most
one call today. A meeting does not get a room of its own: it uses the room
of its conversation. A meeting scheduled outside any channel makes an
ad-hoc channel from its invitees and uses that.

### 2.2 Spaces, not sub-rooms

Lobby, backstage and breakouts are spaces of one session, not rooms of
their own.

- A seat is in exactly one space. The node forwards a track only to seats
  in the same space.
- Moving a seat between spaces changes what its existing `recv`
  connection carries. Nothing is re-negotiated from scratch, no new ICE or
  DTLS handshake happens, so sending forty people to breakouts and back is
  forty small offers, not forty joins.
- `lobby`: seats there have no media connection at all. They hold a seat
  and a signalling channel and wait. A media ticket is issued on
  admission.
- `backstage`: presenters and hosts before going live, or stepping out
  during a show. Never part of any output.
- Breakouts: `b1`, `b2`, … with a name and an optional end time. Hosts
  may move between them. Assignment is by host, at random into N, or by
  choice; the timer warns, then returns everyone to `main`.
- A session's caps count all spaces together.

Rejected: a sub-room as a full room with its own session (every move is a
full rejoin, and a breakout storm is a join storm); the lobby as a media
room (unadmitted strangers would hold media connections).

Moving someone to **another voice channel** is a different room: the
client receives `room.moved` with a ticket for the other room and joins
it.

### 2.3 Session kinds and lifecycle

A session's `kind` is fixed when it starts, from the room and from how it
was started:

| Kind | Where | Starts | Rings | Notice message |
|---|---|---|---|---|
| `call` | A text channel, ad-hoc channel or sealed chat | Someone presses Call | By today's rule (`docs/CALLS.md` section 15) | Yes, as today |
| `voice` | A voice channel in open mode | The first person drops in | Never | No |
| `stage` | A voice channel in stage mode | A host opens backstage or goes live | Never | "Live now" message when it goes live |
| `meeting` | Any channel, at a meeting's time or when a host opens it | Opening | Reminders, not a ring | The invite card |
| `stream` | A channel whose managers allow going live | Someone goes live | Never | "Live now" message |

States: `open` (people may be backstage or in the lobby, nothing is on
air) → `live` → `ended`. `call` and `voice` sessions are `live` from the
start. Ending rules are today's (empty, alone 60 minutes, 24 hours,
closed, server), plus `host_ended`. A `voice` session ends when the last
person leaves; the room stays.

### 2.4 Roles

A seat has one role. A role is a set of rights inside the session.

| Role | Publish microphone | Camera, screen | Moderate | Listed to others | Media path |
|---|---|---|---|---|---|
| `host` | Yes | Yes | Yes, and names co-hosts, ends the session | Yes | Full |
| `cohost` | Yes | Yes | Yes | Yes | Full |
| `presenter` | Yes | Yes | No | Yes | Full |
| `speaker` | Yes | If the room allows video; screen if the room allows | No | Yes | Full |
| `audience` | No, until invited up | No | No | Yes | Receive only |
| `viewer` | No | No | No | Counted, not listed | Receive only, or HLS |

"Moderate" is: mute a seat for everyone, disconnect, move, lock the
room, admit from the lobby, invite up and send back, run breakouts, start
and stop recording, choose Q&A questions.

A **guest** is not a role. It is a kind of identity (section 6.4), and a
guest's seat is a `speaker` at most unless a host raises it.

A raised hand is a flag on a seat with the time it went up; the queue is
the seats ordered by that time.

### 2.5 How roles come from channel permissions

Nothing is granted inside a room that the channel does not allow. The
starting role is worked out by `perm` from the level and the session kind:

| Level in the channel | `call`, `voice`, `meeting` | `stage` | `stream` |
|---|---|---|---|
| Acts as manager | `host` | `host` | `host` |
| Member | `speaker` | `audience` | `viewer`, or `presenter` if they may go live |
| Reader | May not join (today's rule) | `audience` | `viewer` |
| Not a member, public channel | Must join the channel first | Must join the channel first | Must join the channel first |
| Meeting organiser, co-host | `host`, `cohost` | – | – |
| Invited to the meeting, not a member | `speaker`, without the lobby | – | – |
| Guest by link | `speaker`, through the lobby | – | – |

Named presenters of a stage and people a host invites up are role changes
on seats, made by a host. A room remembers its named presenters
(`room_presenters`) so a town hall can be prepared before anyone is in it.

New `perm` actions replace `start_call`:

| Action | Need |
|---|---|
| `join_room` | Member; reader for `stage` and `stream`; archived, timed-out and sealed rules as `start_call` today |
| `watch_room` | Can see the conversation (roster and speaking marks from outside) |
| `moderate_room` | Acts as manager, or the seat's role is `host` or `cohost` |
| `set_room` | Manager: mode, cap, video, lock, who may go live |
| `go_live` | Per the room's `live_who`: `managers`, `chosen` (then `members.may_go_live`, the pattern of `may_post`) or `members` |
| `record_room` | Seat role `host` or `cohost`, and recording switched on for the server |
| `schedule_meeting` | Member |

A seat muted "for everyone" stays muted across rejoins in that channel:
`members.room_muted`, cleared only by someone who may moderate.

### 2.6 Today's calls become a case of this

Replace, do not layer. Churr is unreleased, so nothing is kept for old
clients.

- `internal/calls` becomes `internal/rooms`. `calls` becomes
  `room_sessions` with `kind = call`; `call_participants` becomes
  `room_seats`. The migration copies the rows.
- Signalling frames and events are renamed `call.*` → `room.*`. Ringing
  keeps its own names (`room.ring_started`, …) and applies to `call`
  sessions only.
- The system message kinds `call_started` and `call_ended` stay, with
  `session_id` in place of `call_id`.
- `start_call` becomes `join_room`. The settings `calls.*` become
  `rooms.*`.
- "Nobody can mute or remove another participant" ends: a manager can, in
  every kind of session, sealed chats excepted (two people, no manager).
- `docs/CALLS.md` is rewritten as `docs/ROOMS.md` when stage 1 is built;
  this document stays the design record.

Naming: "stream" is already the link protocol's word (`streams` table,
`model.StreamID`), and `live`, `broadcast` and `relay` are packages. In
code and schema the feature is a room session of kind `stream`; no table,
package or type is named `stream`.

## 3. Media architecture

### 3.1 Media nodes

The media engine becomes a part that can run in two places.

- **Built-in node.** The engine linked into the main server, as today.
  A plain install with `-media :24877` has calls exactly as now, with no
  other process and nothing to install. It carries `call`, `voice` and
  `meeting` sessions within the limits of its machine.
- **Media node.** A separate program, `churr-media`, built from the same
  module, on the same machine or on others. This is the recommended
  arrangement for stages, streams, recording and captions. It holds the
  media port, runs the forwarding, and runs the external tools
  (section 3.6). Several can be attached.

With rooms switched off (`-media off`, or `rooms.enabled` false and no
node attached) the main server opens no media port, starts no process and
allocates nothing for rooms. Each advanced part has its own switch, all
off by default: `rooms.voice`, `rooms.meetings`, `rooms.streams`,
`rooms.recording`, `rooms.hls`, `rooms.transcode`, `rooms.captions`.

**The engine boundary.** `internal/rooms` splits in two:

| Package | Holds | Imports Pion |
|---|---|---|
| `internal/rooms` | Rooms, sessions, seats, roles, permission, tickets, moderation, placement, events, the database. Runs in the main server only | No |
| `internal/rooms/node` | Peer connections, forwarding, layers, speakers, taps, outputs. Runs in the built-in node or in `churr-media` | Yes, the only one |

The main server talks to a node through one Go interface, `Node`. The
built-in node implements it with direct calls; a remote node implements
it over the node connection below. The rule "only one package imports
Pion" holds.

**The node connection.** A media node dials the main server, never the
reverse, so a node needs no inbound port other than its media port.

- An admin makes a node in the console and is shown a node token once
  (stored as a hash, like other tokens). `churr-media -server
  https://chat.example -token …`.
- The node opens a WebSocket to `/api/v1/media-nodes/connect` with the
  token, over TLS. Frames are protobuf, in `proto/`.
- `register`: the node's version (must equal the server's), CPUs, memory
  limit, media addresses and port, whether ffmpeg is present and its
  version, which hardware encoders passed a test encode, whether a speech
  helper is present.
- `load`, every 2 seconds: seats, viewers, packets forwarded a second,
  CPU, memory left, running tool processes.
- Commands from the server: open and close a session; admit a seat (the
  seat's identity, role, rights, space, and the hash of its ticket); change
  a seat (role, space, mute, remove); start and stop an output.
- Signalling: the client keeps talking only to its own server, on its
  WebSocket. The server relays `room.*` media frames (offer, answer,
  candidate, subscribe) to the session's node and the node's frames back.
  This is the shape already used for a call hosted by another server
  (`link.go` `remoteSink`), with a trusted peer.
- Reports from the node: seat connected and lost, track published, mute,
  speakers, output state, segment written.
- Media goes from the device straight to the node's media port. The
  addresses are in the SDP the node writes.

A node never serves HTTP to clients and holds no database connection. It
reads and writes the recording store (section 3.9) with credentials the
server gives it at registration.

**Placement.** A session, with all its spaces, lives on one node, chosen
when the session starts:

1. nodes that have what the session will need (ffmpeg for a recorded or
   multi-speaker HLS session, a hardware encoder if the admin requires
   one, the speech helper for captions);
2. among those, the node with the most room, by its reported load against
   its limits;
3. an admin may pin a room to a node, and may drain a node (no new
   sessions).

The built-in node is used when no media node is attached, or for `call`
sessions when the admin sets "keep calls on the main server". A session
does not move between nodes. If a node is lost, its sessions end with
reason `server`; people rejoin and the new session is placed elsewhere.

Rejected: cascading one room across nodes (a large addition; viewers
beyond one node's low-delay capacity go to HLS instead); the server
dialling nodes (needs inbound ports and certificates on every node).

### 3.2 What the current engine carries, and what changes

It carries rooms of up to about fifty with cameras, as measured once. Four
changes make it carry the roadmap:

1. **Memory per seat is found and fixed first.** 860 MB for 25 people is
   about 30 MB a seat. At that cost a hundred listeners do not fit in
   4 GB. Stage 0 profiles the heap under `loadtest -call`, names the cause
   and sets a budget per seat and per viewer that the capacity test then
   holds (section 9).
2. **Audio slots.** Section 3.3.
3. **Receive-only seats.** Section 3.4.
4. **Taps.** A tap receives the RTP packets of chosen tracks of a session,
   in order, with timing. Recording, HLS, transcoding and captions are all
   consumers of taps. Nothing else in the engine knows they exist. A
   session whose frames are encrypted end to end has no taps.

### 3.3 Audio in large rooms

Today each microphone is its own media line on every `recv` connection,
so every join and leave re-offers to everyone, and a room of 100 has
9,900 audio forwardings set up.

- Each `recv` connection gets a fixed set of **five audio slots**. The
  node puts the loudest five unmuted microphones of the seat's space into
  them, using the levels it already reads, and rewrites sequence numbers
  and timestamps at each switch as it does for video layers.
- The SDP no longer changes when people join or leave. `room.slots_changed`
  tells the client which seat is in which slot.
- A quiet microphone already sends almost nothing (DTX), and a muted one
  nothing, so the saving is in set-up, memory and worst-case traffic.
- Rooms of eight or fewer keep one line per microphone, and so do sealed
  chats, where each sender's frames need their own key.

Rejected: mixing audio on the server (needs decoding and encoding for
every listener and ends end-to-end encryption for good).

### 3.4 One to many

**Low delay, for hundreds.** A `viewer` or `audience` seat opens only the
`recv` connection: one handshake, no uplink state. It receives the
session's **programme**:

- the audio slots of the stage;
- the video the hosts have put on stage: by default the current speaker
  and any shared screen, at most four tiles; a host may pin.

Every viewer gets the same set, each at the simulcast layer its downlink
carries, chosen by the existing picker. A publisher from OBS sends one
layer; with transcoding on, the node adds lower ones (section 3.7).

**HLS, for thousands.** Past the node's low-delay viewer limit, or when
the viewer's client asks for it, the viewer gets HLS (section 3.6). The
server tells the client which in the join answer (`delivery: rtc | hls`).
Viewers on HLS hold no seat; they are counted from playlist requests.

A stage "beyond the room's cap" is this: seats up to the cap are
`audience` in the room, and everyone after that is a `viewer` of the same
session.

### 3.5 Speaking indicators for people outside the room

Anyone who can see a voice channel sees who is in it and who is speaking,
without any media. This is signalling, by interest, like presence.

- A client says which rooms are on its screen:
  `{"type": "rooms.watch", "room_ids": […]}`. The list replaces the one
  before; at most 50; sent again after `ready`. The server keeps only
  rooms the account may `watch_room`.
- The server answers with each newly watched room's state (`room.state`:
  seats with role, mute, camera, screen and hand marks, recording mark,
  viewer count), then sends changes.
- `room.speakers` carries up to three speaking seats per room, at most
  three a second per room. It is state: under pressure the hub merges and
  holds it back exactly as it does `presence.batch`.
- To every member of the conversation go only `room.session_started`,
  `room.session_ended` and `room.count_changed` (the number in the room, at
  most once every five seconds), so a busy voice channel in a large server
  does not cost a frame per join on every connection.
- A node reports speakers to the server at the same three-a-second limit,
  and only for sessions somebody is watching.

### 3.6 Ingest and packaging

**WHIP ingest** (RFC 9725, published March 2025:
<https://www.rfc-editor.org/rfc/rfc9725>).

- `POST /api/v1/whip/{room_id}` on the main server, `Content-Type:
  application/sdp`, `Authorization: Bearer <ingest key>`. The server checks
  `go_live`, starts or joins the room's `stream` session, relays the offer
  to the node, and answers `201` with the node's SDP and a `Location`.
  `DELETE` on that location ends the publication.
- The node is ICE-lite with fixed candidates in the answer, so trickle
  (`PATCH`) is not needed; it is answered `405`.
- An ingest key is made per person per channel in the channel's settings,
  shown once, stored as a hash (`ingest_keys`), and revocable.
- OBS has had WHIP output since version 30.0 and sends H.264 with Opus;
  its WHIP guide lists AV1 and H.265 as well, and says simulcast was not
  in a release as of 32.0
  (<https://obsproject.com/kb/whip-streaming-guide>,
  <https://www.phoronix.com/news/OBS-Studio-30-Released>). The node
  accepts H.264 main and high profile on ingest in addition to
  constrained baseline, and Opus in stereo.
- To verify before building: which H.264 profiles each browser and the
  apps' WebRTC library decode from a forwarded OBS feed.

**Going live from the web chat or the app** is an ordinary seat of role
`presenter` publishing camera or screen over the existing signalling. The
client publishes H.264 when the session has an HLS output or a recording,
so the node can package without converting.

**HLS packaging** runs on the node, in one of two ways:

| Path | When | Needs |
|---|---|---|
| **Passthrough** | One video and one audio source, video already H.264: an OBS feed, or one presenter | Nothing external. Pure Go: the node takes H.264 and Opus from the tap and writes fMP4 (CMAF) segments |
| **Tool** | Anything else: several people speaking (audio must be mixed), VP8 publishers, more than one quality, AAC audio | ffmpeg as a child process |

- Segments are 2 seconds, with a live playlist of the last 6. Delay to the
  viewer is a few seconds, which is what the roadmap asks of HLS.
  Low-latency HLS parts are not used: low delay is the WebRTC path's job.
- The node writes each finished segment to the recording store and tells
  the server. The server writes the playlists and authorises every
  playlist and segment request (section 3.9). The same segments are the
  replay.
- For passthrough, the fMP4 writing uses a pure-Go library rather than new
  code: `mediacommon` and `gohlslib` from the MediaMTX project are MIT,
  pure Go, and write fMP4 with H.264, H.265, AV1, VP9 and Opus
  (<https://pkg.go.dev/github.com/bluenviron/gohlslib/v2>). The node owns
  that dependency as it owns Pion.
- Audio in passthrough is Opus in fMP4. Chrome, Firefox and hls.js play
  it; Safari has decoded Opus in MP4 since version 17, and Apple's own
  HLS documents do not list Opus
  (<https://developer.apple.com/forums/thread/759236>). To verify in a
  spike on Safari, iPhone and the apps' player. If Apple playback fails,
  Apple viewers get the tool path's AAC rendition, or the low-delay path.
- Players: the app plays HLS with `media_kit`, which it already has. The
  web chat vendors hls.js (Apache-2.0,
  <https://github.com/video-dev/hls.js>), its second vendored library;
  from version 1.5 it runs on iPhone 17.1 and later through Managed Media
  Source. Safari's native HLS is the fallback.

**The tool path.** The node starts one ffmpeg per output and feeds it
over local pipes or loopback RTP:

- five audio inputs, one per audio slot, kept continuous by the node
  (silence is filled in), mixed to one track;
- one video input: the programme, which the node makes a single
  continuous stream by switching sources at key frames, as it switches
  layers today. With several tiles on stage ffmpeg composes them;
- outputs: fMP4 segments, in one quality or several, H.264 with AAC.

### 3.7 Several qualities

Off by default (`rooms.transcode`). When on, the tool path encodes a
ladder: the source as sent, plus 720p and 480p (and the admin may choose
others). For low-delay viewers the node feeds the encoded renditions back
in as extra simulcast layers of the programme.

**Hardware encoding.** At start a node asks ffmpeg which encoders it has
and runs a one-second test encode with each of VAAPI, NVENC, QSV and
VideoToolbox. Those that pass are reported at registration and used in
that order of preference set by the admin; software x264 is the fallback,
always present. A failed hardware encode during a session falls back to
software for that output and is logged. Hardware encoders have session
limits of their own; the node counts them.

### 3.8 What needs an external program, and licences

| Feature | Pure Go in the node | Needs ffmpeg | Needs the speech helper |
|---|---|---|---|
| Calls, voice channels, stage, meetings, low-delay viewers, WHIP ingest | Yes | | |
| HLS, replay and clips of a one-source H.264 stream | Yes | | |
| HLS or recording with several speakers, or from VP8 | | Yes | |
| Composite video (several tiles) | | Yes | |
| Several qualities, AAC audio, poster images | | Yes | |
| Captions and transcripts | | | Yes |

- **ffmpeg** is run as a separate program and spoken to over pipes. It is
  not linked. Churr's AGPL licence and ffmpeg's (LGPL 2.1 or later, or
  GPL when built with x264) do not combine: this is two programs side by
  side. Whoever distributes an image that contains ffmpeg must meet
  ffmpeg's licence for that copy, which a distribution's own package
  already arranges. Builds marked "nonfree" are never shipped.
- **H.264 encoding** (x264 or a hardware encoder) may need a patent
  licence in some countries for some operators. Passthrough does not
  encode. `docs/OPERATIONS.md` will say so beside the transcode switch.
- **GStreamer** was considered in ffmpeg's place and rejected: equally
  capable, but a larger set of plugins to package and a harder pipeline
  to drive from another process.
- **Packaging.** Three images:
  - `churr`: as today, static, distroless, no external program. Built-in
    node; passthrough only.
  - `churr-media`: a Debian base with the distribution's ffmpeg and the
    VAAPI drivers; runs `churr-media`.
  - `churr-media-nvidia`: the same for NVENC, for hosts with the NVIDIA
    container runtime.
  On a plain machine the operator installs ffmpeg from the system's
  packages; the node finds it on `PATH` or at `-ffmpeg`.
- The server and the node stay pure Go with no cgo. The speech helper may
  contain native code; it is a separate program (section 3.11).

### 3.9 Recording, replays and clips

**What is recorded.** A recording is the session's programme: the same
audio mix and the same video an HLS viewer would get, of the `main` space
only. Backstage, the lobby and breakouts are never recorded. It is
written as fMP4 segments, so a recording, a replay and the HLS feed are
one thing made once.

- A stream is recorded when the room's "keep replays" is on. A meeting or
  a stage is recorded when a host starts it.
- A meeting's recording therefore needs the tool path (several speakers).
  A one-source stream does not.
- When the session ends the node writes the final playlist and, with
  ffmpeg, one MP4 file for download.

Rejected as the default: one raw file per track (nothing watchable comes
out without a later composing pass; kept as a possible "archive" option);
composing every tile for every recording (several times the processor
cost; available as the recording layout `grid`, default `speaker`).

**Where it is stored.** Recordings, replays, HLS segments and clips go
to a **recording store** of their own, not into the uploads store:

- `-recordings disk:/path` (default: `recordings/` in the data directory)
  or `-recordings s3:…` for an S3-compatible bucket, using the client
  `internal/blob` already has.
- With media nodes on other machines the store must be one every node and
  the server can reach: a bucket, or a shared folder. The built-in node
  uses the local folder.
- Each recording is a row in `recordings` with its segments in
  `recording_segments`. It is attached to a message in the channel (the
  "recording" or "replay" message), which is what makes it visible and
  what retention removes.
- Serving: the server authorises each request. From disk it sends the
  bytes (Range works). From a bucket it answers with a redirect to a
  short-lived signed address, so thousands of HLS viewers are served by
  the bucket, not by the Churr server.
- Quotas: the setting `rooms.recording_quota_bytes` for the server and
  the column `rooms.quota_bytes` for a channel. A recording
  that would pass a quota is refused at the start; one that reaches it
  while running stops with a notice. The admin console shows use per
  channel.
- Retention: the channel's retention applies to the message, and the
  janitor removes the recording's rows and bytes with it. A room may set
  a shorter "replays kept for".

The roadmap says "stored like uploaded files". This differs in where the
bytes go and in not making a `files` row per segment; it keeps the quota
and retention behaviour. It is decision D7.

**Replays with chat.** A replay message carries the recording and its
start and end times. The client plays the VOD playlist and shows the
channel's messages from that span beside it, placed by `created_at`
against the recording's clock. Nothing new is stored: the chat is the
channel's log, read through the ordinary message API with the reader's
own permissions.

**Clips.** A viewer picks a span of a replay, up to two minutes. The
server asks a node to cut it: with ffmpeg, an exact cut into one MP4;
without, a cut at segment boundaries. The clip is a `recordings` row of
kind `clip` attached to the message the viewer posts, and counts toward
the quota.

### 3.10 Sessions on the built-in node

The built-in node runs the same code, so everything above works on one
machine when the admin switches it on and, for the tool path, ffmpeg is
installed. It is the arrangement for small communities and for
development. The recommended arrangement for stages and streams is at
least one media node, so that chat is not slowed by media.

### 3.11 Captions

Last in the order of work; only the hook is designed here.

- A **speech helper** is a separate program started by the node:
  `churr-speech`, kept outside the server's module because it may contain
  native code. It runs a local model; nothing leaves the machine and no
  outside service is used.
- The node gives the helper the Opus packets of each speaking seat from a
  tap, one stream per seat, so every line has its speaker without any
  mixing. The helper answers with lines: seat, start, end, text, final or
  not.
- The node sends lines to the server; the server sends `room.caption` to
  the seats and viewers that asked for captions, and, when the session is
  recorded, writes a WebVTT transcript beside the recording.
- Cost, to be measured before the switch exists, as the roadmap requires.
  From the model's own documents: about 390 MB of memory for the `base`
  model and 850 MB for `small`
  (<https://sources.debian.org/src/whisper.cpp/1.8.3%2Bdfsg-2/README.md>),
  and a processor share that grows with the number of people speaking at
  once. The model and its licence are chosen at stage 5.

## 4. Data model

Identifiers follow the codebase: UUIDv7 from `model.NewID` for every
stored thing with an `id`, as `docs/CONVENTIONS.md` requires for accounts,
messages, files and calls today. This design adds no other format for
them. Things owned by a session use short identifiers unique within it,
as tracks do now: spaces (`main`, `lobby`, `backstage`, `b1`), tracks
(`t7`), audio slots (`a1`). Things a person types or pastes are random
codes stored as hashes: guest links, ingest keys, node tokens. Whether
room and meeting links should carry something shorter than a UUID is
decision D17.

Times are `bigint` milliseconds as elsewhere. Migrations are numbered
pairs for PostgreSQL and SQLite in `internal/store/migrations/`; the next
free number is taken at build time (0011 is the last today, and R87 is
in flight).

### 4.1 Changed

| Table | Change |
|---|---|
| `conversations` | `voice boolean NOT NULL DEFAULT false`: a voice channel. Public, knock and private apply unchanged. `kind` stays `channel` |
| `members` | `may_go_live boolean`, `room_muted boolean` |
| `calls`, `call_participants` | Replaced by `room_sessions`, `room_seats`; rows copied |
| `accounts` | `guest_of uuid` (a meeting), `expires_at bigint`: an ephemeral guest (section 6.4) |
| `settings` | `calls.*` renamed `rooms.*`; new keys in section 9.4 |

A voice channel is a flag, not a third `kind`, because every channel rule
in `perm` should apply to it unchanged, and `kind` is what separates
sealed chats from channels there.

### 4.2 New

**`rooms`**: `id`, `conversation_id` (unique), `mode` (`open | stage`),
`video` (boolean), `cap` (integer, null = server maximum), `locked`,
`live_who` (`managers | chosen | members`), `keep_replays`,
`replay_days`, `quota_bytes`, `node_id` (pinned node, null),
`created_at`.

**`room_presenters`**: `room_id`, `account_id`: a stage's named
presenters.

**`room_sessions`**: `id`, `room_id`, `conversation_id`, `kind`
(`call | voice | stage | meeting | stream`), `meeting_id`, `occurrence_at`
(which occurrence of a repeating meeting), `message_id`, `started_by`,
`started_at`, `live_at`, `ended_at`, `end_reason`, `node_id`,
`host_server_id` (another server carries it), `peak_seats`,
`peak_viewers`. Unique index on `room_id` where `ended_at` is null.

**`room_seats`**: `session_id`, `account_id`, `joined_at`, `left_at`,
`role` (the highest held), `space` (the last), `delivery` (`rtc | hls`).
Primary key (`session_id`, `account_id`, `joined_at`). This is the
attendance record. Space changes during breakouts are added as rows of
**`room_seat_moves`** (`session_id`, `account_id`, `at`, `space`) only for
`meeting` sessions.

**`room_questions`**: `id`, `session_id`, `account_id`, `text`, `state`
(`waiting | shown | answered | dismissed`), `votes`, `created_at`; and
**`room_question_votes`** (`question_id`, `account_id`).

**`media_nodes`**: `id`, `name`, `token_hash`, `created_by`,
`created_at`, `last_seen_at`, `drained`, `removed_at`. Capacity and load
are held in memory from the node's own reports.

**`ingest_keys`**: `id`, `room_id`, `account_id`, `key_hash`, `label`,
`created_at`, `last_used_at`, `revoked_at`.

**`recordings`**: `id`, `session_id`, `conversation_id`, `message_id`,
`kind` (`recording | replay | clip`), `state`
(`writing | finishing | ready | failed`), `started_at`, `ended_at`,
`duration_ms`, `bytes`, `renditions` (jsonb), `download_key`,
`transcript_key`, `poster_key`, `source_id` and `source_from_ms`,
`source_to_ms` (a clip), `started_by`.

**`recording_segments`**: `recording_id`, `rendition`, `n`, `start_ms`,
`duration_ms`, `bytes`, `key`. Primary key (`recording_id`, `rendition`,
`n`).

**`meetings`**: `id`, `conversation_id`, `organiser_id`, `title`,
`description`, `starts_at`, `duration_minutes`, `time_zone`, `repeat`
(jsonb, `model.Repeat`), `repeat_until`, `lobby`
(`off | outside_invite | everyone`), `guests` (boolean),
`guests_may_share`, `record` (`off | ask | auto`), `sequence` (integer,
raised on every change, for .ics), `card_message_id`, `notes_message_id`,
`created_at`, `cancelled_at`.

**`meeting_exceptions`**: `meeting_id`, `occurrence_at`, `cancelled`,
`moved_to`, `duration_minutes`.

**`meeting_invitees`**: `meeting_id`, `account_id`, `cohost`, `answer`
(`none | yes | no | maybe`), `answered_at`; and for a repeating meeting
**`meeting_answers`** (`meeting_id`, `occurrence_at`, `account_id`,
`answer`) for one occurrence.

**`meeting_guest_links`**: `id`, `meeting_id`, `occurrence_at` (null =
every occurrence), `code_hash`, `label` (who it was made for), `email_enc`
(section 6.5), `max_uses`, `uses`, `expires_at`, `revoked_at`,
`created_by`.

**`boards`**: `id`, `conversation_id`, `message_id`, `session_id`,
`created_by`, `created_at`, `closed_at`, `snapshot_key`; and
**`board_ops`** (`board_id`, `seq`, `account_id`, `at`, `op` jsonb)
(section 7).

## 5. API and events, in outline

Operation names follow `docs/openapi.yaml`; paths under `/api/v1`.

**Rooms**

| Operation | Does |
|---|---|
| `getRoom` | The room, its settings and its live session as an outsider sees it |
| `setRoom` | Mode, cap, video, lock, who may go live, replays (`set_room`) |
| `joinRoom` | Checks permission, starts the session if none, answers with the session, the seat's role and space, `delivery`, whether it is being recorded, and a ticket. For a lobby seat there is no ticket yet |
| `leaveRoom` | As `call.leave`, for a client whose connection has gone |
| `goLive`, `endSession` | Stage and stream: `open` → `live`; end for everyone |
| `setSeat` | Role, space, mute for everyone, hand down (`moderate_room`) |
| `removeSeat`, `moveSeat` | Disconnect; move to another voice channel |
| `admitSeat`, `refuseSeat` | The lobby |
| `openSpaces`, `closeSpaces` | Breakouts: names, assignment (`host | random | choice`), timer |
| `setPresenters` | A stage's named presenters |
| `startRecording`, `stopRecording` | |
| `listQuestions`, `askQuestion`, `voteQuestion`, `setQuestion` | Q&A |
| `listIngestKeys`, `createIngestKey`, `revokeIngestKey` | |
| `whipPublish`, `whipStop` | Section 3.6 |
| `getPlaylist`, `getSegment` | HLS, live and replay: `/rooms/{id}/hls/…`, `/recordings/{id}/hls/…` |
| `getRecording`, `deleteRecording`, `createClip` | |
| `getAttendance` | A session's seats and times, for the organiser and hosts |

**Meetings**: `createMeeting`, `getMeeting`, `updateMeeting`,
`cancelMeeting`, `setOccurrence` (move or cancel one), `answerMeeting`,
`listMeetings` (mine, by time), `createGuestLink`, `revokeGuestLink`,
`openGuestLink` and `joinAsGuest` (no account; section 6.4).

**Admin**: `listMediaNodes`, `createMediaNode`, `drainMediaNode`,
`removeMediaNode`, `listLiveSessions`, `testRooms` (today's `testCalls`,
per node).

**Frames a client sends** (on the WebSocket, answered `room.ok` or
`room.error`): `room.join`, `room.resume`, `room.offer`, `room.answer`,
`room.candidate`, `room.subscribe`, `room.mute`, `room.restart`,
`room.leave`, `room.decline`, and new: `room.hand` (up or down),
`room.react` (a quick reaction, not stored), `room.captions` (on or off),
`rooms.watch`, `board.open`, `board.op`.

**Events**

| To | Events |
|---|---|
| Every member of the conversation | `room.session_started`, `room.session_ended`, `room.count_changed`, `room.changed` (settings), `room.live_changed` (live now, viewer count, at most every 10 s) |
| Connections watching the room | `room.state`, `room.seat_joined`, `room.seat_left`, `room.seat_changed` (role, space, mute, camera, screen, hand), `room.speakers`, `room.recording_changed` |
| Seats of the session | Today's set renamed (`room.track_published`, `room.track_unpublished`, `room.mute_changed`, `room.layer_changed`, `room.demand_changed`, `room.declined`), and `room.slots_changed`, `room.reaction`, `room.space_changed` (opened, closing in 60 s, closed), `room.moved`, `room.question_changed`, `room.caption` |
| Hosts | `room.lobby_changed` |
| A person being rung | `room.ring_started`, `room.ring_stopped` |
| Stored, replayed like other conversation events | `meeting.created`, `meeting.changed`, `meeting.cancelled`, `meeting.answered` (to the organiser and the answerer), `recording.changed` |
| Connections with the board open | `board.op` |

System message kinds added: `stream_started` / `stream_ended`,
`stage_started` / `stage_ended`, `meeting` (the invite card), `recording`
(with the recording), `replay`. `call_started` and `call_ended` stay.

## 6. Scheduling, invites and guests

### 6.1 Meetings and recurrence

- A meeting is a row in `meetings`. Its repeat rule is `model.Repeat`
  (`internal/sched/repeat.go`): every day on chosen weekdays, every week,
  or every month, at a wall-clock time in a zone. That covers a weekly
  stand-up. Exceptions are rows in `meeting_exceptions`: one occurrence
  cancelled or moved.
- `internal/sched` gains a meeting kind in its due-item loop (`sched.go`
  `Service.Run`): at each occurrence it sends reminders (15 minutes and
  at the start, to invitees who did not decline), and opens the session's
  window. It keeps no second timer system.
- A meeting's session may be opened by a host from 15 minutes before the
  start. Invitees entering before a host wait in the lobby.

Rejected: storing RRULE (a much larger grammar than the product offers;
RRULE is produced for calendars from the small model).

### 6.2 The invite card

A system message of kind `meeting` in the meeting's conversation, posted
by the server for the organiser, carrying the meeting. Clients draw the
card with Accept, Decline and Maybe, the time in the reader's zone, a
Join button while the session is open, and "Add to calendar". Answers go
through `answerMeeting`, not through forms, because an answer belongs to
a meeting or to one occurrence, and the card is updated in place like a
call's notice. An invitee who is not a member of the conversation gets
the card in their one-person channel.

### 6.3 Calendar files

- "Add to calendar" makes an `.ics` file on the device, as the roadmap
  says: `UID` is the meeting's id at the server's host name, `SEQUENCE`
  is `meetings.sequence`, `RRULE` comes from the repeat rule (`day` →
  `FREQ=WEEKLY;BYDAY=…`, `week` → `FREQ=WEEKLY`, `month` →
  `FREQ=MONTHLY;BYMONTHDAY=n`, or `-1` when the anchor day is past the
  month's end), `EXDATE` and `RECURRENCE-ID` from the exceptions, and the
  join address in `URL` and `LOCATION`.
- When the meeting changes, the card shows "Changed: add again"; the new
  file has the same `UID` and a higher `SEQUENCE`, so a calendar replaces
  the entry.
- The same output is produced in three places (web, app, and the server
  for email), so it is fixed by shared vectors in `testdata/ics/`, the
  pattern `/remind` uses.

### 6.4 Guests

A guest is a person with no account who joins one meeting by a link.

- **Link.** A host makes a guest link for a meeting, or for one
  occurrence. It carries a random code (160 bits), stored as a hash, with
  a use limit and an end: the end of the occurrence, or of the meeting's
  last occurrence. A host can revoke it.
- **Identity.** Opening the link asks for a name. The server makes an
  **ephemeral guest account**: role `guest` (the existing role),
  `guest_of` set to the meeting, no passkey, one session held in a cookie.
  The name is shown with a "guest" mark everywhere, and cannot equal a
  member's display name.
- **Lifetime.** The account lasts for that occurrence: from opening the
  link to 15 minutes after the session ends. It is then removed, as a
  removed account is today, so anything it wrote keeps an author. Next
  week's occurrence is a new guest.
- **What a guest sees.** The meeting's room and nothing else of the
  server:
  - the lobby until a host admits them; guests always pass through the
    lobby;
  - the people in their space, their media, hands and reactions;
  - messages posted in the meeting's conversation while they are
    admitted, files in those messages, polls, the notes page and the
    whiteboard. Nothing from before admission and nothing after they
    leave. This is the R87 horizon with an end as well as a start;
  - no member list beyond the room, no presence, no profiles beyond name
    and picture, no other conversation, no search, no direct chats.
- **What a guest may do.** Speak; camera if the room allows; share a
  screen only if the meeting's `guests_may_share` is on; post in the
  meeting's chat; answer polls; raise a hand. Never host, never record.
- **Limits.** `rooms.guests` switches guests on for the server (off by
  default). At most `rooms.max_guests` (20) guests in a meeting. Opening
  a link is rate-limited per address with `internal/ratelimit`; the lobby
  holds at most 50. A host can remove a guest, which also bars that
  guest's session from the link. Guests are not offered in shared
  channels' meetings on other servers, in sealed chats, or outside
  meetings.
- The guest uses the web chat in a guest shell: the room and its chat,
  nothing else.

### 6.5 Email invites

`DESIGN.md` section 1 says Churr never sends email. The roadmap adds
email invites "with the server's email set-up". The design keeps the
first as the default and adds the second as a switch:

- `-smtp` (host, port, user, password, from address), off unless set. It
  is the server's only outbound email and is listed with the other
  outbound connections in `DESIGN.md` section 17.
- It sends one kind of message: a meeting invite to a guest, with the
  guest link and the `.ics` attached, and an update or cancellation when
  the meeting changes.
- The address is kept encrypted under the server's seal key in
  `meeting_guest_links.email_enc` only so that updates can be sent, and is
  deleted when the link ends. It is never shown to anyone but the
  organiser and never used for anything else.
- Limits: 50 invites a day per organiser, 500 per server
  (`rooms.email_daily`), one message per change with changes within five
  minutes folded together.
- Without SMTP the organiser copies the guest link and sends it
  themselves. That always works.

## 7. Notes and whiteboard

**Meeting notes and agenda: a message.** The notes page is one message
in the meeting's conversation, of an ordinary kind, that the meeting
points to (`meetings.notes_message_id`):

- it uses the message format as it is: headings, lists, checklist items
  for the agenda and actions;
- every seat of the meeting's sessions may edit it, not only its author
  (a flag on the message, `shared_edit`, set only by the server);
- an edit names the version it was made from; if the page moved on, the
  server answers `conflict` and the client merges and tries again.
  `message_versions` already keeps the history;
- it is kept after the meeting like any message, follows retention, is
  searchable, and R87 hides it from later members with the rest;
- a repeating meeting has one page per occurrence, each linking to the
  one before.

This is not two people typing in one line at once; it is saving sections
in turn. Rejected: a document object with merge-as-you-type (a large new
subsystem for a secondary feature); a form (wrong shape).

**Whiteboard: a small new object.** Strokes do not fit in messages, so
the board is its own thing, kept as small as it can be:

- a board belongs to a message in the conversation (`boards.message_id`),
  which is what permission, retention and R87 act on;
- the content is a list of operations: add a stroke, a shape, a text or
  an image by id; move one; delete one; clear. Each operation stands on
  its own, so order between people does not matter and no merge logic is
  needed;
- clients with the board open (`board.open`) get `board.op` live; others
  get nothing. Operations are rate-limited per person;
- when the board is closed, or the session ends, the server writes one
  snapshot of the surviving objects (`boards.snapshot_key`) and drops the
  operation rows; the client that closes it uploads a picture of the
  board as the message's attachment, so it shows in the channel;
- limits: 5,000 objects, 2 MB a board.

Rejected: sharing a screen of an outside tool (the roadmap asks for a
board); one message per stroke (floods the log and its numbering).

**Polls** are `/poll` as it is, posted in the conversation; the room
shows the live tally from `form.answered`. Nothing new.

**Reactions over the video** are `room.react` frames, not stored.

## 8. Privacy and safety

**The private-channel contract (R87).** Everything a session leaves
behind is attached to a message: recording, replay, clip, transcript,
notes page, whiteboard, invite card. Reading any of them is authorised
through that message, with the same check that reads the message. So
when R87 lands, a member who joined after the message cannot open the
recording, play its segments, or read the notes, with no rule of its own
in rooms. In addition:

- every playlist and segment request is checked; a signed address for a
  bucket lasts 60 seconds and is for one segment;
- a live HLS feed in a private channel can be rewound only as far as the
  viewer's own join, never to before it;
- the attendance report goes to the organiser and hosts only, and is
  removed with the session's message under retention;
- a recording of a session is never made available in any other
  conversation; a clip can be posted only in the conversation of its
  source.

Stage 3 must not ship recording before R87 is in the tree.

**Recording consent.**

- While a recording, an HLS output with replays, or captions with a
  transcript is running, the session carries `recording: {on, by, since}`.
  Every client shows a marker that cannot be hidden, and plays a short
  sound when it starts.
- Someone joining a recorded session is told before any of their media is
  sent: the join answer says so, and the node drops media from a seat
  until its client has sent `acknowledged_recording`. A seat that will
  not is offered listening only.
- Starting a recording tells every seat and everyone in the lobby
  (`room.recording_changed`), and people on other servers the same way.
- Only `main` is recorded. Backstage and breakouts never are.

**Blocks (deaf ears).** The rule stays that the blocked person learns
nothing.

- The node does not forward the blocked person's audio or video to the
  blocker. The blocker's client shows their tile collapsed, as their
  messages are. Their hand, reactions and questions are collapsed for
  the blocker too.
- The blocked person receives the blocker as normal. They would notice
  silence from someone marked as speaking.
- A host who has blocked someone still sees them in the lobby and the
  hand queue, collapsed, so hosting works.
- The server gives the node the block pairs among a session's seats when
  each seat is admitted. Recordings are not affected: a recording is the
  room, not one person's view.
- Across servers the host server does not know another server's block
  lists; there the blocker's own client mutes and collapses. Section 8,
  "Linked servers".

**The lock.** A locked session leaves every room, as it leaves a call
today: the client leaves as it locks and the watch removes a seat whose
session has `LockedAt` set. The same check now also covers viewers, HLS
requests (each is authorised against a session that must not be locked)
and guests. The server tells the node to drop the seat's connections at
once rather than at the next watch.

**Sealed chats.** A sealed chat keeps exactly what it has: a two-person
`call` session with frames encrypted on the devices. It has no voice
mode, stage, meeting, stream, recording, HLS or captions, and no taps
exist for its session. Guests cannot be in one.

**End-to-end encryption for larger rooms** was weighed. The mechanism
exists: the encoded-transform interface is in Firefox, Safari and, in its
standard form, Chrome 141 and later
(<https://caniuse.com/mdn-api_rtcrtpscripttransform>,
<https://www.w3.org/TR/webrtc-encoded-transform/>); SFrame as a browser
feature is not shipped. What it would cost:

- a group key agreement Churr does not have (sealed chats are two
  people), with a new key each time someone joins or leaves;
- no recording, HLS, captions or transcoding for such a room, ever: the
  server cannot read the frames;
- no audio slots as designed, and VP8 only;
- nothing for guests in older browsers.

Recommendation: do not build it in these stages. Rooms outside sealed
chats are protected hop by hop (DTLS-SRTP), the server could read them
and does not, and the documents keep saying so plainly. It is decision
D13.

**Linked servers.** The existing rule holds for every kind of session:
it lives on the home server of whoever started it, on one of that
server's nodes; others' members join with a pass, their signalling is
relayed, their media goes straight to the host's node. By stage:

| Stage | Works across servers | Does not |
|---|---|---|
| 1 Base | Calls, exactly as today | – |
| 2 Voice and stage | Dropping in, speaking, hands, moderation by managers of any server (the host checks levels in its own copy), the roster from outside | Live speaking marks for people outside the room on other servers (they see who is in, not who is talking); moving someone to a room hosted elsewhere; server-side blocks |
| 3 Meetings | The invite card (a message), joining, lobby, breakouts, polls, the notes page | Answers from other servers' members update only on their own server until a link frame carries them (built in this stage: `meeting_answer`); guests; recordings are stored on the host and played from it with a playback pass |
| 4 Streams | Watching with low delay by pass; HLS and replays by a playback pass (a signed address from the host, asked for over the link, like a call pass) | Going live by WHIP into another server's room; clips by remote members |
| 5 Captions | Captions for remote seats in the room | Transcripts are with the recording, on the host |

As with calls, a remote member's device connects to the host's node, so
the host's operator learns its address; the client says so before
joining. A recording is never copied to other servers: it would pass
`MaxBlobBytes`, and one copy under one retention rule is the safer
contract.

## 9. Capacity

### 9.1 What is measured

One measurement exists (`BENCHMARKS.md`): 25 on audio, 8 cameras each
shown at layer `h` to everyone, 4 vCPU / 8 GB: 1.2 CPUs, 113 Mbit/s out,
0.007% audio loss, 860 MB held.

### 9.2 Estimates

Everything in this table is an **estimate** worked from that one row and
from the bit rates in `docs/CALLS.md` sections 6 and 7. None is a
promise.

Working figures: 113 Mbit/s for 1.2 CPUs is about 95 Mbit/s of forwarded
media per CPU, or roughly 12,000 packets a second per CPU at about 1,000
bytes a packet. One viewer of one 500 kbit/s picture with audio is about
110 packets a second; at 1,200 kbit/s about 180. One listener in a voice
room with one person talking is about 50.

| What | Per CPU of a node | 2 vCPU node | 4 vCPU node | 8 vCPU node |
|---|---|---|---|---|
| Listeners in voice rooms, one talking | about 240 | 300 | 700 | 1,500 |
| Low-delay viewers at 500 kbit/s | about 110 | 150 | 300 | 700 |
| Low-delay viewers at 1.2 Mbit/s | about 65 | 90 | 190 | 400 |
| Outbound traffic at those viewer counts (500 kbit/s) | 60 Mbit/s | 85 Mbit/s | 170 Mbit/s | 390 Mbit/s |
| Meetings like the measured call (25 people, 8 cameras) | – | 1 | 2 | 4 to 5 |

Columns for whole nodes leave about one CPU in four spare.

| Other costs | Estimate |
|---|---|
| Memory per seat | Unknown until stage 0 explains the 860 MB. The budget to hold afterwards: 4 MB a full seat, 2 MB a receive-only seat |
| HLS passthrough packaging | A few percent of a CPU per stream; the limit is disk or bucket writes |
| HLS viewers | Bandwidth, not processor: 1,000 viewers of a 2.5 Mbit/s stream are 2.5 Gbit/s. Thousands of viewers need a bucket or a caching proxy in front, or a small bit rate |
| Audio mix and copy (tool path, video already H.264) | Under a tenth of a CPU per output |
| One 720p30 software encode | 1 to 1.5 CPUs; 480p about half that |
| The ladder (source + 720p + 480p), software | 1.5 to 2.5 CPUs per stream |
| The same with a hardware encoder | 0.2 to 0.5 CPUs per stream, within the encoder's own session limit |
| Captions | One model in memory (0.4 to 0.9 GB) and a processor share per person speaking at once |
| Main server on 1 vCPU / 1 GB with rooms off | Nothing: no port, no process, no memory |
| Main server on 1 vCPU / 1 GB with the built-in node | Not measured. Expect small voice calls only |

### 9.3 Measurement plan

No number goes into the README, `docs/OPERATIONS.md` or an admin default
before it is measured. `tools/loadtest -call` and
`internal/calls/calltest` are extended, not replaced:

| New flag | Does |
|---|---|
| `-call-rooms N` | N sessions at once in N channels |
| `-call-audio`, `-call-video` | As today, per room |
| `-call-listeners N` | Receive-only seats in a voice or stage room |
| `-call-viewers N` | Low-delay viewers of a stream |
| `-call-hls N` | Clients that fetch the playlist and every segment |
| `-call-whip FILE` | One publisher sending a prepared H.264 and Opus file over WHIP |
| `-call-churn R` | Joins and leaves a second, to measure join time and re-offers |
| `-call-breakouts N` | Splits a meeting into N spaces and back |
| `-call-record`, `-call-ladder` | Turns on the recording and the quality ladder |
| `-heap FILE` | Saves a heap profile of the server or node at the steady point |

Each run records: node and server CPUs, memory, traffic, audio and video
loss, delay, time to join, time to first frame, HLS delay, and what a
viewer on a poor link does to the others.

Scenarios, each on 1 vCPU / 1 GB (built-in node), 2 vCPU / 4 GB,
4 vCPU / 8 GB and 8 vCPU / 16 GB, first with the node alone and then
beside 5,000 chat connections:

1. Stage 0: the measured call again, with a heap profile, before and
   after the memory work.
2. Voice: rooms of 10, 25, 50, 100 listeners with 1 and 3 talking; ten
   rooms of 10 at once; the number at which audio loss passes 1%.
3. Stage: 5 presenters, audience to the cap, then low-delay viewers in
   steps of 100 until loss or delay fails.
4. Meetings: 25 with 8 cameras, with and without recording; breakouts.
5. Streams: one OBS feed at 2.5 and 6 Mbit/s; low-delay viewers to
   failure; HLS at 500, 1,000, 2,000 and 5,000 viewers from disk and from
   a bucket; the ladder in software and on each hardware encoder to hand.
6. Captions: 1, 3 and 5 people speaking at once, for each model size
   considered.

Results go into `BENCHMARKS.md` with the same rules as its other rows,
and `docs/OPERATIONS.md` section 16 gets a media-node sizing table.

### 9.4 Admin limits and the memory guard

| Setting | Default | Meaning |
|---|---|---|
| `rooms.enabled` | `true` when a media port or node exists | Calls, as `calls.enabled` today |
| `rooms.voice`, `rooms.meetings`, `rooms.streams`, `rooms.recording`, `rooms.hls`, `rooms.transcode`, `rooms.captions`, `rooms.guests` | `false` | Each part |
| `rooms.max_participants` | 50 | Full seats in a session; a channel's cap is within it |
| `rooms.max_cameras` | 12 | |
| `rooms.max_audience` | 200 | Receive-only seats in a room, listed |
| `rooms.max_viewers` | From the node | Low-delay viewers per session; beyond it, HLS |
| `rooms.max_sessions` | From the node | Live sessions per node |
| `rooms.max_outputs`, `rooms.max_encodes` | From the node | Recordings and HLS feeds; encodes at once |
| `rooms.recording_quota_bytes` | 50 GB | The server's recording store |
| `rooms.max_guests`, `rooms.email_daily` | 20, 500 | |

"From the node" means: each node works out its own limits from its CPUs,
memory and encoders using the measured table, and the admin can lower or
raise them per node. The console shows each node's limits, its load, and
which limit refused the last join.

**Admission.** A join, a viewer, an output or an encode that would pass a
limit is refused with a reason the client can show (`room_full`,
`viewers_full` with HLS offered, `node_busy`). Nothing is admitted and
then degraded.

**The memory guard.** Each node runs `internal/memguard` against its own
limit, and the built-in node shares the server's guard:

- tight: no new sessions, viewers or outputs on that node; people already
  in stay; new viewers are sent to HLS when there is one;
- critical: the node sheds in this order: low-delay viewers (moved to HLS
  or dropped, newest first), encodes of extra qualities, recordings (with
  a notice), then audience seats. Full seats are last. On the built-in
  node, rooms shed before any chat connection is closed.

## 10. The build plan

Five stages. Each piece is sized for one builder, names what it depends
on, and ends with tests. Every stage keeps `go test ./...`, the e2e
suite and the documents (`docs/ROOMS.md`, `docs/EVENTS.md`,
`docs/API.md`, `docs/openapi.yaml`, `docs/SCHEMA.md`,
`docs/PERMISSIONS.md`, `docs/OPERATIONS.md`, `docs/FEATURES.md`) true.

Fake media everywhere: `calltest` clients in Go tests; Chrome with
`--use-fake-device-for-media-stream` and a tone file in e2e, as
`e2e/calls-web.mjs` does; a prepared H.264 and Opus file for WHIP and
packaging; a stub speech helper that returns fixed lines.

### Stage 1: base

Ships to users: calls as before, plus mute and remove by managers. Ships
to operators: media nodes.

| # | Piece | Depends on | Tests |
|---|---|---|---|
| 1.0 | Memory: profile the measured call, find where 860 MB goes, fix it, set the per-seat budget | – | `loadtest -call -heap`; a capacity test that fails above the budget |
| 1.1 | Rename and split: `internal/calls` → `internal/rooms` and `internal/rooms/node`; `Node` interface; built-in node; `call.*` → `room.*` in server, web and app; tables `rooms`, `room_sessions`, `room_seats` with the copying migration | – | All existing call tests pass under the new names; migration test on both databases |
| 1.2 | Roles, rights and spaces in the engine: seat role, per-role publish rights, forwarding only within a space, moving a seat between spaces, `lobby` seats with no media | 1.1 | Unit and `calltest`: a seat without the right cannot publish; two spaces do not hear each other; a move keeps the connection |
| 1.3 | `perm`: `join_room`, `watch_room`, `moderate_room`, `set_room`; moderation operations (mute for everyone, remove, lock); `members.room_muted` | 1.2 | Permission table tests; e2e: manager mutes and removes |
| 1.4 | Audio slots and receive-only seats | 1.2 | `calltest` with 100 listeners: no re-offer on join; the loudest five are heard; sealed and small rooms unchanged |
| 1.5 | Watching by interest: `rooms.watch`, `room.state`, `room.speakers`, `room.count_changed`, merged under pressure | 1.1 | Hub tests on the pattern of presence interest; a watcher gets no media |
| 1.6 | Media nodes: `churr-media`, node tokens, the node connection, signalling relay, placement, loss of a node, admin console page | 1.1 | Go tests with two nodes in one process; e2e with a real second process; kill a node mid-session |
| 1.7 | Taps, and the recording store (`-recordings`, disk and bucket), quotas, janitor rules | 1.2 | A tap sees every packet in order; store tests on both back ends; quota refusal |
| 1.8 | Limits, admission and the memory guard for nodes | 1.6 | Refusals by each limit; shedding order under a forced guard state |
| 1.9 | Blocks in rooms; the lock dropping seats at once | 1.2 | Blocker receives nothing from the blocked; blocked sees no difference; lock test |
| 1.10 | `loadtest` flags of section 9.3 that apply so far; first measurements | 1.4, 1.6 | The runs of scenarios 1 and 2 |

Migrations: one for rooms, sessions and seats (copying calls), one for
`media_nodes`, `recordings` and `recording_segments`, one for the member
columns.

### Stage 2: voice channels and stage

Ships: voice channels with text chat, push-to-talk, video switch, caps,
the roster and speaking marks in the sidebar, moderation, and stage mode
with presenters, audience, hands, Q&A and backstage. Large audiences use
low-delay viewers up to the node's limit; HLS for more comes in stage 4.

| # | Piece | Depends on | Tests |
|---|---|---|---|
| 2.1 | `conversations.voice`; creating a voice channel; `voice` sessions (no ring, no notice, ends when empty); join muted | 1.x | API and e2e: drop in, drop out, nobody rung |
| 2.2 | Web: the voice channel view, sidebar roster and speaking marks, push-to-talk with a chosen key, device settings | 2.1, 1.5 | e2e with fake media: two browsers, marks seen by a third outside the room |
| 2.3 | App: the same | 2.1, 1.5 | App tests; a shared e2e against the web |
| 2.4 | Room settings by managers: cap, video off, lock; move a person to another voice channel | 1.3 | e2e |
| 2.5 | Stage mode: `open` and `live` states, named presenters, audience seats, raise hand and the queue, invite up and send back, backstage | 1.2, 1.4 | `calltest`: audience cannot publish; backstage is not heard on stage; e2e of a small town hall |
| 2.6 | Q&A: questions, votes, moderation, shown on stage | 2.5 | API tests; e2e |
| 2.7 | Viewers beyond the cap on the low-delay path; the programme | 1.4, 2.5 | `calltest` with viewers; measurement scenario 3 |
| 2.8 | Linked servers: voice and stage across servers as section 8 says | 2.1, 2.5 | Link tests with two servers and a hub |

Migrations: `conversations.voice`, `room_presenters`, `room_questions`.

### Stage 3: meetings

Ships: scheduled and repeating meetings, invite cards, calendar files,
reminders, lobby, co-hosts, hands and reactions, live polls, notes,
whiteboard, breakouts, attendance, recording, and guests with optional
email. Needs R87 in the tree before 3.9.

| # | Piece | Depends on | Tests |
|---|---|---|---|
| 3.1 | `meetings`, exceptions, invitees, answers; the meeting API; `sched` meeting kind and reminders | Stage 1 | Repeat and exception tests beside `repeat_test.go`; reminder timing |
| 3.2 | The invite card in web and app; `.ics` on the device with shared vectors | 3.1 | Vector tests in Go, JavaScript and Dart; e2e: accept, change, re-add |
| 3.3 | `meeting` sessions: opening window, organiser and co-hosts, lobby policies, admit and refuse | 3.1, 1.2 | e2e: an outsider waits and is let in |
| 3.4 | Hands and reactions in meetings; live poll tally in the room | 2.5 | e2e |
| 3.5 | Breakouts: three ways to assign, timer, warning, return; hosts moving between | 1.2 | `calltest` with 40 seats in 8 spaces and back; e2e |
| 3.6 | Notes page: `shared_edit`, version check, one per occurrence | 3.1 | API conflict tests; e2e with two editors |
| 3.7 | Whiteboard: `boards`, operations, snapshot, picture on close; web and app drawing | 3.3 | Operation order tests; limits; e2e |
| 3.8 | Attendance report | 3.3 | API tests; only hosts may read |
| 3.9 | Recording: the tool path with ffmpeg (audio mix, programme video), consent marker and acknowledgement, the recording message, playback, download; `churr-media` images | 1.7, R87 | Node tests with fake media and real ffmpeg in CI; consent: media dropped until acknowledged; a later member cannot play it |
| 3.10 | Guests: links, ephemeral accounts, the guest shell, limits | 3.3, R87 | API abuse tests (rate, caps, expiry, revocation); e2e as a guest; a guest sees nothing from before admission |
| 3.11 | SMTP and email invites | 3.10 | A test SMTP server; limits; address removed at the end |
| 3.12 | Linked servers: `meeting_answer` frame, playback pass for recordings | 3.1, 3.9 | Link tests |

Migrations: meetings tables; boards; guest columns on `accounts`.

### Stage 4: streams

Ships: going live from web, app and OBS; low delay for hundreds and HLS
for thousands; live-now marker and viewer count; notification; chat
beside with slow mode; scheduled streams; replays with chat; clips;
several qualities as an admin switch.

| # | Piece | Depends on | Tests |
|---|---|---|---|
| 4.1 | `go_live`, `live_who`, `members.may_go_live`; `stream` sessions; going live from web and app; the "Live now" message, `room.live_changed`, notification to members who chose it | Stage 1, 2.7 | Permission tests; e2e |
| 4.2 | WHIP: endpoint, ingest keys, wider H.264 profiles | 4.1 | A Pion WHIP client in Go tests; a manual OBS check listed in the release notes |
| 4.3 | HLS passthrough: fMP4 segments, playlists, authorised serving from disk and bucket, viewer counting | 1.7, 4.1 | Segment and playlist tests against vectors; playback in e2e with hls.js; the Apple spike of section 3.6 |
| 4.4 | Players: hls.js in the web chat, `media_kit` in the app, switching between low delay and HLS | 4.3 | e2e; app tests |
| 4.5 | HLS through the tool path for stages (several speakers); AAC rendition | 3.9, 4.3 | Node tests with ffmpeg |
| 4.6 | Replays with chat alongside; retention; `replay_days` | 4.3 | e2e: replay shows the messages of its span; janitor tests |
| 4.7 | Clips | 4.6 | API tests; exact and boundary cuts |
| 4.8 | Qualities: the ladder, hardware detection and fallback, the nvidia image | 4.5 | Node tests with software; a hardware run on the machines to hand; measurement scenario 5 |
| 4.9 | Scheduled streams: a meeting row of kind stream with a reminder | 3.1, 4.1 | API tests |
| 4.10 | Linked servers: low delay and HLS by pass | 4.3 | Link tests |

Migrations: `ingest_keys`; room columns for replays.

### Stage 5: captions and transcripts

| # | Piece | Depends on | Tests |
|---|---|---|---|
| 5.1 | Choose the model: measure size, processor cost and accuracy for the candidates; document | – | Measurement scenario 6 |
| 5.2 | `churr-speech`, its protocol with the node, starting and stopping | 1.7 | Node tests with a stub helper |
| 5.3 | `room.caption`, captions in web and app, per-person switch | 5.2 | e2e with the stub |
| 5.4 | Transcripts beside recordings; consent marker covers them; search stays off for transcripts unless decided otherwise | 5.2, 3.9 | API and retention tests |

## 11. Risks, and what could force a redesign

1. **The unexplained memory.** If the 30 MB a seat is inherent in the
   WebRTC library's connection, not something Churr can configure away,
   low-delay viewers in the hundreds are out of reach on ordinary nodes.
   The fallback is fewer low-delay viewers per node and HLS sooner; if
   even 100 listeners in a voice room cannot be held, the engine choice
   itself is in question. This is why piece 1.0 comes first.
2. **Apple playback of Opus in HLS.** If it fails on Safari, iPhone or
   the apps' player, passthrough HLS needs an audio encode, which means
   ffmpeg for every HLS stream. The design absorbs this: the tool path
   exists. What changes is that "streams with no external program" stops
   being true.
3. **Audio slots and the apps' WebRTC library.** If `flutter_webrtc`
   cannot map a fixed set of audio lines to changing speakers cleanly, or
   plays a click at each switch, large rooms fall back to a line per
   microphone and the listener limit drops sharply.
4. **H.264 from browsers and phones.** Recording and HLS want H.264
   publishers. Devices with no usable H.264 encoder publish VP8, and each
   one costs a software encode on the node, or is left out of the
   programme. If that is common, recording cost is several times the
   estimate.
5. **ffmpeg as a live component.** A long-running child process fed live
   RTP with sources that come and go is the least proven part. If it
   cannot be made steady, the fallback is to record the programme as
   stored packets and make the watchable file after the session ends,
   which keeps recordings and loses multi-speaker HLS.
6. **The node protocol under failure.** Relayed signalling adds a hop;
   if join time through a node is poor, or the server and node disagree
   after a reconnect, sessions break. The rule "a lost node ends its
   sessions" keeps the state simple at the price of interruptions.
7. **One session, one node.** A stage with more low-delay viewers than
   one node carries has no answer but HLS. If the owner wants thousands
   at low delay, the design needs cascading between nodes, which touches
   placement, taps and the audio slots.
8. **R87 is not in the tree.** Recording, replays, notes and guests rely
   on its message-level check and on a member's horizon. If it lands in a
   different shape (for example, per conversation rather than per
   message), section 8's "authorise through the message" must be done
   again.
9. **Guests are a public door.** An unauthenticated way in, with a
   session cookie, on a server that has had none. A flaw there is a flaw
   in the whole server. It is off by default and has its own review.
10. **Email.** It reverses a stated constraint. If the owner would rather
    not, guest links copied by hand cover the need and stage 3 loses one
    piece.
11. **Bandwidth, not processor, limits streams.** Thousands of HLS
    viewers are gigabits. A server on a modest line cannot do it whatever
    the design; the documents must say so beside the switch.
12. **Size of the change to clients.** The rename, slots, spaces and
    roles touch every call screen in the web chat and the app at once in
    stage 1. A mistake there breaks calls, the one live feature that
    works today. Stage 1 ships nothing new to users until its e2e suite
    is as wide as today's.

## 12. Things to verify before building

- Where the 860 MB goes (piece 1.0).
- That a Pion `recv` connection with fixed audio lines can have its
  sources switched without a click in Chrome, Firefox, Safari and the
  apps.
- H.264 profiles each client decodes from a forwarded OBS feed.
- Opus-in-fMP4 HLS on Safari, iPhone (native and Managed Media Source)
  and `media_kit` on all five platforms.
- That the chosen fMP4 library writes what hls.js and `media_kit` play,
  with segments written to a bucket as they close.
- ffmpeg taking five continuous Opus inputs and a switched video input
  over pipes for hours without drift.
- Which hardware encoders the test machines offer, and their session
  limits.
- OBS's current WHIP behaviour (profile, key-frame interval, simulcast)
  against its release notes.

## 13. Open decisions for the owner

Each has a recommendation, marked. The options are written so they can
be asked as multiple choice.

**D1. How lobby, backstage and breakouts are modelled.**
(a) Spaces inside one session, one media connection kept across moves
(recommended). (b) Separate rooms, a rejoin on every move.
Trade-off: (a) is new forwarding rules in the engine; (b) is simpler
code and a join storm at every breakout.

**D2. What a voice channel is in the schema.**
(a) A flag on a channel, `voice`, so every channel rule applies
(recommended). (b) A third conversation `kind`.
Trade-off: (b) reads more literally as "a channel kind" and means
touching every rule that branches on kind.

**D3. Topology.**
(a) Built-in node for plain calls; separate media nodes recommended for
stages, streams, recording and captions; one session on one node
(recommended). (b) Everything inside the main server only. (c) Media
nodes required for any live room, calls included.
Trade-off: (a) is two ways to run the same engine to test; (b) puts
encoding beside chat; (c) breaks calls on a plain install.

**D4. What is on by default.**
(a) Calls on as today; voice channels, meetings, streams, recording, HLS,
several qualities, captions and guests each off until an admin turns them
on (recommended). (b) Voice channels and meetings on, the rest off.
(c) Everything off, calls included.
Trade-off: (a) hides new features until asked for; (b) shows them on
machines that were never sized for them.

**D5. External tools.**
(a) ffmpeg as a separate program on media nodes, required for recording
meetings, multi-speaker HLS, several qualities and clips cut exactly;
one-source H.264 streams packaged in pure Go without it (recommended).
(b) ffmpeg required for every stream and recording. (c) No external
program: one-source streams only, no meeting recording.
Trade-off: (a) two packaging paths; (b) one path and no streams on the
plain image; (c) drops roadmap features.

**D6. What a recording contains.**
(a) The programme: mixed audio and the speaker or shared screen, one
file (recommended). (b) A grid of everyone, composed. (c) One raw file
per person, composed later on request.
Trade-off: (b) costs several times the processor; (c) is cheap live and
gives nothing watchable until processed.

**D7. Where recordings, replays and HLS segments are stored.**
(a) A recording store of their own, a folder or an S3-compatible bucket,
with quotas and retention as the roadmap says (recommended). (b) In the
uploads store as ordinary files, as the roadmap's "like uploaded files"
reads.
Trade-off: (a) is a second store to configure and back up, and lets a
bucket serve thousands of viewers; (b) is one store and puts every
viewer's bytes through the Churr server.

**D8. Readers in a voice channel.**
(a) A reader may listen to a stage or stream and may not join an open
voice room or a meeting (recommended). (b) A reader may listen
everywhere. (c) A reader may join nothing, as with calls today.

**D9. A meeting's place.**
(a) A meeting always belongs to a conversation and uses its room;
scheduling outside a channel makes an ad-hoc channel of the invitees
(recommended). (b) Meetings are objects of their own with a room and a
chat that exist only for the meeting.
Trade-off: (b) keeps meeting chatter out of channels and needs a second
kind of chat, with its own retention and privacy rules.

**D10. Guests.**
(a) An ephemeral guest account for one occurrence, always through the
lobby, seeing only the room and what is posted while admitted, removed 15
minutes after the session; off by default; at most 20 a meeting
(recommended). (b) The same, lasting for the whole meeting series so a
weekly guest keeps a name. (c) No guests without an account.

**D11. Email.**
(a) Optional SMTP, off unless configured, used only for meeting invites
to guests; `DESIGN.md`'s "never sends email" becomes "sends none unless
the operator sets it up" (recommended). (b) No email: the organiser
copies the link.
Trade-off: (a) changes a stated constraint and stores a guest's address,
encrypted, until the link ends.

**D12. Notes and whiteboard.**
(a) Notes as a shared-edit message with version checks; whiteboard as a
small object of its own attached to a message (recommended). (b) Both as
a new document object with live merging. (c) Notes as a message; no
whiteboard until later.
Trade-off: (a) is not typing together in one line; (b) is a large new
subsystem.

**D13. End-to-end encryption beyond sealed chats.**
(a) Not in these stages; sealed chats keep their two-person encrypted
call and get no other room feature (recommended). (b) Build group frame
encryption for private rooms, with no recording, HLS or captions there.
Trade-off: (b) needs a group key agreement Churr does not have.

**D14. Recording consent.**
(a) A marker and a sound for everyone, and a joiner's media is dropped
until their client acknowledges the notice (recommended). (b) Marker and
notice only, no enforcement. (c) Each person must agree before a
recording can start.
Trade-off: (c) lets one person stop every recording.

**D15. Blocks in rooms.**
(a) The server stops sending the blocked person's media to the blocker
and nothing else changes for the blocked person (recommended). (b) The
blocker's client mutes them locally only. (c) Neither hears the other.
Trade-off: (c) tells the blocked person, against R77.

**D16. Who is told when a channel goes live.**
(a) Members whose mode is Any activity, and anyone who turned on a new
per-channel "tell me when this goes live" (recommended). (b) Everyone
not on Nothing. (c) Nobody is notified; the marker only.

**D17. Identifiers.**
(a) UUIDv7 for rooms, sessions, meetings and recordings, as every stored
thing in Churr has today; short ids inside a session; random codes for
guest links and ingest keys (recommended). (b) The same, plus a short
readable code on each meeting for join links and for reading aloud.
(c) A different id format for the new tables.
Trade-off: (b) is one more unique column and a nicer link; (c) breaks
`docs/CONVENTIONS.md`.

**D18. Hosting in shared channels.**
(a) A session lives on the server of whoever starts it, as calls do
(recommended). (b) A voice channel's sessions always live on the
channel's owning server.
Trade-off: (b) gives one stable set of limits and fails for everyone
when the owner has no media node.

**D19. Low-delay viewers beyond one node.**
(a) Past one node's limit viewers get HLS (recommended). (b) Build
cascading between nodes now.
Trade-off: (b) is a large addition before any stream exists to measure.

**D20. Web player.**
(a) Vendor hls.js as the web chat's second third-party library
(recommended). (b) Write a small player on Media Source in the project.
(c) Native HLS only, which leaves out Chrome and Firefox on desktop.

**D21. Sound from a shared screen and music-quality audio.**
(a) Stereo, higher-rate Opus accepted from WHIP publishers only; calls
and meetings stay mono voice, no screen sound (recommended). (b) Allow
screen sound and a "music" switch for presenters in every room.

**D22. Transcripts in search.**
(a) Not searchable (recommended, until captions are measured). (b) In
search, under the recording message's permissions.

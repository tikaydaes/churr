# Live rooms: design of the base infrastructure

The design behind `docs/ROADMAP-LIVE.md`: one room model, one media
architecture and one data model that carry today's calls, voice channels
with stage mode, meetings, streams and captions. It is a proposal. Nothing
here is built. Section 13 records what the owner decided (rulings R88 to
R100); section 14 lists the little that is still open. "Instance" means
one Churr installation; "the server" means its main program. Every number
about capacity is marked as measured or as an estimate.

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
- Linked instances: the call lives on the starter's instance; other
  instances' members get a pass over the link, their signalling is relayed as sealed
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
- The private-channel contract (R87, R90) is in the tree: a member of a
  private channel reads only what was posted at or after their first
  join (`members.visible_from`, `first_joins`, migration
  `0013_first_joins`); coming back within three hours keeps the earlier
  time, later starts fresh; a pinned older message can be opened from the
  pin; quoted text is visible though the quoted message is not.
- Roles (R97, R99; `docs/ROADMAP.md` section 4), being built: Owner and
  Admin of an instance; Manager, Moderator and Member of a channel, with
  Reader as a level a member may be set to; Guest. States such as
  "voiced" and "muted" are flags, not roles. An Admin is not a manager
  of channels. A group has no permissions unless a role is granted to
  it. A Guest account cannot join shared channels.
- A channel a hub owns (R95; migration `0014_hub_channels`) belongs to
  the hub, which is trusted, as are the instances it joins to the
  channel. This design adds no protection against the hub or member
  instances.
- Threads today hang off a message in a channel. R100 rules that a
  thread is always attached to something, which need not be a channel: a
  message, a card or a meeting (`docs/ROADMAP.md` section 3, "Detached
  threads and meetings"). A meeting outside a channel has a chat that
  lasts only as long as the meeting, so this design needs no lasting
  detached thread (section 6.2).

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
| **Room** | The place for live media, and its settings: mode, cap, video allowed, who may go live. A room belongs to **a conversation or a standalone meeting**, never both: one room per conversation, made the first time it is needed (for a voice channel, when the channel is made), and one room per standalone meeting | `rooms` |
| **Session** | One continuous use of a room, from the first person entering to the end. At most one live session per room. It lives on exactly one media node | `room_sessions` |
| **Space** | A group inside a session. Media is forwarded only inside a space. Every session has `main`; a session may also have `lobby`, `backstage` and breakout spaces | In memory; named on seat rows |
| **Seat** | One account, or one guest, in a session, on one device, with a role and a space | `room_seats` (also the attendance record) |
| **Track** | Microphone, camera or screen of a seat, as today | In memory |
| **Share** | What a seat is showing the room: a screen (a track), or a **document** (no track: each client draws it from the live document, section 7.3) | In memory |
| **Output** | Something the node makes from a session's media: a low-delay feed for viewers, an HLS feed, a recording, captions | `recordings` and in memory |
| **Meeting** | A schedule, an invite list and a policy (lobby, guests, co-hosts) for sessions of a room. Each occurrence has its own chat: a thread | `meetings`, `meeting_occurrences` |
| **Document** | A shared, live-edited item that stands on its own, with its own owner and access list: notes, a board, later a task board. It belongs to no conversation. It can be shared in a session (section 7) | `documents`, `document_access` |

One room has at most one live session, as a conversation has at most one
call today.

**A meeting is in a channel, or stands alone** (R100).

- **A meeting in a channel** uses that channel's room. Its invite card
  is a message in the channel, and the meeting's chat is the thread
  under that card. If a session is already live in the room when the
  meeting's time comes, the meeting **joins the running session** (R100,
  N1): the people already there stay, the meeting's hosts gain their
  roles, and the room's chat pane shows the meeting's thread.
- **A standalone meeting** is in no channel. It has a room of its own
  and a chat of its own, attached to nothing but the meeting. Access to
  both comes from the meeting: its organiser, co-hosts and invitees, and
  whoever was admitted. **That chat is ephemeral:** it exists only for
  the meeting and is deleted once the last person has left (section
  6.2). Three people can start one directly, with no schedule: "Meet
  now" with a list of people makes a standalone meeting that starts at
  once and rings them, as a call does. No group chat is made for it.
- **Today's calls** stay what they are: a call in a direct or group chat
  (an ad-hoc channel), a sealed chat or a text channel is a `call`
  session in that conversation's room, its chat is the conversation
  itself, and that conversation keeps its history as now. Nothing about
  them needs a meeting, and nothing about them is ephemeral. "Meet now" from inside a
  chat is that call; "Meet now" from a list of people who have no chat
  open, or with a schedule, a lobby or guests, is a standalone meeting.

In a channel, the notes, the recording and the attendance report attach
to the meeting's thread, and a repeating meeting gets a new thread for
each occurrence. For a standalone meeting they live elsewhere, because
its chat does not last (section 6.2).

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
| `stage` | A voice channel in stage mode | A host opens backstage or goes live | Never | None; the live dot and marker line (section 6.6) |
| `meeting` | A channel's room or a standalone meeting's room, at the meeting's time or when a host opens it | Opening | Reminders; "Meet now" rings its invitees | The invite card, in a channel; none for a standalone meeting |
| `stream` | A channel whose managers allow going live | Someone goes live | Never | None; the live dot and marker line |

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

### 2.5 How room roles come from Churr's roles

Churr's roles are Owner, Admin, Manager, Moderator, Member (with Reader
as a level) and Guest (R97, R99). Room roles are not new roles of that
kind: they are what a person is in one session, worked out by `perm`
when they enter, and changed only by a host's act. Nothing is granted
inside a room that the channel or the meeting does not allow.

**In a channel's room:**

| In the channel | `call`, `voice`, `meeting` | `stage` | `stream` |
|---|---|---|---|
| Manager | `host` | `host` | `host` |
| Moderator | `cohost` | `cohost` | `cohost` |
| Member | `speaker` | `audience` | `viewer`, or `presenter` if they may go live |
| Member set to Reader | May not join | `audience` | `viewer` |
| Guest account that is in the channel | `speaker` | `audience` | `viewer` |
| Not in the channel, public channel | Must join the channel first | The same | The same |
| The meeting's organiser, co-host | `host`, `cohost` for that meeting's session | – | – |
| Invited to the meeting, not in the channel | `speaker`, for that occurrence only (R100) | – | – |
| Meeting guest by link | `speaker`; through the lobby, or after the 5-second notice | – | – |

**In a standalone meeting's room:** the organiser is `host`, co-hosts
are `cohost`, invitees are `speaker`, meeting guests are `speaker`.
There is no Manager or Moderator, because there is no channel.

Who may do what:

| Act | Who |
|---|---|
| Start a session (call, drop into a voice channel, open a meeting they organise) | Member. A Reader may not; a Guest account may where it is in the channel |
| Moderate a room: mute for everyone, disconnect, move, lock, admit from the lobby, invite up and send back | **Moderator and above** in the channel, and the session's `host` and `cohost` |
| Configure a room: mode, cap, video, who may go live, replays, named presenters | **Manager** |
| Go live | As the room's `live_who` says: `managers`, `chosen` (a flag on the member, `may_go_live`) or `members` |
| Record | The session's `host` or `cohost`, where recording is on for the instance |
| Schedule a meeting in a channel | Member |
| Start a standalone meeting | Any account that is not a Guest |

- **Owner and Admin have no place in rooms as such.** An Admin is not a
  manager of channels (R99), so being Admin gives no role in a room. The
  admin console can end any session and remove a node; that is running
  the instance, not hosting.
- **Flags, not roles:** `room_muted` (muted for everyone, cleared only
  by someone who may moderate) and `may_go_live` are flags on a member,
  beside "voiced".
- **A role granted to a group** gives its members that role in the
  channel, and so the room role that follows from it.
- **Guest accounts** cannot join shared channels (R99), so they are in
  no room of a shared channel; meeting guests are not offered there
  either.
- Named presenters of a stage and people a host invites up are role
  changes on seats, made by a host. A room remembers its named
  presenters (`room_presenters`) so a town hall can be prepared before
  anyone is in it.

New `perm` actions replace `start_call`:

| Action | Need |
|---|---|
| `join_room` | Member; Reader for `stage` and `stream`; archived, timed-out and sealed rules as `start_call` today. For a standalone meeting: on its list, or admitted |
| `watch_room` | Can see the conversation (roster and speaking marks from outside) |
| `moderate_room` | Moderator or Manager of the channel, or the seat's role is `host` or `cohost` |
| `set_room` | Manager |
| `go_live` | Per the room's `live_who` |
| `record_room` | Seat role `host` or `cohost`, and recording switched on |
| `schedule_meeting` | Member of the channel; any non-Guest account for a standalone meeting |

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
- "Nobody can mute or remove another participant" ends: a Moderator or
  Manager can, in every kind of session, sealed chats excepted (two
  people, nobody to moderate). In a direct or group chat there is
  nobody above Member, so nobody moderates a call there, as today.
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
allocates nothing for rooms. Each part has its own switch:
`rooms.voice`, `rooms.meetings`, `rooms.streams`,
`rooms.recording`, `rooms.hls`, `rooms.transcode`, `rooms.captions`, `rooms.guests`. On a fresh install
calls, voice channels and meetings are on (`rooms.enabled`, `rooms.voice`,
`rooms.meetings`); streams, recording, HLS, several qualities, captions
and guests are off (R90).

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
every listener).

**Screen sound and music quality** (R91), in every kind of room, at the
choice of whoever is presenting:

- **Screen sound.** Someone sharing a screen may share its sound. It is a
  second audio track of that seat, source `screen_audio`, stereo.
- **Music mode.** A switch on one's own microphone: stereo, about
  128 kbit/s, no silence suppression, and the client turns off echo
  cancelling, noise suppression and automatic gain for it.
- The node registers Opus in stereo beside the mono voice setting. A
  `screen_audio` track and a microphone in music mode are never subject
  to speaker selection: each holds a slot of its own for as long as it is
  published, in addition to the five. At most two such tracks in a
  session at once; a third is refused with `music_in_use`.
- They count as part of the programme, so they are in recordings and HLS,
  mixed in stereo.

### 3.3a Large meetings

A meeting of 60 or 70 people (R90). The server's default cap stays 50;
an admin raises `rooms.max_participants` for a node that carries it.

**What each person sees.** The current speaker large, plus up to nine
tiles with video. Everyone else is shown as their picture, with no video
subscribed. So a client subscribes to at most ten cameras and a screen,
inside today's limit of 16.

**Who is in the nine**, in this order, decided by each client for itself
from what the server tells it:

1. people the viewer pinned (own pin; it affects nobody else);
2. people a host is pointing at (spotlight: `setSeat` with `spotlight`,
   sent to every seat as `room.seat_changed`; it overrides everything but
   one's own pins);
3. people with a raised hand, oldest first;
4. people by voice activity, most recent speaker first. The node ranks
   by the RFC 6464 audio level on the RTP packets, as it does today, and
   sends `room.speakers`; a client may also use its own measured levels
   to move a tile a moment sooner. A speaker already on screen keeps its
   tile, so tiles do not shuffle while two people talk in turn; someone
   who has not spoken for the longest gives up their tile first.

**Layers.** Each camera is sent in a small and a large version
(simulcast `q` and `f`; `h` as well from desktops, as today). The nine
tiles get the small one, the speaker tile the large one; the existing
picker chooses by tile size and downlink.

**Unseen cameras pause.** A camera nobody has in their ten is told so
with `room.demand_changed` and an empty list, and the client stops
encoding it; it starts again when someone shows it. This exists today
per layer; with 60 people it means that at most about fifteen cameras
are ever being encoded and sent, whatever the number switched on. The
`rooms.max_cameras` limit therefore counts cameras being sent, not
cameras switched on.

**Audio** uses the five slots (section 3.3).

**Recording and HLS** take the speaker and any screen, as the programme
does, not the nine.

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
and runs a one-second test encode with each of NVENC, QSV and VAAPI (and
VideoToolbox when the node runs directly on a Mac). Those that pass are
reported at registration and used in the order the admin sets (default:
NVENC, QSV, VAAPI); software x264 is the fallback, always present. A
failed hardware encode during a session falls back to software for that
output and is logged. Hardware encoders have session limits of their own;
the node counts them. The console shows, per node, which encoders were
found and why any was not.

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
- **Packaging.** Two images:
  - `churr`: as today, static, distroless, no external program. Built-in
    node; passthrough only.
  - `churr-media`: one image for every kind of host. A Debian base with
    an ffmpeg built with NVENC, QSV and VAAPI support together, and the
    user-space drivers for Intel and AMD. What is usable is found at
    start; with none, it encodes in software.
  What the host must provide for each:

  | Encoder | Host needs | Container needs |
  |---|---|---|
  | NVENC | An NVIDIA card and its driver; the NVIDIA container toolkit | Started with the GPU passed in (`--gpus`). The driver libraries come from the host through the toolkit, so none are in the image |
  | QSV | An Intel processor with graphics, or an Intel card; the kernel driver | `/dev/dri` passed in; the node's user in the `render` group. The image carries Intel's media driver and runtime |
  | VAAPI | Intel or AMD graphics with the kernel driver | `/dev/dri` passed in; `render` group. The image carries the Intel and Mesa drivers |
  | Software | Nothing | Nothing |

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

The owner chose this over the uploads store (R88): the bytes go to the
recording store and there is no `files` row per segment; quotas and
retention behave as the roadmap says.

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
codes stored as hashes: guest links, ingest keys, node tokens, calendar
feed addresses.

A meeting also has a **short code** (R91), for links and for "Join with
code": nine letters and digits shown in three groups (`kfw-93p-xtq`),
from an alphabet without look-alikes, unique on the server, in
`meetings.code`. It names the meeting; it grants nothing. Joining by
code goes through the same permission, lobby and guest rules as any
other way in.

Times are `bigint` milliseconds as elsewhere. Migrations are numbered
pairs for PostgreSQL and SQLite in `internal/store/migrations/`; the next
free number is taken at build time (0014 is the last today).

### 4.1 Changed

| Table | Change |
|---|---|
| `conversations` | `voice boolean NOT NULL DEFAULT false`: a voice channel. Public, knock and private apply unchanged; its `kind` stays `channel`. A third `kind`, `meeting`: the hidden, ephemeral chat of a standalone meeting's occurrence (section 6.2) |
| `members` | `may_go_live boolean`, `room_muted boolean` |
| `calls`, `call_participants` | Replaced by `room_sessions`, `room_seats`; rows copied |
| `accounts` | `guest_of uuid` (a meeting), `expires_at bigint`: a meeting guest (section 6.4) |
| `messages` | A system kind `meeting` whose thread is the occurrence's chat |
| `settings` | `calls.*` renamed `rooms.*`; new keys in section 9.4 |

A voice channel is a flag, not a third `kind`, because every channel rule
in `perm` should apply to it unchanged, and `kind` is what separates
sealed chats from channels there.

### 4.2 New

**`rooms`**: `id`, `conversation_id` (unique, null for a standalone
meeting's room), `meeting_id` (unique, null for a conversation's room;
exactly one of the two is set), `mode` (`open | stage`),
`video` (boolean), `cap` (integer, null = server maximum), `locked`,
`live_who` (`managers | chosen | members`), `keep_replays`,
`replay_days`, `quota_bytes`, `node_id` (pinned node, null),
`created_at`.

**`room_presenters`**: `room_id`, `account_id`: a stage's named
presenters.

**`room_sessions`**: `id`, `room_id`, `conversation_id` (null in a
standalone meeting's room), `kind`
(`call | voice | stage | meeting | stream`), `meeting_id`, `occurrence_at`
(which occurrence of a repeating meeting), `message_id`, `started_by`,
`started_at`, `live_at`, `ended_at`, `end_reason`, `node_id`,
`host_server_id` (another server carries it), `peak_seats`,
`peak_viewers`. Unique index on `room_id` where `ended_at` is null.

**`room_seats`**: `session_id`, `account_id`, `joined_at`, `left_at`,
`role` (the highest held), `space` (the last), `delivery` (`rtc | hls`),
`record_answer` (`none | yes | no`) and `record_answered_at`.
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

**`recordings`**: `id`, `session_id`, `conversation_id` and `message_id`
(both null for a standalone meeting's recording, which is read through
`meeting_id`), `meeting_id`, `owner_id`,
`kind` (`recording | replay | clip`), `state`
(`writing | finishing | ready | failed`), `started_at`, `ended_at`,
`duration_ms`, `bytes`, `renditions` (jsonb), `download_key`,
`transcript_key`, `poster_key`, `source_id` and `source_from_ms`,
`source_to_ms` (a clip), `started_by`, `consent`
(`all | skip`, section 8).

**`recording_segments`**: `recording_id`, `rendition`, `n`, `start_ms`,
`duration_ms`, `bytes`, `key`. Primary key (`recording_id`, `rendition`,
`n`).

**`meetings`**: `id`, `code` (unique, the short code),
`conversation_id` (the channel it is in; null for a standalone
meeting), `organiser_id`, `title`,
`description`, `starts_at`, `duration_minutes`, `time_zone`, `repeat`
(jsonb, `model.Repeat`), `repeat_until`, `lobby`
(`off | outside_invite | everyone`), `guests` (`off | lobby | direct`),
`guests_may_share`, `record` (`off | ask | auto`), `sequence` (integer,
raised on every change, for calendars), `created_at`, `cancelled_at`.

**`meeting_occurrences`**: `meeting_id`, `occurrence_at`,
`card_message_id` (the invite card, in a channel; null for a standalone
meeting), `chat_conversation_id` (a standalone meeting's ephemeral chat;
null in a channel and once deleted), `chat_delete_at`,
`notes_document_id`, `session_id`, `cancelled`, `moved_to`,
`duration_minutes`. Primary key (`meeting_id`, `occurrence_at`). A row is
made when the occurrence's card is posted, or earlier when one occurrence
is cancelled or moved; it carries the exceptions.

**`meeting_invitees`**: `meeting_id`, `account_id`, `cohost`, `answer`
(`none | yes | no | maybe`), `answered_at`; and for a repeating meeting
**`meeting_answers`** (`meeting_id`, `occurrence_at`, `account_id`,
`answer`) for one occurrence.

**`meeting_guest_links`**: `id`, `meeting_id`, `occurrence_at` (null =
every occurrence), `code_hash`, `max_uses` (optional), `uses`,
`expires_at`, `revoked_at`, `created_by`. Whether a guest waits in the lobby is the
meeting's `guests` value.

**`calendar_feeds`**: `account_id`, `code_hash`, `created_at`,
`revoked_at`: a person's subscribable calendar (section 6.3).

**`documents`**: `id`, `kind` (`notes | board`, later `tasks`,
`timeline`), `owner_id`, `title`, `created_at`, `snapshot_key`,
`snapshot_seq`, `text_key`, `bytes`, `archived_at`. No conversation.
**`document_access`**: `document_id`, `subject_kind`
(`account | group | everyone | conversation`), `subject_id`, `role`
(`editor | commenter | viewer`), `granted_by`, `granted_at`.
**`document_updates`**: `document_id`, `seq`, `account_id`, `at`,
`update` bytea (section 7).

**`recording_documents`**: `recording_id`, `share` (short id), `document_id`,
`kind`, `title`, `from_ms`, `to_ms`, `init_key`: a document shown during
a recording; its timed updates are sidecar segments in the recording
store (section 7.5). **`recording_exports`**: `id`, `recording_id`,
`state` (`queued | rendering | ready | failed`), `asked_by`, `asked_at`,
`done_at`, `download_key`, `bytes`.

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
| `startRecording`, `stopRecording` | `startRecording` takes `consent`: `all` or `skip` |
| `answerRecording` | A seat's yes or no to being recorded; may be changed |
| `listQuestions`, `askQuestion`, `voteQuestion`, `setQuestion` | Q&A |
| `listIngestKeys`, `createIngestKey`, `revokeIngestKey` | |
| `whipPublish`, `whipStop` | Section 3.6 |
| `getPlaylist`, `getSegment` | HLS, live and replay: `/rooms/{id}/hls/…`, `/recordings/{id}/hls/…` |
| `getRecording`, `deleteRecording`, `createClip` | |
| `exportRecording`, `getExport` | Ask for a video file with shared documents rendered in (section 7.5) |
| `getAttendance` | A session's seats and times, for the organiser and hosts |

**Meetings**: `createMeeting` (in a channel, or standalone with a list
of people; `now: true` is "Meet now" and rings them), `getMeeting`, `updateMeeting`,
`cancelMeeting`, `setOccurrence` (move or cancel one), `answerMeeting`,
`listMeetings` (mine, by time), `getMeetingByCode`, `createGuestLink`,
`revokeGuestLink`, `openGuestLink` and `joinAsGuest` (no account;
section 6.4), `getCalendarFeed` (the feed's address), `resetCalendarFeed`,
and `GET /calendar/{code}.ics` (no session; the code is the secret).

**Documents** (outline; section 7): `createDocument`, `getDocument`,
`listDocuments` (those I can open), `renameDocument`, `archiveDocument`,
`deleteDocument`, `getDocumentAccess`, `setDocumentAccess`,
`requestDocumentAccess`, and in a session `presentDocument`,
`setPresenting` (who leads) and `stopPresenting`.

**Admin**: `listMediaNodes`, `createMediaNode`, `drainMediaNode`,
`removeMediaNode`, `listLiveSessions`, `testRooms` (today's `testCalls`,
per node).

**Frames a client sends** (on the WebSocket, answered `room.ok` or
`room.error`): `room.join`, `room.resume`, `room.offer`, `room.answer`,
`room.candidate`, `room.subscribe`, `room.mute`, `room.restart`,
`room.leave`, `room.decline`, and new: `room.hand` (up or down),
`room.react` (a quick reaction, not stored), `room.captions` (on or off),
`rooms.watch`, `doc.open`, `doc.update`, `doc.aware` (cursor, selection,
viewport), `doc.close`.

**Events**

| To | Events |
|---|---|
| Every member of the conversation | `room.session_started`, `room.session_ended`, `room.count_changed`, `room.changed` (settings), `room.live_changed` (live now, viewer count, at most every 10 s; this drives the live dot of section 6.6) |
| Connections watching the room | `room.state`, `room.seat_joined`, `room.seat_left`, `room.seat_changed` (role, space, mute, camera, screen, hand), `room.speakers`, `room.recording_changed` |
| Seats of the session | Today's set renamed (`room.track_published`, `room.track_unpublished`, `room.mute_changed`, `room.layer_changed`, `room.demand_changed`, `room.declined`), and `room.slots_changed`, `room.reaction`, `room.space_changed` (opened, closing in 60 s, closed), `room.moved`, `room.question_changed`, `room.caption`, `room.recording_asked` (answer wanted), `room.presenting_changed` (a document shared, who leads), `room.guest_entering` (a direct-link guest, in 5 seconds) |
| Hosts | `room.lobby_changed` |
| A person being rung | `room.ring_started`, `room.ring_stopped` |
| Stored, replayed like other conversation events | `meeting.created`, `meeting.changed`, `meeting.cancelled`, `meeting.answered` (to the organiser and the answerer), `recording.changed` |
| Connections with the document open | `doc.update`, `doc.aware` |

System message kinds added: `meeting` (the invite card and thread
parent), `recording` (with the recording), `replay`. A document posted
in a conversation is a message object (a card), not a system message. `call_started` and `call_ended` stay. Going
live posts no message (section 6.6).

## 6. Scheduling, invites, calendars and guests

### 6.1 Meetings and recurrence

- A meeting is a row in `meetings`. Its repeat rule is `model.Repeat`
  (`internal/sched/repeat.go`): every day on chosen weekdays, every week,
  or every month, at a wall-clock time in a zone. That covers a weekly
  stand-up. One occurrence cancelled or moved is a row in
  `meeting_occurrences`.
- `internal/sched` gains a meeting kind in its due-item loop (`sched.go`
  `Service.Run`). For each occurrence it posts the occurrence's card
  (section 6.2) a day before, or at once if the meeting is sooner; sends
  reminders (15 minutes before and at the start, to invitees who did not
  decline); and opens the session's window. It keeps no second timer
  system.
- A meeting's session may be opened by a host from 15 minutes before the
  start. Invitees entering before a host wait in the lobby.

Rejected: storing RRULE (a much larger grammar than the product offers;
RRULE is produced for calendars from the small model).

### 6.2 The meeting's chat is a thread

Each occurrence of a meeting has its own chat, and that chat is a thread
(R89). A thread is always attached to something (R100); for a meeting it
is attached to one of two things.

- **A meeting in a channel.** The invite card is a system message of
  kind `meeting` in the channel, posted by the server for the organiser,
  and the thread hangs under it. Who can read the thread is who can read
  the channel, under its rules, plus the people below.
- **A standalone meeting** has no channel and no card message. Its
  chat is described below; the card is shown at the top of it.
- **The card**, in either place, is drawn by clients with Accept,
  Decline and Maybe, the time in the reader's zone, the short code, a
  Join button while the session is open, and "Add to calendar". Answers
  go through `answerMeeting`.
- **The room shows that thread** beside the video. What the meeting
  leaves behind is posted into it by the server: the card of the notes
  document, the recording, a line with the attendance report for hosts.
  Polls posted during the meeting land there.
- **A repeating meeting** gets a new thread for each occurrence (and, in
  a channel, a new card). Each links to the one before.
- **Invitees outside a private host channel** get that occurrence only
  (R100, N6): the room and that occurrence's thread, nothing else of the
  channel, and not the next occurrence unless invited to it. In a public
  channel an invitee who is not in it is offered to join it.
- **Retention.** A channel meeting's thread follows the channel's
  retention.
- A stage or a stream is not a meeting: its chat is the channel's own
  log.

**The chat of a standalone meeting is ephemeral.** Proposed by the
owner; the details marked * are defaults of this design until confirmed.

- It exists only for the meeting and is **deleted when the last person
  leaves**: messages, files posted in it, polls and reactions, all of
  it. Afterwards there is no history, no search and no replay of that
  chat, for anyone.
- *A grace period of 5 minutes runs from the moment the room empties,
  so a dropped connection or an accidental leave does not destroy it.
  Someone who comes back within it finds the chat as it was. Meanwhile
  it is readable only to the people it was readable to before.
- *A scheduled standalone meeting has its chat from the moment it is
  scheduled, for the invite, the answers and messages before the
  meeting. It is deleted by the same rule after the occurrence: 5
  minutes after the last person leaves, or, if nobody came, 5 minutes
  after the scheduled end.
- *A repeating standalone meeting starts each occurrence with an empty
  chat. The next occurrence's chat is made when the last one's is
  deleted.
- While it exists it is never searchable and is not in any export.
- *What is meant to outlive the meeting lives elsewhere and is not
  touched by the deletion:
  - **notes** are a document of their own (the organiser owns, co-hosts
    edit, invitees view; section 7.4);
  - **the attendance report** goes to the organiser, in their meeting
    list;
  - **a recording**, if one was made, is an item of its own: owned by
    whoever started it, viewable by the meeting's invitees and the
    people who took part, listed with the meeting, under the recording
    store's quotas and the instance's retention for recordings. It has
    **no chat replay**, because the chat is gone. The consent notice
    for such a recording says so: "The video and sound are kept. The
    chat is not."
- *The rule people are told, on the scheduling screen and in the room:
  **"To keep the conversation, hold the meeting in a channel."** A
  meeting held in a conversation keeps its thread there, as above.
- Calls in direct and group chats are sessions of those conversations
  and keep their history. Only a meeting attached to nothing but itself
  is ephemeral.

**How it is built, and what it saves.** The ephemeral chat is a hidden
conversation of a third kind, `meeting`, that belongs to the meeting's
occurrence: its members are the organiser, co-hosts and invitees, plus
whoever is admitted; it appears in no channel list, only with the
meeting. Because it does not last, meetings need **no lasting detached
thread**:

- **storage:** messages, files, polls and events use the tables and
  code conversations already have; deleting the chat is deleting one
  conversation, which already cascades;
- **access checks:** `perm` checks membership of that conversation, as
  for any other; there is no new "access from the thing a thread is
  attached to" rule for meetings to wait on;
- **retention:** none to configure. The end of the meeting is the
  retention. There is no question of what a later invitee may read of
  earlier occurrences, because nothing of them remains.

Detached threads are still wanted for task-board cards
(`docs/ROADMAP.md` section 3); meetings no longer depend on them.

### 6.3 Calendars

Churr sends no email (R88; `DESIGN.md` section 1 stands) and signs in to
no calendar service. Meetings reach a calendar in three ways (R89):

- **A personal calendar feed.** Each account has one address,
  `/calendar/{code}.ics`, carrying every meeting it is invited to and has
  not declined, with changes and cancellations. The code is random (160
  bits), stored as a hash, shown in settings with "Copy" and "Reset".
  Any calendar program that subscribes to an address shows the meetings
  and picks up changes when it next fetches. The feed always carries the
  real titles (R100, N4), with times and join links, so its address is a
  secret, and the settings page says so; resetting it ends the old one.
  It is served with no session and rate-limited per address.
- **The app adds to the device's calendar.** On phones the app asks for
  calendar permission once and writes the event itself, and updates or
  removes it when the meeting changes. On desktop it opens the `.ics`
  file with the system's calendar.
- **An `.ics` file**, made on the device from the meeting: `UID` is the
  meeting's id at the server's host name, `SEQUENCE` is
  `meetings.sequence`, `RRULE` comes from the repeat rule (`day` →
  `FREQ=WEEKLY;BYDAY=…`, `week` → `FREQ=WEEKLY`, `month` →
  `FREQ=MONTHLY;BYMONTHDAY=n`, or `-1` when the anchor day is past the
  month's end), `EXDATE` and `RECURRENCE-ID` from the moved and cancelled
  occurrences, and the join address in `URL` and `LOCATION`. When the
  meeting changes the card shows "Changed: add again"; the same `UID`
  with a higher `SEQUENCE` replaces the entry.

The same calendar text is produced by the server (the feed), the web
chat and the app, so it is fixed by shared vectors in `testdata/ics/`,
the pattern `/remind` uses.

A guest gets the meeting by the link the organiser sends them however
they like; the guest page offers the `.ics` file.

### 6.4 Guests

A guest is a person with no account who joins one meeting by a link
(R89, R90).

- **Link.** A host makes a guest link for a meeting, or for one
  occurrence. It carries a random code (160 bits), stored as a hash, with
  an end: the end of the occurrence, or of the meeting's last
  occurrence. A use limit is optional. A host can revoke it.
- **Link kind.** The meeting's configuration chooses: `guests: lobby`
  (a guest waits until a host lets them in) or `guests: direct` (a guest
  with the link comes in). `off` allows none.
- **A direct guest is announced** (R100, N5). When a guest opens a
  direct link and gives a name, everyone in the meeting sees "<name>
  (guest) is entering in 5 seconds" (`room.guest_entering`), and the
  guest's entry is held for those five seconds. A host can refuse in
  that time. Nobody is in the room: the guest enters and waits as anyone
  early does.
- **Identity.** Opening the link asks for a name and offers a picture.
  The guest's browser remembers both and offers them again next time, for
  this server; the guest can change them at any time. Nothing about a
  guest is kept on the server between meetings. A guest is marked
  "guest" everywhere their name appears. There is no rule against a name
  a member has: names are not unique for members either, and the mark is
  what tells them apart.
- **On the server** a guest is a guest account for that occurrence: role
  `guest` (the existing role), `guest_of` set to the meeting, no passkey,
  one session held in a cookie. It lasts from opening the link to 15
  minutes after the session ends and is then removed, as a removed
  account is today, so anything it wrote keeps an author.
- **What a guest sees.** The meeting's room and its thread, and nothing
  else of the server:
  - the lobby until admitted, when the link is of that kind;
  - the people in their space, their media, hands and reactions;
  - messages posted in the meeting's thread while they are admitted,
    files in those messages, polls, and documents shared in the room
    while they are shared, to watch (section 7.3).
    Nothing from before admission and nothing after they leave;
  - no member list beyond the room, no presence, no profiles beyond name
    and picture, no other conversation or thread, no search, no direct
    chats.
- **What a guest may do.** Speak; camera if the room allows; share a
  screen only if the meeting's `guests_may_share` is on; post in the
  thread; answer polls; raise a hand. A guest only watches a shared
  document. Never host, never record.
- **Limits.** `rooms.guests` switches guests on for the server (off on a
  fresh install). There is no separate cap on guests: each counts toward
  the room's size like anyone else. Opening a link is rate-limited per
  address with `internal/ratelimit`; the lobby holds at most 50. A host
  can remove a guest, which also bars that guest's session from the link.
  Guests are not offered in meetings of a shared channel (a Guest cannot
  join shared channels, R99), in sealed chats, or outside meetings.
- The guest uses the web chat in a guest shell: the room and its thread,
  nothing else.

### 6.5 Joining with a code, and links to meetings

- "Join with code" takes a meeting's short code and leads to the same
  place as its link. A member is checked as usual; someone with no
  session is asked for a guest link, because a code alone admits nobody.
- A link to a meeting pasted in any conversation is recognised and shown
  as a preview: title and time, and **"in progress"** while its session
  is live (R90). The preview shows only what the reader may see of the
  meeting; to someone who may not see it, it is a plain link. The state
  is fetched when the message is drawn and kept current by
  `room.session_started` and `room.session_ended` for readers who are
  members of the host conversation.

### 6.6 Going live: a dot, not a notification

Going live notifies nobody (R90).

- A channel with a live stage or stream shows a **red, blinking record
  dot** in the sidebar, with the word "live" beside it for screen
  readers and for the rule that colour never works alone. Under reduced
  motion it is steady. Red is a deliberate exception to the style
  guide's four colours, for this mark only; `docs/STYLE.md` gets the
  token and the exception written down.
- In the channel's chat a **marker line** ("Live now", who, since when,
  viewers, Watch) sits at the bottom of the log while the session is
  live. It is drawn by the client from the room's state; it is not a
  message, has no place in the numbering, and is gone when the session
  ends. What stays afterwards is the replay message, if replays are kept.
- Both come from `room.live_changed`, which every member's connection
  already receives.
- A scheduled stream still sends its reminder to people who asked for
  one on its card.

## 7. Shared documents

Meeting notes are edited live by several people at once, and the
whiteboard is a system of its own in the manner of Miro or FigJam:
boards exist outside meetings and are edited live, in a meeting or not.
Task boards (Kanban), timelines and other kinds follow (R91). So these
are kinds on one **shared documents** layer, and by the owner's ruling
**a document is its own separate thing**: it does not belong to a
channel, and it has its own permissions.

This section is an outline. **Shared documents get their own full design
before anything is built**; boards in particular are not designed here.
What this design fixes is the part rooms depend on.

### 7.1 The layer

- **A document** is a row in `documents`: a kind, a title and an owner.
  It is listed under "Documents" for everyone who has access to it, not
  under any channel.
- **Its own access list** (`document_access`). Each entry gives a role
  to a person, a group, or "everyone on this instance":

  | Role | May |
  |---|---|
  | `owner` | Everything: edit, change the access list, hand over ownership, archive, delete. One owner |
  | `editor` | Edit and comment |
  | `commenter` | Read and comment |
  | `viewer` | Read |

  Nothing else grants access: not having been shown the document once,
  and not a link. There is no link sharing (R100, N11). A channel's
  members gain access only as section 7.2 says.
- **A kind registry.** Each kind registers a name, a schema version, an
  editor and viewer in the web chat and in the app, what "viewport" and
  "pointer" mean for it, a way to render a still picture and plain text
  of itself, and its limits. The server knows a kind only by name and
  limits, and the room code knows only "a document share": an id, a kind
  name, a presenter and a mode. So a new kind is added by registering it
  in the clients and naming it on the server, with no change to rooms.
  A client that does not know a kind shows its still picture.
- **Kinds.** `notes` first: a shared markdown editor. It uses Churr's
  own markdown grammar and message objects (`docs/MESSAGE-FORMAT.md`,
  `internal/markdown`) wherever they fit: the same headings, lists, code,
  mentions, checklist items and recognised objects, drawn by the same
  code as messages. Then `board` (the whiteboard), `tasks` (Kanban),
  `timeline`, and others as they come.
- **Live co-editing by CRDT.** Edits merge without a central lock, so two
  people typing in one line both keep their words, and someone who was
  offline merges when they return.
  - Recommended: **the Yjs document format**. It is the most widely
    implemented: Yjs itself in the web chat (MIT, vendored), the Rust
    implementation `yrs` behind a Dart binding in the app (the app may
    contain native code; the Dart package `y_crdt` wraps it), and a
    pure-Go implementation of the same format, `reearth/ygo`, for the
    server (<https://docs.yjs.dev/ecosystem/ports-to-other-languages>,
    <https://pub.dev/documentation/y_crdt>,
    <https://github.com/reearth/ygo>).
  - The server does not need to understand documents to carry them: it
    stores each update as bytes in order (`document_updates`), sends it
    to the connections that have the document open, and gives a late
    opener the snapshot plus the updates after it. It uses the Go
    implementation only to fold old updates into a snapshot and to pull
    out plain text. If that library proves unfit, clients make snapshots
    and the text and the server stays a carrier; the format does not
    change.
  - Rejected: Automerge (no pure-Go implementation; the app and web both
    need its native core); a CRDT written in the project three times
    with shared vectors, as sealed-chat encryption is (rich text and
    boards are far larger than a cipher; the risk is not worth it);
    operational transformation through the server (needs the server to
    understand every kind).
  - To verify in the documents design: that the three implementations
    read each other's updates for the types notes and boards need, and
    the size of the app's native library on five platforms.
- **Frames.** `doc.open` (with the state the client holds), `doc.update`,
  `doc.aware` (cursors, selections, the viewport; never stored),
  `doc.close`. By interest: only connections with the document open get
  anything. Every `doc.update` is checked against the sender's role on
  the document. Updates are rate- and size-limited per person.
- **Retention and quota** (R100, N14). A document lives until its owner
  deletes it; it is not under any channel's retention. Documents count
  toward a per-person and a per-instance quota, set by the admin. When
  an account is removed, its documents pass to an instance Owner, who
  hands them on or deletes them. The documents design settles the
  numbers.
- **Search** finds a document by its title and plain text, for those on
  its access list only.

### 7.2 A document in a channel is a card

Posting a document in a conversation posts a **link to it**, a message
object like a message link:

- someone the document's own access list allows sees a card: title,
  kind, a still picture, who is in it now, Open;
- anyone else sees "A document you can't see", with a way to ask its
  owner for access;
- **a card posted by the document's owner or an editor gives the channel
  view access** (R100, N12): an entry on the access list for that
  conversation at role `viewer`, made automatically with the post, and
  the poster is told so before sending. It follows the channel's
  membership as it stands: someone who joins gains it, someone who
  leaves loses it. The owner can remove or raise the entry on the access
  list like any other; deleting the card does not remove it;
- **a card posted by anyone else grants nothing.** A viewer or commenter
  who posts a card posts a pointer; the channel's members see "A
  document you can't see" unless they are on the list already.
- **The private-channel contract applies to the card, not to the
  document.** The card is a message: a newcomer to a private channel
  does not see an old card, sees it if it is pinned, and sees its quoted
  text when quoted. The document behind it is governed only by its own
  access list. So a document shared into a private channel can still be
  opened by everyone on its list, inside the channel or not; and a
  newcomer, who gains the channel's view access by joining, can open the
  document from "Documents" though they cannot see the old card.

### 7.3 Sharing a document in a room

In a room a presenter can **share a document instead of a screen**. The
document itself is what the room shows, live; it is not a video of
someone editing it. In the room model a document is a share source
beside camera and screen. It works in a meeting, a voice channel or a
stage; outside a session people simply open the same document together.

- **No video is encoded.** `presentDocument` adds a share to the
  session: document id, kind, presenter. Each client in the space draws
  the document locally from the live state. Text is sharp at any size,
  it costs the node nothing, and a viewer on a poor link still sees the
  document exactly.
- **Who may share** (R100, N2): a seat with the right to share a screen,
  whose role on the document is `owner` or `editor`.
- **Sharing changes nobody's access, and gives nobody rights for the
  session** (R100, N10). There are no session grants.
  - People in the room who have a role on the document use it as they
    would anywhere: an editor edits, a commenter comments.
  - People in the room without access **watch only**: they are sent the
    document's state and updates while it is shared, read-only, and
    nothing once the share stops or they leave the space. They cannot
    open it afterwards, find it, or see its history.
  - To let someone in the room edit, the owner puts them on the access
    list, as they would outside a room.
  - Before sharing, the presenter is told how many people in the room
    are not on the document's list.
- **Follow the presenter.** The presenter's viewport and pointer travel
  as `doc.aware`. Everyone follows by default; anyone may break away to
  look around and return with one press. The presenter may hand the lead
  to another seat who may share the document.
- **Several presenters.** Several documents may be shared in a space at
  once (at most four), each by its own presenter, beside one screen
  share. The client lays them out as it lays out screens, and a host may
  spotlight one. A host may stop any share.
- **Guests, invited outsiders, Readers and viewers** watch a shared
  document like anyone else without access. Low-delay viewers get it
  live; HLS viewers get it with the stream (section 7.5).

### 7.4 Meeting notes

A `notes` document is made for each occurrence of a meeting
(`meeting_occurrences.notes_document_id`), seeded with the agenda, its
card posted in the meeting's thread, and shared in the room by default
when the meeting opens.

Its access list (R100, N13): the **organiser owns it, co-hosts are
editors, invitees are viewers**. The organiser can raise anyone. A guest
watches it in the room and has nothing afterwards.

### 7.5 A shared document in recordings, streams and downloads

Decided (R100, N9): **replay in Churr**, from the document's own
updates, with an on-demand export for a video file.

**In a recording.** The recording stores a **document track** for each
share: the document's state when the share began, every update after
it, and the leader's viewport and pointer, each stamped with the
recording's clock.

- The server, which carries the updates, writes them. The node tells it
  how the recording's clock maps to segments, and the server cuts the
  track into **sidecar segments** that line up with the media segments:
  for media segment `n` there is `doc-<share>-<n>` holding the updates
  and viewport changes of those two seconds, and one `doc-<share>-init`
  with the starting state. They sit beside the media segments in the
  recording store, are listed in a sidecar playlist the server writes
  (`documents.json`: shares, kinds, titles, spans, segment names), and
  are read under the same check as the media.
- Churr's replay player, in the web chat and the app, fetches the
  sidecar with the video and draws the document in step with the sound:
  sharp, and the viewer may break away from the leader's view as in the
  room. Seeking loads `init` and the segments up to that point; the
  server writes a fresh full state every five minutes so a seek never
  replays more than that.
- Sharing a document in a recorded session puts it in the recording. A
  recording is read under its own message's permission, not the
  document's access list, so the presenter is told this when they share
  into a recorded session, and when a recording starts over their share.
- Consent: the track records the document, not people. The cursors and
  names of seats that did not accept recording are left out of it.
- The track follows the recording's retention and counts toward its
  quota.

**In a live stream.** An HLS viewer in Churr's own player can switch to
the document view too. They are several seconds behind the room, so
they are not sent the live `doc.*` frames; they read the **same sidecar
segments**, written as the stream goes and published with the media
segments they belong to. The document is therefore delayed exactly with
the stream, and a viewer never sees an edit before hearing it described.
Low-delay viewers are in real time and get the live frames. One
mechanism serves the live stream and the replay.

**Players outside Churr** (a plain HLS player, the MP4 before export)
see a plain frame where the document was: "Document: <title>".

**Download with the document rendered in.** When someone needs a video
file that holds the document, they ask for an **export**
(`exportRecording`):

- an **export renderer** on a media node loads Churr's replay of the
  document track in a headless browser, captures it as video, and ffmpeg
  splices those spans into the file in place of the plain frames, with
  the recording's sound;
- it is an **optional component**: a headless browser installed beside
  `churr-media` (in the image as an extra tag, `churr-media:export`, or
  on a plain machine found on `PATH`), reported by the node at
  registration. It runs **only for exports, never live**. A node without
  it is unchanged; an instance with no such node offers the download
  with the plain frame and says why;
- **queue and limits:** exports are jobs in a queue (`recording_exports`),
  one at a time per node by default (`rooms.max_exports`), at low
  priority so they never take a CPU a live session wants; who may ask is
  whoever may download the recording; at most three waiting per person
  and twenty per instance; a job that runs longer than four times the
  recording's length is stopped;
- **the result is kept:** the document track never changes, so an export
  is made once per recording and then served as its download. It counts
  toward the recording quota and goes with the recording under
  retention;
- **cost (estimate, to be measured):** capturing is about real time, so
  an hour of shared document takes up to an hour, with one to two CPUs
  and about a gigabyte of memory for the browser while it runs. Spans
  with no document are copied, not rendered. The headless browser is a
  large dependency (several hundred megabytes in the image) with its own
  security updates, which is why it is optional and off the live path.
- Every kind must be drawable by the replay page for this to work, which
  the kind registry already requires for replay.

### 7.6 Linked instances

Outline only; the documents design settles it.

- A document lives on its owner's home instance. It is not copied to
  other instances and is not part of any shared channel's stream.
- A card posted in a shared channel crosses as a message. On another
  instance it shows as "a document on <instance>"; the preview is shown
  only to people the home instance says may see it. The automatic view
  access of section 7.2 for a shared channel's members on other
  instances waits for the frames below.
- Putting a person from another instance on an access list, and their
  live editing, need frames between the two instances of the kind
  `call_signal` is, relayed by the person's own instance. That is new
  link protocol and is not in stages 1 to 5.
- Sharing a document in a room that has seats from other instances:
  they watch, and the document's updates travel to them with the room's
  relayed signalling from the instance hosting the session. If the
  document lives on a third instance, it cannot be shared there until
  the frames above exist.
- In a channel a hub owns, the hub and the instances it joined are
  trusted (R95); nothing here is hidden from them.

**Polls** are `/poll` as it is, posted in the meeting's thread or the
channel; the room shows the live tally from `form.answered`. **Reactions
over the video** are `room.react` frames, not stored.

## 8. Privacy and safety

**The private-channel contract (R87, R90).** Everything a session
leaves behind in the conversation is attached to a message: recording,
replay, clip, transcript, the invite card and its thread, the card of
the notes document. Reading
any of them is authorised through that message, with the same check that
reads the message. So the contract holds for them with no rule of its
own in rooms:

- a member who joined a private channel after the message cannot open
  the recording, play its segments, or read the transcript;
- someone who left and rejoins within 3 hours keeps what they could see
  before, recordings included; after 3 hours they start fresh;
- a **pinned** recording or replay message can be opened from the pin
  by a newcomer, and then played in full, as a pinned older message can;
- a message that **quotes** an older recording or document message shows
  the quoted text (its title and line) and does not open the thing
  itself;
- a meeting's thread follows its parent card: a newcomer who cannot see
  the card cannot see the thread.

**Documents** are not conversation content. A document has its own
access list (section 7.1), and the contract applies only to the card
that links to it, which is a message (section 7.2). A document shared
into a private channel is still governed by its own list; a document
shown in a room is watched, by those without access, only while it is
shared (section 7.3). A recording of a session where a document was
shared holds that document's track (section 7.5), and the track is then
under the recording's rule above.

**A standalone meeting** has no channel, so the private-channel contract
has nothing to act on. Its chat is read by the meeting's organiser,
co-hosts and invitees and by those admitted, and is deleted when the
meeting empties (section 6.2). Its recording is read through the
meeting, not a message: by its owner (whoever started it), the invitees
and the people who took part. Its attendance report is the organiser's.

**Channels a hub owns.** The hub owns the channel and is trusted, with
the instances it joins (R95). A session in such a channel is hosted as
any shared channel's is, and no protection against the hub or a member
instance is designed. A private channel stays private there; rooms add
no way to list or open it.

In addition:

- every playlist and segment request is checked; a signed address for a
  bucket lasts 60 seconds and is for one segment;
- a live HLS feed in a private channel can be rewound only as far as the
  viewer's own join, never to before it;
- the attendance report goes to the organiser and hosts only, and is
  removed with the meeting's card under retention;
- a recording of a session is never made available in any other
  conversation; a clip can be posted only in the conversation of its
  source.

**Recording consent** (R88, R89). A marker, an answer from each person,
and a choice by whoever starts the recording.

- While a recording, an HLS output with replays, or captions with a
  transcript is running, the session carries `recording: {on, by, since,
  consent}`. Every client shows a marker that cannot be hidden, and
  plays a short sound when it starts.
- **Each seat answers** yes or no (`answerRecording`), asked when the
  recording is requested and, for someone joining later, before any of
  their media is sent. An answer can be changed at any time; the change
  applies from that moment on, never to what was already recorded.
- **Whoever starts the recording picks one of two modes:**
  - **`all`: fail unless everyone accepts.** Every seat in `main` is
    asked; the recording starts only when all have said yes, and the
    starter sees who has not. A no, or no answer within 60 seconds, and
    it does not start. Someone joining later must say yes before their
    media is sent to anyone; if they say no they may listen without
    publishing, or leave. If someone already in changes to no, the
    recording stops, with a notice.
  - **`skip`: leave out those who do not accept.** The recording starts
    at once. A seat that has not said yes is left out of it: their
    microphone, camera, screen and screen sound never reach the
    recording tap. When such a person is the speaker, the recording
    shows a frame reading "<name> is speaking (not recorded)" in place
    of the picture, and silence in place of the voice. They are left out
    of captions and transcripts as well. In the room itself nothing
    changes for them: everyone present hears and sees them.
- **Late joiners count as not accepting until they answer**, in both
  modes.
- The node enforces this, not the clients: the recording tap is given
  only the tracks of seats whose answer is yes, and the frame is drawn
  by the tool path from the seat's name. So `skip` needs ffmpeg, which a
  multi-person recording needs anyway. A one-source stream has only its
  publisher to ask.
- Starting a recording tells every seat and everyone in the lobby
  (`room.recording_changed`, `room.recording_asked`), and people on
  other instances the same way.
- Only `main` is recorded. Backstage and breakouts never are.
- Answers are kept on the seat rows (`record_answer`), so there is a
  record of who agreed.

**Blocks (deaf ears)** (R91). The rule stays that the blocked person
learns nothing, and the whole of it is on the blocker's device:

- the blocker's client mutes the blocked person's audio and collapses
  their tile by default, as their messages are collapsed. The blocker
  can unmute them by hand, for that session;
- their hand, reactions and questions are shown collapsed to the
  blocker; a host who has blocked someone still sees them in the lobby
  and the hand queue, so hosting works;
- nothing changes on the server or for the blocked person: their media
  is forwarded as to anyone, they are not told, and they receive the
  blocker as normal;
- this works the same across linked instances, since the client already
  knows its own account's blocks. A call the blocked person starts still
  does not ring the blocker, as today.

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

**No end-to-end encryption for rooms** (R91). Sealed chats keep their
two-person encrypted call; no other room is encrypted end to end. Rooms
are protected hop by hop (DTLS-SRTP): the server and its nodes could read
the media and do not, except where a recording, an HLS feed or captions
were switched on, and the documents say so plainly. This is what lets
recording, HLS, captions, audio slots and transcoding exist at all.

**Linked instances.** The existing rule holds for every kind of session:
it lives on the home instance of whoever started it, on one of that
instance's nodes; others' members join with a pass, their signalling is
relayed, their media goes straight to the host's node. By stage:

| Stage | Works across instances | Does not |
|---|---|---|
| 1 Base | Calls, exactly as today | – |
| 2 Voice and stage | Dropping in, speaking, hands, moderation by Moderators and Managers of any instance (the host checks levels in its own copy), the roster from outside, blocks (they are on the device) | Live speaking marks for people outside the room on other instances (they see who is in, not who is talking); moving someone to a room hosted elsewhere |
| 3 Meetings | The invite card and its thread (messages), joining, lobby, breakouts, polls, recording consent | Answers from other instances' members, carried by a link frame built in this stage (`meeting_answer`); guests; recordings are stored on the host and played from it with a playback pass; shared documents across instances wait for the documents design |
| 4 Streams | Watching with low delay by pass; HLS and replays by a playback pass (a signed address from the host, asked for over the link, like a call pass) | Going live by WHIP into another server's room; clips by remote members |
| 5 Captions | Captions for remote seats in the room | Transcripts are with the recording, on the host |

As with calls, a remote member's device connects to the host's node, so
the host's operator learns its address; the client says so before
joining. A recording is never copied to other instances: it would pass
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
| A large meeting: 70 people, each shown one large and nine small tiles (about 2.5 Mbit/s each, 180 Mbit/s out) | about 2 CPUs each | – | 1 | 2 to 3 |

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
4. Meetings: 25 with 8 cameras, with and without recording; breakouts;
   70 people with 30 cameras switched on, each client showing one large
   and nine small tiles that change with the speaker.
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
| `rooms.voice`, `rooms.meetings` | `true` | Voice channels; meetings |
| `rooms.streams`, `rooms.recording`, `rooms.hls`, `rooms.transcode`, `rooms.captions`, `rooms.guests` | `false` | Each part |
| `rooms.max_participants` | 50 | Full seats in a session, guests included; a channel's cap is within it. Raised by the admin for large meetings |
| `rooms.max_cameras` | 12 | Cameras being sent at once (section 3.3a) |
| `rooms.max_audience` | 200 | Receive-only seats in a room, listed |
| `rooms.max_viewers` | From the node | Low-delay viewers per session; beyond it, HLS |
| `rooms.max_sessions` | From the node | Live sessions per node |
| `rooms.max_outputs`, `rooms.max_encodes` | From the node | Recordings and HLS feeds; encodes at once |
| `rooms.recording_quota_bytes` | 50 GB | The server's recording store |

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

Five stages for rooms, and one for shared documents that runs beside
them. Each piece is sized for one builder, names what it depends
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
| 1.3 | `perm`: `join_room`, `watch_room`, `moderate_room` (Moderator and above), `set_room` (Manager), on the role model of R97; moderation operations (mute for everyone, remove, lock); the `room_muted` flag | 1.2, the role model on main | Permission table tests per role; e2e: a Moderator mutes and removes, a Member cannot, an Admin who is not in the channel cannot |
| 1.4 | Audio slots and receive-only seats | 1.2 | `calltest` with 100 listeners: no re-offer on join; the loudest five are heard; sealed and small rooms unchanged |
| 1.5 | Watching by interest: `rooms.watch`, `room.state`, `room.speakers`, `room.count_changed`, merged under pressure | 1.1 | Hub tests on the pattern of presence interest; a watcher gets no media |
| 1.6 | Media nodes: `churr-media`, node tokens, the node connection, signalling relay, placement, loss of a node, admin console page | 1.1 | Go tests with two nodes in one process; e2e with a real second process; kill a node mid-session |
| 1.7 | Taps, and the recording store (`-recordings`, disk and bucket), quotas, janitor rules | 1.2 | A tap sees every packet in order; store tests on both back ends; quota refusal |
| 1.8 | Limits, admission and the memory guard for nodes | 1.6 | Refusals by each limit; shedding order under a forced guard state |
| 1.9 | The lock dropping seats at once. Blocks in rooms, in the web chat and the app: muted and collapsed on the blocker's device, unmute by hand | 1.2 | Lock test; client tests: the blocked person is muted for the blocker only, and no frame to the server differs |
| 1.10 | `loadtest` flags of section 9.3 that apply so far; first measurements | 1.4, 1.6 | The runs of scenarios 1 and 2 |

Migrations: one for rooms, sessions and seats (copying calls), one for
`media_nodes`, `recordings` and `recording_segments`, one for the member
columns.

### Stage 2: voice channels and stage

Ships: voice channels with text chat, push-to-talk, video switch, caps,
the roster and speaking marks in the sidebar, moderation, screen sound
and music mode, and stage mode with presenters, audience, hands, Q&A and
backstage. Large audiences use
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
| 2.8 | Linked instances: voice and stage across instances as section 8 says | 2.1, 2.5 | Link tests with two instances and a hub |
| 2.9 | Screen sound and music mode: stereo Opus, `screen_audio` tracks, slots of their own, the switches in web and app | 1.4 | `calltest`: a music track is always forwarded and never displaced by speakers; e2e |

Migrations: `conversations.voice`, `room_presenters`, `room_questions`.

### Stage D: shared documents

Runs beside stages 2 and 3. It has its own full design first (section
7); the pieces below are the outline that design will refine. Stage 3's
notes need D.1 to D.4.

Ships: documents in channels (notes first, then boards), edited live by
several people, and presenting a document in a room.

| # | Piece | Depends on | Tests |
|---|---|---|---|
| D.0 | The documents design: ownership and access lists, kinds, the Yjs format check across web, app and Go, quotas, boards in depth, linked servers. Owner approves | – | A spike: the three implementations exchange updates |
| D.1 | The layer on the server: `documents`, `document_access`, `document_updates`, the kind registry, `doc.*` frames by interest with the role checked on every update, snapshots, plain text, quotas | D.0 | Store tests on both databases; two `testkit` clients converge; an update from a viewer is refused; nobody off the list can open |
| D.1a | The access list in web and app; "Documents"; the card in a conversation, automatic view access for the channel when the poster is owner or editor, "a document you can't see", asking for access; documents of a removed account passing to an Owner | D.1 | e2e: an editor's card opens for the channel, a viewer's does not; a newcomer gains access by joining and loses it by leaving; the private-channel rule hides an old card and not the document |
| D.2 | Notes in the web chat: the editor, cursors, offline merge | D.1 | e2e with two browsers typing in one line |
| D.3 | Notes in the app | D.1 | App tests; e2e against the web |
| D.4 | Sharing a document in a room: the document share source, owner and editors present, watch-only for people without access, follow and break away, several presenters | D.1, 1.2 | e2e: followers' views move; someone off the list watches only while it is shared and cannot open it afterwards; their edit is refused; no video track is published |
| D.4a | The document track: sidecar segments aligned to media segments, the sidecar playlist, the replay player drawing it in web and app, the plain frame for other players | D.4, 3.10 | A replay shows the document in step with the audio; seeking lands on the right state; a seat that refused recording has no cursor in it |
| D.4b | The document view for HLS viewers in Churr's player, from the same sidecar, delayed with the stream | D.4a, 4.3 | An HLS viewer sees an edit no earlier than its media segment |
| D.4c | Export: the optional headless renderer on a media node, the export queue and limits, splicing into the download, the kept result | D.4a | A node without the renderer offers the plain-frame download; an export job renders a prepared track and the file holds it; queue limits |
| D.5 | Boards (their own pieces, set by D.0) | D.1 | – |
| D.6 | Task boards, later | D.1 | – |

Migrations: `documents`, `document_access`, `document_updates`.

### Stage 3: meetings

Ships: scheduled and repeating meetings with a short code, invite cards
with a thread for each occurrence, calendars (feed, device calendar,
`.ics`), reminders, lobby, co-hosts, hands and reactions, live polls,
live notes, the large-meeting view, breakouts, attendance, recording
with consent, and guests. Needs stage D up to D.4 before 3.8.

| # | Piece | Depends on | Tests |
|---|---|---|---|
| 3.0 | The ephemeral chat of a standalone meeting: conversation kind `meeting`, hidden, members from the meeting, never searched or exported; deletion 5 minutes after the room empties (or after the scheduled end if nobody came); a fresh one per occurrence | Stage 1 | Store tests: deletion removes messages, files and polls; a return within the grace period finds the chat; search and export never see it; a non-participant cannot read it at any time |
| 3.1 | `meetings`, `meeting_occurrences`, invitees, answers, short codes; rooms that belong to a meeting; the meeting API; `sched` meeting kind, cards and threads per occurrence, reminders | Stage 1 | Repeat and exception tests beside `repeat_test.go`; reminder timing; code uniqueness; a room has a conversation or a meeting, never both |
| 3.2 | The invite card and its thread in web and app, in a channel and standalone; the room showing the thread as its chat; "Meet now" with a list of people, ringing them; a meeting joining a session already live in its channel | 3.0, 3.1 | e2e: accept, change; chat in the room lands in the thread; a second occurrence gets its own; three people meet with no channel, no group chat is made, and their chat is gone 5 minutes after they leave |
| 3.3 | Calendars: the personal feed, `.ics` on the device, the app writing to the device's calendar; shared vectors | 3.1 | Vector tests in Go, JavaScript and Dart; feed reset; a subscribed calendar picks up a change |
| 3.4 | `meeting` sessions: opening window, organiser and co-hosts, lobby policies, admit and refuse; invited outsiders for one occurrence | 3.1, 1.2 | e2e: an outsider waits and is let in, sees only that occurrence's thread, and not the next occurrence |
| 3.5 | Hands and reactions in meetings; live poll tally in the room | 2.5 | e2e |
| 3.6 | Large meetings: the speaker and nine tiles, the order (pin, spotlight, hand, voice), host spotlight, pictures for the rest, paused cameras, the camera limit counting cameras sent | 1.4 | `calltest` with 70 seats: no client is sent more than ten cameras; unseen cameras get empty demand; e2e of the ordering with fake media |
| 3.7 | Breakouts: three ways to assign, timer, warning, return; hosts moving between | 1.2 | `calltest` with 40 seats in 8 spaces and back; e2e |
| 3.8 | Meeting notes: a `notes` document per occurrence, owned by the organiser, co-hosts editors, invitees viewers, its card in the thread, shared when the meeting opens | D.4, 3.2 | e2e: organiser and co-host edit, an invitee cannot, a guest watches and has nothing when the meeting ends |
| 3.9 | Attendance report, posted for hosts in the thread | 3.4 | API tests; only hosts may read |
| 3.10 | Recording: the tool path with ffmpeg (audio mix, programme video), the `churr-media` image, the recording message in the thread, playback, download; a standalone meeting's recording as an item of its own, owned by its starter, with no chat replay | 1.7 | Node tests with fake media and real ffmpeg in CI; a later member cannot play it; a pinned one can be opened |
| 3.11 | Recording consent: asking (the notice says the chat is not kept, for a standalone meeting), the `all` and `skip` modes, the "not recorded" frame, late joiners, changing one's answer | 3.10 | Node tests: a seat that said no is absent from the recorded audio and video and the frame is shown; `all` does not start without every yes; a change applies from then on |
| 3.12 | Guests: links, the two link kinds, the 5-second notice and delay for direct links, guest accounts, name and picture remembered in the browser, the guest shell, limits | 3.4 | API abuse tests (rate, expiry, revocation); e2e as a guest by each kind of link: the notice is shown and entry is held 5 seconds, a host can refuse; a guest sees nothing from before admission; none in a shared channel |
| 3.13 | Join with code; meeting links shown as previews with "in progress" | 3.1 | e2e; a reader who may not see the meeting gets a plain link |
| 3.14 | Linked instances: `meeting_answer` frame, playback pass for recordings | 3.1, 3.10 | Link tests |

Migrations: meetings tables and `calendar_feeds`; guest columns on
`accounts`; consent columns.

### Stage 4: streams

Ships: going live from web, app and OBS; low delay for hundreds and HLS
for thousands; the red live dot, the marker line in chat and the viewer
count; chat beside with slow mode; scheduled streams; replays with chat; clips;
several qualities as an admin switch.

| # | Piece | Depends on | Tests |
|---|---|---|---|
| 4.1 | `go_live`, `live_who`, `members.may_go_live`; `stream` sessions; going live from web and app; `room.live_changed`, the red live dot in the sidebar (steady under reduced motion), the marker line in chat; the style guide's exception | Stage 1, 2.7 | Permission tests; e2e: nobody is notified, the dot and the line appear and go |
| 4.2 | WHIP: endpoint, ingest keys, wider H.264 profiles | 4.1 | A Pion WHIP client in Go tests; a manual OBS check listed in the release notes |
| 4.3 | HLS passthrough: fMP4 segments, playlists, authorised serving from disk and bucket, viewer counting | 1.7, 4.1 | Segment and playlist tests against vectors; playback in e2e with hls.js; the Apple spike of section 3.6 |
| 4.4 | Players: hls.js in the web chat, `media_kit` in the app, switching between low delay and HLS | 4.3 | e2e; app tests |
| 4.5 | HLS through the tool path for stages (several speakers); AAC rendition | 3.10, 4.3 | Node tests with ffmpeg |
| 4.6 | Replays with chat alongside; retention; `replay_days` | 4.3 | e2e: replay shows the messages of its span; janitor tests |
| 4.7 | Clips | 4.6 | API tests; exact and boundary cuts |
| 4.8 | Qualities: the ladder; NVENC, QSV and VAAPI in the one `churr-media` image, detection at start, fallback to software, the host requirements written in `docs/OPERATIONS.md` | 4.5 | Node tests with software; a run on each kind of hardware to hand; an image started with no device falls back; measurement scenario 5 |
| 4.9 | Scheduled streams: a meeting row of kind stream with a reminder | 3.1, 4.1 | API tests |
| 4.10 | Linked instances: low delay and HLS by pass | 4.3 | Link tests |

Migrations: `ingest_keys`; room columns for replays.

### Stage 5: captions and transcripts

| # | Piece | Depends on | Tests |
|---|---|---|---|
| 5.1 | Choose the model: measure size, processor cost and accuracy for the candidates; document | – | Measurement scenario 6 |
| 5.2 | `churr-speech`, its protocol with the node, starting and stopping | 1.7 | Node tests with a stub helper |
| 5.3 | `room.caption`, captions in web and app, per-person switch | 5.2 | e2e with the stub |
| 5.4 | Transcripts beside recordings; people who did not accept recording are left out; transcripts are not searchable (R91) | 5.2, 3.11 | API and retention tests; a seat that said no has no line |

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
8. **The role model is still being built.** Section 2.5 is written
   against rulings R97 and R99, not the code. If Moderator lands
   differently, the mapping of room roles changes with it; the engine
   does not.
9. **Guests are a public door.** An unauthenticated way in, with a
   session cookie, on a server that has had none, and with `direct`
   links no host in the way. A flaw there is a flaw in the whole server.
   It is off on a fresh install and has its own review.
10. **Shared documents are a product of their own.** Live co-editing
    brings a CRDT library into the web chat, the app (as native code)
    and the server. If the three implementations of the format do not
    agree, or the app's library is too heavy on a platform, the layer
    needs a different format, and meeting notes wait for it. Rooms do
    not: stage 3 can ship with notes as the one piece held back.
11. **Bandwidth, not processor, limits streams.** Thousands of HLS
    viewers are gigabits. A server on a modest line cannot do it whatever
    the design; the documents must say so beside the switch.
12. **Size of the change to clients.** The rename, slots, spaces and
    roles touch every call screen in the web chat and the app at once in
    stage 1. A mistake there breaks calls, the one live feature that
    works today. Stage 1 ships nothing new to users until its e2e suite
    is as wide as today's.
13. **The "not recorded" mode depends on the node being right.** A seat
    that said no must never reach the recording tap, including in the
    seconds around a change of answer or a move between spaces. A bug
    there records someone who refused. It gets its own tests at the tap,
    not only at the interface.
14. **Large meetings lean on clients pausing unseen cameras.** A client
    that keeps encoding costs the node inbound traffic for nothing. The
    node drops what nobody wants, so the room is safe, but a phone's
    battery is not.
15. **The document sidecar ties two clocks together.** Document updates
    are timed by the server and media by the node. If the mapping
    drifts, a replay shows edits early or late. And the export renderer
    is a headless browser on a media node: a large, fast-moving
    dependency, kept optional and off the live path for that reason.

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
- That one ffmpeg build carries NVENC, QSV and VAAPI together and starts
  cleanly on a host with none of them.
- Stereo Opus and a second audio track per seat in the apps' WebRTC
  library, and screen sound capture on each platform (it is not offered
  where the system cannot capture it).
- The Yjs format across Yjs, `yrs` in the app and the Go implementation
  (stage D.0).
- Writing to the device calendar from the app on each platform.

## 13. Decided

The owner's answers to this design's questions (rulings R88 to R91).
Entries marked "default" were set by the coordinator where the owner
left the recommended option standing, or filled in a detail; they stand
until the owner says otherwise.

| # | Question | Decided | Ruling |
|---|---|---|---|
| D1 | Lobby, backstage, breakouts | Spaces inside one session; the media connection is kept across moves | Default (R91) |
| D2 | A voice channel in the schema | A flag on a channel, `voice` | Default (R91) |
| D3 | Topology | Built-in node for plain calls; separate media nodes recommended for the rest; one session on one node | Default (R91) |
| D4 | On a fresh install | Calls, voice channels and meetings on. Streams, recording, captions and guests off | R90 |
| D5 | External tools | ffmpeg as a separate program on media nodes; one-source H.264 streams packaged in pure Go without it | Default (R91) |
| D6 | What a recording contains | The programme | R88 |
| D7 | Where recordings go | A recording store of their own: a folder or an S3-compatible bucket | R88 |
| D8 | Readers | Listen to stages and streams only | R91 |
| D9 | A meeting's chat | In a channel: a thread under the invite card, a new one per occurrence, kept there. Standalone: a chat attached only to the meeting, with access from its invitees and participants, and ephemeral (below). No group chat is made for a meeting of people | R89, R100 |
| D10 | Guests | One meeting. The meeting's configuration chooses lobby or direct admit. Name and picture remembered and reused, changeable (in the guest's browser: default). Always marked guest. No rule against a member's name. No separate cap: guests count toward the room's size | R89, R90 |
| D11 | Email | None, ever. Calendars instead: a personal subscribable feed, the app adding to the device's calendar, and `.ics` files. No sign-in to Google or Microsoft | R88, R89, R90 |
| D12 | Notes and whiteboard | Notes are a markdown document co-edited live. Boards are a separate system, existing outside meetings and edited live in or out of one. Task boards, timelines and other kinds follow. One shared documents layer; in a room a presenter shares a document instead of a screen, drawn live by each client; a document belongs to no channel and has its own owner and access list; a post in a channel is a card that grants nothing; sharing in a room gives view-only access for the time of the share. It gets its own design | R91 and the owner's later rulings |
| D13 | End-to-end encryption for rooms | None | R91 |
| D14 | Recording consent | Marker and an answer from each person. Whoever starts chooses: fail unless all accept, or skip those who do not (left out of audio, video, screen, captions and transcripts; a frame "<name> is speaking (not recorded)"). Late joiners count as not accepting until they answer; a change of mind applies from then on | R88, R89 |
| D15 | Blocks in rooms | The blocked person is muted on the blocker's device by default; the blocker can unmute by hand. Nothing changes for the blocked person | R91 |
| D16 | Going live | No notification. A red blinking "live" dot in the sidebar (steady under reduced motion; red is a deliberate style exception) and a temporary marker line in the channel's chat. A meeting link pasted elsewhere shows "in progress" on its preview | R90 |
| D17 | Identifiers | UUIDv7 as elsewhere, plus a short readable meeting code for links and "Join with code" | R91 |
| D18 | Hosting in shared channels | The instance of whoever starts the session | Default (R91) |
| D19 | Low-delay viewers beyond one node | HLS | Default (R91) |
| D20 | Web player | Vendor hls.js | Default (R91) |
| D21 | Screen sound and music quality | Both, in every room, at the presenter's choice | R91 |
| D22 | Transcripts in search | Not searchable for now | R91 |
| – | Large meetings | The speaker and up to nine tiles, the rest as pictures; into view by voice activity, raised hand, host pointing, own pin; unseen cameras pause; small and large camera versions | R90 |
| – | Media image | One `churr-media` image with NVENC, VAAPI and QSV, detected at start, software as fallback | R88 |
| – | Private channels (R87) | Rejoin within 3 hours keeps earlier visibility; pins of older messages open from the pin; quoted text visible. Applied to recordings, threads and documents in section 8 | R90 |

The second round (R100), on the questions the first answers opened:

| # | Question | Decided | Ruling |
|---|---|---|---|
| N1 | A meeting starts where a session is live | The meeting joins the running session | R100 |
| N2 | Who may show a document to a room | Its owner and editors | R100 |
| N3 | The format for live co-editing | The Yjs format, to be confirmed by the D.0 spike | Default (R100) |
| N4 | Titles in the calendar feed | Always the real titles | R100 |
| N5 | Direct-admit guest links | The meeting shows "a guest is entering in 5 seconds" and entry is delayed 5 seconds. No required use limit | R100 |
| N6 | Invitees outside a private host channel | That occurrence only | R100 |
| N7 | Music mode and screen sound at once | At most two such tracks in a session | Default (R100) |
| N8 | The red of the live dot | A token of its own, used by that mark alone | Default (R100) |
| N9 | A shared document in a recording | Replay in Churr from the document's stored updates; HLS viewers in Churr's player can switch to the document view, from a timed sidecar; a plain frame elsewhere; an on-demand export renders it into the video file with an optional headless renderer | R100 |
| N10 | People in the room without access | Watch only. No session grants | R100 |
| N11 | Opening a document by link | No link sharing | R100 |
| N12 | Posting a document's card | Gives the channel view access automatically if the poster is the owner or an editor; otherwise nothing | R100 |
| N13 | Meeting notes | The organiser owns, invitees view, co-hosts edit | R100 |
| N14 | Documents' lifetime and quota | Live until deleted; per-person and per-instance quota; pass to an instance Owner when the account is removed | R100 |
| – | Threads and meetings | A thread is always attached to something, not necessarily a channel. A meeting need not be in a channel | R100 |
| – | A standalone meeting's chat | Ephemeral: it exists only for the meeting and ceases to exist once the last person leaves. No history, search or replay of it afterwards. Calls in direct and group chats are not affected | Owner's proposal |
| – | * Grace period | Deleted 5 minutes after the room empties; unreadable to non-participants meanwhile | Default |
| – | * Scheduled and repeating | The chat exists from scheduling and is deleted after the occurrence by the same rule; a repeating meeting starts each occurrence empty | Default |
| – | * What outlives it | Notes are a document (organiser owns, invitees view); attendance goes to the organiser; a recording is an item of its own, owned by whoever started it, viewable by participants and invitees, with no chat replay, and the consent notice says so | Default |
| – | * What people are told | "To keep the conversation, hold the meeting in a channel" | Default |
| – | Roles | Owner, Admin (not a channel manager), Manager, Moderator, Member, Reader as a level, Guest; flags are not roles. Moderating a room is Moderator and above; configuring it is Manager. Guests cannot join shared channels | R97, R99 |
| – | Channels a hub owns | The hub owns them and is trusted, with the instances it joins. No protection against them is designed | R95 |

## 14. Still open

Nothing here blocks stage 1.

- **The co-editing format** is the Yjs format unless the D.0 spike
  finds the three implementations do not agree (N3).
- **Shared documents** have their own design to come: quotas' numbers,
  boards in depth, access across instances.
- **The defaults marked * in section 13** for a standalone meeting's
  ephemeral chat (the 5-minute grace period, scheduled and repeating
  meetings, what outlives the chat, the wording) stand until the owner
  confirms or changes them.
- **Numbers:** every capacity figure in section 9, the cost of the
  export renderer, and the speech model's size and cost wait for
  measurement.

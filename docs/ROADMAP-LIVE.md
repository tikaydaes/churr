# Live rooms roadmap: voice, meetings and streams

What Churr will add for talking and broadcasting live, decided by the owner
on 2026-11-17. This is a specification of behaviour and an order of work.
It is not a design: the data model, media flow and load estimates come in a
design document, written and approved before anything is built.

Calls in channels exist today (docs/FEATURES.md). Everything below builds
on them.

## Order of work

1. **Base infrastructure, designed first.** One design that supports every
   feature on this page: rooms, roles inside a room, sub-rooms, recording,
   one-to-many delivery and ingest. Nothing is implemented until the owner
   approves it.
2. **Voice channels**, stage mode included.
3. **Meetings**: scheduled meetings, invites, lobby, breakouts, recording.
4. **Streams**: going live, HLS, replays, clips.
5. **Captions and transcripts**, last.

## 1. Voice channels

A voice channel is a channel kind: a room people drop into and out of. Nobody
is rung.

- **Text chat attached.** Every voice channel has an ordinary message log
  beside the room.
- **Created like channels.** Whoever may create a channel may create a voice
  channel. Public, knock and private apply as they do to other channels.
- **Talking.** People join muted. Once unmuted, the microphone is open
  (voice activation). Each person may switch to push-to-talk with a key of
  their choice.
- **Video.** Cameras and screen sharing are allowed. Managers can switch video
  off for a channel.
- **Size.** Managers may set a cap per channel, within a server-wide maximum
  set by the admin from the machine's measured size.
- **Seen from outside.** Anyone who can see the channel sees who is in the
  room, with live speaking indicators and mute and share markers.
- **Moderation by managers:**
  - mute a person for everyone (only a manager can unmute them);
  - disconnect a person (they may rejoin unless banned);
  - move a person to another voice channel;
  - lock the room (nobody new joins until unlocked).

### Stage mode, in the first build

A voice channel can be switched to stage mode. Town halls are stage rooms:
there is no separate town-hall kind; every capability below belongs to stage
mode.

- **Presenters and audience.** Named presenters speak and share. The audience
  listens and cannot unmute unless invited up.
- **Raise hand.** Hands queue in order; a host invites someone up.
- **Moderated Q&A.** The audience submits questions and upvotes them;
  moderators choose which go up.
- **Backstage.** Presenters gather and check their set-up before going live.
- **Large audiences.** Beyond the room's cap, the stage is streamed to
  viewers using the stream feature (section 3).

## 2. Meetings

- **Scheduled meetings** with a time, a room, an organiser and invitees.
- **Recurring meetings**, such as a weekly stand-up, with exceptions.
- **Invites:**
  - an invite card in Churr with Accept, Decline and Maybe, and reminders
    before the start;
  - "Add to calendar" as an .ics file made on the device, updated when the
    meeting changes;
  - email invites to guests, carrying a guest join link (needs the server's
    email set-up).
- **Guest join link.** People without an account join one meeting as a
  named guest.
- **Lobby.** People outside the invite wait until a host lets them in.
- **Hosts.** The organiser can make others co-hosts with the same powers.
- **In the meeting:**
  - raise hand and quick reactions over the video;
  - polls, using the existing /poll, with results shown live;
  - meeting notes and agenda: a shared page tied to the meeting, kept after
    it ends;
  - a whiteboard: a shared drawing surface.
- **Breakout rooms.** A host assigns people, splits them at random into N
  rooms, or lets people choose. Rooms can run on a timer: a warning before
  the end, then everyone returns to the main room.
- **Attendance report** for the organiser: who joined, when and for how long.
- **Recording.** A host starts it. Everyone sees a clear recording marker
  and is told when they join. The recording is saved to the channel.

## 3. Streams

- **Who may go live** is set per channel by its managers: managers only,
  chosen people, or any member.
- **Going live** from the web chat or the app (camera or screen), or from
  OBS and similar tools over WHIP.
- **Delivery:**
  - low delay to the first hundreds of viewers, through the media server;
  - HLS beyond that, for thousands, with a few seconds of delay.
- **Qualities.** One quality as sent, by default. An admin switch turns on
  several qualities (for example 1080p, 720p and 480p); its processor cost
  is measured and documented.
- **Around a stream:**
  - a "live now" marker and viewer count on the channel, and a notification
    to followers;
  - the channel's chat beside the video, with slow mode for busy streams;
  - scheduled streams, announced for a time, with a reminder.
- **Replays.** Streams are recorded and kept under the channel's retention
  rules, watchable with the chat replayed alongside.
- **Clips.** Viewers save a short clip from a replay.

## 4. Captions and transcripts

Live captions and transcripts, from an optional speech-recognition model
that runs on the server itself. It is off by default; an admin turns it on.
Its processor cost is measured and documented first. Churr still uses no
LLM service when it runs: nothing leaves the server.

## Storage

Recordings, replays and clips are stored like uploaded files. They count
toward an admin quota per channel and per server, and follow retention
rules.

## Not decided yet

The design document must settle these and bring them to the owner:

- How breakout rooms, stage presenters and the lobby are modelled, so that
  meetings and voice channels share one room model.
- The media path for one-to-many delivery, HLS packaging and WHIP ingest,
  and how far the current media server carries them.
- Measured capacity per machine size for rooms, stages, streams and
  multi-quality conversion.
- Where the whiteboard and meeting notes live (messages, forms, or a new
  kind of object).
- Guests: what a guest can see, how long a guest identity lasts, and limits.
- The size and processor cost of the local speech model.

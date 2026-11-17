# Live rooms roadmap: voice, meetings and streams

What Churr will add for talking and broadcasting live, decided by the owner
on 2026-11-17. This is a specification of behaviour and an order of work.
It is not a design: the data model, media flow and load estimates are in
`docs/DESIGN-LIVE.md`, whose section 13 records the owner's decisions
(rulings R88 to R100) that this page now reflects. "Instance" means one
Churr installation.

Calls in channels exist today (docs/FEATURES.md). Everything below builds
on them.

## Order of work

1. **Base infrastructure**, as designed in `docs/DESIGN-LIVE.md`: rooms,
   roles inside a room, spaces, media nodes, recording, one-to-many
   delivery and ingest.
2. **Voice channels**, stage mode included.
   Beside stages 2 and 3: **shared documents** (section 2a), after a
   design of their own.
3. **Meetings**: scheduled meetings, invites, calendars, lobby, breakouts,
   recording, guests.
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
- **Sound.** Someone sharing a screen may share its sound, and anyone
  speaking may switch their microphone to music quality. Both are the
  presenter's choice, in every kind of room.
- **Readers** of a channel may listen to a stage or a stream. They may not
  join an open voice room or a meeting.
- **Blocks.** Someone you have blocked is muted on your device by default;
  you can unmute them by hand. Nothing changes for them.
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

- **Scheduled meetings** with a time, a room, an organiser and invitees,
  and a short readable code for links and "Join with code".
- **Recurring meetings**, such as a weekly stand-up, with exceptions.
- **A scheduled meeting lives in a conversation**: a channel, or a direct
  or group chat. Scheduling with a list of people uses their group chat,
  or makes it. (Recommended; awaiting the owner's confirmation.) Its chat
  is a thread there: the invite card is the thread's parent, and the
  notes, the recording and the attendance report attach to it. A
  recurring meeting gets a new thread for each occurrence. A meeting that
  starts while the room is in use joins what is running. Everything on
  this list belongs to meetings in a conversation.
- **"Meet now" outside any chat is super-lightweight.** A few people
  start a meeting directly; it rings them like a call. It has camera,
  screen and document sharing, raised hands, reactions, adding people,
  and a chat that is deleted 30 minutes after the last person leaves.
  It has no recording, no transcription, no history, no attendance and
  no search, and no guests, lobby, breakouts or scheduling. A document
  presented there persists, because it is its own thing. To keep the
  conversation, hold the meeting in a chat or a channel. Calls in direct
  and group chats are not affected: they keep their chat's history.
- **Someone invited who is not in a private host channel** gets that one
  occurrence only.
- **Invites and calendars:**
  - an invite card in Churr with Accept, Decline and Maybe, and reminders
    before the start;
  - a personal calendar feed: a secret address a calendar program
    subscribes to, carrying one's meetings with their real titles and
    their changes;
  - the app adds the meeting to the device's calendar;
  - "Add to calendar" as an .ics file made on the device, updated when the
    meeting changes.
  Churr sends no email, and does not sign in to Google or Microsoft
  calendars.
- **A link to a meeting** pasted in any chat shows "in progress" on its
  preview while the meeting is on.
- **Guest join link.** People without an account join one meeting as a
  named guest. The meeting's settings choose the kind of link: guests wait
  in the lobby, or come straight in, in which case the meeting shows "a
  guest is entering in 5 seconds" and entry is held for those 5 seconds.
  Guests are not offered in shared channels. A guest's name and picture are
  remembered in their browser and offered again, and can be changed. A
  guest is always marked as a guest. Guests count toward the room's size;
  there is no separate limit.
- **Lobby.** People outside the invite wait until a host lets them in.
- **Hosts.** The organiser can make others co-hosts with the same powers.
- **In the meeting:**
  - raise hand and quick reactions over the video;
  - polls, using the existing /poll, with results shown live;
  - meeting notes and agenda: a shared markdown document, edited live,
    kept after the meeting ends. The organiser owns it, co-hosts edit,
    invitees view;
  - sharing a document instead of a screen (section 2a).
- **Large meetings** (60 to 70 people). Each person sees the speaker and
  up to nine others on video, the rest as pictures. People come into view
  by speaking, raising a hand, a host pointing at them, or one's own pin.
  Cameras nobody is looking at pause.
- **Breakout rooms.** A host assigns people, splits them at random into N
  rooms, or lets people choose. Rooms can run on a timer: a warning before
  the end, then everyone returns to the main room.
- **Attendance report** for the organiser: who joined, when and for how long.
- **Recording.** A host starts it. Everyone sees a clear recording marker,
  is asked, and is told when they join. Whoever starts it chooses: it
  fails unless everyone accepts, or it leaves out those who do not: their
  sound, picture and screen are not recorded, and the recording shows
  "<name> is speaking (not recorded)" in their place. Someone joining
  later is left out until they answer; changing one's mind applies from
  then on. The recording is the programme (the mixed sound, and the
  speaker or shared screen), saved to the meeting's thread.

## 2a. Shared documents

Documents are their own separate things, with a design of their own to
come (`docs/ROADMAP.md` section 2). What is decided:

- **Kinds:** notes (a shared markdown editor, using Churr's markdown and
  message objects), a whiteboard in the manner of Miro or FigJam, later a
  Kanban task board, a timeline and others. All are edited live by
  several people at once, in a meeting or not.
- **A document does not belong to a channel.** It has an owner and its
  own access list: people, groups or everyone on the instance, as editor,
  commenter or viewer. There is no sharing by link. A document lives
  until its owner deletes it; when an account is removed its documents
  pass to an instance Owner.
- **Posting a document in a channel** posts a card that links to it. If
  the poster is the document's owner or an editor, the channel's members
  get view access automatically; otherwise the card grants nothing and
  people without access see "a document you can't see".
- **In a room, a presenter shares a document instead of a screen.** Its
  owner and editors may present it. The room shows the document itself,
  live, drawn on each person's device, not a video of it. People follow
  the presenter and may look around and come back. People in the room
  who have no access to it watch only, for as long as it is shared;
  nobody gains rights by being in the room. Several people may present
  at once.
- **In a recording**, the document is replayed by Churr's player from
  its own stored changes, in step with the sound. People watching a
  stream in Churr's player can switch to the document view as well.
  Other players show a plain frame. Someone who needs a video file with
  the document in it asks for an export, which an optional renderer on a
  media node makes on demand.

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
  - a red, blinking "live" dot on the channel in the sidebar (steady
    under reduced motion), a viewer count, and a marker line in the
    channel's chat while it is live. Nobody is notified;
  - the channel's chat beside the video, with slow mode for busy streams;
  - scheduled streams, announced for a time, with a reminder.
- **Replays.** Streams are recorded and kept under the channel's retention
  rules, watchable with the chat replayed alongside.
- **Clips.** Viewers save a short clip from a replay.

## 4. Captions and transcripts

Live captions and transcripts, from an optional speech-recognition model
that runs on the server itself. It is off by default; an admin turns it on.
Its processor cost is measured and documented first. Churr still uses no
LLM service when it runs: nothing leaves the server. People who did not
accept recording are left out of transcripts. Transcripts are not
searchable for now.

## Storage

Recordings, replays and clips are kept in a recording store of their own:
a folder, or an S3-compatible bucket. They count toward an admin quota
per channel and per instance, and follow retention rules.

## Privacy

- No room is encrypted end to end. A sealed chat keeps its two-person
  encrypted call and has no other room feature.
- In a private channel, recordings and meeting threads follow the
  private-channel contract: a newcomer does not see earlier ones, unless
  one is pinned; rejoining within 3 hours keeps earlier visibility. For
  a document the contract covers its card in the channel; the document
  is governed by its own access list.

## Roles in rooms

Rooms use Churr's role model (`docs/ROADMAP.md` section 4). A Member may
start a session. A Moderator and above may moderate a room: mute,
disconnect, move, lock. A Manager configures it. An Admin is not a
manager of channels and has no role in a room as such. In a "Meet now"
outside any chat, whoever started it hosts.

## What is on

On a fresh install calls, voice channels and meetings are on. Streams,
recording, captions and guests are off until an admin turns them on.
Stages, streams, recording and captions are best run on separate media
nodes; one image carries the hardware encoders (NVENC, VAAPI, QSV), found
at start, with software as the fallback.

## Not decided yet

`docs/DESIGN-LIVE.md` section 14 lists what is still open. Still to come:

- Measured capacity per machine size for rooms, stages, streams and
  multi-quality conversion.
- The design of shared documents.
- The size and processor cost of the local speech model.

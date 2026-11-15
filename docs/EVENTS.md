# Events

The live connection: how a client opens it, catches up, stays on it, and
what arrives over it. `CONVENTIONS.md` gives the rules for names,
identifiers and times. Objects named here (`Message`, `Conversation`,
`Member`, `Call` and so on) have the shapes in `openapi.yaml` and
`MESSAGE-FORMAT.md`; an event carries them in full, in the shape the
API returns them.

Contents:

1. The connection
2. Hello and resume
3. Catch-up and reload
4. Heartbeats and activity
5. Back-pressure and limits
6. Close codes
7. Frames a client sends
8. Conversation events
9. Account events
10. Events only bots receive
11. Call signalling
12. Sealed chats

## 1. The connection

One WebSocket per client, at `GET /api/v1/events` (`openEvents`).

- The client asks for the subprotocol `churr.v1` in
  `Sec-WebSocket-Protocol`. The server refuses the upgrade without it.
- Authentication is the same as for any request: the `churr_session`
  cookie, or `Authorization: Bearer <token>` on the upgrade request.
  The web chat uses the cookie. Apps and bots use the header. A token
  is never put in the address.
- With a cookie, the `Origin` header must be the server's own origin.
  Otherwise the upgrade is refused with 403.
- A failed upgrade is an ordinary HTTP error in the usual shape: 401
  `unauthenticated` or `second_factor_required`, 403 `forbidden`, 429
  `rate_limited`.
- Every frame is a text frame holding one JSON object. Binary frames
  are not used. A frame is at most 1 MB.
- An account may hold several connections: one per app, tab or bot
  process. Each is served on its own.

There are two sorts of frame.

- **Events** have a `type` of the form `thing.happened` and an `at`.
  They only travel from server to client.
- **Control frames** have a `type` with no dot when the server sends
  them (`hello`, `ready`, `reload`, `pong`, `ack`, `error`) and are
  listed in sections 2 to 5. Frames the client sends are listed in
  section 7.

Clients ignore event types and fields they do not know.

### What a connection receives

Fan-out is by membership and is decided by the server alone. There is
no subscribe request.

- **Conversation events** go to the connections of the conversation's
  members, and to nobody else. An owner, admin or member who can read
  a public channel without joining it receives nothing live from it;
  they read it over HTTP.
- **Account events** go to the accounts named for each event in
  section 9.
- A bot is an account. It receives the same events as a person would
  in the channels it is in, plus those in section 10.

Parts of an object that differ per reader are filled in for the
account that receives the frame: `reactions[].me`, `form.my_answer`,
and `form.tally` and `form.voters` when the form's `results` setting
allows them for that reader. The same is done on catch-up.

## 2. Hello and resume

    server → client   hello
    client → server   resume
    server → client   replayed events, and reload frames
    server → client   ready
    server → client   live events

**`hello`**, sent by the server as soon as the connection is open:

```json
{
  "type": "hello",
  "connection_id": "c_8Jq2mT0xY4",
  "account_id": "0193a000-…",
  "session_id": "0193a0aa-…",
  "heartbeat_seconds": 30,
  "resume_within_seconds": 10,
  "reconnect_spread_seconds": 20,
  "server_time": "2026-11-13T09:30:00.000Z"
}
```

`connection_id` names this connection for the lifetime of the socket.
It is there for logs and for reporting problems.

`reconnect_spread_seconds` is how many seconds the server asks its
clients to spread their next reconnect over (section 6). It is worked
out when `hello` is sent, from how many live connections the server
has: one second for every 250 connections, rounded up, never less
than 2 and never more than 60. A server of twenty people says 2, one
with 5,000 connections says 20, and from 15,000 it says 60. A client
keeps the last value it was told, because the `hello` that follows a
restart comes from a server that has few connections yet. A server
older than this field does not send it.

**`resume`**, sent by the client within `resume_within_seconds`:

```json
{
  "type": "resume",
  "positions": {
    "0193a6f0-…": 4821,
    "0193a0c7-…": 96
  }
}
```

`positions` maps each conversation the client holds to the last `seq`
it has applied. A client with nothing stored sends `"positions": {}`.

The server then does this for each conversation in `positions`:

- If the account is still a member and the gap can be replayed, it
  sends the missed events in `seq` order, exactly as they would have
  arrived live.
- Otherwise it sends one `reload` frame (section 3).

Events that happen while the replay is running are held back for that
conversation and sent after its replay, so a client never sees a
conversation's events out of order.

**The resume queue.** The server accepts every connection and sends
`hello` at once, but it works only so many `resume` frames at a time,
first come first served; the rest wait in a queue. This is what keeps
a server answering when everyone reconnects in the same few seconds,
after a restart for instance. Nothing changes for a client, which
needs to do nothing for it:

- While its `resume` waits, the connection is open in every other
  way. `ping` is answered with `pong` (section 4), so a client that
  expects one within a heartbeat does not give up, and the connection
  is not closed for the time it waits. `resume_within_seconds` is only
  about how soon the client sends `resume`.
- The replay is worked out when the connection reaches the front of
  the queue, not when `resume` arrived. It therefore holds everything
  up to that moment, including what happened while the connection
  waited, and events after that are held back and sent after it as
  above. Nothing is missed and nothing comes twice.
- Under normal load there is no queue: a `resume` is worked the
  moment it arrives.
- How many are worked at once comes from the machine, not from a
  setting: half the database's connection pool, and at most twice the
  number of processors (ten on PostgreSQL with five or more
  processors, eight with four; four on SQLite).
- The queue holds 1,000 waiting `resume` frames for each one worked
  at once. Past that the server closes the connection with 4029
  (section 6), and the client comes back in about thirty seconds.
- Telling others that an account went offline takes its turn behind
  the waiting `resume` frames. When every connection drops and comes
  straight back, most accounts are connected again before their turn
  comes, and nobody is told anything.

**`ready`** ends the replay:

```json
{
  "type": "ready",
  "new_conversation_ids": ["0193b111-…"]
}
```

`new_conversation_ids` lists conversations the account is in that
were not in `positions`: ones joined while away, or all of them on a
first connection. The client loads those over HTTP.

Account events have no `seq` and are not replayed. After `ready` a
client that was away fetches what it shows of account state again:
`getMe`, `listConversations` (which carries read positions, modes and
counts), and `getStatusSummary`. These are cheap.

`ready` carries nobody's presence. A client has presence from the
accounts it loads over HTTP (`presence` on every account), and that is
its one snapshot. Presence that changes between `hello` and `ready` is
not sent as it happens: the server keeps the newest value for each
account and sends them after `ready` in one frame, `presence.changed`
if there is one and `presence.batch` if there are several (section 9).

From `ready` on, events arrive as they happen. Within a conversation
`seq` goes up by exactly one from event to event. A client that sees a
jump has missed something and calls `listEvents` with the last `seq`
it has.

## 3. Catch-up and reload

The server keeps each conversation's events for 30 days in the same
shape they were sent. Two ways use that log.

- **On the connection**, through `resume`, as above.
- **Over HTTP**, with `GET /api/v1/conversations/{id}/events?after=`
  (`listEvents`), which returns events after a `seq`, oldest first,
  with `has_more`. This also works for a public channel the caller has
  not joined.

Only events with a `seq` are in the log. Typing, call events,
presence and all account events are not.

**`reload`** tells the client to throw away what it holds for one
conversation:

```json
{
  "type": "reload",
  "conversation_id": "0193a6f0-…",
  "reason": "too_far_behind",
  "last_seq": 9120
}
```

| `reason`           | Meaning                                         | What the client does |
|--------------------|-------------------------------------------------|----------------------|
| `too_far_behind`   | The client's `seq` is older than the log, or more than 1,000 events behind. | Drop its cached messages for the conversation, fetch the newest page with `listMessages`, and carry on from `last_seq`. |
| `no_longer_member` | The account left or was removed while away.     | Drop the conversation. `last_seq` is absent. |
| `unknown`          | The conversation does not exist, or the account may not know it does. | Drop the conversation. `last_seq` is absent. |

`listEvents` reports the first case as `"reload": true` with no
events. The server may also send `reload` on a live connection when a
conversation changed in a way events do not describe, for example
after a restore from backup.

## 4. Heartbeats and activity

- The client sends `{"type": "ping"}` every `heartbeat_seconds`. The
  server answers `{"type": "pong", "server_time": "…"}`.
- A connection that has sent nothing for three heartbeats is closed
  with code 4008.
- A client that gets no `pong` within one heartbeat should treat the
  connection as dead and reconnect.

Presence is worked out from connections and from activity.

- A person is `online` while at least one of their connections has
  sent `activity` in the last five minutes, `away` while they have a
  connection and none has, and `offline` with no connection.
- The client sends `{"type": "activity"}` when the person does
  something (a key, a pointer move, the app coming to the front), at
  most once a minute.
- A bot is `online` while it is connected and never needs to send
  `activity`.
- The same five minutes decide phone wake-ups: a phone is woken
  unless a client of that account is connected and active (PUSH.md).
- Appear away, out of office and a status that appears away override
  the result to `away` (DESIGN.md section 3).

## 5. Back-pressure and limits

- Each connection has a send queue. When a client reads too slowly
  and the queue reaches 1,000 frames or 4 MB, the server closes the
  connection with code 4009. Nothing is lost: the client reconnects
  and `resume` replays what it missed.
- Presence is the one thing the server merges and holds back under
  pressure, because presence is state: only the newest value for an
  account matters. Under normal load a change is sent at once as
  `presence.changed`. Under pressure each connection is sent, every so
  often, one `presence.batch` frame holding the newest value for each
  account whose presence changed since the last (section 9). How long
  changes wait goes from nothing up to 1.5 seconds and back down, by
  two things the server sees of itself: how many presence frames a
  second the changes of the last second would have cost sent one by
  one (nothing waits up to 5,000 a second; the wait is then 100 ms for
  every 5,000, so 1.5 seconds from 75,000), and how full this
  connection's own send queue is (nothing waits below a quarter full;
  the whole 1.5 seconds from three quarters). There is no threshold on
  the number of members. A slow client is therefore sent fewer, larger
  presence frames before it is ever closed for a full queue.
- Nothing else is merged, held back or reordered: messages, edits,
  reactions and every other event are one frame each, in order, as
  section 2 says.
- A client may send at most 20 frames a second, call signalling
  included. Past that the server answers `error` with `rate_limited`
  and drops the frame; a client that keeps going is closed with 4029.
  The same code closes a connection whose `resume` found the resume
  queue full (section 2).
  A call frame dropped this way gets the same generic `error` frame,
  not `call.error`.
- A frame the server cannot read (not JSON, an unknown `type`, an
  unknown field, a wrong value) gets an `error` frame and is
  otherwise ignored. Ten in a minute closes the connection with 4004.

A client frame may carry `id`, any string up to 64 characters that
the client chooses, unique on that connection. The server's answer
repeats it as `reply_to`:

```json
{"type": "ack", "reply_to": "17"}
{"type": "error", "reply_to": "17", "error": {"code": "conflict", "message": "The group has moved on."}}
```

`error` uses the API's error shape and codes. A frame without `id`
gets no `ack`, and an `error` without `reply_to` only if it fails.
Call frames are answered with `call.ok` and `call.error` instead
(section 11).

## 6. Close codes

| Code | Meaning                                            | Reconnect? |
|------|----------------------------------------------------|------------|
| 1000 | Normal close by either side.                       | If the client wants to. |
| 1001 | The server is shutting down.                       | Yes, after a short wait. |
| 1012 | The server is restarting.                          | Yes, after a short wait. |
| 4001 | The credential is no longer valid: the session ended, the token was revoked or expired. | Only after signing in again or refreshing the app token. |
| 4003 | The account was removed or banned.                 | No. |
| 4004 | Too many frames the server could not read.         | Yes, after fixing the client. |
| 4005 | No `resume` arrived in time.                       | Yes. |
| 4008 | No heartbeat.                                      | Yes. |
| 4009 | The send queue filled.                             | Yes. |
| 4029 | The client sent too much, or too many clients are reconnecting at once and the resume queue is full (section 2). | Yes, after about 30 seconds: the rule below with an interval of 30. |

Clients reconnect after a wait drawn at random, evenly, between half
the retry interval and the interval plus `reconnect_spread_seconds`
from the last `hello` (5 if the server has never said). The retry
interval is 1 second for the first attempt, then 2, 5, 10 and 30
seconds, and 30 from then on; it starts again at 1 once the
connection is up. So the first wait is between 0.5 seconds and 1
second more than the spread: a server of twenty people is back in at
most three seconds, and 5,000 clients spread themselves over about
twenty. After a 4029 close the interval is 30. A client may connect
at once, without waiting, when its device reports the network is back
or the person brings it to the front.

## 7. Frames a client sends

| `type`          | Fields                         | What it does                           |
|-----------------|--------------------------------|----------------------------------------|
| `resume`        | `positions`                    | Section 2. Once, first.                |
| `ping`          |                                | Section 4.                             |
| `activity`      |                                | The person is active. Section 4.       |
| `typing`        | `conversation_id`, `parent_id` | The same as `sendTyping`. `parent_id` only in a thread. |
| `call.join`, `call.offer`, `call.answer`, `call.candidate`, `call.subscribe`, `call.mute`, `call.restart`, `call.resume`, `call.decline`, `call.leave` | | Section 11. |

Any of them may carry `id` (section 5). Everything else a client does
goes over HTTP. Posting, reacting, reading, and everything a sealed
chat needs (epoch records, unlock requests) are operations, not
frames.

## 8. Conversation events

Each of these belongs to a conversation and has `conversation_id`.
Those marked **stored** also have a `seq`, are kept for catch-up, and
count in the conversation's numbering. The others are sent once and
never stored, and have no `seq`.

Unless a row says otherwise, an event goes to **every member of the
conversation**.

Common fields, not repeated below:

```json
{"type": "message.created", "at": "2026-11-13T09:30:00.000Z", "conversation_id": "0193a6f0-…", "seq": 4821}
```

### Messages

**`message.created`** (stored). A message was posted, by a person, a
bot, a webhook, a schedule or the server itself.

```json
{"type": "message.created", "at": "…", "conversation_id": "…", "seq": 4821,
 "message": { Message },
 "notify": "notify",
 "client_id": "0193c000-…"}
```

`client_id` is present only on the frames sent to the author's own
connections, so the client that posted can match it to what it is
showing as being sent.

`notify` is worked out for the account that receives the frame, by
the one rule that also decides badges and phone wake-ups (PUSH.md
sections 2 and 12). A connected client cannot work it out itself:
group mentions, thread states, blocks and do not disturb are the
server's to decide.

| `notify` | Meaning                                        | What the client does |
|----------|------------------------------------------------|----------------------|
| `none`   | The mode does not count this message.          | Nothing.             |
| `badge`  | It counts towards the badge and would not have notified: a name mention in a conversation whose mode is `nothing` or that is muted, or the account is in do not disturb. | Updates badges. |
| `notify` | It would have notified.                        | Updates badges and shows a notification, unless the conversation is on screen and in front. |

On catch-up `notify` is `none` or `badge`, never `notify`: a client
does not pop up for what happened while it was away. Only
`message.created` carries it. Edits, deletes, reactions, ticks, form
answers and pins never notify. For a reply, `message.parent_id` is set; the
client updates the parent's `thread` summary itself from the reply
(`reply_count` + 1, `last_reply_seq`, `last_reply_order` from the
reply's `order`, `last_reply_at`, and the author added to
`participant_ids`).

In a sealed chat the message has only the fields of
MESSAGE-FORMAT.md section 9: `id`, `conversation_id`, `seq`, `order`,
`author_id`, `client_id`, `sealed` (`key_epoch` and `ciphertext`),
`quiet` and `created_at`, with `late` and `unsent` where they apply. A quiet message (a reaction, tick, edit or
delete, as the sender marked it) always has `notify` of `none` and
does not count as unread.

**Where a message is shown: `order`, `late`, `unsent`.** Every
message carries `order`, a text. A client shows a conversation, and
a thread, sorted by `order`, compared character by character as
plain text, and reads nothing else out of it. `seq` is the event's
arrival number on this server: it is what catch-up, read positions
and unread counts go by, and it does not say where a message is
shown.

- In a conversation that is not shared with other servers, `order`
  follows `seq`: sorting by either gives the same list.
- In a conversation shared with other servers (LINK-PROTOCOL.md
  section 13), `order` comes from the time, by a clock the servers
  share, at which the message was posted. It is the same on every
  server, and it need not follow `seq`: a message from a server that
  was cut off for a while arrives now, with the next `seq`, and
  belongs further up. Such a message has `late` true: it arrived
  after messages ordered later than it had already been delivered
  here. A client inserts it at its place by `order`; it is unread
  like any new message, since its `seq` is above the read position.
  `message.created` for it is an ordinary `message.created`.
- A message posted on this server in a shared conversation is stored
  and shown at once, in its place, with its `seq` and `order`. It has
  `unsent` true until the link to the other servers has acknowledged
  it. Nothing else about it is provisional: it can be edited, deleted
  and reacted to like any other.

**`message.sent`** (stored). Only in a conversation shared with
other servers, and only on the author's own server. The link has
acknowledged a message that was `unsent`.

```json
{"type": "message.sent", "at": "…", "conversation_id": "…", "seq": 4833, "message_id": "0193a6f2-…"}
```

A client clears `unsent` on the message. The event is stored with the
conversation's next `seq` (and goes nowhere over the link), so a
client that was not connected when it happened gets it on catch-up
like any other change; one that reloads sees `unsent` absent on the
message. It is erased with the message.

**`message.placed`** (stored). Only in a conversation shared with
other servers, and only on the author's own server. The message's
`order` changed: the time it was first given was refused by the
conversation's time reference (this server's clock had been set, for
example) and it was given another.

```json
{"type": "message.placed", …, "message": { Message }}
```

A client replaces the message it holds and sorts again. It happens
only to a message that is still `unsent`, and it is rare. When the
message is a reply whose `seq` is its parent's `last_reply_seq`, the
client also sets the parent's `thread.last_reply_order` to the new
`order`; the stored parent already carries it.

**`message.edited`** (stored). The author changed the content.

```json
{"type": "message.edited", …, "message": { Message }}
```

Inside the quick-correction window `message.edited_at` stays absent
and `has_history` stays false. The server sends the same event
when it changes a message it posted itself, as when a call's system
message goes from `call_started` to `call_ended`. It is not used in
a sealed chat, where an edit is a new sealed message.

**`message.deleted`** (stored).

```json
{"type": "message.deleted", …, "message": { Message }}
```

`message` is the marker: `deleted_at` and `deleted_by` are set and
the text, files, cards and form are empty. Its reactions are gone.
For a deleted reply the client lowers the parent's `reply_count`.

**`message.purged`** (stored). A message's history is no longer
there to open.

```json
{"type": "message.purged", …, "message": { Message }, "erased": true}
```

Sent when history is purged at once (a private or ad-hoc channel, or
an admin) and when an author's request hides it in a public channel.
`message.has_history` is false.
`erased` is true when nothing is left but the record that a message
was there, who posted it and when. Hiding and erasing after an
author's request look the same to everyone but admins, and no further
event is sent when an admin later decides between them.

Retention sends no event. It removes messages older than the
conversation's period (`conversation.retention_seconds`, or the
server's in `getServer`), and clients drop the same messages from
their own storage by the same rule.

**`message.history_restored`** (stored). An admin decided a purge
request with "show the history again".

```json
{"type": "message.history_restored", …, "message": { Message }}
```

**`preview.added`** and **`preview.removed`** (stored). The server
added link-preview cards after a message was posted, or the author or
a manager removed one. Not an edit: `edited_at` does not change. A
theme card (a card with `theme`, for a link to one of this server's
themes) arrives and goes the same way.

```json
{"type": "preview.added", …, "message": { Message }}
```

**`reaction.added`** and **`reaction.removed`** (stored).

```json
{"type": "reaction.added", …,
 "message_id": "0193a6f2-…",
 "account_id": "0193a001-…",
 "reaction": {"emoji": "👍", "count": 3, "me": false, "account_ids": ["…", "…", "…"]}}
```

`reaction` is the message's entry for that emoji after the change.
On `reaction.removed` a `count` of 0 means the entry is gone.
Reactions notify nobody. In a sealed chat reactions travel inside
encrypted messages and these events are not sent.

**`task.changed`** (stored). A checklist item was ticked or unticked.

```json
{"type": "task.changed", …,
 "message_id": "0193a6f2-…",
 "task": {"id": "t1", "checked": true, "checked_by_id": "0193a001-…", "checked_at": "2026-11-13T09:41:02.118Z"}}
```

### Forms

**`form.answered`** (stored). Someone answered a form or changed
their answer, or pressed a button. This is also the tally update for
a poll.

```json
{"type": "form.answered", …,
 "message_id": "0193a6f2-…",
 "form": { Form },
 "account_id": "0193a001-…"}
```

- `form` is the whole form as the receiving account may see it:
  `answer_count` always, `tally` and `voters` only when `results`
  allows them for that reader, and `my_answer` for the reader's own
  answer.
- `account_id` is present only when the form is named and the reader
  may see `voters`. On an anonymous form it is never present, for
  anyone.
- Forms with the `bot` action do not produce this event for the
  conversation. Section 10.
- In a shared conversation, a server other than the poll's home
  server publishes `form.answered` for each tally the home server
  sends, with no `account_id`.

**`form.answer_withdrawn`** (stored). The same fields as
`form.answered`. When the poll's home server refuses an answer given
on another server, that server sends `form.answer_withdrawn` to the
answerer only, with `refused` holding the reason (`closed`, `full`
or `invalid`), and the answer is gone.

**`form.option_added`** (stored). An answerer added an option to a
choice field.

```json
{"type": "form.option_added", …, "message_id": "…", "form": { Form }}
```

**`form.closed`** (stored). Closed by a person or by `closes_at`.

```json
{"type": "form.closed", …, "message_id": "…", "form": { Form }, "closed_by_id": "0193a001-…"}
```

`closed_by_id` is absent when the form closed itself. With
`results` of `after_close`, this is the first event whose `form`
carries the tally.

### Members

**`member.joined`** (stored). An account joined or was added.

```json
{"type": "member.joined", …,
 "member": { Member },
 "by_id": "0193a001-…",
 "conversation": { Conversation }}
```

`by_id` is absent when the account joined by itself, and when a group
brought it in. `conversation` is present only on the frames sent to
the account that joined, with its `membership`, so its other clients
can show the conversation at once. For a new ad-hoc chat that
`conversation` carries no `member_ids`; the client reads the members
with `listMembers`. The joining account receives this
as its first event from the conversation.

**`member.left`** (stored).

```json
{"type": "member.left", …,
 "account_id": "0193a002-…",
 "by_id": "0193a001-…",
 "reason": "removed"}
```

`reason` is `left`, `removed`, `group` (they left a group, or the
group was taken out of the channel) or `account_removed`. The account
that left receives this as its last event from the conversation.

**`member.level_changed`** (stored).

```json
{"type": "member.level_changed", …, "member": { Member }, "by_id": "0193a001-…"}
```

**`channel_group.added`**, **`channel_group.changed`** and
**`channel_group.removed`** (stored). A group was added to the
channel at a level, its level was changed, or it was taken out.

```json
{"type": "channel_group.added", …,
 "group": {"conversation_id": "…", "group_id": "0193a0d1-…", "level": "member", "added_by_id": "…", "added_at": "…"}}
```

These stand in for one event per person: no `member.joined` or
`member.left` is sent for the group's members, and clients showing
the member list fetch it again. Each account brought in receives the
event with `conversation` beside it, as for `member.joined`; each
account taken out receives `member.left` with reason `group`. When
one person later joins or leaves the group, `member.joined` or
`member.left` is sent for them in each of the group's channels.

### The conversation itself

**`conversation.updated`** (stored). Name, topic, welcome text,
default level, section, parent, retention or slow mode changed; a public
channel was made private; or a private channel was locked
(`locked_at` is set, for good).

```json
{"type": "conversation.updated", …, "conversation": { Conversation }, "by_id": "0193a001-…"}
```

`conversation.membership` is the receiving account's own.
`conversation.parent_id` is set while the channel is a sub-channel
and absent once it is top-level again. A change of parent is told to
the sub-channel's members only; nothing is sent in the parent. When a
channel goes private, people who are not members get no event: they
were receiving none, and the channel is simply gone from what they
can read.

**`conversation.archived`** and **`conversation.unarchived`**
(stored). The same fields as `conversation.updated`.

**`pin.added`** and **`pin.removed`** (stored).

```json
{"type": "pin.added", …,
 "pin": {"message_id": "0193a6f2-…", "pinned_by_id": "0193a001-…", "pinned_at": "…"}}
{"type": "pin.removed", …, "message_id": "0193a6f2-…", "by_id": "0193a001-…"}
```

**`thread.moved`** (stored). A thread was moved out into a channel
of its own.

```json
{"type": "thread.moved", …,
 "parent": { Message },
 "to_conversation_id": "0193b200-…",
 "pointer": { Message },
 "by_id": "0193a001-…"}
```

Sent in the old conversation. The parent stays there untouched; a
copy of it is the first message of the new channel, and the replies
have moved after it. Clients drop every reply to `parent` and put
`pointer` in their place: a system message with `parent_id` set to
the parent and `system` of
`{"kind": "thread_moved", "conversation_id": "<new channel>"}`.
`parent.thread` now counts that one reply, and the old thread
accepts no more. `pointer.seq` is this event's `seq`. The thread's
followers, who are the new channel's first members, receive
`member.joined` there with the conversation.

**`typing.started`** (not stored).

```json
{"type": "typing.started", "at": "…", "conversation_id": "…",
 "account_id": "0193a001-…", "parent_id": "0193a6f2-…"}
```

`parent_id` only when typing in a thread. It lasts eight seconds
unless it is sent again; there is no "stopped" event. A message from
the same account ends it. Not sent to the account that is typing, and
not sent for accounts the receiver has blocked.

**`file.changed`** (not stored). Only in a conversation shared with
other servers. A file from another server arrived here, or could not
be fetched. Each server fetches its own copy, so this is local and
takes no `seq`.

```json
{"type": "file.changed", "at": "…", "conversation_id": "…",
 "message_id": "0193a6f2-…", "file": { File }}
```

`file.state` is `ready`, `arriving`, `unavailable` or `too_large`.
Only a `ready` file has a `url`.

**`stream.channel_changed`** (not stored). Only in a conversation
that is this server's copy of a channel a hub owns
(`LINK-PROTOCOL.md` section 23a). The hub signed a new record of the
channel: its name or description on the hub, its listing mode, its
servers or its moderators changed; or the channel's home moved to
another hub of the cluster (`LINK-PROTOCOL.md` section 23b), in which
case `channel.hub` names the new home. Local, and takes no `seq`.

```json
{"type": "stream.channel_changed", "at": "…", "conversation_id": "…",
 "stream_id": "0193d100-…", "channel": { HubChannel }}
```

`channel` is the `HubChannel` of `getHubChannel` as every member is
shown it: `invited` and `requests` are empty here, and a moderator
or the owner reads them with `getHubChannel`. A client works out
whether its own account is a moderator from `channel.moderators`
(its account id, and its own server's name). The conversation's own
`shared.owner_server` and `shared.servers` change with a
`conversation.updated` as before.

**`stream.move_changed`** (not stored). Only in a conversation
shared over a direct link, or one that has just left it. A move of
the channel onto a hub (`LINK-PROTOCOL.md` section 21a) was
proposed, answered, committed, finished or abandoned. Local, and
takes no `seq`.

```json
{"type": "stream.move_changed", "at": "…", "conversation_id": "…",
 "stream_id": "0193d100-…", "move": { StreamMove }}
```

`move` is the `StreamMove` of `listStreamMoves`. Every member of the
local channel receives it; only the owner can act on it
(`acceptStreamMove`, `declineStreamMove`, `cancelStreamMove`). While
`move.state` is `accepted` or `committed` a message a member posts
may stay `unsent` for a moment longer than usual; nothing is
refused. When it becomes `done` the conversation's
`shared.owner_server` is the hub's name.

**`sealed.epoch_started`** (stored). Section 12.

### Calls

`CALLS.md` is the full description of calls. This is the part of it
that every member of the conversation receives, so the header can
show that a call is on. None of these is stored or replayed. The
start and end of a call also change one system message, which is
stored. A client that connects during a call finds it in
`conversation.call`.

```json
{"type": "call.started", "at": "…", "conversation_id": "…", "call": { Call }}

{"type": "call.ended", "at": "…", "conversation_id": "…", "call_id": "0193b100-…",
 "reason": "empty", "duration_seconds": 1284}

{"type": "call.participant_joined", "at": "…", "conversation_id": "…", "call_id": "0193b100-…",
 "participant": {"account_id": "0193a001-…", "device_id": "0193a0d1-…", "joined_at": "…"}}

{"type": "call.participant_left", "at": "…", "conversation_id": "…", "call_id": "0193b100-…",
 "account_id": "0193a001-…", "reason": "left"}
```

- `call.ended` `reason`: `empty`, `alone`, `too_long`, `closed` or
  `server`.
- `call.participant_left` `reason`: `left`, `timeout`,
  `joined_elsewhere`, `removed` or `signed_out`.
- `participant.device_id` is the device the account joined on (a
  `CallParticipant`). It is left out when the session has no device,
  and for a participant another server relays.

Events that go only to the people in the call, and the ring events,
are in section 11.

## 9. Account events

These count in no conversation's numbering and have no `seq`, though
some name a conversation in `conversation_id`. None is stored. Each
entry says who receives it. "Own connections" means every connection of
the account concerned.

"Accounts that can see them" means: accounts that share a
conversation with the account concerned. Presence, status,
availability and the do not disturb mark also go to linked servers on
the rule in DESIGN.md section 11, and arrive there as the same
events for the shadow account.

### Presence and status

**`presence.changed`**. To accounts that can see them, and own
connections.

```json
{"type": "presence.changed", "at": "…", "account_id": "0193a001-…", "presence": "away", "in_call": false}
```

**`presence.batch`**. To the same accounts as `presence.changed`, in
its place, when the server is merging presence (section 5) and after
`ready` (section 2). `changes` is a list of `presence.changed`
objects, exactly as each would have been sent by itself, at most one
for an account and at most 2,000 in a frame; `at` is when the frame
was put together.

```json
{"type": "presence.batch", "at": "…", "changes": [
  {"type": "presence.changed", "at": "…", "account_id": "0193a001-…", "presence": "online", "in_call": false},
  {"type": "presence.changed", "at": "…", "account_id": "0193a002-…", "presence": "away", "in_call": false}
]}
```

A client applies each entry as it applies `presence.changed`, in any
order, and redraws once. A connection is only ever sent entries for
accounts it would have been sent `presence.changed` for: the rule on
who can see an account is applied to each change before it is merged.
An account's presence in a batch is the newest the server had when
the frame was made; a `presence.changed` or `presence.batch` that
arrives later replaces it. A client that does not know
`presence.batch` ignores it like any unknown event, and then shows
presence that is out of date until it next loads the account.

**`status.changed`**. To accounts that can see them, and own
connections. `status` is absent when the status was cleared or ran
out.

```json
{"type": "status.changed", "at": "…", "account_id": "0193a001-…",
 "status": {"emoji": "🌴", "text": "On holiday until Monday", "until": "2026-11-16T08:00:00.000Z",
            "pause_notifications": true, "appear_away": true}}
```

**`availability.changed`**. To accounts that can see them, and own
connections. `availability` is absent when it was cleared or ran out.

```json
{"type": "availability.changed", "at": "…", "account_id": "0193a001-…",
 "availability": {"value": "meeting", "until": "2026-11-13T11:00:00.000Z", "set_by": "integration"}}
```

**`dnd.changed`**. To accounts that can see them, and own
connections. Sent when the mark goes on or off for any reason: a
manual do not disturb, quiet hours starting or ending, a status that
pauses notifications, or a meeting with that preference.

```json
{"type": "dnd.changed", "at": "…", "account_id": "0193a001-…", "dnd": true,
 "dnd_state": {"on": true, "sources": ["manual", "quiet"],
               "manual_until": "2026-11-13T12:00:00.000Z"}}
```

`dnd_state` is present only on own connections, and lists the
sources still on: `manual`, `status`, `meeting`, `quiet`. Own
connections also get the event when the detail changes while the
mark stays on. Other people are never told why the mark is on.

### Accounts and groups

**`account.updated`**. A profile changed: username, display name,
avatar, title, pronouns, time zone, role. To accounts that can see
them, and own connections.

```json
{"type": "account.updated", "at": "…", "account": { Account }}
```

**`account.removed`**. To accounts that can see them. `account` has
`removed_at` set. The removed account's own connections are closed
with 4003 instead.

```json
{"type": "account.removed", "at": "…", "account": { Account }}
```

**`me.updated`**. Something only the account sees changed:
preferences, quiet hours, appear away, the passkey count, the
authenticator app. To own connections.

```json
{"type": "me.updated", "at": "…", "account": { Me }}
```

**`group.created`**, **`group.updated`**, **`group.deleted`**. To
every owner, admin and member, and to guests and bots that can see
the group. `group.is_member` is the receiver's own.

```json
{"type": "group.updated", "at": "…", "group": { Group }}
```

**`group.member_added`** and **`group.member_removed`**. To the same
accounts, and to the account concerned.

```json
{"type": "group.member_added", "at": "…", "group_id": "0193a0d1-…", "account_id": "0193a002-…", "group": { Group }}
```

**`block.added`** and **`block.removed`**. To own connections. The
blocked account is told nothing.

```json
{"type": "block.added", "at": "…", "account_id": "0193a009-…"}
```

### Reading, modes and threads

**`read_position.moved`**. The account read a conversation on one
device, or marked it unread. To own connections.

```json
{"type": "read_position.moved", "at": "…",
 "conversation_id": "0193a6f0-…",
 "read_seq": 4821,
 "unread_count": 0, "mention_count": 0, "badge_count": 0,
 "total": {"badge": 3, "mentions": 1, "unread_conversations": 2, "unread_threads": 1}}
```

**`membership.updated`**. The account changed its `mode`, mute,
`show`, star or thread view for a conversation, or a mute ran out.
To own connections.

`membership.mode` is `any`, `mentions` or `nothing`, and decides
what notifies. `membership.show` is `always`, `activity`,
`notification` or `hidden`, or absent for the app's own default, and
decides only whether the conversation has a row in the sidebar. The
server stores `show` and does not work out visibility.

```json
{"type": "membership.updated", "at": "…", "conversation_id": "0193a6f0-…", "membership": { Membership }}
```

**`thread_state.changed`**. The account followed or ignored a
thread, was made a follower by replying or being mentioned, went back
to "neither", or moved its read position in the thread. To own
connections.

```json
{"type": "thread_state.changed", "at": "…",
 "parent_id": "0193a6f2-…", "conversation_id": "0193a6f0-…",
 "thread_state": {"parent_id": "0193a6f2-…", "conversation_id": "0193a6f0-…",
                  "state": "following", "read_seq": 4830, "unread_count": 0}}
```

For "neither", `thread_state` is absent and the client drops its
row.

**`bookmark.added`** and **`bookmark.removed`**. To own connections.

```json
{"type": "bookmark.added", "at": "…", "message_id": "0193a6f2-…", "created_at": "…"}
```

### Notifications and badges

The server decides what is worth notifying, from the conversation's
mode, thread states, blocks, do not disturb and notify anyway
(DESIGN.md section 3, PUSH.md section 2). It tells a connected client
in two places, and there is no separate "notification" event:

- `notify` on each `message.created` (section 8), for messages.
- `call.ring_started` (section 11), for a call that rings.

**`badge.changed`**. The account's counts for a conversation went up.
To own connections, and only when a message counts for the badge
(it notifies under the conversation's mode, or mentions the account
by name); a message that only adds to the unread count does not send
it. Counts going down come with `read_position.moved`.

```json
{"type": "badge.changed", "at": "…",
 "conversation_id": "0193a6f0-…",
 "unread_count": 4, "mention_count": 1, "badge_count": 1,
 "total": {"badge": 4, "mentions": 2, "unread_conversations": 3, "unread_threads": 1}}
```

The three counts are the ones in the conversation's `membership`.
`mode` and `show` are independent: a hidden conversation still
notifies, still gets `badge.changed`, and still counts in `total`.
`total` holds the same four numbers `getStatusSummary` returns, so
every client and the icon badge agree. `read_position.moved` carries
`total` in the same shape.

### Scheduled items

**`scheduled.created`**, **`scheduled.updated`**,
**`scheduled.cancelled`**. To the owner's connections.

```json
{"type": "scheduled.updated", "at": "…", "scheduled": { ScheduledItem }}
```

**`scheduled.fired`**. A scheduled message was sent, a reminder
arrived or a recurring form was posted. To the owner's connections.

```json
{"type": "scheduled.fired", "at": "…", "scheduled": { ScheduledItem }, "message_id": "0193a6f2-…"}
```

`scheduled.next_run_at` is the next run, or absent if it has
finished. If posting failed (the owner left the channel, it was
archived), `message_id` is absent and `error` holds an error object.

### Moderation

**`timeout.started`** and **`timeout.lifted`**. To the account timed
out, to the managers of the channel, and for a server-wide time-out
to the admins.

```json
{"type": "timeout.started", "at": "…", "timeout": { Timeout }}
```

**`report.created`** and **`report.handled`**. To whoever may handle
the report: the channel's managers, and the admins too if the channel
is public.

```json
{"type": "report.created", "at": "…", "report": { Report }}
```

**`purge_request.created`** and **`purge_request.decided`**. To the
admins only. The author is not told the outcome.

```json
{"type": "purge_request.created", "at": "…", "purge_request": { PurgeRequest }}
```

**`webhook.requested`** and **`webhook.decided`**. The first goes to
whoever may approve it: the admins, and the channel's managers if the
setting allows. The second goes to the requester and the same people.

```json
{"type": "webhook.decided", "at": "…", "webhook": { Webhook }}
```

### The server

**`server.updated`**. Something in `getServer` changed: a feature
switch, a limit, the sign-in methods, the default themes
(`setDefaultTheme`). To every connection.

```json
{"type": "server.updated", "at": "…", "server": { Server }}
```

**`sections.changed`**. To every owner, admin and member.

```json
{"type": "sections.changed", "at": "…", "sections": [ { Section } ]}
```

**`emoji.added`**, **`emoji.updated`**, **`emoji.removed`**. To
every connection of a local account.

```json
{"type": "emoji.added", "at": "…", "emoji": { Emoji }}
```

**`theme.published`**. A theme entered the server's theme library
(`publishTheme`), or a theme in it changed (`updateTheme`). To every
connection. A client showing the library adds or replaces it.

```json
{"type": "theme.published", "at": "…", "theme": { Theme }}
```

**`theme.unpublished`**. A theme left the library: its owner took it
out (`unpublishTheme`) or deleted it (`deleteTheme`), or an admin
removed it (`removeLibraryTheme`). To every connection. Copies people
added are theirs and stay.

```json
{"type": "theme.unpublished", "at": "…", "theme_id": "…"}
```

A change to the default themes is `server.updated`. Changes to one's
own themes have no event: the apps fetch `listMyThemes` when the
theme picker opens and after a resume.

### Answers from bots

**`interaction.responded`**. A bot responded to something the person
did: a command, a button press, or an answer to a `bot` form or
dialog. To every connection of that one person, and to nobody else.

```json
{"type": "interaction.responded", "at": "…",
 "interaction_id": "0193c100-…",
 "conversation_id": "0193a6f0-…",
 "parent_id": "0193a6f2-…",
 "bot_id": "0193a0b0-…",
 "response": {"type": "private", "text": "Deploying `api` to staging.", "cards": []}}
```

`parent_id` is set when the person acted in a thread. `response` is
one of:

| `response.type` | Other fields      | What the client does                    |
|-----------------|-------------------|-----------------------------------------|
| `message`       | `message_id`      | Stops waiting. The message itself arrives as `message.created` for everyone. |
| `private`       | `text`, `entities`, `cards` | Draws it in the conversation where the person asked, marked "only you can see this". It is not a message: it has no identifier and no `seq`, is not stored, is not in catch-up, search or the export, and is gone when the client restarts. |
| `dialog`        | `dialog`: a `Dialog` | Shows the form in a dialog, on the client where the person acted. Submitting it is `submitDialog`; closing it is `dismissDialog`. |
| `none`          |                   | Stops waiting. Nothing is shown.        |

A client shows "Waiting for *bot*…" from the moment it sends a
command or presses a button until the first response, and "*bot* did
not answer" after ten seconds. That is display only: a later response
within the interaction's 15 minutes is still delivered.

### Sessions and devices

**`session.created`** and **`session.ended`**. To own connections.
A person sees at once when a new session starts on their account,
and above all a recovery session.

```json
{"type": "session.created", "at": "…", "session": { Session }}
```

**`device.retired`**. A device of the account had no session for 30
days, or its session was revoked. To own connections. An unlocked
device then publishes a device list without it.

```json
{"type": "device.retired", "at": "…", "device": { Device }}
```

The sealed-chat events to an account (`device_list.changed`,
`unlock_request.created`, `unlock_request.answered`,
`backup.changed`) are in section 12.

## 10. Events only bots receive

`BOTS.md` describes bots in full. A bot gets the two events below on
every connection it has open. They have no `seq` and are never
stored. If the bot has no connection when one would be sent, the
person's request fails with `unavailable` and nothing is queued.

Each carries an `interaction_id`, which names one request from one
person to the bot. An interaction lasts 15 minutes, takes at most
five responses, and is held in memory: a server restart ends it.

**`command.invoked`**. Someone ran one of the bot's commands in a
conversation the bot is in. Nothing was posted, and the other members
see nothing.

```json
{"type": "command.invoked", "at": "…",
 "interaction_id": "0193c100-…",
 "conversation_id": "0193a0c7-…",
 "parent_id": "0193a6f2-…",
 "account_id": "0193a001-…",
 "command": "deploy",
 "text": "api to staging",
 "entities": []}
```

`command` is the name without the slash. `text` is everything after
it, trimmed. `entities` is what the server resolved in `text`, as for
a message. `parent_id` is the thread it was typed in, if any.

**`form.answered`**, for a form the bot posted with the `bot` action,
and for a dialog it opened. Someone answered it or pressed one of its
buttons. The answer is stored nowhere else, and the other members
receive no event.

```json
{"type": "form.answered", "at": "…",
 "interaction_id": "0193c300-…",
 "conversation_id": "0193a0c7-…",
 "message_id": "0193c200-…",
 "form_id": "0193c201-…",
 "account_id": "0193a002-…",
 "button_id": "go",
 "answers": {"reason": "Hotfix for the login bug"}}
```

- No `seq` and no `form`: this is not the conversation event of the
  same name in section 8.
- `account_id` is absent when the form is anonymous. The bot can
  still respond, and the server delivers the response to the right
  person.
- `button_id` is absent when the person answered without a button.
  `answers` is empty for a form with no fields.
- For a dialog, `message_id` is absent, `form_id` is the dialog's
  identifier and `"dialog": true` is present. Dismissing a dialog
  sends the bot nothing.
- The server checks the answer against the form first. The bot never
  receives one that does not fit.

A bot also receives every ordinary event of the channels it is in,
reactions included, like any member.

### How a bot answers

One operation, `respondToInteraction`
(`POST /api/v1/interactions/{interaction_id}/responses`), with one of
four types:

| `type`    | Other fields       | What happens                           |
|-----------|--------------------|----------------------------------------|
| `message` | `message`          | Posted in the conversation as the bot, for everyone, as `postMessage` would. This is how a bot answers for everyone. |
| `private` | `text`, `cards`    | Shown to the person who asked and to nobody else. This is how a bot replies only to one person. |
| `dialog`  | `form`             | The person is shown a form in a dialog. Their answer comes back as `form.answered` with a new `interaction_id`. One dialog per interaction. |
| `none`    |                    | Nothing is shown. Tells the person's client to stop waiting. |

The person's clients learn of each response through
`interaction.responded` (section 9).

A bot may also act without responding, with ordinary operations:
post, edit its own message, react. It should still send `none`. A
bot cannot send a private response or open a dialog without an
interaction: both are always an answer to something a person did.

## 11. Call signalling

`CALLS.md` section 10 is the full description: every field, the two
media connections, simulcast layers and reconnection. This section
lists the frames so the live connection is described in one place.
Where the two differ, `CALLS.md` is right.

A call's media goes between each client and the server over WebRTC.
What sets it up travels on the live connection.

1. `joinCall` over HTTP checks permission, starts the call if there
   is none, and answers with the `Call` and a `ticket`.
2. The client sends `call.join` with the ticket. From then on that
   connection is the participant's signalling connection.
3. Offers and answers follow, on two media connections named by `pc`:
   `send` for what the client publishes (the client offers) and
   `recv` for what it receives (the server offers).

**Requests** go from client to server, each with an `id`. The server
answers `call.ok` or `call.error` with `reply_to`, except that a
`call.offer` is answered with `call.answer`.

| `type`           | Fields                                    | Purpose |
|------------------|-------------------------------------------|---------|
| `call.join`      | `call_id`, `ticket`, `client`: `{"simulcast", "video_codecs", "frame_encryption"}` | Enter the call. Answered with `call.ok` holding `call`, `resume_token` and `limits`. |
| `call.offer`     | `pc`: `send`, `sdp`, `ice_restart`, `tracks`: `[{"mid", "source"}]` | The client adds or removes a track, or restarts ICE. Answered with `call.answer`: `pc`, `sdp`, `tracks`: `[{"mid", "track_id"}]`. |
| `call.answer`    | `pc`: `recv`, `offer_id`, `sdp`           | Answers an offer from the server. |
| `call.candidate` | `pc`, `candidate`, `sdp_mid`, `sdp_mline_index` | One per ICE candidate the client finds; `candidate` of `null` when done. The server never sends candidates. |
| `call.subscribe` | `video`: `[{"track_id", "width", "height"}]` | Track subscriptions: whose video the client is showing and at what size. Each frame replaces the last. Audio is always forwarded. Answered with `call.ok` holding `video`: `[{"track_id", "state", "layer"}]`. |
| `call.mute`      | `source`, `muted`                         | Mute or unmute one of the client's own tracks. Turning a camera off is a mute. |
| `call.restart`   | `pc`: `recv`                              | Ask for a fresh offer with new ICE credentials. |
| `call.resume`    | `call_id`, `resume_token`                 | Take the call up again on a new live connection. Answered like `call.join`. |
| `call.decline`   | `call_id`                                 | Decline a ring. |
| `call.leave`     | `call_id`                                 | Leave. |

```json
{"type": "call.join", "id": "c1", "call_id": "0193b100-…", "ticket": "kq3v…",
 "client": {"simulcast": true, "video_codecs": ["vp8", "h264"], "frame_encryption": true}}

{"type": "call.offer", "id": "c2", "pc": "send", "sdp": "v=0\r\n…", "ice_restart": false,
 "tracks": [{"mid": "0", "source": "microphone"}, {"mid": "1", "source": "camera"}]}

{"type": "call.answer", "reply_to": "c2", "pc": "send", "sdp": "v=0\r\n…",
 "tracks": [{"mid": "0", "track_id": "t7"}, {"mid": "1", "track_id": "t8"}]}

{"type": "call.subscribe", "id": "c5",
 "video": [{"track_id": "t8", "width": 640, "height": 360}]}
```

**From the server**, to the people in the call unless a row says
otherwise. The events have `at`, `conversation_id` and `call_id`, no
`seq`, and are never stored.

| `type`                    | To                  | Other fields                    |
|---------------------------|---------------------|---------------------------------|
| `call.offer`              | One connection      | `pc`: `recv`, `offer_id`, `sdp`, `ice_restart`, `tracks`: `[{"mid", "track_id", "account_id", "device_id"}]`. A frame, not an event: no `at`. |
| `call.ok`, `call.error`   | One connection      | `reply_to`. `call.error` has `code` and `message`; the codes are in CALLS.md. |
| `call.track_published`    | Participants, the publisher included | `account_id`, `device_id`, `track`: a `CallTrack` |
| `call.track_unpublished`  | Participants        | `account_id`, `track_id`. Not sent for each track of a participant who leaves: `call.participant_left` stands for them all |
| `call.mute_changed`       | Participants        | `account_id`, `track_id`, `source`, `muted` |
| `call.speakers_changed`   | Participants        | `account_ids`, loudest first    |
| `call.layer_changed`      | One subscriber      | `track_id`, `state`, `layer`, `reason` |
| `call.demand_changed`     | One publisher       | `track_id`, `layers`            |
| `call.declined`           | Participants        | `account_id`                    |
| `call.ring_started`       | The person being rung, every connection | `from_account_id`, `until` |
| `call.ring_stopped`       | The person who was being rung | `reason`: `joined_elsewhere`, `declined_elsewhere`, `ended` or `timeout` |

`call.started`, `call.ended`, `call.participant_joined` and
`call.participant_left` go to every member of the conversation and
are in section 8.

Notes:

- Who is rung, and when a ring is held back by a mode or by do not
  disturb, is in CALLS.md section 15. `call.ring_started` is the
  call's counterpart of `notify` on a message.
- An account is in a call on one device. `call.join` from a second
  device moves the call there, and the first receives
  `call.participant_left` for itself with reason `joined_elsewhere`.
- In a sealed chat the call's `sealed` is true and the client must
  say `frame_encryption` in `call.join`. Clients encrypt each media
  frame with keys derived from the epoch secret, so the server forwards
  what it cannot decode.
- `device_id` names the device a participant is in the call on: in
  each `CallParticipant` (the answer to `call.join` and `call.resume`,
  `conversation.call`, `call.started`, `call.participant_joined`), in
  `call.track_published` and beside each track of a `call.offer`. In a
  sealed chat's call it says whose call sender key decrypts a track
  (CALL-FRAMES.md section 1); a receiver goes by the one on the track,
  because the call can move to the account's other device without a
  new `call.participant_joined`. It is left out when the session has
  no device, and for a participant another server relays.
- In a shared channel the call lives on the server of whoever started
  it. A member of another server sends the same frames to their own
  server, which passes them to the hosting server as sealed direct
  frames between the two servers (LINK-PROTOCOL.md). A client never
  opens a live connection to a server that is not its own.
- A ring reaches a phone as an ordinary wake-up. On iPhone it is an
  ordinary notification with a ring sound; there is no VoIP push.

## 12. Sealed chats

`SEALED.md` is the authority on sealed chats, and its names are used
here. There are no sealed-chat frames: a client does everything
through operations and learns of changes through the events below.

What a device sends, all over HTTP:

| To do this                          | Operation                         |
|-------------------------------------|-----------------------------------|
| Start a sealed chat                 | `openSealedChat`, which carries epoch record 1 in the same request. |
| Start a new key epoch               | `postEpochRecord`: the next `key_epoch` and the record. |
| Catch up on key epochs              | `listEpochRecords` from a given `key_epoch`. Records are kept for the life of the chat. |
| Send text, a reaction, a tick, an edit, a delete | `postMessage` with `client_id`, `sealed`, `quiet` and `file_ids`. |
| Ask another device for the backup key | `createUnlockRequest`; the other device answers with `answerUnlockRequest`. |

`key_epoch` is a plain counter starting at 1. A sealed message whose
`sealed.key_epoch` is not the conversation's current one is refused
with `conflict`, and the device encrypts it again in the current
epoch.

**`sealed.epoch_started`** (stored conversation event). To the
devices of both accounts.

```json
{"type": "sealed.epoch_started", "at": "…", "conversation_id": "0193a0e0-…", "seq": 78,
 "epoch": {"conversation_id": "0193a0e0-…", "key_epoch": 12, "seq": 78,
           "account_id": "0193a000-…", "device_id": "0193aa00-…",
           "record": "base64…", "created_at": "…"}}
```

- `epoch` is the `EpochRecord` in full. `record` is made and signed
  by one device and holds a fresh epoch secret sealed separately to
  every device on both accounts' current device lists. The server
  cannot open it.
- There is one record per `key_epoch`, and only the next number is
  accepted. A second record for the same number is refused with
  `conflict`, and the device that lost is handed the winning record.
- In a sealed chat with someone on another server, a device there
  may have made a record for the same `key_epoch` at the same moment,
  before either server had the other's. The one whose event carries
  the earlier stamp stands (`LINK-PROTOCOL.md` section 11). If that
  is the other server's, this event is sent again for the same
  `key_epoch` with `"replaced": true` beside `epoch`: the record in
  it takes the place of the one the devices here were given. A
  device that made the record that gave way does what it does when
  it loses the race on its own server: it takes this record, and
  sends again, under it, what it had sent under its own.
- Every message after it in the sequence is in the new epoch.
- A device starts a new epoch when either account's device list
  changes, when it has just been unlocked and must include itself,
  and when the current epoch is more than seven days old at the time
  someone sends.
- It does not age out: `listEpochRecords` returns every record the
  chat has had, so a device that is behind by any amount catches up.
  A device that was not yet listed when an epoch was made reads that
  epoch's secret from its account's backup.

**`device_list.changed`**. An account published a new version of its
signed device list. To own connections and to the other account of
each of its sealed chats. Clients fetch the versions after their pin
with `listDeviceListVersions`, check each step, and draw the notice
(a device added or removed, a key change, a reset).

```json
{"type": "device_list.changed", "at": "…", "account_id": "0193a001-…", "version": 7}
```

**`unlock_request.created`**. A new, locked device of the account
asked for the backup key. To the account's other connections.

```json
{"type": "unlock_request.created", "at": "…", "unlock_request": { UnlockRequest }}
```

- The request is stored and lasts ten minutes. The two devices do
  not need to be connected at the same moment.
- `unlock_request.request` carries the asking device's sign key, box
  key and proof.
- An unlocked device answers only after the person has scanned the
  new device's QR code with it or typed the 20 characters into it,
  and the code it computes from the request matches. Approving on
  the device's name alone is not allowed: the code is the only thing
  that stops the server swapping the new device's keys for its own.

**`unlock_request.answered`**. To the connection of the asking
device. The answer is a locked copy of kind `device`, sealed to its
box key.

```json
{"type": "unlock_request.answered", "at": "…", "unlock_request": { UnlockRequest }}
```

**`backup.changed`**. The account's backup changed: it was created,
a locked copy was added or removed, a passkey gained an unlock key,
the backup key was replaced or reset, or a replacement became due. To own connections. Devices
fetch it again with `getBackup`.

```json
{"type": "backup.changed", "at": "…", "key_version": 2, "replacement_due": false}
```

## Summary of event types

Stored conversation events (with `seq`):
`message.created`, `message.edited`, `message.deleted`,
`message.purged`, `message.history_restored`, `preview.added`,
`preview.removed`, `reaction.added`, `reaction.removed`,
`task.changed`, `form.answered`, `form.answer_withdrawn`,
`form.option_added`, `form.closed`, `member.joined`, `member.left`,
`member.level_changed`, `channel_group.added`,
`channel_group.changed`, `channel_group.removed`,
`conversation.updated`, `conversation.archived`,
`conversation.unarchived`, `pin.added`, `pin.removed`,
`thread.moved`, `sealed.epoch_started`, `message.placed`,
`message.sent`.

Conversation events that are not stored:
`typing.started`,
`file.changed`, `stream.channel_changed`, `stream.move_changed`,
`call.started`, `call.ended`,
`call.participant_joined`, `call.participant_left`, and to
participants only `call.track_published`, `call.track_unpublished`,
`call.mute_changed`, `call.speakers_changed`, `call.layer_changed`,
`call.demand_changed`, `call.declined`.

Account events:
`presence.changed`, `presence.batch`, `status.changed`, `availability.changed`,
`dnd.changed`, `account.updated`, `account.removed`, `me.updated`,
`group.created`, `group.updated`, `group.deleted`,
`group.member_added`, `group.member_removed`, `block.added`,
`block.removed`, `read_position.moved`, `membership.updated`,
`thread_state.changed`, `bookmark.added`, `bookmark.removed`,
`badge.changed`, `call.ring_started`, `call.ring_stopped`,
`scheduled.created`, `scheduled.updated`, `scheduled.cancelled`,
`scheduled.fired`, `timeout.started`, `timeout.lifted`,
`report.created`, `report.handled`, `purge_request.created`,
`purge_request.decided`, `webhook.requested`, `webhook.decided`,
`server.updated`, `sections.changed`, `emoji.added`, `emoji.updated`,
`emoji.removed`, `theme.published`, `theme.unpublished`, `interaction.responded`, `session.created`,
`session.ended`, `device.retired`, `device_list.changed`,
`unlock_request.created`,
`unlock_request.answered`, `backup.changed`.

Bots only:
`command.invoked`, and `form.answered` for `bot` forms and dialogs.

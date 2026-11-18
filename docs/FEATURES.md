# Features

The built-in features in enough detail to build them. `DESIGN.md`
says what each feature is for; this document says what the server
does, step by step, what it stores and which events it sends.

- Who may do each thing is in `PERMISSIONS.md` and is not repeated
  here beyond a reminder.
- Tables and columns are those of `schema.sql`, explained in
  `SCHEMA.md`.
- Events are named as in `EVENTS.md`, which defines their fields.
  Section 27 lists every name used here.
- API operations are described in words. Their paths and fields are
  in `openapi.yaml`.
- Where the design left a gap, the simplest choice was made. Those
  are marked "(decision Fn)" and listed in section 28.

Every stored change to a conversation takes the next `seq` from
`conversations.next_seq` and writes one row to `events`. That is not
repeated under each feature.

## 1. Threads

A thread is a top-level message (the **parent**) and the replies
that hang under it. Threads are one level deep. There is one way to
reply and no "also send to channel".

### 1.1 Reply with a quote

The request to post a message may name a message being answered and,
optionally, a range of that message's text.

1. If the named message is top-level, it is the parent. If it is a
   reply, the parent is that reply's parent. The new message's
   `parent_id` is the parent either way.
2. `quote_message_id` is the message that was answered: the parent
   or the reply. `quote_start` and `quote_end` are the selected
   range in UTF-16 code units, or null for the whole message. The
   range must lie inside the quoted text as it is now
   (`invalid_value` otherwise).
3. The quoted text itself is not stored. The server fills in
   `quote.text` each time it shows the reply, from the quoted
   message's current text: the range if there is one, otherwise the
   first 200 characters; absent when the message is deleted or the
   range no longer exists, and clients then draw "original message
   changed". Someone who may not open the quoted message (a newcomer
   to a private channel, PERMISSIONS.md section 4.4) reads the quote
   from `quote.text`; their client says it is a message they can't
   see and offers nothing to jump to.
4. The parent's `reply_count`, `last_reply_seq`, `last_reply_order`
   and `last_reply_at` are updated in the same transaction.
5. Thread states are updated (1.4): the replier follows the thread;
   on the first reply the parent's author follows it too.

A bot or webhook replies the same way, by naming the parent.

A reply naming a message in another conversation is refused with
`not_found`, and a reply to a system notice with `invalid_request`.
Replying in the thread of a deleted parent is allowed; the thread
goes on.

In a sealed chat the parent and quote are inside the ciphertext and
the server sees a flat stream. Sections 1.2 to 1.6 are then the
clients' business alone, worked out from what the devices hold.

Stored: `messages.parent_id`, `quote_message_id`, `quote_start`,
`quote_end`; the summary columns on the parent; `thread_states`.

Events: `message.created`.

### 1.2 Two views

Each person sees threads one of two ways. The choice is
`accounts.prefs.thread_view` (`threaded` by default) and can be
overridden for one conversation in `members.thread_view`. Nobody
sets it for anyone else. The server needs to know it only to count
unread messages (section 2).

| | Threaded | Flat |
|-|----------|------|
| Replies in the conversation | Not shown. The parent shows a summary line: reply count, who replied, time of the last reply. | Shown in the order written, each with a header: what it answers, a link up to that message, a link to the parent, and "open thread". |
| Marker (1.3) | Drawn | Not drawn |
| Opening a thread | Beside the conversation on wide screens, over it on phones | The same |
| Replies and the conversation's unread count | Do not count | Count, except in ignored threads |

Typing in the message box without replying posts a top-level message
in either view. Notifications are the same in both.

The request that lists a conversation's messages returns top-level
messages and replies together in `seq` order, each reply carrying
its `parent_id`. A threaded client leaves the replies out of the
main list and uses them for markers; a flat client shows them. The
request for one thread returns the parent and all its replies.

Stored: `accounts.prefs`, `members.thread_view`.

Events: `me.updated` for the account-wide choice and
`membership.updated` for the override, both to the person's own
connections only.

### 1.3 The moving marker

In the threaded view a thread with replies shows one line in the
conversation at the position of its latest reply, for example "3 new
replies to: Deploy plan for Friday".

- Position: where the message with `seq` equal to the parent's
  `last_reply_seq` would sit, which is the parent's
  `last_reply_order` in the conversation's order. The client needs
  not hold that reply.
- A thread has one marker. A later reply moves it down. The parent
  never moves.
- The count is the number of replies the reader has not seen: those
  with `seq` greater than the reader's position in that thread
  (section 2.1). When the count is zero the marker reads "Replies
  to: …" with no number.
- The title is the first line of the parent's text, cut to 80
  characters.
- No marker is drawn for a thread the reader ignores.
- Selecting it opens the thread.

Nothing is stored for the marker and it has no event of its own. It
does not make the conversation unread and does not notify.

### 1.4 Following, neither, ignored

| State     | Stored as                         | Effect                    |
|-----------|-----------------------------------|---------------------------|
| Following | `thread_states.state = following` | Each reply notifies, subject to the channel mode (section 3.7). The thread has its own read position. |
| Neither   | No row                            | Replies are shown, without notifications. |
| Ignored   | `thread_states.state = ignored`   | Replies are hidden in the flat view, no marker is drawn, replies do not count and do not notify. A mention by name still does. The parent and its summary stay visible. |

The server changes the state by itself in these cases:

| When                                          | Change                           |
|-----------------------------------------------|----------------------------------|
| An account replies in a thread                | It follows, whatever its state was. Its read position is set to its reply. |
| A thread gets its first reply                 | The parent's author follows, if still a member and not already ignoring. |
| A reply mentions an account by name           | It follows, unless it is ignoring the thread. |
| An account leaves the conversation            | Its rows for that conversation are deleted. |

A group, `@channel` or `@here` mention in a reply does not make
anyone follow (decision F1). A new row made by the server starts
with `read_seq` equal to the account's `members.read_seq` at that
moment, so replies it had not seen stay unseen.

People change the state with "Follow thread", "Ignore thread" and
"Stop following", offered on a reply in the flat view, on the
marker, and inside the open thread. Going back to neither deletes
the row.

Opening a thread and reading to its end sends the request to mark
the thread read, which raises `thread_states.read_seq` if a row
exists. For "neither" nothing is stored.

Stored: `thread_states`.

Events: `thread_state.changed`, to the person's own connections.

### 1.5 The Threads view

A list, across all the person's conversations, of the threads they
follow that have unread replies, newest reply first.

A thread is listed when the account has a `following` row for it,
the parent's `last_reply_seq` is greater than the row's `read_seq`,
at least one of those replies is counted (section 2.1), and the
account can still read the conversation. Each entry gives the
parent, the conversation, the number of unread replies and the
latest replies. Reading a thread takes it off the list.

Stored: nothing more. Events: none of its own; clients keep it
current from `message.created` and `thread_state.changed`.

### 1.6 Thread to channel

Moves a thread out into a new named channel. The replies are moved,
not copied, and the move is not undone.

**Who:** the parent's author, at level `member` or above and with a
role that may create channels, or a manager of the channel.
**Refused** when: the conversation is shared, sealed or
archived; the message is a reply, a system notice, or has no
replies (`invalid_request`); the thread was already moved
(`conflict`). The name need not be unused: channel names are not
unique (section 21.8).

Call the old channel C, the parent P with replies r1…rn in `seq`
order, and the new channel D. The design says both that the parent
becomes D's first message and that C keeps the parent. So the
parent is the one thing that ends up in both places: it stays in C
untouched, and D starts with a copy of it (ruling R20). In one
transaction:

1. **Create D** in `conversations`: `kind = channel`, `private` the
   same as C (so a thread from an ad-hoc channel gives a private
   channel), the given `name`, `section_id` and `retention_seconds`
   copied from C, `default_level = member`, `created_by` the mover,
   `moved_from_message_id = P`, `next_seq = 1`. `parent_id` makes D
   a sub-channel of C: it is C if C is a named channel with no
   parent; C's own parent if C is a sub-channel, so D becomes its
   sibling; and null if C is an ad-hoc channel, which can never be a
   parent (section 21.7).
2. **Copy the parent.** A new `messages` row P′ is inserted in D
   with a new identifier and `seq = 1`, and P's `author_id`,
   `created_at`, `edited_at`, `text`, `entities`, `cards`,
   `fallback` and `via` columns. Each file on P is copied to a new
   `files` row with its own copy of the content, thumbnail and video
   poster. P's
   `checklist_items` are copied with their current state and its
   `reactions` are copied. Not copied: P's earlier versions, its
   form and the answers to it, its mentions, pins, bookmarks and
   reports. Those stay with P. If P is deleted, P′ is the same bare
   marker.
3. **Move the replies.** Each reply ri gets `conversation_id = D`,
   `seq = i + 1` and `parent_id = null`. Identifiers, authors,
   `created_at`, text, entities, edit state and history do not
   change. A reply whose `quote_message_id` is P now quotes P′, so
   the quote can be drawn by people who cannot read C. Everything
   that hangs from a reply by its identifier comes with it
   untouched: `message_versions`, `reactions`, `checklist_items`,
   `checklist_history`, `forms` and their answers, `bookmarks`,
   `purge_requests`.
4. **Fix the copies of the conversation.** `files.conversation_id`,
   `reports.conversation_id` and `mentions.conversation_id` become
   D for the moved replies; `mentions.seq` takes the new number and
   `mentions.parent_id` becomes null. On SQLite the
   `conversation_id` in `messages_fts` is updated.
5. **Leave a pointer in C.** A system message is inserted in C as a
   reply to P: `parent_id = P`, the next `seq` of C, `author_id` the
   mover, `via_kind = system`, and `system` set to
   `{"kind": "thread_moved", "conversation_id": D}`. P's thread
   summary becomes `reply_count = 1` with `last_reply_seq`,
   `last_reply_order` and `last_reply_at` those of the pointer. P's thread is now closed:
   a later reply to P is refused with `conflict`, and the client
   sends people to D.
6. **Members of D.** Every account with a `following` row for P that
   is still a member of C and could read P there (in a private
   channel, one whose first join came before P was posted, PERMISSIONS
   section 4.4) gets a `members` row in D: `direct_level`
   their level in C but no higher than `member`, `mode = mentions`,
   and `read_seq` set to the new `seq` of the last moved message
   they had read (their thread position, or 1 if they had read no
   reply). The mover, if a member of C, is added as `manager`
   whether or not they followed. If that leaves no manager, P's
   author is made manager, or failing
   that the longest-standing member of C among those added. Groups
   in C are not carried over.
7. **Clear what no longer applies.** All `thread_states` rows for P
   are deleted. `pins` rows in C for the moved replies are deleted.
   `mentions` rows on the moved replies for accounts that did not
   become members of D are deleted. `events` rows in C that carry
   the moved replies are deleted.
8. **Finish D.** `next_seq = n + 2`, `last_message_at` the
   `created_at` of rn. One `member.joined` event is written in D for
   each initial member, taking the next numbers. No join notices are
   posted.
9. **Tell C.** One `thread.moved` event is written in C under the
   same `seq` as the pointer, naming P, the pointer and D. Clients
   drop the replies they hold and show the pointer. It carries P, so
   it is P's event: a member of a private C who joined after P was
   posted is not sent it.
10. **Audit.** A row is written if the mover is not P's author,
    unless C is an ad-hoc channel.

Afterwards the replies are ordinary top-level messages in D and can
have threads of their own. D is never locked when it is made, even
if C is. Anyone else in C can join D from the pointer, even when D
is private, unless D is later locked: `perm` lets a member of the
conversation that holds `D.moved_from_message_id` join D
(PERMISSIONS section 6). Retention keeps counting from each
message's original `created_at`.

Stored: as above. Events: `thread.moved` in C; `member.joined` in D;
`badge.changed` to anyone whose counts changed.

## 2. Channel modes, unread counts and badges

### 2.1 Positions

Everything is counted from stored positions, by the server, so every
client and the status request agree.

- **Conversation position** `R` = `members.read_seq`. The request to
  mark a conversation read sets it to a given `seq`, anywhere from 0
  to the conversation's latest. Setting it lower is how "mark
  unread" works.
- **Thread position** for a thread T = `thread_states.read_seq` if
  the account has a row for T (following or ignored), otherwise `R`.
- In the **flat view** replies are read in the conversation itself.
  So when a mark-read request arrives for an account whose view of
  that conversation is flat, the server also raises every one of the
  account's `thread_states.read_seq` values in that conversation
  that is below the new `R` up to it. In the threaded view a
  mark-read leaves thread positions alone (decision F2).

A message is **counted** for an account when all of these hold:

- it is not deleted and is not a system notice;
- in a sealed chat, it is not marked quiet (`messages.quiet`: the
  sender's device marks reactions, edits and ticks so);
- the account is not its author, unless it is a reminder
  (`via_kind = reminder`), which counts for its author too;
- the account has not blocked its author.

A counted message is **unread** when it is top-level and its `seq`
is greater than `R`, or it is a reply and its `seq` is greater than
its thread's position.

### 2.2 The sets

For one account in one conversation:

| Set   | Unread counted messages that are…                            |
|-------|--------------------------------------------------------------|
| `TOP` | top-level                                                    |
| `FOL` | replies in threads the account follows                       |
| `NEI` | replies in threads the account neither follows nor ignores   |
| `MEN` | messages with a `mentions` row for the account, leaving out replies in ignored threads unless the row's `kind` is `account` |
| `NAM` | messages with a `mentions` row for the account of `kind = account`, wherever they are |

A message in two sets is counted once: the numbers below are sizes
of unions.

### 2.3 The numbers

A membership has two independent settings. `mode` decides what
notifies and what the badge counts. `show` decides only whether the
conversation has a row in the person's sidebar (2.4). Neither limits
the other.

`members.mode` is `any`, `mentions` or `nothing`. The **mode in
force** is `members.mode`, except that a mute whose `muted_until`
has passed is read as `mode_before_mute`. A mute is `nothing` until
a time.

| Mode in force | View     | `unread_count`      | `badge_count`              | `mention_count` |
|---------------|----------|---------------------|----------------------------|-----------------|
| `any`         | threaded | `TOP`               | `TOP ∪ FOL ∪ MEN`          | `MEN`           |
| `any`         | flat     | `TOP ∪ FOL ∪ NEI`   | `TOP ∪ FOL ∪ NEI ∪ MEN`    | `MEN`           |
| `mentions`    | threaded | `TOP`               | `MEN ∪ FOL`                | `MEN`           |
| `mentions`    | flat     | `TOP ∪ FOL ∪ NEI`   | `MEN ∪ FOL`                | `MEN`           |
| `nothing` (and muted) | threaded | `TOP`       | `NAM`                      | `NAM`           |
| `nothing` (and muted) | flat | `TOP ∪ FOL ∪ NEI` | `NAM`                    | `NAM`           |

- `unread_count` is what "there is something new here" means. It
  depends on the view and not on the mode.
- `badge_count` is the number on the conversation. It counts what
  the mode notifies, plus, under `nothing` and mute, mentions of the
  account by name, which badge without notifying.
- The app's icon badge and the status request's unread total are the
  sum of `badge_count` over **all** the account's conversations,
  whatever their `show`. The status request's mention total is the
  sum of `mention_count`.
- None of the three numbers depends on `show`.

How each mode is drawn, where the conversation has a row:

| Mode in force | Drawn          | Bold when                               |
|---------------|----------------|-----------------------------------------|
| `any`         | Normally       | `unread_count` or `badge_count` is above 0 |
| `mentions`    | Normally       | The same                                |
| `nothing`     | Dimmed         | Never. A name mention still shows its badge. |

A sealed chat has no threads and no mentions the server can see, so
there every message is top-level, `MEN` and `NAM` are empty, and the
`mentions` mode is not offered (`invalid_value`).

A mention in a public channel of someone who has not joined it
notifies them once (section 3.7) and has no badge, because badges
belong to memberships (decision F3).

### 2.4 Mode, mute and show

**Mode and mute**

| Request          | Rows                                                      |
|------------------|-----------------------------------------------------------|
| Set a mode       | `mode` = the mode; `muted_until` and `mode_before_mute` cleared. |
| Mute until T     | If not already muted, `mode_before_mute` = current `mode`. `mode = nothing`, `muted_until = T`. |
| Unmute           | `mode = mode_before_mute`; both mute columns cleared.     |
| The mute runs out | Read as unmuted from that moment. The scheduler then rewrites the row as for Unmute. |

Defaults: `mentions` when joining a named channel, `any` for an
ad-hoc channel or sealed chat. Naming an ad-hoc channel does not
change anyone's mode.

Beside the mode a member may choose to ignore broad mentions in the
conversation (`members.ignore_broad`, section 2.5): `@channel`,
`@here` and `@everyone` then write no mention for them there. It is
set with the membership and is off to begin with.

**Show**

`members.show` says whether the conversation has a row in the
person's sidebar:

| `show`         | The conversation has a row                          |
|----------------|-----------------------------------------------------|
| `always`       | Always                                              |
| `activity`     | Only while `unread_count` is above 0                |
| `notification` | Only while `badge_count` is above 0                 |
| `hidden`       | Never                                               |
| null           | As the app's own default says. That default is a preference kept in the app on the device, not on any server; out of the box it is `always`. |

- The server stores `show` and returns it with the membership. It
  takes no part in any server rule: not notifications, not counts,
  not permissions, not search. The client applies it, using the
  numbers in 2.3.
- The conversation a person is viewing right now is always shown in
  its place in the sidebar, whatever `show` says.
- A conversation with `show = hidden` still notifies as its mode
  says. Its notifications appear in the notifications list and it
  counts in the totals; it just has no row.
- A hidden conversation stays reachable by search, by links, by
  direct navigation, in the channel browser, and through a "show
  hidden" toggle in the sidebar.
- The request sets `show` to one of the four values or back to
  null. It starts null.
- The person stays a member whatever `mode` and `show` are.

Stored: `members.mode`, `mode_before_mute`, `muted_until`, `show`,
`ignore_broad`, `read_seq`; `thread_states.read_seq`; `mentions`.

Events, all to the person's own connections: `membership.updated`
(mode, mute, show, star, view); `read_position.moved` (a position
changed, so reading on one device moves the others; it carries the
new numbers); `badge.changed` (the numbers for a conversation went
up).

### 2.5 Which mentions are stored

When a message is posted or edited the server writes `mentions` rows
from its entities:

| Entity                    | Rows for                                         | `kind`    |
|---------------------------|--------------------------------------------------|-----------|
| `@username`               | That account                                     | `account` |
| `@username@server.name`   | That remote account; its home server writes the row and notifies | `account` |
| `@handle`                 | Each account in the group                        | `group`   |
| `@everyone`               | Each member of the conversation who is in the built-in group `@everyone`; never anyone outside the conversation, even in a public channel | `group` |
| `@channel`                | Each member of the conversation                  | `channel` |
| `@here`                   | Each member whose shown presence is `online` at that moment | `here` |

Then it drops rows for: the author; accounts that cannot read the
conversation; accounts that have blocked the author; removed and
unclaimed accounts; remote accounts (their home server works out its
own). If several kinds apply to one account the row has the first in
the order `account`, `group`, `channel`, `here`.

`@channel`, `@here` and `@everyone` are the **broad mentions**
(ruling R64). `@admins` and every other group are not.

- **Who may use one.** Any account that can post, unless one of
  these says only the channel's managers (section 2.4 of
  `PERMISSIONS.md`) may:
  1. the server setting `mentions.broadcast_managers_only` is on;
  2. the channel's own answer, `conversations.broad_mentions`, is
     `managers`;
  3. the channel has no answer of its own and has more members than
     the server setting `mentions.broad_mention_limit` (default 250;
     0 is no limit).
  A channel whose answer is `anyone` is not held to the limit in 3.
  It is still held to the switch in 1, which is the admins' and which
  no channel loosens. A channel's managers set its answer. Guests,
  bots and webhooks are held to exactly the same rule as members.
- **How often.** Whoever may use them, managers and admins included,
  makes at most `mentions.broad_per_hour` broad mentions in an hour
  across the instance (default 20); past it they are left as plain
  text, as above. Bots are not held to it. An edit that adds a broad
  mention counts as one.
- **Whom they reach.** Only the people in the conversation. `@everyone`
  in a public channel is its members who are in the built-in group,
  never everyone who could read the channel: a channel made for the
  purpose reaches nobody but those in it.
- **Adding people.** One person adds at most `members.adds_per_hour`
  people to conversations in an hour (default 60), counting the people
  a new channel is made with; past it the request answers
  `rate_limited`. Bots are not held to it.
- **When the author may not,** the words are left as plain text: no
  entity, no rows, nobody notified. The message is posted all the
  same, and the answer to the post carries
  `broad_mentions_refused: true` so the client can tell the author.
  A client can know beforehand: the conversation object carries
  `broad_mentions_allowed` for its reader, and `checkBroadMentions`
  reads a text as the post would and says whether its broad mentions
  are allowed and how many people they would reach.
- **Ignoring them.** A member may set `ignore_broad_mentions` on
  their membership of a conversation (`members.ignore_broad`). From
  then on a broad mention there writes no row for them, so it neither
  notifies them nor counts as a mention of them. A mention by name,
  or of a group they are in, still does, also in the same message.
  Messages posted before the change keep the rows they have.
- **Confirming.** `checkBroadMentions` answers `confirm: true` when
  the broad mentions of a text would reach more than 30 people, and
  the client then asks "This will notify N people" before posting.
  The post itself never refuses for size.
- **Cost.** The rows of a broad mention are written by the database
  from the member rows, in one statement whatever the size of the
  channel, and the phone wake-ups it causes are spread over a short
  time (`PUSH.md` section 2).
- If an `@username` resolves to someone who cannot read the
  conversation, the entity is stored, no row is written, and the
  response to the post names that account so the client can tell
  the author.
- Mentions inside a card do not count.
- On an edit the rows are worked out again. New rows notify; rows
  whose mention is gone are deleted.

## 3. Presence, status, availability and do not disturb

All of this belongs to the account and is stored on `accounts`.
Changes are sent as `presence.changed`, `status.changed`,
`availability.changed` and `dnd.changed` to the account's own
connections and to every account that shares a conversation with
it, and to linked servers on the same rule as presence. What only
the account itself sees (quiet hours, preferences) is sent as
`me.updated`. Presence is not stored.

### 3.1 Presence

Worked out by the server from live connections:

| Value     | When                                                       |
|-----------|------------------------------------------------------------|
| `online`  | At least one client is connected and has reported activity in the last 5 minutes |
| `away`    | Clients are connected and none has been active for 5 minutes |
| `offline` | No client is connected                                     |

What other people are **shown**:

1. `away` if appear away is on, if the current status has its
   appear-away switch on, or if the account is out of office.
2. Otherwise the worked-out value.

A mark for "in a call" is added while the account has a
`call_participants` row with no `left_at`.

### 3.2 Appear away

On for a duration or until turned off: `accounts.away_until` is the
end time, `0` for until turned off, null for off.

### 3.3 Status

One emoji, up to 100 characters of text, an optional end time, and
two switches: pause notifications and appear away. Stored in
`status_emoji`, `status_text`, `status_until`, `status_dnd`,
`status_away`.

- A status **exists** while `status_emoji` or `status_text` is set
  and `status_until` is null or in the future.
- The switches count only while the status exists.
- Setting a status replaces the last one whole. Clearing it clears
  all five columns.
- When `status_until` passes, the status stops existing at once for
  every rule here; the scheduler clears the columns and sends
  `status.changed` shortly after.
- A bot can set a status. A person's script can set theirs with a
  personal token.

### 3.4 Presets

Saved statuses in `status_presets`: community ones (`owner_id` null,
made by admins) and each person's own. Applying a preset is the
request to set a status with the preset named: the server copies its
emoji, text and switches, sets `status_until` to now plus
`duration_seconds` (null stays null), and, if the preset has an
`availability`, sets that too with the same end time. Later changes
to the preset do not touch statuses already set from it.

Presets have no event. Clients fetch the list when the status
picker opens.

### 3.5 Availability

One of two fixed values, each with an optional end time:
`accounts.availability` is `meeting` or `out_of_office`, with
`availability_until` and `availability_by`.

- **In a meeting**: a mark beside the name until the end time. It
  also pauses the person's notifications if their preference
  `prefs.meeting_pauses` is true.
- **Out of office**: the person is shown as away, with the date they
  are back. A client shows that to anyone starting a message to
  them.

Set by the person, by their script with a personal token
(`availability_by = self`), or by a server integration allowed to
(`availability_by = integration:<id>`, and an audit row). The last
write wins. An integration's request to clear availability clears it
only if that integration set it, so it cannot wipe what a person set
by hand (decision F4).

### 3.6 Is this account in do not disturb right now?

An account is in do not disturb at moment `now` when **any** of four
things is true:

    manual  =  dnd_until is not null
               and (dnd_until = 0 or dnd_until > now)

    status  =  a status exists (3.3) and status_dnd is true

    meeting =  availability = 'meeting'
               and (availability_until is null or availability_until > now)
               and prefs.meeting_pauses is true
               and not (prefs.meeting_pause_skip_until is set
                        and (it is 0 or now < it))

    quiet   =  quiet_start, quiet_end and quiet_days are set
               and there is a quiet period P with P.start <= now < P.end
               and not (quiet_skip_until is not null
                        and P.end <= quiet_skip_until)

    in_dnd  =  manual or status or meeting or quiet

**Quiet periods.** For each day whose character in `quiet_days` is
`1` (Monday first), a period starts at `quiet_start` on that day and
ends at the next `quiet_end` after it: later the same day if the end
is later than the start, otherwise the following day. Times are wall
clock in `accounts.time_zone`, or the server's time zone if the
account has none. `quiet_start` and `quiet_end` may not be equal.

**The sources do not touch each other.** A manual do not disturb
that outlasts quiet hours stays on. Quiet hours that outlast a
manual duration keep things quiet. Nothing shortens anything else.

| Request                            | What changes                          |
|------------------------------------|---------------------------------------|
| Turn on for a duration             | `dnd_until = now + duration`          |
| Turn on until turned off           | `dnd_until = 0`                       |
| Skip the next quiet period         | `quiet_skip_until` = the end of the next period that has not started. Because the formula skips every period that ends at or before that moment, a period already under way is ended too. Turning do not disturb off during quiet hours ends the current period by the same mechanism |
| Turn do not disturb off            | Everything that makes the account unavailable now is cleared: `dnd_until = null`; `status_dnd = false` if a status exists; if inside a quiet period, `quiet_skip_until` = that period's end; if in a meeting that pauses, `prefs.meeting_pause_skip_until` = `availability_until`, or `0` if it has none |
| Switch the schedule off            | `quiet_start`, `quiet_end`, `quiet_days` and `quiet_skip_until` are cleared. The client keeps the old times to offer them again. |

After "turn do not disturb off" `in_dnd` is false: the account is
available now (ruling R37). Each clearing is as narrow as it can
be. The status keeps its text and its appear-away switch. Only the
current quiet period is skipped. Only the current meeting's pause
is skipped: the meeting mark stays, the preference stays, and
`prefs.meeting_pause_skip_until` is removed the next time
availability is set or cleared, so the next meeting pauses again
(decision F5).

While `in_dnd` is true: no sound, no pop-up, no call ring and no
phone wake-up, which the server does not send at all. Badges keep
counting. Other people see a do not disturb mark on the account and
nothing about why.

### 3.7 Should this event notify this account?

Only two things ever notify: a new message, and a call starting.
Edits, reactions, ticks, form answers, pins and joins never do.

For a new message `m` in conversation `C` and an account `A`, the
server works through these in order. The first that applies decides.

1. `A` is a remote account → **no**. Its home server decides.
2. `A` is removed, unclaimed or a bot → **no**. A bot gets the
   event and needs no notification.
3. `A` is the author of `m` and `m` is not a reminder → **no**.
4. `m` is a system notice, or a sealed-chat message marked quiet →
   **no**.
5. `A` has blocked the author → **no**.
6. `A` cannot read `C` → **no**.
7. Work out whether the mode would notify. Let `row` be A's
   `mentions` row for `m`, if any; `named` be true when
   `row.kind = account`; `state` be A's state for the thread if `m`
   is a reply.

   | Case                                      | `would`                    |
   |-------------------------------------------|----------------------------|
   | `A` is not a member of `C` (a public channel) | `row` exists           |
   | `m` is a reply and `state` is ignored     | `named`                    |
   | Mode `any`, `m` top-level                 | yes                        |
   | Mode `any`, `m` a reply                   | `state` is following, or `row` exists |
   | Mode `mentions`, `m` top-level            | `row` exists               |
   | Mode `mentions`, `m` a reply              | `state` is following, or `row` exists |
   | Mode `nothing`, or muted                  | no                         |

   The mode is the mode in force (2.3). The thread view plays no
   part. If `would` is no → **no**.
8. `A` is not in do not disturb (3.6) → **yes**.
9. `A` is in do not disturb → **no**, unless and until the author
   pushes this message through to `A` and it applies (3.8).

The server sends the answer with the message. Each recipient's copy
of `message.created` carries one of three values (decision F6):

| Value    | When                                                    |
|----------|---------------------------------------------------------|
| `notify` | The answer above is yes.                                |
| `badge`  | The answer is no, and the message is in the recipient's badge set (2.3): a name mention under mode `nothing` or a mute, or anything that would have notified but for do not disturb. |
| `none`   | Otherwise.                                              |

A client makes a sound or a pop-up only for `notify`, so every
client behaves the same. Whether a phone is also woken is decided
by `PUSH.md`; no wake-up is sent unless the answer here is yes.

A **call starting** rings an account when: it is a member of the
conversation at level `member` or above, its mode there is `any`, it
has not blocked the starter, and it is not in do not disturb.
Nothing pushes a ring through do not disturb. In other modes the
call shows in the conversation and does not ring (decision F7).

### 3.8 Notify anyway

A sender who sees that someone is in do not disturb may push one
message through to that one person. It is a request made after the
message is posted, by its author, naming the message and one
account. One person per request.

**It always appears to succeed** (ruling R4). The only refusals are
about the caller, never about the target:

| Check                                                  | Refusal     |
|--------------------------------------------------------|-------------|
| The caller is the message's author                     | `forbidden` |
| The caller is a person, not a bot or webhook           | `forbidden` |
| The named account is one the caller can see            | `not_found` |

Then, in one transaction, the server:

1. Sets `messages.notified_anyway`. Everyone sees the mark. It means
   the sender used notify anyway and nothing more.
2. Works out, without telling anyone, whether it applies. It applies
   only if **all** of these hold for the target:
   - they can read the conversation;
   - they are in do not disturb now (3.6);
   - `accounts.allow_notify_anyway` is true;
   - they have not blocked the sender;
   - step 7 of 3.7 says the mode would have notified them, **and**
     the message mentions them by name or their mode in the
     conversation is `any`. So never for `@channel`, `@here` or a
     group mention, nor in a conversation they have on `nothing`
     or muted. Whether it is hidden from their sidebar plays no
     part.
3. Writes a `notify_anyway` row: the message, the target, the
   sender, the time, and `delivered` true or false.
4. If it applies, notifies the target as if they were not in do not
   disturb. If not, does nothing more.

The response is the same in every case, and so is everything anyone
can later fetch: the message with its mark. `delivered` and the list
of targets are never returned, to the sender, to an admin or in the
export. So the feature cannot be used to learn who has set a
conversation to `nothing`, muted it, blocked someone or turned the
feature off.
Asking again for the same message and person changes nothing.

The account object shows other people the do not disturb mark, so
clients offer the action where it may be useful.

Stored: the `accounts` columns above, `status_presets`,
`messages.notified_anyway`, `notify_anyway`.

Events: `presence.changed`, `status.changed`,
`availability.changed`, `dnd.changed`, `me.updated`;
`message.created` carries the notify decision; `badge.changed`.

## 4. Edits, deletes and history

### 4.1 Edits

Only the author edits, at level `member` or above, in a conversation
that is not archived, and not while timed out. The word filter runs
on the new text. Slow mode does not apply.

Let `age` be now minus `created_at`.

| `age`                                   | What an edit does                   |
|-----------------------------------------|-------------------------------------|
| Less than `messages.quick_correction_seconds` (30 by default; 0 switches quick corrections off) | **Quick correction.** The message is replaced in place. No version is kept, `edited_at` stays null, `version` does not change. Files taken off are deleted outright. |
| After that, within `messages.edit_window_seconds` (no limit by default) | **Edit.** The current content is first copied to `message_versions` (`version` = the current number, `reason = edit`, `replaced_by` the author). Then the message is changed, `version` goes up by one and `edited_at` is set. Files taken off get `detached_at`. |
| Past the edit window                    | Refused with `forbidden`. Bots are not held to the window. |

In both cases the server then:

1. Works out entities again.
2. Matches checklist items: an item whose text is unchanged keeps
   its identifier and state; a reworded item keeps them if it is in
   the same position; a removed item's row gets `removed_at`; a new
   item gets a new identifier.
3. Works out `mentions` again (2.5). Newly mentioned accounts are
   notified as for a new message.
4. Replaces the search entry.
5. Fetches previews for links that were not in the earlier text, and
   drops preview cards whose link is gone (section 23).

A form's fields cannot be changed once anyone has answered
(`conflict`). An edit may not leave the message with no text, file,
card or form (`invalid_request`); delete it instead. A deleted
message cannot be edited.

Stored: `messages`, `message_versions`, `files.detached_at`,
`checklist_items`, `mentions`.

Events: `message.edited`.

### 4.2 Deletes

| Case                                           | What happens                  |
|------------------------------------------------|-------------------------------|
| By the author, inside the quick-correction window, no replies | The row is deleted outright with everything that hangs from it, and its files are removed. No trace is left. |
| By the author, inside the window, with replies | An ordinary delete followed at once by a purge, leaving a bare record so the replies still have a parent. |
| Any other delete                               | An ordinary delete, below.    |

An ordinary delete, in one transaction:

1. Copies the content to `message_versions` with `reason = delete`.
2. Empties `text`, `entities`, `cards` and `fallback`; sets
   `deleted_at` and `deleted_by`.
3. Sets `detached_at` on its files.
4. Deletes its `reactions`, its `mentions`, its `pins` row and its
   search entry.
5. Closes its form, if it has an open one.
6. If it is a reply, updates the parent's thread summary.

The message stays in place as a marker saying who deleted it and
when. A deleted parent keeps its thread. Replies, bookmarks and
reports stay.

Events: `message.deleted`. A delete by someone other than the author
is written to the audit log, except in an ad-hoc channel.

### 4.3 History

Anyone who can read the conversation can open a message's history:
each earlier version with who replaced it and when, and for a
deleted message its last content, with the files each version
carried. `has_history` on the message is true when there is
something to open *for that reader*.

History is not searched. When `messages.history_hidden_at` is set
only admins can open it (section 5).

Sealed chats keep no history on the server. An edit or delete there
is a new encrypted message that tells the devices to replace or
remove what they show.

## 5. Purge

Purging erases for good. For a message that is not deleted it erases
the history and leaves the message as it now reads. For a deleted
message it erases everything and leaves a bare record: that a
message was here, who posted it and when. What "erases" removes, row
by row, is in SCHEMA section 14.2: the database rows, the search
entry, the event-log rows and the stored files.

### 5.1 An author asks

The author asks for a purge of one of their own messages. There must
be something to purge: the message is deleted, or has history
(`conflict` otherwise).

| Conversation      | Steps                                                    |
|-------------------|----------------------------------------------------------|
| Private or ad-hoc channel | The purge is carried out in the same request. No `purge_requests` row, no audit row, no event to anyone outside the channel. |
| Public channel    | 1. `messages.history_hidden_at` is set to now. From this moment only admins can open the history. 2. A `purge_requests` row is written with a null `outcome`. 3. Admins are told. |
| Shared channel    | As above by privacy. A request in a public shared channel goes to the admins of the author's own server. A purge carried out is sent to the linked servers as an event they are expected to honour. |
| Sealed chat       | Refused with `forbidden`. There is no history to purge.  |

If a public channel is made private while a request waits, the
request is carried out as an erasure at once (section 21.5).

Asking twice while a request waits returns the waiting request.
After an outcome of "show again", asking again is refused with
`conflict` unless the message has been edited or deleted since the
decision.

### 5.2 An admin decides

Any admin may decide a waiting request, with one of three outcomes:

| Outcome   | `purge_requests.outcome` | Effect                              |
|-----------|--------------------------|-------------------------------------|
| Show again | `shown`                 | `history_hidden_at` is cleared. Everyone who can read the channel can open the history again. |
| Keep hidden | `hidden`               | Nothing changes. The history stays, and only admins can open it. |
| Erase     | `erased`                 | The purge is carried out.           |

`decided_by` and `decided_at` are set and an audit row is written.
The author is not told. The request, as the author sees it, shows
only that it was made; `outcome` is never returned to a non-admin.

### 5.3 An admin purges directly

An admin can delete and purge any message they can read, in one
step: an ordinary delete (if not already deleted) followed by the
purge, with one audit row. No `purge_requests` row is written. A
waiting request for that message is marked `erased`.

Stored: `messages.history_hidden_at`, `messages.purged_at`,
`purge_requests`.

Events: `message.purged` in the conversation when a purge is carried
out at once, and when an author's request hides the history in a
public channel. To everyone but admins, hidden and erased look the
same, so no further event is sent to the channel when an admin
chooses "keep hidden" or "erase". `message.history_restored` when
an admin chooses "show again". `purge_request.created` and
`purge_request.decided` to admins only.

## 6. Retention

- The server-wide period is the `retention.seconds` setting, or null
  for none. Admins set it.
- A named channel may have its own in
  `conversations.retention_seconds`. Its managers may set a period
  shorter than the server's (any period if the server has none), or
  clear the override by setting `0`. An admin may set any period,
  from the admin console, or in the channel as one of its managers.
  Each change is written to the audit log.
- "Keep for ever" is not a per-channel value: it is the server's
  period being unset. A channel with no override follows the
  server, so it keeps for ever exactly when the server does.
- Ad-hoc channels and sealed chats always follow the server's
  period.
- The stored override is what applies. Lowering the server's period
  later does not change overrides; the admin console lists the
  channels whose override is longer than the server's so the admin
  can clear them (decision F9).
- The janitor runs hourly and deletes each message whose
  `created_at` is older than its conversation's period, with its
  history, files, search entry and log rows (SCHEMA section 14.1). A
  parent that still has younger replies is emptied to a bare record
  and removed once they are gone.
- Retention needs nobody's approval and is not written to the audit
  log message by message. Changing a period is.
- In a shared channel the deletion is also sent to linked servers.
- The Slack importer keeps original times, so imported messages
  older than the period are removed by the next run. The importer
  warns about this before it starts (section 25).

Stored: the setting, `conversations.retention_seconds`.

Events: `conversation.updated` when a channel's period changes. A
retention run sends no event. Clients know each conversation's
period and drop messages older than it from their own storage by
the same rule.

## 7. Forms and polls

The format of a form is in `MESSAGE-FORMAT.md` section 7. This
section covers posting, answering, each action and each preset.

### 7.1 Posting a form

A form arrives as part of the request to post a message. The server
checks:

| Check                                                         | Refusal         |
|---------------------------------------------------------------|-----------------|
| At most 20 fields, 50 options per choice field, 5 buttons     | `invalid_value` |
| Field, option and button ids are 1 to 32 characters of `[a-z0-9_-]`, unique within the form, and not `_button` | `invalid_value` |
| Every field has a known `type` and a `label`                  | `invalid_value` |
| The form has at least one field or one button                 | `invalid_request` |
| `tally` has at least one `choice` field or one button         | `invalid_request` |
| `results` is set only with `tally`                            | `invalid_request` |
| `anonymous` only with `tally` or `collect`                    | `invalid_request` |
| `answerer_group_id`, if present, names a group, and the poster is a manager of the conversation | `invalid_value`, `forbidden` |
| In a conversation shared with other servers the action is `tally` | `forbidden` |
| In a `group` form every button carries `group_action` (`join` or `leave`) and `group_id` | `invalid_value` |
| `one_answer: false` only with `thread` or `channel`           | `invalid_request` |
| `allow_added_options` only if there is a `choice` field       | `invalid_request` |
| `closes_at` is in the future                                  | `invalid_value` |
| The poster may use the action here (PERMISSIONS section 5)    | `forbidden`     |
| `channel`: `name_prefix` is a valid channel name of at most 50 characters; the group exists; `level` is `reader`, `member`, `moderator` or `manager` | `invalid_value` |
| `group`: every group named is open                            | `invalid_value` |
| The conversation is not a sealed chat                         | `invalid_request` |

The server then writes the `forms` row with `owner_id` the author,
and writes the message's `fallback`: the title, each field's label,
and the options of choice fields, one per line.

Events: `message.created`.

### 7.2 Answering

The request to answer carries a map of field id to value, and the
button id if a button was pressed.

1. `perm`: the account is a member of the conversation, is not a
   bot, is not timed out, and the conversation is not archived.
2. The form is open. If `closes_at` has passed and `closed_at` is
   still null, the server closes it now and refuses (`conflict`).
3. If the form has `answerer_group_id`, the account is in that
   group (`forbidden`).
4. Each value is checked against its field, with `invalid_value` and
   the field's id on failure:

   | Type           | Checks                                           |
   |----------------|--------------------------------------------------|
   | `choice`       | Every id is an option. One id unless `multiple`. Between `min` and `max` ids if set. An option with its own `max` (places) is not already full. |
   | `text`         | No line breaks. At most `max_length`, 500 if unset. |
   | `longtext`     | At most `max_length`, 4,000 if unset.            |
   | `number`       | A number between `min` and `max`, on a `step`.   |
   | `date`         | `YYYY-MM-DD`, between `min` and `max`.           |
   | `time`         | `HH:MM`.                                         |
   | `account`      | Accounts the answerer can see. One unless `multiple`. |
   | `conversation` | Channels the answerer can see. One unless `multiple`. |
   | `checkbox`     | `true` or `false`.                               |

   A required field must have a value; for `checkbox` that means
   `true`. A field id the form does not have is `invalid_value`.
5. The action is carried out (7.4).

**Changing and withdrawing.** With `tally` and `collect`, answering
again replaces the earlier answer, and the request to withdraw
removes it, until the form closes. With `thread` and `channel` an
answer cannot be changed or withdrawn, because it has already become
a message; with `one_answer` true a second answer is refused with
`already_exists` (decision F10).

**Adding an option.** Where `allow_added_options` is true, an
answerer may add an option to a `choice` field: a label of up to 100
characters, not equal to an existing one, while the field has fewer
than 50. The server gives it an id, records who added it in the
field, and sends `form.option_added`. This is the one change to `fields`
allowed after answers exist.

### 7.3 Closing

A form closes when its author or a manager of the conversation asks,
or when `closes_at` arrives (the
scheduler sets `closed_at` with a null `closed_by`). Closing is
final. The result stays on the message. Deleting the message closes
its form.

Events: `form.closed`.

### 7.4 What each action does

**`tally`** (a poll)

In a conversation shared with other servers this is the only action
there is, and the steps below run on the server the form was posted
on. An answer from an account elsewhere travels there sealed, so no
other server and no hub sees it; that server keeps the answers and
the tally and sends the tally on as events. `LINK-PROTOCOL.md` has
the frames.

1. In one transaction: find the account's `form_answers` row. If
   there is one, lower `form_tallies.count` for each option it held
   and replace `answers`; otherwise insert the row and raise
   `forms.answer_count`. Raise `count` for each option now chosen.
   A button press is tallied under field `_button`.
2. Send `form.answered` to the conversation's members. Each
   recipient's copy of the form holds what `results` allows them:
   `live` shows `tally` and `voters` to all; `after_answer` only to
   those with an answer; `after_close` to nobody until closed.
   `voters` is never included when `anonymous` is true, for anyone.
   `my_answer` goes only to the answerer.
3. Withdrawing lowers the counts, deletes the row, lowers
   `answer_count` and sends `form.answer_withdrawn`.

**`collect`**

1. Insert or replace the account's `form_answers` row; keep
   `answer_count` right.
2. Send `form.answered` to members with the new `answer_count`
   only.
3. The form's author and the conversation's managers can list the
   answers and download them as CSV: one row per answer, one column
   per field, plus the answerer and the time. On an anonymous form
   the answerer and time columns are left out and rows are returned
   in random order.

**`thread`**

1. Insert a `form_answers` row. After step 2 its
   `result_message_id` is the reply.
2. Post a reply in the form message's thread: `author_id` the
   answerer, `via_kind = command`, `via_name` the form's title, no
   text, and one card whose fields are the form's labels and the
   answers. The server posts it even if the answerer's level is
   `reader`.
3. The usual thread rules follow: the answerer follows the thread,
   the summary is updated.

   Events: `form.answered` and `message.created`.

**`channel`** (a ticket)

1. Take the next number for the prefix from the setting
   `tickets.next.<prefix>` and add one to it.
2. Create a private channel named `<prefix>-<number>` with
   `ticket_number` set and `created_by` the answerer. It is a
   sub-channel of the channel holding the form, or its sibling if
   that channel is itself a sub-channel (`parent_id`, section 21.7).
   The name comes from the counter and need not be unique. Add the
   answerer as `member`. Add the action's group through
   `channel_groups` at the action's `level`, so its members come and
   go with the group. If nobody ends up a manager, the usual rule
   applies and the longest-standing member becomes one.
3. Insert a `form_answers` row.
4. Post the answers as the new channel's first message, in the same
   card form as for `thread`, under the answerer's name, and store
   it in the row's `result_message_id`.
5. Send the answerer the new channel.

   Archiving the channel closes the ticket. Nothing else marks it
   closed.

   Events: `form.answered` (count only), `member.joined` in the new
   channel, `message.created` there.

**`group`**

Each button carries `group_action`, `join` or `leave`, and
`group_id`. So one form can offer several groups.

1. Check the group is still open (`conflict` if not) and the
   presser's role may join groups by itself (`forbidden` for guests).
2. Insert or delete the `group_members` row. Joining a group one is
   already in, or leaving one one is not in, does nothing and
   succeeds.
3. Work out channel memberships that follow (section 14).

   Nothing is written to `form_answers` and `answer_count` stays 0.
   Events: `group.member_added` or `group.member_removed`, and the
   membership events that follow.

**`bot`**

1. Nothing is stored.
2. Send `form.answered`, with the answers, the button and the
   answerer, to the bot that posted the form and to nobody else.
3. The bot answers by editing its message, replying for the
   answerer only, or asking the server to show the answerer a
   dialog.

A **dialog** is a `forms` row with `dialog_for` set and no message.
The server sends `interaction.responded` to that person's
connections with the form to show. Their answer goes to the bot as
`form.answered` and the row is deleted. Dismissing deletes it too.
A bot may open a dialog only in answer to a command, button press
or answer it received from that person in the last 15 minutes.
`BOTS.md` has the rest.

### 7.5 Presets

A preset is an ordinary form with `preset` set so clients know how
to draw it. The client builds the form, from a dialog or from the
command text, and posts it with the ordinary request to post a
message; the server checks it as in 7.1 and does not parse command
text (section 12). The grammar below is fixed so that every client
builds the same form from the same text; both clients read it with one
parser each (`shared/presets.js`, `lib/core/forms/presets.dart`) held
to the cases in `testdata/commands/presets.json`, and the server's form
checks take every form those cases build.

Command text is split on `|`. Parts are trimmed. `\|` is a literal
bar. Flags come after the last part, each starting with `--`:

| Flag               | Meaning                                         | On |
|--------------------|-------------------------------------------------|----|
| `--anonymous`      | `anonymous: true`                               | `/poll` |
| `--multiple`       | The choice field allows several options         | `/poll`, `/signup` |
| `--add`            | `allow_added_options: true`                     | `/poll` |
| `--results <when>` | `live` (default), `after_answer` or `after_close` | `/poll` |
| `--until <when>`   | `closes_at`, in the time grammar of section 9.2, so in the account's time zone | `/poll`, `/signup`, `/approve` |

Options get the ids `o1`, `o2`, … in order.

| Preset    | How it is made                            | The form the server stores |
|-----------|-------------------------------------------|----------------------------|
| `poll`    | `/poll Question? \| Option \| Option …`   | `title` the question. One required `choice` field `choice` with 2 to 50 options. `tally`. |
| `rsvp`    | `/rsvp What \| When`                      | `title` What. One required `choice` field `choice` with options `going`, `maybe`, `cant`. `tally`, named, `results: live`. `closes_at` = When, parsed as a single moment in the time grammar of section 9.2 (the account's time zone); it is the event's time, which is what clients draw as the date. |
| `signup`  | `/signup What \| Slot \| Slot …`          | `title` What. One required `choice` field `choice`, one option per slot. A slot ending in a number in brackets, `Morning (3)`, has that many places (the option's `max`); without one there is no limit. `tally`, named. |
| `approve` | `/approve What` or `/approve What \| @group` | `title` What. No fields. Buttons `approve` (primary) and `decline` (danger). `tally`, named, one answer each, changeable until closed. A manager of the conversation may add a second part naming one group; it becomes `answerer_group_id` and only that group's members may press. From anyone else a second part is refused. |
| `checkin` | Set up in channel settings (section 11)   | 1 to 20 `text` or `longtext` fields. `thread`. Posted on a schedule. |
| `ticket`  | Set up in channel settings by a manager   | `text` and `longtext` fields. `channel`, `one_answer: false`. Posted once, usually pinned. |
| `roles`   | Posted by an admin from the admin console | No fields. One `join` button per open group, up to five. `group`. People leave from the group's page, or the admin posts `leave` buttons (decision F11). |
| (none)    | `/form` opens the client's builder        | Any fields. `collect`.      |

A client refuses, before sending anything: a poll with fewer than
two options or an empty question; an RSVP whose When does not parse
or is in the past; a slot count below 1; an unknown flag. The
server's own checks in 7.1 catch the same mistakes in a form built
some other way, with `invalid_value`.

Stored: `forms`, `form_answers`, `form_tallies`, and what each
action creates.

Events: `message.created`, `form.answered`,
`form.answer_withdrawn`, `form.option_added`, `form.closed`,
`interaction.responded`.

## 8. Checklists

- Each `- [ ]` or `- [x]` line of a message becomes a
  `checklist_items` row when the message is posted, with a short
  `item_id` (`t1`, `t2`, …), its position and text, and the starting
  state the author typed. At most 100 per message.
- **Ticking.** Any member at level `member` or above may set any
  item ticked or unticked. In one transaction the server updates
  `checked`, `checked_by` and `checked_at` on the item and inserts a
  `checklist_history` row. Setting an item to the state it already
  has does nothing and writes no history.
- Ticking is not an edit: `edited_at`, `version` and
  `message_versions` are untouched, and nobody is notified.
- **Editing** the message reconciles items as in section 4.1.
- **History.** Anyone who can read the conversation can open one
  item's history: who, ticked or unticked, when.
- A deleted message's items cannot be ticked.
- In a sealed chat a tick is an encrypted message between the two
  people and the server stores nothing about it.

Stored: `checklist_items`, `checklist_history`.

Events: `task.changed`.

## 9. Reminders

### 9.1 What a reminder is

A reminder is a `scheduled` row of kind `reminder`. When it is due
the server posts its content as a message with
`via = {"kind": "reminder"}`.

| Target              | Delivered                                           |
|---------------------|-----------------------------------------------------|
| Yourself            | In your own one-person channel: the ad-hoc channel whose only member is you. |
| Another person      | In the ad-hoc channel between the two of you. You must be allowed to start a chat with them. |
| A channel           | In that channel, as a top-level message. You must be able to post there when you set it and when it is due. |

The scheduled item names the conversation. For the first two rows
the client first finds or makes the channel with the request that
opens a chat, when the reminder is set.

The message's author is whoever set the reminder. A reminder counts
as unread and notifies for its author too (sections 2.1 and 3.7),
which is the point of it.

"Remind me about this message" sets `about_message_id`. The
delivered message then carries that message as its quote
(`quote_message_id`, no range), so clients draw the original beside
the reminder. This is the one case where a top-level message has a
quote (decision F12).

### 9.2 The `/remind` grammar

    /remind [target] when | what

    target   = "me" | "@" username | channel-link          (default: me)
    when     = once | repeat
    once     = "in" duration
             | day [ "at" time ]
             | date [ "at" time ]
             | "at" time
             | [ "at" | "on" ] instant
    repeat   = "every" unit [ "at" time ]
    duration = number span { number span }
    span     = "m" | "min" | "minutes" | "h" | "hours"
             | "d" | "days" | "w" | "weeks"
    day      = "today" | "tomorrow" | weekday
    weekday  = "monday" | … | "sunday"       (or the first three letters)
    date     = YYYY-MM-DD
    time     = HH:MM | H "am" | H "pm" | H:MM "am" | H:MM "pm"
    instant  = a typed instant (MESSAGE-FORMAT.md 3.1), not a range
    unit     = "day" | "weekday" | "week" | "month"
             | weekday { "," weekday }

- Words are matched without regard to case.
- A channel target is a channel link, `<#conversation-id>`, which
  the composer inserts when the person picks the channel after
  typing `#` (section 21.8). A channel cannot be named by typing its
  name.
- Times are wall clock in the setter's `accounts.time_zone`, or the
  server's (`server.time_zone`, sent as `Server.time_zone`) if they
  have none; never the device's. The client passes this zone to the
  parser, and a repeat carries it as its `time_zone`.
- A wall-clock time the zone's clocks skip, in the gap when they go
  forward, moves on by the length of the gap: 02:30 on a night that
  goes from 02:00 to 03:00 is 03:30. A time they show twice, when they
  go back, is the first of the two. A duration is exact (`in 1d` is 24
  hours). The scheduler works out repeats by the same rule.
- An instant with its own zone or offset (`2026-12-01 14:30
  Europe/Paris`, `2026-12-01T14:30+10:00`) is that exact moment; one
  without is wall clock in the setter's zone. An instant its own zone
  skips, a range, or other words beside it do not read.
- The same holds wherever this grammar is used: `--until` and the
  RSVP's When (section 7.5).
- A missing time means 09:00. `at` alone means the next time the
  clock shows that time. A weekday means its next occurrence after
  today.
- A single moment in the past is refused with `invalid_value`.
- `what` is Churr Markdown and may be empty only for "about this
  message".
- The client reads this grammar and makes the request to create a
  scheduled item; both clients read it with one parser each
  (`shared/remind.js`, `lib/core/commands/remind.dart`) held to
  `testdata/commands/remind.json`, whose repeat rules the server's
  checks take. With nothing after `/remind`, the client opens a form
  that makes the same request (what, when, and to the person or here).
  The grammar is English; clients in other languages offer a dialog
  that makes the same request.

Examples:

    /remind 2026-12-01 14:30 Europe/Paris | Call Paris
    /remind in 20m | Stretch
    /remind me tomorrow at 9 | Send the report
    /remind @sam friday at 14:30 | Review is due
    /remind <#0193a0c7-…> every weekday at 09:30 | Stand-up in five minutes
    /remind every month | Expenses

### 9.3 Repeating

`scheduled.repeat` is null for a single run, otherwise:

    {"every": "day" | "week" | "month",
     "days": "1111100", "at": "09:30", "time_zone": "Europe/Dublin"}

with, for some kinds, `interval`, `day`, `week` and `weekday`:

| Typed or chosen              | Stored                                       |
|------------------------------|----------------------------------------------|
| `every day`                  | `every: day`, `days: 1111111`                |
| `every weekday`              | `every: day`, `days: 1111100`                |
| `every monday,thursday`      | `every: day`, `days: 1001000`                |
| `every week`                 | `every: week`. Runs on the weekday of the first run. |
| Every 2 weeks on Monday and Wednesday (check-ins) | `every: week`, `days: 1010000`, `interval: 2`. Runs on those days every second week, counted from the week of the first run after the rule is set or changed. `interval` is 1 to 52; absent means 1. |
| `every month`                | `every: month`. Runs on the day of the month of the first run, or the month's last day if it has no such day. |
| Monthly on the 15th (check-ins) | `every: month`, `day: 15`. The month's last day if it has no such day. |
| Monthly on the first Monday, the last Friday (check-ins) | `every: month`, `week: 1`, `weekday: 1`; `week: -1`, `weekday: 5`. `week` is 1 to 4 or -1 for the last; `weekday` 1 (Monday) to 7. |

`days` goes only with `day` and `week`, `interval` only with `week`
and `days`, and `day` or `week` with `weekday` only with `month`,
never both.

`next_run_at` is always the next moment the rule gives, worked out
in the rule's time zone so that 09:30 stays 09:30 across a clock
change. A repeating reminder runs until cancelled.

### 9.4 Running

The scheduler wakes for the earliest `next_run_at`. For each due
row:

1. Check the owner may still post the content where it is going. If
   not, the row becomes failed: `failed_at` is set, `failure` holds
   the error code and sentence, `next_run_at` becomes null whether
   or not it repeats, and `scheduled.fired` is sent to the owner
   carrying the error and no message. Changing the item clears the
   failure and sets a new `next_run_at`.
2. Post the message with `created_at` now.
3. Send `scheduled.fired` to the owner, naming the message.
4. Set `last_run_at`. For a repeat set `next_run_at` to the next
   moment; otherwise null.

If the server was down when rows fell due, each runs once when it
starts, and a repeating row then moves to its next future moment. It
does not run once for every moment missed.

An account may have at most 200 waiting `scheduled` rows of all
kinds (decision F13).

Stored: `scheduled` (`kind = reminder`, `content`,
`about_message_id`, `next_run_at`, `repeat`, `last_run_at`,
`cancelled_at`).

Events: `scheduled.created`, `scheduled.updated`,
`scheduled.cancelled`, `scheduled.fired`, all to the owner's
connections; `message.created` when it runs.

### 9.5 Times in messages

A date and time typed in international form (`2026-12-24 18:00
Europe/Berlin`, `2026-12-24T18:00Z`, or `2026-12-24 18:00` in the
author's zone) becomes an instant, shown to every reader in their own
zone (ruling R69; the grammar, resolution and entity are
`MESSAGE-FORMAT.md` section 3.1).

- **In the composer**, a recognised time is offered as it is typed, as
  every exact token is (`CLIENTS.md`, "Objects in the composer"): "Time:
  Fri, 25 Dec, 3:00 am (your time)" and then "Plain text"; plain text is
  sent as an object hint and the text stays as typed.
- **Phrases.** While the author types, a phrase that names a time ("this
  Friday at 5pm", "tomorrow 9:30", "next Tuesday at noon", "in 2 hours",
  "on 3 March at 14:00") brings up "Make this a time: Fri 20 Nov, 5:00 pm
  (your time)" and "Plain text" under the box, as the mention picker does.
  Enter, Tab or a click accepts; Escape, or typing on, dismisses; space
  does not accept. Nothing is converted
  without the author accepting. The words stay the message's text; the
  instant is sent as a hint beside them and becomes an ordinary instant
  entity if the server finds it sound. Editing the words drops it, and the
  phrase may be offered again. Client side only, in English for now; the
  words come from a table per language. The rules (which day "Friday"
  means, what is too vague to offer) are in
  `internal/web/static/shared/instant/phrases.js` and the shared cases
  `testdata/instants/phrases.json`.
- **The time converter.** Selecting an instant opens it: the moment in the
  reader's zone, in the zone as written, in UTC, how long until or since
  (live), any zones the reader added through a search over zone names and
  cities (kept on that device), "Copy as ISO 8601", and "Remind me", which
  sets a reminder about the message at that moment (9.1). "Remind me" is
  not offered for a moment gone by, nor in a sealed chat, where nothing
  can be scheduled.
- **Sealed chats.** The sending device resolves the instants and puts the
  entities in the sealed content (`SEALED.md` 8.3).
- Search and export see the text as typed.

### 9.6 Objects in messages

Ruling R73: things recognised in what people type, each kept as typed
and drawn for every reader (`MESSAGE-FORMAT.md` section 3.2). Nothing
needs an outside service; nothing is found in code, links or escaped
text; the composer offers each as it is typed and the author may keep
it as plain text (`CLIENTS.md`, "Objects in the composer").

- **References.** Admins define server-wide reference patterns in the
  admin console; a named channel's managers define the channel's own in
  its settings ("References"). A pattern has a name, a prefix (`PROJ-`,
  `#`, `RFC`), optionally a restricted expression for the value (digits
  otherwise) and an address with `{value}` in it. `PROJ-123` then becomes
  a link to that address. A channel's pattern wins over the server's with
  the same prefix. A change applies to the next message posted; messages
  already written keep the address they resolved. Bots and webhooks get
  references too. Every change is in the audit log
  (`reference_pattern.created`, `.updated`, `.deleted`) and is sent to
  the people it applies to (`reference_patterns.changed`).
- **Message links.** A link to a message of this server, as "Copy link"
  writes it (the server's address or `churr://` and its host, then
  `/chat/c/…/m/…`, `/chat/c/…/t/…` for a thread, or
  `/chat/c/…/t/…/m/…` for a reply in one), is shown as a compact quote
  of that message: who wrote it, when, where, and its first lines. Each
  reader sees it only if they may see the message, which the server
  checks when their client fetches it; anyone else sees "A message you
  can't see". Nothing of the message is stored with the link, so an edit
  or a deletion shows as it now is. Selecting it goes to the message (a
  reply opens its thread). "Copy link" is on every message outside
  sealed chats in both clients; a sealed chat's messages have no address
  the server can show, and no message link is made in a sealed chat.
- **Measurements.** `5 km`, `3.2 mi`, `72°F`, `20 °C`, `3 lb`, `500 g`,
  `2 l`, `60 mph`, `50 m²`, `10 acres`, `5 GB`, `32 psi`, `500 kcal` and
  the rest of a fixed table of lengths, masses, temperatures, volumes,
  speeds, areas, data sizes, pressures and energies keep what was typed.
  Where a unit has a metric or imperial partner, each reader also sees
  the equivalent in their own system in muted text ("5 km (3.11 mi)"),
  or nothing when it is already their system. Each device chooses
  metric, imperial or off, defaulting to its region (the web chat's
  settings, the app settings). Selecting a measurement opens the unit
  converter, like the time converter of an instant: the value in the
  other useful units of its kind, the reader's system first, each row
  copied with a press.
- **Numbers in another base.** `0x4f22`, `0b1011` and `0o755` (with `_`
  between digits if wanted) keep what was typed; selecting one opens the
  number in decimal, hexadecimal, octal and binary, each row copied with
  a press, and, where the number is a Unicode code point, the character.
- **Code points.** `U+1F600`, `&#x10F2;`, `\u{1F600}` and `\U0001F600`
  keep what was typed with the character beside it in muted text (a
  dotted box with a short name for what cannot be seen). Selecting one
  opens the character large, its UTF-8 and UTF-16, HTML and escapes,
  each copied with a press, and its name and category, which the client
  asks the server for at that moment, so the server learns which code
  point was looked at. Nothing is asked until it is opened.
- **Events.** A date and time with a range (`2026-12-24 18:00–20:00
  Europe/Berlin`, `… 18:00 to 2026-12-25 02:00 UTC`) shows each reader one
  range in their own zone, with "Add to calendar", which makes a calendar
  file on their device holding the message's first line and a link to
  the message. Nothing is sent to a calendar service.
- **Phone numbers and email addresses.** `+61 7 3123 4567` and
  `sam@example.com` become controls: call or copy, write or copy, in the
  device's own phone and mail apps. Only the international `+` form of a
  number is recognised.
- **Places.** Coordinates such as `-27.4698, 153.0251` or
  `geo:48.8584,2.2945` open the device's maps on a phone and are copied
  on a desktop or in the web chat. No map is drawn and nothing is
  fetched.
- **Inline dice.** `[[2d6+3]]` in a sentence is rolled by the server
  when the message is posted, with `/roll`'s grammar and limits (one
  row only: a comma inside is not dice, and the text stays as typed), and
  shown in place with the dice and the total, spinning once for those
  who see it arrive, the total shown once the dice stop. An edit never rolls: changed or added dice show
  that nothing was rolled, so a result cannot be fished for. People
  only; not in sealed chats.
- **Colours.** `#fff`, `#ff8800` and `#ff880080` show a small swatch
  before the typed text; selecting it copies the colour. A reference
  pattern for `#` wins, and the composer offers the colour as the other
  reading.

### 9.7 Watches: "tell me when Ada is around"

Rulings R76 and R77. A person asks to be told once when someone is
next around: from that person's profile ("Tell me when Ada is
around"), from the menu of a message they wrote in the app ("Tell me
when they are around"), or with `/watch @ada`. It is literally a
reminder, not a way of following anyone: one-shot, with no repeating
form and no "every time".

**Around** means others are shown the person `online` (3.1): a client
of theirs is connected and active in the last five minutes (a client
that has just connected counts as active), and they do not appear
away (appear away, a status that appears away, out of office). Merely
connected and idle is `away`, which is not around. There is no appear
offline; `/invisible` is appear away, and a watch never fires while
it is on.

- **Who may.** Any person (not a bot), for an account whose presence
  they are already shown: someone they share a conversation with,
  here or on another server. Not for a bot, not for oneself, not for a
  removed account (`forbidden`). An account they may not see is
  `not_found`.
- **Already around.** If the person is around now, as the asker may
  see them, the answer says so at once (`around: true`) and nothing is
  stored.
- **Blocks.** If the asker has blocked the person: refused plainly,
  "You are ignoring Ada." If the person has blocked the asker, the
  person always appears online to them (ruling R77), so the answer is
  the one for someone around, the same in every byte and by the same
  path, and nothing is stored. A block made after a watch was set: the
  asker's own block cancels the watch (`scheduled.cancelled`); the
  person's block leaves it waiting and it never fires. The server
  decides this through one function, the presence of A as B may see
  it (`people.Service.PresenceSeenBy`).
- **Firing.** When the person next becomes around, the asker is sent
  one reminder in their own one-person channel, "@ada is around",
  posted as a reminder (`via.kind = reminder`): it notifies them and
  wakes their phone as any reminder does, held back by their own do
  not disturb and quiet hours like any other. The watch is then done.
  It is looked at again when it would fire: if the two no longer share
  a conversation, the person has been removed or banned, or either has
  blocked the other, it stays waiting, nobody is told, and it expires.
- **Expiry.** 48 hours after it is made, fixed. It then goes as if
  cancelled.
- **Limits.** At most 5 waiting per person and one per person watched
  (`conflict`); they count among the 200 scheduled items (F13). At
  most 20 asked for a day, whatever became of them (`rate_limited`).
- **Listed** with the reminders as kind `watch`, with whom it waits for
  and when it expires; stopped with `/unwatch @ada`, from the profile,
  or by cancelling it there.
- **The person watched is never told** and nothing anyone can fetch
  says who watches them. The server logs nothing of who watches whom.

**Where it is decided.** The asker's own server keeps the watch and
decides, from the presence it already has. For someone on this server
that is their presence transitions: the waiting watches are held in
memory by the person watched, so a change for someone nobody watches
costs one lookup and nothing more. For someone on another server,
directly linked or through a hub, it is the presence their home server
sends inside the shared channels' streams (`LINK-PROTOCOL.md` section
17): when that says they are online, the watches for them are looked
at. Nothing new travels between servers and nothing is sent to the
other server about the watch. A remote person with no presence yet,
or whose server is not connected, shows none, and the watch simply
waits. A remote person's block of the asker is known here as their
server said it (`LINK-PROTOCOL.md` section 17a), and the watch behaves
as for someone here who blocked the asker.

Stored: a `scheduled` row of kind `watch` in the asker's one-person
channel, `next_run_at` its expiry, the person watched in its
`content` (`watch_account_id`). It survives a restart: the server
reads the waiting watches when it starts.

Events: `scheduled.created`, `scheduled.fired`, `scheduled.cancelled`,
to the asker; `message.created` when it fires.

## 10. Send later

Any message a person could post now can be scheduled instead: text,
files, a form, a reply in a thread. It is a `scheduled` row of kind
`message` with the message in `content` and a single `next_run_at`.
It does not repeat.

- **Setting.** The request is checked as if posted now: permission,
  size limits, the word filter. Notify anyway is not allowed on a
  scheduled message.
- **Before it goes** the owner can list their scheduled messages,
  change the content or the time, send one now, or cancel it.
- **Files** are uploaded when the message is scheduled. The janitor
  does not treat an upload named in a waiting `scheduled` row as an
  orphan.
- **Running** is as in 9.4. Permission, archived state, time-outs,
  slow mode and the word filter are checked again at that moment.
  The message is posted with `via = {"kind": "scheduled"}` and
  `created_at` the time it was sent.
- **Failure** leaves the row in the failed state with its content
  intact, so the owner can send it by hand or delete it.
- Leaving the conversation or being removed deletes the rows.
- Scheduled messages are not available in sealed chats: the server
  cannot hold a message it must not read, and a device may not be
  on at the time (decision F14).

Stored: `scheduled` (`kind = message`).

Events: as for reminders.

## 11. Recurring check-in forms

A stand-up or check-in: a set of questions posted on a schedule,
with each person's answers in that day's thread.

- **Setting up.** A manager of the channel, from the channel's
  settings: a title, 1 to 20 questions (text or long text, each
  optionally required), optional message text, and a schedule in the
  repeat form of 9.3: daily on chosen days, every N weeks on chosen
  days, or monthly on a day of the month or an nth weekday ("first
  Monday", "last Friday"), at a time in a time zone. Editing one keeps
  its kind. Stored as a `scheduled` row of kind `form`
  whose `content` is a message with a `checkin` form, action
  `thread`, `one_answer` true.
- **Each run** posts that message as the row's owner, with
  `via = {"kind": "scheduled", "name": <title>}`. The form's
  `closes_at` is set to the next run's time, so each day's form
  closes when the next is posted.
- **Answering** follows the `thread` action (7.4): each answer is a
  reply under the answerer's name in that run's thread. One answer
  per person per run.
- **Changing it.** Any manager of the channel can
  list, edit, pause (cancel) or delete the channel's recurring
  forms. Whoever edits one becomes its owner.
- **If the owner is no longer a manager of the channel** when a run is due,
  the row becomes failed and `scheduled.fired` goes out with the
  error. Another manager takes it over by editing it.
- Not available in sealed chats, which carry no forms, nor in a
  conversation shared with other servers, where only `tally` forms
  are allowed.

Stored: `scheduled` (`kind = form`), then `messages` and `forms` per
run.

Events: `scheduled.created`, `scheduled.updated`,
`scheduled.cancelled` and `scheduled.fired` to the owner;
`message.created` per run.

## 12. Saved replies, custom commands and slash commands

### 12.1 Saved replies and custom commands

Both are rows in `saved_replies`: a name and a text.

- A **custom command** has a null `owner_id`. Admins make them and
  everyone can use them.
- A **saved reply** belongs to one person and only they can use it.

Typing `/name` and sending posts the text as an ordinary message
from the person who typed it, in the conversation they are in, with
`via = {"kind": "command", "name": <name>}`. Every rule for posting
applies. Nothing may follow the name; extra text is refused with
`invalid_value` so that nothing is dropped silently (decision F15).
In a sealed chat the client fetches the text and sends it itself,
encrypted.

Names follow the command-name rule in `CONVENTIONS.md`. A name
cannot be one held back (12.3). A custom command cannot have
the name of a registered bot command, nor a bot command the name of
a custom command (`name_taken`). A personal saved reply may share a
name with a custom or bot command, and then wins for its owner.

A saved reply, custom command or bot command may have the name of a
replaceable built-in command (ruling R72). The community's command then wins on
this server: the built-in of that name is hidden wherever the
command can be run (everywhere for a custom command, for its owner
for a personal saved reply, in the bot's conversations for a bot
command). Hidden means not in the command menu, not read by the
grammar of 12.4 and not carried out; each name on its own, so a
command called `mahjong` hides `/mahjong` and not `/mahjongg`. Creating or
renaming a saved reply or custom command to a replaceable built-in's name shows
one line saying it replaces the built-in on this server (for a
personal one, for its owner). The rule is one function in each
client and the server (`hiddenBuiltins`, `model.HiddenBuiltins`)
held to `testdata/commands/parse.json`, `hidden_cases`.

**Protected built-ins.** Only the fun and game built-ins are
replaceable: `/shrug`, `/tableflip`, `/unflip`, `/flip`,
`/pick`, `/shuffle`, `/draw`, `/tarot`, `/mahjongg` and `/mahjong`
(marked `replaceable` in `commands.json`). Every other built-in, and
any added later unless someone decides otherwise, is protected: it is
a core chat function (posting, forms, reminders, conversations,
membership, moderation, people and status), where a look-alike could
mislead someone or capture what they type about another person or
their own safety. Creating or renaming a saved reply, custom command
or bot command to a protected name or one of its aliases is refused
with `name_taken` and the sentence "/ban is a built-in command that
cannot be replaced." (naming the command), for people, bots and
integrations alike. One made before its name was protected is
ignored: it is not listed or run, so the built-in runs; its text may
still be changed, and the admin pages flag it so it can be renamed.

### 12.2 How a command is carried out

Typing `/` in a conversation lists what can be run there, from the
request that lists commands: the built-ins, the person's saved
replies, the custom commands, and the commands of bots that are in
that conversation, each with its description and argument hint.

**Built-in commands are carried out by the client.** It reads the
text by the grammar in this document and makes the ordinary
requests: posting a message with a form, or creating a scheduled
item. The server does not parse them; the request that runs a
command refuses a built-in name with `invalid_value`. The grammars
are fixed here so that the apps, the web chat and anyone's script
read the same text the same way (decision F16). A client may offer a
dialog that makes the same requests without any typing.

**Everything else goes to the request that runs a command**, with
the conversation, the name and whatever was typed after it. The
server looks the name up in this order and stops at the first match:

1. The person's own saved replies.
2. Custom commands.
3. Commands registered by a bot that is a member of this
   conversation.

No match is `not_found`, or `invalid_value` for a built-in's name
(the built-in is the client's to carry out). To post a message that really starts with
`/`, a client posts it as a message.

Any member of the conversation may send a bot command, readers
included. A saved reply or custom command posts a message, so it
needs the level `member`.

A bot command posts nothing. The bot receives `command.invoked` with
the text, the conversation and who asked, and answers for everyone
(an ordinary message) or for the asker alone. `BOTS.md` has the
rest.

### 12.3 Built-in slash commands

This is the full list (rulings R67, R68 and R74). The table every client and
the server hold is `testdata/commands/commands.json`; the server lists it
in the command menu and keeps its names from being taken.

| Command      | Arguments                                 | Does                                              | Sealed chat |
|--------------|-------------------------------------------|---------------------------------------------------|-------------|
| `/poll`      | `Question? \| Option \| Option … [flags]` | Posts a poll (7.5)                                | no |
| `/rsvp`      | `What \| When`                            | Posts an RSVP (7.5)                               | no |
| `/signup`    | `What \| Slot \| Slot … [flags]`          | Posts a sign-up sheet (7.5)                       | no |
| `/approve`   | `What [\| @group] [flags]`                | Posts an approval (7.5)                           | no |
| `/form`      | none                                      | Opens the client's form builder (7.5)             | no |
| `/remind`    | `[target] when \| what`                   | Sets a reminder (9.2)                             | yes, arriving elsewhere |
| `/watch`, `/unwatch` | `@person`                         | Tells you once when they are next around, or stops (9.7; `watchAccount`, `unwatchAccount`) | yes |
| `/me`        | text                                      | Posts the text as an action (`MESSAGE-FORMAT.md` 1.1) | yes |
| `/shrug`, `/tableflip`, `/unflip` | `[text]`              | Posts the text with ¯\\_(ツ)_/¯, (╯°□°)╯︵ ┻━┻ or ┬─┬ノ( º _ ºノ) after it | yes |
| `/spoiler`   | text                                      | Posts `\|\|text\|\|`, the spoiler of Churr Markdown | yes |
| `/flip`      | none                                      | The server flips a coin                           | no |
| `/pick`      | `a \| b \| c`                             | The server picks one                              | no |
| `/shuffle`   | `a \| b \| c`                             | The server shuffles them                          | no |
| `/draw`      | `[count] [deck]`                          | The server draws cards from a deck                | no |
| `/mahjongg`, `/mahjong` | `[count]`                      | The server draws mahjong tiles                    | no |
| `/tarot`     | `[spread]`                                | The server draws a tarot reading                  | no |
| `/join`      | `#channel`                                | Joins a channel (`joinConversation`) and opens it | yes |
| `/leave`, `/part` | `[#channel]`                         | Leaves this channel or the one named (`leaveConversation`) | no |
| `/msg`       | `@person [text]`                          | Opens the chat with them (`openChat`) and sends the text there | yes |
| `/topic`     | `[text]`                                  | Shows the topic, or sets it (`updateConversation`) | no |
| `/invite`    | `@person [#channel]`                      | Adds them to this channel or the one named (`addMembers`) | no |
| `/kick`      | `@person`                                 | Removes them from this channel; they may come back (`removeMember`) | no |
| `/ban`, `/unban` | `@person [reason]`, `@person`         | Bans them from this channel after asking, naming them and the channel, or lifts it (19.1; `banFromChannel`, `liftChannelBan`) | no |
| `/exile`, `/unexile` | `@person [reason]`, `@person`     | Admins only: exiles them from the whole server after asking, or lifts the exile (section 19; `createBan`, `liftBan`) | no |
| `/mute`      | `@person [duration]`                      | Times them out here, default an hour (`timeOutMember`) | no |
| `/op`, `/deop` | `@person`                               | Sets their level to manager or member (`setMemberLevel`) | no |
| `/mod`, `/unmod` | `@person`                             | Sets their level to moderator or member (`setMemberLevel`); managers only | no |
| `/names`, `/who` | none                                  | Opens the conversation's members                  | no |
| `/list`      | none                                      | Opens the channel browser                         | yes |
| `/whois`     | `@person`                                 | Opens their profile                               | yes |
| `/ignore`, `/unignore` | `@person`                       | Blocks them, or stops (`blockAccount`, `unblockAccount`) | yes |
| `/nick`      | name                                      | Changes the display name (`updateMe`)             | yes |
| `/lock`      | none                                      | Locks Churr on this device until a passkey or the access key unlocks it (section 30, `lockSession`) | yes |
| `/away`, `/back` | `[duration]`, none                    | Appears away, or stops (`setAway`, `clearAway`)   | yes |
| `/dnd`       | `[duration \| off]`                       | Do not disturb on, or off (`setDnd`, `clearDnd`)  | yes |
| `/quiet`     | `[duration \| off]`                       | Mutes this conversation for a time, default an hour, or unmutes it (`updateMembership`, `muted_until`) | yes |
| `/help`      | `[command]`                               | Opens the help, for this person only: the commands they can use here, grouped, each with its one-line summary and, when chosen, its full entry (what it does and who sees the result, its arguments, examples, other names, where it works, who may use it, related commands), from which it goes in the message box; then "Things you can type". A command's name after it opens that entry; any other word searches | yes |
| `/say`       | text                                      | Posts the text as it is, even when it starts with `/` | yes |
| `/slap`      | `@person`                                 | Posts the action "slaps @person around a bit with a large trout" | yes |
| `/query`     | `@person`                                 | Opens the (unsealed) chat with them and sends nothing (`openChat`) | yes |
| `/clear`     | none                                      | Jumps to the newest message and marks the conversation read (`setReadPosition`) | no |
| `/close`     | none                                      | In a direct or sealed chat: marks it read and closes it for both people (`setReadPosition`, `closeChat`; section 20.3, ruling R79). Anywhere else: marks it read and hides it from the sidebar until something new arrives (`updateMembership`, `show` `activity`) | yes |
| `/knock`     | none                                      | Opens a picker of the knock channels you can see and are not in, and asks to join the one chosen (12.5, `knock`) | yes |
| `/banlist`   | none                                      | Opens the list of the channel's bans; whoever may ban lifts one there (`listChannelBans`, `liftChannelBan`) | no |
| `/voice`, `/devoice` | `@person`                         | In an announcement channel, lets them post, or stops it (`setMemberMayPost`) | no |
| `/whoami`    | none                                      | Opens your own profile                            | yes |
| `/time`      | `@person`                                 | Says their local time from their profile's time zone, or that it is not known | yes |
| `/invisible` | none                                      | Appears away (`setAway`): Churr has no "appear offline" | yes |
| `/motd`      | none                                      | Shows the server welcome text (`getServer`, `welcome`) | yes |
| `/admin`     | none                                      | Says who the owners and the admins are (`listAccounts`, `role`) | yes |
| `/version`, `/info` | none                               | Says the server's version (`getServer`, `version`) | yes |
| `/ping`      | none                                      | Says the delay of the live connection, measured from its heartbeat | yes |
| `/lusers`    | none                                      | Says how many members the conversation has, how many of them are online where the server counts them (large conversations), and how many people are online on the server (`presence.counts`) | no |
| `/broadcast` | `[duration] text \| clear`                | Admins only: a notice to everyone connected (12.6; `sendBroadcast`, `clearBroadcast`) | yes |

`/kline` and `/unkline` are hidden aliases of `/exile` and `/unexile`
(IRC's names), `/who` of `/names`, `/kickban` and `/kb` of `/ban`, and
`/amsg` of `/broadcast`: they run the same and are not shown in the menu
(`commands.json`, `aliases`); `/help` names them beside their command.

**What `/help` says** is one file, `testdata/commands/help.json`, that
both clients draw from (written into each by `tools/help-gen/gen.py`):
for every command of the table a description of what it does and who
sees the result, its arguments, two or more examples with what happens,
where it works, who may use it and related commands; and "Things you can
type", a row for each thing the composer recognises or offers (mentions
and broad mentions, channel links, emoji, commands and the literal slash,
dates and times with their phrases, events, references, message links,
web addresses, measurements, colours, phone numbers, email addresses,
places, inline dice, checklists) and the formatting of section 2 of
`MESSAGE-FORMAT.md`, with the list's rules and what is different in a
sealed chat. The summary, the usage line, the other names, the sealed
and admin marks and whether a command is replaceable come from
`commands.json`; the decks, spreads, units and the reference patterns
that apply in the conversation are filled in from their registries and
the server. A command hidden from the reader has no entry and is never
named to them. A community command shows the description its maker
gave. `internal/model/help_test.go` fails when a command lacks a full
entry or a kind of entity, block or inline span lacks a row.
`/broadcast` is marked `admin` like `/exile`. `/exile` and `/unexile` are marked
`admin` in the table: they are offered to owners and admins only,
and for anyone else they and their aliases are not commands at all
(not shown and not read, rather than shown and refused). The wording
is "ban" for one channel and "exile" for the whole server everywhere a
person reads it, so the two are never confused; the names of the two
server-wide commands are in the table alone and are one line to
change.

Every command makes the ordinary request, so every rule of
`PERMISSIONS.md` applies and a refusal is the server's, shown in its
words. Only `/flip`, `/pick`, `/shuffle`, `/draw`, `/mahjongg` and
`/tarot` need anything new of the server: it draws their result, as a
`chance`. `/roll` and `/dice` are not built-ins but the dice module's,
run with `runCommand` (12.4).
Each instance and hub has a VRF key for proving chance results
(`DESIGN-FAIR.md`), published with its history at `/.well-known/churr`
and in getServer, pinned by linked servers, hubs and clients; the web
chat and the app warn when a server shows another. Results are not yet
proved with it.
Arriving live, a result plays once and never again on a press
(`MESSAGE-FORMAT.md` 1.2, ruling R87): dice roll and then show their
total; cards and tiles lie face down and are turned over one after
another, a tarot reading laid out in its spread's shape (the Celtic
cross and its staff, a horseshoe, a row) with its summary after the
last card; with reduced motion it all simply appears. A card's or
tile's name is not listed: it pops up over the card on hover and on a
tap, and a tarot card pressed opens its position, name, orientation and
meaning.

A drawn playing card shows its rank and suit symbol in the theme's
`pink` (its red) for hearts, diamonds and the red joker, and in ink for
spades, clubs and the other jokers, on its text face; the sentence
around it stays ink. The suit symbol and the card's name say the suit as
before. Tarot cards and mahjong tiles are drawn in ink.

Not built-in (ruling R67): `/quit`, `/mode`, `/notice`, and anything
needing an outside service; of the rest of IRC's list (ruling R74), none
beyond the above, and no lock of the chat yet (not decided). `later` and `status` cannot be taken
by custom commands, saved replies or bots, held back for commands to
come (decision F17); `help` was held back the same way and is now a
built-in. The replaceable names above can be taken, and the
community's command then wins on this server (12.1, ruling R72); so a
saved reply or bot command whose name becomes a replaceable built-in
later keeps working. The protected ones cannot (12.1).

In a sealed chat only the commands marked so are offered. The others,
typed there, are refused by the client with the reason: the forms and
results because the server cannot write into a sealed chat, the rest
because a sealed chat has no such thing.

### 12.4 The grammar of the built-in commands

A command is `/name` at the very start of the text, then whitespace and
the rest, which is trimmed. A name that is not a built-in, or is a
built-in hidden for the reader (12.1), is not read here; a case of
`parse.json` with `hidden` says which. `testdata/commands/parse.json` holds the cases every client
passes; the result names an error the client words itself.

- **text**: the rest, which must not be empty (`/me`, `/spoiler`,
  `/nick`). **optional text**: may be empty.
- **person**: one word, `@username` or `@username@server.name` (the `@`
  may be left out); a second word is a mistake (`/kick @lee now`).
  With text or a channel after it, the rest follows the person.
- **channel**: a channel link from the picker, `<#conversation-id>`, or
  a name typed with or without `#`, which must name exactly one channel
  the person can see.
- **duration**: whole numbers with `m`, `h`, `d` or `w`, run together
  (`30m`, `1h30m`, `2d`), or a bare number of minutes; more than zero and
  at most 366 days. `off` where the table says so. A `/mute` lasts at
  most 28 days, the server's limit on a time-out (section 15, decision
  F19; `parse.json`, `mute_max_seconds`); a longer one is refused
  before anything is sent (`mute_too_long`).
- **options**: separated by `|`, each trimmed and not empty; 2 to 20.
- **draw**: `[count] [deck]`: a count alone, a deck alone, or a count
  then a deck; the deck by name or alias (`standard`, `jokers` or `54`,
  `tarot`, `hanafuda`, `mahjong`, `shoe6` …, see `decks.json`) and the
  count at most its size. **count** (`/mahjongg`): 1 to 144.
- **spread**: one word, a spread's name or alias: `three` (default),
  `celtic`, `horseshoe`, `relationship`.
- **broadcast**: `clear` alone takes the broadcast down; otherwise an
  optional duration with its unit (`15m`, `2h`, `1h30m`; a bare number is
  text) and then the text, which must not be empty. Without a duration it
  shows for 10 minutes; at most 24 hours (`bad_broadcast_time`).
- **The literal slash** (ruling R74): text that starts with whitespace and
  then `/` is never a command. It is posted as a message from the `/`, the
  whitespace dropped (`parse.json`, `literal_cases`; `model.LiteralSlash`,
  `literalSlash`, `literal`). Text in a code span or block starts with a
  backtick, so it is never a command either; `/say` posts any text as it is.
- **dice** (read by the server, `testdata/commands/dice.json`): terms
  joined by `+` and `-`; a term is `NdM` (N defaults to 1, `d%` is
  `d100`), `NdM` with `kh`, `kl`, `dh` or `dl` and a count (keep or drop
  the highest or lowest; `k` alone is `kh`, the count defaults to 1), a
  whole number, or `adv` / `dis` for `2d20kh1` / `2d20kl1`. Case does
  not matter; spaces may stand beside `+` and `-` only. Nothing is
  `1d20`. A bare `d` is never drop (`4d6d1` is refused): only `dh` and
  `dl` drop, since `d` alone starts a die. **Rows**: a roll may be several
  expressions separated by commas (spaces around them allowed), each
  rolled as its own row with its own total, shown on its own line:
  `4d4+d6, 3d10k2`. An empty row is refused. Limits: 100 characters and
  100 dice for the whole roll, 8 rows, 10 terms a row, 2 to 1,000 sides,
  numbers up to 10,000. Inline dice are one row.
- **/dice** (`dice.json`, `d6`): the same grammar, except that in each
  row a first term that is a bare number is that many d6, and only that:
  `/dice 4` is `4d6`, `/dice 4+3` is `4d6+3` (the later 3 is added),
  `/dice 4k3` is `4d6k3`, `/dice 4, 2+1` is `4d6, 2d6+1`; a term written
  with a `d` is as written (`/dice 2d8` is `2d8`); nothing is `1d6`.
- `/roll` and `/dice` are the dice module's commands
  (`DESIGN-MODULES.md` section 9.1), not built-ins: `listCommands` lists
  them with the kind `module`, their forms (dice, or nothing) and their
  help, which comes from the module; the client sends what was typed
  after the name with `runCommand`, and the server reads it, rolls and
  posts the roll as the person's message. They are for people: the
  module refuses them to bots, and they are not listed for one.
- **The command table** (`DESIGN-MODULES.md` section 6.5): where a
  saved reply or custom command, a bot's command, a module's command and
  a built-in share a name, the admin's order of those sources
  (`getCommandSettings`, `updateCommandSettings`; by default in that
  order) says which is listed and which runs; a protected built-in
  always wins. The admin may give a module's command another name: it
  then answers to that name only, and its help says so. Each change is
  written to the audit log. Dice it cannot read are
  refused with `invalid_value` in its words; the grammar is read by the
  server alone (`internal/modules/dice`).
- **Switching a module off** (`DESIGN-MODULES.md` section 6.5): an admin
  switches a built-in module on or off for the whole server
  (`adminListModules`, `setModule`); a named channel's managers switch
  off there what the server has on, and on again
  (`listConversationModules`, `setConversationModule`), but cannot
  switch on what the server has off. Where a module is off its commands
  are not listed and `runCommand` refuses them with `feature_off`, the
  objects it claims stay text (`[[1d6]]` is not rolled), and what it
  made is served as its text: a roll's message without its `view`,
  inline dice without their object. Nothing stored changes, so
  switching it on again serves them as before. Each switch is written to
  the audit log. A module that panics is switched off for the whole
  server; one that panics or does not answer within 2 seconds is
  refused with `unavailable`, logged and audited.

Stored: `saved_replies`, `commands`, `module_switches`, `module_values`.

Events: `command.invoked`. Saved replies and custom commands have no
event; clients fetch the list when the `/` menu opens.

### 12.5 Knock channels and knocks

A named channel has one of three kinds of access (rulings R74 and R79):

- **public**: owner, admin and member accounts see, read and join it;
- **knock**: they see its name, topic, member count and managers (in
  the channel browser, in a search of channel names, and behind a
  channel link) but cannot read it, list its members, see who is
  online in it, find its messages in search or join it; they ask to
  join, and are in once a moderator or manager accepts or adds them;
- **private**: nobody outside it sees it; invitation only.

Underneath, a knock channel is a private channel with the `knock`
column set (migration 0007), so everything that keeps a private
channel's content from outsiders keeps a knock channel's too; it is
shown with `access` `knock` and, to someone outside, `manager_ids`, and
nothing of its welcome text, members or call. Guests and bots outside it
do not see it. A channel ban hides it from the one banned.

Changing the access is a manager's (`updateConversation`, `access`):
public and knock change into each other freely; either may become
private; a private channel never becomes either (the rule that a private
channel is never made public). Becoming knock or private from public
drops the channel for non-members as making it private does. Knock is
for a top-level channel not shared with other servers: a sub-channel
cannot be one, a knock channel cannot be given a parent, and a shared
channel's access is not changed here, and a knock channel cannot be
shared (`conflict`); knock channels are this server's alone for now.

`/knock` takes no name: it opens a picker of the knock channels the
person can see and is not in (several may share a name), and the
browser and a channel link offer "Ask to join" on a knock channel.
Asking (`knock`) leaves a request waiting (`knocks`); at most one per
person and channel, and asking again returns it. Its moderators and
managers get `knock.created`, see a bar "N people ask to join" in the channel with
Review, and add or decline each (`listKnocks`, `answerKnock`). An
Owner or Admin outside the channel answers none. Adding is the ordinary
add at the channel's default level, with all its rules.
The person is told the outcome plainly, with the channel's name and not
who answered (`knock.answered`). Owner, admin and member accounts may
knock: at most ten knocks in a rolling hour that were not accepted
(waiting or declined; one that was accepted does not count). Answers are in the audit log.
Requests to join a private channel on a hub wait in `knocks` too, and
count toward the same ten (21.5b); a request from another instance is
told at once to this instance's moderators and managers on the hub's
lists.

Stored: `conversations.knock`, `knocks`. Events: `knock.created`,
`knock.answered`, `conversation.updated`.

### 12.6 The server broadcast

`/broadcast [duration] text` (alias `/amsg`, ruling R74) is an admin's
notice to everyone connected, for example "upgrading in 5 minutes, expect
15m of downtime". Admins (owners included) only; the command is not
offered to anyone else.

- It is shown as a banner above everything, with "Server notice" and who
  sent it, until its time is up (10 minutes unless given, at most 24
  hours) or the person dismisses it on that device. A screen reader is
  told it once.
- One line of plain text, up to 500 characters, with the inline Markdown
  of messages.
- Every connection gets `server.broadcast` at once, and the one showing
  is in `getStatusSummary`, which every client fetches when it connects,
  so whoever connects while it shows sees it too. A new one replaces it;
  `/broadcast clear` takes it down for everyone (`server.broadcast`
  without one).
- It is not a message in any conversation. It is kept in memory only (a
  restart clears it) and written to the audit log (`broadcast.sent`,
  `broadcast.cleared`). It does not cross to linked servers. Ten an hour
  per admin.

Stored: nothing but the audit log. Events: `server.broadcast`.

## 13. Welcome texts and starting channels

**Server welcome text.** The `welcome.text` setting, Churr Markdown,
set by admins. A client shows it once, the first time a person signs
in, and records that by setting `prefs.welcome_seen_at`. It is shown
again from the server's "about" page on request. It is not a
message.

**Channel welcome text.** `conversations.welcome`, set by a manager
of a named channel. It is returned with the
channel and a client shows it to a person when they join or are
added, above the messages, until dismissed. It is not a message and
nobody else sees that it was shown.

**Default channels.** The setting `channels.default` (admin console,
Messages and channels: "Default channels") lists the named channels a
new account joins at sign-up (ruling R103).

- When an owner, admin or `member` account is created or claimed, it
  gets a `members` row in each default channel that is not archived or
  locked, at the channel's default level, with mode `mentions` and
  `read_seq` at the channel's latest, so it starts with nothing
  unread. That is the one time it applies: a default channel can be
  left like any other.
- A new guest or bot joins none of them.
- A channel added to the list later is not joined for the people
  already here: each is **offered** it, as a group's channel is
  (section 14.4, source `default`), and may accept, decline, or join
  later from Browse while it stays a default channel. Taking a channel
  off the list takes away the offers that wait.
- A fresh instance's `#general` is its one default channel (section
  26).
- A channel `@everyone` has been added to is offered to every owner,
  admin and member (section 14.4), new accounts included.

**Join notices.** Everyone who arrives in a private channel posts a
`member_joined` system notice there, by whatever route: joining,
being added (the notice names who added them), a group added to the
channel, or a change to a group already in it (ruling R87). Nobody
can turn them off: the members are told who can now read what they
say. Someone an Owner or an Admin adds by override is announced with an
`owner_override` notice instead (21.12). The people a channel is
created with, and those a moved thread or a ticket starts with, are
not announced. Public channels, knock
channels, ad-hoc channels and sealed chats post none; their member
lists change and `member.joined` is sent as always. In a private
channel shared with other servers the arrival's own server posts the
notice, and it travels to the others like any message.

Stored: the setting, `accounts.prefs`, `conversations.welcome`,
`channel_groups`, `members`.

Events: `server.updated`, `conversation.updated`,
`channel_group.added`, `member.joined`.

## 14. Groups

### 14.1 Ordinary groups

A row in `groups` with a handle, a name, a description, an optional
colour and the `open` switch; who is in it is in `group_members`.

- Admins create, edit and delete groups and set who is in them.
- Local accounts of any role can be put in a group, including guests
  and bots. Remote accounts cannot.
- The handle shares a namespace with usernames and follows the same
  rule.
- A colour is one of the card colours in `MESSAGE-FORMAT.md` and is
  drawn as a label beside members' names in member lists and on
  profiles.
- Mentioning the group writes `mentions` rows as in section 2.5.
- Deleting a group removes it from every channel, and old messages
  that mention it show the handle as written.

### 14.2 Open groups

A group with `open` true can be joined and left by any owner, admin
or `member` account by themselves, from the group list or through a
form button (7.4). Guests and bots are put in by an admin.

### 14.3 Built-in and provider-mapped groups

- `@everyone` and `@admins` have rows in `groups` with `builtin`
  set and no rows in `group_members`. Who is in them follows
  `accounts.role` (SCHEMA section 4). They cannot be edited, opened
  or deleted.
- A group with `provider_id` and `provider_group` set takes its
  members from the identity provider. Each time a person signs in
  through that provider, the server reads their groups from the
  provider's group claim and adds or removes that person's
  `group_members` rows for the groups mapped to that provider.
  Membership therefore changes at sign-in, not in between; with the
  24-hour session for company sign-in it is at most a day stale.
  Admins cannot set the members of a mapped group by hand, and it
  cannot be open.

### 14.4 Adding a group to a channel

The request names a channel, a group and a level: `reader`, `member`,
`moderator` or `manager`. A group grants nothing by itself; adding it
to a channel at a level gives its people **access** at that role
there, never above the guest's: a guest account is a guest in every
channel. Access is not a place (ruling R103): nobody is put in a
channel they did not accept.

1. A `channel_groups` row is written.
2. Every account in the group that is not in the channel is
   **offered** it: a `channel_offers` row `offered`, and
   `channel_offer.added`. Both clients show the offer in the left
   panel with **Accept** and **Decline**; a public channel can be
   opened and read from there before answering, a private or knock
   channel cannot.
3. **Accept** makes the account a member: its `members` row has
   `via_group` true and `level` the highest of its direct level and
   all its group levels there. **Decline** closes the offer and it
   leaves the left panel. While a group gives access, the account may
   still join later from Browse channels, whatever the channel's
   access, without an invitation or knocking: Browse lists the
   channel for it, private included.
4. An account already in the channel takes the group's level at once
   (it is there by its own choice).
5. From then on every change to the group, to the row, or to an
   account's role runs the same working-out for the accounts
   affected (SCHEMA section 5, `members`): someone newly in the group
   is offered the channel, someone who left the group loses the offer.

A guest is held to `member` at most, so a group that gives moderators
or managers gives a guest `member`; in a channel shared with other
servers a guest is offered nothing. A banned account is offered
nothing.

Consequences:

- When the last group that gives an account access goes, the account
  loses its offer (whatever its answer was), and if it was in the
  channel only through groups it leaves it, losing its thread states
  and mentions there like anyone who leaves. A group that gives access
  again later asks again.
- An account in a channel through a group may leave it like anyone
  (its offer counts as declined) and may join again while the access
  lasts. It cannot be removed from it one at a time.
- So the built-in group `@everyone` added to a channel offers it to
  every owner, admin and member; nobody is made a member of it.
- Groups are added to named channels only, and not to a locked one.
  Locking a channel detaches its groups and keeps their people
  (section 21.6).
- Changing a group's level in a channel is the same request with a
  new level; removing it deletes the row.

Stored: `groups`, `group_members`, `channel_groups`, `members`,
`channel_offers`.

Events: `group.created`, `group.updated`, `group.deleted`,
`group.member_added`, `group.member_removed` (to accounts that may
see the group). In a channel: `channel_group.added`,
`channel_group.changed` and `channel_group.removed`, each standing
in for one event per person; then `member.joined`, `member.left`
and `member.level_changed` as single people come and go with the
group.

## 15. Time out

Stops an account posting and reacting for a set time. It can still
read.

- **In one channel**: set by a moderator or manager there. A
  moderator does not time out a moderator or a manager of the
  channel, and nobody times out an owner of the instance or
  themselves; an admin is timed out like anyone.
  **Server-wide**: set by an admin (`conversation_id` null).
- The request gives the account, a duration from 60 seconds to 28
  days, and an optional reason (decision F19). A `timeouts` row is
  written with `until`.
- A new time-out for the same account and scope replaces the one in
  force: the old row gets `lifted_at`.
- It ends by itself at `until`, or earlier when someone who could
  have set it lifts it (`lifted_at`).
- What is refused while it lasts is in PERMISSIONS section 11. The
  refusal is `timed_out`, and its message says until when.
- The account is told, with the reason and end time. Other members
  are not.
- Written to the audit log, except in an ad-hoc channel.

Stored: `timeouts`.

Events: `timeout.started` and `timeout.lifted`, to the account and
to those who can manage time-outs in that scope.

## 16. Slow mode

A channel setting: `conversations.slow_mode_seconds`, 0 for off,
otherwise 5 seconds to 6 hours. Set by a moderator or manager of the
channel.

- It applies to posting a top-level message or a reply. Not to
  edits, reactions, ticks or form answers.
- Before accepting a post the server compares now with
  `members.last_post_at`. If less than the interval has passed the
  post is refused with `slow_mode` and `retry_after_seconds`.
  Otherwise the post goes through and `last_post_at` is set.
- Exempt: the channel's moderators and managers, bots and webhooks.
- A scheduled message that falls due inside the interval fails
  (section 10).
- Clients show the interval and a countdown in the message box.

Stored: `conversations.slow_mode_seconds`, `members.last_post_at`.

Events: `conversation.updated`.

## 17. Word filter

An admin's list in `word_filters`: a pattern, whether it is a
regular expression, and an action.

- **Plain pattern**: a word or phrase, matched without regard to
  case and only at word boundaries, so `ass` does not match
  `class`.
- **Regular expression**: RE2 syntax, which cannot be made to run
  for unbounded time. Matched without regard to case. An expression
  that does not compile is refused when it is saved.
- **Checked**: the text of every message posted or edited in a
  channel by a local account, including bots and webhooks, and
  scheduled messages when set and when sent. Not checked: sealed
  chats (the server cannot read them), file contents, card text,
  form labels and answers.
- **`block`**: the post or edit is refused with `blocked_by_filter`.
  The message tells the author a word is not allowed here and does
  not quote the pattern. Nothing is stored and nothing is logged.
- **`flag`**: the message goes through, and the server makes a report
  on it (section 18) with `source = filter`, no reporter
  (`reported_by` null) and the filter's identifier as the reason. One
  flag per message, however many patterns match.
- If both kinds match, `block` wins.
- A message arriving from another server cannot be blocked, since it
  is already part of the shared channel. A `block` match on it is
  treated as `flag`.
- Flags follow the routing of reports (section 18): a flag in a
  private channel reaches its moderators and managers, and the admins
  when it is about one of them; a flag in a chat reaches the admins.
- Changing the list is written to the audit log. Matches are not.

Stored: `word_filters`; `reports` for flags.

Events: `report.created` for a flag. The list itself has no event;
the admin console fetches it.

## 18. Reports

Anyone who can read a message can report it, with a reason of up to
1,000 characters. Anyone who can see a person can report them, for
their name, status or picture. Nobody reports in a sealed chat.

1. A `reports` row is written. For a message it keeps a snapshot of
   the message as it is then: where it was (a public channel, a
   private channel, or a chat), its text, its author, its times and
   the names of its files, never the files. Deleting or purging the
   message, or the chat it was in, leaves the report and its snapshot
   as they are. For a person it keeps their username, display name,
   status text and picture address. A second report of the same
   message, or person, by the same person while the first waits
   returns the first.
2. The handlers are told:

   | Report made in         | Handlers                                 |
   |------------------------|------------------------------------------|
   | Public channel         | Its moderators and managers, and every admin, for what is the instance's to do (an exile, a purge) |
   | Private channel        | Its moderators and managers; and every admin as well when the report is about one of its moderators or managers, or the channel has none besides the author |
   | Direct or group chat   | Every admin: a chat has no moderator or manager (PERMISSIONS 2.4) |
   | A person               | Every admin                              |

   The moderators and managers are those whose stored level there is
   `moderator` or `manager`, and they are told only of messages from
   their first join there on (PERMISSIONS 4.4). Nobody is told of, or
   handles, a report about themselves.

   **What an admin sees of a report sent to the admins** (marked
   `to_admins`) from a private channel or a chat they are not a
   moderator of: who reported it and why, and the snapshot (the
   message's text, author, time and file names; for a channel, that
   it was a private one, and its name as the console shows any named
   channel). Nothing else: not the conversation, not the message as it
   is now, not the messages around it, not the files. A report in a
   chat tells the admins that the reporter and the author share a
   chat; the report dialog says so before it is sent.

3. A handler chooses an outcome, stored in `outcome` with
   `handled_by` and `handled_at`:

   | Outcome     | Effect                                           |
   |-------------|--------------------------------------------------|
   | `dismissed` | Nothing else.                                    |
   | `deleted`   | The message is deleted (4.2) by the handler. An admin who is not a moderator or manager of the channel purges it instead where they can read it (5.3), which deletes it too; where they cannot (a private channel, a chat) it is refused. |
   | `timed_out` | The author is timed out for a duration the handler gives (section 15): in that channel by its moderators and managers, on the whole instance by an admin who is not one. |

   Choosing an outcome settles every waiting report on that message,
   or that person.
4. The reporter is not told the outcome, and the author is not told
   of the report or who made it.
5. A private channel or a chat whose last member leaves is deleted
   with its content (PERMISSIONS 6), except while a report about it
   waits: it is deleted when the last such report is handled.

Handling is written to the audit log, without the message text: for
public and private channels with the channel and the message, and for
a chat's report, which only admins handle, without naming the chat or
the message.

Stored: `reports`.

Events: `report.created` and `report.handled`, to the handlers only.

## 19. Removal and bans

**Removing an account** (an admin; an owner for an admin; nobody
removes an owner):

1. `removed_at` and `removed_by` are set and the rows in SCHEMA
   section 14.3 are deleted, in one transaction.
2. Its live connections are closed.
3. `member.left` is written in each conversation it was in. The
   last-manager and last-member rules run for each.
4. Its messages stay, shown under its name as a former member.
5. An audit row is written.

**Exiling** (the server-wide ban, IRC's kline; `/exile`, ruling R72;
the API calls it a ban, `createBan`) is removal plus `bans` rows: one
per identity the
account had in `identities`, with the provider and subject copied
before those rows are deleted, or one row with neither for a
passkey-only account. From then on a company sign-in whose provider
and subject match a ban that is not lifted is refused, at sign-in
and at registration. Lifting an exile sets `lifted_at`; the person may
then register again and gets a new account. The removed account is
not brought back. Every screen a person reads says "exile" for this
and "ban" for 19.1 only.

Stored: `accounts.removed_at`, `removed_by`, `bans`.

Events: `account.removed` to accounts that could see it;
`member.left` in each conversation.

### 19.1 Channel bans

IRC's ban beside its kline (ruling R72): one account kept out of one
channel. `/ban @person [reason]` and `/unban @person`, the member
menu and the channel's settings make the same requests.

- **Who**: whoever may remove members there (PERMISSIONS 6.1): the
  channel's moderators and managers, in a named channel. Never in a
  direct or group chat, which has no manager or moderator, nor in a
  sealed chat. A moderator does not ban a moderator or a manager of
  the channel; nobody bans an owner of the instance or themselves. An
  admin is banned like anyone, and bans nobody unless a moderator or
  manager of the channel. Lifting is for the same people.
- **What it does**: the account is taken out of the channel, through
  a group too, and kept out: it cannot join, cannot be added
  (`forbidden`, "banned from this channel"), and a group it is in
  brings it in no more. A public channel is hidden from it altogether:
  not in the browser, not in search, not readable (`not_found`). The
  in-memory member lists drop it with the same transaction, so it is
  not sent the next message.
- **Reason and end**: an optional reason of up to 1,000 characters and
  an optional length, a minute to 366 days; without one the ban lasts
  until lifted. A new ban replaces the one in force. Lifting does not
  bring the person back: they may join or be added again.
- **Shown**: in the channel's settings, to its moderators and
  managers, with who banned, when, until when and why; in the admin
  console beside the exiles, for every named channel, to see only.
- **The one banned** is told once, plainly, as it happens: "You were
  banned from #name", with the end and the reason if there are any
  (`channel_ban.created`), and their open clients drop the channel at
  once. Nobody else is told beyond `member.left` (reason `banned`).
- **Audit**: `channel_ban.created` and `channel_ban.lifted`, except in
  an ad-hoc channel (nothing done inside one reaches the audit log).
- **Sub-channels** do not inherit a parent's ban: one level, nothing
  inherited (7.2 of PERMISSIONS). Ban someone from each.
- **Shared channels**: a channel shared with other servers, or owned
  by a hub, takes no new bans for now (`conflict`): a ban there would
  need every server to keep it, which the link protocol does not yet
  carry. Removal still works there; in a channel a hub owns its
  moderators and managers remove someone on every instance (21.5b). A
  ban made before a channel was shared still holds on this server and
  can be lifted.

Stored: `channel_bans`.

Events: `channel_ban.created` and `channel_ban.lifted`, to the account
and to the channel's moderators and managers; `member.left` with reason `banned` in
the channel.

## 20. Personal blocks, chat requests and closing a chat

Rulings R77, R78 and R79. Three things that belong together: who may
open a chat with whom, how a chat ends, and how a block keeps someone
out without ever telling them so.

### 20.1 Personal blocks: deaf ears

A `blocks` row: `account_id` has blocked `blocked_id`. Any person
may block any other account. **Nothing ever tells the blocked person**
(ruling R77): no refusal, no error text, no answer shaped differently,
no count and no shortcut that is measurably faster. Whatever they do
takes the same path as with anyone else up to the moment something
would be delivered to the blocker, and there it stops.

While X blocks Y:

- **New chats.** Y can start a direct chat or a sealed chat with X.
  To Y it is exactly what it would be with anyone: a chat request
  that waits (20.2), or, if X approves new chats automatically, a
  chat that appears open and that Y can write in. Either way it
  waits for ever, and X receives nothing: no request, no
  notification, no row in any list.
- **Group chats.** Y can start an ad-hoc channel that includes X, or
  invite X to one. To everyone in it X appears invited, not yet joined,
  for ever, as someone who never answers (20.2); X is not told. If X
  would have been added at once (X approves new direct chats
  automatically, or the two have an open direct chat), X appears added
  to Y alone, and the other members see who is really there.
- **Presence.** To Y, X is always online, with no status, no
  availability, no do not disturb mark, no call and no last seen
  other than "online", and Y is sent no change of any of them.
  `people.Service.PresenceSeenBy` is the one rule; every path that
  shows one account's presence to another applies it: the account
  fetched alone and in lists (where Y is shown X in a chat that looks
  open without sharing any real conversation, X is online there too,
  as in a chat they really shared), `account.updated`, `presence.changed`
  and the other account events, the first answer to a
  `presence.subscribe` and the batches after it, typing notices, and
  watches (9.7), which answer at once that X is around. When the
  block is made Y is sent, only where it differs, what X now looks
  like to them (online, no status), as if X had come back and
  cleared it; when it is lifted, the truth again.
- **Typing.** Neither is shown the other typing.
- Y's messages in conversations both are in reach X as usual, and
  X's clients collapse them behind "message from someone you
  blocked", using X's block list. Y's mentions of X write no
  `mentions` row; Y's messages never notify X and are not counted in
  X's unread numbers or badges (sections 2.1 and 3.7); a call Y
  starts does not ring X; notify anyway from Y does nothing for X,
  and Y cannot tell (section 3.8).
- Adding X to a named channel is still refused, with wording that
  does not say why: there X would be a member everyone sees.

Conversations they already share stay as they are. **Unblocking**
deletes the row. From then on Y's messages count and notify like
anyone's, including any that are still unread; a chat request of Y's
that still waits appears in X's list of requests (without a second
notification); and what Y wrote in a chat that appeared open to them
appears for X only if X accepts that request. Someone X was shown as
added to an ad-hoc channel stays a person X is shown there and nothing
more.

An admin can turn blocking off for the server
(`blocking.enabled`). Stored blocks then have no effect and new ones
are refused with `feature_off`.

What could not be made identical, and why:

- In an ad-hoc channel of three or more that Y starts with X in it,
  or adds X to, where X would have been added at once, the other
  members are shown the truth, so they could tell Y. Y is told live by a `conversation.updated` carrying the
  member list Y sees on a reload, in place of the `member.joined` a
  real addition sends; the channel's sequence numbers move as far as
  for a real addition. A client that looks for `member.joined`
  itself would see the difference.
- What 20.4 says of other servers.

### 20.2 Chat requests and invitations

**First contact** (ruling R103): nobody ends up in a conversation they
did not accept, and until they accept nothing from it reaches them: no
message, no notification other than the one request, and no ring for a
call. A request has one of four kinds:

| Kind | Made by | Opens at once when |
|------|---------|--------------------|
| `direct` | Starting a chat with one other person | They approve new direct chats automatically |
| `sealed` | Starting a sealed chat | They approve new sealed chats automatically |
| `group` | Including someone in a group chat (an ad-hoc channel of three or more), when it is started or later | They approve new direct chats automatically, or already have an open direct chat with whoever included them |
| `channel` | A moderator or manager adding someone to a named channel (`/invite`, the member list, creating the channel with people), an owner's override or appointment from the admin console | Never |

A group chat request and a channel invitation (together, invitations)
work like a chat request, below, with these differences:

- **The others** go on using the conversation. They see the person
  invited as "invited, not yet joined" (`invited_ids`, and listed
  `invited` after the members), until they accept or decline.
  Removing them withdraws the invitation.
- **The person invited** sees, under Requests, who invited them and to
  what: for a group chat the names of who is in it, nothing more; for
  a channel its name, topic and access, and whether it is an owner's
  override. A public channel can be opened and read before answering;
  a private or knock channel and a group chat cannot.
- **Accept** makes them a member at the level the invitation brings.
  In a group chat or a private channel they read from their join, not
  earlier history, as a newcomer to a private channel does (ruling
  R87): the people there spoke before they were in it. Joining by any
  other route (a public channel, a knock answered) answers the
  invitation too.
- **Decline** takes them off the invited list; the one who invited
  them may not invite them again for 24 hours. **Dismiss** and
  **Block** leave them listed as invited, for ever, as someone who
  never answers.
- Someone who has blocked the one inviting is invited and never told:
  invited for ever to everyone. Where they would have joined at once,
  the one inviting is shown them joined (20.1).
- One open invitation per person and conversation, whoever sent it.
- The 24-hour wait applies after a decline of any request from the
  same person. Group chat requests count with direct and sealed ones
  for the caps below; channel invitations have a cap of their own: at
  most 100 a rolling day that were not accepted.
- Starting a group chat with the same people, by someone invited to
  it, accepts the invitation.
- Group chats that run between servers, and named channels shared
  with other servers, take their people from other servers as before
  (20.4); requests do not travel between servers.

Joining a public channel oneself, knocking, approvals, and channels
offered through a group or as a default channel (section 14.4) are
not invitations.

Each person has two preferences, both **off** by default:
"Auto approve new direct chats" (`prefs.auto_approve_direct`) and
"Auto approve new sealed chats" (`prefs.auto_approve_sealed`). With
one on, a new chat of that kind with the person opens at once, as
it always did. With it off, starting one is a **request**:

- **The person asking** sees the chat in their sidebar at once,
  marked as waiting (`chat_state` `waiting`), with the other person
  among its members. Nothing can be written in it: no first message,
  no file, no typing, until it is accepted. A sealed chat waits with
  its epoch record 1 already made and sealed to both people's
  devices (`SEALED.md` section 6), so it is ready the moment it is
  accepted. They may **withdraw** it while it waits; the chat goes.
- **The person asked** sees a request: who is asking, as their
  profile already shows them (display name and username, avatar,
  whatever else is visible), and for what kind of chat. Nothing
  more. It is notified once like a mention, unless they are in do not
  disturb, and listed under "Requests" with a count in both clients.
  Four answers:
  - **Accept**: the chat opens for both.
  - **Decline**: "I don't want to". The person asking sees it was
    declined (`chat_state` `declined`); the chat stays theirs to read
    and is no longer the chat between the two.
  - **Dismiss**: it leaves the list. To the person asking it goes on
    waiting, for ever, as if not yet seen.
  - **Block**: blocks the person asking (20.1). To them it likewise
    goes on waiting.
- Starting a chat with someone whose request to you is open accepts
  it.

Limits, all about what the person asking can see of their own
requests, so none of them can tell a block or a dismissal from a
request not yet seen:

| Rule | Answer |
|------|--------|
| One open request per pair and kind | Asking again gives the chat that waits |
| After a decline, or after withdrawing, the same person cannot be asked again for 24 hours | `rate_limited` |
| At most 10 requests a rolling day that were not accepted and kept: waiting, declined, dismissed, blocked, withdrawn, or accepted and closed within 10 minutes | `rate_limited` |
| At most 100 a day that were accepted and kept, chats that opened at once included | `rate_limited` |

There is no wait after a chat was accepted and later closed. Existing
chats are not affected by any of this. A chat with oneself, with a
bot, or with someone on another server opens as before (20.4).

Stored: `chat_requests`, `ghost_members`, `conversations.request_state`,
`prefs.auto_approve_direct`, `prefs.auto_approve_sealed`;
`chat_requests.level` and `override` for channel invitations.

### 20.3 Closing a chat

Either person can **close** a direct chat (an ad-hoc channel they are
shown with one other person in it) or a sealed chat: from its menu,
or with `/close` typed in it, which marks it read and closes it. (In
any other conversation `/close` keeps its meaning: hide it from the
sidebar.) Once closed:

- both keep the history, read-only, marked closed (`chat_state`
  `closed`); neither can write in it;
- nothing says who closed it;
- it is no longer the chat between the two. "Ask to chat again"
  starts a new one, which is a new chat request unless the other
  person approves chats automatically. No 24-hour wait applies.

A chat whose request still waits is withdrawn rather than closed.

**Sealed chats.** The scheme has no leaving: a sealed chat's members
are its two accounts for good, and there is nothing to take a key
away from. Closing is therefore the server refusing every new
message and every new epoch record in it from then on, as it does
for a chat whose other person was removed (`SEALED.md` section 16).
History stays readable on every device from the keys it already
holds. "Ask to chat again" starts a fresh sealed chat: a new
conversation identifier and a new epoch record 1, made by the asking
device as in `SEALED.md` section 6. Nothing new is invented; what
the scheme cannot do is a cryptographic end: a dishonest server could
still accept messages in a closed chat, exactly as it could in a
read-only one.

Stored: `conversations.closed_at`, `chat_requests.closed_at`.

### 20.4 Across servers

A block is kept on the blocker's own server. This server does all of
20.1 between its own people. Between servers:

- If X here has blocked Y on another server and Y starts a direct
  chat, a sealed chat or an ad-hoc channel with X, X's server takes
  the offer as any other (and answers the request for X's device
  lists as for anyone) but leaves X out: Y's server sees the chat
  taken, Y writes in it, and nothing reaches X. In a chat of two, X's
  typing does not leave X's server.
- X's server tells Y's server, and no other, that X has blocked Y
  (`LINK-PROTOCOL.md` section 17a). Y's server then shows Y X online
  with nothing more, by every path presence takes, and passes on none
  of X's typing, in any conversation; Y is told nothing. The price is
  that Y's server, and its operator, know of the block. It is held in
  memory there, never stored, and no other server or hub sees it.
- Still distinguishable: adding X to an existing ad-hoc channel that
  runs between servers: X's server does not join X, so Y never sees X
  arrive. Until Y's server has been told (just after the block, or
  after it restarts), Y may be shown X's presence as anyone is.
- If X on another server has blocked Y here, this server does as X's
  server says for presence and typing, and for nothing else.
- Chat requests do not travel between servers: a direct or sealed chat
  with someone on another server opens as before, and their
  preferences are not asked.

Events: `block.added`, `block.removed`, `chat_request.received` and
`chat_request.removed` to the account's own connections;
`conversation.updated` with `chat_state` to the members of the chat.

## 21. Pins, bookmarks, sections and channel settings

### 21.1 Pins

A pin marks a message for everyone in a channel.

- A manager of the channel pins and unpins; a moderator does not,
  since a pin shows a message to the channel's future members, even
  in a private channel (21.5a). At most 100 per
  conversation; the 101st is refused with `conflict`.
- A deleted message cannot be pinned, and deleting a pinned message
  unpins it.
- Pinning posts a `pinned` system notice. Unpinning posts nothing.
- The request that lists pins returns the messages, newest pin
  first.
- Sealed chats have no pins: nobody there is a manager.
- In a private channel, pinning shows a message to people who join
  later: they see the pin and can open the message on its own, without
  its reactions or its thread, though it is not in their history
  (PERMISSIONS.md section 4.4). Unpinning takes that away. Pinning a
  message older than some member's first join is written to the audit
  log (`message.pinned_for_newcomers`).

Stored: `pins`. Events: `pin.added`, `pin.removed`, and
`message.created` for the notice.

### 21.2 Bookmarks

A bookmark saves a message for one person.

- Anyone can bookmark any message they can read, including one in a
  sealed chat (only the identifier is stored).
- The list is newest first and leaves out messages the person can no
  longer read.
- Nobody else can see a person's bookmarks.

Stored: `bookmarks`. Events: `bookmark.added`, `bookmark.removed`,
to the person's own connections.

### 21.3 Sections

Sections are the headings named channels are grouped under in
the channel list. They are the server's default grouping: what a new
member sees until they arrange their own sidebar (21.4).

- Admins create, rename, reorder and delete them (`sections.name`,
  `position`). `position` is the default order of the sections only.
  Nobody orders the channels inside a section for anyone else (ruling
  R83): each person sorts them for themselves.
- A manager of a channel chooses which section it sits in
  (`conversations.section_id`), or none (decision F20); an admin may
  too, from the admin console.
- A sub-channel is listed under its parent and takes its parent's
  section for display; its own `section_id` is kept and ignored
  while it has a parent (section 21.7).
- Channels with no section are listed under a last heading the
  client supplies. Deleting a section moves its channels there.
- Ad-hoc channels and sealed chats have no section; they are listed
  under "Chats".

Stored: `sections`, `conversations.section_id`. Events:
`sections.changed`, `conversation.updated`.

### 21.4 Each person's own sidebar

Ruling R83. Each person arranges this server's conversations for
themselves, and the arrangement is kept on the server, so it follows
them to every device and both clients. Nobody arranges anyone
else's: not a channel's managers, not admins.

- **Own sections.** A person makes up to 50 sections of their own
  (names of 1 to 64 characters), renames, reorders and deletes them.
  Deleting one sends its conversations back to where the server
  lists them.
- **Placement.** Any conversation can be moved into any section: one
  of their own, any of the admin's, or "Channels" (ruling R84);
  otherwise it sits where the server lists it (its admin section,
  "Channels", Chats or Sealed). Chats and Sealed take only their own
  conversations. A sub-channel goes with its parent unless moved
  itself. A placement in an admin section that is deleted is dropped,
  and the conversation goes back where the server lists it.
- **Heading order.** Own sections and the admin's sections, and
  "Channels", in the person's order. The default: own sections in
  the order made, then the admin's in their order, then "Channels".
  Chats and Sealed are always last.
- **Sort, per heading.** Alphabetical (the default), last activity
  (the default for Chats and Sealed) or custom (the order the person
  dragged them into). Dragging a row switches its heading to custom.
  "Reset to alphabetical" puts one heading back to its default; "Reset
  my sidebar" puts everything back to the server's default and keeps
  the stars.
- **Stars.** A person can star any conversation they are in
  (`members.starred`). A starred conversation sits at the top of its
  own heading, stars in the order they were starred, which the person
  can change by dragging among them. Starring does not override
  `show` (CLIENTS.md 9.2).
- Linked servers' shared channels are conversations of this server
  like any other and are arranged the same way.

The whole layout is read in one call (`getSidebar`) and changed with
small changes (`changeSidebar`: make, rename, move or delete a
section, move a conversation, reorder, set a sort, reset, order the
stars), each naming the version it was made against: a change made
against an older version is refused with `conflict`, so two devices
cannot overwrite each other unseen. Leaving a conversation drops it
from the layout at the next change.

Stored: `sidebars`, `members.starred`. Events: `sidebar.changed`,
`membership.updated`.

### 21.5 Making a public channel private

A manager of a public channel can make it private. A private channel can never be made public or a knock
channel, and an ad-hoc channel is always private; those requests are
refused with `forbidden`.

In one transaction the server:

1. Sets `conversations.private` to true.
2. Deletes `mentions` rows in the channel for accounts that are not
   members.
3. Carries out every waiting purge request in the channel as an
   erasure (`outcome = erased`, `decided_by` null), since in a
   private channel the author's request would have been carried out
   at once.
4. Writes an audit row.

Members keep reading everything they could; anyone added from then
on reads only what is posted from their first join (PERMISSIONS.md
section 4.4).

From that moment people who are not members lose the channel:
reading, history, files and search all answer as if it did not
exist, and it leaves the channel browser. Their bookmarks of its
messages are kept and not shown. Nothing changes for members.
Waiting reports now belong to the channel's moderators and managers
alone, and an admin who is not a member has only the admin view of a
private channel.

Stored: `conversations.private`.

Events: `conversation.updated` to members; `message.purged` for
each request carried out. People who are not members receive
nothing, as always; their clients find the channel gone the next
time they ask.

### 21.5a What a newcomer to a private channel reads

A private channel promises its members that what they say there will
not be opened up (ruling R87). Someone who joins or is added reads
only what was posted from their first join on: messages, threads,
files, pins, search, reactions, polls and everything else about older
messages are not there for them, and the server answers as if they
did not exist. Someone who leaves (or is removed) and comes back within
three hours reads from when they first came, including what was
posted while they were away; coming back later, they read only from
the new join, as any newcomer. Admins and owners are no exception,
and someone added by override reads from that join too.
The people a channel starts with read all of it. Every arrival is
announced in the channel (section 13). Public, knock and ad-hoc
channels are unchanged: a newcomer reads the whole history. The full
rule is PERMISSIONS.md section 4.4.

Stored: `first_joins`, `members.visible_from`.

### 21.5b A private channel on a hub

A private channel can be shared through a hub, or moved onto one from
a direct link (ruling R94). The hub owns it, as it owns every channel
shared through it, and is trusted as its owner. The instances the hub
joins and approves are trusted to keep the channel's contents from
people who are not allowed to see them. Nothing is kept from the hub:
it makes and keeps the keys of every channel it owns and holds each
of them whole (every message as it stands, its files and custom
emoji, its members and the profiles and pictures of everyone who
wrote in it), so its operator can read the channel. Nothing of the channel depends on any instance staying: an
instance that leaves takes none of its history away, and an instance
that joins is given its history by the hub even with every other
instance switched off. What was said before the hub held the channel
the hub asks the instances for, and the hub it was handed over from,
until it has it: across restarts, and as soon as one that holds it
comes back. A private channel is still not encrypted against
the hub or its instances: for that, use a sealed chat. Being private
keeps out, as well as it can, the people nobody invited.

- **Who runs it.** The hub's record names its **managers**, who hold
  it as an owner would (which instances are in it, its name and
  description, its managers and moderators, closing it), and its
  **moderators**, who keep the peace (deleting messages for everyone,
  admitting and removing people). A manager may do all a moderator
  may. Sharing brings every manager of the channel along as a manager
  on the hub, and nothing else changes: members, levels, first joins,
  settings, pins and history stay as they were. A channel with groups
  in it is not shared until its groups are removed. On each instance
  the copy's levels follow the hub's lists: its accounts on the
  manager list are `manager`, those on the moderator list
  `moderator`, and anyone else at those levels is set to `member`.
  When the copy's last local manager leaves, nobody is promoted.
- **Never listed.** It stays private and unlisted for good: no
  directory, and nobody outside it sees it on any instance. Instances
  join only when a manager invites them, and a joining instance's
  copy starts with nobody of its own, not even the Owner who
  accepted.
- **Who is in it.** Nobody adds people to it on any instance: not a
  local manager, not an Admin, not an Owner, and there is no owner
  override (21.12). A moderator or manager,
  from any instance, admits someone directly (their own people, or
  people of other instances they have met in a shared conversation),
  or the person asks with the join code a moderator or manager gave
  them (the channel's stream identifier) and any moderator or manager
  approves or declines. A request is seen by moderators and managers
  on every instance, and those of the person's own instance are told
  of it at once. A guest is never admitted, and never asks. The arrival is announced once, naming who
  admitted them, and the person reads from their first join, as in
  21.5a.
- **Removing people.** A moderator or manager removes someone on
  every instance, in any channel a hub owns.

The full rules are PERMISSIONS.md section 14.1 and LINK-PROTOCOL.md
section 23a.

Stored: `stream_channels.managers`, `stream_channels.moderators`,
`stream_channels.private`, `knocks`. On the hub: the channel's record
and keys, and the whole channel in the hub's holder (`holder/` in its
data directory, `OPERATIONS.md`).

### 21.6 Locking a private channel

A moderator or manager of a named private channel can lock it, from
inside (a direct or group chat has neither, so it is never locked); the admin console does not lock. It cannot be
undone.
Refused: on a public channel
(`conflict`; make it private first), on a conversation shared with
other servers (`forbidden`), on an archived channel (`archived`).

In one transaction the server:

1. Sets `locked_at` and `locked_by`.
2. For every member who is in through a group, sets `direct_level`
   to their current `level` and `via_group` to false.
3. Deletes the channel's `channel_groups` rows.
4. Writes an audit row, unless the channel is ad-hoc.

So the people a group had brought in stay, at the levels they had;
the group brings in nobody new; and those people can now leave like
anyone else (decision F23).

From then on, with the refusal `locked`: nobody can be added, no
bot and no group can be added, nobody can join (not even from the
pointer of a moved thread), no webhook can be requested, and an
invite that names the channel skips it. People can still leave and
moderators and managers can still remove people. Someone who leaves
or is removed cannot return, except by override (21.12), the
one way into a locked channel. Everything else about the channel works as before:
posting, levels, pins, name and topic, retention, archiving. When
the last member leaves, the channel is deleted with its content.

Clients say plainly, before the moderator or manager confirms, that
a lock is for good.

Stored: `conversations.locked_at`, `locked_by`; `members`;
`channel_groups`.

Events: `conversation.updated`.

### 21.7 Sub-channels

A named channel may have a parent, another named channel
(`conversations.parent_id`).

- **One level only.** A sub-channel cannot have sub-channels, and a
  channel that has sub-channels cannot be given a parent. Ad-hoc
  channels and sealed chats are never parents or children.
- **Nothing is inherited.** A sub-channel has its own members,
  levels, public or private setting, lock, retention, slow mode,
  welcome text and everything else. Joining a parent does not join
  its sub-channels; clients offer the public ones to someone who
  joins. Archiving, locking or making a parent private does nothing
  to its sub-channels.
- **Who sets or clears the parent:** a manager of the sub-channel
  that is also a member of the parent, or an admin from the admin
  console.
  It can be given when the channel is created; the creator must then
  be a member of the parent, or an admin.
- **Display.** A sub-channel is listed under its parent and takes
  the parent's section. A reader who cannot see the parent (it is
  private and they are not in it) is not told it exists: the
  sub-channel reaches them with no `parent_id` and is listed as a
  top-level channel (decision F26).
- **A private sub-channel** is seen only by its members, and by
  admins in the admin view, like any named private channel. Being in
  the parent shows nothing of it.
- **If a parent is deleted** (its last member left a private one),
  its sub-channels become top-level and use their own section again.
- **Local.** The parent link is not sent to linked servers. A shared
  channel may be a parent or a sub-channel on each server
  separately.
- Channels made by moving a thread out (1.6) and ticket channels
  (7.4) start as sub-channels.

The request to set a parent checks, in order:

| Check                                                        | Refusal         |
|--------------------------------------------------------------|-----------------|
| The caller can see the parent                                | `not_found`     |
| The caller is a manager of the channel and a member of the parent, or is an admin using the admin console | `forbidden` |
| Both are named channels, and they are not the same channel   | `invalid_value` |
| The parent has no parent of its own                          | `conflict`      |
| The channel has no sub-channels                              | `conflict`      |
| The channel is not archived                                  | `archived`      |

Clearing the parent needs the same right over the sub-channel and no
membership of the parent. Naming an ad-hoc channel gives it no
parent; one can be set afterwards.

Stored: `conversations.parent_id`.

Events: `conversation.updated` in the sub-channel. Nothing is sent
in the parent; clients that list it fetch its sub-channels.

### 21.8 Channel names, creating and browsing

Channel names are not unique. Any number of channels may share a
name, anywhere, including under the same parent. A name is
free-form: 1 to 64 characters of any script, spaces and capitals
allowed, no control characters, no leading or trailing space, and
runs of spaces collapsed to one by the server. Clients sort and
match names without regard to case. `name_taken` is never returned
for a
channel name, so creating or renaming a channel can never reveal
that a private channel of that name exists.

- **Identity.** A channel is identified by its identifier in every
  request, event, link and stored reference. Nothing looks a channel
  up by name alone.
- **Creating.** The request gives a name, public or private, and
  optionally a topic, a section and a parent. The creator is its
  first manager. It is refused only for an invalid name
  (`invalid_value`), a role that may not create channels
  (`forbidden`), or a parent that fails the checks in 21.7 (a parent
  that is not a named channel is `invalid_value`; one that is itself
  a sub-channel is `conflict`).
- **Browsing.** The channel browser lists the public channels and
  the caller's own, each with its `parent_id`, section, creation
  time and member count (not its managers: the managers of a channel
  are read from its member list). Guests and bots see the channels
  they are in. A text filter on the name may return several channels
  of the same name.
- **Telling same-named channels apart** is the client's job,
  wherever a person must choose: search, the browser, the `#`
  picker, the quick switcher. Clients show the parent, the section,
  the creation date and the member count, and in the apps the
  server.

**Linking a channel in a message.** A channel is linked by its
identifier, never by its name.

1. In stored text a link is written `<#conversation-id>`, with the
   channel's UUID.
2. When a message is posted or edited, the server turns each such
   link that names a channel the author can see into a
   `conversation` entity covering the whole `<#…>`. One that names
   no channel, or a channel the author cannot see, stays as written
   and gets no entity; clients draw it as "unknown channel".
3. Clients draw an entity as the channel's current name, so a rename
   shows everywhere at once. A reader who cannot see the channel is
   shown "private channel" and is not given its name.
4. `#games` typed as plain characters is plain text. It is never
   resolved, by the server or by a client.
5. **In the composer**, typing `#` followed by characters that match
   a channel the person can see always opens a picker asking whether
   to link, listing the matches with the detail that tells them
   apart. Matching ignores case. Choosing one inserts the link,
   drawn as a chip showing the channel's current name. Dismissing
   leaves the characters as plain text.
6. Wherever the server writes plain text from a message (the search
   index, fallbacks, notification bodies), it writes the channel's
   name at that moment in place of the link, or "private channel"
   where the reader could not see it.
7. Bots, webhooks and scripts link a channel by writing
   `<#conversation-id>` in their text, under the same rule 2.
   Slack's `<#C…|name>` and Discord's `<#id>` in converted webhook
   payloads name channels of those services and become the plain
   text `#name`, or nothing if no name is given.

No request carries a hint about which channel a name means; there
is nothing to hint, since names are never resolved.

Stored: `conversations.name`, `parent_id`; `messages.entities`.

Events: `conversation.updated`.

### 21.9 Announcement channels

A named channel can be an **announcement channel** (ruling R66): a few
people post and everyone else reads. It is a setting of the channel
(`announcement`), chosen when the channel is made or set later by a
manager of the channel, and it can be turned off again. Nobody's
level changes either way.

In an announcement channel:

- **Who posts.** Its managers, the
  members a manager has allowed (`setMemberMayPost`, "May post here"
  in the member list), and a webhook its managers approved. A member
  must be at level `member` to be allowed: a `reader` posts nowhere.
  Everyone else's post, form, scheduled message, saved reply, call or
  upload is refused with `forbidden`.
- **Reactions** are open to every member, readers included, as in any
  channel: a reaction is one small event and is how a large audience
  answers. Polls and forms posted there can be answered by everyone.
- **Replies in threads** are off, so a thread holds only what the
  posters add. A manager can turn them on (`announcement_replies`):
  then members at level `member` or above reply in threads, and still
  do not post at the top level. Off is the default because a channel
  of thousands with open threads is a channel of thousands posting;
  the switch is there for the team that wants questions under each
  announcement.
- **The member list** is shown to its moderators and managers only,
  since moderators remove people and need to see who is in it.
  Everyone else is
  told how many members there are and not who: `listMembers` answers
  `forbidden`. Join and leave events are still in the conversation's
  log, so a client that kept every one of them could work out part of
  the list; the list itself, and search over it, are not offered.
- **No typing and no presence.** Nobody is shown typing, and no online
  count is shown (section 21.10).

A channel shared with other servers cannot be an announcement channel,
and an announcement channel cannot be shared (`conflict` both ways):
the other servers would not know the rule. This is for local channels
for now.

`default_level: reader` is still there and is a different thing: it
decides the level people join at. An announcement channel leaves
levels alone, so turning it off gives everyone back what they had.

Stored: `conversations.announcement`, `announcement_replies`;
`members.may_post`.

Events: `conversation.updated`; `membership.updated` to the member
whose `may_post` changed.

### 21.10 Large conversations

Two sizes, both server settings an admin can change, decide what a
big conversation does without (ruling R66):

| Setting | Default | Above it |
|---------|---------|----------|
| `channels.large_feature_limit` | 50 members | Nobody is shown typing. No presence of single members: clients show how many members are online. The conversation object says `large: true`. An announcement channel is always large in this sense, and shows no count either |
| `channels.large_channel_limit` | 1,000 members | The channel is **delivered lazily**: in full only to the connections that have it open; the others are told that it changed, at most once a second, and fetch the difference when they open it (`docs/EVENTS.md` section 4.1). A channel's managers can say `on` or `off` for their channel whatever its size (`lazy_delivery`) |

0 switches either off.

- **Typing.** `sendTyping` and the `typing` frame are accepted and go
  nowhere, so a client need not know the size; typing from another
  server of a shared conversation is dropped too.
- **Read positions.** Churr has never shown anyone where someone else
  has read to: `read_position.moved` goes to the reader's own devices
  and nobody else, in a conversation of any size. That is unchanged,
  and a person's own position still follows them across devices.
- **Presence.** Presence by interest still gives live presence in
  small conversations and chats between two. In a large one a client
  does not ask for its members' presence; it shows the online count
  from `presence.counts`, which the server counts at most every five
  seconds. A profile that is open still shows that person's presence.
- **What notifies is unchanged.** Lazy delivery decides which frames a
  connection is sent, not who is notified: the one rule of section 3.7
  decides, a message that notifies its reader is sent to them whole at
  once whatever they have open, and phones are woken as before.

### 21.11 Last seen

A profile says roughly when the person was last here, never the time
(ruling R66): `online` while others are shown them online, `recently`
within three days (and while they are connected but away),
`within_week` within seven, `long_ago` otherwise. It is on the account
fetched by itself (`getAccount`), for a viewer who is shown the
person's presence, and for nobody about a bot or an account of
another server. The server works it out from when the account's
sessions were last used, which it notes at most once a minute, and
sends the answer only: no exact time of another account leaves it.
Signing out everywhere therefore reads as `long_ago`.

A person can hide it (`hide_last_seen` in their preferences): the
field is then absent for everyone. Their presence is still shown as
section 3.1 says; "appear away" is the switch for that.

### 21.12 Moderators, managers and owners

Rulings R97 and R99. The roles are few on purpose; a channel that
needs finer control installs a bot. The full rules are PERMISSIONS.md
sections 2.4, 6, 6.3, 7 and 9.1.

- **Levels in a channel**: `reader` < `member` < `moderator` <
  `manager`. Its managers set them (`setMemberLevel`, `/op` and
  `/deop` for manager and member, `/mod` and `/unmod` for moderator
  and member); a moderator sets none. A guest
  account is a guest in every channel it is in: `reader` or `member`,
  never `moderator` or `manager` (`forbidden`), and it is never in a
  channel shared with other servers. An account of another server is
  never a moderator or a manager here. A bot may be either.
- **Moderators keep the peace**: they answer knocks, add people at
  the channel's default level, remove members, ban and lift bans, see
  the bans, delete others' messages and remove any link preview, time
  people out and lift time-outs, set slow mode and are exempt from it,
  lock a private channel, handle reports, and see an announcement
  channel's members. A moderator does not remove, ban or time out
  another moderator or a manager.
- **Managers hold the channel**: everything a moderator does, and
  the topic, name, welcome text, section, parent, pins, levels, the
  default level, bots, groups, announcement settings, lazy delivery,
  broad mentions, reference patterns, shorter retention, archiving,
  making it private, moving others' threads, closing any form,
  reading `collect` answers, answerer groups and recurring forms.
- **Admins are not managers.** An Admin or Owner has no power in a
  channel by their role: they are its moderators or managers only
  when made one, like anyone, and are removed, banned and timed out
  there like anyone, except that nobody acts on an Owner. The
  instance's own powers stay theirs: the admin console, exiles,
  server-wide time-outs, purges, purge requests, webhooks, and a
  public channel's reports. The admin console shows each named
  channel's name, members and managers and sets its section, parent
  and retention; it does not archive, lock or lift bans.
- **Last manager.** When a channel's last manager leaves or is
  removed, its longest-standing member who is a person of this
  instance and not a guest becomes manager. In a channel a hub owns
  nobody is promoted: the hub's lists decide.
- **Several owners.** An owner makes any other active person's account
  of the instance an owner (`setAccountRole`, role `owner`; audited as
  `owner.made`). Nobody demotes an owner, another owner included
  (`forbidden`). An owner steps down to admin, member or guest only
  while another active owner remains (`conflict` for the last;
  `owner.stepped_down`). Nobody removes, exiles or times out an
  owner, and an owner makes a recovery link only for themselves.
- **An Owner or an Admin appoints or dismisses a manager** of any
  named channel without being in it (`appointManager`,
  `dismissManager`; ruling R104), for a channel that has lost its managers
  or has a rogue one. In a public channel the person may be anyone of
  the instance who may join it, and joins as part of it; in a knock
  channel they are added as an add is. In a private or locked channel
  they must be a member already, or override is asked for and they
  are added that way first. Guests, bots and accounts of other
  servers are not appointed; a channel a hub owns is refused
  (`conflict`). Audited as `channel.manager_appointed`. A dismissed
  manager stays a member; the channel is posted a `manager_dismissed`
  notice naming who did it, which nobody can turn off, audited as
  `channel.manager_dismissed`. An Admin dismisses no Owner and no other
  Admin; a manager through a group and the last manager are refused
  (`conflict`).
- **Override** (`ownerOverrideAdd`), by an Owner or an Admin, puts an
  active person of this instance into a private or knock channel,
  locked or not, without the one doing it being in it, at the
  channel's default level. It posts an `owner_override` system notice
  naming the person and who did it, which every member is shown and clients draw in the pink of
  "wants attention" (STYLE.md); nobody can turn it off. It is audited
  as `channel.owner_override`. The person reads from that join, as any
  newcomer (21.5a), and whoever did it gains nothing: they still cannot
  read the channel. Refused: a public channel (`conflict`: anyone may
  join), a channel shared with other servers (`forbidden`), a channel
  a hub owns (`conflict`), a bot, a guest into a shared channel,
  someone banned from the channel (`forbidden`), someone already in
  it (`already_exists`), an archived channel (`archived`).
- **The operator** of the machine settles owners when the instance
  cannot: `churr owner grant <username>` makes a person's account an
  owner and `churr owner revoke <username>` makes an owner an admin,
  refusing the last owner. The server is stopped while it runs, and
  each change is in the audit log with `actor_kind` `cli`
  (OPERATIONS.md).
- **Guests and shared channels.** A guest is never added, invited,
  admitted or brought by a group into a channel shared with other
  servers, and sharing a channel that has guests in it is refused
  (`conflict`: remove its guests first).

Stored: `members.level`, `channel_groups.level`, `accounts.role`,
`audit_log`.

Events: `member.level_changed`, `member.joined`, `message.created`
for the notice, `account.updated`.

### 21.13 Notices and access alerts

**Notices.** Something done that concerns one account and is not a
message anywhere is kept for it as a notice, until the account is
removed: it hears of it when it next looks, connected or not. Only its
holder reads its notices (`listNotices`, newest first, with how many
are unseen); `markNoticesSeen` marks them all seen and they stay
listed. A new one reaches the holder's connections as
`notice.created`. The kinds:

- `recovery_link_used`: a recovery link was used to sign in to this
  account, naming who made it (none for `churr recovery`). The web
  chat also shows a passing message at the moment it happens; the
  notice is what stays.
- To every Owner: `owner_made` and `owner_stepped_down`,
  `export_started` and `export_downloaded`, and `override_used`.

The web chat shows "Notices" in the sidebar's top links once there is
one, with the unseen count, and lists them at `/chat/notices`; the app
has the same entry and screen (CLIENTS.md 9.30). Opening the list marks
them seen.

**Access alerts.** Some acts of an Owner or an Admin let them, or
whoever they hand something to, read a conversation that is not public
as somebody else. Each posts an alert in every conversation concerned
(a named private or knock channel, a direct or group chat; never a
public channel, nor a sealed chat, which the instance cannot read
whoever holds the account), in the same transaction as the act. Like
an owner override it is drawn as wanting attention, nobody can turn it
off, and it names who did it:

- `bot_token_made`: a new token for a bot, in the bot's conversations,
  naming who made it. Whoever holds the token reads there as the bot.
- `account_recovered`: a recovery link was used on the account, naming
  who made the link.
- `account_claimed`: an imported account was claimed with a claim
  link, naming who made the link.
- `group_access`: someone arrived in a private channel through a group
  an Owner or Admin put them into from the console, naming the group
  and who did it, the Owner or Admin themself included. It takes the
  place of the ordinary arrival line.
- `channel_shared`: a private channel was shared with another instance
  or a hub, or an instance was added to it, naming the instances.
  Only a manager of the channel may do either (ruling R96).

Each act is also in the audit log (`token.created`,
`recovery.link_used` with `link_made_by`, `account.claimed` with
`link_made_by`, `group.member_added`, `link.channel_shared`,
`link.server_offered`).

## 22. Custom emoji

- **Adding.** An admin, or any `member` if `emoji.members_may_add`
  is on, uploads an image with a name. The name follows the rule in
  `CONVENTIONS.md` and must not already be a custom emoji
  (`name_taken`) or a standard short code (`name_taken`).
- **The image** must be PNG, JPEG, GIF or WebP by content, at most
  256 KB and 256 pixels on each side (`too_large`). It is stored
  as a `files` row with purpose `emoji` in the library conversation
  and served like any file, to any signed-in account.
- **Using.** `:name:` in text becomes an `emoji` entity carrying the
  emoji's identifier. A reaction stores `:name:`.
- **Removing.** An admin, or the member who added it. The `emoji`
  row and its file are deleted. Messages keep their text and draw
  `:name:` as written; reactions that used it are drawn as the
  written name.
- There is no rename. Remove it and add it again.
- Names are unique among the server's own emoji. An emoji that
  arrived with a shared channel has `server_id` set and its own
  name space, so it never clashes with a local one. In text and in
  reactions it is written `:name@server.name:`. It is drawn in the
  messages that came with it, anyone may add to a reaction that
  uses it, and it is not offered in the local picker.

Stored: `emoji`, `files`.

Events: `emoji.added`, `emoji.removed`, to everyone.

## 23. Link previews

Off until an admin turns `previews.enabled` on. Never in sealed
chats.

### 23.1 When a preview is fetched

After a message is posted in a channel, the server takes its links:
bare addresses and Markdown links, `http` and `https` only, in
order, without repeats, and leaving out links to the server's own
address. For the first three it looks for a preview, outside the
posting transaction, so posting never waits for a fetch.

On an edit, only links that were not in the earlier text are looked
up, and preview cards whose link has gone are dropped. So a preview
someone removed does not come back.

### 23.2 The cache

The address is normalised (scheme and host lower-cased, fragment
dropped) and hashed. If `preview_cache` has a row for it that is
less than 24 hours old (one hour if its `card` is null), that is the
answer. Otherwise the server fetches and writes the row. Two
requests for the same address at once share one fetch.

### 23.3 Fetch rules

| Rule                | Value                                              |
|---------------------|----------------------------------------------------|
| Schemes and ports   | `http` on 80, `https` on 443. Nothing else.        |
| Addresses           | The host is resolved and **every** address it resolves to must be public. Refused: loopback, private ranges, link-local, carrier-grade NAT, unique-local and site-local IPv6, multicast, the unspecified address, IPv4-mapped forms of any of these, and the server's own addresses. |
| Connection          | Made to the address that was checked, not resolved again, so a name cannot change between the check and the use. |
| Redirects           | At most 3. Each target goes through every rule again. |
| Time                | 5 seconds for the page in all; 5 seconds for the image. |
| Size                | The first 1 MB of the page is read and the rest ignored. Image at most 5 MB. |
| Content             | The page must be `text/html`. The image must decode as PNG, JPEG, GIF or WebP. |
| What is sent        | A `GET` with a fixed `User-Agent` that names Churr and not the server. No cookies, no credentials, no referrer. |
| TLS                 | Certificates are verified.                         |
| Load                | At most 4 fetches at once for the server and one per host. Fetches are rate-limited per posting account. |

A failure of any rule gives a null card. The reason is logged for
the operator and not shown to members.

### 23.4 The card

From the page the server reads, in order of preference: Open Graph
tags (`og:title`, `og:description`, `og:image`, `og:site_name`),
then `<title>` and the `description` meta tag. It makes a card:
`title` (cut to 200 characters), `text` (the description, cut to
300, as plain text), `url`, `footer` (the site name or the host) and
`color` `grey`. A page with no title gives no card.

The image is fetched under the same rules, re-encoded to at most 720
pixels on the long edge with metadata removed, and stored as a
`files` row with purpose `card` in the library conversation, owned
by the cache row.

### 23.5 Adding it to the message

For each link with a card, the server appends the card to
`messages.cards` with `preview_url` set to the address it previews,
which is what tells it from a card a bot or webhook posted, and
copies the cached image to a new
`files` row of purpose `card` on that message. It stops at three
previews, and at five cards in all. This is not an edit: `version`,
`edited_at` and history are untouched.

The message's author or a moderator or manager of the channel can
remove one preview card, named by its position in `cards`; its image file is
deleted.

Stored: `preview_cache`, `messages.cards`, `files`.

Events: `preview.added`, `preview.removed`.

### 23.6 Theme cards

A link to one of this server's own themes, `{origin}/themes/{id}`
(the server's own origin, the page path `/themes/`, a theme id; the
web pages serve that page), is not fetched. The server reads the
theme from its own database and attaches a theme card by the same
path as a preview (23.5): `preview_url` and `url` the link, `title`
the theme's name, and `theme` a copy of the theme (`id`, `name`,
`kind`, `colours`, `author_id`) from which clients draw its swatches
and offer "try" and "add" (`addTheme`). It counts among the three
previews, arrives with `preview.added` and is removed with
`removeLinkPreview` by the same people. It needs no fetch, so it is
attached whether or not link previews are switched on; never in a
sealed chat. A link to a theme that does not exist, or to another
server's theme page, has no card. The card is a copy: it stays as it
was if the theme later changes or is deleted.

Nobody else can make a theme card: a posted or edited card with
`theme` or `preview_url` is refused with `invalid_value`, except that
an edit may send back a card the message already carries.

An attached `.churrtheme` file (a theme exported to JSON, for sharing
between servers) is an ordinary file to the server; clients draw it
as a theme card.

## 24. Search

Full-text search over the channel messages the asker may read
(PERMISSIONS section 4.2). Deleted messages, history, sealed chats
and file contents are never found.

### 24.1 The request

The search request takes the words to find and five filters, each
optional:

| Parameter  | Meaning                                              |
|------------|------------------------------------------------------|
| `q`        | The words, in the grammar below                      |
| `in`       | Only this conversation, by identifier                |
| `from`     | Only this author, by identifier                      |
| `since`    | Only messages posted at or after this time           |
| `until`    | Only messages posted before this time                |
| `has_file` | Only messages with at least one file                 |

`since` and `until` are times, as everywhere else in the API. A
request needs `q` or at least one filter.

    q     = term { term }
    term  = word | word "*" | '"' phrase '"' | "-" word

| In `q`           | Meaning                                              |
|------------------|------------------------------------------------------|
| `deploy friday`  | Messages with both words, anywhere, in any order     |
| `"deploy plan"`  | The words next to each other in that order           |
| `depl*`          | Words starting with `depl` (at least two letters)    |
| `-staging`       | Not containing `staging`                             |

- Matching ignores case. Words are not stemmed.
- Anything else in `q`, including characters that mean something to
  the database's own query language, is treated as text to find.
- A filter that names a conversation the asker cannot read, or an
  unknown account, gives an empty result, not an error, so search
  cannot be used to test whether a private channel exists.

**Filters typed in the search box** are the client's business, like
built-in slash commands (decision F24). Every client reads the same
five and turns them into the parameters above before sending:

| Typed               | Becomes                                           |
|---------------------|---------------------------------------------------|
| `in:` then a channel picked from the picker that `#` opens | `in`, the identifier of that channel. The client shows the detail that tells same-named channels apart (section 21.8). |
| `from:@sam`, `from:me` | `from`, that account's identifier              |
| `since:2026-11-01`  | `since`, the start of that day in the person's time zone |
| `until:2026-11-08`  | `until`, the start of that day in the person's time zone |
| `has:file`          | `has_file`                                        |

A typed filter the client cannot resolve (an unknown channel or
person) is left in `q` as words.

### 24.2 What comes back

Results are newest first and paged with a cursor. Each is the
message, as the asker would see it anywhere else, and a short
plain-text snippet around the matched words. The conversation is
not included: the client knows it from the message's
`conversation_id` (and looks up its `parent_id` to tell channels of
the same name apart). `q` may be empty when a filter is given. A
result
that is a reply opens its thread. Channels hidden from the sidebar
and archived
channels are searched like any others. Messages from accounts the
asker has blocked are returned and drawn collapsed.

The indexed text is the message's text and its fallback, so the
title and options of a poll and the text of cards are found. A
channel link is indexed as the channel's name at the time, so
searching for a channel's name finds messages that link it.

Stored: the search index (SCHEMA section 13). Events: none.

## 25. The Slack importer

`churr import slack <export.zip>` is run by the operator on the
server's machine, with the server stopped (decision F21). It writes
straight to the database through `store`.

### 25.1 Before it starts

- It reads the export and prints what it found: channels, people,
  messages, files.
- If the server has a retention period it prints how many messages
  are already older than it and would be deleted by the next janitor
  run, and asks the operator to confirm.
- It keeps a record of what it has made, Slack identifier to Churr
  identifier, in a file in the data directory. Running it again on
  the same export continues from there and makes nothing twice.

### 25.2 What maps to what

| In the Slack export                 | In Churr                                |
|-------------------------------------|-----------------------------------------|
| A user                              | An unclaimed account (`claimed_at` null) with role `member`. Slack owners and admins are not made admins. |
| A restricted or single-channel guest | An unclaimed account with role `guest` |
| A bot user, or a bot that only appears as a message author | An account with role `bot` and no token |
| A deleted user                      | A removed account, so its messages have an author |
| The user's handle                   | `username`, lower-cased and cut to the username rule; a clash gets a number on the end |
| Real name or display name, title, time zone | `display_name`, `title`, `time_zone` |
| Profile picture                     | Downloaded and stored as the avatar     |
| Email address                       | A keyed hash in `import_claims`, nothing else |
| A public channel                    | A public named channel with the same name, no parent. Names need not be unique, so nothing is renamed. If Slack's general channel and an empty `#general` both exist (a fresh server), the empty one is used. |
| A private channel, if in the export | A private named channel                 |
| A group direct message, if in the export | An ad-hoc channel with those members |
| A direct message, if in the export  | An ad-hoc channel with those two members; a direct message to oneself becomes the one-person channel |
| Channel topic; purpose              | `topic`; the purpose is used if there is no topic |
| Channel creator; members            | `created_by` and a `manager`; `members` rows at level `member` |
| An archived channel                 | `archived_at` set                       |
| A message                           | A `messages` row with the original time as `created_at`, `via_kind = import`, `via_name = Slack` |
| A message with `thread_ts` of another message | A reply to that message      |
| A thread reply "also sent to channel" | The reply only                        |
| An edited message                   | `edited_at` set; no earlier versions exist to import |
| Reactions                           | `reactions` rows: standard emoji by Slack's short-code table, custom ones as `:name:`. A reaction by an unknown user is dropped. |
| Pinned items                        | `pins` rows                             |
| Files                               | Downloaded from the links in the export while they still work, and stored as `files` on their message. Imported videos have no poster image. A file that cannot be fetched leaves a line "File not available: name" in the message. |
| Attachments (the older rich format) | Cards: title, link, text, fields, footer, time, colour mapped to the nearest. Images at other sites are not fetched; the address goes in the footer. |
| Layout blocks                       | The message's plain `text` field        |
| Custom emoji, if the export lists them | `emoji` rows with downloaded images; aliases are skipped |
| Join, leave, topic, rename and other notice messages | Not imported |
| User groups, reminders, bookmarks, canvases, huddles, workflows, apps | Not imported |

### 25.3 Text

Slack's markup is rewritten as Churr Markdown:

| Slack                       | Churr                              |
|-----------------------------|------------------------------------|
| `*bold*`                    | `**bold**`                         |
| `_italic_`                  | `*italic*`                         |
| `~strike~`                  | `~~strike~~`                       |
| `<@U123>`                   | `@username`                        |
| `<#C123\|name>`             | `<#conversation-id>` of the imported channel if that Slack channel was imported; otherwise the plain text `#name` |
| `<!channel>`, `<!everyone>` | `@channel`                         |
| `<!here>`                   | `@here`                            |
| `<https://x\|label>`        | `[label](https://x)`               |
| `<https://x>`               | `https://x`                        |
| `&amp;` `&lt;` `&gt;`       | `&` `<` `>`                        |
| Code, quotes, `:emoji:`     | Unchanged                          |

Entities are worked out as for any message, so mentions draw
correctly.

### 25.4 What it does not do

- It writes no `mentions` rows and sends no notifications.
- It writes no `events` rows. Clients load imported history through
  the ordinary requests.
- It does not run the word filter or slow mode.
- Every member's `read_seq` is set to the channel's latest, so
  nobody starts with years of unread messages.
- Each conversation's messages are numbered in order of their Slack
  timestamps, and `next_seq` is set past the last.
- The search index is filled as the messages are written.

### 25.5 Claiming

When it finishes it prints one claim link per imported person: an
`invites` row with `claims_account_id` set, one use, expiring 30
days after the import, revocable by an admin, who can also make a
fresh one later. Opening
the link and making a passkey (or taking an access key) sets `claimed_at`, deletes the
`import_claims` row, and puts the account in `@everyone` and so in
the starting channels.

With company sign-in on, an unclaimed account is claimed without a
link when someone signs in through a provider and the keyed hash of
the provider's email address matches an `import_claims` row. The
address is not stored.

Stored: nearly every table of sections 3, 5 and 6 of SCHEMA.
Events: none during the import. An audit row with `actor_kind`
`cli` is written at the end.

## 26. First-run setup

1. **First start.** The server creates the data directory layout,
   the key file and the server's signing and encryption keys, and
   runs the migrations. It then creates what every server has: the
   two built-in groups and the library conversation.
2. **Setup link.** If no owner exists, it makes a random token,
   stores its hash in the setting `setup.token_hash`, and prints a
   one-time setup link to its log and standard output. The link
   works until it is used, across restarts; a later start says the
   earlier link still works, and `churr setup-link` replaces a lost
   one. Until setup
   is done every other page and API request answers `unavailable`.
3. **The first owner's account.** Opening the link shows the setup page:
   a name for the community, and the owner's username and display
   name. The owner makes a passkey; the page asks the passkey for
   the PRF feature. An owner who cannot use a passkey takes an access
   key instead (`AUTH.md` section 5.6): made in the page, shown once
   and confirmed by typing its last group. The account is created with
   role `owner`. With a passkey, eight recovery codes are shown once.
4. **Outside connections.** The page shows the table of connections
   the server can make, each with its switch and its default:
   certificate renewal (on when not behind a proxy; a start-up flag,
   shown for information), push relay (on), link previews (off),
   object storage (as configured at start, shown for information),
   UnifiedPush (off), identity provider (off; set up later in the
   admin console), links to other servers (off). The owner must
   confirm this page to go on.
5. **Joining.** Registration mode (`registration.mode`, `invite` by
   default) and whether members may invite
   (`invites.members_may_invite`, off by default).
6. **Finish.** In one transaction the server writes the settings,
   creates `#general` as a public channel with the owner as its
   creator and manager, makes it the one default channel
   (`channels.default`, section 14), deletes `setup.token_hash`, sets `setup.done_at`, and writes the
   first audit rows. The owner is signed in.
7. **First invite.** The last page offers to make an invite link and
   points at the admin console and the apps.

If the owner closes the page after step 3 and before step 6, the
account exists and signing in returns to step 4. Setup counts as
done only when `setup.done_at` is set.

A locked-out owner uses `churr recovery <username>`, which prints a
recovery link; it does not go through setup again. Further owners are
made by an owner, or by the operator with `churr owner grant`
(21.12).

Stored: `settings`, `accounts`, `passkeys` or `access_keys`, `recovery_codes`,
`groups`, `conversations`, `channel_groups`, `members`,
`audit_log`.

Events: none; nobody is connected yet.

## 27. Events named in this document

The names are those of `EVENTS.md`, which defines their fields and
is right where the two differ. "Seq" means the event belongs to a
conversation, takes a `seq` and is kept in the event log.

| Event                    | Seq | Used here for                            |
|--------------------------|-----|------------------------------------------|
| `message.created`        | Y   | Every new message. Each recipient's copy says whether it notifies, only counts for the badge, or neither (3.7). |
| `message.edited`         | Y   | Edits and quick corrections              |
| `message.deleted`        | Y   | Deletes                                  |
| `message.purged`         | Y   | A purge carried out, and history hidden by an author's request |
| `message.history_restored` | Y | An admin chose "show again"              |
| `preview.added`, `preview.removed` | Y | Link previews                    |
| `reaction.added`, `reaction.removed` | Y | Reactions                      |
| `task.changed`           | Y   | Checklist ticks                          |
| `form.answered`, `form.answer_withdrawn` | Y | Answers; for the `bot` action, to the bot only and not stored |
| `form.option_added`, `form.closed` | Y | Forms                            |
| `thread.moved`           | Y   | Thread to channel, in the old channel    |
| `member.joined`, `member.left`, `member.level_changed` | Y | Membership |
| `channel_group.added`, `channel_group.changed`, `channel_group.removed` | Y | A group in a channel |
| `conversation.updated`, `conversation.archived`, `conversation.unarchived` | Y | Channel settings |
| `pin.added`, `pin.removed` | Y | Pins                                     |
| `badge.changed`, `read_position.moved` | – | Counts going up; positions and counts going down |
| `membership.updated`     | –   | Mode, mute, show, star, thread view      |
| `thread_state.changed`   | –   | Following, ignored, neither, thread read position |
| `bookmark.added`, `bookmark.removed`, `block.added`, `block.removed` | – | Own connections |
| `scheduled.created`, `scheduled.updated`, `scheduled.cancelled`, `scheduled.fired` | – | Scheduled items; `scheduled.fired` carries the error when a run failed |
| `interaction.responded`  | –   | A bot's dialog shown to one person       |
| `command.invoked`        | –   | To the bot                               |
| `presence.changed`, `status.changed`, `availability.changed`, `dnd.changed` | – | Section 3 |
| `account.updated`, `account.removed`, `me.updated` | – | Profiles, removal, own preferences |
| `group.created`, `group.updated`, `group.deleted`, `group.member_added`, `group.member_removed` | – | Groups |
| `emoji.added`, `emoji.removed` | – | Custom emoji                         |
| `theme.published`, `theme.unpublished` | – | The theme library (29)       |
| `sections.changed`       | –   | Sections                                 |
| `sidebar.changed`        | –   | A person's own sidebar (21.4), own connections |
| `reference_patterns.changed` | – | Reference patterns (9.6): the server's to every local account, a channel's to its members |
| `server.updated`         | –   | Settings that clients need, the default themes (29) |
| `timeout.started`, `timeout.lifted` | – | Time-outs                       |
| `channel_ban.created`, `channel_ban.lifted` | – | Channel bans (19.1), to the one banned and the managers |
| `report.created`, `report.handled` | – | To the handlers                  |
| `purge_request.created`, `purge_request.decided` | – | To admins          |
| `approval.created`, `approval.decided` | – | The approvals queue (31): to those who may decide that kind, and the decision to the one who asked |
| `approvals.changed`      | –   | The flag "may approve requests" given or taken away, to its account (31) |

Things in this document that have **no** event in `EVENTS.md`, and
so are fetched when needed: status presets, saved replies and custom
commands, one's own themes, the word-filter list, and retention runs (clients drop
messages older than the period by themselves).

## 28. Decisions recorded here

| No. | Gap                                              | Choice                                   |
|-----|--------------------------------------------------|------------------------------------------|
| F1  | Which mentions in a reply make someone follow the thread | Mentions by name only.           |
| F2  | How a thread's read position relates to the conversation's | A thread with a state row has its own; others use the conversation's. Reading in the flat view moves both. |
| F3  | A mention of someone not in a public channel     | Notifies once, no badge.                 |
| F4  | An integration and a person both setting availability | Last write wins; an integration clears only what it set. |
| F5  | How "clear the pause from the current meeting" (ruling R37) is stored | `prefs.meeting_pause_skip_until`, removed when availability next changes. |
| F6  | How a client knows to make a sound               | The server says so on each recipient's copy of `message.created`. |
| F7  | Who a starting call rings                        | Members at `member` or above with the conversation on Any activity. |
| F8  | (Replaced by ruling R4: notify anyway always appears to succeed.) | Section 3.8.                 |
| F9  | A channel override longer than a lowered server period | Stands until an admin clears it.   |
| F10 | Changing an answer on a `thread` or `channel` form | Not possible.                          |
| F11 | The `roles` preset and leaving a group           | Buttons join or leave, as the format says; there is no toggle. |
| F12 | How "remind me about this message" shows the message | As a quote on the reminder.          |
| F13 | A cap on scheduled items                         | 200 waiting per account.                 |
| F14 | Send later in sealed chats                       | Not available.                           |
| F15 | Text after a saved reply's name                  | Refused.                                 |
| F16 | Who parses a built-in slash command              | The client, by the grammar fixed here. The server runs only saved replies, custom commands and bot commands. |
| F17 | Built-in and reserved command names              | The table of 12.3 (rulings R67, R68), with `help`, `later` and `status` held back. |
| F18 | Join notices for groups                          | One per group added; none for later changes. |
| F19 | Limits on a time-out                             | 60 seconds to 28 days.                   |
| F20 | Who files a channel under a section              | The channel's managers.                  |
| F21 | Whether the importer runs against a live server  | No; the server is stopped.               |
| F22 | (Confirmed as ruling R20.)                       | Section 1.6.                             |
| F23 | What locking does to people who are in through a group | They become direct members and the groups are detached. |
| F24 | Who reads filters typed in the search box        | The client, which sends them as parameters. |
| F25 | Purge requests waiting when a channel goes private | Carried out as erasures.               |
| F26 | A sub-channel whose parent the reader cannot see | It is returned with no `parent_id` and listed as top-level. |
| F27 | Whether a theme card waits for link previews to be on | No: it needs no fetch (23.6). |
| F28 | Adding a theme that is not published             | Allowed by its link, as R61 shows "add" on every theme card. |
| F29 | What an admin's removal does to a library theme  | Unpublishes it; the owner keeps it and may publish it again. |
| F30 | What a server default holds                      | A copy of the chosen theme, so it stays if the theme changes or goes. |

## 29. Themes

Ruling R61. A theme is a named set of base colours for one kind, dark
or light: `colours` holds exactly `bg`, `panel`, `raise`, `ink`,
`dim`, `lav`, `cyan`, `pink` and `yel`, each `#rrggbb`. Clients derive
every other colour by the rules of `STYLE.md` ("Themes"); the server
derives them the same way to check the theme, and refuses with
`invalid_value` (`colours`) a theme with a text pairing below 4.5:1 or
a control-edge pairing below 3:1, naming the first failing pairing
and its ratio cut to two decimals ("dim on raise is 4.06:1; text
needs at least 4.5:1."). A dark theme whose ground is not darker than
its ink, or a light one the other way round, is refused on `kind`.
The name is 1 to 64 characters.

- **Own themes.** `listMyThemes`, `createTheme`, `updateTheme`,
  `deleteTheme`. At most 50 per account (`conflict`). Members,
  admins, owners and guests; bots own none.
- **Links.** A theme's link is `{origin}/themes/{id}`. `getTheme`
  answers anyone signed in who has the id, published or not.
- **The library.** `publishTheme` and `unpublishTheme` (members and
  above, not guests), `listThemeLibrary` (newest first, `q` matches
  the name), `addTheme` (a copy the caller owns, unpublished, from
  the library or by link). `theme.published` and `theme.unpublished`
  go to every connection. An admin's `removeLibraryTheme`
  unpublishes a theme and is audited as `theme.removed`.
- **Defaults.** `setDefaultTheme` sets or clears the default dark or
  light theme (settings `themes.default_dark`, `themes.default_light`,
  each a copy of the theme). They are in `getServer` as
  `default_themes`, before sign-in too, and a change sends
  `server.updated`. Audited as `theme.default_set` and
  `theme.default_cleared`.
- **Cards.** Section 23.6.
- Which theme a device uses is the device's own preference, not
  stored on the server.

Stored: `themes`, `settings`.

## 30. The lock

Ruling R75. A person at a shared or unattended computer or phone locks
Churr there, so someone else cannot read their chats or type as them,
without signing out (which would throw away the device's sealed-chat
set-up).

- **Ways.** `/lock` (protected: no community command may take the
  name), "Lock" in the account menu, Mod+Shift+L, and, per device and
  off by default, an automatic lock after 1, 5, 15 or 60 minutes
  without use, and on phones a lock when the app goes to the
  background (`CLIENTS.md` section 9.27).
- **What it does.** The session is locked on the server
  (`sessions.locked_at`): until a passkey assertion with user
  verification, or the account's access key, unlocks it, the server
  answers it only to unlock or sign out, closes its live connection and
  refuses a new one, and its phone shows only "You have a new
  notification" for a wake-up. Each
  session locks on its own; locking one does not lock the person's
  other devices. Every tab of one browser shares its session and
  locks with it (`AUTH.md` section 10.6).
- **What the person sees.** The whole interface is replaced by the
  lock screen: the product mark, "Locked" and one Unlock button (and
  the access key's field, for an account that has one), over a blurred
  stand-in of the layout. The content itself is gone, not
  blurred: nothing of it is in the page, the widget tree or memory.
- **Calls.** Locking, however it happens, ends this device's part in a
  call; the command, the menu and the key ask first, the automatic
  lock does not.
- **Sealed chats.** Locking makes the device a locked sealed-chat
  device; unlocking brings it back in the same passkey touch or access
  key, and
  nothing sealed is lost (`AUTH.md` section 10.6, `SEALED.md` section
  13.3).
- **After unlocking** the client reconnects and catches up as after
  any reconnection. A session revoked or ended while locked is simply
  signed out.

Stored: `sessions.locked_at`; the device's own settings stay on the
device.

## 31. Approvals

The instance keeps one queue of requests that wait for someone to
approve them. Each kind of request is an entry in a registry
(`internal/approvals`) that says who may decide it and what approving
and declining do; adding a kind is adding an entry with those two.

**The kinds**

| Kind | Waits when | Approving | Declining | Decided by |
|------|------------|-----------|-----------|------------|
| `registration` | Registration is `open`, the setting `registration.approval` is on, and the person registers without an invite | The account becomes an active member, in `@everyone` and its channels like anyone who registers; nothing is posted about it | The account stays as it is, signing in to nothing; its username stays taken | Owners, admins, and holders of the flag |
| `channel` | The setting `channels.creation_approval` is on and a member who may not approve requests makes a channel | The channel is made as asked, in the requester's name, with them as its first manager | Nothing is made | Owners, admins, and holders of the flag |
| `webhook` | A webhook is asked for (`BOTS.md` section 9) | `approveWebhook` | `declineWebhook` | Admins, as in the console (the channel's managers decide it in the channel when the setting hands it to them) |

Both settings are off by default. Requests an invite was made for go
straight in: an invite is someone already vouching. A company
sign-in's registration is not held: the company's sign-in has vouched
for the person.

What stays where it is, and why: a purge request has three outcomes
(show again, keep hidden, erase), not two, and stays in the admin
console with the admins; a knock and a request to join a channel on a
hub are the channel's, and its moderators and managers answer them in
the channel (12.5); a chat request is the person's own (20.2).

**The flag.** "May approve requests" is an instance-level flag on an
account, not a role (ruling R97): an owner or an admin gives it to an
active owner, admin or member, and takes it away, from the people list
of the admin console (`setMayApprove`). It is written to the audit log
and the account is told (`approvals.changed`). Its holder decides
registrations and channel requests and sees nothing else of the admin
console, and their own new channels need no approval. It is given to
accounts only; a group is never given it.

**Applying.** The person chooses a username and display name, may say
in at most 500 characters why they would like to join, and makes their
passkey or access key as anyone registering does. The account is made
but not claimed: it signs in to nothing and is in no group or channel.
Signing in answers `approval_pending` while it waits, and the page
says the request waits and that they can sign in once it is approved;
after a decline, `forbidden` with the reason the decider gave; after
14 days without a decision the request lapses, and signing in says so.
At most 200 registrations wait at once (`rate_limited` beyond), on top
of the limits every registration has.

**Asking for a channel.** `createChannel` answers `approval_required`
to a member whose channels wait, after every other check, and the
client offers to ask instead (`requestChannel`), with a note. The
request is checked as making the channel would be, waits 14 days, and
is made as asked when approved; a declined request is told its reason.

**The queue.** Those who may decide a kind see it on one Approvals
screen, in the web chat and the app, reached from the account menu (not
the admin console), with the kinds as filters, a count beside the menu
entry, and the oldest first: who asked (as they are shown that
account), what (a channel's name, a webhook's name and channel), the
note, and when. A new request reaches them live (`approval.created`);
whether it also notifies follows their own notification settings.

**Deciding.** Approve, or decline with an optional reason (at most 500
characters). Each decision is written to the audit log
(`approval.approved`, `approval.declined`) with who decided. The one
who asked is told the outcome and the reason (`approval.decided`),
never who decided; an applicant is told when they next sign in. A
request that has been decided or has lapsed cannot be decided again
(`not_found`).

Stored: `approvals` (kind, requester, note, what to make, state,
reason, who decided and when, its end) and `approvers` (who holds the
flag, who gave it and when). The janitor marks lapsed requests.

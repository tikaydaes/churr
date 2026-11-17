# Churr design

Self-hosted team chat: one server binary, apps for phone and desktop, and
channels that can be shared between servers.

Nothing described here is built yet. This is the overall design that
`BRIEF.md` (second version, 13 November 2026) asks for. The whole of it
is version 1: there are no stages. The exact tables, endpoints, events
and formats are in `docs/` (listed in section 20). Section 21 has the
risks and the questions still open.

## 1. Constraints

- **One static Go binary for the server**, no cgo, cross-compiled. State
  is one data directory, plus a PostgreSQL database if the operator
  chooses one.
- **Nothing outside is required.** With every switch off the server
  makes no outbound connection. The connections that exist are listed in
  section 17 and each has a switch.
- **API first.** Apps, web chat, bots and scripts use one HTTP API and
  one kind of live connection.
- **Passkeys first.** No passwords. OIDC and SAML are optional. Churr
  never sends email and never needs an email address.
- **One community per server, one process per community.** No clustering.
- **Servers share only what their admins agreed to share.** There is no
  open federation.
- **The apps may contain native code** (WebRTC). The no-cgo rule is for
  the server, the hub and the relay. Sealed-chat encryption is written
  in Dart in the apps and JavaScript in the web chat, with a Go
  reference in the server's tests that all of them must match.

## 2. System overview

    Apps (Flutter) ──────┐                ┌───────────────── churr ─────────────────┐
    Web chat, admin ─────┼─ HTTPS + WS ──▶│ api · live · auth · perm · search       │
    Bots, scripts ───────┘                │ bots · preview · link · janitor · web   │
    Apps in a call ─── UDP (TCP fallback)▶│ calls (SFU)                             │
                                          │ store: SQLite or PostgreSQL · files/    │
                                          └────┬───────────────────────┬────────────┘
                                               │ wake-ups              │ link protocol (WS, dialled out)
                                               ▼                       ▼
                                          churr-relay            churr-hub or another churr
                                          APNs · FCM

| Package   | Responsibility                                                  |
|-----------|-----------------------------------------------------------------|
| `store`   | Storage interface with SQLite and PostgreSQL backends           |
| `perm`    | The one place that answers "may this account do this here"      |
| `api`     | REST endpoints, sessions, tokens, uploads, rate limits          |
| `live`    | WebSocket connections and fan-out of events to those allowed    |
| `auth`    | Passkeys, recovery, OIDC, SAML, TOTP, OAuth for the apps        |
| `bots`    | Webhook intake and payload conversion, commands                 |
| `forms`   | Forms, polls, answers, tallies and form actions                 |
| `sched`   | Messages sent later, reminders, recurring forms                 |
| `mod`     | Timeouts, slow mode, word filter, reports, bans                 |
| `blob`    | File storage on disk or in an S3-compatible store               |
| `search`  | Full-text queries on either backend                             |
| `preview` | Link preview fetcher with private-address refusal               |
| `link`    | Server identity, peers, shared conversations, the link protocol |
| `calls`   | Call rooms and media forwarding (Pion)                          |
| `push`    | Device registrations and wake-ups through the relay             |
| `importer`| Slack export reader                                             |
| `janitor` | Retention, expiry of sessions, invites and orphaned uploads     |
| `web`     | Embedded sign-in, account, admin and web chat pages             |
| `acme`    | Certificates when no proxy is in front                          |

Two more programs come from the same repository:

- `churr-hub` uses `link` and a small log store, and nothing else.
- `churr-relay` talks to APNs and FCM and stores nothing but counters.

## 3. Concepts

These are the things the rest of the design is built from.

**Account.** A person, a bot, or the local shadow of someone on another
server. Each has a server role: `owner`, `admin`, `member`, `guest` or
`bot`. An instance has one or more owners. The shadow of someone on
another server is stored with the role `remote`, which carries no powers
here. A role on the instance gives no power inside a channel: that comes
from the level held there.

**Conversation.** Anything messages are posted in. There are two kinds:

- A **channel** is public or private and has one or more members. It
  may have a name. A channel with no name is **ad-hoc**: it starts
  private, is listed under "Chats" and is titled with its members'
  names. An ordinary chat between two people is an ad-hoc channel, and
  so is a channel with only yourself in it. Members can add people and
  leave. Giving it a name moves it to the channel list; nothing else
  about it changes, so there is no conversion and no lost history.
- A **sealed chat** is exactly two accounts, end-to-end encrypted from
  its first message (section 14). There is one per pair, and nobody can
  be added to it.

Anyone who joins or is added to a channel sees all of its history and
can search it. The client says so before a person is added to an ad-hoc
channel. Starting a chat with a set of people opens the existing ad-hoc
channel with exactly those members if there is one, and creates it
otherwise. What two people want kept between them goes in a sealed chat.

- **Public to private, never back.** A manager can make a public
  channel private. People who are not members lose it at once,
  history and search included. A private channel can never be made
  public, because the people in it were told it was private. An ad-hoc
  channel is always private.
- **Locking.** A moderator or manager can lock a private channel. From then on
  nobody can be added or join, a group attached to it brings in nobody
  new, and someone who leaves cannot come back. A lock cannot be undone.
- A private or ad-hoc channel whose last member leaves is deleted with
  its content.
- **Names are not unique.** Any number of channels may share a name.
  Channels are identified by their identifiers, and where a person has
  to choose between two of the same name, clients tell them apart by
  parent, section, creation date, member count and managers, and by
  server in the apps. Names are free-form: any script, spaces and
  capitals allowed.
- **Linking a channel.** A message links to a channel by its
  identifier, never by its name. When someone types `#` and characters
  that match a channel they can see, the client always asks whether to
  link and lists the matches. A link is drawn as the channel's current
  name, so it follows renames. `#games` typed without choosing is plain
  text.
- **Sub-channels.** A named channel can have a parent channel, one
  level deep, and is listed under it. Nothing is inherited: a
  sub-channel has its own members, levels and public or private
  setting, and joining a parent only offers its public sub-channels. A
  manager of the sub-channel who is in the parent sets the parent, and
  an admin can set it from the admin console. A thread moved out to a channel becomes a sub-channel of
  the channel it came from, and so does a ticket channel opened from a
  form. If a parent is deleted its sub-channels become top-level.

**Membership.** An account's place in a conversation, with a level:

| Level     | May                                                            |
|-----------|----------------------------------------------------------------|
| `reader`  | Read, react, vote and answer forms, mark read                  |
| `member`  | Also post, upload, tick checklists, edit and delete their own messages |
| `moderator` | Also keep the peace: answer requests to join, add and remove people, ban them from the channel, delete others' messages, time people out, set slow mode, lock, handle reports |
| `manager` | Also hold the channel: set name, topic and welcome, pin, set levels, add bots and groups, post scheduled forms, set its settings, make it private, archive |

A channel has a default level for people who join it. An announcement
channel is a channel where only its managers, and the members they
allow, post. The creator of a channel is its first manager. Both sides
of a sealed chat are `member`.

A channel's managers can change any member's level in that channel, so a
person can be moderator or manager of one channel and nothing more. A
guest account is a guest in every channel it is in: it reads or takes
part at level `member`, and is never a moderator or a manager. If a
channel's last manager leaves, its longest-standing member who is a
person of this instance and not a guest becomes manager.

Each membership also has two settings the person chooses for
themselves. They are independent of each other.

**What notifies** (`mode`):

| Mode         | Badge                                     | Notifies      |
|--------------|-------------------------------------------|---------------|
| Any activity | Every unread message                      | Every message |
| Mentions     | Mentions, and replies in followed threads | The same      |
| Nothing      | Mentions of them by name only             | Nothing       |

"Mute until" is Nothing until a time, after which the earlier mode
returns. A conversation on Nothing is drawn dimmed.

**Where it shows** (`show`), in the person's sidebar:

| Show            | The conversation has a row in the sidebar           |
|-----------------|-----------------------------------------------------|
| Always          | Always                                              |
| On activity     | Only while it has unread messages                   |
| On notification | Only while it has a badge                           |
| Hidden          | Never                                               |

- Named channels start on Mentions. Ad-hoc channels and sealed chats
  start on Any activity.
- A conversation with no `show` chosen follows the app's own default,
  a preference kept in the app on the device and not on any server. Out
  of the box that default is Always.
- The conversation a person is viewing is always shown in its place,
  whatever `show` says. One that appeared because of activity goes away
  once it is read and the person has moved on.
- Hidden means no row in the sidebar and nothing else. A hidden
  conversation still notifies according to its mode, its notifications
  appear in the notifications list, and it is still reached by search,
  links, direct navigation and a "show hidden" switch.
- A mention is `@username`, a group the person is in, `@channel` or
  `@here`. Under Nothing and mute only `@username` still badges.
- The badge always counts what the mode would have notified, and the
  app's icon badge is the sum of the badges. The server works both out,
  so every client and the status endpoint agree. The server stores
  `show` and does nothing else with it.
- The person stays a member whatever they choose.
- Thread states sit inside the mode: an ignored thread never counts,
  and a followed thread's replies count only in the two loud modes. In
  Any activity, replies count for a person viewing that channel flat.

**Presence, status and do not disturb** belong to the account, not to a
channel.

- **Presence** is worked out by the server: `online` while any client
  is connected and active, `away` after a while idle, `offline`
  otherwise. A person can choose to **appear away**, which overrides
  the automatic value and shows them as away while they are active, for
  a duration or until turned off. Being in a
  call shows as a small mark beside presence.
- **Status** is set by the person: one emoji and a short line of text,
  such as "On holiday until Monday", an optional time at which it clears
  itself, and two switches that last as long as the status does: pause
  notifications (do not disturb) and appear away. It is shown beside
  their name. Bots can set one too, and a person's own script can set
  theirs through the API with a personal token.
- **Presets** are saved statuses. Admins define a set for the
  community, such as "Off sick" (away, notifications paused, until
  tomorrow) or "Focus" (notifications paused, two hours), and each
  person can save their own. Choosing one applies its text, duration,
  switches and availability together.
- **Availability** is one of two fixed values on top of presence, each
  with an optional end time:
  - *In a meeting.* Shown as a mark beside the person's name until the
    end time. A personal setting decides whether it also pauses their
    notifications.
  - *Out of office.* Shows the person as away with the date they are
    back, and anyone starting a message to them sees that before they
    send it.
  A person sets these themselves, or their own script does with a
  personal token (`PUT me/availability`). For a calendar integration
  that covers everyone, an admin creates a server integration (below).
  The server never reads a calendar itself.
- **Do not disturb** stops every notification for the account: sounds,
  pop-ups, call rings and phone wake-ups, which the server does not send
  at all while it is on. It is turned on for a duration or until turned
  off. Badges keep counting, so nothing is lost. Other people see a do
  not disturb mark on the person, so they know not to expect an answer.
- **Notify anyway.** Someone who messages a person in do not disturb
  is told so, and may push that one message through to that one person.
  It takes effect only where the person would otherwise have been
  notified: a mention by name, or a conversation they have on Any
  activity. It does nothing for `@channel`, `@here` or group mentions,
  for a conversation they have silenced or hidden, if they have blocked
  the sender, or if they have turned the feature off. The sender is
  never told which: it always appears to have worked, so it cannot be
  used to find out who has muted or blocked whom. The message is marked
  as notified anyway for everyone to see. Bots cannot use it.
- **Quiet hours** are a daily schedule: a start time, an end time and
  a choice of days, in the person's time zone, which their client keeps
  current. An account is in do not disturb whenever a manual do not
  disturb is on or it is inside quiet hours. The two do not touch each
  other: a manual one that outlasts quiet hours stays on, and quiet
  hours that outlast a manual duration keep things quiet. A person can
  skip the next quiet period in advance, which affects that period
  only. Turning do not disturb off during quiet hours ends that period
  and any manual one together. The schedule can be switched off.
- Presence, status and the do not disturb mark go to accounts that share
  a conversation with the person, and to linked servers on the same
  rule as presence (section 11).

**Server integration.** A credential for a program that works on the
server as a whole, not in channels. It is not an account: it has no
name in member lists, is in no conversation and can read no messages. An
admin creates one, names it, ticks what it may do and receives its
token. Everything it does goes to the audit log. There is
one thing to tick: set availability for accounts, found by username or
by their company sign-in identifier. Bots keep their rule unchanged: a
bot has no powers beyond the channels it is in.

**Group.** A named set of local accounts with a handle, such as
`@design` or `@mods`.

- Admins create groups and set who is in them. A group marked open can
  be joined and left by members themselves.
- Mentioning a group notifies those of its members who can see the
  conversation.
- A group can be added to a channel at a level, by anyone who could add
  a person there. Everyone in the group is then in the channel at that
  level, and stays in step as people join and leave the group. Someone
  who is in a channel both directly and through groups has the highest
  of their levels. A person who is in a channel through a group cannot
  leave it; they can hide or silence it, or leave the group if it is open.
- That is the only power a group has. There is no permission matrix and
  there are no per-channel overrides. Server roles stay separate.
- With company sign-in, a group at the identity provider can be mapped
  to a Churr group, which then takes its membership from the provider.
- A group may have a colour, shown as a label beside its members' names
  in member lists and on profiles.
- Bots and guests can be in groups. Accounts from other servers cannot.
- Two groups are built in and cannot be edited. `@everyone` is every
  owner, admin and member, and no guest or bot. `@admins` is the owners
  and the admins. A channel that `@everyone` has been added to is a
  channel every member is in, which is how a new member's starting
  channels are decided. A fresh server has one, `#general`.

**Message.** One post in a conversation, optionally a reply to a
top-level message (threads are one level deep). It has up to four
parts, which every client draws the same way: text, files, cards and a
form (section 5). Reactions and checklist ticks sit beside a message
and are not edits to it.

**Account profile.** Username, display name, avatar, an optional title
line and pronouns, and a time zone, which lets others see the person's
local time. A person can **block** another account: that account's
messages are collapsed for them, it cannot start an ad-hoc channel or
sealed chat with them, and its mentions do not notify them. An admin
can turn blocking off for a server.

**Event.** Every change (message posted, edited, reaction added, member
joined) is an event with a sequence number in its conversation. The live
connection carries events, read positions are sequence numbers, and
shared channels exchange the same events between servers.

### Identifiers

- Every account, conversation, message and file has a UUIDv7, so
  identifiers never clash between servers.
- A server's name is the host name of its public address. An account's
  full name is `username@server`; the server part is shown only for
  people from other servers.
- Each server numbers the events of each conversation it holds from 1,
  in the order they arrive there (`seq`). That number drives catch-up,
  read positions and unread counts on that server. Where a message is
  shown is its `order`, which follows `seq` in a conversation that is
  not shared and the shared clock in one that is (section 11). Nobody
  hands out positions to other servers.

## 4. Permissions

`perm` holds every rule. Handlers, search, file downloads and the live
fan-out all ask it and nothing else decides.

| Question                         | Answer                                                    |
|----------------------------------|-----------------------------------------------------------|
| Who can see that a conversation exists? | Its members. For a public channel, also every owner, admin and member. For a named private channel, also admins, in the admin console, which shows its name, members and managers and nothing else. |
| Who can read and search it?      | Its members. For a public channel, also every owner, admin and member. Nobody else, admins included. |
| Who can react, answer a form or tick a checklist? | Members only. Someone reading a public channel joins first. |
| Who receives its live events?    | Members only                                              |
| Who can post?                    | Members at level `member` or above, if it is not archived |
| Who can join a public channel?   | Owner, admin, member. Guests and bots must be added.      |
| Who can add people?              | Any member of an ad-hoc channel; moderators and managers of a named one (bots and groups: managers). Nobody, in a sealed chat or a locked channel, except an owner by owner override. |
| Who keeps the peace in a channel? | Its moderators and managers: remove and ban people, delete messages, time people out, slow mode, lock, reports |
| Who holds the channel?           | Its managers: name, topic, pins, levels, settings, archive |
| Who can create channels?         | Owner, admin, member                                      |
| Who can start an ad-hoc channel or sealed chat with whom? | Any local accounts; a remote account only while both share a shared channel. Bots cannot be in a sealed chat. |

Server roles:

- **Guests and bots** see only the conversations they are in and the
  accounts in those conversations.
- **Admins are not members of everything.** An admin or owner is a
  member only of the channels they join or are added to. Their channel
  list, unread counts and notifications cover those channels and no
  others, and they are not shown in the member list of a channel they
  have not joined. Their powers do not depend on membership.
- **Admins are not managers.** An admin or owner has a moderator's or
  manager's powers in a channel only where they hold that level, like
  anyone. Admins manage accounts, invites, sections, bots, settings and
  the audit log, exile people from the instance, purge messages, and
  handle reports in public channels for what is the instance's to do.
  The API gives them no way to read a private channel they are not in.
  For every named channel the admin console shows its name, members and
  managers, and lets an admin set its section, parent and retention.
  Ad-hoc channels and sealed chats do not appear there. That is policy:
  the operator holds the database.
- **Owners** are admins who can also change roles, make other people
  owners, run the export, and manage links to other servers. An
  instance may have several. Nobody demotes an owner, another owner
  included; an owner may step down while another owner remains. For a
  channel that has lost its managers or has a rogue one, an owner can
  appoint a manager from the admin console, and can add someone to a
  private channel, locked or not, by owner override, which every member
  is shown and which gives the owner no access. The operator of the
  machine can grant and revoke the owner role with `churr owner`.

Removing an account ends its sessions and tokens and removes it from
every conversation. Its messages stay, shown under its name as a former
member.

## 5. Messages, files and search

The exact format is in `docs/MESSAGE-FORMAT.md`. In outline:

- **Text** is Churr Markdown, a fixed subset every client implements
  the same way: bold, italic, strikethrough, inline code, links, block
  quotes, bullet and numbered lists, tick-box lists, tables, headings,
  and fenced code blocks. A fenced block may name a language
  (` ```go `) and is then syntax-highlighted by the client from one
  fixed list of languages shared by the apps and the web chat; an
  unknown name shows as plain monospace. The highlighter ships inside
  the clients. Clients build display nodes from the text directly and
  never parse HTML. An image written in Markdown is shown as its link.
- **Entities.** When a message is posted the server works out what the
  text refers to and stores that beside it: accounts, groups,
  `@channel` and `@here`, channel links, custom emoji, links and
  checklist items, each with its position in the text and the
  identifier it resolved to. Clients draw from the entities and never
  guess who `@sam` is, and a rename does not break an old message.
- **Checklists.** Each tick-box line (`- [ ] text`) is an item with its
  own identifier. Anyone in the channel at level `member` or above can
  tick or untick it. A ticked item is struck through and shows who
  ticked it and when. Every tick and untick is kept per item and anyone
  who can read the channel can open that history. Ticking is not an
  edit. The author adds, rewords and removes items by editing.
- **Files.** Up to ten per message, each with a name, size, type and an
  optional caption (see Files below).
- **Cards.** Up to five per message, posted by bots, webhooks and the
  server itself, never typed by a person. A card has fixed parts: a
  colour bar, title, link, text in Churr Markdown, up to ten
  label/value fields, one image, a footer and a time. Link previews are
  cards. Slack attachments and Discord embeds map onto cards. A card's
  image is an uploaded file, or is fetched once by the server and
  stored, under the link-preview rules; clients never load it from
  another site.
- **Forms.** One per message. A form is a list of fields and up to five
  buttons, and it is the one interactive mechanism: polls, sign-ups,
  approvals and bot menus are all forms.
  - Fields: single choice, multiple choice, short text, long text,
    number, date, time, person, channel, tick-box.
  - The server collects the answers, one set per account, changeable
    until the form closes. What happens with them is the form's
    **action**: keep a live tally (a poll), collect them for the author
    to read and download, post each answer as a reply in the message's
    thread, open a channel (a ticket), join or leave an open group, or
    send them to the bot that posted the form.
  - Settings: named or anonymous, when results show (at once, after
    answering, after closing), a closing time, and whether answerers
    may add options to a choice field.
  - Anyone who can read the channel can answer. Bots cannot.
  - People post forms from presets: `/poll`, `/rsvp`, `/signup`,
    `/approve`, `/form`. Bots and webhooks post the same forms through
    the API.
- **Polls.** A poll is a form with one choice field and the tally
  action. Counts update live. A named poll shows who chose what; an
  anonymous one never does, to members or admins through the API. The
  creator or a channel manager can close it, and the final
  result stays on the message.
- **Reactions.** Any standard or custom emoji, one of each per account
  per message, shown as counts with the names behind them. Anyone who
  can read the channel can react. At most twenty different emoji per
  message. Reactions do not notify. Bots see them in their channels.
- **Fallback.** The server writes a plain-text version of every card
  and form into the message, so a client or bot that does not know a
  part still shows something sensible.
- **In a sealed chat** a message has text, files and reactions, all
  encrypted on the device, and tick-box changes travel as encrypted
  messages between the two people. There are no cards and no forms.
- **Mentions.** `@username`, `@group`, `@channel` (every member) and
  `@here` (members who are online). An admin setting can limit
  `@channel`, `@here` and `@everyone` to channel managers.
- **Threads.** Any top-level message can be replied to, and replies
  hang under it, one level deep. There is one way to reply, and no
  "also send to channel".
  - **Reply with a quote.** "Reply" on a message, or on text selected
    in it, makes a thread reply that carries the quote: the selected
    part, or a short excerpt of the whole message. Replying to a
    top-level message starts its thread or adds to it. Replying to a
    reply stays in that thread and quotes that reply. A reply stores
    the message it quotes and the quoted range; clients draw the quote
    from the current text, so an edited or deleted original is never
    shown stale.
  - **Two views of the same data**, chosen per person, saved on the
    server, and overridable per conversation:
    - *Threaded* (the default): the parent with its summary line, and
      the marker described below.
    - *Flat*: replies appear in the conversation in the order they were
      written. Each has a header showing what it answers, a link up to
      that message, a link to the top message, and "open thread". No
      marker is drawn. Replies count towards the conversation's unread
      number for people using this view.
    Typing in the message box without replying posts a top-level
    message in either view. Notifications are the same in both.
  - In the conversation the parent shows a summary line: reply count,
    who replied, time of the last reply. Opening it shows the thread
    beside the conversation on wide screens and over it on phones.
  - The parent never moves. When a thread gets a reply, a one-line
    **marker** appears in the conversation at the point the reply was
    made, for example "3 new replies to: Deploy plan for Friday". A
    thread has one marker at most: a later reply moves the marker down,
    never the message. Selecting it opens the thread. The marker counts
    the replies the reader has not seen. It is drawn by the client from
    the parent's last-reply sequence number, so nothing extra is stored,
    and it does not mark the conversation unread or notify anyone.
  - Each person has one of three states for a thread:
    - *Following*: replies are shown (inline in the flat view, by the
      marker in the threaded view) and each one notifies them. A person
      follows a thread automatically when they start it, reply in it or
      are mentioned in it.
    - *Neither*, the state for threads they have not touched: replies
      are shown the same way, without notifications.
    - *Ignored*: replies are hidden from the flat view, no marker is
      drawn, nothing notifies and replies do not count as unread. A
      mention of the person by name still notifies them. The parent and
      its summary line stay visible.
    "Follow thread" and "Ignore thread" are offered on a reply in the
    flat view, on the marker and inside the open thread. Replying in an
    ignored thread sets it back to following.
  - **Thread to channel.** A long thread can be moved out into a
    channel of its own, by the person who started the thread, a manager
    of the channel, or an admin. They give it a name. The parent stays
    where it was in the old channel, and a copy of it becomes the new
    channel's first message. The replies move: they become ordinary
    top-level messages after it, keeping their authors, times, quotes
    and reactions, so they can now have threads of their own. Under the
    parent in the old channel a line says where the thread went, and
    that thread takes no more replies. The new channel is public if the
    old one was; otherwise it is private. The people following the
    thread become its members and anyone else in the old channel can
    join from the link. This is a move, not a copy, and it is not
    undone. It is not offered in channels shared with other servers or
    in sealed chats.
  - A "Threads" view lists the threads a person follows that have
    unread replies, newest first, across all their conversations.
  - Replies take sequence numbers from the conversation like any other
    event, so live delivery, catch-up and shared channels need nothing
    extra. Each follower has a read position per thread.
  - Search finds replies, and a result opens its thread.
  - Bots and webhooks reply in a thread by naming its parent.
  - In a sealed chat the link from a reply to its parent is inside the
    encrypted message, so the server sees a flat stream.
- **Edits, deletes and purges.**
  - *Quick corrections.* For a short time after posting (a server
    setting, 30 seconds by default) the author can edit freely. The
    message is not marked as edited and no earlier version is kept.
  - *Edits.* After that, an edit marks the message as edited and keeps
    the earlier version. A server setting gives how long after posting
    an author may still edit: a number of minutes, or no limit.
  - *Deletes.* A deleted message is replaced by a marker saying who
    deleted it and when. Its text and files are kept as history.
  - *History.* Anyone who can read the conversation can open a
    message's history: its earlier versions, and its last text if it
    was deleted. History is not searched.
  - *Purge.* Purging erases a message's history for good, and for a
    deleted message erases its content and leaves only the record that
    a message was there, who posted it and when. An author can ask for
    a purge of their own message.
    - In a private or ad-hoc channel the purge happens at once. No
      admin is involved and nobody outside the channel sees anything.
    - In a public channel the history is hidden from everyone except
      admins as soon as the author asks. An admin then chooses one of
      three outcomes: show the history again, keep it hidden but kept
      for admins, or erase the content and leave only the record. The
      author is not told which was chosen.
    - An admin can delete and purge any message they are able to see,
      in one step.
    - Erased means gone from the database, the search index and the
      file store.
  - Sealed chats keep no history on the server: an edit or delete there
    replaces the message, and there is nothing to purge.
  - In a shared channel, purge requests go to the admins of the
    author's own server, and the purge is sent to the linked servers as
    an event they are expected to honour.
- **Files.** Uploaded to a conversation first, then attached to a
  message. Stored in `files/` under random names, or, if the operator
  configures it, in an S3-compatible object store. The object store is
  off by default, holds file contents only, and is reached only by the
  server: clients always download through the server, so permissions
  and content types are enforced the same way. Images in PNG, JPEG,
  GIF and WebP are recognised by their content, get a server-made
  thumbnail and show inline, several as a gallery. Audio and video in
  the common web formats play inline. Text and code files show their
  first lines. Everything else is a download. Every download has a
  content type the server chose, the `nosniff` header, and a content
  security policy that forbids scripts, so no upload can run as a page.
- **Custom emoji.** Named images (`:nightjar:`), added by admins, or by
  members if a setting allows. Stored like files, with a size cap.
- **Link previews.** When the setting is on and a message contains a
  link, `preview` fetches the page once, keeps its title, description
  and one image (re-encoded and stored locally), and adds them to the
  message as an event. It connects only to public addresses on ports 80
  and 443, checks the address again after every redirect, and caps time,
  size and redirect count. Clients never contact the linked site.
- **Search.** SQLite uses FTS5; PostgreSQL uses a `tsvector` column with
  a GIN index. A query is joined to what `perm` says the asker can read.
  Filters: conversation, author, date range, has file.
- **Retention.** Admins set one period for the server, or none. A
  channel's managers can set a shorter period for that channel, and an
  admin can set any period for a channel from the admin console. Ad-hoc channels and sealed
  chats follow the server's period. The janitor deletes expired
  messages with their history, search entries and files, with SQLite's
  secure-delete options on so the text does not linger in free pages.
  Retention needs nobody's approval.

## 6. Storage

`store.Store` is a Go interface. The rest of the server depends only on
it, and one shared test suite runs against both backends.

| Backend    | When                         | Notes                               |
|------------|------------------------------|-------------------------------------|
| SQLite     | Default                      | Pure Go driver, one file, WAL mode  |
| PostgreSQL | Optional, `-db postgres://…` | For larger or existing deployments  |

`churr migrate-db` copies a community from one backend to the other.
Uploads and keys stay in the data directory with either backend.

Every table and column is in `docs/schema.sql`, explained in
`docs/SCHEMA.md`. The groups of tables are: the server (settings, audit
log), accounts and sign-in, groups, conversations and membership,
messages and their parts, moderation, scheduling, bots and webhooks,
calls, links to other servers, and sealed chats.

Tokens are 256-bit random values stored only as SHA-256 hashes. Secrets
the server must be able to use (TOTP secrets, the OIDC client secret,
stream keys) are sealed with AES-256-GCM under a key file in `keys/`.

## 7. API

All under `/api/v1`, JSON, authenticated by a session cookie or
`Authorization: Bearer <token>`. `docs/openapi.yaml` is the contract,
with 325 operations, and `docs/API.md` is the guide to it. The table
below is only an outline of the areas; where a path here differs from
the contract, the contract is right.

| Area          | Endpoints                                                    |
|---------------|--------------------------------------------------------------|
| Server        | `GET server` (name, sign-in methods, features that are on)   |
| Sign-in       | `passkey/register/{begin,finish}`, `passkey/login/{begin,finish}`, `recovery`, `totp`, `sso/{provider}/start`, `logout` |
| Me            | `GET/PATCH me`, `PUT me/status`, `PUT me/availability`,  `passkeys`, `sessions`, `tokens`, `totp`, `devices` |
| Status        | `GET status` (unread and mention totals, newest event; cheap and cacheable) |
| Accounts      | `GET accounts`, `GET accounts/{id}`, `GET groups`, `POST/DELETE groups/{id}/members/me` (open groups) |
| Conversations | `GET conversations`, `GET channels` (browse), `POST channels`, `POST chats` (find or create an ad-hoc channel), `POST sealed`, `PATCH conversations/{id}`, `join`, `leave`, `members`, `read`, `typing`, `pins` |
| Messages      | `GET conversations/{id}/messages`, `POST …/messages`, `GET messages/{id}/thread`, `PATCH/DELETE messages/{id}`, `PUT/DELETE messages/{id}/reactions/{emoji}`, `PUT messages/{id}/checklist/{item}`, `PUT/DELETE messages/{id}/form/answer`, `POST messages/{id}/form/buttons/{button}`, `POST messages/{id}/form/close`, `history`, `purge`, `report`, `bookmark` |
| Files         | `POST conversations/{id}/files`, `GET files/{id}/{name}`, `GET files/{id}/thumb` |
| Search        | `GET search?q=&in=&from=&since=&until=&has_file=`            |
| Events        | `GET events` (WebSocket), `GET conversations/{id}/events?after=` (catch-up) |
| Bots          | `POST commands`, `POST conversations/{id}/webhook-requests`  |
| Scheduling    | `GET/POST/PATCH/DELETE scheduled` (messages to send later, reminders, recurring forms) |
| Calls         | `POST conversations/{id}/call/join`; signalling runs over the WebSocket |
| Integrations  | `PUT integration/accounts/{id}/availability`, authenticated by an integration token |
| Admin         | `settings`, `invites`, `accounts`, `groups`, `channels`, `sections`, `bots`, `integrations`, `saved-replies`, `word-filters`, `reports`, `purge-requests`, `bans`, `timeouts`, `webhooks`, `emoji`, `sso`, `peers`, `streams`, `audit`, `export` |

Outside `/api/v1`: `/oauth/*` for the apps (with `/oauth/unlock`, the
page that unlocks sealed chats with a passkey), `/hooks/{token}` (and
`/hooks/{token}/slack`, `/discord`, `/github`) for webhooks,
`/.well-known/churr` for the server's name and public keys, and `/chat/`
for the web chat. These are in the contract. Not in it: the form posts
of the server's own pages, `POST /oauth/authorize` (approving an app)
and `POST /sso/register` (the username at a first company sign-in);
`GET /healthz`; `/link/v1` for other servers and hubs; the theme page
at `/themes/{id}`; the other web pages; and, on port 80 when the server
fetches its own certificate, the certificate authority's challenge,
with every other request there redirected to HTTPS.

## 8. Live delivery

- One WebSocket per client at `/api/v1/events`. After connecting, the
  client sends the last sequence number it has for each conversation and
  the server replays what it missed from the `events` table, or tells
  the client to reload a conversation that has fallen too far behind.
- Fan-out is by membership. To deliver an event the hub asks `perm` for
  the conversation's members and writes to their connections. An account
  that is not a member cannot be subscribed, so there is no subscription
  request to get wrong.
- Typing and presence are events that are sent and never stored.
  Status and do not disturb are stored on the account and changes to
  them are sent the same way (section 3).
- Reading on one device moves the read position on the others.
- A slow connection is dropped when its send queue fills, and catches up
  when it reconnects.

## 9. Authentication

**Ways in**

| Method       | Notes                                                         |
|--------------|---------------------------------------------------------------|
| Passkey      | WebAuthn, discoverable credentials, user verification required. Sign-in needs no username. Always available to the owner and admins. |
| OIDC         | Authorization code flow with PKCE against one or more providers the admin adds. |
| SAML 2.0     | Sign-in started from Churr, signed assertions only, provider metadata pasted or fetched once. |
| Recovery code| Eight single-use codes shown once when a passkey account is created. |
| Recovery link| One-time, made by an admin (an owner's only by that owner), or printed by `churr recovery <username>` for a locked-out owner. |

**Policy**, set by the admin:

| Setting              | Values                                                  |
|----------------------|---------------------------------------------------------|
| Registration         | `open`, `invite` (default), `closed`                    |
| Who may invite       | Admins (default), or admins and members. Guest invites are always made by admins. |
| Member sign-in       | `passkey` (default), `either`, `sso`                    |
| Second factor        | `off` (default), `required`                             |
| Accepted as second factor | Churr passkey, authenticator-app code, the provider's own statement that multi-factor was used. The admin ticks which. |
| Session length       | 30 days for passkey sign-ins, 24 hours for company sign-ins |

- A passkey sign-in checks a PIN or biometric on the device, so it
  already counts as two factors and is never asked for a second.
- With a second factor required, a company sign-in is followed by one of
  the accepted steps before a session exists. A member with none set up
  is walked through adding one on first sign-in.
- With member sign-in set to `sso`, members cannot register or use
  passkeys to sign in. Owners and admins still can, so a broken
  identity provider cannot lock the server.
- An account is created the first time someone signs in through a
  provider, if registration allows it. The provider's subject identifier
  is stored; the email address is not. A provider group can be mapped to
  the admin role or to a Churr group (section 3).
- The first account is made through a one-time setup link printed when
  the server first starts, and becomes the first owner.

**Apps and tokens**

- The apps use OAuth 2.0 authorization code with PKCE in the system
  browser, against the server's own address, exactly as Codemail's apps
  do. The app therefore gets every sign-in method without implementing
  any. QR pairing from a signed-in web page is the second way in.
- A member can create personal tokens that act as them.
- A bot has one or more tokens and cannot sign in any other way.

## 10. Bots, webhooks and commands

The model, from the brief: a bot is a member with a token; its reach is
the channels it is in; a webhook is the smallest bot.

- **Bots.** An admin creates a bot: a name, an avatar, a token. It is an
  account with role `bot`. It uses the API and the WebSocket like any
  client and is subject to `perm` like any guest. It cannot browse, join
  by itself, be an admin, or read sealed chats.
- **Webhooks.** A webhook is a bot account bound to one named channel,
  with a token that only works at `POST /hooks/{token}`. It can post
  there and do nothing else. A member asks for one from a channel's
  menu; the request waits in the admin console; once it is approved the
  requester collects the address, which is shown once, with a signing
  secret for senders such as GitHub that sign what they send. Ad-hoc
  channels cannot have webhooks.
- **Payloads.** A webhook address accepts Churr's own message shape
  (text, files, cards, a form, an optional thread). With `/slack` on
  the end it accepts Slack's incoming-webhook shape, with `/discord`
  Discord's, and with `/github` the events GitHub sends from a
  repository. Slack attachments and Discord embeds become cards. Layout
  that has no equivalent becomes text.
- **Commands.** A bot registers commands with a name, a description and
  an argument hint. In a conversation the bot is in, typing `/` lists
  them beside the built-in ones. Sending one posts nothing; the bot
  receives a `command.invoked` event with the text, the conversation
  and who asked, and may answer for everyone or only for the asker.
- **Forms.** A bot posts forms like anyone else and chooses the "send
  to the bot" action. Each answer or button press reaches it as an
  event on its connection. The bot usually answers by editing its
  message or replying only to the person. A bot can also ask the server
  to show one person a form in a dialog, in answer to a command or a
  button, when it needs more detail from them.
- There are no outgoing webhooks. A bot connects to the server, so it
  can run on a laptop or behind a firewall, and the server never calls
  an address a member supplied.

## 11. Shared channels between servers

### Shape

- **Peer.** A lasting, mutually accepted connection between this server
  and either another Churr server (a direct link) or a hub. Peers are
  made by the owner. A direct link joins exactly two servers, and a
  server never relays between two others. Three or more servers share a
  channel through a hub.
- **Stream.** One conversation shared over a peer. A server pair or a
  hub carries any number of streams.
- **Who owns a stream.** On a direct link, the **owner server**: the one
  that shared the channel. On a hub, a channel belongs to the **hub**
  (see "Channels a hub owns"). An ad-hoc channel or sealed chat that
  crosses a hub is not the hub's: it stays under the servers involved,
  and the hub only carries it.
- **Time reference.** The party whose clock a stream is timed by, and
  which keeps the stream's log: the owner server on a direct link, the
  channel's home hub on a hub. It orders nothing.

Every server in a stream keeps its own full copy of the conversation and
serves its own members from it. Nothing is fetched from another server
when a member reads.

### Identity and trust

- At first start a server creates an Ed25519 signing key and an X25519
  encryption key in `keys/`, and publishes the public halves at
  `/.well-known/churr`.
- **Direct link.** Owner A creates a link invitation: A's address, key
  fingerprint and a one-time secret, valid for seven days. Owner B
  pastes it into B's admin console. B's server dials A at `/link/v1`,
  each side proves its key by signing the other's challenge, and the
  secret proves the invitation. Both consoles show the other server's
  name and fingerprint. Keys are pinned from then on.
- **Hub.** The hub's operator issues join invitations the same way. A
  server that joins sees the list of other servers on the hub and their
  keys, signed by the hub and re-checkable against each server's own
  `/.well-known/churr` where it is reachable.
- The server that accepts an invitation always dials out and keeps one
  WebSocket open. On a hub, every server dials out, so no member server
  needs to be reachable from the internet.

### Order: a shared clock

Every shared conversation, over a direct link or a hub, is ordered by a
clock its servers share. Nobody hands out positions.

- Each server measures how far its clock is from the time reference's,
  with timed exchanges over the link (four timestamps, as NTP does,
  keeping the best of recent samples). It refreshes the figure while
  connected and keeps it while not.
- Every event a server makes in a shared conversation carries a stamp
  under its signature: `at`, the reference clock's time in milliseconds
  as that server reads it, and `n`, that server's own counter. `at` is
  always later than anything the server has already applied in the
  conversation, so a reply sorts after what it answers.
- The order is by (`at`, origin server name, `n`). Every server and hub
  works it out the same way from the events alone.
- The time reference refuses an event stamped more than 30 seconds ahead
  of its clock, or more than 30 seconds behind unless the origin was
  disconnected at that time. The origin stamps it again and resends.
- A server's own `seq` stays: it numbers what arrives on that server, for
  catch-up, read positions and unread counts. Every message carries
  `order`, a text that sorts in the conversation's order, and clients
  sort by it. A message that arrives after later ones were already
  delivered is marked `late`, takes its place, and is unread like any
  new message.
- A member's message is stored and shown in its place at once. It is
  **unsent** only while the link has not yet acknowledged it; while the
  reference cannot be reached, everything waits in `outbox` and goes on
  reconnection.
- Changes that need one authority (the server list, managers and moderators, key
  epochs, a channel's record) are versioned by their signer, not ordered
  by the clock. Of two edits to one message the later stamp stands.
- Servers and hubs catch up with each other by per-origin counters: each
  says the highest `n` it holds from each origin.

### Sharing a channel

The owner of server A opens a channel, chooses "Share", picks a peer,
and chooses whether to send the last 0 to 90 days of history.

- **Over a direct link** A sends B an offer. B's owner attaches it to a
  new local channel. A is the owner server.
- **Over a hub** the channel becomes the hub's, and the person who
  shared it is its first manager. A may name servers on the hub to
  invite, and sets the channel's listing mode (below).

From then on a member posts, their server stores and shows the message
at once with its stamp, and sends the event to the time reference,
which checks the stamp and passes it to every other server in the
stream. Each server checks it again and delivers it to its own members.

### Envelope

    stream id · key epoch · origin server · origin event id
    stamp: at, n, the offset the origin used
    ciphertext (XChaCha20-Poly1305 under the stream key)
    signature (Ed25519, origin server, over all of the above)

- On a direct link the owner server makes the stream key. On a hub, at
  every change to the server list the hub names one member server, the
  key-maker, to make the new key. Either way the key is sent to each
  server encrypted to its X25519 key. A hub never holds a stream key,
  so it sees sizes, times and server names, and no content.
- When a server leaves or is removed, a new key epoch starts and the
  removed server is sent nothing further.
- The same envelope is used on direct links and through hubs, so there
  is one code path.
- Duplicates are dropped by origin event id.

### What a server accepts

Each receiving server checks every event itself. It does not rely on the
hub or on the sender being honest.

| Event                               | Accepted only from                         |
|-------------------------------------|--------------------------------------------|
| Message, edit, delete, reaction by an account | That account's home server       |
| Profile of an account, join, leave  | That account's home server                 |
| Remove any message                  | The owner server on a direct link; on a hub, the home server of a manager or moderator on the hub-signed lists |
| Change name or topic                | The owner server on a direct link; on a hub, only a record the hub signed |
| Add or remove a server, managers, moderators, listing mode | The owner server on a direct link; on a hub, only the hub's signature |
| New key epoch                       | The owner server on a direct link; on a hub, the key-maker the hub named |
| Typing, presence                    | The account's home server; never stored    |

A server may always act on its own copy: hide a message locally, mute a
remote account, stop sharing a channel, or drop a peer. Those do not
travel.

### Channels a hub owns

A channel shared through a hub belongs to the hub. The hub keeps a
record of each one: stream id, name, description, listing mode, member
servers, managers and moderators. Only the hub signs a change to that
record, and member servers accept a change only with the hub's
signature.

- **Managers** are accounts, written `name@server`, and hold the
  channel. Whoever shares a channel to a hub is its first manager. A
  manager may invite an instance, approve or decline an instance's
  request to join, ask the hub to remove an instance, change the name
  and description, set the listing mode, add and remove managers and
  moderators, and close the channel. A channel keeps at least one
  manager.
- **Moderators** are accounts too, and keep the peace: they delete any
  message for everyone, admit people to a private channel and decline
  their requests, and remove people on every instance. A manager may do
  all a moderator may.
- A manager's or moderator's action is signed by their home instance
  and accepted when they are on the hub-signed list. On each instance
  the copy's managers and moderators are the local accounts on those
  lists, and nobody else: a role on the instance gives no authority in
  the channel.
- **Listing mode**, per channel: `unlisted` (instances join only when a
  manager invites them), `request` (every instance on the hub sees it in
  the hub's directory and may ask to join; a manager decides) or
  `open` (every server on the hub sees it and may join without asking).
- **The directory** is served by the hub to its member servers. It shows
  each listed channel's name, description, listing mode, managers and
  number of servers.
- Each server's pins, slow mode and so on stay local. Authority over
  the channel on other instances comes only from the hub's lists.
- The hub's operator can appoint and dismiss managers and remove
  servers, as a last resort.

### Moving a two-server channel onto a hub

A channel shared over a direct link can be moved onto a hub both servers
are already on, so that a third server can join.

1. The owner server proposes the move, naming the hub by name and key
   fingerprint. The other server's owner accepts or declines. Nothing
   changes until it is accepted, and a proposal expires.
2. On acceptance the stream, with the same stream id and the same
   channel on both servers, is opened on the hub. The hub becomes its
   owner and time reference, and a new key epoch starts. Its managers
   are every account that was a manager of the channel on either server
   at that moment. The former owner server keeps no special role.
3. Nothing is frozen: events carry their own stamps, so messages posted
   during the move keep their places, and none is lost, duplicated or
   reordered. Neither server ends up with a second channel.
4. If the hub refuses, either server cannot be reached before the
   proposal expires, or any step fails before the move is committed, it
   is abandoned and the channel carries on over the direct link.

Afterwards further servers join through the hub in the ordinary way. The
direct link stays for the two servers' other streams. The hub learns
nothing it would not learn of any channel shared to it.

### Trunks between hubs

Hubs can be joined by trunks into a cluster, a tree with no loops, with
a roster every hub signs into. A channel then reaches servers on several
hubs.

- Each channel has a **home hub**, the hub it was shared to, which owns
  it and is its time reference. The other hubs carry it for their own
  servers.
- If the cluster splits, each side keeps delivering among its own
  servers. Server-list, manager and moderator changes happen only where the home
  hub can be reached.
- When the sides join again, the hubs exchange what each missed, by
  per-origin counters, and late messages take their place by `at`.
- An operator can move a channel's home to another hub of the cluster.
- Ad-hoc channels, sealed chats and moves from a direct link do not
  cross a trunk.

### People, ad-hoc channels, sealed chats and presence

- An account from another server appears locally as a shadow account,
  `name@server`, created when its home server announces that it joined a
  shared channel. It cannot sign in here and holds no local role.
- A local member may start an ad-hoc channel or a sealed chat with
  remote accounts while each of them shares at least one shared channel
  with the member. Every server involved checks this, and all of them
  must sit on one hub or one direct link. The new conversation is a
  stream of its own over the same peer, with the same envelopes. When
  the people no longer share a channel it becomes read-only.
- A sealed chat's envelopes carry ciphertext only the two people's
  devices can open, so neither server and no hub can read it.
- Presence is sent for an account only to servers that share a channel
  with it, and an admin can turn sending off.
- Files and custom emoji do not travel in the stream. An event names a
  file by its hash; each receiving server fetches it from the server
  it came from over the link, checks the hash, and stores its own copy,
  up to a size the stream's owner sets. A hub passes these along
  without keeping them.
- Only polls work across servers: a form in a shared conversation must
  use the tally action. Answers go, sealed, to the server the form was
  posted on, which keeps the count.
- Thread-to-channel moves, levels, pins, slow mode, the word filter,
  time-outs and archiving stay local to each server.

### The hub

`churr-hub` is the link package with a log and a clock, and the owner of
the channels shared through it. It accepts servers its operator invited,
measures their clocks, checks the stamp on every envelope, forwards it,
and keeps the log for 30 days so a server that was offline can catch up.
It keeps each channel's record and serves the directory. It has no
members, accounts, messages or search. It has an operator command line
and a status page, and enforces a rate and size limit per server.

What a hub operator can see: each server's name and address; which
servers are in which stream; the size and time of everything carried;
and for each channel the hub owns, its name, description, listing mode,
managers, moderators and member servers. What they can do: admit and
remove servers, and appoint and dismiss managers. What they cannot do: read
any message or file, or see who the members are; the hub never holds a
stream key. They could admit a server they control, which would then be
given the key from then on, and every member server would see the
server list change when it happened. The hub's public status page shows
how many channels it has by listing mode, and no names.

## 12. Notifications

| Client          | When the app is not in front                                |
|-----------------|-------------------------------------------------------------|
| Web chat        | Browser notifications while the page is open. Nothing after. |
| Desktop apps    | Stay in the tray with the WebSocket open                    |
| Android         | Woken by FCM through the relay. Without Google services: UnifiedPush if the phone has a distributor and the admin has turned it on, otherwise a foreground service that keeps the WebSocket open |
| iPhone          | An APNs notification through the relay, completed on the device. An incoming call is an ordinary notification with a ring sound, not the system call screen, because that would tell the relay the event is a call |

**What the server sends.** When a member with a registered phone is
mentioned, messaged directly or replied to, and no client of theirs is
connected and in use, the server posts to the relay: the device's push address, the platform,
and an opaque event identifier. No text, no names, no conversation.

**What the phone does.** On iPhone the push is a visible notification
with placeholder text. A notification extension in the app receives it
first, fetches the real message from the member's own server with the
app's token, decrypts it if the conversation is sealed, and replaces the
placeholder. If the fetch fails the placeholder "New message" is shown.
Android does the same from a data message.

**The relay.** `churr-relay` is a small Go program holding the APNs and
FCM keys for the published apps. It needs no account: any Churr server
may use it. It limits requests per device and per server address, and
keeps counters only. An admin can turn it off or point the server at a
different relay, which only helps someone who also builds and signs
their own apps.

**The status endpoint.** `GET /api/v1/status` returns unread and mention
totals and the newest event identifier. The apps call it when woken, on
background refresh and after reconnecting, and use it to set the icon
badge. It is the fallback when no push arrives.

## 13. Calls

- Any conversation can have one call at a time. Starting it posts a
  message; members join from that message or the conversation header.
  There are no separate voice channels.
- Voice, camera and screen sharing. Media goes from each app to the
  server, which forwards it to the others (a selective forwarding unit,
  built on Pion). Apps never connect to each other, so a member's
  address is not shown to other members and no TURN server is needed.
- The server listens on one UDP port (24877 by default), with ICE over
  TCP on the same number as a fallback. Behind a reverse proxy that port is published
  directly, and the operator gives the server its public address.
- Signalling runs over the existing WebSocket. `perm` decides who may
  join: members of the conversation at level `member` or above.
- The server forwards audio from everyone, and video from the people a
  client is currently showing, at the size it is showing them.
- In a shared channel the call lives on the server of whoever started
  it. Members of other servers join that server's media port with a
  short-lived pass their own server requests over the link. Their
  signalling is relayed between the two servers in frames only those
  two can open.
- Calls work in the apps and in the web chat. Both use WebRTC, which
  browsers have built in, so the server treats them alike. In a browser:
  the page must be served over HTTPS (or localhost), the browser asks
  for microphone and camera permission, phone browsers cannot share
  their screen, an incoming call shows only while the page is open, and
  an iPhone may stop the audio when the tab goes to the background.
- Nothing is recorded.

## 14. Sealed chats

- A sealed chat is a direct conversation between exactly two accounts,
  encrypted end to end from its first message. It is a kind of
  conversation of its own, not a setting on a channel: channels are
  never sealed and a sealed chat never becomes a channel. There is one
  per pair of people. The other person may be on another server
  (section 11).
- `docs/SEALED.md` is the full design. This is the outline.
- **Keys.** Churr uses its own small scheme here, built from five
  standard parts (X25519, Ed25519, HKDF-SHA-256, AES-256-GCM, SHA-256)
  that exist in Dart, in every current browser, in Apple's own library
  and in Go. It does not use the MLS standard: a sealed chat is always
  two people with a handful of devices, and a full MLS stack would mean
  a second language, native builds for five platforms and a browser
  module, for machinery made to manage thousands of members.
- **Epochs.** The chat has a current secret. When either person adds or
  loses a device, or the secret is more than seven days old when
  someone sends, one device makes a fresh secret, encrypts it
  separately to every device on both people's signed device lists, and
  signs the record, which also names the previous one. The server
  stores and orders these records and cannot open them.
- **Content.** Messages are encrypted under a key derived from the
  current secret and signed by the sending device. Someone who obtains
  one epoch's secret can read that whole epoch; that is the price of
  letting a later device read earlier messages.
- **What is encrypted:** message text, file contents, file names,
  reactions, tick-box changes and edits. The server still sees who is
  in the chat, who posted, when, how large, and a flag saying whether
  an item should notify.
- **Key backup.** Each account has one random backup key. It encrypts a
  set of small records on the server, one per sealed chat per key
  epoch, holding the secret that epoch's content key is derived from,
  and one account record
  holding the account's signing key. The server can neither open nor
  forge them. A device's own private keys are never in the backup.
- **Opening the backup.** The backup key is stored on the server in
  several locked copies, any one of which opens it:
  - one per passkey, locked to a key pair derived from that passkey's
    PRF secret. This is the main way: signing in with the passkey on a
    new device or browser also unlocks sealed history.
  - one locked with a twelve-word recovery phrase, shown once.
  - approval from a device that is already unlocked.
- **Several passkeys.** Every passkey has a different PRF secret and
  its own locked copy. An unlocked device can make a copy for a passkey
  that is not present. Removing a passkey that could unlock, revoking a
  device from another device, or replacing the phrase replaces the
  backup key and the account key. Signing out on a device itself
  replaces nothing.
- **Passkeys without PRF.** Churr asks for PRF whenever a passkey is
  created, because it cannot be added afterwards. Passkeys from stores
  without it sign in normally, are marked as unable to unlock, and rely
  on the phrase or on another device. Accounts that only use company
  sign-in add a passkey for unlocking or use the phrase.
- With no working passkey, no phrase and no other device, earlier
  sealed messages cannot be read again.
- **Devices.** Each account has a list of its devices, signed with its
  account key. Clients show a notice in the chat when the other
  person's list changes, and two people can compare a 60-digit code to
  check there is nobody in between.
- What a sealed chat does not have: search, export, bots, webhooks,
  cards, forms, link previews, and any way to add a third person.
- **Where the server's code is trusted.** A passkey only works on a
  page served from the server's own address. So unlocking with a
  passkey passes the key through a page the server supplies, in the
  apps as well as in the web chat. After that the apps do their
  encryption in code that came from the app store, while the web chat
  runs encryption code the server sends on every load. A dishonest or
  compromised server could therefore capture a backup key at unlock
  time on any client, and could do more against a browser. The phrase
  and device approval do not pass through a server page in the apps.
  The README says this plainly.
- **In the web chat** the browser counts as a device. Its keys sit in
  the browser's storage for the server's address until the member
  signs out, which wipes them; signing in again with a passkey restores
  them. The encryption is plain JavaScript on the browser's built-in
  cryptography, so there is no module to build. Browser devices are
  labelled as such in the device list.
- In a sealed chat's call, media frames are encrypted with keys taken
  from the current secret, so the forwarding server cannot decode them.
- An admin can turn sealed chats off for the whole server. Retention
  still deletes the stored ciphertext. The export leaves sealed chats
  out entirely.

## 15. Slack import

`churr import slack <export.zip>` is run by the operator.

- Creates channels, messages, threads, reactions, pins and custom emoji
  with their original times.
- Creates an account for each Slack user in an unclaimed state and
  prints one claim link per person. Opening the link and making a
  passkey takes over that account and its history. With company sign-in
  on, an account is claimed automatically when the provider's email
  matches the one in the export; the email is kept only as a hash and
  only until then.
- Slack exports hold links to files, not the files. The importer
  downloads them while the links still work. This is the one time the
  operator's machine contacts Slack.
- A standard Slack export has public channels only. Private channels and
  direct messages are imported when the export contains them. Slack
  direct messages become ad-hoc channels.
- Slack's message markup is converted to Markdown. Layout blocks become
  text.

## 16. Clients

| Client   | Stack                                | Role                          |
|----------|--------------------------------------|-------------------------------|
| Apps     | Flutter, one codebase in `app/` for Android, iPhone, macOS, Windows and Linux | The full product: several servers side by side, calls, sealed chats, notifications, themes |
| Web chat | Plain HTML, CSS and JavaScript modules, embedded, at `/chat/` | Channels (named and ad-hoc), threads, files, search, reactions, commands, calls and sealed chats, while the page is open |
| Web pages| The same, at `/`                     | Sign-in, OAuth approval for the apps, account, admin console, first-run setup |

- The admin console exists only on the web. The apps link to it.
- The apps keep a local database of recent messages per server, so they
  open instantly and work through short disconnections.
- **Themes.** Dark (the default), light, and a high-distinction pair are
  built in, in the apps, the web chat and the web pages alike, following
  the approved style guide (`docs/style-guide.html`, `docs/STYLE.md`).
  A theme is a named set of colours for dark or light: three ground
  steps, ink, dim text and four role colours. Everything else is derived
  by the client, and shape, type, layout and motion are not themeable.
  Contrast is enforced: a theme with text below 4.5:1, or control edges
  and icons below 3:1, cannot be saved, shared or applied. The editor
  is in the apps and the web chat. Themes are stored per account on a
  server, can be published to its theme library, and an admin sets the
  server's default dark and light themes. A theme link in a message is
  drawn as a theme card, and a theme travels between servers as a small
  `.churrtheme` file. Which theme is in use is an app preference on the
  device: one for the whole app, or each server's default.
- **Sidebar layouts**, chosen per person in the app: a merged tree
  (each server an expandable heading with its channels beneath), a
  server rail (one server's channels at a time), or hybrid (the
  person's own groups in their own order, holding conversations from
  any server by drag and drop). The choice and the hybrid arrangement
  are kept on the device. The web chat is one server, so it has the
  rail layout only.
- **Across servers.** Servers stay independent, and the app joins them
  up: one notifications list, one search and one quick switcher across
  every signed-in server; status and do not disturb set once and
  applied to each server unless one is opted out; quiet hours per
  server; mute a whole server for a time; profile changes applied to
  the servers the person picks. None of this needs anything new from a
  server.
- **Distribution.** App Store and Play Store builds use the relay. The
  F-Droid build carries no Google libraries and uses UnifiedPush or the
  foreground service. Desktop builds are downloads attached to releases.

## 17. Security and privacy

**Outbound connections**

| Connection           | Trigger                              | Default | Switch |
|----------------------|--------------------------------------|---------|--------|
| Certificate renewal  | Running without a proxy              | On in that mode | `-behind-proxy` |
| Push relay           | Wake-up for a phone                  | Off until a relay address is set | Admin setting |
| UnifiedPush          | Wake-up sent to an address an Android phone registered | Off | Admin setting |
| Link previews        | A posted link                        | Off     | Admin setting |
| Object storage       | Reading and writing uploaded files   | Off     | Configured or not |
| Identity provider    | Company sign-in: discovery, keys, token exchange | Off | Configured or not |
| Peers                | A link or hub the owner accepted     | Off     | Remove the peer |

First-run setup shows this table to the owner with the switches. The push relay is set up in the admin console, since it needs an address first.

**Rules**

- `perm` is the only authority (section 4), and the tests in the brief's
  definition of done exercise it for members, guests, bots and other
  servers.
- Message content cannot inject markup: clients build nodes from
  Markdown and never parse HTML.
- Uploads cannot run as pages (section 5).
- Web sessions use an HttpOnly, SameSite=Strict cookie. Requests that
  change anything and carry a cookie must come from the server's own
  origin, and so must the WebSocket.
- A strict Content-Security-Policy on every page; no third-party
  scripts, fonts or analytics.
- Rate limits on sign-up, sign-in, recovery, posting, uploads, webhook
  posts, preview fetches and events from each peer.
- Every admin and owner action goes to the audit log: role changes,
  removals, deleting another person's message, settings, bots and
  webhooks, sign-in policy, peers and streams, exports.
- SAML handling accepts only assertions signed by the configured
  provider, checks audience, recipient and time window, and refuses
  responses that were not asked for.

**What is not protected**

- Unsealed content is not encrypted at rest. The operator can read it,
  and so can anyone who takes the disk. Use disk encryption.
- A linked server keeps its copy of a shared channel. Deletes and
  retention on one server are requests to the others.
- A hub and the relay see traffic patterns (section 11, section 12).
- Sealed chats hide content from the server and not who talks
  to whom.

## 18. Deployment

- **Ports:** 443 (HTTPS), 80 (certificate challenges and redirect), and
  one UDP and TCP port for call media. With a reverse proxy: one
  localhost HTTP port for the proxy, plus the media port published
  directly.
- **TLS:** built-in ACME for the server's host name, or `-behind-proxy`.
- **Configuration:** flags or environment for listeners, host name, data
  directory and database. Everything else is set in the admin console
  and stored in the database.
- **Backup:** copy the data directory, or `churr backup` for a
  consistent snapshot while running. With PostgreSQL, `pg_dump` plus the
  data directory.
- **Upgrade:** replace the binary; migrations run at start.
- **Commands:** `serve`, `backup`, `recovery`, `owner`, `migrate-db`,
  `import slack`, `export`, `version`. An owner's export includes
  private and ad-hoc channels and leaves sealed chats out.

## 19. Built in, so no plugin is needed

These are the things people most often add an app or bot for in Slack
and Discord. Churr does them itself. They use the parts already
described: forms, groups, levels and scheduled posts.

| Need                         | How Churr does it                                  |
|------------------------------|----------------------------------------------------|
| Polls, votes                 | `/poll`: a form with the tally action              |
| Sign-ups, RSVPs, approvals, surveys | `/signup`, `/rsvp`, `/approve`, `/form`: form presets |
| Stand-ups and check-ins      | A recurring form: a channel manager sets questions and a schedule, and each answer is posted in that day's thread |
| Reminders                    | `/remind` yourself, a person or a channel, once or repeating, or "remind me about this message". A reminder to yourself arrives in your own one-person channel |
| Send later                   | Any message can be scheduled, then listed, edited or cancelled before it goes |
| Saved replies and custom commands | Admins define `/name` commands that post preset text; each person can save their own |
| Welcome and starting channels | `@everyone` channels, a server welcome text shown on first sign-in, and a welcome text per channel shown on joining |
| Pick-your-own roles          | A form button that joins or leaves an open group   |
| Support tickets              | A form with the open-a-channel action: it makes a private channel for the person and a chosen group, with the answers as its first message. Archiving the channel closes the ticket |
| Moderation                   | See below                                          |
| Saving messages              | Pins for the channel, bookmarks for yourself       |
| Alerts from other tools      | Webhooks that accept Slack, Discord and GitHub payloads, shown as cards |
| Calendar status              | A server integration sets in-a-meeting and out-of-office |
| Calls                        | Built in (section 13)                              |

**Moderation**

- **Time out.** A channel's moderator or manager can stop an account posting and
  reacting in that channel for a set time; an admin can do it for the
  whole server. The account can still read.
- **Slow mode.** A channel setting: each person may post once per
  chosen interval. Moderators and managers are exempt.
- **Word filter.** An admin's list of words and patterns. A match
  either blocks the message and tells its author, or lets it through
  and flags it for review.
- **Reports.** Anyone can report a message with a reason. In a private
  or ad-hoc channel the report goes to that channel's moderators and
  managers only. In a public channel it goes to them and to the admins,
  for what is the instance's to do. Whoever handles it can dismiss it,
  delete the message (an admin's delete is a purge), or time the author
  out.
- **Remove and ban.** Removing an account is described in section 4. A
  ban also stops the same person registering again, by their company
  sign-in identity where there is one.
- Every one of these is written to the audit log.

**Left to bots**, because they need an outside service or are a matter
of taste: GIF search, music, games and economies, experience levels,
LLM assistants, translation, and two-way sync with tools such as Jira
or Trello. `examples/` in the repository holds small bots that show
how.

## 20. Documents and tests

`docs/` holds the detail this document summarises.

| File                       | Contents                                           |
|----------------------------|----------------------------------------------------|
| `docs/CONVENTIONS.md`      | Identifiers, times, names, errors, paging, limits  |
| `docs/MESSAGE-FORMAT.md`   | Churr Markdown, entities, files, cards, forms      |
| `docs/schema.sql`, `docs/SCHEMA.md` | Every table and column, for both databases |
| `docs/openapi.yaml`, `docs/API.md` | Every endpoint; a guide to using them       |
| `docs/EVENTS.md`           | The live connection and every event                |
| `docs/PERMISSIONS.md`      | The full rule table that `perm` implements         |
| `docs/AUTH.md`             | Passkeys, recovery, OIDC, SAML, second factors, app sign-in, tokens |
| `docs/BOTS.md`             | Bots, webhooks and their payloads, commands, integrations |
| `docs/FEATURES.md`         | The built-in features of section 19 in detail      |
| `docs/LINK-PROTOCOL.md`    | Peers, streams, envelopes, the hub                 |
| `docs/PUSH.md`             | Device registration, the relay, the status endpoint |
| `docs/CALLS.md`            | Call signalling and media                          |
| `docs/SEALED.md`           | Sealed chats, device keys, key backup              |
| `docs/CLIENTS.md`          | The apps and the web: screens, local storage, themes |
| `docs/OPERATIONS.md`       | Configuration, deployment, backup, import, export  |
| `docs/STRUCTURE.md`        | Repository layout and Go packages                  |

Tests: `go test ./...` drives the API end to end with a virtual passkey
authenticator, as Codemail does, on both databases. Link tests start two
servers and a hub in one process. A browser test drives the web chat.
App tests run in Flutter's own harness.

## 21. Risks and open questions

**Risks**

- **Size.** This is several products' worth of work built as one, with
  no stages. Nothing is usable until a great deal exists, and a design
  mistake in the foundations is found late. The owner has chosen this.
- **Sealed chats** are the hardest part to get right. A flaw is
  worse than not having the feature, because people will rely on it.
- **Sealed chats use Churr's own scheme, not a published protocol.**
  It is built from standard parts in a well-understood pattern, but
  nobody outside has analysed this exact design, and it is implemented
  three times (Dart, JavaScript, a little Swift). Shared test vectors
  keep the three in step. Outside cryptographic review is needed before
  anyone relies on it.
- **Sealed history trades away some protection.** One secret covers
  every message until the next key change, and the backup keeps old
  secrets so new devices can read old messages. Whoever gets a secret
  or the backup key can read what it covers.
- **Two views of one channel.** People in the same channel see threads
  differently and their unread counts differ. The owner has accepted
  this: the view is a personal preference with a per-channel override,
  and nothing sets it for other people. The thread design is untried, so
  it should be tried with a real group as soon as the web chat can show
  it.
- **The time reference is a single point of pause.** If a hub or an
  owner server is down, its shared channels stop moving between servers
  until it returns. Local posting and reading continue, and what was
  posted takes its place by its stamp when the link returns. Between
  hubs joined by trunks, each side of a split keeps delivering among its
  own servers.
- **A hub operator holds real power over the channels the hub owns.**
  They can see each channel's name, description, moderators and member
  servers, admit and remove servers, and appoint moderators. They cannot
  read content, but could admit a server they control, which would get
  the key from then on; every member server sees that change.
- **A linked server can keep what it was sent.** No protocol fixes that.
- **Phones depend on the relay and on two app stores.** If the relay is
  down or an app is pulled, phone notifications stop.
- **Calls on a small server.** Forwarding video costs bandwidth and CPU.
  The 1 CPU target may only hold for audio.
- **SAML** has a history of signature-handling flaws in every language.
  It uses a maintained library and the narrowest configuration.
- **Two databases** double the storage test matrix for every feature.

**Open questions**

- The relay's address and the apps' bundle identifiers.
- How many members' passkeys support PRF in practice. Support was
  confirmed for Apple's and Google's passkey stores in November 2026 and
  was partial or unconfirmed for Firefox, third-party managers, Windows
  Hello and hardware keys. Test early.
- Where exactly the browser keeps sealed keys. The owner asked for local
  storage; IndexedDB is the likely choice because the key state is
  binary and grows. Either is wiped on sign-out.
- Whether a hub should be able to hold a stream's owner role, so a
  shared channel survives its founding server leaving. In this design
  ownership can be handed to another server but not to the hub.
- Whether the apps should offer search inside a sealed chat from their
  own local copy later. In version 1 a sealed chat cannot be searched.

# Roadmap

What Churr does not do yet and is planned, as the owner decided it. Each
entry says what is decided and what still needs a design. Nothing here is
built unless the entry says so. What Churr does today is in
`docs/FEATURES.md`.

## 1. Live rooms: voice, meetings and streams

Voice channels with stage mode, meetings, streams, and captions. The
decisions are in `docs/ROADMAP-LIVE.md`. A design exists and waits for the
owner's approval before anything is built.

## 2. Shared documents

A document is a thing of its own. It does not belong to a channel. It has an
owner and its own access list.

- **Edited live** by several people at once, in a meeting or outside one.
- **Kinds**, each added without changing the others:
  - **notes**: a shared editor in Churr's own markdown, with mentions,
    instants and the other message objects;
  - **whiteboards**, in the manner of Miro or FigJam;
  - **task boards** (Kanban), see section 3;
  - **timelines**, and others later.
- **Posting a document in a channel** posts a card that links to it. People
  the document allows see a preview; others see "a document you can't see".
- **In a meeting a presenter shares a document instead of a screen.** The
  room shows the live document itself, drawn on each person's device, not a
  video of someone editing it.
- **Meeting notes** are a document made for the meeting.

Needs its own design before anything is built: the method of live editing,
access lists, quotas, and what a recording keeps of a presented document.

## 3. Task boards and tickets

- **A Kanban task tracker**: boards of cards in columns, as a kind of shared
  document, usable in meetings like any other document.
- **Support tickets and bug reports** are the same system, not a second one.
  A ticket is a card. A support queue or a bug list is a view or mode of a
  board.
- **Opened by a form.** Someone reports an issue or asks for support by
  answering a form; the answer becomes a ticket on the board and stays
  linked to it.

- **Every card has a thread** for its discussion.
- **A thread can become a channel.** A ticket can be created with a channel
  from the start (as a form's ticket channel is today, `docs/FEATURES.md`),
  or promoted to one later.
- **A thread can be linked into the sidebar** without becoming a channel.

Needs its own design: card fields and states, views (board, queue, list),
who may see and move a ticket, the link between a form's answer, the card
and its discussion, and reference patterns pointing at tickets.

### Detached threads and meetings

The question the design must answer first: a thread today hangs off a
message in a channel and takes its access from that channel. A card's
thread, and a meeting that is not held in a channel, have no such message.
The proposal is one idea for both: a thread may be attached to a message, a
card or a meeting, or to nothing, and takes its access from what it is
attached to (the channel, the board's access list, the meeting's invitees,
or its own list of people). A detached meeting is then a meeting whose chat
is a detached thread.

## 4. One simple role model

Six roles, and nothing finer. Anything more advanced is a job for a bot.

| Role | Where | What it is |
| --- | --- | --- |
| Owner | an instance or a hub | owns it |
| Admin | an instance or a hub | runs it: instance-level bans, the admin panel; slightly less than the owner |
| Manager | a channel | like the channel's owner, whichever instance they are from |
| Moderator | a channel | keeps the peace: lets people in, removes them, removes content, slows or locks a channel |
| Member | a channel | takes part |
| Guest | a channel or a meeting | a visitor |

- States such as "voiced", read-only and muted are flags on a person, not
  roles.
- A group has no permissions. A role may be granted to a group, which gives
  that role to its members.

The permission system as built is being compared with this model; the
changes follow that comparison.

## 5. Channels a hub owns

A channel moved to a hub transfers to the hub. The hub owns it and is
trusted as its owner; the instances the hub joins to it are trusted too.
The channel keeps its managers, permissions and members. A private channel
stays private on a hub and can never be listed or opened. Being built.

## 6. Smaller planned items

- A crash-style game on a "game card": a message that updates live, with
  buttons.
- Dealing cards: a deck held for a table, with private hands.
- More from the owner's utility apps: an inline maths object, base 36, 62
  and 64 in the number popup.

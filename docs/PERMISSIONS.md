# Permissions

The complete rule set that the server's `perm` package implements.
Handlers, search, file downloads, the live fan-out and the link code
all ask `perm`, and nothing else decides. `CONVENTIONS.md` gives the
wording and the error codes used here. `DESIGN.md` section 4 is the
summary this document expands.

Where the design left a gap, the simplest rule consistent with the
rest was chosen. Those choices are marked "(decision Pn)" and listed
in section 16.

In the tables, **Y** means allowed and **–** means refused. A number
in brackets points at a note under the table.

## 1. How `perm` is asked

Every question has the same shape: *may this actor do this action to
this target, now?* The answer is "yes" or one refusal code.

`perm` reads only stored rows and the clock. It keeps no state of its
own, so the same question always has the same answer on both
databases.

Checks run in this order and the first failure is the answer:

| Step | Check                                                  | Refusal            |
|------|--------------------------------------------------------|--------------------|
| 1    | The credential is valid and the account is not removed | `unauthenticated`  |
| 2    | The actor can see that the target exists (section 4)   | `not_found`        |
| 3    | The feature is switched on for the server              | `feature_off`      |
| 4    | The actor's role and level allow the action            | `forbidden`        |
| 5    | The conversation is not archived (section 10)          | `archived`         |
| 6    | The channel is not locked, for actions that bring someone in (section 10.1) | `locked` |
| 7    | The actor is not timed out (section 11)                | `timed_out`        |

Slow mode (`slow_mode`), the word filter (`blocked_by_filter`) and
rate limits (`rate_limited`) are applied after `perm` has said yes.
`perm` does answer one question for them: whether the actor is exempt
from slow mode (section 7).

Step 2 matters. A private channel, an ad-hoc channel, a sealed chat,
or a message or file inside one, does not exist as far as an outsider
can tell. The answer is `not_found`, never `forbidden`.

One refusal comes from elsewhere and is not `perm`'s: a change that
needs a recent passkey or authenticator check answers
`fresh_check_required` (`AUTH.md`).

Channel names are not unique, so no request about a channel is ever
refused because of its name being in use, and `name_taken` is never
returned for one. Creating or renaming a channel therefore cannot
show that a private channel of that name exists.

A channel is linked in message text by its identifier, written
`<#conversation-id>`. The link becomes a `conversation` entity only
if the author can see that channel; otherwise it stays as written
and the author learns nothing from it. A reader who cannot see a
linked channel is given the entity without the channel's name and
is shown "private channel". Plain characters such as `#games` are
never resolved.

## 2. Inputs

### 2.1 The actor

| Actor             | What it is                                              |
|-------------------|---------------------------------------------------------|
| `owner`           | The one owner. An admin with a few extra powers.        |
| `admin`           | "Admin" below includes the owner unless it says not.    |
| `member`          | An ordinary person with the server role `member`.       |
| `guest`           | A person invited into chosen conversations only.        |
| `bot`             | An account marked as a bot, acting with a bot token.    |
| webhook           | A bot account with `bots.webhook_only` set, acting with a webhook secret. |
| `remote`          | The local shadow of an account on another server. It never signs in here. Its actions arrive as signed events from its home server. |
| integration       | A server integration token. Not an account.             |
| nobody            | No credential, or one that is not valid.                |

A personal token acts as its account, with that account's role and
levels. It cannot manage sign-in (passkeys, tokens, sessions,
recovery); `AUTH.md` has that list (decision P1). Where company
sign-in is required, a personal token stops working 30 days after
the account's last company sign-in.

### 2.2 Account state

| State        | Stored as                                  | Effect                |
|--------------|--------------------------------------------|-----------------------|
| Active       | `removed_at` is null, `claimed_at` is set  | The matrix applies.   |
| Unclaimed    | `claimed_at` is null (imported, section 12)| Cannot sign in. Holds memberships and authorship. |
| Timed out    | A row in `timeouts` that has not ended or been lifted, for one conversation or the whole server | Section 11. |
| Removed      | `removed_at` is set                        | Section 12.           |
| Banned       | Removed, plus a row in `bans` not lifted   | Section 12.           |
| Blocked by X | A row in `blocks` with `account_id` X and `blocked_id` the actor | Section 13. |

### 2.3 Conversation kind

| Kind in this document | `conversations` columns                          |
|-----------------------|--------------------------------------------------|
| Public channel        | `kind = channel`, `private = false`, `name` set  |
| Private channel       | `kind = channel`, `private = true`, `name` set   |
| Ad-hoc channel        | `kind = channel`, `private = true`, `name` null  |
| Sealed chat           | `kind = sealed`                                  |

An ad-hoc channel is always private. A public channel can be made
private; a private channel can never be made public, because the
people in it were told it was private (section 7.1). Naming an
ad-hoc channel makes it a private channel and changes nothing else.

The server also keeps one "library" conversation that holds avatar
and emoji files. It has no members, never appears in any list, and
every question about it as a conversation is answered `not_found`.

### 2.4 Membership and level

A row in `members` makes an account a member. `members.level` is
`reader`, `member` or `manager`, and is always the highest of the
level held directly and the levels given by groups (section 9). No
row means "not a member".

**Acts as manager.** An account acts as manager of a channel when:

- its level there is `manager`; or
- its role is owner or admin and the channel is public, whether or
  not it is a member; or
- its role is owner or admin and it is a member of the channel at any
  level (decision P3).

Acting as manager gives the management actions in section 7. It does
not give membership. An admin who has not joined a public channel
can pin, rename and moderate there, and cannot post, react or receive
its live events until they join.

One action ignores "acts as manager" and looks at the stored level
only: handling reports in private and ad-hoc channels (section 8.4).

### 2.5 Conversation state

| State    | Stored as                          | Effect      |
|----------|------------------------------------|-------------|
| Archived | `conversations.archived_at` is set | Section 10. |
| Locked   | `conversations.locked_at` is set   | Section 10.1. Private and ad-hoc channels only. |
| Shared   | `conversations.stream_id` is set   | Section 14. |

## 3. Words used in the matrix

- **See**: know the conversation exists; get its name, topic,
  section, kind and settings.
- **Read**: fetch its messages and their reactions, checklist state
  and forms; fetch a message's thread; download its files and
  thumbnails; list its pins; open message and checklist history.
- **Search**: have its messages appear in search results.
- **Live events**: receive the conversation's events on a live
  connection, and catch up on them after reconnecting.

## 4. Seeing, reading, searching and live events

### 4.1 By role and membership

| Actor                                   | Public channel | Private channel | Ad-hoc channel | Sealed chat |
|-----------------------------------------|----------------|-----------------|----------------|-------------|
| A member of it, any role, any level     | See, read, search, live events | See, read, search, live events | See, read, search, live events | See, read (ciphertext), live events. No search. |
| Owner or admin, not a member            | See, read, search. No live events. | See [1]. Nothing else. | Nothing | Nothing |
| `member`, not a member of it            | See, read, search. No live events. | Nothing [2] | Nothing | Nothing |
| `guest`, `bot`, webhook, `remote`, not a member | Nothing | Nothing | Nothing | Nothing |
| Integration, nobody                     | Nothing        | Nothing         | Nothing        | Nothing     |

[1] The admin view of a private channel is exactly: its name, the
list of its members, and which of them are managers. Not the topic,
the welcome text, pins, messages, files, counts of messages, or the
time of the last message. Ad-hoc channels and sealed chats do not
appear in any admin list. This view, and the four things an admin
can do from it (appoint a manager, archive or unarchive, lock, set
retention), are operations of the admin console. Every ordinary
conversation operation treats the admin as any other outsider and
answers `not_found` (decision P22).

[2] One exception: a member of the channel a thread was moved out of
can see and join the channel made from that thread, even if it is
private (section 6, note 5).

"Nothing" means every request about the conversation, its messages
and its files answers `not_found`.

### 4.2 The reading actions one by one

| Action                              | Member of the conversation | Non-member who can read it (4.1) | Admin seeing a private channel they are not in | Anyone else |
|-------------------------------------|----|----|----|----|
| See it in their conversation list   | Y  | –  | –  | –  |
| See it in the channel browser       | Y  | Y  | –  | –  |
| See it in the admin console         | admins | admins | Y | – |
| List its members and their levels   | Y  | Y  | Y  | –  |
| Read messages, threads, pins        | Y  | Y  | –  | –  |
| Download files and thumbnails       | Y  | Y  | –  | –  |
| Open message history                | Y [1] | Y [1] | – | – |
| Open checklist item history         | Y  | Y  | –  | –  |
| Search it                           | Y [2] | Y | – | –  |
| Receive live events, catch up       | Y  | –  | –  | –  |
| Mark read, set mode, mute, show, star | Y | –  | –  | –  |
| Bookmark a message in it            | Y  | Y  | –  | –  |
| Report a message in it              | Y [3] | Y | – | –  |

[1] Unless a purge has been asked for and the history is hidden
(`messages.history_hidden_at` is set). Then only admins who can read
the conversation may open it, and not the author (section 8.2).

[2] Not in a sealed chat. Search never returns anything from one.

[3] Not in a sealed chat (decision P4). The server cannot read the
message and the chat has no manager. A person there can block.

**Files.** A file follows the conversation it was uploaded to. A file
that an edit or a delete detached from its message follows the rule
for message history. An upload not yet attached to a message can be
fetched only by its uploader. Avatar and emoji files can be fetched
by any signed-in account (decision P5).

**Search** is joined to this table. For each asker the searchable set
is: the channels they are a member of, plus, for owner, admin and
`member` roles, every public channel. Guests and bots search only
what they are in. Deleted messages, message history and sealed chats
are never searched.

**Live events.** Fan-out is by membership only. A non-member reading
a public channel gets no events from it and fetches when they look.
An account cannot subscribe to anything. Events whose content differs
by reader (a form's tally and voters, hidden history) are made per
recipient under the same rules as a read.

### 4.3 Seeing accounts and groups

| Actor                  | Sees                                                    |
|------------------------|---------------------------------------------------------|
| Owner, admin, `member` | Every local account, including guests and bots. A remote account only while they share a conversation with it. |
| `guest`, `bot`         | Only accounts that are in a conversation with them.     |
| Webhook, integration   | Nothing. An integration can look an account up by username or sign-in identifier to set its availability, and gets back nothing else. |

- Presence, status, availability and the do not disturb mark go only
  to accounts that share a conversation with the person.
- A removed account stays visible as the author of its messages,
  shown as a former member.
- Owner, admin and `member` roles see every group and who is in it.
  A guest or bot sees only the groups it is in and the groups added
  to its conversations (decision P6).

## 5. Taking part in a conversation

These actions need membership. The column is the actor's level. An
admin who is not a member has none of them, in any kind of
conversation.

| Action                                   | Not a member | `reader` | `member` | `manager` | Notes |
|------------------------------------------|----|----|----|----|-------|
| React, remove own reaction               | –  | Y  | Y  | Y  | [1]   |
| Answer a form, change or withdraw own answer, press a form button | – | Y | Y | Y | [1] [2] |
| Add an option to a choice field          | –  | Y  | Y  | Y  | Only if the form allows it |
| Mark read                                | –  | Y  | Y  | Y  |       |
| Post a top-level message                 | –  | –  | Y  | Y  |       |
| Reply in a thread                        | –  | –  | Y  | Y  |       |
| Upload a file                            | –  | –  | Y  | Y  |       |
| Tick or untick a checklist item          | –  | –  | Y  | Y  |       |
| Send a typing notice                     | –  | –  | Y  | Y  |       |
| Post a form (`tally`, `collect`, `thread` actions) | – | – | Y | Y | [3] |
| Post a form with the `channel` action    | –  | –  | –  | Y  | [3]   |
| Post a form with the `group` action      | –  | –  | –  | –  | Admins who are members at `member` or above [3] |
| Post a form with the `bot` action, post cards | – | – | bots | bots | Section 15 |
| Set up a recurring form                  | –  | –  | –  | Y  |       |
| Mention `@channel`, `@here`, `@everyone` | –  | –  | Y [4] | Y |     |
| Push own message through one person's do not disturb | – | – | Y | Y | Not bots or webhooks [8] |
| Send a command to a bot in the conversation | – | Y | Y | Y | Not while timed out |
| Run a saved reply or custom command      | –  | –  | Y  | Y  | It posts a message |
| Schedule a message for later             | –  | –  | Y  | Y  | Checked again when it is sent |
| Edit own message                         | –  | –  | Y  | Y  | [5]   |
| Delete own message                       | Y [6] | Y | Y | Y  |       |
| Ask for a purge of own message           | Y [6] | Y | Y | Y  | Section 8.2 |
| Close own form                           | Y [6] | Y | Y | Y  |       |
| Remove a link preview from own message   | –  | –  | Y  | Y  |       |
| Start or join a call                     | –  | –  | Y  | Y  | `feature_off` if calls are switched off |
| Follow or ignore a thread                | –  | Y  | Y  | Y  |       |
| Move own thread out to a channel         | –  | –  | Y  | Y  | [7]   |
| Leave                                    | –  | Y  | Y  | Y  | Section 6 |

[1] "Anyone who can read the channel" in the design means anyone in
it. A person reading a public channel they have not joined cannot
react or answer; they join first (decision P7).

[2] Bots and webhooks can never answer a form. A form may carry
`answerer_group_id`; then only members of that group may answer or
press, and everyone else in the conversation is refused with
`forbidden`. Only an account acting as manager of the conversation
may post a form that carries it.

[3] Not in a sealed chat, which carries no forms and no cards. In a
shared conversation only `tally` forms are allowed, named or
anonymous (section 14).

[4] Only accounts acting as manager (the channel's managers, and an
admin) may use them when the server setting
`mentions.broadcast_managers_only` is on, when the channel's own
answer (`broad_mentions`) is `managers`, or when the channel has no
answer of its own and has more members than the server setting
`mentions.broad_mention_limit` (default 250, 0 for no limit). A
channel whose answer is `anyone` is not held to that limit; it is
still held to the server's managers-only switch. For anyone else the
words stay plain text and notify nobody, and the answer to the post
says so. Guests, bots and webhooks are held to the same rule as
members. `@admins` is not a broad mention (ruling R64).

[5] Only while the edit window set by the admin is open. Bots are not
held to the edit window. Nobody can edit another account's message,
whatever their role. In a sealed chat a message is never edited in
place: an edit is a separate sealed item of kind `edit` that the
author's device posts as a new message (`SEALED.md` section 8.3), and
the edit operation refuses a sealed message with `invalid_request`.

[6] The author keeps these three rights for as long as they can still
read the conversation, even after leaving it or being lowered to
`reader`. Once they cannot read it they cannot reach the message
(decision P8).

[7] The thread's starter is the author of its parent. They must also
hold a role that may create channels (section 9.1). Refused in shared
channels, sealed chats and archived channels.

[8] The request is accepted whenever the caller is the message's
author and a person. It never says whether the other person was
reached: if they have the conversation on `nothing` or muted, blocked
the sender, turned the feature off or cannot read the conversation,
the server quietly does nothing and answers the same way. No rule in
this document may reveal those settings to a sender.

In a sealed chat both people are `member` and nobody is a manager.

## 6. Membership

| Action                               | Public channel | Private channel | Ad-hoc channel | Sealed chat |
|--------------------------------------|----------------|-----------------|----------------|-------------|
| Join by oneself                      | Owner, admin, `member` [1] | – [5] | – | – |
| Add a local account                  | Acts as manager | Acts as manager | Any member [2] | – |
| Add a bot                            | Acts as manager | Acts as manager | Any member | – |
| Add a remote account                 | – [3]          | – [3]           | [3]            | –           |
| Add a group at a level               | Acts as manager [4] | Acts as manager [4] | – | – |
| Remove a group, change its level     | Acts as manager | Acts as manager | –             | –           |
| Remove another account               | Acts as manager [6] | Acts as manager [6] | Acts as manager [6] | – |
| Ban an account from the channel, lift a ban, see the bans | Acts as manager (6.1) | Acts as manager (6.1) | Acts as manager (6.1) | – |
| Ask to be added (knock)              | – (join instead) | Knock channel: owner, admin, `member` (6.2) | – | – |
| See and answer the requests to be added | – | Knock channel: acts as manager (6.2) | – | – |
| Set another member's level           | Acts as manager [7] | Acts as manager [7] | Acts as manager [7] | – |
| Appoint a manager from outside       | (admins act as manager) | Admins [8] | –     | –           |
| Set the level new members get        | Acts as manager | Acts as manager | –             | –           |
| Leave                                | Any member [9] | Any member [9]  | Any member     | – [10]      |

[1] Guests, bots and remote accounts never join by themselves; they
are added. A join gives the channel's default level.

[2] The adder must be able to see the account they add (4.3). In a
named channel the account must not have blocked them (section 13); in
an ad-hoc channel someone who has blocked them is shown to them as
added and is not added (section 13, ruling R77). An ad-hoc channel
holds at most 50 members; past that it must be named first.

[3] A remote account is never added to a channel that is not shared.
In a shared channel it arrives through its home server's join event.
A local member may start an ad-hoc channel or sealed chat with a
remote account only while they share a shared channel, and a remote
account cannot be added later to an ad-hoc channel that began local
(section 14).

[4] The actor must hold the owner, admin or `member` role; guests
and bots cannot add groups even where they are managers. Groups are
not added to ad-hoc channels, whose identity is their member list
(decision P9).

[5] A private channel made by moving a thread can be joined by any
member of the channel the thread was moved out of, at the default
level, if their role is owner, admin or `member`.

[6] Not an account that is in the channel through a group; remove the
group, or take the person out of the group. A manager cannot remove
an admin or the owner. An admin cannot remove the owner.

[7] Sets the level held directly. The effective level stays the
highest of direct and group levels. A channel's last manager cannot
lower their own level (`conflict`).

[8] In a private channel the admin is not in, an admin may raise one
of its existing members to `manager`, and nothing else: not lower a
level, not add or remove anyone, not add themselves.

[9] Refused with `conflict` while the account is in the channel
through a group. It can hide the channel from its sidebar or set
it to `nothing`, or leave the group if it is open.

[10] A sealed chat cannot be left. It can be hidden from the sidebar
(decision P10).

Every "add" and "join" in this table is refused with `locked` in a
locked channel (section 10.1). Leaving and removing still work
there, and nobody is in a locked channel through a group, so notes 6
and 9 do not arise.

**Last manager.** When a channel's last manager leaves or is removed
from the server, its longest-standing member becomes manager. When
the last member of a private or ad-hoc channel leaves, the channel
and everything in it is deleted, because nobody could read it again
(ruling R21). A public channel with no members stays and can be
joined.

### 6.1 Channel bans

A channel ban (ruling R72, FEATURES.md 19.1) is decided by the same
rule as removing someone: an account acting as manager of the
channel, named or ad-hoc, never in a sealed chat (`forbidden`), and
not while timed out there. A manager cannot ban an admin; an admin
may ban another admin, as they may remove one; nobody bans the owner
or themselves. Guests and bots that are managers may ban, as they may
remove. A channel shared with other servers, or owned by a hub, takes
no new bans (`conflict`); someone on another server is never banned
(`forbidden`). Lifting a ban and seeing the bans are for those who
may ban, and for an admin through the console in any named channel
(section 4.1 note 1).

While a ban is in force, for the account banned:

- the channel is not seen and not read, public or not (`not_found`
  for everything about it, as for a conversation it may not know);
- it cannot join, cannot be added (`forbidden`), and no group brings
  it in;
- it receives no live events of the channel.

A ban takes the account out and a banned account is never a member,
so perm reads the ban only for an actor outside the channel. Nothing
is inherited: a ban on a parent says nothing about its sub-channels
(7.2). The admin console's view of the channel (4.1 note 1) is not
affected.

### 6.2 Knock channels and knocks

A knock channel (rulings R74, R79, FEATURES.md 12.5) is a private named
channel that owner, admin and member accounts outside it **see** (4.1:
it exists for them, with its name, topic, member count and managers)
and do not **read**: reading, searching its messages, listing its
members, receiving its live events and joining are refused as for any
private channel (`not_found`, or `forbidden` for joining). Guests and
bots outside it do not see it. A channel ban hides it, as it hides a
public channel. Members read it as any member reads.

Only a knock channel takes requests (`knock`): a private one cannot be
seen (`not_found`), a public one is joined (`conflict`), and so is one
the actor is in. Owner, admin and member accounts may knock, ten an hour
(`rate_limited`). The requests waiting are seen, and added or declined,
by those acting as its manager, in a named channel that is not archived
or locked and while they are not timed out; an admin outside a knock
channel does not act as its manager. Adding is the ordinary add (section
6), with all its refusals. The one who knocked is told the outcome and
the channel's name, never who answered.

Changing the access (section 7.1) is for those acting as manager:
public to knock and back, either to private; a private channel never
becomes public or knock, for anyone.

## 7. Managing a conversation

"Acts as manager" is defined in 2.4. The second column is the one
case where an admin has a power without being able to read.

| Action                                    | Acts as manager | Admin, private channel they are not in | Notes |
|-------------------------------------------|----|----|-------|
| Set the topic                             | Y  | –  |       |
| Set or change the name                    | Y  | –  | Naming an ad-hoc channel makes it a private channel |
| Set the welcome text                      | Y  | –  | Named channels |
| Choose its section                        | Y  | –  | From the sections admins defined |
| Set or clear its parent                   | [4] | Admins | Section 7.2 |
| Pin and unpin a message                   | Y  | –  |       |
| Remove a link preview from any message    | Y  | –  |       |
| Close any form                            | Y  | –  |       |
| Read and download `collect` form answers  | Y, and the form's author | – | Never who answered what on an anonymous form |
| Delete another account's message          | Y  | –  | Section 8.1 |
| Set slow mode; be exempt from it          | Y  | –  | Bots and webhooks are also exempt |
| Define the channel's own reference patterns (`set_reference_patterns`, ruling R73) | Y | – | Named channels. Anyone who can see the channel may list them |
| Set who may use broad mentions in the channel (`broad_mentions`) | Y | – | Section 5 note 4 |
| Time an account out in this channel       | Y  | –  | Section 11 |
| Handle reports                            | [1] | – | Section 8.4 |
| Shorten retention                         | Y  | –  | Named channels only [2] |
| Set any retention, or none                | Admins | Y | Named channels only [2] |
| Archive, unarchive                        | Y  | Y  | Never a sealed chat |
| Make a public channel private             | Y  | –  | Section 7.1. Never the other way |
| Lock a private or ad-hoc channel          | Y  | Y  | Section 10.1. Cannot be undone |
| Approve a webhook request                 | Admins [3] | Admins [3] | Section 15 |
| Move any thread out to a channel          | Y  | –  | Section 5 note 7 |
| Share the channel with other servers      | Owner only | – | The owner must be able to read it. Not a locked channel |

[1] In a public channel: accounts acting as manager, which includes
every admin. In a private or ad-hoc channel: only accounts whose
stored level is `manager`.

[2] Ad-hoc channels and sealed chats always follow the server's
period.

[3] Or the channel's managers, if the admin setting hands approval to
them.

[4] An account acting as manager of the channel that is also a
member of the parent, or any admin.

None of these exist in a sealed chat.

Three more settings of a named channel are its managers' (ruling
R66): `announcement` and `announcement_replies` (an account acting as
manager; refused with `conflict` in a channel shared with other
servers), `lazy_delivery` (an account acting as manager), and who may
post in an announcement channel (`setMemberMayPost`, an account
acting as manager). None exists outside a named channel.

### 7.0 Announcement channels

In a channel whose `announcement` is on, the rows of section 5 for
posting are narrowed, after everything else has been asked (so a
reader is still told that readers cannot post, an archived channel
answers `archived`, and someone timed out `timed_out`):

| Action | Reader | Member | Member with `may_post` | Acting as manager | Approved webhook |
|--------|--------|--------|------------------------|-------------------|------------------|
| Read, search, bookmark, react, answer a poll or form | yes | yes | yes | yes | (a webhook only posts) |
| Post, post a form or cards, schedule, run a saved reply, start a call, broad mention | no | no | yes | yes | post only |
| Reply in a thread, upload | no | only if `announcement_replies` | yes | yes | yes |
| List the members | no | no | no | yes | no |
| Set `may_post`, `announcement`, `announcement_replies` | no | no | no | yes | no |

The role does not matter beyond what it already decides: a guest or a
bot at level `member` is a member here, can be allowed to post, and is
refused otherwise; an admin acts as manager of a public channel and of
a private one they are in. The refusal is `forbidden`. Typing is not
refused: it is accepted and shown to nobody.

### 7.1 Public to private

An account acting as manager of a public channel may make it
private. That is its managers and every admin, since admins act as
manager in public channels. It is written to the audit log.

- It takes effect at once. From that moment section 4 treats the
  channel as private: an account that is not a member can no longer
  see it, read it, search it, download its files or open its
  history, and requests about it answer `not_found`. Nothing is
  kept back for people who used to read it without joining.
- Members keep everything, at the levels they had.
- An admin who is not a member falls back to the admin view of a
  private channel (4.1, note 1). An admin who made the channel
  private from outside therefore loses the ability to read it by
  doing so.
- Mentions stored for non-members are deleted, and their bookmarks
  of its messages stop being shown.
- Purge requests still waiting for an admin are carried out as
  erasures at once, which is what the author would have got in a
  private channel (decision P24). Waiting reports are from then on
  for the channel's managers only.
- Making a private channel public is refused with `forbidden` for
  everyone, the owner included. So is any change to an ad-hoc
  channel's privacy.
- In a shared channel only the owner server may do it (section 14).
- Refused with `archived` in an archived channel.

### 7.2 Sub-channels

A named channel may sit under another named channel
(`conversations.parent_id`).

**Who may set or clear the parent**

| Actor                                                        | May |
|--------------------------------------------------------------|-----|
| An account acting as manager of the sub-channel that is also a member of the parent | Set and clear |
| An account acting as manager of the sub-channel, not a member of the parent | Clear only |
| An admin, for channels the admin can see (public ones, and named private ones through the admin view) | Set and clear |
| Anyone else                                                  | –   |

A parent can be named when a channel is created, by a creator who
is a member of that parent or an admin. A caller who cannot see the
parent gets `not_found`, so naming a private channel as parent
cannot be used to test whether it exists.

**The one-level rule**, refused with `conflict`: giving a parent to
a channel that has sub-channels; choosing as parent a channel that
is itself a sub-channel. Refused with `invalid_value`: an ad-hoc
channel or sealed chat as parent or child; a channel as its own
parent.

**Nothing is inherited.** Every question in this document about a
sub-channel is answered from the sub-channel's own rows: its own
members, levels, privacy, lock, archive state and retention. Being
a member or a manager of the parent gives nothing in a sub-channel,
and the other way round. Joining a parent joins nothing else.
Archiving, locking or making a parent private changes nothing in
its sub-channels.

**Seeing**

- A public sub-channel is seen and read under section 4 like any
  public channel, whoever can or cannot see its parent.
- A private sub-channel is seen only by its members, and by admins
  in the admin view. A member of the parent who is not in it is told
  nothing: it is absent from the parent's list of sub-channels, and
  requests about it answer `not_found`.
- The list of a channel's sub-channels holds only those the caller
  can see.
- A reader who can see a sub-channel and not its parent (a private
  parent they are not in) is not given the `parent_id`.

**When a parent is deleted** (the last member of a private parent
leaves), its sub-channels are not deleted. They become top-level
channels with their own members and content untouched.

A channel made by moving a thread out, and a ticket channel, start
as a sub-channel of the channel they came from, or as its sibling if
that channel is itself a sub-channel. This is done by the server and
needs no right over the parent.

The parent link is local to a server and no linked server is told
of it.

## 8. Messages: delete, purge, history, reports

### 8.1 Delete

| Whose message   | Who may delete it                                            |
|-----------------|--------------------------------------------------------------|
| Own             | The author, while they can read the conversation.            |
| Another's       | An account acting as manager of that channel. In a channel a hub owns, also a moderator of the channel (section 14.1). |
| In a sealed chat| The author only. The server erases the row's ciphertext and file blobs and leaves the marker. |

A webhook cannot delete; it can only post.

### 8.2 Purge

| Case                                         | What happens                              |
|----------------------------------------------|-------------------------------------------|
| Author asks, private or ad-hoc channel       | Purged at once. No request is stored, no admin is told, nothing is written to the audit log. |
| Author asks, public channel                  | History is hidden from everyone but admins at once. A request waits for the admins. |
| Author asks, shared channel                  | Private: purged at once and the purge is sent to the linked servers. Public: the request goes to the admins of the author's own server. |
| Author asks, sealed chat                     | Refused with `forbidden`. The server keeps no history there. |
| Admin decides a waiting request              | Any admin. Outcomes: show again, keep hidden, erase. The author is not told which. |
| Admin purges directly                        | Any message the admin can read: every public channel, and channels they are a member of. Delete and purge are one step. |
| Channel manager who is not an admin          | Cannot purge. Can delete.                 |

- Only the author may ask for a purge of a message. A manager or
  another member cannot ask on their behalf.
- After an admin has chosen "show again", the author cannot ask again
  for the same message unless it has been edited or deleted since
  (`conflict`). Otherwise an author could overrule the admin by
  asking again (decision P12).
- While history is hidden, and after "keep hidden", admins can open
  it and nobody else can.

### 8.3 History

Anyone who can read the conversation can open a message's earlier
versions and the last content of a deleted message, subject to 8.2.
Checklist history has no hidden state.

### 8.4 Reports

| Conversation        | Who may report            | Who is told and may handle it        |
|---------------------|---------------------------|--------------------------------------|
| Public channel      | Anyone who can read it    | Accounts acting as manager, which includes all admins |
| Private channel     | Its members               | Accounts whose level there is `manager`, only |
| Ad-hoc channel      | Its members               | Accounts whose level there is `manager`, only |
| Sealed chat         | Nobody                    | –                                    |

- An admin who is not a manager of a private or ad-hoc channel never
  sees its reports, even as a member of it. They would have to make
  themselves manager first, which the channel can see.
- A handler may dismiss the report, delete the message, or time the
  author out in that channel. Each needs the handler to hold that
  power anyway under this document.
- A report about a handler's own message is hidden from that handler
  if another handler exists.
- Messages flagged by the word filter are reports made by the server
  and follow the same routing.

## 9. The server as a whole

### 9.1 By role

| Action                                         | Owner | Admin | `member` | `guest` | `bot` | `remote` |
|------------------------------------------------|----|----|----|----|----|----|
| Browse public channels and the account list    | Y  | Y  | Y  | [10] | [10] | –  |
| Create a named channel, public or private      | Y  | Y  | Y  | –  | –  | –  |
| Start an ad-hoc channel                        | Y  | Y  | Y  | [1] | [1] | [2] |
| Start a sealed chat                            | Y  | Y  | Y  | [1] | –  | [2] |
| Create an invite for a member                  | Y  | Y  | [3] | – | –  | –  |
| Create an invite for a guest, or one that carries groups or channels | Y | Y | – | – | – | – |
| Revoke an invite                               | Y  | Y  | Own | – | –  | –  |
| Join or leave an open group                    | Y  | Y  | Y  | – [4] | – [4] | – |
| Create, edit and delete groups; set who is in them | Y | Y | – | – | –  | –  |
| Remove, ban, or time out an account server-wide | Y | [5] | – | –  | –  | –  |
| Make a recovery link for an account            | Y  | [5] | – | –  | –  | –  |
| Change an account's role; hand over ownership  | Y  | –  | –  | –  | –  | –  |
| Create and remove bots, make bot tokens        | Y  | Y  | –  | –  | –  | –  |
| Ask for a webhook in a named channel they are in | Y | Y  | Y  | –  | –  | –  |
| Approve, decline or revoke a webhook           | Y  | Y  | [6] | – | –  | –  |
| Create and revoke server integrations          | Y  | Y  | –  | –  | –  | –  |
| Change server settings and sign-in policy      | Y  | Y  | –  | –  | –  | –  |
| Define sections                                | Y  | Y  | –  | –  | –  | –  |
| Define the server's reference patterns (`define_reference_patterns`, ruling R73) | Y | Y | – | – | – | – |
| Add custom emoji                               | Y  | Y  | [7] | – | –  | –  |
| Remove custom emoji                            | Y  | Y  | Own [7] | – | – | –  |
| Community saved replies, community status presets | Y | Y | –  | –  | –  | –  |
| Word filters, server-wide retention, edit policy | Y | Y | –  | –  | –  | –  |
| Decide purge requests                          | Y  | Y  | –  | –  | –  | –  |
| Read the audit log                             | Y  | Y  | –  | –  | –  | –  |
| Send or clear the server broadcast (ruling R74) | Y | Y  | –  | –  | –  | –  |
| Run the export                                 | Y  | –  | –  | –  | –  | –  |
| Link servers, join hubs, share and unshare channels | Y | – | –  | –  | –  | –  |
| Register commands                              | –  | –  | –  | –  | Y  | –  |
| Make personal tokens                           | Y  | Y  | Y  | –  | –  | –  |
| Own profile, status, availability, do not disturb, quiet hours | Y | Y | Y | Y | Y [8] | – |
| Own bookmarks, blocks, saved replies, presets, reminders, scheduled messages | Y | Y | Y | Y | – | – |
| Own themes: make, change, delete, browse the theme library, add a theme to their own [9] | Y | Y | Y | Y | – | – |
| Publish and unpublish their own themes                 | Y  | Y  | Y  | –  | –  | –  |
| Look at one theme by its link (its id)                 | Y  | Y  | Y  | Y  | Y  | –  |
| Remove a theme from the library; set the default dark and light themes | Y | Y | – | – | – | – |

[1] Only with accounts they can see (4.3), which for a guest or bot
means accounts already in a conversation with them (decision P13).

[2] A remote account's home server starts these, and this server
accepts them only while the two accounts share a shared channel
(section 14).

[3] Only if the admin setting lets members invite. A member's invite
always gives the `member` role and carries no groups or channels.

[4] Admins put guests and bots into groups. They cannot join by
themselves, because a group can carry channel memberships
(decision P14).

[5] An admin may act on `member`, `guest` and `bot` accounts. Only
the owner acts on an admin. Nobody can remove, ban or time out the
owner (decision P15).

[6] A channel's managers, if the admin setting hands approval to
them.

[7] Only if the admin setting lets members add emoji.

[8] A bot may set its profile and status. The rest has no meaning for
a bot.

[9] Ruling R61. A theme's id is its link: whoever has it may look at
the theme and add a copy, published or not. Only the owner changes,
deletes, publishes or unpublishes a theme; to anyone else a theme they
may not change answers `not_found`. A guest who is given a link or
browses the library may use and add themes but not publish one.
Bots own no themes. Removing a theme from the library unpublishes
it; its owner keeps it.

[10] The channel browser shows a guest or a bot only the channels it
is in; it is not refused. The account list shows them the accounts
they can see (4.3).

The command line (`churr recovery`, `import`, `export`, `migrate-db`,
`backup`) is the operator's and does not pass through `perm`. Its
actions are written to the audit log with `actor_kind` `cli`.

### 9.2 What the owner and admins cannot do

- Read, search or receive anything from a private channel, ad-hoc
  channel or sealed chat they are not in. No API operation does it.
- Add themselves to a private channel. They can be added by one of
  its managers like anyone else.
- See that an ad-hoc channel or a sealed chat exists.
- See who answered what on an anonymous form.
- Edit another account's message.

The export is the one stated exception: it contains private and
ad-hoc channels. Only the owner can run it, it is written to the
audit log, and it never contains sealed chats. The design says
plainly that whoever runs the server holds the database.

### 9.3 Groups and channel membership

- A group added to a channel (`channel_groups`) puts every account in
  the group into that channel at the group's level, and keeps them in
  step as the group changes.
- An account in a channel by more than one route (directly, and
  through one or more groups) has the **highest** of those levels.
  The order is `reader` < `member` < `manager`.
- When a route goes away (the account leaves the group, the group is
  taken out of the channel, the group is deleted) the level is worked
  out again from the routes that remain. If none remain the account
  is no longer a member.
- An account in a channel through a group cannot leave it and cannot
  be removed from it one at a time (section 6, notes 6 and 9).
- `@everyone` is every owner, admin and `member` account that is
  active. `@admins` is the owner and the admins. Both follow roles
  and cannot be edited. A role change moves the account in or out of
  them, and so in or out of the channels they are in.
- Guests and bots can be in ordinary groups, placed there by an
  admin, and then gain those groups' channels. Remote accounts cannot
  be in groups.
- Being in a group gives nothing else. There is no permission matrix
  per group.

## 10. Archived and locked conversations

An archived channel is frozen.

| Still allowed                                   | Refused with `archived`                |
|-------------------------------------------------|----------------------------------------|
| See, read, search, history, download            | Post, reply, upload, schedule          |
| Mark read, set mode and show, star, bookmark    | React, tick, answer or close forms     |
| Leave                                           | Edit                                   |
| Delete own message, ask for a purge             | Pin, set topic, name, welcome, slow mode |
| An admin or manager deleting a message; purge   | Join, add or remove people or groups, set levels |
| Report a message                                | Start or join a call                   |
| Unarchive (section 7)                           | Move a thread out; request a webhook   |
| Retention, which keeps running                  | Posts from its webhooks and scheduled items |

### 10.1 Locked channels

An account acting as manager of a private channel, named or ad-hoc,
may lock it. Locking is one-way: no operation unlocks a channel, for
anyone. A public channel cannot be locked (`conflict`); it is made
private first. A sealed chat has nothing to lock. A shared
conversation cannot be locked and a locked channel cannot be shared
(`forbidden`), because who joins a shared conversation is partly
other servers' doing (decision P25).

Any admin may also lock a named private channel without being a
member of it, from the admin view, the same way an admin can appoint
a manager or archive it. An ad-hoc channel is invisible to admins
outside it, so there only its members acting as manager can lock.

When a channel is locked, each account that was in it through a
group becomes a direct member at the level it had, and the groups
are detached from the channel. So the people the groups had brought
in stay, the groups bring in nobody new, and those people can now
leave like anyone else (decision P23).

| Refused with `locked`, for everyone, admins and the owner included | Still allowed |
|----------------------------------------------------|------------------------------|
| Add a person, a bot or a remote account            | Everything in section 5      |
| Join, including through the link left by a moved thread | Leave                   |
| Add a group                                        | Remove a member              |
| Ask for or approve a webhook, which would add its account | Set levels; an admin appointing a manager |
| Use an invite that names the channel (the invite works; the channel is skipped) | Name, topic, pin, slow mode, retention |
| Share it with other servers                        | Archive and unarchive        |

Someone who leaves or is removed cannot come back, since nobody can
be added. When the last member leaves, the channel is deleted with
its content like any private channel. A thread moved out of a locked
channel makes a new channel that is not locked.

## 11. Timed out

A time-out is for one conversation (set by an account acting as
manager there) or for the whole server (set by an admin). It has an
end time and can be lifted early by anyone who could have set it.

A manager cannot time out an admin or the owner. An admin cannot time
out another admin; the owner can. A time-out on a remote account is
local to this server (section 14).

While it lasts, in the conversations it covers:

| Refused with `timed_out`                     | Still allowed                         |
|----------------------------------------------|---------------------------------------|
| Post, reply, upload, schedule                | See, read, search, history, download  |
| React                                        | Receive live events, mark read        |
| Tick a checklist, answer a form, press a form button | Set mode and show, star, bookmark, follow or ignore threads |
| Edit own message                             | Delete own message, ask for a purge   |
| Start or join a call, send typing notices    | Report a message, leave               |
| Start an ad-hoc channel or sealed chat (server-wide time-out only) | Change own profile and status |
| Use management powers there (a timed-out manager) | Join a public channel (still timed out if server-wide) |

The design says a timed-out account cannot post or react and can
still read. Everything else that puts content in front of other
people is refused on the same footing (decision P16).

## 12. Removed, banned and unclaimed accounts

**Removed.** Removal ends every session and token, takes the account
out of every conversation and every group, and cancels what it had
scheduled. From then on:

- Every credential it held answers `unauthenticated`. It can do
  nothing.
- Its messages stay where they are, shown under its name as a former
  member. They can still be read, deleted by managers, purged by
  admins and removed by retention.
- It cannot be mentioned, added, invited, or chosen in a form.
- Its username stays taken, so an old `@name` never comes to mean
  someone else (decision P17).
- A sealed chat it was in stays readable by the other person and
  accepts nothing new.

**Banned** (in the interface: **exiled**, ruling R72; a ban from one
channel is section 6.1). A ban is a removal that also refuses the same
person at the door: a company sign-in with a banned provider and subject is
refused, whether signing in or registering. A passkey-only person has
no identity to match, so for them a ban is a removal and nothing
more. Lifting a ban lets the person register again as a new account.

**Unclaimed.** An account made by the Slack importer and not yet
claimed cannot sign in, so it performs no action. It is the author of
its imported messages and a member of its imported channels, is not
in `@everyone`, and is given no mentions or notifications until it is
claimed (decision P18).

**Pending second factor.** A session waiting for its second factor
can complete the second factor and nothing else
(`second_factor_required`).

**Locked session** (the lock, ruling R75, `AUTH.md` section 10.6). A
`web` or `app` session may lock itself (`lockSession`); nobody can lock
another's session, and a personal or bot token cannot lock (`forbidden`).
While locked, the session can only lock again, begin unlocking, and
sign out; an app may still refresh its tokens. Everything else, the
live connection and the wake-up fetch included, answers 401
`session_locked`, whatever the role: the owner's locked session is as
locked as a guest's. To an operation that also works signed out, it is
nobody. It is unlocked only by a user-verified assertion of one of the
account's own passkeys (`finishSessionUnlock`, which needs no
credential: the ceremony names the session). The account's other
sessions are not affected.

## 13. Personal blocks

When X has blocked Y, and blocking is on for the server:

Ruling R77: a block is deaf ears. Nothing Y does is refused because
of it, and nothing Y can see differs from what it would be with anyone
else; it simply never reaches X.

| Y tries to                                        | Result                           |
|---------------------------------------------------|----------------------------------|
| Start a direct chat or sealed chat with X         | Allowed, and looks normal: a chat request that waits for ever, or, if X approves new chats automatically, a chat that appears open. X receives nothing (section 13b) |
| Start an ad-hoc channel that includes X           | Allowed. Y is shown X as a member; X is not one and is not told |
| Add X to an ad-hoc channel                        | The same                         |
| Add X to a named channel                          | `forbidden`, with wording that does not say why |
| See X's presence, status, availability, do not disturb, call, last seen, typing | X is always online with nothing else, and no change is sent |
| Mention X                                         | Allowed. X gets no mention and no notification. |
| Post in a conversation both are in                | Allowed. X's clients collapse the message; it never notifies X or counts in X's badges. |
| Ring X by starting a call                         | The call starts. X is not rung.  |
| Push a message through X's do not disturb         | Appears to succeed. X is not notified. |

- A block changes nothing for anyone else and nothing about what Y
  can read.
- Conversations the two already share stay as they are, including an
  existing two-person channel or sealed chat.
- A block does not limit moderation: a manager or admin whom X has
  blocked can still act on X's messages and membership.
- If an admin turns blocking off, the stored blocks stop having any
  effect and making a new one answers `feature_off`.
- A block made on another server is known here only as that server
  says it, and only for the row on presence and typing (section 14).

## 13a. Watches

"Tell me when Ada is around" (rulings R76 and R77, `FEATURES.md`
section 9.7). `perm.CanWatch` and `perm.WatchMayFire` decide.

| Check, in order                                             | Refusal       |
|-------------------------------------------------------------|---------------|
| The caller is a person, not a bot                           | `forbidden`   |
| The caller can see the account (4.3)                        | `not_found`   |
| It is not the caller, not a bot, not removed                | `forbidden`   |
| The two share a conversation, so the caller is shown its presence (4.3) | `forbidden` |
| The caller has not blocked it, while blocking is on         | `forbidden`, "You are ignoring Ada." |

Nothing the watched person chose decides the answer. If they have
blocked the caller they always appear online to the caller (ruling
R77), so the answer is "around now", identical to the genuine case,
and nothing is stored. A watch fires only while the two still share a
conversation, the person is not removed or banned, and neither has
blocked the other; otherwise it waits silently and expires. A block of
the caller made on another server counts as that server says it
(section 14).

## 13b. Chat requests and closing a chat

Rulings R78 and R79; `FEATURES.md` sections 20.2 and 20.3. The chat
service decides, with `perm.HasBlocked` and the person's preferences;
`perm.Check` refuses writing in a chat that waits, was declined or
was closed.

| Action | Who | Refusal |
|--------|-----|---------|
| Start a direct or sealed chat with someone of this server who does not approve new chats of that kind automatically | As starting any such chat | It becomes a request. `rate_limited` within 24 hours of the last request to that person being declined or withdrawn, or past the caps of `FEATURES.md` 20.2 |
| List, accept, decline, dismiss, block from a request | The person asked, for a request in their list | `not_found` for anyone else and for a request not in the list (from someone they have blocked, already answered, withdrawn) |
| Withdraw a request | The person who asked, while it waits | `conflict` once it is open, declined or closed |
| Close a chat | Either member of a direct chat (an ad-hoc channel they are shown with one other person) or a sealed chat | `forbidden` for any other conversation; `conflict` while its request waits (withdraw instead) |
| Post, reply, react, upload, type, add members, start a call, schedule, in a chat with `chat_state` `waiting`, `declined` or `closed` | Nobody | `forbidden`. Reading, marking read and hiding still work |

Nothing about a request tells the person asking whether it was seen,
dismissed or blocked: those three look like a request not yet seen,
and count the same in the caps.

## 14. Shared conversations and remote accounts

`LINK-PROTOCOL.md` has the protocol. These are the rules `perm`
applies.

**A remote account**

- has the role `remote`, which gives nothing server-wide (9.1);
- is a member only of shared conversations, placed there by its home
  server's join event, at the conversation's default level. It is
  never a manager here (decision P19);
- cannot be in a group, be invited, hold a token, or be added to a
  conversation that is not shared;
- sees and is seen under 4.3.

**Events arriving for a remote account** are checked as if that
account had made the same request here: same matrix, same level, same
archived and timed-out rules. An event that fails is dropped and not
delivered. On top of that the link rules in `DESIGN.md` section 11
apply: an action by an account is accepted only from that account's
home server.

**What a shared conversation refuses or changes**

| Action                                 | In a shared conversation                         |
|----------------------------------------|--------------------------------------------------|
| Move a thread out to a channel         | Refused.                                         |
| Forms                                  | Only the `tally` action, named or anonymous. Answers go sealed to the server the form was posted on, which keeps the tally and sends it on. `collect`, `thread`, `channel`, `group` and `bot` forms are refused with `forbidden`. |
| Make private; lock                     | Making it private: accounts acting as manager on the owner server. Locking: refused. |
| Set name or topic, archive             | Accounts acting as manager on the owner server. Refused on other servers. |
| Delete another account's message       | On the owner server it travels. On another server a manager's delete hides the message on that server only. In a channel a hub owns there is no owner server: a moderator's delete travels, from whichever server is the moderator's home (section 14.1), and a manager's who is not a moderator hides the message on that server only. |
| Time out a remote account              | Local: this server stops accepting that account's posts in that conversation while it lasts. |
| Slow mode                              | Local: each server applies its own to its own accounts. |
| Purge                                  | Section 8.2. The purge travels as a request the other servers are expected to honour. |
| Retention                              | Local. The deletion is also sent on.             |
| Share, unshare, add or remove a server | The owner of the owner server. In a channel a hub owns: sharing it to the hub, joining it, asking to join, answering an invitation and leaving are each server's owner's; inviting, approving, removing a server and closing the channel are its moderators' (section 14.1). |
| Move a direct-link channel onto a hub  | Proposed by the owner of the owner server, accepted or declined by the owner of the other server (`LINK-PROTOCOL.md` section 21a). While the move is in progress the server that accepted refuses, with `conflict`, any change of a member's level in the channel and a manager's leaving it. |

### 14.1 Channels a hub owns

A named channel shared through a hub belongs to the hub (ruling R56;
`LINK-PROTOCOL.md` section 23a). It has **moderators**: accounts,
each named with its home server in the record the hub signs. The
server keeps, with the newest record it has verified, which of the
moderators are its own accounts (`stream_channels.moderators`), and
`perm` reads that: `State.HubModerator` is true when the conversation
is this server's copy of such a channel and the actor is a local
account on that list.

**Where a moderator's authority comes from.** From the hub's signed
list and from nowhere else. It does not come from the account's role
on its server or its level in the server's copy of the channel, and
those do not take it away: a moderator may be a plain member, at
level `member` or `reader`, or not a member of the local copy at
all. The other way round, being the owner, an admin or a manager on a
server gives no authority over the channel on other servers, and
owning the server that first shared the channel gives none either.

**What a moderator may do, and how it meets the local rules.**

| Action | Rule on the moderator's own server | What the others check |
|--------|------------------------------------|-----------------------|
| Delete another account's message for everyone | Allowed when the account is active, is a person's (not a bot, not `remote`), can see the conversation (4.1), and is not timed out there or server-wide (11). The level column of 8.1 is not asked. The message is deleted here as a manager's delete is, and the removal is sent on. | Every server that receives the removal checks the account against the moderator list of the record it holds, as an account of the server that signed. It applies no rule of its own roles or levels to it. |
| Invite, approve or decline a server; have a server removed; change the channel's name or description on the hub; set the listing mode; add or remove a moderator; close the channel (`moderateHubChannel`) | Allowed when the account is active, a person's, and on the list as this server's. It need not be the owner or an admin. | The hub checks the same against its own record; servers take the result only as a record the hub signed. |
| Everything else in the channel (posting, pinning, levels, slow mode, time-outs, archiving the local copy) | Under this server's ordinary rules, at the account's own role and level. Being a moderator adds nothing here. | – |

Three consequences worth stating:

- A server may still stop its own account: a time-out here stops a
  moderator's deletes from this server, and removing or banning the
  account ends them. That is the home server declining to sign for
  the account. It cannot stop a moderator of another server.
- A manager of the local copy who is not a moderator keeps every
  local power, and their delete of another account's message hides
  it on this server only.
- An owner or admin who is not a moderator cannot do what the second
  row lists: `forbidden`. They can read the record, and they still
  decide what is their server's to decide: joining, asking to join,
  answering an invitation, leaving.

**Who sees the record.** A member of the local channel, a moderator,
or the owner. The servers invited and asking to join are shown to
moderators and the owner only.

**Ad-hoc channels and sealed chats across servers**

- A local account may start one with remote accounts only while it
  shares at least one shared channel with each of them, and all the
  servers involved sit on one hub or one direct link.
- When the people no longer share a channel, the conversation becomes
  read-only: every posting action in section 5 is refused with
  `forbidden`, and reading continues.
- A block by a local account is honoured against remote accounts in
  the same way as in section 13, as far as this server can: an offer
  from the blocked account is taken and leaves the local account out,
  and in a chat of two its typing is not sent. The blocked account's
  server is told of the block (`LINK-PROTOCOL.md` section 17a).
- A block a remote account made of a local one, as its server says
  it, is honoured for presence and typing alone: the local account is
  shown it online with nothing more, none of its typing, and a watch
  as in section 13a. `perm.Checker.RemoteDeaf` answers it; nothing
  else here refuses or hides anything because of it.
- Across a hub they are not the hub's (ruling R59): they have no
  record, no listing and no moderators, and section 14.1 does not
  apply to them.

## 15. Bots, webhooks and integrations

**Bots.** A bot is checked like a guest: it sees and reads only the
conversations it is in, and what it may do there is its level. In
addition:

| A bot cannot                                    | A bot can, and a person cannot             |
|-------------------------------------------------|--------------------------------------------|
| Browse, or join a channel by itself             | Post cards                                 |
| Be in a sealed chat                             | Post a form with the `bot` action          |
| Hold the owner or admin role                    | Register commands and receive them         |
| Answer a form or press a form button            | Show one person a form in a dialog, in answer to that person's command or button press |
| Push a message through do not disturb           | Edit its own message after the edit window |
| Join an open group by itself; create invites    |                                            |
| Add a group to a channel                        |                                            |

- A bot receives a command only from a conversation it is in, and a
  form answer only for a form it posted.
- A bot at level `manager` has the management powers of section 7 in
  that channel, and can add only accounts it can see.
- A bot may start an ad-hoc channel with accounts it can see.

**Webhooks.** A webhook's secret works for one thing: posting into
its one conversation, as a top-level message or as a reply to a
message there. Its account is a member of that conversation at level
`member`. It can read nothing, edit nothing and delete nothing. It
may post text, files, cards and a `tally`, `collect` or `thread`
form. It exists only in named channels: asking for one in an ad-hoc
channel would show the channel to the admins who approve it
(ruling R8). In a shared channel its forms are `tally` only.
Revoking it, archiving the channel or removing its account stops
it.

**Integrations.** An integration token does only what was ticked when
it was made. The one thing there is to tick is setting availability
(in a meeting, out of office) for an account found by username or
sign-in identifier. It is in no conversation, reads no message, sees
no member list, and every use is written to the audit log.

## 16. Decisions recorded here

| No. | Gap                                              | Choice                                   |
|-----|--------------------------------------------------|------------------------------------------|
| P1  | What a personal token may do                     | Everything its account may, except managing sign-in. |
| P2  | (Replaced by ruling R1: public to private only.) | Section 7.1.                             |
| P3  | An admin's powers in a private channel they belong to | They act as manager there. The design gives them level-setting and message deletion there already. |
| P4  | Reports in a sealed chat                         | None.                                    |
| P5  | Who may fetch avatar and emoji files             | Any signed-in account.                   |
| P6  | Who sees groups                                  | Owner, admin and `member`: all. Guests and bots: the groups they are in or that are in their conversations. |
| P7  | Whether a non-member reading a public channel may react or answer | No. They join first.    |
| P8  | An author's rights after leaving                 | Delete, ask for purge and close own form remain while they can read the conversation. Editing needs level `member`. |
| P9  | Groups in ad-hoc channels                        | Not allowed.                             |
| P10 | Leaving a sealed chat                            | Not possible; set `show` to `hidden`.    |
| P11 | A private or ad-hoc channel with no members left | Deleted with its content.                |
| P12 | Asking for a purge again after "show again"      | Refused until the message changes.       |
| P13 | Guests and bots starting ad-hoc channels         | Allowed, with accounts they can already see. |
| P14 | Guests and bots joining open groups              | Not by themselves.                       |
| P15 | Who may remove, ban or time out whom             | Admins act on members, guests and bots; the owner on admins; nobody on the owner; managers on nobody above `member` role. |
| P16 | What a time-out stops beyond posting and reacting | Everything that puts content before others; see section 11. |
| P17 | A removed account's username                     | Stays taken.                             |
| P18 | Unclaimed imported accounts                      | Not in `@everyone`, never notified.      |
| P19 | A remote account's level                         | The conversation's default level; never manager. |
| P20 | (Replaced by ruling R12: `tally` only in shared conversations.) | Section 14.              |
| P21 | Webhooks in ad-hoc channels                      | Not allowed.                             |
| P22 | What an admin gets from ordinary operations on a private channel they are not in | `not_found`, like anyone else. The admin view is separate. |
| P23 | What "a group brings in no new people, its current members stay, people can still leave" means in rows | On locking, group members become direct members and the groups are detached. |
| P24 | Purge requests waiting when a channel goes private | Carried out as erasures at once.       |
| P25 | Locking and sharing                              | A shared conversation cannot be locked; a locked channel cannot be shared. |
| P26 | Who may lock (owner's confirmation)              | Accounts acting as manager of the channel, and any admin for a named private channel, member or not. An ad-hoc channel only from inside. |

## 17. Test cases

An automated suite must cover each line, on both databases. "Refused"
names the code the API must return. "PRIV" is a named private
channel, "PUB" a named public channel, "ADHOC" an ad-hoc channel.
Unless a line says otherwise the actor is active, not timed out, and
the conversation is not archived or shared.

### Reading what one is not in

1. Given a `member` not in PRIV, when they fetch its messages, then
   `not_found`.
2. Given a `member` not in PRIV, when they fetch PRIV itself by
   identifier, then `not_found`.
3. Given a `member` not in PRIV, when they search for a word that
   appears only in PRIV, then the result list is empty.
4. Given a `member` not in PRIV, when they download a file uploaded
   to PRIV, then `not_found`.
5. Given a `member` not in PRIV, when they fetch that file's
   thumbnail, then `not_found`.
6. Given a `member` not in PRIV with a live connection, when a
   message is posted in PRIV, then no event reaches that connection.
7. Given a `member` not in PRIV, when they ask to catch up on PRIV's
   events, then `not_found`.
8. Given a `guest` not in PRIV, when they do each of 1 to 7, then the
   same refusals.
9. Given a `bot` not in PRIV, when it does each of 1 to 7, then the
   same refusals.
10. Given a `guest` not in PUB, when they fetch PUB's messages, then
    `not_found`.
11. Given a `bot` not in PUB, when it searches for a word that
    appears only in PUB, then the result list is empty.
12. Given a `guest`, when they open the channel browser, then they
    see only the channels they are in.
13. Given a `guest`, when they fetch an account that shares no
    conversation with them, then `not_found`.
14. Given a `member` not in ADHOC, when they fetch it, its messages
    or a file in it, then `not_found` each time.
15. Given a `member` not in PUB, when they fetch PUB's messages and
    search it, then both succeed.
16. Given a `member` not in PUB with a live connection, when a
    message is posted in PUB, then no event reaches that connection.
17. Given a `member` not in PUB, when they react to a message in
    PUB, then `forbidden`.
18. Given a `member` not in PUB, when they answer a poll in PUB, then
    `forbidden`.
19. Given a `member` not in PUB, when they post in PUB, then
    `forbidden`.
20. Given a member of PRIV who then leaves, when they fetch a message
    they posted there, then `not_found`.
21. Given an upload to PRIV that is not yet attached, when another
    member of PRIV downloads it, then `not_found`.

### Admins are not members of everything

22. Given an admin not in PRIV, when they list channels in the admin
    console, then PRIV appears with its name, members and managers
    and no topic, no message count and no last-message time.
23. Given an admin not in PRIV, when they fetch PRIV's messages, then
    `not_found`.
24. Given an admin not in PRIV, when they search for a word that
    appears only in PRIV, then the result list is empty.
25. Given an admin not in PRIV, when they download a file from PRIV,
    then `not_found`.
26. Given an admin not in PRIV with a live connection, when a message
    is posted in PRIV, then no event reaches that connection.
27. Given an admin not in PRIV, when they try to join PRIV, then
    `not_found`.
28. Given an admin not in PRIV, when they try to add themselves or
    anyone else to PRIV, then `not_found`.
29. Given an admin not in PRIV, when they raise an existing member of
    PRIV to `manager`, then it succeeds and is in the audit log.
30. Given an admin not in PRIV, when they lower a manager of PRIV to
    `member`, then `not_found`, and the admin console offers no such
    operation.
31. Given an admin not in PRIV, when they archive PRIV, then it
    succeeds.
32. Given an admin not in PRIV, when they set PRIV's topic, then
    `not_found`.
33. Given the owner not in ADHOC, when they list channels in the
    admin console, then ADHOC does not appear.
34. Given the owner not in ADHOC, when they fetch ADHOC by
    identifier, then `not_found`.
35. Given the owner, when they list conversations in the admin
    console, then no sealed chat appears.
36. Given an admin who has joined no channel, when they list their
    own conversations, then the list is empty and the status request
    reports zero unread.
37. Given an admin not in PUB, when a message that says `@channel` is
    posted in PUB, then the admin gets no mention and no
    notification.
38. Given an admin not in PUB, when PUB's member list is fetched,
    then the admin is not in it.
39. Given an admin not in PUB, when they pin a message in PUB, then
    it succeeds.
40. Given an admin not in PUB, when they post in PUB, then
    `forbidden`.
41. Given an admin, when they edit another account's message in PUB,
    then `forbidden`.
42. Given an admin who is a `member`-level member of PRIV, when they
    list waiting reports for PRIV, then `forbidden`.
43. Given an admin, when they run the export, then `forbidden`; given
    the owner, then it succeeds and is in the audit log.
44. Given an admin, when they change an account's role, then
    `forbidden`.
45. Given an admin, when they remove another admin, then `forbidden`.
46. Given the owner, when anyone tries to remove, ban or time out the
    owner, then `forbidden`.
47. Given an admin, when they create a link to another server, then
    `forbidden`.

### Levels

48. Given a `reader` in an announcement channel, when they post, then
    `forbidden`.
49. Given a `reader`, when they reply in a thread, then `forbidden`.
50. Given a `reader`, when they upload a file, then `forbidden`.
51. Given a `reader`, when they tick a checklist item, then
    `forbidden`.
52. Given a `reader`, when they react and answer a poll, then both
    succeed.
53. Given a `member`-level account, when they pin a message, then
    `forbidden`.
54. Given a `member`-level account, when they set the topic, then
    `forbidden`.
55. Given a `member`-level account in PRIV, when they add someone,
    then `forbidden`.
56. Given a `member`-level account in ADHOC, when they add someone
    they can see, then it succeeds and the newcomer can read all
    earlier messages.
57. Given a `member`-level account in ADHOC, when they remove someone
    else, then `forbidden`.
58. Given a `member`-level account, when they delete another
    account's message, then `forbidden`.
59. Given a manager who is not an admin, when they purge a message,
    then `forbidden`.
60. Given a `member`-level account, when they set slow mode, time
    someone out or archive, then `forbidden` each time.
61. Given a `member`-level account, when they post a form with the
    `channel` action, then `forbidden`.
62. Given a manager who is not an admin, when they post a form with
    the `group` action, then `forbidden`.
63. Given a person, when they post a message that carries a card or a
    form with the `bot` action, then `forbidden`.
64. Given a manager, when they set a retention period longer than the
    server's, then `forbidden`; shorter, then it succeeds.
65. Given a manager of ADHOC, when they set a retention period, then
    `forbidden`.
66. Given a channel's only manager, when they lower their own level,
    then `conflict`.
67. Given a channel's only manager, when they leave, then the
    longest-standing member becomes manager.
68. Given a manager who is not an admin, when they time out an admin
    in their channel, then `forbidden`.
69. Given a `member`-level account, when they read the answers to
    someone else's `collect` form, then `forbidden`.
70. Given an anonymous poll, when its author, a manager, an admin and
    the owner each fetch it, then none of them receives voters.
71. Given the edit window has closed, when the author edits their
    message, then `forbidden`.
72. Given a `member`-level account, when they close another account's
    poll, then `forbidden`.
73. Given a `member`-level account who is not the thread's starter,
    when they move the thread to a channel, then `forbidden`.

### Guests, bots, webhooks, integrations

74. Given a `guest`, when they join PUB by themselves, then
    `not_found`.
75. Given a `guest`, when they create a channel, then `forbidden`.
76. Given a `guest`, when they create an invite, then `forbidden`.
77. Given a `guest`, when they join an open group, then `forbidden`.
78. Given a `guest`, when they start an ad-hoc channel with an
    account they share no conversation with, then `not_found`.
79. Given a `member` and the setting off, when they create an invite,
    then `forbidden`.
80. Given a `member` and the setting on, when they create a guest
    invite or an invite that carries a group, then `forbidden`.
81. Given a `bot`, when it joins PUB by itself, then `not_found`.
82. Given a `bot` in PUB, when it answers a form there, then
    `forbidden`.
83. Given a `bot`, when it posts with notify anyway, then
    `forbidden`.
84. Given a `bot`, when anyone tries to start a sealed chat with it,
    then `forbidden`.
85. Given a `bot` in PUB as `reader`, when it posts, then
    `forbidden`.
86. Given a `bot` removed from PUB with a live connection, when a
    message is posted in PUB, then no event reaches it.
87. Given a `bot`, when a command it registered is run in a
    conversation it is not in, then the bot receives nothing and the
    asker gets `not_found`.
88. Given a `member` with no admin role, when they create a bot or a
    bot token, then `forbidden`.
89. Given a webhook secret, when it is used as a bearer token on any
    API request, then `unauthenticated`.
90. Given a webhook for PUB, when its request names a parent message
    in another conversation, then `not_found`.
91. Given a revoked webhook, when it posts, then `not_found`.
92. Given a `member` in ADHOC, when they ask for a webhook there,
    then `forbidden`.
93. Given a `guest` in PUB, when they ask for a webhook there, then
    `forbidden`.
94. Given an integration token, when it fetches conversations,
    messages or the account list, then `forbidden`.
95. Given an integration token without the availability tick, when it
    sets someone's availability, then `forbidden`.
96. Given a personal token, when it adds a passkey to its account,
    then `forbidden`.

### Groups

97. Given a group added to PRIV as `reader` and an account in it also
    added directly as `member`, when the level is read, then it is
    `member`.
98. Given the same account, when its direct level is removed, then it
    is `reader`.
99. Given two groups in a channel at `member` and `manager` and an
    account in both, when it leaves the `manager` group, then its
    level is `member`.
100. Given an account in PRIV only through a group, when it leaves
     the group, then it can no longer read PRIV and gets no more
     events from it.
101. Given an account in a channel through a group, when it tries to
     leave the channel, then `conflict`.
102. Given an account in a channel through a group, when a manager
     tries to remove it from the channel, then `conflict`.
103. Given a group taken out of a channel, when a former member
     through that group fetches the channel, then `not_found` if it
     is private.
104. Given a new `member` account, when it is created, then it is a
     member of every channel `@everyone` is in and of no other.
105. Given a new `guest` account, when it is created, then it is in
     none of the `@everyone` channels.
106. Given an account whose role changes from `member` to `guest`,
     when the change is saved, then it has left the channels it was
     in only through `@everyone`.
107. Given a remote account, when an admin adds it to a group, then
     `forbidden`.
108. Given a `member`, when they edit a group or `@everyone`, then
     `forbidden`; given an admin editing `@everyone`, then
     `forbidden`.
109. Given a manager of ADHOC, when they add a group to it, then
     `forbidden`.
110. Given a group that is not open, when a `member` joins it by
     themselves, then `forbidden`.

### Purge, history and reports

111. Given an author in PRIV, when they ask for a purge, then the
     history is erased at once, no purge request is stored, and no
     audit entry is written.
112. Given an author in PUB, when they ask for a purge, then a
     non-admin opening the history gets `forbidden` and an admin
     still can.
113. Given a waiting purge request, when the author opens the
     message's history, then `forbidden`.
114. Given a waiting purge request, when a non-admin lists purge
     requests, then `forbidden`.
115. Given a decided purge request, when the author fetches the
     request, then no outcome is returned.
116. Given a request decided "show again" and an unchanged message,
     when the author asks again, then `conflict`.
117. Given a message by someone else, when a member asks for its
     purge, then `forbidden`.
118. Given a purged message, when anyone including the owner opens
     its history, searches for its text or downloads its files, then
     nothing is returned.
119. Given a sealed chat, when either person asks for a purge, then
     `forbidden`.
120. Given a report made in PRIV, when an admin who is not a manager
     of PRIV lists reports, then it is not among them.
121. Given a report made in ADHOC, when the owner lists reports, then
     it is not among them.
122. Given a report made in PUB, when an admin who has not joined PUB
     lists reports, then it is among them.
123. Given a `member` not in PRIV, when they report a message in PRIV
     by identifier, then `not_found`.

### Archived, timed out, removed, blocked

124. Given an archived channel, when a manager posts, then
     `archived`.
125. Given an archived channel, when a member reacts, edits, ticks or
     answers, then `archived` each time.
126. Given an archived channel, when its webhook posts, then
     `archived`.
127. Given an archived channel, when a member reads and searches it,
     then both succeed.
128. Given an archived channel, when an author deletes their own
     message, then it succeeds.
129. Given an account timed out in PUB, when it posts, replies,
     reacts, uploads, ticks, answers a form or edits there, then
     `timed_out` each time.
130. Given an account timed out in PUB, when it reads PUB and posts
     in another channel, then both succeed.
131. Given an account timed out server-wide, when it posts in any
     conversation or starts an ad-hoc channel, then `timed_out`.
132. Given a time-out that has ended or been lifted, when the account
     posts, then it succeeds.
133. Given a timed-out manager, when they pin a message there, then
     `timed_out`.
134. Given a removed account, when any of its session, personal or
     bot tokens is used, then `unauthenticated`.
135. Given a removed account with an open live connection, when it is
     removed, then the connection is closed and receives nothing
     more.
136. Given a removed account, when another account lists the members
     of a channel it was in, then it is not listed, and its messages
     are still there.
137. Given a removed account, when a member starts an ad-hoc channel
     with it or mentions it, then the first is `forbidden` and the
     second notifies nobody.
138. Given a removed account's username, when someone registers with
     it, then `name_taken`.
139. Given a banned sign-in identity, when that identity signs in or
     registers through the provider, then it is refused.
140. Given an unclaimed imported account, when a channel it is in
     gets `@channel`, then no mention is stored for it.
141. Given X has blocked Y, when Y starts an ad-hoc channel including
     X, starts a sealed chat with X, or adds X to an ad-hoc channel,
     then each answers as it would for anyone (ruling R77), X is not
     a member and receives nothing.
142. Given X has blocked Y, when Y mentions X in a channel both are
     in, then the message is posted and X has no mention, no
     notification and no badge change.
143. Given blocking is off for the server, when anyone creates a
     block, then `feature_off`, and existing blocks have no effect.

### Sealed chats and shared conversations

144. Given a sealed chat, when a third account fetches it, then
     `not_found`.
145. Given a sealed chat, when either person tries to add someone,
     then `forbidden`.
146. Given a sealed chat, when either person searches for its text,
     then the result list is empty.
147. Given a sealed chat, when a message with a card or a form is
     posted, then `invalid_request`.
148. Given sealed chats are off for the server, when a member starts
     one, then `feature_off`.
149. Given a shared channel, when a manager moves a thread to a
     channel, then `forbidden`.
150. Given a shared channel, when a member posts a `collect`,
     `thread`, `channel`, `group` or `bot` form, then `forbidden`
     each time.
151. Given a remote account, when a local manager tries to make it a
     manager, then `forbidden`.
152. Given a remote account that shares no shared channel with a
     local member, when the member starts an ad-hoc channel or sealed
     chat with it, then `forbidden`.
153. Given an event for a remote account's message signed by a server
     that is not its home server, when it arrives, then it is
     dropped.
154. Given a remote account at `reader` level, when its home server
     sends a post from it, then the event is dropped.
155. Given an ad-hoc channel with a remote account whose last shared
     channel has ended, when a local member posts, then `forbidden`,
     and reading still succeeds.
156. Given a server that a channel is not shared with, when a message
     is posted there, then nothing is sent to that server.

### Public to private (R1)

157. Given PUB and one of its managers, when they make it private,
     then it succeeds and is in the audit log.
158. Given PUB and an admin who has not joined it, when they make it
     private, then it succeeds, and the admin's next fetch of its
     messages is `not_found`.
159. Given PUB made private, when a `member` who had been reading it
     without joining fetches it, its messages, a file in it and a
     message's history, then `not_found` each time.
160. Given PUB made private, when that `member` searches for a word
     that appears only there, then the result list is empty.
161. Given PUB made private, when that `member` lists bookmarks, then
     a bookmark of a message there is not among them.
162. Given PUB made private, when that `member` tries to join it,
     then `not_found`.
163. Given PUB made private, when a member of it reads and searches
     it, then both succeed and their level is unchanged.
164. Given PUB with a purge request waiting, when it is made private,
     then the history is erased and no admin can open it.
165. Given PRIV, when a manager, an admin and the owner each try to
     make it public, then `forbidden` each time.
166. Given ADHOC, when its manager tries to make it public, then
     `forbidden`.
167. Given PUB, when a `member`-level account tries to make it
     private, then `forbidden`.
168. Given an archived PUB, when a manager tries to make it private,
     then `archived`.

### Locking (R2)

169. Given PRIV, when a manager locks it, then it succeeds and
     `locked_at` is set.
170. Given ADHOC, when its manager locks it, then it succeeds.
171. Given PRIV, when a `member`-level account locks it, then
     `forbidden`.
172. Given PRIV and an admin not in it, when the admin locks it from
     the admin view, then it succeeds, is in the audit log, and the
     admin still cannot read it or join it.
173. Given PUB, when a manager tries to lock it, then `conflict`.
174. Given a locked PRIV, when a manager adds a person, then
     `locked`.
175. Given a locked ADHOC, when a member adds a person, then
     `locked`.
176. Given a locked PRIV, when a manager adds a bot or a group, then
     `locked` each time.
177. Given a locked PRIV, when an admin who is a member adds someone,
     then `locked`.
178. Given a locked PRIV made from a moved thread, when a member of
     the old channel tries to join it, then `locked`.
179. Given a locked PRIV, when a member asks for a webhook, then
     `locked`.
180. Given a guest invite that names a locked PRIV and another
     channel, when it is used, then the account is made and is in
     the other channel only.
181. Given PRIV with a group attached, when it is locked, then the
     group's members are still members at the same levels and the
     channel has no group.
182. Given that channel after locking, when someone joins the group,
     then they are not in the channel and cannot read it.
183. Given that channel after locking, when someone who came in
     through the group leaves the group, then they are still in the
     channel.
184. Given that channel after locking, when someone who came in
     through the group leaves the channel, then it succeeds.
185. Given a locked PRIV, when a member leaves and a manager then
     tries to add them back, then `locked`.
186. Given a locked PRIV, when anyone, the owner included, tries to
     unlock it, then no operation exists and a change that clears the
     lock is refused with `forbidden`.
187. Given a locked PRIV, when its last member leaves, then the
     channel and its content are deleted.
188. Given a locked PRIV, when a member posts, reacts and a manager
     pins, then all succeed.
189. Given a shared private channel, when a manager tries to lock
     it, then `forbidden`; given a locked PRIV, when the owner tries
     to share it, then `forbidden`.

### Notify anyway (R4)

190. Given a target in do not disturb who has the conversation on Any
     activity, when the author pushes a message through, then the
     request succeeds and the target is notified.
191. Given a target whose mode for the conversation is `nothing`,
     when the author pushes a message through, then the response is
     identical to case 190 and the target is not notified.
192. Given a target who has muted the conversation, then the same.
193. Given a target who has blocked the author, then the same.
194. Given a target who has turned notify anyway off, then the same.
195. Given a target who cannot read the conversation, then the same.
196. Given any of cases 190 to 195, when the message is fetched by
     anyone, then `notified_anyway` is true and nothing in any
     response or event says whether the target was reached.
197. Given a message by someone else, when a member pushes it
     through, then `forbidden`.
198. Given a webhook or a bot, when it pushes a message through,
     then `forbidden`.

### Forms in shared conversations (R12)

199. Given a shared channel, when a member posts a named `tally`
     form and when they post an anonymous one, then both succeed.
200. Given a `tally` form in a shared channel, when an account on
     another server answers, then its answer reaches the form's home
     server only, and the other servers receive the tally and not
     the answer.
201. Given an anonymous `tally` form in a shared channel, when any
     server's admin fetches it, then no voters are returned.
202. Given a shared channel, when its webhook posts a `collect`
     form, then `forbidden`.
203. Given a shared ad-hoc channel, when a member posts a `thread`
     form, then `forbidden`.

### Reading without joining (R22)

204. Given a `member` not in PUB, when they tick a checklist item in
     PUB, then `forbidden`.
205. Given a `member` not in PUB, when they press a form button or
     add an option to a poll in PUB, then `forbidden` each time.
206. Given a `member` not in PUB, when they join PUB and then react,
     answer and tick, then all succeed (ticking needs the level
     `member`).
207. Given an admin not in PUB, when they react to or answer a form
     in PUB, then `forbidden`, although they act as manager there.

### Who may answer (R33)

208. Given a form with `answerer_group_id`, when a member of the
     conversation who is not in that group answers or presses, then
     `forbidden`.
209. Given a `member`-level account, when they post a form with
     `answerer_group_id`, then `forbidden`; given an account acting
     as manager, then it succeeds.
210. Given ADHOC and the owner not in it, when the owner tries to
     lock it by identifier, then `not_found`.
211. Given PRIV locked by an admin from outside, when one of its
     managers adds a person, then `locked`.
212. Given a `member` with no admin role who is not in PRIV, when
     they try to lock it, then `not_found`.
213. Given a target in do not disturb with the conversation on Any
     activity and `show` set to `hidden`, when the author pushes a
     message through, then the target is notified as in case 190.
214. Given a member whose `show` is `hidden`, when a message that
     mentions them is posted, then they are notified and the status
     request's totals count it.
215. Given a member, when they set `mode` to anything but `any`,
     `mentions` or `nothing`, or `show` to anything but its four
     values or null, then `invalid_value`.
216. Given an account that is not a member of PUB, when it sets
     `mode` or `show` for PUB, then `forbidden`.

### Sub-channels and names (R49, R50)

217. Given a manager of PUB2 who is a member of PUB, when they set
     PUB as PUB2's parent, then it succeeds.
218. Given a manager of PUB2 who is not a member of PUB, when they
     set PUB as PUB2's parent, then `forbidden`.
219. Given a manager of PUB2 who is not in PRIV, when they set PRIV
     as PUB2's parent, then `not_found`.
220. Given a `member`-level account in both channels, when they set
     or clear the parent, then `forbidden`.
221. Given an admin in neither channel, when they set PUB as PUB2's
     parent, then it succeeds.
222. Given a sub-channel, when anyone tries to make it the parent of
     another channel, then `conflict`.
223. Given a channel that has sub-channels, when anyone tries to
     give it a parent, then `conflict`.
224. Given ADHOC, when its manager sets a parent for it, or names it
     as another channel's parent, then `invalid_value`.
225. Given a channel, when its manager sets it as its own parent,
     then `invalid_value`.
226. Given a member of PUB and a private sub-channel of PUB they are
     not in, when they list PUB's sub-channels, then it is not among
     them, and fetching it by identifier is `not_found`.
227. Given a manager of PUB who is not in its private sub-channel,
     when they fetch the sub-channel's messages, then `not_found`.
228. Given a member of a sub-channel who is not in its parent, when
     they post in the parent PRIV, then `not_found`; in the parent
     PUB without joining, then `forbidden`.
229. Given a `member` who joins PUB, when their conversations are
     listed, then none of PUB's sub-channels has been joined.
230. Given a public sub-channel of PRIV and a `member` not in PRIV,
     when they fetch the sub-channel, then it succeeds and carries
     no `parent_id`.
231. Given PRIV with sub-channels, when PRIV's last member leaves,
     then PRIV is deleted and each sub-channel remains, with its
     members and messages, and has no parent.
232. Given a parent that is archived, locked or made private, when a
     member of a sub-channel posts there, then it succeeds and the
     sub-channel's own state is unchanged.
233. Given a thread moved out of PUB, then the new channel's parent
     is PUB; out of a sub-channel of PUB, then its parent is PUB;
     out of ADHOC, then it has no parent.
234. Given a ticket form in a sub-channel of PUB, when it is
     answered, then the ticket channel's parent is PUB.
235. Given a shared channel with a local parent, when its events
     are sent to a linked server, then nothing names the parent.
236. Given PRIV named `plans`, when a `member` not in it creates a
     channel named `plans`, then it succeeds, and nothing in the
     response differs from creating any other name.
237. Given two channels named `ops` under the same parent, when a
     third is created with that name and parent, then it succeeds.
238. Given a channel, when a manager renames it to the name of
     another channel, public or private, then it succeeds.
239. Given a thread moved to a channel under a name already in use,
     then it succeeds.
240. Given a message whose text holds `<#id>` for a channel the
     author can see, when it is posted, then it has a `conversation`
     entity for that channel covering the whole link.
241. Given a message whose text holds `<#id>` for PRIV, which the
     author is not in, when it is posted, then it is accepted, has
     no entity for it, and nothing in the response says whether that
     channel exists.
242. Given a message whose text holds `<#id>` with an identifier
     that names no channel, when it is posted, then the result is
     the same as case 241.
243. Given a message with an entity for PRIV posted by a member of
     PRIV in PUB, when a reader not in PRIV fetches it, then the
     entity carries the identifier and no name, and the client draws
     "private channel".
244. Given the same message, when that reader searches for PRIV's
     name, then the message is not found by it.
245. Given two visible channels named `ops`, when a message
     containing the plain characters `#ops` is posted, then it has
     no `conversation` entity, and the same holds when only one
     channel has that name.
246. Given a webhook that posts `<#id>` for a channel its account is
     not in, then no entity is made.
247. Given any request to post or edit a message that carries
     `entity_hints`, then `invalid_request`, as for any unknown
     field.
248. Given a channel renamed after a message linked it, when the
     message is fetched, then its text and entity are unchanged and
     the channel object carries the new name.
249. Given a channel named with capitals and spaces, when it is
     created, then it succeeds and the name is stored as written.

### Channels a hub owns (R56)

250. Given a channel a hub owns and a moderator whose level in their
     server's copy is `member`, when they delete another account's
     message, then it succeeds.
251. Given the same account before it is a moderator, or after it
     has been dismissed, when it deletes another account's message,
     then `forbidden`.
252. Given a moderator who is timed out in the conversation, when
     they delete another account's message, then `timed_out`.
253. Given a moderator who cannot see the conversation (a private
     local copy they are not a member of), when they delete a
     message in it, then `not_found`.
254. Given a bot whose account id is on the moderator list, when it
     deletes another account's message, then `forbidden`.
255. Given a sealed chat, when an account marked as a moderator
     deletes the other person's message, then `forbidden`.
256. Given a manager of the local copy who is not a moderator, when
     they delete another account's message, then it succeeds (and
     the link sends nothing: the delete is local).
257. Given an owner who is not a moderator, when they send
     `moderateHubChannel`, then `forbidden`; and given a plain
     member who is one, then it succeeds.

### Themes (R61)

258. Given a member, admin, owner or guest, when they create a theme,
     then it succeeds; given a bot, then `forbidden`.
259. Given a guest who owns a theme, when they publish it, then
     `forbidden`; given a member, then it succeeds.
260. Given an account that is not a theme's owner, when it changes,
     deletes, publishes or unpublishes that theme, then `not_found`.
261. Given any signed-in account, a bot included, when it gets an
     unpublished theme by its id, then it succeeds.
262. Given a member or guest, when they add a theme by its id, then
     they own a copy that is not published; given a bot, then
     `forbidden`.
263. Given a member, when they remove a theme from the library or set
     a default theme, then `forbidden`; given an admin, then it
     succeeds and is in the audit log.
264. Given a channel with more members than
     `mentions.broad_mention_limit` and no answer of its own, when a
     `member`-level member, guest or bot posts `@channel`, `@here` or
     `@everyone`, then the message is stored with the words as plain
     text, nobody is notified and the answer carries
     `broad_mentions_refused`; given a manager of the channel or an
     admin who is a member, then the mention is made.
265. Given the same channel with `broad_mentions` set to `anyone`,
     when a `member`-level account posts `@channel`, then the mention
     is made; given `managers` on a channel below the limit, then it
     is left as plain text.
266. Given `mentions.broadcast_managers_only` on and a channel whose
     `broad_mentions` is `anyone`, when a `member`-level account posts
     `@channel`, then it is left as plain text.
267. Given a `member`-level account, when it sets `broad_mentions`,
     then `forbidden`; given an account acting as manager, then it
     succeeds.
268. Given a member who has set `ignore_broad_mentions` in a channel,
     when someone posts `@channel` there, then no mention is written
     for that member; when someone mentions them by name or a group
     they are in, then one is.

### Announcement channels (R66)

269. Given ANN, an announcement channel, and a `member`-level account
     in it, when they post, then `forbidden`; when they react or
     answer a poll, then it succeeds.
270. Given the same, when they reply in a thread or upload a file,
     then `forbidden`; given `announcement_replies` on, then the
     reply and the upload succeed and a top-level post is still
     `forbidden`.
271. Given a guest and a bot at level `member` in ANN, when each
     posts, then `forbidden`; when a manager has set `may_post` for
     each, then each succeeds; when it is taken away, then
     `forbidden` again.
272. Given a `reader` in ANN with `may_post` set, when they post,
     then `forbidden`: a reader posts nowhere.
273. Given an account acting as manager of ANN (a manager; an admin in
     a public ANN, member or not; an admin who is a member of a
     private one), when they list the members, then it succeeds; when
     one of them who is a member posts, then it succeeds. An admin who
     is not a member posts nowhere, here as elsewhere.
274. Given a member of ANN who is not acting as manager, with or
     without `may_post`, when they list the members, then
     `forbidden`; the conversation still tells them the member count.
275. Given a `member`-level account, when it sets `announcement`,
     `announcement_replies`, `lazy_delivery` or someone's `may_post`,
     then `forbidden`; given an account acting as manager, then each
     succeeds.
276. Given a channel shared with other servers, when a manager sets
     `announcement`, then `conflict`; given ANN, when the owner shares
     it, then `conflict`.
277. Given ANN archived, when a manager posts, then `archived`.
278. Given a webhook its managers approved for ANN, when it posts,
     then it succeeds.
279. Given an ad-hoc channel or a sealed chat, when a member sets
     `announcement` or `lazy_delivery`, then `forbidden`.
280. Given ANN made ordinary again, when a `member`-level account
     posts, then it succeeds: no level was changed.

### Channel bans (R72)

281. Given a `member`-level account in PUB or PRIV, when they ban
     anyone, then `forbidden`; given a manager, when they ban a member,
     a guest or a bot, then it succeeds.
282. Given an admin not in PUB, when they ban someone from PUB, then
     it succeeds; given an admin not in PRIV, then `not_found`; given
     one who is a member of PRIV, then it succeeds.
283. Given a manager, when they ban an admin or themselves, then
     `forbidden`; given an admin, when they ban another admin, then it
     succeeds, and the owner, then `forbidden`.
284. Given a guest or a bot at level manager, when they ban a member,
     then it succeeds; at level member, then `forbidden`.
285. Given ADHOC, when its manager bans a member, then it succeeds and
     adding them back is `forbidden`; when a `member`-level account
     bans someone, then `forbidden`. Given a sealed chat, then
     `forbidden`.
286. Given a channel shared with other servers, when a manager bans
     someone, then `conflict`; when they lift a ban made before it was
     shared, then it succeeds. Given a timed-out manager, then
     `timed_out`.
287. Given a member banned from PUB, then they are no longer a member,
     their next message from PUB does not reach them, and seeing,
     reading, joining, searching and listing its members are each
     `not_found`; PUB is not in their channel browser. An admin banned
     from PUB still sees it in the admin console.
288. Given the ban lifted, or past its end, then they may join PUB
     again; they are not brought back by the lifting.
289. Given a member banned from PUB, when a group they are in is added
     to PUB, then they are not brought in.
290. Given a member banned from PUB, when they join a sub-channel of
     PUB, then it succeeds: nothing is inherited.

### Watches (R76, R77)

291. Given a member who shares a conversation with Y, when they watch
     Y while Y is offline, then 201 and a `watch` item that expires in
     48 hours; when Y connects, they get one reminder "@y is around"
     that notifies them, and the watch is done.
292. Given Y around, when someone watches Y, then 200 with
     `around: true` and nothing stored.
293. Given Y has blocked X, when X watches Y, then the answer is
     identical to case 292 and nothing is stored.
294. Given X has blocked Y, when X watches Y, then `forbidden`; given
     a watch of X's for Y, when X blocks Y, then the watch is
     cancelled.
295. Given a watch of X's for Y, when Y blocks X, or Y leaves every
     conversation they share, or Y appears away, then Y becoming
     active fires nothing and the watch stays waiting.
296. Given a watch for oneself, a bot, a removed account or someone
     sharing no conversation, then `forbidden`; by a bot, `forbidden`.
297. Given 5 waiting watches, a sixth is `conflict`; a second for the
     same person is `conflict`; the 21st request in a day is
     `rate_limited`.

### Knock channels and the server broadcast (R74, R79)

298. Given KNOCK, a knock channel, then a `member` outside it sees it
     in the browser and by its id with its name, topic, member count and
     managers, and its welcome text, members and messages are not sent;
     reading, listing members and searching are `not_found` and joining
     `forbidden`.
299. Given KNOCK, then a guest or bot outside it does not see it
     (`not_found`) and cannot knock; members of any role read it.
300. Given PRIV, when a `member` knocks, then `not_found`; given PUB,
     `conflict`; given KNOCK and a member of it, `conflict`.
301. Given a member outside KNOCK, when they knock twice, then one
     request waits and the second answer returns it; its managers are
     told once; a `member`-level member of KNOCK cannot list or answer.
302. Given a request in KNOCK, when its manager adds the one who
     knocked, then they are a member at the default level, read it, and
     are told without who answered; answering again is `not_found`.
303. Given a request declined, then the one who knocked is told plainly
     and is not a member, and may knock again.
304. Given PUB, its manager makes it knock, public again, then private;
     then neither knock nor public is allowed (`forbidden`); a
     sub-channel cannot be made knock (`invalid_value`); an account
     banned from KNOCK no longer sees it.
305. Given a `member`, guest or bot, when they send or clear the server
     broadcast, then `forbidden`; an admin's text must be one line of 1
     to 500 characters and its time a minute to a day (`invalid_value`).
306. Given an admin's broadcast, then every connection, guests' too, gets
     `server.broadcast`, and `getStatusSummary` carries it while it shows.
307. Given the broadcast cleared by the owner or an admin, then every
     connection gets `server.broadcast` without one, the summary no
     longer carries it, and both are in the audit log.

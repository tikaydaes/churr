# Churr: product brief

13 November 2026, second version. It replaces the brief written earlier the same day. Amended
14 November 2026 with the owner's later decisions, listed at the end of the decisions table.

## Summary

Churr is self-hosted team chat that aims to replace Slack and Discord for a group that wants to
own its chat: channels, threads, sealed two-person chats, files, search, calls, bots, company sign-in,
and channels shared between Churr servers. The server is one binary that one person can install
in an afternoon. People chat in apps for phone and desktop, or in a basic web chat the server
serves itself. It is free, licensed AGPL-3.0, and will be listed in the Tikaydaes catalogue as
item TD-005 at $0.00.

The first brief scoped a small single-server chat with a web app. On 13 November the owner
widened it. The decisions are listed in the next section and the rest of this brief is written
to match them. `DESIGN.md` holds the technical design. Most of it has since been built;
`README.md` says what exists, what is being built and what has never been tried.

Version 1 is everything in this brief. It is designed completely and then built completely:
there are no stages and no milestones.

## Decisions made on 13 November 2026

| Question | Owner's decision | What it changed |
| --- | --- | --- |
| Main chat client | Flutter apps. The server also serves a basic web chat at `/chat/` that works only while it is open. | The web app is no longer the product. |
| Scripts and bots | A bot is an ordinary account marked as a bot. Admins create bots. Webhooks exist, and a member can request one for a channel. | Brought in from "after version 1". |
| Bot and webhook design | Take the best of Slack and Discord and boil it down until it is simpler than both. | See "Bots, boiled down" below. |
| Notifications when the app is closed | Yes. Tikaydaes runs a wake-up relay, on by default, so self-hosters need no Apple or Google account. A light status endpoint backs it up. | Bends "no external services". |
| Lost passkey | Recovery codes at sign-up and a one-time link from an admin. | |
| Company sign-in | OIDC and SAML both in version 1. Admins can always use a passkey. The admin chooses whether members may use passkeys or must use company sign-in, and can require a second factor, including authenticator-app codes. | Replaces "passkeys only". |
| Channels shared between servers | In version 1. Two admins link their servers directly, or several servers meet at a hub. The hub is a separate program. | Was "not in version 1". The hub's role was widened on 14 November: see "Hubs own their channels" below. |
| What crosses between servers | Any number of shared channels, plus ad-hoc channels, sealed chats and presence between people who are in a shared channel together. | |
| Database | SQLite by default, PostgreSQL as an option. | Was SQLite only. |
| Public channels | A member can read and search a public channel without joining it. | |
| Calls, end-to-end encryption, link previews, custom emoji and themes | All in version 1. | All four were "not in version 1". |
| Sealing | A sealed chat is its own thing: a direct, end-to-end encrypted conversation between exactly two people. The server cannot read it, it cannot be searched, and it is not in the export. Channels are never sealed. | Replaces the earlier idea of sealing a direct message or private channel. |
| Conversations | Everything that is not a sealed chat is a channel. A channel is public or private, has one or more people in it, and is either named or ad-hoc (no name, titled by its members). People are invited or join, and a newcomer sees all its history and can search it. An ordinary chat between two people is an ad-hoc private channel. | Removes direct messages and group DMs as separate things. |
| App publishing | App Store, Play Store, F-Droid and direct downloads. | Accepts the costs listed below. |
| Importing | Slack import is in version 1. | Was "after version 1". |
| Guests | Kept. | |
| Calls in the web chat | Yes. The web chat joins the same calls as the apps. Browser limits are accepted: permission prompts, HTTPS or localhost only, no screen sharing from phone browsers, nothing when the page is closed, and audio that may stop when an iPhone tab goes to the background. | Was apps only. |
| Unlocking sealed chats on a new device | The passkey is the main way: signing in with it also unlocks sealed history (the passkey "PRF" feature). A recovery phrase is the fallback. Each passkey unlocks separately, and passkeys from stores without this feature can sign in but not unlock. | |
| Sealed chats in the web chat | Yes. The browser keeps its keys until the member signs out. | Was apps only. A browser trusts the server for its code, and the README says so. |
| Threads | One way to reply, one level deep. A reply can quote the selected text or the whole message, and it always goes into that message's thread. Each person chooses to see threads tucked under their parent or flattened into the channel with links back up. Replies notify only the thread's followers, and anyone can ignore a thread to hide its replies and marker. The parent message never moves; a single moving marker in the channel shows where the latest reply happened. | |
| Long threads | A thread can be moved out into its own named channel by its starter or a channel manager. It is a move, not a copy, and it is not available in channels shared with other servers. The parent becomes its first message and the replies become ordinary messages that can have threads of their own. The old channel keeps the parent with a pointer to the new channel. | |
| People seeing threads differently | Accepted. The view is a personal preference with a per-channel override, and nobody sets it for anyone else. | |
| Channel managers | A channel's managers can make any member a moderator or a manager of just that channel. Managers hold the channel (name, topic, pins, levels, settings, archive); moderators keep the peace (let people in, remove and ban them, delete messages, time people out, slow mode, lock, reports). A guest is a guest in every channel and is never either. Admins and owners are neither unless made one. | |
| Groups | Named sets of people such as `@design`. Mention one to notify its members, or add one to a channel at a level so its members are in that channel. Admins manage groups, a group can be open for people to join themselves, and company sign-in can fill groups from the identity provider. No permission matrix. | New. |
| Admins and private channels | Admins can see the name, members and managers of every named private channel, and can set its section, parent and retention. They cannot read it. An owner can appoint a manager of any channel, and add someone to a private channel by owner override, which every member is shown. Ad-hoc channels and sealed chats stay invisible to them. | Was: admins could not see a private channel existed. |
| Admins and membership | An admin or owner is a member only of channels they join. Admin powers work without membership and give no power inside a channel, so no channel is added to their list, unread counts or notifications because of their role. | Unlike Discord. |
| Channel modes | Each person sets what notifies per channel: Any activity, Mentions (the default for named channels), or Mute for a duration. The badge counts what would notify. Ad-hoc channels and sealed chats default to Any activity. | New. Later split into two settings: see "Notify and show, separately". |
| Do not disturb | Account-wide, for a duration or until turned off. Stops all notifications, including call rings and phone wake-ups. Badges keep counting and other people see the mark. | New. |
| Quiet hours | A daily schedule that puts the account in do not disturb. It is independent of a manual do not disturb: neither shortens the other. The next quiet period can be skipped in advance, and turning do not disturb off during quiet hours ends that period. | New. |
| Statuses | Automatic presence (online, away, offline, in a call), plus a status the person sets: an emoji, a short line of text, an optional end time, and switches to pause notifications and appear away while it lasts. Statuses can be saved as presets, by admins for everyone or by each person. | New. |
| In a meeting and out of office | Two fixed availability values with an end time, set by the person or through the API, so a calendar integration can set them. | New. |
| Server integrations | A credential for a program that works on the server as a whole, such as a calendar integration that sets availability for everyone. It is not a bot and not an account: it is in no channel and can read no messages. Admins create them and choose what each may do. | New. Bots keep their one rule. |
| Appear away | A person can show as away while active, for a duration or until turned off. | New. |
| Notify anyway | A sender can push one message through someone's do not disturb. It is marked on the message, limited to cases where the person would normally be notified, and each person can turn it off. | New. |
| Starting channels | A built-in group, `@everyone`, holds every member (not guests or bots). A new member is in whatever channels `@everyone` has been added to. | Uses groups; no separate "default channel" setting. |
| Who can invite | Admins, and members too if the admin allows it. Guest invites are always admin-only. | |
| Retention | A server-wide period, which a channel's managers can shorten for their channel and admins can set per channel from the admin console. | Was one period for everything. |
| Edits, deletes and purges | A short window for quick corrections that leave no trace. After that, edits and deletes keep history that anyone in the channel can open. An admin policy sets how long editing stays possible. An author can ask for a purge. In a private or ad-hoc channel it happens at once and no admin sees anything. In a public channel the history is hidden from everyone but admins at once, and an admin chooses to show it again, keep it hidden, or erase the content and leave only the record; the author is not told which. Admins can delete and purge directly. | Was: edits and deletes erased the old text immediately. |
| File storage | Disk by default, with S3-compatible object storage as an option. Clients still download through the server. | Was disk only. |
| Message format | Text in a fixed Markdown subset, plus files, cards and one form per message, drawn the same by every client. Code blocks are syntax-highlighted by language name. | Replaces "Markdown and buttons". |
| Polls and forms | A form is the one interactive mechanism and the server collects the answers. A poll is a form with one choice field and a live tally. | New. |
| Checklists | Anyone in the channel who can post can tick or untick an item. Who and when is shown, and every change is kept per item. | New. |
| Built in instead of plugins | The common reasons people install Slack apps and Discord bots are covered by the server: polls, forms, check-ins, reminders, send later, saved replies, welcome texts, join-a-group buttons, tickets, moderation. GIF search, music, games, levels and AI bots are left to bots. | New. |
| Sealed chats carry | Text, files and reactions, all encrypted on the device. No cards and no forms. | |
| API documentation | An OpenAPI file plus a prose guide. | |
| Public and private | A public channel can be made private. A private channel can never be made public. | |
| Locking a private channel | A moderator or manager can lock it: nobody can be added or join afterwards, and it cannot be unlocked. | New. |
| Channel names | Not unique. Any number of channels may share a name; they are told apart by where they sit, when they were made, how many members they have and who manages them. | Reverses the earlier "unique across the server". It also removes the name leak. |
| Free-form channel names | Any script, spaces and capitals, up to 64 characters. | Was lowercase letters, digits, hyphens and underscores. |
| Linking a channel | Typing a channel's name always offers to link it. A link points at the channel by its identifier, so it survives renames; `#games` typed without choosing is plain text. | Replaces automatic `#name` links. |
| Sub-channels | A channel can have a parent channel, one level deep, shown as a tree. Nothing is inherited. Threads moved out to a channel, and ticket channels, become sub-channels of where they came from. | New. |
| Erasing | Deleted from the database, the search index and the file store is the standard. | |
| Notify anyway, refined | It always appears to work, and quietly does nothing when the person would not have been notified, so it cannot reveal who has muted or blocked whom. | |
| Sealed-chat encryption | Churr's own scheme: a fresh secret encrypted to each device whenever devices change, from standard parts. Written in Dart and JavaScript. No MLS library, no Rust. Needs outside review before anyone relies on it. | Replaces MLS through a Rust library. |
| Who can lock | A channel's moderators and managers. A lock is one-way and makes the channel unjoinable; someone who leaves cannot return. | |
| Notify and show, separately | Each person sets two things per conversation: what notifies (any activity, mentions, nothing, or mute for a time) and whether it shows in their sidebar (always, on activity, on notification, hidden). Hidden only removes it from the sidebar; it can still notify. The default for "show" is an app preference on the device. | Replaces the single five-value mode. |
| Sidebar layouts | Merged tree of servers, a server rail, or a hybrid the person arranges by drag and drop across servers. | New. |
| Several servers in one app | One notifications list, one search and one quick switcher across all signed-in servers; status and do not disturb set once; quiet hours and muting per server. | New. Done in the apps; servers stay independent. |
| No stages | The whole project is designed in full and then built in full. No milestones. | Replaces the seven milestones. |
| Moving a two-server channel onto a hub (14 November) | A direct link still joins exactly two servers. To bring in a third, a channel shared directly is moved onto a hub both servers are on, by a transfer the other server's owner must accept. Nothing is lost, duplicated or reordered, and neither server ends up with a second channel. If any step fails the channel carries on as before. | New. |
| Hubs own their channels (14 November) | The hub keeps a record of every channel on it (name, description, listing mode, member servers, managers, moderators) and is the only one that signs changes to it. Managers, who are accounts on member servers, admit servers and run the channel; moderators delete messages for everyone, admit people to private channels and remove people. A channel can be unlisted, listed for servers to ask to join, or open; listed channels appear in the hub's directory. The hub still never holds a key and cannot read content. Ad-hoc channels and sealed chats that cross a hub are not the hub's. | Was: the hub only relayed, and the server that shared a channel owned it. |
| Shared-clock ordering (14 November) | Every shared conversation, over a direct link or a hub, is ordered by a clock its servers share, measured over the link. Nobody hands out positions. A message shows at once in its place; "unsent" means only that the link has not yet acknowledged it. | Replaces ordering by a sequencer's numbers. |
| Trunks between hubs (14 November) | Hubs can be joined into a cluster so that a channel reaches servers on several hubs. Each channel has a home hub; during a split each side keeps working among its own servers. | Was left for later. |
| Themes (14 November) | A theme is a named set of colours, dark or light. People make them in an editor in the apps and the web chat, contrast is enforced, themes follow the person between devices, can be published to the server's theme library and shared as a link or a small file. Admins set the server's defaults. The web pages follow the approved style guide in both dark and light. | Replaces the accent-colour setting and "web pages are dark only". |

## Who it is for

Churr is for groups of roughly 5 to 5,000 people who want chat they own: small teams,
open-source projects, clubs, friend groups, families, and companies that already have a sign-in
system and want chat to plug into it.

They have two options today and neither fits. Hosted chat charges per seat, keeps the history on
someone else's servers, holds features such as guests, shared channels and company sign-in for
paid tiers, and can change its terms. The existing self-hosted servers usually need a separate
database and several services to run, and some also hold features back.

Churr's promise: every feature in the box, no charge per person, one process to run, one folder
to back up.

## House rules

These apply to every Tikaydaes product. Three of them were amended for Churr by the owner's
decisions above, and the amended wording is what the build follows.

- **One static binary for the server.** Go, no cgo, cross-compiled. All state lives in one data
  directory, or in that directory plus a PostgreSQL database if the operator chooses one. The hub
  and the push relay are separate small programs built from the same repository.
- **Nothing outside is required** (amended). A Churr server runs with every outside connection
  switched off. The features that reach outside are listed in the security section, each has a
  switch, and the first-run setup shows them to the admin.
- **API first.** The apps, the web chat, bots and scripts use the same HTTP API. Nothing is
  reachable only through one client.
- **Passkeys first** (amended). Passkeys are the default and admins can always use one. A server
  can add company sign-in over OIDC or SAML. There are no passwords, and Churr never asks for or
  sends email.
- **Private by default** (amended). No telemetry and no analytics. The web pages load nothing
  from other people's servers. The push relay is the one outside connection that is on by
  default, and it carries no message content.
- **Runs behind a reverse proxy or alone.** It ships a Dockerfile and a compose file that publish
  on localhost, and it can also fetch its own certificate when run directly.
- **AGPL-3.0**, with a sponsor link to github.com/sponsors/tikaydaes.
- **It prints its catch.** The README says plainly what Churr does not do and what each operator
  can see, and says how it was built.

## The parts

| Part | What it is | Who runs it |
| --- | --- | --- |
| `churr` | The server: API, live connection, call media, sign-in and admin pages, basic web chat. | Each community |
| Churr apps | One Flutter codebase for Android, iPhone, macOS, Windows and Linux. One app can be signed in to several Churr servers. | Each person |
| `churr-hub` | Where several servers meet to share channels. It owns the channels shared through it and keeps their record and directory, and can be joined to other hubs by trunks. It has no members and cannot read what it carries. | Whoever convenes a group of servers |
| `churr-relay` | The push relay. It holds the Apple and Google push keys and sends wake-ups that contain no message content. | Tikaydaes, or anyone who builds their own apps |

## Version 1 scope

Everything in this table ships in version 1.

| Area | What version 1 does |
| --- | --- |
| Community | One server is one community. The app can hold several servers side by side. |
| Channels | Public and private channels with a topic, grouped into sections the admins define, with sub-channels one level deep. Names need not be unique. Create, join, leave, archive, pin messages. Each channel has managers who hold it and moderators who keep the peace. An announcement channel is a channel where members can read and only managers, and the members they allow, can post. |
| Ad-hoc channels | Start a chat with one or more people, or only yourself, and you get a private channel with no name, titled by its members. Anyone in it can add people, and whoever is added sees the whole history. Naming it makes it a regular channel; nothing else changes. |
| Groups | Named sets of people with a handle such as `@design`. Mention a group to notify its members. Add a group to a channel as reader, member, moderator or manager and its members are in that channel, staying in step as the group changes. Groups can be open to join, and can be filled from company sign-in. |
| Message format | One format every client draws the same way: text, files, cards and a form. Text is a fixed Markdown subset with tables, lists, tick-box lists and syntax-highlighted code blocks. |
| Checklists | Tick-box lists anyone in the channel can tick and untick. A ticked item is struck through and shows who and when, with full history per item. |
| Forms and polls | One interactive mechanism. A form has fields and buttons and the server collects the answers. Presets: `/poll`, `/rsvp`, `/signup`, `/approve`, `/form`. Polls can be named or anonymous, single or multiple choice, with a closing time. |
| Built-in tools | Reminders, send later, recurring check-in forms, saved replies and custom commands, welcome texts, join-a-group buttons, support tickets, bookmarks. |
| Moderation | Time-outs, slow mode, a word filter, message reports, bans, personal blocking. |
| Messages | Markdown text, edit and delete with visible history and purge on request, emoji reactions, @mentions, and replies in threads one level deep, with an optional quote of the selected text. Each person can view threads tucked under the parent or flattened into the channel. A long thread can be moved out into its own channel. People follow the threads they take part in, a reply notifies only its followers, and a Threads view lists followed threads with unread replies. The parent message stays where it was posted; a one-line marker at the point of the latest reply shows the channel that the thread is active. |
| Files | Up to ten files per message. Images, audio and video show inline; text files show their first lines. A size limit the admin sets. Stored on disk or in S3-compatible storage. |
| Search | Full-text search across the messages a person is allowed to see. |
| Notify and show | Per person, per conversation, two separate settings: what notifies (any activity, mentions, nothing, or mute for a time), which also decides the badge; and whether it shows in the sidebar (always, on activity, on notification, or hidden). |
| Status and do not disturb | Presence, a personal status (emoji and text, optionally timed), and an account-wide do not disturb for a duration or until turned off, with optional daily quiet hours, appear away, in-a-meeting and out-of-office availability that an API can set, status presets and notify anyway. |
| Live updates | New messages, edits, typing and presence arrive without a refresh. Unread counts and a read position per conversation. |
| Accounts and roles | Owner, admin, member, guest and bot; an instance can have several owners. Invite links. Recovery codes and one-time recovery links. Admins can remove people, and delete and purge messages. New members start in the channels `@everyone` is in. |
| Admin | Server settings, sections, a message retention period with per-channel overrides, the edit policy, purge requests, an audit log, and a full export of the community's data. |
| Web | Sign-in, account and admin pages, and a basic web chat at `/chat/`: text, threads, files, search and reactions while the page is open, with calls and sealed chats. Closing the page is like quitting the app: nothing arrives until it is opened again. |
| Bots and webhooks | Bots are accounts with a token. Incoming webhooks that accept Slack, Discord and GitHub payloads and show them as cards. Bot commands and forms. Personal tokens for a member's own scripts. Server integrations for programs that work on the whole server. |
| Custom emoji | Uploaded emoji for the community, usable in messages and reactions. |
| Link previews | The server fetches a title, description and image for a pasted link. Off until the admin turns it on. |
| Slack import | A command that reads a Slack export and creates its channels, people, messages, threads, reactions and files. Imported people claim their account with an invite link. |
| Apps | Everything in the rows above except admin, plus a tray icon on desktop. |
| Themes | Dark, light and a high-distinction pair built in, in the apps, the web chat and the web pages. A theme editor in the apps and the web chat with contrast enforced, a theme library per server, theme links drawn as cards, and a small file to carry a theme to another server. Admins set the server's default dark and light themes. |
| Notifications | Desktop apps stay connected. Phones are woken through the push relay, or through UnifiedPush or a foreground service on Android. A light status endpoint reports unread and mention counts. |
| Company sign-in | OIDC and SAML. Sign-in policy for members: passkey, either, or company sign-in required. Optional second factor: a passkey, an authenticator-app code, or proof of multi-factor from the identity provider. |
| Shared channels | Channels shared between servers, linked directly or through a hub, ordered by a clock the servers share. A channel on a hub belongs to the hub and is run by its managers and moderators; it can be unlisted, listed in the hub's directory for servers to ask to join, or open. A channel shared directly can be moved onto a hub to bring in a third server. Hubs can be joined by trunks. People on other servers show as `name@server`. Ad-hoc channels, sealed chats and presence work between people who share a channel. |
| Calls | Voice, video and screen sharing in any conversation, carried by the server itself, in the apps and in the web chat. |
| Sealed chats | A direct conversation between exactly two people, end-to-end encrypted from its first message. The server stores and delivers ciphertext it cannot read. No search, no export, no bots, no link previews. One sealed chat per pair of people, and nobody can be added. |

Targets to test against. None of these is a measured result.

- 500 members on a small virtual server (1 CPU, 1 GB of memory) with SQLite.
- 5,000 members on a mid-sized server (4 CPUs, 8 GB) with PostgreSQL.
- A call with 25 people on audio and 8 on video on the mid-sized server.

### Bots, boiled down

Slack has apps, scopes, incoming webhooks, slash commands, an events API and a layout language.
Discord has bot users, a gateway, intents, roles, interactions and channel webhooks. Churr keeps
three ideas and drops the rest.

1. **A bot is a member with a token.** It uses the same API and the same live connection as a
   person. Nothing in the API is bot-only.
2. **A bot's reach is the channels it is in, and what it can do there is its level in that
   channel:** reader, member or manager, the same three levels people have. There are no scope
   lists. To let a bot post in a channel, add it. To stop it, remove it.
3. **A webhook is the smallest bot:** a secret address that posts into one channel and can read
   nothing. It accepts the JSON that Slack and Discord webhooks accept, so a tool that can post
   to either can post to Churr by changing the address.

Bots can also offer commands (typed as `/name` in the message box) and post forms, with fields
and up to five buttons. Commands, answers and button presses reach the bot over its live connection, so a bot
needs no public address and the server never calls out to one. Admins create bots. Any member
can request a webhook for a channel they are in, and an admin approves or declines it.

## Not in version 1

Each of these is left out on purpose.

| Left out | Why |
| --- | --- |
| Open federation, and federation with Matrix or other systems | Servers share channels only where their admins have agreed to. An open network multiplies the abuse surface. |
| Running one community across several servers | One process per community. No clustering and no failover for a server. (Hubs can be joined by trunks, but that links communities; it does not spread one.) |
| Outgoing webhooks, an app directory, a message layout language | Bots connect out to the server, so the server never calls a bot. Messages are text, files, fixed cards and forms. |
| Call recording, and calls that span servers without a shared channel | Recording needs storage and consent rules that are a product of their own. |
| Sealed channels | A sealed chat is always exactly two people. Every channel can be read by its server, which is what lets newcomers see history and lets search work. |
| Automatic account removal from an identity provider (SCIM) | A person removed at the identity provider can no longer sign in, and sessions from company sign-in are short. Full provisioning is later work. |
| Importing from Discord | Discord has no official export. Slack import is in version 1; Discord is the first candidate after it. |
| A public Tikaydaes-run chat server | It would mean reading rights over other people's conversations and a moderation burden. Tikaydaes runs only the push relay. |

## Architecture and stack

    Churr apps (Flutter) ─┐                     ┌────────────── churr (one Go binary) ──────────────┐      ┌── data directory ──┐
    Web chat and admin ───┼─ HTTPS + WebSocket ▶│ HTTP API   Live hub     Permissions  Search       │      │ churr.db (SQLite)  │
    Bots and scripts ─────┘                     │ Sign-in    Bots, hooks  Links        Janitor      │ ───▶ │ files/   keys/     │
    Apps in a call ───────── media, one port ──▶│ Call media (SFU)        Link previews             │      └────────────────────┘
                                                └──────┬─────────────────────┬──────────────────────┘        or PostgreSQL for
                                                       │ content-free        │ signed, encrypted             the database part
                                                       ▼ wake-ups            ▼ events
                                                  churr-relay           another churr server,
                                                  (Apple, Google)       directly or through churr-hub

- **Server language:** Go, built without cgo so it cross-compiles to one static binary.
- **Storage:** SQLite in the data directory by default, PostgreSQL as an option, behind one
  storage interface with one shared test suite. Search uses each database's built-in full-text
  index. Uploads and keys are plain files in the data directory, and uploads can be put in
  S3-compatible object storage instead.
- **Transport:** an HTTP JSON API, plus one WebSocket per client for live events. Bots use the
  same two.
- **Apps:** Flutter, one codebase in `app/`. They sign in through the system browser, the way
  Codemail's apps do.
- **Web:** HTML, CSS and JavaScript embedded in the binary, with no framework and no build step,
  so the server builds with the Go toolchain alone.
- **Calls:** a media server inside the binary (Pion, which is pure Go), on one UDP port with a
  TCP fallback.
- **Sealing:** Churr's own small scheme, built from five standard cryptographic parts that every
  platform already has. It is written in Dart in the apps and JavaScript in the web chat; the
  server only stores and delivers ciphertext. It is not the MLS standard, which is built for
  large groups and would have meant a second language and native builds for every platform. In
  the web chat the server also supplies the code that does the encrypting, which is weaker than
  the apps and is said plainly in the README.
- **Certificates:** fetched and renewed by the binary when it runs directly on the internet;
  plain HTTP on localhost when a reverse proxy is in front.
- **Repository layout:** `cmd/churr`, `cmd/churr-hub`, `cmd/churr-relay`, `internal/...`, `app/`,
  `Dockerfile`, `compose.yaml`, `DESIGN.md`, `docs/API.md`.

## Security, privacy and the catch

The catch that goes on the catalogue listing: **whoever runs a Churr server can read everything
on it, private channels included, except sealed chats, which are always between exactly two
people.** A shared channel can be read by the operator of every server it is shared with. A
hub's operator cannot read it, but sees its name, its managers and moderators and which servers are in it,
and decides which servers are let in. That is why Churr is built for a group to run for itself,
and the README must say so in its first screen.

One limit on sealed chats that the README must also state: unlocking sealed history with a
passkey happens on a page the server supplies, in the apps as well as in the browser, so a
dishonest or compromised server could capture the key at that moment. The recovery phrase and
approval from another device do not have this weakness in the apps.

What each operator can see:

| Operator of | Can read | Cannot read |
| --- | --- | --- |
| Your Churr server | All messages and files, who talks to whom and when | The content of sealed chats |
| A server you share a channel with | That channel, and ad-hoc channels between its members and yours | Anything else on your server |
| A hub | Which servers exchange how much, and when. For each channel the hub owns: its name, description, listing mode, managers, moderators and member servers. The operator can admit and remove servers and appoint managers | Any content, or who the members are. Servers encrypt to each other before the hub sees it, and the hub never holds a key. An operator could admit a server they control, which would get the key from then on; every member server sees that happen |
| The push relay | A device's push address, which server woke it, and when | Any content, any name, any channel, or whether it is a call |

Outside connections a server can make. Everything else stays inside.

| Connection | When | Default |
| --- | --- | --- |
| Certificate renewal | When the server runs without a reverse proxy | On in that mode |
| Push relay | A member who uses a phone app is mentioned or messaged while away | On |
| UnifiedPush | An Android phone without Google services registered an address to be woken at | Off |
| Link previews | A member posts a link | Off |
| Object storage | The operator chose S3-compatible storage for uploaded files | Off |
| Identity provider | Company sign-in is configured | Off |
| Linked servers and hubs | An admin has linked a server or joined a hub | Off |

Requirements:

- **Permissions are enforced by the server**, on every read, search result, file download and
  live event, for people and bots alike. Nobody receives a message from a private channel they
  are not in.
- **Messages are shown safely.** Markdown is rendered without running scripts or loading remote
  images, the way Codemail displays mail.
- **Uploads are served as downloads from the server's own address**, with content types it sets,
  so an uploaded file cannot run as a page.
- **Link previews cannot be used to probe private networks.** The server refuses addresses that
  resolve to private, loopback or link-local ranges, and limits size, time and redirects.
- **Events from another server are checked, not trusted.** Each is signed by the server it came
  from, and a server can only speak for its own members.
- **Abuse limits exist from day one:** rate limits on sign-up, sign-in, posting, uploads,
  webhooks and linked servers, plus invite-only as the default registration mode.
- **Deleting is not erasing until it is purged.** An edited or deleted message keeps its history
  until an admin purges it or retention removes it, and the apps say so where people delete.
- **Retention is real on your own server.** When the retention period deletes a message, it is
  gone from the database, the search index and the files folder. A deletion is also sent to
  linked servers, which are expected to honour it and cannot be forced to.
- **The audit trail is small and visible.** Admin actions are logged where admins can see them.

## Deliverables and definition of done

The team hands over a public repository at github.com/tikaydaes/churr containing:

1. The server and its embedded web pages, building to one binary with a single command, plus the
   hub and the relay.
2. The Flutter apps in `app/`.
3. A Dockerfile, a compose file that publishes on localhost only, and an example environment file.
4. `DESIGN.md` and an API reference in `docs/API.md`.
5. A README covering what it does, the catch, a local trial in two commands, a real install,
   backup and restore, and how it was built.
6. Automated tests, run on every push, that include the permission rules in the security section
   and run the storage tests against both databases.
7. A catalogue entry for tikaydaes.dev: name, one-line pitch, description, three spec rows and
   the catch.

Version 1 is done when all of these are true:

- [ ] A new person can go from an empty server to a working community with an invited second
      member by following only the README.
- [ ] Every row of the scope table works in the apps on Android, iPhone, macOS, Windows and
      Linux, and every row the web chat covers works in current Firefox, Chrome and Safari.
- [ ] A test proves that a member, a guest and a bot cannot read, search, download or receive
      live events from a private channel they are not in.
- [ ] A test proves the server holds no readable text or file from a sealed chat, that search
      returns nothing from one, and that the export contains nothing from one.
- [ ] A test with two servers and a hub proves a server receives nothing from a channel that is
      not shared with it, and rejects an event signed by the wrong server.
- [ ] Copying the data directory to another machine and starting the binary restores the
      community.
- [ ] Each target in the scope section has been load-tested and the result is written in the
      README, whatever it is.
- [ ] With every switch in the outside-connections table off, the running server makes no
      outbound network requests.
- [ ] An iPhone and an Android phone with the app closed show a notification for a mention
      within a minute, with the relay on.

## What this now costs to run

The first brief cost nothing to operate. These are new and fall on Tikaydaes, not on self-hosters.

- **An Apple developer account** (yearly fee) and a Google Play account, because push only
  reaches apps signed and published by the holder of the push keys. Codemail avoided the app
  stores; Churr cannot if iPhone notifications are to work.
- **A small server for the push relay**, and the duty to keep it up. If it is down, phones are
  not woken.
- **App store review** for each release of the apps.

## Open decisions for the owner

Each has a default the team follows unless the owner says otherwise.

| Decision | Default | What it trades |
| --- | --- | --- |
| Who approves webhook requests | Admins, who can hand this to channel managers in settings | Fewer bottlenecks against less central control. |
| Session length for company sign-in | 24 hours, then sign in again | Stands in for automatic account removal until SCIM exists. |
| The relay's address and the apps' identifiers | Not chosen | Both are baked into the published apps and are hard to change later. |
| What a standard Slack export lacks | Import what the file contains | A standard Slack export holds public channels only. Private channels and direct messages come only in the export Slack gives to its higher paid plans. Slack direct messages are imported as ad-hoc channels. |

## Name and brand

The product is called Churr, after the low trilling call of a nightjar. Its mascot is a nightjar
in headphones sitting on a server box.

- **Name checks done (13 November 2026).** The name `churr` was unclaimed on npm, PyPI and
  crates.io, and no software product called Churr turned up in a web search. The nearest was a
  fan app spelled Churrrrr.
- **Name checks not done.** No trademark register has been searched, and neither app store has
  been searched for the name. The domains churr.chat, churr.dev and churr.org had no nameservers,
  which suggests they are unregistered but does not confirm it. Nothing has been registered or
  reserved yet.
- **Handles.** The GitHub username and Docker Hub namespace `churr` belong to other people, so
  everything is published under `tikaydaes/churr`.
- **Sticker.** The chosen catalogue sticker is `art/churr-sticker.png` in this folder (also
  `art/p-churr-b.png` in the tikaydaes.dev repository, with the alternative `p-churr-a.png`
  beside it).
- **Sounds.** The ring and the start-up sound are synthetic, built from measurements of a
  European nightjar's churr: about 28 clicks a second near 1,870 Hz, dropping briefly to about
  46 a second near 1,450 Hz. The chosen files are `art/sounds/churr-ring` (the softer version)
  and `art/sounds/churr-startup`, written by `tools/sounds/churr_synth.py`. No recording is used.
- **Look.** The apps, the web chat and the web pages follow the style guide the owner approved
  on 14 November 2026 (`docs/style-guide.html`, with `docs/STYLE.md`): the tikaydaes.dev colours,
  flat fills, no rounded corners, no gradients or soft shadows, in dark and light alike. People
  can make their own themes within its contrast rules.

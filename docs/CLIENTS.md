# Clients

The Flutter apps and the web pages the server serves. `DESIGN.md`
section 16 is the outline this document fills in. What a message looks
like is in `MESSAGE-FORMAT.md`; this document says where it is drawn.
Nothing here is built yet.

`<APP-ID-NOT-CHOSEN>` stands for the apps' bundle and application
identifier, which the owner has not chosen.

Part one (sections 1 to 14) is the apps. Part two (sections 15 to 22)
is the web.

---

# Part one: the apps

## 1. Targets

One Flutter codebase in `app/` builds five apps.

| Target | Form | Notes |
|--------|------|-------|
| Android | Phone and tablet | Two builds: Play Store (wake-ups through FCM) and F-Droid (no Google libraries) |
| iPhone | Phone; runs on iPad as the same adaptive app | Has a notification extension and a screen-broadcast extension |
| macOS | Desktop, menu-bar icon | |
| Windows | Desktop, tray icon | |
| Linux | Desktop, tray icon | GTK. X11 and Wayland |

The apps are the full product: several servers, every conversation
feature, calls, sealed chats, notifications and themes. They contain
no admin screens. "Server settings" opens the admin console in the
system browser.

There is no Flutter web build. The web chat is separate, plain
JavaScript (part two).

## 2. Project layout

    app/
      pubspec.yaml
      analysis_options.yaml
      README.md
      lib/
        main.dart                  starts the app
        background_service.dart    entry point of the Android foreground service
        push_background.dart       entry point run when a push arrives with no screen
        core/                      plain Dart, no Flutter imports
          api/                     HTTP client, errors, one file per API area
          models/                  accounts, conversations, messages, forms, calls
          oauth.dart               sign-in through the system browser, PKCE, pairing
          live.dart                the WebSocket: connect, heartbeat, reconnect
          sync.dart                catch-up by sequence number, applying events
          db/                      local database: tables and queries
          markdown/                Churr Markdown parser and node types
          highlight/               language list and grammar loading
          notify/                  wording of notifications, in the reader's language
          calls/                   signalling, room state, layout sizes
          sealed/                  sealed-chat encryption, in Dart (section 5)
          session_store.dart       tokens in the platform key store
        state/                     controllers the screens listen to
        platform/                  notifications, push, tray, badge, deep links,
                                   start at login, screen capture
        ui/
          app.dart                 root widget, routes
          theme/                   colour tokens, text styles, the two themes
          layout/                  the adaptive shell: rail, list, panes
          screens/                 one folder per screen in section 9
          message/                 text, files, cards, forms, reactions, markers
          widgets/                 shared pieces: avatar, badge, menus, pickers
        l10n/                      wording, one file per language
      android/  ios/  macos/  windows/  linux/
      ios/NotificationService/     the notification extension (Swift)
      ios/BroadcastUpload/         the screen-broadcast extension (Swift)
      assets/
        fonts/                     Archivo, Martian Mono, with their licences
        icons/  sounds/  tray/
        emoji.json                 the standard emoji list and short codes
      test/                        unit and widget tests
      integration_test/            tests that run the whole app against a server
      tool/                        icon generation, the Play-build switch

`lib/core` has no Flutter in it, so the background entry points can
use it without a screen, and its tests run as plain Dart.

## 3. Packages

All are published on pub.dev. Those marked † are listed again in
section 25 because their current state should be checked before the
build depends on them.

| Need | Package | Notes |
|------|---------|-------|
| HTTP | `http` | The API client |
| WebSocket | `web_socket_channel` | The live connection. Maintained by the Dart team |
| Local database | `drift`, with `sqlite3` and `sqlite3_flutter_libs` † | SQLite with typed queries and streams the screens can watch |
| Secure key storage | `flutter_secure_storage` | Tokens and the local database key, in the Keychain, the Android Keystore, the Windows credential store or libsecret |
| Preferences | `shared_preferences` | Small settings that are not secret |
| WebRTC | `flutter_webrtc` | Calls on all five targets, with screen capture and frame encryption † |
| Notifications | `flutter_local_notifications` | Showing notifications on every target |
| Push on Android (Play build only) | `firebase_messaging` † | Receives the FCM data message. Added by `tool/play_flavor.dart`; the repository as checked in has no Google library |
| UnifiedPush | `unifiedpush` † | The connector published by the UnifiedPush project |
| Foreground service | `flutter_foreground_task` | Android stay-connected mode, as in Codemail |
| Tray | `tray_manager` | Tray and menu-bar icon |
| Window | `window_manager` | Hide to tray, restore, always-on-top ring window |
| Start at login | `launch_at_startup` † | Desktop |
| Sign-in through the system browser | `flutter_web_auth_2` | Opens the server's own sign-in page and returns the redirect. The app does the OAuth and PKCE steps itself with `crypto` |
| Deep links | `app_links` | `churr://` links and, on phones, verified web links |
| Opening links | `url_launcher` | Links always open outside the app |
| QR scanning | `qr_code_scanner_plus` † | Pairing. Chosen, as in Codemail, because it needs no Google library |
| QR display | `qr_flutter` | Device approval |
| Syntax highlighting | `re_highlight` † | A Dart port of highlight.js with the same grammars and token names as the copy vendored in the web chat |
| Sealed-chat cryptography | `cryptography` † | X25519, Ed25519, HKDF-SHA-256, AES-256-GCM and SHA-256 in Dart (section 5). `cryptography_plus` is its maintained fork, if the original has gone quiet |
| Encrypted local store | `sqlcipher_flutter_libs` † | The sealed store, a SQLite file encrypted at rest (section 6) |
| Files | `file_picker`, `image_picker`, `desktop_drop` † | Attaching, camera, drag and drop |
| Inline audio and video | `media_kit` † | Plays the web formats on all five targets |
| Paths, version | `path_provider`, `package_info_plus` | |
| Keep the screen on in a call | `wakelock_plus` | |
| Sounds | `audioplayers` | Ring and notification sounds |

Not used, on purpose:

- No passkey package. The apps never handle passkeys or access keys: sign-in happens
  on the server's own page in the system browser, so every sign-in
  method works without app code (`DESIGN.md` section 9).
- No analytics, crash reporting or remote configuration package.
- No Markdown package. Churr Markdown is a fixed subset with its own
  parser (section 18), because a general Markdown package would draw
  things the format forbids.
- No state-management package (section 4).

Where a platform gives no package worth trusting, the app has a small
platform channel of its own in `lib/platform`: the raw APNs device
token on iPhone, the icon badge on desktop, and passing the tapped
notification from the iPhone extension to the app.

## 4. State

The local database is the single source of truth for everything that
came from a server. Nothing the screens show about conversations or
messages lives only in memory.

    server ── events ──▶ sync ──▶ local database ──▶ watched queries ──▶ screens
                                        ▲
    screens ── actions ──▶ controllers ─┘── API requests ──▶ server

- **Reads.** A screen watches a database query and redraws when its
  rows change. A new message appears because sync wrote it, whichever
  device sent it.
- **Writes.** A controller writes the change to the database first
  (a message in the outbox, a read position, a reaction marked
  pending), then calls the API. The server's event confirms or corrects
  it. A failure marks the row and the screen offers a retry.
- **Controllers** are plain `ChangeNotifier` classes in `lib/state`,
  handed down the widget tree with `InheritedNotifier`. They hold only
  what is not in the database: which conversation is open, the text
  being typed, the scroll position, call state, connection state.
- One `AppState` at the top owns a `ServerState` per signed-in server.
  A `ServerState` owns that server's database, API client, live
  connection and controllers.

This is Codemail's approach scaled up. It uses nothing outside Flutter
and drift, and it means the foreground service and the push handlers,
which have no widgets, update the same database the screens watch.

## 5. Sealed-chat encryption in the clients

Sealed chats use Churr's own small scheme, set out in
`docs/SEALED.md`, which is the authority on it. It is built from five
primitives and nothing else: X25519, Ed25519, HKDF-SHA-256,
AES-256-GCM and SHA-256. Every platform Churr runs on has these, so
each client implements the scheme in its own language and there is no
shared native library.

| Where | Written in | Uses |
|-------|------------|------|
| The apps | Dart, in `app/lib/core/sealed/` | The `cryptography` package |
| The web chat and the server's sign-in, account and unlock pages | Plain JavaScript, in `internal/web/static/shared/sealed/` | The browser's Web Crypto |
| The iPhone notification extension | Swift, in `app/ios/NotificationService/` | CryptoKit. It only checks and decrypts a message |
| The server's tests | Go, in `internal/sealed/ref` | The standard library. A reference, never linked into the server |

What each full implementation (Dart and JavaScript) does:

- device keys, the account key, and the signed device list: making,
  checking, pinning;
- the one "seal to a public key" construction;
- key epochs: making an epoch record (a fresh epoch secret sealed to
  every device on both accounts' lists, signed, chained to the record
  before), checking one, and starting a new epoch when a device list
  changes or the epoch is more than seven days old at sending time;
- sealed messages: signing, encrypting, checking, decrypting;
- file and thumbnail encryption for sealed attachments;
- call frame keys from the epoch secret (`CALLS.md` section 14);
- the backup: the backup key, backup records, locked copies for a
  passkey, the twelve-word phrase or a device, replacement and reset;
- the device-approval code and the 60-digit comparison code.

**One set of test vectors.** The Go reference writes test vectors to
`testdata/sealed/`: for each construction, inputs and the exact bytes
that must come out, and messages that must be refused. The Dart,
JavaScript and Swift code must all pass them (`STRUCTURE.md` section
5). A change to the scheme starts with the reference and new vectors.

**Where the apps keep sealed state.** Device keys, epoch secrets and
decrypted sealed messages are in the app's encrypted local store
(section 6). The store's key, and the device's private keys, are in
the platform's secure storage.

**What the iPhone notification extension can reach.** The app and
the extension share one keychain access group. Whenever the app
learns an epoch secret it writes to that group what the extension
needs to read a message in that epoch: the content key and the
senders' device keys. The extension reads those, checks the
signature and decrypts. It cannot reach the encrypted store, the
device's private keys or the backup key, and decrypting changes no
key, so the app and the extension need no lock between them. On
Android the push handler is Dart and runs the app's own code.

The server never runs any of this outside its tests.

## 6. The local database

Each signed-in server has its own folder under the app's support
directory, named by a random identifier:

    servers/<local id>/
      cache.db       everything below, through drift
      sealed.db      the sealed store, encrypted at rest
      files/         downloaded files and thumbnails, a size-capped cache

Signing out of a server deletes its folder and its tokens.

### What `cache.db` holds

| Table | Contents |
|-------|----------|
| `me` | The signed-in account, its preferences, status, do not disturb and quiet hours |
| `accounts` | Every account seen in a conversation or a member list: names, avatar, presence, status |
| `groups`, `emoji`, `sections`, `saved_replies`, `status_presets`, `commands` | The server's small lists, whole |
| `conversations` | Every conversation the account is in, with its own membership: level, `mode`, `muted_until`, `show`, read position, counts, starred, thread view, and `synced_seq`, the last sequence number applied |
| `notifications` | The items of the notifications list for this server (section 8.3) |
| `messages` | Recent messages per conversation, in the API's shape, with their reactions, checklist state, form and thread summary |
| `thread_states` | Following or ignored, and the read position, per thread |
| `drafts` | Unsent text per conversation and per thread, with attachments chosen |
| `outbox` | Messages written while offline or not yet confirmed, each with its `client_id` |
| `bookmarks`, `scheduled` | The account's own lists |
| `files` | Which downloads are in `files/` and when they were last opened |
| `settings` | Per-server choices made on this device: notification sounds; the server's default themes as last seen |

**Thread states** arrive beside messages, in a `thread_states` list
on each page and event, not inside the message object. The app writes
them to its `thread_states` table and draws markers from there.

**How much is kept.** For each conversation, an unbroken run of
messages ending at `synced_seq`: the newest 500, or everything the
person has scrolled back through in the last week if that is more.
Conversations not opened for 30 days keep the newest 50. Thread replies
are messages like any other and are kept with their conversation. The
file cache is capped at 1 GB by default and drops the least recently
opened first.

**Sealed chats.** Their rows in `messages` hold only what the server
sent: ciphertext. `sealed.db` holds epoch secrets, pinned account
keys, verified marks and the decrypted messages. It is a SQLite file
encrypted at rest, with its key in the platform's secure storage. A
device that loses it restores what the backup allows
(`docs/SEALED.md`).

**What is not kept.** Search results, other people's typing, message
history (earlier versions), and anything about conversations the
account is not in.

## 7. Catch-up

Every event in a conversation has a sequence number (`CONVENTIONS.md`).
The app remembers, per conversation, the last one it applied.

**On start.**

1. Open the database and draw from it at once. The app is usable
   before any network request finishes.
2. Call the status endpoint to set badges early.
3. Open the WebSocket.

**On connect and on every reconnect.**

1. The server sends `hello`. The app answers with `resume`, whose
   `positions` map holds, for every conversation in the database, its
   identifier and `synced_seq` (`docs/EVENTS.md` section 2). An app
   with nothing stored sends an empty map.
2. For each conversation in the map the server either replays the
   events after that number, in order, or sends a `reload` frame: the
   conversation has fallen too far behind to replay, or the account
   is no longer in it. Events are kept for 30 days.
3. The app applies replayed events through the same code as live
   ones. Each event is applied in one database transaction together
   with the new `synced_seq`, so a crash never leaves the two out of
   step.
4. For a `reload`, the app deletes that conversation's cached
   messages, then fetches the newest page and sets `synced_seq` from
   it, or drops the conversation if it is no longer a member.
5. The server ends the replay with `ready`, which lists
   `new_conversation_ids`: conversations the account is in that were
   not in the map. The app loads those over HTTP. Until `ready`, the
   app shows "catching up" and holds its own sends.
5a. **Large channels.** When `hello` carries
   `conversations_open_max` the app and the web chat say which
   conversation is on screen (`docs/EVENTS.md` section 4.1): in
   `resume` as `open`, and with `conversations.open` whenever it
   changes, each time with where what they hold of it ends
   (`synced_seq` in the app; the newest `seq` of the loaded room in
   the web chat, which names a room only once it has loaded and is
   showing its newest page). The server sends what came after that
   position and then the events as they happen, so opening is the
   catch-up of step 2 for one conversation, `reload` included.
   A conversation that is delivered lazily and is not on screen sends
   no events: the client is sent `conversation.changed` at most once
   a second. It moves the conversation's `last_seq` and its unread
   numbers (the ones in the signal, or else its own count plus
   `messages`) and leaves the cached messages and the position where
   they are: a signal is never a gap to fill. A signal with `event`
   carries one event to apply at once without moving the position: a
   message that notifies (announced and listed under notifications;
   the app does not put it among the cached messages, where it would
   stand apart from the ones before it) or a change to the
   conversation itself. Both clients treat an event they are sent
   twice as one.
   In a conversation whose object says `large` the clients send no
   typing notice, show nobody typing, and show "N online" from
   `presence.counts` in its header in place of presence lights. The
   same event carries the server's own count, which is the "N online"
   under the server's name in the app's sidebar. In an announcement
   channel the message box gives way to a notice for anyone who may
   not post (which says whether replies in threads are allowed), and
   the member list is offered to its managers only.
6. Changes that have no sequence number (the account itself, groups,
   emoji, settings, read positions, modes and counts) are not
   replayed. After `ready` the app fetches its own account, the list
   of conversations and the status summary again, and the small lists
   it keeps whole.
   Presence is among these: it comes with each account loaded over
   HTTP. Live, it arrives as `presence.changed` for one account or,
   when many people's presence changes at once and just after `ready`,
   as `presence.batch`, a list of the same objects (`docs/EVENTS.md`
   sections 5 and 9). The app applies a batch in one database write,
   so the screens that show presence redraw once; an account it does
   not hold is passed over. The web chat does the same in its store.
   Presence is sent by interest (`docs/EVENTS.md` section 4, ruling
   R62): a client hears of the accounts it says it is showing and of
   nobody else. When `hello` carries `presence_subscribe_max`, the app
   and the web chat send `presence.subscribe` with the accounts whose
   presence is on screen: the account itself, the other person of
   every two-person chat in the sidebar, and the profile that is open.
   They send it again when that set changes, after it has stayed the
   same for 300 ms, and after every `ready`; the server answers with
   where the newly named accounts stand. Presence of anyone else is as
   old as the last time the account was loaded over HTTP, and must not
   be shown as if it were current: a screen that starts showing
   someone's presence adds them to the set. A client that never
   subscribes is only told about the people of its direct and small
   chats (ten members or fewer). To a server whose `hello` lacks the
   field nothing is sent, and it tells the client about everyone, as
   before.
7. The outbox is sent, oldest first. Each message carries its
   `client_id`, so one that had in fact arrived before the connection
   dropped is not posted twice.

**Gaps.** An event whose `seq` is not exactly one more than
`synced_seq` means something was missed. The app stops applying events
for that conversation and asks for the missing range through the
catch-up operation before going on.

**Scrolling back** fetches older pages and extends the unbroken run
downwards. **Jumping** to a message far from the run (a search result,
a link to a message, a bookmark) shows a page fetched for the purpose
and held in memory. It is not written to the database unless it joins
the run.

**Reconnecting.** The retry interval is 1 second for the first
attempt, then 2, 5, 10 and 30 seconds, and 30 from then on. The wait
before each attempt is drawn at random, evenly, between half the
interval and the interval plus the server's spread:

    wait = random between interval / 2 and interval + spread

so the shortest waits are 0.5, 1, 2.5, 5 and 15 seconds. The spread is
`reconnect_spread_seconds` from the last `hello` the client received
(`docs/EVENTS.md` section 2), which grows with the number of people
connected: 2 seconds on a small server, 20 at 5,000 connections, 60
at most. The client remembers it for as long as it runs, because the
`hello` after a restart comes from a server that is still nearly
empty; until a server has said, and with a server too old to say, the
spread is 5. A connection closed with 4029 (the server has too much
to do) waits by the same rule with an interval of 30. The interval
goes back to 1 once the connection is up (the app: at `ready`; the
web chat: at `hello`).

The client tries at once, whatever is left of the wait, when the
device reports the network is back or the app comes to the front (the
web chat: the browser's `online` event, or the tab being looked at
again).

While it waits, the client says how long: "Reconnecting in 7
seconds", counting down each second. The number is the wait that was
really chosen, rounded up to whole seconds and never shown as 0. The
app shows it in the line under the server's name in the channel list
(the soonest, when several servers are waiting) and in the banner
above a conversation; the web chat shows it in the status line at the
top of its sidebar, where the count itself is kept from screen
readers so that the line is not read out every second.

Nothing here depends on the server's resume queue (`docs/EVENTS.md`
section 2), and the queue does not depend on this: a client that has
connected and sent `resume` may wait a while for its replay when
everyone reconnects at once, during which it shows "catching up" and
its pings are answered.

## 8. Several servers

One app can be signed in to any number of Churr servers. The servers
stay independent: none knows about the others, and nothing here needs
an API operation that a single server does not already have. What
joins them together is the app.

### 8.1 What stays separate

- Each server has its own tokens, local database, live connection,
  controllers and, with "use each server's default", its own theme.
- A link to a message or channel (`churr://<server>/…`, or the
  server's own web address on phones) opens the right server, and
  offers to sign in if the app does not know that server.
- Notifications say which server they are from when more than one is
  signed in. The icon badge is the sum over servers.
- **Connections.** Desktop apps keep every server connected. Phone
  apps, while in front, keep every server connected in the merged
  layout (section 9.1), and in the rail layout keep the
  selected server connected and reconnect the others as they are
  opened. A server that is not connected still has current badges,
  from wake-ups and its status summary (`getStatusSummary`).

### 8.2 App preferences

Some choices belong to the app and not to any server. They are kept
on the device, in an `app.db` beside the per-server folders, and no
server ever sees them.

| Preference | Out of the box |
|------------|----------------|
| Sidebar layout: merged tree or server rail | Server rail |
| Default for `show`, used by every conversation whose own `show` is not set | `always` |
| "Show hidden" on or off | Off |
| The order of servers | The order they were added |
| Servers opted out of shared status and do not disturb | None |
| Theme, text size, Enter sends, language, sounds | Section 9.23 |

**Copying them to another device.** App settings has "Set up another
device". It shows a QR code holding the app preferences above and the
addresses of the signed-in servers. It holds no token and no key. The
new device scans it under "Add a server → Copy from another device",
takes the layout, the default for `show` and the rest, and then walks
the person through signing in to each server in turn. How each
server's conversations are arranged needs no code: each server keeps
its person's own sidebar (9.1). Servers the person skips are kept and
offered as soon as they are added. A code too large for one QR code is
shown as several, one after another.

When a device is added by scanning a server's pairing code from a web
page (section 9.22), there is no app on the other side to copy from,
and the new device starts with the defaults.

### 8.3 One notifications list

One list, across every signed-in server (screen 9.24).

- **What goes in it:** each message that notified or would have
  notified the person: a mention by name, by group or by `@channel`,
  a reply in a followed thread, and a message in a conversation whose
  mode is `any` (which is what ad-hoc channels and sealed chats
  start at), plus missed calls. It is driven by mode and by nothing
  else, so a conversation whose `show` is `hidden` puts its
  notifications here like any other. For a hidden conversation this
  list is where they are seen.
- **Where items come from.** Each server marks every message event
  with `notify` for its recipient (`PUSH.md` section 12). The app
  writes an item for each event marked `notify` or `badge`, live or
  replayed on catch-up, into that server's local database. After a
  reload, or on a server that was never connected on this device, it
  fills in from `listConversations` (which gives each conversation's
  badge and read position), then `listMessages` after the read
  position for conversations with a badge, and `listThreads` for
  followed threads with unread replies.
- **Order:** newest first across all servers, by the message's time.
  Each item carries its server's mark.
- **Filter:** all servers, or one.
- **Clearing.** An item clears when that conversation's or thread's
  read position passes it, on any device: opening the item does this,
  and so does reading it elsewhere (the read events and
  `listConversations` carry the position). "Mark all read" calls the
  mark-read operation for each conversation and thread with items,
  on each server in the filter. Cleared items stay under "Earlier"
  for a week, on this device only.

### 8.4 One search

The search screen (9.8) asks every signed-in server at once.

- The app calls `search` on each server with the same words and
  filters, in parallel, each with its own cursor.
- Results are merged newest first by message time. Servers rank by
  their own rules and their scores cannot be compared, so time is the
  only fair order.
- Results appear as each server answers. A server that is slow or
  unreachable is named in a line at the top and does not hold the
  others back.
- Loading more asks each server for its next page and merges again.
- Filters by conversation or author apply to the one server they
  belong to. A filter chooses all servers or one.
- Sealed chats are not searched on any server.

### 8.5 One quick switcher

The quick switcher (9.17) looks through the local databases of all
servers at once: conversations by name, and people the app has seen.
It needs no request. Each result shows its server. People not yet
seen on this device are found with `listAccounts` on each server once
three characters have been typed.

### 8.6 Status and do not disturb, set once

- Setting a status, clearing it, turning do not disturb on or off,
  and setting availability or appear away are done once in the app
  and sent to every signed-in server: `setStatus`, `clearStatus`,
  `setDnd`, `clearDnd`, `setAvailability` and `updateMe`, one call
  per server.
- A server can be **opted out** in its settings (9.12). An opted-out
  server is left alone by these, and has its own controls there.
- Each server stores its own copy, so they can drift: a status set
  from the web chat, or by a calendar integration, changes one
  server. The app shows the shared value when all taking part agree
  and "differs on <server>" otherwise, with a button to make them the
  same.
- A custom emoji in a status exists on one server only. The others
  get the text without it.
- Status presets are per server. The picker lists the app's shared
  ones first: a preset is shared when the person saved it from the
  app, which creates it on each server with `createStatusPreset`.
- A server that cannot be reached is sent the change when it next
  connects, unless the person has changed it again since.

### 8.7 Quiet hours per server

Quiet hours are not shared. Each server has its own schedule
(`getQuietHours`, `setQuietHours`, `skipQuietHours`), so a work
server can go quiet in the evening and a friends' server at night.
The settings screen shows them side by side, with "Copy to…" to put
one server's schedule on others.

### 8.8 Muting a whole server

"Mute this server" for an hour, eight hours, a day, a week or until a
chosen time. The app turns do not disturb on for that server alone
(`setDnd` there, with the end time) and leaves the others as they
are. The server stops notifying and waking the person's devices and
its badges keep counting. The dialog says plainly, before the person
confirms: "People on <server> will see that you are in do not
disturb." Unmuting is `clearDnd` there. A muted server is drawn
dimmed in every layout, and is left out of shared do not disturb
changes until the mute ends.

### 8.9 Profile on chosen servers

Editing one's own profile (9.11) ends with "Apply to": the servers to
change, all ticked to begin with. The app calls `updateMe` on each
ticked server, and uploads a new avatar to each. Usernames are not
part of this: a username belongs to one server. A server that
refuses a value (a display name its rules do not allow) is reported
by name and the others still change.

### 8.10 Adding a server

Three ways, all ending in the same sign-in (9.22):

| Way | What happens |
|-----|--------------|
| Address | The person types the server's address. The app calls `getServer` there and shows its name before anything else |
| Invite link | Opening an invite link for a server the app does not know opens that server's join page in the browser, which ends by handing back to the app, signed in |
| QR code | Scanning a server's pairing code from a signed-in web page, or another device's set-up code (8.2) |

## 9. Screens

Each entry says what the screen shows and what can be done there. How
the screens are arranged on different sizes of device is in section
10.

### 9.1 The sidebar, its two layouts, and each person's own arrangement

The sidebar is where conversations are listed. The person chooses one
of two layouts in App settings → Sidebar. The app also offers the
choice once, when a second server is added. The choice is an app
preference on the device (8.2).

In every layout the top of the sidebar has the person's own status
and do not disturb, **Notifications** (9.24) with its count,
**Threads**, **Bookmarks**, search, and the quick switcher. At the
bottom are "Browse channels", "New…", and "Show hidden".

**Merged tree.** Every server at once, in one scrolling list.

- Each server is a heading: its icon, name, badge and a mark if it is
  disconnected or muted. A heading collapses and expands, and
  remembers which. Its menu has the server's settings, mute, server
  settings in the browser and sign out.
- Under a server's heading, that server's headings in the person's
  own arrangement (below).
- Servers are in the person's order, changed by dragging a heading.

**Server rail.** One server's conversations at a time.

- A rail holds one square tile per server (its icon, or its initials
  on lavender) with that server's badge, a mark on any that
  is disconnected or muted, and a "+" tile. Selecting a tile shows
  that server. Tiles reorder by dragging. A tile's menu is the same
  as a heading's above.
- The list for the selected server has the same headings in the same
  order as under its heading in the merged tree.

**Each person's own arrangement** (ruling R83, `FEATURES.md` 21.4).
Under each server the headings, their order and the order of rows are
the person's own, kept by that server: the same on every device and in
the web chat, and changed on one device, changed on the others at once
(`sidebar.changed`). Nobody arranges anyone else's. Out of the box it
is the server's default: the admin's sections in the admin's order,
"Channels" (named channels in no section), then **Chats** (ad-hoc
channels) and **Sealed**.

- **Own sections.** "New section" (in "New…", in a heading's menu, and
  in "Move…") makes one; a heading's menu renames or deletes it.
  Deleting one sends its conversations back where the server lists
  them. An own section stays drawn while empty, with a line saying
  how to fill it.
- **Moving a conversation.** By dragging its row onto another row
  (it goes before that one) or onto a heading (it goes at the end),
  or by "Move…" in the row's menu: the person's own sections, "where
  the server lists it", "New section…", "Move up", "Move down" and
  "Reorder…". A row goes only under one of the person's own sections,
  its own heading, or back where the server lists it. A sub-channel
  goes with its parent unless moved itself.
- **Sort, per heading,** from the heading's menu: Alphabetical (the
  default), Last activity (the default for Chats and Sealed) or Custom
  order. Moving a row within a heading makes its order custom. "Reset
  to alphabetical" (Chats and Sealed: "Reset to last activity") puts
  one heading back; "Reset my sidebar…" puts everything back to the
  server's default, stars kept.
- **Heading order.** Own sections, the admin's sections and
  "Channels" are dragged, or moved with "Move up" and "Move down" in
  the heading's menu. Chats and Sealed stay last.
- **Stars.** A starred conversation sits at the top of its heading,
  in the order starred; dragging among the starred rows reorders
  them. A star is set from the row's menu.
- **Keyboard.** Alt+Up and Alt+Down move the focused row one place
  within its heading, or the focused heading one place among the
  headings. Elsewhere they go to the previous or next conversation
  (keyboard shortcuts, section 13).
- **Touch.** A long press opens the row's menu; "Reorder…" there puts
  the server's list in reordering mode: each row has Move up, Move
  down and a handle to drag it by, and a bar at the top says so with
  "Done".
- **Said aloud.** Each move is announced: where the row now is ("town
  is in Focus, 2 of 5"), or that it is already first or last.

The two layouts differ only in whether servers are shown together.
Which rows are drawn at all is decided the same way in each (9.2).

### 9.2 Which conversations have a row

Each membership has two independent settings on its server
(`updateMembership`): `mode`, what notifies, and `show`, whether the
conversation has a row in the sidebar.

| `show` | The row is drawn |
|--------|------------------|
| `always` | Always |
| `activity` | Only while the conversation has unread messages, whether or not they notify |
| `notification` | Only while the conversation has a badge |
| `hidden` | Never, unless "Show hidden" is on |
| Not set | As the app's default says: one of the first three, chosen in App settings, `always` out of the box |

The exact behaviour:

1. **The conversation being viewed always has its row, in its usual
   place,** whatever `show` says. Opening a hidden conversation from
   search, a link, a notification or the quick switcher puts its row
   in the sidebar for as long as it is open.
2. **A row appears** the moment its condition becomes true: an unread
   message arrives (`activity`), or the badge goes above zero
   (`notification`). It appears in its normal position in its group,
   not at the top, and the selection does not move.
3. **A row does not vanish under the person's hand.** Reading a
   conversation makes its condition false, and the row stays while
   the conversation is still open. It goes when both are true: the
   conversation is read (no unread messages for `activity`, no badge
   for `notification`), and the person has moved on to another
   conversation or screen. Reading it on another device removes the
   row at once if it is not the one open here.
4. **Marking a conversation unread** brings its row back, for
   `activity` and `notification` alike.
5. **`hidden` rows** are drawn only while "Show hidden" is on, with a
   crossed-eye mark, in their usual places. The toggle is an app
   preference. A hidden conversation is also always reachable by
   search, by a link, by the quick switcher and from the
   notifications list.
6. **A heading with no rows is not drawn:** an admin's section,
   Channels, Chats or Sealed with nothing to show has no heading
   either. The person's own sections are drawn while empty, so they
   can be moved into.
7. **Starring does not override `show`.** A starred conversation set
   to `activity` comes and goes like any other.
8. **Totals ignore `show`.** A server's badge, the icon badge and the
   notifications list count a conversation whether or not it has a
   row. A person who hides a busy channel and leaves its mode at
   `any` will see the server's badge rise with no row to explain it;
   the Notifications entry is where that count leads.

**How a row is drawn.**

- The name, a lock for private, a closed padlock for locked, a mark
  for shared with other servers, a call mark while a call is on, and
  the other person's presence for a two-person chat.
- A badge with the count its mode notifies. Bold when unread and the
  mode is `any` or `mentions`.
- **Dimmed** when the mode is `nothing` or the conversation is muted,
  and never bold. A mention of the person by name still badges there.
  A muted row also shows when the mute ends.

**Does:** open a conversation; from a row's menu: what notifies, show
in sidebar, mute, star, mark read or unread, Move… and Reorder… (9.1),
leave; collapse a group; "Browse channels" (all public channels of a
server, with join); "New channel"; "New chat" (pick people; opens the
existing ad-hoc channel with exactly them or makes one, with the
notice that anyone added later sees the whole history); "New sealed
chat" (9.19). In the merged layout the "New…" and
"Browse" actions ask which server first.

### 9.3 Conversation

- **Header:** name or members, topic, member count, a Join button
  while a call is on, and buttons for call, search in this
  conversation, pins, members and the conversation menu (what
  notifies, show in sidebar, thread view, star, settings for
  managers, share, leave).
- **Body:** messages, oldest at the top, drawn as `MESSAGE-FORMAT.md`
  says. Day dividers. A "new" line at the read position. Consecutive
  messages by one author within five minutes share one avatar and
  name. System messages are small centred lines. Messages from a
  blocked account are collapsed to one line that can be opened.
- **Threaded view** (the default):
  - A message with replies shows a summary line under it: reply count,
    the faces of who replied, the time of the last reply. Selecting it
    opens the thread (9.4).
  - Replies are not in the body.
  - **The marker.** When a thread has replies, one line is drawn in
    the body at the position of the latest reply, for example "3 new
    replies to: Deploy plan for Friday", or "Replies to: Deploy plan
    for Friday" when the reader has seen them all (`FEATURES.md`
    section 1.3). There is at most one marker per thread. A later reply moves it down. It is
    drawn from the parent's last-reply sequence number and the
    reader's thread position; nothing is stored for it. Selecting it
    opens the thread. Its menu has Follow and Ignore. No marker is
    drawn for an ignored thread.
- **Flat view:**
  - Replies appear in the body in the order they were written.
  - Each reply has a header: who it answers and the start of that
    message, as a link up to it; a link to the top message when that
    is a different one; and "Open thread".
  - No markers. Replies in ignored threads are left out.
- The view is the person's preference with an override per
  conversation, both saved on the server.
- **Does:** scroll back through history; jump to the read position and
  to the newest message (jumping to a date is not built: the API has no
  way to list messages by date); select a message for its actions
  (9.6); open a person's profile from their name; answer a form; tick
  a checklist item; open a file or image; drop files to attach.
- The read position moves when the newest message has been on screen
  with the app in front.
- **A public channel opened without joining** can be read and
  searched and nothing more. Reactions, form answers, tick-boxes and
  the composer are replaced by one bar: "Join to take part".

### 9.4 Thread pane

- **Shows:** the parent message, then its replies oldest first, each
  with its quote if it has one. A header with Follow or Ignore and
  "Move to a channel" for those allowed. A composer at the bottom.
- **Does:** reply; reply to a reply (stays in this thread, quotes that
  reply); follow, stop following or ignore; move the thread out to its
  own channel (asks for a name, says it cannot be undone; not offered
  in shared channels or sealed chats).
- **After a move:** the parent stays where it was. Under it is one
  system reply saying where the thread went, with a link to the new
  channel, whose first message is a copy of the parent. The replies
  are in the new channel. The old thread takes no more replies, so
  the pane shows the link in place of the composer. The new channel
  is a sub-channel of the one the thread was in, so it appears under
  it in the sidebar (9.25).
- Opens beside the conversation on wide screens and over it on narrow
  ones (section 10).

### 9.5 Composer

- **Text:** a growing box that takes Churr Markdown as typed, with a
  formatting bar (bold, italic, strike, code, link, lists, checklist,
  quote, code block) and a preview toggle. Enter sends on desktop;
  Shift+Enter makes a new line. On phones the send button sends.
- **Focus:** the box keeps the keyboard, so that typing goes straight
  in (the web chat's `chat/keepfocus.js`, the app's
  `keep_focus.dart`). After sending, the box that sent keeps focus, on
  phones too. On a desktop, opening a conversation or a thread puts
  focus in its box, and so do a click on a message, the sidebar, a
  button or empty space, and closing a dialog, menu or picker that a
  click opened; a click that leaves text selected does not, so text
  can still be selected and copied. Another text box (search, a
  dialog's field, the other composer, settings) keeps focus while it
  has it. A printable key pressed while focus is on no text box goes
  into the box, once: Ctrl, Cmd and Alt combinations, Space on a
  control, keys a focused message or control already handles (R, E, T,
  P on a message in the web chat) and anything typed in a dialog or
  menu are left alone; pasting there pastes in the box. Keyboard use
  moves focus as it always did: Tab, Shift+Tab and the arrow keys go
  where they go, and a dialog or menu opened from the keyboard gives
  focus back to what opened it. On a phone nothing but sending moves
  focus, so the on-screen keyboard never pops up by itself. With a
  thread pane open, typing goes to the box used last. A conversation
  that is locked, read-only, closed, waiting on a request, or an
  announcement channel the person may not post in has no box to focus.
- **Mentions:** typing `@` lists people in the conversation first,
  then groups, `@channel` and `@here`. Picking someone who is not in
  the conversation says they will not be notified. While the text
  holds `@channel`, `@here` or `@everyone` and the conversation says
  the person may not use them (`broad_mentions_allowed` false), a line
  under the box says so before sending: "Only managers can use
  @channel, @here and @everyone here. This will be sent as plain text
  and will notify nobody." When they may, sending first asks the
  server (`checkBroadMentions`), and above 30 recipients a dialog
  asks "This will notify N people" with Send and Cancel. If the
  answer to the post carries `broad_mentions_refused`, the same line
  is shown after the fact. Picking someone in
  do not disturb says so. Picking someone out of office shows the
  date they are back. A person on another server is written
  `@name@server.name`.
- **Channels:** typing `#` followed by characters that match a
  channel the person can see opens a picker asking whether to link
  one. Choosing inserts a link chip; dismissing leaves what was typed
  as plain text (9.26).
- **Notify anyway:** on a message the person has just sent, when
  someone it mentions or messages is in do not disturb and allows it,
  the app offers "Notify anyway" for that one person. It always
  appears to work. The app shows the mark on the message and nothing
  else: it is never told, and never shows, whether the person was
  reached, because they may have set the conversation to notify
  nothing or blocked the sender.
- **Emoji:** typing `:` completes standard short codes and custom
  emoji. A picker has search, recents and the server's custom set.
  Where the person may add a custom emoji (admins always, members when
  the server's "Members may add emoji" is on, guests and bots never),
  the picker ends with "Add emoji": a name and a picture, after which
  the new emoji is picked. The same is in the web chat's settings and
  the app's server settings. Where it is not allowed it is not shown.
- **Commands:** typing `/` at the start lists the built-in commands
  (`FEATURES.md` section 12.3: forms, `/remind`, `/me` and the text
  conveniences, the chance commands, the IRC-style shortcuts), saved
  replies and the commands of bots in this conversation, each with its
  hint and a one-line description. Presets open a small dialog when
  they need more than one line. `/remind` is read by the grammar of
  `FEATURES.md` 9.2; with nothing after it, a form asks what, when and
  where. Built-ins are read by the one grammar
  of `FEATURES.md` section 12.4, held in `lib/core/commands` (app) and
  `shared/commands.js` (web chat) to `testdata/commands`. Picking a
  built-in about a person from the list types `@` after it, which opens
  the people picker; channels come from the `#` picker as links. A
  replaceable built-in (the fun and games; the others are protected,
  `FEATURES.md` 12.1) that a saved reply, custom command or bot command
  listed here shares a name with is hidden: not offered, not read by the grammar,
  not carried out; `/exile` and `/unexile` exist for admins only
  (ruling R72, the one rule `hiddenBuiltins` in both clients, held to
  `testdata/commands/parse.json`). A
  command with a mistake, or one that does not work in a sealed chat, is
  refused with the reason before anything is sent; one the server
  refuses shows the server's words. Each carries out the request a
  button elsewhere would make (join, leave, open a chat, set the topic,
  add, remove, time out, ban after a confirmation, set the level, block,
  open a profile, change the display name, browse, members, away, do not
  disturb, mute the conversation). Results the server draws are drawn
  and animated as `MESSAGE-FORMAT.md` section 1.2 says; `/me` as section
  1.1 says. A server started with `-dev` serves `/dev/chance`, a page
  with a button per chance command for judging the animation.
- **The second batch** (ruling R74, `FEATURES.md` 12.3 to 12.6): `/help`
  opens a searchable help panel (a dialog on wide screens, a screen of
  its own on phones) of what the person can use here, from the server's
  command list (so hidden built-ins are left out), grouped as
  `commands.json` groups them with saved replies and bots after, each
  with its one-line summary, the aliases named beside their command,
  and at the foot "Things you can type", a row per thing that opens to
  say what to type, what it becomes, how the list behaves and an
  example, with the reference patterns that apply in the conversation
  and the unit table filled in, then the formatting. Choosing a command,
  or naming it after `/help`, shows its full entry, with a way back to
  the list and buttons for related commands; "Put /name in the message
  box" puts `/name ` there. Both clients draw every word of it from
  `testdata/commands/help.json` (`shared/help.js`,
  `lib/core/commands/help.dart`). `/banlist` opens
  the channel's bans, each with Lift. `/clear` scrolls to the newest
  message and marks the conversation read; `/close` marks it read, sets
  `show` to `activity` and closes it. `/motd` opens the welcome text and
  says that Churr keeps no separate rules; `/admin`, `/version`,
  `/time`, `/lusers` and `/ping` answer in one line. `/ping` is the time
  from the live connection's `ping` to its `pong`: the heartbeat
  measures it, and the command sends one more `ping` for a fresh reading
  (nothing new in the protocol). `/invisible` appears away, since there
  is no appear offline. `/voice` and `/devoice` say plainly that a
  channel is not an announcement channel instead of asking the server.
  `/knock` opens a picker of the knock channels the person can see and
  is not in, each with its topic, member count and managers and "Ask to
  join"; the channel browser and a channel link offer "Ask to join" on
  a knock channel instead of Read and Join. A knock channel's managers
  see a bar "N people ask to join" with Review, a dialog with Add and
  Decline per person; the one who knocked is told the outcome once.
  Channel settings let managers make a public channel a knock channel,
  a knock channel public again, and either private. `/broadcast`
  (admins) shows its notice to everyone as a banner over the app with
  "Server notice", who sent it and a Dismiss button, until its `until`;
  dismissed it stays hidden on that device for the session; a screen
  reader is told it once; it comes from `server.broadcast` and from
  `getStatusSummary` after every `ready`.
- **The literal slash:** text that starts with whitespace and then `/`
  is sent as text from the `/` and never run, in sealed chats too
  (`parse.json`, `literal_cases`).
- **Attachments:** pick files, take a photo, paste an image, or drop
  files. Up to ten. Each shows a thumbnail, its size, upload progress,
  an optional caption and a remove button. The server's size limit is
  checked before uploading. For a video, the app takes one frame as a
  poster image and sends it with the upload, because the server does
  not make one. A video that arrives without a poster is drawn with a
  plain placeholder until it is played.
- **Scheduling:** the send button's menu has "Send later" with a few
  suggested times and a date and time picker.
- **Forms:** `/form` opens the builder: fields, buttons, named or
  anonymous, when results show, a closing time.
- **Replying:** when replying, the quoted text sits above the box and
  can be dismissed, which leaves a plain reply in the thread.
- **Also:** a draft is kept per conversation and per thread. A line
  under the box shows who is typing. The box is replaced by a notice
  when the person cannot post: a reader in an announcement channel,
  timed out, slow mode with a countdown, or an archived channel.
- **Times** (ruling R69, `FEATURES.md` 9.5): every post and edit carries
  the device's IANA zone as `time_zone` (the app sends it only when the
  platform says what it is). A date and time typed in the text is an
  exact token and is offered as the objects below are. A phrase naming a
  time that ends at the cursor is a loose phrase: it opens the list in
  the picker's place, "Make this a time: … (your time)" and "Plain text";
  an accepted phrase shows as a dashed chip above the box ("Not a time"
  takes it back). Accepted phrases go as `instant_hints`, kept on their
  words as the text around them changes and dropped if the words
  themselves change. Positions are in the text as sent (a channel chip
  counts as its `<#…>`). The web chat's sealed composer, a plain box,
  resolves typed times and references on send but offers nothing.
- **Objects in the composer** (ruling R73; the one rule for instants and
  every object of `MESSAGE-FORMAT.md` 3.2). The list is the same
  component as the people, channel and emoji pickers, at the caret on
  wide screens and in the strip above the keyboard on phones; Up and
  Down move through its rows and wrap (up on the first row goes to the
  last, down on the last to the first), in every picker. Its rows are
  announced to screen readers as the selection moves, and the outcome
  ("PROJ-12 accepted", "kept as plain text") is announced too.
  *Class A, exact tokens* (a typed date and time, a reference pattern's
  match, and the later kinds): when a complete, valid token ends at the
  caret the list opens with each reading of it in the order the kinds
  are tried, the first chosen, then a last row "Plain text: “…”".
  Finishing the word (space, punctuation, or Enter to send) takes the
  chosen row, and the key goes in as typed (Enter then sends; Enter
  always sends for class A); Tab takes it without typing; a tap takes the
  row tapped; Escape takes plain text. Backspace as the very next key
  after an object was accepted makes it plain text again and deletes
  nothing. Typing on extends the token and offers it afresh. A token is
  offered once: a choice stays with its text while it stands alone, and
  goes when the text is edited. Plain text and any reading but the first
  go as `object_hints`; the first reading sends nothing, since the
  server reads it so anyway. *Class B, loose phrases* (natural-language
  times): never taken by themselves; Tab or Enter takes the chosen row
  and Enter then does not send; Escape, or typing on, dismisses; space
  does not take it. The last row is "Plain text" here too.
- **References** (ruling R73) are links over the typed text to the
  address they resolved to; on the app each is a control of its own,
  reached with the keyboard and labelled with the text as typed. The
  admin console has "Reference patterns" for the server's; a named
  channel's settings have "References", where its managers add, change
  and remove the channel's and everyone sees what turns into a link
  there. Both clients keep the patterns that apply in memory, fetched
  when a composer opens and replaced by `reference_patterns.changed`.
- **Message links** (ruling R73) are drawn as a compact quote: the
  author, the local time, the channel and the first three lines, with
  a cyan bar, as one control labelled "Link to a message (the link as
  typed)" and what it shows. The message is fetched with `getMessage`
  (the web chat uses a message it already holds), kept a minute, then
  fetched again, so edits and deletions show; a refusal shows "A message
  you can't see". Selecting it opens the conversation at the message, or
  a reply's thread. The app opens a pasted message link of a signed-in
  server the same way. "Copy link" writes
  `{origin}/chat/c/{conversation}/m/{message}`, or for a reply
  `/chat/c/{conversation}/t/{thread}/m/{reply}`.
- **Measurements** (ruling R73) are drawn as typed with the
  equivalent after them in the dim colour, in brackets, as part of the
  text: no control, since there is nothing to do with them, and a screen
  reader reads both. The system is a per-device choice: "Measurements"
  in the web chat's settings (local storage `churr-units`) and in the app
  settings (`measure_system`), with "From this device's region" first.
  The composer's row says what readers of the other system will see.
- **Events** (ruling R73) look like instants, with the calendar glyph,
  showing one range; a control labelled with both ends in full, the
  reader's zone and the text as typed. Selecting it opens a menu: "Add
  to calendar" (the web chat downloads `event.ics`; the app asks where to
  save it) and "Open the time converter" for the start. Both clients
  write the file with the same code paths the shared cases check.
- **Phone numbers and email addresses** (ruling R73) are drawn as
  links over the typed text; each is a control whose menu has "Call" or
  "Write" (`tel:` or `mailto:` to the device's own app) and "Copy".
- **Places** (ruling R73) are the typed text with a dotted cyan
  underline, a control labelled "Place … Open in maps" on phones (the
  app launches `geo:` on Android and `maps:` on iOS) and "Place … Copy"
  on desktops and in the web chat, which copy `lat,lon`.
- **Inline dice** (ruling R73) are drawn in the sentence: the typed
  text dim, each die (dropped ones struck through), a plus or minus for
  plain numbers, and `= total`, labelled with the whole roll in words.
  They spin once when the message arrived live (the web chat's `_live`,
  the app's `ChanceLive`, noted per dice) and again when selected. For
  its sender a message arrives twice, as the answer to posting it and as
  its live event; whichever comes first counts, and the other is the
  same arrival. The spin belongs to the message, not to one drawing of
  it: a message drawn again while its reels turn carries on turning from
  where they were, and once the spin is over nothing turns them again.
  Neither composer offers them in a sealed chat.
- **Colours** (ruling R73) are a control each: a swatch (a square of
  the colour inside a ring of the background colour and one of the ink
  colour) and the typed text, labelled "Colour #fff. Copy"; selecting it
  copies the colour's `value`. In the composer `#` starts a channel
  link; when no channel matches, an exact token there (`#fff`, `#4521`)
  is offered as usual.
- **The time converter** opens from an instant in a message (click, tap,
  Enter or Space): a dialog on wide screens, a sheet on phones. Rows for
  the reader's zone (marked with the lavender bar), the zone as written,
  UTC and each zone the reader added; how long until or since, ticking
  each second; a search over zone names and cities (matched on any part,
  spaces for underscores) to add a row, kept per device (the web chat's
  local storage, the app preferences); "Copy as ISO 8601"; and "Remind
  me" for a moment to come outside sealed chats, which sets a reminder
  about the message at that moment as "Remind me about this" does.
  Instants are drawn in the device's zone and locale: the web chat asks
  `Intl`, the apps the `timezone` package and `intl`.

### 9.6 Message actions

On hover on desktop, and by long-press on touch.

| Action | Notes |
|--------|-------|
| React | Recent emoji first, then the picker |
| Reply | Replies in the thread. With text selected, quotes the selection |
| Copy text, copy link | |
| Bookmark | |
| Pin | Managers |
| Remind me about this | Suggested times, or pick one |
| Tell me when they are around | A watch for the author (FEATURES.md 9.7). Messages by another person who is not a bot and not blocked; the app's message menu |
| Edit | Own messages, while the edit policy allows |
| Delete | Own messages; others' for managers and admins. Says that the content is kept as history until purged |
| History | Opens 9.7. Shown when the message has history |
| Ask for purge | Own messages. Says what happens in this kind of channel |
| Report | Asks for a reason |
| Follow thread, Ignore thread | On a parent, a reply in the flat view, and a marker |
| Mark unread from here | |
| Block this person | From the author's menu, where blocking is on |

A sealed chat's messages have React, Reply, Copy, Edit and Delete
only.

### 9.7 History view

- **Shows:** a message's earlier versions, newest first, each with
  when it was replaced and by whom, and the differences from the
  version after it marked. For a deleted message, its last content and
  who deleted it. For a checklist item, opened from the item: every
  tick and untick with who and when.
- **Does:** nothing but read. A purged or hidden history shows a line
  saying so.

### 9.8 Search

- **Shows:** a query box with filters for server, conversation,
  author, dates (since and until) and "has a file"; results newest
  first across every signed-in server, each with its server,
  conversation, author, time and the matching lines.
- **Does:** open a result in place: in its conversation, or in its
  thread if it is a reply, switching server if need be. Search within
  the open conversation starts with that server and conversation set.
- How the servers are asked and the results merged is in section 8.4.
- Sealed chats are never searched, and the screen says so when a
  sealed chat is open.

### 9.9 Threads view

- **Shows:** every thread the person follows that has unread replies,
  newest reply first, across all conversations on this server: the
  parent's first lines, where it is, and the unread replies.
- **Does:** reply in place; mark a thread read; stop following; open
  the thread in its conversation. "Show read threads" lists followed
  threads with nothing new.

### 9.10 Bookmarks

- **Shows:** the person's bookmarked messages, newest bookmark first,
  with their conversation.
- **Does:** open in place; remove the bookmark.

### 9.11 Profile and status

- **Someone else's profile:** avatar, display name, username (with
  `@server` for people from other servers), title, pronouns, local
  time, presence, status, availability, group labels, role. Buttons to
  message them (opens the ad-hoc channel with them), start a sealed
  chat, call, and block; and "Tell me when Ada is around", or "Stop
  watching for Ada" while one waits (a watch, FEATURES.md section 9.7).
  Not for a bot, oneself, a removed account or someone one has blocked.
  The answer is said in a toast: that it is set for 48 hours, or that
  they are around now.
- **Blocked accounts:** the people one has blocked, each with Unblock;
  on the account page in the web, and in the server's settings in the
  app ("Blocked accounts").
  Under presence, for someone who is not online, roughly when they
  were last here: "Last seen recently", "within a week" or "a while
  ago", from `last_seen` on the account fetched for the profile.
  Never a time; nothing when the person hides it. Hiding one's own is
  a switch in the server's settings in the app and in Preferences on
  the account page (`hide_last_seen`).
- **One's own profile:** the same fields, editable, one tab per
  server, since a person can be someone slightly different on each.
  Saving asks which servers to apply the change to (section 8.9). A
  link to each server's account page in the browser for passkeys, the
  access key, sessions and tokens.
- **Status:** an emoji, a line of text, when it clears, and two
  switches: pause notifications and appear away. Presets, with "Save
  as preset". Set once for every server that has not been opted out
  (section 8.6); the screen names any server where it differs.
- **Availability:** in a meeting or out of office, each with an end
  time, and the personal switch for whether a meeting pauses
  notifications.
- **Appear away:** for a duration or until turned off.

### 9.12 Notification and sidebar settings

**For one conversation,** from its menu or its row's menu. Two
settings, side by side, that do not constrain each other.

*What notifies* (`mode`):

| Choice | Effect |
|--------|--------|
| Any activity (`any`) | Every message notifies and counts in the badge |
| Mentions (`mentions`) | Mentions and replies in followed threads notify and count |
| Nothing (`nothing`) | Nothing notifies. The row is dimmed. A mention by name still counts in the badge |
| Mute for… | An hour, eight hours, a day, a week, or until a chosen time. Behaves as Nothing until then, and then the choice above returns. Sent as `muted_until` |

Named channels start at Mentions; ad-hoc channels and sealed chats at
Any activity. A sealed chat has no Mentions choice.

Beside it, for a channel, a switch: *Ignore @channel, @here and
@everyone* (`ignore_broad_mentions`). On, those no longer notify the
person here; a mention by name or of another group still does.

*Show in sidebar* (`show`):

| Choice | Effect |
|--------|--------|
| App default | Follows the default set in App settings. The screen names what that currently is |
| Always (`always`) | The row is always there |
| When there is activity (`activity`) | Only while there are unread messages |
| When it notifies (`notification`) | Only while it has a badge |
| Hidden (`hidden`) | No row. Still reachable by search, links, the quick switcher and "Show hidden" |

Under the two is one line that puts the pair into words, for example
"Mentions will notify you. The channel appears in the sidebar only
while it has something unread." When the pair is Hidden with a mode
that notifies, the line says where those notifications will be seen:
the notifications list. The thread view override is on the same
screen. Each change is one `updateMembership` call.

**For one server:**

- the default thread view, and whether notify anyway is allowed
  (`updatePrefs`);
- mute this server for a duration (section 8.8);
- "Take part in shared status and do not disturb", on by default
  (section 8.6);
- quiet hours for this server (9.13);
- a table of every conversation on the server with its two settings,
  hidden ones included, where either can be changed in place and
  several rows can be changed together.

**For this device** (app preferences, section 8.2):

- the default for *Show in sidebar*: Always, When there is activity,
  or When it notifies;
- "Show hidden";
- whether notifications are shown at all, sounds, and whether a
  notification shows the message text or only the sender;
- on Android, the choice between UnifiedPush and staying connected
  (`PUSH.md` section 10);
- "Send a test notification".

### 9.13 Do not disturb and quiet hours

- **Shows:** whether the person is in do not disturb now, on which
  servers, why (manual, quiet hours, a status, a meeting, a muted
  server), and until when.
- **Does:** turn do not disturb on for 30 minutes, an hour, until
  tomorrow morning, until a chosen time, or until turned off; turn it
  off. Both go to every server taking part (section 8.6). Turning it
  off makes the person available now on those servers, whatever the
  cause was: it clears a manual do not disturb, the pause switch of
  the current status, the current quiet period, and the pause from
  the current meeting. The screen says which of those it will clear.
- **Quiet hours** are per server (section 8.7): for each, a switch, a
  start and end time, the days, and the time zone, which the app
  keeps current from the device. "Skip tonight" skips that server's
  next period only. "Copy to…" puts a schedule on other servers.
- Do not disturb is also one tap away at the top of the sidebar and
  in the desktop tray menu.

### 9.14 Call screen

- **Before joining:** a preview of the camera, microphone level,
  device choices, and Join with microphone and camera on or off.
- **In the call:** a grid of tiles (camera, or avatar when the camera
  is off), each with the name, a muted mark and a speaking outline. A
  shared screen takes the main area with cameras in a strip. Layouts:
  grid, speaker, and pinned. A tile whose video is paused for
  bandwidth says so.
- **Controls:** microphone, camera, share screen, switch camera
  (phones), speaker or earpiece (phones), device settings, the
  participant list, leave.
- **Does:** tell the server which tiles are showing and how big they
  are whenever the layout changes (`CALLS.md` section 7); shrink to a
  small floating window so the conversation can be used during the
  call; mute someone for oneself only.
- A sealed chat's call shows a lock and "End-to-end encrypted".
- Before joining a call that another server hosts, the app says so:
  the device will connect to that server, and its operator can see
  the device's network address (`CALLS.md` section 16).
- **Incoming call:** a banner or small window with the caller,
  the conversation, Join and Decline (`PUSH.md` section 13).

### 9.15 Members and conversation settings

- **Members:** the list with levels and group labels; who is here
  through a group. Managers can add people and groups at a level,
  change levels and remove people. Adding to an ad-hoc channel shows
  the notice about history. In a locked channel the add buttons are
  gone and a line says why.
- **Settings** (managers): name, topic, section, default level,
  welcome text, retention, slow mode, check-in and ticket forms,
  archive, and the parent channel (9.25). Naming an ad-hoc channel is
  here. Channel names need not be unique. When the name typed is
  already used by channels the person can see, the dialog lists them
  with their details (9.26) and lets the person carry on.
- **Check-ins** (managers, not in a channel shared with other servers;
  `FEATURES.md` section 11): the channel's recurring check-in forms,
  each with when it runs, how many questions, the next run or why it
  failed, and who set it up; "New check-in", Edit and Stop. The editor
  has a title, an optional message, up to 20 questions (each short or
  long, optionally required), the days of the week, a time and a time
  zone (the device's to start with). Whoever edits one becomes its
  owner. A check-in that runs every week or month is shown as such and
  becomes daily on the days chosen when edited.
- **Make private** (managers and admins, public channels only). A
  dialog says: people who are not members lose the channel at once,
  its history and search results included, and a private channel can
  never be made public again. The person types the channel's name to
  confirm. There is no "make public" anywhere.
- **Lock** (private channels: the managers of a named or ad-hoc one,
  and any admin for a named one, from the admin console). A dialog
  says what locking does and that it cannot be undone: nobody can be
  added or join, a group attached to the channel brings in nobody
  new, people can still leave, and someone who leaves cannot come
  back. The person types "lock" to confirm. A locked channel shows a
  closed-padlock mark in its header and in the channel list.
- **@channel, @here and @everyone** (managers): who may use them in
  this channel. "Follow the server" (anyone, until the channel is
  larger than the server's limit), "Anyone" or "Managers only". Sent
  as `broad_mentions`.
- **Announcement channel** (managers, in a named channel that is not
  shared): a switch, and under it "Replies in threads" while it is on.
  In the member list of an announcement channel a manager's menu for a
  member has "Let them post here" / "Stop them posting here"
  (`setMemberMayPost`), and whoever is allowed is marked. For anyone
  who is not acting as manager the Members tab says how many members
  there are and that an announcement channel does not show who; the
  list is not asked for. The same switch is offered when a channel is
  made.
- **Delivery when not open** (managers, named channels): "Follow the
  server", "Only to people who have it open" or "To everyone,
  always". Sent as `lazy_delivery` (`docs/EVENTS.md` section 4.1).
- **For everyone:** pins and files posted here (both also from
  buttons in the conversation's header: the pins as a list that opens
  each message in place, the files as a search with "has a file" set), "Request a webhook" (in
  named channels), leave. Someone here through a group is told they
  cannot leave and why. Leaving a locked channel warns that there is
  no way back. The last person leaving a private or ad-hoc channel is
  told that the channel and its content will be deleted.
- Moderation for managers (time out, reports for this channel) opens
  from the member's menu and the conversation menu.
- **Groups.** "Browse channels" lists the server's groups (not the two
  built-in ones) with their handle, name and size. An open group that is
  not filled from a provider has Join or Leave for owners, admins and
  members; joining adds the person to every channel the group is in.
  Guests and bots are put in groups by admins. In the web chat a
  profile's group tags carry the same Join or Leave.

### 9.16 Scheduled, reminders and saved replies

- **Shows:** messages waiting to be sent, reminders, watches ("When
  Ada is around", with when it expires), and the person's saved
  replies. In the app these are screens in the server's settings:
  "Scheduled" (messages and reminders), "Saved replies" and
  "Watching for" (the waiting watches).
- **Does:** edit, reschedule, send now or cancel a scheduled message;
  cancel a reminder; stop a watch; add, edit or delete a saved reply.

### 9.17 Quick switcher

A box that finds conversations and people by name as it is typed,
across every signed-in server, most recent first, each with its
server's mark (section 8.5). Enter opens. It finds hidden
conversations too. On phones it is the search field at the top of the
sidebar.

### 9.18 Welcome and first run

After the first sign-in to a server: the server's welcome text if it
has one, the notification permission request with a line on what
notifications contain, and on Android without Google services the
choice of how to be notified.

### 9.19 Sealed chat set-up

- **Starting one:** pick one person. The screen says what a sealed
  chat is and is not: two people only, no search, no export, not
  readable by the server, and that losing every way to unlock loses
  its history.
- **The first sealed chat on an account** also makes the backup key.
  The screen shows the recovery phrase, twelve words, once; asks for
  it to be written down; and checks two of its words before going on.
  It says which of the person's passkeys can unlock sealed history
  and which cannot.
- **Where the server's code is trusted.** The same screen says, in
  plain words: a passkey only works on a page served from the
  server's own address, so unlocking with a passkey passes the key
  through a page the server supplies, in this app as in the web chat.
  After that the app encrypts and decrypts in its own code. The
  recovery phrase and approval from another device do not pass
  through a server page.
- **In the chat:** a lock in the header; a notice in the body when
  either person's device list changes, when a browser is among the
  other person's devices, and when the other person has reset their
  keys; "Compare codes" in the menu (9.21).

### 9.20 Unlocking and device approval

A device is either unlocked and in the account's device list, or
neither. A device that has signed in and is still locked (the sign-in
was company sign-in, or by a passkey that cannot unlock) shows the
ways forward (`docs/SEALED.md` section 13):

| Way | What the person does |
|-----|----------------------|
| Use a passkey | The app opens the server's unlock page in the system browser. The person touches a passkey that can unlock, and the page hands the key back to the app, sealed to the app's own key. The screen repeats that this step runs on a page the server supplies |
| Recovery phrase | Types the twelve words into the app. Nothing goes through a server page |
| Another device | Below |
| Carry on locked | Sealed chats are listed and cannot be read on this device |
| Start again | For someone with no passkey that can unlock, no phrase and no other device. After a warning that earlier sealed messages can never be read again on any device, the app makes new keys and a new phrase. The other person in each sealed chat is shown that a reset happened |

**Approval from another device.**

- **On the new device:** a code of 20 characters in four groups of
  five, the same as a QR code, and "Waiting for approval". The
  request lasts ten minutes, with a button to make a new one.
- **On a device already unlocked:** a prompt, "Unlock a new device
  (Android app)?", naming the device and when it signed in. The
  person scans the QR code or types the 20 characters. The device
  goes on only if the code matches what it worked out from the
  request itself. A wrong code says the request may have been
  tampered with and offers to end that device's session.
- The code is long on purpose and the screen says why in one line: it
  is the only thing that stops a server swapping in keys of its own.

**The device list** is on the same screen: each device's name and
platform, whether it is a browser, when it was last seen, and which
one this is. Removing a device ends its session and takes it out of
the person's sealed chats; the app then replaces the backup key, and
says so.

### 9.21 Comparison code

From a sealed chat's menu, "Compare codes". Shows 60 digits in twelve
groups of five, and a QR code holding the same. Both people see the
same 60 digits if and only if nobody stands between them. They
compare in person or on a call they trust, or one scans the other's
QR code, which checks it automatically.

- Marking the other person verified is kept on this device only. It
  is not synced, and a new device starts without it.
- The screen says what a match proves and what it does not: nothing
  about the code a browser is running, or about who is holding the
  other phone.
- After the other person replaces their keys the code changes and the
  mark stays. After a reset the mark is removed and the chat asks the
  two to compare again.

### 9.22 Sign-in and pairing

- **Add a server:** a field for the server's address. The app reads
  the server's public description, shows its name and what it is, and
  then offers:
  - **Sign in.** The system browser opens the server's own sign-in
    page (OAuth with PKCE). The person signs in there by whatever
    methods that server allows, approves this device by name, and the
    browser hands back to the app: a loopback address on desktop, an
    app link on phones. The app makes its device keys before it
    starts and sends the public half with the request. If the person
    signs in with a passkey that can unlock, the server's page opens
    the backup key and seals it to the app, so the app arrives
    unlocked.
  - **Scan a code** (phones). The camera scans the QR code shown under
    "Add device" on the account page of a signed-in browser. The
    person confirms the device's name in that browser. If that
    browser is unlocked, the code also carries a secret that never
    reaches the server, and the one scan signs the app in and unlocks
    it.
- **Invite links** open the server's join page in the browser, which
  ends by offering "Open in the app".
- **After sign-in:** the device is registered for notifications
  (`PUSH.md` section 3), sealed history is unlocked if this sign-in
  can do it (9.20 otherwise), and the first sync runs.
- **Sessions and devices** (in the server's settings): every browser,
  app and token signed in, with when each was last seen; signing one
  out, or "Sign out all other sessions" after a confirmation, which
  ends every other one at once, personal tokens too.
- **Things that need a fresh check.** The apps hold no passkey and
  never take an access key, so anything the server guards with a fresh
  check (adding a passkey, a new access key, new recovery codes,
  personal tokens, "Add device") is done on the account page in the
  browser, where the passkey or the access key gives the check. If the API answers
  `fresh_check_required`, the app opens that page.
- **Signed out by the server** (a session that ended, as company
  sign-in sessions do daily): the server's tile shows a mark and the
  app asks to sign in again, keeping the local database until it
  knows the same account came back.

### 9.23 App settings

- **Sidebar:** the layout (merged tree, server rail); the
  default for *Show in sidebar*; "Show hidden"; the order of servers.
- **Servers:** each signed-in server with its settings (9.12), "Add a
  server", and "Set up another device" (section 8.2).
- Theme (section 12): one theme for the whole app or each server's
  default, the dark and light themes follow-the-system picks between,
  the high-distinction palette, increase contrast, and the theme
  editor; text size; whether Enter sends; the lock (9.27): lock automatically
  after a time without use and, on phones, lock in the background; start at login and close-to-tray on
  desktop; language; the file cache's size and "Clear cache";
  licences and the app's version; a link to the source.

### 9.24 Notifications

The one list of section 8.3.

- **Shows:** items newest first across all servers. Each has its
  server's mark, the conversation (and thread, for a reply), who, the
  time, why it is here (mentioned you, mentioned @design, reply in a
  thread you follow, new message, missed call) and the first lines.
  A sealed message is decrypted on the device like anywhere else.
  Items from one conversation that arrive together are folded into
  one entry with a count. Below the unread items, "Earlier" holds
  those cleared in the last week.
- **Filter:** all servers or one; and all kinds, mentions only, or
  threads only.
- **Does:** open an item in place, which reads it; mark one read
  without opening; "Mark all read" for the current filter; from an
  item's menu, change that conversation's two settings, or follow or
  ignore the thread.
- Its count in the sidebar is the number of unread items, which is
  the sum of the servers' badges.
- Notifications from hidden conversations are here like any others.

### 9.25 Sub-channels

A named channel may sit under another named channel, one level deep.
A sub-channel is a channel in its own right: its own members, levels,
public or private setting, lock and retention. Nothing is inherited.
The link is local to the server.

**In the sidebar.**

- A sub-channel is drawn indented one step under its parent, in the
  parent's section. Its own section is ignored while it has a parent.
- A parent with sub-channels has a disclosure mark. Collapsing it
  hides the sub-channel rows and is remembered on the device. A
  collapsed parent shows its own badge, and beside it a second,
  outlined count for what its hidden sub-channels hold, so nothing
  is lost from view. Opening a sub-channel expands its parent.
- Sub-channels are ordered by name under the parent, then by creation
  date where names are equal.
- **Merged tree and server rail:** as above, inside each server.
- **Stars:** a starred sub-channel stays under its parent.
- **Own sections:** a sub-channel goes with its parent unless moved
  itself. When it is under the same heading as its parent it is drawn
  indented under it; anywhere else its row reads `parent › name`.

**In a sub-channel and not in its parent.**

- If the person can see the parent (it is public), the parent has no
  row, because the person is not in it. The sub-channel is drawn at
  the parent's level, in the parent's section, as `parent › name`.
  The parent's name there opens the public channel, readable, with
  "Join to take part".
- If the person cannot see the parent (it is private and they are not
  in it), the server gives them no parent for that channel. The
  sub-channel is drawn as a top-level channel in its own section, and
  nothing hints that a parent exists.

**How `show` applies.** Every row follows its own `show` (9.2), and a
sub-channel's activity is not its parent's activity: it does not make
the parent unread or give it a badge.

- When a sub-channel's row is drawn and its parent's would not be
  (the parent is set to `activity` or `notification` and has nothing,
  or is `hidden`), no row is drawn for the parent. A hidden
  conversation never has a row in the sidebar, and nothing stands in
  for one. The sub-channel is drawn at the parent's level, in the
  parent's section, as `parent › name`.
- When the parent's own row appears, the sub-channel moves under it
  and drops the prefix. When the parent's row goes, it moves back
  out. Neither happens to the row of the conversation being viewed
  while it is open; it is put right when the person moves on.
- A parent's row being drawn does not bring its sub-channels' rows
  with it.
- The sub-channel being viewed always has its row, under its parent
  if the parent has one and as `parent › name` if not.

**Creating one.** "New channel" has an optional "Under" field: a
picker of top-level named channels the person is a member of. The
same choice is offered as "New sub-channel" in a channel's menu. It
calls `createChannel` with the parent set.

**Moving a channel under a parent, or out again.** In the channel's
settings, "Parent channel": pick one, or "None"
(`updateConversation`). On desktop, a row
can also be dragged onto a channel or back out to a section, in the
rail and merged layouts.

- Offered to a manager of the channel who is also a member of the
  chosen parent, and to admins.
- Refused, with the reason shown, when the channel has sub-channels
  of its own, or the chosen parent is itself a sub-channel. Ad-hoc
  channels and sealed chats have no such setting.
- It changes where the channel is listed and nothing else: not who is
  in it, not whether it is private. The dialog says so.

**On joining a parent.** After joining a channel that has public
sub-channels, the app shows them in a short list with their member
counts and topics, none ticked, and "Join selected". Skipping it
joins none. Private sub-channels are not listed. The same list is in
the parent's menu afterwards as "Sub-channels".

**What else appears as a sub-channel.**

- A thread moved to a channel (9.4): under the channel the thread was
  in, or beside it if that channel is itself a sub-channel.
- A ticket channel opened by a form: under the channel holding the
  form, by the same rule. Ticket rows are named `<prefix>-<number>`.
  A parent with many tickets is best kept collapsed, and its tickets
  set to show only on activity.

**When the parent changes.** If a parent is deleted, its sub-channels
become top-level channels in their own sections. Archiving, locking
or making a parent private does nothing to its sub-channels, and
their rows stay where they are.

### 9.26 Telling channels with the same name apart

Channel names are not unique. Any number of channels may share a
name, even under the same parent. The apps identify a channel by its
identifier everywhere and never by its name, and wherever a person
picks or sees a channel they show enough to tell which one it is.

**The detail line.** One line of secondary text, built from, in this
order and leaving out what does not apply:

- the server, when more than one is signed in;
- the parent, as `parent ›`;
- the section;
- the member count, and whether the person is in it;
- up to two managers' names;
- the date it was created.

for example "Friends' server · design › · Projects · 14 members,
you're in it · managed by Sam, Lee · since March 2026".

| Place | What is shown |
|-------|---------------|
| Sidebar | Position does most of the work: section, parent and server heading. When two rows with the same name sit under the same parent or in the same group, each gets the creation date after its name, and the full detail line as a tooltip or on long-press |
| Browse channels | The tree, with the detail line under every channel. Same-named channels are listed together, those the person is in first |
| Quick switcher | The detail line under every channel result. Words typed are matched against the parent's name as well as the channel's, so typing both narrows to that parent |
| The channel picker in the composer | The detail line under every entry; channels of the open conversation's server only; those the person is in first, then by recent use |
| Search | Each result's conversation is written `parent › name`, with the server's mark. The conversation filter is the same picker as above |
| Notifications list | The conversation is written `parent › name` with its server's mark. If that still matches another channel the person is in, the section is added |
| Conversation header | `parent › name`; the detail line is in the channel's details |
| Creating or renaming | The dialog lists the visible channels that already have the name, with their detail lines, and does not refuse |

**How a name is drawn.** A channel's name is free-form: 1 to 64
characters of any script, with spaces and capitals, such as "Design
reviews" or "デザイン". It is not a slug, and the apps do not treat it
as one.

- In the sidebar a row is an icon and the name exactly as written.
  The icon says what kind of channel it is: a hash sign for a public
  channel, a lock for a private one, a closed padlock for a locked
  one. The hash sign is an icon and not part of the name.
- In the conversation header the name is drawn the same way, after
  `parent ›` for a sub-channel.
- A name too long for its row is cut with an ellipsis at the end and
  shown whole in a tooltip or on long-press. Sub-channel rows cut the
  parent part first.
- Text direction follows the name's own script.
- Sorting and matching ignore case, and matching also ignores
  accents: typing "design" finds "Design reviews". Lists that sort by
  name use the device's locale order.
- Ticket channels are still named `<prefix>-<number>`.

**Linking a channel while writing.** A channel is linked by its
identifier, never by its name.

- Typing `#` and then characters opens the picker whenever what
  follows the `#` matches the start of a word in the name of a
  channel the person can see on this server. It opens every time
  there is a match, lists the matches with their detail lines, and
  asks whether to link one. Further typing narrows the list, and
  spaces are allowed, so `#design rev` still matches.
- **Choosing** a channel replaces the typed characters with a link
  chip that shows the channel's icon and current name. In the text
  that is sent, the chip is `<#conversation-id>`, the channel's
  identifier and nothing else. The chip is one unit: the cursor steps
  over it and one backspace removes it.
- **Dismissing** the picker (Escape, tapping away, or typing on past
  the last match) leaves the characters exactly as typed. `#games`
  typed and left alone is plain text. Nothing resolves it later, on
  the server or in any client.
- A draft keeps its chips. Editing a message shows its existing links
  as chips again and sends them back as they were.
- Pasting text that contains `<#…>` with a valid identifier of a
  channel the person can see shows a chip; anything else is pasted as
  it is.

**A channel link in a message being read.**

- The server turns each `<#conversation-id>` that names a channel the
  author could see into a channel entity. The link is that one
  channel, however many channels share its name.
- It is drawn as a chip with the channel's icon and its **current**
  name, so a rename shows everywhere at once. On hover or long-press
  it shows a small card: the detail line, the topic, and Open or
  Join. The card is how a reader tells which of several same-named
  channels is meant.
- Opening it goes to that channel.
- A link to a channel the reader cannot see is drawn "private
  channel", with no name and no card.
- A `<#…>` that names no channel has no entity. It is drawn "unknown
  channel".
- **Copying a message as text** writes the channel's name in place of
  each link, as it is at that moment, and the words "private channel"
  or "unknown channel" for the other two. Identifiers are never put
  on the clipboard this way. "Copy link" on the chip copies a link to
  the channel itself.

### 9.27 The lock

For a person at a shared or unattended computer or phone (ruling R75,
`AUTH.md` section 10.6): Churr is locked on this device until a
passkey, or the account's access key, unlocks it, without signing
out.

- **Ways to lock.** `/lock` (a protected built-in command), "Lock" in
  the account menu, Mod+Shift+L on desktop and in the web chat
  (section 13), and two settings of the device, both off by default:
  lock automatically after 1, 5, 15 or 60 minutes without use, and,
  on phones, lock when the app goes to the background. Use is a
  pointer or a key in the app's window; in the web chat, in any tab of
  the browser. The setting is in app settings (9.23) and, for a
  browser, on the account page's "Sessions and devices".
- **Calls.** Any lock ends this device's part in a call in progress.
  The command, the menu and the key ask first ("Lock and leave the
  call?"); the automatic lock does not ask and fires during a call
  too, as its setting says (the owner's decision).
- **What is locked.** In the apps: every signed-in server's session,
  each on its own (`lockSession`); the person's other devices are not
  locked. In the web chat: the browser's session, so every tab of it.
- **The lock screen.** It takes the whole interface's place: in the
  apps the navigator and every route on it leave the widget tree; in
  the web chat the page is replaced by `/locked/` (with
  `location.replace`, so Back does not bring it back), which unloads
  everything the chat held in the page and in memory, and removes the
  drafts it keeps in the browser's storage; the tab is
  titled "Churr". It shows the product mark, "Locked" and one Unlock
  button (for an account with an access key, the key's field as well:
  under "Use your access key instead" when it has a passkey, alone when
  it has none or the browser cannot use passkeys), over a blurred
  stand-in of the layout made of empty blocks:
  no name, message, avatar, title, count or preview, because none is
  there to blur. The blur is the owner's request for this one screen
  and the single exception to `STYLE.md`'s "no blur". The servers'
  in-memory state (conversations, accounts, badges) is let go in the
  apps, and a restart while locked stays locked and reads nothing.
- **Screen capture.** While locked the window is kept out of
  screenshots, screen recording and the app switcher's picture where
  the platform allows: FLAG_SECURE on Android (also whenever "lock in
  the background" is on, since the picture is taken as the app
  leaves), a sharing type of none on macOS. iPhone, Windows and Linux
  have no such switch for an app's window.
- **Unlock.** The web chat runs the passkey assertion, or takes the
  access key, on its lock screen. An app opens each locked server's
  `unlock_url` in the system browser (ruling R43), where the same page
  offers the passkey or the access key, and asks the server every two
  seconds, for up to five minutes, until the session answers again; then every server
  reads its database, connects and catches up as after any
  reconnection. Sealed chats come back from the copy of the backup key
  left for the device's own box key, or, in the web chat, from the
  same touch's PRF output; otherwise they ask as they already do
  (9.20).
- **Signed out while locked.** If the session was revoked or ended
  meanwhile, unlocking finds it signed out: the web chat goes to the
  sign-in page, and an app shows that server as signed out.
- **The tray** on desktop shows nothing while locked: the plain icon,
  no count, and a menu of Open Churr and Quit. Desktop notifications
  stop, since the connection is closed.
- **Phones while locked** show, for a wake-up, only "You have a new
  notification" (`PUSH.md` section 8).

### 9.28 Chat requests and closed chats

Rulings R77, R78 and R79; `FEATURES.md` section 20. The same in the
apps and the web chat.

**Requests.** A "Requests" entry sits with Notifications, Threads and
Bookmarks at the top of the sidebar, with the number waiting (across
servers in the apps). It opens the list: for each request the
person's avatar, display name and `@username`, whether they ask for a
direct or a sealed chat, and when; four buttons, Accept, Decline,
Dismiss and Block; and one line saying that a decline is told and
the other two are not. Accept opens the chat. Nothing else about the
person or their intent is shown, because nothing else is sent. A
`chat_request.received` with `notify` true shows a notification like
a mention (the app's own switches and the server's mute apply), whose
title is the person and whose body is "wants to chat with you". A
phone that is woken for a request (`PUSH.md` section 2) fetches the
wake-up and shows the same notification, opening the app; when the
request no longer waits it shows nothing (Android) or the placeholder
(iPhone).

**A chat that waits, was declined or was closed** (`chat_state`). Its
row carries a tag (`waiting`, `declined`, `closed`). In place of the
composer a bar says what it is:

| `chat_state` | The bar says | Action |
|--------------|--------------|--------|
| `waiting` | Waiting for Ada to accept your request. You can write once they do. | Withdraw request |
| `declined` | Ada declined this chat. | – |
| `closed` | This chat is closed. You can both still read it. | Ask to chat again |

"Ask to chat again" starts a new chat with the same person
(`openChat`, or for a sealed chat a new sealed chat with a new epoch
record 1 made on the device) and opens it.

**Closing.** "Close chat" is in the menu of a direct chat and of a
sealed chat (the header's close button in the app's conversation
screen, the sealed chat's menu, the web chat's conversation and row
menus), and `/close` typed in one marks it read and closes it. It asks
first, saying both people keep the history and neither can write. In
a chat that waits the same item is "Withdraw request…". `/close`
anywhere else hides the conversation until something new arrives.

**Preferences.** "Auto approve new direct chats" and "Auto approve new
sealed chats", both off, sit with the account's other preferences
(the app's server settings, the account page in the browser).

## 10. Adaptive layout

One layout shell arranges the same screens by available width, not by
platform. A resized desktop window and a tablet in split view follow
the same rules. "Sidebar" below is whichever of the two layouts the
person chose (9.1).

| Width | Class | Arrangement |
|-------|-------|-------------|
| Under 600 (phones) | Compact | One pane at a time. Sidebar → conversation → thread, each pushed over the last, with a back gesture. Call and search are full screen |
| 600 to 1023 (tablets, small windows) | Medium | Two panes: sidebar and conversation. A thread opens over the conversation. Settings and profiles are dialogs |
| 1024 to 1399 | Expanded | Sidebar and conversation side by side. A thread opens as a third pane and the sidebar narrows to make room |
| 1400 and over | Wide | Sidebar, conversation and thread pane all at full width |

Widths are in logical pixels.

How each layout fits each class:

| Layout | Compact | Medium | Expanded and wide |
|--------|---------|--------|-------------------|
| Merged tree | The sidebar is the whole first screen: server headings that collapse, so a person with many servers can fold all but one | One column, 300 wide | One column, 280 to 360, dragged to taste. No rail is drawn |
| Server rail | The rail is a strip of tiles across the top of the first screen; swiping sideways on the list moves to the next server | A 64-wide rail beside a 280-wide list | The same: rail, list, conversation |

- A thread pane is 380 to 480 wide. The conversation takes the rest
  and its text column stops growing at about 900, so lines stay
  readable on a wide monitor.
- Pane dividers can be dragged on desktop, and the widths are
  remembered.
- On touch devices: swipe a row for its main action, long-press for a
  menu, pull down to load older messages. On desktop: hover actions,
  right-click menus, drag and drop.
- In the merged layout there is no rail, so per-server actions are
  reached from a server's heading and from App settings.
- Rows and headings are dragged with the pointer at once; on a touch
  screen, by their handles in reordering mode (9.1).
- Phones in landscape use the compact class unless the width passes
  600.
- The on-screen keyboard pushes the composer up and never covers it.
- Desktop windows have a minimum size of 360 by 480.

## 11. Accessibility

Requirements, to be met on every target and in the web chat.

- **Screen readers.** Every control has a name. A message is read as
  one item: author, time, text, then what it carries ("2 files, a
  poll, 3 reactions"). New messages in the open conversation are
  announced politely. The marker, the "new" line and day dividers are
  announced as what they are.
- **Keyboard.** Everything can be reached and operated without a
  pointer on desktop and in the web chat. Focus order follows reading
  order. Focus is always visible: a 3-pixel outline in the focus
  colour (section 12). Dialogs trap focus; closed, they return it to
  what opened them when that was the keyboard, and to the composer
  when it was a click (9.5).
- **Text size.** The apps follow the system text size, and layouts
  hold up to 200%. Nothing is cut off; rows grow.
- **Contrast.** Text and icons meet 4.5 to 1 against their background
  in both themes, and control outlines meet 3 to 1. The colour tokens
  in section 12 were chosen to do this; the figures are computed, not
  yet checked on real screens.
- **Not by colour alone.** Unread is also bold, a mention also has a
  count, presence also has a shape, an error also has an icon and
  words, a muted microphone also has a struck-through icon.
- **Touch targets** are at least 48 by 48 logical pixels.
- **Motion.** The apps follow the system's reduce-motion setting.
  Nothing flashes. Animated emoji and GIFs do not play by themselves
  when reduce-motion is on.
- **Sound.** Every sound has a visible counterpart, and the ring also
  vibrates on phones.
- **Language.** Wording is in `l10n` files, dates and numbers follow
  the locale, and layouts work right-to-left. English ships first.
- **Images** can have a caption (`MESSAGE-FORMAT.md` section 5), which
  is what a screen reader reads for them.

## 12. Themes

Colour, shape and type follow `STYLE.md` (ruling R60) and the approved
guide, `docs/style-guide.html`; where this section and `STYLE.md`
differ, `STYLE.md` is right. There is no accent setting: the four
neons each keep their one job, and a server looks like itself through
its default themes instead (ruling R61).

- **Built-in themes:** dark (the default) and light, each also in the
  high-distinction palette (pink becomes orange, lavender blue), and
  each with "increase contrast", which follows the system unless set.
  They cannot be edited, only copied.
- **Themes people make** (ruling R61): a name, a kind (dark or light)
  and nine base colours: the three ground steps `bg`, `panel`,
  `raise`, `ink`, `dim`, and `lav`, `cyan`, `pink`, `yel`. Every other
  token is derived from those by the rules of `STYLE.md` ("Themes"),
  and a theme whose text falls below 4.5:1 or whose control edges fall
  below 3:1 cannot be saved, shared or applied. The editor (apps and
  web chat) starts from any theme, previews a real conversation, shows
  each pairing's measured ratio and offers the smallest change that
  fixes a failing colour. Themes are stored per account on a server
  (`listMyThemes` and the rest of the Themes area of the API), can be
  published to that server's theme library and added from it, and
  have a link, `{origin}/themes/{id}`, drawn in a message as a theme
  card with swatches, name, author, "try" and "add". Between servers
  a theme travels as a `.churrtheme` file (JSON), also drawn as a
  theme card.
- **Which theme is in use** is a preference on the device: one theme
  for the whole app, or "use each server's default" (the server's
  `default_themes`, which an admin sets and which apply before sign-in
  too). Follow-the-system picks between the person's chosen dark and
  light themes.

### Colour tokens

The tokens, their values in both built-in themes and in the
high-distinction palette, and the contrast every pairing must meet are
those of `STYLE.md` ("Colour" and "Contrast"): `--bg`, `--panel`,
`--raise`, `--line`, `--grid`, `--ink`, `--dim`, `--faint`, `--edge`,
`--lav`, `--cyan`, `--pink`, `--yel`, `--yel-ink`, their partner text
colours `--on-lav`, `--on-cyan`, `--on-pink`, `--on-yel`, and `--on`
and `--paper`. The apps hold the same values in
`app/lib/ui/theme/tokens.dart`; the web in `shared/tokens.css`.

- Every fill is used with its partner text colour, chosen by
  measurement. Yellow is never text on light: where yellow must be
  text or a thin line, `--yel-ink`.
- Semantic states reuse the four: success and "on" are cyan, failure
  and danger pink, waiting yellow. There is no green and no red.
- A card's `color` (`MESSAGE-FORMAT.md` section 6) and a group's
  colour label are drawn with the four role colours for lavender,
  cyan, pink and yellow; green is drawn as cyan, red as pink and grey
  as faint.
- Code is highlighted with these tokens: keywords `--pink`, strings
  `--yel-ink`, numbers and constants `--cyan`, names of functions and
  types `--lav`, comments `--dim`, everything else `--ink`. The apps
  and the web chat use the same mapping from highlight.js token names.
- The focus outline is 3 pixels of `--yel`, with a dark halo on light.

### Shape and type

- Corners are square everywhere: buttons, fields, tiles, cards,
  images, avatars.
- Outlines are solid, 1.5 or 2 pixels. A focused or selected thing
  gets a thicker line or a fill, never a glow.
- **Archivo** for interface text and messages. **Martian Mono** for
  code, times, counts, labels and keyboard hints. Both are bundled
  with the apps and served by the server for the web; nothing is
  fetched from a font service. Emoji use the system's emoji font.
- The catalogue's crooked stickers and glitch lettering are for the
  sign-in and empty screens only. Working screens are straight.

## 13. Keyboard shortcuts on desktop

"Mod" is Command on macOS and Control elsewhere. The web chat uses
the same keys where the browser does not take them.

| Keys | Action |
|------|--------|
| Mod+K | Quick switcher |
| Mod+1 … Mod+9 | Switch to that server in the rail, or jump to its heading in the merged tree |
| Alt+Up, Alt+Down | Previous, next conversation; on a focused sidebar row or heading, move it one place (9.1) |
| Alt+Shift+Up, Alt+Shift+Down | Previous, next unread conversation |
| Mod+F | Search in this conversation |
| Mod+Shift+F | Search everywhere |
| Mod+Shift+T | Threads view |
| Mod+Shift+S | Bookmarks |
| Mod+Shift+A | Mark all read on this server |
| Mod+Shift+I | Notifications |
| Mod+N | New chat |
| Mod+Shift+N | New channel |
| Mod+, | App settings |
| Mod+Shift+D | Do not disturb on or off |
| Escape | Close the thread pane, dialog or search; then mark the conversation read |
| Up, in an empty composer | Edit one's last message |
| Enter | Send. Shift+Enter: new line |
| Mod+B, Mod+I, Mod+Shift+X | Bold, italic, strikethrough |
| Mod+E, Mod+Shift+C | Inline code, code block |
| Mod+Shift+K | Insert a link |
| Mod+U | Attach a file |
| Mod+Shift+M | Mute or unmute the microphone in a call |
| Mod+Shift+V | Camera on or off in a call |
| Mod+Shift+H | Leave the call |
| Mod+Shift+L | Lock Churr on this device (9.27). Safari keeps Command+Shift+L for its sidebar; there, use the menu or `/lock` |
| With a message focused: R, E, T, P, Delete | React, edit, reply in thread, pin, delete |
| Mod+/ | Show this list |

## 14. Distribution

| Channel | Build | Notifications |
|---------|-------|---------------|
| App Store | iPhone, signed by Tikaydaes, identifier `<APP-ID-NOT-CHOSEN>` | APNs through the relay |
| Play Store | Android with `firebase_messaging` added | FCM through the relay |
| F-Droid | Android with no Google library, built from the repository as checked in | UnifiedPush or the foreground service |
| Direct downloads | macOS (signed and notarised disk image), Windows (signed installer), Linux (AppImage and a `.deb`), and the F-Droid-style Android APK | Desktop apps stay connected |

- Desktop builds and the APK are attached to each release on GitHub
  with checksums.
- The Play and App Store builds are the only ones the Tikaydaes relay
  can wake, because push reaches only apps signed by the holder of the
  push keys. Someone who builds their own iPhone or Play app must run
  their own relay with their own keys and point their server at it.
- The apps do not update themselves. Stores update store builds. A
  desktop app checks nothing by itself; the server tells a connected
  app the lowest app version it supports, and an app older than that
  says an update is needed.
- Every build shows its version and the commit it was built from.
- App store review applies to each release of the store builds
  (`BRIEF.md`, "What this now costs to run").

---

# Part two: the web

## 15. Pages the binary serves

All are embedded in the `churr` binary and served from the server's
own address. They load nothing from anywhere else.

| Address | Page | For |
|---------|------|-----|
| `/` | Redirects to `/chat/` when signed in, to `/login` otherwise | |
| `/setup/<token>` | First-run setup | The person installing the server. Works once |
| `/login` | Sign-in | Passkey, access key (username and key), each company sign-in provider as a button, recovery code. Then the second factor step when policy requires one. A browser without passkeys is offered the access key first |
| `/join/<code>` | Join by invite link | Choose a username and make a passkey, or take an access key ("I can't use a passkey — give me an access key"), or use company sign-in. Also claims an imported account |
| `/recover/<token>` | One-time recovery link | Make a new passkey or a new access key |
| `/oauth/authorize` | Approve an app | Shown in the system browser when an app signs in: sign-in by passkey, access key or provider, then the app's name and platform, a field for the device's name, Approve and Cancel |
| `/unlock` | Unlock sealed history with a passkey | Opened by an app that is signed in and locked. Described below |
| `/account` | Account | Section below |
| `/admin` | Admin console | Section 16. Admins and the owner |
| `/chat/` | Web chat | Section 22 |

Addresses used during company sign-in and the OAuth token exchange are
defined in `docs/AUTH.md`.

**First-run setup** walks through: the owner's username, display name
and passkey (or access key); the recovery codes or the access key,
shown once; the community's name; the
table of outside connections from `DESIGN.md` section 17 with a switch
for each that has one (the push relay is set up later in the admin
console, since it needs an address); the registration mode. It ends in `#general`.

**The fresh check prompt.** Some actions on the account page and in
the admin console need the session to have been checked within the
last ten minutes (`docs/AUTH.md` section 10.4). When the API answers
`fresh_check_required`, the page shows a dialog asking for a passkey
touch, the access key (under "Use your access key instead" when the
account also has a passkey) or an authenticator code, or sends the
person back through company sign-in if they have none, and then
repeats the action. It
is one shared dialog, in `shared/fresh.js`.

**The unlock page** is the server page the apps open to unlock sealed
history with a passkey (9.20), and the last step of sign-in when the
passkey used can unlock. It asks for the passkey, opens the backup
key in the page, seals it to the waiting app's key and keeps nothing.
`docs/SEALED.md` section 13.3 defines it.

**The account page** has:

- profile (the same fields as in the apps);
- passkeys: add, rename, remove, and for each whether it can unlock
  sealed history;
- access key: whether the account has one, when it was made and last
  used (never the key); make one or a new one, which is shown once and
  confirmed by typing its last group; remove it while a passkey or a
  company identity remains;
- recovery codes: how many are left, make a new set;
- authenticator app: set up or remove;
- company sign-in identities linked to the account;
- sessions and devices: every browser and app signed in, last seen,
  sign out any of them; "Add device" shows the pairing QR code;
- personal tokens: create, name, revoke;
- sealed chats: whether this browser is unlocked; unlock it with a
  passkey, the phrase or another device; approve a waiting device by
  its 20-character code; replace the phrase;
- notification preferences that are not per device: default thread
  view, notify anyway, quiet hours, meeting pauses notifications;
- blocked accounts.

Leaving the server by deleting one's own account is not built: there
is no operation for it. An admin removes an account.

## 16. The admin console

One page at `/admin` with a section per row below. It exists only on
the web. "Owner" marks sections only the owner sees.

| Section | Shows and does |
|---------|----------------|
| Overview | Version, uptime, database kind and size, file store and size, members online, connected clients, live calls, the state of each outside connection, and warnings (a certificate near expiry, a failing peer, a media port that looks closed) |
| Settings: General | Community name and icon, the server welcome text |
| Settings: Sign-in | Registration mode, who may invite, member sign-in policy, second factor and what is accepted, session lengths. Each change needs a fresh check |
| Settings: Messages | Quick-correction window, edit window, server retention period, who may use `@channel`, `@here` and `@everyone` and the size of channel above which only managers may, who may add emoji, upload size limit, blocking on or off |
| Settings: Outside connections | The table from first-run setup: push relay switch and address (the relay is off until it has an address), UnifiedPush, badge numbers in wake-ups, link previews, object storage, sealed chats on or off, calls on or off and their limits, presence to linked servers |
| Invites | Create (role, uses, expiry, groups, and for guests the channels), list, revoke. Claim links for imported accounts |
| Accounts | Search and list; role (owner only for changes), sign-in methods, groups, last seen; make a recovery link; end sessions; time out server-wide; remove; ban |
| Groups | Create, edit handle, name, colour and open; members; map to a provider group; the two built-in groups shown read-only |
| Channels | Every public channel, and every named private channel with its name, members and managers only. Appoint a manager, archive, set a retention period, move to a section, make a public channel private and lock a named private channel (both one-way, with the same warnings as in the apps). Ad-hoc channels and sealed chats do not appear |
| Sections | Create, rename, reorder |
| Bots | Create (name, avatar), tokens, the channels a bot is in, its commands, remove |
| Webhooks | Requests waiting, with who asked and for which channel: approve or decline. Existing webhooks: revoke. Who approves requests |
| Integrations | Create, tick what it may do, show the token once, last used, revoke |
| Saved replies | The community's `/name` commands |
| Status presets | The community's presets |
| Emoji | Upload, rename, remove |
| Word filter | Words and patterns, each set to block or flag |
| Reports | Open reports from public channels: dismiss, delete the message, time the author out |
| Purge requests | Waiting requests from public channels: show the history again, keep it hidden, or erase |
| Timeouts and bans | Current ones; lift |
| Company sign-in | Add an OIDC or SAML provider, test it, map provider groups to the admin role and to groups, enable or disable |
| Linked servers (owner) | Create a link invitation, accept one, join a hub; each peer's name, key fingerprint and state; pause or remove |
| Shared channels (owner) | Offers received and sent; share a channel (peer, servers, days of history); stop sharing; per-stream file size limit |
| Calls | Live calls with participant counts (not who), the media address in use, and "Test calls" (`CALLS.md` section 12) |
| Audit log | Every admin and owner action, filtered by actor, action and date |
| Export (owner) | Start an export, download it when ready |

Changing a setting takes effect at once, with no restart, and is
written to the audit log.

## 17. How the pages are built

- **Plain HTML, CSS and JavaScript modules.** No framework, no
  bundler, no transpiler, no package manager. The files in the
  repository are the files the browser gets. `go build` alone produces
  the whole server, as with Codemail.
- **One small HTML file per area**, each loading one entry module with
  `<script type="module">`. Inside an area, moving between views is
  done in the page with the History interface; the server answers any
  address under `/admin` or `/chat/` with that area's HTML file.
- **Everything goes through the API.** The pages hold no data of their
  own and have no private endpoints. The web chat uses the same live
  connection and the same catch-up as the apps (section 7), keeping
  what it has loaded in memory.
- **DOM built by hand.** A small helper makes elements. Message
  content never goes through `innerHTML`: the Markdown renderer makes
  nodes one by one.
- **Styling** is one set of CSS custom properties, the tokens in
  section 12, in `shared/tokens.css`. The pages follow the system's
  light or dark unless the browser's own setting says otherwise
  (`STYLE.md`, "Themes and settings").
- **Third-party code** is limited to one vendored piece, committed
  as files: highlight.js (core and the grammars for the languages in
  `MESSAGE-FORMAT.md` section 2.3). Sealed-chat encryption is the
  project's own JavaScript on the browser's Web Crypto.
- **Caching.** Files are served with an `ETag` and `no-cache`, so a
  browser always asks whether its copy is current and a new server
  version is picked up on the next load without a build step naming
  files by hash.
- **Browsers:** current Firefox, Chrome and Safari, on desktop and
  phone.

## 18. One Markdown, three implementations

Churr Markdown is drawn by the apps (Dart) and the web chat
(JavaScript), and read by the server (Go) to find entities, write
fallback text and make search text. Three languages cannot share
code, so they share a specification and a set of tests.

- **The specification** is `MESSAGE-FORMAT.md` section 2. It is a
  fixed list of constructs. Anything not on it is shown as typed.
- **The node tree.** Each implementation parses text and entities into
  the same tree: blocks (`paragraph`, `heading` with a level, `quote`,
  `list` with `ordered` and `start`, `item` with an optional task
  identifier, `code` with a language, `table` with alignments, `rule`)
  holding inlines (`text`, `bold`, `italic`, `strike`, `spoiler`,
  `code`, `link`, `break`, and one node per entity type). A renderer
  then turns the tree into widgets or DOM nodes.
- **Instants** (ruling R69) are an inline node of the tree, `instant`,
  with the typed `local` date and time and the `zone` as written. Their
  grammar is in `MESSAGE-FORMAT.md` 3.1 and the corpus
  (`testdata/markdown/instants.json`); resolution and the composer's
  phrases have their own shared cases in `testdata/instants/`, run by
  `go test ./internal/instant`, `e2e/instants.mjs` and the Flutter tests
  in `app/test/core/instant/`.
- **Objects** (ruling R73) are not nodes: each lies inside a `text`
  node. Each client has its own scanner for them (the web chat's
  `shared/md/objects.js`, the app's `core/markdown/objects.dart`), used
  in sealed chats and the composer and compared with every corpus case's
  entities of those types; the composer's rules are tested without a page
  or widgets in `e2e/objects.mjs` and `app/test/core/objects/`.
- **The test corpus** is `testdata/markdown/` at the top of the
  repository: several hundred cases, each a JSON file with a message's
  `text` and `entities` and the tree it must parse to. It includes the
  awkward cases: unclosed markers, nesting limits, tables over the
  size limit, entities next to formatting, positions counted in UTF-16
  units across emoji.
- **All three run it.** `go test` runs it against
  `internal/markdown`. The Flutter tests run it against
  `app/lib/core/markdown`. The browser tests run it against
  `internal/web/static/shared/md/`. A change to the format starts with
  a new case that fails in three places.
- **Highlighting** is shared the same way. The language list and
  aliases are one JSON file in `testdata/highlight/`. The web chat
  uses highlight.js itself; the apps use a Dart port of it. The token
  names both produce map to colours by one table (section 12). The
  corpus has a few cases per language that check the tokens come out
  the same.

## 19. Content security policy

Every page is served with a policy. This is the one for the web chat,
which needs the most:

    default-src 'none';
    script-src 'self';
    style-src 'self';
    img-src 'self' blob:;
    media-src 'self' blob:;
    font-src 'self';
    connect-src 'self' wss://<this server's host name>;
    worker-src 'self';
    manifest-src 'self';
    form-action 'self';
    frame-ancestors 'none';
    base-uri 'none'

| Part | Why |
|------|-----|
| `default-src 'none'` | Anything not listed is refused |
| `script-src 'self'` | Only the server's own script files. No inline script, no `eval` |
| `style-src 'self'` | No inline styles. Sizes and positions that must be computed are set through the style interface from script, which this allows |
| `img-src`, `media-src` with `blob:` | Files come from the server. `blob:` is for sealed attachments, which are decrypted in the page, and for the camera preview |
| `connect-src` | The API and the WebSocket, same server only |
| `worker-src 'self'` | The call frame-encryption worker |
| `frame-ancestors 'none'` | The pages cannot be put in a frame |

- The other pages get the same policy without `blob:`, `media-src`
  and `worker-src`.
- Uploaded files are served with their own stricter policy,
  `default-src 'none'; sandbox` (`MESSAGE-FORMAT.md` section 5).
- Also on every page: `X-Content-Type-Options: nosniff`,
  `Referrer-Policy: no-referrer`, `Cross-Origin-Opener-Policy:
  same-origin`, a `Permissions-Policy` that allows the camera,
  microphone and screen capture on `/chat/` only, and
  `Strict-Transport-Security` when the server terminates TLS itself.
- A media connection for a call is not an HTTP request and is not
  covered by `connect-src`. Calls hosted by another server
  (`CALLS.md` section 16) therefore work under this policy.

## 20. Installability

The web chat can be installed to a home screen or as a desktop
shortcut, where the browser offers that.

- A web app manifest at `/chat/manifest.webmanifest`: name "Churr"
  with the community's name, start address `/chat/`, standalone
  display, navy background and theme colour, icons at 192 and 512
  pixels and a maskable one.
- An Apple touch icon and the meta tags iPhones use for "Add to Home
  Screen".
- **No service worker.** Installing gives a window without browser
  chrome and nothing more: no offline use, no background activity, no
  push. This is on purpose. A service worker would keep running old
  code after the server was upgraded, and for a page that handles
  sealed-chat keys the code that runs should always be the code the
  server serves now.

## 21. File layout

    internal/web/
      web.go                     embeds static/, routes, headers, caching
      static/
        shared/
          tokens.css             colour, type and spacing tokens
          base.css               elements, buttons, fields, dialogs
          fonts/                 archivo.woff2, martian-mono.woff2, licences
          icons.svg              one sprite
          api.js                 fetch wrapper, errors, paging
          live.js                WebSocket, heartbeat, catch-up
          dom.js                 element helper, focus handling
          passkey.js             WebAuthn calls, PRF
          accesskey.js           the access key grammar (AUTH.md section 5.6)
          accesskeyui.js         a new key shown once; the key field
          fresh.js               the fresh check dialog
          md/                    parse.js, render.js
          highlight/             vendored highlight.js core and grammars; hl.js
          message/               message.js, files.js, cards.js, forms.js,
                                 reactions.js, checklist.js
          emoji/                 emoji.json, picker.js
          sealed/                the sealed-chat scheme on Web Crypto: seal.js,
                                 devices.js, epochs.js, messages.js, files.js,
                                 backup.js, codes.js, bip39.js
        login/                   index.html, login.js
        setup/                   index.html, setup.js
        join/                    index.html, join.js
        oauth/                   authorize.html, authorize.js
        unlock/                  index.html, unlock.js
        account/                 index.html, account.js, and one module per section
        admin/                   index.html, admin.js, admin.css,
                                 sections/ (one module per row of section 16)
        chat/
          index.html
          manifest.webmanifest
          chat.css
          app.js                 start-up, routing, layout
          store.js               in-memory state fed by live.js
          list.js                channel list
          conversation.js        threaded and flat views, markers
          thread.js              thread pane
          composer.js            text, mentions, emoji, commands, attachments
          search.js  threads.js  profile.js  modes.js  notify.js
          sealed/                keystore.js (IndexedDB), session.js, backup.js,
                                 verify.js
          call/                  signal.js, room.js, media.js, layout.js,
                                 frames-worker.js
        icons/                   favicon, 192, 512, maskable, apple-touch

There is no `node_modules`, no `package.json` and no `dist/` anywhere
under `internal/web`.

## 22. The web chat

A basic client for when the app is not to hand. It works only while
its page is open.

| It does | |
|---------|--|
| Channels | The sidebar as in the apps: the person's own arrangement kept by the server (9.1), with drag and drop, Move…, the heading menu, Alt+Up and Alt+Down, and on a touch screen a long press for reordering mode; browse and join; join and leave open groups; new chat. `show` is honoured exactly as in the apps (9.2), with a "Show hidden" toggle |
| Reading and posting | Text in Churr Markdown, files, cards and forms drawn as the format says; answering forms and polls; ticking checklists |
| Threads | Both views, the marker, the thread pane, follow and ignore, the Threads view |
| Messages | React, reply with a quote, edit, delete, history, pin, report |
| Composer | Mentions, emoji, commands and presets typed as commands, attachments, drafts kept across reloads |
| Search | With the same filters |
| Personal state | Set what notifies and whether it shows, per conversation; do not disturb on and off; set a status |
| Chat requests | The Requests list with its four answers, the bar of a chat that waits, was declined or was closed, closing and `/close`, as in 9.28 |
| Calls | Join, start, camera, microphone, screen sharing on desktop browsers (`CALLS.md` section 13) |
| Sealed chats | Read and write, with keys kept in the browser until sign-out (`DESIGN.md` section 14) |
| Notifications | Browser notifications while the page is open (`PUSH.md` section 13), and a notifications list of what arrived while the page was open |
| The longer tail | Bookmarks; scheduled messages and reminders, with saved replies; the form builder; status presets; thread to channel; conversation settings for managers (name, topic, section, parent, default level, welcome text, retention, slow mode, archive, make private, lock, check-ins); creating a sub-channel and moving a channel under a parent; the moderators and listing of a channel on a hub |
| Themes | The theme editor and the server's theme library (`STYLE.md`, "Themes") |
| The lock | `/lock`, the account menu, Mod+Shift+L and the automatic lock; the lock screen at `/locked/`; unlocking with a passkey or the access key (9.27) |

| It does not | |
|-------------|--|
| Anything when closed | No notifications, no rings, no badge. Closing the page is like quitting the app. It catches up when opened again |
| Several servers | It is one server's page. It therefore has the rail layout only, with no rail to draw: no merged tree, and none of the cross-server features of section 8 |
| A notifications list across servers | Its list covers one server, and only what arrived while the page was open. The list across servers is in the apps |
| A local copy of messages | It loads what is on screen and keeps it in memory. Reloading the page fetches again. Only sealed-chat keys and state, and unsent drafts, are stored, in the browser's storage for this server's address |
| Offline use | No service worker |
| Screen sharing on phones | Browsers there cannot |
| Quiet hours and availability editing | On the account page |
| Protect sealed keys from the server | The browser runs whatever code the server sends each time. A dishonest or broken-into server could send code that leaks the keys, on any page load. The apps do their encryption in their own code, with one exception they share with the browser: unlocking with a passkey always passes the key through a page the server supplies, because a passkey only works for the server's own web address. The web chat says this where a sealed chat is opened, and browser devices are labelled in device lists |
| Keep decrypted sealed messages | It decrypts what it shows each time it loads |

**Where the web chat keeps its default for `show`.** The web chat has
no device store of its own beyond the browser. The default used when
a conversation's `show` is not set, and the "Show hidden" toggle, are
kept in the browser's local storage for the server's address, under
the account's identifier. They are set in the web chat's settings
panel, start at `always` and off, and are removed on sign-out.
Nothing else about the sidebar is stored there. Each conversation's
own `mode` and `show` are on the server and are the same in the web
chat and the apps.

**Drafts.** The text and quoted reply of an unsent message are kept
per conversation and per thread in the browser's local storage for the
server's address, under the account's identifier, so a reload brings
them back. Sending one, or emptying the box, removes it; one older than
30 days is dropped, and at most 100 are kept. Sealed chats keep no
draft there, as the apps keep none in their unencrypted cache. Locking
and signing out remove every draft, so a locked browser holds nothing
of the account that can be read. Files chosen are not kept.

Signing out wipes the sealed keys and the drafts from the browser.
Signing in again with a passkey that can unlock restores the keys.

---

## 23. Decisions recorded here

- State is the local database plus plain `ChangeNotifier` controllers.
  No state-management package.
- Sealed-chat encryption is written three times (Dart, JavaScript,
  Swift for decrypting only) against one set of test vectors from a
  Go reference. There is no shared native library.
- Sealed-chat state lives in one encrypted store in the app. The
  iPhone notification extension sees only per-epoch content keys and
  sender device keys, through a shared keychain group.
- Make private and Lock each ask the person to type a word to
  confirm, because neither can be undone.
- The repository as checked in builds the Android app with no Google
  library. A tool adds `firebase_messaging` for the Play build.
- QR scanning uses a package with no Google dependency, as Codemail
  does.
- Phone apps keep every server connected while in front in the
  merged layout, and only the selected one in the rail layout.
- The rail is the layout out of the box.
- A sub-channel whose parent has no row is drawn at the parent's
  level as `parent › name`. No row ever stands in for a parent.
- A collapsed parent shows a second count for its sub-channels.
- The detail line of 9.26 is the one way same-named channels are told
  apart, in the same order of parts everywhere.
- Each person's arrangement of a server's sidebar is kept by that
  server (ruling R83), not on the device, so it is the same in every
  app and the web chat. Admins' sections are only the default
  grouping; nobody orders channels for anyone else.
- App preferences are copied to another device by a set-up QR code
  shown by an app. It carries no tokens or keys.
- Muting a whole server is do not disturb on that server alone, so
  people there see the mark.
- The notifications list is built on the device from the `notify`
  mark on events. No server keeps such a list.
- The web chat keeps its default for `show` in the browser's local
  storage.
- The web pages follow the system's light or dark, or the browser's
  own choice (`STYLE.md`, "Themes and settings"). There is no accent.
- The web chat has no service worker and keeps no local copy of
  messages.
- The web chat leaves out the screens listed in section 22.
- The Markdown test corpus lives in `testdata/markdown/` and is run by
  all three implementations.
- Web files are served with `ETag` and `no-cache` instead of hashed
  names.

## 24. Needs from other documents

- **API:** the catch-up operation accepts a range, for filling a gap.
  The "server" operation gives the lowest supported app version, the
  upload limit, which features are on and the accepted push kinds. The
  small lists (groups, emoji, sections, presets, saved replies,
  commands) can each be fetched whole.
- **Events:** the client sends its `synced_seq` per conversation after
  connecting; the server answers per conversation with replayed events
  or a "too far behind" notice.
- **Auth:** the OAuth redirect accepts a loopback address on desktop
  and the apps' link scheme on phones; the pairing-code grant.
- **Sealed:** nothing more. Screens 9.19 to 9.21 follow
  `docs/SEALED.md` sections 13 and 15.
- **API:** an upload accepts one poster image for a video.

## 25. To verify before building

Packages named in section 3 whose current state should be checked:

- `sqlite3_flutter_libs`: whether current `sqlite3` releases still
  need it or bundle SQLite themselves.
- `flutter_webrtc`: frame encryption on all five targets, screen
  capture on Linux under Wayland and through the iPhone broadcast
  extension. The exact format of its frame cryptor (`CALLS.md`
  section 20).
- `firebase_messaging`: that it can be added and removed cleanly by a
  build tool, so the F-Droid build has no trace of it.
- `unifiedpush`: the current connector version and what it requires of
  the server.
- `launch_at_startup`, `desktop_drop`, `media_kit`: maintained, and
  working on all the desktop targets.
- `qr_code_scanner_plus`: maintained, and free of Google libraries.
- `re_highlight`: that it is a port of the same major version of
  highlight.js as the copy vendored for the web, and covers every
  language in `MESSAGE-FORMAT.md` section 2.3 (notably `zig`,
  `protobuf`, `graphql` and `elixir`).
- `cryptography` (2.9.0) against its fork `cryptography_plus`: which
  is maintained, and that it has all five primitives on all five
  targets at usable speed.
- `sqlcipher_flutter_libs`: maintained, and working with `drift` on
  all five targets.
- That Web Crypto in current Firefox, Chrome and Safari has X25519
  and Ed25519. If one lacks either, that page needs a small vendored
  implementation.
- That flat square styling can be applied to every Material widget the
  apps use without replacing them.

# Churr apps

One Flutter codebase for Android, iPhone, macOS, Windows and Linux.
`docs/CLIENTS.md` is the specification; this file says how to run the
app, how it is put together, and how to add to it.

> **The bundle identifier `example.notfinal.churr` is a placeholder.**
> The owner has not chosen the apps' identifier (`<APP-ID-NOT-CHOSEN>`
> in `docs/CLIENTS.md`). When it is chosen, replace it in `macos/`,
> `ios/`, `android/`, `linux/` and `windows/` together.

## Run it against a development server

From the repository root:

    go run ./tools/devseed -data /tmp/churr-dev -o /tmp/churr-dev/seed.json
    go run ./cmd/churr serve -dev -data /tmp/churr-dev        # http://localhost:8472

`devseed` fills a fresh data directory with an owner (`kai`), four
members, a bot, sections, channels with sub-channels, chats, a message
with every Markdown construct, threads, reactions, a poll, a checklist,
files and cards, and prints app tokens (for `kai` and `sam` unless
`-tokens` says otherwise). Then, from `app/`:

    flutter run -d macos --dart-define=CHURR_DEV_SEED=$(base64 < /tmp/churr-dev/seed.json | tr -d '\n')

A debug build given `CHURR_DEV_SEED` signs in as the first account in
the seed. Without it, the first screen is "Add a server", which in debug
builds also has a form taking an address and a devseed token. Both are
development only and are compiled out of release builds. The real way
in is "Sign in with your browser": to try it, register an account of
your own at `http://localhost:8472` with the `invite_code` in the seed.

    flutter analyze
    flutter test                 # unit and widget tests
    tool/integration.sh          # builds the server, seeds it, runs the app on macOS

`tool/integration.sh` brings the app's window to the front while it
runs (macOS sends no frames to a window that is not in front) and puts
back the app that was there. `SHOTS=dir` copies out its screenshots.

Android, Windows and Linux folders exist but have not been built: the
machine this was written on has no Android SDK and is not Windows or
Linux.

## Architecture

    server ── events ──▶ sync ──▶ local database ──▶ watched queries ──▶ screens
                                        ▲
    screens ── actions ──▶ controllers ─┘── API requests ──▶ server

The local database is the single source of truth. Screens never read
the network: they watch database queries. Sync writes the database.

| Where | What |
|-------|------|
| `lib/core/` | Plain Dart, no Flutter imports, so background entry points can use it |
| `core/themes/` | `palette.dart` the theme arithmetic of docs/STYLE.md "Themes" (a port of `internal/themes/palette`, held to its table cases); `theme.dart` the built-in themes, `themeTokens` (every theme, built-in or custom, becomes the app's tokens here), `.churrtheme` and theme links |
| `core/models/` | The API's objects, hand-written. Each keeps the JSON it came from, so unknown fields survive storage |
| `core/api/` | `operations.dart` lists every operation called (the only place a path is written; a test holds it to `docs/openapi.yaml`). `client.dart` does tokens, refresh with rotation, the error shape, rate limits, paging. One extension file per API area |
| `core/oauth.dart` | Authorization code with PKCE: the protocol. `platform/browser_sign_in.dart` opens the browser |
| `core/live.dart` | The WebSocket: hello, resume, ready, heartbeats, reconnect with backoff, close codes |
| `core/sync.dart` | Applies events by sequence number, fills gaps, reloads, fetches account state after `ready`, and runs the outbox |
| `core/db/` | `cache.db` per server and `app.db` for the app: SQLite through drift, with hand-written SQL (no generated code) |
| `core/markdown/` | The Churr Markdown parser, a port of the server's, held to `testdata/markdown` |
| `lib/state/` | `AppState` owns the app preferences and one `ServerState` per signed-in server; a `ServerState` owns that server's database, client, connection and sync. Screen controllers are plain `ChangeNotifier`s. `sidebar_model.dart` holds the rules for which rows the sidebar has |
| `lib/platform/` | The key store, folders on disk, the browser |
| `lib/ui/` | `theme/` (the style guide in code: `tokens.dart` the colour tokens as plain data with both themes, the high-distinction palette and "increase contrast"; `theme.dart` the Material theme; `shapes.dart` the notch, hard offset block, dashed outline, crooked tags, price-tag count, dot grid; `churr_icons.dart` the icon set; `motion.dart`), `layout/` (the adaptive shell, rail, sidebar in its three layouts), `screens/`, `message/` (Markdown, files, cards, reactions), `widgets/` (`controls.dart`: switch, presence, sealed shield, chevrons, markers, glitch) |

Several servers: each has its own folder (`servers/<local id>/`), tokens
(in the platform key store under that id), database and
connection. `AppState.addServer` and `removeServer` add and remove them at
runtime; removing deletes the folder and the tokens.

## How to add a screen

Copy the conversation screen: `lib/state/conversation_controller.dart`
and `lib/ui/screens/conversation/conversation_screen.dart`.

1. **Operations.** If the screen calls an operation the app does not
   call yet, add it to `core/api/operations.dart` (the contract test
   checks it) and a typed method to the extension file of its area.
2. **Storage.** If it shows something not cached yet, add a table to
   `core/db/cache_db.dart` as a new schema version, with its queries and
   a `watch…` for each query a screen draws from. Make `core/sync.dart`
   write it: in `_applyOne` for events with a `seq`, `_applyUnstored`
   for account events, `refreshAccountState` for what is fetched whole.
3. **Controller.** A `ChangeNotifier` in `lib/state/` taking the
   `ServerState`. In `start()`, listen to the watched queries and keep
   their latest answers in fields. Hold only what is not in the
   database (text being typed, scroll position). For a write: change
   the database first, then call the API; let the server's event
   confirm or correct it.
4. **Widget.** A `StatefulWidget` in `lib/ui/screens/<name>/` that
   makes the controller in `initState`, disposes it in `dispose`, and
   builds inside `ListenableBuilder`. Follow `docs/style-guide.html`
   (ruling R60): colours from `ChurrColors.of(context)` and nothing
   else, icons from `ChurrIcons` drawn by `Glyph` (never Material's
   `Icons`), shapes from `theme/shapes.dart`. No rounded corner, no
   blur, no easing; areas told apart by fill steps, not lines.
   `test/ui/contrast_test.dart` holds every colour pairing to 4.5:1
   (text) and 3:1 (edges and icons); add a pairing there when a screen
   puts text on a new ground.
   Get the app and server with `AppScope.of(context)` and the
   `ServerState` you were given.
5. **Placement.** Panes go in `lib/ui/layout/shell.dart`, by width
   class. Dialog-or-page screens follow `_settings` there.
6. **Tests.** A unit test for what sync writes (see
   `test/core/sync_test.dart`) and a widget test at phone and desktop
   sizes (see `test/ui/shell_test.dart`, which runs the app on
   in-memory databases with no network).

## Wording (lib/l10n)

All wording goes through Flutter's localisation. English is the one
file: `lib/l10n/app_en.arb`. `l10n.yaml` sets it up and `generate: true`
in `pubspec.yaml` makes `flutter pub get` and every build turn it into
`lib/l10n/app_localizations*.dart` (class `L10n`). The generated files
are checked in so `flutter analyze` works on a fresh tree; run
`flutter gen-l10n` after editing the `.arb`.

To move a screen's wording in:

1. Add each string to `app_en.arb` under a lowerCamel key named for
   what it says, not where it is (`markAllRead`, not
   `notificationsButton3`). Look for an existing key first (`cancel`,
   `save`, `close`, `retry`, `didNotWork`).
2. A string with values uses ICU placeholders and an `@key` entry:

       "untilTime": "until {time}",
       "@untilTime": {"placeholders": {"time": {"type": "String"}}},
       "unreadReplies": "{count, plural, =1{1 unread reply} other{{count} unread replies}}",
       "@unreadReplies": {"placeholders": {"count": {"type": "int"}}}

   Never build a sentence by joining translated pieces; give the whole
   sentence one key with placeholders.
3. Run `flutter gen-l10n`, then in the widget
   `final t = L10n.of(context);` and `Text(t.markAllRead)`. Code with no
   `BuildContext` (controllers, `lib/core`) returns values or enums and
   the widget picks the words.
4. Times and dates go through `intl`'s `DateFormat` with
   `Localizations.localeOf(context)`, never hand-built strings.
5. Widget tests need `localizationsDelegates: L10n.localizationsDelegates`
   and `supportedLocales: L10n.supportedLocales` on their `MaterialApp`
   (`test/account_support.dart`, `harness`).

The account area's screens (`lib/ui/screens/account`, `add_server`,
`settings`, `lib/ui/layout/account_entries.dart`) are moved; the rest
of the foundation's and other areas' inline English is still to move.

## Packages, and why each is here

| Package | Why |
|---------|-----|
| `http` | The API client |
| `protobuf`, `fixnum` | Protocol Buffers for the API and the live connection (docs/WIRE-PROTOBUF.md): the runtime of the generated classes in `lib/core/wire/gen`, and the 64-bit integers it uses |
| `web_socket_channel` | The live connection; its `dart:io` channel lets the token travel in the upgrade's `Authorization` header |
| `drift` | SQLite: opening, migrating, transactions and table-update notifications. Used without its code generator |
| `sqlite3` | The SQLite library itself. Version 3 bundles SQLite through build hooks, so `sqlite3_flutter_libs` (named in CLIENTS.md, now end-of-life) is not needed |
| `sqlite3` with `source: sqlite3mc` | `pubspec.yaml`'s `hooks` makes package:sqlite3 bundle SQLite3MultipleCiphers (checked on pub.dev 2026-11-14: sqlite3 3.9.1, maintained; `sqlcipher_flutter_libs` is 0.7.0+eol and does nothing with sqlite3 3.x). It reads and writes plain files exactly as SQLite does, so `cache.db` and `app.db` are unchanged, and encrypts `sealed.db` (the sealed store, `lib/core/sealed_client/sqlite_store.dart`) on all five targets with a key from secure storage. Its default cipher (ChaCha20-Poly1305) protects the file at rest; it is not part of the sealed scheme |
| `flutter_secure_storage` | Tokens in the Keychain, Keystore, credential store or libsecret. Every secret goes through `platformSecretStore()` (`lib/platform/secret_storage.dart`): **debug builds on macOS, Windows and Linux keep secrets unprotected in `dev-secrets.json` in the support directory** instead, because a re-signed debug build raises a keychain prompt at every launch. That store is guarded by `kDebugMode` and cannot be chosen in release or profile builds or on phones; App settings shows a line while it is in use. Keychain tokens are not moved: sign in again in a debug build |
| `flutter_web_auth_2` | The system sign-in sheet on phones, returning to `churr://oauth` |
| `url_launcher` | Opens the browser for desktop sign-in, and links, which always open outside the app |
| `crypto` | SHA-256 for the PKCE challenge |
| `re_highlight` | highlight.js grammars in Dart, for code blocks. It imports Flutter, so the grammar table is in `lib/ui/message/highlight.dart` and only the language list is in `lib/core/highlight/` |
| `path_provider`, `path` | The app's support directory |
| `fake_async` (dev) | Timers in the live connection's tests |
| `flutter_localizations`, `intl` | Wording in `lib/l10n`, dates and numbers by locale |
| `audioplayers` | The start-up sound and the ring. Maintained, and plays bundled assets on all five targets with nothing more (AVFoundation, MediaPlayer, Media Foundation, GStreamer); `just_audio` needs a second package for Windows and Linux. The sounds are WAV, which each of those decodes |
| `qr_flutter` | "Set up another device" shows its code |
| `mobile_scanner` | Scanning pairing and set-up codes (Android, iOS, macOS; elsewhere the code's text is pasted) |
| `file_selector` | Choosing a new avatar, on all five targets |
| `timezone`, `flutter_timezone` | Someone's local time on their profile; keeping the person's own time zone current from the device |
| `media_kit`, `media_kit_video`, `media_kit_libs_video` | Inline video and audio in messages, and the poster frame the app takes of a video before uploading it (`Player.screenshot`). Chosen because it plays MP4 (H.264/AAC), WebM, MP3, M4A, Ogg and WAV on all five targets through libmpv; checked on pub.dev 2026-11-14 (1.2.6, not discontinued). The official `video_player` has no Windows or Linux. `MediaKit.ensureInitialized` is called on first use (`lib/platform/media_files.dart`) |
| `file_picker` | Attaching files from the composer, and saving downloads, on all five targets |
| `desktop_drop` | Dropping files on a conversation to attach them |
| `pasteboard` | Pasting an image from the clipboard as an attachment |
| `flutter_local_notifications` | The system's notifications on all five targets (23.0.2, 2026-11-13). The one maintained package covering Android, iOS, macOS, Windows and Linux, the one CLIENTS.md names, and its Android part needs no Google library |
| `tray_manager`, `nativeapi` | The tray and menu-bar icon and its menu (0.8.3; built on `nativeapi`, one C++ core for the three desktops). `nativeapi`'s `LaunchAtLogin` is start at login: `launch_at_startup` (CLIENTS.md †) has had no release since 2025-04 and cannot resolve beside `flutter_secure_storage` 11 (`win32` 5 against 6). 0.9 needs Flutter 3.50 |
| `window_manager` | Hiding the window to the tray, showing it, focus (0.5.2: `unifiedpush_linux` holds it below 0.6) |
| `unifiedpush` | The UnifiedPush connector (6.2.0, unifiedpush.org), Android only in use. Brings `webcrypto` for Web Push and the JVM `tink`, excluded in `android/app/build.gradle.kts` in favour of `tink-android` |
| `flutter_foreground_task` | The Android stay-connected service (11.0.3), as CLIENTS.md names |
| `flutter_webrtc` | Calls (`lib/platform/calls/webrtc_media.dart`): peer connections, simulcast, screen capture and the frame cryptor of a sealed chat's call. Checked on pub.dev 2026-11-14: 1.6.3 (2026-10-26), not discontinued, Android, iOS, macOS, Windows and Linux; it ships WebRTC-SDK 150.7871.04 on Apple platforms. Needs macOS 10.15 and Android minSdk 21, both below the app's own |

The other packages of CLIENTS.md section 3 are added by whoever builds
the feature that needs them. `shared_preferences` is not used: app
preferences live in `app.db`, as section 8.2 says.

## Decisions made where the documents are silent

- **drift without generated code.** Tables are SQL strings and rows are
  mapped by hand, so the app builds with no code generation step.
  Objects are stored as the API's JSON beside the few columns queries
  need.
- **Reconnect delays** are those of CLIENTS.md section 7 and EVENTS.md
  section 6, which now agree (ruling R63): a wait drawn between half
  the retry interval (1, 2, 5, 10, then 30 seconds) and the interval
  plus the spread the server gave in `hello`. `LiveConnection.retryAt`
  is the moment chosen; `ReconnectCountdown` shows it counting down.
- **New conversations are taken in at their `last_seq`** with no
  messages; history is fetched when one is first opened.
- **`rate_limited` is retried** twice after the wait the server asks
  for (up to 30 seconds), for every method; `slow_mode` never.
- **A starred parent** is listed under Starred only; its sub-channels
  stay in its section as `parent › name`.
- **Sorting by name** lower-cases and compares code units; there is no
  locale collation without another package.
- **Parents the account is not in** are fetched with `getConversation`
  after each `ready` and kept apart from the account's conversations.
- **No accent setting.** The style guide gives its four colours fixed
  roles (lavender yours, cyan go, pink attention, yellow counts) and
  allows no other, so the per-server accent of CLIENTS.md section 12
  was removed. Server tiles and initials take one of the four fills
  from their id.
- **Where a message is shown** is by `order` (EVENTS.md section 8);
  the window of cached messages is still chosen by `seq`. `cache.db`
  version 4 adds `ord`; a conversation whose cached messages include one
  stored without `order` loses its cached run, fetched again on opening.
  A thread's marker sits by the `order` of its latest cached reply (the
  thread summary has no order), else just after its parent. The "New"
  line goes before the first unread message that is not `late`; the
  read position moves to the highest `seq` loaded.
- **Phone redirect** is `churr://oauth` (`AppScheme` in the server).
- **`zig`** is accepted as a language and drawn plain: `re_highlight`
  0.0.3 has no zig grammar. `html` uses the XML grammar and `toml` the
  INI grammar, as highlight.js does.
- **The fonts** are the two variable fonts from the house site,
  converted from WOFF2 to TrueType because Flutter cannot load WOFF2.
  They are Latin subsets; other scripts fall back to system fonts.
  Archivo carries weight (100–900) and width (62–125) axes: the narrow
  heavy cuts are `fontVariations: [FontVariation.width(…)]` with an
  ordinary `fontWeight` (the engine maps `fontWeight` onto the weight
  axis itself; an explicit `wght` variation would override every later
  weight, so none is set). See `archivo()` and `ChurrType`.

## The style guide: decisions where the guide is silent or cannot apply

- **Quiet rows** (notify nothing, muted) are drawn in the faint colour,
  not at the guide's 50% opacity, which would fall below 4.5:1.
- **Focus on light** is the dark amber (`yel-ink`): a yellow outline on
  the pale paper does not reach 3:1.
- **Increase contrast** follows the platform setting through
  `MaterialApp.highContrastTheme`: every line takes the edge colour and
  faint text is drawn as dim.
- **Material's own animations** (menus, dialogs, sheets, pages) are
  turned off at each call site and in the page transitions; the
  composer's block cursor blinks for ten seconds after the box empties,
  then stays lit.
- **Dropdown menus** (`DropdownButton`) have no shadow at all rather
  than the hard offset block: Flutter gives that menu no shape to draw
  it with.
- **Text fields** keep Material's label in the gap of the edge, not a
  label above the box as in the guide.

## Themes and hub screens: decisions where the documents are silent

- **One path for every theme.** `themeTokens` in `core/themes/theme.dart`
  turns a theme into the app's tokens: a built-in theme is the guide's
  hand-set table, any other is derived by `palette.dart`.
  `GuideSchemes`, `ChurrColors.forTheme` and `buildTheme(theme:)` all go
  through it. The palette setting is gone from App settings: the
  high-distinction pair are built-in themes (an earlier choice of that
  palette still picks them until another theme is chosen).
- **The same arithmetic as the server.** `test/core/themes/palette_cases.json`
  is a copy of the table cases of `internal/themes/palette/palette_test.go`,
  and `palette_corpus.json` the Go package's answers (derived colours,
  every measured ratio, the refusal) for 600 generated themes; both are run
  by `palette_test.dart`. Regenerate the corpus, if the rules change,
  from the repository root with
  `go test ./internal/themes/palette -run TestAppCorpus -update-corpus`.
- **Choosing** is three app preferences: `theme` (dark, light, follow the
  system), `theme_dark` and `theme_light` (which theme of each kind), and
  `theme_per_server` ("use each server's default": the open or selected
  server's default of that kind, else the chosen one). `theme_copies` and
  `server_default_themes` keep what is in use, so a start with no network
  has the right colours. Saved themes are fetched when the picker opens
  and after each `ready` (they have no events).
- **Try** applies a theme until "Keep it" or "Stop" in a strip across the
  top of the window; nothing is saved.
- **Fix** (the editor's) mixes the failing base colour towards black or
  white by the smallest whole per cent that makes every pairing it takes
  part in pass (black on a tie); faint and edge are fixed through dim,
  yel-ink through yel, a partner text colour through its fill.
- **The colour picker** is the app's own: a hex field and hue, saturation
  and lightness sliders (`toHsl`/`fromHsl` in `palette.dart`), no package.
- **Poll bars** under a custom theme lighten their tint until ink on them
  reaches 4.5:1 (the built-in themes keep 28%).
- **Lavender text.** The theme check holds lavender to 3:1 on ground and
  panel, but other people's mentions and two labels are lavender text on
  those grounds; with a custom theme they can fall below 4.5:1. Not
  changed here; `contrast_test.dart` holds custom themes to the check's
  own 3:1 for that pairing.
- **Hub channels.** A shared conversation's hub record is fetched with
  `getHubChannel` when the conversation or its settings open and kept in
  `ServerState.hubChannels`, replaced by `stream.channel_changed`;
  moderators' screens fetch it again for `invited` and `requests`. Whether
  the reader is a moderator is read from the record's list (account id and
  this server's name). "Delete for everyone" (a moderator) and "Delete
  here" (a local manager who is not one) both call `deleteMessage`; the
  server decides the reach. A moderator always deletes for everyone.
- **Sharing to a hub** is in a channel's settings for the server's owner;
  the hub directory is under Hubs in a server's settings. Linking servers
  stays in the web admin console (`/admin/linked-servers`), which both
  link to. Moves of a direct-link channel onto a hub have API methods
  (`core/api/hubs.dart`) but no screen yet.
- **Replaced sealed epochs.** `Chat.epochStarted` takes a
  `sealed.epoch_started` event; with `replaced: true`, `Chat.replaceEpoch`
  takes the other server's record and returns what this device had sent
  under its own, sealed again with the same references and new counters.
  The chat keeps the current epoch's sent contents for that
  (`sent_under` in its stored state). Nothing in the app posts sealed
  messages yet, so posting the returned messages is for the sealed-chat
  screens.

## The account area: decisions where the documents are silent

- **Notifications keep their message.** `cache.db` version 2 stores the
  message JSON, its thread and the reason with each item, so the list
  survives trimming. A reply clears by its thread's position when the
  account has one, else by the conversation's (FEATURES.md 2.1).
  Items from one conversation and thread within 30 minutes fold.
- **Filling in the list** after a reload or on a never-connected server
  writes an item for each message after the read position of each
  badged conversation: all of them under `any`, mentions and followed
  replies otherwise (the server's per-message `notify` is not
  replayable). Done once per server, after its first `ready`.
- **Trimming:** opened in the last 7 days keeps everything (stands in
  for "scrolled back through this week"); opened in the last 30 days
  keeps the newest 500; otherwise 50. Run at each start. The file cache
  is trimmed on disk by access time to its cap (1 GB, App settings).
- **Shared changes** (status, do not disturb, availability, appear away)
  wait per server and kind in that server's `settings` table when it
  cannot be reached; a later change of the same kind replaces the
  waiting one. Presets are applied by their fields, not by preset id,
  since ids differ per server; "shared" presets are remembered by their
  content in an app preference.
- **Muting a server** is recorded in an app preference (address and end)
  so it can be drawn dimmed and left out of shared do not disturb.
- **Default thread view, notify anyway and meeting pauses** use
  `updatePrefs`; CLIENTS.md 9.12 says `updateMe`, which takes none of
  them.
- **OAuth endpoints** come from the metadata document, are checked to be
  on the server's own origin, and are kept in the server's `settings`
  so refreshes after a restart use them.
- **Invite links** open the join page in the browser; the person then
  signs in with "Sign in with your browser", which only asks to approve
  because the browser holds the new session.
- **Set-up codes** are `churr://setup?p=&n=&d=`, gzipped JSON in
  base64url, 800 characters a code, cycled every 1.5 s. They carry the
  layout, default `show`, show hidden, theme, text size, Enter sends,
  language, shared opt-outs, the hybrid arrangement and server
  addresses.
- **Phones in the rail layout** connect only the selected (or open)
  server; the others' badges come from `getStatusSummary` every two
  minutes.
- **Open in place:** `AppState.openMessage` sets `focusMessageId` and
  `focusThreadId`; the conversation screen is to scroll there and call
  `clearFocus()`.
- Not built: the hybrid "Servers" menu at
  the top of the sidebar (server menus are on headings, tiles and in
  App settings), call and sealed-chat buttons on a profile.

## Sealed chats: how the app plays its part, and decisions

`lib/core/sealed` is the scheme; `lib/core/sealed_client` makes this app
a device: `session.dart` (registering, unlocking, epochs, rows, sending),
`sqlite_store.dart` (`servers/<id>/sealed.db`, encrypted at rest, key and
device private keys in the secret store), `api.dart`, `notify_keys.dart`
(the notification extension's items, in app/docs/NOTIFY-SHARED.md's
format, written on iPhone only). `lib/state/sealed_state.dart` feeds it
the sealed events the sync engine passes on and the sealed rows that
reach `cache.db`; the screens are `lib/ui/screens/sealed`.
`tool/sealed_e2e.sh` runs the end-to-end test against a fresh dev server.

- **The passkey way** opens the server's `/oauth/unlock` page; the page
  stores a `device` locked copy for this device and the app finds it
  with `getBackup` (as the server and `openUnlockPage` do it; SEALED.md
  13.3 describes an unlock request answered by the page instead).
- **Decrypted messages** are the library's chat state in `sealed.db`; the
  app adds a table of each row's server id, time, place and state
  (`ok`, `quiet`, `unchecked`, `nokey`, `dropped`). A row whose epoch
  secret has not arrived is kept there and read again when it does.
- **Entities** of received text are worked out again on the receiving
  device from the text (task item ids `t1`, `t2`… in order; mentions only
  of the two people), since the library's drawn message keeps no
  entities. Quotes are not drawn; a reply shows "Replying to …".
- **Drafts** in a sealed chat are not kept (`cache.db` is not encrypted).
- **Removing a device** always replaces the backup key and account key
  (CLIENTS.md 9.20); the server's `device.retired` and a list without
  this device are acted on (wipe, "Set up again").
- **QR pairing**: a `secret` parameter in the pairing code, if present, is
  the pairing secret of SEALED.md 13.7; the server's own pairing page
  puts none there yet.

## Calls

`docs/CALLS.md` is the specification. Where things are:

| Where | What |
|-------|------|
| `lib/core/calls/call_session.dart` | The signalling of CALLS.md 10 and 11 as a state machine, plain Dart: ticket, `call.join`, the `send` offer, answering `recv` offers, subscriptions (sent 200 ms after the last change, never twice the same), mute, screen share as an unpublish, `call.resume` on a new WebSocket, ICE restart (3 s disconnected or failed; three tries 5 s apart; then a rejoin with a new ticket), frame-key handling, and pacing under the server's 20 frames a second (15, candidates last) |
| `lib/core/calls/call_layout.dart` | Pure layout and subscription decisions: grid, speaker, duo (phone), screen; tile sizes in physical pixels; "+N"; the server's layer rule for checks |
| `lib/core/calls/call_media.dart`, `live_signal.dart`, `sdp.dart`, `ringing.dart`, `frame_crypto.dart`, `sealed_call_keys.dart` | The media interface the session drives, the live connection as a signal, SDP edits, ring rules, the cryptor's settings, frame keys from `Chat.callKeys` |
| `lib/platform/calls/webrtc_media.dart` | flutter_webrtc: the two peer connections, capture, simulcast `q`/`h`/`f` (phones `q`/`h`), demand, devices, frame cryptors |
| `lib/state/call_service.dart` | One per app: live calls per conversation, the call this device is in (one at a time), rings |
| `lib/ui/screens/call/` | The call screen, the compact bar and floating tile, the incoming prompt, the header button and the system message (registered through `ConversationHeaderSlots` and `MessageSlots.system`) |
| `tools/callpeer` (Go) | A second participant for the real check: a Pion client from `internal/calls/calltest` |

**For the notifications code:** `CallService.of(app).showIncomingCall(serverId:, conversationId:, callId:, fromAccountId:, until:)` opens the in-app incoming-call prompt (it rings unless the rules of CALLS.md 15 say not to). `serverId` is `ServerState.localId`.

**Platform set-up.** macOS: `com.apple.security.device.audio-input` in both entitlements files (the camera one was there), `NSMicrophoneUsageDescription`, and the camera string now mentions calls. Screen sharing needs the Screen Recording permission, which macOS asks for itself; no entitlement. iOS: `NSMicrophoneUsageDescription`, the camera string, and `UIBackgroundModes` `audio` so a call keeps going in the background. Android: `CAMERA`, `RECORD_AUDIO`, `MODIFY_AUDIO_SETTINGS`, `ACCESS_NETWORK_STATE`, `BLUETOOTH_CONNECT` (and `INTERNET`) in the manifest; minSdk is Flutter's default, above the plugin's 21.

### Decisions where the documents are silent

- **The camera goes out as VP8.** libwebrtc on macOS offers H.264 first, and the server answers with the first codec it shares, so the camera was H.264 in the first real check. `setCodecPreferences` fails on this library (error 7), so the app moves VP8 to the front of its offer's video codecs (`preferVp8`). Removing the other codecs makes the library refuse its own description.
- **Mid of each line** is read from the offer's SDP in transceiver order; the offer is set as the local description only when the server answers, so a refused offer (`cameras_full`, `screen_in_use`) leaves nothing to undo (as `calltest` does).
- **Camera off is a mute** (CALLS.md 10) and stops capture: the sender's track is replaced by none, so the light goes out. A camera never turned on is not published until it is.
- **Screen sharing** on macOS, Windows and Linux (the app's own picker of screens and windows). Not offered on phones: the iPhone needs a broadcast extension (without one, flutter_webrtc captures only Churr itself), and Android 10+ needs a media-projection foreground service, which this build does not have.
- **Speaker or earpiece** on phones (`Helper.setSpeakerphoneOn`); device choice (microphone, camera, speaker) on desktops. Switch camera on phones.
- **Shrinking the call**: the compact bar above the shell keeps it going; on tablet and desktop widths a floating tile (the loudest speaker or the shared screen) sits in a corner and can be dragged. No system picture-in-picture: flutter_webrtc offers none on macOS.
- **The pre-join step** asks only microphone and camera on or off; device choice is in the call (no preview or level meter before joining yet).
- **Who is being rung**, shown to the caller while alone: the other members of a sealed chat or ad-hoc channel, each marked "Ringing" or "Do not disturb" from their account's `dnd`. Their mode for the conversation is private to them, so a member who changed it from Any activity still shows "Ringing".
- **Missed call** is shown on an ended call's message, in a sealed chat or ad-hoc channel, to a member not in `participant_ids` who did not start it.
- **A ring** is held back on this device when it is in do not disturb or the server is muted from the app; a ring while in another call is a prompt without sound.
- **Mute for me** turns off that person's received audio track on this device.

### Frame encryption (sealed chats)

Verified on macOS on 2026-11-14 by experiment, and written down for
the web chat in `docs/CALL-FRAMES.md`. Summary:

- The plugin never takes an AES key. Its key provider derives one with
  PBKDF2 or HKDF from what it is given. The app gives each call sender
  key (32 bytes, SEALED.md 5.2) and chooses HKDF with the salt
  `churr call frames v1`; the plugin uses info = 128 zero bytes and
  16 bytes out, so frames are **AES-128-GCM**, tag 16. This is the one
  departure from SEALED.md 2.2 (AES-256-GCM): recorded in CALL-FRAMES.md
  as the named exception for call frames.
- Frame: readable header | ciphertext and tag | IV (12) | `0x0c` | key
  index. The header is the associated data: 1 byte for Opus, 3 for a
  VP8 delta frame, 10 for a VP8 key frame. IV = SSRC, RTP timestamp,
  timestamp minus a frame counter.
- Key index: the byte the sender sets with `setKeyIndex` (5 in the test
  ended every frame in `05`); key ring of 16 per participant.
- Ratcheting off (`ratchetWindowSize` 0, `failureTolerance` -1); frames
  before a key is set are dropped; a frame that does not decrypt is
  reported "decryption failed" (seen) and dropped (the library's
  behaviour, not separately observed).
- **Participants are named by device id.** The server says which
  device each track comes from (`device_id` in `call.offer`), every
  device of the epoch has its call sender key set under its own id,
  and a receiver's cryptor is made for the sending device
  (`frameParticipant` in `lib/core/calls/frame_crypto.dart`). With a
  server that names no device, the app decrypts only when the other
  account has one device in the epoch.
- **A new epoch during a call** re-keys it: `CallService` listens to
  the sealed chats' changes and `CallSession.refreshKeys` takes the
  new keys when the epoch number moved. New keys go in under the new
  index, senders switch index, and ten seconds later the other
  devices' old keys are overwritten with random bytes (the key
  provider cannot remove one). The decisions are `frameKeyChange`,
  unit-tested; that the plugin behaves so (the overwrite, and a new
  cryptor on a receiver whose line changed device) has not been run
  against the real library.
- Vectors: `test/core/calls/frame_vectors_test.dart` checks what the
  app derives and hands the plugin against
  `testdata/call-frames/vectors.json`, the file the web chat passes.
- H.264 under the cryptor was not settled; a sealed call's camera goes
  out as VP8 (the codec order above).

How it was found: `integration_test/call_real_test.dart` with
`CALL_CRYPTO=1` sets fixed keys (this device 0x11 × 32, the other
0x22 × 32, index 5) and joins a call beside `tools/callpeer -key 2222…
-other-key 1111… -key-index 5 -salt "churr call frames v1"`. callpeer
samples the payloads it receives and tries candidate derivations and
header lengths until AES-GCM opens them (it reported "hkdf readable=1,
400 of 400" for audio; for VP8, 213 delta frames with 3 readable bytes
and the key frames with 10), then encrypts its own microphone frames
the same way; the app's receiver reported "ok" for them.

### The real check

    go build -o /tmp/cc/churr ./cmd/churr && go build -o /tmp/cc/devseed ./tools/devseed
    go build -o /tmp/cc/callpeer ./tools/callpeer
    /tmp/cc/devseed -data /tmp/cc/data -http localhost:8573 -o /tmp/cc/seed.json
    /tmp/cc/churr serve -dev -data /tmp/cc/data -http localhost:8573 \
        -media 127.0.0.1:24977 -media-address 127.0.0.1 &
    /tmp/cc/callpeer -seed /tmp/cc/seed.json -as sam -conv chat_sam -for 30s &
    flutter test integration_test/call_real_test.dart -d macos \
        --dart-define=CHURR_SEED=$(base64 < /tmp/cc/seed.json | tr -d '\n') \
        --dart-define=CALL_CAMERA=1 [--dart-define=CALL_CRYPTO=1]

(other `-http` and `-media` ports than the defaults when another dev
server holds them). The app joins `chat_sam` as kai with microphone and
camera; callpeer joins as sam, publishes made-up audio and VP8 and
subscribes to the app's camera. Result on 2026-11-14: the call went
live; the app sent about 80 KB of audio and 450 KB of video and
received about 30 KB of audio and 200 KB of video in 20 seconds;
callpeer counted 855 audio and 468 video packets from the app; the
server reported kai speaking from the microphone's level. macOS did not
prompt for the microphone or camera on this machine (the test runner
already had both); on a machine where it does, the first run waits on
the prompt: a person must click Allow for "churr" (Microphone, then
Camera), or grant them in System Settings → Privacy & Security, and run
again.

Not exercised: iPhone hardware or the simulator, Android, Windows,
Linux, a real network (only the loopback address), TCP fallback, ICE
restart and resume against the real server (unit-tested against
recorded frames only), screen sharing on a real screen (the picker
needs the Screen Recording permission), and a sealed chat's call between
two real sealed devices (the keys path is `Chat.callKeys`, the format
was checked with fixed keys).

## Notifications, wake-ups and the tray: how it fits and what has run

Spec: docs/PUSH.md. Shared interfaces with sealed chats and calls:
`docs/NOTIFY-SHARED.md` (the keychain items the iPhone extension reads,
the incoming-call hook, who sends which `registerDevice` field).

| Where | What |
|-------|------|
| `core/notify/` | Plain Dart: the decision (`decision.dart`), what a notification says (`content.dart`), wake-ups and the fetch (`wake.dart`), registration (`registration.dart`), the tray's state (`tray_state.dart`), the items shared with the iPhone extension (`shared_items.dart`) |
| `state/notify_center.dart` | Shows notifications for events sync marks `notify` (`local.notify` on `SyncEngine.localEvents`), removes them when `cache.db`'s items clear (read here or elsewhere), sets the icon badge from the servers' totals, works out the tray, rings |
| `platform/notifications.dart` | `flutter_local_notifications`, the Android channels, the badge channel |
| `platform/desktop.dart` | Tray, menu, close to tray, start at login |
| `platform/push.dart`, `push_background.dart`, `background_service.dart` | Phone wake-ups: APNs token (`ios/Runner/AppDelegate.swift`, channel `churr/push`), FCM (`platform/fcm.dart`, a stub until `dart run tool/play_flavor.dart`), UnifiedPush, the stay-connected service |
| `ios/NotificationService/` | The notification service extension in Swift; a Swift package so `swift test` runs the sealed vectors on a Mac. `tool/ios_notification_extension.rb` adds the target to the Xcode project (idempotent) |
| `tool/tray_icons.py` | Draws `assets/tray/` and Android's small icon |

Decisions where the documents are silent:

- **Shown while in front, elsewhere.** A message marked `notify` pops up
  unless its conversation is on screen in a focused window (PUSH.md 12
  and 13), so it also shows while the app is in front on another
  conversation. Replayed events older than ten minutes do not pop up.
- **The server's list for the extension is a keychain item**, not a file
  in an app-group container: no app group, and no container path to
  pass from Dart.
- **The extension does not renew tokens.** With an expired access token
  it leaves "New message". (PUSH.md 8 has it take the app's renewal lock;
  the app has no such lock yet.)
- **The Android service and the app** share tokens through the key store
  but renew independently; two renewals at once can spend a refresh token
  twice and sign the server out. Not yet guarded.
- **Desktop activity** (pointer and keys in the window) is now reported
  (`LiveConnection.noteActivity` was never called before).
- **On macOS a login-item start opens the window**; `--hidden` works where
  the login item passes arguments (Windows, Linux).
- **Android launcher badges** come from the notifications; the Windows
  taskbar overlay and the Linux launcher count are not built (the tray
  icon carries the count).
- **Background refresh** on iPhone uses the old background-fetch call;
  Android has none apart from FCM, UnifiedPush or the service.

Never run: real APNs or FCM delivery, the relay, UnifiedPush with a
distributor, the Android app on any device or emulator (it builds), the
Windows and Linux builds, the extension on a real iPhone (it builds for
the simulator, and its logic passes `swift test`), keychain sharing
between app and extension (needs a signed build).

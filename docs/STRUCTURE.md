# Repository structure

The layout to build to: the tree, what each Go package is for and what
it exports, which packages may import which, the module's
dependencies, where tests live and what CI runs. `DESIGN.md` section 2
lists the packages in outline; this document is the detail.

This was written before the build and has been brought up to date
with what was built. Type and function names are the shape, not a
promise about every signature.

The module is `github.com/tikaydaes/churr`.

## 1. The tree

    churr/
      BRIEF.md
      DESIGN.md
      README.md
      LICENSE                       AGPL-3.0
      go.mod  go.sum
      Dockerfile                    builds churr, churr-hub, churr-relay (OPERATIONS.md 8)
      compose.yaml                  publishes on localhost; media port direct
      .env.example
      .dockerignore
      art/                          the mascot and sticker art

      cmd/
        churr/                      the server binary
          main.go                   picks the command, parses flags and environment
          flags.go  serve.go  backup.go  recovery.go  migratedb.go
          importslack.go  export.go
        churr-hub/
          main.go                   serve, invite, servers, remove, channels, appoint,
                                    dismiss, expel, trunk, cluster, rehome, backup, version
        churr-relay/
          main.go                   serve, version

      internal/
        model/                      shared types; imports nothing of ours
        config/                     flags and environment for all three programs
        version/                    version and commit, set at build time
        secret/                     key files, sealing, tokens, hashes
        ratelimit/                  token buckets
        memguard/                   the memory guard: how much memory there is, and when it is short
        markdown/                   Churr Markdown: parse, entities, plain text
        wirepb/                     the contract in Protocol Buffers: JSON to protobuf and back (WIRE-PROTOBUF.md)
        store/                      the storage interface
          sqlite/                   SQLite backend
          postgres/                 PostgreSQL backend
          migrations/               one generated file per dialect (sqlite/, postgres/),
                                    written from docs/schema.sql and checked in
          storetest/                the one suite both backends must pass
        blob/                       file contents on disk or in an object store
        perm/                       every permission rule
        notify/                     who is notified, badges, do not disturb
        live/                       WebSocket connections, fan-out, presence
        search/                     full-text queries
        push/                       devices, wake-ups, the relay client
        preview/                    link preview fetcher
        mod/                        timeouts, slow mode, word filter, reports, bans
        people/                     accounts, profiles, groups, status, invites, blocks
        chat/                       conversations, members, messages, threads
        forms/                      forms, polls, answers, tallies, actions
        sched/                      send later, reminders, recurring forms
        bots/                       bots, webhooks, payload conversion, commands
        calls/                      call rooms, signalling, media forwarding
          calltest/                 CallClient: a Pion participant for tests and tools
        sealed/                     what the server stores for sealed chats
          ref/                      Go reference of the sealed scheme; tests only
        link/                       server identity, peers, streams, the link protocol,
                                    shared-clock ordering, hub-owned channels, moves
          wire/                     frames, the protobuf codec and its schema table, compression,
                                    identity, clock and cluster roster
        auth/                       passkeys, access keys, recovery, OIDC, SAML, TOTP, OAuth, sessions
        importer/                   the Slack export reader
        export/                     the community export
        janitor/                    retention and expiry
        acme/                       certificates when no proxy is in front
        api/                        HTTP handlers for /api/v1 and the other paths
        web/                        embedded pages
          web.go
          static/                   plain HTML, CSS, JavaScript (CLIENTS.md 21)
        server/                     builds a running server out of the packages above
        hub/                        the hub: envelope log, ordering checks, the channels it
                                    owns and their directory, trunks and clusters
                                    (cluster.go, trunk.go, ship.go, replica.go)
        themes/                     colour themes: an account's own, the library, defaults
          palette/                  derived colours and the contrast rules (ruling R61)
        broadcast/                  the server broadcast: an admin's notice to every connection (R74)
        relay/                      the push relay: APNs, FCM, limits, counters
        testkit/                    helpers for tests: servers, clients, authenticators

      app/                          the Flutter apps (CLIENTS.md 2)
        lib/
        android/  ios/  macos/  windows/  linux/
        assets/  test/  integration_test/  tool/

      docs/
        CONVENTIONS.md  MESSAGE-FORMAT.md
        schema.sql  SCHEMA.md
        openapi.yaml  API.md  EVENTS.md
        PERMISSIONS.md  AUTH.md  BOTS.md  FEATURES.md
        LINK-PROTOCOL.md  PUSH.md  CALLS.md  SEALED.md
        CLIENTS.md  OPERATIONS.md  STRUCTURE.md
        STYLE.md  style-guide.html  ART.md  CATALOGUE.md

      testdata/
        markdown/                   the Markdown corpus all three parsers run
        highlight/                  the language list and per-language cases
        slack/                      small Slack exports for the importer
        webhooks/                   Slack, Discord and GitHub payloads with expected cards
        sealed/                     test vectors written by internal/sealed/ref
        art/                        the chance artwork sheets and their faces, from tools/art-gen

      examples/                     small bots that show how (DESIGN.md 19)
        README.md
        echo-bot/                   connects, answers a command
        poll-bot/                   posts a form, receives answers
        gif-bot/                    a command that calls an outside service
        webhook.sh                  posting to a webhook with curl
        calendar-integration/       sets availability with an integration token

      e2e/                          browser tests
        package.json  package-lock.json
        run.sh                      builds the server, starts it, runs the tests
        web.mjs                     sign-in, setup, account, admin
        chat.mjs                    the web chat
        calls.mjs                   two browsers in a call, with fake devices
        sealed.mjs                  two browsers in a sealed chat
        sealed-vectors.mjs          runs testdata/sealed against the page's JavaScript
        markdown.mjs                runs testdata/markdown in the page
        shots/                      screenshots from failures

      tools/
        loadtest/                   drives the targets in BRIEF.md against a server
        openapi-gen/                writes docs/openapi.yaml and internal/api/operations_gen.go
        pgtest.sh                   a throwaway PostgreSQL cluster for the tests
        sounds/                     writes the ring and start-up sounds
        devseed/                    fills a data directory with a small community to try by hand
        callpeer/                   a second call participant for trying the apps' calls
        art-gen/                    the chance artwork: prompts, generation over ssh, sprite sheets (docs/ART.md)

      .github/
        workflows/
          build.yml                 written, never run (section 6)

## 2. The Go packages

Every package is under `internal/`, so nothing outside this module can
import it. For each: what it is responsible for, and the main things
it exports.

### Foundations

**`model`** — the types everything else speaks in. No behaviour beyond
validation and JSON.

- `AccountID`, `ConversationID`, `MessageID`, `FileID` … (UUIDv7), and
  `NewID()`.
- `Account`, `Role`, `Group`, `Conversation`, `Member`, `Level`,
  `Mode`, `Message`, `Entity`, `File`, `Card`, `Form`, `Reaction`,
  `ThreadState`, `Call`, `Device`, `Session`, `Peer`, `Stream`.
- `Event` (type, time, conversation, seq, payload) and the event
  names.
- `Error` with the codes in `CONVENTIONS.md`, and helpers such as
  `ErrNotFound`, `ErrForbidden`.
- `Time` (milliseconds in the database, RFC 3339 in JSON).
- Name rules: `ValidUsername`, `ValidChannelName`, `ValidEmojiName`.

**`config`** — reads flags and the matching environment variables.

- `Serve`, `Hub`, `Relay` structs, one per program.
- `ParseServe(args []string, env func(string) string) (Serve, error)`
  and the same for the others.

**`version`** — `Version`, `Commit`, `String()`.

**`secret`** — the key files in `keys/` and everything done with them.

- `Open(dir string) (*Keys, error)`; makes the keys on first use.
- `(*Keys).Seal`, `(*Keys).Unseal` (AES-256-GCM, for sealed columns).
- `(*Keys).Sign`, `(*Keys).SignPublic`, `(*Keys).BoxPublic`,
  `(*Keys).OpenBox`.
- `(*Keys).MAC(label string, parts ...[]byte) []byte`, used for
  wake-up routes and collapse keys.
- `NewToken(prefix string) (token string, hash []byte)`,
  `HashToken(token string) []byte`.

**`ratelimit`** — `Limiter`, `New(rate, burst)`,
`(*Limiter).Allow(key string) (ok bool, retryAfter time.Duration)`.

**`markdown`** — the server's reader of Churr Markdown. It does not
render.

- `Parse(text string) Doc`.
- `Resolve(doc Doc, r Resolver) []model.Entity`, where `Resolver`
  looks up usernames, handles, channels and emoji.
- `Tasks(doc Doc) []Task`, the checklist items.
- `Plain(text string, entities []model.Entity) string`, for search
  text, fallbacks and notification bodies.
- Passes the corpus in `testdata/markdown`.

### Storage

**`store`** — the storage interface. The rest of the server depends on
this package and never on a backend.

- `Store`, with one method per group of tables: `Accounts()`,
  `Groups()`, `Conversations()`, `Members()`, `Messages()`, `Files()`,
  `Forms()`, `Events()`, `Sessions()`, `Devices()`, `Settings()`,
  `NotifyAnyway()`, `Calls()`, `Peers()`, `Streams()`, `StreamLog()`,
  `Sealed()`, `Audit()`, and so on.
- `Sealed()` covers every sealed-chat table: `device_lists` (every
  version), `sealed_epochs`, `backup_keys`, `key_backups`,
  `account_records` and `unlock_requests`. The store treats their
  contents as opaque bytes.
- `StreamLog()` is the log of envelopes this server numbered, for
  streams it sequences, kept 30 days. `Outbox()` and `StreamSeen()`
  sit beside it.
- `(Store).Tx(ctx, func(Tx) error) error`. Anything that must change
  together (a message, its event, its mentions, its search entry) is
  done inside one.
- `Open(ctx, dataDir, dbURL string) (Store, error)` picks the backend
  and runs migrations.
- `Copy(ctx, from, to Store) error`, for `migrate-db`.
- `Snapshot(ctx, s Store, path string) error`, for `backup`.

**`store/sqlite`**, **`store/postgres`** — the two backends. Each
exports `Open`. They share SQL where the dialects agree, and differ in
search, in JSON columns and in how a conversation's next sequence
number is taken.

**`store/migrations`** — numbered SQL files for each dialect, embedded,
and `manifest.txt`, their checked-in hashes. `Apply(ctx, db, dialect)`
checks the database's recorded migrations against the build, then
applies the pending ones; `Inspect` does the check alone.

**`store/storetest`** — `Run(t *testing.T, open func(t *testing.T)
store.Store)`. Every storage behaviour is tested here once and run
against both backends.

**`blob`** — file contents.

- `Blobs` interface: `Put(ctx, r io.Reader) (key string, size int64,
  sha256 []byte, err error)`, `Get(ctx, key) (io.ReadCloser, error)`,
  `Delete(ctx, key) error`.
- `Disk(dir string)`, `S3(cfg S3Config)`, and `Mixed(disk, s3 Blobs)`,
  which reads each key from wherever it lives and writes to the
  configured one.
- `Sniff(head []byte) (mediaType, kind string)`: type from content,
  never from the name.
- `Thumbnail(src io.Reader) (io.Reader, error)`: re-encoded, at most
  720 pixels, metadata gone.
- `Probe(src io.ReadSeeker) (width, height, durationSeconds int)`, for
  images and the audio and video containers `MESSAGE-FORMAT.md` lists.
- No video is decoded. A video's poster is an image the uploading
  client sent, stored under `files.poster_key` like a thumbnail.

### Rules

**`perm`** — the one place that answers "may this account do this
here". Handlers, search, downloads and the live fan-out all ask it.

- `New(s store.Store) *Checker`.
- `CanSee`, `CanRead`, `CanPost`, `CanReact`, `CanJoin`, `CanAdd`,
  `CanManage`, `CanJoinCall`, `CanStartChat`, `CanAdminister`, each
  `(ctx, actor model.Account, target) error`, returning `nil` or the
  `model.Error` to send.
- The locking rules live here: `CanAdd` and `CanJoin` refuse a locked
  channel with `locked`, group membership stops bringing people into
  one, and `CanMakePrivate` and `CanLock` say who may take those two
  one-way steps. A named private channel may be locked by its
  managers or any admin; an ad-hoc channel only from inside.
- `Recipients(ctx, conversation) ([]model.AccountID, error)`: who
  receives a conversation's live events.
- `Readable(ctx, actor) Scope`: a description of what the actor may
  read, which `search` and list queries join against.
- `docs/PERMISSIONS.md` is the table this package implements, and its
  tests are that table.

**`notify`** — what an event means for each person.

- `Decide(ctx, msg model.Message, to model.AccountID) Level`, where
  `Level` is `None`, `Badge` or `Notify` (`PUSH.md` sections 2 and 12).
- `Rings(ctx, call model.Call) ([]model.AccountID, error)`.
- `AnywayApplies(ctx, msg model.Message, to model.AccountID) bool`:
  whether a notify-anyway request reaches its target. The answer goes
  into `notify_anyway.delivered` and to `push`, and nowhere else.
- `InDND(acct model.Account, now time.Time) (bool, time.Time)`:
  manual, quiet hours, status, meeting.
- `Totals(ctx, acct model.AccountID) (Totals, error)`: the numbers the
  status endpoint returns, kept in memory and updated per event.
- `Badge(ctx, acct, conversation) (int, error)`.

**`mod`** — moderation checks made when someone posts, and the
moderator's actions.

- `Check(ctx, actor, conversation, text) error`: timeouts, slow mode,
  the word filter.
- `Timeout`, `LiftTimeout`, `SetSlowMode`, `Report`, `HandleReport`,
  `Ban`, `LiftBan`, `Filters`. A word-filter flag is a report with
  no reporter and source `filter`.

### Delivery

**`live`** — the WebSocket hub.

- `New(perm *perm.Checker, s store.Store) *Hub`.
- `(*Hub).Serve(w, r, session model.Session)`: one connection.
  Handles the heartbeat, catch-up by sequence number, and dropping a
  connection whose queue is full.
- `(*Hub).Publish(ctx, e model.Event)`: fan-out to the conversation's
  recipients, adding each one's `notify` level.
- `(*Hub).Send(account model.AccountID, v any)`, for events addressed
  to one account.
- `(*Hub).Handle(prefix string, h ClientHandler)`: lets another
  package receive client messages whose type starts with a prefix.
  `calls` registers `call.`.
- `(*Hub).Active(account) bool`, `(*Hub).Presence(account) Presence`.

**`search`** — `Search(ctx, actor model.Account, q Query) (Results,
error)`. Builds the query for whichever backend is in use, joined to
`perm.Readable`.

**`push`** — phone wake-ups.

- `(*Service).Register(ctx, session, DeviceInput) (model.Device, route
  string, error)`.
- `(*Service).Wake(ctx, account model.AccountID, ref Ref)`: makes the
  event identifier and collapse key, and posts to the relay or a
  UnifiedPush endpoint, with retries.
- `(*Service).Resolve(ctx, session, eventID string) (Ref, error)`, for
  the wake-up fetch.
- `RelayClient`, and the forgetting rules of `PUSH.md` section 6.

**`preview`** — `Fetch(ctx, url string) (model.Card, error)`. Refuses
private addresses, checks again after each redirect, caps time and
size, stores the image through `blob`. Exports the guarded dialer,
`SafeDialer`, which `push` uses for UnifiedPush endpoints.

### Features

**`people`** — accounts and everything attached to one.

- `(*Service).Create`, `Update`, `Remove`, `SetRole`.
- `SetStatus`, `SetAvailability`, `SetDND`, `SetQuietHours`,
  `SetAppearAway`, `Presets`.
- `Groups`: create, edit, members, open join and leave, provider
  mapping.
- `Invites`: create, claim, revoke. `Block`, `Unblock`.

**`chat`** — the conversation service. Every change to a conversation
goes through it, whoever asked: a handler, a bot, a scheduled job, the
importer or another server.

- `(*Service).Post(ctx, actor, PostInput) (model.Message, error)`: the
  one way a message is created. It checks `perm` and `mod`, resolves
  entities, stores the message with its event, mentions and search
  entry in one transaction, publishes to `live`, asks `notify` and
  `push` who to wake, and queues a link preview.
- `Edit`, `Delete`, `History`, `Purge`, `React`, `Unreact`, `Tick`,
  `Pin`, `Bookmark`, `MarkRead`, `Typing`.
- `CreateChannel`, `FindOrCreateChat`, `Rename`, `Archive`, `Join`,
  `Leave`, `AddMember`, `AddGroup`, `SetLevel`, `SetMode`.
- `MakePrivate` and `Lock`: the two one-way changes. `Lock` sets
  `conversations.locked_at`. Both publish `conversation.updated`.
  `Leave` deletes a private or ad-hoc channel with its content when
  its last member goes.
- `NotifyAnyway(ctx, actor, message, target) error`: writes the
  `notify_anyway` row, sets the public mark on the message, asks
  `notify` whether it applies and wakes the target if so. It returns
  the same answer either way.
- `FollowThread`, `IgnoreThread`, `MoveThreadToChannel`.
- `Hooks`: interfaces other packages implement to take part in a post
  without being imported here, such as `Outbound` (implemented by
  `link`, for shared conversations).

**`forms`** — `Attach`, `Answer`, `Press`, `Close`, `Tally`, `Answers`,
`OpenDialog`, and one function per action type (`tally`, `collect`,
`thread`, `channel`, `group`, `bot`).

**`sched`** — `(*Runner).Run(ctx)`, the loop that fires due rows;
`Schedule`, `Update`, `Cancel`, `List`; `Remind`.

**`bots`** — `CreateBot`, `Tokens`; `RequestWebhook`, `Approve`,
`Decline` (named channels only); `Intake(ctx, token, flavour, body)
error`, which converts a Churr, Slack, Discord or GitHub payload to a
message and checks GitHub's signature against the webhook's own
sealed signing secret; `SetCommands`, which replaces a bot's whole
list; `Dispatch`, which sends a `command.invoked` interaction; and
`Respond`, the one answer to any interaction (`docs/BOTS.md`).

**`calls`** — the media server (`CALLS.md`).

- `New(cfg MediaConfig, …) (*Server, error)`: opens the media port.
- `(*Server).Join(ctx, actor, conversation) (model.Call, ticket
  string, error)`.
- `(*Server).HandleClient`, registered with `live` for `call.`
  messages.
- `Room`, `Participant`, `Track`, the forwarder and the layer picker.
- `Passes`: an interface for getting and checking passes, and for
  relaying signalling, for calls hosted on other servers. `link`
  implements it with sealed direct frames.

**`sealed`** — what the server keeps for sealed chats, none of which
it can read (`docs/SEALED.md` section 16).

- `Start(ctx, a, b model.AccountID)`, `Post` (a sealed message with
  its `client_id`, key epoch and `quiet` flag).
- `DeviceLists`: publish a version, fetch versions. Every version is
  kept.
- `Epochs`: accept one epoch record per key epoch per chat,
  atomically, giving it the conversation's next `seq`; list epoch
  records; publish `sealed.epoch_started`. `key_epoch` is a plain
  counter from 1.
- `BackupKeys` (locked copies), `Backups` (records), `AccountRecords`,
  and replacing or resetting them in one step.
- `UnlockRequests`: post, list, answer; they expire after ten
  minutes.
- `Handover`: holds the sealed hand-over to an app with its
  authorization code. `auth` reaches it through an interface.

**`sealed/ref`** — a Go reference implementation of the sealed-chat
scheme in `docs/SEALED.md`, from the standard library's X25519,
Ed25519, HKDF-SHA-256, AES-256-GCM and SHA-256. The server never
links it: it is imported only from `_test.go` files.

- `Seal`, `Open`: the one seal-to-a-public-key construction.
- `DeviceList`, `EpochRecord`, `Message`, `File`, `Backup`,
  `LockedCopy`, `FrameKeys`, `ApprovalCode`, `ComparisonCode`: make
  and check each.
- `WriteVectors(dir string) error`: writes `testdata/sealed/`, from
  fixed seeds so the output is the same every time. Each vector has
  inputs, the exact bytes expected, and cases that must be refused.
- The server's own tests use it to play the part of a device.

**`link`** — sharing between servers (`docs/LINK-PROTOCOL.md`).

- `Identity`, `WellKnown`.
- `Peers`: invite, accept, dial, pause, remove.
- `Streams`: offer, accept, leave, new key epoch.
- `Envelope`, `Seal`, `Open`, `Verify`.
- `Direct`: direct frames between two servers, sealed to the receiver
  and signed by the sender. Offers, stream keys, call passes and call
  signalling travel this way.
- Shared-clock ordering (`flow.go`, `outbound.go`, `clock.go`): the
  offset from each conversation's time reference, the stamp on every
  event a server originates, and catch-up by per-origin counters. A
  server that owns a stream on a direct link keeps its log for the
  other server; nobody hands out positions.
- `hubchan.go`: channels a hub owns, their moderators and the
  directory. `move.go`: moving a direct-link channel onto a hub.
  `cluster.go`: the roster of hubs joined by trunks.
- `FetchFile`: gets a shared conversation's file from the server it
  came from and checks it against the hash in the signed event.
- `(*Service).Outbound`, which `chat` calls through its hook, and
  `(*Service).Apply`, which checks an incoming event against "what a
  server accepts" and hands it to `chat`.
- `wire/`: `Conn`, the WebSocket protocol itself, its frames and
  codec, identity, the clock exchange and the cluster roster, which
  `hub` also uses. Everything on a link is compressed Protocol
  Buffers (ruling R70). The schema is
  `proto/churr/link/v1/link.proto`, written with its number lock and
  with `wire/schema_gen.go` by `tools/openapi-gen` from `link.py`;
  nobody edits the three. The codec is the package's own and walks
  that table: `EncodeFrame`/`DecodeFrame` for frames and
  `Marshal`/`Unmarshal` for what is signed or kept, beside Go
  structs (`pbstruct.go`); `EncodeDirect`/`EncodeEvent` and their
  opposites for the plaintext of direct messages and envelopes,
  beside the loose objects `link` builds (`pbtree.go`), using
  `wirepb` for the objects the client schema already has.
  `compress.go` is the rule for which frames go deflated. The tests
  check the table against the schema as `protoc` read it
  (`wire/testdata/link.binpb`, rewritten by `tools/protogen.sh`).

**`auth`** — getting from a request to a session.

- `Passkeys` (register, sign in, PRF flag), `Recovery` (codes and
  links), `OIDC`, `SAML`, `TOTP`, `SecondFactor`.
- `OAuth`: authorization, token exchange, refresh, pairing codes.
- `Sessions`: create, look up by cookie or bearer token, end.
- `Authenticate(r *http.Request) (model.Session, error)`.

**`importer`** — `Slack(ctx, zipPath string, opts Options) (Report,
error)`.

**`export`** — `Write(ctx, w io.Writer, opts Options) error`.

**`janitor`** — `(*Janitor).Run(ctx)`: retention, expired sessions,
invites and links, orphaned uploads, old events, forgotten push
addresses, ended calls.

**`acme`** — `Manager(cfg) *tls.Config` and the port 80 handler.

### The edges

**`api`** — the HTTP surface. Handlers decode a request, call a
service and encode the answer. They hold no rules of their own.

- `New(deps Deps) http.Handler`.
- One file per area of `DESIGN.md` section 7, plus `/oauth/*`,
  `/hooks/*`, `/link/v1`, `/.well-known/churr` and `/healthz`.
- Middleware: authentication, origin check for cookie requests, rate
  limits and their headers, request size, error shape.

**`web`** — `Handler() http.Handler`. Serves the embedded files with
the headers in `CLIENTS.md` section 19. Contains no logic about chat.

**`server`** — `New(cfg config.Serve) (*Server, error)` builds every
service and connects them; `(*Server).Run(ctx) error` listens. The
commands in `cmd/churr` are thin wrappers over this package, and tests
start whole servers with it.

**`hub`** — the `churr-hub` program: `New(cfg config.Hub)`, an
envelope log in its own small SQLite file, the shared-clock checks on
what it forwards, the record of every channel it owns with its
moderators and the directory it serves, trunks to other hubs and the
cluster roster, the status page.

**`themes`** — colour themes (ruling R61): an account's own, the
server's library, the server's default dark and light themes, the
theme card for a theme link. `themes/palette` derives the colours a
theme does not store and enforces the contrast rules.

**`broadcast`** — the server broadcast (ruling R74): an admin's notice
to every connection, held in memory with its end, one live event, and
the one showing for the status summary.

**`relay`** — the `churr-relay` program: `New(cfg config.Relay)`, the
`/v1/wake` handler, the APNs and FCM clients, the limits and counters.

**`testkit`** — for tests only.

- `NewServer(t, opts) *TestServer`: a whole server on a temporary
  directory, on SQLite or PostgreSQL.
- `(*TestServer).Member(t, name)`, `.Guest`, `.Bot`, `.Admin`: makes
  an account through the real API with a virtual passkey
  authenticator and returns a client for it.
- `NewLinked(t, n int, withHub bool)`: n servers, linked directly or
  through a hub, in one process.
- `NoOutbound(t)`: fails the test if anything dials out.
- `FakeAPNs`, `FakeFCM`, `FakeIdP`, `FakeS3`.
- `CallClient`: a Pion client that joins a call.

## 3. Which packages may import which

The rule that matters most: **`store` and `perm` are at the bottom.**
`store` knows nothing about rules, and `perm` knows nothing about
HTTP, WebSockets or features. Everything that makes a decision about
access goes down to `perm`; nothing comes back up.

Packages are in layers. A package may import packages in lower
layers, and `model`. It may not import a package in its own layer or
above, except where the last column says so.

| Layer | Packages | May also import |
|-------|----------|-----------------|
| 0 | `model`, `version` | Nothing of ours |
| 1 | `config`, `secret`, `ratelimit`, `markdown`, `memguard` | |
| 2 | `store` and its backends | |
| 3 | `blob`, `perm` | `perm` imports `store` and `model` only |
| 4 | `notify`, `search`, `mod`, `preview` | |
| 5 | `live`, `push` | `push` may import `live` (to ask whether an account is active) |
| 6 | `people`, `chat` | `chat` may import `people` |
| 7 | `forms`, `sched`, `bots`, `calls`, `sealed`, `link`, `auth`, `importer`, `export`, `janitor`, `acme` | `sched` may import `forms` |
| 8 | `api`, `web` | |
| 9 | `server`, `hub`, `relay` | |
| — | `testkit` | Anything. Imported only from `_test.go` files |
| — | `sealed/ref` | Nothing of ours. Imported only from `_test.go` files and `testkit` |
| — | `cmd/*` | `config`, `version`, and `server`, `hub` or `relay` |

Further rules:

| Rule | Why |
|------|-----|
| Only `store/sqlite` and `store/postgres` import a database driver | So no query is written outside the storage layer |
| Only `store` and its backends contain SQL | The same |
| `perm` imports `model` and `store` and nothing else of ours | It must be small enough to read in one sitting |
| No package calls `store` to answer an access question itself | `perm` is the only authority |
| Packages in layer 7 do not import each other | A feature that needs another declares a small interface, and `server` connects them. `calls.Passes` is implemented by `link`; `chat.Outbound` by `link`; `bots` reaches `forms` through `chat` |
| `web` imports `version` and nothing else of ours | The pages talk to the API like any client |
| `hub` imports `link`, `secret`, `config`, `ratelimit`, `acme`, `version`, `model` | A hub must not be able to do what a server does. (`link/wire` imports `wirepb` for the client schema's messages inside events; a hub never reads an event, and links that code without calling it) |
| `relay` imports `config`, `ratelimit`, `acme`, `version` | A relay has no store, no model and no link code |
| Only `preview`, `push`, `blob`, `auth`, `link`, `acme`, `importer` and `relay` make outbound connections | These are the rows of the outside-connections table. A test checks that nothing else dials |
| Only `calls` imports Pion; only `auth` imports the WebAuthn, OIDC and SAML libraries; only `blob` imports the object-store client | Each large dependency has one owner |

A test in the repository root reads the import graph and fails when a
rule in this section is broken, so the table is enforced and not just
written down.

## 4. Module dependencies

The server, hub and relay are built with `CGO_ENABLED=0`. Every
dependency must be pure Go.

| Module | Used by | Why it is needed |
|--------|---------|------------------|
| `modernc.org/sqlite` | `store/sqlite`, `hub` | SQLite translated to Go: the default database, with FTS5 for search, and no cgo |
| `github.com/jackc/pgx/v5` | `store/postgres` | PostgreSQL driver and connection pool |
| `github.com/go-webauthn/webauthn` | `auth` | Passkey registration and sign-in |
| `github.com/coder/websocket` | `live`, `link`, `hub` | WebSocket server, and client for dialling peers. Works with `context` and `net/http`, no dependencies of its own |
| `github.com/pion/webrtc/v4` | `calls` | The WebRTC stack the media server is built on |
| `github.com/pion/interceptor`, `github.com/pion/rtp`, `github.com/pion/rtcp` | `calls` | Used directly for retransmission, feedback and bandwidth estimation, and for reading and rewriting packets |
| `github.com/coreos/go-oidc/v3` | `auth` | OIDC discovery and identity-token checking |
| `golang.org/x/oauth2` | `auth`, `relay` | The authorization-code exchange with PKCE for OIDC, and Google access tokens for FCM in the relay |
| `github.com/crewjam/saml` | `auth` | SAML 2.0 service provider. A maintained library, used in its narrowest configuration (`DESIGN.md` section 21) |
| `golang.org/x/crypto` | `acme`, `link`, `secret` | ACME certificates, XChaCha20-Poly1305 for envelopes, sealing a stream key to a server's public key |
| `golang.org/x/net` | `preview`, `config` | Reading the head of an HTML page for previews; host name handling |
| `golang.org/x/image` | `blob` | Decoding WebP and scaling images for thumbnails |
| `golang.org/x/text` | `model`, `markdown` | Normalising names and search text |
| `github.com/minio/minio-go/v7` | `blob` | S3-compatible object storage client, pure Go |
| `github.com/google/uuid` | `model` | UUIDv7 identifiers |
| `google.golang.org/protobuf` | `wirepb`, `link/wire` | The protobuf wire format (`protowire`); the tests use the rest of it to check the codec against the schema as `protoc` reads it |
| `github.com/skip2/go-qrcode` | `api` | The pairing QR code on the account page, as in Codemail |
| `github.com/descope/virtualwebauthn` | Tests only | A virtual passkey authenticator, as in Codemail |

Done with the standard library, on purpose:

| Need | With |
|------|------|
| HTTP routing | `net/http` and its method-and-path patterns. No router package |
| Logging | `log/slog` |
| JSON, zip, images (PNG, JPEG, GIF) | `encoding/json`, `archive/zip`, `image/*` |
| Ed25519, X25519, AES-GCM, HKDF, SHA-256 | `crypto/*` |
| Authenticator-app codes (TOTP) | `crypto/hmac`. It is forty lines |
| APNs: HTTP/2 and the signed token | `net/http`, `crypto/ecdsa` |
| Embedding the web pages and migrations | `embed` |
| SQL | `database/sql` for SQLite, pgx's own interface for PostgreSQL. No ORM and no query builder |

Sealed chats add no dependency. The server stores bytes it cannot
read, and the reference in `internal/sealed/ref` uses only the
standard library. Nothing in the repository needs a toolchain other
than Go for the server, and Flutter with the platform tools for the
apps.

## 5. Tests

### Layout

| What | Where | How it runs |
|------|-------|-------------|
| Storage | `internal/store/storetest`, called from `sqlite/sqlite_test.go` and `postgres/postgres_test.go` | The same suite against both backends. PostgreSQL runs when `CHURR_TEST_POSTGRES` holds an address, and is required in CI |
| Permissions | `internal/perm/perm_test.go` | A table of actor, action, target and expected answer, taken from `docs/PERMISSIONS.md`, against a store filled with one of everything |
| The API, end to end | `internal/api/*_test.go` | `testkit.NewServer` starts a whole server. Accounts are made through the real sign-up with a virtual passkey authenticator. Run on SQLite always and on PostgreSQL in CI |
| Live delivery | `internal/live`, `internal/api` | Real WebSocket clients: fan-out, catch-up by sequence number, a slow client being dropped, read positions across two devices |
| The proofs in the brief | `internal/api/proof_test.go` | A member, a guest and a bot cannot read, search, download or receive events from a private channel they are not in. The server holds no readable text or file from a sealed chat, search returns nothing from one, and the export contains nothing from one. A notify-anyway request answers the same, and sends the sender the same events, whether or not its target was reached. Nobody can be added to a locked channel by any route, a group included |
| No outbound connections | `internal/server/outbound_test.go` | With every switch off, a server is exercised under `testkit.NoOutbound` |
| Links | `internal/link/*_test.go` | `testkit.NewLinked` starts two or three servers and a hub in one process. A server receives nothing from a channel not shared with it; an event signed by the wrong server is rejected; a server that was offline catches up; a removed server gets nothing after the new key epoch |
| Calls | `internal/calls/*_test.go` | Pion clients (`CallClient`, in `internal/calls/calltest`) in the test process join a room on the loopback address: forwarding, layer choice from requested sizes, speaker detection, ICE restart, the ticket and pass rules |
| Push | `internal/push`, `internal/relay` | The decision rule as a table. The relay against fake APNs and FCM servers: payload shape, error mapping, limits, and that nothing is logged |
| Authentication | `internal/auth/*_test.go` | Passkeys with the virtual authenticator; OIDC and SAML against `testkit.FakeIdP`, including assertions that must be refused |
| Markdown | `internal/markdown` | The corpus in `testdata/markdown` |
| Webhook payloads | `internal/bots` | Each file in `testdata/webhooks` converts to its expected message |
| Import and export | `internal/importer`, `internal/export` | The small exports in `testdata/slack`; an export read back and compared |
| The import rules of section 3 | `imports_test.go` in the root | Reads the package graph |
| Browser | `e2e/` | Playwright drives the embedded pages against a real server binary |
| Sealed vectors | `internal/sealed/ref`, `testdata/sealed/` | `go test` checks the reference against the committed vectors and fails if `WriteVectors` would write anything different. The same files are then run by the Dart code (`app/test/core/sealed_vectors_test.dart`), the page's JavaScript (`e2e/sealed-vectors.mjs`) and the Swift decrypt (a unit test in `app/ios`, for the message vectors) |
| Flutter | `app/test/`, `app/integration_test/` | Flutter's own harness |
| Load | `tools/loadtest/` | Run by hand against a real machine; results go in the README |

### Browser tests

`e2e/run.sh` builds the server, starts it with `-dev` on a temporary
data directory, and runs the Playwright scripts against it, as
Codemail's does.

- Sign-in uses the browser's virtual authenticator, so real passkey
  code paths run, including the PRF extension.
- `chat.mjs` covers the web chat: posting, both thread views and the
  marker, files, reactions, forms, search, modes.
- `calls.mjs` opens two pages with fake camera and microphone devices
  and checks that each receives the other's audio and video.
- `sealed.mjs` checks a sealed chat between two browser contexts, sign
  out wiping the keys, and sign in restoring them.
- `markdown.mjs` runs the shared corpus against the page's parser.
- CI runs them in Chromium on every push, and in Firefox and WebKit
  nightly and before a release, because the definition of done names
  all three.

### Flutter tests

- `app/test/core/`: plain Dart tests of the API client, sync and
  catch-up against recorded event streams, the Markdown parser against
  `testdata/markdown`, and notification wording.
- `app/test/ui/`: widget tests of each screen in `CLIENTS.md` section
  9, at the four widths of section 10 and in both themes.
- `app/integration_test/`: the whole app on a Linux desktop against a
  server binary built from the same commit: sign in through the OAuth
  flow, post, receive, reconnect and catch up.
- Phone-only behaviour (wake-ups, the notification extension, the
  foreground service) cannot run in CI. It has a written checklist run
  on real devices before each release.

### What is run where

    go vet ./... && go test ./...          everything in Go, on SQLite
    CHURR_TEST_POSTGRES=postgres://… go test ./...     the same, both backends
    e2e/run.sh                             browser tests
    (cd app && flutter test)               the apps

## 6. Continuous integration

**There is no CI yet.** What follows is the plan. `build.yml` has been
written and has never run; `apps.yml` has not been written. Every
test named in this document has been run by hand, on one laptop.

Two workflows, split as in Codemail so that a change to the server
does not rebuild five apps.

### `.github/workflows/build.yml`

Runs on every push to `main`, every pull request, and version tags.

| Job | Does |
|-----|------|
| `docs` | Runs `tools/openapi-gen` and fails if `docs/openapi.yaml` differs from what it writes |
| `test` | Starts a PostgreSQL service container. `go vet ./...`. `go test ./...` on SQLite. `go test ./...` again with `CHURR_TEST_POSTGRES` set. Checks formatting with `gofmt` |
| `static` | Builds all three programs with `CGO_ENABLED=0` for Linux, macOS and Windows on amd64 and arm64, to prove nothing needs cgo |
| `browser` | Needs `test`. Installs Node and Playwright's Chromium, runs `e2e/run.sh`, uploads screenshots on failure |
| `browsers-all` | Nightly and on tags: the same in Firefox and WebKit |
| `vectors` | Runs `WriteVectors` and fails if `testdata/sealed/` differs from what is committed. The `browser` job runs `sealed-vectors.mjs` with the rest |
| `image` | Needs `test`. Builds the three images for amd64 and arm64 and, outside pull requests, pushes them to `ghcr.io/tikaydaes/` with tags for the branch, the commit and the version |
| `release` | On a version tag: attaches the static binaries and their checksums to the GitHub release |

### `.github/workflows/apps.yml`

Runs when `app/**`, `testdata/**` or the workflow itself changes, on
version tags, and by hand.

| Job | Does |
|-----|------|
| `test` | `flutter pub get`, `dart format --set-exit-if-changed`, `flutter analyze`, `flutter test`, which includes the sealed vectors |
| `integration` | Builds the server from the same commit, runs `app/integration_test` on a Linux desktop under a virtual display |
| `android` | Builds the APK with no Google library, then checks the result contains none. This is the F-Droid build |
| `android-play` | Runs `tool/play_flavor.dart` and builds the Play bundle |
| `ios` | Builds without signing on a macOS runner, including both extensions, and runs the notification extension's Swift decrypt against the sealed vectors |
| `macos`, `windows`, `linux` | Build each desktop app |
| `release` | On a version tag: attaches the desktop builds and the APK to the GitHub release. Store uploads are done by hand, with signing keys that are not in the repository |

Neither workflow holds the Apple or Google push keys. Those exist only
on the relay.

## 7. Decisions recorded here

- Three packages beyond `DESIGN.md` section 2 hold what would
  otherwise have no home: `chat` (the conversation service), `people`
  (accounts, groups, status) and `notify` (the notification rule).
- Supporting packages added: `model`, `config`, `version`, `secret`,
  `ratelimit`, `markdown`, `sealed`, `sealed/ref`, `export`,
  `server`, `hub`, `relay`, `testkit`, and since then `themes` with
  `themes/palette`, `calls/calltest` and `link/wire`.
- The sealed-chat scheme has a Go reference used only by tests, and
  its test vectors in `testdata/sealed/` bind the Dart, JavaScript
  and Swift implementations to it.
- Feature packages never import each other. `server` connects them
  through small interfaces.
- The import rules are checked by a test.
- No router, ORM, logging or TOTP dependency.
- The Markdown corpus and other shared fixtures live in `testdata/` at
  the top, because Go, Dart and JavaScript all read them.
- `tools/openapi-gen` holds the generator for `docs/openapi.yaml`, so
  the file is never edited by hand.
- A `tools/loadtest` program exists because the definition of done
  requires each target to be load-tested.
- Browser tests run in Chromium on every push and in all three
  engines nightly.

## 8. To verify before building

- `github.com/crewjam/saml`: that it is still the best-maintained
  pure-Go SAML library, against `github.com/russellhaering/gosaml2`.
- `github.com/skip2/go-qrcode`: it works and Codemail uses it, but it
  has not changed in years. Drawing the code in the page instead would
  remove it.
- `github.com/go-webauthn/webauthn`: that it passes the PRF extension
  through at registration and sign-in.
- `modernc.org/sqlite`: that the build includes FTS5 with its
  secure-delete option, and supports `VACUUM INTO` for snapshots.
- Pion: the items in `CALLS.md` section 20.
- That the Swift unit test for the notification extension can run on
  a CI macOS runner without signing.
- Whether reading video dimensions and duration needs a container
  parser dependency, or the small reader planned inside `blob` is
  enough.

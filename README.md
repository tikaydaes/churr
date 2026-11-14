# Churr

Churr is self-hosted team chat for a group that wants to own its chat:
channels, threads, files, search, bots, company sign-in, calls, sealed
two-person chats, and channels shared between Churr servers. The server
is one Go binary with an embedded database. One person can install it in
an afternoon.

**The catch: whoever runs a Churr server can read everything on it,
private channels included, except sealed chats, which are always between
exactly two people.** A shared channel can be read by the operator of
every server it is shared with. A hub's operator cannot read it, but sees
its name, its moderators and which servers are in it, and decides which
servers are let in. That is why Churr is built for a group to run for
itself.

One limit on sealed chats: unlocking sealed history with a passkey
happens on a page the server supplies, in the apps as well as in the
browser, so a dishonest or compromised server could capture the key at
that moment. The recovery phrase and approval from another device do not
have this weakness in the apps.

**Churr is not finished, and nobody has used it yet.** The server, the
web chat and the app are built and pass their automated tests, but no
person has used it end to end, the app has not been run on a phone, and
much has only ever run on one laptop. Read
[What exists today](#what-exists-today) before you plan anything around
it.

## What it does

This is what version 1 is designed to do. `BRIEF.md` is the scope and
`DESIGN.md` the design.

- Named channels, public and private, with sub-channels, sections, slow
  mode, and a retention period that really deletes.
- Ad-hoc channels between any set of people, and threads that can be
  moved out into a channel of their own.
- Messages in a small, safe Markdown: nothing in a message can run a
  script or load a remote image. Edits and deletions keep their history
  until it is purged.
- Files, served only as downloads from the server's own address.
- Search across everything you can read, and nothing you cannot.
- Polls, forms, checklists, reminders, scheduled messages, saved replies.
- Bots that are ordinary accounts with a token, and webhooks that accept
  Churr's own payloads and Slack's, Discord's and GitHub's.
- Passkeys for sign-in, with recovery codes. Optionally company sign-in
  over OIDC or SAML, and a second factor. No passwords, and no email.
- Sealed chats between exactly two people, end-to-end encrypted: the
  server stores ciphertext it cannot read.
- Voice, video and screen sharing, carried by the server itself.
- Channels shared between Churr servers, linked directly or through a
  hub, ordered by a clock the servers share. A channel on a hub belongs
  to the hub and is run by its moderators; a channel two servers share
  directly can be moved onto a hub to bring in a third. Hubs can be
  joined to each other by trunks.
- Colour themes: dark, light and a high-distinction pair built in, a
  theme editor that enforces contrast, and a theme library per server.
- An export of everything the server can read, for the owner, and an
  importer for Slack exports.
- SQLite by default, PostgreSQL if you prefer, and a command that moves a
  community between the two.

## What each operator can see

| Operator of | Can read | Cannot read |
| --- | --- | --- |
| Your Churr server | All messages and files, who talks to whom and when | The content of sealed chats |
| A server you share a channel with | That channel, and ad-hoc channels between its members and yours | Anything else on your server |
| A hub | Which servers exchange how much, and when. For each channel the hub owns: its name, description, listing mode, moderators and member servers. The operator can admit and remove servers and appoint moderators | Any content, or who the members are. Servers encrypt to each other before the hub sees it, and the hub never holds a key. An operator could admit a server they control, which would get the key from then on; every member server sees that happen |
| The push relay | A device's push address, which server woke it, and when | Any content, any name, any channel, or whether it is a call |

Nothing in the data directory is encrypted at rest except sealed secrets
and the ciphertext of sealed chats. Use disk encryption. The owner's
export contains every private and ad-hoc channel; whoever can run it
already holds the database.

## Outside connections

A server can make these connections and no others. Each has a switch.

| Connection | When | Default |
| --- | --- | --- |
| Certificate renewal | When the server runs without a reverse proxy | On in that mode |
| Push relay | A member who uses a phone app is mentioned or messaged while away | On |
| UnifiedPush | An Android phone without Google services registered an address to be woken at | Off |
| Link previews | A member posts a link | Off |
| Object storage | The operator chose S3-compatible storage for uploaded files | Off |
| Identity provider | Company sign-in is configured | Off |
| Linked servers and hubs | An admin has linked a server or joined a hub | Off |

One command makes a connection of its own: `churr import slack` fetches
the files a Slack export links to, from the addresses in the export,
unless it is given `-no-files`.

## What exists today

Written on 14 November 2026. "Tested" below means automated tests that
run in this repository (Go tests on SQLite and on a throwaway PostgreSQL,
browser tests of the web pages, Flutter tests of the app). There is no
CI: every run so far has been by hand, on one laptop.

**Built and tested by automated tests:**

- The `churr` server, on SQLite and on PostgreSQL: every operation in
  `docs/openapi.yaml` has a handler (`internal/api/UNIMPLEMENTED.txt`,
  which a test rewrites, lists none missing), with the live connection,
  the operator commands, sign-in in all its forms, bots and webhooks,
  the Slack importer and the export.
- Links between servers: shared-clock ordering, channels a hub owns with
  moderators and a directory, moving a two-server channel onto a hub,
  and trunks between hubs. The hub (`churr-hub`) and the push relay
  (`churr-relay`) build and are tested.
- The web pages (setup, sign-in, joining, account, admin) and the web
  chat at `/chat/`, including calls and sealed chats.
- The app, including calls, sealed chats, notifications and the desktop
  tray icon.
- The sealed-chat encryption, in Go (the reference, which writes the
  test vectors in `testdata/sealed/`) and in Dart. Both pass the same
  vectors. Calls in a sealed chat are encrypted with keys for each
  device, in the web chat and in the app.

**App builds made:** an Android debug APK, macOS, and the iOS simulator.

**Never yet done:**

- A person using it end to end, or a real community on it.
- The app on an Android device, or on a real iPhone.
- Windows and Linux builds of the app.
- The web chat in Firefox or Safari.
- Real Apple and Google push: the relay has only run against stand-ins,
  and has never been in service.
- Fetching a real certificate. Serving HTTPS with a certificate given as
  files has run in a test.
- Docker: the `Dockerfile` and `compose.yaml` have never been built or
  started.
- CI: the workflow in `.github/workflows/` has never run.
- Real Slack, Discord and GitHub payloads, and a real Slack export: only
  small hand-made ones have been tried.
- A client against a real hub: hubs have only run in tests.
- A live call in a sealed chat between the web chat and the app. The two
  agree on shared test vectors; they have never been in a call with
  each other.
- The reverse-proxy configurations in `docs/OPERATIONS.md`.
- An outside review of the sealed-chat cryptography. Do not rely on it
  until one has been done.

**Measured on one laptop only.** `BRIEF.md` sets targets to test
against. These were run once each on one machine, with the load
generator on the same machine; they are measurements of those runs and
nothing more.

| Target | Result |
| --- | --- |
| 500 members, 1 CPU, 1 GB, SQLite | Run on a laptop; see below |
| 5,000 members, 4 CPUs, 8 GB, PostgreSQL | Run once on a rented 4-CPU, 8 GB machine with PostgreSQL 18 at its defaults, the load generator on a second machine beside it. All 5,000 connected. At 5 messages a second for 60 s and at 20 a second for 30 s every post was accepted, every delivery arrived (99th percentile under 200 ms) and no connection was lost. The server peaked at about 770 MB. All four CPUs were busy while the 5,000 connected (about a minute); while posting, a third to a half of the machine was in use. The first run of this test failed and found a stall on PostgreSQL, since fixed |
| A call with 25 on audio and 8 on video | A 25-person call was run on one laptop. Not run on the mid-sized server |
| One hub | About 11,900 messages a second through one hub, and about 6,000 through three hubs in a line, their servers simulated in the same process (`internal/hub/load_test.go`) |

The 500-member run: a MacBook (Apple A18 Pro, 8 GB, macOS 27.0.1), Go
1.27.4, the server started with `GOMAXPROCS=1 GOMEMLIMIT=1GiB` in `-dev`
mode on SQLite, and `tools/loadtest` on the same machine, which made 500
members in 21 channels, connected one live connection each, and posted
for a minute. `GOMEMLIMIT` is a limit the Go runtime aims for, not one
the operating system enforces.

| | 5 messages a second, 60 s | 20 messages a second, 30 s |
| --- | --- | --- |
| Members connected | 500 of 500 | 500 of 500 |
| Messages posted | 300, none refused | 600, none refused |
| Deliveries to connections | 38,650 of 38,650 | 75,175 of 75,175 |
| Post, request to answer | p50 30 ms, p95 106 ms, p99 114 ms | p50 18 ms, p95 397 ms, p99 712 ms |
| Delivery, post to arrival | p50 35 ms, p95 105 ms, p99 113 ms | p50 42 ms, p95 426 ms, p99 691 ms |
| Server memory, everyone connected | 77 MiB | 82 MiB |
| Server memory, at its highest | 124 MiB | 147 MiB |

The second run followed the first on the same data directory, so its
database already held the first run's members and messages. No files, no
search, no calls and no phones were part of either.

Not in it at all, by design or not yet: deleting one's own account, and
jumping to a date in a conversation.

## Try it locally

    go build -o churr ./cmd/churr
    ./churr serve -dev

Open the setup link printed in the log to make the owner account with a
passkey. In `-dev` mode the server is at http://localhost:8472 over plain
HTTP, with data in `./data`. Never use `-dev` on a network.

After setup the web chat is at http://localhost:8472/chat/. Invite a
second person from the admin pages to have someone to talk to.

## Run it for real

You need a machine with a public address, a host name that points at it,
and ports 80 and 443 reachable. Calls also need port 24877 on UDP and
TCP. **Choose the host name with care:** passkeys are tied to it, and
changing it later means everyone makes new ones.

`docs/OPERATIONS.md` is the full guide: every flag and setting, ports,
proxies, containers, PostgreSQL, object storage, upgrading and
troubleshooting.

### Alone

    ./churr serve -hostname chat.example.com -data /var/lib/churr

Churr listens on 80 and 443, fetches a certificate for the host name from
Let's Encrypt and renews it. To use a certificate of your own, give
`-tls-cert` and `-tls-key`; port 80 then only redirects.

### Behind a reverse proxy

    ./churr serve -behind-proxy -hostname chat.example.com -data /var/lib/churr

Churr serves plain HTTP on `127.0.0.1:8472`. The proxy must terminate
HTTPS, pass WebSocket upgrades and the `Host` header, set
`X-Forwarded-For` and `X-Forwarded-Proto`, and allow uploads as large as
your limit. The media port does not go through the proxy.

### With Docker

`compose.yaml` publishes the web port on localhost only, for the host's
reverse proxy, and the media port to the network. Copy `.env.example` to
`.env`, fill it in, make the data folder writable by the image's user
(`mkdir data && sudo chown 65532:65532 data`), and run
`docker compose up -d`. The setup link is in `docker compose logs churr`.
This has never been built or run: see [What exists today](#what-exists-today).

### Every flag has an environment variable

`-behind-proxy` is `CHURR_BEHIND_PROXY`, and so on. There is no
configuration file. Everything else about a server is a setting in its
database.

## Backup and restore

Everything a community is lives in the data directory, plus the
PostgreSQL database if you use one. To back up while the server runs:

    ./churr backup -data /var/lib/churr /backups/churr-$(date +%F)

The destination becomes a complete data directory: a consistent snapshot
of the database, the keys, and the uploaded files. With PostgreSQL, dump
the database with `pg_dump` first; `churr backup -db postgres://…` then
copies the keys and files.

**Keep the `keys/` folder with every backup.** Without `seal.key` the
secrets stored in the database cannot be read, and without the other two
the server has a new identity. **A backup is as sensitive as the
server:** it holds everything the server can read. Encrypt it.

To restore: stop the server, put the backup where the data directory
should be, and start the server with the same `-hostname`. Try this on a
spare machine before you rely on it.

To upgrade, back up, replace the binary and start it. The database is
migrated at start. There is no downgrade: to go back, restore the backup.

Other commands for the operator:

    ./churr recovery <username> -data /var/lib/churr      a one-time link for someone locked out
    ./churr migrate-db -from <path or URL> -to <path or URL>   move between SQLite and PostgreSQL
    ./churr export -data /var/lib/churr -o export.zip     everything the server can read, as files
    ./churr import slack -data /var/lib/churr -hostname chat.example.com export.zip

## Development

    go test ./...                      everything, on SQLite
    tools/pgtest.sh go test ./...      the same with a throwaway PostgreSQL
    go run ./tools/loadtest -h         the load test

`docs/STRUCTURE.md` describes the packages and which may import which; a
test in the repository root holds the code to it. `docs/openapi.yaml` is
the API contract, written by `tools/openapi-gen`.

## How it was built

Churr is being co-coded with Claude Fable 5.2, Anthropic's AI model,
working in Claude Code. The owner set the requirements and made the
decisions: the scope, the trade-offs recorded in `BRIEF.md`, and every
ruling where two documents disagreed. Fable wrote the design documents,
the server and its tests, and ran them. What has only been run in tests
is listed above, and nothing here has been used by a real community.
Bug reports and corrections are welcome.

Churr uses no AI service when it runs.

## Licence

Churr is free software, released under the GNU Affero General Public
License, version 3. See `LICENSE`. If you run a modified version as a
service, you must offer its source to the people who use it.

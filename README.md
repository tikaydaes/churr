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
web chat and the app are built and pass their automated tests, three
tests of the links between servers excepted. Servers have run on rented
machines with a load tool as their members. No person has used Churr end
to end, and the app has not run on a phone. Read
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

Status at 15 November 2026.

**Built, and covered by automated tests** (Go tests on SQLite and on a
throwaway PostgreSQL, browser tests of the web pages, Flutter tests of
the app; run by hand, since there is no CI):

- The `churr` server, on SQLite and on PostgreSQL: every operation in
  `docs/openapi.yaml` has a handler (`internal/api/UNIMPLEMENTED.txt`,
  which a test rewrites, lists none missing), with the live connection,
  the operator commands, sign-in in all its forms, bots and webhooks,
  the Slack importer and the export.
- Two wire formats, compressed JSON and compressed Protocol Buffers
  (`docs/WIRE-PROTOBUF.md`). The web chat speaks JSON. The app, the load
  tool and the links between servers speak Protocol Buffers; the API
  answers in either.
- Large-channel mode: a channel with more members than
  `channels.large_channel_limit` (1,000 unless changed) is delivered in
  full only to the connections that have it open.
- Announcement channels, instants (a time in a message, shown to each
  reader in their own time zone), and the built-in slash commands, among
  them the chance commands (`/roll`, `/flip`, `/pick` and others).
- Links between servers: shared-clock ordering, channels a hub owns with
  moderators and a directory, moving a two-server channel onto a hub,
  and trunks between hubs. The hub (`churr-hub`) and the push relay
  (`churr-relay`).
- The web pages (setup, sign-in, joining, account, admin) and the web
  chat at `/chat/`, including calls and sealed chats.
- The app, including calls, sealed chats, notifications and the desktop
  tray icon. Builds exist for Android (a debug APK), macOS and the iOS
  simulator.
- The sealed-chat encryption, in Go (the reference, which writes the
  test vectors in `testdata/sealed/`) and in Dart. Both pass the same
  vectors. Calls in a sealed chat are encrypted with keys for each
  device, in the web chat and in the app.

**Failing tests:** three tests of the links between servers fail
intermittently. One of them fails every time:
`TestSlowedDownEventsThatGrowOldAreStampedAgain`.

**Verified on rented servers, with a load tool acting as every member**
(figures in [BENCHMARKS.md](BENCHMARKS.md)):

- Single servers of 500, 1,000, 5,000, 20,000 and 50,000 members.
- A call with 25 on audio and 8 on video on a 4-CPU server, beside chat.
- Three servers sharing a channel through a hub.
- Serving HTTPS with a real certificate from Let's Encrypt.

**Not done yet:**

- A person using it by hand, end to end, or a real community on it.
- The app on an Android device, or on a real iPhone.
- Windows and Linux builds of the app.
- The web chat in Firefox or Safari.
- Real Apple and Google push: the relay has only run against stand-ins.
- A person's client on a channel that goes through a hub.
- A live call in a sealed chat between the web chat and the app. The two
  agree on shared test vectors only.
- Docker: the `Dockerfile` and `compose.yaml` have never been built or
  started.
- CI: the workflow in `.github/workflows/` has never run.
- Real Slack, Discord and GitHub payloads, and a real Slack export: only
  small hand-made ones have been tried.
- The reverse-proxy configurations in `docs/OPERATIONS.md`.
- An outside review of the sealed-chat cryptography. Do not rely on it
  until one has been done.
- Deleting one's own account, and jumping to a date in a conversation:
  not in Churr at all.

## Performance and sizing

Churr has been measured with a load tool acting as its members, not yet
with people. The smallest machine tried, 1 vCPU and 1 GB with SQLite,
carried 1,000 members, all online, at 5 posts a second. The largest, 16
vCPU and 64 GB with PostgreSQL, carried 50,000 members, all online, at
50 posts a second. Neither was pushed until it failed, and the two
databases have not been measured on the same hardware, so nothing here
says which database a community of some size needs.

Every measurement, with its conditions, is in
[BENCHMARKS.md](BENCHMARKS.md).

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

Churr is built by agentic software development, with Claude Fable 5.2 in
Claude Code. The owner sets the requirements and makes the decisions.
Bug reports and corrections are welcome.

Churr uses no LLM service when it runs.

## Licence

Churr is free software, released under the GNU Affero General Public
License, version 3. See `LICENSE`. If you run a modified version as a
service, you must offer its source to the people who use it.

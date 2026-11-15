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
(figures under [Performance and sizing](#performance-and-sizing)):

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

**Conditions for every figure in this section.** Synthetic text chat
from `tools/loadtest`: 200-byte messages, one post in ten to a channel
every member is in, the rest to channels of about 750 members. Rented
virtual machines, load generators on separate machines, one run per
line, measured 15 November 2026. "Typical" is a fifth of the members
connected and 20 posts a second in total. "Everyone online" is every
member connected. Delivery time runs from a post to its arrival at each
connection, given as p50 / p95 / p99: half, 95 in 100 and 99 in 100
arrived within that time.

### What hardware do I need?

| Members | Machine | Database | Churr CPUs: typical / everyone online | Peak memory: typical / everyone online | Basis |
| --- | --- | --- | --- | --- | --- |
| 500 | 1 vCPU, 1 GB | SQLite | – / 0.04 | – / 95 MB | measured, 5 posts/s |
| 1,000 | 1 vCPU, 1 GB | SQLite | – / 0.10 | – / 146 MiB | measured, 5 posts/s |
| 5,000 | 4 vCPU, 8 GB | PostgreSQL 18 | 0.24 / 0.62 | 155 MB / 576 MB | measured, 20 posts/s |
| 20,000 | 8 vCPU, 16 GB | PostgreSQL 18 | 0.19 / 1.2 (busiest 4.1) | 453 MB / 1.9 GB | measured, 20 / 50 posts/s |
| 50,000 | 16 dedicated vCPU, 64 GB | PostgreSQL 18 | ~0.4 / ~1.6 to ~3 | ~1.1 GB / 3.9 to ~4.3 GB | measured, 20 / 50 posts/s |
| A size between two rows | the larger row's machine | as that row | not run | not run | inference |
| A row's size on a smaller machine | – | – | not run | not run | inference: Churr used a small part of each machine, so there is probably room, but how much is not known |

PostgreSQL used 0.2 of a CPU or less in every run. A 25-person call adds
about 1.2 CPUs and up to about 1.2 GB (below).

![Bar chart of the CPUs Churr used and its peak memory for nine runs, from 500 members on 1 vCPU (0.04 CPUs, 95 MB) to 50,000 members all online on 16 vCPU (about 1.6 to 3 CPUs, 3.9 to about 4.3 GB). The table above has the figures.](docs/perf/ladder-resources.svg)

### How far can I push it?

These are the heaviest loads that have been run. None is a ceiling: no
run in this table was pushed until it failed.

| Members | Machine | Load | Delivered | Delivery p50 / p95 / p99 | All connections dropped and reconnecting at once |
| --- | --- | --- | --- | --- | --- |
| 500 | 1 vCPU, 1 GB, SQLite | everyone online, 5 posts/s | 100% | 7 / 17 / 23 ms | all back in 0.65 s |
| 500 | same, with a real certificate | everyone online, 5 posts/s, protobuf | – | 9.5 / – / 21 ms | – |
| 1,000 | same, with a real certificate | everyone online, 5 posts/s, protobuf | – | 12 / – / 25 ms | – |
| 5,000 | 4 vCPU, 8 GB, PostgreSQL 18 | 1,000 online, 20 posts/s | 100% | 13 / 27 / 31 ms | 1.0 s |
| 5,000 | same | everyone online, 20 posts/s | 100% | 22 / 81 / 93 ms | 6.5 s |
| 20,000 | 8 vCPU, 16 GB, PostgreSQL 18 | 4,000 online, 20 posts/s | 100% | 17 / 29 / 32 ms | 2.5 s |
| 20,000 | same | everyone online, 50 posts/s | 100% | 78 / 455 ms / 1.0 s | 13,265 of 20,000 back in 18 s; the rest are told to retry later |
| 50,000 | 16 dedicated vCPU, 64 GB, PostgreSQL 18 | 10,000 online, 20 posts/s, protobuf | 100% | 35 / 59 / 66 ms | 10,000 back in 6.7 s |
| 50,000 | same | everyone online, 5% viewing the 50,000-member channel, 50 posts/s | 100% | 12 / 231 / 369 ms | – |
| 50,000 | same | everyone online, all viewing the 50,000-member channel, 50 posts/s, protobuf | 100% | 178 / 365 / 541 ms | – |

– is a figure that was not recorded. Clients spoke compressed JSON
unless the row says protobuf.

![Chart of delivery time on a logarithmic scale, at p50, p95 and p99, for nine runs: from 7, 17 and 23 ms at 500 members to 78 ms, 455 ms and 1.0 s with 20,000 members all online. The table above has the figures.](docs/perf/ladder-delivery.svg)

#### The smallest machine

1 vCPU and 1 GB with SQLite carries 1,000 members, all online, at 5
posts a second: half of deliveries within 12 ms and 99 in 100 within 25
ms, with Churr using 0.10 of the CPU and 146 MiB. With 500 online the
machine is 91% idle.

#### The largest run

50,000 members, all online and all viewing one channel, at 50 posts a
second, on 16 dedicated vCPU: every message delivered, 99 in 100 within
541 ms, Churr using about 1.6 CPUs and about 4.3 GB. The server's
network settings were the defaults; the test clients' were tuned. The
slowest single delivery took 13 s, in the first second after the server
began pacing its writes (see below).

#### Everyone reconnecting at once

After every connection is dropped together, 5,000 members are back in
6.5 s. Of 20,000, 13,265 are back within 18 s; the server tells the rest
to retry later. The load tool does not retry, so the time until all
20,000 are back is not known.

#### A call beside chat

25 on audio and 8 on video, on the 4-vCPU machine, the participants made
by a load generator.

| Chat on the same server | Audio lost | Video per person | Delay, mean | Churr CPUs |
| --- | --- | --- | --- | --- |
| None | 0.007% | 3.8 Mbit/s | 2.4 ms | 1.2 |
| 1,000 connected, 20 posts/s | 0 | 3.8 Mbit/s | 2.5 ms | 1.2 |
| 5,000 connected, 20 posts/s | 0.22% | 2.7 Mbit/s, some tiles lowered | – | 1.3 |

The call sends about 113 Mbit/s and nobody is dropped. In the last row
the 2-CPU machine generating the load was about 70% busy, which is the
likely cause of the lower video; all 3,486,001 chat deliveries arrived.
The server holds about 860 MB during the call (99 MB before it) and 1.24
GB with chat as well. Why a call takes that much is not known.

### What happens as my server grows?

**CPU.** At a typical load Churr uses under half a CPU at every size
measured: 0.24 with 1,000 online, 0.19 with 4,000, about 0.4 with
10,000. With everyone online it uses 0.62 CPUs at 5,000 members and 20
posts a second, 1.2 at 20,000 and 50, and about 1.6 to 3 at 50,000 and
50. Members online and posting rate rise together in those runs, so
their separate effects cannot be read from them. With the members fixed
(the linked servers below), CPU doubles when the posting rate doubles.

**Memory** follows the number of connections: 576 MB with 5,000 online,
1.9 GB with 20,000, 3.9 to about 4.3 GB with 50,000. That works out at
roughly 80 to 115 MB for each thousand connections (arithmetic on those
peaks, not a separate measurement).

**Bandwidth** follows deliveries: each post is sent once to every
connection that receives it.

![Bar chart of bytes per frame for the web chat, which speaks compressed JSON, and the apps, which speak compressed protobuf. A message with 200 bytes of text: 334 and 274. Reaction: 213 and 88. Read position: 188 and 56. Typing: 176 and 55. Presence change: 115 and 41.](docs/perf/frame-sizes.svg)

Over a whole run a delivered frame averages 244 bytes to a web client
and 185 to an app. With 50,000 online, all viewing one channel, at 50
posts a second, the server sends 68.5 MB/s if every client speaks JSON
and 51.9 MB/s if every client speaks protobuf. As a rule of thumb,
multiply deliveries a second by those averages: that run makes about
284,000 deliveries a second (5 posts to 50,000 connections and 45 to
about 750), which at 185 bytes is about 52 MB/s. A 25-person call with 8
cameras sends about 113 Mbit/s.

**SQLite or PostgreSQL.** SQLite has been measured to 1,000 members at 5
posts a second, and PostgreSQL from 5,000 up. Nothing between has been
run on either. SQLite writes one transaction at a time, so move when
many people post at once; `churr migrate-db` moves a community either
way.

**Large channels.** A channel with more than 1,000 members
(`channels.large_channel_limit`) is sent in full only to the connections
that have it open; the others are told that it changed. With 50,000
online and 5% of them viewing the 50,000-member channel, half of
deliveries arrive within 12 ms and 99 in 100 within 369 ms.

**The network card, on big servers.** The first limit a big server meets
is the number of small packets its network card passes: about 220,000 a
second on the virtual machines used here, whose card has one queue. On
Linux the server watches the machine's transmit queue and paces its own
writes when the queue overflows; nothing needs to be set.
`CHURR_LIVE_WRITES_PER_SECOND` pins the pace or turns it off, and
`docs/OPERATIONS.md` section 16 has optional tuning that raises what the
card carries. Raise the open-file limit (`LimitNOFILE=65536`) for more
than a few hundred people.

### Linking servers

Three 1-vCPU servers and a hub, 300 members on each server, all in one
shared channel, with compressed protobuf on the links and the clients.

![Chart of delivery time through a hub, at p50, p95 and p99. At 5 posts a second: 27, 38 and 43 ms. At 10: 25, 37 and 45 ms. At 20: 25, 36 and 42 ms.](docs/perf/hub-delivery.svg)

| Posts/s | Delivered | Delivery p50 / p95 / p99 | Each server, of 1 CPU | Hub CPUs |
| --- | --- | --- | --- | --- |
| 5 | all | 27 / 38 / 43 ms | 0.06–0.08 | 0.02 |
| 10 | all | 25 / 37 / 45 ms | 0.12–0.14 | 0.03 |
| 20 | all | 25 / 36 / 42 ms | 0.23–0.26 | 0.06 |

- **Delay added by the hub.** The median delivery to a member on the
  poster's own server is 12.5 to 14 ms, and to a member on another
  server 28 to 30 ms.
- **What a hub costs.** 0.02 to 0.06 of a CPU and 23 to 26 MiB at these
  rates. Each server holds 91 to 96 MiB. A relayed message is about 310
  bytes on the hub link.
- **A server cut off from the hub for 30 s** loses nothing, and its
  backlog clears within about 2 s of the link returning.

Linked servers larger than 300 members each have not been measured with
the present link protocol.

### What these numbers don't cover

- Uploads, search and push. The load is text only and steady; real
  people post in bursts.
- Variation: most lines are a single run.
- Other hardware. The machines are one provider's virtual machines
  (Hetzner Cloud for the 5,000 to 50,000 rows), with a one-queue network
  card; 1% to 4% of packets were lost in a way neither end could account
  for.
- Sizes and machines that are not in the tables. Lines marked inference
  are not measurements.
- Use by people. Every member in every run is the load tool.

The figures are in `docs/perf/data.json`; `docs/perf/README.md` says how
the charts are drawn.

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

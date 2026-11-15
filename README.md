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
tests of the links between servers excepted. The server has carried a
load tool's members on rented machines for one day. No person has used
it end to end, and the app has not been run on a phone. Read
[What exists today](#what-exists-today) and
[Measured results](#measured-results) before you plan anything around
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

Written on 15 November 2026. "Tested" below means automated tests that
run in this repository (Go tests on SQLite and on a throwaway PostgreSQL,
browser tests of the web pages, Flutter tests of the app). There is no
CI: every test run so far has been by hand, on one laptop.

**Built and tested by automated tests:**

- The `churr` server, on SQLite and on PostgreSQL: every operation in
  `docs/openapi.yaml` has a handler (`internal/api/UNIMPLEMENTED.txt`,
  which a test rewrites, lists none missing), with the live connection,
  the operator commands, sign-in in all its forms, bots and webhooks,
  the Slack importer and the export.
- Compressed Protocol Buffers as a second wire format beside JSON
  (`docs/WIRE-PROTOBUF.md`): the API and the live connection, the app,
  the load tool, and the links between servers.
- Large-channel mode: a channel with more members than
  `channels.large_channel_limit` (1,000 unless changed) is delivered in
  full only to the connections that have it open.
- Announcement channels, instants (a time in a message, shown to each
  reader in their own time zone), and the built-in slash commands, among
  them the chance commands (`/roll`, `/flip`, `/pick` and others).
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

**Tests that do not pass:** three tests of the links between servers
fail some of the time and are being looked into. One of them fails every
time: `TestSlowedDownEventsThatGrowOldAreStampedAgain`.

**Run on rented servers, once, under a load tool** (15 November 2026;
the numbers are under [Measured results](#measured-results)):

- Communities of 500, 5,000, 20,000 and 50,000 members made by
  `tools/loadtest`, on four sizes of machine.
- A call with 25 on audio and 8 on video on a 4-CPU server, the
  participants made by a load generator.
- Three servers sharing a channel through a real hub, with real
  certificates from Let's Encrypt.

**App builds made:** an Android debug APK, macOS, and the iOS simulator.

**Never yet done:**

- A person using it by hand, end to end, or a real community on it.
  Every member in the measurements below was the load tool.
- The app on an Android device, or on a real iPhone.
- Windows and Linux builds of the app.
- The web chat in Firefox or Safari.
- Real Apple and Google push: the relay has only run against stand-ins,
  and has never been in service.
- Protocol Buffers on the links between real machines. It has only run
  in tests; the hub measurements below did not use it.
- Docker: the `Dockerfile` and `compose.yaml` have never been built or
  started.
- CI: the workflow in `.github/workflows/` has never run.
- Real Slack, Discord and GitHub payloads, and a real Slack export: only
  small hand-made ones have been tried.
- A person's client on a channel that goes through a hub: the real hub
  has only carried the load tool's members.
- A live call in a sealed chat between the web chat and the app. The two
  agree on shared test vectors; they have never been in a call with
  each other.
- The reverse-proxy configurations in `docs/OPERATIONS.md`.
- An outside review of the sealed-chat cryptography. Do not rely on it
  until one has been done.

Not in it at all, by design or not yet: deleting one's own account, and
jumping to a date in a conversation.

## Measured results

Measured on 15 November 2026 on rented virtual machines (Hetzner Cloud,
Helsinki), with the load generators on separate machines on a private
network. The machines have since been deleted. Read
[what these numbers are not](#what-these-numbers-are-not) before relying
on any of them.

**The load.** `tools/loadtest` acts as every member. Messages are 200
bytes of text. One post in ten goes to #general, which every member is
in; the rest go to channels of about 750 members. "Typical" below means
a fifth of the members connected and 20 posts a second in total.
"Everyone online" means every member connected. Clients spoke compressed
JSON unless a row says protobuf. Each line is one run.

**In short:**

- **500 members, everyone online:** 1 vCPU and 1 GB with SQLite. Half of
  deliveries arrived within 7 ms and 99 in 100 within 23 ms. Churr used
  0.04 of the CPU and 95 MB.
- **5,000 members:** 4 vCPU and 8 GB with PostgreSQL. With everyone
  online, 99 in 100 deliveries arrived within 93 ms, and Churr used 0.62
  of a CPU and 576 MB. The same size of machine carried a call with 25
  on audio and 8 on video using 1.2 CPUs.
- **20,000 members:** 8 vCPU and 16 GB. A typical load was light (0.19
  of a CPU, 99 in 100 within 32 ms). With all 20,000 online at 50 posts
  a second, half arrived within 78 ms and 99 in 100 within 1.0 s. When
  all 20,000 were dropped and reconnected at once, 13,265 were back
  within 18 s and the rest were told to try again later.
- **50,000 members:** 16 dedicated vCPU and 64 GB. A typical load used
  about 0.4 of a CPU. With all 50,000 online and all of them viewing the
  one channel everybody is in, at 50 posts a second, every message was
  delivered, half within 178 ms and 99 in 100 within 541 ms. That last
  case delivered 38% of its messages when the day began.

In every run in the table below every message was delivered. The most
Churr used was about 3 of 16 CPUs; one run on 8 CPUs had a busiest
reading of 4.1. No machine smaller than the one in each line was tried
for that line.

![Bar chart: the CPUs Churr used and the peak memory in eight load-test runs, from 500 members on 1 vCPU (0.04 CPUs, 95 MB) to 50,000 members all online on 16 vCPU (about 1.6 to 3 CPUs, 3.9 to about 4.3 GB). The table below has every figure.](docs/perf/ladder-resources.svg)

![Chart of delivery time on a logarithmic scale, at the 50th, 95th and 99th percentile, for the same eight runs: from 7, 17 and 23 ms at 500 members to 78 ms, 455 ms and 1.0 s with 20,000 members all online. The table below has every figure.](docs/perf/ladder-delivery.svg)

| Members | Machine | Case | Posts/s | Delivery p50 / p95 / p99 | Churr CPUs | PostgreSQL CPUs | Peak memory | Everyone dropped and reconnecting at once |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 500 | 1 vCPU, 1 GB, SQLite | everyone online | 5 | 7 / 17 / 23 ms | 0.04 | – | 95 MB | all back in 0.65 s |
| 5,000 | 4 vCPU, 8 GB, PostgreSQL 18 | 1,000 online | 20 | 13 / 27 / 31 ms | 0.24 | 0.10 | 155 MB | 1.0 s |
| 5,000 | same | everyone online | 20 | 22 / 81 / 93 ms | 0.62 | 0.10 | 576 MB | 6.5 s |
| 20,000 | 8 vCPU, 16 GB, PostgreSQL 18 | 4,000 online | 20 | 17 / 29 / 32 ms | 0.19 | 0.05 | 453 MB | 2.5 s |
| 20,000 | same | everyone online | 50 | 78 / 455 ms / 1.0 s | 1.2 (busiest 4.1) | 0.14 | 1.9 GB | 13,265 of 20,000 back in 18 s; the rest were told to retry later, and the tool does not retry |
| 50,000 | 16 dedicated vCPU, 64 GB, PostgreSQL 18 | 10,000 online, protobuf | 20 | 35 / 59 / 66 ms | ~0.4 | ~0.05 | ~1.1 GB | 10,000 back in 6.7 s (in a JSON run) |
| 50,000 | same | everyone online, 5% viewing the 50,000-member channel (large-channel mode) | 50 | 12 / 231 / 369 ms | ~3 | 0.1 | 3.9 GB | – |
| 50,000 | same | everyone online and viewing the 50,000-member channel, protobuf | 50 | 178 / 365 / 541 ms | ~1.6 | 0.2 | ~4.3 GB | – |

Every row delivered 100% of its messages. Delivery is timed from the
post to its arrival at each connection. The machines were Hetzner's
CPX02 class, CPX32, CPX42 and CCX43. In the last row the server's
network settings were the defaults and the test clients' were tuned.

### What the day's fixes changed

The first runs of the day were much worse than the table above. These
are the same tests before and after.

![Bar chart of the hardest case in four steps. Before any fix 38% of messages were delivered. After presence by interest and compression, 98.6%, half within 11 s. After compressing once, half within 1.1 s. After self-adjusting write pacing, 100%, half within 178 ms.](docs/perf/hardest-case.svg)

| Test | Before | After |
| --- | --- | --- |
| 50,000 online, all viewing one channel, 50 posts/s | 38% delivered, 32,216 connections lost, 600–850 MB/s sent | 100% delivered, p50 178 ms, p95 365 ms, p99 541 ms |
| 50,000 members, 10,000 online, 20 posts/s | p50 1.7 s, p99 5.8 s, Churr ~6.5 CPUs | p50 35 ms, p99 66 ms, Churr ~0.4 CPUs |
| 5,000 members connecting | 59.9 s, with four CPUs busy for a minute | 9.8 s once presence was batched; after presence by interest, connect p99 31 ms |
| The first 5,000-member run, which found a stall on PostgreSQL | 84 of 300 posts accepted, delivery p99 45.6 s, 3,858 connections lost | 300 of 300, p99 180 ms, none lost |

The steps in the chart are: sending presence only to those with an
interest in it, and compressing (98.6% delivered, p50 11 s); compressing
each message once (p50 1.1 s, p95 21 s); and
the server pacing its own writes to what the network card carries (the
final line). `docs/OPERATIONS.md` section 16 describes the pacing.

### JSON and protobuf

![Grouped bar chart of bytes per frame as JSON, compressed JSON, protobuf and compressed protobuf for five kinds of frame. A message with 200 bytes of text is 592, 334, 323 and 274 bytes. The table below has every figure.](docs/perf/frame-sizes.svg)

| Frame | JSON | JSON, compressed | protobuf | protobuf, compressed |
| --- | --- | --- | --- | --- |
| Message with 200 bytes of text | 592 | 334 | 323 | 274 |
| Reaction | 415 | 213 | 138 | 88 |
| Read position | 267 | 188 | 50 | 56 |
| Typing | 170 | 176 | 49 | 55 |
| Presence change | 149 | 115 | 35 | 41 |

Bytes per frame. The smallest frames grow a little when compressed.
Run back to back on the same 50,000-member machine, compressed JSON
averaged 244 bytes per delivered frame (474 before compression) and
compressed protobuf 185 (212 before). With everyone viewing, the server
sent 68.5 MB/s as JSON and 51.9 MB/s as protobuf. Delivery times and CPU
were the same for both.

### A call beside chat

25 on audio and 8 on video, on the 4-vCPU machine. The participants
were made by a load generator on another machine.

| Chat on the same server | Audio lost | Video per person | Delay, mean | Churr CPUs |
| --- | --- | --- | --- | --- |
| None | 0.007% | 3.8 Mbit/s | 2.4 ms | 1.2 |
| 1,000 connected, 20 posts/s | 0 | 3.8 Mbit/s | 2.5 ms | 1.2 |
| 5,000 connected, 20 posts/s | 0.22% | 2.7 Mbit/s, some tiles lowered | not recorded | 1.3 |

Nobody was dropped from the idle-server call, which sent about 113
Mbit/s. In the last row the 2-CPU machine generating the load was about
70% busy and is the likely cause of the lower video; all 3,486,001 chat
deliveries in that run arrived. The server's memory grew from 99 MB to
about 860 MB during the call, and to 1.24 GB with chat as well. Why has
not been looked into.

### Three servers through one hub

Three 8-vCPU servers with 2,000 members each, all in one shared channel
of 6,000, joined through one hub on a 16-vCPU machine, with certificates
from Let's Encrypt.

![Chart of delivery time through a hub on a logarithmic scale. At 15 posts a second: 69, 336 and 737 ms at the 50th, 95th and 99th percentile. At 30: 40, 270 and 647 ms. At 60: 211 ms, 1.59 s and 3.0 s.](docs/perf/hub-delivery.svg)

| Posts/s | Delivery p50 / p95 / p99 | Median, same server | Median, other servers | Each server | Hub |
| --- | --- | --- | --- | --- | --- |
| 15 | 69 / 336 / 737 ms | 26 ms | 95 ms | 0.6–0.7 of 8 CPUs | 0.02 CPU |
| 30 | 40 / 270 / 647 ms | 24 ms | 46 ms | not recorded | 0.06 CPU |
| 60 | 211 ms / 1.59 s / 3.0 s | not recorded | not recorded | 2.2–2.9 CPUs | 0.12 CPU, 52 MiB |

Every run delivered 100%. The rows are not a clean comparison: before
the 30 posts/s run the test client machine's packet handling was spread
across its CPUs, and before the 60 posts/s run the same was done on all
four machines.

In a failure drill on an earlier build, one server was cut off from the
hub for 30 s at 15 posts a second. All 9,000,000 deliveries arrived,
none out of order; the longest wait was 49.7 s.

The hub alone, in an in-process test on a laptop
(`internal/hub/load_test.go`): with the JSON link protocol it began to
fall behind at 12,000 envelopes a second; with the protobuf link
protocol it did not at 12,000 (p99 21 ms). A relayed message is 1,051
bytes as JSON and 503 as protobuf.

### What these numbers are not

- **Synthetic, steady, text only.** No uploads, no search and no push
  were part of any run. Real people post in bursts and do other things.
- **Mostly single runs,** on one provider and one type of CPU. Nothing
  here says how much the numbers vary from run to run.
- **A network card with one queue.** These virtual machines have one,
  and it carried about 220,000 small packets a second at most. Other
  hardware will have a different ceiling. Between 1% and 4% of packets
  were also lost in a way neither end could account for.
- **The first second of a pace is rough.** When the server starts pacing
  its writes, packets are dropped in the first second. The worst single
  delivery in the hardest case took 13 s.
- **Sizes not run are guesses.** `docs/OPERATIONS.md` section 16 gives
  sizing advice and says which lines were measured and which are
  inferred from them.
- **Protobuf on the links between servers** has never run on real
  machines.
- **Nobody has used Churr by hand.** A load tool is not a person.

The data behind the charts is in `docs/perf/data.json`, and
`docs/perf/README.md` says how they were drawn.

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

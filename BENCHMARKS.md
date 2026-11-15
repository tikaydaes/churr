# Benchmarks

Every measurement of Churr under load, by configuration. The README says
what this means for someone choosing a machine; this file is the data.

## Conditions

Unless a row says otherwise:

- **Load.** Synthetic text chat from `tools/loadtest`, which acts as
  every member: 200-byte messages, one post in ten to a channel every
  member is in (#general), the rest to channels of about 750 members.
  Steady rate. No uploads, no search, no push.
- **Machines.** Rented virtual machines, load generators on separate
  machines. The 4-, 8- and 16-vCPU machines are Hetzner Cloud's CPX32,
  CPX42 and CCX43, on a private network.
- **Runs.** One run per row, measured 15 November 2026.
- **Wire.** `JSON` is compressed JSON, as the web chat speaks.
  `protobuf` is compressed Protocol Buffers, as the apps speak.

Columns, the same in every table:

| Column | Meaning |
| --- | --- |
| Members | Accounts in the community |
| Online | Members holding a live connection |
| Posts/s | Messages posted a second, in total |
| Wire | What the clients spoke |
| Delivered | Share of deliveries that arrived |
| Delivery p50 / p95 / p99 | Time from a post to its arrival at each connection: half, 95 in 100 and 99 in 100 arrived within it |
| Churr CPUs | Processors the `churr` process used |
| DB CPUs | Processors PostgreSQL used (SQLite runs inside `churr`) |
| Peak memory | The server's highest memory |
| Reconnect | Every connection dropped at once: time until they are back |

`–` is a figure that was not recorded. `~` is a figure recorded as
approximate.

## Standalone servers

![Bar chart of the CPUs Churr used and its peak memory for nine runs, from 500 members on 1 vCPU (0.04 CPUs, 95 MB) to 50,000 members all online on 16 vCPU (about 1.6 to 3 CPUs, 3.9 to about 4.3 GB). The tables below have the figures.](docs/perf/ladder-resources.svg)

![Chart of delivery time on a logarithmic scale, at p50, p95 and p99, for nine runs: from 7, 17 and 23 ms at 500 members to 78 ms, 455 ms and 1.0 s with 20,000 members all online. The tables below have the figures.](docs/perf/ladder-delivery.svg)

### 1 vCPU, 1 GB, SQLite

| Members | Online | Posts/s | Wire | Delivered | Delivery p50 / p95 / p99 | Churr CPUs | DB CPUs | Peak memory | Reconnect |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 500 | 500 | 5 | JSON | 100% | 7 / 17 / 23 ms | 0.04 | in `churr` | 95 MB | all back in 0.65 s |
| 500 | 500 | 5 | protobuf | – | 9.5 / – / 21 ms | – (machine 91% idle) | in `churr` | – | – |
| 1,000 | 1,000 | 5 | protobuf | – | 12 / – / 25 ms | 0.10 | in `churr` | 146 MiB | – |

The two protobuf rows served HTTPS with a real certificate.

### 4 vCPU, 8 GB, PostgreSQL 18

| Members | Online | Posts/s | Wire | Delivered | Delivery p50 / p95 / p99 | Churr CPUs | DB CPUs | Peak memory | Reconnect |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 5,000 | 1,000 | 20 | JSON | 100% | 13 / 27 / 31 ms | 0.24 | 0.10 | 155 MB | 1.0 s |
| 5,000 | 5,000 | 20 | JSON | 100% | 22 / 81 / 93 ms | 0.62 | 0.10 | 576 MB | 6.5 s |

### 8 vCPU, 16 GB, PostgreSQL 18

| Members | Online | Posts/s | Wire | Delivered | Delivery p50 / p95 / p99 | Churr CPUs | DB CPUs | Peak memory | Reconnect |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 20,000 | 4,000 | 20 | JSON | 100% | 17 / 29 / 32 ms | 0.19 | 0.05 | 453 MB | 2.5 s |
| 20,000 | 20,000 | 50 | JSON | 100% | 78 / 455 ms / 1.0 s | 1.2 (busiest 4.1) | 0.14 | 1.9 GB | 13,265 back in 18 s; the rest told to retry later |

The load tool does not retry, so the time until all 20,000 are back is
not known.

### 16 dedicated vCPU, 64 GB, PostgreSQL 18

| Members | Online | Posts/s | Wire | Delivered | Delivery p50 / p95 / p99 | Churr CPUs | DB CPUs | Peak memory | Reconnect |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 50,000 | 10,000 | 20 | protobuf | 100% | 35 / 59 / 66 ms | ~0.4 | ~0.05 | ~1.1 GB | 10,000 back in 6.7 s (JSON) |
| 50,000 | 50,000, 5% viewing #general | 50 | JSON | 100% | 12 / 231 / 369 ms | ~3 | 0.1 | 3.9 GB | – |
| 50,000 | 50,000, all viewing #general | 50 | protobuf | 100% | 178 / 365 / 541 ms | ~1.6 | 0.2 | ~4.3 GB | – |

#general has all 50,000 members, so it is delivered in large-channel
mode: in full only to the connections that have it open
(`channels.large_channel_limit`, 1,000 unless changed). In the last row
the server's network settings were the defaults and the test clients'
were tuned; the slowest single delivery took 13 s, in the first second
after the server began pacing its writes (see
[The network card](#the-network-card)).

## Linked servers

### Three servers of 1 vCPU and one hub

300 members on each server, all in one shared channel. Compressed
protobuf on the links and the clients.

![Chart of delivery time through a hub, at p50, p95 and p99. At 5 posts a second: 27, 38 and 43 ms. At 10: 25, 37 and 45 ms. At 20: 25, 36 and 42 ms.](docs/perf/hub-delivery.svg)

| Members | Online | Posts/s | Wire | Delivered | Delivery p50 / p95 / p99 | Churr CPUs | DB CPUs | Peak memory | Reconnect |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 3 × 300 | – | 5 | protobuf | all | 27 / 38 / 43 ms | 0.06–0.08 each server; hub 0.02 | – | 91–96 MiB each server; hub 23–26 MiB | – |
| 3 × 300 | – | 10 | protobuf | all | 25 / 37 / 45 ms | 0.12–0.14 each server; hub 0.03 | – | as above | – |
| 3 × 300 | – | 20 | protobuf | all | 25 / 36 / 42 ms | 0.23–0.26 each server; hub 0.06 | – | as above | – |

- Median delivery to a member on the poster's own server: 12.5–14 ms. To
  a member on another server: 28–30 ms.
- A relayed message is about 310 bytes on the hub link.
- A server cut off from the hub for 30 s: nothing lost; the backlog
  clears within about 2 s of the link returning.

## A call beside chat

25 on audio and 8 on video, on the 4-vCPU, 8 GB machine. The
participants are made by a load generator.

| Chat on the same server | Audio lost | Video per person | Delay, mean | Churr CPUs |
| --- | --- | --- | --- | --- |
| None | 0.007% | 3.8 Mbit/s | 2.4 ms | 1.2 |
| 1,000 connected, 20 posts/s | 0 | 3.8 Mbit/s | 2.5 ms | 1.2 |
| 5,000 connected, 20 posts/s | 0.22% | 2.7 Mbit/s, some tiles lowered | – | 1.3 |

The call sends about 113 Mbit/s and nobody is dropped. In the last row
the 2-CPU machine generating the load was about 70% busy, the likely
cause of the lower video; all 3,486,001 chat deliveries arrived. The
server holds about 860 MB during the call (99 MB before it) and 1.24 GB
with chat as well. Why a call takes that much is not known.

## Bytes on the wire

![Bar chart of bytes per frame for the web chat, which speaks compressed JSON, and the apps, which speak compressed protobuf. A message with 200 bytes of text: 334 and 274. Reaction: 213 and 88. Read position: 188 and 56. Typing: 176 and 55. Presence change: 115 and 41.](docs/perf/frame-sizes.svg)

| Frame | Web chat (compressed JSON) | Apps (compressed protobuf) |
| --- | --- | --- |
| Message with 200 bytes of text | 334 | 274 |
| Reaction | 213 | 88 |
| Read position | 188 | 56 |
| Typing | 176 | 55 |
| Presence change | 115 | 41 |
| Average per delivered frame over a 50,000-member run | 244 | 185 |

Bytes per frame sent to one connection. With 50,000 online, all viewing
one channel, at 50 posts a second, the server sends 68.5 MB/s when every
client speaks JSON and 51.9 MB/s when every client speaks protobuf.
Delivery times and CPU are the same for both.

## The network card

The 16-vCPU virtual machine has a network card with one queue, which
passes about 220,000 small packets a second at most. On Linux the server
watches the machine's transmit queue and paces its own writes when the
queue overflows (`docs/OPERATIONS.md` section 16). Between 1% and 4% of
packets were lost in these runs in a way neither end could account for.

## What these numbers don't cover

- Uploads, search and push.
- Bursts. The load is steady; real people are not.
- Variation between runs: most rows are a single run.
- Other hardware and other providers.
- Use by people. Every member in every row is the load tool.

The figures behind the charts are in `docs/perf/data.json`;
`docs/perf/README.md` says how the charts are drawn.

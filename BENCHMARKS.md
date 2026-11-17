# Benchmarks

Every measurement of Churr under load, by machine size, database and
arrangement. The README says what this means for someone choosing a
machine; `docs/OPERATIONS.md` section 16 turns it into rules for sizing;
this file is the data.

## Conditions

Unless a table says otherwise:

- **Build.** The current server (build `8d2ecf07`) for every post-rate
  figure. Connection ceilings on the 1- and 2-vCPU machines were measured
  on builds `6755bb2f` and `285680b9`, which have the same memory code;
  one size per machine was checked again on `8d2ecf07`.
- **Load.** Synthetic text chat from `tools/loadtest`, acting as every
  member, over compressed protobuf (`-wire protobuf`). Each member is in 3
  channels of about 750 members and in #general, which everyone is in;
  one post in ten goes to #general. Every connected member is sent every
  message in full (`-viewing 1.0`). Steady rate, 40-second steps. No
  uploads, no search, no push, no calls (calls: [A call beside chat](#a-call-beside-chat)).
- **Machines.** Rented virtual machines on Hetzner Cloud with a private
  network between them: 1 vCPU / 1 GB (CPX02), 2 vCPU / 4 GB (CPX22) and
  8 vCPU / 16 GB. Two 4-vCPU machines generate the load. No swap, nothing
  tuned. The 1- and 2-vCPU servers end TLS themselves with a real
  certificate; the 8-vCPU server serves plain HTTP on the private
  address (behind-proxy mode).
- **Databases.** SQLite: the default, inside the server. "PostgreSQL,
  dedicated": PostgreSQL 18 on its own 8 vCPU / 16 GB machine
  (`shared_buffers` 4 GB, about 1 ms away), default pool of 20.
  "PostgreSQL, same box": PostgreSQL 18 on the server's own machine,
  with its default settings on 2 vCPU; on 8 vCPU it is the instance that
  serves as the dedicated database for the smaller servers.
- **Runs.** One run per row, 15 and 16 November 2026.

**Healthy** means every delivery arrived, and 99 in 100 posts were
answered and 99 in 100 deliveries arrived within one second.

Columns:

| Column | Meaning |
| --- | --- |
| Online | Members holding a live connection |
| Posts/s | Messages posted a second, in total |
| Deliveries/s | Messages arriving at connections a second, in total |
| Post p50 / p99 | Time from sending a post to its answer: half, and 99 in 100, within it |
| Delivery p50 / p99 | Time from a post to its arrival at each connection |
| Idle | Share of the server machine's CPU left idle |
| Server memory | Resident memory of the `churr` process at its highest |

## Every configuration at a glance

| Machine | Database | Online, healthy at 5 posts/s | More online than that | Posts/s, healthy | Deliveries/s at that rate | What ran out | Past the limit |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 vCPU / 1 GB | SQLite | 12,000 | Refuses new connections from about 14,000 (4029, "short of memory") and stays up | 50, with 5,000 online | 58,700 | CPU | Slower; every post accepted; at 200 posts/s the slowest take about 28 s |
| 1 vCPU / 1 GB | PostgreSQL, dedicated | 12,000 | Refuses from about 14,000, stays up | 100, with 5,000 online | 116,000 | CPU | Slower; every post accepted; at 200 posts/s the slowest take about 24 s |
| 1 vCPU / 1 GB | PostgreSQL, same box | not measured | | | | | |
| 2 vCPU / 4 GB | SQLite | 20,000 | 40,000: all delivered, 99 in 100 within 1.5 s. Refuses from about 56,000, stays up | 50, with 10,000 online | 83,500 | CPU | Slower; at 200 posts/s 131 posts of 8,000 timed out |
| 2 vCPU / 4 GB | PostgreSQL, dedicated | 20,000 | 40,000: all delivered, 99 in 100 within 1.5 s | 100, with 10,000 online | 164,600 | CPU | Slower; at 200 posts/s none timed out, slowest about 26 s |
| 2 vCPU / 4 GB | PostgreSQL, same box | 20,000 | 60,000 all admitted, 99 in 100 within 4.1 s | 50, with 10,000 online | 83,500 | CPU, shared with PostgreSQL | Slower; at 200 posts/s 563 posts of 8,000 timed out |
| 8 vCPU / 16 GB | SQLite | 20,000 | 60,000: all delivered, 99 in 100 within 1.2 s (the channel everyone is in); no refusal. More not measured | 200, with 20,000 online | 530,000 | At 400 posts/s: the network card's transmit queue dropping packets; CPU 31% idle | 400 posts/s: 99 in 100 within 9.6 s; 18 of 20,000 connections closed as too slow |
| 8 vCPU / 16 GB | PostgreSQL, same box | 20,000 | 60,000: all delivered, 99 in 100 within 1.2 s; no refusal. More not measured | 400 (the highest tried), with 20,000 online; 1,600 in small channels | 1,053,600 | Not reached | not reached |
| 8 vCPU / 16 GB | PostgreSQL, dedicated | not measured | | | | | |
| Three 1 vCPU / 1 GB servers linked through a 1 vCPU / 1 GB hub | SQLite on each | 6,000 measured (2,000 a server, all in one shared channel) | not measured | 40 with 3,000 in the channel; 20 with 6,000 | 120,000 | The servers' CPU; the hub stayed at 0.11 of its CPU or less | Deliveries from other servers late by seconds; on the poster's own server still quick |

Sources: `RESULTS-rates-8d2ecf07.md` and the per-configuration
`RESULTS-*.md` files of the load-testing work.

## 1 vCPU, 1 GB

### SQLite

People online, at 5 posts/s:

| Online | Posts/s | Delivered | Post p50 / p99 | Delivery p50 / p99 | Idle | Server memory | Outcome |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 5,000 | 5 | 100% | 11 / 121 ms | 20 / 130 ms | 83% | 311 MiB | healthy |
| 10,000 | 5 | 100% | 12 / 250 ms | 88 / 266 ms | – | 556 MiB | healthy |
| 12,000 | 5 | 100% | 12 / 377 ms | 123 / 393 ms | – | 580 MiB | healthy |
| 15,000 asked | 5 | 100% of 14,018 admitted; 982 refused | 12 / 565 ms | 170 / 655 ms | – | 582 MiB | refusing new connections, alive |
| 20,000 asked | 5 | 100% of 15,498 admitted; 4,502 refused | 14 / 795 ms | 225 / 934 ms | – | 644 MiB | refusing new connections, alive |

The 12,000 row ran with `MemoryMax=700M`; the 20,000 row on build
`1f258ca9`. Posting rate, 5,000 online:

| Online | Posts/s | Deliveries/s | Post p50 / p99 | Delivery p50 / p99 | Idle | Outcome |
| --- | --- | --- | --- | --- | --- | --- |
| 5,000 | 20 | 24,600 | 12 / 85 ms | 25 / 148 ms | 49% | healthy |
| 5,000 | 50 | 58,700 | 19 / 176 ms | 68 / 324 ms | 9% | healthy, CPU nearly full |
| 5,000 | 100 | 116,000 | 23 / 664 ms | 135 ms / 1.20 s | 7% | just over |
| 5,000 | 200 | 234,000 | 36 ms / 27.8 s | 287 ms / 28 s | 7% | overloaded; every post accepted |

### PostgreSQL, dedicated

People online, at 5 posts/s:

| Online | Posts/s | Delivered | Post p50 / p99 | Delivery p50 / p99 | Idle | Server memory | Outcome |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 5,000 | 5 | 100% | 13 / 126 ms | 25 / 143 ms | – | 296 MiB | healthy |
| 10,000 | 5 | 100% | 15 / 252 ms | 91 / 320 ms | – | 586 MiB | healthy |
| 12,000 | 5 | 100% | 15 / 324 ms | 133 / 393 ms | – | 610 MiB | healthy |
| 15,000 asked | 5 | 100% of 14,215 admitted; 785 refused | 16 / 447 ms | 176 / 549 ms | – | 638 MiB | refusing new connections, alive |

Posting rate, 5,000 online:

| Online | Posts/s | Deliveries/s | Post p50 / p99 | Delivery p50 / p99 | Idle | Outcome |
| --- | --- | --- | --- | --- | --- | --- |
| 5,000 | 20 | 24,600 | 14 / 116 ms | 31 / 166 ms | 56% | healthy |
| 5,000 | 50 | 58,700 | 61 / 170 ms | 93 / 266 ms | 14% | healthy |
| 5,000 | 100 | 116,000 | 66 / 459 ms | 154 / 705 ms | 12% | healthy |
| 5,000 | 200 | 234,000 | 79 ms / 23.6 s | 258 ms / 24 s | 20% | overloaded; every post accepted |

The database machine was never above 4% busy.

## 2 vCPU, 4 GB

### SQLite

| Online | Posts/s | Delivered | Post p50 / p99 | Delivery p50 / p99 | Idle | Server memory | Outcome |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 5,000 | 5 | 100% | 13 / 67 ms | 17 / 79 ms | 89% | 327 MiB | healthy |
| 10,000 | 5 | 100% | 13 / 164 ms | 54 / 162 ms | 84% | 585 MiB | healthy |
| 20,000 | 5 | 100% | 14 / 352 ms | 143 / 393 ms | 77% | 1,080 MiB | healthy |
| 40,000 | 5 | 100% | 18 ms / 1.28 s | 410 ms / 1.47 s | 58% | 2,049 MiB | over: a message to all 40,000 takes over a second to send |
| 60,000 asked | 5 | 99.6% of 56,537 admitted; 3,463 refused | 51 ms / 2.3 s | 737 ms / 3.1 s | 43% | 2,558 MiB | refusing new connections, alive, slow |
| 10,000 | 20 | 100% | 7 / 109 ms | 59 / 211 ms | 55% | – | healthy |
| 10,000 | 50 | 100% | 15 / 162 ms | 86 / 299 ms | 22% | – | healthy |
| 10,000 | 100 | 100% | 24 ms / 1.07 s | 207 ms / 1.59 s | 8% | – | just over |
| 10,000 | 200 | 131 posts timed out | 39 ms / 28.8 s | 8.0 / 32 s | – | – | overloaded |

On `8d2ecf07`, 20,000 online at 5 posts/s: delivery p50 133 ms / p99
311 ms, post p99 182 ms, 957 MiB. Deliveries a second at 20, 50, 100 and
200 posts/s: 35,800, 83,500, 164,600 and 330,000.

### PostgreSQL, dedicated

| Online | Posts/s | Delivered | Post p50 / p99 | Delivery p50 / p99 | Idle | Server memory | Outcome |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 10,000 | 5 | 100% | 15 / 172 ms | 59 / 193 ms | – | 576 MiB | healthy |
| 20,000 | 5 | 100% | 15 / 418 ms | 158 / 451 ms | – | 1,080 MiB | healthy |
| 40,000 | 5 | 100% | 21 ms / 1.20 s | 422 ms / 1.52 s | – | 1,947 MiB | over, as with SQLite |
| 10,000 | 20 | 100% | 10 / 160 ms | 72 / 283 ms | 58% | – | healthy |
| 10,000 | 50 | 100% | 56 / 215 ms | 114 / 377 ms | 29% | – | healthy |
| 10,000 | 100 | 100% | 77 / 672 ms | 244 / 942 ms | 21% | – | healthy |
| 10,000 | 200 | 100%, none timed out | 76 ms / 24.9 s | 7.3 / 26 s | – | – | overloaded |

The database machine was never above 4% busy (3.8% of its 8 CPUs at
200 posts/s).

### PostgreSQL, same box

| Online | Posts/s | Delivered | Post p50 / p99 | Delivery p50 / p99 | Idle | Server memory | Outcome |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 10,000 | 5 | 100% | 14 / 244 ms | 60 / 225 ms | 80% | 600 MiB | healthy |
| 20,000 | 5 | 100% | 17 / 447 ms | 176 / 459 ms | – | 1,055 MiB | healthy |
| 40,000 | 5 | 100% | 22 ms / 1.18 s | 455 ms / 1.59 s | – | 2,013 MiB | over, as with SQLite |
| 60,000 | 5 | 100%, none refused | 279 ms / 4.16 s | 1.46 / 4.10 s | 36% | 2,400 MiB | slow |
| 10,000 | 20 | 100% | 16 / 234 ms | 89 / 328 ms | 43% | 512 MiB | healthy |
| 10,000 | 50 | 100% | 53 / 328 ms | 166 / 512 ms | 11% | 566 MiB | healthy, CPUs nearly full |
| 10,000 | 100 | 100% | 68 ms / 3.15 s | 1.51 / 3.57 s | 8% | 597 MiB | over |
| 10,000 | 200 | 563 posts timed out | 217 ms / 21.2 s | 7.0 / 38.8 s | 7% | 650 MiB | overloaded |

PostgreSQL itself held 1.1 to 2.9 GB of the 4 GB (resident memory of
its processes summed, which counts shared buffers more than once).

## 8 vCPU, 16 GB

Plain HTTP on the private network (no TLS). Server memory is the
machine's memory in use at its peak (PostgreSQL included where it runs),
because the per-process sampler recorded nothing on this machine; about
1,000 MB is in use with the server idle. Deliveries a second at 5, 50,
100, 200 and 400 posts/s: 15,300 (20,000 online), 133,000, 261,000,
530,000 and 1,053,600.

### SQLite

| Online | Posts/s | Delivered | Post p50 / p99 | Delivery p50 / p99 | Idle | Server memory | Outcome |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 20,000 | 5 | 100% | 6.1 / 156 ms | 148 / 901 ms | 96.5% | 1,606 MB (machine) | healthy |
| 60,000 | 5 | 100% | 6.3 / 430 ms | 295 ms / 1.20 s | 91% | 3,081 MB (machine) | just over: the channel everyone is in |
| 20,000 | 50 | 100% | 6.3 / 67 ms | 65 / 291 ms | 81% | 1,691 MB (machine) | healthy |
| 20,000 | 100 | 100% | 5.8 / 73 ms | 71 / 215 ms | 69% | 1,731 MB (machine) | healthy |
| 20,000 | 200 | 100% | 4.9 / 274 ms | 77 / 565 ms | 53% | 1,840 MB (machine) | healthy |
| 20,000 | 400 | 99.93%; 18 connections closed as too slow | 19 ms / 1.01 s | 160 ms / 9.57 s | 31% | 4,026 MB (machine) | over |

At 400 posts/s the server logged that the machine's transmit queue was
dropping packets, and one load generator was 85% busy in its busiest
second; SQLite logged no busy or locked error.

**SQLite's own write ceiling**, with 4,000 online in 600 channels of
about 20 and no #general, so that writing rather than delivery is the
work:

| Online | Posts/s | Delivered | Post p50 / p99 | Delivery p50 / p99 | Idle | Outcome |
| --- | --- | --- | --- | --- | --- | --- |
| 4,000 | 50 | 100% | 2.3 / 9.5 ms | 2.4 / 9.5 ms | 98% | healthy |
| 4,000 | 200 | 100% | 1.9 / 11.8 ms | 2.0 / 11.8 ms | 93% | healthy |
| 4,000 | 400 | 100% | 1.6 / 17.7 ms | 1.7 / 18.4 ms | 88% | healthy |
| 4,000 | 800 | 100% | 1.7 / 23.0 ms | 1.9 / 22.8 ms | 80% | healthy |
| 4,000 | 1,600 | 4 of 1,347,145 missing | 9.7 / 17.0 s | 9.7 / 17.0 s | 71% | over: posts wait with the CPU mostly idle |

Every post was accepted at every step. The ceiling lies between 800 and
1,600 posts a second.

### PostgreSQL, same box

| Online | Posts/s | Delivered | Post p50 / p99 | Delivery p50 / p99 | Idle | Server memory | Outcome |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 20,000 | 5 | 100% | 6.9 / 48 ms | 133 / 844 ms | 97% | 1,792 MB (machine) | healthy |
| 60,000 | 5 | 100% | 7.2 / 90 ms | 315 ms / 1.16 s | 92% | 3,325 MB (machine) | just over: the channel everyone is in |
| 20,000 | 50 | 100% | 7.0 / 62 ms | 71 / 590 ms | 81% | 1,895 MB (machine) | healthy |
| 20,000 | 100 | 100% | 6.3 / 53 ms | 72 / 279 ms | 69% | 1,953 MB (machine) | healthy |
| 20,000 | 200 | 100% | 5.8 / 115 ms | 76 / 287 ms | 53% | 2,034 MB (machine) | healthy |
| 20,000 | 400 | 100% | 6.2 / 240 ms | 96 / 459 ms | 35% | 2,246 MB (machine) | healthy |

The same write ceiling run, PostgreSQL on the box:

| Online | Posts/s | Delivered | Post p50 / p99 | Delivery p50 / p99 | Idle | Outcome |
| --- | --- | --- | --- | --- | --- | --- |
| 4,000 | 50 | 100% | 3.2 / 9.7 ms | 3.3 / 9.9 ms | 97% | healthy |
| 4,000 | 200 | 100% | 1.9 / 9.1 ms | 1.9 / 9.2 ms | 94% | healthy |
| 4,000 | 400 | 100% | 1.7 / 6.7 ms | 1.8 / 6.8 ms | 89% | healthy |
| 4,000 | 800 | 100% | 1.6 / 7.0 ms | 1.7 / 7.2 ms | 79% | healthy |
| 4,000 | 1,600 | 100% | 1.7 / 8.4 ms | 1.8 / 9.0 ms | 62% | healthy; the ceiling was not reached |

PostgreSQL's own processes held 2.0 to 3.3 GB. The same transmit-queue
warning was logged at every rate, without loss.

At 400 posts/s the PostgreSQL run was healthy and the SQLite run was
not. SQLite carried 800 posts a second in its write ceiling run and
logged no busy or locked error, so the difference is not SQLite's write
speed; it is not explained, and each is a single run.


## Unit costs

| Cost | Figure | Where from |
| --- | --- | --- |
| Memory per person online | About 50 KiB of server memory (311 MiB at 5,000 and 556 MiB at 10,000 on 1 vCPU; 585 MiB at 10,000, 1,080 MiB at 20,000 and 2,049 MiB at 40,000 on 2 vCPU; 957 MiB at 20,000 on `8d2ecf07`) | 1- and 2-vCPU connection tables |
| Deliveries a second per CPU, at the largest healthy rate | SQLite: about 59,000 (1 vCPU), about 42,000 (2 vCPU). PostgreSQL dedicated: about 116,000 (1 vCPU), about 82,000 (2 vCPU). PostgreSQL same box: about 42,000 (2 vCPU). 8 vCPU, either database: about 66,000 at 200 posts/s with half the machine idle; PostgreSQL same box about 132,000 at 400 posts/s | rate tables |
| Where the CPU goes when full | About half `churr`, half the kernel sending packets (1 vCPU SQLite: 34% user, 32% system, 25% software interrupts) | `RESULTS-rates-8d2ecf07.md` |
| Bytes sent per delivery | 116 to 125 bytes (server's network counter); 152 to 185 bytes per frame as the client counts it | 1- and 2-vCPU SQLite, 8-vCPU write ramp |
| Database growth per message | SQLite: about 1.5 KB (1.46 KB over 120,000 messages in the write ceiling run; 1.5 KB over 28,000 in the standard shape). PostgreSQL: about 1.6 KB (1.5 KB over 120,000 messages in the write ceiling run; 1.6 KB over 28,000 in the standard shape). Search index and events log included; 200-byte messages | 8-vCPU runs |
| Database size per member | SQLite: about 1.6 KB (31 MiB at 20,000, 92 MiB at 60,000). PostgreSQL: about 1.3 KB (37 MB at 20,000, 90 MB at 60,000). Each member in 4 channels | 8-vCPU runs |
| SQLite's write-ahead log | 16 MiB through 126,000 messages; 34 to 80 MiB after a reconnect storm | 8- and 2-vCPU runs |

## Reconnect storms

Every connection dropped at once, then back.

| Machine | Database | Online | How they come back | Back within | Turned away (4029) |
| --- | --- | --- | --- | --- | --- |
| 1 vCPU / 1 GB | SQLite | 10,000 | As the apps do, each after a random 0.5 to 41 s | 10,000 in 49 s | none |
| 2 vCPU / 4 GB | SQLite | 20,000 | All at the same instant | 9,806 in 47 s | the rest, "too many clients are reconnecting" |
| 2 vCPU / 4 GB | PostgreSQL, dedicated | 20,000 | All at the same instant | 4,007 in 20 s | the rest, as above |
| 2 vCPU / 4 GB | PostgreSQL, same box | 20,000 | All at the same instant | 4,008 in 30 s | the rest, as above |
| 8 vCPU / 16 GB | SQLite | 20,000 | As the apps do, each after a random 0.5 to 41 s | 15,669 in 49 s | 4,331, "too many clients are reconnecting" |
| 8 vCPU / 16 GB | PostgreSQL, same box | 20,000 | As the apps do | 20,000 in 41 s | none |

The load tool makes one attempt and does not retry a 4029; the apps
wait about 30 seconds and try again, so "turned away" is a count of
clients that would come back later, not of clients lost. Posts went on
during every storm and no event was missing or delivered twice.

## Linked servers

### Three 1-vCPU servers through a 1-vCPU hub

Each server has N members, all online and all in one channel shared
through the hub, and every post goes to that channel: each post is
delivered to N members on its own server and 2N on the other two. SQLite
on each server; compressed protobuf on the links. The hub's submit
limit for each server was raised to 120,000 envelopes a minute
(`churr-hub limits`; the default is 3,000). The hub ran the build of
15 November 2026, the servers the current build.

| Members in the channel | Posts/s | Deliveries/s | Delivered | Post p50 / p99 | Delivery p50 / p99: own server | Delivery p50 / p99: other servers | Each server CPU / memory | Hub CPU / memory | Outcome |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 3 × 1,000 | 20 | 60,000 | 100% | 15 / 33 ms | 16 / 41 ms | 33 / 60 ms | 0.59–0.61 / 109–115 MiB | 0.03 / 29 MiB | healthy |
| 3 × 1,000 | 40 | 120,000 | 100% | 25 / 50 ms | 27 / 63 ms | 47 / 98 ms | 0.88–0.92 / 109–118 MiB | 0.05 / 29 MiB | healthy |
| 3 × 1,000 | 80 | 240,000 | 100% | 37 / 69 ms | 42 / 82 ms | 6.7 / 12.3 s | 0.97–0.99 / 113–121 MiB | 0.09 / 35 MiB | over |
| 3 × 1,000 | 160 | 480,000 asked | 36% within the run | 1.7 / 10.7 s | 1.7 / 10.7 s | 4.5 / 48 s | 0.97–1.00 / 164–177 MiB | 0.01 / 38 MiB | overloaded |
| 3 × 2,000 | 20 | 120,000 | 100% | 43 / 74 ms | 51 / 104 ms | 79 / 139 ms | 0.89–0.92 / 170–176 MiB | 0.03 / 46 MiB | healthy |
| 3 × 2,000 | 40 | 240,000 | 100% | 57 / 97 ms | 67 / 126 ms | 2.6 / 10.2 s | 0.97–1.00 / 176–181 MiB | 0.04 / 46 MiB | over |

- The servers' single CPU is what ran out, at about the same deliveries
  a second per server as a server standing alone. The hub's highest use
  in any step was 0.11 of its CPU and 46 MiB; its machine stayed 85% to
  97% idle.
- Past the limit, members on the poster's own server still had their
  messages within 130 ms at the 99th percentile; deliveries from the
  other servers fell behind. At 160 posts/s the load tool stopped
  counting at the end of the run with most deliveries from other servers
  still to come; whether they arrived afterwards was not measured. No
  post was refused and no connection lost at any step.
- A relayed message costs about 320 bytes on the hub link.

### Three 1-vCPU servers through a hub, earlier build

300 members on each server, all in one shared channel; build of 15
November 2026.

![Chart of delivery time through a hub, at p50, p95 and p99. At 5 posts a second: 27, 38 and 43 ms. At 10: 25, 37 and 45 ms. At 20: 25, 36 and 42 ms.](docs/perf/hub-delivery.svg)

| Members in the channel | Posts/s | Delivered | Delivery p50 / p95 / p99 | Each server CPU / memory | Hub CPU / memory |
| --- | --- | --- | --- | --- | --- |
| 3 × 300 | 5 | 100% | 27 / 38 / 43 ms | 0.06–0.08 / 91–96 MiB | 0.02 / 23–26 MiB |
| 3 × 300 | 10 | 100% | 25 / 37 / 45 ms | 0.12–0.14 / 91–96 MiB | 0.03 / 23–26 MiB |
| 3 × 300 | 20 | 100% | 25 / 36 / 42 ms | 0.23–0.26 / 91–96 MiB | 0.06 / 23–26 MiB |

A server cut off from the hub for 30 s lost nothing; the backlog cleared
within about 2 s of the link returning.


## A call beside chat

25 on audio and 8 on video, on a 4 vCPU / 8 GB machine with PostgreSQL,
on an earlier build than the rest of this file. The participants are
made by a load generator.

| Chat on the same server | Audio lost | Video per person | Delay, mean | Churr CPUs |
| --- | --- | --- | --- | --- |
| None | 0.007% | 3.8 Mbit/s | 2.4 ms | 1.2 |
| 1,000 connected, 20 posts/s | 0 | 3.8 Mbit/s | 2.5 ms | 1.2 |
| 5,000 connected, 20 posts/s | 0.22% | 2.7 Mbit/s, some tiles lowered | – | 1.3 |

The call sends about 113 Mbit/s and nobody is dropped. In the last row
the 2-CPU machine generating the load was about 70% busy, the likely
cause of the lower video; all 3,486,001 chat deliveries arrived. The
server holds about 860 MB during the call (99 MB before it) and 1.24 GB
with chat as well. Most of that was the build measured: for every
subscriber of every camera it kept a full-size copy of the last 1,024
packets sent, 192 such buffers in this call. The current build keeps
each camera's recent packets once. Its memory in a call has not been
measured on these machines.

## Larger machines, earlier build

These sizes have not been measured on the current build. They ran on
the build of 15 November 2026, with PostgreSQL 18, clients speaking
compressed JSON unless the row says protobuf, and a fifth or all of the
members online.

![Bar chart of the CPUs Churr used and its peak memory, from 500 members on 1 vCPU (0.04 CPUs, 95 MB) to 50,000 members all online on 16 vCPU (about 1.6 to 3 CPUs, 3.9 to about 4.3 GB). The tables have the figures.](docs/perf/ladder-resources.svg)

![Chart of delivery time on a logarithmic scale, at p50, p95 and p99, for the same runs. The tables have the figures.](docs/perf/ladder-delivery.svg)

| Machine | Members | Online | Posts/s | Wire | Delivered | Delivery p50 / p95 / p99 | Churr CPUs | PostgreSQL CPUs | Peak memory | Every connection dropped at once |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 vCPU, 1 GB, SQLite | 500 | 500 | 5 | JSON | 100% | 7 / 17 / 23 ms | 0.04 | – | 95 MB | all back in 0.65 s |
| 1 vCPU, 1 GB, SQLite | 1,000 | 1,000 | 5 | protobuf | – | 12 / – / 25 ms | 0.10 | – | 146 MiB | – |
| 4 vCPU, 8 GB (CPX32) | 5,000 | 1,000 | 20 | JSON | 100% | 13 / 27 / 31 ms | 0.24 | 0.10 | 155 MB | 1.0 s |
| 4 vCPU, 8 GB (CPX32) | 5,000 | 5,000 | 20 | JSON | 100% | 22 / 81 / 93 ms | 0.62 | 0.10 | 576 MB | 6.5 s |
| 16 dedicated vCPU, 64 GB (CCX43) | 50,000 | 10,000 | 20 | protobuf | 100% | 35 / 59 / 66 ms | ~0.4 | ~0.05 | ~1.1 GB | 10,000 back in 6.7 s (JSON) |
| 16 dedicated vCPU, 64 GB (CCX43) | 50,000 | 50,000, 5% viewing #general | 50 | JSON | 100% | 12 / 231 / 369 ms | ~3 | 0.1 | 3.9 GB | – |
| 16 dedicated vCPU, 64 GB (CCX43) | 50,000 | 50,000, all viewing #general | 50 | protobuf | 100% | 178 / 365 / 541 ms | ~1.6 | 0.2 | ~4.3 GB | – |

`~` is a figure recorded as approximate. On the 16-vCPU machine #general
has all 50,000 members, so it is delivered in large-channel mode: in
full only to the connections that have it open
(`channels.large_channel_limit`, 1,000 unless changed). In the last row
the server's network settings were the defaults; the slowest single
delivery took 13 s, in the first second after the server began pacing
its writes (see [The network card](#the-network-card)).

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

Bytes per frame sent to one connection, measured on the 16-vCPU
machine. With 50,000 online, all viewing one channel, at 50 posts a
second, the server sends 68.5 MB/s when every client speaks JSON and
51.9 MB/s when every client speaks protobuf. Delivery times and CPU are
the same for both.

## The network card

These virtual machines have a network card with one queue; on the
16-vCPU machine it passed about 220,000 small packets a second at most.
On Linux the server watches the machine's transmit queue and paces its
own writes when the queue overflows (`docs/OPERATIONS.md` section 16).
Between 1% and 4% of packets were lost on the 16-vCPU runs in a way
neither end could account for.

## What these numbers don't cover

- **Members who are not online.** Every table counts people online; a
  community with many more members than are connected was not measured.
- **Bursts and real people.** The load is steady and synthetic; nobody
  has used Churr by hand. No browser was measured, and nothing on
  Firefox or Safari.
- **Uploads, search and push.**
- **A dedicated PostgreSQL behind the 8-vCPU server**, PostgreSQL on the
  same box on 1 vCPU, and the 4- and 16-vCPU machines on the current
  build.
- **Calls** on any machine but the 4-vCPU one, or beside more than 5,000
  chat connections.
- **Hub arrangements** other than the ones measured: a larger server as
  one of the linked servers, many shared channels, two hubs.
- **Reconnect storms** as the apps do them on 2 vCPU (there, only all at
  once).
- **Variation between runs**: every row is a single run.
- **Other hardware and other providers.**

The figures behind the charts are in `docs/perf/data.json`;
`docs/perf/README.md` says how the charts are drawn.

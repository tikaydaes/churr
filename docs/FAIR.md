# Fair chance: the specification

How a Churr server's chance outcomes are made and how anyone checks them,
with no server reachable. This is the specification a third party
implements from; `DESIGN-FAIR.md` is the design and the reasons.

A server draws once per message. A draw is an ECVRF proof
(ECVRF-SECP256K1-SHA256-TAI, RFC 9381, the owner's library
github.com/atikayda/ecvrf) over an `alpha` the chain fixes (section 6). Its
output `beta`, 32 bytes, is turned into outcomes by a frozen rule, the
derivation `derive: 1` (sections 3 to 5), which uses no floating point and
no other randomness: anyone holding `beta` re-derives every outcome.

The derivation is written three times and held to one vectors file
(section 7): Go `internal/fair`, JavaScript
`internal/web/static/shared/fair/derive.js`, Dart
`app/lib/core/fair/derive.dart`.

## 1. Conventions

- `‖` is concatenation of bytes.
- `u32be(x)` is x as 4 bytes, big-endian, unsigned; `u64be(x)` 8 bytes.
- `SHA-256` is FIPS 180-4.
- Whole numbers only. No step divides except `mod`, of whole numbers.
- In JSON, bytes are base64url without padding in a message's `fair`
  block, as everywhere in Churr's JSON, and hex in the vectors file.

## 2. Versions

`derive` names the rule. This document is `derive: 1`. A change to any
step, limit that changes an outcome, or order of consumption is a new
number; a message keeps the number it was drawn under, and a verifier
that does not know a message's number says so rather than guessing.

## 3. The stream and whole numbers

**Objects.** A message has one draw. Each chance object in it (the roll a
command made, each inline roll `[[...]]`, each value a module made) takes
an object index `i`, from 0, in order of appearance: a command's own
value first, then the objects of the text in the order their characters
stand. The index an object took is recorded with it (section 8).

**Blocks.** Object i's stream is the blocks

    stream(i, k) = SHA-256(beta ‖ u32be(i) ‖ u32be(k))      k = 0, 1, 2, ...

**Words.** The stream is read as unsigned 32-bit big-endian words: the
eight words of block 0 (bytes 0–3, 4–7, ..., 28–31), then the eight of
block 1, and so on. Every step below takes words from the object's
stream, in the order written, and nothing else does.

**below(n)**, for whole n with 1 ≤ n ≤ 2^32, a whole number from 0 to
n−1, every one equally likely, by rejection:

    if n = 1: the result is 0, and no word is taken
    limit = 2^32 − (2^32 mod n)
    repeat: w = next word; if w < limit, the result is w mod n

A word at or above `limit` is thrown away and the next one taken.

## 4. The primitives

What a caller may ask of a draw: a closed list. A primitive is a JSON
object with `op` and its inputs; fields it does not use are left out, and
a missing number is 0. Its result is a JSON object with one field. One
object may take several primitives, one after another from its stream.

| op | inputs | result | steps |
|---|---|---|---|
| `int` | `min`, `max` | `value` | `min + below(max − min + 1)` |
| `ints` | `min`, `max`, `count` | `values` | `int`, `count` times |
| `pick` | `n`, `k`, `replace`?, `weights`? | `picked` | below |
| `shuffle` | `n` | `order` | below |
| `deal` | `n`, `hands` | `dealt` | pick without replacement of the sum of `hands`, split in order |
| `yesno` | `num`, `den` | `yes` | `below(den) < num` |
| `dice` | `rows` | `rows` | section 5 |

Positions are whole numbers from 0 to n−1: a caller holds the list and
maps them.

**shuffle(n)** is Fisher–Yates as Durstenfeld wrote it:

    a = [0, 1, ..., n−1]
    for j = n−1 down to 1: r = below(j + 1); swap a[j] and a[r]
    order = a

**pick without replacement, no weights**: the first k steps of the same
walk, taking what lands at the end:

    a = [0, 1, ..., n−1]
    for t = 0 to k−1: j = n−1−t; r = below(j + 1); swap a[j] and a[r]; picked[t] = a[j]

With k = n the last step is below(1), which takes no word.

**pick with replacement, no weights**: `picked[t] = below(n)`, k times.

**pick with weights** (one whole number ≥ 0 per position):

    for t = 0 to k−1:
      W = the sum of the weights
      r = below(W)
      j = the first position whose weight is greater than r, taking each
          position's weight from r as it is passed (r −= w[j])
      picked[t] = j
      without replacement: w[j] = 0

**deal(n, hands)**: `taken` = pick of `sum(hands)` of n without
replacement, no weights; `dealt[0]` is its first `hands[0]` positions,
`dealt[1]` the next `hands[1]`, and so on.

**yesno(num, den)**: one `below(den)` (none when den = 1); yes when it is
below `num`.

**Limits.** A primitive outside them is refused, and nothing is drawn.
They bound the work a verifier is asked to do.

| | |
|---|---|
| `int`, `ints` | −2^31 ≤ min ≤ max ≤ 2^31 − 1; 1 ≤ count ≤ 1,000 |
| `pick` | 1 ≤ n ≤ 10,000; 1 ≤ k; k ≤ n without replacement, k ≤ 1,000 with it; weights: n of them, each 0 to 2^32, adding up to 1 to 2^32, and without replacement at least k of them above 0 |
| `shuffle` | 1 ≤ n ≤ 10,000 |
| `deal` | 1 ≤ n ≤ 10,000; 1 to 10,000 hands, each ≥ 1, together ≤ n |
| `yesno` | 1 ≤ den ≤ 2^32; 0 ≤ num ≤ den |

## 5. Dice

The rows and terms of the catalogue's `dice_roll` (`MESSAGE-FORMAT.md`),
with two more fields a term may have:

| field | |
|---|---|
| `sign` | 1 or −1 |
| `count`, `sides` | `count` dice of `sides` sides; `count` 0 is a plain number, `value` |
| `keep`, `keep_n` | `kh`, `kl`, `dh`, `dl`: keep highest, keep lowest, drop highest, drop lowest, `keep_n` of them |
| `explode` | each die that comes up `sides` rolls another |
| `success` | above 0: the term's value counts the counted dice that came up `success` or more, in place of their sum |

What the dice module writes maps onto these: `d20` is 1d20, `d%` is
1d100, `adv` is 2d20 `kh` 1, `dis` 2d20 `kl` 1, `k` alone is `kh`, and a
roll's comma-separated rows are its rows. The module's grammar has no
exploding or success dice yet; derive 1 has them so that adding them is
not a new rule.

**Order of consumption.** Rows in order; in each row, terms in order; in
each term, dice in order:

    for each die d of count:
      v = 1 + below(sides); append v to rolls
      while explode and v = sides and extra < 100:
        extra += 1; v = 1 + below(sides); append v to rolls

so an explosion's die stands in `rolls` right after the die that
exploded, and a term adds at most 100 dice by exploding. A plain number
takes nothing.

**Dropped.** With `keep`, the dice not counted, by their position in
`rolls`, listed in increasing order. `kh` and `dl` drop the lowest (all
but `keep_n` for `kh`, `keep_n` for `dl`); `kl` and `dh` drop the highest
likewise. Positions are ordered by roll, the lowest (or highest) first,
and of equal rolls the later position first; the first so many are
dropped.

**Value and total.** A term's `value` is the sum of its rolls not
dropped, or with `success` the number of them at or above it; a plain
number's is itself. A row's `total` is the sum of `sign × value` over its
terms.

**Limits.** 1 to 100 rows, 1 to 100 terms a row, 1 to 1,000 dice a term
and 10,000 in all (before explosions), 2 to 1,000,000 sides; `kh`/`kl`
keep 1 to `count`, `dh`/`dl` drop 1 to `count`−1; `success` 0 to
`sides`; a plain number has no `sides`, `keep`, `explode` or `success`.
The dice module's grammar is narrower (8 rows, 10 terms, 100 dice, 1,000
sides).

## 6. The chain

Each conversation has one chain of draws, numbered from 1, kept by the
server that draws them. What is built: an instance draws for the messages
its own people post, in its own conversations and in shared ones alike,
and is the `owner` of those draws. A hub channel's messages are drawn by
their author's instance the same way, `owner` naming that instance; a hub
drawing for its channels, and two servers mixing their outputs, are not
built (`DESIGN-FAIR.md` section 6).

**The chain's name.** The conversation's id, or, for a conversation
shared over the link, the stream's id, which every server of the stream
knows it by. A conversation drawn in before it was shared keeps those
draws on the chain of its own id and draws on the stream's from then on.

**alpha** for draw n:

    alpha(n) = "churr-fair-1"
             ‖ u16be(len(owner)) ‖ owner
             ‖ u16be(len(chain)) ‖ chain
             ‖ u64be(n)
             ‖ prev

- `"churr-fair-1"` is those 12 ASCII bytes, with no length before them.
- `owner` is the drawing server's name and `chain` the chain's name,
  each UTF-8 with its length in bytes before it.
- `prev` is SHA-256 of draw n−1's proof, 32 bytes; for draw 1 it is 32
  zero bytes.

Nothing in alpha comes from the person or the request. The proof is
`ECVRF_prove(sk, alpha)` and `beta = ECVRF_proof_to_hash(proof)`.

**The head.** The server keeps each chain's head, the number of its last
draw and SHA-256 of its proof (`fair_chains`, `SCHEMA.md`). A message's
draw is made, from the head, the first time something in it asks for
chance; the head is moved to it in the same transaction that stores the
message, and only from where the draw was made. If another message moved
it first, the message is made again from the start with the next draw
(the same alpha, so the same result, as anyone would get). A draw is used
by exactly one message and none is skipped. A message that asks for no
chance takes no draw.

**Deleting.** A deleted message keeps its `fair` block: its marker
carries it, a deletion soon after posting included, and so does a
channel's history sent to a server that joins. A message whose history
has been removed for good (retention, an admin's purge) still leaves the
next draw's `prev`, which says what came before.

## 7. The vectors

`testdata/fair/derive.json`, frozen with `derive: 1`:

- `streams`: `beta`, `i`, `k` and the `block` they give;
- `below`: one object's stream (`beta`, `i`), a list `ns`, the `values`
  below(n) gives for each in turn, and the `words` taken in all; one
  case rejects a quarter of its words (n = 3·2^30);
- `cases`: one object's stream and a list of primitives (`draws`) taken
  from it in turn, their `results`, and the `words` taken — every
  primitive and every form of dice, ties, explosions up to the cap and
  successes;
- `invalid`: primitives every implementation refuses, having taken
  nothing.

An implementation passes when every block, value, result and word count
is the same, and every invalid primitive is refused. The Go test writes
the file only when told to (`CHURR_WRITE_FAIR_VECTORS=1`), which was done
once; after that a change to its output is a new derive number.

The clients' check (section 9) is held to `testdata/fair/message.json`, a
message with a roll and inline dice, the history its server published and
its chain's names, written with the derive vectors; Go, the web chat and
the app all pass it as it is and fail it changed.

## 8. The `fair` block

In the message's JSON, beside `view` (`MESSAGE-FORMAT.md`):

| field | |
|---|---|
| `v` | 1: this layout |
| `derive` | 1: the rule outcomes are derived by |
| `owner` | the server that drew, by name |
| `n` | the draw's number in its chain |
| `pk` | the owner's VRF public key, 33 bytes, compressed |
| `alpha` | section 6 |
| `beta` | the proof's output, 32 bytes |
| `proof` | the ECVRF proof, 81 bytes |
| `prev` | SHA-256 of the chain's previous proof, 32 bytes |

Each value of the message that took chance from the draw says so in its
envelope: `draw: {i}`, its object index (section 3), and, for a module
that asked the host for declared outcomes (`Host.Draw`), `primitives`, what
it asked for, in order. A value whose kind has its own derivation, such as
`dice_roll`, lists none: its data is what was derived.

The block is stored with the message, never edited, carried unchanged by
the link and by a channel's history, and kept on its marker when it is
deleted. An edit that rolls nothing (inline dice changed are marked
`removed`) keeps it.

## 9. Checking a draw

With no server reachable, from the message, the conversation, and the VRF
key history of the owner (`/.well-known/churr`, a pin, a hub channel's
record):

1. `v` and `derive` are known.
2. alpha's layout holds and names this conversation's chain (its id, or
   its stream's); alpha is rebuilt from `owner`, that chain, `n` and
   `prev`.
3. `pk` is the key the owner's history had in force at the message's
   `created_at`: the last entry to have begun by then.
4. `ECVRF_verify(pk, proof, alpha)` holds and gives `beta`.
5. Every value with `draw` is re-derived from `stream(i, ·)` of `beta`: a
   roll's every die, every dropped die, every value and total
   (section 5).
6. Where the previous draw is at hand, its proof hashes to `prev`.

**A server receiving a message** over the link checks 1 to 4: the owner
must be the author's server, and the history is its own for its own
people, the pin of a direct link, or the hub channel's record. A
message older than its stream on the receiving server, drawn before the
conversation was shared, is checked against the chain its alpha names,
which the receiver cannot know otherwise. A block that fails is dropped,
and with it every value that took chance from it: the roll shows as its
text. The message still arrives, and `link.fair_block_dropped` is
audited. Outcomes are not re-derived on the server beyond each kind's own
check; the clients re-derive them.

**The clients** (the web chat, `shared/fair/verify.js`; the app,
`lib/core/fair/verify.dart`) check 1 to 5 against the history they pinned
for the block's owner, by name, and mark the roll "Verified", or, plainly,
"Not verified" with why (a check that was made and did not hold), or
"Cannot be checked here" with why (a draw by a server whose history the
client was never given); the roll is shown every way, and the block is
shown in full on request. Their own server's history comes from getServer;
every other server's from the conversation: a shared conversation's
`shared.vrf_keys` (`Conversation`, `docs/openapi.yaml`) gives the history of
every server whose draws it may hold, from sources that outlive those
servers: a hub channel's record (every server it ever named, one that left
included, and the hub's own) or the serving server's pin of each server of
a direct link. A client pins each history by server name on first sight; a
later one that is the pinned one with keys added is taken, and any other is
a warning and every draw of that server "Not verified".

## 10. Still drawn with ordinary randomness

Flip, pick, shuffle, draw, tarot and mahjongg are not yet modules: the
server draws them from the operating system's randomness, as before, and
they carry no proof. They move to the draw as they become modules, using
the primitives of section 4.

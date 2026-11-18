# Fair chance: the specification

How a Churr server's chance outcomes are made and how anyone checks them,
with no server reachable. This is the specification a third party
implements from; `DESIGN-FAIR.md` is the design and the reasons.

A server draws once per message. A draw is an ECVRF proof
(ECVRF-SECP256K1-SHA256-TAI, RFC 9381, the owner's library
github.com/atikayda/ecvrf) over an `alpha` the author's client fixes
(section 6). Its
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
an object index `i`, recorded with it (section 8), by one rule, the same
for every message, so that a server cannot try indexes until an outcome
suits it:

- the command's own value (the message's `view`), when it took chance, is
  object 0;
- the object entities that took chance follow, in the order of their
  position `at` in the text, from 1 when the view took chance and from 0
  when it did not;
- with no gap and no repeat, and nothing else takes an index (an object
  that asked for chance and made no value gives its index back to the next
  one).

An edit never adds a chance object. A roll it changes is marked `removed`
and keeps its entity and its index (it shows no outcome); one whose
characters it deletes is gone and leaves its index unused. So on an edited
message the indexes still rise with the position and never repeat, and a
gap is left only by a deletion; a message never edited has no gap, and a
`removed` object on it is refused. A message that drew is never quickly
corrected: every edit of it sets `edited_at`. A message whose indexes are not
these fails its check (section 9). A card's `fair_mark` names objects of this
set only. `testdata/fair/indexes.json` holds layouts accepted and refused;
Go (`fair.CheckIndexes`), the web chat (`shared/fair/indexes.js`) and the
app (`lib/core/fair/indexes.dart`) give the same answer for each.

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

## 5a. The built-in kinds

Each chance kind of the catalogue besides a roll is a fixed composition of
the primitives of section 4, asked in the order written here of the
value's object stream, and read from what the value's data says was asked.
A verifier given the data and the draw asks the same primitives of the same
stream and compares every outcome the data shows with what they give. The
modules draw through the same functions (`internal/fair/kinds.go`:
`KindAsks`, `Rederive`), and the web chat (`shared/fair/kinds.js`) and the
app (`lib/core/fair/kinds.dart`) repeat them.

| kind | its data | asks, in order | the outcome |
|---|---|---|---|
| `coin_flip` (`/flip`) | `outcome` | `yesno(1, 2)` | yes is `heads`, no is `tails` |
| `pick` (`/pick`) | `options`, `picked` | `pick(n = len(options), k = 1)` | `picked` is `picked[0]` |
| `shuffle` (`/shuffle`) | `options`, `order` | `shuffle(n = len(options))` | `order` is `order` |
| `card_draw` (`/draw`, `/mahjongg`) | `deck`, `cards`, `reversed` | `pick(n = size of the deck, k = len(cards))`; then, for a deck with orientation, `ints(0, 1, count = len(cards))` | card t is the deck's card at position `picked[t]`, the deck's cards in their order in `testdata/commands/decks.json`; card t is reversed when `values[t]` is 1 |
| `tarot_reading` (`/tarot`) | `spread`, `cards`, `reversed` | as `card_draw` of the `tarot` deck with `len(cards)` the spread's positions, always with orientation | as `card_draw` |
| `dice_roll` (`/roll`, `/dice`, inline) | `dice`, `rows` | `dice(rows)`, the rows with nothing rolled | section 5 |

A card is the outcome, not its position: where a deck holds a card more
than once (mahjong's tiles, pinochle, the shoes), any of its positions
shows the same card, and the check compares cards. `/mahjongg` is a draw
from the `mahjong` deck: mahjong's rules ask nothing more of the draw. No
primitive was added for these kinds.

`testdata/fair/kinds.json` holds a value of every kind drawn from a stated
`beta` and object index: what it asks, what derive 1 gives, and its data;
and values whose outcome the draw did not give, which every implementation
says do not re-derive.

## 6. The input

**One shape for every draw.** Every draw a server's VRF key ever makes, of
any kind (a message's now; tables, crash rounds, public chains and bots'
draws when they come), is proved over an input of exactly one shape, a
text:

    alpha = "<t>|<nonce>"

- `t` is a second in Unix time, in decimal, with no sign, no leading zeros
  and no spaces.
- `nonce` is a UUID in its canonical spelling: lowercase hexadecimal,
  hyphenated 8-4-4-4-12, nothing around it.
- The VRF is given the text's UTF-8 bytes: 47 bytes for a ten-digit `t`.
  For example `1763456789|0193a8ff-7c2e-7b1a-9f3e-2d4c5b6a7e80`.

There is one spelling for a pair, so a pair has one input. The proof is
`ECVRF_prove(sk, alpha)` and `beta = ECVRF_proof_to_hash(proof)`. The key
is used for nothing else, and one function of the server applies it
(`fair.Drawer.Draw`, `internal/fair`): every draw passes through its
tracker of used pairs before the key is touched, and a source test holds
the server to that one call site.

**A message's draw.** The author's client gives both halves of the input:
`t`, the second it sends the post at by its clock corrected to the
server's, as `draw_t`; and the message's `client_id` as the nonce, made
before it is sent. A post that takes chance (a module's command through
`runCommand`, a message with inline dice) needs both, or it is refused
with `invalid_value`. `draw_t` is stamped when the post is sent, never
when it is queued: a message waiting in an outbox takes its second when it
goes out.

**The client's clock.** A client knows the server's time before its first
draw and keeps it current from ordinary traffic: the live connection's
`hello` and every `pong` carry `server_time` (taken as received, a pong's
age half its round trip), and every answer of the API its `Date` header,
taken only until the live connection has said. A stated time more than five
minutes from the device's own clock is not taken once another has been
learnt, and a failure that turned on it says so.

**An honest client is never refused.** Nothing an honest client does
gets a draw refused: its clock is kept to the server's, a restart or a
crash of the server forgets no pair, and nothing else refuses a draw. So a
refusal is not routine. To the server it means the client tried to reuse or
steer a draw; to the client, that the server did. The server audits each
refusal of a draw for its second or its nonce as `fair.draw_refused`
(account, conversation, reason `second_outside_window` or
`nonce_reused`; at most once a minute for each account). The client shows
it to the person and never sends the draw again by itself.

**The window and the tracker.** The server draws only if `t` is within
10 seconds of its own clock, either way, and the nonce has not drawn under
any second the server still keeps; otherwise it draws nothing. Outside the
window the post is refused with `clock` and `server_time`, the server's
time. A nonce is unique across the whole window: one that has drawn under
any second still kept is refused with `conflict`, whatever `t` it now comes
with, unless it is the same post again with the same pair. The server keeps
the pairs it has taken in memory, per second, and lets a second's set go
once that second has left the window, and its nonces with it. Each pair is
also written to the table `draws_used` (second, nonce, and who asked),
durably, before the key is applied, and the table is read back before the
first draw after a start; its rows are deleted, in batches, once their
second has left the window. So a restart or a crash forgets no pair and
refuses nothing it would not have refused before: a crash between the write
and the proof leaves the pair taken, and the same request made again draws,
to the same result. A post that draws costs one statement more than one
that does not (the write), and one more once a window (the deletion). The
clock may step backwards: the server keeps the highest second its clock has
shown and holds the window's lower bound, and the letting go of seconds, to
it, so a step back never reopens a second it has let go.

**A retry gets the same draw.** A post that got no answer (the network
failed, the request timed out) is sent again with the same `client_id` and
the same `draw_t`, and only while that second is still inside the window;
after that it is not sent again, the person is told "The roll did not go
through", and rolls again themselves. The same person sending the same
`client_id` in the same conversation is given the message it made, with its
block, by the ordinary `client_id` rule, whatever the clock says: that is
not a second draw. A post that failed before it was stored, sent again with
the same pair, is drawn again, to the same result. So a server cannot
re-roll by failing a request. A nonce the server has seen is never sent
with another second: a retry that changes the second takes a new
`client_id`, and only the person makes one, by rolling again.

**A refused draw is not sent again.** A client never resends a draw refused
with `clock` or `conflict`, by itself or when the person retries the
message: it shows "The server refused this roll" with the server's reason
and notes it on the device. A refusal's `server_time` is learnt for later
posts only. When the request took longer than the window to be answered,
which the client measures, the client says it was the network instead.

**Nothing after the draw refuses the post for its outcome.** Every check a
post can fail that does not need the module's answer is made before
anything is drawn: permission, a sealed chat, slow mode, the thread, parent
and quote, files and cards, the form, the length of what was typed and its
checklist, and the word filter, which sees what the person typed (a
command's arguments, or the text with its inline dice as written) and never
the text a built-in module wrote. A module's own caps keep its text within
a message's length (each module's tests hold its longest). What can still
fail after the draw is the server's own machinery (its database, a module
that panics or does not answer in time), the same whatever the outcome;
sent again with the same pair, the post draws the same result.

**What remains.** A server can still decline a draw after working it out:
it holds the key, so it can compute the result as the request arrives and
refuse the post when it does not like it. The person sees the refusal (and
the server's audit records it), and if they roll again, that is a new
draw with a new nonce, which the server could not have worked out before
it arrived: a blind second draw, not a choice among known ones.

**Who is protected from whom.** The person cannot predict or choose the
result: it needs the server's key. The server cannot predict it before
the request arrives (the nonce is the client's, made fresh), cannot choose
among seconds (they are the client's, and one outside the window draws
nothing), and cannot change it after, since a VRF has one output for a key
and an input. A server and a person acting together can still choose a
nonce they like: the server can work out results ahead for nonces the
person will send.

**Each draw stands alone.** Nothing of another draw goes into an input.
An ECVRF proof is not unique: whoever holds the key can make many valid
proofs of one input, by choosing the nonce (RFC 6979's is only what an
honest prover uses); only the output, `beta`, is unique for a key and an
input. A proof fed into an input would let the key holder choose. The key
says whose draw it is; nothing of the conversation is in the input.

**Deleting.** A deleted message keeps its `fair` block and its
`client_id`: its marker carries both, a deletion soon after posting and a
purge included, and so does a channel's history sent to a server that
joins.

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

`testdata/fair/indexes.json` holds layouts of a message's chance values
whose object indexes are and are not the canonical ones (section 3).

An implementation passes when every block, value, result and word count
is the same, and every invalid primitive is refused. The Go test writes
the file only when told to (`CHURR_WRITE_FAIR_VECTORS=1`), which was done
once; after that a change to its output is a new derive number.

The clients' check (section 9) is held to `testdata/fair/message.json`, a
message with a roll and inline dice, its `client_id`, and the history its
server published, written with the derive vectors; Go, the web chat and
the app all pass it as it is and fail it changed.

## 8. The `fair` block

In the message's JSON, beside `view` (`MESSAGE-FORMAT.md`):

| field | |
|---|---|
| `v` | 1: this layout |
| `derive` | 1: the rule outcomes are derived by |
| `owner` | the server that drew, by name |
| `t` | the second the author's client gave the draw (section 6) |
| `pk` | the owner's VRF public key, 33 bytes, compressed |
| `alpha` | the input, as the text it is (section 6) |
| `beta` | the proof's output, 32 bytes |
| `proof` | the ECVRF proof, 81 bytes |

Each value of the message that took chance from the draw says so in its
envelope: `draw: {i}`, its object index (section 3), and, for a module
that asked the host for declared outcomes (`Host.Draw`), `primitives`, what
it asked for, in order. A value whose kind has its own derivation, such as
`dice_roll`, lists none: its data is what was derived.

The message's `client_id` is served with it whenever it carries a block:
it is the nonce of its input. The block is stored with the message, never
edited, carried unchanged by the link and by a channel's history, and kept
on its marker when it is deleted. An edit rolls nothing and keeps it: inline
dice it changes are marked `removed`, keeping their object index, and dice it
writes are marked `removed` with no `draw` (section 3).

## 9. Checking a draw

With no server reachable, from the message and the VRF key history of the
owner (`/.well-known/churr`, a pin, a hub channel's record):

1. `v` and `derive` are known.
2. `alpha` is exactly `"<t>|<client_id>"` spelt from the block's `t` and
   the message's own `client_id`, which is canonical: so a proof cannot
   be moved to another message.
3. `t` is within 10 seconds of the message's `created_at`; 120 for a
   message drawn by another server, whose clock may be off from the
   checker's.
4. `pk` is the key the owner's history had in force at the message's
   `created_at`: the last entry to have begun by then.
5. `ECVRF_verify(pk, proof, UTF-8(alpha))` holds and gives `beta`.
6. The object indexes are the canonical ones (section 3).
7. Every value with `draw` is re-derived from `stream(i, ·)` of `beta`: a
   roll's every die, every dropped die, every value and total
   (section 5); every other kind's outcome by its composition
   (section 5a). A value of a kind the checker knows whose data does not
   hold together fails; one of a kind it does not know is proved but not
   re-derived.

**A server receiving a message** over the link checks 1 to 6, with the
looser bound of 3: the owner must be the author's server, and the history
is its own for its own people, the pin of a direct link, or the hub
channel's record. A block that fails is dropped, and with it every value
that took chance from it: the roll shows as its text. The message still
arrives, and `link.fair_block_dropped` is audited. Outcomes are not
re-derived on the server beyond each kind's own check; the clients
re-derive them.

**The clients** (the web chat, `shared/fair/verify.js`; the app,
`lib/core/fair/verify.dart`) check 1 to 7 against the history they pinned
for the block's owner, by name, and draw the shield (`DESIGN-FAIR.md`
section 13) from that check alone: "Verified", a shield with a tick, or,
plainly, "Not verified" with why, a warning mark (a check that was made and
did not hold), or "Cannot be checked here" with why, a shield with a
question mark (a draw by a server whose history the client was never
given). The result is shown every way, and the shield opens Validate result
with the draw filled in (section 11). Their own server's history comes from getServer;
every other server's from the conversation: a shared conversation's
`shared.vrf_keys` (`Conversation`, `docs/openapi.yaml`) gives the history of
every server whose draws it may hold, from sources that outlive those
servers: a hub channel's record (every server it ever named, one that left
included, and the hub's own) or the serving server's pin of each server of
a direct link. A client pins each history by server name on first sight; a
later one that is the pinned one with keys added is taken, and any other is
a warning and every draw of that server "Not verified".

## 10. Nothing drawn with ordinary randomness

Every chance outcome a server shows is drawn from a message's draw: dice,
flip, pick, shuffle, draw, tarot and mahjongg are built-in modules, and a
module may import no other randomness. The operating system's randomness
serves only what is not a chance outcome: keys, tokens, identifiers,
nonces and the jitter of retries.

## 11. Validate result, and its document

Every instance and hub serves `/validate` to anyone, with no account: a
page that checks a draw with the code the chat checks with
(`shared/fair/validate.js`), in the browser, asking nothing of any server
once loaded unless the viewer names a server, whose `/.well-known/churr`
it then fetches (served to any origin) or whose pin it reads from the
browser. The app has the same view. A shield opens it with the draw filled
in (`DESIGN-FAIR.md` sections 12 and 13). Its verdicts are separate, each
pass or fail with its reason:

1. **proof**: `ECVRF_verify(pk, proof, UTF-8(alpha))` holds;
2. **beta**: `beta` is `ECVRF_proof_to_hash(proof)`;
3. **outcome**, one line per primitive: each object's primitives, asked in
   order of `stream(i, ·)` of `beta` (sections 3 and 4), give the claimed
   results; a primitive derive 1 does not have fails, and those after it on
   that object are not checked; a dice result is compared by its rows'
   rolls, dropped dice, values and totals; objects whose indexes repeat or
   fall back in the order listed fail (a document carries no positions, so
   whether one was left out is the message's check, section 3);
4. **alpha**: it is `"<t>|<nonce>"` in the one spelling of section 6, its
   second shown as a time;
5. **key**: when a server is named and its published history is to hand,
   `pk` is in it and is the key in force at the second `t`; otherwise "key
   not attributed", which is neither pass nor fail.

**The document.** "Copy the data" and "Download" give one JSON object, and
a link to the page carries it, UTF-8 and then base64url, after the `#`,
which a browser never sends to the server:

| field | |
|---|---|
| `churr_fair` | 1: this layout |
| `suite` | `ECVRF-SECP256K1-SHA256-TAI` |
| `derive` | 1: the rule the outcomes are derived by |
| `owner` | the server whose key it claims to be, by name; optional |
| `pk`, `beta`, `proof` | as in the `fair` block (section 8): base64url; a reader also takes hex |
| `alpha` | the input, as the text it is |
| `objects` | a list, each `{i, kind, primitives, results}`: the object index, the catalogue kind it was (optional, for the reader), the primitives asked of its stream in order (section 4), and the result claimed for each |
| `spec`, `library` | where to read this specification and the ecvrf library, so anyone checks it without Churr |

A message's value gives the primitives it recorded (`draw.primitives`), or
its kind's composition (section 5a), with the outcome its data shows as
their results. A card is claimed by what it is: where a deck holds a card
more than once, the positions the draw gives are written when they hold the
cards shown, and otherwise the first free position of each, which then does
not re-derive.

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
- In JSON, bytes are base64 (standard alphabet, padded) in a message's
  `fair` block and hex in the vectors file.

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

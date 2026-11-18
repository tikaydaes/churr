# Provably fair chance: every outcome from an ECVRF

A design, partly built. Parts marked [proposed] are not confirmed by the
owner. Built: the library in three languages (section 9), the key
(section 1), the chain and the derivation (sections 2 and 3), the `fair`
block (section 4), and dice on them, checked by the clients.
`FAIR.md` is the specification of what is built: every encoding, the
order outcomes are derived in, and the vectors.

## What the owner decided (R110)

- All chance outcomes run on ECVRF (github.com/atikayda/ecvrf,
  ECVRF-SECP256K1-SHA256-TAI; Go proves, Go/JS/Dart verify).
- Every server (instance and hub) has a secp256k1 key, generated once at
  first start, published.
- pk, alpha, randomness (beta) and proof are stored in the message's
  metadata, in sealed and clear messages alike.
- Verifiable after the instance has gone away. The instance cannot cheat.
- It must serve the crash game: result unknown before the server reveals
  it, fair ahead of time.

## Scope

Chance outcomes only: dice, flip, pick, shuffle, draw, tarot, mahjongg,
later games. Never secrets (keys, tokens, identifiers, nonces): those stay
on the operating system's randomness.

## 1. The key [built]

- Made at first start, `keys/vrf.key` beside the server's other key
  files, with `keys/vrf.history`: every VRF key the server has had and
  when each began. Never replaced while the server runs: a history whose
  key file is missing or different is refused at start. Losing it means
  a new key appended to the history, an operator's act (the command is
  not built); the old one stays in the history. Instance and hub alike.
- Published, so it outlives the server (`LINK-PROTOCOL.md` section 3):
  - in the server's signed identity: each entry of the history signed by
    its signing key, carried in the hello and at `/.well-known/churr`,
    unauthenticated, on the instance and on the hub;
  - in every hub channel's record (`vrf_keys`), a history per server
    that the hub keeps for good, including servers that left, and the
    hub's own; carried in rosters across a cluster;
  - pinned by each peer on a direct link (`servers.vrf_keys`) and by the
    hub for each of its servers: a later history must be the pinned one
    with keys added, or it is refused and audited;
  - given to clients in getServer, and every other server's in the
    shared conversations that may hold its draws (`shared.vrf_keys`, from
    the hub channel's record or the direct link's pin); the web chat and
    the app pin each by server name on first sight and warn when a later
    one is not the pinned one with keys added. A draw by a server whose
    history a client was never given says it cannot be checked there.
    [built]
- A message's `pk` must be the key the record or the pin held for that
  server at that time. A server cannot use a second key. [built: a
  receiving server and the clients check it, `FAIR.md` section 9]

## 2. The chain: what alpha is [built]

One chain per conversation, kept by the conversation's owner (the
instance; for a hub channel, the hub). Built: an instance draws for its
own people's messages; a hub channel's messages are drawn by their
author's instance, the chain named by the channel's stream, until the hub
draws for its channels. The exact encodings are `FAIR.md` section 6.

    alpha(n) = "churr-fair-1" ‖ owner name ‖ conversation id ‖ n

- Nothing in alpha comes from the person rolling or from the request.
  So nobody can grind: not the player with nonces, not the server by
  dropping a request and waiting for another (the next draw gets the same
  alpha and so the same result).
- A VRF has exactly one valid output per key and alpha. The server cannot
  choose, and cannot show two people different results.
- One draw per message. A message with several chance objects splits the
  one output: `stream(i, k) = SHA-256(beta ‖ i ‖ k)` for object i,
  block k.
- Each draw stands alone: draw n's alpha and output depend on the owner,
  the chain and n only. n is used exactly once per chain, and a gap in n
  is what shows a skipped or unsent draw. A deleted roll keeps its
  metadata as a marker, so no gap is made by deleting; history aged out
  leaves a gap that says so.
- Why the proof is not chained: an ECVRF proof is not unique (the key
  holder can make other valid proofs of one alpha with other nonces);
  only its output is. Chaining alpha to the previous proof would let the
  server choose among next draws; chaining to the previous output would
  add nothing n does not already give.

## 3. From randomness to outcome [built]

A frozen, versioned derivation (`derive: 1`), identical in Go,
JavaScript and Dart, held to one vectors file: unbiased integers by
rejection sampling from the stream, dice (including exploding), coin,
pick, Fisher–Yates shuffle, draws from a deck. A new rule is a new
version number; old messages keep theirs.

## 4. What a message carries [built]

In the metadata field, clear and sealed alike:

    fair { v, derive, owner, n, pk, alpha, beta, proof }

`beta` and `alpha` are recomputable; they are stored because the owner
asked, and they make a quick check possible. About 190 bytes.

A verifier, with no server reachable, checks: the proof under pk; alpha
rebuilt from the owner, the conversation and n; beta from the proof; the
outcome re-derived from beta and the request; pk against the record or
pin; and, where the conversation's other draws are at hand, that each n
is used once and what gaps there are.

## 5. Sealed chats [proposed]

The server sees alpha and makes the proof; it never sees the request or
the outcome.

1. At send, the client asks the owner for the next draw. It gets n and
   the whole `fair` block.
2. The client derives the outcome, writes it into the text, and seals
   the message; `fair` goes in the unsealed metadata and n inside the
   sealed part too.
3. A draw that is taken and not sent within a short time is posted by
   the server as a notice ("draw 7 was taken by X and not sent"), with
   its proof. Cherry-picking is public.

So the result is in the text, the check works offline, and the server
learns only that a draw happened.

## 6. Across servers [proposed]

- Hub channel: the hub is the owner and makes the draw with its key.
- Direct link between two instances: the two outputs are mixed,
  `beta = SHA-256(beta_A ‖ beta_B)`, each under its own key and the same
  alpha. Neither operator can know a result alone. (This replaces the
  open question about a modified instance faking its members' rolls.)
- One instance alone: its key only.

## 7. The crash game

- A game has its own chain; its start is announced with the key.
- Everyone can compute alpha for round n before it starts: it is the
  game's chain and n. That is the commitment: the result is already fixed,
  and nobody without the key can know it.
- The server computes the proof at round start, keeps it, and reveals it
  at the crash. Everyone verifies and derives the crash point.
- Needs timers and live cards (stage 7 of the modules work).

## 8. What this does not stop

- **The key holder knows results early.** An operator can compute the
  next draw before anyone asks. Alone that changes nothing. With a
  player in on it, they can choose when to roll or let someone else take
  a draw. Mixing two servers' outputs (section 6) removes this wherever
  a second server exists; on a single instance it remains.
- **Order is the server's.** Who gets draw n, and whether a cash-out
  arrived before the crash, is decided by the server's ordering of
  messages. The VRF does not prove ordering.
- **The start of a chain.** A conversation or game identifier is chosen
  by the server, so it could try several. Mixing in a second server, or
  the participants' own values at the start of a game, closes it.

## 9. In the code

- `modhost`: `Host.Random()` is the draw's stream, and `Host.Draw`
  derives declared outcomes from it. Modules are forbidden `crypto/rand`
  and `math/rand` by the layer rules, so every outcome a module makes is
  provable by construction. [built]
- The old chance code (flip, pick, shuffle, draw, tarot, mahjongg) moves
  to the stream as it moves into modules.
- The ecvrf sources are vendored (the owner's own code) [built]: Go in
  `internal/ecvrf` with the secp256k1 library it is written on; the web
  chat's verifier in `internal/web/static/shared/fair/ecvrf.js`, a plain
  port to BigInt and the browser's SHA-256 (verify and proof-to-hash
  only, no third-party code); the app's in `app/lib/core/fair/`. All
  three run the library's vectors, `testdata/ecvrf/vectors.json`.
- Each value records only its share of the draw (`draw: {i}`, and any
  declared outcomes it asked for): the proof is the message's. [built]

## 10. A general service: modules, manifests, bots, integrations, games

Owner, 2026-11-18: "this should be a generally available feature so
games, bots, integrations etc can all use it." [all below proposed]

**The rule: only a server holds a key and makes a draw.** A bot,
manifest, integration or game never does, so a rogue one cannot cheat
the randomness, wherever it runs. A bot whose home is another instance
still gets its draws from the conversation's owner.

### Three ways to use a draw

1. **Declared outcome (the normal way).** The caller says what it wants
   from a closed list of primitives and the owner's server derives it:
   whole number in a range; several of them; pick k of a list, with or
   without weights; shuffle a list; deal from a named deck; yes/no with
   a probability; plus the specialised kinds (dice expression, tarot
   spread). A generic `chance` kind carries the primitives, their inputs
   and results, so any client verifies any game built from them without
   knowing the game. The caller never sees raw randomness.
2. **Built-in modules** take the stream itself (`Host.Random()`), since
   they are our reviewed code, and their kinds have their own frozen
   derivations.
3. **Raw output, on request.** A bot may ask for the 32 bytes and
   compute its own outcome. The proof is carried and checked, and every
   client labels the result "randomness verified; outcome worked out by
   <bot>". An admin approves this in the bot's manifest.

### Where a draw lives

- **In a conversation:** the conversation's chain. Every draw shows as a
  message or a notice; none is silent. Limits per bot and per
  conversation stop a bot spending draws to shift the order.
- **A table:** a chain of its own inside a conversation, opened by a
  module or bot for a game, announced with the key and its start. The
  participants present may each add a value to the start, so the server
  cannot choose a favourable chain. The crash game is a table.
- **Outside any conversation** (an integration, a raffle on a web page,
  a separate product such as a board): a named public chain on the
  instance, with a log anyone can read and check. A separate product
  that is itself a server has its own key and chains under the same
  rules.

### When a draw is revealed

A closed list, stated in the proof block so readers know who knew what:

- now;
- at a stated instant (a raffle);
- after a time derived from the draw itself (the crash point; the
  server reveals, no bot knows early);
- part to one person now and all at the end (cards in a hand);
- when the caller asks. If the caller is given the output early, the
  block says so and since when.

### The interfaces

- Modules: `Host.Random()`, `Host.Draw(primitives)`, `Host.Table(...)`.
- Bots: the same primitives in a response to an invocation or a press,
  and operations to open a table, take a draw and reveal one. Declared
  in the bot's manifest and approved by an admin.
- Manifests: declared outcomes only (a deck, a pick list), resolved by
  the owner's server.
- Anyone, with no account: a published description of the proof block,
  the derivation and its vectors, the server's key at a well-known
  address, and the public chain logs. The ecvrf library verifies it in
  thirteen languages.

## Order of work

1. Keys: made at first start, in the identity statement, the hub record
   and the pins. [built]
2. The derivation and its vectors in three languages. [built]
3. The chain, the `fair` block, `Host.Random()`, dice on it; clients
   verify and show "verified". [built]
4. Sealed draws; two-server mixing on direct links.
5. The rest of chance as it becomes modules; the crash game after live
   cards.

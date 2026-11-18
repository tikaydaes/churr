# Provably fair chance: every outcome from an ECVRF

A design, partly built. Parts marked [proposed] are not confirmed by the
owner. Built: the library in three languages (section 9), the key
(section 1), the input and the derivation (sections 2 and 3), the `fair`
block (section 4), and every chance command on them as built-in modules
(dice, flip, pick, shuffle, draw, tarot, mahjongg), checked by the
clients.
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

## 2. The input: what alpha is [built]

Owner's ruling: the input is supplied by the client and only has to be
unique. The exact spelling is `FAIR.md` section 6.

    alpha = "<unix seconds>|<nonce>"      e.g. 1763456789|0193a8ff-7c2e-7b1a-9f3e-2d4c5b6a7e80

- For a message the client gives both: the second, by its clock
  corrected to the server's (`draw_t`), and the message's `client_id` as
  the nonce. One canonical spelling, so one pair has one input.
- Every draw the key ever makes, of any kind, has an input of this shape
  and passes through the one tracker of used pairs before the key is
  applied; the key is used for nothing else, and one function in the
  server applies it.
- The server takes a second only within 10 seconds of its clock and a pair
  only once. It keeps the pairs in memory per second, and in the table
  `draws_used`, written before the key is applied and read back at start,
  so a restart or a crash never lets a pair draw twice; a second's pairs
  go once it has left the window. An honest client is never refused, so a
  refusal is audited as a suspected attempt to reuse or steer a draw
  (`FAIR.md` section 6).
- A retry is the same post with the same `client_id`, and gets the same
  message and draw back. The server cannot re-roll by failing a request.
- Who is protected from whom: the person cannot predict or choose the
  result (it needs the key); the server cannot predict it before the
  request arrives, cannot choose among seconds (the client's), and cannot
  change it after (a VRF has one output per key and input). A server and
  a person acting together can still pick a nonce they like.
- One draw per message. A message with several chance objects splits the
  one output: `stream(i, k) = SHA-256(beta ‖ i ‖ k)` for object i,
  block k.
- Why no proof is fed into an input: an ECVRF proof is not unique (the
  key holder can make other valid proofs of one input with other
  nonces); only its output is. A proof in an input would let the server
  choose.

## 3. From randomness to outcome [built]

A frozen, versioned derivation (`derive: 1`), identical in Go,
JavaScript and Dart, held to one vectors file: unbiased integers by
rejection sampling from the stream, dice (including exploding), coin,
pick, Fisher–Yates shuffle, draws from a deck. A new rule is a new
version number; old messages keep theirs.

## 4. What a message carries [built]

In the metadata field, clear and sealed alike:

    fair { v, derive, owner, t, pk, alpha, beta, proof }

with the message's `client_id` served beside it. `alpha` is stored as the
readable text it is; `beta` is recomputable. About 220 bytes.

A verifier, with no server reachable, checks: alpha spelt from the
message's own `t` and `client_id`; `t` near the message's time; the proof
under pk; beta from the proof; the outcome re-derived from beta and the
request; pk against the record or pin.

## 5. Sealed chats [proposed]

A sealed message has a part that is not sealed. The server sees the
input and makes the proof; it never sees the request or the outcome.

1. The client seals the message as now, with the request (the dice, the
   deck) inside. The unsealed part carries `<t>` and the message's
   `client_id`, as any drawing post does.
2. The server accepts the message, makes the draw and puts the `fair`
   block in the unsealed part.
3. Each reader's client checks the proof and derives the outcome from the
   output and the request it unsealed.

The sender cannot see the result before sending. The server learns only
that a draw happened. The sealed text itself holds no result.

## 6. Across servers [proposed]

- Hub channel: the hub is the owner and makes the draw with its key.
- Direct link between two instances: the two outputs are mixed,
  `beta = SHA-256(beta_A ‖ beta_B)`, each under its own key and the same
  alpha. Neither operator can know a result alone. (This replaces the
  open question about a modified instance faking its members' rolls.)
- One instance alone: its key only.

## 7. The crash game

- A round's input has the one shape, `<t>|<nonce>`, its nonce announced
  before the round: everyone can compute alpha before it starts. That is
  the commitment: the result is already fixed, and nobody without the key
  can know it.
- The server computes the proof at round start, keeps it, and reveals it
  at the crash. Everyone verifies and derives the crash point.
- Needs timers and live cards (stage 7 of the modules work).

## 8. What this does not stop

- **A pre-disclosed input is known to the key holder.** Where an input is
  announced before the result is wanted (a crash round), the server can
  work the result out at once. That is intended; it is fair to the
  players when the party that chose the input is not the key holder.
- **Order is the server's.** Whose message comes first, and whether a cash-out
  arrived before the crash, is decided by the server's ordering of
  messages. The VRF does not prove ordering.
- **A chosen nonce.** A server and a person acting together can pick a
  nonce whose result they like, the server working results out ahead.
  Mixing in a second server's output closes it where one exists.

## 9. In the code

- `modhost`: `Host.Random()` is the draw's stream, and `Host.Draw`
  derives declared outcomes from it. Modules are forbidden `crypto/rand`
  and `math/rand` by the layer rules, so every outcome a module makes is
  provable by construction. [built]
- Flip, pick, shuffle, draw, tarot and mahjongg are built-in modules on
  the draw (`internal/modules/dice`, `choose`, `cards`), each kind a fixed
  composition of the primitives (`FAIR.md` section 5a). [built]
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

## 11. Pre-disclosed inputs and outside verification (owner, 2026-11-18)

Owner: "people can take the alpha, the public key, the proof (from
pre-disclosed alphas, like for crash or other bot games), and use the
verified output. This helps if servers want to run real-money games in
bots, etc. The primitives we're using here enable these kinds of
activities."

What that needs from Churr [proposed]:
- The input is the text `<unix seconds>|<nonce>` (R111); the key, alpha
  and proof are all anyone needs, with any standard
  ECVRF-SECP256K1-SHA256-TAI verifier. No Churr code is required.
- **Register now, reveal later.** A caller (a bot, a game) submits an
  alpha inside the time window. The server marks the pair used, makes
  the proof, keeps it, and returns a receipt signed by its identity key:
  "this alpha was registered at this time". The receipt is what a game
  shows players before bets. The proof is released when the caller asks,
  or at a stated time.
- The receipt and the later proof are both public and checkable with no
  account: the key at the well-known address, the derivation in
  `docs/FAIR.md`.
- A bot never holds the key, and the server never picks a bot's alpha.

## 12. The "Validate result" view (owner, 2026-11-18) [built: `FAIR.md` section 11]

Owner: "I'd like a 'Validate result' view that can take a game's alpha,
beta, proof, pk, requested primitives and certify accuracy (separately)
- people can take the data and independently verify too if they wish."

[proposed shape]
- A page on every instance and hub, public, no account, working with no
  network once loaded (the verifier and derivation run in the browser).
  The same view in the app. Opened from any roll's "Verified" mark with
  the fields filled in, or empty for pasting.
- Fields: alpha, beta, proof, pk, and the requested primitives with the
  claimed outcome (one JSON block, and the fields separately).
- Separate verdicts, each with its reason:
  1. the proof is valid for this pk and alpha;
  2. beta is the output of that proof;
  3. each requested primitive, re-derived from beta, gives the claimed
     outcome (one line per primitive);
  4. the alpha is well formed (`<unix seconds>|<nonce>`), with its time
     shown;
  5. whose key this is: checked against a server's published key history
     when the viewer names a server or the page knows it; otherwise
     "key not attributed".
- "Copy the data" and "Download" give one JSON document with everything
  needed, plus pointers to the specification and the ecvrf library, so
  anyone can verify without Churr.
- A link to the view carries the data in the URL fragment, so the server
  serving the page never sees what is being checked.

## 13. The shield (owner, 2026-11-18) [built]

Owner: "make it a little green shield that can be included on any card
(a 'type'?)"

[proposed shape]
- A catalogue block, `fair_mark`, that any card may include: a built-in
  module's, a manifest's or a bot's. Its data only names which draw and
  which outcomes on the card it stands for. It carries no verdict. Built:
  a card's `fair_mark` (`Card.fair_mark`, `{objects}`), the outcomes by
  their object index; the server refuses any other field in it. Cards of
  blocks come later; the built-in kinds draw it themselves.
- The client draws it from its OWN check, never from what the card
  says: green shield when the proof, the output and every named outcome
  check out; grey shield "cannot be checked here" when the key is not
  known; warning mark "not verified" when a check fails. A card cannot
  make it green.
- A tap opens "Validate result" with that draw filled in.
- Not colour alone: each state has its own shape and a spoken label.
- Reserved: no other block may draw this shield (the rule against
  imitating trust marks). Built-in kinds (a dice roll, a card draw) show
  it by default.

## Order of work

1. Keys: made at first start, in the identity statement, the hub record
   and the pins. [built]
2. The derivation and its vectors in three languages. [built]
3. The input, the `fair` block, `Host.Random()`, dice on it; clients
   verify and show "verified". [built]
4. Sealed draws; two-server mixing on direct links.
5. The rest of chance as it becomes modules [built]; the crash game after
   live cards.

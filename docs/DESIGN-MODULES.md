# Modules: the catalogue, three ways to extend, and the owner rule

A paper design for moving commands and recognised objects into modules
(rulings R106 to R109). It is a proposal. Nothing here is built and no
code was changed to write it. "Instance" means one Churr installation;
"the server" means its main program. Section 1 was read from the code at
`0ce9643`.

Every statement of design carries one of three marks:

- **[decided]**: the owner said it (R106 as later rulings left it, R107,
  R108, R109).
- **[unconfirmed]**: a coordinator reading or recommendation recorded in
  the rulings or in `notes/TYPES-CATALOGUE.md` as not confirmed. A
  recommendation awaiting the owner.
- **[proposed]**: proposed, awaiting the owner. This document's own
  fill-ins.

Numbers given as caps are proposals unless the code already holds them.

## 1. What exists today

**On the wire and stored**

- `Message.chance` (`Chance`, field 34) holds a result for six kinds:
  roll, flip, pick, shuffle, draw, tarot. `MessageInput.chance`
  (`ChanceInput`, field 13) asks for one. Beside them: `DiceRow`,
  `DiceTerm` and the enums `ChanceKind`, `ChanceOutcome`,
  `ChanceInputKind`, `DiceTermKeep` (`proto/churr/v1/churr.proto`,
  written by `tools/openapi-gen`).
- Inline dice are typed fields on `Entity`: `terms` (23), `total` (24),
  `removed` (25), with `ENTITY_TYPE_DICE` and `OBJECT_HINT_KIND_DICE`.
  Every other recognised object is typed fields on `Entity` too.
- Stored: `messages.chance` (JSON, migration 0003), the entities column,
  `fallback` and `search_text`. A chance message has **no text**; its
  words are in `fallback`.
- `internal/model/chance.go` (735 lines) is the dice grammar, the
  limits, rolling, card drawing, the validity check and the fallback.

**Who does what**

- Built-in commands are read and carried out **by the clients**
  (`shared/commands.js`, `chat/commands.js`,
  `app/lib/core/commands/commands.dart`). The server lists them and
  holds their names (`model.BuiltinCommands`). The table exists three
  times; `testdata/commands/commands.json` keeps them equal.
- A chance result is drawn by the instance the author is on
  (`chat.checkMarks`, `model.DrawChance`). People only, never sealed.
- Objects in text are found **by the server** on post
  (`internal/markdown/objects.go`, ten kinds in a fixed order). The
  clients hold the same scanner for the composer and sealed chats, all
  held to `testdata/markdown`. The composer sends only its exceptions,
  as `object_hints`.
- Inline dice go through `markdown.DiceSource`; an edit rolls nothing
  and keeps a roll only where the same dice stand in the same place
  (`internal/chat/dice.go` `keepRolls`).
- Reference patterns: a prefix and a value expression in a subset of
  regular expressions **with no groups** (`model.CheckRefExpression`),
  per channel and instance-wide.
- The dice grammar exists three times (Go, `shared/md/dice.js`,
  `app/lib/core/markdown/dice.dart`).
- Time phrases have two readers on each client (`parseMoment` for
  `/remind`, `instant/phrases` for the composer) and none in Go.

**Bots** (`internal/bots`, `docs/BOTS.md`)

- An admin creates a bot; whoever may add people adds it to a channel.
- A bot registers commands freely: name, description and a `hint` that
  is "not parsed". It receives `command.invoked` with the raw text.
- It answers an interaction with one of four types: `message`,
  `private` ("only you can see this", not stored), `dialog`, `none`.
- Its only buttons are a `bot` form with up to five; "no menu or picker
  outside a form's fields". Only a bot may post `cards`.
- In a shared channel its commands and dialogs reach only its own
  instance's accounts, and `bot` forms are refused.

**Between instances** (`docs/LINK-PROTOCOL.md`)

- Each instance keeps its own copy of a shared conversation.
  `message.created` is accepted only from the home instance of its
  author (section 12).
- A chance result travels as it is; the receiver checks that it holds
  together and takes the origin's word that the draw was fair.
- A hub owns the named channels on it and signs their records, yet it
  never holds a stream key and cannot read them: a member instance, the
  key-maker, makes each key and sends joining instances their history
  (sections 10, 13, 23a). `internal/hub` imports `link/wire`, `model`,
  `secret`, `config`, `ratelimit`, `acme`, `version` and nothing else.

## 2. The design in one page

1. **The catalogue** [decided: a set of cards, dialogs and recognisers
   every client knows, ours only; unconfirmed: its scope]. Everything a
   client must know how to do, in the web chat and the app: values,
   blocks, inputs, behaviours, effects, surfaces and viewer rules. A
   complete generic layer goes into the first app people keep, because
   an installed app may never be updated.
2. **Three ways to extend, one contract** [decided, R108]. A built-in
   module (our reviewed Go, compiled in), a manifest (data only,
   installed by an admin without a rebuild), an external bot (outside
   the process). All three declare commands as forms, receive structured
   invocations and presses, and answer with values and effects from the
   catalogue. No third-party code runs in any server, a hub included.
3. **The owner rule** [decided for hubs, R109; proposed as the general
   rule]. A conversation runs its owner's built-in modules, and only its
   owner admits bots and manifests. Peers send invocations and presses
   to the owner. The owner validates every value in its conversations.
   For a channel on a hub the owner is the hub.

Two rules run through all of it:

- **The text is the message; the card is a view of it** [proposed].
  Every module message and every object keeps its plain text. Search,
  notifications, exports, quotes and older clients use that. R106 asked
  for plain text "always"; this makes it the message itself.
- **Everything a client is handed is data** [decided: no module code in
  clients; unconfirmed: "treat card data as untrusted"]. The owner
  checks every value, the client checks it again, and anything unknown
  or invalid falls back.

## 3. The catalogue

From `notes/TYPES-CATALOGUE.md`. All of it is **[unconfirmed]**: the
coordinator's proposal at the owner's request ("Build as comprehensive
list of types as you can up front ... Servers can be updated, we may
have to deal with deprecated clients forever"). Section 3.10 is this
document's check of it against the clients.

### 3.1 The principle

An instance can be updated; an installed app may never be. So the
catalogue has two layers.

- **The generic layer** must be complete in the first app anyone keeps.
  Anything a later feature needs can be said with it, plainly, on an app
  of any age.
- **Specialised kinds** (a dice roll, a tarot spread) are better-looking
  versions of something the generic layer can already say. A kind that
  is not in the first release carries a generic rendition of itself.

The fallback has three steps: specialised kind, generic rendition, text.

"B" means baseline: in the first released client. "L" means later, with
a generic rendition carried.

### 3.2 Day-one rules

Harder to change than any type.

1. **Three-step fallback**. The text is always present.
2. **Unknown things degrade one piece at a time**: an unknown field is
   ignored; an unknown choice takes its field's declared default; an
   unknown block shows that block's text; an unknown behaviour is not
   applied; an unknown effect becomes a notice with its text; an unknown
   input makes the form say "this needs a newer app".
3. **A client says what it knows**: one catalogue level number, sent
   when it connects. The owner of a conversation says its own level in
   its manifest; a composer marks only what the owner knows.
4. **"This needs a newer app"** is a built-in state of every card and
   form. An instance may set a minimum client level, below which the app
   shows an update screen. Both exist in the first release.
5. **Unknown values survive**: a client that quotes, forwards or edits a
   message keeps values it does not understand byte for byte.
6. **Fields are only ever added** within a kind. A change of meaning is
   a new kind name.
7. **Limits come from the owner's manifest**, with hard caps in the
   client that no manifest can raise.
8. **Colour is by tone, not value**: accent, success, warning, danger,
   neutral, and a numbered categorical palette. Only the colour value
   carries a literal colour.
9. **Every picture has alternative text; every card a spoken label;
   every animation a still form.**
10. **Icons**: a named set built into the client, plus any emoji. An
    unknown name shows a neutral mark.
11. **Pictures**: PNG, JPEG, WebP, and SVG shown as a picture only. A
    sprite sheet is a picture plus an index of cells. No sound, font,
    script or style from outside.
12. **No secret entry**: no input hides what is typed, and no card may
    ask for a passkey, access key or password.

### 3.3 Values

Each has a recogniser where text can express it, a chip, a tooltip and a
dialog that is also its picker.

| Group | Kinds | |
|-------|-------|-|
| Plain | text; long text; number (range, step); yes/no; choice; several choices; list of one kind | B |
| Graded | rating; level; state (named, with a tone); tag; progress; percentage; fraction or ratio; range of numbers | B |
| Time | instant; calendar date; time of day; duration; interval; recurrence; time zone; position in a recording | B |
| Quantity | measurement; measurement range; compound measurement (5 ft 10 in); money (converted only with rates the owner supplies); number in a base (16, 2, 8, 36, 62, 64) | B |
| Text-like | Unicode character; colour; maths expression with its result; code snippet; key press; fingerprint; QR content; language; country or region; coordinates or place; link | B |
| Churr things | person; group; role; channel; direct or group chat; message; thread; file; picture; sound; video; emoji; instance; module or bot; command (tapping it fills the composer) | B |
| Churr things to come | meeting; room; recording; clip; shared document; ticket or card on a board | L |
| Chance | dice expression; die face; roll; coin; playing card; tile; deck or set | B |
| Derived | reference: a pattern or word list, with captures placed into a label, fields and a link | B |

### 3.4 Blocks

What a card or dialog is made of. Any block may hold values.

| Group | Kinds | |
|-------|-------|-|
| Text | text with typed holes (values shown the viewer's way); heading; caption; quote; code; maths; divider | B |
| Structure | stack; row; grid; columns; section with a side piece; titled group; collapsible; tabs; pager; carousel; scrolling area with a cap | B |
| Data | fields; list; table (typed columns, sorted and paged on the device); tree; board (lanes); timeline; calendar month; agenda; stat; comparison | B |
| Charts | bar; line; pie; sparkline; meter; progress bar | B |
| Pictures and media | picture; gallery; sprite cell; icon; avatar; avatar stack; badge; image viewer; sound player; video player; waveform; document preview | B |
| Live and time | countdown; clock in chosen zones; elapsed timer; live value (moves along a fixed curve from a start instant, with nothing sent); live dot; "working" placeholder; expiry | B |
| Placed | layout (items at x, y, rotation, layer, face up or down, labelled); grid board; track; pile; hand or fan; seats; roster (speaking, muted, hand raised) | B |
| Chance pieces | dice row (kept, dropped, exploded, success, failure, highest, lowest); coin; card; tile; token; wheel | B |
| States | notice (info, success, warning, danger); empty; error; needs a newer app | B |
| Embeds of what exists | message preview; person card; channel card; file card; link preview; poll; form; event; reminder; request (accept, decline, dismiss); theme card | B |
| Embeds to come | room card; meeting card; recording with chapters; stream card; document card; ticket card | L |

### 3.5 Inputs

| Group | Kinds | |
|-------|-------|-|
| Acting | button (primary, plain, danger; confirm; cooldown; disabled); button group; menu; link button (opens after confirmation); copy button | B |
| Entering | text field (length, pattern); long text; number; stepper; slider; yes/no switch; choice (list, radio, segmented); several choices; ranked list; rating | B |
| Pickers | instant; date; time of day; duration; interval; recurrence; time zone; measurement with unit; money; colour; person; group; role; channel; emoji; file | B |
| Finding | search field with suggestions from a fixed list, from the conversation's members, or asked of the module as the person types | B |
| Whole | form (required answers, ranges, pages); confirm; prompt | B |
| Never | a field that hides what is typed | |

### 3.6 Behaviours on the device

Declared on a block; nothing is sent. All B.

- **Triggers**: tap or click; hover (pointer only); first seen; a time
  is reached; a live value passes a mark.
- **Behaviours**: flip; reveal or hide; expand or collapse; step; sort;
  filter; find; select; zoom and pan; copy; highlight what is linked;
  switch tab; play, pause and seek; switch the unit, zone or base shown;
  show the original text.
- **Motion**: roll; deal; turn over; count up; shake; each with a still
  form. Sound and touch feedback only if the viewer has them on.

Tooltip, tap and dialog [decided: the three; unconfirmed: no hold]:

| Input | Tooltip only | Dialog only | Both |
|-------|--------------|-------------|------|
| Hover or keyboard focus | Tooltip | Nothing | Tooltip |
| Click, or Enter on focus | Tooltip stays | Dialog | Dialog |
| Tap on touch | Tooltip | Dialog | Dialog, with the tooltip's content at the top |

R107 asked "hold (if tap already used)?". The recommendation is to drop
hold: it is undiscoverable and is the system's text-selection gesture.

### 3.7 Effects

What the answer to a command or a press may do. A closed list. All B
except starting or joining a room (L).

- change the card for everyone; change it for me only; send a private
  part to one person;
- open a dialog (a form or a card); close it; mark a field as wrong;
- show a short notice; raise a notification;
- go to a conversation, message, thread, person or panel;
- open a link (after confirmation); copy text; download a file;
- put text or a command into the composer;
- start or join a room;
- nothing.

### 3.8 Surfaces

Where a module, manifest or bot may appear. A surface missing from an
old client is simply not shown there. All B.

| Surface | What it is |
|---------|------------|
| Command | Forms of typed slots, named options, sub-commands, optional and repeated slots; the message replied to and an attached file as implicit arguments |
| Object | A value marked in a message's text |
| Card | A value as a message |
| Message, person, conversation action | An entry in that thing's menu |
| Composer entry | An entry in the composer's add menu |
| Annotation | A chip put under someone else's message |
| Panel | A view made of blocks, fetched when opened |
| Sidebar entry; channel header | An entry with a count or dot; a badge or tab on a channel |
| Profile field; status | A line on a profile; a status with an end time set for a person |
| Link preview | An answer for links matching its patterns |
| Search | Its results in search |
| Notification | Text and where it leads |
| Settings | A section for a person, a channel's managers and the instance, each a form |

### 3.9 Who sees what, and how

All B.

- **Audience of any block**: everyone; one person; a role and above;
  until an instant; from an instant. The owner enforces this by what it
  sends, not the client by what it hides.
- **The viewer's own**: time zone, 12 or 24 hours, language, number and
  date conventions, preferred units, theme, text size, reduced motion,
  screen reader. The viewer's own rows, votes and name are marked.

### 3.10 Checked against the clients [proposed]

"Exists" below means both clients have code that does it for one fixed
feature. None of it is yet a block or value addressed by data.

**Already in both clients**

| Group | Exists in the web chat and the app |
|-------|------------------------------------|
| Values | text, long text, number, yes/no, choice, several choices, date, time of day (form fields); instant, interval (`event`), duration, recurrence (reminder repeats), time zone (the converter's zone search); measurement, number in base 16, 2 and 8, Unicode character, colour, code snippet, coordinates, link; fingerprint and QR (sealed chats, pairing); person, group, channel, message link, thread, file, picture, sound, video, emoji, command; dice expression, die face, roll, coin, playing card, tile, deck; reference |
| Blocks | heading, quote, code with highlighting, table without sorting (Markdown); fields (the bot card); picture, gallery, sprite cell, icon, avatar, badge, image viewer, sound and video players, text-file preview; countdown, "working"; layout (tarot spreads: x, y, turn, face down); dice row (kept and dropped only), coin, card, tile; notice, empty, error; roster (a call); collapsible, tabs; a bar (poll tallies only); message preview, person card, channel card, file card, link preview, poll, form, event, request, theme card |
| Inputs | button, menu, copy button; text, long text, number, yes/no, choice, several choices; date and time pickers, time zone, person, channel, emoji, file; colour (theme editor only); search with suggestions (composer, quick switcher); form, confirm, prompt |
| Behaviours | flip and turn over, reveal (spoilers, cards), expand, step, zoom and pan, copy, play and seek, switch unit, zone and base; roll (the reels), deal; still forms under reduced motion |
| Effects | change the card for everyone (a bot edits its message), a private part (the `private` response), open a dialog (bot dialogs), short notice, notification, go to, open a link, copy, download, text into the composer |
| Surfaces | command (no forms), object, card, link preview (the instance's own), notification, search (core only), status (the `availability` integration) |
| Viewer | time zone, units, theme, reduced motion, screen-reader labels; "only you can see this"; answers limited to a group; a form's closing time |

**New work in the baseline** (in neither client): the graded values;
money, measurement ranges and compounds, bases above 16; maths, key
press, language, country; text with typed holes; structure blocks as
data; pager, carousel; tree, board, timeline, calendar month, agenda,
stat, comparison; every chart but tally bars; waveform; clock, elapsed
timer, live value, expiry; grid board, track, pile, hand, seats, wheel;
stepper, slider, ranked list and rating as inputs; pickers for duration,
interval, recurrence, money, group and role; suggestions asked as the
person types; every surface from "message action" down except those
listed above.

**Exists today and is missing from the catalogue**

| Missing | Where it is today |
|---------|-------------------|
| Phone number and email address values, with their tap actions | `phone` and `email` entities; `ContactChip` |
| The reel: spin through values and land on one | R67's one animation, used by roll, flip, pick and shuffle. The motion list has roll, deal, turn over, count up, shake |
| A block for "picked one of" and "shuffled order" | `chance` pick and shuffle |
| Checklist: a shared tick list with who ticked and when | `tasks`, `task.changed` |
| Poll options: anonymous answers, results after closing, options added by answerers, one answer each | `Form` settings |
| Presence and status as values | `PresenceLight`; status with emoji, text and end |
| A reaction on a card as an input | `BOTS.md` section 7 |
| The call card (started, ended, join) | Calls exist now; the catalogue puts rooms in L |
| The event line ("Ada joined", `/me`) | System notices; actions |
| Thread summary; quote of a range | `ThreadSummaryChip`; `Quote` |
| Quick choices on a picker ("In 30 minutes", "Tomorrow at 09:00") | The schedule sheet |
| Add to calendar, made on the device | `ics.js`, `ics.dart` |
| A banner above a conversation | The broadcast notice, the knock bar |
| A help entry for each command | `/help`; R106 asked for help text |
| The bot card's author line, footer and time, and its seven named colours | `model.Card`, `CardColors` |

**Doubtful, or not expressible generically**

| Item | The problem | Suggested |
|------|-------------|-----------|
| Structure blocks with triggers paired to behaviours | A layout language under another name. R107 dropped card configurations | Containers nest at most three deep; no conditions, expressions or bindings; each block has a fixed list of triggers, not free pairing |
| Text with typed holes "in the viewer's language" | A client can localise the values, not the sentence. Both clients are English-only today | Values localised; the sentence in the owner's language; per-language strings later |
| Every value usable as a command argument | A Go text recogniser for some sixty kinds, a third implementation of each | Go checks structured values by schema. Go text recognisers only for kinds found in free text today plus person, channel, duration, number, choice. Bots send the rest structured |
| Maths "with its result"; money conversion; live values | Three evaluators must agree to the last digit; rates change | The result, the converted amounts and the display precision travel in the value. Clients show, never compute or fetch |
| Audience of any block, enforced by what is sent | One message with different content per reader. It breaks stored-once, each instance's full copy, search text, quotes and export | Baseline: everyone, and one person as a separate unstored private part (exists). Role and time windows later, as whole messages |
| Surfaces outside a conversation (panel, sidebar, profile, status, search, settings) | No conversation, so no owner. They also add entry points: open a panel, suggest, search, preview a link | They belong to the person's home instance only. Each new entry point has a deadline, and no answer means nothing shown |
| Fingerprint; a request; a danger notice | They can imitate Churr's own trust marks | A thirteenth rule: no block may draw the sealed shield, a verified fingerprint, the bot mark or "only you can see this". Dialogs and panels show who made them |
| QR content | Carries a link past the confirmation step | The address is shown as text beside it |
| One level number (rule 3) | Assumes every client has every item at its level. Touch has no hover; a platform may lack one input | The level, plus a short list of known gaps |
| Unknown values survive an edit (rule 5) | The client sends text and marks; it cannot send back what it cannot read | A server rule outside sealed chats: an unreadable value is kept where its text is unchanged |
| SVG "as a picture only" (rule 11) | The web chat draws sheet cells through an SVG `use` today (`chanceart.js`) | Say whether an external `use` counts |
| Tones (rule 8) | Themes have nine keys and no success, warning or danger; bot cards use seven named colours | Derive the tones from the nine keys; map the named colours |
| Named icons (rule 10) | The two sets differ in names (`bell` in the web chat, `notify` in the app) | One list in `testdata/`, both held to it |
| Board, track, seats, hand, wheel | No game uses them yet, the reason rooms are L. No input chooses a placed item. R105 made boards a separate product | Mark them L, or add "choose a placed item" and prove each with one real use first |
| Named options, optional and repeated slots | The matcher is client code, so it is baseline too | Specify it now (section 5.2). An old app sends raw text and Go reads it |
| A frozen bug | A broken block in an app that never updates cannot be fixed | The manifest may say "below level N, do not use block X: use text" |
| About 200 items, twice | The first app people keep waits for all of it | Nothing enters the baseline until one real feature uses it |

## 4. Values on the wire

### 4.1 The envelope [decided: one generic envelope; proposed: this shape]

    Envelope {
      kind     "dice_roll"       the catalogue kind
      level    3                 the lowest catalogue level that reads data in full
      data     {...}             that kind's own message: an object in JSON,
                                 its encoded bytes in protobuf
      alt      Envelope          the generic rendition, for a kind after the baseline
      by       "dice"            the module, manifest or bot that made it;
                                 absent when the composer marked a kind by itself
      source   "parcels/parcel"  the derived type, when there is one
      removed  true              an edit changed the span; nothing was resolved
    }

- R106 said "module, kind, the module's data". Proposed: `kind` names the
  **catalogue kind**, so a client can draw a value from a module it has
  never heard of.
- A client draws `data` when it knows the kind and `level` is not above
  its own; otherwise `alt`; otherwise the text.
- **Typed data per kind** [unconfirmed]: each kind has a protobuf
  message defined with the other schemas (`tools/openapi-gen`). The
  alternative, the open `Value` union, needs no generator work but is
  larger and has no schema.
- Attached in two places and nowhere else:
  - a span: `Entity {type: "object", at, len, object: Envelope}`. The
    span's characters are the text.
  - a message: `Message.view`, one per message. `Message.text` is the
    text, escaped by the host so it reads the same everywhere.
- **Replaced**: `Message.chance`, `MessageInput.chance`, `Chance`,
  `ChanceInput`, `DiceRow`, `DiceTerm`, the four enums, `Entity.terms`,
  `total`, `removed`, `ENTITY_TYPE_DICE`, `OBJECT_HINT_KIND_DICE`. Their
  numbers are retired in the number lock. No conversion path [decided:
  unreleased].
- **Stored**: the envelope as it is, in the entities column for spans
  and a new `view` column for messages. Never anything drawn. Search
  indexes the text with no special case. `fallback` stays for forms.
- The typed entity fields of instants, measurements, numbers, code
  points, colours, phones, emails, places and references move into
  envelopes one stage at a time (section 10). Mentions, links, emoji,
  channel links and tasks stay entities: they carry permission and
  notification meaning.

### 4.2 Data is not trusted [unconfirmed]

One validator per kind in each of Go, JavaScript and Dart, tested
against `testdata/types/<kind>.json`: schema, counts, lengths, ranges.

- Text is drawn as text. No markup.
- A picture is named by hash or file identifier, never by address.
- An address is `https` only and opens through the usual link step.
- A failed check falls back one step. It is not an error to the reader.

### 4.3 Derived types [decided: pattern and static recognisers with capture groups; proposed: mapping]

A manifest, a bot or a built-in module declares a type with no code:

- **Pattern**: a regular expression with named captures.
- **Static**: a list of words or phrases, each with a row of values.

Each maps its captures **by plain substitution** into the fields of the
`reference` kind: a label, fields and a link. No formula language.

The existing reference patterns become derived types [unconfirmed]:
they are the manifest an admin or a channel's managers edit by hand.

**The expression subset** [unconfirmed: "what Go's regexp accepts, with
length caps"; proposed: the detail]. Written in Go's syntax and checked
by Go when installed; one that fails is refused.

| Allowed | Refused |
|---------|---------|
| Literals, classes, `\d` `\w` `\s`, alternation | Anchors: the object edges of `objects.go` decide where a match may stand |
| Groups, and named captures `(?P<name>...)`, at most 8 | Back-references and look-around |
| `?` `*` `+` `{n,m}` with m at most 32 | A repeat applied to anything that itself repeats |
| `(?i)` for the whole pattern | Flags inside; classes outside ASCII, to begin with |

Caps: 200 characters, a match of at most 128, 50 derived types a
manifest, 2,000 entries a list. The clients run backtracking engines, so
refusing nested repetition is what keeps a pattern fast, and
`testdata/types/patterns.json` holds the three engines to the same
matches.

### 4.4 Recognition

**In the composer** [unconfirmed reading: "marked as objects on the way
in"]. Today's behaviour stays (`docs/CLIENTS.md`, "Objects in the
composer"). What changes:

- The readings come from the conversation's manifest (section 7.2).
- **Every** mark is sent: `marks: [{start, end, kind, source, data}]`,
  with `plain` for a range kept as typed. This replaces `object_hints`
  and `instant_hints`.
- Recognition runs off the typing path with a time budget (proposed:
  8 ms a pass). A pattern that overruns is switched off for the session
  and reported [unconfirmed].

**What the owner checks** [unconfirmed]. For each mark:

1. The range lies inside one run of plain text, on the object edges.
2. The kind or derived type is on in this conversation.
3. Where a Go recogniser exists, it reads the range again and its data
   **replaces** the client's. Where none exists, the data is checked
   against the schema and stands as the author's statement.
4. If a built-in module claims the kind for resolving on send, it is
   called and its value stored (inline dice).

A mark that fails is dropped without an error. A sender that sends no
`marks` at all (a webhook, the importer, a plain API client) is scanned
with the Go recognisers, as today [proposed].

**Edits**: nothing is resolved on an edit. A resolved object is kept
where the same text stands at the same place among that kind's objects;
otherwise it is marked `removed`. This is `keepRolls` made general.

**Overlaps and precedence** [proposed]. Objects never overlap or nest;
text is read left to right. At one position, in order: the author's
choice; the channel's derived types before the instance's; the admin's
order (section 6.5); the order within a manifest. The default order is
today's, so `#123456` still reads as a reference where a pattern says so
and a colour elsewhere.

**Sealed chats** [unconfirmed]. No modules, no bots, nothing resolved on
send. The composer marks catalogue kinds and derived types; the values
travel inside the sealed payload; the reader's client validates. Sealed
entities already travel this way (`sealedEntities`).

## 5. The composer and command forms

The composer is **one marked-text editor** [unconfirmed]. In a message
it marks objects; in a command line it marks the slots of the form. The
marks are worked out from the text after every change. The only state
kept beside the text is what the author chose, and a choice lasts while
its text stands, as `track` in `compose.js` does today.

### 5.1 Forms [decided]

A command declares several forms. A slot has a name, a hint and a kind
from the catalogue or a derived type; `text` takes anything.

    /remind [instant] [message]
    /remind [instant]
    /remind [message]

### 5.2 Matching

The same algorithm in Go, JavaScript and Dart, held to
`testdata/commands/forms.json`. It is baseline: an app that never
updates keeps the matcher it shipped with.

| Rule | Status |
|------|--------|
| The name is looked up in the conversation's command table (section 6.5) | decided |
| Forms are tried in declared order; the first whose slots are all filled wins | unconfirmed |
| A `text` slot may only be last, and takes the rest of the line as typed | unconfirmed |
| A form whose `text` slot would hide a typed slot of a later form is refused when declared, so the typed reading wins by construction | unconfirmed ("typed reading wins"); the check is proposed |
| White space separates slots. A slot's recogniser is asked for the **longest** value from the current place that ends at white space or the end | proposed |
| Quotes force a boundary: `"..."` is exactly one argument | unconfirmed |
| No backtracking between slots; if the rest does not fit, the next form is tried | proposed |
| Named options (`name:value`) may stand anywhere after the name and are taken out first. An optional slot is a second form. A repeated slot may only be last | proposed, to meet section 3.8 |
| A form may name a minimum level. A client below it sends the line as raw text | proposed |
| If no form fits, nothing is sent and the composer shows the forms | proposed |

The invocation is sent **structured**: the command, the form, each
slot's value and the raw text. The owner checks each value again. A
plain API client or an old app sends raw text and the Go matcher reads
it by the same rules [unconfirmed].

### 5.3 What the author sees [proposed; the owner's words ended in question marks]

| Input | What happens |
|-------|--------------|
| `/` then a name | The command menu, as today |
| Name and a space | A strip under the line shows the current form as slot chips with hints, and "1 of 3" when there are more forms |
| Typing | The slot being filled is highlighted. Text that fills a slot is marked as a chip; it is still text and can be edited in place |
| Tab, Shift+Tab | The next or previous slot. On an empty slot whose kind has a picker, Tab opens it |
| Up, Down | Switch form, wrapping, while the caret is on the command's first line and no list is open. The text stays and is read again |
| Tap or click a slot chip | Go to that slot and open its picker |
| Tap or click "1 of 3" | The next form. No hold [unconfirmed] |
| Paste, or an edit in the middle | The whole line is read again in one pass. A hand-picked form is kept if the line still fits it |
| Enter | Sends when the form is complete. Otherwise moves to the first empty slot and says what is missing |
| Screen reader | Slot and form changes are announced |

## 6. Three ways to extend, one contract

### 6.1 The contract [decided, R108: "the same types"; proposed: the shape]

| | Built-in module | Manifest | External bot |
|-|-----------------|----------|--------------|
| What it is | Our Go, reviewed, compiled in | Data only | A program outside the process |
| Arrives by | A release of Churr | An admin installs it at run time; no rebuild | An admin creates the bot and approves its manifest |
| Runs | In the owner's server | On the device, checked by the owner | At the bot's home instance's door |
| Declares | Commands as forms, derived types, claims, pictures, help | Derived types, word lists, pictures, decks and layouts for kinds that take them, command presets | Commands as forms, derived types, the surfaces and effects it will use |
| Receives | Invocation, press, resolve on send | Nothing | Invocation, press |
| Answers with | Values and effects | Nothing | Values and effects |
| Store, randomness | The host's | None | Its own |

No third-party code runs in any server [decided]. There is no build
tool, no generated main and no public package for outsiders.

### 6.2 Built-in modules [decided: compiled in, ours; proposed: the shape]

Three entry points, the same a bot has plus one:

| Entry | Called when | Gives back |
|-------|-------------|------------|
| Command | One of its commands is invoked, with the form and typed values | A card, a private part, or nothing |
| Press | An input on a card **it made** is used, or a timer on that card fires | An update to that card, another effect, or nothing |
| Resolve | A marked object of a kind it claims is sent | The value for that span. Built-in only: it sits in the posting path |

The interface is narrow and under `internal/`. Names are the shape, not
a promise:

    type Module struct {
        Name, Title string        // [a-z0-9-]{2,32}
        Commands    []Command     // name, other names, forms, summary, help
        Types       []DerivedType
        Claims      []Claim       // kind and the pattern that marks it
        Pictures    fs.FS
        Handler     Handler
    }
    type Handler interface {
        Command(ctx context.Context, h Host, in Invocation) (Reply, error)
        Press(ctx context.Context, h Host, in Press) (Reply, error)
        Resolve(ctx context.Context, h Host, in Marked) (Value, error)
    }

`Host` gives the verified actor and conversation, `crypto/rand`, the
clock and timers, the key-value store, and nothing else. A reply is
values and effects from section 3.7. The host refuses a card with no
text, validates every value and stamps `by` itself.

**The store** [decided: namespaced key-value, no module migrations].
One table: module, scope (instance, conversation, account), key, bytes.
Proposed caps: 64 KB a value, 1 MB a conversation, 64 MB a module.

**Defensive, though the code is ours** [proposed]: a panic is recovered,
the module switched off and the admins told; a handler has a deadline
(2 s for a command or resolve, 5 s for a press).

**The tree and the layers** [proposed]

    internal/modhost/        the contract, registry, dispatch, checks
    internal/modules/dice/   one package per module
    internal/modules/cards/ ...

| Rule | Why |
|------|-----|
| `modhost` is layer 1: it imports `model`, `markdown`, `instant`, `wirepb` and never `store` or `chat` | Both `churr` and `churr-hub` must host modules (section 8). Storage, posting, clock and randomness reach it through small interfaces |
| `modules/*` import `modhost` and `model` only | A module cannot reach a database or a socket |
| Only `server` and `hub` import `modules/*` | No feature leans on a module being there; `server` connects them, as it does every feature |
| `modules/*` make no outbound connections | Added to the outside-connections test |

`imports_test.go` needs two new layer entries and these rules. `cmd/`
does not change.

### 6.3 Manifests [decided, R108: "a simple manifest"; proposed: the contents]

A file of data and its pictures, installed in the admin console.

| May hold | Notes |
|----------|-------|
| Derived types | Section 4.3 |
| Pictures and sprite sheets | Rule 11. Addressed by SHA-256. Proposed caps: 1 MB a file, 16 MB a manifest |
| Decks and layouts | Data for kinds and built-in modules that take them: a deck of cards for `/draw`, a spread for `/tarot` |
| Command presets | A name that stands for another command with slots filled: `/fireball` is `/roll 8d6` |

It is checked whole when installed (schema, the pattern subset, picture
types, caps) and stored under its hash. It holds nothing that runs.

### 6.4 External bots [decided, R108; proposed: the changes]

A bot keeps what it is today: an account with a token on one instance,
on the ordinary API. What changes in `internal/bots` and `docs/BOTS.md`:

| Today | Becomes |
|-------|---------|
| `setCommands` takes name, description and a `hint` that is not parsed | The bot submits a **manifest**: commands as forms, derived types, the surfaces, effects and kinds it will use |
| Commands take effect at once | An admin approves the manifest. One that asks for the same or less applies at once; one that asks for **more** (a command, a surface, an effect, a kind, a pattern) waits for approval again |
| `command.invoked` carries `text` and entities | It carries the form and the typed values too; `text` stays |
| Presses exist only as `form.answered` on a `bot` form of up to five buttons | A press on any input of a card the bot made |
| Four response types: `message`, `private`, `dialog`, `none` | Values and the effects of section 3.7. The four map onto them |
| Only a bot may post `cards`, of one fixed shape | A bot posts a `view` of any kind it is approved for. The bot card becomes blocks (question 9) |
| Commands, dialogs and `bot` forms stop at its own instance in a shared channel | The owner admits the bot to the conversation and routes invocations and presses to its home instance (section 7.3) |

A bot may declare derived types. It may not claim a kind for resolving
on send [proposed]: that sits in the posting path. Its interaction and
rate limits stay as they are.

### 6.5 Switches, precedence and renames [decided]

- **Per instance**: the admin console lists built-in modules, installed
  manifests and bots, each with what it declares and an on/off switch.
- **Per channel**: its managers switch off what the instance has on. A
  channel cannot switch on what the instance has off.
- **Off means**: its commands leave the table, its types are not marked,
  its presses answer `feature_off`, and its values are served as their
  text. They stay stored, so switching it on again brings the cards back
  [proposed reading of "plain text ... only in case the plugin gets
  turned off or removed later"].
- **Precedence**: the admin orders the sources of commands. Today the
  order is fixed and a community command hides a replaceable built-in
  (R72). Proposed: one list on the admin's "Commands" page; the same
  order settles object precedence. Protected built-ins stay protected.
- **Renames**: the admin gives a command another name. `GET /commands`,
  now with forms, carries the final names and the clients parse by it.
  Help text writes the name as `{cmd}`.

## 7. The owner rule

### 7.1 The rule [decided for hub channels, R109; proposed as general]

**A conversation runs its owner's built-in modules, and only its owner
admits bots and manifests.** Peers send invocations and presses to the
owner. The owner validates every value in its conversations. Nobody
else's module output is accepted there.

| Conversation | Owner | Status |
|--------------|-------|--------|
| Local | The instance | fits today |
| Channel shared over a direct link | The instance that shared it (`OwnerServerID`); it can be handed over (`stream.owner_changed`) | Needs the link changes of 7.3 |
| Channel on a hub | **The hub** | decided, R109; needs section 8 |
| Direct or group chat across instances (`adhoc`) | The instance of the member who first added someone from elsewhere; a hub carries it without knowing what it is (R59) | Section 7.4 |
| Sealed chat | The starter's instance | No modules, no bots |

### 7.2 What crosses instances [proposed]

| Thing | Direction | Checked by |
|-------|-----------|-----------|
| The conversation's manifest: what is on, the commands and forms, derived types, pictures by hash, the owner's level | Owner to peers, then to their clients | Each peer runs the Go pattern check before its clients see it |
| Values, on the text they belong to | Inside messages | The owner; every receiving instance; the client |
| Invocations and presses, with the actor | Peer to owner | The link's signature shows the actor's home instance vouches for them; the owner checks membership and level |
| An invocation or press for a bot | Owner to the bot's home instance | The owner checks the bot is admitted and the command is in its approved manifest |
| Pictures, by hash | Fetched through one's own instance | Hash and size, as files are today (section 15) |

Never: code, store contents, module output from anyone but the owner. A
press is addressed to a message, and the owner sends it only to whatever
made that message. A peer may always refuse a kind **locally** and serve
its text.

A client fetches its own instance's manifest at start-up [decided] and
caches it by hash; a conversation owned elsewhere names its owner's.

### 7.3 What changes on the link [proposed]

For direct links and hubs alike. Section 8 is what the hub needs first.

| Change | Detail |
|--------|--------|
| A new frame to the owner: `module.invoked` | From the actor's home instance. It carries the command, form, values and raw text, or a press and its message. It posts nothing and is not kept in the log |
| The acceptance rule for `message.created` (section 12) gains a row | Accepted **from the owner** when it carries a view with `by` set and names a `module.invoked` whose origin is the home of `author_id`. The author is the person who asked |
| `view.updated`, owner only | A card changes for everyone |
| `object.resolved`, owner only | The author's home sends `message.created` with the claimed span unresolved; the owner follows with the value. The author sees "working" until it lands. For the owner's own members it is resolved in the posting path, as today |
| `conversation.manifest_changed`, owner only | What is on, and the manifest's hash. On a hub it is part of the signed record |
| The owner forwards a bot's invocation to the bot's home, and a private answer goes back to the asker's home | The bot's own messages still come from its home, under the unchanged rule |
| The receiver's check of `chance` goes | Receivers validate values against the kind, and that module output came from the owner |

What this costs:

- A round trip for every invocation from a peer.
- While the owner cannot be reached there are no module commands or
  presses. Proposed: fail with `unavailable` after a few seconds; do not
  queue. A roll that lands an hour late is worse than none.
- After a handover the conversation has the new owner's manifest. Old
  values still draw; their buttons work only if the new owner has what
  made them.
- Commands differ by conversation: `/roll` may be absent or renamed in a
  channel another instance owns.
- A peer's patterns run in my composer. The Go check on arrival, the
  caps and the time budget stand between a careless owner and my typing.

### 7.4 Direct and group chats across instances [proposed]

R109 is about the channels a hub owns. A direct or group chat across
instances has an arbitrary owner (whoever first added someone from
elsewhere), nobody manages it, and a hub carries it without holding its
key (R59). Two ways:

- (a) The owner rule as it stands. One rule everywhere; but which
  instance's commands a chat has depends on who added whom, and over a
  hub the chat loses its commands when that one instance is away.
- (b) As chance works today: each member's home instance runs its own
  built-in modules for its own members; every value carries and shows
  where it was made; no per-chat switches; a bot's interactive side
  stays on its own instance. This needs no new link event.

**Recommended: (b)**, for these chats only. Said plainly: under (b) a
modified instance can fake its own members' rolls. The receiver checks
the shape, not the fairness; the label says whose word it is; and
joining instances are trusted by the owner's own model (R95).

## 8. What must change for the hub to act as owner

R109: "The hub owns a shared channel. There's no keeping things secret
from it. It's trusted as the owner." What is built keeps content from
the hub. That is the defect; this section is the repair. Channels shared
over a direct link keep their owner instance and need none of it.
[decided: the hub reads and acts as owner; unconfirmed: the hub makes
and holds the keys and key-makers go; proposed: the rest.]

### 8.1 Keys

| Today | Becomes |
|-------|---------|
| The hub never holds a stream key (section 10) | The home hub **makes** each epoch's key for a channel it owns and keeps every epoch's, sealed at rest as instances keep `stream_keys` |
| The hub names a key-maker instance on every change to the server list; the 30-second fallback; `early_epoch`; the record's `key_maker` and `makers` (section 23a) | All removed. The hub sends `key` to each instance in its signed list and starts the epoch itself |
| An instance takes a key only from the instance the record names | Only from the home hub |
| Moving a channel onto a hub hands over no key (section 21a) | The move ends with the hub starting a new epoch with a key of its own |
| A hub of a cluster "holds no stream key and no plaintext" (section 23b) | True of an edge hub still. The **home** hub holds them; handing a channel to another hub of the cluster starts a new epoch there |

The hub needs a key that direct messages can be sealed to. Today it has
a signing key only.

### 8.2 History and the joining snapshot

Today the key-maker of the epoch an instance joins at sends its history
(section 13). A snapshot is state, not a replay: messages as edited,
reactions, ticks, accounts, members, up to 90 days. The hub keeps a
30-day log of envelopes and no such state.

- (a) The hub names a **history sender** in the record (the
  longest-standing member instance), and a joiner takes an unasked
  snapshot only from it. One field replaces `key_maker`.
- (b) The hub keeps the conversation's state and sends the snapshot
  itself. The hub then needs a message store and becomes most of an
  instance.

Recommended: (a) now. It keeps the hub small, and the hub can still
check what the snapshot carries.

### 8.3 Reading, validating and acting

- The hub opens every envelope of a channel it owns before taking it
  into its log, validates the values in it (section 4.2) and refuses
  one that fails with `invalid`.
- The hub becomes an **origin**: it makes envelopes of its own (module
  output, `view.updated`, `object.resolved`), stamped and signed with
  the hub's key. Today it signs records only.
- Section 12's rows change as in 7.3, with "the owner" read as the home
  hub. Everything else in the table stands.
- What is on, the admitted bots and the applying manifests go into the
  signed record. Managers ask through `channel_action`; the operator
  sets what the hub offers at all.
- A bot is still an account on a member instance. The hub admits it and
  forwards its invocations there. The hub runs no bot.
- A manifest admitted to a hub channel is stored by the hub and served
  to member instances by hash.

### 8.4 How `churr-hub` gets the module code

`STRUCTURE.md` section 3 says "a hub must not be able to do what a
server does" and limits its imports. Proposed: the hub may also import
`modhost`, `modules/*`, `markdown`, `instant`, `unicodename` and
`wirepb`, which is the reason `modhost` sits at layer 1 and reaches
storage through an interface (section 6.2). It still may not import
`store`, `chat`, `perm`, `people` or `api`. The hub supplies the store,
clock and randomness from its own SQLite file. `link/wire` already
links `wirepb` into the hub; the hub would now call it.

R108 holds for the hub as for an instance: built-in modules only.

### 8.5 What the hub stores, and what its operator can read

| Stored | Today | After |
|--------|-------|-------|
| Channel records | Yes | Yes, with switches, bots and manifests |
| The envelope log, 30 days | Ciphertext it cannot open | The same bytes, which it can now open |
| Stream keys | None | Every epoch of every channel it owns |
| Module store; the cards its modules made | None | Yes |
| Admitted manifests and their pictures | None | Yes |
| Files | None: bytes travel between instances in sealed direct messages (section 15) | Unchanged, unless the owner wants the hub to check files too (question 4) |

The operator of a hub can read every message of every channel the hub
owns, as it passes and for as long as the log keeps it. That is the
decision; the documents must say it in those words. Direct and group
chats and sealed chats carried by the hub stay closed to it (R59).

### 8.6 What is superseded

| Text | What happens to it |
|------|--------------------|
| R56: "Message content, files and everything inside envelopes stay unreadable to the hub; it never holds a stream key" | Superseded by R95 and R109. The rest of R56 stands |
| `LINK-PROTOCOL.md` section 1: the rows for hub, owner server, hub-owned channel, key-maker, time reference | Rewritten; key-maker removed |
| Section 2, "a hub reads those": the signed part only | A home hub reads the whole envelope |
| Section 10, keys and epochs | The hub makes keys; "takes the owner's word for a new epoch" goes |
| Section 12, who may say what | The rows of 7.3 |
| Section 13, history on joining; an envelope under a key from before a server joined | Section 8.2 |
| Section 17, "it cannot read" ephemeral frames | True only of what it does not own |
| Section 21a, the move onto a hub; section 21, handing over | A new epoch made by the hub |
| Section 23a: the trust paragraph, the record's `key_maker` and `makers`, "Keys: the key-maker" | Rewritten |
| Section 23b: "holds no stream key and no plaintext" | Edge hubs only |
| Section 24, threats: "A malicious hub: read content: stopped" | "Not stopped for channels it owns. That is the trust placed in a hub" |
| `STRUCTURE.md` section 3, the hub's import rule; `imports_test.go` `onlyThese` | Section 8.4 |
| Hub tests that hold it blind (`internal/hub/*_test.go`) | Rewritten with the code |

### 8.7 Simpler, and harder

**Simpler**: no key-makers, no fallback timer, no passed-over epochs,
no bookkeeping of who is owed a key. One validator for every channel on
a hub. One place decides what is on. A module command needs only the
hub to be up, not any one member instance.

**Harder**: the hub is a content-bearing server. A broken-into hub
gives up every channel it owns, where before it gave up names and
timing. It holds keys that must be guarded. It decrypts and validates
every envelope, so the measured capacity of a hub no longer stands and
must be measured again. It carries recognisers and module code and has
a catalogue level of its own; an instance can be newer than its hub.
Handing a channel between hubs of a cluster must end in a new epoch.
The threat section has to be rewritten honestly.

## 9. Worked examples

### 9.1 Dice, the first module

**Scope**: `/roll`, `/dice`, inline `[[...]]`, rows separated by commas,
the N-gon and coin drawing. Recommended: `/flip` moves with it, since a
coin is already a two-sided die in `diceshape.js` (question 6).

| The module declares | |
|---------------------|-|
| `/roll [dice]`, `/roll` | `dice` is a derived pattern, `[0-9dDkKhHlLaAvVisS+, -]{1,100}`; no argument is `1d20`, as today |
| `/dice [dice]`, `/dice` | A bare number is that many d6 (`DiceD6`) |
| Claim | `[[`, the same pattern, `]]`: kind `dice_roll`, resolved on send |
| Picture | The `dice-cube` sheet: SVG, WebP and its index |

**Kind `dice_roll`** is today's shape: `dice`, and `rows` of `{dice,
terms, total}`, each term `{sign, count, sides, keep, keep_n, rolls,
dropped, value}`, with today's caps (`MaxDiceRows` 8, `MaxDiceTerms`
10, `MaxDice` 100, `MaxDieSides` 1000). The text is today's fallback:
"Rolled 2d6+3: [4, 1] + 3 = 8". Kept: reels spin once on live arrival,
a d6 comes from the cube sheet, other dice are an N-gon up to 30 sides
and a circle beyond, a d2 is a coin, dropped dice are struck, reduced
motion shows the result at once. As a baseline kind it needs no `alt`.

**For people**: the grammar lives once, in Go, instead of three times.
The price: the composer cannot refuse a bad expression before sending.
`/roll banana` comes back as the server's sentence, and `[[banana]]` is
offered as dice and then stays text.

**Server files**

| File | What happens |
|------|--------------|
| `tools/openapi-gen/schemas.py`, `proto.py`, `link.py` | `Envelope`, the `dice_roll` message; dice leave `Chance`, `ChanceInput`, `Entity` |
| Written by `tools/protogen.sh`: `proto/churr/v1/churr.proto` and its number lock, `docs/openapi.yaml`, `internal/wirepb/schema_gen.go`, `internal/link/wire/schema_gen.go`, `internal/wirepb/testdata/churr.binpb`, `internal/link/wire/testdata/link.binpb`, `testdata/wirepb/bodies.jsonl`, `frames.jsonl`, `app/lib/core/wire/gen/churr.pb.dart`, `churr.pbenum.dart`, `app/lib/core/wire/schema.g.dart` | Regenerated |
| `internal/model/chance.go` | The dice half moves to `internal/modules/dice/` (`ParseDiceRows`, `ParseDice`, `diceTerm`, `DiceD6`, `DiceString`, `RollDice`, `droppedDice`, `writeDiceRow`, `DiceTerm`, `DiceRow`, the dice limits). The rest stays until stage 4 |
| `internal/model/chance_test.go`, `testdata/commands/dice.json` | The dice cases move with it; once the client parsers go, only Go reads them |
| `internal/model/message.go` | `Entity` loses `Terms`, `Total`, `Removed` and gains `Object`; `Message` gains `View` |
| `internal/model/scheduled.go` | `roll` and `dice` leave `BuiltinCommands`; `ObjectHints` becomes marks |
| `internal/chat/dice.go` | `InlineDice` goes; `diceMode` and `keepRolls` become the general rules for resolved objects |
| `internal/chat/chance.go`, `messages.go`, `resolve.go`, `instants.go` | The roll path calls `modhost`; `cryptoIntn` backs the host's randomness |
| `internal/markdown/objects.go`, `markdown_test.go`, `testdata/markdown/dice.json` | `dice()`, `DiceSource` and the three dice modes go; the resolver asks a claim source |
| `internal/api/messages.go`, `bots.go`, `dice_test.go`, `commands_r67_test.go`, `commands_clash_test.go` | `runCommand` takes a form and values and runs module commands |
| `internal/link/events.go`, `outbound.go`, `snapshot_test.go` | `wireMessage` carries `view`; the roll leaves `chance` |
| `internal/sched/sched.go` | Its refusals name `view` |
| `internal/store/sqlstore/messages.go`, `helpers.go`, `janitor.go`, `thread_move.go`, `storetest/messages_marks.go`, `storetest/bots.go`, a migration (next free: 0021), `docs/schema.sql` | The `view` column; the module store |
| `imports_test.go`, `docs/STRUCTURE.md` | Section 6.2 |
| `testdata/commands/commands.json`, `parse.json`, `help.json`; `tools/help-gen/gen.py` and its outputs `shared/helptext.js`, `app/lib/core/commands/help_text.dart`; `internal/model/help_test.go` | `roll` and `dice` leave the three tables; their help comes from the module; the test checks the modules' help instead |
| `tools/art-gen/pack.py`, `testdata/art/sheets.json`, `shared/chance/sheets.js`, `internal/web/compress_test.go` | `dice-cube` is packed into the module (stage 4; bundled until then) |
| New: `internal/modhost/`, `internal/modules/dice/`, `testdata/types/dice_roll.json`, `testdata/commands/forms.json` | |
| Docs: `MESSAGE-FORMAT.md` 1.2, 3.2, 10; `FEATURES.md` 12.3, 12.4; `CLIENTS.md`; `LINK-PROTOCOL.md` 11, 12; `ART.md`; `SEALED.md` | |

**Web chat files** (`internal/web/static/`)

| File | What happens |
|------|--------------|
| `shared/message/diceshape.js` | Moves to `shared/types/dice-roll/` unchanged |
| `shared/objects/dice.js` | Becomes the kind's chip |
| `shared/message/chance.js` | The roll half (`rowSlots`, the roll branches of `slots`, `headline`, `spokenResult`, `renderChance`, the reels) becomes the kind's card; the rest imports the reels back until stage 4 |
| `shared/message/chanceart.js` | `paintDie` and the cube go with the kind; the sheet loader becomes the picture loader |
| `shared/md/dice.js` | Deleted |
| `shared/md/objects.js`, `render.js`, `entities.js`, `md.css` | `dice` leaves `OBJECT_KINDS`; `case 'dice'` becomes `case 'object'`, drawn by the catalogue |
| `shared/objects/compose.js`, `rows.js`, `words.js` | Readings come from the manifest; all marks are sent |
| `shared/commands.js`, `chat/commands.js` | `roll` and `dice` leave `COMMANDS`, `CHANCE`, `REPLACEABLE`, `GROUP_OF`, `STRINGS`, `parse` and `chanceInput`; module commands go to `runCommand` |
| `chat/composer.js`, `chat/store.js`, `chat/env.js`, `chat/sealed/view.js`, `chat/chat.css`, `shared/message/message.js` | The `view`; the live spin; the fallback steps |
| `dev/chance.html`, `dev/chance.js` | The contact sheet follows |
| New: `shared/types/catalogue.js` (kind to validator, chip, tooltip, dialog, card; the level) | |
| `e2e/commands.mjs`, `objects.mjs`, `markdown.mjs`, `help.mjs`, `objects-web.mjs`, `chanceart.mjs`, `chancereveal.mjs`, `chat.mjs`, `run.sh` | Follow; the first four run under Node without a browser |

**App files** (`app/`)

| File | What happens |
|------|--------------|
| `lib/ui/message/dice_shape.dart`, `dice_view.dart` | Move to `lib/ui/types/dice_roll/` |
| `lib/ui/message/chance_view.dart`, `chance_art.dart`, `object_rows.dart` | The roll half (`_rowSlots`, the roll branches, the reels, `DieStrike`) becomes the kind's card |
| `lib/core/markdown/dice.dart` | Deleted |
| `lib/core/markdown/objects.dart`, `lib/core/objects/compose.dart` | As the web's |
| `lib/core/commands/commands.dart`, `chance_live.dart` | `roll` and `dice` leave `builtinCommands`, `chanceCommands`, `chanceRequest` |
| `lib/core/models/message.dart`, `lib/core/api/messages.dart`, `lib/core/sync.dart`, `lib/core/sealed_client/content_view.dart`, `lib/state/composer_controller.dart` | `view`; `postMessageChance` becomes an invocation |
| `lib/ui/message/markdown_view.dart`, `message_tile.dart`, `lib/ui/screens/conversation/chat_commands.dart`, `command_dialogs.dart` | Draw through the catalogue |
| `lib/l10n/app_en.arb` and the generated `app_localizations*.dart` | Dice strings move with the kind |
| `assets/chance/dice-cube.json`, `dice-cube.webp`, `pubspec.yaml` | Leave the bundle at stage 4: fetched by hash and cached; the die shows its number until then |
| New: `lib/core/types/` (schemas, validators, recognisers; no Flutter), `lib/ui/types/` | |
| `test/core/dice_grammar_test.dart` | Deleted |
| `test/core/chance_live_test.dart`, `help_test.dart`, `markdown_corpus_test.dart`, `objects/objects_compose_test.dart`, `test/state/conversation_state_test.dart`, `test/ui/chance_art_test.dart`, `chance_view_test.dart`, `chat_commands_test.dart`, `dice_shape_test.dart`, `irc_commands_test.dart`, `object_view_test.dart` | Follow |

### 9.2 `/remind` with its three forms

`/remind` is a core command and stays one: it needs the scheduler.
Forms belong to the command table, so core commands use them too.

Today it is `/remind [target] when | what`, read by each client
(`shared/remind.js`, `app/lib/core/commands/remind.dart`,
`testdata/commands/remind.json`) and carried out with `createScheduled`.

| Form, in declared order | Meaning | Status |
|-------------------------|---------|--------|
| `/remind [instant] [message]` | Remind me then, with these words | decided; the order is proposed |
| `/remind [instant]` | Remind me then, about this conversation or the message replied to | decided; meaning proposed |
| `/remind [message]` | No time typed: Tab or Enter opens the instant picker; the chosen time is written in and the line becomes the first form | decided; meaning proposed |

| Typed | Form | Slots |
|-------|------|-------|
| `/remind tomorrow at 9 send the report` | 1 | instant "tomorrow at 9"; message "send the report" |
| `/remind in 20m` | 2 | instant "in 20m" |
| `/remind call mum` | 3 | message "call mum" |
| `/remind tomorrow` | 2 | Down switches to form 3: a reminder that says "tomorrow" |
| `/remind friday at 9 we ship` | 1 | The longest instant is "friday at 9"; message "we ship" |
| `/remind "friday" at 9 we ship` | 1 | The quotes end the instant at "friday"; message "at 9 we ship" |

- The instant kind becomes the one reader of time phrases. Today each
  client has two and Go has none; raw invocations need the phrase
  grammar in Go, a third implementation of
  `testdata/instants/phrases.json`.
- The three forms have no place for today's target (`me`, `@person`,
  `<#channel>`), for repeats, or for the `|` (question 7).
- `remind.js`, `remind.dart` and `remind.json` become form declarations
  and vectors in `forms.json`.

### 9.3 A pattern object from a third party

A manifest, installed by an admin in the console. No rebuild, no code:

    {"name": "parcels", "title": "Parcel numbers", "version": "1.0.0",
     "types": [{
       "id": "parcel",
       "pattern": "PKG-(?P<region>[A-Z]{2})(?P<num>[0-9]{8})",
       "into": "reference",
       "fields": {"label": "Parcel {region} {num}",
                  "link": "https://track.example.org/p/{region}{num}",
                  "note": "Region {region}"}}]}

1. The console checks it and shows what it declares: one pattern, one
   address. The admin switches it on.
2. The pattern reaches clients in the manifest. Typing `PKG-AU12345678`
   offers "Parcel AU 12345678" and "Plain text".
3. The mark is sent. The owner matches the range again with Go's regexp,
   fills the fields by substitution (captures are encoded into the
   address, so they cannot change its host), validates against
   `reference` and stores `{kind: "reference", by: "parcels", source:
   "parcels/parcel"}`.
4. A reader sees a chip; the tooltip shows the note and "from Parcel
   numbers"; a click opens the address through the usual link step.
5. It works in a sealed chat: the sender's client substitutes and the
   reader's validates.
6. Switched off or removed, the message still says `PKG-AU12345678`.

A manifest cannot fetch the parcel's status. For that the author writes
a **bot** that declares the same type and a "Where is it?" action in
its manifest; an admin approves it; a press reaches the bot at its home
instance and it answers with a `fields` value.

## 10. Order of work [proposed]

One builder, one step at a time. Each step names the one test that shows
it; browser tests and full suites are left to the GitHub build.

| Stage | Step | Shown by |
|-------|------|----------|
| 0 | The GitHub build is green | The build |
| 1 Envelope | 1a `Envelope` and `dice_roll` in the schema; regenerate | `internal/wirepb` vectors test |
| | 1b `Message.view`, the `object` entity, the `view` column; a roll is stored as text plus view; the built-in code still rolls | One `storetest` case; `internal/api/dice_test.go` |
| | 1c Web: `shared/types/catalogue.js` and `dice-roll`; the cheap day-one rules (three-step fallback, unknown pieces, the level, "needs a newer app", unknown values kept) | `e2e/objects.mjs` under Node; a new `e2e/types.mjs` |
| | 1d App: the same | `test/ui/dice_shape_test.dart`; a new `test/core/types_test.dart` |
| | 1e Link carries `view`; the receiver validates | `internal/link/snapshot_test.go` |
| 2 Built-in modules | 2a `modhost` and the layer rules | `imports_test.go` |
| | 2b `internal/modules/dice` | Its test on `dice.json` |
| | 2c `runCommand` runs `/roll` and `/dice`; the resolver asks the claim; client parsers deleted | `internal/api/commands_r67_test.go`; `markdown_test.go` |
| | 2d The store, quotas, panic recovery, deadlines | One `modhost` test each |
| | 2e Switches per instance and per channel; the admin list; off serves text | One `internal/api` test |
| | 2f The command table with forms, precedence and renames | `internal/api/commands_clash_test.go` |
| 3 Composer | 3a The form matcher in Go, JS and Dart on `forms.json` | One test per language |
| | 3b All marks sent; the server checks instead of scanning | `markdown_test.go`; `e2e/objects.mjs` |
| | 3c The form strip, Tab, arrows, taps (web, then app) | `e2e/commands.mjs`; `test/ui/chat_commands_test.dart` |
| | 3d `/remind` as forms; time phrases in Go | `internal/instant` test on `phrases.json` |
| | 3e The client's level sent on connecting; the admin view | One `internal/api` test |
| 4 Cards and tarot | 4a Pictures by hash; `dice-cube` leaves the bundles | One `internal/api` test; `test/ui/chance_art_test.dart` |
| | 4b `layout` and the reel; `internal/modules/cards` takes `/draw`, `/tarot`, `/mahjongg`; `/pick`, `/shuffle`, `/flip` move; `Chance` is deleted | Its test on `decks.json`, `tarot.json` |
| 5 Manifests | 5a The pattern checker and `patterns.json` in three engines | One test per language |
| | 5b `reference`; reference patterns become derived types | `markdown_test.go` reference cases |
| | 5c Installing, switching and removing a manifest; decks and presets from one | One `internal/api` test each |
| 6 Bots | 6a The bot's manifest and its approval, again on more | `internal/bots/service_test.go` |
| | 6b Structured `command.invoked`; presses on cards; values and effects in responses | The same |
| 7 Live cards | Inputs on built-in cards, `view.updated`, timers, private parts | One `modhost` test each |
| 8 Owner rule, direct link | 8a The manifest and what is on travel from the owner | `internal/link` event tests |
| | 8b `module.invoked`; owner-made messages; `object.resolved` | The same |
| 9 Hub as owner | 9a The hub makes and holds keys; key-makers, the fallback and the record's key fields go; the history sender | `internal/hub` tests, one at a time |
| | 9b The hub opens and validates envelopes; `LINK-PROTOCOL.md` sections rewritten (8.6) | The same |
| | 9c `modhost` in `churr-hub` with its own store; switches, bots and manifests in the record | `imports_test.go`; one `internal/hub` test |
| | 9d Invocations and presses in hub channels | One `internal/hub` test |
| | 9e The hub's capacity measured again | `tools/loadtest`, on a rented machine, not the laptop |
| 10 Bots across instances | The owner forwards to a bot's home; private answers return | `internal/link` event tests |
| 11 Instants | `instant` and `event` onto the envelope; the dialog as picker | `testdata/instants` vectors |
| 12 Measurements | `measure` onto the envelope | `testdata/objects/conversions.json` |
| 13 Bases, Unicode | `number` and `codepoint` onto the envelope | `testdata/objects/codepoints.json` |
| 14 The baseline | The rest of the generic layer, in both clients | Before the first app release people keep |

Step 9a depends on nothing before it. If the owner wants the defect of
R109 repaired first, it can be taken straight after stage 0.

Stages 11 to 13 are mostly a move on the wire: the recognisers and
dialogs exist in all three languages already.

## 11. Costs and risks

- **Every type is written two or three times.** JavaScript and Dart
  always, Go too where text is read on the server. The shared vectors
  are what keep it honest.
- **The baseline is large.** About 200 items, twice, before the first
  app people keep. Items designed before anything uses them will be
  wrong in places, and rule 6 freezes them.
- **App-store lag.** A later kind reaches the web chat with the instance
  and the app with a store release. Until then app users see the generic
  rendition or the text. An app can also be newer than its instance, and
  an instance newer than its hub.
- **Third parties cannot add a kind.** A manifest derives; a bot
  composes blocks. If the generic layer is too poor, widening it is our
  work on our schedule.
- **A bot is a remote process in the conversation.** Its manifest is
  approved, but what it answers is its own word, and a slow bot is a
  waiting person.
- **The hub becomes a content-bearing server** (section 8.7): keys to
  guard, everything readable by its operator, capacity to measure again.
- **The owner rule** adds a round trip, stops module commands while the
  owner is away, makes commands differ by conversation, and changes the
  acceptance rule that keeps instances from speaking for each other's
  members. That rule must be widened with care.
- **The composer loses early refusals** for any grammar that lives only
  in a module's Go (section 9.1).
- **Patterns run in backtracking engines** on the clients. A gap in the
  subset is a frozen composer.
- **Renames** make help text, documentation and habits differ between
  instances.
- **The app loses bundled art.** First use needs a fetch.
- **Finding out why something is not a chip** gets harder: the manifest,
  the client's level, a channel switch, the admin's order, or who owns
  the conversation.

## 12. Where the code contradicts the decisions

| # | The code and documents today | The decision |
|---|------------------------------|--------------|
| 1 | **The hub is kept from the content of channels it owns**: no stream key, key-maker instances, "read content: stopped" (`LINK-PROTOCOL.md` 10, 13, 23a, 24; `internal/hub`; R56's wording) | The hub is the owner and nothing is secret from it [decided, R95, R109] |
| 2 | `internal/hub` may import seven packages and no chat or module code (`STRUCTURE.md` 3, `imports_test.go`) | The hub runs built-in modules and validates [follows from R109] |
| 3 | `message.created` only from the author's home instance | The owner makes module output for a remote invoker [follows from the owner rule] |
| 4 | The author's own instance draws a chance result in a shared channel and peers take its word | Only the owner produces module output [proposed; see 7.4 for chats] |
| 5 | Typed `chance` and dice fields on the wire | One generic envelope [decided] |
| 6 | Clients read and carry out built-in commands; three copies of the table; `runCommand` refuses built-ins | Commands belong to modules; the clients fetch the table [decided] |
| 7 | The server scans text for objects; the composer sends only exceptions | Objects are marked in the composer on the way in [decided]; the server checks [unconfirmed] |
| 8 | Command order is fixed; a community command hides a replaceable built-in (R72) | The admin sets precedence and renames [decided] |
| 9 | `/remind [target] when \| what`, with repeats | Three forms with no target, repeat or `\|` [decided as the example] |
| 10 | Reference expressions allow no groups | Patterns with capture groups [decided] |
| 11 | A bot's command has an unparsed `hint`, gets raw text, answers with four fixed types and posts one fixed card shape | A bot uses "the same types" as a built-in module or a manifest [decided, R108] |
| 12 | A bot registers commands freely; nothing is approved | An admin approves a bot's manifest [unconfirmed] |
| 13 | A bot's commands, dialogs and buttons stop at its own instance in a shared channel | The owner admits bots and routes to them [proposed] |
| 14 | No conversation says what is on in it; a hub's record has no such field | Off per instance and per channel [decided], decided by the owner |
| 15 | A chance message has no text; the words are in `fallback` | The text is the message [proposed] |
| 16 | A client reports nothing about itself; "too old to answer" exists for forms only | The client says its level; every card can say it needs a newer app [unconfirmed] |
| 17 | Art is bundled in the web chat and the app | Pictures fetched from the instance [decided] |
| 18 | The dice grammar in three languages, refused in the composer | One grammar, in the module [follows from "no module code in clients"] |
| 19 | Both clients are English-only; the two icon sets differ in names; themes have no tone colours | Catalogue rules 8, 10 and section 3.9 [unconfirmed] |
| 20 | "Card" and "dialog" already mean a bot's card and a bot's dialog | R107 uses both words for the catalogue. This document says "view" and "kind" |
| 21 | `internal/mod` is moderation, and `STRUCTURE.md` section 4 says "module" for Go modules | The word is taken twice; the host is `modhost` |

Dissolved by R108: the xcaddy-style build, a public package, a generated
main and the licence question no longer appear anywhere in this design.

## 13. Open questions for the owner

1. **The hub's keys.** The hub makes and holds the keys of channels it
   owns, and key-makers go? *Recommended: yes; it is what R109 needs.*
2. **History for a joining instance.** A member instance named by the
   hub sends the snapshot (a), or the hub keeps the state and sends it
   (b)? *Recommended: (a).*
3. **The owner rule on direct links.** The owner instance makes module
   output for the other instance's members too, with the widened
   acceptance rule? *Recommended: yes, the same rule as on a hub.*
4. **Files in hub channels.** They pass the hub sealed between
   instances. Should the hub read them as well? *Recommended: leave
   them; it is a choice, not a protection.*
5. **Direct and group chats across instances.** The owner rule (a), or
   each instance runs its own modules for its own members with values
   labelled where they were made (b)? *Recommended: (b), accepting that
   a modified instance can fake its own members' rolls there.*
6. **What the dice module owns.** Also `/flip`? Where do `/pick` and
   `/shuffle` go? *Recommended: `/flip` with dice; the other two in a
   small module on the reel, at stage 4.*
7. **`/remind`.** Do the target and repeats survive? *Recommended: yes.
   Add forms with `[person]` and `[channel]` first; let a slot accept
   either an instant or a recurrence; drop the `|`.*
8. **Form matching** as in section 5.2, including named options,
   repeated slots and the raw-text fallback for old apps. *Recommended:
   yes, and settle it before any app is released.*
9. **Bot cards.** Fold today's `cards` into the catalogue's blocks when
   bots move onto the contract, with no second card shape? *Recommended:
   yes.*
10. **Typed data per kind** over an untyped value. *Recommended: typed.*
11. **The level.** One number per client, plus a short list of known
    gaps, and a `level` on each value? *Recommended: yes.*
12. **The catalogue's scope.** Accept the seven groups and twelve rules,
    with the changes of section 3.10: phone, email, the reel, checklist
    and the rest added; per-block audience cut to "everyone" and "one
    person"; boards and game pieces marked later; a thirteenth rule
    against imitating trust marks; a list of blocks known to be broken
    below a level. *Recommended: yes.*
13. **Go recognisers.** Only for kinds read from free text and the few
    argument kinds; everything else arrives structured and is checked by
    schema. *Recommended: yes.*
14. **Language.** Values are shown the viewer's way; sentences come in
    the owner's language until both clients are translated.
    *Recommended: yes.*
15. **A bot's manifest.** Approved by an admin; approved again when it
    asks for a new command, surface, effect, kind or pattern.
    *Recommended: yes.*
16. **Who installs a manifest.** Admins install; a channel's managers
    switch it on or off there and keep editing their own reference
    patterns. *Recommended: yes.*
17. **Marks.** The composer sends all of them and the server checks,
    with a scan kept for senders that send none. *Recommended: yes.*
18. **Switched off.** Old cards turn to text and return when switched on
    again. *Recommended: yes.*
19. **Touch.** Drop "hold". *Recommended: yes.*

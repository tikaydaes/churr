# Message format

What a message is made of, and how every client draws it. `CONVENTIONS.md`
gives the rules for identifiers, times and limits used here.

A message has up to four parts, in this order on screen:

1. **Text**, in Churr Markdown.
2. **Files**.
3. **Cards**.
4. **A form**.

Beside the message, and not part of its content: reactions, checklist
ticks, thread information and pins. Changing those is never an edit.

## 1. The message object

This is the shape the API returns and events carry.

```json
{
  "id": "0193a6f2-7c1e-7b3a-9f0e-2a4d5c6b7e8f",
  "conversation_id": "0193a6f0-…",
  "seq": 4821,
  "author_id": "0193a000-…",
  "parent_id": null,
  "quote": null,
  "text": "**Deploy** at 5. @sam can you check <#0193a0c7-…>?\n\n- [ ] Tag the release\n- [ ] Tell support",
  "entities": [
    {"type": "account", "at": 17, "len": 4, "account_id": "0193a001-…"},
    {"type": "conversation", "at": 36, "len": 4, "conversation_id": "0193a0c7-…"},
    {"type": "task", "at": 43, "len": 21, "item_id": "t1"},
    {"type": "task", "at": 65, "len": 18, "item_id": "t2"}
  ],
  "tasks": [
    {"id": "t1", "checked": true, "checked_by_id": "0193a001-…", "checked_at": "2026-11-13T09:41:02.118Z"},
    {"id": "t2", "checked": false}
  ],
  "files": [],
  "cards": [],
  "form": null,
  "fallback": "",
  "reactions": [
    {"emoji": "👍", "count": 3, "me": true, "account_ids": ["0193a001-…", "0193a002-…", "0193a003-…"]}
  ],
  "thread": {"reply_count": 2, "last_reply_seq": 4830, "last_reply_order": "0000000000000000!!00000000000012de", "last_reply_at": "2026-11-13T09:50:11.000Z", "participant_ids": ["0193a001-…"]},
  "via": null,
  "pinned": false,
  "notified_anyway": false,
  "has_history": false,
  "order": "0000000000000000!!00000000000012d5",
  "created_at": "2026-11-13T09:30:00.000Z",
  "edited_at": null,
  "deleted_at": null,
  "deleted_by_id": null
}
```

| Field             | Meaning                                                    |
|-------------------|------------------------------------------------------------|
| `parent_id`       | The top-level message this replies to. `null` for a top-level message. A reply never has replies. |
| `quote`           | For a reply: `{"message_id", "start", "end"}`, the message quoted and the range of its text, or no range for the whole message. Clients draw the quote from the quoted message's current text. If that message is deleted or the range no longer exists, they draw "original message changed". |
| `text`            | Churr Markdown, exactly as the author sent it (section 2). May be empty if there is a file, card or form. |
| `entities`        | What the server resolved in the text (section 3).          |
| `tasks`           | The state of each checklist item (section 4).              |
| `files`           | Section 5.                                                 |
| `cards`           | Section 6.                                                 |
| `form`            | Section 7.                                                 |
| `fallback`        | Plain text the server wrote to stand in for the cards, the form and a chance result. Shown by a client or bot that cannot draw them. Empty if there are none. |
| `view`            | The message as a value of the catalogue (a `ValueEnvelope`, `DESIGN-MODULES.md` section 4.1): `kind`, `level`, `data` (that kind's own protobuf message, as base64url bytes), `by`, and `alt`, `source`, `removed` where they apply. Today only a roll has one (section 1.2). `text` is the message; the view is how a client that knows the kind and reads its level draws it, and a client that does not shows the text. Stored as it came; never edited; removed with the text on a delete. |
| `action`          | Present and true when the text says what the author is doing, typed with `/me` (section 1.1). |
| `chance`          | A result the server drew for `/flip`, `/pick`, `/shuffle`, `/draw`, `/mahjongg` or `/tarot` (section 1.2). A roll is a `view` instead. |
| `reactions`       | Section 8. `account_ids` holds at most the first ten; the rest come from the reactions endpoint. |
| `thread`          | Top-level messages with replies only. `last_reply_order` is the `order` of the reply at `last_reply_seq`: where the thread's marker is drawn. |
| `via`             | How it was posted if not typed by its author: `{"kind": "webhook" \| "reminder" \| "scheduled" \| "command" \| "import" \| "system", "name": "…", "account_id": "…", "icon_file_id": "…"}`. `account_id` is who caused it (the person who sent a command a bot answered). `icon_file_id` is a webhook's icon for this message. |
| `notified_anyway` | The author used "notify anyway" on it. Says nothing about whether anyone was reached. |
| `has_history`     | There are earlier versions to open.                        |
| `order`           | Where the message is shown. An opaque string; clients sort a conversation by it as plain text. In a conversation that is not shared it follows `seq`. In a shared one it is made from the shared-clock stamp of the event that created the message, so it is the same on every server (`DESIGN.md` section 11). |
| `late`            | Present and true when the message arrived here after messages ordered later than it had already been delivered. It is unread like any new message. |
| `unsent`          | Present and true, in a shared conversation, while the link has not yet acknowledged the message. It is shown in its place all the same. |
| `deleted_at`      | Set on a deleted message. Its `text`, `files`, `cards` and `form` are then empty in this object; the earlier content is in its history until purged. |

### 1.1 Actions (`/me`)

`/me waves` posts `waves` with `"action": true` (ruling R67). Every
client draws an action as the author's display name in bold, then the
text in italic; one line of text runs on from the name, longer text
goes under it. The name always shows, even when the message is grouped
under the one before. The text is ordinary Churr Markdown with its
entities, so mentions, search, edits, export and threads work as for
any message; an edit keeps the mark. A client that does not know the
field shows the text plainly. In a sealed chat the mark is inside the
content (`SEALED.md` section 8.3). A message cannot be an action
without text.

### 1.2 Chance results

The client asks (`postMessage` with `chance`, a `ChanceInput`); the
server draws the result with `crypto/rand` and stores it on the message.
No edit can change it, so every reader sees a result nobody made up. The
request comes with no text, files, cards or form. People only: a bot's
request is refused. Never in a sealed chat, which the server cannot
write into. Deleting the message removes the result.

A **roll** is stored as the message's `text`, the roll in plain English
("Rolled 2d6+3: [4, 2] + 3 = 9", dropped dice in parentheses, rows
separated by `; `), written with no Markdown so it reads the same with
no client support, and its `view`: a value of kind `dice_roll` (level 1,
`by` `dice`) whose data is a `DiceRoll`, `{dice, rows}`. An edit that
changes the text of a message with a view is refused with `conflict`.
Every other result is the message's `chance`, with its `fallback` the
result in plain English, which old clients show and search reads.

| `kind`    | Asked with                       | Stored                                                   |
|-----------|----------------------------------|----------------------------------------------------------|
| `roll`    | `dice`: the expression (`FEATURES.md` section 12.4), rows separated by commas | In the view's `DiceRoll`: `dice` as read (`d20` is `1d20`, `adv` is `2d20kh1`; rows separated by `, `), `rows` (1 to 8, in order, each `{dice, terms, total}`: the row as read, its terms, each `{sign, count, sides, keep, keep_n, rolls, dropped, value}` for dice or `{sign, value}` for a number, and its total) |
| `flip`    | nothing                          | `outcome`: `heads` or `tails`                            |
| `pick`    | `options`: 2 to 20               | `options` as given, `picked`: the position of the one picked |
| `shuffle` | `options`: 2 to 20               | `options` as given, `order`: their positions in shuffled order |
| `draw`    | `count` (default 1), `deck` by name or alias (default `standard`) | `deck` (its name), `cards` (codes, in order drawn, no card drawn more often than the deck holds it), and `reversed` (one per card) for a deck with orientation |
| `tarot`   | `spread` by name or alias (default `three`) | `deck` (`tarot`), `spread`, `cards` (one per position), `reversed` |

The decks, their card codes and the tarot spreads, positions and meanings
are the shared tables `testdata/commands/decks.json` and `tarot.json`,
written by `tools/decks-gen/gen.py`. A card is `<suit>-<rank>`
(`hearts-queen`, `cups-10`, `pine-bright`, `dots-5`) or a card outside
the suits (`joker-red`, `major-0`, `wind-east`, `season-spring`).
Clients word every card, position and meaning from their own wording
files and always show a card's name in words; a Unicode suit symbol,
playing-card or mahjong character is drawn beside the words, never in
their place.

**How a result is drawn.** A panel with the cut corner and one slot per die, coin, pick, shuffled place or card:
mono numerals on the panel fill with an edge, or the slot's picture
(`ART.md`: cards, tiles, dice, and a coin for `/flip` and for a d2, which
reads "heads (1)" or "tails (2)"); a dropped die faded and struck
through, with no word beside it (its label says "dropped"); a roll's total after `=`,
a roll of several rows as a line per row, each starting with its
expression and ending with its own total; a shuffle's places listed
under the slots. Cards and tiles are not listed by name: a card's name
pops up over it on hover, on keyboard focus and on a tap, and is the
card's label for screen readers.

A tarot reading is laid out in its spread's shape, from the layout in
`testdata/commands/tarot.json` (each position's place, in card steps,
and its turn): three cards in a row; the Celtic cross with the present
in the centre, the challenge laid across it (turned a quarter; reversed
it is also upside down), the past to the left, the future to the right,
above over and below under it, and the staff to the right from advice at
the bottom up to the outcome at the top; the horseshoe as a U of seven;
the relationship spread as you, the dynamics and the partner across the
top, the recent past, the foundation and the near future below, and the
advice at the foot. The table shrinks to the message's width, a spread
of more than three cards drawn smaller. No words are listed under it but
the reading's overall energy. Each card is a button labelled with its
position, name and "upright" or "reversed"; hovering shows its name,
and pressing it (or Enter or Space) opens a popover with its position,
name, orientation and meaning, one at a time, closed by Escape or a
press elsewhere. Tab and the arrow keys go through the cards in the
reading's order.

**The one animation.** When the message arrives live for the reader,
and for its sender when it first appears (never when it is loaded from
history, met on scrolling back or drawn again), it plays once; pressing
the result never plays it again.

- Dice, coins, picks and places spin like slot-machine reels through
  values of their own kind (faces of that die, the options) and stop on
  the server's value, left to right, in 1.2 seconds; more than ten slots
  get one short flurry together. A roll's total and its dropped dice
  appear only once the last reel has stopped. The rows of a roll spin
  together, each stopping left to right within the same 1.2 seconds,
  and each row's total and dropped dice appear once its own last reel
  has stopped.
- Cards and tiles (`/draw`, `/mahjongg`, `/tarot`) lie face down
  (`ART.md`) and are turned over one after another: 200 ms face down,
  then each turn takes 280 ms, the next card starting 260 ms after the
  one before (less for many cards: a whole draw is over in about 2.4 s
  at most, three cards in 1.2 s, five in 1.7 s). A reversed card lands upside down.
- A tarot reading turns its cards in the spread's order, one every
  700 ms (less past four cards); the card that has just landed wears the
  focus outline until the next lands, and the overall energy appears
  350 ms after the last. A three-card reading takes 2.4 s, a Celtic cross 3.7 s.

With the platform's reduced-motion setting nothing moves: the result
appears face up, in place, every word with it. The animation decides nothing, and
its last frame is the static drawing.

### 1.3 Messages the server posts

Messages the server posts itself (someone joined, a call started, a
thread moved to a channel) have `via.kind` of `system`, an `author_id`
of the account concerned, and a `system` object in place of text:
`{"kind": "member_joined" | "member_left" | "call_started" | "call_ended" | "thread_moved" | "channel_renamed" | "pinned" | "owner_override" | "manager_dismissed" | "bot_token_made" | "account_recovered" | "account_claimed" | "group_access" | "channel_shared", …}`.
Clients word these themselves, so they follow the reader's language.
`member_joined` carries `by_id` when someone else added the person
("Ada was added by Lee"), and none when they joined ("Sam joined"); it
is posted in private channels (FEATURES.md section 13).

`owner_override` is posted in place of `member_joined` when an Owner or
an Admin of the instance adds someone to a private channel without
being in it (PERMISSIONS.md section 6.3). Its `author_id` is the person
added and its `by_id` who did it ("Ada was added by override, by Lee").
`manager_dismissed` is posted when an Owner or an Admin takes a
manager's level away from the admin console (PERMISSIONS.md section 6,
note 8): its `author_id` is the former manager and its `by_id` who did
it ("Ada is no longer a manager, by Lee"). Clients draw both as notices
that want attention, in pink (`STYLE.md`), not as plain lines, and
nobody can turn them off.

The access alerts (FEATURES.md section 21.13) are drawn the same way.
Each is posted in every conversation of its `author_id` that is not
public, and `by_id` names who did it when that is known:

- `bot_token_made`: a new token was made for the author, a bot.
- `account_recovered`: a recovery link `by_id` made was used to sign
  in as the author; no `by_id` for a link made on the command line.
- `account_claimed`: the author, an imported account, was claimed with
  a claim link `by_id` made.
- `group_access`: the author arrived in this private channel through
  the group `name`, put there by `by_id`, who may be the author. It is
  posted in place of `member_joined`.
- `channel_shared`: the author shared this channel with the instances
  or hub named in `name`, or added that instance to it.

## 2. Churr Markdown

A fixed subset. Every client implements exactly this and nothing more.
Text that is not one of these constructs is shown as typed.

### Blocks

| Construct       | Written as                           | Notes                  |
|-----------------|--------------------------------------|------------------------|
| Paragraph       | Text. A blank line starts a new one. | A single line break inside a paragraph is kept as a line break. |
| Heading         | `# `, `## `, `### `                  | Three levels. More `#` are level three. |
| Quote           | `> text`                             | May contain other blocks. |
| Bullet list     | `- item` or `* item`                 | Nest by indenting two spaces, three levels deep. |
| Numbered list   | `1. item`                            | Numbering follows the first number. |
| Checklist item  | `- [ ] item`, `- [x] item`           | Section 4. May be mixed with bullets in one list. |
| Code block      | Three backticks, optional language, the code, three backticks | Section 2.3. |
| Table           | Pipe rows with a `---` row under the header | Section 2.4. |
| Rule            | `---` on its own line                |                        |

### Inline

| Construct     | Written as                       | Notes                       |
|---------------|----------------------------------|-----------------------------|
| Bold          | `**text**`                       |                             |
| Italic        | `*text*` or `_text_`             | `_` only at word edges.     |
| Strikethrough | `~~text~~`                       |                             |
| Spoiler       | `\|\|text\|\|`                   | Hidden until the reader selects it. |
| Inline code   | `` `text` ``                     | Nothing inside is formatted. |
| Link          | `[text](https://…)`              | `http`, `https`, `mailto` and `tel` only. Anything else is shown as its text. |
| Bare link     | `https://…` in the text          | Becomes a link entity.      |
| Mention       | `@username`, `@handle`, `@channel`, `@here`, `@username@server.name` | Becomes an entity if it resolves; otherwise plain text. The last form names someone on another server and resolves only in a conversation shared with that server. |
| Channel link  | `<#conversation-id>`             | Becomes an entity if it names a channel the author can see. Clients insert it when the person picks a channel from the picker, and draw it as the channel's current name. `#games` typed as plain characters is plain text and is never resolved. |
| Emoji         | `:name:`, `:name@server.name:`   | A custom emoji if one has that name (the second form is another server's, in a shared conversation); a standard emoji if `name` is a standard short code; otherwise plain text. Standard emoji typed as characters are just characters. |
| Escape        | `\` before a punctuation character | Shows the character plainly. |

Not supported, and shown as typed: HTML, images (`![…](…)` shows as a
link to the address), footnotes, reference-style links, and nesting a
table inside a list.

Links always open outside the client, show their destination host on
hover or long-press, and are marked when the visible text looks like a
different address from the real one.

### 2.3 Code blocks and highlighting

    ```go
    func main() { fmt.Println("churr") }
    ```

- The word after the opening backticks is the language. It is matched
  without regard to case against the list below.
- Clients highlight with the same grammar set: highlight.js grammars in
  the web chat (vendored, no network), and their port in the apps. The
  list is fixed here so both show the same thing.
- An unknown or missing language is shown in monospace with no colour.
  Clients do not guess the language.
- Code blocks show a copy button and scroll sideways; they do not wrap
  unless the reader turns wrapping on.

Languages, with accepted aliases in brackets:

`bash` (`sh`, `shell`, `zsh`), `c`, `cpp` (`c++`, `cc`), `csharp`
(`cs`, `c#`), `css`, `dart`, `diff` (`patch`), `dockerfile` (`docker`),
`elixir`, `go` (`golang`), `graphql`, `haskell` (`hs`), `html`, `ini`,
`java`, `javascript` (`js`), `json`, `kotlin` (`kt`), `lua`, `makefile`
(`make`), `markdown` (`md`), `nginx`, `objectivec` (`objc`), `perl`,
`php`, `plaintext` (`text`, `txt`), `powershell` (`ps1`), `protobuf`
(`proto`), `python` (`py`), `r`, `ruby` (`rb`), `rust` (`rs`), `scala`,
`scss`, `sql`, `swift`, `toml`, `typescript` (`ts`), `xml`, `yaml`
(`yml`), `zig`.

### 2.4 Tables

    | Service | Status | Owner |
    |---------|:------:|------:|
    | api     | up     | @sam  |
    | search  | down   | @lee  |

- The second row sets alignment per column: `:--` left, `:-:` centre,
  `--:` right.
- Cells hold inline constructs only.
- At most 20 columns and 200 rows. A larger table is shown as typed.
- On a narrow screen a table scrolls sideways inside the message.

## 3. Entities

When a message is posted or edited the server reads the text, resolves
what it refers to, and stores the result. Clients draw from entities
and never resolve names themselves.

- `at` and `len` are positions in the text counted in UTF-16 code
  units, which is how both JavaScript and Dart index strings.
- Entities are in order of `at` and do not overlap, with one
  exception: a `task` entity covers its item's text, and mentions,
  channels, emoji and links inside that text are entities of their own
  within it. A mention inside a checklist item notifies as usual.
- Nothing inside inline code or a code block becomes an entity.

| `type`         | Written as            | Extra fields                    | Drawn as                    |
|----------------|-----------------------|---------------------------------|-----------------------------|
| `account`      | `@username`           | `account_id`                    | The account's current display name, highlighted if it is the reader. |
| `group`        | `@handle`             | `group_id`                      | The group's current handle, highlighted if the reader is in it. |
| `broadcast`    | `@channel`, `@here`   | `scope`: `channel` (every member) or `here` (the members online when it was posted) | Highlighted for everyone.   |
| `conversation` | `<#conversation-id>`  | `conversation_id`               | The channel's current name, as a link, or "private channel" if the reader cannot see it. `at` and `len` cover the whole `<#…>`. |
| `emoji`        | `:name:`              | `emoji_id`                      | The custom emoji's image.   |
| `link`         | A bare address        | `url`                           | A link.                     |
| `task`         | `- [ ] text`          | `item_id`                       | A tick-box (section 4). `at` and `len` cover the item's text after the box. |
| `instant`      | `2026-12-24 18:00 Europe/Berlin` | `time`, `precision`, `zone` | The moment in the reader's own zone, as a control that opens a time converter (section 3.1). |
| `reference`    | `PROJ-123`, `#4521` (a reference pattern's match) | `pattern_id`, `value`, `url` | A link, over the typed text, to `url` (section 3.2). |
| `message_link` | A link to a message of this server | `conversation_id`, `message_id`, `url` | A compact quote of the message as the reader may see it, or "a message you can't see" (section 3.2). |
| `measure`      | `5 km`, `72°F`, `3 lb` | `value`, `unit` | The typed text, then the reader's system's equivalent in muted text, as a control that opens the unit converter (section 3.2). |
| `event`        | `2026-12-24 18:00–20:00 Europe/Berlin` | `time`, `end`, `precision`, `zone` | One range in the reader's zone, as a control with "Add to calendar" (section 3.2). |
| `phone`        | `+61 7 3123 4567` | `value` | The typed text, as a control: call (`tel:`) or copy (section 3.2). |
| `email`        | `sam@example.com` | `value` | The typed text, as a control: write (`mailto:`) or copy (section 3.2). |
| `geo`          | `-27.4698, 153.0251`, `geo:48.8584,2.2945` | `lat`, `lon` | The typed text, as a control: the device's maps on phones, copy elsewhere (section 3.2). |
| `object`       | `[[2d6+3]]` in a sentence (inline dice) | `object`: a value of the catalogue (section 1, `view`); inline dice are kind `dice_roll`, or `removed` | The typed text, and for inline dice the server's roll inline, spun once when it arrives live (section 3.2). |
| `colour`       | `#fff`, `#ff8800`, `#ff880080` | `value` | A swatch before the typed text; selecting it copies the colour (section 3.2). |
| `number`       | `0x4f22`, `0b1011`, `0o755` | `value`, `base` | The typed text, as a control that opens the number in decimal, hexadecimal, octal and binary (section 3.2). |
| `codepoint`    | `U+1F600`, `&#x10F2;`, `\u{1F600}` | `value` | The typed text and the character beside it in muted text, as a control that opens what the code point is (section 3.2). |

`@everyone` is the built-in group and is stored as a `group` entity.
It, `@channel` and `@here` are the broad mentions. Where the author
may not use them (`FEATURES.md` section 2.5: the server or the channel
limits them to managers, or the channel is larger than the server's
limit) no entity is stored for them: the words stay in `text` as
typed and are drawn as plain text.

A mention notifies only accounts that can read the conversation. An
`account` entity for someone who cannot is still stored and drawn, and
the author is told they will not be notified.

When an account, group or channel is renamed, old messages keep their
text and their entities, and clients show the new name. When it is
removed, clients show the name as it was written.

### 3.1 Instants

A date and time typed in international form becomes an `instant` entity
(ruling R69), shown to every reader in their own zone. The text keeps
exactly what was typed, which is what old clients, search, export and
notifications show.

    instant = date sep time [ zone ]
    date    = YYYY "-" MM "-" DD              a real day of the calendar
    sep     = "T" | " "                       exactly one space
    time    = HH ":" MM [ ":" SS [ "." 1-9 digits ] ]   00:00 to 23:59:59
    zone    = "Z" | offset | " " name
    offset  = ("+" | "-") HH [ [":"] MM ]     hours to 14, minutes to 59
    name    = an IANA name (`Europe/Berlin`, `America/Argentina/Buenos_Aires`),
              or `UTC` or `GMT`

- It stands on word boundaries: the character before is not a letter, a
  digit or one of `. - + / : @ # \`; the character after is not a letter
  or a digit, nor one of `: - + / . , –` followed by a digit. So a version
  number, an interval (`…Z/…Z`) and a log line with comma milliseconds
  stay plain text.
- A date alone or a time alone is never an instant. Nothing inside inline
  code, a code block, a link's text or address, or after a backslash
  becomes one. Escaping any character of it (`2026\-12-24 18:00`) keeps
  it plain; a composer sends one as plain text with an object hint of
  kind `plain` instead (section 3.2), so the text stays as typed.
- A `-` offset counts only after `T` or after seconds: in
  `2026-11-20 10:00-11:30` the `-11:30` reads as the end of a range, and
  the whole is an event (section 3.2). `2026-11-20T10:00:00-11:30` is the
  offset.
- A time followed by a space and something that looks like a zone
  abbreviation (one to four capitals and a `T`: `EST`, `CEST`) or a spaced
  offset (`+10:00`) stays plain text, so it is not read in the author's
  zone when another was meant. A zone-shaped name the database does not
  know (`Mars/Olympus_Mons`, `europe/berlin`) leaves it plain too.

| Field       | Value |
|-------------|-------|
| `time`      | The moment in UTC, RFC 3339 with milliseconds. (`at` is the position, so the moment is `time`.) |
| `precision` | `minute`, or `second` when seconds were typed. A fraction is kept to the millisecond. |
| `zone`      | The zone as written (`Z`, `+10:00`, `-05`, `Europe/Berlin`), or the author's zone when none was written. |

**Resolution.** With `Z`, an offset or a name the moment is fixed. With no
zone it is read in the author's IANA zone, which the posting client sends
as `time_zone` with the message (and with an edit). Without one (bots,
webhooks, older clients) such text stays plain; a zone-bearing instant
resolves for them all the same. Names resolve with the tz database the
server carries itself. A wall time the zone's clocks skip when they go
forward is not an instant and stays plain text; one they show twice when
they go back is the first of the two. The server resolves on post and on
edit; an edit that leaves the text alone keeps its instants. Shared
conversations carry instant entities to the other servers unchanged.

**Hints.** A client may send `instant_hints` with a post or an edit:
ranges of the text the author's client resolved, such as a phrase
"tomorrow at 5pm" the author accepted in the composer. Each is
`{start, end, time, precision, zone}`, positions in UTF-16 code units.
The server keeps a hint as an `instant` entity over those words when its
range lies inside one run of plain text (not in code, a link's text,
across formatting marks, or over another entity) on character
boundaries, `time` is an RFC 3339 moment in the years 1000 to 9999,
`precision` is `minute` or `second`, and `zone` is an offset or a known
IANA name. Others are dropped without an error. At most 50 are read.
Bots may send hints too.

**Drawing.** In the reader's zone and locale, compact (`Fri 20 Nov, 2:30
pm`, the year only when it is not the current one, `Today`, `Tomorrow` or
`Yesterday` where true), as a control with a clock: focusable, with an
accessible label holding the full date, the reader's zone and the text
as typed. Selecting it opens the time converter (`CLIENTS.md`).

The shared cases: `testdata/markdown/instants.json` (the grammar, in
context, for all three parsers), `testdata/instants/resolve.json`
(resolution, run by the server and both clients) and
`testdata/instants/phrases.json` (the composer's phrases, both clients).

### 3.2 Objects

Ruling R73 adds objects recognised inside plain text. Unlike the
candidates of section 2 they are not nodes of the tree: each lies inside
one run of plain text (a `text` node), and a client draws it by cutting
that run where the entity says. So none is ever found in code, a link's
text or address, a bare link, an escape, across formatting marks, or in
a checklist item. The text always keeps what was typed. The kinds, in
the order they are tried at a position (so the first that reads wins):

1. `message_link`, 2. `reference`, 3. `measure`, 4. `colour`, 5. `number`, 6. `codepoint`, 7. `phone`, 8. `email`, 9. `geo`, 10. `dice`.

**Edges.** Every object stands on the same boundaries. Before it: the
start of the text, white space, or one of `( [ { " ' ~ * _ | > , ; : =
“ ‘ « — –`. After it: the end of the text, white space, one of `) ] } "
' * _ ~ | < ” ’ » … — – ! ?`, or one of `. , ; :` that no letter or digit
follows. So `page.html#fff`, `a/PROJ-1` and `PROJ-1/x` are plain text.

**References.** A server's admins define server-wide reference
patterns; a named channel's managers define the channel's own
(`createReferencePattern`). A pattern is a `prefix` followed at once by
a value, and a `url_template` with `{value}` where the value goes:

| Field          | Rule |
|----------------|------|
| `name`         | 1 to 64 characters. |
| `prefix`       | 1 to 16 of letters, digits and `# _ . / : -`, matched exactly (case and all). Not ending in a digit; one with no letter or digit in it starts with `#`. |
| `expression`   | What the value matches, at most 64 characters, in the restricted form below; absent, 1 to 12 digits. |
| `url_template` | `https://` or `http://`, at most 500 characters, `{value}` after the host, no white space, control characters or `" < > \` `` ` ``. |

The restricted form is a small part of regular expressions that RE2, the
browser and Dart all read the same way, and that cannot be made slow:

    expression = 1*( atom [ repeat ] )
    atom       = letter | digit | "_" | "-" | "\." | "\-" | "\d" | "\w" | class
    class      = "[" 1*( member | member "-" member ) "]"
    member     = letter | digit | "_" | "." | "\-" | "\."
    repeat     = "?" | "+" | "*" | "{" n "}" | "{" n "," m "}"      m ≤ 32

No groups, no alternatives, no `.`, no negated class, no anchors; at
most three parts that vary in length; it must not match nothing. So a
value is made only of letters, digits and `_ . -` (ASCII), goes into the
address as it is, and cannot choose the host.

A reference is the prefix and then the longest value the expression
matches there, at most 64 characters, standing on the edges. Where
several patterns could read at a position, a channel's is tried before
the server's (so a channel's pattern wins over the server's with the same
match), then the longer prefix, then the older. At most 50 patterns for
the server and 50 for each channel. The entity carries the `pattern_id`,
the `value`, and the `url` the template gave when the text was written:
a later change to the pattern, or its removal, applies to messages
posted after it, and old messages keep what they resolved. An edit that
leaves the text alone keeps its references; one that changes the text
reads it with the patterns of that moment. Bots and webhooks get
references like people. A sealed chat's sending device finds them with
the server's patterns (a sealed chat has none of its own); the receiving
device keeps a sender's reference that lies in the text, ends in its
value and has a web address.

**Message links.** A link to a message of this server, as "Copy link"
writes it, is a `message_link` entity:

    message_link = home "/chat/c/" id ( "/m/" id | "/t/" id [ "/m/" id ] )
    home         = the server's origin (scheme and host, matched without
                   regard to case) | "churr://" the origin's host
    id           = a UUID, lower case with hyphens

Nothing may follow it: no query, fragment or slash. The `https` form is a
bare link (section 2), so the entity covers exactly that bare link in
place of a `link` entity; the `churr://` form lies in a run of text on
the edges above. A Markdown link `[text](…)` stays a link. `/t/{thread}`
names the thread's first message and `/t/{thread}/m/{reply}` the reply.
The entity carries `conversation_id`, `message_id` and the `url` as
typed, and never anything of the message: each reader's client fetches
it with `getMessage`, which answers only a reader who may see it, and
draws "a message you can't see" otherwise. So a deleted or edited
message shows as it now is. A message link gets no link preview. A
`plain` hint keeps it a plain `link`. A sealed chat makes no message
links (its messages have no address the server can show).

**Measurements.** A number and a unit of a fixed table:

    measure = [ "-" | "−" ] digits [ "." digits ] [ space ] unit
    digits  = 1 to 9 digits before the point, 1 to 6 after
    space   = one U+0020, U+00A0 or U+202F

| Kind | Units as typed (code) |
|------|-----------------------|
| length | `mm`, `cm`, `m`, `km`, `in` (no space before it, so "5 in the morning" stays text), `ft`, `yd`, `mi` |
| mass | `mg`, `g`, `kg`, `oz`, `lb`, `lbs` (`lb`) |
| temperature | `°C`, `℃` (`°C`), `°F`, `℉` (`°F`); only these may be negative |
| volume | `ml`, `mL` (`ml`), `l`, `L` (`l`), `gal` (US gallons) |
| speed | `km/h`, `kph` (`km/h`), `m/s`, `mph`, `kn` (knots) |
| area | `m²`, `km²`, `ft²`, `sq ft` (`ft²`), `mi²`, `sq mi` (`mi²`), `acre`, `acres` (`ac`), `hectare`, `hectares` (`ha`) |
| data | `kB`, `KB` (`kB`, 1000 bytes), `MB`, `GB`, `TB`, `KiB`, `MiB`, `GiB`, `TiB` |
| pressure | `hPa`, `mbar` (`hPa`), `kPa`, `psi`, `inHg`, `mmHg` |
| energy | `kJ`, `kcal`, `Wh`, `kWh` |

Units are matched exactly, case and all (`5 KM`, `5 gb` are text), the
longest first, and must stand on the right edge (`5 min`, `5 kmh` are
text). A number a digit and a point or comma come straight before
(`1,000 km`, `1.2.3 m`) is not read. No currency and no durations. Some
units are left out because the symbol is also a word or another thing:
`bar` (bars of music), `K` (thousands), `ha`, `PB` (a personal best),
`atm`, `cal`, `Hz` and its multiples, `N`, `lx`, `cd`, angles,
accelerations and concentrations. The table is written by
`tools/units-gen/gen.py`, which every parser takes it from.

The entity carries `value`, the number as typed with `-` for a minus
sign, and `unit`, the code. Each reader sees the typed text and, for
length, mass, temperature, volume, speed (not knots), area and pressure,
the equivalent in their own system in muted text: metric or imperial, or
off, a choice kept on each device and defaulting to the device's region
(imperial for the United States, Liberia and Myanmar). Nothing is added
when the unit is already of the reader's system. Whole numbers from 100
up, otherwise three significant digits; temperatures whole.
`testdata/objects/conversions.json` holds the equivalents both clients
show.

Selecting a measurement opens the unit converter: the text as typed,
then the value in the other units of its kind that readers use (length:
mm, cm, m, km, in, ft, yd, mi, nautical miles; temperature: °C, °F, K;
and so on), the reader's system first. A value is written with four
significant digits and the locale's separators, and as a power of ten
from 10^15 up and below 10^-6 ("1.23×10¹⁵"). Each row copies its value.
`testdata/objects/units.json` holds the units and the rows both clients
list.

**Numbers in another base.** `0x` (hexadecimal), `0b` (binary) or `0o`
(octal), the letter in either case, then digits of that base with
single `_` between digits (`0xFFFF_FFFF`), on the edges: `0x4f22`,
`0X4F22`, `0b1011`, `0o755`. The number must be below 2^128. Bare hex
(`4f22`, `deadbeef`), decimals, `#…` (a colour) and a `_` at either end
are text. `value` is the number in decimal and `base` 16, 2 or 8.
Selecting it opens the number in decimal (with the locale's grouping),
hexadecimal, octal and binary (in groups of four), each row copied
without its grouping; a number up to 10FFFF that is a Unicode scalar
value also shows as a character, which opens that code point.

**Code points.** A Unicode scalar value (not a surrogate, at most
10FFFF) written as `U+` and 4 to 6 hex digits (`U+10F2`, `U+1F600`),
`&#x` and 1 to 6 and `;` (`&#x10F2;`), `\u{` and 1 to 6 and `}`,
`\u` and exactly 4, or `\U` and exactly 8 (`\U0001F600`), on the edges.
`u+1F600`, `U+41` and `U+D800` are text. `value` is `U+` and at least
four upper-case hex digits. Each reader sees the typed text and the
character after it in muted text; a character that cannot be seen (a
control, a format character, a space, a combining mark) is shown as a
dotted box with its short name or a dotted circle. Selecting it opens
the character large with the code point, decimal, UTF-8 bytes, UTF-16
units, the HTML entity and the JSON, JavaScript and Python escapes,
each copied with a press, and the character's name and general
category. Entities carry only the code point: the name comes from the
server (`lookupCodePoints`, the Unicode Character Database of the
version it carries), asked for only when a person opens the code point,
so opening it tells the server which code point was looked at. Until
the answer comes, or if it fails, everything else is shown.

**Colours.** `#` and exactly 3, 6 or 8 hexadecimal digits, in either
case, on the edges: `#fff`, `#FF8800`, `#ff880080` (`#abcd` and `#12345`
are text, and so is `page.html#fff`). Any three digits are a colour,
`#482` included; in the composer, the popup offers plain text as its
other row. `value` is the colour as `#rrggbb`
or `#rrggbbaa` in lower case (`#fff` is `#ffffff`). A reference pattern
that reads at the same place wins (`#123456` with a `#` pattern is a
reference); the composer then offers the colour as a second row, sent as
an object hint of kind `colour`. `rgb()` and `hsl()` are not read.
Drawn as a small swatch, bordered in both the ink and background colours
so it shows on any theme, before the typed text; selecting it copies
`value`.

**Events.** Two instants joined by a range, in the instants grammar
(section 3.1), are one `event` entity:

    event = date sep time range end [ zone ]
    range = "–" | "-" | " – " | " - " | " to "
    end   = time | date sep time

A bare hyphen separates only where no offset could stand (not after `T`
or seconds). The one zone after the end is for both ends; with none,
both are read in the author's zone, as an instant is. An end given as a
time alone is on the start's day, or the next day when it is not after
the start's time (`22:00–02:00`). The end must come after the start, or
the whole stays plain text; so does an event either end of which does
not resolve. The tree's `instant` node carries `end` (the end's local
date and time, in the form of `local`). The entity has `time` (the
start), `end`, the start's `precision` and the `zone`. An edit that
leaves the text alone keeps its events. Drawn as one range in the
reader's zone and locale ("Thu 24 Dec, 6:00 pm – 8:00 pm", the end's
day only when it is another), as a control whose menu has "Add to
calendar", which makes an RFC 5545 file on the reader's device (title:
the message's first line; description and URL: a link to the message;
`testdata/objects/ics.json`), and the time converter for the start. The
composer offers a typed event as an exact token; it does not yet offer
loose phrases such as "Friday 2–4pm" as events.

**Phone numbers and email addresses.** Only clearly formed ones, and
nothing is ever looked up:

    phone = "+" group *( [ sep ] group )     7 to 15 digits, the first not 0
    group = 1*digit | "(" 1*digit ")"
    sep   = " " | "-" | "."
    email = local "@" label 1*( "." label )
    local = 1 to 64 of ASCII letters, digits and . _ % + -, not starting or
            ending with a dot, no two dots together
    label = ASCII letters, digits and -, not starting or ending with -;
            the last all letters, at least two; the domain at most 253

A bracketed lone `0` (`+44 (0)20 7946 0958`) is not dialled. A phone's
`value` is `+` and the digits (`+442079460958`); an email's is the
address as typed. A number without `+` is text. `@name@server` is a
mention, never an address. Clients draw each as the typed text, a
control labelled "Phone number …" or "Email address …" whose menu calls
or writes through the device's own app (`tel:`, `mailto:`) or copies the
value.

**Map coordinates.** A decimal latitude and longitude:

    geo     = degrees "," [ " " ] degrees      3 to 8 decimals each
            | "geo:" degrees "," degrees       1 to 8 decimals each
    degrees = [ "-" ] digits "." digits

The latitude has at most 2 whole digits and lies in -90 to 90, the
longitude at most 3 and -180 to 180. So that a list of numbers is not a
place, the plain form needs three decimals, may not follow a number and
a point or comma (with or without a space), and may not be followed by a
comma and another number. `lat` and `lon` are the numbers as typed.
There are no map tiles: on a phone selecting it opens the device's maps
(`geo:` on Android, Apple Maps' `maps:` on iOS); on a desktop and in the
web chat it copies `lat,lon`.

**Inline dice.** `[[`, a dice expression of `/roll`'s grammar
(section 1.2, `FEATURES.md` 12.4, the same limits, one row: a comma
is not read as dice) of 1 to 100
characters with no bracket or line break, and `]]`, on the edges:
`I hit for [[2d6+3]] damage`. The server rolls it when a person's
message is posted (with the random source of `/roll`), never on an edit:
the entity is an `object` whose `object` is a value of kind `dice_roll`,
its `DiceRoll` holding the expression written out in full and one row
with each term as it fell and the total. An edit keeps the
roll of dice it leaves exactly as they were (the same characters, in
the same place among the message's dice); dice it writes or changes get
a value with `removed: true`, no data and nothing rolled. An author's
object hint names inline dice with kind `dice`. Bots, webhooks, imports and commands
get no inline dice, and a sealed chat has none: the text stays as
typed. Clients draw the typed text, each die (drawn as a `/roll`
result's, the height of the line: `ART.md`) and the total in the
sentence, and spin the reels once when the message arrives live, the
total shown once they stop, as for a `/roll` result; removed dice say "not rolled: written in an edit".
A client recognises the shape for the composer with its own copy of the
grammar (`testdata/commands/dice.json`) and never rolls.

**Object hints.** A post, an edit, a scheduled message and a bot's reply
may carry `object_hints`: what the author chose in the composer for
ranges of the text, each `{start, end, kind}` in UTF-16 code units, at
most 50 read. Kind `plain` keeps the range as typed: no object is
recognised in it or reaching into it, and no typed instant overlapping
it resolves. Any other kind is an entity type the range is to be read
as, where it could be more than one (`#123` as a colour rather than a
reference, once colours exist); such a hint holds only when an object of
that kind covers exactly that range. A hint that does not fit is ignored
without an error. Hints are not stored.

The shared cases: `testdata/markdown/references.json` (with each case's
`resolver` carrying its `patterns` and `object_hints`) and
`message-links.json` (its `origin`) `measures.json`, `colours.json`, `numbers.json`, `codepoints.json`, `events.json` (its trees too), `contacts.json`, `places.json` and `dice.json` (rolled with every die
on its highest face, or `"dice": "edit"` as an edit reads it; a client
finds them as `dice` and compares only the range), run by the server and compared with
each client's own scanner.

## 4. Checklists

- Each checklist line becomes an item. The server gives it an
  `item_id` when the message is posted. On an edit, an item whose text
  is unchanged keeps its identifier and state; a reworded item keeps
  them if it is in the same position; a removed item's history is kept
  with the message's history.
- The `[ ]` or `[x]` the author typed sets the starting state. After
  that the state lives in `tasks`, not in the text.
- Any member of the conversation at level `member` or above may tick
  or untick any item. Readers see the list and cannot change it.
- A ticked item is drawn struck through, followed by who ticked it and
  when.
- Every change is kept: who, ticked or unticked, when. Anyone who can
  read the conversation can open an item's history.
- A change produces a `task.changed` event and is not an edit.
- In a sealed chat, a tick is sent as an encrypted message between the
  two people and the history is what their devices hold.

## 5. Files

```json
{
  "id": "0193a700-…",
  "name": "floorplan.png",
  "size": 482113,
  "media_type": "image/png",
  "kind": "image",
  "width": 1600,
  "height": 900,
  "duration_seconds": null,
  "caption": "Second floor",
  "preview_text": null,
  "url": "/api/v1/files/0193a700-…/floorplan.png",
  "thumb_url": "/api/v1/files/0193a700-…/thumb"
}
```

| `kind`  | Decided by                          | Drawn as                           |
|---------|-------------------------------------|------------------------------------|
| `image` | Content is PNG, JPEG, GIF or WebP   | Inline thumbnail, opening full size. Several images in one message form a gallery. |
| `video` | Content is MP4 (H.264/AAC) or WebM  | An inline player with a poster frame. |
| `audio` | Content is MP3, M4A, Ogg or WAV     | An inline player.                  |
| `text`  | UTF-8 text under 1 MB, by content   | The first 20 lines in monospace (`preview_text`), highlighted if the file extension maps to a language in section 2.3, with "show all". |
| `file`  | Anything else                       | Name, size and a download button.  |

- `kind` and `media_type` come from the file's content, never from the
  name or from what the uploader claimed. SVG is always `file`.
- A file is uploaded to a conversation first and attached by its
  identifier when the message is posted. An upload never attached is
  deleted after 24 hours.
- Downloads go through the server, which checks permission each time,
  sends its own content type with `X-Content-Type-Options: nosniff`,
  and a content security policy of `default-src 'none'; sandbox`.
  Kinds other than `image`, `video` and `audio` are sent with
  `Content-Disposition: attachment`.
- Thumbnails are made by the server: at most 720 px on the long edge,
  re-encoded, with location and camera details removed. The original
  is stored and served unchanged.
- The server does not make a poster frame for a video. The uploading
  client may send one image with the upload, returned as `poster_url`;
  without one the player shows a plain placeholder.
- In a conversation shared with other servers, a file from another
  server has a `state`: `ready`, `arriving`, `unavailable` or
  `too_large` for this server's limit. Only `ready` files have a `url`.

## 6. Cards

A card is a fixed layout for structured information. Bots, webhooks
and the server post cards. A person typing a message
cannot.

```json
{
  "color": "pink",
  "author": {"name": "CI", "icon_file_id": null},
  "title": "Build #482 failed",
  "url": "https://ci.example/builds/482",
  "text": "`main` · 3 tests failing",
  "fields": [
    {"label": "Branch", "value": "main", "short": true},
    {"label": "Duration", "value": "4m 12s", "short": true}
  ],
  "image_file_id": null,
  "footer": "ci.example",
  "at": "2026-11-13T09:28:10.000Z"
}
```

| Field           | Rule                                                       |
|-----------------|------------------------------------------------------------|
| `color`         | One of `lavender`, `cyan`, `pink`, `yellow`, `green`, `red`, `grey`. Drawn as a bar down the card's edge. Other colours from converted payloads are mapped to the nearest. |
| `author`        | A small line above the title: a name and optional icon.    |
| `title`         | Up to 200 characters, plain text.                          |
| `url`           | Makes the title a link.                                    |
| `text`          | Churr Markdown, inline constructs and lists only, up to 4,000 characters. Mentions in a card do not notify. |
| `fields`        | Up to ten. `short` fields sit two to a row on wide screens. Values are inline Churr Markdown. |
| `image_file_id` | One image, shown under the fields.                         |
| `footer`, `at`  | A small last line.                                         |
| `preview_url`   | Set by the server on a link preview: the address it previews. A card without it was posted by a bot or webhook. |
| `theme`         | Set by the server on a theme card, the preview of a link to one of its own themes (which also has `preview_url`): `{"id", "name", "kind", "colours", "author_id"}`. Clients draw it with its swatches and buttons to try and add the theme (`FEATURES.md` section 23.6). |

`preview_url` and `theme` are the server's. A card a client, bot or
webhook posts with either is refused with `invalid_value` on `cards`.

Every field is optional, but a card must have at least a title, text or
image.

**Images.** A card never makes a client load anything from another
site. `image_file_id` and `icon_file_id` name files on this server.
When a converted payload or a link preview gives an image address, the
server fetches it once under the link-preview rules, re-encodes it and
stores it as a file. If link previews are off or the fetch fails, the
card is posted without the image and with the address in its footer.

**Link previews** are cards the server adds after a message is posted:
the page's title, description, site name and one image. At most three
per message. The author or a channel manager can remove one.

## 7. Forms

A form is how a message asks for input. One per message.

```json
{
  "id": "0193a800-…",
  "preset": "poll",
  "title": "Lunch on Friday?",
  "fields": [
    {
      "id": "choice",
      "type": "choice",
      "label": "Where",
      "required": true,
      "multiple": false,
      "options": [
        {"id": "a", "label": "Noodles"},
        {"id": "b", "label": "Tacos"}
      ]
    }
  ],
  "buttons": [],
  "action": {"type": "tally"},
  "anonymous": false,
  "results": "live",
  "allow_added_options": false,
  "closes_at": "2026-11-14T02:00:00.000Z",
  "closed_at": null,
  "answer_count": 7,
  "tally": {"choice": {"a": 4, "b": 3}},
  "voters": {"choice": {"a": ["0193a001-…"], "b": ["0193a002-…"]}},
  "my_answer": {"choice": ["a"]}
}
```

### Fields

| `type`     | Input                     | Options                                   | Value in an answer      |
|------------|---------------------------|-------------------------------------------|-------------------------|
| `choice`   | Pick from options         | `options`, `multiple`, `min`, `max`       | List of option ids      |
| `text`     | One line                  | `placeholder`, `max_length`               | String                  |
| `longtext` | Several lines             | `placeholder`, `max_length`               | String                  |
| `number`   | A number                  | `min`, `max`, `step`                      | Number                  |
| `date`     | A calendar date           | `min`, `max`                              | `YYYY-MM-DD`            |
| `time`     | A time of day             |                                           | `HH:MM`                 |
| `account`  | Pick people               | `multiple`                                | List of account ids     |
| `conversation` | Pick channels         | `multiple`                                | List of conversation ids |
| `checkbox` | One tick-box              |                                           | `true` or `false`       |

Every field has `id`, `type`, `label`, and optional `help` and
`required`. A `choice` field with up to five options is drawn as
buttons or radio rows in the message; with more, as a menu. Fields
other than a single `choice` are filled in a dialog opened by an
"Answer" button, so the message stays compact.

### Buttons

    {"id": "approve", "label": "Approve", "style": "primary"}

`style` is `default`, `primary` or `danger`. Pressing a button submits
the form's current fields together with the button's `id`. A form with
no fields is just a row of buttons.

### Actions

What the server does with an answer.

| `action.type` | Extra fields               | What happens                               |
|---------------|----------------------------|--------------------------------------------|
| `tally`       |                            | Counts per option are kept on the form and shown according to `results`. This is a poll. |
| `collect`     |                            | Answers are stored. The form's author and the channel's managers can read them and download them as CSV. Others see only `answer_count`. |
| `thread`      |                            | Each answer is posted as a reply in the message's thread, under the answerer's name, as a card listing the fields. |
| `channel`     | `name_prefix`, `group_id`, `level` | Opens a private channel named `<prefix>-<number>` with the answerer and the group in it, and posts the answer as its first message. For tickets. Only a channel manager may post a form with this action. |
| `group`       | On each button: `group_id` and `group_action` (`join` or `leave`) | The button joins or leaves that group. The group must be open. |
| `bot`         |                            | The answer is sent to the bot that posted the form as a `form.answered` event and stored nowhere else. Only a bot may use this action. |

### Settings

| Field                 | Values                                              |
|-----------------------|-----------------------------------------------------|
| `anonymous`           | `false`: answers carry the account. `true`: the API never reveals who answered what, to anyone. The server still stores the account, to hold each person to one answer and let them change it. Cannot be changed after posting. |
| `results`             | `live`, `after_answer` (shown to those who have answered), `after_close`. For `tally` only. |
| `allow_added_options` | Answerers may add an option to a `choice` field. Added options record who added them. |
| `closes_at`           | The form closes itself at this time.                |
| `answerer_group_id`   | Only members of this group may answer or press. Only a manager of the conversation may set it. |
| `one_answer`          | Default `true`: one answer per account, changeable until the form closes. `false` allows many, for `thread` and `channel` actions. |

- Any member of the conversation may answer, readers included. Bots
  may not. Someone reading a public channel without having joined it
  joins first.
- In a conversation shared with other servers only the `tally` action
  is allowed. Answers go, sealed, to the server the form was posted
  on, which keeps the tally.
- Answers to `thread` and `channel` forms cannot be changed once sent.
  `anonymous` is allowed only with `tally` and `collect`.
- The author or a channel manager can close a form. A closed
  form keeps its result on the message and accepts no more answers.
- `tally` and `voters` appear in the object only when `results` allows
  it for the reader. `voters` never appears on an anonymous form.
- `my_answer` is the reader's own answer and is never sent to anyone
  else.
- Editing a message cannot change its form's fields once anyone has
  answered; the author can still close it or delete the message.

### Presets

A preset is a form the client builds from a short command or a dialog.
The server stores an ordinary form; `preset` only tells clients how to
draw it.

| Preset    | Command                                   | Form                                         |
|-----------|-------------------------------------------|----------------------------------------------|
| `poll`    | `/poll Question? \| Option \| Option`     | One `choice` field, `tally`, drawn as bars with counts. |
| `rsvp`    | `/rsvp What \| When`                      | One `choice` field with Going, Maybe, Can't, `tally`, named, drawn with the date and attendee faces. |
| `signup`  | `/signup What \| Slot \| Slot`            | One `choice` field, `tally`, named, with `max` per option as the number of places. |
| `approve` | `/approve What`                           | Buttons Approve and Decline, `tally`, named. Channel managers may restrict who can press. |
| `checkin` | Set up in channel settings                | Several text fields, `thread`, posted on a schedule. |
| `ticket`  | Set up in channel settings                | Text fields, `channel`.                       |
| `roles`   | Set up by an admin                        | One button per open group, `group`.           |
| (none)    | `/form` opens a builder                   | Any fields, `collect`.                        |

### Dialogs for bots

A bot that needs more from one person can answer a command or a button
press by asking the server to show that person a form in a dialog. The
dialog form has the same fields and buttons, always uses the `bot`
action, and is not a message: nobody else sees it and it is gone once
answered or dismissed.

## 8. Reactions

- A reaction is one emoji on one message by one account. The emoji is a
  standard emoji sequence, or `:name:` for a custom one.
- Any member of the conversation may react, readers included.
- A message can carry at most twenty different emoji. Adding a
  twenty-first is refused; adding to one already there is not.
- Skin-tone variants are different emoji.
- Reactions produce `reaction.added` and `reaction.removed` events and
  do not notify anyone.
- Deleting a message removes its reactions.

## 9. Sealed chats

`SEALED.md` is the authority on sealed chats. In outline, a message in a
sealed chat has this shape on the server:

```json
{
  "id": "0193a900-…",
  "conversation_id": "0193a0e0-…",
  "seq": 77,
  "author_id": "0193a000-…",
  "client_id": "0193a8ff-…",
  "sealed": {"key_epoch": 12, "ciphertext": "base64…"},
  "quiet": false,
  "created_at": "2026-11-13T09:30:00.000Z"
}
```

Inside the ciphertext is a JSON object the two people's devices
understand:

    {"kind": "message", "text": "…", "entities": […], "files": […], "parent_id": null, "quote": null}
    {"kind": "message", "text": "waves", …, "action": true}
    {"kind": "message", "text": "at 2026-12-24 18:00", "entities": [{"type": "instant", "at": 3, "len": 16, "time": "2026-12-24T08:00:00.000Z", "precision": "minute", "zone": "Australia/Brisbane"}], …}
    {"kind": "edit", "message_id": "…", "text": "…", "entities": […]}
    {"kind": "delete", "message_id": "…"}
    {"kind": "reaction", "message_id": "…", "emoji": "👍", "on": true}
    {"kind": "task", "message_id": "…", "item_id": "t1", "checked": true}
    {"kind": "history", …}

- References inside the ciphertext (`message_id`, `parent_id`, the
  quote) are the sender-made `client_id` of the message referred to,
  not the server's row identifier, so the server cannot link them.
- `quiet` is the one thing the sender tells the server about the
  content: reactions, ticks, edits and deletes are quiet, so they do
  not notify or count as unread.
- `history` is how one person's device shares keys for epochs the
  other person's new device missed (`SEALED.md`).
- Text is Churr Markdown and is drawn the same way. Entities are worked
  out by the sending device. That includes instants (section 3.1): the
  sender reads a time typed without a zone in its own zone and puts the
  entity in the content; the receiver keeps the sender's instants that
  hold up, since it cannot know the sender's zone.
- Files are encrypted on the device with a random key before upload, in
  chunks. The file entry inside the ciphertext carries that key, the
  real name, the media type and an encrypted thumbnail made by the
  sender. The server stores an unnamed blob.
- There are no cards and no forms, no link previews, and no history
  kept by the server. A delete also erases the deleted message's
  ciphertext on the server.

## 10. What clients must do with something they do not understand

- An unknown `entities[].type` is drawn as the plain text it covers.
- An unknown `form.fields[].type`, `action.type` or `preset` makes the
  whole form undrawable: show `fallback` and a line saying this client
  is too old to answer.
- An unknown `cards[]` field is ignored.
- An unknown `files[].kind` is drawn as `file`.
- An unknown `system.kind` is drawn as "Something happened here".
- An unknown `chance.kind`, deck or card is drawn as `fallback`.

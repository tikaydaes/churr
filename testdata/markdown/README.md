# The Churr Markdown corpus

Cases that every parser of Churr Markdown must pass: the server's
(`internal/markdown`, Go), the web chat's (JavaScript) and the apps'
(Dart). `docs/MESSAGE-FORMAT.md` sections 2 to 4 define the format; this
directory pins down what those sections leave open, as cases.

The Go parser passes all of it (`go test ./internal/markdown`). The
expected values were written by that parser (`-update`) and then read
against the format document case by case; where the document is silent
the rule that was chosen is under "Decisions" below.

## Files

| File | Holds |
|------|-------|
| `resolver.json` | The names that resolve in every case that does not bring its own |
| `blocks.json`, `inline.json`, `links.json`, `mentions.json`, `emoji.json`, `tasks.json`, `code.json`, `tables.json`, `unicode.json`, `edges.json` | Cases, by topic. Each file is a JSON array |

Every `.json` file other than `resolver.json` is a list of cases. A new
file is picked up without registering it anywhere.

## A case

```json
{
  "name": "account mention",
  "note": "optional: why the case exists",
  "text": "hello @sam",
  "resolver": { "...": "optional: replaces resolver.json for this case" },
  "entities": [{"type": "account", "at": 6, "len": 4, "account_id": "0193a000-0000-7000-8000-000000000001"}],
  "tasks": [],
  "plain": "hello @sam",
  "tree": [{"t": "p", "at": 0, "len": 10, "c": [
    {"t": "text", "at": 0, "len": 6, "text": "hello "},
    {"t": "mention", "at": 6, "len": 4, "name": "sam"}]}]
}
```

| Field | Meaning |
|-------|---------|
| `name` | Unique across the whole corpus |
| `note` | For people. Parsers ignore it |
| `text` | The input, exactly as an author sent it |
| `resolver` | The names that resolve, in the shape of `resolver.json`. Absent means `resolver.json` |
| `entities` | What the server stores: the entity objects of MESSAGE-FORMAT.md section 3, in order of `at`, never overlapping. Task entities carry `item_id` `t1`, `t2`, … in order, as a newly posted message gets |
| `tasks` | The checklist items in order: `at` and `len` (the item's text after the box, the same range as its task entity), `text` (that text) and `checked` (what the author typed) |
| `plain` | The plain text used for search, fallbacks and notification bodies |
| `tree` | The parse tree, in the compact form below |

A client parser draws from the tree and the entities the server sent; it
never resolves names itself. So a client implementation compares its
`tree` (and, if it has one, its plain text) with the case, and may skip
`entities`, or check that drawing the tree with the case's `entities`
highlights exactly the ranges they cover. The server implementation
checks all four.

## The resolver

```json
{
  "accounts": {"sam": "<account id>"},
  "remote_accounts": {"mia@other.example": "<account id>"},
  "groups": {"everyone": "<group id>", "design": "<group id>"},
  "broadcasts": true,
  "channels": {"<conversation id>": "ops"},
  "emoji": {"party": "<emoji id>", "wave@other.example": "<emoji id>"},
  "zone": "Australia/Brisbane",
  "patterns": [{"id": "<pattern id>", "prefix": "PROJ-", "url_template": "https://t.example/{value}", "created_at": "…"}],
  "object_hints": [{"start": 0, "end": 6, "kind": "plain"}],
  "origin": "https://chat.example"
}
```

`zone` is the author's IANA zone, as a posting client sends it, in which
a date and time typed without a zone is read (ruling R69). Absent means
the client sent none, and such text stays plain.

`patterns` are the reference patterns that apply where the text is
written, and `object_hints` what the author chose in the composer
(ruling R73, `MESSAGE-FORMAT.md` 3.2). Objects are not nodes of the
tree: they are entities that lie inside one `text` node, so the tree of
a case is the same with or without them. `origin` is the server's own
origin, for links to its messages. A client runs its own object scanner
over each case with these fields and compares what it finds with the
case's entities of the object types.

### Channel links

A channel is linked in text by its identifier, never by its name:
`<#0193a000-0000-7000-8000-000000000020>`, which is `<#`, the identifier
exactly as identifiers are written everywhere (lower case, with hyphens)
and `>`. Anything else that starts `<#` is plain text, and so is `#ops`
typed as plain characters: a name is never looked up, by a server or by a
client.

`channels` maps the identifier of each channel the author can see to the
channel's name. A link whose identifier is listed gets a `conversation`
entity covering the whole `<#…>`, and the plain text has the name in its
place. A link whose identifier is not listed gets no entity, stays in the
text as written, and is `unknown channel` in the plain text; a client
draws it the same way. The tree has a `channel` node either way.
`channels.json` holds the cases.

A name that is not listed does not resolve, and the text stays plain
text. `broadcasts` says whether `@channel` and `@here` may be used.
Where they may not, `@everyone` is left out of `groups` too, which is
how a server limits broadcast mentions to managers.

## Positions

`at` and `len` count UTF-16 code units from the start of `text`, which
is how JavaScript and Dart index strings. A character outside the Basic
Multilingual Plane (most emoji) counts two; a `\r` counts one; nothing is
normalised first.

## The tree

`tree` is a list of block nodes. Blocks and inline spans share one
shape, and a field is present only where it means something. Every node
has `t` (its kind), `at` and `len`.

Blocks:

| `t` | Other fields | Covers |
|-----|--------------|--------|
| `p` | `c`: inline spans | A paragraph |
| `h` | `level` 1 to 3, `c` | A heading, marks included |
| `quote` | `b`: blocks | A quote |
| `list` | `kind` `bullet` or `numbered`, `start` (numbered only), `items` | A list |
| `item` (inside `items`) | `checked` (present only on a checklist item), `c`, `b`: nested lists | From the marker to the end of the item's own line |
| `codeblock` | `lang` (the canonical language, absent for none), `text` (the code) | From the opening fence to the closing one |
| `table` | `align` per column (`none`, `left`, `center`, `right`), `head`: cells, `rows`: rows of cells | The whole table |
| `cell` (inside `head` and `rows`) | `c` | A cell's content without the spaces around it |
| `rule` | | A `---` line |

Inline spans:

| `t` | Other fields | Written as |
|-----|--------------|------------|
| `text` | `text` | Plain characters |
| `bold`, `italic`, `strike`, `spoiler` | `c` | `**…**`, `*…*` or `_…_`, `~~…~~`, `\|\|…\|\|`; `at` and `len` include the marks |
| `code` | `text` (without the backticks) | `` `…` `` |
| `link` | `url`, `c` | `[text](address)` with an `http`, `https`, `mailto` or `tel` address |
| `nolink` | `url`, `c` | `[text](address)` with any other address: drawn as its text |
| `image` | `url`, `c` | `![alt](address)`: drawn as a link to the address |
| `url` | `url` | A bare `https://…` or `http://…` |
| `mention` | `name`, `server` (for `@name@server`) | A candidate mention, not yet looked up |
| `channel` | `id` | `<#conversation-id>`, not yet looked up |
| `emoji` | `name`, `server` | `:name:` or `:name@server:`, not yet looked up |
| `br` | | A line break inside a paragraph |
| `esc` | `ch` | A backslash and the punctuation character it escapes |
| `instant` | `local`, `zone` | A date and time (`MESSAGE-FORMAT.md` 3.1): `local` is `YYYY-MM-DDTHH:MM[:SS[.f]]` (always with `T`), `zone` exactly as written, absent for none. A candidate: the resolver decides whether it is an entity |

`mention`, `channel` and `emoji` nodes are candidates: the parser makes
them from the shape of the text alone. Whether one becomes an entity is
the resolver's answer. One that does not resolve is drawn as the text it
covers.

## Running the corpus in another implementation

1. Read `resolver.json`.
2. For each other `.json` file, for each case: parse `text`; turn the
   result into the compact tree; compare with `tree` as JSON values
   (object key order does not matter; absent and empty are the same for
   `c`, `b`, `items`).
3. If the implementation resolves entities, resolve with the case's
   resolver (or the default) and compare with `entities`; compare the
   checklist items with `tasks` and the plain text with `plain`.
4. Fail if the corpus holds fewer than 60 cases, so a wrong path cannot
   pass by finding nothing.

To change a rule: change the parser, run
`go test ./internal/markdown -run TestCorpus -update` to rewrite the
expected fields, and read the diff. `-dump` prints every case with the
text each node and entity covers, for reading expectations by eye.

## Decisions

Where MESSAGE-FORMAT.md leaves an edge open, the simplest rule
consistent with it was chosen. Each has a case.

| Edge | Rule |
|------|------|
| Entities inside a checklist item | A task entity covers the item's whole text, and entities never overlap, so a mention, channel, emoji or bare link inside a checklist item is not an entity. It is drawn from the tree as plain text and notifies nobody. (This follows from two statements of the format document; it is reported as something the document should settle.) |
| Where a bare link ends | At white space. Trailing `.`, `,`, `!`, `?`, `:`, `;`, quotes and a closing bracket with no opening one inside the address are punctuation and not part of it |
| What ends a mention | The name rules of CONVENTIONS.md; a trailing `.`, `-` or `_` followed by white space or the end is punctuation, so `@sam.` mentions `sam` |
| Where a mention may start | At the start of the text or after a character that is not a letter or digit in any script, so `a@b` and `日@sam` are not mentions. A channel link is delimited by its own brackets and may stand anywhere |
| `@a@b` | Read as the remote form `@username@server`. If that does not resolve it stays plain text; it is not read as two mentions |
| Standard emoji short codes (`:smile:`) | No entity: the entity table has custom emoji only. The text stays as typed and `plain` keeps it |
| Colons in times and ratios | `10:30:45` is not an emoji candidate: a short code must not touch a letter or digit on either side |
| An unterminated mark (`**`, `` ` ``, `\|\|`, a code fence) | Shown as typed. What follows is parsed as usual |
| `_` inside a word | Not emphasis: `_` works only at word edges |
| A checklist box with no text after it | Not a checklist item; the box is shown as typed |
| `- [X]` | Ticked, the same as `[x]` |
| Lists deeper than three levels | The deeper items join the third level |
| Quotes deeper than three levels | A `>` inside the third quote is plain text |
| Headings | A `#` run needs a space after it. Trailing `#` are text |
| Rules | `---` (three or more dashes) alone on a line. `***` and `- - -` are not rules |
| A table inside a list, a table with no `---` row, a table over 20 columns or 200 rows | Shown as typed |
| Language of a code block | Matched without regard to case against the list and aliases of MESSAGE-FORMAT.md 2.3; `lang` is the canonical name; an unknown one is no language |
| CRLF | A `\r\n` ends a line like `\n`; positions still count the `\r` |
| References (`references.json`) | A pattern's prefix, exactly, then the longest value its expression matches, standing on the object edges of `MESSAGE-FORMAT.md` 3.2; a channel's pattern before the server's, then the longer prefix, then the older; not in code, links, bare links, escapes or checklist items; a `plain` hint keeps a range, and anything reaching into it, as text; a hint that fits nothing is ignored. A `plain` hint over a typed instant keeps it plain too (`instants.json`) |
| Message links (`message-links.json`) | A bare link, or `churr://` and the origin's host in a run of text, then exactly `/chat/c/{id}/m/{id}`, `/t/{id}` or `/t/{id}/m/{id}` with lower-case identifiers and nothing after; the scheme and host are matched without regard to case, a port must match too; the bare link's `link` entity becomes the `message_link`; a Markdown link stays a link; a `plain` hint keeps it a `link` |
| Measurements (`measures.json`) | A number, at most one space (U+0020, U+00A0, U+202F), and a unit of the fixed table exactly as listed, longest first, on the edges; `in` only with no space; only a temperature negative; not after a digit and a point or comma |
| Instants (`instants.json`) | The grammar of `MESSAGE-FORMAT.md` 3.1 with its word boundaries. A `-` offset needs `T` or seconds, so `10:00-11:30` stays a plain range; a zone abbreviation or a spaced offset after the time leaves it plain; a time the zone skips is no entity, one it shows twice is the first; no entity inside a checklist item, as for mentions |
| Plain text | One line per block; a line break inside a paragraph becomes a space; marks are dropped; a link is its text, an image its address; table cells are joined with ` \| `; mentions are written as typed; a channel link is written as the channel's name at that moment, or `unknown channel` |

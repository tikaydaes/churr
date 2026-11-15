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
| `action`          | Present and true when the text says what the author is doing, typed with `/me` (section 1.1). |
| `chance`          | A result the server drew for `/roll`, `/flip`, `/pick`, `/shuffle`, `/draw`, `/mahjongg` or `/tarot` (section 1.2). |
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
No edit can change it, so every reader sees a result nobody made up. A
message with a result has no text, files, cards or form when posted, and
its `fallback` is the result in plain English ("Rolled 2d6+3: [4, 2] + 3
= 9"), which old clients show and search reads. People only: a bot's
request is refused. Never in a sealed chat, which the server cannot
write into. Deleting the message removes the result.

| `kind`    | Asked with                       | Stored                                                   |
|-----------|----------------------------------|----------------------------------------------------------|
| `roll`    | `dice`: the expression (`FEATURES.md` section 12.4) | `dice` as read (`d20` is `1d20`, `adv` is `2d20kh1`), `terms` (each `{sign, count, sides, keep, keep_n, rolls, dropped, value}` for dice or `{sign, value}` for a number), `total` |
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

**How a result is drawn.** A panel with the cut corner, a tag "drawn by
the server", and one slot per die, coin, pick, shuffled place or card:
mono numerals on the panel fill with an edge; a dropped die struck
through with the word "dropped"; a roll's total after `=`; a draw's and a
shuffle's names listed under the slots; a tarot reading as a list of
positions, each with its card, "upright" or "reversed", and its meaning,
then the reading's overall energy and a line that it is for fun.

**The one animation.** When the message arrives live for the reader
(never when it is loaded from history or met on scrolling back), each
slot spins once like a slot-machine reel through values of its own kind
(faces of that die, the deck's cards, the options) and stops on the
server's value, left to right, in about 1.2 seconds. More than ten slots
get one short flurry together. With the platform's reduced-motion
setting the result simply appears. Pressing the result spins it again.
The animation decides nothing: it changes only the text of each slot,
each slot keeps the width of the longest value it can show, and its last
frame is the static drawing.

### 1.3 Messages the server posts

Messages the server posts itself (someone joined, a call started, a
thread moved to a channel) have `via.kind` of `system`, an `author_id`
of the account concerned, and a `system` object in place of text:
`{"kind": "member_joined" | "member_left" | "call_started" | "call_ended" | "thread_moved" | "channel_renamed" | "pinned", …}`.
Clients word these themselves, so they follow the reader's language.

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
| `channel`     | `name_prefix`, `group_id`, `level` | Opens a private channel named `<prefix>-<number>` with the answerer and the group in it, and posts the answer as its first message. For tickets. Only a channel manager or admin may post a form with this action. |
| `group`       | On each button: `group_id` and `group_action` (`join` or `leave`) | The button joins or leaves that group. The group must be open. |
| `bot`         |                            | The answer is sent to the bot that posted the form as a `form.answered` event and stored nowhere else. Only a bot may use this action. |

### Settings

| Field                 | Values                                              |
|-----------------------|-----------------------------------------------------|
| `anonymous`           | `false`: answers carry the account. `true`: the API never reveals who answered what, to anyone. The server still stores the account, to hold each person to one answer and let them change it. Cannot be changed after posting. |
| `results`             | `live`, `after_answer` (shown to those who have answered), `after_close`. For `tally` only. |
| `allow_added_options` | Answerers may add an option to a `choice` field. Added options record who added them. |
| `closes_at`           | The form closes itself at this time.                |
| `answerer_group_id`   | Only members of this group may answer or press. Only an account acting as manager of the conversation may set it. |
| `one_answer`          | Default `true`: one answer per account, changeable until the form closes. `false` allows many, for `thread` and `channel` actions. |

- Any member of the conversation may answer, readers included. Bots
  may not. Someone reading a public channel without having joined it
  joins first.
- In a conversation shared with other servers only the `tally` action
  is allowed. Answers go, sealed, to the server the form was posted
  on, which keeps the tally.
- Answers to `thread` and `channel` forms cannot be changed once sent.
  `anonymous` is allowed only with `tally` and `collect`.
- The author, a channel manager or an admin can close a form. A closed
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
  out by the sending device.
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

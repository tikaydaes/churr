# Churr style

The reference for anyone building a Churr screen: the web pages, the web
chat and the apps. It writes out ruling R60 and the approved guide,
`docs/style-guide.html` (open it in a browser to see every piece live).
Where this file and the guide disagree, the guide wins; say so and fix
this file.

On the web, all of this lives in one token file,
`internal/web/static/shared/tokens.css`, and the shared pieces in
`shared/base.css`, `shared/icons.js` and `chat/chat.css`. Nothing names a
colour outside the token file.

## The rules in one screen

1. **Cut, don't round.** No rounded corners anywhere. Things that matter
   lose their top-right corner instead: one cut per object.
2. **Flat and hard.** Solid colour only. No gradients, no blur, no glass, no
   soft shadows. Depth is a hard offset block.
3. **Fills, not frames.** Areas are told apart by steps of fill (ground,
   panel, raised), not by boxes and rules. A coloured bar on the left edge
   marks what is selected or yours.
4. **Stickers sit crooked.** Tags, badges and the "new" marker are rotated
   1 to 4 degrees. Reading text never is.
5. **Glitch is a treat.** The split-colour glitch is for the wordmark, the
   start-up screen and empty states, never for text you have to read.
6. **No terminal props.** No prompts (`>`, `$`, `./`), no bracketed buttons,
   no tree lines, no monospace headings or section names.
7. **Colour never works alone.** Every colour that means something comes
   with a shape or a word.
8. **Four neons, each with one job.** No fifth accent colour; states reuse
   the four.
9. **Nothing from other people's servers.** Fonts, icons and scripts ship
   with the server and the apps.

## Colour

### The roles

| Colour | Job |
| --- | --- |
| Lavender | the brand and "yours": your mentions, your choices, primary structure |
| Cyan | live and go: links, the main action, online, the current channel, the cursor |
| Pink | wants attention: new messages, problems, destructive actions, do not disturb |
| Yellow | counts and points: unread counts, keyboard focus, highlights, away |
| Ground, panel, raised | the room: three steps that do all the layering |

Semantic states reuse these: success and "on" are cyan, failure and danger
are pink, waiting is yellow. There is no green and no red: the red suits
of a playing card (hearts, diamonds, the red joker) are pink too.

### Tokens

Every fill has a partner text colour, chosen by measurement, never by eye.
Use the pair together.

| Token | Use | Dark | Light |
| --- | --- | --- | --- |
| `--bg` | ground: page, message log, inputs | `#0d1026` | `#f5f2ff` |
| `--panel` | panel: sidebar, cards, headers | `#161a3d` | `#ffffff` |
| `--raise` | raised: hover, selected, code, secondary buttons | `#1d2250` | `#ebe6ff` |
| `--line` | the rare line (a divider in a menu, `---` in a message) | `#2c3170` | `#c9c1ee` |
| `--grid` | the dot grid behind empty space | `#1d2250` | `#dcd5f7` |
| `--ink` | text | `#efeaff` | `#15173a` |
| `--dim` | secondary text, icons | `#a09dc9` | `#55527f` |
| `--faint` | section names, times, placeholders (ground and panel only) | `#8b88b8` | `#6b6894` |
| `--edge` | the edge of a control: inputs, switches, the line under a button | `#6368bd` | `#7e73c2` |
| `--lav` | lavender | `#b9a0ff` | `#5b3fd6` |
| `--on-lav` | text on lavender | `#0d1026` | `#ffffff` |
| `--cyan` | cyan | `#5df0e4` | `#006e66` |
| `--on-cyan` | text on cyan | `#0d1026` | `#ffffff` |
| `--pink` | pink | `#ff5ccf` | `#b3127f` |
| `--on-pink` | text on pink | `#0d1026` | `#ffffff` |
| `--yel` | yellow, as a fill only | `#ffd95e` | `#ffd95e` |
| `--on-yel` | text on yellow | `#15173a` | `#15173a` |
| `--yel-ink` | yellow where it must be text or a thin line | `#ffd95e` | `#7a5600` |
| `--on` | text on any neon, in general | `#0d1026` | `#ffffff` |
| `--paper` | the opposite of the ground | `#f5f2ff` | `#15173a` |

Web-only helpers in the same file: `--scrim` (behind a dialog), `--halo`
(the dark edge of the focus ring on light; none on dark), `--sep` (a line
between areas, only when contrast is increased), `--chev` (the drawn arrow
of a select).

**Yellow is never text on light.** Where yellow text is wanted (the count
in a "2 new replies" marker, a string in code, a link warning) use
`--yel-ink`, which is yellow on dark and a dark amber on light.

### The high-distinction palette

A setting. It swaps pink for orange and lavender for blue, so the four roles
differ along blue to yellow and in lightness, which red-green colour
blindness leaves intact. Only these tokens change; everything else is as
above.

| Token | Dark | Light |
| --- | --- | --- |
| `--lav` | `#7fb2ff` | `#1f56c4` |
| `--cyan` | (unchanged) `#5df0e4` | `#00626b` |
| `--pink` | `#ff9a4d` | `#a8480a` |
| `--yel` | `#ffe066` | `#ffe066` |
| `--yel-ink` | `#ffe066` | (unchanged) `#7a5600` |

### Increase contrast

Follows the system's "increase contrast" unless set. Every line takes the
edge colour (`--line: var(--edge)`), faint text becomes dim, and a line in
the edge colour is drawn where areas meet (`--sep`).

### Themes and settings

Both themes follow the guide; dark is the default. Pages follow the
system's light or dark. Each browser can override it, and choose the
palette and contrast, in **Web chat settings** or **Account → Appearance**.
The choice is kept in the browser only (`localStorage["churr-look"]`), as
the other web preferences are, and is applied by `shared/look.js` as
attributes on `<html>`: `data-theme`, `data-palette="hd"`,
`data-contrast`. The apps offer the same three settings.

## Contrast

| What | Needs |
| --- | --- |
| Text against what it sits on | 4.5 : 1 |
| Large headings | 3 : 1 (we still aim for 4.5) |
| Edges of controls, icons, presence shapes, the selected bar | 3 : 1 |
| The focus ring against its surroundings | 3 : 1 |

`e2e/contrast.mjs` reads the token file and checks every pairing the pages
use in all eight sets (dark or light, standard or high distinction, normal
or increased contrast); it fails on any pair below its requirement and is
part of `e2e/run.sh`. Add a pair there when a screen puts a token on a
surface it has not stood on before.

Lowest margins today, so you know where the edges are: `--edge` on
`--raise` (3.0 dark), `--faint` on `--bg` (4.7 light). `--faint` on
`--raise` fails in light (4.3), which is why faint is never text on the
raised step: a hovered or selected row lifts it to `--dim`.

Opacity is not a way to dim text. A conversation set to notify nothing is
drawn in `--faint`, not at half opacity.

## Themes

Ruling R61. A theme is a name, a kind (`dark` or `light`) and nine base
colours, the only ones a theme stores, each six hex digits:

| Key | Token |
| --- | --- |
| `bg`, `panel`, `raise` | the three ground steps |
| `ink`, `dim` | text and secondary text |
| `lav`, `cyan`, `pink`, `yel` | the four roles (`yel` as a fill) |

Shape, type, layout and motion are not themeable. The built-in themes
(dark, light and their high-distinction pair) are the tables above and
are not derived; every other theme is, by exactly these rules, which the
web chat, the apps and the server (`internal/themes/palette`) all follow.
Anything a theme does not name (`--line`, `--grid`, `--on`, `--paper`,
the web-only helpers) is taken from the built-in theme of the same kind.

**Arithmetic.** Every implementation gets the same float64 (IEEE 754
binary64, round to nearest, ties to even) for every luminance and ratio,
bit for bit, on every machine, and so the same decision. That holds
because each step below is a single correctly rounded operation, done in
the order written, and nothing else takes part: no power function, no
fused multiply-add, no reordering, no wider intermediate.

- A colour is three channels, 0 to 255.
- Linear light of a channel `v` is a table of 256 float64s, not a
  computation. Entry `v` is the exact real value of the WCAG 2 step,
  with `x = v / 255`: `x / 12.92` when `x <= 0.03928` (that is,
  `v <= 10`), otherwise `((x + 0.055) / 1.055) ^ 2.4`, rounded once to
  the nearest float64. Equivalently `v / 3294.6`, or `q ^ (12/5)` with
  `q = (1000 v + 14025) / 269025`. The table is written into each
  implementation's source as literals (the shortest decimal that reads
  back as that float64, which every language here reads exactly), the
  same text in all three: `internal/themes/palette/palette.go`
  (`linear`), `shared/palette.js` (`LINEAR`), `core/themes/palette.dart`
  (`_linear`). `TestLinearTable` in the Go package is the generator: it
  recomputes every entry in exact rational arithmetic (the fifth root of
  `q ^ 12` at 320 bits) and prints the table if one differs. Entry 0 is
  0 and entry 255 is 1.
- Relative luminance, as four roundings in this order:
  `r = 0.2126 × lin(R)`, `g = 0.7152 × lin(G)`, `b = 0.0722 × lin(B)`
  (each product rounded on its own), then `L = (r + g) + b`. Go fuses
  `x × y + z` into one fused multiply-add on some processors (arm64)
  unless the product is converted, so the Go code writes
  `float64(0.2126 * lin)` for each product; JavaScript and Dart never
  fuse, and round every operation to float64.
- Contrast of two colours: `(Lhigh + 0.05) / (Llow + 0.05)`, the two
  sums rounded, then the quotient rounded; no product, so nothing to
  fuse. Compared unrounded (as a float64) with what a pairing needs
  (`>=`). Shown cut (not rounded) to two decimals, `floor(ratio × 100)`,
  so a failing pair never reads 4.50.
- Comparisons in exact integer or rational arithmetic were considered and
  not taken: the ratio is shown and sent as a number, and with the
  steps above the float64 result is already the same everywhere, so it
  is the definition. The three test suites require bit-for-bit equal
  luminances and ratios against the Go package's own answers
  (`internal/web/testdata/palette_cases.json`,
  `app/test/core/themes/palette_corpus.json`).
- `mix(a, b, k)`, `b` laid over `a` at `k` per cent for a whole number
  `k` from 0 to 100, channel by channel in whole numbers:
  `floor((a * (100 - k) + b * k + 50) / 100)`.
- "The smallest k that reaches N against S" means: try `k = 0, 1, …, 100`
  in turn and take the first whose colour has contrast of at least N
  with every surface in S. If none does, use `k = 100`.

**Derived colours.**

| Token | Rule |
| --- | --- |
| `--on-lav`, `--on-cyan`, `--on-pink`, `--on-yel` (and `--on`, as `--on-lav`) | `ink` or `bg`, whichever has the higher contrast with the fill; `ink` on a tie |
| `--faint` | `mix(bg, dim, k)` for the smallest k that reaches 4.5 against `bg` and `panel` |
| `--edge` | `mix(bg, dim, k)` for the smallest k that reaches 3 against `bg`, `panel` and `raise` |
| `--yel-ink` | `mix(yel, ink, k)` for the smallest k that reaches 4.5 against `bg`, `panel` and `raise` (so `yel` itself when it already does) |

Increase contrast then applies to a theme as to the built-in ones.

**The check.** A theme is valid when its kind fits (a dark theme's `bg`
has lower luminance than its `ink`; a light theme's higher) and every
pairing below reaches its need, checked in this order; the first that
does not is the one reported, as "dim on raise is 4.06:1; text needs at
least 4.5:1.". A theme that fails cannot be saved, shared or applied.

Every role colour that either client draws as text needs 4.5 against
`bg`, `panel` and `raise`. Today that is:

| Colour | Drawn as text in the web chat | Drawn as text in the apps |
| --- | --- | --- |
| `cyan` | links, channel chips, thread links, your own form answers, code numbers | links, notes, code numbers and names of variables |
| `pink` | problems, a failed send, danger in a menu, code keywords and removed lines, the red suits of a drawn playing card | errors, "remove" and "leave", danger, code keywords, the red suits of a drawn playing card |
| `yel-ink` (yellow as text) | private-message marks, the real address of a link, code strings | status words, warnings, code strings |
| `lav` | code function names, the tick of the chosen theme | other people's mentions, channel names in a message, search and notification places, unread replies, two settings labels, code function and type names |

`yel` itself is never text (it is a fill, with `on-yel` on it, and on
dark the focus ring); text on any fill is its `on-` colour. A new use of a
role colour as text adds it here and to the check.

| Need | Pairings (fore on back) |
| --- | --- |
| Text, 4.5 | `ink`, `dim` on `bg`, `panel`, `raise`; `faint` on `bg`, `panel`; `cyan`, `pink`, `yel-ink`, `lav` on `bg`, `panel`, `raise`; `on-lav` on `lav`; `on-cyan` on `cyan`; `on-pink` on `pink`; `on-yel` on `yel` |
| Control edge, 3 | `edge` on `bg`, `panel`, `raise`; `faint` on `raise`; on a dark theme only, `yel` on `bg`, `panel`, `raise` (the focus ring; on light it carries a dark halo) |

Lavender's icons and edges need only 3; its text pairings cover them.

The guide's dark and light colours pass as themes; their derived
`--faint`, `--edge` and `--yel-ink` differ a little from the hand-set
built-in values, which is expected.

**Sharing.** A theme saved on a server has a link, `{origin}/themes/{id}`;
in a message it is drawn as a theme card (swatches, name, author, "try",
"add"). Between servers a theme travels as a `.churrtheme` file: JSON
`{"name", "kind", "colours"}` with the keys above, drawn as a theme card
when attached.

## Colour never alone

| Meaning | The shape or word that goes with the colour |
| --- | --- |
| Online | a solid square |
| Away | a hollow square |
| Do not disturb | a diamond (a square on its point) |
| Offline | a dotted hollow square |
| Sealed chat | a shield in place of the square: solid online, outline away, solid with a bar for do not disturb, dotted outline offline |
| Unread | bold, plus a count when it notifies |
| New messages | the word NEW on the pink line |
| Your vote, your reaction | a tick, a bar and bold (vote); a thick edge and bold (reaction) |
| A mention of you | a filled block with the `@` |
| Notifies nothing / muted | dimmed, plus a struck bell |
| Hidden | the word "hidden" as a tag, and the name in italic |
| Tags | always a word ("private", "failed", "on") |
| Problem | the word "Problem" before the message |

## Type

Two open-licensed families (SIL OFL), shipped with the server and the
apps.

- **Archivo** does the talking: messages, names, headings, buttons,
  labels. One variable font with a width axis, so headings go narrow and
  loud and body text stays normal.
- **Martian Mono** only for times, counts, code, small tags and states
  such as `connected`. Never for headings, buttons, section names or lists.
- Message text is 16px and never mono, never uppercase, never tilted.

| Role | Setting | Used for |
| --- | --- | --- |
| display | Archivo 900, width 70–75%, caps, line 0.92 | page titles, empty states |
| title | Archivo 800, width 85%, 1.15–1.5rem | conversation title, card titles, community name (900, 78%, caps) |
| body | Archivo 400, 16px, line 1.5 | messages and prose |
| label | Archivo 700, width 85%, 11–12px, caps, tracking 0.08–0.1em | field labels, section names, the day |
| meta | Martian Mono 400, 10–11.5px | times, "edited", counts, connection state |
| code | Martian Mono 400, 13px | code, with a cyan bar on its left |
| tag | Martian Mono 700, 10px, caps, tracking 0.06em | tags and stickers |

## Shape

- **The cut corner (notch).** The top-right corner cut at 45°, 6 to 8px
  (14px on the sign-in card). One per object, on things that matter:
  avatars, primary and danger buttons, cards from bots, file cards,
  recovery codes, the sign-in card. Never on plain containers.
  `clip-path: polygon(0 0, calc(100% - 8px) 0, 100% 8px, 100% 100%, 0 100%)`.
  A clipped element would clip its focus ring, so a notched button is
  drawn whole while it has keyboard focus.
- **The hard offset block.** Anything that floats (menus, dialogs,
  popovers, toasts, a message's tools) gets a 2px ink edge and a solid
  block offset 4px down and right in lavender. Never a blur.
- **Dashed is provisional.** Only things that are unsent, pending, drafts,
  previews or drop targets are dashed. Nothing settled is dashed.
- **Crooked tags.** Tags and badges rotate 1 to 4 degrees, alternating
  direction by colour. The unread count is a yellow price tag: tight,
  Archivo 900 narrow, a 2px ink edge, rotated −4°.
- **The dot grid** sits behind empty space only (sign-in, empty states,
  the start of a conversation), never behind text.
- **Fills, not frames.** Lists and tables are fills with 2px of the step
  behind showing between rows, not rules. A coloured bar on the left edge
  (4px, `inset` shadow) marks the current item (cyan), yours (lavender), a
  marker (yellow) or a problem (pink).

## Icons

- A 24px grid, a 2px line, square line ends, sharp (mitre) joins.
- Corners cut at 45° instead of rounded. Circles only where the thing is
  round: a clock, a face.
- Outline only. A solid fill means "on": a pinned pin, a starred star.
- A public channel is `#`; a private channel a small padlock with a solid
  body; a locked channel an outline padlock with a keyhole; a sealed chat a
  shield.
- On the web every icon is inline SVG from one module,
  `shared/icons.js`, starting from the guide's fourteen (chat, thread,
  bell, bookmark, search, pin, plus, send, edit, attach, call, sealed,
  settings, star). Draw a new icon there, to these rules; never load one.
- An icon on its own is a button with an `aria-label`; an icon beside a
  word is hidden from screen readers.

## Controls

| Control | How it looks |
| --- | --- |
| Main action | a cyan block with the notch, Archivo 800 narrow caps; pink on hover and nudged 1px, as if pressed |
| Lavender and danger actions | the same, in lavender or pink |
| Secondary action | a quiet raised block with a 2px edge line under it; the line turns cyan on hover |
| Quiet action | no block until hovered |
| Icon button | no block until hovered; pressed shows a raised block with a lavender line |
| Text field | ground fill, a 2px edge all round, Archivo 15px; label above in the label style |
| Select | as a text field, with a drawn chevron |
| Tick box | a 20px square; ticked is lavender with a dark tick |
| Radio | a diamond; chosen has a lavender square inside |
| Switch | a 40×20 track with an edge; a grey square on the left when off, a cyan square on the right and a cyan edge when on |
| Message box | a panel with an edge line under it; the text caret is cyan |
| Tabs | raised blocks; the chosen one has a cyan line under it |
| Focus | always a 3px yellow outline, offset 3px (inset 3px on list rows). On light it carries a dark halo so it stands 3:1 against the paper |
| Movement | quick and stepped, about a tenth of a second. Nothing eases or bounces. Under "reduce motion" nothing moves |

## The channel list

The guide's "all together" example is the reference. In the web chat it is
`chat/list.js`.

- **Top:** the community name (Archivo 900 narrow caps) and the
  connection as a presence light with a word. Then Jump to, Notifications,
  Threads, Bookmarks, Search, Scheduled, with counts as price tags.
- **Sections** (Starred, the admin's sections, Chats, Sealed): a name in
  the label style with a drawn chevron before it and the number of
  conversations after it. The whole width is the button. Folding a
  section keeps its unread rows and the open conversation in view.
- **Sub-channels** are simply indented under their parent. No tree lines.
- **Parents** fold with a drawn chevron in the left margin; the whole
  margin, the full height of the row, is its target. A folded parent says
  "N more" (a small tag with an edge; pressing it unfolds) and keeps its
  unread sub-channels, and the open one, under it.
- **A sub-channel shown without its parent's row** (its parent is folded
  away with its section, or has no row) stands at the parent's level and
  reads "parent › name", the parent part in dim.
- **One column.** Every row's glyph and name start at the same place: the
  fold chevron sits in the margin, never in the row.
- **Rows:** `#` for public, the padlock for private, a presence square for
  a chat with one person, a shield for a sealed chat. Dim by default;
  unread is ink and bold, with a yellow count when it notifies; the open
  conversation is raised with a cyan bar on the left; a conversation set
  to notify nothing, or muted, is faint with a struck bell.
- **Hidden** conversations have no row until "Show hidden" (a switch under
  the list) is on; then they are drawn in italic with a "hidden" tag.
- **You** are at the foot, on a raised block: avatar, name, status with
  your presence shape, do not disturb, the account menu.
- **Keyboard:** Tab reaches every control. In the list, Up and Down move
  between section names and rows, Home and End go to the ends, Right
  opens and Left folds (a section from its name, a parent from its row),
  Left on a sub-channel goes to its parent, Enter or Space opens a row or
  presses a button. Folding keeps focus where it was.

## Chance artwork

Playing cards, tarot, hanafuda, mahjong tiles and dice in chance results
are drawn from bitmap sprite sheets in their own fixed colours, the same
on every theme. `docs/ART.md` is their art direction and how they are
made.

## Do and don't

Do: use the four neons for their jobs and nothing else; cut one corner on
things that matter; keep mono for times, counts, code and tags; give every
floating thing a hard offset block; tilt badges and tags a little; say
what a thing does not do, in plain words.

Don't: round a corner, blur a shadow or fade a gradient; tilt, glitch or
uppercase anything people read at length; use colour alone to carry
meaning; add a fifth accent colour; box things in with bordered panels,
bracketed buttons, prompts or tree lines; animate with easing or bounce;
load a font, icon or script from someone else's server.

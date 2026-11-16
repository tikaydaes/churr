# Chance artwork

The pictures of what Churr's chance commands show (ruling R81): playing
cards of every deck, tarot, hanafuda, mahjong tiles and dice. They are
generated artwork, made with Google's Nano Banana image models through the
`nano-banana` command, and used exactly as generated: bitmaps, never traced
or turned into vectors. The ranks on cards and the numbers on dice are not
drawn by the model; a script and the clients set them in Archivo, so every
one is the same.

The tools are in `tools/art-gen/`. The sheets they make are not in the
tree until real artwork has been packed: with no sheets, both clients draw
chance results as text, as they always have.

## A card is its own object

A card, tile or die looks the same on every theme. Its colours are fixed,
not theme tokens, taken from `docs/style-guide.html`:

| Role | Colour | Where it comes from |
| --- | --- | --- |
| Paper, the face | `#f5f2ff` | the built-in light theme's ground |
| Ink, every line and dark area | `#0d1026` | the built-in dark theme's ground |
| Lavender | `#b9a0ff` | the brand neon, built-in value |
| Cyan | `#5df0e4` | the brand neon, built-in value |
| Pink | `#ff5ccf` | the brand neon, built-in value |
| Yellow | `#ffd95e` | the brand neon, built-in value |
| Suit red | `#d81e2c` | true red, for suits only |
| Suit black | `#111111` | true black, for suits only |

The pictures use the first six and nothing else: no white, grey, tints,
and no pink turned red. Suit red and suit black are for suits alone:

- every stamped index (rank and small suit mark) and every die number;
  suit red for hearts, diamonds and the red joker, suit black for
  everything else, including every deck without a red and black
  convention (tarot, Latin, German, Swiss);
- the suit marks drawn on French cards (pips, the ace's mark, the marks
  beside the court figures), which the prompts give as the only uses of
  those two colours.

Suit red on paper is 4.6 : 1, suit black 17 : 1. The web chat names them
once, with the ink, as `--card-ink`, `--card-red` and `--card-black` in
`shared/tokens.css`, which no theme redefines; the app has `cardSuitBlack`
in `lib/ui/message/chance_art.dart`. The chat's text fallback keeps the
theme's pink and ink glyphs.

## Shape and line

- A sharp rectangle with the top-right corner cut at 45°, a hard ink edge,
  no rounded corners. The packer cuts the corner (transparent) and draws
  the edge, so every card has the same ones; the model draws neither.
- One line weight in each picture, square line ends, mitred joins. Every
  coloured shape has an ink outline.
- Flat solid fills.
- Figures are blocky, angular and face the viewer, in the manner of the
  guide's icons. Circles only for things that are round (a coin, a sun, a
  moon, a mahjong dot).
- Court cards are double-ended. Number cards repeat the suit mark in the
  traditional layout: French in their usual columns, Latin-suited and
  Marseille swords, wands and batons crossed in a lattice.
- The card back is lavender with a grid of round ink dots.
- Mahjong tiles: the same paper, ink and neons; the packer adds a solid
  lavender block offset down and to the right for the tile's thickness,
  the guide's hard offset block.
- A six-sided die is a flat paper square with the cut corner and square
  navy ink pips, each a single filled block (the one is a larger pink
  pip). Other dice are flat polygons:
  d4, d8 and d20 triangles, d10 and d100 kites, a d12 pentagon, and a
  hexagon for any other number of sides.

Forbidden: gradients, shading, shadows, highlights, glow, gloss, texture,
grain, noise, blur, halftones, varying line weight, rounded corners,
perspective or 3D, realistic or photographic rendering, sketchy lines,
lettering (other than the Chinese characters of mahjong tiles),
signatures, watermarks, logos, a frame around the picture.

## The sheets

One sprite sheet per set: a uniform grid, filled left to right and top to
bottom in the order below. Cells are at twice the size they are shown
(a card shows at 88 × 123).

| Sheet | Serves | Cell (px) | Columns | Cells | Generated originals |
| --- | --- | --- | --- | --- | --- |
| `french` | standard, jokers, jokers3, piquet, durak, euchre, pinochle, canasta, shoe2 to shoe8 | 176 × 246 | 8 | 56: 52 cards, red, black and white jokers, the back | 56 |
| `tarot` | tarot | 176 × 246 | 10 | 78: 56 minor, 22 major | 78 |
| `latin` | spanish, italian | 176 × 246 | 8 | 52 | 40 |
| `german` | german, austrian | 176 × 246 | 8 | 32 | 32 |
| `swiss` | swiss | 176 × 246 | 9 | 36 | 36 |
| `hanafuda` | hanafuda | 148 × 246 | 8 | 48 | 48 |
| `mahjong` | mahjong | 144 × 192 | 9 | 42 | 42 |
| `dice-cube` | six-sided dice | 128 × 128 | 6 | 6 | 6 |
| `dice-poly` | every other die | 128 × 128 | 7 | 7 | 7 |
| | | | | 357 | 345 |

- Spanish and Italian decks share their figures but not their card codes:
  the Spanish courts are `sota`, `caballo`, `rey`, the Italian `fante`,
  `cavallo`, `re`. The sheet has a cell for each of the 52 codes, and the
  Italian courts are cut from the Spanish originals with their own index
  letter, so the model draws 40.
- German and Austrian decks hold the same 32 cards; they share the sheet
  while the registry says so (the inventory fails if they part).
- Hanafuda cards carry no index. Their cell is narrower than a playing
  card's, about 3 : 5 like printed hanafuda; the model is asked for 9 : 16
  and the packer trims the top and bottom.
- A polyhedral die's number is the only thing that changes between its
  faces, so `dice-poly` holds one blank shape per kind of die, and the
  clients set the number over it. Cards keep a whole face each: their
  pictures differ (pip layouts, figures), and so do the six cube faces.
- The faces of each sheet, in order, are in `testdata/art/sheets.json`,
  written from the deck registry by `tools/art-gen/inventory.py`.

### The index file

Beside each `<sheet>.webp` is `<sheet>.json`:

```json
{"sheet": "french", "image": "french.webp", "cell": [176, 246],
 "columns": 8, "rows": 7, "size": [1408, 1722],
 "faces": {"spades-ace": [0, 0], "spades-2": [1, 0], "...": []},
 "decks": ["standard", "..."]}
```

A face is a card code from the registry (`hearts-queen`, `major-16`,
`pine-bright`, `wind-east`), `back`, `d6-1` to `d6-6`, or a die shape (`d4`,
`d8`, `d10`, `d12`, `d20`, `d100`, `dn`); its value is `[column, row]`.
`dice-poly.json` adds:

- `text`: for each shape, the box its number goes in, in cell pixels
  (`{"x", "y", "w", "h"}`), found from the shape's outline in the packed
  cell;
- `text_colour`: suit black;
- `sides`: which shape a number of sides takes (`"20": "d20"`), and
  `other`: the shape of any other die (`dn`).

The clients set the number in Archivo 900 at 72% width, in suit black,
as large as fits the box.

### The index on cards

The packer stamps each card's index (the registry's short form: `A`, `2`
to `10`, `J`, `Q`, `K`, `P`, `Kn`, `S`, `C`, `R`, `F`, `U`, `O`, `B`,
`JK`, and a tarot major's number) in Archivo 900 at 72% width, in the
top-left corner and turned half round in the bottom-right. On French
cards a small suit mark, drawn from straight lines and 45° cuts, sits
under the rank. Hearts, diamonds and the red joker are suit red;
everything else is suit black. The model is told to leave the corners
empty, and the packer also shrinks each card's picture to 90% of the
cell (hanafuda 94%) on the original's own paper, so the picture keeps
clear of the index where the model reaches into a corner.

## Where the sheets live, and how they are shown

`tools/art-gen/pack.py` writes each sheet and its index to
`internal/web/static/shared/chance/` and `app/assets/chance/`, and writes
the web chat's list of the sheets it has, `shared/chance/sheets.js`
(empty in the tree).

- **Web chat** (`shared/message/chanceart.js`): the first time a result
  shows something from a listed set, it fetches that sheet's index and
  picture once for the page. Each slot face becomes a box the size of the
  cell's half, with the sheet as its background, moved to the face's
  cell; its text stays in the element for screen readers, unseen. A
  reversed tarot card is turned round, a dropped die is faded beside the
  word "dropped". If the sheet is not listed, fails to load, or lacks a
  face, the slot keeps its text face, with the pink glyphs of red suits.
- **App** (`lib/ui/message/chance_art.dart`): the sheets are bundled; a
  result reads its sheet's index the first time it needs it and draws the
  face's cell from the picture, with the same fallback.
- The reels spin through pictures: each frame shows its own card or cube
  face, and on a polyhedral die only the number changes.
- Inline dice in a sentence stay as text.

## Making the artwork

Install Pillow for the packing scripts (`inventory.py`, `prompts.py` and
`generate.py` need only Python 3):

    python3 -m pip install Pillow

The generation runs `nano-banana` on another machine over ssh (by default
`pole.lan`, where its Gemini key is set up; nothing here reads or sends a
key). Only the prompt and any reference picture leave this machine.

1. Check the inventory against the registry:

       python3 tools/art-gen/inventory.py --write

2. Read the prompts, and what a run would cost, without sending anything:

       python3 tools/art-gen/prompts.py reference-board
       python3 tools/art-gen/generate.py --set reference-board --dry-run

3. Generate the reference board (six faces: the ace of spades, the queen
   of hearts, The Tower, the pine with crane, the plum flower tile, a die
   showing five) and judge it:

       python3 tools/art-gen/generate.py --set reference-board
       python3 tools/art-gen/pack.py --cells /tmp/board

   The last packs just the board's six, with their edges, cut corners and
   indices, one PNG per cell (`--only sheet/face` for others).

4. Generate each set, with an approved face as the reference picture for
   consistency:

       python3 tools/art-gen/generate.py --set french --reference art-originals/french/spades-ace.png
       python3 tools/art-gen/generate.py --set tarot --only major-0 --only major-1

   Originals land in `art-originals/<sheet>/<face>.png` (ignored by git),
   with the prompt beside each as `<face>.txt`. A face already there is
   skipped, so a stopped run starts again where it was; delete a face's
   picture to make it again. Flags: `--model flash|pro|<model id>`,
   `--size 512|1K|2K|4K` (1K by default), `--aspect` (by default the
   sheet's: 2:3 for cards, 9:16 hanafuda, 3:4 tiles, 1:1 dice; the packer
   cuts to the cell), `--jobs N`, `--host`, `--binary`, `--remote-path`.
   On the remote machine everything goes in `~/churr-art/`.

5. Pack:

       python3 tools/art-gen/pack.py

   It fails, naming them, if any original is missing. The same originals
   give the same bytes. Sheets are lossless WebP; `--lossy Q` writes
   lossy WebP at quality Q instead, to compare sizes.

To try the whole path before there is artwork, draw stand-ins and pack
them somewhere outside the product trees:

    python3 tools/art-gen/placeholders.py --out /tmp/churr-art-ph
    python3 tools/art-gen/pack.py --originals /tmp/churr-art-ph --web /tmp/churr-art-web --no-app

### Cost

`nano-banana` states, per image on the flash model, about $0.045 at 512,
$0.067 at 1K, $0.101 at 2K and $0.151 at 4K; the pro model costs twice
that. At 1K on flash the reference board (6) is about $0.40 and the
whole set (345 originals, the board's six among them) about $23;
`--dry-run` prints the figure for any run.

## Tests

    python3 -m unittest discover -s tools/art-gen    the inventory, prompts, generation script, packer
    node e2e/chanceart.mjs                           the web chat's sheets, sprites and fallback
    cd app && flutter test test/ui/chance_art_test.dart

The inventory must match the registry and `testdata/art/sheets.json`;
every original has a prompt, and no prompt names a forbidden thing outside
its "do not" sentence; packing placeholders twice gives the same bytes and
an index that covers every face. The client tests use placeholder indexes
of their own.

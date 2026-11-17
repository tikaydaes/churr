# Chance artwork

The pictures of what Churr's chance commands show (rulings R81, R82):
playing cards of every deck, tarot, hanafuda, mahjong tiles and dice. The
pictures are generated artwork, made with Google's Nano Banana image models
through the `nano-banana` command, snapped to the card palette and traced
to vectors. Each card is composited from components: a card body drawn by
rule, the traced picture, and an index set in outlined Archivo glyphs.
Dice are drawn by rule.

The tools are in `tools/art-gen/`. A set with no sprite in the tree is
drawn as text by both clients, as chance results always were.

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

The tracer sets every fill to one of these eight hexes exactly; nothing
else appears in a sprite. Suit red on paper is 4.6 : 1, suit black 17 : 1.
The web chat names them
once, with the ink, as `--card-ink`, `--card-red` and `--card-black` in
`shared/tokens.css`, which no theme redefines; the app has `cardSuitBlack`
in `lib/ui/message/chance_art.dart`. The chat's text fallback keeps the
theme's pink and ink glyphs.

## Shape and line

- A sharp rectangle with the top-right corner cut at 45°, a hard ink edge,
  no rounded corners: the card body, drawn by the packer, the same on
  every card; the model draws neither.
- One line weight in each picture, square line ends, mitred joins. Every
  coloured shape has an ink outline.
- Flat solid fills.
- Figures are blocky, angular and face the viewer, in the manner of the
  guide's icons. Circles only for things that are round (a coin, a sun, a
  moon, a mahjong dot).
- French court cards are double-ended, one continuous design: the model
  draws the whole card (the figure in the top half, the same figure upside
  down in the bottom half, joined on a diagonal through the centre with
  the clothes running across it); the packer keeps the top half, cut at
  the centre of what was drawn, and adds it turned half round, so every
  court is exactly symmetrical and joined where the model joined it.
  Courts of the other decks are single figures, as in their real decks.
- Number cards repeat the suit mark in the traditional layout, placed by
  the packer from one generated mark per suit: French pips in their usual
  columns with those below the middle turned round; swords, wands and
  batons upright side by side in rows. Counts are exact.
- The card back is lavender with a straight grid of round ink dots, drawn
  by rule (the model could not keep a grid regular).
- Mahjong tiles: the same paper, ink and neons; the packer adds a solid
  lavender block offset down and to the right for the tile's thickness,
  the guide's hard offset block.
- Dice are drawn by rule, so they are exact. A six-sided die is a flat
  paper square with the cut corner, the ink edge and square navy pips on a
  three by three grid (the one is a larger pink pip with an ink edge).
  Every other die, and a coin, is drawn by the clients themselves
  ("Dice and coins drawn on the fly", below).

Forbidden: gradients, shading, shadows, highlights, glow, gloss, texture,
grain, noise, blur, halftones, varying line weight, rounded corners,
perspective or 3D, realistic or photographic rendering, sketchy lines,
lettering (other than the Chinese characters of mahjong tiles),
signatures, watermarks, logos, a frame around the picture.

## The sprites

One SVG sprite per set: a `<symbol id="<face>" viewBox="0 0 w h">` per
face, built from shared components in `<defs>` referenced with `<use>`:
the card body and its clip, each traced picture once (`c-<original>`),
each index's glyphs once (`i-<text>`), the small suit marks (`m-<suit>`).
A number card is a few `<use>` of its suit's mark, so it costs almost
nothing. Faces are in sprite units, a card 176 × 246, shown at half that
(88 × 123).

| Sprite | Serves | Face | Faces | Generated originals |
| --- | --- | --- | --- | --- |
| `french` | standard, jokers, jokers3, piquet, durak, euchre, pinochle, canasta, shoe2 to shoe8 | 176 × 246 | 56: 52 cards, red, black and white jokers, the back | 23: 4 aces, 4 marks, 12 courts, 3 jokers |
| `tarot` | tarot | 176 × 246 | 78: 56 minor, 22 major | 46: 22 majors, 4 aces, 16 courts, 4 marks |
| `latin` | spanish, italian | 176 × 246 | 52 | 20: 4 aces, 12 courts, 4 marks |
| `german` | german, austrian | 176 × 246 | 32 | 20 |
| `swiss` | swiss | 176 × 246 | 36 | 24 |
| `hanafuda` | hanafuda | 148 × 246 | 48 | 48 |
| `mahjong` | mahjong | 144 × 192 | 42 | 42 |
| `dice-cube` | six-sided dice | 128 × 128 | 6 | none: drawn by rule |
| | | | 350 | 223 |

- Spanish and Italian decks share their figures but not their card codes:
  the Spanish courts are `sota`, `caballo`, `rey`, the Italian `fante`,
  `cavallo`, `re`. Each code has its own face, and the Italian courts use
  the Spanish pictures with their own index letter.
- German and Austrian decks hold the same 32 cards; they share the sprite
  while the registry says so (the inventory fails if they part).
- Hanafuda cards carry no index. Their face is about 3 : 5 like printed
  hanafuda; the model is asked for 9 : 16.
- The faces of each sprite, in order, are in `testdata/art/sheets.json`,
  written from the deck registry by `tools/art-gen/inventory.py`.

### The index file

Beside each `<sheet>.svg` is `<sheet>.json`:

```json
{"sheet": "french", "file": "french.svg", "cell": [176, 246],
 "faces": ["spades-ace", "spades-2", "..."], "decks": ["standard", "..."]}
```

A face is a card code from the registry (`hearts-queen`, `major-16`,
`pine-bright`, `wind-east`), `back`, or `d6-1` to `d6-6`.

### Dice and coins drawn on the fly

Every die but the six-sided one, and every coin, has no sprite: both
clients draw it as it is shown (web: `shared/message/diceshape.js`; app:
`lib/ui/message/dice_shape.dart`), in a 64 × 64 box, the size of a cube
face.

- A die of N sides, for N from 3 to `NGON_MAX_SIDES` (30; `ngonMaxSides`
  in the app, held equal by a test), is a regular N-sided polygon with
  one corner straight up, its corners on a circle of radius 29, set in
  the middle of the box by its own height. A die of more sides (d100,
  d1000) is a circle of the same radius.
- Every die is one colour, as the six-sided cube is: a paper fill and a
  2 px ink edge with mitred corners. These are card colours, the same on
  every theme.
- The number it shows is in Archivo 900 at 72% width, in suit black, as
  large as fits the largest box of its shape inside the circle that fits
  inside the polygon, 2 px clear of the edge; four figures fit as well as
  one.
- A two-sided die is a coin, and so is the result of `/flip`: a paper
  circle with the ink edge and a thin inner ring. Heads (a d2's 1) is a
  large H under a small cut-corner crown; tails (a d2's 2) a large T
  between two small diamonds. The letter is always there; the marks only
  help it. A d2 adds up as before (heads 1, tails 2) and reads as
  "heads (1)".

### The index on cards

Each card's index is the registry's short form (`A`, `2` to `10`, `J`,
`Q`, `K`, `P`, `Kn`, `S`, `C`, `R`, `F`, `U`, `O`, `B`, `JK`) or a tarot
major's Roman numeral (`0` for The Fool), as outlined glyph paths of the
bundled Archivo at 900 and 72% width (no font in the sprite), in the
top-left corner and turned half round in the bottom-right. A long numeral
is set smaller to keep to its corner. On French cards a small suit mark,
drawn from straight lines and 45° cuts, sits under the rank. Hearts,
diamonds and the red joker are suit red; everything else is suit black.
The model is told to leave the corners empty, and each picture is shrunk
to 90% of the card (hanafuda 94%, tiles 92%), so it keeps clear of the
index where the model reached into a corner.

### Tracing

`tools/art-gen/trace.py` turns an original into a component: upscale 2x
(Lanczos), 3 px median, nearest palette colour (navy and suit black
merged), thick off-palette areas repainted as one block in the nearest
fill colour (pale skin tints to paper), a 5 px mode filter, each dark
shape made navy or suit black as a whole by its mean colour; then
vtracer (colour, stacked, polygons; speckle 8, corner 60, length 4, splice
45) and every fill set to its exact hex. Components are kept in
`art-originals/.traced/`. The packer prints a review list: faces where the
snap repainted a large area, or left small islands on the paper.

## Where the sprites live, and how they are shown

`tools/art-gen/pack.py` writes each sprite and its index to
`internal/web/static/shared/chance/`, with the web chat's list of the
sprites it has, `shared/chance/sheets.js`, and each set's atlas to
`app/assets/chance/`.

- **Web chat** (`shared/message/chanceart.js`): the first time a result
  shows something from a listed set, it fetches that set's index and
  sprite once. Dice and coins drawn on the fly are an inline `<svg>` of
  their own and fetch nothing. Each slot face becomes a box half the face's size holding
  `<svg><use href="/shared/chance/<set>.svg#<face>"></svg>`; its text
  stays in the element for screen readers, unseen. A dropped die is
  faded beside the word "dropped". If
  the set is not listed, its index or sprite fails to load, or it lacks a
  face, the slot keeps its text face, with the pink glyphs of red suits.
- **App** (`lib/ui/message/chance_art.dart`): each set is bundled as a
  WebP atlas rendered from the same sprite with resvg at 1.5 times the
  face's size (three times what is shown), with an index of logical cell
  sizes and positions (`cell`, `columns`, `rows`, `size`, `faces`:
  `{"spades-ace": [0, 0]}`). It needs no SVG package, reads one picture
  per set, and draws exactly the pixels of the vectors. It is sharp up to
  three times the chat size, which is all the app shows.
- The reels spin through pictures: each frame shows its own card or cube
  face; on a die drawn on the fly only the number changes, and a coin
  turns over, heads and tails by turns.
- A reversed card (tarot, or any deck drawn with orientation) is drawn
  upside down in both clients, in the result as it stands and in the
  reel's last frame; while the reel turns its frames are upright. The
  "reversed" label and the words a screen reader reads stay as they are.
- Inline dice in a sentence stay as text.

## Making the artwork

`inventory.py`, `prompts.py` and `generate.py` need only Python 3.
Tracing and packing need:

    python3 -m pip install Pillow numpy scipy vtracer fonttools resvg-py brotli

(brotli only for the size report).

The generation runs `nano-banana` on another machine over ssh (by default
`pole.lan`, where its Gemini key is set up; nothing here reads or sends a
key). Only the prompt and any reference picture leave this machine.

1. Check the inventory against the registry:

       python3 tools/art-gen/inventory.py --write

2. Read the prompts, and what a run would cost, without sending anything:

       python3 tools/art-gen/prompts.py reference-board
       python3 tools/art-gen/generate.py --set reference-board --dry-run

3. Generate the reference board (the ace of spades, the queen of hearts,
   The Tower, the pine with crane, the plum flower tile, a die showing
   five) and judge it:

       python3 tools/art-gen/generate.py --set reference-board

4. Generate each set, with an approved face as the reference picture for
   consistency:

       python3 tools/art-gen/generate.py --set french --reference art-originals/french/spades-ace.png
       python3 tools/art-gen/generate.py --set tarot --only major-0 --only major-1

   Originals land in `art-originals/<sheet>/<face>.png` (ignored by git),
   with the prompt beside each as `<face>.txt`. A face already there is
   skipped, so a stopped run starts again where it was; delete a face's
   picture to make it again. Flags: `--model flash|pro|<model id>`,
   `--size 512|1K|2K|4K` (1K by default), `--aspect` (by default the
   face's: 2:3 for cards, 1:1 for a suit mark, 9:16 hanafuda, 3:4
   tiles), `--jobs N`, `--host`, `--binary`,
   `--remote-path`.
   On the remote machine everything goes in `~/churr-art/`.

5. Trace and pack, and look at what the review list names:

       python3 tools/art-gen/pack.py --review /tmp/review.json

   It fails, naming them, if any original is missing, and prints each
   sprite's size raw, gzipped and with brotli. The same originals give the
   same bytes. App atlases are lossless WebP (`--lossy Q` to compare).

To try the whole path before there is artwork, draw stand-ins and pack
them somewhere outside the product trees:

    python3 tools/art-gen/placeholders.py --out /tmp/churr-art-ph --scale 0.25
    python3 tools/art-gen/pack.py --originals /tmp/churr-art-ph --web /tmp/churr-art-web --no-app

### Cost

`nano-banana` states, per image on the flash model, about $0.045 at 512,
$0.067 at 1K, $0.101 at 2K and $0.151 at 4K; the pro model costs twice
that. The whole set is 223 originals, about $30 at 1K on pro before
rerolls; `--dry-run` prints the figure for any run.

## Tests

    python3 -m unittest discover -s tools/art-gen    the inventory, prompts, generation script, tracer, packer
    node e2e/chanceart.mjs                           the web chat's sprites and fallback
    cd app && flutter test test/ui/chance_art_test.dart

The inventory must match the registry and `testdata/art/sheets.json`;
every original has a prompt, and no prompt names a forbidden thing outside
its "do not" sentence; tracing is deterministic and stays in the palette;
a sprite built from placeholders has a symbol for every face, every
`<use>` finds its target, no colour outside the palette appears, and
building it twice gives the same bytes. The client tests use placeholder
indexes of their own; e2e checks a `/draw` drawn from the real sprite and
the text when the sprite cannot load.

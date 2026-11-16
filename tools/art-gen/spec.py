"""The fixed facts of Churr's chance artwork (docs/ART.md): the palette, the
sheets and their cells. Every other script reads them from here.
"""
import os

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, '..', '..'))

# The card's own colours, the same on every theme (docs/style-guide.html):
# the paper is the built-in light theme's ground, the ink the built-in dark
# theme's ground, the four neons the built-in (dark) values.
PAPER = '#f5f2ff'
INK = '#0d1026'
LAVENDER = '#b9a0ff'
CYAN = '#5df0e4'
PINK = '#ff5ccf'
YELLOW = '#ffd95e'
PALETTE = {'paper': PAPER, 'ink': INK, 'lavender': LAVENDER, 'cyan': CYAN, 'pink': PINK, 'yellow': YELLOW}
# The suit colours (ruling R81, after the first reference board): every
# stamped index and die number, and the suit marks of French cards, are
# true red or black, never the brand pink. Red only where a deck has the
# red and black convention (French hearts, diamonds, the red joker).
SUIT_RED = '#d81e2c'
SUIT_BLACK = '#111111'

# The font the indices and die numbers are set in: Archivo at weight 900,
# width 72% (the guide's display setting), shipped with the app.
FONT = os.path.join(ROOT, 'app', 'assets', 'fonts', 'Archivo.ttf')
FONT_WEIGHT = 900
FONT_WIDTH = 72

# Where pack.py writes the sheets.
WEB_DIR = os.path.join(ROOT, 'internal', 'web', 'static', 'shared', 'chance')
APP_DIR = os.path.join(ROOT, 'app', 'assets', 'chance')
# Where generate.py keeps the originals: ignored by git.
ORIGINALS = os.path.join(ROOT, 'art-originals')


class Sheet:
    """One sprite sheet: its cell in pixels, columns, the aspect ratio the
    model is asked for (nano-banana's nearest ratio at least as tall as the
    cell), the cut corner and ink edge in pixels at cell size, and what the
    packer draws on each cell."""

    def __init__(self, name, cell, columns, aspect, cut, edge, kind, decks=()):
        self.name, self.cell, self.columns, self.aspect = name, cell, columns, aspect
        self.cut, self.edge, self.kind, self.decks = cut, edge, kind, list(decks)


FRENCH_DECKS = ['standard', 'jokers', 'jokers3', 'piquet', 'durak', 'euchre', 'pinochle', 'canasta',
                'shoe2', 'shoe4', 'shoe6', 'shoe8']
SHEETS = [
    Sheet('french', (176, 246), 8, '2:3', 16, 4, 'card', FRENCH_DECKS),
    Sheet('tarot', (176, 246), 10, '2:3', 16, 4, 'card', ['tarot']),
    Sheet('latin', (176, 246), 8, '2:3', 16, 4, 'card', ['spanish', 'italian']),
    Sheet('german', (176, 246), 8, '2:3', 16, 4, 'card', ['german', 'austrian']),
    Sheet('swiss', (176, 246), 9, '2:3', 16, 4, 'card', ['swiss']),
    Sheet('hanafuda', (148, 246), 8, '9:16', 14, 4, 'hanafuda', ['hanafuda']),
    Sheet('mahjong', (144, 192), 9, '3:4', 14, 4, 'tile', ['mahjong']),
    Sheet('dice-cube', (128, 128), 6, '1:1', 14, 4, 'die', []),
]
BY_NAME = {s.name: s for s in SHEETS}

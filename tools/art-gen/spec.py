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
    Sheet('hanafuda', (196, 246), 8, '4:5', 16, 4, 'hanafuda', ['hanafuda']),
    Sheet('mahjong', (144, 192), 9, '3:4', 14, 4, 'tile', ['mahjong']),
    Sheet('dice-cube', (128, 128), 6, '1:1', 14, 4, 'die', []),
    Sheet('dice-poly', (128, 128), 7, '1:1', 0, 0, 'poly', []),
]
BY_NAME = {s.name: s for s in SHEETS}

# The polyhedral dice: one blank shape each. Any other number of sides
# (2 to 1000, other than 6) takes the generic shape. The text box is where
# the clients set the number, as fractions of the shape's own bounding box
# found in the packed cell: centre x, centre y, width, height.
POLY = {
    'd4': {'sides': [4], 'box': (0.5, 0.64, 0.42, 0.30)},
    'd8': {'sides': [8], 'box': (0.5, 0.64, 0.42, 0.30)},
    'd10': {'sides': [10], 'box': (0.5, 0.50, 0.46, 0.30)},
    'd12': {'sides': [12], 'box': (0.5, 0.54, 0.50, 0.34)},
    'd20': {'sides': [20], 'box': (0.5, 0.64, 0.42, 0.30)},
    'd100': {'sides': [100], 'box': (0.5, 0.50, 0.50, 0.26)},
    'dn': {'sides': [], 'box': (0.5, 0.50, 0.56, 0.36)},
}

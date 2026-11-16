#!/usr/bin/env python3
"""Every face of every sheet, in a stable order, built from the deck
registry (tools/decks-gen/gen.py, through what it writes:
testdata/commands/decks.json). Fails loudly when the registry and the
sheets drift apart.

    python3 tools/art-gen/inventory.py           print the faces per sheet
    python3 tools/art-gen/inventory.py --write   also write testdata/art/sheets.json

A face is what a cell of a sheet shows. Its `original` is the generated
picture it is cut from: usually itself, but the Italian courts are cut from
the Spanish ones (the same figures; only the index letter differs). Dice
other than the six-sided one have no sheet: the clients draw them.
"""
import json, os, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import spec  # noqa: E402

DECKS_JSON = os.path.join(spec.ROOT, 'testdata', 'commands', 'decks.json')
OUT_JSON = os.path.join(spec.ROOT, 'testdata', 'art', 'sheets.json')

# The cards whose index is printed in suit red; every other index is suit
# black. Only the French deck has the red and black convention.
RED_SUITS = {'hearts', 'diamonds'}
RED_EXTRAS = {'joker-red'}
# The suits whose index carries a small suit mark under the rank.
MARKED_SUITS = {'spades', 'hearts', 'diamonds', 'clubs'}
# The Italian courts and the Spanish figure each is cut from.
ITALIAN_COURTS = {'fante': 'sota', 'cavallo': 'caballo', 're': 'rey'}


class Drift(Exception):
    pass


def load_registry(path=DECKS_JSON):
    with open(path) as f:
        return json.load(f)


def _unique(seq):
    return list(dict.fromkeys(seq))


def _face(fid, original=None, index=None, red=False, mark=None, build=None, pips=0):
    """build says how the packer makes the cell from its original: None
    (the original is the face), 'double' (the original is the top half of a
    double-ended court card, turned and joined below itself), 'pips' (the
    original is one suit mark, laid out `pips` times), 'rule' (drawn by the
    packer alone: the card back and the dice)."""
    return {'id': fid, 'original': original or fid, 'index': index, 'red': red, 'mark': mark, 'build': build,
            'pips': pips}


ROMAN = ['0', 'I', 'II', 'III', 'IV', 'V', 'VI', 'VII', 'VIII', 'IX', 'X', 'XI', 'XII', 'XIII', 'XIV', 'XV', 'XVI',
         'XVII', 'XVIII', 'XIX', 'XX', 'XXI']
# The French courts are double-ended; the courts of the other decks are not.
DOUBLE_ENDED = {'french': {'jack', 'queen', 'king'}}


def build(reg):
    """Returns {sheet name: [face, ...]} or raises Drift."""
    decks = {d['name']: d for d in reg['decks']}
    ranks, extras = reg['ranks'], reg['extras']
    claimed = {d for s in spec.SHEETS for d in s.decks}
    unknown = sorted(set(decks) - claimed)
    if unknown:
        raise Drift('decks with no sheet: %s (add them to a sheet in spec.py)' % ', '.join(unknown))
    missing = sorted(claimed - set(decks))
    if missing:
        raise Drift('sheets name decks the registry does not have: %s' % ', '.join(missing))

    def index_of(code):
        if code.startswith('major-'):
            return ROMAN[int(code.split('-')[1])]
        if code in extras:
            return extras[code]['short']
        suit, rank = code.split('-', 1)
        if rank not in ranks:
            raise Drift('card %s has a rank the registry does not name' % code)
        return ranks[rank]['short']

    def red(code):
        return code in RED_EXTRAS or code.split('-', 1)[0] in RED_SUITS

    def mark(code):
        suit = code.split('-', 1)[0]
        return suit if suit in MARKED_SUITS and code not in extras else None

    out = {}
    for s in spec.SHEETS:
        if s.kind in ('card',):
            codes = _unique(c for d in s.decks for c in decks[d]['cards'])
            faces = []
            for c in codes:
                orig = c
                if s.name == 'latin':
                    suit, rank = c.split('-', 1)
                    if rank in ITALIAN_COURTS:
                        orig = '%s-%s' % (suit, ITALIAN_COURTS[rank])
                        if orig not in codes:
                            raise Drift('%s is cut from %s, which the Spanish deck lacks' % (c, orig))
                french = s.name == 'french'
                rank = c.split('-', 1)[1] if c not in extras else ''
                build, pips = None, 0
                if rank.isdigit() and int(rank) >= 2:
                    build, pips = 'pips', int(rank)
                    orig = 'mark-' + c.split('-', 1)[0]
                elif rank in DOUBLE_ENDED.get(s.name, ()):
                    build = 'double'
                faces.append(_face(c, orig, index_of(c), french and red(c), mark(c) if french else None, build, pips))
            if s.name == 'french':
                # The back is a dot grid, drawn by rule.
                faces.append(_face('back', build='rule'))
            out[s.name] = faces
        elif s.kind in ('hanafuda', 'tile'):
            out[s.name] = [_face(c) for c in _unique(c for d in s.decks for c in decks[d]['cards'])]
        elif s.kind == 'die':
            out[s.name] = [_face('d6-%d' % n, build='rule') for n in range(1, 7)]
    check(reg, out)
    return out


# The counts the owner's ruling (R81) and the registry agree on, as cells
# and as generated originals.
EXPECTED = {
    'french': (56, 23), 'tarot': (78, 46), 'latin': (52, 20), 'german': (32, 20), 'swiss': (36, 24),
    'hanafuda': (48, 48), 'mahjong': (42, 42), 'dice-cube': (6, 0),
}


def check(reg, out):
    decks = {d['name']: d for d in reg['decks']}
    for s in spec.SHEETS:
        ids = [f['id'] for f in out[s.name]]
        if len(ids) != len(set(ids)):
            raise Drift('%s: a face is listed twice' % s.name)
        for d in s.decks:
            lost = sorted(set(decks[d]['cards']) - set(ids))
            if lost:
                raise Drift('%s: deck %s has cards the sheet lacks: %s' % (s.name, d, ', '.join(lost)))
        want = EXPECTED.get(s.name)
        got = (len(ids), len(originals(out[s.name])))
        if want != got:
            raise Drift('%s: %d cells from %d originals, expected %d from %d' % ((s.name,) + got + want))
    # German and Austrian share the sheet only while their cards are the same.
    if decks['german']['cards'] != decks['austrian']['cards']:
        raise Drift('german and austrian no longer hold the same cards; give austrian its own sheet')


def originals(faces):
    """The generated pictures a sheet needs (the back and dice are drawn
    by rule)."""
    return _unique(f['original'] for f in faces if f['build'] != 'rule')


def table(reg=None):
    """What testdata/art/sheets.json holds: for each sheet its cell, its
    columns, the decks it serves and its faces in order."""
    out = build(reg or load_registry())
    return {
        'description': 'The sprite sheets of the chance artwork (docs/ART.md), written by '
                       'tools/art-gen/inventory.py from the deck registry. Each sheet: its cell in '
                       'pixels, its columns, the decks it serves, and its faces in order (cell n is at '
                       'column n % columns, row n / columns). dice-cube serves six-sided dice; '
                       'the clients draw every other die, and coins, themselves.',
        'sheets': {s.name: {'cell': list(s.cell), 'columns': s.columns, 'decks': s.decks,
                            'faces': [f['id'] for f in out[s.name]]} for s in spec.SHEETS},
    }


def main():
    try:
        reg = load_registry()
        out = build(reg)
    except Drift as e:
        sys.exit('inventory: ' + str(e))
    total_cells = total_orig = 0
    for s in spec.SHEETS:
        faces = out[s.name]
        n_orig = len(originals(faces))
        total_cells += len(faces)
        total_orig += n_orig
        print('%-10s %3d cells, %3d originals: %s' % (s.name, len(faces), n_orig, ' '.join(f['id'] for f in faces)))
    print('total      %3d cells, %3d originals' % (total_cells, total_orig))
    if '--write' in sys.argv[1:]:
        os.makedirs(os.path.dirname(OUT_JSON), exist_ok=True)
        with open(OUT_JSON, 'w') as f:
            f.write(json.dumps(table(reg), indent=1) + '\n')
        print('wrote', os.path.relpath(OUT_JSON, spec.ROOT))


if __name__ == '__main__':
    main()

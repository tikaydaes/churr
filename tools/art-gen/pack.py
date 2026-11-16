#!/usr/bin/env python3
"""Builds one SVG sprite per set from the traced originals, with its index,
and the app's atlas rendered from the same sprite (docs/ART.md, R82).

    python3 tools/art-gen/pack.py                      every sheet
    python3 tools/art-gen/pack.py --set french --set dice-cube
    python3 tools/art-gen/pack.py --originals /tmp/ph --web /tmp/w --app /tmp/a

Every card is composited from components:
- the card body (paper, ink edge, cut corner), drawn by rule;
- the picture: the traced original (trace.py), laid over the body and
  clipped to it; a French court is its traced top half and a copy turned
  half round; a number card is the traced suit mark placed with <use> in
  the traditional layout; the card back is a dot grid drawn by rule;
- the index: the rank as outlined glyphs of the bundled Archivo (900, width
  72%) and, on French cards, a small suit mark, in suit red or black;
- a mahjong tile's thickness block, drawn by rule.
Dice are drawn by rule: a cube face with square pips, or a polygon whose
number the clients set as text.

Writes <sheet>.svg (a <symbol> per face, shared components in <defs>) and
<sheet>.json into the web chat's static tree, the web chat's list of
sheets (shared/chance/sheets.js), and into the app's assets <sheet>.webp,
an atlas of every face rendered with resvg at 1.5 times the cell (three
times the size the app shows it), with <sheet>.json. The same originals
always give the same bytes.
"""
import argparse, gzip, io, json, math, os, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import spec, inventory  # noqa: E402

IMAGE_EXT = ('.png', '.jpg', '.jpeg', '.webp')
TILE_BLOCK = 8  # the mahjong tile's offset block, in cell units
# Pictures are shrunk to this share of the card, so they keep clear of the
# index even where the model reached into a corner.
INSET = {'card': 0.9, 'hanafuda': 0.94, 'tile': 0.92}
ATLAS_SCALE = 1.5
CACHE = os.path.join(spec.ORIGINALS, '.traced')
PAPER, INK = spec.PAPER.upper(), spec.INK.upper()
RED, BLACK = spec.SUIT_RED.upper(), spec.SUIT_BLACK.upper()

# Where the pips of a number card go, as centres in the card (x, y from 0
# to 1): the French layouts, which the compact marks of every deck use. On
# French cards the pips below the middle are turned half round.
PIP_LAYOUT = {
    2: [(0.5, 0.2), (0.5, 0.8)],
    3: [(0.5, 0.2), (0.5, 0.5), (0.5, 0.8)],
    4: [(0.33, 0.2), (0.67, 0.2), (0.33, 0.8), (0.67, 0.8)],
    5: [(0.33, 0.2), (0.67, 0.2), (0.5, 0.5), (0.33, 0.8), (0.67, 0.8)],
    6: [(0.33, 0.2), (0.67, 0.2), (0.33, 0.5), (0.67, 0.5), (0.33, 0.8), (0.67, 0.8)],
    7: [(0.33, 0.2), (0.67, 0.2), (0.5, 0.35), (0.33, 0.5), (0.67, 0.5), (0.33, 0.8), (0.67, 0.8)],
    8: [(0.33, 0.2), (0.67, 0.2), (0.5, 0.35), (0.33, 0.5), (0.67, 0.5), (0.5, 0.65), (0.33, 0.8), (0.67, 0.8)],
    9: [(0.33, 0.2), (0.67, 0.2), (0.33, 0.4), (0.67, 0.4), (0.5, 0.5), (0.33, 0.6), (0.67, 0.6), (0.33, 0.8),
        (0.67, 0.8)],
    10: [(0.33, 0.2), (0.67, 0.2), (0.5, 0.3), (0.33, 0.4), (0.67, 0.4), (0.33, 0.6), (0.67, 0.6), (0.5, 0.7),
         (0.33, 0.8), (0.67, 0.8)],
}
# Long marks (swords, wands, batons) stand upright side by side, in rows.
LONG_MARKS = {'swords', 'wands', 'batons'}
LONG_ROWS = {2: [2], 3: [3], 4: [2, 2], 5: [3, 2], 6: [3, 3], 7: [4, 3], 8: [4, 4], 9: [3, 3, 3], 10: [5, 5]}

# The small suit marks of the index, from straight lines and 45° cuts, in a
# unit box (x right, y down).
_HEART = [(0.5, 0.28), (0.72, 0.06), (0.9, 0.06), (1, 0.18), (1, 0.46), (0.5, 0.98), (0, 0.46), (0, 0.18),
          (0.1, 0.06), (0.28, 0.06)]
_OCT = [(-1, -0.42), (-0.42, -1), (0.42, -1), (1, -0.42), (1, 0.42), (0.42, 1), (-0.42, 1), (-1, 0.42)]
MARKS = {
    'hearts': [_HEART],
    'diamonds': [[(0.5, 0), (0.88, 0.5), (0.5, 1), (0.12, 0.5)]],
    'spades': [[(x, 0.82 - y * 0.82) for x, y in _HEART], [(0.44, 0.6), (0.56, 0.6), (0.68, 1), (0.32, 1)]],
    'clubs': [[(cx + 0.21 * x, cy + 0.21 * y) for x, y in _OCT] for cx, cy in [(0.5, 0.22), (0.22, 0.56), (0.78, 0.56)]]
             + [[(0.44, 0.4), (0.56, 0.4), (0.68, 1), (0.32, 1)]],
}

# The polyhedral dice, drawn by rule: shape and fill.
POLY_DRAW = {'d4': ('triangle', spec.CYAN), 'd8': ('triangle', spec.YELLOW), 'd20': ('triangle', spec.LAVENDER),
             'd10': ('kite', spec.PINK), 'd100': ('kite', spec.YELLOW), 'd12': ('pentagon', spec.LAVENDER),
             'dn': ('hexagon', spec.PAPER)}


def n(v):
    """A number for SVG: at most two decimals, no trailing zeros."""
    s = ('%.2f' % v).rstrip('0').rstrip('.')
    return '0' if s in ('-0', '') else s


def poly_d(pts):
    return 'M' + 'L'.join('%s %s' % (n(x), n(y)) for x, y in pts) + 'Z'


def outline(w, h, cut, inset=0.0):
    """The cut-corner rectangle, inset by `inset` on every side."""
    e = inset
    if cut <= 0:
        return [(e, e), (w - e, e), (w - e, h - e), (e, h - e)]
    d = e * math.sqrt(2) - e
    return [(e, e), (w - cut - d, e), (w - e, cut + d), (w - e, h - e), (e, h - e)]


# --- the index font ------------------------------------------------------------

_FONT = {}


def font():
    if 'f' not in _FONT:
        from fontTools.ttLib import TTFont
        from fontTools.varLib import instancer
        f = instancer.instantiateVariableFont(TTFont(spec.FONT), {'wght': spec.FONT_WEIGHT, 'wdth': spec.FONT_WIDTH})
        _FONT['f'] = f
    return _FONT['f']


def glyph_run(text):
    """The text as one path in font units (y down, baseline at 0), and its
    advance width and cap height."""
    from fontTools.pens.svgPathPen import SVGPathPen
    from fontTools.pens.transformPen import TransformPen
    f = font()
    gs, cmap, hmtx = f.getGlyphSet(), f.getBestCmap(), f['hmtx']
    pen = SVGPathPen(gs, ntos=lambda v: n(v))
    x = 0
    for ch in text:
        g = cmap[ord(ch)]
        gs[g].draw(TransformPen(pen, (1, 0, 0, -1, x, 0)))
        x += hmtx[g][0]
    return pen.getCommands(), x, f['OS/2'].sCapHeight


# --- the originals -------------------------------------------------------------

def find_original(folder, sheet, original):
    for ext in IMAGE_EXT:
        p = os.path.join(folder, sheet, original + ext)
        if os.path.exists(p):
            return p
    return None


class Sprite:
    """One sheet's SVG: shared definitions, then a symbol per face."""

    def __init__(self, s, originals, cache):
        self.s, self.originals, self.cache = s, originals, cache
        self.defs, self.symbols, self.have, self.comps, self.notes = [], [], set(), {}, {}

    def define(self, key, text):
        if key not in self.have:
            self.have.add(key)
            self.defs.append(text)

    def component(self, original):
        """The traced original, defined once as <g id="c-...">."""
        if original not in self.comps:
            import trace
            path = find_original(self.originals, self.s.name, original)
            if not path:
                raise SystemExit('%s: no original for %s (in %s)' % (self.s.name, original,
                                                                     os.path.join(self.originals, self.s.name)))
            comp = trace.cached(path, os.path.join(self.cache, self.s.name))
            self.comps[original] = comp
            if comp['notes']:
                self.notes[original] = comp['notes']
            body = ''.join('<path fill="%s" d="%s"/>' % (c, d) for c, d in comp['paths'])
            self.define('c-' + original, '<g id="c-%s">%s</g>' % (original, body))
        return self.comps[original]

    def body(self):
        w, h = self.s.cell
        cut, e = self.s.cut, self.s.edge
        block = TILE_BLOCK if self.s.kind == 'tile' else 0
        fw, fh = w - block, h - block
        if 'body' not in self.have:
            parts = []
            if block:
                parts.append('<path fill="%s" d="%s"/>' % (spec.LAVENDER.upper(),
                                                            poly_d([(x + block, y + block) for x, y in outline(fw, fh, cut)])))
            parts.append('<path fill="%s" d="%s"/>' % (INK, poly_d(outline(fw, fh, cut))))
            parts.append('<path fill="%s" d="%s"/>' % (PAPER, poly_d(outline(fw, fh, cut, e))))
            self.define('body', '<g id="body">%s</g>' % ''.join(parts))
            self.define('clip', '<clipPath id="clip"><path d="%s"/></clipPath>' % poly_d(outline(fw, fh, cut, e)))
        return fw, fh

    def index_glyphs(self, text):
        key = 'i-' + text
        if key not in self.have:
            d, adv, cap = glyph_run(text)
            self.define(key, '<path id="%s" d="%s"/>' % (key, d))
            self.idx_metrics = getattr(self, 'idx_metrics', {})
            self.idx_metrics[text] = (adv, cap)
        return self.idx_metrics[text]

    def suit_mark(self, suit):
        key = 'm-' + suit
        self.define(key, '<path id="%s" d="%s"/>' % (key, ''.join(
            poly_d([(x * 100, y * 100) for x, y in p]) for p in MARKS[suit])))

    def index(self, face, w, h):
        """The index in the top-left corner and turned half round."""
        text = face['index']
        adv, cap = self.index_glyphs(text)
        e = self.s.edge
        size = w * 0.14  # em size in cell units
        if adv * size / 1000 > w * 0.17:
            size = max(w * 0.07, w * 0.17 * 1000 / adv)
        k = size / 1000
        cx = e + w * 0.11
        top = e + w * 0.065
        colour = RED if face['red'] else BLACK
        parts = ['<use href="#i-%s" transform="translate(%s %s) scale(%s)"/>' % (
            text, n(cx - adv * k / 2), n(top + cap * k), n(k))]
        if face.get('mark'):
            self.suit_mark(face['mark'])
            m = w * 0.085
            parts.append('<use href="#m-%s" transform="translate(%s %s) scale(%s)"/>' % (
                face['mark'], n(cx - m / 2), n(top + cap * k + w * 0.025), n(m / 100)))
        one = '<g fill="%s">%s</g>' % (colour, ''.join(parts))
        return one + '<g transform="rotate(180 %s %s)">%s</g>' % (n(w / 2), n(h / 2), one)

    def placed(self, comp, original, x, y, bw, bh, turn=False, anchor='centre'):
        """The component's box fitted into bw x bh around (x, y) (or with its
        bottom on y, for anchor='bottom')."""
        bx0, by0, bx1, by1 = comp['box']
        k = min(bw / (bx1 - bx0), bh / (by1 - by0))
        cx, cy = (bx0 + bx1) / 2, (by0 + by1) / 2
        if anchor == 'bottom':
            y = y - (by1 - by0) * k / 2
        t = 'translate(%s %s)%s scale(%s) translate(%s %s)' % (
            n(x), n(y), ' rotate(180)' if turn else '', n(k), n(-cx), n(-cy))
        return '<use href="#c-%s" transform="%s"/>' % (original, t)

    def picture(self, face, fw, fh):
        build = face.get('build')
        if build == 'rule':
            return self.back(fw, fh)
        comp = self.component(face['original'])
        if build == 'pips':
            return self.pips(face, comp, fw, fh)
        k = INSET.get(self.s.kind, 1)
        if build == 'double':
            top_h = fh / 2
            bx0, by0, bx1, by1 = comp['box']
            s_ = min(top_h * 0.84 / (by1 - by0), fw * 0.8 / (bx1 - bx0))
            # Each half reaches one unit past the middle, so no seam shows.
            half = self.placed(comp, face['original'], fw / 2, top_h + 1, (bx1 - bx0) * s_, (by1 - by0) * s_,
                               anchor='bottom')
            art = '%s<g transform="rotate(180 %s %s)">%s</g>' % (half, n(fw / 2), n(fh / 2), half)
        else:
            # The original covers the card (its centre), as the model drew it.
            s_ = max(fw / comp['w'], fh / comp['h'])
            art = '<use href="#c-%s" transform="translate(%s %s) scale(%s) translate(%s %s)"/>' % (
                face['original'], n(fw / 2), n(fh / 2), n(s_), n(-comp['w'] / 2), n(-comp['h'] / 2))
        if k < 1:
            art = '<g transform="translate(%s %s) scale(%s) translate(%s %s)">%s</g>' % (
                n(fw / 2), n(fh / 2), n(k), n(-fw / 2), n(-fh / 2), art)
        return art

    def pips(self, face, comp, w, h):
        suit, count = face['id'].split('-')[0], face['pips']
        out = []
        if suit in LONG_MARKS:
            rows = LONG_ROWS[count]
            bh = (0.62 if len(rows) == 1 else 0.6 / len(rows) * 0.92) * h
            for r, c in enumerate(rows):
                cy = (0.5 if len(rows) == 1 else 0.2 + 0.6 * r / (len(rows) - 1)) * h
                bw = 0.56 * w / c * 0.9
                for i in range(c):
                    out.append(self.placed(comp, face['original'], (0.22 + 0.56 * (i + 0.5) / c) * w, cy, bw, bh))
            return ''.join(out)
        bw, bh = (0.2 if count <= 6 else 0.18) * w, (0.17 if count <= 8 else 0.15) * h
        turn = self.s.name == 'french'
        for x, y in PIP_LAYOUT[count]:
            out.append(self.placed(comp, face['original'], x * w, y * h, bw, bh, turn and y > 0.5))
        return ''.join(out)

    def back(self, w, h, across=12):
        step = w / across
        self.define('dots', '<pattern id="dots" width="%s" height="%s" patternUnits="userSpaceOnUse" x="0" y="%s">'
                            '<circle cx="%s" cy="%s" r="%s" fill="%s"/></pattern>' % (
                                n(step), n(step), n((h - step * int(h / step)) / 2), n(step / 2), n(step / 2),
                                n(step * 0.21), INK))
        return '<rect width="%s" height="%s" fill="%s"/><rect width="%s" height="%s" fill="url(#dots)"/>' % (
            n(w), n(h), spec.LAVENDER.upper(), n(w), n(h))

    def card(self, face):
        fw, fh = self.body()
        inner = self.picture(face, fw, fh)
        out = '<use href="#body"/><g clip-path="url(#clip)">%s</g>' % inner
        if face.get('index'):
            out += self.index(face, fw, fh)
        return out

    def cube(self, face):
        w, h = self.s.cell
        fw, fh = self.body()
        v = int(face['id'].split('-')[1])
        spots = {1: [(1, 1)], 2: [(0, 0), (2, 2)], 3: [(0, 0), (1, 1), (2, 2)], 4: [(0, 0), (2, 0), (0, 2), (2, 2)],
                 5: [(0, 0), (2, 0), (1, 1), (0, 2), (2, 2)], 6: [(0, 0), (2, 0), (0, 1), (2, 1), (0, 2), (2, 2)]}[v]
        out = ['<use href="#body"/>']
        for gx, gy in spots:
            cx, cy = fw * (0.27 + 0.23 * gx), fh * (0.27 + 0.23 * gy)
            if v == 1:
                r = fw * 0.15
                e = self.s.edge
                out.append('<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>' % (
                    n(cx - r), n(cy - r), n(2 * r), n(2 * r), INK))
                out.append('<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>' % (
                    n(cx - r + e), n(cy - r + e), n(2 * r - 2 * e), n(2 * r - 2 * e), spec.PINK.upper()))
            else:
                r = fw * 0.085
                out.append('<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>' % (
                    n(cx - r), n(cy - r), n(2 * r), n(2 * r), INK))
        return ''.join(out)

    def poly_points(self, key):
        w, h = self.s.cell
        shape = POLY_DRAW[key][0]
        cx, cy, r = w / 2, h / 2, w * 0.42
        if shape == 'kite':
            return [(cx, cy - r), (cx + r * 0.72, cy - r * 0.18), (cx, cy + r), (cx - r * 0.72, cy - r * 0.18)]
        sides = {'triangle': 3, 'pentagon': 5, 'hexagon': 6}[shape]
        a0 = -math.pi / 2 if sides % 2 else 0
        pts = [(cx + r * math.cos(a0 + 2 * math.pi * i / sides), cy + r * math.sin(a0 + 2 * math.pi * i / sides))
               for i in range(sides)]
        # Sit the shape in the middle of the cell by its own box.
        ys = [y for _, y in pts]
        dy = cy - (min(ys) + max(ys)) / 2
        return [(x, y + dy) for x, y in pts]

    def poly(self, face):
        pts = self.poly_points(face['id'])
        fill = POLY_DRAW[face['id']][1].upper()
        return '<path d="%s" fill="%s" stroke="%s" stroke-width="%s" stroke-linejoin="miter"/>' % (
            poly_d(pts), fill, INK, n(4))

    def text_box(self, key):
        pts = self.poly_points(key)
        x0, x1 = min(x for x, _ in pts), max(x for x, _ in pts)
        y0, y1 = min(y for _, y in pts), max(y for _, y in pts)
        cx, cy, fw, fh = spec.POLY[key]['box']
        bw, bh = x1 - x0, y1 - y0
        w, h = bw * fw, bh * fh
        return {'x': round(x0 + bw * cx - w / 2), 'y': round(y0 + bh * cy - h / 2), 'w': round(w), 'h': round(h)}

    def add(self, face):
        s = self.s
        if s.kind == 'die':
            inner = self.cube(face)
        elif s.kind == 'poly':
            inner = self.poly(face)
        else:
            inner = self.card(face)
        self.symbols.append('<symbol id="%s" viewBox="0 0 %d %d">%s</symbol>' % (face['id'], s.cell[0], s.cell[1], inner))

    def svg(self):
        return ('<svg xmlns="http://www.w3.org/2000/svg"><defs>%s</defs>%s</svg>\n' % (
            ''.join(self.defs), ''.join(self.symbols)))


def build_sheet(name, originals, faces=None, cache=CACHE):
    """(svg text, web index, review notes) for one sheet."""
    s = spec.BY_NAME[name]
    faces = faces if faces is not None else inventory.build(inventory.load_registry())[name]
    sp = Sprite(s, originals, cache)
    for f in faces:
        sp.add(f)
    index = {'sheet': name, 'file': name + '.svg', 'cell': list(s.cell), 'faces': [f['id'] for f in faces],
             'decks': s.decks}
    if s.kind == 'poly':
        index['text'] = {f['id']: sp.text_box(f['id']) for f in faces}
        index['text_colour'] = spec.SUIT_BLACK
        index['sides'] = {str(v): k for k, p in spec.POLY.items() for v in p['sides']}
        index['other'] = 'dn'
    return sp.svg(), index, sp.notes


def atlas(svg, index, scale=ATLAS_SCALE, lossy=None):
    """The app's atlas: every face of the sprite rendered with resvg in a
    grid, and its index (cell and size in logical units, the picture at
    `scale` times them)."""
    import resvg_py
    from PIL import Image
    s = spec.BY_NAME[index['sheet']]
    cw, ch = s.cell
    cols = s.columns
    faces = index['faces']
    rows = math.ceil(len(faces) / cols)
    W, H = cw * cols, ch * rows
    body = svg.split('<svg xmlns="http://www.w3.org/2000/svg">', 1)[1].rsplit('</svg>', 1)[0]
    uses = ''.join('<use href="#%s" x="%d" y="%d" width="%d" height="%d"/>' % (f, (i % cols) * cw, (i // cols) * ch, cw, ch)
                   for i, f in enumerate(faces))
    page = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">%s%s</svg>' % (
        W, H, round(W * scale), round(H * scale), body, uses)
    png = bytes(resvg_py.svg_to_bytes(svg_string=page))
    img = Image.open(io.BytesIO(png)).convert('RGBA')
    buf = io.BytesIO()
    if lossy is None:
        img.save(buf, 'WEBP', lossless=True, quality=100, method=6, exact=False)
    else:
        img.save(buf, 'WEBP', quality=lossy, alpha_quality=100, method=6)
    app_index = {'sheet': index['sheet'], 'image': index['sheet'] + '.webp', 'cell': [cw, ch], 'columns': cols,
                 'rows': rows, 'size': [W, H],
                 'faces': {f: [i % cols, i // cols] for i, f in enumerate(faces)}, 'decks': index['decks']}
    for k in ('text', 'text_colour', 'sides', 'other'):
        if k in index:
            app_index[k] = index[k]
    return buf.getvalue(), app_index


def render_face(svg, face, w, h):
    """One face of a sprite as a PIL image, for review and tests."""
    import resvg_py
    from PIL import Image
    body = svg.split('<svg xmlns="http://www.w3.org/2000/svg">', 1)[1].rsplit('</svg>', 1)[0]
    page = '<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">%s<use href="#%s" width="%d" height="%d"/></svg>' % (
        w, h, body, face, w, h)
    return Image.open(io.BytesIO(bytes(resvg_py.svg_to_bytes(svg_string=page)))).convert('RGBA')


def write_manifest(web_dir):
    names = sorted(f[:-5] for f in os.listdir(web_dir) if f.endswith('.json')
                   and os.path.exists(os.path.join(web_dir, f[:-5] + '.svg')))
    text = ('// Written by tools/art-gen/pack.py. Do not edit.\n//\n'
            '// The chance artwork sprites this server has (docs/ART.md). Empty: none, and\n'
            '// the web chat draws chance results as text.\n\n'
            'export const SHEETS = %s;\n' % json.dumps(names))
    with open(os.path.join(web_dir, 'sheets.js'), 'w') as f:
        f.write(text)
    return names


def sizes(data):
    out = [len(data), len(gzip.compress(data, 9, mtime=0))]
    try:
        import brotli
        out.append(len(brotli.compress(data, quality=11)))
    except ImportError:
        out.append(None)
    return out


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--set', action='append', default=[], choices=[s.name for s in spec.SHEETS])
    ap.add_argument('--originals', default=spec.ORIGINALS)
    ap.add_argument('--cache', default=None, help='where traced components are kept (default <originals>/.traced)')
    ap.add_argument('--web', default=spec.WEB_DIR, help='the web chat folder (default shared/chance)')
    ap.add_argument('--app', default=spec.APP_DIR, help="the app's folder (default app/assets/chance)")
    ap.add_argument('--no-app', action='store_true')
    ap.add_argument('--lossy', type=int, default=None, metavar='Q', help='lossy WebP atlases at quality Q')
    ap.add_argument('--review', metavar='FILE', help='write the review list (JSON) here')
    args = ap.parse_args(argv)
    cache = args.cache or os.path.join(args.originals, '.traced')
    try:
        all_faces = inventory.build(inventory.load_registry())
    except inventory.Drift as e:
        sys.exit('inventory: ' + str(e))
    names = args.set or [s.name for s in spec.SHEETS]
    os.makedirs(args.web, exist_ok=True)
    if not args.no_app:
        os.makedirs(args.app, exist_ok=True)
    review = {}
    for name in names:
        svg, index, notes = build_sheet(name, args.originals, all_faces[name], cache)
        data = svg.encode()
        with open(os.path.join(args.web, name + '.svg'), 'wb') as f:
            f.write(data)
        with open(os.path.join(args.web, name + '.json'), 'w') as f:
            f.write(json.dumps(index, separators=(',', ':')) + '\n')
        raw, gz, br = sizes(data)
        line = '%-10s %3d faces  svg %7d bytes, gzip %6d, brotli %s' % (name, len(index['faces']), raw, gz, br)
        if not args.no_app:
            webp, app_index = atlas(svg, index, lossy=args.lossy)
            with open(os.path.join(args.app, name + '.webp'), 'wb') as f:
                f.write(webp)
            with open(os.path.join(args.app, name + '.json'), 'w') as f:
                f.write(json.dumps(app_index, separators=(',', ':')) + '\n')
            line += '; app atlas %d bytes' % len(webp)
        print(line, flush=True)
        if notes:
            review[name] = notes
    print('web chat lists:', ', '.join(write_manifest(args.web)) or 'nothing')
    for sheet, faces in review.items():
        for face, notes in faces.items():
            print('review %s/%s: %s' % (sheet, face, '; '.join(notes)))
    if args.review:
        with open(args.review, 'w') as f:
            json.dump(review, f, indent=1, sort_keys=True)


if __name__ == '__main__':
    main()

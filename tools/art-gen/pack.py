#!/usr/bin/env python3
"""Packs the originals into one sprite sheet per set, with its index
(docs/ART.md).

    python3 tools/art-gen/pack.py                      every sheet
    python3 tools/art-gen/pack.py --set french --set tarot
    python3 tools/art-gen/pack.py --originals /tmp/ph --web /tmp/w --app /tmp/a

For each face: the original is cut to the cell's proportions (centred), cut
again at the top-right corner (transparent), edged in ink, stamped with its
index in Archivo 900 narrow, and shrunk to the cell with a Lanczos filter.
All drawing is done at four times the cell and shrunk once, so edges and
the cut are smooth. The cells go in a uniform grid, left to right, top to
bottom, in the inventory's order. Writes <sheet>.webp (lossless) and
<sheet>.json into the web chat's static tree and the app's assets, and
the web chat's list of sheets, shared/chance/sheets.js. The same originals
always give the same bytes.
"""
import argparse, io, json, math, os, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import spec, inventory  # noqa: E402

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:  # pragma: no cover
    sys.exit('pack.py needs Pillow: python3 -m pip install Pillow (docs/ART.md)')

SS = 4  # drawing scale
IMAGE_EXT = ('.png', '.jpg', '.jpeg', '.webp')
TILE_BLOCK = 8  # the mahjong tile's offset block, at cell size
# Cards are shrunk to this share of the cell and laid on their own paper,
# so the picture keeps clear of the stamped index even where the model
# reaches into the corners.
INSET = {'card': 0.9, 'hanafuda': 0.94}


def rgb(h):
    return tuple(int(h[i:i + 2], 16) for i in (1, 3, 5))


def find_original(folder, sheet, original):
    for ext in IMAGE_EXT:
        p = os.path.join(folder, sheet, original + ext)
        if os.path.exists(p):
            return p
    return None


def cover(img, size):
    """Cuts img to size's proportions around its centre and scales it."""
    w, h = img.size
    tw, th = size
    if w * th > h * tw:  # too wide
        nw = round(h * tw / th)
        img = img.crop(((w - nw) // 2, 0, (w - nw) // 2 + nw, h))
    else:
        nh = round(w * th / tw)
        img = img.crop((0, (h - nh) // 2, w, (h - nh) // 2 + nh))
    return img.resize(size, Image.LANCZOS)


def border_colour(img):
    """The original's own paper: the median of its outermost pixels."""
    w, h = img.size
    px = [img.getpixel((x, y)) for x in range(0, w, 7) for y in (1, h - 2)] + \
         [img.getpixel((x, y)) for y in range(0, h, 7) for x in (1, w - 2)]
    return tuple(sorted(c[i] for c in px)[len(px) // 2] for i in range(3))


def inset(img, k):
    """img shrunk to k of its size, centred on its own paper."""
    if k >= 1:
        return img
    w, h = img.size
    small = img.resize((round(w * k), round(h * k)), Image.LANCZOS)
    out = Image.new('RGB', (w, h), border_colour(img))
    out.paste(small, ((w - small.width) // 2, (h - small.height) // 2))
    return out


def outline(w, h, cut, inset=0.0):
    """The cut-corner rectangle, inset by `inset` on every side."""
    e = inset
    if cut <= 0:
        return [(e, e), (w - e, e), (w - e, h - e), (e, h - e)]
    d = e * math.sqrt(2) - e
    return [(e, e), (w - cut - d, e), (w - e, cut + d), (w - e, h - e), (e, h - e)]


def font(size):
    f = ImageFont.truetype(spec.FONT, size)
    f.set_variation_by_axes([spec.FONT_WEIGHT, spec.FONT_WIDTH])
    return f


# The small suit marks of the index, drawn from straight lines and 45°
# cuts like the rest of Churr, in a unit box (x right, y down).
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


def stamp_index(canvas, text, colour, edge, mark=None):
    """The index (the rank, and under it a small suit mark where the suit
    has one) in the top-left corner, and turned half round in the
    bottom-right. Sizes are at drawing scale."""
    w, h = canvas.size
    size = round(w * 0.14)
    f = font(size)
    layer = Image.new('RGBA', (w, h), (0, 0, 0, 0))
    d = ImageDraw.Draw(layer)
    # Each index is centred on the same column, so "A" and "10" line up.
    cx = edge + round(w * 0.11)
    top = edge + round(w * 0.065)
    d.text((cx, top), text, font=f, fill=colour + (255,), anchor='mt')
    if mark:
        box = d.textbbox((cx, top), text, font=f, anchor='mt')
        m = round(w * 0.085)
        x0, y0 = cx - m / 2, box[3] + round(w * 0.025)
        for poly in MARKS[mark]:
            d.polygon([(x0 + x * m, y0 + y * m) for x, y in poly], fill=colour + (255,))
    canvas.alpha_composite(layer)
    canvas.alpha_composite(layer.rotate(180))


def face_card(img, s, face, block=0):
    """A card, hanafuda card, tile or cube die face at drawing scale."""
    W, H = s.cell[0] * SS, s.cell[1] * SS
    fw, fh = W - block * SS, H - block * SS
    cut, edge = s.cut * SS, s.edge * SS
    art = inset(cover(img.convert('RGB'), (fw, fh)), INSET.get(s.kind, 1)).convert('RGBA')
    if face.get('index'):
        stamp_index(art, face['index'], rgb(spec.SUIT_RED if face['red'] else spec.SUIT_BLACK), edge, face.get('mark'))
    out = Image.new('RGBA', (W, H), (0, 0, 0, 0))
    d = ImageDraw.Draw(out)
    if block:
        b = block * SS
        d.polygon([(x + b, y + b) for x, y in outline(fw, fh, cut)], fill=rgb(spec.LAVENDER))
    d.polygon(outline(fw, fh, cut), fill=rgb(spec.INK))
    mask = Image.new('L', (fw, fh), 0)
    ImageDraw.Draw(mask).polygon(outline(fw, fh, cut, edge), fill=255)
    out.paste(art, (0, 0), mask)
    return out


def face_poly(img, s):
    """A polyhedral die shape: the paper around it made transparent."""
    W, H = s.cell[0] * SS, s.cell[1] * SS
    art = cover(img.convert('RGB'), (W, H)).convert('RGBA')
    for xy in [(0, 0), (W - 1, 0), (0, H - 1), (W - 1, H - 1)]:
        if art.getpixel(xy)[3]:
            ImageDraw.floodfill(art, xy, (0, 0, 0, 0), thresh=60)
    return art


def text_box(cell_img, key):
    """Where the clients set a die's number, in cell pixels."""
    bbox = cell_img.getchannel('A').point(lambda a: 255 if a > 127 else 0).getbbox()
    if not bbox:
        bbox = (0, 0) + cell_img.size
    x0, y0, x1, y1 = bbox
    cx, cy, fw, fh = spec.POLY[key]['box']
    bw, bh = x1 - x0, y1 - y0
    w, h = round(bw * fw), round(bh * fh)
    return {'x': round(x0 + bw * cx - w / 2), 'y': round(y0 + bh * cy - h / 2), 'w': w, 'h': h}


def pack_cell(img, s, face):
    """One face of sheet s, cut, edged, stamped and shrunk to its cell."""
    if s.kind == 'poly':
        big = face_poly(img, s)
    else:
        big = face_card(img, s, face, TILE_BLOCK if s.kind == 'tile' else 0)
    return big.resize(s.cell, Image.LANCZOS)


def pack_cells(originals, wanted, all_faces, out):
    """Packs only the faces named sheet/face (a reference board) and
    writes each cell as <out>/<sheet>--<face>.png, for judging."""
    os.makedirs(out, exist_ok=True)
    for w in wanted:
        sheet, fid = w.split('/', 1)
        face = next((f for f in all_faces.get(sheet, []) if f['id'] == fid), None)
        if face is None:
            raise SystemExit('no face %s' % w)
        path = find_original(originals, sheet, face['original'])
        if not path:
            raise SystemExit('no original for %s' % w)
        with Image.open(path) as im:
            cell = pack_cell(im.convert('RGB'), spec.BY_NAME[sheet], face)
        dest = os.path.join(out, '%s--%s.png' % (sheet, fid))
        cell.save(dest)
        print(dest)


def encode(img, lossy=None):
    buf = io.BytesIO()
    if lossy is None:
        img.save(buf, 'WEBP', lossless=True, quality=100, method=6, exact=False)
    else:
        img.save(buf, 'WEBP', quality=lossy, alpha_quality=100, method=6)
    return buf.getvalue()


def pack_sheet(name, originals, faces=None, lossy=None):
    """Returns (webp bytes, index dict) for one sheet."""
    s = spec.BY_NAME[name]
    faces = faces if faces is not None else inventory.build(inventory.load_registry())[name]
    cw, ch = s.cell
    cols = s.columns
    rows = math.ceil(len(faces) / cols)
    sheet = Image.new('RGBA', (cw * cols, ch * rows), (0, 0, 0, 0))
    index = {'sheet': name, 'image': name + '.webp', 'cell': [cw, ch], 'columns': cols, 'rows': rows,
             'size': [cw * cols, ch * rows], 'faces': {}}
    missing = []
    cache = {}
    for n, face in enumerate(faces):
        path = find_original(originals, name, face['original'])
        if not path:
            missing.append(face['original'])
            continue
        if path not in cache:
            with Image.open(path) as im:
                im.load()
                cache[path] = im.copy()
        img = cache[path]
        cell = pack_cell(img, s, face)
        col, row = n % cols, n // cols
        sheet.paste(cell, (col * cw, row * ch))
        index['faces'][face['id']] = [col, row]
        if s.kind == 'poly':
            index.setdefault('text', {})[face['id']] = text_box(cell, face['id'])
    if missing:
        raise SystemExit('%s: no original for %s (in %s)' % (name, ', '.join(sorted(set(missing))),
                                                               os.path.join(originals, name)))
    if s.kind == 'poly':
        index['text_colour'] = spec.SUIT_BLACK
        index['sides'] = {str(n): k for k, v in spec.POLY.items() for n in v['sides']}
        index['other'] = 'dn'
    index['decks'] = s.decks
    return encode(sheet, lossy), index


def write_manifest(web_dir):
    names = sorted(f[:-5] for f in os.listdir(web_dir) if f.endswith('.json')
                   and os.path.exists(os.path.join(web_dir, f[:-5] + '.webp')))
    text = ('// Written by tools/art-gen/pack.py. Do not edit.\n//\n'
            '// The chance artwork sheets this server has (docs/ART.md). Empty: none, and\n'
            '// the web chat draws chance results as text.\n\n'
            'export const SHEETS = %s;\n' % json.dumps(names))
    with open(os.path.join(web_dir, 'sheets.js'), 'w') as f:
        f.write(text)
    return names


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--set', action='append', default=[], choices=[s.name for s in spec.SHEETS])
    ap.add_argument('--originals', default=spec.ORIGINALS)
    ap.add_argument('--web', default=spec.WEB_DIR, help='the web chat folder (default shared/chance)')
    ap.add_argument('--app', default=spec.APP_DIR, help="the app's folder (default app/assets/chance)")
    ap.add_argument('--no-app', action='store_true')
    ap.add_argument('--lossy', type=int, default=None, metavar='Q', help='lossy WebP at quality Q instead')
    ap.add_argument('--cells', metavar='DIR', help='pack only the --only faces, each as a PNG cell in DIR')
    ap.add_argument('--only', action='append', default=[], metavar='SHEET/FACE',
                    help="with --cells: a face to pack ('reference-board' for the board's six)")
    args = ap.parse_args(argv)
    reg = inventory.load_registry()
    try:
        all_faces = inventory.build(reg)
    except inventory.Drift as e:
        sys.exit('inventory: ' + str(e))
    if args.cells:
        import prompts
        wanted = []
        for o in args.only or ['reference-board']:
            wanted += ['%s/%s' % x for x in prompts.REFERENCE_BOARD] if o == 'reference-board' else [o]
        pack_cells(args.originals, wanted, all_faces, args.cells)
        return
    names = args.set or [s.name for s in spec.SHEETS]
    outs = [args.web] + ([] if args.no_app else [args.app])
    for d in outs:
        os.makedirs(d, exist_ok=True)
    for name in names:
        data, index = pack_sheet(name, args.originals, all_faces[name], args.lossy)
        text = json.dumps(index, separators=(',', ':')) + '\n'
        for d in outs:
            with open(os.path.join(d, name + '.webp'), 'wb') as f:
                f.write(data)
            with open(os.path.join(d, name + '.json'), 'w') as f:
                f.write(text)
        print('%-10s %4d x %-4d %3d faces %8d bytes' % (name, index['size'][0], index['size'][1],
                                                         len(index['faces']), len(data)))
    print('web chat lists:', ', '.join(write_manifest(args.web)) or 'nothing')


if __name__ == '__main__':
    main()

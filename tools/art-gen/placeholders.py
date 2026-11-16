#!/usr/bin/env python3
"""Plain stand-in originals, drawn with Pillow, for every face, so the
packer and the clients can be tried before real artwork exists
(docs/ART.md). Never packed into the product.

    python3 tools/art-gen/placeholders.py --out /tmp/churr-placeholders
    python3 tools/art-gen/pack.py --originals /tmp/churr-placeholders --web /tmp/w --app /tmp/a

Each is paper with an ink edge, the cut corner and the face's id in
Archivo, at the size nano-banana gives at 1K for the sheet's aspect; a
polyhedral die is its polygon in a neon, so the packer's background
removal and number box are exercised too.
"""
import argparse, math, os, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import spec, inventory  # noqa: E402

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:  # pragma: no cover
    sys.exit('placeholders.py needs Pillow: python3 -m pip install Pillow (docs/ART.md)')

# Roughly what the model returns at 1K, by aspect.
SIZES = {'2:3': (832, 1248), '4:5': (896, 1120), '3:4': (864, 1152), '1:1': (1024, 1024)}
POLY_SHAPE = {'d4': (3, spec.CYAN), 'd8': (3, spec.YELLOW), 'd20': (3, spec.LAVENDER), 'd10': ('kite', spec.PINK),
              'd100': ('kite', spec.YELLOW), 'd12': (5, spec.LAVENDER), 'dn': (6, spec.PAPER)}


def rgb(h):
    return tuple(int(h[i:i + 2], 16) for i in (1, 3, 5))


def font(size):
    f = ImageFont.truetype(spec.FONT, size)
    f.set_variation_by_axes([spec.FONT_WEIGHT, spec.FONT_WIDTH])
    return f


def polygon(kind, w, h):
    cx, cy, r = w / 2, h / 2, w * 0.38
    if kind == 'kite':
        return [(cx, cy - r), (cx + r * 0.8, cy - r * 0.15), (cx, cy + r), (cx - r * 0.8, cy - r * 0.15)]
    n = kind
    start = -math.pi / 2 if n % 2 else -math.pi / 2 + math.pi / n
    return [(cx + r * math.cos(start + 2 * math.pi * i / n), cy + r * math.sin(start + 2 * math.pi * i / n) + r * 0.1)
            for i in range(n)]


def draw(sheet, face_id):
    s = spec.BY_NAME[sheet]
    w, h = SIZES[s.aspect]
    img = Image.new('RGB', (w, h), rgb(spec.PAPER))
    d = ImageDraw.Draw(img)
    line = max(4, w // 60)
    if s.kind == 'poly':
        kind, fill = POLY_SHAPE[face_id]
        d.polygon(polygon(kind, w, h), fill=rgb(fill), outline=rgb(spec.INK), width=line)
        return img
    m = w // 8
    cut = w // 10
    d.polygon([(m, m), (w - m - cut, m), (w - m, m + cut), (w - m, h - m), (m, h - m)],
              outline=rgb(spec.INK), width=line)
    f = font(max(24, w // (len(face_id) // 2 + 4)))
    d.text((w / 2, h / 2), face_id, font=f, fill=rgb(spec.INK), anchor='mm')
    d.text((w / 2, h - m - line * 3), sheet, font=font(w // 16), fill=rgb(spec.LAVENDER), anchor='md')
    return img


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--out', required=True, help='a folder outside the product trees')
    ap.add_argument('--set', action='append', default=[], choices=[s.name for s in spec.SHEETS])
    args = ap.parse_args(argv)
    out = os.path.abspath(args.out)
    for d in (spec.WEB_DIR, spec.APP_DIR):
        if out == os.path.abspath(d) or out.startswith(os.path.abspath(d) + os.sep):
            sys.exit('placeholders never go into the product trees')
    faces = inventory.build(inventory.load_registry())
    n = 0
    for s in spec.SHEETS:
        if args.set and s.name not in args.set:
            continue
        folder = os.path.join(out, s.name)
        os.makedirs(folder, exist_ok=True)
        for o in inventory.originals(faces[s.name]):
            draw(s.name, o).save(os.path.join(folder, o + '.png'), optimize=False)
            n += 1
    print('%d placeholders in %s' % (n, out))


if __name__ == '__main__':
    main()

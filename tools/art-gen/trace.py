#!/usr/bin/env python3
"""Turns a generated original into a vector component in the card palette
(docs/ART.md, ruling R82): snap every pixel to the palette, then trace.

    python3 tools/art-gen/trace.py art-originals/french/mark-hearts.jpeg   print the component

The snap (settings from the tracing trial, "A-vt-poly-peaks"): upscale 2x
(Lanczos), 3 px median, nearest palette colour with navy and suit black
merged, thick off-palette areas repainted as one block in the nearest fill
colour (pale skin tints to paper), a 5 px mode filter, then each dark
connected shape made navy or suit black as a whole by its mean colour.
The trace: vtracer, colour mode, stacked, polygons. Every fill is then set
to the exact palette hex, the paper ground is dropped (a component is laid
on a card), and points are written relative.

A component is {'w', 'h', 'box', 'paths': [(hex, d)], 'notes'}: the
traced size (2x the original), the box around everything that is not
paper, the paths bottom to top, and what the review should look at (large
areas whose colour the snap changed, specks left after the snap). The same
original always gives the same component.
"""
import io, json, os, re, sys

import numpy as np
from PIL import Image, ImageFilter
from scipy import ndimage as ndi
import vtracer

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import spec  # noqa: E402

# Bump when the snap or the trace changes, so cached components are redone.
VERSION = 2
NAMES = ['paper', 'ink', 'lavender', 'cyan', 'pink', 'yellow', 'red', 'black']
HEX = [spec.PAPER, spec.INK, spec.LAVENDER, spec.CYAN, spec.PINK, spec.YELLOW, spec.SUIT_RED, spec.SUIT_BLACK]
HEX = [h.upper() for h in HEX]
PAL = np.array([[int(h[i:i + 2], 16) for i in (1, 3, 5)] for h in HEX], float)
PAPER, INK, RED, BLACK = 0, 1, 6, 7
FILLS = [0, 2, 3, 4, 5]
# Review thresholds, in pixels of the 2x snap.
REPAINT_FLAG = 20000
SPECK = 600


def nearest(rgb, pal=PAL):
    d = ((rgb[..., None, :].astype(float) - pal[None, None]) ** 2).sum(-1)
    return d.argmin(-1).astype(np.uint8)


def snap(img):
    """The original as palette indices at twice its size, and notes."""
    notes = []
    img = img.convert('RGB')
    w, h = img.size
    big = img.resize((w * 2, h * 2), Image.LANCZOS).filter(ImageFilter.MedianFilter(3))
    a = np.asarray(big)
    pal7 = PAL[:7]
    idx = nearest(a, pal7)
    dmin = np.sqrt(((a[..., None, :].astype(float) - pal7[None, None]) ** 2).sum(-1).min(-1))
    far = dmin > 30
    thick = ndi.binary_dilation(ndi.binary_opening(far, iterations=4), iterations=2) & far
    lab, n = ndi.label(thick)
    for r in range(1, n + 1):
        m = lab == r
        size = int(m.sum())
        if size < 2000:
            continue
        med = np.median(a[m], 0)
        if int(nearest(med[None, None], pal7)[0, 0]) in (INK, RED):
            continue  # dark ink or suit red that drifted: the plain snap is right
        if med.min() > 160 and med @ [0.299, 0.587, 0.114] > 190:
            k = PAPER  # a pale tint (skin): faces and hands are paper
        else:
            k = FILLS[int(nearest(med[None, None], pal7[FILLS])[0, 0])]
        idx[m] = k
        if size >= REPAINT_FLAG:
            notes.append('repainted %d px of #%02X%02X%02X as %s' % ((size,) + tuple(int(v) for v in med) + (NAMES[k],)))
    idx = np.asarray(Image.fromarray(idx).filter(ImageFilter.ModeFilter(5))).copy()
    dark = idx == INK
    lab, n = ndi.label(dark)
    if n:
        means = np.stack([ndi.mean(a[..., c].astype(float), lab, np.arange(1, n + 1)) for c in range(3)], -1)
        black = np.concatenate([[False], ((means - PAL[BLACK]) ** 2).sum(-1) < ((means - PAL[INK]) ** 2).sum(-1)])
        idx[black[lab]] = BLACK
    # Specks: small islands standing alone on the paper that are big enough
    # to survive the trace (it drops anything under 8 x 8 px).
    lab, n = ndi.label(idx != PAPER)
    if n:
        sizes = np.bincount(lab.ravel())[1:]
        specks = int(((sizes >= 64) & (sizes < SPECK)).sum())
        if specks:
            notes.append('%d small islands (%d to %d px) on the paper' % (specks, 64, SPECK))
    return idx, notes


_PATH = re.compile(r'<path d="([^"]+)" fill="(#[0-9A-Fa-f]{6})" transform="translate\(([-\d.]+),([-\d.]+)\)"\s*/>')


def relative(d, tx, ty):
    """A vtracer polygon path, moved by (tx, ty), written with relative
    moves and lines and no needless spaces."""
    out = []
    cx = cy = 0.0
    start = None
    for cmd, args in re.findall(r'([MLZ])([^MLZ]*)', d):
        if cmd == 'Z':
            out.append('z')
            if start:
                cx, cy = start
            continue
        nums = [float(v) for v in re.findall(r'-?\d+(?:\.\d+)?', args)]
        for i in range(0, len(nums), 2):
            x, y = nums[i] + tx, nums[i + 1] + ty
            dx, dy = x - cx, y - cy
            if cmd == 'M' and i == 0:
                out.append('m%s %s' % (num(dx), num(dy)))
                start = (x, y)
            else:
                out.append('l%s %s' % (num(dx), num(dy)) if not out or out[-1][0] != 'l' else
                           ' %s %s' % (num(dx), num(dy)))
            cx, cy = x, y
    s = ''.join(out)
    return re.sub(r' -', '-', s)


def num(v):
    s = ('%.1f' % v).rstrip('0').rstrip('.')
    return '0' if s in ('-0', '') else s


def trace_indices(idx):
    h, w = idx.shape
    rgb = PAL[idx].astype(np.uint8)
    b = io.BytesIO()
    Image.fromarray(rgb).save(b, 'PNG')
    svg = vtracer.convert_raw_image_to_svg(
        b.getvalue(), img_format='png', colormode='color', hierarchical='stacked', mode='polygon',
        filter_speckle=8, color_precision=8, layer_difference=8, corner_threshold=60, length_threshold=4.0,
        splice_threshold=45, path_precision=1)
    paths = []
    for d, fill, tx, ty in _PATH.findall(svg):
        c = PAL[nearest(np.array([[[int(fill[i:i + 2], 16) for i in (1, 3, 5)]]], float))[0, 0]]
        hexv = HEX[int(np.argmin(((PAL - c) ** 2).sum(-1)))]
        full = re.fullmatch(r'M0,0 L%d,0 L%d,%d L0,%d Z\s*' % (w, w, h, h), d)
        if full and hexv == HEX[PAPER]:
            continue  # the paper ground
        paths.append((hexv, relative(d, float(tx), float(ty))))
    return paths


def trace(path):
    """The component of an original file."""
    with Image.open(path) as im:
        idx, notes = snap(im)
    h, w = idx.shape
    ys, xs = np.nonzero(idx != PAPER)
    box = [int(xs.min()), int(ys.min()), int(xs.max()) + 1, int(ys.max()) + 1] if len(xs) else [0, 0, w, h]
    return {'version': VERSION, 'w': w, 'h': h, 'box': box, 'axis': axis(xs, ys), 'paths': trace_indices(idx),
            'notes': notes}


def axis(xs, ys):
    """The long axis of what is drawn: its angle from the vertical in
    degrees (clockwise), its length and width along that axis, and the
    centre of that box, so a long mark drawn on a slant can be stood up."""
    if len(xs) < 2:
        return {'angle': 0.0, 'long': 1.0, 'wide': 1.0, 'centre': [0.0, 0.0]}
    x, y = xs.astype(float), ys.astype(float)
    mx, my = x.mean(), y.mean()
    cov = np.cov(np.stack([x - mx, y - my]))
    vals, vecs = np.linalg.eigh(cov)
    vx, vy = vecs[:, int(np.argmax(vals))]
    if vy < 0:
        vx, vy = -vx, -vy
    a = float(np.degrees(np.arctan2(vx, vy)))  # from the downward vertical
    u = (x - mx) * vx + (y - my) * vy           # along the long axis
    v = (x - mx) * vy - (y - my) * vx           # across it
    cu, cv = (u.min() + u.max()) / 2, (v.min() + v.max()) / 2
    centre = [mx + cu * vx + cv * vy, my + cu * vy - cv * vx]
    return {'angle': round(a, 2), 'long': round(float(u.max() - u.min()), 1),
            'wide': round(float(v.max() - v.min()), 1), 'centre': [round(float(c), 1) for c in centre]}


def cached(path, cache_dir):
    """trace(path), kept in cache_dir by the original's name, size and
    time, so a sheet is not traced again for every pack."""
    st = os.stat(path)
    key = '%s-%d-%d-v%d.json' % (os.path.basename(path), st.st_size, int(st.st_mtime), VERSION)
    p = os.path.join(cache_dir, key)
    if os.path.exists(p):
        with open(p) as f:
            return json.load(f)
    comp = trace(path)
    os.makedirs(cache_dir, exist_ok=True)
    with open(p, 'w') as f:
        json.dump(comp, f)
    return comp


def main():
    comp = trace(sys.argv[1])
    print(json.dumps({k: v for k, v in comp.items() if k != 'paths'}))
    print('%d paths, %d bytes of path data' % (len(comp['paths']), sum(len(d) for _, d in comp['paths'])))


if __name__ == '__main__':
    main()

#!/usr/bin/env python3
"""Draws the desktop tray icons into assets/tray (docs/PUSH.md section 13).

The mark is the mascot of art/churr-sticker.png, the owl, reduced to an
outline drawn to docs/STYLE.md's icon rules: a 24-unit grid, a 2-unit
line, sharp joins, corners cut at 45 degrees instead of rounded, outline
only (the eyes and beak are the only solid parts). Each state adds a
different shape, so it reads without colour:

    normal         the owl
    unread         a small solid square, top right
    mention-N      a larger block, top right, with the count knocked out
    dnd            a crescent moon, top right
    disconnected   the owl's outline dotted, in grey

Two sets: template/ (black on transparent, 36 px, for the macOS menu bar,
which tints template images itself) and color/ (32 px, for the Windows
and Linux trays, whose backgrounds may be light or dark: the line is the
light theme's edge colour, which keeps 3:1 on both, and each mark has an
ink edge).

Also Android's small notification icon (white, drawable-*/ic_notification).

Run from app/:  python3 tool/tray_icons.py
Plain Python, no packages: PNGs are written with zlib.
"""

import os
import struct
import zlib

SS = 8  # supersampling per pixel

INK = (0x15, 0x17, 0x3A)
LINE = (0x7E, 0x73, 0xC2)  # --edge, light theme
GREY = (0x8B, 0x88, 0xB8)  # --faint, dark theme
YEL = (0xFF, 0xD9, 0x5E)
PINK = (0xFF, 0x5C, 0xCF)
ON_PINK = (0x0D, 0x10, 0x26)
BLACK = (0, 0, 0)

OWL = [(3, 7), (6, 2), (9, 5), (15, 5), (18, 2), (21, 7), (21, 18), (17, 22), (7, 22), (3, 18)]


def inset(poly, d):
    """Offsets a clockwise-in-screen polygon inwards by d with mitre joins."""
    n = len(poly)
    lines = []
    for i in range(n):
        (x1, y1), (x2, y2) = poly[i], poly[(i + 1) % n]
        dx, dy = x2 - x1, y2 - y1
        ln = (dx * dx + dy * dy) ** 0.5
        nx, ny = -dy / ln, dx / ln  # left normal; inwards for this winding
        lines.append(((x1 + nx * d, y1 + ny * d), (dx, dy)))
    out = []
    for i in range(n):
        (p, r), (q, s) = lines[i - 1], lines[i]
        cross = r[0] * s[1] - r[1] * s[0]
        t = ((q[0] - p[0]) * s[1] - (q[1] - p[1]) * s[0]) / cross
        out.append((p[0] + t * r[0], p[1] + t * r[1]))
    return out


def inside(poly, x, y):
    c = False
    n = len(poly)
    for i in range(n):
        (x1, y1), (x2, y2) = poly[i], poly[(i + 1) % n]
        if (y1 > y) != (y2 > y) and x < (x2 - x1) * (y - y1) / (y2 - y1) + x1:
            c = not c
    return c


def rect(x0, y0, x1, y1):
    return [(x0, y0), (x1, y0), (x1, y1), (x0, y1)]


# The digits of the count, 3 by 5 cells.
FONT = {
    '0': ['###', '#.#', '#.#', '#.#', '###'],
    '1': ['.#.', '##.', '.#.', '.#.', '###'],
    '2': ['###', '..#', '###', '#..', '###'],
    '3': ['###', '..#', '.##', '..#', '###'],
    '4': ['#.#', '#.#', '###', '..#', '..#'],
    '5': ['###', '#..', '###', '..#', '###'],
    '6': ['###', '#..', '###', '#.#', '###'],
    '7': ['###', '..#', '.#.', '.#.', '.#.'],
    '8': ['###', '#.#', '###', '#.#', '###'],
    '9': ['###', '#.#', '###', '..#', '###'],
    '+': ['...', '.#.', '###', '.#.', '...'],
}


def glyphs(text, x0, y0, cell):
    """Rectangles for text drawn from FONT, top left at (x0, y0)."""
    out = []
    x = x0
    for ch in text:
        for r, row in enumerate(FONT[ch]):
            for c, on in enumerate(row):
                if on == '#':
                    out.append(rect(x + c * cell, y0 + r * cell, x + (c + 1) * cell, y0 + (r + 1) * cell))
        x += 4 * cell
    return out


def layers(state, count, template):
    """Returns a list of (test(x, y) -> bool, colour) from bottom to top,
    plus a knock-out test that clears the owl under the state's mark."""
    inner = inset(OWL, 2)
    line = LINE if not template else BLACK
    if state == 'disconnected':
        line = GREY if not template else BLACK

    def outline(x, y):
        if not (inside(OWL, x, y) and not inside(inner, x, y)):
            return False
        if state == 'disconnected':
            # Dotted: two units on, two off, along the diagonal.
            return int((x + y) / 2.0) % 2 == 0
        return True

    eyes = [rect(7.5, 9, 10.5, 12), rect(13.5, 9, 16.5, 12)]
    beak = [(12, 13), (13.2, 14.6), (12, 16.2), (10.8, 14.6)]

    def face(x, y):
        return any(inside(e, x, y) for e in eyes) or inside(beak, x, y)

    out = [(outline, line), (face, line)]
    knock = lambda x, y: False
    if state == 'unread':
        box = rect(16, 0, 23, 7)
        edge = rect(15, -1, 24, 8)
        knock = lambda x, y: inside(rect(14, -2, 25, 9), x, y)
        if template:
            out.append((lambda x, y: inside(box, x, y), BLACK))
        else:
            out.append((lambda x, y: inside(edge, x, y), INK))
            out.append((lambda x, y: inside(box, x, y), YEL))
    elif state == 'mention':
        label = '9+' if count > 9 else str(max(count, 1))
        cell = 1.4
        w = (4 * len(label) - 1) * cell
        box = rect(23.5 - w - 3, 0, 24, 11)
        edge = rect(23.5 - w - 4, -1, 25, 12)
        bx = box[0][0]
        digits = glyphs(label, bx + (24 - bx - w) / 2, 0 + (11 - 5 * cell) / 2, cell)
        knock = lambda x, y: inside(rect(bx - 2.5, -2, 26, 13), x, y)
        if template:
            out.append((lambda x, y: inside(box, x, y) and not any(inside(d, x, y) for d in digits), BLACK))
        else:
            out.append((lambda x, y: inside(edge, x, y), INK))
            out.append((lambda x, y: inside(box, x, y), PINK))
            out.append((lambda x, y: any(inside(d, x, y) for d in digits), ON_PINK))
    elif state == 'dnd':
        def moon(x, y, grow=0.0):
            a = (x - 18.5) ** 2 + (y - 5.5) ** 2 <= (5 + grow) ** 2
            b = (x - 21.5) ** 2 + (y - 3.0) ** 2 <= (4.2 - grow) ** 2
            return a and not b
        knock = lambda x, y: moon(x, y, 2.0)
        if template:
            out.append((lambda x, y: moon(x, y), BLACK))
        else:
            out.append((lambda x, y: moon(x, y, 1.0), INK))
            out.append((lambda x, y: moon(x, y), PINK))
    return out, knock


def render(state, count, size, template):
    scale = 24.0 / size
    ls, knock = layers(state, count, template)
    px = bytearray()
    for py in range(size):
        px.append(0)  # PNG filter: none
        for pxx in range(size):
            acc = [0.0, 0.0, 0.0, 0.0]
            for sy in range(SS):
                for sx in range(SS):
                    x = (pxx + (sx + 0.5) / SS) * scale
                    y = (py + (sy + 0.5) / SS) * scale
                    col = None
                    for i, (test, c) in enumerate(ls):
                        if i < 2 and knock(x, y):
                            continue
                        if test(x, y):
                            col = c
                    if col is not None:
                        acc[0] += col[0]
                        acc[1] += col[1]
                        acc[2] += col[2]
                        acc[3] += 1
            n = SS * SS
            a = acc[3] / n
            if acc[3]:
                px += bytes([round(acc[0] / acc[3]), round(acc[1] / acc[3]), round(acc[2] / acc[3]), round(a * 255)])
            else:
                px += b'\0\0\0\0'
    return png(size, size, bytes(px))


def png(w, h, raw):
    def chunk(t, d):
        c = struct.pack('>I', len(d)) + t + d
        return c + struct.pack('>I', zlib.crc32(t + d) & 0xFFFFFFFF)
    return (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 6, 0, 0, 0))
            + chunk(b'IDAT', zlib.compress(raw, 9)) + chunk(b'IEND', b''))


def main():
    root = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'assets', 'tray')
    for template, size, folder in ((True, 36, 'template'), (False, 32, 'color')):
        d = os.path.join(root, folder)
        os.makedirs(d, exist_ok=True)
        jobs = [('normal', 0, 'normal'), ('unread', 0, 'unread'), ('dnd', 0, 'dnd'),
                ('disconnected', 0, 'disconnected')]
        jobs += [('mention', n, 'mention-%d' % n) for n in range(1, 10)] + [('mention', 10, 'mention-9plus')]
        for state, count, name in jobs:
            with open(os.path.join(d, name + '.png'), 'wb') as f:
                f.write(render(state, count, size, template))
    # Android's small notification icon: the owl in white on transparent,
    # at each density (android/app/src/main/res/drawable-*/).
    global BLACK
    BLACK = (255, 255, 255)
    res = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'android', 'app', 'src', 'main', 'res')
    for density, size in (('mdpi', 24), ('hdpi', 36), ('xhdpi', 48), ('xxhdpi', 72), ('xxxhdpi', 96)):
        d = os.path.join(res, 'drawable-' + density)
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, 'ic_notification.png'), 'wb') as f:
            f.write(render('normal', 0, size, True))


if __name__ == '__main__':
    main()

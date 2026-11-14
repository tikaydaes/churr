#!/bin/sh
# Makes the web icons in internal/web/static/icons from art/churr-sticker.png.
# Run by hand when the art changes; the results are committed.
#
# sips (macOS) does the resizing and padding to a square. sips cannot lay a
# transparent picture on a colour, which the maskable and Apple icons need
# (a phone would otherwise fill the gaps with white), so a few lines of
# Python with nothing but the standard library do that last step.
set -e
cd "$(dirname "$0")/.."
art=art/churr-sticker.png
out=internal/web/static/icons
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"

# make <size> <art size inside it> <name>
make() {
  sips -Z "$2" "$art" --out "$tmp/a.png" >/dev/null
  sips -p "$1" "$1" --padColor 0D1026 "$tmp/a.png" --out "$tmp/b.png" >/dev/null 2>&1
  python3 - "$tmp/b.png" "$out/$3" <<'PY'
import struct, sys, zlib

def read(path):
    data = open(path, 'rb').read()
    assert data[:8] == b'\x89PNG\r\n\x1a\n', 'not a PNG'
    pos, chunks = 8, []
    while pos < len(data):
        n, kind = struct.unpack('>I4s', data[pos:pos + 8])
        chunks.append((kind, data[pos + 8:pos + 8 + n]))
        pos += 12 + n
    w, h, depth, colour = struct.unpack('>IIBB', dict(chunks)[b'IHDR'][:10])
    assert depth == 8 and colour in (2, 6), 'expected 8-bit RGB or RGBA'
    bpp = 4 if colour == 6 else 3
    raw = zlib.decompress(b''.join(body for kind, body in chunks if kind == b'IDAT'))
    stride, rows, prev = w * bpp, [], bytearray(w * bpp)
    for y in range(h):
        f = raw[y * (stride + 1)]
        row = bytearray(raw[y * (stride + 1) + 1:(y + 1) * (stride + 1)])
        for i in range(stride):
            a = row[i - bpp] if i >= bpp else 0
            b = prev[i]
            c = prev[i - bpp] if i >= bpp else 0
            if f == 1: row[i] = (row[i] + a) & 255
            elif f == 2: row[i] = (row[i] + b) & 255
            elif f == 3: row[i] = (row[i] + (a + b) // 2) & 255
            elif f == 4:
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                row[i] = (row[i] + (a if pa <= pb and pa <= pc else b if pb <= pc else c)) & 255
        rows.append(row)
        prev = row
    return w, h, bpp, rows

def write(path, w, h, rows):
    def chunk(kind, body):
        return struct.pack('>I', len(body)) + kind + body + struct.pack('>I', zlib.crc32(kind + body) & 0xffffffff)
    raw = b''.join(b'\x00' + bytes(r) for r in rows)
    open(path, 'wb').write(b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0))
                           + chunk(b'IDAT', zlib.compress(raw, 9)) + chunk(b'IEND', b''))

w, h, bpp, rows = read(sys.argv[1])
navy = (0x0d, 0x10, 0x26)
flat = []
for row in rows:
    out = bytearray(w * 3)
    for x in range(w):
        px = row[x * bpp:x * bpp + bpp]
        alpha = px[3] if bpp == 4 else 255
        for k in range(3):
            out[x * 3 + k] = (px[k] * alpha + navy[k] * (255 - alpha) + 127) // 255
    flat.append(out)
write(sys.argv[2], w, h, flat)
PY
}

make 64 58 favicon.png
make 192 176 icon-192.png
make 512 470 icon-512.png
# A maskable icon may be cropped to a circle: the art stays inside the
# middle four fifths.
make 512 360 maskable-512.png
make 180 156 apple-touch.png
# The sticker by itself, with its transparent edge, for the pages' own
# brand line.
sips -Z 132 "$art" --out "$out/sticker.png" >/dev/null
ls -l "$out"

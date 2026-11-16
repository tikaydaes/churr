#!/usr/bin/env python3
"""The names of Unicode code points, which the server answers
`lookupCodePoints` from (docs/MESSAGE-FORMAT.md section 3.2). Clients
carry no name data.

    curl -O https://www.unicode.org/Public/16.0.0/ucd/UnicodeData.txt
    python3 tools/unicode-gen/gen.py UnicodeData.txt

It checks the file against the pinned version's SHA-256 and writes
internal/unicodename/ucd.txt.gz, which the server embeds: one line a
code point, `delta;category;name` (delta in hex from the line before),
and one line a range the database names by rule or not at all,
`=first;last;category;prefix` (the name is the prefix and the code
point's hex digits; `*hangul` names Hangul syllables by their jamo; an
empty prefix means no name). A control character's name is its Unicode 1
name.
"""
import gzip, hashlib, os, sys

VERSION = '16.0.0'
SHA256 = 'ff58e5823bd095166564a006e47d111130813dcf8bf234ef79fa51a870edb48f'
ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..')
OUT = os.path.join(ROOT, 'internal/unicodename/ucd.txt.gz')

data = open(sys.argv[1], 'rb').read()
if hashlib.sha256(data).hexdigest() != SHA256:
    sys.exit('UnicodeData.txt is not that of Unicode %s' % VERSION)

PREFIX = {'CJK Ideograph': 'CJK UNIFIED IDEOGRAPH-', 'Tangut Ideograph': 'TANGUT IDEOGRAPH-',
          'Hangul Syllable': '*hangul'}
lines, prev, first = [], 0, None
for row in data.decode('utf-8').splitlines():
    f = row.split(';')
    cp, name, cat = int(f[0], 16), f[1], f[2]
    if name.endswith(', First>'):
        first = cp
        continue
    if name.endswith(', Last>'):
        kind = name[1:-len(', Last>')]
        prefix = next((p for k, p in PREFIX.items() if kind.startswith(k)), '')
        lines.append('=%X;%X;%s;%s' % (first, cp, cat, prefix))
        continue
    if name == '<control>':
        name = f[10]
    lines.append('%X;%s;%s' % (cp - prev, cat, name))
    prev = cp
blob = gzip.compress(('\n'.join(lines) + '\n').encode(), compresslevel=9, mtime=0)
with open(OUT, 'wb') as fh:
    fh.write(blob)
print('Unicode %s: %d lines, %d bytes compressed' % (VERSION, len(lines), len(blob)))

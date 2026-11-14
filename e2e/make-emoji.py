#!/usr/bin/env python3
"""Writes internal/web/static/shared/emoji/emoji.json from Unicode's emoji-test.txt.

Run by hand when a new Unicode emoji version is wanted; the result is
committed, and nothing is fetched when the server runs.

    python3 e2e/make-emoji.py path/to/emoji-test.txt

The source is https://unicode.org/Public/emoji/latest/emoji-test.txt, a
Unicode data file under the Unicode License v3 (kept beside the output).
Every fully-qualified emoji is kept except the skin-tone variants, in
Unicode's own groups and order. A short code is the emoji's Unicode name in
lower case with every run of other characters made one underscore, which is
a rule anyone can repeat; a short table of the names people actually type
(`+1`, `tada`, `smile`) is added on top.
"""
import json, re, sys

ALIASES = {
    '+1': '👍', 'thumbsup': '👍', '-1': '👎', 'thumbsdown': '👎', 'smile': '😄', 'smiley': '😃', 'grin': '😁',
    'laughing': '😆', 'joy': '😂', 'rofl': '🤣', 'sweat_smile': '😅', 'wink': '😉', 'blush': '😊', 'heart_eyes': '😍',
    'thinking': '🤔', 'cry': '😢', 'sob': '😭', 'angry': '😠', 'scream': '😱', 'sunglasses': '😎', 'sleeping': '😴',
    'skull': '💀', 'heart': '❤️', 'broken_heart': '💔', 'tada': '🎉', 'fire': '🔥', 'eyes': '👀', 'wave': '👋',
    'pray': '🙏', 'clap': '👏', 'rocket': '🚀', 'white_check_mark': '✅', 'check': '✅', 'x': '❌', 'warning': '⚠️',
    '100': '💯', 'ok_hand': '👌', 'raised_hands': '🙌', 'sparkles': '✨', 'star': '⭐', 'bug': '🐛', 'coffee': '☕',
    'point_up': '👆', 'point_down': '👇', 'point_left': '👈', 'point_right': '👉', 'muscle': '💪', 'shrug': '🤷',
    'facepalm': '🤦', 'party': '🥳', 'bulb': '💡', 'zap': '⚡', 'boom': '💥', 'poop': '💩', 'ship': '🚢',
    'lock': '🔒', 'key': '🔑', 'bell': '🔔', 'pin': '📌', 'memo': '📝', 'calendar': '📅', 'question': '❓',
    'exclamation': '❗', 'moon': '🌙', 'sun': '☀️', 'rainbow': '🌈', 'cat': '🐱', 'dog': '🐶', 'owl': '🦉', 'bird': '🐦',
}

def code(name):
    return re.sub(r'[^a-z0-9]+', '_', name.lower()).strip('_')

groups, group = [], None
for line in open(sys.argv[1], encoding='utf-8'):
    if line.startswith('# group:'):
        name = line.split(':', 1)[1].strip()
        group = None if name == 'Component' else {'name': name, 'emoji': []}
        if group: groups.append(group)
        continue
    m = re.match(r'^([0-9A-F ]+);\s*fully-qualified\s*#\s*(\S+)\s+E[\d.]+\s+(.*)$', line)
    if not m or group is None:
        continue
    points, char, name = m.groups()
    if 'skin tone' in name:
        continue
    group['emoji'].append([char, code(name)])

out = {'source': 'Unicode emoji-test.txt', 'groups': groups, 'aliases': ALIASES}
dest = 'internal/web/static/shared/emoji/emoji.json'
json.dump(out, open(dest, 'w', encoding='utf-8'), ensure_ascii=False, separators=(',', ':'))
n = sum(len(g['emoji']) for g in groups)
print(f'{dest}: {n} emoji in {len(groups)} groups, {len(ALIASES)} aliases')

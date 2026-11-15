#!/usr/bin/env python3
"""The decks /draw draws from and the tarot of /tarot (rulings R67, R68): one
registry, written out for everyone who needs it.

    python3 tools/decks-gen/gen.py

The registry is ported from the owner's game library (gamelib: cards.go,
game_cards.go, tarot_reading.go), with its mistakes put right (see the
notes in DECKS). It writes:

- testdata/commands/decks.json and tarot.json, the shared tables the server
  and both clients are tested against;
- internal/model/decks_gen.go, the server's registry, with the English
  names the fallback text uses;
- internal/web/static/shared/message/cardnames.js, the web chat's words;
- app/lib/ui/message/card_names.dart and the card and tarot keys of
  app/lib/l10n/app_en.arb, the app's words through l10n.

A card is `<suit>-<rank>` or the code of a card outside the suits (a joker,
a major arcanum, a mahjong honour). A deck may hold a card more than once.
tarot_meanings.json holds each tarot card's meanings, from the library,
with its typos and a few wordings mended.
"""
import json, os, re, subprocess

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, '..', '..')

# code: (English name, Unicode symbol or '')
SUITS = {
    'spades': ('spades', '♠'), 'hearts': ('hearts', '♥'), 'diamonds': ('diamonds', '♦'), 'clubs': ('clubs', '♣'),
    'cups': ('cups', ''), 'pentacles': ('pentacles', ''), 'wands': ('wands', ''), 'swords': ('swords', ''),
    'coins': ('coins', ''), 'batons': ('clubs', ''),
    'acorns': ('acorns', ''), 'leaves': ('leaves', ''), 'gheart': ('hearts', ''), 'bells': ('bells', ''),
    'shields': ('shields', ''), 'roses': ('roses', ''),
    'dots': ('dots', ''), 'bamboo': ('bamboo', ''), 'characters': ('characters', ''),
    'pine': ('pine', ''), 'plum': ('plum blossom', ''), 'cherry': ('cherry blossom', ''), 'wisteria': ('wisteria', ''),
    'iris': ('iris', ''), 'peony': ('peony', ''), 'clover': ('bush clover', ''), 'pampas': ('pampas grass', ''),
    'chrysanthemum': ('chrysanthemum', ''), 'maple': ('maple', ''), 'willow': ('willow', ''), 'paulownia': ('paulownia', ''),
}
WORDS = ['one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten']
# code: (English name, short form)
RANKS = {str(n): (WORDS[n - 1], str(n)) for n in range(1, 11)}
RANKS.update({
    'jack': ('jack', 'J'), 'queen': ('queen', 'Q'), 'king': ('king', 'K'), 'ace': ('ace', 'A'),
    'page': ('page', 'P'), 'knight': ('knight', 'Kn'),
    'sota': ('sota', 'S'), 'caballo': ('caballo', 'C'), 'rey': ('rey', 'R'),
    'fante': ('fante', 'F'), 'cavallo': ('cavallo', 'C'), 're': ('re', 'R'),
    'unter': ('unter', 'U'), 'ober': ('ober', 'O'), 'banner': ('banner', 'B'), 'under': ('under', 'U'),
    'bright': ('bright', 'B'), 'animal': ('animal', 'A'), 'ribbon': ('ribbon', 'R'),
    'chaff1': ('chaff', 'C'), 'chaff2': ('chaff', 'C'), 'chaff3': ('chaff', 'C'),
})
MAJOR = ['The Fool', 'The Magician', 'The High Priestess', 'The Empress', 'The Emperor', 'The Hierophant',
         'The Lovers', 'The Chariot', 'Strength', 'The Hermit', 'Wheel of Fortune', 'Justice', 'The Hanged Man',
         'Death', 'Temperance', 'The Devil', 'The Tower', 'The Star', 'The Moon', 'The Sun', 'Judgement', 'The World']
# code: (English name, short form). The library gave Joker:Red the black
# joker's character; each has its own here.
EXTRAS = {'joker-red': ('red joker', 'JK'), 'joker-black': ('black joker', 'JK'), 'joker-white': ('white joker', 'JK')}
for i, n in enumerate(MAJOR):
    EXTRAS['major-%d' % i] = (n, str(i))
for w in ['east', 'south', 'west', 'north']:
    EXTRAS['wind-' + w] = (w + ' wind', w[0].upper())
for d in ['red', 'green', 'white']:
    EXTRAS['dragon-' + d] = (d + ' dragon', d[0].upper() + 'D')
for f in ['plum', 'orchid', 'chrysanthemum', 'bamboo']:
    EXTRAS['flower-' + f] = (f + ' flower', 'F')
for s in ['spring', 'summer', 'autumn', 'winter']:
    EXTRAS['season-' + s] = (s, 'S')

# Characters a card has in Unicode, where there is one, drawn beside its
# words (never in place of them).
GLYPHS = {}
for suit, base in [('spades', 0x1F0A0), ('hearts', 0x1F0B0), ('diamonds', 0x1F0C0), ('clubs', 0x1F0D0)]:
    for r, off in [('ace', 1)] + [(str(n), n) for n in range(2, 11)] + [('jack', 11), ('queen', 13), ('king', 14)]:
        GLYPHS[suit + '-' + r] = chr(base + off)
GLYPHS.update({'joker-red': chr(0x1F0BF), 'joker-black': chr(0x1F0CF), 'joker-white': chr(0x1F0DF)})
for i, w in enumerate(['east', 'south', 'west', 'north']):
    GLYPHS['wind-' + w] = chr(0x1F000 + i)
for i, d in enumerate(['red', 'green', 'white']):
    GLYPHS['dragon-' + d] = chr(0x1F004 + i)
for suit, base in [('characters', 0x1F007), ('bamboo', 0x1F010), ('dots', 0x1F019)]:
    for n in range(1, 10):
        GLYPHS['%s-%d' % (suit, n)] = chr(base + n - 1)
for i, f in enumerate(['plum', 'orchid', 'bamboo', 'chrysanthemum']):
    GLYPHS['flower-' + f] = chr(0x1F022 + i)
for i, s in enumerate(['spring', 'summer', 'autumn', 'winter']):
    GLYPHS['season-' + s] = chr(0x1F026 + i)

FRENCH = ['spades', 'hearts', 'diamonds', 'clubs']
R52 = ['ace'] + [str(n) for n in range(2, 11)] + ['jack', 'queen', 'king']
UP_FROM = lambda first: [r for r in [str(n) for n in range(2, 11)] + ['jack', 'queen', 'king', 'ace'] if r not in [str(n) for n in range(2, first)]]
GERMAN = ['acorns', 'leaves', 'gheart', 'bells']

# The months of hanafuda and their four cards. The library gave every month
# one light, animal, ribbon and chaff card; the real deck does not.
HANAFUDA = [
    ('pine', ['bright', 'ribbon', 'chaff1', 'chaff2']), ('plum', ['animal', 'ribbon', 'chaff1', 'chaff2']),
    ('cherry', ['bright', 'ribbon', 'chaff1', 'chaff2']), ('wisteria', ['animal', 'ribbon', 'chaff1', 'chaff2']),
    ('iris', ['animal', 'ribbon', 'chaff1', 'chaff2']), ('peony', ['animal', 'ribbon', 'chaff1', 'chaff2']),
    ('clover', ['animal', 'ribbon', 'chaff1', 'chaff2']), ('pampas', ['bright', 'animal', 'chaff1', 'chaff2']),
    ('chrysanthemum', ['animal', 'ribbon', 'chaff1', 'chaff2']), ('maple', ['animal', 'ribbon', 'chaff1', 'chaff2']),
    ('willow', ['bright', 'animal', 'ribbon', 'chaff1']), ('paulownia', ['bright', 'chaff1', 'chaff2', 'chaff3']),
]

# name, aliases, suits, ranks, copies, extras [(code, count)], orientation, naming.
# naming: `of` "queen of cups"; `suit_rank` "pine bright"; `suit_number`
# "dots 5" (the rank's short form). Notes on the library's decks:
# - German and Austrian packs have unter and ober, not jack and queen, and the
#   Swiss pack banner, under and ober.
# - The library's "German 36 (Skat)" is left out: Skat is played with 32.
#   Its "28: cribbage" is left out too: cribbage uses the full 52.
# - Uno, Phase 10, Skip-Bo and Rook are left out (ruling R68).
DECKS = [
    ('standard', ['52', 'french'], FRENCH, R52, 1, [], False, 'of'),
    ('jokers', ['54'], FRENCH, R52, 1, [('joker-red', 1), ('joker-black', 1)], False, 'of'),
    ('jokers3', ['55'], FRENCH, R52, 1, [('joker-red', 1), ('joker-black', 1), ('joker-white', 1)], False, 'of'),
    ('piquet', ['32'], FRENCH, UP_FROM(7), 1, [], False, 'of'),
    ('durak', ['36'], FRENCH, UP_FROM(6), 1, [], False, 'of'),
    ('euchre', ['24'], FRENCH, UP_FROM(9), 1, [], False, 'of'),
    ('tarot', ['78'], ['wands', 'cups', 'swords', 'pentacles'], ['ace'] + [str(n) for n in range(2, 11)] + ['page', 'knight', 'queen', 'king'], 1,
     [('major-%d' % i, 1) for i in range(22)], True, 'of'),
    ('spanish', [], ['coins', 'cups', 'swords', 'batons'], ['ace'] + [str(n) for n in range(2, 8)] + ['sota', 'caballo', 'rey'], 1, [], False, 'of'),
    ('italian', [], ['coins', 'cups', 'swords', 'batons'], ['ace'] + [str(n) for n in range(2, 8)] + ['fante', 'cavallo', 're'], 1, [], False, 'of'),
    ('german', [], GERMAN, ['7', '8', '9', '10', 'unter', 'ober', 'king', 'ace'], 1, [], False, 'of'),
    ('austrian', [], GERMAN, ['7', '8', '9', '10', 'unter', 'ober', 'king', 'ace'], 1, [], False, 'of'),
    ('swiss', [], ['shields', 'roses', 'acorns', 'bells'], ['6', '7', '8', '9', 'banner', 'under', 'ober', 'king', 'ace'], 1, [], False, 'of'),
    ('pinochle', ['48'], FRENCH, UP_FROM(9), 2, [], False, 'of'),
    ('canasta', ['108'], FRENCH, R52, 2, [('joker-red', 2), ('joker-black', 2)], False, 'of'),
    ('hanafuda', [], [], [], 1, [('%s-%s' % (m, c), 1) for m, cs in HANAFUDA for c in cs], False, 'suit_rank'),
    ('mahjong', ['mahjongg', '144'], ['dots', 'bamboo', 'characters'], [str(n) for n in range(1, 10)], 4,
     [('wind-' + w, 4) for w in ['east', 'south', 'west', 'north']] + [('dragon-' + d, 4) for d in ['red', 'green', 'white']] +
     [('flower-' + f, 1) for f in ['plum', 'orchid', 'chrysanthemum', 'bamboo']] + [('season-' + s, 1) for s in ['spring', 'summer', 'autumn', 'winter']],
     False, 'suit_number'),
    ('shoe2', [], FRENCH, R52, 2, [], False, 'of'),
    ('shoe4', [], FRENCH, R52, 4, [], False, 'of'),
    ('shoe6', [], FRENCH, R52, 6, [], False, 'of'),
    ('shoe8', [], FRENCH, R52, 8, [], False, 'of'),
]

# The spreads of /tarot, from the library. code: (English name, aliases,
# positions, description).
POSITIONS = {
    'past': 'Past', 'present': 'Present', 'future': 'Future', 'challenge': 'Challenge', 'above': 'Above',
    'below': 'Below', 'advice': 'Advice', 'external': 'External influences', 'hopes': 'Hopes and fears',
    'outcome': 'Outcome', 'hidden': 'Hidden influences', 'obstacles': 'Obstacles', 'you': 'You',
    'partner': 'Partner', 'foundation': 'Foundation of the relationship', 'recent': 'Recent past',
    'dynamics': 'Current dynamics', 'near': 'Near future',
}
SPREADS = [
    ('three', 'Three cards', ['3', 'three-card'], ['past', 'present', 'future'],
     'The past, the present and where things are heading.'),
    ('celtic', 'Celtic cross', ['celtic-cross', 'cross', '10'],
     ['present', 'challenge', 'past', 'future', 'above', 'below', 'advice', 'external', 'hopes', 'outcome'],
     'Ten cards for a long look at a tangled question.'),
    ('horseshoe', 'Horseshoe', ['7'], ['past', 'present', 'hidden', 'obstacles', 'external', 'advice', 'outcome'],
     'Seven cards on how a situation will unfold.'),
    ('relationship', 'Relationship', ['love'], ['you', 'partner', 'foundation', 'recent', 'dynamics', 'near', 'advice'],
     'Seven cards on what passes between two people.'),
]
# The reading's overall energy (the library's Interpret), worded once.
SUMMARY = {
    'major_high': 'Many major arcana: big forces and turning points are at work.',
    'major_low': 'Mostly minor arcana: this is about everyday matters, not upheaval.',
    'major_balanced': 'A mix of major and minor arcana: the everyday ties into the larger path.',
    'reversed_many': 'Many reversed cards: obstacles, delays or inner doubts to deal with first.',
    'reversed_few': 'Nearly all upright: the way looks clear.',
    'suit_cups': 'Cups stand out: feelings, relationships and intuition.',
    'suit_pentacles': 'Pentacles stand out: money, work and practical things.',
    'suit_wands': 'Wands stand out: drive, creativity and action.',
    'suit_swords': 'Swords stand out: thought, decisions and conflict.',
}
FOR_FUN = 'For fun: the cards were drawn by the server at random.'
MEANINGS = json.load(open(os.path.join(HERE, 'tarot_meanings.json')))


def cards(d):
    _, _, suits, ranks, copies, extras, _, _ = d
    out = []
    for _ in range(copies):
        for s in suits:
            for r in ranks:
                out.append(s + '-' + r)
    for code, n in extras:
        out += [code] * n
    return out


def english(deck, code):
    if code in EXTRAS:
        return EXTRAS[code][0]
    s, r = code.split('-', 1)
    if deck[7] == 'suit_rank':
        return '%s %s' % (SUITS[s][0], RANKS[r][0])
    if deck[7] == 'suit_number':
        return '%s %s' % (SUITS[s][0], RANKS[r][1])
    return '%s of %s' % (RANKS[r][0], SUITS[s][0])


all_codes = {c for d in DECKS for c in cards(d)}
used_suits = sorted({c.split('-', 1)[0] for c in all_codes if c not in EXTRAS})
used_ranks = sorted({c.split('-', 1)[1] for c in all_codes if c not in EXTRAS}, key=lambda r: (not r.isdigit(), int(r) if r.isdigit() else 0, r))
used_extras = [e for e in EXTRAS if e in all_codes]
assert set(MEANINGS) == set(cards(DECKS[6])), 'every tarot card has its meanings'


def write(path, text):
    with open(os.path.join(ROOT, path), 'w') as f:
        f.write(text)


deck_rows = [{'name': d[0], 'aliases': d[1], 'size': len(cards(d)), 'orientation': d[6], 'naming': d[7],
              'cards': cards(d)} for d in DECKS]
write('testdata/commands/decks.json', json.dumps({
    'description': 'The decks /draw draws from (docs/FEATURES.md section 12.4, rulings R67 and R68), written by '
                   'tools/decks-gen/gen.py. A deck is named by its name or an alias; cards are listed in the '
                   "server's order, and a card may be in a deck more than once. A card is `<suit>-<rank>` or a "
                   'card outside the suits (extras). orientation: each card drawn is also upright or reversed. '
                   'naming: `of` reads "queen of cups", `suit_rank` "pine bright", `suit_number` "dots 5" (the '
                   "rank's short form). glyphs: the Unicode character of a card where it has one, drawn beside its "
                   'words. english: the English name of every card, which the server writes into the fallback '
                   'text; clients take their words from their own wording files.',
    'decks': deck_rows,
    'suits': {s: {'name': SUITS[s][0], 'symbol': SUITS[s][1]} for s in used_suits},
    'ranks': {r: {'name': RANKS[r][0], 'short': RANKS[r][1]} for r in used_ranks},
    'extras': {e: {'name': EXTRAS[e][0], 'short': EXTRAS[e][1]} for e in used_extras},
    'glyphs': GLYPHS,
    'english': {d[0]: {c: english(d, c) for c in dict.fromkeys(cards(d))} for d in DECKS},
}, ensure_ascii=False, indent=1) + '\n')

# The overall energy of a few readings, so each client words the same.
def summary(cs, rev):
    n = len(cs)
    major = sum(1 for c in cs if c.startswith('major-'))
    out = ['major_high' if major * 100 > 60 * n else 'major_low' if major * 100 < 30 * n else 'major_balanced']
    r = sum(1 for x in rev if x)
    if r * 100 > 60 * n:
        out.append('reversed_many')
    elif r * 100 < 20 * n:
        out.append('reversed_few')
    for s in ['cups', 'pentacles', 'wands', 'swords']:
        k = sum(1 for c in cs if c.startswith(s + '-'))
        # The library counted a suit as standing out above a quarter of the
        # cards, by whole-number division: in three cards, one card did.
        # Two at least are needed here.
        if k >= 2 and k * 4 > n:
            out.append('suit_' + s)
    return out

SUMMARY_CASES = [
    (['major-0', 'major-13', 'cups-2'], [False, False, True]),
    (['cups-ace', 'cups-3', 'swords-9'], [True, True, True]),
    (['wands-2', 'pentacles-king', 'major-21'], [False, False, False]),
    (['major-1', 'cups-5', 'cups-6', 'cups-7', 'swords-2', 'swords-3', 'wands-4', 'pentacles-5', 'major-10', 'major-19'],
     [True, False, False, False, True, False, False, False, True, False]),
]
write('testdata/commands/tarot.json', json.dumps({
    'description': 'The tarot of /tarot (docs/FEATURES.md section 12.4, rulings R67 and R68), written by '
                   'tools/decks-gen/gen.py from the owner\'s game library. spreads: each spread\'s positions in '
                   'order. meanings: each card upright and reversed. summary: the overall energy of a reading, '
                   'by key, with the rule in gen.py (summary_cases are readings and the keys they give). '
                   'Clients word everything from their own wording files.',
    'spreads': [{'name': s[0], 'title': s[1], 'aliases': s[2], 'positions': s[3], 'description': s[4]} for s in SPREADS],
    'positions': POSITIONS, 'meanings': MEANINGS, 'summary': SUMMARY, 'for_fun': FOR_FUN,
    'summary_cases': [{'cards': c, 'reversed': r, 'keys': summary(c, r)} for c, r in SUMMARY_CASES],
}, ensure_ascii=False, indent=1) + '\n')

# --- Go ---
def gostr(s):
    return json.dumps(s, ensure_ascii=False)

go = ['// Code generated by tools/decks-gen/gen.py. DO NOT EDIT.', '', 'package model', '',
      '// decks is the registry of the decks /draw draws from (rulings R67 and R68,',
      '// testdata/commands/decks.json).', 'var decks = []deck{']
for d in DECKS:
    extras = ', '.join('{%s, %d}' % (gostr(c), n) for c, n in d[5])
    go.append('\t{Name: %s, Aliases: []string{%s}, Suits: []string{%s}, Ranks: []string{%s}, Copies: %d, Extras: []deckExtra{%s}, Orientation: %s, Naming: %s},' % (
        gostr(d[0]), ', '.join(map(gostr, d[1])), ', '.join(map(gostr, d[2])), ', '.join(map(gostr, d[3])), d[4], extras,
        'true' if d[6] else 'false', gostr(d[7])))
go += ['}', '', '// The English names of suits, ranks and the cards outside the suits, for the', '// fallback text.', 'var (']
go.append('\tsuitNames = map[string]string{' + ', '.join('%s: %s' % (gostr(s), gostr(SUITS[s][0])) for s in used_suits) + '}')
go.append('\trankNames = map[string]string{' + ', '.join('%s: %s' % (gostr(r), gostr(RANKS[r][0])) for r in used_ranks) + '}')
go.append('\trankShort = map[string]string{' + ', '.join('%s: %s' % (gostr(r), gostr(RANKS[r][1])) for r in used_ranks) + '}')
go.append('\textraNames = map[string]string{' + ', '.join('%s: %s' % (gostr(e), gostr(EXTRAS[e][0])) for e in used_extras) + '}')
go.append(')')
go += ['', '// tarotSpreads are the spreads of /tarot (testdata/commands/tarot.json).', 'var tarotSpreads = []tarotSpread{']
for s in SPREADS:
    go.append('\t{Name: %s, Title: %s, Aliases: []string{%s}, Positions: []string{%s}},' % (
        gostr(s[0]), gostr(s[1]), ', '.join(map(gostr, s[2])), ', '.join(map(gostr, s[3]))))
go += ['}', '', '// tarotPositions are the English names of the positions.',
       'var tarotPositions = map[string]string{' + ', '.join('%s: %s' % (gostr(k), gostr(v)) for k, v in POSITIONS.items()) + '}']
write('internal/model/decks_gen.go', '\n'.join(go) + '\n')
subprocess.run(['gofmt', '-w', os.path.join(ROOT, 'internal/model/decks_gen.go')], check=True)

# --- l10n keys ---
def camel(prefix, code):
    parts = re.split(r'[^a-zA-Z0-9]+', code)
    return prefix + ''.join(p[:1].upper() + p[1:] for p in parts)

keys = {}
for s in used_suits:
    keys[camel('cardSuit', s)] = SUITS[s][0]
for r in used_ranks:
    keys[camel('cardRank', r)] = RANKS[r][0]
for e in used_extras:
    keys[camel('cardExtra', e)] = EXTRAS[e][0]
for code, m in MEANINGS.items():
    keys[camel('tarotUp', code)] = m['upright']
    keys[camel('tarotRev', code)] = m['reversed']
for k, v in POSITIONS.items():
    keys[camel('tarotPos', k)] = v
for s in SPREADS:
    keys[camel('tarotSpread', s[0])] = s[1]
for k, v in SUMMARY.items():
    keys[camel('tarotSum', k)] = v
keys['tarotForFun'] = FOR_FUN

# --- web ---
js = ['// Generated by tools/decks-gen/gen.py from its registry. Do not edit.', '//',
      "// The words for the cards /draw draws and the tarot of /tarot (rulings R67",
      "// and R68): the web chat's wording, kept in one place like the rest of its",
      '// strings.', '',
      'export const DECKS = ' + json.dumps({d[0]: {'aliases': d[1], 'orientation': d[6], 'naming': d[7], 'cards': cards(d)} for d in DECKS}, ensure_ascii=False) + ';', '',
      'export const SUITS = ' + json.dumps({s: {'name': SUITS[s][0], 'symbol': SUITS[s][1]} for s in used_suits}, ensure_ascii=False) + ';',
      'export const RANKS = ' + json.dumps({r: {'name': RANKS[r][0], 'short': RANKS[r][1]} for r in used_ranks}, ensure_ascii=False) + ';',
      'export const EXTRAS = ' + json.dumps({e: {'name': EXTRAS[e][0], 'short': EXTRAS[e][1]} for e in used_extras}, ensure_ascii=False) + ';',
      'export const GLYPHS = ' + json.dumps(GLYPHS, ensure_ascii=False) + ';', '',
      'export const SPREADS = ' + json.dumps({s[0]: {'title': s[1], 'aliases': s[2], 'positions': s[3], 'description': s[4]} for s in SPREADS}, ensure_ascii=False) + ';',
      'export const POSITIONS = ' + json.dumps(POSITIONS, ensure_ascii=False) + ';',
      'export const MEANINGS = ' + json.dumps(MEANINGS, ensure_ascii=False) + ';',
      'export const SUMMARY = ' + json.dumps(SUMMARY, ensure_ascii=False) + ';',
      'export const FOR_FUN = ' + json.dumps(FOR_FUN, ensure_ascii=False) + ';', '']
write('internal/web/static/shared/message/cardnames.js', '\n'.join(js))

# --- app ---
def dstr(s):
    return "'" + s.replace('\\', '\\\\').replace("'", "\\'").replace('$', '\\$') + "'"

core = ['// Generated by tools/decks-gen/gen.py from its registry. Do not edit.', '',
        '/// The decks /draw draws from (rulings R67, R68): aliases, whether a card',
        '/// is also upright or reversed, how a card is named (`of`: "queen of',
        '/// cups"; `suit_rank`: "pine bright"; `suit_number`: "dots 5"), and the',
        "/// cards in the server's order.",
        'const cardDecks = <String, ({List<String> aliases, bool orientation, String naming, List<String> cards})>{']
for d in DECKS:
    core.append('  %s: (aliases: [%s], orientation: %s, naming: %s, cards: [%s]),' % (
        dstr(d[0]), ', '.join(map(dstr, d[1])), 'true' if d[6] else 'false', dstr(d[7]), ', '.join(map(dstr, cards(d)))))
core += ['};', '', '/// The Unicode symbol of a suit, where it has one.',
         'const cardSuitSymbols = <String, String>{' + ', '.join('%s: %s' % (dstr(s), dstr(SUITS[s][1])) for s in used_suits if SUITS[s][1]) + '};',
         '', '/// The short form of a rank or a card outside the suits, for the reel.',
         'const cardShort = <String, String>{' + ', '.join('%s: %s' % (dstr(k), dstr(v[1])) for k, v in list({**{r: RANKS[r] for r in used_ranks}, **{e: EXTRAS[e] for e in used_extras}}.items())) + '};',
         '', '/// The Unicode character of a card, where it has one.',
         'const cardGlyphs = <String, String>{' + ', '.join('%s: %s' % (dstr(k), dstr(v)) for k, v in GLYPHS.items()) + '};',
         '', '/// The cards outside the suits.',
         'const cardExtras = <String>{' + ', '.join(dstr(e) for e in used_extras) + '};',
         '', '/// The spreads of /tarot: positions in order, and aliases.',
         'const tarotSpreads = <String, ({List<String> aliases, List<String> positions})>{']
for sp in SPREADS:
    core.append('  %s: (aliases: [%s], positions: [%s]),' % (dstr(sp[0]), ', '.join(map(dstr, sp[2])), ', '.join(map(dstr, sp[3]))))
core.append('};')
write('app/lib/core/commands/decks.dart', '\n'.join(core) + '\n')

dart = ['// Generated by tools/decks-gen/gen.py from its registry. Do not edit.', '',
        "import '../../l10n/app_localizations.dart';", '',
        '// The words for cards and the tarot, through l10n. The decks themselves',
        '// are lib/core/commands/decks.dart.']
def switch(fn, doc, prefix, codes, fallback):
    dart.extend(['', '/// ' + doc, 'String%s %s(L10n t, String code) => switch (code) {' % ('?' if fallback == 'null' else '', fn)])
    for c in codes:
        dart.append('  %s => t.%s,' % (dstr(c), camel(prefix, c)))
    dart.extend(['  _ => %s,' % fallback, '};'])

switch('cardSuitName', 'The word for a suit.', 'cardSuit', used_suits, 'code')
switch('cardRankName', 'The word for a rank.', 'cardRank', used_ranks, 'code')
switch('cardExtraName', 'The name of a card outside the suits, or null.', 'cardExtra', used_extras, 'null')
switch('tarotUpright', 'A tarot card\'s meaning upright.', 'tarotUp', list(MEANINGS), "''")
switch('tarotReversed', 'A tarot card\'s meaning reversed.', 'tarotRev', list(MEANINGS), "''")
switch('tarotPosition', 'The name of a position in a spread.', 'tarotPos', list(POSITIONS), 'code')
switch('tarotSpreadName', 'The name of a spread.', 'tarotSpread', [s[0] for s in SPREADS], 'code')
switch('tarotSummary', 'A sentence of a reading\'s overall energy.', 'tarotSum', list(SUMMARY), "''")
write('app/lib/ui/message/card_names.dart', '\n'.join(dart) + '\n')

# The app's wording file: these keys are added or brought up to date and
# nothing else in it is touched. Keys this script wrote before and no longer
# writes are taken out.
arb_path = os.path.join(ROOT, 'app/lib/l10n/app_en.arb')
with open(arb_path) as f:
    arb = json.load(f)
for k in [k for k in arb if re.match(r'^(cardSuit|cardRank|cardExtra|tarotUp|tarotRev|tarotPos|tarotSpread|tarotSum)[A-Z0-9]', k) and k not in keys]:
    del arb[k]
arb.update(keys)
with open(arb_path, 'w') as f:
    f.write(json.dumps(arb, ensure_ascii=False, indent=2) + '\n')
print('%d decks, %d spreads, %d words' % (len(DECKS), len(SPREADS), len(keys)))

// The built-in chance kinds under derive 1 (docs/FAIR.md section 5a), as the
// server's internal/fair (kinds.go) and the web chat's shared/fair/kinds.js
// write them, all three held to testdata/fair/kinds.json: each kind a fixed
// composition of the primitives, read from what its data says was asked, so
// the outcome its data shows is compared with what the primitives give from
// the value's share of the draw. Plain Dart.

import '../commands/decks.dart';
import 'derive.dart';

List<Json> _cards(int size, int count, bool oriented) => [
  {'op': 'pick', 'n': size, 'k': count},
  if (oriented) {'op': 'ints', 'min': 0, 'max': 1, 'count': count},
];

List<String> _deckOf(String kind, Json d) =>
    cardDecks[kind == 'tarot_reading' ? 'tarot' : d['deck']]!.cards;

/// The primitives a value of a built-in chance kind asks of its stream, in
/// order, from its checked data, or null for a kind with no composition.
List<Json>? kindAsks(String kind, Json d) {
  switch (kind) {
    case 'coin_flip':
      return [
        {'op': 'yesno', 'num': 1, 'den': 2},
      ];
    case 'pick':
      return [
        {'op': 'pick', 'n': (d['options'] as List).length, 'k': 1},
      ];
    case 'shuffle':
      return [
        {'op': 'shuffle', 'n': (d['options'] as List).length},
      ];
    case 'card_draw':
      final deck = cardDecks[d['deck']]!;
      return _cards(
        deck.cards.length,
        (d['cards'] as List).length,
        deck.orientation,
      );
    case 'tarot_reading':
      return _cards(
        cardDecks['tarot']!.cards.length,
        (d['cards'] as List).length,
        true,
      );
    case 'dice_roll':
      return [
        {
          'op': 'dice',
          'rows': [
            for (final r in d['rows'] as List)
              {
                'dice': (r as Map)['dice'],
                'terms': [
                  for (final t in r['terms'] as List)
                    if (((t as Map)['count'] ?? 0) != 0)
                      {
                        'sign': t['sign'],
                        'count': t['count'],
                        'sides': t['sides'],
                        'keep': ?t['keep'],
                        'keep_n': ?t['keep_n'],
                      }
                    else
                      {'sign': t['sign'], 'value': t['value']},
                ],
              },
          ],
        },
      ];
  }
  return null;
}

bool _sameCards(List<String> deck, List picked, List shown) {
  if (picked.length != shown.length) return false;
  for (var i = 0; i < picked.length; i++) {
    final p = picked[i] as int;
    if (p < 0 || p >= deck.length || deck[p] != shown[i]) return false;
  }
  return true;
}

String _list(Object? v) => v is List ? v.join(',') : '';

/// Whether a value of a built-in chance kind is what derive 1 gives from
/// object [i] of a draw whose output is [beta]: true, false, or null for a
/// kind with no composition. [d] is the checked data.
bool? rederive(String kind, Json d, List<int> beta, int i) {
  if (kind == 'dice_roll') {
    return sameRoll(FairStream(beta, i), d['rows'] as List);
  }
  final asks = kindAsks(kind, d);
  if (asks == null) return null;
  final s = FairStream(beta, i);
  final rs = <Json>[];
  try {
    for (final p in asks) {
      rs.add(drawPrimitive(s, p));
    }
  } on PrimitiveError {
    return false;
  }
  switch (kind) {
    case 'coin_flip':
      return rs[0]['yes'] == (d['outcome'] == 'heads');
    case 'pick':
      return (rs[0]['picked'] as List)[0] == d['picked'];
    case 'shuffle':
      return _list(rs[0]['order']) == _list(d['order']);
    case 'card_draw':
    case 'tarot_reading':
      if (!_sameCards(
        _deckOf(kind, d),
        rs[0]['picked'] as List,
        d['cards'] as List,
      )) {
        return false;
      }
      if (rs.length < 2) return d['reversed'] == null;
      return _list((rs[1]['values'] as List).map((v) => v == 1).toList()) ==
          _list(d['reversed']);
  }
  return null;
}

/// The outcome a value's data claims, as the results of its primitives (the
/// Validate result view's export, docs/FAIR.md section 11), as the web
/// chat's kindClaims: a card is claimed by what it is, so the positions the
/// draw gives are taken when they hold the cards shown, and otherwise the
/// first free position of each card.
List<Json>? kindClaims(String kind, Json d, List<int> beta, int i) {
  switch (kind) {
    case 'coin_flip':
      return [
        {'yes': d['outcome'] == 'heads'},
      ];
    case 'pick':
      return [
        {
          'picked': [d['picked']],
        },
      ];
    case 'shuffle':
      return [
        {'order': d['order']},
      ];
    case 'dice_roll':
      return [
        {'rows': d['rows']},
      ];
    case 'card_draw':
    case 'tarot_reading':
      final deck = _deckOf(kind, d);
      final asks = kindAsks(kind, d)!;
      List? picked;
      try {
        picked = drawPrimitive(FairStream(beta, i), asks[0])['picked'] as List;
      } on PrimitiveError {
        picked = null;
      }
      final shown = d['cards'] as List;
      if (picked == null || !_sameCards(deck, picked, shown)) {
        final used = <int>{};
        picked = [
          for (final c in shown)
            () {
              var p = -1;
              for (var j = 0; j < deck.length; j++) {
                if (deck[j] == c && !used.contains(j)) {
                  p = j;
                  break;
                }
              }
              used.add(p);
              return p;
            }(),
        ];
      }
      return [
        {'picked': picked},
        if (asks.length > 1)
          {
            'values': [
              for (final r in (d['reversed'] as List? ?? const []))
                r == true ? 1 : 0,
            ],
          },
      ];
  }
  return null;
}

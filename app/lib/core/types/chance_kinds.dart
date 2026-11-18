// The data of the chance kinds besides a roll (docs/FAIR.md section 5a; the
// schemas CoinFlip, Pick, Shuffle, CardDraw and TarotReading of
// docs/openapi.yaml): a coin flipped (/flip), one option picked (/pick), the
// options shuffled (/shuffle), cards drawn (/draw, /mahjongg) and a tarot
// reading (/tarot). The checks are the server's (internal/model,
// kinds_chance.go) and the web chat's (shared/types/chance/data.js), and
// testdata/types/<kind>.json holds all three to the same answers. Each gives
// a copy of what is drawn, or null, which makes the value fall back a step.
// No Flutter.

import '../commands/decks.dart';
import '../models/json.dart';

/// The limits of a pick's or shuffle's options (model.MaxChanceOption).
const minChanceOptions = 2, maxChanceOptions = 20, maxChanceOption = 200;

List<String>? _options(Object? v) {
  if (v is! List ||
      v.length < minChanceOptions ||
      v.length > maxChanceOptions) {
    return null;
  }
  final out = <String>[];
  for (final o in v) {
    if (o is! String ||
        o.isEmpty ||
        o.runes.length > maxChanceOption ||
        o.contains(RegExp(r'[\r\n]'))) {
      return null;
    }
    out.add(o);
  }
  return out;
}

int? _whole(Object? v) {
  if (v is int) return v;
  if (v is double && v.isFinite && v == v.roundToDouble()) return v.toInt();
  return null;
}

List<bool>? _bools(Object? v, int n) {
  if (v is! List || v.length != n || v.any((b) => b is! bool)) return null;
  return v.cast<bool>().toList();
}

/// Whether [cards] could have been drawn from [deck] without putting any
/// back: each at most as often as the deck holds it.
bool _cardsFrom(List<String> deck, Object? cards) {
  if (cards is! List || cards.isEmpty || cards.length > deck.length) {
    return false;
  }
  final left = <String, int>{};
  for (final x in deck) {
    left[x] = (left[x] ?? 0) + 1;
  }
  for (final x in cards) {
    if (x is! String || (left[x] ?? 0) == 0) return false;
    left[x] = left[x]! - 1;
  }
  return true;
}

Json? coinFlipData(Object? d) {
  if (d is! Map) return null;
  final o = d['outcome'];
  return o == 'heads' || o == 'tails' ? {'outcome': o} : null;
}

Json? pickData(Object? d) {
  if (d is! Map) return null;
  final opts = _options(d['options']);
  final p = _whole(d['picked']);
  if (opts == null || p == null || p < 0 || p >= opts.length) return null;
  return {'options': opts, 'picked': p};
}

Json? shuffleData(Object? d) {
  if (d is! Map) return null;
  final opts = _options(d['options']);
  final order = d['order'];
  if (opts == null || order is! List || order.length != opts.length) {
    return null;
  }
  final seen = <int>{};
  final out = <int>[];
  for (final x in order) {
    final i = _whole(x);
    if (i == null || i < 0 || i >= opts.length || !seen.add(i)) return null;
    out.add(i);
  }
  return {'options': opts, 'order': out};
}

Json? cardDrawData(Object? d) {
  if (d is! Map || d['deck'] is! String) return null;
  final deck = cardDecks[d['deck']];
  if (deck == null || !_cardsFrom(deck.cards, d['cards'])) return null;
  final cards = (d['cards'] as List).cast<String>().toList();
  if (deck.orientation) {
    final rev = _bools(d['reversed'], cards.length);
    if (rev == null) return null;
    return {'deck': d['deck'], 'cards': cards, 'reversed': rev};
  }
  if (d['reversed'] != null) return null;
  return {'deck': d['deck'], 'cards': cards};
}

Json? tarotReadingData(Object? d) {
  if (d is! Map || d['spread'] is! String) return null;
  final sp = tarotSpreads[d['spread']];
  if (sp == null) return null;
  final n = sp.positions.length;
  final cards = d['cards'];
  final rev = _bools(d['reversed'], n);
  if (cards is! List ||
      cards.length != n ||
      rev == null ||
      !_cardsFrom(cardDecks['tarot']!.cards, cards)) {
    return null;
  }
  return {
    'spread': d['spread'],
    'cards': cards.cast<String>().toList(),
    'reversed': rev,
  };
}

/// A value of a chance kind in the shape the reels draw
/// (lib/ui/message/chance_view.dart), which drew the same results before
/// they were kinds; null for a kind that is not one.
Json? chanceShape(String kind, Json d) => switch (kind) {
  'coin_flip' => {'kind': 'flip', 'outcome': d['outcome']},
  'pick' => {'kind': 'pick', 'options': d['options'], 'picked': d['picked']},
  'shuffle' => {
    'kind': 'shuffle',
    'options': d['options'],
    'order': d['order'],
  },
  'card_draw' => {
    'kind': 'draw',
    'deck': d['deck'],
    'cards': d['cards'],
    'reversed': ?d['reversed'],
  },
  'tarot_reading' => {
    'kind': 'tarot',
    'deck': 'tarot',
    'spread': d['spread'],
    'cards': d['cards'],
    'reversed': d['reversed'],
  },
  'dice_roll' => {'kind': 'roll', 'dice': d['dice'], 'rows': d['rows']},
  _ => null,
};

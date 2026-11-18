// The data of kind dice_roll (docs/DESIGN-MODULES.md sections 4.2 and 9.1;
// the schema DiceRoll of docs/openapi.yaml): dice the server rolled, as
// `{dice, rows}`, each row `{dice, terms, total}`, each term `{sign, count,
// sides, keep, keep_n, rolls, dropped, value}` for dice or `{sign, value}`
// for a number. The checks are the server's (internal/model/chance.go,
// DiceRoll.Valid) and the web chat's (shared/types/dice-roll/data.js), and
// testdata/types/dice_roll.json holds all three to the same answers. No
// Flutter.

import '../models/json.dart';

/// The limits a roll is held to (model.MaxDiceRows and the rest).
const maxDiceRows = 8, maxDiceTerms = 10, maxDice = 100, maxDieSides = 1000;

const _keeps = {'kh', 'kl', 'dh', 'dl'};

/// The largest whole number JavaScript and JSON readers agree on.
const _safe = 9007199254740991;

/// [v] as a whole number, or null when it is not one.
int? _whole(Object? v) {
  if (v is int) return v.abs() <= _safe ? v : null;
  if (v is double && v.isFinite && v == v.roundToDouble() && v.abs() <= _safe) {
    return v.toInt();
  }
  return null;
}

/// [v] as a list of whole numbers, or null.
List<int>? _wholes(Object? v) {
  if (v is! List) return null;
  final out = <int>[];
  for (final e in v) {
    final n = _whole(e);
    if (n == null) return null;
    out.add(n);
  }
  return out;
}

bool _named(Object? v) => v is String && v.isNotEmpty;

/// Checks a value's data and gives the roll as this client draws it, or
/// null when it does not hold together, which makes the value fall back a
/// step (to its alt, then its text). Fields this client does not know are
/// left out of what it draws; a `keep` it does not know is taken as its
/// default, none (day-one rule 2). The data itself is never changed: what
/// is returned is a new object.
Json? diceRollData(Object? d) {
  if (d is! Map || !_named(d['dice']) || d['rows'] is! List) return null;
  final rows = d['rows'] as List;
  if (rows.isEmpty || rows.length > maxDiceRows) return null;
  var dice = 0;
  final out = <Json>[];
  for (final row in rows) {
    if (row is! Map || !_named(row['dice']) || row['terms'] is! List) {
      return null;
    }
    final rowTotal = _whole(row['total']);
    final terms = row['terms'] as List;
    if (rowTotal == null || terms.isEmpty || terms.length > maxDiceTerms) {
      return null;
    }
    var total = 0;
    final drawn = <Json>[];
    for (final t in terms) {
      if (t is! Map) return null;
      final sign = _whole(t['sign']), value = _whole(t['value']);
      if ((sign != 1 && sign != -1) || value == null) return null;
      for (final k in const ['count', 'sides', 'keep_n']) {
        if (t.containsKey(k) && _whole(t[k]) == null) return null;
      }
      for (final k in const ['rolls', 'dropped']) {
        if (t.containsKey(k) && _wholes(t[k]) == null) return null;
      }
      if (t.containsKey('keep') && t['keep'] is! String) return null;
      final term = <String, dynamic>{'sign': sign, 'value': value};
      final count = _whole(t['count']) ?? 0;
      if (count > 0) {
        final sides = _whole(t['sides']) ?? 0;
        final rolls = t.containsKey('rolls') ? _wholes(t['rolls'])! : <int>[];
        if (rolls.length != count || sides < 2 || sides > maxDieSides) {
          return null;
        }
        if (rolls.any((r) => r < 1 || r > sides)) return null;
        final dropped = t.containsKey('dropped')
            ? _wholes(t['dropped'])!
            : <int>[];
        if (dropped.any((i) => i < 0 || i >= rolls.length) ||
            dropped.toSet().length != dropped.length) {
          return null;
        }
        dice += count;
        term
          ..['count'] = count
          ..['sides'] = sides
          ..['rolls'] = rolls
          ..['dropped'] = dropped;
        if (_keeps.contains(t['keep'])) {
          term['keep'] = t['keep'];
          if (t.containsKey('keep_n')) term['keep_n'] = _whole(t['keep_n']);
        }
      }
      total += sign! * value;
      drawn.add(term);
    }
    if (total != rowTotal) return null;
    out.add({'dice': row['dice'], 'terms': drawn, 'total': rowTotal});
  }
  if (dice > maxDice) return null;
  return {'dice': d['dice'], 'rows': out};
}

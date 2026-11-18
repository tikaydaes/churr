// The derivation `derive: 1` (docs/FAIR.md): a draw's 32-byte output turned
// into whole numbers, picks, shuffles, deals, yes or no and dice, exactly as
// the server's internal/fair does it and the web chat's shared/fair/derive.js,
// all three held to testdata/fair/derive.json (test/core/fair/derive_test.dart).
// Primitives and results are their JSON shapes, as a message carries them. No
// floating point: every number is a whole number below 2^53.

import 'dart:typed_data';

import 'package:crypto/crypto.dart';

/// The version of the derivation implemented here.
const deriveVersion = 1;

const _span = 4294967296; // 2^32
const maxN = 10000;
const maxCount = 1000;
const minInt = -2147483648;
const maxInt = 2147483647;
const maxDiceRows = 100;
const maxDiceTerms = 100;
const maxDiceCount = 1000;
const maxDiceTotal = 10000;
const maxDieSides = 1000000;
const maxExplode = 100;

typedef Json = Map<String, dynamic>;

/// Block k of object i's stream: SHA-256(beta ‖ u32be(i) ‖ u32be(k)).
Uint8List fairBlock(List<int> beta, int i, int k) {
  final b = Uint8List(beta.length + 8)..setAll(0, beta);
  final v = ByteData.sublistView(b);
  v.setUint32(beta.length, i);
  v.setUint32(beta.length + 4, k);
  return Uint8List.fromList(sha256.convert(b).bytes);
}

/// One chance object's stream: its blocks read as big-endian words, in order.
class FairStream {
  FairStream(List<int> beta, this.i) : _beta = Uint8List.fromList(beta);

  final Uint8List _beta;
  final int i;
  int _k = 0;
  ByteData? _buf;
  int _at = 32;

  /// Words taken.
  int used = 0;

  int word() {
    if (_at == 32) {
      _buf = ByteData.sublistView(fairBlock(_beta, i, _k));
      _k++;
      _at = 0;
    }
    final w = _buf!.getUint32(_at);
    _at += 4;
    used++;
    return w;
  }

  /// A whole number from 0 to n-1 by rejection; n = 1 takes no word.
  int below(int n) {
    if (n < 1 || n > _span) throw RangeError('fair: below out of range');
    if (n == 1) return 0;
    final limit = _span - (_span % n);
    for (;;) {
      final w = word();
      if (w < limit) return w % n;
    }
  }
}

/// A primitive derive 1 does not draw.
class PrimitiveError implements Exception {
  PrimitiveError(this.message);
  final String message;
  @override
  String toString() => 'fair: not a primitive of derive 1: $message';
}

Never _bad(String m) => throw PrimitiveError(m);

int _int(Object? v) => v == null
    ? 0
    : v is int
    ? v
    : _bad('not a whole number');

void _checkDice(Object? rowsRaw) {
  if (rowsRaw is! List || rowsRaw.isEmpty || rowsRaw.length > maxDiceRows) {
    _bad('rows');
  }
  var total = 0;
  for (final r in rowsRaw) {
    final terms = (r as Map)['terms'];
    if (terms is! List || terms.isEmpty || terms.length > maxDiceTerms) {
      _bad('terms');
    }
    for (final t in terms.cast<Map>()) {
      final count = _int(t['count']), sides = _int(t['sides']);
      final keep = (t['keep'] as String?) ?? '', n = _int(t['keep_n']);
      final success = _int(t['success']);
      final sign = t['sign'];
      if (sign != 1 && sign != -1) _bad('sign');
      if (count == 0) {
        if (keep != '' || t['explode'] == true || success != 0 || sides != 0) {
          _bad('a plain number has no dice');
        }
        continue;
      }
      if (count < 1 ||
          count > maxDiceCount ||
          sides < 2 ||
          sides > maxDieSides) {
        _bad('dice');
      }
      total += count;
      if (total > maxDiceTotal) _bad('too many dice');
      switch (keep) {
        case '':
          if (n != 0) _bad('keep_n');
        case 'kh' || 'kl':
          if (n < 1 || n > count) _bad('keep');
        case 'dh' || 'dl':
          if (n < 1 || n >= count) _bad('drop');
        default:
          _bad('keep');
      }
      if (success < 0 || success > sides) _bad('success');
    }
  }
}

/// Throws [PrimitiveError] unless [p] is a primitive of derive 1.
void checkPrimitive(Json p) {
  final min = _int(p['min']), max = _int(p['max']);
  final count = _int(p['count']), n = _int(p['n']), k = _int(p['k']);
  final replace = p['replace'] == true;
  switch (p['op']) {
    case 'int' || 'ints':
      if (min < minInt || max > maxInt || min > max) _bad('min and max');
      if (p['op'] == 'ints' && (count < 1 || count > maxCount)) _bad('count');
    case 'pick':
      if (n < 1 || n > maxN) _bad('n');
      if (k < 1 || (!replace && k > n) || (replace && k > maxCount)) {
        _bad('k');
      }
      final ws = p['weights'];
      if (ws != null) {
        if (ws is! List || ws.length != n) _bad('weights');
        var total = 0, positive = 0;
        for (final w in ws) {
          final x = _int(w);
          if (x < 0 || x > _span) _bad('a weight');
          total += x;
          if (x > 0) positive++;
        }
        if (total < 1 || total > _span) _bad('the weights in all');
        if (!replace && k > positive) _bad('k above the weighted');
      }
    case 'shuffle':
      if (n < 1 || n > maxN) _bad('n');
    case 'deal':
      final hands = p['hands'];
      if (n < 1 || n > maxN || hands is! List || hands.isEmpty) _bad('deal');
      if (hands.length > maxN) _bad('deal');
      var total = 0;
      for (final h in hands) {
        final x = _int(h);
        if (x < 1) _bad('a hand');
        total += x;
        if (total > n) _bad('the hands');
      }
    case 'yesno':
      final num = _int(p['num']), den = _int(p['den']);
      if (den < 1 || den > _span || num < 0 || num > den) _bad('num/den');
    case 'dice':
      _checkDice(p['rows']);
    default:
      _bad('op');
  }
}

/// Whether [p] is a primitive of derive 1.
bool isPrimitive(Json p) {
  try {
    checkPrimitive(p);
    return true;
  } on PrimitiveError {
    return false;
  }
}

List<int> _pickPlain(FairStream s, int n, int k) {
  final a = List<int>.generate(n, (j) => j);
  final out = <int>[];
  for (var t = 0; t < k; t++) {
    final j = n - 1 - t;
    final r = s.below(j + 1);
    final x = a[j];
    a[j] = a[r];
    a[r] = x;
    out.add(a[j]);
  }
  return out;
}

List<int> _pick(FairStream s, Json p) {
  final n = _int(p['n']), k = _int(p['k']);
  final replace = p['replace'] == true;
  final ws = p['weights'] as List?;
  if (ws == null) {
    if (!replace) return _pickPlain(s, n, k);
    return [for (var t = 0; t < k; t++) s.below(n)];
  }
  final w = [for (final x in ws) x as int];
  final out = <int>[];
  for (var t = 0; t < k; t++) {
    var r = s.below(w.fold(0, (a, b) => a + b));
    var j = 0;
    for (; j < w.length; j++) {
      if (r < w[j]) break;
      r -= w[j];
    }
    out.add(j);
    if (!replace) w[j] = 0;
  }
  return out;
}

/// The rolls of a term that do not count, by position, in increasing order.
List<int> droppedDice(String keep, int n, List<int> rolls) {
  if (keep.isEmpty) return const [];
  final drop = keep[0] == 'k' ? rolls.length - n : n;
  if (drop <= 0) return const [];
  final lowest = keep == 'dl' || keep == 'kh';
  bool before(int a, int b) => rolls[a] != rolls[b]
      ? (lowest ? rolls[a] < rolls[b] : rolls[a] > rolls[b])
      : a > b;
  final pos = List<int>.generate(rolls.length, (j) => j);
  for (var i = 1; i < pos.length; i++) {
    for (var j = i; j > 0 && before(pos[j], pos[j - 1]); j--) {
      final x = pos[j];
      pos[j] = pos[j - 1];
      pos[j - 1] = x;
    }
  }
  return pos.sublist(0, drop)..sort();
}

/// A rolled term's value: the sum of its counted rolls, or how many reached
/// its success number.
int scoreDice(Map t) {
  final skip = {for (final j in (t['dropped'] as List? ?? const [])) j as int};
  final success = _int(t['success']);
  final rolls = (t['rolls'] as List? ?? const []).cast<int>();
  var v = 0;
  for (var j = 0; j < rolls.length; j++) {
    if (skip.contains(j)) continue;
    if (success > 0) {
      if (rolls[j] >= success) v++;
    } else {
      v += rolls[j];
    }
  }
  return v;
}

List<Json> _rollRows(FairStream s, List rows) {
  final out = <Json>[];
  for (final r in rows.cast<Map>()) {
    final row = <String, dynamic>{'terms': <Json>[], 'total': 0};
    if ((r['dice'] as String?)?.isNotEmpty ?? false) row['dice'] = r['dice'];
    var total = 0;
    for (final src in (r['terms'] as List).cast<Map>()) {
      final t = Map<String, dynamic>.from(src)
        ..remove('rolls')
        ..remove('dropped');
      final count = _int(t['count']), sides = _int(t['sides']);
      if (count > 0) {
        final rolls = <int>[];
        var extra = 0;
        for (var d = 0; d < count; d++) {
          var v = 1 + s.below(sides);
          rolls.add(v);
          while (t['explode'] == true && v == sides && extra < maxExplode) {
            extra++;
            v = 1 + s.below(sides);
            rolls.add(v);
          }
        }
        t['rolls'] = rolls;
        final dr = droppedDice(
          (t['keep'] as String?) ?? '',
          _int(t['keep_n']),
          rolls,
        );
        if (dr.isNotEmpty) t['dropped'] = dr;
        t['value'] = scoreDice(t);
      }
      (row['terms'] as List<Json>).add(t);
      total += (t['sign'] as int) * _int(t['value']);
    }
    row['total'] = total;
    out.add(row);
  }
  return out;
}

/// A primitive's result, derived from the stream.
Json drawPrimitive(FairStream s, Json p) {
  checkPrimitive(p);
  final min = _int(p['min']), max = _int(p['max']);
  switch (p['op']) {
    case 'int':
      return {'value': min + s.below(max - min + 1)};
    case 'ints':
      return {
        'values': [
          for (var j = 0; j < _int(p['count']); j++)
            min + s.below(max - min + 1),
        ],
      };
    case 'pick':
      return {'picked': _pick(s, p)};
    case 'shuffle':
      final n = _int(p['n']);
      final a = List<int>.generate(n, (j) => j);
      for (var j = n - 1; j >= 1; j--) {
        final r = s.below(j + 1);
        final x = a[j];
        a[j] = a[r];
        a[r] = x;
      }
      return {'order': a};
    case 'deal':
      final hands = (p['hands'] as List).cast<int>();
      final taken = _pickPlain(s, _int(p['n']), hands.fold(0, (a, b) => a + b));
      final dealt = <List<int>>[];
      var at = 0;
      for (final h in hands) {
        dealt.add(taken.sublist(at, at + h));
        at += h;
      }
      return {'dealt': dealt};
    case 'yesno':
      return {'yes': s.below(_int(p['den'])) < _int(p['num'])};
    case 'dice':
      return {'rows': _rollRows(s, p['rows'] as List)};
  }
  _bad('op');
}

/// Whether [rows] (a dice_roll's rows) are what the stream rolls for them:
/// every roll, every dropped die, every value and every total.
bool sameRoll(FairStream s, List rows) {
  List<Json> again;
  try {
    again = (drawPrimitive(s, {'op': 'dice', 'rows': rows})['rows'] as List)
        .cast<Json>();
  } on PrimitiveError {
    return false;
  }
  if (again.length != rows.length) return false;
  bool same(Object? a, Object? b) =>
      _listEq((a as List?) ?? const [], (b as List?) ?? const []);
  for (var i = 0; i < rows.length; i++) {
    final a = again[i], b = rows[i] as Map;
    final ta = a['terms'] as List, tb = b['terms'] as List;
    if (a['total'] != b['total'] || ta.length != tb.length) return false;
    for (var j = 0; j < ta.length; j++) {
      final x = ta[j] as Map, y = tb[j] as Map;
      if (_int(x['value']) != _int(y['value']) ||
          !same(x['rolls'], y['rolls']) ||
          !same(x['dropped'], y['dropped'])) {
        return false;
      }
    }
  }
  return true;
}

bool _listEq(List a, List b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

// The app's derivation (lib/core/fair/derive.dart) held to
// testdata/fair/derive.json, the file the server's (internal/fair) and the web
// chat's (shared/fair/derive.js) run: the stream's blocks, rejection, every
// primitive in order from one object's stream, re-deriving a roll, and the
// primitives refused.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/fair/derive.dart';
import 'package:flutter_test/flutter_test.dart';

List<int> fromHex(String hex) => [
  for (var i = 0; i < hex.length; i += 2)
    int.parse(hex.substring(i, i + 2), radix: 16),
];

String toHex(List<int> bytes) =>
    bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

/// Equal as JSON, whatever the order of keys.
bool sameJson(Object? a, Object? b) {
  if (a is List && b is List) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (!sameJson(a[i], b[i])) return false;
    }
    return true;
  }
  if (a is Map && b is Map) {
    if (a.length != b.length) return false;
    return a.keys.every((k) => b.containsKey(k) && sameJson(a[k], b[k]));
  }
  return a == b;
}

void main() {
  // `flutter test` runs from app/.
  final v =
      jsonDecode(File('../testdata/fair/derive.json').readAsStringSync())
          as Map<String, dynamic>;

  test('the vectors file is derive 1\'s', () {
    expect(v['derive'], deriveVersion);
    expect((v['cases'] as List).isNotEmpty, isTrue);
  });

  test('the stream\'s blocks', () {
    for (final s in (v['streams'] as List).cast<Map<String, dynamic>>()) {
      expect(
        toHex(fairBlock(fromHex(s['beta'] as String), s['i'], s['k'])),
        s['block'],
      );
    }
  });

  test('whole numbers below n, by rejection', () {
    for (final b in (v['below'] as List).cast<Map<String, dynamic>>()) {
      final s = FairStream(fromHex(b['beta'] as String), b['i'] as int);
      final got = [for (final n in (b['ns'] as List).cast<int>()) s.below(n)];
      expect(got, b['values']);
      expect(s.used, b['words']);
    }
  });

  test('every case, in order from one object\'s stream', () {
    for (final c in (v['cases'] as List).cast<Map<String, dynamic>>()) {
      final s = FairStream(fromHex(c['beta'] as String), c['i'] as int);
      final draws = (c['draws'] as List).cast<Map<String, dynamic>>();
      final got = [for (final p in draws) drawPrimitive(s, p)];
      expect(sameJson(got, c['results']), isTrue, reason: c['label']);
      expect(s.used, c['words'], reason: c['label']);
    }
  });

  test('a roll is re-derived, and a changed die is caught', () {
    for (final c in (v['cases'] as List).cast<Map<String, dynamic>>()) {
      final draws = (c['draws'] as List).cast<Map<String, dynamic>>();
      for (var j = 0; j < draws.length; j++) {
        if (draws[j]['op'] != 'dice') continue;
        FairStream upTo() {
          final s = FairStream(fromHex(c['beta'] as String), c['i'] as int);
          for (final p in draws.sublist(0, j)) {
            drawPrimitive(s, p);
          }
          return s;
        }

        final rows = (c['results'] as List)[j]['rows'] as List;
        expect(sameRoll(upTo(), rows), isTrue, reason: c['label']);
        final forged = jsonDecode(jsonEncode(rows)) as List;
        final t = ((forged[0] as Map)['terms'] as List).cast<Map>().firstWhere(
          (t) => t['rolls'] != null,
        );
        final rolls = t['rolls'] as List;
        rolls[0] = rolls[0] == 1 ? 2 : 1;
        expect(sameRoll(upTo(), forged), isFalse, reason: c['label']);
      }
    }
  });

  test('the primitives every implementation refuses', () {
    for (final c in (v['invalid'] as List).cast<Map<String, dynamic>>()) {
      expect(
        isPrimitive(c['draw'] as Map<String, dynamic>),
        isFalse,
        reason: c['label'],
      );
    }
  });
}

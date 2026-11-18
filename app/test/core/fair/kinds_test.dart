// The built-in chance kinds under derive 1 (docs/FAIR.md section 5a), held
// to testdata/fair/kinds.json as the server's internal/fair and the web
// chat's e2e/fair.mjs are: each value asks the primitives its data names,
// they give the results recorded, the value re-derives and claims what it
// shows, and each invalid value does not re-derive.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/fair/derive.dart';
import 'package:churr/core/fair/kinds.dart';
import 'package:churr/core/types/catalogue.dart';
import 'package:flutter_test/flutter_test.dart';

List<int> _hex(String s) => [
  for (var i = 0; i < s.length; i += 2)
    int.parse(s.substring(i, i + 2), radix: 16),
];

/// A primitive or result as JSON with its empty fields left out, as Go
/// writes it, so the two compare by what they hold.
String _norm(Object? v) => jsonEncode(_strip(v));
Object? _strip(Object? v) {
  if (v is List) return [for (final x in v) _strip(x)];
  if (v is Map) {
    final keys = v.keys.cast<String>().toList()..sort();
    return {
      for (final k in keys)
        if (v[k] != null && v[k] != 0 && v[k] != false) k: _strip(v[k]),
    };
  }
  return v;
}

void main() {
  final f = jsonDecode(
    File('../testdata/fair/kinds.json').readAsStringSync(),
  ) as Map<String, dynamic>;

  test('the file is derive 1\'s', () {
    expect(f['derive'], 1);
    expect((f['cases'] as List).length, greaterThanOrEqualTo(10));
  });
  for (final c in (f['cases'] as List).cast<Map>()) {
    test('${c['label']}', () {
      final kind = c['kind'] as String;
      final data = catalogueKinds[kind]!.data(c['data'])!;
      final beta = _hex(c['beta'] as String);
      final i = c['i'] as int;
      final asks = kindAsks(kind, data)!;
      expect(_norm(asks), _norm(c['asks']));
      final s = FairStream(beta, i);
      final results = [for (final p in asks) drawPrimitive(s, p)];
      expect(_norm(results), _norm(c['results']));
      expect(rederive(kind, data, beta, i), isTrue);
      expect(_norm(kindClaims(kind, data, beta, i)), _norm(c['results']));
    });
  }
  for (final c in (f['invalid'] as List).cast<Map>()) {
    test('invalid: ${c['label']}', () {
      final kind = c['kind'] as String;
      final data = catalogueKinds[kind]!.data(c['data'])!;
      expect(
        rederive(kind, data, _hex(c['beta'] as String), c['i'] as int),
        isFalse,
      );
    });
  }
}

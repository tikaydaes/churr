// Numbers in another base and code points, against the cases the web chat
// passes too.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/objects/codepoint.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final cases = jsonDecode(
    File('../testdata/objects/codepoints.json').readAsStringSync(),
  ) as Map<String, dynamic>;
  for (final c in cases['numbers'] as List) {
    test('number ${c['value']}', () {
      final r = numberRows('${c['value']}');
      expect(
        [r.decimal, r.hex, r.octal, r.binary, r.copy, r.codepoint],
        [
          c['decimal'],
          c['hex'],
          c['octal'],
          c['binary'],
          c['copy'],
          c['codepoint'],
        ],
      );
    });
  }
  for (final c in cases['codepoints'] as List) {
    test('code point ${c['value']}', () {
      final f = codePointFacts('${c['value']}');
      expect(
        [
          f.value,
          f.char,
          f.decimal,
          f.utf8,
          f.utf16,
          f.html,
          f.json,
          f.js,
          f.python,
          f.shownKind,
          f.shown,
        ],
        [
          c['value'],
          c['char'],
          c['decimal'],
          c['utf8'],
          c['utf16'],
          c['html'],
          c['json'],
          c['js'],
          c['python'],
          c['shown']['kind'],
          c['shown']['text'],
        ],
      );
    });
  }
  test('a name is asked for once a session', () async {
    var asked = 0;
    Future<CodePointName?> lookup(String v) async {
      asked++;
      return (
        name: 'GRINNING FACE',
        category: 'So',
        categoryName: 'Other Symbol',
      );
    }

    expect((await codePointName('U+1F600', lookup))?.name, 'GRINNING FACE');
    await codePointName('U+1F600', lookup);
    expect(asked, 1);
    expect(
      await codePointName('U+0041', (_) async => throw Exception('no')),
      isNull,
    );
  });
}

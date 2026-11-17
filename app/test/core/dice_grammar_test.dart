// The dice grammar the composer reads inline dice with (ruling R73),
// against /roll's cases.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/markdown/dice.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final d = jsonDecode(
    File('../testdata/commands/dice.json').readAsStringSync(),
  ) as Map<String, dynamic>;
  for (final c in d['parse'] as List) {
    test('reads ${jsonEncode(c['input'])}', () {
      expect(parseDice('${c['input']}'), c['dice']);
    });
  }
  for (final c in d['d6'] as List) {
    test('/dice ${jsonEncode(c['input'])} is ${c['dice']}', () {
      expect(diceD6('${c['input']}'), c['dice']);
    });
  }
  for (final c in d['errors'] as List) {
    test('refuses ${jsonEncode(c['input'])}', () {
      expect(parseDice('${c['input']}'), isNull);
    });
  }
}

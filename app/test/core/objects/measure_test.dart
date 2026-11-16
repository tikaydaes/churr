// The equivalents shown beside measurements (ruling R73), against the
// cases the web chat passes too.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/objects/measure.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final cases = jsonDecode(
    File('../testdata/objects/conversions.json').readAsStringSync(),
  ) as Map<String, dynamic>;
  for (final c in cases['conversions'] as List) {
    test('${c['value']} ${c['unit']} for ${c['system']}', () {
      expect(
        convertMeasure('${c['value']}', '${c['unit']}', '${c['system']}'),
        c['shows'],
      );
    });
  }
  for (final c in cases['regions'] as List) {
    test('${c['locale']} measures in ${c['system']}', () {
      expect(localeSystem('${c['locale']}'), c['system']);
    });
  }
  test('a choice wins over the region', () {
    expect(viewerMeasureSystem('off', 'US'), 'off');
    expect(viewerMeasureSystem('', 'US'), 'imperial');
    expect(viewerMeasureSystem('', null), 'metric');
  });
}

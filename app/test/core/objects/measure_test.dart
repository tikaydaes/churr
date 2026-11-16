// The equivalents shown beside measurements (ruling R73), against the
// cases the web chat passes too.

import 'dart:convert';
import 'dart:io';

import 'package:churr/core/objects/measure.dart';
import 'package:churr/core/objects/units.dart';
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
  final units = jsonDecode(
    File('../testdata/objects/units.json').readAsStringSync(),
  ) as Map<String, dynamic>;
  test('every unit as the registry has it', () {
    for (final u in units['units'] as List) {
      final m = measureUnitTable[u['code']]!;
      expect(
        [m.cat, m.sym, m.size, m.sys, m.alt],
        [u['category'], u['symbol'], (u['size'] as num?)?.toDouble(), u['system'], u['alt']],
        reason: '${u['code']}',
      );
    }
    expect(measureUnitTable.length, (units['units'] as List).length);
  });
  for (final c in units['numbers'] as List) {
    test('written ${c['value']}', () {
      expect(measureWritten((c['value'] as num).toDouble()), c['shows']);
    });
  }
  for (final c in units['rows'] as List) {
    test('rows of ${c['value']} ${c['unit']} for ${c['system']}', () {
      expect(
        measureRows('${c['value']}', '${c['unit']}', '${c['system']}')
            .map((r) => r.text)
            .toList(),
        c['rows'],
      );
    });
  }
  test('a locale\'s marks', () {
    expect(measureSeparators('5,000,000.25', '.', ','), '5.000.000,25');
  });
}

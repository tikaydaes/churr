import 'dart:convert';
import 'dart:io';

import 'package:churr/core/commands/remind.dart';
import 'package:flutter_test/flutter_test.dart';

/// /remind against the shared cases in testdata/commands/remind.json.
DateTime _local(String s) {
  final m = RegExp(r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$').firstMatch(s)!;
  return DateTime(
    int.parse(m[1]!),
    int.parse(m[2]!),
    int.parse(m[3]!),
    int.parse(m[4]!),
    int.parse(m[5]!),
  );
}

void main() {
  test('the shared cases (testdata/commands/remind.json)', () {
    final v =
        jsonDecode(File('../testdata/commands/remind.json').readAsStringSync())
            as Map;
    final now = _local(v['now'] as String);
    final errors = (v['errors'] as List).cast<String>();
    for (final c in (v['cases'] as List).cast<Map>()) {
      final args = c['args'] as String;
      try {
        final got = parseRemind(args, now);
        expect(c['error'], isNull, reason: args);
        final want = c['want'] as Map;
        expect(got.target, want['target'], reason: args);
        expect(got.what, want['what'], reason: args);
        expect(
          got.at,
          want['at'] == null ? isNull : _local(want['at'] as String),
          reason: args,
        );
        expect(got.repeat, want['repeat'], reason: args);
      } on RemindError catch (e) {
        expect(e.code, c['error'], reason: '$args: ${e.message}');
        expect(errors, contains(e.code), reason: args);
      }
    }
  });

  test('a repeat said back in words', () {
    expect(
      repeatWords({'every': 'day', 'days': '1111100', 'at': '09:30'}),
      'every weekday at 09:30',
    );
    expect(
      repeatWords({'every': 'day', 'days': '1001000', 'at': '07:15'}),
      'every monday, thursday at 07:15',
    );
    expect(repeatWords({'every': 'month', 'at': '09:00'}), 'every month at 09:00');
  });
}

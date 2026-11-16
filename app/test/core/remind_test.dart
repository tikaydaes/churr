import 'dart:convert';
import 'dart:io';

import 'package:churr/core/commands/remind.dart';
import 'package:churr/core/instant/zone.dart';
import 'package:flutter_test/flutter_test.dart';

/// /remind against the shared cases in testdata/commands/remind.json.
/// A time as the shared vectors write it: wall clock in [zone], or an
/// exact moment when it ends in Z.
DateTime _local(String s, String zone) {
  if (s.endsWith('Z')) return DateTime.parse(s);
  final m = RegExp(r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$').firstMatch(s)!;
  int g(int i) => int.parse(m[i]!);
  return DateTime.fromMillisecondsSinceEpoch(
    fromWall(utcMs(g(1), g(2), g(3), g(4), g(5)), zone)!,
    isUtc: true,
  );
}

void main() {
  test('the shared cases (testdata/commands/remind.json)', () {
    final v =
        jsonDecode(File('../testdata/commands/remind.json').readAsStringSync())
            as Map;
    final errors = (v['errors'] as List).cast<String>();
    for (final c in (v['cases'] as List).cast<Map>()) {
      final args = c['args'] as String;
      final zone = c['zone'] as String? ?? v['zone'] as String;
      final now = _local(c['now'] as String? ?? v['now'] as String, zone);
      try {
        final got = parseRemind(args, now, zone);
        expect(c['error'], isNull, reason: args);
        final want = c['want'] as Map;
        expect(got.target, want['target'], reason: args);
        expect(got.what, want['what'], reason: args);
        expect(
          got.at?.millisecondsSinceEpoch,
          want['at'] == null
              ? isNull
              : _local(want['at'] as String, zone).millisecondsSinceEpoch,
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
